//go:build !linux

package main

import (
	"errors"
	"os"
)

// 리눅스가 아닌 환경(개발용 빌드)에서는 raw 입력을 지원하지 않습니다. 운영 대상은 리눅스(RHEL6/8)입니다.
func ttyIsTerminal(fd int) bool {
	fi, err := os.Stdin.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}

func ttyMakeRaw(fd int) (func(), error) {
	return nil, errors.New("이 플랫폼에서는 비밀번호 raw 입력을 지원하지 않습니다 (리눅스에서 실행하십시오)")
}

func drainPending(in *os.File) {}

func ttyFlushInput(fd int) {}
