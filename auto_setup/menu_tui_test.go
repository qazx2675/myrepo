// menu_tui_test.go - v0.8.0 항목별 인라인 메뉴 h(호스트 작업)·w(AWX), s 키 hook, 새 안내 줄·도움말, 골든.
// 메뉴 열기/선택/닫기, 숨은 단축키(c t g v d) 유지, w 의 ww / w Enter / w 1 = 예전 AWX 실행, 열려 있는 동안 다른 키 무시(m 메뉴와 같은 규칙).
package main

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

const (
	hostMenuText = "호스트 작업 ▶ c 체크+수정  t 체크만  g 재확인  d 수동 완료  v 결과   (Esc 닫기)"
	awxMenuText  = "AWX ▶ 1 AWX 실행  2 신규 설치 양식   (Esc 닫기)"
	hintTail1    = "w AWX·신규설치  m 변경  s gossh 작업  x 단독 체크"
	hintTail2    = "h 호스트 작업  w AWX·신규설치  m 변경  s gossh 작업  x 단독 체크"
	msgNoAwxDir  = "awx_dir 가 비었거나"
	msgNewhost   = newhostNoDirMsg // awx_dir 가 비어 있어 시작 시 안내하고 끝남
)

// menuTUI: m(변경) 도 켜 둔 TUI (m 과의 배타·안내 줄의 m 변경 확인용). 화면 2(a.yml)로 들어가려면 h.enter.
func menuTUI(t *testing.T) *chgTUI { return chgTUISetup(t, true, true, false) }

// lastLines: 마지막 n 줄 (화면 맨 아래)
func lastLines(frame string, n int) []string {
	ls := strings.Split(strings.TrimRight(frame, "\n"), "\n")
	if len(ls) < n {
		return ls
	}
	return ls[len(ls)-n:]
}

// ---- h 메뉴 ----

func TestHostMenuOpenCloseIdentical(t *testing.T) {
	h := menuTUI(t)
	h.keys("h")
	if h.st.menu != nil {
		t.Fatal("화면 1 에서 h 는 무반응이어야 함")
	}
	h.enter("a.yml", "a2")
	before := h.frame()
	if !strings.Contains(before, hintTail2) {
		t.Fatalf("화면 2 안내 줄:\n%s", before)
	}
	for _, closeKey := range []string{"\x1b", "h", "H", "q"} {
		h.keys("h")
		if h.st.menu == nil || h.st.menu.Open != 'h' {
			t.Fatalf("h 로 메뉴가 열려야 함")
		}
		f := h.frame()
		if !strings.Contains(f, hostMenuText) {
			t.Fatalf("메뉴 줄:\n%s", f)
		}
		// 인라인: 화면 2 가 그대로 있고(호스트표) 메뉴 줄은 키 안내 줄 바로 아래(맨 아래)
		if !strings.Contains(f, "a2") || strings.TrimSpace(lastLines(f, 1)[0]) != hostMenuText {
			t.Fatalf("메뉴는 화면 2 맨 아래에 붙어야 함:\n%s", f)
		}
		if h.handleKeyQuits(closeKey) || h.st.menu != nil {
			t.Fatalf("%q 는 메뉴만 닫고 TUI 는 종료하지 않아야 함", closeKey)
		}
		if h.frame() != before {
			t.Errorf("%q 로 닫은 화면이 열기 전과 다름:\n%s", closeKey, h.frame())
		}
	}
}

// 글자·번호 모두 같은 항목: 기존 핸들러를 그대로 호출한다 (c/1 t/2 확인 단계, g/3 재확인 요청, d/4 수동 완료 프롬프트, v/5 결과)
func TestHostMenuSelectLettersAndNumbers(t *testing.T) {
	type check struct {
		keys []string
		ok   func(h *chgTUI) bool
		what string
	}
	cases := []check{
		{[]string{"hc", "h1", "hC"}, func(h *chgTUI) bool { return h.st.sel.Confirm && h.st.sel.Mode == "" }, "c 체크+수정 확인 단계"},
		{[]string{"ht", "h2", "hT"}, func(h *chgTUI) bool { return h.st.sel.Confirm && h.st.sel.Mode == ModeCheck }, "t 체크만 확인 단계"},
		{[]string{"hg", "h3", "hG"}, func(h *chgTUI) bool {
			src := h.st.src.(*fakeSrc)
			return h.st.view != nil && h.st.view.kind == viewRecheck && src.reqs[len(src.reqs)-1].kind == ReqRecheck
		}, "g 재확인 요청"},
		{[]string{"hd", "h4", "hD"}, func(h *chgTUI) bool { return h.st.done != nil }, "d 수동 완료 프롬프트"},
		{[]string{"hv", "h5", "hV"}, func(h *chgTUI) bool {
			return strings.Contains(h.st.sel.Msg, "수동 실행 결과가 아직 없습니다")
		}, "v 결과 안내"},
	}
	for _, c := range cases {
		for _, ks := range c.keys {
			h := menuTUI(t)
			h.enter("a.yml", "a2")
			h.keys(ks)
			if h.st.menu != nil {
				t.Errorf("%s(%q): 선택하면 메뉴는 닫혀야 함", c.what, ks)
			}
			if !c.ok(h) {
				t.Errorf("%s(%q) 실패:\n%s", c.what, ks, h.frame())
			}
		}
	}
}

// 숨은 단축키: 메뉴 없이 c t g v d 를 바로 눌러도 예전처럼 동작 (같은 결과)
func TestHiddenShortcutsStillWork(t *testing.T) {
	for _, k := range []string{"c", "t", "g", "d", "v"} {
		viaMenu, direct := menuTUI(t), menuTUI(t)
		viaMenu.enter("a.yml", "a2")
		direct.enter("a.yml", "a2")
		viaMenu.keys("h" + k)
		direct.keys(k)
		if direct.st.menu != nil {
			t.Errorf("%s: 직접 누르면 메뉴가 열리면 안 됨", k)
		}
		if viaMenu.frame() != direct.frame() {
			t.Errorf("%s: 메뉴로 고른 결과와 직접 누른 결과가 다름\n--- 메뉴 ---\n%s\n--- 직접 ---\n%s", k, viaMenu.frame(), direct.frame())
		}
	}
}

// 열려 있는 동안 다른 키는 무시(방향키·Enter·Ctrl+C·m·x·w·s·? 등) — 화면도 바뀌지 않는다. 없는 번호도 무시
func TestHostMenuIgnoresOtherKeys(t *testing.T) {
	h := menuTUI(t)
	h.enter("a.yml", "a2")
	h.keys("h")
	open := h.frame()
	for _, k := range []string{"z", "\x1b[D", "\x1b[A", "\x1b[B", "\r", "x", "X", "w", "s", "m", "a", "f", "r", "6", "0", "\x03", "\x1a", "?"} {
		h.keys(k)
		if h.st.menu == nil || h.st.chgMenu != nil || h.st.chg != nil || h.st.oo != nil || h.st.awx != nil || h.st.done != nil || h.st.help ||
			h.st.sel.Confirm || h.st.view != nil || !h.st.sel.Detail || h.st.sel.Yml != "a.yml" {
			t.Fatalf("메뉴가 열린 동안 %q 는 무시여야 함", k)
		}
	}
	if h.frame() != open {
		t.Errorf("무시한 키로 화면이 바뀜:\n%s", h.frame())
	}
}

// 전체 보기에서는 기존에도 안 되던 c t v 를 메뉴에서 빼고 g d 만. 숨은 단축키는 예전 안내 그대로
func TestHostMenuAllView(t *testing.T) {
	h := menuTUI(t)
	h.enter("a.yml", "a2")
	h.keys("a") // 전체 보기
	if h.st.sel.Yml != allView {
		t.Fatal("전체 보기가 아님")
	}
	h.keys("h")
	f := h.frame()
	if !strings.Contains(f, "호스트 작업 ▶ g 재확인  d 수동 완료   (Esc 닫기)") || strings.Contains(f, "c 체크+수정  t") || strings.Contains(f, "v 결과") {
		t.Fatalf("전체 보기 메뉴:\n%s", f)
	}
	h.keys("c1") // 메뉴에 없는 항목은 무시, 메뉴 유지
	if h.st.menu == nil || h.st.sel.Confirm {
		t.Errorf("전체 보기 메뉴에서 c 는 무시여야 함")
	}
	h.keys("g")
	if h.st.menu != nil || h.st.view == nil || h.st.view.kind != viewRecheck {
		t.Errorf("전체 보기 메뉴에서 g 는 재확인:\n%s", h.frame())
	}
	h2 := menuTUI(t)
	h2.enter("a.yml", "a2")
	h2.keys("ac") // 숨은 단축키 c: 기존 안내
	if !strings.Contains(h2.frame(), "전체 보기에서는 수동 실행을 할 수 없습니다") {
		t.Errorf("전체 보기의 c 안내:\n%s", h2.frame())
	}
}

// 메뉴가 열린 채 그룹이 사라져 화면 1 로 돌아가면 h 메뉴도 함께 닫힌다
func TestHostMenuClosesWhenGroupGone(t *testing.T) {
	h := menuTUI(t)
	h.enter("a.yml", "a2")
	h.keys("h")
	h.st.src.(*fakeSrc).snap = tsSnap(true)
	h.st.refresh()
	h.st.draw()
	if h.st.sel.Detail || h.st.menu != nil {
		t.Errorf("그룹이 사라지면 화면 1 + 메뉴 닫힘: detail=%v menu=%v", h.st.sel.Detail, h.st.menu != nil)
	}
}

// ---- w 메뉴 ----

func TestAwxMenuOpenClose(t *testing.T) {
	h := menuTUI(t)
	before := h.frame()
	if !strings.Contains(before, hintTail1) || strings.Contains(before, "h 호스트 작업") {
		t.Fatalf("화면 1 안내 줄 (h 없음):\n%s", before)
	}
	for _, scr := range []string{"화면 1", "화면 2"} {
		if scr == "화면 2" {
			h.enter("a.yml", "a2")
			before = h.frame()
		}
		for _, closeKey := range []string{"\x1b", "q", "Q"} {
			h.keys("w")
			if h.st.menu == nil || h.st.menu.Open != 'w' {
				t.Fatalf("%s: w 로 메뉴가 열려야 함", scr)
			}
			f := h.frame()
			if !strings.Contains(f, awxMenuText) || strings.Contains(f, "킥스타트") {
				t.Fatalf("%s: AWX 메뉴 줄:\n%s", scr, f)
			}
			if strings.TrimSpace(lastLines(f, 1)[0]) != awxMenuText {
				t.Fatalf("%s: 메뉴는 맨 아래:\n%s", scr, f)
			}
			if h.handleKeyQuits(closeKey) || h.st.menu != nil || h.st.awx != nil || h.st.oo != nil {
				t.Fatalf("%s: %q 는 메뉴만 닫아야 함", scr, closeKey)
			}
			if h.frame() != before {
				t.Errorf("%s: %q 로 닫은 화면이 열기 전과 다름", scr, closeKey)
			}
		}
	}
}

// ww / w Enter / w 1 = 예전의 w (startAwx), w 2 = 신규 설치 양식 hook
func TestAwxMenuSelect(t *testing.T) {
	for _, scr := range []string{"화면 1", "화면 2"} {
		for _, ks := range []string{"ww", "wW", "w\r", "w1", "WW"} {
			h := menuTUI(t) // awx_dir 가 비어 있어 startAwx 는 안내만 낸다
			if scr == "화면 2" {
				h.enter("a.yml", "a2")
			}
			h.keys(ks)
			if h.st.menu != nil || !strings.Contains(h.st.sel.Msg, msgNoAwxDir) {
				t.Errorf("%s %q: startAwx 안내 기대, msg=%q", scr, ks, h.st.sel.Msg)
			}
		}
		h := menuTUI(t)
		if scr == "화면 2" {
			h.enter("a.yml", "a2")
		}
		h.keys("w2")
		if h.st.menu != nil || h.st.sel.Msg != msgNewhost || h.st.awx != nil || h.st.oo != nil {
			t.Errorf("%s w 2: startNewhost hook 기대, msg=%q", scr, h.st.sel.Msg)
		}
		if !strings.Contains(h.frame(), msgNewhost) {
			t.Errorf("%s: 준비 중 안내가 화면에 없음", scr)
		}
		h.keys("w3") // 3번(킥스타트)은 자리만 — 항목 없음, 무시하고 메뉴 유지
		if h.st.menu == nil || h.st.sel.Msg != "" {
			t.Errorf("%s w 3: 무시여야 함", scr)
		}
	}
}

// 실제로 AWX 실행 흐름이 열린다 (awx_dir 와 01 이 있을 때): ww → user 선택 단계(st.oo)
func TestAwxMenuStartsAwxFlow(t *testing.T) {
	for _, ks := range []string{"ww", "w\r", "w1"} {
		h := menuTUI(t)
		dir := t.TempDir()
		os.WriteFile(filepath.Join(dir, awxScript), []byte("#!/bin/bash\n"), 0755)
		awx_dir = dir // chgTUISetup 의 Cleanup 이 되돌린다
		setProfiles(t)
		h.keys(ks)
		if h.st.menu != nil || h.st.oo == nil {
			t.Errorf("%q: AWX 흐름(user 선택)이 열려야 함, msg=%q", ks, h.st.sel.Msg)
		}
	}
}

// 열려 있는 동안 다른 키는 무시 (w 메뉴): h/s/m/x/방향키/없는 번호/Ctrl+C
func TestAwxMenuIgnoresOtherKeys(t *testing.T) {
	h := menuTUI(t)
	h.keys("w")
	open := h.frame()
	for _, k := range []string{"z", "\x1b[D", "\x1b[A", "h", "s", "m", "x", "d", "a", "r", "3", "0", "\x03", "\x1a", "?"} {
		h.keys(k)
		if h.st.menu == nil || h.st.chgMenu != nil || h.st.oo != nil || h.st.awx != nil || h.st.help || h.st.sel.Msg != "" {
			t.Fatalf("메뉴가 열린 동안 %q 는 무시여야 함 (msg=%q)", k, h.st.sel.Msg)
		}
	}
	if h.frame() != open {
		t.Errorf("무시한 키로 화면이 바뀜")
	}
}

// m 메뉴가 열려 있으면 h/w/s 는 m 규칙대로 무시, h/w 메뉴가 열려 있으면 m 은 무시 (동시에 열리지 않음)
func TestMenusMutuallyExclusive(t *testing.T) {
	h := menuTUI(t)
	h.enter("a.yml", "a2")
	h.keys("m")
	h.keys("hws")
	if h.st.chgMenu == nil || h.st.menu != nil {
		t.Fatal("m 메뉴가 열려 있는 동안 h/w/s 는 무시여야 함")
	}
	h.keys("\x1b")
	h.keys("h")
	h.keys("m")
	if h.st.chgMenu != nil || h.st.menu == nil || strings.Contains(h.frame(), "변경 ▶") {
		t.Fatal("h 메뉴가 열려 있는 동안 m 은 무시여야 함")
	}
}

// ---- s 키 hook ----

func TestWorkKeyHook(t *testing.T) {
	h := menuTUI(t)
	for _, scr := range []string{"화면 1", "화면 2"} {
		if scr == "화면 2" {
			h.enter("a.yml", "a2")
		}
		for _, k := range []string{"s", "S"} {
			h.st.sel.Msg = ""
			h.keys(k) // -user 없음 → 세션 user 선택 단계가 먼저 (startWork → needSessionUser)
			if h.st.menu != nil || h.st.oo == nil || h.st.oo.onUser == nil || h.st.wk != nil {
				t.Errorf("%s %q: startWork hook(세션 user 선택) 기대", scr, k)
			}
			h.keys("\x1b")
			if h.st.oo != nil || h.st.wk != nil {
				t.Errorf("%s %q: Esc 로 취소", scr, k)
			}
		}
	}
}

// -user(forceUser) 로 실행해도 메뉴·hook 이 똑같이 동작
func TestMenusWithForceUser(t *testing.T) {
	saveSessionState(t)
	forceUser = "alice"
	h := menuTUI(t)
	h.keys("w2")
	if h.st.sel.Msg != msgNewhost {
		t.Errorf("-user: w 2 msg=%q", h.st.sel.Msg)
	}
	h.keys("s")
	if h.st.wk == nil || h.st.wk.user != "alice" || !strings.Contains(h.frame(), "[user: alice] gossh 작업") {
		t.Errorf("-user: s 는 바로 gossh 작업 화면")
	}
	h.keys("q")
	h.keys("ww")
	if !strings.Contains(h.st.sel.Msg, msgNoAwxDir) {
		t.Errorf("-user: ww msg=%q", h.st.sel.Msg)
	}
	h.enter("a.yml", "a2")
	h.keys("hc")
	if !h.st.sel.Confirm || h.st.menu != nil {
		t.Errorf("-user: h c 확인 단계 기대")
	}
}

// ---- 안내 줄 ----

func TestMenuHintLines(t *testing.T) {
	s := sampleNormal()
	j := s.Jobs[0]
	for _, change := range []bool{false, true} {
		over := stripANSI(renderOverview(s, selection{Change: change, DateFilter: true}, 120, 30, false))
		det := stripANSI(renderDetail(s, selection{Change: change, Detail: true, JobID: j.ID, Yml: j.Groups[0].Yml}, 120, 30, false))
		all := stripANSI(renderDetail(s, selection{Change: change, Detail: true, JobID: j.ID, Yml: allView}, 120, 30, false))
		tail1, tail2 := hintTail1, hintTail2
		if !change {
			tail1 = strings.Replace(tail1, "m 변경  ", "", 1)
			tail2 = strings.Replace(tail2, "m 변경  ", "", 1)
		}
		for name, c := range map[string]struct{ out, want, not string }{
			"화면 1":    {over, tail1, "h 호스트 작업"},
			"화면 2":    {det, tail2, ""},
			"화면 2 전체": {all, tail2, ""},
		} {
			if !strings.Contains(c.out, c.want) {
				t.Errorf("change=%v %s 안내 줄에 %q 없음:\n%s", change, name, c.want, c.out)
			}
			if c.not != "" && strings.Contains(c.out, c.not) {
				t.Errorf("change=%v %s 에 %q 가 있으면 안 됨", change, name, c.not)
			}
			for _, old := range []string{"c 체크+수정", "t 체크만", "g 재확인", "d 수동 완료", "v 결과"} { // 숨은 단축키는 안내 줄에서 빠졌다
				if strings.Contains(c.out, old) {
					t.Errorf("%s 안내 줄에 예전 항목 %q", name, old)
				}
			}
		}
		if !strings.Contains(det, "a 전체 그룹") || !strings.Contains(all, "a 그룹별 보기") {
			t.Error("첫 줄(보기·이동)은 기존 그대로")
		}
	}
	// 색: h 보라, s 연초록, w 파랑, m 주황, x 자홍
	col := renderDetail(s, selection{Change: true, Detail: true, JobID: j.ID, Yml: j.Groups[0].Yml}, 120, 30, true)
	for _, want := range []string{"\x1b[" + hintStyles["h"] + "mh 호스트 작업\x1b[0m", "\x1b[" + hintStyles["s"] + "ms gossh 작업\x1b[0m", "\x1b[1;34mw AWX·신규설치\x1b[0m"} {
		if !strings.Contains(col, want) {
			t.Errorf("색 항목 %q 없음", want)
		}
	}
	seen := map[string]string{}
	for k, v := range hintStyles {
		if o, dup := seen[v]; dup && v != "1;36" { // 이동 키(↑↓ ←→ ← Enter)만 같은 색을 공유
			t.Errorf("hintStyles %q 와 %q 색 %s 중복", k, o, v)
		}
		seen[v] = k
	}
}

// 안내 줄·메뉴 줄은 어떤 폭에서도 폭을 넘지 않고 항목을 잃지 않으며 … 로 자르지 않는다
func TestMenuHintAndMenuLinesWrap(t *testing.T) {
	menus := map[string]*inlineMenu{"h": hostMenu(false), "h전체": hostMenu(true), "w": awxMenu()}
	for _, s := range []Snapshot{sampleNormal(), sampleKorean(), sampleLong()} {
		refs := flatGroups(s)
		if len(refs) == 0 {
			continue
		}
		j := s.Jobs[refs[0].Job]
		for _, w := range []int{40, 60, 80, 120, 200} {
			for _, color := range []bool{false, true} {
				outs := []string{renderOverview(s, selection{Change: true, DateFilter: true}, w, 30, color)}
				for _, yml := range []string{j.Groups[refs[0].Grp].Yml, allView} {
					outs = append(outs, renderDetail(s, selection{Change: true, Detail: true, JobID: j.ID, Yml: yml}, w, 30, color))
				}
				for oi, o := range outs {
					p := stripANSI(o)
					for _, l := range strings.Split(p, "\n") {
						if strWidth(l) > w {
							t.Errorf("w=%d 폭 초과 %d: %q", w, strWidth(l), l)
						}
					}
					sq := squash(p)
					items := []string{"w AWX·신규설치", "m 변경", "s gossh 작업", "x 단독 체크"}
					if oi > 0 {
						items = append(items, "h 호스트 작업")
					}
					for _, it := range items {
						if !strings.Contains(sq, squash(it)) {
							t.Errorf("w=%d color=%v 화면 %d: 안내 항목 %q 사라짐", w, color, oi, it)
						}
					}
				}
				for name, m := range menus {
					lines := menuLines(m, w, color)
					for _, l := range lines {
						p := stripANSI(l)
						if strWidth(p) > w || strings.Contains(p, "…") {
							t.Errorf("w=%d %s 메뉴 줄 폭 초과·잘림: %q", w, name, p)
						}
					}
					out := strings.Join(lines, "\n")
					if !color && strings.Contains(out, "\x1b") {
						t.Errorf("%s: 색 끄면 ANSI 없음", name)
					}
					plain := squash(stripANSI(out))
					for _, it := range m.Items {
						if !strings.Contains(plain, squash(string(it.Key)+" "+it.Name)) {
							t.Errorf("w=%d %s: 항목 %q 사라짐: %s", w, name, it.Name, plain)
						}
					}
					if !strings.Contains(plain, squash(m.Label+"▶")) || !strings.Contains(plain, "(Esc닫기)") {
						t.Errorf("w=%d %s: 이름·닫기 안내 사라짐: %s", w, name, plain)
					}
				}
			}
		}
	}
}

// 메뉴가 닫혀 있으면 화면은 열기 전과 같고, 열면 메뉴 줄이 맨 아래에 붙으며 높이를 넘지 않는다
func TestMenuInlineHeight(t *testing.T) {
	s := sampleNormal()
	j := s.Jobs[0]
	for _, w := range []int{40, 60, 80, 120} {
		sel := selection{Change: true, Detail: true, JobID: j.ID, Yml: j.Groups[0].Yml}
		sel.Inline = menuLines(hostMenu(false), w, false)
		open := strings.Split(renderDetail(s, sel, w, 24, false), "\n")
		if len(open) > 24 {
			t.Errorf("w=%d: 열면 높이 24 이하, 실제 %d", w, len(open))
		}
		if strings.Join(open[len(open)-len(sel.Inline):], "\n") != strings.Join(sel.Inline, "\n") {
			t.Errorf("w=%d: 메뉴 줄이 맨 아래에 와야 함", w)
		}
	}
}

// ---- 도움말 ----

func TestHelpMenuStructure(t *testing.T) {
	h := squash(stripANSI(renderHelp(120, 90, false)))
	for _, s := range []string{
		"항목별메뉴", "h호스트작업메뉴", "c(1)체크+수정", "t(2)체크만", "g(3)재확인", "d(4)수동완료", "v(5)결과",
		"wAWX메뉴", "1AWX실행", "ww가예전의w", "2신규설치양식", "m변경작업메뉴", "sgossh작업", "x체크스크립트단독실행",
		"c t g v d 는 h 메뉴 없이", "바로눌러도동작", "Esc/q로메뉴만닫기",
	} {
		if !strings.Contains(h, squash(s)) {
			t.Errorf("도움말에 %q 없음", s)
		}
	}
}

// ---- 골든 (폭 60/80/120 × plain/color): 안내 줄, h·w 메뉴가 열린 화면 1·2 ----

func TestMenusGolden(t *testing.T) {
	s := sampleNormal()
	j := s.Jobs[0]
	yml := j.Groups[0].Yml
	for _, w := range []int{60, 80, 120} {
		for _, color := range []bool{false, true} {
			mode := "plain"
			if color {
				mode = "color"
			}
			var sb strings.Builder
			add := func(name, out string) { sb.WriteString("== " + name + " ==\n" + out + "\n") }
			over := selection{Change: true, DateFilter: true}
			det := selection{Change: true, Detail: true, JobID: j.ID, Yml: yml}
			all := selection{Change: true, Detail: true, JobID: j.ID, Yml: allView}
			add("overview", renderOverview(s, over, w, 24, color))
			over.Inline = menuLines(awxMenu(), w, color)
			add("overview-wmenu", renderOverview(s, over, w, 24, color))
			add("overview-nochange", renderOverview(s, selection{DateFilter: true}, w, 24, color))
			add("detail", renderDetail(s, det, w, 24, color))
			det.Inline = menuLines(hostMenu(false), w, color)
			add("detail-hmenu", renderDetail(s, det, w, 24, color))
			det.Inline = menuLines(awxMenu(), w, color)
			add("detail-wmenu", renderDetail(s, det, w, 24, color))
			all.Inline = menuLines(hostMenu(true), w, color)
			add("allview-hmenu", renderDetail(s, all, w, 24, color))
			got := goldenText(sb.String())
			name := "tui_menus_" + mode + "_" + strconv.Itoa(w) + ".golden"
			path := filepath.Join("testdata", name)
			if *updateGolden {
				if err := os.WriteFile(path, []byte(got), 0644); err != nil {
					t.Fatal(err)
				}
				continue
			}
			want, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("골든 없음 %s (go test -run MenusGolden -update): %v", name, err)
			}
			if string(want) != got {
				t.Errorf("%s 불일치\n--- got ---\n%s", name, got)
			}
		}
	}
}
