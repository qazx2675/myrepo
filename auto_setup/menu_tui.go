// menu_tui.go - v0.8.0 항목별 인라인 메뉴: h(호스트 작업, 화면 2) · w(AWX, 화면 1·2). m 변경 메뉴(change_tui.go)와 같은 방식 —
// 화면을 바꾸지 않고 키 안내 줄 바로 아래에 한 줄(좁으면 여러 줄)로 열리고(selection.Inline), 글자 또는 번호로 고르며,
// Esc/q(와 h 메뉴는 h)로 메뉴만 닫는다. 그 밖의 키는 무시한다.
//
// 기존 단축키 c t g v d 는 그대로 동작한다(메뉴 안 항목은 같은 글자 → 같은 핸들러 handleDetail 을 호출).
// 메뉴에 없는 s(gossh 작업)·신규 설치 양식은 이 파일의 hook 함수(startWork, startNewhost)가 연결 지점이다.
package main

// menuItem: 메뉴 한 항목. Key = 선택 글자(기존 단축키와 같은 글자 또는 번호), Alt = 번호로도 고를 수 있으면 그 번호, Run = 선택 시 동작
type menuItem struct {
	Key  rune
	Alt  rune
	Name string
	Run  func(st *tuiState)
}

// inlineMenu: 열려 있는 항목별 메뉴
type inlineMenu struct {
	Open    rune   // 메뉴를 연 키 ('h' 또는 'w') — 안내 줄 색과 닫기 키
	Label   string // 메뉴 이름 (줄 맨 앞)
	Items   []menuItem
	Default rune // Enter 와 연 키를 다시 눌렀을 때 고르는 항목의 Key (0 = 연 키는 메뉴 닫기, Enter 는 무시)
}

// lowerKey: 영문 대문자를 소문자로 (메뉴 글자는 대소문자 구분 없음)
func lowerKey(r rune) rune {
	if r >= 'A' && r <= 'Z' {
		return r + 'a' - 'A'
	}
	return r
}

// hostKey: h 메뉴 항목 — 선택하면 같은 글자의 숨은 단축키 핸들러(handleDetail)를 그대로 호출한다
func hostKey(key, alt rune, name string) menuItem {
	return menuItem{Key: key, Alt: alt, Name: name, Run: func(st *tuiState) { st.handleDetail(keyEv{k: kRune, r: key}) }}
}

// hostMenu: h 메뉴 — ` 호스트 작업 ▶ c 체크+수정  t 체크만  g 재확인  d 수동 완료  v 결과   (Esc 닫기)`.
// 전체 보기(allView)에서는 기존에도 안 되던 c t v 는 표시하지 않는다(눌러도 같은 안내가 나오는 숨은 단축키는 그대로).
func hostMenu(allViewSel bool) *inlineMenu {
	var items []menuItem
	if !allViewSel {
		items = append(items, hostKey('c', '1', "체크+수정"), hostKey('t', '2', "체크만"))
	}
	items = append(items, hostKey('g', '3', "재확인"), hostKey('d', '4', "수동 완료"))
	if !allViewSel {
		items = append(items, hostKey('v', '5', "결과"))
	}
	return &inlineMenu{Open: 'h', Label: "호스트 작업", Items: items}
}

// awxMenu: w 메뉴 — ` AWX ▶ 1 AWX 실행  2 신규 설치 양식   (Esc 닫기)`. w / Enter / 1 = AWX 실행(예전 w).
// 3번 자리: (예정) 킥스타트 파티션 변경 — 이번 범위가 아니므로 항목을 만들지 않는다 (수정계획서 §5-2).
func awxMenu() *inlineMenu {
	return &inlineMenu{Open: 'w', Label: "AWX", Default: '1', Items: []menuItem{
		{Key: '1', Name: "AWX 실행", Run: (*tuiState).startAwx},
		{Key: '2', Name: "신규 설치 양식", Run: (*tuiState).startNewhost},
	}}
}

// openMenu: h / w 키 — 메뉴를 연다 (h 는 화면 2 에서만)
func (st *tuiState) openMenu(open rune) {
	switch open {
	case 'h':
		if st.sel.Detail {
			st.menu = hostMenu(st.sel.Yml == allView)
		}
	case 'w':
		st.menu = awxMenu()
	}
}

// handleMenu: 메뉴가 열려 있을 때의 키 — 글자/번호 선택, Esc/q(h 메뉴는 h 도) 닫기, Enter·w 는 w 메뉴의 기본 항목, 나머지는 무시
func (st *tuiState) handleMenu(ev keyEv) {
	m := st.menu
	r := lowerKey(ev.r)
	switch {
	case ev.k == kEsc || (ev.k == kRune && (r == 'q' || (r == m.Open && m.Default == 0))):
		st.menu = nil
	case m.Default != 0 && (ev.k == kEnter || (ev.k == kRune && r == m.Open)):
		st.pickMenu(m.Default)
	case ev.k == kRune:
		st.pickMenu(r)
	}
}

// pickMenu: 글자 또는 번호에 맞는 항목이 있으면 메뉴를 닫고 실행 (없으면 메뉴 유지)
func (st *tuiState) pickMenu(key rune) {
	for _, it := range st.menu.Items {
		if it.Key == key || (it.Alt != 0 && it.Alt == key) {
			st.menu = nil
			st.sel.Msg = ""
			it.Run(st)
			return
		}
	}
}

// menuLines: 인라인 메뉴 줄들 (selection.Inline) — `호스트 작업 ▶ c 체크+수정 …   (Esc 닫기)`, 항목(두 칸 간격) 단위로 줄바꿈, … 로 자르지 않음.
func menuLines(m *inlineMenu, width int, color bool) []string {
	width = clampWidth(width)
	segs := []seg{{" ", ""}, {m.Label + " ▶ ", hintStyles[string(m.Open)]}}
	for i, it := range m.Items {
		if i > 0 {
			segs = append(segs, seg{"  ", ""})
		}
		style, ok := hintStyles[string(it.Key)]
		if !ok {
			style = "1;32"
		}
		segs = append(segs, seg{string(it.Key), style}, seg{" " + it.Name, stBold})
	}
	segs = append(segs, seg{"   ", ""}, seg{"(Esc 닫기)", stGray})
	return wrapSegs(color, width, 2, "   ", segs...)
}

// ---- 다음 단계가 채우는 hook ----

// startNewhost: w 메뉴 2번 — 신규 설치 양식 진입 (newhost_tui.go)
func (st *tuiState) startNewhost() {
	st.startNewhostFlow()
}

// startWork: s 키 — gossh 작업 화면 진입(메뉴 없이 바로, work_tui.go). user 가 정해지지 않았으면 세션 user 를 한 번 고른다.
func (st *tuiState) startWork() {
	st.needSessionUser(st.openWork)
}
