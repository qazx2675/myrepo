package main

// -run -auto 스케줄러(계획서_auto.md, auto_flow.svg). 질문 없이 모든 호스트를 완료 | 설정체크 O | CPU O | stress-ng O 로 만든다.
// 사람이 답하는 질문은 인벤토리 사용 중(r/x, busyAsk)과 (3단계) yaml 바뀜 둘뿐이다.
//
//	① 시작 판정
//	② 설정체크 X → 설정수정 자동(호스트당 최대 3회, 그래도 O 가 아니면 포기)
//	③ CPU 대기열(체크 O, CPU X/NOTRUN): 묶음은 한 번에 하나, 묶음 = 그 시점의 대기열 전체.
//	   CPU X 호스트는 1분 부하가 3 미만일 때만 다시 돌린다(기존 "부하 < 3 재실행" 질문에 자동으로 '다시')
//	④ launch 50분 뒤 판정, O 가 아니면 5분마다 최대 30분 더 → 그래도 아니면 실패 1회(대기열 뒤로), 3회면 포기
//	⑤ CPU O·stress-ng O 아님 → stress-ng(종료 시각 = launch 날 +2일 00:00), launch 되면 바로 O, launch 실패 3회면 포기
//	⑥ 접속 안 되는 호스트는 시작부터 10분마다 재판정(완료가 되면 대기열 뒤에 붙어 ②~⑤를 거친다)
//	12시간이 지나면 새 작업을 시작하지 않고, 도는 stress-ng 와 이미 launch 한 CPU 판정을 마친 뒤 결과를 쓰고 끝낸다.
//
// 판정·설정수정은 차례로 한다(그동안 다른 일은 기다림). stress-test 는 launch 까지만 기다리고, 끝나는 것은 뒤에서 기다린다.

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// autoMaxTries 는 -auto 에서 호스트당 단계별 최대 시도 횟수(다 쓰고도 O 가 아니면 포기).
const autoMaxTries = 3

// autoPoll 은 stress-test 가 뒤에서 도는 동안 끝났는지 확인하는 간격.
var autoPoll = 5 * time.Second

// autoIdleMax 는 할 일이 없을 때도 깨어나 진행 설명(남은 시간)을 갱신하는 최대 간격.
var autoIdleMax = time.Minute

// autoRun 은 -auto 한 번 실행 동안의 메모리 상태(뒤에서 도는 stress-test).
type autoRun struct {
	w       *worker
	judged  bool           // 이번 실행에서 판정에 한 번이라도 성공함
	cpuCh   chan stressRes // launch 뒤 아직 안 끝난 CPU stress-test(없으면 nil)
	ngCh    chan stressRes // launch 뒤 아직 안 끝난 stress-ng stress-test(없으면 nil)
	ngHosts []string
}

// runAuto 는 -auto 스케줄러를 끝(할 일 없음 / 제한 시간 뒤 정리 끝 / 중지)까지 돌린다.
// 재기동이면 기록된 CPU 묶음 판정 시간표와 시도 횟수를 이어 간다(뒤에서 돌던 stress-test 는 기다리지 않는다).
func (w *worker) runAuto() {
	a := &autoRun{w: w}
	w.update(func(s *runState) {
		if s.Auto.Start.IsZero() {
			s.Auto.Start = w.ex.Now()
		}
		s.Auto.Limit = w.conf.AutoLimit
		in := toSet(s.Auto.Batch)
		for i := range s.Hosts {
			if r := &s.Hosts[i]; in[r.Host] && r.CPU != "O" {
				r.Running = prgCPURun
			}
		}
	})
	w.logf("-auto 시작: 시작 %s, 제한 %s(%s 까지)", w.st.Auto.Start.Format("2006-01-02 15:04:05"), w.conf.AutoLimit,
		w.st.Auto.Start.Add(w.conf.AutoLimit).Format("2006-01-02 15:04:05"))
	needJudge := true
	for {
		w.pollYamlAnswer() // yaml 바뀜 질문의 답(s 는 중지 요청이 되므로 바로 아래에서 처리)
		if w.stopped() {
			a.drain()
			w.stop()
			return
		}
		a.reap()
		now := w.ex.Now()
		au := w.st.Auto
		if !au.Ending && !now.Before(au.Start.Add(w.conf.AutoLimit)) {
			w.update(func(s *runState) { s.Auto.Ending = true })
			w.logf("제한 시간(%s)이 지났습니다: 새 작업은 시작하지 않고 진행 중인 것만 마칩니다", w.conf.AutoLimit)
			w.cancelYamlQ("제한 시간이 지나 새 launch 를 하지 않으므로 질문을 취소합니다")
			continue
		}
		if len(au.Batch) > 0 && !now.Before(au.BatchNext) { // ④ 이미 launch 한 묶음 판정은 제한 시간 뒤에도 한다
			a.cpuJudge(now)
			continue
		}
		if !au.Ending {
			if needJudge || ((!a.judged || w.autoUnreached() > 0) && !now.Before(au.NextRecheck)) { // ① ⑥
				needJudge = false
				a.judge()
				w.update(func(s *runState) { s.Auto.NextRecheck = w.ex.Now().Add(w.conf.AutoRecheck) })
				continue
			}
			if hosts := w.autoFixTargets(); len(hosts) > 0 { // ②
				a.fix(hosts)
				continue
			}
			if a.ngCh == nil && !now.Before(au.NGWait) { // ⑤
				if hosts := w.autoNGTargets(); len(hosts) > 0 && a.launchNG(hosts) {
					continue
				}
			}
			if len(au.Batch) == 0 && a.cpuCh == nil && len(au.Queue) > 0 && !now.Before(au.QueueWait) { // ③
				if a.launchCPU() {
					continue
				}
			}
		}
		idle := len(w.st.Auto.Batch) == 0 && a.cpuCh == nil && a.ngCh == nil
		if idle && (w.st.Auto.Ending || a.nothingLeft()) {
			a.finish()
			return
		}
		a.wait(now)
	}
}

// autoYamlReady 는 -auto 에서 CPU·stress-ng launch 바로 앞에 부른다(name = "CPU" / "stress-ng").
// 인벤토리 소스의 source_path·인벤토리 호스트 이름 집합이 기억한 것(Auto.Yml·Auto.YmlHosts = 지난 launch 때 stress-test 가 등록한 값)과
// 다르면 'yaml 바뀜' y/s 질문을 내고 false 를 돌려준다. false 면 이번에는 launch 하지 않고 다음 회차에 다시 부른다.
// 질문은 기다리지 않고 state 에만 걸어 두므로(답은 루프 맨 위 pollYamlAnswer 가 받는다) 그동안 설정수정·재판정·CPU 판정은 계속되고
// 12시간 타이머도 흐른다. y 면 다음 launch 한 번은 확인 없이 진행한다: launch 가 stress-test 를 부르고 그것이 우리 yaml 을 새로 만들어
// 소스에 PATCH·동기화하므로(register.go) 소스와 호스트가 우리 것으로 덮어써진다. 따로 되돌리는 호출은 없다.
// 첫 launch(기억한 것이 없음)는 묻지 않는다. 확인 자체가 실패하면(stress-test 옛 버전·AWX 불통) 기록만 남기고 진행한다.
func (w *worker) autoYamlReady(name string, hosts []string) bool {
	if w.yamlQ != "" { // 질문의 답을 기다리는 중
		return false
	}
	if w.yamlOK {
		w.yamlOK = false
		return true
	}
	au := w.st.Auto
	if au.Yml == "" { // 첫 launch: 비교할 기억이 없다
		return true
	}
	if w.st.Question != nil { // 다른 질문이 떠 있으면 겹치지 않게 미룬다
		return false
	}
	w.update(func(s *runState) { s.Busy = true; s.Detail = "자동 진행 — launch 전 AWX yaml 확인 중" })
	cur, err := w.ex.YamlState()
	w.update(func(s *runState) { s.Busy = false })
	if err != nil {
		w.logf("%s launch 전 yaml 확인 실패(확인 없이 진행): %v", name, err)
		return true
	}
	if !yamlChanged(au.Yml, au.YmlHosts, cur) {
		return true
	}
	w.askYaml(name, len(hosts), au.Yml, au.YmlHosts, cur)
	return false
}

// yamlChanged 는 현재 인벤토리 상태가 우리가 등록한 것과 다른지: 소스 yml 이 다르거나, 기억한 호스트가 있고 호스트 이름 집합이 다르면 true.
func yamlChanged(ourYml string, ourHosts []string, cur yamlInfo) bool {
	if cur.Yml != ourYml {
		return true
	}
	if len(ourHosts) == 0 { // 등록 때 호스트를 읽지 못함: yml 만 비교
		return false
	}
	add, del := hostDelta(ourHosts, cur.Hosts)
	return len(add)+len(del) > 0
}

// hostDelta 는 ours 에 없고 cur 에만 있는 호스트(add)와 ours 에만 있는 호스트(del)를 정렬해서 돌려준다.
func hostDelta(ours, cur []string) (add, del []string) {
	o, c := toSet(ours), toSet(cur)
	for h := range c {
		if !o[h] {
			add = append(add, h)
		}
	}
	for h := range o {
		if !c[h] {
			del = append(del, h)
		}
	}
	sort.Strings(add)
	sort.Strings(del)
	return add, del
}

// listMax 는 목록을 최대 n 개까지 공백으로 이어 보이고 넘치면 "외 k대" 를 붙인다(없으면 "없음").
func listMax(l []string, n int) string {
	if len(l) == 0 {
		return "없음"
	}
	if len(l) <= n {
		return strings.Join(l, " ")
	}
	return fmt.Sprintf("%s 외 %d대", strings.Join(l[:n], " "), len(l)-n)
}

// askYaml 은 'yaml 바뀜' 질문(y = 우리 yaml 로 덮어쓰고 launch, s = 중지)을 걸어 두고 바로 돌아온다. 현재 상태 전체는 로그에 남긴다.
func (w *worker) askYaml(name string, n int, ourYml string, ourHosts []string, cur yamlInfo) {
	add, del := hostDelta(ourHosts, cur.Hosts)
	ours := fmt.Sprintf("우리가 등록한 %d대", len(ourHosts))
	if len(ourHosts) == 0 {
		ours = "우리가 등록한 호스트 수는 모름"
	}
	text := fmt.Sprintf("%s 테스트(%d대)를 launch 하기 전에 AWX 인벤토리가 우리가 등록한 것과 달라졌습니다(다른 사람이 같은 인벤토리로 OS 설치 등을 해서 바꿨을 수 있습니다).\n"+
		"yml: 지금 %s / 우리가 등록한 %s\n호스트: 지금 %d대 / %s", name, n, cur.Yml, ourYml, len(cur.Hosts), ours)
	if len(ourHosts) > 0 {
		text += fmt.Sprintf("\n  지금만 있는 호스트 %d대: %s\n  우리 것인데 없어진 호스트 %d대: %s", len(add), listMax(add, 5), len(del), listMax(del, 5))
	}
	text += "\ny: 우리 yaml 로 다시 등록해 덮어쓰고(다른 작업의 내용은 worker.log 에 남깁니다) launch 를 계속합니다. s: 작업을 중지합니다."
	os.Remove(filepath.Join(w.dir, answerFile)) // 지난 답이 남아 있으면 버림
	var id string
	w.update(func(s *runState) {
		s.QSeq++
		id = fmt.Sprintf("q%d", s.QSeq)
		s.Question = &question{ID: id, Stage: s.Stage, Kind: "yaml", Asked: w.ex.Now(), Text: text,
			Choices: []choice{{Key: ansYes, Label: "우리 yaml 로 덮어쓰고 계속"}, {Key: ansStop, Label: "중지"}}}
		s.Busy = false
	})
	w.yamlQ = id
	w.logf("질문 %s (y/s, %s launch 보류, yaml 바뀜): yml 지금 %s / 우리 %s, 호스트 지금 %d대 / 우리 %d대", id, name, cur.Yml, ourYml, len(cur.Hosts), len(ourHosts))
	w.logf("  지금 인벤토리 호스트 %d대(y 로 덮어쓰기 전 기록): %s", len(cur.Hosts), strings.Join(cur.Hosts, " "))
}

// pollYamlAnswer 는 'yaml 바뀜' 질문의 답을 한 번 확인한다(기다리지 않는다). y 면 다음 launch 를 확인 없이 진행하게 하고,
// s 면 중지 요청 파일을 써서 루프가 중지하게 한다. 다른 id·선택지의 답은 무시한다.
func (w *worker) pollYamlAnswer() {
	if w.yamlQ == "" {
		return
	}
	a := takeAnswer(w.dir)
	if a == nil {
		return
	}
	if a.ID != w.yamlQ || (a.Choice != ansYes && a.Choice != ansStop) {
		w.logf("무시한 답: id=%s choice=%s (기다리는 질문 %s)", a.ID, a.Choice, w.yamlQ)
		return
	}
	id := w.yamlQ
	w.yamlQ = ""
	w.update(func(s *runState) {
		if s.Question != nil && s.Question.ID == id {
			s.Question = nil
		}
	})
	w.logf("답 %s: %s", id, a.Choice)
	if a.Choice == ansStop {
		_ = writeStopReq(w.dir)
		return
	}
	w.yamlOK = true
	w.logf("y: 다음 launch 에서 우리 yaml 을 다시 등록해 덮어씁니다")
}

// cancelYamlQ 는 답을 기다리는 'yaml 바뀜' 질문이 있으면 취소한다(제한 시간이 지나 launch 할 일이 없어질 때).
func (w *worker) cancelYamlQ(why string) {
	if w.yamlQ == "" {
		return
	}
	id := w.yamlQ
	w.yamlQ = ""
	w.update(func(s *runState) {
		if s.Question != nil && s.Question.ID == id {
			s.Question = nil
		}
	})
	w.logf("질문 %s 취소: %s", id, why)
}

// rememberYaml 은 launch 가 끝난 stress-test 가 등록한 yml·호스트를 기억한다(다음 launch 직전 비교 기준).
func (w *worker) rememberYaml(yml string, hosts []string) {
	w.update(func(s *runState) {
		s.Auto.Yml = yml
		s.Auto.YmlHosts = append([]string(nil), hosts...)
	})
	w.logf("등록한 yml 기억: %s (호스트 %d대)", yml, len(hosts))
}

// autoEndArg 는 stress-ng 종료 시각: now 의 날짜(now 의 시간대) + 2일 00:00 을 stress-test -end 형식 "연,월,일,시,분" 으로.
// 월·연이 넘어가는 것은 time.Date 가 맞춰 준다.
func autoEndArg(now time.Time) string {
	t := time.Date(now.Year(), now.Month(), now.Day()+2, 0, 0, 0, 0, now.Location())
	return fmt.Sprintf("%04d,%02d,%02d,00,00", t.Year(), int(t.Month()), t.Day())
}

// autoUnreached 는 상태가 완료가 아닌(접속불가·설치중 등) 호스트 수.
func (w *worker) autoUnreached() int {
	n := 0
	for i := range w.st.Hosts {
		if w.st.Hosts[i].Status != stDone {
			n++
		}
	}
	return n
}

// autoQueueable 은 CPU 대기열에 들어갈 호스트인지(체크 O, CPU X 또는 NOTRUN, 포기 아님).
func (w *worker) autoQueueable(r *hostRow) bool {
	return active(r) && w.checkOK(r.Check) && (r.CPU == cpuNotRun || r.CPU == "X")
}

// autoSyncQueue 는 대기열을 고친다: 더는 해당 없는 호스트는 빼고(순서 유지), 새로 해당되는 호스트는 뒤에 붙인다(도는 묶음은 빼고).
// update 안에서 부른다.
func (w *worker) autoSyncQueue(s *runState) {
	in := toSet(s.Auto.Batch)
	seen := map[string]bool{}
	var q []string
	add := func(h string) {
		if r := s.row(h); r != nil && !in[h] && !seen[h] && w.autoQueueable(r) {
			seen[h] = true
			q = append(q, h)
		}
	}
	for _, h := range s.Auto.Queue {
		add(h)
	}
	for i := range s.Hosts {
		add(s.Hosts[i].Host)
	}
	s.Auto.Queue = q
}

// autoNGTargets 는 stress-ng 대상(CPU O, stress-ng O 아님). 도는 묶음 안에서 먼저 CPU O 가 된 호스트도 들어간다.
func (w *worker) autoNGTargets() []string { return w.stressTargets(nil) }

// autoFixTargets 는 설정수정 대상 중 아직 3회를 다 쓰지 않은 호스트(3회째 뒤 판정에 실패해도 4회째는 하지 않는다).
func (w *worker) autoFixTargets() []string {
	var out []string
	for _, h := range w.fixTargets() {
		if r := w.st.row(h); r != nil && r.FixTries < autoMaxTries {
			out = append(out, h)
		}
	}
	return out
}

// nothingLeft 는 제한 시간 전이라도 더 할 일이 없는지(모두 완료이고 대기열·대상이 비었음).
func (a *autoRun) nothingLeft() bool {
	w := a.w
	return a.judged && w.autoUnreached() == 0 && len(w.autoFixTargets()) == 0 && len(w.st.Auto.Queue) == 0 && len(w.autoNGTargets()) == 0
}

// judge 는 판정(재체크)을 한 번 한다. 설정수정을 3회 하고도 체크가 O 가 아닌 호스트는 포기로 두고 대기열을 고친다.
func (a *autoRun) judge() bool {
	w := a.w
	w.update(func(s *runState) { s.Busy = true; s.Detail = "자동 진행 — 판정(재체크) 중" })
	jr, err := w.ex.Judge()
	if err != nil {
		msg := "판정 실패: " + errText(err)
		w.logf("%s", msg)
		w.update(func(s *runState) { s.LastError = msg; s.Busy = false })
		return false
	}
	var gave []string
	w.update(func(s *runState) {
		w.apply(s, jr)
		s.Busy = false
		for i := range s.Hosts {
			r := &s.Hosts[i]
			if active(r) && !w.checkOK(r.Check) && r.FixTries >= autoMaxTries {
				r.GaveUp = "설정수정"
				r.AutoErr = "설정체크 " + r.Check
				gave = append(gave, r.Host)
			}
		}
		w.autoSyncQueue(s)
	})
	a.judged = true
	if len(gave) > 0 {
		w.logf("설정수정 %d회 뒤에도 설정체크가 O 가 아니어서 포기 %d대: %s", autoMaxTries, len(gave), strings.Join(gave, " "))
	}
	return true
}

// fix 는 설정수정(config_check -auto <user>)을 한 번 하고 재판정한다.
func (a *autoRun) fix(hosts []string) {
	w := a.w
	if w.reload != nil {
		w.reload()
	}
	_ = writeList(w.listPath("fix.txt"), hosts)
	set := toSet(hosts)
	w.update(func(s *runState) {
		s.Busy = true
		s.Detail = fmt.Sprintf("자동 진행 — 설정수정 중: config_check %s %s (%d대)", ccFix, s.User, len(hosts))
		for i := range s.Hosts {
			if set[s.Hosts[i].Host] {
				s.Hosts[i].FixTries++
			}
		}
	})
	w.logf("설정수정 시작 %d대: %s", len(hosts), strings.Join(hosts, " "))
	if err := w.ex.ConfigFix(hosts); err != nil {
		msg := "설정수정 실패: " + errText(err)
		w.logf("%s", msg)
		w.update(func(s *runState) {
			s.LastError = msg
			for i := range s.Hosts {
				if set[s.Hosts[i].Host] {
					s.Hosts[i].AutoErr = msg
				}
			}
		})
	}
	if w.stopped() {
		return
	}
	a.judge()
}

// startStress 는 stress-test 를 띄우고 launch(감시 시작 줄) 또는 종료까지 기다린다.
// launch 됐으면 ok=true 이고, 아직 도는 중이면 ch 로 나중에 종료 결과가 온다(이미 끝났으면 ch=nil 이고 결과는 res).
// stress-test 가 "[등록]" 줄을 냈으면(launch 가 그 뒤에 실패했어도 소스는 이미 우리 yml 이므로) 그 yml·호스트를 기억한다(다음 launch 직전 yaml 확인의 기준).
func (w *worker) startStress(steps, list string, hosts []string, end, name string) (ok bool, ch chan stressRes, res stressRes) {
	launched := make(chan struct{})
	var once sync.Once
	var regMu sync.Mutex
	var regYml string
	var regHosts []string
	registered := func(yml string, hs []string) {
		regMu.Lock()
		regYml, regHosts = yml, hs
		regMu.Unlock()
	}
	remember := func() { // stress-test 가 낸 [등록] 줄의 yml·호스트를 기억
		regMu.Lock()
		yml, hs := regYml, regHosts
		regMu.Unlock()
		if yml != "" {
			w.rememberYaml(yml, hs)
		}
	}
	ch = make(chan stressRes, 1)
	go func() {
		c, e := w.ex.StressTest(steps, list, hosts, end, func() { once.Do(func() { close(launched) }) }, registered, w.busyFn(name))
		ch <- stressRes{c, e}
	}()
	select {
	case <-launched:
		remember()
		return true, ch, stressRes{}
	case res = <-ch:
		select {
		case <-launched:
			ok = true
		default:
		}
		remember()
		return ok, nil, res
	}
}

// launchFail 은 launch 되지 않은 stress-test 결과를 오류 문구로 만든다.
func launchFail(name string, r stressRes) string {
	fatal, info := stressOutcome(name, r.code, r.err)
	if fatal != "" {
		return fatal
	}
	if info != "" {
		return name + " 테스트가 launch 되지 않았습니다(" + info + ")"
	}
	return name + " 테스트가 launch 되지 않았습니다(stress-test 결과는 worker.log 참고)"
}

// noteStress 는 끝난 stress-test 결과를 로그에 남긴다(판정은 따로 하므로 기록만).
func (w *worker) noteStress(name string, r stressRes) {
	fatal, info := stressOutcome(name, r.code, r.err)
	switch {
	case fatal != "":
		w.logf("%s", fatal)
	case info != "":
		w.logf("%s", info)
	default:
		w.logf("%s stress-test 끝남", name)
	}
}

// launchCPU 는 대기열 전체를 CPU 묶음 하나로 launch 한다. CPU X 호스트는 부하가 3 미만인 것만 넣는다.
// 무엇이든 했으면 true, yaml 확인이 아직이라 아무것도 안 했으면 false.
func (a *autoRun) launchCPU() bool {
	w := a.w
	if w.yamlQ != "" { // yaml 바뀜 질문의 답을 기다리는 중: 부하 확인도 미룬다
		return false
	}
	queue := append([]string{}, w.st.Auto.Queue...)
	var xs []string
	for _, h := range queue {
		if r := w.st.row(h); r != nil && r.CPU == "X" {
			xs = append(xs, h)
		}
	}
	skip := map[string]bool{}
	if len(xs) > 0 {
		w.update(func(s *runState) {
			s.Busy = true
			s.Detail = fmt.Sprintf("자동 진행 — CPU X 호스트 %d대 부하 확인 중", len(xs))
		})
		loads, err := w.ex.Loads(xs)
		if err != nil {
			w.logf("CPU X 호스트 부하 확인 실패: %v", err)
			loads = nil
		}
		var low, high []string
		for _, h := range xs {
			if l, ok := loads[h]; ok && l < cpuRetryMaxLoad {
				low = append(low, fmt.Sprintf("%s(%.2f)", h, l))
			} else {
				skip[h] = true
				high = append(high, h)
			}
		}
		w.logf("CPU X 호스트 %d대 부하 확인: 다시 돌림(부하 %.0f 미만) %d [%s], 이번엔 건너뜀(부하 높음·응답 없음) %d [%s]",
			len(xs), cpuRetryMaxLoad, len(low), strings.Join(low, " "), len(high), strings.Join(high, " "))
	}
	var hosts []string
	for _, h := range queue {
		if !skip[h] {
			hosts = append(hosts, h)
		}
	}
	now := w.ex.Now()
	if len(hosts) == 0 {
		w.update(func(s *runState) { s.Busy = false; s.Auto.QueueWait = now.Add(w.conf.AutoCPUInt) })
		return true
	}
	if !w.autoYamlReady("CPU", hosts) {
		w.update(func(s *runState) { s.Busy = false })
		return false
	}
	if w.reload != nil {
		w.reload()
	}
	list := w.listPath("cpu.txt")
	if err := writeList(list, hosts); err != nil {
		msg := "목록 파일을 쓸 수 없습니다: " + err.Error()
		w.logf("%s", msg)
		w.update(func(s *runState) { s.Busy = false; s.LastError = msg; s.Auto.QueueWait = now.Add(w.conf.AutoCPUInt) })
		return true
	}
	set := toSet(hosts)
	w.update(func(s *runState) {
		s.Busy = true
		s.Detail = fmt.Sprintf("자동 진행 — CPU 등록·launch 중 (%d대)", len(hosts))
		for i := range s.Hosts {
			if r := &s.Hosts[i]; set[r.Host] {
				r.CPUTries++
				r.Running = prgCPURun
				r.Rebooted, r.NoResp = false, false
			}
		}
	})
	w.logf("CPU 묶음 launch 시작 %d대: %s", len(hosts), strings.Join(hosts, " "))
	ok, ch, res := w.startStress("a", list, hosts, "", "CPU")
	now = w.ex.Now()
	if ok {
		a.cpuCh = ch
		w.update(func(s *runState) {
			s.Busy = false
			s.Auto.Batch = hosts
			s.Auto.BatchLaunch = now
			s.Auto.BatchNext = now.Add(w.conf.AutoCPUWait)
			for i := range s.Hosts {
				if set[s.Hosts[i].Host] {
					s.Hosts[i].AutoErr = ""
				}
			}
			w.autoSyncQueue(s)
		})
		w.logf("CPU 묶음 launch 완료 %d대: 첫 판정 %s", len(hosts), w.st.Auto.BatchNext.Format("15:04:05"))
		if ch == nil {
			w.noteStress("CPU", res)
		}
		return true
	}
	msg := launchFail("CPU", res)
	var gave []string
	w.update(func(s *runState) {
		s.Busy = false
		s.LastError = msg
		s.Auto.QueueWait = now.Add(w.conf.AutoCPUInt)
		for i := range s.Hosts {
			if r := &s.Hosts[i]; set[r.Host] {
				r.Running = ""
				r.AutoErr = msg
				if r.CPUTries >= autoMaxTries {
					r.GaveUp = "CPU"
					gave = append(gave, r.Host)
				}
			}
		}
		w.autoSyncQueue(s)
	})
	w.logf("CPU 묶음 launch 실패(%d대): %s", len(hosts), msg)
	if len(gave) > 0 {
		w.logf("CPU %d회 시도 뒤 포기 %d대: %s", autoMaxTries, len(gave), strings.Join(gave, " "))
	}
	return true
}

// cpuJudge 는 도는 CPU 묶음을 판정한다(launch + 50분부터 5분마다, launch + 80분이 마감).
// 모두 O 이거나 마감이면 묶음을 끝내고, O 가 아닌 호스트는 실패 1회로 세어 대기열 뒤에 다시 넣는다(3회면 포기).
func (a *autoRun) cpuJudge(now time.Time) {
	w := a.w
	batch := append([]string{}, w.st.Auto.Batch...)
	var pending []string
	for _, h := range batch {
		if r := w.st.row(h); r != nil && r.CPU != "O" {
			pending = append(pending, h)
		}
	}
	var res map[string]probeInfo
	var err error
	if len(pending) > 0 {
		w.update(func(s *runState) {
			s.Busy = true
			s.Detail = fmt.Sprintf("자동 진행 — CPU 판정 중 (%d대)", len(pending))
		})
		res, err = w.ex.Probe(pending)
		if err != nil {
			w.logf("CPU 판정 확인 실패: %v", err)
		}
	}
	deadline := w.st.Auto.BatchLaunch.Add(w.conf.AutoCPUWait + w.conf.AutoCPUMax)
	final := false
	var okHosts, failed, gave []string
	left := 0
	w.update(func(s *runState) {
		s.Busy = false
		if err != nil {
			s.LastError = "CPU 판정 확인 실패: " + errText(err)
		}
		left = 0
		for _, h := range batch {
			r := s.row(h)
			if r == nil {
				continue
			}
			if r.CPU != "O" {
				if p, ok := res[h]; ok && err == nil {
					r.NoResp = false
					r.CPU = p.CPU
				} else {
					r.NoResp = true
				}
			}
			if r.CPU == "O" {
				if r.Running == prgCPURun {
					r.Running = ""
					r.AutoErr = ""
					okHosts = append(okHosts, h)
				}
			} else {
				left++
			}
		}
		if left > 0 && now.Before(deadline) {
			n := now.Add(w.conf.AutoCPUInt)
			if n.After(deadline) {
				n = deadline
			}
			s.Auto.BatchNext = n
			return
		}
		final = true
		for _, h := range batch {
			r := s.row(h)
			if r == nil || r.CPU == "O" {
				continue
			}
			r.Running = ""
			if r.NoResp {
				r.AutoErr = "CPU 판정: 응답 없음"
			} else {
				r.AutoErr = "CPU 판정: " + r.CPU
			}
			failed = append(failed, h)
			if r.CPUTries >= autoMaxTries {
				r.GaveUp = "CPU"
				gave = append(gave, h)
			}
		}
		s.Auto.Batch, s.Auto.BatchLaunch, s.Auto.BatchNext = nil, time.Time{}, time.Time{}
		w.autoSyncQueue(s)
	})
	if len(okHosts) > 0 {
		w.logf("CPU O %d대: %s", len(okHosts), strings.Join(okHosts, " "))
	}
	if !final {
		w.logf("CPU 판정: 아직 O 아님 %d대, 다음 판정 %s", left, w.st.Auto.BatchNext.Format("15:04:05"))
		return
	}
	w.logf("CPU 묶음 끝: %d대 중 O 아님 %d대 [%s]", len(batch), len(failed), strings.Join(failed, " "))
	if len(gave) > 0 {
		w.logf("CPU %d회 시도 뒤 포기 %d대: %s", autoMaxTries, len(gave), strings.Join(gave, " "))
	}
}

// launchNG 는 stress-ng 를 launch 한다(종료 시각 자동). launch 되면 바로 stress-ng O.
// 무엇이든 했으면 true, yaml 확인이 아직이라 아무것도 안 했으면 false.
func (a *autoRun) launchNG(hosts []string) bool {
	w := a.w
	if !w.autoYamlReady("stress-ng", hosts) {
		return false
	}
	if w.reload != nil {
		w.reload()
	}
	now := w.ex.Now()
	end := autoEndArg(now)
	list := w.listPath("stressng.txt")
	if err := writeList(list, hosts); err != nil {
		msg := "목록 파일을 쓸 수 없습니다: " + err.Error()
		w.logf("%s", msg)
		w.update(func(s *runState) { s.LastError = msg; s.Auto.NGWait = now.Add(w.conf.AutoCPUInt) })
		return true
	}
	set := toSet(hosts)
	w.update(func(s *runState) {
		s.Busy = true
		s.StressEnd = end
		s.Detail = fmt.Sprintf("자동 진행 — stress-ng 등록·launch 중 (%d대, 종료 시각 %s)", len(hosts), endText(end))
		for i := range s.Hosts {
			if r := &s.Hosts[i]; set[r.Host] {
				r.NGTries++
				r.Running = prgNGRun
			}
		}
	})
	w.logf("stress-ng launch 시작 %d대(종료 시각 %s): %s", len(hosts), endText(end), strings.Join(hosts, " "))
	ok, ch, res := w.startStress("b", list, hosts, end, "stress-ng")
	now = w.ex.Now()
	if ok {
		w.update(func(s *runState) {
			s.Busy = false
			for i := range s.Hosts {
				if r := &s.Hosts[i]; set[r.Host] {
					r.NGLaunched = true
					r.Stress = "O"
					r.AutoErr = ""
					if ch == nil {
						r.Running = ""
					}
				}
			}
		})
		w.logf("stress-ng launch 완료 %d대: stress-ng O 로 표시", len(hosts))
		if ch != nil {
			a.ngCh, a.ngHosts = ch, hosts
		} else {
			w.noteStress("stress-ng", res)
		}
		return true
	}
	msg := launchFail("stress-ng", res)
	var gave []string
	w.update(func(s *runState) {
		s.Busy = false
		s.LastError = msg
		s.Auto.NGWait = now.Add(w.conf.AutoCPUInt)
		for i := range s.Hosts {
			if r := &s.Hosts[i]; set[r.Host] {
				r.Running = ""
				r.AutoErr = msg
				if r.NGTries >= autoMaxTries {
					r.GaveUp = "stress-ng"
					gave = append(gave, r.Host)
				}
			}
		}
	})
	w.logf("stress-ng launch 실패(%d대): %s", len(hosts), msg)
	if len(gave) > 0 {
		w.logf("stress-ng %d회 시도 뒤 포기 %d대: %s", autoMaxTries, len(gave), strings.Join(gave, " "))
	}
	return true
}

// reap 은 뒤에서 돌던 stress-test 가 끝났으면 거둔다.
func (a *autoRun) reap() {
	if a.cpuCh != nil {
		select {
		case r := <-a.cpuCh:
			a.cpuCh = nil
			a.w.noteStress("CPU", r)
		default:
		}
	}
	if a.ngCh != nil {
		select {
		case r := <-a.ngCh:
			a.ngCh = nil
			a.w.noteStress("stress-ng", r)
			a.w.clearRunning(a.ngHosts)
			a.ngHosts = nil
		default:
		}
	}
}

// drain 은 중지할 때 뒤에서 도는 stress-test 가 (취소되어) 끝나기를 기다린다.
func (a *autoRun) drain() {
	if a.cpuCh != nil {
		a.w.noteStress("CPU", <-a.cpuCh)
		a.cpuCh = nil
	}
	if a.ngCh != nil {
		a.w.noteStress("stress-ng", <-a.ngCh)
		a.ngCh = nil
	}
}

// wait 은 다음 할 일 시각(CPU 판정·재판정·대기열·제한 시간)까지 쉰다. stress-test 가 뒤에서 돌면 autoPoll 마다 깬다.
func (a *autoRun) wait(now time.Time) {
	w := a.w
	au := w.st.Auto
	next := now.Add(autoIdleMax)
	if a.cpuCh != nil || a.ngCh != nil {
		next = now.Add(autoPoll)
	}
	if w.yamlQ != "" { // 답이 오면 바로 이어 가려고 자주 깬다
		next = now.Add(autoPoll)
	}
	soon := func(t time.Time) {
		if t.After(now) && t.Before(next) {
			next = t
		}
	}
	if len(au.Batch) > 0 {
		soon(au.BatchNext)
	}
	if !au.Ending {
		soon(au.Start.Add(w.conf.AutoLimit))
		if !a.judged || w.autoUnreached() > 0 {
			soon(au.NextRecheck)
		}
		if len(au.Queue) > 0 {
			soon(au.QueueWait)
		}
		soon(au.NGWait)
	}
	detail := a.detail(now)
	w.update(func(s *runState) { s.Busy = false; s.Detail = detail })
	w.ex.Sleep(next.Sub(now))
}

// detail 은 쉬는 동안의 진행 설명(한 줄).
func (a *autoRun) detail(now time.Time) string {
	w := a.w
	au := w.st.Auto
	var p []string
	if au.Ending {
		p = append(p, "자동 진행 — 제한 시간 지남, 진행 중인 것만 마치는 중")
	} else {
		p = append(p, "자동 진행 — 남은 시간 "+fmtDur(au.Start.Add(w.conf.AutoLimit).Sub(now)))
	}
	if len(au.Batch) > 0 {
		p = append(p, fmt.Sprintf("CPU 묶음 %d대 다음 판정 %s", len(au.Batch), au.BatchNext.Format("15:04")))
	}
	if len(au.Queue) > 0 {
		p = append(p, fmt.Sprintf("CPU 대기열 %d대", len(au.Queue)))
	}
	if a.ngCh != nil {
		p = append(p, fmt.Sprintf("stress-ng %d대 stress-test 진행 중", len(a.ngHosts)))
	}
	if w.yamlQ != "" {
		p = append(p, "yaml 바뀜 질문 답 기다리는 중(launch 보류)")
	}
	if n := w.autoUnreached(); n > 0 && !au.Ending {
		p = append(p, fmt.Sprintf("미완료 %d대 다음 재판정 %s", n, au.NextRecheck.Format("15:04")))
	}
	return strings.Join(p, ", ")
}

// finish 는 결과를 기록하고 작업을 끝낸다(완료 단계).
func (a *autoRun) finish() {
	w := a.w
	w.yamlQ = ""
	all, left, gave := 0, 0, 0
	for i := range w.st.Hosts {
		r := &w.st.Hosts[i]
		switch {
		case r.GaveUp != "":
			gave++
		case r.Status == stDone && w.checkOK(r.Check) && r.CPU == "O" && r.Stress == "O":
			all++
		default:
			left++
		}
	}
	why := "할 일이 끝남"
	if w.st.Auto.Ending {
		why = "제한 시간 지남"
	}
	w.update(func(s *runState) {
		s.Stage = stageFinish
		s.Busy = false
		s.Question = nil
		s.Detail = fmt.Sprintf("자동 진행 끝(%s): 모두 O %d대, 남음 %d대, 포기 %d대", why, all, left, gave)
	})
	w.saveResult()
}
