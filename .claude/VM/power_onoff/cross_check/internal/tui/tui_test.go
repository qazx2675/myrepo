package tui

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"cross_check/internal/daemon"
	"cross_check/internal/store"
)

// ---- 가짜 데몬 클라이언트 ----

type fakeClient struct {
	mu        sync.Mutex
	pw        map[string]string // 등록된 사용자 → 비밀번호
	jobs      []daemon.JobView
	listCalls int
	calls     []string
	onList    func(n int) // List 가 n 번째로 불릴 때(감시 모드 시험용)
	connErr   error       // 모든 호출이 통신 오류
}

func newFake() *fakeClient {
	return &fakeClient{pw: map[string]string{}}
}

func derr(msg string) error { return &daemon.DaemonError{Msg: msg} }

func (f *fakeClient) record(s string) {
	f.calls = append(f.calls, s)
}

func (f *fakeClient) UserRegistered(user string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.connErr != nil {
		return false, f.connErr
	}
	_, ok := f.pw[user]
	return ok, nil
}

func (f *fakeClient) Register(user, password string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record("register " + user)
	if _, ok := f.pw[user]; ok {
		return derr(daemon.ErrAlreadyRegistered.Error())
	}
	f.pw[user] = password
	return nil
}

func (f *fakeClient) auth(user, password string) error {
	if want, ok := f.pw[user]; !ok || want != password {
		return derr(daemon.MsgAuthFailed)
	}
	return nil
}

func (f *fakeClient) ChangePassword(user, oldPassword, newPassword string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record("change_password " + user)
	if err := f.auth(user, oldPassword); err != nil {
		return err
	}
	f.pw[user] = newPassword
	return nil
}

func (f *fakeClient) List(user string) ([]daemon.JobView, error) {
	f.mu.Lock()
	f.listCalls++
	n := f.listCalls
	cb := f.onList
	f.mu.Unlock()
	if cb != nil {
		cb(n)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.connErr != nil {
		return nil, f.connErr
	}
	var out []daemon.JobView
	for _, j := range f.jobs {
		if j.User == user || j.Checker == user {
			out = append(out, j)
		}
	}
	return out, nil
}

func (f *fakeClient) find(id string) *daemon.JobView {
	for i := range f.jobs {
		if f.jobs[i].ID == id {
			return &f.jobs[i]
		}
	}
	return nil
}

func (f *fakeClient) Get(id, user, password string) (daemon.JobView, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	j := f.find(id)
	if j == nil || (j.User != user && j.Checker != user) {
		return daemon.JobView{}, derr("작업이 없거나 이 작업의 작업자·확인자가 아님")
	}
	return *j, nil
}

func (f *fakeClient) Approve(id, user, password string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record("approve " + id + " " + user)
	if err := f.auth(user, password); err != nil {
		return err
	}
	j := f.find(id)
	if j == nil || j.Checker != user {
		return derr("작업이 없거나 이 작업의 확인자가 아님")
	}
	if j.Status != daemon.StatusWaiting {
		return derr("대기 상태가 아니라 승인할 수 없음(현재 " + string(j.Status) + ")")
	}
	now := time.Now()
	j.Status, j.ApprovedAt = daemon.StatusApproved, &now
	return nil
}

func (f *fakeClient) Cancel(id, user, password string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record("cancel " + id + " " + user)
	if err := f.auth(user, password); err != nil {
		return err
	}
	j := f.find(id)
	if j == nil || j.Checker != user {
		return derr("작업이 없거나 이 작업의 확인자가 아님")
	}
	j.Status, j.Reason = daemon.StatusAborted, daemon.ReasonCancel(user)
	return nil
}

func (f *fakeClient) adminAuth(pw string) error { return f.auth(daemon.AdminUser, pw) }

func (f *fakeClient) AdminList(pw string) ([]store.Job, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record("admin_list")
	if err := f.adminAuth(pw); err != nil {
		return nil, err
	}
	var out []store.Job
	for _, j := range f.jobs {
		out = append(out, store.Job{ID: j.ID, User: j.User, Checker: j.Checker, Targets: j.Targets,
			Status: j.Status, Reason: j.Reason, CreatedAt: j.CreatedAt, ApprovedAt: j.ApprovedAt, StartedAt: j.StartedAt,
			WorkerUID: 0, CheckerUID: -1})
	}
	return out, nil
}

func (f *fakeClient) AdminSet(pw, id string, status store.Status, reason string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record("admin_set " + id + " " + string(status) + " " + reason)
	if err := f.adminAuth(pw); err != nil {
		return err
	}
	j := f.find(id)
	if j == nil {
		return derr("작업을 찾을 수 없음: " + id)
	}
	j.Status = status
	j.Reason = daemon.ReasonAdminSet + ": " + reason
	return nil
}

func (f *fakeClient) AdminDelete(pw, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record("admin_delete " + id)
	if err := f.adminAuth(pw); err != nil {
		return err
	}
	for i := range f.jobs {
		if f.jobs[i].ID == id {
			f.jobs = append(f.jobs[:i], f.jobs[i+1:]...)
			return nil
		}
	}
	return derr("작업을 찾을 수 없음: " + id)
}

func (f *fakeClient) AdminClear(pw string) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record("admin_clear")
	if err := f.adminAuth(pw); err != nil {
		return 0, err
	}
	n := len(f.jobs)
	f.jobs = nil
	return n, nil
}

func (f *fakeClient) AdminSetPassword(pw, user, newPassword string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record("admin_set_password " + user)
	if err := f.adminAuth(pw); err != nil {
		return err
	}
	f.pw[user] = newPassword
	return nil
}

func (f *fakeClient) hasCall(prefix string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, c := range f.calls {
		if strings.HasPrefix(c, prefix) {
			return true
		}
	}
	return false
}

// ---- 도구 ----

var t0 = time.Date(2026, 10, 24, 9, 0, 0, 0, time.Local)

func job(id, user, checker string, st store.Status, reason string, targets ...string) daemon.JobView {
	return daemon.JobView{ID: id, User: user, Checker: checker, Status: st, Reason: reason,
		TargetCount: len(targets), Targets: targets, CreatedAt: t0, UpdatedAt: t0}
}

// runUser 는 각본 입력으로 사용자 화면을 돌린다. hide 호출 횟수도 센다.
func runUser(t *testing.T, f *fakeClient, user, input string, opt Options) (string, int, int) {
	t.Helper()
	var out bytes.Buffer
	hides := 0
	opt.HideInput = func() func() { hides++; return nil }
	if opt.Now == nil {
		opt.Now = func() time.Time { return t0.Add(30 * time.Second) }
	}
	s := New(strings.NewReader(input), &out, f, opt)
	code := s.RunUser(context.Background(), user)
	return out.String(), code, hides
}

func mustContain(t *testing.T, out string, subs ...string) {
	t.Helper()
	for _, s := range subs {
		if !strings.Contains(out, s) {
			t.Fatalf("출력에 %q 가 없음:\n%s", s, out)
		}
	}
}

// ---- 사용자 화면 ----

func TestFirstRegistrationMismatchThenOK(t *testing.T) {
	f := newFake()
	out, code, hides := runUser(t, f, "alice", "secret1\nsecret2\nsecret1\nsecret1\nq\n", Options{})
	if code != ExitOK {
		t.Fatalf("code=%d\n%s", code, out)
	}
	mustContain(t, out, "[첫 실행]", "두 입력이 서로 다릅니다", "[등록 완료]", "내 작업 (작업자)", "내가 확인자인 작업")
	if f.pw["alice"] != "secret1" {
		t.Fatalf("등록된 비밀번호 = %q", f.pw["alice"])
	}
	if hides != 4 {
		t.Fatalf("비밀번호 입력 4번 모두 에코를 꺼야 함: %d", hides)
	}
	if strings.Contains(out, "secret1") || strings.Contains(out, "secret2") {
		t.Fatalf("비밀번호가 화면에 나옴:\n%s", out)
	}
}

func TestRegistrationRuleAndConflict(t *testing.T) {
	f := newFake()
	// 앞 공백·6자 미만은 규칙 위반 → 다시 입력 → 3번 모두 실패하면 종료 1
	out, code, _ := runUser(t, f, "bob", " abcdef\n abcdef\nx\ny\nabc\nabc\n", Options{})
	if code != ExitFail || f.hasCall("register") {
		t.Fatalf("code=%d calls=%v\n%s", code, f.calls, out)
	}
	mustContain(t, out, "앞뒤에 공백", "6~128자", "규칙: 6~128자", "3번 실패")

	// 등록 충돌: 그사이 다른 곳에서 먼저 등록 → 안내 후 화면은 계속
	g := &conflictClient{fakeClient: newFake()}
	var buf bytes.Buffer
	s := New(strings.NewReader("pw1pw1\npw1pw1\nq\n"), &buf, g, Options{})
	if c := s.RunUser(context.Background(), "carol"); c != ExitOK {
		t.Fatalf("충돌 후에도 화면은 계속되어야 함: %d\n%s", c, buf.String())
	}
	mustContain(t, buf.String(), "[등록 충돌]")
}

// conflictClient 는 UserRegistered 는 false 인데 Register 하면 이미 등록됐다고 답한다(동시 등록 경쟁).
type conflictClient struct{ *fakeClient }

func (c *conflictClient) UserRegistered(string) (bool, error) { return false, nil }
func (c *conflictClient) Register(string, string) error {
	return derr(daemon.ErrAlreadyRegistered.Error())
}

func TestListRendering(t *testing.T) {
	f := newFake()
	f.pw["alice"] = "a"
	appr := t0.Add(10 * time.Second)
	j2 := job("job2", "alice", "bob", daemon.StatusApproved, "", "vm03ev01")
	j2.ApprovedAt = &appr
	f.jobs = []daemon.JobView{
		job("job1", "alice", "bob", daemon.StatusWaiting, "", "vm01ev01", "vm02ev01"),
		j2,
		job("job3", "alice", "bob", daemon.StatusAborted, daemon.ReasonCancel("bob"), "vm01ev01"),
		job("job4", "alice", "carol", daemon.StatusAborted, daemon.ReasonExpired, "vm01ev01"),
		job("job5", "alice", "bob", daemon.StatusAborted, daemon.ReasonWorkerGone, "vm01ev01"),
		job("job6", "alice", "bob", daemon.StatusAborted, daemon.ReasonAdminSet+": 잘못된 기록", "vm01ev01"),
		job("job7", "dave", "alice", daemon.StatusWaiting, "", "vm09ev01"),
	}
	out, code, _ := runUser(t, f, "alice", "q\n", Options{})
	if code != ExitOK {
		t.Fatalf("code=%d", code)
	}
	// 작업자에게 보이는 상태·사유: 확인자 취소, 2분 만료, 작업자 스크립트 종료, 관리자 변경
	mustContain(t, out, "확인자 bob 가 취소", "승인 후 2분 내 미진행", "작업자 스크립트 종료", "관리자 변경: 잘못된 기록")
	// 승인된 내 작업은 눈에 띄게 표시
	mustContain(t, out, "★ [승인] 내 작업 job2", "승인 ★", "남은 100초")
	mustContain(t, out, "내 작업 (작업자) — 6건", "내가 확인자인 작업 — 1건", "내 승인을 기다리는 작업 1건")
	// 표 열이 맞는지: 같은 표의 행에서 ID 열 시작 위치가 같아야 한다
	var cols []int
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "[") && strings.Contains(line, "job") {
			i := strings.Index(line, "job")
			cols = append(cols, dispWidth(line[:i]))
		}
	}
	if len(cols) != 7 {
		t.Fatalf("행 수 = %d\n%s", len(cols), out)
	}
	for _, c := range cols {
		if c != cols[0] {
			t.Fatalf("ID 열 위치가 다름: %v\n%s", cols, out)
		}
	}
	// 번호는 두 표에 걸쳐 이어진다: 확인자 표 첫 행이 [7]
	mustContain(t, out, "[7]   job7  dave")
}

func TestApproveWrongThenRightPassword(t *testing.T) {
	f := newFake()
	f.pw["bob"] = "bobpw"
	f.pw["alice"] = "alicepw"
	f.jobs = []daemon.JobView{job("j1", "alice", "bob", daemon.StatusWaiting, "", "vm01ev01", "vm02ev01")}
	in := "1\na\nwrong\na\nbobpw\nq\n"
	out, code, hides := runUser(t, f, "bob", in, Options{})
	if code != ExitOK {
		t.Fatalf("code=%d\n%s", code, out)
	}
	mustContain(t, out, "내 역할   : 확인자", "vm01ev01", "vm02ev01", "승인하려면 패스워드를 입력하세요",
		"승인 실패: 인증 실패", "[승인 완료] 작업 j1", "[필수 정책]")
	if hides != 2 {
		t.Fatalf("hide 호출 = %d", hides)
	}
	if f.jobs[0].Status != daemon.StatusApproved {
		t.Fatalf("승인 안 됨: %+v", f.jobs[0])
	}
	if strings.Contains(out, "bobpw") || strings.Contains(out, "wrong\n") {
		t.Fatalf("비밀번호가 화면에 나옴:\n%s", out)
	}
}

func TestApproveLockedNoInfiniteLoop(t *testing.T) {
	f := newFake()
	f.pw["bob"] = "bobpw"
	f.jobs = []daemon.JobView{job("j1", "alice", "bob", daemon.StatusWaiting, "", "vm01ev01")}
	// 입력이 끝나면(EOF) 반드시 끝난다
	out, code, _ := runUser(t, f, "bob", "o j1\na\nx\na\ny\n", Options{})
	if code != ExitOK {
		t.Fatalf("code=%d", code)
	}
	if strings.Count(out, "승인 실패: 인증 실패") != 2 {
		t.Fatalf("두 번 실패해야 함:\n%s", out)
	}
}

func TestApproveAlreadyAbortedReturnsToList(t *testing.T) {
	f := newFake()
	f.pw["bob"] = "bobpw"
	f.jobs = []daemon.JobView{job("j1", "alice", "bob", daemon.StatusWaiting, "", "vm01ev01")}
	var once sync.Once
	g := &abortBeforeApprove{fakeClient: f, once: &once}
	var buf bytes.Buffer
	s := New(strings.NewReader("1\na\nbobpw\nq\n"), &buf, g, Options{})
	if c := s.RunUser(context.Background(), "bob"); c != ExitOK {
		t.Fatalf("code=%d", c)
	}
	mustContain(t, buf.String(), "대기 상태가 아니라 승인할 수 없음(현재 중단)", "작업자 스크립트 종료")
}

// abortBeforeApprove 는 승인 직전에 작업자가 끊겨 중단된 상황을 흉내 낸다.
type abortBeforeApprove struct {
	*fakeClient
	once *sync.Once
}

func (a *abortBeforeApprove) Approve(id, user, pw string) error {
	a.once.Do(func() {
		a.mu.Lock()
		a.jobs[0].Status, a.jobs[0].Reason = daemon.StatusAborted, daemon.ReasonWorkerGone
		a.mu.Unlock()
	})
	return a.fakeClient.Approve(id, user, pw)
}

func TestCancelFlow(t *testing.T) {
	f := newFake()
	f.pw["bob"] = "bobpw"
	f.jobs = []daemon.JobView{job("j1", "alice", "bob", daemon.StatusWaiting, "", "vm01ev01")}
	out, code, _ := runUser(t, f, "bob", "o 1\nc\n\nc\nbobpw\nq\n", Options{})
	if code != ExitOK {
		t.Fatalf("code=%d", code)
	}
	mustContain(t, out, "입력이 없어 취소하지 않았습니다", "[취소 완료] 작업 j1")
	if f.jobs[0].Status != daemon.StatusAborted || f.jobs[0].Reason != "확인자 bob 가 취소" {
		t.Fatalf("%+v", f.jobs[0])
	}
	// 취소 뒤 목록에 사유가 보인다
	if i := strings.LastIndex(out, "확인자 bob 가 취소"); i < strings.Index(out, "[취소 완료]") {
		t.Fatalf("취소 뒤 목록에 사유가 보여야 함:\n%s", out)
	}
}

func TestWorkerCannotApproveOwnJobAndNonMemberCannotOpen(t *testing.T) {
	f := newFake()
	f.pw["alice"] = "a"
	f.pw["carol"] = "c"
	f.jobs = []daemon.JobView{job("j1", "alice", "bob", daemon.StatusWaiting, "", "vm01ev01")}
	// 작업자는 상세만 보고 a 명령이 없다(Enter 로 돌아감 → a 는 알 수 없는 명령)
	out, _, _ := runUser(t, f, "alice", "1\n\na\nq\n", Options{})
	mustContain(t, out, "내 역할   : 작업자", "Enter 를 누르면 목록으로", "알 수 없는 명령")
	if f.hasCall("approve") {
		t.Fatal("작업자가 승인을 보냄")
	}
	// 제3자는 열 수도 없다
	out, _, _ = runUser(t, f, "carol", "o j1\nq\n", Options{})
	mustContain(t, out, "작업 j1 를 열 수 없습니다: 작업이 없거나")
	if f.hasCall("approve") {
		t.Fatal("제3자가 승인을 보냄")
	}
}

func TestWatchRefreshShowsTransition(t *testing.T) {
	f := newFake()
	f.pw["alice"] = "a"
	f.jobs = []daemon.JobView{
		job("j1", "alice", "bob", daemon.StatusWaiting, "", "vm01ev01"),
		job("j2", "alice", "bob", daemon.StatusWaiting, "", "vm02ev01"),
	}
	pr, pw := io.Pipe()
	f.onList = func(n int) {
		switch n {
		case 3: // 감시 중 두 번째 갱신: 확인자가 j1 승인, j2 는 2분 만료와 무관하게 취소
			f.mu.Lock()
			now := t0.Add(20 * time.Second)
			f.jobs[0].Status, f.jobs[0].ApprovedAt = daemon.StatusApproved, &now
			f.jobs[1].Status, f.jobs[1].Reason = daemon.StatusAborted, daemon.ReasonCancel("bob")
			f.mu.Unlock()
		case 4:
			go func() {
				pw.Write([]byte("\nq\n"))
				pw.Close()
			}()
		}
	}
	go pw.Write([]byte("w\n"))
	var out bytes.Buffer
	s := New(pr, &out, f, Options{WatchInterval: 20 * time.Millisecond, Now: func() time.Time { return t0.Add(30 * time.Second) }})
	done := make(chan int)
	go func() { done <- s.RunUser(context.Background(), "alice") }()
	select {
	case code := <-done:
		if code != ExitOK {
			t.Fatalf("code=%d", code)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("감시 모드가 끝나지 않음")
	}
	o := out.String()
	mustContain(t, o, "[감시 모드]", "★ [승인] 내 작업 j1", "[중단] 내 작업 j2 — 확인자 bob 가 취소")
	if f.listCalls < 4 {
		t.Fatalf("자동 갱신이 안 됨: List %d번", f.listCalls)
	}
}

func TestChangePassword(t *testing.T) {
	f := newFake()
	f.pw["alice"] = "old"
	out, _, _ := runUser(t, f, "alice", "p\nbad\nnew_one1\nnew_one1\np\nold\nnew_one1\nnew_two2\np\nold\nshort\nshort\np\nold\nnew_one1\nnew_one1\nq\n", Options{})
	mustContain(t, out, "변경 실패: 인증 실패", "두 입력이 서로 다릅니다", "6~128자", "[변경 완료]")
	if f.pw["alice"] != "new_one1" {
		t.Fatalf("pw=%q", f.pw["alice"])
	}
}

func TestDaemonDownAndCtrlC(t *testing.T) {
	f := newFake()
	f.connErr = errors.New("cross_check 데몬 연결 실패")
	_, code, _ := runUser(t, f, "alice", "q\n", Options{})
	if code != ExitError {
		t.Fatalf("통신 오류는 3: %d", code)
	}

	g := newFake()
	g.pw["alice"] = "a"
	ctx, cancel := context.WithCancel(context.Background())
	pr, _ := io.Pipe() // 입력이 오지 않는다
	var out bytes.Buffer
	s := New(pr, &out, g, Options{})
	done := make(chan int)
	go func() { done <- s.RunUser(ctx, "alice") }()
	time.Sleep(50 * time.Millisecond)
	cancel()
	select {
	case c := <-done:
		if c != ExitInterrupt {
			t.Fatalf("Ctrl+C 는 130: %d", c)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Ctrl+C 에 끝나지 않음")
	}
}

// ---- 관리자 화면 ----

func adminOut(t *testing.T, f *fakeClient, input string) (string, int) {
	t.Helper()
	var out bytes.Buffer
	s := New(strings.NewReader(input), &out, f, Options{Now: func() time.Time { return t0 }})
	code := s.RunAdmin(context.Background())
	return out.String(), code
}

// admin 항목이 없으면 화면에서 만들지 않고(선점 방지) install.sh / auth.conf 안내만 하고 종료 1.
func TestAdminMissingEntryNoRegistration(t *testing.T) {
	f := newFake()
	out, code := adminOut(t, f, "adm_pw_1\nadm_pw_1\nq\n")
	if code != ExitFail {
		t.Fatalf("code=%d\n%s", code, out)
	}
	mustContain(t, out, "관리자(admin) 항목이 auth.conf 에 없어", "install.sh", "admin=<패스워드>")
	if f.hasCall("register") || f.hasCall("admin_list") {
		t.Fatalf("등록·로그인 시도를 하면 안 됨: %v", f.calls)
	}
	if _, ok := f.pw["admin"]; ok {
		t.Fatal("admin 이 만들어짐")
	}
	if strings.Contains(out, "[첫 실행]") || strings.Contains(out, "새 패스워드") {
		t.Fatalf("첫 실행 등록 화면이 나오면 안 됨:\n%s", out)
	}
}

// 로그인 중 데몬이 admin 항목 없음(MsgNoAdmin)으로 답하면 같은 안내 후 종료 1.
func TestAdminLoginNoAdminFromDaemon(t *testing.T) {
	g := &noAdminClient{fakeClient: newFake()}
	var out bytes.Buffer
	s := New(strings.NewReader("whatever\n"), &out, g, Options{})
	if c := s.RunAdmin(context.Background()); c != ExitFail {
		t.Fatalf("code=%d\n%s", c, out.String())
	}
	mustContain(t, out.String(), "install.sh")
}

// noAdminClient 는 admin 이 등록된 것처럼 답하지만 관리자 op 는 MsgNoAdmin 으로 거부한다(그사이 줄이 지워진 경우).
type noAdminClient struct{ *fakeClient }

func (c *noAdminClient) UserRegistered(string) (bool, error) { return true, nil }
func (c *noAdminClient) AdminList(string) ([]store.Job, error) {
	return nil, derr(daemon.MsgNoAdmin)
}

func TestAdminLoginFailsThreeTimes(t *testing.T) {
	f := newFake()
	f.pw["admin"] = "adm"
	out, code := adminOut(t, f, "x\ny\nz\n1\n")
	if code != ExitFail {
		t.Fatalf("code=%d\n%s", code, out)
	}
	mustContain(t, out, "인증 실패 (3/3)", "3번 실패")
}

func TestAdminMenuFlows(t *testing.T) {
	f := newFake()
	f.pw["admin"] = "adm"
	f.pw["alice"] = "a"
	f.jobs = []daemon.JobView{
		job("j1", "alice", "bob", daemon.StatusWaiting, "", "vm01ev01", "vm02ev01"),
		job("j2", "alice", "bob", daemon.StatusWaiting, "", "vm03ev01"),
		job("j3", "carol", "bob", daemon.StatusAborted, daemon.ReasonWorkerGone, "vm04ev01"),
	}
	in := strings.Join([]string{
		"adm",
		"1",                     // 전체 목록
		"2", "1", "2", "잘못된 기록", // [1]=j1 → 중단
		"2", "j2", "1", "", // j2 → 승인
		"2", "j2", "9", // 잘못된 선택
		"3", "3", "n", // 삭제 취소
		"3", "3", "y", // j3 삭제
		"5", "alice", "np", "np", // 6자 미만 → 재설정 안 함
		"5", "alice", "new_pw_1", "new_pw_1", // 재설정
		"6",        // 사용자 보기(지원 안 함)
		"4", "아니오", // 정리 취소
		"4", "전체 정리", // 정리
		"7", "q",
	}, "\n") + "\n"
	out, code := adminOut(t, f, in)
	if code != ExitOK {
		t.Fatalf("code=%d\n%s", code, out)
	}
	mustContain(t, out, "[전체 작업] 3건", "[작업별 대상]", "j1 (2개): vm01ev01, vm02ev01",
		"[변경 완료] 작업 j1 → 중단", "[변경 완료] 작업 j2 → 승인", "[필수 정책]", "1 또는 2 를 입력하세요",
		"삭제하지 않았습니다", "[삭제 완료] 작업 j3", "6~128자", "[재설정 완료] alice",
		"등록 사용자 목록 조회 기능이 없어", "alice, bob",
		"확인 문구가 달라", "[정리 완료] 2건 삭제", "1~6 또는 q")
	for _, c := range []string{"admin_set j1 중단 잘못된 기록", "admin_set j2 승인 ", "admin_delete j3", "admin_set_password alice", "admin_clear"} {
		if !f.hasCall(c) {
			t.Fatalf("호출 없음 %q: %v", c, f.calls)
		}
	}
	if f.pw["alice"] != "new_pw_1" || len(f.jobs) != 0 {
		t.Fatalf("pw=%q jobs=%d", f.pw["alice"], len(f.jobs))
	}
}

func TestWriteWorkerList(t *testing.T) {
	appr := t0.Add(5 * time.Second)
	j := job("j1", "alice", "bob", daemon.StatusApproved, "", "vm01ev01", "vm02ev01")
	j.ApprovedAt = &appr
	jobs := []daemon.JobView{j, job("j0", "dave", "alice", daemon.StatusWaiting, "", "x")}
	var buf bytes.Buffer
	WriteWorkerList(&buf, Painter{}, "alice", jobs, 2*time.Minute, t0.Add(10*time.Second), 10)
	mustContain(t, buf.String(), "내 작업 리스트 (작업자 alice, 1건)", "j1", "bob", "승인", "vm01ev01, vm02ev01", "남은 115초")
	if strings.Contains(buf.String(), "j0") {
		t.Fatal("남의 작업(확인자로서)은 작업자 리스트에 넣지 않음")
	}
}

func TestDispWidthAndPolicy(t *testing.T) {
	if dispWidth("대기") != 4 || dispWidth("ab") != 2 || dispWidth(pad("승인", 6)) != 6 {
		t.Fatal("폭 계산 오류")
	}
	if !strings.Contains(PolicyText(3*time.Second), "3초") || strings.Contains(PolicyText(2*time.Minute), "설정") {
		t.Fatal("정책 문구")
	}
}
