package main

import (
	"errors"
	"strings"
	"testing"
	"time"

	"biostool/internal/mockbmc"
)

// redfish.go 의 Client 를 단계 3 의 mockbmc(testdata 합성 트리)에 붙여 보는 통합 테스트.
// 실제 BMC 에는 접속하지 않는다. 계획서 성공기준 2·3 (허용목록, 호스트당 세션 1·1) 을 mock 쪽 집계로 확인한다.

var mockTrees = []string{"dell-r660", "hpe-dl360gen11", "hpe-dl360gen10", "lenovo-sr650v3", "cisco-c220m7", "supermicro-x12"}

func startMockTree(t *testing.T, name string, mod func(o *mockbmc.Options)) *mockbmc.Server {
	t.Helper()
	o := mockbmc.Options{User: testUser, Pass: testPass}
	if mod != nil {
		mod(&o)
	}
	s, err := mockbmc.New("../../testdata/"+name, o)
	if err != nil {
		t.Fatalf("mockbmc.New(%s): %v", name, err)
	}
	s.Start()
	t.Cleanup(s.Close)
	return s
}

func mockClient(s *mockbmc.Server, mode Mode, retries int) (*Client, *sleepRec) {
	c := NewClient(ClientOpts{
		BaseURL: s.URL(), User: testUser, Pass: testPass, Insecure: true,
		Timeout: 5 * time.Second, Retries: retries, Mode: mode, Gap: -1,
	})
	sr := &sleepRec{}
	c.sleep = sr.sleep
	return c, sr
}

// link 는 {"@odata.id": ...} 객체에서 경로를 꺼낸다.
type link struct {
	ID string `json:"@odata.id"`
}

func TestMockReadOnlyFlowAllTrees(t *testing.T) {
	for _, name := range mockTrees {
		name := name
		t.Run(name, func(t *testing.T) {
			s := startMockTree(t, name, nil)
			c, _ := mockClient(s, ModeReadOnly, 0)

			if err := c.Login(); err != nil {
				t.Fatalf("Login: %v", err)
			}
			var rootDoc struct {
				RedfishVersion string
				Systems        link
			}
			if err := c.GetJSON("/redfish/v1", &rootDoc); err != nil || rootDoc.RedfishVersion == "" {
				t.Fatalf("ServiceRoot: %v %+v", err, rootDoc)
			}
			var systems struct{ Members []link }
			if err := c.GetJSON(rootDoc.Systems.ID, &systems); err != nil || len(systems.Members) != 1 {
				t.Fatalf("Systems: %v %+v", err, systems)
			}
			var sys struct {
				Manufacturer, Model, BiosVersion string
				Bios                             link
			}
			if err := c.GetJSON(systems.Members[0].ID, &sys); err != nil || sys.Model == "" || sys.BiosVersion == "" {
				t.Fatalf("System: %v %+v", err, sys)
			}
			var bios struct {
				Attributes map[string]interface{}
				Settings   struct{ SettingsObject link } `json:"@Redfish.Settings"`
			}
			if err := c.GetJSON(sys.Bios.ID, &bios); err != nil || len(bios.Attributes) < 15 {
				t.Fatalf("Bios: %v (속성 %d개)", err, len(bios.Attributes))
			}
			if bios.Settings.SettingsObject.ID != "" {
				var st struct{ Attributes map[string]interface{} }
				if err := c.GetJSON(bios.Settings.SettingsObject.ID, &st); err != nil {
					t.Fatalf("Settings: %v", err)
				}
			}
			if err := c.Close(); err != nil {
				t.Fatalf("Close: %v", err)
			}

			ss := s.Sessions()
			if ss.Created != 1 || ss.Deleted != 1 || ss.LoginFailed != 0 {
				t.Errorf("mock 세션 = %+v, 기대 생성 1 / 삭제 1", ss)
			}
			if st := c.Stats(); st.Sessions != 1 || st.Deletes != 1 || st.AuthMode != "session" {
				t.Errorf("client Stats = %+v", st)
			}
			if got := s.Count("POST", "/redfish/v1/SessionService/Sessions"); got != 1 {
				t.Errorf("세션 POST %d회, 기대 1", got)
			}
			if s.LogHits() != 0 {
				t.Errorf("LogHits = %d, 기대 0", s.LogHits())
			}
			if s.Writes() != 0 {
				t.Errorf("Writes = %d, 기대 0 (읽기 전용 흐름)", s.Writes())
			}
			if g := s.Count("GET", "/"); g != c.Stats().Gets {
				t.Errorf("mock GET %d ≠ client GET %d", g, c.Stats().Gets)
			}
		})
	}
}

func TestMockClientNeverReachesLogPaths(t *testing.T) {
	s := startMockTree(t, "dell-r660", nil)
	c, _ := mockClient(s, ModeSet, 0)
	defer c.Close()
	for _, p := range []string{
		"/redfish/v1/Systems/System.Embedded.1/LogServices",
		"/redfish/v1/Systems/System.Embedded.1/LogServices/Sel/Entries",
		"/redfish/v1/Managers/iDRAC.Embedded.1/LogServices/Lclog",
		"/redfish/v1/systems/system.embedded.1/logservices/",
		"/redfish/v1/Systems/System.Embedded.1/LogServices?$expand=*",
	} {
		if _, err := c.GetRaw(p); !errors.Is(err, ErrNotAllowed) {
			t.Errorf("GET %s: %v, 기대 ErrNotAllowed", p, err)
		}
	}
	if _, _, err := c.Patch("/redfish/v1/Systems/System.Embedded.1/LogServices/Sel", map[string]interface{}{"Attributes": map[string]string{}}); !errors.Is(err, ErrNotAllowed) {
		t.Errorf("로그 경로 PATCH: %v", err)
	}
	if s.LogHits() != 0 || s.Count("", "/") != 0 {
		t.Errorf("차단된 호출이 mock 에 도달함: LogHits=%d 전체=%d", s.LogHits(), s.Count("", "/"))
	}
	// 거부된 호출은 로그인(세션 POST)도 일으키지 않았다
	if s.Sessions().Created != 0 {
		t.Errorf("거부된 호출만 했는데 세션이 만들어짐: %+v", s.Sessions())
	}
}

func TestMockBasicFallbackWhenNoSessionService(t *testing.T) {
	s := startMockTree(t, "hpe-dl360gen11", func(o *mockbmc.Options) { o.NoSessions = true })
	c, _ := mockClient(s, ModeReadOnly, 0)
	if err := c.Login(); err != nil {
		t.Fatalf("Login: %v", err)
	}
	if c.Stats().AuthMode != "basic" {
		t.Fatalf("AuthMode = %q, 기대 basic", c.Stats().AuthMode)
	}
	var d struct{ Id string }
	if err := c.GetJSON("/redfish/v1/systems/1/", &d); err != nil || d.Id != "1" {
		t.Fatalf("Basic 으로 조회: %v %+v", err, d)
	}
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	if ss := s.Sessions(); ss.Created != 0 || ss.Deleted != 0 {
		t.Errorf("세션 = %+v", ss)
	}
	if s.Count("DELETE", "/") != 0 {
		t.Errorf("세션이 없는데 DELETE 가 나감")
	}
}

func TestMockAuthFailStopsImmediately(t *testing.T) {
	s := startMockTree(t, "dell-r660", nil)
	c := NewClient(ClientOpts{BaseURL: s.URL(), User: testUser, Pass: "틀린-비밀번호", Insecure: true, Retries: 3, Gap: -1})
	c.sleep = func(time.Duration) {}
	err := c.Login()
	if rfStatus(t, err) != StatusAuthFail {
		t.Fatalf("Login = %v, 기대 AUTH_FAIL", err)
	}
	if err := c.GetJSON("/redfish/v1", &struct{}{}); rfStatus(t, err) != StatusAuthFail {
		t.Errorf("이후 조회 = %v", err)
	}
	_ = c.Close()
	if n := s.Count("", "/"); n != 1 {
		t.Errorf("mock 이 받은 요청 %d건, 기대 1 (로그인 1회 후 재시도·추가 요청 없음)", n)
	}
	if ss := s.Sessions(); ss.LoginFailed != 1 || ss.Created != 0 {
		t.Errorf("세션 = %+v", ss)
	}
}

func TestMockSetModeWritesPendingAndJob(t *testing.T) {
	s := startMockTree(t, "dell-r660", nil)
	const set = "/redfish/v1/Systems/System.Embedded.1/Bios/Settings"
	const jobs = "/redfish/v1/Managers/iDRAC.Embedded.1/Jobs"

	// 읽기 전용 클라이언트는 쓰기가 아예 나가지 않는다
	ro, _ := mockClient(s, ModeReadOnly, 0)
	if _, _, err := ro.Patch(set, map[string]interface{}{"Attributes": map[string]string{"LogicalProc": "Disabled"}}); !errors.Is(err, ErrNotAllowed) {
		t.Errorf("읽기 전용 PATCH: %v", err)
	}
	if _, _, _, err := ro.PostJob(jobs, map[string]string{"TargetSettingsURI": set}); !errors.Is(err, ErrNotAllowed) {
		t.Errorf("읽기 전용 PostJob: %v", err)
	}
	_ = ro.Close()
	if s.Writes() != 0 {
		t.Fatalf("읽기 전용 클라이언트인데 Writes = %d", s.Writes())
	}

	c, _ := mockClient(s, ModeSet, 3)
	st, _, err := c.Patch(set, map[string]interface{}{"Attributes": map[string]string{"LogicalProc": "Disabled"}})
	if err != nil || st != 200 {
		t.Fatalf("PATCH = %d %v", st, err)
	}
	var cur struct{ Attributes map[string]interface{} }
	if err := c.GetJSON(set, &cur); err != nil || cur.Attributes["LogicalProc"] != "Disabled" {
		t.Errorf("Settings 재조회: %v %v", err, cur.Attributes)
	}
	var bios struct{ Attributes map[string]interface{} }
	if err := c.GetJSON("/redfish/v1/Systems/System.Embedded.1/Bios", &bios); err != nil || bios.Attributes["LogicalProc"] != "Enabled" {
		t.Errorf("현재 Bios 는 그대로여야 함: %v %v", err, bios.Attributes["LogicalProc"])
	}
	jst, hdr, _, err := c.PostJob(jobs, map[string]string{"TargetSettingsURI": set})
	if err != nil || jst != 202 || !strings.HasSuffix(hdr.Get("Location"), "/Jobs/JID_000000000001") {
		t.Fatalf("PostJob = %d %v %v", jst, hdr.Get("Location"), err)
	}
	// 잘못된 값은 400 → HTTP_ERROR, 재시도 없음
	bst, _, err := c.Patch(set, map[string]interface{}{"Attributes": map[string]string{"LogicalProc": "Maybe"}})
	if bst != 400 || rfStatus(t, err) != StatusHTTPError {
		t.Errorf("잘못된 값 PATCH = %d %v", bst, err)
	}
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	if got := s.Count("PATCH", "/"); got != 2 {
		t.Errorf("PATCH %d회, 기대 2 (성공 1 + 거부 1, 재시도 없음)", got)
	}
	if got := s.Count("POST", "/redfish/v1/Managers"); got != 1 {
		t.Errorf("Job POST %d회, 기대 1", got)
	}
	if s.LogHits() != 0 {
		t.Errorf("LogHits = %d", s.LogHits())
	}
	if ss := s.Sessions(); ss.Created != 1 || ss.Deleted != 1 {
		t.Errorf("세션 = %+v (읽기 전용 클라이언트는 로그인도 안 함, 쓰기 클라이언트 1·1)", ss)
	}
}

func TestMockTransientErrorsAreRetried(t *testing.T) {
	s := startMockTree(t, "lenovo-sr650v3", func(o *mockbmc.Options) {
		o.Fail = map[string]mockbmc.Fault{"/redfish/v1/Systems": {Status: 503, Times: 2}}
	})
	c, sr := mockClient(s, ModeReadOnly, 3)
	defer c.Close()
	if err := c.GetJSON("/redfish/v1/Systems", &struct{}{}); err != nil {
		t.Fatalf("재시도 후 성공해야 함: %v", err)
	}
	if got := s.Count("GET", "/redfish/v1/Systems"); got != 3 {
		t.Errorf("Systems GET %d회, 기대 3 (503, 503, 200)", got)
	}
	if len(sr.get()) != 2 {
		t.Errorf("백오프 대기 = %v, 기대 2회", sr.get())
	}
	if s.Sessions().Created != 1 {
		t.Errorf("재시도 중 세션이 늘어남: %+v", s.Sessions())
	}
}

func TestMockSlowResponseIsTimeout(t *testing.T) {
	s := startMockTree(t, "cisco-c220m7", func(o *mockbmc.Options) {
		o.Fail = map[string]mockbmc.Fault{"/redfish/v1/Systems": {Delay: 30 * time.Second}}
	})
	c := NewClient(ClientOpts{BaseURL: s.URL(), User: testUser, Pass: testPass, Insecure: true, Timeout: 300 * time.Millisecond, Gap: -1})
	defer c.Close()
	err := c.GetJSON("/redfish/v1/Systems", &struct{}{})
	if rfStatus(t, err) != StatusTimeout {
		t.Errorf("느린 응답 = %v, 기대 TIMEOUT", err)
	}
}

func TestMockDroppedConnectionIsUnreachable(t *testing.T) {
	s := startMockTree(t, "supermicro-x12", func(o *mockbmc.Options) {
		o.Fail = map[string]mockbmc.Fault{"/redfish/v1/Systems": {Drop: true}}
	})
	c, _ := mockClient(s, ModeReadOnly, 0)
	defer c.Close()
	err := c.GetJSON("/redfish/v1/Systems", &struct{}{})
	if rfStatus(t, err) != StatusUnreachable {
		t.Errorf("연결 끊김 = %v, 기대 UNREACHABLE", err)
	}
}
