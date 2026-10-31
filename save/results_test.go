package save

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/beck-8/subs-check/check"
	"github.com/beck-8/subs-check/config"
)

func withMediaCheck(t *testing.T, on bool) {
	t.Helper()
	old := config.GlobalConfig.MediaCheck
	config.GlobalConfig.MediaCheck = on
	t.Cleanup(func() { config.GlobalConfig.MediaCheck = old })
}

func detailMap(rec NodeRecord) map[string]string {
	m := map[string]string{}
	for _, d := range rec.Details {
		m[d.Key] = d.Value
	}
	return m
}

func TestNewNodeRecord_VlessReality(t *testing.T) {
	withMediaCheck(t, true)
	r := check.Result{
		Proxy: map[string]any{
			"name": "HK_1|2.0MB/s|NF-HK", "type": "vless", "server": "hk.example.net", "port": 443,
			"uuid": "secret-uuid", "tls": true, "udp": true, "servername": "www.microsoft.com",
			"flow": "xtls-rprx-vision", "client-fingerprint": "chrome", "network": "tcp",
			"reality-opts": map[string]any{"public-key": "secret-pk", "short-id": "secret-sid"},
		},
		Speed: 2048, Country: "HK", IP: "203.0.113.1", IPRisk: "12%",
	}
	parts := check.NameParts{
		Base:     "HK_1",
		SpeedTag: "2.0MB/s",
		Media:    []check.MediaTag{{Platform: "netflix", Tag: "NF-HK"}, {Platform: "iprisk", Tag: "12%"}, {Platform: "disney"}},
	}

	rec := newNodeRecord(r, parts)

	if rec.Name != "HK_1|2.0MB/s|NF-HK|12%" || rec.BaseName != "HK_1" {
		t.Errorf("name = %q, baseName = %q", rec.Name, rec.BaseName)
	}
	if rec.Type != "vless" || rec.Server != "hk.example.net" || rec.Port != "443" || rec.SNI != "www.microsoft.com" {
		t.Errorf("basic fields = %+v", rec)
	}
	if !rec.TLS || !rec.Reality || !rec.UDP || rec.Network != "tcp" {
		t.Errorf("tls/reality/udp/network = %v/%v/%v/%q", rec.TLS, rec.Reality, rec.UDP, rec.Network)
	}
	if rec.Speed != 2048 || rec.Country != "HK" || rec.IP != "203.0.113.1" || rec.IPRisk != "12%" {
		t.Errorf("check fields = %+v", rec)
	}
	// iprisk is not an unlock platform; misses stay so the page can show them.
	if len(rec.Media) != 2 || rec.Media[0].Tag != "NF-HK" || rec.Media[1].Platform != "disney" || rec.Media[1].Tag != "" {
		t.Errorf("media = %+v", rec.Media)
	}
	d := detailMap(rec)
	if d["flow"] != "xtls-rprx-vision" || d["client-fingerprint"] != "chrome" {
		t.Errorf("details = %v", d)
	}

	data, err := json.Marshal(rec)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"secret-uuid", "secret-pk", "secret-sid"} {
		if strings.Contains(string(data), secret) {
			t.Errorf("record leaks %q: %s", secret, data)
		}
	}
}

func TestNewNodeRecord_ProtocolDefaults(t *testing.T) {
	withMediaCheck(t, false)
	tests := []struct {
		name    string
		proxy   map[string]any
		tls     bool
		udp     bool
		network string
		sni     string
		details map[string]string
	}{
		{
			name:    "hysteria2 is always tls+udp over quic",
			proxy:   map[string]any{"type": "hysteria2", "server": "sg.example.com", "port": 8443, "password": "secret", "sni": "sg.example.com", "up": "50 Mbps", "alpn": []any{"h3"}},
			tls:     true,
			udp:     true,
			network: "quic",
			sni:     "sg.example.com",
			details: map[string]string{"up": "50 Mbps", "alpn": "h3"},
		},
		{
			name:    "ss without udp flag",
			proxy:   map[string]any{"type": "ss", "server": "203.0.113.8", "port": "8388", "cipher": "aes-256-gcm", "password": "secret"},
			network: "tcp",
			details: map[string]string{"cipher": "aes-256-gcm"},
		},
		{
			name:    "trojan over ws",
			proxy:   map[string]any{"type": "trojan", "server": "de.example.com", "port": 443, "password": "secret", "udp": true, "network": "ws", "skip-cert-verify": false, "ws-opts": map[string]any{"path": "/ray", "headers": map[string]any{"Host": "cdn.example.com"}}},
			tls:     true,
			udp:     true,
			network: "ws",
			details: map[string]string{"ws-path": "/ray", "ws-host": "cdn.example.com", "skip-cert-verify": "false"},
		},
		{
			name:    "wireguard without udp flag",
			proxy:   map[string]any{"type": "wireguard", "server": "203.0.113.20", "port": 51820, "private-key": "secret"},
			network: "udp",
		},
		{
			name:    "wireguard with udp flag",
			proxy:   map[string]any{"type": "wireguard", "server": "203.0.113.21", "port": 51820, "private-key": "secret", "udp": true},
			udp:     true,
			network: "udp",
		},
		{
			name:    "servername is ignored without tls",
			proxy:   map[string]any{"type": "vless", "server": "203.0.113.9", "port": 2087, "uuid": "secret", "tls": false, "udp": true, "servername": "/?--junk--", "network": "ws"},
			udp:     true,
			network: "ws",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rec := newNodeRecord(check.Result{Proxy: tc.proxy}, check.NameParts{Base: "n", Media: []check.MediaTag{{Platform: "netflix", Tag: "NF"}}})
			if rec.TLS != tc.tls || rec.UDP != tc.udp || rec.Network != tc.network || rec.SNI != tc.sni {
				t.Errorf("tls/udp/network/sni = %v/%v/%q/%q", rec.TLS, rec.UDP, rec.Network, rec.SNI)
			}
			if rec.Media != nil {
				t.Errorf("media should be empty when media check is off, got %+v", rec.Media)
			}
			d := detailMap(rec)
			for k, want := range tc.details {
				if d[k] != want {
					t.Errorf("detail %s = %q, want %q", k, d[k], want)
				}
			}
			data, _ := json.Marshal(rec)
			if strings.Contains(string(data), "secret") {
				t.Errorf("record leaks password: %s", data)
			}
		})
	}
}

func TestSaveResultsSnapshot_EmptyRoundWritesEmptyList(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "results.json")
	old := ResultsPath
	ResultsPath = func() string { return path }
	t.Cleanup(func() { ResultsPath = old })

	saveResultsSnapshot(nil)

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read snapshot: %v", err)
	}
	var snap map[string]any
	if err := json.Unmarshal(data, &snap); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	nodes, ok := snap["nodes"].([]any)
	if !ok || len(nodes) != 0 {
		t.Fatalf("nodes = %#v, want empty list", snap["nodes"])
	}
	if snap["checkedAt"] == nil {
		t.Fatal("checkedAt missing")
	}
}
