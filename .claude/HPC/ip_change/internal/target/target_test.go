package target

import (
	"os"
	"path/filepath"
	"testing"
)

func write(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "user.txt")
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLoad(t *testing.T) {
	p := write(t, "hostname1 3.3.3.3\nhostname2 2.2.2.2\n\n# comment\n")
	entries, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("want 2 entries, got %d", len(entries))
	}
	if entries[0].Host != "hostname1" || entries[0].NewIP != "3.3.3.3" {
		t.Fatalf("unexpected entry[0]: %+v", entries[0])
	}
	if entries[1].Host != "hostname2" || entries[1].NewIP != "2.2.2.2" {
		t.Fatalf("unexpected entry[1]: %+v", entries[1])
	}
}

func TestLoadRejectsBadFormat(t *testing.T) {
	p := write(t, "hostname1 3.3.3.3 extra\n")
	if _, err := Load(p); err == nil {
		t.Fatal("want error for extra field")
	}
}

func TestLoadRejectsBadIP(t *testing.T) {
	p := write(t, "hostname1 not-an-ip\n")
	if _, err := Load(p); err == nil {
		t.Fatal("want error for invalid IP")
	}
}

func TestLoadRejectsDuplicateHost(t *testing.T) {
	p := write(t, "hostname1 3.3.3.3\nhostname1 4.4.4.4\n")
	if _, err := Load(p); err == nil {
		t.Fatal("want error for duplicate host")
	}
}

func TestLoadRejectsEmpty(t *testing.T) {
	p := write(t, "# only comments\n\n")
	if _, err := Load(p); err == nil {
		t.Fatal("want error for empty target list")
	}
}

func TestGateway(t *testing.T) {
	cases := map[string]string{
		"3.3.3.3":    "3.3.3.1",
		"2.2.2.2":    "2.2.2.1",
		"10.0.5.200": "10.0.5.1",
	}
	for ip, want := range cases {
		got, err := Gateway(ip)
		if err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Errorf("Gateway(%q) = %q, want %q", ip, got, want)
		}
	}
}
