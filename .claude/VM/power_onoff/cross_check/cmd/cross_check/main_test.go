package main

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestParseTargets(t *testing.T) {
	in := "# 주석\nvm01ev01\tvm02ev01  vm03ev01\n\n  vm04ev01 # 뒤 주석\r\nvm05ev01"
	got, err := parseTargets(strings.NewReader(in))
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"vm01ev01", "vm02ev01", "vm03ev01", "vm04ev01", "vm05ev01"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v", got)
	}
}

func TestUsageExitCodes(t *testing.T) {
	t.Setenv("CROSS_CHECK_SOCKET", filepath.Join(t.TempDir(), "none.sock"))
	cases := []struct {
		args []string
		want int
	}{
		{nil, exitUsage},                                  // 사용자 미지정
		{[]string{"-h"}, exitOK},                          // 도움말
		{[]string{"--version"}, exitOK},                   // 버전
		{[]string{"--run", "--admin"}, exitUsage},         // 모드 둘
		{[]string{"--user", "a", "extra"}, exitUsage},     // 남는 인자
		{[]string{"--bogus"}, exitUsage},                  // 모르는 플래그
		{[]string{"--run", "--user", "alice"}, exitUsage}, // 확인자 없음
		{[]string{"--start"}, exitUsage},                  // job 없음
		{[]string{"--start", "--job", "x"}, exitUsage},    // token 없음
		{[]string{"--user", "Bad-Name"}, exitUsage},       // 이름 규칙
		{[]string{"--user", "admin"}, exitUsage},          // 예약 이름
		{[]string{"--user", "alice", "--checker", "bob"}, exitUsage},
		{[]string{"--user", "alice"}, exitError}, // 데몬 없음
		{[]string{"--start", "--job", "x", "--token", "t"}, exitError},
	}
	for _, c := range cases {
		if got := realMain(c.args); got != c.want {
			t.Errorf("%v → %d, want %d", c.args, got, c.want)
		}
	}
}

func TestCheckerTypoAlias(t *testing.T) {
	t.Setenv("CROSS_CHECK_SOCKET", filepath.Join(t.TempDir(), "none.sock"))
	// --cechker 도 확인자로 받아 "확인자 없음"(2)이 아니라 같은 사람 → 중단(1)이 된다
	if got := realMain([]string{"--run", "--user", "alice", "--cechker", "alice", "--targets-file", filepath.Join(t.TempDir(), "x")}); got != exitFail {
		t.Fatalf("got %d", got)
	}
	if got := realMain([]string{"-run", "-user", "alice", "-checker", "alice", "--targets-file", filepath.Join(t.TempDir(), "x")}); got != exitFail {
		t.Fatalf("got %d", got)
	}
}

func TestNewFlagUsageExitCodes(t *testing.T) {
	t.Setenv("CROSS_CHECK_SOCKET", filepath.Join(t.TempDir(), "none.sock"))
	t.Setenv("CROSS_CHECK_TOKEN", "")
	cases := []struct {
		args []string
		want int
	}{
		{[]string{"--finish"}, exitUsage},                                                      // job 없음
		{[]string{"--finish", "--job", "x"}, exitUsage},                                        // result 없음
		{[]string{"--finish", "--job", "x", "--result", "maybe"}, exitUsage},                   // result 값
		{[]string{"--finish", "--job", "x", "--result", "done"}, exitUsage},                    // token 없음
		{[]string{"--finish", "--start", "--job", "x"}, exitUsage},                             // 모드 둘
		{[]string{"--user", "alice", "--caller", "x"}, exitUsage},                              // --caller 는 --run 과만
		{[]string{"--start", "--job", "x", "--result", "done"}, exitUsage},                     // --result 는 --finish 와만
		{[]string{"--run", "--plain", "--user", "a", "--checker", "b"}, exitUsage},             // --plain 은 화면에만
		{[]string{"--finish", "--job", "x", "--result", "aborted", "--token", "t"}, exitError}, // 데몬 없음
		{[]string{"--user", "alice", "--plain"}, exitError},                                    // 데몬 없음(줄 입력 화면 강제)
	}
	for _, c := range cases {
		if got := realMain(c.args); got != c.want {
			t.Errorf("%v → %d, want %d", c.args, got, c.want)
		}
	}
}

func TestCallerFromCmdline(t *testing.T) {
	cases := []struct {
		args []string
		comm string
		want string
	}{
		{[]string{"bash", "/root/cc_playground_src/test/playground_worker.sh", "alice", "bob"}, "bash", "playground_worker.sh"},
		{[]string{"/bin/bash", "-x", "./vm_onoff.sh"}, "bash", "vm_onoff.sh"},
		{[]string{"-bash"}, "bash", "bash"}, // 로그인 셸에서 직접 실행
		{[]string{"bash", "-c", "cross_check --run ..."}, "bash", "bash"},
		{[]string{"sh", "--", "run.sh"}, "sh", "run.sh"},
		{[]string{"/usr/bin/python3.6", "-W", "ignore", "/opt/tools/power.py", "--all"}, "python3", "power.py"},
		{[]string{"python3", "-m", "mod"}, "python3", "python3"},
		{[]string{"perl", "-w", "x.pl"}, "perl", "x.pl"},
		{[]string{"node", "app.js"}, "node", "app.js"},
		{[]string{"ksh", "+o", "emacs", "job.ksh"}, "ksh", "job.ksh"},
		{[]string{"/usr/local/bin/vm_power_onoff", "-c", "bob", "on"}, "vm_power_onof", "vm_power_onoff"},
		{nil, "vm_power_onof", "vm_power_onof"}, // cmdline 이 비면 comm
		{nil, "", ""},
	}
	for _, c := range cases {
		if got := callerFromCmdline(c.args, c.comm); got != c.want {
			t.Errorf("callerFromCmdline(%q, %q) = %q, want %q", c.args, c.comm, got, c.want)
		}
	}
}

func TestResolveCallerOrder(t *testing.T) {
	t.Setenv(envCaller, "env_script.sh")
	if got := resolveCaller("flag.sh"); got != "flag.sh" {
		t.Fatalf("--caller 우선: %q", got)
	}
	if got := resolveCaller(""); got != "env_script.sh" {
		t.Fatalf("환경변수 다음: %q", got)
	}
	t.Setenv(envCaller, "\x1b[2J")
	if got := resolveCaller("  \x07 "); got == "" || strings.Contains(got, "\x1b") {
		t.Fatalf("제어문자만 있으면 자동 감지로: %q", got)
	}
	long := strings.Repeat("x", 100)
	if got := resolveCaller(long); len(got) != 64 {
		t.Fatalf("64자 제한: %d", len(got))
	}
}
