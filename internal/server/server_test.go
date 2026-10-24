package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"

	"datacenter-viewer/internal/storage"
)

func TestServerAPI_SharedDirAndSync(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "dc_server_test_*")
	if err != nil {
		t.Fatalf("MkdirTemp failed: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	exeDir := filepath.Join(tmpDir, "exe")
	_ = os.MkdirAll(exeDir, 0o755)
	dataDir := filepath.Join(tmpDir, "data")
	st, err := storage.New(dataDir, exeDir)
	if err != nil {
		t.Fatalf("storage.New failed: %v", err)
	}

	dummyFS := fstest.MapFS{
		"index.html": &fstest.MapFile{Data: []byte("<html></html>")},
	}
	handler, tracker := New(dummyFS, st)
	ts := httptest.NewServer(handler)
	defer ts.Close()

	// 1. GET /api/health
	resp, err := http.Get(ts.URL + "/api/health")
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /api/health failed: %v", err)
	}

	// 2. GET /api/config/data-dir
	resp, err = http.Get(ts.URL + "/api/config/data-dir")
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /api/config/data-dir failed: %v", err)
	}
	var info map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		t.Fatalf("Decode /api/config/data-dir failed: %v", err)
	}
	if info["dir"] != dataDir {
		t.Errorf("expected dir %s, got %v", dataDir, info["dir"])
	}

	// 3. POST /api/save
	savePayload := map[string]any{
		"layout": map[string]any{"sites": []any{map[string]any{"siteId": "SITE-A"}}},
		"csv":    "SITE-A,012_001,X001Y001,srv01,1,1",
	}
	saveBytes, _ := json.Marshal(savePayload)
	resp, err = http.Post(ts.URL+"/api/save", "application/json", bytes.NewReader(saveBytes))
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("POST /api/save failed: %v", err)
	}

	// 4. GET /api/sync
	resp, err = http.Get(ts.URL + "/api/sync")
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /api/sync failed: %v", err)
	}
	var syncRes map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&syncRes); err != nil {
		t.Fatalf("Decode /api/sync failed: %v", err)
	}
	if syncRes["ok"] != true || syncRes["csv"] != "SITE-A,012_001,X001Y001,srv01,1,1" {
		t.Errorf("unexpected sync result: %v", syncRes)
	}

	// 5. POST /api/config/data-dir (switch to another directory)
	shareDir := filepath.Join(tmpDir, "share_dir")
	chgPayload := map[string]string{"dataDir": shareDir}
	chgBytes, _ := json.Marshal(chgPayload)
	resp, err = http.Post(ts.URL+"/api/config/data-dir", "application/json", bytes.NewReader(chgBytes))
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("POST /api/config/data-dir failed: %v", err)
	}
	if st.Dir() != shareDir {
		t.Errorf("expected new dir %s, got %s", shareDir, st.Dir())
	}

	if !tracker.IsConnected() {
		t.Errorf("expected tracker.IsConnected() true after API requests")
	}
}

func TestGuard_BlocksForeignSitesAndRebinding(t *testing.T) {
	tmpDir := t.TempDir()
	exeDir := filepath.Join(tmpDir, "exe")
	_ = os.MkdirAll(exeDir, 0o755)
	st, err := storage.New(filepath.Join(tmpDir, "data"), exeDir)
	if err != nil {
		t.Fatalf("storage.New failed: %v", err)
	}
	handler, _ := New(fstest.MapFS{"index.html": &fstest.MapFile{Data: []byte("<html></html>")}}, st)

	do := func(method, target, host, origin, ctype, fetchSite string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, target, bytes.NewReader([]byte(`{"dataDir":"x"}`)))
		req.Host = host
		if origin != "" {
			req.Header.Set("Origin", origin)
		}
		if ctype != "" {
			req.Header.Set("Content-Type", ctype)
		}
		if fetchSite != "" {
			req.Header.Set("Sec-Fetch-Site", fetchSite)
		}
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec
	}

	cases := []struct {
		name                              string
		method, path, host, origin, ctype string
		fetchSite                         string
		want                              int
	}{
		{"same-origin bootstrap", "GET", "/api/bootstrap", "127.0.0.1:5000", "", "", "same-origin", http.StatusOK},
		{"DNS rebinding host", "GET", "/api/bootstrap", "evil.example:5000", "", "", "", http.StatusForbidden},
		{"cross-site origin POST", "POST", "/api/config/data-dir", "127.0.0.1:5000", "http://evil.example", "application/json", "cross-site", http.StatusForbidden},
		{"other local port origin", "POST", "/api/config/data-dir", "127.0.0.1:5000", "http://127.0.0.1:5500", "application/json", "", http.StatusForbidden},
		{"text/plain POST (no preflight)", "POST", "/api/save", "127.0.0.1:5000", "", "text/plain", "", http.StatusUnsupportedMediaType},
		{"bye beacon without body", "POST", "/api/bye", "127.0.0.1:5000", "http://127.0.0.1:5000", "", "same-origin", http.StatusOK},
	}
	for _, c := range cases {
		rec := do(c.method, c.path, c.host, c.origin, c.ctype, c.fetchSite)
		if rec.Code != c.want {
			t.Errorf("%s: got %d, want %d", c.name, rec.Code, c.want)
		}
	}

	rec := do("GET", "/index.html", "localhost:5000", "", "", "")
	if rec.Header().Get("Content-Security-Policy") == "" || rec.Header().Get("X-Frame-Options") != "DENY" {
		t.Errorf("security headers missing: %v", rec.Header())
	}
}
