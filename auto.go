package main

// -run -auto 스케줄러(계획서_auto.md, auto_flow.svg). 질문 없이 모든 호스트를 완료 | 설정체크 O | CPU O | stress-ng O 로 만든다.
// 사람이 답하는 질문은 인벤토리 사용 중(r/x)과 (3단계) yaml 바뀜 둘뿐이다. 둘 다 state 에만 걸어 두고 스케줄러가 반복 때마다 답을 확인한다
// (질문이 떠 있어도 스케줄러는 멈추지 않고, 답을 기다리는 launch 만 보류한다. 계획서_auto_막힘.md 4.3). 질문은 한 번에 하나만 띄운다.
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
// 단 launch 중 인벤토리 사용 중 질문이 뜨면 그 launch 만 보류하고(autoLaunch) 스케줄러는 계속 돈다.

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

// autoEnvWaitMax 는 연속 환경 실패 때 재시도 간격의 상한(기본 간격 AUTO_CPU_INT 5분에서 2배씩 늘어 5→10→20→30분). 시험에서 바꾼다.
var autoEnvWaitMax = 30 * time.Minute

// autoPoll 은 stress-test 가 뒤에서 도는 동안 끝났는지 확인하는 간격.
var autoPoll = 5 * time.Second

// autoIdleMax 는 할 일이 없을 때도 깨어나 진행 설명(남은 시간)을 갱신하는 최대 간격.
var autoIdleMax = time.Minute

// autoRun 은 -auto 한 번 실행 동안의 메모리 상태(뒤에서 도는 stress-test).
type autoRun struct {
	w       *worker
	judged  bool           // 이번 실행에서 판정에 한 번이라도 성공함
	jfail   bool           // 마지막 판정이 실패함(성공할 때까지 AUTO_RECHECK 마다 다시 판정하고, 설정수정은 하지 않는다)
	cpuCh   chan stressRes // launch 뒤 아직 안 끝난 CPU stress-test(없으면 nil)
	ngCh    chan stressRes // launch 뒤 아직 안 끝난 stress-ng stress-test(없으면 nil)
	ngHosts []string
	ngL     *autoLaunch   // ngCh 의 launch(이벤트 [결과] 를 읽는다). 이벤트를 모르는 실행기면 nil
	pend    []*autoLaunch // 인벤토리 사용 중 질문의 답을 기다려 보류된 launch(들어온 차례, 종류별 최대 하나). 메모리에만 있다(재기동하면 없음)
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
			// 재기동 복구: launch 는 받아들여졌는데 감시 결과를 받기 전에 워커가 죽었다면 뒤 프로세스를 놓친 것이므로 launch 된 것으로 보고 O 로 둔다(옛 동작).
			// launch 도중에 죽은 호스트(NGAccepted 아님)는 stress-ng 대상으로 남는다.
			if r := &s.Hosts[i]; r.Running == prgNGRun || r.NGAccepted {
				r.Running = ""
				if r.NGAccepted {
					r.NGAccepted, r.NGLaunched, r.Stress = false, true, "O"
				}
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
		a.pollBusy() // 인벤토리 사용 중 질문의 답(받으면 보류된 launch 를 이어 간다)
		a.reap()
		now := w.ex.Now()
		au := w.st.Auto
		if !au.Ending && !now.Before(au.Start.Add(w.conf.AutoLimit)) {
			w.update(func(s *runState) { s.Auto.Ending = true })
			w.logf("제한 시간(%s)이 지났습니다: 새 작업은 시작하지 않고 진행 중인 것만 마칩니다", w.conf.AutoLimit)
			w.cancelYamlQ("제한 시간이 지나 새 launch 를 하지 않으므로 질문을 취소합니다")
			continue
		}
		if au.Ending && len(a.pend) > 0 { // 보류된 launch 는 x 로 끝낸다(stress-test 가 그 묶음을 중단)
			a.cancelBusy("제한 시간이 지나 새 launch 를 하지 않으므로 x(이 묶음 중단)로 답합니다")
			continue
		}
		if len(au.Batch) > 0 && !now.Before(au.BatchNext) { // ④ 이미 launch 한 묶음 판정은 제한 시간 뒤에도 한다
			a.cpuJudge(now)
			continue
		}
		if !au.Ending {
			if needJudge || ((!a.judged || a.jfail || w.autoUnreached() > 0) && !now.Before(au.NextRecheck)) { // ① ⑥
				needJudge = false
				a.judge()
				w.update(func(s *runState) { s.Auto.NextRecheck = w.ex.Now().Add(w.conf.AutoRecheck) })
				continue
			}
			if hosts := w.autoFixTargets(); len(hosts) > 0 && !a.jfail { // ② (판정이 실패한 동안은 상태가 낡았으므로 시도를 쓰지 않는다)
				a.fix(hosts)
				continue
			}
			if a.ngCh == nil && !a.held("stress-ng") && !now.Before(au.NGWait) { // ⑤
				if hosts := w.autoNGTargets(); len(hosts) > 0 && a.launchNG(hosts) {
					continue
				}
			}
			if len(au.Batch) == 0 && a.cpuCh == nil && !a.held("CPU") && len(au.Queue) > 0 && !now.Before(au.QueueWait) { // ③
				if a.launchCPU() {
					continue
				}
			}
		}
		idle := len(w.st.Auto.Batch) == 0 && a.cpuCh == nil && a.ngCh == nil && len(a.pend) == 0
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
	return active(r) && !r.VendorX && w.checkOK(r.Check) && (r.CPU == cpuNotRun || r.CPU == "X")
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
// 실행 확인 중(prgNGRun)인 호스트는 다시 고르지 않는다.
func (w *worker) autoNGTargets() []string {
	var out []string
	for _, h := range w.stressTargets(nil) {
		if r := w.st.row(h); r != nil && r.Running != prgNGRun {
			out = append(out, h)
		}
	}
	return out
}

// autoFixTargets 는 지금 설정수정할 호스트: autoFixPending 중 환경 실패 대기(FixWait)가 끝난 경우만.
func (w *worker) autoFixTargets() []string {
	if w.ex.Now().Before(w.st.Auto.FixWait) {
		return nil
	}
	return w.autoFixPending()
}

// autoFixPending 은 설정수정 대상 중 아직 3회를 다 쓰지 않은 호스트(3회째 뒤 판정에 실패해도 4회째는 하지 않는다). 대기 시각은 보지 않는다.
func (w *worker) autoFixPending() []string {
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
	return a.judged && !a.jfail && w.autoUnreached() == 0 && len(w.autoFixPending()) == 0 && len(w.st.Auto.Queue) == 0 && len(w.autoNGTargets()) == 0
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
		a.jfail = true
		return false
	}
	var gave, vendor []string
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
			if active(r) && r.VendorX && r.CPU != "O" && w.checkOK(r.Check) { // CPU 벤더가 Intel/AMD 가 아니면 CPU 는 늘 X: 80분×3회를 쓰지 않고 바로 포기
				r.GaveUp = "CPU"
				r.AutoErr = "CPU 벤더 미인식"
				vendor = append(vendor, r.Host)
			}
		}
		w.autoSyncQueue(s)
	})
	a.judged, a.jfail = true, false
	if len(vendor) > 0 {
		w.logf("CPU 벤더가 Intel/AMD 가 아니어서 CPU 테스트 없이 포기 %d대: %s", len(vendor), strings.Join(vendor, " "))
	}
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
	// 명령 오류·시간초과면 환경 실패(시도에 세지 않고 간격을 두고 다시), 명령이 끝났는데 O 가 아니면 재판정 뒤 호스트 실패(시도로 센다).
	if err := w.ex.ConfigFix(hosts); err != nil {
		msg := "설정수정 실패: " + errText(err)
		var wait time.Duration
		now := w.ex.Now()
		w.update(func(s *runState) {
			s.LastError = msg
			wait = w.envFailLocked(s, set, msg, func(r *hostRow) *int { return &r.FixTries })
			s.Auto.FixWait = now.Add(wait)
		})
		w.logf("환경 실패(시도 안 셈): 설정수정 %d대 — %s / 연속 %d회, 다음 재시도 %s(%s 뒤)", len(hosts), msg, w.st.Auto.EnvFail, now.Add(wait).Format("15:04:05"), fmtDur(wait))
	} else {
		w.update(func(s *runState) { envOKLocked(s, set) })
	}
	if w.stopped() {
		return
	}
	a.judge()
}

// autoLaunch 는 -auto 에서 띄운 stress-test 한 번의 launch 다(계획서_auto_막힘.md 4.3).
// stress-test 가 "[인벤토리사용중]" 줄을 내면 busy 콜백(출력 감시 고루틴에서 불림)은 answer.json 을 직접 보지 않는다.
// 메시지를 busyCh 로 스케줄러에 넘기고 ansCh 의 답(r / x)을 기다린다. 그동안 STRESS_TIMEOUT 은 멈춰 있다(watchStress 의 hold).
// 스케줄러는 이 launch 만 보류하고(autoRun.pend) 다른 일을 계속하며, 반복 때마다 답을 확인해(pollBusy) ansCh 로 넘긴다.
type autoLaunch struct {
	name     string // "CPU" / "stress-ng"
	steps    string // "a" / "b"
	hosts    []string
	launched chan struct{}  // [감시] 줄이 오면 닫힘
	ch       chan stressRes // stress-test 종료 결과(버퍼 1)
	busyCh   chan string    // 콜백 → 스케줄러: 인벤토리 사용 중 메시지(버퍼 1)
	ansCh    chan string    // 스케줄러 → 콜백: r / x(버퍼 1)
	hasEv    bool
	evs      stressEvents
	regMu    sync.Mutex
	regYml   string
	regHosts []string

	msg     string // 답을 기다리는 인벤토리 사용 중 메시지(보류 중일 때)
	qid     string // 상태에 건 질문 id("" = 아직 묻지 않음: 다른 질문이 떠 있어 차례를 기다린다)
	aborted bool   // x 로 답함(사람·제한 시간·중지). launch 되지 않으면 환경 실패로 센다(시도 안 셈)
}

// startLaunch 는 stress-test 를 뒤에서 띄우고 바로 돌아온다. 결과는 await 로 받는다.
func (w *worker) startLaunch(steps, list string, hosts []string, end, name string) *autoLaunch {
	l := &autoLaunch{name: name, steps: steps, hosts: hosts, launched: make(chan struct{}),
		ch: make(chan stressRes, 1), busyCh: make(chan string, 1), ansCh: make(chan string, 1)}
	var once sync.Once
	launched := func() { once.Do(func() { close(l.launched) }) }
	registered := func(yml string, hs []string) {
		l.regMu.Lock()
		l.regYml, l.regHosts = yml, hs
		l.regMu.Unlock()
	}
	// busy 는 기다리기만 한다: 답은 스케줄러가 준다. 중지 요청(stopCh)이 오면 답 없이 x 로 끝낸다(고루틴이 남지 않게).
	busy := func(msg string) string {
		select {
		case l.busyCh <- msg:
		case <-w.stopCh:
			return ansExclude
		}
		select {
		case c := <-l.ansCh:
			return c
		case <-w.stopCh:
			return ansExclude
		}
	}
	es, hasEv := w.ex.(eventStresser)
	l.hasEv = hasEv
	go func() {
		var c int
		var e error
		if hasEv {
			c, e = es.StressTestEv(steps, list, hosts, end, launched, registered, busy, l.evs.Add)
		} else {
			c, e = w.ex.StressTest(steps, list, hosts, end, launched, registered, busy)
		}
		l.ch <- stressRes{c, e}
	}()
	return l
}

// await 는 launch(감시 시작 줄)·종료·인벤토리 사용 중 질문 가운데 먼저 오는 것까지 기다린다.
// 질문이면 held=true 이고 메시지는 l.msg 에 둔다(보류). 그 밖에는 launch 됐으면 ok=true 이고, 아직 도는 중이면 ch 로 나중에 종료 결과가 온다
// (이미 끝났으면 ch=nil 이고 결과는 res). stress-test 가 "[등록]" 줄을 냈으면(launch 가 그 뒤에 실패했어도 소스는 이미 우리 yml 이므로)
// 그 yml·호스트를 기억한다(다음 launch 직전 yaml 확인의 기준). env 는 launch 되지 않았을 때 그 실패가 환경 실패인지(launchEnvFail, x 로 답함)다.
func (w *worker) await(l *autoLaunch) (held, ok bool, ch chan stressRes, res stressRes, env bool) {
	remember := func() {
		l.regMu.Lock()
		yml, hs := l.regYml, l.regHosts
		l.regMu.Unlock()
		if yml != "" {
			w.rememberYaml(yml, hs)
		}
	}
	select {
	case <-l.launched:
		remember()
		return false, true, l.ch, stressRes{}, false
	case res = <-l.ch:
		select {
		case <-l.launched:
			ok = true
		default:
		}
		remember()
		if !ok {
			env = l.aborted || launchEnvFail(l.hasEv, l.evs.Stage(l.steps), res)
		}
		return false, ok, nil, res, env
	case l.msg = <-l.busyCh:
		return true, false, nil, stressRes{}, false
	}
}

// advance 는 launch 를 다음 고비까지 이어 간다: 질문이면 보류 목록에 넣고, 아니면 launch 결과를 기록한다(settleCPU / settleNG).
func (a *autoRun) advance(l *autoLaunch) {
	held, ok, ch, res, env := a.w.await(l)
	if held {
		a.pend = append(a.pend, l)
		a.w.update(func(s *runState) { s.Busy = false })
		a.w.logf("%s launch 보류(%d대): 템플릿 인벤토리 사용 중 — 답을 기다리는 동안 다른 일은 계속합니다", l.name, len(l.hosts))
		a.askBusy()
		return
	}
	if l.name == "CPU" {
		a.settleCPU(l, ok, ch, res, env)
	} else {
		a.settleNG(l, ok, ch, res, env)
	}
}

// held 는 name 종류의 launch 가 질문 답을 기다려 보류 중인지.
func (a *autoRun) held(name string) bool {
	for _, l := range a.pend {
		if l.name == name {
			return true
		}
	}
	return false
}

// askBusy 는 보류된 launch 중 아직 묻지 않은 첫 것의 질문을 state 에 건다(기다리지 않는다).
// 다른 질문(인벤토리 사용 중·yaml 바뀜)이 떠 있으면 그것이 끝날 때까지 미룬다(질문은 한 번에 하나).
func (a *autoRun) askBusy() {
	w := a.w
	if w.yamlQ != "" || w.st.Question != nil {
		return
	}
	var l *autoLaunch
	for _, p := range a.pend {
		if p.qid != "" {
			return
		}
		if l == nil {
			l = p
		}
	}
	if l == nil {
		return
	}
	os.Remove(filepath.Join(w.dir, answerFile)) // 지난 답이 남아 있으면 버림
	var id string
	w.update(func(s *runState) {
		s.QSeq++
		id = fmt.Sprintf("q%d", s.QSeq)
		s.Question = busyQuestion(id, s.Stage, l.name, l.msg, w.ex.Now())
	})
	l.qid = id
	w.logf("질문 %s (r/x, %s 인벤토리 사용 중, launch 보류): %s", id, l.name, strings.ReplaceAll(l.msg, "\n", " / "))
}

// pollBusy 는 떠 있는 인벤토리 사용 중 질문의 답을 한 번 확인한다(기다리지 않는다). 다른 id·선택지의 답은 무시한다.
// 떠 있는 질문이 없으면 차례를 기다리던 질문을 건다.
func (a *autoRun) pollBusy() {
	w := a.w
	var l *autoLaunch
	for _, p := range a.pend {
		if p.qid != "" {
			l = p
			break
		}
	}
	if l == nil {
		a.askBusy()
		return
	}
	ans := takeAnswer(w.dir)
	if ans == nil {
		return
	}
	if ans.ID != l.qid || (ans.Choice != ansRetry && ans.Choice != ansExclude) {
		w.logf("무시한 답: id=%s choice=%s (기다리는 질문 %s)", ans.ID, ans.Choice, l.qid)
		return
	}
	w.logf("답 %s: %s", l.qid, ans.Choice)
	a.reply(l, ans.Choice)
}

// reply 는 보류된 launch 에 답(r / x)을 넘기고 질문을 내린 뒤, 그 launch 를 다음 고비(launch·종료·다시 질문)까지 이어 간다.
// x 면 stress-test 가 그 묶음을 중단하고 끝나며, 환경 실패로 센다(시도 안 셈, 간격을 두고 다시).
func (a *autoRun) reply(l *autoLaunch, c string) {
	w := a.w
	id := l.qid
	for i, p := range a.pend {
		if p == l {
			a.pend = append(a.pend[:i:i], a.pend[i+1:]...)
			break
		}
	}
	l.qid, l.msg = "", ""
	if c != ansRetry {
		c, l.aborted = ansExclude, true
	}
	what := "다시 확인 중"
	if l.aborted {
		what = "중단 중"
	}
	w.update(func(s *runState) {
		if id != "" && s.Question != nil && s.Question.ID == id {
			s.Question = nil
		}
		s.Busy = true
		s.Detail = fmt.Sprintf("자동 진행 — %s 인벤토리 사용 중 질문에 %s: launch %s", l.name, c, what)
	})
	l.ansCh <- c // 버퍼 1: 콜백이 이미 중지로 끝났어도 막히지 않는다
	a.advance(l)
	a.askBusy()
}

// cancelBusy 는 보류된 launch 를 모두 x 로 끝낸다(제한 시간이 지남). 떠 있던 질문은 취소로 기록한다.
func (a *autoRun) cancelBusy(why string) {
	for _, l := range append([]*autoLaunch(nil), a.pend...) {
		if l.qid != "" {
			a.w.logf("질문 %s 취소: %s", l.qid, why)
		} else {
			a.w.logf("%s 인벤토리 사용 중 질문(묻기 전) 취소: %s", l.name, why)
		}
		a.reply(l, ansExclude)
	}
}

// envWait 는 연속 환경 실패 n 회째(1 부터)의 재시도 간격: base 에서 2배씩 늘리되 autoEnvWaitMax 를 넘지 않는다.
func envWait(base time.Duration, n int) time.Duration {
	d := base
	for i := 1; i < n && d < autoEnvWaitMax; i++ {
		d *= 2
	}
	if d > autoEnvWaitMax {
		d = autoEnvWaitMax
	}
	return d
}

// envFailLocked 는 환경 실패(호스트와 상관없는 실패: AWX·ssh·시간초과·인벤토리 불일치 등)를 기록하고 다음 재시도 간격을 돌려준다.
// 시도에 세지 않으므로 호출 앞에서 올려 둔 시도 횟수(tries 가 고르는 필드)를 되돌린다. update 안에서 부른다.
func (w *worker) envFailLocked(s *runState, set map[string]bool, reason string, tries func(r *hostRow) *int) time.Duration {
	s.Auto.EnvFail++
	s.Auto.EnvReason = reason
	msg := fmt.Sprintf("환경 실패 %d회: %s", s.Auto.EnvFail, reason)
	for i := range s.Hosts {
		r := &s.Hosts[i]
		if !set[r.Host] {
			continue
		}
		if t := tries(r); *t > 0 {
			*t--
		}
		r.EnvFails++
		r.AutoErr = msg
	}
	return envWait(w.conf.AutoCPUInt, s.Auto.EnvFail)
}

// envOKLocked 는 launch·설정수정이 성공했을 때 환경 실패 횟수와 재시도 대기를 처음(기본 간격)으로 되돌린다. update 안에서 부른다.
func envOKLocked(s *runState, set map[string]bool) {
	s.Auto.EnvFail, s.Auto.EnvReason, s.Auto.FixWait = 0, "", time.Time{}
	for i := range s.Hosts {
		if set[s.Hosts[i].Host] {
			s.Hosts[i].EnvFails = 0
		}
	}
}

// launchEnvFail 는 launch 되지 않은 stress-test 가 환경 실패인지 가른다(실패 분류, 계획서 4.2).
//   - 이벤트를 받는 실행기(eventStresser): [job] 줄이 나오기 전에 끝났으면 환경 실패, 나온 뒤면 호스트 실패.
//     옛 stress-test 는 [job] 줄을 내지 않으므로 launch 실패는 모두 환경 실패로 본다.
//   - 이벤트를 모르는 실행기(시험용 가짜 등): 실행 자체의 오류(err: ssh 실패·시간초과)나 종료코드 2(중지)면 환경 실패,
//     종료코드 1 등 그 밖은 호스트 실패로 본다.
func launchEnvFail(hasEv bool, ev stageEvents, r stressRes) bool {
	if hasEv {
		return !ev.JobSeen
	}
	return r.err != nil || r.code == 2
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
	if len(xs) > 0 { // 늦게 끝난 CPU 테스트: 부하를 보기 전에 CPU 열을 다시 읽어 O 가 된 호스트는 대기열에서 빼고 stress-ng 대상으로 넘긴다
		if late := a.lateCPU(xs); len(late) > 0 {
			gone := toSet(late)
			queue, xs = withoutSet(queue, gone), withoutSet(xs, gone)
		}
	}
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
		w.update(func(s *runState) {
			for _, h := range xs {
				if l, ok := loads[h]; ok {
					if r := s.row(h); r != nil {
						r.LastLoad = l
					}
				}
			}
		})
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
	a.advance(w.startLaunch("a", list, hosts, "", "CPU"))
	return true
}

// withoutSet 은 l 에서 gone 에 든 것을 뺀 새 목록(순서 유지).
func withoutSet(l []string, gone map[string]bool) []string {
	var out []string
	for _, h := range l {
		if !gone[h] {
			out = append(out, h)
		}
	}
	return out
}

// lateCPU 는 CPU 대기열의 CPU X 호스트 xs 를 Probe 로 다시 읽어, CPU 열이 O 가 된 호스트(앞 묶음이 늦게 끝남)를 O 로 고치고
// 대기열에서 뺀 뒤 돌려준다(그 호스트는 stress-ng 대상이 된다). Probe 가 실패하면 기록만 하고 지금까지처럼 진행한다.
func (a *autoRun) lateCPU(xs []string) []string {
	w := a.w
	w.update(func(s *runState) {
		s.Busy = true
		s.Detail = fmt.Sprintf("자동 진행 — CPU X 호스트 %d대 CPU 열 다시 확인 중", len(xs))
	})
	res, err := w.ex.Probe(xs)
	if err != nil {
		w.logf("CPU X 호스트 CPU 열 확인 실패(그대로 진행): %v", err)
		return nil
	}
	var late []string
	w.update(func(s *runState) {
		for _, h := range xs {
			if p, ok := res[h]; ok && p.CPU == "O" {
				if r := s.row(h); r != nil {
					r.CPU, r.CPURetry, r.NoResp, r.AutoErr, r.LastLoad = "O", false, false, "", 0
					late = append(late, h)
				}
			}
		}
		w.autoSyncQueue(s)
	})
	if len(late) > 0 {
		w.logf("대기열의 CPU X 호스트 중 CPU 열이 O 로 바뀐 %d대는 CPU 를 다시 돌리지 않고 stress-ng 대상으로 넘김: %s", len(late), strings.Join(late, " "))
	}
	return late
}

// excludedBack 은 launch 된 stress-test 가 [제외] 줄로 뺀 호스트(접속체크 실패)를 launch 한 묶음에서 가려낸다(l.hasEv 가 아니면 없음).
// launched = 실제 launch 된 호스트, excl = 빠진 호스트(둘 다 l.hosts 순서).
func (l *autoLaunch) excludedBack() (launched, excl []string) {
	if !l.hasEv {
		return l.hosts, nil
	}
	ex := toSet(l.evs.Stage(l.steps).Excluded)
	for _, h := range l.hosts {
		if ex[h] {
			excl = append(excl, h)
		} else {
			launched = append(launched, h)
		}
	}
	return launched, excl
}

// excludedReason 은 stress-test 가 launch 에서 뺀 호스트의 환경 실패 사유다.
const excludedReason = "stress-test 접속불가"

// settleCPU 는 CPU launch 결과를 기록한다: launch 됐으면 묶음과 판정 시간표, 아니면 환경 실패(시도 안 셈) 또는 호스트 실패(시도 1회).
//
// 인벤토리 사용 중 질문으로 보류됐던 launch 는 답을 받은 뒤 [감시] 줄·종료가 올 때 여기로 온다.
func (a *autoRun) settleCPU(l *autoLaunch, ok bool, ch chan stressRes, res stressRes, env bool) {
	w := a.w
	hosts := l.hosts
	set := toSet(hosts)
	now := w.ex.Now()
	if ok {
		a.cpuCh = ch
		launched, excl := l.excludedBack() // stress-test 가 접속체크로 뺀 호스트는 묶음에 넣지 않고 대기열 뒤로 보낸다(환경 실패, 시도 안 셈)
		set = toSet(launched)
		var wait time.Duration
		w.update(func(s *runState) {
			s.Busy = false
			if len(launched) > 0 {
				s.Auto.Batch = launched
				s.Auto.BatchLaunch = now
				s.Auto.BatchNext = now.Add(w.conf.AutoCPUWait)
			}
			for i := range s.Hosts {
				if set[s.Hosts[i].Host] {
					s.Hosts[i].AutoErr = ""
				}
			}
			envOKLocked(s, set)
			if len(excl) > 0 {
				ex := toSet(excl)
				for i := range s.Hosts {
					if ex[s.Hosts[i].Host] {
						s.Hosts[i].Running = ""
					}
				}
				wait = w.envFailLocked(s, ex, excludedReason, func(r *hostRow) *int { return &r.CPUTries })
				s.Auto.QueueWait = now.Add(wait)
			}
			w.autoSyncQueue(s)
			if len(excl) > 0 { // 빠진 호스트는 대기열 맨 뒤로
				ex := toSet(excl)
				q := withoutSet(s.Auto.Queue, ex)
				for _, h := range s.Auto.Queue {
					if ex[h] {
						q = append(q, h)
					}
				}
				s.Auto.Queue = q
			}
		})
		if len(excl) > 0 {
			w.logf("CPU launch 에서 stress-test 가 접속불가로 뺀 %d대는 묶음에 넣지 않고 대기열 뒤로(환경 실패, 시도 안 셈, 다음 시도 %s): %s", len(excl), now.Add(wait).Format("15:04:05"), strings.Join(excl, " "))
		}
		w.logf("CPU 묶음 launch 완료 %d대: 첫 판정 %s", len(launched), w.st.Auto.BatchNext.Format("15:04:05"))
		if ch == nil {
			w.noteStress("CPU", res)
		}
		return
	}
	msg := launchFail("CPU", res)
	if l.aborted {
		msg = abortMsg("CPU", msg)
	}
	if env {
		var wait time.Duration
		w.update(func(s *runState) {
			s.Busy = false
			s.LastError = msg
			for i := range s.Hosts {
				if set[s.Hosts[i].Host] {
					s.Hosts[i].Running = ""
				}
			}
			wait = w.envFailLocked(s, set, msg, func(r *hostRow) *int { return &r.CPUTries })
			s.Auto.QueueWait = now.Add(wait)
			w.autoSyncQueue(s)
		})
		w.logf("환경 실패(시도 안 셈): CPU launch %d대 — %s / 연속 %d회, 다음 재시도 %s(%s 뒤)", len(hosts), msg, w.st.Auto.EnvFail, now.Add(wait).Format("15:04:05"), fmtDur(wait))
		return
	}
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
	return
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

// launchNG 는 stress-ng 를 launch 한다(종료 시각 자동). launch 가 받아들여져도 바로 O 가 아니라 감시가 끝나 [결과] 를 받은 뒤 O(settleNG·endNG).
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
	a.advance(w.startLaunch("b", list, hosts, end, "stress-ng"))
	return true
}

// settleNG 는 stress-ng launch 결과를 기록한다: launch 됐으면 모드에 따라 O 로 두거나 실행 확인을 기다리고, 아니면 환경 실패(시도 안 셈) 또는 호스트 실패(시도 1회).
//   - 이벤트를 받는 실행기(eventStresser): launch 된 호스트는 O 가 아니라 "실행 확인 중"(Running=prgNGRun, NGAccepted)으로 두고,
//     stress-test 가 끝날 때의 [결과] 줄로 endNG 가 가른다(done·nomon → O, notrun·어디에도 없음 → 호스트 실패 1회).
//   - 대체(fallback): [결과] 줄이 없으면(옛 stress-test, 이벤트를 모르는 실행기, 워커 재기동으로 뒤 프로세스를 놓침) 예전처럼 launch 된 호스트를 O 로 둔다.
//     이벤트를 모르는 실행기는 launch 를 받아들인 때 바로, 이벤트를 아는데 끝에 [결과] 가 없으면 stress-test 가 끝난 때 O 로 둔다(영원히 "실행 확인 중" 으로 남지 않게).
//
// 인벤토리 사용 중 질문으로 보류됐던 launch 는 답을 받은 뒤 [감시] 줄·종료가 올 때 여기로 온다.
func (a *autoRun) settleNG(l *autoLaunch, ok bool, ch chan stressRes, res stressRes, env bool) {
	w := a.w
	hosts := l.hosts
	set := toSet(hosts)
	now := w.ex.Now()
	if ok {
		launched, excl := l.excludedBack() // stress-test 가 접속체크로 뺀 호스트는 launch 된 것으로 보지 않는다(환경 실패, 시도 안 셈)
		set = toSet(launched)
		var wait time.Duration
		w.update(func(s *runState) {
			s.Busy = false
			for i := range s.Hosts {
				if r := &s.Hosts[i]; set[r.Host] {
					r.AutoErr = ""
					if l.hasEv { // 실행 확인 중: 감시가 끝나 [결과] 를 받을 때까지 O 가 아니다
						r.NGAccepted = true
						r.Running = prgNGRun
					} else { // 이벤트를 모르는 실행기: 예전처럼 launch 되면 바로 O
						r.NGLaunched = true
						r.Stress = "O"
						if ch == nil {
							r.Running = ""
						}
					}
				}
			}
			envOKLocked(s, set)
			if len(excl) > 0 {
				ex := toSet(excl)
				for i := range s.Hosts {
					if ex[s.Hosts[i].Host] {
						s.Hosts[i].Running = ""
					}
				}
				wait = w.envFailLocked(s, ex, excludedReason, func(r *hostRow) *int { return &r.NGTries })
				s.Auto.NGWait = now.Add(wait)
			}
		})
		if len(excl) > 0 {
			w.logf("stress-ng launch 에서 stress-test 가 접속불가로 뺀 %d대는 launch 된 것으로 보지 않음(환경 실패, 시도 안 셈, 다음 시도 %s): %s", len(excl), now.Add(wait).Format("15:04:05"), strings.Join(excl, " "))
		}
		if l.hasEv {
			w.logf("stress-ng launch 완료 %d대: 실행 확인 중(stress-test 감시가 끝나 [결과] 를 받으면 O 로 표시)", len(launched))
		} else {
			w.logf("stress-ng launch 완료 %d대: stress-ng O 로 표시", len(launched))
		}
		if ch != nil {
			a.ngCh, a.ngHosts = ch, launched
			if l.hasEv {
				a.ngL = l
			}
		} else {
			w.noteStress("stress-ng", res)
			if l.hasEv {
				a.endNG(l, launched)
			}
		}
		return
	}
	msg := launchFail("stress-ng", res)
	if l.aborted {
		msg = abortMsg("stress-ng", msg)
	}
	if env {
		var wait time.Duration
		w.update(func(s *runState) {
			s.Busy = false
			s.LastError = msg
			for i := range s.Hosts {
				if set[s.Hosts[i].Host] {
					s.Hosts[i].Running = ""
				}
			}
			wait = w.envFailLocked(s, set, msg, func(r *hostRow) *int { return &r.NGTries })
			s.Auto.NGWait = now.Add(wait)
		})
		w.logf("환경 실패(시도 안 셈): stress-ng launch %d대 — %s / 연속 %d회, 다음 재시도 %s(%s 뒤)", len(hosts), msg, w.st.Auto.EnvFail, now.Add(wait).Format("15:04:05"), fmtDur(wait))
		return
	}
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
	return
}

// endNG 는 stress-ng stress-test 가 끝났을 때 "실행 확인 중" 호스트를 가른다([결과] 줄 기준, settleNG 설명 참고).
//   - [결과] 가 있으면: done → O, nomon → O + 비고 "감시 제외", notrun 과 어느 목록에도 없는 호스트 → 호스트 실패 1회(시도는 launch 때 이미 셈, 3회면 포기, 다음 launch 는 NGWait 뒤).
//   - [결과] 가 없으면: launch 가 받아들여졌으므로 모두 O(옛 동작).
func (a *autoRun) endNG(l *autoLaunch, hosts []string) {
	w := a.w
	now := w.ex.Now()
	res := l.evs.Stage(l.steps).Result
	set := toSet(hosts)
	done, nomon := toSet(res.Done), toSet(res.NoMon)
	var oks, mons, bad, gave []string
	w.update(func(s *runState) {
		for i := range s.Hosts {
			r := &s.Hosts[i]
			if !set[r.Host] || !r.NGAccepted {
				continue
			}
			r.Running = ""
			r.NGAccepted = false
			switch {
			case !res.Have, done[r.Host], nomon[r.Host]:
				r.NGLaunched, r.Stress, r.AutoErr = true, "O", ""
				if res.Have && nomon[r.Host] && !done[r.Host] {
					r.NGNote = "감시 제외"
					mons = append(mons, r.Host)
				} else {
					r.NGNote = ""
				}
				oks = append(oks, r.Host)
			default: // notrun 또는 [결과] 의 어느 목록에도 없음: 호스트 실패
				r.AutoErr = "stress-ng 미실행(부하 안 오름)"
				bad = append(bad, r.Host)
				if r.NGTries >= autoMaxTries {
					r.GaveUp = "stress-ng"
					gave = append(gave, r.Host)
				}
			}
		}
		if len(bad) > 0 {
			s.Auto.NGWait = now.Add(w.conf.AutoCPUInt)
		}
	})
	if !res.Have {
		w.logf("stress-ng [결과] 줄이 없어 launch 된 %d대를 O 로 표시: %s", len(oks), strings.Join(oks, " "))
	} else {
		w.logf("stress-ng 감시 끝: O %d대(감시 제외 %d대 포함), 미실행 %d대", len(oks), len(mons), len(bad))
	}
	if len(bad) > 0 {
		w.logf("stress-ng 미실행(부하 안 오름, 시도 1회로 셈, 다음 launch %s): %s", now.Add(w.conf.AutoCPUInt).Format("15:04:05"), strings.Join(bad, " "))
	}
	if len(gave) > 0 {
		w.logf("stress-ng %d회 시도 뒤 포기 %d대: %s", autoMaxTries, len(gave), strings.Join(gave, " "))
	}
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
			a.finishNG()
		default:
		}
	}
}

// drain 은 중지할 때 뒤에서 도는 stress-test 가 (취소되어) 끝나기를 기다린다.
func (a *autoRun) drain() {
	for _, l := range a.pend { // 보류된 launch: x 로 답해 stress-test 를 끝내고 기다린다(콜백도 stopCh 로 x 를 낸다)
		if l.qid != "" {
			a.w.logf("질문 %s 취소: 중지 요청", l.qid)
		}
		l.aborted = true
		select {
		case l.ansCh <- ansExclude:
		default:
		}
		a.w.noteStress(l.name, <-l.ch)
	}
	a.pend = nil
	if a.cpuCh != nil {
		a.w.noteStress("CPU", <-a.cpuCh)
		a.cpuCh = nil
	}
	if a.ngCh != nil {
		a.w.noteStress("stress-ng", <-a.ngCh)
		a.ngCh = nil
		a.finishNG()
	}
}

// finishNG 는 끝난 stress-ng stress-test 의 호스트를 정리한다(이벤트를 아는 실행기면 endNG 로 가르고, 아니면 Running 만 지운다).
func (a *autoRun) finishNG() {
	if a.ngL != nil {
		a.endNG(a.ngL, a.ngHosts)
	}
	a.w.clearRunning(a.ngHosts)
	a.ngHosts, a.ngL = nil, nil
}

// wait 은 다음 할 일 시각(CPU 판정·재판정·대기열·제한 시간)까지 쉰다. stress-test 가 뒤에서 돌면 autoPoll 마다 깬다.
func (a *autoRun) wait(now time.Time) {
	w := a.w
	au := w.st.Auto
	next := now.Add(autoIdleMax)
	if a.cpuCh != nil || a.ngCh != nil {
		next = now.Add(autoPoll)
	}
	if w.yamlQ != "" || len(a.pend) > 0 { // 답이 오면 바로 이어 가려고 자주 깬다
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
		if !a.judged || a.jfail || w.autoUnreached() > 0 {
			soon(au.NextRecheck)
		}
		if len(au.Queue) > 0 {
			soon(au.QueueWait)
		}
		soon(au.NGWait)
		if len(w.autoFixPending()) > 0 {
			soon(au.FixWait)
		}
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
		if a.ngL != nil {
			p = append(p, fmt.Sprintf("stress-ng %d대 실행 확인 중(stress-test 감시가 끝나면 O 로 표시)", len(a.ngHosts)))
		} else {
			p = append(p, fmt.Sprintf("stress-ng %d대 stress-test 진행 중", len(a.ngHosts)))
		}
	}
	if w.yamlQ != "" {
		p = append(p, "yaml 바뀜 질문 답 기다리는 중(launch 보류)")
	}
	for _, l := range a.pend {
		if l.qid != "" {
			p = append(p, l.name+" 템플릿 인벤토리 사용 중, 답을 기다리는 중(launch 보류)")
		} else {
			p = append(p, l.name+" 템플릿 인벤토리 사용 중, 앞 질문 뒤에 물음(launch 보류)")
		}
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
		for i := range s.Hosts { // 부하가 높아 다시 돌리지 않은 CPU X 호스트: 일정은 그대로 두고 사유만 남긴다
			if r := &s.Hosts[i]; r.CPU == "X" && r.LastLoad >= cpuRetryMaxLoad && r.AutoErr == "" {
				r.AutoErr = "부하 높아 재실행 안 함"
			}
		}
		s.Stage = stageFinish
		s.Busy = false
		s.Question = nil
		s.Detail = fmt.Sprintf("자동 진행 끝(%s): 모두 O %d대, 남음 %d대, 포기 %d대", why, all, left, gave)
		if left > 0 && s.Auto != nil && s.Auto.EnvReason != "" {
			s.Detail += fmt.Sprintf(" — 마지막 환경 실패(연속 %d회): %s", s.Auto.EnvFail, s.Auto.EnvReason)
		}
	})
	w.saveResult()
}

// abortMsg 는 인벤토리 사용 중 질문에 x 로 답해(사람·제한 시간) launch 되지 않은 경우의 오류 문구다.
func abortMsg(name, msg string) string {
	return name + " 템플릿 인벤토리 사용 중 질문에 x(이 묶음 중단) — " + msg
}
