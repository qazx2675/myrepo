// ctl_test.go - CLI 분기(무인자/--plain/-h/잘못된 인자), 데몬 --start/--stop/--restart, done/request 파일 생성
package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"
)

func localEnv(t *testing.T, dir string) []string {
	return append(os.Environ(), "AUTO_SETUP_DIR="+dir, "AUTO_SETUP_NOW=4700", "NO_COLOR=1")
}

func TestCLIUsageAndReport(t *testing.T) {
	lb, _ := buildBins(t)
	env := localEnv(t, t.TempDir())

	if r := runBin(t, lb, env, "-h"); r.RC != 0 || !strings.Contains(r.Out, "사용법: auto_setup") || r.Err != "" {
		t.Fatalf("-h: %+v", r)
	}
	if r := runBin(t, lb, env, "--help"); r.RC != 0 || !strings.Contains(r.Out, "--restart") {
		t.Fatalf("--help: %+v", r)
	}
	for _, args := range [][]string{{"bogus"}, {"--start", "x"}, {"--plain", "x"}, {"code"}, {"code", "1", "2"},
		{"done"}, {"request"}, {"request", "manual-run", "j"}, {"request", "cancel"}, {"request", "x", "y"}, {"snapshot", "z"}} {
		if r := runBin(t, lb, env, args...); r.RC != 1 || r.Out != "" || !strings.Contains(r.Err, "사용법: auto_setup") {
			t.Errorf("%v: 사용법+exit 1 아님: %+v", args, r)
		}
	}
	// 인자 없음 + 비 tty → plain 표
	r := runBin(t, lb, env)
	if r.RC != 0 || !strings.Contains(r.Out, "진행 중인 작업 없음") || !strings.Contains(r.Out, "데몬 중지") {
		t.Fatalf("무인자: %+v", r)
	}
	if p := runBin(t, lb, env, "--plain"); p != r {
		t.Fatalf("--plain 과 무인자(비 tty) 불일치:\n%+v\n%+v", p, r)
	}
	// 작업이 있는 plain 표
	dir := prepState(t)
	r = runBin(t, lb, localEnv(t, dir), "--plain")
	if r.RC != 0 || !strings.Contains(r.Out, "a.yml") || !strings.Contains(r.Out, "20251002-153000-u1") ||
		!strings.Contains(r.Out, "전체 5") {
		t.Fatalf("plain 표: %q", r.Out)
	}
}

func TestRunReportHook(t *testing.T) {
	dir := prepState(t)
	_ = dir
	old := runReport
	var gotPlain bool
	var gotLocal bool
	runReport = func(src SnapshotSource, plain bool) int { gotPlain, gotLocal = plain, src.Local(); return 7 }
	defer func() { runReport = old }()
	if rc := run([]string{"--plain"}); rc != 7 || !gotPlain || !gotLocal {
		t.Fatalf("--plain → runReport(plain=true): rc=%d plain=%v local=%v", rc, gotPlain, gotLocal)
	}
	// 테스트 프로세스의 stdout 은 tty 가 아니므로 무인자도 plain
	gotPlain = false
	if rc := run(nil); rc != 7 || !gotPlain {
		t.Fatalf("무인자 비 tty → plain: rc=%d plain=%v", rc, gotPlain)
	}
}

func TestCLIDoneAndRequestLocal(t *testing.T) {
	lb, _ := buildBins(t)
	dir := prepState(t)
	env := localEnv(t, dir)
	r := runBin(t, lb, env, "done", "h1", "h2")
	if r.RC != 0 || r.Out != "[O] 완료 처리 요청: 2대\n" || !strings.HasPrefix(r.Err, "[!] ") {
		t.Fatalf("done: %+v", r)
	}
	for _, h := range []string{"h1", "h2"} {
		b, err := os.ReadFile(filepath.Join(dir, "done", h))
		f := strings.Fields(string(b))
		if err != nil || len(f) != 4 || f[3] != "manual" || f[2] != "-" {
			t.Fatalf("done/%s: %q %v", h, b, err)
		}
	}
	if r := runBin(t, lb, env, "done", "bad/host"); r.RC != 1 || !strings.HasPrefix(r.Err, "[X] ") {
		t.Fatalf("잘못된 호스트명: %+v", r)
	}
	r = runBin(t, lb, env, "request", "manual-run", "20251002-153000-u1", "a.yml")
	if r.RC != 0 || !regexp.MustCompile(`^\[O\] 요청 등록: \d+_manual-run(_\d+)?\.req\n$`).MatchString(r.Out) {
		t.Fatalf("request manual-run: %+v", r)
	}
	if r := runBin(t, lb, env, "request", "cancel", "20251002-153000-u1"); r.RC != 0 {
		t.Fatalf("request cancel: %+v", r)
	}
	if m, _ := filepath.Glob(filepath.Join(dir, "requests", "*_cancel*.req")); len(m) != 1 {
		t.Fatalf("cancel 요청 파일: %v", m)
	}
	if r := runBin(t, lb, env, "request", "cancel", "a\nb"); r.RC != 1 || !strings.HasPrefix(r.Err, "[X] ") {
		t.Fatalf("개행 payload 는 거부: %+v", r)
	}
}

// 1차 status 출력은 바이트 동일: formatJobStatus 와 같은 결과
func TestStatusUnchanged(t *testing.T) {
	lb, _ := buildBins(t)
	dir := prepState(t)
	jobs, err := loadJobs()
	if err != nil {
		t.Fatal(err)
	}
	var want strings.Builder
	for _, j := range jobs {
		want.WriteString(formatJobStatus(j))
	}
	if r := runBin(t, lb, localEnv(t, dir), "status"); r.Out != want.String() || r.RC != 0 {
		t.Fatalf("status 출력 변경:\n%q\n%q", r.Out, want.String())
	}
}

var pidRe = regexp.MustCompile(`\(pid (\d+)\)`)

func pidOf(t *testing.T, out string) int {
	t.Helper()
	m := pidRe.FindStringSubmatch(out)
	if m == nil {
		t.Fatalf("pid 없음: %q", out)
	}
	n, _ := strconv.Atoi(m[1])
	return n
}

// 실제 바이너리로 데몬 기동·중복·재기동·정지·pid 정리
func TestDaemonStartStopRestart(t *testing.T) {
	lb, _ := buildBins(t)
	dir := t.TempDir()
	env := localEnv(t, dir)
	t.Cleanup(func() { runBin(t, lb, env, "--stop") })
	pidFile := filepath.Join(dir, "auto_setup.pid")

	if r := runBin(t, lb, env, "--stop"); r.RC != 0 || r.Out != "" || !strings.HasPrefix(r.Err, "[!] 데몬이 실행 중이 아닙니다") {
		t.Fatalf("미실행 stop: %+v", r)
	}
	r := runBin(t, lb, env, "--start")
	if r.RC != 0 || !strings.HasPrefix(r.Out, "[O] 데몬 기동 (pid ") {
		t.Fatalf("start: %+v", r)
	}
	pid1 := pidOf(t, r.Out)
	if _, err := os.Stat(pidFile); err != nil {
		t.Fatalf("pid 파일 없음: %v", err)
	}
	if r := runBin(t, lb, env, "start"); r.RC != 0 || r.Out != "[O] 이미 실행 중 (pid "+strconv.Itoa(pid1)+")\n" {
		t.Fatalf("중복 start: %+v", r)
	}
	// 실행 중 snapshot/plain 에 데몬 상태 반영
	if s := runBin(t, lb, env, "snapshot"); !strings.Contains(s.Out, `"running": true`) {
		t.Fatalf("snapshot 데몬 상태: %s", s.Out)
	}
	r = runBin(t, lb, env, "restart")
	if r.RC != 0 || strings.Count(r.Out, "[O]") != 2 {
		t.Fatalf("restart: %+v", r)
	}
	lines := strings.Split(strings.TrimSpace(r.Out), "\n")
	if pidOf(t, lines[0]) != pid1 || pidOf(t, lines[1]) == pid1 {
		t.Fatalf("restart pid 변경 이상: %q", r.Out)
	}
	r = runBin(t, lb, env, "stop")
	if r.RC != 0 || !strings.HasPrefix(r.Out, "[O] 데몬 종료 (pid ") {
		t.Fatalf("stop: %+v", r)
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		if _, err := os.Stat(pidFile); os.IsNotExist(err) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("정지 후 pid 파일이 남음")
		}
		time.Sleep(50 * time.Millisecond)
	}
	// 미실행 상태의 restart = 기동만
	r = runBin(t, lb, env, "--restart")
	if r.RC != 0 || !strings.Contains(r.Out, "[O] 데몬 기동") {
		t.Fatalf("미실행 restart: %+v", r)
	}
	if r := runBin(t, lb, env, "--stop"); r.RC != 0 || !strings.HasPrefix(r.Out, "[O] 데몬 종료") {
		t.Fatalf("마지막 stop: %+v", r)
	}
}

func TestColorOnlyOnTTYAndNoColor(t *testing.T) {
	if s := ctlPaint("[O]", 32, false); s != "[O]" {
		t.Fatalf("비 tty 색: %q", s)
	}
	t.Setenv("NO_COLOR", "")
	if s := ctlPaint("[O]", 32, true); s != "\x1b[32m[O]\x1b[0m" {
		t.Fatalf("tty 색: %q", s)
	}
	t.Setenv("NO_COLOR", "1")
	if s := ctlPaint("[O]", 32, true); s != "[O]" {
		t.Fatalf("NO_COLOR: %q", s)
	}
}

func TestRelayArgsOK(t *testing.T) {
	ok := [][]string{{"status"}, {"status", "x"}, {"snapshot"}, {"code", "1"}, {"cancel", "j"}, {"done", "h"}, {"done", "a", "b"},
		{"request", "manual-run", "j", "y"}, {"request", "cancel", "j"}}
	bad := [][]string{{"snapshot", "x"}, {"code"}, {"cancel"}, {"done"}, {"request"}, {"request", "cancel"}, {"request", "manual-run", "j"}, {"request", "zz", "j"}}
	for _, a := range ok {
		if !relayArgsOK(a) {
			t.Errorf("%v 허용이어야 함", a)
		}
	}
	for _, a := range bad {
		if relayArgsOK(a) {
			t.Errorf("%v 거부여야 함", a)
		}
	}
}
