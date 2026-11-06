package info

import (
	"os"
	"path/filepath"
	"testing"
)

func writeTemp(t *testing.T, name, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLoadAccountsAndFormatting(t *testing.T) {
	p := writeTemp(t, "info.txt", `
# 주석 줄
asdf 1234 qwer12345:/asdf/asdf
user02	5678	nas01:/vol/home/
`)
	accts, err := LoadAccounts(p)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(accts) != 2 {
		t.Fatalf("want 2 accounts, got %d", len(accts))
	}

	a1 := accts[0]
	if a1.User != "asdf" || a1.ID != "1234" || a1.Storage != "qwer12345:/asdf/asdf" {
		t.Fatalf("unexpected a1: %+v", a1)
	}
	if got, want := a1.PasswdLine(), "asdf:x:1234:1234:rasdf:/user/asdf:/bin/csh"; got != want {
		t.Errorf("PasswdLine() = %q, want %q", got, want)
	}
	if got, want := a1.GroupLine(), "asdf:x:1234:"; got != want {
		t.Errorf("GroupLine() = %q, want %q", got, want)
	}
	if got, want := a1.AutoHomeLine(), "asdf -rw,hard,actimeo=1,proto=tcp,vers=3 qwer12345:/asdf/asdf/asdf"; got != want {
		t.Errorf("AutoHomeLine() = %q, want %q", got, want)
	}

	a2 := accts[1]
	if got, want := a2.AutoHomeLine(), "user02 -rw,hard,actimeo=1,proto=tcp,vers=3 nas01:/vol/home/user02"; got != want {
		t.Errorf("trailing slash should be normalized: got %q, want %q", got, want)
	}
}

func TestLoadAccountsRejectsInvalid(t *testing.T) {
	cases := []struct {
		name    string
		content string
	}{
		{"wrong field count", "asdf 1234\n"},
		{"extra field", "asdf 1234 qwer:/a extra\n"},
		{"non-numeric uid", "asdf abcd qwer:/a\n"},
		{"negative uid", "asdf -10 qwer:/a\n"},
		{"invalid user char", "as:df 1234 qwer:/a\n"},
		{"invalid storage", "asdf 1234 qwer_no_colon\n"},
		{"duplicate user", "asdf 1234 qwer:/a\nasdf 5678 qwer:/b\n"},
		{"empty file", "# comment only\n\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := writeTemp(t, "info.txt", tc.content)
			if _, err := LoadAccounts(p); err == nil {
				t.Fatalf("expected error for %s, got nil", tc.name)
			}
		})
	}
}

func TestLoadHosts(t *testing.T) {
	p := writeTemp(t, "hosts.txt", "testserver0001\n# comment\ntestserver0002\n")
	hosts, err := LoadHosts(p)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(hosts) != 2 || hosts[0] != "testserver0001" || hosts[1] != "testserver0002" {
		t.Fatalf("unexpected hosts: %v", hosts)
	}
}

func TestLoadHostsRejectsInvalid(t *testing.T) {
	p1 := writeTemp(t, "hosts.txt", "host1\nhost1\n")
	if _, err := LoadHosts(p1); err == nil {
		t.Fatal("expected duplicate host error")
	}
	p2 := writeTemp(t, "hosts.txt", "host1 extra\n")
	if _, err := LoadHosts(p2); err == nil {
		t.Fatal("expected extra field error")
	}
}
