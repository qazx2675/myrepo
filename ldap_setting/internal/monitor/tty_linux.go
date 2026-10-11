//go:build linux

package monitor

import (
	"os"
	"os/exec"
	"syscall"
	"unsafe"
)

func ioctl(fd uintptr, req uintptr, arg unsafe.Pointer) error {
	if _, _, e := syscall.Syscall(syscall.SYS_IOCTL, fd, req, uintptr(arg)); e != 0 {
		return e
	}
	return nil
}

// makeRaw 는 터미널을 raw 모드로 바꾸고 원래대로 돌리는 함수를 돌려줍니다
// (golang.org/x/term 의 MakeRaw 와 같은 설정 — 외부 의존성 없이 표준 라이브러리로).
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

// ttyRows 는 터미널 세로 줄 수를 돌려줍니다(모르면 24).
func ttyRows(f *os.File) int {
	var ws struct{ Row, Col, X, Y uint16 }
	if err := ioctl(f.Fd(), syscall.TIOCGWINSZ, unsafe.Pointer(&ws)); err != nil || ws.Row == 0 {
		return 24
	}
	return int(ws.Row)
}

func setProcessGroup(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true
}

// killProcessGroup 은 gossh 와 그 자식들(프로세스 그룹 전체)을 끝냅니다.
func killProcessGroup(pid int) {
	_ = syscall.Kill(-pid, syscall.SIGTERM)
}
