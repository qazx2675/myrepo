// done_tui.go - d 키(v0.7.1): 화면 2 에서 수동 완료. 안내 줄 아래 인라인 프롬프트(1 이 호스트 / 2 그룹 미완료) → 확인(y/n) → 실행.
// 실행은 CLI `auto_setup done` 과 같다: 로컬 데몬이면 markDone(DoneSrcManual), 원격 클라이언트(os6_mgmt)면 `done` 중계(remoteRun).
// 부팅 시각 검증이 없고 체크스크립트도 실행하지 않으므로 반드시 확인 단계를 거친다.
package main

import "fmt"

// doneState: d 프롬프트 상태. 대상 호스트 목록은 상태에 저장하지 않고 그리기·실행 때마다 현재 스냅샷으로 다시 구한다
// (그 사이 완료된 호스트를 다시 건드리지 않게).
type doneState struct {
	confirm bool   // false = 1/2 선택 중, true = y/n 확인 중
	kind    int    // 1 = 이 호스트, 2 = 그룹 미완료 전체
	host    string // 프롬프트를 연 시점의 커서 호스트
}

const doneHintStyle = "1;38;5;175" // hintStyles["d"] 와 같은 색

// doneTargets: 현재 스냅샷 기준 완료 처리 대상 (kind 1 = d.host 가 미완료일 때, kind 2 = 그룹의 미완료 전체)
func (st *tuiState) doneTargets(kind int, host string) (j *SnapJob, g *SnapGroup, hosts []string) {
	j, g = findGroup(st.snap, st.sel.JobID, st.sel.Yml)
	if g == nil {
		return j, nil, nil
	}
	for _, h := range g.Hosts {
		if h.Stage == StageDone {
			continue
		}
		if kind == 2 && g.Yml != allView || kind == 1 && h.Host == host {
			hosts = append(hosts, h.Host)
		}
	}
	return j, g, hosts
}

// startDone: d 키 — 선택지가 있으면 프롬프트를 연다, 없으면(종료된 작업·전부 완료) 안내만
func (st *tuiState) startDone() {
	j, g := findGroup(st.snap, st.sel.JobID, st.sel.Yml)
	if j == nil || g == nil {
		return
	}
	if j.Closed {
		st.sel.Msg = "종료된 작업은 완료 처리할 수 없습니다"
		return
	}
	name := ""
	if hs := visibleHosts(g, st.sel.Filter); st.sel.Host >= 0 && st.sel.Host < len(hs) {
		name = hs[st.sel.Host].Host
	}
	_, _, one := st.doneTargets(1, name)
	_, _, grp := st.doneTargets(2, "")
	switch {
	case len(one) == 0 && len(grp) == 0 && name != "" && g.Yml == allView:
		st.sel.Msg = "이미 완료된 호스트입니다 (" + cleanText(name) + ") - 완료 처리할 대상이 없습니다"
	case len(one) == 0 && len(grp) == 0 && name != "":
		st.sel.Msg = "이 호스트(" + cleanText(name) + ")와 그룹 모두 이미 완료입니다 - 완료 처리할 대상이 없습니다"
	case len(one) == 0 && len(grp) == 0:
		st.sel.Msg = "완료 처리할 호스트가 없습니다"
	default:
		st.done = &doneState{host: name}
	}
}

// handleDone: d 프롬프트의 키 (다른 모든 키는 이 안에서 소비 — x/w/m 과 동시에 열리지 않음)
func (st *tuiState) handleDone(ev keyEv) {
	d := st.done
	if d.confirm {
		st.done = nil
		if ev.k == kRune && (ev.r == 'y' || ev.r == 'Y') {
			st.runDone(d)
		} else {
			st.sel.Msg = "취소했습니다"
		}
		return
	}
	switch {
	case ev.k == kEsc || (ev.k == kRune && (ev.r == 'q' || ev.r == 'Q' || ev.r == 'd' || ev.r == 'D')):
		st.done = nil
		st.sel.Msg = "취소했습니다"
	case ev.k == kRune && (ev.r == '1' || ev.r == '2'):
		kind := int(ev.r - '0')
		if _, _, hs := st.doneTargets(kind, d.host); len(hs) > 0 {
			d.kind, d.confirm = kind, true
		}
	}
}

// runDone: y 확인 후 실행. 결과는 메시지 줄에.
func (st *tuiState) runDone(d *doneState) {
	j, g, hosts := st.doneTargets(d.kind, d.host)
	if g == nil || len(hosts) == 0 {
		st.sel.Msg = "완료 처리할 호스트가 없습니다 (그 사이 완료되었거나 작업이 사라짐)"
		return
	}
	var err error
	if st.changeRemote() { // os6_mgmt 클라이언트: os8_mgmt 의 `auto_setup done` 으로 중계 (큰 그룹은 호스트 나열 대신 --job/--yml)
		args := []string{"done"}
		if d.kind == 2 {
			args = append(args, "--job", j.ID, "--yml", g.Yml)
		} else {
			args = append(args, hosts...)
		}
		var r remoteResult
		if r, err = remoteRun(args); err == nil && r.RC != 0 {
			err = remoteFail("done", r)
		}
	} else {
		err = markDone(hosts, DoneSrcManual)
	}
	if err != nil {
		st.sel.Msg = "[X] 완료 처리 실패: " + err.Error()
		return
	}
	st.sel.Msg = fmt.Sprintf("완료 처리 요청: %d대 (데몬이 5초 안에 반영)", len(hosts))
}

// doneLines: 안내 줄 아래 프롬프트 줄들 (selection.Inline). 열려 있지 않으면 nil.
func (st *tuiState) doneLines(width int, color bool) []string {
	d := st.done
	if d == nil {
		return nil
	}
	width = clampWidth(width)
	_, g, one := st.doneTargets(1, d.host)
	_, _, grp := st.doneTargets(2, "")
	if g == nil {
		return nil
	}
	if !d.confirm {
		segs := []seg{{" ", ""}, {"수동 완료 ▶ ", doneHintStyle}}
		if len(one) > 0 {
			segs = append(segs, seg{"1", "1;32"}, seg{" 이 호스트(" + d.host + ")", stBold})
		}
		if len(grp) > 0 {
			if len(one) > 0 {
				segs = append(segs, seg{"  ", ""})
			}
			segs = append(segs, seg{"2", "1;32"}, seg{fmt.Sprintf(" 그룹 미완료 %d대", len(grp)), stBold})
		}
		segs = append(segs, seg{"   ", ""}, seg{"(Esc 취소)", stGray})
		return wrapSegs(color, width, 2, "   ", segs...)
	}
	desc := "호스트 " + d.host
	if d.kind == 2 {
		desc = fmt.Sprintf("그룹 %s 의 미완료 %d대", g.Yml, len(grp))
	}
	return wrapSegs(color, width, 1, "   ", seg{" " + desc + " 을(를) ", stBold},
		seg{"완료로 처리합니다 — 부팅 시각 검증 없이 인정되고 체크스크립트는 실행되지 않습니다.", stRed},
		seg{" 계속? (y/n)", stYellow})
}
