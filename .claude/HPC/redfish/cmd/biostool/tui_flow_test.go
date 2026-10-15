package main

// tui_flow.go 시험: 가짜 TUI(tuiOpen + io.Pipe 키) + mock BMC. 흐름이 키를 기다리기 직전(biosFlow.step)에 그 단계의 키를
// 넣으므로 진행(Progress) 중에는 키가 쌓이지 않는다. 설정 계획·적용은 flowBuildPlan/flowApplyPlan 을 바꿔 끼운다.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"biostool/internal/mockbmc"
)

type flowStep struct{ name, keys string }

type flowRig struct {
	f      *biosFlow
	r      *tuiRig
	steps  []string
	out    bytes.Buffer
	errOut bytes.Buffer
}

// screen 은 TUI 에 그린 전체 출력 (ANSI 제거).
func (fr *flowRig) screen() string { return tuiStripANSI(fr.r.out.String()) }

// runFlowT 는 가짜 TUI 로 흐름을 끝까지 돌립니다. 단계마다 script 의 다음 키를 넣고(이름이 다르면 실패 후 입력 끝),
// optional 의 단계는 순서와 상관없이 그 키를 넣습니다 (단계 7 구현에 따라 나올 수도 있는 화면). initial 은 시작 전에 넣는 키.
func runFlowT(t *testing.T, a biosTUIArgs, initial string, script []flowStep, optional map[string]string) (*flowRig, error) {
	t.Helper()
	pr, pw := io.Pipe()
	t.Cleanup(func() { pw.Close() })
	u, r := newTUIRigReader(t, pr, nil)
	fr := &flowRig{r: r}
	next := 0
	f := &biosFlow{ui: u, a: a}
	f.step = func(name string) {
		if k, ok := optional[name]; ok {
			pw.Write([]byte(k))
			return
		}
		fr.steps = append(fr.steps, name)
		if next >= len(script) || script[next].name != name {
			t.Errorf("예상 밖 단계 %q (지나온 단계 %v)", name, fr.steps)
			pw.Close() // 입력 끝 → 흐름이 끝난다
			return
		}
		pw.Write([]byte(script[next].keys))
		next++
	}
	if initial != "" {
		go pw.Write([]byte(initial))
	}
	err := f.runAll(&fr.out, &fr.errOut)
	if next < len(script) {
		t.Errorf("지나가지 않은 단계 %v (지나온 단계 %v)", script[next:], fr.steps)
	}
	fr.f = f
	return fr, err
}

type applyFn = func(*runContext, *unifiedRun, *setPlan, bool, func(string, string), <-chan struct{}) (*setOutcome, error)

// swapFlowPlan 은 설정 계획·적용을 바꿔 끼웁니다. apply 가 nil 이면 불리면 실패.
func swapFlowPlan(t *testing.T, build func(*Config, *unifiedRun, *compareResult) *setPlan, apply applyFn) {
	t.Helper()
	ob, oa := flowBuildPlan, flowApplyPlan
	t.Cleanup(func() { flowBuildPlan, flowApplyPlan = ob, oa })
	flowBuildPlan = build
	flowApplyPlan = apply
	if apply == nil {
		flowApplyPlan = func(*runContext, *unifiedRun, *setPlan, bool, func(string, string), <-chan struct{}) (*setOutcome, error) {
			t.Error("설정하면 안 되는데 applySetPlan 이 불림")
			return &setOutcome{}, nil
		}
	}
}

// flowEnv 는 conf(refs_dir 은 빈 임시 폴더 또는 refs) 와 기본 commonOpts 입니다.
func flowEnv(t *testing.T, refs string) (*rtEnv, *commonOpts) {
	t.Helper()
	e := newRTEnv(t, 3, "")
	if refs == "" {
		refs = t.TempDir()
	}
	conf, err := os.ReadFile(e.conf)
	must(t, err)
	must(t, os.WriteFile(e.conf, append(conf, []byte("refs_dir="+refs+"\n")...), 0o600))
	return e, &commonOpts{conf: e.conf, hosts: e.hosts, user: "user.txt"}
}

func flowNames(s []flowStep) []string {
	out := make([]string, len(s))
	for i, x := range s {
		out[i] = x.name
	}
	return out
}

// 붙여넣기 대상 → 프로파일 고르기 → 1차 화면 q → 전체비교 y (비교서버 hostname) → q → 수정 n.
func TestBiosFlowHappyPathPasteCompareNoSet(t *testing.T) {
	a, ua := startAllSrv(t, "hpe-dl360gen11", nil)
	b, ub := startAllSrv(t, "hpe-dl360gen11", func(s *mockbmc.Server) {
		setAttrs(t, s, map[string]interface{}{"ProcHyperthreading": "Disabled"})
	})
	_, o := flowEnv(t, "")
	var gotCmp *compareResult
	swapFlowPlan(t, func(cfg *Config, u *unifiedRun, cmp *compareResult) *setPlan {
		gotCmp = cmp
		return &setPlan{Items: []setItem{{Host: ub, Attr: "ProcHyperthreading", From: "Disabled", To: "Enabled", Source: SetSrcProfile}}}
	}, nil)
	script := []flowStep{
		{"targets", "2"},
		{"targets-paste", ua + "\n" + ub + "\x04"},
		{"profile", "2"}, // OTHER, VM
		{"profile-result", "q"},
		{"ask-compare", "y"},
		{"ref", "1"},
		{"ref-host", ua + "\r"},
		{"compare-result", "q"},
		{"ask-set", "n"},
	}
	fr, err := runFlowT(t, biosTUIArgs{O: o}, "", script, nil)
	if err != nil {
		t.Fatalf("흐름 오류: %v\n%s", err, fr.out.String())
	}
	if fmt.Sprint(fr.steps) != fmt.Sprint(flowNames(script)) {
		t.Errorf("단계 = %v", fr.steps)
	}
	out := fr.out.String()
	for _, want := range []string{
		"프로파일 VM, 대상 2대 점검 — OK 1 · difference 1 · N/A 0 · 오류 0",
		"전체비교: 기준 비교서버 " + ua + " — 차이 있는 호스트 1대",
		"설정: 설정하지 않았습니다",
		"결과 폴더: ", "profile_long.csv", "compare_long.csv",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("요약에 %q 없음:\n%s", want, out)
		}
	}
	if fr.errOut.Len() != 0 {
		t.Errorf("경고 출력 = %q", fr.errOut.String())
	}
	sc := fr.screen()
	for _, want := range []string{"[진행] [2/4 접속·조회", "BIOS 점검 결과 — 프로파일 VM", "전체비교 — 기준 비교서버 " + ua,
		"전체비교 결과를 볼까요? [y/n]", "수정 미리보기", "수정할까요? [y/n]"} {
		if !strings.Contains(sc, want) {
			t.Errorf("화면에 %q 없음", want)
		}
	}
	if gotCmp == nil || gotCmp.Label != "비교서버 "+ua {
		t.Errorf("설정 계획에 전체비교 결과가 넘어가야 함: %+v", gotCmp)
	}
	if fr.r.restored != 1 || fr.r.closed != 1 {
		t.Errorf("TUI 복원 restored=%d closed=%d", fr.r.restored, fr.r.closed)
	}
	for _, s := range []*mockbmc.Server{a, b} { // 비교서버 a 는 대상 스냅샷을 재사용 → 세션 1회
		assertOneSession(t, s)
		assertClean(t, s)
	}
	for _, f := range []string{"profile_long.csv", "compare_long.csv"} {
		if _, err := os.Stat(filepath.Join(fr.f.u.Check.Dir, f)); err != nil {
			t.Errorf("%s 없음", f)
		}
	}
}

// 대상 파일 지정(기본값 지우고 입력) → refs JSON 기준 (읽을 수 없는 JSON 은 메뉴에서 뺌) → 설정할 항목 없음.
func TestBiosFlowFileTargetRefJSON(t *testing.T) {
	a, ua := startAllSrv(t, "hpe-dl360gen11", nil)
	_, ub := startAllSrv(t, "hpe-dl360gen11", func(s *mockbmc.Server) {
		setAttrs(t, s, map[string]interface{}{"ProcTurbo": "Disabled"})
	})
	refs := t.TempDir()
	var bios struct{ Attributes map[string]interface{} }
	c, _ := mockClient(a, ModeReadOnly, 0)
	must(t, c.GetJSON("/redfish/v1/systems/1/bios", &bios))
	_ = c.Close()
	jb, err := json.Marshal(map[string]interface{}{"host": "golden", "vendor": "HPE", "model": "ProLiant DL360 Gen11", "bios_version": "x", "attributes": bios.Attributes})
	must(t, err)
	must(t, os.WriteFile(filepath.Join(refs, "golden.json"), jb, 0o600))
	must(t, os.WriteFile(filepath.Join(refs, "bad.json"), []byte(`{"attributes":{}}`), 0o600))
	e, o := flowEnv(t, refs)
	targets := e.writeFile("targets.txt", ua, ub)

	var gotCmp *compareResult
	swapFlowPlan(t, func(cfg *Config, u *unifiedRun, cmp *compareResult) *setPlan {
		gotCmp = cmp
		return &setPlan{}
	}, nil)
	script := []flowStep{
		{"targets", "1"},
		{"targets-file", strings.Repeat("\x7f", len("user.txt")) + targets + "\r"},
		{"profile", "2"},
		{"profile-result", "q"},
		{"ask-compare", "y"},
		{"ref", "2"}, // 1 hostname, 2 golden.json, 3 diff.txt
		{"compare-result", "q"},
	}
	fr, err := runFlowT(t, biosTUIArgs{O: o}, "", script, map[string]string{"preview-empty": "q"})
	if err != nil {
		t.Fatalf("흐름 오류: %v", err)
	}
	if o.user != targets {
		t.Errorf("대상 파일 = %q", o.user)
	}
	sc := fr.screen()
	if !strings.Contains(sc, "golden.json (HPE ProLiant DL360 Gen11 x)") || strings.Contains(sc, "bad.json") || !strings.Contains(sc, "diff.txt (모델별 기준)") {
		t.Errorf("기준 메뉴:\n%s", tuiLastFrame(fr.r))
	}
	out := fr.out.String()
	if !strings.Contains(out, "전체비교: 기준 refs JSON golden.json — 차이 있는 호스트 1대") || !strings.Contains(out, "설정: 설정할 항목이 없습니다") {
		t.Errorf("요약:\n%s", out)
	}
	if gotCmp == nil || !strings.Contains(gotCmp.Label, "golden.json") {
		t.Errorf("전체비교 결과 = %+v", gotCmp)
	}
}

// -user·-profile 지정 → 대상·프로파일을 묻지 않음, 1차 화면 e → 전체비교 질문 없이 수정 단계, Pending 질문 y/n/Ctrl+X.
func TestBiosFlowEditPendingBranches(t *testing.T) {
	for _, c := range []struct {
		key     string
		applied bool
		include bool
		note    string
	}{
		{"y", true, true, "설정: Pending 확인 1대 · 실패·미반영 0대 (" + flowNoReboot + ")"},
		{"n", true, false, "설정: Pending 확인 1대 · 실패·미반영 0대 (" + flowNoReboot + ")"},
		{"\x18", false, false, "설정: 설정을 취소했습니다"},
	} {
		t.Run(fmt.Sprintf("%q", c.key), func(t *testing.T) {
			s, ua := startAllSrv(t, "hpe-dl360gen11", nil)
			e, o := flowEnv(t, "")
			o.user, o.profile = e.writeFile("targets.txt", ua), "VM"
			var built bool
			var gotCmp *compareResult
			applied, include := false, false
			swapFlowPlan(t, func(cfg *Config, u *unifiedRun, cmp *compareResult) *setPlan {
				built, gotCmp = true, cmp
				return &setPlan{
					Items:   []setItem{{Host: ua, Attr: "ProcTurbo", From: "Enabled", To: "Disabled", Source: SetSrcProfile}},
					Pending: []pendingInfo{{Host: ua, Attr: "ProcTurbo", Pending: "Enabled"}},
				}
			}, func(rc *runContext, u *unifiedRun, p *setPlan, inc bool, report func(string, string), cancel <-chan struct{}) (*setOutcome, error) {
				applied, include = true, inc
				report(ua, "Pending 설정")
				return &setOutcome{Lines: []string{ua + ": 설정 확인 OK"}, OK: 1}, nil
			})
			script := []flowStep{{"profile-result", "e"}, {"ask-set", "y"}, {"ask-pending", c.key}}
			if c.applied {
				script = append(script, flowStep{"set-result", "q"})
			}
			fr, err := runFlowT(t, biosTUIArgs{O: o, UserSet: true}, "", script, nil)
			if err != nil {
				t.Fatalf("흐름 오류: %v", err)
			}
			if !built || gotCmp != nil {
				t.Errorf("e 는 전체비교 없이 계획: built=%v cmp=%v", built, gotCmp)
			}
			if applied != c.applied || include != c.include {
				t.Errorf("적용=%v 포함=%v, 기대 %v %v", applied, include, c.applied, c.include)
			}
			sc := fr.screen()
			if !strings.Contains(sc, "Pending 이 있는 장비도 포함할까요?") {
				t.Error("Pending 질문이 그려져야 함")
			}
			if c.applied && (!strings.Contains(sc, "설정 결과") || !strings.Contains(sc, ua+": 설정 확인 OK") || !strings.Contains(sc, flowNoReboot)) {
				t.Errorf("설정 결과 화면:\n%s", tuiLastFrame(fr.r))
			}
			if out := fr.out.String(); !strings.Contains(out, c.note) {
				t.Errorf("요약:\n%s\n기대 %q", out, c.note)
			}
			assertClean(t, s) // 적용은 가짜 → 실제 쓰기 없음
		})
	}
}

// 접속·조회 중 Ctrl+X: 아직 시작하지 않은 호스트는 접속하지 않고 CANCELED (retry.txt), 결과 화면·요약은 그대로.
func TestBiosFlowCancelDuringCollect(t *testing.T) {
	slow := startMockTree(t, "hpe-dl360gen11", func(o *mockbmc.Options) {
		o.Fail = map[string]mockbmc.Fault{"POST /redfish/v1/SessionService/Sessions": {Delay: 400 * time.Millisecond}}
	})
	ua := strings.TrimPrefix(slow.URL(), "https://")
	b, ub := startAllSrv(t, "hpe-dl360gen11", nil)
	c, uc := startAllSrv(t, "hpe-dl360gen11", nil)
	e, o := flowEnv(t, "")
	o.user, o.profile = e.writeFile("targets.txt", ua, ub, uc), "VM"
	swapFlowPlan(t, func(*Config, *unifiedRun, *compareResult) *setPlan { return &setPlan{} }, nil)
	// Ctrl+X 는 시작 전에 넣는다 — 키를 처음 받는 곳이 [2/4 접속·조회] 진행 화면이다 (첫 호스트는 로그인 400ms 지연)
	fr, err := runFlowT(t, biosTUIArgs{O: o, UserSet: true}, "\x18",
		[]flowStep{{"profile-result", "q"}, {"ask-compare", "n"}}, map[string]string{"preview-empty": "q"})
	if err != nil {
		t.Fatalf("흐름 오류: %v", err)
	}
	hosts := fr.f.u.Check.Hosts
	if hosts[1].Status != StatusCanceled || hosts[2].Status != StatusCanceled {
		t.Errorf("상태 = %s %s %s, 기대 뒤 두 대 CANCELED", hosts[0].Status, hosts[1].Status, hosts[2].Status)
	}
	if b.CallCount() != 0 || c.CallCount() != 0 {
		t.Errorf("취소한 호스트에 접속함: %d %d", b.CallCount(), c.CallCount())
	}
	rt, err := os.ReadFile(filepath.Join(fr.f.u.Check.Dir, "retry.txt"))
	if err != nil || !strings.Contains(string(rt), ub) || !strings.Contains(string(rt), uc) {
		t.Errorf("retry.txt = %q (%v)", rt, err)
	}
	if sc := fr.screen(); !strings.Contains(sc, "취소를 요청했습니다") || !strings.Contains(sc, "조회하지 않은") {
		t.Errorf("취소 안내가 그려져야 함:\n%s", tuiLastFrame(fr.r))
	}
	if out := fr.out.String(); !strings.Contains(out, "취소: ") || !strings.Contains(out, StatusCanceled) || !strings.Contains(out, "N/A") {
		t.Errorf("요약:\n%s", out)
	}
}

// 전체비교의 비교서버 조회 중 Ctrl+X: 아직 시작하지 않은 비교서버는 접속하지 않고(요청 0), 그 기준이 필요한 대상은
// REF_CANCELED(N/A, 설정 제외), 조회를 마친 기준과는 그대로 비교한다 (부분 결과·CSV·요약 안내).
func TestBiosFlowCancelDuringRefFetch(t *testing.T) {
	// 비교서버 3대 (대상 목록에 없음): 첫 대는 로그인 400ms 지연 → 그동안 Ctrl+X 가 처리된다 (웜업이라 한 대씩)
	slowRef := startMockTree(t, "hpe-dl360gen11", func(o *mockbmc.Options) {
		o.Fail = map[string]mockbmc.Fault{"POST /redfish/v1/SessionService/Sessions": {Delay: 400 * time.Millisecond}}
	})
	r1 := strings.TrimPrefix(slowRef.URL(), "https://")
	ref2, r2 := startAllSrv(t, "dell-r660", nil)
	ref3, r3 := startAllSrv(t, "lenovo-sr650v3", nil)
	_, u1 := startAllSrv(t, "hpe-dl360gen11", func(s *mockbmc.Server) {
		setAttrs(t, s, map[string]interface{}{"ProcTurbo": "Disabled"})
	})
	_, u2 := startAllSrv(t, "dell-r660", nil)
	_, u3 := startAllSrv(t, "lenovo-sr650v3", nil)
	e, o := flowEnv(t, "")
	o.user, o.profile = e.writeFile("targets.txt", u1, u2, u3), "VM"
	var plan *setPlan
	var gotCmp *compareResult
	swapFlowPlan(t, func(cfg *Config, u *unifiedRun, cmp *compareResult) *setPlan {
		gotCmp = cmp
		plan = buildSetPlan(&Config{AllcheckSet: true}, u, cmp) // 실제 계획 규칙으로 설정 제외를 확인
		return &setPlan{}
	}, nil)
	ref := compareRef{Hosts: []string{r1 + "," + r2 + "," + r3}}
	fr, err := runFlowT(t, biosTUIArgs{O: o, UserSet: true, Ref: ref},
		"", []flowStep{{"profile-result", "q"}, {"ask-compare", "y\x18"}, {"compare-result", "q"}}, map[string]string{"preview-empty": "q"})
	if err != nil {
		t.Fatalf("흐름 오류: %v", err)
	}
	if ref2.CallCount() != 0 || ref3.CallCount() != 0 {
		t.Errorf("취소 뒤 시작하지 않은 비교서버에 접속함: %d %d", ref2.CallCount(), ref3.CallCount())
	}
	// 첫 비교서버는 Ctrl+X 가 처리되기 전에 시작했을 수도, 아닐 수도 있다 (시작했으면 끝까지 조회하고 비교함)
	if gotCmp == nil || gotCmp.RefCanceled < 2 {
		t.Fatalf("전체비교 결과 RefCanceled: %+v", gotCmp)
	}
	first := slowRef.CallCount() > 0
	if h := cmpHostOf(t, gotCmp, u1); (first && h.Status != StatusDiff) || (!first && h.Status != StatusRefCanceled) {
		t.Errorf("첫 비교서버 조회=%v, 대상 상태 %s %s", first, h.Status, h.Detail)
	}
	if first != (gotCmp.RefCanceled == 2) {
		t.Errorf("첫 비교서버 조회=%v, RefCanceled=%d", first, gotCmp.RefCanceled)
	}
	for _, in := range []string{u2, u3} {
		h := cmpHostOf(t, gotCmp, in)
		if h.Status != StatusRefCanceled || h.compared() || !strings.Contains(h.Detail, StatusCanceled) {
			t.Errorf("%s: 상태 %s (%s), 기대 %s", in, h.Status, h.Detail, StatusRefCanceled)
		}
		ls := linesOf(gotCmp.Lines, h.Target.Hostname)
		if l, ok := ls["-"]; len(ls) != 1 || !ok || !l.HostError || l.Tag != TagNA || l.Note != StatusRefCanceled {
			t.Errorf("%s: 줄 = %+v", in, ls)
		}
	}
	// 설정 계획: REF_CANCELED 대상은 전체비교 항목이 없고 제외 사유가 남는다. 비교한 대상은 그대로 계획됨.
	if plan == nil {
		t.Fatal("설정 계획이 불리지 않음")
	}
	for _, it := range plan.Items {
		if it.Source == SetSrcCompare && it.Host != u1 {
			t.Errorf("REF_CANCELED 대상에 전체비교 설정 항목: %+v", it)
		}
	}
	if !hasLine(plan.Skipped, "전체비교 대상 아님 ("+StatusRefCanceled+")") {
		t.Errorf("제외 사유 없음: %q", plan.Skipped)
	}
	if first && !hasLine(planKeys(plan), u1, "ProcTurbo") {
		t.Errorf("비교한 대상의 항목은 계획에 남아야 함: %q", planKeys(plan))
	}
	if _, err := os.Stat(filepath.Join(fr.f.u.Check.Dir, "compare_long.csv")); err != nil {
		t.Errorf("취소해도 비교한 만큼은 CSV 저장: %v", err)
	}
	if sc := fr.screen(); !strings.Contains(sc, fmt.Sprintf("취소했습니다 — 비교서버 %d대 조회 안 함", gotCmp.RefCanceled)) {
		t.Errorf("취소 안내가 그려져야 함:\n%s", tuiLastFrame(fr.r))
	}
	if out := fr.out.String(); !strings.Contains(out, fmt.Sprintf("전체비교 취소: 비교서버 %d대 조회 안 함 — 대상 %d대 %s", gotCmp.RefCanceled, gotCmp.RefCanceled, StatusRefCanceled)) {
		t.Errorf("요약:\n%s", out)
	}
}

// CSV 저장 중 취소: 다음 파일부터 쓰지 않는다.
func TestWriteResultCSVsCancel(t *testing.T) {
	dir := t.TempDir()
	done := make(chan struct{})
	close(done)
	paths, err := writeResultCSVsCancel(dir, "x_", []ResultLine{tuiL(TagOK, "HPE", "h1", "iLO 5", "A", "1", "1", "OK")}, done)
	ents, _ := os.ReadDir(dir)
	if err == nil || !strings.Contains(err.Error(), "취소") || len(paths) != 0 || len(ents) != 0 {
		t.Errorf("err=%v paths=%v files=%d", err, paths, len(ents))
	}
}

// 대상 고르기에서 취소 → 접속하지 않고 끝, 요약은 "취소했습니다".
func TestBiosFlowCancelAtTargets(t *testing.T) {
	e, o := flowEnv(t, "")
	before := e.dirs()
	swapFlowPlan(t, func(*Config, *unifiedRun, *compareResult) *setPlan {
		t.Error("계획을 만들면 안 됨")
		return &setPlan{}
	}, nil)
	fr, err := runFlowT(t, biosTUIArgs{O: o}, "", []flowStep{{"targets", "\x18"}}, nil)
	if err != nil || !strings.Contains(fr.out.String(), "취소했습니다 — 점검하지 않았습니다") {
		t.Errorf("err=%v 요약=%q", err, fr.out.String())
	}
	e.noNewDir(before)
}

// runUnified Cancel 이 이미 닫혀 있으면 아무 호스트에도 접속하지 않는다.
func TestUnifiedCancelBeforeStart(t *testing.T) {
	s, ua := startAllSrv(t, "hpe-dl360gen11", nil)
	fastCheck(t)
	rc := checkRCFor(t, 2, "", ua)
	done := make(chan struct{})
	close(done)
	u, err := runUnified(rc, unifiedOpts{Profile: testProf(t), ResultDir: t.TempDir(), Now: checkNow, Cancel: done})
	if err != nil || u == nil {
		t.Fatal(err)
	}
	if st := u.Check.Hosts[0].Status; st != StatusCanceled || s.CallCount() != 0 {
		t.Errorf("상태 %s, 요청 %d", st, s.CallCount())
	}
	if l := u.ProfileLines(); len(l) != 1 || l[0].Tag != TagNA || l[0].Note != StatusCanceled {
		t.Errorf("줄 = %+v", l)
	}
}

// newSinkProgress: 진행 줄은 sink 로만 간다.
func TestSinkProgress(t *testing.T) {
	var got []string
	p := newSinkProgress(4, func(l string) { got = append(got, l) })
	p.Stage(2, "접속·조회", 3)
	p.Add(TagOK)
	p.Add(progTagErr)
	p.Stop()
	if len(got) < 2 || !strings.HasPrefix(got[len(got)-1], "[2/4 접속·조회 2/3 · OK 1 difference 0 N/A 0 오류 1 · 경과 ") {
		t.Errorf("sink = %q", got)
	}
}
