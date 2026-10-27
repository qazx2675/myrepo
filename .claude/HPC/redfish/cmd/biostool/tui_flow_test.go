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
	return runFlowTC(t, a, initial, script, optional, nil)
}

// runFlowTC 는 runFlowT 에 check 를 더한 것입니다: script 의 i 번째 단계에서 키를 넣기 직전에 부른다 (nil 이면 안 부름).
func runFlowTC(t *testing.T, a biosTUIArgs, initial string, script []flowStep, optional map[string]string, check func(i int, f *biosFlow)) (*flowRig, error) {
	t.Helper()
	pr, pw := io.Pipe()
	t.Cleanup(func() { pw.Close() })
	// 단계의 키는 TUI 가 그 화면을 그리고 키를 기다리기 시작할 때(env.Ready) 넣는다 — Ask 가 묻기 전에 쌓인 키를 버려도 남게.
	pending := ""
	u, r := newTUIRigReader(t, pr, func(e *tuiEnv, _ *tuiRig) {
		e.Ready = func() {
			if k := pending; k != "" {
				pending = ""
				pw.Write([]byte(k))
			}
		}
	})
	fr := &flowRig{r: r}
	next := 0
	f := &biosFlow{ui: u, a: a}
	f.step = func(name string) {
		if k, ok := optional[name]; ok {
			pending += k
			return
		}
		fr.steps = append(fr.steps, name)
		if next >= len(script) || script[next].name != name {
			t.Errorf("예상 밖 단계 %q (지나온 단계 %v)", name, fr.steps)
			pw.Close() // 입력 끝 → 흐름이 끝난다
			return
		}
		if check != nil {
			check(next, f)
		}
		pending += script[next].keys
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
	return e, &commonOpts{conf: e.conf, hosts: e.hosts, user: "user.txt", routeXML: true} // cmdBios 처럼 classifyTargets
}

func flowNames(s []flowStep) []string {
	out := make([]string, len(s))
	for i, x := range s {
		out[i] = x.name
	}
	return out
}

// 붙여넣기 대상 → 프로파일 고르기 → 1차 화면 Tab → 전체 설정 비교 (비교서버 hostname) → q → 수정 n.
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
		{"targets-xml-paste", "\x04"}, // IP 대상이라 Cisco(XML) 대상을 더 묻는다 (없음)
		{"profile", "2"},              // OTHER, VM
		{"profile-result", "\t"},
		{"ref", "1"},
		{"ref-host", ua + "\x04"},
		{"compare-result", "e"},
		{"edit-menu", "1"}, // 점검 결과 기준 수정
		{"ask-set", "n"},
		{"compare-result", "qq"}, // 거절하면 결과 보기로 돌아오고, 다시 q 두 번이면 묻지 않고 끝
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
		"화면  >[프로파일 점검] [전체 설정 비교]", "[프로파일 점검] >[전체 설정 비교]", "수정 미리보기", "위 수정대상을 적용할까요? [y/n]"} {
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

// 화면 탭: 첫 Tab 은 기준 선택 + 비교, 다시 Tab 이면 프로파일 화면(필터 유지), 그다음 Tab 은 기준을 묻지 않고 비교 화면,
// r 은 기준을 다시 골라 비교 (CSV 는 덮어써 한 번만 셈). 기준 서버 자신은 자기 값으로 OK·REFERENCE 줄.
func TestBiosFlowViewTabsAndReRef(t *testing.T) {
	_, ua := startAllSrv(t, "hpe-dl360gen11", nil)
	_, ub := startAllSrv(t, "hpe-dl360gen11", func(s *mockbmc.Server) {
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
		{"profile", "2"},
		{"profile-result", "f\t"}, // 3: 프로파일 화면 difference 필터 → Tab
		{"ref", "1"},
		{"ref-host", ua + "\x04"},
		{"compare-result", "o\t"}, // 6
		{"profile-result", "\t"},  // 7: 필터 유지
		{"compare-result", "r"},   // 8: 기준을 묻지 않고 비교 화면 (필터 유지)
		{"ref", "1"},
		{"ref-host", ub + "\x04"},
		{"compare-result", "e"}, // 11
		{"edit-menu", "1"},
		{"ask-set", "n"},
		{"compare-result", "qq"},
	}
	refLines := func(cmp *compareResult, host string) {
		n := 0
		for _, l := range cmp.Lines {
			if l.Host != host {
				continue
			}
			n++
			if l.HostError || l.Tag != TagOK || l.Note != StatusIsReference || l.Expected != l.Current || l.Attr == "-" {
				t.Errorf("기준 서버 줄: %+v", l)
			}
		}
		if n < 5 {
			t.Errorf("기준 서버 %s 줄 %d개 (속성마다 줄이어야 함)", host, n)
		}
	}
	fr, err := runFlowTC(t, biosTUIArgs{O: o}, "", script, map[string]string{"targets-xml-paste": "\x04"}, func(i int, f *biosFlow) {
		ui := f.ui
		switch i {
		case 3:
			if ui.Tab() != 0 || f.cmp != nil {
				t.Errorf("시작은 프로파일 화면, 비교 전: tab=%d", ui.Tab())
			}
		case 6:
			if ui.Tab() != 1 || f.cmp == nil || f.cmp.Label != "비교서버 "+ua || !ui.refKey {
				t.Fatalf("첫 Tab → 비교 화면: tab=%d cmp=%v", ui.Tab(), f.cmp)
			}
			refLines(f.cmp, ua)
		case 7:
			if ui.Tab() != 0 || ui.filter != tuiFilterDiff || !strings.Contains(ui.title, "프로파일 VM") {
				t.Errorf("Tab → 프로파일 화면, 필터 유지: tab=%d filter=%c title=%q", ui.Tab(), ui.filter, ui.title)
			}
		case 8:
			if ui.Tab() != 1 || ui.filter != tuiFilterOK || f.cmp.Label != "비교서버 "+ua {
				t.Errorf("다시 Tab → 비교 화면(다시 비교 안 함), 필터 유지: tab=%d filter=%c", ui.Tab(), ui.filter)
			}
		case 11:
			if ui.Tab() != 1 || ui.filter != tuiFilterAll || f.cmp.Label != "비교서버 "+ub || !ui.refKey {
				t.Errorf("r → 새 기준 결과: tab=%d filter=%c label=%q", ui.Tab(), ui.filter, f.cmp.Label)
			}
			refLines(f.cmp, ub)
		}
	})
	if err != nil {
		t.Fatalf("흐름 오류: %v", err)
	}
	if fmt.Sprint(fr.steps) != fmt.Sprint(flowNames(script)) {
		t.Errorf("단계 = %v", fr.steps)
	}
	if gotCmp == nil || gotCmp.Label != "비교서버 "+ub {
		t.Errorf("설정 계획에는 마지막 전체비교 결과: %+v", gotCmp)
	}
	seen := map[string]bool{}
	for _, p := range fr.f.csvs {
		if seen[p] {
			t.Errorf("CSV 중복: %s", p)
		}
		seen[p] = true
	}
	if out := fr.out.String(); !strings.Contains(out, "전체비교: 기준 비교서버 "+ub+" — 차이 있는 호스트 1대") {
		t.Errorf("요약:\n%s", out)
	}
	if sc := fr.screen(); !strings.Contains(sc, "r 기준 다시 선택") || !strings.Contains(sc, "전체비교 — 기준 비교서버 "+ub) {
		t.Errorf("비교 화면 안내:\n%s", tuiLastFrame(fr.r))
	}
}

// 비교 화면으로 처음 넘어가 기준 고르기를 취소하면 프로파일 화면으로 돌아오고, 다음 Tab 에 다시 묻는다.
func TestBiosFlowViewTabRefCanceled(t *testing.T) {
	_, ua := startAllSrv(t, "hpe-dl360gen11", nil)
	e, o := flowEnv(t, "")
	o.user, o.profile = e.writeFile("targets.txt", ua), "VM"
	swapFlowPlan(t, func(*Config, *unifiedRun, *compareResult) *setPlan { return &setPlan{} }, nil)
	script := []flowStep{
		{"profile-result", "\t"},
		{"ref", "\x1b"},
		{"profile-result", "\t"},
		{"ref", "1"},
		{"ref-host", ua + "\x04"},
		{"compare-result", "qq"},
	}
	fr, err := runFlowT(t, biosTUIArgs{O: o, UserSet: true}, "", script, map[string]string{"preview-empty": "q"})
	if err != nil {
		t.Fatalf("흐름 오류: %v", err)
	}
	if fmt.Sprint(fr.steps) != fmt.Sprint(flowNames(script)) {
		t.Errorf("단계 = %v", fr.steps)
	}
	if !strings.Contains(fr.screen(), "전체 설정 비교 기준을 고르지 않았습니다") || fr.f.cmp == nil {
		t.Errorf("취소 안내·두 번째 비교: cmp=%v\n%s", fr.f.cmp, tuiLastFrame(fr.r))
	}
}

// 비교 화면으로 넘어가며 뜬 기준 고르기(목록·hostname 입력)에서도 Tab 으로 프로파일 화면으로 돌아간다.
// 이미 비교한 뒤 r 로 기준을 다시 고르다가 Tab 을 눌러도 프로파일 화면으로 가고, 앞의 비교 결과는 그대로 둔다.
func TestBiosFlowViewTabOutOfRefChooser(t *testing.T) {
	_, ua := startAllSrv(t, "hpe-dl360gen11", nil)
	e, o := flowEnv(t, "")
	o.user, o.profile = e.writeFile("targets.txt", ua), "VM"
	swapFlowPlan(t, func(*Config, *unifiedRun, *compareResult) *setPlan { return &setPlan{} }, nil)
	script := []flowStep{
		{"profile-result", "\t"},  // 0: 기준 목록이 뜬다
		{"ref", "\t"},             // 1: 목록에서 Tab → 프로파일 화면
		{"profile-result", "\t"},  // 2: 다시 Tab → 목록
		{"ref", "1"},              // 3: 비교서버 hostname 입력
		{"ref-host", "\t"},        // 4: 입력 줄에서 Tab → 프로파일 화면
		{"profile-result", "\t"},  // 5
		{"ref", "1"},              // 6
		{"ref-host", ua + "\x04"}, // 7: 비교 실행
		{"compare-result", "r"},   // 8: 기준 다시 고르기
		{"ref", "\t"},             // 9: 목록에서 Tab → 프로파일 화면 (비교 결과 유지)
		{"profile-result", "\t"},  // 10: Tab → 이미 비교한 화면 (다시 묻지 않음)
		{"compare-result", "qq"},  // 11
	}
	fr, err := runFlowTC(t, biosTUIArgs{O: o, UserSet: true}, "", script, map[string]string{"preview-empty": "q"}, func(i int, f *biosFlow) {
		switch i {
		case 2, 5:
			if f.ui.Tab() != 0 || f.cmp != nil || f.ui.tabOut {
				t.Errorf("step %d: Tab 으로 나오면 프로파일 화면·비교 안 함: tab=%d cmp=%v tabOut=%v", i, f.ui.Tab(), f.cmp, f.ui.tabOut)
			}
		case 10:
			if f.ui.Tab() != 0 || f.cmp == nil || f.cmp.Label != "비교서버 "+ua {
				t.Errorf("r 중 Tab: 프로파일 화면, 앞 비교 유지: tab=%d cmp=%v", f.ui.Tab(), f.cmp)
			}
		case 11:
			if f.ui.Tab() != 1 || f.cmp == nil {
				t.Errorf("다시 Tab → 비교 화면: tab=%d", f.ui.Tab())
			}
		}
	})
	if err != nil {
		t.Fatalf("흐름 오류: %v", err)
	}
	if fmt.Sprint(fr.steps) != fmt.Sprint(flowNames(script)) {
		t.Errorf("단계 = %v", fr.steps)
	}
	if strings.Contains(fr.screen(), "기준을 고르지 않았습니다") {
		t.Errorf("Tab 으로 나온 것은 '고르지 않았습니다' 안내가 없어야 함:\n%s", tuiLastFrame(fr.r))
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
		{"targets-xml-file", "\r"}, // IP 대상이라 Cisco(XML) 대상 파일을 더 묻는다 (비움 = 없음)
		{"profile", "2"},
		{"profile-result", "\t"},
		{"ref", "2"}, // 1 hostname, 2 golden.json, 3 diff.txt
		{"compare-result", "qq"},
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

// -user·-profile 지정 → 대상·프로파일을 묻지 않음, 1차 화면 e → 전체비교 없이 수정 단계, Pending 질문 y/n/Ctrl+X.
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
			script := []flowStep{{"profile-result", "e"}, {"edit-menu", "1"}, {"ask-set", "y"}, {"ask-pending", c.key}}
			if c.applied {
				script = append(script, flowStep{"set-result", "q"}) // 설정 결과 q → 결과 보기로
			}
			script = append(script, flowStep{"profile-result", "qq"}) // 적용·취소 뒤에는 다시 묻지 않고 끝
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
		[]flowStep{{"profile-result", "qq"}}, map[string]string{"preview-empty": "q"})
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
	// -ref 를 줬으므로 수집 직후 바로 비교한다: 그 비교서버 조회 진행 화면에서 Ctrl+X
	fr, err := runFlowT(t, biosTUIArgs{O: o, UserSet: true, Ref: ref},
		"", []flowStep{{"compare-auto", "\x18"}, {"profile-result", "qq"}}, map[string]string{"preview-empty": "q"})
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

// 옵션(-user·-profile·-ref)을 모두 주면 대상·프로파일·기준을 묻지 않고 수집 직후 전체 설정 비교까지 해 둔다
// (첫 Tab 은 기준을 묻지 않고 바로 비교 화면). 설정은 하지 않고 수정 단계의 확인은 그대로 묻는다.
func TestBiosFlowOneShotOptionsAutoCompare(t *testing.T) {
	a, ua := startAllSrv(t, "hpe-dl360gen11", nil)
	b, ub := startAllSrv(t, "hpe-dl360gen11", func(s *mockbmc.Server) {
		setAttrs(t, s, map[string]interface{}{"ProcHyperthreading": "Disabled"})
	})
	e, o := flowEnv(t, "")
	o.user, o.profile = e.writeFile("targets.txt", ua, ub), "VM"
	var gotCmp *compareResult
	swapFlowPlan(t, func(cfg *Config, u *unifiedRun, cmp *compareResult) *setPlan {
		gotCmp = cmp
		return &setPlan{Items: []setItem{{Host: ub, Attr: "ProcHyperthreading", From: "Disabled", To: "Enabled", Source: SetSrcProfile}}}
	}, nil)
	script := []flowStep{
		{"compare-auto", ""},
		{"profile-result", "\t"}, // 1
		{"compare-result", "e"},  // 2
		{"edit-menu", "1"},
		{"ask-set", "n"},
		{"compare-result", "qq"},
	}
	fr, err := runFlowTC(t, biosTUIArgs{O: o, UserSet: true, Ref: compareRef{Hosts: []string{ua}}}, "", script, nil, func(i int, f *biosFlow) {
		switch i {
		case 1:
			if f.ui.Tab() != 0 || f.cmp == nil || f.cmp.Label != "비교서버 "+ua || !strings.Contains(f.ui.title, "프로파일 VM") {
				t.Errorf("Tab 전에 비교가 끝나 있고 첫 화면은 프로파일: tab=%d cmp=%v title=%q", f.ui.Tab(), f.cmp, f.ui.title)
			}
		case 2:
			if f.ui.Tab() != 1 || f.ui.title != "전체비교 — 기준 비교서버 "+ua || !f.ui.refKey {
				t.Errorf("Tab → 기준을 묻지 않고 비교 화면: tab=%d title=%q", f.ui.Tab(), f.ui.title)
			}
		}
	})
	if err != nil {
		t.Fatalf("흐름 오류: %v", err)
	}
	if fmt.Sprint(fr.steps) != fmt.Sprint(flowNames(script)) {
		t.Errorf("단계 = %v (대상·프로파일·기준을 묻지 않아야 함)", fr.steps)
	}
	if gotCmp == nil {
		t.Error("설정 계획에 전체비교 결과가 넘어가야 함")
	}
	out := fr.out.String()
	for _, want := range []string{"전체비교: 기준 비교서버 " + ua + " — 차이 있는 호스트 1대", "설정: 설정하지 않았습니다", "compare_long.csv"} {
		if !strings.Contains(out, want) {
			t.Errorf("요약에 %q 없음:\n%s", want, out)
		}
	}
	for _, s := range []*mockbmc.Server{a, b} {
		assertOneSession(t, s)
		assertClean(t, s)
	}
}

// q 는 한 단계 뒤로: 프로파일 고르기의 q → 대상 입력. 결과 맨 위의 q→Esc·N·Ctrl+X 는 끝내지 않고, e 는 수정 단계,
// 거기서 취소하면 결과 보기로 돌아온다. q 두 번은 수정 여부를 묻지 않고 끝 (TestBiosFlowRefSplitAndQuitWithoutAsking).
func TestBiosFlowQuitBackAndSetLoop(t *testing.T) {
	for _, c := range []struct {
		name   string
		first  string // 결과 화면 첫 키
		builds int    // ask-set 때까지 계획을 만든 횟수
	}{
		{"e", "q\x1bN\x18e", 1},
	} {
		t.Run(c.name, func(t *testing.T) {
			_, ua := startAllSrv(t, "hpe-dl360gen11", nil)
			_, o := flowEnv(t, "")
			builds := 0
			swapFlowPlan(t, func(*Config, *unifiedRun, *compareResult) *setPlan {
				builds++
				return &setPlan{Items: []setItem{{Host: ua, Attr: "ProcTurbo", From: "Enabled", To: "Disabled", Source: SetSrcProfile}}}
			}, nil)
			script := []flowStep{
				{"targets", "2"},
				{"targets-paste", ua + "\x04"},
				{"targets-xml-paste", "\x04"},
				{"profile", "q"}, // 프로파일 고르기 q → 대상 입력으로
				{"targets", "2"},
				{"targets-paste", ua + "\x04"},
				{"targets-xml-paste", "\x04"},
				{"profile", "2"},
				{"profile-result", c.first},
				{"edit-menu", "1"},
				{"ask-set", "\x18"}, // 10: 수정 단계 취소 → 결과 보기로
				{"profile-result", "qq"},
			}
			fr, err := runFlowTC(t, biosTUIArgs{O: o}, "", script, nil, func(i int, f *biosFlow) {
				if i == 10 && builds != c.builds {
					t.Errorf("ask-set 까지 계획 %d번, 기대 %d (e 경로면 q/Esc/N/Ctrl+X 로 끝나지 않았음)", builds, c.builds)
				}
			})
			if err != nil {
				t.Fatalf("흐름 오류: %v", err)
			}
			if fmt.Sprint(fr.steps) != fmt.Sprint(flowNames(script)) {
				t.Errorf("단계 = %v", fr.steps)
			}
			sc := fr.screen()
			for _, want := range []string{"결과 보기로 돌아왔습니다", "q 두 번 종료"} {
				if !strings.Contains(sc, want) {
					t.Errorf("화면에 %q 없음", want)
				}
			}
			if !strings.Contains(sc, tuiQuitConfirm) || !strings.Contains(sc, "Ctrl+X 는 진행 중 작업 취소입니다") {
				t.Errorf("종료 확인·Ctrl+X 안내가 그려져야 함")
			}
			if out := fr.out.String(); !strings.Contains(out, "설정: 설정하지 않았습니다") || !strings.Contains(out, "대상 1대 점검") {
				t.Errorf("요약:\n%s", out)
			}
		})
	}
}

// 흐름 중 패닉이어도 터미널(TUI Close)과 prepare 경고 출력 곳은 되돌린 뒤 패닉이 위로 올라간다.
func TestBiosFlowPanicRestoresTerminal(t *testing.T) {
	_, ua := startAllSrv(t, "hpe-dl360gen11", nil)
	e, o := flowEnv(t, "")
	o.user, o.profile = e.writeFile("targets.txt", ua), "VM"
	swapFlowPlan(t, func(*Config, *unifiedRun, *compareResult) *setPlan { panic("계획 패닉") }, nil)
	oldWarn := prepareWarnOut
	var flow *biosFlow
	func() {
		defer func() {
			if v := recover(); v != "계획 패닉" {
				t.Errorf("패닉이 위로 올라가야 함: %v", v)
			}
		}()
		runFlowTC(t, biosTUIArgs{O: o, UserSet: true}, "", []flowStep{{"profile-result", "qq"}}, nil, func(i int, f *biosFlow) { flow = f })
	}()
	if flow == nil || !flow.ui.closed || prepareWarnOut != oldWarn {
		t.Errorf("패닉 뒤 복원: closed=%v warnOut 복원=%v", flow != nil && flow.ui.closed, prepareWarnOut == oldWarn)
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

// flowNoPlan 은 설정 계획을 만들면 실패하게 바꿔 끼웁니다 (대상 입력만 보는 시험).
func flowNoPlan(t *testing.T) {
	swapFlowPlan(t, func(*Config, *unifiedRun, *compareResult) *setPlan {
		t.Error("계획을 만들면 안 됨")
		return &setPlan{}
	}, nil)
}

// flowTargetsOf 는 대상의 "입력=XML여부" 목록입니다.
func flowTargetsOf(ts []Target) string {
	var out []string
	for _, x := range ts {
		out = append(out, fmt.Sprintf("%s=%v", x.Input, x.XML))
	}
	return strings.Join(out, " ")
}

const flowCiscoHintC = "hostname 이 c 로 시작하면 자동, IP 는 Cisco(XML) 대상에 따로 넣으십시오"

// 대상 파일에 IP 가 있으면 Cisco(XML) 대상 파일을 더 묻는다 (없는 파일이면 다시 묻고), 그 목록은 모두 XML 대상.
// 프로파일 고르기에서 q 로 대상 입력에 돌아오면 앞의 Cisco(XML) 대상은 버린다.
func TestBiosFlowTargetsFileAsksCiscoFile(t *testing.T) {
	e, o := flowEnv(t, "")
	flowNoPlan(t)
	rf := e.writeFile("targets.txt", "10.0.0.1", "host1")
	cisco := e.writeFile("cisco.txt", "10.0.0.2")
	var got, gotXML string
	script := []flowStep{
		{"targets", "1"},
		{"targets-file", strings.Repeat("\x7f", len("user.txt")) + rf + "\r"},
		{"targets-xml-file", "nope.txt\r"},
		{"targets-xml-file", cisco + "\r"},
		{"profile", "q"},
		{"targets", "\x18"},
	}
	fr, err := runFlowTC(t, biosTUIArgs{O: o}, "", script, nil, func(i int, f *biosFlow) {
		if i == 4 {
			got, gotXML = flowTargetsOf(f.rc.Targets), f.a.O.userXML
		}
	})
	if err != nil {
		t.Fatalf("흐름 오류: %v", err)
	}
	if got != "10.0.0.1=false host1=false 10.0.0.2=true" || gotXML != cisco {
		t.Errorf("대상 = %s, userXML = %q", got, gotXML)
	}
	if o.userXML != "" {
		t.Errorf("대상 입력으로 돌아오면 Cisco(XML) 대상을 버려야 함: %q", o.userXML)
	}
	sc := fr.screen()
	for _, want := range []string{"Cisco 장비는 XML 로 점검합니다 —", flowCiscoHintC, "Cisco(XML) 대상 파일 (IP 로 적은 Cisco 장비, 비우면 없음)",
		"Cisco(XML) 대상 파일을 찾을 수 없습니다: nope.txt"} {
		if !strings.Contains(sc, want) {
			t.Errorf("화면에 %q 없음", want)
		}
	}
}

// 붙여넣기에 IP 가 있으면 두 번째 붙여넣기 화면(Cisco(XML) 대상)을 보이고 두 목록을 함께 쓴다.
func TestBiosFlowTargetsPasteAsksCisco(t *testing.T) {
	_, o := flowEnv(t, "")
	flowNoPlan(t)
	var got, gotArg string
	script := []flowStep{
		{"targets", "2"},
		{"targets-paste", "10.0.0.1\nc201\x04"},
		{"targets-xml-paste", "10.0.0.2\n10.0.0.3\x04"},
		{"profile", "q"},
		{"targets", "\x18"},
	}
	fr, err := runFlowTC(t, biosTUIArgs{O: o}, "", script, nil, func(i int, f *biosFlow) {
		if i == 3 {
			got, gotArg = flowTargetsOf(f.rc.Targets), f.a.O.userXMLArg
		}
	})
	if err != nil {
		t.Fatalf("흐름 오류: %v", err)
	}
	if got != "10.0.0.1=false c201=true 10.0.0.2=true 10.0.0.3=true" || gotArg != "10.0.0.2\n10.0.0.3" {
		t.Errorf("대상 = %s, 붙여넣은 Cisco = %q", got, gotArg)
	}
	sc := fr.screen()
	for _, want := range []string{flowCiscoHintC, "Cisco(XML) 대상 붙여넣기 (IP 로 적은 Cisco 장비만, 없으면 바로 Ctrl+D)"} {
		if !strings.Contains(sc, want) {
			t.Errorf("화면에 %q 없음", want)
		}
	}
}

// 대상이 hostname 뿐이면 Cisco(XML) 대상을 묻지 않는다 (접두어로 자동). xml_host_prefix 가 비면 hostname 이어도 묻는다.
func TestBiosFlowTargetsNoIPNoCiscoAsk(t *testing.T) {
	for _, c := range []struct {
		name, conf, hint, want string
		ask                    bool
	}{
		{"prefix", "", flowCiscoHintC, "c201=true host1=false", false},
		{"noprefix", "xml_host_prefix=\n", "IP·이름 모두 Cisco(XML) 대상에 넣으십시오", "c201=false host1=false", true},
	} {
		t.Run(c.name, func(t *testing.T) {
			e, o := flowEnv(t, "")
			flowNoPlan(t)
			if c.conf != "" {
				conf, err := os.ReadFile(e.conf)
				must(t, err)
				must(t, os.WriteFile(e.conf, append(conf, []byte(c.conf)...), 0o600))
			}
			rf := e.writeFile("names.txt", "c201", "host1")
			script := []flowStep{
				{"targets", "1"},
				{"targets-file", strings.Repeat("\x7f", len("user.txt")) + rf + "\r"},
			}
			if c.ask {
				script = append(script, flowStep{"targets-xml-file", "\r"})
			}
			pi := len(script)
			script = append(script, flowStep{"profile", "q"}, flowStep{"targets", "\x18"})
			var got string
			fr, err := runFlowTC(t, biosTUIArgs{O: o}, "", script, nil, func(i int, f *biosFlow) {
				if i == pi {
					got = flowTargetsOf(f.rc.Targets)
				}
			})
			if err != nil {
				t.Fatalf("흐름 오류: %v", err)
			}
			if got != c.want {
				t.Errorf("대상 = %s, 기대 %s", got, c.want)
			}
			if sc := fr.screen(); !strings.Contains(sc, c.hint) {
				t.Errorf("화면에 %q 없음", c.hint)
			}
		})
	}
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

// 비교서버는 공백·줄바꿈으로 여러 개를 줄 수 있고(대상 입력과 같은 규칙), q 두 번은 수정 여부를 묻지 않고 끝낸다 (수정은 e 로만).
func TestBiosFlowRefSplitAndQuitWithoutAsking(t *testing.T) {
	_, ua := startAllSrv(t, "hpe-dl360gen11", nil)
	_, ub := startAllSrv(t, "hpe-dl360gen11", func(s *mockbmc.Server) {
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
		{"targets-xml-paste", "\x04"},
		{"profile", "2"},
		{"profile-result", "\t"},
		{"ref", "1"},
		{"ref-host", ua + " " + ub + "\x04"},
		{"compare-result", "qq"}, // 설정할 항목이 있어도 묻지 않고 끝 (ask-set 단계가 없어야 함)
	}
	fr, err := runFlowT(t, biosTUIArgs{O: o}, "", script, nil)
	if err != nil {
		t.Fatalf("흐름 오류: %v", err)
	}
	if fmt.Sprint(fr.steps) != fmt.Sprint(flowNames(script)) {
		t.Errorf("단계 = %v", fr.steps)
	}
	if gotCmp == nil || !strings.Contains(gotCmp.Label, ua) || !strings.Contains(gotCmp.Label, ub) {
		t.Errorf("비교서버 2개가 모두 쓰여야 함: %+v", gotCmp)
	}
}
