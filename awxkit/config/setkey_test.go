package config

import (
	"os"
	"path/filepath"
	"testing"
)

func writeTemp(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "u_setting.conf")
	if err := os.WriteFile(p, []byte(content), 0o640); err != nil {
		t.Fatal(err)
	}
	return p
}

func readFile(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestSetKeyReplace(t *testing.T) {
	p := writeTemp(t, "# 주석\nawx_url = http://x\ns2_inventory_source = 12\ns2_inventory = 3\n")
	if err := SetKey(p, "s2_inventory_source", "34"); err != nil {
		t.Fatal(err)
	}
	want := "# 주석\nawx_url = http://x\ns2_inventory_source = 34\ns2_inventory = 3\n"
	if got := readFile(t, p); got != want {
		t.Fatalf("got %q want %q", got, want)
	}
	cfg, err := Load(p)
	if err != nil || cfg.S2InventorySource != "34" || cfg.S2Inventory != "3" {
		t.Fatalf("Load 결과 이상: %+v %v", cfg, err)
	}
}

func TestSetKeyEmptyValueAndTrailingComment(t *testing.T) {
	p := writeTemp(t, "s2_inventory_source =\nfoo=1   # 메모 # 둘\ns2_inventory_source_x = 9\n")
	if err := SetKey(p, "s2_inventory_source", "7"); err != nil {
		t.Fatal(err)
	}
	want := "s2_inventory_source = 7\nfoo=1   # 메모 # 둘\ns2_inventory_source_x = 9\n"
	if got := readFile(t, p); got != want {
		t.Fatalf("got %q want %q", got, want)
	}

	p2 := writeTemp(t, "a=1\ns2_inventory_source = 12   # AWX 소스\nb=2\n")
	if err := SetKey(p2, "s2_inventory_source", "345"); err != nil {
		t.Fatal(err)
	}
	want = "a=1\ns2_inventory_source = 345   # AWX 소스\nb=2\n"
	if got := readFile(t, p2); got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestSetKeyCRLF(t *testing.T) {
	p := writeTemp(t, "a = 1\r\ns2_inventory_source = 12\r\n# c\r\nb = 2\r\n")
	if err := SetKey(p, "s2_inventory_source", "99"); err != nil {
		t.Fatal(err)
	}
	want := "a = 1\r\ns2_inventory_source = 99\r\n# c\r\nb = 2\r\n"
	if got := readFile(t, p); got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestSetKeyAppendWhenMissing(t *testing.T) {
	p := writeTemp(t, "a = 1\r\nb = 2")
	if err := SetKey(p, "s2_inventory_source", "5"); err != nil {
		t.Fatal(err)
	}
	want := "a = 1\r\nb = 2\r\ns2_inventory_source = 5\r\n"
	if got := readFile(t, p); got != want {
		t.Fatalf("got %q want %q", got, want)
	}

	p2 := writeTemp(t, "a = 1\n")
	if err := SetKey(p2, "s2_inventory_source", "5"); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, p2); got != "a = 1\ns2_inventory_source = 5\n" {
		t.Fatalf("got %q", got)
	}
}

func TestSetKeyNoLeftoverTempAndMissingFile(t *testing.T) {
	p := writeTemp(t, "a = 1\n")
	if err := SetKey(p, "a", "2"); err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(filepath.Dir(p))
	if len(entries) != 1 {
		t.Fatalf("임시 파일이 남았습니다: %v", entries)
	}
	if err := SetKey(filepath.Join(t.TempDir(), "none.conf"), "a", "1"); err == nil {
		t.Fatal("없는 파일은 오류여야 합니다")
	}
}
