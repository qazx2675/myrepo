// ensure.go - flock 중복 방지(+lock 교체·중복 데몬 감지), ensure(데몬 없으면 setsid 백그라운드 기동)
package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
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

// ---- 단일 인스턴스 보강 (flock 은 lock 파일이 지워졌다 다시 만들어지면 두 번째 데몬을 막지 못한다) ----

// lockHeldCheck: 데몬이 쥔 lock fd 가 아직 유효한지 확인하는 함수를 만든다. 문제가 있으면 사유, 정상이면 "".
//   - lock 파일 경로의 inode 가 쥐고 있는 fd 의 inode 와 다름(삭제·재생성) → 다른 데몬이 새 lock 을 잡을 수 있음
//   - pid 파일의 pid 가 자기 pid 가 아님 → 다른 데몬이 pid 파일을 덮어씀 (pid 파일이 없으면 다시 기록)
func lockHeldCheck(lock *os.File) func() string {
	return func() string {
		held, err := lock.Stat()
		if err != nil {
			return fmt.Sprintf("lock fd 확인 실패: %v", err)
		}
		cur, err := os.Stat(lockPath())
		if err != nil {
			return fmt.Sprintf("lock 파일 없음(%v)", err)
		}
		if !os.SameFile(held, cur) {
			return "lock 파일이 교체됨(inode 다름)"
		}
		pid, _, err := readPid()
		if err != nil {
			if os.IsNotExist(err) {
				_ = writePid() // pid 파일만 지워진 경우: 자기 것으로 복구
				return ""
			}
			return fmt.Sprintf("pid 파일 읽기 실패: %v", err)
		}
		if pid != os.Getpid() {
			return fmt.Sprintf("pid 파일이 다른 데몬(pid=%d)의 것", pid)
		}
		return ""
	}
}

// findDaemonPids: procRoot(/proc) 에서 dir 을 데이터 디렉터리로 쓰는 `auto_setup daemon` 프로세스 pid (self 제외, 오름차순).
// 데이터 디렉터리는 /proc/<pid>/environ 의 AUTO_SETUP_DIR (없으면 기본값). environ 을 읽을 수 없으면(권한) 건너뛴다.
// procRoot 가 없으면 nil.
func findDaemonPids(procRoot, dir string, self int) []int {
	ents, err := os.ReadDir(procRoot)
	if err != nil {
		return nil
	}
	want := filepath.Clean(dir)
	var out []int
	for _, e := range ents {
		pid, err := strconv.Atoi(e.Name())
		if err != nil || pid <= 0 || pid == self {
			continue
		}
		pd := filepath.Join(procRoot, e.Name())
		cmd, err := os.ReadFile(filepath.Join(pd, "cmdline"))
		if err != nil {
			continue
		}
		args := strings.Split(strings.TrimRight(string(cmd), "\x00"), "\x00")
		if len(args) < 2 || args[1] != "daemon" || !strings.HasPrefix(filepath.Base(args[0]), "auto_setup") {
			continue
		}
		env, err := os.ReadFile(filepath.Join(pd, "environ"))
		if err != nil {
			continue
		}
		got := defaultDataDir
		for _, kv := range strings.Split(string(env), "\x00") {
			if strings.HasPrefix(kv, "AUTO_SETUP_DIR=") {
				if v := strings.TrimPrefix(kv, "AUTO_SETUP_DIR="); v != "" {
					got = v
				}
			}
		}
		if filepath.Clean(got) == want {
			out = append(out, pid)
		}
	}
	sort.Ints(out)
	return out
}

// stopStrayDaemons: 같은 데이터 디렉터리를 쓰는 다른 데몬(lock 이 교체돼 살아남은 옛 데몬)에 SIGTERM. 보낸 pid 를 로그에 남기고 반환.
func stopStrayDaemons(procRoot, dir string, self int) []int {
	var sent []int
	for _, pid := range findDaemonPids(procRoot, dir, self) {
		if err := syscall.Kill(pid, syscall.SIGTERM); err != nil {
			logf("[X] 다른 데몬 종료 실패(pid=%d): %v", pid, err)
			continue
		}
		logf("[!] 같은 디렉터리를 쓰는 다른 데몬 pid=%d 에 SIGTERM", pid)
		sent = append(sent, pid)
	}
	return sent
}
