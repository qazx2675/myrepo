package main

// caller.go - --run 을 부른 스크립트 이름(caller) 정하기. 정보용 표시값이며 위조할 수 있으므로 권한 판단에 쓰지 않는다.
//
// 순서: --caller NAME → 환경변수 CROSS_CHECK_CALLER → 부모 프로세스에서 자동 감지 → "(알 수 없음)".
// 자동 감지: /proc/<부모 pid>/cmdline (없으면 comm). 부모가 인터프리터(bash, sh, dash, zsh, ksh, python*, perl, ruby, node)이면
// 첫 번째 옵션 아닌 인자(스크립트)의 basename, 아니면 부모 실행 파일의 basename.
// (작업 스크립트 안의 out=$(... | cross_check --run ...) 는 부모가 스크립트를 fork 한 서브셸이라 cmdline 이 스크립트와 같다.)

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"cross_check/internal/daemon"
)

const (
	envCaller     = "CROSS_CHECK_CALLER" // --caller 대신 쓰는 환경변수
	unknownCaller = "(알 수 없음)"
)

// resolveCaller 는 flag → 환경변수 → 자동 감지 순으로 caller 를 정하고 정리한다(제어문자 제거, 64자).
func resolveCaller(flagVal string) string {
	for _, v := range []string{flagVal, os.Getenv(envCaller)} {
		if c := daemon.SanitizeCaller(v); c != "" {
			return c
		}
	}
	if c := daemon.SanitizeCaller(detectCaller(os.Getppid())); c != "" {
		return c
	}
	return unknownCaller
}

// detectCaller 는 /proc 에서 pid 의 cmdline(없으면 comm)을 읽어 callerFromCmdline 으로 이름을 정한다. 못 읽으면 "".
func detectCaller(pid int) string {
	if pid <= 1 {
		return ""
	}
	dir := filepath.Join("/proc", strconv.Itoa(pid))
	var args []string
	if b, err := os.ReadFile(filepath.Join(dir, "cmdline")); err == nil {
		for _, a := range strings.Split(string(b), "\x00") {
			if a != "" {
				args = append(args, a)
			}
		}
	}
	comm := ""
	if b, err := os.ReadFile(filepath.Join(dir, "comm")); err == nil {
		comm = strings.TrimSpace(string(b))
	}
	return callerFromCmdline(args, comm)
}

// interpreterName 은 argv[0] 의 basename 이 스크립트 인터프리터인지(로그인 셸의 앞 '-' 는 뗀다).
func interpreterName(argv0 string) bool {
	b := strings.TrimPrefix(filepath.Base(argv0), "-")
	switch b {
	case "bash", "sh", "dash", "zsh", "ksh", "perl", "ruby", "node":
		return true
	}
	return strings.HasPrefix(b, "python")
}

// optsWithValue 는 인터프리터 옵션 중 다음 인자를 값으로 받는 것(그 값을 스크립트로 오인하지 않게).
var optsWithValue = map[string]bool{"-W": true, "-X": true, "-o": true, "-O": true, "+O": true, "+o": true, "-I": true, "-r": true}

// inlineOpts 는 스크립트 파일 대신 명령 글을 받는 옵션(이 경우 스크립트 이름이 없으므로 인터프리터 이름을 쓴다).
var inlineOpts = map[string]bool{"-c": true, "-e": true, "-m": true, "-E": true}

// callerFromCmdline 은 부모의 argv(와 comm)로 caller 이름을 정한다. 정할 수 없으면 "".
func callerFromCmdline(args []string, comm string) string {
	if len(args) == 0 {
		return strings.TrimPrefix(comm, "-")
	}
	exe := strings.TrimPrefix(filepath.Base(args[0]), "-")
	if !interpreterName(args[0]) {
		return exe
	}
	for i := 1; i < len(args); i++ {
		a := args[i]
		if inlineOpts[a] {
			return exe
		}
		if optsWithValue[a] {
			i++
			continue
		}
		if a == "--" {
			if i+1 < len(args) {
				return filepath.Base(args[i+1])
			}
			return exe
		}
		if strings.HasPrefix(a, "-") || strings.HasPrefix(a, "+") {
			continue
		}
		return filepath.Base(a)
	}
	return exe
}
