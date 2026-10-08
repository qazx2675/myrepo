//go:build linux

// tty_linux.go - TUI 용 raw 터미널(termios ioctl TCGETS/TCSETS)·창 크기(TIOCGWINSZ)·tty 판별. 표준 라이브러리 syscall 만 사용(정적 빌드·RHEL6 호환)
package main

import (
	"os"
	"syscall"
	"time"
	"unsafe"
)

// tcsetsf: TCSETSF(0x5412, x86·arm 공통) - syscall 패키지에 상수가 없다. 출력 대기 후 적용하며 입력 버퍼를 버린다
const tcsetsf = 0x5412

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

// withAwxTermios: AWX(01) 실행용 터미널 설정 — 현재 termios 를 저장하고 cooked(ISIG|ICANON|ECHO …) 로 바꾼 뒤
// intr=Ctrl+X(0x18), susp=비활성(_POSIX_VDISABLE=0) 로 설정한다. 돌려주는 함수가 저장해 둔 termios 로 되돌린다
// (입력 버퍼에 남은 키는 버림 - TCSETSF).
func withAwxTermios(f *os.File) (func(), error) {
	fd := f.Fd()
	var old syscall.Termios
	if err := ioctl(fd, syscall.TCGETS, unsafe.Pointer(&old)); err != nil {
		return nil, err
	}
	t := old
	t.Iflag |= syscall.ICRNL | syscall.IXON
	t.Oflag |= syscall.OPOST | syscall.ONLCR
	t.Lflag |= syscall.ISIG | syscall.ICANON | syscall.ECHO | syscall.ECHOE | syscall.ECHOK | syscall.IEXTEN
	t.Cc[syscall.VINTR] = 0x18
	t.Cc[syscall.VSUSP] = 0
	if err := ioctl(fd, syscall.TCSETS, unsafe.Pointer(&t)); err != nil {
		return nil, err
	}
	return func() { _ = ioctl(fd, tcsetsf, unsafe.Pointer(&old)) }, nil
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

// oneoffSysProcAttr: 단독 실행 프로세스를 새 프로세스 그룹으로
func oneoffSysProcAttr() *syscall.SysProcAttr { return &syscall.SysProcAttr{Setpgid: true} }

// killGroup: 프로세스 그룹 전체에 시그널
func killGroup(pid int, sig syscall.Signal) { _ = syscall.Kill(-pid, sig) }

// ttySize: 터미널 칸 수 (실패하면 80x24)
func ttySize(f *os.File) (int, int) {
	var ws struct{ Row, Col, X, Y uint16 }
	if err := ioctl(f.Fd(), syscall.TIOCGWINSZ, unsafe.Pointer(&ws)); err != nil || ws.Row == 0 || ws.Col == 0 {
		return 80, 24
	}
	return int(ws.Col), int(ws.Row)
}
