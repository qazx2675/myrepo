//go:build windows

package main

import (
	"bufio"
	"io"
	"os"
	"strings"
	"syscall"
	"unsafe"
)

var kernel32 = syscall.NewLazyDLL("kernel32.dll")

// setupConsole 은 콘솔 출력 코드페이지를 UTF-8 로 바꿔 한글이 깨지지 않게 한다.
func setupConsole() {
	if p := kernel32.NewProc("SetConsoleOutputCP"); p.Find() == nil {
		_, _, _ = p.Call(65001)
	}
}

func consoleMode(h syscall.Handle) (uint32, bool) {
	var m uint32
	r, _, _ := kernel32.NewProc("GetConsoleMode").Call(uintptr(h), uintptr(unsafe.Pointer(&m)))
	return m, r != 0
}

// stdinIsTerminal 은 표준 입력이 콘솔인지 본다.
func stdinIsTerminal() bool {
	_, ok := consoleMode(syscall.Handle(os.Stdin.Fd()))
	return ok
}

// readPassword 는 콘솔 에코를 끄고 한 줄을 읽는다.
func readPassword(in *bufio.Reader, out io.Writer) (string, error) {
	h := syscall.Handle(os.Stdin.Fd())
	if m, ok := consoleMode(h); ok {
		set := kernel32.NewProc("SetConsoleMode")
		_, _, _ = set.Call(uintptr(h), uintptr(m&^0x0004)) // ENABLE_ECHO_INPUT 끔
		defer set.Call(uintptr(h), uintptr(m))
	}
	line, err := in.ReadString('\n')
	if err != nil && line == "" {
		return "", err
	}
	return strings.TrimRight(line, "\r\n"), nil
}
