//go:build linux

package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
)

// setSetsid 는 워커를 새 세션으로 띄운다(터미널이 끊겨도 SIGHUP 을 받지 않음).
func setSetsid(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}

// errLocked 는 다른 워커가 이미 lock 을 잡고 있다는 뜻이다.
var errLocked = errors.New("다른 워커가 이미 실행 중입니다")

// lockState 는 <dir>/lock 에 flock(배타, 비차단)을 건다. 프로세스가 끝나면 커널이 풀어 준다.
func lockState(dir string) (func(), error) {
	f, err := os.OpenFile(filepath.Join(dir, lockFile), os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		if err == syscall.EWOULDBLOCK {
			return nil, errLocked
		}
		return nil, err
	}
	return func() {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		f.Close()
	}, nil
}

// workerAlive 는 그 상태 폴더의 워커가 살아 있는지(lock 이 잡혀 있는지) 본다.
func workerAlive(dir string) bool {
	f, err := os.Open(filepath.Join(dir, lockFile))
	if err != nil {
		return false
	}
	defer f.Close()
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_SH|syscall.LOCK_NB); err != nil {
		return err == syscall.EWOULDBLOCK
	}
	_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	return false
}
