package tui

// screen_user.go - cross_check --user <나> 의 전체 화면(auto_setup 상태 리포트와 같은 모양).
//
//	목록  : 머리말(제목·갱신·데몬) / 카운터 / 구분선 / 작업 구역(내가 확인자인 작업 · 내 작업, ←→/Tab 전환, a 전체 보기) / 안내 / 키 안내
//	상세  : 작업 하나(확인자에게는 대상 목록을 보이지 않음)
//	승인  : 1/3 대상 붙여넣기(인식한 이름 수만 표시, 판정은 보이지 않음) → 2/3 패스워드 → 3/3 결과
//	취소  : 패스워드 → 중단
//	등록  : 첫 실행 패스워드 등록(두 번 입력)
//	변경  : p 패스워드 변경

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"cross_check/internal/daemon"
)

type userMode int

const (
	umList userMode = iota
	umDetail
	umApprove
	umCancel
	umPassword
	umRegister
	umHelp
)

// 승인 단계.
const (
	apPaste  = 1
	apPass   = 2
	apResult = 3
)

// UserHint 는 목록 화면의 키 안내(문서에 그대로 쓰는 문구).
// (한 줄에 들어가지 않으면 "\n" 위치에서 두 줄로 나눈다)
const UserHint = "↑↓ 이동  ←→/Tab 구역 전환  Enter 상세/승인  a 전체 보기  r 새로고침\nc 취소  p 패스워드 변경  ? 도움말  q 종료"

type userScreen struct {
	c    Client
	user string
	now  func() time.Time
	loc  *time.Location
	code int

	jobs []daemon.JobView // 마지막으로 받은 목록(데몬 순서: 오래된 것부터)
	ds   daemonState
	info daemon.Info
	err  string // 마지막 갱신 오류(없으면 "")

	prev     map[string]string // 지난 갱신 때의 작업별 상태 표기(알림용)
	prevInit bool

	mode  userMode
	pane  int  // 0 = 내가 확인자인 작업(기본), 1 = 내 작업(작업자)
	all   bool // a 전체 보기: 두 구역을 함께
	selID string
	row   int

	msg     string
	msgKind msgKind

	detID     string
	detScroll flowScroll

	ap struct {
		id, worker string
		step       int
		box        pasteBox
		pw         field
		ok         bool
		result     string
		scroll     flowScroll
	}
	cn struct {
		id, worker string
		pw         field
	}
	pc struct {
		cur, n1, n2 field
		step        int
	}
	reg struct {
		p1, p2 field
		step   int
		tries  int
	}
	helpScroll flowScroll
}

// newUserScreen 은 사용자 화면. registered 가 false 면 첫 실행 등록부터 한다.
func newUserScreen(c Client, user string, registered bool, now func() time.Time, loc *time.Location) *userScreen {
	if now == nil {
		now = time.Now
	}
	if loc == nil {
		loc = time.Local
	}
	u := &userScreen{c: c, user: user, now: now, loc: loc, prev: map[string]string{}}
	u.ap.pw.secret = true
	u.cn.pw.secret = true
	u.pc.cur.secret, u.pc.n1.secret, u.pc.n2.secret = true, true, true
	u.reg.p1.secret, u.reg.p2.secret = true, true
	if !registered {
		u.mode = umRegister
	}
	return u
}

func (u *userScreen) exitCode() int { return u.code }

func (u *userScreen) batching() bool { return u.mode == umApprove && u.ap.step == apPaste }

func (u *userScreen) setMsg(k msgKind, format string, a ...any) {
	u.msg, u.msgKind = fmt.Sprintf(format, a...), k
}

// ---- 데이터 ----

func (u *userScreen) refresh() {
	info, ierr := u.c.Info()
	jobs, err := u.c.List(u.user)
	if err != nil {
		u.ds.alive = false
		u.err = errText(err)
		return
	}
	u.ds.alive = ierr == nil
	if ierr == nil {
		u.info, u.ds.pid = info, info.PID
	}
	u.ds.at = u.now()
	u.err = ""
	u.notices(jobs)
	u.jobs = jobs
	u.clamp()
}

func errText(err error) string {
	if msg, ok := isDaemonErr(err); ok {
		return msg
	}
	return err.Error()
}

// notices 는 지난 갱신과 비교해 바뀐 것을 안내 줄에 알린다(목록·상세 화면에서만).
func (u *userScreen) notices(jobs []daemon.JobView) {
	cur := map[string]string{}
	for _, j := range jobs {
		cur[j.ID] = daemon.StatusLabel(j.Status, j.Phase)
	}
	if u.prevInit && (u.mode == umList || u.mode == umDetail) {
		for i := len(jobs) - 1; i >= 0; i-- {
			j := jobs[i]
			old, seen := u.prev[j.ID]
			now := cur[j.ID]
			if seen && old == now {
				continue
			}
			switch {
			case j.Checker == u.user && j.Status == daemon.StatusWaiting && !seen:
				u.setMsg(msgInfo, "새 승인 요청: 작업 %s (작업자 %s) — 선택하고 Enter 로 승인", j.ID, j.User)
			case j.User == u.user && j.Status == daemon.StatusAborted && seen:
				u.setMsg(msgErr, "내 작업 %s 중단 — %s", j.ID, j.Reason)
			case j.User == u.user && j.Status == daemon.StatusApproved && j.Phase == daemon.PhaseNone && seen:
				u.setMsg(msgOK, "내 작업 %s 승인됨 (확인자 %s)", j.ID, j.Checker)
			case j.User == u.user && seen:
				u.setMsg(msgInfo, "내 작업 %s → %s", j.ID, now)
			}
		}
	}
	u.prev, u.prevInit = cur, true
}

// visible 은 지금 구역에 보이는 작업(최근 것부터).
func (u *userScreen) visible() []daemon.JobView {
	var out []daemon.JobView
	for i := len(u.jobs) - 1; i >= 0; i-- {
		j := u.jobs[i]
		switch {
		case u.all && (j.User == u.user || j.Checker == u.user):
		case !u.all && u.pane == 1 && j.User == u.user:
		case !u.all && u.pane == 0 && j.Checker == u.user:
		default:
			continue
		}
		out = append(out, j)
	}
	return out
}

// clamp 는 선택을 작업 ID 로 유지하고(목록이 바뀌어도 같은 작업), 없으면 범위 안으로.
func (u *userScreen) clamp() {
	vis := u.visible()
	if u.selID != "" {
		for i, j := range vis {
			if j.ID == u.selID {
				u.row = i
				return
			}
		}
	}
	u.row = rMax(rMin(u.row, len(vis)-1), 0)
	if len(vis) > 0 {
		u.selID = vis[u.row].ID
	} else {
		u.selID = ""
	}
}

func (u *userScreen) find(id string) (daemon.JobView, bool) {
	for _, j := range u.jobs {
		if j.ID == id {
			return j, true
		}
	}
	return daemon.JobView{}, false
}

func (u *userScreen) selected() (daemon.JobView, bool) {
	vis := u.visible()
	if u.row >= 0 && u.row < len(vis) {
		return vis[u.row], true
	}
	return daemon.JobView{}, false
}

// canDecide 는 내가 확인자인 대기 작업인지(승인·취소 가능).
func (u *userScreen) canDecide(j daemon.JobView) bool {
	return j.Checker == u.user && j.Status == daemon.StatusWaiting
}

// ---- 키 ----

func (u *userScreen) key(ev keyEv) bool {
	switch u.mode {
	case umRegister:
		return u.keyRegister(ev)
	case umApprove:
		u.keyApprove(ev)
		return false
	case umCancel:
		u.keyCancel(ev)
		return false
	case umPassword:
		u.keyPassword(ev)
		return false
	case umHelp:
		if ev.paste {
			return false
		}
		if !u.helpScroll.key(ev) {
			u.mode = umList
		}
		return false
	}
	if ev.paste { // 목록·상세에서 붙여넣은 글은 단축키로 해석하지 않는다
		return false
	}
	if ev.k == kRune && (ev.r == runeCtrlC || ev.r == runeCtrlZ) {
		u.setMsg(msgInfo, "종료는 q 를 누르세요")
		return false
	}
	if u.mode == umDetail {
		u.keyDetail(ev)
		return false
	}
	return u.keyList(ev)
}

func (u *userScreen) keyList(ev keyEv) bool {
	vis := u.visible()
	move := func(row int) {
		u.row = rMax(rMin(row, len(vis)-1), 0)
		if len(vis) > 0 {
			u.selID = vis[u.row].ID
		}
	}
	isR := func(rs ...rune) bool {
		if ev.k != kRune {
			return false
		}
		for _, r := range rs {
			if ev.r == r {
				return true
			}
		}
		return false
	}
	switch {
	case ev.k == kUp || isR('k'):
		move(u.row - 1)
	case ev.k == kDown || isR('j'):
		move(u.row + 1)
	case ev.k == kPgUp:
		move(u.row - 10)
	case ev.k == kPgDn:
		move(u.row + 10)
	case ev.k == kHome:
		move(0)
	case ev.k == kEnd:
		move(len(vis) - 1)
	case ev.k == kLeft || ev.k == kRight || ev.k == kBackTab || (ev.k == kRune && ev.r == '\t'):
		if u.all {
			u.all = false
		} else {
			u.pane = 1 - u.pane
		}
		u.selID, u.row = "", 0
		u.clamp()
		u.msg = ""
	case isR('a', 'A'):
		u.all = !u.all
		u.clamp()
		if u.all {
			u.setMsg(msgInfo, "전체 보기: 내 작업과 내가 확인자인 작업을 함께 보여 줍니다 (a: 작업별 보기)")
		} else {
			u.msg = ""
		}
	case ev.k == kEnter:
		j, ok := u.selected()
		if !ok {
			u.setMsg(msgInfo, "선택한 작업이 없습니다")
			return false
		}
		u.msg = ""
		if u.canDecide(j) {
			u.startApprove(j)
			return false
		}
		u.detID, u.detScroll = j.ID, flowScroll{}
		u.mode = umDetail
	case isR('c', 'C'):
		if j, ok := u.selected(); ok {
			u.startCancel(j)
		} else {
			u.setMsg(msgInfo, "선택한 작업이 없습니다")
		}
	case isR('r', 'R'):
		u.refresh()
		if u.err == "" {
			u.setMsg(msgInfo, "새로 고쳤습니다")
		}
	case isR('p', 'P'):
		u.startPassword()
	case isR('?'):
		u.mode, u.helpScroll = umHelp, flowScroll{}
	case ev.k == kEsc || isR('q', 'Q'):
		u.code = ExitOK
		return true
	}
	return false
}

func (u *userScreen) keyDetail(ev keyEv) {
	j, ok := u.find(u.detID)
	switch {
	case u.detScroll.key(ev):
	case ev.k == kLeft || ev.k == kEsc || (ev.k == kRune && (ev.r == 'q' || ev.r == 'Q')):
		u.mode = umList
	case ev.k == kEnter && ok && u.canDecide(j):
		u.startApprove(j)
	case ev.k == kRune && (ev.r == 'c' || ev.r == 'C') && ok:
		u.startCancel(j)
	case ev.k == kRune && (ev.r == 'r' || ev.r == 'R'):
		u.refresh()
	case ev.k == kEnter:
		u.mode = umList
	}
}

// ---- 승인: 붙여넣기 → 패스워드 → 결과 ----

func (u *userScreen) startApprove(j daemon.JobView) {
	u.ap.id, u.ap.worker, u.ap.step = j.ID, j.User, apPaste
	u.ap.box.reset()
	u.ap.pw.clear()
	u.ap.ok, u.ap.result = false, ""
	u.ap.scroll = flowScroll{Follow: true}
	u.msg = ""
	u.mode = umApprove
}

func (u *userScreen) keyApprove(ev keyEv) {
	switch u.ap.step {
	case apPaste:
		if ev.k == kEsc && !ev.paste {
			u.ap.box.reset()
			u.mode = umList
			u.setMsg(msgInfo, "승인하지 않고 목록으로 돌아왔습니다")
			return
		}
		if ev.k == kRune && ev.r == runeCtrlC && !ev.paste {
			u.setMsg(msgInfo, "돌아가기는 Esc (승인하지 않음)")
			return
		}
		if (ev.k == kUp || ev.k == kDown || ev.k == kPgUp || ev.k == kPgDn) && !ev.paste {
			u.ap.scroll.key(ev)
			return
		}
		switch u.ap.box.key(ev) {
		case pasteEdited:
			u.ap.scroll.Follow = true
			if u.ap.box.full {
				u.setMsg(msgErr, "붙여넣기가 너무 깁니다(%d KiB 까지) — 나머지는 무시했습니다", maxPaste>>10)
			} else if u.msg != "" && u.msgKind != msgErr {
				u.msg = ""
			}
		case pasteDone:
			if u.ap.box.count() == 0 {
				u.setMsg(msgErr, "붙여넣은 대상이 없습니다 — 작업자 %s 에게 전달받은 대상을 붙여넣으세요", u.ap.worker)
				return
			}
			u.ap.step = apPass
			u.ap.pw.clear()
			u.msg = ""
		}
	case apPass:
		if ev.k == kEsc && !ev.paste {
			u.ap.pw.clear()
			u.ap.step = apPaste
			u.ap.scroll.Follow = true
			u.msg = ""
			return
		}
		if ev.k == kEnter {
			if len(u.ap.pw.rs) == 0 {
				u.setMsg(msgErr, "패스워드를 입력하세요")
				return
			}
			u.submitApprove()
			return
		}
		u.ap.pw.key(ev)
	case apResult:
		if ev.paste {
			return
		}
		if ev.k == kEnter || ev.k == kEsc || (ev.k == kRune && (ev.r == 'q' || ev.r == 'Q')) {
			u.ap.box.reset()
			u.mode = umList
			u.refresh()
		}
	}
}

// submitApprove 는 붙여넣은 글과 패스워드로 승인을 보낸다. 패스워드가 틀리면 2/3 단계에 머문다(상태 그대로).
func (u *userScreen) submitApprove() {
	pw := u.ap.pw.text()
	u.ap.pw.clear()
	err := u.c.Approve(u.ap.id, u.user, pw, u.ap.box.text())
	pw = ""
	if err == nil {
		u.ap.ok, u.ap.step = true, apResult
		u.ap.result = fmt.Sprintf("[승인 완료] 작업 %s 를 승인했습니다 — 작업자 %s 의 스크립트가 다음 단계로 진행합니다.", u.ap.id, u.ap.worker)
		u.ap.box.reset()
		u.msg = ""
		u.refresh()
		return
	}
	msg, isD := isDaemonErr(err)
	if isD && isAuthMsg(msg) {
		hint := ""
		if msg == daemon.MsgAuthFailed {
			hint = " — 패스워드를 다시 입력하세요(5회 연속 실패하면 이후 시도는 1초씩 늦게 처리됩니다)"
		}
		u.setMsg(msgErr, "[X] %s%s", msg, hint)
		return
	}
	u.ap.ok, u.ap.step = false, apResult
	switch {
	case isD && daemon.IsMismatchReason(msg):
		u.ap.result = "[X] " + msg
	case isD:
		u.ap.result = "[X] 승인 거부: " + msg
	default:
		u.ap.result = "[X] 승인 요청 실패: " + err.Error()
	}
	u.ap.box.reset()
	u.msg = ""
	u.refresh()
}

// ---- 취소 ----

func (u *userScreen) startCancel(j daemon.JobView) {
	if !u.canDecide(j) {
		u.setMsg(msgInfo, "c 취소는 내가 확인자인 대기 작업에서만 됩니다 (작업자는 작업 스크립트에서 Ctrl+C)")
		return
	}
	u.cn.id, u.cn.worker = j.ID, j.User
	u.cn.pw.clear()
	u.msg = ""
	u.mode = umCancel
}

func (u *userScreen) keyCancel(ev keyEv) {
	switch {
	case ev.k == kEsc && !ev.paste:
		u.cn.pw.clear()
		u.mode = umList
		u.setMsg(msgInfo, "취소하지 않았습니다")
	case ev.k == kEnter:
		if len(u.cn.pw.rs) == 0 {
			u.setMsg(msgErr, "패스워드를 입력하세요")
			return
		}
		pw := u.cn.pw.text()
		u.cn.pw.clear()
		err := u.c.Cancel(u.cn.id, u.user, pw)
		pw = ""
		if err == nil {
			u.mode = umList
			u.setMsg(msgOK, "[취소 완료] 작업 %s 를 중단했습니다 — 작업자 %s 의 스크립트도 중단됩니다", u.cn.id, u.cn.worker)
			u.refresh()
			return
		}
		if msg, ok := isDaemonErr(err); ok && isAuthMsg(msg) {
			u.setMsg(msgErr, "[X] %s — 패스워드를 다시 입력하세요", msg)
			return
		}
		u.mode = umList
		u.setMsg(msgErr, "[X] 취소 실패: %s", errText(err))
		u.refresh()
	default:
		u.cn.pw.key(ev)
	}
}

// ---- 패스워드 변경 ----

func (u *userScreen) startPassword() {
	u.pc.cur.clear()
	u.pc.n1.clear()
	u.pc.n2.clear()
	u.pc.step = 0
	u.msg = ""
	u.mode = umPassword
}

func (u *userScreen) pcField() *field {
	switch u.pc.step {
	case 1:
		return &u.pc.n1
	case 2:
		return &u.pc.n2
	}
	return &u.pc.cur
}

func (u *userScreen) keyPassword(ev keyEv) {
	switch {
	case ev.k == kEsc && !ev.paste:
		u.startPassword()
		u.mode = umList
		u.setMsg(msgInfo, "패스워드를 바꾸지 않았습니다")
	case ev.k == kEnter:
		f := u.pcField()
		if len(f.rs) == 0 {
			u.setMsg(msgErr, "입력이 비어 있습니다")
			return
		}
		if u.pc.step < 2 {
			u.pc.step++
			u.msg = ""
			return
		}
		cur, n1, n2 := u.pc.cur.text(), u.pc.n1.text(), u.pc.n2.text()
		u.startPassword()
		if n1 != n2 {
			u.setMsg(msgErr, "두 입력이 서로 다릅니다 — 처음부터 다시 입력하세요")
			return
		}
		if err := daemon.CheckPassword(n1); err != nil {
			u.setMsg(msgErr, "%v — 처음부터 다시 입력하세요", err)
			return
		}
		if err := u.c.ChangePassword(u.user, cur, n1); err != nil {
			u.setMsg(msgErr, "[X] 변경 실패: %s", errText(err))
			return
		}
		u.mode = umList
		u.setMsg(msgOK, "[변경 완료] %s 의 패스워드를 바꿨습니다", u.user)
	default:
		u.pcField().key(ev)
	}
}

// ---- 첫 실행 등록 ----

func (u *userScreen) keyRegister(ev keyEv) bool {
	f := &u.reg.p1
	if u.reg.step == 1 {
		f = &u.reg.p2
	}
	switch {
	case ev.k == kEsc && !ev.paste:
		u.reg.p1.clear()
		u.reg.p2.clear()
		u.code = ExitOK
		return true
	case ev.k == kEnter:
		if len(f.rs) == 0 {
			u.setMsg(msgErr, "입력이 비어 있습니다")
			return false
		}
		if u.reg.step == 0 {
			u.reg.step = 1
			u.msg = ""
			return false
		}
		p1, p2 := u.reg.p1.text(), u.reg.p2.text()
		u.reg.p1.clear()
		u.reg.p2.clear()
		u.reg.step = 0
		retry := func(format string, a ...any) bool {
			u.reg.tries++
			if u.reg.tries >= 3 {
				u.code = ExitFail
				return true
			}
			u.setMsg(msgErr, format+fmt.Sprintf(" — 다시 입력하세요 (%d/3)", u.reg.tries), a...)
			return false
		}
		if p1 != p2 {
			return retry("두 입력이 서로 다릅니다")
		}
		if err := daemon.CheckPassword(p1); err != nil {
			return retry("%v", err)
		}
		err := u.c.Register(u.user, p1)
		if err == nil {
			u.mode = umList
			u.setMsg(msgOK, "[등록 완료] %s 의 패스워드가 등록되었습니다", u.user)
			u.refresh()
			return false
		}
		msg, ok := isDaemonErr(err)
		switch {
		case ok && msg == daemon.ErrAlreadyRegistered.Error():
			u.mode = umList
			u.setMsg(msgInfo, "[등록 충돌] %s 은(는) 방금 다른 곳에서 먼저 등록되었습니다 — 그 패스워드를 쓰거나 관리자에게 재설정을 요청하세요", u.user)
			return false
		case ok && strings.Contains(msg, "비밀번호"):
			return retry("등록 거부: %s", msg)
		case ok:
			u.setMsg(msgErr, "[X] 등록 거부: %s", msg)
			u.code = ExitFail
			return true
		default:
			u.setMsg(msgErr, "[X] 등록 요청 실패: %v", err)
			u.code = ExitError
			return true
		}
	default:
		f.key(ev)
	}
	return false
}

// ---- 그리기 ----

func (u *userScreen) render(w, h int, color bool) string {
	w = clampWidth(w)
	switch u.mode {
	case umRegister:
		return u.renderRegister(w, h, color)
	case umApprove:
		return u.renderApprove(w, h, color)
	case umCancel:
		return u.renderCancel(w, h, color)
	case umPassword:
		return u.renderPassword(w, h, color)
	case umHelp:
		return u.renderHelp(w, h, color)
	case umDetail:
		if _, ok := u.find(u.detID); ok {
			return u.renderDetail(w, h, color)
		}
		u.mode = umList
	}
	return u.renderList(w, h, color)
}

func (u *userScreen) head(w int, color bool, kinds []stateKind) []string {
	head := titleLines(color, w, "cross_check 상태 리포트", "user: "+u.user, u.ds, u.loc, counterSegs(kinds), ttlSegs(u.info.ApprovalTTL, 0, false))
	return append(head, sepLine(w))
}

func (u *userScreen) renderList(w, h int, color bool) string {
	vis := u.visible()
	kinds := make([]stateKind, 0, len(vis))
	rows := make([]tableRow, 0, len(vis))
	for _, j := range vis {
		k := kindOf(j.Status, j.Phase)
		kinds = append(kinds, k)
		rows = append(rows, tableRow{id: j.ID, worker: j.User, checker: j.Checker, label: daemon.StatusLabel(j.Status, j.Phase),
			caller: callerText(j.Caller), at: shortTime(j.UpdatedAt, u.loc), kind: k})
	}
	head := u.head(w, color, kinds)

	var mine, chk, chkWait int
	for _, j := range u.jobs {
		if j.User == u.user {
			mine++
		}
		if j.Checker == u.user {
			chk++
			if j.Status == daemon.StatusWaiting {
				chkWait++
			}
		}
	}
	var bar []seg
	tab := func(active bool, t string) seg {
		if active {
			return seg{"[" + t + "]", stBold}
		}
		return seg{" " + t + " ", stGray}
	}
	if u.all {
		bar = []seg{{" ", ""}, tab(true, fmt.Sprintf("전체 보기 %d", len(vis))), {"  (a: 작업별 보기)", stGray}}
	} else {
		bar = []seg{{" ", ""}, tab(u.pane == 0, fmt.Sprintf("내가 확인자인 작업 %d", chk)), {"  ", ""},
			tab(u.pane == 1, fmt.Sprintf("내 작업 (작업자) %d", mine))}
	}
	if chkWait > 0 {
		bar = append(bar, seg{"  ", ""}, seg{fmt.Sprintf("승인 대기 %d건", chkWait), stYellow})
	}
	body := [][]string{wrapSegs(color, w, 2, "  ", bar...)}
	cursor := 0
	if len(rows) == 0 {
		empty := " (작업 없음 — 작업자가 작업 스크립트에서 cross_check --run 으로 요청하면 여기에 표시됩니다)"
		if !u.all && u.pane == 1 {
			empty = " (내가 작업자인 작업이 없습니다 — 작업 스크립트가 cross_check --run 을 부르면 표시됩니다)"
		} else if !u.all {
			empty = " (내가 확인자인 작업이 없습니다 — 작업자가 나를 확인자로 지정하면 표시됩니다)"
		}
		body = append(body, wrapText(color, w, stGray, "   ", empty))
	} else {
		tbl := jobTable(color, w, rows, u.row, "")
		body = append(body, tbl...)
		cursor = 2 + u.row
	}
	var foot []string
	msg, kind := u.msg, u.msgKind
	if msg == "" && u.err != "" {
		msg, kind = "[X] 상태 갱신 실패: "+u.err+" (데몬 확인: cross_check --ensure)", msgErr
	}
	foot = append(foot, msgLines(color, w, msg, kind)...)
	foot = append(foot, hintLines(color, w, UserHint)...)
	return finish(head, body, cursor, h, foot)
}

func (u *userScreen) renderDetail(w, h int, color bool) string {
	j, _ := u.find(u.detID)
	role := "작업자"
	if j.Checker == u.user && j.User != u.user {
		role = "확인자"
	}
	k := kindOf(j.Status, j.Phase)
	var body []string
	add := func(name string, val seg) { body = append(body, kv(color, w, name, val)...) }
	add("내 역할", seg{role, ""})
	add("작업자", seg{j.User, ""})
	add("확인자", seg{j.Checker, ""})
	add("상태", seg{daemon.StatusLabel(j.Status, j.Phase), kindStyle[k]})
	if j.Reason != "" {
		add("사유", seg{j.Reason, ""})
	}
	add("호출 스크립트", seg{callerText(j.Caller), ""})
	add("생성", seg{longTime(&j.CreatedAt, u.loc), ""})
	add("승인", seg{longTime(j.ApprovedAt, u.loc), ""})
	add("작업 시작", seg{longTime(j.StartedAt, u.loc), ""})
	add("작업 종료", seg{longTime(j.FinishedAt, u.loc), ""})
	if r := remainText(j, u.info.ApprovalTTL, u.now()); r != "" {
		add("승인 유효시간", seg{r, stYellow})
	}
	body = append(body, "")
	if j.User == u.user {
		add("대상 VM", seg{fmt.Sprintf("%d개", len(j.Targets)), ""})
		for i, t := range j.Targets {
			body = append(body, segLine(color, w, seg{fmt.Sprintf("    %3d) ", i+1), stGray}, seg{t, ""}))
		}
	} else {
		add("대상 VM", seg{"확인자에게는 표시되지 않습니다 — 작업자에게 전달받은 대상을 붙여넣어 승인합니다(데몬이 대조)", stGray})
	}
	hint := "↑↓ 스크롤  Esc 목록  r 새로고침"
	if u.canDecide(j) {
		hint = "Enter 승인(대상 붙여넣기)  c 취소  ↑↓ 스크롤  Esc 목록"
	}
	return renderFlow(flowFrame{Title: fmt.Sprintf("작업 %s 상세  (user: %s)", j.ID, u.user), Body: body,
		Msg: u.msg, MsgErr: u.msgKind == msgErr, MsgOK: u.msgKind == msgOK, Hint: hint}, &u.detScroll, w, h, color)
}

func (u *userScreen) renderApprove(w, h int, color bool) string {
	f := flowFrame{Msg: u.msg, MsgErr: u.msgKind == msgErr, MsgOK: u.msgKind == msgOK}
	j, found := u.find(u.ap.id)
	stale := ""
	if u.ap.step != apResult && found && j.Status != daemon.StatusWaiting {
		stale = fmt.Sprintf("이 작업은 더 이상 대기 상태가 아닙니다(현재 %s%s) — Esc 로 돌아가세요", daemon.StatusLabel(j.Status, j.Phase), reasonSuffix(j.Reason))
	}
	switch u.ap.step {
	case apPaste:
		f.Title = fmt.Sprintf("작업 %s 승인 — 1/3 대상 붙여넣기  (확인자 %s)", u.ap.id, u.user)
		f.Head = append(f.Head, wrapText(color, w, "", "   ", fmt.Sprintf(" 작업자 %s 에게 전달받은 작업 대상을 붙여넣으세요. 줄바꿈·탭·공백 어느 것으로 구분해도 됩니다.", u.ap.worker))...)
		f.Head = append(f.Head, wrapText(color, w, stGray, "   ", " 대상 목록은 이 화면에 표시되지 않습니다. 대조는 데몬이 하며 결과는 패스워드 확인 뒤에 나옵니다.")...)
		f.Head = append(f.Head, "")
		for _, l := range u.ap.box.lines {
			f.Body = append(f.Body, segLine(color, w, seg{"   │ ", stGray}, seg{strings.ReplaceAll(l, "\t", " "), ""}))
		}
		// 입력 중인 줄(가짜 커서 "_")은 붙여넣은 줄들 바로 아래 — 본문은 끝을 따라간다
		f.Body = append(f.Body, segLine(color, w, seg{"   │ ", stGray}, seg{lastCols(strings.ReplaceAll(string(u.ap.box.cur), "\t", " "), w-7), ""}, seg{"_", stBold}))
		f.Foot = append(f.Foot, segLine(color, w, seg{" 인식한 이름: ", stBold}, seg{fmt.Sprintf("%d개", u.ap.box.count()), stCyanB},
			seg{"   (대조 결과는 패스워드 확인 뒤에 나옵니다)", stGray}))
		f.Hint = "Ctrl+D 완료  Enter 줄바꿈(빈 줄에서 Enter = 완료)  Backspace 지우기  Esc 돌아가기"
		u.ap.scroll.Follow = u.ap.scroll.Follow || len(f.Body) == 0
	case apPass:
		f.Title = fmt.Sprintf("작업 %s 승인 — 2/3 패스워드  (확인자 %s)", u.ap.id, u.user)
		f.Head = append(f.Head, wrapText(color, w, "", "   ", fmt.Sprintf(" 붙여넣은 이름 %d개를 보냅니다. 확인자 %s 의 패스워드를 입력하세요(화면에는 * 로 보입니다).", u.ap.box.count(), u.user))...)
		f.Head = append(f.Head, wrapText(color, w, stGray, "   ", " 패스워드를 먼저 확인한 뒤 대상을 대조합니다. 대상이 다르면 작업은 중단되고 작업자에게 알려집니다.")...)
		f.Foot = append(f.Foot, u.ap.pw.line(color, w, "패스워드"))
		f.Hint = "Enter 승인 요청  Esc 붙여넣기로 돌아가기"
	default:
		f.Title = fmt.Sprintf("작업 %s 승인 — 3/3 결과", u.ap.id)
		if u.ap.ok {
			f.Body = append(f.Body, wrapText(color, w, stGreen, "   ", " "+u.ap.result)...)
			f.Body = append(f.Body, "")
			f.Body = append(f.Body, wrapText(color, w, stBold, "   ", " "+PolicyText(jobTTL(j, u.info.ApprovalTTL)))...)
		} else {
			f.Body = append(f.Body, wrapText(color, w, stRed, "   ", " "+u.ap.result)...)
		}
		f.Hint = "Enter 목록으로"
	}
	if stale != "" && f.Msg == "" {
		f.Msg, f.MsgErr = stale, true
	}
	return renderFlow(f, &u.ap.scroll, w, h, color)
}

// lastCols 는 글의 끝쪽 w 칸(입력 중인 줄이 길 때 커서 쪽을 보이게).
func lastCols(s string, w int) string {
	s = cleanText(s)
	for w > 0 && strWidth(s) > w {
		_, n := firstRune(s)
		s = s[n:]
	}
	return s
}

func reasonSuffix(r string) string {
	if r == "" {
		return ""
	}
	return ": " + r
}

func (u *userScreen) renderCancel(w, h int, color bool) string {
	f := flowFrame{Title: fmt.Sprintf("작업 %s 취소(중단)  (확인자 %s)", u.cn.id, u.user),
		Msg: u.msg, MsgErr: u.msgKind == msgErr, MsgOK: u.msgKind == msgOK,
		Hint: "Enter 취소 실행  Esc 돌아가기"}
	f.Head = append(f.Head, wrapText(color, w, stRed, "   ", fmt.Sprintf(" 이 작업을 중단합니다. 작업자 %s 의 스크립트도 중단됩니다.", u.cn.worker))...)
	f.Head = append(f.Head, wrapText(color, w, "", "   ", " 확인자 패스워드를 입력하면 취소합니다.")...)
	f.Foot = append(f.Foot, u.cn.pw.line(color, w, "패스워드"))
	return renderFlow(f, nil, w, h, color)
}

func (u *userScreen) renderPassword(w, h int, color bool) string {
	labels := []string{"현재 패스워드", "새 패스워드", "새 패스워드 확인"}
	f := flowFrame{Title: fmt.Sprintf("패스워드 변경  (user: %s)", u.user),
		Msg: u.msg, MsgErr: u.msgKind == msgErr, MsgOK: u.msgKind == msgOK, Hint: "Enter 다음  Esc 취소"}
	f.Head = append(f.Head, wrapText(color, w, "", "   ", fmt.Sprintf(" 규칙: %d~%d자, 앞뒤 공백·줄바꿈 금지. 분실하면 관리자(cross_check --admin)에게 재설정을 요청하세요.", daemon.MinPasswordLen, daemon.MaxPasswordLen))...)
	fs := []*field{&u.pc.cur, &u.pc.n1, &u.pc.n2}
	for i := 0; i <= u.pc.step; i++ {
		f.Foot = append(f.Foot, fs[i].line(color, w, padR(labels[i], 16)))
	}
	return renderFlow(f, nil, w, h, color)
}

func (u *userScreen) renderRegister(w, h int, color bool) string {
	f := flowFrame{Title: fmt.Sprintf("cross_check 첫 실행 — 패스워드 등록  (user: %s)", u.user),
		Msg: u.msg, MsgErr: u.msgKind == msgErr, MsgOK: u.msgKind == msgOK, Hint: "Enter 다음  Esc 등록하지 않고 종료"}
	for _, t := range []string{
		fmt.Sprintf(" %s 은(는) 아직 cross_check 에 등록되지 않았습니다. 사용할 패스워드를 두 번 입력하세요(화면에는 * 로 보입니다).", u.user),
		" 등록은 선착순이며 이후에는 덮어쓸 수 없습니다. 변경: 목록 화면의 p (현재 패스워드 필요) / 분실: 관리자에게 재설정 요청",
		fmt.Sprintf(" 규칙: %d~%d자, 앞뒤 공백·줄바꿈 금지", daemon.MinPasswordLen, daemon.MaxPasswordLen),
	} {
		f.Head = append(f.Head, wrapText(color, w, "", "   ", t)...)
	}
	f.Foot = append(f.Foot, u.reg.p1.line(color, w, padR("새 패스워드", 16)))
	if u.reg.step == 1 {
		f.Foot = append(f.Foot, u.reg.p2.line(color, w, padR("새 패스워드 확인", 16)))
	}
	return renderFlow(f, nil, w, h, color)
}

func (u *userScreen) renderHelp(w, h int, color bool) string {
	ttl := u.info.ApprovalTTL
	lines := []string{
		" [목록]",
		"   ↑↓ (j/k)  작업 이동            PgUp/PgDn  한 쪽씩",
		"   ←→ / Tab  구역 전환: 내가 확인자인 작업 ↔ 내 작업(작업자) (Shift+Tab 은 이전)",
		"   a         전체 보기(두 구역을 함께) ↔ 작업별 보기",
		"   Enter     내가 확인자인 대기 작업이면 승인 화면, 아니면 상세",
		"   c         취소(중단) — 내가 확인자인 대기 작업, 패스워드 필요",
		"   r         새로고침(화면은 2초마다 자동으로 갱신됩니다)",
		"   p         내 패스워드 변경",
		"   q / Esc   종료",
		"",
		" [승인 — 확인자]",
		"   1/3  작업자에게 전달받은 대상을 붙여넣습니다(줄바꿈·탭·공백 구분). 화면에는 인식한 이름 수만 보입니다.",
		"        Ctrl+D 또는 빈 줄에서 Enter 로 끝냅니다. Esc 는 승인하지 않고 돌아갑니다.",
		"   2/3  확인자 패스워드를 입력합니다(* 로 보임). 틀리면 상태는 그대로이고 다시 입력합니다.",
		"   3/3  결과: 대상이 일치하면 승인, 다르면 작업이 중단되고 작업자에게 알려집니다(개수만 표시).",
		"",
		" [상태]",
		"   대기 → 승인(본 작업 전) → 승인/작업중(--start) → 승인/작업완료 또는 승인/작업중단",
		"   중단: 취소·대상 불일치·작업자 종료·승인 유효시간 초과 등",
		"",
		" " + PolicyText(ttl),
	}
	var body []string
	for _, l := range lines {
		body = append(body, wrapText(color, w, "", "              ", l)...)
	}
	return renderFlow(flowFrame{Title: "cross_check 도움말  (user: " + u.user + ")", Body: body, Hint: "↑↓ 스크롤  아무 키나 누르면 목록으로"}, &u.helpScroll, w, h, color)
}

// ---- 진입점 ----

// RunUserFull 은 cross_check --user <user> 를 전체 화면으로 돌린다. raw 모드로 바꾸지 못하면 ok=false
// (화면에 아무것도 쓰지 않았으므로 호출자가 줄 입력 화면으로 대신한다).
// 등록 여부를 먼저 확인하고(데몬 오류면 화면을 열지 않고 종료 코드를 돌려준다), 미등록이면 화면 안에서 등록부터 한다.
func RunUserFull(env Env, c Client, user string) (code int, ok bool) {
	reg, err := c.UserRegistered(user)
	if err != nil {
		if msg, isD := isDaemonErr(err); isD {
			fmt.Fprintln(env.Out, "[오류] 등록 여부를 확인할 수 없습니다: "+msg)
			return ExitFail, true
		}
		fmt.Fprintln(env.Out, "[오류] "+err.Error())
		return ExitError, true
	}
	m := newUserScreen(c, user, reg, env.Now, env.Loc)
	code, err = runLoop(env, m)
	var rf rawFailed
	if errors.As(err, &rf) {
		return 0, false
	}
	if code != ExitOK && m.msg != "" { // 등록 실패 등: 대체 화면을 떠난 뒤에도 이유가 남게
		fmt.Fprintln(env.Out, m.msg)
	}
	return code, true
}
