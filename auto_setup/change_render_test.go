package main

import (
	"strings"
	"testing"
)

// chgRenderStates: 모든 단계의 화면 상태 (긴 호스트명·긴 한국어 상세 포함)
func chgRenderStates(t *testing.T) map[string]*changeState {
	t.Helper()
	r := newChgRig(t)
	long := "very-long-hostname-for-wrap-test-0001.example.internal"
	r.env.Tracked[long] = "J20261012-0001/infra_inventory-very-long-group-name.yml"
	r.env.User = &oneoffState{menu: []string{"1) alice — 아주 긴 설명이 붙은 사용자 메뉴 줄입니다 폭이 좁아도 잘리면 안 됩니다"}, route: "/r"}
	st := map[string]*changeState{}

	st["fatal"] = &changeState{env: r.env, kind: kindLDAP, step: csFatal, fatal: "ldap-config-engine 을 찾을 수 없습니다 (auto_setup.conf 의 ldap_engine 또는 auto_setup 과 같은 디렉터리·PATH)"}
	st["rbsel"] = newChangeState(r.env, kindRollback)
	st["user"] = newChangeState(r.env, kindIP)
	c := newChangeState(r.env, kindBoth)
	c.user, c.step, c.infras = "user1", csInfra, []string{"alpha", "beta-infra-with-a-long-name"}
	st["infra"] = c
	c = newChangeState(r.env, kindBoth)
	c.user, c.infra, c.step = "user1", "alpha", csInput
	c.in = []rune(long + " 10.123.123.123\nh2 10.1.1.6\nh3 300.1.1.1\n" + long + "x")
	st["input"] = c
	c = newChangeState(r.env, kindBoth)
	c.user, c.infra, c.step = "user1", "alpha", csPreview
	c.entries, c.warns = parseIPInput(long + " 10.123.123.123\nh2 10.1.1.6\nh3 300.1.1.1\n")
	c.hosts = []string{long, "h2"}
	c.dry = map[string]*chgHost{long: {Host: long, LDAPResult: rsFail, Detail: "LDAP: /etc/sssd/sssd.conf 에 도메인 섹션이 없습니다 (sssd 미구성 노드로 판단) / 추가 상세 설명이 아주 길게 이어집니다"},
		"h2": {Host: "h2", LDAPResult: rsChanged}}
	st["preview"] = c
	c = newChangeState(r.env, kindBoth)
	c.user, c.infra, c.step, c.hosts = "user1", "alpha", csRun, []string{long, "h2"}
	st["run"] = c
	c = newChangeState(r.env, kindBoth)
	c.user, c.infra, c.step, c.cancelled, c.rbIP = "user1", "alpha", csResult, true, false
	c.results = []*chgHost{
		{Host: long, IPResult: rsOK, OldIP: "10.100.100.100", NewIP: "10.123.123.123", GW: "10.123.123.1", LDAPResult: rsFail, Route: "os6",
			Detail: "LDAP: /etc/sssd/sssd.conf 에 도메인 섹션이 없습니다 (sssd 미구성 노드로 판단) / 두 번째 상세 줄도 아주 길게 이어집니다"},
		{Host: "h2", IPResult: rsFail, LDAPResult: rsSkipIP, Detail: "IP: ifcfg 를 찾지 못했습니다 (network_scripts_dir=/etc/sysconfig/network-scripts)"},
		{Host: "h3", IPResult: rsOK, NewIP: "10.1.1.7", GW: "10.1.1.1", LDAPResult: rsOK, ApplPending: true},
		{Host: "h4", IPResult: rsCancel},
	}
	c.recPath = "/tmp/auto_setup/changes/user1/20261012093000_both.json"
	st["result"] = c
	return st
}

func TestChangeRenderWrapNoTruncation(t *testing.T) {
	for name, c := range chgRenderStates(t) {
		for _, w := range []int{60, 80, 120} {
			for _, color := range []bool{false, true} {
				out := renderChange(c, w, 0, color)
				for i, l := range strings.Split(out, "\n") {
					p := stripANSI(l)
					if sw := strWidth(p); sw > w {
						t.Errorf("%s w=%d color=%v 줄 %d 폭 %d 초과: %q", name, w, color, i, sw, p)
					}
					if strings.Contains(p, "…") {
						t.Errorf("%s w=%d 줄 %d 잘림(…): %q", name, w, i, p)
					}
				}
			}
		}
	}
	// 줄바꿈해도 내용은 모두 남는다 (폭 60 에서 긴 호스트명·상세가 그대로 보임)
	st := chgRenderStates(t)
	plain := strings.Join(strings.Fields(stripANSI(renderChange(st["result"], 60, 0, false))), "")
	for _, s := range []string{"very-long-hostname-for-wrap-test-0001.example.internal", "두번째상세줄도아주길게이어집니다", "건너뜀(IP실패)", "미확인(취소)",
		"취소됨—일부호스트만변경됐을수있음", "20261012093000_both.json"} {
		if !strings.Contains(plain, s) {
			t.Errorf("폭 60 결과 화면에 %q 없음:\n%s", s, renderChange(st["result"], 60, 0, false))
		}
	}
}

func TestChangeRenderHeightFits(t *testing.T) {
	for name, c := range chgRenderStates(t) {
		for _, h := range []int{15, 24, 40} {
			out := renderChange(c, 80, h, false)
			if n := len(strings.Split(out, "\n")); n != h {
				t.Errorf("%s 높이 %d: %d 줄", name, h, n)
			}
		}
	}
}

func TestChangeRenderRedDanger(t *testing.T) {
	st := chgRenderStates(t)
	if out := renderChange(st["preview"], 120, 0, true); !strings.Contains(out, "\x1b[1;31m yes 를 입력하면 2대에 실제로 적용합니다") {
		t.Errorf("yes 안내는 빨강:\n%q", out)
	}
	res := renderChange(st["result"], 120, 0, true)
	for _, want := range []string{"\x1b[1;31mFAIL\x1b[0m", "\x1b[90m건너뜀(IP 실패)\x1b[0m", "\x1b[1;31m 취소됨 — 일부 호스트만 변경됐을 수 있음"} {
		if !strings.Contains(res, want) {
			t.Errorf("결과 색 %q 없음", want)
		}
	}
	if strings.Contains(renderChange(st["result"], 120, 0, false), "\x1b[") {
		t.Error("색 끄면 ANSI 없음")
	}
}
