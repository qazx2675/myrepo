// change_tui.go - m 키(v0.7.0): 변경 메뉴 화면과 TUI 연결.
// 메뉴 항목 선택 → newChangeState → 이후 모든 키는 changeState.handleKey 로, 화면은 renderChange 로 (change.go·change_render.go).
// 실행 중에는 x 와 같이 fast tick(1초)으로 화면을 갱신하고 q/Esc/Ctrl+C/Ctrl+Z 는 무시한다 (취소는 Ctrl+X → y).
// 꺼져 있으면(conf·--admin 모두 아님) m 키는 무반응이고 안내 줄에도 나오지 않는다 — 화면은 v0.6.2 와 같다.
package main

import (
	"strings"
	"time"
)

// changeRemoteMsg: os6_mgmt 원격 클라이언트에서 m 을 눌렀을 때 (메시지 줄에만 표시, 안내 줄에는 넣지 않음)
const changeRemoteMsg = "변경 기능은 os8_mgmt 에서 실행하세요"

// changeEnabled: 변경 메뉴가 conf 또는 --admin 으로 켜져 있음 (항목이 하나라도 있음)
func changeEnabled() bool { return ipChangeEnabled() || ldapChangeEnabled() }

// changeRemote: 원격 클라이언트(os6_mgmt 에서 os8_mgmt 의 데몬을 보는 TUI) — 변경은 로컬 데몬 모드(os8)에서만
func (st *tuiState) changeRemote() bool { return os8_mgmt != "" || !st.src.Local() }

// changeAvailable: 안내 줄에 `m 변경` 을 보일 조건 (켜져 있고 로컬 데몬 모드)
func (st *tuiState) changeAvailable() bool { return changeEnabled() && !st.changeRemote() }

// startChange: m 키. 처리했으면(메뉴를 열었거나 원격 안내를 띄움) true, 꺼져 있어 무반응이면 false.
func (st *tuiState) startChange() bool {
	if !changeEnabled() {
		return false
	}
	if st.changeRemote() {
		st.sel.Msg = changeRemoteMsg
		return true
	}
	st.chgMenu = changeMenuItems()
	return st.chgMenu != nil
}

// handleChange: 변경 메뉴·흐름 화면의 키. 메뉴는 번호 선택 / Esc·q 닫기, 흐름은 changeState.handleKey.
func (st *tuiState) handleChange(ev keyEv) {
	if st.chg == nil { // 메뉴
		switch {
		case ev.k == kEsc || ev.k == kLeft || (ev.k == kRune && (ev.r == 'q' || ev.r == 'Q')):
			st.chgMenu = nil
		case ev.k == kRune:
			for _, it := range st.chgMenu {
				if it.Key == ev.r {
					st.openChange(it.Kind)
					return
				}
			}
		}
		return
	}
	c := st.chg
	if ev.k == kRune && (ev.r == runeCtrlC || ev.r == runeCtrlZ) { // 종료 키가 아니므로 무시 (실행 중이면 안내)
		if c.running() {
			c.msg = "실행 중에는 닫을 수 없습니다 — 취소는 Ctrl+X"
		}
		return
	}
	if c.handleKey(ev) {
		st.sel.Msg = c.CloseMsg
		st.closeChange()
		return
	}
	st.pollChange()
}

// openChange: 메뉴에서 고른 항목의 흐름 시작 (user 선택은 x 와 같은 newUserStep)
func (st *tuiState) openChange(kind changeKind) {
	st.chgMenu = nil
	st.chg = newChangeState(newChangeEnv(&st.snap, st.newUserStep(false, nil)), kind)
	st.pollChange()
}

// pollChange: 실행이 끝났으면 다음 단계로 (이벤트·tick 마다) + 실행 중에만 1초 fast tick 유지
func (st *tuiState) pollChange() {
	c := st.chg
	if c == nil {
		return
	}
	c.tick()
	switch {
	case c.running() && st.fastTick == nil:
		st.fastTick, st.fastStop = st.env.Tick(time.Second)
	case !c.running():
		st.stopFastTick()
	}
}

// closeChange: 변경 흐름을 닫고 메인 화면(1 또는 2)으로
func (st *tuiState) closeChange() {
	st.stopFastTick()
	st.chg, st.chgMenu = nil, nil
}

// changeShutdown: TUI 종료 시 실행 중이면 취소해 기록·임시 파일을 정리한다 (정말 종료되는 경로에서만 호출)
func (st *tuiState) changeShutdown() {
	if st.chg != nil {
		st.chg.shutdown()
	}
}

// ---- 화면 ----

// changeItemName / changeItemDesc: 메뉴 항목 이름·한 줄 설명 (계획서 §1 메뉴)
func changeItemName(k changeKind) string {
	if k == kindBoth {
		return "IP + LDAP 변경"
	}
	return k.title()
}

func changeItemDesc(k changeKind) string {
	switch k {
	case kindIP:
		return "hostname 과 새 IP 입력 — ifcfg 만 바꾸고 네트워크는 재시작하지 않음"
	case kindLDAP:
		return "infra 선택 + hostname 입력 — dry-run 으로 바뀔 내용을 먼저 확인"
	case kindBoth:
		return "IP 먼저, 실패 호스트는 LDAP 건너뜀"
	case kindRollback:
		return "가장 최근 백업으로 (서비스 재시작 없음)"
	case kindApplRestore:
		return "LDAP 적용 후 대기 중인 auto.appl 반영"
	}
	return ""
}

// renderChangeMenu: m 메뉴 화면 — 켜진 항목을 번호로 고른다. 모든 줄은 폭에 맞춰 줄바꿈 (… 로 자르지 않음).
func renderChangeMenu(items []changeMenuItem, width, height int, color bool) string {
	width = clampWidth(width)
	title := " 변경 작업"
	if adminMode {
		title += " (--admin)"
	}
	head := append(wrapSegs(color, width, 1, "  ", seg{title, stBold}), sepLine(width))
	var body []string
	add := func(cont string, segs ...seg) { body = append(body, wrapSegs(color, width, 1, cont, segs...)...) }
	add("   ", seg{" 노드의 ", ""}, seg{"설정 변경", stRed},
		seg{" 작업입니다 — 항목을 고르면 user·대상 입력 → 미리보기 → yes 확인 순으로 진행하며, 확인 전에는 아무것도 바꾸지 않습니다", ""})
	body = append(body, "")
	for _, it := range items {
		add("    ", seg{" ", ""}, seg{string(it.Key), stGreen}, seg{" " + changeItemName(it.Kind), stBold},
			seg{" — " + changeItemDesc(it.Kind), ""})
	}
	foot := append([]string{sepLine(width)}, hintLines(color, width, " 번호 선택  Esc/q 닫기")...)
	if height > 0 {
		room := rMax(height-len(head)-len(foot), 1)
		if len(body) > room {
			body = body[:room]
		}
		for len(body) < room {
			body = append(body, "")
		}
	}
	return strings.Join(append(append(head, body...), foot...), "\n")
}
