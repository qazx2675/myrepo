// hpcbot — 서버 운영 매뉴얼 챗봇 (REPL + 한 줄 실행)
package main

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
)

const sepLine = "─────────────────────────────────────────────"

// 한 줄 실행 종료 코드
const (
	exitAnswer     = 0
	exitNoMatch    = 1
	exitCandidates = 2
	exitError      = 3
)

type app struct {
	d       *Data
	m       *Matcher
	ql      *QLog
	out     io.Writer
	errOut  io.Writer
	verbose bool
	jevKey  string // 비어 있으면 --jev 꺼짐
	jevURL  string // 테스트용 (비면 기본 엔드포인트)

	pending     []Hit // REPL 후보 선택 대기 목록
	pendingCode string
	pendingIn   string
}

func (a *app) logWrite(e LogEntry) {
	if err := a.ql.Write(e); err != nil {
		fmt.Fprintln(a.errOut, "[경고] 로그 기록 실패:", err)
	}
}

// label 은 블록 표기 (예: "§6.1 GPU 드라이버 설치").
func (a *app) label(id string) string { return a.d.Blocks[id].Label }

func (a *app) printAnswer(id string, others []Hit, code string, jev bool) string {
	text := RenderBlock(a.d.Blocks[id])
	if jev {
		text = strings.Replace(text, "]", "] [Jev]", 1)
	}
	fmt.Fprintln(a.out, sepLine)
	fmt.Fprintln(a.out, text)
	fmt.Fprintln(a.out, sepLine)
	var names []string
	for _, h := range others {
		if h.ID != id && h.Score > 0 && len(names) < 2 {
			names = append(names, a.label(h.ID))
		}
	}
	if len(names) > 0 {
		fmt.Fprintln(a.out, "다른 후보: "+strings.Join(names, " / "))
	}
	fmt.Fprintln(a.out, "code "+code)
	return text
}

func (a *app) logEntry(v Verdict, code, mode, id, answer string, jev bool) LogEntry {
	e := LogEntry{Code: code, Input: v.Input, Converted: v.Converted, Mode: mode, ID: id,
		Top3: v.Top, Answer: answer, Jev: jev}
	if id != "" {
		e.Section, e.Title = splitLabel(a.label(id))
	}
	return e
}

// handle 은 질문 1건을 처리하고 종료 코드(0/2/1)를 돌려준다.
// interactive 이면 후보 모드에서 번호 선택 대기 상태를 남긴다.
func (a *app) handle(input string, interactive bool) int {
	v := a.m.Query(input)
	jev := false
	if a.jevKey != "" && v.Mode != ModeAnswer {
		if id, p, err := JevPick(a.d, a.jevKey, v.Input, a.jevURL); err != nil {
			fmt.Fprintln(a.errOut, "[경고] --jev 호출 실패:", err)
		} else if p >= jevMinChoice {
			top := []Hit{{ID: id, Score: p}}
			for _, h := range v.Top {
				if h.ID != id {
					top = append(top, h)
				}
			}
			v.Mode, v.Top, jev = ModeAnswer, top, true
		}
	}
	code := a.ql.NewCode()
	if v.Converted != "" {
		if v.Auto {
			fmt.Fprintln(a.out, "[자동 변환] "+v.Converted)
		} else {
			fmt.Fprintln(a.out, "[변환] "+v.Converted)
		}
	}
	exit := exitNoMatch
	a.pending = nil
	switch v.Mode {
	case ModeAnswer:
		id := v.Top[0].ID
		text := a.printAnswer(id, v.Top[1:], code, jev)
		a.logWrite(a.logEntry(v, code, ModeAnswer, id, text, jev))
		exit = exitAnswer
	case ModeCandidates:
		var sb strings.Builder
		fmt.Fprintf(&sb, "여러 항목이 비슷합니다. 번호를 입력하세요. (code %s)", code)
		for i, h := range v.Cands {
			fmt.Fprintf(&sb, "\n %d) %s", i+1, a.label(h.ID))
		}
		text := sb.String()
		fmt.Fprintln(a.out, text)
		a.logWrite(a.logEntry(v, code, ModeCandidates, "", text, false))
		exit = exitCandidates
		if interactive {
			a.pending, a.pendingCode, a.pendingIn = v.Cands, code, v.Input
		}
	default:
		var sb strings.Builder
		sb.WriteString("매뉴얼에서 찾지 못했습니다.")
		if len(v.Top) > 0 {
			sb.WriteString(" 비슷한 항목:")
			for _, h := range v.Top {
				sb.WriteString("\n - " + a.label(h.ID))
			}
		}
		sb.WriteString("\ncode " + code)
		text := sb.String()
		fmt.Fprintln(a.out, text)
		a.logWrite(a.logEntry(v, code, ModeNoMatch, "", text, false))
	}
	if a.verbose {
		for _, h := range v.Top {
			fmt.Fprintf(a.out, "  [점수] %s %.3f\n", h.ID, h.Score)
		}
	}
	return exit
}

// choose 는 후보 목록에서 번호로 고른 블록을 답변 모드로 출력한다 (같은 code).
func (a *app) choose(n int) {
	h := a.pending[n-1]
	text := a.printAnswer(h.ID, a.pending, a.pendingCode, false)
	e := a.logEntry(Verdict{Input: a.pendingIn, Top: []Hit{h}}, a.pendingCode, "choice", h.ID, text, false)
	e.Input = fmt.Sprintf("%s → %d", a.pendingIn, n)
	a.logWrite(e)
	a.pending = nil
}

func (a *app) repl(in io.Reader) {
	fmt.Fprintln(a.out, "=====================================================")
	fmt.Fprintln(a.out, "    서버 운영 및 관리 챗봇 (HPC/업무 프로세스 가이드)   ")
	fmt.Fprintln(a.out, "=====================================================")
	fmt.Fprintln(a.out, "안내: 영타로 입력된 한글은 자동 변환됩니다. 강제로 변환하려면 앞에 '/h '를 붙여주세요.")
	fmt.Fprintln(a.out, "예시: /h xptmxm -> 테스트")
	fmt.Fprintln(a.out, "종료하려면 'exit' 또는 'quit'를 입력하세요.")
	fmt.Fprintln(a.out, "=====================================================")
	sc := bufio.NewScanner(in)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for {
		if len(a.pending) > 0 {
			fmt.Fprint(a.out, "번호 > ")
		} else {
			fmt.Fprint(a.out, "\n질문 입력 > ")
		}
		if !sc.Scan() {
			fmt.Fprintln(a.out)
			return
		}
		input := strings.TrimSpace(sc.Text())
		switch {
		case input == "exit" || input == "quit":
			fmt.Fprintln(a.out, "챗봇을 종료합니다.")
			return
		case input == "":
			continue
		}
		if n, err := strconv.Atoi(input); err == nil && len(a.pending) > 0 {
			if n >= 1 && n <= len(a.pending) {
				a.choose(n)
			} else {
				fmt.Fprintf(a.out, "1~%d 중에서 입력하세요.\n", len(a.pending))
			}
			continue
		}
		a.handle(input, true)
	}
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

func run(args []string, in io.Reader, out, errOut io.Writer) int {
	fs := flag.NewFlagSet("hpcbot", flag.ContinueOnError)
	fs.SetOutput(errOut)
	useJev := fs.Bool("jev", false, "로컬 판정이 애매할 때 Jev API 를 한 번 호출")
	verbose := fs.Bool("v", false, "데이터 출처와 상위 3개 점수 표시")
	if err := fs.Parse(args); err != nil {
		return exitError
	}
	d, err := LoadData()
	if err != nil {
		fmt.Fprintln(errOut, "오류:", err)
		return exitError
	}
	a := &app{d: d, m: NewMatcher(d), ql: NewQLog(), out: out, errOut: errOut, verbose: *verbose}
	if *verbose {
		fmt.Fprintln(out, d.SourceReport())
	}
	if *useJev {
		if a.jevKey = LoadJevKey(); a.jevKey == "" {
			fmt.Fprintln(errOut, "[안내] Jev 키를 찾지 못해 --jev 를 무시합니다 (JEV_API_KEY, ~/.jev-claude.env, ~/.jev-router.env).")
		}
	}
	if q := strings.TrimSpace(strings.Join(fs.Args(), " ")); q != "" {
		return a.handle(q, false)
	}
	a.repl(in)
	return exitAnswer
}
