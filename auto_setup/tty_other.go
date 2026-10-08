//go:build !linux

// tty_other.go - 비 linux 빌드용 대체(개발 PC 에서 vet 만): tty 없음으로 취급 → 항상 --plain 동작
package main

import (
	"errors"
	"os"
	"syscall"
	"time"
)

func isTTY(f *os.File) bool { return false }

func makeRaw(f *os.File) (func(), error) {
	return nil, errors.New("raw tty 는 linux 에서만 지원")
}

func withAwxTermios(f *os.File) (func(), error) { return func() {}, nil }

func waitReadable(f *os.File, d time.Duration) bool { return true }

func oneoffSysProcAttr() *syscall.SysProcAttr { return nil }

func killGroup(pid int, sig syscall.Signal) {}

func ttySize(f *os.File) (int, int) { return 80, 24 }
