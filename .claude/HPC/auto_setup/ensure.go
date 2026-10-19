// ensure.go - flock 중복 방지, ensure(데몬 없으면 setsid 백그라운드 기동)
package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
)

// withStdPath: cron 최소 PATH(/usr/bin:/bin)에서도 gossh(/usr/local/bin)·ssh·wall 을 찾도록
// 표준 경로 중 빠진 것만 뒤에 붙인다 (기존 순서 유지)
func withStdPath(p string) string {
	have := map[string]bool{}
	for _, d := range filepath.SplitList(p) {
		have[d] = true
	}
	for _, d := range []string{"/usr/local/sbin", "/usr/local/bin", "/usr/sbin", "/usr/bin", "/sbin", "/bin"} {
		if !have[d] {
			if p != "" {
				p += ":"
			}
			p += d
		}
	}
	return p
}

// tryLock: auto_setup.lock 에 LOCK_EX|LOCK_NB. 잡히면 (file, true), 이미 잡혀 있으면 (nil, false).
func tryLock() (*os.File, bool, error) {
	f, err := os.OpenFile(lockPath(), os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		return nil, false, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		if err == syscall.EWOULDBLOCK {
			return nil, false, nil
		}
		return nil, false, err
	}
	return f, true, nil
}

func runEnsure() int {
	if err := ensureDirs(); err != nil {
		logf("[X] ensure: 디렉터리 생성 실패: %v", err)
		return 1
	}
	f, ok, err := tryLock()
	if err != nil {
		logf("[X] ensure: lock 실패: %v", err)
		return 1
	}
	if !ok {
		return 0 // 살아 있음
	}
	f.Close() // 해제 후 데몬이 다시 잡는다

	exe, err := os.Executable()
	if err != nil {
		exe = os.Args[0]
	}
	cmd := exec.Command(exe, "daemon")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		logf("[X] ensure: 데몬 기동 실패: %v", err)
		return 1
	}
	logf("ensure: 데몬 기동 pid=%d", cmd.Process.Pid)
	_ = cmd.Process.Release()
	return 0
}
