// change_tui_test.go - m 변경 메뉴의 TUI 연결(v0.7.0): 가시성(conf·--admin·원격), 메뉴 → 흐름, 실행 중 키 무시·취소,
// 안내 줄·도움말, 꺼진 상태의 화면 불변, 메뉴 화면 레이아웃·골든. 외부 명령은 changerun_test.go 의 가짜 세계(chgWorld)가 대신한다.
package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// chgTUI: 키를 직접 먹이는 TUI 상태 (goroutine·실제 tty 없음 — 루프의 handle → poll → draw 순서를 그대로 흉내)
type chgTUI struct {
	t   *testing.T
	st  *tuiState
	rg  *rig
	w   *chgWorld
	dir string
}

// chgTUISetup: 변경 메뉴 전역 변수(conf·--admin·엔진·경유·실행기)를 테스트 값으로 바꾸고 첫 화면까지 그린다
func chgTUISetup(t *testing.T, ip, ldap, admin bool) *chgTUI {
	t.Helper()
	r := newChgRig(t)
	saved := []*string{&enable_ip_change, &enable_ldap_change, &os8_mgmt, &ip_engine, &ldap_engine, &confLoadedFrom,
		&os6_mgmt, &os6_gossh, &awx_dir, &os_check_sh}
	old := make([]string, len(saved))
	for i, p := range saved {
		old[i] = *p
	}
	oldAdmin, oldExec := adminMode, changeExec
	t.Cleanup(func() {
		for i, p := range saved {
			*p = old[i]
		}
		adminMode, changeExec = oldAdmin, oldExec
	})
	enable_ip_change, enable_ldap_change = "", ""
	if ip {
		enable_ip_change = "y"
	}
	if ldap {
		enable_ldap_change = "yes"
	}
	adminMode = admin
	os8_mgmt, awx_dir, os_check_sh = "", "", ""
	ip_engine, ldap_engine = "/x/ip-change-engine", "/x/ldap-config-engine"
	confLoadedFrom = filepath.Join(r.dir, "conf", "auto_setup.conf")
	os6_mgmt, os6_gossh = "os6mgmt", "/opt/gossh/gossh"
	changeExec = r.w.exec
	t.Setenv("AUTO_SETUP_DIR", filepath.Join(r.dir, "state"))

	env, rg := newRig(strings.NewReader(""))
	h := &chgTUI{t: t, rg: rg, w: r.w, dir: r.dir,
		st: &tuiState{src: &fakeSrc{snap: twoGroupSnap(), local: true}, env: env, pending: map[string]time.Time{}, restore: func() {}}}
	h.st.sel.DateFilter = true
	h.st.refresh()
	h.st.draw()
	return h
}

// keys: 문자열을 키로 (\n Enter, ESC Esc, 0x04 Ctrl+D, 0x18 Ctrl+X, 0x03 Ctrl+C, 0x1a Ctrl+Z) 하나씩 먹이고 그때마다 다시 그린다
func (h *chgTUI) keys(s string) {
	h.t.Helper()
	for _, ev := range parseKeys([]byte(s)) {
		h.st.handle(ev)
		h.st.pollOneoff()
		h.st.pollChange()
		h.st.draw()
	}
}

func (h *chgTUI) frame() string { return stripANSI(lastFrame(h.rg)) }

func (h *chgTUI) waitStep(step int) {
	h.t.Helper()
	waitFor(h.t, "변경 흐름 단계", func() bool {
		h.st.pollChange()
		h.st.draw()
		return h.st.chg != nil && h.st.chg.step == step
	})
}

// ---- 꺼져 있으면: 키 무반응 · 화면 불변 · 안내 줄에도 없음 ----

func TestChangeKeyOffIsNoop(t *testing.T) {
	h := chgTUISetup(t, false, false, false)
	wrap := func(s string) string { return "\x1b[H" + strings.ReplaceAll(s, "\n", "\x1b[K\r\n") + "\x1b[K\x1b[J" }
	before := lastFrame(h.rg)
	if want := wrap(renderOverview(h.st.snap, h.st.sel, 100, 24, false)); before != want {
		t.Fatalf("꺼진 상태의 화면 1 이 v0.6.2 렌더와 다름:\n%q\n%q", before, want)
	}
	if strings.Contains(before, "m 변경") {
		t.Error("꺼져 있는데 안내 줄에 m 변경")
	}
	h.keys("mM")
	if lastFrame(h.rg) != before || h.st.chg != nil || h.st.chgMenu != nil || h.st.sel.Msg != "" {
		t.Errorf("꺼져 있으면 m 은 무반응이어야 함:\n%s", h.frame())
	}
	h.keys("\r") // 화면 2
	before = lastFrame(h.rg)
	if want := wrap(renderDetail(h.st.snap, h.st.sel, 100, 24, false)); before != want || strings.Contains(before, "m 변경") {
		t.Fatalf("꺼진 상태의 화면 2 가 v0.6.2 렌더와 다름")
	}
	h.keys("m")
	if lastFrame(h.rg) != before || h.st.chgMenu != nil {
		t.Error("화면 2 에서도 m 무반응")
	}
}

// ---- 켜짐: 안내 줄 · 메뉴 항목 · 닫기 ----

func TestChangeKeyOnShowsHintAndMenu(t *testing.T) {
	five := []string{"1 IP 변경", "2 LDAP 변경", "3 IP + LDAP 변경 — IP 먼저, 실패 호스트는 LDAP 건너뜀",
		"4 원복 — 가장 최근 백업으로 (서비스 재시작 없음)", "5 auto.appl 원복 — LDAP 적용 후 대기 중인 auto.appl 반영"}
	cases := []struct {
		name            string
		ip, ldap, admin bool
		want, not       []string
	}{
		{"ip 만", true, false, false, []string{five[0], five[3]}, []string{"2 LDAP", "3 IP + LDAP", "5 auto.appl"}},
		{"ldap 만", false, true, false, []string{five[1], five[3], five[4]}, []string{"1 IP 변경", "3 IP + LDAP"}},
		{"둘 다", true, true, false, five, nil},
		{"--admin", false, false, true, append([]string{"변경 작업 (--admin)"}, five...), nil},
	}
	for _, c := range cases {
		h := chgTUISetup(t, c.ip, c.ldap, c.admin)
		over := h.frame()
		if !strings.Contains(over, "x 단독 체크  w AWX  m 변경") {
			t.Errorf("%s: 화면 1 안내 줄에 `x 단독 체크  w AWX  m 변경`:\n%s", c.name, over)
		}
		h.keys("m")
		if h.st.chgMenu == nil {
			t.Fatalf("%s: m 으로 메뉴가 열려야 함", c.name)
		}
		menu := h.frame()
		for _, s := range c.want {
			if !strings.Contains(menu, s) {
				t.Errorf("%s: 메뉴에 %q 없음:\n%s", c.name, s, menu)
			}
		}
		for _, s := range c.not {
			if strings.Contains(menu, s) {
				t.Errorf("%s: 꺼진 항목 %q 가 보임", c.name, s)
			}
		}
		if !c.admin && strings.Contains(menu, "(--admin)") {
			t.Errorf("%s: --admin 표시가 있으면 안 됨", c.name)
		}
		h.keys("z") // 목록에 없는 키는 무시
		if h.st.chgMenu == nil || h.st.chg != nil {
			t.Errorf("%s: 없는 번호는 무시", c.name)
		}
		h.keys("q")
		if h.st.chgMenu != nil || h.frame() != over {
			t.Errorf("%s: q 로 메뉴를 닫으면 화면 1 로 돌아와야 함", c.name)
		}
		h.keys("\rM") // 화면 2 에서도 (대문자 M)
		if h.st.chgMenu == nil || !strings.Contains(h.frame(), "변경 작업") {
			t.Errorf("%s: 화면 2 에서 M", c.name)
		}
		h.keys("\x1b")
		if h.st.chgMenu != nil || !strings.Contains(h.frame(), "m 변경") || !strings.Contains(h.frame(), "호스트") {
			t.Errorf("%s: Esc 로 메뉴 닫고 화면 2 로:\n%s", c.name, h.frame())
		}
	}
}

// ---- 원격 클라이언트(os6_mgmt): 켜져 있어도 안내 줄에 없고, m 은 메시지만 ----

func TestChangeKeyRemoteClient(t *testing.T) {
	for _, mode := range []string{"os8_mgmt", "src.Local()=false"} {
		h := chgTUISetup(t, true, true, false)
		if mode == "os8_mgmt" {
			os8_mgmt = "os8.test"
		} else {
			h.st.src = &fakeSrc{snap: twoGroupSnap(), local: false}
		}
		h.keys("r") // 다시 그리기
		if strings.Contains(h.frame(), "m 변경") {
			t.Errorf("%s: 원격 클라이언트의 안내 줄에는 m 변경이 없어야 함", mode)
		}
		h.keys("m")
		f := h.frame()
		if h.st.chgMenu != nil || h.st.chg != nil || !strings.Contains(f, "변경 기능은 os8_mgmt 에서 실행하세요") {
			t.Errorf("%s: 원격에서는 안내 메시지만:\n%s", mode, f)
		}
		if strings.Contains(f, "m 변경") {
			t.Errorf("%s: 안내 줄에 넣지 않음", mode)
		}
		h.keys("x") // 다른 키를 누르면 메시지는 사라진다 (x 는 os_check_sh 가 비어 안내만)
		if strings.Contains(h.frame(), "변경 기능은 os8_mgmt") {
			t.Errorf("%s: 다음 키에서 메시지가 지워져야 함", mode)
		}
	}
}

// ---- 메뉴 → 흐름 진입 → 완료 → 닫기 ----

func TestChangeMenuSelectFlowBoth(t *testing.T) {
	h := chgTUISetup(t, true, true, false)
	over := h.frame()
	h.keys("m3")
	if h.st.chg == nil || h.st.chg.kind != kindBoth || h.st.chgMenu != nil {
		t.Fatalf("3 → IP+LDAP 변경 흐름")
	}
	if f := h.frame(); !strings.Contains(f, "IP+LDAP 변경 - user 선택") {
		t.Fatalf("user 선택 화면:\n%s", f)
	}
	h.keys("user1\n1\n") // user → infra(alpha)
	if f := h.frame(); !strings.Contains(f, "대상 입력") || !strings.Contains(f, "user user1 - infra alpha") {
		t.Fatalf("대상 입력 화면:\n%s", f)
	}
	// 입력창의 m 은 글자일 뿐 메뉴를 다시 열지 않는다
	h.keys("m")
	if h.st.chgMenu != nil || string(h.st.chg.in) != "m" {
		t.Fatalf("입력 중 m 은 글자: menu=%v in=%q", h.st.chgMenu != nil, string(h.st.chg.in))
	}
	h.keys("\x7f")
	h.w.gate = make(chan struct{}) // dry-run 을 붙잡아 조회 중 상태를 확정적으로 본다
	h.keys("h1 10.1.1.5\nh2 10.1.1.6\n\x04")
	if h.st.chg.step != csDry || h.st.fastTick == nil || len(h.rg.ticks) != 1 || h.rg.ticks[0] != time.Second {
		t.Errorf("조회 중에는 1초 fast tick 하나: step=%d ticks=%v", h.st.chg.step, h.rg.ticks)
	}
	if f := h.frame(); !strings.Contains(f, "미리보기 조회(dry-run) 중") || !strings.Contains(f, "취소: Ctrl+X") {
		t.Errorf("조회 중 화면:\n%s", f)
	}
	close(h.w.gate)
	h.waitStep(csPreview)
	if f := h.frame(); !strings.Contains(f, "미리보기") || !strings.Contains(f, "yes 를 입력하면") {
		t.Fatalf("미리보기:\n%s", f)
	}
	if h.st.fastTick != nil {
		t.Error("조회가 끝나면 fast tick 정지")
	}
	h.w.gate = make(chan struct{})
	h.keys("yes\n")
	if h.st.chg.step != csRun || h.st.fastTick == nil || len(h.rg.ticks) != 2 {
		t.Errorf("실행 중 fast tick 다시 시작: step=%d ticks=%v", h.st.chg.step, h.rg.ticks)
	}
	if f := h.frame(); !strings.Contains(f, "실행 중 (2대)") {
		t.Errorf("실행 중 화면:\n%s", f)
	}
	close(h.w.gate)
	h.waitStep(csResult)
	f := h.frame()
	if !strings.Contains(f, "결과 (2대)") || !strings.Contains(f, "h1") || !strings.Contains(f, "OK") {
		t.Fatalf("결과 화면:\n%s", f)
	}
	if h.st.fastTick != nil {
		t.Error("끝나면 fast tick 정지")
	}
	h.keys("q")
	if h.st.chg != nil || h.frame() != over {
		t.Errorf("결과를 닫으면 화면 1 로 돌아와야 함:\n%s", h.frame())
	}
	// 변경 흐름에서 Esc 로 취소 → 닫힘 메시지가 메시지 줄에 보임
	h.keys("m1\x1b")
	if h.st.chg != nil {
		t.Fatal("user 단계에서 Esc 는 닫기")
	}
	h.keys("m1u1\nh1 10.1.1.5\n\x04\x1b")
	if h.st.chg != nil || !strings.Contains(h.frame(), "IP 변경 을(를) 취소했습니다") {
		t.Errorf("미리보기에서 Esc: 취소 안내가 메시지 줄에:\n%s", h.frame())
	}
}

// 진행 중 job 의 호스트면 미리보기에 빨간 경고 (job·snapshot 은 그대로)
func TestChangePreviewWarnsTrackedHost(t *testing.T) {
	h := chgTUISetup(t, true, false, false)
	h.keys("m1u1\na1 10.1.1.5\n\x04")
	f := h.frame()
	if !strings.Contains(f, "1대가 진행 중 job 에 있음") || !strings.Contains(f, "a1(J1/a.yml)") {
		t.Errorf("진행 중 job 경고:\n%s", f)
	}
}

// ---- 실행 중: q/Esc/Ctrl+C/Ctrl+Z 무시, Ctrl+X → y 취소 ----

func TestChangeRunningIgnoresCloseKeysAndCancels(t *testing.T) {
	h := chgTUISetup(t, true, false, false)
	h.w.block = true
	h.keys("m1u1\nh1 10.1.1.5\nh2 10.1.1.6\n\x04yes\n")
	if h.st.chg == nil || h.st.chg.step != csRun {
		t.Fatalf("실행 중이어야 함:\n%s", h.frame())
	}
	waitFor(t, "첫 호출", func() bool { h.w.mu.Lock(); defer h.w.mu.Unlock(); return len(h.w.calls) == 1 })
	if h.st.fastTick == nil || len(h.rg.ticks) != 1 {
		t.Errorf("실행 중 fast tick: %v", h.rg.ticks)
	}
	h.keys("q\x1bQ\x03\x1a")
	if h.st.chg == nil || !h.st.chg.running() {
		t.Fatal("q/Esc/Ctrl+C/Ctrl+Z 로 닫히면 안 됨")
	}
	f := h.frame()
	if !strings.Contains(f, "실행 중에는 닫을 수 없습니다") || !strings.Contains(f, "취소: Ctrl+X") {
		t.Errorf("무시 안내:\n%s", f)
	}
	if strings.Contains(f, "종료는 q 를 누르세요") {
		t.Error("변경 실행 중 Ctrl+C 안내는 q 종료가 아니라 Ctrl+X 여야 함")
	}
	h.keys("\x18")
	if f := h.frame(); !strings.Contains(f, "[y/n]") {
		t.Errorf("취소 확인:\n%s", f)
	}
	h.keys("n")
	if h.st.chg.cancelAsk || h.st.chg.job.wasCancelled() {
		t.Error("n 이면 계속")
	}
	h.keys("\x18y")
	h.waitStep(csResult)
	f = h.frame()
	if !strings.Contains(f, "취소됨") || !strings.Contains(f, "일부 호스트만 변경됐을 수 있음") || !strings.Contains(f, "미확인(취소)") {
		t.Errorf("취소 결과:\n%s", f)
	}
	if h.st.fastTick != nil {
		t.Error("끝나면 fast tick 정지")
	}
	h.keys("\x1b")
	if h.st.chg != nil || strings.Contains(h.frame(), "결과 (") {
		t.Error("결과 닫기")
	}
}

// 실제로 종료되는 경로(입력 EOF)에서는 실행 중인 작업을 취소하고 기록까지 남긴 뒤 끝난다
func TestChangeShutdownOnLoopExit(t *testing.T) {
	h := chgTUISetup(t, true, false, false)
	h.w.block = true
	env, r := newRig(strings.NewReader("m1u1\nh1 10.1.1.5\n\x04yes\n"))
	done := make(chan int, 1)
	go func() { done <- runTUILoop(&fakeSrc{snap: twoGroupSnap(), local: true}, env) }()
	select {
	case code := <-done:
		if code != 0 {
			t.Fatalf("exit=%d", code)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("종료하지 않음 (실행 중 작업이 취소되지 않음)")
	}
	assertRestored(t, r)
	recs, _ := filepath.Glob(filepath.Join(h.dir, "state", "changes", "u1", "*.json"))
	if len(recs) != 1 {
		t.Fatalf("종료 시 취소 기록 1건: %v", recs)
	}
	if rec := readChgRecord(t, recs[0]); !rec.Cancelled || rec.End == "" {
		t.Errorf("기록: %+v", rec)
	}
}

// ---- 안내 줄·도움말 ----

func TestChangeHintStyleAndHelp(t *testing.T) {
	on := renderOverview(sampleNormal(), selection{Change: true}, 120, 24, true)
	if !strings.Contains(on, "\x1b[1;38;5;208mm 변경\x1b[0m") {
		t.Errorf("m 변경 색(주황): %q", on)
	}
	for _, c := range []string{"1;36", stRed, "1;32", "1;33", "1;35", "1;34"} {
		if hintStyles["m"] == c {
			t.Errorf("m 색이 기존 색 %s 와 겹침", c)
		}
	}
	off := renderOverview(sampleNormal(), selection{}, 120, 24, true)
	if strings.Contains(off, "m 변경") {
		t.Error("Change=false 면 안내 줄에 없음")
	}
	// 도움말: m 이 꺼져 있어도 설명 + 켜는 법 한 줄
	h := stripANSI(renderHelp(120, 60, false))
	for _, s := range []string{"   m  ", "변경 작업 메뉴", "미리보기 → yes 입력 → 실행 → 결과", "서비스는 재시작하지 않음", "Ctrl+X → y",
		"conf enable_ip_change/enable_ldap_change 또는 --admin 으로 켭니다"} {
		if !strings.Contains(h, s) {
			t.Errorf("도움말에 %q 없음:\n%s", s, h)
		}
	}
}

// 안내 줄에 m 변경을 더해도 폭을 넘지 않고(…·잘림 없음) 항상 보인다
func TestChangeHintWrapsNoTruncation(t *testing.T) {
	for _, s := range []Snapshot{sampleNormal(), sampleStuck(), sampleKorean(), sampleLong()} {
		refs := flatGroups(s)
		for _, w := range []int{40, 60, 80, 120, 200} {
			for _, color := range []bool{false, true} {
				outs := []string{renderOverview(s, selection{Change: true}, w, 30, color)}
				if len(refs) > 0 {
					j := s.Jobs[refs[0].Job]
					sel := selection{Change: true, Detail: true, JobID: j.ID, Yml: j.Groups[refs[0].Grp].Yml}
					outs = append(outs, renderDetail(s, sel, w, 30, color))
					sel.Yml = allView
					outs = append(outs, renderDetail(s, sel, w, 30, color))
				}
				for _, o := range outs {
					p := stripANSI(o)
					if !strings.Contains(p, "m 변경") {
						t.Errorf("w=%d color=%v: 안내 줄에 m 변경이 없음:\n%s", w, color, p)
					}
					for _, l := range strings.Split(p, "\n") {
						if strWidth(l) > w {
							t.Errorf("w=%d 폭 초과 %d: %q", w, strWidth(l), l)
						}
					}
				}
			}
		}
	}
}

// ---- 메뉴 화면 레이아웃 ----

var chgMenuAll = []changeMenuItem{{'1', kindIP, ""}, {'2', kindLDAP, ""}, {'3', kindBoth, ""}, {'4', kindRollback, ""}, {'5', kindApplRestore, ""}}

func TestChangeMenuRenderWrapNoTruncation(t *testing.T) {
	for _, admin := range []bool{false, true} {
		old := adminMode
		adminMode = admin
		for _, w := range []int{60, 80, 120} {
			for _, color := range []bool{false, true} {
				for _, hgt := range []int{0, 24} {
					out := renderChangeMenu(chgMenuAll, w, hgt, color)
					lines := strings.Split(out, "\n")
					if hgt > 0 && len(lines) != hgt {
						t.Errorf("w=%d 높이 %d: %d 줄", w, hgt, len(lines))
					}
					for i, l := range lines {
						p := stripANSI(l)
						if strWidth(p) > w || strings.Contains(p, "…") {
							t.Errorf("w=%d color=%v 줄 %d 폭 초과·잘림: %q", w, color, i, p)
						}
					}
					if !color && strings.Contains(out, "\x1b") {
						t.Error("색 끄면 ANSI 없음")
					}
					plain := strings.Join(strings.Fields(stripANSI(out)), "")
					want := []string{"변경작업", "설정변경", "미리보기→yes확인", "1IP변경—", "2LDAP변경—", "3IP+LDAP변경—IP먼저,실패호스트는LDAP건너뜀",
						"4원복—가장최근백업으로(서비스재시작없음)", "5auto.appl원복—LDAP적용후대기중인auto.appl반영", "번호선택", "Esc/q닫기",
						"hostname과새IP입력—ifcfg만바꾸고네트워크는재시작하지않음", "dry-run으로바뀔내용을먼저확인"}
					if admin {
						want = append(want, "(--admin)")
					}
					for _, s := range want {
						if !strings.Contains(plain, s) {
							t.Errorf("w=%d color=%v admin=%v: %q 없음:\n%s", w, color, admin, s, stripANSI(out))
						}
					}
					if (strings.Contains(plain, "(--admin)")) != admin {
						t.Errorf("w=%d: --admin 표시 불일치", w)
					}
					if color && !strings.Contains(out, "\x1b[1;31m설정") {
						t.Errorf("w=%d: `설정 변경` 은 빨강", w)
					}
				}
			}
		}
		adminMode = old
	}
}

func TestChangeMenuRenderGolden(t *testing.T) {
	for _, color := range []bool{false, true} {
		mode := "plain"
		if color {
			mode = "color"
		}
		oldAdmin := adminMode
		var sb strings.Builder
		for _, c := range []struct {
			name  string
			items []changeMenuItem
			admin bool
		}{{"all", chgMenuAll, false}, {"ip-only", chgMenuAll[:1], false}, {"admin", chgMenuAll, true}} {
			adminMode = c.admin
			sb.WriteString("== " + c.name + " ==\n" + renderChangeMenu(c.items, 60, 24, color) + "\n")
		}
		adminMode = oldAdmin
		got := goldenText(sb.String())
		name := "tui_changemenu_" + mode + "_60.golden"
		path := filepath.Join("testdata", name)
		if *updateGolden {
			if err := os.WriteFile(path, []byte(got), 0644); err != nil {
				t.Fatal(err)
			}
			continue
		}
		want, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("골든 없음 %s (go test -run ChangeMenuRenderGolden -update): %v", name, err)
		}
		if string(want) != got {
			t.Errorf("%s 불일치\n--- got ---\n%s", name, got)
		}
	}
}
