//go:build !linux

// tty_other.go - 비 linux 빌드용 대체(개발 PC 에서 vet·렌더 단위 테스트만): tty 없음으로 취급 → 항상 줄 입력 화면.
package tui

import (
	"errors"
	"os"
	"time"
)

// fullScreenOS 는 이 빌드가 전체 화면 TUI 를 지원하는지(linux 만).
const fullScreenOS = false

func isTTY(f *os.File) bool { return false }

func makeRaw(f *os.File) (func(), error) {
	return nil, errors.New("raw tty 는 linux 에서만 지원")
}

func waitReadable(f *os.File, d time.Duration) bool { return true }

func ttySize(f *os.File) (int, int) { return 80, 24 }

func notifyResize(c chan<- os.Signal) {}

func ignoreStop() {}
func resetStop()  {}
