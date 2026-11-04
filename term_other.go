//go:build !linux

package main

import (
	"errors"
	"fmt"
	"os"
	"strings"
)

func terminalSize(fd int) (cols, rows int) { return 0, 0 }

func enableCbreak(fd int) (restore func(), err error) {
	return nil, errors.New("이 OS에서는 지원하지 않습니다")
}

func enableRaw(fd int) (restore func(), err error) {
	return nil, errors.New("이 OS에서는 지원하지 않습니다")
}

func watchResize(fd int, fn func(cols, rows int)) (stop func()) { return func() {} }

func promptPassword(prompt string) (string, error) {
	fmt.Fprint(os.Stderr, prompt)
	s, err := stdinReader.ReadString('\n')
	if err != nil && s == "" {
		return "", err
	}
	return strings.TrimRight(s, "\r\n"), nil
}
