// Package export serves check results as client subscriptions at /export/:target.
//
// Only the authenticated admin API starts a sub-store conversion (Generate, in
// the background); the public route only reads files that already exist, so
// anonymous traffic never reaches sub-store. A generated target stays enabled,
// across restarts too, and is rebuilt after every check round until disabled.
//
// Files are named <id>.g<round>.<unixnano>. The newest round wins, so a slow
// conversion from an older round can never replace a newer one.
package export

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"math"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/beck-8/subs-check/config"
	"github.com/beck-8/subs-check/save/method"
	"github.com/beck-8/subs-check/utils"
	"golang.org/x/sync/singleflight"
)

const (
	fetchTimeout   = 60 * time.Second
	maxContentSize = 64 << 20

	// DefaultCooldown is the minimum gap between two manual generations of a target.
	DefaultCooldown = 30 * time.Second
)

// Target states reported to the admin UI.
const (
	StateNone       = "none"       // not generated
	StateReady      = "ready"      // generated from the current round
	StateOutdated   = "outdated"   // from a previous round or run, waiting for rebuild
	StateGenerating = "generating" // conversion in progress
	StateFailed     = "failed"     // last conversion failed; any old file is still served
	StatePreset     = "preset"     // pre-generated every round
)

var (
	ErrUnknownTarget = errors.New("不支持的导出格式")
	ErrNotGenerated  = errors.New("该格式尚未生成，请在管理页面的导出订阅中生成")
	ErrPreset        = errors.New("该格式每轮检测完成后自动生成，无需手动生成")
)

// CooldownError is returned when a target was generated too recently.
type CooldownError struct {
	Remaining time.Duration
}

func (e *CooldownError) Error() string {
	return fmt.Sprintf("操作过于频繁，请 %d 秒后再试", int(math.Ceil(e.Remaining.Seconds())))
}

// Target is an exportable subscription format.
type Target struct {
	ID     string `json:"id"`
	Label  string `json:"label"`
	Group  string `json:"group"` // client | core
	Note   string `json:"note,omitempty"`
	Path   string `json:"path"`   // subscription URL path
	Preset bool   `json:"preset"` // pre-generated into the output dir

	platform    string // sub-store target name
	file        string // pre-generated file name
	contentType string
}

const textContent = "text/plain; charset=utf-8"

func lazy(id, label, group, note, platform, contentType string) Target {
	return Target{ID: id, Label: label, Group: group, Note: note, Path: "/export/" + id, platform: platform, contentType: contentType}
}

func preset(id, label, note, file string) Target {
	return Target{ID: id, Label: label, Group: "core", Note: note, Path: "/sub/" + file, Preset: true, file: file}
}

// Ref: ../sub-store/backend/src/core/proxy-utils/producers/index.js
var targets = []Target{
	lazy("surge", "Surge", "client", "", "Surge", textContent),
	lazy("loon", "Loon", "client", "", "Loon", textContent),
	lazy("qx", "Quantumult X", "client", "", "QX", textContent),
	lazy("shadowrocket", "Shadowrocket", "client", "", "Shadowrocket", textContent),
	lazy("stash", "Stash", "client", "", "Stash", textContent),
	lazy("surfboard", "Surfboard", "client", "", "Surfboard", textContent),
	lazy("egern", "Egern", "client", "", "Egern", textContent),
	preset("mihomo", "Mihomo", "完整配置", "mihomo.yaml"),
	lazy("clashmeta", "Clash.Meta", "core", "仅节点", "ClashMeta", textContent),
	lazy("clash", "Clash", "core", "", "Clash", textContent),
	lazy("sing-box", "sing-box", "core", "", "sing-box", "application/json; charset=utf-8"),
	preset("v2ray", "V2Ray", "Base64", "base64.txt"),
	lazy("uri", "URI", "core", "逐行链接", "URI", textContent),
}

// Lookup finds a target by id.
func Lookup(id string) (Target, bool) {
	for _, t := range targets {
		if t.ID == id {
			return t, true
		}
	}
	return Target{}, false
}

// Enabled reports whether the built-in sub-store is configured.
func Enabled() bool {
	return config.GlobalConfig.SubStorePort != ""
}

// FetchFunc converts the current nodes into the given sub-store platform.
type FetchFunc func(ctx context.Context, platform string) ([]byte, error)

type failure struct {
	gen uint64
	msg string
}

// Cache generates and serves export files.
type Cache struct {
	dir       string
	cooldown  time.Duration
	fetch     FetchFunc
	outputDir func() string
	now       func() time.Time

	gen atomic.Uint64 // check round
	sf  singleflight.Group
	wg  sync.WaitGroup // background manual generations

	mu         sync.Mutex
	enabled    map[string]bool      // targets rebuilt after each round
	inflight   map[string]int       // conversions in progress
	failures   map[string]failure   // last conversion error
	lastManual map[string]time.Time // last successful manual generation, for cooldown
}

// New creates a cache in dir and restores targets left by a previous run;
// outputDir locates pre-generated files.
func New(dir string, fetch FetchFunc, outputDir func() string) *Cache {
	c := &Cache{
		dir:        dir,
		cooldown:   DefaultCooldown,
		fetch:      fetch,
		outputDir:  outputDir,
		now:        time.Now,
		enabled:    map[string]bool{},
		inflight:   map[string]int{},
		failures:   map[string]failure{},
		lastManual: map[string]time.Time{},
	}
	c.restore()
	return c
}

// restore keeps a previous run's targets enabled. The round counter continues
// past their files, so they show as outdated until the next round rebuilds them.
func (c *Cache) restore() {
	items, err := os.ReadDir(c.dir)
	if err != nil {
		return
	}
	for _, it := range items {
		// Leftovers from an interrupted write.
		if strings.HasPrefix(it.Name(), ".tmp-") {
			os.Remove(filepath.Join(c.dir, it.Name()))
		}
	}
	var maxGen uint64
	found := false
	for _, t := range targets {
		if t.Preset {
			continue
		}
		if e := c.latest(t.ID); e != nil {
			c.enabled[t.ID] = true
			maxGen = max(maxGen, e.gen)
			found = true
		}
	}
	if found {
		c.gen.Store(maxGen + 1)
	}
}

var (
	defaultOnce  sync.Once
	defaultCache *Cache
)

// Default returns the process-wide cache, kept in this instance's cache dir.
func Default() *Cache {
	defaultOnce.Do(func() {
		dir := filepath.Join(utils.CacheDir(), "export")
		defaultCache = New(dir, fetchFromSubStore, localOutputDir)
	})
	return defaultCache
}

// RoundComplete starts a new round once sub-store holds the new nodes and
// rebuilds enabled targets in the background.
func RoundComplete() {
	c := Default()
	c.Advance()
	go c.RegenerateEnabled()
}

// Advance moves to the next check round.
func (c *Cache) Advance() {
	c.gen.Add(1)
}

// RegenerateEnabled rebuilds enabled targets one at a time to go easy on sub-store.
func (c *Cache) RegenerateEnabled() {
	for _, t := range targets {
		c.mu.Lock()
		on := c.enabled[t.ID]
		c.mu.Unlock()
		if !on {
			continue
		}
		if err := c.run(t); err != nil {
			slog.Warn(fmt.Sprintf("自动更新导出订阅 %s 失败: %v", t.ID, err))
		}
	}
}

// Generate enables a target and converts it in the background. It returns the
// status right away; the outcome shows up in later statuses. The target is then
// rebuilt after every round until disabled.
func (c *Cache) Generate(id string) (Status, error) {
	t, ok := Lookup(id)
	if !ok {
		return Status{}, ErrUnknownTarget
	}
	if t.Preset {
		return c.status(t), ErrPreset
	}

	c.mu.Lock()
	c.enabled[id] = true
	// Already converting: that conversion produces this round's file.
	if c.inflight[id] > 0 {
		c.mu.Unlock()
		return c.status(t), nil
	}
	if last, ok := c.lastManual[id]; ok {
		if wait := c.cooldown - c.now().Sub(last); wait > 0 {
			c.mu.Unlock()
			return c.status(t), &CooldownError{Remaining: wait}
		}
	}
	c.inflight[id]++
	c.wg.Add(1)
	c.mu.Unlock()

	go func() {
		defer c.wg.Done()
		defer c.track(id, -1)
		if err := c.run(t); err != nil {
			slog.Warn(fmt.Sprintf("生成导出订阅 %s 失败: %v", id, err))
			return
		}
		c.mu.Lock()
		c.lastManual[id] = c.now()
		c.mu.Unlock()
	}()
	return c.status(t), nil
}

// Disable stops rebuilding a target and removes its files, so the public link
// returns 404 and the target is not restored after a restart.
func (c *Cache) Disable(id string) (Status, error) {
	t, ok := Lookup(id)
	if !ok {
		return Status{}, ErrUnknownTarget
	}
	if t.Preset {
		return c.status(t), ErrPreset
	}

	c.mu.Lock()
	delete(c.enabled, id)
	delete(c.failures, id)
	delete(c.lastManual, id)
	err := c.removeFiles(id)
	c.mu.Unlock()

	if err != nil {
		return c.status(t), fmt.Errorf("删除缓存文件失败: %w", err)
	}
	return c.status(t), nil
}

// run converts and stores a target. Concurrent calls in the same round share
// one conversion.
func (c *Cache) run(t Target) error {
	gen := c.gen.Load()
	key := t.ID + "@" + strconv.FormatUint(gen, 10)
	_, err, _ := c.sf.Do(key, func() (any, error) {
		c.track(t.ID, 1)
		defer c.track(t.ID, -1)

		// Detached from any request: a shared conversion must not stop when one caller leaves.
		ctx, cancel := context.WithTimeout(context.Background(), fetchTimeout)
		defer cancel()
		data, err := c.fetch(ctx, t.platform)
		var tmp string
		if err == nil {
			tmp, err = c.writeTemp(t.ID, data)
		}

		// Check enablement and publish under the lock Disable holds, so a
		// disabled target's file never becomes reachable, even briefly.
		c.mu.Lock()
		defer c.mu.Unlock()
		if !c.enabled[t.ID] {
			if tmp != "" {
				os.Remove(tmp)
			}
			return nil, nil
		}
		if err == nil {
			err = c.publish(t.ID, gen, tmp)
		}
		if err != nil {
			// Don't let an older round's failure mask a newer file.
			if e := c.latest(t.ID); e == nil || e.gen <= gen {
				c.failures[t.ID] = failure{gen: gen, msg: err.Error()}
			}
			return nil, err
		}
		if f, ok := c.failures[t.ID]; ok && f.gen <= gen {
			delete(c.failures, t.ID)
		}
		return nil, nil
	})
	return err
}

func (c *Cache) track(id string, delta int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.inflight[id] += delta
	if c.inflight[id] <= 0 {
		delete(c.inflight, id)
	}
}

type entry struct {
	path string
	gen  uint64
	nano int64
}

func (e entry) before(o entry) bool {
	if e.gen != o.gen {
		return e.gen < o.gen
	}
	return e.nano < o.nano
}

// writeTemp writes data to a private temp file in the cache dir; publish makes it visible.
func (c *Cache) writeTemp(id string, data []byte) (string, error) {
	if err := os.MkdirAll(c.dir, 0o700); err != nil {
		return "", fmt.Errorf("写入缓存文件失败: %w", err)
	}
	f, err := os.CreateTemp(c.dir, ".tmp-"+id+"-*")
	if err != nil {
		return "", fmt.Errorf("写入缓存文件失败: %w", err)
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		os.Remove(f.Name())
		return "", fmt.Errorf("写入缓存文件失败: %w", err)
	}
	if err := f.Close(); err != nil {
		os.Remove(f.Name())
		return "", fmt.Errorf("写入缓存文件失败: %w", err)
	}
	return f.Name(), nil
}

// publish renames tmp to <id>.g<round>.<unixnano> and removes older files.
// Callers hold c.mu so it can't interleave with Disable.
func (c *Cache) publish(id string, gen uint64, tmp string) error {
	cur := entry{gen: gen, nano: c.now().UnixNano()}
	cur.path = filepath.Join(c.dir, fmt.Sprintf("%s.g%d.%d", id, cur.gen, cur.nano))
	if err := os.Rename(tmp, cur.path); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("写入缓存文件失败: %w", err)
	}
	// Remove only older files; a newer round may have finished first.
	// On Windows a file being served can't be removed; the next publish retries.
	for _, e := range c.entries(id) {
		if e.before(cur) {
			os.Remove(e.path)
		}
	}
	return nil
}

// removeFiles deletes every cache file of id and returns the first failure.
func (c *Cache) removeFiles(id string) error {
	var firstErr error
	for _, e := range c.entries(id) {
		if err := os.Remove(e.path); err != nil && !errors.Is(err, fs.ErrNotExist) && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// entries lists the cache files of id, named <id>.g<round>.<unixnano>.
func (c *Cache) entries(id string) []entry {
	items, err := os.ReadDir(c.dir)
	if err != nil {
		return nil
	}
	prefix := id + ".g"
	var out []entry
	for _, it := range items {
		name := it.Name()
		if it.IsDir() || !strings.HasPrefix(name, prefix) {
			continue
		}
		genStr, nanoStr, ok := strings.Cut(strings.TrimPrefix(name, prefix), ".")
		if !ok {
			continue
		}
		gen, err1 := strconv.ParseUint(genStr, 10, 64)
		nano, err2 := strconv.ParseInt(nanoStr, 10, 64)
		if err1 != nil || err2 != nil {
			continue
		}
		out = append(out, entry{path: filepath.Join(c.dir, name), gen: gen, nano: nano})
	}
	return out
}

func (c *Cache) latest(id string) *entry {
	var best *entry
	for _, e := range c.entries(id) {
		if best == nil || best.before(e) {
			e := e
			best = &e
		}
	}
	return best
}

// Served is content for the public route; the caller closes File.
type Served struct {
	File        *os.File
	ModTime     time.Time
	ContentType string
}

// Open opens the latest generated file for the public route. It never
// triggers a conversion.
func (c *Cache) Open(id string) (*Served, error) {
	t, ok := Lookup(id)
	if !ok || t.Preset {
		return nil, ErrUnknownTarget
	}
	// The picked file may be replaced and removed concurrently; retry.
	for i := 0; i < 3; i++ {
		e := c.latest(id)
		if e == nil {
			return nil, ErrNotGenerated
		}
		f, err := os.Open(e.path)
		if err == nil {
			return &Served{File: f, ModTime: time.Unix(0, e.nano), ContentType: t.contentType}, nil
		}
		if !errors.Is(err, fs.ErrNotExist) {
			return nil, err
		}
	}
	return nil, ErrNotGenerated
}

// Status is the current state of a target.
type Status struct {
	Target
	State       string     `json:"state"`
	Enabled     bool       `json:"enabled"` // rebuilt after each round until disabled
	GeneratedAt *time.Time `json:"generatedAt,omitempty"`
	Size        int64      `json:"size,omitempty"`
	Error       string     `json:"error,omitempty"`
	Cooldown    int        `json:"cooldown,omitempty"` // seconds until manual generation is allowed
}

// Statuses returns all targets in display order.
func (c *Cache) Statuses() []Status {
	out := make([]Status, 0, len(targets))
	for _, t := range targets {
		out = append(out, c.status(t))
	}
	return out
}

func (c *Cache) status(t Target) Status {
	s := Status{Target: t, State: StateNone}
	if t.Preset {
		if dir := c.outputDir(); dir != "" {
			if fi, err := os.Stat(filepath.Join(dir, t.file)); err == nil && !fi.IsDir() {
				mod := fi.ModTime()
				s.State, s.GeneratedAt, s.Size = StatePreset, &mod, fi.Size()
			}
		}
		return s
	}

	if e := c.latest(t.ID); e != nil {
		at := time.Unix(0, e.nano)
		s.GeneratedAt = &at
		if fi, err := os.Stat(e.path); err == nil {
			s.Size = fi.Size()
		}
		s.State = StateReady
		if e.gen < c.gen.Load() {
			s.State = StateOutdated
		}
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	s.Enabled = c.enabled[t.ID]
	if f, ok := c.failures[t.ID]; ok {
		s.State, s.Error = StateFailed, f.msg
	}
	if c.inflight[t.ID] > 0 {
		s.State = StateGenerating
	}
	if last, ok := c.lastManual[t.ID]; ok {
		if wait := c.cooldown - c.now().Sub(last); wait > 0 {
			s.Cooldown = int(math.Ceil(wait.Seconds()))
		}
	}
	return s
}

func localOutputDir() string {
	saver, err := method.NewLocalSaver()
	if err != nil {
		return ""
	}
	return saver.OutputPath
}

var subStoreClient = &http.Client{Timeout: fetchTimeout}

func fetchFromSubStore(ctx context.Context, platform string) ([]byte, error) {
	u := fmt.Sprintf("%s/download/%s?target=%s", utils.SubStoreBaseURL(), utils.SubName, url.QueryEscape(platform))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	resp, err := subStoreClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("请求 sub-store 失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		slog.Debug(fmt.Sprintf("sub-store 转换 %s 失败, 状态码: %d, 响应: %s", platform, resp.StatusCode, body))
		return nil, fmt.Errorf("sub-store 返回 HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxContentSize+1))
	if err != nil {
		return nil, fmt.Errorf("读取 sub-store 响应失败: %w", err)
	}
	if len(data) > maxContentSize {
		return nil, fmt.Errorf("转换结果超过 %d MB", maxContentSize>>20)
	}
	if len(data) == 0 {
		return nil, errors.New("sub-store 返回了空内容")
	}
	return data, nil
}
