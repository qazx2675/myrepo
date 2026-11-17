//go:build !windows

package main

import (
	"bufio"
	"errors"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
)

func setupConsole() {}

// oNoFollow 는 기록 파일을 열 때 심볼릭 링크를 따라가지 않게 한다.
const oNoFollow = syscall.O_NOFOLLOW

// isTTY 는 f 가 터미널인지 본다 (/dev/null 같은 문자 장치는 stty 가 거부한다).
func isTTY(f *os.File) bool {
	st, err := f.Stat()
	if err != nil || st.Mode()&os.ModeCharDevice == 0 {
		return false
	}
	c := exec.Command("stty", "-g")
	c.Stdin = f
	return c.Run() == nil
}

func stdinIsTerminal() bool  { return isTTY(os.Stdin) }
func stdoutIsTerminal() bool { return isTTY(os.Stdout) }

// readPassword 는 stty -echo 로 입력을 숨기고 한 줄을 읽는다.
func readPassword(in *bufio.Reader, out io.Writer) (string, error) {
	off := exec.Command("stty", "-echo")
	off.Stdin = os.Stdin
	_ = off.Run()
	defer func() {
		on := exec.Command("stty", "echo")
		on.Stdin = os.Stdin
		_ = on.Run()
	}()
	line, err := in.ReadString('\n')
	if err != nil && line == "" {
		return "", err
	}
	return strings.TrimRight(line, "\r\n"), nil
}

// termRaw 는 표준 입력 터미널을 raw 모드로 바꾸고 되돌리는 함수를 돌려준다.
func termRaw() (func(), error) {
	get := exec.Command("stty", "-g")
	get.Stdin = os.Stdin
	saved, err := get.Output()
	if err != nil {
		return nil, err
	}
	set := exec.Command("stty", "raw", "-echo")
	set.Stdin = os.Stdin
	if err := set.Run(); err != nil {
		return nil, err
	}
	return func() {
		c := exec.Command("stty", strings.TrimSpace(string(saved)))
		c.Stdin = os.Stdin
		_ = c.Run()
	}, nil
}

// termSize 는 터미널 행, 열 수다. 모르면 24x80.
func termSize() (int, int) {
	c := exec.Command("stty", "size")
	c.Stdin = os.Stdin
	out, err := c.Output()
	if err == nil {
		f := strings.Fields(string(out))
		if len(f) == 2 {
			r, e1 := strconv.Atoi(f[0])
			cl, e2 := strconv.Atoi(f[1])
			if e1 == nil && e2 == nil && r > 0 && cl > 0 {
				return r, cl
			}
		}
	}
	return 24, 80
}

// fileUID 는 파일 소유자 uid 다.
func fileUID(fi os.FileInfo) (int, bool) {
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return -1, false
	}
	return int(st.Uid), true
}

func euid() int { return os.Geteuid() }

// checkOwnerPerm 은 리눅스 설정파일 규칙이다: 권한 600, 소유자는 실행 사용자(root 는 예외).
func checkOwnerPerm(path string, fi os.FileInfo, wantRootOwner bool) error {
	if fi.Mode().Perm() != 0o600 {
		return errors.New("권한이 600 이 아닙니다 (" + strconv.FormatUint(uint64(fi.Mode().Perm()), 8) + "). 'chmod 600 " + path + "' 를 실행하세요")
	}
	uid, ok := fileUID(fi)
	if !ok {
		return nil
	}
	if wantRootOwner {
		if uid != 0 {
			return errors.New("소유자가 root 가 아닙니다")
		}
		return nil
	}
	if uid != euid() && euid() != 0 {
		return errors.New("소유자(uid " + strconv.Itoa(uid) + ")가 실행 사용자(uid " + strconv.Itoa(euid()) + ")와 다릅니다")
	}
	return nil
}
