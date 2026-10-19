// oneoff.go - x 키: 체크스크립트(os_check) 단독 실행. user 선택 → 호스트 입력 → 모드 선택 → 실행 → 결과 보기.
// 일회성: 임시 디렉터리에서 실행하고 결과 창을 닫으면 삭제한다 (auto_setup 의 codes·runs·log 에는 아무것도 남기지 않는다).
package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"
	"unicode"
)

const (
	viewOneoff    = "oneoff"
	oneoffTailN   = 15
	oneoffKillGap = 3 * time.Second
	runeCtrlD     = 0x04
	runeCtrlX     = 0x18
	runeBackspace = 0x7f
)

const (
	ooUser  = iota // user 메뉴 번호 또는 user 이름 입력
	ooHosts        // 호스트 여러 줄 입력
	ooMode         // t / c
	ooRun          // 실행 중
)

var (
	userRouteRe = regexp.MustCompile(`(?m)^[ \t]*user_route=["']?([^"'\s]*)["']?`)
	userNameRe  = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)
)

// oneoffProc: 실행 중인 체크스크립트 (테스트에서 가짜로 교체)
type oneoffProc interface {
	Done() <-chan struct{} // 종료되면 닫힘
	Rc() int               // 종료 코드 (Done 이후)
	Kill()                 // 프로세스 그룹에 SIGTERM, 3초 뒤에도 살아 있으면 SIGKILL
	LogPath() string
	Dir() string // 임시 디렉터리
}

var (
	oneoffStart  = realOneoffStart
	oneoffCmdOut = realCmdOut // user 메뉴 스크립트 실행 (stdout, 10초 제한)
)

type oneoffState struct {
	step      int
	menu      []string // user 메뉴 출력 (없으면 user 이름 직접 입력)
	route     string   // user_route 절대 경로
	in        []rune   // 현재 입력 (user/번호 한 줄 또는 호스트 여러 줄)
	msg       string
	user      string
	hosts     []string
	mode      string
	awx       bool        // w(AWX 실행) 흐름: user → 대상 목록 → 01 실행 (모드 선택·결과 창 없음)
	awxProf   *AwxProfile // w 흐름에서 고른 auto 프로파일 (nil = 일반 실행)
	proc      oneoffProc
	started   time.Time
	cancelAsk bool
	cancelled bool

	onUser func(user string) // 세션 user 선택 단계(needSessionUser)에서만 채움: user 가 정해지면 호출. nil = x/w/m 의 일반 user 단계
}

// ---- 순수 함수 ----

// normalizeHosts: 공백·쉼표·탭·| 로 나누고 빈 항목 제거, 중복 제거(순서 유지)
func normalizeHosts(s string) []string {
	f := strings.FieldsFunc(s, func(r rune) bool { return unicode.IsSpace(r) || r == ',' || r == '|' })
	seen := map[string]bool{}
	out := []string{}
	for _, h := range f {
		if !seen[h] {
			seen[h] = true
			out = append(out, h)
		}
	}
	return out
}

// parseUserRoute: 01 스크립트의 첫 user_route="..." 값 (들여쓰기 허용). 없거나 비면 "".
func parseUserRoute(script string) string {
	b, err := os.ReadFile(script)
	if err != nil {
		return ""
	}
	if m := userRouteRe.FindSubmatch(b); m != nil {
		return string(m[1])
	}
	return ""
}

func realCmdOut(dir, script string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "bash", append([]string{script}, args...)...)
	cmd.Dir = dir
	var out bytes.Buffer
	cmd.Stdout = &out
	err := cmd.Run()
	return out.String(), err
}

// tailLines: 파일의 마지막 n 줄 (끝 64KB 만 읽음)
func tailLines(p string, n int) []string {
	f, err := os.Open(p)
	if err != nil {
		return nil
	}
	defer f.Close()
	if fi, err := f.Stat(); err == nil && fi.Size() > 65536 {
		f.Seek(fi.Size()-65536, 0)
	}
	var buf bytes.Buffer
	buf.ReadFrom(f)
	ls := splitLines(strings.TrimRight(buf.String(), "\n"))
	if len(ls) > n {
		ls = ls[len(ls)-n:]
	}
	return ls
}

// ---- 실제 실행 ----

type realOneoffProc struct {
	cmd  *exec.Cmd
	dir  string
	log  string
	done chan struct{}
	rc   int
}

func (p *realOneoffProc) Done() <-chan struct{} { return p.done }
func (p *realOneoffProc) Rc() int               { return p.rc }
func (p *realOneoffProc) LogPath() string       { return p.log }
func (p *realOneoffProc) Dir() string           { return p.dir }
func (p *realOneoffProc) Kill() {
	pid := p.cmd.Process.Pid
	killGroup(pid, syscall.SIGTERM)
	go func() {
		select {
		case <-p.done:
		case <-time.After(oneoffKillGap):
			killGroup(pid, syscall.SIGKILL)
		}
	}()
}

func realOneoffStart(osCheck, user, mode string, hosts []string) (oneoffProc, error) {
	dir, err := os.MkdirTemp("", "as_oneoff_")
	if err != nil {
		return nil, err
	}
	fail := func(err error) (oneoffProc, error) {
		os.RemoveAll(dir)
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(dir, "targets.txt"), []byte(strings.Join(hosts, "\n")+"\n"), 0644); err != nil {
		return fail(err)
	}
	if src := firstExisting(dhcpCandidates(osCheck)); src != "" {
		_ = os.Symlink(src, filepath.Join(dir, "dhcp.sh"))
	}
	logPath := filepath.Join(dir, "os_check.log")
	lf, err := os.Create(logPath)
	if err != nil {
		return fail(err)
	}
	flag := "-auto"
	if mode == ModeCheck {
		flag = "-auto-check"
	}
	cmd := exec.Command("bash", osCheck, flag, user, "targets.txt")
	// os6_mgmt 가 설정돼 있으면 자동 흐름(run)과 같게: 먼저 경로를 판별(route-split — os8 무응답·os6_mgmt 응답 호스트)하고
	// gossh 래퍼로 os8 호스트는 이 서버에서, os6 호스트는 os6_mgmt 경유로 체크한다 (전에는 x 에서 os6 호스트는 항상 접속불가)
	if self, err := os.Executable(); err == nil && os6_mgmt != "" && os6_gossh != "" && writeOS6Wrapper() == nil {
		cmd = exec.Command("bash", "-c", `"$0" route-split targets.txt > os6_hosts.txt; exec bash "$@"`, self, osCheck, flag, user, "targets.txt")
		cmd.Env = append(os.Environ(), "PATH="+binDir()+string(os.PathListSeparator)+os.Getenv("PATH"),
			"AUTO_SETUP_OS6_HOSTS="+filepath.Join(dir, "os6_hosts.txt"))
	}
	cmd.Dir = dir
	cmd.Stdout, cmd.Stderr = lf, lf
	cmd.SysProcAttr = oneoffSysProcAttr()
	if err := cmd.Start(); err != nil {
		lf.Close()
		return fail(err)
	}
	p := &realOneoffProc{cmd: cmd, dir: dir, log: logPath, done: make(chan struct{})}
	go func() {
		err := cmd.Wait()
		lf.Close()
		if ee, ok := err.(*exec.ExitError); ok {
			p.rc = ee.ExitCode()
		} else if err != nil {
			p.rc = -1
		}
		close(p.done)
	}()
	return p, nil
}

// cmdRouteSplit: `auto_setup route-split <목록 파일>` (내부용, x 단독 체크) — 데몬의 수동 run 경로 재판별과 같은 규칙:
// os8_mgmt 에서 준비확인(gossh uptime)이 응답하면 os8, 아니면 os6_mgmt 경유로 확인해 응답한 호스트만 stdout 에 한 줄씩.
// 진행·결과 안내는 stderr (체크 로그에 남는다). os6 미설정·확인 실패면 아무것도 출력하지 않는다 (전부 os8 에서 실행).
func cmdRouteSplit(list string) int {
	b, err := os.ReadFile(list)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[auto_setup] 경로 판별 생략: %v\n", err)
		return 0
	}
	hosts := uniq(strings.Fields(string(b)))
	if len(hosts) == 0 || os6_mgmt == "" || os6_gossh == "" {
		return 0
	}
	fmt.Fprintf(os.Stderr, "[auto_setup] 경로 판별: %d대를 os8_mgmt 에서 확인 중…\n", len(hosts))
	ck := NewRealChecker()
	res, err := ck.Check("local", hosts)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[auto_setup] 경로 판별 실패(os8): %v — 전부 os8 에서 실행\n", err)
		return 0
	}
	var miss []string
	for _, h := range hosts {
		if !res[h].Responded {
			miss = append(miss, h)
		}
	}
	if len(miss) == 0 {
		fmt.Fprintln(os.Stderr, "[auto_setup] 경로 판별: 전부 os8_mgmt 에서 응답")
		return 0
	}
	res, err = ck.Check("os6", miss)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[auto_setup] 경로 판별 실패(os6): %v — 무응답 %d대는 os8 에서 그대로 시도\n", err, len(miss))
		return 0
	}
	var o6 []string
	for _, h := range miss {
		if res[h].Responded {
			o6 = append(o6, h)
			fmt.Println(h)
		}
	}
	fmt.Fprintf(os.Stderr, "[auto_setup] 경로 판별: os6_mgmt 경유 %d대%s, 접속불가 %d대\n", len(o6), listSuffix(o6), len(miss)-len(o6))
	return 0
}

func listSuffix(hs []string) string {
	if len(hs) == 0 {
		return ""
	}
	return " (" + strings.Join(hs, " ") + ")"
}

// ---- 상태 전이 ----

// startOneoff: x 키
func (st *tuiState) startOneoff() {
	if os_check_sh == "" {
		st.sel.Msg = "os_check_sh 가 비어 있어 실행할 수 없습니다 (/etc/auto_setup/auto_setup.conf)"
		return
	}
	st.oo = st.newUserStep(false, nil)
}

// newUserStep: user 선택 단계 상태 (x 와 w 가 공유). 01 의 user_route 메뉴를 읽을 수 있으면 번호 선택, 아니면 이름 직접 입력.
func (st *tuiState) newUserStep(awx bool, p *AwxProfile) *oneoffState {
	if forceUser != "" && userNameRe.MatchString(forceUser) { // auto_setup -user: user 를 묻지 않고 바로 다음 단계
		return &oneoffState{step: ooHosts, user: forceUser, awx: awx, awxProf: p}
	}
	o := &oneoffState{step: ooUser, awx: awx, awxProf: p}
	o.menu, o.route = userMenu()
	if o.menu == nil {
		o.msg = userMenuMissingMsg
	}
	return o
}

const userMenuMissingMsg = "user 메뉴를 찾을 수 없습니다 (awx_dir 의 01 user_route 확인) — user 이름을 직접 입력하세요"

// userMenu: 01 의 user_route 아래 info_mn.sh 출력(user 메뉴 줄)과 user_route 절대 경로. 읽을 수 없으면 (nil, "").
func userMenu() (menu []string, route string) {
	if awx_dir == "" {
		return nil, ""
	}
	route = parseUserRoute(filepath.Join(awx_dir, "01.AWX_nodeinfo_V2.sh"))
	if route == "" {
		return nil, ""
	}
	if !filepath.IsAbs(route) {
		route = filepath.Join(awx_dir, route)
	}
	if out, err := oneoffCmdOut(awx_dir, filepath.Join(route, "info_mn.sh")); err == nil && strings.TrimSpace(out) != "" {
		return splitLines(strings.TrimRight(out, "\n")), route
	}
	return nil, ""
}

// pickUser: user 선택 입력 한 줄 → user 이름. menu 가 있으면 번호로 보고 info.sh 로 이름을 조회하고, 없으면 입력을 이름으로 본다.
// 실패하면 errMsg(화면·CLI 에 보일 안내)를 돌려준다.
func pickUser(menu []string, route, line string) (user, errMsg string) {
	user = strings.TrimSpace(line)
	if menu != nil {
		out, err := oneoffCmdOut(awx_dir, filepath.Join(route, "info.sh"), user)
		if err != nil {
			return "", "[X] user 조회 실패 — 번호를 다시 입력하세요"
		}
		user = strings.TrimSpace(out)
	}
	if !userNameRe.MatchString(user) {
		return "", "[X] user 이름이 올바르지 않습니다 (영문·숫자·. _ - 만) — 다시 입력하세요"
	}
	return user, ""
}

// parseAwxList: AWX 대상 목록 입력 → (<user>.txt 에 쓸 줄, 대상 호스트명).
// 12필드(공백 구분) 줄은 그대로 한 줄로 유지하고 호스트명은 4번째 필드, 그 밖의 줄은 공백·쉼표·| 로 나눠 호스트명 한 줄씩. 중복 제거(순서 유지).
func parseAwxList(raw string) (lines, hosts []string) {
	seenL, seenH := map[string]bool{}, map[string]bool{}
	addH := func(h string) {
		if !seenH[h] {
			seenH[h] = true
			hosts = append(hosts, h)
		}
	}
	for _, ln := range strings.Split(strings.ReplaceAll(raw, "\r", ""), "\n") {
		if f := strings.Fields(ln); len(f) == 12 {
			l := strings.Join(f, " ")
			if !seenL[l] {
				seenL[l] = true
				lines = append(lines, l)
			}
			addH(f[3])
			continue
		}
		for _, h := range normalizeHosts(ln) {
			if !seenL[h] {
				seenL[h] = true
				lines = append(lines, h)
			}
			addH(h)
		}
	}
	return
}

// awxLaunch: w 흐름의 마지막 — 입력한 목록을 awx_dir/<user>.txt 에 쓰고(덮어씀) 대상 확인용 파일을 만든 뒤 01 을 실행한다.
func (st *tuiState) awxLaunch(lines, hosts []string) {
	o := st.oo
	verify := ""
	if len(lines) > 0 {
		if err := os.WriteFile(filepath.Join(awx_dir, o.user+".txt"), []byte(strings.Join(lines, "\n")+"\n"), 0644); err != nil {
			o.msg = "[X] " + o.user + ".txt 쓰기 실패: " + err.Error()
			return
		}
		if f, err := os.CreateTemp("", "as_awx_verify_"); err == nil {
			f.WriteString(strings.Join(hosts, "\n") + "\n")
			f.Close()
			verify = f.Name()
		}
	}
	p, user := o.awxProf, o.user
	st.oo = nil
	st.runAwx(p, user, verify)
	if verify != "" {
		os.Remove(verify)
	}
}

func (st *tuiState) handleOneoff(ev keyEv) {
	o := st.oo
	switch o.step {
	case ooUser:
		st.ooUserKey(ev)
	case ooHosts:
		st.ooHostsKey(ev)
	case ooMode:
		st.ooModeKey(ev)
	case ooRun:
		st.ooRunKey(ev)
	}
}

// editLine: 한 줄 입력 편집. 처리했으면 true.
func (o *oneoffState) editLine(ev keyEv) bool {
	switch {
	case ev.k == kRune && ev.r == runeBackspace:
		if n := len(o.in); n > 0 {
			o.in = o.in[:n-1]
		}
		return true
	case ev.k == kRune && ev.r >= 0x20 && ev.r != 0x7f:
		o.in = append(o.in, ev.r)
		return true
	}
	return false
}

func (st *tuiState) ooCancel() { st.oo = nil }

func (st *tuiState) ooUserKey(ev keyEv) {
	o := st.oo
	switch {
	case ev.k == kEsc || (ev.k == kRune && ev.r == 'q' && len(o.in) == 0):
		st.ooCancel()
	case ev.k == kEnter:
		line := strings.TrimSpace(string(o.in))
		if line == "" {
			return
		}
		user, errMsg := pickUser(o.menu, o.route, line)
		if errMsg == "" && o.onUser != nil && !validChangeUser(user) { // 세션 user 는 디렉터리 이름으로도 쓰이므로 . 와 .. 불가
			errMsg = "[X] user 이름이 올바르지 않습니다 (. 와 .. 는 쓸 수 없음) — 다시 입력하세요"
		}
		if errMsg != "" {
			o.msg, o.in = errMsg, nil
			return
		}
		if o.onUser != nil { // 세션 user: 기억하고 이 단계를 닫은 뒤 다음 단계로 이어 준다
			next := o.onUser
			st.oo, sessionUser = nil, user
			next(user)
			return
		}
		o.user, o.in, o.msg, o.step = user, nil, "", ooHosts
	default:
		o.editLine(ev)
	}
}

func (st *tuiState) ooHostsKey(ev keyEv) {
	o := st.oo
	switch {
	case ev.k == kEsc || (ev.k == kRune && ev.r == 'q' && len(o.in) == 0):
		st.ooCancel()
	case ev.k == kRune && ev.r == runeCtrlD && o.awx:
		// AWX: 목록 없이 Ctrl+D 면 건너뜀 (기존 <user>.txt 사용, 대상 확인 생략)
		lines, hosts := parseAwxList(string(o.in))
		st.awxLaunch(lines, hosts)
	case ev.k == kRune && ev.r == runeCtrlD:
		hosts := normalizeHosts(string(o.in))
		if len(hosts) == 0 {
			o.msg = "호스트가 없습니다 — 1대 이상 입력하세요"
			return
		}
		o.hosts, o.msg, o.step = hosts, "", ooMode
	case ev.k == kEnter:
		o.in = append(o.in, '\n')
	case ev.k == kRune && ev.r == '\t':
		o.in = append(o.in, '\t')
	default:
		o.editLine(ev)
	}
}

func (st *tuiState) ooModeKey(ev keyEv) {
	switch {
	case ev.k == kEsc:
		st.ooCancel()
	case ev.k == kRune && (ev.r == 't' || ev.r == 'T'):
		st.ooLaunch(ModeCheck)
	case ev.k == kRune && (ev.r == 'c' || ev.r == 'C'):
		st.ooLaunch("")
	}
}

func (st *tuiState) ooLaunch(mode string) {
	o := st.oo
	p, err := oneoffStart(os_check_sh, o.user, mode, o.hosts)
	if err != nil {
		o.msg = "[X] 실행 시작 실패: " + err.Error()
		return
	}
	o.mode, o.proc, o.step, o.started, o.msg = mode, p, ooRun, st.env.Now(), ""
	if st.fastTick == nil {
		st.fastTick, st.fastStop = st.env.Tick(time.Second)
	}
}

func (st *tuiState) ooRunKey(ev keyEv) {
	o := st.oo
	if o.cancelAsk {
		o.cancelAsk = false
		if ev.k == kRune && (ev.r == 'y' || ev.r == 'Y') {
			o.cancelled = true
			o.proc.Kill()
			o.msg = "취소 요청을 보냈습니다 — 종료를 기다리는 중"
		} else {
			o.msg = ""
		}
		return
	}
	switch {
	case ev.k == kRune && ev.r == runeCtrlX:
		select {
		case <-o.proc.Done():
		default:
			o.cancelAsk, o.msg = true, ""
		}
	case ev.k == kEsc || (ev.k == kRune && (ev.r == 'q' || ev.r == 'Q')):
		o.msg = "실행 중에는 닫을 수 없습니다 — 취소는 Ctrl+X"
	}
}

// pollOneoff: 실행이 끝났으면 결과 화면으로 (이벤트·tick 마다 호출)
func (st *tuiState) pollOneoff() {
	o := st.oo
	if o == nil || o.step != ooRun {
		return
	}
	select {
	case <-o.proc.Done():
	default:
		return
	}
	st.stopFastTick()
	title := fmt.Sprintf("체크스크립트 단독 실행 결과 [%s] - user %s %d대", modeLabel(o.mode), o.user, len(o.hosts))
	v := &viewState{kind: viewOneoff, title: title, done: true, cleanup: o.proc.Dir()}
	if o.cancelled {
		v.status = "취소됨"
		v.lines = append([]string{"취소됨 — 그때까지의 로그 꼬리:", ""}, tailLines(o.proc.LogPath(), 30)...)
	} else {
		logText, _ := os.ReadFile(o.proc.LogPath())
		resFile := "check.res_" + o.user + "_postapply"
		if o.mode == ModeCheck {
			resFile = "check.res_" + o.user
		}
		post, postErr := os.ReadFile(filepath.Join(o.proc.Dir(), resFile))
		text, _ := buildCodeTextMode(o.mode, string(logText), string(post), postErr == nil, o.hosts, o.proc.LogPath())
		v.status = fmt.Sprintf("완료 (rc=%d)", o.proc.Rc())
		v.lines = splitLines(strings.TrimRight(text, "\n"))
	}
	st.view, st.oo = v, nil
}

func (st *tuiState) stopFastTick() {
	if st.fastStop != nil {
		st.fastStop()
	}
	st.fastTick, st.fastStop = nil, nil
}

// closeView: 보기를 닫고 단독 실행의 임시 디렉터리를 지운다
func (st *tuiState) closeView() {
	if st.view != nil && st.view.cleanup != "" {
		os.RemoveAll(st.view.cleanup)
	}
	st.view = nil
}

// oneoffShutdown: TUI 종료 시 실행 중 프로세스·임시 디렉터리 정리
func (st *tuiState) oneoffShutdown() {
	st.stopFastTick()
	if o := st.oo; o != nil && o.proc != nil {
		o.proc.Kill()
		os.RemoveAll(o.proc.Dir())
	}
	if st.view != nil && st.view.cleanup != "" {
		os.RemoveAll(st.view.cleanup)
	}
}

// ---- 화면 ----

func renderOneoff(o *oneoffState, now time.Time, width, height int, color bool) string {
	width = clampWidth(width)
	var title string
	var pre, post []string // 본문: 고정 머리 / 항상 남기는 입력 줄 (모두 줄바꿈이 끝난 화면 줄)
	var list [][]string    // 넘치면 줄이는 목록 (블록 하나 = 원래 한 줄이 줄바꿈된 화면 줄들)
	keepTail := false      // 목록이 넘칠 때 true 면 끝(최근)을 남기고 false 면 처음을 남김
	var hint string
	// 본문 한 줄: 앞 한 칸 들여쓰고, 폭에 들어가면 종전 그대로, 넘치면 단어 단위로 줄바꿈 (이어지는 줄은 3칸 들여씀)
	line := func(l string) []string { return wrapSegs(color, width, 1, "   ", seg{" " + l, ""}) }
	switch o.step {
	case ooUser:
		title = o.name() + " - user 선택"
		if o.onUser != nil {
			title = "이 세션에서 사용할 user 선택 (TUI 를 닫을 때까지 기억합니다)"
		}
		if o.menu != nil {
			for _, m := range o.menu {
				list = append(list, line(m))
			}
			post = append(line(""), inputLines(color, width, "번호 입력 > "+string(o.in)+"_")...)
			hint = " Enter 선택  Esc 취소 (입력이 비었을 때 q 도 취소)"
		} else {
			post = inputLines(color, width, "user 이름 입력 > "+string(o.in)+"_")
			hint = " Enter 확인  Esc 취소 (입력이 비었을 때 q 도 취소)"
		}
	case ooHosts:
		title = o.name() + " - user " + o.user + " - " + map[bool]string{true: "작업 대상 서버 목록", false: "호스트 입력"}[o.awx]
		// 한 문장이지만 80칸 터미널에서 잘리지 않게 두 줄로 나눠 보여 준다
		var hs, intro []string
		if o.awx {
			intro = []string{"작업 대상 서버 목록을 붙여넣은 뒤 Ctrl+D 를 누르세요 (호스트명 또는 12필드 줄)",
				"(공백·쉼표·| 구분 가능, Esc 취소) — 비워 두고 Ctrl+D 면 건너뜁니다",
				"※ 입력하면 " + filepath.Join(awx_dir, o.user+".txt") + " 를 덮어쓰고, 01 의 '등록 후 확인' 에도 자동 입력됩니다", ""}
			_, hs = parseAwxList(string(o.in))
		} else {
			intro = []string{"호스트를 입력하거나 붙여넣은 뒤 Ctrl+D 를 누르세요", "(공백·쉼표·탭·| 는 줄바꿈으로 바뀝니다, Esc 취소)", ""}
			hs = normalizeHosts(string(o.in))
		}
		for _, l := range intro {
			pre = append(pre, line(l)...)
		}
		pre = append(pre, line(fmt.Sprintf("입력된 호스트 %d대:", len(hs)))...)
		if len(hs) > 0 { // 입력한 호스트 에코 — 폭에 맞춰 이어 붙이고 넘치면 호스트 단위로 다음 줄(긴 호스트명은 글자 단위)
			for _, row := range wrapSegs(color, width, 1, "   ", seg{"   " + strings.Join(hs, " "), ""}) {
				list = append(list, []string{row})
			}
		}
		keepTail = true
		cur := string(o.in)
		if i := strings.LastIndex(cur, "\n"); i >= 0 {
			cur = cur[i+1:]
		}
		post = append(line(""), inputLines(color, width, "> "+cur+"_")...)
		hint = " Enter 줄바꿈  Ctrl+D 입력 완료  Esc 취소 (입력이 비었을 때 q 도 취소)"
		if o.awx {
			hint = " Enter 줄바꿈  Ctrl+D 입력 완료 (비었으면 건너뜀)  Esc 취소"
		}
	case ooMode:
		title = "체크스크립트 단독 실행 - 모드 선택"
		pre = append(line(fmt.Sprintf("user %s   호스트 %d대", o.user, len(o.hosts))), line("")...)
		pre = append(pre, wrapSegs(color, width, 1, "     ", seg{"   t 설정체크만 (환경설정 수정 안 함)", stGreen})...)
		pre = append(pre, wrapSegs(color, width, 1, "     ", seg{"   c 설정체크 + ", stGreen}, seg{"설정수정 (환경설정 수정함)", stRed})...)
		hint = " t / c 를 누르면 바로 실행합니다  Esc 취소"
	case ooRun:
		title = fmt.Sprintf("체크스크립트 단독 실행 [%s] - user %s %d대", modeLabel(o.mode), o.user, len(o.hosts))
		el := int(now.Sub(o.started).Seconds())
		if el < 0 {
			el = 0
		}
		pre = append(line(fmt.Sprintf("경과 %d분 %02d초", el/60, el%60)), line("")...)
		for _, l := range tailLines(o.proc.LogPath(), oneoffTailN) {
			list = append(list, line(l))
		}
		keepTail = true // 로그는 최근 줄이 보여야 한다
		hint = " 실행 중 — 취소: Ctrl+X"
		if o.cancelAsk {
			hint = " 실행을 취소할까요? [y/n]"
		}
	}
	out := append(wrapSegs(color, width, 1, "  ", redSegs(" "+title, stBold)...), sepLine(width))
	foot := []string{sepLine(width)}
	if o.msg != "" {
		foot = append(foot, wrapSegs(color, width, 1, "  ", seg{" " + o.msg, stYellow})...)
	}
	foot = append(foot, hintLines(color, width, hint)...)
	avail := 0 // 0 = 높이 제한 없음
	if height > 0 {
		avail = rMax(height-len(out)-len(foot), 1)
	}
	out = append(out, fitBody(color, avail, keepTail, pre, list, post)...)
	if height > 0 {
		for len(out) < height-len(foot) {
			out = append(out, "")
		}
		if len(out) > height-len(foot) {
			out = out[:rMax(height-len(foot), 1)]
		}
	}
	return strings.Join(append(out, foot...), "\n")
}

// inputLines: 입력 한 줄(프롬프트 포함)을 글자 단위로 줄바꿈한다 — 편집 중인 글자가 단어 단위로 튀지 않고,
// 긴 입력도 프롬프트 바로 뒤에서 이어진다. 폭에 들어가면 종전과 같은 한 줄 (앞 한 칸 들여쓰기, 이어지는 줄은 3칸).
func inputLines(color bool, width int, text string) []string {
	text = cleanText(" " + text)
	if strWidth(text) <= width {
		return []string{segLine(color, width, seg{text, ""})}
	}
	var out []string
	for _, l := range wrapAtoms(width, "", "   ", []atom{{segs: []seg{{text, ""}}}}) {
		out = append(out, segLine(color, width, l...))
	}
	return out
}

// fitBody: 본문(pre + list + post, 모두 줄바꿈이 끝난 화면 줄)을 avail 줄 안에 넣는다. avail <= 0 이면 전부.
// list 는 블록(원래 한 줄이 줄바꿈된 여러 줄) 목록이다. post(입력 줄)는 항상 남기고(post 자체가 길면 커서가
// 있는 끝을 남김), list 가 넘치면 keepTail 에 따라 앞(위) 또는 뒤(아래)를 블록 단위로 버리고 "(위/아래 N줄 생략)"
// 한 줄로 바꾼다. 입력 줄이 화면 밖으로 밀리지 않게 하는 것이 목적.
func fitBody(color bool, avail int, keepTail bool, pre []string, list [][]string, post []string) []string {
	n := 0
	for _, b := range list {
		n += len(b)
	}
	flat := func(blocks [][]string) []string {
		var out []string
		for _, b := range blocks {
			out = append(out, b...)
		}
		return out
	}
	if avail <= 0 || len(pre)+n+len(post) <= avail {
		return append(append(append([]string{}, pre...), flat(list)...), post...)
	}
	if len(post) >= avail {
		return append([]string{}, post[len(post)-avail:]...)
	}
	budget := avail - len(post)
	room := budget - len(pre) // list 에 줄 수 있는 줄 (생략 안내 한 줄 포함)
	dir := map[bool]string{true: "위", false: "아래"}[keepTail]
	mark := func(hidden int) string {
		return paint(color, stGray, fmt.Sprintf("   (%s %d줄 생략)", dir, hidden))
	}
	var mid []string
	switch {
	case room >= 2:
		var kept []string
		if keepTail { // 끝에서부터 블록 단위로 채움 (블록 하나가 room-1 보다 길면 그 블록의 끝 부분)
			for i := len(list) - 1; i >= 0 && len(kept) < room-1; i-- {
				if b := list[i]; len(kept)+len(b) <= room-1 {
					kept = append(append([]string{}, b...), kept...)
				} else if len(kept) == 0 {
					kept = append([]string{}, b[len(b)-(room-1):]...)
				} else {
					break
				}
			}
			mid = append([]string{mark(n - len(kept))}, kept...)
		} else {
			for i := 0; i < len(list) && len(kept) < room-1; i++ {
				if b := list[i]; len(kept)+len(b) <= room-1 {
					kept = append(kept, b...)
				} else if len(kept) == 0 {
					kept = append(kept, b[:room-1]...)
				} else {
					break
				}
			}
			mid = append(kept, mark(n-len(kept)))
		}
	case room == 1:
		mid = []string{mark(n)}
	default:
		pre = pre[:budget] // 머리만으로도 넘치는 아주 작은 창
	}
	return append(append(append([]string{}, pre...), mid...), post...)
}

// name: 화면 제목 머리말
func (o *oneoffState) name() string {
	if o.awx {
		return "AWX 실행"
	}
	return "체크스크립트 단독 실행"
}
