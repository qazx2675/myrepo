package main

// resultconv.go — 프로파일 점검 결과(hostCheck)와 전체비교 결과(allHost)를 화면·CSV 공통 줄(ResultLine)로 바꿉니다.
// 태그는 tagFor, 값 표시는 showValue("-"·빈 값 → (N/A)), 비고는 원래 상태명입니다 (계획서_v0.2_TUI.md).

import "sort"

// profileLines 는 프로파일 점검 결과를 호스트 입력 순서·항목 순서대로 줄로 바꿉니다.
// 호스트 단위 오류는 Attr "-" 인 HostError 줄 1개(태그 N/A, 비고 = 오류 상태)입니다.
func profileLines(hosts []*hostCheck) []ResultLine {
	var out []ResultLine
	for _, h := range hosts {
		base := ResultLine{
			Kind: KindProfile, Host: h.Target.Hostname, IP: dash(h.Target.IP),
			Vendor: "-", Model: "-", BiosVersion: "-", MgmtModel: dash(h.MgmtModel), MgmtVersion: dash(h.MgmtVersion),
		}
		if h.Info != nil {
			base.Vendor, base.Model, base.BiosVersion = dash(h.Info.Vendor), dash(h.Info.Model), dash(h.Info.BiosVersion)
		}
		if h.Status != "" {
			l := base
			l.Tag, l.Attr, l.Expected, l.Current, l.Note, l.HostError = TagNA, "-", naValue, naValue, withNote(h.Status, h.ProtoNote), true
			out = append(out, l)
			continue
		}
		for _, it := range h.Items {
			l := base
			if it.Server != "" { // XML 대상의 서버가 여럿: "<호스트>/<서버 dn>" 마다 따로
				l.Host, l.Model = it.Server, dash(it.Model)
			}
			l.Attr = it.Attr
			if l.Attr == "" || l.Attr == "-" { // 프로파일에 이 모델 행이 없어 속성을 모름 → 표준 항목 이름으로 보인다
				l.Attr = it.Std
			}
			l.Tag, l.Expected, l.Current, l.Note = tagFor(it.Result), showValue(it.Expected), showValue(it.Current), it.Result
			if it.Note != "" {
				l.Note = withNote(it.Result, it.Note)
			} else {
				l.Note = withNote(it.Result, h.ProtoNote)
			}
			out = append(out, l)
		}
	}
	return out
}

// withNote 는 비고(상태명) 뒤에 프로토콜 안내를 " [XML]" 처럼 붙입니다 (안내가 없으면 그대로).
func withNote(status, note string) string {
	if note == "" {
		return status
	}
	return status + " [" + note + "]"
}

// compareLines 는 전체비교 대상 1대를 기준(비교서버·refs JSON) 대비 속성마다 1줄로 바꿉니다 (이름순).
// 같은 값 → OK(SAME), 다른 값 → difference(DIFF), 기준에만 있음 → N/A(ONLY_REF, 현재값 (N/A)),
// 대상에만 있음 → difference(ONLY_HOST, 기준값 (없음)). ign 에 걸린 속성은 줄을 만들지 않습니다.
// 기준 호스트 자신(REFERENCE)은 자기 값을 기준값·현재값으로 한 OK 줄들(referenceLines)이고,
// 그 밖에 비교하지 못한 호스트(접속 실패, 기준 없음 등)는 HostError 줄 1개입니다.
// h.Snap.Attrs 와 h.Ref.Snap.Attrs 가 있어야 하므로 allOpts.OnHost 안에서 부르십시오 (doAllCheck 는 그 뒤 대상 속성을 버림).
// 관리 컨트롤러(MgmtModel·MgmtVersion)는 대상 스냅샷의 값입니다: 프로파일 점검과 함께 읽은 스냅샷(unified.go)은 채워져 있고,
// allcheck 는 Manager 를 읽지 않아 "-" 입니다.
func compareLines(h *allHost, ign *ignoreSet) []ResultLine {
	base := ResultLine{
		Kind: KindCompare, Host: h.Target.Hostname, IP: dash(h.Target.IP),
		Vendor: "-", Model: "-", BiosVersion: "-", MgmtModel: "-", MgmtVersion: "-",
	}
	if s := h.Snap; s != nil {
		base.MgmtModel, base.MgmtVersion = dash(s.MgmtModel), dash(s.MgmtVersion)
		if s.ok() {
			base.Vendor, base.Model, base.BiosVersion = dash(s.Vendor), dash(s.Model), dash(s.BiosVersion)
		}
	}
	if h.Status == StatusIsReference && h.Snap != nil && h.Snap.Attrs != nil {
		return referenceLines(base, h.Snap.Attrs, ign)
	}
	if !h.compared() || h.Ref == nil || h.Ref.Snap == nil {
		l := base
		l.Tag, l.Attr, l.Expected, l.Current, l.Note, l.HostError = TagNA, "-", naValue, naValue, h.Status, true
		if h.Status == StatusXMLReadOnly { // XML 대상은 오류가 아니라 비교 제외 (화면은 회색)
			l.Note = xmlCompareNote
		}
		return []ResultLine{l}
	}
	ref, host := h.Ref.Snap.Attrs, h.Snap.Attrs
	names := make([]string, 0, len(ref)+len(host))
	for n := range ref {
		names = append(names, n)
	}
	for n := range host {
		if _, ok := ref[n]; !ok {
			names = append(names, n)
		}
	}
	sort.Strings(names)
	out := make([]ResultLine, 0, len(names))
	for _, n := range names {
		if ign.match(n) {
			continue
		}
		l := base
		l.Attr = n
		rv, rok := ref[n]
		hv, hok := host[n]
		switch {
		case rok && hok:
			l.Expected, l.Current, l.Note = showValue(rv), showValue(hv), StatusSame
			if rv != hv {
				l.Note = attrDiff
			}
		case rok:
			l.Expected, l.Current, l.Note = showValue(rv), naValue, attrOnlyRef
		default:
			l.Expected, l.Current, l.Note = noneValue, showValue(hv), attrOnlyHost
		}
		l.Tag = tagFor(l.Note)
		out = append(out, l)
	}
	return out
}

// referenceLines 는 기준 호스트 자신(REFERENCE)의 줄입니다: 속성마다 기준값 = 현재값 = 자기 값, 태그 OK, 비고 REFERENCE
// (기준 서버가 곧 표준값으로 보이게, 이름순, ign 에 걸린 속성 제외).
func referenceLines(base ResultLine, attrs map[string]string, ign *ignoreSet) []ResultLine {
	names := make([]string, 0, len(attrs))
	for n := range attrs {
		if !ign.match(n) {
			names = append(names, n)
		}
	}
	sort.Strings(names)
	out := make([]ResultLine, 0, len(names))
	for _, n := range names {
		l := base
		l.Tag, l.Attr, l.Note = TagOK, n, StatusIsReference
		l.Expected = showValue(attrs[n])
		l.Current = l.Expected
		out = append(out, l)
	}
	return out
}
