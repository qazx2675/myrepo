//go:build linux

// os6run_detach_linux_test.go - os6_mgmt 직접 실행: 체크스크립트가 TUI 와 다른 세션(제어 터미널 없음)에서 돌아 터미널 SIGHUP 을 받지 않는다
package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

// 데몬용 Runner(detach=false)는 종전대로 같은 세션, os6 직접 실행 Runner(detach=true)는 새 세션
func TestRunnerDetachNewSession(t *testing.T) {
	needBash(t)
	setupDir(t)
	oldC := os_check_sh
	t.Cleanup(func() { os_check_sh = oldC })
	tmp := t.TempDir()
	sh := filepath.Join(tmp, "oc.sh")
	os.WriteFile(sh, []byte("#!/bin/bash\nawk '{print $6}' /proc/$$/stat > sid.txt\n"), 0755) // stat 6번째 필드 = session id
	st, _ := os.ReadFile("/proc/self/stat")
	mySid, _ := strconv.Atoi(strings.Fields(string(st[strings.LastIndex(string(st), ")")+2:]))[3]) // ) 뒤: state ppid pgrp session
	for _, detach := range []bool{false, true} {
		res, _ := realRunner{osCheck: sh, detach: detach}.Run(&Job{ID: "j1", User: "user1"}, []string{"host01"}, false)
		b, err := os.ReadFile(filepath.Join(runsDir(), res.Code, "sid.txt"))
		if err != nil {
			t.Fatal(err)
		}
		sid, _ := strconv.Atoi(strings.TrimSpace(string(b)))
		if detach == (sid == mySid) {
			t.Fatalf("detach=%v: 스크립트 세션 %d, 테스트 세션 %d", detach, sid, mySid)
		}
	}
}

// TestHupHelper: TestHupDuringDirectRun 이 pty(script) 안에서 다시 실행하는 쪽 — TUI 처럼 SIGHUP 을 받아 두고(Notify) 체크스크립트를 돌린다
func TestHupHelper(t *testing.T) {
	dir := os.Getenv("AS_HUP_DIR")
	if dir == "" {
		t.Skip("TestHupDuringDirectRun 에서만 실행")
	}
	setupDir(t)
	hup := make(chan os.Signal, 4)
	signal.Notify(hup, syscall.SIGHUP)
	sh := filepath.Join(dir, "oc.sh")
	_, err := realRunner{osCheck: sh, detach: os.Getenv("AS_HUP_DETACH") == "1"}.Run(&Job{ID: "j1", User: "user1"}, []string{"host01"}, false)
	got := "nohup"
	select {
	case <-hup:
		got = "hup"
	default:
	}
	os.WriteFile(filepath.Join(dir, "helper.out"), []byte(got+"\n"+fmt.Sprint(err)+"\n"), 0644)
}

// 터미널(pty)이 끊겨 SIGHUP 이 가도 detach 한 체크스크립트는 끝까지 돌고, detach 하지 않으면(종전) 죽는다
func TestHupDuringDirectRun(t *testing.T) {
	needBash(t)
	if _, err := exec.LookPath("script"); err != nil {
		t.Skip("script 없음")
	}
	for _, detach := range []string{"0", "1"} {
		dir := t.TempDir()
		os.WriteFile(filepath.Join(dir, "oc.sh"), []byte("#!/bin/bash\nsleep 2\ntouch '"+dir+"/finished'\n"), 0755)
		cmd := exec.Command("script", "-qec", "bash -c '"+os.Args[0]+" -test.run=TestHupHelper -test.count=1; true'", "/dev/null") // 세션 리더는 bash, 헬퍼는 그 포그라운드 자식 (실제 TUI 와 같은 위치)
		cmd.Env = append(os.Environ(), "AS_HUP_DIR="+dir, "AS_HUP_DETACH="+detach)
		cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
		cmd.Stdin = strings.NewReader("")
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		time.Sleep(1 * time.Second)
		cmd.Process.Kill() // script 가 죽으면 pty master 가 닫혀 포그라운드 프로세스 그룹에 SIGHUP
		cmd.Wait()
		time.Sleep(3 * time.Second)
		_, ferr := os.Stat(filepath.Join(dir, "finished"))
		if (detach == "1") != (ferr == nil) {
			t.Errorf("detach=%s: finished 파일 있음=%v", detach, ferr == nil)
		}
		if detach == "1" {
			if b, _ := os.ReadFile(filepath.Join(dir, "helper.out")); !strings.HasPrefix(string(b), "hup\n") {
				t.Errorf("helper(TUI 역할)가 SIGHUP 을 받고도 살아 끝나야 함: %q", b)
			}
		}
	}
}

// brokenOut: 끊긴 터미널 — 쓰기가 EIO 로 실패한다 (패닉·멈춤 없이 지나가야 함)
type brokenOut struct{ broken atomic.Bool }

func (b *brokenOut) Write(p []byte) (int, error) {
	if b.broken.Load() {
		return 0, syscall.EIO
	}
	return len(p), nil
}

// 직접 실행 중 SIGHUP(+끊긴 tty 의 출력 오류·입력 끝)이 와도 TUI 는 실행이 끝날 때까지 살아 기록·완료기록을 남기고 잠금을 지운 뒤 종료한다
func TestOS6TUIDirectRunSurvivesHUP(t *testing.T) {
	setupDir(t)
	dir := withOS6Direct(t, t.TempDir())
	f := &os6FakeRunner{dir: dir, started: make(chan struct{}), release: make(chan struct{})}
	newOS6Runner = func(string) Runner { return f }
	src := &fakeSrc{snap: twoGroupSnap()}
	pr, pw := io.Pipe()
	env, _ := newRig(pr)
	out := &brokenOut{}
	env.Out = out
	sig := make(chan os.Signal, 1)
	env.Signals = sig
	done := make(chan int, 1)
	go func() { done <- runTUILoop(src, env) }()
	pw.Write([]byte("\rcy"))
	select {
	case <-f.started:
	case <-time.After(5 * time.Second):
		t.Fatal("직접 실행이 시작되지 않음")
	}
	out.broken.Store(true) // 터미널이 닫힘: 출력 실패 → SIGHUP → 입력 끝
	sig <- syscall.SIGHUP
	pw.Close()
	select {
	case <-done:
		t.Fatal("SIGHUP 으로 실행 중에 TUI 가 종료됨")
	case <-time.After(300 * time.Millisecond):
	}
	close(f.release)
	select {
	case code := <-done:
		if code != 0 {
			t.Fatalf("exit=%d", code)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("실행이 끝났는데 종료하지 않음")
	}
	if m, _ := filepath.Glob(filepath.Join(dir, "active", "*")); len(m) != 0 {
		t.Fatalf("잠금이 남음: %v", m)
	}
	if m, _ := filepath.Glob(filepath.Join(dir, "done", "a*")); len(m) != 2 {
		t.Fatalf("완료기록 %v", m)
	}
	if m, _ := filepath.Glob(filepath.Join(dir, "runs", "*.json")); len(m) != 1 {
		t.Fatalf("run 기록 %v", m)
	}
}
