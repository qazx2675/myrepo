//go:build linux

package remote

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"unsafe"
)

func ioctl(fd uintptr, req uintptr, arg unsafe.Pointer) error {
	if _, _, e := syscall.Syscall(syscall.SYS_IOCTL, fd, req, uintptr(arg)); e != 0 {
		return e
	}
	return nil
}

// ReadPassword 는 /dev/tty 에 안내문을 보이고 에코 없이 한 줄을 읽습니다.
// (golang.org/x/term 없이 표준 라이브러리만 씁니다)
func ReadPassword(prompt string) (string, error) {
	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return "", errors.New("터미널(/dev/tty)을 열 수 없습니다")
	}
	defer tty.Close()

	fd := tty.Fd()
	var old syscall.Termios
	if err := ioctl(fd, syscall.TCGETS, unsafe.Pointer(&old)); err != nil {
		return "", errors.New("터미널 설정을 읽을 수 없습니다")
	}
	t := old
	t.Lflag &^= syscall.ECHO
	if err := ioctl(fd, syscall.TCSETS, unsafe.Pointer(&t)); err != nil {
		return "", errors.New("터미널 에코를 끌 수 없습니다")
	}
	restore := func() { _ = ioctl(fd, syscall.TCSETS, unsafe.Pointer(&old)) }
	defer restore()

	// 입력 중 Ctrl+C 로 끝나더라도 에코를 되돌립니다.
	sigc := make(chan os.Signal, 1)
	signal.Notify(sigc, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		if _, ok := <-sigc; ok {
			restore()
			fmt.Fprintln(tty)
			os.Exit(130)
		}
	}()
	defer func() { signal.Stop(sigc); close(sigc) }()

	fmt.Fprint(tty, prompt)
	line, rerr := bufio.NewReader(tty).ReadString('\n')
	fmt.Fprintln(tty)
	if rerr != nil && line == "" {
		return "", rerr
	}
	pw := strings.TrimRight(line, "\r\n")
	if pw == "" {
		return "", errors.New("빈 패스워드입니다")
	}
	return pw, nil
}
