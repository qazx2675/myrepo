package ucsmxml

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// 모든 테스트는 httptest 가짜 UCSM 으로만 한다 (실제 UCSM 접속 금지).

const (
	tUser   = "ucsread"
	tPass   = `P@ss<"&'>w0rd` // XML 이스케이프가 필요한 문자를 모두 넣는다
	tCookie = "1700000000/ck-SECRET-1a2b3c"
)

// countSrv 는 받은 요청 본문을 모두 기록하는 가짜 UCSM 입니다.
type countSrv struct {
	mu     sync.Mutex
	bodies []string
	reply  func(body string) string // nil 이면 메서드별 기본 성공 응답
}

func (s *countSrv) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	b, _ := io.ReadAll(r.Body)
	s.mu.Lock()
	s.bodies = append(s.bodies, string(b))
	s.mu.Unlock()
	if s.reply != nil {
		io.WriteString(w, s.reply(string(b)))
		return
	}
	switch {
	case strings.HasPrefix(string(b), "<aaaLogin"):
		fmt.Fprintf(w, `<aaaLogin cookie="" response="yes" outCookie="%s"> </aaaLogin>`, tCookie)
	case strings.HasPrefix(string(b), "<configResolveClass"):
		fmt.Fprintf(w, `<configResolveClass cookie="%s" response="yes" classId="biosVfCPUPerformance"><outConfigs/></configResolveClass>`, tCookie)
	default:
		io.WriteString(w, `<aaaLogout cookie="" response="yes" outStatus="success"/>`)
	}
}

func (s *countSrv) n() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.bodies)
}

func newSrv(t *testing.T, reply func(string) string) (*countSrv, *httptest.Server) {
	t.Helper()
	s := &countSrv{reply: reply}
	ts := httptest.NewTLSServer(s)
	t.Cleanup(ts.Close)
	return s, ts
}

func newC(url string) *Client {
	return New(Options{
		BaseURL: url, User: tUser, Pass: tPass, Insecure: true, Timeout: 5 * time.Second,
		// configConfMo·aaaUser 는 bios* 가 아니라서 허용목록에 들어가지 않아야 한다.
		Classes: []string{"biosVfCPUPerformance", "configConfMo", "aaaUser"},
	})
}

// 허용목록 밖 요청은 gate 에서 거부되고 서버는 아무것도 받지 않는다.
func TestGateRejectsBeforeSend(t *testing.T) {
	s, ts := newSrv(t, nil)
	c := newC(ts.URL)
	c.sec().cookie = tCookie // 로그인한 상태를 흉내 낸다
	bad := []string{
		`<configConfMo cookie="` + tCookie + `" dn="org-root/bios-prof-x" inHierarchical="false"><inConfig><biosVProfile name="x" rebootOnUpdate="yes"/></inConfig></configConfMo>`,
		`<configConfMos cookie="` + tCookie + `" inHierarchical="false"/>`,
		`<configResolveDn cookie="` + tCookie + `" dn="sys/chassis-1/blade-1" inHierarchical="false"/>`,
		`<configResolveChildren cookie="` + tCookie + `" inDn="sys" inHierarchical="false"/>`,
		`<configResolveClass cookie="` + tCookie + `" inHierarchical="false" classId="configConfMo"/>`,
		`<configResolveClass cookie="` + tCookie + `" inHierarchical="false" classId="aaaUser"/>`,
		`<configResolveClass cookie="` + tCookie + `" inHierarchical="false" classId="lsPower"/>`,
		`<configResolveClass cookie="` + tCookie + `" inHierarchical="true" classId="biosVfCPUPerformance"/>`,
		`<configResolveClass cookie="other" inHierarchical="false" classId="biosVfCPUPerformance"/>`,
		`<configResolveClass cookie="` + tCookie + `" inHierarchical="false" classId="biosVfCPUPerformance" dn="x"/>`,
		`<configResolveClass cookie="` + tCookie + `" cookie="` + tCookie + `" inHierarchical="false" classId="biosVfCPUPerformance"/>`,
		`<configResolveClass cookie="` + tCookie + `" inHierarchical="false" classId="biosVfCPUPerformance"><inFilter/></configResolveClass>`,
		`<configResolveClass cookie="` + tCookie + `" inHierarchical="false" classId="biosVfCPUPerformance"/><configConfMo cookie="` + tCookie + `"/>`,
		`<aaaLogout inCookie="other"/>`,
		`<aaaLogin inName="u" inPassword="p" inDomain="x"/>`,
		`<aaaRefresh inName="u" inPassword="p" inCookie="` + tCookie + `"/>`,
		`<?xml version="1.0"?><aaaLogout inCookie="` + tCookie + `"/>`,
		`<!-- x --><aaaLogout inCookie="` + tCookie + `"/>`,
		`<x:aaaLogout xmlns:x="urn:x" inCookie="` + tCookie + `"/>`,
		`text<aaaLogout inCookie="` + tCookie + `"/>`,
		`not xml`,
		``,
	}
	for _, b := range bad {
		if _, err := c.post("test", []byte(b)); !errors.Is(err, ErrNotAllowed) {
			t.Errorf("거부되어야 함: %s → %v", b, err)
		}
	}
	if n := s.n(); n != 0 {
		t.Errorf("거부된 요청인데 서버가 %d건을 받음", n)
	}
	// 거부 메시지에 쿠키 값은 들어가지 않는다
	if _, err := c.post("test", []byte(`<aaaLogout inCookie="`+tCookie+`x"/>`)); err == nil || strings.Contains(err.Error(), tCookie) {
		t.Errorf("거부 메시지에 쿠키 노출 또는 거부 안 됨: %v", err)
	}

	// 공개 API 로도 허용목록 밖 classId 는 보낼 수 없다.
	for _, cls := range []string{"configConfMo", "aaaUser", "biosVfOther", "", "lsPower", "lsServerX", "computeRackUnits"} {
		if _, err := c.ResolveClass(cls); !errors.Is(err, ErrNotAllowed) {
			t.Errorf("ResolveClass(%q) 는 거부되어야 함: %v", cls, err)
		}
	}
	if n := s.n(); n != 0 {
		t.Errorf("서버가 %d건을 받음, 기대 0", n)
	}
}

// 이 패키지가 만드는 세 가지 본문은 (특수문자 비밀번호여도) gate 를 통과한다.
func TestGateAllowsBuiltRequests(t *testing.T) {
	s, ts := newSrv(t, nil)
	c := newC(ts.URL)
	if err := c.Login(); err != nil {
		t.Fatalf("Login: %v", err)
	}
	if _, err := c.ResolveClass(BladeClass); err != nil {
		t.Fatalf("ResolveClass computeBlade: %v", err)
	}
	for _, cls := range []string{RackClass, ServiceProfileClass} { // 고정 허용 (서버·서비스 프로파일 읽기)
		if _, err := c.ResolveClass(cls); err != nil {
			t.Fatalf("ResolveClass %s: %v", cls, err)
		}
	}
	if _, err := c.ResolveClass("biosVfCPUPerformance"); err != nil {
		t.Fatalf("ResolveClass biosVf: %v", err)
	}
	if err := c.Logout(); err != nil {
		t.Fatalf("Logout: %v", err)
	}
	if err := c.Logout(); err != nil || s.n() != 6 {
		t.Errorf("Logout 은 멱등이어야 함 (요청 %d건, 기대 6): %v", s.n(), err)
	}
	s.mu.Lock()
	login := s.bodies[0]
	s.mu.Unlock()
	if strings.Contains(login, tPass) || !strings.Contains(login, "&lt;&#34;&amp;&#39;&gt;") {
		t.Errorf("aaaLogin 본문의 비밀번호가 XML 이스케이프되지 않음")
	}
	// 로그아웃 뒤에는 다시 조회하지 않는다
	if _, err := c.ResolveClass(BladeClass); err == nil {
		t.Error("로그아웃 뒤 조회가 성공하면 안 됨")
	}
}

// 로그인 거부는 AUTH_FAIL, 1회만 보내고, 되돌아온 비밀번호는 오류에 없다.
func TestLoginAuthFailOnceMasked(t *testing.T) {
	s, ts := newSrv(t, func(string) string {
		return fmt.Sprintf(`<aaaLogin cookie="" response="yes" errorCode="551" errorDescr="Authentication failed for %s / %s"/>`,
			esc(tPass), escQuot(tPass))
	})
	c := newC(ts.URL)
	err := c.Login()
	var e *Error
	if !errors.As(err, &e) || e.Status != StatusAuthFail || e.Code != "551" {
		t.Fatalf("AUTH_FAIL(551) 기대: %v", err)
	}
	for _, sec := range []string{tPass, esc(tPass), escQuot(tPass)} {
		if strings.Contains(err.Error(), sec) || strings.Contains(fmt.Sprintf("%+v %#v", c, c), sec) {
			t.Errorf("오류/Client 출력에 비밀번호 노출: %s", err)
		}
	}
	_ = c.Login()
	if _, err := c.ResolveClass(BladeClass); err == nil {
		t.Error("로그인 실패 뒤 조회가 성공하면 안 됨")
	}
	if err := c.Logout(); err != nil {
		t.Errorf("쿠키가 없으면 Logout 은 아무것도 보내지 않아야 함: %v", err)
	}
	if n := s.n(); n != 1 {
		t.Errorf("aaaLogin 은 1회만 보내야 함 (실제 요청 %d건)", n)
	}
}

// 응답이 XML 이 아니거나 루트가 다르면 XML_ERROR, HTTP 401 로그인은 AUTH_FAIL, 평문 http 는 거부.
func TestResponseClassification(t *testing.T) {
	_, ts := newSrv(t, func(string) string { return "<html><body>maintenance</body></html>" })
	if err := newC(ts.URL).Login(); !isStatus(err, StatusXMLError) {
		t.Errorf("HTML 응답은 XML_ERROR: %v", err)
	}
	_, ts2 := newSrv(t, func(string) string { return "plain text" })
	if err := newC(ts2.URL).Login(); !isStatus(err, StatusXMLError) {
		t.Errorf("텍스트 응답은 XML_ERROR: %v", err)
	}
	ts3 := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusUnauthorized) }))
	defer ts3.Close()
	if err := newC(ts3.URL).Login(); !isStatus(err, StatusAuthFail) {
		t.Errorf("HTTP 401 로그인은 AUTH_FAIL: %v", err)
	}
	if err := New(Options{BaseURL: "http://192.0.2.1", User: tUser, Pass: tPass}).Login(); err == nil || !strings.Contains(err.Error(), "https") {
		t.Errorf("평문 http 는 거부: %v", err)
	}
	// 클래스 오류(errorCode) 는 Code 가 채워진 XML_ERROR 이고 쿠키는 Detail 에 없다
	_, ts4 := newSrv(t, func(b string) string {
		if strings.HasPrefix(b, "<aaaLogin") {
			return fmt.Sprintf(`<aaaLogin response="yes" outCookie="%s"/>`, tCookie)
		}
		return fmt.Sprintf(`<configResolveClass cookie="%s" response="yes" errorCode="103" errorDescr="no class (%s)"/>`, tCookie, tCookie)
	})
	c := newC(ts4.URL)
	if err := c.Login(); err != nil {
		t.Fatal(err)
	}
	_, err := c.ResolveClass("biosVfCPUPerformance")
	var e *Error
	if !errors.As(err, &e) || e.Status != StatusXMLError || e.Code != "103" || strings.Contains(e.Error(), tCookie) {
		t.Errorf("클래스 오류 분류/쿠키 마스킹: %v", err)
	}
}

func isStatus(err error, st string) bool {
	var e *Error
	return errors.As(err, &e) && e.Status == st
}
