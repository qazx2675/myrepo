//go:build windows

package main

import "syscall"

// setupConsole 은 콘솔 출력 코드페이지를 UTF-8 로 바꿔 한글이 깨지지 않게 한다.
func setupConsole() {
	k := syscall.NewLazyDLL("kernel32.dll")
	if p := k.NewProc("SetConsoleOutputCP"); p.Find() == nil {
		_, _, _ = p.Call(65001)
	}
}
