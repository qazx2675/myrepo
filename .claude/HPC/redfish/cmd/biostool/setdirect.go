package main

// setdirect.go — TUI 수정 단계의 "BIOS 값 직접 입력": 붙여넣은 "<Redfish 속성 이름> <설정값>" 줄로 설정 계획을 만듭니다.
// 프로파일과 상관없이, 이번에 점검한 Redfish 호스트 전부가 대상입니다. 계획은 BMC 에 접속하지 않고 점검 때 읽은
// 스냅샷(u.Snaps 의 Bios Attributes, hostCheck.pendDiff)만 씁니다 (점검은 레지스트리를 남기지 않음).
//
// 안전장치 (setplan.go 와 같은 계획·적용 경로):
//  1. 보호 속성(protect.go: 내장 목록 ∪ bios.conf protect_attrs)은 어느 호스트에서도 항목이 되지 않는다 (Protected, 건너뜀).
//     적용 단계 runHost(호스트 통째 PROTECTED_ATTR)·send(본문 재검사)·checkAttributes 가 다시 막는다.
//  2. 설정 제외 목록(내장 기본 ∪ exclude_attrs)·위험 속성(attrBlocked)·이 호스트에 없는 속성·이미 같은 값은 건너뛴다.
//  3. 레지스트리가 필요한 검사(ReadOnly·허용값·Integer 범위·형식)는 적용 때 planHost 가 레지스트리를 다시 읽어
//     typedValue 로 한다 (맞지 않는 항목만 보내지 않음). 레지스트리가 없으면 현재값의 JSON 형식을 따른다.
//  4. 쓰기 대상 벤더·Settings 경로·모델명·남의 Pending(Dell 은 제외)은 addItems 가 프로파일 항목과 똑같이 본다.
//  5. 재부팅·Actions 는 없다 (Pending 까지만).

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"unicode"
)

// directReq 는 직접 입력 1줄입니다 (속성 이름은 입력 그대로, 값은 앞뒤 공백을 뺀 것).
type directReq struct {
	Attr, Value string
}

// parseDirectLines 는 직접 입력 줄들을 읽습니다: 빈 줄·# 줄은 무시, 첫 공백(스페이스·탭) 앞이 속성 이름, 뒤 전부가 값
// (값 안의 공백은 그대로). 값이 없는 줄·제어문자·속성 이름 중복(대소문자 무시)은 errs 에 줄마다 사유를 남긴다.
func parseDirectLines(lines []string) (reqs []directReq, errs []string) {
	seen := map[string]int{}
	for n, raw := range lines {
		s := strings.TrimSpace(raw)
		if s == "" || strings.HasPrefix(s, "#") {
			continue
		}
		bad := func(why string) { errs = append(errs, fmt.Sprintf("%d번째 줄 %s: %s", n+1, bv(s), why)) }
		k := strings.IndexFunc(s, unicode.IsSpace)
		if k < 0 {
			bad("설정값이 없음 (속성 이름 뒤에 공백 또는 탭, 그다음 설정값)")
			continue
		}
		name, val := s[:k], strings.TrimSpace(s[k:])
		if strings.IndexFunc(name+val, func(r rune) bool { return !unicode.IsPrint(r) }) >= 0 { // 값 가운데의 탭도 막는다
			bad("제어문자(탭 포함)가 들어 있음")
			continue
		}
		if m, dup := seen[strings.ToLower(name)]; dup {
			bad(fmt.Sprintf("속성 이름 중복 (%d번째 줄과 같음)", m))
			continue
		}
		seen[strings.ToLower(name)] = n + 1
		reqs = append(reqs, directReq{Attr: name, Value: val})
	}
	return reqs, errs
}

// directSame 은 두 값이 같은 설정인지 봅니다 (대소문자 무시, 10진 정수는 정수로 — "05" 와 5 는 같음).
func directSame(a, b string) bool {
	return strings.EqualFold(strings.TrimSpace(a), strings.TrimSpace(b)) || matchAny([]string{a}, b)
}

// directTypeWhy 는 레지스트리 없이 점검 때 현재값 표기로 알 수 있는 형식 불일치 사유입니다 (불리언·정수, 아니면 "").
// 문자열 속성의 현재값이 숫자처럼 보이면 보수적으로 정수만 받는다 (적용 때 typedValue 가 실제 형식으로 다시 본다).
func directTypeWhy(cur, val string) string {
	if cur == "true" || cur == "false" {
		if !strings.EqualFold(val, "true") && !strings.EqualFold(val, "false") {
			return "형식이 맞지 않는 값 (현재값이 true/false)"
		}
		return ""
	}
	if _, err := strconv.ParseInt(cur, 10, 64); err == nil {
		if _, err := strconv.ParseInt(val, 10, 64); err != nil {
			return "형식이 맞지 않는 값 (현재값이 정수)"
		}
	}
	return ""
}

// directCanon 은 입력값을 이 BMC 의 표기로 바꿉니다: Enumeration 이면 정확히 같은 값, 없으면 대소문자만 다른 레지스트리
// 값, 불리언(레지스트리 Boolean 또는 레지스트리가 없고 현재값이 true/false)이면 소문자. 그 밖에는 그대로입니다.
// 허용값 밖이면 그대로 두어 typedValue 가 거부합니다.
func directCanon(val string, ra *regAttr, cur []byte) string {
	if ra != nil && ra.Type == "Enumeration" {
		if inList(ra.Values, val) {
			return val
		}
		for _, v := range ra.Values {
			if strings.EqualFold(v, val) {
				return v
			}
		}
		return val
	}
	c := strings.TrimSpace(string(cur))
	if (ra != nil && ra.Type == "Boolean") || (ra == nil && (c == "true" || c == "false")) {
		if strings.EqualFold(val, "true") || strings.EqualFold(val, "false") {
			return strings.ToLower(val)
		}
	}
	return val
}

// buildDirectPlan 은 직접 입력(reqs)으로 점검한 호스트 전부의 설정 계획을 만듭니다. BMC 에 접속하지 않습니다.
// 건너뛴 항목은 Protected·Excluded(Reason), 건너뛴 호스트는 확인 화면 참고(notes)에 남습니다.
func buildDirectPlan(cfg *Config, u *unifiedRun, reqs []directReq) *setPlan {
	p := &setPlan{prot: protectFor(cfg), ign: setIgnore(cfg, nil), nCmp: map[int]bool{}}
	switch {
	case u == nil || u.Check == nil:
		p.note("점검 결과가 없어 설정할 수 없습니다")
		return p
	case u.Check.FromDump != "" || u.fromDump != "":
		p.note("덤프 읽기 모드에서는 설정하지 않습니다")
		return p
	}
	for i, hc := range u.Check.Hosts {
		p.addDirectHost(u, i, hc, reqs)
	}
	return p
}

// addDirectHost 는 호스트 1대의 직접 입력 항목을 판정합니다 (보호 속성 → 이 호스트에 있는 속성 → 제외 목록·위험 속성 →
// 형식 → 이미 같은 값 → addItems). 남는 항목이 없으면 그 호스트는 건너뜁니다.
func (p *setPlan) addDirectHost(u *unifiedRun, i int, hc *hostCheck, reqs []directReq) {
	if hc == nil {
		return
	}
	name := hc.Target.Hostname
	var snap *biosSnap
	if i < len(u.Snaps) {
		snap = u.Snaps[i]
	}
	switch {
	case hc.XML:
		p.note(name + "  건너뜀: Cisco(XML) 대상 — 점검만 (읽기 전용)")
		return
	case hc.Status != "":
		p.note(name + "  건너뜀: 점검 오류 (" + hc.Status + ")")
		return
	case snap == nil || len(snap.Attrs) == 0:
		p.note(name + "  건너뜀: BIOS 속성을 읽지 못함")
		return
	}
	var items []setItem
	for _, r := range reqs {
		si := setItem{Host: name, Attr: r.Attr, From: "-", To: r.Value, Source: SetSrcDirect, std: SetSrcDirect, hostIdx: i}
		attr, ok := lookupAttr(snap.Attrs, r.Attr)
		if ok {
			si.Attr, si.From = attr, snap.Attrs[attr]
		}
		if p.protect(si) { // 입력 이름·BMC 이름은 대소문자만 달라 보호 패턴 판정이 같다
			continue
		}
		if !ok {
			p.exclude(si, "미지원 속성 (이 호스트에 없음)")
			continue
		}
		if p.ign.match(attr) {
			p.exclude(si, "제외 목록 (내장 기본·exclude_attrs)")
			continue
		}
		if why := attrBlocked(attr, r.Value); why != "" {
			p.exclude(si, "설정 불가 속성 ("+why+")")
			continue
		}
		if why := directTypeWhy(si.From, r.Value); why != "" {
			p.exclude(si, why)
			continue
		}
		if si.From == "true" || si.From == "false" {
			si.To = strings.ToLower(si.To)
		}
		if directSame(si.From, si.To) {
			p.exclude(si, "이미 같은 값")
			continue
		}
		if isSysProfileAttr(attr) {
			si.std = profileStd // 시스템 프로파일이 바뀌면 다른 항목은 미루는 규칙(buildPlan)을 그대로 탄다
		}
		items = append(items, si)
	}
	if len(items) > 0 {
		p.addItems(u, i, hc, items)
	}
}

// DirectLines 는 직접 입력 미리보기입니다: 호스트(점검 순서)별 "속성: 현재값 -> 변경값 (수정대상)" / "(건너뜀: 사유)",
// 이어서 남의 Pending 목록과 건너뛴 호스트 (값은 제어문자만 정리하고 자르지 않음).
func (p *setPlan) DirectLines() []string {
	type row struct {
		idx  int
		line string
	}
	var rows []row
	add := func(it setItem, tag string) {
		rows = append(rows, row{it.hostIdx, fmt.Sprintf("%s  %s: %s -> %s (%s)", it.Host, sanitizeDetail(it.Attr), fullVal(it.From), fullVal(it.To), tag)})
	}
	for _, it := range p.Items {
		tag := "수정대상"
		if it.PendingHost {
			tag += ", 남의 Pending 있는 장비"
		}
		if it.Note != "" {
			tag += " — " + it.Note
		}
		add(it, tag)
	}
	for _, it := range p.Protected {
		add(it, "건너뜀: "+it.Reason)
	}
	for _, it := range p.Excluded {
		add(it, "건너뜀: "+it.Reason)
	}
	sort.SliceStable(rows, func(a, b int) bool { return rows[a].idx < rows[b].idx })
	out := []string{fmt.Sprintf("수정 전 확인 (값 직접 입력): 수정대상 %s건 · 건너뜀 %s건 — 현재값 -> 변경값 (BMC Pending 까지만 설정, 재부팅 없음)",
		commaInt(len(p.Items)), commaInt(len(p.Protected)+len(p.Excluded)))}
	for _, r := range rows {
		out = append(out, r.line)
	}
	out = append(out, "※ ReadOnly·허용값·형식은 적용 때 레지스트리로 다시 확인해 맞지 않는 항목은 보내지 않습니다 (보호 속성은 어떤 경우에도 보내지 않음)")
	if len(p.Pending) > 0 {
		out = append(out, "", "남의 Pending 이 있는 장비 (포함 여부를 따로 묻습니다):")
		for _, l := range p.PendingLines() {
			out = append(out, "  "+l)
		}
	}
	if len(p.notes) > 0 {
		out = append(out, "", "참고:")
		for _, s := range p.notes {
			out = append(out, "  "+s)
		}
	}
	return out
}
