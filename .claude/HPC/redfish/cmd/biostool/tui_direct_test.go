package main

// setdirect.go · tui_flow.go directStep 시험 (BIOS 값 직접 입력). 실제 BMC 는 쓰지 않고 internal/mockbmc(testdata 합성 트리)만 쓴다.

import (
	"fmt"
	"strings"
	"testing"

	"biostool/internal/mockbmc"
)

// 줄 읽기: 빈 줄·# 줄 무시, 첫 공백(스페이스·탭) 뒤 전부가 값, 값 없음·중복(대소문자 무시)·제어문자는 오류.
func TestParseDirectLines(t *testing.T) {
	reqs, errs := parseDirectLines([]string{
		"", "# 주석", "  SysProfile   PerfOptimized  ", "Attr\tvalue with  spaces", "NoValue", "sysprofile Other", "Bad\x01 x", "Ok\tA\tB",
	})
	if got := fmt.Sprint(reqs); got != "[{SysProfile PerfOptimized} {Attr value with  spaces}]" {
		t.Errorf("reqs = %s", got)
	}
	want := []string{
		"5번째 줄 NoValue: 설정값이 없음",
		"6번째 줄 sysprofile Other: 속성 이름 중복 (3번째 줄과 같음)",
		"7번째 줄 Bad? x: 제어문자",
		"8번째 줄 Ok A B: 제어문자", // 값 가운데의 탭
	}
	if len(errs) != len(want) {
		t.Fatalf("errs = %q", errs)
	}
	for i, w := range want {
		if !strings.Contains(errs[i], w) {
			t.Errorf("errs[%d] = %q, 기대 %q 포함", i, errs[i], w)
		}
	}
	if r, e := parseDirectLines([]string{"# 만", " "}); len(r) != 0 || len(e) != 0 {
		t.Errorf("주석만: %v %v", r, e)
	}
}

// reasonOf 는 목록에서 host·attr 항목의 사유를 돌려줍니다 (없으면 "").
func reasonOf(list []setItem, host, attr string) string {
	for _, it := range list {
		if it.Host == host && it.Attr == attr {
			return it.Reason
		}
	}
	return "(없음)"
}

// 호스트별 판정 (점검 스냅샷만, BMC 접속 없음): 같은 값(대소문자 무시)·미지원 속성·보호 속성(내장 ∪ protect_attrs)·
// 제외 목록·형식·XML·점검 오류·쓰기 대상 아닌 벤더·Dell 남의 Pending 은 건너뛰고, 남은 항목만 계획에 넣는다.
func TestDirectPlanDecisions(t *testing.T) {
	type hs struct {
		name, vendor string
		attrs        map[string]string
		mod          func(h *hostCheck)
	}
	specs := []hs{
		{"hpe-a", "HPE", map[string]string{"ProcTurbo": "Enabled", "BootMode": "Uefi", "ProcX2Apic": "Enabled", "NumVal": "5", "Flag": "false",
			"SystemTime": "x", "WorkloadProfile": "GeneralPowerEfficientCompute", "ProcVirtualization": "Enabled"}, nil},
		{"hpe-b", "HPE", map[string]string{"ProcTurbo": "DISABLED", "BootMode": "Uefi", "Flag": "true"}, nil}, // 모두 건너뜀 → 호스트 통째로
		{"cisco", "Cisco", map[string]string{"ProcTurbo": "Enabled"}, func(h *hostCheck) { h.XML = true }},
		{"down", "HPE", nil, func(h *hostCheck) { h.Status = StatusUnreachable }},
		{"dell-p", "Dell", map[string]string{"ProcTurbo": "Enabled"}, func(h *hostCheck) { h.pendDiff = map[string]string{"MemTest": "Disabled"} }},
		{"hpe-p", "HPE", map[string]string{"ProcTurbo": "Enabled"}, func(h *hostCheck) {
			h.pendDiff = map[string]string{"Sriov": "Disabled", "ProcTurbo": "Disabled"}
		}},
		{"smc", "Supermicro", map[string]string{"ProcTurbo": "Enabled"}, nil},
	}
	u := &unifiedRun{Check: &checkRun{}}
	for _, s := range specs {
		hc := &hostCheck{Target: Target{Hostname: s.name}, Info: &SystemInfo{Vendor: s.vendor, Model: "M1", SettingsPath: "/redfish/v1/Systems/1/Bios/Settings"}}
		if s.mod != nil {
			s.mod(hc)
		}
		u.Check.Hosts = append(u.Check.Hosts, hc)
		u.Snaps = append(u.Snaps, &biosSnap{Target: hc.Target, Attrs: s.attrs})
	}
	reqs, errs := parseDirectLines([]string{
		"ProcTurbo Disabled", "BootMode LegacyBios", "ProcX2Apic Disabled", "NoSuchAttr 1", "NumVal abc", "Flag TRUE", "SystemTime 1", "WorkloadProfile Custom",
	})
	if len(errs) != 0 {
		t.Fatal(errs)
	}
	reqs = append(reqs, directReq{Attr: "ProcVirtualization", Value: "Dis\x1b[2Jabled"}) // 줄 읽기를 거치지 않은 제어문자도 화면에는 정리
	p := buildDirectPlan(&Config{ProtectAttrs: []string{"ProcX2Apic"}}, u, reqs)

	var keys []string
	for _, it := range p.Items {
		keys = append(keys, fmt.Sprintf("%s|%s|%s|%s|%s|%v", it.Host, it.Attr, it.From, it.To, it.std, it.PendingHost))
	}
	want := []string{
		"hpe-a|ProcTurbo|Enabled|Disabled|direct|false", "hpe-a|Flag|false|true|direct|false",
		"hpe-a|WorkloadProfile|GeneralPowerEfficientCompute|Custom|" + profileStd + "|false",
		"hpe-a|ProcVirtualization|Enabled|Dis\x1b[2Jabled|direct|false",
		"hpe-p|ProcTurbo|Enabled|Disabled|direct|true",
	}
	if fmt.Sprint(keys) != fmt.Sprint(want) {
		t.Errorf("항목 =\n%q\n기대\n%q", keys, want)
	}
	for _, it := range p.Items {
		if it.Source != SetSrcDirect {
			t.Errorf("%s %s 출처 = %s", it.Host, it.Attr, it.Source)
		}
	}
	for _, c := range []struct {
		list       []setItem
		host, attr string
		want       string
	}{
		{p.Protected, "hpe-a", "BootMode", "보호 속성 (패턴 BootMode)"},
		{p.Protected, "hpe-a", "ProcX2Apic", "보호 속성 (패턴 ProcX2Apic)"}, // bios.conf protect_attrs
		{p.Protected, "smc", "BootMode", "보호 속성"},                     // 없는 속성이어도 보호 속성이 먼저
		{p.Excluded, "hpe-a", "NoSuchAttr", "미지원 속성"},
		{p.Excluded, "hpe-a", "NumVal", "형식이 맞지 않는 값 (현재값이 정수)"},
		{p.Excluded, "hpe-a", "SystemTime", "제외 목록"},
		{p.Excluded, "hpe-b", "ProcTurbo", "이미 같은 값"},
		{p.Excluded, "hpe-b", "Flag", "이미 같은 값"},
		{p.Excluded, "dell-p", "ProcTurbo", "Dell 은 설정 Job 이 남의 Pending 까지 적용하므로 제외"},
		{p.Excluded, "smc", "ProcTurbo", "쓰기 대상 벤더가 아님"},
	} {
		if r := reasonOf(c.list, c.host, c.attr); !strings.Contains(r, c.want) {
			t.Errorf("%s %s 사유 = %q, 기대 %q 포함", c.host, c.attr, r, c.want)
		}
	}
	if fmt.Sprint(p.Pending) != "[{hpe-p Sriov Disabled}]" { // ProcTurbo Pending 은 이번 입력값이라 남의 것이 아님
		t.Errorf("Pending = %v", p.Pending)
	}
	if n := p.Items[len(p.Items)-1].Note; !strings.Contains(n, "Pending 이 이미 목표값") {
		t.Errorf("hpe-p Note = %q", n)
	}

	lines := strings.Join(p.DirectLines(), "\n")
	for _, w := range []string{
		"수정대상 5건",
		"hpe-a  ProcTurbo: Enabled -> Disabled (수정대상)",
		"hpe-a  NoSuchAttr: - -> 1 (건너뜀: 미지원 속성 (이 호스트에 없음))",
		"hpe-a  BootMode: Uefi -> LegacyBios (건너뜀: 보호 속성 (패턴 BootMode)",
		"hpe-b  ProcTurbo: DISABLED -> Disabled (건너뜀: 이미 같은 값)",
		"hpe-p  ProcTurbo: Enabled -> Disabled (수정대상, 남의 Pending 있는 장비",
		"hpe-p  Sriov  Pending=Disabled",
		"cisco  건너뜀: Cisco(XML) 대상",
		"down  건너뜀: 점검 오류 (UNREACHABLE)",
		"smc  쓰기 대상 벤더가 아님",
		"ProcVirtualization: Enabled -> Dis?[2Jabled (수정대상)",
	} {
		if !strings.Contains(lines, w) {
			t.Errorf("미리보기에 %q 없음:\n%s", w, lines)
		}
	}
	if strings.Contains(lines, "\x1b") {
		t.Error("미리보기에 제어문자가 그대로 나옴")
	}
	if strings.Index(lines, "hpe-a  ") > strings.Index(lines, "hpe-b  ") {
		t.Error("미리보기는 점검 순서대로")
	}
}

// 적용 (mock BMC): 대소문자만 다른 값은 레지스트리 표기로 보내고, 허용값 밖 값은 그 항목만 보내지 않는다 (INVALID_VALUE).
// 계획에 보호 속성을 억지로 넣어도 runHost 가 그 호스트를 통째로 막는다 (PATCH 없음).
func TestDirectPlanApplyMock(t *testing.T) {
	a, ua := startAllSrv(t, "hpe-dl360gen11", nil)
	b, ub := startAllSrv(t, "hpe-dl360gen11", func(s *mockbmc.Server) {
		setAttrs(t, s, map[string]interface{}{"ProcTurbo": "Disabled"})
	})
	rc := checkRCFor(t, 2, "", ua, ub)
	u := runUnifiedSet(t, rc)
	reqs, errs := parseDirectLines([]string{"procturbo disabled", "MinProcIdlePower nocstates", "ThermalConfig Bogus Value"})
	if len(errs) != 0 {
		t.Fatal(errs)
	}
	before := callCounts(a, b)
	p := buildDirectPlan(rc.Conf, u, reqs)
	if fmt.Sprint(callCounts(a, b)) != fmt.Sprint(before) {
		t.Error("buildDirectPlan 이 BMC 에 접속함")
	}
	want := []string{
		ua + "|ProcTurbo|Enabled|disabled|direct|false", ua + "|MinProcIdlePower|C6|nocstates|direct|false",
		ua + "|ThermalConfig|OptimalCooling|Bogus Value|direct|false",
		ub + "|MinProcIdlePower|C6|nocstates|direct|false", ub + "|ThermalConfig|OptimalCooling|Bogus Value|direct|false",
	}
	if got := planKeys(p); fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("항목 = %v", got)
	}
	out, _ := applyT(t, rc, u, p, false, nil, nil)
	if fmt.Sprint(out.Tried) != "[0 1]" {
		t.Errorf("Tried = %v", out.Tried)
	}
	pa, pb := patchBodies(a), patchBodies(b)
	if len(pa) != 1 || fmt.Sprint(patchAttrs(t, pa[0])) != "map[MinProcIdlePower:NoCStates ProcTurbo:Disabled]" {
		t.Errorf("a PATCH = %v", pa)
	}
	if len(pb) != 1 || fmt.Sprint(patchAttrs(t, pb[0])) != "map[MinProcIdlePower:NoCStates]" {
		t.Errorf("b PATCH = %v", pb)
	}
	for host, attr := range map[string]string{ua: "ProcTurbo", ub: "MinProcIdlePower"} {
		if st, d := itemStatus(t, u, host, attr); st != StatusApplied {
			t.Errorf("%s %s = %s (%s)", host, attr, st, d)
		}
	}
	if st, d := itemStatus(t, u, ua, "ThermalConfig"); st != StatusInvalidValue || !strings.Contains(d, "허용값") {
		t.Errorf("ThermalConfig = %s (%s), 기대 INVALID_VALUE", st, d)
	}

	// 방어 중첩: 보호 속성이 섞인 직접 입력 계획 → 접속도 PATCH 도 없음 (PROTECTED_ATTR)
	bad := &setPlan{Items: []setItem{
		{Host: ua, Attr: "ProcVirtualization", From: "Enabled", To: "Disabled", Source: SetSrcDirect, std: SetSrcDirect, hostIdx: 0},
		{Host: ua, Attr: "BootMode", From: "Uefi", To: "LegacyBios", Source: SetSrcDirect, std: SetSrcDirect, hostIdx: 0},
	}}
	n := a.CallCount()
	if out, _ := applyT(t, rc, u, bad, false, nil, nil); len(out.Tried) != 0 {
		t.Errorf("보호 속성 계획 Tried = %v", out.Tried)
	}
	if a.CallCount() != n || len(patchBodies(a)) != 1 {
		t.Error("보호 속성이 섞인 계획으로 BMC 에 접속하거나 PATCH 함")
	}
	if st, _ := itemStatus(t, u, ua, "ProcVirtualization"); st != StatusProtectedAttr {
		t.Errorf("ProcVirtualization = %s, 기대 %s", st, StatusProtectedAttr)
	}
	for _, s := range []*mockbmc.Server{a, b} {
		assertSetSafe(t, s)
		for _, body := range patchBodies(s) {
			if strings.Contains(body, "Bogus") || strings.Contains(body, "BootMode") {
				t.Errorf("보내면 안 되는 값: %s", body)
			}
		}
	}
}

// 흐름: e → 수정 방법 2 → 붙여넣기(# 줄·탭 구분) → 미리보기(건너뜀 사유) → y → 설정 결과 → 보낸 호스트만 재점검 → 직접 입력 확인.
// 모든 항목이 건너뛰어지는 b 는 접속하지 않는다.
func TestBiosFlowDirectInput(t *testing.T) {
	a, ua := startAllSrv(t, "hpe-dl360gen11", nil)
	b, ub := startAllSrv(t, "hpe-dl360gen11", func(s *mockbmc.Server) {
		setAttrs(t, s, map[string]interface{}{"ProcTurbo": "Disabled", "MinProcIdlePower": "NoCStates"})
	})
	e, o := flowEnv(t, "")
	o.user, o.profile = e.writeFile("targets.txt", ua, ub), "VM"
	swapFlowPlan(t, func(*Config, *unifiedRun, *compareResult) *setPlan {
		t.Error("직접 입력은 점검 결과 기준 계획을 만들지 않음")
		return &setPlan{}
	}, applySetPlan)
	script := []flowStep{
		{"profile-result", "e"},
		{"edit-menu", "2"},
		{"direct-input", "# comment\nprocturbo disabled\nminprocidlepower\tnocstates\nBootMode LegacyBios\nNoSuchAttr 1\x04"},
		{"direct-ask", "y"}, // 3
		{"set-result", "q"},
		{"direct-check", "q"}, // 5
		{"profile-result", "qq"},
	}
	fr, err := runFlowTC(t, biosTUIArgs{O: o, UserSet: true}, "", script, nil, func(i int, f *biosFlow) {
		text := strings.Join(f.ui.text, "\n")
		switch i {
		case 3:
			for _, w := range []string{
				"수정대상 2건",
				ua + "  ProcTurbo: Enabled -> disabled (수정대상)", ua + "  MinProcIdlePower: C6 -> nocstates (수정대상)",
				ua + "  BootMode: Uefi -> LegacyBios (건너뜀: 보호 속성", ua + "  NoSuchAttr: - -> 1 (건너뜀: 미지원 속성",
				ub + "  ProcTurbo: Disabled -> disabled (건너뜀: 이미 같은 값)", ub + "  MinProcIdlePower: NoCStates -> nocstates (건너뜀: 이미 같은 값)",
			} {
				if !strings.Contains(text, w) {
					t.Errorf("미리보기에 %q 없음:\n%s", w, text)
				}
			}
		case 5:
			for _, w := range []string{
				"목표값 확인 2/2건",
				ua + "  ProcTurbo: 현재값 Enabled · Pending Disabled (Pending 목표값 확인",
				ua + "  MinProcIdlePower: 현재값 C6 · Pending NoCStates (Pending 목표값 확인",
			} {
				if !strings.Contains(text, w) {
					t.Errorf("직접 입력 확인에 %q 없음:\n%s", w, text)
				}
			}
			if strings.Contains(text, ub) {
				t.Error("보내지 않은 b 는 직접 입력 확인에 없어야 함")
			}
			if !strings.Contains(f.setNote, "재점검 1대 완료") || !strings.Contains(f.setNote, "직접 입력 재점검 목표값 2/2건") {
				t.Errorf("setNote = %q", f.setNote)
			}
		}
	})
	if err != nil {
		t.Fatalf("흐름 오류: %v", err)
	}
	if fmt.Sprint(fr.steps) != fmt.Sprint(flowNames(script)) {
		t.Errorf("단계 = %v", fr.steps)
	}
	pa := patchBodies(a)
	if len(pa) != 1 || fmt.Sprint(patchAttrs(t, pa[0])) != "map[MinProcIdlePower:NoCStates ProcTurbo:Disabled]" {
		t.Errorf("a PATCH = %v", pa)
	}
	assertSetSafe(t, a)
	if ss := a.Sessions(); ss.Created != 3 || ss.Deleted != 3 { // 점검 · 설정 · 재점검
		t.Errorf("a 세션 = %+v, 기대 3", ss)
	}
	assertOneSession(t, b) // 모두 건너뜀 → 설정·재점검 접속 없음
	assertClean(t, b)
	if out := fr.out.String(); !strings.Contains(out, "설정: Pending 확인 1대") || !strings.Contains(out, "직접 입력 재점검 목표값 2/2건") {
		t.Errorf("요약:\n%s", out)
	}
}

// 수정 방법 고르기 취소 → 결과 보기, 잘못된 줄 → 오류를 보이고 다시 입력, Ctrl+X → 결과 보기,
// 모두 건너뛰면 미리보기만 보이고 적용하지 않음.
func TestBiosFlowDirectInputCancelErrorsEmpty(t *testing.T) {
	a, ua := startAllSrv(t, "hpe-dl360gen11", nil)
	e, o := flowEnv(t, "")
	o.user, o.profile = e.writeFile("targets.txt", ua), "VM"
	swapFlowPlan(t, func(*Config, *unifiedRun, *compareResult) *setPlan { return &setPlan{} }, nil) // 적용이 불리면 실패
	script := []flowStep{
		{"profile-result", "e"},
		{"edit-menu", "\x1b"}, // 고르기 취소
		{"profile-result", "e"},
		{"edit-menu", "2"},
		{"direct-input", "NoValueLine\x04"},
		{"direct-input", "\x18"}, // 5: 오류를 보고 Ctrl+X
		{"profile-result", "e"},
		{"edit-menu", "2"},
		{"direct-input", "BootMode LegacyBios\nprocturbo enabled\x04"},
		{"direct-empty", "q"}, // 9
		{"profile-result", "qq"},
	}
	fr, err := runFlowTC(t, biosTUIArgs{O: o, UserSet: true}, "", script, nil, func(i int, f *biosFlow) {
		text := strings.Join(f.ui.text, "\n")
		switch i {
		case 5:
			if !strings.Contains(text, "1번째 줄 NoValueLine: 설정값이 없음") {
				t.Errorf("입력 오류 화면:\n%s", text)
			}
		case 9:
			for _, w := range []string{ua + "  BootMode: Uefi -> LegacyBios (건너뜀: 보호 속성", ua + "  ProcTurbo: Enabled -> enabled (건너뜀: 이미 같은 값)", "수정대상 0건"} {
				if !strings.Contains(text, w) {
					t.Errorf("미리보기에 %q 없음:\n%s", w, text)
				}
			}
		}
	})
	if err != nil {
		t.Fatalf("흐름 오류: %v", err)
	}
	if fmt.Sprint(fr.steps) != fmt.Sprint(flowNames(script)) {
		t.Errorf("단계 = %v", fr.steps)
	}
	assertOneSession(t, a)
	assertClean(t, a)
	if out := fr.out.String(); !strings.Contains(out, "설정: 설정할 항목이 없습니다") {
		t.Errorf("요약:\n%s", out)
	}
}

// 덤프 읽기 모드는 수정 방법을 묻지 않고 설정하지 않는다.
func TestEditStepFromDumpRefuses(t *testing.T) {
	f := &biosFlow{a: biosTUIArgs{O: &commonOpts{fromDump: "dump"}}, step: func(name string) { t.Errorf("단계 %q 가 나오면 안 됨", name) }}
	if f.editStep() || f.setNote != "덤프 읽기 모드라 설정하지 않았습니다" {
		t.Errorf("editStep: setNote=%q", f.setNote)
	}
}

// directCanon: Enumeration 은 레지스트리 표기, 불리언은 소문자, 그 밖은 그대로.
func TestDirectCanon(t *testing.T) {
	enum := &regAttr{Type: "Enumeration", Values: []string{"Enabled", "Disabled", "enabled"}}
	for _, c := range []struct {
		val  string
		ra   *regAttr
		cur  string
		want string
	}{
		{"disabled", enum, `"Enabled"`, "Disabled"},
		{"enabled", enum, `"Enabled"`, "enabled"}, // 정확히 같은 값이 먼저
		{"Bogus", enum, `"Enabled"`, "Bogus"},     // 허용값 밖은 그대로 (typedValue 가 거부)
		{"TRUE", &regAttr{Type: "Boolean"}, `false`, "true"},
		{"TRUE", nil, `false`, "true"},
		{"TRUE", nil, `"x"`, "TRUE"},
		{"Abc", &regAttr{Type: "String"}, `"x"`, "Abc"},
	} {
		if got := directCanon(c.val, c.ra, []byte(c.cur)); got != c.want {
			t.Errorf("directCanon(%q, %v, %s) = %q, 기대 %q", c.val, c.ra, c.cur, got, c.want)
		}
	}
	if !directSame("05", "5") || !directSame("Enabled", "enabled") || directSame("x", "5") {
		t.Error("directSame: 대소문자 무시·정수 정규형으로 같아야 함")
	}
}
