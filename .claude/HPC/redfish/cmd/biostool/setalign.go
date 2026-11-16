package main

// setalign.go — TUI 수정 단계의 "대표서버 기준으로 맞추기" (수정계획서_대표서버_기준맞추기.md 3.B).
// 전체비교(대표서버 자동 지정이든 hostname·diff.txt 든)에서 대표서버와 값이 다른 속성(DIFF)을 (태그, 모델) 묶음별 속성 줄로
// 보이고, 사람이 체크한 속성만 설정 계획(setPlan)에 넣습니다. BMC 에는 접속하지 않고 비교 결과(cmp.Hosts[i].Diffs)와
// 점검 스냅샷만 씁니다. 적용은 setplan.go applySetPlan → set.go runHost 그대로입니다 (출처 SetSrcCompare — set.go 가 레지스트리
// 형식 Enumeration·Integer·Boolean·ReadOnly·허용값을 다시 확인해 맞지 않는 항목은 보내지 않음).
//
// 안전장치:
//  1. 보호 속성(protect.go)은 어느 줄에서도 고를 수 없다 ([-] 줄, 계획의 Protected). 설정 제외 목록(setIgnore)·고유값 모양 값
//     (looksUnique)·위험 속성(attrBlocked)·프로파일이 정하는 속성(충돌이면 둘 다 제외)도 같다 — compareItems 와 같은 순서.
//  2. 한쪽에만 있는 속성은 그 속성만 제외한다 (대표서버에만 있음: Redfish 로 속성을 추가할 수 없음 / 대상에만 있음: 맞출 기준값
//     없음). 같은 호스트의 나머지 DIFF 는 후보로 남는다 (속성 목록 관문 sameAttrNames 를 이 메뉴에서만 속성 단위로 바꿈).
//  3. 호스트 관문(쓰기 벤더·Settings 경로·모델명·남의 Pending(Dell 제외))은 setPlan.addItems 를 그대로 쓴다.
//  4. 대표서버와 BIOS 버전이 다른 호스트는 경고(⚠ BIOS 버전 다름)를 붙여 고를 수 있게 한다 (처음엔 모두 해제).
//  5. 고르지 않은 줄은 계획에서 버린다 (수정제외로 세지 않음). 자동 제외 항목은 Protected·Excluded 에 남아 확인 화면에 나온다.
//  6. bios.conf allcheck_set 과 무관하다: 메뉴 선택·체크·확인 2단계가 명시적 동의다.
// 이 파일은 applySetPlan 을 부르지 않는다 (setplan_test.go TestApplySetPlanOnlyFromTUI).

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// 맞추기 사유·안내 문구.
const (
	alignReasonOnlyRef  = "대표서버에만 있음 — Redfish 로 속성을 추가할 수 없음"
	alignReasonOnlyHost = "대상에만 있음 — 맞출 기준값 없음"
	alignReasonUnique   = "값이 서버 고유값 모양"
	alignReasonModel    = "비교서버와 같은 모델인지 확인할 수 없어 맞추기 제외"
	alignBiosWarn       = "⚠ BIOS 버전 다름"
	alignNote           = "※ 비교서버 값은 적용 때 레지스트리 형식을 다시 확인해 맞지 않는 항목은 보내지 않습니다"
	alignMsgOnlyRep     = "비교할 서버가 없습니다(대표 1대뿐)"
	alignMsgNoDiff      = "대표서버와 다른 항목이 없습니다"
)

// alignCand 는 체크 목록의 한 줄입니다: 한 묶음 안에서 같은 속성을 같은 대표값(To)으로 맞출 호스트들.
type alignCand struct {
	Attr, To string
	Items    []setItem // 고를 수 있는 호스트별 항목 (호스트 관문을 통과한 것, 점검 순서)
	BiosDiff int       // 그 중 대표서버와 BIOS 버전이 다른 호스트 수
	sys      bool      // 시스템 프로파일 성격 속성 (한 서버에 하나만 설정할 수 있음)
}

// alignOff 는 묶음의 자동 제외 줄 ([-] 속성 (제외: 사유)) 입니다.
type alignOff struct{ Attr, Reason string }

// alignBundle 은 (태그, 모델) 묶음입니다.
type alignBundle struct {
	Name  string   // "[gold] R660"
	Reps  []string // 대표서버 hostname (중복 없음)
	Hosts int      // 대표와 비교한 호스트 수 (대표 자신은 아님)
	Cands []*alignCand
	Off   []alignOff

	byKey   map[string]*alignCand
	hasCand map[string]bool // 소문자 속성 이름: 고를 수 있는 줄이 있음
}

// alignCands 는 맞추기 후보 전체입니다 (buildAlignCands 결과).
type alignCands struct {
	Bundles []*alignBundle
	// Msg 는 고를 줄이 하나도 없을 때의 안내입니다 (후보도 제외 줄도 없음). 비어 있지 않으면 Rows 는 nil 입니다.
	Msg string

	u        *unifiedRun
	base     *setPlan // 후보를 만들 때 이미 정해진 제외 (보호·제외 목록·고유값·위험·충돌·한쪽에만 있음·참고)
	gate     *setPlan // 호스트 관문(addItems)을 모든 후보에 한 번 돌려 본 결과 — Items 가 고를 수 있는 항목, Excluded 는 관문 제외
	biosNote map[int]string
	byID     map[string]*alignBundle
	rowCand  []*alignCand // Rows 가 만든 줄마다 후보 (머리·제외 줄은 nil)
}

func (b *alignBundle) addRep(name string) {
	for _, r := range b.Reps {
		if r == name {
			return
		}
	}
	b.Reps = append(b.Reps, name)
}

func (b *alignBundle) addCand(it setItem, biosDiff bool) {
	low := strings.ToLower(it.Attr)
	// BIOS 버전이 다른 호스트는 같은 버전 호스트와 다른 줄이다 (같은 버전 호스트만 골라 적용할 수 있게)
	k := low + "\x00" + it.To
	if biosDiff {
		k += "\x00" + alignBiosWarn
	}
	cd := b.byKey[k]
	if cd == nil {
		cd = &alignCand{Attr: it.Attr, To: it.To, sys: it.std == profileStd}
		b.byKey[k] = cd
		b.Cands = append(b.Cands, cd)
	}
	cd.Items = append(cd.Items, it)
	if biosDiff {
		cd.BiosDiff++
	}
	b.hasCand[low] = true
}

func (c *alignCands) bundleFor(tagMode bool, s *biosSnap) *alignBundle {
	tag := ""
	if tagMode {
		tag = s.Target.Tag
	}
	id := autoBundleID(tagMode, tag, s.Key)
	if b := c.byID[id]; b != nil {
		return b
	}
	b := &alignBundle{Name: autoBundleName(tagMode, tag, s), byKey: map[string]*alignCand{}, hasCand: map[string]bool{}}
	c.byID[id] = b
	c.Bundles = append(c.Bundles, b)
	return b
}

// reGateCount 는 호스트 관문 사유 끝의 " (N건 제외)" 입니다 (호스트마다 달라 제외 줄을 합칠 때 뗀다).
var reGateCount = regexp.MustCompile(`\s*\(\d+건 제외\)$`)

// alignOffReason 은 [-] 줄에 보일 짧은 사유입니다.
func alignOffReason(r string) string {
	r = reGateCount.ReplaceAllString(r, "")
	if strings.HasPrefix(r, "보호 속성") {
		if k := strings.Index(r, " — "); k > 0 {
			r = r[:k]
		}
	}
	return r
}

// buildAlignCands 는 전체비교 결과(cmp)로 맞추기 후보를 만듭니다. BMC 에 접속하지 않습니다 (비교 줄 DIFF 와 스냅샷만 씀).
func buildAlignCands(cfg *Config, u *unifiedRun, cmp *compareResult) *alignCands {
	p := &setPlan{prot: protectFor(cfg), ign: setIgnore(cfg, cmp), nCmp: map[int]bool{}}
	c := &alignCands{u: u, base: p, gate: &setPlan{prot: p.prot, ign: p.ign, nCmp: map[int]bool{}}, biosNote: map[int]string{}, byID: map[string]*alignBundle{}}
	switch {
	case u == nil || u.Check == nil:
		c.Msg = "점검 결과가 없어 설정할 수 없습니다"
		return c
	case u.Check.FromDump != "" || u.fromDump != "":
		c.Msg = "덤프 읽기 모드에서는 설정하지 않습니다"
		return c
	case cmp == nil || len(cmp.Hosts) != len(u.Check.Hosts):
		c.Msg = "전체비교 결과와 점검 대상이 맞지 않습니다 — 다시 비교하십시오"
		return c
	}
	bundleOf := map[int]*alignBundle{}
	for i, hc := range u.Check.Hosts {
		if hc == nil || hc.Status != "" {
			continue // 접속·조회 실패는 N/A
		}
		name := hc.Target.Hostname
		if hc.XML {
			line := name + "  XML API 대상 — 점검만 (읽기 전용, 설정 제외)"
			p.Skipped = append(p.Skipped, line)
			p.xml = append(p.xml, line)
			continue
		}
		ch := cmp.Hosts[i]
		if ch == nil || ch.Target != hc.Target || ch.Snap == nil || !ch.Snap.ok() {
			p.note(name + "  전체비교 결과가 없어 맞추기 제외")
			continue
		}
		if ch.Status != StatusIsReference && !ch.compared() {
			p.note(fmt.Sprintf("%s  전체비교 대상 아님 (%s) — 대표서버 기준으로 맞추지 않음", name, ch.Status))
			continue
		}
		b := c.bundleFor(cmp.TagMode, ch.Snap)
		bundleOf[i] = b
		if ch.Status == StatusIsReference {
			b.addRep(name) // 대표서버 자신은 대상이 아니다
			continue
		}
		b.Hosts++
		if ch.Ref != nil {
			b.addRep(ch.Ref.Target.Hostname)
		}
		c.addHost(i, hc, ch, b)
	}
	for _, b := range c.Bundles {
		sort.SliceStable(b.Cands, func(x, y int) bool {
			ax, ay := strings.ToLower(b.Cands[x].Attr), strings.ToLower(b.Cands[y].Attr)
			if ax != ay {
				return ax < ay
			}
			return b.Cands[x].To < b.Cands[y].To
		})
	}
	// 자동 제외 줄: 묶음마다 (속성, 사유) 한 줄. 같은 속성을 고를 수 있는 줄이 있으면 따로 보이지 않는다 (호스트별 제외는 확인 화면에 나옴).
	seen := map[*alignBundle]map[string]bool{}
	collect := func(list []setItem) {
		for _, it := range list {
			b := bundleOf[it.hostIdx]
			if b == nil || b.hasCand[strings.ToLower(it.Attr)] {
				continue
			}
			why := alignOffReason(it.Reason)
			k := strings.ToLower(it.Attr) + "\x00" + why
			if seen[b] == nil {
				seen[b] = map[string]bool{}
			}
			if seen[b][k] {
				continue
			}
			seen[b][k] = true
			b.Off = append(b.Off, alignOff{Attr: it.Attr, Reason: why})
		}
	}
	collect(p.Protected)
	collect(p.Excluded)
	collect(c.gate.Excluded)
	compared, rows := 0, 0
	for _, b := range c.Bundles {
		sort.SliceStable(b.Off, func(x, y int) bool {
			ax, ay := strings.ToLower(b.Off[x].Attr), strings.ToLower(b.Off[y].Attr)
			if ax != ay {
				return ax < ay
			}
			return b.Off[x].Reason < b.Off[y].Reason
		})
		compared += b.Hosts
		rows += len(b.Cands) + len(b.Off)
	}
	switch {
	case rows > 0:
	case len(c.Bundles) > 0 && compared == 0:
		c.Msg = alignMsgOnlyRep
	case compared == 0:
		c.Msg = "비교할 서버가 없습니다"
	default:
		c.Msg = alignMsgNoDiff
	}
	return c
}

// addHost 는 비교한 호스트 1대의 DIFF 줄을 compareItems 와 같은 순서로 거릅니다 (보호 → 제외 목록 → 고유값 → [호스트 모델 확인] →
// 한쪽에만 있음 → 위험 속성 → 프로파일이 정하는 속성) → 남은 항목은 호스트 관문(addItems)을 돌려 고를 수 있는 항목으로 모읍니다.
func (c *alignCands) addHost(i int, hc *hostCheck, ch *allHost, b *alignBundle) {
	p := c.base
	name := hc.Target.Hostname
	gateWhy := ""
	if ch.Ref == nil || ch.Ref.Snap == nil || ch.Snap.Key != ch.Ref.Snap.Key || normalizeModel(ch.Snap.Model) == "" {
		gateWhy = alignReasonModel
		p.note(name + "  " + gateWhy)
	}
	biosDiff := ch.hasNote(noteBiosVerDiff)
	if biosDiff {
		for _, n := range ch.Notes {
			if n.Code == noteBiosVerDiff {
				c.biosNote[i] = n.Detail
			}
		}
	}
	var items []setItem
	for _, d := range ch.Diffs {
		si := setItem{Host: name, Attr: d.Attr, From: d.Host, To: d.Ref, Source: SetSrcCompare, std: SetSrcCompare, hostIdx: i}
		switch d.Status {
		case attrOnlyRef:
			si.From = absentMark
		case attrOnlyHost:
			si.To = absentMark
		}
		switch {
		case p.protect(si):
			continue
		case p.ign.match(d.Attr):
			p.exclude(si, "제외 목록 (내장 기본·ignore_attrs.txt·exclude_attrs)")
			continue
		case uniquePair(d.Attr, d.Ref, d.Status != attrOnlyHost, d.Host, d.Status != attrOnlyRef): // 비교 줄과 같은 규칙 (양쪽 모두 고유값 모양일 때)
			p.exclude(si, alignReasonUnique)
			continue
		case gateWhy != "":
			p.exclude(si, gateWhy)
			continue
		case d.Status == attrOnlyRef:
			p.exclude(si, alignReasonOnlyRef)
			continue
		case d.Status == attrOnlyHost:
			p.exclude(si, alignReasonOnlyHost)
			continue
		case d.Status != attrDiff:
			continue
		}
		if why := attrBlocked(d.Attr, d.Ref); why != "" {
			p.exclude(si, "설정 불가 속성 ("+why+")")
			continue
		}
		var pi *itemResult
		for k := range hc.Items {
			if it := &hc.Items[k]; strings.EqualFold(it.Attr, d.Attr) && it.Expected != "-" && it.Result != StatusMappingMissing {
				pi = it
				break
			}
		}
		if pi != nil { // 프로파일이 정하는 속성은 비교서버 값으로 따로 맞추지 않는다 (목표값이 다르면 충돌 — 둘 다 설정하지 않음)
			if !matchAny(splitAllowed(pi.Expected), d.Ref) {
				p.Conflicts = append(p.Conflicts, setConflict{Host: name, Attr: d.Attr, ProfileTo: pi.Expected, CompareTo: d.Ref})
				p.exclude(si, "충돌: 프로파일 목표값 "+fullVal(pi.Expected)+" 와 비교서버 값이 다름 — 둘 다 설정하지 않음")
			} else {
				p.exclude(si, "프로파일 항목("+pi.Std+", "+pi.Result+")이 정하는 속성 — 비교서버 값으로 따로 설정하지 않음")
			}
			continue
		}
		if isSysProfileAttr(d.Attr) {
			si.std = profileStd
		}
		if biosDiff {
			si.Note = alignBiosWarn
		}
		items = append(items, si)
	}
	if len(items) == 0 {
		return
	}
	n := len(c.gate.Items)
	c.gate.addItems(c.u, i, hc, items)
	for _, it := range c.gate.Items[n:] {
		b.addCand(it, biosDiff)
	}
}

// label 은 체크 목록 줄 글입니다: "LogicalProc   Disabled(10) Auto(2) -> Enabled   (12대)" (현재값이 모두 같으면 개수 없이).
func (cd *alignCand) label() string {
	cnt := map[string]int{}
	for _, it := range cd.Items {
		cnt[it.From]++
	}
	vals := make([]string, 0, len(cnt))
	for v := range cnt {
		vals = append(vals, v)
	}
	sort.Slice(vals, func(a, b int) bool {
		if cnt[vals[a]] != cnt[vals[b]] {
			return cnt[vals[a]] > cnt[vals[b]]
		}
		return vals[a] < vals[b]
	})
	parts := make([]string, len(vals))
	for k, v := range vals {
		parts[k] = fullVal(v)
		if len(vals) > 1 {
			parts[k] += fmt.Sprintf("(%d)", cnt[v])
		}
	}
	s := fmt.Sprintf("%s   %s -> %s   (%d대)", sanitizeDetail(cd.Attr), strings.Join(parts, " "), fullVal(cd.To), len(cd.Items))
	if cd.BiosDiff > 0 {
		s += fmt.Sprintf("  %s %d대", alignBiosWarn, cd.BiosDiff)
	}
	if cd.sys {
		s += "  [시스템 프로파일: 한 서버에 하나만]"
	}
	return s
}

// Rows 는 ChooseMulti 에 넘길 줄입니다: 묶음 머리 줄 → 속성 줄(고를 수 있음) → 자동 제외 줄 ([-]). 처음에는 모두 해제입니다.
// 줄 번호가 alignPlan 의 picked 입니다. Msg 가 있으면 nil.
func (c *alignCands) Rows() []TUIMultiRow {
	if c == nil {
		return nil
	}
	c.rowCand = nil
	if c.Msg != "" {
		return nil
	}
	var rows []TUIMultiRow
	add := func(r TUIMultiRow, cd *alignCand) {
		rows = append(rows, r)
		c.rowCand = append(c.rowCand, cd)
	}
	for _, b := range c.Bundles {
		head := fmt.Sprintf("%s — 대표서버 %s · 비교 %d대", b.Name, strings.Join(b.Reps, ", "), b.Hosts)
		if b.Hosts == 0 {
			head = fmt.Sprintf("%s — 대표서버 %s · %s", b.Name, strings.Join(b.Reps, ", "), alignMsgOnlyRep)
		}
		add(TUIMultiRow{Group: true, Label: head}, nil)
		for _, cd := range b.Cands {
			add(TUIMultiRow{Label: cd.label()}, cd)
		}
		for _, o := range b.Off {
			add(TUIMultiRow{Label: sanitizeDetail(o.Attr), Disabled: true, Reason: o.Reason}, nil)
		}
	}
	return rows
}

// alignPlan 은 고른 줄(Rows 의 줄 번호)만 설정 항목으로 하는 계획입니다. 고르지 않은 줄은 버립니다 (수정제외로 세지 않음).
// 자동 제외 항목은 Protected·Excluded 로 남아 확인 화면에 나옵니다. 호스트 관문(남의 Pending 등)은 고른 항목만으로 다시 봅니다.
// 제외 줄·머리 줄을 가리키는 번호는 무시합니다 (보호 속성은 어떤 경우에도 항목이 되지 않음).
func alignPlan(c *alignCands, picked []int) *setPlan {
	if c == nil || c.base == nil {
		return &setPlan{}
	}
	p := &setPlan{prot: c.base.prot, ign: c.base.ign, nCmp: map[int]bool{}}
	p.Protected = append(p.Protected, c.base.Protected...)
	p.Excluded = append(append(p.Excluded, c.base.Excluded...), c.gate.Excluded...)
	p.Conflicts = append(p.Conflicts, c.base.Conflicts...)
	p.Skipped = append(append(p.Skipped, c.base.Skipped...), c.gate.Skipped...)
	p.notes = append(append(p.notes, c.base.notes...), c.gate.notes...)
	p.xml = append(p.xml, c.base.xml...)

	byHost := map[int][]setItem{}
	seen := map[int]bool{}
	for _, r := range picked {
		if r < 0 || r >= len(c.rowCand) || seen[r] || c.rowCand[r] == nil {
			continue
		}
		seen[r] = true
		for _, it := range c.rowCand[r].Items {
			it.PendingHost, it.Note = false, ""
			if c.biosNote[it.hostIdx] != "" {
				it.Note = alignBiosWarn
			}
			byHost[it.hostIdx] = append(byHost[it.hostIdx], it)
		}
	}
	hosts := make([]int, 0, len(byHost))
	for i := range byHost {
		hosts = append(hosts, i)
	}
	sort.Ints(hosts)
	for _, i := range hosts {
		items := byHost[i]
		sort.SliceStable(items, func(a, b int) bool { return strings.ToLower(items[a].Attr) < strings.ToLower(items[b].Attr) })
		hc := c.u.Check.Hosts[i]
		if bn := c.biosNote[i]; bn != "" {
			p.note(hc.Target.Hostname + "  " + alignBiosWarn + " (" + bn + ") — 고른 항목은 그래도 맞춥니다")
		}
		if items = p.oneProfile(items); len(items) > 0 {
			p.addItems(c.u, i, hc, items)
		}
	}
	return p
}
