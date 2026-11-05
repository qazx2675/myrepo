//go:build !linux

package main

// 리눅스가 아니면(개발용 빌드) TUI 를 쓰지 않는다: isTerminal 이 항상 false 라 현재 상태를 글로만 낸다.

import (
	"errors"
	"os"
)

func isTerminal(fd int) bool { return false }

func enterRaw(fd int) (func(), error) {
	return nil, errors.New("이 운영체제에서는 TUI 를 지원하지 않습니다")
}

func termSize(fd int) (int, int) { return 80, 24 }

func notifyResize(c chan<- os.Signal) {}
