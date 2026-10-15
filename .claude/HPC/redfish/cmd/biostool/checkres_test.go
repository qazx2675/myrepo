package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func resLine(host, attr, tag, exp, cur string) ResultLine {
	return ResultLine{Host: host, Attr: attr, Tag: tag, Expected: exp, Current: cur}
}

func TestWriteCheckRes(t *testing.T) {
	lines := []ResultLine{
		resLine("hpe-node01", "A", TagOK, "1", "1"),
		resLine("hpe-node02", "CollabPowerControl", TagDiff, "Disabled", "Enabled"),
		resLine("hpe-node02", "ProcHyperthreading", TagDiff, "Enabled", "Disabled"),
		resLine("hpe-node02", "X", TagOK, "1", "1"),
		{Host: "hpe-node03", Attr: "-", Tag: TagNA, Expected: "-", Current: "-", Note: "UNREACHABLE", HostError: true},
		resLine("hpe-node04", "A", TagNA, "1", "-"),
		resLine("hpe-node04", "B", TagNA, "1", "-"),
		resLine("hpe-node04", "C", TagOK, "1", "1"),
		resLine("extra", "A", TagOK, "1", "1"),
	}
	path := filepath.Join(t.TempDir(), "check.res_root")
	order := []string{"hpe-node01", "hpe-node02", "hpe-node03", "hpe-node04", "hpe-node05"}
	if err := writeCheckRes(path, lines, order); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	want := "hpe-node01: OK\n" +
		"hpe-node02: FAIL difference 2건 (CollabPowerControl Enabled→Disabled, ProcHyperthreading Disabled→Enabled)\n" +
		"hpe-node03: FAIL N/A 접속실패(UNREACHABLE)\n" +
		"hpe-node04: OK (N/A 2건)\n" +
		"hpe-node05: FAIL N/A 결과없음\n" +
		"extra: OK\n"
	if string(b) != want {
		t.Fatalf("got:\n%s\nwant:\n%s", b, want)
	}
	if runtime.GOOS != "windows" {
		if fi, _ := os.Stat(path); fi.Mode().Perm() != 0o644 {
			t.Errorf("권한 %v", fi.Mode().Perm())
		}
	}
}

func TestCheckResCap(t *testing.T) {
	var lines []ResultLine
	for i := 1; i <= 13; i++ {
		lines = append(lines, resLine("h", fmt.Sprintf("A%d", i), TagDiff, "e", "c"))
	}
	got := checkResLine("h", lines)
	if !strings.HasPrefix(got, "h: FAIL difference 13건 (A1 c→e, A2 c→e") || !strings.HasSuffix(got, ", A10 c→e 외 3건)") {
		t.Fatalf("got %s", got)
	}
	if strings.Contains(got, "A11") {
		t.Fatalf("상한을 넘은 항목이 풀려 있다: %s", got)
	}
}

func TestCheckResOneLine(t *testing.T) {
	got := checkResLine("h", []ResultLine{resLine("h", "A", TagDiff, "e\nf", "-")})
	if strings.ContainsAny(got, "\r\n") || got != "h: FAIL difference 1건 (A (N/A)→e f)" {
		t.Fatalf("got %q", got)
	}
}
