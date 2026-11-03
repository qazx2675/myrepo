// change_render.go - m 메뉴(변경 흐름) 화면 문자열: 원복 종류·user·infra·입력·dry-run·미리보기·실행·결과.
// 긴 줄은 … 로 자르지 않고 줄바꿈한다(wrapSegs). 위험 문구는 빨강(stRed), 경고는 노랑. 사용자 문구는 한국어.
package main

import (
	"fmt"
	"strings"
	"time"
)

// renderChange: 변경 흐름 전체 화면. height>0 이면 본문을 창 높이에 맞춘다(미리보기·결과는 c.scroll, 입력·실행은 끝부분).
func renderChange(c *changeState, width, height int, color bool) string {
	width = clampWidth(width)
	var head, body, foot []string
	add := func(dst *[]string, segs ...seg) { *dst = append(*dst, wrapSegs(color, width, 1, "   ", segs...)...) }
	line := func(dst *[]string, st, t string) { add(dst, seg{" " + t, st}) }
	blank := func(dst *[]string) { *dst = append(*dst, "") }
	title, hint, follow := c.kind.title(), "", false
	who := ""
	if c.user != "" {
		who = " - user " + c.user
		if c.infra != "" {
			who += " - infra " + c.infra
		}
	}

	switch c.step {
	case csFatal:
		line(&body, stRed, c.fatal)
		hint = " Esc 닫기"
	case csRbSel:
		title += " - 종류 선택"
		line(&body, "", "각 노드의 가장 최근 백업으로 되돌립니다 (서비스 재시작 안 함)")
		blank(&body)
		for _, o := range c.rbOpts {
			line(&body, stGreen, "  "+string(o.key)+" "+o.label)
		}
		hint = " 번호 선택  Esc 취소"
	case csUser:
		title += " - user 선택"
		if u := c.env.User; u != nil && u.menu != nil {
			for _, m := range u.menu {
				line(&body, "", m)
			}
			blank(&body)
			line(&body, "", "번호 입력 > "+string(c.in)+"_")
			hint = " Enter 선택  Esc 취소 (입력이 비었을 때 q 도 취소)"
		} else {
			line(&body, "", "user 이름 입력 > "+string(c.in)+"_")
			hint = " Enter 확인  Esc 취소 (입력이 비었을 때 q 도 취소)"
		}
	case csInfra:
		title += who + " - infra 선택"
		line(&body, "", "ldap_config.conf 의 infra:")
		for i, n := range c.infras {
			line(&body, stGreen, fmt.Sprintf("  %d %s", i+1, n))
		}
		blank(&body)
		line(&body, "", "번호 입력 > "+string(c.in)+"_")
		hint = " Enter 선택  Esc 취소"
	case csInput:
		title += who + " - 대상 입력"
		follow = true
		c.inputHead(&head, line)
		body = c.inputBody(color, width)
		blank(&foot)
		cur := string(c.in)
		if c.prefilled {
			cur = ""
		} else if i := strings.LastIndex(cur, "\n"); i >= 0 {
			cur = cur[i+1:]
		}
		line(&foot, "", "> "+cur+"_")
		hint = " Enter 줄바꿈  Ctrl+D 입력 완료  Esc 취소 (입력이 비었을 때 q 도 취소)"
	case csDry, csRun:
		follow = true
		if c.step == csDry {
			title += who + " - 미리보기 조회(dry-run) 중"
		} else {
			title += who + fmt.Sprintf(" - 실행 중 (%d대)", len(c.hosts))
		}
		el := 0
		if c.env.Now != nil {
			el = int(c.env.Now().Sub(c.started) / time.Second)
		}
		if el < 0 {
			el = 0
		}
		phase, tail := "", []string(nil)
		if c.job != nil {
			phase, tail = c.job.progress()
		}
		line(&head, "", fmt.Sprintf("경과 %d분 %02d초   %s", el/60, el%60, phase))
		if c.step == csRun {
			line(&head, stRed, "실행 중 — 노드 설정을 바꾸고 있습니다 (서비스 재시작 안 함)")
		}
		head = append(head, "")
		for _, l := range tail {
			line(&body, "", l)
		}
		hint = " 실행 중 — 취소: Ctrl+X"
		if c.cancelAsk {
			hint = " 실행을 취소할까요? (일부 호스트만 바뀐 상태로 남을 수 있음) [y/n]"
		}
	case csPreview:
		title += who + " - 미리보기"
		body = c.previewBody(color, width)
		blank(&foot)
		add(&foot, seg{" " + fmt.Sprintf("yes 를 입력하면 %d대에 실제로 적용합니다 (다른 입력은 취소)", len(c.hosts)), stRed})
		line(&foot, "", "확인 > "+string(c.in)+"_")
		hint = " yes Enter 진행  다른 입력 Enter·Esc 취소  ↑↓ 스크롤"
	case csResult:
		title += who + fmt.Sprintf(" - 결과 (%d대)", len(c.results))
		body = c.resultBody(color, width)
		hint = " ↑↓ j k 스크롤  Space/b 페이지  q/Esc/Enter 닫기"
	}

	top := append(wrapSegs(color, width, 1, "   ", seg{" " + title, stBold}), sepLine(width))
	head = append(top, head...)
	f := []string{sepLine(width)}
	f = append(foot, f...)
	if c.msg != "" {
		f = append(f, wrapSegs(color, width, 1, "   ", seg{" " + c.msg, stYellow})...)
	}
	f = append(f, hintLines(color, width, hint)...)

	if height > 0 {
		room := height - len(head) - len(f)
		if room < 1 {
			room = 1
		}
		if follow {
			if len(body) > room {
				body = append([]string{" …"}, body[len(body)-room+1:]...)
			}
		} else {
			c.maxScroll = rMax(len(body)-room, 0)
			if c.scroll > c.maxScroll {
				c.scroll = c.maxScroll
			}
			if c.scroll < 0 {
				c.scroll = 0
			}
			body = body[c.scroll:rMin(len(body), c.scroll+room)]
		}
		for len(body) < room {
			body = append(body, "")
		}
	}
	out := append(head, body...)
	return strings.Join(append(out, f...), "\n")
}

// inputHead: 입력 안내 (종류별)
func (c *changeState) inputHead(head *[]string, line func(*[]string, string, string)) {
	switch c.kind {
	case kindIP, kindBoth:
		line(head, "", "한 줄에 'hostname 새IP' 를 입력하거나 붙여넣은 뒤 Ctrl+D (공백·탭·쉼표·=·-> 구분 가능)")
		line(head, "", "GW 는 새 IP 의 마지막 자리를 1 로 (x.x.x.1) — ifcfg 만 바꾸고 네트워크를 재시작하지 않습니다")
		if c.kind == kindBoth {
			line(head, "", "IP 먼저 바꾸고, IP 가 성공한 호스트만 LDAP 을 적용합니다 (다음 단계에서 LDAP dry-run 확인)")
		}
	case kindLDAP:
		line(head, "", "hostname 을 입력하거나 붙여넣은 뒤 Ctrl+D (공백·쉼표·탭·| 구분 가능)")
		line(head, "", "site 는 assets.txt 로 판정 — 다음 단계에서 dry-run 으로 바뀔 내용을 먼저 확인합니다")
	default:
		line(head, "", "대상 호스트 — user 기록에서 자동으로 채웠습니다 (그대로 Ctrl+D)")
		line(head, "", "새로 입력·붙여넣기하면 목록을 대체, Backspace 로 편집")
	}
	*head = append(*head, "")
}

// inputBody: 지금까지 입력된 대상 (가로로 채워 여러 줄) + 제외·경고 수
func (c *changeState) inputBody(color bool, width int) []string {
	raw := string(c.in)
	var items, warns []string
	switch c.kind {
	case kindIP, kindBoth:
		es, w := parseIPInput(raw)
		for _, e := range es {
			items = append(items, e.Host+"→"+e.IP)
		}
		warns = w
	default:
		items, warns = parseHostInput(raw)
	}
	label := "입력된 대상"
	if c.prefilled {
		label = "기록에서 채운 대상"
	}
	out := []string{segLine(color, width, seg{fmt.Sprintf(" %s %d대:", label, len(items)), ""})}
	if len(items) > 0 { // 두 칸 간격으로 가로로 채우고 폭을 넘으면 항목 단위로 줄바꿈 (긴 항목은 글자 단위)
		out = append(out, wrapSegs(color, width, 2, "   ", seg{"   " + strings.Join(items, "  "), ""})...)
	}
	if len(warns) > 0 {
		out = append(out, wrapSegs(color, width, 1, "   ", seg{fmt.Sprintf(" 제외·경고 %d건: %s", len(warns), warns[len(warns)-1]), stYellow})...)
	}
	return out
}

// previewBody: 미리보기 본문
func (c *changeState) previewBody(color bool, width int) []string {
	var out []string
	add := func(segs ...seg) { out = append(out, wrapSegs(color, width, 1, "      ", segs...)...) }
	line := func(st, t string) { add(seg{" " + t, st}) }
	ipKind := c.kind == kindIP || c.kind == kindBoth

	line(stBold, fmt.Sprintf("대상 %d대", len(c.hosts)))
	switch {
	case ipKind:
		line("", "서비스 재시작 안 함 · IP 는 재부팅/네트워크 재시작 후 반영 · 현재 IP 는 실행 때 노드에서 확인")
	case c.kind == kindLDAP:
		line("", "서비스 재시작 안 함 · auto.appl 은 /etc/auto.appl.bak 에 대기 (반영은 m 메뉴의 auto.appl 원복)")
	case c.kind == kindRollback:
		line("", "각 노드의 가장 최근 백업으로 되돌림 · 서비스 재시작 안 함")
		if c.rbIP {
			line("", "IP 원복도 ifcfg 만 되돌립니다 — 재부팅/네트워크 재시작 후 반영")
		}
	case c.kind == kindApplRestore:
		line("", "노드의 "+applScriptPath+" 만 실행 (/etc/auto.appl.bak → /etc/auto.appl) · 서비스 재시작 안 함")
	}
	// 진행 중 job 경고 (job·snapshot·데몬은 건드리지 않음)
	var tr []string
	for _, h := range c.hosts {
		if jy, ok := c.env.Tracked[h]; ok {
			tr = append(tr, h+"("+jy+")")
		}
	}
	if len(tr) > 0 {
		msg := fmt.Sprintf("%d대가 진행 중 job 에 있음 — ", len(tr))
		if ipKind || (c.kind == kindRollback && c.rbIP) {
			msg += "변경 IP 반영 후 추적이 끊길 수 있음"
		} else {
			msg += "진행 중인 설치·체크와 겹칠 수 있음"
		}
		line(stRed, msg)
		show := tr
		if len(show) > 10 {
			show = append(append([]string(nil), tr[:10]...), fmt.Sprintf("외 %d대", len(tr)-10))
		}
		line(stRed, "  "+strings.Join(show, " "))
	}
	for _, w := range c.warns {
		line(stYellow, "제외·경고: "+w)
	}
	out = append(out, "")

	switch {
	case ipKind:
		hw := 0
		for _, e := range c.entries {
			hw = rMax(hw, strWidth(e.Host))
		}
		hw = rMin(hw, 30)
		for _, e := range c.entries {
			segs := []seg{{" " + padTo(e.Host, hw), ""}, {" → " + e.IP + " (GW " + e.GW + ")", stBold}}
			if c.kind == kindBoth {
				segs = append(segs, seg{"   LDAP ", ""})
				segs = append(segs, c.dryCell(e.Host)...)
			}
			add(segs...)
			if d := c.dryDetail(e.Host); d != "" {
				add(seg{"      └ " + d, stYellow})
			}
		}
	case c.kind == kindLDAP:
		hw := 0
		for _, h := range c.hosts {
			hw = rMax(hw, strWidth(h))
		}
		hw = rMin(hw, 30)
		for _, h := range c.hosts {
			add(append([]seg{{" " + padTo(h, hw) + "  ", ""}}, c.dryCell(h)...)...)
			if d := c.dryDetail(h); d != "" {
				add(seg{"      └ " + d, stYellow})
			}
		}
	default:
		add(seg{" " + strings.Join(c.hosts, "  "), ""})
	}
	if c.dry != nil {
		out = append(out, "")
		line("", "LDAP dry-run: "+countLine(c.dryList(), func(h *chgHost) string { return h.LDAPResult }))
	}
	return out
}

func (c *changeState) dryList() []*chgHost {
	var l []*chgHost
	for _, h := range c.hosts {
		if x := c.dry[h]; x != nil {
			l = append(l, x)
		}
	}
	return l
}

func (c *changeState) dryCell(h string) []seg {
	x := c.dry[h]
	if x == nil {
		return []seg{{"미조회", stGray}}
	}
	t, st := rsLabel(x.LDAPResult)
	if x.LDAPResult == rsChanged {
		t, st = "CHANGED(바뀔 예정)", stCyan
	}
	return []seg{{t, st}}
}

func (c *changeState) dryDetail(h string) string {
	if x := c.dry[h]; x != nil && x.LDAPResult != rsChanged && x.LDAPResult != rsNoChange {
		return strings.TrimPrefix(x.Detail, "LDAP: ")
	}
	return ""
}

// rsLabel: 결과 상태 → 화면 문구·색 (OK 초록, FAIL·응답 없음 굵은 빨강, 건너뜀·미실행 회색, NOCHANGE 기본)
func rsLabel(st string) (string, string) {
	switch st {
	case rsOK:
		return "OK", stGreen
	case rsChanged:
		return "CHANGED", stCyan
	case rsNoChange:
		return "NOCHANGE", ""
	case rsFail:
		return "FAIL", stRed
	case rsUnreach:
		return "응답 없음", stRed
	case rsNoResult:
		return "결과 없음", stRed
	case rsNoBackup:
		return "백업 없음", stYellow
	case rsSkipIP:
		return "건너뜀(IP 실패)", stGray
	case rsSkipAsset:
		return "건너뜀(자산현황 없음)", stGray
	case rsCancel:
		return "미확인(취소)", stYellow
	case rsNoPending:
		return "대기 없음", ""
	case "":
		return "미실행", stGray
	}
	return st, ""
}

// countLine: 상태별 대수 "OK 3 · FAIL 1 …" (고정 순서)
func countLine(hs []*chgHost, get func(*chgHost) string) string {
	n := map[string]int{}
	for _, h := range hs {
		n[get(h)]++
	}
	var parts []string
	for _, st := range []string{rsOK, rsChanged, rsNoChange, rsNoPending, rsFail, rsUnreach, rsNoResult, rsNoBackup, rsSkipIP, rsSkipAsset, rsCancel, ""} {
		if n[st] > 0 {
			t, _ := rsLabel(st)
			parts = append(parts, fmt.Sprintf("%s %d", t, n[st]))
		}
	}
	return strings.Join(parts, " · ")
}

// resultBody: 결과 화면 본문 — 호스트별 한 줄(IP/LDAP 칸) + 상세(들여쓰기), 요약·안내
func (c *changeState) resultBody(color bool, width int) []string {
	var out []string
	add := func(segs ...seg) { out = append(out, wrapSegs(color, width, 2, "      ", segs...)...) }
	prose := func(st, t string) { out = append(out, wrapSegs(color, width, 1, "   ", seg{" " + t, st})...) }
	type col struct {
		name string
		get  func(*chgHost) string
	}
	var cols []col
	ipCol := col{"IP", func(h *chgHost) string { return h.IPResult }}
	ldCol := col{"LDAP", func(h *chgHost) string { return h.LDAPResult }}
	switch c.kind {
	case kindIP:
		cols = []col{ipCol}
	case kindLDAP:
		cols = []col{ldCol}
	case kindBoth:
		cols = []col{ipCol, ldCol}
	case kindRollback:
		if c.rbIP {
			cols = append(cols, col{"IP 원복", ipCol.get})
		}
		if c.rbLDAP {
			cols = append(cols, col{"LDAP 원복", ldCol.get})
		}
	case kindApplRestore:
		cols = []col{{"auto.appl", func(h *chgHost) string { return h.ApplResult }}}
	}
	if c.cancelled {
		prose(stRed, "취소됨 — 일부 호스트만 변경됐을 수 있음 (아래 호스트별 결과를 확인하십시오)")
		out = append(out, "")
	}
	hw := 0
	for _, h := range c.results {
		hw = rMax(hw, strWidth(h.Host))
	}
	hw = rMin(hw, 30)
	pending := 0
	for _, h := range c.results {
		if h.ApplPending {
			pending++
		}
		segs := []seg{{" " + padTo(h.Host, hw), ""}}
		for _, cl := range cols {
			t, st := rsLabel(cl.get(h))
			segs = append(segs, seg{"  " + cl.name + " ", ""}, seg{t, st})
			if cl.name == "IP" && h.IPResult == rsOK && h.NewIP != "" {
				info := h.NewIP
				if h.OldIP != "" {
					info = h.OldIP + " → " + h.NewIP
				}
				if h.GW != "" {
					info += " (GW " + h.GW + ")"
				}
				segs = append(segs, seg{" " + info, ""})
			}
		}
		if h.Route == "os6" {
			segs = append(segs, seg{"  os6경유", stGray})
		}
		add(segs...)
		if h.Detail != "" && hostOutcome(h) != "ok" {
			out = append(out, wrapSegs(color, width, 1, "        ", seg{"      └ " + h.Detail, stYellow})...)
		}
	}
	out = append(out, "")
	for _, cl := range cols {
		prose(stBold, cl.name+": "+countLine(c.results, cl.get))
	}
	if c.kind == kindIP || c.kind == kindBoth || (c.kind == kindRollback && c.rbIP) {
		prose("", "IP 는 ifcfg 만 바뀌었습니다 — 네트워크 재시작/재부팅 후 반영됩니다 (이 도구는 재시작하지 않음)")
	}
	if pending > 0 {
		prose(stYellow, fmt.Sprintf("auto.appl 대기 %d대 — m 메뉴의 auto.appl 원복으로 반영", pending))
	}
	prose("", "무작위로 몇 대를 직접 접속해 확인하십시오")
	switch {
	case c.recErr != "":
		prose(stRed, "기록 문제: "+c.recErr)
	case c.recPath != "":
		prose(stGray, "기록: "+c.recPath)
	}
	return out
}

// padTo: 폭 w 까지 공백을 채운다 (더 길면 자르지 않고 그대로 — 줄바꿈은 wrapSegs 가 한다)
func padTo(s string, w int) string {
	s = cleanText(s)
	if n := w - strWidth(s); n > 0 {
		s += strings.Repeat(" ", n)
	}
	return s
}
