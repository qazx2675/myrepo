package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
	"unicode"
)

// 보안 리뷰 지적 F1~F9 회귀 테스트. 모두 httptest mock 서버로만 한다 (실제 BMC 접속 금지).

const (
	secBios = "/redfish/v1/Systems/1/Bios/Settings"
	secJob  = "/redfish/v1/Managers/iDRAC.Embedded.1/Jobs"
)

// F1: PATCH Attributes 내부 검증 -----------------------------------------------------------

func TestSecAttributesValidation(t *testing.T) {
	cases := []struct {
		name, body string
		ok         bool
	}{
		// 정상 표준 속성은 위험 속성명 규칙에 걸리지 않는다.
		{"SysProfile", `{"Attributes":{"SysProfile":"PerfOptimized"}}`, true},
		{"LogicalProc", `{"Attributes":{"LogicalProc":"Enabled"}}`, true},
		{"ProcHyperthreading", `{"Attributes":{"ProcHyperthreading":"Enabled"}}`, true},
		{"LlcPrefetch", `{"Attributes":{"LlcPrefetch":"Disabled"}}`, true},
		{"SubNumaCluster", `{"Attributes":{"SubNumaCluster":"Enabled"}}`, true},
		{"SubNumaClustering", `{"Attributes":{"SubNumaClustering":"Enable"}}`, true},
		{"WorkloadProfile", `{"Attributes":{"WorkloadProfile":"HPC"}}`, true},
		{"여러 속성", `{"Attributes":{"SysProfile":"Custom","LogicalProc":"Disabled","LlcPrefetch":"Enabled"}}`, true},
		{"숫자 값", `{"Attributes":{"SomeLimit":5}}`, true},
		{"불리언 값", `{"Attributes":{"SomeFlag":true}}`, true},

		// 키가 @ 로 시작 (ApplyTime 우회 포함)
		{"@ 키", `{"Attributes":{"@Redfish.SettingsApplyTime":{"ApplyTime":"Immediate"}}}`, false},
		{"@ 키 + 정상", `{"Attributes":{"A":"B","@odata.id":"x"}}`, false},
		{"빈 키", `{"Attributes":{"":"x"}}`, false},

		// 값이 객체/배열/null/빈값
		{"객체 값", `{"Attributes":{"A":{"nested":[1,2]}}}`, false},
		{"배열 값", `{"Attributes":{"A":["x"]}}`, false},
		{"null 값", `{"Attributes":{"A":null}}`, false},
		{"빈 문자열", `{"Attributes":{"A":""}}`, false},
		{"공백 문자열", `{"Attributes":{"A":"  "}}`, false},
		{"빈 Attributes", `{"Attributes":{}}`, false},

		// 위험 속성명
		{"SetupPassword", `{"Attributes":{"SetupPassword":"x"}}`, false},
		{"SysPassword", `{"Attributes":{"SysPassword":"y"}}`, false},
		{"AdminPasswd", `{"Attributes":{"AdminPasswd":"y"}}`, false},
		{"Pwd", `{"Attributes":{"BmcPwd":"y"}}`, false},
		{"RestoreDefaults", `{"Attributes":{"RestoreDefaults":"Yes"}}`, false},
		{"RestoreManufacturingDefaults", `{"Attributes":{"RestoreManufacturingDefaults":"Yes"}}`, false},
		{"FactoryReset", `{"Attributes":{"FactoryReset":"Yes"}}`, false},
		{"ResetBios", `{"Attributes":{"ResetBios":"Yes"}}`, false},
		{"ResetCmos", `{"Attributes":{"ResetCmos":"Yes"}}`, false},
		{"ClearCmos", `{"Attributes":{"ClearCmos":"Yes"}}`, false},
		{"BootOrder", `{"Attributes":{"BootOrder":"NIC"}}`, false},
		{"SetBootOrderEn", `{"Attributes":{"SetBootOrderEn":"NIC.PxeDevice.1-1"}}`, false},
		{"BootSeq", `{"Attributes":{"BootSeq":"x"}}`, false},
		{"BootSequence", `{"Attributes":{"BootSequence":"x"}}`, false},
		{"Clear", `{"Attributes":{"ClearMemory":"Yes"}}`, false},
		{"LoadDefaults", `{"Attributes":{"LoadDefaults":"Yes"}}`, false},
		{"Default", `{"Attributes":{"Default":"Yes"}}`, false},
		{"대소문자 무시", `{"Attributes":{"SETUPPASSWORD":"x"}}`, false},
		{"정상 속성 옆의 위험 속성", `{"Attributes":{"LogicalProc":"Enabled","RestoreDefaults":"Yes"}}`, false},
	}
	for _, tc := range cases {
		_, err := checkBody("PATCH", secBios, []byte(tc.body))
		if (err == nil) != tc.ok {
			t.Errorf("%s: ok=%v 기대 %v (err=%v) body=%s", tc.name, err == nil, tc.ok, err, tc.body)
		}
	}

	// 정상 본문은 값 형식(숫자·불리언)을 그대로 보존한다.
	out, err := checkBody("PATCH", secBios, []byte(`{"Attributes":{"N":5,"F":true,"S":"x"}}`))
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]map[string]interface{}
	if err := json.Unmarshal(out, &got); err != nil || got["Attributes"]["N"] != float64(5) || got["Attributes"]["F"] != true || got["Attributes"]["S"] != "x" {
		t.Errorf("값 형식이 바뀜: %s (%v)", out, err)
	}
}

// 위험 Attributes 는 클라이언트 경로에서도 서버에 도달하지 않는다 (로그인 POST 도 없음).
func TestSecAttributesNotSent(t *testing.T) {
	m := newMock(t, nil)
	c, _ := newTestClient(m, ModeSet, 0)
	for _, attrs := range []map[string]interface{}{
		{"SetupPassword": "x"},
		{"RestoreDefaults": "Yes"},
		{"@Redfish.SettingsApplyTime": map[string]string{"ApplyTime": "Immediate"}},
		{"A": map[string]string{"x": "y"}},
		{"A": nil},
	} {
		_, _, err := c.Patch(secBios, map[string]interface{}{"Attributes": attrs})
		if !errors.Is(err, ErrNotAllowed) {
			t.Errorf("%v: ErrNotAllowed 여야 함: %v", attrs, err)
		}
	}
	if s := m.snapshot(); s.total != 0 {
		t.Errorf("거부된 PATCH 가 서버에 도달: %v", s.reqs)
	}
}

// F2: Job TargetSettingsURI 는 정규형과 원문이 같을 때만 -------------------------------------

func TestSecJobTargetCanonicalOnly(t *testing.T) {
	const good = "/redfish/v1/Systems/System.Embedded.1/Bios/Settings"
	bad := []string{
		"/redfish/v1/Managers/iDRAC.Embedded.1/Attributes/../../../Systems/1/Bios/Settings",
		"/redfish/v1/Systems/1/Bios/Settings#/../../../../Managers/iDRAC.Embedded.1/Attributes",
		"/redfish/v1/Systems/1/Bios/Settings?$x=/redfish/v1/Managers/1/Attributes",
		"/redfish/v1/Systems/1/Bios/%53ettings",
		"/redfish/v1/Systems/1/Bios/Settings/", // 끝 슬래시는 정규형이 아님
		"/redfish/v1/Systems/1/../1/Bios/Settings",
		"/redfish/v1//Systems/1/Bios/Settings",
		"/redfish/v1/Systems/1/Bios/Settings/.",
	}
	for _, tgt := range bad {
		b, _ := json.Marshal(map[string]string{"TargetSettingsURI": tgt})
		if _, err := checkBody("POST", secJob, b); err == nil {
			t.Errorf("정규형이 아닌 TargetSettingsURI 가 허용됨: %q", tgt)
		}
	}
	b, _ := json.Marshal(map[string]string{"TargetSettingsURI": good})
	out, err := checkBody("POST", secJob, b)
	if err != nil || !strings.Contains(string(out), good) {
		t.Errorf("정규형은 허용되어야 함: %v %s", err, out)
	}

	// 클라이언트 경로: 거부는 서버에 도달하지 않고, 허용은 정규형 그대로 전송된다.
	m := newMock(t, nil)
	c, _ := newTestClient(m, ModeSet, 0)
	_, _, _, err = c.PostJob(secJob, map[string]string{"TargetSettingsURI": bad[0]})
	if !errors.Is(err, ErrNotAllowed) || m.snapshot().total != 0 {
		t.Errorf("비정규형 Job: err=%v reqs=%v", err, m.snapshot().reqs)
	}
	if _, _, _, err := c.PostJob(secJob, map[string]string{"TargetSettingsURI": good}); err != nil {
		t.Fatal(err)
	}
	if s := m.snapshot(); len(s.bodies) != 1 || s.bodies[0] != `{"TargetSettingsURI":"`+good+`"}` {
		t.Errorf("전송 본문: %q", s.bodies)
	}
}

// F3: GET 이 AUTH_FAIL 을 받은 뒤에도 Close 는 자기 세션을 DELETE 한다 ------------------------

func TestSecSessionDeletedAfterAuthFail(t *testing.T) {
	m := newMock(t, func(m *mockBMC) { m.getStatus = http.StatusUnauthorized })
	c, _ := newTestClient(m, ModeReadOnly, 3)
	if _, err := c.GetRaw("/redfish/v1/Systems"); rfStatus(t, err) != StatusAuthFail {
		t.Fatalf("GET 401: %v", err)
	}
	// AUTH_FAIL 후 다른 GET 은 전송하지 않는다 (기존 동작 유지)
	for i := 0; i < 3; i++ {
		if _, err := c.GetRaw("/redfish/v1/Systems"); rfStatus(t, err) != StatusAuthFail {
			t.Errorf("#%d: %v", i, err)
		}
	}
	if s := m.snapshot(); s.total != 2 { // 세션 POST 1 + GET 1
		t.Errorf("AUTH_FAIL 후 요청이 더 나감: %v", s.reqs)
	}
	if err := c.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
	s := m.snapshot()
	if s.sessCreate != 1 || s.sessDelete != 1 || s.total != 3 {
		t.Errorf("세션생성=%d 삭제=%d 전체=%d reqs=%v (기대 1/1/3)", s.sessCreate, s.sessDelete, s.total, s.reqs)
	}
	if st := c.Stats(); st.Sessions != 1 || st.Deletes != 1 {
		t.Errorf("Stats=%+v", st)
	}
}

// 세션 DELETE 의 401 은 loginErr 에 기록되지 않는다.
func TestSecDeleteAuthFailNotRecorded(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost:
			w.Header().Set("X-Auth-Token", "tok")
			w.Header().Set("Location", testSessPath)
			w.WriteHeader(http.StatusCreated)
		case http.MethodDelete:
			w.WriteHeader(http.StatusUnauthorized)
		default:
			io.WriteString(w, "{}")
		}
	}))
	defer srv.Close()
	c := NewClient(ClientOpts{BaseURL: srv.URL, User: "u", Pass: "p", Insecure: true, Gap: -1})
	if _, err := c.GetRaw("/redfish/v1"); err != nil {
		t.Fatal(err)
	}
	if err := c.Close(); rfStatus(t, err) != StatusAuthFail {
		t.Errorf("DELETE 401 은 Close 오류로 보고: %v", err)
	}
	if c.loginErr != nil {
		t.Errorf("DELETE 401 이 loginErr 에 기록됨: %v", c.loginErr)
	}
}

// F4: Detail 의 제어문자·비출력 문자 치환 ----------------------------------------------------

func TestSecDetailControlChars(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			w.WriteHeader(http.StatusNotFound) // Basic 폴백
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
		io.WriteString(w, "\x1b[2J\x1b]0;pwned\x07\x1b[31mFAKE OK\x1b[0m\x00\x08\x08\ttab\r\nline​zw")
	}))
	defer srv.Close()
	c := NewClient(ClientOpts{BaseURL: srv.URL, User: "u", Pass: "p", Insecure: true, Gap: -1})
	_, err := c.GetRaw("/redfish/v1")
	var rf *RFError
	if !errors.As(err, &rf) {
		t.Fatalf("RFError 아님: %v", err)
	}
	for _, r := range rf.Detail {
		if r != ' ' && !unicode.IsPrint(r) {
			t.Errorf("Detail 에 비출력 문자 %U 가 남음: %q", r, rf.Detail)
		}
	}
	if strings.ContainsAny(err.Error(), "\x1b\x07\x00\x08\r\n\t") {
		t.Errorf("오류 문자열에 제어문자: %q", err.Error())
	}
	if !strings.Contains(rf.Detail, "FAKE OK") || !strings.Contains(rf.Detail, "?") {
		t.Errorf("치환 결과가 이상함: %q", rf.Detail)
	}
}

func TestSecSanitizeDetail(t *testing.T) {
	cases := []struct{ in, want string }{
		{"a\tb\nc\r\nd", "a b c d"},
		{"  x   y  ", "x y"},
		{"\x1b[31mred\x1b[0m", "?[31mred?[0m"},
		{"bel\x07nul\x00", "bel?nul?"},
		{"한글 정상 텍스트", "한글 정상 텍스트"},
		{"zero​width", "zero?width"},
		{"del\x7fchar", "del?char"},
	}
	for _, tc := range cases {
		if got := sanitizeDetail(tc.in); got != tc.want {
			t.Errorf("sanitizeDetail(%q) = %q, 기대 %q", tc.in, got, tc.want)
		}
	}
}

// F5: 로그인 실패 Detail 에는 응답 본문 없음 / 비밀번호의 JSON 이스케이프 변형 마스킹 ------------

func TestSecLoginFailureDetailHasNoBody(t *testing.T) {
	for _, code := range []int{400, 401, 403, 500, 503} {
		srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(code)
			io.WriteString(w, "LEAK-BODY-123 "+testPass)
		}))
		c := NewClient(ClientOpts{BaseURL: srv.URL, User: testUser, Pass: testPass, Insecure: true, Gap: -1})
		err := c.Login()
		srv.Close()
		var rf *RFError
		if !errors.As(err, &rf) {
			t.Errorf("%d: RFError 아님: %v", code, err)
			continue
		}
		if strings.Contains(err.Error(), "LEAK-BODY") || strings.Contains(err.Error(), "S3cr3t") {
			t.Errorf("%d: 로그인 실패 오류에 본문이 실림: %s", code, err)
		}
		if rf.HTTP != code || !strings.Contains(rf.Detail, "HTTP "+strconv.Itoa(code)) {
			t.Errorf("%d: 고정 문구 + HTTP 코드여야 함: %+v", code, rf)
		}
	}
}

func TestSecRedactPasswordForms(t *testing.T) {
	type pwCase struct {
		pw    string
		forms []string // BMC 가 되돌릴 수 있는 형태 (독립적으로 손으로 적음)
	}
	u := func(hex ...string) string { // JSON \uXXXX 이스케이프 (소스에 직접 쓰면 편집 도구가 풀어 버려 조립한다)
		var b strings.Builder
		for _, h := range hex {
			b.WriteString(string(rune(92)) + "u" + h)
		}
		return b.String()
	}
	cases := []pwCase{
		{`ab<"cd`, []string{`ab<"cd`, `ab<\"cd`, "ab" + u("003c") + `\"cd`, "ab" + u("003C") + `\"cd`}},
		{`a/b/c`, []string{`a/b/c`, `a\/b\/c`}},
		{`한글pw`, []string{`한글pw`, u("d55c", "ae00") + "pw", u("D55C", "AE00") + "pw"}},
		{`x😀y/z"<&`, []string{
			`x😀y/z"<&`, `x😀y/z\"<&`,
			"x" + u("d83d", "de00") + `y/z\"<&`, "x" + u("D83D", "DE00") + `y\/z\"<&`,
			`x😀y\/z\"<&`, `x😀y\/z\"` + u("003c", "0026"),
		}},
	}
	for _, tc := range cases {
		// 표준 라이브러리가 만드는 두 가지 JSON 형태도 함께 검사
		forms := append([]string(nil), tc.forms...)
		jp, _ := json.Marshal(tc.pw)
		forms = append(forms, strings.Trim(string(jp), `"`))
		var buf bytes.Buffer
		enc := json.NewEncoder(&buf)
		enc.SetEscapeHTML(false)
		_ = enc.Encode(tc.pw)
		forms = append(forms, strings.TrimSuffix(strings.TrimSuffix(buf.String(), "\n"), `"`)[1:])

		c := NewClient(ClientOpts{BaseURL: "https://x", User: "u", Pass: tc.pw})
		for _, f := range forms {
			e := c.rfErr(StatusBMCError, 500, `{"error":"bad password `+f+` here"}`)
			if strings.Contains(e.Detail, f) {
				t.Errorf("pw=%q: 형태 %q 가 지워지지 않음: %q", tc.pw, f, e.Detail)
			}
			if !strings.Contains(e.Detail, "***") {
				t.Errorf("pw=%q: 형태 %q 에 마스킹 표시가 없음: %q", tc.pw, f, e.Detail)
			}
		}
	}

	// 끝에 따옴표가 있는 비밀번호도 JSON 형태가 온전히 지워진다
	c := NewClient(ClientOpts{BaseURL: "https://x", User: "u", Pass: `pw"`})
	if e := c.rfErr(StatusBMCError, 500, `{"p":"pw\""}`); strings.Contains(e.Detail, `pw\`) {
		t.Errorf("끝 따옴표 비밀번호: %q", e.Detail)
	}

	// 끝-끝: 서버가 SetEscapeHTML(false) 형태로 되돌려도 GET 오류 Detail 에 없다.
	pw := `p<w>&"/q`
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(pw)
	echo := strings.TrimSuffix(buf.String(), "\n")
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
		io.WriteString(w, `{"echo":`+echo+`}`)
	}))
	defer srv.Close()
	cl := NewClient(ClientOpts{BaseURL: srv.URL, User: "u", Pass: pw, Insecure: true, Gap: -1})
	_, err := cl.GetRaw("/redfish/v1")
	if err == nil || strings.Contains(err.Error(), `p<w>&\"\/q`) || strings.Contains(err.Error(), `p<w>&\"/q`) || strings.Contains(err.Error(), "p<w>") {
		t.Errorf("GET 5xx Detail 에 비밀번호 JSON 형태 노출: %v", err)
	}
}

// F6: GET 거부 세그먼트 --------------------------------------------------------------------

func TestSecGetDenySegments(t *testing.T) {
	deny := []string{
		// 끝의 . 과 ~ 를 붙여 우회
		"/redfish/v1/Managers/1/LogServices.",
		"/redfish/v1/Managers/1/LogServices./Sel/Entries",
		"/redfish/v1/Managers/1/LogServices~",
		"/redfish/v1/Managers/1/LogServices..",
		"/redfish/v1/Managers/1/LogServices%2E",
		"/redfish/v1/Managers/1/LogServices%7E",
		"/redfish/v1/Managers/1/Sel./Entries",
		"/redfish/v1/Systems/1/Actions.",
		// 새로 추가된 세그먼트
		"/redfish/v1/EventService/SSE",
		"/redfish/v1/EventService/sse",
		"/redfish/v1/EventService/SSE.",
		"/redfish/v1/Managers/1/LogEntries",
		"/redfish/v1/Systems/1/LogEntries~",
		// log 접미 규칙은 유지
		"/redfish/v1/UpdateService/AuditLog",
		"/redfish/v1/Managers/1/Logs",
		"/redfish/v1/Managers/1/Logs.",
		"/redfish/v1/Managers/iDRAC.Embedded.1/Logs/Lclog",
		"/redfish/v1/Systems/1/LogServices/EventLog/Entries/1",
	}
	for _, p := range deny {
		if _, err := checkAllowed("GET", p, ModeReadOnly, ""); !errors.Is(err, ErrNotAllowed) {
			t.Errorf("GET %q: 거부여야 함 (err=%v)", p, err)
		}
	}
	allow := []string{
		"/redfish/v1/UpdateService/Catalog", // "log" 접미사지만 로그가 아님
		"/redfish/v1/UpdateService/catalog",
		"/redfish/v1/AccountService/Accounts",
		"/redfish/v1/Systems/1/Bios",
		"/redfish/v1/Systems/System.Embedded.1/Bios/Settings",
		"/redfish/v1/Registries/BiosAttributeRegistry.v1_0_0",
		"/redfish/v1/EventService",
		"/redfish/v1/EventService/Subscriptions",
	}
	for _, p := range allow {
		if _, err := checkAllowed("GET", p, ModeReadOnly, ""); err != nil {
			t.Errorf("GET %q: 허용이어야 함: %v", p, err)
		}
	}

	m := newMock(t, nil)
	c, _ := newTestClient(m, ModeReadOnly, 0)
	for _, p := range deny {
		if _, err := c.GetRaw(p); !errors.Is(err, ErrNotAllowed) {
			t.Errorf("GetRaw %q: %v", p, err)
		}
	}
	if s := m.snapshot(); s.total != 0 {
		t.Errorf("거부된 GET 이 서버에 도달: %v", s.reqs)
	}
}

// F7: Retries 상한, 백오프 오버플로 없음 ----------------------------------------------------

func TestSecRetriesCapAndBackoff(t *testing.T) {
	if c := NewClient(ClientOpts{BaseURL: "https://x", Retries: 1000}); c.retries != 5 {
		t.Errorf("Retries 1000 → %d (기대 상한 5)", c.retries)
	}
	if c := NewClient(ClientOpts{BaseURL: "https://x", Retries: 3}); c.retries != 3 {
		t.Errorf("Retries 3 → %d", c.retries)
	}
	if c := NewClient(ClientOpts{BaseURL: "https://x", Retries: -4}); c.retries != 0 {
		t.Errorf("Retries -4 → %d", c.retries)
	}

	want := map[int]time.Duration{
		0: 300 * time.Millisecond, 1: 600 * time.Millisecond, 2: 1200 * time.Millisecond,
		3: 2400 * time.Millisecond, 4: 4800 * time.Millisecond, 5: 5 * time.Second,
		10: 5 * time.Second, 34: 5 * time.Second, 35: 5 * time.Second, 36: 5 * time.Second,
		40: 5 * time.Second, 64: 5 * time.Second, 1000: 5 * time.Second,
	}
	for i, d := range want {
		if got := backoffFor(i); got != d {
			t.Errorf("backoffFor(%d) = %v, 기대 %v", i, got, d)
		}
	}

	// 끝-끝: 큰 Retries 도 6번 시도(재시도 5), 모든 대기가 0 < d <= 5s
	m := newMock(t, func(m *mockBMC) { m.getStatus = http.StatusServiceUnavailable })
	c, sr := newTestClient(m, ModeReadOnly, 1000)
	if _, err := c.GetRaw("/redfish/v1/Systems"); rfStatus(t, err) != StatusBMCError {
		t.Fatalf("GET 5xx: %v", err)
	}
	if s := m.snapshot(); s.gets != 6 {
		t.Errorf("GET 시도 %d (기대 6)", s.gets)
	}
	got := sr.get()
	if len(got) != 5 {
		t.Errorf("백오프 %d회 (기대 5): %v", len(got), got)
	}
	for _, d := range got {
		if d <= 0 || d > 5*time.Second {
			t.Errorf("백오프 %v 는 (0, 5s] 이어야 함", d)
		}
	}
}

// F8: Transport 한도, Close 가 유휴 연결을 닫는다 --------------------------------------------

func TestSecTransportLimits(t *testing.T) {
	c := NewClient(ClientOpts{BaseURL: "https://x"})
	tr, ok := c.httpc.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("Transport 타입: %T", c.httpc.Transport)
	}
	if tr.IdleConnTimeout != 30*time.Second {
		t.Errorf("IdleConnTimeout = %v", tr.IdleConnTimeout)
	}
	if tr.MaxResponseHeaderBytes != 64<<10 {
		t.Errorf("MaxResponseHeaderBytes = %d", tr.MaxResponseHeaderBytes)
	}
}

func TestSecResponseHeaderLimit(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("X-Big", strings.Repeat("a", 128<<10))
		io.WriteString(w, "{}")
	}))
	defer srv.Close()
	c := NewClient(ClientOpts{BaseURL: srv.URL, User: "u", Pass: "p", Insecure: true, Gap: -1})
	if _, err := c.GetRaw("/redfish/v1"); err == nil {
		t.Errorf("64KB 를 넘는 응답 헤더는 오류여야 함")
	}
}

func TestSecCloseClosesIdleConnections(t *testing.T) {
	var idle, closed int32
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		io.WriteString(w, "{}")
	}))
	srv.Config.ConnState = connStateCounter(&idle, &closed)
	srv.StartTLS()
	defer srv.Close()

	c := NewClient(ClientOpts{BaseURL: srv.URL, User: "u", Pass: "p", Insecure: true, Gap: -1})
	if _, err := c.GetRaw("/redfish/v1"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { return atomic.LoadInt32(&idle) >= 1 }, "유휴 연결 생성")
	if atomic.LoadInt32(&closed) != 0 {
		t.Fatalf("Close 전에 연결이 닫힘")
	}
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { return atomic.LoadInt32(&closed) >= 1 }, "Close 후 유휴 연결 닫힘")
}

func connStateCounter(idle, closed *int32) func(net.Conn, http.ConnState) {
	return func(_ net.Conn, s http.ConnState) {
		switch s {
		case http.StateIdle:
			atomic.AddInt32(idle, 1)
		case http.StateClosed:
			atomic.AddInt32(closed, 1)
		}
	}
}

func waitFor(t *testing.T, cond func() bool, what string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("시간 초과: %s", what)
}

// F9: AuthMode 비공개화 ------------------------------------------------------------------

func TestSecAuthModePrivate(t *testing.T) {
	// Client 는 공개 필드를 갖지 않는다 (같은 package 의 다른 코드가 인증 방식을 바꾸지 못함)
	rt := reflect.TypeOf(Client{})
	for i := 0; i < rt.NumField(); i++ {
		if f := rt.Field(i); f.PkgPath == "" {
			t.Errorf("Client 에 공개 필드 %q 가 있음", f.Name)
		}
	}
	if _, ok := rt.FieldByName("AuthMode"); ok {
		t.Errorf("Client.AuthMode 가 남아 있음")
	}

	// 구조 가드가 authMode 접근(Basic 다운그레이드 시도)을 잡는다
	src := "package main\nfunc f(c *Client) { c.authMode = \"basic\" }\n"
	if v := rawHTTPViolations("x.go", []byte(src)); !strings.Contains(strings.Join(v, "\n"), ".authMode") {
		t.Errorf("가드가 authMode 접근을 잡지 못함: %v", v)
	}

	// Stats().AuthMode 로만 노출된다
	m := newMock(t, nil)
	c, _ := newTestClient(m, ModeReadOnly, 0)
	if c.Stats().AuthMode != "" {
		t.Errorf("로그인 전 AuthMode = %q", c.Stats().AuthMode)
	}
	if err := c.Login(); err != nil {
		t.Fatal(err)
	}
	if got := c.Stats().AuthMode; got != "session" {
		t.Errorf("세션 로그인 후 AuthMode = %q", got)
	}
	_ = c.Close()
	m2 := newMock(t, func(m *mockBMC) { m.sessionStatus = 404 })
	c2, _ := newTestClient(m2, ModeReadOnly, 0)
	_ = c2.Login()
	if got := c2.Stats().AuthMode; got != "basic" {
		t.Errorf("Basic 폴백 후 AuthMode = %q", got)
	}
}
