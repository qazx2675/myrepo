//go:build linux

package main

// 리눅스 터미널 제어: termios ioctl 을 syscall 로 직접 부른다(외부 라이브러리 없음).

import (
	"errors"
	"os"
	"os/signal"
	"syscall"
	"unsafe"
)

type winsize struct{ Row, Col, X, Y uint16 }

func ioctl(fd int, req uintptr, p unsafe.Pointer) error {
	_, _, e := syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), req, uintptr(p))
	if e != 0 {
		return e
	}
	return nil
}

// isTerminal 은 fd 가 터미널인지.
func isTerminal(fd int) bool {
	var t syscall.Termios
	return ioctl(fd, syscall.TCGETS, unsafe.Pointer(&t)) == nil
}

// enterRaw 는 fd 를 raw 모드(에코·줄 단위 입력 끔, Ctrl-C 는 SIGINT 로 유지)로 바꾸고 원래대로 되돌리는 함수를 돌려준다.
func enterRaw(fd int) (func(), error) {
	var orig syscall.Termios
	if err := ioctl(fd, syscall.TCGETS, unsafe.Pointer(&orig)); err != nil {
		return nil, err
	}
	raw := orig
	raw.Iflag &^= syscall.IGNBRK | syscall.BRKINT | syscall.PARMRK | syscall.ISTRIP | syscall.INLCR | syscall.IGNCR | syscall.ICRNL | syscall.IXON
	raw.Lflag &^= syscall.ECHO | syscall.ECHONL | syscall.ICANON | syscall.IEXTEN
	raw.Cflag &^= syscall.CSIZE | syscall.PARENB
	raw.Cflag |= syscall.CS8
	raw.Cc[syscall.VMIN] = 1
	raw.Cc[syscall.VTIME] = 0
	if err := ioctl(fd, syscall.TCSETS, unsafe.Pointer(&raw)); err != nil {
		return nil, errors.New("터미널을 raw 모드로 바꿀 수 없습니다: " + err.Error())
	}
	return func() { _ = ioctl(fd, syscall.TCSETS, unsafe.Pointer(&orig)) }, nil
}

// termSize 는 터미널 폭·높이(실패하면 80x24).
func termSize(fd int) (int, int) {
	var ws winsize
	if err := ioctl(fd, syscall.TIOCGWINSZ, unsafe.Pointer(&ws)); err != nil || ws.Col == 0 || ws.Row == 0 {
		return 80, 24
	}
	return int(ws.Col), int(ws.Row)
}

// notifyResize 는 창 크기가 바뀌면 c 로 신호를 보낸다.
func notifyResize(c chan<- os.Signal) { signal.Notify(c, syscall.SIGWINCH) }
