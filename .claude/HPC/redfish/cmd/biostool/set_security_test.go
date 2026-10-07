package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"biostool/internal/mockbmc"
)

// 단계 6 보안 리뷰(BIOS Pending 쓰기 경로) 결함의 회귀 시험. 리뷰 재현 시험을 정리해 옮긴 것이다.
// 실제 BMC 는 쓰지 않고 httptest·internal/mockbmc 만 쓴다.

// secClient 는 h 로 응답하는 TLS 서버에 붙은 쓰기 모드 Client 입니다.
func secClient(t *testing.T, h http.HandlerFunc) *Client {
	t.Helper()
	srv := httptest.NewTLSServer(h)
	t.Cleanup(srv.Close)
	c := NewClient(ClientOpts{BaseURL: srv.URL, User: "u", Pass: "p", Insecure: true, Mode: ModeSet, Gap: -1})
	t.Cleanup(func() { c.Close() })
	return c
}

// itemResultOf 는 점검 결과에서 속성 하나의 항목을 찾습니다.
func itemResultOf(t *testing.T, h *hostCheck, attr string) itemResult {
	t.Helper()
	for _, it := range h.Items {
		if it.Attr == attr {
			return it
		}
	}
	t.Fatalf("속성 %s 항목 없음: %+v", attr, h.Items)
	return itemResult{}
}

// ---- R1: 2xx 로 받아들여진 뒤 응답을 끝까지 못 받은 쓰기는 REJECTED 가 아니라 SET_ERROR ----

func TestSecR1_Write2xxReadErrorIsSetError(t *testing.T) {
	var patches int32
	c := secClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case "POST":
			w.WriteHeader(404) // 세션 미지원 → Basic
		case "PATCH":
			atomic.AddInt32(&patches, 1)
			w.Header().Set("Content-Length", "1000")
			w.WriteHeader(200)
			w.Write([]byte(`{"@Message`)) // 본문 도중 연결 끊김
		}
	})
	st, _, err := c.Patch("/redfish/v1/Systems/1/Bios/Settings", json.RawMessage(`{"Attributes":{"LogicalProc":"Enabled"}}`))
	s, why := writeErrStatus(st, err)
	if st != 200 || err == nil || s != StatusSetError || !strings.Contains(why, "반영됐을 수 있음") || !strings.Contains(why, "재점검") {
		t.Errorf("PATCH 200 + 본문 끊김: st=%d err=%v → %s (%s), 기대 SET_ERROR", st, err, s, why)
	}
	if patches != 1 {
		t.Errorf("PATCH %d회, 기대 1 (재시도 금지)", patches)
	}

	// (code, err) 조합 표: 2xx 에 오류가 붙으면 결과 불명, 4xx 는 그대로 거부.
	for _, c := range []struct {
		code int
		err  error
		want string
	}{
		{200, &RFError{Status: StatusHTTPError, HTTP: 200, Detail: "응답 본문이 8MB 를 넘습니다"}, StatusSetError},
		{202, &RFError{Status: StatusTimeout}, StatusSetError},
		{204, &RFError{Status: StatusUnreachable}, StatusSetError},
		{400, &RFError{Status: StatusHTTPError, HTTP: 400, Detail: "bad"}, StatusRejected},
		{409, &RFError{Status: StatusHTTPError, HTTP: 409}, StatusRejected},
		{0, &RFError{Status: StatusUnreachable}, StatusSetError},
	} {
		if s, why := writeErrStatus(c.code, c.err); s != c.want {
			t.Errorf("writeErrStatus(%d, %v) = %s (%s), 기대 %s", c.code, c.err, s, why, c.want)
		}
	}

	// Dell Job POST 도 같다: 2xx 후 본문 끊김은 REJECTED 가 아니라 SET_ERROR(결과 불명)로 보고된다.
	jc := secClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == "POST" && strings.HasSuffix(strings.ToLower(r.URL.Path), "/sessions"):
			w.WriteHeader(404)
		case r.Method == "POST":
			w.Header().Set("Content-Length", "1000")
			w.WriteHeader(200)
			w.Write([]byte(`{"Id`))
		case r.Method == "GET":
			w.Write([]byte(`{"Members":[]}`))
		}
	})
	p := &setPlan{JobPath: dellJobs, JobBody: []byte(`{"TargetSettingsURI":"` + dellSettings + `"}`)}
	if _, errText := dellJob(jc, p, 200, []byte(`{}`), map[string]bool{}); !strings.Contains(errText, StatusSetError) || strings.Contains(errText, StatusRejected) {
		t.Errorf("Job POST 200 + 본문 끊김: %q", errText)
	}
}

// ---- R2·R4(a): Dell Job 자동 생성 인정은 엄격하게 ----

func TestSecR2_DellJobAutoCreateStrict(t *testing.T) {
	const old = dellJobs + "/JID_OLD"
	type job struct{ typ, state string }
	cases := []struct {
		name     string
		status   int
		body     string
		newJobs  map[string]job // PATCH 뒤 새로 보이는 멤버 (원래 대소문자 ID)
		hidden   map[string]job // 목록에는 없고 GET 으로만 보이는 Job (본문 URI 시험용)
		wantPost bool
	}{
		{"무관한 새 Job(Export)", 200, `{}`, map[string]job{dellJobs + "/JID_EXPORT": {"ExportConfiguration", "Scheduled"}}, nil, true},
		{"끝난 BIOS Job", 200, `{}`, map[string]job{dellJobs + "/JID_DONE": {"BIOSConfiguration", "Completed"}}, nil, true},
		{"본문에 JID_ 문자열만", 200, `{"@Message.ExtendedInfo":[{"Message":"Job JID_123 is already scheduled for another component"}]}`, nil, nil, true},
		{"본문의 Job URI 가 Jobs 밖", 200, `{"@odata.id":"/redfish/v1/Managers/Other.1/Jobs/JID_9"}`, nil, map[string]job{"/redfish/v1/Managers/Other.1/Jobs/JID_9": {"BIOSConfiguration", "Scheduled"}}, true},
		{"본문의 Job URI 가 무관한 Job", 200, `{"Location":"` + dellJobs + `/JID_X"}`, nil, map[string]job{dellJobs + "/JID_X": {"Export", "Scheduled"}}, true},
		{"새 멤버가 예약된 BIOS Job (대소문자 무시)", 200, `{}`, map[string]job{dellJobs + "/JID_New": {"biosconfiguration", "scheduled"}}, nil, false},
		{"본문의 Job URI 가 예약된 BIOS Job", 200, `{"Location":"` + dellJobs + `/JID_Body"}`, nil, map[string]job{dellJobs + "/JID_Body": {"BIOSConfiguration", "New"}}, false},
		{"PATCH 202", 202, `{}`, nil, nil, false},
	}
	for _, cs := range cases {
		cs := cs
		t.Run(cs.name, func(t *testing.T) {
			var mu sync.Mutex
			var posts int
			var gets []string
			c := secClient(t, func(w http.ResponseWriter, r *http.Request) {
				mu.Lock()
				defer mu.Unlock()
				switch {
				case r.Method == "POST" && strings.HasSuffix(strings.ToLower(r.URL.Path), "/sessions"):
					w.WriteHeader(404)
				case r.Method == "POST":
					posts++
					w.WriteHeader(202)
				case r.Method == "GET":
					gets = append(gets, r.URL.Path)
					if r.URL.Path == dellJobs {
						ms := []map[string]string{{"@odata.id": old}}
						for id := range cs.newJobs {
							ms = append(ms, map[string]string{"@odata.id": id})
						}
						json.NewEncoder(w).Encode(map[string]interface{}{"Members": ms})
						return
					}
					j, ok := cs.newJobs[r.URL.Path]
					if !ok {
						j, ok = cs.hidden[r.URL.Path]
					}
					if ok {
						json.NewEncoder(w).Encode(map[string]string{"JobType": j.typ, "JobState": j.state})
						return
					}
					w.WriteHeader(404)
				}
			})
			p := &setPlan{JobPath: dellJobs, JobBody: []byte(`{"TargetSettingsURI":"` + dellSettings + `"}`)}
			before := map[string]bool{strings.ToLower(old): true}
			note, errText := dellJob(c, p, cs.status, []byte(cs.body), before)
			mu.Lock()
			defer mu.Unlock()
			if (posts == 1) != cs.wantPost || posts > 1 || errText != "" {
				t.Errorf("Job POST %d회 (기대 POST=%v) note=%q err=%q", posts, cs.wantPost, note, errText)
			}
			for _, g := range gets {
				if strings.Contains(g, "Other.1") {
					t.Errorf("Jobs 밖 URI 를 GET 함: %s", g)
				}
				if strings.EqualFold(g, dellJobs+"/JID_New") && g != dellJobs+"/JID_New" {
					t.Errorf("멤버 GET 이 원래 대소문자를 쓰지 않음: %s", g)
				}
			}
		})
	}
}

// ---- R3: 레지스트리 조회 실패 캐시가 같은 기종 다른 호스트의 사전검증을 끄지 않는다 ----

func TestSecR3_RegistryFailureNotCached(t *testing.T) {
	const regPath = "/redfish/v1/Registries/BiosAttributeRegistry.v1_0_0.json"
	s1 := startMockTree(t, "dell-r660", nil)
	s2 := startMockTree(t, "dell-r660", nil)
	for _, s := range []*mockbmc.Server{s1, s2} {
		must(t, s.SetBiosAttr("LogicalProc", "Disabled"))
	}
	must(t, s2.Modify(regPath, func(m map[string]interface{}) {
		for _, a := range m["RegistryEntries"].(map[string]interface{})["Attributes"].([]interface{}) {
			if am := a.(map[string]interface{}); am["AttributeName"] == "LogicalProc" {
				am["ReadOnly"] = true
			}
		}
	}))
	rc := checkRCFor(t, 1, "", s1.URL(), s2.URL()) // 동시 1: s1 이 먼저 실패해 캐시에 남는다
	run := runCheckP(t, rc, setProf(t, setProfText))
	s1.SetFault("GET "+regPath, mockbmc.Fault{Status: 404})
	b2 := s2.Count("PATCH", "/")
	applyY(t, rc, run)
	res := applyResults(t, run)
	wantStatus(t, res, s2, "LogicalProc", StatusSkippedReadOnly)
	if d := s2.Count("PATCH", "/") - b2; d != 0 {
		t.Errorf("s2 의 ReadOnly 속성이 사전검증 없이 PATCH 됨 (%d회)", d)
	}
	if n := s2.Count("GET", regPath); n < 1 {
		t.Errorf("s2 가 레지스트리를 다시 받지 않음")
	}
	// 실패한 호스트는 기존 보수 규칙대로 보내되, 결과에 사유를 남긴다.
	if r := res[hostKey(s1)+"|LogicalProc"]; r.status != StatusApplied || !strings.Contains(r.detail, "레지스트리를 읽지 못함") || !strings.Contains(r.detail, "404") {
		t.Errorf("s1 = %+v (레지스트리 실패 사유 기록 기대)", r)
	}
}

// ---- R4: Dell 의 Job 없는 Pending ----

func TestSecR4_DellPendingWithoutJob(t *testing.T) {
	prof := setProf(t, setProfText)
	s := startMockTree(t, "dell-r660", nil)
	must(t, s.SetBiosAttr("LogicalProc", "Disabled"))
	rc := checkRCFor(t, 1, "", s.URL())
	run := runCheckP(t, rc, prof)

	// 1차: PATCH 는 됐으나 Job POST 실패 → APPLY_UNCONFIRMED + Dell Job 경고
	s.SetFault("POST "+dellJobs, mockbmc.Fault{Status: 500})
	out := applyY(t, rc, run)
	wantStatus(t, applyResults(t, run), s, "LogicalProc", StatusApplyUnconfirmed)
	if !strings.Contains(out, "iDRAC Job Queue 에 BIOS 설정 Job 이 없으면 반영되지 않을 수 있음") {
		t.Errorf("APPLY_UNCONFIRMED 안내에 Dell Job 경고 없음:\n%s", out)
	}
	s.ClearFaults()

	// 재점검: Pending 은 기대값이나 Job 이 없음 → PENDING_NO_JOB, 설정 대상
	run2 := runCheckP(t, rc, prof)
	if it := itemResultOf(t, run2.Hosts[0], "LogicalProc"); it.Result != StatusPendingNoJob {
		t.Fatalf("재점검 LogicalProc = %s (%s), 기대 PENDING_NO_JOB", it.Result, it.Reason)
	}
	if f := run2.failItems(); len(f) != 1 || f[0].Attr != "LogicalProc" {
		t.Fatalf("PENDING_NO_JOB 이 설정 대상이 아님: %+v", f)
	}
	var rep bytes.Buffer
	writeReport(&rep, run2, testROpts)
	if !strings.Contains(rep.String(), StatusPendingNoJob) || strings.Contains(rep.String(), "(재부팅 대기)") {
		t.Errorf("리포트:\n%s", rep.String())
	}
	// dry-run 도 같은 PATCH + Job 을 보여 준다 (쓰기 없음)
	w0 := s.Writes()
	var dry bytes.Buffer
	must(t, doDryRun(rc, run2, &dry))
	real, _, job := dryBodies(dry.String())
	if !strings.Contains(real[hostKey(s)], `"LogicalProc":"Enabled"`) || job[hostKey(s)] == "" || s.Writes() != w0 {
		t.Errorf("dry-run: real=%v job=%v writes %d→%d\n%s", real, job, w0, s.Writes(), dry.String())
	}

	// Y: 같은 값 PATCH 1회 + Job 1회 → APPLIED
	before := snapOf(s)
	applyY(t, rc, run2)
	now := snapOf(s)
	wantStatus(t, applyResults(t, run2), s, "LogicalProc", StatusApplied)
	if now.patches-before.patches != 1 || now.jobs-before.jobs != 1 || len(s.Jobs()) != 1 || s.Jobs()[0].Target != dellSettings {
		t.Errorf("PATCH %d / Job POST %d / Jobs %+v, 기대 1/1/1", now.patches-before.patches, now.jobs-before.jobs, s.Jobs())
	}
	if b := patchBodies(s); len(b) == 0 || b[len(b)-1] != real[hostKey(s)] {
		t.Errorf("실제 본문 %q != dry-run 본문 %q", b, real[hostKey(s)])
	}
	assertSetSafe(t, s)

	// 그 뒤 재점검: Job 이 생겼으므로 PENDING_OK
	run3 := runCheckP(t, rc, prof)
	if it := itemResultOf(t, run3.Hosts[0], "LogicalProc"); it.Result != StatusPendingOK {
		t.Errorf("Job 생성 뒤 재점검 = %s, 기대 PENDING_OK", it.Result)
	}
}

func TestSecR4_PendingNoJobVariants(t *testing.T) {
	prof := setProf(t, setProfText)
	mk := func(mod func(s *mockbmc.Server)) *mockbmc.Server {
		s := startMockTree(t, "dell-r660", nil)
		must(t, s.SetBiosAttr("LogicalProc", "Disabled"))
		setPending(t, s, dellSettings, map[string]interface{}{"LogicalProc": "Enabled"})
		if mod != nil {
			mod(s)
		}
		return s
	}
	noJob := mk(nil)
	otherJob := mk(func(s *mockbmc.Server) { s.AddJob(dellJobs, "", "ExportConfiguration", "Scheduled") })
	doneJob := mk(func(s *mockbmc.Server) { s.AddJob(dellJobs, dellSettings, "BIOSConfiguration", "Completed") })
	withJob := mk(func(s *mockbmc.Server) { s.AddJob(dellJobs, dellSettings, "", "") })
	unknown := mk(func(s *mockbmc.Server) { s.SetFault("GET "+dellJobs, mockbmc.Fault{Status: 500}) })
	foreign := mk(func(s *mockbmc.Server) {
		setPending(t, s, dellSettings, map[string]interface{}{"ProcCStates": "Disabled"})
	})
	unv := startMockTree(t, "dell-r660", nil) // verified=N 행(SubNumaCluster)의 Job 없는 Pending
	must(t, unv.SetBiosAttr("SubNumaCluster", "Enabled"))
	setPending(t, unv, dellSettings, map[string]interface{}{"SubNumaCluster": "Disabled"})

	servers := []*mockbmc.Server{noJob, otherJob, doneJob, withJob, unknown, foreign, unv}
	var urls []string
	for _, s := range servers {
		urls = append(urls, s.URL())
	}
	rc := checkRCFor(t, 2, "", urls...)
	run := runCheckP(t, rc, prof)
	want := []struct {
		attr, result string
		settable     bool
	}{
		{"LogicalProc", StatusPendingNoJob, true},
		{"LogicalProc", StatusPendingNoJob, true},
		{"LogicalProc", StatusPendingNoJob, true},
		{"LogicalProc", StatusPendingOK, false},
		{"LogicalProc", StatusPendingOK, false}, // Job 확인 불가 → 쓰지 않고 경고만
		{"LogicalProc", StatusPendingNoJob, false},
		{"SubNumaCluster", StatusPendingNoJob, false},
	}
	for i, w := range want {
		h := run.Hosts[i]
		it := itemResultOf(t, h, w.attr)
		if it.Result != w.result || h.settable(it) != w.settable {
			t.Errorf("호스트 %d %s = %s settable=%v (%s), 기대 %s settable=%v", i, w.attr, it.Result, h.settable(it), it.Reason, w.result, w.settable)
		}
	}
	if !itemResultOf(t, run.Hosts[4], "LogicalProc").JobUnknown {
		t.Error("Job 확인 불가 표시가 없음")
	}
	var rep bytes.Buffer
	writeReport(&rep, run, testROpts)
	for _, s := range []string{"[설정 불가]", "PENDING_NO_JOB", "Dell 은 iDRAC Job Queue 에 BIOS 설정 Job 이 없으면 재부팅해도 반영되지 않을 수 있음 — 확인 필요"} {
		if !strings.Contains(rep.String(), s) {
			t.Errorf("리포트에 %q 없음:\n%s", s, rep.String())
		}
	}

	// 점검 뒤 누군가 BIOS 설정 Job 을 만들었으면 set 이 다시 확인해 쓰지 않는다 (ALREADY_OK).
	otherJob.AddJob(dellJobs, dellSettings, "", "")
	// Job 목록 조회가 실패하면 쓰지 않는다.
	doneJob.SetFault("GET "+dellJobs, mockbmc.Fault{Status: 500})
	ws := make([]int, len(servers))
	for i, s := range servers {
		ws[i] = s.Writes()
	}
	applyY(t, rc, run)
	res := applyResults(t, run)
	wantStatus(t, res, noJob, "LogicalProc", StatusApplied)
	wantStatus(t, res, otherJob, "LogicalProc", StatusAlreadyOK)
	wantStatus(t, res, doneJob, "LogicalProc", StatusAlreadyOK)
	if r := res[hostKey(doneJob)+"|LogicalProc"]; !strings.Contains(r.detail, "확인하지 못해 쓰지 않음") {
		t.Errorf("Job 확인 불가 사유: %q", r.detail)
	}
	for i, s := range servers {
		want := 0
		if s == noJob {
			want = 2 // PATCH + Job
		}
		if d := s.Writes() - ws[i]; d != want {
			t.Errorf("호스트 %d 쓰기 %d건, 기대 %d", i, d, want)
		}
		assertSetSafe(t, s)
	}
}

// ---- R5: 같은 BMC 판정은 IP 정규화 + 기본 포트 443 ----

func TestSecR5_DedupIPForms(t *testing.T) {
	var ts []Target
	for _, in := range []string{"2001:db8::1", "2001:db8:0:0:0:0:0:1", "192.0.2.1", "::ffff:192.0.2.1", "[2001:DB8::1]:443", "192.0.2.1:8443"} {
		r, err := resolveTargets([]string{in}, "/nonexistent")
		must(t, err)
		ts = append(ts, r[0])
	}
	var fs []failItem
	for _, x := range ts {
		fs = append(fs, failItem{Target: x, Attr: "x"})
	}
	hs := applyHosts(fs)
	wantDup := []string{"", "2001:db8::1", "", "192.0.2.1", "2001:db8::1", ""}
	for i, h := range hs {
		if h.DupOf != wantDup[i] {
			t.Errorf("%s (IP=%s Port=%s) DupOf=%q, 기대 %q", h.Target.Input, h.Target.IP, h.Target.Port, h.DupOf, wantDup[i])
		}
	}
}

// ---- R6: dry-run 도 같은 중복 판정을 써서 PATCH 건수가 실제 적용과 같다 ----

func TestSecR6_DryRunDuplicates(t *testing.T) {
	s := startMockTree(t, "dell-r660", nil)
	must(t, s.SetBiosAttr("LogicalProc", "Disabled"))
	port := s.URL()[strings.LastIndex(s.URL(), ":")+1:]
	rc := checkRCFor(t, 2, "127.0.0.1 dupe-m\n", s.URL(), "dupe-m:"+port)
	run := runCheckP(t, rc, setProf(t, setProfText))
	var out bytes.Buffer
	must(t, doDryRun(rc, run, &out))
	if n := strings.Count(out.String(), "  PATCH "); n != 1 || !strings.Contains(out.String(), StatusDuplicateTarget) {
		t.Errorf("dry-run PATCH %d건 (기대 1, DUPLICATE_TARGET 표시):\n%s", n, out.String())
	}
	before := snapOf(s)
	applyY(t, rc, run)
	if d := snapOf(s).patches - before.patches; d != 1 {
		t.Errorf("실제 적용 PATCH %d건, 기대 1", d)
	}
}

// ---- R7: 이미 같은 값(정수 정규화 포함)이면 FAIL 도 PATCH 도 없다 ----

func TestSecR7_IntegerAlreadyOK(t *testing.T) {
	s := startMockTree(t, "lenovo-sr650v3", nil)
	must(t, s.SetBiosAttr("SystemRecovery_POSTWatchdogTimerValue", 5))
	prof := setProf(t, strings.Replace(setProfText, "SystemRecovery_POSTWatchdogTimerValue,5,Y", "SystemRecovery_POSTWatchdogTimerValue,05,Y", 1))
	rc := checkRCFor(t, 1, "", s.URL())
	run := runCheckP(t, rc, prof)
	if it := itemResultOf(t, run.Hosts[0], "SystemRecovery_POSTWatchdogTimerValue"); it.Result != StatusOK {
		t.Errorf("점검: 현재값 5, 기대 05 → %s, 기대 OK", it.Result)
	}
	// 점검을 우회한 입력이어도 set 은 같은 값을 다시 보내지 않는다.
	h := run.Hosts[0]
	applyDirect(t, rc, run, []failItem{{Target: h.Target, Info: h.Info, Std: "watchdog_value", Attr: "SystemRecovery_POSTWatchdogTimerValue", Expected: "05", Current: "5"}})
	wantStatus(t, applyResults(t, run), s, "SystemRecovery_POSTWatchdogTimerValue", StatusAlreadyOK)
	if s.Count("PATCH", "/") != 0 {
		t.Errorf("이미 같은 값인데 PATCH %d회", s.Count("PATCH", "/"))
	}

	for _, c := range []struct {
		allowed []string
		v       string
		want    bool
	}{{[]string{"05"}, "5", true}, {[]string{"5"}, " 05 ", true}, {[]string{"-1"}, "-01", true}, {[]string{"5"}, "5.0", false}, {[]string{"Enabled"}, "enabled", false}} {
		if got := matchAny(c.allowed, c.v); got != c.want {
			t.Errorf("matchAny(%v, %q) = %v", c.allowed, c.v, got)
		}
	}
	// Enumeration 은 문자열 그대로 보내므로 정수 정규화로 통과시키지 않는다.
	reg := &attrRegistry{byName: map[string]*regAttr{"Mode": {Name: "Mode", Type: "Enumeration", Values: []string{"1", "2"}}}}
	if _, st, _ := typedValue("01", reg, "Mode", nil); st != StatusInvalidValue {
		t.Errorf("Enumeration 허용값 1 에 01 → %q, 기대 INVALID_VALUE", st)
	}
}

// ---- R8: bmcMessage 는 JSON 이스케이프를 풀지 않는다 (부분 이스케이프 비밀번호 복원 방지) ----

func TestSecR8_BMCMessageNoUnescape(t *testing.T) {
	c := NewClient(ClientOpts{BaseURL: "https://192.0.2.1", User: "admin", Pass: "Secret1!", Mode: ModeSet})
	esc := func(hex string) string { return string(rune(0x5c)) + "u" + hex } // JSON 의 \uXXXX
	for _, body := range []string{
		`{"error":{"message":"value Secr` + esc("0065") + `t1! rejected"}}`, // 일부만 이스케이프 → 마스킹을 피함
		`{"error":{"message":"value Secret1! rejected"}}`,                   // 평문 → rfErr 가 먼저 지움
		`{"error":{"message":"value ` + esc("0053") + esc("0065") + esc("0063") + `ret1! rejected"}}`,
	} {
		if !strings.Contains(body, "Secret1!") && !strings.Contains(body, string(rune(0x5c))+"u") {
			t.Fatalf("시험 전제: 이스케이프가 본문에 있어야 함 %q", body)
		}
		err := c.rfErr(StatusHTTPError, 400, body)
		s, why := writeErrStatus(400, err)
		if s != StatusRejected || strings.Contains(why, "Secret1!") || strings.Contains(err.Detail, "Secret1!") {
			t.Errorf("본문 %s → %s %q (비밀번호가 평문으로 남음)", body, s, why)
		}
	}
	if got := bmcMessage(c.rfErr(StatusHTTPError, 400, `{"error":{"message":"pw Secret1! x"}}`)); !strings.Contains(got, "pw *** x") {
		t.Errorf("마스킹 뒤 메시지 = %q", got)
	}
}

// ---- W: verified=Y 는 모델을 지정한 행에만 ----

func TestSecW_WildcardVerifiedRejected(t *testing.T) {
	_, err := parseProfile(tabs("vendor@model@std_name@attribute@value@verified\n# c\nDELL@*@hyper_threading@LogicalProc@Enabled@Y\n"), "VM.tsv")
	if err == nil || !strings.Contains(err.Error(), "VM.tsv:3:") || !strings.Contains(err.Error(), "verified=Y 는 모델을 지정한 행에만 쓸 수 있습니다 — * 행은 점검 비교용") {
		t.Errorf("* 행 verified=Y 오류: %v", err)
	}
	if _, err := parseProfile(tabs("vendor@model@std_name@attribute@value@verified\nDELL@*@hyper_threading@LogicalProc@Enabled@N\n"), "VM.tsv"); err != nil {
		t.Errorf("* 행 verified=N 은 허용: %v", err)
	}
	for _, f := range []string{profFixture, "../../profiles/VM.tsv"} {
		p, err := loadProfile(f, "VM")
		must(t, err)
		for _, r := range p.Rows {
			if r.modelKey == "*" && r.Verified {
				t.Errorf("%s:%d * 행이 verified=Y", f, r.Line)
			}
		}
	}

	// 방어 중첩: 파서를 우회해 * 행이 verified=Y 가 되어도 set 은 쓰지 않는다.
	prof := setProf(t, "vendor,model,std_name,attribute,value,verified\nDELL,*,hyper_threading,LogicalProc,Enabled,N\n")
	prof.Rows[0].Verified = true
	s := startMockTree(t, "dell-r660", nil)
	must(t, s.SetBiosAttr("LogicalProc", "Disabled"))
	rc := checkRCFor(t, 1, "", s.URL())
	run := runCheckP(t, rc, prof)
	if n := len(run.failItems()); n != 1 {
		t.Fatalf("시험 준비: FAIL %d건", n)
	}
	sess := s.Sessions()
	applyY(t, rc, run)
	wantStatus(t, applyResults(t, run), s, "LogicalProc", StatusUnverified)
	if s.Writes() != 0 || s.Sessions() != sess {
		t.Errorf("* 행으로 쓰기/접속함: Writes=%d 세션 %+v", s.Writes(), s.Sessions())
	}

	// BMC 모델이 비어 있으면 (정확 행이 있어도) 쓰지 않는다.
	s2 := startMockTree(t, "dell-r660", nil)
	must(t, s2.SetBiosAttr("LogicalProc", "Disabled"))
	rc2 := checkRCFor(t, 1, "", s2.URL())
	run2 := runCheckP(t, rc2, setProf(t, setProfText))
	h := run2.Hosts[0]
	info := *h.Info
	info.Model = ""
	applyDirect(t, rc2, run2, []failItem{{Target: h.Target, Info: &info, Std: "hyper_threading", Attr: "LogicalProc", Expected: "Enabled", Current: "Disabled"}})
	if r := applyResults(t, run2)[hostKey(s2)+"|LogicalProc"]; r.status != StatusUnverified || !strings.Contains(r.detail, "모델명이 비어") {
		t.Errorf("빈 모델 = %+v", r)
	}
	if s2.Writes() != 0 {
		t.Errorf("빈 모델인데 쓰기 %d건", s2.Writes())
	}
}

// ---- Y/N: 비대화형 표준입력은 -stdin-ok 없이는 N ----

func TestSecYN_NonInteractiveStdin(t *testing.T) {
	prof := setProf(t, setProfText)
	yes := func(t *testing.T) *os.File {
		p := filepath.Join(t.TempDir(), "answer")
		must(t, os.WriteFile(p, []byte("Y\n"), 0o600))
		f, err := os.Open(p)
		must(t, err)
		t.Cleanup(func() { f.Close() })
		return f
	}
	prep := func() (*mockbmc.Server, *runContext, *checkRun) {
		s := startMockTree(t, "dell-r660", nil)
		must(t, s.SetBiosAttr("LogicalProc", "Disabled"))
		rc := checkRCFor(t, 1, "", s.URL())
		return s, rc, runCheckP(t, rc, prof)
	}

	// 파일(비대화형)의 Y: 읽지 않고 N
	s, rc, run := prep()
	var out bytes.Buffer
	must(t, confirmApplyStdin(rc, run, yes(t), false, &out, false, testROpts))
	if s.Writes() != 0 || !strings.Contains(out.String(), msgNonInteractive) || strings.Contains(out.String(), "(Y/N)") {
		t.Errorf("비대화형 Y 가 적용됨 또는 안내 없음: Writes=%d\n%s", s.Writes(), out.String())
	}
	// -stdin-ok: 자동화 예외로 적용
	s, rc, run = prep()
	out.Reset()
	must(t, confirmApplyStdin(rc, run, yes(t), true, &out, false, testROpts))
	if s.Writes() != 2 || !strings.Contains(out.String(), "APPLIED") {
		t.Errorf("-stdin-ok 인데 적용 안 됨: Writes=%d\n%s", s.Writes(), out.String())
	}
	// 터미널이면 Y 를 받는다
	old := stdinIsTerminal
	stdinIsTerminal = func(*os.File) bool { return true }
	t.Cleanup(func() { stdinIsTerminal = old })
	s, rc, run = prep()
	out.Reset()
	must(t, confirmApplyStdin(rc, run, yes(t), false, &out, false, testROpts))
	if s.Writes() != 2 {
		t.Errorf("터미널 Y 인데 적용 안 됨: Writes=%d\n%s", s.Writes(), out.String())
	}
	stdinIsTerminal = old

	// cmdCheck 전체 경로: 표준입력을 Y 파일로 바꿔도 플래그 없이는 설정하지 않고, -stdin-ok 면 경고 후 설정한다.
	for _, withFlag := range []bool{false, true} {
		s := startMockTree(t, "dell-r660", nil)
		must(t, s.SetBiosAttr("LogicalProc", "Disabled"))
		env := newRTEnv(t, 0, "")
		user := env.writeFile("user.txt", hostKey(s))
		args := []string{"-conf", env.conf, "-hosts", env.hosts, "-profile", "VM", "-user", user}
		if withFlag {
			args = append(args, "-stdin-ok")
		}
		oldIn, oldErr := os.Stdin, os.Stderr
		os.Stdin = yes(t)
		ef, err := os.Create(filepath.Join(t.TempDir(), "stderr"))
		must(t, err)
		os.Stderr = ef
		out, cerr := rtCapture(t, func() error { return cmdCheck(args) })
		os.Stdin, os.Stderr = oldIn, oldErr
		ef.Close()
		errOut, _ := os.ReadFile(ef.Name())
		if cerr != nil {
			t.Fatalf("cmdCheck: %v\n%s", cerr, out)
		}
		switch {
		case !withFlag && (s.Writes() != 0 || !strings.Contains(out, msgNonInteractive)):
			t.Errorf("플래그 없음: Writes=%d\n%s", s.Writes(), out)
		case withFlag && (s.Writes() != 2 || !strings.Contains(string(errOut), "-stdin-ok")):
			t.Errorf("-stdin-ok: Writes=%d stderr=%q\n%s", s.Writes(), errOut, out)
		}
	}
	src, err := os.ReadFile("../../bios_check.sh")
	must(t, err)
	if !strings.Contains(string(src), "-stdin-ok") || !strings.Contains(usageText, "-stdin-ok") {
		t.Error("bios_check.sh usage / biostool usage 에 -stdin-ok 설명이 없음")
	}
}

// ---- 사소: 프롬프트는 실제 적용 가능 건수, apply_requests.txt 의 Sent 는 사실대로 ----

func TestSecPromptCountsApplicableOnly(t *testing.T) {
	dell := startMockTree(t, "dell-r660", nil)
	must(t, dell.SetBiosAttr("LogicalProc", "Disabled"))
	cisco := startMockTree(t, "cisco-c220m7", nil)
	must(t, cisco.SetBiosAttr("IntelHyperThread", "disabled"))
	port := dell.URL()[strings.LastIndex(dell.URL(), ":")+1:]
	rc := checkRCFor(t, 1, "127.0.0.1 dupe-m\n", dell.URL(), cisco.URL(), "dupe-m:"+port)
	run := runCheckP(t, rc, setProf(t, setProfText))
	if n := len(run.failItems()); n != 3 {
		t.Fatalf("FAIL %d건, 기대 3", n)
	}
	var out bytes.Buffer
	must(t, confirmApply(rc, run, strings.NewReader("n\n"), &out, false, testROpts))
	for _, w := range []string{"FAIL 1건(호스트 1대)을 Pending 으로 설정하시겠습니까?", "FAIL 3건(호스트 3대) 중 2건은 설정하지 않습니다 (쓰기 대상이 아닌 벤더 1건, 같은 BMC 중복 대상 1건"} {
		if !strings.Contains(out.String(), w) {
			t.Errorf("프롬프트에 %q 없음:\n%s", w, out.String())
		}
	}
}

func TestSecApplyRequestsSentFlag(t *testing.T) {
	s := startMockTree(t, "dell-r660", nil)
	must(t, s.SetBiosAttr("LogicalProc", "Disabled"))
	rc := checkRCFor(t, 1, "", s.URL())
	run := runCheckP(t, rc, setProf(t, setProfText))
	applyY(t, rc, run)
	reqs, err := os.ReadFile(filepath.Join(run.Dir, "apply_requests.txt"))
	must(t, err)
	if !strings.Contains(string(reqs), "Sent=true") {
		t.Errorf("전송한 요청에 Sent=true 가 없음:\n%s", reqs)
	}

	// 허용목록에서 막힌 PATCH 는 Sent=false 로 기록되고 BMC 에 닿지 않는다.
	c := NewClient(ClientOpts{BaseURL: s.URL(), User: testUser, Pass: testPass, Insecure: true, Mode: ModeSet, Gap: -1})
	defer c.Close()
	it := &setItem{Std: "hyper_threading", Attr: "LogicalProc", Expected: "Enabled", Value: "Enabled"}
	h := &setHost{Target: run.Hosts[0].Target, Info: run.Hosts[0].Info, Items: []*setItem{it}, Plan: &setPlan{
		Items: []*setItem{it}, Path: dellSystem + "/Actions/ComputerSystem.Reset", Body: []byte(`{"Attributes":{"LogicalProc":"Enabled"}}`),
	}}
	w0 := s.Writes()
	(&setEnv{}).send(h, c)
	if h.Sent || !h.Tried || it.Status != StatusSetUnsupported || s.Writes() != w0 {
		t.Errorf("막힌 PATCH: Tried=%v Sent=%v 상태=%s Writes %d→%d", h.Tried, h.Sent, it.Status, w0, s.Writes())
	}
	dir := t.TempDir()
	must(t, writeApplyFiles(dir, []*setHost{h}))
	b, err := os.ReadFile(filepath.Join(dir, "apply_requests.txt"))
	must(t, err)
	if !strings.Contains(string(b), "Sent=false") {
		t.Errorf("apply_requests.txt:\n%s", b)
	}
	if !errors.Is(func() error { _, err := checkAllowed(http.MethodPatch, h.Plan.Path, ModeSet, ""); return err }(), ErrNotAllowed) {
		t.Error("시험 전제: 경로가 허용목록 밖이어야 함")
	}
}
