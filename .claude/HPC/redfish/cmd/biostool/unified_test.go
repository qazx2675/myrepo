package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"biostool/internal/mockbmc"
)

// unified.go / unified_cmd.go 시험. 실제 BMC 는 쓰지 않고 internal/mockbmc(testdata 합성 트리)만 쓴다.
// 호스트마다 mock 서버를 따로 띄우고(포트가 다름 → 서로 다른 대상) 속성·BIOS 버전은 SetBiosAttr/Modify 로 바꾼다.

func runUnifiedT(t *testing.T, rc *runContext, ign *ignoreSet) *unifiedRun {
	t.Helper()
	fastCheck(t)
	u, err := runUnified(rc, unifiedOpts{Profile: testProf(t), ResultDir: t.TempDir(), HostsPath: writeHosts(t, ""), Ignore: ign, Now: checkNow})
	if err != nil || u == nil {
		t.Fatalf("runUnified: %v", err)
	}
	return u
}

func compareT(t *testing.T, u *unifiedRun, ref compareRef) *compareResult {
	t.Helper()
	res, err := u.Compare(ref, progStage{})
	if err != nil {
		t.Fatalf("Compare(%+v): %v", ref, err)
	}
	if len(res.Hosts) != len(u.Snaps) {
		t.Fatalf("비교 호스트 %d, 기대 %d", len(res.Hosts), len(u.Snaps))
	}
	return res
}

func cmpHostOf(t *testing.T, res *compareResult, name string) *allHost {
	t.Helper()
	for _, h := range res.Hosts {
		if h.Target.Hostname == name {
			return h
		}
	}
	t.Fatalf("비교 대상 %s 없음", name)
	return nil
}

// linesOf 는 host 의 줄을 속성 → 줄로 모읍니다.
func linesOf(lines []ResultLine, host string) map[string]ResultLine {
	m := map[string]ResultLine{}
	for _, l := range lines {
		if l.Host == host {
			m[l.Attr] = l
		}
	}
	return m
}

// callCounts 는 서버마다 받은 요청 수입니다 (전체비교가 대상에 다시 접속하지 않는지 볼 때).
func callCounts(ss ...*mockbmc.Server) []int {
	out := make([]int, len(ss))
	for i, s := range ss {
		out[i] = s.CallCount()
	}
	return out
}

// 호스트당 세션 1회로 프로파일 판정 + 스냅샷, 비교서버가 대상 목록에 있으면 다시 접속하지 않음,
// 비교 줄에 관리 컨트롤러 모델, BIOS 버전 차이 특이사항, 속성 이름 목록 관문.
func TestUnifiedOneSessionProfileAndLiveRef(t *testing.T) {
	a, ua := startAllSrv(t, "hpe-dl360gen11", nil)
	b, ub := startAllSrv(t, "hpe-dl360gen11", func(s *mockbmc.Server) {
		setAttrs(t, s, map[string]interface{}{"ProcHyperthreading": "Disabled", "NewAttrX1": "v"})
	})
	c, uc := startAllSrv(t, "hpe-dl360gen11", func(s *mockbmc.Server) {
		setSys(t, s, map[string]interface{}{"BiosVersion": "U54 v9.99 (01/01/2026)"})
		setAttrs(t, s, map[string]interface{}{"ProcTurbo": "Disabled"})
	})
	d, ud := startAllSrv(t, "dell-r660", nil)
	all := []*mockbmc.Server{a, b, c, d}
	rc := checkRCFor(t, 4, "", ua, ub, uc, ud)
	u := runUnifiedT(t, rc, nil)

	for i, s := range all {
		assertOneSession(t, s)
		assertClean(t, s)
		if sn := u.Snaps[i]; !sn.ok() || len(sn.Attrs) < 15 || sn.MgmtModel == "" || sn.MgmtModel == "-" || sn.Key == "" {
			t.Errorf("스냅샷 %d = %s 속성 %d개 mgmt %q key %q", i, sn.Status, len(sn.Attrs), sn.MgmtModel, sn.Key)
		}
		if u.Check.Hosts[i].attrs != nil {
			t.Errorf("hostCheck.attrs 가 남아 있음 (스냅샷에만 두어야 함)")
		}
	}
	for _, f := range []string{"result.tsv", "ok.txt", "fail.tsv", "retry.txt", "run_info.txt"} {
		if _, err := os.Stat(filepath.Join(u.Check.Dir, f)); err != nil {
			t.Errorf("기존 결과 파일 %s 없음: %v", f, err)
		}
	}

	// 프로파일 줄: b 의 ProcHyperthreading 만 difference, 관리 컨트롤러 모델 채움
	pl := u.ProfileLines()
	if fmt.Sprint(pl) != fmt.Sprint(profileLines(u.Check.Hosts)) {
		t.Error("ProfileLines 가 profileLines(Check.Hosts) 와 다름")
	}
	if l := linesOf(pl, ub)["ProcHyperthreading"]; l.Tag != TagDiff || l.Note != StatusFail || l.MgmtModel != "iLO 6 (mock)" {
		t.Errorf("b 프로파일 줄 = %+v", l)
	}
	if l := linesOf(pl, ua)["ProcHyperthreading"]; l.Tag != TagOK {
		t.Errorf("a 프로파일 줄 = %+v", l)
	}
	if fmt.Sprint(u.HostOrder()) != fmt.Sprint([]string{ua, ub, uc, ud}) {
		t.Errorf("HostOrder = %v", u.HostOrder())
	}

	// 비교서버 = 대상 a → 어떤 서버에도 새 요청이 없어야 한다
	before := callCounts(all...)
	res := compareT(t, u, compareRef{Hosts: []string{ua}})
	if fmt.Sprint(callCounts(all...)) != fmt.Sprint(before) {
		t.Errorf("전체비교가 대상에 다시 접속함: %v → %v", before, callCounts(all...))
	}
	if h := cmpHostOf(t, res, ua); h.Status != StatusIsReference {
		t.Errorf("a = %s, 기대 REFERENCE", h.Status)
	}
	hb := cmpHostOf(t, res, ub)
	if hb.Status != StatusDiff || hb.NDiff != 1 || hb.NOnlyHost != 1 || hb.sameAttrNames() {
		t.Errorf("b = %s diff %d only_host %d same=%v, 기대 DIFF 1/1, 속성 목록 다름", hb.Status, hb.NDiff, hb.NOnlyHost, hb.sameAttrNames())
	}
	bl := linesOf(res.Lines, ub)
	if l := bl["ProcHyperthreading"]; l.Kind != KindCompare || l.Tag != TagDiff || l.Expected != "Enabled" || l.Current != "Disabled" ||
		l.MgmtModel != "iLO 6 (mock)" || l.MgmtVersion == "-" {
		t.Errorf("b 비교 줄 = %+v", l)
	}
	if l := bl["NewAttrX1"]; l.Note != attrOnlyHost || l.Expected != noneValue {
		t.Errorf("b NewAttrX1 = %+v", l)
	}
	if _, ok := bl["SerialNumber"]; ok {
		t.Error("내장 기본 제외 속성(SerialNumber) 줄이 있음")
	}
	hc := cmpHostOf(t, res, uc)
	if hc.Status != StatusDiff || !hc.hasNote(noteBiosVerDiff) || !hc.sameAttrNames() {
		t.Errorf("c = %s notes %v same=%v, 기대 DIFF + BIOS_VER_DIFF + 속성 목록 같음", hc.Status, hc.Notes, hc.sameAttrNames())
	}
	hd := cmpHostOf(t, res, ud)
	if hd.Status != StatusNoReference || !strings.Contains(hd.Detail, "비교서버") || hd.sameAttrNames() {
		t.Errorf("dell = %s (%s), 기대 NO_REFERENCE (다른 모델과 비교하지 않음)", hd.Status, hd.Detail)
	}
	if l := linesOf(res.Lines, ud)["-"]; !l.HostError || l.MgmtModel == "-" {
		t.Errorf("dell 비교 줄 = %+v (호스트 줄 1개, 관리 컨트롤러 채움)", l)
	}

	// CSV
	paths, err := u.WriteOutputs("compare_", res.Lines)
	if err != nil || len(paths) < 4 {
		t.Fatalf("WriteOutputs = %v, %v", paths, err)
	}
	if _, err := os.Stat(filepath.Join(u.Check.Dir, "compare_long.csv")); err != nil {
		t.Error(err)
	}
	for _, s := range all {
		assertClean(t, s)
	}
}

// 대상 목록에 없는 비교서버는 그 서버만 세션 1회·GET 4번, 대상은 다시 접속하지 않음. exclude_attrs 는 ignore 와 합쳐 비교에서 빠짐.
func TestUnifiedExternalRefAndExclude(t *testing.T) {
	r, ur := startAllSrv(t, "hpe-dl360gen11", nil)
	a, ua := startAllSrv(t, "hpe-dl360gen11", func(s *mockbmc.Server) {
		setAttrs(t, s, map[string]interface{}{"ProcTurbo": "Disabled", "ServerName": "other"})
	})
	rc := checkRCFor(t, 2, "", ua)
	rc.Conf.ExcludeAttrs = []string{"procturbo"}
	u := runUnifiedT(t, rc, parseIgnore("ServerName\n", "ignore_attrs.txt"))
	if !strings.Contains(u.Ignore.Source, "exclude_attrs") || !u.Ignore.match("ProcTurbo") || !u.Ignore.match("servername") {
		t.Fatalf("제외 = %+v, 기대 ignore_attrs.txt ∪ exclude_attrs", u.Ignore)
	}
	before := a.CallCount()
	res := compareT(t, u, compareRef{Hosts: []string{ur}})
	if a.CallCount() != before {
		t.Errorf("대상에 다시 접속함 (%d → %d)", before, a.CallCount())
	}
	assertOneSession(t, r)
	if g := r.Count("GET", "/"); g != 4 {
		t.Errorf("비교서버 GET %d회, 기대 4 (ServiceRoot, Systems, System, Bios)", g)
	}
	assertClean(t, r)
	h := cmpHostOf(t, res, ua)
	if h.Status != StatusSame || !h.sameAttrNames() || h.Ignored != 2 {
		t.Errorf("a = %s ignored %d diffs %v, 기대 SAME (ProcTurbo·ServerName 제외)", h.Status, h.Ignored, diffKeys(h))
	}
	al := linesOf(res.Lines, ua)
	if _, ok := al["ProcTurbo"]; ok {
		t.Error("exclude_attrs 속성 ProcTurbo 줄이 있음")
	}
	if l := al["ProcHyperthreading"]; l.Tag != TagOK || l.Note != StatusSame {
		t.Errorf("같은 값 줄 = %+v", l)
	}

	// 같은 수집 결과로 다시 비교 (제외 없음): 비교서버만 다시 읽고 ProcTurbo 가 difference
	u.Ignore = newIgnoreSet(nil, "없음")
	res2 := compareT(t, u, compareRef{Hosts: []string{ur}})
	if l := linesOf(res2.Lines, ua)["ProcTurbo"]; l.Tag != TagDiff {
		t.Errorf("제외 없이 ProcTurbo = %+v", l)
	}
	if a.CallCount() != before || r.Sessions().Created != 2 {
		t.Errorf("재비교: 대상 요청 %d→%d, 비교서버 세션 %+v", before, a.CallCount(), r.Sessions())
	}
	assertClean(t, a)
}

// refs JSON 기준 (목록·읽기·같은 모델만·BIOS 버전 차이) 과 diff.txt 기준.
func TestUnifiedCompareJSONAndDiffTxt(t *testing.T) {
	a, ua := startAllSrv(t, "hpe-dl360gen11", nil)
	b, ub := startAllSrv(t, "hpe-dl360gen11", func(s *mockbmc.Server) {
		setAttrs(t, s, map[string]interface{}{"ProcTurbo": "Disabled"})
	})
	g, ug := startAllSrv(t, "hpe-dl360gen10", nil)
	r10, ur10 := startAllSrv(t, "hpe-dl360gen10", func(s *mockbmc.Server) {
		setAttrs(t, s, map[string]interface{}{"WorkloadProfile": "Virtualization-MaxPerformance"})
	})
	rc := checkRCFor(t, 3, "", ua, ub, ug)
	u := runUnifiedT(t, rc, nil)
	targets := []*mockbmc.Server{a, b, g}

	// a 의 스냅샷을 export 형식으로 저장하되 BIOS 버전만 다르게 → 기준 JSON
	refs := t.TempDir()
	sa := *u.Snaps[0]
	sa.BiosVersion = "U54 v0.01 (01/01/2020)"
	var n int32
	must(t, exportAttrs(refs, &sa, &n))
	must(t, os.WriteFile(filepath.Join(refs, "bad.json"), []byte(`{"host":"x","attributes":{}}`), 0o600))
	must(t, os.WriteFile(filepath.Join(refs, "note.txt"), []byte("json 아님"), 0o600))
	list, err := listRefJSON(refs)
	if err != nil || len(list) != 2 {
		t.Fatalf("listRefJSON = %+v, %v", list, err)
	}
	good, bad := list[0], list[1] // 이름순: 127.0.0.1_<포트>.json, bad.json
	if bad.Name != "bad.json" || bad.Err == "" {
		t.Errorf("bad.json = %+v, 기대 Err", bad)
	}
	if good.Err != "" || good.Host != ua || good.Vendor != "HPE" || good.Model != "ProLiant DL360 Gen11" || good.Attrs != len(u.Snaps[0].Attrs) {
		t.Errorf("기준 JSON = %+v", good)
	}
	if l, err := listRefJSON(filepath.Join(refs, "없음")); err != nil || l != nil {
		t.Errorf("없는 폴더 = %v, %v", l, err)
	}

	before := callCounts(targets...)
	res := compareT(t, u, compareRef{JSONPath: good.Path})
	if fmt.Sprint(callCounts(targets...)) != fmt.Sprint(before) {
		t.Error("JSON 기준 비교가 대상에 접속함")
	}
	ha := cmpHostOf(t, res, ua)
	if ha.Status != StatusSame || !ha.hasNote(noteBiosVerDiff) || !ha.sameAttrNames() {
		t.Errorf("a = %s notes %v, 기대 SAME + BIOS_VER_DIFF (JSON 은 자기 자신으로 보지 않음)", ha.Status, ha.Notes)
	}
	if hb := cmpHostOf(t, res, ub); hb.Status != StatusDiff || fmt.Sprint(diffKeys(hb)) != "[DIFF|ProcTurbo|Enabled|Disabled]" {
		t.Errorf("b = %s %v", hb.Status, diffKeys(hb))
	}
	if hg := cmpHostOf(t, res, ug); hg.Status != StatusNoReference {
		t.Errorf("gen10 = %s, 기대 NO_REFERENCE (다른 모델)", hg.Status)
	}
	if !strings.Contains(res.Label, filepath.Base(good.Path)) {
		t.Errorf("Label = %q", res.Label)
	}
	if _, err := u.Compare(compareRef{JSONPath: filepath.Join(refs, "bad.json")}, progStage{}); err == nil {
		t.Error("속성 없는 JSON 이 기준으로 받아들여짐")
	}
	if _, err := u.Compare(compareRef{JSONPath: good.Path, DiffFile: "diff.txt"}, progStage{}); err == nil {
		t.Error("기준 두 개를 함께 받아들임")
	}

	// diff.txt: a(대상, 재사용) + r10(대상 밖, 1회 조회)
	diff := filepath.Join(t.TempDir(), "diff.txt")
	must(t, os.WriteFile(diff, []byte("# 기준\n"+ua+"\n"+ur10+"\n"), 0o600))
	res = compareT(t, u, compareRef{DiffFile: diff})
	if fmt.Sprint(callCounts(targets...)) != fmt.Sprint(before) {
		t.Error("diff.txt 기준 비교가 대상에 접속함")
	}
	assertOneSession(t, r10)
	assertClean(t, r10)
	if h := cmpHostOf(t, res, ua); h.Status != StatusIsReference {
		t.Errorf("a = %s, 기대 REFERENCE", h.Status)
	}
	if h := cmpHostOf(t, res, ub); h.Status != StatusDiff || h.Ref.Target.Hostname != ua {
		t.Errorf("b = %s ref %v", h.Status, h.Ref)
	}
	hg := cmpHostOf(t, res, ug)
	if hg.Status != StatusDiff || hg.Ref.Target.Hostname != ur10 || fmt.Sprint(diffKeys(hg)) != "[DIFF|WorkloadProfile|Virtualization-MaxPerformance|"+u.Snaps[2].Attrs["WorkloadProfile"]+"]" {
		t.Errorf("gen10 = %s %v", hg.Status, diffKeys(hg))
	}
	if l := linesOf(res.Lines, ug)["WorkloadProfile"]; l.MgmtModel == "-" || l.Vendor != "HPE" {
		t.Errorf("gen10 비교 줄 = %+v", l)
	}
	for _, s := range targets {
		assertOneSession(t, s)
		assertClean(t, s)
	}
}

// 진행 표시: 접속·조회 단계에서 호스트마다 Add (OK / difference / 오류).
func TestUnifiedProgressCounts(t *testing.T) {
	_, ua := startAllSrv(t, "hpe-dl360gen11", nil)
	_, ub := startAllSrv(t, "hpe-dl360gen11", func(s *mockbmc.Server) {
		setAttrs(t, s, map[string]interface{}{"ProcHyperthreading": "Disabled"})
	})
	fastCheck(t)
	rc := checkRCFor(t, 2, "", ua, ub, "127.0.0.1:1") // 마지막은 접속 실패
	var buf strings.Builder
	p := newProgress(&buf, false, 3)
	u, err := runUnified(rc, unifiedOpts{Profile: testProf(t), ResultDir: t.TempDir(), Now: checkNow, Collect: progStage{P: p, Idx: 2, Name: "접속·조회"}})
	if err != nil || u == nil {
		t.Fatal(err)
	}
	p.Stop()
	if !strings.Contains(buf.String(), "[2/3 접속·조회 3/3 · OK 1 difference 1 N/A 0 오류 1") {
		t.Errorf("진행 표시 = %q", buf.String())
	}
	if u.Snaps[2].ok() {
		t.Error("접속 실패 호스트 스냅샷이 ok")
	}
}

// bios -auto / -auto-check: 프로파일 점검만, ./check.res_<user>(+_postapply), 쓰기 요청 없음, FAIL 이 있어도 종료 0.
func TestBiosAutoCheckWritesCheckRes(t *testing.T) {
	e := newRTEnv(t, 3, "")
	a, ua := startAllSrv(t, "hpe-dl360gen11", nil)
	b, ub := startAllSrv(t, "hpe-dl360gen11", func(s *mockbmc.Server) {
		setAttrs(t, s, map[string]interface{}{"ProcHyperthreading": "Disabled"})
	})
	conf, err := os.ReadFile(e.conf)
	must(t, err)
	must(t, os.WriteFile(e.conf, append(conf, []byte("auto_profile=VM\n")...), 0o600))
	targets := e.writeFile("targets.txt", ua, ub)
	wd, err := os.Getwd()
	must(t, err)
	must(t, os.Chdir(e.dir))
	t.Cleanup(func() { _ = os.Chdir(wd) })

	before := e.dirs()
	out, err := rtCapture(t, func() error {
		return cmdBios([]string{"-conf", e.conf, "-hosts", e.hosts, "-auto", "alice", targets})
	})
	if err != nil {
		t.Fatalf("bios -auto: %v\n%s", err, out)
	}
	want := ua + ": OK\n" + ub + ": FAIL difference 1건 (ProcHyperthreading Disabled→Enabled)\n"
	for _, f := range []string{"check.res_alice", "check.res_alice_postapply"} {
		got, err := os.ReadFile(filepath.Join(e.dir, f))
		if err != nil || string(got) != want {
			t.Errorf("%s =\n%s\n기대\n%s (%v)", f, got, want, err)
		}
		if st, err := os.Stat(filepath.Join(e.dir, f)); err == nil && st.Mode().Perm() != 0o644 && filepath.Separator == '/' {
			t.Errorf("%s 권한 %v, 기대 0644", f, st.Mode().Perm())
		}
	}
	dir := e.newDir(before)
	for _, f := range []string{"result.tsv", "ok.txt", "fail.tsv", "retry.txt", "run_info.txt", "profile_summary.csv", "profile_long.csv"} {
		if _, err := os.Stat(filepath.Join(dir, f)); err != nil {
			t.Errorf("결과 폴더에 %s 없음", f)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "compare_long.csv")); err == nil {
		t.Error("auto 모드가 전체비교 CSV 를 씀")
	}
	if !strings.Contains(out, "check.res_alice_postapply") {
		t.Errorf("출력 = %s", out)
	}

	// -auto-check (옵션을 위치 인자 뒤에): _postapply 없음
	_, err = rtCapture(t, func() error {
		return cmdBios([]string{"-conf", e.conf, "-hosts", e.hosts, "-auto-check", "bob", targets, "-profile", "VM"})
	})
	must(t, err)
	if got, err := os.ReadFile(filepath.Join(e.dir, "check.res_bob")); err != nil || string(got) != want {
		t.Errorf("check.res_bob = %s (%v)", got, err)
	}
	if _, err := os.Stat(filepath.Join(e.dir, "check.res_bob_postapply")); err == nil {
		t.Error("-auto-check 가 _postapply 를 씀")
	}

	// 쓰기 요청 없음: PATCH 0, POST 는 세션 로그인뿐, 실행마다 세션 1회
	for _, s := range []*mockbmc.Server{a, b} {
		assertClean(t, s)
		if s.Count("PATCH", "/") != 0 || s.Count("POST", "/") != s.Count("POST", "/redfish/v1/SessionService/Sessions") {
			t.Errorf("쓰기 요청이 나감: PATCH %d POST %d", s.Count("PATCH", "/"), s.Count("POST", "/"))
		}
		if ss := s.Sessions(); ss.Created != 2 || ss.Deleted != 2 {
			t.Errorf("세션 = %+v, 기대 실행 2번 × 1", ss)
		}
	}

	// 치명적 오류만 0 이 아닌 종료: 대상 없음 · 잘못된 사용자 이름 · 프로파일 없음 · 비교 옵션
	empty := e.writeFile("empty.txt", "# 없음")
	for _, args := range [][]string{
		{"-auto-check", "carol", empty},
		{"-auto-check", "../x", targets},
		{"-auto-check", "carol"},
		{"-auto", "carol", targets, "-profile", "NOPE"},
		{"-auto", "carol", targets, "-ref", ua},
	} {
		_, err := rtCapture(t, func() error { return cmdBios(append([]string{"-conf", e.conf, "-hosts", e.hosts}, args...)) })
		if err == nil {
			t.Errorf("%v: 오류가 아님", args)
		}
	}
	if _, err := os.Stat(filepath.Join(e.dir, "check.res_carol")); err == nil {
		t.Error("치명적 오류인데 check.res_carol 이 생김")
	}
}

// bios -res-dir: check.res_<user> 를 현재 폴더가 아니라 지정한 폴더에 쓴다 (bios.sh 래퍼가 호출한 쪽 작업 폴더를 넘김).
func TestBiosAutoResDir(t *testing.T) {
	e := newRTEnv(t, 3, "")
	_, ua := startAllSrv(t, "hpe-dl360gen11", nil)
	conf, err := os.ReadFile(e.conf)
	must(t, err)
	must(t, os.WriteFile(e.conf, append(conf, []byte("auto_profile=VM\n")...), 0o600))
	targets := e.writeFile("targets.txt", ua)
	job := filepath.Join(e.dir, "job1")
	must(t, os.Mkdir(job, 0o755))
	wd, err := os.Getwd()
	must(t, err)
	must(t, os.Chdir(e.dir))
	t.Cleanup(func() { _ = os.Chdir(wd) })

	out, err := rtCapture(t, func() error {
		return cmdBios([]string{"-conf", e.conf, "-hosts", e.hosts, "-res-dir", job, "-auto", "dave", targets})
	})
	if err != nil {
		t.Fatalf("bios -auto -res-dir: %v\n%s", err, out)
	}
	for _, f := range []string{"check.res_dave", "check.res_dave_postapply"} {
		if got, err := os.ReadFile(filepath.Join(job, f)); err != nil || string(got) != ua+": OK\n" {
			t.Errorf("%s = %q (%v)", f, got, err)
		}
		if _, err := os.Stat(filepath.Join(e.dir, f)); err == nil {
			t.Errorf("%s 가 현재 폴더에도 생김", f)
		}
	}
	if !strings.Contains(out, filepath.Join(job, "check.res_dave")) {
		t.Errorf("출력에 결과 경로가 없음: %s", out)
	}
}

// bios -ref-json (refs_dir 의 파일 이름만) / -ref :프로파일 줄과 전체비교 줄을 출력하고 profile_·compare_ CSV 를 씀.
func TestBiosCmdCompare(t *testing.T) {
	e := newRTEnv(t, 3, "")
	a, ua := startAllSrv(t, "hpe-dl360gen11", nil)
	b, ub := startAllSrv(t, "hpe-dl360gen11", func(s *mockbmc.Server) {
		setAttrs(t, s, map[string]interface{}{"ProcTurbo": "Disabled"})
	})
	refs := filepath.Join(e.dir, "refs")
	must(t, os.MkdirAll(refs, 0o700))
	conf, err := os.ReadFile(e.conf)
	must(t, err)
	must(t, os.WriteFile(e.conf, append(conf, []byte("refs_dir="+refs+"\nexclude_attrs=NoSuchAttr\n")...), 0o600))
	targets := e.writeFile("targets.txt", ua, ub)

	before := e.dirs()
	out, err := rtCapture(t, func() error {
		return cmdBios([]string{"-conf", e.conf, "-hosts", e.hosts, "-user", targets, "-profile", "VM", "-plain", "-ref", ua})
	})
	if err != nil {
		t.Fatalf("bios -ref: %v\n%s", err, out)
	}
	if !strings.Contains(out, "== 전체비교 (기준: 비교서버 "+ua) || !strings.Contains(out, "[difference] "+ub) ||
		!strings.Contains(out, "ProcTurbo\tEnabled\tDisabled\tDIFF") || strings.Contains(out, "\x1b[") {
		t.Errorf("출력 =\n%s", out)
	}
	dir := e.newDir(before)
	for _, f := range []string{"profile_long.csv", "compare_long.csv", "compare_summary.csv"} {
		if _, err := os.Stat(filepath.Join(dir, f)); err != nil {
			t.Errorf("%s 없음", f)
		}
	}

	// 기준 JSON: 첫 실행의 a 스냅샷과 같은 내용을 refs/ 에 두고 파일 이름만 준다
	attrs := map[string]interface{}{}
	var bios struct{ Attributes map[string]interface{} }
	c, _ := mockClient(a, ModeReadOnly, 0)
	must(t, c.GetJSON("/redfish/v1/systems/1/bios", &bios))
	_ = c.Close()
	for k, v := range bios.Attributes {
		attrs[k] = v
	}
	jb, err := json.Marshal(map[string]interface{}{"host": "golden", "vendor": "HPE", "model": "ProLiant DL360 Gen11", "bios_version": "x", "attributes": attrs})
	must(t, err)
	must(t, os.WriteFile(filepath.Join(refs, "golden.json"), jb, 0o600))
	out, err = rtCapture(t, func() error {
		return cmdBios([]string{"-conf", e.conf, "-hosts", e.hosts, "-user", targets, "-profile", "VM", "-plain", "-ref-json", "golden.json"})
	})
	if err != nil {
		t.Fatalf("bios -ref-json: %v\n%s", err, out)
	}
	if !strings.Contains(out, "refs JSON golden.json") || !strings.Contains(out, "[difference] "+ub) || !strings.Contains(out, noteBiosVerDiff) {
		t.Errorf("출력 =\n%s", out)
	}
	// 없는 JSON 은 접속 전에 오류
	n := a.Sessions().Created
	if _, err := rtCapture(t, func() error {
		return cmdBios([]string{"-conf", e.conf, "-hosts", e.hosts, "-user", targets, "-profile", "VM", "-ref-json", "none.json"})
	}); err == nil || a.Sessions().Created != n {
		t.Errorf("없는 JSON: err=%v, 세션 %d→%d", err, n, a.Sessions().Created)
	}
	for _, s := range []*mockbmc.Server{a, b} {
		assertClean(t, s)
	}
}
