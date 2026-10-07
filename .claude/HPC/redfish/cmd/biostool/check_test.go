package main

import (
	"bytes"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
	"time"

	"biostool/internal/mockbmc"
)

// check.go / report.go 시험. 실제 BMC 는 쓰지 않고 internal/mockbmc(testdata 합성 트리)와
// testdata/profiles-test/VM.tsv 만 쓴다.

var checkNow = time.Date(2026, 10, 7, 14, 2, 0, 0, time.UTC)

func fastCheck(t *testing.T) {
	t.Helper()
	old := checkClientGap
	checkClientGap = -1
	t.Cleanup(func() { checkClientGap = old })
}

func testProf(t *testing.T) *Profile {
	t.Helper()
	p, err := loadProfile(profFixture, "VM")
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// checkRCFor 는 입력(https:// 는 떼어 냄)을 해석해 runContext 를 만듭니다. 이름 입력은 hostsText 로 해석합니다.
func checkRCFor(t *testing.T, conc int, hostsText string, inputs ...string) *runContext {
	t.Helper()
	var in []string
	for _, u := range inputs {
		in = append(in, strings.TrimPrefix(u, "https://"))
	}
	ts, err := resolveTargets(in, writeHosts(t, hostsText))
	if err != nil {
		t.Fatal(err)
	}
	return &runContext{
		Conf:     &Config{User: testUser, Concurrency: conc, Timeout: 5 * time.Second, Insecure: true, Retries: 0},
		Targets:  ts,
		Password: testPass,
	}
}

func runCheckT(t *testing.T, rc *runContext, fromDump string) *checkRun {
	t.Helper()
	fastCheck(t)
	run, err := doCheck(rc, checkOpts{Profile: testProf(t), FromDump: fromDump, ResultDir: t.TempDir(), Now: checkNow})
	if err != nil {
		t.Fatalf("doCheck: %v", err)
	}
	return run
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func setPending(t *testing.T, s *mockbmc.Server, settingsPath string, kv map[string]interface{}) {
	t.Helper()
	must(t, s.Modify(settingsPath, func(m map[string]interface{}) {
		attrs, _ := m["Attributes"].(map[string]interface{})
		if attrs == nil {
			attrs = map[string]interface{}{}
			m["Attributes"] = attrs
		}
		for k, v := range kv {
			attrs[k] = v
		}
	}))
}

func itemOf(t *testing.T, h *hostCheck, std string) itemResult {
	t.Helper()
	for _, it := range h.Items {
		if it.Std == std {
			return it
		}
	}
	t.Fatalf("항목 %s 없음 (Status=%s Items=%+v)", std, h.Status, h.Items)
	return itemResult{}
}

func assertClean(t *testing.T, s *mockbmc.Server) {
	t.Helper()
	if s.Writes() != 0 {
		t.Errorf("Writes = %d, 기대 0", s.Writes())
	}
	if s.LogHits() != 0 {
		t.Errorf("LogHits = %d, 기대 0", s.LogHits())
	}
}

func assertOneSession(t *testing.T, s *mockbmc.Server) {
	t.Helper()
	if ss := s.Sessions(); ss.Created != 1 || ss.Deleted != 1 || ss.LoginFailed != 0 {
		t.Errorf("세션 = %+v, 기대 생성 1 / 삭제 1", ss)
	}
	if got := s.Count("POST", "/redfish/v1/SessionService/Sessions"); got != 1 {
		t.Errorf("세션 POST %d회, 기대 1", got)
	}
}

const dellSettings = "/redfish/v1/Systems/System.Embedded.1/Bios/Settings"

// (a)(b) 판정 표: 같은 프로파일로 Dell mock 의 상태만 바꿔 가며 항목별 결과를 확인한다.
func TestJudgmentTable(t *testing.T) {
	cases := []struct {
		name  string
		setup func(t *testing.T, s *mockbmc.Server)
		want  map[string]string // std → 결과 (나머지는 OK)
		extra func(t *testing.T, h *hostCheck)
	}{
		{"전부 OK (정확 일치 행이 * 행보다 우선)", nil, nil, nil},
		{"FAIL: 후보 2순위(LogicalProc) 사용", func(t *testing.T, s *mockbmc.Server) {
			must(t, s.SetBiosAttr("LogicalProc", "Disabled"))
		}, map[string]string{"hyper_threading": StatusFail}, func(t *testing.T, h *hostCheck) {
			it := itemOf(t, h, "hyper_threading")
			if it.Attr != "LogicalProc" || it.Expected != "Enabled" || it.Current != "Disabled" || it.Pending != "-" || !it.Verified {
				t.Errorf("FAIL 항목 내용: %+v", it)
			}
		}},
		{"FAIL: 후보 1순위가 BMC 에 있으면 그것을 쓴다", func(t *testing.T, s *mockbmc.Server) {
			must(t, s.SetBiosAttr("Hyperthreading", "Disabled"))
		}, map[string]string{"hyper_threading": StatusFail}, func(t *testing.T, h *hostCheck) {
			if it := itemOf(t, h, "hyper_threading"); it.Attr != "Hyperthreading" || it.Current != "Disabled" {
				t.Errorf("우선순위: %+v", it)
			}
		}},
		{"FAIL: model=* 행(Custom)이 아니라 정확 일치 행(PerfOptimized)", func(t *testing.T, s *mockbmc.Server) {
			must(t, s.SetBiosAttr("SysProfile", "Custom"))
		}, map[string]string{"system_profile": StatusFail}, nil},
		{"PENDING_OK (Dell 은 예약된 BIOS 설정 Job 이 있어야 함)", func(t *testing.T, s *mockbmc.Server) {
			must(t, s.SetBiosAttr("LogicalProc", "Disabled"))
			setPending(t, s, dellSettings, map[string]interface{}{"LogicalProc": "Enabled"})
			s.AddJob(dellJobs, dellSettings, "", "")
		}, map[string]string{"hyper_threading": StatusPendingOK}, func(t *testing.T, h *hostCheck) {
			if it := itemOf(t, h, "hyper_threading"); it.Pending != "Enabled" || it.Current != "Disabled" {
				t.Errorf("PENDING_OK 항목: %+v", it)
			}
		}},
		{"UNVERIFIED (verified=N 행)", func(t *testing.T, s *mockbmc.Server) {
			must(t, s.SetBiosAttr("SubNumaCluster", "Enabled"))
		}, map[string]string{"sub_numa_cluster": StatusUnverified}, nil},
		{"PENDING_EXISTS: 우리 항목이 아닌 속성의 Pending", func(t *testing.T, s *mockbmc.Server) {
			must(t, s.SetBiosAttr("LogicalProc", "Disabled"))
			setPending(t, s, dellSettings, map[string]interface{}{"ProcCStates": "Disabled"})
		}, map[string]string{"hyper_threading": StatusPendingExists}, func(t *testing.T, h *hostCheck) {
			if h.Foreign != 1 {
				t.Errorf("Foreign = %d, 기대 1", h.Foreign)
			}
			if h.Jobs < 0 {
				t.Errorf("Dell 은 Job 개수(정보용)를 읽어야 함: Jobs=%d", h.Jobs)
			}
		}},
		{"PENDING_EXISTS: 우리 속성에 기대·현재 아닌 Pending", func(t *testing.T, s *mockbmc.Server) {
			must(t, s.SetBiosAttr("LogicalProc", "Disabled"))
			setPending(t, s, dellSettings, map[string]interface{}{"LogicalProc": "Weird"})
		}, map[string]string{"hyper_threading": StatusPendingExists}, func(t *testing.T, h *hostCheck) {
			if it := itemOf(t, h, "hyper_threading"); it.Pending != "Weird" || h.Foreign != 0 {
				t.Errorf("항목 %+v Foreign=%d", it, h.Foreign)
			}
		}},
		{"Pending 값이 현재값과 같으면 변경이 아니므로 FAIL", func(t *testing.T, s *mockbmc.Server) {
			must(t, s.SetBiosAttr("LogicalProc", "Disabled"))
			setPending(t, s, dellSettings, map[string]interface{}{"ProcCStates": "Enabled", "LogicalProc": "Disabled"})
		}, map[string]string{"hyper_threading": StatusFail}, nil},
		{"남의 Pending 이 있어도 불일치가 없으면 전부 OK", func(t *testing.T, s *mockbmc.Server) {
			setPending(t, s, dellSettings, map[string]interface{}{"ProcCStates": "Disabled"})
		}, nil, func(t *testing.T, h *hostCheck) {
			if h.Foreign != 1 || h.Jobs != -1 {
				t.Errorf("Foreign=%d Jobs=%d", h.Foreign, h.Jobs)
			}
		}},
		{"UNVERIFIED 가 PENDING_EXISTS 보다 우선", func(t *testing.T, s *mockbmc.Server) {
			must(t, s.SetBiosAttr("SubNumaCluster", "Enabled"))
			setPending(t, s, dellSettings, map[string]interface{}{"ProcCStates": "Disabled"})
		}, map[string]string{"sub_numa_cluster": StatusUnverified}, nil},
		{"MAPPING_MISSING: 후보 속성이 BMC 에 없음", func(t *testing.T, s *mockbmc.Server) {
			must(t, s.SetBiosAttr("LlcPrefetch", nil))
		}, map[string]string{"llc_prefetch": StatusMappingMissing}, func(t *testing.T, h *hostCheck) {
			it := itemOf(t, h, "llc_prefetch")
			if it.Attr != "LlcPrefetch" || it.Current != "-" || !strings.Contains(it.Reason, "BMC 에 없음") {
				t.Errorf("MAPPING_MISSING 항목: %+v", it)
			}
			if len(h.Cands) != 4 {
				t.Errorf("후보 탐색 결과가 없음: %+v", h.Cands)
			}
		}},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			s := startMockTree(t, "dell-r660", nil)
			if c.setup != nil {
				c.setup(t, s)
			}
			run := runCheckT(t, checkRCFor(t, 1, "", s.URL()), "")
			h := run.Hosts[0]
			if h.Status != "" || len(h.Items) != 5 {
				t.Fatalf("Status=%q Detail=%q 항목 %d개", h.Status, h.Detail, len(h.Items))
			}
			for _, it := range h.Items {
				want := StatusOK
				if w, ok := c.want[it.Std]; ok {
					want = w
				}
				if it.Result != want {
					t.Errorf("%s = %s (%s), 기대 %s", it.Std, it.Result, it.Reason, want)
				}
			}
			if c.extra != nil {
				c.extra(t, h)
			}
			assertClean(t, s)
			assertOneSession(t, s)
		})
	}
}

// (b) A|B 허용값, 모델 정규화(ProLiant DL360 Gen11 ↔ DL360 Gen11), HPE * 폴백(비교만, UNVERIFIED), 모델별 전용 행.
func TestAllowedValuesAndModelFallback(t *testing.T) {
	cases := []struct {
		tree, attr, val string
		std, want       string
	}{
		{"hpe-dl360gen11", "WorkloadProfile", "GeneralPowerEfficientCompute", "system_profile", StatusOK},
		{"hpe-dl360gen11", "WorkloadProfile", "GeneralThroughputCompute", "system_profile", StatusOK}, // A|B 의 B
		{"hpe-dl360gen11", "WorkloadProfile", "GeneralPeakFrequencyCompute", "system_profile", StatusFail},
		{"hpe-dl360gen11", "ProcHyperthreading", "Disabled", "hyper_threading", StatusFail},      // Gen11 정확 행
		{"hpe-dl360gen10", "ProcHyperthreading", "Disabled", "hyper_threading", StatusFail},      // Gen10 정확 행
		{"hpe-dl360gen10", "SubNumaClustering", "Enabled", "sub_numa_cluster", StatusUnverified}, // 정확 행 없음 → HPE * 행으로 비교, * 행은 verified=N
		{"hpe-dl360gen10", "WorkloadProfile", "GeneralPeakFrequencyCompute", "system_profile", StatusOK},
		{"hpe-dl360gen10", "WorkloadProfile", "GeneralPowerEfficientCompute", "system_profile", StatusFail}, // Gen10 전용 행
	}
	for _, c := range cases {
		s := startMockTree(t, c.tree, nil)
		must(t, s.SetBiosAttr(c.attr, c.val))
		h := runCheckT(t, checkRCFor(t, 1, "", s.URL()), "").Hosts[0]
		if got := itemOf(t, h, c.std).Result; got != c.want {
			t.Errorf("%s %s=%s: %s = %s, 기대 %s", c.tree, c.attr, c.val, c.std, got, c.want)
		}
	}
}

// (c) 호스트 오류가 있어도 나머지는 끝까지 점검하고, 상태와 retry.txt 가 정확해야 한다.
func TestHostErrorsDoNotStopOthers(t *testing.T) {
	good := startMockTree(t, "dell-r660", nil)
	badAuth := startMockTree(t, "hpe-dl360gen11", func(o *mockbmc.Options) { o.Pass = "다른-비밀번호" })
	svc503 := startMockTree(t, "lenovo-sr650v3", nil)
	svc503.SetFault("/redfish/v1/Systems*", mockbmc.Fault{Status: 503})
	unsup := startMockTree(t, "supermicro-x12", nil)
	unsup.Remove("/redfish/v1")
	dead := startMockTree(t, "cisco-c220m7", nil)
	deadURL := dead.URL()
	dead.Close()

	rc := checkRCFor(t, 3, "192.0.2.1 other-m\n", good.URL(), badAuth.URL(), svc503.URL(), unsup.URL(), deadURL, "ghost")
	run := runCheckT(t, rc, "")

	wantStatus := []string{"", StatusAuthFail, StatusBMCError, StatusUnsupported, StatusUnreachable, StatusNoHostsEntry}
	for i, w := range wantStatus {
		if run.Hosts[i].Status != w {
			t.Errorf("호스트 %d (%s) 상태 %q, 기대 %q (%s)", i, run.Hosts[i].Target.Input, run.Hosts[i].Status, w, run.Hosts[i].Detail)
		}
	}
	if !run.Hosts[0].allGood() {
		t.Errorf("정상 호스트가 끝까지 점검되지 않음: %+v", run.Hosts[0].Items)
	}
	// 401 은 재시도 없이 로그인 1번, 만들어진 세션이 없으니 삭제도 없다.
	if ss := badAuth.Sessions(); ss.LoginFailed != 1 || ss.Created != 0 || badAuth.Count("POST", "/redfish/v1/SessionService/Sessions") != 1 {
		t.Errorf("AUTH_FAIL 호스트 세션: %+v POST=%d", ss, badAuth.Count("POST", "/redfish/v1/SessionService/Sessions"))
	}
	assertOneSession(t, good)
	assertOneSession(t, svc503) // 점검이 실패해도 자기 세션은 정리한다
	assertOneSession(t, unsup)
	for _, s := range []*mockbmc.Server{good, badAuth, svc503, unsup} {
		assertClean(t, s)
	}

	read := func(name string) string {
		b, err := os.ReadFile(filepath.Join(run.Dir, name))
		must(t, err)
		return string(b)
	}
	wantRetry := strings.TrimPrefix(svc503.URL(), "https://") + "\n" + strings.TrimPrefix(deadURL, "https://") + "\n"
	if got := read("retry.txt"); got != wantRetry {
		t.Errorf("retry.txt = %q, 기대 %q (BMC_ERROR, UNREACHABLE 만, 입력 형식 그대로)", got, wantRetry)
	}
	if got := read("ok.txt"); got != run.Hosts[0].Target.Hostname+"\n" {
		t.Errorf("ok.txt = %q", got)
	}
	rows := strings.Split(strings.TrimSpace(read("result.tsv")), "\n")
	if len(rows) != 1+5+5 { // 머리글 + 정상 호스트 5항목 + 오류 호스트 5행
		t.Fatalf("result.tsv 행 %d", len(rows))
	}
	errRows := 0
	for _, r := range rows[1:] {
		f := strings.Split(r, "\t")
		if f[6] == "-" {
			errRows++
			if f[7] != "-" || f[8] != "-" || f[9] != "-" || f[10] != "-" || f[11] == "OK" {
				t.Errorf("호스트 오류 행: %q", r)
			}
		}
	}
	if errRows != 5 {
		t.Errorf("호스트 오류 행 %d개, 기대 5", errRows)
	}
	info := read("run_info.txt")
	for _, w := range []string{"대상 수: 6", "AUTH_FAIL: 1", "BMC_ERROR: 1", "UNREACHABLE: 1", "NO_HOSTS_ENTRY: 1", "UNSUPPORTED: 1"} {
		if !strings.Contains(info, w) {
			t.Errorf("run_info.txt 에 %q 없음:\n%s", w, info)
		}
	}
}

// (d) result.tsv 형식·행 수·권한, 값 안의 탭/개행 치환.
func TestResultFilesFormat(t *testing.T) {
	var urls []string
	var servers []*mockbmc.Server
	for _, name := range mockTrees {
		s := startMockTree(t, name, nil)
		servers = append(servers, s)
		urls = append(urls, s.URL())
	}
	must(t, servers[0].SetBiosAttr("LogicalProc", "Dis\tabled\nx")) // dell: FAIL, 값에 탭·개행
	run := runCheckT(t, checkRCFor(t, 4, "", urls...), "")

	if m := regexp.MustCompile(`^\d{8}_\d{6}$`).MatchString(filepath.Base(run.Dir)); !m || filepath.Base(run.Dir) != "20261007_140200" {
		t.Errorf("결과 폴더 이름 %q", run.Dir)
	}
	b, err := os.ReadFile(filepath.Join(run.Dir, "result.tsv"))
	must(t, err)
	lines := strings.Split(strings.TrimSuffix(string(b), "\n"), "\n")
	const header = "hostname\tip\tprofile\tvendor\tmodel\tbios_version\tstd_name\tattribute\texpected\tcurrent\tpending\tresult"
	if lines[0] != header {
		t.Errorf("머리글 = %q", lines[0])
	}
	if len(lines) != 1+6*5 {
		t.Fatalf("result.tsv 행 %d, 기대 %d (호스트 6 × 항목 5 + 머리글)", len(lines), 1+30)
	}
	counts := map[string]int{}
	for _, l := range lines[1:] {
		f := strings.Split(l, "\t")
		if len(f) != 12 {
			t.Fatalf("열 %d개: %q", len(f), l)
		}
		counts[f[0]]++
		if f[2] != "VM" {
			t.Errorf("profile 열: %q", l)
		}
		if f[6] == "hyper_threading" && f[3] == "Dell" && f[9] != "Dis abled x" {
			t.Errorf("탭/개행 치환 실패: current = %q", f[9])
		}
	}
	for h, n := range counts {
		if n != 5 {
			t.Errorf("%s 행 %d개, 기대 5", h, n)
		}
	}

	// fail.tsv: FAIL 1(dell) + lenovo MAPPING_MISSING 5 + gen10 llc_prefetch MAPPING_MISSING 1
	fb, _ := os.ReadFile(filepath.Join(run.Dir, "fail.tsv"))
	fl := strings.Split(strings.TrimSuffix(string(fb), "\n"), "\n")
	if fl[0] != header || len(fl) != 1+7 {
		t.Errorf("fail.tsv: %d줄\n%s", len(fl), fb)
	}
	// ok.txt: dell(FAIL), lenovo(MM), gen10(MM) 제외 → 3대
	ob, _ := os.ReadFile(filepath.Join(run.Dir, "ok.txt"))
	if n := len(strings.Fields(string(ob))); n != 3 {
		t.Errorf("ok.txt %d대, 기대 3:\n%s", n, ob)
	}

	if runtime.GOOS != "windows" {
		for _, f := range []string{"result.tsv", "ok.txt", "fail.tsv", "retry.txt", "run_info.txt"} {
			st, err := os.Stat(filepath.Join(run.Dir, f))
			if err != nil {
				t.Errorf("%s: %v", f, err)
			} else if st.Mode().Perm() != 0o600 {
				t.Errorf("%s 권한 %v, 기대 0600", f, st.Mode().Perm())
			}
		}
	}
}

// (e) 6개 트리 모두: 쓰기 0, 로그 접근 0, 세션 호스트당 1·1, 호출 수가 작다 (MAPPING_MISSING 덤프 호스트 포함).
func TestReadOnlyAndSessionCountsAllTrees(t *testing.T) {
	var urls []string
	var servers []*mockbmc.Server
	for _, name := range mockTrees {
		s := startMockTree(t, name, nil)
		servers = append(servers, s)
		urls = append(urls, s.URL())
	}
	run := runCheckT(t, checkRCFor(t, 3, "", urls...), "")
	for i, s := range servers {
		assertClean(t, s)
		assertOneSession(t, s)
		if run.Hosts[i].Status != "" {
			t.Errorf("%s: %s", mockTrees[i], run.Hosts[i].Status)
		}
	}
	// 매핑이 모두 있는 Dell 은 root, Systems, System, Bios, Registries/<id>, Settings 정도만 읽는다.
	if g := servers[0].Count("GET", "/"); g > 7 {
		t.Errorf("Dell GET %d회, 기대 7 이하: %+v", g, servers[0].Calls())
	}
	if run.MMModels != 2 { // lenovo(행 없음), gen10(LlcPrefetch 없음)
		t.Errorf("MMModels = %d, 기대 2", run.MMModels)
	}
}

// (f) MAPPING_MISSING: 덤프는 모델당 1대, 레지스트리는 모델당 1번, 후보 이름 표시.
func TestMappingMissingDumpOncePerModel(t *testing.T) {
	l1 := startMockTree(t, "lenovo-sr650v3", nil)
	l2 := startMockTree(t, "lenovo-sr650v3", nil)
	g10 := startMockTree(t, "hpe-dl360gen10", nil)
	dell := startMockTree(t, "dell-r660", nil)
	run := runCheckT(t, checkRCFor(t, 1, "", l1.URL(), l2.URL(), g10.URL(), dell.URL()), "")

	if run.MMModels != 2 {
		t.Errorf("MMModels = %d, 기대 2", run.MMModels)
	}
	dumped := []bool{true, false, true, false}
	for i, h := range run.Hosts {
		if (h.Dump != "") != dumped[i] {
			t.Errorf("호스트 %d Dump = %q, 덤프 저장 기대 %v", i, h.Dump, dumped[i])
		}
	}
	mm := filepath.Join(run.Dir, "mapping_missing")
	hostDirs := 0
	err := filepath.WalkDir(mm, func(p string, d fs.DirEntry, err error) error {
		if err == nil && d.IsDir() && d.Name() == "redfish" {
			if _, serr := os.Stat(filepath.Join(p, "v1.json")); serr == nil {
				hostDirs++
			}
		}
		return err
	})
	must(t, err)
	if hostDirs != 2 {
		t.Errorf("덤프 호스트 폴더 %d개, 기대 2", hostDirs)
	}
	sum, err := os.ReadFile(filepath.Join(mm, "summary.txt"))
	must(t, err)
	for _, w := range []string{"ThinkSystem SR650 V3", "DL360 Gen10"} {
		if !strings.Contains(string(sum), w) {
			t.Errorf("summary.txt 에 %q 없음:\n%s", w, sum)
		}
	}
	idx, err := os.ReadFile(filepath.Join(mm, "index.tsv"))
	must(t, err)
	if n := len(strings.Split(strings.TrimSpace(string(idx)), "\n")); n != 3 {
		t.Errorf("index.tsv %d줄, 기대 3 (머리글 + 2)", n)
	}
	// 레지스트리 JSON: 첫 Lenovo 만 받고(후보 표시 + 덤프), 둘째는 캐시로 받지 않는다.
	if n := l1.Count("GET", "/redfish/v1/RegistryStore"); n < 1 {
		t.Errorf("첫 Lenovo 가 레지스트리를 받지 않음")
	}
	if n := l2.Count("GET", "/redfish/v1/RegistryStore"); n != 0 {
		t.Errorf("둘째 Lenovo 가 레지스트리를 %d번 받음 (캐시 기대)", n)
	}
	for _, s := range []*mockbmc.Server{l1, l2, g10, dell} {
		assertClean(t, s)
		assertOneSession(t, s) // 덤프 호스트도 같은 세션을 쓴다
	}

	// 후보 이름이 리포트에 나온다 (판정은 하지 않음)
	var out bytes.Buffer
	writeReport(&out, run, reportOpts{ListMax: 100, FailMax: 200})
	for _, w := range []string{"MAPPING_MISSING", "후보: ", "Processors_HyperThreading=Enable", "mapping_missing"} {
		if !strings.Contains(out.String(), w) {
			t.Errorf("리포트에 %q 없음:\n%s", w, out.String())
		}
	}
	if it := itemOf(t, run.Hosts[0], "hyper_threading"); it.Result != StatusMappingMissing || it.Attr != "-" || it.Expected != "-" {
		t.Errorf("행이 없는 모델의 항목: %+v", it)
	}
}

// (g) -from-dump: dump 결과 폴더를 입력으로 온라인과 같은 판정, 서버 없이 동작, 덤프 없는 대상은 NO_DUMP.
func TestFromDumpMatchesOnline(t *testing.T) {
	fastDump(t)
	trees := []string{"dell-r660", "hpe-dl360gen11", "lenovo-sr650v3", "cisco-c220m7", "supermicro-x12", "hpe-dl360gen10"}
	var servers []*mockbmc.Server
	var urls []string
	for _, n := range trees {
		s := startMockTree(t, n, nil)
		servers = append(servers, s)
		urls = append(urls, s.URL())
	}
	must(t, servers[0].SetBiosAttr("LogicalProc", "Disabled"))
	setPending(t, servers[0], dellSettings, map[string]interface{}{"ProcCStates": "Disabled"}) // PENDING_EXISTS
	must(t, servers[1].SetBiosAttr("WorkloadProfile", "GeneralThroughputCompute"))             // A|B
	must(t, servers[3].SetBiosAttr("SNC", "enabled"))                                          // UNVERIFIED
	must(t, servers[4].SetBiosAttr("Hyper-Threading[ALL]", "Disable"))                         // UNVERIFIED

	out := t.TempDir()
	if _, err := runDump(dumpRC(t, 3, urls...), dumpOpts{Out: out}); err != nil {
		t.Fatal(err)
	}
	online := runCheckT(t, checkRCFor(t, 3, "", urls...), "")
	for _, s := range servers {
		s.Close() // 이후 오프라인 점검은 서버 없이 해야 한다
	}

	offIn := append(append([]string(nil), urls...), "127.0.0.1:1")
	offRC := checkRCFor(t, 3, "", offIn...)
	offRC.Password = "" // 오프라인은 비밀번호가 필요 없다
	offRC.Conf.User = ""
	offline := runCheckT(t, offRC, out)

	for i := range urls {
		a, b := online.rows(online.Hosts[i]), offline.rows(offline.Hosts[i])
		if fmt.Sprint(a) != fmt.Sprint(b) {
			t.Errorf("%s: 온라인과 오프라인 결과가 다름\n온라인:  %v\n오프라인: %v", trees[i], a, b)
		}
	}
	if st := offline.Hosts[len(urls)].Status; st != StatusNoDump {
		t.Errorf("덤프 없는 대상 = %q, 기대 NO_DUMP", st)
	}
	if got := itemOf(t, offline.Hosts[0], "hyper_threading").Result; got != StatusPendingExists {
		t.Errorf("오프라인 Dell hyper_threading = %s", got)
	}
	if got := itemOf(t, offline.Hosts[1], "system_profile").Result; got != StatusOK {
		t.Errorf("오프라인 HPE A|B = %s", got)
	}
	if len(offline.Hosts[2].Cands) == 0 || offline.Hosts[2].Dump != "" {
		t.Errorf("오프라인 MAPPING_MISSING: 후보 %d, Dump=%q (후보만 있고 덤프는 저장하지 않아야 함)", len(offline.Hosts[2].Cands), offline.Hosts[2].Dump)
	}
	if _, err := os.Stat(filepath.Join(offline.Dir, "mapping_missing")); err == nil {
		t.Error("오프라인 모드에서 mapping_missing 폴더가 만들어짐")
	}
	ri, _ := os.ReadFile(filepath.Join(offline.Dir, "run_info.txt"))
	if !strings.Contains(string(ri), "덤프 읽기") || !strings.Contains(string(ri), "NO_DUMP: 1") {
		t.Errorf("run_info.txt:\n%s", ri)
	}

	// 덤프 모드 리포트와 프롬프트: 설정 불가 안내만, 묻지 않는다.
	var rep bytes.Buffer
	writeReport(&rep, offline, reportOpts{ListMax: 100, FailMax: 200})
	if !strings.Contains(rep.String(), "덤프 읽기 모드") || !strings.Contains(rep.String(), "NO_DUMP") {
		t.Errorf("오프라인 리포트:\n%s", rep.String())
	}
}

// -from-dump 폴더 탐색: 폴더 이름 = 호스트 이름(포트는 _), 같은 이름이 둘이면 최신 v1.json, 대소문자 무시.
func TestIndexDumpHosts(t *testing.T) {
	root := t.TempDir()
	mk := func(rel string, mod time.Time) string {
		p := filepath.Join(root, filepath.FromSlash(rel), "redfish")
		must(t, os.MkdirAll(p, 0o755))
		f := filepath.Join(p, "v1.json")
		must(t, os.WriteFile(f, []byte("{}"), 0o644))
		must(t, os.Chtimes(f, mod, mod))
		return filepath.Join(root, filepath.FromSlash(rel))
	}
	old := time.Now().Add(-time.Hour)
	mk("Dell/R660/1.0/host0001", old)
	newer := mk("Dell/R660/2.0/Host0001", time.Now())
	port := mk("HPE/DL360/1/127.0.0.1_8443", old)
	must(t, os.MkdirAll(filepath.Join(root, "x/redfish"), 0o755)) // v1.json 없는 폴더는 무시
	idx, err := indexDumpHosts(root)
	must(t, err)
	if len(idx) != 2 || idx["host0001"] != newer || idx["127.0.0.1_8443"] != port {
		t.Errorf("indexDumpHosts = %v", idx)
	}
	if _, err := indexDumpHosts(filepath.Join(root, "없음")); err == nil {
		t.Error("없는 폴더인데 오류가 없음")
	}
}

// (h) Y/N 프롬프트.
func TestConfirmApplyPrompt(t *testing.T) {
	oldApply := applyFails
	defer func() { applyFails = oldApply }()
	var calls [][]failItem
	applyFails = func(rc *runContext, _ *checkRun, fails []failItem, _ io.Writer, _ reportOpts) error {
		calls = append(calls, fails)
		return nil
	}

	s := startMockTree(t, "dell-r660", nil)
	must(t, s.SetBiosAttr("LogicalProc", "Disabled"))
	rc := checkRCFor(t, 1, "", s.URL())
	run := runCheckT(t, rc, "")
	if n := len(run.failItems()); n != 1 {
		t.Fatalf("FAIL 항목 %d개", n)
	}
	const prompt = "FAIL 1건(호스트 1대)을 Pending 으로 설정하시겠습니까? 재부팅은 하지 않습니다. (Y/N): "

	for _, c := range []struct {
		in   string
		want bool
	}{
		{"Y\n", true}, {"y\n", true}, {"Y", true}, {"  Y  \n", true},
		{"n\n", false}, {"N\n", false}, {"", false}, {"\n", false}, {"yes\n", false}, {"예\n", false}, {"Yes please\n", false},
	} {
		calls = nil
		var out bytes.Buffer
		if err := confirmApply(rc, run, strings.NewReader(c.in), &out, false, reportOpts{}); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out.String(), prompt) {
			t.Errorf("입력 %q: 프롬프트가 없음: %q", c.in, out.String())
		}
		if got := len(calls) == 1; got != c.want {
			t.Errorf("입력 %q: applyFails 호출 %d회, 기대 호출=%v", c.in, len(calls), c.want)
		}
		if c.want {
			f := calls[0][0]
			if f.Attr != "LogicalProc" || f.Expected != "Enabled" || f.Current != "Disabled" || f.Std != "hyper_threading" || f.Info == nil || f.Info.SettingsPath == "" {
				t.Errorf("전달된 FAIL 항목: %+v", f)
			}
		}
	}

	// -no-prompt 와 덤프 모드: 입력을 읽지 않고 호출도 없다.
	calls = nil
	var out bytes.Buffer
	if err := confirmApply(rc, run, failReader{t}, &out, true, reportOpts{}); err != nil || len(calls) != 0 || strings.Contains(out.String(), "(Y/N)") {
		t.Errorf("-no-prompt: err=%v calls=%d out=%q", err, len(calls), out.String())
	}
	dumpRun := *run
	dumpRun.FromDump = "dumps"
	out.Reset()
	if err := confirmApply(rc, &dumpRun, failReader{t}, &out, false, reportOpts{}); err != nil || len(calls) != 0 || strings.Contains(out.String(), "(Y/N)") {
		t.Errorf("덤프 모드: err=%v calls=%d out=%q", err, len(calls), out.String())
	}

	// FAIL 이 없으면 묻지 않는다.
	ok := startMockTree(t, "dell-r660", nil)
	okRun := runCheckT(t, checkRCFor(t, 1, "", ok.URL()), "")
	out.Reset()
	if err := confirmApply(rc, okRun, failReader{t}, &out, false, reportOpts{}); err != nil || len(calls) != 0 || out.Len() != 0 {
		t.Errorf("FAIL 0건: err=%v calls=%d out=%q", err, len(calls), out.String())
	}

	// UNVERIFIED·PENDING_EXISTS·MAPPING_MISSING 만 있으면 FAIL(설정 가능)이 아니므로 묻지 않는다.
	blocked := startMockTree(t, "dell-r660", nil)
	must(t, blocked.SetBiosAttr("SubNumaCluster", "Enabled"))
	must(t, blocked.SetBiosAttr("LlcPrefetch", nil))
	bRun := runCheckT(t, checkRCFor(t, 1, "", blocked.URL()), "")
	out.Reset()
	if err := confirmApply(rc, bRun, failReader{t}, &out, false, reportOpts{}); err != nil || len(calls) != 0 || out.Len() != 0 {
		t.Errorf("설정 불가만 있는 경우: err=%v calls=%d out=%q", err, len(calls), out.String())
	}
}

type failReader struct{ t *testing.T }

func (f failReader) Read([]byte) (int, error) {
	f.t.Error("묻지 않아야 하는데 입력을 읽음")
	return 0, io.EOF
}

// (i) 리포트와 결과 파일 어디에도 비밀번호·토큰이 없다.
func TestNoSecretsInOutput(t *testing.T) {
	good := startMockTree(t, "dell-r660", nil)
	must(t, good.SetBiosAttr("LogicalProc", "Disabled"))
	badAuth := startMockTree(t, "hpe-dl360gen11", func(o *mockbmc.Options) { o.Pass = "다른-비밀번호" })
	lenovo := startMockTree(t, "lenovo-sr650v3", nil)
	fine := startMockTree(t, "cisco-c220m7", nil) // OK 호스트 (초록색 확인용)
	run := runCheckT(t, checkRCFor(t, 2, "", good.URL(), badAuth.URL(), lenovo.URL(), fine.URL()), "")

	var texts []string
	for _, color := range []bool{false, true} {
		var rep bytes.Buffer
		writeReport(&rep, run, reportOpts{ListMax: 100, FailMax: 200, Color: color})
		texts = append(texts, rep.String())
	}
	var conf bytes.Buffer
	confirmApply(&runContext{}, run, strings.NewReader("n\n"), &conf, false, reportOpts{})
	texts = append(texts, conf.String())
	err := filepath.WalkDir(run.Dir, func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			b, rerr := os.ReadFile(p)
			must(t, rerr)
			texts = append(texts, string(b))
		}
		return err
	})
	must(t, err)
	for _, text := range texts {
		for _, secret := range []string{testPass, "S3cr3t", "다른-비밀번호", "X-Auth-Token", "Authorization", "Basic "} {
			if strings.Contains(text, secret) {
				t.Errorf("출력에 %q 가 있음", secret)
			}
		}
	}
	if strings.Contains(texts[0], "\x1b") {
		t.Error("색이 꺼져 있는데 ANSI 코드가 있음")
	}
	if !strings.Contains(texts[1], "\x1b[31m") || !strings.Contains(texts[1], "\x1b[32m") {
		t.Error("색이 켜져 있는데 빨강/초록이 없음")
	}
}

// 리포트 구성: 헤더·집계·섹션·기타 오류·결과 파일 안내.
func TestReportLayout(t *testing.T) {
	d := startMockTree(t, "dell-r660", nil)
	must(t, d.SetBiosAttr("LogicalProc", "Disabled"))
	g10 := startMockTree(t, "hpe-dl360gen10", nil)
	okHost := startMockTree(t, "cisco-c220m7", nil)
	dead := startMockTree(t, "supermicro-x12", nil)
	deadURL := dead.URL()
	dead.Close()
	run := runCheckT(t, checkRCFor(t, 1, "", d.URL(), g10.URL(), okHost.URL(), deadURL), "")

	var out bytes.Buffer
	writeReport(&out, run, reportOpts{ListMax: 100, FailMax: 200})
	text := out.String()
	for _, w := range []string{
		"== BIOS 점검 결과 (profile=VM, 대상 4 / 2026-10-07 14:02) ==",
		"[OK] 1대", "[FAIL 상세] 1건 (호스트 1대)", "Dell PowerEdge R660", "hyper_threading", "LogicalProc", "기대=Enabled", "현재=Disabled",
		"[설정 불가] 1건 (호스트 1대)", "llc_prefetch", "MAPPING_MISSING", "[기타 오류]", "UNREACHABLE", "retry.txt", "ok.txt", "fail.tsv",
	} {
		if !strings.Contains(text, w) {
			t.Errorf("리포트에 %q 없음:\n%s", w, text)
		}
	}
	// 집계는 호스트 대표 상태 기준
	for _, w := range []string{"OK                     1", "FAIL                   1", "MAPPING_MISSING         1", "기타 오류                 1"} {
		if !strings.Contains(strings.Join(strings.Fields(text), " "), strings.Join(strings.Fields(w), " ")) {
			t.Errorf("집계 줄 %q 없음", w)
		}
	}
}

func syntheticRun(hosts int, mod func(i int, h *hostCheck)) *checkRun {
	run := &checkRun{Profile: "VM", Now: checkNow, Dir: "results/20261007_140200"}
	for i := 0; i < hosts; i++ {
		h := newHostCheck(Target{Input: fmt.Sprintf("host%04d", i), Hostname: fmt.Sprintf("host%04d", i), IP: "192.0.2.1"})
		h.Info = &SystemInfo{Vendor: "Dell", Model: "PowerEdge R660"}
		h.Items = []itemResult{{Std: "hyper_threading", Attr: "LogicalProc", Expected: "Enabled", Current: "Enabled", Pending: "-", Result: StatusOK}}
		if mod != nil {
			mod(i, h)
		}
		run.Hosts = append(run.Hosts, h)
	}
	return run
}

func TestReportLimits(t *testing.T) {
	// OK 호스트가 list-max 를 넘으면 이름 대신 개수와 ok.txt 경로
	run := syntheticRun(150, nil)
	var out bytes.Buffer
	writeReport(&out, run, reportOpts{ListMax: 100, FailMax: 200})
	if !strings.Contains(out.String(), "[OK] 150대") || strings.Contains(out.String(), "host0001") || !strings.Contains(out.String(), "ok.txt") {
		t.Errorf("list-max 초과:\n%s", out.String())
	}
	// list-max 이내이고 줄 수가 20 미만이면 이름을 나열
	out.Reset()
	writeReport(&out, syntheticRun(30, nil), reportOpts{ListMax: 100, FailMax: 200})
	if !strings.Contains(out.String(), "host0001") || !strings.Contains(out.String(), "host0029") {
		t.Errorf("OK 이름 나열:\n%s", out.String())
	}
	// 이름이 길어 20줄 이상이 되면 개수만
	out.Reset()
	writeReport(&out, syntheticRun(400, nil), reportOpts{ListMax: 1000, FailMax: 200})
	if strings.Contains(out.String(), "host0001") || !strings.Contains(out.String(), "이름은 생략") {
		t.Errorf("20줄 이상 규칙:\n%s", out.String())
	}
	// fail-max 초과분은 fail.tsv 안내
	failRun := syntheticRun(5, func(i int, h *hostCheck) {
		h.Items[0].Current, h.Items[0].Result = "Disabled", StatusFail
	})
	out.Reset()
	writeReport(&out, failRun, reportOpts{ListMax: 100, FailMax: 2})
	if !strings.Contains(out.String(), "[FAIL 상세] 5건 (호스트 5대)") || !strings.Contains(out.String(), "... 외 3건") ||
		strings.Contains(out.String(), "host0002") || !strings.Contains(out.String(), "host0001") {
		t.Errorf("fail-max:\n%s", out.String())
	}
	if len(failRun.failItems()) != 5 {
		t.Errorf("failItems %d개", len(failRun.failItems()))
	}
}

func TestCommaInt(t *testing.T) {
	for in, want := range map[int]string{0: "0", 7: "7", 999: "999", 1000: "1,000", 1180: "1,180", 1234567: "1,234,567", -1234: "-1,234"} {
		if got := commaInt(in); got != want {
			t.Errorf("commaInt(%d) = %q, 기대 %q", in, got, want)
		}
	}
}

// 동시 실행(-race 용): 여러 호스트를 워커 여러 개로 점검해도 호스트당 세션 1·1, 결과는 입력 순서.
func TestConcurrentCheck(t *testing.T) {
	var urls []string
	var servers []*mockbmc.Server
	for i := 0; i < 12; i++ {
		tree := "dell-r660"
		if i%3 == 1 {
			tree = "lenovo-sr650v3" // MAPPING_MISSING (덤프·캐시 경로도 동시에 탄다)
		}
		s := startMockTree(t, tree, nil)
		servers = append(servers, s)
		urls = append(urls, s.URL())
	}
	run := runCheckT(t, checkRCFor(t, 6, "", urls...), "")
	var names []string
	for i, h := range run.Hosts {
		names = append(names, h.Target.Input)
		assertClean(t, servers[i])
		assertOneSession(t, servers[i])
		if h.Status != "" || len(h.Items) != 5 {
			t.Errorf("호스트 %d: %q %d", i, h.Status, len(h.Items))
		}
	}
	want := make([]string, len(urls))
	for i, u := range urls {
		want[i] = strings.TrimPrefix(u, "https://")
	}
	if fmt.Sprint(names) != fmt.Sprint(want) {
		t.Errorf("결과 순서가 입력 순서와 다름: %v", names)
	}
	if run.MMModels != 1 {
		t.Errorf("MMModels = %d, 기대 1 (Lenovo 4대 중 1대만 덤프)", run.MMModels)
	}
}
