package daemon

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"cross_check/internal/store"
)

// ---- 테스트 도구 ----

// fakeClock 은 테스트에서 시간을 직접 움직이는 시계.
type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func newFakeClock() *fakeClock { return &fakeClock{t: time.Date(2026, 10, 24, 9, 0, 0, 0, time.Local)} }

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *fakeClock) Add(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

// logBuf 는 데몬 로그를 모아 비밀번호가 새지 않는지 검사한다.
type logBuf struct {
	mu    sync.Mutex
	lines []string
}

func (l *logBuf) Logf(f string, a ...any) {
	l.mu.Lock()
	l.lines = append(l.lines, fmt.Sprintf(f, a...))
	l.mu.Unlock()
}

func (l *logBuf) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return strings.Join(l.lines, "\n")
}

// 테스트 사용자와 비밀번호.
var testPasswords = map[string]string{
	"alice": "pw_alice_1",
	"bob":   "pw=bob 2", // '=' 와 가운데 공백도 비밀번호에 쓸 수 있다
	"carol": "pw_carol_3",
	"admin": "pw_admin_4",
}

var aliceTargets = []string{"vm01ev01", "vm01ev02", "vm02ev01", "vm03ev01"}

// testUsers 는 하네스의 users= 목록(dave~gina 는 목록에는 있지만 auth.conf 에는 아직 없음).
var testUsers = []string{"alice", "bob", "carol", "dave", "eve", "frank", "gina"}

type harness struct {
	t         *testing.T
	dir       string
	sock      string
	storePath string
	authPath  string
	targetDir string
	logs      *logBuf
	srv       *Server
	serveErr  chan error
}

// writeAuth 는 비밀번호 파일을 0600 으로 쓴다.
func writeAuth(t *testing.T, path string, users map[string]string) {
	t.Helper()
	var b strings.Builder
	b.WriteString("# 테스트 비밀번호\n\n")
	for u, p := range users {
		b.WriteString(u + "=" + p + "\n")
	}
	if err := os.WriteFile(path, []byte(b.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	os.Chmod(path, 0o600)
}

func writeTargets(t *testing.T, dir, user, body string) {
	t.Helper()
	if err := os.WriteFile(TargetFilePath(dir, user), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// newHarness 는 짧은 임시 경로에 데몬을 띄운다(Unix 소켓 경로 길이 한도 108자 때문에 t.TempDir 대신 MkdirTemp).
// prep 으로 파일을 미리 고치고, mod 로 Config 를 고친다.
func newHarness(t *testing.T, mod func(*harness, *Config)) *harness {
	t.Helper()
	dir, err := os.MkdirTemp("", "ccd")
	if err != nil {
		t.Fatal(err)
	}
	h := &harness{
		t: t, dir: dir,
		sock:      filepath.Join(dir, "d.sock"),
		storePath: filepath.Join(dir, "state", "jobs.json"),
		authPath:  filepath.Join(dir, "etc", "auth.conf"),
		targetDir: filepath.Join(dir, "targets"),
		logs:      &logBuf{},
	}
	os.MkdirAll(filepath.Dir(h.authPath), 0o700)
	os.MkdirAll(h.targetDir, 0o755)
	writeAuth(t, h.authPath, testPasswords)
	writeTargets(t, h.targetDir, "alice", "# alice 대상\n\nvm01ev01\n  vm01ev02  \nvm02ev01\nvm03ev01\n# 끝\n")
	writeTargets(t, h.targetDir, "bob", "vm09ev01\n")
	cfg := Config{
		SocketPath:        h.sock,
		StorePath:         h.storePath,
		AuthPath:          h.authPath,
		AuthSkipPermCheck: runtime.GOOS == "windows",
		TargetDir:         h.targetDir,
		AllowedUsers:      testUsers,
		SweepInterval:     time.Hour, // 스위퍼가 필요한 테스트만 줄인다
		Logf:              h.logs.Logf,
	}
	if mod != nil {
		mod(h, &cfg)
	}
	srv, err := NewServer(cfg)
	if err != nil {
		os.RemoveAll(dir)
		t.Fatalf("NewServer: %v", err)
	}
	h.srv = srv
	h.serveErr = make(chan error, 1)
	go func() { h.serveErr <- srv.Serve(context.Background()) }()
	t.Cleanup(func() {
		srv.Close()
		select {
		case err := <-h.serveErr:
			if err != nil {
				t.Errorf("Serve: %v", err)
			}
		case <-time.After(5 * time.Second):
			t.Error("Serve 가 끝나지 않음")
		}
		os.RemoveAll(dir)
	})
	return h
}

func (h *harness) client() *Client {
	h.t.Helper()
	c, err := Dial(h.sock)
	if err != nil {
		h.t.Fatal(err)
	}
	h.t.Cleanup(func() { c.Close() })
	return c
}

func (h *harness) run(c *Client, user, checker string, targets []string) *RunHandle {
	h.t.Helper()
	rh, err := c.Run(user, checker, targets)
	if err != nil {
		h.t.Fatalf("Run(%s,%s): %v", user, checker, err)
	}
	h.t.Cleanup(func() { rh.Close() })
	return rh
}

func (h *harness) job(id string) store.Job {
	h.t.Helper()
	j, ok := h.srv.st.Get(id)
	if !ok {
		h.t.Fatalf("작업 %s 없음", id)
	}
	return j
}

func waitFor(t *testing.T, d time.Duration, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("시간 안에 조건을 만족하지 않음: %s", what)
}

// nextEvent 는 이벤트 하나를 기다린다. 기대 상태와 사유(부분 문자열)를 확인한다.
func nextEvent(t *testing.T, rh *RunHandle, status store.Status, reasonPart string) Event {
	t.Helper()
	select {
	case ev, ok := <-rh.Events():
		if !ok {
			t.Fatalf("이벤트 채널이 닫힘(기대: %s %q)", status, reasonPart)
		}
		if ev.Event != EventStatus || ev.Status != status || !strings.Contains(ev.Reason, reasonPart) {
			t.Fatalf("이벤트 불일치: %+v (기대: %s %q)", ev, status, reasonPart)
		}
		return ev
	case <-time.After(3 * time.Second):
		t.Fatalf("이벤트가 오지 않음(기대: %s %q)", status, reasonPart)
	}
	return Event{}
}

func expectClosed(t *testing.T, rh *RunHandle) {
	t.Helper()
	select {
	case ev, ok := <-rh.Events():
		if ok {
			t.Fatalf("이벤트 채널이 닫혀야 하는데 이벤트가 옴: %+v", ev)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("이벤트 채널이 닫히지 않음")
	}
}

func wantDaemonErr(t *testing.T, err error, part string) {
	t.Helper()
	var de *DaemonError
	if !errors.As(err, &de) {
		t.Fatalf("DaemonError 기대(%q), 받은 것: %v", part, err)
	}
	if !strings.Contains(de.Msg, part) {
		t.Fatalf("오류 문구에 %q 가 없음: %q", part, de.Msg)
	}
}

// ---- 기본 흐름 ----

func TestPing(t *testing.T) {
	h := newHarness(t, nil)
	if err := h.client().Ping(); err != nil {
		t.Fatal(err)
	}
}

func TestHappyPath(t *testing.T) {
	h := newHarness(t, nil)
	c := h.client()
	rh := h.run(c, "alice", "bob", []string{"vm01ev01", "vm02ev01"})
	if rh.ID == "" || len(rh.Token) != 32 {
		t.Fatalf("ID/token 이상: %q %q", rh.ID, rh.Token)
	}
	ev := nextEvent(t, rh, StatusWaiting, "bob")
	if ev.By != BySystem {
		t.Errorf("대기 이벤트 by = %q", ev.By)
	}

	// 확인자는 목록에서 대상을 볼 수 있다(붙여넣기 없이 보고 승인)
	jobs, err := c.List("bob")
	if err != nil || len(jobs) != 1 || jobs[0].ID != rh.ID || jobs[0].Status != StatusWaiting {
		t.Fatalf("List(bob) = %+v, %v", jobs, err)
	}
	if len(jobs[0].Targets) != 2 || jobs[0].TargetCount != 2 {
		t.Fatalf("확인자에게 대상이 보여야 함: %+v", jobs[0])
	}
	if v, err := c.Get(rh.ID, "bob", ""); err != nil || v.Checker != "bob" {
		t.Fatalf("Get = %+v, %v", v, err)
	}

	if err := c.Approve(rh.ID, "bob", testPasswords["bob"]); err != nil {
		t.Fatal(err)
	}
	if err := rh.Wait(context.Background()); err != nil {
		t.Fatalf("Wait: %v", err)
	}
	ev = nextEvent(t, rh, StatusApproved, "bob 가 승인")
	if ev.By != "bob" {
		t.Errorf("승인 이벤트 by = %q", ev.By)
	}
	j := h.job(rh.ID)
	if j.Status != StatusApproved || j.ApprovedAt == nil || j.StartedAt != nil {
		t.Fatalf("승인 후 작업 = %+v", j)
	}

	if err := c.Start(rh.ID, rh.Token); err != nil {
		t.Fatalf("Start: %v", err)
	}
	ev = nextEvent(t, rh, StatusApproved, ReasonStarted)
	if ev.By != "alice" {
		t.Errorf("시작 이벤트 by = %q", ev.By)
	}
	expectClosed(t, rh)
	if j := h.job(rh.ID); j.StartedAt == nil || j.Status != StatusApproved {
		t.Fatalf("시작 후 작업 = %+v", j)
	}
	wantDaemonErr(t, c.Start(rh.ID, rh.Token), "이미 시작")
}

func TestSameUserAborted(t *testing.T) {
	h := newHarness(t, nil)
	_, err := h.client().Run("alice", "alice", []string{"vm01ev01"})
	wantDaemonErr(t, err, ReasonSameUser)
	var de *DaemonError
	errors.As(err, &de)
	if de.ID == "" {
		t.Fatal("동일인 중단 기록의 ID 가 응답에 없음")
	}
	if j := h.job(de.ID); j.Status != StatusAborted || j.Reason != ReasonSameUser {
		t.Fatalf("동일인 작업 = %+v", j)
	}
}

func TestWorkerDisconnectAborts(t *testing.T) {
	h := newHarness(t, nil)
	c := h.client()
	rh := h.run(c, "alice", "bob", []string{"vm01ev01"})
	nextEvent(t, rh, StatusWaiting, "")
	rh.Close() // Ctrl+C·kill -9 와 같음: 커널이 소켓을 닫는다
	waitFor(t, time.Second, "작업자 종료 → 중단", func() bool {
		j := h.job(rh.ID)
		return j.Status == StatusAborted && j.Reason == ReasonWorkerGone
	})
	wantDaemonErr(t, c.Approve(rh.ID, "bob", testPasswords["bob"]), "대기 상태가 아니")

	// ctx 취소도 같은 결과
	rh2 := h.run(c, "alice", "bob", []string{"vm01ev02"})
	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(50 * time.Millisecond); cancel() }()
	if err := rh2.Wait(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("Wait(ctx 취소) = %v", err)
	}
	waitFor(t, time.Second, "ctx 취소 → 중단", func() bool { return h.job(rh2.ID).Reason == ReasonWorkerGone })
}

func TestCheckerCancel(t *testing.T) {
	h := newHarness(t, nil)
	c := h.client()
	rh := h.run(c, "alice", "bob", []string{"vm01ev01"})
	nextEvent(t, rh, StatusWaiting, "")

	wantDaemonErr(t, c.Cancel(rh.ID, "bob", "wrong"), MsgAuthFailed)
	wantDaemonErr(t, c.Cancel(rh.ID, "alice", testPasswords["alice"]), msgNoJob) // 작업자는 취소 못 함
	wantDaemonErr(t, c.Cancel(rh.ID, "carol", testPasswords["carol"]), msgNoJob)
	if h.job(rh.ID).Status != StatusWaiting {
		t.Fatal("실패한 취소가 상태를 바꿈")
	}
	if err := c.Cancel(rh.ID, "bob", testPasswords["bob"]); err != nil {
		t.Fatal(err)
	}
	err := rh.Wait(context.Background())
	var ae *AbortedError
	if !errors.As(err, &ae) || ae.Reason != ReasonCancel("bob") || ae.By != "bob" {
		t.Fatalf("Wait = %v", err)
	}
	nextEvent(t, rh, StatusAborted, "확인자 bob 가 취소")
	expectClosed(t, rh)
	if j := h.job(rh.ID); j.Status != StatusAborted || j.Reason != ReasonCancel("bob") {
		t.Fatalf("취소 후 작업 = %+v", j)
	}
}

// ---- 인증 ----

func TestApproveRequiresCheckerPassword(t *testing.T) {
	h := newHarness(t, nil)
	c := h.client()
	rh := h.run(c, "alice", "bob", []string{"vm01ev01"})

	wantDaemonErr(t, c.Approve(rh.ID, "bob", ""), MsgAuthFailed)                     // 비밀번호 없음
	wantDaemonErr(t, c.Approve(rh.ID, "bob", "pw=bob"), MsgAuthFailed)               // 틀림
	wantDaemonErr(t, c.Approve(rh.ID, "bob", testPasswords["carol"]), MsgAuthFailed) // 남의 비밀번호로 bob 사칭
	wantDaemonErr(t, c.Approve(rh.ID, "carol", testPasswords["carol"]), msgNoJob)    // carol 본인 인증은 되지만 확인자가 아님
	wantDaemonErr(t, c.Approve(rh.ID, "alice", testPasswords["alice"]), msgNoJob)    // 작업자 자신
	wantDaemonErr(t, c.Approve("nosuchjob", "bob", testPasswords["bob"]), msgNoJob)  // 없는 작업
	wantDaemonErr(t, c.Approve(rh.ID, "zed", "anything"), MsgAuthFailed)             // 없는 사용자도 같은 문구
	wantDaemonErr(t, c.Approve(rh.ID, "BOB", testPasswords["bob"]), MsgAuthFailed)   // 이름 규칙 위반
	if h.job(rh.ID).Status != StatusWaiting {
		t.Fatal("실패한 승인이 상태를 바꿈")
	}
	if err := c.Approve(rh.ID, "bob", testPasswords["bob"]); err != nil {
		t.Fatal(err)
	}
	wantDaemonErr(t, c.Approve(rh.ID, "bob", testPasswords["bob"]), "대기 상태가 아니")
}

func TestCheckerWithoutEntryDenied(t *testing.T) {
	h := newHarness(t, nil)
	c := h.client()
	rh := h.run(c, "alice", "bob", []string{"vm01ev01"})
	// 관리자가 bob 항목을 지움 → bob 은 더 이상 승인 못 함
	users := map[string]string{}
	for u, p := range testPasswords {
		if u != "bob" {
			users[u] = p
		}
	}
	writeAuth(t, h.authPath, users)
	waitFor(t, 2*time.Second, "파일 다시 읽기", func() bool { ok, _ := c.UserRegistered("bob"); return !ok })
	wantDaemonErr(t, c.Approve(rh.ID, "bob", testPasswords["bob"]), MsgAuthFailed)
	if h.job(rh.ID).Status != StatusWaiting {
		t.Fatal("상태가 바뀜")
	}
	// 미등록 확인자로는 run 자체가 거부된다
	_, err := c.Run("alice", "bob", []string{"vm01ev01"})
	wantDaemonErr(t, err, "확인자 미등록")
}

func TestAdminOpsRequireAdminPassword(t *testing.T) {
	h := newHarness(t, nil)
	c := h.client()
	rh := h.run(c, "alice", "bob", []string{"vm01ev01", "vm01ev02"})
	nextEvent(t, rh, StatusWaiting, "")

	for _, pw := range []string{"", "wrong", testPasswords["bob"]} {
		_, err := c.AdminList(pw)
		wantDaemonErr(t, err, MsgAuthFailed)
		wantDaemonErr(t, c.AdminSet(pw, rh.ID, StatusAborted, "x"), MsgAuthFailed)
		wantDaemonErr(t, c.AdminDelete(pw, rh.ID), MsgAuthFailed)
		_, err = c.AdminClear(pw)
		wantDaemonErr(t, err, MsgAuthFailed)
		wantDaemonErr(t, c.AdminSetPassword(pw, "bob", "newpw"), MsgAuthFailed)
		h.srv.thr.ok(AdminUser) // 잠금이 걸리지 않게 실패 횟수를 지움
	}
	if h.job(rh.ID).Status != StatusWaiting {
		t.Fatal("거부된 관리자 요청이 상태를 바꿈")
	}

	apw := testPasswords["admin"]
	all, err := c.AdminList(apw)
	if err != nil || len(all) != 1 || len(all[0].Targets) != 2 {
		t.Fatalf("AdminList = %+v, %v", all, err)
	}
	if err := c.AdminSet(apw, rh.ID, StatusApproved, ""); err != nil {
		t.Fatal(err)
	}
	ev := nextEvent(t, rh, StatusApproved, ReasonAdminSet)
	if ev.By != ByAdmin {
		t.Errorf("관리자 승인 by = %q", ev.By)
	}
	if err := c.AdminSet(apw, rh.ID, StatusAborted, "테스트"); err != nil {
		t.Fatal(err)
	}
	nextEvent(t, rh, StatusAborted, ReasonAdminSet+": 테스트")
	expectClosed(t, rh)
	wantDaemonErr(t, c.AdminSet(apw, rh.ID, StatusWaiting, ""), "대기로는")
	wantDaemonErr(t, c.AdminSet(apw, rh.ID, "이상한값", ""), "승인 또는 중단")

	// 기다리는 작업자가 있는 작업 삭제 → 중단 이벤트
	rh2 := h.run(c, "alice", "bob", []string{"vm01ev01"})
	nextEvent(t, rh2, StatusWaiting, "")
	if err := c.AdminDelete(apw, rh2.ID); err != nil {
		t.Fatal(err)
	}
	nextEvent(t, rh2, StatusAborted, ReasonAdminDelete)
	wantDaemonErr(t, c.AdminDelete(apw, rh2.ID), "찾을 수 없음")

	rh3 := h.run(c, "alice", "bob", []string{"vm01ev01"})
	nextEvent(t, rh3, StatusWaiting, "")
	n, err := c.AdminClear(apw)
	if err != nil || n != 2 { // rh(중단) + rh3(대기)
		t.Fatalf("AdminClear = %d, %v", n, err)
	}
	nextEvent(t, rh3, StatusAborted, ReasonAdminClear)

	// 관리자 비밀번호 재설정
	if err := c.AdminSetPassword(apw, "bob", "brand_new"); err != nil {
		t.Fatal(err)
	}
	rh4 := h.run(c, "alice", "bob", []string{"vm01ev01"})
	wantDaemonErr(t, c.Approve(rh4.ID, "bob", testPasswords["bob"]), MsgAuthFailed)
	if err := c.Approve(rh4.ID, "bob", "brand_new"); err != nil {
		t.Fatal(err)
	}
}

func TestAuthModeOff(t *testing.T) {
	h := newHarness(t, func(h *harness, cfg *Config) {
		cfg.AuthMode = AuthOff
		cfg.TargetDir = "" // 대상 파일 검사도 생략
		os.Remove(h.authPath)
	})
	c := h.client()
	rh := h.run(c, "dave", "erin", []string{"anything01"}) // 미등록 사용자·대상 파일 없음
	if err := c.Approve(rh.ID, "erin", ""); err != nil {
		t.Fatal(err)
	}
	if err := rh.Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := c.Start(rh.ID, ""); err != nil { // token 검사도 하지 않음
		t.Fatal(err)
	}
	if _, err := c.AdminList(""); err != nil {
		t.Fatal(err)
	}
}

// sleepRecorder 는 주입용 sleep: 부른 횟수와 시간을 기록하고 실제로는 기다리지 않는다.
type sleepRecorder struct {
	mu    sync.Mutex
	calls []time.Duration
}

func (r *sleepRecorder) Sleep(d time.Duration) {
	r.mu.Lock()
	r.calls = append(r.calls, d)
	r.mu.Unlock()
}

func (r *sleepRecorder) n() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.calls)
}

// 잠금 대신 지연: 5번 연속 실패 뒤의 시도는 1초 지연을 거치지만, 올바른 비밀번호는 그때도 통과하고 횟수를 지운다.
func TestThrottleDelaysButCorrectPasswordWins(t *testing.T) {
	rec := &sleepRecorder{}
	h := newHarness(t, func(h *harness, cfg *Config) { cfg.Sleep = rec.Sleep })
	c := h.client()
	rh := h.run(c, "alice", "bob", []string{"vm01ev01"})
	for i := 1; i <= 5; i++ {
		wantDaemonErr(t, c.Approve(rh.ID, "bob", "guess_pw"), MsgAuthFailed)
	}
	if rec.n() != 0 {
		t.Fatalf("5번째 실패까지는 지연 없음: %d", rec.n())
	}
	// 6번째부터는 지연 뒤 검사(문구는 그대로 일반 실패)
	wantDaemonErr(t, c.Approve(rh.ID, "bob", "guess_pw"), MsgAuthFailed)
	if rec.n() != 1 || rec.calls[0] != DefaultAuthDelay {
		t.Fatalf("6번째 시도는 %s 지연이어야 함: %v", DefaultAuthDelay, rec.calls)
	}
	// 다른 사용자는 영향 없음
	if _, err := c.AdminList(testPasswords["admin"]); err != nil {
		t.Fatal(err)
	}
	if rec.n() != 1 {
		t.Fatalf("다른 이름은 지연되면 안 됨: %d", rec.n())
	}
	// 지연 중에도 올바른 비밀번호는 통과(잠금 DoS 없음)하고 횟수를 지운다
	if err := c.Approve(rh.ID, "bob", testPasswords["bob"]); err != nil {
		t.Fatalf("지연 중 올바른 비밀번호가 거부됨: %v", err)
	}
	if rec.n() != 2 {
		t.Fatalf("지연 중 시도는 성공이어도 지연을 거침: %d", rec.n())
	}
	if n := h.srv.thr.failures("bob"); n != 0 {
		t.Fatalf("성공 후 연속 실패 횟수 = %d", n)
	}
	rh2 := h.run(c, "alice", "bob", []string{"vm01ev02"})
	wantDaemonErr(t, c.Approve(rh2.ID, "bob", "guess_pw"), MsgAuthFailed)
	if rec.n() != 2 {
		t.Fatalf("횟수가 지워졌으면 다음 실패는 지연 없음: %d", rec.n())
	}
	// 목록 밖 이름은 이름별 기록을 남기지 않는다(아무 이름이나 보내 맵을 키울 수 없음)
	for i := 0; i < 10; i++ {
		c.Approve(rh2.ID, fmt.Sprintf("zed%d", i), "x_guess")
	}
	h.srv.thr.mu.Lock()
	tracked := len(h.srv.thr.users)
	h.srv.thr.mu.Unlock()
	if tracked > 1 { // bob 만
		t.Fatalf("목록 밖 이름까지 기록함: %d", tracked)
	}
}

func TestAuthFileMissingOrLoose(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix 권한 검사")
	}
	h := newHarness(t, nil)
	c := h.client()
	rh := h.run(c, "alice", "bob", []string{"vm01ev01"})

	os.Chmod(h.authPath, 0o640) // 그룹이 읽을 수 있음 → 아무도 인증하지 않음
	wantDaemonErr(t, c.Approve(rh.ID, "bob", testPasswords["bob"]), "chmod 600")
	_, err := c.Run("alice", "bob", []string{"vm01ev01"})
	wantDaemonErr(t, err, "인증 불가")
	_, err = c.UserRegistered("bob")
	wantDaemonErr(t, err, "인증 불가")
	wantDaemonErr(t, c.Register("dave", "pw_dave_1"), "변경하지 않음") // 손상된 파일은 덮어쓰지 않음

	os.Chmod(h.authPath, 0o600) // 권한을 고치면 재시작 없이 다시 동작
	if err := c.Approve(rh.ID, "bob", testPasswords["bob"]); err != nil {
		t.Fatal(err)
	}

	os.Remove(h.authPath)
	_, err = c.AdminList(testPasswords["admin"])
	wantDaemonErr(t, err, MsgNoAdmin) // 파일이 없으면 admin 도 없음 → install.sh 안내
	wantDaemonErr(t, c.Cancel(rh.ID, "bob", testPasswords["bob"]), "없음")
}

func TestAuthFileFormatErrors(t *testing.T) {
	cases := map[string]string{
		"등호 없음":  "alice\n",
		"빈 비밀번호": "alice=\n",
		"중복":     "alice=a\nalice=b\n",
		"이름 규칙":  "Alice=a\n",
	}
	for name, body := range cases {
		if _, err := parseAuthFile([]byte(body)); err == nil {
			t.Errorf("%s: 오류여야 함", name)
		} else if strings.Contains(err.Error(), "=a") || strings.Contains(err.Error(), "=b") {
			t.Errorf("%s: 오류에 비밀번호가 들어감: %v", name, err)
		}
	}
	m, err := parseAuthFile([]byte("# c\n\n  bob = x=y z  \n"))
	if err != nil || m["bob"] != "x=y z" {
		t.Fatalf("첫 '=' 에서 나누기: %v %v", m, err)
	}
}

func TestAuthFileReloadOnChange(t *testing.T) {
	h := newHarness(t, nil)
	c := h.client()
	if ok, _ := c.UserRegistered("dave"); ok {
		t.Fatal("dave 는 아직 없어야 함")
	}
	users := map[string]string{"dave": "pw_dave"}
	for u, p := range testPasswords {
		users[u] = p
	}
	writeAuth(t, h.authPath, users) // 관리자가 손으로 편집
	waitFor(t, 2*time.Second, "편집 반영", func() bool { ok, _ := c.UserRegistered("dave"); return ok })
}

func TestStartRequiresToken(t *testing.T) {
	h := newHarness(t, nil)
	c := h.client()
	rh := h.run(c, "alice", "bob", []string{"vm01ev01"})
	wantDaemonErr(t, c.Start(rh.ID, rh.Token), "승인 상태가 아니") // 승인 전
	if err := c.Approve(rh.ID, "bob", testPasswords["bob"]); err != nil {
		t.Fatal(err)
	}
	wantDaemonErr(t, c.Start(rh.ID, ""), MsgAuthFailed)
	wantDaemonErr(t, c.Start(rh.ID, strings.Repeat("0", 32)), MsgAuthFailed)
	other := h.run(c, "alice", "bob", []string{"vm01ev02"})
	wantDaemonErr(t, c.Start(rh.ID, other.Token), MsgAuthFailed) // 다른 작업의 token
	wantDaemonErr(t, c.Start("nosuch", rh.Token), MsgAuthFailed)
	if j := h.job(rh.ID); j.StartedAt != nil || j.Status != StatusApproved {
		t.Fatalf("잘못된 token 이 승인을 소모함: %+v", j)
	}
	if err := c.Start(rh.ID, rh.Token); err != nil {
		t.Fatal(err)
	}
}

// ---- 2분 규칙 ----

func TestStartAfterTTLExpires(t *testing.T) {
	clk := newFakeClock()
	h := newHarness(t, func(h *harness, cfg *Config) { cfg.Now = clk.Now })
	c := h.client()

	ok := h.run(c, "alice", "bob", []string{"vm01ev01"})
	c.Approve(ok.ID, "bob", testPasswords["bob"])
	clk.Add(2 * time.Minute) // 정확히 2분은 허용
	if err := c.Start(ok.ID, ok.Token); err != nil {
		t.Fatalf("2분 정각 Start: %v", err)
	}

	rh := h.run(c, "alice", "bob", []string{"vm01ev02"})
	nextEvent(t, rh, StatusWaiting, "")
	if err := c.Approve(rh.ID, "bob", testPasswords["bob"]); err != nil {
		t.Fatal(err)
	}
	nextEvent(t, rh, StatusApproved, "")
	clk.Add(2*time.Minute + time.Second)
	wantDaemonErr(t, c.Start(rh.ID, rh.Token), ReasonExpired)
	nextEvent(t, rh, StatusAborted, ReasonExpired)
	if j := h.job(rh.ID); j.Status != StatusAborted || j.Reason != ReasonExpired {
		t.Fatalf("만료 후 작업 = %+v", j)
	}
	// 중단된 뒤에도 시작은 계속 거부되고, 올바른 token 이면 실제 중단 사유를 알려 준다(틀린 token 은 인증 실패)
	wantDaemonErr(t, c.Start(rh.ID, rh.Token), ReasonExpired)
	wantDaemonErr(t, c.Start(rh.ID, "bad"), MsgAuthFailed)
}

func TestSweeperExpiresUnstarted(t *testing.T) {
	h := newHarness(t, func(h *harness, cfg *Config) {
		cfg.ApprovalTTL = 150 * time.Millisecond
		cfg.SweepInterval = 20 * time.Millisecond
	})
	c := h.client()
	idle := h.run(c, "alice", "bob", []string{"vm01ev01"})
	started := h.run(c, "alice", "bob", []string{"vm01ev02"})
	waiting := h.run(c, "alice", "bob", []string{"vm02ev01"})
	c.Approve(idle.ID, "bob", testPasswords["bob"])
	c.Approve(started.ID, "bob", testPasswords["bob"])
	if err := c.Start(started.ID, started.Token); err != nil {
		t.Fatal(err)
	}
	nextEvent(t, idle, StatusWaiting, "")
	nextEvent(t, idle, StatusApproved, "")
	ev := nextEvent(t, idle, StatusAborted, ReasonExpired)
	if ev.By != BySystem {
		t.Errorf("만료 이벤트 by = %q", ev.By)
	}
	expectClosed(t, idle)
	if j := h.job(idle.ID); j.Status != StatusAborted || j.Reason != ReasonExpired {
		t.Fatalf("스위퍼 만료 = %+v", j)
	}
	// 스위퍼가 먼저 만료시킨 뒤의 start 도 "인증 실패" 가 아니라 만료 사유를 받아야 한다
	wantDaemonErr(t, c.Start(idle.ID, idle.Token), ReasonExpired)
	wantDaemonErr(t, c.Start(idle.ID, "bad"), MsgAuthFailed)
	time.Sleep(300 * time.Millisecond)
	if j := h.job(started.ID); j.Status != StatusApproved {
		t.Fatalf("시작한 작업은 만료되면 안 됨: %+v", j)
	}
	if j := h.job(waiting.ID); j.Status != StatusWaiting {
		t.Fatalf("대기 작업에는 시간 제한이 없음: %+v", j)
	}
}
