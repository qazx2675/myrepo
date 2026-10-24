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
