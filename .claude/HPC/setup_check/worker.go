package main

// -run 백그라운드 워커: 판정 → 설정수정 → 부하(CPU + stress-ng) → stress-ng → 완료 상태기계.
// 단계가 끝날 때마다 재체크(판정)하고 state.json 에 질문을 써 둔 뒤 answer.json 으로 답(c/r/x/s)을 받는다.
// 외부 동작은 executor 로만 하므로 시험에서는 가짜 실행기와 가짜 시각으로 전체 흐름을 돌린다.

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// btimeTolerance 는 btime 이 이만큼 넘게 바뀌어야 재부팅으로 본다(/proc/stat btime 은 읽을 때마다 1초쯤 흔들릴 수 있음).
const btimeTolerance = 60

// heartbeatEvery 는 워커가 외부 명령 중에도 state.json 의 heartbeat 를 갱신하는 간격.
const heartbeatEvery = 3 * time.Second

// answerPoll 은 answer.json 을 확인하는 간격.
const answerPoll = time.Second

// stopPoll 은 stop.req(중지 요청)를 확인하는 간격(외부 명령 실행 중에도 본다).
var stopPoll = time.Second

type worker struct {
	dir   string
	ex    executor
	conf  config
	notes map[string]string // 장애 목록(호스트 소문자 → 사유)
	log   io.Writer         // 워커 진행 로그

	mu sync.Mutex // st 쓰기·저장 보호(heartbeat 고루틴과 공유)
	st *runState

	qmu   sync.Mutex // 단계 중간(인벤토리 사용 중) 질문을 한 번에 하나만 띄우려고(CPU·stress-ng 프로세스가 동시에 걸릴 수 있음)
	logMu sync.Mutex // log 쓰기 보호(인벤토리 질문 고루틴도 로그를 쓴다)

	reload func() // 단계 시작(재시도 포함)마다 설정 파일을 다시 읽는다(실행 중 고친 값 반영). nil 이면 안 함

	info []string // 이번 단계에서 질문 문구에 덧붙일 안내(stress-test 종료코드 1 등)

	yamlQ  string // -auto: 답을 기다리는 'yaml 바뀜' 질문의 id(없으면 "")
	yamlOK bool   // -auto: 그 질문에 y 를 답함 — 다음 launch 한 번은 yaml 확인 없이 진행(launch 가 우리 yaml 을 다시 등록한다)

	stopCh   chan struct{} // 중지 요청을 받으면 닫힘(실행 중인 외부 명령 취소에도 쓰인다)
	stopOnce sync.Once
}

func newWorker(dir string, st *runState, ex executor, conf config, notes map[string]string, log io.Writer) *worker {
	if notes == nil {
		notes = map[string]string{}
	}
	return &worker{dir: dir, st: st, ex: ex, conf: conf, notes: notes, log: log, stopCh: make(chan struct{})}
}

// stopped 는 중지 요청이 들어왔는지(stop.req 가 있으면 stopCh 를 닫아 실행 중인 명령도 취소시킨다).
func (w *worker) stopped() bool {
	select {
	case <-w.stopCh:
		return true
	default:
	}
	if !stopRequested(w.dir) {
		return false
	}
	w.stopOnce.Do(func() {
		w.logf("중지 요청(stop.req)을 받았습니다")
		close(w.stopCh)
	})
	return true
}

// watchStop 은 done 이 닫힐 때까지 stopPoll 마다 중지 요청을 확인한다(외부 명령 중에도 취소하려고).
func (w *worker) watchStop(done <-chan struct{}) {
	t := time.NewTicker(stopPoll)
	defer t.Stop()
	for {
		select {
		case <-done:
			return
		case <-t.C:
			if w.stopped() {
				return
			}
		}
	}
}

// checkPass 는 설정체크 열 값 c 가 통과인지: O, 또는 설정체크를 안 하는 설정(OS_CHECK_SH 비어 있음, off)의 '-'.
// ★ OS_CHECK_SH 가 있는데 '-' 이면 judge 중 config_check 실행이 실패한 것이므로 통과가 아니다.
func checkPass(off bool, c string) bool { return c == "O" || (off && c == "-") }

func (w *worker) checkOK(c string) bool { return checkPass(w.conf.OSCheckSh == "", c) }

func (w *worker) logf(format string, a ...interface{}) {
	w.logMu.Lock()
	defer w.logMu.Unlock()
	fmt.Fprintf(w.log, "%s %s\n", w.ex.Now().Format("2006-01-02 15:04:05"), fmt.Sprintf(format, a...))
}

// update 는 상태를 바꾸고(fn) state.json 에 저장한다. 상태 변경은 모두 이 함수를 거친다.
func (w *worker) update(fn func(s *runState)) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if fn != nil {
		fn(w.st)
	}
	w.st.Updated = w.ex.Now()
	w.st.Heartbeat = time.Now()
	if err := saveState(w.dir, w.st); err != nil {
		fmt.Fprintf(w.log, "경고: 상태 저장 실패: %v\n", err)
	}
}

// heartbeat 는 stop 이 닫힐 때까지 몇 초마다 heartbeat 를 갱신한다.
func (w *worker) heartbeat(stop <-chan struct{}) {
	t := time.NewTicker(heartbeatEvery)
	defer t.Stop()
	for {
		select {
		case <-stop:
			return
		case <-t.C:
			w.mu.Lock()
			w.st.Heartbeat = time.Now()
			_ = saveState(w.dir, w.st)
			w.mu.Unlock()
		}
	}
}

// ---------------------------------------------------------------------------
// 대상 선정
// ---------------------------------------------------------------------------

// active 는 작업 대상이 될 수 있는 행(상태 완료, 제외 아님, -auto 에서 포기 아님).
func active(r *hostRow) bool { return r.Status == stDone && !r.Excluded && r.GaveUp == "" }

func (w *worker) pick(f func(r *hostRow) bool) []string {
	var out []string
	for i := range w.st.Hosts {
		if r := &w.st.Hosts[i]; active(r) && f(r) {
			out = append(out, r.Host)
		}
	}
	return out
}

// fixTargets: 설정체크 X, 출력없음, 또는 OS_CHECK_SH 가 있는데 '-'(체크 실행 실패).
func (w *worker) fixTargets() []string {
	return w.pick(func(r *hostRow) bool { return r.Check == "X" || r.Check == noOutput || !w.checkOK(r.Check) })
}

// cpuRetryMaxLoad 는 CPU X 호스트의 1분 부하가 이 값 미만이어야 "이전 CPU 테스트가 멈춘 것"으로 보고 재실행 후보로 삼는다.
const cpuRetryMaxLoad = 3.0

// cpuTargets: 설정체크 O(또는 안 함 '-')이고 CPU NOTRUN, 또는 사용자가 재실행을 고른(CPURetry) 호스트.
func (w *worker) cpuTargets() []string {
	return w.pick(func(r *hostRow) bool { return w.checkOK(r.Check) && (r.CPU == cpuNotRun || r.CPURetry) })
}

// xCandidates: 설정체크 O(또는 안 함)이고 CPU X 인데 아직 재실행으로 고르지 않은 호스트(부하 확인 전 후보).
func (w *worker) xCandidates() []string {
	return w.pick(func(r *hostRow) bool { return w.checkOK(r.Check) && r.CPU == "X" && !r.CPURetry })
}

// stressTargets: 설정체크 O, CPU O, stress-ng O 아님, 이번 CPU 대상 아님.
func (w *worker) stressTargets(cpu []string) []string {
	skip := toSet(cpu)
	return w.pick(func(r *hostRow) bool {
		return w.checkOK(r.Check) && r.CPU == "O" && r.Stress != "O" && !skip[r.Host]
	})
}

// ngTargets: 부하 단계에서 CPU 를 돌린(또는 stress-ng 를 건너뛴) 호스트 중 CPU O 가 됐고 stress-ng O 아님.
func (w *worker) ngTargets() []string {
	skipped := toSet(w.st.NGSkip) // 부하 단계에서 stress-ng 만 건너뛴 호스트도 다시 대상
	return w.pick(func(r *hostRow) bool { return (r.CPUTarget || skipped[r.Host]) && r.CPU == "O" && r.Stress != "O" })
}

// notOK 는 그 단계 기준으로 O 가 아닌(x 로 제외할) 호스트.
func (w *worker) notOK(stage int) []string {
	cpu, ng := toSet(w.st.CPUList), toSet(w.st.StressList)
	return w.pick(func(r *hostRow) bool {
		switch stage {
		case stageJudge, stageFix:
			return !w.checkOK(r.Check)
		case stageLoad:
			return (cpu[r.Host] && r.CPU != "O") || (ng[r.Host] && r.Stress != "O")
		case stageNG:
			return ng[r.Host] && r.Stress != "O"
		}
		return false
	})
}

// nextStage 는 from 다음으로 할 단계(대상이 없는 단계는 건너뜀, 없으면 완료).
func (w *worker) nextStage(from int) int {
	for n := from + 1; n < stageFinish; n++ {
		switch n {
		case stageFix:
			if len(w.fixTargets()) > 0 {
				return n
			}
		case stageLoad:
			cpu := w.cpuTargets()
			if len(cpu) > 0 || len(w.stressTargets(cpu)) > 0 || len(w.xCandidates()) > 0 {
				return n
			}
		case stageNG:
			if len(w.ngTargets()) > 0 {
				return n
			}
		}
	}
	return stageFinish
}

// ---------------------------------------------------------------------------
// 상태기계
// ---------------------------------------------------------------------------

// run 은 완료 또는 중지될 때까지 단계를 진행한다.
// ★ 재기동(상태에 진행 중 표시나 질문이 남아 있음)이면 진행 중이던 외부 명령을 다시 돌리지 않고 재체크 후 질문부터 한다.
// 단, 부하 단계에서 CPU launch 시각이 기록돼 있고 감시가 안 끝났으면 다시 launch 하지 않고 감시를 이어 간다.
func (w *worker) run() {
	resume := w.st.Busy || w.st.Question != nil
	w.update(func(s *runState) {
		if s.Stage < stageJudge {
			s.Stage = stageJudge
		}
		s.Question = nil
		s.LastError = ""
		s.CheckOff = w.conf.OSCheckSh == ""
		if resume {
			for i := range s.Hosts {
				s.Hosts[i].Running = ""
			}
		}
	})
	// 중지 요청은 외부 명령 중에도 보고 취소한다
	done := make(chan struct{})
	defer close(done)
	go w.watchStop(done)
	if resume {
		w.logf("재기동: %s 단계 — 진행 중이던 명령은 다시 실행하지 않고 재체크부터 합니다", stageName(w.st.Stage))
	}
	if w.st.Auto != nil { // -auto: 단계·질문 대신 스케줄러(auto.go)
		w.runAuto()
		w.logf("워커 종료: %s", w.st.Detail)
		return
	}
	for !w.st.Finished() {
		if w.stopped() {
			w.stop()
			break
		}
		w.runStage(w.st.Stage, resume)
		resume = false
	}
	w.logf("워커 종료: %s", w.st.Detail)
}

// runStage 는 한 단계(동작 → 재체크 → 질문 → 답 처리)를 한 번 한다. 다음에 할 단계는 상태에 기록된다.
func (w *worker) runStage(stage int, resume bool) {
	w.info = nil
	if w.reload != nil {
		w.reload()
	}
	var actErr string
	switch stage {
	case stageFix:
		if !resume {
			actErr = w.doFix()
		}
	case stageLoad:
		if !resume && !w.st.XAsked { // 부하 단계를 새로 시작: CPU X 호스트 재실행 여부를 한 번 묻는다
			if !w.askCPURetry() {
				return
			}
			cpu := w.cpuTargets()
			if len(cpu) == 0 && len(w.stressTargets(cpu)) == 0 { // 할 일이 없으면(모두 n·부하 높음) 부하 단계는 건너뜀
				w.logf("부하 단계: 돌릴 대상이 없어 건너뜁니다")
				w.advance(stage)
				return
			}
		}
		if !resume || (!w.st.CPULaunch.IsZero() && !w.st.CPUWaitDone) {
			actErr = w.doLoad(resume)
		}
	case stageNG:
		if !resume {
			actErr = w.doNG()
		}
	}
	if w.st.Stopped { // 동작 중 질문에서 중지를 골랐음(이미 stop() 처리됨)
		return
	}
	if w.stopped() { // 동작 중 중지 요청: 재체크·질문 없이 중지
		w.stop()
		return
	}
	if !w.rejudge(stage) {
		return
	}
	if actErr != "" {
		w.update(func(s *runState) { s.LastError = actErr })
		switch w.ask(stage, actErr+"\n"+w.summary(stage), ansRetry, ansStop) {
		case ansStop:
			w.stop()
		}
		return
	}
	bad := w.notOK(stage)
	// ★ 부하·stress-ng 단계는 대상이 모두 O 가 돼야 다음으로 간다: O 아닌 호스트가 있으면 '계속'은 없고
	// 다시(r) / O 아닌 호스트 제외하고 계속(x) / 중지(s) 만 고른다. (CPU 가 O 가 돼야 stress-ng 를 돌린다)
	blocked := len(bad) > 0 && (stage == stageLoad || stage == stageNG)
	var keys []string
	if !blocked {
		keys = append(keys, ansCont)
	}
	keys = append(keys, ansRetry)
	if len(bad) > 0 {
		keys = append(keys, ansExclude)
	}
	keys = append(keys, ansStop)
	text := w.summary(stage)
	if len(w.info) > 0 {
		text += "\n" + strings.Join(w.info, "\n")
	}
	if blocked {
		text += fmt.Sprintf("\nO 가 아닌 호스트 %d대가 있어 다음 단계로 그냥 넘어갈 수 없습니다 — r 로 다시 하거나 x 로 그 호스트를 빼고 계속하세요", len(bad))
	}
	switch w.ask(stage, text, keys...) {
	case ansCont:
		w.advance(stage)
	case ansExclude:
		ex := toSet(w.notOK(stage))
		w.update(func(s *runState) {
			for i := range s.Hosts {
				if ex[s.Hosts[i].Host] {
					s.Hosts[i].Excluded = true
				}
			}
		})
		w.logf("제외: %d대", len(ex))
		w.advance(stage)
	case ansStop:
		w.stop()
	}
	// ansRetry: 단계 그대로 → 같은 단계를 다시 한다
}

// advance 는 다음 단계로 넘어간다(완료면 결과 저장).
func (w *worker) advance(stage int) {
	if stage == stageLoad { // 부하 단계가 끝나면 CPU 재실행 표시를 끈다
		w.update(func(s *runState) {
			for i := range s.Hosts {
				s.Hosts[i].CPURetry = false
			}
		})
	}
	n := w.nextStage(stage)
	if n == stageFinish {
		w.finish()
		return
	}
	w.update(func(s *runState) {
		s.Stage = n
		s.Detail = stageName(n) + " 단계 시작"
		if n == stageLoad {
			s.XAsked = false
		}
	})
	w.logf("단계 %d %s → %d %s", stage, stageName(stage), n, stageName(n))
}

func (w *worker) finish() {
	bad := 0
	for i := range w.st.Hosts {
		r := &w.st.Hosts[i]
		if active(r) && (!w.checkOK(r.Check) || r.CPU != "O" || r.Stress != "O") {
			bad++
		}
	}
	w.update(func(s *runState) {
		s.Stage = stageFinish
		s.Busy = false
		s.Question = nil
		s.Detail = "완료"
		if bad > 0 {
			s.Detail = fmt.Sprintf("완료(세 열이 모두 O 가 아닌 호스트 %d대)", bad)
		}
	})
	w.saveResult()
}

func (w *worker) stop() {
	w.update(func(s *runState) {
		s.Stopped = true
		s.Busy = false
		s.Question = nil
		for i := range s.Hosts {
			s.Hosts[i].Running = ""
		}
		s.Detail = stageName(s.Stage) + " 단계에서 중지"
	})
	os.Remove(filepath.Join(w.dir, stopReqFile))
	w.saveResult()
}

func (w *worker) saveResult() {
	w.mu.Lock()
	err := writeResult(w.dir, w.st)
	w.mu.Unlock()
	if err != nil {
		w.logf("결과 저장 실패: %v", err)
	} else {
		w.logf("결과 저장: %s", filepath.Join(w.dir, resultFile))
	}
}

// ask 는 질문을 기록하고 answer.json 에 같은 id·허용된 선택이 올 때까지 기다린다.
func (w *worker) ask(stage int, text string, keys ...string) string {
	c, _ := w.askQ(stage, text, nil, keys...)
	return c
}

// askQ 는 ask 에 입력 항목(inputs)을 더한 것: inputs 가 있으면 계속(c)은 검사를 통과한 값이 담긴 답일 때만 받아들이고 그 값을 돌려준다.
func (w *worker) askQ(stage int, text string, inputs []inputField, keys ...string) (string, map[string]string) {
	os.Remove(filepath.Join(w.dir, answerFile)) // 지난 답이 남아 있으면 버림
	var id string
	w.update(func(s *runState) {
		s.QSeq++
		id = fmt.Sprintf("q%d", s.QSeq)
		q := &question{ID: id, Stage: stage, Text: text, Asked: w.ex.Now(), Inputs: inputs}
		for _, k := range keys {
			label := w.choiceLabel(stage, k)
			if len(inputs) > 0 && k == ansCont {
				label = "입력한 값으로 계속"
			}
			q.Choices = append(q.Choices, choice{Key: k, Label: label})
		}
		s.Question = q
		s.Busy = false
		s.Detail = stageName(stage) + " 단계 — 답을 기다리는 중"
	})
	w.logf("질문 %s (%s): %s", id, strings.Join(keys, "/"), strings.ReplaceAll(text, "\n", " / "))
	ok := toSet(keys)
	for {
		w.ex.Sleep(answerPoll)
		if w.stopped() {
			return ansStop, nil
		}
		a := takeAnswer(w.dir)
		if a == nil {
			continue
		}
		if a.ID != id || !ok[a.Choice] {
			w.logf("무시한 답: id=%s choice=%s (기다리는 질문 %s)", a.ID, a.Choice, id)
			continue
		}
		if len(inputs) > 0 && a.Choice == ansCont {
			if err := checkEndAll(a.Values, w.ex.Now()); err != nil {
				w.logf("무시한 답: 입력값 오류(%v)", err)
				w.update(func(s *runState) { s.LastError = "입력값 오류: " + err.Error() })
				continue
			}
		}
		w.update(func(s *runState) { s.Question = nil })
		w.logf("답 %s: %s", id, a.Choice)
		return a.Choice, a.Values
	}
}

// askEnd 는 stress-ng 종료 시각(5개 값)을 묻는다. 값을 stress_end 에 기록해 "연,월,일,시,분" 으로 돌려준다.
// 결과: endCont(값 받음) / endSkip(k: stress-ng 만 건너뜀, 기록 안 함) / endStop(중지 — 이미 stop() 을 불렀다).
func (w *worker) askEnd(stage int, n int, name string) (end string, res int) {
	text := fmt.Sprintf("%s 대상 %d대에 stress-ng 를 돌립니다.\n끝낼 시각을 입력하세요 (지금보다 뒤).", name, n)
	c, vals := w.askQ(stage, text, endFields, ansCont, ansSkip, ansStop)
	switch c {
	case ansSkip:
		w.logf("stress-ng 건너뜀 (%d대)", n)
		return "", endSkip
	case ansCont:
	default:
		w.stop()
		return "", endStop
	}
	end = endArg(vals)
	w.update(func(s *runState) {
		s.StressEnd = end
		if strings.HasPrefix(s.LastError, "입력값 오류") {
			s.LastError = ""
		}
	})
	w.logf("stress-ng 종료 시각: %s", endText(end))
	return end, endCont
}

// askEnd 결과
const (
	endCont = iota // 종료 시각을 입력받음
	endSkip        // k: stress-ng 만 건너뜀
	endStop        // 중지
)

// clearNGSkip 은 stress-ng 를 실제로 띄우는 호스트를 건너뜀 목록에서 뺀다(w.update 안에서 부른다).
func clearNGSkip(s *runState, hosts []string) {
	set := toSet(hosts)
	var rest []string
	for _, h := range s.NGSkip {
		if !set[h] {
			rest = append(rest, h)
		}
	}
	s.NGSkip = rest
}

func endFieldNames() string {
	var p []string
	for _, f := range endFields {
		p = append(p, f.Key+"("+f.Label+")")
	}
	return strings.Join(p, " ")
}

// busyQuestion 은 인벤토리 사용 중 질문(r/x)이다. 대화형 busyAsk 와 -auto 의 보류 질문(auto.go askBusy)이 같이 쓴다.
func busyQuestion(id string, stage int, name, msg string, now time.Time) *question {
	return &question{ID: id, Stage: stage, Kind: "busy", Asked: now,
		Text: name + " 테스트: 작업 템플릿의 인벤토리가 DEFAULT 가 아니어서 멈췄습니다(시간 제한은 멈춰 있습니다).\n" + msg +
			"\n그 인벤토리 사용자가 DEFAULT 로 되돌린 뒤 r(다시 확인), 이 묶음을 포기하려면 x.",
		Choices: []choice{{Key: ansRetry, Label: "다시 확인"}, {Key: ansExclude, Label: "이 묶음 중단"}}}
}

// busyAsk 는 stress-test 가 템플릿 인벤토리 사용 중이라 멈췄을 때(단계는 그대로) r(다시 확인)/x(이 묶음 중단)를 묻고 답을 돌려준다.
// 질문은 한 번에 하나만 띄운다(다른 프로세스가 걸리면 앞 질문이 끝날 때까지 기다린다). 중지 요청이 오면 x.
func (w *worker) busyAsk(name, msg string) string {
	w.qmu.Lock()
	defer w.qmu.Unlock()
	if w.stopped() {
		return ansExclude
	}
	os.Remove(filepath.Join(w.dir, answerFile))
	var id, saved, waitDetail string
	w.update(func(s *runState) {
		s.QSeq++
		id = fmt.Sprintf("q%d", s.QSeq)
		saved = s.Detail
		waitDetail = stageName(s.Stage) + " 단계 — " + name + " 템플릿 인벤토리 사용 중, 답을 기다리는 중"
		s.Question = busyQuestion(id, s.Stage, name, msg, w.ex.Now())
		s.Busy = false
		s.Detail = waitDetail
	})
	w.logf("질문 %s (r/x, %s 인벤토리 사용 중): %s", id, name, strings.ReplaceAll(msg, "\n", " / "))
	clear := func(resume bool) {
		w.update(func(s *runState) {
			if s.Question != nil && s.Question.ID == id {
				s.Question = nil
			}
			if resume && !s.Stopped {
				s.Busy = true
				if s.Detail == waitDetail {
					s.Detail = saved
				}
			}
		})
	}
	for {
		w.ex.Sleep(answerPoll)
		if w.stopped() {
			clear(false)
			return ansExclude
		}
		a := takeAnswer(w.dir)
		if a == nil {
			continue
		}
		if a.ID != id || (a.Choice != ansRetry && a.Choice != ansExclude) {
			w.logf("무시한 답: id=%s choice=%s (기다리는 질문 %s)", a.ID, a.Choice, id)
			continue
		}
		clear(true)
		w.logf("답 %s: %s", id, a.Choice)
		return a.Choice
	}
}

// busyFn 은 name(CPU / stress-ng) 프로세스용 busy 콜백.
func (w *worker) busyFn(name string) func(string) string {
	return func(msg string) string { return w.busyAsk(name, msg) }
}

func (w *worker) choiceLabel(stage int, k string) string {
	switch k {
	case ansCont:
		n := w.nextStage(stage)
		if n == stageFinish {
			return "계속 → 완료(결과 저장)"
		}
		return "계속 → " + stageName(n)
	case ansRetry:
		return "재시도(" + []string{"", "재판정", "설정수정 다시", "부하 다시", "stress-ng 다시", ""}[stage] + ")"
	case ansExclude:
		return fmt.Sprintf("O 아닌 %d대 제외하고 계속", len(w.notOK(stage)))
	case ansYes:
		return "모두 CPU 재실행에 포함"
	case ansNo:
		return "모두 제외(이번엔 X 그대로)"
	case ansSkip:
		return "stress-ng 만 건너뜀(다른 테스트는 그대로)"
	case ansStop:
		return "중지"
	}
	return k
}

// askCPURetry 는 부하 단계를 새로 시작할 때 한 번, CPU 열이 X 인 호스트(강제 중지된 이전 CPU 테스트 흔적)의 1분 부하를
// gossh 로 재서 부하가 cpuRetryMaxLoad 미만인 호스트를 CPU 재실행에 넣을지 y/n 으로 묻는다.
// 부하가 높거나 응답 없는 호스트는 묻지 않고 건너뛰며 안내만 남긴다. 중지했으면 false(이미 stop() 을 불렀다).
func (w *worker) askCPURetry() bool {
	w.update(func(s *runState) { s.XAsked = true })
	cand := w.xCandidates()
	if len(cand) == 0 {
		return true
	}
	w.update(func(s *runState) {
		s.Busy = true
		s.Detail = fmt.Sprintf("CPU X 호스트 %d대 부하 확인 중", len(cand))
	})
	loads, err := w.ex.Loads(cand)
	w.update(func(s *runState) { s.Busy = false })
	if err != nil {
		w.logf("CPU X 호스트 부하 확인 실패: %v", err)
		loads = nil
	}
	if w.stopped() {
		w.stop()
		return false
	}
	var low, busy, noResp []string
	for _, h := range cand {
		l, ok := loads[h]
		switch {
		case !ok:
			noResp = append(noResp, h)
		case l < cpuRetryMaxLoad:
			low = append(low, fmt.Sprintf("%s(%.2f)", h, l))
		default:
			busy = append(busy, fmt.Sprintf("%s(%.2f)", h, l))
		}
	}
	w.logf("CPU X 호스트 %d대 부하 확인: 낮음 %d, 높음(아직 도는 중) %d, 응답 없음 %d", len(cand), len(low), len(busy), len(noResp))
	if len(busy) > 0 {
		w.info = append(w.info, fmt.Sprintf("CPU X 이지만 부하 %.0f 이상이라 아직 도는 중으로 보고 건너뜀: %s", cpuRetryMaxLoad, strings.Join(busy, " ")))
	}
	if len(noResp) > 0 {
		w.info = append(w.info, "CPU X 이지만 부하 확인에 응답이 없어 건너뜀: "+strings.Join(noResp, " "))
	}
	if len(low) == 0 {
		return true
	}
	text := fmt.Sprintf("CPU 열이 X 인 호스트 중 1분 부하가 %.0f 미만인 %d대가 있습니다(이전 CPU 테스트가 강제로 멈춘 것으로 보임): %s", cpuRetryMaxLoad, len(low), strings.Join(low, " "))
	if len(busy)+len(noResp) > 0 {
		text += fmt.Sprintf("\n건너뜀: 부하 높음 %d대, 응답 없음 %d대", len(busy), len(noResp))
	}
	text += "\n이 호스트들을 이번 CPU 테스트에 다시 포함할까요? y = 모두 포함, n = 모두 제외(이번엔 X 그대로)"
	switch w.ask(stageLoad, text, ansYes, ansNo, ansStop) {
	case ansYes:
		var set = map[string]bool{}
		for _, h := range cand {
			l, ok := loads[h]
			set[h] = ok && l < cpuRetryMaxLoad
		}
		w.update(func(s *runState) {
			for i := range s.Hosts {
				if set[s.Hosts[i].Host] {
					s.Hosts[i].CPURetry = true
				}
			}
		})
		w.logf("CPU X 호스트 %d대를 CPU 재실행에 포함", len(low))
	case ansStop:
		w.stop()
		return false
	default:
		w.logf("CPU X 호스트 %d대는 이번엔 제외(X 그대로)", len(low))
	}
	return true
}

// summary 는 질문에 넣을 단계 결과 요약.
func (w *worker) summary(stage int) string {
	switch stage {
	case stageLoad:
		s := w.listSummary("CPU", w.st.CPUList, func(r *hostRow) bool { return r.CPU == "O" })
		if len(w.st.StressList) > 0 {
			s += " / " + w.listSummary("stress-ng", w.st.StressList, func(r *hostRow) bool { return r.Stress == "O" })
		}
		return s
	case stageNG:
		return w.listSummary("stress-ng", w.st.StressList, func(r *hostRow) bool { return r.Stress == "O" })
	}
	done, excl := 0, 0
	cnt := map[string]int{}
	for i := range w.st.Hosts {
		r := &w.st.Hosts[i]
		if r.Status != stDone {
			continue
		}
		done++
		if r.Excluded {
			excl++
			continue
		}
		cnt[r.Check]++
	}
	return fmt.Sprintf("완료 %d대(제외 %d): 설정체크 O %d, X %d, 출력없음 %d, - %d", done, excl, cnt["O"], cnt["X"], cnt[noOutput], cnt["-"])
}

func (w *worker) listSummary(name string, hosts []string, ok func(r *hostRow) bool) string {
	o, rb, nr := 0, 0, 0
	for _, h := range hosts {
		r := w.st.row(h)
		if r == nil {
			continue
		}
		if ok(r) {
			o++
		}
		if r.Rebooted {
			rb++
		}
		if r.NoResp {
			nr++
		}
	}
	s := fmt.Sprintf("%s 대상 %d대: O %d, 미완료 %d", name, len(hosts), o, len(hosts)-o)
	if rb > 0 {
		s += fmt.Sprintf(", 재부팅됨 %d", rb)
	}
	if nr > 0 {
		s += fmt.Sprintf(", 응답없음 %d", nr)
	}
	return s
}

// rejudge 는 판정을 다시 돌려 표를 갱신한다. 실패하면 r/s 를 묻는다(r 이면 다시 판정). 중지했으면 false.
func (w *worker) rejudge(stage int) bool {
	for {
		w.update(func(s *runState) { s.Busy = true; s.Detail = stageName(stage) + " — 판정(재체크) 중" })
		jr, err := w.ex.Judge()
		if err == nil {
			w.update(func(s *runState) { w.apply(s, jr); s.Busy = false })
			return true
		}
		msg := "판정 실패: " + errText(err)
		w.logf("%s", msg)
		w.update(func(s *runState) { s.LastError = msg; s.Busy = false })
		if w.stopped() {
			w.stop()
			return false
		}
		if w.ask(stage, msg, ansRetry, ansStop) == ansStop {
			w.stop()
			return false
		}
	}
}

// apply 는 판정 결과로 행을 다시 만든다(호스트파일 순서). 진행 표시(제외·재부팅 등)는 이름으로 이어 받는다.
func (w *worker) apply(s *runState, jr *judgeResult) {
	old := map[string]hostRow{}
	for _, r := range s.Hosts {
		old[r.Host] = r
	}
	rows := make([]hostRow, 0, len(jr.Hosts))
	for _, h := range jr.Hosts {
		r := old[h.Host]
		r.Host, r.Status, r.Check, r.CPU, r.Stress, r.HT, r.OS = h.Host, h.Status, h.Check, h.CPU, h.Stress, h.HT, h.OS
		r.Note = w.notes[strings.ToLower(h.Host)]
		r.VendorX = h.VendorUnknown
		if r.CPU == "O" {
			r.CPURetry = false
		}
		if r.NGLaunched && r.Status == stDone { // -auto: stress-ng 는 launch 되면 O 로 둔다(로그가 아직 없어도)
			r.Stress = "O"
		}
		rows = append(rows, r)
	}
	s.Hosts = rows
}

// errText 는 오류를 질문용 문구로 만든다(시간 제한이면 "시간초과").
func errText(err error) string {
	if errors.Is(err, errTimeout) {
		return "시간초과"
	}
	if errors.Is(err, errCanceled) {
		return "중지 요청"
	}
	return err.Error()
}

// ---------------------------------------------------------------------------
// 단계 동작
// ---------------------------------------------------------------------------

func (w *worker) listPath(name string) string { return filepath.Join(w.dir, listsDir, name) }

// doFix 는 설정수정(config_check -auto <user>)을 한다. 실패하면 질문 문구를 돌려준다.
func (w *worker) doFix() string {
	hosts := w.fixTargets()
	if len(hosts) == 0 {
		return ""
	}
	_ = writeList(w.listPath("fix.txt"), hosts)
	w.update(func(s *runState) {
		s.Busy = true
		s.Detail = fmt.Sprintf("설정수정 중: config_check %s %s (%d대)", ccFix, s.User, len(hosts))
	})
	w.logf("설정수정 시작 %d대: %s", len(hosts), strings.Join(hosts, " "))
	if err := w.ex.ConfigFix(hosts); err != nil {
		w.logf("설정수정 실패: %v", err)
		return "설정수정 실패: " + errText(err)
	}
	return ""
}

// stressOutcome 은 stress-test 종료를 해석한다. fatal 이 비어 있지 않으면 r/s 질문.
func stressOutcome(name string, code int, err error) (fatal, info string) {
	switch {
	case err != nil:
		return name + " stress-test " + errText(err), ""
	case code == 1:
		return "", name + " stress-test: 미실행/실패 호스트가 있습니다(종료코드 1, worker.log 참고)"
	case code != 0:
		return fmt.Sprintf("%s stress-test 오류 종료(종료코드 %d, worker.log 참고)", name, code), ""
	}
	return "", ""
}

// doNG 는 stress-ng 단계(이번 CPU 대상 중 CPU O 가 된 호스트에 stress-test -steps b).
func (w *worker) doNG() string {
	hosts := w.ngTargets()
	if len(hosts) == 0 {
		return ""
	}
	list := w.listPath("stressng.txt")
	if err := writeList(list, hosts); err != nil {
		return "목록 파일을 쓸 수 없습니다: " + err.Error()
	}
	end, res := w.askEnd(stageNG, len(hosts), "stress-ng")
	switch res {
	case endStop:
		return ""
	case endSkip:
		// stress-ng 는 반드시 돌아야 하므로 열은 O 가 아닌 채 남고, 단계 끝 질문(r/x/s)을 한 번 더 한다.
		w.update(func(s *runState) { s.StressList = hosts })
		return ""
	}
	set := toSet(hosts)
	w.update(func(s *runState) {
		s.StressList = hosts
		clearNGSkip(s, hosts)
		s.Busy = true
		s.Detail = fmt.Sprintf("stress-ng 등록·실행 중 (%d대)", len(hosts))
		for i := range s.Hosts {
			if set[s.Hosts[i].Host] {
				s.Hosts[i].Running = prgNGRun
			}
		}
	})
	w.logf("stress-ng 단계 시작 %d대", len(hosts))
	code, err := w.ex.StressTest("b", list, hosts, end, nil, nil, w.busyFn("stress-ng"))
	w.clearRunning(hosts)
	fatal, info := stressOutcome("stress-ng", code, err)
	if info != "" {
		w.info = append(w.info, info)
	}
	return fatal
}

func (w *worker) clearRunning(hosts []string) {
	set := toSet(hosts)
	w.update(func(s *runState) {
		for i := range s.Hosts {
			if set[s.Hosts[i].Host] {
				s.Hosts[i].Running = ""
			}
		}
	})
}

type stressRes struct {
	code int
	err  error
}

// doLoad 는 부하 단계: CPU(stress-test -steps a) 등록·launch 가 끝난 뒤 stress-ng(-steps b) 를 띄우고,
// 두 감시를 함께 진행한다. resume 이면 아무것도 띄우지 않고 기록된 CPU 감시 시간표만 이어 간다.
func (w *worker) doLoad(resume bool) string {
	var fatal []string
	note := func(name string, r stressRes) {
		f, i := stressOutcome(name, r.code, r.err)
		if f != "" {
			fatal = append(fatal, f)
			w.logf("%s", f)
		}
		if i != "" {
			w.info = append(w.info, i)
		}
	}
	var cpuCh, ngCh chan stressRes
	cpu, stress := w.st.CPUList, w.st.StressList
	end := ""

	if !resume {
		cpu = w.cpuTargets()
		stress = w.stressTargets(cpu)
		if len(stress) > 0 { // CPU 를 띄우기 전에 stress-ng 종료 시각부터 받는다
			var res int
			end, res = w.askEnd(stageLoad, len(stress), "stress-ng")
			switch res {
			case endStop:
				return ""
			case endSkip: // stress-ng 만 건너뜀: CPU 는 그대로 진행, 건너뛴 호스트는 다음 stress-ng 단계에서 다시 묻는다
				skipped := stress
				stress = nil
				w.update(func(s *runState) {
					have := toSet(s.NGSkip)
					for _, h := range skipped {
						if !have[h] {
							s.NGSkip = append(s.NGSkip, h)
						}
					}
				})
			}
		}
		cpuSet, ngSet := toSet(cpu), toSet(stress)
		w.update(func(s *runState) {
			s.CPUList, s.StressList = cpu, stress
			clearNGSkip(s, stress)
			s.CPULaunch, s.NextCheck, s.CheckStart, s.Deadline = time.Time{}, time.Time{}, time.Time{}, time.Time{}
			s.CPUWaitDone = false
			s.Busy = true
			for i := range s.Hosts {
				r := &s.Hosts[i]
				switch {
				case cpuSet[r.Host]:
					r.CPUTarget = true
					r.Rebooted, r.NoResp, r.Btime = false, false, 0
					r.Running = prgCPURun
				case ngSet[r.Host]:
					r.Running = prgNGRun
				}
			}
		})
		w.logf("부하 단계 시작: CPU %d대 [%s], stress-ng %d대 [%s]", len(cpu), strings.Join(cpu, " "), len(stress), strings.Join(stress, " "))

		if len(cpu) > 0 {
			list := w.listPath("cpu.txt")
			if err := writeList(list, cpu); err != nil {
				w.clearRunning(append(append([]string{}, cpu...), stress...))
				return "목록 파일을 쓸 수 없습니다: " + err.Error()
			}
			w.update(func(s *runState) { s.Detail = fmt.Sprintf("CPU 등록·launch 중 (%d대)", len(cpu)) })
			launched := make(chan struct{})
			var once sync.Once
			cpuCh = make(chan stressRes, 1)
			go func() {
				c, e := w.ex.StressTest("a", list, cpu, "", func() { once.Do(func() { close(launched) }) }, nil, w.busyFn("CPU"))
				cpuCh <- stressRes{c, e}
			}()
			ok := false
			select {
			case <-launched:
				ok = true
			case r := <-cpuCh:
				cpuCh = nil
				select {
				case <-launched:
					ok = true
				default:
				}
				note("CPU", r)
				if !ok && len(fatal) == 0 {
					fatal = append(fatal, "CPU 테스트가 launch 되지 않았습니다(stress-test 결과는 worker.log 참고)")
				}
			}
			if !ok {
				// ★ CPU 가 launch 되지 않았으면 stress-ng 도 띄우지 않고 질문(r/s)한다.
				w.clearRunning(append(append([]string{}, cpu...), stress...))
				return strings.Join(fatal, "\n")
			}
			now := w.ex.Now()
			w.update(func(s *runState) {
				s.CPULaunch = now
				s.CheckStart = now.Add(w.conf.CPUWaitStart)
				s.Deadline = s.CheckStart.Add(w.conf.CPUWaitMax)
				s.NextCheck = w.nextTick(s, now)
			})
			w.logf("CPU launch 완료: 완료 확인 시작 %s, 마감 %s", w.st.CheckStart.Format("15:04:05"), w.st.Deadline.Format("15:04:05"))
			w.recordBtime(cpu)
		}
		if len(stress) > 0 {
			list := w.listPath("stress.txt")
			if err := writeList(list, stress); err != nil {
				fatal = append(fatal, "목록 파일을 쓸 수 없습니다: "+err.Error())
				w.clearRunning(stress)
			} else {
				ngCh = make(chan stressRes, 1)
				go func() {
					c, e := w.ex.StressTest("b", list, stress, end, nil, nil, w.busyFn("stress-ng"))
					ngCh <- stressRes{c, e}
				}()
			}
		}
	} else {
		pend := toSet(w.cpuPending())
		w.update(func(s *runState) {
			s.Busy = true
			for i := range s.Hosts {
				if pend[s.Hosts[i].Host] {
					s.Hosts[i].Running = prgCPURun
				}
			}
		})
		w.logf("CPU 감시 이어서 진행: 다음 확인 %s", w.st.NextCheck.Format("15:04:05"))
	}

	ngDone := func(r stressRes) {
		note("stress-ng", r)
		w.recheckStress(stress)
	}
	// CPU 감시 시간표: NextCheck 마다 btime·응답(·확인 시작 뒤에는 CPU 열) 확인
	for !w.st.CPULaunch.IsZero() && !w.st.CPUWaitDone {
		if ngCh != nil {
			select {
			case r := <-ngCh:
				ngCh = nil
				ngDone(r)
			default:
			}
		}
		if cpuCh != nil {
			select {
			case r := <-cpuCh:
				cpuCh = nil
				note("CPU", r)
			default:
			}
		}
		if w.stopped() {
			break
		}
		w.update(func(s *runState) {
			s.Detail = fmt.Sprintf("CPU 감시 중: 다음 확인 %s, 완료 확인 시작 %s, 마감 %s",
				s.NextCheck.Format("15:04:05"), s.CheckStart.Format("15:04:05"), s.Deadline.Format("15:04:05"))
		})
		if d := w.st.NextCheck.Sub(w.ex.Now()); d > 0 {
			w.ex.Sleep(d)
			if w.stopped() {
				break
			}
			continue
		}
		w.cpuTick(w.ex.Now())
	}
	if cpuCh != nil {
		note("CPU", <-cpuCh)
	}
	if ngCh != nil {
		w.update(func(s *runState) { s.Detail = "stress-ng 감시가 끝나기를 기다리는 중" })
		ngDone(<-ngCh)
	}
	w.clearRunning(append(append([]string{}, cpu...), stress...))
	return strings.Join(fatal, "\n")
}

// nextTick 은 prev 다음 확인 시각: 간격마다, 단 완료 확인 시작 시각과 마감 시각은 건너뛰지 않는다.
func (w *worker) nextTick(s *runState, prev time.Time) time.Time {
	n := prev.Add(w.conf.CPUWaitInt)
	if prev.Before(s.CheckStart) && n.After(s.CheckStart) {
		n = s.CheckStart
	}
	if n.After(s.Deadline) {
		n = s.Deadline
	}
	return n
}

// cpuPending 은 이번 CPU 대상 중 아직 CPU O 가 아닌 호스트.
func (w *worker) cpuPending() []string {
	set := toSet(w.st.CPUList)
	return w.pick(func(r *hostRow) bool { return set[r.Host] && r.CPU != "O" })
}

// recordBtime 은 launch 직후 부팅 시각을 기록한다(응답 없으면 첫 감시 때 기록).
func (w *worker) recordBtime(hosts []string) {
	res, err := w.ex.Probe(hosts)
	if err != nil {
		w.logf("launch 시 btime 확인 실패: %v", err)
		return
	}
	w.update(func(s *runState) {
		for _, h := range hosts {
			if p, ok := res[h]; ok {
				if r := s.row(h); r != nil {
					r.Btime = p.Btime
				}
			}
		}
	})
}

// cpuTick 은 감시 한 회차: btime·응답 확인, 확인 시작 뒤면 CPU 열 갱신, 끝나면 CPUWaitDone.
func (w *worker) cpuTick(now time.Time) {
	pending := w.cpuPending()
	var res map[string]probeInfo
	var err error
	if len(pending) > 0 {
		res, err = w.ex.Probe(pending)
		if err != nil {
			w.logf("CPU 감시 확인 실패: %v", err)
		}
	}
	checking := !now.Before(w.st.CheckStart)
	w.update(func(s *runState) {
		if err != nil {
			s.LastError = "CPU 감시 확인 실패: " + errText(err)
		}
		for _, h := range pending {
			r := s.row(h)
			if r == nil || err != nil {
				continue
			}
			p, ok := res[h]
			if !ok {
				r.NoResp = true
				continue
			}
			r.NoResp = false
			switch {
			case r.Btime == 0:
				r.Btime = p.Btime
			case p.Btime-r.Btime > btimeTolerance || r.Btime-p.Btime > btimeTolerance:
				r.Rebooted = true
			}
			if checking {
				r.CPU = p.CPU
				if p.CPU == "O" {
					r.Running = ""
					r.CPURetry = false
				}
			}
		}
	})
	left := len(w.cpuPending())
	if left == 0 || !now.Before(w.st.Deadline) {
		w.update(func(s *runState) { s.CPUWaitDone = true })
		w.logf("CPU 감시 끝: 미완료 %d대", left)
		return
	}
	w.update(func(s *runState) {
		n := w.nextTick(s, s.NextCheck)
		for !n.After(now) && n.Before(s.Deadline) {
			n = w.nextTick(s, n)
		}
		s.NextCheck = n
	})
}

// recheckStress 는 stress-ng 쪽 stress-test 가 끝난 뒤 그 호스트들의 stress-ng 값을 다시 확인한다.
func (w *worker) recheckStress(hosts []string) {
	if len(hosts) == 0 {
		return
	}
	res, err := w.ex.Probe(hosts)
	if err != nil {
		w.logf("stress-ng 재확인 실패: %v", err)
	}
	set := toSet(hosts)
	w.update(func(s *runState) {
		for i := range s.Hosts {
			r := &s.Hosts[i]
			if !set[r.Host] {
				continue
			}
			r.Running = ""
			if p, ok := res[r.Host]; ok {
				r.Stress = p.Stress
			}
		}
	})
}

// ---------------------------------------------------------------------------
// 워커 진입·기동
// ---------------------------------------------------------------------------

// loadRunConf 는 상태에 기록된 설정 파일을 읽는다(빈 경로 = 기본값).
func loadRunConf(p string) (config, error) {
	if p == "" {
		return parseConf(""), nil
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return config{}, fmt.Errorf("설정 파일을 읽을 수 없습니다: %v", err)
	}
	c := relToConf(parseConf(string(b)), p)
	if err := remoteConfErr(c); err != nil {
		return config{}, err
	}
	return c, nil
}

// runWorker 는 `new_setup -run-worker <상태폴더>` 진입점. stdout·stderr 는 worker.log 로 이어져 있다.
// 시작 준비가 실패하면 last_error 만 기록하고 끝낸다(중지로 표시하지 않으므로 고친 뒤 재기동할 수 있다).
func runWorker(dir string) int {
	if a, err := filepath.Abs(dir); err == nil {
		dir = a
	}
	st, err := loadState(dir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "오류: 상태를 읽을 수 없습니다: %v\n", err)
		return 2
	}
	release, err := lockState(dir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "오류: %v\n", err)
		return 1
	}
	defer release()
	_ = os.WriteFile(filepath.Join(dir, pidFile), []byte(fmt.Sprintf("%d\n", os.Getpid())), 0o644)

	ex := &realExec{hostArg: st.HostArg, user: st.User, dir: dir, log: os.Stdout}
	w := newWorker(dir, st, ex, config{}, nil, os.Stderr)
	setupFail := func(msg string) int {
		w.logf("워커 시작 실패: %s", msg)
		w.update(func(s *runState) { s.LastError = msg; s.Busy = false; s.WorkerPID = 0 })
		return 2
	}
	w.update(func(s *runState) { s.WorkerPID = os.Getpid() })
	if err := os.Chdir(st.WorkDir); err != nil {
		return setupFail(fmt.Sprintf("작업 경로로 이동할 수 없습니다: %v", err))
	}
	conf, err := loadRunConf(st.ConfPath)
	if err != nil {
		return setupFail(err.Error())
	}
	for _, m := range conf.Warnings {
		w.logf("경고: %s", m)
	}
	gossh := resolveGosshPath(conf.Gossh)
	if gossh == "" {
		p, err := exec.LookPath("gossh")
		if err != nil {
			return setupFail("gossh 를 찾을 수 없습니다(설정 파일 GOSSH= 또는 PATH)")
		}
		gossh = p
	} else if _, err := os.Stat(gossh); err != nil {
		return setupFail("설정 파일의 GOSSH 경로에 gossh 가 없습니다: " + gossh)
	}
	if st.ErrPath != "" {
		if b, err := os.ReadFile(st.ErrPath); err == nil {
			w.notes = parseErrorList(string(b))
		} else {
			w.logf("경고: 장애 목록을 읽을 수 없습니다: %v", err)
		}
	}
	ex.conf, ex.gossh = conf, gossh
	w.conf = conf
	w.reload = func() {
		c, err := loadRunConf(st.ConfPath)
		if err != nil {
			w.logf("경고: 설정 파일을 다시 읽지 못해 이전 값을 씁니다: %v", err)
			return
		}
		if c.StressTest != w.conf.StressTest || c.StressTestConf != w.conf.StressTestConf || c.StressRemote != w.conf.StressRemote {
			w.logf("설정 파일 다시 읽음: STRESS_TEST=%s STRESS_TEST_CONF=%s STRESS_REMOTE=%s", c.StressTest, c.StressTestConf, c.StressRemote)
		}
		ex.conf, w.conf = c, c
	}
	cancelCh = w.stopCh
	if err := os.MkdirAll(filepath.Join(dir, listsDir), 0o755); err != nil {
		return setupFail(err.Error())
	}

	stop := make(chan struct{})
	go w.heartbeat(stop)
	w.logf("워커 시작 pid=%d user=%s 호스트파일=%s 단계=%s", os.Getpid(), st.User, st.HostFile, stageName(st.Stage))
	w.run()
	close(stop)
	w.update(func(s *runState) { s.WorkerPID = 0 })
	return 0
}

// startWorker 는 현재 실행 파일을 `-run-worker <dir>` 로 새 세션(setsid)에 띄운다.
// stdout·stderr 는 <dir>/worker.log 에 이어 붙이고 pid 를 <dir>/worker.pid 에 쓴다. 띄운 pid 를 돌려준다.
func startWorker(dir string) (int, error) {
	if a, err := filepath.Abs(dir); err == nil {
		dir = a
	}
	exe, err := os.Executable()
	if err != nil {
		return 0, err
	}
	if r, err := filepath.EvalSymlinks(exe); err == nil {
		exe = r
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return 0, err
	}
	lf, err := os.OpenFile(filepath.Join(dir, workerLog), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return 0, err
	}
	defer lf.Close()
	devnull, err := os.Open(os.DevNull)
	if err != nil {
		return 0, err
	}
	defer devnull.Close()
	cmd := exec.Command(exe, "-run-worker", dir)
	cmd.Dir = dir
	cmd.Stdin = devnull
	cmd.Stdout = lf
	cmd.Stderr = lf
	setSetsid(cmd)
	if err := cmd.Start(); err != nil {
		return 0, err
	}
	pid := cmd.Process.Pid
	_ = os.WriteFile(filepath.Join(dir, pidFile), []byte(fmt.Sprintf("%d\n", pid)), 0o644)
	go cmd.Wait() // TUI 가 살아 있는 동안 워커가 먼저 끝나도 좀비로 남지 않게
	return pid, nil
}
