// newhost_tui.go - v0.8.0 신규 설치 양식 흐름(w 메뉴 2번 / CLI auto_setup newhost)의 상태기계·화면.
// 순수 로직(붙여넣기 해석·지정 언어·추천·저장)은 newhost.go, 화면 틀·붙여넣기 입력·비동기는 flow_tui.go.
//
// 흐름: user(needSessionUser / CLI askUserCLI) → 1 붙여넣기 → 2 템플릿 지정 → 3 DNS 점검(백그라운드, a/n/s/q)
// → 4 미리보기(y 저장) → 5 저장 확인(AWX 실행? y/n). Esc = 이전 단계(첫 단계는 닫기), Ctrl+X → y = 취소(저장 안 함),
// Ctrl+C/Ctrl+Z 는 무시. os6_mgmt 원격 클라이언트에서는 열지 않고 m 과 같은 방식으로 안내만 한다.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// 단계
const (
	nhPaste   = iota // 1 붙여넣기
	nhAssign         // 2 템플릿 지정
	nhDNS            // 3 DNS 조회 중 (백그라운드)
	nhDNSAsk         // 3 불일치 처리 a/n/s/q
	nhDNSEach        // 3 하나씩 y/n
	nhPreview        // 4 미리보기
	nhSaved          // 5 저장 확인 + AWX 실행?
)

// 확인 질문
const (
	nhAskNone    = iota
	nhAskCancel  // Ctrl+X — 취소할까요?
	nhAskDiscard // 붙여넣기 단계 Esc — 입력을 버리고 닫을까요?
)

const (
	nhCancelMsg    = "신규 설치 양식을 취소했습니다 — 저장하지 않음"
	nhStopMsg      = "신규 설치 양식을 중단했습니다 (DNS 점검에서 q) — 저장하지 않음"
	nhSavedOnlyMsg = "저장만 완료 — w 로 나중에 실행 가능"
)

// newhostDNS: DNS 점검 함수 (테스트에서 가짜 결과로 바꾼다)
var newhostDNS = CheckDNS

// newhostState: 신규 설치 양식 흐름 하나
type newhostState struct {
	user, stateDir, awxDir string
	now                    func() time.Time

	step    int
	ask     int
	msg     string
	msgErr  bool
	sc      flowScroll
	rows    []nhRow
	paste   pasteBuf
	editRow int    // e N 으로 다시 입력 중인 행 (1부터), 0 = 새 줄 추가
	in      []rune // 지정 입력 줄
	jumpRow int    // 다음 렌더에서 보이게 할 행 (0부터), -1 = 없음

	tpls        []nhTpl
	tplProblems []string
	assign      map[string]*nhVals // 소문자 hostname → 명시 지정 (템플릿·열 수정)
	hist        *nhHistIndex
	rec         map[string]nhVals // 소문자 hostname → 이력 추천 (명시 지정이 없는 행만)

	dns        *bgTask[DNSReport]
	rep        DNSReport
	dnsDone    bool              // 지금 행들로 DNS 점검이 끝났음
	dnsChanged map[string]string // 소문자 hostname → "옛IP → 새IP"
	each       []DNSHostResult
	eachI      int
	eachPick   []string

	saved      nhSaveResult
	savedLines []string

	closed   bool
	closeMsg string
	awx      *nhAwxReq // 닫힐 때 AWX 로 이어 갈 요청

	// 본문 캐시 (붙여넣기 대량 입력 때 키마다 전체를 다시 줄바꿈하지 않게)
	ver      int
	cacheKey string
	cache    []string
	rowKey   []string
	rowLines [][]string
	hdrN     int // 지정 표 머리 줄 수
}

// newNewhostState: user 가 정해진 뒤 흐름 시작 (템플릿은 여기서 한 번 읽는다)
func newNewhostState(user, stateDir, awxDir string, now func() time.Time) *newhostState {
	n := &newhostState{user: user, stateDir: stateDir, awxDir: awxDir, now: now, jumpRow: -1,
		assign: map[string]*nhVals{}, rec: map[string]nhVals{}, dnsChanged: map[string]string{}}
	n.sc.Follow = true
	n.tpls, n.tplProblems = loadNewhostTemplates(stateDir, user)
	return n
}

func (n *newhostState) changed()         { n.ver++ }
func (n *newhostState) setMsg(m string)  { n.msg, n.msgErr = m, false }
func (n *newhostState) setErr(m string)  { n.msg, n.msgErr = m, true }
func (n *newhostState) close(msg string) { n.closed, n.closeMsg = true, msg }

// eff: 행의 현재 값 (명시 지정 → 추천 → 없음)
func (n *newhostState) eff(r nhRow) nhVals {
	k := strings.ToLower(r.Host)
	if v := n.assign[k]; v != nil {
		return *v
	}
	if v, ok := n.rec[k]; ok {
		return v
	}
	return nhVals{Label: "-", Src: nhSrcNone}
}

// rowNums: 행 번호 목록 표시 (앞 10개까지)
func rowNums(idx []int) string {
	var s []string
	for i, x := range idx {
		if i == 10 {
			s = append(s, "…")
			break
		}
		s = append(s, strconv.Itoa(x+1))
	}
	return strings.Join(s, ",")
}

func (n *newhostState) badRows() []int {
	var out []int
	for i, r := range n.rows {
		if r.bad() != "" {
			out = append(out, i)
		}
	}
	return out
}

// ---------------------------------------------------------------- 키

func (n *newhostState) handleKey(ev keyEv) {
	if ev.k == kRune && (ev.r == runeCtrlC || ev.r == runeCtrlZ) {
		n.setMsg("이전 단계·닫기는 Esc, 취소는 Ctrl+X")
		return
	}
	if n.ask != nhAskNone {
		n.ask = nhAskNone
		if ev.k == kRune && (ev.r == 'y' || ev.r == 'Y') {
			n.close(nhCancelMsg)
		} else {
			n.setMsg("")
		}
		return
	}
	if ev.k == kRune && ev.r == runeCtrlX {
		if n.step == nhSaved { // 이미 저장됨 — 취소할 것이 없으니 n 과 같게
			n.close(nhSavedOnlyMsg + " (" + n.saved.Path + ")")
			return
		}
		n.ask = nhAskCancel
		return
	}
	switch n.step {
	case nhPaste:
		n.pasteKey(ev)
	case nhAssign:
		n.assignKey(ev)
	case nhDNS:
		if ev.k == kEsc { // 결과는 버린다 (goroutine 은 끝나면 버퍼에 넣고 사라짐)
			n.dns, n.step = nil, nhAssign
			n.setMsg("DNS 점검을 멈추고 템플릿 지정으로 돌아왔습니다")
		}
	case nhDNSAsk:
		n.dnsAskKey(ev)
	case nhDNSEach:
		n.dnsEachKey(ev)
	case nhPreview:
		n.previewKey(ev)
	case nhSaved:
		n.savedKey(ev)
	}
}

// ---- 1 붙여넣기 ----

func (n *newhostState) pasteKey(ev keyEv) {
	if n.sc.key(ev) {
		return
	}
	if ev.k == kEsc {
		if len(n.rows) == 0 && n.paste.text() == "" {
			n.close("신규 설치 양식을 닫았습니다")
			return
		}
		n.ask = nhAskDiscard
		return
	}
	e, line := n.paste.key(ev)
	switch e {
	case pasteLine:
		n.commitLine(line)
	case pasteEnd:
		if n.editRow != 0 && line == "" { // e N 중 빈 Enter = 다시 입력 취소
			n.editRow = 0
			n.setMsg("다시 입력을 취소했습니다")
			return
		}
		if line != "" {
			n.commitLine(line)
		}
		n.finishPaste()
	case pasteBack: // 빈 줄에서 Backspace — 마지막 줄을 꺼내 고친다
		if n.editRow == 0 && len(n.rows) > 0 {
			last := n.rows[len(n.rows)-1]
			n.rows = n.rows[:len(n.rows)-1]
			n.paste.set(last.Raw)
			nhCheckDups(n.rows)
			n.changed()
		}
	}
}

// commitLine: 한 줄 확정 — 고치기 명령(d/e)이면 실행, 아니면 hostname ip mac 으로 해석해 추가(또는 e 로 고친 행 교체)
func (n *newhostState) commitLine(line string) {
	kind, nums, err := nhParseRowCmd(line, len(n.rows))
	if kind != 0 && n.editRow == 0 {
		if err != nil {
			n.setErr("[X] " + err.Error())
			return
		}
		if kind == 'e' {
			n.editRow = nums[0]
			n.paste.set(n.rows[nums[0]-1].Raw)
			n.setMsg(fmt.Sprintf("행 %d 다시 입력 — Enter 로 바꿈 (빈 Enter 는 취소)", nums[0]))
			n.jumpRow = nums[0] - 1
			return
		}
		del := map[int]bool{}
		for _, x := range nums {
			del[x-1] = true
		}
		var keep []nhRow
		for i, r := range n.rows {
			if !del[i] {
				keep = append(keep, r)
			}
		}
		n.rows = keep
		nhCheckDups(n.rows)
		n.changed()
		n.setMsg(fmt.Sprintf("행 %s 삭제 (%d줄)", rowNums(func() []int {
			var x []int
			for _, v := range nums {
				x = append(x, v-1)
			}
			return x
		}()), len(nums)))
		return
	}
	r := nhParseLine(line)
	if n.editRow != 0 {
		n.rows[n.editRow-1] = r
		n.jumpRow = n.editRow - 1
		n.editRow = 0
	} else {
		n.rows = append(n.rows, r)
	}
	nhCheckDups(n.rows)
	n.changed()
	if n.msgErr && len(n.badRows()) == 0 {
		n.setMsg("")
	}
}

// finishPaste: 빈 줄·. — 오류가 없으면 템플릿 지정으로
func (n *newhostState) finishPaste() {
	if len(n.rows) == 0 {
		n.setErr("hostname ip mac 을 1줄 이상 붙여넣으세요")
		return
	}
	if bad := n.badRows(); len(bad) > 0 {
		n.setErr(fmt.Sprintf("[X] 오류 %d줄 (행 %s) — d 번호 로 지우거나 e 번호 로 고친 뒤 빈 줄로 다시 끝내세요", len(bad), rowNums(bad)))
		n.jumpRow = bad[0]
		return
	}
	n.enterAssign()
}

// ---- 2 템플릿 지정 ----

func (n *newhostState) enterAssign() {
	n.step, n.in, n.sc = nhAssign, nil, flowScroll{}
	n.setMsg("")
	if n.hist == nil {
		n.hist = loadNewhostHistory(newhostHistFiles(n.stateDir, n.awxDir))
	}
	n.recompute()
}

// recompute: 명시 지정이 없는 행의 이력 추천을 다시 계산
func (n *newhostState) recompute() {
	n.rec = map[string]nhVals{}
	for _, r := range n.rows {
		k := strings.ToLower(r.Host)
		if n.assign[k] != nil {
			continue
		}
		if h, ok := n.hist.recommend(r.Host); ok {
			n.rec[k] = nhVals{T: h.T, Label: "추천", Src: nhSrcRec, From: h.Host}
		}
	}
	n.changed()
}

func (n *newhostState) assignKey(ev keyEv) {
	if n.sc.key(ev) {
		return
	}
	switch {
	case ev.k == kEsc:
		n.step, n.in, n.sc = nhPaste, nil, flowScroll{Follow: true}
		n.setMsg("")
		n.changed()
	case ev.k == kEnter:
		line := strings.TrimSpace(string(n.in))
		n.in = nil
		switch {
		case line == "":
			n.toDNS()
		case strings.HasPrefix(line, "+"):
			n.addTpl(line)
		default:
			n.applyAssign(line)
		}
	case ev.k == kRune && ev.r == runeBackspace:
		if len(n.in) > 0 {
			n.in = n.in[:len(n.in)-1]
		}
	case ev.k == kRune && ev.r == '\t':
		n.in = append(n.in, ' ')
	case ev.k == kRune && ev.r >= 0x20:
		n.in = append(n.in, ev.r)
	}
}

// applyAssign: 지정 한 줄 — 전부 검사한 뒤 적용 (하나라도 틀리면 아무것도 바꾸지 않고 입력을 되돌려 줌)
func (n *newhostState) applyAssign(line string) {
	fail := func(err error) {
		n.in = []rune(line)
		n.setErr("[X] " + err.Error())
	}
	ops, err := nhParseAssign(line, n.tpls)
	if err != nil {
		fail(err)
		return
	}
	targets := make([][]int, len(ops))
	for i, op := range ops {
		if targets[i], err = nhTargets(n.rows, op.target); err != nil {
			fail(err)
			return
		}
	}
	touched := map[int]bool{}
	for i, op := range ops {
		for _, ri := range targets[i] {
			k := strings.ToLower(n.rows[ri].Host)
			if op.tpl >= 0 {
				t := n.tpls[op.tpl].NewhostTpl
				t.Alias, t.No = "", 0
				n.assign[k] = &nhVals{T: t, Label: nhTplLabel(op.tpl, n.tpls[op.tpl]), Src: nhSrcTpl}
			}
			if len(op.cols) > 0 {
				cur := n.eff(n.rows[ri])
				nv := nhVals{T: cur.T, Label: cur.Label, Src: nhSrcEdit}
				switch cur.Src {
				case nhSrcTpl:
					nv.Label = cur.Label + "*"
				case nhSrcRec, nhSrcNone:
					nv.Label = "수정"
				}
				for _, c := range op.cols {
					nhSetCol(&nv.T, c[0], c[1])
				}
				n.assign[k] = &nv
			}
			touched[ri] = true
		}
	}
	n.recompute()
	n.setMsg(fmt.Sprintf("%d행 지정했습니다", len(touched)))
}

// addTpl: `+ infra nic disk 용량 os boot [별칭]` — user 별 templates 에 저장하고 목록에 추가
func (n *newhostState) addTpl(line string) {
	t, err := nhParsePlus(line)
	if err == nil {
		_, err = addNewhostTemplate(n.stateDir, n.user, n.tpls, t)
	}
	if err != nil {
		n.in = []rune(line)
		n.setErr("[X] 템플릿 추가 실패: " + err.Error())
		return
	}
	n.tpls = append(n.tpls, nhTpl{NewhostTpl: t, User: true})
	n.changed()
	n.setMsg(fmt.Sprintf("템플릿 %s 추가 (%s) — 다음에도 목록에 나옵니다", nhTplLabel(len(n.tpls)-1, n.tpls[len(n.tpls)-1]), t.Name()))
}

// toDNS: 빈 Enter — 모든 행에 값이 있으면 DNS 점검을 백그라운드로 시작
func (n *newhostState) toDNS() {
	var miss []int
	for i, r := range n.rows {
		if !nhComplete(n.eff(r).T) {
			miss = append(miss, i)
		}
	}
	if len(miss) > 0 {
		n.setErr(fmt.Sprintf("[X] 값 없는 행 %d개 (행 %s) — 템플릿을 지정하거나 열을 채우세요", len(miss), rowNums(miss)))
		n.jumpRow = miss[0]
		return
	}
	entries := n.entries()
	n.dns = startBG(n.now(), func() DNSReport { return newhostDNS(entries) })
	n.step, n.dnsDone = nhDNS, false
	n.setMsg("")
}

func (n *newhostState) entries() []DNSEntry {
	out := make([]DNSEntry, len(n.rows))
	for i, r := range n.rows {
		out[i] = DNSEntry{Host: r.Host, IP: r.IP}
	}
	return out
}

// ---- 3 DNS ----

// pollDNS: 백그라운드 점검이 끝났으면 다음 단계로 (tick·키마다)
func (n *newhostState) pollDNS() {
	if n.step != nhDNS || n.dns == nil {
		return
	}
	rep, ok := n.dns.poll()
	if !ok {
		return
	}
	n.dns, n.rep, n.dnsDone = nil, rep, true
	if !rep.Enabled || len(rep.Mismatches()) == 0 {
		n.toPreview()
		return
	}
	n.step, n.sc = nhDNSAsk, flowScroll{}
}

func (n *newhostState) dnsAskKey(ev keyEv) {
	if n.sc.key(ev) {
		return
	}
	switch {
	case ev.k == kEsc:
		n.step, n.sc = nhAssign, flowScroll{}
	case ev.k == kRune && (ev.r == 'a' || ev.r == 'A'):
		n.toPreview()
		n.applyDNS(DNSMismatchHosts(n.rep))
	case ev.k == kRune && (ev.r == 'n' || ev.r == 'N'):
		n.toPreview()
		n.setMsg("등록값 그대로 진행합니다")
	case ev.k == kRune && (ev.r == 's' || ev.r == 'S'):
		n.each, n.eachI, n.eachPick, n.step = n.rep.Mismatches(), 0, nil, nhDNSEach
	case ev.k == kRune && (ev.r == 'q' || ev.r == 'Q'):
		n.close(nhStopMsg)
	}
}

func (n *newhostState) dnsEachKey(ev keyEv) {
	switch {
	case ev.k == kEsc:
		n.step = nhDNSAsk
		return
	case ev.k == kRune && (ev.r == 'y' || ev.r == 'Y'):
		n.eachPick = append(n.eachPick, n.each[n.eachI].Host)
	case ev.k == kRune && (ev.r == 'n' || ev.r == 'N'):
	default:
		return
	}
	if n.eachI++; n.eachI >= len(n.each) {
		n.toPreview()
		n.applyDNS(n.eachPick)
	}
}

// applyDNS: hosts 의 등록 IP 를 DNS IP 로 (ApplyDNS) — 바뀐 줄은 auto_setup.log 에 남기고 중복을 다시 검사한다
func (n *newhostState) applyDNS(hosts []string) {
	es := n.entries()
	changed := ApplyDNS(es, n.rep, hosts)
	for i := range n.rows {
		if es[i].IP != n.rows[i].IP {
			n.dnsChanged[strings.ToLower(n.rows[i].Host)] = n.rows[i].IP + " → " + es[i].IP
			n.rows[i].IP = es[i].IP
		}
	}
	for _, l := range changed {
		logf("newhost user=%s %s", n.user, l)
	}
	nhCheckDups(n.rows)
	n.changed()
	if len(changed) > 0 {
		n.setMsg(fmt.Sprintf("DNS IP 로 %d대 바꿨습니다", len(changed)))
	}
}

// ---- 4 미리보기 · 5 저장 ----

func (n *newhostState) toPreview() {
	n.step, n.sc = nhPreview, flowScroll{}
	n.setMsg("")
	n.changed()
}

func (n *newhostState) previewKey(ev keyEv) {
	if n.sc.key(ev) {
		return
	}
	switch {
	case ev.k == kEsc:
		n.step, n.sc = nhAssign, flowScroll{}
		n.setMsg("")
	case ev.k == kRune && (ev.r == 'y' || ev.r == 'Y'):
		n.save()
	}
}

// lines: 저장할 12필드 줄 (붙여넣은 순서)
func (n *newhostState) lines() []string {
	out := make([]string, len(n.rows))
	for i, r := range n.rows {
		out[i] = nhLine(r, n.eff(r).T)
	}
	return out
}

func (n *newhostState) save() {
	if bad := n.badRows(); len(bad) > 0 {
		n.setErr(fmt.Sprintf("[X] 저장할 수 없음: 오류 %d줄 (행 %s) — Esc 로 돌아가 고치세요", len(bad), rowNums(bad)))
		return
	}
	for i, r := range n.rows {
		if !nhComplete(n.eff(r).T) {
			n.setErr(fmt.Sprintf("[X] 저장할 수 없음: 행 %d 값 없음", i+1))
			return
		}
	}
	res, err := newhostSave(n.awxDir, n.stateDir, n.user, n.lines(), n.now())
	if err != nil {
		n.setErr("[X] 저장 실패: " + err.Error())
		return
	}
	bk := res.Backup
	if bk == "" {
		bk = "없음(새 파일)"
	}
	logf("newhost user=%s %d대 저장 %s (백업 %s)", n.user, len(n.rows), res.Path, bk)
	n.saved = res
	n.savedLines = nil
	if b, err := os.ReadFile(res.Path); err == nil {
		n.savedLines = splitLines(strings.TrimRight(string(b), "\n"))
	} else {
		n.savedLines = []string{"[X] 저장한 파일을 다시 읽지 못함: " + err.Error()}
	}
	n.step, n.sc = nhSaved, flowScroll{}
	n.setMsg("")
	if res.HistoryErr != "" {
		n.setErr("[!] 저장은 됐지만 이력 사본을 남기지 못했습니다: " + res.HistoryErr)
	}
	n.changed()
}

func (n *newhostState) savedKey(ev keyEv) {
	if n.sc.key(ev) {
		return
	}
	switch {
	case ev.k == kRune && (ev.r == 'y' || ev.r == 'Y'):
		var hosts []string
		for _, r := range n.rows {
			hosts = append(hosts, r.Host)
		}
		n.awx = &nhAwxReq{User: n.user, Hosts: hosts, DNSChecked: n.dnsDone && n.rep.Enabled}
		n.close("")
	case ev.k == kEsc || (ev.k == kRune && (ev.r == 'n' || ev.r == 'N' || ev.r == 'q' || ev.r == 'Q')):
		n.close(nhSavedOnlyMsg + " (" + n.saved.Path + ")")
	}
}

// ---------------------------------------------------------------- tuiState 연결

// startNewhostFlow: w 메뉴 2번 (menu_tui.go startNewhost) — 원격 클라이언트·awx_dir 확인 → 세션 user → 붙여넣기
func (st *tuiState) startNewhostFlow() {
	if st.changeRemote() { // awx_dir/<user>.txt 와 01 은 os8_mgmt 에만 의미가 있다 (m 과 같은 판단·안내 방식)
		st.sel.Msg = newhostRemoteMsg
		return
	}
	if m := newhostDirProblem(); m != "" {
		st.sel.Msg = m
		return
	}
	st.needSessionUser(st.openNewhost)
}

// openNewhost: user 가 정해지면 흐름 화면을 연다
func (st *tuiState) openNewhost(user string) {
	st.nh = newNewhostState(user, dataDir(), awx_dir, st.env.Now)
}

func (st *tuiState) handleNewhost(ev keyEv) {
	st.nh.handleKey(ev)
	st.pollNewhost()
}

// pollNewhost: DNS 결과 확인 + 조회 중에만 1초 tick + 닫혔으면 정리 (AWX 로 이어 가기 포함)
func (st *tuiState) pollNewhost() {
	n := st.nh
	if n == nil {
		return
	}
	n.pollDNS()
	if !n.closed {
		st.fastTickWhile(n.step == nhDNS)
		return
	}
	st.nh = nil
	st.fastTickWhile(false)
	if n.saved.Path != "" {
		st.nhSaved = true
	}
	st.sel.Msg = n.closeMsg
	if n.awx != nil {
		st.newhostAwx(*n.awx)
	}
}

// newhostAwx: 저장 뒤 y — 프로파일이 있으면 기존 w 처럼 고르게 하고, 없으면 바로 실행 (user·대상 목록은 양식 그대로)
func (st *tuiState) newhostAwx(req nhAwxReq) {
	if fi, err := os.Stat(filepath.Join(awx_dir, awxScript)); err != nil || fi.IsDir() {
		st.sel.Msg = "양식은 저장됨 — " + awxScript + " 가 없어 AWX 는 실행하지 않았습니다 (awx_dir 확인)"
		return
	}
	valid, problems := parseAwxProfiles()
	if len(valid) == 0 {
		st.runNewhostAwx(req, nil)
		return
	}
	st.awx = &awxState{step: awxAsk, profiles: valid, problems: problems, nh: &req}
}

// runNewhostAwx: 대상 확인 파일(hostname 목록)을 만들고 01 실행 (p 가 있으면 nodeinfo N 강제, nil 이면 자동 응답 없음)
func (st *tuiState) runNewhostAwx(req nhAwxReq, p *AwxProfile) {
	verify := ""
	if f, err := os.CreateTemp("", "as_awx_verify_"); err == nil {
		f.WriteString(strings.Join(req.Hosts, "\n") + "\n")
		f.Close()
		verify = f.Name()
	}
	st.runAwxEnv(newhostAwxEnv(p, req.User, verify, req.DNSChecked), newhostAwxNote(p))
	if verify != "" {
		os.Remove(verify)
	}
}

// cmdNewhost: CLI `auto_setup [-user 이름] newhost` — 무인자 TUI 와 같은 렌더러로 이 흐름만 띄우고 닫히면 끝낸다.
// 저장했으면 0, 저장하지 않고 끝나면 1.
func cmdNewhost() int {
	if os8_mgmt != "" {
		msgErr(newhostRemoteMsg)
		return 1
	}
	if !isTTY(os.Stdin) || !isTTY(os.Stdout) {
		msgErr("auto_setup newhost 는 터미널(tty)에서 실행하세요")
		return 1
	}
	if m := newhostDirProblem(); m != "" {
		msgErr("%s", m)
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
		s.openNewhost(user)
	}); rc != 0 {
		return rc
	}
	if st != nil && st.sel.Msg != "" {
		fmt.Println(st.sel.Msg)
	}
	if st != nil && st.nhSaved {
		return 0
	}
	return 1
}

// ---------------------------------------------------------------- 화면

var nhStepNames = map[int]string{
	nhPaste: "1/5 붙여넣기", nhAssign: "2/5 템플릿 지정", nhDNS: "3/5 DNS 점검", nhDNSAsk: "3/5 DNS 점검",
	nhDNSEach: "3/5 DNS 점검 (하나씩)", nhPreview: "4/5 미리보기", nhSaved: "5/5 저장 완료",
}

// nhPad: 폭이 모자랄 때만 오른쪽에 공백 (자르지 않음)
func nhPad(s string, w int) string {
	if n := w - strWidth(s); n > 0 {
		return s + strings.Repeat(" ", n)
	}
	return s
}

// renderNewhost: 흐름 전체 화면 (flowFrame)
func renderNewhost(n *newhostState, now time.Time, width, height int, color bool) string {
	width = clampWidth(width)
	ln := func(st, t string) []string { return wrapSegs(color, width, 1, "   ", seg{" " + t, st}) }
	f := flowFrame{Title: fmt.Sprintf("신규 설치 양식 [user: %s] - %s", n.user, nhStepNames[n.step]), Msg: n.msg, MsgErr: n.msgErr}
	switch n.step {
	case nhPaste:
		f.Head = append(f.Head, ln("", "hostname ip mac 을 한 줄에 한 대씩 붙여넣으세요 — 구분자(공백·탭·쉼표)·순서 무관")...)
		f.Head = append(f.Head, ln(stGray, "MAC: aa:bb:cc:dd:ee:ff · aa-bb-… · aabb.ccdd.eeff · aabbccddeeff → 소문자 aa:bb:… 로 저장")...)
		f.Head = append(f.Head, ln(stGray, "끝내기: 빈 줄 또는 . (Ctrl+D)   고치기: d 번호 삭제(d 3-5,8) · e 번호 다시 입력 · 빈 줄에서 Backspace = 앞 줄")...)
		segs := []seg{{fmt.Sprintf(" 입력 %d대", len(n.rows)), stBold}}
		if bad := len(n.badRows()); bad > 0 {
			segs = append(segs, seg{fmt.Sprintf("  오류 %d줄 (빨강)", bad), stRed})
		}
		f.Head = append(f.Head, wrapSegs(color, width, 1, "   ", segs...)...)
		f.Body = n.pasteBody(width, color)
		prompt := "> "
		if n.editRow != 0 {
			prompt = fmt.Sprintf("e %d > ", n.editRow)
		}
		f.Foot = append([]string{""}, inputLines(color, width, prompt+n.paste.text()+"_")...)
		f.Hint = " Enter 줄 확정  빈 줄/. 입력 끝  ↑↓ PgUp/PgDn 스크롤  Esc 닫기  Ctrl+X 취소"
	case nhAssign:
		tl := n.tplLines(width, color)
		head := append([]string{}, tl...)
		head = append(head, "")
		head = append(head, n.assignStatus(width, color)...)
		if height <= 0 || len(head) <= height/3 {
			f.Head = head
			f.Body = n.assignBody(width, color)
		} else { // 템플릿이 많거나 창이 낮으면 목록도 본문에 넣어 함께 스크롤
			f.Body = append(head, n.assignBody(width, color)...)
		}
		f.Foot = append(f.Foot, ln(stGray, "예) svr001-020=1  spice*=mec  1-20=2  *=A  ·  열 수정: svr005 os=9.6  svr001-005 nic=eth0 boot=legacy")...)
		f.Foot = append(f.Foot, ln(stGray, "새 템플릿: + sec bond sda 500 8.10 uefi [별칭]  ·  열: infra nic disk size(용량) os boot  ·  나중 지정이 덮어씀")...)
		f.Foot = append(f.Foot, inputLines(color, width, "지정 > "+string(n.in)+"_")...)
		f.Hint = " Enter 적용 (빈 입력이면 다음: DNS 점검)  ↑↓ PgUp/PgDn 스크롤  Esc 이전 단계  Ctrl+X 취소"
	case nhDNS:
		el := 0
		if n.dns != nil {
			el = rMax(int(now.Sub(n.dns.Started)/time.Second), 0)
		}
		f.Head = append(f.Head, ln("", fmt.Sprintf("DNS 조회 중… %d대 (resolv.conf 의 서버에 A 레코드를 직접 묻습니다)", len(n.rows)))...)
		f.Head = append(f.Head, ln(stGray, fmt.Sprintf("경과 %d초", el))...)
		f.Hint = " 조회 중 — Esc 이전 단계  Ctrl+X 취소"
	case nhDNSAsk:
		f.Head = append(f.Head, ln(stYellow, fmt.Sprintf("등록 IP 가 DNS 와 다른 호스트 %d대 — 판단 불가·미등록·조회 실패는 표에만 보이고 바꾸지 않습니다", len(n.rep.Mismatches())))...)
		f.Head = append(f.Head, "")
		f.Body = n.dnsTable(n.rep.Problems(), width, color)
		f.Foot = append(f.Foot, ln(stBold, "a 모두 DNS 로 / n 그대로 / s 하나씩 / q 중단 :")...)
		f.Hint = " a 모두 DNS 로  n 그대로  s 하나씩  q 중단  ↑↓ PgUp/PgDn 스크롤  Esc 이전 단계"
	case nhDNSEach:
		h := n.each[n.eachI]
		f.Head = append(f.Head, ln(stBold, fmt.Sprintf("(%d/%d) %s", n.eachI+1, len(n.each), h.Host))...)
		f.Head = append(f.Head, ln("", fmt.Sprintf("등록 IP %s → DNS IP %s", h.IP, h.DNSIP))...)
		for _, w := range h.Warnings {
			f.Head = append(f.Head, ln(stYellow, "⚠ "+w)...)
		}
		f.Foot = append(f.Foot, ln(stBold, "DNS IP 로 바꿀까요? [y/n]")...)
		f.Hint = " y 바꿈  n 그대로  Esc 처음 선택으로"
	case nhPreview:
		f.Head = append(f.Head, n.dnsSummary(width, color)...)
		f.Body = n.previewBody(width, color)
		if bad := n.badRows(); len(bad) > 0 {
			f.Foot = append(f.Foot, ln(stRed, fmt.Sprintf("저장할 수 없음: 오류 %d줄 (행 %s) — Esc 로 돌아가 고치세요", len(bad), rowNums(bad)))...)
		} else {
			f.Foot = append(f.Foot, ln(stBold, fmt.Sprintf("y 를 누르면 %s 에 %d대를 저장합니다 (기존 파일은 백업)", filepath.Join(n.awxDir, n.user+".txt"), len(n.rows)))...)
		}
		f.Hint = " y 저장  ↑↓ PgUp/PgDn 스크롤  Esc 이전 단계  Ctrl+X 취소"
	case nhSaved:
		f.Head = append(f.Head, ln(stGreen, fmt.Sprintf("저장 완료: %s (%d대)", n.saved.Path, len(n.rows)))...)
		bk := n.saved.Backup
		if bk == "" {
			bk = "없음 (새 파일)"
		}
		f.Head = append(f.Head, ln("", "기존 파일 백업: "+bk)...)
		if n.saved.History != "" {
			f.Head = append(f.Head, ln("", "이력: "+n.saved.History)...)
		}
		f.Head = append(f.Head, ln(stGray, "저장된 내용 (다시 읽음):")...)
		for _, l := range n.savedLines {
			f.Body = append(f.Body, ln("", l)...)
		}
		f.Foot = append(f.Foot, ln(stBold, "AWX 실행할까요? [y/n]")...)
		f.Hint = " y AWX 실행 (프로파일 선택 시 nodeinfo N · 대상 목록 자동)  n 저장만 하고 닫기  ↑↓ PgUp/PgDn 스크롤"
	}
	switch n.ask {
	case nhAskCancel:
		f.Foot = append(f.Foot, ln(stRed, "신규 설치 양식을 취소할까요? (저장하지 않음) [y/n]")...)
		f.Hint = " y 취소  다른 키 계속"
	case nhAskDiscard:
		f.Foot = append(f.Foot, ln(stRed, fmt.Sprintf("입력한 %d줄을 버리고 닫을까요? [y/n]", len(n.rows)))...)
		f.Hint = " y 닫기  다른 키 계속"
	}
	if n.jumpRow >= 0 && f.Body != nil { // 오류·값 없는 행으로 스크롤 (본문 줄 위치는 캐시에서)
		if i := n.rowLineIndex(n.jumpRow); i >= 0 {
			n.sc.show(i + len(f.Body) - len(n.cache))
		}
		n.jumpRow = -1
	}
	return renderFlow(f, &n.sc, width, height, color)
}

// rowLineIndex: 캐시된 본문에서 행 i 의 첫 줄 위치 (붙여넣기·지정 본문만), 모르면 -1
func (n *newhostState) rowLineIndex(i int) int {
	if i < 0 || i >= len(n.rowLines) {
		return -1
	}
	at := 0
	if n.step == nhAssign {
		at = n.hdrN // 표 머리
	}
	for j := 0; j < i; j++ {
		at += len(n.rowLines[j])
	}
	return at
}

// pasteBody: 붙여넣은 행 — 바뀐 행만 다시 줄바꿈한다 (대량 붙여넣기에서도 키마다 O(보이는 줄))
func (n *newhostState) pasteBody(width int, color bool) []string {
	key := fmt.Sprintf("p/%d/%d/%v", n.ver, width, color)
	if key == n.cacheKey {
		return n.cache
	}
	if len(n.rowKey) > len(n.rows) {
		n.rowKey, n.rowLines = n.rowKey[:len(n.rows)], n.rowLines[:len(n.rows)]
	}
	for len(n.rowKey) < len(n.rows) {
		n.rowKey, n.rowLines = append(n.rowKey, ""), append(n.rowLines, nil)
	}
	n.cache = n.cache[:0]
	for i, r := range n.rows {
		k := fmt.Sprintf("%d/%v/%d\x00%s\x00%s\x00%s", width, color, i, r.Raw, r.Err, r.Dup)
		if n.rowKey[i] != k {
			n.rowKey[i] = k
			num := fmt.Sprintf("%4d  ", i+1)
			if b := r.bad(); b != "" {
				n.rowLines[i] = wrapSegs(color, width, 2, "      ", seg{num + cleanText(r.Raw) + "  ", ""}, seg{"✗ " + b, stRed})
			} else {
				n.rowLines[i] = wrapSegs(color, width, 2, "      ", seg{num + nhPad(r.Host, 16) + "  " + nhPad(r.IP, 15) + "  " + r.MAC, ""})
			}
		}
		n.cache = append(n.cache, n.rowLines[i]...)
	}
	n.cacheKey = key
	return n.cache
}

// tplLines: 템플릿 목록 — 양식 그대로 `1 [A]   tmp tmp sec <hostname> <ip> <mac> bond sda sda5  500 8.10 uefi   (sec-bond-…)`
func (n *newhostState) tplLines(width int, color bool) []string {
	out := wrapSegs(color, width, 1, "   ", seg{" 템플릿 — 번호·별칭·이름으로 지정합니다", stBold})
	if len(n.tpls) == 0 {
		out = append(out, wrapSegs(color, width, 1, "     ", seg{"   (템플릿 없음 — 아래에서 + 로 만들거나 conf 의 newhost_template_1~9)", stGray})...)
	}
	var w [8]int
	cols := func(i int, t nhTpl) [8]string {
		return [8]string{nhTplLabel(i, t), t.Infra, t.NIC, t.Disk, t.Part, t.Size, t.OS, t.Boot}
	}
	for i, t := range n.tpls {
		for c, v := range cols(i, t) {
			if x := strWidth(v); x > w[c] {
				w[c] = x
			}
		}
	}
	for i, t := range n.tpls {
		c := cols(i, t)
		text := fmt.Sprintf("   %s  tmp tmp %s <hostname> <ip> <mac> %s %s %s %s %s %s",
			nhPad(c[0], w[0]), nhPad(c[1], w[1]), nhPad(c[2], w[2]), nhPad(c[3], w[3]), nhPad(c[4], w[4]), nhPad(c[5], w[5]), nhPad(c[6], w[6]), nhPad(c[7], w[7]))
		segs := []seg{{text, ""}, {"   (" + t.Name() + ")", stGray}}
		if t.User {
			segs = append(segs, seg{" user", stGray})
		}
		out = append(out, wrapSegs(color, width, 2, "       ", segs...)...)
	}
	for _, p := range n.tplProblems {
		out = append(out, wrapSegs(color, width, 1, "     ", seg{"   ! " + p, stGray})...)
	}
	return out
}

// assignStatus: `총 N대 · 지정 a · 추천 r (노랑) · 값 없음 m (빨강)`
func (n *newhostState) assignStatus(width int, color bool) []string {
	var set, rec, miss int
	for _, r := range n.rows {
		v := n.eff(r)
		switch {
		case !nhComplete(v.T):
			miss++
		case v.Src == nhSrcRec:
			rec++
		default:
			set++
		}
	}
	segs := []seg{{fmt.Sprintf(" 총 %d대 · 지정 %d", len(n.rows), set), stBold}, {fmt.Sprintf(" · 추천 %d", rec), stYellow}}
	if miss > 0 {
		segs = append(segs, seg{fmt.Sprintf(" · 값 없음 %d", miss), stRed})
	}
	return wrapSegs(color, width, 1, "   ", segs...)
}

// assignBody: 행번호·hostname·ip·mac·템플릿·값 표 (지정이 바뀔 때만 다시 만든다)
func (n *newhostState) assignBody(width int, color bool) []string {
	key := fmt.Sprintf("a/%d/%d/%v", n.ver, width, color)
	if key == n.cacheKey {
		return n.cache
	}
	wn, wh, wi, wl := len(strconv.Itoa(len(n.rows))), len("hostname"), len("ip"), strWidth("템플릿")
	vals := make([]nhVals, len(n.rows))
	for i, r := range n.rows {
		vals[i] = n.eff(r)
		wh, wi, wl = rMax(wh, strWidth(r.Host)), rMax(wi, strWidth(r.IP)), rMax(wl, strWidth(vals[i].Label))
	}
	cache := wrapSegs(color, width, 2, "      ", seg{fmt.Sprintf(" %s  %s  %s  %s  %s  %s", nhPad("#", wn), nhPad("hostname", wh), nhPad("ip", wi),
		nhPad("mac", 17), nhPad("템플릿", wl), "값 (infra nic disk 용량 os boot)"), stBold})
	n.hdrN = len(cache)
	n.rowKey, n.rowLines = make([]string, len(n.rows)), make([][]string, len(n.rows))
	for i, r := range n.rows {
		v := vals[i]
		lst, vst := stGreen, ""
		switch v.Src {
		case nhSrcEdit:
			lst = stCyan
		case nhSrcRec:
			lst, vst = stYellow, stYellow
		case nhSrcNone:
			lst = stRed
		}
		vt := "(값 없음)"
		if v.Src != nhSrcNone {
			var f []string
			for _, x := range []string{v.T.Infra, v.T.NIC, v.T.Disk, v.T.Size, v.T.OS, v.T.Boot} {
				if x == "" {
					x = "?"
				}
				f = append(f, x)
			}
			vt = strings.Join(f, " ")
		}
		if !nhComplete(v.T) {
			vst = stRed
		}
		segs := []seg{{fmt.Sprintf(" %s  %s  %s  %s  ", nhPad(strconv.Itoa(i+1), wn), nhPad(r.Host, wh), nhPad(r.IP, wi), nhPad(r.MAC, 17)), ""},
			{nhPad(v.Label, wl), lst}, {"  ", ""}, {vt, vst}}
		if v.Src == nhSrcRec && v.From != "" && !strings.EqualFold(v.From, r.Host) {
			segs = append(segs, seg{"  ← " + v.From, stGray})
		}
		n.rowLines[i] = wrapSegs(color, width, 2, "      ", segs...)
		cache = append(cache, n.rowLines[i]...)
	}
	n.cache, n.cacheKey = cache, key
	return cache
}

// dnsTable: RenderDNSTable 줄에 상태별 색 (불일치 노랑, 그 밖 회색), 폭을 넘으면 열 경계에서 줄바꿈
func (n *newhostState) dnsTable(rows []DNSHostResult, width int, color bool) []string {
	t := RenderDNSTable(rows)
	if t == "" {
		return nil
	}
	var out []string
	for i, l := range strings.Split(t, "\n") {
		st := stBold
		if i > 0 {
			st = stGray
			if rows[i-1].Status == DNSMismatch {
				st = stYellow
			}
		}
		out = append(out, wrapSegs(color, width, 2, "      ", seg{" " + l, st})...)
	}
	return out
}

// dnsSummary: 미리보기 머리의 DNS 한 줄 (생략 사유 또는 판정 수)
func (n *newhostState) dnsSummary(width int, color bool) []string {
	if !n.dnsDone {
		return nil
	}
	if !n.rep.Enabled {
		return wrapSegs(color, width, 1, "   ", seg{" " + n.rep.Reason, stYellow})
	}
	cnt := map[DNSStatus]int{}
	for _, h := range n.rep.Hosts {
		cnt[h.Status]++
	}
	s := fmt.Sprintf(" DNS 점검: 일치 %d · 불일치 %d (DNS 로 바꿈 %d) · 판단 불가 %d · 미등록 %d · 조회 실패 %d",
		cnt[DNSMatch], cnt[DNSMismatch], len(n.dnsChanged), cnt[DNSAmbiguous], cnt[DNSUnregistered], cnt[DNSNoAnswer])
	return wrapSegs(color, width, 1, "   ", seg{s, ""})
}

// previewBody: (DNS 경고 표) + 템플릿(값)별로 묶은 12필드 줄 — 추천 행 노랑, DNS 로 바꾼 행 청록, 오류 행 빨강
func (n *newhostState) previewBody(width int, color bool) []string {
	key := fmt.Sprintf("v/%d/%d/%v", n.ver, width, color)
	if key == n.cacheKey {
		return n.cache
	}
	var out []string
	if n.dnsDone && n.rep.Enabled {
		var left []DNSHostResult
		for _, h := range n.rep.Problems() {
			if _, ok := n.dnsChanged[strings.ToLower(h.Host)]; !ok {
				left = append(left, h)
			}
		}
		if len(left) > 0 {
			out = append(out, wrapSegs(color, width, 1, "   ", seg{" DNS 경고 (바꾸지 않음):", stYellow})...)
			out = append(out, n.dnsTable(left, width, color)...)
			out = append(out, "")
		}
	}
	lines := n.lines()
	var w [12]int
	for _, l := range lines {
		for c, v := range strings.Fields(l) {
			w[c] = rMax(w[c], strWidth(v))
		}
	}
	type grp struct {
		name string
		idx  []int
	}
	var groups []*grp
	byName := map[string]*grp{}
	for i, r := range n.rows {
		name := n.eff(r).T.Name()
		g := byName[name]
		if g == nil {
			g = &grp{name: name}
			byName[name] = g
			groups = append(groups, g)
		}
		g.idx = append(g.idx, i)
	}
	for _, g := range groups {
		ref := ""
		for i, t := range n.tpls {
			if t.Name() == g.name {
				ref = " = " + nhTplLabel(i, t)
				break
			}
		}
		out = append(out, wrapSegs(color, width, 1, "   ", seg{fmt.Sprintf(" ■ %s%s — %d대", g.name, ref, len(g.idx)), stBold})...)
		for _, i := range g.idx {
			r := n.rows[i]
			f := strings.Fields(lines[i])
			for c := range f {
				f[c] = nhPad(f[c], w[c])
			}
			st, note := "", ""
			if v := n.eff(r); v.Src == nhSrcRec {
				st, note = stYellow, "  (추천"
				if v.From != "" && !strings.EqualFold(v.From, r.Host) {
					note += " ← " + v.From
				}
				note += ")"
			}
			if c, ok := n.dnsChanged[strings.ToLower(r.Host)]; ok {
				st, note = stCyan, note+"  (DNS 로 바꿈 "+c+")"
			}
			if b := r.bad(); b != "" {
				st, note = stRed, note+"  ✗ "+b
			}
			out = append(out, wrapSegs(color, width, 2, "      ", seg{"   " + strings.TrimRight(strings.Join(f, " "), " "), st}, seg{note, st})...) // 표시(…)는 통째로 다음 줄로
		}
	}
	n.cache, n.cacheKey = out, key
	return out
}
