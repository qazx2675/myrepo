package main

import (
	"strings"
	"testing"
)

func TestLogLineKind(t *testing.T) {
	ts := "2026-11-09 13:00:00 "
	cases := []struct {
		line string
		want int
	}{
		{"+ (cd /x && /x/stress-test -user kdh)", logCmd},
		{"+ ssh root@B (ls)", logCmd},
		{ts + "판정 시작", logMsg},
		{ts + "경고: 응답 없음", logWarn},
		{ts + "무시한 답 x", logWarn},
		{ts + "호스트 ev01 실패", logErr},
		{ts + "launch 되지 않았", logErr},
		{ts + "질문 q1 (c/r/s): 오류 처리?", logQA},
		{ts + "답 c", logQA},
		{"[감시] CPU 40%", logOut},
		{"some output", logOut},
		{"ERROR: boom", logErr},
		{"접속 실패", logErr},
		{"", logOut},
	}
	for _, c := range cases {
		if got := logLineKind(c.line); got != c.want {
			t.Errorf("logLineKind(%q)=%d, 기대 %d", c.line, got, c.want)
		}
	}
}

func TestLogPaneColor(t *testing.T) {
	v := viewState{LogLines: []string{"+ (cd /x && ./stress-test)", "stress out", "2026-11-09 13:00:00 메시지"}}
	pane := strings.Join(logPane(v, 60, 4), "\n")
	if !strings.Contains(pane, "\x1b[1;36m+ ") || !strings.Contains(pane, "\x1b[90mstress out") {
		t.Errorf("색 로그 창에 명령/출력 색이 없음: %q", pane)
	}
	for _, l := range logPane(v, 12, 4) {
		if displayWidth(stripANSI(l)) > 12 {
			t.Errorf("폭 초과: %q", l)
		}
	}
	v.NoColor = true
	if p := strings.Join(logPane(v, 60, 4), "\n"); strings.Contains(p, "\x1b") {
		t.Errorf("NoColor 인데 ESC 포함: %q", p)
	}
}
