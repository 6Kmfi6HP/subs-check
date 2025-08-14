package proxy

import (
	"fmt"
	"strings"
	"sync"
	"testing"
)

func TestRename(t *testing.T) {
	// Reset counters before test
	ResetRenameCounter()

	testCases := []struct {
		input string
		want  string
	}{
		{"this is a hk node", "🇭🇰香港1"},
		{"this is a hong kong node", "🇭🇰香港2"},
		{"us node", "🇺🇸美国1"},
		{"some random name", "🌀其他1-some random name"},
		{"another us node", "🇺🇸美国2"},
		{"日本节点", "🇯🇵日本1"},
		{"jp-node", "🇯🇵日本2"},
		{"Taiwan server", "🇹🇼台湾1"},
		{"TW proxy", "🇹🇼台湾2"},
	}

	for _, tc := range testCases {
		t.Run(tc.input, func(t *testing.T) {
			got := Rename(tc.input)
			if got != tc.want {
				t.Errorf("Rename(%q) = %q; want %q", tc.input, got, tc.want)
			}
		})
	}
}

func TestResetRenameCounter(t *testing.T) {
	// Call rename a few times to make sure counters are not zero
	Rename("hk")
	Rename("us")
	Rename("jp")

	// Reset counters
	ResetRenameCounter()

	// Check if a counter is actually reset
	// We expect the count to be 1 after the reset
	got := Rename("hk")
	want := "🇭🇰香港1"
	if got != want {
		t.Errorf("Rename(\"hk\") after reset = %q; want %q", got, want)
	}
}

func TestRenameConcurrency(t *testing.T) {
	ResetRenameCounter()
	var wg sync.WaitGroup
	iterations := 100
	wg.Add(iterations)

	for i := 0; i < iterations; i++ {
		go func() {
			defer wg.Done()
			Rename("us")
		}()
	}

	wg.Wait()

	// After 100 concurrent calls for "us", the next one should be "🇺🇸美国101"
	got := Rename("us")
	want := "🇺🇸美国101"
	if got != want {
		t.Errorf("Concurrent Rename(\"us\") check failed. got %q; want %q", got, want)
	}
}

func TestAllCountryCodes(t *testing.T) {
	ResetRenameCounter()
	for code, info := range countryMap {
		t.Run(code, func(t *testing.T) {
			got := Rename(code)
			expected := fmt.Sprintf("%s%s1", info.Emoji, info.Name)
			if !strings.HasPrefix(got, expected) {
				t.Errorf("Rename(%q) = %q; want prefix %q", code, got, expected)
			}
		})
	}
}
