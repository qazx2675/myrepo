//go:build !linux

// daemon_other.go - 비 linux 빌드용 대체(개발 PC 에서 vet·빌드만): 데몬은 linux 에서만 지원한다.
package main

import (
	"errors"
	"syscall"
)

func detachAttr() *syscall.SysProcAttr { return nil }

func lockExclusive(path string) (func(), error) {
	return nil, errors.New("데몬은 linux 에서만 지원")
}
