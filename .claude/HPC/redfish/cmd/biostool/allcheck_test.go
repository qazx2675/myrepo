package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"biostool/internal/mockbmc"
)

// allcheck.go / allreport.go 시험 (계획서 성공기준 7). 실제 BMC 는 쓰지 않고 internal/mockbmc(testdata 합성 트리)만 쓴다.
// 호스트마다 mock 서버를 따로 띄우고(포트가 다름 → 서로 다른 대상), 속성·모델·BIOS 버전은 Server.SetBiosAttr/Modify 로 바꾼다.

var testIgnore = newIgnoreSet(defaultIgnore, "내장 기본 목록")

// startAllSrv 는 트리 이름의 mock BMC 를 띄우고 mod 로 시나리오를 만든 뒤 "127.0.0.1:포트" 를 돌려줍니다.
func startAllSrv(t *testing.T, tree string, mod func(s *mockbmc.Server)) (*mockbmc.Server, string) {
	t.Helper()
	s := startMockTree(t, tree, nil)
	if mod != nil {
		mod(s)
	}
	return s, strings.TrimPrefix(s.URL(), "https://")
}

func setSys(t *testing.T, s *mockbmc.Server, kv map[string]interface{}) {
	t.Helper()
	must(t, s.Modify("/redfish/v1/systems/1", func(m map[string]interface{}) {
		for k, v := range kv {
			m[k] = v
		}
	}))
}

func setAttrs(t *testing.T, s *mockbmc.Server, kv map[string]interface{}) {
	t.Helper()
	for k, v := range kv {
		must(t, s.SetBiosAttr(k, v))
	}
}

// runAllT 는 diff.txt 텍스트와 대상 목록으로 doAllCheck 를 돌립니다.
func runAllT(t *testing.T, conc int, diffText string, targets []string, fromDump string) *allRun {
	t.Helper()
	fastCheck(t)
	rc := checkRCFor(t, conc, "", targets...)
	if fromDump != "" {
		rc.Password = ""
		rc.Conf.User = ""
	}
	refs, err := newAllRefs(parseRefList(diffText), writeHosts(t, ""))
	must(t, err)
	run, err := doAllCheck(rc, allOpts{Refs: refs, Ignore: testIgnore, FromDump: fromDump, ResultDir: t.TempDir(), Now: checkNow})
	if err != nil {
		t.Fatalf("doAllCheck: %v", err)
	}
	return run
}

func hostOf(t *testing.T, run *allRun, name string) *allHost {
	t.Helper()
	for _, h := range run.Hosts {
		if h.Target.Hostname == name {
			return h
		}
	}
	t.Fatalf("대상 %s 없음", name)
	return nil
}

func readTSV(t *testing.T, p string) [][]string {
	t.Helper()
	var rows [][]string
	for _, l := range readLines(t, p) {
		rows = append(rows, strings.Split(l, "\t"))
	}
	return rows
}

func countIgnored(attrs map[string]string, ign *ignoreSet) int {
	n := 0
	for k := range attrs {
		if ign.match(k) {
			n++
		}
	}
	return n
}

// diffKey 는 차이 1건을 비교하기 쉬운 문자열로 만듭니다.
func diffKey(d attrDiffRow) string {
	return fmt.Sprintf("%s|%s|%s|%s", d.Status, d.Attr, d.refCell(), d.hostCell())
}

func diffKeys(h *allHost) []string {
	var out []string
	for _, d := range h.Diffs {
		out = append(out, diffKey(d))
	}
	return out
}

// ---- 단위 시험 ----

func TestParseRefList(t *testing.T) {
	text := "\ufeff# 기준 목록\n\ncheckhostname1\ncheckhostname2   DL360Gen11  \t# 주석\n  host3-m\tProLiant DL360 Gen10\r\ncheckhostname1\n127.0.0.1:8443\n"
	got := parseRefList(text)
	want := []refEntry{
		{"checkhostname1", ""}, {"checkhostname2", "DL360Gen11"}, {"host3-m", "ProLiant DL360 Gen10"}, {"127.0.0.1:8443", ""},
	}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("parseRefList =\n%v\n기대\n%v", got, want)
	}
	if _, err := loadRefList(filepath.Join(t.TempDir(), "없음.txt")); err == nil || !strings.Contains(err.Error(), "diff.txt.example") {
		t.Errorf("없는 diff.txt 오류 = %v", err)
	}
	p := filepath.Join(t.TempDir(), "diff.txt")
	must(t, os.WriteFile(p, []byte("# 비어 있음\n\n"), 0o600))
	if _, err := loadRefList(p); err == nil || !strings.Contains(err.Error(), "비어") {
		t.Errorf("빈 diff.txt 오류 = %v", err)
	}
}

func TestIgnoreAndGlob(t *testing.T) {
	for _, c := range []struct {
		pat, s string
		want   bool
	}{
		{"servicetag", "servicetag", true}, {"bootseq*", "bootseqretry", true}, {"bootseq*", "uefibootseq", false},
		{"*uuid*", "systemuuid", true}, {"*macaddr*", "nicmacaddress", true}, {"*macaddr*", "machinecheckrecovery", false},
		{"hyper-threading[all]", "hyper-threading[all]", true}, {"a*b*c", "axxbyyc", true}, {"a*b*c", "axxbyy", false},
		{"*", "아무거나", true}, {"abc", "abcd", false}, {"", "", true}, {"x*", "", false},
	} {
		if got := globMatch(c.pat, c.s); got != c.want {
			t.Errorf("globMatch(%q, %q) = %v, 기대 %v", c.pat, c.s, got, c.want)
		}
	}
	ign := parseIgnore("\ufeff# 주석\n  ServiceTag \nBootSeq*   # 뒤 주석\n\n*UUID*\nservicetag\n", "t")
	if fmt.Sprint(ign.Pats) != "[servicetag bootseq* *uuid*]" {
		t.Errorf("Pats = %v", ign.Pats)
	}
	for _, n := range []string{"ServiceTag", "SERVICETAG", "BootSeqRetry", "SystemUuid", "SysUUID"} {
		if !ign.match(n) {
			t.Errorf("%s 는 제외 대상이어야 함", n)
		}
	}
	if ign.match("ProcTurbo") {
		t.Error("ProcTurbo 가 제외됨")
	}
	// 기본 목록: 호스트마다 다른 값은 빠지고, 설정값(MachineCheck 등)은 빠지지 않는다.
	def := newIgnoreSet(defaultIgnore, "")
	for _, n := range []string{"ServiceTag", "AssetTag", "SerialNumber", "SystemServiceTag", "SysMfrContactInfo", "UefiBootSeq", "BootSeq1", "SystemUuid", "NicMacAddress1"} {
		if !def.match(n) {
			t.Errorf("기본 목록이 %s 를 제외하지 않음", n)
		}
	}
	for _, n := range []string{"MachineCheckRecovery", "ProcTurbo", "BootMode", "SubNumaClustering"} {
		if def.match(n) {
			t.Errorf("기본 목록이 %s 를 제외함 (진짜 설정이 가려짐)", n)
		}
	}

	// 파일: 없으면 내장 기본 목록, -ignore 로 직접 줬는데 없으면 오류, 있으면 그 파일만.
	dir := t.TempDir()
	s, err := loadIgnore(filepath.Join(dir, "ignore_attrs.txt"), false)
	if err != nil || len(s.Pats) != len(def.Pats) || !strings.Contains(s.Source, "내장") {
		t.Errorf("파일 없음 → %+v, %v", s, err)
	}
	if _, err := loadIgnore(filepath.Join(dir, "ignore_attrs.txt"), true); err == nil {
		t.Error("명시한 파일이 없는데 오류가 아님")
	}
	p := filepath.Join(dir, "my.txt")
	must(t, os.WriteFile(p, []byte("OnlyThis\n"), 0o600))
	if s, err := loadIgnore(p, true); err != nil || fmt.Sprint(s.Pats) != "[onlythis]" {
		t.Errorf("파일 있음 → %+v, %v", s, err)
	}
}

func TestCompareAttrs(t *testing.T) {
	ref := map[string]string{"A": "1", "B": "x", "OnlyR": "r", "ServiceTag": "S1", "Empty": "", "N": "null"}
	host := map[string]string{"A": "1", "B": "y", "OnlyH": "h", "ServiceTag": "S2", "Empty": "z", "N": "null"}
	h := &allHost{}
	compareAttrs(h, ref, host, newIgnoreSet([]string{"servicetag"}, ""))
	want := []string{`DIFF|B|x|y`, `DIFF|Empty|""|z`, `ONLY_REF|OnlyR|r|<없음>`, `ONLY_HOST|OnlyH|<없음>|h`}
	if fmt.Sprint(diffKeys(h)) != fmt.Sprint(want) {
		t.Errorf("Diffs = %v\n기대   %v", diffKeys(h), want)
	}
	// 합집합 A,B,Empty,N,OnlyH,OnlyR + 제외 ServiceTag
	if h.Compared != 6 || h.Ignored != 1 || h.NDiff != 2 || h.NOnlyRef != 1 || h.NOnlyHost != 1 {
		t.Errorf("개수 = %+v", *h)
	}
	same := &allHost{}
	compareAttrs(same, ref, ref, newIgnoreSet(nil, ""))
	if len(same.Diffs) != 0 || same.Compared != 6 {
		t.Errorf("같은 속성끼리: %+v", *same)
	}
}

// ---- 시나리오 ----

type allScenario struct {
	ref11, a11, b11, v11, m11, ref10, a10, b10, dell *mockbmc.Server
	url                                              map[string]string
	diff                                             string
	targets                                          []string
	all                                              []*mockbmc.Server
}

// newAllScenario 는 모델 2종(DL360 Gen11, Gen10) × 기준 1 + 대상 여럿 + 기준 없는 모델(Dell) 을 만듭니다.
func newAllScenario(t *testing.T) *allScenario {
	t.Helper()
	sc := &allScenario{url: map[string]string{}}
	add := func(name, tree string, mod func(s *mockbmc.Server)) *mockbmc.Server {
		s, u := startAllSrv(t, tree, mod)
		sc.url[name] = u
		sc.all = append(sc.all, s)
		sc.targets = append(sc.targets, u)
		return s
	}
	sc.ref11 = add("ref11", "hpe-dl360gen11", nil)
	// a11: 값 1개 다름(DIFF) + 기준에만 있는 속성(ONLY_REF) + 대상에만 있는 속성(ONLY_HOST). 서비스태그류는 달라도 제외돼야 한다.
	sc.a11 = add("a11", "hpe-dl360gen11", func(s *mockbmc.Server) {
		setAttrs(t, s, map[string]interface{}{"ProcTurbo": "Disabled", "ThermalConfig": nil, "NewAttrX1": "v", "ServerName": "other-name", "SerialNumber": "SN-ZZZ"})
	})
	// b11: 제외 속성만 다름 → 완전 일치
	sc.b11 = add("b11", "hpe-dl360gen11", func(s *mockbmc.Server) {
		setAttrs(t, s, map[string]interface{}{"ServerName": "b11-name", "SerialNumber": "SN-B11"})
	})
	// v11: BIOS 버전이 다름 → 비교는 하고 BIOS_VER_DIFF. 속성 목록도 달라짐.
	sc.v11 = add("v11", "hpe-dl360gen11", func(s *mockbmc.Server) {
		setSys(t, s, map[string]interface{}{"BiosVersion": "U54 v1.60 (09/01/2025)"})
		setAttrs(t, s, map[string]interface{}{"Sriov": "Disabled", "NewInV160": "On", "ProcX2Apic": nil})
	})
	// m11: Model 표기만 다른 같은 기종 (DL360Gen11) → 같은 모델 키로 분류돼야 한다.
	sc.m11 = add("m11", "hpe-dl360gen11", func(s *mockbmc.Server) { setSys(t, s, map[string]interface{}{"Model": "DL360Gen11"}) })
	sc.ref10 = add("ref10", "hpe-dl360gen10", nil)
	sc.a10 = add("a10", "hpe-dl360gen10", func(s *mockbmc.Server) {
		setAttrs(t, s, map[string]interface{}{"WorkloadProfile": "Virtualization-MaxPerformance"})
	})
	sc.b10 = add("b10", "hpe-dl360gen10", nil)
	sc.dell = add("dell", "dell-r660", nil)
	// diff.txt 는 hostname 만 (모델은 BMC 에서 자동 조회)
	sc.diff = "# 기준\n" + sc.url["ref11"] + "\n" + sc.url["ref10"] + "\n"
	return sc
}

func TestAllCheckTwoModels(t *testing.T) {
	sc := newAllScenario(t)
	run := runAllT(t, 4, sc.diff, sc.targets, "")
	u := sc.url

	// 모델 자동 분류: 같은 모델의 기준과만 비교
	for _, n := range []string{"a11", "b11", "v11", "m11"} {
		if h := hostOf(t, run, u[n]); h.Ref == nil || h.Ref.Target.Hostname != u["ref11"] {
			t.Errorf("%s 의 기준 = %+v, 기대 ref11", n, h.Ref)
		}
	}
	for _, n := range []string{"a10", "b10"} {
		if h := hostOf(t, run, u[n]); h.Ref == nil || h.Ref.Target.Hostname != u["ref10"] {
			t.Errorf("%s 의 기준 = %+v, 기대 ref10", n, h.Ref)
		}
	}
	// 기준 자신 (같은 모델의 다른 기준이 없음) · 기준 없는 모델
	for _, n := range []string{"ref11", "ref10"} {
		if h := hostOf(t, run, u[n]); h.Status != StatusIsReference || len(h.Diffs) != 0 {
			t.Errorf("%s = %s, 기대 REFERENCE (자기 자신과 비교 없음)", n, h.Status)
		}
	}
	if h := hostOf(t, run, u["dell"]); h.Status != StatusNoReference || !strings.Contains(h.Detail, "dell|r660") {
		t.Errorf("dell = %s (%s), 기대 NO_REFERENCE", h.Status, h.Detail)
	}

	// (a)(b) DIFF / ONLY_REF / ONLY_HOST 정확, 제외 속성 개수
	ref11 := run.Refs[0].Snap
	a := hostOf(t, run, u["a11"])
	wantA := []string{
		"DIFF|ProcTurbo|" + ref11.Attrs["ProcTurbo"] + "|Disabled",
		"ONLY_REF|ThermalConfig|" + ref11.Attrs["ThermalConfig"] + "|<없음>",
		"ONLY_HOST|NewAttrX1|<없음>|v",
	}
	if a.Status != StatusDiff || fmt.Sprint(diffKeys(a)) != fmt.Sprint(wantA) {
		t.Errorf("a11 = %s %v\n기대 %v", a.Status, diffKeys(a), wantA)
	}
	ign11 := countIgnored(ref11.Attrs, testIgnore)
	if ign11 != 2 || a.Ignored != 2 {
		t.Errorf("제외 속성 수: 기준 %d, a11 %d, 기대 둘 다 2 (ServerName, SerialNumber)", ign11, a.Ignored)
	}
	// 합집합 = 기준 N개 + ONLY_HOST 1개, 제외 2개
	if a.Compared != len(ref11.Attrs)+1-2 || a.NDiff != 1 || a.NOnlyRef != 1 || a.NOnlyHost != 1 {
		t.Errorf("a11 개수 = compared %d diff %d only_ref %d only_host %d (기준 속성 %d개)", a.Compared, a.NDiff, a.NOnlyRef, a.NOnlyHost, len(ref11.Attrs))
	}
	b := hostOf(t, run, u["b11"])
	if b.Status != StatusSame || len(b.Diffs) != 0 || b.Ignored != 2 || b.Compared != len(ref11.Attrs)-2 {
		t.Errorf("b11 = %s diffs=%v ignored=%d compared=%d, 기대 SAME(제외 속성만 다름)", b.Status, diffKeys(b), b.Ignored, b.Compared)
	}
	a10 := hostOf(t, run, u["a10"])
	ref10 := run.Refs[1].Snap
	if a10.Status != StatusDiff || len(a10.Diffs) != 1 || diffKey(a10.Diffs[0]) != "DIFF|WorkloadProfile|"+ref10.Attrs["WorkloadProfile"]+"|Virtualization-MaxPerformance" {
		t.Errorf("a10 = %s %v", a10.Status, diffKeys(a10))
	}
	if h := hostOf(t, run, u["b10"]); h.Status != StatusSame {
		t.Errorf("b10 = %s %v", h.Status, diffKeys(h))
	}

	// (l) 정규화: Model "DL360Gen11" 도 "ProLiant DL360 Gen11" 과 같은 키 → 같은 기준과 비교돼 SAME
	if m := hostOf(t, run, u["m11"]); m.Status != StatusSame || m.Snap.Key != ref11.Key || m.Snap.Model != "DL360Gen11" {
		t.Errorf("m11 = %s key=%q model=%q (기준 키 %q)", m.Status, m.Snap.Key, m.Snap.Model, ref11.Key)
	}
	if ref11.Key != "hpe|dl360gen11" || run.Refs[1].Snap.Key != "hpe|dl360gen10" {
		t.Errorf("모델 키 = %q, %q", ref11.Key, run.Refs[1].Snap.Key)
	}

	// (c) BIOS 버전이 달라도 비교하고 BIOS_VER_DIFF 특이사항
	v := hostOf(t, run, u["v11"])
	wantV := []string{
		"DIFF|Sriov|" + ref11.Attrs["Sriov"] + "|Disabled",
		"ONLY_REF|ProcX2Apic|" + ref11.Attrs["ProcX2Apic"] + "|<없음>",
		"ONLY_HOST|NewInV160|<없음>|On",
	}
	if v.Status != StatusDiff || fmt.Sprint(diffKeys(v)) != fmt.Sprint(wantV) {
		t.Errorf("v11 = %s %v\n기대 %v", v.Status, diffKeys(v), wantV)
	}
	if !v.hasNote(noteBiosVerDiff) || !strings.Contains(v.notesText(), "대상=U54 v1.60 (09/01/2025) 기준="+ref11.BiosVersion) {
		t.Errorf("v11 notes = %q", v.notesText())
	}
	for _, n := range []string{"a11", "b11", "m11", "a10", "b10"} {
		if hostOf(t, run, u[n]).hasNote(noteBiosVerDiff) {
			t.Errorf("%s 에 BIOS_VER_DIFF 가 붙음 (버전이 같음)", n)
		}
	}

	// (g) 기준 호스트가 user.txt 에도 있어도 세션은 1회뿐, (h) 쓰기·로그 0건 / GET 4건 / 레지스트리·Settings 없음
	for i, s := range sc.all {
		name := sc.targets[i]
		if s.Writes() != 0 || s.LogHits() != 0 {
			t.Errorf("%s: Writes=%d LogHits=%d, 기대 0", name, s.Writes(), s.LogHits())
		}
		assertOneSession(t, s)
		if got := s.Count("GET", "/"); got != 4 {
			t.Errorf("%s: GET %d회, 기대 4 (ServiceRoot, Systems, System, Bios)", name, got)
		}
		for _, pre := range []string{"/redfish/v1/registr", "/redfish/v1/managers", "/redfish/v1/systems/1/bios/settings", "/redfish/v1/systems/system.embedded.1/bios/settings"} {
			if n := s.Count("", pre); n != 0 {
				t.Errorf("%s: %s 에 요청 %d회 (필요 없는 GET)", name, pre, n)
			}
		}
	}

	// 합계: 세션 = GET 4 × 호스트 수 (기준과 같은 대상은 한 번만 읽음)
	if sess, gets := run.callTotals(); sess != len(sc.all) || gets != 4*len(sc.all) {
		t.Errorf("callTotals = 세션 %d, GET %d, 기대 %d, %d", sess, gets, len(sc.all), 4*len(sc.all))
	}

	// 대상의 속성 값은 비교 뒤 버려진다 (수천 대여도 메모리에 쌓지 않음). 기준의 것만 남는다.
	for _, h := range run.Hosts {
		_, isRef := map[string]bool{u["ref11"]: true, u["ref10"]: true}[h.Target.Hostname]
		if isRef == (h.Snap.Attrs == nil) {
			t.Errorf("%s: 기준 여부 %v 인데 Attrs 보관 = %v", h.Target.Hostname, isRef, h.Snap.Attrs != nil)
		}
	}

	// (j) 결과 파일
	checkAllFiles(t, run)

	// 리포트
	var rep bytes.Buffer
	writeAllReport(&rep, run, allReportOpts{ListMax: 20, DiffMax: 30})
	out := rep.String()
	for _, want := range []string{
		"== BIOS 전체 비교 (대상 9대, 기준 2대 / 2026-10-07 14:02) ==",
		"[HPE ProLiant DL360 Gen11] 기준: " + u["ref11"] + " (BIOS U54 v1.40 (01/15/2025)) / 대상 4대 / 완전 일치 2대 / 차이 있음 2대 (기준 자신 1대 제외)",
		"[HPE ProLiant DL360 Gen10] 기준: " + u["ref10"] + " (BIOS U32 v2.80 (07/15/2023)) / 대상 2대 / 완전 일치 1대 / 차이 있음 1대",
		u["a11"] + "  DIFF 1, ONLY_REF 1, ONLY_HOST 1",
		"기준=" + ref11.Attrs["ProcTurbo"] + " → 대상=Disabled",
		"기준=<없음> → 대상=v  (ONLY_HOST)",
		"[BIOS_VER_DIFF]",
		"[속성별 집계]",
		"[특이사항]",
		"BIOS_VER_DIFF  대상 BIOS 버전이 기준과 다름",
		"대상=U54 v1.60 (09/01/2025) 기준=U54 v1.40 (01/15/2025)",
		"ONLY_REF / ONLY_HOST 가 늘 수 있습니다",
		"[분류 불가·오류]",
		"NO_REFERENCE",
		"[Dell PowerEdge R660] 1대: " + u["dell"],
		"diff.txt 에 추가한 뒤 다시 실행",
		"결과 파일: ",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("리포트에 %q 없음\n%s", want, out)
		}
	}
	if strings.Contains(out, "\x1b[") {
		t.Error("색이 꺼져 있는데 ANSI 코드가 나옴")
	}
	if strings.Contains(out, testPass) {
		t.Error("리포트에 비밀번호가 나옴")
	}
}

// checkAllFiles 는 결과 파일의 열 수·권한·내용 형식과 비밀번호 미노출을 확인합니다 (j).
func checkAllFiles(t *testing.T, run *allRun) {
	t.Helper()
	if !strings.HasSuffix(run.Dir, "_all") {
		t.Errorf("결과 폴더 %q 는 _all 로 끝나야 함", run.Dir)
	}
	cols := map[string]int{"all_diff.tsv": len(allDiffHeader), "summary.tsv": len(summaryHeader)}
	for name, n := range cols {
		p := filepath.Join(run.Dir, name)
		for i, row := range readTSV(t, p) {
			if len(row) != n {
				t.Errorf("%s %d행: 열 %d개, 기대 %d: %v", name, i+1, len(row), n, row)
			}
		}
	}
	if got := readTSV(t, filepath.Join(run.Dir, "all_diff.tsv"))[0]; strings.Join(got, " ") != "model ref_host host bios_version ref_bios_version attribute ref_value host_value status" {
		t.Errorf("all_diff.tsv 머리글 = %v", got)
	}
	if got := readTSV(t, filepath.Join(run.Dir, "summary.tsv"))[0]; strings.Join(got, " ") != "host ip model ref_host bios_version ref_bios_version compared ignored diff only_ref only_host status notes" {
		t.Errorf("summary.tsv 머리글 = %v", got)
	}
	// summary.tsv: 대상마다 1행
	if rows := readTSV(t, filepath.Join(run.Dir, "summary.tsv")); len(rows) != 1+len(run.Hosts) {
		t.Errorf("summary.tsv 행 %d, 기대 %d", len(rows), 1+len(run.Hosts))
	}
	// all_diff.tsv: DIFF/ONLY_* 행 수 = 호스트별 Diffs 합
	want := 0
	for _, h := range run.Hosts {
		want += len(h.Diffs)
	}
	if rows := readTSV(t, filepath.Join(run.Dir, "all_diff.tsv")); len(rows) != 1+want {
		t.Errorf("all_diff.tsv 행 %d, 기대 %d", len(rows), 1+want)
	}
	ents, err := os.ReadDir(run.Dir)
	must(t, err)
	names := map[string]bool{}
	for _, e := range ents {
		names[e.Name()] = true
		b, err := os.ReadFile(filepath.Join(run.Dir, e.Name()))
		must(t, err)
		if bytes.Contains(b, []byte(testPass)) || bytes.Contains(b, []byte("S3cr3t")) {
			t.Errorf("%s 에 비밀번호가 있음", e.Name())
		}
		if runtime.GOOS != "windows" {
			fi, _ := e.Info()
			if fi.Mode().Perm() != 0o600 {
				t.Errorf("%s 권한 = %v, 기대 0600", e.Name(), fi.Mode().Perm())
			}
		}
	}
	for _, n := range []string{"all_diff.tsv", "summary.tsv", "retry.txt", "run_info.txt"} {
		if !names[n] {
			t.Errorf("결과 파일 %s 없음 (있는 것: %v)", n, names)
		}
	}
	if fi, err := os.Stat(run.Dir); err == nil && runtime.GOOS != "windows" && fi.Mode().Perm() != 0o700 {
		t.Errorf("결과 폴더 권한 = %v, 기대 0700", fi.Mode().Perm())
	}
}

// (d) 같은 모델 기준이 2개면 BIOS 버전이 같은 쪽을 쓰고, 없으면 diff.txt 의 첫 기준을 쓰며 특이사항에 남긴다.
func TestAllCheckMultipleRefsSameKey(t *testing.T) {
	_, rA := startAllSrv(t, "hpe-dl360gen11", func(s *mockbmc.Server) {
		setSys(t, s, map[string]interface{}{"BiosVersion": "U54 v1.50 (05/01/2025)"})
		setAttrs(t, s, map[string]interface{}{"Sriov": "Disabled"})
	})
	_, rB := startAllSrv(t, "hpe-dl360gen11", nil) // 원래 버전 U54 v1.40
	_, v1 := startAllSrv(t, "hpe-dl360gen11", nil)
	_, v3 := startAllSrv(t, "hpe-dl360gen11", func(s *mockbmc.Server) {
		setSys(t, s, map[string]interface{}{"BiosVersion": "U54 v9.99"})
	})
	_, v50 := startAllSrv(t, "hpe-dl360gen11", func(s *mockbmc.Server) {
		setSys(t, s, map[string]interface{}{"BiosVersion": "U54 v1.50 (05/01/2025)"})
		setAttrs(t, s, map[string]interface{}{"Sriov": "Disabled"})
	})
	// rA 가 diff.txt 의 첫 줄이다 (버전이 안 맞을 때 첫 기준이 되는지도 확인)
	run := runAllT(t, 3, rA+"\n"+rB+"\n", []string{v1, v3, v50, rB}, "")

	h1 := hostOf(t, run, v1)
	if h1.Ref.Target.Hostname != rB || h1.Status != StatusSame || h1.hasNote(noteBiosVerDiff) ||
		!strings.Contains(h1.notesText(), "기준 2개 중 "+rB+" 선택 (BIOS 버전 일치)") {
		t.Errorf("v1: 기준 %s 상태 %s notes %q, 기대 rB(버전 일치) SAME", h1.Ref.Target.Hostname, h1.Status, h1.notesText())
	}
	h50 := hostOf(t, run, v50)
	if h50.Ref.Target.Hostname != rA || h50.Status != StatusSame || !strings.Contains(h50.notesText(), "BIOS 버전 일치") {
		t.Errorf("v50: 기준 %s 상태 %s notes %q, 기대 rA(버전 일치) SAME", h50.Ref.Target.Hostname, h50.Status, h50.notesText())
	}
	h3 := hostOf(t, run, v3)
	if h3.Ref.Target.Hostname != rA || !h3.hasNote(noteBiosVerDiff) ||
		!strings.Contains(h3.notesText(), "BIOS 버전이 같은 기준이 없어 diff.txt 순서의 첫 기준") {
		t.Errorf("v3: 기준 %s notes %q, 기대 첫 기준 rA + BIOS_VER_DIFF", h3.Ref.Target.Hostname, h3.notesText())
	}
	// 기준 rB 가 대상에도 있으면 자신은 빼고 다른 기준(rA)과 비교한다 (기준은 1개뿐이라 MULTI_REF 는 없음)
	hb := hostOf(t, run, rB)
	if hb.Ref == nil || hb.Ref.Target.Hostname != rA || hb.Status != StatusDiff || hb.hasNote(noteMultiRef) || !hb.hasNote(noteBiosVerDiff) {
		t.Errorf("rB(대상): 기준 %v 상태 %s notes %q", hb.Ref, hb.Status, hb.notesText())
	}
	var rep bytes.Buffer
	writeAllReport(&rep, run, allReportOpts{ListMax: 20, DiffMax: 30})
	for _, want := range []string{"MULTI_REF  같은 모델의 기준이 여러 개", "기준 2개 중 " + rB + " 선택 (BIOS 버전 일치)"} {
		if !strings.Contains(rep.String(), want) {
			t.Errorf("리포트에 %q 없음\n%s", want, rep.String())
		}
	}
}

// (e) 기준 접속 실패 → REF_UNREACHABLE (모델 칸이 있으면 같은 모델만), 기준이 아예 없는 모델 → NO_REFERENCE.
func TestAllCheckRefUnreachable(t *testing.T) {
	_, t11 := startAllSrv(t, "hpe-dl360gen11", nil)
	_, t10 := startAllSrv(t, "hpe-dl360gen10", nil)
	_, tOK := startAllSrv(t, "hpe-dl360gen11", nil)
	_, r10 := startAllSrv(t, "hpe-dl360gen10", nil)
	dead := "127.0.0.1:1" // 연결 거부

	// 모델 칸이 없는 접속 불가 기준: 어떤 모델의 기준이었는지 모르므로 기준 없는 모델은 모두 REF_UNREACHABLE
	run := runAllT(t, 2, dead+"\n"+r10+"\n", []string{t11, t10, tOK, r10}, "")
	for _, n := range []string{t11, tOK} {
		h := hostOf(t, run, n)
		if h.Status != StatusRefUnreachable || !strings.Contains(h.Detail, dead+"(UNREACHABLE)") {
			t.Errorf("%s = %s (%s), 기대 REF_UNREACHABLE", n, h.Status, h.Detail)
		}
	}
	if h := hostOf(t, run, t10); h.Status != StatusSame || h.Ref.Target.Hostname != r10 {
		t.Errorf("t10 = %s, 기대 SAME (살아 있는 기준 r10 과 비교)", h.Status)
	}
	if run.Refs[0].Snap.Status != StatusUnreachable {
		t.Errorf("죽은 기준 상태 = %s", run.Refs[0].Snap.Status)
	}
	retry := readLines(t, filepath.Join(run.Dir, "retry.txt"))
	if fmt.Sprint(retry) != fmt.Sprint([]string{t11, tOK}) {
		t.Errorf("retry.txt = %v, 기대 REF_UNREACHABLE 대상 [%s %s]", retry, t11, tOK)
	}
	var rep bytes.Buffer
	writeAllReport(&rep, run, allReportOpts{ListMax: 20, DiffMax: 30})
	for _, want := range []string{"[기준 호스트 오류]", dead, "REF_UNREACHABLE", "분류 불가", "재시도 대상 2대"} {
		if !strings.Contains(rep.String(), want) {
			t.Errorf("리포트에 %q 없음\n%s", want, rep.String())
		}
	}

	// 모델 칸이 DL360Gen10 인 접속 불가 기준: Gen11 대상은 그 기준의 모델이 아니므로 NO_REFERENCE
	run = runAllT(t, 2, dead+"   DL360Gen10\n", []string{t11, t10}, "")
	if h := hostOf(t, run, t11); h.Status != StatusNoReference {
		t.Errorf("t11 = %s (%s), 기대 NO_REFERENCE (접속 불가 기준의 모델 칸이 다른 모델)", h.Status, h.Detail)
	}
	if h := hostOf(t, run, t10); h.Status != StatusRefUnreachable {
		t.Errorf("t10 = %s (%s), 기대 REF_UNREACHABLE (접속 불가 기준의 모델 칸과 같은 모델)", h.Status, h.Detail)
	}
}

// (f) diff.txt 의 모델 칸이 실제와 다르면 경고만 하고 실제 모델로 분류한다 (맞는 칸은 표기가 달라도 경고 없음).
func TestAllCheckDiffTxtModelMismatch(t *testing.T) {
	_, ref := startAllSrv(t, "hpe-dl360gen11", nil)
	_, ref2 := startAllSrv(t, "hpe-dl360gen11", func(s *mockbmc.Server) { setAttrs(t, s, map[string]interface{}{"Sriov": "Disabled"}) })
	_, tgt := startAllSrv(t, "hpe-dl360gen11", nil)
	run := runAllT(t, 2, ref+"  DL360Gen10\n"+ref2+"  ProLiant DL360 Gen11\n", []string{tgt}, "")
	h := hostOf(t, run, tgt)
	if h.Ref == nil || h.Ref.Target.Hostname != ref || !strings.Contains(h.notesText(), noteMultiRef) {
		t.Errorf("대상 = %s 기준 %v notes %q, 실제 모델(Gen11)로 두 기준이 후보가 돼야 함", h.Status, h.Ref, h.notesText())
	}
	if len(run.Refs[0].Notes) != 1 || run.Refs[0].Notes[0].Code != noteDiffTxtModelMismatch ||
		!strings.Contains(run.Refs[0].Notes[0].Detail, "DL360Gen10") || !strings.Contains(run.Refs[0].Notes[0].Detail, "ProLiant DL360 Gen11") {
		t.Errorf("ref 노트 = %+v", run.Refs[0].Notes)
	}
	if len(run.Refs[1].Notes) != 0 {
		t.Errorf("맞는 모델 칸(표기만 다름)에 경고가 붙음: %+v", run.Refs[1].Notes)
	}
	var rep bytes.Buffer
	writeAllReport(&rep, run, allReportOpts{ListMax: 20, DiffMax: 30})
	if !strings.Contains(rep.String(), "DIFF_TXT_MODEL_MISMATCH") || !strings.Contains(rep.String(), `diff.txt 의 모델 "DL360Gen10"`) {
		t.Errorf("리포트:\n%s", rep.String())
	}
	if ri, _ := os.ReadFile(filepath.Join(run.Dir, "run_info.txt")); !strings.Contains(string(ri), "DIFF_TXT_MODEL_MISMATCH") {
		t.Errorf("run_info.txt:\n%s", ri)
	}
}

// (i) 같은 서버를 dump 로 저장한 뒤 서버 없이 -from-dump 로 비교한 결과가 온라인과 같다.
func TestAllCheckFromDumpMatchesOnline(t *testing.T) {
	fastDump(t)
	sc := newAllScenario(t)
	online := runAllT(t, 4, sc.diff, sc.targets, "")

	out := t.TempDir()
	if _, err := runDump(dumpRC(t, 3, sc.targets...), dumpOpts{Out: out}); err != nil {
		t.Fatal(err)
	}
	for _, s := range sc.all {
		s.Close() // 이후 비교는 서버 없이 해야 한다
	}
	offline := runAllT(t, 4, sc.diff, sc.targets, out)

	for _, name := range []string{"all_diff.tsv", "summary.tsv", "retry.txt"} {
		a, _ := os.ReadFile(filepath.Join(online.Dir, name))
		b, _ := os.ReadFile(filepath.Join(offline.Dir, name))
		if string(a) != string(b) {
			t.Errorf("%s 가 온라인과 다름\n온라인:\n%s\n오프라인:\n%s", name, a, b)
		}
	}
	ri, _ := os.ReadFile(filepath.Join(offline.Dir, "run_info.txt"))
	if !strings.Contains(string(ri), "덤프 읽기") || !strings.Contains(string(ri), "세션 생성 0, GET 0") {
		t.Errorf("run_info.txt:\n%s", ri)
	}
	checkAllFiles(t, offline)

	// 덤프에 없는 대상·기준: NO_DUMP
	run := runAllT(t, 2, sc.url["ref11"]+"\n127.0.0.1:2\n", []string{sc.url["a11"], "127.0.0.1:3"}, out)
	if h := hostOf(t, run, "127.0.0.1:3"); h.Status != StatusNoDump {
		t.Errorf("덤프 없는 대상 = %s, 기대 NO_DUMP", h.Status)
	}
	if run.Refs[1].Snap.Status != StatusNoDump {
		t.Errorf("덤프 없는 기준 = %s", run.Refs[1].Snap.Status)
	}
	if h := hostOf(t, run, sc.url["a11"]); h.Status != StatusDiff {
		t.Errorf("a11 = %s", h.Status)
	}
}

// 이름 해석 실패(NO_HOSTS_ENTRY)는 기준·대상 모두 그 상태로 남고 접속하지 않는다.
func TestAllCheckNoHostsEntry(t *testing.T) {
	_, ref := startAllSrv(t, "hpe-dl360gen11", nil)
	fastCheck(t)
	rc := checkRCFor(t, 2, "127.0.0.1  있는호스트-m\n", ref, "없는호스트", "있는호스트")
	refs, err := newAllRefs(parseRefList(ref+"\n기준없는이름\n"), writeHosts(t, ""))
	must(t, err)
	run, err := doAllCheck(rc, allOpts{Refs: refs, Ignore: testIgnore, ResultDir: t.TempDir(), Now: checkNow})
	must(t, err)
	if run.Refs[1].Snap.Status != StatusNoHostsEntry {
		t.Errorf("해석 실패 기준 = %s", run.Refs[1].Snap.Status)
	}
	if h := hostOf(t, run, "없는호스트"); h.Status != StatusNoHostsEntry {
		t.Errorf("해석 실패 대상 = %s", h.Status)
	}
	if h := hostOf(t, run, ref); h.Status != StatusIsReference {
		t.Errorf("기준 자신 = %s", h.Status)
	}
	if h := hostOf(t, run, "있는호스트"); h.Status != StatusUnreachable {
		t.Errorf("해석된 대상(127.0.0.1:443 접속 거부) = %s, 기대 UNREACHABLE", h.Status)
	}
}

// (k) AUTH_FAIL 차단기: 전 호스트 401 이면 기준·대상 합쳐 auth_fail_stop 만큼만 로그인을 시도하고 나머지는 접속하지 않는다.
func TestAllCheckAuthStop(t *testing.T) {
	for _, nRefs := range []int{2, 3} {
		nRefs := nRefs
		t.Run(fmt.Sprintf("기준 %d개", nRefs), func(t *testing.T) {
			f, port := newFarmOrSkip(t, mockbmc.FarmOptions{})
			const total = 40
			ips := addFarmHosts(t, f, loadTreeT(t, "hpe-dl360gen11"), total, 0, total) // 전부 비밀번호 불일치 → 401
			rc := farmRC(t, ips[nRefs:], port, 8, 3)
			var diff strings.Builder
			for _, ip := range ips[:nRefs] {
				fmt.Fprintf(&diff, "%s:%d\n", ip, port)
			}
			fastCheck(t)
			refs, err := newAllRefs(parseRefList(diff.String()), writeHosts(t, ""))
			must(t, err)
			run, err := doAllCheck(rc, allOpts{Refs: refs, Ignore: testIgnore, ResultDir: t.TempDir(), Now: checkNow})
			must(t, err)

			tot := f.Totals()
			if tot.Sessions.LoginFailed != 3 || tot.Sessions.Created != 0 {
				t.Errorf("BMC 로그인 시도 = %+v, 기대 실패 3 (기준 %d + 대상 %d 합쳐 3)", tot.Sessions, nRefs, 3-nRefs)
			}
			ts := run.tally()
			if ts[StatusAuthFail] != 3-nRefs || ts[StatusSkippedAuthStop] != total-nRefs-(3-nRefs) {
				t.Errorf("대상 집계 = %v, 기대 AUTH_FAIL %d + SKIPPED_AUTH_STOP %d", ts, 3-nRefs, total-3)
			}
			retry := readLines(t, filepath.Join(run.Dir, "retry.txt"))
			if len(retry) != ts[StatusSkippedAuthStop] {
				t.Errorf("retry.txt %d줄, 기대 SKIPPED_AUTH_STOP %d (AUTH_FAIL 은 넣지 않음)", len(retry), ts[StatusSkippedAuthStop])
			}
			var rep bytes.Buffer
			writeAllReport(&rep, run, allReportOpts{ListMax: 5, DiffMax: 30})
			if !strings.Contains(rep.String(), "계정 잠금 방지") || !strings.Contains(rep.String(), "SKIPPED_AUTH_STOP") {
				t.Errorf("리포트에 차단기 경고 없음:\n%s", rep.String())
			}
			if tot.Writes != 0 || tot.LogHits != 0 {
				t.Errorf("Writes=%d LogHits=%d", tot.Writes, tot.LogHits)
			}
		})
	}
}

// 대상 100대 규모(Farm)에서 동시 실행해도 결과가 정확하다 (-race 용). 기준 1 + 대상 100, 일부는 값이 다르다.
func TestAllCheckConcurrentFarm(t *testing.T) {
	f, port := newFarmOrSkip(t, mockbmc.FarmOptions{})
	tree := loadTreeT(t, "hpe-dl360gen11")
	const n = 100
	ips := make([]string, n+1)
	for i := range ips {
		ips[i] = farmIP(i)
		var over map[string]interface{}
		if i >= 1 && i%10 == 0 { // 10% 는 ProcTurbo 가 다르다
			over = map[string]interface{}{"ProcTurbo": "Disabled"}
		}
		must(t, f.AddHost(ips[i], tree, mockbmc.HostOptions{User: testUser, Pass: testPass, AttrOverrides: over}))
	}
	rc := farmRC(t, ips[1:], port, 20, 3)
	fastCheck(t)
	refs, err := newAllRefs(parseRefList(fmt.Sprintf("%s:%d\n", ips[0], port)), writeHosts(t, ""))
	must(t, err)
	run, err := doAllCheck(rc, allOpts{Refs: refs, Ignore: testIgnore, ResultDir: t.TempDir(), Now: checkNow})
	must(t, err)
	ts := run.tally()
	if ts[StatusDiff] != n/10 || ts[StatusSame] != n-n/10 {
		t.Errorf("집계 = %v, 기대 DIFF %d SAME %d", ts, n/10, n-n/10)
	}
	for _, h := range run.Hosts {
		if h.Status == StatusDiff && (len(h.Diffs) != 1 || h.Diffs[0].Attr != "ProcTurbo") {
			t.Errorf("%s diffs = %v", h.Target.Hostname, diffKeys(h))
		}
	}
	tot := f.Totals()
	if tot.Writes != 0 || tot.LogHits != 0 || tot.Sessions.Created != n+1 || tot.Sessions.Deleted != n+1 {
		t.Errorf("Farm 합계 = %+v, 기대 쓰기 0 / 로그 0 / 세션 %d·%d", tot, n+1, n+1)
	}
	if tot.Calls != (n+1)*(4+2) { // GET 4 + 세션 POST/DELETE 2
		t.Errorf("호출 합계 %d, 기대 %d", tot.Calls, (n+1)*6)
	}
	checkAllFiles(t, run)
}

// 리포트 상한: 호스트당 차이 속성 수(-diff-max), 나열할 호스트 수(-list-max), 속성별 집계의 "기준이 예외일 수 있음" 안내.
func TestAllCheckReportLimits(t *testing.T) {
	_, ref := startAllSrv(t, "hpe-dl360gen11", nil)
	many := func(s *mockbmc.Server) {
		setAttrs(t, s, map[string]interface{}{"ProcTurbo": "Disabled", "Sriov": "Disabled", "BootMode": "Legacy", "ProcX2Apic": "Disabled", "ThermalConfig": "IncreasedCooling"})
	}
	_, d1 := startAllSrv(t, "hpe-dl360gen11", many)
	_, d2 := startAllSrv(t, "hpe-dl360gen11", many)
	_, d3 := startAllSrv(t, "hpe-dl360gen11", nil)
	run := runAllT(t, 3, ref+"\n", []string{d1, d2, d3}, "")

	var rep bytes.Buffer
	writeAllReport(&rep, run, allReportOpts{ListMax: 1, DiffMax: 2})
	out := rep.String()
	for _, want := range []string{
		"… 외 3건, all_diff.tsv 참고", // 호스트당 5건 중 2건만
		"… 외 1대의 차이는 ",            // -list-max 1
		"… 외 3개 속성, all_diff.tsv 참고",
		"대상 과반에서 같은 속성이 다릅니다", "기준 호스트(" + ref + ")의 값이 예외",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("리포트에 %q 없음\n%s", want, out)
		}
	}
	if strings.Count(out, "기준=") != 2 {
		t.Errorf("속성 줄 수 = %d, 기대 2 (-diff-max 2, 호스트 1대만 나열)\n%s", strings.Count(out, "기준="), out)
	}
	// 과반 표시는 2대 이상 + 절반 초과일 때만: 3대 중 2대
	if !strings.Contains(out, "2대 (66%)") {
		t.Errorf("집계에 '2대 (66%%)' 없음\n%s", out)
	}

	// 한 호스트만 다르면(1/3) 안내가 없다
	_, e1 := startAllSrv(t, "hpe-dl360gen11", many)
	_, e2 := startAllSrv(t, "hpe-dl360gen11", nil)
	_, e3 := startAllSrv(t, "hpe-dl360gen11", nil)
	run = runAllT(t, 3, ref+"\n", []string{e1, e2, e3}, "")
	rep.Reset()
	writeAllReport(&rep, run, allReportOpts{ListMax: 20, DiffMax: 30})
	if strings.Contains(rep.String(), "대상 과반에서") {
		t.Errorf("1/3 인데 과반 안내가 나옴\n%s", rep.String())
	}
	// 0 이면 상세는 건수·파일 안내만
	rep.Reset()
	writeAllReport(&rep, run, allReportOpts{ListMax: 0, DiffMax: 0})
	if !strings.Contains(rep.String(), "… 외 1대의 차이는 ") || strings.Contains(rep.String(), "기준=") {
		t.Errorf("상한 0:\n%s", rep.String())
	}
}

// 수많은 BIOS 버전 불일치 호스트도 그룹으로 묶어 상한 안에서 보여 준다.
func TestAllCheckSpecialListIsBounded(t *testing.T) {
	run := &allRun{Now: checkNow, Dir: "results/x_all", Ignore: testIgnore}
	snap := &biosSnap{Vendor: "HPE", Model: "ProLiant DL360 Gen11", BiosVersion: "A", Key: "hpe|dl360gen11"}
	ref := &allRef{Target: Target{Hostname: "ref"}, Snap: snap}
	run.Refs = []*allRef{ref}
	for i := 0; i < 500; i++ {
		hs := &biosSnap{Vendor: "HPE", Model: "ProLiant DL360 Gen11", BiosVersion: "B", Key: snap.Key}
		run.Hosts = append(run.Hosts, &allHost{
			Target: Target{Hostname: fmt.Sprintf("h%03d", i)}, Snap: hs, Status: StatusSame, Ref: ref,
			Notes: []hostNote{{noteBiosVerDiff, "대상=B 기준=A"}},
		})
	}
	var rep bytes.Buffer
	writeAllReport(&rep, run, allReportOpts{ListMax: 10, DiffMax: 30})
	out := rep.String()
	if !strings.Contains(out, "BIOS_VER_DIFF  대상 BIOS 버전이 기준과 다름 (비교는 그대로 진행함): 500대") || !strings.Contains(out, "… 외 490대") {
		t.Errorf("특이사항:\n%s", out)
	}
	if strings.Contains(out, "h499") || len(strings.Split(out, "\n")) > 40 {
		t.Errorf("목록이 상한 없이 길어짐 (%d줄)\n%s", len(strings.Split(out, "\n")), out)
	}
}

// cmdAllCheck 연결: conf·pass.enc·diff.txt·user.txt 로 실제 서브커맨드를 돌린다 (-dry-run 은 거부).
func TestCmdAllCheckEndToEnd(t *testing.T) {
	fastCheck(t)
	dir := t.TempDir()
	_, ref := startAllSrv(t, "hpe-dl360gen11", nil)
	_, tgt := startAllSrv(t, "hpe-dl360gen11", func(s *mockbmc.Server) { setAttrs(t, s, map[string]interface{}{"ProcTurbo": "Disabled"}) })
	pass, key := filepath.Join(dir, "pass.enc"), filepath.Join(dir, "key.bin")
	must(t, encryptFromReader(strings.NewReader(testPass), pass, key))
	conf := filepath.Join(dir, "bios.conf")
	must(t, os.WriteFile(conf, []byte(fmt.Sprintf("user=%s\npass_file=%s\nkey_file=%s\nresult_dir=%s\nconcurrency=2\ntimeout=5\nretries=0\n",
		testUser, pass, key, filepath.Join(dir, "results"))), 0o600))
	must(t, os.WriteFile(filepath.Join(dir, "diff.txt"), []byte("# 기준\n"+ref+"\n"), 0o600))
	must(t, os.WriteFile(filepath.Join(dir, "user.txt"), []byte(tgt+"\n"+ref+"\n"), 0o600))
	hosts := writeHosts(t, "")
	args := []string{"-conf", conf, "-user", filepath.Join(dir, "user.txt"), "-diff", filepath.Join(dir, "diff.txt"), "-hosts", hosts}

	if err := cmdAllCheck(append([]string{"-dry-run"}, args...)); err == nil || !strings.Contains(err.Error(), "읽기 전용") {
		t.Errorf("-dry-run 오류 = %v", err)
	}
	if err := cmdAllCheck(append([]string{"-profile", "VM"}, args...)); err == nil {
		t.Error("-profile 이 거부되지 않음")
	}
	if err := cmdAllCheck(append([]string{"-ignore", filepath.Join(dir, "없는파일.txt")}, args...)); err == nil {
		t.Error("명시한 -ignore 파일이 없는데 오류가 아님")
	}
	if err := cmdAllCheck(append([]string{"-diff", filepath.Join(dir, "없음.txt")}, args[:4]...)); err == nil || !strings.Contains(err.Error(), "diff.txt.example") {
		t.Errorf("diff.txt 없음 오류 = %v", err)
	}

	if err := cmdAllCheck(args); err != nil {
		t.Fatalf("cmdAllCheck: %v", err)
	}
	dirs, err := filepath.Glob(filepath.Join(dir, "results", "*_all"))
	must(t, err)
	if len(dirs) != 1 {
		t.Fatalf("결과 폴더 = %v", dirs)
	}
	rows := readTSV(t, filepath.Join(dirs[0], "summary.tsv"))
	if len(rows) != 3 || rows[1][11] != StatusDiff || rows[2][11] != StatusIsReference {
		t.Errorf("summary.tsv = %v", rows)
	}
	dr := readTSV(t, filepath.Join(dirs[0], "all_diff.tsv"))
	if len(dr) != 2 || dr[1][5] != "ProcTurbo" || dr[1][8] != "DIFF" {
		t.Errorf("all_diff.tsv = %v", dr)
	}
}

// ignore_attrs.txt.example 는 내장 기본 목록과 같은 내용이어야 한다 (목록을 고칠 때 한쪽만 바꾸는 실수 방지).
func TestIgnoreExampleMatchesDefault(t *testing.T) {
	b, err := os.ReadFile("../../ignore_attrs.txt.example")
	if err != nil {
		t.Fatal(err)
	}
	ex := parseIgnore(string(b), "example")
	def := newIgnoreSet(defaultIgnore, "")
	if fmt.Sprint(ex.Pats) != fmt.Sprint(def.Pats) {
		t.Errorf("ignore_attrs.txt.example 패턴 %v\n내장 기본 목록        %v", ex.Pats, def.Pats)
	}
	// diff.txt.example 도 그대로 해석돼야 한다 (이름 3개, 마지막만 모델 칸).
	b, err = os.ReadFile("../../diff.txt.example")
	if err != nil {
		t.Fatal(err)
	}
	es := parseRefList(string(b))
	if len(es) != 3 || es[0].Input != "checkhostname1" || es[0].Model != "" || es[2].Model != "DL360Gen11" {
		t.Errorf("diff.txt.example = %v", es)
	}
}
