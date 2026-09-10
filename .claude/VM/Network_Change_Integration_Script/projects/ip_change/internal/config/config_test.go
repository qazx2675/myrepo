package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadMissingFileUsesDefaults(t *testing.T) {
	cfg, err := Load(filepath.Join(t.TempDir(), "nope.conf"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.NetworkScriptsDir != DefaultNetworkScriptsDir {
		t.Fatalf("want default dir, got %q", cfg.NetworkScriptsDir)
	}
	if cfg.RHEL9Path != "" {
		t.Fatalf("want empty rhel9_path, got %q", cfg.RHEL9Path)
	}
}

func TestLoadOverrides(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "ip_change.conf")
	content := "# comment\nnetwork_scripts_dir=/etc/sysconfig/network-scripts\nrhel9_path=/etc/NM/system-connections\n"
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.NetworkScriptsDir != "/etc/sysconfig/network-scripts" {
		t.Fatalf("unexpected dir: %q", cfg.NetworkScriptsDir)
	}
	if cfg.RHEL9Path != "/etc/NM/system-connections" {
		t.Fatalf("unexpected rhel9_path: %q", cfg.RHEL9Path)
	}
}

func TestLoadRejectsUnknownKey(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "ip_change.conf")
	if err := os.WriteFile(p, []byte("bogus_key=1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(p); err == nil {
		t.Fatal("want error for unknown key")
	}
}
