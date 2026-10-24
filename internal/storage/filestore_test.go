package storage

import (
	"os"
	"path/filepath"
	"testing"
)

func TestStore_SaveLoadAndSetDir(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "dc_storage_test_*")
	if err != nil {
		t.Fatalf("MkdirTemp failed: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	exeDir := filepath.Join(tmpDir, "exe")
	dataDir1 := filepath.Join(tmpDir, "data1")
	dataDir2 := filepath.Join(tmpDir, "data2")

	st, err := New(dataDir1, exeDir)
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}

	layout1 := []byte(`{"sites":[{"siteId":"SITE-A"}]}`)
	csv1 := "SITE-A,012_001,X001Y001,srv01,1,1"

	if err := st.Save(layout1, csv1); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	loadedLayout, loadedCsv, err := st.Load()
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if string(loadedLayout) != string(layout1) {
		t.Errorf("expected layout %s, got %s", layout1, loadedLayout)
	}
	if loadedCsv != csv1 {
		t.Errorf("expected csv %s, got %s", csv1, loadedCsv)
	}

	// Test SetDir to second directory (simulating switching to network share)
	if err := st.SetDir(dataDir2); err != nil {
		t.Fatalf("SetDir failed: %v", err)
	}
	if st.Dir() != dataDir2 {
		t.Errorf("expected Dir() %s, got %s", dataDir2, st.Dir())
	}

	layout2 := []byte(`{"sites":[{"siteId":"SITE-B"}]}`)
	csv2 := "SITE-B,012_002,X002Y002,srv02,2,10"
	if err := st.Save(layout2, csv2); err != nil {
		t.Fatalf("Save in dir2 failed: %v", err)
	}

	// Verify persistence in config file
	cfgPath := LoadConfigPath(exeDir)
	if cfgPath != dataDir2 {
		t.Errorf("expected LoadConfigPath %s, got %s", dataDir2, cfgPath)
	}

	info := st.Info()
	if info["dir"] != dataDir2 {
		t.Errorf("expected info.dir %s, got %v", dataDir2, info["dir"])
	}
	if info["hasLayout"] != true || info["hasCsv"] != true {
		t.Errorf("expected both hasLayout and hasCsv true, got %v", info)
	}
}
