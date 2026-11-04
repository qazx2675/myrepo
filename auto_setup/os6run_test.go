// os6run_test.go - os6_mgmt 직접 실행: 잠금·run 기록·완료기록 작성, 데몬의 완료기록 수거(검증 포함)·잠금 호스트 제외·응답 없는 잠금 무시,
// 스냅샷 비고, 스냅샷 캐시 대체, TUI 의 직접 실행(c y)·실행 중 종료 막기·os8 실행 중 호스트 제외
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

// withOS6Direct: 원격 클라이언트(os6_mgmt) 설정 + os6_autosetup=dir (os6_run 은 dir/os6_run)
func withOS6Direct(t *testing.T, dir string) string {
	t.Helper()
	a, b, c, d, e := os8_mgmt, os6_autosetup, newOS6Runner, newOS6Checker, os_check_sh
	os8_mgmt, os6_autosetup, os_check_sh = "os8test", dir, "/x/check.sh"
	t.Cleanup(func() { os8_mgmt, os6_autosetup, newOS6Runner, newOS6Checker, os_check_sh = a, b, c, d, e })
	return filepath.Join(dir, os6RunName)
}

// withOS6Watch: os8 데몬 쪽 — os6_autosetup=dir 만 (원격 클라이언트 아님)
func withOS6Watch(t *testing.T, dir string) string {
	t.Helper()
	a := os6_autosetup
	os6_autosetup = dir
	t.Cleanup(func() { os6_autosetup = a })
	return filepath.Join(dir, os6RunName)
}

// os6FakeRunner: code 파일·postapply 를 남기는 가짜 Runner (started 를 닫고 release 까지 기다림)
type os6FakeRunner struct {
	mu       sync.Mutex
	started  chan struct{}
	release  chan struct{}
	activeN  int // Run 중에 본 active/ 잠금 수
	dir      string
	hosts    []string
	hasOS6   bool
	mode     string
	failed   []string
	callsRun int
}

func (f *os6FakeRunner) Run(j *Job, hosts []string, hasOS6 bool) (RunResult, error) {
	f.mu.Lock()
	f.callsRun++
	f.hosts, f.hasOS6, f.mode = append([]string(nil), hosts...), hasOS6, j.RunMode
	m, _ := filepath.Glob(filepath.Join(f.dir, "active", "*.lock"))
	f.activeN = len(m)
	f.mu.Unlock()
	if f.started != nil {
		close(f.started)
	}
	if f.release != nil {
		<-f.release
	}
	code := "0007"
	var post strings.Builder
	for _, h := range hosts {
		st := "OK"
		for _, x := range f.failed {
			if x == h {
				st = "FAIL"
			}
		}
		post.WriteString(h + ": " + st + " 점검\n")
	}
	if err := os.MkdirAll(filepath.Join(runsDir(), code), 0755); err != nil {
		return RunResult{}, err
	}
	os.WriteFile(filepath.Join(runsDir(), code, "check.res_"+j.User+"_postapply"), []byte(post.String()), 0644)
	os.MkdirAll(codesDir(), 0755)
	os.WriteFile(codePath(code), []byte("작업 : "+modeLabel(j.RunMode)+"\n결과 본문\n"), 0644)
	return RunResult{Processed: hosts, Code: code, Failed: f.failed}, nil
}

func readRunRec(t *testing.T, dir, id string) os6RunRec {
	t.Helper()
	var r os6RunRec
	if err := json.Unmarshal([]byte(mustRead(t, filepath.Join(dir, "runs", id+".json"))), &r); err != nil {
		t.Fatal(err)
	}
	return r
}

// 직접 실행 1회: 잠금(실행 중에만)·run 기록(running → done)·결과 본문·완료기록(형식·sha256·출처), 설정체크만은 완료기록 없음
func TestOS6ExecuteWritesRecords(t *testing.T) {
	setupDir(t)
	dir := withOS6Direct(t, t.TempDir())
	job := &Job{ID: "J1", User: "u1", Hosts: map[string]*Host{"a1": {Route: "local"}, "a2": {Route: "local"}}}
	lk := newOS6Lock(job, "a.yml", "", []string{"a1", "a2"}, time.Unix(tsNow, 0))
	lp, err := os6Acquire(dir, lk)
	if err != nil {
		t.Fatal(err)
	}
	if r := readRunRec(t, dir, lk.ID); r.State != "running" || !reflect.DeepEqual(r.Hosts, []string{"a1", "a2"}) {
		t.Fatalf("시작 기록 이상: %+v", r)
	}
	f := &os6FakeRunner{dir: dir, failed: []string{"a2"}}
	rec, text := os6Execute(dir, f, job, lk, lp, func() time.Time { return time.Unix(tsNow+60, 0) })
	if f.activeN != 1 || f.hasOS6 {
		t.Fatalf("실행 중 잠금 %d개 hasOS6=%v (1, false 여야 함)", f.activeN, f.hasOS6)
	}
	if _, err := os.Stat(lp); !os.IsNotExist(err) {
		t.Fatal("끝난 뒤 잠금이 남음")
	}
	r := readRunRec(t, dir, lk.ID)
	if r.State != "done" || r.Ended != tsNow+60 || r.Code != "0007" || !reflect.DeepEqual(r.Failed, []string{"a2"}) || r.Result != "runs/"+lk.ID+".txt" {
		t.Fatalf("끝 기록 이상: %+v", r)
	}
	if rec.State != "done" || !strings.Contains(text, "결과 본문") || mustRead(t, filepath.Join(dir, r.Result)) != text {
		t.Fatalf("결과 본문 이상: %q", text)
	}
	for _, h := range []string{"a1", "a2"} {
		d, err := parseDoneRecord([]byte(mustRead(t, filepath.Join(dir, "done", h))))
		st := "OK"
		if h == "a2" {
			st = "FAIL"
		}
		sum := sha256.Sum256([]byte(h + ": " + st + " 점검\n"))
		if err != nil || d.Epoch != tsNow+60 || d.User != "u1" || d.Source != os6DoneSrc || d.Sha != hex.EncodeToString(sum[:]) {
			t.Fatalf("완료기록 %s 이상: %+v %v", h, d, err)
		}
	}

	// 설정체크만: 완료기록을 남기지 않는다
	os.RemoveAll(filepath.Join(dir, "done"))
	job.RunMode = ModeCheck
	lk2 := newOS6Lock(job, "a.yml", ModeCheck, []string{"a1"}, time.Unix(tsNow+100, 0))
	lp2, err := os6Acquire(dir, lk2)
	if err != nil {
		t.Fatal(err)
	}
	os6Execute(dir, &os6FakeRunner{dir: dir}, job, lk2, lp2, time.Now)
	if m, _ := filepath.Glob(filepath.Join(dir, "done", "*")); len(m) != 0 {
		t.Fatalf("설정체크만인데 완료기록: %v", m)
	}
}

// 공유 경로에 못 쓰면 실행하지 않는다 (오류, 패닉 없음)
func TestOS6AcquireNotWritable(t *testing.T) {
	setupDir(t)
	base := t.TempDir()
	if err := os.WriteFile(filepath.Join(base, os6RunName), []byte("x"), 0644); err != nil { // 디렉터리 자리에 파일
		t.Fatal(err)
	}
	dir := withOS6Direct(t, base)
	lk := newOS6Lock(&Job{ID: "J1", User: "u1"}, "a.yml", "", []string{"a1"}, time.Now())
	if _, err := os6Acquire(dir, lk); err == nil || !strings.Contains(err.Error(), "쓸 수 없습니다") {
		t.Fatalf("오류여야 함: %v", err)
	}
	if _, err := os6Acquire("", lk); err == nil {
		t.Fatal("경로 없음은 오류여야 함")
	}
}

// 데몬: <os6_run>/done 의 완료기록을 같은 규칙으로 인정(run 제외·applied 이동), 전달 이전 기록은 거부(로그 1회, 그대로)
func TestOS6DoneAcceptedByDaemon(t *testing.T) {
	setupDir(t)
	dir := withOS6Watch(t, t.TempDir())
	x := newX(map[string]*simHost{"h1": install(60, 125, 400, 600), "h2": install(60, 125, 400, 600), "h3": install(60, 125, 400, 600)})
	x.noSSH["h2"] = true
	id := submit(t, "u1", 0, "h1", "h2", "h3")
	d := drive(t, x.world, newXDaemon(x, false, false), 0, 695)
	od := filepath.Join(dir, "done")
	if err := writeDoneRecordIn(od, "h2", doneRecord{Epoch: base + 700, User: "u1", Sha: "abc", Source: os6DoneSrc}); err != nil {
		t.Fatal(err)
	}
	writeDoneRecordIn(od, "h3", doneRecord{Epoch: base - 10, User: "u1", Sha: "abc", Source: os6DoneSrc})
	drive(t, x.world, d, 700, 1500)
	wantRuns(t, x.world, []runCall{{At: 700, Hosts: []string{"h1", "h3"}}})
	j := doneJob(t, id)
	if h := j.Hosts["h2"]; h.Processed != DoneSrcExternal || h.DoneSrc != DoneSrcExternal {
		t.Fatalf("h2 인정 이상: %+v", h)
	}
	if _, err := os.Stat(filepath.Join(od, "applied", "h2")); err != nil {
		t.Fatal("os6_run/done/applied/h2 없음")
	}
	if _, err := os.Stat(filepath.Join(od, "h3")); err != nil {
		t.Fatal("거부된 h3 기록이 사라짐")
	}
	if n := logCount(t, "완료기록 거부: h3 "); n != 1 {
		t.Fatalf("h3 거부 로그 %d회 (1회여야 함)", n)
	}
	if n := logCount(t, "출처 os6_direct"); n != 1 {
		t.Fatalf("인정 로그 %d회", n)
	}
}

func putOS6Lock(t *testing.T, dir string, lk os6Lock, mtime time.Time) string {
	t.Helper()
	os.MkdirAll(filepath.Join(dir, "active"), 0755)
	p := filepath.Join(dir, "active", lk.ID+".lock")
	if err := os6WriteJSON(p, lk); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(p, mtime, mtime); err != nil {
		t.Fatal(err)
	}
	return p
}

// 데몬: 잠금이 잡은 호스트는 갱신되는 동안 run 에서 빠지고, 갱신이 os6Stale 동안 멈추면 무시(로그 1회)하고 실행한다
func TestOS6LockExcludesHostsUntilStale(t *testing.T) {
	setupDir(t)
	dir := withOS6Watch(t, t.TempDir())
	w := newWorld(map[string]*simHost{"h1": install(60, 120, 400, 600), "h2": install(60, 150, 420, 620)})
	submit(t, "u1", 0, "h1", "h2")
	lp := putOS6Lock(t, dir, os6Lock{ID: "1_os6_9", Server: "other-os6", PID: 9, Job: "J", Yml: "a.yml", Hosts: []string{"h2"}}, time.Unix(1000, 0))
	d := newTestDaemon(w)
	// 0~900 동안 60초마다 잠금 갱신 (mtime 값은 데몬 시계와 무관 — 바뀌는지만 본다)
	for o, k := int64(0), int64(1); o < 900; o, k = o+60, k+1 {
		d = drive(t, w, d, o, o+55)
		os.Chtimes(lp, time.Unix(1000+k, 0), time.Unix(1000+k, 0))
	}
	if len(w.runs) != 1 || !reflect.DeepEqual(w.runs[0].Hosts, []string{"h1"}) {
		t.Fatalf("잠금 동안 h2 가 run 에 들어감: %+v", w.runs)
	}
	d = drive(t, w, d, 900, 1300) // 갱신 멈춤 → 900 + os6Stale 뒤 무시
	if len(w.runs) != 2 || !reflect.DeepEqual(w.runs[1].Hosts, []string{"h2"}) || w.runs[1].At < 900+secs(os6Stale) {
		t.Fatalf("응답 없는 잠금 뒤 h2 run 이상: %+v", w.runs)
	}
	if logCount(t, "os6 직접 실행 감지: 1_os6_9") != 1 || logCount(t, "잠금 응답 없음") != 1 {
		t.Fatalf("로그 이상:\n%s", mustRead(t, logPath()))
	}
	os.Remove(lp)
	drive(t, w, d, 1305, 1310)
	if logCount(t, "os6 직접 실행 잠금 해제: 1_os6_9") != 1 {
		t.Fatal("잠금 해제 로그 없음")
	}
}

// 데몬: 잠금 호스트의 수동 run(os8 c) 은 잠금이 풀릴 때까지 미룬다
func TestOS6LockDefersManualRun(t *testing.T) {
	setupDir(t)
	dir := withOS6Watch(t, t.TempDir())
	w := newWorld(map[string]*simHost{"h1": oldOS()})
	id := submit(t, "u1", 0, "h1")
	d := drive(t, w, newTestDaemon(w), 0, 10)
	lp := putOS6Lock(t, dir, os6Lock{ID: "1_os6_9", Server: "other", Hosts: []string{"h1"}}, time.Unix(1000, 0))
	if _, err := writeRequest(ReqManualRun, map[string]string{"jobid": id, "yml": allGroup}); err != nil {
		t.Fatal(err)
	}
	d = drive(t, w, d, 15, 60)
	if len(w.runs) != 0 {
		t.Fatalf("잠금 중 수동 run 시작: %+v", w.runs)
	}
	os.Remove(lp)
	drive(t, w, d, 65, 80)
	if len(w.runs) != 1 || !reflect.DeepEqual(w.runs[0].Hosts, []string{"h1"}) {
		t.Fatalf("잠금 해제 뒤 수동 run 없음: %+v", w.runs)
	}
}

// os6 TUI 쪽: 같은 서버의 죽은 잠금은 지우고 무시, 다른 서버의 오래된 잠금은 무시(지우지 않음), 살아 있는 잠금은 잡음
func TestOS6HeldHosts(t *testing.T) {
	dir := filepath.Join(t.TempDir(), os6RunName)
	now := time.Now()
	c := exec.Command("true")
	if err := c.Run(); err != nil {
		t.Skip("true 실행 불가")
	}
	me, _ := os.Hostname()
	dead := putOS6Lock(t, dir, os6Lock{ID: "1_a_1", Server: me, PID: c.Process.Pid, Hosts: []string{"d1"}}, now)
	old := putOS6Lock(t, dir, os6Lock{ID: "2_b_2", Server: "other", PID: 2, Hosts: []string{"o1"}}, now.Add(-os6StaleAbs-time.Minute))
	putOS6Lock(t, dir, os6Lock{ID: "3_c_3", Server: "other", PID: 3, Hosts: []string{"l1", "l2"}}, now)
	got := os6HeldHosts(dir, now)
	if !reflect.DeepEqual(got, map[string]string{"l1": "3_c_3", "l2": "3_c_3"}) {
		t.Fatalf("잡은 호스트 이상: %v", got)
	}
	if _, err := os.Stat(dead); !os.IsNotExist(err) {
		t.Fatal("같은 서버의 죽은 잠금이 남음")
	}
	if _, err := os.Stat(old); err != nil {
		t.Fatal("다른 서버의 잠금은 지우지 않아야 함")
	}
}

// 스냅샷 비고: 실행 중 / 응답 없는 잠금 / 끝난 run 결과(OK·FAIL·접속불가·체크만), 전달 이전 run 은 무시
func TestOS6SnapshotNotes(t *testing.T) {
	dir := withOS6Watch(t, t.TempDir())
	now := time.Unix(tsNow, 0)
	putOS6Lock(t, dir, os6Lock{ID: "9_x_1", Hosts: []string{"a1"}}, now)
	putOS6Lock(t, dir, os6Lock{ID: "9_x_2", Hosts: []string{"a2"}}, now.Add(-os6StaleAbs-time.Minute))
	os.MkdirAll(filepath.Join(dir, "runs"), 0755)
	os6WriteJSON(filepath.Join(dir, "runs", "r1.json"), os6RunRec{os6Lock: os6Lock{ID: "r1", Hosts: []string{"b1", "b2", "b3"}, Started: tsNow - 600},
		State: "done", Processed: []string{"b1", "b2"}, Failed: []string{"b2"}})
	os6WriteJSON(filepath.Join(dir, "runs", "r2.json"), os6RunRec{os6Lock: os6Lock{ID: "r2", Mode: ModeCheck, Hosts: []string{"b4"}, Started: tsNow - 300},
		State: "done", Processed: []string{"b4"}})
	os6WriteJSON(filepath.Join(dir, "runs", "r3.json"), os6RunRec{os6Lock: os6Lock{ID: "r3", Hosts: []string{"b5"}, Started: tsNow - 9000}, State: "done"})
	sj := tsJob("J1", "u1", false, tsGroup("a.yml", "ib", "r8", "pxe", "y",
		tsHost("a1", StageReady, 1, ""), tsHost("a2", StageReady, 1, "os6경유"), tsHost("b1", StageReady, 1, ""), tsHost("b2", StageReady, 1, ""),
		tsHost("b3", StageReady, 1, ""), tsHost("b4", StageReady, 1, ""), tsHost("b5", StageReady, 1, "")))
	annotateOS6(&sj, readOS6SnapInfo(now))
	tm := func(ago int64) string { return time.Unix(tsNow-ago, 0).Format("01-02 15:04") }
	want := []string{"os6_mgmt 직접 실행 중", "os6경유, os6_mgmt 직접 실행 잠금(응답 없음)", "os6 직접 " + tm(600) + " OK", "os6 직접 " + tm(600) + " FAIL",
		"os6 직접 " + tm(600) + " 접속불가", "os6 직접(체크만) " + tm(300) + " OK", ""}
	for i, h := range sj.Groups[0].Hosts {
		if h.Note != want[i] {
			t.Errorf("%s 비고 %q (기대 %q)", h.Host, h.Note, want[i])
		}
	}
	if readOS6SnapInfo(now) == nil {
		t.Fatal("정보가 있어야 함")
	}
	os6_autosetup = ""
	if readOS6SnapInfo(now) != nil {
		t.Fatal("os6_autosetup 이 비면 nil (기존 스냅샷 그대로)")
	}
}

// 스냅샷 캐시: 받은 스냅샷 저장 → os8 실패 시 캐시 + *snapCachedErr, 캐시가 없으면 안내를 붙인 오류. -user 필터도 캐시에 적용.
func TestOS6SnapCacheFallback(t *testing.T) {
	withOS6Direct(t, t.TempDir())
	s := twoGroupSnap()
	if _, err := cachedSnapshot(errors.New("boom")); err == nil || !strings.Contains(err.Error(), "저장된 스냅샷 없음") {
		t.Fatalf("캐시 없음 오류 이상: %v", err)
	}
	saveSnapCache(s)
	got, err := cachedSnapshot(errors.New("boom"))
	var ce *snapCachedErr
	if !errors.As(err, &ce) || len(got.Jobs) != 1 || got.Now != s.Now {
		t.Fatalf("캐시 대체 이상: %v %+v", err, got)
	}
	if !strings.Contains(err.Error(), "저장된 스냅샷") || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("안내 문구 이상: %v", err)
	}
	a := forceUser
	forceUser = "nobody"
	defer func() { forceUser = a }()
	fs, ferr := userFilterSource{&fakeSrc{snap: got, err: err}}.Snapshot()
	if !errors.As(ferr, &ce) || len(fs.Jobs) != 0 {
		t.Fatalf("-user 필터가 캐시에 적용되지 않음: %v %d", ferr, len(fs.Jobs))
	}
	// TUI: 캐시 스냅샷으로 그룹을 보여 주고 안내 줄에 캐시 사실
	r := runKeys(t, &fakeSrc{snap: got, err: err}, "")
	if f := lastFrame(r); !strings.Contains(f, "a.yml") || !strings.Contains(f, "os8_mgmt 접속 불가") {
		t.Fatalf("캐시 화면 이상:\n%s", f)
	}
}

// TUI(os6_mgmt): \rcy → 이 서버에서 직접 실행(요청 없음, hasOS6=false). 실행 중 q·입력 끝은 종료하지 않고 안내, 끝난 뒤 종료.
func TestOS6TUIDirectRunQuitBlocked(t *testing.T) {
	setupDir(t)
	dir := withOS6Direct(t, t.TempDir())
	f := &os6FakeRunner{dir: dir, started: make(chan struct{}), release: make(chan struct{})}
	newOS6Runner = func(sh string) Runner {
		if sh != "/x/check.sh" {
			t.Errorf("체크스크립트 %q", sh)
		}
		return f
	}
	src := &fakeSrc{snap: twoGroupSnap()}
	pr, pw := io.Pipe()
	env, r := newRig(pr)
	done := make(chan int, 1)
	go func() { done <- runTUILoop(src, env) }()
	pw.Write([]byte("\rcy"))
	select {
	case <-f.started:
	case <-time.After(5 * time.Second):
		t.Fatal("직접 실행이 시작되지 않음")
	}
	pw.Write([]byte("q")) // 진행 보기 닫기
	pw.Write([]byte("q")) // 화면 1 로
	pw.Write([]byte("q")) // 종료 시도 → 막힘
	pw.Close()            // 입력 끝 → 끝나면 종료
	select {
	case <-done:
		t.Fatal("실행 중에 TUI 가 종료됨")
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
	if !strings.Contains(r.out.String(), os6BusyMsg) {
		t.Fatal("실행 중 안내 문구 없음")
	}
	if len(src.reqs) != 0 || f.callsRun != 1 || f.hasOS6 || f.activeN != 1 || !reflect.DeepEqual(f.hosts, []string{"a1", "a2"}) {
		t.Fatalf("직접 실행 이상: reqs=%v calls=%d hasOS6=%v active=%d hosts=%v", src.reqs, f.callsRun, f.hasOS6, f.activeN, f.hosts)
	}
	if m, _ := filepath.Glob(filepath.Join(dir, "active", "*")); len(m) != 0 {
		t.Fatalf("잠금이 남음: %v", m)
	}
	if m, _ := filepath.Glob(filepath.Join(dir, "done", "a*")); len(m) != 2 {
		t.Fatalf("완료기록 %v", m)
	}
	assertRestored(t, r)
}

// TUI(os6_mgmt): os8 데몬이 체크 중인 호스트는 빼고 실행, 설정체크만(t)은 모드 전달
func TestOS6TUIDirectRunSkipsOS8Busy(t *testing.T) {
	setupDir(t)
	dir := withOS6Direct(t, t.TempDir())
	f := &os6FakeRunner{dir: dir}
	newOS6Runner = func(string) Runner { return f }
	snap := tsSnap(true, tsJob("J1", "user1", false,
		tsGroup("a.yml", "ib", "rhel8", "pxe", "y", tsHost("a1", StageReady, 10, ""), tsHost("a2", StageChecking, 50, ""))))
	r := runKeys(t, &fakeSrc{snap: snap}, "\rty")
	if !reflect.DeepEqual(f.hosts, []string{"a1"}) || f.mode != ModeCheck {
		t.Fatalf("hosts=%v mode=%q", f.hosts, f.mode)
	}
	if !strings.Contains(r.out.String(), "제외(os8 데몬 실행 중) 1대: a2") {
		t.Fatal("제외 안내 없음")
	}
	if m, _ := filepath.Glob(filepath.Join(dir, "done", "*")); len(m) != 0 {
		t.Fatalf("체크만인데 완료기록: %v", m)
	}
}

// TUI(os6_mgmt): g → 이 서버의 gossh 로 바로 준비확인 (요청 없음), 결과표
func TestOS6TUIDirectCheck(t *testing.T) {
	setupDir(t)
	withOS6Direct(t, t.TempDir())
	var got []string
	newOS6Checker = func() Checker {
		return checkFunc(func(route string, hosts []string) (map[string]CheckResult, error) {
			got = append([]string{route}, hosts...)
			return map[string]CheckResult{"a2": {Responded: true, Uptime: 42}}, nil
		})
	}
	src := &fakeSrc{snap: twoGroupSnap()}
	r := runKeys(t, src, "\rg")
	if !reflect.DeepEqual(got, []string{"local", "a2"}) || len(src.reqs) != 0 {
		t.Fatalf("준비확인 호출 %v, 요청 %v", got, src.reqs)
	}
	if o := r.out.String(); !strings.Contains(o, "os6 응답") || !strings.Contains(o, "uptime 42s") {
		t.Fatal("결과표 없음")
	}
}

type checkFunc func(route string, hosts []string) (map[string]CheckResult, error)

func (c checkFunc) Check(route string, hosts []string) (map[string]CheckResult, error) {
	return c(route, hosts)
}
