package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"

	"biostool/internal/mockbmc"
)

// set.go 시험 (단계 6). 실제 BMC 는 쓰지 않고 internal/mockbmc(testdata 합성 트리)만 쓴다.

// setProfText 는 set 시험용 프로파일입니다 (쉼표는 탭으로 바뀐다). testdata 트리의 속성명에 맞춘 합성 값입니다.
// verified=Y 는 모델을 지정한 행에만 쓸 수 있으므로(* 행은 점검 비교용) 모든 Y 행에 모델을 적는다.
const setProfText = `vendor,model,std_name,attribute,value,verified
DELL,R660,system_profile,SysProfile,PerfOptimized,Y
DELL,R660,hyper_threading,LogicalProc,Enabled,Y
DELL,R660,llc_prefetch,LlcPrefetch,Enabled,Y
DELL,R660,sub_numa_cluster,SubNumaCluster,Disabled,N
HPE,DL360 Gen11,system_profile,WorkloadProfile,GeneralPowerEfficientCompute|GeneralThroughputCompute,Y
HPE,DL360 Gen11,hyper_threading,ProcHyperthreading,Enabled,Y
HPE,DL360 Gen11,llc_prefetch,LlcPrefetch,Enabled,Y
HPE,DL360 Gen11,sub_numa_cluster,SubNumaClustering,Disabled,Y
LENOVO,SR650 V3,system_profile,OperatingModes_ChooseOperatingMode,MaximumPerformance,Y
LENOVO,SR650 V3,hyper_threading,Processors_HyperThreading,Enable,Y
LENOVO,SR650 V3,llc_prefetch,Processors_LLCPrefetch,Enable,Y
LENOVO,SR650 V3,sub_numa_cluster,Processors_SNC,Disable,Y
LENOVO,SR650 V3,watchdog_value,SystemRecovery_POSTWatchdogTimerValue,5,Y
CISCO,UCSC-C220-M7S,hyper_threading,IntelHyperThread,enabled,Y
SUPERMICRO,X12DPi-N6,hyper_threading,Hyper-Threading[ALL],Enable,Y
`

const (
	dellBios     = "/redfish/v1/Systems/System.Embedded.1/Bios"
	dellSystem   = "/redfish/v1/Systems/System.Embedded.1"
	dellJobs     = "/redfish/v1/Managers/iDRAC.Embedded.1/Jobs"
	hpeSettings  = "/redfish/v1/systems/1/bios/settings/"
	lnvSettings  = "/redfish/v1/Systems/1/Bios/Pending"
	pendKeyDell  = "/redfish/v1/systems/system.embedded.1/bios/settings"
	pendKeyHPE   = "/redfish/v1/systems/1/bios/settings"
	pendKeyLnv   = "/redfish/v1/systems/1/bios/pending"
	dellPatchKey = "PATCH " + dellSettings
)

func setProf(t *testing.T, text string) *Profile {
	t.Helper()
	p, err := parseProfile(strings.ReplaceAll(text, ",", "\t"), "set-test.tsv")
	if err != nil {
		t.Fatal(err)
	}
	p.Name = "VM"
	return p
}

func runCheckP(t *testing.T, rc *runContext, prof *Profile) *checkRun {
	t.Helper()
	fastCheck(t)
	run, err := doCheck(rc, checkOpts{Profile: prof, ResultDir: t.TempDir(), Now: checkNow})
	if err != nil {
		t.Fatalf("doCheck: %v", err)
	}
	return run
}

var testROpts = reportOpts{ListMax: 100, FailMax: 200}

// applyY 는 Y 를 입력해 실제 후크(doApply)로 설정하고 출력을 돌려줍니다.
func applyY(t *testing.T, rc *runContext, run *checkRun) string {
	t.Helper()
	var out bytes.Buffer
	if err := confirmApply(rc, run, strings.NewReader("Y\n"), &out, false, testROpts); err != nil {
		t.Fatalf("confirmApply: %v\n%s", err, out.String())
	}
	return out.String()
}

// applyDirect 는 프롬프트 없이 주어진 항목으로 doApply 를 부릅니다 (방어 중첩 시험용).
func applyDirect(t *testing.T, rc *runContext, run *checkRun, fails []failItem) string {
	t.Helper()
	var out bytes.Buffer
	if err := doApply(rc, run, fails, &out, testROpts); err != nil {
		t.Fatalf("doApply: %v\n%s", err, out.String())
	}
	return out.String()
}

type applyRow struct{ status, detail string }

// applyResults 는 apply_result.tsv 를 host|attribute → (상태, 사유) 로 읽습니다.
func applyResults(t *testing.T, run *checkRun) map[string]applyRow {
	t.Helper()
	rows := readTSV(t, filepath.Join(run.Dir, "apply_result.tsv"))
	if len(rows) == 0 || strings.Join(rows[0], "\t") != strings.Join(applyHeader, "\t") {
		t.Fatalf("apply_result.tsv 머리글: %v", rows)
	}
	out := map[string]applyRow{}
	for _, r := range rows[1:] {
		if len(r) != len(applyHeader) {
			t.Fatalf("열 수 %d: %v", len(r), r)
		}
		out[r[0]+"|"+r[5]] = applyRow{r[7], r[8]}
	}
	return out
}

func hostKey(s *mockbmc.Server) string { return strings.TrimPrefix(s.URL(), "https://") }

func wantStatus(t *testing.T, res map[string]applyRow, s *mockbmc.Server, attr, want string) {
	t.Helper()
	got, ok := res[hostKey(s)+"|"+attr]
	if !ok {
		t.Errorf("%s %s: 결과 행 없음 (%v)", hostKey(s), attr, res)
		return
	}
	if got.status != want {
		t.Errorf("%s %s = %s (%s), 기대 %s", hostKey(s), attr, got.status, got.detail, want)
	}
}

func biosAttr(t *testing.T, s *mockbmc.Server, biosPath, name string) interface{} {
	t.Helper()
	var v interface{}
	must(t, s.Modify(biosPath, func(m map[string]interface{}) {
		a, _ := m["Attributes"].(map[string]interface{})
		v = a[name]
	}))
	return v
}

type mockSnap struct {
	patches, jobs, writes int
	sess                  mockbmc.SessionStats
}

func snapOf(s *mockbmc.Server) mockSnap {
	return mockSnap{patches: s.Count("PATCH", "/"), jobs: s.Count("POST", "/redfish/v1/Managers"), writes: s.Writes(), sess: s.Sessions()}
}

func patchBodies(s *mockbmc.Server) []string {
	var out []string
	for _, c := range s.Calls() {
		if c.Method == "PATCH" {
			out = append(out, c.Body)
		}
	}
	return out
}

func jobBodies(s *mockbmc.Server) []string {
	var out []string
	for _, c := range s.Calls() {
		if c.Method == "POST" && strings.Contains(strings.ToLower(c.Path), "/jobs") {
			out = append(out, c.Body)
		}
	}
	return out
}

// assertSetSafe 는 쓰기 경로 공통 안전 조건입니다: 로그 경로·Actions(Reset 등) 요청 0, 허용 메서드만.
func assertSetSafe(t *testing.T, s *mockbmc.Server) {
	t.Helper()
	if s.LogHits() != 0 {
		t.Errorf("LogHits = %d", s.LogHits())
	}
	for _, c := range s.Calls() {
		lp := strings.ToLower(c.Path)
		if strings.Contains(lp, "/actions") || strings.Contains(lp, "reset") {
			t.Errorf("Actions/Reset 요청: %s %s", c.Method, c.Path)
		}
		switch c.Method {
		case "GET", "PATCH":
		case "POST":
			if !strings.HasSuffix(lp, "/sessionservice/sessions") && !strings.HasSuffix(lp, "/jobs") {
				t.Errorf("허용되지 않은 POST: %s", c.Path)
			}
		case "DELETE":
			if !strings.Contains(lp, "/sessionservice/sessions/") {
				t.Errorf("세션 외 DELETE: %s", c.Path)
			}
		default:
			t.Errorf("허용되지 않은 메서드: %s %s", c.Method, c.Path)
		}
	}
}

// assertBodySafe 는 PATCH 본문이 {"Attributes":{프로파일 속성만}} + (선택) ApplyTime=OnReset 뿐인지 봅니다 (k).
func assertBodySafe(t *testing.T, prof *Profile, body string, wantApplyTime bool) map[string]interface{} {
	t.Helper()
	var m map[string]json.RawMessage
	if err := json.Unmarshal([]byte(body), &m); err != nil {
		t.Fatalf("PATCH 본문 JSON: %v (%s)", err, body)
	}
	names := map[string]bool{}
	for _, r := range prof.Rows {
		names[strings.ToLower(r.Attr)] = true
	}
	var attrs map[string]interface{}
	for k, v := range m {
		switch k {
		case "Attributes":
			if err := json.Unmarshal(v, &attrs); err != nil {
				t.Fatal(err)
			}
			for a := range attrs {
				if !names[strings.ToLower(a)] {
					t.Errorf("프로파일에 없는 속성 %q 를 보냄", a)
				}
			}
		case "@Redfish.SettingsApplyTime":
			if string(v) != `{"ApplyTime":"OnReset"}` {
				t.Errorf("ApplyTime = %s", v)
			}
		default:
			t.Errorf("PATCH 본문에 허용되지 않은 키 %q", k)
		}
	}
	if _, ok := m["@Redfish.SettingsApplyTime"]; ok != wantApplyTime {
		t.Errorf("ApplyTime 포함=%v, 기대 %v (%s)", ok, wantApplyTime, body)
	}
	return attrs
}

// ---- (a)(k)(l) Y 흐름 ----

func TestSetApplyYFlow(t *testing.T) {
	dell := startMockTree(t, "dell-r660", nil)
	must(t, dell.SetBiosAttr("LogicalProc", "Disabled"))
	must(t, dell.SetBiosAttr("LlcPrefetch", "Disabled"))
	hpe := startMockTree(t, "hpe-dl360gen11", nil)
	must(t, hpe.SetBiosAttr("ProcHyperthreading", "Disabled"))
	lnv := startMockTree(t, "lenovo-sr650v3", nil)
	must(t, lnv.SetBiosAttr("Processors_HyperThreading", "Disable"))
	must(t, lnv.SetBiosAttr("SystemRecovery_POSTWatchdogTimerValue", 7))

	prof := setProf(t, setProfText)
	rc := checkRCFor(t, 3, "", dell.URL(), hpe.URL(), lnv.URL())
	rc.Conf.Retries = 2
	run := runCheckP(t, rc, prof)
	if n := len(run.failItems()); n != 5 {
		t.Fatalf("FAIL 항목 %d개, 기대 5", n)
	}
	servers := []*mockbmc.Server{dell, hpe, lnv}
	before := make([]mockSnap, len(servers))
	for i, s := range servers {
		before[i] = snapOf(s)
		if before[i].writes != 0 {
			t.Fatalf("점검 단계에서 쓰기 %d건", before[i].writes)
		}
	}

	out := applyY(t, rc, run)
	res := applyResults(t, run)
	for _, c := range []struct {
		s    *mockbmc.Server
		attr string
	}{{dell, "LogicalProc"}, {dell, "LlcPrefetch"}, {hpe, "ProcHyperthreading"}, {lnv, "Processors_HyperThreading"}, {lnv, "SystemRecovery_POSTWatchdogTimerValue"}} {
		wantStatus(t, res, c.s, c.attr, StatusApplied)
	}
	if len(res) != 5 {
		t.Errorf("결과 행 %d개, 기대 5", len(res))
	}

	// 호스트당 PATCH 정확히 1, 허용 경로, Dell 만 Job 1, 세션 1·1
	for i, c := range []struct {
		s        *mockbmc.Server
		settings string
		jobs     int
	}{{dell, dellSettings, 1}, {hpe, hpeSettings, 0}, {lnv, lnvSettings, 0}} {
		now := snapOf(c.s)
		if d := now.patches - before[i].patches; d != 1 {
			t.Errorf("서버 %d: PATCH %d회, 기대 1", i, d)
		}
		if d := now.jobs - before[i].jobs; d != c.jobs {
			t.Errorf("서버 %d: Job POST %d회, 기대 %d", i, d, c.jobs)
		}
		if d := now.writes - before[i].writes; d != 1+c.jobs {
			t.Errorf("서버 %d: 쓰기 %d건, 기대 %d", i, d, 1+c.jobs)
		}
		if now.sess.Created-before[i].sess.Created != 1 || now.sess.Deleted-before[i].sess.Deleted != 1 || now.sess.LoginFailed != 0 {
			t.Errorf("서버 %d: 설정 단계 세션 %+v → %+v, 기대 생성 1·삭제 1", i, before[i].sess, now.sess)
		}
		for _, call := range c.s.Calls() {
			if call.Method == "PATCH" && call.Path != c.settings {
				t.Errorf("서버 %d: PATCH 경로 %s, 기대 %s", i, call.Path, c.settings)
			}
		}
		bodies := patchBodies(c.s)
		if len(bodies) == 1 {
			assertBodySafe(t, prof, bodies[0], true)
		}
		assertSetSafe(t, c.s)
	}
	// Dell Job 본문은 TargetSettingsURI 하나뿐 (재부팅 필드 없음)
	if jb := jobBodies(dell); len(jb) != 1 || jb[0] != `{"TargetSettingsURI":"`+dellSettings+`"}` {
		t.Errorf("Dell Job 본문 = %q", jb)
	}
	if js := dell.Jobs(); len(js) != 1 || js[0].Target != dellSettings {
		t.Errorf("Dell Job = %+v", js)
	}

	// mock 의 pending 에 기대값, 현재 Bios 는 불변
	pd, ph, pl := dell.Pending()[pendKeyDell], hpe.Pending()[pendKeyHPE], lnv.Pending()[pendKeyLnv]
	if pd["LogicalProc"] != "Enabled" || pd["LlcPrefetch"] != "Enabled" || len(pd) != 2 {
		t.Errorf("Dell pending = %v", pd)
	}
	if ph["ProcHyperthreading"] != "Enabled" || len(ph) != 1 {
		t.Errorf("HPE pending = %v", ph)
	}
	if pl["Processors_HyperThreading"] != "Enable" || pl["SystemRecovery_POSTWatchdogTimerValue"] != float64(5) || len(pl) != 2 {
		t.Errorf("Lenovo pending = %v (정수 속성은 숫자로 보내야 함)", pl)
	}
	if v := biosAttr(t, dell, dellBios, "LogicalProc"); v != "Disabled" {
		t.Errorf("현재 Bios 가 바뀜: LogicalProc=%v", v)
	}
	if v := biosAttr(t, hpe, "/redfish/v1/systems/1/bios", "ProcHyperthreading"); v != "Disabled" {
		t.Errorf("현재 Bios 가 바뀜: ProcHyperthreading=%v", v)
	}

	// 결과 파일·콘솔
	if got := readLines(t, filepath.Join(run.Dir, "applied.txt")); fmt.Sprint(got) != fmt.Sprint([]string{hostKey(dell), hostKey(hpe), hostKey(lnv)}) {
		t.Errorf("applied.txt = %v", got)
	}
	if _, err := os.Stat(filepath.Join(run.Dir, "apply_failed.txt")); err == nil {
		t.Error("실패가 없는데 apply_failed.txt 가 생김")
	}
	if runtime.GOOS != "windows" {
		for _, f := range []string{"apply_result.tsv", "applied.txt", "apply_requests.txt"} {
			if fi, err := os.Stat(filepath.Join(run.Dir, f)); err != nil || fi.Mode().Perm() != 0o600 {
				t.Errorf("%s 권한: %v %v", f, fi, err)
			}
		}
	}
	reqs, err := os.ReadFile(filepath.Join(run.Dir, "apply_requests.txt"))
	must(t, err)
	for _, s := range servers {
		if b := patchBodies(s); len(b) == 1 && !strings.Contains(string(reqs), "  본문  "+b[0]+"\n") {
			t.Errorf("apply_requests.txt 에 실제 전송 본문 %s 가 없음", b[0])
		}
	}
	for _, w := range []string{
		"FAIL 5건(호스트 3대)을 Pending 으로 설정하시겠습니까?", "== BIOS Pending 설정 결과 (호스트 3대, 항목 5건) ==",
		"APPLIED", "[적용된 호스트 (Pending 설정 확인)] 3대", hostKey(dell), hostKey(lnv),
		"재부팅은 하지 않았습니다", "bios_check.sh 로 재점검", "랜덤한 서버 몇 대를 직접 확인해 실제 변경되었는지 확인",
	} {
		if !strings.Contains(out, w) {
			t.Errorf("출력에 %q 없음:\n%s", w, out)
		}
	}
	assertNoSecrets(t, out, run.Dir)
}

// assertNoSecrets 는 출력과 결과 폴더 파일에 비밀번호·토큰 흔적이 없는지 봅니다 (l).
func assertNoSecrets(t *testing.T, out, dir string) {
	t.Helper()
	texts := []string{out}
	must(t, filepath.Walk(dir, func(p string, fi os.FileInfo, err error) error {
		if err == nil && !fi.IsDir() {
			b, rerr := os.ReadFile(p)
			must(t, rerr)
			texts = append(texts, string(b))
		}
		return err
	}))
	for _, text := range texts {
		for _, secret := range []string{testPass, "S3cr3t", "X-Auth-Token", "Authorization", "Basic "} {
			if strings.Contains(text, secret) {
				t.Errorf("출력/결과 파일에 %q 가 있음", secret)
			}
		}
	}
}

// ---- 요구사항 1 + (b): FAIL 만 넘어오고, set 이 독립적으로 다시 막는다 ----

func TestSetOnlyVerifiedFailAndDefense(t *testing.T) {
	unv := startMockTree(t, "dell-r660", nil) // UNVERIFIED (verified=N 행)
	must(t, unv.SetBiosAttr("SubNumaCluster", "Enabled"))
	pex := startMockTree(t, "dell-r660", nil) // PENDING_EXISTS
	must(t, pex.SetBiosAttr("LogicalProc", "Disabled"))
	setPending(t, pex, dellSettings, map[string]interface{}{"ProcCStates": "Disabled"})
	ok := startMockTree(t, "dell-r660", nil) // 대조군 FAIL
	must(t, ok.SetBiosAttr("LogicalProc", "Disabled"))

	prof := setProf(t, setProfText)
	rc := checkRCFor(t, 1, "", unv.URL(), pex.URL(), ok.URL())
	run := runCheckP(t, rc, prof)
	fails := run.failItems()
	if len(fails) != 1 || fails[0].Target.Hostname != hostKey(ok) {
		t.Fatalf("failItems 는 verified=Y 이고 PENDING_EXISTS 가 아닌 FAIL 만이어야 함: %+v", fails)
	}
	for _, h := range run.Hosts {
		for _, it := range h.Items {
			if it.Result == StatusFail && (!it.Verified || h.Foreign > 0) {
				t.Errorf("FAIL 인데 verified=N 이거나 남의 Pending 있음: %+v", it)
			}
		}
	}
	applyY(t, rc, run)
	if unv.Writes() != 0 || pex.Writes() != 0 || ok.Writes() != 2 {
		t.Errorf("Writes unv=%d pex=%d ok=%d, 기대 0/0/2", unv.Writes(), pex.Writes(), ok.Writes())
	}

	// 점검 판정을 우회한 입력(조작된 failItems)도 set 이 스스로 막는다.
	byHost := map[string]*hostCheck{}
	for _, h := range run.Hosts {
		byHost[h.Target.Hostname] = h
	}
	mk := func(s *mockbmc.Server, std, attr, exp string) failItem {
		h := byHost[hostKey(s)]
		return failItem{Target: h.Target, Info: h.Info, Std: std, Attr: attr, Expected: exp, Current: "x"}
	}
	run2 := runCheckP(t, rc, prof) // 결과 폴더만 새로
	unvSess := unv.Sessions()
	out := applyDirect(t, rc, run2, []failItem{
		mk(unv, "sub_numa_cluster", "SubNumaCluster", "Disabled"), // verified=N 행
		mk(pex, "hyper_threading", "LogicalProc", "Enabled"),      // 남의 Pending
		mk(ok, "hyper_threading", "LogicalProc", "Disabled"),      // 기대값 조작
		mk(ok, "c_states", "ProcCStates", "Enabled"),              // 프로파일에 없는 속성
	})
	res := applyResults(t, run2)
	wantStatus(t, res, unv, "SubNumaCluster", StatusUnverified)
	wantStatus(t, res, pex, "LogicalProc", StatusPendingExists)
	wantStatus(t, res, ok, "LogicalProc", StatusUnverified)
	wantStatus(t, res, ok, "ProcCStates", StatusUnverified)
	if unv.Writes() != 0 || pex.Writes() != 0 || ok.Writes() != 2 {
		t.Errorf("조작된 입력으로 쓰기가 나감: unv=%d pex=%d ok=%d\n%s", unv.Writes(), pex.Writes(), ok.Writes(), out)
	}
	if unv.Sessions() != unvSess {
		t.Errorf("UNVERIFIED 만 있는 호스트는 접속하지 않아야 함: %+v", unv.Sessions())
	}
}

// ---- (c) 재조회: 점검 이후 바뀐 호스트는 쓰지 않는다 ----

func TestSetRereadDetectsChanges(t *testing.T) {
	type sc struct {
		name, attr, want string
		mod              func(t *testing.T, s *mockbmc.Server)
	}
	cases := []sc{
		{"이미 고쳐짐", "LogicalProc", StatusAlreadyOK, func(t *testing.T, s *mockbmc.Server) { must(t, s.SetBiosAttr("LogicalProc", "Enabled")) }},
		{"누가 이미 기대값 Pending (+BIOS 설정 Job)", "LogicalProc", StatusAlreadyOK, func(t *testing.T, s *mockbmc.Server) {
			setPending(t, s, dellSettings, map[string]interface{}{"LogicalProc": "Enabled"})
			s.AddJob(dellJobs, dellSettings, "", "") // Dell 은 Job 이 없으면 Pending 만으로는 반영되지 않음 (그 경우는 set_security_test)
		}},
		{"새 남의 Pending", "LogicalProc", StatusPendingExists, func(t *testing.T, s *mockbmc.Server) {
			setPending(t, s, dellSettings, map[string]interface{}{"ProcCStates": "Disabled"})
		}},
		{"우리 속성에 다른 Pending", "LogicalProc", StatusPendingExists, func(t *testing.T, s *mockbmc.Server) {
			setPending(t, s, dellSettings, map[string]interface{}{"LogicalProc": "Weird"})
		}},
		{"OK 였던 표준 속성에 비표준 Pending", "LogicalProc", StatusPendingExists, func(t *testing.T, s *mockbmc.Server) {
			setPending(t, s, dellSettings, map[string]interface{}{"LlcPrefetch": "Disabled"})
		}},
		{"모델 변경", "LogicalProc", StatusChanged, func(t *testing.T, s *mockbmc.Server) {
			must(t, s.Modify(dellSystem, func(m map[string]interface{}) { m["Model"] = "PowerEdge R760" }))
		}},
		{"벤더 변경", "LogicalProc", StatusChanged, func(t *testing.T, s *mockbmc.Server) {
			must(t, s.Modify(dellSystem, func(m map[string]interface{}) { m["Manufacturer"] = "Other Corp" }))
		}},
		{"Settings 경로 변경", "LogicalProc", StatusChanged, func(t *testing.T, s *mockbmc.Server) {
			must(t, s.Modify(dellBios, func(m map[string]interface{}) {
				m["@Redfish.Settings"] = map[string]interface{}{"SettingsObject": map[string]interface{}{"@odata.id": dellBios + "/Pending"}}
			}))
		}},
		{"속성 사라짐", "LogicalProc", StatusChanged, func(t *testing.T, s *mockbmc.Server) { must(t, s.SetBiosAttr("LogicalProc", nil)) }},
		{"Settings 404", "LogicalProc", StatusSetUnsupported, func(t *testing.T, s *mockbmc.Server) { s.Remove(dellSettings) }},
	}
	var servers []*mockbmc.Server
	var urls []string
	for range cases {
		s := startMockTree(t, "dell-r660", nil)
		must(t, s.SetBiosAttr("LogicalProc", "Disabled"))
		servers = append(servers, s)
		urls = append(urls, s.URL())
	}
	rc := checkRCFor(t, 4, "", urls...)
	run := runCheckP(t, rc, setProf(t, setProfText))
	if n := len(run.failItems()); n != len(cases) {
		t.Fatalf("FAIL %d건, 기대 %d", n, len(cases))
	}
	for i, c := range cases {
		c.mod(t, servers[i])
	}
	applyY(t, rc, run)
	res := applyResults(t, run)
	for i, c := range cases {
		wantStatus(t, res, servers[i], c.attr, c.want)
		if w := servers[i].Writes(); w != 0 {
			t.Errorf("%s: Writes = %d, 기대 0", c.name, w)
		}
		assertSetSafe(t, servers[i])
	}
}

// ---- (d) 레지스트리 사전검증 ----

func TestSetRegistryPrecheck(t *testing.T) {
	text := setProfText +
		"HPE,DL360 Gen11,secure_boot,SecureBootStatus,Enabled,Y\n" + // ReadOnly 속성
		"HPE,DL360 Gen11,turbo,ProcTurbo,Maybe,Y\n" // 허용값 밖
	text = strings.Replace(text, "SystemRecovery_POSTWatchdogTimerValue,5,Y", "SystemRecovery_POSTWatchdogTimerValue,50,Y", 1) // 상한 20 초과
	prof := setProf(t, text)

	onlyBad := startMockTree(t, "hpe-dl360gen11", nil) // ReadOnly·허용값 밖만 FAIL → 아무것도 안 보냄
	mixed := startMockTree(t, "hpe-dl360gen11", nil)   // + 정상 FAIL 1건 → 그것만 보냄
	must(t, mixed.SetBiosAttr("ProcHyperthreading", "Disabled"))
	lnv := startMockTree(t, "lenovo-sr650v3", nil) // Integer 범위 밖
	rc := checkRCFor(t, 2, "", onlyBad.URL(), mixed.URL(), lnv.URL())
	run := runCheckP(t, rc, prof)
	if n := len(run.failItems()); n != 6 {
		t.Fatalf("FAIL %d건, 기대 6", n)
	}
	applyY(t, rc, run)
	res := applyResults(t, run)
	for _, s := range []*mockbmc.Server{onlyBad, mixed} {
		wantStatus(t, res, s, "SecureBootStatus", StatusSkippedReadOnly)
		wantStatus(t, res, s, "ProcTurbo", StatusInvalidValue)
	}
	wantStatus(t, res, mixed, "ProcHyperthreading", StatusApplied)
	wantStatus(t, res, lnv, "SystemRecovery_POSTWatchdogTimerValue", StatusInvalidValue)
	if onlyBad.Writes() != 0 || lnv.Writes() != 0 {
		t.Errorf("Writes onlyBad=%d lenovo=%d, 기대 0", onlyBad.Writes(), lnv.Writes())
	}
	if b := patchBodies(mixed); len(b) != 1 || !strings.Contains(b[0], `"Attributes":{"ProcHyperthreading":"Enabled"}`) {
		t.Errorf("mixed PATCH 본문 = %v (ReadOnly·허용값 밖 속성이 들어가면 안 됨)", b)
	}
}

// ---- (e) 시스템 프로파일 종속 ----

func TestSetProfileDependency(t *testing.T) {
	prof := setProf(t, setProfText)
	// 프로파일(Custom → PerfOptimized) + 다른 항목 FAIL: 프로파일만 보내고 나머지는 DEPENDS_ON_PROFILE.
	both := startMockTree(t, "dell-r660", nil)
	must(t, both.SetBiosAttr("SysProfile", "Custom"))
	must(t, both.SetBiosAttr("LogicalProc", "Disabled"))
	must(t, both.SetBiosAttr("LlcPrefetch", "Disabled"))
	// 프로파일 변경이 이미 (우리 표준값으로) Pending: 다른 항목은 보내지 않는다.
	pendProf := startMockTree(t, "dell-r660", nil)
	must(t, pendProf.SetBiosAttr("SysProfile", "Custom"))
	setPending(t, pendProf, dellSettings, map[string]interface{}{"SysProfile": "PerfOptimized"})
	pendProf.AddJob(dellJobs, dellSettings, "", "") // 예약된 BIOS 설정 Job 이 있어야 PENDING_OK
	must(t, pendProf.SetBiosAttr("LogicalProc", "Disabled"))
	// 프로파일만 FAIL: 그대로 보낸다.
	alone := startMockTree(t, "dell-r660", nil)
	must(t, alone.SetBiosAttr("SysProfile", "Custom"))

	rc := checkRCFor(t, 1, "", both.URL(), pendProf.URL(), alone.URL())
	run := runCheckP(t, rc, prof)
	if got := itemOf(t, run.Hosts[1], "system_profile").Result; got != StatusPendingOK {
		t.Fatalf("pendProf system_profile = %s", got)
	}
	out := applyY(t, rc, run)
	res := applyResults(t, run)
	wantStatus(t, res, both, "SysProfile", StatusApplied)
	wantStatus(t, res, both, "LogicalProc", StatusDependsOnProfile)
	wantStatus(t, res, both, "LlcPrefetch", StatusDependsOnProfile)
	wantStatus(t, res, pendProf, "LogicalProc", StatusDependsOnProfile)
	wantStatus(t, res, alone, "SysProfile", StatusApplied)
	b := patchBodies(both)
	if len(b) != 1 {
		t.Fatalf("both PATCH %d회", len(b))
	}
	if attrs := assertBodySafe(t, prof, b[0], true); len(attrs) != 1 || attrs["SysProfile"] != "PerfOptimized" {
		t.Errorf("프로파일 항목만 보내야 함: %v", attrs)
	}
	if pendProf.Writes() != 0 {
		t.Errorf("프로파일 Pending 대기 중인 호스트에 쓰기 %d건", pendProf.Writes())
	}
	if !strings.Contains(out, "재부팅 후 bios_check.sh 로 재점검") {
		t.Errorf("DEPENDS_ON_PROFILE 안내 없음:\n%s", out)
	}

	// 목표 프로파일이 Custom: 레지스트리가 있으면 함께, 없으면 프로파일만 (보수적).
	customProf := setProf(t, strings.Replace(setProfText, "SysProfile,PerfOptimized,Y", "SysProfile,Custom,Y", 1))
	withReg := startMockTree(t, "dell-r660", nil)
	noReg := startMockTree(t, "dell-r660", nil)
	noRegSingle := startMockTree(t, "dell-r660", nil)
	for _, s := range []*mockbmc.Server{withReg, noReg} {
		must(t, s.SetBiosAttr("LogicalProc", "Disabled"))
	}
	must(t, noRegSingle.SetBiosAttr("SysProfile", "Custom"))
	must(t, noRegSingle.SetBiosAttr("LogicalProc", "Disabled"))
	for _, s := range []*mockbmc.Server{noReg, noRegSingle} {
		s.Remove("/redfish/v1/Registries/BiosAttributeRegistry.v1_0_0")
	}
	rc2 := checkRCFor(t, 1, "", withReg.URL(), noReg.URL(), noRegSingle.URL())
	run2 := runCheckP(t, rc2, customProf)
	applyY(t, rc2, run2)
	res2 := applyResults(t, run2)
	wantStatus(t, res2, withReg, "SysProfile", StatusApplied)
	wantStatus(t, res2, withReg, "LogicalProc", StatusApplied)
	wantStatus(t, res2, noReg, "SysProfile", StatusApplied)
	wantStatus(t, res2, noReg, "LogicalProc", StatusDependsOnProfile)
	wantStatus(t, res2, noRegSingle, "LogicalProc", StatusApplied) // 프로파일 비종속 단일 항목은 진행
	if r := res2[hostKey(noRegSingle)+"|LogicalProc"]; !strings.Contains(r.detail, "레지스트리") {
		t.Errorf("레지스트리 없이 보낸 사유가 기록되지 않음: %q", r.detail)
	}
}

// ---- (f) 쓰기 대상이 아닌 벤더 ----

func TestSetVendorNotSupported(t *testing.T) {
	cisco := startMockTree(t, "cisco-c220m7", nil)
	must(t, cisco.SetBiosAttr("IntelHyperThread", "disabled"))
	smc := startMockTree(t, "supermicro-x12", nil)
	must(t, smc.SetBiosAttr("Hyper-Threading[ALL]", "Disable"))
	rc := checkRCFor(t, 2, "", cisco.URL(), smc.URL())
	run := runCheckP(t, rc, setProf(t, setProfText))
	if n := len(run.failItems()); n != 2 {
		t.Fatalf("FAIL %d건", n)
	}
	sc, ss := cisco.Sessions(), smc.Sessions()
	applyY(t, rc, run)
	res := applyResults(t, run)
	wantStatus(t, res, cisco, "IntelHyperThread", StatusSetNotSupportedVendor)
	wantStatus(t, res, smc, "Hyper-Threading[ALL]", StatusSetNotSupportedVendor)
	if cisco.Writes() != 0 || smc.Writes() != 0 {
		t.Errorf("Writes cisco=%d smc=%d", cisco.Writes(), smc.Writes())
	}
	if cisco.Sessions() != sc || smc.Sessions() != ss {
		t.Error("쓰기 대상이 아닌 벤더는 설정 단계에서 접속하지 않아야 함")
	}
}

// ---- (g) -dry-run ----

// dryBodies 는 dry-run 출력에서 호스트별 [Y 응답 시 보낼 요청] / [미검증 ...] 의 본문·Job 줄을 뽑습니다.
func dryBodies(out string) (real, trial, job map[string]string) {
	real, trial, job = map[string]string{}, map[string]string{}, map[string]string{}
	host, mode := "", ""
	for _, l := range strings.Split(out, "\n") {
		switch {
		case l != "" && !strings.HasPrefix(l, " ") && strings.Contains(l, "  "):
			host, mode = strings.Fields(l)[0], ""
		case l == "  [Y 응답 시 보낼 요청]":
			mode = "real"
		case strings.HasPrefix(l, "  [미검증 — 실제 설정 불가, 시험 출력]"):
			mode = "trial"
		case strings.HasPrefix(l, "  본문  ") && mode == "real":
			real[host] = strings.TrimPrefix(l, "  본문  ")
		case strings.HasPrefix(l, "  본문  ") && mode == "trial":
			trial[host] = strings.TrimPrefix(l, "  본문  ")
		case strings.HasPrefix(l, "  Job   POST ") && mode == "real":
			f := strings.Fields(l)
			job[host] = f[len(f)-1]
		}
	}
	return
}

func TestSetDryRunMatchesApply(t *testing.T) {
	dell := startMockTree(t, "dell-r660", nil)
	must(t, dell.SetBiosAttr("LogicalProc", "Disabled"))
	must(t, dell.SetBiosAttr("SubNumaCluster", "Enabled")) // UNVERIFIED 도 시험 출력
	hpe := startMockTree(t, "hpe-dl360gen11", nil)
	must(t, hpe.SetBiosAttr("ProcHyperthreading", "Disabled"))
	must(t, hpe.SetBiosAttr("WorkloadProfile", "Virtualization-MaxPerformance"))
	lnv := startMockTree(t, "lenovo-sr650v3", nil)
	must(t, lnv.SetBiosAttr("Processors_LLCPrefetch", "Disable"))
	onlyUnv := startMockTree(t, "dell-r660", nil)
	must(t, onlyUnv.SetBiosAttr("SubNumaCluster", "Enabled"))
	servers := []*mockbmc.Server{dell, hpe, lnv, onlyUnv}

	prof := setProf(t, setProfText)
	rc := checkRCFor(t, 2, "", dell.URL(), hpe.URL(), lnv.URL(), onlyUnv.URL())
	run := runCheckP(t, rc, prof)
	var dry bytes.Buffer
	if err := doDryRun(rc, run, &dry); err != nil {
		t.Fatal(err)
	}
	out := dry.String()
	for _, s := range servers {
		if s.Writes() != 0 {
			t.Errorf("dry-run 인데 Writes = %d", s.Writes())
		}
		assertSetSafe(t, s)
	}
	if strings.Contains(out, "(Y/N): ") || !strings.Contains(out, "미검증 — 실제 설정 불가, 시험 출력") || !strings.Contains(out, "BMC 에 아무것도 쓰지 않았") {
		t.Errorf("dry-run 출력:\n%s", out)
	}
	real, trial, job := dryBodies(out)
	if len(real) != 3 || real[hostKey(onlyUnv)] != "" {
		t.Errorf("실제 적용 본문 대상 = %v (UNVERIFIED 만 있는 호스트는 없어야 함)", real)
	}
	if !strings.Contains(trial[hostKey(dell)], `"SubNumaCluster":"Disabled"`) || !strings.Contains(trial[hostKey(onlyUnv)], `"SubNumaCluster":"Disabled"`) {
		t.Errorf("미검증 시험 본문 = %v", trial)
	}
	if strings.Contains(real[hostKey(dell)], "SubNumaCluster") {
		t.Errorf("실제 적용 본문에 미검증 속성이 있음: %s", real[hostKey(dell)])
	}
	if job[hostKey(dell)] == "" || job[hostKey(hpe)] != "" {
		t.Errorf("Job 표시 = %v (Dell 만)", job)
	}

	// 같은 상태에서 Y 로 실제 적용 → 전송 본문이 dry-run 본문과 바이트 단위로 같다.
	applyY(t, rc, run)
	for _, s := range servers[:3] {
		b := patchBodies(s)
		if len(b) != 1 || b[0] != real[hostKey(s)] {
			t.Errorf("%s: 실제 본문 %q != dry-run 본문 %q", hostKey(s), b, real[hostKey(s)])
		}
	}
	if jb := jobBodies(dell); len(jb) != 1 || jb[0] != job[hostKey(dell)] {
		t.Errorf("Dell Job 실제 %q != dry-run %q", jb, job[hostKey(dell)])
	}
	if onlyUnv.Writes() != 0 {
		t.Errorf("UNVERIFIED 만 있는 호스트에 쓰기 %d건", onlyUnv.Writes())
	}
}

// -dry-run 은 덤프 읽기(-from-dump)로도 같은 본문을 낸다 (계획서 성공기준 1).
func TestSetDryRunFromDump(t *testing.T) {
	fastDump(t)
	dell := startMockTree(t, "dell-r660", nil)
	must(t, dell.SetBiosAttr("LogicalProc", "Disabled"))
	hpe := startMockTree(t, "hpe-dl360gen11", nil)
	must(t, hpe.SetBiosAttr("SubNumaClustering", "Enabled"))
	urls := []string{dell.URL(), hpe.URL()}
	prof := setProf(t, setProfText)

	rc := checkRCFor(t, 2, "", urls...)
	var online bytes.Buffer
	must(t, doDryRun(rc, runCheckP(t, rc, prof), &online))
	dumpDir := t.TempDir()
	if _, err := runDump(dumpRC(t, 2, urls...), dumpOpts{Out: dumpDir}); err != nil {
		t.Fatal(err)
	}
	dell.Close()
	hpe.Close()

	offRC := checkRCFor(t, 2, "", urls...)
	offRC.Password, offRC.Conf.User = "", ""
	fastCheck(t)
	offRun, err := doCheck(offRC, checkOpts{Profile: prof, FromDump: dumpDir, ResultDir: t.TempDir(), Now: checkNow})
	must(t, err)
	var offline bytes.Buffer
	must(t, doDryRun(offRC, offRun, &offline))
	on, _, onJob := dryBodies(online.String())
	off, _, offJob := dryBodies(offline.String())
	if len(on) != 2 || fmt.Sprint(on) != fmt.Sprint(off) || fmt.Sprint(onJob) != fmt.Sprint(offJob) {
		t.Errorf("온라인/덤프 dry-run 본문이 다름\n온라인: %v %v\n덤프:   %v %v\n%s", on, onJob, off, offJob, offline.String())
	}
	if !strings.Contains(offline.String(), "덤프 읽기 모드") {
		t.Errorf("덤프 모드 표시 없음:\n%s", offline.String())
	}
	// 덤프 읽기 모드에서는 실제 적용 자체가 거부된다.
	if err := doApply(offRC, offRun, offRun.failItems(), &bytes.Buffer{}, testROpts); err == nil {
		t.Error("덤프 읽기 모드인데 doApply 가 거부하지 않음")
	}
}

// ---- (h) N / EOF / -no-prompt ----

func TestSetPromptNoWrites(t *testing.T) {
	s := startMockTree(t, "dell-r660", nil)
	must(t, s.SetBiosAttr("LogicalProc", "Disabled"))
	rc := checkRCFor(t, 1, "", s.URL())
	run := runCheckP(t, rc, setProf(t, setProfText))
	sess := s.Sessions()
	for _, c := range []struct {
		in       string
		noPrompt bool
	}{{"n\n", false}, {"N\n", false}, {"", false}, {"\n", false}, {"yes\n", false}, {"Y\n", true}} {
		var out bytes.Buffer
		must(t, confirmApply(rc, run, strings.NewReader(c.in), &out, c.noPrompt, testROpts))
		if s.Writes() != 0 || s.Sessions() != sess {
			t.Errorf("입력 %q no-prompt=%v: Writes=%d 세션 %+v", c.in, c.noPrompt, s.Writes(), s.Sessions())
		}
	}
	if _, err := os.Stat(filepath.Join(run.Dir, "apply_result.tsv")); err == nil {
		t.Error("설정하지 않았는데 apply_result.tsv 가 생김")
	}
}

// ---- (i)(n) 응답 분류와 재시도 없음 ----

func TestSetWriteErrorsNoRetry(t *testing.T) {
	cases := []struct {
		name     string
		tree     string
		faults   map[string]mockbmc.Fault
		want     string
		jobs     int    // Job POST 기대 횟수
		detail   string // 사유에 들어 있어야 할 문자열
		timeout  time.Duration
		pendWant bool
	}{
		{"400", "dell-r660", map[string]mockbmc.Fault{dellPatchKey: {Status: 400}}, StatusRejected, 0, "injected fault", 0, false},
		{"409", "dell-r660", map[string]mockbmc.Fault{dellPatchKey: {Status: 409}}, StatusRejected, 0, "HTTP 409", 0, false},
		{"405", "dell-r660", map[string]mockbmc.Fault{dellPatchKey: {Status: 405}}, StatusSetUnsupported, 0, "HTTP 405", 0, false},
		{"501", "dell-r660", map[string]mockbmc.Fault{dellPatchKey: {Status: 501}}, StatusSetUnsupported, 0, "HTTP 501", 0, false},
		{"500", "dell-r660", map[string]mockbmc.Fault{dellPatchKey: {Status: 500}}, StatusSetError, 0, "재점검", 0, false},
		{"503", "hpe-dl360gen11", map[string]mockbmc.Fault{"PATCH " + hpeSettings: {Status: 503}}, StatusSetError, 0, "BMC_ERROR", 0, false},
		{"타임아웃", "dell-r660", map[string]mockbmc.Fault{dellPatchKey: {Delay: 3 * time.Second}}, StatusSetError, 0, "TIMEOUT", time.Second, false},
		{"연결 끊김", "dell-r660", map[string]mockbmc.Fault{dellPatchKey: {Drop: true}}, StatusSetError, 0, "UNREACHABLE", 0, false},
		{"401", "dell-r660", map[string]mockbmc.Fault{dellPatchKey: {Status: 401}}, StatusAuthFail, 0, "HTTP 401", 0, false},
		{"403", "hpe-dl360gen11", map[string]mockbmc.Fault{"PATCH " + hpeSettings: {Status: 403}}, StatusAuthFail, 0, "HTTP 403", 0, false},
		// (n) PATCH 를 200 으로 받고 pending 에 반영하지 않는 장애: 재조회로 APPLY_UNCONFIRMED
		{"200 미반영 HPE", "hpe-dl360gen11", map[string]mockbmc.Fault{"PATCH " + hpeSettings: {Status: 200}}, StatusApplyUnconfirmed, 0, "Pending 이 없음", 0, false},
		{"200 미반영 Dell (Job 은 만듦)", "dell-r660", map[string]mockbmc.Fault{dellPatchKey: {Status: 200}}, StatusApplyUnconfirmed, 1, "Job 생성", 0, false},
		{"202 Dell: Job 자동 생성으로 보고 POST 안 함", "dell-r660", map[string]mockbmc.Fault{dellPatchKey: {Status: 202}}, StatusApplyUnconfirmed, 0, "자동 생성", 0, false},
		{"Dell Job 생성 실패", "dell-r660", map[string]mockbmc.Fault{"POST " + dellJobs: {Status: 500}}, StatusApplyUnconfirmed, 1, "Job 생성 실패", 0, true},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			s := startMockTree(t, c.tree, nil)
			attr := "LogicalProc"
			if strings.HasPrefix(c.tree, "hpe") {
				attr = "ProcHyperthreading"
			}
			must(t, s.SetBiosAttr(attr, "Disabled"))
			rc := checkRCFor(t, 1, "", s.URL())
			rc.Conf.Retries = 3 // GET 은 재시도해도 쓰기는 재시도하지 않는다
			if c.timeout > 0 {
				rc.Conf.Timeout = c.timeout
			}
			run := runCheckP(t, rc, setProf(t, setProfText))
			for k, f := range c.faults {
				s.SetFault(k, f)
			}
			before := snapOf(s)
			out := applyY(t, rc, run)
			res := applyResults(t, run)
			wantStatus(t, res, s, attr, c.want)
			if r := res[hostKey(s)+"|"+attr]; !strings.Contains(r.detail, c.detail) {
				t.Errorf("사유 %q 에 %q 없음", r.detail, c.detail)
			}
			now := snapOf(s)
			if d := now.patches - before.patches; d != 1 {
				t.Errorf("PATCH %d회, 기대 1 (재시도 금지)", d)
			}
			if d := now.jobs - before.jobs; d != c.jobs {
				t.Errorf("Job POST %d회, 기대 %d", d, c.jobs)
			}
			if got := len(s.Pending()) > 0; got != c.pendWant {
				t.Errorf("pending 존재=%v, 기대 %v (%v)", got, c.pendWant, s.Pending())
			}
			if now.sess.Created-before.sess.Created != 1 {
				t.Errorf("설정 단계 세션 생성 %d", now.sess.Created-before.sess.Created)
			}
			if applyFailedStatus[c.want] {
				if got := readLines(t, filepath.Join(run.Dir, "apply_failed.txt")); len(got) != 1 {
					t.Errorf("apply_failed.txt = %v", got)
				}
			}
			if c.want == StatusSetError && !strings.Contains(out, "재시도하지 않음") {
				t.Errorf("SET_ERROR 안내 없음:\n%s", out)
			}
			assertSetSafe(t, s)
		})
	}
}

// 401 차단기: PATCH 가 401 인 호스트가 auth_fail_stop 에 이르면 나머지는 접속하지 않는다.
func TestSetAuthFailBreaker(t *testing.T) {
	var servers []*mockbmc.Server
	var urls []string
	for i := 0; i < 5; i++ {
		s := startMockTree(t, "hpe-dl360gen11", nil)
		must(t, s.SetBiosAttr("ProcHyperthreading", "Disabled"))
		servers = append(servers, s)
		urls = append(urls, s.URL())
	}
	rc := checkRCFor(t, 5, "", urls...)
	rc.Conf.AuthFailStop = 2
	run := runCheckP(t, rc, setProf(t, setProfText))
	var sess []mockbmc.SessionStats
	for _, s := range servers {
		s.SetFault("PATCH "+hpeSettings, mockbmc.Fault{Status: 401})
		sess = append(sess, s.Sessions())
	}
	out := applyY(t, rc, run)
	res := applyResults(t, run)
	patches := 0
	for i, s := range servers {
		patches += s.Count("PATCH", "/")
		want := StatusAuthFail
		if i >= 2 {
			want = StatusSkippedAuthStop
			if s.Sessions() != sess[i] {
				t.Errorf("차단 후 호스트 %d 에 접속함", i)
			}
		}
		wantStatus(t, res, s, "ProcHyperthreading", want)
	}
	if patches != 2 {
		t.Errorf("PATCH 총 %d회, 기대 2", patches)
	}
	if !strings.Contains(out, "!! 계정 잠금 방지") || len(readLines(t, filepath.Join(run.Dir, "apply_failed.txt"))) != 5 {
		t.Errorf("차단기 안내/apply_failed.txt:\n%s", out)
	}
}

// (k) Bios 가 OnReset 을 지원한다고 하지 않으면 ApplyTime 을 넣지 않는다.
func TestSetApplyTimeOnlyWhenSupported(t *testing.T) {
	s := startMockTree(t, "hpe-dl360gen11", nil)
	must(t, s.SetBiosAttr("ProcHyperthreading", "Disabled"))
	must(t, s.Modify("/redfish/v1/systems/1/bios", func(m map[string]interface{}) {
		rs := m["@Redfish.Settings"].(map[string]interface{})
		rs["SupportedApplyTimes"] = []interface{}{"Immediate", "AtMaintenanceWindowStart"}
	}))
	prof := setProf(t, setProfText)
	rc := checkRCFor(t, 1, "", s.URL())
	run := runCheckP(t, rc, prof)
	applyY(t, rc, run)
	b := patchBodies(s)
	if len(b) != 1 {
		t.Fatalf("PATCH %d회", len(b))
	}
	assertBodySafe(t, prof, b[0], false)
	wantStatus(t, applyResults(t, run), s, "ProcHyperthreading", StatusApplied)
}

// ---- (j) 읽기 전용 경로는 ModeSet 을 쓰지 않는다 ----

// ModeSet·Patch·PostJob 은 redfish.go(정의)와 set.go(설정 경로)에만 나와야 한다.
func TestModeSetOnlyInSetPath(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil || len(files) == 0 {
		t.Fatal(err)
	}
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") || f == "redfish.go" || f == "set.go" {
			continue
		}
		fset := token.NewFileSet()
		af, err := parser.ParseFile(fset, f, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(af, func(n ast.Node) bool {
			switch x := n.(type) {
			case *ast.Ident:
				if x.Name == "ModeSet" {
					t.Errorf("%s:%d ModeSet 사용 (읽기 전용 경로)", f, fset.Position(x.Pos()).Line)
				}
			case *ast.CallExpr:
				if sel, ok := x.Fun.(*ast.SelectorExpr); ok && (sel.Sel.Name == "Patch" || sel.Sel.Name == "PostJob") {
					t.Errorf("%s:%d .%s 호출 (읽기 전용 경로)", f, fset.Position(x.Pos()).Line, sel.Sel.Name)
				}
			}
			return true
		})
	}
	// set.go 안에서도 ModeSet 은 apply 일 때 한 곳(runHost)과 계획 검증용 checkAllowed 에만 나온다.
	src, err := os.ReadFile("set.go")
	must(t, err)
	if n := strings.Count(string(src), "mode = ModeSet"); n != 1 || !strings.Contains(string(src), "if e.apply {\n\t\tmode = ModeSet") {
		t.Errorf("set.go 의 ModeSet 대입은 e.apply 분기 한 곳이어야 함 (%d)", n)
	}
}

// 실행 시에도: 같은 FAIL 호스트에 check·dump·allcheck·dry-run 을 돌려도 쓰기 0.
func TestReadOnlyPathsNeverWrite(t *testing.T) {
	fastDump(t)
	s := startMockTree(t, "dell-r660", nil)
	must(t, s.SetBiosAttr("LogicalProc", "Disabled"))
	rc := checkRCFor(t, 1, "", s.URL())
	run := runCheckP(t, rc, setProf(t, setProfText))
	must(t, doDryRun(rc, run, &bytes.Buffer{}))
	if _, err := runDump(dumpRC(t, 1, s.URL()), dumpOpts{Out: t.TempDir()}); err != nil {
		t.Fatal(err)
	}
	runAllT(t, 1, hostKey(s)+"\n", []string{s.URL()}, "")
	if s.Writes() != 0 || s.LogHits() != 0 {
		t.Errorf("읽기 전용 경로에서 Writes=%d LogHits=%d", s.Writes(), s.LogHits())
	}
}

// ---- (m) 규모: Farm 300대 FAIL → 모두 APPLIED, 동시 50 ----

func TestSetScaleFarm300(t *testing.T) {
	f, port := newFarmOrSkip(t, mockbmc.FarmOptions{})
	dellT, hpeT := loadTreeT(t, "dell-r660"), loadTreeT(t, "hpe-dl360gen11")
	const n = 300
	ips := make([]string, n)
	for i := range ips {
		ips[i] = farmIP(i)
		tree, over := dellT, map[string]interface{}{"LogicalProc": "Disabled", "LlcPrefetch": "Disabled"}
		if i%2 == 1 {
			tree, over = hpeT, map[string]interface{}{"ProcHyperthreading": "Disabled"}
		}
		if err := f.AddHost(ips[i], tree, mockbmc.HostOptions{User: testUser, Pass: testPass, AttrOverrides: over}); err != nil {
			t.Fatal(err)
		}
	}
	rc := farmRC(t, ips, port, 50, 3)
	prof := setProf(t, setProfText)
	run := runCheckP(t, rc, prof)
	fails := run.failItems()
	if len(fails) != n/2*3 {
		t.Fatalf("FAIL %d건, 기대 %d", len(fails), n/2*3)
	}
	start := time.Now()
	out := applyDirect(t, rc, run, fails)
	t.Logf("설정 %d대 %s", n, time.Since(start))
	res := applyResults(t, run)
	applied := 0
	for _, r := range res {
		if r.status == StatusApplied {
			applied++
		}
	}
	if applied != len(fails) {
		t.Errorf("APPLIED %d / %d", applied, len(fails))
	}
	for i, ip := range ips {
		h := f.Host(ip)
		if h.Count("PATCH", "/") != 1 {
			t.Errorf("%s: PATCH %d회", ip, h.Count("PATCH", "/"))
		}
		wantJobs, key, attr, val := 0, pendKeyHPE, "ProcHyperthreading", "Enabled"
		if i%2 == 0 {
			wantJobs, key, attr = 1, pendKeyDell, "LogicalProc"
		}
		if h.Count("POST", "/redfish/v1/Managers") != wantJobs || h.Pending()[key][attr] != val {
			t.Errorf("%s: Job %d, pending %v", ip, h.Count("POST", "/redfish/v1/Managers"), h.Pending()[key])
		}
		if ss := h.Sessions(); ss.Created != 2 || ss.Deleted != 2 {
			t.Errorf("%s: 세션 %+v (점검 1 + 설정 1)", ip, ss)
		}
	}
	tot := f.Totals()
	if tot.Writes != n+n/2 || tot.LogHits != 0 {
		t.Errorf("Farm 합계 Writes=%d LogHits=%d", tot.Writes, tot.LogHits)
	}
	if got := readLines(t, filepath.Join(run.Dir, "applied.txt")); len(got) != n {
		t.Errorf("applied.txt %d줄", len(got))
	}
	if !strings.Contains(out, "호스트가 많아 이름은 생략") {
		t.Errorf("list-max(100) 초과 시 이름 생략 안내가 없음")
	}
}

// ---- 공격자 관점 회귀 ----

// user.txt 의 서로 다른 줄이 같은 BMC 로 해석되어도 PATCH·Job 은 한 번만 나간다 (동시 실행 포함).
func TestSetDuplicateTargetWrittenOnce(t *testing.T) {
	s := startMockTree(t, "dell-r660", nil)
	must(t, s.SetBiosAttr("LogicalProc", "Disabled"))
	port := s.URL()[strings.LastIndex(s.URL(), ":")+1:]
	rc := checkRCFor(t, 2, "127.0.0.1 dupe-m\n", s.URL(), "dupe-m:"+port, "dupe:"+port)
	run := runCheckP(t, rc, setProf(t, setProfText))
	if n := len(run.failItems()); n != 3 {
		t.Fatalf("FAIL %d건 (대상 3줄이 모두 같은 BMC)", n)
	}
	before := snapOf(s)
	applyY(t, rc, run)
	now := snapOf(s)
	if now.patches-before.patches != 1 || now.jobs-before.jobs != 1 || now.sess.Created-before.sess.Created != 1 {
		t.Errorf("같은 BMC 에 PATCH %d / Job %d / 세션 %d, 기대 1/1/1",
			now.patches-before.patches, now.jobs-before.jobs, now.sess.Created-before.sess.Created)
	}
	var got []string
	for _, r := range readTSV(t, filepath.Join(run.Dir, "apply_result.tsv"))[1:] {
		got = append(got, r[7])
	}
	if fmt.Sprint(got) != fmt.Sprint([]string{StatusApplied, StatusDuplicateTarget, StatusDuplicateTarget}) {
		t.Errorf("상태 = %v", got)
	}
}

// 포트 생략(443)과 :443 도 같은 BMC 로 본다.
func TestApplyHostsDedupKey(t *testing.T) {
	a := Target{Input: "192.0.2.1", Hostname: "192.0.2.1", IP: "192.0.2.1"}
	b := Target{Input: "192.0.2.1:443", Hostname: "192.0.2.1:443", IP: "192.0.2.1", Port: "443"}
	c := Target{Input: "192.0.2.1:8443", Hostname: "192.0.2.1:8443", IP: "192.0.2.1", Port: "8443"}
	hs := applyHosts([]failItem{{Target: a, Attr: "x"}, {Target: b, Attr: "x"}, {Target: c, Attr: "x"}, {Target: a, Attr: "y"}})
	if len(hs) != 3 || hs[0].DupOf != "" || hs[1].DupOf != a.Hostname || hs[2].DupOf != "" || len(hs[0].Items) != 2 {
		t.Errorf("applyHosts = %+v %+v %+v", hs[0], hs[1], hs[2])
	}
}

// 정수 기대값은 정규형으로 보내고 같은 정규형으로 확인한다 ("05" → 5).
func TestSetIntegerCanonical(t *testing.T) {
	s := startMockTree(t, "lenovo-sr650v3", nil)
	must(t, s.SetBiosAttr("SystemRecovery_POSTWatchdogTimerValue", 7))
	prof := setProf(t, strings.Replace(setProfText, "SystemRecovery_POSTWatchdogTimerValue,5,Y", "SystemRecovery_POSTWatchdogTimerValue,05,Y", 1))
	rc := checkRCFor(t, 1, "", s.URL())
	run := runCheckP(t, rc, prof)
	applyY(t, rc, run)
	wantStatus(t, applyResults(t, run), s, "SystemRecovery_POSTWatchdogTimerValue", StatusApplied)
	if b := patchBodies(s); len(b) != 1 || !strings.Contains(b[0], `"SystemRecovery_POSTWatchdogTimerValue":5}`) {
		t.Errorf("PATCH 본문 = %v", b)
	}
}

// 본문을 만들 수 없는 프로파일(위험 속성명, 같은 속성 중복)은 보내지 않는다.
func TestSetUnsafeProfileNotSent(t *testing.T) {
	s := startMockTree(t, "dell-r660", nil)
	must(t, s.SetBiosAttr("LogicalProc", "Disabled"))
	info := &SystemInfo{Vendor: "Dell", Model: "PowerEdge R660", SettingsPath: dellSettings, ManagerPath: "/redfish/v1/Managers/iDRAC.Embedded.1"}
	mk := func(std, attr, val string) *setItem {
		b, _ := json.Marshal(val)
		return &setItem{Std: std, Attr: attr, Value: val, raw: b}
	}
	for _, c := range []struct {
		name  string
		items []*setItem
		want  string
	}{
		{"위험 속성명", []*setItem{mk("x", "SetupPassword", "a")}, StatusInvalidValue},
		{"기본값 복원류", []*setItem{mk("x", "LoadDefaults", "Yes")}, StatusInvalidValue},
		{"같은 속성 두 번", []*setItem{mk("a", "LogicalProc", "Enabled"), mk("b", "LogicalProc", "Disabled")}, StatusInvalidValue},
	} {
		p := buildPlan(info, true, true, "", c.items)
		if p.Err == "" || p.ErrStatus != c.want || p.ready() {
			t.Errorf("%s: 계획 %+v", c.name, p)
		}
	}
	// Settings 경로가 허용목록 밖이거나 Dell Manager 를 모르면 보내지 않는다.
	bad := *info
	bad.SettingsPath = "/redfish/v1/Systems/System.Embedded.1/Actions/ComputerSystem.Reset"
	if p := buildPlan(&bad, true, true, "", []*setItem{mk("h", "LogicalProc", "Enabled")}); p.ready() || p.ErrStatus != StatusSetUnsupported {
		t.Errorf("허용목록 밖 경로: %+v", p)
	}
	noMgr := *info
	noMgr.ManagerPath = ""
	if p := buildPlan(&noMgr, true, true, "", []*setItem{mk("h", "LogicalProc", "Enabled")}); p.ready() {
		t.Errorf("Dell Manager 없음: %+v", p)
	}
	// 정상 계획의 Job 본문에는 재부팅 관련 키가 절대 없다.
	p := buildPlan(info, true, true, "", []*setItem{mk("h", "LogicalProc", "Enabled")})
	var jb map[string]string
	must(t, json.Unmarshal(p.JobBody, &jb))
	keys := make([]string, 0, len(jb))
	for k := range jb {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	if fmt.Sprint(keys) != "[TargetSettingsURI]" {
		t.Errorf("Job 본문 키 = %v", keys)
	}
	if s.Writes() != 0 {
		t.Errorf("Writes = %d", s.Writes())
	}
}

// BMC 오류 메시지의 제어문자·긴 본문은 정리되어 기록된다. JSON 이스케이프는 풀지 않으므로(R8) \u001b 는 글자 그대로 남고
// 제어문자(ESC)는 터미널에 나가지 않는다.
func TestBMCMessageSanitized(t *testing.T) {
	err := &RFError{Status: StatusHTTPError, HTTP: 400, Detail: `{"error":{"@Message.ExtendedInfo":[{"Message":"bad \u001b[31mvalue"}],"message":"One or more"}}`}
	got := bmcMessage(err)
	if strings.ContainsRune(got, 0x1b) || !strings.Contains(got, `bad \u001b[31mvalue`) || !strings.Contains(got, "One or more") {
		t.Errorf("bmcMessage = %q", got)
	}
}
