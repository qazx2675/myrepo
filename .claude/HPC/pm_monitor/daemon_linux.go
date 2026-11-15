//go:build linux

// daemon_linux.go - 데몬을 터미널에서 떼어 내고(setsid) 같은 호스트파일의 데몬이 둘 되지 않게 잠근다(flock).
package main

import (
	"os"
	"syscall"
)

// detachAttr 는 새 세션으로 시작해 화면(터미널)을 닫아도 데몬이 살아남게 한다.
func detachAttr() *syscall.SysProcAttr { return &syscall.SysProcAttr{Setsid: true} }

// lockExclusive 는 파일 잠금을 기다리지 않고 잡는다. 이미 잡혀 있으면 error 다.
// 프로세스가 끝나면(죽어도) 운영체제가 잠금을 풀어 준다.
func lockExclusive(path string) (release func(), err error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		return nil, err
	}
	return func() { f.Close() }, nil
}
