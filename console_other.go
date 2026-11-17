//go:build !windows

package main

import (
	"bufio"
	"io"
	"os"
	"os/exec"
	"strings"
)

func setupConsole() {}

// stdinIsTerminal 은 표준 입력이 TTY 인지 본다 (/dev/null 같은 문자 장치는 stty 가 거부한다).
func stdinIsTerminal() bool {
	st, err := os.Stdin.Stat()
	if err != nil || st.Mode()&os.ModeCharDevice == 0 {
		return false
	}
	c := exec.Command("stty", "-g")
	c.Stdin = os.Stdin
	return c.Run() == nil
}

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
