package main

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"biostool/internal/mockbmc"
)

// setplan.go 시험 (단계 7). 실제 BMC 는 쓰지 않고 internal/mockbmc(testdata 합성 트리)만 쓴다.

func runUnifiedSet(t *testing.T, rc *runContext) *unifiedRun {
	t.Helper()
	fastCheck(t)
	u, err := runUnified(rc, unifiedOpts{Profile: setProf(t, setProfText), ResultDir: t.TempDir(), HostsPath: writeHosts(t, ""), Now: checkNow})
	if err != nil || u == nil {
		t.Fatalf("runUnified: %v", err)
	}
	return u
}

// planKeys 는 항목을 "host|attr|from|to|출처|남의Pending" 으로 줄입니다.
func planKeys(p *setPlan) []string {
	var out []string
	for _, it := range p.Items {
		out = append(out, fmt.Sprintf("%s|%s|%s|%s|%s|%v", it.Host, it.Attr, it.From, it.To, it.Source, it.PendingHost))
	}
	return out
}

func hasLine(lines []string, subs ...string) bool {
	for _, l := range lines {
		ok := true
		for _, s := range subs {
			ok = ok && strings.Contains(l, s)
		}
		if ok {
			return true
		}
	}
	return false
}

// applyT 는 applySetPlan 을 부르고 report 줄을 모읍니다 (report 가 동시에 불리지 않는지도 본다).
func applyT(t *testing.T, rc *runContext, u *unifiedRun, p *setPlan, incl bool, cancel <-chan struct{}, onReport func(host, msg string)) (*setOutcome, []string) {
	t.Helper()
	var mu sync.Mutex
	busy := false
	var reps []string
	out, err := applySetPlan(rc, u, p, incl, func(host, msg string) {
		mu.Lock()
		if busy {
			t.Error("report 가 동시에 불림")
		}
		busy = true
		reps = append(reps, host+" "+msg)
		mu.Unlock()
		if onReport != nil {
			onReport(host, msg)
		}
		mu.Lock()
		busy = false
		mu.Unlock()
	}, cancel)
	if err != nil || out == nil {
		t.Fatalf("applySetPlan: %v", err)
	}
	return out, reps
}

// patchAttrs 는 PATCH 본문 1건을 검사해 Attributes 를 돌려줍니다 (키는 Attributes 와 ApplyTime=OnReset 뿐).
func patchAttrs(t *testing.T, body string) map[string]interface{} {
	t.Helper()
	var m map[string]json.RawMessage
	must(t, json.Unmarshal([]byte(body), &m))
	var attrs map[string]interface{}
	for k, v := range m {
		switch k {
		case "Attributes":
			must(t, json.Unmarshal(v, &attrs))
		case "@Redfish.SettingsApplyTime":
			if string(v) != `{"ApplyTime":"OnReset"}` {
				t.Errorf("ApplyTime = %s", v)
			}
		default:
			t.Errorf("PATCH 본문에 허용되지 않은 키 %q (%s)", k, body)
		}
	}
	return attrs
}

func itemStatus(t *testing.T, u *unifiedRun, host, attr string) (string, string) {
	t.Helper()
	res := applyResultsDir(t, u.Check.Dir)
	r, ok := res[host+"|"+attr]
	if !ok {
		t.Fatalf("%s %s 결과 행 없음 (%v)", host, attr, res)
	}
	return r.status, r.detail
}

func applyResultsDir(t *testing.T, dir string) map[string]applyRow {
	t.Helper()
	return applyResults(t, &checkRun{Dir: dir})
}

// 프로파일만: verified=Y FAIL 만 항목, UNVERIFIED 는 제외 줄, 계획은 BMC 에 접속하지 않음.
func TestSetPlanProfileOnly(t *testing.T) {
	d, ud := startAllSrv(t, "dell-r660", func(s *mockbmc.Server) {
		setAttrs(t, s, map[string]interface{}{"LogicalProc": "Disabled", "SubNumaCluster": "Enabled"})
	})
	h, _ := startAllSrv(t, "hpe-dl360gen11", nil)
	rc := checkRCFor(t, 2, "", ud, strings.TrimPrefix(h.URL(), "https://"))
	u := runUnifiedSet(t, rc)
	before := callCounts(d, h)
	p := buildSetPlan(rc.Conf, u, nil)
	if fmt.Sprint(callCounts(d, h)) != fmt.Sprint(before) {
		t.Error("buildSetPlan 이 BMC 에 접속함")
	}
	if got := planKeys(p); fmt.Sprint(got) != fmt.Sprint([]string{ud + "|LogicalProc|Disabled|Enabled|profile|false"}) {
		t.Errorf("항목 = %v", got)
	}
	if !hasLine(p.Skipped, ud, "SubNumaCluster", "UNVERIFIED") || hasLine(p.Skipped, "allcheck_set") {
		t.Errorf("제외 = %v", p.Skipped)
	}
	pre := p.Preview()
	if !hasLine(pre, ud, "LogicalProc  Disabled → Enabled  (프로파일)") || !hasLine(pre, "호스트 1대, 1건") || len(p.PendingLines()) != 0 || p.Empty() {
		t.Errorf("미리보기 =\n%s", strings.Join(pre, "\n"))
	}
	for _, s := range []*mockbmc.Server{d, h} {
		assertClean(t, s)
	}
}

// 전체비교: allcheck_set 관문, 같은 목표값은 하나(프로파일), 목표값이 다르면 [충돌](둘 다 안 함), 위험 속성·제외 목록·
// 속성 목록이 다른 호스트는 빠짐.
func TestSetPlanCompareGateConflictDedupe(t *testing.T) {
	a, ua := startAllSrv(t, "hpe-dl360gen11", func(s *mockbmc.Server) {
		setAttrs(t, s, map[string]interface{}{"LlcPrefetch": "Disabled", "SubNumaClustering": "Auto",
			"AdminPassword": "x", "ExclMe": "1", "MyUuidX": "u1"})
	})
	b, ub := startAllSrv(t, "hpe-dl360gen11", func(s *mockbmc.Server) {
		setAttrs(t, s, map[string]interface{}{"ProcHyperthreading": "Disabled", "ProcTurbo": "Disabled", "SubNumaClustering": "Enabled",
			"AdminPassword": "y", "ExclMe": "2", "MyUuidX": "u2"})
	})
	c, uc := startAllSrv(t, "hpe-dl360gen11", func(s *mockbmc.Server) {
		setAttrs(t, s, map[string]interface{}{"ProcTurbo": "Disabled", "NewAttrX1": "v"})
	})
	rc := checkRCFor(t, 3, "", ua, ub, uc)
	rc.Conf.ExcludeAttrs = []string{"exclme"}
	u := runUnifiedSet(t, rc)
	cmp := compareT(t, u, compareRef{Hosts: []string{ua}})
	if !cmpHostOf(t, cmp, ub).sameAttrNames() || cmpHostOf(t, cmp, uc).sameAttrNames() {
		t.Fatal("시험 준비: b 는 속성 목록이 같고 c 는 달라야 함")
	}
	before := callCounts(a, b, c)

	// allcheck_set=off: 프로파일 항목만 + 안내 줄 1개
	p := buildSetPlan(rc.Conf, u, cmp)
	want := []string{
		ua + "|LlcPrefetch|Disabled|Enabled|profile|false", ua + "|SubNumaClustering|Auto|Disabled|profile|false",
		ub + "|ProcHyperthreading|Disabled|Enabled|profile|false", ub + "|SubNumaClustering|Enabled|Disabled|profile|false",
	}
	if got := planKeys(p); fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("allcheck_set=off 항목 =\n%v\n기대\n%v", got, want)
	}
	if !hasLine(p.Skipped, "allcheck_set=off") || len(p.Conflicts) != 0 {
		t.Errorf("allcheck_set=off 제외 %v 충돌 %v", p.Skipped, p.Conflicts)
	}

	// allcheck_set=on
	rc.Conf.AllcheckSet = true
	p = buildSetPlan(rc.Conf, u, cmp)
	want = []string{
		ua + "|LlcPrefetch|Disabled|Enabled|profile|false", ua + "|SubNumaClustering|Auto|Disabled|profile|false",
		ub + "|ProcHyperthreading|Disabled|Enabled|profile|false", // 비교서버도 Enabled → 하나만 (출처 프로파일)
		ub + "|ProcTurbo|Disabled|Enabled|compare|false",
	}
	if got := planKeys(p); fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("allcheck_set=on 항목 =\n%v\n기대\n%v", got, want)
	}
	wantC := []setConflict{
		{ub, "LlcPrefetch", "Enabled", "Disabled"},    // 프로파일 OK 인 속성을 비교서버가 다른 값으로
		{ub, "SubNumaClustering", "Disabled", "Auto"}, // 둘 다 바꾸려는데 목표값이 다름 → 프로파일 항목도 뺌
	}
	if fmt.Sprint(p.Conflicts) != fmt.Sprint(wantC) {
		t.Errorf("충돌 = %v", p.Conflicts)
	}
	if !hasLine(p.Skipped, ub, "AdminPassword", "위험 속성") || !hasLine(p.Skipped, uc, "속성 목록이 비교서버와 달라 설정 제외") || hasLine(p.Skipped, "allcheck_set") {
		t.Errorf("제외 = %v", p.Skipped)
	}
	pre := strings.Join(p.Preview(), "\n")
	for _, w := range []string{
		ub + "  ProcTurbo  Disabled → Enabled  (비교서버)", "[충돌] " + ub + " LlcPrefetch 프로파일=Enabled 비교서버=Disabled",
		"[충돌] " + ub + " SubNumaClustering 프로파일=Disabled 비교서버=Auto", "제외:",
	} {
		if !strings.Contains(pre, w) {
			t.Errorf("미리보기에 %q 없음:\n%s", w, pre)
		}
	}
	for _, no := range []string{"ExclMe", "MyUuidX", "NewAttrX1", uc + "  ProcTurbo"} {
		if strings.Contains(pre, no) {
			t.Errorf("미리보기에 %q 가 있음 (제외 목록·속성 목록 불일치):\n%s", no, pre)
		}
	}
	if fmt.Sprint(callCounts(a, b, c)) != fmt.Sprint(before) {
		t.Error("buildSetPlan 이 BMC 에 접속함")
	}
	for _, s := range []*mockbmc.Server{a, b, c} {
		assertClean(t, s)
	}
}

// 남의 Pending: 목록·포함 여부(false 면 건너뜀, true 면 그 목록과 같은 Pending 만 허용, 새로 생긴 Pending 은 거부).
func TestSetPlanPendingIncludeExclude(t *testing.T) {
	b, ub := startAllSrv(t, "hpe-dl360gen11", func(s *mockbmc.Server) {
		setAttrs(t, s, map[string]interface{}{"ProcHyperthreading": "Disabled"})
	})
	setPending(t, b, hpeSettings, map[string]interface{}{"ProcTurbo": "Disabled"})
	c, uc := startAllSrv(t, "hpe-dl360gen11", func(s *mockbmc.Server) {
		setAttrs(t, s, map[string]interface{}{"ProcHyperthreading": "Disabled"})
	})
	setPending(t, c, hpeSettings, map[string]interface{}{"Sriov": "Disabled"})
	d, ud := startAllSrv(t, "hpe-dl360gen11", func(s *mockbmc.Server) {
		setAttrs(t, s, map[string]interface{}{"ProcHyperthreading": "Disabled"})
	})
	rc := checkRCFor(t, 3, "", ub, uc, ud)
	u := runUnifiedSet(t, rc)
	if it := itemOf(t, u.Check.Hosts[0], "hyper_threading"); it.Result != StatusPendingExists {
		t.Fatalf("시험 준비: b = %s, 기대 PENDING_EXISTS", it.Result)
	}
	p := buildSetPlan(rc.Conf, u, nil)
	want := []string{
		ub + "|ProcHyperthreading|Disabled|Enabled|profile|true", uc + "|ProcHyperthreading|Disabled|Enabled|profile|true",
		ud + "|ProcHyperthreading|Disabled|Enabled|profile|false",
	}
	if got := planKeys(p); fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("항목 = %v", got)
	}
	wantP := []string{ub + "  ProcTurbo  Pending=Disabled", uc + "  Sriov  Pending=Disabled"}
	if got := p.PendingLines(); fmt.Sprint(got) != fmt.Sprint(wantP) {
		t.Errorf("PendingLines = %q", got)
	}
	if pre := strings.Join(p.Preview(), "\n"); !strings.Contains(pre, "남의 Pending 이 있는 호스트 2대 포함") || !strings.Contains(pre, "  "+ub+"  ProcTurbo  Pending=Disabled") {
		t.Errorf("미리보기 =\n%s", pre)
	}

	// 포함하지 않음: b·c 는 접속도 쓰기도 없음, d 만 PATCH 1
	bc, cc := b.CallCount(), c.CallCount()
	out, reps := applyT(t, rc, u, p, false, nil, nil)
	if b.CallCount() != bc || c.CallCount() != cc {
		t.Error("포함하지 않은 호스트에 접속함")
	}
	if pb := patchBodies(d); len(pb) != 1 || fmt.Sprint(patchAttrs(t, pb[0])) != "map[ProcHyperthreading:Enabled]" {
		t.Errorf("d PATCH = %v", pb)
	}
	if out.OK != 1 || out.Skipped != 2 || out.Fail != 0 {
		t.Errorf("결과 OK %d 제외 %d 실패 %d\n%s", out.OK, out.Skipped, out.Fail, strings.Join(out.Lines, "\n"))
	}
	if st, _ := itemStatus(t, u, ub, "ProcHyperthreading"); st != StatusPendingExists {
		t.Errorf("b = %s", st)
	}
	if !hasLine(reps, ub, "[제외]") || !hasLine(reps, ud, "설정 시작") || !hasLine(reps, ud, "[완료]") {
		t.Errorf("report = %v", reps)
	}

	// 포함: c 에 계획 뒤 새 Pending 이 생김 → c 는 거부, b 는 목록과 같은 Pending 만 있어 PATCH 1 (우리 속성만)
	setPending(t, c, hpeSettings, map[string]interface{}{"ProcVirtualization": "Disabled"})
	out, _ = applyT(t, rc, u, p, true, nil, nil)
	if pb := patchBodies(b); len(pb) != 1 || fmt.Sprint(patchAttrs(t, pb[0])) != "map[ProcHyperthreading:Enabled]" {
		t.Errorf("b PATCH = %v", pb)
	}
	if n := c.Count("PATCH", "/"); n != 0 || c.Writes() != 0 {
		t.Errorf("새 Pending 이 생긴 c 에 쓰기 %d", c.Writes())
	}
	if n := d.Count("PATCH", "/"); n != 1 {
		t.Errorf("d PATCH %d회, 기대 1 (이미 Pending=목표값)", n)
	}
	if st, _ := itemStatus(t, u, ub, "ProcHyperthreading"); st != StatusApplied {
		t.Errorf("b = %s", st)
	}
	if st, det := itemStatus(t, u, uc, "ProcHyperthreading"); st != StatusPendingExists || !strings.Contains(det, "ProcVirtualization") {
		t.Errorf("c = %s (%s)", st, det)
	}
	if st, _ := itemStatus(t, u, ud, "ProcHyperthreading"); st != StatusAlreadyOK {
		t.Errorf("d = %s", st)
	}
	if out.OK != 2 || out.Fail != 1 {
		t.Errorf("결과 OK %d 실패 %d\n%s", out.OK, out.Fail, strings.Join(out.Lines, "\n"))
	}
	for _, s := range []*mockbmc.Server{b, c, d} {
		assertSetSafe(t, s)
	}
}

// 적용: 호스트당 PATCH 1회에 계획한 속성만 (프로파일 + 비교서버, ReadOnly 는 빠짐), Dell Job, Pending 재조회 결과.
func TestApplySetPlanBodiesAndVerify(t *testing.T) {
	a, ua := startAllSrv(t, "hpe-dl360gen11", nil)
	b, ub := startAllSrv(t, "hpe-dl360gen11", func(s *mockbmc.Server) {
		setAttrs(t, s, map[string]interface{}{"ProcHyperthreading": "Disabled", "ProcTurbo": "Disabled", "SecureBootStatus": "Enabled"})
	})
	d, ud := startAllSrv(t, "dell-r660", func(s *mockbmc.Server) {
		setAttrs(t, s, map[string]interface{}{"LogicalProc": "Disabled"})
	})
	rc := checkRCFor(t, 3, "", ua, ub, ud)
	rc.Conf.AllcheckSet = true
	u := runUnifiedSet(t, rc)
	p := buildSetPlan(rc.Conf, u, compareT(t, u, compareRef{Hosts: []string{ua}}))
	want := []string{
		ub + "|ProcHyperthreading|Disabled|Enabled|profile|false", ub + "|ProcTurbo|Disabled|Enabled|compare|false",
		ub + "|SecureBootStatus|Enabled|Disabled|compare|false", ud + "|LogicalProc|Disabled|Enabled|profile|false",
	}
	if got := planKeys(p); fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("항목 = %v", got)
	}
	out, reps := applyT(t, rc, u, p, false, nil, nil)

	pb := patchBodies(b)
	if len(pb) != 1 {
		t.Fatalf("b PATCH %d회, 기대 1", len(pb))
	}
	if got := fmt.Sprint(patchAttrs(t, pb[0])); got != "map[ProcHyperthreading:Enabled ProcTurbo:Enabled]" {
		t.Errorf("b PATCH 속성 = %s (ReadOnly SecureBootStatus 는 빠져야 함)", got)
	}
	if pd := patchBodies(d); len(pd) != 1 || fmt.Sprint(patchAttrs(t, pd[0])) != "map[LogicalProc:Enabled]" {
		t.Errorf("Dell PATCH = %v", pd)
	}
	if jb := jobBodies(d); len(jb) != 1 || jb[0] != `{"TargetSettingsURI":"`+dellSettings+`"}` {
		t.Errorf("Dell Job 본문 = %q", jb)
	}
	assertClean(t, a)
	for _, s := range []*mockbmc.Server{a, b, d} {
		assertSetSafe(t, s)
	}
	for _, c := range []struct{ host, attr, st string }{
		{ub, "ProcHyperthreading", StatusApplied}, {ub, "ProcTurbo", StatusApplied},
		{ub, "SecureBootStatus", StatusSkippedReadOnly}, {ud, "LogicalProc", StatusApplied},
	} {
		if st, det := itemStatus(t, u, c.host, c.attr); st != c.st {
			t.Errorf("%s %s = %s (%s), 기대 %s", c.host, c.attr, st, det, c.st)
		}
	}
	if ph := b.Pending()[pendKeyHPE]; ph["ProcHyperthreading"] != "Enabled" || ph["ProcTurbo"] != "Enabled" || len(ph) != 2 {
		t.Errorf("HPE pending = %v", ph)
	}
	if out.OK != 1 || out.Fail != 1 {
		t.Errorf("결과 OK %d 실패 %d", out.OK, out.Fail)
	}
	lines := strings.Join(out.Lines, "\n")
	for _, w := range []string{
		ud + "  [완료] Pending 목표값 확인 1/1건", ub + "  [미반영] 목표값 확인 2/3건", "ProcTurbo → Enabled  APPLIED (PATCH",
		"재조회 Pending=Enabled 확인", "SecureBootStatus → Disabled  SKIPPED_READONLY", "재부팅은 하지 않았습니다",
	} {
		if !strings.Contains(lines, w) {
			t.Errorf("결과 줄에 %q 없음:\n%s", w, lines)
		}
	}
	for _, h := range []string{ub, ud} {
		if !hasLine(reps, h, "설정 시작") || !hasLine(reps, h, "[") {
			t.Errorf("report 에 %s 시작·끝 줄 없음: %v", h, reps)
		}
	}
	if _, err := os.Stat(filepath.Join(u.Check.Dir, "apply_requests.txt")); err != nil {
		t.Error(err)
	}
	assertNoSecrets(t, lines, u.Check.Dir)
}

// 취소: 시작하지 않은 호스트는 접속하지 않음. 빈 계획·계획 불일치는 BMC 에 아무것도 보내지 않음.
func TestApplySetPlanCancelEmptyMismatch(t *testing.T) {
	h1, u1 := startAllSrv(t, "hpe-dl360gen11", func(s *mockbmc.Server) {
		setAttrs(t, s, map[string]interface{}{"ProcHyperthreading": "Disabled"})
	})
	h2, u2 := startAllSrv(t, "hpe-dl360gen11", func(s *mockbmc.Server) {
		setAttrs(t, s, map[string]interface{}{"ProcHyperthreading": "Disabled"})
	})
	rc := checkRCFor(t, 1, "", u1, u2)
	u := runUnifiedSet(t, rc)
	p := buildSetPlan(rc.Conf, u, nil)
	if len(p.Items) != 2 {
		t.Fatalf("항목 = %v", planKeys(p))
	}

	// 빈 계획 · 계획 불일치: 접속 없음
	before := callCounts(h1, h2)
	if pre := (&setPlan{}).Preview(); pre != nil {
		t.Errorf("빈 계획 미리보기 = %q, 기대 nil", pre)
	}
	if out, err := applySetPlan(rc, u, &setPlan{}, true, nil, nil); err != nil || out == nil || len(out.Lines) != 0 {
		t.Errorf("빈 계획: %+v, %v", out, err)
	}
	bad := &setPlan{Items: append([]setItem(nil), p.Items...)}
	bad.Items[1].Host = "other"
	if _, err := applySetPlan(rc, u, bad, true, nil, nil); err == nil {
		t.Error("계획 불일치를 받아들임")
	}
	if fmt.Sprint(callCounts(h1, h2)) != fmt.Sprint(before) {
		t.Error("빈 계획·계획 불일치에서 BMC 에 접속함")
	}

	// 이미 취소됨: 아무 호스트에도 접속하지 않음
	closed := make(chan struct{})
	close(closed)
	out, reps := applyT(t, rc, u, p, true, closed, nil)
	if fmt.Sprint(callCounts(h1, h2)) != fmt.Sprint(before) || out.Cancelled != 2 || !hasLine(reps, u1, "[취소]") {
		t.Errorf("취소된 적용: 요청 %v→%v, 결과 %+v, report %v", before, callCounts(h1, h2), out, reps)
	}

	// 첫 호스트가 끝난 뒤 취소 (concurrency 1): 두 번째는 시작하지 않음
	cancel := make(chan struct{})
	var once sync.Once
	out, _ = applyT(t, rc, u, p, true, cancel, func(host, msg string) {
		if strings.HasPrefix(msg, "[") {
			once.Do(func() { close(cancel) })
		}
	})
	if n := h1.Count("PATCH", "/"); n != 1 {
		t.Errorf("h1 PATCH %d회, 기대 1", n)
	}
	if h2.CallCount() != before[1] {
		t.Error("취소 뒤 h2 에 접속함")
	}
	if st, _ := itemStatus(t, u, u2, "ProcHyperthreading"); st != StatusCanceled || out.OK != 1 || out.Cancelled != 1 {
		t.Errorf("h2 = %s, 결과 %+v", st, out)
	}
	assertClean(t, h2)
	assertSetSafe(t, h1)
}

// applySetPlan(BMC 쓰기)은 TUI 흐름(tui_flow.go)에서만 부른다: 비대화형 bios -auto/-auto-check·check·allcheck 경로는
// 참조하지 않는다 (실행 시 쓰기 0 은 TestBiosAutoCheckWritesCheckRes 가 확인).
func TestApplySetPlanOnlyFromTUI(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil || len(files) == 0 {
		t.Fatal(err)
	}
	allowed := map[string]bool{"setplan.go": true, "tui_flow.go": true}
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") || allowed[f] {
			continue
		}
		fset := token.NewFileSet()
		af, err := parser.ParseFile(fset, f, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(af, func(n ast.Node) bool {
			if id, ok := n.(*ast.Ident); ok && id.Name == "applySetPlan" {
				t.Errorf("%s:%d applySetPlan 참조 (설정은 TUI 흐름에서만)", f, fset.Position(id.Pos()).Line)
			}
			return true
		})
	}
}
