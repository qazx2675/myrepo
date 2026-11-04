// os6run_tui.go - os6_mgmt 직접 실행의 TUI 쪽: g(준비확인)·c/t(설정체크)를 데몬 없이 TUI 프로세스 안에서 1회 실행, 진행·결과 보기,
// 실행 중에는 종료(q·EOF·시그널)를 막고 끝난 뒤에만 끝낸다. 기록·잠금은 os6run.go
package main

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

const viewOS6 = "os6direct" // os6_mgmt 직접 실행 진행·결과 보기

// os6DirState: 진행 중인 os6 직접 실행 (한 번에 하나)
type os6DirState struct {
	kind     string // "check" (g) | "run" (c/t)
	started  time.Time
	view     *viewState    // 진행·결과 보기 (사용자가 닫아도 여기 남아 끝나면 다시 연다)
	head     []string      // 보기 맨 위 줄 (실행 위치·대상·제외)
	wake     chan struct{} // 끝나면 닫힘 → 루프를 깨운다
	out      os6DirOut     // wake 가 닫힌 뒤에만 읽는다
	tick     <-chan time.Time
	stopTick func()
}

type os6DirOut struct {
	status string
	lines  []string
}

func (st *tuiState) os6Wake() <-chan struct{} {
	if st.o6 == nil {
		return nil
	}
	return st.o6.wake
}

func (st *tuiState) os6Tick() <-chan time.Time {
	if st.o6 == nil {
		return nil
	}
	return st.o6.tick
}

// os6Busy: 직접 실행 중이면 안내하고 보기를 다시 열고 true
func (st *tuiState) os6Busy() bool {
	if st.o6 == nil {
		return false
	}
	st.view = st.o6.view
	st.sel.Msg = "os6_mgmt 직접 실행 중입니다 — 끝난 뒤 다시 실행하세요"
	return true
}

// os6Begin: f 를 백그라운드로 실행하고 진행 보기를 연다
func (st *tuiState) os6Begin(kind, title string, head []string, f func() os6DirOut) {
	o := &os6DirState{kind: kind, started: st.env.Now(), head: head, wake: make(chan struct{})}
	o.view = &viewState{kind: viewOS6, title: title, jobID: st.sel.JobID, yml: st.sel.Yml}
	o.tick, o.stopTick = st.env.Tick(time.Second)
	go func() {
		o.out = f()
		close(o.wake)
	}()
	st.o6 = o
	st.view = o.view
	st.os6Status()
}

func (st *tuiState) os6Status() {
	o := st.o6
	el := int(st.env.Now().Sub(o.started) / time.Second)
	o.view.status = fmt.Sprintf("실행 중 (%02d:%02d) — os6_mgmt 에서 직접 실행, 끝날 때까지 TUI 를 종료할 수 없습니다", el/60, el%60)
	o.view.lines = o.head
}

// pollOS6: 루프마다 — 끝났으면 결과를 보기에 채운다 (보기를 닫았고 다른 화면이 없으면 다시 연다)
func (st *tuiState) pollOS6() {
	o := st.o6
	if o == nil {
		return
	}
	select {
	case <-o.wake:
	default:
		st.os6Status()
		return
	}
	o.stopTick()
	st.o6 = nil
	v := o.view
	v.done, v.status, v.scroll = true, o.out.status, 0
	v.lines = append(append([]string(nil), o.head...), o.out.lines...)
	if st.view == nil && st.nh == nil && st.wk == nil && st.chg == nil && st.awx == nil && st.oo == nil {
		st.view = v
	}
}

// startOS6Check: g (os6_mgmt) — 그룹(전체 보기면 job)의 완료 제외 호스트를 이 서버의 gossh 로 바로 준비확인. os8 상태는 바꾸지 않는다.
func (st *tuiState) startOS6Check() {
	if st.os6Busy() {
		return
	}
	_, g := findGroup(st.snap, st.sel.JobID, st.sel.Yml)
	if g == nil {
		st.sel.Msg = "그룹을 찾을 수 없습니다"
		return
	}
	var hosts []string
	for _, h := range g.Hosts {
		if h.Stage != StageDone {
			hosts = append(hosts, h.Host)
		}
	}
	if len(hosts) == 0 {
		st.sel.Msg = "재확인할 호스트가 없습니다 (모두 완료)"
		return
	}
	os6EnsureGossh()
	c := newOS6Checker()
	head := []string{fmt.Sprintf("os6_mgmt 에서 직접 준비확인 (이 서버의 gossh) — %d대, os8 데몬 상태는 바꾸지 않음", len(hosts)), ""}
	st.os6Begin("check", "서버 상태 확인 (g, os6_mgmt 직접) - "+recheckTarget(st.sel.Yml), head, func() os6DirOut { return os6CheckOut(c, hosts) })
}

// os6CheckOut: 준비확인 결과표
func os6CheckOut(c Checker, hosts []string) os6DirOut {
	res, err := c.Check("local", hosts)
	if err != nil {
		return os6DirOut{status: "실패", lines: []string{"[X] 준비확인 호출 실패: " + err.Error()}}
	}
	lines := []string{fmt.Sprintf("%-28s %-8s %s", "호스트", "결과", "상세")}
	ok := 0
	for _, h := range hosts {
		cr := res[h]
		if !cr.Responded {
			lines = append(lines, fmt.Sprintf("%-28s %-8s %s", h, "접속불가", strings.TrimSpace(whySuffix(cr))))
			continue
		}
		ok++
		d := fmt.Sprintf("uptime %.0fs", cr.Uptime)
		if cr.Anaconda {
			d += ", 설치중(anaconda)"
		}
		if cr.OldOS {
			d += ", 기존 OS(새 설치 아님)"
		}
		lines = append(lines, fmt.Sprintf("%-28s %-8s %s", h, "os6 응답", d))
	}
	return os6DirOut{status: fmt.Sprintf("완료 — 응답 %d대 / 접속불가 %d대", ok, len(hosts)-ok), lines: lines}
}

// startOS6Run: c/t 확인(y) 후 (os6_mgmt) — 그룹 호스트를 이 서버의 체크스크립트로 바로 실행.
// os8 데몬이 실행 중인 호스트(최신 스냅샷의 체크중·2차체크·실행 중 수동 run)와 다른 os6 직접 실행의 호스트는 뺀다.
func (st *tuiState) startOS6Run() {
	mode := st.sel.Mode
	if st.os6Busy() {
		return
	}
	sh := os6DirectScript()
	if sh == "" {
		st.sel.Msg = "[X] os_check_sh(또는 os6_os_check_sh) 가 비어 있어 os6_mgmt 에서 실행할 수 없습니다"
		return
	}
	s, err := st.src.Snapshot() // 겹침 판단은 최신 스냅샷으로 (os8_mgmt 접속 불가면 판단 생략 — 잠금으로 os8 이 건너뜀)
	var ce *snapCachedErr
	live := err == nil
	if live || errors.As(err, &ce) {
		st.snap, st.err = s, ""
		if ce != nil {
			st.err = ce.Error()
		}
	}
	j, g := findGroup(st.snap, st.sel.JobID, st.sel.Yml)
	if j == nil || g == nil {
		st.sel.Msg = "그룹을 찾을 수 없습니다"
		return
	}
	now := st.env.Now()
	dir := os6RunDir()
	busy := map[string]bool{}
	if live {
		busy = os8BusyHosts(st.snap)
	}
	held := os6HeldHosts(dir, now)
	var hosts, skip8, skip6 []string
	for _, h := range g.Hosts {
		switch {
		case busy[h.Host]:
			skip8 = append(skip8, h.Host)
		case held[h.Host] != "":
			skip6 = append(skip6, h.Host)
		default:
			hosts = append(hosts, h.Host)
		}
	}
	if len(hosts) == 0 {
		st.sel.Msg = fmt.Sprintf("[X] 실행할 호스트가 없습니다 — os8 데몬 실행 중 %d대, 다른 os6 직접 실행 중 %d대", len(skip8), len(skip6))
		return
	}
	job := &Job{ID: j.ID, User: j.User, Submitted: j.Submitted, RunMode: mode, Hosts: map[string]*Host{}}
	for _, n := range hosts {
		job.Hosts[n] = &Host{Route: "local"}
	}
	lk := newOS6Lock(job, g.Yml, mode, hosts, now)
	lp, err := os6Acquire(dir, lk)
	if err != nil {
		st.sel.Msg = "[X] os6_mgmt 직접 실행 불가: " + err.Error()
		return
	}
	os6EnsureGossh()
	head := []string{
		"실행 위치: os6_mgmt 직접 (데몬 없이 이 TUI 에서 1회) — 체크스크립트 " + sh,
		fmt.Sprintf("대상: %d대 %s", len(hosts), strings.Join(hosts, " ")),
	}
	if len(skip8) > 0 {
		head = append(head, fmt.Sprintf("제외(os8 데몬 실행 중) %d대: %s", len(skip8), strings.Join(skip8, " ")))
	}
	if len(skip6) > 0 {
		head = append(head, fmt.Sprintf("제외(다른 os6 직접 실행 중) %d대: %s", len(skip6), strings.Join(skip6, " ")))
	}
	if !live {
		head = append(head, "[!] os8_mgmt 접속 불가 — os8 데몬 실행 여부는 확인하지 못함 (잠금이 있는 동안 os8 데몬은 이 호스트를 건너뜀)")
	}
	head = append(head, "기록: "+dir+"/runs/"+lk.ID+".json", "")
	r, clock := newOS6Runner(sh), st.env.Now
	title := fmt.Sprintf("os6_mgmt 직접 실행 [%s] - %s", modeLabel(mode), g.Yml)
	st.os6Begin("run", title, head, func() os6DirOut {
		rec, text := os6Execute(dir, r, job, lk, lp, clock)
		status := fmt.Sprintf("완료 — 처리 %d대 / 결과 없음(접속불가 등) %d대", len(rec.Processed), len(hosts)-len(rec.Processed))
		switch {
		case rec.State == "error":
			status = "실패 — " + rec.Error
		case rec.Abnormal:
			status += " (os_check 비정상 종료)"
		case mode != ModeCheck && len(rec.Processed) > 0:
			status += " — 완료기록을 os8 데몬이 다음 주기에 반영"
		}
		return os6DirOut{status: status, lines: splitLines(strings.TrimRight(text, "\n"))}
	})
}
