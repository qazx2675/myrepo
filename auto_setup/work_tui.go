// work_tui.go - v0.8.0 gossh 작업 화면 (TUI s / CLI auto_setup s). 로직·저장·실행은 work.go, 화면 틀·입력은 flow_tui.go.
//
// 흐름: user(needSessionUser / CLI askUserCLI) → 작업 목록(파일별 묶음, 최신이 위)
//   - n 새 작업: 호스트 붙여넣기 → (같은 목록이 있으면 재사용? y/n) → 명령 한 줄 → TUI 를 떠나 터미널에서 gossh 실행 → 목록
//   - Enter: 1 같은 파일로 새 명령  2 재실행  3 실패·접속불가만 재실행(새 파일, 파생 표시)  4 결과 보기(less -R)  5 메모  6 호스트 목록
//   - / 호스트 검색: 그 호스트가 들어 있던 작업과 명령을 시간순으로
//
// 실행은 awxrun.go 의 runAwxEnv 와 같은 방식으로 TUI 를 잠시 떠난다(workOutside): 키 읽기 정지 → 대체 화면 종료·termios 복원 →
// script 로 감싼 gossh(터미널 그대로) → Enter 대기 → raw·대체 화면 재진입.
package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"
)

// 화면 단계
const (
	wkList     = iota // 작업 목록
	wkMenu            // 항목 메뉴 (Enter)
	wkPaste           // n: 호스트 붙여넣기
	wkReuse           // 같은 목록 재사용? y/n
	wkCmd             // 명령 입력
	wkAsk             // y/n 확인 (재실행·위험 명령·os6 미설정 대량)
	wkMemo            // 메모 입력
	wkHosts           // 호스트 목록 보기
	wkSearchIn        // / 검색어 입력
	wkSearch          // 검색 결과
)

// workDraft: 실행 준비 중인 작업
type workDraft struct {
	File, Parent string
	Hosts        []string
	NewList      bool // 새 /tmp 파일 (이름만 정해 둠 — 실행 직전에 만든다)
	Cmd          string
	Danger       bool
	Plan         workPlanT
}

type workNote struct{ style, text string }

// workState: gossh 작업 화면 하나
type workState struct {
	user   string
	client bool      // os6_mgmt 원격 클라이언트 (기록은 os8_mgmt)
	store  workStore // 기록 저장소
	local  workStore // 원격 클라이언트에서 os8 전송이 안 될 때 보관할 이 서버의 저장소 (os8_mgmt 면 nil)
	db     *workDB
	gs     []workGroup
	flat   []*workRec // 목록 화면 순서의 실행 줄
	notes  []workNote // 목록 머리 안내 (경로·경고)
	now    func() time.Time
	loc    *time.Location

	step, back int // back = 메뉴·확인을 닫으면 돌아갈 화면 (wkList | wkSearch)
	sc         flowScroll
	cur, scur  int // 목록·검색 결과 커서
	msg        string
	msgErr     bool
	paste      pasteBuf
	pasted     []string
	pastedSet  map[string]bool
	reuse      string // 재사용 후보 파일
	in         pasteBuf
	query      string
	found      []*workRec
	sel        *workRec
	draft      workDraft
	ask        string
	askYes     func()

	pending *workDraft // 터미널에서 실행할 작업 (tuiState.pollWork 가 꺼냄)
	view    string     // 결과 보기 요청 (실행 id)
	closed  bool
}

func (w *workState) setMsg(m string) { w.msg, w.msgErr = m, false }
func (w *workState) setErr(m string) { w.msg, w.msgErr = m, true }

// newWorkState: user 가 정해진 뒤 화면을 연다 — 기록을 읽고 가볍게 정리(출력 7일·200MB, /tmp 7일)
func newWorkState(user string, client bool, now func() time.Time) *workState {
	w := &workState{user: user, client: client, now: now, loc: time.Local}
	dir, err := workUserDir(user)
	if err != nil {
		w.db = &workDB{Memo: map[string]string{}, Lists: map[string][]string{}}
		w.setErr("[X] " + err.Error())
		return w
	}
	local := localWorkStore{dir}
	w.store = local
	if client {
		w.store, w.local = remoteWorkStore{user: user}, local
	}
	w.reload()
	if !client {
		workCleanOut(dir, now())
	}
	workCleanTmp(w.db, now())
	w.notes = workNotes(client)
	return w
}

// workNotes: 목록 머리 안내 — 실행 위치 규칙, work_os6_over 경고, 보관 규칙
func workNotes(client bool) []workNote {
	var n []workNote
	over, problems := parseWorkOS6Over()
	switch {
	case client:
		n = append(n, workNote{"", fmt.Sprintf("os6_mgmt 에서 실행 — 대수와 상관없이 이 서버의 gossh 로 바로 실행, 기록은 os8_mgmt(%s) 에 보관", os8_mgmt)})
	case os6_mgmt != "" && os6_gossh != "":
		n = append(n, workNote{"", fmt.Sprintf("실행: %s대 이하 os8 gossh (접속불가는 os6_mgmt 경유 재실행) · 초과는 전량 os6_mgmt(%s) 경유", commaN(over), os6_mgmt)})
	default:
		n = append(n, workNote{stYellow, "os6_mgmt/os6_gossh 미설정 — os8 gossh 로만 실행 (접속불가 재실행·전량 os6 불가)"})
	}
	for _, p := range problems {
		n = append(n, workNote{stYellow, "⚠ " + p})
	}
	return append(n, workNote{stGray, "요약은 영구 보관 · 전체 출력은 7일(user 당 200MB) · 목록 파일(" + workListDir() + ")은 7일 뒤 정리"})
}

// reload: 기록을 다시 읽는다. 원격 클라이언트에서 os8 를 읽지 못하면 이 서버의 기록으로 바꿔 보여 준다.
func (w *workState) reload() {
	db, err := w.store.Load()
	if err != nil && w.local != nil {
		w.setErr("[X] os8_mgmt 기록을 읽지 못했습니다: " + err.Error() + " — 이 서버(os6_mgmt)의 기록을 보여 줍니다")
		w.store, w.local = w.local, nil
		db, err = w.store.Load()
	}
	if err != nil {
		w.setErr("[X] 기록 읽기 실패: " + err.Error())
	}
	w.setDB(db)
}

// setDB: 기록을 바꾸고 목록 화면 순서를 다시 만든다
func (w *workState) setDB(db *workDB) {
	if db == nil {
		db = &workDB{}
	}
	if db.Memo == nil {
		db.Memo = map[string]string{}
	}
	if db.Lists == nil {
		db.Lists = map[string][]string{}
	}
	w.db = db
	w.gs = db.groups()
	w.flat = nil
	for _, g := range w.gs {
		w.flat = append(w.flat, g.Runs...)
	}
	w.cur = rMax(rMin(w.cur, len(w.flat)-1), 0)
}

// ---------------------------------------------------------------- 키

func (w *workState) key(ev keyEv) {
	switch w.step {
	case wkList:
		w.keyList(ev)
	case wkMenu:
		w.keyMenu(ev)
	case wkPaste:
		w.keyPaste(ev)
	case wkReuse:
		w.keyReuse(ev)
	case wkCmd:
		w.keyCmd(ev)
	case wkAsk:
		w.keyAsk(ev)
	case wkMemo:
		w.keyMemo(ev)
	case wkHosts:
		if ev.k == kEsc || (ev.k == kRune && (ev.r == 'q' || ev.r == 'Q')) {
			w.step = w.back
			return
		}
		w.sc.key(ev)
	case wkSearchIn:
		w.keySearchIn(ev)
	case wkSearch:
		w.keySearch(ev)
	}
}

// moveCur: ↑↓ PgUp PgDn 으로 커서 이동 (처리했으면 true)
func (w *workState) moveCur(ev keyEv, cur *int, n int) bool {
	page := w.sc.Page
	if page < 1 {
		page = 10
	}
	switch {
	case ev.k == kUp || (ev.k == kRune && ev.r == 'k'):
		*cur--
	case ev.k == kDown || (ev.k == kRune && ev.r == 'j'):
		*cur++
	case ev.k == kPgUp:
		*cur -= page
	case ev.k == kPgDn:
		*cur += page
	default:
		return false
	}
	*cur = rMax(rMin(*cur, n-1), 0)
	return true
}

func (w *workState) keyList(ev keyEv) {
	if w.moveCur(ev, &w.cur, len(w.flat)) {
		return
	}
	w.msg = ""
	switch {
	case ev.k == kEnter:
		if len(w.flat) > 0 {
			w.sel, w.back, w.step = w.flat[w.cur], wkList, wkMenu
		}
	case ev.k == kRune && (ev.r == 'n' || ev.r == 'N'):
		w.paste, w.pasted, w.pastedSet, w.back = pasteBuf{}, nil, map[string]bool{}, wkList
		w.sc = flowScroll{Follow: true}
		w.step = wkPaste
	case ev.k == kRune && ev.r == '/':
		w.in = pasteBuf{}
		w.step = wkSearchIn
	case ev.k == kEsc || (ev.k == kRune && (ev.r == 'q' || ev.r == 'Q')):
		w.closed = true
	}
}

// workMenuItems: 항목 메뉴
var workMenuItems = []string{"1 같은 파일로 새 명령", "2 재실행", "3 실패·접속불가만 재실행", "4 결과 보기", "5 메모", "6 호스트 목록 보기"}

func (w *workState) keyMenu(ev keyEv) {
	if ev.k == kEsc || (ev.k == kRune && (ev.r == 'q' || ev.r == 'Q')) {
		w.step = w.back
		return
	}
	if ev.k != kRune {
		return
	}
	r := w.sel
	list := w.db.Lists[r.File]
	switch ev.r {
	case '1', '2':
		if len(list) == 0 {
			w.setErr("[X] " + r.File + " 의 호스트 목록 사본이 없습니다 — n 으로 새 작업을 만드세요")
			w.step = w.back
			return
		}
		d := workDraft{File: r.File, Parent: "", Hosts: list}
		if ev.r == '1' {
			w.toCmd(d)
			return
		}
		d.Cmd = r.Cmd
		w.confirm(fmt.Sprintf("재실행: %s (%s대) — %s   [y/n]", workPath(r.File), commaN(len(list)), r.Cmd), func() { w.submit(d) })
	case '3':
		hosts := workRetryHosts(r, list)
		if len(hosts) == 0 {
			w.setMsg("실패·접속불가 호스트가 없습니다")
			w.step = w.back
			return
		}
		name, err := workNewName(w.db)
		if err != nil {
			w.setErr("[X] " + err.Error())
			w.step = w.back
			return
		}
		d := workDraft{File: name, Parent: r.File, NewList: true, Hosts: hosts, Cmd: r.Cmd}
		w.confirm(fmt.Sprintf("실패·접속불가 %s대만 새 파일 %s 로 재실행 (%s 에서 파생) — %s   [y/n]", commaN(len(hosts)), workPath(name), r.File, r.Cmd),
			func() { w.submit(d) })
	case '4':
		w.view = r.ID
		w.step = w.back
	case '5':
		w.in = pasteBuf{}
		w.in.set(w.db.Memo[r.File])
		w.step = wkMemo
	case '6':
		w.sc = flowScroll{}
		w.step = wkHosts
	}
}

func (w *workState) keyPaste(ev keyEv) {
	pe, line := w.paste.key(ev)
	switch pe {
	case pasteLine:
		w.addHosts(line)
	case pasteEnd:
		w.addHosts(line)
		w.donePaste()
	case pasteNone:
		if ev.k == kEsc {
			w.step = wkList
			w.setMsg("새 작업을 취소했습니다")
			return
		}
		w.sc.key(ev)
	}
}

func (w *workState) addHosts(line string) {
	for _, h := range workParseHosts(line) {
		if !w.pastedSet[h] && len(w.pasted) < workMaxHosts {
			w.pastedSet[h] = true
			w.pasted = append(w.pasted, h)
		}
	}
}

func (w *workState) donePaste() {
	if len(w.pasted) == 0 {
		w.step = wkList
		w.setMsg("호스트를 입력하지 않아 닫았습니다")
		return
	}
	if f := w.db.sameList(w.pasted); f != "" {
		w.reuse = f
		w.step = wkReuse
		return
	}
	w.newList()
}

// newList: 붙여넣은 목록으로 새 /tmp 이름을 정하고 명령 입력으로
func (w *workState) newList() {
	name, err := workNewName(w.db)
	if err != nil {
		w.setErr("[X] " + err.Error())
		w.step = wkList
		return
	}
	w.toCmd(workDraft{File: name, NewList: true, Hosts: w.pasted})
}

func (w *workState) keyReuse(ev keyEv) {
	switch {
	case ev.k == kRune && (ev.r == 'y' || ev.r == 'Y'):
		w.toCmd(workDraft{File: w.reuse, Hosts: w.db.Lists[w.reuse]})
	case ev.k == kRune && (ev.r == 'n' || ev.r == 'N'):
		w.newList()
	case ev.k == kEsc:
		w.step = wkPaste
	}
}

func (w *workState) toCmd(d workDraft) {
	w.draft = d
	w.in = pasteBuf{}
	w.msg = ""
	w.step = wkCmd
}

func (w *workState) keyCmd(ev keyEv) {
	pe, line := w.in.key(ev)
	switch {
	case pe == pasteLine || (pe == pasteEnd && line != ""):
		d := w.draft
		d.Cmd = line
		w.submit(d)
	case pe == pasteEnd:
		w.setErr("명령을 입력하세요 (취소는 Esc)")
	case pe == pasteNone && ev.k == kEsc:
		w.step = wkList
		w.setMsg("취소했습니다")
	}
}

// confirm: y/n 확인 — y 면 yes, n/Esc 면 돌아갈 화면으로
func (w *workState) confirm(q string, yes func()) {
	w.ask, w.askYes = q, yes
	w.step = wkAsk
}

func (w *workState) keyAsk(ev keyEv) {
	switch {
	case ev.k == kRune && (ev.r == 'y' || ev.r == 'Y'):
		f := w.askYes
		w.step = w.back
		f()
	case ev.k == kEsc || (ev.k == kRune && (ev.r == 'n' || ev.r == 'N')):
		w.step = w.back
		w.setMsg("취소했습니다")
	}
}

// submit: 경로를 정하고 (위험 명령·os6 미설정 대량이면 확인 후) 실행 대기열로
func (w *workState) submit(d workDraft) {
	d.Plan = workPlan(len(d.Hosts), w.client)
	d.Danger = workDanger(d.Cmd)
	queue := func() {
		dd := d
		w.pending = &dd
		w.step = w.back
	}
	next := queue
	if d.Plan.Ask != "" {
		next = func() { w.confirm(d.Plan.Ask, queue) }
	}
	if d.Danger {
		w.confirm("위험 명령(reboot·halt 등)이 들어 있습니다 — gossh 의 위험 작업 확인(대상 목록 + y/N)을 거쳐 실행합니다. 계속? [y/n]", next)
		return
	}
	next()
}

func (w *workState) keyMemo(ev keyEv) {
	pe, line := w.in.key(ev)
	switch {
	case pe == pasteLine || pe == pasteEnd:
		if err := w.store.SetMemo(w.sel.File, line); err != nil {
			w.setErr("[X] 메모 저장 실패: " + err.Error())
		} else if line == "" {
			w.setMsg(w.sel.File + " 의 메모를 지웠습니다")
		} else {
			w.setMsg(w.sel.File + " 의 메모를 저장했습니다")
		}
		w.reload()
		w.step = w.back
	case pe == pasteNone && ev.k == kEsc:
		w.step = w.back
	}
}

func (w *workState) keySearchIn(ev keyEv) {
	pe, line := w.in.key(ev)
	switch {
	case pe == pasteLine || (pe == pasteEnd && line != ""):
		w.query = line
		w.found = w.db.search(line)
		w.scur = 0
		w.sc = flowScroll{}
		w.step = wkSearch
		if len(w.found) == 0 {
			w.setMsg(line + " 가 들어 있던 작업이 없습니다")
		} else {
			w.msg = ""
		}
	case pe == pasteEnd || (pe == pasteNone && ev.k == kEsc):
		w.step = wkList
	}
}

func (w *workState) keySearch(ev keyEv) {
	if w.moveCur(ev, &w.scur, len(w.found)) {
		return
	}
	switch {
	case ev.k == kEnter:
		if len(w.found) > 0 {
			w.sel, w.back, w.step = w.found[w.scur], wkSearch, wkMenu
		}
	case ev.k == kRune && ev.r == '/':
		w.in = pasteBuf{}
		w.step = wkSearchIn
	case ev.k == kEsc || (ev.k == kRune && (ev.r == 'q' || ev.r == 'Q')):
		w.step, w.back = wkList, wkList
	}
}

// ---------------------------------------------------------------- tuiState 연결

// openWork: user 가 정해지면 화면을 연다
func (st *tuiState) openWork(user string) {
	st.wk = newWorkState(user, st.changeRemote(), st.env.Now)
}

func (st *tuiState) handleWork(ev keyEv) {
	st.wk.key(ev)
	st.pollWork()
}

// pollWork: 실행·결과 보기 요청을 처리하고, 닫혔으면 정리
func (st *tuiState) pollWork() {
	w := st.wk
	if w == nil {
		return
	}
	if w.pending != nil {
		d := *w.pending
		w.pending = nil
		st.runWorkDraft(d)
	}
	if w.view != "" {
		id := w.view
		w.view = ""
		st.viewWorkOut(id)
	}
	if w.closed {
		st.wk = nil
	}
}

// workPrepareTmp: 실행 직전에 /tmp/<파일> 을 준비한다 — 새 목록은 새로 만들고(이름이 그새 생겼으면 다른 이름),
// 기존 목록은 없으면 사본으로 다시 만들고, 내용이 사본과 다르면(누가 고침) 건드리지 않고 새 이름으로 파생시킨다.
func workPrepareTmp(db *workDB, d *workDraft) (note string, err error) {
	if !d.NewList {
		exists, same := workTmpMatches(d.File, d.Hosts)
		switch {
		case same:
			return "", nil
		case !exists:
			return "", workWriteTmp(d.File, d.Hosts, true)
		}
		note = fmt.Sprintf("%s 내용이 기록과 달라 그대로 두고 ", workPath(d.File))
		d.Parent, d.NewList = d.File, true
		if d.File, err = workNewName(db); err != nil {
			return "", err
		}
	}
	for try := 0; ; try++ {
		err = workWriteTmp(d.File, d.Hosts, true)
		if err == nil || !os.IsExist(err) || try >= 5 {
			break
		}
		if d.File, err = workNewName(db); err != nil {
			return "", err
		}
	}
	if note != "" && err == nil {
		note += workPath(d.File) + " 로 실행했습니다 — "
	}
	return note, err
}

// workPause: 실행이 끝난 뒤 터미널에서 Enter 를 기다린다 (출력을 읽을 시간 — 테스트에서 바꿀 수 있음)
var workPause = func(in io.Reader, out io.Writer) {
	fmt.Fprint(out, "[auto_setup] Enter 를 누르면 gossh 작업 목록으로 돌아갑니다 (전체 출력은 Enter → 4 결과 보기) ")
	readLineRaw(in)
}

// workOutside: TUI 를 잠시 떠나 f 를 실행하고 돌아온다 (runAwxEnv 와 같은 순서 — awxrun.go 주석 참고).
// 외부 명령(script·less)이 터미널을 직접 쓰므로 TUI 의 raw 모드를 원래 termios(cooked, intr=^C)로 되돌린 채 실행한다.
func (st *tuiState) workOutside(f func(out io.Writer)) string {
	out := st.env.Out
	st.inMu.Lock()
	io.WriteString(out, leaveScreen)
	st.restore()
	f(out)
	if st.env.Signals != nil { // 자식이 받은 ^C 등이 Notify 채널에 남아 TUI 가 끝나지 않게 비운다
		for drained := false; !drained; {
			select {
			case <-st.env.Signals:
			default:
				drained = true
			}
		}
	}
	msg := ""
	if r, err := st.env.MakeRaw(); err == nil {
		st.restore = r
	} else {
		st.restore = func() {}
		msg = "[X] 터미널을 raw 모드로 되돌리지 못했습니다: " + err.Error() + " — q 로 종료하세요. "
	}
	io.WriteString(out, enterScreen)
	st.inMu.Unlock()
	return msg
}

// runWorkDraft: /tmp 파일을 준비하고 터미널에서 실행 → 기록 → 목록으로
func (st *tuiState) runWorkDraft(d workDraft) {
	w := st.wk
	note, err := workPrepareTmp(w.db, &d)
	if err != nil {
		w.setErr("[X] " + workPath(d.File) + " 를 만들지 못했습니다: " + err.Error())
		return
	}
	req := workRunReq{User: w.user, File: d.File, Parent: d.Parent, Hosts: d.Hosts, Cmd: d.Cmd, Danger: d.Danger, Plan: d.Plan, Client: w.client}
	var msg string
	var saved bool
	raw := st.workOutside(func(out io.Writer) {
		_, msg, saved = workExecute(req, w.store, w.local, out, st.env.Now)
		workPause(st.env.In, out)
	})
	st.refresh()
	w.reload()
	w.step, w.back, w.cur = wkList, wkList, 0
	if raw+note != "" || !saved || strings.HasPrefix(msg, "[X]") {
		w.setErr(raw + note + msg)
	} else {
		w.setMsg(msg)
	}
}

// workLess: 결과 보기 — less -R (테스트에서 교체)
var workLess = func(path string) error {
	cmd := exec.Command("less", "-R", path)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	return cmd.Run()
}

// viewWorkOut: 4 결과 보기 — 압축 출력을 풀어 0600 임시 파일로 less -R (끝나면 삭제). 만료돼 지워졌으면 안내.
func (st *tuiState) viewWorkOut(id string) {
	w := st.wk
	gz, err := w.store.Output(id)
	if err != nil {
		if os.IsNotExist(err) || strings.Contains(err.Error(), "출력 없음") {
			w.setMsg("이 실행의 전체 출력은 보관 기간(7일)이 지나 삭제됐거나 없습니다 — 요약만 남아 있습니다")
		} else {
			w.setErr("[X] 출력을 읽지 못했습니다: " + err.Error())
		}
		return
	}
	b, err := gunzipBytes(gz)
	if err != nil {
		w.setErr("[X] 출력 압축을 풀지 못했습니다: " + err.Error())
		return
	}
	f, err := os.CreateTemp("", "as_work_view_*.log") // 0600
	if err != nil {
		w.setErr("[X] " + err.Error())
		return
	}
	p := f.Name()
	defer os.Remove(p)
	_, werr := f.Write(b)
	f.Close()
	if werr != nil {
		w.setErr("[X] " + werr.Error())
		return
	}
	var lerr error
	raw := st.workOutside(func(io.Writer) { lerr = workLess(p) })
	switch {
	case raw != "":
		w.setErr(raw)
	case lerr != nil:
		w.setErr("[X] less 실행 실패: " + lerr.Error())
	default:
		w.msg = ""
	}
}

// cmdWork: CLI `auto_setup [-user 이름] s` — 무인자 TUI 와 같은 화면으로 gossh 작업 목록만 띄우고 닫히면 끝낸다
func cmdWork() int {
	if !isTTY(os.Stdin) || !isTTY(os.Stdout) {
		msgErr("auto_setup s 는 터미널(tty)에서 실행하세요")
		return 1
	}
	user, err := askUserCLI(os.Stdin, os.Stdout)
	if err != nil {
		msgErr("%v", err)
		return 1
	}
	env, cleanup := realTUIEnv(os.Stdin, os.Stdout)
	defer cleanup()
	var st *tuiState
	if rc := runTUILoopWith(newSource(), env, func(s *tuiState) {
		st = s
		s.oneShot = true
		s.openWork(user)
	}); rc != 0 {
		return rc
	}
	if st != nil && st.sel.Msg != "" {
		fmt.Println(st.sel.Msg)
	}
	return 0
}

// ---------------------------------------------------------------- 화면

// workClock: 실행 시각 10/12 14:02
func (w *workState) workClock(at int64) string {
	return time.Unix(at, 0).In(w.loc).Format("01/02 15:04")
}

// workListText: svr001 등 20대
func workListText(hosts []string) string {
	switch len(hosts) {
	case 0:
		return "목록 사본 없음"
	case 1:
		return hosts[0] + " 1대"
	}
	return hosts[0] + " 등 " + commaN(len(hosts)) + "대"
}

// workLines: 세그먼트를 폭에 맞춰 줄바꿈 (sel 이면 > 표시 줄을 반전)
func workLines(segs []seg, cont string, sel bool, width int, color bool) []string {
	var out []string
	for _, l := range wrapSegLines(width, 2, cont, segs) {
		if sel {
			out = append(out, selLine(color, width, l))
		} else {
			out = append(out, segLine(color, width, l...))
		}
	}
	return out
}

// groupLines: 묶음 머리 ` qkz : svr001 등 20대        (메모: …)`
func (w *workState) groupLines(g workGroup, width int, color bool) []string {
	segs := []seg{{" " + g.File, stBold}, {" : " + workListText(g.Hosts), ""}}
	if g.Parent != "" {
		segs = append(segs, seg{"  (" + g.Parent + " 의 실패·접속불가)", stGray})
	}
	if g.Memo != "" {
		memo := "(메모: " + g.Memo + ")"
		gap := width - strWidth(cleanText(segsText(segs))) - strWidth(cleanText(memo)) - 1
		if gap < 2 {
			gap = 2
		}
		segs = append(segs, seg{strings.Repeat(" ", gap), ""}, seg{memo, stCyan})
	}
	return workLines(segs, "     ", false, width, color)
}

// runLines: 실행 줄 `   ├ 10/12 14:02 os8      cat /proc/uptime        성공 18 접속불가 2(os6 재실행 → 성공 2)`
func (w *workState) runLines(r *workRec, tree string, sel bool, width int, color bool) []string {
	mark := "  "
	if sel {
		mark = "> "
	}
	pre := mark + " " + tree + " " + w.workClock(r.At) + " " + padR(r.Side, 8) + " "
	res := workResText(r)
	rs := stGreen
	if workResBad(r) {
		rs = stRed
	}
	cmd := cleanText(r.Cmd)
	cmdW := width - strWidth(pre) - 2 - strWidth(res)
	segs := []seg{{pre, ""}}
	if strWidth(cmd) <= cmdW {
		segs = append(segs, seg{padR(cmd, cmdW), stBold})
	} else {
		segs = append(segs, seg{cmd, stBold})
	}
	segs = append(segs, seg{"  ", ""}, seg{res, rs})
	return workLines(segs, "      ", sel, width, color)
}

// listBody: 목록 본문과 커서 줄 위치
func (w *workState) listBody(width int, color bool) ([]string, int) {
	if len(w.gs) == 0 {
		return wrapSegs(color, width, 1, "   ", seg{" 기록이 없습니다 — n 으로 새 작업을 시작하세요", stGray}), -1
	}
	var body []string
	curLine, k := -1, 0
	for _, g := range w.gs {
		body = append(body, w.groupLines(g, width, color)...)
		for i, r := range g.Runs {
			tree := "├"
			if i == len(g.Runs)-1 {
				tree = "└"
			}
			sel := k == w.cur
			if sel {
				curLine = len(body)
			}
			body = append(body, w.runLines(r, tree, sel, width, color)...)
			k++
		}
	}
	return body, curLine
}

// searchBody: 검색 결과 (시간순) — `10/12 14:02 qkz os8  cat /proc/uptime  → 접속불가 → os6 성공`
func (w *workState) searchBody(width int, color bool) ([]string, int) {
	if len(w.found) == 0 {
		return wrapSegs(color, width, 1, "   ", seg{" (없음)", stGray}), -1
	}
	var body []string
	curLine := -1
	for i, r := range w.found {
		mark := "  "
		if i == w.scur {
			mark, curLine = "> ", len(body)
		}
		h := workMatchHost(w.db.Lists[r.File], w.query)
		segs := []seg{{mark + " " + w.workClock(r.At) + " " + r.File + " " + padR(r.Side, 8) + " ", ""}, {cleanText(r.Cmd), stBold},
			{"  ", ""}, {"→ " + h + " " + workHostStatus(r, h), ""}}
		body = append(body, workLines(segs, "      ", i == w.scur, width, color)...)
	}
	return body, curLine
}

// ensureVisible: 커서 줄이 보이게 스크롤 (renderFlow 는 가려진 줄이 있으면 첫·끝 줄을 표시 줄로 바꾸므로 그 안쪽으로)
func ensureVisible(sc *flowScroll, line int) {
	if line < 0 {
		return
	}
	page := sc.Page
	if page < 1 {
		page = 10
	}
	sc.Follow = false
	if line <= sc.Top {
		sc.Top = rMax(line-1, 0)
	} else if line > sc.Top+page {
		sc.Top = line - page
	}
}

// menuFoot: 항목 메뉴 줄 ` qkz 10/12 14:02 ▶ 1 같은 파일로 새 명령  2 재실행 …   (Esc 닫기)`
func (w *workState) menuFoot(width int, color bool) []string {
	r := w.sel
	segs := []seg{{" " + r.File + " " + w.workClock(r.At) + " ▶ ", hintStyles["s"]}}
	for i, it := range workMenuItems {
		if i > 0 {
			segs = append(segs, seg{"  ", ""})
		}
		segs = append(segs, seg{it[:1], "1;32"}, seg{it[1:], stBold})
	}
	segs = append(segs, seg{"   ", ""}, seg{"(Esc 닫기)", stGray})
	return wrapSegs(color, width, 2, "   ", segs...)
}

// renderWork: gossh 작업 화면 전체 (flowFrame)
func renderWork(w *workState, width, height int, color bool) string {
	width = clampWidth(width)
	ln := func(st, t string) []string { return wrapSegs(color, width, 1, "   ", seg{" " + t, st}) }
	f := flowFrame{Title: fmt.Sprintf("[user: %s] gossh 작업", w.user), Msg: w.msg, MsgErr: w.msgErr}
	listView := func() {
		for _, n := range w.notes {
			f.Head = append(f.Head, ln(n.style, n.text)...)
		}
		body, cur := w.listBody(width, color)
		f.Body = body
		ensureVisible(&w.sc, cur)
	}
	searchView := func() {
		f.Title += fmt.Sprintf(" - 검색: %s (%d건)", w.query, len(w.found))
		body, cur := w.searchBody(width, color)
		f.Body = body
		ensureVisible(&w.sc, cur)
	}
	under := func() { // 메뉴·확인·검색어 입력이 덮는 화면
		if w.back == wkSearch && w.step != wkSearchIn {
			searchView()
		} else {
			listView()
		}
	}
	switch w.step {
	case wkList:
		listView()
		f.Hint = " ↑↓ 이동  n 새 작업  / 호스트 검색  Enter 선택  q 닫기"
	case wkMenu:
		under()
		f.Foot = w.menuFoot(width, color)
		f.Hint = " 번호를 누르세요  Esc 닫기"
	case wkAsk:
		under()
		f.Foot = ln(stBold, w.ask)
		f.Hint = " y 예  n 아니오"
	case wkSearchIn:
		listView()
		f.Foot = inputLines(color, width, "/ 호스트 > "+w.in.text()+"_")
		f.Hint = " Enter 검색 (정확한 이름 또는 짧은 이름)  Esc 취소"
	case wkSearch:
		searchView()
		f.Hint = " ↑↓ 이동  Enter 선택  / 다시 검색  Esc 목록"
	case wkPaste:
		f.Title += " - 새 작업: 호스트 붙여넣기"
		f.Head = append(f.Head, ln("", "호스트를 붙여넣으세요 — 공백·쉼표·줄바꿈 구분, 중복은 한 번만, svr[001-020] 범위 표기 가능")...)
		f.Head = append(f.Head, ln(stGray, "끝내기: 빈 줄 또는 . (Ctrl+D)")...)
		f.Head = append(f.Head, ln(stBold, fmt.Sprintf("입력 %s대", commaN(len(w.pasted))))...)
		if len(w.pasted) > 0 {
			var segs []seg
			for i, h := range w.pasted {
				if i > 0 {
					segs = append(segs, seg{"  ", ""})
				}
				segs = append(segs, seg{h, ""})
			}
			f.Body = wrapSegs(color, width, 2, "   ", append([]seg{{"   ", ""}}, segs...)...)
		}
		f.Foot = append([]string{""}, inputLines(color, width, "> "+w.paste.text()+"_")...)
		f.Hint = " Enter 줄 확정  빈 줄/. 입력 끝  ↑↓ PgUp/PgDn 스크롤  Esc 취소"
	case wkReuse:
		f.Title += " - 새 작업"
		f.Head = append(f.Head, ln(stBold, fmt.Sprintf("입력 %s대", commaN(len(w.pasted))))...)
		f.Foot = ln(stBold, fmt.Sprintf("%s 와 같은 목록(%s대) — 재사용? [y/n]", w.reuse, commaN(len(w.db.Lists[w.reuse]))))
		f.Hint = " y 재사용 (" + workPath(w.reuse) + ")  n 새 목록 만들기  Esc 이전"
	case wkCmd:
		d := w.draft
		f.Title += " - 명령 입력"
		kind := "기존 목록"
		switch {
		case d.Parent != "":
			kind = d.Parent + " 에서 파생"
		case d.NewList:
			kind = "새 목록"
		}
		f.Head = append(f.Head, ln(stBold, fmt.Sprintf("파일: %s (%s) · %s", workPath(d.File), kind, workListText(d.Hosts)))...)
		p := workPlan(len(d.Hosts), w.client)
		where := "실행: " + p.Side + " gossh"
		switch {
		case p.Remote:
			where = fmt.Sprintf("실행: 전량 os6_mgmt(%s) 경유 (%s대 > 기준 %s대)", os6_mgmt, commaN(len(d.Hosts)), commaN(workOS6Over()))
		case w.client:
			where = "실행: 이 서버(os6_mgmt)의 gossh"
		case p.Retry:
			where += " — 접속불가 호스트는 os6_mgmt 경유로 자동 재실행"
		case p.Note != "":
			where += " — " + p.Note + " (os6 재실행 안 함)"
		}
		f.Head = append(f.Head, ln("", where)...)
		f.Head = append(f.Head, ln(stGray, "TUI 를 잠시 떠나 터미널에서 gossh 를 그대로 실행합니다 (출력·색·위험 명령 확인 그대로, 끝나면 이 목록으로)")...)
		f.Foot = inputLines(color, width, "gossh -w "+workPath(d.File)+" > "+w.in.text()+"_")
		f.Hint = " Enter 실행  Esc 취소"
	case wkMemo:
		g := w.sel.File
		f.Title += " - 메모"
		f.Head = append(f.Head, ln(stBold, g+" : "+workListText(w.db.Lists[g]))...)
		f.Head = append(f.Head, ln(stGray, "메모는 파일 단위입니다 · 빈 값으로 저장하면 지웁니다")...)
		f.Foot = inputLines(color, width, "메모 > "+w.in.text()+"_")
		f.Hint = " Enter 저장  Esc 취소"
	case wkHosts:
		r := w.sel
		list := w.db.Lists[r.File]
		f.Title += fmt.Sprintf(" - 호스트 목록 %s (%s대)", r.File, commaN(len(list)))
		f.Head = append(f.Head, ln(stGray, "결과 기준: "+w.workClock(r.At)+" "+cleanText(r.Cmd))...)
		hw := 0
		for _, h := range list {
			hw = rMax(hw, strWidth(cleanText(h)))
		}
		for _, h := range list {
			s := workHostStatus(r, h)
			st := stGreen
			if s != "성공" && s != "접속불가 → os6 성공" {
				st = stRed
			}
			f.Body = append(f.Body, workLines([]seg{{"   " + padR(h, hw) + "  ", ""}, {s, st}}, "     ", false, width, color)...)
		}
		if len(list) == 0 {
			f.Body = ln(stGray, "목록 사본이 없습니다")
		}
		f.Hint = " ↑↓ PgUp/PgDn 스크롤  Esc 닫기"
	}
	return renderFlow(f, &w.sc, width, height, color)
}
