package main

// gossh -h <옵션> : 옵션 설명 + 사용법(살펴보는 옵션만 다른 색) + 출력결과 예시를 보여준다.
// ★ 출력결과는 문자열을 직접 적지 않고, 가짜 호스트 결과(svr1~svr10 등)를 실제 출력 함수
// (printCommandResult, printPdshStyle, printBunched, printUnreachableGroup, printSummary,
// writeRPMReport, askSpecial, askRPMRef, confirmDanger, renderHangListLocked 등)에 그대로 넣어서 만든다.
// 출력 형식이 바뀌면 예시도 자동으로 같이 바뀐다. 서버에는 접속하지 않는다.
// 실제 실행에서 표준출력/표준에러로 나뉘는 줄들은 터미널에서 보이는 순서 그대로 표준출력 하나로 모아 찍는다.

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// 사용법 줄에서 살펴보는 옵션(과 그 값)을 나머지와 다른 색으로 보여준다(굵은 자홍).
const colorHelpOpt = "\033[1;35m"

const exampleHostFile = "hostnamefile"

// ★ 예시가 매번 같게 보이도록 소요시간·실행시각은 고정값을 쓴다.
var (
	exampleElapsed = 2*time.Second + 41372915*time.Nanosecond
	exampleStart   = time.Date(2026, 11, 7, 10, 30, 0, 0, time.Local)
)

// 예시용 가짜 결과: svr1~3 은 RHEL8 커널, svr4~10 은 RHEL7 커널, svr11 은 응답 없는 서버.
const (
	kernel8 = "4.18.0-553.el8_10.x86_64"
	kernel7 = "3.10.0-1160.el7.x86_64"
)

var exampleTimeoutErr = errors.New("dial tcp 10.0.0.11:22: i/o timeout")

func isHelpArg(a string) bool {
	switch a {
	case "-h", "-help", "--h", "--help":
		return true
	}
	return false
}

func findOptionDoc(name string) (optionDoc, bool) {
	name = strings.TrimPrefix(strings.TrimPrefix(name, "-"), "-")
	for _, d := range optionDocs {
		if d.flag == name {
			return d, true
		}
	}
	return optionDoc{}, false
}

// runHelpExamples는 "gossh -h <옵션> ..." 을 처리한다. 모르는 옵션이 하나라도 있으면 아무것도 출력하지 않고 1.
func runHelpExamples(args []string) int {
	var docs []optionDoc
	for _, a := range args {
		d, ok := findOptionDoc(a)
		if !ok {
			var names []string
			for _, d := range optionDocs {
				names = append(names, "-"+d.flag)
			}
			fmt.Fprintf(os.Stderr, "알 수 없는 옵션입니다: %s\n", a)
			fmt.Fprintf(os.Stderr, "사용 가능한 옵션: %s\n", strings.Join(names, " "))
			return 1
		}
		docs = append(docs, d)
	}
	color := isCharDevice(os.Stdout)
	for i, d := range docs {
		if i > 0 {
			fmt.Println("\n" + strings.Repeat("=", 60) + "\n")
		}
		fmt.Print(helpExampleText(d, color))
	}
	return 0
}

// helpExampleText는 옵션 하나의 설명·사용법·출력결과를 만든다. color 면 터미널 색상을 넣는다.
func helpExampleText(d optionDoc, color bool) string {
	name := filepath.Base(os.Args[0])
	var b strings.Builder
	b.WriteString(optionLabel(d) + "\n")
	b.WriteString("    " + d.summary + "\n")
	for _, line := range d.detail {
		b.WriteString("    " + line + "\n")
	}
	for _, ex := range helpExamples()[d.flag] {
		b.WriteString("\n사용법:\n")
		b.WriteString("  " + usageLine(name, ex.args, d, color) + "\n")
		if ex.note != "" {
			b.WriteString("  ※ " + ex.note + "\n")
		}
		b.WriteString("\n출력결과:\n")
		b.WriteString(runExample(ex, color))
	}
	return b.String()
}

// usageLine은 예시 명령줄을 만들고, 살펴보는 옵션(값이 있는 옵션은 값까지)만 강조색으로 칠한다.
func usageLine(name string, args []string, d optionDoc, color bool) string {
	parts := []string{name}
	for i := 0; i < len(args); i++ {
		if args[i] != "-"+d.flag {
			parts = append(parts, args[i])
			continue
		}
		tok := args[i]
		if d.arg != "" && i+1 < len(args) {
			i++
			tok += " " + args[i]
		}
		if color {
			tok = colorHelpOpt + tok + colorReset
		}
		parts = append(parts, tok)
	}
	return strings.Join(parts, " ")
}

type helpExample struct {
	args   []string // 사용법 줄의 인자(실행 파일 이름 제외)
	note   string   // 사용법 아래 한 줄 설명(없으면 생략)
	script bool     // -script 처럼 색상 없이 출력되는 예시
	run    func()   // 실제 출력 함수로 예시 출력을 만든다(captureOutput 으로 모음)
}

// runExample은 예시 하나를 실행해 터미널에 보이는 그대로의 출력 문자열을 돌려준다.
func runExample(ex helpExample, color bool) string {
	saveErr, saveOut := colorEnabled, colorOutEnabled
	colorEnabled = color && !ex.script
	colorOutEnabled = color && !ex.script
	defer func() { colorEnabled, colorOutEnabled = saveErr, saveOut }()
	return captureOutput(ex.run)
}

// captureOutput은 fn 이 os.Stdout/os.Stderr 로 찍는 내용을 찍힌 순서대로 모아 돌려준다(파일은 만들지 않음).
func captureOutput(fn func()) string {
	r, w, err := os.Pipe()
	if err != nil {
		return fmt.Sprintf("(예시 출력을 만들 수 없습니다: %v)\n", err)
	}
	done := make(chan []byte)
	go func() {
		b, _ := io.ReadAll(r)
		r.Close()
		done <- b
	}()
	saveOut, saveErr := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = w, w
	func() {
		defer func() { os.Stdout, os.Stderr = saveOut, saveErr }()
		fn()
	}()
	w.Close()
	return string(<-done)
}

// echoLines는 예시용 표준입력이다. Read 한 번에 한 줄만 돌려주고(프롬프트마다 한 줄씩 소비),
// 터미널처럼 입력한 내용을 화면(현재 os.Stderr)에 그대로 보여준다.
type echoLines struct{ lines []string }

func (e *echoLines) Read(p []byte) (int, error) {
	if len(e.lines) == 0 {
		return 0, io.EOF
	}
	n := copy(p, e.lines[0]+"\n")
	fmt.Fprint(os.Stderr, e.lines[0]+"\n")
	e.lines = e.lines[1:]
	return n, nil
}

// withInput은 fn 동안 표준입력 리더(stdinReader)를 예시 입력으로 바꾼다.
func withInput(lines []string, fn func()) {
	save := stdinReader
	stdinReader = bufio.NewReader(&echoLines{lines: lines})
	defer func() { stdinReader = save }()
	fn()
}

// exampleRun은 main 의 실행 흐름(호스트별 결과 → 진행률 줄 → 재검증 → 묶음 출력 → 작업 요약)을
// 가짜 결과로 같은 출력 함수를 불러 재현한다.
type exampleRun struct {
	hosts       []string
	out         map[string]string // 정상 호스트의 원격 표준출력
	unreachable []string          // 응답 없는(Timeout) 호스트
	osInstall   []string          // -pm: OS 설치중
	noSvrAuto   []string          // -pm: autofs 계정 경로 미접근
	bunch       bool
	script      bool
	pm          bool
	concurrency int
	before      func()                                           // 호스트 실행 전 출력(질문·안내 등)
	finish      func(outputs map[string]string, failed []string) // 묶음 출력 대신(-rpm)
}

func (r exampleRun) run() {
	if r.before != nil {
		r.before()
	}
	unreach := map[string]bool{}
	for _, h := range r.unreachable {
		unreach[h] = true
	}
	osInst := map[string]bool{}
	for _, h := range r.osInstall {
		osInst[h] = true
	}
	outputs := map[string]string{}
	var mu sync.Mutex
	success := 0
	for _, h := range r.hosts {
		switch {
		case unreach[h]:
			printPdshStyle(h, "", connectFailErr(exampleTimeoutErr))
		case osInst[h]:
			printOSInstalling(h)
		default:
			printCommandResult(h, r.out[h], "", nil, r.bunch, outputs, &mu)
			success++
		}
	}
	// 터미널에서는 실행이 끝나면 마지막 진행률 줄이 화면에 남는다.
	fmt.Fprintln(os.Stderr, progressLabel(int64(len(r.hosts)), int64(len(r.hosts))))
	if len(r.unreachable) > 0 {
		if !r.script {
			printRetryNotice(len(r.unreachable))
		}
		for _, h := range r.unreachable {
			printPdshStyle(h, "", connectFailErr(exampleTimeoutErr))
		}
	}
	if r.finish != nil {
		r.finish(outputs, r.unreachable)
	} else if r.bunch {
		printBunched(r.hosts, outputs)
		printUnreachableGroup(r.unreachable, nil)
	}
	if r.script {
		return
	}
	conc := r.concurrency
	if conc == 0 {
		conc = 1000
	}
	printSummary(runSummary{
		total: len(r.hosts), concurrency: conc, success: success,
		elapsed: exampleElapsed, pmMode: r.pm,
		osInstallHosts: r.osInstall, noSvrAutoHosts: r.noSvrAuto,
		failedHosts:   r.unreachable,
		osInstallFile: exampleHostFile + sufOSInstall, noSvrAutoFile: exampleHostFile + sufNoSvrAuto,
		offFile: exampleHostFile + sufResOff, refusedFile: exampleHostFile + sufResRefused,
		cancelFile: exampleHostFile + sufResCancel,
	})
}

// exampleHosts는 hostnamefile 에 "svr[1-n]" 한 줄이 있을 때와 같은 호스트 목록이다.
func exampleHosts(n int) []string {
	return expandHostLine(fmt.Sprintf("svr[1-%d]", n))
}

// sameOutput은 모든 호스트가 같은 결과(text)를 내는 가짜 결과다.
func sameOutput(hosts []string, text string) map[string]string {
	m := map[string]string{}
	for _, h := range hosts {
		m[h] = text
	}
	return m
}

// kernelOutput은 "uname -r" 가짜 결과다(svr1~3 RHEL8, 나머지 RHEL7).
func kernelOutput(hosts []string) map[string]string {
	m := map[string]string{}
	for i, h := range hosts {
		if i < 3 {
			m[h] = kernel8
		} else {
			m[h] = kernel7
		}
	}
	return m
}

// unameRun은 "uname -r" 을 svr1~10 에 실행한 기본 출력 예시다.
func unameRun() exampleRun {
	hosts := exampleHosts(10)
	return exampleRun{hosts: hosts, out: kernelOutput(hosts)}
}

// unameRunWithDown은 svr11(응답 없음)까지 넣은 예시다.
func unameRunWithDown(bunch bool) exampleRun {
	hosts := exampleHosts(11)
	return exampleRun{hosts: hosts, out: kernelOutput(hosts), unreachable: []string{"svr11"}, bunch: bunch}
}

const (
	sameAsDefault = "출력 형식은 옵션 없이 실행할 때와 같습니다."
	uname         = `"uname -r"`
)

// helpExamples는 optionDocs 의 옵션마다 사용법·출력 예시를 정의한다.
func helpExamples() map[string][]helpExample {
	hosts10 := exampleHosts(10)
	limitsSHA := func() string {
		sum := sha256.Sum256([]byte("*  soft  nofile  65536\n*  hard  nofile  65536\n"))
		return hex.EncodeToString(sum[:])[:12]
	}()
	tmpLine := "[tmp] q /tmp 1777 root root 24h"
	limitLine := "[limit] 완료: 비교서버와 동일 (sha256 " + limitsSHA + ")"

	return map[string][]helpExample{
		"w": {{
			args: []string{"-w", exampleHostFile, uname},
			note: exampleHostFile + " 내용이 \"svr[1-10]\" 한 줄이면 svr1 ~ svr10 10대로 펼쳐집니다.",
			run:  unameRun().run,
		}},
		"u": {{
			args: []string{"-w", exampleHostFile, "-u", "admin", uname},
			note: "admin 계정으로 접속합니다. " + sameAsDefault,
			run:  unameRun().run,
		}},
		"p": {{
			args: []string{"-w", exampleHostFile, "-p", "'비밀번호'", uname},
			note: "키로 접속되지 않는 호스트에만 비밀번호를 씁니다. " + sameAsDefault,
			run:  unameRun().run,
		}},
		"i": {
			{
				args: []string{"-i", "svr1"},
				note: "svr1 에 대화형으로 접속합니다. 아래 [root@svr1 ~]# 줄들은 원격 셸 화면 예시이고, 마지막 줄은 exit 후 gossh 가 출력합니다.",
				run: func() {
					fmt.Fprintln(os.Stdout, "[root@svr1 ~]# uname -r")
					fmt.Fprintln(os.Stdout, kernel8)
					fmt.Fprintln(os.Stdout, "[root@svr1 ~]# exit")
					fmt.Fprintln(os.Stdout, "logout")
					fmt.Fprintf(os.Stderr, connClosedFmt, "svr1")
				},
			},
			{
				args: []string{"-w", exampleHostFile, "-i", "~/.ssh/id_rsa", uname},
				note: "-w 와 같이 쓰면 키 파일 경로입니다. " + sameAsDefault,
				run:  unameRun().run,
			},
		},
		"P": {{
			args: []string{"-w", exampleHostFile, "-P", "2222", uname},
			note: "2222 포트로 접속합니다. " + sameAsDefault,
			run:  unameRun().run,
		}},
		"c": {
			{
				args: []string{"-w", exampleHostFile, "-c", "200", uname},
				note: "200대씩 동시에 접속합니다. 작업 요약의 \"동시 접속 수\" 만 달라집니다.",
				run: func() {
					r := unameRun()
					r.concurrency = 200
					r.run()
				},
			},
			{
				args: []string{"-w", exampleHostFile, "-c", "1000", `"ls -d /user/kdh"`},
				note: "명령에 /user/ 경로가 있으면 -c 값과 관계없이 자동 제한됩니다.",
				run: func() {
					cmd := "ls -d /user/kdh"
					r := exampleRun{hosts: hosts10, out: sameOutput(hosts10, "/user/kdh"), concurrency: 1000}
					if isAutofsUserPath(cmd) {
						r.concurrency = autofsSafeConcurrency
						r.before = func() { printAutofsLimitNotice(1000) }
					}
					r.run()
				},
			},
		},
		"cf": {{
			args: []string{"-w", exampleHostFile, "-cf", "1000", `"ls -d /user/kdh"`},
			note: "/user/ 경로가 있어도 자동 제한 안내 없이 1000대씩 실행합니다.",
			run: func() {
				exampleRun{hosts: hosts10, out: sameOutput(hosts10, "/user/kdh"), concurrency: 1000}.run()
			},
		}},
		"t": {{
			args: []string{"-w", exampleHostFile, "-t", "5", uname},
			note: "svr11 은 응답 없는 서버 예시입니다(5초 안에 로그인 못 하면 접속불가, 끝난 뒤 한 번 재검증).",
			run:  unameRunWithDown(false).run,
		}},
		"b": {{
			args: []string{"-w", exampleHostFile, "-b", uname},
			note: "svr11 은 응답 없는 서버 예시입니다.",
			run:  unameRunWithDown(true).run,
		}},
		"m": {{
			args: []string{"-w", exampleHostFile, "-m", `"yum -y update"`},
			note: "터미널에서 실행한 경우에만 동작합니다. ※ 줄은 화면 설명입니다.",
			run:  monitorExample,
		}},
		"pm": {{
			args: []string{"-w", exampleHostFile, "-pm", uname},
			note: "svr4 는 OS 설치중, svr9·svr10 은 특정 autofs 계정 경로 미접근인 예시입니다.",
			run: func() {
				r := unameRun()
				r.pm = true
				r.osInstall = []string{"svr4"}
				r.noSvrAuto = []string{"svr9", "svr10"}
				r.run()
			},
		}},
		"script": {{
			args:   []string{"-w", exampleHostFile, "-script", uname},
			note:   "작업 요약·색상 없이 결과만 나옵니다(맨 아래 진행률 줄은 터미널에서만 보이고 저장되지 않음).",
			script: true,
			run: func() {
				r := unameRun()
				r.script = true
				r.run()
			},
		}},
		"dnlgjawkrdjqghkrdls": {
			{
				args: []string{"-w", exampleHostFile, `"reboot"`},
				note: "위험 명령을 이 옵션 없이 실행하면 거부합니다(종료코드 1).",
				run: func() {
					if isDangerousCommand("reboot") {
						printDangerBlocked("reboot")
					}
				},
			},
			{
				args: []string{"-w", exampleHostFile, "-dnlgjawkrdjqghkrdls", `"reboot"`},
				note: "대상 호스트를 다시 입력(svr[1-10], 빈 줄로 끝)하고 y 를 입력한 예시입니다.",
				run: func() {
					r := exampleRun{hosts: hosts10, out: map[string]string{}}
					r.before = func() {
						withInput([]string{"svr[1-10]", "", "y"}, func() { confirmDanger("reboot", hosts10) })
					}
					r.run()
				},
			},
		},
		"tmp": {{
			args: []string{"-w", exampleHostFile, "-tmp"},
			note: "질문에 24 를 입력한 예시입니다.",
			run: func() {
				exampleRun{hosts: hosts10, out: sameOutput(hosts10, tmpLine), bunch: true,
					before: func() { withInput([]string{"24"}, func() { askSpecial(true, false) }) }}.run()
			},
		}},
		"limit": {{
			args: []string{"-w", exampleHostFile, "-limit"},
			note: "비교서버에 ref01, 복사 진행에 y 를 입력한 예시입니다.",
			run: func() {
				exampleRun{hosts: hosts10, out: sameOutput(hosts10, limitLine), bunch: true,
					before: func() { withInput([]string{"ref01", "y"}, func() { askSpecial(false, true) }) }}.run()
			},
		}},
		"rpm": {{
			args: []string{"-w", exampleHostFile, "-rpm"},
			note: "비교서버에 ref01 을 입력한 예시입니다(svr11 은 응답 없는 서버). 화면 출력 뒤에 보고서 파일 내용을 붙였습니다.",
			run:  rpmExample,
		}},
		"tl": {{
			args: []string{"-w", exampleHostFile, "-tl"},
			note: "설정값 24, 비교서버 ref01, 복사 진행 y 를 입력한 예시입니다.",
			run: func() {
				exampleRun{hosts: hosts10, out: sameOutput(hosts10, tmpLine+"\n"+limitLine), bunch: true,
					before: func() { withInput([]string{"24", "ref01", "y"}, func() { askSpecial(true, true) }) }}.run()
			},
		}},
	}
}

// monitorExample은 -m 예시다: 실행 중 진행률 줄 → Enter 로 본 행리스트 화면 → 끝난 뒤 화면.
func monitorExample() {
	saveOn, saveTimes, saveStart, saveText := monitorOn, hostTimes, monitorStart, progressText
	defer func() { monitorOn, hostTimes, monitorStart, progressText = saveOn, saveTimes, saveStart, saveText }()

	now := time.Now()
	monitorOn = true
	monitorStart = now.Add(-130 * time.Second)
	hostTimes = map[string]*hostTiming{
		"svr3": {start: now.Add(-125 * time.Second)},
		"svr7": {start: now.Add(-75 * time.Second)},
		"svr5": {start: now.Add(-200 * time.Second), end: now.Add(-110 * time.Second)},
	}
	progressText = progressLabel(8, 10)
	r := unameRun()
	r.before = func() {
		fmt.Fprintln(os.Stderr, "※ 실행 중 화면 맨 아래 진행률 줄:")
		fmt.Fprintln(os.Stderr, progressText)
		fmt.Fprintln(os.Stderr, "※ Enter 를 누르면 보이는 행리스트 화면 (다시 Enter 로 원래 화면):")
		list := captureOutput(func() {
			outMu.Lock()
			renderHangListLocked()
			outMu.Unlock()
		})
		fmt.Fprintln(os.Stderr, stripANSI(list))
		fmt.Fprintln(os.Stderr, "※ 작업이 끝난 뒤 화면:")
		hostTimes = map[string]*hostTiming{} // 끝나면 실행 중인 호스트가 없다
	}
	r.run()
}

// rpmExample은 -rpm 예시다. 보고서 파일은 임시 폴더에 만들고 내용을 보여준 뒤 지운다(현재 폴더에 남기지 않음).
func rpmExample() {
	hosts := exampleHosts(11)
	refPkgs := []string{"bash.x86_64", "glibc.i686", "glibc.x86_64", "kernel.x86_64", "nfs-utils.x86_64", "vim-enhanced.x86_64"}
	ref := parsePkgSet(strings.Join(refPkgs, "\n"))
	out := map[string]string{}
	for _, h := range hosts {
		pkgs := refPkgs
		switch h {
		case "svr8", "svr9":
			pkgs = []string{"bash.x86_64", "glibc.i686", "glibc.x86_64", "kernel.x86_64", "vim-enhanced.x86_64"}
		case "svr10":
			pkgs = append(append([]string{}, refPkgs...), "telnet.x86_64")
		}
		out[h] = strings.Join(pkgs, "\n")
	}

	dir, err := os.MkdirTemp("", "gossh-help-")
	if err != nil {
		fmt.Fprintf(os.Stderr, "(예시용 임시 폴더를 만들 수 없습니다: %v)\n", err)
		return
	}
	defer os.RemoveAll(dir)
	cwd, err := os.Getwd()
	if err != nil || os.Chdir(dir) != nil {
		fmt.Fprintln(os.Stderr, "(예시용 임시 폴더로 이동할 수 없습니다)")
		return
	}
	defer os.Chdir(cwd)

	report := exampleHostFile + sufRPMReport
	exampleRun{
		hosts: hosts, out: out, unreachable: []string{"svr11"},
		bunch: true, // 실제와 같이 -rpm 은 결과를 모아 비교한다
		before: func() {
			withInput([]string{"ref01"}, func() { askRPMRef() })
		},
		finish: func(outputs map[string]string, failed []string) {
			writeRPMReport("ref01", ref, hosts, outputs, failed, report,
				exampleHostFile+sufRPMTab, exampleHostFile+sufRPMDiff, exampleStart)
		},
	}.run()
	if data, err := os.ReadFile(report); err == nil {
		fmt.Fprintf(os.Stderr, "\n※ %s 내용:\n%s", report, data)
	}
}
