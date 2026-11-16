//go:build linux

// tty_linux.go - TUI 용 raw 터미널(termios ioctl TCGETS/TCSETS)·창 크기(TIOCGWINSZ)·tty 판별.
// 표준 라이브러리 syscall 만 쓴다(정적 빌드·폐쇄망 호환). auto_setup 의 tty_linux.go 에서 필요한 부분만 옮겼다.
package main

import (
	"os"
	"os/signal"
	"syscall"
	"time"
	"unsafe"
)

// notifyResize: 창 크기 변경(SIGWINCH)을 c 로 받는다
func notifyResize(c chan<- os.Signal) { signal.Notify(c, syscall.SIGWINCH) }

// ignoreSuspend: Ctrl+Z(SIGTSTP)로 TUI 가 멈추지 않게 한다. 돌려주는 함수가 원래대로 되돌린다.
func ignoreSuspend() func() {
	signal.Ignore(syscall.SIGTSTP)
	return func() { signal.Reset(syscall.SIGTSTP) }
}

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

// waitReadable: d 안에 f 에서 읽을 수 있는 데이터(또는 EOF)가 생기면 true (select). EINTR 등 오류는 false.
func waitReadable(f *os.File, d time.Duration) bool {
	fd := int(f.Fd())
	var fds syscall.FdSet
	bits := uint(unsafe.Sizeof(fds.Bits[0]) * 8)
	if fd < 0 || fd >= len(fds.Bits)*int(bits) {
		return true // 범위 밖이면 그냥 Read 로 넘긴다
	}
	fds.Bits[uint(fd)/bits] |= 1 << (uint(fd) % bits)
	tv := syscall.NsecToTimeval(int64(d))
	n, err := syscall.Select(fd+1, &fds, nil, nil, &tv)
	return err == nil && n > 0
}

// ttySize: 터미널 칸 수 (실패하면 80x24)
func ttySize(f *os.File) (int, int) {
	var ws struct{ Row, Col, X, Y uint16 }
	if err := ioctl(f.Fd(), syscall.TIOCGWINSZ, unsafe.Pointer(&ws)); err != nil || ws.Row == 0 || ws.Col == 0 {
		return 80, 24
	}
	return int(ws.Col), int(ws.Row)
}
