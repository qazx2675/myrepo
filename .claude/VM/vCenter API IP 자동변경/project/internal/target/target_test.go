package target

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadEntries(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "list.txt")
	content := string(rune(0xFEFF)) + "web01 10.10.5.23\n# comment\n\nweb02 10.10.5.24\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	entries, err := LoadEntries(path)
	if err != nil {
		t.Fatalf("LoadEntries: %v", err)
	}
	want := []Entry{
		{Hostname: "web01", NewIP: "10.10.5.23"},
		{Hostname: "web02", NewIP: "10.10.5.24"},
	}
	if len(entries) != len(want) {
		t.Fatalf("got %d entries, want %d", len(entries), len(want))
	}
	for i := range want {
		if entries[i] != want[i] {
			t.Errorf("entry %d = %+v, want %+v", i, entries[i], want[i])
		}
	}
}

func TestLoadEntriesInvalidIP(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "list.txt")
	if err := os.WriteFile(path, []byte("web01 not-an-ip\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadEntries(path); err == nil {
		t.Fatal("expected error for invalid IP, got nil")
	}
}

func TestGateway(t *testing.T) {
	cases := map[string]string{
		"10.10.5.23":    "10.10.5.1",
		"192.168.1.254": "192.168.1.1",
		"172.16.0.2":    "172.16.0.1",
	}
	for ip, want := range cases {
		if got := Gateway(ip); got != want {
			t.Errorf("Gateway(%q) = %q, want %q", ip, got, want)
		}
	}
}

func TestPairHostname(t *testing.T) {
	if pair, ok := PairHostname("host0001ev02"); !ok || pair != "host0001ev01" {
		t.Errorf("PairHostname(host0001ev02) = %q, %v, want host0001ev01, true", pair, ok)
	}
	if _, ok := PairHostname("host0001ev01"); ok {
		t.Error("PairHostname(host0001ev01) 은 ev02 가 아니므로 ok=false 여야 합니다")
	}
	if _, ok := PairHostname("web01"); ok {
		t.Error("PairHostname(web01) 은 ev02 가 아니므로 ok=false 여야 합니다")
	}
}

func TestBareMetalName(t *testing.T) {
	if bm, ok := BareMetalName("host0001ev02"); !ok || bm != "host0001" {
		t.Errorf("BareMetalName(host0001ev02) = %q, %v, want host0001, true", bm, ok)
	}
	if _, ok := BareMetalName("host0001ev01"); ok {
		t.Error("BareMetalName(host0001ev01) 은 ev02 가 아니므로 ok=false 여야 합니다")
	}
}

func TestSanitizeID(t *testing.T) {
	cases := map[string]string{
		"web01":     "web01",
		"web-01_a":  "web-01_a",
		"db.prod01": "db_prod01",
		"서버01":      "__01",
	}
	for in, want := range cases {
		if got := SanitizeID(in); got != want {
			t.Errorf("SanitizeID(%q) = %q, want %q", in, got, want)
		}
	}
}
