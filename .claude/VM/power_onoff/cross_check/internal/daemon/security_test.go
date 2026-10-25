package daemon

// 보안 강화 회귀 시험. 이전 보안 검토의 공격 PoC(가짜 확인자 자기 승인, admin 선점, 잠금 DoS,
// 토큰 재사용, 자원 고갈)가 이제 실패함을 보인다.

import (
	"bufio"
	"errors"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"cross_check/internal/store"
)

// wantExact 는 데몬 오류 문구가 정확히 msg 인지 확인한다(일반 문구로 존재 여부를 숨기는지 보려고).
func wantExact(t *testing.T, err error, msg string) {
	t.Helper()
	var de *DaemonError
	if !errors.As(err, &de) || de.Msg != msg {
		t.Fatalf("오류 문구가 정확히 %q 여야 함: %v", msg, err)
	}
}

func withUsers(m map[string]string, extra map[string]string, drop ...string) map[string]string {
	out := map[string]string{}
	for k, v := range m {
		out[k] = v
	}
	for k, v := range extra {
		out[k] = v
	}
	for _, d := range drop {
		delete(out, d)
	}
	return out
}

// ---- 1. users= 목록: 가짜 확인자로 자기 작업 승인 차단 ----

// PoC: 작업자가 목록 밖 이름(alice_alt)을 등록해 확인자로 지정하고 자기 비밀번호로 승인 → 이제 모두 거부.
func TestRegression_SockpuppetCheckerRejected(t *testing.T) {
	h := newHarness(t, nil)
	c := h.client()
	wantDaemonErr(t, c.Register("alice_alt", "alice_own_pw"), "users= 목록에 없어")
	if ok, _ := c.UserRegistered("alice_alt"); ok {
		t.Fatal("목록 밖 이름이 등록됨")
	}
	_, err := c.Run("alice", "alice_alt", []string{"vm01ev01"})
	wantDaemonErr(t, err, "확인자 alice_alt 는 cross_check.conf 의 users= 목록에 없음")

	// root 가 auth.conf 에 손으로 넣었더라도(목록 밖) run·승인 모두 불가
	writeAuth(t, h.authPath, withUsers(testPasswords, map[string]string{"alice_alt": "alice_own_pw"}))
	waitFor(t, 2*time.Second, "파일 다시 읽기", func() bool { ok, _ := c.UserRegistered("alice_alt"); return ok })
	_, err = c.Run("alice", "alice_alt", []string{"vm01ev01"})
	wantDaemonErr(t, err, "users= 목록에 없음")
	writeTargets(t, h.targetDir, "alice_alt", "vm01ev01\n")
	_, err = c.Run("alice_alt", "bob", []string{"vm01ev01"})
	wantDaemonErr(t, err, "작업자 alice_alt 는 cross_check.conf 의 users= 목록에 없음")
	if n := len(h.srv.st.List()); n != 0 {
		t.Fatalf("거부된 run 이 작업을 만듦: %d", n)
	}
}

// 목록 밖 이름(auth.conf 에 있어도)과 admin 이름은 approve/cancel/change_password/get(비밀번호) 이 일반 실패 문구로 거부된다.
func TestUnlistedNamesDeniedWithGenericMessage(t *testing.T) {
	h := newHarness(t, func(h *harness, cfg *Config) { cfg.AllowedUsers = []string{"alice", "bob"} })
	c := h.client()
	rh := h.run(c, "alice", "bob", []string{"vm01ev01"})
	cpw := testPasswords["carol"] // carol 은 auth.conf 에는 있지만 목록 밖

	_, err := c.Run("alice", "carol", []string{"vm01ev01"})
	wantDaemonErr(t, err, "확인자 carol 는")
	wantExact(t, c.Approve(rh.ID, "carol", cpw, h.paste(rh.ID)), MsgAuthFailed)
	wantExact(t, c.Cancel(rh.ID, "carol", cpw), MsgAuthFailed)
	wantExact(t, c.ChangePassword("carol", cpw, "carol_new_pw"), MsgAuthFailed)
	_, err = c.Get(rh.ID, "carol", cpw)
	wantExact(t, err, MsgAuthFailed)
	wantDaemonErr(t, c.Register("carol", "carol_pw_x"), "users= 목록에 없어")
	// admin 이라는 이름은 관리자 op 에서만 쓴다
	wantExact(t, c.Approve(rh.ID, "admin", testPasswords["admin"], h.paste(rh.ID)), MsgAuthFailed)
	wantExact(t, c.ChangePassword("admin", testPasswords["admin"], "new_admin_pw"), MsgAuthFailed)

	if h.job(rh.ID).Status != StatusWaiting {
		t.Fatal("거부된 요청이 상태를 바꿈")
	}
	if !h.srv.auth.Verify("carol", cpw) || !h.srv.auth.Verify("admin", testPasswords["admin"]) {
		t.Fatal("거부된 change_password 가 비밀번호를 바꿈")
	}
	// 목록 안 이름은 정상
	if err := c.Approve(rh.ID, "bob", testPasswords["bob"], h.paste(rh.ID)); err != nil {
		t.Fatal(err)
	}
}

// auth=off 는 목록을 보지 않는다(시험용). 그래도 admin 등록은 거부한다.
func TestAuthOffIgnoresUserList(t *testing.T) {
	h := newHarness(t, func(h *harness, cfg *Config) {
		cfg.AuthMode = AuthOff
		cfg.AllowedUsers = nil
		cfg.TargetDir = ""
	})
	c := h.client()
	if err := c.Register("zed", "zed_pw_1"); err != nil {
		t.Fatalf("auth=off 에서 목록 밖 등록: %v", err)
	}
	wantDaemonErr(t, c.Register("admin", "admin_pw_1"), "등록할 수 없음")
	rh := h.run(c, "zed", "yan", []string{"x01"})
	if err := c.Approve(rh.ID, "yan", "", h.paste(rh.ID)); err != nil {
		t.Fatal(err)
	}
}

// ---- 2. admin 선점 차단 ----

// PoC: auth.conf 에 admin 이 없을 때 아무나 admin 을 등록해 관리자 승인으로 확인자를 우회 → 이제 불가.
func TestRegression_AdminSquatRejected(t *testing.T) {
	h := newHarness(t, func(h *harness, cfg *Config) {
		writeAuth(t, h.authPath, withUsers(testPasswords, nil, "admin"))
	})
	c := h.client()
	wantDaemonErr(t, c.Register("admin", "mine_pw_1"), "등록할 수 없음")
	if ok, _ := c.UserRegistered("admin"); ok {
		t.Fatal("admin 이 만들어짐")
	}
	rh := h.run(c, "alice", "bob", []string{"vm01ev01"})
	nextEvent(t, rh, StatusWaiting, "")

	_, err := c.AdminList("mine_pw_1")
	wantExact(t, err, MsgNoAdmin)
	wantExact(t, c.AdminSet("mine_pw_1", rh.ID, StatusApproved, ""), MsgNoAdmin)
	wantExact(t, c.AdminDelete("mine_pw_1", rh.ID), MsgNoAdmin)
	_, err = c.AdminClear("mine_pw_1")
	wantExact(t, err, MsgNoAdmin)
	wantExact(t, c.AdminSetPassword("mine_pw_1", "bob", "taken_pw"), MsgNoAdmin)
	if !strings.Contains(MsgNoAdmin, "install.sh") || !strings.Contains(MsgNoAdmin, "admin=") {
		t.Fatalf("안내 문구에 install.sh / admin= 가 있어야 함: %s", MsgNoAdmin)
	}
	if j := h.job(rh.ID); j.Status != StatusWaiting {
		t.Fatalf("관리자 없이 상태가 바뀜: %+v", j)
	}
	wantDaemonErr(t, c.Start(rh.ID, rh.Token), "승인 상태가 아니")
	// bob 은 빼앗기지 않음
	if err := c.Approve(rh.ID, "bob", testPasswords["bob"], h.paste(rh.ID)); err != nil {
		t.Fatal(err)
	}

	// install.sh 가 admin 줄을 쓰면(재시작 없이) 관리자 기능이 열린다
	writeAuth(t, h.authPath, testPasswords)
	waitFor(t, 2*time.Second, "admin 줄 반영", func() bool { _, err := c.AdminList(testPasswords["admin"]); return err == nil })
}

// ---- 3. 잠금 대신 지연 ----

// PoC: 남이 bob·admin 이름으로 일부러 틀려 정당한 사용자를 잠그던 DoS → 이제 올바른 비밀번호는 항상 통과.
func TestRegression_NoLockoutDoS(t *testing.T) {
	rec := &sleepRecorder{}
	h := newHarness(t, func(h *harness, cfg *Config) { cfg.Sleep = rec.Sleep })
	c := h.client()
	rh := h.run(c, "alice", "bob", []string{"vm01ev01"})
	for i := 0; i < 20; i++ {
		wantExact(t, c.Approve(rh.ID, "bob", "wrong_pw", h.paste(rh.ID)), MsgAuthFailed)
	}
	if err := c.Approve(rh.ID, "bob", testPasswords["bob"], h.paste(rh.ID)); err != nil {
		t.Fatalf("연속 실패 뒤 정당한 확인자가 막힘: %v", err)
	}
	for i := 0; i < 20; i++ {
		_, err := c.AdminList("wrong_pw")
		wantExact(t, err, MsgAuthFailed)
	}
	if _, err := c.AdminList(testPasswords["admin"]); err != nil {
		t.Fatalf("연속 실패 뒤 정당한 관리자가 막힘: %v", err)
	}
	if rec.n() < 30 {
		t.Fatalf("5번 넘게 틀린 시도들은 지연을 거쳐야 함: sleep %d번", rec.n())
	}
}

// 같은 이름의 지연 시도는 한 번에 하나씩(직렬)이고, 지연 중에도 서버 mu 를 잡지 않아 다른 요청은 바로 처리된다.
func TestThrottleSerializedPerUserWithoutServerLock(t *testing.T) {
	var mu sync.Mutex
	active, maxActive := 0, 0
	entered := make(chan struct{}, 16)
	release := make(chan struct{})
	sleep := func(time.Duration) {
		mu.Lock()
		active++
		if active > maxActive {
			maxActive = active
		}
		mu.Unlock()
		entered <- struct{}{}
		<-release
		mu.Lock()
		active--
		mu.Unlock()
	}
	h := newHarness(t, func(h *harness, cfg *Config) { cfg.Sleep = sleep })
	c := h.client()
	rh := h.run(c, "alice", "bob", []string{"vm01ev01"})
	for i := 0; i < DefaultAuthMaxFailures; i++ {
		wantExact(t, c.Approve(rh.ID, "bob", "wrong_pw", h.paste(rh.ID)), MsgAuthFailed)
	}
	refs := func() int {
		h.srv.thr.mu.Lock()
		defer h.srv.thr.mu.Unlock()
		if u, ok := h.srv.thr.users["bob"]; ok {
			return u.refs
		}
		return 0
	}
	results := make(chan error, 3)
	attempt := func(pw string) {
		cc, err := Dial(h.sock)
		if err != nil {
			results <- err
			return
		}
		defer cc.Close()
		results <- cc.Approve(rh.ID, "bob", pw, h.paste(rh.ID))
	}
	go attempt("wrong_a1")
	go attempt("wrong_b2")
	waitFor(t, 3*time.Second, "틀린 시도 둘이 지연 관문에 들어감", func() bool { return refs() == 2 })
	go attempt(testPasswords["bob"])
	waitFor(t, 3*time.Second, "올바른 시도도 관문에 들어감", func() bool { return refs() == 3 })
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("지연이 시작되지 않음")
	}
	// 지연 중에도 서버 mu 를 쓰는 다른 요청(run, 다른 확인자의 승인)은 막히지 않는다
	rh2 := h.run(c, "alice", "carol", []string{"vm01ev02"})
	if err := c.Approve(rh2.ID, "carol", testPasswords["carol"], h.paste(rh2.ID)); err != nil {
		t.Fatalf("지연 중 다른 사용자 승인: %v", err)
	}
	select {
	case <-entered:
		t.Fatal("같은 이름의 시도가 동시에 둘 지연됨")
	case <-time.After(100 * time.Millisecond):
	}
	for i := 0; i < 3; i++ {
		release <- struct{}{}
		if i < 2 {
			select {
			case <-entered:
			case <-time.After(3 * time.Second):
				t.Fatal("다음 시도가 이어서 지연되지 않음")
			}
		}
	}
	okN, failN := 0, 0
	for i := 0; i < 3; i++ {
		select {
		case err := <-results:
			var de *DaemonError
			switch {
			case err == nil:
				okN++
			case errors.As(err, &de) && de.Msg == MsgAuthFailed:
				failN++
			default:
				t.Errorf("예상 밖 결과: %v", err)
			}
		case <-time.After(3 * time.Second):
			t.Fatal("시도 결과가 오지 않음")
		}
	}
	if okN != 1 || failN != 2 || maxActive != 1 {
		t.Fatalf("성공 %d, 실패 %d, 동시 지연 최대 %d (기대 1, 2, 1)", okN, failN, maxActive)
	}
	if h.job(rh.ID).Status != StatusApproved {
		t.Fatal("지연 중 올바른 비밀번호로 승인되어야 함")
	}
}

// 전체 실패가 한도를 넘으면(이름과 상관없이) 실패 응답마다 지연. 성공은 지연 없음. 구간이 지나면 풀린다.
func TestGlobalFailureCapDelaysFailures(t *testing.T) {
	rec := &sleepRecorder{}
	clk := newFakeClock()
	h := newHarness(t, func(h *harness, cfg *Config) {
		cfg.Sleep = rec.Sleep
		cfg.Now = clk.Now
		cfg.GlobalFailLimit = 3
		cfg.AuthMaxFailures = 100 // 이름별 지연은 빼고 전체 한도만 본다
	})
	c := h.client()
	rh := h.run(c, "alice", "bob", []string{"vm01ev01"})
	for _, u := range []string{"bob", "carol", "zed1"} {
		c.Approve(rh.ID, u, "wrong_pw", h.paste(rh.ID))
	}
	if rec.n() != 0 {
		t.Fatalf("한도(3) 이하에서는 지연 없음: %d", rec.n())
	}
	wantExact(t, c.Approve(rh.ID, "zed2", "wrong_pw", h.paste(rh.ID)), MsgAuthFailed)
	if rec.n() != 1 {
		t.Fatalf("한도를 넘은 실패는 지연: %d", rec.n())
	}
	if err := c.Approve(rh.ID, "bob", testPasswords["bob"], h.paste(rh.ID)); err != nil {
		t.Fatal(err)
	}
	if rec.n() != 1 {
		t.Fatalf("성공은 지연하지 않음: %d", rec.n())
	}
	clk.Add(DefaultGlobalFailWindow + time.Second)
	c.Approve(rh.ID, "bob", "wrong_pw", h.paste(rh.ID))
	if rec.n() != 1 {
		t.Fatalf("구간이 지나면 다시 한도 아래: %d", rec.n())
	}
}

// ---- 4. 비밀번호 최소 길이 ----

func TestPasswordLengthLimits(t *testing.T) {
	if err := CheckPassword("12345"); err == nil || !strings.Contains(err.Error(), "6~128자") {
		t.Fatalf("5자는 거부: %v", err)
	}
	if err := CheckPassword("123456"); err != nil {
		t.Fatalf("6자는 허용: %v", err)
	}
	if err := CheckPassword(strings.Repeat("x", 129)); err == nil {
		t.Fatal("129자는 거부")
	}
	h := newHarness(t, nil)
	c := h.client()
	wantDaemonErr(t, c.Register("dave", "12345"), "6~128자")
	wantDaemonErr(t, c.ChangePassword("bob", testPasswords["bob"], "12345"), "6~128자")
	wantDaemonErr(t, c.AdminSetPassword(testPasswords["admin"], "bob", "12345"), "6~128자")
	if !h.srv.auth.Verify("bob", testPasswords["bob"]) {
		t.Fatal("거부된 변경이 비밀번호를 바꿈")
	}
}

// admin_set_password 는 목록 안 사용자만, 항목이 없으면 새로 만든다. admin 자신과 목록 밖 이름은 거부.
func TestAdminSetPasswordListedUsersOnly(t *testing.T) {
	h := newHarness(t, nil)
	c := h.client()
	apw := testPasswords["admin"]
	wantDaemonErr(t, c.AdminSetPassword(apw, "zed", "zed_pw_1"), "users= 목록에 없음")
	wantDaemonErr(t, c.AdminSetPassword(apw, "admin", "new_admin_pw"), "바꿀 수 없음")
	if ok, _ := c.UserRegistered("zed"); ok {
		t.Fatal("목록 밖 항목이 만들어짐")
	}
	if ok, _ := c.UserRegistered("gina"); ok {
		t.Fatal("gina 는 아직 없어야 함")
	}
	if err := c.AdminSetPassword(apw, "gina", "gina_pw_1"); err != nil {
		t.Fatalf("목록 안 사용자 항목 새로 만들기: %v", err)
	}
	rh := h.run(c, "alice", "gina", []string{"vm01ev01"})
	if err := c.Approve(rh.ID, "gina", "gina_pw_1", h.paste(rh.ID)); err != nil {
		t.Fatal(err)
	}
}

// ---- 5. 자원 한도 ----

// readOneLine 은 연결에서 한 줄을 읽는다(제한 시간 3초).
func readOneLine(t *testing.T, conn net.Conn, r *bufio.Reader) (string, error) {
	t.Helper()
	conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	return r.ReadString('\n')
}

func TestConnLimit(t *testing.T) {
	h := newHarness(t, func(h *harness, cfg *Config) { cfg.MaxConns = 3 })
	var conns []net.Conn
	for i := 0; i < 3; i++ {
		conn, err := net.Dial("unix", h.sock)
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Close()
		conn.Write([]byte(`{"op":"ping"}` + "\n"))
		if line, err := readOneLine(t, conn, bufio.NewReader(conn)); err != nil || !strings.Contains(line, `"ok":true`) {
			t.Fatalf("한도 안 연결 %d: %q %v", i, line, err)
		}
		conns = append(conns, conn)
	}
	extra, err := net.Dial("unix", h.sock)
	if err != nil {
		t.Fatal(err)
	}
	defer extra.Close()
	r := bufio.NewReader(extra)
	line, err := readOneLine(t, extra, r)
	if err != nil || !strings.Contains(line, msgTooManyConns) || !strings.Contains(line, `"ok":false`) {
		t.Fatalf("한도 초과 연결은 오류 한 줄: %q %v", line, err)
	}
	if _, err := readOneLine(t, extra, r); err == nil {
		t.Fatal("오류 뒤에는 연결이 닫혀야 함")
	}
	// 하나를 닫으면 다시 받는다
	conns[0].Close()
	waitFor(t, 3*time.Second, "자리가 나면 새 연결 허용", func() bool {
		cc, err := Dial(h.sock)
		if err != nil {
			return false
		}
		defer cc.Close()
		return cc.Ping() == nil
	})
}

func TestReadDeadlineAndClientReconnect(t *testing.T) {
	old := idleReconnect
	idleReconnect = 50 * time.Millisecond
	t.Cleanup(func() { idleReconnect = old })
	h := newHarness(t, func(h *harness, cfg *Config) { cfg.ReadTimeout = 150 * time.Millisecond })

	// 아무것도 안 보내는 연결, 줄을 끝내지 않는 연결은 닫힌다
	for name, send := range map[string]string{"빈 연결": "", "끝나지 않은 줄": `{"op":"pi`} {
		conn, err := net.Dial("unix", h.sock)
		if err != nil {
			t.Fatal(err)
		}
		if send != "" {
			conn.Write([]byte(send))
		}
		start := time.Now()
		_, err = readOneLine(t, conn, bufio.NewReader(conn))
		conn.Close()
		if err == nil || time.Since(start) > 2*time.Second {
			t.Fatalf("%s: 읽기 제한으로 닫혀야 함(%v, %s)", name, err, time.Since(start))
		}
	}

	// 요청마다 제한을 새로 건다: 100ms 간격 ping 5번(총 500ms > 150ms)은 한 연결로 모두 성공
	conn, err := net.Dial("unix", h.sock)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	r := bufio.NewReader(conn)
	for i := 0; i < 5; i++ {
		time.Sleep(100 * time.Millisecond)
		conn.Write([]byte(`{"op":"ping"}` + "\n"))
		if line, err := readOneLine(t, conn, r); err != nil || !strings.Contains(line, `"ok":true`) {
			t.Fatalf("%d번째 ping: %q %v", i, line, err)
		}
	}

	// run 연결에는 제한이 없고, 오래 쉰 클라이언트는 다시 연결해 승인한다
	c := h.client()
	rh := h.run(c, "alice", "bob", []string{"vm01ev01"})
	nextEvent(t, rh, StatusWaiting, "")
	time.Sleep(400 * time.Millisecond)
	if err := c.Approve(rh.ID, "bob", testPasswords["bob"], h.paste(rh.ID)); err != nil {
		t.Fatalf("쉬었다가 승인(재연결): %v", err)
	}
	nextEvent(t, rh, StatusApproved, "bob 가 승인")
}

func TestWaitingJobsCapPerWorker(t *testing.T) {
	h := newHarness(t, func(h *harness, cfg *Config) { cfg.MaxWaitingPerUser = 2 })
	c := h.client()
	a := h.run(c, "alice", "bob", []string{"vm01ev01"})
	h.run(c, "alice", "bob", []string{"vm01ev02"})
	_, err := c.Run("alice", "bob", []string{"vm02ev01"})
	wantDaemonErr(t, err, "대기 작업이 한도(2건)")
	// 다른 작업자는 영향 없음
	writeTargets(t, h.targetDir, "carol", "vm07ev01\n")
	h.run(c, "carol", "bob", []string{"vm07ev01"})
	// 하나가 끝나면 다시 가능
	if err := c.Cancel(a.ID, "bob", testPasswords["bob"]); err != nil {
		t.Fatal(err)
	}
	h.run(c, "alice", "bob", []string{"vm02ev01"})
}

// PoC: 동일인 run 을 반복해 저장 파일을 끝없이 키우던 것 → 전체 한도에서 거부.
func TestStoredJobsCap(t *testing.T) {
	h := newHarness(t, func(h *harness, cfg *Config) { cfg.MaxJobs = 3 })
	c := h.client()
	for i := 0; i < 3; i++ {
		_, err := c.Run("alice", "alice", []string{"vm01ev01"})
		wantDaemonErr(t, err, ReasonSameUser)
	}
	_, err := c.Run("alice", "alice", []string{"vm01ev01"})
	wantDaemonErr(t, err, "한도(3건)")
	_, err = c.Run("alice", "bob", []string{"vm01ev01"})
	wantDaemonErr(t, err, "한도(3건)")
	if n := h.srv.st.Len(); n != 3 {
		t.Fatalf("저장 작업 = %d", n)
	}
	if _, err := c.AdminClear(testPasswords["admin"]); err != nil {
		t.Fatal(err)
	}
	h.run(c, "alice", "bob", []string{"vm01ev01"})
}

func TestPruneOldAbortedJobs(t *testing.T) {
	clk := &fakeClock{t: time.Now()}
	h := newHarness(t, func(h *harness, cfg *Config) { cfg.Now = clk.Now })
	c := h.client()
	bpw := testPasswords["bob"]

	old := h.run(c, "alice", "bob", []string{"vm01ev01"})
	if err := c.Cancel(old.ID, "bob", bpw); err != nil {
		t.Fatal(err)
	}
	started := h.run(c, "alice", "bob", []string{"vm01ev02"})
	c.Approve(started.ID, "bob", bpw, h.paste(started.ID))
	if err := c.Start(started.ID, started.Token); err != nil {
		t.Fatal(err)
	}
	waiting := h.run(c, "alice", "bob", []string{"vm02ev01"})
	h.srv.mu.Lock()
	_, hasTok := h.srv.tokens[old.ID]
	h.srv.mu.Unlock()
	if !hasTok {
		t.Fatal("중단된 작업의 token 은 남아 있어야 함(만료 사유 안내용)")
	}

	clk.Add(29 * 24 * time.Hour)
	if n := h.srv.prune(); n != 0 {
		t.Fatalf("30일 전에는 지우지 않음: %d", n)
	}
	// 29일째에 중단된 작업(시각은 가짜 시계 기준으로 직접 넣음)
	recent, err := h.srv.st.Add(store.Job{User: "alice", Checker: "bob", Targets: []string{"vm03ev01"},
		Status: StatusAborted, Reason: ReasonWorkerGone, CreatedAt: clk.Now(), UpdatedAt: clk.Now()})
	if err != nil {
		t.Fatal(err)
	}
	clk.Add(2 * 24 * time.Hour) // 처음 중단 후 31일
	if n := h.srv.prune(); n != 1 {
		t.Fatalf("31일 지난 중단 작업 1건만 지워야 함: %d", n)
	}
	if _, ok := h.srv.st.Get(old.ID); ok {
		t.Fatal("오래된 중단 작업이 남음")
	}
	h.srv.mu.Lock()
	_, hasTok = h.srv.tokens[old.ID]
	h.srv.mu.Unlock()
	if hasTok {
		t.Fatal("지운 작업의 token 도 지워야 함")
	}
	for _, id := range []string{started.ID, waiting.ID, recent.ID} {
		if _, ok := h.srv.st.Get(id); !ok {
			t.Fatalf("지우면 안 되는 작업 %s 가 지워짐", id)
		}
	}
}

func TestPruneLoopRuns(t *testing.T) {
	h := newHarness(t, func(h *harness, cfg *Config) {
		cfg.PruneInterval = 20 * time.Millisecond
		cfg.PruneAge = time.Millisecond
	})
	_, err := h.client().Run("alice", "alice", []string{"vm01ev01"})
	var de *DaemonError
	if !errors.As(err, &de) || de.ID == "" {
		t.Fatalf("동일인 기록: %v", err)
	}
	waitFor(t, 3*time.Second, "정리 주기가 오래된 중단 작업을 지움", func() bool { _, ok := h.srv.st.Get(de.ID); return !ok })
}

// ---- 6. token 폐기·되살리기 금지 ----

// PoC: start 성공 뒤에도 token 이 남아, 관리자 재승인 후 같은 token 으로 다시 시작 → 이제 불가.
func TestRegression_TokenRetiredAfterStart(t *testing.T) {
	h := newHarness(t, nil)
	c := h.client()
	apw := testPasswords["admin"]
	rh := h.run(c, "alice", "bob", []string{"vm01ev01"})
	if err := c.Approve(rh.ID, "bob", testPasswords["bob"], h.paste(rh.ID)); err != nil {
		t.Fatal(err)
	}
	if err := c.Start(rh.ID, rh.Token); err != nil {
		t.Fatal(err)
	}
	hasToken := func(id string) bool {
		h.srv.mu.Lock()
		defer h.srv.mu.Unlock()
		_, ok := h.srv.tokens[id]
		return ok
	}
	// token 은 finish 까지 남는다(작업중). 두 번째 start 는 거부
	if !hasToken(rh.ID) {
		t.Fatal("시작 뒤(작업중) token 은 finish 까지 남아야 함")
	}
	wantDaemonErr(t, c.Start(rh.ID, rh.Token), "이미 시작")
	wantDaemonErr(t, c.AdminSet(apw, rh.ID, StatusApproved, ""), "이미 승인된")
	if err := c.Finish(rh.ID, rh.Token, ResultDone); err != nil {
		t.Fatal(err)
	}
	if hasToken(rh.ID) {
		t.Fatal("finish 뒤 token 이 남음")
	}
	wantDaemonErr(t, c.Start(rh.ID, rh.Token), "이미 끝난 작업")
	wantDaemonErr(t, c.Finish(rh.ID, rh.Token, ResultAborted), "이미 끝난 작업")
	if j := h.job(rh.ID); j.StartedAt == nil || j.Phase != PhaseDone {
		t.Fatalf("시작·완료 기록: %+v", j)
	}

	// 중단된 작업의 token 은 남아 작업자가 실제 사유를 받는다
	rh2 := h.run(c, "alice", "bob", []string{"vm01ev02"})
	c.Approve(rh2.ID, "bob", testPasswords["bob"], h.paste(rh2.ID))
	if err := c.AdminSet(apw, rh2.ID, StatusAborted, "점검"); err != nil {
		t.Fatal(err)
	}
	wantDaemonErr(t, c.Start(rh2.ID, rh2.Token), ReasonAdminSet+": 점검")
}

func TestAdminSetTransitions(t *testing.T) {
	h := newHarness(t, nil)
	c := h.client()
	apw := testPasswords["admin"]

	a := h.run(c, "alice", "bob", []string{"vm01ev01"})
	if err := c.AdminSet(apw, a.ID, StatusAborted, ""); err != nil { // 대기→중단
		t.Fatal(err)
	}
	wantExact(t, c.AdminSet(apw, a.ID, StatusApproved, ""), MsgNoResurrect)
	wantDaemonErr(t, c.AdminSet(apw, a.ID, StatusAborted, ""), "이미 중단")
	wantDaemonErr(t, c.Start(a.ID, a.Token), "승인 상태가 아니")
	if j := h.job(a.ID); j.Status != StatusAborted || j.ApprovedAt != nil {
		t.Fatalf("중단된 작업이 바뀜: %+v", j)
	}

	b := h.run(c, "alice", "bob", []string{"vm01ev02"})
	if err := c.AdminSet(apw, b.ID, StatusApproved, ""); err != nil { // 대기→승인
		t.Fatal(err)
	}
	wantDaemonErr(t, c.AdminSet(apw, b.ID, StatusApproved, ""), "이미 승인된")
	if err := c.AdminSet(apw, b.ID, StatusAborted, ""); err != nil { // 승인→중단
		t.Fatal(err)
	}

	cc := h.run(c, "alice", "bob", []string{"vm02ev01"})
	c.Cancel(cc.ID, "bob", testPasswords["bob"]) // 확인자 취소 → 중단
	wantExact(t, c.AdminSet(apw, cc.ID, StatusApproved, ""), MsgNoResurrect)
}
