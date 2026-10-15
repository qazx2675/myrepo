//go:build !linux

package main

// tui_tty_other.go — 리눅스가 아닌 환경(개발용 빌드)용 자리 채움. TUI 는 열리지 않고 평문 출력으로 대신한다.

import (
	"errors"
	"os"
)

func tuiMakeRaw(fd int) (func(), error) {
	return nil, errors.New("이 플랫폼에서는 TUI 를 지원하지 않습니다 (리눅스에서 실행하십시오)")
}

func tuiTermSize(fd int) (int, int) { return 80, 24 }

func tuiNotifyResize(c chan<- os.Signal) {}

func tuiQuitSignals() []os.Signal { return []os.Signal{os.Interrupt} }
