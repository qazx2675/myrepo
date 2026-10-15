package main

import (
	"fmt"
	"io"
	"strings"
	"testing"
)

// tui_flow.go setStep 시험: 적용 전 확인 화면 1(차이 항목 전부, 수정대상/수정제외) → 수정제외가 있으면 확인 화면 2 →
// (남의 Pending 질문) → 적용. 어느 확인에서든 n·Ctrl+X 면 아무것도 쓰지 않고 false (결과 보기로 돌아감).

// runSetStepT 는 가짜 TUI 로 setStep 만 돌립니다. 단계 이름 순서가 script 와 같아야 하고, 키는 그 화면이 키를 기다리기
// 시작할 때(env.Ready) 넣습니다. 반환: setStep 결과, 지나온 단계, 화면 출력(ANSI 제거), 흐름.
func runSetStepT(t *testing.T, plan *setPlan, script []flowStep, apply applyFn) (bool, []string, string, *biosFlow) {
	t.Helper()
	swapFlowPlan(t, func(*Config, *unifiedRun, *compareResult) *setPlan { return plan }, apply)
	pr, pw := io.Pipe()
	t.Cleanup(func() { pw.Close() })
	pending := ""
	u, r := newTUIRigReader(t, pr, func(e *tuiEnv, r *tuiRig) {
		r.w = 240 // 확인 줄이 잘리지 않게 넓은 화면
		e.Ready = func() {
			if k := pending; k != "" {
				pending = ""
				pw.Write([]byte(k))
			}
		}
	})
	var steps []string
	next := 0
	f := &biosFlow{ui: u, a: biosTUIArgs{O: &commonOpts{}}, rc: &runContext{Conf: &Config{}}}
	f.step = func(name string) {
		steps = append(steps, name)
		if next >= len(script) || script[next].name != name {
			t.Errorf("예상 밖 단계 %q (지나온 단계 %v)", name, steps)
			pw.Close()
			return
		}
		pending += script[next].keys
		next++
	}
	ok := f.setStep()
	if next < len(script) {
		t.Errorf("지나가지 않은 단계 %v (지나온 단계 %v)", script[next:], steps)
	}
	return ok, steps, tuiStripANSI(r.out.String()), f
}

// confirmPlan 은 수정대상 2건(2대) + 보호 속성 1건 + 그 밖의 수정제외 1건 계획입니다.
func confirmPlan(pending bool) *setPlan {
	p := &setPlan{
		Items: []setItem{
			{Host: "srvB", Attr: "ProcTurbo", From: "Disabled", To: "Enabled", Source: SetSrcCompare, hostIdx: 1, PendingHost: pending},
			{Host: "srvA", Attr: "ProcHyperthreading", From: "Disabled", To: "Enabled", Source: SetSrcProfile, hostIdx: 0},
		},
		Protected: []setItem{{Host: "srvB", Attr: "BootMode", From: "LegacyBios", To: "Uefi", Source: SetSrcCompare, hostIdx: 1, Reason: "보호 속성 (패턴 BootMode)"}},
		Excluded:  []setItem{{Host: "srvA", Attr: "SubNumaCluster", From: "Enabled", To: "Disabled", Source: SetSrcProfile, hostIdx: 0, Reason: "프로파일 행 verified=N (UNVERIFIED)"}},
	}
	if pending {
		p.Pending = []pendingInfo{{Host: "srvB", Attr: "Sriov", Pending: "Disabled"}}
	}
	return p
}

func TestSetStepConfirmScreensApply(t *testing.T) {
	var gotIncl []bool
	apply := func(_ *runContext, _ *unifiedRun, p *setPlan, incl bool, _ func(string, string), _ <-chan struct{}) (*setOutcome, error) {
		gotIncl = append(gotIncl, incl)
		return &setOutcome{Lines: []string{"적용 결과 줄"}, OK: 2}, nil
	}
	ok, steps, sc, f := runSetStepT(t, confirmPlan(false), []flowStep{{"ask-set", "y"}, {"ask-excluded", "y"}, {"set-result", "q"}}, apply)
	if !ok || fmt.Sprint(gotIncl) != "[false]" {
		t.Errorf("결과 %v 적용 %v (단계 %v)", ok, gotIncl, steps)
	}
	for _, w := range []string{
		"srvA  ProcHyperthreading  Disabled -> Enabled (수정대상)", "srvA  SubNumaCluster  Enabled -> Disabled (수정제외: 프로파일 행 verified=N (UNVERIFIED))",
		"srvB  ProcTurbo  Disabled -> Enabled (수정대상)", "srvB  BootMode  LegacyBios -> Uefi (수정제외: 보호 속성 (패턴 BootMode))",
		"위 수정대상을 적용할까요? [y/n]", "아래 항목은 변경하지 않습니다 (수정제외 2건)", "수정제외 항목을 확인했습니다. 진행할까요? [y/n]", "적용 결과 줄",
	} {
		if !strings.Contains(sc, w) {
			t.Errorf("화면에 %q 없음", w)
		}
	}
	// 확인 화면 1 은 호스트(점검 순서)별: srvA 줄이 srvB 줄보다 먼저
	cl := strings.Join(confirmPlan(false).ConfirmLines(), "\n")
	if strings.Index(cl, "srvA  ProcHyperthreading") > strings.Index(cl, "srvB  ProcTurbo") ||
		strings.Index(cl, "srvA  SubNumaCluster") > strings.Index(cl, "srvB  ProcTurbo") {
		t.Errorf("호스트별로 묶이지 않음:\n%s", cl)
	}
	if !strings.Contains(f.setNote, "Pending 확인 2대") {
		t.Errorf("setNote = %q", f.setNote)
	}
}

// 어느 확인에서든 n·Ctrl+X 면 쓰지 않고 false. 수정제외가 없으면 확인 화면 2 는 나오지 않는다.
func TestSetStepConfirmCancel(t *testing.T) {
	for _, c := range []struct {
		name    string
		plan    *setPlan
		script  []flowStep
		applied bool
	}{
		{"1차 n", confirmPlan(false), []flowStep{{"ask-set", "n"}}, false},
		{"1차 Ctrl+X", confirmPlan(false), []flowStep{{"ask-set", "\x18"}}, false},
		{"2차 n", confirmPlan(false), []flowStep{{"ask-set", "y"}, {"ask-excluded", "n"}}, false},
		{"2차 Ctrl+X", confirmPlan(false), []flowStep{{"ask-set", "y"}, {"ask-excluded", "\x18"}}, false},
		{"Pending Ctrl+X", confirmPlan(true), []flowStep{{"ask-set", "y"}, {"ask-excluded", "y"}, {"ask-pending", "\x18"}}, false},
		{"수정제외 없음", &setPlan{Items: confirmPlan(false).Items}, []flowStep{{"ask-set", "y"}, {"set-result", "q"}}, true},
	} {
		t.Run(c.name, func(t *testing.T) {
			called := false
			apply := func(*runContext, *unifiedRun, *setPlan, bool, func(string, string), <-chan struct{}) (*setOutcome, error) {
				called = true
				return &setOutcome{}, nil
			}
			ok, steps, _, f := runSetStepT(t, c.plan, c.script, apply)
			if ok != c.applied || called != c.applied {
				t.Errorf("결과 %v 적용 %v, 기대 %v (단계 %v)", ok, called, c.applied, steps)
			}
			if !c.applied && !strings.Contains(f.setNote, "설정") {
				t.Errorf("setNote = %q", f.setNote)
			}
		})
	}
}

// 설정할 항목이 없어도 수정제외(보호 속성 등)가 있으면 보여 주고 false.
func TestSetStepEmptyShowsExcluded(t *testing.T) {
	p := confirmPlan(false)
	p.Items = nil
	ok, _, sc, f := runSetStepT(t, p, []flowStep{{"preview-empty", "q"}}, nil)
	if ok || f.setNote != "설정할 항목이 없습니다" || !strings.Contains(sc, "srvB  BootMode  LegacyBios -> Uefi (수정제외: 보호 속성") {
		t.Errorf("결과 %v setNote %q\n%s", ok, f.setNote, sc)
	}
}
