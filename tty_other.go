//go:build !linux

// tty_other.go - 비 linux 빌드용 대체(개발 PC 에서 vet·빌드만): tty 없음으로 취급한다.
package main

import (
	"errors"
	"os"
	"time"
)

func isTTY(f *os.File) bool { return false }

func makeRaw(f *os.File) (func(), error) {
	return nil, errors.New("raw tty 는 linux 에서만 지원")
}

func waitReadable(f *os.File, d time.Duration) bool { return true }

func ttySize(f *os.File) (int, int) { return 80, 24 }

func notifyResize(c chan<- os.Signal) {}

func ignoreSuspend() func() { return func() {} }
