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

// BMC 에서 온 제어 문자(ESC·BEL·TAB·C1 CSI)는 공백으로 바뀐다 — 터미널 이스케이프 주입 방지.
func TestCheckResControlChars(t *testing.T) {
	got := checkResLine("h\x1b[2J", []ResultLine{resLine("h", "A\x1b]0;x\x07", TagDiff, "e\tf", "c\u009b31m")})
	if want := "h [2J: FAIL difference 1건 (A ]0;x  c 31m→e f)"; got != want {
		t.Fatalf("got %q\nwant %q", got, want)
	}
	got = checkResLine("h", []ResultLine{{Host: "h", Attr: "-", Tag: TagNA, Note: "x\x1b[31my", HostError: true}})
	if got != "h: FAIL N/A 접속실패(x [31my)" {
		t.Fatalf("got %q", got)
	}
}

// 원자적 쓰기: 대상 경로의 심볼릭 링크를 따라가지 않고(링크가 가리키는 파일은 그대로) 임시 파일도 남기지 않는다.
func TestWriteCheckResSymlinkNotFollowed(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("심볼릭 링크 권한 필요")
	}
	dir := t.TempDir()
	victim := filepath.Join(dir, "victim")
	must(t, os.WriteFile(victim, []byte("keep\n"), 0o600))
	path := filepath.Join(dir, "check.res_root")
	must(t, os.Symlink(victim, path))
	must(t, writeCheckRes(path, []ResultLine{resLine("h", "A", TagOK, "1", "1")}, nil))
	if b, _ := os.ReadFile(victim); string(b) != "keep\n" {
		t.Fatalf("링크 대상이 바뀜: %q", b)
	}
	fi, err := os.Lstat(path)
	must(t, err)
	if fi.Mode()&os.ModeSymlink != 0 || fi.Mode().Perm() != 0o644 {
		t.Fatalf("결과 파일 모드 %v (일반 파일 0644 기대)", fi.Mode())
	}
	if b, _ := os.ReadFile(path); string(b) != "h: OK\n" {
		t.Fatalf("내용 %q", b)
	}
	ents, err := os.ReadDir(dir)
	must(t, err)
	if len(ents) != 2 {
		t.Fatalf("임시 파일이 남음: %v", ents)
	}
}

// 비대화형 실행은 시작하자마자 이전 check.res_<user>·_postapply 를 지운다: 치명적 오류로 끝나도 옛 OK 줄이 남지 않는다.
func TestBiosAutoRemovesStaleCheckRes(t *testing.T) {
	e := newRTEnv(t, 3, "")
	empty := e.writeFile("empty.txt", "# 없음")
	stale := []string{filepath.Join(e.dir, "check.res_erin"), filepath.Join(e.dir, "check.res_erin_postapply")}
	for _, mode := range []string{"-auto-check", "-auto"} {
		for _, f := range stale {
			must(t, os.WriteFile(f, []byte("old: OK\n"), 0o644))
		}
		_, err := rtCapture(t, func() error {
			return cmdBios([]string{"-conf", e.conf, "-hosts", e.hosts, "-res-dir", e.dir, mode, "erin", empty})
		})
		if err == nil {
			t.Fatalf("%s: 대상 없음인데 오류가 아님", mode)
		}
		for _, f := range stale {
			if _, err := os.Stat(f); err == nil {
				t.Errorf("%s: 치명적 오류 뒤 이전 %s 이 남음", mode, filepath.Base(f))
			}
		}
	}
}
