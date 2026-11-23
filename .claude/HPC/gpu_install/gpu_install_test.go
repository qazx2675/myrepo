package main

import (
	"bufio"
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestParseArgs(t *testing.T) {
	c, err := ParseArgs([]string{"/a"})
	if err != nil || c.AutofsPath != "/a" || c.ListFile != "./list" || c.Parallel != 10 {
		t.Fatalf("기본값: %+v %v", c, err)
	}
	c, err = ParseArgs([]string{"-p", "3", "/a", "l.txt", "-mount-user", "u1", "-gossh", "/x/gossh"})
	if err != nil || c.Parallel != 3 || c.ListFile != "l.txt" || c.MountUser != "u1" || c.Gossh != "/x/gossh" {
		t.Fatalf("전체: %+v %v", c, err)
	}
	for _, bad := range [][]string{{}, {"/a", "-p"}, {"/a", "-p", "0"}, {"/a", "-p", "x"}, {"/a", "-z"}, {"/a", "b", "c"}} {
		if _, err := ParseArgs(bad); err == nil {
			t.Errorf("오류여야 함: %v", bad)
		}
	}
}

func TestParseList(t *testing.T) {
	es, err := ParseList(strings.NewReader("# 머리말\n\ngpu01   # 주석\n  gpu02\t570.172.08\n"))
	want := []Entry{{"gpu01", ""}, {"gpu02", "570.172.08"}}
	if err != nil || !reflect.DeepEqual(es, want) {
		t.Fatalf("%v %v", es, err)
	}
	for _, bad := range []string{"a\nb\na 550.1\n", "a 1 2\n", "a ../x\n", "# 없음\n\n"} {
		if _, err := ParseList(strings.NewReader(bad)); err == nil {
			t.Errorf("오류여야 함: %q", bad)
		}
	}
}

func mkRuns(t *testing.T, vs ...string) string {
	d := t.TempDir()
	for _, v := range vs {
		if err := os.WriteFile(RunFile(d, v), []byte("x"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return d
}

func TestRunFileAndList(t *testing.T) {
	d := mkRuns(t, "580.65.06", "550.54.14")
	os.WriteFile(filepath.Join(d, "other.run"), nil, 0o644)
	if got := RunFile("/d", "550.1"); filepath.ToSlash(got) != "/d/NVIDIA-Linux-x86_64-550.1.run" {
		t.Errorf("RunFile %s", got)
	}
	vs, err := ListVersions(d)
	if err != nil || !reflect.DeepEqual(vs, []string{"550.54.14", "580.65.06"}) {
		t.Errorf("%v %v", vs, err)
	}
}

func TestCheckRuns(t *testing.T) {
	d := mkRuns(t, "550.54.14")
	if err := CheckRuns(d, []Entry{{"a", "550.54.14"}, {"b", ""}}); err != nil {
		t.Error(err)
	}
	err := CheckRuns(d, []Entry{{"a", "470.1"}, {"b", "550.54.14"}})
	if err == nil || !strings.Contains(err.Error(), "470.1") {
		t.Errorf("누락 오류 기대: %v", err)
	}
}

func TestResolveVersions(t *testing.T) {
	d := mkRuns(t, "550.54.14", "580.65.06")
	// 버전 없는 줄 2개 → 한 번만 질문(잘못된 입력 1회 뒤 2번)
	es := []Entry{{"a", ""}, {"b", "550.54.14"}, {"c", ""}}
	var out bytes.Buffer
	if err := ResolveVersions(d, es, bufio.NewReader(strings.NewReader("9\n2\n")), &out); err != nil {
		t.Fatal(err)
	}
	if es[0].Version != "580.65.06" || es[2].Version != "580.65.06" || es[1].Version != "550.54.14" {
		t.Errorf("%+v", es)
	}
	if strings.Count(out.String(), "드라이버를 고르세요") != 1 || !strings.Contains(out.String(), "1) 550.54.14") {
		t.Errorf("출력: %s", out.String())
	}
	// 모두 버전 있음 → 질문 없음
	out.Reset()
	es = []Entry{{"a", "550.54.14"}}
	if err := ResolveVersions(d, es, bufio.NewReader(strings.NewReader("")), &out); err != nil || out.Len() != 0 {
		t.Errorf("질문하면 안 됨: %v %q", err, out.String())
	}
	// 입력 끝 → 오류
	es = []Entry{{"a", ""}}
	if err := ResolveVersions(d, es, bufio.NewReader(strings.NewReader("")), &out); err == nil {
		t.Error("EOF 오류 기대")
	}
	// .run 없는 버전 → 시작 전 오류
	if err := ResolveVersions(d, []Entry{{"a", "1.2"}}, bufio.NewReader(strings.NewReader("")), &out); err == nil {
		t.Error("누락 오류 기대")
	}
}
