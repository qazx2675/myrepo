// check_test.go - check.go 단위 테스트: gossh 출력 파싱(응답/anaconda/무응답), local·os6 호출 형태, 임시파일 정리
package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fakeExternal(t *testing.T, fn func(stdin, name string, args ...string) (string, error)) {
	t.Helper()
	old := runExternal
	runExternal = fn
	t.Cleanup(func() { runExternal = old })
}

func TestParseCheck(t *testing.T) {
	hosts := []string{"h1", "h2", "h3", "h4"}
	out := "h1: 123.45 678.9\nunknown: 1.0 2.0\nh2 : 5.5 6.6\nh1: extra line\nbad line\n"
	res := parseCheck(hosts, out, "h3\nnope\n")
	if r := res["h1"]; !r.Responded || r.Anaconda || r.Uptime != 123.45 {
		t.Fatalf("h1: %+v", r)
	}
	if r := res["h2"]; !r.Responded || r.Uptime != 5.5 {
		t.Fatalf("h2: %+v", r)
	}
	if r := res["h3"]; !r.Responded || !r.Anaconda {
		t.Fatalf("h3(anaconda): %+v", r)
	}
	if r := res["h4"]; r.Responded || r.Anaconda {
		t.Fatalf("h4(무응답): %+v", r)
	}
	if _, ok := res["unknown"]; ok {
		t.Fatal("요청하지 않은 호스트가 결과에 포함됨")
	}
}

func TestCheckLocal(t *testing.T) {
	var listFile string
	fakeExternal(t, func(stdin, name string, args ...string) (string, error) {
		if name != "gossh" {
			t.Fatalf("명령: %s", name)
		}
		want := []string{"-pm", "-script", "-w"}
		if len(args) != 5 || args[0] != want[0] || args[1] != want[1] || args[2] != want[2] || args[4] != "cat /proc/uptime" {
			t.Fatalf("인자: %v", args)
		}
		listFile = args[3]
		b, err := os.ReadFile(listFile)
		if err != nil || string(b) != "a\nb\nc\n" {
			t.Fatalf("목록 파일: %q %v", b, err)
		}
		os.WriteFile(listFile+"_os_install", []byte("b\n"), 0644)
		os.WriteFile(listFile+"_res_off", []byte("c\n"), 0644)
		return "a: 10.5 20.0\n", nil
	})
	res, err := NewRealChecker().Check("local", []string{"a", "b", "c"})
	if err != nil {
		t.Fatal(err)
	}
	if !res["a"].Responded || res["a"].Uptime != 10.5 || res["a"].Anaconda {
		t.Fatalf("a: %+v", res["a"])
	}
	if !res["b"].Anaconda {
		t.Fatalf("b: %+v", res["b"])
	}
	if res["c"].Responded {
		t.Fatalf("c: %+v", res["c"])
	}
	if m, _ := filepath.Glob(listFile + "*"); len(m) != 0 {
		t.Fatalf("임시 파일이 남음: %v", m)
	}
}

func TestCheckLocalExecFailure(t *testing.T) {
	fakeExternal(t, func(stdin, name string, args ...string) (string, error) {
		return "", errors.New("gossh 없음")
	})
	if _, err := NewRealChecker().Check("local", []string{"a"}); err == nil {
		t.Fatal("gossh 실행 실패는 에러여야 함")
	}
}

func TestCheckOS6(t *testing.T) {
	oldM, oldG := os6_mgmt, os6_gossh
	os6_mgmt, os6_gossh = "mgmtstub", "/stub/gossh"
	t.Cleanup(func() { os6_mgmt, os6_gossh = oldM, oldG })

	fakeExternal(t, func(stdin, name string, args ...string) (string, error) {
		if name != "ssh" || len(args) != 2 || args[0] != "mgmtstub" {
			t.Fatalf("호출: %s %v", name, args)
		}
		if stdin != "a\nb\n" {
			t.Fatalf("stdin: %q", stdin)
		}
		r := args[1]
		for _, s := range []string{"f=$(mktemp)", "cat > $f", "/stub/gossh -pm -script -w $f 'cat /proc/uptime'",
			"echo ==OSI==", "cat ${f}_os_install", "rm -f $f ${f}_*"} {
			if !strings.Contains(r, s) {
				t.Fatalf("원격 문자열에 %q 없음: %s", s, r)
			}
		}
		return "a: 77.7 1.0\n==OSI==\nb\n", nil
	})
	res, err := NewRealChecker().Check("os6", []string{"a", "b"})
	if err != nil {
		t.Fatal(err)
	}
	if !res["a"].Responded || res["a"].Uptime != 77.7 || !res["b"].Anaconda {
		t.Fatalf("결과: %+v", res)
	}
}

func TestCheckOS6BadOutput(t *testing.T) {
	oldM, oldG := os6_mgmt, os6_gossh
	os6_mgmt, os6_gossh = "mgmtstub", "/stub/gossh"
	t.Cleanup(func() { os6_mgmt, os6_gossh = oldM, oldG })
	fakeExternal(t, func(stdin, name string, args ...string) (string, error) {
		return "", errors.New("ssh 실패")
	})
	if _, err := NewRealChecker().Check("os6", []string{"a"}); err == nil {
		t.Fatal("구분선 없는 응답은 에러여야 함")
	}
}

func TestCheckUnknownRouteAndEmpty(t *testing.T) {
	if _, err := NewRealChecker().Check("x", []string{"a"}); err == nil {
		t.Fatal("알 수 없는 route 는 에러")
	}
	if res, err := NewRealChecker().Check("local", nil); err != nil || len(res) != 0 {
		t.Fatalf("빈 목록: %v %v", res, err)
	}
}
