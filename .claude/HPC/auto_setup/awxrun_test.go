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
	r := runKeys(t, &fakeSrc{snap: twoGroupSnap(), local: true}, "wy2alice\r\x04")
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
	r := runKeys(t, &fakeSrc{snap: twoGroupSnap(), local: true}, "wnalice\r\x04")
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
	r := runKeys(t, &fakeSrc{snap: twoGroupSnap(), local: true}, "walice\r\x04")
	if c.n != 1 || c.has("AWX_AUTO=1") {
		t.Errorf("n=%d 바로 일반 실행이어야 함", c.n)
	}
	if strings.Contains(r.out.String(), "AWX auto 실행?") {
		t.Errorf("프로파일이 없으면 묻지 않아야 함")
	}
}

func TestAwxInterruptedMessage(t *testing.T) {
	useFakeAwx(t, 130)
	r := runKeys(t, &fakeSrc{snap: twoGroupSnap(), local: true}, "walice\r\x04")
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
	g := &gateIn{data: []byte("walice\r\x04")}
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

func TestParseAwxList(t *testing.T) {
	twelve := "Dell R750 INFRA-A host01 10.0.0.1 aa:bb:cc:dd:ee:01 eth0 sda sda5 1200 RHEL8 UEFI"
	lines, hosts := parseAwxList("h1, h2|h3\n  h2  \r\n" + twelve + "\n" + twelve + "\n")
	if strings.Join(lines, "/") != "h1/h2/h3/"+twelve || strings.Join(hosts, ",") != "h1,h2,h3,host01" {
		t.Fatalf("lines=%q hosts=%q", lines, hosts)
	}
	if l, h := parseAwxList(" \n\t\n"); len(l) != 0 || len(h) != 0 {
		t.Fatalf("빈 입력: %q %q", l, h)
	}
}

// w → user → 대상 목록: <user>.txt 덮어쓰기, AWX_USER·AWX_VERIFY_FILE 전달, 실행 뒤 확인용 임시 파일 삭제
func TestAwxListWrittenAndEnv(t *testing.T) {
	c := useFakeAwx(t, 0)
	userTxt := filepath.Join(awx_dir, "alice.txt")
	os.WriteFile(userTxt, []byte("old content\n"), 0644)
	var verifyBody string
	oldExec := awxExec
	awxExec = func(dir string, env []string) (int, error) {
		for _, e := range env {
			if strings.HasPrefix(e, "AWX_VERIFY_FILE=") {
				b, _ := os.ReadFile(strings.TrimPrefix(e, "AWX_VERIFY_FILE="))
				verifyBody = string(b)
			}
		}
		return oldExec(dir, env)
	}
	r := runKeys(t, &fakeSrc{snap: twoGroupSnap(), local: true}, "walice\rh1 h2,h3\r\r\x04")
	if c.n != 1 || c.val("AWX_USER") != "alice" {
		t.Fatalf("n=%d AWX_USER=%q", c.n, c.val("AWX_USER"))
	}
	if b, _ := os.ReadFile(userTxt); string(b) != "h1\nh2\nh3\n" {
		t.Fatalf("alice.txt 덮어쓰기: %q", b)
	}
	if verifyBody != "h1\nh2\nh3\n" {
		t.Fatalf("확인용 파일: %q", verifyBody)
	}
	if p := c.val("AWX_VERIFY_FILE"); p == "" {
		t.Fatal("AWX_VERIFY_FILE 없음")
	} else if _, err := os.Stat(p); !os.IsNotExist(err) {
		t.Fatalf("확인용 임시 파일이 남음: %s", p)
	}
	o := r.out.String()
	if !strings.Contains(o, "작업 대상 서버 목록을 붙여넣은 뒤 Ctrl+D") || !strings.Contains(o, "덮어쓰고") {
		t.Errorf("목록 입력 안내 없음")
	}
}

// 목록을 비우고 Ctrl+D → 건너뜀: 기존 파일 유지, AWX_VERIFY_FILE 없음 (AWX_USER 는 전달)
func TestAwxListSkipKeepsFile(t *testing.T) {
	c := useFakeAwx(t, 0)
	userTxt := filepath.Join(awx_dir, "alice.txt")
	os.WriteFile(userTxt, []byte("keep\n"), 0644)
	runKeys(t, &fakeSrc{snap: twoGroupSnap(), local: true}, "walice\r\x04")
	if c.n != 1 || c.val("AWX_USER") != "alice" || c.has("AWX_VERIFY_FILE") {
		t.Fatalf("n=%d env 이상", c.n)
	}
	if b, _ := os.ReadFile(userTxt); string(b) != "keep\n" {
		t.Fatalf("건너뛰었는데 파일이 바뀜: %q", b)
	}
}

// 프로파일 + 목록: auto 환경변수와 함께 전달, Esc 로 목록 입력 취소
func TestAwxProfileWithListAndEsc(t *testing.T) {
	c := useFakeAwx(t, 0, "y|2025|a")
	runKeys(t, &fakeSrc{snap: twoGroupSnap(), local: true}, "wy1bob\rh9\x04")
	if c.n != 1 || c.val("AWX_AUTO") != "1" || c.val("AWX_USER") != "bob" || c.val("AWX_VERIFY_FILE") == "" {
		t.Fatalf("env 이상 n=%d", c.n)
	}
	c2 := useFakeAwx(t, 0)
	runKeys(t, &fakeSrc{snap: twoGroupSnap(), local: true}, "walice\rh1\x1b")
	if c2.n != 0 {
		t.Fatal("Esc 로 취소했는데 실행됨")
	}
}
