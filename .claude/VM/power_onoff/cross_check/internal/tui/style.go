package tui

import (
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"cross_check/internal/daemon"
	"cross_check/internal/store"
)

// ---- 색상(vm_power_onoff ui.go 와 같은 스타일) ----

// ColorEnabled 는 f 로 나가는 출력에 색을 칠할지 정한다.
// CROSS_CHECK_COLOR=never|always 가 우선, 그다음 NO_COLOR, 마지막으로 f 가 터미널인지.
func ColorEnabled(f *os.File) bool {
	switch strings.ToLower(os.Getenv("CROSS_CHECK_COLOR")) {
	case "never":
		return false
	case "always":
		return true
	}
	if _, ok := os.LookupEnv("NO_COLOR"); ok {
		return false
	}
	return IsTerminal(f)
}

// Painter 는 색상이 켜져 있을 때만 ANSI 코드로 감싼다.
type Painter struct{ On bool }

func (p Painter) paint(code, s string) string {
	if !p.On || s == "" {
		return s
	}
	return "\x1b[" + code + "m" + s + "\x1b[0m"
}

func (p Painter) Red(s string) string    { return p.paint("31;1", s) }
func (p Painter) Green(s string) string  { return p.paint("32", s) }
func (p Painter) Yellow(s string) string { return p.paint("33", s) }
func (p Painter) Cyan(s string) string   { return p.paint("36", s) }
func (p Painter) Bold(s string) string   { return p.paint("1", s) }

// Status 는 상태 글자에 색을 칠한다: 대기=노랑, 승인=초록, 중단=빨강.
func (p Painter) Status(st store.Status, s string) string {
	switch st {
	case daemon.StatusWaiting:
		return p.Yellow(s)
	case daemon.StatusApproved:
		return p.Green(s)
	case daemon.StatusAborted:
		return p.Red(s)
	}
	return s
}

// Label 은 상태+단계 표기(대기·승인·승인/작업중·승인/작업완료·승인/작업중단·중단)에 전체 화면과 같은 색을 칠한다.
func (p Painter) Label(st store.Status, ph store.Phase, s string) string {
	return p.paint(kindStyle[kindOf(st, ph)], s)
}

// ---- 폭 계산(한글은 2칸) ----

// dispWidth 는 터미널 표시 폭(전체 화면과 같은 runeWidth 표: 한글·한자·전각 2칸, 제어·결합 문자 0칸).
func dispWidth(s string) int { return strWidth(s) }

// pad 는 s 뒤에 공백을 붙여 표시 폭을 w 로 맞춘다.
func pad(s string, w int) string {
	if n := w - dispWidth(s); n > 0 {
		return s + strings.Repeat(" ", n)
	}
	return s
}

// table 은 열 폭을 맞춘 표를 쓴다. color 는 (행, 열, 칸 글자) 로 이미 폭이 맞춰진 칸에 색을 입힌다(nil 이면 없음).
// 마지막 열은 폭을 맞추지 않는다(사유처럼 긴 글).
func table(w io.Writer, indent string, headers []string, rows [][]string, color func(row, col int, cell string) string) {
	widths := make([]int, len(headers))
	for i, h := range headers {
		widths[i] = dispWidth(h)
	}
	for _, r := range rows {
		for i, c := range r {
			if i < len(widths) && dispWidth(c) > widths[i] {
				widths[i] = dispWidth(c)
			}
		}
	}
	line := func(cells []string, row int) string {
		var sb strings.Builder
		sb.WriteString(indent)
		for i, c := range cells {
			cell := c
			if i < len(cells)-1 {
				cell = pad(c, widths[i])
			}
			if color != nil && row >= 0 {
				cell = color(row, i, cell)
			}
			sb.WriteString(cell)
			if i < len(cells)-1 {
				sb.WriteString("  ")
			}
		}
		return strings.TrimRight(sb.String(), " ")
	}
	fmt.Fprintln(w, line(headers, -1))
	total := 0
	for _, x := range widths {
		total += x + 2
	}
	fmt.Fprintln(w, indent+strings.Repeat("-", total))
	for i, r := range rows {
		fmt.Fprintln(w, line(r, i))
	}
}

// ---- 시각·문구 ----

// fmtTime 은 표에 쓰는 시각 "01-02 15:04:05". zero 면 "-".
func fmtTime(t time.Time) string {
	if t.IsZero() {
		return "-"
	}
	return t.Local().Format("01-02 15:04:05")
}

func fmtTimePtr(t *time.Time) string {
	if t == nil {
		return "-"
	}
	return fmtTime(*t)
}

// PolicyText 는 계획서 4-2 의 필수 정책 문구. 시간은 이 데몬의 승인 유효시간(approval_ttl_sec)으로 만든다
// (기본 2분). 기본값이 아니면 설정으로 바뀐 값임을 덧붙인다.
func PolicyText(ttl time.Duration) string {
	if ttl <= 0 {
		ttl = daemon.DefaultApprovalTTL
	}
	s := "[필수 정책] cross_check 데몬을 사용하는 작업은 확인자의 승인 완료 후 " + daemon.FormatTTLDuration(ttl) +
		" 내에 본 작업을 진행하지 않으면 해당 작업 승인은 자동으로 취소(중단) 처리됩니다."
	if ttl != daemon.DefaultApprovalTTL {
		s += fmt.Sprintf(" (이 데몬 설정: approval_ttl_sec=%d)", int(ttl/time.Second))
	}
	return s
}

// ttlHeader 는 사용자·관리자 화면 머리말 아래에 보여 주는 현재 승인 유효시간 한 줄.
func ttlHeader(ttl time.Duration) string {
	if ttl <= 0 {
		ttl = daemon.DefaultApprovalTTL
	}
	s := fmt.Sprintf("  승인 유효시간: %s (approval_ttl_sec=%d) — 확인자 승인 후 이 시간 안에 본 작업을 시작하지 않으면 승인이 자동 취소됩니다",
		daemon.FormatTTLDuration(ttl), int(ttl/time.Second))
	return s
}

// jobTTL 은 작업 하나에 적용되는 승인 유효시간: 데몬이 준 값(JobView.TTLSec)을 우선하고, 없으면 def(그것도 없으면 기본값).
func jobTTL(j daemon.JobView, def time.Duration) time.Duration {
	if j.TTLSec > 0 {
		return time.Duration(j.TTLSec) * time.Second
	}
	if def > 0 {
		return def
	}
	return daemon.DefaultApprovalTTL
}

// remainText 는 승인 후 아직 시작하지 않은 작업의 남은 시간 문구.
// 만료 시각은 데몬이 준 ExpiresAt 을 우선하고, 없으면 승인 시각 + 유효시간으로 계산한다.
func remainText(j daemon.JobView, ttl time.Duration, now time.Time) string {
	if j.Status != daemon.StatusApproved || j.Phase != daemon.PhaseNone || j.ApprovedAt == nil || j.StartedAt != nil {
		return ""
	}
	ttl = jobTTL(j, ttl)
	exp := j.ApprovedAt.Add(ttl)
	if j.ExpiresAt != nil {
		exp = *j.ExpiresAt
	}
	left := exp.Sub(now)
	if left <= 0 {
		return "승인 유효시간 지남(곧 중단 처리)"
	}
	return fmt.Sprintf("승인 후 %s 내 본 작업 필요(남은 %d초)", daemon.FormatTTLDuration(ttl), int((left+time.Second-1)/time.Second))
}

// reasonText 는 상세·작업자 리스트의 사유: 중단 사유, 시작·종료 기록, 승인 후 남은 시간 순으로 보여준다.
func reasonText(j daemon.JobView, ttl time.Duration, now time.Time) string {
	parts := []string{}
	if j.Reason != "" {
		parts = append(parts, j.Reason)
	}
	if j.Status == daemon.StatusApproved {
		if j.StartedAt != nil {
			parts = append(parts, "본 작업 시작 "+fmtTime(*j.StartedAt))
		}
		if j.FinishedAt != nil {
			parts = append(parts, string(j.Phase)+" "+fmtTime(*j.FinishedAt))
		}
		if r := remainText(j, ttl, now); r != "" {
			parts = append(parts, r)
		}
	}
	if len(parts) == 0 {
		return "-"
	}
	return strings.Join(parts, " / ")
}

// joinTargets 는 대상 이름을 쉼표로 잇되 max 개를 넘으면 "외 N개" 로 줄인다(max<=0 이면 전부).
func joinTargets(ts []string, max int) string {
	if len(ts) == 0 {
		return "-"
	}
	if max <= 0 || len(ts) <= max {
		return strings.Join(ts, ", ")
	}
	return strings.Join(ts[:max], ", ") + fmt.Sprintf(" 외 %d개", len(ts)-max)
}

// WriteWorkerList 는 --run 승인 직후 작업자에게 보여주는 "내 작업 리스트"(INTERVIEW Q21):
// user 가 작업자인 작업의 ID, 확인자, 상태(색), 시각, 대상 VM 수와 이름. 최근 limit 건만(0 이면 전부).
func WriteWorkerList(w io.Writer, p Painter, user string, jobs []daemon.JobView, ttl time.Duration, now time.Time, limit int) {
	var mine []daemon.JobView
	for _, j := range jobs {
		if j.User == user {
			mine = append(mine, j)
		}
	}
	fmt.Fprintln(w, p.Bold(fmt.Sprintf("내 작업 리스트 (작업자 %s, %d건)", user, len(mine))))
	if len(mine) == 0 {
		fmt.Fprintln(w, "  (없음)")
		return
	}
	skipped := 0
	if limit > 0 && len(mine) > limit {
		skipped = len(mine) - limit
		mine = mine[skipped:]
	}
	rows := make([][]string, 0, len(mine))
	for _, j := range mine {
		at := fmtTime(j.CreatedAt)
		if j.ApprovedAt != nil {
			at = fmtTime(*j.ApprovedAt)
		}
		rows = append(rows, []string{j.ID, j.Checker, daemon.StatusLabel(j.Status, j.Phase), at,
			fmt.Sprintf("%d", j.TargetCount), joinTargets(j.Targets, 10) + "  [" + reasonText(j, ttl, now) + "]"})
	}
	table(w, "  ", []string{"ID", "확인자", "상태", "시각(승인/생성)", "대상수", "대상 VM [사유]"}, rows,
		func(row, col int, cell string) string {
			if col == 2 {
				return p.Label(mine[row].Status, mine[row].Phase, cell)
			}
			return cell
		})
	if skipped > 0 {
		fmt.Fprintf(w, "  (이전 작업 %d건 생략 — 전체는 cross_check --user %s)\n", skipped, user)
	}
}
