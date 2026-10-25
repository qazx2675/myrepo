package tui

// screen_admin.go - cross_check --admin 의 전체 화면(사용자 화면과 같은 모양).
//
//	로그인 : 관리자 패스워드(3번까지)
//	목록   : 모든 작업(대상 수 포함), 머리말에 승인 유효시간·작업 제한 시간
//	상세   : 대상 목록·감사 uid 포함
//	s      : 상태 변경 — 선택한 작업에 허용된 전이만 번호로(대기→승인/중단, 승인→작업중단/중단, 작업중→작업완료/작업중단/중단 …)
//	         → 사유(선택) → 적용
//	d      : 삭제(y/n)
//	x      : 전체 정리 — 확인 문구 "전체 정리" 를 그대로 입력
//	p      : 사용자 패스워드 재설정 — 이름 → 새 패스워드 두 번

import (
	"errors"
	"fmt"
	"strconv"
	"time"

	"cross_check/internal/daemon"
	"cross_check/internal/store"
)

type adminMode int

const (
	amLogin adminMode = iota
	amList
	amDetail
	amMenu   // 상태 변경 번호 선택(목록 아래 인라인)
	amReason // 상태 변경 사유 입력(인라인)
	amDelete // 삭제 y/n(인라인)
	amClear  // 전체 정리 확인 문구
	amReset  // 패스워드 재설정
	amHelp
)

// AdminHint 는 관리자 목록 화면의 키 안내.
// (한 줄에 들어가지 않으면 "\n" 위치에서 두 줄로 나눈다)
const AdminHint = "↑↓ 이동  Enter 상세  s 상태 변경  d 삭제  x 전체 정리\np 패스워드 재설정  r 새로고침  ? 도움말  q 종료"

// adminChoice 는 상태 변경 메뉴 항목 하나.
type adminChoice struct {
	label  string
	status store.Status
	phase  store.Phase
}

// adminChoices 는 작업의 지금 상태에서 데몬이 허락하는 변경(관리자 admin_set 규칙과 같다).
func adminChoices(j store.Job) []adminChoice {
	ab := adminChoice{"중단", daemon.StatusAborted, ""}
	switch kindOf(j.Status, j.Phase) {
	case skWaiting:
		return []adminChoice{{"승인", daemon.StatusApproved, ""}, ab}
	case skApproved:
		return []adminChoice{{"승인/작업중단", daemon.StatusApproved, daemon.PhaseAborted}, ab}
	case skWorking:
		return []adminChoice{{"승인/작업완료", daemon.StatusApproved, daemon.PhaseDone}, {"승인/작업중단", daemon.StatusApproved, daemon.PhaseAborted}, ab}
	case skDone, skStopped:
		return []adminChoice{ab}
	}
	return nil
}

type adminScreen struct {
	c    Client
	now  func() time.Time
	loc  *time.Location
	code int

	pw     string // 로그인한 관리자 패스워드(세션 동안 메모리에만)
	login  field
	tries  int
	jobs   []store.Job
	ds     daemonState
	info   daemon.Info
	err    string
	mode   adminMode
	row    int
	selID  string
	msg    string
	kind   msgKind
	scroll flowScroll

	choices []adminChoice
	choice  adminChoice
	text    field // 사유·확인 문구·사용자 이름
	reset   struct {
		step   int
		n1, n2 field
		user   string
	}
}

func newAdminScreen(c Client, now func() time.Time, loc *time.Location) *adminScreen {
	if now == nil {
		now = time.Now
	}
	if loc == nil {
		loc = time.Local
	}
	a := &adminScreen{c: c, now: now, loc: loc}
	a.login.secret = true
	a.reset.n1.secret, a.reset.n2.secret = true, true
	return a
}

func (a *adminScreen) exitCode() int  { return a.code }
func (a *adminScreen) batching() bool { return false }

func (a *adminScreen) setMsg(k msgKind, format string, args ...any) {
	a.msg, a.kind = fmt.Sprintf(format, args...), k
}

func (a *adminScreen) refresh() {
	info, ierr := a.c.Info()
	a.ds.alive = ierr == nil
	if ierr == nil {
		a.info, a.ds.pid = info, info.PID
	}
	if a.pw == "" {
		if ierr == nil {
			a.ds.at = a.now()
		}
		return
	}
	jobs, err := a.c.AdminList(a.pw)
	if err != nil {
		a.err = errText(err)
		if ierr != nil {
			a.ds.alive = false
		}
		return
	}
	a.ds.at, a.err = a.now(), ""
	// 최근 것부터
	a.jobs = a.jobs[:0]
	for i := len(jobs) - 1; i >= 0; i-- {
		a.jobs = append(a.jobs, jobs[i])
	}
	a.clamp()
}

func (a *adminScreen) clamp() {
	if a.selID != "" {
		for i, j := range a.jobs {
			if j.ID == a.selID {
				a.row = i
				return
			}
		}
	}
	a.row = rMax(rMin(a.row, len(a.jobs)-1), 0)
	if len(a.jobs) > 0 {
		a.selID = a.jobs[a.row].ID
	} else {
		a.selID = ""
	}
}

func (a *adminScreen) selected() (store.Job, bool) {
	if a.row >= 0 && a.row < len(a.jobs) {
		return a.jobs[a.row], true
	}
	return store.Job{}, false
}

// ---- 키 ----

func (a *adminScreen) key(ev keyEv) bool {
	switch a.mode {
	case amLogin:
		return a.keyLogin(ev)
	case amReason:
		a.keyReason(ev)
		return false
	case amClear:
		a.keyClear(ev)
		return false
	case amReset:
		a.keyReset(ev)
		return false
	}
	if ev.paste {
		return false
	}
	if ev.k == kRune && (ev.r == runeCtrlC || ev.r == runeCtrlZ) {
		a.setMsg(msgInfo, "종료는 q 를 누르세요")
		return false
	}
	switch a.mode {
	case amHelp:
		if !a.scroll.key(ev) {
			a.mode = amList
		}
		return false
	case amMenu:
		a.keyMenu(ev)
		return false
	case amDelete:
		a.keyDelete(ev)
		return false
	case amDetail:
		switch {
		case a.scroll.key(ev):
		case ev.k == kRune && (ev.r == 'r' || ev.r == 'R'):
			a.refresh()
		case ev.k == kRune && (ev.r == 's' || ev.r == 'S'):
			a.openMenu()
		default:
			a.mode = amList
		}
		return false
	}
	return a.keyList(ev)
}

func (a *adminScreen) keyLogin(ev keyEv) bool {
	switch {
	case ev.k == kEsc && !ev.paste:
		a.login.clear()
		a.code = ExitOK
		return true
	case ev.k == kEnter:
		if len(a.login.rs) == 0 {
			a.setMsg(msgErr, "패스워드를 입력하세요")
			return false
		}
		pw := a.login.text()
		a.login.clear()
		_, err := a.c.AdminList(pw)
		if err == nil {
			a.pw = pw
			a.mode = amList
			a.setMsg(msgOK, "[관리자] 로그인했습니다")
			a.refresh()
			return false
		}
		msg, ok := isDaemonErr(err)
		if !ok {
			a.setMsg(msgErr, "[X] 데몬 요청 실패: %v", err)
			a.code = ExitError
			return true
		}
		if msg == daemon.MsgNoAdmin {
			a.setMsg(msgErr, "%s", AdminMissingText)
			a.code = ExitFail
			return true
		}
		a.tries++
		if a.tries >= 3 {
			a.setMsg(msgErr, "관리자 인증을 3번 실패해 종료합니다")
			a.code = ExitFail
			return true
		}
		a.setMsg(msgErr, "[X] %s (%d/3)", msg, a.tries)
	default:
		a.login.key(ev)
	}
	return false
}

func (a *adminScreen) keyList(ev keyEv) bool {
	move := func(row int) {
		a.row = rMax(rMin(row, len(a.jobs)-1), 0)
		if len(a.jobs) > 0 {
			a.selID = a.jobs[a.row].ID
		}
	}
	r := func(cs ...rune) bool {
		if ev.k != kRune {
			return false
		}
		for _, c := range cs {
			if ev.r == c {
				return true
			}
		}
		return false
	}
	switch {
	case ev.k == kUp || r('k'):
		move(a.row - 1)
	case ev.k == kDown || r('j'):
		move(a.row + 1)
	case ev.k == kPgUp:
		move(a.row - 10)
	case ev.k == kPgDn:
		move(a.row + 10)
	case ev.k == kHome:
		move(0)
	case ev.k == kEnd:
		move(len(a.jobs) - 1)
	case ev.k == kEnter:
		if _, ok := a.selected(); ok {
			a.mode, a.scroll, a.msg = amDetail, flowScroll{}, ""
		}
	case r('s', 'S'):
		a.openMenu()
	case r('d', 'D'):
		if j, ok := a.selected(); ok {
			a.selID = j.ID
			a.mode, a.msg = amDelete, ""
		}
	case r('x', 'X'):
		a.text = field{}
		a.mode, a.msg = amClear, ""
	case r('p', 'P'):
		a.text = field{}
		a.reset.step, a.reset.user = 0, ""
		a.reset.n1.clear()
		a.reset.n2.clear()
		a.mode, a.msg = amReset, ""
	case r('r', 'R'):
		a.refresh()
		if a.err == "" {
			a.setMsg(msgInfo, "새로 고쳤습니다")
		}
	case r('?'):
		a.mode, a.scroll = amHelp, flowScroll{}
	case ev.k == kEsc || r('q', 'Q'):
		a.code = ExitOK
		return true
	}
	return false
}

func (a *adminScreen) openMenu() {
	j, ok := a.selected()
	if !ok {
		a.setMsg(msgInfo, "선택한 작업이 없습니다")
		return
	}
	a.choices = adminChoices(j)
	if len(a.choices) == 0 {
		a.setMsg(msgInfo, "작업 %s 은(는) 이미 중단되어 바꿀 수 없습니다(중단된 작업은 되살릴 수 없음)", j.ID)
		a.mode = amList
		return
	}
	a.selID = j.ID
	a.mode, a.msg = amMenu, ""
}

func (a *adminScreen) keyMenu(ev keyEv) {
	if ev.k == kEsc || (ev.k == kRune && (ev.r == 'q' || ev.r == 's')) {
		a.mode = amList
		return
	}
	if ev.k == kRune && ev.r >= '1' && ev.r <= '9' {
		n := int(ev.r - '1')
		if n < len(a.choices) {
			a.choice = a.choices[n]
			a.text = field{}
			a.mode = amReason
		}
	}
}

func (a *adminScreen) keyReason(ev keyEv) {
	switch {
	case ev.k == kEsc && !ev.paste:
		a.mode = amList
		a.setMsg(msgInfo, "변경하지 않았습니다")
	case ev.k == kEnter:
		reason := a.text.text()
		var err error
		if a.choice.phase != "" {
			err = a.c.AdminSetPhase(a.pw, a.selID, a.choice.phase, reason)
		} else {
			err = a.c.AdminSet(a.pw, a.selID, a.choice.status, reason)
		}
		a.mode = amList
		if err != nil {
			a.setMsg(msgErr, "[X] 상태 변경 거부: %s", errText(err))
		} else {
			a.setMsg(msgOK, "[변경 완료] 작업 %s → %s", a.selID, a.choice.label)
		}
		a.refresh()
	default:
		a.text.key(ev)
	}
}

func (a *adminScreen) keyDelete(ev keyEv) {
	a.mode = amList
	if ev.k == kRune && (ev.r == 'y' || ev.r == 'Y') {
		if err := a.c.AdminDelete(a.pw, a.selID); err != nil {
			a.setMsg(msgErr, "[X] 삭제 거부: %s", errText(err))
		} else {
			a.setMsg(msgOK, "[삭제 완료] 작업 %s", a.selID)
			a.selID = ""
		}
		a.refresh()
		return
	}
	a.setMsg(msgInfo, "삭제하지 않았습니다")
}

func (a *adminScreen) keyClear(ev keyEv) {
	switch {
	case ev.k == kEsc && !ev.paste:
		a.mode = amList
		a.setMsg(msgInfo, "정리하지 않았습니다")
	case ev.k == kEnter:
		in := a.text.text()
		a.mode = amList
		if in != ClearConfirmText {
			a.setMsg(msgInfo, "확인 문구가 달라 정리하지 않았습니다")
			return
		}
		n, err := a.c.AdminClear(a.pw)
		if err != nil {
			a.setMsg(msgErr, "[X] 전체 정리 거부: %s", errText(err))
		} else {
			a.setMsg(msgOK, "[정리 완료] %d건 삭제", n)
		}
		a.refresh()
	default:
		a.text.key(ev)
	}
}

func (a *adminScreen) keyReset(ev keyEv) {
	if ev.k == kEsc && !ev.paste {
		a.reset.n1.clear()
		a.reset.n2.clear()
		a.mode = amList
		a.setMsg(msgInfo, "재설정하지 않았습니다")
		return
	}
	if ev.k != kEnter {
		switch a.reset.step {
		case 0:
			a.text.key(ev)
		case 1:
			a.reset.n1.key(ev)
		default:
			a.reset.n2.key(ev)
		}
		return
	}
	switch a.reset.step {
	case 0:
		u := a.text.text()
		if !daemon.ValidName(u) {
			a.setMsg(msgErr, "사용자 이름이 올바르지 않습니다(영소문자·숫자·_ 만, %d자 이하)", daemon.MaxNameLen)
			return
		}
		a.reset.user, a.reset.step, a.msg = u, 1, ""
	case 1:
		if len(a.reset.n1.rs) == 0 {
			a.setMsg(msgErr, "입력이 비어 있습니다")
			return
		}
		a.reset.step, a.msg = 2, ""
	default:
		n1, n2 := a.reset.n1.text(), a.reset.n2.text()
		a.reset.n1.clear()
		a.reset.n2.clear()
		a.reset.step = 1
		if n1 != n2 {
			a.setMsg(msgErr, "두 입력이 서로 다릅니다 — 새 패스워드를 다시 입력하세요")
			return
		}
		if err := daemon.CheckPassword(n1); err != nil {
			a.setMsg(msgErr, "%v — 다시 입력하세요", err)
			return
		}
		a.mode = amList
		if err := a.c.AdminSetPassword(a.pw, a.reset.user, n1); err != nil {
			a.setMsg(msgErr, "[X] 패스워드 재설정 거부: %s", errText(err))
			return
		}
		a.setMsg(msgOK, "[재설정 완료] %s 의 패스워드를 바꿨습니다(연속 실패 지연도 풀림)", a.reset.user)
	}
}

// ---- 그리기 ----

func (a *adminScreen) render(w, h int, color bool) string {
	w = clampWidth(w)
	switch a.mode {
	case amLogin:
		return a.renderLogin(w, h, color)
	case amDetail:
		if j, ok := a.selected(); ok {
			return a.renderDetail(j, w, h, color)
		}
		a.mode = amList
	case amClear:
		return a.renderClear(w, h, color)
	case amReset:
		return a.renderReset(w, h, color)
	case amHelp:
		return a.renderHelp(w, h, color)
	}
	return a.renderList(w, h, color)
}

func (a *adminScreen) renderLogin(w, h int, color bool) string {
	f := flowFrame{Title: "cross_check 관리자 로그인", Msg: a.msg, MsgErr: a.kind == msgErr, Hint: "Enter 로그인  Esc 종료"}
	f.Head = append(f.Head, wrapText(color, w, "", "   ", " auth.conf 의 admin 패스워드를 입력하세요(화면에는 * 로 보입니다). 3번 틀리면 종료합니다.")...)
	f.Head = append(f.Head, wrapText(color, w, stGray, "   ", " "+ttlText(a.info))...)
	f.Foot = append(f.Foot, a.login.line(color, w, "관리자 패스워드"))
	return renderFlow(f, nil, w, h, color)
}

func ttlText(info daemon.Info) string {
	segs := ttlSegs(info.ApprovalTTL, info.WorkTimeout, true)
	return segsText(segs)
}

func (a *adminScreen) renderList(w, h int, color bool) string {
	kinds := make([]stateKind, 0, len(a.jobs))
	rows := make([]tableRow, 0, len(a.jobs))
	for _, j := range a.jobs {
		k := kindOf(j.Status, j.Phase)
		kinds = append(kinds, k)
		rows = append(rows, tableRow{id: j.ID, worker: j.User, checker: j.Checker, label: daemon.StatusLabel(j.Status, j.Phase),
			caller: callerText(j.Caller), at: shortTime(j.UpdatedAt, a.loc), extra: strconv.Itoa(len(j.Targets)), kind: k})
	}
	head := titleLines(color, w, "cross_check 관리자 상태 리포트", "admin", a.ds, a.loc, counterSegs(kinds), ttlSegs(a.info.ApprovalTTL, a.info.WorkTimeout, true))
	head = append(head, sepLine(w))
	var body [][]string
	cursor := 0
	if len(rows) == 0 {
		body = append(body, wrapText(color, w, stGray, "   ", " (작업 기록 없음)"))
	} else {
		body = jobTable(color, w, rows, a.row, "대상")
		cursor = 1 + a.row
	}
	var foot []string
	switch a.mode {
	case amMenu:
		segs := []seg{{" 상태 변경 ▶ ", stBold}, {a.selID + "  ", ""}}
		for i, c := range a.choices {
			segs = append(segs, seg{fmt.Sprintf("%d ", i+1), "1;33"}, seg{c.label + "  ", ""})
		}
		segs = append(segs, seg{"(번호 선택, Esc 닫기)", stGray})
		foot = append(foot, wrapSegs(color, w, 2, "   ", segs...)...)
	case amReason:
		foot = append(foot, segLine(color, w, seg{fmt.Sprintf(" 작업 %s → %s", a.selID, a.choice.label), stBold}))
		foot = append(foot, a.text.line(color, w, "사유(선택, Enter 적용 · Esc 취소)"))
	case amDelete:
		foot = append(foot, segLine(color, w, seg{fmt.Sprintf(" 작업 %s 를 삭제할까요? 기다리던 작업자에게는 중단으로 알립니다 (y/n)", a.selID), stRed}))
	}
	msg, kind := a.msg, a.kind
	if msg == "" && a.err != "" {
		msg, kind = "[X] 상태 갱신 실패: "+a.err, msgErr
	}
	foot = append(foot, msgLines(color, w, msg, kind)...)
	foot = append(foot, hintLines(color, w, AdminHint)...)
	return finish(head, body, cursor, h, foot)
}

func (a *adminScreen) renderDetail(j store.Job, w, h int, color bool) string {
	k := kindOf(j.Status, j.Phase)
	var body []string
	add := func(name string, val seg) { body = append(body, kv(color, w, name, val)...) }
	add("작업자", seg{j.User, ""})
	add("확인자", seg{j.Checker, ""})
	add("상태", seg{daemon.StatusLabel(j.Status, j.Phase), kindStyle[k]})
	if j.Reason != "" {
		add("사유", seg{j.Reason, ""})
	}
	add("호출 스크립트", seg{callerText(j.Caller), ""})
	add("생성", seg{longTime(&j.CreatedAt, a.loc), ""})
	add("승인", seg{longTime(j.ApprovedAt, a.loc), ""})
	add("작업 시작", seg{longTime(j.StartedAt, a.loc), ""})
	add("작업 종료", seg{longTime(j.FinishedAt, a.loc), ""})
	add("갱신", seg{longTime(&j.UpdatedAt, a.loc), ""})
	add("uid(작업/확인)", seg{fmt.Sprintf("%d / %d", j.WorkerUID, j.CheckerUID), ""})
	body = append(body, "")
	add("대상 VM", seg{fmt.Sprintf("%d개", len(j.Targets)), ""})
	for i, t := range j.Targets {
		body = append(body, segLine(color, w, seg{fmt.Sprintf("    %3d) ", i+1), stGray}, seg{t, ""}))
	}
	return renderFlow(flowFrame{Title: fmt.Sprintf("작업 %s 상세  (admin)", j.ID), Body: body,
		Msg: a.msg, MsgErr: a.kind == msgErr, MsgOK: a.kind == msgOK, Hint: "↑↓ 스크롤  s 상태 변경  r 새로고침  Esc 목록"}, &a.scroll, w, h, color)
}

func (a *adminScreen) renderClear(w, h int, color bool) string {
	f := flowFrame{Title: "전체 정리  (admin)", Msg: a.msg, MsgErr: a.kind == msgErr, Hint: "Enter 확인  Esc 취소"}
	f.Head = append(f.Head, wrapText(color, w, stRed, "   ", " 모든 작업 기록을 지웁니다. 기다리던 작업자 스크립트는 모두 중단됩니다.")...)
	f.Head = append(f.Head, wrapText(color, w, "", "   ", fmt.Sprintf(" 계속하려면 %q 를 그대로 입력하고 Enter. 다른 입력이면 아무것도 지우지 않습니다.", ClearConfirmText))...)
	f.Foot = append(f.Foot, a.text.line(color, w, "확인 문구"))
	return renderFlow(f, nil, w, h, color)
}

func (a *adminScreen) renderReset(w, h int, color bool) string {
	f := flowFrame{Title: "사용자 패스워드 재설정  (admin)", Msg: a.msg, MsgErr: a.kind == msgErr, Hint: "Enter 다음  Esc 취소"}
	f.Head = append(f.Head, wrapText(color, w, "", "   ", " users= 목록에 있는 사용자의 패스워드를 바꿉니다(항목이 없으면 새로 등록). admin 패스워드는 여기서 바꿀 수 없습니다.")...)
	if a.reset.step == 0 {
		f.Foot = append(f.Foot, a.text.line(color, w, padR("사용자 이름", 16)))
	} else {
		f.Foot = append(f.Foot, segLine(color, w, seg{" " + padR("사용자 이름", 16) + " > ", stBold}, seg{a.reset.user, ""}))
		f.Foot = append(f.Foot, a.reset.n1.line(color, w, padR("새 패스워드", 16)))
		if a.reset.step == 2 {
			f.Foot = append(f.Foot, a.reset.n2.line(color, w, padR("새 패스워드 확인", 16)))
		}
	}
	return renderFlow(f, nil, w, h, color)
}

func (a *adminScreen) renderHelp(w, h int, color bool) string {
	lines := []string{
		" [관리자 목록]",
		"   ↑↓ (j/k)  작업 이동            Enter  상세(대상 목록·uid)",
		"   s         상태 변경: 대기→승인/중단, 승인→승인/작업중단·중단, 승인/작업중→승인/작업완료·승인/작업중단·중단,",
		"             끝난 작업(작업완료·작업중단)→중단. 중단된 작업은 되살릴 수 없습니다. 번호 선택 → 사유(선택) → Enter",
		"   d         작업 삭제(y/n)",
		"   x         전체 정리 — 확인 문구 \"" + ClearConfirmText + "\" 를 그대로 입력",
		"   p         사용자 패스워드 재설정(이름 → 새 패스워드 두 번)",
		"   r         새로고침(2초마다 자동)     q / Esc  종료",
		"",
		" " + ttlText(a.info),
	}
	var body []string
	for _, l := range lines {
		body = append(body, wrapText(color, w, "", "             ", l)...)
	}
	return renderFlow(flowFrame{Title: "cross_check 관리자 도움말", Body: body, Hint: "↑↓ 스크롤  아무 키나 누르면 목록으로"}, &a.scroll, w, h, color)
}

// RunAdminFull 은 cross_check --admin 을 전체 화면으로 돌린다. raw 모드로 바꾸지 못하면 ok=false.
// admin 항목이 없거나 데몬 오류면 화면을 열지 않고 안내 후 종료 코드를 돌려준다.
func RunAdminFull(env Env, c Client) (code int, ok bool) {
	reg, err := c.UserRegistered(daemon.AdminUser)
	if err != nil {
		if msg, isD := isDaemonErr(err); isD {
			fmt.Fprintln(env.Out, "[오류] 관리자 등록 여부를 확인할 수 없습니다: "+msg)
			return ExitFail, true
		}
		fmt.Fprintln(env.Out, "[오류] "+err.Error())
		return ExitError, true
	}
	if !reg {
		fmt.Fprintln(env.Out, "[오류] "+AdminMissingText)
		return ExitFail, true
	}
	m := newAdminScreen(c, env.Now, env.Loc)
	code, err = runLoop(env, m)
	var rf rawFailed
	if errors.As(err, &rf) {
		return 0, false
	}
	m.pw = ""
	if code != ExitOK && m.msg != "" {
		fmt.Fprintln(env.Out, m.msg)
	}
	return code, true
}
