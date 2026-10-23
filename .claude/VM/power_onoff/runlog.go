package main

import (
	"fmt"
	"io"
	"sync"
	"time"
)

// RunLog 는 작업 진행 로그다. 화면(w)에 쓰고, tee(감사 로그)가 있으면 같은 내용을 시각과 함께 남긴다.
// 질문(c/s/q 등)을 하는 동안에는 Pause 로 다른 BM 의 진행 로그를 모아 두었다가 Resume 에 한꺼번에 낸다.
type RunLog struct {
	mu     sync.Mutex
	w      io.Writer
	tee    io.Writer
	paused int
	buf    []string
}

func NewRunLog(w, tee io.Writer) *RunLog { return &RunLog{w: w, tee: tee} }

// Logf 는 `[vm] 메시지` 한 줄을 낸다. vm 이 비면 접두어 없이 낸다.
func (l *RunLog) Logf(vm, format string, a ...any) {
	msg := fmt.Sprintf(format, a...)
	line := msg
	if vm != "" {
		line = "[" + vm + "] " + msg
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.auditLocked(line)
	if l.paused > 0 {
		l.buf = append(l.buf, line)
		return
	}
	fmt.Fprintln(l.w, line)
}

// Raw 는 접두어·감사 기록 없이 화면에 그대로 쓴다(표, 질문 안내 등).
func (l *RunLog) Raw(format string, a ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	fmt.Fprintf(l.w, format, a...)
}

// Audit 는 화면에는 내지 않고 감사 로그에만 남긴다.
func (l *RunLog) Audit(format string, a ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.auditLocked(fmt.Sprintf(format, a...))
}

func (l *RunLog) auditLocked(line string) {
	if l.tee != nil {
		fmt.Fprintf(l.tee, "%s %s\n", time.Now().Format("2006-01-02 15:04:05"), line)
	}
}

func (l *RunLog) Pause() {
	l.mu.Lock()
	l.paused++
	l.mu.Unlock()
}

func (l *RunLog) Resume() {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.paused > 0 {
		l.paused--
	}
	if l.paused == 0 {
		for _, s := range l.buf {
			fmt.Fprintln(l.w, s)
		}
		l.buf = nil
	}
}

// Result / 결과 요약 -------------------------------------------------------

const (
	StOK   = "성공"
	StSkip = "스킵"
	StFail = "실패"
)

// Result 는 VM 한 대의 한 단계 결과다. 재부팅은 Off/On 두 줄이 나온다.
type Result struct {
	VM      string
	Op      string // "Off" / "On"
	Status  string // StOK / StSkip / StFail
	Elapsed time.Duration
	Note    string
}

type resultSet struct {
	mu    sync.Mutex
	op    string
	order []string
	m     map[string]Result
}

func newResultSet(op string, vms []*VMInfo) *resultSet {
	s := &resultSet{op: op, m: map[string]Result{}}
	for _, v := range vms {
		s.order = append(s.order, v.Name)
	}
	return s
}

func (s *resultSet) set(vm, status, note string, el time.Duration) {
	s.mu.Lock()
	s.m[vm] = Result{VM: vm, Op: s.op, Status: status, Elapsed: el, Note: note}
	s.mu.Unlock()
}

func (s *resultSet) has(vm string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.m[vm]
	return ok
}

// list 는 입력 순서대로 돌려준다. 결과가 없는 VM(중단으로 실행 못 함)은 실패로 채운다.
func (s *resultSet) list(missingNote string) []Result {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Result, 0, len(s.order))
	for _, n := range s.order {
		r, ok := s.m[n]
		if !ok {
			r = Result{VM: n, Op: s.op, Status: StFail, Note: missingNote}
		}
		out = append(out, r)
	}
	return out
}

func fmtElapsed(d time.Duration) string {
	if d <= 0 {
		return "-"
	}
	s := int(d.Round(time.Second) / time.Second)
	return fmt.Sprintf("%02d:%02d", s/60, s%60)
}

// PrintSummary 는 결과 표를 출력하고 실패 건수를 돌려준다.
func PrintSummary(w io.Writer, results []Result, color bool) (failed int) {
	paint := func(code, s string) string {
		if !color {
			return s
		}
		return "\x1b[" + code + "m" + s + "\x1b[0m"
	}
	fmt.Fprintf(w, "\n%-20s %-4s %-6s %-6s %s\n", "VM", "작업", "결과", "소요", "비고")
	var ok, skip int
	for _, r := range results {
		st := r.Status
		switch r.Status {
		case StOK:
			ok++
			st = paint("32", st)
		case StSkip:
			skip++
			st = paint("33", st)
		default:
			failed++
			st = paint("1;31", st)
		}
		pad := ""
		if n := 6 - 2*len([]rune(r.Status)); n > 0 { // 한글 2칸 폭 보정
			pad = fmt.Sprintf("%*s", n, "")
		}
		fmt.Fprintf(w, "%-20s %-4s %s%s %-6s %s\n", r.VM, r.Op, st, pad, fmtElapsed(r.Elapsed), r.Note)
	}
	fmt.Fprintf(w, "\n합계 %d건: 성공 %d / 스킵 %d / 실패 %d\n", len(results), ok, skip, failed)
	return failed
}

func (s *resultSet) get(vm string) Result {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.m[vm]
}
