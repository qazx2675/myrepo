package main

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

// 전원 작업(계획서 7장). vCenter 에 대한 실제 호출은 VMOps 로 분리해 가짜 구현으로 시험한다.

// VMOps 는 vCenter 에 보내는 전원 동작이다. 성공하면 vm 의 상태(Power 등)가 갱신되어 있어야 한다.
type VMOps interface {
	PowerOn(ctx context.Context, vm *VMInfo) error
	ShutdownGuest(ctx context.Context, vm *VMInfo) error // 요청만 보낸다(종료 완료는 Refresh 로 확인)
	PowerOff(ctx context.Context, vm *VMInfo) error
	Refresh(ctx context.Context, vm *VMInfo) error
}

// AskFunc 는 사용자에게 한 줄을 묻는다(공용 stdin 리더로 구현, input_timeout_sec 제한).
// 에러(EOF, Ctrl+C, 시간 초과 ErrTimeout)는 호출자가 "No/중단"으로 본다.
type AskFunc func(ctx context.Context, prompt string) (string, error)

// WaitBootFunc 는 부팅 완료 판정이다. *BootChecker.WaitBoot 가 구현한다.
type WaitBootFunc func(ctx context.Context, vm *VMInfo, timeout time.Duration) BootResult

type Runner struct {
	Cfg      *Config
	Ops      VMOps
	WaitBoot WaitBootFunc
	Log      *RunLog
	Ask      AskFunc

	askMu sync.Mutex // 질문은 한 번에 하나씩
}

// forEachPar 는 최대 n 개씩 병렬로 fn 을 실행한다. ctx 가 취소되면 새 항목을 시작하지 않는다.
func forEachPar[T any](ctx context.Context, n int, items []T, fn func(T)) {
	if n < 1 {
		n = 1
	}
	sem := make(chan struct{}, n)
	var wg sync.WaitGroup
	for _, it := range items {
		if ctx.Err() != nil {
			break
		}
		sem <- struct{}{}
		wg.Add(1)
		go func(it T) {
			defer wg.Done()
			defer func() { <-sem }()
			fn(it)
		}(it)
	}
	wg.Wait()
}

func sleepCtx(ctx context.Context, d time.Duration) {
	select {
	case <-ctx.Done():
	case <-time.After(d):
	}
}

const notRun = "중단되어 실행하지 않음"

// RunOff : Guest 종료 → 시간 초과/Tools 미실행 VM 은 목록을 보여주고 YeS 확인 후 강제 PowerOff.
func (r *Runner) RunOff(ctx context.Context, vms []*VMInfo) []Result {
	res := newResultSet("Off", vms)
	start := time.Now()
	var mu sync.Mutex
	var waiting []*VMInfo
	type forced struct {
		vm     *VMInfo
		reason string
	}
	var force []forced

	forEachPar(ctx, r.Cfg.Concurrency, vms, func(vm *VMInfo) {
		switch vm.Power {
		case "poweredOff":
			res.set(vm.Name, StSkip, "이미 꺼져 있음", 0)
		case "poweredOn":
			if !vm.ToolsRunning {
				mu.Lock()
				force = append(force, forced{vm, "VMware Tools 미실행 (Guest 종료 불가)"})
				mu.Unlock()
				return
			}
			if err := r.Ops.ShutdownGuest(ctx, vm); err != nil {
				mu.Lock()
				force = append(force, forced{vm, "Guest 종료 요청 실패: " + err.Error()})
				mu.Unlock()
				return
			}
			r.Log.Logf(vm.Name, "Guest 종료 요청")
			mu.Lock()
			waiting = append(waiting, vm)
			mu.Unlock()
		default:
			res.set(vm.Name, StFail, "전원 상태 "+vm.Power+" — 작업할 수 없음", 0)
		}
	})

	// 종료 대기
	deadline := start.Add(r.Cfg.ShutdownTimeout)
	for len(waiting) > 0 && ctx.Err() == nil {
		var still []*VMInfo
		var smu sync.Mutex
		forEachPar(ctx, r.Cfg.Concurrency, waiting, func(vm *VMInfo) {
			if err := r.Ops.Refresh(ctx, vm); err == nil && vm.Power == "poweredOff" {
				res.set(vm.Name, StOK, "Guest 종료", time.Since(start))
				r.Log.Logf(vm.Name, "종료 완료 (%s)", fmtElapsed(time.Since(start)))
				return
			}
			smu.Lock()
			still = append(still, vm)
			smu.Unlock()
		})
		waiting = still
		if len(waiting) == 0 {
			break
		}
		if time.Now().Add(r.Cfg.BootPollInterval).After(deadline) {
			for _, vm := range waiting {
				force = append(force, forced{vm, fmt.Sprintf("Guest 종료 %d초 안에 끝나지 않음", int(r.Cfg.ShutdownTimeout/time.Second))})
			}
			waiting = nil
			break
		}
		sleepCtx(ctx, r.Cfg.BootPollInterval)
	}

	if len(force) > 0 && ctx.Err() == nil {
		r.Log.Raw("\n정상 종료되지 않은 VM %d대:\n", len(force))
		for _, f := range force {
			r.Log.Raw("  %-20s %s\n", f.vm.Name, f.reason)
		}
		ans, err := r.ask(ctx, "강제 PowerOff 하시겠습니까? (YeS/No): ")
		timedOut := errors.Is(err, ErrTimeout)
		if timedOut { // 계속 기다리지 않는다: No 로 처리하고 결과표·결과 보고는 정상 흐름대로
			r.Log.Logf("", "[안내] %s — 강제 PowerOff 하지 않습니다 (No 로 처리)", inputTimeoutReason(r.Cfg.InputTimeout))
		}
		if err == nil && ans == "YeS" {
			targets := make([]*VMInfo, len(force))
			for i, f := range force {
				targets[i] = f.vm
			}
			forEachPar(ctx, r.Cfg.Concurrency, targets, func(vm *VMInfo) {
				t0 := time.Now()
				if err := r.Ops.PowerOff(ctx, vm); err != nil {
					res.set(vm.Name, StFail, "강제 PowerOff 실패: "+err.Error(), time.Since(t0))
					return
				}
				_ = r.Ops.Refresh(ctx, vm)
				if vm.Power != "poweredOff" {
					res.set(vm.Name, StFail, "강제 PowerOff 후에도 "+vm.Power, time.Since(t0))
					return
				}
				res.set(vm.Name, StOK, "강제 PowerOff", time.Since(start))
				r.Log.Logf(vm.Name, "강제 PowerOff 완료")
			})
		} else {
			why := "강제 PowerOff 안 함 — "
			if timedOut {
				why = "강제 PowerOff 안 함(입력 시간 초과) — "
			}
			for _, f := range force {
				res.set(f.vm.Name, StFail, why+f.reason, 0)
			}
		}
	}
	for _, vm := range waiting { // 중단으로 대기를 못 끝낸 VM
		if !res.has(vm.Name) {
			res.set(vm.Name, StFail, "중단 — 종료 확인 못 함", time.Since(start))
		}
	}
	return res.list(notRun)
}

// RunOnAll : 순서·부팅 완료 확인 없이 한 번에 켠다(OS 설치용).
func (r *Runner) RunOnAll(ctx context.Context, vms []*VMInfo) []Result {
	res := newResultSet("On", vms)
	forEachPar(ctx, r.Cfg.Concurrency, vms, func(vm *VMInfo) { r.powerOnOne(ctx, vm, res) })
	return res.list(notRun)
}

// powerOnOne : PowerOn 요청 후 poweredOn 확인. 성공하면 true.
func (r *Runner) powerOnOne(ctx context.Context, vm *VMInfo, res *resultSet) bool {
	switch vm.Power {
	case "poweredOn":
		res.set(vm.Name, StSkip, "이미 켜져 있음", 0)
		return true
	case "poweredOff":
	default:
		res.set(vm.Name, StFail, "전원 상태 "+vm.Power+" — 작업할 수 없음", 0)
		return false
	}
	t0 := time.Now()
	if err := r.Ops.PowerOn(ctx, vm); err != nil {
		res.set(vm.Name, StFail, "PowerOn 실패: "+err.Error(), time.Since(t0))
		return false
	}
	_ = r.Ops.Refresh(ctx, vm)
	if vm.Power != "poweredOn" {
		res.set(vm.Name, StFail, "PowerOn 후에도 "+vm.Power, time.Since(t0))
		return false
	}
	res.set(vm.Name, StOK, "PowerOn", time.Since(t0))
	r.Log.Logf(vm.Name, "PowerOn 완료")
	return true
}

type bmGroup struct {
	key string
	vms []*VMInfo
}

func groupByBM(vms []*VMInfo) []bmGroup {
	idx := map[string]int{}
	var gs []bmGroup
	for _, v := range vms {
		k := v.GroupKey()
		i, ok := idx[k]
		if !ok {
			i = len(gs)
			idx[k] = i
			gs = append(gs, bmGroup{key: k})
		}
		gs[i].vms = append(gs[i].vms, v)
	}
	sort.SliceStable(gs, func(i, j int) bool { return gs[i].key < gs[j].key })
	for _, g := range gs {
		sort.SliceStable(g.vms, func(i, j int) bool { return g.vms[i].Ev < g.vms[j].Ev })
	}
	return gs
}

// GroupLpage : 그 BM 의 대상 VM 중 하나라도 lpage 를 쓰면 사용중.
func GroupLpage(vms []*VMInfo) bool {
	for _, v := range vms {
		if v.Lpage {
			return true
		}
	}
	return false
}

// RunOnSeq : 순차 부팅. 같은 ev 번호끼리 한 단계(wave)로 묶는다.
//
//	lpage 사용중 BM 의 VM: ev01 전부 동시에 PowerOn → 전부 부팅 완료될 때까지 대기 → ev02 전부 동시에 PowerOn → …
//	lpage 미사용 BM 의 VM: 안내 후 첫 단계와 함께 한 번에 PowerOn (부팅 완료 대기 없음)
func (r *Runner) RunOnSeq(ctx context.Context, vms []*VMInfo) []Result {
	res := newResultSet("On", vms)
	var batch []*VMInfo
	waves := map[int][]*VMInfo{}
	for _, g := range groupByBM(vms) {
		if !GroupLpage(g.vms) {
			r.Log.Logf(g.key, "※ lpage 미사용 — 한 번에 부팅해도 됩니다. (일괄로 켭니다)")
			batch = append(batch, g.vms...)
			continue
		}
		r.Log.Logf(g.key, "lpage 사용중 — ev 단계별 순차 부팅 대상 (%d대)", len(g.vms))
		for _, v := range g.vms {
			waves[v.Ev] = append(waves[v.Ev], v)
		}
	}
	evs := make([]int, 0, len(waves))
	for ev := range waves {
		evs = append(evs, ev)
	}
	sort.Ints(evs)

	var wg sync.WaitGroup
	if len(batch) > 0 { // 미사용 BM 은 순차 단계와 상관없이 한 번에
		wg.Add(1)
		go func() {
			defer wg.Done()
			forEachPar(ctx, r.Cfg.Concurrency, batch, func(vm *VMInfo) { r.powerOnOne(ctx, vm, res) })
		}()
	}
	for i, ev := range evs {
		if ctx.Err() != nil {
			r.markWavesNotRun(res, waves, evs[i:], notRun)
			break
		}
		if !r.powerOnWave(ctx, ev, waves[ev], res) { // 사용자가 중단(q) 선택
			r.markWavesNotRun(res, waves, evs[i+1:], "순차 부팅 중단으로 실행하지 않음")
			break
		}
	}
	wg.Wait()
	return res.list(notRun)
}

func (r *Runner) markWavesNotRun(res *resultSet, waves map[int][]*VMInfo, evs []int, why string) {
	for _, ev := range evs {
		for _, v := range waves[ev] {
			if !res.has(v.Name) {
				res.set(v.Name, StFail, why, 0)
			}
		}
	}
}

// powerOnWave : 한 ev 단계의 VM 전부에 동시에 PowerOn 을 보내고, 전부 부팅 완료될 때까지 기다린다.
// 제한 시간 안에 부팅 못 한 VM 이 있으면 한 번만 묻는다. 다음 단계로 계속하면 true, 중단(q)이면 false.
func (r *Runner) powerOnWave(ctx context.Context, ev int, vms []*VMInfo, res *resultSet) bool {
	r.Log.Logf("", "── ev%02d 단계: %d대 동시 PowerOn ──", ev, len(vms))
	t0 := time.Now()
	wasOn := map[string]bool{}
	var mu sync.Mutex
	var waiting, failed []*VMInfo
	forEachPar(ctx, r.Cfg.Concurrency, vms, func(vm *VMInfo) {
		switch vm.Power {
		case "poweredOn":
			mu.Lock()
			wasOn[vm.Name] = true
			waiting = append(waiting, vm)
			mu.Unlock()
			r.Log.Logf(vm.Name, "이미 켜져 있음 — 부팅 완료만 확인")
		case "poweredOff":
			if err := r.Ops.PowerOn(ctx, vm); err != nil {
				res.set(vm.Name, StFail, "PowerOn 실패: "+err.Error(), time.Since(t0))
				mu.Lock()
				failed = append(failed, vm)
				mu.Unlock()
				return
			}
			r.Log.Logf(vm.Name, "PowerOn 요청")
			mu.Lock()
			waiting = append(waiting, vm)
			mu.Unlock()
		default:
			res.set(vm.Name, StFail, "전원 상태 "+vm.Power+" — 작업할 수 없음", 0)
			mu.Lock()
			failed = append(failed, vm)
			mu.Unlock()
		}
	})
	for {
		var stuck []*VMInfo
		details := map[string]string{}
		forEachPar(ctx, r.Cfg.Concurrency, waiting, func(vm *VMInfo) {
			br := r.WaitBoot(ctx, vm, r.Cfg.BootTimeout)
			if br.OK {
				st, note := StOK, "부팅 완료 ("+br.Via+")"
				if wasOn[vm.Name] {
					st, note = StSkip, "이미 켜져 있음, 부팅 확인됨 ("+br.Via+")"
				}
				res.set(vm.Name, st, note, time.Since(t0))
				r.Log.Logf(vm.Name, "부팅 완료 — %s (%s)", br.Via, fmtElapsed(time.Since(t0)))
				return
			}
			mu.Lock()
			stuck = append(stuck, vm)
			details[vm.Name] = br.Detail
			mu.Unlock()
		})
		for _, vm := range waiting { // 대기 중 Ctrl+C 등으로 결과가 없는 VM
			if ctx.Err() != nil && !res.has(vm.Name) {
				res.set(vm.Name, StFail, "중단 — "+details[vm.Name], time.Since(t0))
			}
		}
		if ctx.Err() != nil {
			return true // 호출자가 ctx 를 보고 남은 단계를 처리
		}
		if len(stuck) == 0 && len(failed) == 0 {
			return true
		}
		ans := r.askWave(ctx, ev, stuck, details, failed, res)
		switch ans {
		case "c":
			waiting = stuck // 못 켜진 VM 만 다시 대기
			continue
		case "s":
			for _, vm := range stuck {
				res.set(vm.Name, StFail, "부팅 미완료, 건너뜀 — "+details[vm.Name], time.Since(t0))
			}
			return true
		default: // q
			for _, vm := range stuck {
				res.set(vm.Name, StFail, "부팅 미완료, 순차 부팅 중단 — "+details[vm.Name], time.Since(t0))
			}
			return false
		}
	}
}

// RunReboot : Off 가 모두 성공(또는 이미 꺼짐)이어야 On 으로 간다. Off/On 모두 병렬(순차 부팅은 2번 기능만).
func (r *Runner) RunReboot(ctx context.Context, vms []*VMInfo) []Result {
	off := r.RunOff(ctx, vms)
	for _, o := range off {
		if o.Status == StFail {
			r.Log.Logf("", "Off 단계에 실패한 VM 이 있어 On 단계는 진행하지 않습니다.")
			return off
		}
	}
	var bad []Result
	var bmu sync.Mutex
	forEachPar(ctx, r.Cfg.Concurrency, vms, func(vm *VMInfo) { // 꺼진 것을 한 번 더 확인(병렬)
		_ = r.Ops.Refresh(ctx, vm)
		if vm.Power != "poweredOff" {
			r.Log.Logf(vm.Name, "Off 확인 실패 (%s) — On 단계는 진행하지 않습니다.", vm.Power)
			bmu.Lock()
			bad = append(bad, Result{VM: vm.Name, Op: "Off", Status: StFail, Note: "Off 확인 실패: " + vm.Power})
			bmu.Unlock()
		}
	})
	if len(bad) > 0 {
		return append(off, bad...)
	}
	return append(off, r.RunOnAll(ctx, vms)...)
}

// 질문 ----------------------------------------------------------------------

func (r *Runner) ask(ctx context.Context, prompt string) (string, error) {
	r.askMu.Lock()
	defer r.askMu.Unlock()
	r.Log.Pause()
	defer r.Log.Resume()
	return r.Ask(ctx, prompt)
}

// askChoice : allowed 중 하나가 올 때까지 다시 묻는다. 입력 에러(EOF, Ctrl+C, 입력 시간 초과)는 "q".
func (r *Runner) askChoice(ctx context.Context, prompt, allowed string) string {
	r.askMu.Lock()
	defer r.askMu.Unlock()
	r.Log.Pause()
	defer r.Log.Resume()
	for {
		ans, err := r.Ask(ctx, prompt)
		if errors.Is(err, ErrTimeout) { // 계속 기다리지 않는다: 가장 안전한 중단(q)으로 처리
			r.Log.Logf("", "[안내] %s — 순차 부팅을 중단합니다 (q 로 처리)", inputTimeoutReason(r.Cfg.InputTimeout))
			return "q"
		}
		if err != nil {
			return "q"
		}
		ans = strings.ToLower(strings.TrimSpace(ans))
		if len(ans) == 1 && strings.Contains(allowed, ans) {
			return ans
		}
	}
}

// askWave : 한 ev 단계에서 부팅이 끝나지 않았거나 PowerOn 에 실패한 VM 을 보여주고 한 번에 묻는다.
// 실패(PowerOn 불가) VM 은 다시 기다릴 수 없으므로, 부팅 대기 중인 VM 이 없으면 c 를 받지 않는다.
func (r *Runner) askWave(ctx context.Context, ev int, stuck []*VMInfo, details map[string]string, failed []*VMInfo, res *resultSet) string {
	r.Log.Raw("\nev%02d 단계에서 문제가 있는 VM:\n", ev)
	for _, vm := range stuck {
		r.Log.Raw("  %-20s %d초 안에 부팅 완료되지 않음 (%s)\n", vm.Name, int(r.Cfg.BootTimeout/time.Second), details[vm.Name])
	}
	for _, vm := range failed {
		note := ""
		if x := res.get(vm.Name); x.Note != "" {
			note = x.Note
		}
		r.Log.Raw("  %-20s %s\n", vm.Name, note)
	}
	if len(stuck) == 0 {
		return r.askChoice(ctx, "다음 ev 단계로 계속(s) / 순차 부팅 중단(q): ", "sq")
	}
	return r.askChoice(ctx, "계속 대기(c) / 이 VM 건너뛰고 다음 ev 단계(s) / 순차 부팅 중단(q): ", "csq")
}

// Run 은 기능 번호에 맞는 작업을 실행한다.
func (r *Runner) Run(ctx context.Context, act Action, vms []*VMInfo) []Result {
	switch act {
	case ActPowerOff:
		return r.RunOff(ctx, vms)
	case ActPowerOnSeq:
		return r.RunOnSeq(ctx, vms)
	case ActPowerOnAll:
		return r.RunOnAll(ctx, vms)
	case ActReboot:
		return r.RunReboot(ctx, vms)
	}
	return nil
}
