//go:build !linux

package main

import "errors"

func terminalSize(fd int) (cols, rows int) { return 0, 0 }

func enableCbreak(fd int) (restore func(), err error) {
	return nil, errors.New("이 OS에서는 지원하지 않습니다")
}
