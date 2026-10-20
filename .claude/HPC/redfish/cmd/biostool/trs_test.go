package main

import (
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// trsServer 는 시나리오별 가짜 BMC 입니다. basic/login/data 는 각 요청에 줄 상태 코드와 본문을 정합니다.
type trsServer struct {
	rootStatus      int
	basicStatus     int
	loginStatus     int
	loginBody       string
	basicBody       string
	denyPath        string // 인증된 요청에서 403 을 줄 경로
	denyUnlessU     string // 이 User-Agent 로 오면 허용
	sessNeedCT      string // 세션 생성 Content-Type 이 이것이 아니면 415
	noAcceptP       string // 이 경로는 Accept 헤더가 있으면 406
	status405P      string // 이 경로는 항상 405 (Allow: PATCH)
	redirectNoSlash bool   // 슬래시 없는 /redfish/v1 은 301
	calls           []string
}

func (s *trsServer) handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.calls = append(s.calls, r.Method+" "+r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		p := strings.TrimRight(r.URL.Path, "/")
		if s.redirectNoSlash && r.URL.Path == "/redfish/v1" {
			w.Header().Set("Location", "/redfish/v1/")
			w.WriteHeader(301)
			return
		}
		_, _, hasBasic := r.BasicAuth()
		tok := r.Header.Get("X-Auth-Token") == "tok-1"
		switch {
		case r.Method == http.MethodPost && p == "/redfish/v1/SessionService/Sessions":
			if s.sessNeedCT != "" && r.Header.Get("Content-Type") != s.sessNeedCT {
				w.WriteHeader(415)
				return
			}
			if s.loginStatus != 201 {
				w.WriteHeader(s.loginStatus)
				_, _ = w.Write([]byte(s.loginBody))
				return
			}
			w.Header().Set("X-Auth-Token", "tok-1")
			w.Header().Set("Location", "/redfish/v1/SessionService/Sessions/7")
			w.WriteHeader(201)
			_, _ = w.Write([]byte(`{}`))
		case r.Method == http.MethodDelete:
			w.WriteHeader(204)
		case p == "/redfish/v1":
			if s.rootStatus != 200 && !hasBasic && !tok {
				w.WriteHeader(s.rootStatus)
				return
			}
			_, _ = w.Write([]byte(`{"Vendor":"Lenovo","Product":"XCC","RedfishVersion":"1.15.0"}`))
		default:
			if hasBasic && s.basicStatus != 200 {
				w.WriteHeader(s.basicStatus)
				_, _ = w.Write([]byte(s.basicBody))
				return
			}
			if !hasBasic && !tok {
				w.WriteHeader(401)
				return
			}
			if s.noAcceptP != "" && p == s.noAcceptP && r.Header.Get("Accept") != "" {
				w.WriteHeader(406)
				return
			}
			if s.status405P != "" && p == s.status405P {
				w.Header().Set("Allow", "PATCH")
				w.WriteHeader(405)
				return
			}
			if p == s.denyPath && r.Header.Get("User-Agent") != s.denyUnlessU {
				w.WriteHeader(403)
				_, _ = w.Write([]byte(`{"error":{"code":"Base.1.8.InsufficientPrivilege","message":"no","@Message.ExtendedInfo":[{"MessageId":"Base.1.8.InsufficientPrivilege","Message":"denied"}]}}`))
				return
			}
			switch p {
			case "/redfish/v1/Systems":
				_, _ = w.Write([]byte(`{"Members":[{"@odata.id":"/redfish/v1/Systems/1"}]}`))
			case "/redfish/v1/Systems/1":
				_, _ = w.Write([]byte(`{"Manufacturer":"Lenovo","Model":"SR630 V2","BiosVersion":"x"}`))
			default:
				_, _ = w.Write([]byte(`{}`))
			}
		}
	})
}

func trsTarget(t *testing.T, srv *httptest.Server) Target {
	t.Helper()
	host, port, err := net.SplitHostPort(strings.TrimPrefix(srv.URL, "https://"))
	if err != nil {
		t.Fatal(err)
	}
	return Target{Input: host, Hostname: "bmc1", IP: host, Port: port}
}

func trsRun(t *testing.T, s *trsServer) (string, trsFacts) {
	t.Helper()
	srv := httptest.NewTLSServer(s.handler())
	defer srv.Close()
	conf := &Config{User: "admin", Insecure: true, Timeout: 5 * time.Second}
	return trsRunHost(trsTarget(t, srv), conf, "S3cretPw!", nil, nil)
}

func TestTrsPasswordChangeRequired(t *testing.T) {
	body := `{"error":{"code":"Base.1.8.GeneralError","message":"x","@Message.ExtendedInfo":[{"MessageId":"Base.1.8.PasswordChangeRequired","Message":"change it"}]}}`
	s := &trsServer{rootStatus: 200, basicStatus: 403, basicBody: body, loginStatus: 403, loginBody: body}
	rep, f := trsRun(t, s)
	if f.tokenOK || f.basicOK {
		t.Fatal("인증이 통과로 기록됨")
	}
	if !strings.Contains(rep, "비밀번호 변경 필요") {
		t.Errorf("진단에 비밀번호 변경 필요가 없음:\n%s", rep)
	}
	if strings.Contains(rep, "S3cretPw!") {
		t.Error("리포트에 비밀번호가 있음")
	}
	for _, c := range s.calls {
		if strings.HasPrefix(c, "DELETE") || strings.HasPrefix(c, "PATCH") {
			t.Errorf("보내면 안 되는 호출: %s", c)
		}
	}
}

func TestTrsBasicDisabledSessionOK(t *testing.T) {
	s := &trsServer{rootStatus: 200, basicStatus: 403, loginStatus: 201}
	rep, f := trsRun(t, s)
	if !f.tokenOK || !f.basicDisabledHint {
		t.Fatalf("토큰 인증/Basic 비활성 판정 실패: %+v", f)
	}
	if !strings.Contains(rep, "Basic 인증만 403") {
		t.Errorf("진단 누락:\n%s", rep)
	}
	last := s.calls[len(s.calls)-1]
	if last != "DELETE /redfish/v1/SessionService/Sessions/7" {
		t.Errorf("마지막 호출이 세션 삭제가 아님: %s", last)
	}
}

func TestTrsUnauthorizedStopsAfterBasic(t *testing.T) {
	s := &trsServer{rootStatus: 200, basicStatus: 401, loginStatus: 201}
	_, f := trsRun(t, s)
	if !f.halted {
		t.Fatal("401 인데 중단하지 않음")
	}
	for _, c := range s.calls {
		if strings.HasPrefix(c, "POST") {
			t.Error("401 뒤에 세션 생성을 시도함 (잠금 위험)")
		}
	}
}

func TestTrsHeaderVariantDiff(t *testing.T) {
	s := &trsServer{rootStatus: 200, basicStatus: 200, loginStatus: 201, denyPath: "/redfish/v1/Systems/1", denyUnlessU: "curl/7.76.1"}
	rep, f := trsRun(t, s)
	if len(f.denied) == 0 || len(f.variantDiff) == 0 {
		t.Fatalf("403 경로/변형 차이 기록 실패: %+v", f)
	}
	if !strings.Contains(rep, "User-Agent=curl") {
		t.Errorf("변형 비교 결과 누락:\n%s", rep)
	}
}

func TestTrsRootForbidden(t *testing.T) {
	s := &trsServer{rootStatus: 403, basicStatus: 200, loginStatus: 201}
	rep, _ := trsRun(t, s)
	if !strings.Contains(rep, "IP 접근 제한") {
		t.Errorf("IP 제한 진단 누락:\n%s", rep)
	}
}

func TestTrsTCPFail(t *testing.T) {
	conf := &Config{User: "admin", Insecure: true, Timeout: 1 * time.Second}
	rep, f := trsRunHost(Target{Hostname: "x", IP: "127.0.0.1", Port: "1"}, conf, "pw", nil, nil)
	if f.tcpErr == "" || !strings.Contains(rep, "TCP 연결이 안 됩니다") {
		t.Errorf("TCP 실패 진단 누락:\n%s", rep)
	}
}

func TestTrsAllowedGuards(t *testing.T) {
	p := NewProber("127.0.0.1:1", true, time.Second)
	defer p.Close()
	for _, c := range []struct{ m, p string }{
		{http.MethodPatch, "/redfish/v1/Systems/1/Bios/Settings"},
		{http.MethodPost, "/redfish/v1/Systems/1/Actions/ComputerSystem.Reset"},
		{http.MethodGet, "/redfish/v1/Systems/1/LogServices"},
		{http.MethodDelete, "/redfish/v1/AccountService/Accounts/3"},
		{http.MethodDelete, "/redfish/v1/SessionService/Sessions/7"}, // 만들지 않은 세션
		{http.MethodGet, "/etc/passwd"},
	} {
		if r := p.Send(ProbeReq{Method: c.m, Path: c.p}); !errors.Is(r.Err, ErrNotAllowed) {
			t.Errorf("%s %s 가 허용됨: %v", c.m, c.p, r.Err)
		}
	}
	if r := p.Send(ProbeReq{Method: http.MethodGet, Path: "/redfish/v1/Systems"}); r.Err == nil || errors.Is(r.Err, ErrNotAllowed) {
		t.Errorf("GET Systems 는 허용돼야 함 (연결 실패여야 함): %v", r.Err)
	}
	if !p.SetSession("https://x/redfish/v1/SessionService/Sessions/7") || p.Session() == "" {
		t.Error("세션 등록 실패")
	}
	if p.SetSession("/redfish/v1/AccountService/Accounts/3") {
		t.Error("세션이 아닌 경로가 등록됨")
	}
}

func TestTrsRoot401IsNotBlock(t *testing.T) {
	s := &trsServer{rootStatus: 401, basicStatus: 200, loginStatus: 201}
	rep, f := trsRun(t, s)
	if strings.Contains(rep, "IP 접근 제한") || len(f.uaDiff) != 0 {
		t.Errorf("ServiceRoot 401 을 접속 차단으로 판정함:\n%s", rep)
	}
	if !strings.Contains(rep, "모두 정상") {
		t.Errorf("정상 판정 누락:\n%s", rep)
	}
}

func TestTrsSession415VariantRecovers(t *testing.T) {
	s := &trsServer{rootStatus: 200, basicStatus: 200, loginStatus: 201, sessNeedCT: "application/json;charset=utf-8"}
	rep, f := trsRun(t, s)
	if !f.tokenOK || f.sessionVariantOK == "" {
		t.Fatalf("415 뒤 변형으로 세션을 얻지 못함: %+v", f)
	}
	if !strings.Contains(rep, "HTTP 415") || !strings.Contains(rep, "Content-Type") {
		t.Errorf("415 설명 누락:\n%s", rep)
	}
	if last := s.calls[len(s.calls)-1]; !strings.HasPrefix(last, "DELETE ") {
		t.Errorf("변형으로 만든 세션을 지우지 않음: %s", last)
	}
}

func TestTrs406And405Notes(t *testing.T) {
	s := &trsServer{rootStatus: 200, basicStatus: 200, loginStatus: 201, noAcceptP: "/redfish/v1/Systems/1", status405P: "/redfish/v1/Managers"}
	rep, f := trsRun(t, s)
	for _, want := range []string{"HTTP 406", "HTTP 405", "Allow=PATCH", "Accept·OData-Version 없음"} {
		if !strings.Contains(rep, want) {
			t.Errorf("%q 누락:\n%s", want, rep)
		}
	}
	if len(f.variantDiff) == 0 {
		t.Error("406 를 푸는 변형을 기록하지 못함")
	}
}

func TestTrs4xxTableCovers(t *testing.T) {
	for _, c := range []int{400, 404, 405, 406, 408, 409, 411, 413, 414, 415, 417, 422, 423, 429, 431} {
		if _, ok := trs4xx[c]; !ok {
			t.Errorf("%d 설명이 표에 없음", c)
		}
	}
	f := &trsFacts{codes: map[int][]string{499: {"GET /x"}}}
	if n := trs4xxNotes(f); len(n) != 1 || !strings.Contains(n[0], "기타 4xx") {
		t.Errorf("표에 없는 코드 처리: %v", n)
	}
}

func TestTrsReplayRedirectNoSlash(t *testing.T) {
	s := &trsServer{rootStatus: 200, basicStatus: 200, loginStatus: 201, redirectNoSlash: true}
	rep, f := trsRun(t, s)
	if f.rootNoSlash != 301 || !f.replayRan || f.replayErr == "" {
		t.Fatalf("리다이렉트 재현 실패: %+v", f)
	}
	for _, want := range []string{"리다이렉트", "/redfish/v1/ 로 요청하도록 수정"} {
		if !strings.Contains(rep, want) {
			t.Errorf("%q 누락:\n%s", want, rep)
		}
	}
}

func TestTrsReplayOK(t *testing.T) {
	s := &trsServer{rootStatus: 200, basicStatus: 200, loginStatus: 201}
	rep, f := trsRun(t, s)
	if !f.replayRan || f.replayErr != "" {
		t.Fatalf("정상 재현이 실패로 기록됨: %+v", f)
	}
	if !strings.Contains(rep, "bios.sh 와 같은 방식(실제 Client)") {
		t.Errorf("재현 결과 문구 누락:\n%s", rep)
	}
}
