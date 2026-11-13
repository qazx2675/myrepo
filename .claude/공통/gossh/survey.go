package main

// -sv : 여러 서버에 같은 조사 명령어들을 실행해 "호스트|결과1|결과2|..." 표 하나로 모으는 옵션.
// 명령어는 대화형으로 입력받고(수량 → 명령어 → 미리보기 → 전체 실행), 결과는 표준출력 표,
// <호스트파일>_survey.tsv(엑셀 붙여넣기용), <호스트파일>_survey.txt(| 구분)로 남긴다.
// ★ 명령어는 한 글자도 바꾸지 않고 원격 bash 의 표준입력으로 보낸다("bash -s"). 따옴표·$·|·역슬래시를
// 서버에서 직접 친 것과 똑같이 해석하며, 원격 로그인 셸이 csh/tcsh 여도 영향이 없다.

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/crypto/ssh"
)

const (
	sufSurveyTSV = "_survey.tsv"
	sufSurveyTxt = "_survey.txt"
)

// surveyCell은 명령 하나의 결과다. ok=false 는 접속 실패 등으로 실행하지 못한 칸("-")이다.
type surveyCell struct {
	out string
	ok  bool
}

// surveyHost는 호스트 하나의 조사 결과다.
type surveyHost struct {
	reachable bool
	err       string // 접속불가 사유(화면 안내용)
	cells     []surveyCell
}

// surveyRunFunc는 hosts 에 cmds 를 실행해 호스트별 결과를 돌려준다(full: 전체 실행이면 접속불가 재검증을 한다).
type surveyRunFunc func(hosts, cmds []string, full bool) map[string]surveyHost

// ---------------------------------------------------------------- 표 만들기

var surveyCRRegex = strings.NewReplacer("\r\n", "\n", "\r", "\n")

// normalizeSurveyOut은 결과의 색상 코드를 지우고 앞뒤 공백·줄바꿈을 자른다(안쪽 줄바꿈은 그대로 둔다).
func normalizeSurveyOut(s string) string {
	return strings.TrimSpace(surveyCRRegex.Replace(stripANSI(s)))
}

// surveyLineCount는 정리한 결과의 줄 수다(빈 결과는 0).
func surveyLineCount(norm string) int {
	if norm == "" {
		return 0
	}
	return strings.Count(norm, "\n") + 1
}

// surveyCellText는 표 칸에 넣을 글자다. 실행하지 못한 칸은 "-", join 이면 줄바꿈을 "/" 로 바꾼다.
func surveyCellText(c surveyCell, join bool) string {
	if !c.ok {
		return "-"
	}
	t := normalizeSurveyOut(c.out)
	if join {
		t = strings.ReplaceAll(t, "\n", "/")
	}
	return t
}

// buildSurveyTable은 머리줄(hostname|명령어...)과 호스트별 줄을 호스트 파일 순서대로 만든다.
// 접속불가 호스트는 모든 칸이 "-", 접속된 호스트의 빈 결과는 빈 칸이다.
func buildSurveyTable(cmds, hosts []string, res map[string]surveyHost, join bool) [][]string {
	rows := [][]string{append([]string{"hostname"}, cmds...)}
	for _, h := range hosts {
		row := []string{h}
		r, ok := res[h]
		for i := range cmds {
			if !ok || !r.reachable || i >= len(r.cells) {
				row = append(row, "-")
				continue
			}
			row = append(row, surveyCellText(r.cells[i], join))
		}
		rows = append(rows, row)
	}
	return rows
}

// renderSurveyTable은 표를 sep 로 이은 글자로 만든다. sep 이 탭이면 칸 안의 탭은 공백으로 바꾼다
// (탭이 남으면 엑셀에서 칸이 밀린다).
func renderSurveyTable(rows [][]string, sep string) string {
	var b strings.Builder
	for _, row := range rows {
		cells := row
		if sep == "\t" {
			cells = make([]string, len(row))
			for i, c := range row {
				cells[i] = strings.ReplaceAll(c, "\t", " ")
			}
		}
		b.WriteString(strings.Join(cells, sep))
		b.WriteString("\n")
	}
	return b.String()
}

// surveyMulti는 결과가 2줄 이상인 칸이 있는 명령 하나의 요약이다.
type surveyMulti struct {
	cmdIdx int // 0부터
	hosts  []string
}

// findSurveyMulti는 결과가 2줄 이상인 칸을 명령별로 모은다(명령 번호 순).
func findSurveyMulti(cmds, hosts []string, res map[string]surveyHost) []surveyMulti {
	var out []surveyMulti
	for i := range cmds {
		var hs []string
		for _, h := range hosts {
			r, ok := res[h]
			if !ok || !r.reachable || i >= len(r.cells) || !r.cells[i].ok {
				continue
			}
			if surveyLineCount(normalizeSurveyOut(r.cells[i].out)) >= 2 {
				hs = append(hs, h)
			}
		}
		if len(hs) > 0 {
			out = append(out, surveyMulti{cmdIdx: i, hosts: hs})
		}
	}
	return out
}

// ---------------------------------------------------------------- 미리보기 입력 해석

type previewKind int

const (
	pvRunFirst previewKind = iota // 엔터: 호스트 파일의 첫 호스트로 확인
	pvRunHost                     // 호스트명: 그 호스트로 확인
	pvEdit                        // Ni
	pvAdd                         // a
	pvDel                         // Nd
	pvNext                        // next
	pvBad                         // 알 수 없는 입력
)

type previewKey struct {
	kind previewKind
	n    int    // 1부터(pvEdit, pvDel)
	host string // pvRunHost
	msg  string // pvBad 안내
}

var previewNumKeyRegex = regexp.MustCompile(`^([0-9]+)([idID])$`)

// parsePreviewKey는 미리보기 화면에서 입력한 글자를 해석한다. nCmds 는 현재 명령어 수다.
func parsePreviewKey(in string, nCmds int) previewKey {
	in = strings.TrimSpace(in)
	if in == "" {
		return previewKey{kind: pvRunFirst}
	}
	switch strings.ToLower(in) {
	case "next":
		return previewKey{kind: pvNext}
	case "a":
		return previewKey{kind: pvAdd}
	}
	if m := previewNumKeyRegex.FindStringSubmatch(in); m != nil {
		n, _ := strconv.Atoi(m[1])
		if n < 1 || n > nCmds {
			return previewKey{kind: pvBad, msg: fmt.Sprintf("%d번 명령어는 없습니다 (1~%d).", n, nCmds)}
		}
		if strings.ToLower(m[2]) == "i" {
			return previewKey{kind: pvEdit, n: n}
		}
		if nCmds <= 1 {
			return previewKey{kind: pvBad, msg: "명령어가 1개뿐이라 삭제할 수 없습니다."}
		}
		return previewKey{kind: pvDel, n: n}
	}
	if strings.ContainsAny(in, " \t") {
		return previewKey{kind: pvBad, msg: "알 수 없는 입력입니다. 호스트명에는 공백이 들어갈 수 없습니다."}
	}
	return previewKey{kind: pvRunHost, host: in}
}

// ---------------------------------------------------------------- 입력

// readSurveyLine은 표준입력에서 한 줄을 있는 그대로 읽는다(끝의 줄바꿈과 \r 만 뺀다). 입력이 끝났으면 ok=false.
func readSurveyLine() (line string, ok bool) {
	s, err := stdinReader.ReadString('\n')
	if err != nil && s == "" {
		return "", false
	}
	return strings.TrimRight(s, "\r\n"), true
}

// surveyPrompt는 질문을 표준에러로 찍고 한 줄을 읽는다. 입력이 끝났으면 안내 후 종료한다.
func surveyPrompt(msg string) string {
	fmt.Fprint(os.Stderr, msg)
	line, ok := readSurveyLine()
	if !ok {
		fmt.Fprintln(os.Stderr, "\n입력이 끝나 조사를 중단합니다.")
		os.Exit(1)
	}
	return line
}

// askSurveyCount는 "1. 조사할 명령어 수량 : " 을 1 이상의 정수가 입력될 때까지 묻는다.
func askSurveyCount() int {
	for {
		s := strings.TrimSpace(surveyPrompt("1. 조사할 명령어 수량 : "))
		if n, err := strconv.Atoi(s); err == nil && n >= 1 {
			return n
		}
		fmt.Fprintln(os.Stderr, "1 이상의 숫자를 입력하세요.")
	}
}

// askSurveyCommand는 명령어 한 줄을 있는 그대로 받는다. 빈 줄·위험 명령이면 다시 묻는다.
func askSurveyCommand(prompt string) string {
	for {
		line := surveyPrompt(prompt)
		if strings.TrimSpace(line) == "" {
			fmt.Fprintln(os.Stderr, "명령어가 비어 있습니다. 다시 입력하세요.")
			continue
		}
		if isDangerousCommand(line) {
			fmt.Fprintln(os.Stderr, "위험 명령(reboot/shutdown/mkfs 등)이 들어 있어 조사 명령어로 쓸 수 없습니다. 다시 입력하세요.")
			continue
		}
		return line
	}
}

// askSurveyCommands는 수량과 명령어들을 차례로 묻는다.
func askSurveyCommands() []string {
	n := askSurveyCount()
	cmds := make([]string, 0, n)
	for k := 1; k <= n; k++ {
		cmds = append(cmds, askSurveyCommand(fmt.Sprintf("2. 조사 명령어 %d/%d : ", k, n)))
	}
	return cmds
}

// ---------------------------------------------------------------- 화면

func surveyWarn(s string) string { return colorize(colorYellow, s) }

func printSurveyList(cmds []string) {
	var b strings.Builder
	b.WriteString("\n[조사 명령어]\n")
	for i, c := range cmds {
		fmt.Fprintf(&b, "  %d. %s\n", i+1, c)
	}
	fmt.Fprint(os.Stderr, b.String())
}

const surveyPreviewPrompt = "엔터=첫 호스트로 확인 | 호스트명=그 호스트로 확인 | Ni=N번 수정(예 2i) | a=추가 | Nd=N번 삭제(예 3d) | next=전체 실행\n> "

// printSurveyPreview는 한 호스트의 결과를 명령어마다 보여주고, 한 줄이 아닌 결과는 바로 경고한다.
func printSurveyPreview(host string, cmds []string, r surveyHost) {
	var b strings.Builder
	if !r.reachable {
		b.WriteString(colorize(colorRed, fmt.Sprintf("\n[확인] %s : 접속할 수 없습니다 (%s)", host, r.err)) + "\n")
		fmt.Fprint(os.Stderr, b.String())
		return
	}
	fmt.Fprintf(&b, "\n[확인] %s\n", host)
	for i, c := range cmds {
		fmt.Fprintf(&b, "  %d. %s\n", i+1, c)
		if i >= len(r.cells) || !r.cells[i].ok {
			b.WriteString(surveyWarn("     ※ 경고: 이 명령은 실행하지 못했습니다.") + "\n")
			continue
		}
		norm := normalizeSurveyOut(r.cells[i].out)
		lines := surveyLineCount(norm)
		if lines == 0 {
			b.WriteString("     (결과 없음)\n")
			b.WriteString(surveyWarn("     ※ 경고: 결과가 비어 있습니다.") + "\n")
			continue
		}
		for _, l := range strings.Split(norm, "\n") {
			b.WriteString("     " + l + "\n")
		}
		if lines >= 2 {
			b.WriteString(surveyWarn(fmt.Sprintf("     ※ 경고: 결과가 %d줄입니다. 한 줄로 나오게 명령어를 고치세요 (Ni).", lines)) + "\n")
		}
	}
	fmt.Fprint(os.Stderr, b.String())
}

// surveyHostSample은 호스트 이름을 최대 max 개까지 보여주고 나머지는 "외 N대" 로 줄인다.
func surveyHostSample(hosts []string, max int) string {
	if len(hosts) <= max {
		return strings.Join(hosts, ", ")
	}
	return fmt.Sprintf("%s 외 %d대", strings.Join(hosts[:max], ", "), len(hosts)-max)
}

func printSurveyMultiWarning(multi []surveyMulti) {
	var b strings.Builder
	b.WriteString("\n" + surveyWarn("[경고] 결과가 2줄 이상인 칸이 있습니다.") + "\n")
	for _, m := range multi {
		fmt.Fprintf(&b, "  명령 %d번: %d대 (%s)\n", m.cmdIdx+1, len(m.hosts), surveyHostSample(m.hosts, 5))
	}
	fmt.Fprint(os.Stderr, b.String())
}

// ---------------------------------------------------------------- 진행 흐름

// surveyPreviewLoop는 명령어 목록을 보여주고 키를 받아 next 가 입력될 때까지 확인·수정한다.
func surveyPreviewLoop(cmds []string, hosts []string, run surveyRunFunc) []string {
	for {
		printSurveyList(cmds)
		key := parsePreviewKey(surveyPrompt(surveyPreviewPrompt), len(cmds))
		switch key.kind {
		case pvNext:
			return cmds
		case pvRunFirst, pvRunHost:
			h := key.host
			if key.kind == pvRunFirst {
				h = hosts[0]
			}
			fmt.Fprintf(os.Stderr, "%s 에서 명령어 %d개를 실행합니다...\n", h, len(cmds))
			printSurveyPreview(h, cmds, run([]string{h}, cmds, false)[h])
		case pvEdit:
			fmt.Fprintf(os.Stderr, "현재 %d번 명령어: %s\n", key.n, cmds[key.n-1])
			line := surveyPrompt("새 명령어 (엔터=그대로 유지) : ")
			switch {
			case line == "":
				fmt.Fprintln(os.Stderr, "그대로 유지합니다.")
			case isDangerousCommand(line):
				fmt.Fprintln(os.Stderr, "위험 명령(reboot/shutdown/mkfs 등)이 들어 있어 바꾸지 않았습니다.")
			default:
				cmds[key.n-1] = line
			}
		case pvAdd:
			line := surveyPrompt("추가할 명령어 (엔터=취소) : ")
			switch {
			case strings.TrimSpace(line) == "":
				fmt.Fprintln(os.Stderr, "추가하지 않았습니다.")
			case isDangerousCommand(line):
				fmt.Fprintln(os.Stderr, "위험 명령(reboot/shutdown/mkfs 등)이 들어 있어 추가하지 않았습니다.")
			default:
				cmds = append(cmds, line)
			}
		case pvDel:
			cmds = append(cmds[:key.n-1], cmds[key.n:]...)
			fmt.Fprintf(os.Stderr, "%d번 명령어를 삭제했습니다.\n", key.n)
		case pvBad:
			fmt.Fprintln(os.Stderr, key.msg)
		}
	}
}

// surveyFlow는 질문 → 미리보기 → 전체 실행 → (2줄 이상 결과가 있으면) 다시 입력 여부 확인까지 진행하고,
// 최종 명령어·결과와 줄바꿈을 "/" 로 이어야 하는지(join)를 돌려준다.
func surveyFlow(hosts []string, run surveyRunFunc) (cmds []string, res map[string]surveyHost, join bool) {
	cmds = askSurveyCommands()
	for {
		cmds = surveyPreviewLoop(cmds, hosts, run)
		fmt.Fprintf(os.Stderr, "\n전체 실행: %d대 x 명령어 %d개 (동시 실행)\n", len(hosts), len(cmds))
		res = run(hosts, cmds, true)
		multi := findSurveyMulti(cmds, hosts, res)
		if len(multi) == 0 {
			return cmds, res, false
		}
		printSurveyMultiWarning(multi)
		fmt.Fprint(os.Stderr, "명령어를 다시 입력하시겠습니까? (y/N) : ")
		ans, _ := readSurveyLine()
		if strings.ToLower(strings.TrimSpace(ans)) == "y" {
			continue
		}
		fmt.Fprintln(os.Stderr, "줄바꿈은 / 로 이어서 한 줄로 만듭니다.")
		return cmds, res, true
	}
}

// printSurveyDone은 표를 표준출력에 찍고, 파일 두 개를 저장한 뒤 경로를 표준에러로 알린다.
func printSurveyDone(cmds, hosts []string, res map[string]surveyHost, join bool, hostFile string) {
	rows := buildSurveyTable(cmds, hosts, res, join)
	fmt.Fprint(os.Stdout, renderSurveyTable(rows, "|"))

	tsvPath, txtPath := hostFile+sufSurveyTSV, hostFile+sufSurveyTxt
	for _, f := range []struct{ path, text string }{
		{tsvPath, renderSurveyTable(rows, "\t")},
		{txtPath, renderSurveyTable(rows, "|")},
	} {
		if err := os.WriteFile(f.path, []byte(f.text), 0644); err != nil {
			fmt.Fprintf(os.Stderr, "결과 파일 저장 실패 (%s): %v\n", f.path, err)
		}
	}
	var down []string
	for _, h := range hosts {
		if r, ok := res[h]; !ok || !r.reachable {
			down = append(down, h)
		}
	}
	if len(down) > 0 {
		fmt.Fprintf(os.Stderr, "\n접속불가 %d대 (모든 칸 -): %s\n", len(down), surveyHostSample(down, 10))
	}
	fmt.Fprintf(os.Stderr, "\n결과 파일 (엑셀 붙여넣기용, 탭 구분): %s\n결과 파일 (| 구분): %s\n", tsvPath, txtPath)
}

// ---------------------------------------------------------------- 원격 실행

type surveyOpts struct {
	user        string
	auth        []ssh.AuthMethod
	port        string
	timeout     time.Duration
	concurrency int
	forceConc   int
}

// surveyScript는 명령어 한 줄을 원격 bash 가 표준입력으로 읽을 스크립트로 만든다. 명령어 글자는 그대로이고,
// 중괄호 묶음 + </dev/null 로 감싸 명령어가 표준입력(=스크립트)을 읽어 가지 못하게 한다.
func surveyScript(cmd string) string {
	return "{\n" + cmd + "\n} </dev/null\n"
}

// surveyConnect는 호스트에 SSH 로 접속한다(DNS 선행 조회 → TCP → 핸드셰이크·인증, 접속 설정은 다른 옵션과 같다).
func surveyConnect(host string, o surveyOpts, timeout time.Duration, dnsSem chan struct{}) (*ssh.Client, error) {
	ctx := context.Background()
	if err := lookupHostWithRetry(ctx, dnsSem, host); err != nil {
		return nil, fmt.Errorf("DNS 조회 실패: %v", err)
	}
	target := host
	if net.ParseIP(host) != nil || !strings.Contains(host, ":") {
		target = net.JoinHostPort(host, o.port)
	}
	conn, err := net.DialTimeout("tcp", target, timeout)
	if err != nil {
		return nil, connectFailErr(err)
	}
	conn.SetDeadline(time.Now().Add(timeout)) // -t 는 로그인(핸드셰이크+인증)까지만
	c, chans, reqs, err := ssh.NewClientConn(conn, target, newSSHClientConfig(o.user, o.auth, timeout))
	if err != nil {
		conn.Close()
		return nil, connectFailErr(err)
	}
	conn.SetDeadline(time.Time{})
	return ssh.NewClient(c, chans, reqs), nil
}

// surveyExec는 세션 하나에서 명령어 하나를 실행하고 표준출력을 돌려준다. 종료코드가 0이 아니어도 출력은 결과로 쓴다.
func surveyExec(client *ssh.Client, cmd string) (string, error) {
	sess, err := client.NewSession()
	if err != nil {
		return "", err
	}
	defer sess.Close()
	var out bytes.Buffer
	sess.Stdin = strings.NewReader(surveyScript(cmd))
	sess.Stdout = &out
	err = sess.Run("bash -s")
	if _, isExit := err.(*ssh.ExitError); isExit {
		err = nil
	}
	return out.String(), err
}

// surveyRunHost는 호스트 하나에 접속해 명령어들을 차례로(명령어마다 세션 하나) 실행한다.
func surveyRunHost(host string, cmds []string, o surveyOpts, timeout time.Duration, dnsSem chan struct{}) surveyHost {
	client, err := surveyConnect(host, o, timeout, dnsSem)
	if err != nil {
		return surveyHost{err: err.Error()}
	}
	defer client.Close()
	r := surveyHost{reachable: true, cells: make([]surveyCell, len(cmds))}
	for i, c := range cmds {
		out, err := surveyExec(client, c)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s: 명령 %d번 실행 실패: %v\n", host, i+1, err)
			continue
		}
		r.cells[i] = surveyCell{out: out, ok: true}
	}
	return r
}

// run은 surveyRunFunc 구현이다: 호스트마다 고루틴을 띄우되 동시 접속 수는 기존 제한(-c, /user/ 경로면 450, -cf)을 따른다.
func (o surveyOpts) run(hosts, cmds []string, full bool) map[string]surveyHost {
	conc := o.concurrency
	if o.forceConc > 0 {
		conc = o.forceConc
	} else {
		for _, c := range cmds {
			if isAutofsUserPath(c) && conc > autofsSafeConcurrency {
				conc = autofsSafeConcurrency
				break
			}
		}
	}
	if conc < 1 {
		conc = 1
	}
	res := map[string]surveyHost{}
	var mu sync.Mutex
	dnsSem := make(chan struct{}, dnsLookupConcurrency)

	pass := func(list []string, conc int, timeout time.Duration, progress bool) {
		sem := make(chan struct{}, conc)
		var wg sync.WaitGroup
		var done int64
		stop := make(chan struct{})
		if progress && stderrTTY {
			go func() {
				t := time.NewTicker(time.Second)
				defer t.Stop()
				for {
					select {
					case <-t.C:
						fmt.Fprintf(os.Stderr, "\r%s", progressLabel(atomic.LoadInt64(&done), int64(len(list))))
					case <-stop:
						return
					}
				}
			}()
		}
		for _, h := range list {
			wg.Add(1)
			go func(h string) {
				defer wg.Done()
				sem <- struct{}{}
				r := surveyRunHost(h, cmds, o, timeout, dnsSem)
				<-sem
				mu.Lock()
				res[h] = r
				mu.Unlock()
				atomic.AddInt64(&done, 1)
			}(h)
		}
		wg.Wait()
		close(stop)
		if progress && stderrTTY {
			fmt.Fprintf(os.Stderr, "\r%s\n", progressLabel(int64(len(list)), int64(len(list))))
		}
	}

	pass(hosts, conc, o.timeout, full)

	// 접속불가로 나온 호스트는 전체 실행일 때만 낮은 동시성·짧은 제한시간으로 한 번 더 확인한다(기존 재검증과 같은 방식).
	if full {
		var retry []string
		for _, h := range hosts {
			if !res[h].reachable {
				retry = append(retry, h)
			}
		}
		if len(retry) > 0 {
			rc := conc
			if rc > 50 {
				rc = 50
			}
			rt := o.timeout
			if rt > retryConnectTimeout {
				rt = retryConnectTimeout
			}
			printRetryNotice(len(retry))
			pass(retry, rc, rt, false)
		}
	}
	return res
}

// runSurvey는 -sv 전체를 진행한다(main 에서 호스트 목록을 읽은 뒤 부른다). 종료코드를 돌려준다.
func runSurvey(hosts []string, hostFile string, o surveyOpts) int {
	os.Remove(hostFile + sufSurveyTSV)
	os.Remove(hostFile + sufSurveyTxt)
	start := time.Now()
	cmds, res, join := surveyFlow(hosts, o.run)
	printSurveyDone(cmds, hosts, res, join, hostFile)
	fmt.Fprintf(os.Stderr, "소요 시간: %s\n", fmtDuration(time.Since(start)))
	return 0
}
