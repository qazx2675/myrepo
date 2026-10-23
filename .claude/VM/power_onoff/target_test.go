package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeTarget(t *testing.T, dir, user, body string) {
	t.Helper()
	if err := os.WriteFile(TargetFilePath(dir, user), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestParseVMName(t *testing.T) {
	cases := []struct {
		in string
		bm string
		ev int
		ok bool
	}{
		{"svr1ev01", "svr1", 1, true},
		{"a-b.cev99", "a-b.c", 99, true},
		{"ev01", "", 0, false},
		{"svr1ev1", "", 0, false},
		{"svr1ev00", "", 0, false},
		{"devev01", "dev", 1, true},
		{"svr1ev001", "", 0, false},
		{"-svrev01", "", 0, false},
		{"svr 1ev01", "", 0, false},
		{"", "", 0, false},
	}
	for _, c := range cases {
		bm, ev, ok := ParseVMName(c.in)
		if bm != c.bm || ev != c.ev || ok != c.ok {
			t.Errorf("%q: got (%q,%d,%v) want (%q,%d,%v)", c.in, bm, ev, ok, c.bm, c.ev, c.ok)
		}
	}
}

func TestSortByBMEv(t *testing.T) {
	in := []string{"svr2ev01", "svr1ev10", "svr1ev02", "svr10ev01"}
	got := SortByBMEv(in)
	want := "svr1ev02,svr1ev10,svr10ev01,svr2ev01"
	if strings.Join(got, ",") != want {
		t.Errorf("got %v", got)
	}
	if in[0] != "svr2ev01" {
		t.Error("원본이 바뀌면 안 됨")
	}
}

func TestValidateUserAndPath(t *testing.T) {
	cfg := &Config{Users: []string{"lsh", "ljh"}}
	if ValidateUser(cfg, "lsh") != nil || ValidateUser(cfg, "x") == nil {
		t.Error("ValidateUser 이상")
	}
	if got := TargetFilePath("/d", "lsh"); got != "/d/on_off_lsh.txt" {
		t.Errorf("경로: %s", got)
	}
}

func TestLoadTargetsOK(t *testing.T) {
	dir := t.TempDir()
	writeTarget(t, dir, "x", "# 주석\n\n  svr2ev01  \nsvr1ev02\n\t# 또 주석\nsvr1ev01\n")
	got, err := LoadTargets(dir, "x")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(got, ",") != "svr2ev01,svr1ev02,svr1ev01" {
		t.Errorf("파일 순서 유지 실패: %v", got)
	}
}

func TestLoadTargetsErrors(t *testing.T) {
	dir := t.TempDir()
	writeTarget(t, dir, "x", "svr1ev01\nbad\nsvr1ev01\nsvr2ev00\n")
	_, err := LoadTargets(dir, "x")
	if err == nil {
		t.Fatal("오류가 나야 함")
	}
	for _, s := range []string{"2번째 줄", "3번째 줄", "4번째 줄"} {
		if !strings.Contains(err.Error(), s) {
			t.Errorf("%s 누락(모아서 보고해야 함): %v", s, err)
		}
	}
	writeTarget(t, dir, "empty", "# 없음\n\n")
	if _, err := LoadTargets(dir, "empty"); err == nil {
		t.Error("0대는 오류")
	}
	if _, err := LoadTargets(dir, "missing"); err == nil {
		t.Error("파일 없음은 오류")
	}
}

func TestLoadTargetsOnlyExactFile(t *testing.T) {
	dir := t.TempDir()
	writeTarget(t, dir, "x", "svr1ev01\n")
	// 다른 user 이름의 파일은 읽지 않는다
	if _, err := LoadTargets(dir, "y"); err == nil {
		t.Error("on_off_y.txt 가 없으면 on_off_x.txt 를 읽으면 안 됨")
	}
	if err := os.WriteFile(filepath.Join(dir, "other.txt"), []byte("svr9ev01\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, _ := LoadTargets(dir, "x")
	if len(got) != 1 || got[0] != "svr1ev01" {
		t.Errorf("got %v", got)
	}
}

func TestLoadTargetsSymlinkAndDir(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, "real.txt")
	if err := os.WriteFile(real, []byte("svr1ev01\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(real, TargetFilePath(dir, "x")); err != nil {
		t.Skip("심볼릭 링크 생성 불가")
	}
	if _, err := LoadTargets(dir, "x"); err == nil || !strings.Contains(err.Error(), "심볼릭") {
		t.Errorf("심볼릭 링크는 거부: %v", err)
	}
	if err := os.Mkdir(TargetFilePath(dir, "d"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadTargets(dir, "d"); err == nil {
		t.Error("디렉터리는 거부")
	}
}
