package save

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
	"time"

	"github.com/beck-8/subs-check/check"
	"github.com/beck-8/subs-check/config"
	"github.com/beck-8/subs-check/utils"
)

// ResultsPath returns where the snapshot for /admin/results is stored: this
// instance's cache dir, not the output dir that /sub/ serves publicly.
// Tests may replace it.
var ResultsPath = func() string {
	return filepath.Join(utils.CacheDir(), "results.json")
}

// ResultsSnapshot is the content of results.json.
type ResultsSnapshot struct {
	CheckedAt  time.Time    `json:"checkedAt"`
	SpeedTest  bool         `json:"speedTest"`
	MediaCheck bool         `json:"mediaCheck"`
	Nodes      []NodeRecord `json:"nodes"`
}

// NodeRecord is one node on the results page.
// Fields are whitelisted so credentials (password, uuid, private-key...) never get in.
type NodeRecord struct {
	Name     string           `json:"name"`     // display name, same as all.yaml
	BaseName string           `json:"baseName"` // name without speed/media tags
	Type     string           `json:"type"`
	Server   string           `json:"server"`
	Port     string           `json:"port"`
	SNI      string           `json:"sni,omitempty"`
	TLS      bool             `json:"tls"`
	Reality  bool             `json:"reality,omitempty"`
	UDP      bool             `json:"udp"`
	Network  string           `json:"network"`
	Speed    int              `json:"speed"` // KB/s, 0 if not tested
	Country  string           `json:"country,omitempty"`
	IP       string           `json:"ip,omitempty"`
	IPRisk   string           `json:"ipRisk,omitempty"`
	Media    []check.MediaTag `json:"media,omitempty"` // per platform in config order, when media check is on
	SubTag   string           `json:"subTag,omitempty"`
	Details  []NodeDetail     `json:"details,omitempty"`
}

// NodeDetail is one extra proxy parameter shown in the detail panel.
type NodeDetail struct {
	Key   string `json:"k"`
	Value string `json:"v"`
}

// detailFields whitelists detail parameters: key for the UI, path in the mihomo proxy map.
var detailFields = []struct {
	key  string
	path []string
}{
	{"cipher", []string{"cipher"}},
	{"flow", []string{"flow"}},
	{"client-fingerprint", []string{"client-fingerprint"}},
	{"alpn", []string{"alpn"}},
	{"skip-cert-verify", []string{"skip-cert-verify"}},
	{"ws-path", []string{"ws-opts", "path"}},
	{"ws-host", []string{"ws-opts", "headers", "Host"}},
	{"grpc-service-name", []string{"grpc-opts", "grpc-service-name"}},
	{"h2-path", []string{"h2-opts", "path"}},
	{"http-path", []string{"http-opts", "path"}},
	{"plugin", []string{"plugin"}},
	{"obfs", []string{"obfs"}},
	{"protocol", []string{"protocol"}},
	{"up", []string{"up"}},
	{"down", []string{"down"}},
	{"ports", []string{"ports"}},
	{"congestion-controller", []string{"congestion-controller"}},
	{"udp-relay-mode", []string{"udp-relay-mode"}},
	{"version", []string{"version"}},
}

// newNodeRecord builds a results-page record from a check result and its rendered name.
func newNodeRecord(r check.Result, parts check.NameParts) NodeRecord {
	p := r.Proxy
	typ := proxyString(p, "type")
	rec := NodeRecord{
		Name:     parts.String(),
		BaseName: parts.Base,
		Type:     typ,
		Server:   proxyString(p, "server"),
		Port:     proxyString(p, "port"),
		Network:  proxyString(p, "network"),
		Speed:    r.Speed,
		Country:  r.Country,
		IP:       r.IP,
		IPRisk:   r.IPRisk,
		SubTag:   parts.SubTag,
	}
	if v, ok := lookup(p, "reality-opts"); ok {
		if m, ok := v.(map[string]any); ok && len(m) > 0 {
			rec.Reality = true
		}
	}

	switch typ {
	case "trojan", "hysteria", "hysteria2", "tuic", "anytls":
		rec.TLS = true
	default:
		rec.TLS = proxyBool(p, "tls") || rec.Reality
	}
	// SNI only applies with TLS; non-TLS nodes often carry junk in servername.
	if rec.TLS {
		rec.SNI = proxyString(p, "servername")
		if rec.SNI == "" {
			rec.SNI = proxyString(p, "sni")
		}
	}
	// Only these always relay UDP; WireGuard follows its udp flag like the rest.
	switch typ {
	case "hysteria", "hysteria2", "tuic":
		rec.UDP = true
	default:
		rec.UDP = proxyBool(p, "udp")
	}
	if rec.Network == "" {
		switch typ {
		case "hysteria", "hysteria2", "tuic":
			rec.Network = "quic"
		case "wireguard":
			rec.Network = "udp"
		default:
			rec.Network = "tcp"
		}
	}

	if config.GlobalConfig.MediaCheck {
		for _, m := range parts.Media {
			// IP risk is not an unlock result; it lives in IPRisk.
			if m.Platform != "iprisk" {
				rec.Media = append(rec.Media, m)
			}
		}
	}

	for _, f := range detailFields {
		if v, ok := lookup(p, f.path...); ok {
			if s := formatValue(v); s != "" {
				rec.Details = append(rec.Details, NodeDetail{Key: f.key, Value: s})
			}
		}
	}
	return rec
}

// saveResultsSnapshot writes the round snapshot, even when empty, so the page
// never shows an older round.
func saveResultsSnapshot(nodes []NodeRecord) {
	if nodes == nil {
		nodes = []NodeRecord{}
	}
	data, err := json.Marshal(ResultsSnapshot{
		CheckedAt:  time.Now(),
		SpeedTest:  config.GlobalConfig.SpeedTestUrl != "",
		MediaCheck: config.GlobalConfig.MediaCheck,
		Nodes:      nodes,
	})
	if err != nil {
		slog.Error(fmt.Sprintf("序列化检测结果快照失败: %v", err))
		return
	}
	if err := utils.WriteFileAtomic(ResultsPath(), data); err != nil {
		slog.Error(fmt.Sprintf("保存检测结果快照失败: %v", err))
	}
}

func lookup(m map[string]any, path ...string) (any, bool) {
	var cur any = m
	for _, k := range path {
		mm, ok := cur.(map[string]any)
		if !ok {
			return nil, false
		}
		if cur, ok = mm[k]; !ok {
			return nil, false
		}
	}
	return cur, cur != nil
}

func proxyString(m map[string]any, key string) string {
	v, _ := lookup(m, key)
	return formatValue(v)
}

func proxyBool(m map[string]any, key string) bool {
	v, _ := lookup(m, key)
	b, _ := v.(bool)
	return b
}

func formatValue(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return strings.TrimSpace(x)
	case []string:
		return strings.Join(x, ", ")
	case []any:
		parts := make([]string, 0, len(x))
		for _, e := range x {
			if s := formatValue(e); s != "" {
				parts = append(parts, s)
			}
		}
		return strings.Join(parts, ", ")
	case map[string]any:
		// Nested objects are skipped; needed sub-fields are listed in detailFields.
		return ""
	default:
		return fmt.Sprint(x)
	}
}
