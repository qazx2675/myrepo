//go:build linux

// tty_linux.go - TUI 용 raw 터미널(termios ioctl TCGETS/TCSETS)·창 크기(TIOCGWINSZ)·tty 판별. 표준 라이브러리 syscall 만 사용(정적 빌드·RHEL6 호환)
package main

import (
	"os"
	"syscall"
	"unsafe"
)

func ioctl(fd uintptr, req uintptr, arg unsafe.Pointer) error {
	if _, _, e := syscall.Syscall(syscall.SYS_IOCTL, fd, req, uintptr(arg)); e != 0 {
		return e
	}
	return nil
}

// isTTY: f 가 터미널이면 true
func isTTY(f *os.File) bool {
	var t syscall.Termios
	return ioctl(f.Fd(), syscall.TCGETS, unsafe.Pointer(&t)) == nil
}

// makeRaw: 터미널을 raw 모드로 바꾸고 원래대로 돌리는 함수를 돌려준다 (x/term MakeRaw 와 같은 설정)
func makeRaw(f *os.File) (func(), error) {
	fd := f.Fd()
	var old syscall.Termios
	if err := ioctl(fd, syscall.TCGETS, unsafe.Pointer(&old)); err != nil {
		return nil, err
	}
	t := old
	t.Iflag &^= syscall.IGNBRK | syscall.BRKINT | syscall.PARMRK | syscall.ISTRIP |
		syscall.INLCR | syscall.IGNCR | syscall.ICRNL | syscall.IXON
	t.Oflag &^= syscall.OPOST
	t.Lflag &^= syscall.ECHO | syscall.ECHONL | syscall.ICANON | syscall.ISIG | syscall.IEXTEN
	t.Cflag &^= syscall.CSIZE | syscall.PARENB
	t.Cflag |= syscall.CS8
	t.Cc[syscall.VMIN] = 1
	t.Cc[syscall.VTIME] = 0
	if err := ioctl(fd, syscall.TCSETS, unsafe.Pointer(&t)); err != nil {
		return nil, err
	}
	return func() { _ = ioctl(fd, syscall.TCSETS, unsafe.Pointer(&old)) }, nil
}

// ttySize: 터미널 칸 수 (실패하면 80x24)
func ttySize(f *os.File) (int, int) {
	var ws struct{ Row, Col, X, Y uint16 }
	if err := ioctl(f.Fd(), syscall.TIOCGWINSZ, unsafe.Pointer(&ws)); err != nil || ws.Row == 0 || ws.Col == 0 {
		return 80, 24
	}
	return int(ws.Col), int(ws.Row)
}
