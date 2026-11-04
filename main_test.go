package main

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestParseHostLines(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want []string
	}{
		{"기본", "a\nb\n", []string{"a", "b"}},
		{"주석·빈줄·공백", "  # c\n\n  a  \n#b\n", []string{"a"}},
		{"쉼표·공백 혼용", "a,b c ,d\te", []string{"a", "b", "c", "d", "e"}},
		{"범위", "n[1-3]", []string{"n1", "n2", "n3"}},
		{"0 패딩", "n[01-03]", []string{"n01", "n02", "n03"}},
		{"쉼표는 먼저 분리됨(gossh 동일)", "a[1,3,5-7]", []string{"a[1", "3", "5-7]"}},
		{"범위 안 단일값", "a[1-2]b", []string{"a1b", "a2b"}},
		{"역순 범위", "n[3-1]", []string{"n1", "n2", "n3"}},
		{"괄호 여러 개", "h[01-02]ev[1-2]", []string{"h01ev1", "h01ev2", "h02ev1", "h02ev2"}},
		{"중복 제거(첫 순서)", "b\na\nb\nn[1-2]\nn1", []string{"b", "a", "n1", "n2"}},
		{"CRLF", "a\r\nb\r\n", []string{"a", "b"}},
	}
	for _, c := range cases {
		if got := parseHostLines(c.in); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: got %v want %v", c.name, got, c.want)
		}
	}
}

func TestParseConf(t *testing.T) {
	c := parseConf("# 주석\nGOSSH = /opt/gossh \nDISK_PATTERN=Disk \"\" given\nDISK_PATTERN=second=one\nERROR_PATTERN=\nERROR_PATTERN=fatal error\n\nBOGUS\n")
	if c.Gossh != "/opt/gossh" {
		t.Errorf("GOSSH=%q", c.Gossh)
	}
	if want := []string{`Disk "" given`, "second=one"}; !reflect.DeepEqual(c.DiskPatterns, want) {
		t.Errorf("disk=%v", c.DiskPatterns)
	}
	if want := []string{"fatal error"}; !reflect.DeepEqual(c.ErrPatterns, want) {
		t.Errorf("err=%v", c.ErrPatterns)
	}
	d := parseConf("")
	if len(d.DiskPatterns) != 1 || d.DiskPatterns[0] != defaultDisk || len(d.ErrPatterns) != 0 {
		t.Errorf("기본값 오류: %+v", d)
	}
}

func TestShQuote(t *testing.T) {
	cases := map[string]string{
		"abc":    "'abc'",
		"it's":   `'it'\''s'`,
		`a "" b`: `'a "" b'`,
		"$x `y`": "'$x `y`'",
		"":       "''",
	}
	for in, want := range cases {
		if got := shQuote(in); got != want {
			t.Errorf("shQuote(%q)=%q want %q", in, got, want)
		}
	}
}

func TestBuildRemoteCmd(t *testing.T) {
	cmd := buildRemoteCmd([]string{"d1", "it's"}, nil)
	if !strings.Contains(cmd, `grep -qF -e 'd1' -e 'it'\''s' /tmp/anaconda.log && echo NS_DISK`) {
		t.Errorf("디스크 줄 오류: %s", cmd)
	}
	if strings.Contains(cmd, "NS_ERR") {
		t.Errorf("ERROR 패턴이 없으면 NS_ERR 줄이 없어야 함: %s", cmd)
	}
	if !strings.Contains(cmd, "NS_NOLOG") {
		t.Errorf("NS_NOLOG 누락: %s", cmd)
	}
	cmd = buildRemoteCmd([]string{"d"}, []string{"e1", "e2"})
	if !strings.Contains(cmd, `grep -qF -e 'e1' -e 'e2' /tmp/anaconda.log && echo NS_ERR`) {
		t.Errorf("ERROR 줄 오류: %s", cmd)
	}
	// 기본 명령은 gossh 위험어 검사를 통과해야 한다
	if isDangerousCommand(buildRemoteCmd([]string{defaultDisk}, []string{"fatal error"})) {
		t.Errorf("기본 명령이 위험어로 판정됨")
	}
}

func TestDanger(t *testing.T) {
	for _, p := range []string{"reboot now", "System SHUTDOWN", "poweroff", "halt", "init 0", "init 6", "xddcx"} {
		if len(dangerousPatterns([]string{p})) != 1 {
			t.Errorf("%q 는 거부되어야 함", p)
		}
	}
	for _, p := range []string{`Disk "" given in clearpart command does not exist`, "fatal error", "No space left"} {
		if len(dangerousPatterns([]string{p})) != 0 {
			t.Errorf("%q 는 허용되어야 함", p)
		}
	}
}

func TestParseRetryOutput(t *testing.T) {
	out := "n1: NS_DISK\nn2: NS_NOLOG\nn3: Welcome banner\nn4: NS_ERR\nn4: NS_DISK\r\nnoise\n: NS_ERR\n"
	got := parseRetryOutput(out)
	want := map[string]map[string]bool{
		"n1": {mkDisk: true},
		"n2": {mkNoLog: true},
		"n4": {mkErr: true, mkDisk: true},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v want %v", got, want)
	}
}

func TestRetryStatusPriority(t *testing.T) {
	cases := []struct {
		m    map[string]bool
		want string
	}{
		{map[string]bool{mkDisk: true, mkErr: true}, stNoDisk},
		{map[string]bool{mkErr: true}, stStopped},
		{map[string]bool{mkNoLog: true}, stInstall},
		{map[string]bool{}, stInstall},
		{nil, stInstall},
	}
	for _, c := range cases {
		if got := retryStatus(c.m); got != c.want {
			t.Errorf("%v: got %s want %s", c.m, got, c.want)
		}
	}
}

func TestClassify(t *testing.T) {
	hosts := []string{"ok", "off", "disk", "err", "none", "nolog", "lost", "both"}
	off := toSet([]string{"off", "both"})
	cand := toSet([]string{"disk", "err", "none", "nolog", "lost", "both"})
	retry := map[string]map[string]bool{
		"disk":  {mkDisk: true},
		"err":   {mkErr: true},
		"nolog": {mkNoLog: true},
		"both":  {mkDisk: true},
		"none":  {},
	}
	want := []string{stDone, stUnreach, stNoDisk, stStopped, stInstall, stInstall, stInstall, stUnreach}
	if got := classify(hosts, off, cand, retry); !reflect.DeepEqual(got, want) {
		t.Errorf("got %v want %v", got, want)
	}
}

func TestResolveGosshPath(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "gossh")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\n"), 0755); err != nil {
		t.Fatal(err)
	}
	cases := map[string]string{
		"":               "",
		bin:              bin,
		`"` + bin + `"`:  bin,
		"'" + bin + "'":  bin,
		dir:              bin,
		dir + "/":        bin,
		`"` + dir + `/"`: bin,
		"/no/such/gossh": "/no/such/gossh",
	}
	for in, want := range cases {
		if got := resolveGosshPath(in); got != want {
			t.Errorf("resolveGosshPath(%q) = %q, want %q", in, got, want)
		}
	}
	home, _ := os.UserHomeDir()
	if got := resolveGosshPath("~/x/gossh"); got != filepath.Join(home, "x/gossh") {
		t.Errorf("~ 확장 실패: %q", got)
	}
}
