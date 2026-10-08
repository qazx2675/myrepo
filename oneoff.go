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
	o := &oneoffState{step: ooUser, awx: awx, awxProf: p}
	if awx_dir != "" {
		if route := parseUserRoute(filepath.Join(awx_dir, "01.AWX_nodeinfo_V2.sh")); route != "" {
			if !filepath.IsAbs(route) {
				route = filepath.Join(awx_dir, route)
			}
			if out, err := oneoffCmdOut(awx_dir, filepath.Join(route, "info_mn.sh")); err == nil && strings.TrimSpace(out) != "" {
				o.menu, o.route = splitLines(strings.TrimRight(out, "\n")), route
			}
		}
	}
	if o.menu == nil {
		o.msg = "user 메뉴를 찾을 수 없습니다 (awx_dir 의 01 user_route 확인) — user 이름을 직접 입력하세요"
	}
	return o
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
		user := line
		if o.menu != nil {
			out, err := oneoffCmdOut(awx_dir, filepath.Join(o.route, "info.sh"), line)
			if err != nil {
				o.msg, o.in = "[X] user 조회 실패 — 번호를 다시 입력하세요", nil
				return
			}
			user = strings.TrimSpace(out)
		}
		if !userNameRe.MatchString(user) {
			o.msg, o.in = "[X] user 이름이 올바르지 않습니다 (영문·숫자·. _ - 만) — 다시 입력하세요", nil
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
	var body []string
	var hint string
	switch o.step {
	case ooUser:
		title = o.name() + " - user 선택"
		if o.menu != nil {
			body = append(body, o.menu...)
			body = append(body, "", "번호 입력 > "+string(o.in)+"_")
			hint = " Enter 선택  Esc 취소 (입력이 비었을 때 q 도 취소)"
		} else {
			body = append(body, "user 이름 입력 > "+string(o.in)+"_")
			hint = " Enter 확인  Esc 취소 (입력이 비었을 때 q 도 취소)"
		}
	case ooHosts:
		title = o.name() + " - user " + o.user + " - " + map[bool]string{true: "작업 대상 서버 목록", false: "호스트 입력"}[o.awx]
		// 한 문장이지만 80칸 터미널에서 잘리지 않게 두 줄로 나눠 보여 준다
		var hs []string
		if o.awx {
			body = append(body, "작업 대상 서버 목록을 붙여넣은 뒤 Ctrl+D 를 누르세요 (호스트명 또는 12필드 줄)",
				"(공백·쉼표·| 구분 가능, Esc 취소) — 비워 두고 Ctrl+D 면 건너뜁니다",
				"※ 입력하면 "+filepath.Join(awx_dir, o.user+".txt")+" 를 덮어쓰고, 01 의 '등록 후 확인' 에도 자동 입력됩니다", "")
			_, hs = parseAwxList(string(o.in))
		} else {
			body = append(body, "호스트를 입력하거나 붙여넣은 뒤 Ctrl+D 를 누르세요", "(공백·쉼표·탭·| 는 줄바꿈으로 바뀝니다, Esc 취소)", "")
			hs = normalizeHosts(string(o.in))
		}
		body = append(body, fmt.Sprintf("입력된 호스트 %d대:", len(hs)))
		room := rMax(height-9, 3)
		line := ""
		var rows []string
		for _, h := range hs {
			if line != "" && strWidth(line)+1+strWidth(h) > width-3 {
				rows = append(rows, "  "+line)
				line = ""
			}
			if line != "" {
				line += " "
			}
			line += h
		}
		if line != "" {
			rows = append(rows, "  "+line)
		}
		if len(rows) > room {
			rows = append([]string{"  …"}, rows[len(rows)-room+1:]...)
		}
		body = append(body, rows...)
		body = append(body, "", "> ")
		cur := string(o.in)
		if i := strings.LastIndex(cur, "\n"); i >= 0 {
			cur = cur[i+1:]
		}
		body[len(body)-1] += cur + "_"
		hint = " Enter 줄바꿈  Ctrl+D 입력 완료  Esc 취소 (입력이 비었을 때 q 도 취소)"
		if o.awx {
			hint = " Enter 줄바꿈  Ctrl+D 입력 완료 (비었으면 건너뜀)  Esc 취소"
		}
	case ooMode:
		title = "체크스크립트 단독 실행 - 모드 선택"
		body = []string{fmt.Sprintf("user %s   호스트 %d대", o.user, len(o.hosts)), "",
			"  t 설정체크만 (환경설정 수정 안 함)"}
		hint = " t / c 를 누르면 바로 실행합니다  Esc 취소"
	case ooRun:
		title = fmt.Sprintf("체크스크립트 단독 실행 [%s] - user %s %d대", modeLabel(o.mode), o.user, len(o.hosts))
		el := int(now.Sub(o.started).Seconds())
		if el < 0 {
			el = 0
		}
		body = append(body, fmt.Sprintf("경과 %d분 %02d초", el/60, el%60), "")
		body = append(body, tailLines(o.proc.LogPath(), oneoffTailN)...)
		hint = " 실행 중 — 취소: Ctrl+X"
		if o.cancelAsk {
			hint = " 실행을 취소할까요? [y/n]"
		}
	}
	out := []string{segLine(color, width, redSegs(" "+title, stBold)...), sepLine(width)}
	for _, l := range body {
		switch {
		case o.step == ooMode && strings.HasPrefix(l, "  t "):
			out = append(out, segLine(color, width, seg{" " + l, stGreen}))
			out = append(out, segLine(color, width, seg{"   c 설정체크 + ", stGreen}, seg{"설정수정 (환경설정 수정함)", stRed}))
		default:
			out = append(out, segLine(color, width, seg{" " + l, ""}))
		}
	}
	foot := []string{sepLine(width)}
	if o.msg != "" {
		foot = append(foot, segLine(color, width, seg{" " + o.msg, stYellow}))
	}
	foot = append(foot, segLine(color, width, hintSegs(hint)...))
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

// name: 화면 제목 머리말
func (o *oneoffState) name() string {
	if o.awx {
		return "AWX 실행"
	}
	return "체크스크립트 단독 실행"
}
