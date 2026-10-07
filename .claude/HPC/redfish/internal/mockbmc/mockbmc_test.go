package mockbmc

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
)

// 실제 BMC 접속 없이 mock 자체를 시험한다. 트리는 PROJ/testdata (합성 자료) 를 쓴다.

const (
	tUser = "admin"
	tPass = `Mock-P@ss!<"1">`
)

var treeNames = []string{"dell-r660", "hpe-dl360gen11", "hpe-dl360gen10", "lenovo-sr650v3", "cisco-c220m7", "supermicro-x12"}

func treeDir(name string) string { return filepath.Join("..", "..", "testdata", name) }

func newSrv(t *testing.T, name string, mod func(o *Options)) *Server {
	t.Helper()
	o := Options{User: tUser, Pass: tPass}
	if mod != nil {
		mod(&o)
	}
	s, err := New(treeDir(name), o)
	if err != nil {
		t.Fatalf("New(%s): %v", name, err)
	}
	t.Cleanup(s.Close)
	return s
}

// cli 는 mock 에 직접 요청하는 시험용 HTTP 클라이언트입니다 (biostool 의 Client 가 아님).
type cli struct {
	t     *testing.T
	base  string
	hc    *http.Client
	token string
}

func newCli(t *testing.T, base string, timeout time.Duration) *cli {
	tr := &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, DisableKeepAlives: true}
	t.Cleanup(tr.CloseIdleConnections)
	return &cli{t: t, base: base, hc: &http.Client{Transport: tr, Timeout: timeout,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
}

// do 는 요청을 보낸다. c.token 이 있으면 토큰 헤더를, basic(user, pass) 을 주면 Basic 헤더를 같이 보낸다.
func (c *cli) do(method, p string, body interface{}, basic ...string) (int, http.Header, []byte, error) {
	var rd *bytes.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	} else {
		rd = bytes.NewReader(nil)
	}
	req, _ := http.NewRequest(method, c.base+p, rd)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if len(basic) == 2 {
		req.SetBasicAuth(basic[0], basic[1])
	}
	if c.token != "" {
		req.Header.Set("X-Auth-Token", c.token)
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		return 0, nil, nil, err
	}
	defer resp.Body.Close()
	var buf bytes.Buffer
	_, _ = buf.ReadFrom(resp.Body)
	return resp.StatusCode, resp.Header, buf.Bytes(), nil
}

func (c *cli) must(method, p string, body interface{}, want int) []byte {
	c.t.Helper()
	st, _, b, err := c.do(method, p, body)
	if err != nil {
		c.t.Fatalf("%s %s: %v", method, p, err)
	}
	if st != want {
		c.t.Fatalf("%s %s = %d, 기대 %d: %s", method, p, st, want, b)
	}
	return b
}

func (c *cli) login() string {
	c.t.Helper()
	st, h, b, err := c.do("POST", "/redfish/v1/SessionService/Sessions", map[string]string{"UserName": tUser, "Password": tPass})
	if err != nil || st != 201 {
		c.t.Fatalf("로그인 실패: %d %v %s", st, err, b)
	}
	c.token = h.Get("X-Auth-Token")
	if c.token == "" || h.Get("Location") == "" {
		c.t.Fatalf("토큰/Location 없음: %v", h)
	}
	return h.Get("Location")
}

func (c *cli) attrs(p string) map[string]interface{} {
	c.t.Helper()
	var d struct{ Attributes map[string]interface{} }
	if err := json.Unmarshal(c.must("GET", p, nil, 200), &d); err != nil {
		c.t.Fatal(err)
	}
	return d.Attributes
}

func startLogin(t *testing.T, name string, mod func(o *Options)) (*Server, *cli) {
	t.Helper()
	s := newSrv(t, name, mod)
	c := newCli(t, s.Start(), 5*time.Second)
	c.login()
	return s, c
}

// ---- 트리 ----

// 표준 4개 항목(system_profile, hyper_threading, llc_prefetch, sub_numa_cluster) 의 합성 속성명. "" 은 일부러 없음.
var stdAttrs = map[string][4]string{
	"dell-r660":      {"SysProfile", "LogicalProc", "LlcPrefetch", "SubNumaCluster"},
	"hpe-dl360gen11": {"WorkloadProfile", "ProcHyperthreading", "LlcPrefetch", "SubNumaClustering"},
	"hpe-dl360gen10": {"WorkloadProfile", "ProcHyperthreading", "", "SubNumaClustering"},
	"lenovo-sr650v3": {"OperatingModes_ChooseOperatingMode", "Processors_HyperThreading", "Processors_LLCPrefetch", "Processors_SNC"},
	"cisco-c220m7":   {"CPUPerformance", "IntelHyperThread", "LLCPrefetch", "SNC"},
	"supermicro-x12": {"PowerTechnology", "Hyper-Threading[ALL]", "LLCPrefetch", "SNC"},
}

// 트리 전체에서 @odata.id 링크와 Location[].Uri 를 모은다.
func collectLinks(v interface{}, out *[]string) {
	switch x := v.(type) {
	case map[string]interface{}:
		for k, val := range x {
			if s, ok := val.(string); ok && (k == "@odata.id" || k == "Uri") {
				*out = append(*out, s)
			}
			collectLinks(val, out)
		}
	case []interface{}:
		for _, e := range x {
			collectLinks(e, out)
		}
	}
}

func TestTreesLoadAndAreConsistent(t *testing.T) {
	for _, name := range treeNames {
		name := name
		t.Run(name, func(t *testing.T) {
			s := newSrv(t, name, nil)
			for p, b := range s.tree {
				var m map[string]interface{}
				if err := json.Unmarshal(b, &m); err != nil {
					t.Fatalf("%s: %v", p, err)
				}
				oem, _ := m["Oem"].(map[string]interface{})
				if oem["MockData"] != true {
					t.Errorf("%s: Oem.MockData 표식이 없음 (합성 자료 표시 필수)", p)
				}
				var links []string
				collectLinks(m, &links)
				for _, l := range links {
					if _, ok := s.tree[normPath(l)]; !ok {
						t.Errorf("%s: 링크 %q 가 트리에 없음", p, l)
					}
				}
			}
			// ServiceRoot → Systems → System → Bios
			var rootDoc struct {
				RedfishVersion string
				Systems        struct {
					ID string `json:"@odata.id"`
				}
			}
			_ = json.Unmarshal(s.tree[root], &rootDoc)
			if rootDoc.RedfishVersion == "" || rootDoc.Systems.ID == "" {
				t.Fatalf("ServiceRoot 에 RedfishVersion/Systems 없음")
			}
			if len(s.biosPaths) != 1 {
				t.Fatalf("Bios 경로 = %v", s.biosPaths)
			}
			sysPath := strings.TrimSuffix(s.biosPaths[0], "/bios")
			var sys map[string]interface{}
			_ = json.Unmarshal(s.tree[sysPath], &sys)
			for _, k := range []string{"Manufacturer", "Model", "BiosVersion", "SerialNumber"} {
				if str, _ := sys[k].(string); str == "" {
					t.Errorf("Systems 에 %s 없음", k)
				}
			}
			if _, ok := sys["LogServices"]; !ok {
				t.Errorf("Systems 에 LogServices 링크가 없음 (LogHits 시험용으로 필요)")
			}

			var bios struct {
				Attributes map[string]interface{}
				Settings   json.RawMessage `json:"@Redfish.Settings"`
			}
			_ = json.Unmarshal(s.tree[s.biosPaths[0]], &bios)
			if len(bios.Attributes) < 15 {
				t.Errorf("Bios 속성 %d개 (최소 15)", len(bios.Attributes))
			}
			for i, a := range stdAttrs[name] {
				if a == "" {
					continue
				}
				if _, ok := bios.Attributes[a]; !ok {
					t.Errorf("표준 항목 #%d 속성 %q 가 Bios 에 없음", i, a)
				}
			}
			hostSpecific := 0
			for a := range bios.Attributes {
				la := strings.ToLower(a)
				if strings.Contains(la, "tag") || strings.Contains(la, "serial") {
					hostSpecific++
				}
			}
			if hostSpecific == 0 {
				t.Errorf("호스트별로 다른 값(태그/시리얼 계열) 속성이 없음")
			}
			// 레지스트리: 모든 속성이 등재돼 있고 현재값이 허용값 안
			s.mu.Lock()
			reg, ok := s.registryLocked(s.biosPaths[0])
			s.mu.Unlock()
			if !ok {
				t.Fatalf("레지스트리를 찾지 못함")
			}
			for a, v := range bios.Attributes {
				e := validateAttr(a, v, reg)
				if e != nil && e.msgID != "PropertyNotWritable" {
					t.Errorf("속성 %s 현재값 %v 가 레지스트리와 맞지 않음: %s", a, v, e.msg)
				}
			}
			if len(reg) != len(bios.Attributes) {
				t.Errorf("레지스트리 속성 %d개 ≠ Bios 속성 %d개", len(reg), len(bios.Attributes))
			}
			// Settings 경로
			s.mu.Lock()
			settings := ""
			for p := range s.tree {
				if _, ok := s.settingsForLocked(p); ok {
					settings = p
				}
			}
			s.mu.Unlock()
			want := map[string]string{
				"lenovo-sr650v3": "/redfish/v1/systems/1/bios/pending",
				"cisco-c220m7":   "/redfish/v1/systems/mock-c220m7/bios/settings",
				"supermicro-x12": "",
			}
			w, has := want[name]
			if !has {
				w = sysPath + "/bios/settings"
			}
			if settings != w {
				t.Errorf("SettingsObject = %q, 기대 %q", settings, w)
			}
		})
	}
}

func TestAllTreesServeRootWithSession(t *testing.T) {
	for _, name := range treeNames {
		name := name
		t.Run(name, func(t *testing.T) {
			s, c := startLogin(t, name, nil)
			var d struct{ RedfishVersion string }
			if err := json.Unmarshal(c.must("GET", "/redfish/v1", nil, 200), &d); err != nil || d.RedfishVersion == "" {
				t.Fatalf("ServiceRoot: %v %+v", err, d)
			}
			c.must("GET", "/redfish/v1/", nil, 200) // 끝 슬래시
			c.must("GET", "/redfish/v1/Systems", nil, 200)
			c.must("GET", "/redfish/v1/nope", nil, 404)
			if s.LogHits() != 0 {
				t.Errorf("LogHits = %d", s.LogHits())
			}
		})
	}
}

func TestLoadTreeErrors(t *testing.T) {
	if _, err := New(filepath.Join(t.TempDir(), "없는디렉터리"), Options{User: "a", Pass: "b"}); err == nil {
		t.Error("없는 디렉터리인데 오류가 없음")
	}
	d := t.TempDir()
	if _, err := New(d, Options{User: "a", Pass: "b"}); err == nil || !strings.Contains(err.Error(), "ServiceRoot") {
		t.Errorf("ServiceRoot 없음 오류 기대: %v", err)
	}
	mustWrite(t, d, "redfish/v1.json", `{"a":`)
	if _, err := New(d, Options{User: "a", Pass: "b"}); err == nil || !strings.Contains(err.Error(), "JSON") {
		t.Errorf("깨진 JSON 오류 기대: %v", err)
	}
	mustWrite(t, d, "redfish/v1.json", `{}`)
	if _, err := New(d, Options{}); err == nil {
		t.Error("계정 없이 만들어짐")
	}
	if _, err := New(d, Options{User: "a", Pass: "b"}); err != nil {
		t.Errorf("정상 트리: %v", err)
	}
	// 대소문자만 다른 파일 (대소문자 구분 파일시스템에서만 만들어진다)
	mustWrite(t, d, "redfish/v1/X.json", `{}`)
	mustWrite(t, d, "redfish/v1/x.JSON", `{}`)
	if ents, _ := os.ReadDir(filepath.Join(d, "redfish", "v1")); len(ents) == 2 {
		if _, err := New(d, Options{User: "a", Pass: "b"}); err == nil || !strings.Contains(err.Error(), "중복") {
			t.Errorf("대소문자 중복 오류 기대: %v", err)
		}
	}
}

func mustWrite(t *testing.T, dir, rel, content string) {
	t.Helper()
	p := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// ---- 경로 규칙 ----

func TestPathLookupIgnoresCaseAndTrailingSlash(t *testing.T) {
	_, c := startLogin(t, "hpe-dl360gen11", nil)
	for _, p := range []string{
		"/redfish/v1/systems/1/bios/settings/",
		"/redfish/v1/systems/1/bios/settings",
		"/redfish/v1/Systems/1/Bios/Settings",
		"/REDFISH/V1/SYSTEMS/1/BIOS/SETTINGS/",
	} {
		c.must("GET", p, nil, 200)
	}
	// 소문자 경로로 PATCH 도 된다
	c.must("PATCH", "/redfish/v1/systems/1/bios/settings/", map[string]interface{}{"Attributes": map[string]interface{}{"ProcHyperthreading": "Disabled"}}, 200)
	if got := c.attrs("/redfish/v1/Systems/1/Bios/Settings")["ProcHyperthreading"]; got != "Disabled" {
		t.Errorf("pending = %v", got)
	}
}

func TestPathRuleWithJSONSuffixedURI(t *testing.T) {
	// Dell 의 레지스트리 URI 는 ".json" 으로 끝나며 파일은 "....json.json" 이다.
	_, c := startLogin(t, "dell-r660", nil)
	b := c.must("GET", "/redfish/v1/Registries/BiosAttributeRegistry.v1_0_0.json", nil, 200)
	if !strings.Contains(string(b), "RegistryEntries") {
		t.Errorf("레지스트리 본문이 아님: %.80s", b)
	}
}

// ---- 세션·인증 ----

func TestSessionCreateDeleteAnd401(t *testing.T) {
	s := newSrv(t, "dell-r660", nil)
	c := newCli(t, s.Start(), 5*time.Second)

	if st, _, _, _ := c.do("GET", "/redfish/v1", nil); st != 401 {
		t.Errorf("토큰 없음 = %d, 기대 401", st)
	}
	if st, _, b, _ := c.do("POST", "/redfish/v1/SessionService/Sessions", map[string]string{"UserName": tUser, "Password": "틀림"}); st != 401 || strings.Contains(string(b), tPass) {
		t.Errorf("틀린 비밀번호 = %d %s", st, b)
	}
	loc := c.login()
	if loc != "/redfish/v1/SessionService/Sessions/1" {
		t.Errorf("Location = %q", loc)
	}
	c.must("GET", "/redfish/v1/Systems", nil, 200)
	c.must("GET", loc, nil, 200)
	good := c.token
	c.token = "엉터리-토큰"
	if st, _, _, _ := c.do("GET", "/redfish/v1/Systems", nil); st != 401 {
		t.Errorf("틀린 토큰 = %d, 기대 401", st)
	}
	// 틀린 토큰은 Basic 으로 넘어가지 않는다
	if st, _, _, _ := c.do("GET", "/redfish/v1/Systems", nil, tUser, tPass); st != 401 {
		t.Errorf("틀린 토큰 + 올바른 Basic = %d, 기대 401 (토큰 우선)", st)
	}
	c.token = good
	c.must("DELETE", loc, nil, 204)
	if st, _, _, _ := c.do("GET", "/redfish/v1/Systems", nil); st != 401 {
		t.Errorf("삭제된 세션 토큰 = %d, 기대 401", st)
	}
	c.must("DELETE", loc, nil, 401) // 이미 없는 토큰으로는 인증부터 실패
	ss := s.Sessions()
	if ss.Created != 1 || ss.Deleted != 1 || ss.LoginFailed != 1 {
		t.Errorf("Sessions = %+v", ss)
	}
	// 새 세션 번호는 증가한다
	c.token = ""
	if l2 := c.login(); l2 != "/redfish/v1/SessionService/Sessions/2" {
		t.Errorf("두 번째 Location = %q", l2)
	}
	c.must("DELETE", "/redfish/v1/SessionService/Sessions/99", nil, 404)
}

func TestBasicAuthAndNoSessionsFallback(t *testing.T) {
	s := newSrv(t, "dell-r660", func(o *Options) { o.NoSessions = true })
	c := newCli(t, s.Start(), 5*time.Second)
	if st, _, _, _ := c.do("POST", "/redfish/v1/SessionService/Sessions", map[string]string{"UserName": tUser, "Password": tPass}); st != 404 {
		t.Errorf("세션 미지원 모드의 세션 POST = %d, 기대 404", st)
	}
	if st, _, _, _ := c.do("GET", "/redfish/v1/Systems", nil, tUser, tPass); st != 200 {
		t.Errorf("Basic = %d", st)
	}
	if st, _, _, _ := c.do("GET", "/redfish/v1/Systems", nil, tUser, "틀림"); st != 401 {
		t.Errorf("틀린 Basic = %d", st)
	}
	if ss := s.Sessions(); ss.Created != 0 {
		t.Errorf("세션이 만들어짐: %+v", ss)
	}
	// 세션 지원 모드에서도 Basic 은 받는다
	s2 := newSrv(t, "dell-r660", nil)
	c2 := newCli(t, s2.Start(), 5*time.Second)
	if st, _, _, _ := c2.do("GET", "/redfish/v1/Systems", nil, tUser, tPass); st != 200 {
		t.Errorf("세션 지원 BMC 의 Basic = %d", st)
	}
}

// ---- 쓰기 ----

func patchBody(kv ...interface{}) map[string]interface{} {
	a := map[string]interface{}{}
	for i := 0; i+1 < len(kv); i += 2 {
		a[kv[i].(string)] = kv[i+1]
	}
	return map[string]interface{}{"Attributes": a}
}

func TestPatchGoesToPendingAndCurrentIsUnchanged(t *testing.T) {
	s, c := startLogin(t, "dell-r660", nil)
	const bios, set = "/redfish/v1/Systems/System.Embedded.1/Bios", "/redfish/v1/Systems/System.Embedded.1/Bios/Settings"
	before := c.attrs(bios)
	if len(c.attrs(set)) != 0 {
		t.Fatalf("처음 Settings 는 비어 있어야 함")
	}
	c.must("PATCH", set, patchBody("LogicalProc", "Disabled", "SubNumaCluster", "Enabled"), 200)
	c.must("PATCH", set, patchBody("LogicalProc", "Enabled", "AssetTag", "NEW-TAG"), 200) // 병합·덮어쓰기
	pend := c.attrs(set)
	if pend["LogicalProc"] != "Enabled" || pend["SubNumaCluster"] != "Enabled" || pend["AssetTag"] != "NEW-TAG" || len(pend) != 3 {
		t.Errorf("pending = %v", pend)
	}
	after := c.attrs(bios)
	if fmt.Sprint(before) != fmt.Sprint(after) {
		t.Errorf("현재 Bios 속성이 바뀜")
	}
	if got := s.Pending()[strings.ToLower(set)]; len(got) != 3 {
		t.Errorf("Pending() = %v", got)
	}
	if s.Writes() != 2 || s.Count("PATCH", "/redfish/v1/systems/") != 2 {
		t.Errorf("Writes=%d PATCH=%d", s.Writes(), s.Count("PATCH", "/redfish/v1/systems/"))
	}
	// 기록에 본문이 남는다 (비밀번호는 없다)
	var patchCalls int
	for _, cl := range s.Calls() {
		if cl.Method == "PATCH" {
			patchCalls++
			if !strings.Contains(cl.Body, "Attributes") || cl.Status != 200 {
				t.Errorf("PATCH 기록: %+v", cl)
			}
		}
		if strings.Contains(cl.Body, tPass) || strings.Contains(cl.Path, tPass) {
			t.Errorf("기록에 비밀번호가 있음: %+v", cl)
		}
	}
	if patchCalls != 2 {
		t.Errorf("PATCH 기록 %d", patchCalls)
	}
}

func TestPatchValidationRejectsAndLeavesPendingUntouched(t *testing.T) {
	s, c := startLogin(t, "dell-r660", nil)
	const set = "/redfish/v1/Systems/System.Embedded.1/Bios/Settings"
	bad := []map[string]interface{}{
		patchBody("NoSuchAttr", "x"),
		patchBody("LogicalProc", "Maybe"),
		patchBody("LogicalProc", 1),
		patchBody("ServiceTag", "HACK"),                   // ReadOnly
		patchBody("LogicalProc", "Disabled", "Nope", "x"), // 하나라도 틀리면 전체 거부
		{"Attributes": map[string]interface{}{}},          // 빈 Attributes
		{"Foo": 1},                                        // Attributes 없음
	}
	for i, b := range bad {
		st, _, body, _ := c.do("PATCH", set, b)
		if st != 400 || !strings.Contains(string(body), "MessageId") {
			t.Errorf("#%d: %d %s, 기대 400 + ExtendedInfo", i, st, body)
		}
	}
	if len(c.attrs(set)) != 0 || len(s.Pending()) != 0 {
		t.Errorf("거부된 PATCH 가 pending 에 반영됨: %v", s.Pending())
	}
	if st, _, _, _ := c.do("PATCH", set, nil); st != 400 {
		t.Errorf("본문 없는 PATCH = %d", st)
	}
	// ReadOnly 오류 메시지 구분
	_, _, body, _ := c.do("PATCH", set, patchBody("ServiceTag", "x"))
	if !strings.Contains(string(body), "PropertyNotWritable") {
		t.Errorf("ReadOnly 메시지: %s", body)
	}
}

func TestPatchTypesWithCustomRegistry(t *testing.T) {
	d := t.TempDir()
	mustWrite(t, d, "redfish/v1.json", `{}`)
	mustWrite(t, d, "redfish/v1/Systems/1/Bios.json", `{"AttributeRegistry":"R","Attributes":{"N":5},"@Redfish.Settings":{"SettingsObject":{"@odata.id":"/redfish/v1/Systems/1/Bios/Settings"}}}`)
	mustWrite(t, d, "redfish/v1/Systems/1/Bios/Settings.json", `{"Attributes":{}}`)
	mustWrite(t, d, "redfish/v1/Registries/R.json", `{"Location":[{"Uri":"/redfish/v1/Registries/R/reg"}]}`)
	mustWrite(t, d, "redfish/v1/Registries/R/reg.json", `{"RegistryEntries":{"Attributes":[
	 {"AttributeName":"N","Type":"Integer","ReadOnly":false,"LowerBound":1,"UpperBound":10},
	 {"AttributeName":"B","Type":"Boolean","ReadOnly":false},
	 {"AttributeName":"S","Type":"String","ReadOnly":false,"MaxLength":4},
	 {"AttributeName":"P","Type":"Password","ReadOnly":false,"MaxLength":4}]}}`)
	s, err := New(d, Options{User: tUser, Pass: tPass})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	c := newCli(t, s.Start(), 5*time.Second)
	c.login()
	const set = "/redfish/v1/Systems/1/Bios/Settings"
	for _, tc := range []struct {
		body map[string]interface{}
		want int
	}{
		{patchBody("N", 5), 200}, {patchBody("N", 0), 400}, {patchBody("N", 11), 400}, {patchBody("N", 2.5), 400}, {patchBody("N", "5"), 400},
		{patchBody("B", true), 200}, {patchBody("B", "true"), 400},
		{patchBody("S", "abcd"), 200}, {patchBody("S", "abcde"), 400}, {patchBody("S", 1), 400},
		{patchBody("P", "abcd"), 200},
	} {
		if st, _, b, _ := c.do("PATCH", set, tc.body); st != tc.want {
			t.Errorf("%v = %d %s, 기대 %d", tc.body, st, b, tc.want)
		}
	}
	// 레지스트리를 못 찾으면 검증하지 않는다
	s.Remove("/redfish/v1/Registries/R")
	c.must("PATCH", set, patchBody("Whatever", "x"), 200)
}

func TestSettingsObjectVariantsAnd405(t *testing.T) {
	// Lenovo: Bios/Pending 이 SettingsObject. Bios/Settings 는 없으므로 405.
	s, c := startLogin(t, "lenovo-sr650v3", nil)
	c.must("PATCH", "/redfish/v1/Systems/1/Bios/Pending", patchBody("Processors_HyperThreading", "Disable"), 200)
	if got := c.attrs("/redfish/v1/Systems/1/Bios/Pending")["Processors_HyperThreading"]; got != "Disable" {
		t.Errorf("Lenovo pending = %v", got)
	}
	c.must("PATCH", "/redfish/v1/Systems/1/Bios/Settings", patchBody("Processors_HyperThreading", "Disable"), 405)
	c.must("PATCH", "/redfish/v1/Systems/1/Bios", patchBody("Processors_HyperThreading", "Disable"), 405) // 현재 Bios 는 PATCH 불가
	c.must("PATCH", "/redfish/v1/Systems/1", map[string]string{"AssetTag": "x"}, 405)
	if s.Writes() != 4 {
		t.Errorf("Writes = %d", s.Writes())
	}

	// Supermicro: 쓰기 미지원
	_, c2 := startLogin(t, "supermicro-x12", nil)
	for _, p := range []string{"/redfish/v1/Systems/1/Bios/Settings", "/redfish/v1/Systems/1/Bios", "/redfish/v1/Systems/1/Bios/SD"} {
		c2.must("PATCH", p, patchBody("BootMode", "Legacy"), 405)
	}
	if len(c2.attrs("/redfish/v1/Systems/1/Bios")) == 0 {
		t.Error("Supermicro Bios 조회가 안 됨")
	}
}

func TestDellJobs(t *testing.T) {
	s, c := startLogin(t, "dell-r660", nil)
	const jobs = "/redfish/v1/Managers/iDRAC.Embedded.1/Jobs"
	const set = "/redfish/v1/Systems/System.Embedded.1/Bios/Settings"
	c.must("PATCH", set, patchBody("LogicalProc", "Disabled"), 200)

	st, h, _, _ := c.do("POST", jobs, map[string]string{"TargetSettingsURI": set})
	if st != 202 || h.Get("Location") != jobs+"/JID_000000000001" {
		t.Fatalf("Job 생성 = %d %v", st, h)
	}
	_, h2, _, _ := c.do("POST", jobs+"/", map[string]string{"TargetSettingsURI": strings.ToLower(set) + "/"})
	if h2.Get("Location") != jobs+"/JID_000000000002" {
		t.Errorf("두 번째 Job Location = %q (끝 슬래시 요청도 같은 규칙)", h2.Get("Location"))
	}
	var jd struct{ JobState, TargetSettingsURI string }
	if err := json.Unmarshal(c.must("GET", jobs+"/JID_000000000001", nil, 200), &jd); err != nil || jd.JobState != "Scheduled" || jd.TargetSettingsURI != set {
		t.Errorf("Job GET: %v %+v", err, jd)
	}
	var coll struct {
		Count   int `json:"Members@odata.count"`
		Members []struct {
			ID string `json:"@odata.id"`
		}
	}
	_ = json.Unmarshal(c.must("GET", jobs, nil, 200), &coll)
	if coll.Count != 2 || len(coll.Members) != 2 {
		t.Errorf("Jobs 컬렉션 = %+v", coll)
	}
	c.must("GET", jobs+"/JID_000000000099", nil, 404)
	// 잘못된 대상·본문
	for _, b := range []interface{}{map[string]string{"TargetSettingsURI": "/redfish/v1/Systems/System.Embedded.1/Bios"}, map[string]string{}, map[string]string{"TargetSettingsURI": "/nope"}} {
		c.must("POST", jobs, b, 400)
	}
	if len(s.Jobs()) != 2 || s.Jobs()[0].Target != set {
		t.Errorf("Jobs() = %+v", s.Jobs())
	}
	// Jobs 컬렉션이 없는 벤더는 405
	_, c2 := startLogin(t, "hpe-dl360gen11", nil)
	c2.must("POST", "/redfish/v1/Managers/1/Jobs", map[string]string{"TargetSettingsURI": "/redfish/v1/systems/1/bios/settings"}, 405)
	// 그 밖의 POST(Actions) 는 405
	c.must("POST", "/redfish/v1/Systems/System.Embedded.1/Actions/ComputerSystem.Reset", map[string]string{"ResetType": "On"}, 405)
}

// ---- 기록·로그 ----

func TestLogHitsCountsAnyLogRequest(t *testing.T) {
	s := newSrv(t, "dell-r660", nil)
	c := newCli(t, s.Start(), 5*time.Second)
	// 로그 경로는 인증 전이라도 센다 (401 이어도 mock 에 도달한 것)
	c.do("GET", "/redfish/v1/Systems/System.Embedded.1/LogServices", nil)
	c.login()
	for _, p := range []string{
		"/redfish/v1/Systems/System.Embedded.1/LogServices",
		"/redfish/v1/Systems/System.Embedded.1/LogServices/Sel/Entries",
		"/redfish/v1/Managers/iDRAC.Embedded.1/LogServices/Lclog/Entries/1",
		"/redfish/v1/managers/idrac.embedded.1/logservices/lclog",
	} {
		c.must("GET", p, nil, 200)
	}
	c.must("POST", "/redfish/v1/Systems/System.Embedded.1/LogServices/Sel/Actions/LogService.ClearLog", map[string]string{}, 405)
	c.must("DELETE", "/redfish/v1/Systems/System.Embedded.1/LogServices/Sel/Entries/1", nil, 405)
	if got := s.LogHits(); got != 7 {
		t.Errorf("LogHits = %d, 기대 7", got)
	}
	before := s.LogHits()
	c.must("GET", "/redfish/v1/Systems/System.Embedded.1/Bios", nil, 200)
	c.must("GET", "/redfish/v1/Managers/iDRAC.Embedded.1/Jobs", nil, 200)
	if s.LogHits() != before {
		t.Errorf("로그가 아닌 경로가 LogHits 에 잡힘")
	}
}

func TestCallsCountAndWrites(t *testing.T) {
	s, c := startLogin(t, "lenovo-sr650v3", nil)
	c.must("GET", "/redfish/v1", nil, 200)
	c.must("GET", "/redfish/v1/Systems/1/Bios", nil, 200)
	c.must("GET", "/redfish/v1/Systems/1/Bios/Pending", nil, 200)
	if s.Writes() != 0 {
		t.Errorf("읽기만 했는데 Writes = %d", s.Writes())
	}
	c.must("DELETE", "/redfish/v1/SessionService/Sessions/1", nil, 204)
	if s.Writes() != 0 {
		t.Errorf("세션 삭제는 쓰기가 아님: %d", s.Writes())
	}
	calls := s.Calls()
	want := []string{"POST /redfish/v1/SessionService/Sessions 201", "GET /redfish/v1 200", "GET /redfish/v1/Systems/1/Bios 200", "GET /redfish/v1/Systems/1/Bios/Pending 200", "DELETE /redfish/v1/SessionService/Sessions/1 204"}
	var got []string
	for _, cl := range calls {
		got = append(got, fmt.Sprintf("%s %s %d", cl.Method, cl.Path, cl.Status))
		if cl.Body != "" {
			t.Errorf("본문이 기록됨: %+v (세션 POST 에는 비밀번호가 있음)", cl)
		}
	}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("Calls =\n%v\n기대\n%v", got, want)
	}
	if s.Count("GET", "/redfish/v1/systems/1") != 2 || s.Count("", "/redfish/v1") != 5 || s.Count("POST", "/redfish/v1/Systems") != 0 {
		t.Errorf("Count: %d %d %d", s.Count("GET", "/redfish/v1/systems/1"), s.Count("", "/redfish/v1"), s.Count("POST", "/redfish/v1/Systems"))
	}
	c.token = ""
	c.must("PATCH", "/redfish/v1/Systems/1/Bios/Pending", patchBody("Processors_SNC", "Enable"), 401)
	if s.Writes() != 1 {
		t.Errorf("401 로 거부된 PATCH 도 도달했으므로 Writes 에 잡혀야 함: %d", s.Writes())
	}
}

// ---- 상태 변경 도우미 ----

func TestModifyAndSetBiosAttrAndRemove(t *testing.T) {
	s, c := startLogin(t, "hpe-dl360gen11", nil)
	if err := s.SetBiosAttr("ProcHyperthreading", "Disabled"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetBiosAttr("SubNumaClustering", nil); err != nil {
		t.Fatal(err)
	}
	a := c.attrs("/redfish/v1/systems/1/bios")
	if a["ProcHyperthreading"] != "Disabled" {
		t.Errorf("SetBiosAttr 반영 안 됨: %v", a["ProcHyperthreading"])
	}
	if _, ok := a["SubNumaClustering"]; ok {
		t.Errorf("속성 삭제 안 됨")
	}
	if err := s.Modify("/redfish/v1/Systems/1", func(m map[string]interface{}) { m["BiosVersion"] = "TEST" }); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(c.must("GET", "/redfish/v1/systems/1/", nil, 200)), `"BiosVersion":"TEST"`) {
		t.Error("Modify 반영 안 됨")
	}
	if s.Modify("/redfish/v1/없음", func(map[string]interface{}) {}) == nil {
		t.Error("없는 리소스 Modify 가 성공")
	}
	s.Remove("/redfish/v1/Systems/1/Bios")
	c.must("GET", "/redfish/v1/systems/1/bios", nil, 404)
}

// ---- 장애 주입 ----

func TestFaultStatusTimes(t *testing.T) {
	_, c := startLogin(t, "dell-r660", func(o *Options) {
		o.Fail = map[string]Fault{"/redfish/v1/Systems/": {Status: 503, Times: 2}} // 끝 슬래시는 무시
	})
	var got []int
	for i := 0; i < 4; i++ {
		st, _, _, _ := c.do("GET", "/redfish/v1/Systems", nil)
		got = append(got, st)
	}
	if fmt.Sprint(got) != "[503 503 200 200]" {
		t.Errorf("상태 = %v", got)
	}
}

func TestFaultAlways401OnSessionsAndMethodAndPrefix(t *testing.T) {
	s := newSrv(t, "dell-r660", nil)
	c := newCli(t, s.Start(), 5*time.Second)
	s.SetFault("POST /redfish/v1/SessionService/Sessions", Fault{Status: 401})
	for i := 0; i < 3; i++ {
		if st, _, _, _ := c.do("POST", "/redfish/v1/SessionService/Sessions", map[string]string{"UserName": tUser, "Password": tPass}); st != 401 {
			t.Fatalf("항상 401 = %d", st)
		}
	}
	if ss := s.Sessions(); ss.Created != 0 {
		t.Errorf("세션이 만들어짐: %+v", ss)
	}
	s.ClearFaults()
	c.login()
	// 메서드 한정: GET 은 정상, PATCH 만 500
	s.SetFault("PATCH /redfish/v1/Systems/System.Embedded.1/Bios/Settings", Fault{Status: 500})
	c.must("GET", "/redfish/v1/Systems/System.Embedded.1/Bios/Settings", nil, 200)
	c.must("PATCH", "/redfish/v1/Systems/System.Embedded.1/Bios/Settings", patchBody("LogicalProc", "Disabled"), 500)
	if len(s.Pending()) != 0 {
		t.Error("장애 응답인데 pending 에 반영됨")
	}
	// 접두 규칙 + 더 구체적인 규칙 우선
	s.ClearFaults()
	s.SetFault("/redfish/v1/Systems/*", Fault{Status: 502})
	s.SetFault("/redfish/v1/Systems/System.Embedded.1/Bios", Fault{Status: 504})
	c.must("GET", "/redfish/v1/Systems", nil, 200) // "/systems/" 접두에 안 걸림
	c.must("GET", "/redfish/v1/Systems/System.Embedded.1", nil, 502)
	c.must("GET", "/redfish/v1/Systems/System.Embedded.1/Bios", nil, 504)
}

func TestFaultDelayTimeoutAndCloseDoesNotHang(t *testing.T) {
	s := newSrv(t, "dell-r660", nil)
	c := newCli(t, s.Start(), 5*time.Second)
	c.login()
	s.SetFault("/redfish/v1/Systems", Fault{Delay: 30 * time.Second})
	fast := newCli(t, s.URL(), 200*time.Millisecond)
	fast.token = c.token
	start := time.Now()
	if _, _, _, err := fast.do("GET", "/redfish/v1/Systems", nil); err == nil {
		t.Fatal("타임아웃이 나야 함")
	}
	if time.Since(start) > 3*time.Second {
		t.Errorf("타임아웃이 너무 늦음: %v", time.Since(start))
	}
	// 지연 중인 요청이 있어도 Close 가 막히지 않는다
	go func() { _, _, _, _ = c.do("GET", "/redfish/v1/Systems", nil) }()
	time.Sleep(100 * time.Millisecond)
	done := make(chan struct{})
	go func() { s.Close(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Close 가 지연 요청 때문에 막힘")
	}
	s.Close() // 두 번 불러도 된다
}

func TestFaultDropConnection(t *testing.T) {
	s, c := startLogin(t, "dell-r660", nil)
	s.SetFault("/redfish/v1/Managers", Fault{Drop: true})
	if _, _, _, err := c.do("GET", "/redfish/v1/Managers", nil); err == nil {
		t.Fatal("연결이 끊겨야 함")
	}
	s.ClearFaults()
	c.must("GET", "/redfish/v1/Managers", nil, 200)
	var dropped bool
	for _, cl := range s.Calls() {
		if cl.Path == "/redfish/v1/Managers" && cl.Status == 0 {
			dropped = true
		}
	}
	if !dropped {
		t.Error("끊긴 요청이 Status 0 으로 기록돼야 함")
	}
}

// ---- 실행 방식 ----

func TestListenTLSAndServe(t *testing.T) {
	s := newSrv(t, "cisco-c220m7", nil)
	addr, err := s.ListenTLS("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	c := newCli(t, "https://"+addr, 5*time.Second)
	c.login()
	c.must("GET", "/redfish/v1/Systems/MOCK-C220M7/Bios", nil, 200)
	if _, err := s.ListenTLS("256.0.0.1:1"); err == nil {
		t.Error("잘못된 주소가 성공")
	}
	s.Close()
	if _, _, _, err := c.do("GET", "/redfish/v1", nil); err == nil {
		t.Error("Close 후에도 응답함")
	}

	s2 := newSrv(t, "cisco-c220m7", nil)
	errc := make(chan error, 1)
	go func() { errc <- s2.ListenAndServeTLS("127.0.0.1:0") }()
	for i := 0; i < 200; i++ { // 서비스가 열릴 때까지
		s2.mu.Lock()
		up := s2.hs != nil
		s2.mu.Unlock()
		if up {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	time.Sleep(50 * time.Millisecond)
	s2.Close()
	select {
	case err := <-errc:
		if err != nil {
			t.Errorf("정상 종료인데 오류: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("ListenAndServeTLS 가 Close 후에도 안 돌아옴")
	}
}

func TestLogfHasNoSecrets(t *testing.T) {
	var mu sync.Mutex
	var lines []string
	s := newSrv(t, "dell-r660", func(o *Options) {
		o.Logf = func(f string, a ...interface{}) {
			mu.Lock()
			lines = append(lines, fmt.Sprintf(f, a...))
			mu.Unlock()
		}
	})
	c := newCli(t, s.Start(), 5*time.Second)
	c.login()
	c.must("GET", "/redfish/v1", nil, 200)
	mu.Lock()
	defer mu.Unlock()
	joined := strings.Join(lines, "\n")
	if len(lines) != 2 || strings.Contains(joined, tPass) || strings.Contains(joined, c.token) {
		t.Errorf("Logf = %q", lines)
	}
}

// -race 로 돌렸을 때 동시 접근 오류가 없는지 본다.
func TestConcurrentClients(t *testing.T) {
	s := newSrv(t, "dell-r660", nil)
	base := s.Start()
	const n = 16
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			c := newCli(t, base, 10*time.Second)
			loc := c.login()
			for j := 0; j < 5; j++ {
				c.must("GET", "/redfish/v1/Systems/System.Embedded.1/Bios", nil, 200)
				c.must("PATCH", "/redfish/v1/Systems/System.Embedded.1/Bios/Settings", patchBody("AssetTag", fmt.Sprintf("T%d-%d", i, j)), 200)
				c.must("GET", "/redfish/v1/Systems/System.Embedded.1/Bios/Settings", nil, 200)
			}
			c.must("POST", "/redfish/v1/Managers/iDRAC.Embedded.1/Jobs", map[string]string{"TargetSettingsURI": "/redfish/v1/Systems/System.Embedded.1/Bios/Settings"}, 202)
			_ = s.SetBiosAttr("ProcX2Apic", "Disabled")
			_ = s.Calls()
			c.must("DELETE", loc, nil, 204)
		}()
	}
	wg.Wait()
	if ss := s.Sessions(); ss.Created != n || ss.Deleted != n {
		t.Errorf("Sessions = %+v", ss)
	}
	if len(s.Jobs()) != n {
		t.Errorf("Jobs = %d", len(s.Jobs()))
	}
	ids := map[string]bool{}
	for _, j := range s.Jobs() {
		ids[j.ID] = true
	}
	var keys []string
	for k := range ids {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	if len(ids) != n || keys[0] != "JID_000000000001" || keys[n-1] != fmt.Sprintf("JID_%012d", n) {
		t.Errorf("Job ID 가 겹치거나 비연속: %v", keys)
	}
}
