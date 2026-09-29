package tui

import (
	"errors"
	"fmt"
	"os"
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

func TestSaveLines(t *testing.T) {
	tr := newTracker(5)
	tr.VMs[0].SetPhase(status.PhaseChecking)
	tr.VMs[0].Finish(status.OutcomeDone, nil)
	tr.VMs[1].SetPhase(status.PhaseApplying)
	tr.VMs[1].Finish(status.OutcomeFailed, errors.New("line1\nline2"))
	tr.VMs[2].SetPhase(status.PhaseApplying)
	tr.VMs[3].SetPhase(status.PhaseChecking)
	// VMs[4] 는 대기

	tests := []struct {
		name string
		cat  saveCategory
		want []string
	}{
		{"done", saveDone, []string{"host0000 10.0.0.1 00:00:00"}},
		{"failed", saveFailed, []string{"host0001 10.0.0.1 # line1 line2"}},
		{"running", saveRunning, []string{
			"host0002 10.0.0.1 # " + status.PhaseApplying.Label() + " 00:00:00",
			"host0003 10.0.0.1 # " + status.PhaseChecking.Label() + " 00:00:00",
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := saveLines(tr, tc.cat)
			if strings.Join(got, "|") != strings.Join(tc.want, "|") {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
			for _, l := range got {
				f := strings.Fields(l)
				if len(f) < 2 || !strings.HasPrefix(f[0], "host") || f[1] != "10.0.0.1" {
					t.Errorf("첫 두 열이 hostname ip 가 아님: %q", l)
				}
			}
		})
	}
}

func TestSaveLinesEmptyAndUnknownErr(t *testing.T) {
	tr := newTracker(2)
	for _, c := range []saveCategory{saveDone, saveFailed, saveRunning} {
		if got := saveLines(tr, c); len(got) != 0 {
			t.Errorf("category %d: got %q, want empty", c, got)
		}
	}
	tr.VMs[0].SetPhase(status.PhaseChecking)
	tr.VMs[0].Finish(status.OutcomeFailed, nil)
	if got := saveLines(tr, saveFailed); len(got) != 1 || got[0] != "host0000 10.0.0.1 # 알 수 없음" {
		t.Errorf("got %q", got)
	}
}

func TestSaveListEmptyFile(t *testing.T) {
	t.Chdir(t.TempDir())
	name, n, err := saveList(newTracker(2), saveDone)
	if err != nil || n != 0 {
		t.Fatalf("n=%d err=%v", n, err)
	}
	b, err := os.ReadFile(name)
	if err != nil || len(b) != 0 {
		t.Fatalf("파일 내용 = %q err=%v", b, err)
	}
}

func TestNoticeShownInHeader(t *testing.T) {
	tr := newTracker(5)
	lines := listLines(tr, 0, 10, "!! Ctrl+C 1/3")
	if lines[headerLines-1] != "!! Ctrl+C 1/3" {
		t.Fatalf("안내 줄 = %q", lines[headerLines-1])
	}
}
