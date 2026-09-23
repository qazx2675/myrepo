package tui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"vm-ip-change/internal/status"
)

func newTracker(n int) *status.Tracker {
	vms := make([]*status.VM, n)
	for i := range vms {
		vms[i] = &status.VM{Hostname: fmt.Sprintf("host%04d", i), NewIP: "10.0.0.1", Gateway: "10.0.0.1"}
	}
	return status.NewTracker(vms)
}

func TestListLinesPagination(t *testing.T) {
	tr := newTracker(1000)
	const pageSize = 20

	lines := listLines(tr, 0, pageSize, "")
	if got := len(lines) - headerLines; got != pageSize {
		t.Fatalf("첫 페이지 줄 수 = %d, want %d", got, pageSize)
	}
	if !strings.Contains(lines[2], "[페이지 1/50]") {
		t.Errorf("페이지 표시 = %q", lines[2])
	}

	lines = listLines(tr, 999, pageSize, "")
	if !strings.Contains(lines[2], "[페이지 50/50]") {
		t.Errorf("마지막 페이지 표시 = %q", lines[2])
	}
	if !strings.HasPrefix(lines[len(lines)-1], "> host0999") {
		t.Errorf("선택 표시 = %q", lines[len(lines)-1])
	}
}

func TestFinishedElapsedFreezes(t *testing.T) {
	v := &status.VM{Hostname: "h"}
	v.SetPhase(status.PhaseChecking)
	time.Sleep(20 * time.Millisecond)
	v.Finish(status.OutcomeDone, nil)
	first := v.Snapshot().Elapsed
	time.Sleep(50 * time.Millisecond)
	if again := v.Snapshot().Elapsed; again != first {
		t.Fatalf("완료 후 경과시간이 변함: %v -> %v", first, again)
	}
}

// 1000대일 때 한 프레임을 만드는 비용과 출력 크기가 대상 수와 무관하게
// 한 화면 분량인지 확인한다.
func TestFrameCostWith1000VMs(t *testing.T) {
	tr := newTracker(1000)
	for i, v := range tr.VMs {
		if i%3 == 0 {
			v.SetPhase(status.PhaseApplying)
		}
		if i%3 == 1 {
			v.SetPhase(status.PhaseChecking)
			v.Finish(status.OutcomeDone, nil)
		}
	}

	start := time.Now()
	const iters = 100
	var frame string
	for i := 0; i < iters; i++ {
		frame = buildFrame(listLines(tr, 500, 40, ""))
	}
	per := time.Since(start) / iters
	t.Logf("1000대, 40줄 페이지: 프레임 %d바이트, 프레임당 %v", len(frame), per)

	if len(frame) > 8*1024 {
		t.Errorf("프레임이 너무 큼: %d바이트", len(frame))
	}
	if per > 10*time.Millisecond {
		t.Errorf("프레임 생성이 너무 느림: %v", per)
	}
}

func TestNoticeShownInHeader(t *testing.T) {
	tr := newTracker(5)
	lines := listLines(tr, 0, 10, "!! Ctrl+C 1/3")
	if lines[headerLines-1] != "!! Ctrl+C 1/3" {
		t.Fatalf("안내 줄 = %q", lines[headerLines-1])
	}
}
