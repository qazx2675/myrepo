//go:build linux

package main

// tui_tty_linux.go — TUI 용 raw 터미널(termios TCGETS/TCSETS)·창 크기(TIOCGWINSZ)·SIGWINCH.
// 표준 라이브러리 syscall 만 사용 (정적 빌드·RHEL6 커널 2.6.32 호환). auto_setup tty_linux.go 이식.

import (
	"os"
	"os/signal"
	"syscall"
	"unsafe"
)

// tuiMakeRaw 는 터미널을 완전한 raw 모드로 바꾸고 되돌리는 함수를 돌려줍니다.
// ttyMakeRaw(비밀번호 입력용)와 달리 ISIG 도 꺼서 Ctrl+C/Ctrl+Z 가 키로 들어오게 한다 (종료는 q).
func tuiMakeRaw(fd int) (func(), error) {
	var old syscall.Termios
	if _, _, e := syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), uintptr(syscall.TCGETS), uintptr(unsafe.Pointer(&old))); e != 0 {
		return nil, e
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
	if _, _, e := syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), uintptr(syscall.TCSETS), uintptr(unsafe.Pointer(&t))); e != 0 {
		return nil, e
	}
	return func() {
		syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), uintptr(syscall.TCSETS), uintptr(unsafe.Pointer(&old)))
	}, nil
}

// tuiTermSize 는 터미널 칸 수입니다 (실패하면 80x24).
func tuiTermSize(fd int) (int, int) {
	var ws struct{ Row, Col, X, Y uint16 }
	if _, _, e := syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), uintptr(syscall.TIOCGWINSZ), uintptr(unsafe.Pointer(&ws))); e != 0 || ws.Row == 0 || ws.Col == 0 {
		return 80, 24
	}
	return int(ws.Col), int(ws.Row)
}

// tuiNotifyResize 는 창 크기 변경(SIGWINCH)을 c 로 받습니다.
func tuiNotifyResize(c chan<- os.Signal) { signal.Notify(c, syscall.SIGWINCH) }

// tuiQuitSignals 는 TUI 를 끝내고 터미널을 되돌려야 하는 시그널입니다 (Ctrl+C 는 raw 모드라 키로 들어온다).
func tuiQuitSignals() []os.Signal {
	return []os.Signal{os.Interrupt, syscall.SIGTERM, syscall.SIGHUP}
}
