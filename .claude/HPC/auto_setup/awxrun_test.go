// awxrun_test.go - w AWX 실행: 안내 메시지, 프로파일 y/n/번호 선택, 환경변수, 취소 메시지, 키 읽기 정지 (가짜 실행기)
package main

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

type awxCall struct {
	n   int
	dir string
	env []string
	rc  int
}

// useFakeAwx: awx_dir 에 빈 01 스크립트를 두고 awxExec 를 가짜로 교체
func useFakeAwx(t *testing.T, rc int, profiles ...string) *awxCall {
	t.Helper()
	saveConfVars(t)
	oldDir, oldExec := awx_dir, awxExec
	t.Cleanup(func() { awx_dir, awxExec = oldDir, oldExec })
	awx_dir = t.TempDir()
	os.WriteFile(filepath.Join(awx_dir, awxScript), []byte("#!/bin/bash\n"), 0755)
	setProfiles(t, profiles...)
	c := &awxCall{rc: rc}
	awxExec = func(dir string, env []string) (int, error) {
		c.n++
		c.dir, c.env = dir, env
		return c.rc, nil
	}
	return c
}

func (c *awxCall) has(prefix string) bool {
	for _, e := range c.env {
		if strings.HasPrefix(e, prefix) {
			return true
		}
	}
	return false
}

func (c *awxCall) val(key string) string {
	for _, e := range c.env {
		if strings.HasPrefix(e, key+"=") {
			return e[len(key)+1:]
		}
	}
	return ""
}

func TestAwxMissingDirOrScript(t *testing.T) {
	c := useFakeAwx(t, 0)
	want := "awx_dir 가 비었거나 01.AWX_nodeinfo_V2.sh 가 없습니다 (/etc/auto_setup/auto_setup.conf)"
	awx_dir = ""
	r := runKeys(t, &fakeSrc{snap: twoGroupSnap(), local: true}, "w")
	if f := lastFrame(r); !strings.Contains(f, want) {
		t.Errorf("awx_dir 빈 값 안내 없음:\n%s", f)
	}
	awx_dir = t.TempDir() // 01 없음
	r = runKeys(t, &fakeSrc{snap: twoGroupSnap(), local: true}, "w")
	if f := lastFrame(r); !strings.Contains(f, want) {
		t.Errorf("01 없음 안내 없음:\n%s", f)
	}
	if c.n != 0 {
		t.Errorf("실행되면 안 됨: %d", c.n)
	}
}

func TestAwxProfileYesPickEnv(t *testing.T) {
	c := useFakeAwx(t, 0, "n|2024|첫째", "y|2025|둘째", "bad|x|y")
	r := runKeys(t, &fakeSrc{snap: twoGroupSnap(), local: true}, "wy2")
	o := r.out.String()
	for _, want := range []string{"AWX auto 실행? [y/n]", "yml 마다 OS 버전이 다르면 N 을 입력하세요",
		"1) nodeinfo=n  OS=2024  첫째", "2) nodeinfo=y  OS=2025  둘째", "awx_profile_3", "AWX 종료 (rc=0)"} {
		if !strings.Contains(o, want) {
			t.Errorf("화면에 %q 없음", want)
		}
	}
	if c.n != 1 || c.dir != awx_dir {
		t.Fatalf("n=%d dir=%q", c.n, c.dir)
	}
	if c.val("AWX_AUTO") != "1" || c.val("AWX_AUTO_NODEINFO") != "y" || c.val("AWX_AUTO_OS") != "2025" {
		t.Errorf("env: AWX_AUTO=%q NODEINFO=%q OS=%q", c.val("AWX_AUTO"), c.val("AWX_AUTO_NODEINFO"), c.val("AWX_AUTO_OS"))
	}
	if r.raws != 2 || r.restored != 2 { // raw 진입 2회(시작·AWX 후 재진입), 복원 2회(AWX 시작 전·종료 때)
		t.Errorf("raw=%d restored=%d", r.raws, r.restored)
	}
	if !strings.HasSuffix(o, leaveScreen) || strings.Count(o, enterScreen) != 2 {
		t.Errorf("대체 화면 진입/복귀 시퀀스 이상 (enter=%d)", strings.Count(o, enterScreen))
	}
	if !strings.Contains(o, awxStartLine) {
		t.Errorf("시작 안내 없음")
	}
}

func TestAwxProfileNoRunsPlain(t *testing.T) {
	t.Setenv("AWX_AUTO", "")
	c := useFakeAwx(t, 3, "y|2025|a")
	r := runKeys(t, &fakeSrc{snap: twoGroupSnap(), local: true}, "wn")
	if c.n != 1 || c.has("AWX_AUTO=1") || c.has("AWX_AUTO_NODEINFO") || c.has("AWX_AUTO_OS") {
		t.Errorf("n=%d AWX_* 환경변수가 있으면 안 됨", c.n)
	}
	if f := lastFrame(r); !strings.Contains(f, "AWX 종료 (rc=3)") {
		t.Errorf("종료 메시지 없음:\n%s", f)
	}
}

func TestAwxNoProfilesNoPrompt(t *testing.T) {
	t.Setenv("AWX_AUTO", "")
	c := useFakeAwx(t, 0)
	r := runKeys(t, &fakeSrc{snap: twoGroupSnap(), local: true}, "w")
	if c.n != 1 || c.has("AWX_AUTO=1") {
		t.Errorf("n=%d 바로 일반 실행이어야 함", c.n)
	}
	if strings.Contains(r.out.String(), "AWX auto 실행?") {
		t.Errorf("프로파일이 없으면 묻지 않아야 함")
	}
}

func TestAwxInterruptedMessage(t *testing.T) {
	useFakeAwx(t, 130)
	r := runKeys(t, &fakeSrc{snap: twoGroupSnap(), local: true}, "w")
	if f := lastFrame(r); !strings.Contains(f, "AWX 취소됨 (Ctrl+X)") {
		t.Errorf("취소 메시지 없음:\n%s", f)
	}
}

func TestAwxEscCancelsPrompt(t *testing.T) {
	c := useFakeAwx(t, 0, "y|2025|a")
	// ask 에서 Esc → 취소, 다시 w y Esc → 뒤로(ask), Esc → 취소
	r := runKeys(t, &fakeSrc{snap: twoGroupSnap(), local: true}, "w\x1b"+"wy\x1b\x1b")
	if c.n != 0 {
		t.Errorf("실행되면 안 됨: %d", c.n)
	}
	if f := lastFrame(r); !strings.Contains(f, "취소했습니다") || strings.Contains(f, "AWX auto 실행?") {
		t.Errorf("취소 후 화면:\n%s", f)
	}
}

func TestAwxPickIgnoresInvalidNumber(t *testing.T) {
	c := useFakeAwx(t, 0, "y|2025|a")
	runKeys(t, &fakeSrc{snap: twoGroupSnap(), local: true}, "wy5\x1b\x1b")
	if c.n != 0 {
		t.Errorf("없는 번호로 실행되면 안 됨")
	}
}

// gateIn: AWX 실행(inExec) 동안 Read 가 불리면 bad — 키 읽기 goroutine 이 멈춰 있는지 확인하는 가짜 입력
type gateIn struct {
	mu     sync.Mutex
	data   []byte
	inExec bool
	bad    bool
}

func (g *gateIn) ready(time.Duration) bool {
	time.Sleep(time.Millisecond)
	g.mu.Lock()
	defer g.mu.Unlock()
	return len(g.data) > 0
}

func (g *gateIn) Read(p []byte) (int, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.inExec {
		g.bad = true
	}
	n := copy(p, g.data)
	g.data = g.data[n:]
	return n, nil
}

func TestAwxPausesKeyReader(t *testing.T) {
	c := useFakeAwx(t, 0)
	g := &gateIn{data: []byte("w")}
	env, r := newRig(g)
	env.InReady = g.ready
	preps := 0
	env.AwxPrep = func() (func(), error) { preps++; return func() { preps += 10 }, nil }
	awxExec = func(dir string, e []string) (int, error) {
		g.mu.Lock()
		g.inExec = true
		g.data = append(g.data, 'q') // 자식이 실행되는 동안 입력된 키 - auto_setup 이 가로채면 안 됨
		g.mu.Unlock()
		time.Sleep(60 * time.Millisecond)
		g.mu.Lock()
		g.inExec = false
		g.mu.Unlock()
		c.n++
		return 0, nil
	}
	if code := runTUILoop(&fakeSrc{snap: twoGroupSnap(), local: true}, env); code != 0 {
		t.Fatalf("exit=%d", code)
	}
	if g.bad {
		t.Errorf("AWX 실행 중에 키 읽기 goroutine 이 입력을 읽음")
	}
	if c.n != 1 || preps != 11 {
		t.Errorf("exec=%d preps=%d (AwxPrep 호출 후 원복까지 11)", c.n, preps)
	}
	if !strings.Contains(r.out.String(), "AWX 종료 (rc=0)") {
		t.Errorf("종료 메시지 없음")
	}
}
