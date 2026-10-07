package main

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// 모든 테스트는 httptest mock 서버로만 한다 (실제 BMC 접속 금지).

const (
	testUser     = "rfuser"
	testPass     = `S3cr3t-P@ss!<"x">` // JSON 이스케이프(<, \") 형태 노출도 검사
	testToken    = "tok-SECRET-9f8e7d6c"
	testSessPath = "/redfish/v1/SessionService/Sessions/42"
)

// mockBMC 는 요청을 세는 가짜 Redfish 서버입니다. 설정 필드는 서버 시작 전에만 쓴다.
type mockBMC struct {
	// 설정
	sessionStatus int           // 0 이면 201 + 토큰
	location      string        // "" 이면 testSessPath, "-" 이면 Location 헤더 생략
	odataID       string        // "" 이면 testSessPath
	getStatus     int           // 0 이면 200 (/redfish/v1 포함)
	rootStatus    int           // /redfish/v1 전용 상태 (0 이면 getStatus 따름)
	patchStatus   int           // 0 이면 200
	jobStatus     int           // 0 이면 202
	getDelay      time.Duration // GET 응답 지연
	redirect      bool          // GET 에 302 → LogServices
	bigBody       bool          // GET 에 8MB 초과 본문
	echoSecrets   bool          // 오류 본문에 비밀번호·토큰·Basic 값을 그대로 되돌림

	srv *httptest.Server

	mu sync.Mutex
	n  mockCounts
}

// mockCounts 는 mock 서버가 받은 요청 집계입니다 (mu 보호).
type mockCounts struct {
	total       int
	sessCreate  int
	sessDelete  int
	gets        int
	patches     int
	jobs        int
	overrideHdr int
	reqs        []string // "METHOD 경로"
	auths       []string // 세션 POST 외 요청의 인증 방식: token/basic/none
	bodies      []string // PATCH·Job 본문
}

func newMock(t *testing.T, cfg func(m *mockBMC)) *mockBMC {
	t.Helper()
	m := &mockBMC{}
	if cfg != nil {
		cfg(m)
	}
	m.srv = httptest.NewUnstartedServer(m)
	m.srv.StartTLS()
	t.Cleanup(m.srv.Close)
	return m
}

func (m *mockBMC) echo() string {
	jp, _ := json.Marshal(testPass)
	b64 := base64.StdEncoding.EncodeToString([]byte(testUser + ":" + testPass))
	return fmt.Sprintf(`{"error":"pw=%s json=%s tok=%s basic=%s"}`, testPass, jp, testToken, b64)
}

func (m *mockBMC) fail(w http.ResponseWriter, status int) {
	w.WriteHeader(status)
	if m.echoSecrets {
		io.WriteString(w, m.echo())
	} else {
		io.WriteString(w, `{"error":"mock"}`)
	}
}

func (m *mockBMC) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	p := r.URL.Path
	m.mu.Lock()
	m.n.total++
	m.n.reqs = append(m.n.reqs, r.Method+" "+p)
	for _, h := range []string{"X-HTTP-Method-Override", "X-HTTP-Method", "X-Method-Override"} {
		if r.Header.Get(h) != "" {
			m.n.overrideHdr++
		}
	}
	if r.URL.RawQuery != "" {
		m.n.reqs = append(m.n.reqs, "QUERY "+r.URL.RawQuery)
	}
	m.mu.Unlock()

	if r.Method == http.MethodPost && p == "/redfish/v1/SessionService/Sessions" {
		m.mu.Lock()
		m.n.sessCreate++
		m.mu.Unlock()
		if m.sessionStatus != 0 {
			m.fail(w, m.sessionStatus)
			return
		}
		var cr struct{ UserName, Password string }
		_ = json.Unmarshal(body, &cr)
		if cr.UserName != testUser || cr.Password != testPass {
			m.fail(w, http.StatusUnauthorized)
			return
		}
		loc, id := m.location, m.odataID
		if loc == "" {
			loc = testSessPath
		}
		if id == "" {
			id = testSessPath
		}
		w.Header().Set("X-Auth-Token", testToken)
		if loc != "-" {
			w.Header().Set("Location", loc)
		}
		w.WriteHeader(http.StatusCreated)
		fmt.Fprintf(w, `{"@odata.id":%q}`, id)
		return
	}

	kind := "none"
	if r.Header.Get("X-Auth-Token") == testToken {
		kind = "token"
	} else if u, pw, ok := r.BasicAuth(); ok && u == testUser && pw == testPass {
		kind = "basic"
	}
	m.mu.Lock()
	m.n.auths = append(m.n.auths, kind)
	m.mu.Unlock()
	if kind == "none" {
		m.fail(w, http.StatusUnauthorized)
		return
	}

	switch r.Method {
	case http.MethodDelete:
		if p != testSessPath {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		m.mu.Lock()
		m.n.sessDelete++
		m.mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	case http.MethodGet:
		m.mu.Lock()
		m.n.gets++
		m.mu.Unlock()
		if m.getDelay > 0 {
			time.Sleep(m.getDelay)
		}
		st := m.getStatus
		if p == "/redfish/v1" && m.rootStatus != 0 {
			st = m.rootStatus
		}
		switch {
		case m.redirect:
			w.Header().Set("Location", "/redfish/v1/Managers/1/LogServices")
			w.WriteHeader(http.StatusFound)
		case m.bigBody:
			w.WriteHeader(http.StatusOK)
			w.Write(make([]byte, maxBody+10))
		case st != 0:
			m.fail(w, st)
		default:
			fmt.Fprintf(w, `{"@odata.id":%q,"Name":"mock"}`, p)
		}
	case http.MethodPatch:
		m.mu.Lock()
		m.n.patches++
		m.n.bodies = append(m.n.bodies, string(body))
		m.mu.Unlock()
		if m.patchStatus != 0 {
			m.fail(w, m.patchStatus)
			return
		}
		io.WriteString(w, `{}`)
	case http.MethodPost:
		m.mu.Lock()
		m.n.jobs++
		m.n.bodies = append(m.n.bodies, string(body))
		m.mu.Unlock()
		if m.jobStatus != 0 {
			m.fail(w, m.jobStatus)
			return
		}
		w.Header().Set("Location", "/redfish/v1/Managers/iDRAC.Embedded.1/Jobs/JID_1")
		w.WriteHeader(http.StatusAccepted)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (m *mockBMC) snapshot() mockCounts {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := m.n
	n.reqs = append([]string(nil), m.n.reqs...)
	n.auths = append([]string(nil), m.n.auths...)
	n.bodies = append([]string(nil), m.n.bodies...)
	return n
}

// sleepRec 는 실제로 자지 않고 대기 요청만 기록한다.
type sleepRec struct {
	mu sync.Mutex
	ds []time.Duration
}

func (s *sleepRec) sleep(d time.Duration) {
	s.mu.Lock()
	s.ds = append(s.ds, d)
	s.mu.Unlock()
}

func (s *sleepRec) get() []time.Duration {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]time.Duration(nil), s.ds...)
}

func newTestClient(m *mockBMC, mode Mode, retries int) (*Client, *sleepRec) {
	c := NewClient(ClientOpts{
		BaseURL: m.srv.URL, User: testUser, Pass: testPass, Insecure: true,
		Timeout: 5 * time.Second, Retries: retries, Mode: mode, Gap: -1,
	})
	sr := &sleepRec{}
	c.sleep = sr.sleep
	return c, sr
}

func rfStatus(t *testing.T, err error) string {
	t.Helper()
	var rf *RFError
	if !errors.As(err, &rf) {
		t.Fatalf("RFError 가 아님: %v", err)
	}
	return rf.Status
}

// (a) 허용목록 표 테스트
func TestCheckAllowedTable(t *testing.T) {
	const own = testSessPath
	cases := []struct {
		method, path string
		mode         Mode
		sess         string
		ok           bool
		send         string // 허용일 때 기대 전송 경로 ("" 면 검사 생략)
	}{
		// --- 허용 ---
		{"GET", "/redfish/v1", ModeReadOnly, "", true, "/redfish/v1"},
		{"GET", "/redfish/v1/", ModeReadOnly, "", true, "/redfish/v1/"},
		{"GET", "/redfish/v1/Systems/System.Embedded.1/Bios", ModeReadOnly, "", true, ""},
		{"GET", "/redfish/v1/Systems/1/Bios/Settings", ModeReadOnly, "", true, ""},
		{"GET", "/redfish/v1/Registries/BiosAttributeRegistry.v1_0_0", ModeReadOnly, "", true, ""},
		{"GET", "/redfish/v1/Managers/iDRAC.Embedded.1", ModeReadOnly, "", true, ""},
		{"GET", "/redfish/v1/SessionService/Sessions", ModeSet, "", true, ""},
		{"GET", "/redfish/v1/Systems?$expand=*($levels=3)", ModeReadOnly, "", true, "/redfish/v1/Systems"},
		{"GET", "/redfish/v1/Systems?x=/redfish/v1/Managers/1/LogServices", ModeReadOnly, "", true, "/redfish/v1/Systems"},
		{"GET", "/redfish/v1/Systems#/../Managers/1/LogServices", ModeReadOnly, "", true, "/redfish/v1/Systems"},
		{"GET", "/redfish/v1/systems/1/../2", ModeReadOnly, "", true, "/redfish/v1/systems/2"},
		{"GET", "/redfish/v1/Systems/1/LogServices/../Bios", ModeReadOnly, "", true, "/redfish/v1/Systems/1/Bios"},
		{"POST", "/redfish/v1/SessionService/Sessions", ModeReadOnly, "", true, ""},
		{"POST", "/REDFISH/V1/SESSIONSERVICE/SESSIONS/", ModeReadOnly, "", true, "/REDFISH/V1/SESSIONSERVICE/SESSIONS/"},
		{"POST", "/redfish/v1/Managers/iDRAC.Embedded.1/Jobs", ModeSet, "", true, ""},
		{"PATCH", "/redfish/v1/Systems/System.Embedded.1/Bios/Settings", ModeSet, "", true, ""},
		{"PATCH", "/redfish/v1/systems/1/bios/settings/", ModeSet, "", true, "/redfish/v1/systems/1/bios/settings/"},
		{"PATCH", "/redfish/v1/Systems/1/Bios/Pending", ModeSet, "", true, ""},
		{"PATCH", "/redfish/v1/Systems/1/Bios/Settings?$x=1", ModeSet, "", true, "/redfish/v1/Systems/1/Bios/Settings"},
		{"DELETE", own, ModeReadOnly, own, true, own},
		{"DELETE", own + "/", ModeReadOnly, own, true, own + "/"},

		// --- 거부: 위험 Actions ---
		{"POST", "/redfish/v1/Managers/1/LogServices/Log1/Actions/LogService.ClearLog", ModeSet, "", false, ""},
		{"POST", "/redfish/v1/Systems/1/Actions/ComputerSystem.Reset", ModeSet, "", false, ""},
		{"POST", "/redfish/v1/Managers/iDRAC.Embedded.1/Actions/Manager.Reset", ModeSet, "", false, ""},
		{"POST", "/redfish/v1/Managers/iDRAC.Embedded.1/Actions/Oem/DellLCService.ClearLog", ModeSet, "", false, ""},
		{"POST", "/redfish/v1/Systems/1/Bios/Actions/Bios.ResetBios", ModeSet, "", false, ""},
		{"POST", "/redfish/v1/Systems/1/Actions/ComputerSystem.Reset?/redfish/v1/SessionService/Sessions", ModeSet, "", false, ""},
		{"POST", "/redfish/v1/SessionService/Sessions/42", ModeSet, "", false, ""},
		{"POST", "/redfish/v1/Managers/1/Jobs/JID_1", ModeSet, "", false, ""},
		{"POST", "/redfish/v1/Managers/1/Oem/Jobs", ModeSet, "", false, ""},
		{"POST", "/redfish/v1/Managers/1/JobsX", ModeSet, "", false, ""},
		{"POST", "/redfish/v1/Managers/1/Jobs", ModeReadOnly, "", false, ""},
		// --- 거부: 로그 조회 ---
		{"GET", "/redfish/v1/Managers/1/LogServices", ModeReadOnly, "", false, ""},
		{"GET", "/redfish/v1/Managers/1/LogServices/Sel/Entries", ModeReadOnly, "", false, ""},
		{"GET", "/redfish/v1/Systems/1/LogServices/EventLog/Entries/1", ModeReadOnly, "", false, ""},
		{"GET", "/REDFISH/V1/MANAGERS/1/LOGSERVICES", ModeReadOnly, "", false, ""},
		{"GET", "/redfish/v1/Managers/iDRAC.Embedded.1/Logs/Lclog", ModeReadOnly, "", false, ""},
		{"GET", "/redfish/v1/Managers/iDRAC.Embedded.1/Logs/Sel", ModeReadOnly, "", false, ""},
		{"GET", "/redfish/v1/Managers/1/AuditLog", ModeReadOnly, "", false, ""},
		{"GET", "/redfish/v1/Systems/1/Actions", ModeReadOnly, "", false, ""},
		{"GET", "/redfish/v1/Systems/1/Actions/ComputerSystem.Reset", ModeSet, "", false, ""},
		// --- 거부: 우회 시도 ---
		{"GET", "/redfish/v1/Systems/1/../../v1/Managers/1/LogServices", ModeReadOnly, "", false, ""},
		{"GET", "/redfish/v1//Managers//1//LogServices", ModeReadOnly, "", false, ""},
		{"GET", "/redfish/v1/Systems/%2e%2e/Managers/1/LogServices", ModeReadOnly, "", false, ""},
		{"GET", "/redfish/v1/Managers/1/%4c%6f%67%53%65%72%76%69%63%65%73", ModeReadOnly, "", false, ""},
		{"GET", "/redfish/v1/Managers/1/%254c%256f%2567Services", ModeReadOnly, "", false, ""},
		{"GET", "/redfish/v1/Managers/1/%25254c%25256f%252567Services", ModeReadOnly, "", false, ""},
		{"GET", "/redfish/v1/Managers/1/Log%Services", ModeReadOnly, "", false, ""},
		{"GET", "/redfish/v1/Managers/1/LogServices%3fx", ModeReadOnly, "", false, ""},
		{"GET", "/redfish/v1/Managers/1/LogServices;x=1", ModeReadOnly, "", false, ""},
		{"GET", "/redfish/v1/Managers/1/..;/LogServices", ModeReadOnly, "", false, ""},
		{"GET", `/redfish/v1/Managers/1\LogServices`, ModeReadOnly, "", false, ""},
		{"GET", "/redfish/v1/Managers/1/ＬogServices", ModeReadOnly, "", false, ""},
		{"GET", "/redfish/v1/Systems\r\nX-HTTP-Method-Override: DELETE", ModeReadOnly, "", false, ""},
		{"GET", "/redfish/v1/Systems/1 HTTP/1.1", ModeReadOnly, "", false, ""},
		{"GET", "/redfish/v1/../../etc/passwd", ModeReadOnly, "", false, ""},
		{"GET", "/redfish/v1foo", ModeReadOnly, "", false, ""},
		{"GET", "/other", ModeReadOnly, "", false, ""},
		{"GET", "", ModeReadOnly, "", false, ""},
		{"GET", "redfish/v1", ModeReadOnly, "", false, ""},
		{"GET", "https://evil.example/redfish/v1", ModeReadOnly, "", false, ""},
		{"GET", "//evil.example/redfish/v1", ModeReadOnly, "", false, ""},
		// --- 거부: PATCH ---
		{"PATCH", "/redfish/v1/Systems/1/Bios/Settings", ModeReadOnly, "", false, ""},
		{"PATCH", "/redfish/v1/Systems/1/Bios", ModeSet, "", false, ""},
		{"PATCH", "/redfish/v1/Systems/1/Bios/SettingsX", ModeSet, "", false, ""},
		{"PATCH", "/redfish/v1/Systems/1/Extra/Bios/Settings", ModeSet, "", false, ""},
		{"PATCH", "/redfish/v1/Managers/1/Bios/Settings", ModeSet, "", false, ""},
		{"PATCH", "/x/redfish/v1/Systems/1/Bios/Settings", ModeSet, "", false, ""},
		{"PATCH", "/redfish/v1/Managers/iDRAC.Embedded.1/Attributes", ModeSet, "", false, ""},
		{"PATCH", "/redfish/v1/AccountService/Accounts/2", ModeSet, "", false, ""},
		// --- 거부: DELETE ---
		{"DELETE", "/redfish/v1/SessionService/Sessions/99", ModeSet, own, false, ""},
		{"DELETE", own, ModeSet, "", false, ""},
		{"DELETE", "/redfish/v1/sessionservice/sessions/42", ModeSet, own, false, ""},
		{"DELETE", own + "/../43", ModeSet, own, false, ""},
		{"DELETE", "/redfish/v1/Managers/1/LogServices/Sel/Entries", ModeSet, own, false, ""},
		{"DELETE", "/redfish/v1/Managers/1/Jobs/JID_1", ModeSet, own, false, ""},
		{"DELETE", "/redfish/v1/Managers/1/Jobs/JID_CLEARALL", ModeSet, own, false, ""},
		{"DELETE", "/redfish/v1/Systems/1/Bios/Settings", ModeSet, "/redfish/v1/Systems/1/Bios/Settings", false, ""},
		// --- 거부: 그 외 메서드 ---
		{"PUT", "/redfish/v1/Systems/1/Bios/Settings", ModeSet, "", false, ""},
		{"HEAD", "/redfish/v1", ModeSet, "", false, ""},
		{"OPTIONS", "/redfish/v1", ModeSet, "", false, ""},
		{"get", "/redfish/v1", ModeSet, "", false, ""},
		{"GET ", "/redfish/v1", ModeSet, "", false, ""},
		{"Patch", "/redfish/v1/Systems/1/Bios/Settings", ModeSet, "", false, ""},
	}
	if len(cases) < 40 {
		t.Fatalf("케이스 수 %d < 40", len(cases))
	}
	for _, tc := range cases {
		send, err := checkAllowed(tc.method, tc.path, tc.mode, tc.sess)
		if tc.ok {
			if err != nil {
				t.Errorf("%s %q mode=%d: 허용이어야 함: %v", tc.method, tc.path, tc.mode, err)
			} else if tc.send != "" && send != tc.send {
				t.Errorf("%s %q: 전송 경로 %q, 기대 %q", tc.method, tc.path, send, tc.send)
			}
			continue
		}
		if !errors.Is(err, ErrNotAllowed) {
			t.Errorf("%s %q mode=%d sess=%q: 거부여야 함 (send=%q, err=%v)", tc.method, tc.path, tc.mode, tc.sess, send, err)
		}
	}
}

// 설정 본문 가드: ApplyTime=Immediate(즉시 재부팅) 등 거부, 중복 키 정리.
func TestCheckBody(t *testing.T) {
	const bios = "/redfish/v1/Systems/1/Bios/Settings"
	const job = "/redfish/v1/Managers/iDRAC.Embedded.1/Jobs"
	cases := []struct {
		method, path, body string
		ok                 bool
	}{
		{"PATCH", bios, `{"Attributes":{"LogicalProc":"Enabled"}}`, true},
		{"PATCH", bios, `{"Attributes":{"A":"B"},"@Redfish.SettingsApplyTime":{"ApplyTime":"OnReset"}}`, true},
		{"PATCH", bios, `{"Attributes":{"A":"B"},"@Redfish.SettingsApplyTime":{"ApplyTime":"Immediate"}}`, false},
		{"PATCH", bios, `{"Attributes":{"A":"B"},"@Redfish.SettingsApplyTime":{"ApplyTime":"AtMaintenanceWindowStart"}}`, false},
		{"PATCH", bios, `{"Attributes":{"A":"B"},"@Redfish.SettingsApplyTime":{"ApplyTime":"OnReset","MaintenanceWindowStartTime":"x"}}`, false},
		{"PATCH", bios, `{"Attributes":{"A":"B"},"@redfish.settingsapplytime":{"ApplyTime":"Immediate"}}`, false},
		{"PATCH", bios, `{"Attributes":{"A":"B"},"Reset":true}`, false},
		{"PATCH", bios, `{"@Redfish.SettingsApplyTime":{"ApplyTime":"OnReset"}}`, false},
		{"PATCH", bios, `{"Attributes":[1]}`, false},
		{"PATCH", bios, `null`, false},
		{"PATCH", bios, `[]`, false},
		{"POST", job, `{"TargetSettingsURI":"/redfish/v1/Systems/System.Embedded.1/Bios/Settings"}`, true},
		{"POST", job, `{"TargetSettingsURI":"/redfish/v1/Systems/System.Embedded.1/Bios/Settings","ScheduledStartTime":"TIME_NOW"}`, true},
		{"POST", job, `{"TargetSettingsURI":"/redfish/v1/Managers/iDRAC.Embedded.1/Attributes"}`, false},
		{"POST", job, `{"TargetSettingsURI":"/redfish/v1/Systems/1/Bios/Settings/../../../../Managers/1/Attributes"}`, false},
		{"POST", job, `{"TargetSettingsURI":"/redfish/v1/Systems/1/Bios/Settings","RebootJobType":"PowerCycle"}`, false},
		{"POST", job, `{"TargetSettingsURI":1}`, false},
		{"POST", job, `{"ScheduledStartTime":"TIME_NOW"}`, false},
	}
	for _, tc := range cases {
		_, err := checkBody(tc.method, tc.path, []byte(tc.body))
		if (err == nil) != tc.ok {
			t.Errorf("%s %s: ok=%v 기대 %v (err=%v)", tc.method, tc.body, err == nil, tc.ok, err)
		}
	}
	// 중복 키: BMC 가 첫 값을 쓰는 경우를 막기 위해 정규형으로 다시 만든다.
	dup := `{"Attributes":{"A":"B"},"@Redfish.SettingsApplyTime":{"ApplyTime":"Immediate"},"@Redfish.SettingsApplyTime":{"ApplyTime":"OnReset"}}`
	dup2 := `{"Attributes":{"A":"B"},"@Redfish.SettingsApplyTime":{"ApplyTime":"Immediate","ApplyTime":"OnReset"}}`
	for _, d := range []string{dup, dup2} {
		out, err := checkBody("PATCH", bios, []byte(d))
		if err != nil {
			t.Fatalf("중복 키 본문: %v", err)
		}
		if strings.Contains(string(out), "Immediate") || strings.Count(string(out), `"ApplyTime"`) != 1 {
			t.Errorf("정규화 실패: %s", out)
		}
	}
}

// (b) 호스트당 세션 생성 1·삭제 1, 요청 N개여도 로그인 1번.
func TestSessionOncePerHost(t *testing.T) {
	for h := 0; h < 3; h++ {
		m := newMock(t, nil)
		c, _ := newTestClient(m, ModeReadOnly, 2)
		for i := 0; i < 10; i++ {
			var v map[string]interface{}
			if err := c.GetJSON("/redfish/v1/Systems/"+strconv.Itoa(i), &v); err != nil {
				t.Fatal(err)
			}
		}
		if err := c.Login(); err != nil { // 명시적 재호출도 POST 하지 않음
			t.Fatal(err)
		}
		if err := c.Close(); err != nil {
			t.Fatal(err)
		}
		s := m.snapshot()
		if s.sessCreate != 1 || s.sessDelete != 1 || s.gets != 10 {
			t.Errorf("host%d: 세션생성=%d 삭제=%d GET=%d (기대 1/1/10)", h, s.sessCreate, s.sessDelete, s.gets)
		}
		for _, a := range s.auths {
			if a != "token" {
				t.Errorf("세션 토큰 인증이어야 함: %v", s.auths)
				break
			}
		}
		st := c.Stats()
		if st.Sessions != 1 || st.Deletes != 1 || st.Gets != 10 || st.Posts != 1 || st.AuthMode != "session" {
			t.Errorf("Stats=%+v", st)
		}
	}
}

// (c) 401 → 로그인 1회만, 재시도 없음. 이후 호출도 다시 POST 하지 않음. 로그인 403 도 AUTH_FAIL.
func TestLoginAuthFailNoRetry(t *testing.T) {
	for _, code := range []int{http.StatusUnauthorized, http.StatusForbidden} {
		m := newMock(t, func(m *mockBMC) { m.sessionStatus = code })
		c, sr := newTestClient(m, ModeReadOnly, 5)
		err := c.Login()
		if rfStatus(t, err) != StatusAuthFail {
			t.Errorf("%d: 상태 %v", code, err)
		}
		if _, err := c.GetRaw("/redfish/v1"); rfStatus(t, err) != StatusAuthFail {
			t.Errorf("이후 GET 도 같은 AUTH_FAIL 이어야 함: %v", err)
		}
		_ = c.Close()
		s := m.snapshot()
		if s.sessCreate != 1 || s.total != 1 || len(sr.get()) != 0 {
			t.Errorf("%d: 세션POST=%d 전체=%d sleep=%v (기대 1/1/없음)", code, s.sessCreate, s.total, sr.get())
		}
	}
	// GET 401 도 재시도 없음
	m := newMock(t, func(m *mockBMC) { m.getStatus = http.StatusUnauthorized })
	c, sr := newTestClient(m, ModeReadOnly, 5)
	if _, err := c.GetRaw("/redfish/v1/Systems"); rfStatus(t, err) != StatusAuthFail {
		t.Errorf("GET 401: %v", err)
	}
	if s := m.snapshot(); s.gets != 1 || len(sr.get()) != 0 {
		t.Errorf("GET 401 재시도됨: gets=%d sleep=%v", s.gets, sr.get())
	}

	// Basic 폴백(비밀번호 틀림) → 첫 GET 401 이후에는 아무것도 보내지 않는다 (계정 잠금 방지).
	m2 := newMock(t, func(m *mockBMC) { m.sessionStatus = 404 })
	c2 := NewClient(ClientOpts{BaseURL: m2.srv.URL, User: testUser, Pass: "wrong", Insecure: true, Retries: 3, Gap: -1})
	for i := 0; i < 5; i++ {
		if _, err := c2.GetRaw("/redfish/v1/Systems"); rfStatus(t, err) != StatusAuthFail {
			t.Errorf("#%d: %v", i, err)
		}
	}
	_ = c2.Close()
	if s := m2.snapshot(); s.total != 2 { // 세션 POST 1 + GET 1
		t.Errorf("401 이후에도 요청이 나감: %v", s.reqs)
	}
}

// (d) GET 5xx 는 Retries 만큼 재시도(300ms×2^n) 후 BMC_ERROR, PATCH·Job 5xx 는 재시도 0.
func TestRetryPolicy(t *testing.T) {
	m := newMock(t, func(m *mockBMC) { m.getStatus = http.StatusServiceUnavailable; m.patchStatus = 500; m.jobStatus = 503 })
	c, sr := newTestClient(m, ModeSet, 2)
	_, err := c.GetRaw("/redfish/v1/Systems")
	if rfStatus(t, err) != StatusBMCError {
		t.Errorf("상태: %v", err)
	}
	s := m.snapshot()
	got := sr.get()
	if s.gets != 3 || len(got) != 2 || got[0] != 300*time.Millisecond || got[1] != 600*time.Millisecond {
		t.Errorf("GET 시도=%d 백오프=%v (기대 3, [300ms 600ms])", s.gets, got)
	}

	st, _, err := c.Patch("/redfish/v1/Systems/1/Bios/Settings", map[string]interface{}{"Attributes": map[string]string{"A": "B"}})
	if st != 500 || rfStatus(t, err) != StatusBMCError {
		t.Errorf("PATCH: st=%d err=%v", st, err)
	}
	st, _, _, err = c.PostJob("/redfish/v1/Managers/iDRAC.Embedded.1/Jobs", map[string]string{"TargetSettingsURI": "/redfish/v1/Systems/1/Bios/Settings"})
	if st != 503 || rfStatus(t, err) != StatusBMCError {
		t.Errorf("Job: st=%d err=%v", st, err)
	}
	s = m.snapshot()
	if s.patches != 1 || s.jobs != 1 || len(sr.get()) != 2 {
		t.Errorf("PATCH=%d Job=%d sleep=%v (재시도 없어야 함)", s.patches, s.jobs, sr.get())
	}
}

// 연결 불가: GET 은 재시도 후 UNREACHABLE, 세션 POST 는 dial 실패라 재시도.
func TestUnreachableRetry(t *testing.T) {
	m := newMock(t, nil)
	url := m.srv.URL
	c, sr := newTestClient(m, ModeReadOnly, 2)
	if err := c.Login(); err != nil {
		t.Fatal(err)
	}
	m.srv.Close() // 이후 연결 거부
	if _, err := c.GetRaw("/redfish/v1/Systems"); rfStatus(t, err) != StatusUnreachable {
		t.Errorf("GET: %v", err)
	}
	if n := len(sr.get()); n != 2 {
		t.Errorf("GET 재시도 sleep %d회 (기대 2)", n)
	}
	_ = c.Close() // 서버가 죽어도 패닉 없이

	c2 := NewClient(ClientOpts{BaseURL: url, User: testUser, Pass: testPass, Insecure: true, Retries: 3, Gap: -1})
	sr2 := &sleepRec{}
	c2.sleep = sr2.sleep
	if err := c2.Login(); rfStatus(t, err) != StatusUnreachable {
		t.Errorf("Login: %v", err)
	}
	if n := len(sr2.get()); n != 3 {
		t.Errorf("세션 POST dial 실패 재시도 %d회 (기대 3)", n)
	}
}

// 세션 POST 가 연결된 뒤 실패(5xx)하면 재시도하지 않고, 폴백도 하지 않는다.
func TestLoginServerErrorNoRetryNoFallback(t *testing.T) {
	for _, code := range []int{500, 503, 400} {
		m := newMock(t, func(m *mockBMC) { m.sessionStatus = code })
		c, sr := newTestClient(m, ModeReadOnly, 3)
		if err := c.Login(); err == nil {
			t.Errorf("%d: 오류여야 함", code)
		}
		_, _ = c.GetRaw("/redfish/v1")
		if s := m.snapshot(); s.sessCreate != 1 || s.total != 1 || len(sr.get()) != 0 || c.Stats().AuthMode == "basic" {
			t.Errorf("%d: 세션POST=%d 전체=%d sleep=%v mode=%q", code, s.sessCreate, s.total, sr.get(), c.Stats().AuthMode)
		}
	}
}

func TestTimeout(t *testing.T) {
	m := newMock(t, func(m *mockBMC) { m.getDelay = 400 * time.Millisecond })
	c := NewClient(ClientOpts{BaseURL: m.srv.URL, User: testUser, Pass: testPass, Insecure: true,
		Timeout: 100 * time.Millisecond, Retries: 1, Gap: -1})
	sr := &sleepRec{}
	c.sleep = sr.sleep
	if _, err := c.GetRaw("/redfish/v1/Systems"); rfStatus(t, err) != StatusTimeout {
		t.Errorf("TIMEOUT 이어야 함: %v", err)
	}
	if s := m.snapshot(); s.gets != 2 {
		t.Errorf("GET 시도 %d (기대 2)", s.gets)
	}
}

// (e) 세션 404/405/501 → Basic 폴백, 이후 Authorization: Basic, 세션 DELETE 없음.
func TestBasicFallback(t *testing.T) {
	for _, code := range []int{404, 405, 501} {
		m := newMock(t, func(m *mockBMC) { m.sessionStatus = code })
		c, _ := newTestClient(m, ModeReadOnly, 1)
		if err := c.Login(); err != nil {
			t.Fatalf("%d: %v", code, err)
		}
		for i := 0; i < 3; i++ {
			if _, err := c.GetRaw("/redfish/v1/Systems"); err != nil {
				t.Fatal(err)
			}
		}
		if err := c.Close(); err != nil {
			t.Fatal(err)
		}
		s := m.snapshot()
		if c.Stats().AuthMode != "basic" || c.Stats().Sessions != 0 {
			t.Errorf("%d: stats=%+v", code, c.Stats())
		}
		if s.sessCreate != 1 || s.sessDelete != 0 || c.Stats().Deletes != 0 {
			t.Errorf("%d: 세션생성=%d 삭제=%d", code, s.sessCreate, s.sessDelete)
		}
		for _, a := range s.auths {
			if a != "basic" {
				t.Errorf("%d: Basic 인증이어야 함: %v", code, s.auths)
				break
			}
		}
	}
}

// (f) Close 멱등·실패해도 패닉 없음, Close 후 재로그인 없음.
func TestCloseIdempotent(t *testing.T) {
	m := newMock(t, nil)
	c, _ := newTestClient(m, ModeReadOnly, 0)
	if err := c.Close(); err != nil { // 로그인 전 Close: 아무것도 안 보냄
		t.Fatal(err)
	}
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := c.GetRaw("/redfish/v1"); !errors.Is(err, errClosed) {
		t.Errorf("Close 후 호출은 errClosed: %v", err)
	}
	if s := m.snapshot(); s.total != 0 {
		t.Errorf("요청이 나감: %v", s.reqs)
	}

	m2 := newMock(t, nil)
	c2, _ := newTestClient(m2, ModeReadOnly, 0)
	if err := c2.Login(); err != nil {
		t.Fatal(err)
	}
	m2.srv.Close()
	err := c2.Close() // 실패해도 패닉 없이 오류만
	if err == nil {
		t.Errorf("서버가 죽었으면 DELETE 오류가 나와야 함")
	}
	if err := c2.Close(); err != nil {
		t.Errorf("두 번째 Close 는 nil: %v", err)
	}
	if st := c2.Stats(); st.Deletes != 1 {
		t.Errorf("DELETE 시도는 1회(재시도 없음): %+v", st)
	}
}

// (g) 거부된 호출은 mock 서버에 요청이 도달하지 않는다 (로그인 POST 도 없음).
func TestDeniedNotSent(t *testing.T) {
	m := newMock(t, nil)
	ro, _ := newTestClient(m, ModeReadOnly, 3)
	set, _ := newTestClient(m, ModeSet, 3)
	attrs := map[string]interface{}{"Attributes": map[string]string{"A": "B"}}
	var v interface{}
	errs := []error{
		ro.GetJSON("/redfish/v1/Managers/1/LogServices/Sel/Entries", &v),
		ro.GetJSON("/REDFISH/V1/Managers/1/%4c%6f%67%53%65%72%76%69%63%65%73", &v),
		ro.GetJSON("https://evil.example/redfish/v1", &v),
		set.GetJSON("/redfish/v1/Systems/1/Actions", &v),
	}
	_, _, e := ro.Patch("/redfish/v1/Systems/1/Bios/Settings", attrs)
	errs = append(errs, e)
	_, _, _, e = ro.PostJob("/redfish/v1/Managers/1/Jobs", map[string]string{"TargetSettingsURI": "/redfish/v1/Systems/1/Bios/Settings"})
	errs = append(errs, e)
	_, _, e = set.Patch("/redfish/v1/Systems/1/Actions/ComputerSystem.Reset", attrs)
	errs = append(errs, e)
	_, _, e = set.Patch("/redfish/v1/Systems/1/Bios/Settings", map[string]interface{}{
		"Attributes": map[string]string{"A": "B"}, "@Redfish.SettingsApplyTime": map[string]string{"ApplyTime": "Immediate"}})
	errs = append(errs, e)
	_, _, _, e = set.PostJob("/redfish/v1/Managers/1/LogServices/Sel/Actions/LogService.ClearLog", map[string]string{})
	errs = append(errs, e)
	_, _, _, e = set.PostJob("/redfish/v1/Systems/1/Actions/ComputerSystem.Reset", map[string]string{"ResetType": "ForceRestart"})
	errs = append(errs, e)
	// PostJob 으로 세션을 추가로 만들 수 없다
	_, _, _, e = set.PostJob("/redfish/v1/SessionService/Sessions", map[string]string{"UserName": "x", "Password": "y"})
	errs = append(errs, e)
	_, _, _, e = set.PostJob("/redfish/v1/Managers/1/Jobs", map[string]string{"TargetSettingsURI": "/redfish/v1/Managers/1/Attributes"})
	errs = append(errs, e)
	_, _, _, e = set.do("PUT", "/redfish/v1/Systems/1/Bios/Settings", attrs)
	errs = append(errs, e)
	_, _, _, e = set.do("DELETE", "/redfish/v1/SessionService/Sessions/99", nil)
	errs = append(errs, e)
	_, _, _, e = set.do("DELETE", "/redfish/v1/Managers/1/Jobs/JID_CLEARALL", nil)
	errs = append(errs, e)
	for i, err := range errs {
		if !errors.Is(err, ErrNotAllowed) {
			t.Errorf("#%d: ErrNotAllowed 여야 함: %v", i, err)
		}
	}
	if s := m.snapshot(); s.total != 0 {
		t.Errorf("거부된 호출이 서버에 도달: %v", s.reqs)
	}
	if ro.Stats() != (CallStats{}) || set.Stats() != (CallStats{}) {
		t.Errorf("거부된 호출이 통계에 잡힘: %+v %+v", ro.Stats(), set.Stats())
	}
}

// 허용 경로의 정상 설정 흐름 + 쿼리 미전송 + 메서드 오버라이드 헤더 미전송.
func TestSetFlowAndNoQueryNoOverride(t *testing.T) {
	m := newMock(t, nil)
	c, _ := newTestClient(m, ModeSet, 0)
	if _, err := c.GetRaw("/redfish/v1/Systems?$expand=*($levels=5)"); err != nil {
		t.Fatal(err)
	}
	st, _, err := c.Patch("/redfish/v1/systems/1/bios/settings/", map[string]interface{}{
		"Attributes": map[string]string{"LogicalProc": "Enabled"}, "@Redfish.SettingsApplyTime": map[string]string{"ApplyTime": "OnReset"}})
	if err != nil || st != 200 {
		t.Fatalf("PATCH: %d %v", st, err)
	}
	st, hdr, _, err := c.PostJob("/redfish/v1/Managers/iDRAC.Embedded.1/Jobs", map[string]string{
		"TargetSettingsURI": "/redfish/v1/Systems/System.Embedded.1/Bios/Settings", "ScheduledStartTime": "TIME_NOW"})
	if err != nil || st != 202 || hdr.Get("Location") == "" {
		t.Fatalf("Job: %d %v", st, err)
	}
	_ = c.Close()
	s := m.snapshot()
	want := []string{
		"POST /redfish/v1/SessionService/Sessions", "GET /redfish/v1/Systems",
		"PATCH /redfish/v1/systems/1/bios/settings/", "POST /redfish/v1/Managers/iDRAC.Embedded.1/Jobs",
		"DELETE " + testSessPath,
	}
	if strings.Join(s.reqs, "|") != strings.Join(want, "|") {
		t.Errorf("요청 순서/경로\n got=%q\nwant=%q", s.reqs, want)
	}
	if s.overrideHdr != 0 {
		t.Errorf("메서드 오버라이드 헤더가 전송됨")
	}
}

// 세션 URI: 절대 URL 이면 경로만(호스트는 BaseURL), SessionService/Sessions/<id> 꼴이 아니면 DELETE 안 함.
func TestSessionURIValidation(t *testing.T) {
	cases := []struct {
		loc, id    string
		wantDelete int
	}{
		{"https://evil.example:1" + testSessPath, "", 1},
		{"-", testSessPath, 1},
		{"/redfish/v1/Managers/1/LogServices/Sel/Entries", "/redfish/v1/Managers/1/LogServices/Sel/Entries", 0},
		{"/redfish/v1/Systems/1", "-", 0},
		{"/redfish/v1/SessionService/Sessions/42/../../../Systems/1/Bios/Settings", "-", 0},
	}
	for _, tc := range cases {
		m := newMock(t, func(m *mockBMC) { m.location = tc.loc; m.odataID = tc.id })
		c, _ := newTestClient(m, ModeSet, 0)
		if err := c.Login(); err != nil {
			t.Fatal(err)
		}
		_ = c.Close()
		s := m.snapshot()
		if s.sessDelete != tc.wantDelete || len(s.reqs) != 1+tc.wantDelete {
			t.Errorf("loc=%q: 삭제=%d 요청=%v", tc.loc, s.sessDelete, s.reqs)
		}
	}
}

// 리다이렉트는 따라가지 않는다 (LogServices 로 302 → 서버에 그 경로 요청 없음).
func TestRedirectNotFollowed(t *testing.T) {
	m := newMock(t, func(m *mockBMC) { m.redirect = true })
	c, _ := newTestClient(m, ModeReadOnly, 2)
	_, err := c.GetRaw("/redfish/v1/Systems")
	if rfStatus(t, err) != StatusHTTPError {
		t.Errorf("3xx 는 HTTP_ERROR: %v", err)
	}
	for _, r := range m.snapshot().reqs {
		if strings.Contains(r, "LogServices") {
			t.Errorf("리다이렉트를 따라감: %v", m.snapshot().reqs)
		}
	}
}

func TestUnsupportedAndHTTPError(t *testing.T) {
	m := newMock(t, func(m *mockBMC) { m.sessionStatus = 404; m.getStatus = 404 })
	c, _ := newTestClient(m, ModeReadOnly, 2)
	if _, err := c.GetRaw("/redfish/v1"); rfStatus(t, err) != StatusUnsupported {
		t.Errorf("/redfish/v1 404 → UNSUPPORTED: %v", err)
	}
	_, err := c.GetRaw("/redfish/v1/Systems/1/Bios")
	var rf *RFError
	if !errors.As(err, &rf) || rf.Status != StatusHTTPError || rf.HTTP != 404 {
		t.Errorf("그 외 404 → HTTP_ERROR: %v", err)
	}
	if s := m.snapshot(); s.gets != 2 {
		t.Errorf("4xx 는 재시도하지 않음: gets=%d", s.gets)
	}
}

func TestBodyLimit(t *testing.T) {
	m := newMock(t, func(m *mockBMC) { m.bigBody = true })
	c, _ := newTestClient(m, ModeReadOnly, 2)
	if _, err := c.GetRaw("/redfish/v1/Systems"); err == nil || !strings.Contains(err.Error(), "8MB") {
		t.Errorf("8MB 초과는 오류: %v", err)
	}
}

func TestGap(t *testing.T) {
	m := newMock(t, nil)
	c := NewClient(ClientOpts{BaseURL: m.srv.URL, User: testUser, Pass: testPass, Insecure: true}) // Gap 0 → 100ms
	sr := &sleepRec{}
	c.sleep = sr.sleep
	for i := 0; i < 3; i++ {
		if _, err := c.GetRaw("/redfish/v1"); err != nil {
			t.Fatal(err)
		}
	}
	got := sr.get()
	if len(got) != 3 { // 로그인→GET1→GET2→GET3 사이 3번
		t.Fatalf("간격 대기 %d회 (기대 3): %v", len(got), got)
	}
	for _, d := range got {
		if d <= 0 || d > defaultGap {
			t.Errorf("간격 %v 는 (0, 100ms] 이어야 함", d)
		}
	}
	m2 := newMock(t, nil)
	c2, sr2 := newTestClient(m2, ModeReadOnly, 0) // Gap -1 → 간격 없음
	for i := 0; i < 3; i++ {
		_, _ = c2.GetRaw("/redfish/v1")
	}
	if len(sr2.get()) != 0 {
		t.Errorf("Gap<0 인데 대기함: %v", sr2.get())
	}
}

// (h) 오류 문자열·%+v 출력에 비밀번호·토큰이 없다.
func TestNoSecretLeak(t *testing.T) {
	jp, _ := json.Marshal(testPass)
	b64 := base64.StdEncoding.EncodeToString([]byte(testUser + ":" + testPass))
	passForms := []string{testPass, strings.Trim(string(jp), `"`), b64}
	check := func(label, s string) {
		t.Helper()
		for _, sec := range append(passForms, testToken) {
			if strings.Contains(s, sec) {
				t.Errorf("%s 에 비밀값 노출: %s", label, s)
			}
		}
	}
	// 로그인 실패 본문에 비밀번호가 되돌아와도 Detail 에 없음.
	// (토큰은 이때 클라이언트가 받은 적이 없으므로 비밀번호 형태만 본다)
	m := newMock(t, func(m *mockBMC) { m.sessionStatus = 401; m.echoSecrets = true })
	c, _ := newTestClient(m, ModeReadOnly, 0)
	err := c.Login()
	for _, s := range []string{err.Error(), fmt.Sprintf("%+v %#v", err, err), fmt.Sprintf("%+v", c)} {
		for _, sec := range passForms {
			if strings.Contains(s, sec) {
				t.Errorf("401 오류에 비밀번호 노출: %s", s)
			}
		}
	}

	// 세션 후 GET 5xx 본문에 토큰이 되돌아와도 Detail 에 없음, Client 출력에도 없음
	m2 := newMock(t, func(m *mockBMC) { m.getStatus = 500; m.echoSecrets = true })
	c2, _ := newTestClient(m2, ModeReadOnly, 0)
	_, err = c2.GetRaw("/redfish/v1")
	check("500 오류", err.Error())
	for _, f := range []string{"%v", "%+v", "%#v", "%s", "%d", "%x", "%q"} {
		check("client "+f, fmt.Sprintf(f, c2))
	}
	check("stats", fmt.Sprintf("%+v", c2.Stats()))

	// Basic 모드: 되돌아온 Basic 헤더 값도 지운다
	m3 := newMock(t, func(m *mockBMC) { m.sessionStatus = 404; m.getStatus = 503; m.echoSecrets = true })
	c3, _ := newTestClient(m3, ModeReadOnly, 0)
	_, err = c3.GetRaw("/redfish/v1/Systems")
	for _, s := range []string{err.Error(), fmt.Sprintf("%+v", c3)} {
		for _, sec := range passForms { // basic 모드는 토큰을 받은 적이 없음
			if strings.Contains(s, sec) {
				t.Errorf("basic 모드 출력에 비밀번호 노출: %s", s)
			}
		}
	}

	// 거부 오류, BaseURL 오류
	_, _, err = c2.Patch("/redfish/v1/Systems/1/Bios/Settings", map[string]string{"Password": testPass})
	check("거부 오류", err.Error())
	bad := NewClient(ClientOpts{BaseURL: "https://" + testUser + ":" + testPass + "@host", User: testUser, Pass: testPass})
	check("BaseURL 오류", bad.Login().Error())
	httpc := NewClient(ClientOpts{BaseURL: "http://192.0.2.1", User: testUser, Pass: testPass})
	if err := httpc.Login(); err == nil || !strings.Contains(err.Error(), "https") {
		t.Errorf("평문 http 는 거부: %v", err)
	}
	// Detail 길이 200자 제한
	var rf *RFError
	if errors.As(c2.rfErr(StatusBMCError, 500, strings.Repeat("가", 500)), &rf) && len([]rune(rf.Detail)) != detailLimit {
		t.Errorf("Detail 길이 %d", len([]rune(rf.Detail)))
	}
}

// (i) 구조 가드: redfish.go·*_test.go 외 소스는 net/http 로 직접 요청하거나 Client 내부를 건드리지 못한다.
var rawHTTPPatterns = []string{
	"http.Get(", "http.Post(", "http.PostForm(", "http.Head(", "http.NewRequest",
	"http.DefaultClient", "http.DefaultTransport", "http.Client{", "http.Transport{",
	"RoundTrip(", "net.Dial", "tls.Dial", "net/http/httputil",
}

// net/http 패키지에서 다른 파일이 쓰면 안 되는 이름 (별칭 import 로 우회하는 경우까지 AST 로 잡는다)
var httpForbidden = map[string]bool{
	"Get": true, "Post": true, "PostForm": true, "Head": true, "NewRequest": true,
	"NewRequestWithContext": true, "DefaultClient": true, "DefaultTransport": true,
	"Client": true, "Transport": true,
}

// Client 의 비공개 관문·상태 (같은 package main 이라 컴파일러가 막지 못하므로 테스트로 막는다)
var clientPrivate = map[string]bool{
	"do": true, "roundTrip": true, "httpc": true, "cred": true, "rfMode": true,
	"sessClean": true, "sessSend": true, "authMode": true,
}

func rawHTTPViolations(name string, src []byte) []string {
	var out []string
	s := string(src)
	for _, p := range rawHTTPPatterns {
		if strings.Contains(s, p) {
			out = append(out, p)
		}
	}
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, name, src, 0)
	if err != nil {
		return append(out, "파싱 실패: "+err.Error())
	}
	httpNames := map[string]bool{}
	for _, im := range f.Imports {
		p, _ := strconv.Unquote(im.Path.Value)
		if p != "net/http" {
			continue
		}
		n := "http"
		if im.Name != nil {
			n = im.Name.Name
		}
		if n == "." {
			out = append(out, "net/http dot import")
		}
		httpNames[n] = true
	}
	ast.Inspect(f, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.SelectorExpr:
			if id, ok := x.X.(*ast.Ident); ok && httpNames[id.Name] && httpForbidden[x.Sel.Name] {
				out = append(out, fmt.Sprintf("%s:%d %s.%s", name, fset.Position(x.Pos()).Line, id.Name, x.Sel.Name))
			}
			if clientPrivate[x.Sel.Name] {
				out = append(out, fmt.Sprintf("%s:%d .%s (Client 내부 접근)", name, fset.Position(x.Pos()).Line, x.Sel.Name))
			}
		case *ast.CompositeLit:
			if id, ok := x.Type.(*ast.Ident); ok && id.Name == "Client" {
				out = append(out, fmt.Sprintf("%s:%d Client{} 직접 생성 (NewClient 사용)", name, fset.Position(x.Pos()).Line))
			}
		}
		return true
	})
	return out
}

func TestNoRawHTTPOutsideRedfish(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil || len(files) == 0 {
		t.Fatalf("소스 목록: %v", err)
	}
	checked := 0
	for _, f := range files {
		if f == "redfish.go" || strings.HasSuffix(f, "_test.go") {
			continue
		}
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		checked++
		if v := rawHTTPViolations(f, src); len(v) > 0 {
			t.Errorf("%s: redfish.go 밖에서 HTTP 직접 사용/Client 내부 접근: %v", f, v)
		}
	}
	if checked == 0 {
		t.Fatal("검사한 파일이 없음")
	}
	// 가드 자체가 동작하는지 (별칭 import, 내부 필드, 직접 생성)
	bad := `package main
import h "net/http"
func f(c *Client) {
	r, _ := h.NewRequest("DELETE", "https://x/redfish/v1/Managers/1/LogServices/Sel", nil)
	_, _ = (&h.Client{}).Do(r)
	c.rfMode = ModeSet
	c.do("POST", "/redfish/v1/Systems/1/Actions/ComputerSystem.Reset", nil)
	_ = c.httpc
	_ = &Client{}
}`
	v := rawHTTPViolations("bad.go", []byte(bad))
	for _, want := range []string{"h.NewRequest", "h.Client", ".rfMode", ".do", ".httpc", "Client{} 직접 생성"} {
		if !strings.Contains(strings.Join(v, "\n"), want) {
			t.Errorf("가드가 %q 를 잡지 못함: %v", want, v)
		}
	}
	if v := rawHTTPViolations("ok.go", []byte("package main\nimport \"net/http\"\nfunc g(h http.Header) string { return h.Get(\"Location\") }\n")); len(v) != 0 {
		t.Errorf("http.Header 사용은 허용되어야 함: %v", v)
	}
}
