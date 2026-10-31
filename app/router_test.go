package app

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/beck-8/subs-check/config"
	"github.com/beck-8/subs-check/save"
	"github.com/gin-gonic/gin"
)

// newTestRouter registers all routes with temp dirs and a fixed API key.
// Registration itself is a check: gin panics on conflicting routes.
func newTestRouter(t *testing.T) *gin.Engine {
	t.Helper()
	oldCfg := *config.GlobalConfig
	oldResults := save.ResultsPath
	t.Cleanup(func() {
		*config.GlobalConfig = oldCfg
		save.ResultsPath = oldResults
	})
	config.GlobalConfig.EnableWebUI = true
	config.GlobalConfig.APIKey = "test-key"
	config.GlobalConfig.SubStorePort = ""
	config.GlobalConfig.OutputDir = t.TempDir()
	resultsFile := filepath.Join(t.TempDir(), "results.json")
	save.ResultsPath = func() string { return resultsFile }

	gin.SetMode(gin.TestMode)
	router, err := (&App{}).newRouter()
	if err != nil {
		t.Fatalf("newRouter: %v", err)
	}
	return router
}

func doRequest(router http.Handler, method, path string, withKey bool) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, nil)
	if withKey {
		req.Header.Set("X-API-Key", "test-key")
	}
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

func TestRouter_ResultsPage(t *testing.T) {
	router := newTestRouter(t)
	w := doRequest(router, http.MethodGet, "/admin/results", false)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "导出订阅") {
		t.Fatal("results page did not render")
	}
}

func TestRouter_ResultsAPI(t *testing.T) {
	router := newTestRouter(t)

	if w := doRequest(router, http.MethodGet, "/api/results", false); w.Code != http.StatusUnauthorized {
		t.Fatalf("without key: status = %d, want 401", w.Code)
	}

	w := doRequest(router, http.MethodGet, "/api/results", true)
	if w.Code != http.StatusOK || strings.TrimSpace(w.Body.String()) != `{"nodes":[]}` {
		t.Fatalf("no snapshot: status = %d, body = %s", w.Code, w.Body.String())
	}

	snapshot := `{"checkedAt":"2026-09-15T14:32:08Z","speedTest":true,"mediaCheck":false,"nodes":[]}`
	if err := os.WriteFile(save.ResultsPath(), []byte(snapshot), 0o600); err != nil {
		t.Fatal(err)
	}
	w = doRequest(router, http.MethodGet, "/api/results", true)
	if w.Code != http.StatusOK || w.Body.String() != snapshot {
		t.Fatalf("with snapshot: status = %d, body = %s", w.Code, w.Body.String())
	}
}

func TestRouter_ExportRoutes(t *testing.T) {
	router := newTestRouter(t)
	tests := []struct {
		method string
		path   string
		key    bool
		want   int
	}{
		{http.MethodGet, "/export/unknown", false, http.StatusNotFound},
		// Preset targets are only served under /sub/.
		{http.MethodGet, "/export/mihomo", false, http.StatusNotFound},
		{http.MethodGet, "/api/export", false, http.StatusUnauthorized},
		{http.MethodPost, "/api/export/surge", false, http.StatusUnauthorized},
		{http.MethodPost, "/api/export/unknown", true, http.StatusNotFound},
		{http.MethodPost, "/api/export/mihomo", true, http.StatusBadRequest},
		{http.MethodDelete, "/api/export/surge", false, http.StatusUnauthorized},
		{http.MethodDelete, "/api/export/unknown", true, http.StatusNotFound},
		{http.MethodDelete, "/api/export/mihomo", true, http.StatusBadRequest},
		// Generation requires sub-store.
		{http.MethodPost, "/api/export/surge", true, http.StatusServiceUnavailable},
	}
	for _, tc := range tests {
		w := doRequest(router, tc.method, tc.path, tc.key)
		if w.Code != tc.want {
			t.Errorf("%s %s (key=%v): status = %d, want %d, body = %s", tc.method, tc.path, tc.key, w.Code, tc.want, w.Body.String())
		}
	}
}
