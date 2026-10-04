//go:build !windows

// hpcbot — 로그 파일 잠금 (유닉스: flock)
package main

import (
	"os"
	"syscall"
)

// lockFile 은 잠금 파일(0666)을 열고 LOCK_EX 를 건다. 반환 함수로 해제한다.
func lockFile(path string) (func(), error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o666)
	if err != nil {
		return nil, err
	}
	_ = f.Chmod(0o666)
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		f.Close()
		return nil, err
	}
	return func() {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		f.Close()
	}, nil
}
