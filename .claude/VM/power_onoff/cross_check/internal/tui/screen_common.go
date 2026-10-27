package tui

// screen_common.go - 사용자·관리자 전체 화면이 함께 쓰는 조각: 머리말(제목·갱신 시각·데몬 상태·카운터), 상태 표기·색, 작업 표.

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"cross_check/internal/daemon"
	"cross_check/internal/store"
)

// stateKind 는 카운터·색에 쓰는 상태 구분(상태 + 단계).
type stateKind int

const (
	skWaiting  stateKind = iota // 대기
	skApproved                  // 승인(본 작업 시작 전)
	skWorking                   // 승인/작업중
	skDone                      // 승인/작업완료
	skStopped                   // 승인/작업중단
	skAborted                   // 중단
	skCount
)

// kindOf 는 작업의 상태 구분.
func kindOf(st store.Status, ph store.Phase) stateKind {
	switch st {
	case daemon.StatusWaiting:
		return skWaiting
	case daemon.StatusAborted:
		return skAborted
	}
	switch ph {
	case daemon.PhaseWorking:
		return skWorking
	case daemon.PhaseDone:
		return skDone
	case daemon.PhaseAborted:
		return skStopped
	}
	return skApproved
}

// kindStyle 은 상태 구분의 색: 대기 노랑, 승인 청록, 작업중 굵은 청록, 작업완료 초록, 작업중단 주황, 중단 빨강.
var kindStyle = [skCount]string{stYellow, stCyan, stCyanB, stGreen, stOrange, stRed}

// counterName 은 카운터 줄의 이름(승인 = 승인됐고 본 작업 시작 전).
var counterName = [skCount]string{"대기", "승인", "작업중", "작업완료", "작업중단", "중단"}

// counterSegs 는 " 전체 N  대기 N  승인 N  작업중 N  작업완료 N  작업중단 N  중단 N" (0 이면 회색).
func counterSegs(kinds []stateKind) []seg {
	var n [skCount]int
	for _, k := range kinds {
		n[k]++
	}
	out := []seg{{" ", ""}, {fmt.Sprintf("전체 %d", len(kinds)), stBold}}
	for k := stateKind(0); k < skCount; k++ {
		st := kindStyle[k]
		if n[k] == 0 {
			st = stGray
		}
		out = append(out, seg{"  ", ""}, seg{fmt.Sprintf("%s %d", counterName[k], n[k]), st})
	}
	return out
}

// daemonState 는 머리말 오른쪽에 보이는 데몬 상태.
type daemonState struct {
	alive bool      // 마지막 갱신에서 ping 이 성공
	pid   int       // ping 의 pid(구버전 데몬이면 0)
	at    time.Time // 마지막으로 성공한 갱신 시각(0 이면 아직 없음)
}

// titleLines 는 머리말 줄들: [제목 + 사용자 … 갱신 HH:MM:SS  ● 데몬 실행 (pid N)] / [카운터 … extra] (좁으면 줄바꿈).
func titleLines(color bool, width int, title, who string, ds daemonState, loc *time.Location, counters, extra []seg) []string {
	left := []seg{{" " + title, stBold}}
	if who != "" {
		left = append(left, seg{"  (" + who + ")", stYelB})
	}
	clock := "--:--:--"
	if !ds.at.IsZero() {
		clock = ds.at.In(loc).Format("15:04:05")
	}
	var dm seg
	if ds.alive {
		t := "● 데몬 실행"
		if ds.pid > 0 {
			t += fmt.Sprintf(" (pid %d)", ds.pid)
		}
		dm = seg{t, stGreenB}
	} else {
		dm = seg{"○ 데몬 중지", stRed}
	}
	right := []seg{{"갱신 " + clock, ""}, {"  ", ""}, dm}
	var out []string
	lw, rw := strWidth(segsText(left)), strWidth(segsText(right))
	if lw+2+rw+1 <= width {
		out = append(out, segLine(color, width, append(append(left, seg{strings.Repeat(" ", width-lw-rw-1), ""}), right...)...))
	} else { // 좁으면 갱신 시각·데몬 상태를 다음 줄로
		out = append(out, wrapSegs(color, width, 1, "   ", left...)...)
		out = append(out, wrapSegs(color, width, 2, " ", append([]seg{{" ", ""}}, right...)...)...)
	}
	cw, ew := strWidth(segsText(counters)), strWidth(segsText(extra))
	if len(extra) > 0 && cw+3+ew+1 <= width {
		out = append(out, segLine(color, width, append(append(counters, seg{strings.Repeat(" ", width-cw-ew-1), ""}), extra...)...))
	} else {
		out = append(out, wrapSegs(color, width, 2, " ", counters...)...)
		if len(extra) > 0 {
			out = append(out, wrapSegs(color, width, 2, " ", append([]seg{{" ", ""}}, extra...)...)...)
		}
	}
	return out
}

// shortTime 은 표의 시각 "01-02 15:04" (zero 면 "-").
func shortTime(t time.Time, loc *time.Location) string {
	if t.IsZero() {
		return "-"
	}
	return t.In(loc).Format("01-02 15:04")
}

// longTime 은 상세 화면의 시각 "2006-01-02 15:04:05" (nil/zero 면 "-").
func longTime(t *time.Time, loc *time.Location) string {
	if t == nil || t.IsZero() {
		return "-"
	}
	return t.In(loc).Format("2006-01-02 15:04:05")
}

// callerText 는 표·상세의 호출 스크립트(없으면 "-").
func callerText(c string) string {
	if strings.TrimSpace(c) == "" {
		return "-"
	}
	return c
}

// tableRow 는 작업 표 한 줄의 내용.
type tableRow struct {
	id, worker, checker, label, caller, at, extra string
	kind                                          stateKind
}

// jobTable 은 작업 표를 블록(행 하나 = 블록 하나)으로 그린다. 첫 블록은 표 머리.
// 열: ID, 작업자, 확인자, 상태, 호출 스크립트, [extraHead], 시각. 호출 스크립트 칸이 너무 좁으면 2줄 카드형으로 바꾼다.
// sel 은 선택된 행 번호(-1 = 없음).
func jobTable(color bool, width int, rows []tableRow, sel int, extraHead string) [][]string {
	idW, wkW, ckW, stW, exW := strWidth("ID"), strWidth("작업자"), strWidth("확인자"), strWidth("상태"), 0
	if extraHead != "" {
		exW = strWidth(extraHead)
	}
	for _, r := range rows {
		idW = rMax(idW, strWidth(r.id))
		wkW = rMax(wkW, strWidth(r.worker))
		ckW = rMax(ckW, strWidth(r.checker))
		stW = rMax(stW, strWidth(r.label))
		if extraHead != "" {
			exW = rMax(exW, strWidth(r.extra))
		}
	}
	idW, wkW, ckW = rMin(idW, 16), rMin(wkW, 12), rMin(ckW, 12)
	const tmW = 11 // "01-02 15:04"
	fixed := 2 + idW + 2 + wkW + 2 + ckW + 2 + stW + 2 + 2 + tmW
	if extraHead != "" {
		fixed += exW + 2
	}
	callerW := width - fixed
	card := callerW < 10
	var out [][]string
	if card {
		out = append(out, []string{segLine(color, width, seg{"  " + padR("ID", idW) + "  " + padR("상태", stW) + "  " + "시각", stHead})})
	} else {
		h := "  " + padR("ID", idW) + "  " + padR("작업자", wkW) + "  " + padR("확인자", ckW) + "  " + padR("상태", stW) + "  " +
			padR("호출 스크립트", callerW) + "  "
		if extraHead != "" {
			h += padL(extraHead, exW) + "  "
		}
		out = append(out, []string{segLine(color, width, seg{h + "시각", stHead})})
	}
	for i, r := range rows {
		mark := "  "
		if i == sel {
			mark = "> "
		}
		var lines [][]seg
		if card {
			lines = append(lines, []seg{{mark + padR(r.id, idW) + "  ", ""}, {padR(r.label, stW), kindStyle[r.kind]}, {"  " + r.at, stGray}})
			l2 := "    " + cleanText(r.worker) + " → " + cleanText(r.checker) + "  " + cleanText(r.caller)
			if extraHead != "" {
				l2 += "  " + extraHead + " " + r.extra
			}
			lines = append(lines, []seg{{l2, ""}})
		} else {
			l := []seg{{mark + padR(r.id, idW) + "  " + padR(r.worker, wkW) + "  " + padR(r.checker, ckW) + "  ", ""},
				{padR(r.label, stW), kindStyle[r.kind]}, {"  " + padR(r.caller, callerW) + "  ", ""}}
			if extraHead != "" {
				l = append(l, seg{padL(r.extra, exW) + "  ", ""})
			}
			lines = append(lines, append(l, seg{r.at, stGray}))
		}
		var blk []string
		for _, l := range lines {
			if i == sel && color {
				blk = append(blk, selLine(color, width, l))
			} else {
				blk = append(blk, segLine(color, width, l...))
			}
		}
		out = append(out, blk)
	}
	return out
}

// ttlSegs 는 머리말 오른쪽의 "승인 대기 10분 · 승인 유효시간 2분" (+ 관리자는 키 이름과 작업 제한).
// wait 가 0 이하면(구버전 데몬: 대기 제한 없음) 승인 대기 부분을 빼고 그린다.
func ttlSegs(wait, ttl, work time.Duration, admin bool) []seg {
	if ttl <= 0 {
		ttl = daemon.DefaultApprovalTTL
	}
	var out []seg
	if wait > 0 {
		t, sep := "승인 대기 "+daemon.FormatTTLDuration(wait), " · "
		if admin {
			// 관리자 줄은 길어서 좁은 화면에서 항목 단위로 접히도록 두 칸(wrapSegs 의 gap)으로 구분한다
			t, sep = t+" (wait_timeout_sec="+strconv.Itoa(int(wait/time.Second))+")", "  "
		}
		out = append(out, seg{t, stGray}, seg{sep, stGray})
	}
	ti := len(out)
	out = append(out, seg{"승인 유효시간 " + daemon.FormatTTLDuration(ttl), stGray})
	if admin {
		out[ti].t += " (approval_ttl_sec=" + strconv.Itoa(int(ttl/time.Second)) + ")"
		if work <= 0 {
			work = daemon.DefaultWorkTimeout
		}
		out = append(out, seg{"  ", ""}, seg{"작업 제한 " + daemon.FormatTTLDuration(work) + " (work_timeout_sec=" + strconv.Itoa(int(work/time.Second)) + ")", stGray})
	}
	return out
}

// msgLine 은 안내 줄(노랑 / 오류 빨강 / 성공 초록). 빈 글이면 nil.
func msgLines(color bool, width int, msg string, kind msgKind) []string {
	if msg == "" {
		return nil
	}
	st := stYellow
	switch kind {
	case msgErr:
		st = stRed
	case msgOK:
		st = stGreen
	}
	return wrapText(color, width, st, "   ", " "+msg)
}

type msgKind int

const (
	msgInfo msgKind = iota
	msgOK
	msgErr
)
