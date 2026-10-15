package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"biostool/internal/mockbmc"
)

// resultconv.go 와 bios.conf exclude_attrs 적용(전체비교 합집합·프로파일 점검 제외) 시험.

// lineKey 는 줄을 비교하기 쉬운 문자열로 만듭니다.
func lineKey(l ResultLine) string {
	return fmt.Sprintf("%s|%s|%s|%s|%s|%t", l.Tag, l.Attr, l.Expected, l.Current, l.Note, l.HostError)
}

func TestMergeExclude(t *testing.T) {
	base := newIgnoreSet(defaultIgnore, "내장 기본 목록")
	if got := mergeExclude(base, nil); got != base {
		t.Error("exclude_attrs 가 비면 원래 목록을 그대로 써야 함")
	}
	m := mergeExclude(base, []string{"ProcTurbo", "*uuid*", "Mem*Mode"})
	for _, n := range []string{"procturbo", "PROCTURBO", "ServiceTag", "SystemUuid", "MemOpMode"} {
		if !m.match(n) {
			t.Errorf("%s 가 제외돼야 함 (합집합)", n)
		}
	}
	if m.match("ProcTurboX") || m.match("LogicalProc") {
		t.Error("패턴에 없는 속성이 제외됨")
	}
	if len(m.Pats) != len(base.Pats)+2 { // *uuid* 는 기본 목록과 겹쳐 한 번만
		t.Errorf("패턴 수 %d, 기대 %d (중복 제거)", len(m.Pats), len(base.Pats)+2)
	}
	if !strings.Contains(m.Source, "내장 기본 목록") || !strings.Contains(m.Source, "exclude_attrs") {
		t.Errorf("Source = %q", m.Source)
	}
	if n := mergeExclude(nil, []string{"A"}); !n.match("a") {
		t.Error("ignore 목록이 없어도 exclude_attrs 는 적용돼야 함")
	}
	var nilSet *ignoreSet
	if nilSet.match("x") {
		t.Error("nil 목록은 아무것도 제외하지 않아야 함")
	}
}

// cmdAllCheck: bios.conf exclude_attrs 로 뺀 속성은 ignore_attrs.txt(내장 기본 목록)와 함께 비교에서 빠진다.
func TestCmdAllCheckExcludeAttrs(t *testing.T) {
	fastCheck(t)
	dir := t.TempDir()
	_, ref := startAllSrv(t, "hpe-dl360gen11", nil)
	_, tgt := startAllSrv(t, "hpe-dl360gen11", func(s *mockbmc.Server) { setAttrs(t, s, map[string]interface{}{"ProcTurbo": "Disabled"}) })
	pass, key := filepath.Join(dir, "pass.enc"), filepath.Join(dir, "key.bin")
	must(t, encryptFromReader(strings.NewReader(testPass), pass, key))
	conf := filepath.Join(dir, "bios.conf")
	must(t, os.WriteFile(conf, []byte(fmt.Sprintf("user=%s\npass_file=%s\nkey_file=%s\nresult_dir=%s\nconcurrency=2\ntimeout=5\nretries=0\nexclude_attrs=nothing|procturbo\n",
		testUser, pass, key, filepath.Join(dir, "results"))), 0o600))
	must(t, os.WriteFile(filepath.Join(dir, "diff.txt"), []byte(ref+"\n"), 0o600))
	must(t, os.WriteFile(filepath.Join(dir, "user.txt"), []byte(tgt+"\n"), 0o600))
	args := []string{"-conf", conf, "-user", filepath.Join(dir, "user.txt"), "-diff", filepath.Join(dir, "diff.txt"), "-hosts", writeHosts(t, "")}
	if err := cmdAllCheck(args); err != nil {
		t.Fatalf("cmdAllCheck: %v", err)
	}
	dirs, err := filepath.Glob(filepath.Join(dir, "results", "*_all"))
	must(t, err)
	if len(dirs) != 1 {
		t.Fatalf("결과 폴더 = %v", dirs)
	}
	rows := readTSV(t, filepath.Join(dirs[0], "summary.tsv"))
	if len(rows) != 2 || rows[1][11] != StatusSame {
		t.Errorf("ProcTurbo 를 뺐으니 SAME 이어야 함: %v", rows)
	}
	if dr := readTSV(t, filepath.Join(dirs[0], "all_diff.tsv")); len(dr) != 1 {
		t.Errorf("all_diff.tsv 에 차이 행이 없어야 함: %v", dr)
	}
	info, err := os.ReadFile(filepath.Join(dirs[0], "run_info.txt"))
	must(t, err)
	if !strings.Contains(string(info), "exclude_attrs") {
		t.Errorf("run_info.txt 에 제외 출처가 없음:\n%s", info)
	}
}

// 프로파일 점검: exclude_attrs 에 걸린 속성의 항목은 점검·집계하지 않는다 (ignore_attrs.txt 는 쓰지 않음).
func TestCheckProfileExclude(t *testing.T) {
	s := startMockTree(t, "dell-r660", nil)
	must(t, s.SetBiosAttr("LogicalProc", "Disabled")) // 제외 안 하면 hyper_threading FAIL
	must(t, s.SetBiosAttr("LlcPrefetch", nil))        // 제외 안 하면 llc_prefetch MAPPING_MISSING
	rc := checkRCFor(t, 1, "", s.URL())
	// LogicalProc: BMC 에서 찾은 이름, *CLUSTER*: 대소문자 무시, LLCPREFETCH: BMC 에 없지만 후보가 모두 제외
	rc.Conf.ExcludeAttrs = []string{"logicalproc", "*CLUSTER*", "LLCPREFETCH"}
	run := runCheckT(t, rc, "")
	h := run.Hosts[0]
	if h.Status != "" {
		t.Fatalf("Status=%q %s", h.Status, h.Detail)
	}
	var stds []string
	for _, it := range h.Items {
		stds = append(stds, it.Std+"="+it.Result)
	}
	if got := strings.Join(stds, ","); got != "system_profile=OK,virtualization=OK" {
		t.Errorf("항목 = %s", got)
	}
	if !h.allGood() {
		t.Error("제외 후 남은 항목이 모두 OK 인데 allGood 이 아님")
	}
	if rows := readLines(t, filepath.Join(run.Dir, "result.tsv")); len(rows) != 1+2 {
		t.Errorf("result.tsv 행 %d, 기대 머리글+2", len(rows))
	}
	if h.MgmtModel == "" || h.MgmtModel == "-" {
		t.Errorf("MgmtModel 이 채워지지 않음: %q (MgmtVersion %q)", h.MgmtModel, h.MgmtVersion)
	}
	// ignore_attrs 기본 목록(*Uuid* 등)은 프로파일 점검에 쓰지 않는다: exclude 가 없으면 5항목 그대로.
	rc2 := checkRCFor(t, 1, "", s.URL())
	if n := len(runCheckT(t, rc2, "").Hosts[0].Items); n != 5 {
		t.Errorf("exclude 없음: 항목 %d개, 기대 5", n)
	}
}

func TestMgmtModelVersion(t *testing.T) {
	get, err := newDumpFileGet("../../testdata/hpe-dl360gen10")
	must(t, err)
	m := newMemoGet(get)
	info, err := detectSystemWith(m.fetch)
	must(t, err)
	if mm, v := mgmtModelVersion(m, info); mm != "-" || v != "-" {
		t.Errorf("Manager 읽기 전 = %q %q", mm, v)
	}
	_, err = m.fetch(info.ManagerPath)
	must(t, err)
	if mm, v := mgmtModelVersion(m, info); mm != "iLO 5 (mock)" || v != mgmtVersion(m, info) {
		t.Errorf("= %q %q", mm, v)
	}
}

func TestProfileLines(t *testing.T) {
	ok := &hostCheck{
		Target:      Target{Hostname: "h1", IP: "10.0.0.1"},
		Info:        &SystemInfo{Vendor: "HPE", Model: "ProLiant DL360 Gen10", BiosVersion: "U32"},
		MgmtModel:   "iLO 5",
		MgmtVersion: "iLO 5 2.99",
		Items: []itemResult{
			{Std: "a", Attr: "A", Expected: "E", Current: "E", Result: StatusOK},
			{Std: "b", Attr: "B", Expected: "E", Current: "X", Result: StatusPendingOK},
			{Std: "c", Attr: "C", Expected: "E", Current: "-", Result: StatusFail},
			{Std: "d", Attr: "D", Expected: "E", Current: "X", Result: StatusUnverified},
			{Std: "e", Attr: "E", Expected: "E", Current: "X", Result: StatusPendingExists},
			{Std: "f", Attr: "F", Expected: "E", Current: "E", Result: StatusPendingNoJob},
			{Std: "g", Attr: "G", Expected: "E", Current: "-", Result: StatusMappingMissing},
			{Std: "llc_prefetch", Attr: "-", Expected: "-", Current: "-", Result: StatusMappingMissing},
		},
	}
	bad := &hostCheck{Target: Target{Hostname: "h2"}, Status: StatusUnreachable}
	got := profileLines([]*hostCheck{ok, bad})
	want := []string{
		"OK|A|E|E|OK|false",
		"OK|B|E|X|PENDING_OK|false",
		"difference|C|E|(N/A)|FAIL|false",
		"difference|D|E|X|UNVERIFIED|false",
		"difference|E|E|X|PENDING_EXISTS|false",
		"difference|F|E|E|PENDING_NO_JOB|false",
		"N/A|G|E|(N/A)|MAPPING_MISSING|false",
		"N/A|llc_prefetch|(N/A)|(N/A)|MAPPING_MISSING|false",
		"N/A|-|(N/A)|(N/A)|UNREACHABLE|true",
	}
	if len(got) != len(want) {
		t.Fatalf("줄 %d개, 기대 %d개: %+v", len(got), len(want), got)
	}
	for i, l := range got {
		if k := lineKey(l); k != want[i] {
			t.Errorf("줄 %d = %s, 기대 %s", i, k, want[i])
		}
		if l.Kind != KindProfile {
			t.Errorf("줄 %d Kind = %q", i, l.Kind)
		}
	}
	l := got[0]
	if l.Host != "h1" || l.IP != "10.0.0.1" || l.Vendor != "HPE" || l.Model != "ProLiant DL360 Gen10" || l.BiosVersion != "U32" ||
		l.MgmtModel != "iLO 5" || l.MgmtVersion != "iLO 5 2.99" {
		t.Errorf("호스트 칸: %+v", l)
	}
	e := got[len(got)-1]
	if e.Host != "h2" || e.IP != "-" || e.Vendor != "-" || e.Model != "-" || e.BiosVersion != "-" || e.MgmtModel != "-" || e.MgmtVersion != "-" {
		t.Errorf("오류 호스트 칸: %+v", e)
	}
}

func TestCompareLines(t *testing.T) {
	ref := &allRef{Target: Target{Hostname: "ref"}, Snap: &biosSnap{Attrs: map[string]string{
		"A": "1", "B": "1", "C": "x", "SystemUuid": "u1", "Empty": "", "Turbo": "On",
	}}}
	h := &allHost{
		Target: Target{Hostname: "h1", IP: "10.0.0.1"}, Status: StatusDiff, Ref: ref,
		Snap: &biosSnap{Vendor: "Dell", Model: "PowerEdge R660", BiosVersion: "1.8.2", Attrs: map[string]string{
			"A": "1", "B": "2", "D": "y", "SystemUuid": "u2", "Empty": "", "Turbo": "Off",
		}},
	}
	ign := mergeExclude(newIgnoreSet(defaultIgnore, "내장"), []string{"turbo"})
	got := compareLines(h, ign)
	want := []string{
		"OK|A|1|1|SAME|false",
		"difference|B|1|2|DIFF|false",
		"N/A|C|x|(N/A)|ONLY_REF|false",
		"difference|D|(없음)|y|ONLY_HOST|false",
		"OK|Empty|(N/A)|(N/A)|SAME|false",
	}
	if len(got) != len(want) {
		t.Fatalf("줄 %d개, 기대 %d개 (SystemUuid·Turbo 는 제외): %+v", len(got), len(want), got)
	}
	for i, l := range got {
		if k := lineKey(l); k != want[i] {
			t.Errorf("줄 %d = %s, 기대 %s", i, k, want[i])
		}
		if l.Kind != KindCompare || l.Host != "h1" || l.Vendor != "Dell" || l.BiosVersion != "1.8.2" || l.MgmtModel != "-" {
			t.Errorf("줄 %d 호스트 칸: %+v", i, l)
		}
	}

	for _, st := range []string{StatusUnreachable, StatusNoReference} {
		e := &allHost{Target: Target{Hostname: "h2"}, Status: st, Snap: &biosSnap{Status: StatusUnreachable}}
		if st != StatusUnreachable {
			e.Snap = &biosSnap{Vendor: "HPE", Attrs: map[string]string{"A": "1"}}
		}
		ls := compareLines(e, ign)
		if len(ls) != 1 || lineKey(ls[0]) != "N/A|-|(N/A)|(N/A)|"+st+"|true" {
			t.Errorf("%s: %+v", st, ls)
		}
	}

	// 기준 호스트 자신: 자기 값을 기준값·현재값으로 OK 줄 (비고 REFERENCE, 제외 속성 빼고 이름순)
	e := &allHost{Target: Target{Hostname: "ref"}, Status: StatusIsReference, Snap: &biosSnap{Vendor: "HPE", Model: "DL360", BiosVersion: "U46",
		Attrs: map[string]string{"B": "2", "A": "1", "Turbo": "On", "SystemUuid": "u", "Empty": ""}}}
	ls := compareLines(e, ign)
	wantRef := []string{"OK|A|1|1|REFERENCE|false", "OK|B|2|2|REFERENCE|false", "OK|Empty|(N/A)|(N/A)|REFERENCE|false"}
	if len(ls) != len(wantRef) {
		t.Fatalf("REFERENCE 줄 %d개, 기대 %d개: %+v", len(ls), len(wantRef), ls)
	}
	for i, l := range ls {
		if k := lineKey(l); k != wantRef[i] || l.Host != "ref" || l.Vendor != "HPE" || l.Kind != KindCompare {
			t.Errorf("REFERENCE 줄 %d = %s %+v, 기대 %s", i, k, l, wantRef[i])
		}
	}
}

// allOpts.OnHost 안에서는 대상·기준 속성이 남아 있어 compareLines 를 부를 수 있다 (그 뒤 대상 속성은 버려짐).
func TestAllCheckOnHostCompareLines(t *testing.T) {
	_, ref := startAllSrv(t, "hpe-dl360gen11", nil)
	_, dst := startAllSrv(t, "hpe-dl360gen11", func(s *mockbmc.Server) {
		setAttrs(t, s, map[string]interface{}{"ProcTurbo": "Disabled"})
	})
	fastCheck(t)
	rc := checkRCFor(t, 2, "", dst, ref)
	refs, err := newAllRefs(parseRefList(ref+"\n"), writeHosts(t, ""))
	must(t, err)
	lines := make([][]ResultLine, len(rc.Targets))
	run, err := doAllCheck(rc, allOpts{Refs: refs, Ignore: testIgnore, ResultDir: t.TempDir(), Now: checkNow,
		OnHost: func(i int, h *allHost) { lines[i] = compareLines(h, testIgnore) }})
	if err != nil {
		t.Fatalf("doAllCheck: %v", err)
	}
	if run.Hosts[0].Snap.Attrs != nil {
		t.Error("대상 속성은 OnHost 뒤에 버려져야 함")
	}
	var diff []string
	for _, l := range lines[0] {
		if l.Tag != TagOK {
			diff = append(diff, lineKey(l))
		}
	}
	if len(lines[0]) < 5 || strings.Join(diff, ",") != "difference|ProcTurbo|Enabled|Disabled|DIFF|false" {
		t.Errorf("대상 줄 %d개, 차이 %v", len(lines[0]), diff)
	}
	if len(lines[1]) < 5 {
		t.Errorf("기준 자신은 속성마다 줄: %d개", len(lines[1]))
	}
	for _, l := range lines[1] {
		if l.HostError || l.Tag != TagOK || l.Note != StatusIsReference || l.Expected != l.Current {
			t.Errorf("기준 자신 줄: %+v", l)
		}
	}
}
