package main

// 부하 감시: interval 마다 1분 로드를 읽어 기준 미만 호스트 목록을 갱신한다.

import (
	"fmt"
	"io"
	"strings"
	"time"
)

// loadChecker 는 호스트 목록의 1분 로드를 읽는다 (응답 없는 호스트는 맵에 없음).
type loadChecker func(hosts []string) (map[string]float64, error)

// 시간 함수 (테스트에서 바꾼다)
var (
	nowFn   = time.Now
	sleepFn = time.Sleep
)

// monRow 는 감시 화면의 한 줄이다.
type monRow struct {
	t    *target
	load float64
	ok   bool // 이번 회차에 응답했는지
}

// monitorLoad 는 최대 sec 초 동안 interval 초마다 로드를 확인해, 끝까지 기준 미만이었던 호스트를 돌려준다.
// 목록이 비면 바로 끝난다. screen 이면 ANSI 로 화면을 지우고 표를 다시 그리고, 아니면 회차마다 한 줄 요약.
func monitorLoad(w io.Writer, title string, list []*target, check loadChecker, sec, interval int, threshold float64, screen bool) []*target {
	rows := make([]*monRow, 0, len(list))
	for _, t := range list {
		rows = append(rows, &monRow{t: t})
	}
	start := nowFn()
	deadline := start.Add(time.Duration(sec) * time.Second)
	for tick := 1; len(rows) > 0; tick++ {
		next := start.Add(time.Duration(tick*interval) * time.Second)
		if next.After(deadline) {
			next = deadline
		}
		if d := next.Sub(nowFn()); d > 0 {
			sleepFn(d)
		}
		hosts := make([]string, len(rows))
		for i, r := range rows {
			hosts[i] = r.t.Host
		}
		loads, err := check(hosts)
		if err != nil {
			fmt.Fprintf(w, "%s 로드 확인 실패: %v\n", markWarn, err)
			loads = map[string]float64{}
		}
		var keep []*monRow
		for _, r := range rows {
			v, ok := loads[r.t.Host]
			if ok && v >= threshold {
				continue // 부하 확인됨
			}
			r.load, r.ok = v, ok
			keep = append(keep, r)
		}
		rows = keep
		elapsed := int(nowFn().Sub(start).Seconds() + 0.5)
		if screen {
			fmt.Fprint(w, renderScreen(title, rows, elapsed, sec, threshold, len(list)))
		} else {
			fmt.Fprintln(w, summaryLine(title, rows, elapsed, sec, threshold))
		}
		if !nowFn().Before(deadline) {
			break
		}
	}
	out := make([]*target, len(rows))
	for i, r := range rows {
		out[i] = r.t
	}
	return out
}

// renderScreen 은 top 처럼 화면을 지우고 기준 미만 호스트 표를 그린다.
func renderScreen(title string, rows []*monRow, elapsed, sec int, threshold float64, total int) string {
	var b strings.Builder
	b.WriteString("\033[H\033[2J")
	fmt.Fprintf(&b, "[감시] %s  경과 %d/%d초  로드 기준 %g  부하 확인 %d/%d대  기준 미만 %d대\n\n",
		title, elapsed, sec, threshold, total-len(rows), total, len(rows))
	b.WriteString(fmtTable([]string{"hostname", "CPU벤더", "load", "비고"}, monTableRows(rows)))
	return b.String()
}

func monTableRows(rows []*monRow) [][]string {
	var out [][]string
	for _, r := range rows {
		load := "응답없음"
		if r.ok {
			load = fmt.Sprintf("%.2f", r.load)
		}
		out = append(out, []string{r.t.Host, vendorLabel(r.t), load, strings.Join(r.t.Notes, ", ")})
	}
	return out
}

// summaryLine 은 비대화형용 한 줄 요약이다.
func summaryLine(title string, rows []*monRow, elapsed, sec int, threshold float64) string {
	parts := make([]string, 0, len(rows))
	for _, r := range rows {
		if r.ok {
			parts = append(parts, fmt.Sprintf("%s(%.2f)", r.t.Host, r.load))
		} else {
			parts = append(parts, r.t.Host+"(응답없음)")
		}
	}
	return fmt.Sprintf("[감시] %s 경과 %d/%d초 기준(%g) 미만 %d대: %s", title, elapsed, sec, threshold, len(rows), strings.Join(parts, " "))
}

// fmtTable 은 열 너비를 맞춘 표 문자열을 만든다 (한글은 2칸으로 계산).
func fmtTable(head []string, rows [][]string) string {
	w := make([]int, len(head))
	for i, h := range head {
		w[i] = dispWidth(h)
	}
	for _, r := range rows {
		for i, c := range r {
			if d := dispWidth(c); d > w[i] {
				w[i] = d
			}
		}
	}
	var b strings.Builder
	line := func(cells []string) {
		for i, c := range cells {
			if i == len(cells)-1 {
				b.WriteString(c)
			} else {
				b.WriteString(c + strings.Repeat(" ", w[i]-dispWidth(c)+2))
			}
		}
		b.WriteString("\n")
	}
	line(head)
	for _, r := range rows {
		line(r)
	}
	return b.String()
}

func dispWidth(s string) int {
	n := 0
	for _, r := range s {
		if r >= 0x1100 {
			n += 2
		} else {
			n++
		}
	}
	return n
}
