package main

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"biostool/internal/ucsmxml"
)

// 모든 테스트는 httptest 가짜 UCSM 으로만 한다 (실제 UCSM 접속 금지).
// 계정은 redfish_test.go 의 testUser/testPass 를 그대로 쓴다.

const ucsmTestCookie = "1700000000/ck-SECRET-7a6b5c"

// ucsmMock 은 aaaLogin / configResolveClass / aaaLogout 만 아는 가짜 UCSM(/nuova) 입니다.
// 설정 필드는 서버 시작 전에만 쓴다.
type ucsmMock struct {
	authFail bool              // aaaLogin 을 항상 errorCode=551 로 거부 (입력 비밀번호를 errorDescr 에 되돌림)
	classErr map[string]bool   // errorCode 를 돌려줄 classId (errorDescr 에 쿠키를 되돌림)
	objects  map[string]string // classId → outConfigs 안의 XML

	srv *httptest.Server

	mu      sync.Mutex
	calls   []string // "aaaLogin", "configResolveClass <classId>", "aaaLogout"
	logouts int      // 자기 쿠키로 온 aaaLogout 수
}

func newUcsmMock(t *testing.T, cfg func(m *ucsmMock)) *ucsmMock {
	t.Helper()
	m := &ucsmMock{classErr: map[string]bool{}, objects: map[string]string{}}
	if cfg != nil {
		cfg(m)
	}
	m.srv = httptest.NewTLSServer(m)
	t.Cleanup(m.srv.Close)
	return m
}

func ucsmXMLEsc(s string) string {
	var b bytes.Buffer
	_ = xml.EscapeText(&b, []byte(s))
	return b.String()
}

func (m *ucsmMock) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	var req struct {
		XMLName    xml.Name
		InName     string `xml:"inName,attr"`
		InPassword string `xml:"inPassword,attr"`
		Cookie     string `xml:"cookie,attr"`
		ClassID    string `xml:"classId,attr"`
		InCookie   string `xml:"inCookie,attr"`
	}
	_ = xml.Unmarshal(body, &req)
	call := req.XMLName.Local
	if req.ClassID != "" {
		call += " " + req.ClassID
	}
	m.mu.Lock()
	m.calls = append(m.calls, call)
	m.mu.Unlock()
	if r.Method != http.MethodPost || r.URL.Path != "/nuova" {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	switch req.XMLName.Local {
	case "aaaLogin":
		if m.authFail || req.InName != testUser || req.InPassword != testPass {
			fmt.Fprintf(w, `<aaaLogin cookie="" response="yes" errorCode="551" invocationResult="unidentified-fail" errorDescr="Authentication failed (%s)"> </aaaLogin>`,
				ucsmXMLEsc(req.InPassword))
			return
		}
		fmt.Fprintf(w, `<aaaLogin cookie="" response="yes" outCookie="%s" outRefreshPeriod="600" outPriv="read-only" outVersion="4.2(3b)"> </aaaLogin>`, ucsmTestCookie)
	case "configResolveClass":
		if req.Cookie != ucsmTestCookie {
			fmt.Fprintf(w, `<configResolveClass cookie="" response="yes" classId="%s" errorCode="552" errorDescr="Authorization required"/>`, req.ClassID)
			return
		}
		if m.classErr[req.ClassID] {
			fmt.Fprintf(w, `<configResolveClass cookie="%s" response="yes" classId="%s" errorCode="103" invocationResult="unidentified-fail" errorDescr="no class %s (cookie %s)"/>`,
				ucsmTestCookie, req.ClassID, req.ClassID, ucsmTestCookie)
			return
		}
		fmt.Fprintf(w, `<configResolveClass cookie="%s" response="yes" classId="%s"><outConfigs>%s</outConfigs></configResolveClass>`,
			ucsmTestCookie, req.ClassID, m.objects[req.ClassID])
	case "aaaLogout":
		if req.InCookie == ucsmTestCookie {
			m.mu.Lock()
			m.logouts++
			m.mu.Unlock()
		}
		io.WriteString(w, `<aaaLogout cookie="" response="yes" outStatus="success"> </aaaLogout>`)
	default:
		w.WriteHeader(http.StatusBadRequest)
	}
}

func (m *ucsmMock) snapshot() ([]string, int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]string(nil), m.calls...), m.logouts
}

// ucsmTestMap 은 시험용 매핑입니다 (@ 는 tabs 가 탭으로 바꾼다).
const ucsmTestMap = `# 시험용 UCSM 매핑
vendor@model@std_name@xml_class@xml_attribute@value@protocol@xml_example@verified
CISCO@UCS B200 M5@system_profile@biosVfCPUPerformance@vpCPUPerformance@hpc@xml@<biosVfCPUPerformance vpCPUPerformance="hpc"/>@N
CISCO@UCS B200 M5@hyper_threading@biosVfIntelHyperThreadingTech@vpIntelHyperThreadingTech@enabled@xml@-@N
CISCO@UCS B200 M5@mem_ras@biosVfSelectMemoryRASConfiguration@vpSelectMemoryRASConfiguration@maximum-performance@xml@-@N
CISCO@UCS X210c M7@system_profile@biosVfCPUPerformance@vpCPUPerformance@hpc|HPC@xml@-@Y
CISCO@UCS X210c M7@hyper_threading@biosVfIntelHyperThreadingTech@vpIntelHyperThreadingTech@enabled@xml@-@N
# UNMAPPED CISCO UCS B200 M5 llc_prefetch: none_of_candidates
`

// ucsmFixture: blade-1(B200 M5), blade-2(X210c M7), chassis-2/blade-1(B200 M6, 매핑 없음).
// 정책(org-root/bios-prof-*)과 모르는 서버의 인스턴스는 무시되어야 한다.
func ucsmFixture(m *ucsmMock) {
	m.objects["computeBlade"] = `<computeBlade dn="sys/chassis-1/blade-1" model="UCSB-B200-M5" serial="FCH0000SER1" vendor="Cisco Systems Inc"/>` +
		`<computeBlade dn="sys/chassis-1/blade-2" model="UCSX-210C-M7" serial="FCH0000SER2"/>` +
		`<computeBlade dn="sys/chassis-2/blade-1" model="UCSB-B200-M6" serial="FCH0000SER3"/>`
	m.objects["biosVfCPUPerformance"] = `<biosVfCPUPerformance dn="org-root/bios-prof-HPC/CPU-Performance" vpCPUPerformance="enterprise"/>` +
		`<biosVfCPUPerformance dn="sys/chassis-1/blade-1/bios/bios-settings/CPU-Performance" vpCPUPerformance="hpc"/>` +
		`<biosVfCPUPerformance dn="sys/chassis-1/blade-2/bios/bios-settings/CPU-Performance" vpCPUPerformance="platform-default"/>` +
		`<biosVfCPUPerformance dn="sys/chassis-9/blade-9/bios/bios-settings/CPU-Performance" vpCPUPerformance="enterprise"/>`
	m.objects["biosVfIntelHyperThreadingTech"] = `<biosVfIntelHyperThreadingTech dn="sys/chassis-1/blade-1/bios/bios-settings/Intel-HyperThreading-Tech" vpIntelHyperThreadingTech="disabled"/>` +
		`<biosVfIntelHyperThreadingTech dn="sys/chassis-1/blade-2/bios/bios-settings/Intel-HyperThreading-Tech"/>`
	m.classErr["biosVfSelectMemoryRASConfiguration"] = true
}

func ucsmTestMapT(t *testing.T) *ucsmMap {
	t.Helper()
	mp, err := parseUcsmMap(tabs(ucsmTestMap), "map.tsv")
	must(t, err)
	return mp
}

func ucsmRunT(t *testing.T, rc *runContext) *ucsmRun {
	t.Helper()
	run, err := doUcsm(rc, ucsmOpts{Map: ucsmTestMapT(t), ResultDir: t.TempDir(), Now: checkNow})
	if run == nil {
		t.Fatalf("doUcsm: %v", err)
	}
	must(t, err)
	return run
}

func ucsmItemOf(t *testing.T, h *ucsmHost, dn, std string) ucsmItem {
	t.Helper()
	for _, s := range h.Servers {
		if s.DN != dn {
			continue
		}
		for _, it := range s.Items {
			if it.Std == std {
				return it
			}
		}
	}
	t.Fatalf("%s %s 항목 없음 (Status=%s Detail=%s)", dn, std, h.Status, h.Detail)
	return ucsmItem{}
}

// OK·UNVERIFIED·FAIL(platform-default)·MAPPING_MISSING·UNSUPPORTED 판정, 호출 순서, 로그아웃, 결과 파일.
func TestUcsmJudgeFlow(t *testing.T) {
	m := newUcsmMock(t, ucsmFixture)
	run := ucsmRunT(t, checkRCFor(t, 1, "", m.srv.URL))
	h := run.Hosts[0]
	if h.Status != "" || len(h.Servers) != 3 {
		t.Fatalf("호스트 상태 %q (%s), 서버 %d대", h.Status, h.Detail, len(h.Servers))
	}
	for _, w := range []struct{ dn, std, result, cur string }{
		{"sys/chassis-1/blade-1", "system_profile", StatusOK, "hpc"},
		{"sys/chassis-1/blade-1", "hyper_threading", StatusUnverified, "disabled"},
		{"sys/chassis-1/blade-1", "llc_prefetch", StatusMappingMissing, "-"},
		{"sys/chassis-1/blade-1", "sub_numa_cluster", StatusMappingMissing, "-"},
		{"sys/chassis-1/blade-1", "mem_ras", StatusUnsupported, "-"},
		{"sys/chassis-1/blade-2", "system_profile", StatusFail, "platform-default"},
		{"sys/chassis-1/blade-2", "hyper_threading", StatusUnsupported, "-"},
		{"sys/chassis-1/blade-2", "mem_ras", StatusMappingMissing, "-"},
		{"sys/chassis-2/blade-1", "system_profile", StatusMappingMissing, "-"},
		{"sys/chassis-2/blade-1", "hyper_threading", StatusMappingMissing, "-"},
	} {
		it := ucsmItemOf(t, h, w.dn, w.std)
		if it.Result != w.result || it.Current != w.cur {
			t.Errorf("%s %s = %s 현재=%q (%s), 기대 %s 현재=%q", w.dn, w.std, it.Result, it.Current, it.Reason, w.result, w.cur)
		}
	}
	if it := ucsmItemOf(t, h, "sys/chassis-1/blade-1", "llc_prefetch"); !strings.Contains(it.Reason, "none_of_candidates") {
		t.Errorf("UNMAPPED 사유가 없음: %q", it.Reason)
	}
	if it := ucsmItemOf(t, h, "sys/chassis-1/blade-2", "system_profile"); !strings.Contains(it.Reason, "biosPlatformDefaults") {
		t.Errorf("platform-default 안내가 없음: %q", it.Reason)
	}

	calls, logouts := m.snapshot()
	wantCalls := "aaaLogin|configResolveClass computeBlade|configResolveClass biosVfCPUPerformance|" +
		"configResolveClass biosVfIntelHyperThreadingTech|configResolveClass biosVfSelectMemoryRASConfiguration|aaaLogout"
	if got := strings.Join(calls, "|"); got != wantCalls {
		t.Errorf("호출 순서\n got  %s\n want %s", got, wantCalls)
	}
	if logouts != 1 {
		t.Errorf("자기 쿠키 aaaLogout %d회, 기대 1", logouts)
	}

	lines := readLines(t, filepath.Join(run.Dir, ucsmResultFile))
	if len(lines) != 1+3*5 || lines[0] != strings.Join(ucsmResultHeader, "\t") {
		t.Fatalf("ucsm_result.tsv %d줄 / 머리글 %q", len(lines), lines[0])
	}
	host := strings.TrimPrefix(m.srv.URL, "https://")
	wantRow := strings.Join([]string{host, "127.0.0.1", "xml", "UCSM", "sys/chassis-1/blade-1", "Cisco", "UCSB-B200-M5",
		"system_profile", "biosVfCPUPerformance", "vpCPUPerformance", "hpc", "hpc", "OK"}, "\t")
	if lines[1] != wantRow {
		t.Errorf("첫 행\n got  %q\n want %q", lines[1], wantRow)
	}
	all := strings.Join(lines, "\n")
	if strings.Contains(all, "FCH0000SER") || strings.Contains(all, "enterprise") {
		t.Errorf("serial 이나 정책/모르는 서버 값이 결과에 들어감:\n%s", all)
	}
	if _, err := os.Stat(filepath.Join(run.Dir, "result.tsv")); err == nil {
		t.Error("ucsm 은 Redfish 의 result.tsv 를 만들면 안 됨")
	}

	var out bytes.Buffer
	writeUcsmReport(&out, run, ucsmReportOpts{ListMax: 20})
	for _, want := range []string{"서버(블레이드) 3대", "[FAIL] 서버 1대 (항목 1건)", "[UNVERIFIED] 서버 1대 (항목 1건)",
		"biosPlatformDefaults", "[MAPPING_MISSING] 항목 10건", "[UNSUPPORTED] 항목 2건", ucsmResultFile} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("요약에 %q 없음:\n%s", want, out.String())
		}
	}
	out.Reset()
	writeUcsmReport(&out, run, ucsmReportOpts{ListMax: 0})
	if !strings.Contains(out.String(), "... 외 1대") {
		t.Errorf("-list-max 0 이면 서버 목록 대신 개수만:\n%s", out.String())
	}
}

// AUTH_FAIL: aaaLogin 1회만, 이후 호출·로그아웃 없음, 차단기가 다음 UCSM 을 건너뜀.
func TestUcsmAuthFailOnce(t *testing.T) {
	m1 := newUcsmMock(t, func(m *ucsmMock) { ucsmFixture(m); m.authFail = true })
	m2 := newUcsmMock(t, func(m *ucsmMock) { ucsmFixture(m); m.authFail = true })
	rc := checkRCFor(t, 1, "", m1.srv.URL, m2.srv.URL)
	rc.Conf.AuthFailStop = 1
	run := ucsmRunT(t, rc)
	if run.Hosts[0].Status != StatusAuthFail || run.Hosts[1].Status != StatusSkippedAuthStop {
		t.Fatalf("상태 %s / %s, 기대 AUTH_FAIL / SKIPPED_AUTH_STOP", run.Hosts[0].Status, run.Hosts[1].Status)
	}
	if calls, logouts := m1.snapshot(); strings.Join(calls, "|") != "aaaLogin" || logouts != 0 {
		t.Errorf("AUTH_FAIL UCSM 호출 %v (로그아웃 %d), 기대 aaaLogin 1회뿐", calls, logouts)
	}
	if calls, _ := m2.snapshot(); len(calls) != 0 {
		t.Errorf("차단기가 건너뛴 UCSM 이 요청 %v 를 받음", calls)
	}
	lines := readLines(t, filepath.Join(run.Dir, ucsmResultFile))
	if len(lines) != 3 || !strings.HasSuffix(lines[1], "\t-\t-\t-\t-\t-\t-\t-\t-\tAUTH_FAIL") {
		t.Errorf("호스트 단위 오류 행:\n%s", strings.Join(lines, "\n"))
	}
}

// 허용목록: 쓰기·임의 조회 classId 는 로그인 전후 모두 보내지 않는다 (서버가 받는 요청 없음).
func TestUcsmAllowlistRejectsWrites(t *testing.T) {
	m := newUcsmMock(t, ucsmFixture)
	c := ucsmxml.New(ucsmxml.Options{
		BaseURL: m.srv.URL, User: testUser, Pass: testPass, Insecure: true, Timeout: 5 * time.Second,
		Classes: []string{"biosVfCPUPerformance", "configConfMo"}, // configConfMo 는 bios* 가 아니라 버려진다
	})
	bad := []string{"configConfMo", "configConfMos", "configResolveDn", "configResolveChildren", "lsPower", "aaaUser", "biosVfOther", ""}
	for _, cls := range bad {
		if _, err := c.ResolveClass(cls); !errors.Is(err, ucsmxml.ErrNotAllowed) {
			t.Errorf("로그인 전 ResolveClass(%q) 는 ErrNotAllowed 여야 함: %v", cls, err)
		}
	}
	if calls, _ := m.snapshot(); len(calls) != 0 {
		t.Fatalf("거부된 호출인데 서버가 %v 를 받음", calls)
	}
	must(t, c.Login())
	for _, cls := range bad {
		if _, err := c.ResolveClass(cls); !errors.Is(err, ucsmxml.ErrNotAllowed) {
			t.Errorf("로그인 후 ResolveClass(%q) 는 ErrNotAllowed 여야 함: %v", cls, err)
		}
	}
	if calls, _ := m.snapshot(); strings.Join(calls, "|") != "aaaLogin" {
		t.Fatalf("거부된 호출이 전송됨: %v", calls)
	}
	if objs, err := c.ResolveClass("biosVfCPUPerformance"); err != nil || len(objs) != 4 {
		t.Errorf("허용 클래스 조회: %d개 %v", len(objs), err)
	}
	must(t, c.Logout())
	if calls, logouts := m.snapshot(); len(calls) != 3 || logouts != 1 {
		t.Errorf("호출 %v, 로그아웃 %d", calls, logouts)
	}
}

// 비밀번호·쿠키는 결과 파일·요약·오류 상세 어디에도 없다 (장비가 오류 설명에 되돌려도).
func TestUcsmNoSecretLeak(t *testing.T) {
	ok := newUcsmMock(t, ucsmFixture) // biosVfSelectMemoryRASConfiguration 오류 설명에 쿠키를 되돌림
	bad := newUcsmMock(t, func(m *ucsmMock) { m.authFail = true })
	run := ucsmRunT(t, checkRCFor(t, 1, "", ok.srv.URL, bad.srv.URL))
	if run.Hosts[1].Status != StatusAuthFail {
		t.Fatalf("두 번째 UCSM 상태 %s", run.Hosts[1].Status)
	}
	var out bytes.Buffer
	writeUcsmReport(&out, run, ucsmReportOpts{ListMax: 20})
	tsv, err := os.ReadFile(filepath.Join(run.Dir, ucsmResultFile))
	must(t, err)
	var dump strings.Builder
	for _, h := range run.Hosts {
		fmt.Fprintf(&dump, "%+v\n", *h)
		for _, s := range h.Servers {
			fmt.Fprintf(&dump, "%+v\n", *s)
		}
	}
	for label, s := range map[string]string{"요약": out.String(), "TSV": string(tsv), "결과 구조체": dump.String()} {
		for _, sec := range []string{testPass, ucsmXMLEsc(testPass), ucsmTestCookie} {
			if strings.Contains(s, sec) {
				t.Errorf("%s 에 비밀값 %q 노출:\n%s", label, sec, s)
			}
		}
	}
	if !strings.Contains(out.String(), "AUTH_FAIL") || !strings.Contains(out.String(), "***") {
		t.Errorf("AUTH_FAIL 상세(마스킹된 사유)가 요약에 없음:\n%s", out.String())
	}
}

func TestUcsmModelKey(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{"UCS B200 M5", "b200m5"},
		{"UCSB-B200-M5", "b200m5"},
		{"UCS B200 M4", "b200m4"},
		{"UCSB-B200-M4", "b200m4"},
		{"UCS B480 M5", "b480m5"},
		{"UCSB-B480-M5", "b480m5"},
		{"UCS X210c M7", "x210cm7"},
		{"UCSX-210C-M7", "x210cm7"},
		{"ucsx-210c-m7", "x210cm7"},
		{"UCSB-B200-M6", "b200m6"},     // 매핑 행이 없으면 MAPPING_MISSING (M5 로 추측하지 않음)
		{"UCSC-C220-M5SX", "cc220m5sx"}, // 랙 서버 PID 는 규칙 밖 → 어떤 행과도 맞지 않음
		{"UCS", ""},
		{"", ""},
	} {
		if got := ucsmModelKey(c.in); got != c.want {
			t.Errorf("ucsmModelKey(%q) = %q, 기대 %q", c.in, got, c.want)
		}
	}
}

func TestUcsmMapParse(t *testing.T) {
	// 저장소의 실제 매핑 파일
	m, err := loadUcsmMap("../../" + ucsmDefaultMap)
	must(t, err)
	if len(m.Rows) != 8 || strings.Join(m.Classes, ",") != "biosVfCPUPerformance,biosVfIntelHyperThreadingTech" || len(m.Unmapped) != 8 {
		t.Errorf("실제 매핑: 행 %d, 클래스 %v, UNMAPPED %d", len(m.Rows), m.Classes, len(m.Unmapped))
	}
	if m.rowFor(ucsmModelKey("UCSB-B200-M5"), "system_profile") == nil || m.rowFor(ucsmModelKey("UCSX-210C-M7"), "hyper_threading") == nil {
		t.Error("UCSM PID 로 매핑 행을 찾지 못함")
	}
	if m.Unmapped["b480m5|llc_prefetch"] != "none_of_candidates" {
		t.Errorf("UNMAPPED 해석: %v", m.Unmapped)
	}

	// 주석·빈 줄·CRLF·BOM·UNMAPPED
	hdr := "vendor@model@std_name@xml_class@xml_attribute@value@protocol@xml_example@verified\n"
	row := "CISCO@UCS B200 M5@system_profile@biosVfCPUPerformance@vpCPUPerformance@hpc@xml@-@N\n"
	text := "\ufeff# 주석\r\n\r\n" + hdr + row + "# UNMAPPED CISCO UCS B200 M5 llc_prefetch: none_of_candidates\r\n# 그냥 주석: UNMAPPED 아님\n"
	p, err := parseUcsmMap(tabs(text), "t.tsv")
	must(t, err)
	if len(p.Rows) != 1 || p.Unmapped["b200m5|llc_prefetch"] != "none_of_candidates" || len(p.Unmapped) != 1 || len(p.Items) != len(stdKeywords) {
		t.Errorf("주석/UNMAPPED 해석: rows=%d unmapped=%v items=%v", len(p.Rows), p.Unmapped, p.Items)
	}

	for _, c := range []struct{ name, text, want string }{
		{"머리글 없음", row, "머리글"},
		{"주석만", "# 주석\n", "머리글"},
		{"행 없음", hdr, "항목 행이"},
		{"열 수", hdr + "CISCO@UCS B200 M5@system_profile\n", "9개 열"},
		{"protocol", hdr + strings.Replace(row, "@xml@", "@json-redfish@", 1), "protocol"},
		{"verified", hdr + strings.Replace(row, "@N\n", "@X\n", 1), "verified"},
		{"쓰기 클래스", hdr + strings.Replace(row, "biosVfCPUPerformance", "configConfMo", 1), "xml_class"},
		{"벤더", hdr + strings.Replace(row, "CISCO", "DELL", 1), "vendor"},
		{"중복", hdr + row + row, "중복"},
		{"빈 허용값", hdr + strings.Replace(row, "@hpc@", "@hpc|@", 1), "빈 허용값"},
		{"UNMAPPED 형식", hdr + row + "# UNMAPPED CISCO llc_prefetch none\n", "UNMAPPED"},
	} {
		if _, err := parseUcsmMap(tabs(c.text), "t.tsv"); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: 오류에 %q 기대, 실제 %v", c.name, c.want, err)
		}
	}
}

// cmdUcsm 전체 경로: conf·암호화 비밀번호·user 파일로 실행 → results/<일시>/ucsm_result.tsv, 지원하지 않는 옵션은 거부.
func TestCmdUcsmEndToEnd(t *testing.T) {
	m := newUcsmMock(t, ucsmFixture)
	env := newRTEnv(t, 3, "")
	user := env.writeFile("ucsm.txt", strings.TrimPrefix(m.srv.URL, "https://"))
	mapPath := env.writeFile("map.tsv", tabs(ucsmTestMap))
	before := env.dirs()
	out, err := rtCapture(t, func() error {
		return cmdUcsm([]string{"-conf", env.conf, "-hosts", env.hosts, "-user", user, "-xml-map", mapPath})
	})
	if err != nil {
		t.Fatalf("cmdUcsm: %v\n%s", err, out)
	}
	dir := env.newDir(before)
	if lines := readLines(t, filepath.Join(dir, ucsmResultFile)); len(lines) != 16 {
		t.Errorf("ucsm_result.tsv %d줄, 기대 16", len(lines))
	}
	for _, want := range []string{"UCSM BIOS 점검 결과", "[FAIL] 서버 1대", "configResolveClass"} {
		if !strings.Contains(out, want) {
			t.Errorf("출력에 %q 없음:\n%s", want, out)
		}
	}
	if strings.Contains(out, testPass) || strings.Contains(out, ucsmTestCookie) {
		t.Errorf("출력에 비밀값 노출:\n%s", out)
	}

	before = env.dirs()
	for _, extra := range [][]string{{"-profile", "VM"}, {"-dry-run"}, {"-from-dump", env.dir}, {"-list-max", "-1"}} {
		args := append([]string{"-conf", env.conf, "-hosts", env.hosts, "-user", user, "-xml-map", mapPath}, extra...)
		if _, err := rtCapture(t, func() error { return cmdUcsm(args) }); err == nil {
			t.Errorf("%v 는 거부되어야 함", extra)
		}
	}
	env.noNewDir(before)
}
