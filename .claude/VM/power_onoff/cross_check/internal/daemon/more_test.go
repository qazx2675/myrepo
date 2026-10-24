package daemon

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"cross_check/internal/store"
)

// ---- 등록·비밀번호 변경 ----

func TestRegistration(t *testing.T) {
	h := newHarness(t, nil)
	c := h.client()
	if ok, err := c.UserRegistered("dave"); err != nil || ok {
		t.Fatalf("UserRegistered(dave) = %v, %v", ok, err)
	}
	if err := c.Register("dave", "pw dave"); err != nil { // 가운데 공백은 허용
		t.Fatal(err)
	}
	if ok, _ := c.UserRegistered("dave"); !ok {
		t.Fatal("등록 후 registered 여야 함")
	}
	wantDaemonErr(t, c.Register("dave", "other_pw"), "이미 등록됨")
	wantDaemonErr(t, c.Register("admin", "takeover"), "등록할 수 없음") // admin 은 있든 없든 register 불가
	wantDaemonErr(t, c.Register("zed", "zed_pw_1"), "users= 목록에 없어")
	for _, bad := range []string{"", "short", " lead_pw", "trail_pw ", "a\nbcdefg", "a\x00bcdefg", strings.Repeat("가", 129)} {
		wantDaemonErr(t, c.Register("frank", bad), "비밀번호")
	}
	wantDaemonErr(t, c.Register("Bad-Name", "pw_bad_1"), "사용자 이름")
	if err := c.Register("frank", strings.Repeat("가", 128)); err != nil {
		t.Fatalf("128자는 허용: %v", err)
	}
	if runtime.GOOS != "windows" {
		fi, _ := os.Stat(h.authPath)
		if fi.Mode().Perm() != 0o600 {
			t.Fatalf("등록 후 파일 권한 = %o", fi.Mode().Perm())
		}
	}
	b, _ := os.ReadFile(h.authPath)
	if !strings.Contains(string(b), "# 테스트 비밀번호") || !strings.Contains(string(b), "dave=pw dave\n") {
		t.Fatalf("기존 줄을 유지하고 끝에 붙여야 함:\n%s", b)
	}
	// 새로 등록한 사용자가 바로 확인자로 쓰인다
	rh := h.run(c, "alice", "dave", []string{"vm01ev01"})
	if err := c.Approve(rh.ID, "dave", "pw dave"); err != nil {
		t.Fatal(err)
	}
}

// 첫 실행(파일·디렉터리 없음): 일반 사용자 등록은 파일을 0600·디렉터리 0700 으로 만들지만, admin 은 만들 수 없고
// admin 항목이 없는 동안 관리자 op 는 모두 안내 문구로 거부된다.
func TestFirstRunCreatesFileButNeverAdmin(t *testing.T) {
	h := newHarness(t, func(h *harness, cfg *Config) {
		os.Remove(h.authPath) // 첫 실행: 파일 자체가 없음
		os.Remove(filepath.Dir(h.authPath))
	})
	c := h.client()
	_, err := c.AdminList("x")
	wantDaemonErr(t, err, MsgNoAdmin)
	wantDaemonErr(t, c.Register("admin", "first_admin"), "등록할 수 없음")
	if err := c.Register("alice", "first_alice"); err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" {
		fi, err := os.Stat(filepath.Dir(h.authPath))
		if err != nil || fi.Mode().Perm() != 0o700 {
			t.Fatalf("디렉터리는 0700 으로 만들어야 함: %v %v", fi, err)
		}
		fi, err = os.Stat(h.authPath)
		if err != nil || fi.Mode().Perm() != 0o600 {
			t.Fatalf("파일은 0600 으로 만들어야 함: %v %v", fi, err)
		}
	}
	_, err = c.AdminList("first_admin")
	wantDaemonErr(t, err, MsgNoAdmin)
	if ok, _ := c.UserRegistered("admin"); ok {
		t.Fatal("admin 이 만들어지면 안 됨")
	}
}

func TestChangePassword(t *testing.T) {
	h := newHarness(t, nil)
	c := h.client()
	wantDaemonErr(t, c.ChangePassword("bob", "wrong", "new_bob"), MsgAuthFailed)
	wantDaemonErr(t, c.ChangePassword("bob", testPasswords["bob"], " bad_pw_x"), "공백")
	wantDaemonErr(t, c.ChangePassword("bob", testPasswords["bob"], "12345"), "6~128자")
	if err := c.ChangePassword("bob", testPasswords["bob"], "new_bob"); err != nil {
		t.Fatal(err)
	}
	rh := h.run(c, "alice", "bob", []string{"vm01ev01"})
	wantDaemonErr(t, c.Approve(rh.ID, "bob", testPasswords["bob"]), MsgAuthFailed)
	if err := c.Approve(rh.ID, "bob", "new_bob"); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(h.authPath)
	if strings.Count(string(b), "bob=") != 1 {
		t.Fatalf("bob 줄은 하나여야 함:\n%s", b)
	}
}

func TestConcurrentRegisterOneWins(t *testing.T) {
	h := newHarness(t, nil)
	const n = 20
	var wg sync.WaitGroup
	var mu sync.Mutex
	wins, dup := 0, 0
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			c, err := Dial(h.sock)
			if err != nil {
				t.Error(err)
				return
			}
			defer c.Close()
			err = c.Register("eve", fmt.Sprintf("pw_eve_%d", i))
			mu.Lock()
			defer mu.Unlock()
			var de *DaemonError
			switch {
			case err == nil:
				wins++
			case errors.As(err, &de) && de.Msg == "이미 등록됨":
				dup++
			default:
				t.Errorf("예상 밖 오류: %v", err)
			}
		}(i)
	}
	wg.Wait()
	if wins != 1 || dup != n-1 {
		t.Fatalf("성공 %d, 중복 %d (기대 1, %d)", wins, dup, n-1)
	}
	b, _ := os.ReadFile(h.authPath)
	if strings.Count(string(b), "eve=") != 1 {
		t.Fatalf("eve 줄은 하나여야 함:\n%s", b)
	}
}

// ---- run 전제 조건 ----

func TestRunRequiresRegistration(t *testing.T) {
	h := newHarness(t, nil)
	c := h.client()
	writeTargets(t, h.targetDir, "dave", "vm05ev01\n")
	_, err := c.Run("dave", "bob", []string{"vm05ev01"})
	wantDaemonErr(t, err, "작업자 미등록: cross_check --user dave")
	_, err = c.Run("alice", "dave", []string{"vm01ev01"})
	wantDaemonErr(t, err, "확인자 미등록")
	if n := len(h.srv.st.List()); n != 0 {
		t.Fatalf("거부된 run 이 작업을 만듦: %d", n)
	}
	if err := c.Register("dave", "pw_dave"); err != nil {
		t.Fatal(err)
	}
	rh := h.run(c, "dave", "bob", []string{"vm05ev01"})
	nextEvent(t, rh, StatusWaiting, "")
}

func TestRunTargetsMustBeOwn(t *testing.T) {
	h := newHarness(t, nil)
	c := h.client()

	// 부분집합(주석·빈 줄·앞뒤 공백 무시)
	rh := h.run(c, "alice", "bob", []string{"vm01ev02", "vm03ev01"})
	nextEvent(t, rh, StatusWaiting, "")

	// 등록되지 않은 이름 → 거부(이름을 알려줌), 작업은 만들지 않음
	before := len(h.srv.st.List())
	_, err := c.Run("alice", "bob", []string{"vm01ev01", "zz01ev01", "vm09ev01"})
	wantDaemonErr(t, err, "zz01ev01")
	wantDaemonErr(t, err, "vm09ev01") // bob 의 대상이라도 alice 는 못 씀
	wantDaemonErr(t, err, "2개")
	if len(h.srv.st.List()) != before {
		t.Fatal("거부된 run 이 작업을 만듦")
	}

	// 대상 파일 없음
	_, err = c.Run("carol", "bob", []string{"vm01ev01"})
	wantDaemonErr(t, err, "대상 파일이 없음")

	// 비어 있음(주석만)
	writeTargets(t, h.targetDir, "carol", "# 아직 없음\n\n")
	_, err = c.Run("carol", "bob", []string{"vm01ev01"})
	wantDaemonErr(t, err, "비어 있음")

	// 이름 규칙 위반 줄
	writeTargets(t, h.targetDir, "carol", "vm01ev01\nbad name!\n")
	_, err = c.Run("carol", "bob", []string{"vm01ev01"})
	wantDaemonErr(t, err, "이름 규칙")

	// 매번 다시 읽는다(캐시 없음)
	writeTargets(t, h.targetDir, "carol", "vm07ev01\n")
	rh2 := h.run(c, "carol", "bob", []string{"vm07ev01"})
	nextEvent(t, rh2, StatusWaiting, "")

	// 심볼릭 링크 거부
	if runtime.GOOS != "windows" {
		real := filepath.Join(h.dir, "real.txt")
		os.WriteFile(real, []byte("vm08ev01\n"), 0o644)
		p := TargetFilePath(h.targetDir, "carol")
		os.Remove(p)
		if err := os.Symlink(real, p); err != nil {
			t.Fatal(err)
		}
		_, err = c.Run("carol", "bob", []string{"vm08ev01"})
		wantDaemonErr(t, err, "심볼릭 링크")
	}
}

func TestValidateRun(t *testing.T) {
	ok := Request{User: "alice", Checker: "bob", Targets: []string{"a.b-c_1"}}
	if err := validateRun(ok); err != nil {
		t.Fatal(err)
	}
	many := make([]string, MaxTargets+1)
	for i := range many {
		many[i] = fmt.Sprintf("t%d", i)
	}
	bad := map[string]Request{
		"작업자 대문자":  {User: "Alice", Checker: "bob", Targets: []string{"a"}},
		"확인자 빈값":   {User: "alice", Checker: "", Targets: []string{"a"}},
		"admin 예약": {User: "alice", Checker: "admin", Targets: []string{"a"}},
		"대상 없음":    {User: "alice", Checker: "bob"},
		"대상 문자":    {User: "alice", Checker: "bob", Targets: []string{"a b"}},
		"대상 중복":    {User: "alice", Checker: "bob", Targets: []string{"a", "a"}},
		"대상 초과":    {User: "alice", Checker: "bob", Targets: many},
	}
	for name, r := range bad {
		if err := validateRun(r); err == nil {
			t.Errorf("%s: 거부해야 함", name)
		}
	}
}

// ---- 조회 ----

func TestListAndGetScope(t *testing.T) {
	h := newHarness(t, nil)
	c := h.client()
	a := h.run(c, "alice", "bob", []string{"vm01ev01"})
	b := h.run(c, "alice", "carol", []string{"vm01ev02"})

	bob, _ := c.List("bob")
	if len(bob) != 1 || bob[0].ID != a.ID {
		t.Fatalf("bob 은 자기가 확인자인 작업만: %+v", bob)
	}
	alice, _ := c.List("alice")
	if len(alice) != 2 || len(alice[1].Targets) != 1 {
		t.Fatalf("alice 는 자기 작업 둘(대상 포함): %+v", alice)
	}
	if none, err := c.List("zed"); err != nil || len(none) != 0 {
		t.Fatalf("관계없는 사용자 = %+v, %v", none, err)
	}
	_, err := c.List("Bad")
	wantDaemonErr(t, err, "사용자 이름")

	_, err = c.Get(b.ID, "bob", "")
	wantDaemonErr(t, err, msgNotMember)
	_, err = c.Get("nosuch", "bob", "")
	wantDaemonErr(t, err, msgNotMember)
	_, err = c.Get(b.ID, "carol", "wrong")
	wantDaemonErr(t, err, MsgAuthFailed)
	if v, err := c.Get(b.ID, "carol", testPasswords["carol"]); err != nil || v.Targets[0] != "vm01ev02" {
		t.Fatalf("Get(carol) = %+v, %v", v, err)
	}
}

// ---- 비밀번호·token 이 어디에도 새지 않음 ----

func TestSecretsNeverLeak(t *testing.T) {
	h := newHarness(t, nil)
	c := h.client()
	rh := h.run(c, "alice", "bob", []string{"vm01ev01"})
	var outputs []string
	collect := func(v any, err error) {
		b, _ := json.Marshal(v)
		outputs = append(outputs, string(b))
		if err != nil {
			outputs = append(outputs, err.Error())
		}
	}
	collect(nil, c.Approve(rh.ID, "bob", "wrong_guess_pw"))
	collect(nil, c.Approve(rh.ID, "bob", testPasswords["carol"]))
	collect(nil, c.Cancel(rh.ID, "carol", testPasswords["carol"]))
	collect(nil, c.ChangePassword("carol", "wrong_guess_pw", "new_secret_pw"))
	collect(nil, c.Register("bob", "another_secret_pw"))
	collect(nil, c.Register("gina", " bad_secret_pw"))
	collect(c.AdminList("wrong_guess_pw"))
	collect(nil, c.Start(rh.ID, "bad_token_value"))
	collect(c.Get(rh.ID, "bob", "wrong_guess_pw"))
	collect(nil, c.Approve(rh.ID, "bob", testPasswords["bob"]))
	collect(c.List("bob"))
	collect(c.List("alice"))
	collect(c.Get(rh.ID, "bob", testPasswords["bob"]))
	collect(nil, c.Start(rh.ID, rh.Token))
	collect(c.AdminList(testPasswords["admin"]))
	for ev := range rh.Events() {
		collect(ev, nil)
	}
	all := strings.Join(outputs, "\n") + "\n" + h.logs.String()
	b, _ := os.ReadFile(h.storePath)
	all += "\n" + string(b)
	secrets := []string{"wrong_guess_pw", "new_secret_pw", "another_secret_pw", "bad_secret_pw", "bad_token_value", rh.Token}
	for _, p := range testPasswords {
		secrets = append(secrets, p)
	}
	for _, s := range secrets {
		if strings.Contains(all, s) {
			t.Errorf("비밀값 %q 가 응답·오류·로그·저장 파일에 나타남", s)
		}
	}
}

// ---- 재시작·종료 ----

func TestRestartAbortsLeftover(t *testing.T) {
	var leftover []string
	h := newHarness(t, func(h *harness, cfg *Config) {
		st, err := store.Open(h.storePath)
		if err != nil {
			t.Fatal(err)
		}
		now := time.Now()
		for _, s := range []store.Status{StatusWaiting, StatusApproved, StatusAborted} {
			j, err := st.Add(store.Job{User: "alice", Checker: "bob", Targets: []string{"vm01ev01"}, Status: s, ApprovedAt: &now})
			if err != nil {
				t.Fatal(err)
			}
			leftover = append(leftover, j.ID)
		}
	})
	for i, id := range leftover[:2] {
		if j := h.job(id); j.Status != StatusAborted || j.Reason != ReasonRestart {
			t.Fatalf("남은 작업 %d = %+v", i, j)
		}
	}
	if j := h.job(leftover[2]); j.Reason == ReasonRestart {
		t.Fatal("이미 중단된 작업의 사유를 덮어씀")
	}
}

func TestShutdownNotifiesWorker(t *testing.T) {
	h := newHarness(t, nil)
	c := h.client()
	rh := h.run(c, "alice", "bob", []string{"vm01ev01"})
	nextEvent(t, rh, StatusWaiting, "")
	h.srv.Close()
	nextEvent(t, rh, StatusAborted, ReasonShutdown)
	if j := h.job(rh.ID); j.Reason != ReasonShutdown {
		t.Fatalf("종료 후 작업 = %+v", j)
	}
	// 같은 경로로 새 데몬이 다시 뜬다(소켓 파일은 닫을 때 지워짐)
	srv2, err := NewServer(Config{SocketPath: h.sock, StorePath: h.storePath, AuthPath: h.authPath,
		AuthSkipPermCheck: runtime.GOOS == "windows", TargetDir: h.targetDir, AllowedUsers: testUsers, Logf: h.logs.Logf})
	if err != nil {
		t.Fatal(err)
	}
	srv2.Close()
}

func TestStaleSocketRemovedLiveSocketKept(t *testing.T) {
	h := newHarness(t, nil)
	// 살아 있는 데몬의 소켓은 건드리지 않는다
	_, err := NewServer(Config{SocketPath: h.sock, StorePath: filepath.Join(h.dir, "x.json"), AuthMode: AuthOff, Logf: h.logs.Logf})
	if err == nil || !strings.Contains(err.Error(), "이미 소켓을 사용") {
		t.Fatalf("살아 있는 소켓: %v", err)
	}
	// 죽은 데몬이 남긴 소켓 파일은 지우고 연다
	stale := filepath.Join(h.dir, "s.sock")
	ln, err := net.Listen("unix", stale)
	if err != nil {
		t.Fatal(err)
	}
	ln.(*net.UnixListener).SetUnlinkOnClose(false)
	ln.Close()
	if _, err := os.Lstat(stale); err != nil {
		t.Skipf("이 OS 는 소켓 파일을 남기지 않음: %v", err)
	}
	srv, err := NewServer(Config{SocketPath: stale, StorePath: filepath.Join(h.dir, "y.json"), AuthMode: AuthOff, Logf: h.logs.Logf})
	if err != nil {
		t.Fatalf("남은 소켓 파일: %v", err)
	}
	srv.Close()
	// 내용 있는 일반 파일은 지우지 않는다
	reg := filepath.Join(h.dir, "r.sock")
	os.WriteFile(reg, []byte("data"), 0o644)
	if _, err := NewServer(Config{SocketPath: reg, StorePath: filepath.Join(h.dir, "z.json"), AuthMode: AuthOff, Logf: h.logs.Logf}); err == nil {
		t.Fatal("일반 파일을 지우면 안 됨")
	}
}

// ---- 프로토콜 ----

func rawCall(t *testing.T, sock, line string) map[string]any {
	t.Helper()
	conn, err := net.Dial("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	go conn.Write([]byte(line + "\n"))
	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	resp, err := bufio.NewReader(conn).ReadString('\n')
	if err != nil {
		t.Fatalf("응답 읽기: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(resp), &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func TestOversizedLineRejected(t *testing.T) {
	h := newHarness(t, nil)
	m := rawCall(t, h.sock, `{"op":"ping","pad":"`+strings.Repeat("x", MaxLineSize)+`"}`)
	if m["ok"] != false || m["error"] != msgTooLarge {
		t.Fatalf("큰 줄 응답 = %v", m)
	}
	// 데몬은 계속 산다
	if err := h.client().Ping(); err != nil {
		t.Fatal(err)
	}
	// 클라이언트도 보내기 전에 거른다
	_, err := h.client().Run("alice", "bob", []string{strings.Repeat("a", MaxLineSize)})
	if err == nil || !strings.Contains(err.Error(), "1 MiB") {
		t.Fatalf("클라이언트 크기 검사: %v", err)
	}
}

func TestUnknownOpAndBadJSON(t *testing.T) {
	h := newHarness(t, nil)
	if m := rawCall(t, h.sock, `{"op":"rm_rf"}`); m["ok"] != false || !strings.Contains(m["error"].(string), "알 수 없는 op") {
		t.Fatalf("알 수 없는 op = %v", m)
	}
	if m := rawCall(t, h.sock, `{"op":`); m["ok"] != false || m["error"] != "요청 JSON 형식 오류" {
		t.Fatalf("깨진 JSON = %v", m)
	}
	if m := rawCall(t, h.sock, `{"op":"ping"}`); m["ok"] != true {
		t.Fatalf("ping = %v", m)
	}
	// list 결과의 JSON 키에 비밀번호류가 없음
	c := h.client()
	h.run(c, "alice", "bob", []string{"vm01ev01"})
	m := rawCall(t, h.sock, `{"op":"list","user":"bob"}`)
	b, _ := json.Marshal(m)
	for _, k := range []string{"password", "token", "worker_uid"} {
		if strings.Contains(string(b), k) {
			t.Errorf("list 응답에 %s 가 있음: %s", k, b)
		}
	}
}

func TestPeerUIDRecordedOnly(t *testing.T) {
	var mu sync.Mutex
	uid, known := 4321, true
	h := newHarness(t, func(h *harness, cfg *Config) {
		cfg.PeerUID = func(net.Conn) (int, bool) {
			mu.Lock()
			defer mu.Unlock()
			return uid, known
		}
	})
	c := h.client()
	rh := h.run(c, "alice", "bob", []string{"vm01ev01"})
	if j := h.job(rh.ID); j.WorkerUID != 4321 {
		t.Fatalf("WorkerUID = %d", j.WorkerUID)
	}
	// 같은 uid 여도(모두 root) 승인은 막지 않는다. uid 를 몰라도 아무것도 거부하지 않는다.
	mu.Lock()
	known = false
	mu.Unlock()
	c2 := h.client()
	if err := c2.Approve(rh.ID, "bob", testPasswords["bob"]); err != nil {
		t.Fatal(err)
	}
	if j := h.job(rh.ID); j.CheckerUID != -1 {
		t.Fatalf("모르는 uid 는 -1 로 기록: %d", j.CheckerUID)
	}
}

// ---- 동시성 ----

func TestConcurrentJobs(t *testing.T) {
	h := newHarness(t, nil)
	const n = 30
	var wg sync.WaitGroup
	errs := make(chan error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			c, err := Dial(h.sock)
			if err != nil {
				errs <- err
				return
			}
			defer c.Close()
			rh, err := c.Run("alice", "bob", []string{aliceTargets[i%len(aliceTargets)]})
			if err != nil {
				errs <- err
				return
			}
			defer rh.Close()
			if i%2 == 0 {
				err = c.Approve(rh.ID, "bob", testPasswords["bob"])
			} else {
				err = c.Cancel(rh.ID, "bob", testPasswords["bob"])
			}
			if err != nil {
				errs <- err
				return
			}
			werr := rh.Wait(context.Background())
			var ae *AbortedError
			switch {
			case i%2 == 0 && werr != nil:
				errs <- fmt.Errorf("%d: 승인 기대, %v", i, werr)
			case i%2 == 1 && !errors.As(werr, &ae):
				errs <- fmt.Errorf("%d: 중단 기대, %v", i, werr)
			case i%2 == 0:
				if err := c.Start(rh.ID, rh.Token); err != nil {
					errs <- fmt.Errorf("%d: start %v", i, err)
				}
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
	approved, aborted := 0, 0
	for _, j := range h.srv.st.List() {
		switch {
		case j.Status == StatusApproved && j.StartedAt != nil:
			approved++
		case j.Status == StatusAborted:
			aborted++
		}
	}
	if approved != n/2 || aborted != n/2 {
		t.Fatalf("승인+시작 %d, 중단 %d", approved, aborted)
	}
}

// ---- 설정 파일 ----

func TestLoadConf(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "cross_check.conf")
	abs := "/etc/vcetner_pwon_off/targets"

	os.WriteFile(p, []byte("# 주석\n\ntarget_dir="+abs+"\nauth=required\nusers= hong, kim ,lee_2\napproval_ttl_sec=30\n"), 0o644)
	c, err := LoadConf(p)
	if err != nil || c.TargetDir != filepath.Clean(abs) || c.AuthMode != AuthRequired || c.ApprovalTTL != 30*time.Second ||
		strings.Join(c.Users, ",") != "hong,kim,lee_2" {
		t.Fatalf("LoadConf = %+v, %v", c, err)
	}
	var cfg Config
	c.Apply(&cfg)
	if cfg.TargetDir != c.TargetDir || cfg.ApprovalTTL != 30*time.Second || strings.Join(cfg.AllowedUsers, ",") != "hong,kim,lee_2" {
		t.Fatalf("Apply = %+v", cfg)
	}

	os.WriteFile(p, []byte("auth=off\n"), 0o644)
	if c, err := LoadConf(p); err != nil || c.AuthMode != AuthOff || c.ApprovalTTL != DefaultApprovalTTL {
		t.Fatalf("auth=off 는 target_dir·users 없이 가능: %+v %v", c, err)
	}

	u := "\nusers=alice,bob\n"
	bad := map[string]string{
		"모르는 키":       "target_dir=" + abs + u + "foo=1\n",
		"중복 키":        "target_dir=" + abs + "\ntarget_dir=" + abs + u,
		"상대 경로":       "target_dir=rel/dir" + u,
		"auth 값":      "target_dir=" + abs + u + "auth=maybe\n",
		"ttl 값":       "target_dir=" + abs + u + "approval_ttl_sec=0\n",
		"형식":          "target_dir\n",
		"dir 필수":      "auth=required" + u,
		"dir 필수 2":    "# 비어 있음\n",
		"users 필수":    "target_dir=" + abs + "\n",
		"users 빈값":    "target_dir=" + abs + "\nusers=\n",
		"users admin": "target_dir=" + abs + "\nusers=alice,admin\n",
		"users 대문자":   "target_dir=" + abs + "\nusers=Alice\n",
		"users 빈 항목":  "target_dir=" + abs + "\nusers=alice,,bob\n",
		"users 중복":    "target_dir=" + abs + "\nusers=alice,alice\n",
		"users 중복 키":  "target_dir=" + abs + u + "users=carol\n",
	}
	for name, body := range bad {
		os.WriteFile(p, []byte(body), 0o644)
		if _, err := LoadConf(p); err == nil {
			t.Errorf("%s: 오류여야 함", name)
		}
	}
	// 파일이 없으면 기본값(required) → target_dir 가 없으니 오류(fail closed)
	if _, err := LoadConf(filepath.Join(dir, "none.conf")); err == nil || !strings.Contains(err.Error(), "target_dir") {
		t.Fatalf("파일 없음: %v", err)
	}
	// Config 도 required 에서 TargetDir 없으면 거부
	if _, err := NewServer(Config{SocketPath: filepath.Join(dir, "q.sock"), StorePath: filepath.Join(dir, "q.json"), AllowedUsers: []string{"a"}}); err == nil {
		t.Fatal("TargetDir 없는 required 데몬은 뜨면 안 됨")
	}
	// required 에서 users 가 없거나 잘못되면 거부(fail closed)
	for name, users := range map[string][]string{"없음": nil, "admin": {"alice", "admin"}, "대문자": {"Bob"}, "중복": {"a", "a"}} {
		_, err := NewServer(Config{SocketPath: filepath.Join(dir, "q.sock"), StorePath: filepath.Join(dir, "q.json"), TargetDir: abs, AllowedUsers: users})
		if err == nil {
			t.Errorf("users %s: 데몬이 뜨면 안 됨", name)
		} else if name == "없음" && !strings.Contains(err.Error(), "users=") {
			t.Errorf("users 없음 오류 문구가 불친절함: %v", err)
		}
	}
}

func TestSocketFromEnv(t *testing.T) {
	t.Setenv(EnvSocket, "")
	if SocketFromEnv() != DefaultSocketPath {
		t.Fatal("기본값")
	}
	t.Setenv(EnvSocket, "/tmp/x.sock")
	if SocketFromEnv() != "/tmp/x.sock" {
		t.Fatal("환경변수")
	}
}
