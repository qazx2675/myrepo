// hang_linux_test.go - gossh 멈춤 감시(hang.go) 테스트: 가짜 gossh(이 테스트 바이너리를 gossh 이름의 링크로 실행) 프로세스 트리에
// INT×2 → (반응 없으면) TERM, 무관한 gossh 는 건드리지 않음, os6 래퍼의 USR1/USR2 → os6_mgmt(가짜 ssh) gossh 취소, 2차 체크 감시
package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

// 가짜 gossh: AUTO_SETUP_FAKE_GOSSH 가 있으면 테스트 대신 이것만 실행한다 (Go 라 INT 가 무시된 채 시작돼도 signal.Notify 로 받는다 — 실제 gossh 와 같음).
// -w 목록의 호스트마다 "<host>: <디렉터리 이름> ok" 출력. hang 으로 시작하는 호스트가 있으면 멈춰 있다가 1초 안의 INT 두 번 →
// 그 호스트를 <목록>_res_cancel 에 적고 나머지 결과로 끝 (FAKE_GOSSH_IGNORE_INT 면 INT 는 기록만). TERM → 기록 후 143.
func init() {
	if os.Getenv("AUTO_SETUP_FAKE_GOSSH") != "" {
		os.Exit(fakeGosshMain())
	}
}

func fakeGosshMain() int {
	name := filepath.Base(filepath.Dir(os.Args[0]))
	lg := func(s string) {
		if f, err := os.OpenFile(os.Getenv("FAKE_GOSSH_LOG"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644); err == nil {
			fmt.Fprintln(f, s+" "+name)
			f.Close()
		}
	}
	sig := make(chan os.Signal, 8)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	list := ""
	for i, a := range os.Args {
		if a == "-w" && i+1 < len(os.Args) {
			list = os.Args[i+1]
		}
	}
	b, _ := os.ReadFile(list)
	var hang, ok []string
	for _, h := range strings.Fields(string(b)) {
		if strings.HasPrefix(h, "hang") {
			hang = append(hang, h)
		} else {
			ok = append(ok, h)
		}
	}
	lg("start")
	out := func() {
		for _, h := range ok {
			fmt.Printf("%s: %s ok\n", h, name)
		}
	}
	if len(hang) == 0 {
		out()
		return 0
	}
	var last time.Time
	for s := range sig {
		if s == syscall.SIGTERM {
			lg("TERM")
			return 143
		}
		lg("INT")
		if os.Getenv("FAKE_GOSSH_IGNORE_INT") != "" {
			continue
		}
		if !last.IsZero() && time.Since(last) < time.Second {
			os.WriteFile(list+"_res_cancel", []byte(strings.Join(hang, "\n")+"\n"), 0644)
			out()
			return 0
		}
		last = time.Now()
	}
	return 0
}

// fakeGossh: dir/gossh → 이 테스트 바이너리 링크
func fakeGossh(t *testing.T, dir string) string {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	os.MkdirAll(dir, 0755)
	p := filepath.Join(dir, "gossh")
	if err := os.Symlink(exe, p); err != nil {
		t.Fatal(err)
	}
	return p
}

func fakeEnv(logp string, extra ...string) []string {
	return append(append(os.Environ(), "AUTO_SETUP_FAKE_GOSSH=1", "FAKE_GOSSH_LOG="+logp), extra...)
}

func readFakeLog(p string) string {
	b, _ := os.ReadFile(p)
	return string(b)
}

// startTree: bash 부모 아래에서 명령 실행 (부모 = 감시 root, 별도 프로세스 그룹 — 끝나면 그룹째 정리)
func startTree(t *testing.T, env []string, stdout *bytes.Buffer, script string, args ...string) *exec.Cmd {
	t.Helper()
	cmd := exec.Command("bash", append([]string{"-c", script + "; rc=$?; exit $rc", "root"}, args...)...)
	cmd.Env = env
	cmd.Stdout = stdout
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		cmd.Wait()
	})
	return cmd
}

func waitExit(t *testing.T, cmd *exec.Cmd, d time.Duration) {
	t.Helper()
	ch := make(chan struct{})
	go func() { cmd.Wait(); close(ch) }() // 출력 복사까지 끝날 때까지
	select {
	case <-ch:
	case <-time.After(d):
		t.Fatal("프로세스가 끝나지 않음")
	}
}

// 체크스크립트 아래 멈춘 gossh: limit 넘으면 INT×2 → gossh 가 걸린 호스트를 취소하고 끝, 체크스크립트는 계속. 트리 밖 gossh 는 그대로.
func TestHangWatchCancelsHungGossh(t *testing.T) {
	needBash(t)
	setupDir(t)
	tmp := t.TempDir()
	g := fakeGossh(t, filepath.Join(tmp, "local"))
	list := filepath.Join(tmp, "hosts")
	os.WriteFile(list, []byte("a1\nhang1\n"), 0644)
	logp := filepath.Join(tmp, "log")
	other := exec.Command(g, "-w", list) // 같은 이름이지만 이 run 의 자손이 아닌 gossh (사용자가 띄운 것)
	other.Env = fakeEnv(filepath.Join(tmp, "other.log"))
	if err := other.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { other.Process.Kill(); other.Wait() }()

	var out bytes.Buffer
	cmd := startTree(t, fakeEnv(logp), &out, `"$1" -pm -w "$2" uptime; echo "after rc=$?"`, g, list)
	waitFor(t, "gossh 시작", func() bool { return strings.Contains(readFakeLog(logp), "start local") })
	w := newHangWatch("T1", cmd.Process.Pid, 300*time.Millisecond)
	stop, done := make(chan struct{}), make(chan struct{})
	go func() { w.run(stop, 50*time.Millisecond); close(done) }()
	waitExit(t, cmd, 5*time.Second)
	close(stop)
	<-done
	if lg := readFakeLog(logp); lg != "start local\nINT local\nINT local\n" {
		t.Fatalf("시그널 기록: %q", lg)
	}
	if s := out.String(); s != "a1: local ok\nafter rc=0\n" {
		t.Fatalf("출력(취소 후 계속): %q", s)
	}
	if b, _ := os.ReadFile(list + "_res_cancel"); string(b) != "hang1\n" {
		t.Fatalf("res_cancel: %q", b)
	}
	if lg := readFakeLog(filepath.Join(tmp, "other.log")); strings.Contains(lg, "INT") || strings.Contains(lg, "TERM") {
		t.Fatalf("무관한 gossh 에 시그널: %q", lg)
	}
	if lg, _ := os.ReadFile(logPath()); !strings.Contains(string(lg), "[!] gossh hang 감지: code=T1 pid=") {
		t.Fatalf("로그: %s", lg)
	}
}

// INT 에 반응하지 않는 gossh: INT×2 뒤 term 이 지나면 TERM
func TestHangWatchTermFallback(t *testing.T) {
	needBash(t)
	setupDir(t)
	tmp := t.TempDir()
	g := fakeGossh(t, filepath.Join(tmp, "local"))
	list := filepath.Join(tmp, "hosts")
	os.WriteFile(list, []byte("hang1\n"), 0644)
	logp := filepath.Join(tmp, "log")
	var out bytes.Buffer
	cmd := startTree(t, fakeEnv(logp, "FAKE_GOSSH_IGNORE_INT=1"), &out, `"$1" -w "$2" x; echo "after rc=$?"`, g, list)
	waitFor(t, "gossh 시작", func() bool { return strings.Contains(readFakeLog(logp), "start local") })
	w := newHangWatch("T2", cmd.Process.Pid, 200*time.Millisecond)
	w.term = 400 * time.Millisecond
	stop, done := make(chan struct{}), make(chan struct{})
	go func() { w.run(stop, 50*time.Millisecond); close(done) }()
	waitExit(t, cmd, 5*time.Second)
	close(stop)
	<-done
	if lg := readFakeLog(logp); lg != "start local\nINT local\nINT local\nTERM local\n" {
		t.Fatalf("시그널 기록: %q", lg)
	}
	if s := out.String(); s != "after rc=143\n" {
		t.Fatalf("출력: %q", s)
	}
}

// os6 래퍼 실행 환경: 이 서버 gossh(local)·os6_mgmt gossh(remote) 가짜, ssh 는 로컬 bash. 목록: a1(os8), b6·hang6(os6)
func wrapperHangRig(t *testing.T) (w, list, logp string, env []string) {
	t.Helper()
	needBash(t)
	setupDir(t)
	oldM, oldG := os6_mgmt, os6_gossh
	t.Cleanup(func() { os6_mgmt, os6_gossh = oldM, oldG })
	tmp := t.TempDir()
	lg := fakeGossh(t, filepath.Join(tmp, "local"))
	os6_mgmt, os6_gossh = "mgmtstub", fakeGossh(t, filepath.Join(tmp, "remote"))
	fakeBin := filepath.Join(tmp, "bin")
	os.MkdirAll(fakeBin, 0755)
	os.WriteFile(filepath.Join(fakeBin, "ssh"), []byte("#!/bin/bash\nwhile [ \"$1\" = -o ]; do shift 2; done\nshift\nexec bash -c \"$1\"\n"), 0755)
	os.MkdirAll(binDir(), 0755)
	w = filepath.Join(binDir(), "gossh")
	if err := os.WriteFile(w, []byte(os6WrapperScript(lg)), 0755); err != nil {
		t.Fatal(err)
	}
	list = filepath.Join(tmp, "targets.txt")
	os.WriteFile(list, []byte("a1\nb6\nhang6\n"), 0644)
	o6 := filepath.Join(tmp, "os6_hosts.txt")
	os.WriteFile(o6, []byte("b6\nhang6\n"), 0644)
	logp = filepath.Join(tmp, "log")
	env = fakeEnv(logp, "PATH="+fakeBin+string(os.PathListSeparator)+os.Getenv("PATH"), "AUTO_SETUP_OS6_HOSTS="+o6)
	return
}

// 래퍼에 USR1 → os6_mgmt 의 gossh(표지로 찾음)에 INT×2 → 걸린 호스트 취소, 출력·결과 파일은 종전처럼 합쳐짐
func TestOS6WrapperUSR1CancelsRemote(t *testing.T) {
	w, list, logp, env := wrapperHangRig(t)
	var out bytes.Buffer
	cmd := exec.Command("bash", w, "-pm", "-w", list, "uptime")
	cmd.Env, cmd.Stdout = env, &out
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "원격 gossh 시작", func() bool { return strings.Contains(readFakeLog(logp), "start remote") })
	time.Sleep(100 * time.Millisecond)
	syscall.Kill(cmd.Process.Pid, syscall.SIGUSR1)
	waitExit(t, cmd, 10*time.Second)
	if sortedLines(out.String()) != "a1: local ok,b6: remote ok" {
		t.Fatalf("출력:\n%s", out.String())
	}
	if b, _ := os.ReadFile(list + "_res_cancel"); string(b) != "hang6\n" {
		t.Fatalf("원격 res_cancel 회수: %q", b)
	}
	if lg := readFakeLog(logp); strings.Count(lg, "INT remote") != 2 || strings.Contains(lg, "INT local") {
		t.Fatalf("시그널 기록: %q", lg)
	}
}

// USR1 에도 끝나지 않으면(INT 무시) USR2 → os6_mgmt 의 gossh 에 TERM
func TestOS6WrapperUSR2TermsRemote(t *testing.T) {
	w, list, logp, env := wrapperHangRig(t)
	cmd := exec.Command("bash", w, "-pm", "-w", list, "uptime")
	cmd.Env = append(env, "FAKE_GOSSH_IGNORE_INT=1")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "원격 gossh 시작", func() bool { return strings.Contains(readFakeLog(logp), "start remote") })
	time.Sleep(100 * time.Millisecond)
	syscall.Kill(cmd.Process.Pid, syscall.SIGUSR1)
	waitFor(t, "INT 2회", func() bool { return strings.Count(readFakeLog(logp), "INT remote") == 2 })
	syscall.Kill(cmd.Process.Pid, syscall.SIGUSR2)
	waitExit(t, cmd, 10*time.Second)
	if lg := readFakeLog(logp); !strings.Contains(lg, "TERM remote") {
		t.Fatalf("시그널 기록: %q", lg)
	}
}

// 감시는 맨 위 래퍼에만 USR1/USR2(서브셸 제외), 실제 gossh(comm=gossh)에는 INT/TERM 을 보낸다
func TestHangWatchWrapperSignals(t *testing.T) {
	w, list, logp, env := wrapperHangRig(t)
	var out bytes.Buffer
	cmd := startTree(t, append(env, "FAKE_GOSSH_IGNORE_INT=1"), &out, `bash "$1" -pm -w "$2" uptime`, w, list)
	waitFor(t, "원격 gossh 시작", func() bool { return strings.Contains(readFakeLog(logp), "start remote") })
	hw := newHangWatch("T3", cmd.Process.Pid, 300*time.Millisecond)
	hw.term = 300 * time.Millisecond
	var mu sync.Mutex
	var sent []string
	hw.kill = func(pid int, sig syscall.Signal) error { // 보내지 않고 기록만 — 보낼 때의 대상 종류로 분류
		procs := readProcs("/proc")
		p := procs[pid]
		kind := "?"
		switch {
		case p == nil:
		case hw.isWrapper(p) && hw.isWrapper(procs[p.ppid]):
			kind = "subshell"
		case hw.isWrapper(p):
			kind = "wrapper"
		case p.comm == "gossh":
			kind = fmt.Sprintf("gossh%d", pid)
		}
		mu.Lock()
		defer mu.Unlock()
		sent = append(sent, kind+" "+fmt.Sprint(sig))
		return nil
	}
	for i := 0; i < 30; i++ {
		hw.scan()
		time.Sleep(50 * time.Millisecond)
	}
	mu.Lock()
	defer mu.Unlock()
	byKind := map[string][]string{}
	for _, s := range sent {
		f := strings.SplitN(s, " ", 2)
		byKind[f[0]] = append(byKind[f[0]], f[1])
	}
	if strings.Join(byKind["wrapper"], ",") != "user defined signal 1,user defined signal 2" || len(byKind["subshell"]) > 0 || len(byKind["?"]) > 0 {
		t.Fatalf("래퍼 시그널(맨 위 래퍼에만 USR1·USR2): %v", byKind)
	}
	// 멈춘 gossh(가짜 ssh 라 os6_mgmt 쪽 gossh 도 이 서버의 자손으로 보인다 — 실제로는 ssh 너머)는 INT·INT·TERM.
	// 이 서버 gossh(a1)는 바로 끝나지만 느린 환경(-race)에서는 limit 을 넘겨 INT 를 받을 수 있다
	hung := 0
	for k, v := range byKind {
		if !strings.HasPrefix(k, "gossh") {
			continue
		}
		switch strings.Join(v, ",") {
		case "interrupt,interrupt,terminated":
			hung++
		case "interrupt,interrupt":
		default:
			t.Fatalf("gossh 시그널: %v", byKind)
		}
	}
	if hung != 1 {
		t.Fatalf("멈춘 gossh 시그널: %v", byKind)
	}
}

// 2차 체크 감시: 표지를 가진 os6_mgmt(가짜 ssh = 로컬) gossh 가 limit 을 넘기면 INT×2, 반응 없으면 TERM. 표지 없는 gossh 는 그대로.
func TestSecondHangWatch(t *testing.T) {
	needBash(t)
	secondEnv(t, "exit 0\n")
	tmp := t.TempDir()
	g := fakeGossh(t, filepath.Join(tmp, "remote"))
	list := filepath.Join(tmp, "hosts")
	os.WriteFile(list, []byte("hang1\n"), 0644)
	kv := "AUTO_SETUP_2ND=c0001.1.2"
	start := func(logp string, extra ...string) *exec.Cmd {
		c := exec.Command(g, "-w", list)
		c.Env = fakeEnv(logp, extra...)
		if err := c.Start(); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { c.Process.Kill(); c.Wait() })
		return c
	}
	logA, logB, logC := filepath.Join(tmp, "a"), filepath.Join(tmp, "b"), filepath.Join(tmp, "c")
	a := start(logA, kv)                            // INT×2 에 취소
	b := start(logB, kv, "FAKE_GOSSH_IGNORE_INT=1") // TERM 까지
	start(logC, "AUTO_SETUP_2ND=c9999.1.2")         // 다른 run
	waitFor(t, "gossh 시작", func() bool { return readFakeLog(logA) != "" && readFakeLog(logB) != "" && readFakeLog(logC) != "" })
	stopW := secondHangWatch("0001", kv, time.Second, 1500*time.Millisecond, 300*time.Millisecond)
	waitExit(t, a, 10*time.Second)
	waitExit(t, b, 10*time.Second)
	stopW()
	if lg := readFakeLog(logA); lg != "start remote\nINT remote\nINT remote\n" {
		t.Fatalf("a: %q", lg)
	}
	if lg := readFakeLog(logB); lg != "start remote\nINT remote\nINT remote\nTERM remote\n" {
		t.Fatalf("b: %q", lg)
	}
	if lg := readFakeLog(logC); lg != "start remote\n" {
		t.Fatalf("다른 run 의 gossh 에 시그널: %q", lg)
	}
}

func TestParseEtime(t *testing.T) {
	for in, want := range map[string]int64{"00:05": 5, " 07:01": 421, "1:02:03": 3723, "2-00:00:01": 172801, "x": -1, "1:x": -1, "5": -1} {
		if got := parseEtime(in); got != want {
			t.Errorf("%q: %d, 기대 %d", in, got, want)
		}
	}
}
