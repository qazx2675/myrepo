package monitor

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

func okLabel(lines []string) (string, bool) {
	if strings.HasPrefix(lines[len(lines)-1], "OK") {
		return "완료(OK)", false
	}
	return "실패", true
}

func newTestMonitor(n int) (*Monitor, *time.Time) {
	hosts := make([]string, n)
	for i := range hosts {
		hosts[i] = fmt.Sprintf("host%04d", i)
	}
	m := New("테스트", hosts, okLabel, nil)
	clock := m.start
	m.now = func() time.Time { return clock }
	return m, &clock
}

func TestSnapshotStates(t *testing.T) {
	m, clock := newTestMonitor(4)
	*clock = clock.Add(10 * time.Second)
	m.HostLine("host0000", "OK done")
	m.HostLine("host0001", "boom")
	m.HostError("host0002", "ERROR: SSH 접속 실패")
	m.HostError("host0003", "경고성 표준에러") // ERROR 아님 → 아직 안 끝남
	m.HostLine("unknown", "OK")         // 목록에 없는 호스트는 무시
	*clock = clock.Add(50 * time.Second)

	rows, c := m.snapshot()
	if c.total != 4 || c.done != 3 || c.failed != 2 || c.pending != 1 {
		t.Fatalf("counts = %+v", c)
	}
	want := []string{"완료(OK)", "실패", "실패(접속)", pendingLabel}
	for i, w := range want {
		if rows[i].label != w {
			t.Errorf("rows[%d].label = %q, want %q", i, rows[i].label, w)
		}
	}
	// 끝난 호스트의 경과시간은 끝난 시점(10초)에서 멈추고, 안 끝난 호스트는 계속 증가(60초)
	if rows[0].elapsed != 10*time.Second || rows[3].elapsed != 60*time.Second {
		t.Errorf("elapsed = %v / %v", rows[0].elapsed, rows[3].elapsed)
	}
}

func TestStderrThenStdoutUsesStdout(t *testing.T) {
	// gossh 재검증: 처음엔 접속 실패(ERROR)였다가 다시 시도해 성공하면 표준출력 결과를 따른다.
	m, _ := newTestMonitor(1)
	m.HostError("host0000", "ERROR: SSH 접속 실패: timeout")
	m.HostLine("host0000", "OK")
	rows, c := m.snapshot()
	if rows[0].label != "완료(OK)" || c.failed != 0 {
		t.Fatalf("label=%q failed=%d", rows[0].label, c.failed)
	}
}

func TestListPaging(t *testing.T) {
	m, _ := newTestMonitor(1000)
	rows, c := m.snapshot()
	ps := pageSize(40)
	lines := listLines("테스트", rows, c, ps+3, ps, time.Minute, "")
	if len(lines) != headerLines+ps {
		t.Fatalf("줄 수 = %d, want %d", len(lines), headerLines+ps)
	}
	pages := (1000 + ps - 1) / ps
	if !strings.Contains(lines[2], fmt.Sprintf("[페이지 2/%d]", pages)) {
		t.Errorf("페이지 표시 = %q", lines[2])
	}
	if !strings.HasPrefix(lines[headerLines+3], "> ") {
		t.Errorf("선택 표시가 없음: %q", lines[headerLines+3])
	}
	// 1000대여도 한 프레임은 한 화면 분량
	if f := buildFrame(lines); len(f) > 8*1024 {
		t.Errorf("프레임이 너무 큼: %d bytes", len(f))
	}
}

func TestLastPageShort(t *testing.T) {
	m, _ := newTestMonitor(5)
	rows, c := m.snapshot()
	lines := listLines("테스트", rows, c, 4, 3, 0, "")
	if got := len(lines) - headerLines; got != 2 {
		t.Fatalf("마지막 페이지 줄 수 = %d, want 2", got)
	}
}

func TestDetailTruncates(t *testing.T) {
	var r row
	r.name, r.label, r.done = "h", "실패", true
	for i := 0; i < 100; i++ {
		r.lines = append(r.lines, fmt.Sprintf("line%d", i))
	}
	lines := detailLines("테스트", r, 24, "")
	if len(lines) > 24 {
		t.Fatalf("상세 화면이 터미널보다 김: %d", len(lines))
	}
	if !strings.Contains(strings.Join(lines, "\n"), "line99") {
		t.Errorf("최근 줄이 보여야 함")
	}
}

func TestAbortSummary(t *testing.T) {
	m, _ := newTestMonitor(3)
	m.HostLine("host0000", "OK")
	s := m.abortSummary("Ctrl+C 3회")
	if !strings.Contains(s, "안 끝남 2") || !strings.Contains(s, "host0001") || !strings.Contains(s, "host0002") {
		t.Fatalf("요약:\n%s", s)
	}
}

func TestFmtDuration(t *testing.T) {
	if got := fmtDuration(49*time.Hour + 2*time.Minute + 3*time.Second); got != "49:02:03" {
		t.Fatalf("got %s", got)
	}
}
