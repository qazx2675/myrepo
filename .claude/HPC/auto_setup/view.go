// view.go - TUI 전체 화면 보기: 수동 실행(c/t) 결과, g(재확인) 진행 과정, v(최근 결과) — 데몬이 끝낼 때까지 실시간 갱신
package main

import (
	"fmt"
	"strings"
)

const (
	viewRun     = "run"     // 수동 실행 결과 (c / t / v)
	viewRecheck = "recheck" // g 재확인 진행 과정
)

// viewState: 전체 화면 보기 상태
type viewState struct {
	kind   string
	jobID  string
	yml    string
	mode   string // viewRun: 요청한 모드 (ModeCheck 또는 "")
	title  string
	status string   // 한 줄 상태 (대기 중 / 실행 중 / 완료 …)
	lines  []string // 본문
	scroll int
	follow bool // 끝을 따라감 (사용자가 위로 올리면 해제)
	done   bool // 더 이상 갱신하지 않음

	cleanup string // 닫을 때 삭제할 임시 디렉터리 (단독 실행 결과)

	baseRuns int   // viewRun: 요청 시점의 job.Runs 개수 (새로 생긴 수동 run 을 찾는 기준)
	prevAt   int64 // viewRecheck: 요청 시점의 Recheck.At (바뀌면 이번 요청의 기록)
	prevNone bool  // viewRecheck: 요청 시점에 기록이 없었음
}

func findSnapJob(s Snapshot, id string) *SnapJob {
	for i := range s.Jobs {
		if s.Jobs[i].ID == id {
			return &s.Jobs[i]
		}
	}
	return nil
}

// lastManualRun: 그룹(yml) 의 가장 최근 수동 run (since 번째 이후만). 없으면 nil.
func lastManualRun(j *SnapJob, yml string, since int) *Run {
	for i := len(j.Runs) - 1; i >= since && i >= 0; i-- {
		if r := j.Runs[i]; r.Manual && r.Yml == yml {
			return &j.Runs[i]
		}
	}
	return nil
}

// openRunView: c / t 요청 직후 — 결과가 나올 때까지 상태를 보여 주고 끝나면 결과(code 본문)를 보여 준다
func (st *tuiState) openRunView(mode string) {
	j := findSnapJob(st.snap, st.sel.JobID)
	v := &viewState{kind: viewRun, jobID: st.sel.JobID, yml: st.sel.Yml, mode: mode, follow: false,
		title: fmt.Sprintf("수동 실행 [%s] - %s", modeLabel(mode), st.sel.Yml)}
	if j != nil {
		v.baseRuns = len(j.Runs)
	}
	v.status = "요청 전달됨 - 데몬이 수거하기를 기다리는 중 (최대 5초)"
	st.view = v
	st.updateView()
}

// openLatestResult: v — 그룹의 가장 최근 수동 실행 결과
func (st *tuiState) openLatestResult() {
	j := findSnapJob(st.snap, st.sel.JobID)
	if j == nil || st.sel.Yml == allView {
		st.sel.Msg = "그룹별 화면에서 v 를 누르세요 (a 로 돌아가 그룹 선택)"
		return
	}
	r := lastManualRun(j, st.sel.Yml, 0)
	if r == nil {
		st.sel.Msg = "이 그룹의 수동 실행 결과가 아직 없습니다 (c 또는 t 로 실행)"
		return
	}
	v := &viewState{kind: viewRun, jobID: j.ID, yml: st.sel.Yml, mode: r.Mode, baseRuns: -1,
		title: fmt.Sprintf("최근 수동 실행 결과 [%s] - %s", modeLabel(r.Mode), st.sel.Yml)}
	st.view = v
	st.fillRunResult(v, r)
}

// openRecheckView: g 요청 직후 — 데몬이 기록하는 진행 과정을 실시간으로
func (st *tuiState) openRecheckView() {
	v := &viewState{kind: viewRecheck, jobID: st.sel.JobID, yml: st.sel.Yml, follow: true,
		title: "서버 상태 재확인 (g) - " + recheckTarget(st.sel.Yml)}
	if j := findSnapJob(st.snap, st.sel.JobID); j != nil && j.Recheck != nil {
		v.prevAt = j.Recheck.At
	} else {
		v.prevNone = true
	}
	v.status = "요청 전달됨 - 데몬이 수거하기를 기다리는 중 (최대 5초)"
	st.view = v
	st.updateView()
}

func recheckTarget(yml string) string {
	if yml == allView || yml == "" {
		return "job 전체 (완료 제외)"
	}
	return yml + " (완료 제외)"
}

// updateView: 새 스냅샷으로 보기 내용 갱신 (refresh 마다)
func (st *tuiState) updateView() {
	v := st.view
	if v == nil || v.done || v.kind == viewOneoff {
		return
	}
	j := findSnapJob(st.snap, v.jobID)
	if j == nil {
		v.status = "job 을 찾을 수 없습니다 (종료되었거나 취소됨)"
		v.done = true
		return
	}
	switch v.kind {
	case viewRecheck:
		rc := j.Recheck
		if rc == nil || (!v.prevNone && rc.At == v.prevAt) {
			return // 아직 이번 요청의 기록이 없음
		}
		v.lines = recheckLines(rc)
		if rc.Done {
			v.status = "완료"
			v.done = true
		} else {
			v.status = "진행 중…"
		}
	case viewRun:
		if v.baseRuns < 0 {
			return
		}
		if r := lastManualRun(j, v.yml, v.baseRuns); r != nil {
			st.fillRunResult(v, r)
			return
		}
		v.status = "요청 전달됨 - 데몬이 수거하기를 기다리는 중 (최대 5초)"
		for _, m := range j.Manual {
			if m.Yml != v.yml {
				continue
			}
			if m.State == "running" {
				v.status = "실행 중 - config_check 진행 중 (끝나면 이 화면에 결과가 표시됩니다)"
			} else {
				v.status = "대기 중 - 앞선 실행이 끝나면 시작합니다 (실행은 한 번에 하나씩)"
			}
		}
	}
}

// fillRunResult: run 의 code 본문을 읽어 보기에 채운다
func (st *tuiState) fillRunResult(v *viewState, r *Run) {
	text, err := st.src.Code(r.Code)
	v.done = true
	if err != nil {
		v.status = fmt.Sprintf("완료 (code %s) - 결과 파일을 읽지 못했습니다: %v", r.Code, err)
		v.lines = []string{"auto_setup code " + r.Code + " 로 다시 확인하세요."}
		return
	}
	v.status = fmt.Sprintf("완료 - code %s  (auto_setup code %s)", r.Code, r.Code)
	v.mode = r.Mode
	v.lines = splitLines(strings.TrimRight(text, "\n"))
	v.scroll = 0
}

// recheckLines: 진행 줄 + (끝나면) 호스트별 결과표
func recheckLines(rc *Recheck) []string {
	out := append([]string(nil), rc.Lines...)
	if rc.Done && len(rc.Hosts) > 0 {
		out = append(out, "", fmt.Sprintf("%-28s %-8s %s", "호스트", "결과", "상세"))
		for _, h := range rc.Hosts {
			res := map[string]string{"os8": "os8 응답", "os6": "os6 경유", "fail": "접속불가"}[h.Result]
			if res == "" {
				res = h.Result
			}
			out = append(out, fmt.Sprintf("%-28s %-8s %s", h.Host, res, h.Detail))
		}
	}
	return out
}

// ---- 키 / 렌더 ----

// handleView: 보기 화면 키. 닫으면 st.view=nil.
func (st *tuiState) handleView(ev keyEv) {
	v := st.view
	_, h := st.env.Size()
	page := rMax(h-5, 1)
	switch {
	case ev.k == kEsc || ev.k == kLeft || (ev.k == kRune && (ev.r == 'q' || ev.r == 'Q')):
		st.closeView()
	case ev.k == kUp || (ev.k == kRune && ev.r == 'k'):
		v.follow = false
		v.scroll = rMax(v.scroll-1, 0)
	case ev.k == kDown || (ev.k == kRune && ev.r == 'j'):
		v.scroll++
	case ev.k == kRune && ev.r == ' ':
		v.scroll += page
	case ev.k == kRune && ev.r == 'b':
		v.follow = false
		v.scroll = rMax(v.scroll-page, 0)
	case ev.k == kRune && (ev.r == 'G'):
		v.follow = true
	case ev.k == kRune && (ev.r == 'r' || ev.r == 'R'):
		st.refresh()
	case ev.k == kRune && ev.r == 'g' && v.kind == viewRecheck && v.done: // 같은 화면에서 다시 재확인
		st.requestRecheckAgain()
	}
}

// requestRecheckAgain: 재확인 보기에서 g 를 다시 누르면 새 요청
func (st *tuiState) requestRecheckAgain() {
	v := st.view
	st.view = nil
	st.sel.JobID, st.sel.Yml = v.jobID, v.yml
	st.requestRecheck()
}

func lineStyle(l string) string {
	// "무응답 0대" / "접속불가 0대" 는 문제가 없다는 뜻이므로 빨강으로 보이지 않게 한다
	clean := strings.NewReplacer("무응답 0대", "", "접속불가 0대", "").Replace(l)
	switch {
	case strings.Contains(clean, "FAIL") && !strings.Contains(clean, "NO FAIL :") || strings.Contains(clean, "접속불가") || strings.Contains(clean, "실패") || strings.Contains(clean, "무응답"):
		return stRed
	case strings.Contains(l, "NO FAIL"), strings.Contains(l, "os8 응답"), strings.Contains(l, "os6 경유"), strings.Contains(l, "최종 결과"):
		return stGreen
	case strings.HasPrefix(l, "=====") || strings.HasPrefix(l, "작업 :"):
		return stBold
	}
	return ""
}

func renderView(v *viewState, width, height int, color bool) string {
	width = clampWidth(width)
	head := []string{segLine(color, width, redSegs(" "+v.title, stBold)...),
		segLine(color, width, seg{" 상태: " + v.status, map[bool]string{true: stGreen, false: stCyan}[v.done]}),
		sepLine(width)}
	hint := " ↑↓ 스크롤  Space/b 쪽 이동  q/Esc 닫기"
	switch {
	case v.kind == viewRecheck && v.done:
		hint += "  g 다시 재확인"
	case !v.done:
		hint += "  (닫아도 데몬은 계속 진행합니다. 결과는 v 로 다시 볼 수 있습니다)"
	}
	foot := []string{sepLine(width), segLine(color, width, seg{hint, stBold})}
	avail := rMax(height-len(head)-len(foot), 1)
	if height <= 0 {
		avail = len(v.lines)
	}
	maxStart := rMax(len(v.lines)-avail, 0)
	start := v.scroll
	if v.follow {
		start = maxStart
	}
	if start > maxStart {
		start = maxStart
	}
	v.scroll = start
	body := make([]string, 0, avail)
	for i := start; i < len(v.lines) && len(body) < avail; i++ {
		body = append(body, segLine(color, width, redSegs(" "+v.lines[i], lineStyle(v.lines[i]))...))
	}
	for len(body) < avail && height > 0 {
		body = append(body, "")
	}
	return strings.Join(append(append(head, body...), foot...), "\n")
}

// renderRecheckSplit: g 재확인 화면 — 위에는 호스트표(단계가 갱신되는 것을 바로 확인), 아래에는 진행 과정·결과
func renderRecheckSplit(s Snapshot, sel selection, v *viewState, width, height int, color bool) string {
	topH := rMin(rMax(height/2, 12), 16)
	top := strings.Split(renderDetail(s, sel, width, topH, color), "\n")
	for len(top) < topH {
		top = append(top, "")
	}
	top = top[:topH]
	return strings.Join(top, "\n") + "\n" + renderView(v, width, height-topH, color)
}
