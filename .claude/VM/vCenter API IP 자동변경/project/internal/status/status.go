// Package status tracks each target VM's live progress so the CLI's
// progress line and interactive list (internal/tui) can read it while
// workers in cmd/vm-ip-change update it concurrently.
package status

import (
	"sync"
	"time"
)

// Phase is which of the three guest-side steps a VM is currently on
// or last attempted. It says nothing about success/failure — see Outcome.
type Phase int

const (
	PhasePending    Phase = iota // 대기중 (아직 워커를 시작하지 않음)
	PhaseChecking                // 연결 확인
	PhaseApplying                // IP 설정 적용
	PhaseRestarting              // 연결 재기동
)

func (p Phase) Label() string {
	switch p {
	case PhasePending:
		return "대기중"
	case PhaseChecking:
		return "연결 확인"
	case PhaseApplying:
		return "IP 설정 적용"
	case PhaseRestarting:
		return "연결 재기동"
	default:
		return "?"
	}
}

// Outcome is the terminal result once a VM is no longer being worked on.
// OutcomeNone means "still pending or in progress".
type Outcome int

const (
	OutcomeNone   Outcome = iota
	OutcomeDone           // 완료(OK)
	OutcomeFailed         // 실패
)

func (o Outcome) Label() string {
	switch o {
	case OutcomeDone:
		return "완료(OK)"
	case OutcomeFailed:
		return "실패"
	default:
		return ""
	}
}

// VM is one target's live status. Safe for concurrent use: one worker
// goroutine writes to it while the TUI goroutine reads it.
type VM struct {
	Hostname string
	NewIP    string
	Gateway  string

	mu         sync.Mutex
	phase      Phase
	outcome    Outcome
	startedAt  time.Time
	finishedAt time.Time
	err        error
}

// SetPhase records which step the VM is currently attempting and starts its
// elapsed-time clock on the first non-pending transition.
func (v *VM) SetPhase(p Phase) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.startedAt.IsZero() && p != PhasePending {
		v.startedAt = time.Now()
	}
	v.phase = p
}

// Finish records the terminal outcome (and, for failures, the error) for
// the phase currently set. It does not change the phase, so the detail view
// can still show which step this VM was on when it finished.
func (v *VM) Finish(o Outcome, err error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.outcome = o
	v.err = err
	if v.finishedAt.IsZero() {
		v.finishedAt = time.Now()
	}
}

// Label is the short Korean string shown in the progress list: the outcome
// once finished, otherwise the current phase.
func (v *VM) Label() string {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.outcome != OutcomeNone {
		return v.outcome.Label()
	}
	return v.phase.Label()
}

// Snapshot is a consistent, lock-free-to-use copy of the VM's current state.
type Snapshot struct {
	Phase   Phase
	Outcome Outcome
	Elapsed time.Duration
	Err     error
}

func (v *VM) Snapshot() Snapshot {
	v.mu.Lock()
	defer v.mu.Unlock()
	var elapsed time.Duration
	switch {
	case v.startedAt.IsZero():
	case !v.finishedAt.IsZero():
		elapsed = v.finishedAt.Sub(v.startedAt) // 끝난 대상은 끝난 시점에서 시간이 멈춘다
	default:
		elapsed = time.Since(v.startedAt)
	}
	return Snapshot{Phase: v.phase, Outcome: v.outcome, Elapsed: elapsed, Err: v.err}
}

// Finished reports whether this VM has reached a terminal outcome.
func (v *VM) Finished() bool {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.outcome != OutcomeNone
}

// Tracker holds every target VM in list.txt order plus the run's start time,
// so the progress line can compute "경과"/퍼센트 and the TUI can decide when
// 60초 threshold has passed.
type Tracker struct {
	VMs   []*VM
	Start time.Time
}

func NewTracker(vms []*VM) *Tracker {
	return &Tracker{VMs: vms, Start: time.Now()}
}

// Counts returns how many VMs have reached a terminal outcome.
func (t *Tracker) Counts() (done, total int) {
	total = len(t.VMs)
	for _, v := range t.VMs {
		if v.Finished() {
			done++
		}
	}
	return
}
