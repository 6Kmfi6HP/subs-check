package check

import (
	"strings"
	"testing"

	"github.com/beck-8/subs-check/config"
)

func TestUpdateProxyName(t *testing.T) {
	// Mock config
	originalConfig := config.GlobalConfig
	defer func() {
		config.GlobalConfig = originalConfig
	}()

	testCases := []struct {
		name           string
		initialProxy   map[string]any
		result         *Result
		config         *config.Config
		expectedName   string
		speed          int
		httpClient     *ProxyClient
	}{
		{
			name:         "Simple case with only speed",
			initialProxy: map[string]any{"name": "test-proxy"},
			result:       &Result{},
			config: &config.Config{
				SpeedTestUrl: "http://example.com",
				MediaCheck:   false,
				Platforms:    []string{},
			},
			speed:        1234, // 1.2MB/s
			expectedName: "test-proxy|1.2MB/s",
		},
		{
			name:         "Speed in KB",
			initialProxy: map[string]any{"name": "test-proxy"},
			result:       &Result{},
			config: &config.Config{
				SpeedTestUrl: "http://example.com",
				MediaCheck:   false,
				Platforms:    []string{},
			},
			speed:        512, // 512KB/s
			expectedName: "test-proxy|512KB/s",
		},
		{
			name:         "With media checks",
			initialProxy: map[string]any{"name": "media-node"},
			result: &Result{
				Netflix: true,
				Disney:  true,
				Openai:  true,
				Gemini:  true,
				Youtube: "US",
				TikTok: "GB",
			},
			config: &config.Config{
				MediaCheck: true,
				Platforms:  []string{"netflix", "disney", "openai", "gemini", "youtube", "tiktok"},
			},
			speed:        0,
			expectedName: "media-node|NF|D+|GPT⁺|GM|YT-US|TK-GB",
		},
		{
			name:         "With partial media checks and specific order",
			initialProxy: map[string]any{"name": "partial-media"},
			result: &Result{
				Netflix:   true,
				OpenaiWeb: true,
				Youtube:   "JP",
			},
			config: &config.Config{
				MediaCheck: true,
				Platforms:  []string{"youtube", "openai", "netflix"}, // Custom order
			},
			speed:        0,
			expectedName: "partial-media|YT-JP|GPT|NF",
		},
		{
			name:         "With IP Risk and ASN",
			initialProxy: map[string]any{"name": "security-node"},
			result: &Result{
				IPRisk: "80%",
				ASN:    "12345",
			},
			config: &config.Config{
				MediaCheck: true,
				Platforms:  []string{"iprisk", "asn"},
			},
			speed:        0,
			expectedName: "security-node|80%|AS12345",
		},
		{
			name:         "All features combined",
			initialProxy: map[string]any{"name": "full-node"},
			result: &Result{
				Netflix: true,
				Openai:  true,
				IPRisk:  "10%",
				ASN:     "54321",
				Youtube: "KR",
			},
			config: &config.Config{
				SpeedTestUrl: "http://example.com",
				MediaCheck:   true,
				Platforms:    []string{"netflix", "openai", "iprisk", "asn", "youtube"},
			},
			speed:        2048,
			expectedName: "full-node|2.0MB/s|NF|GPT⁺|10%|AS54321|YT-KR",
		},
		{
			name:         "Removes old tags",
			initialProxy: map[string]any{"name": "old-node|NF|1.5MB/s|AS1111"},
			result: &Result{
				Disney: true,
				ASN:    "22222",
			},
			config: &config.Config{
				SpeedTestUrl: "http://example.com",
				MediaCheck:   true,
				Platforms:    []string{"disney", "asn"},
			},
			speed:        800,
			expectedName: "old-node|800KB/s|D+|AS22222",
		},
		{
			name:         "No checks enabled",
			initialProxy: map[string]any{"name": "plain-node"},
			result: &Result{
				Netflix: true,
			},
			config: &config.Config{
				SpeedTestUrl: "",
				MediaCheck:   false,
			},
			speed:        0,
			expectedName: "plain-node",
		},
		{
			name: "With sub tag",
			initialProxy: map[string]any{"name": "tagged-node", "sub_tag": "MyTag"},
			result: &Result{
				Netflix: true,
			},
			config: &config.Config{
				MediaCheck: true,
				Platforms: []string{"netflix"},
			},
			speed: 0,
			expectedName: "tagged-node|NF|MyTag",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			config.GlobalConfig = tc.config
			// The function modifies the proxy map in place, so we create a copy for the result
			resultWithProxy := *tc.result
			resultWithProxy.Proxy = tc.initialProxy

			pc := &ProxyChecker{}
			pc.updateProxyName(&resultWithProxy, tc.httpClient, tc.speed)

			got := resultWithProxy.Proxy["name"].(string)
			if got != tc.expectedName {
				t.Errorf("updateProxyName() got name %q, want %q", got, tc.expectedName)
			}

			// Also test that old tags are truly gone
			if strings.Contains(got, "AS1111") && tc.name == "Removes old tags" {
				t.Error("updateProxyName() did not remove old ASN tag")
			}
			if strings.Contains(got, "|NF") && tc.name == "Removes old tags" {
				t.Error("updateProxyName() did not remove old NF tag")
			}
		})
	}
}
