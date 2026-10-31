package export

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// fakeSubStore counts conversions per platform and returns canned content or errors.
type fakeSubStore struct {
	mu      sync.Mutex
	calls   map[string]int
	content string
	err     error
	release chan struct{} // if set, fetch blocks until closed
}

func newFakeSubStore(content string) *fakeSubStore {
	return &fakeSubStore{calls: map[string]int{}, content: content}
}

func (f *fakeSubStore) fetch(ctx context.Context, platform string) ([]byte, error) {
	f.mu.Lock()
	f.calls[platform]++
	release, content, err := f.release, f.content, f.err
	f.mu.Unlock()
	if release != nil {
		<-release
	}
	if err != nil {
		return nil, err
	}
	return []byte(content + ":" + platform), nil
}

func (f *fakeSubStore) set(content string, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.content, f.err = content, err
}

func (f *fakeSubStore) count(platform string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls[platform]
}

type testClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *testClock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *testClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

func newTestCacheIn(t *testing.T, dir string, fake *fakeSubStore, clock *testClock) (*Cache, string) {
	t.Helper()
	outDir := t.TempDir()
	c := New(dir, fake.fetch, func() string { return outDir })
	c.now = clock.now
	return c, outDir
}

func newTestCache(t *testing.T, fake *fakeSubStore) (*Cache, *testClock, string) {
	t.Helper()
	clock := &testClock{t: time.Date(2026, 9, 15, 14, 0, 0, 0, time.UTC)}
	c, outDir := newTestCacheIn(t, filepath.Join(t.TempDir(), "export"), fake, clock)
	return c, clock, outDir
}

// generateAndWait calls Generate and waits for its background conversion.
func generateAndWait(t *testing.T, c *Cache, id string) Status {
	t.Helper()
	if _, err := c.Generate(id); err != nil {
		t.Fatalf("Generate(%q): %v", id, err)
	}
	c.wg.Wait()
	return c.status(mustLookup(t, id))
}

func readServed(t *testing.T, c *Cache, id string) string {
	t.Helper()
	s, err := c.Open(id)
	if err != nil {
		t.Fatalf("Open(%q): %v", id, err)
	}
	defer s.File.Close()
	data, err := io.ReadAll(s.File)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	return string(data)
}

func mustLookup(t *testing.T, id string) Target {
	t.Helper()
	target, ok := Lookup(id)
	if !ok {
		t.Fatalf("unknown target %q", id)
	}
	return target
}

func TestOpen_RequiresGenerate(t *testing.T) {
	c, _, _ := newTestCache(t, newFakeSubStore("x"))

	if _, err := c.Open("surge"); !errors.Is(err, ErrNotGenerated) {
		t.Fatalf("Open before Generate: got %v, want ErrNotGenerated", err)
	}
	// Preset and unknown targets are never served from /export.
	for _, id := range []string{"mihomo", "v2ray", "nope"} {
		if _, err := c.Open(id); !errors.Is(err, ErrUnknownTarget) {
			t.Errorf("Open(%q): got %v, want ErrUnknownTarget", id, err)
		}
	}
}

func TestGenerate_RunsInBackground(t *testing.T) {
	fake := newFakeSubStore("round1")
	fake.release = make(chan struct{})
	c, _, _ := newTestCache(t, fake)

	st, err := c.Generate("surge")
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if st.State != StateGenerating || !st.Enabled {
		t.Fatalf("status returned by Generate = %+v, want enabled and generating", st)
	}

	close(fake.release)
	c.wg.Wait()

	st = c.status(mustLookup(t, "surge"))
	if st.State != StateReady || st.GeneratedAt == nil || st.Size == 0 {
		t.Fatalf("status after conversion = %+v", st)
	}
	if got := readServed(t, c, "surge"); got != "round1:Surge" {
		t.Fatalf("served %q, want round1:Surge", got)
	}
}

func TestGenerate_UnknownAndPreset(t *testing.T) {
	c, _, _ := newTestCache(t, newFakeSubStore("x"))
	if _, err := c.Generate("nope"); !errors.Is(err, ErrUnknownTarget) {
		t.Errorf("unknown target: got %v", err)
	}
	if _, err := c.Generate("mihomo"); !errors.Is(err, ErrPreset) {
		t.Errorf("preset target: got %v", err)
	}
}

func TestGenerate_RepeatedClicksShareOneConversion(t *testing.T) {
	fake := newFakeSubStore("round1")
	fake.release = make(chan struct{})
	c, _, _ := newTestCache(t, fake)

	for i := 0; i < 8; i++ {
		st, err := c.Generate("loon")
		if err != nil || st.State != StateGenerating {
			t.Fatalf("click %d: status %+v, err %v", i, st, err)
		}
	}
	close(fake.release)
	c.wg.Wait()

	if got := fake.count("Loon"); got != 1 {
		t.Fatalf("sub-store called %d times, want 1", got)
	}
}

func TestGenerate_Cooldown(t *testing.T) {
	fake := newFakeSubStore("round1")
	c, clock, _ := newTestCache(t, fake)

	generateAndWait(t, c, "surge")
	st, err := c.Generate("surge")
	var cd *CooldownError
	if !errors.As(err, &cd) {
		t.Fatalf("second Generate: got %v, want CooldownError", err)
	}
	if st.Cooldown != 30 {
		t.Errorf("cooldown = %d, want 30", st.Cooldown)
	}

	clock.advance(DefaultCooldown + time.Second)
	generateAndWait(t, c, "surge")
	if got := fake.count("Surge"); got != 2 {
		t.Fatalf("sub-store called %d times, want 2", got)
	}
}

func TestGenerate_FailureDoesNotStartCooldown(t *testing.T) {
	fake := newFakeSubStore("x")
	fake.set("", errors.New("sub-store 返回 HTTP 500"))
	c, _, _ := newTestCache(t, fake)

	st := generateAndWait(t, c, "stash")
	if st.State != StateFailed || st.Error == "" || st.GeneratedAt != nil || !st.Enabled {
		t.Fatalf("status after failure = %+v", st)
	}

	fake.set("ok", nil)
	if st := generateAndWait(t, c, "stash"); st.State != StateReady || st.Error != "" {
		t.Fatalf("status after retry = %+v", st)
	}
}

func TestRoundComplete_RegeneratesOnlyEnabledTargets(t *testing.T) {
	fake := newFakeSubStore("round1")
	c, _, _ := newTestCache(t, fake)
	generateAndWait(t, c, "surge")

	c.Advance()
	if st := c.status(mustLookup(t, "surge")); st.State != StateOutdated {
		t.Fatalf("state after Advance = %q, want outdated", st.State)
	}

	fake.set("round2", nil)
	c.RegenerateEnabled()

	if st := c.status(mustLookup(t, "surge")); st.State != StateReady {
		t.Fatalf("state after regenerate = %q, want ready", st.State)
	}
	if got := readServed(t, c, "surge"); got != "round2:Surge" {
		t.Fatalf("served %q, want round2:Surge", got)
	}
	if got := fake.count("Loon"); got != 0 {
		t.Fatalf("never-generated target was converted %d times", got)
	}
	if st := c.status(mustLookup(t, "loon")); st.State != StateNone || st.Enabled {
		t.Fatalf("loon status = %+v, want none and disabled", st)
	}
}

func TestRegenerateFailure_KeepsServingPreviousFile(t *testing.T) {
	fake := newFakeSubStore("round1")
	c, _, _ := newTestCache(t, fake)
	generateAndWait(t, c, "uri")

	c.Advance()
	fake.set("", errors.New("sub-store 返回 HTTP 500"))
	c.RegenerateEnabled()

	st := c.status(mustLookup(t, "uri"))
	if st.State != StateFailed || st.GeneratedAt == nil {
		t.Fatalf("status = %+v, want failed with previous file", st)
	}
	if got := readServed(t, c, "uri"); got != "round1:URI" {
		t.Fatalf("served %q, want previous round content", got)
	}
}

func TestRestore_KeepsTargetsFromPreviousRun(t *testing.T) {
	fake := newFakeSubStore("run1")
	clock := &testClock{t: time.Date(2026, 9, 15, 14, 0, 0, 0, time.UTC)}
	dir := filepath.Join(t.TempDir(), "export")

	first, _ := newTestCacheIn(t, dir, fake, clock)
	generateAndWait(t, first, "surge")
	first.Advance()
	first.RegenerateEnabled()
	tmp := filepath.Join(dir, ".tmp-surge-123")
	if err := os.WriteFile(tmp, []byte("partial"), 0o600); err != nil {
		t.Fatal(err)
	}

	// Simulate a restart.
	clock.advance(time.Minute)
	second, _ := newTestCacheIn(t, dir, fake, clock)

	st := second.status(mustLookup(t, "surge"))
	if st.State != StateOutdated || !st.Enabled {
		t.Fatalf("restored status = %+v, want enabled and outdated", st)
	}
	if got := readServed(t, second, "surge"); got != "run1:Surge" {
		t.Fatalf("served %q, want previous run content", got)
	}
	if st := second.status(mustLookup(t, "loon")); st.Enabled {
		t.Fatal("loon was never generated but is enabled")
	}
	if _, err := os.Stat(tmp); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("temp leftover not removed: %v", err)
	}

	fake.set("run2", nil)
	second.RegenerateEnabled()
	if st := second.status(mustLookup(t, "surge")); st.State != StateReady {
		t.Fatalf("state after first round = %q, want ready", st.State)
	}
	if got := readServed(t, second, "surge"); got != "run2:Surge" {
		t.Fatalf("served %q, want run2:Surge", got)
	}
	if got := len(second.entries("surge")); got != 1 {
		t.Fatalf("%d files left, want 1", got)
	}
}

func TestDisable_RemovesFilesAndStopsRebuilds(t *testing.T) {
	fake := newFakeSubStore("round1")
	c, _, _ := newTestCache(t, fake)
	generateAndWait(t, c, "surge")

	st, err := c.Disable("surge")
	if err != nil {
		t.Fatalf("Disable: %v", err)
	}
	if st.State != StateNone || st.Enabled {
		t.Fatalf("status after Disable = %+v", st)
	}
	if _, err := c.Open("surge"); !errors.Is(err, ErrNotGenerated) {
		t.Fatalf("Open after Disable: got %v, want ErrNotGenerated", err)
	}

	c.Advance()
	c.RegenerateEnabled()
	if got := fake.count("Surge"); got != 1 {
		t.Fatalf("disabled target was rebuilt (%d conversions)", got)
	}

	// Not restored after a restart.
	restarted := New(c.dir, fake.fetch, c.outputDir)
	if st := restarted.status(mustLookup(t, "surge")); st.Enabled || st.State != StateNone {
		t.Fatalf("status after restart = %+v", st)
	}

	// Disabling clears the cooldown, so it can be enabled again right away.
	if st := generateAndWait(t, c, "surge"); st.State != StateReady {
		t.Fatalf("status after re-enable = %+v", st)
	}

	if _, err := c.Disable("mihomo"); !errors.Is(err, ErrPreset) {
		t.Errorf("preset: got %v", err)
	}
	if _, err := c.Disable("nope"); !errors.Is(err, ErrUnknownTarget) {
		t.Errorf("unknown: got %v", err)
	}
}

func TestDisable_DuringConversionDropsResult(t *testing.T) {
	fake := newFakeSubStore("round1")
	fake.release = make(chan struct{})
	c, _, _ := newTestCache(t, fake)

	if _, err := c.Generate("stash"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Disable("stash"); err != nil {
		t.Fatal(err)
	}
	close(fake.release)
	c.wg.Wait()

	if _, err := c.Open("stash"); !errors.Is(err, ErrNotGenerated) {
		t.Fatalf("Open: got %v, want ErrNotGenerated", err)
	}
	if st := c.status(mustLookup(t, "stash")); st.State != StateNone || st.Enabled {
		t.Fatalf("status = %+v, want none and disabled", st)
	}
}

// store writes and publishes content for id at the given round.
func store(t *testing.T, c *Cache, id string, gen uint64, content string) {
	t.Helper()
	tmp, err := c.writeTemp(id, []byte(content))
	if err != nil {
		t.Fatal(err)
	}
	if err := c.publish(id, gen, tmp); err != nil {
		t.Fatal(err)
	}
}

func TestWriteTemp_NotServedUntilPublished(t *testing.T) {
	c, _, _ := newTestCache(t, newFakeSubStore("x"))

	tmp, err := c.writeTemp("surge", []byte("pending"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Open("surge"); !errors.Is(err, ErrNotGenerated) {
		t.Fatalf("Open before publish: got %v, want ErrNotGenerated", err)
	}
	if st := c.status(mustLookup(t, "surge")); st.State != StateNone {
		t.Fatalf("state before publish = %q, want none", st.State)
	}

	if err := c.publish("surge", 0, tmp); err != nil {
		t.Fatal(err)
	}
	if got := readServed(t, c, "surge"); got != "pending" {
		t.Fatalf("served %q after publish", got)
	}
	if _, err := os.Stat(tmp); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("temp file still present after publish: %v", err)
	}
}

func TestPublish_OlderRoundNeverOverridesNewer(t *testing.T) {
	c, clock, _ := newTestCache(t, newFakeSubStore("x"))

	store(t, c, "surge", 2, "new")
	// A conversion from the previous round finishes later.
	clock.advance(time.Second)
	store(t, c, "surge", 1, "old")
	if got := readServed(t, c, "surge"); got != "new" {
		t.Fatalf("served %q, want new", got)
	}

	clock.advance(time.Second)
	store(t, c, "surge", 3, "newest")
	if got := len(c.entries("surge")); got != 1 {
		t.Fatalf("%d files left, want only the newest", got)
	}
	if got := readServed(t, c, "surge"); got != "newest" {
		t.Fatalf("served %q, want newest", got)
	}
}

func TestStatuses_PresetReadsOutputDir(t *testing.T) {
	c, _, outDir := newTestCache(t, newFakeSubStore("x"))
	if err := os.WriteFile(filepath.Join(outDir, "mihomo.yaml"), []byte("proxies: []"), 0o644); err != nil {
		t.Fatal(err)
	}

	byID := map[string]Status{}
	for _, s := range c.Statuses() {
		byID[s.ID] = s
	}
	if len(byID) != len(targets) {
		t.Fatalf("got %d statuses, want %d", len(byID), len(targets))
	}
	if s := byID["mihomo"]; s.State != StatePreset || s.Size == 0 || s.Path != "/sub/mihomo.yaml" {
		t.Errorf("mihomo status = %+v", s)
	}
	if s := byID["v2ray"]; s.State != StateNone || !s.Preset {
		t.Errorf("v2ray status = %+v", s)
	}
	if s := byID["sing-box"]; s.Path != "/export/sing-box" || s.Preset {
		t.Errorf("sing-box status = %+v", s)
	}
}
