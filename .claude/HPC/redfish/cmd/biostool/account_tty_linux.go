//go:build linux

package main

import (
	"os"
	"syscall"
	"time"
	"unsafe"
)

// ttyIsTerminal 은 fd 가 터미널인지 봅니다 (termios 를 읽을 수 있으면 터미널).
func ttyIsTerminal(fd int) bool {
	var t syscall.Termios
	_, _, e := syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), uintptr(syscall.TCGETS), uintptr(unsafe.Pointer(&t)))
	return e == 0
}

// ttyMakeRaw 는 에코와 줄 단위 입력(ICANON)을 끄고 되돌리는 함수를 돌려줍니다 (ISIG 는 유지: Ctrl-C 가 동작).
func ttyMakeRaw(fd int) (func(), error) {
	var old syscall.Termios
	if _, _, e := syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), uintptr(syscall.TCGETS), uintptr(unsafe.Pointer(&old))); e != 0 {
		return nil, e
	}
	nt := old
	nt.Lflag &^= syscall.ECHO | syscall.ICANON | syscall.ECHONL
	nt.Cc[syscall.VMIN] = 1
	nt.Cc[syscall.VTIME] = 0
	if _, _, e := syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), uintptr(syscall.TCSETS), uintptr(unsafe.Pointer(&nt))); e != 0 {
		return nil, e
	}
	return func() {
		syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), uintptr(syscall.TCSETS), uintptr(unsafe.Pointer(&old)))
	}, nil
}

// ttyFlushInput 은 아직 읽지 않은 터미널 입력을 버립니다 (TCFLSH, TCIFLUSH). 실패해도 무시합니다.
func ttyFlushInput(fd int) {
	const tcflsh, tciflush = 0x540B, 0
	syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), uintptr(tcflsh), uintptr(tciflush))
}

// drainPending 은 터미널에 남아 있는 입력을 비웁니다 (붙여넣기 잔여분). 읽을 것이 없으면 바로 돌아옵니다.
func drainPending(in *os.File) {
	fd := int(in.Fd())
	var old syscall.Termios
	if _, _, e := syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), uintptr(syscall.TCGETS), uintptr(unsafe.Pointer(&old))); e != 0 {
		return
	}
	nt := old
	nt.Cc[syscall.VMIN] = 0
	nt.Cc[syscall.VTIME] = 1 // 0.1초
	if _, _, e := syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), uintptr(syscall.TCSETS), uintptr(unsafe.Pointer(&nt))); e != 0 {
		return
	}
	defer syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), uintptr(syscall.TCSETS), uintptr(unsafe.Pointer(&old)))
	buf := make([]byte, 512)
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		n, _ := in.Read(buf)
		if n == 0 {
			return
		}
	}
}
