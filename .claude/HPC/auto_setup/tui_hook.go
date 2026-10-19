// tui_hook.go - 무인자 리포트 진입점. 기본은 plain 표, TUI(tui.go)가 init() 에서 덮어쓴다.
package main

import (
	"fmt"
	"os"
	"strings"
	"text/tabwriter"
)

// runReport: 리포트 실행(TUI 또는 plain). plain=true 면 텍스트 표. 종료코드 반환.
var runReport = func(src SnapshotSource, plain bool) int {
	s, err := src.Snapshot()
	if err != nil {
		fmt.Fprintln(os.Stderr, ctlPaint("[X]", 31, ctlIsTTY(os.Stderr)), "상태 읽기 실패:", err)
		return 1
	}
	fmt.Print(hookPlainReport(s))
	return 0
}

// plainReport: 스냅샷 → 텍스트 표 (색 없음)
func hookPlainReport(s Snapshot) string {
	var sb strings.Builder
	daemon := "중지"
	if s.Daemon.Running {
		daemon = fmt.Sprintf("실행중(pid %d)", s.Daemon.PID)
	}
	t := s.Totals
	fmt.Fprintf(&sb, "갱신 %s  데몬 %s\n", s.NowText, daemon)
	fmt.Fprintf(&sb, "전체 %d  완료 %d  진행 %d  정체 %d  실패 %d\n", t.Total, t.Done,
		t.Total-t.Done-t.Stuck-t.Failed, t.Stuck, t.Failed)
	if len(s.Jobs) == 0 {
		sb.WriteString("진행 중인 작업 없음\n")
		return sb.String()
	}
	tw := tabwriter.NewWriter(&sb, 0, 8, 2, ' ', 0)
	fmt.Fprintln(tw, "작업\t그룹\tinfra\tos\tboot\tsplunk\t호스트\t단계\t최장경과")
	for _, j := range s.Jobs {
		id := j.ID
		if j.Closed {
			id += "(종료)"
		}
		for _, g := range j.Groups {
			var st []string
			for _, sg := range StageOrder {
				if n := g.Counts.Get(sg); n > 0 {
					st = append(st, fmt.Sprintf("%s %d", sg.Label(), n))
				}
			}
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%d\t%s\t%s\n", id, g.Yml, g.Infra, g.OS, g.Boot, g.Splunk,
				g.Counts.Total, strings.Join(st, ", "), plainElapsed(g.MaxElapsed))
		}
	}
	tw.Flush()
	return sb.String()
}

func plainElapsed(sec int64) string {
	if sec <= 0 {
		return "-"
	}
	return fmt.Sprintf("%d:%02d:%02d", sec/3600, sec%3600/60, sec%60)
}
