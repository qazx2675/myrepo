package main

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// xmlhost.go / 프로파일 구역 / classifyTargets 시험. UCSM 은 ucsm_test.go 의 httptest 가짜(ucsmMock),
// Redfish 는 internal/mockbmc 만 쓴다 (실제 장비 접속 금지).

// xmlProfSection 은 시험 프로파일에 붙이는 # [xml] 구역입니다 (@ 는 tabs 가 탭으로 바꾼다).
const xmlProfSection = `
#   [ XML ]
CISCO@UCS B200 M5@system_profile@biosVfCPUPerformance.vpCPUPerformance@hpc@N
CISCO@UCS X210c M7@system_profile@biosVfCPUPerformance.vpCPUPerformance@hpc|HPC@Y
CISCO@UCSC-C220-M5SX@system_profile@biosVfCPUPerformance.vpCPUPerformance@hpc@Y
CISCO@UCSC-C220-M5SX@hyper_threading@biosVfIntelHyperThreadingTech.vpIntelHyperThreadingTech@enabled@N
`

// xmlProfText 는 시험 픽스처(Redfish 행) + # [xml] 구역입니다.
func xmlProfText(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(profFixture)
	must(t, err)
	return string(b) + tabs(xmlProfSection)
}

func xmlTestProf(t *testing.T) *Profile {
	t.Helper()
	p, err := parseProfile(xmlProfText(t), "VM.tsv")
	must(t, err)
	p.Name = "VM"
	return p
}

// rackFixture: standalone CIMC 처럼 computeBlade 는 errorCode, 랙 서버 1대 (sys/rack-unit-1).
func rackFixture(m *ucsmMock) {
	m.classErr["computeBlade"] = true
	m.classErr["lsServer"] = true
	m.objects["computeRackUnit"] = `<computeRackUnit dn="sys/rack-unit-1" model="UCSC-C220-M5SX" serial="WZP0000SER9"/>`
	m.objects["biosVfCPUPerformance"] = `<biosVfCPUPerformance dn="sys/rack-unit-1/bios/bios-settings/CPU-Performance" vpCPUPerformance="hpc"/>`
	m.objects["biosVfIntelHyperThreadingTech"] = `<biosVfIntelHyperThreadingTech dn="sys/rack-unit-1/bios/bios-settings/Intel-HyperThreading-Tech" vpIntelHyperThreadingTech="enabled"/>`
}

// spFixture: ucsmFixture(블레이드 3대) + 서비스 프로파일 c201 → blade-2, other → blade-1.
func spFixture(m *ucsmMock) {
	ucsmFixture(m)
	m.objects["lsServer"] = `<lsServer dn="org-root/ls-c201" name="c201" pnDn="sys/chassis-1/blade-2"/>` +
		`<lsServer dn="org-root/ls-other" name="other" pnDn="sys/chassis-1/blade-1"/>` +
		`<lsServer dn="org-root/ls-free" name="free" pnDn=""/>`
}

func xmlRunT(t *testing.T, rc *runContext) *unifiedRun {
	t.Helper()
	fastCheck(t)
	u, err := runUnified(rc, unifiedOpts{Profile: xmlTestProf(t), ResultDir: t.TempDir(), HostsPath: writeHosts(t, ""), Now: checkNow})
	if err != nil || u == nil {
		t.Fatalf("runUnified: %v", err)
	}
	return u
}

func TestProfileSections(t *testing.T) {
	p, err := parseProfile(xmlProfText(t), "VM.tsv")
	must(t, err)
	fix := testProf(t)
	if len(p.Rows) != len(fix.Rows) || len(p.XML.Rows) != 4 || !reflect.DeepEqual(p.Items, fix.Items) {
		t.Fatalf("redfish %d (기대 %d), xml %d, Items %v", len(p.Rows), len(fix.Rows), len(p.XML.Rows), p.Items)
	}
	r := p.XML.Rows[1]
	if r.Class != "biosVfCPUPerformance" || r.Attr != "vpCPUPerformance" || !r.Verified || !reflect.DeepEqual(r.Allowed, []string{"hpc", "HPC"}) {
		t.Errorf("xml 행 해석: %+v", r)
	}
	if got := strings.Join(p.XML.Classes, ","); got != "biosVfCPUPerformance,biosVfIntelHyperThreadingTech" {
		t.Errorf("Classes = %s", got)
	}
	if fix.XML == nil || len(fix.XML.Rows) != 0 || len(fix.XML.Items) != len(stdKeywords) {
		t.Errorf("구역 없는 프로파일의 XML = %+v", fix.XML)
	}

	// 구역 표시 전 행은 redfish, # [redfish] 로 돌아올 수 있음, xml 만 있는 프로파일도 됨
	q := mustProfile(t, `vendor@model@std_name@attribute@value@verified
DELL@R660@hyper_threading@LogicalProc@Enabled@N
#[xml]
CISCO@UCS B200 M5@hyper_threading@biosVfIntelHyperThreadingTech.vpIntelHyperThreadingTech@enabled@N
# UNMAPPED CISCO UCS B200 M5 llc_prefetch: none_of_candidates
# [ReDfish ]
HPE@*@llc_prefetch@Foo@Enabled@N
# UNMAPPED 형식 아님 — redfish 구역에서는 그냥 주석
`)
	if len(q.Rows) != 2 || len(q.XML.Rows) != 1 || q.XML.Unmapped["b200m5|llc_prefetch"] != "none_of_candidates" {
		t.Errorf("구역 전환: redfish %d xml %d unmapped %v", len(q.Rows), len(q.XML.Rows), q.XML.Unmapped)
	}
	x := mustProfile(t, `vendor@model@std_name@attribute@value@verified
# [xml]
CISCO@UCS B200 M5@hyper_threading@biosVfIntelHyperThreadingTech.vpIntelHyperThreadingTech@enabled@N
`)
	if len(x.Rows) != 0 || len(x.XML.Rows) != 1 {
		t.Errorf("xml 만: redfish %d xml %d", len(x.Rows), len(x.XML.Rows))
	}

	hdr := `vendor@model@std_name@attribute@value@verified
# [xml]
`
	row := "CISCO@UCS B200 M5@system_profile@biosVfCPUPerformance.vpCPUPerformance@hpc@N"
	for _, c := range []struct{ name, text, want string }{
		{"점 없음", hdr + "CISCO@UCS B200 M5@system_profile@vpCPUPerformance@hpc@N", "<xml_class>.<xml_attribute>"},
		{"bios 아닌 클래스", hdr + "CISCO@UCS B200 M5@system_profile@aaaUser.pwd@x@N", "읽기 허용목록"},
		{"쓰기 메서드 클래스", hdr + "CISCO@UCS B200 M5@system_profile@configConfMo.dn@x@N", "읽기 허용목록"},
		{"속성 문자", hdr + "CISCO@UCS B200 M5@system_profile@biosVfCPUPerformance.vp-CPU@hpc@N", "xml_attribute"},
		{"빈 속성", hdr + "CISCO@UCS B200 M5@system_profile@biosVfCPUPerformance.@hpc@N", "xml_attribute"},
		{"벤더", hdr + strings.Replace(row, "CISCO", "DELL", 1), "vendor 는 CISCO"},
		{"모델 *", hdr + strings.Replace(row, "UCS B200 M5", "*", 1), "* 를 쓸 수 없음"},
		{"중복", hdr + row + `
` + row, "중복"},
		{"verified", hdr + strings.Replace(row, "@N", "@X", 1), "verified"},
		{"UNMAPPED 형식", hdr + row + `
# UNMAPPED CISCO llc_prefetch none`, "UNMAPPED"},
	} {
		_, err := parseProfile(tabs(c.text), "VM.tsv")
		if err == nil || !strings.Contains(err.Error(), c.want) || !strings.Contains(err.Error(), "VM.tsv:") {
			t.Errorf("%s: 오류 %v, 기대 %q 포함", c.name, err, c.want)
		}
	}
}

func TestConfXMLHostPrefix(t *testing.T) {
	for _, c := range []struct {
		text string
		want []string
	}{
		{"user=a", []string{"c"}},
		{"user=a" + nl + "xml_host_prefix= C1 , Ucs ,,", []string{"c1", "ucs"}},
		{"user=a" + nl + "xml_host_prefix=", nil},
	} {
		conf, err := parseConf(c.text, "bios.conf")
		must(t, err)
		if !reflect.DeepEqual(conf.XMLHostPrefix, c.want) {
			t.Errorf("%q → %v, 기대 %v", c.text, conf.XMLHostPrefix, c.want)
		}
	}
}

// nl 은 줄바꿈 1글자입니다.
const nl = `
`

func TestClassifyTargets(t *testing.T) {
	hosts := writeHosts(t, `192.0.2.1 c201-m
192.0.2.2 node01-m
192.0.2.3 ucs9-m
192.0.2.4 C202-m
`)
	kinds := func(conf *Config, rf, xml []string) string {
		ts, err := classifyTargets(conf, rf, xml, hosts)
		must(t, err)
		var out []string
		for _, tg := range ts {
			k := "rf"
			if tg.XML {
				k = "xml"
			}
			out = append(out, tg.Hostname+"="+k)
		}
		return strings.Join(out, " ")
	}
	rf := []string{"c201", "node01-m", "C202-m:8443", "192.0.2.9", "10.0.0.1:443", "ucs9"}
	def := &Config{XMLHostPrefix: []string{"c"}}
	if got, want := kinds(def, rf, nil), "c201=xml node01=rf C202:8443=xml 192.0.2.9=rf 10.0.0.1:443=rf ucs9=rf"; got != want {
		t.Errorf("기본 접두어 c\n got  %s\n want %s", got, want)
	}
	custom := &Config{XMLHostPrefix: []string{"ucs", "node"}}
	if got, want := kinds(custom, rf, nil), "c201=rf node01=xml C202:8443=rf 192.0.2.9=rf 10.0.0.1:443=rf ucs9=xml"; got != want {
		t.Errorf("접두어 ucs,node\n got  %s\n want %s", got, want)
	}
	none := &Config{}
	if got, want := kinds(none, rf, nil), "c201=rf node01=rf C202:8443=rf 192.0.2.9=rf 10.0.0.1:443=rf ucs9=rf"; got != want {
		t.Errorf("빈 접두어\n got  %s\n want %s", got, want)
	}
	// -user-xml: IP·이름 모두 XML, 두 목록에 다 있으면 XML 쪽 하나, 순서는 -user 다음 -user-xml
	if got, want := kinds(none, []string{"node01", "192.0.2.9"}, []string{"192.0.2.9", "node01-m:9"}), "node01=rf 192.0.2.9=xml node01:9=xml"; got != want {
		t.Errorf("-user-xml\n got  %s\n want %s", got, want)
	}
}

// standalone CIMC: 서버 1대(computeRackUnit) → 그 서버가 호스트, 점검만 (전체비교 XML_READONLY, 설정 제외).
func TestXMLSingleRackServer(t *testing.T) {
	m := newUcsmMock(t, rackFixture)
	rc := checkRCFor(t, 1, "", m.srv.URL)
	rc.Targets[0].XML = true
	u := xmlRunT(t, rc)
	h := u.Check.Hosts[0]
	if h.Status != "" || !h.XML || h.Info == nil || h.Info.Vendor != ucsmVendor || h.Info.Model != "UCSC-C220-M5SX" {
		t.Fatalf("호스트 = %q (%s) xml=%v info=%+v", h.Status, h.Detail, h.XML, h.Info)
	}
	if it := itemOf(t, h, "system_profile"); it.Result != StatusOK || it.Attr != "biosVfCPUPerformance.vpCPUPerformance" || it.Server != "" {
		t.Errorf("system_profile = %+v", it)
	}
	if it := itemOf(t, h, "llc_prefetch"); it.Result != StatusMappingMissing {
		t.Errorf("llc_prefetch = %+v", it)
	}
	host := u.HostOrder()[0]
	pl := linesOf(u.ProfileLines(), host)
	if l := pl["biosVfCPUPerformance.vpCPUPerformance"]; l.Tag != TagOK || l.Vendor != ucsmVendor || l.Note != "OK [XML]" || l.MgmtModel != xmlMgmtModel {
		t.Errorf("프로파일 줄 = %+v", l)
	}
	if u.Snaps[0].Status != StatusXMLReadOnly {
		t.Errorf("스냅샷 상태 %s, 기대 %s", u.Snaps[0].Status, StatusXMLReadOnly)
	}
	res := compareT(t, u, compareRef{Hosts: []string{host}})
	if h := res.Hosts[0]; h.Status != StatusXMLReadOnly || len(res.Lines) != 1 || !res.Lines[0].HostError {
		t.Errorf("전체비교 = %s, 줄 %d", h.Status, len(res.Lines))
	}
	if l := res.Lines[0]; l.Vendor != ucsmVendor || l.Model != u.Snaps[0].Model || l.Model == "-" { // 벤더 탭·CSV 이름이 "-" 가 되지 않게
		t.Errorf("전체비교 XML 줄 벤더·모델 = %q %q", l.Vendor, l.Model)
	}
	if calls, logouts := m.snapshot(); strings.Contains(strings.Join(calls, "|"), "configConf") || logouts != 1 || calls[0] != "aaaLogin" {
		t.Errorf("호출 %v 로그아웃 %d", calls, logouts)
	}
	rows := readLines(t, filepath.Join(u.Check.Dir, "result.tsv"))
	if len(rows) < 2 || !strings.Contains(rows[1], "	xml	") {
		t.Errorf("result.tsv protocol 이 xml 이 아님: %v", rows)
	}
}

// 블레이드 여러 대: 서비스 프로파일 이름 = 호스트 이름인 서버만 판정, verified=Y FAIL 이어도 설정 계획에서 빠짐.
func TestXMLMultiBladeServiceProfileMatch(t *testing.T) {
	m := newUcsmMock(t, spFixture)
	port := m.srv.URL[strings.LastIndex(m.srv.URL, ":")+1:]
	rc := checkRCFor(t, 1, "127.0.0.1 c201-m", "c201-m:"+port)
	rc.Targets[0].XML = true
	u := xmlRunT(t, rc)
	h := u.Check.Hosts[0]
	if h.Status != "" || !h.XML || h.Info.Model != "UCSX-210C-M7" {
		t.Fatalf("호스트 = %q (%s) info=%+v", h.Status, h.Detail, h.Info)
	}
	it := itemOf(t, h, "system_profile")
	if it.Result != StatusFail || it.Current != "platform-default" || it.Server != "" || len(h.Items) != len(xmlTestProf(t).XML.Items) {
		t.Errorf("system_profile = %+v, 항목 %d", it, len(h.Items))
	}
	if got := u.HostOrder(); len(got) != 1 || got[0] != "c201:"+port {
		t.Errorf("HostOrder = %v", got)
	}
	if h.settable(it) {
		t.Error("XML 항목이 settable")
	}
	plan := buildSetPlan(rc.Conf, u, nil)
	if !plan.Empty() || !strings.Contains(strings.Join(plan.Skipped, nl), "XML API 대상") {
		t.Errorf("설정 계획에 XML 호스트: items %d skipped %v", len(plan.Items), plan.Skipped)
	}
}

// 예전 방식 check(bios_check.sh 옛 옵션)도 XML 대상은 점검만: 리포트 [설정 불가] 에 FAIL 이 사유와 함께 나오고
// [FAIL 상세]·dry-run 대상에는 없다. allcheck 는 XML 대상에 로그인만 해 보고 XML_READONLY (비교 제외).
func TestXMLLegacyCheckAndAllcheck(t *testing.T) {
	m := newUcsmMock(t, spFixture)
	port := m.srv.URL[strings.LastIndex(m.srv.URL, ":")+1:]
	rc := checkRCFor(t, 1, "127.0.0.1 c201-m", "c201-m:"+port)
	rc.Targets[0].XML = true
	fastCheck(t)
	run, err := doCheck(rc, checkOpts{Profile: xmlTestProf(t), ResultDir: t.TempDir(), Now: checkNow})
	must(t, err)
	var b strings.Builder
	writeReport(&b, run, reportOpts{ListMax: 100, FailMax: 100})
	out := b.String()
	if strings.Contains(out, "[FAIL 상세]") || !strings.Contains(out, "[설정 불가]") ||
		!strings.Contains(out, "biosVfCPUPerformance.vpCPUPerformance  FAIL") || !strings.Contains(out, "XML API 대상 — 점검만") {
		t.Errorf("check 리포트:%s%s", nl, out)
	}
	if hs := dryRunHosts(run); len(hs) != 0 {
		t.Errorf("dry-run 대상에 XML 호스트 %d대", len(hs))
	}
	before, _ := m.snapshot()
	n := len(before)
	s := (&allEnv{conf: rc.Conf, password: rc.Password}).fetch(rc.Targets[0])
	if s.Status != StatusXMLReadOnly {
		t.Errorf("allcheck XML 스냅샷 = %s", s.Status)
	}
	if calls, _ := m.snapshot(); strings.Join(calls[n:], ",") != "aaaLogin,aaaLogout" { // XML 장비인지 로그인만 확인 (조회 없음)
		t.Errorf("allcheck 의 XML 호출: %v", calls[n:])
	}
}

// 블레이드 여러 대인데 서비스 프로파일 이름이 맞지 않음 → 서버마다 <호스트>/<dn> 으로 모두 보고, 비고에 SP 이름.
func TestXMLMultiBladeNoMatch(t *testing.T) {
	m := newUcsmMock(t, spFixture)
	port := m.srv.URL[strings.LastIndex(m.srv.URL, ":")+1:]
	rc := checkRCFor(t, 1, "127.0.0.1 c999-m", "c999-m:"+port)
	rc.Targets[0].XML = true
	u := xmlRunT(t, rc)
	name := "c999:" + port
	want := []string{name + "/sys/chassis-1/blade-1", name + "/sys/chassis-1/blade-2", name + "/sys/chassis-2/blade-1"}
	if got := u.HostOrder(); !reflect.DeepEqual(got, want) {
		t.Fatalf("HostOrder = %v", got)
	}
	pl := u.ProfileLines()
	if l := linesOf(pl, want[0])["biosVfCPUPerformance.vpCPUPerformance"]; l.Note != "OK [XML SP=other]" || l.Model != "UCSB-B200-M5" {
		t.Errorf("blade-1 줄 = %+v", l)
	}
	if l := linesOf(pl, want[2])["system_profile"]; l.Note != "MAPPING_MISSING [XML SP=-]" || l.Model != "UCSB-B200-M6" {
		t.Errorf("chassis-2 줄 = %+v", l)
	}
	res := filepath.Join(t.TempDir(), "check.res_x")
	must(t, writeCheckRes(res, pl, u.HostOrder()))
	lines := readLines(t, res)
	if len(lines) != 3 || !strings.HasPrefix(lines[0], want[0]+": OK") || !strings.HasPrefix(lines[1], want[1]+": FAIL difference 1건") {
		t.Errorf("check.res =%s%s", nl, strings.Join(lines, nl))
	}
	tsv := strings.Join(readLines(t, filepath.Join(u.Check.Dir, "result.tsv")), nl)
	if !strings.Contains(tsv, want[2]+"	127.0.0.1") || strings.Contains(tsv, "FCH0000SER") {
		t.Errorf("result.tsv 서버별 이름이 없거나 serial 노출:%s%s", nl, tsv)
	}
}

// XML 로그인 전 실패(Redfish 장비라 /nuova 가 XML 이 아님) → Redfish 로 1번 다시, 비고 "XML 실패 → Redfish".
func TestXMLFallbackToRedfish(t *testing.T) {
	s, us := startAllSrv(t, "hpe-dl360gen11", nil)
	rc := checkRCFor(t, 1, "", us)
	rc.Targets[0].XML = true
	u := xmlRunT(t, rc)
	h := u.Check.Hosts[0]
	if h.Status != "" || h.XML || h.ProtoNote != xmlFallbackNote || h.Info == nil || h.Info.Vendor != "HPE" {
		t.Fatalf("호스트 = %q (%s) xml=%v note=%q", h.Status, h.Detail, h.XML, h.ProtoNote)
	}
	if !strings.Contains(strings.Join(h.Notes, nl), xmlFallbackNote) {
		t.Errorf("Notes = %v", h.Notes)
	}
	if l := linesOf(u.ProfileLines(), us)["ProcHyperthreading"]; l.Note != "OK [XML 실패 → Redfish]" {
		t.Errorf("프로파일 줄 = %+v", l)
	}
	if !u.Snaps[0].ok() {
		t.Errorf("Redfish 로 읽은 스냅샷은 전체비교 가능해야 함: %s", u.Snaps[0].Status)
	}
	if ss := s.Sessions(); ss.Created != 1 || ss.Deleted != 1 {
		t.Errorf("Redfish 세션 %+v, 기대 1회", ss)
	}
}

// AUTH_FAIL 은 Redfish 로 다시 시도하지 않고, 차단기는 XML·Redfish 대상을 함께 센다.
func TestXMLAuthFailNoFallback(t *testing.T) {
	m := newUcsmMock(t, func(m *ucsmMock) { spFixture(m); m.authFail = true })
	r, ur := startAllSrv(t, "hpe-dl360gen11", nil)
	rc := checkRCFor(t, 1, "", m.srv.URL, ur)
	rc.Targets[0].XML = true
	rc.Conf.AuthFailStop = 1
	u := xmlRunT(t, rc)
	h := u.Check.Hosts[0]
	if h.Status != StatusAuthFail || !h.XML || h.ProtoNote != xmlNote {
		t.Fatalf("XML 호스트 = %q xml=%v note=%q", h.Status, h.XML, h.ProtoNote)
	}
	if calls, _ := m.snapshot(); strings.Join(calls, "|") != "aaaLogin" {
		t.Errorf("AUTH_FAIL 뒤 호출 %v, 기대 aaaLogin 1회", calls)
	}
	if u.Check.Hosts[1].Status != StatusSkippedAuthStop || r.CallCount() != 0 {
		t.Errorf("차단기: 두 번째 %s, Redfish 요청 %d", u.Check.Hosts[1].Status, r.CallCount())
	}
	if l := u.ProfileLines()[0]; !l.HostError || l.Note != "AUTH_FAIL [XML]" {
		t.Errorf("오류 줄 = %+v", l)
	}
}

// bios -auto: Redfish IP + 접두어 c 이름(UCSM 블레이드) + -user-xml IP(CIMC) 를 한 번에, check.res 는 입력 순서.
func TestBiosAutoMixedRedfishXML(t *testing.T) {
	ucsm := newUcsmMock(t, spFixture)
	cimc := newUcsmMock(t, rackFixture)
	_, ua := startAllSrv(t, "hpe-dl360gen11", nil)
	port := ucsm.srv.URL[strings.LastIndex(ucsm.srv.URL, ":")+1:]
	e := newRTEnv(t, 3, "127.0.0.1 c201-m")
	must(t, os.WriteFile(filepath.Join(e.dir, "profiles", "VM.tsv"), []byte(xmlProfText(t)), 0o600))
	targets := e.writeFile("targets.txt", "c201-m:"+port, ua)
	cimcIP := strings.TrimPrefix(cimc.srv.URL, "https://")
	xmlList := e.writeFile("xml.txt", cimcIP)
	out, err := rtCapture(t, func() error {
		return cmdBios([]string{"-conf", e.conf, "-hosts", e.hosts, "-res-dir", e.dir, "-auto-check", "mix", targets, "-profile", "VM", "-user-xml", xmlList})
	})
	if err != nil {
		t.Fatalf("bios -auto-check: %v%s%s", err, nl, out)
	}
	lines := readLines(t, filepath.Join(e.dir, "check.res_mix"))
	want := []string{
		"c201:" + port + ": FAIL difference 1건 (biosVfCPUPerformance.vpCPUPerformance platform-default→hpc|HPC)",
		ua + ": OK",
		cimcIP + ": OK (N/A 2건)",
	}
	if !reflect.DeepEqual(lines, want) {
		t.Errorf("check.res_mix%s got  %v%s want %v", nl, lines, nl, want)
	}
	for _, m := range []*ucsmMock{ucsm, cimc} {
		calls, logouts := m.snapshot()
		for _, c := range calls {
			if c != "aaaLogin" && c != "aaaLogout" && !strings.HasPrefix(c, "configResolveClass ") {
				t.Errorf("읽기 전용이 아닌 호출 %q", c)
			}
		}
		if logouts != 1 {
			t.Errorf("로그아웃 %d", logouts)
		}
	}
}

// 전체비교의 XML 대상 줄은 N/A 회색(오류 빨강 아님)이고 비고는 "XML 대상 — 비교 제외(읽기 전용)", 다른 호스트 오류는 그대로 빨강.
// 설정 확인 화면에는 XML 대상 호스트가 "제외:" 아래에 사유와 함께 나온다.
func TestXMLCompareLineAndSetConfirmExcluded(t *testing.T) {
	ls := compareLines(&allHost{Target: Target{Hostname: "c201", IP: "10.0.0.2"}, Status: StatusXMLReadOnly}, nil)
	if len(ls) != 1 {
		t.Fatalf("줄 %d", len(ls))
	}
	l := ls[0]
	if l.Tag != TagNA || l.Note != xmlCompareNote || l.Attr != "-" || lineIsError(l) {
		t.Errorf("XML 줄 = %+v", l)
	}
	row := tuiLineRow(l)
	if row[0].t != "[N/A]" || row[0].st != tuiStGray || row[len(row)-1].t != xmlCompareNote || row[len(row)-1].st == tuiStRed {
		t.Errorf("TUI 칸 = %+v", row)
	}
	if lineColor(l) != lnGray || strings.Contains(formatLine(l, true), lnRed) {
		t.Errorf("평문 색 = %q", formatLine(l, true))
	}
	if sum := tuiSummaryRow(tuiSummarize(LineGroup{Hosts: []string{"c201"}, Lines: ls})[0]); sum[len(sum)-1].st != tuiStGray || sum[0].st != "" {
		t.Errorf("요약 칸 = %+v", sum)
	}
	e := compareLines(&allHost{Target: Target{Hostname: "h1"}, Status: StatusUnreachable}, nil)[0]
	if !lineIsError(e) || tuiTagStyle(e) != tuiStRed || lineColor(e) != lnRed {
		t.Errorf("오류 줄은 빨강이어야 함: %+v", e)
	}

	p := &setPlan{}
	p.addHost(nil, 0, &hostCheck{Target: Target{Hostname: "c201"}, XML: true}, nil, false)
	cl := strings.Join(p.ConfirmLines(), "\n")
	if !p.Empty() || !strings.Contains(cl, "\n제외:\n  c201  XML API 대상 — 점검만 (읽기 전용, 설정 제외)") || p.ExcludedLines() != nil {
		t.Errorf("확인 화면:\n%s", cl)
	}
}
