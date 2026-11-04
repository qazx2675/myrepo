// ssh_timeout_test.go - conf ssh_timeout(os6_mgmt ssh 접속 제한) 와 ssh 실패 원인 분류 테스트
package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSSHTimeoutSec(t *testing.T) {
	saveConfVars(t)
	cases := []struct {
		in   string
		want int
	}{
		{"", 30}, {"abc", 30}, {" 45 ", 45}, {"5", 5}, {"4", 5}, {"0", 5}, {"-3", 5}, {"10", 10}, {"3.5", 30},
	}
	for _, c := range cases {
		ssh_timeout = c.in
		if got := sshTimeoutSec(); got != c.want {
			t.Errorf("ssh_timeout=%q: %d, want %d", c.in, got, c.want)
		}
	}
	ssh_timeout = "20"
	if sshConnectOpt() != "ConnectTimeout=20" {
		t.Errorf("sshConnectOpt = %q", sshConnectOpt())
	}
}

func TestSSHTimeoutFromConf(t *testing.T) {
	saveConfVars(t)
	loadConf([]string{writeConf(t, "ssh_timeout=45\n")})
	if sshTimeoutSec() != 45 {
		t.Fatalf("conf 에서 읽지 못함: %q", ssh_timeout)
	}
}

// 전체 상한: 접속 제한이 10초를 넘는 만큼의 2배를 더한다 (기본 30초 → +40초), 10초 이하면 그대로
func TestSSHLimit(t *testing.T) {
	saveConfVars(t)
	for _, c := range []struct {
		to   string
		want time.Duration
	}{{"", 100 * time.Second}, {"10", 60 * time.Second}, {"5", 60 * time.Second}, {"60", 160 * time.Second}} {
		ssh_timeout = c.to
		if got := sshLimit(60 * time.Second); got != c.want {
			t.Errorf("ssh_timeout=%q: sshLimit(60s)=%v, want %v", c.to, got, c.want)
		}
	}
	ssh_timeout = ""
	if got := sshLimit(45 * time.Second); got != 85*time.Second {
		t.Errorf("sshLimit(45s)=%v", got)
	}
	if whyTimeoutOS6() != "시간 초과(100초)" {
		t.Errorf("whyTimeoutOS6=%q", whyTimeoutOS6())
	}
}

// os6 래퍼(스크립트)의 ssh 도 conf 값을 따른다
func TestWrapperUsesSSHTimeout(t *testing.T) {
	saveConfVars(t)
	os6_mgmt, os6_gossh = "m", "/g"
	ssh_timeout = "42"
	s := os6WrapperScript("/real")
	if strings.Contains(s, "ConnectTimeout=10") || strings.Count(s, "ConnectTimeout=42") != 3 {
		t.Errorf("래퍼의 ConnectTimeout 이 conf 를 따르지 않음: %d곳", strings.Count(s, "ConnectTimeout=42"))
	}
}

func TestSSHReason(t *testing.T) {
	cases := []struct{ out, want string }{
		{"@@@@ WARNING: REMOTE HOST IDENTIFICATION HAS CHANGED! @@@\nHost key verification failed.\n", "호스트 키 변경"},
		{"Host key verification failed.\n", "호스트 키 미등록"},
		{"root@x: Permission denied (publickey,password).\n", "인증 실패"},
		{"ssh: connect to host x port 22: Connection timed out\n", "접속 시간 초과"},
		{"Connection timed out during banner exchange\n", "접속 시간 초과"},
		{"ssh: connect to host x port 22: Connection refused\n", "접속 거부"},
		{"kex_exchange_identification: Connection closed by remote host\n", "MaxStartups"},
		{"Connection closed by 10.0.0.1 port 22\n", "MaxStartups"},
		{"bash: fork: retry: Resource temporarily unavailable\n", "프로세스 한도"},
		{"exec request failed on channel 0\n", "원격 셸 실행 실패"},
		{"bash: /opt/x/auto_setup: Is a directory\n", "디렉터리"},
		{"bash: /opt/x/auto_setup: Exec format error\n", "형식 오류"},
		{"bash: /opt/x/auto_setup: No such file or directory\n", "실행 파일 없음"},
		{"ssh: Could not resolve hostname zz: Name or service not known\n", "이름 해석"},
	}
	for _, c := range cases {
		if got := sshReason(c.out); !strings.Contains(got, c.want) {
			t.Errorf("sshReason(%q)=%q, want contains %q", c.out, got, c.want)
		}
	}
	if sshReason("정상 출력\n") != "" {
		t.Error("원인이 없는 출력은 빈 값")
	}
	if got := diagReason("Host key verification failed.\n"); !strings.HasPrefix(got, "[호스트 키 미등록") || !strings.Contains(got, "Host key verification failed.") {
		t.Errorf("diagReason=%q", got)
	}
	if diagReason("그냥 오류 한 줄\n") != "그냥 오류 한 줄" {
		t.Error("원인을 모르면 diagHead 그대로")
	}
}

// ssh 가 실패하면 준비확인 오류에 원인이 붙는다
func TestCheckOS6ErrorHasReason(t *testing.T) {
	saveConfVars(t)
	os6_mgmt, os6_gossh = "m", "/g"
	old := runExternalDiag
	runExternalDiag = func(stdin, name string, args ...string) (string, error) {
		return "Host key verification failed.\n", errors.New("exit status 255")
	}
	t.Cleanup(func() { runExternalDiag = old })
	_, err := checkOS6([]string{"a"})
	if err == nil || !strings.Contains(err.Error(), "[호스트 키 미등록") {
		t.Fatalf("err=%v", err)
	}
}

// ssh 의 @@@ 경고 틀 줄이 아니라 핵심 줄이 원인 뒤에 붙는다
func TestDiagReasonPicksKeyLine(t *testing.T) {
	out := "@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@\n@    WARNING: REMOTE HOST IDENTIFICATION HAS CHANGED!     @\n@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@\nHost key verification failed.\n"
	got := diagReason(out)
	if !strings.HasPrefix(got, "[호스트 키 변경") || !strings.HasSuffix(got, "WARNING: REMOTE HOST IDENTIFICATION HAS CHANGED!") {
		t.Errorf("diagReason=%q", got)
	}
}

// 같은 원인은 한 번만 로그, 원인이 바뀌면 다시
func TestLogSSHReasonOnce(t *testing.T) {
	setupDir(t)
	sshReasonMu.Lock()
	sshReasonSeen = map[string]string{}
	sshReasonMu.Unlock()
	for _, o := range []string{"Host key verification failed.\n", "Host key verification failed.\n", "Connection refused\n", "Connection refused\n", "상관없는 출력\n"} {
		logSSHReason("준비확인", o)
	}
	lg, _ := os.ReadFile(logPath())
	if n := strings.Count(string(lg), "os6_mgmt ssh 문제(준비확인)"); n != 2 {
		t.Errorf("로그 %d줄, want 2:\n%s", n, lg)
	}
}

// ssh 가 말없이 시간 초과되면 호스트 사유가 시간 초과(상한 초)이고 로그에 한 번 남는다
func TestCheckOS6SilentTimeoutLogged(t *testing.T) {
	setupDir(t)
	saveConfVars(t)
	sshReasonMu.Lock()
	sshReasonSeen = map[string]string{}
	sshReasonMu.Unlock()
	os6_mgmt, os6_gossh = "m", "/g"
	old := runExternalDiag
	runExternalDiag = func(stdin, name string, args ...string) (string, error) { return "", errCheckTimeout }
	t.Cleanup(func() { runExternalDiag = old })
	for i := 0; i < 2; i++ {
		res, err := checkOS6([]string{"a"})
		if err != nil || res["a"].Responded || res["a"].Why != "시간 초과(100초)" {
			t.Fatalf("res=%+v err=%v", res, err)
		}
	}
	lg, _ := os.ReadFile(logPath())
	if n := strings.Count(string(lg), "응답 없음"); n != 1 {
		t.Errorf("로그 %d줄, want 1:\n%s", n, lg)
	}
}

// 2차 체크의 ssh(runExternal)가 실패하면 stderr 에서 알아본 원인이 로그에 남고, stdout 만 돌려준다
func TestRunExternalSSHReasonLogged(t *testing.T) {
	needBash(t)
	setupDir(t)
	sshReasonMu.Lock()
	sshReasonSeen = map[string]string{}
	sshReasonMu.Unlock()
	bin := t.TempDir()
	os.WriteFile(filepath.Join(bin, "ssh"), []byte("#!/bin/bash\necho out1\necho 'Permission denied (publickey).' >&2\nexit 255\n"), 0755)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	out, err := runExternal("", "ssh", "m", "x")
	if err == nil || out != "out1\n" {
		t.Fatalf("out=%q err=%v", out, err)
	}
	lg, _ := os.ReadFile(logPath())
	if !strings.Contains(string(lg), "os6_mgmt ssh 문제(2차 체크)") || !strings.Contains(string(lg), "인증 실패") {
		t.Errorf("로그에 원인 없음:\n%s", lg)
	}
}
