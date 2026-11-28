package main

import (
	"flag"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"ip-change/internal/remote"
)

// main() 과 같은 옵션 집합을 가진 FlagSet 입니다 (값을 받는 옵션은 valueFlags 와 같아야 합니다).
func engineFlagSet() *flag.FlagSet {
	fs := flag.NewFlagSet("ip-change-engine", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	for name := range valueFlags {
		fs.String(name, "", "")
	}
	for _, name := range []string{"check", "rollback", "p", "fix-gateway", "monitor"} {
		fs.Bool(name, false, "")
	}
	return fs
}

// 목록파일은 위치 인자로 줄 수 있고, 옵션은 목록파일 앞에도 뒤에도 올 수 있다.
func TestListFileAsPositionalArgument(t *testing.T) {
	cases := []struct {
		name   string
		args   []string
		list   string
		check  bool
		values map[string]string
	}{
		{"목록파일만", []string{"list.txt"}, "list.txt", false, nil},
		{"앞 옵션", []string{"-check", "list.txt"}, "list.txt", true, nil},
		{"뒤 옵션", []string{"list.txt", "-check"}, "list.txt", true, nil},
		{"뒤 옵션 값", []string{"list.txt", "-u", "admin", "-P", "2222", "-i", "/k/id_rsa"}, "list.txt", false,
			map[string]string{"u": "admin", "P": "2222", "i": "/k/id_rsa"}},
		{"앞뒤 섞음", []string{"-u", "admin", "list.txt", "-bond-mode", "lacp", "-check"}, "list.txt", true,
			map[string]string{"u": "admin", "bond-mode": "lacp"}},
		{"등호 형식", []string{"list.txt", "-bond-mode=lacp", "-c", "5"}, "list.txt", false,
			map[string]string{"bond-mode": "lacp", "c": "5"}},
		{"롤백 STAMP", []string{"list.txt", "-rollback", "-rollback-to", "20260101000000"}, "list.txt", false,
			map[string]string{"rollback-to": "20260101000000"}},
		{"-- 뒤는 모두 위치 인자", []string{"-check", "--", "-weird.txt"}, "-weird.txt", true, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			fs := engineFlagSet()
			if err := fs.Parse(reorderArgs(c.args)); err != nil {
				t.Fatalf("파싱 실패: %v", err)
			}
			if fs.NArg() != 1 || fs.Arg(0) != c.list {
				t.Fatalf("위치 인자 = %v, 기대 [%s]", fs.Args(), c.list)
			}
			if got := fs.Lookup("check").Value.String() == "true"; got != c.check {
				t.Errorf("-check = %v, 기대 %v", got, c.check)
			}
			for k, v := range c.values {
				if got := fs.Lookup(k).Value.String(); got != v {
					t.Errorf("-%s = %q, 기대 %q", k, got, v)
				}
			}
		})
	}
}

// 값을 받는 옵션 목록과 참/거짓 옵션 목록이 겹치지 않아야 목록파일이 옵션 값으로 먹히지 않는다.
func TestValueFlagsDoNotSwallowBoolFlags(t *testing.T) {
	for _, b := range []string{"check", "rollback", "p", "fix-gateway", "monitor"} {
		if valueFlags[b] {
			t.Errorf("-%s 는 값을 받지 않는 옵션인데 valueFlags 에 있습니다", b)
		}
	}
	want := []string{"config", "targets", "gossh", "u", "i", "P", "c", "t", "remote-path", "rollback-to", "bond-mode"}
	for _, v := range want {
		if !valueFlags[v] {
			t.Errorf("-%s 는 값을 받는 옵션인데 valueFlags 에 없습니다", v)
		}
	}
	if len(valueFlags) != len(want) {
		t.Errorf("valueFlags 에 예상 밖 항목이 있습니다: %v", valueFlags)
	}
}

func TestReorderArgsKeepsOrderOfOptionsAndPositionals(t *testing.T) {
	got := reorderArgs([]string{"a.txt", "-check", "-u", "root", "-p"})
	want := []string{"-check", "-u", "root", "-p", "a.txt"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("reorderArgs = %v, 기대 %v", got, want)
	}
}

// gossh 를 어디서도 찾지 못하면 안내 문구로 실패하고, 목록파일은 읽지 않는다.
func TestRunGosshNotFoundMessage(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	t.Setenv("IP_CHANGE_GOSSH", "")
	err := run(runOpts{list: filepath.Join(t.TempDir(), "없는목록.txt")})
	if err == nil || err.Error() != remote.GosshNotFound {
		t.Fatalf("err = %v, 기대 %q", err, remote.GosshNotFound)
	}
	for _, w := range []string{"-gossh", "IP_CHANGE_GOSSH"} {
		if !strings.Contains(err.Error(), w) {
			t.Errorf("안내 문구에 %q 가 없습니다: %s", w, err)
		}
	}
}

// -gossh 로 지정한 경로가 틀리면 다른 곳을 찾지 않고 그 경로를 알려준다.
func TestRunGosshWrongExplicitPath(t *testing.T) {
	good := filepath.Join(t.TempDir(), "gossh")
	if err := os.WriteFile(good, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", filepath.Dir(good))
	bad := filepath.Join(t.TempDir(), "nope", "gossh")
	err := run(runOpts{list: "x", remote: remote.Options{GosshPath: bad}})
	if err == nil || !strings.Contains(err.Error(), bad) {
		t.Fatalf("err = %v, 틀린 경로 %s 를 알려야 합니다", err, bad)
	}
}

// gossh 의 키 인증 실패 표준에러 한 줄(호스트 이름은 앞에서 떼어진 상태)은 "패스워드 필요" 로 보고된다.
func TestParseNodeAuthFailureSampleLine(t *testing.T) {
	body := "ERROR: SSH 접속 실패: ssh: handshake failed: ssh: unable to authenticate, attempted methods [none publickey], no supported methods remain"
	r := remote.Result{Host: "node01", Errs: []string{body}}
	if !r.AuthFailed() {
		t.Fatalf("AuthFailed 가 false 입니다")
	}
	row := parseNode("node01", r, true)
	if row.Status != "FAIL" || row.Note != noteNeedPassword {
		t.Fatalf("row = %+v", row)
	}
	r.PasswordTried = true
	if row := parseNode("node01", r, true); row.Note != notePasswordFailed {
		t.Fatalf("패스워드 시도 뒤 비고 = %q", row.Note)
	}
	// 접속 시간 초과 등 다른 사유는 인증 실패로 보지 않는다
	other := remote.Result{Host: "node01", Errs: []string{"ERROR: SSH 접속 실패: dial tcp 10.0.0.1:22: i/o timeout"}}
	if other.AuthFailed() {
		t.Errorf("시간 초과를 인증 실패로 봤습니다")
	}
	if row := parseNode("node01", other, true); !strings.Contains(row.Note, "i/o timeout") {
		t.Errorf("비고 = %q", row.Note)
	}
}
