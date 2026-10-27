package main

// setplan.go — 프로파일 difference + 전체비교 difference 를 합친 설정 계획과 적용 (계획서_v0.2_TUI.md 흐름 6, 단계 7).
// 단계 6(TUI 흐름)과 단계 7(설정 엔진)이 함께 쓰는 API 입니다.
//
// 안전장치 (set.go 의 겹겹 방어를 그대로 타고, 계획 단계에서 한 번 더 거른다):
//  1. 계획(buildSetPlan)은 BMC 에 접속하지 않는다. 프로파일은 verified=Y 인 FAIL(·설정 가능한 PENDING_NO_JOB)만,
//     전체비교는 allcheck_set=on 이고 같은 모델 기준과 비교했고 속성 이름 목록이 같고(sameAttrNames) BIOS 버전도 같은
//     호스트의 값 차이(DIFF)만. 제외 목록(내장 기본 ∪ ignore_attrs.txt ∪ exclude_attrs)·위험 속성(checkAttributes)은
//     미리 빼서 (수정제외) 로 보인다.
//  2. 보호 속성(protect.go: 서버 고유값·부팅·보안·RAID·PXE·NIC, 내장 목록 ∪ bios.conf protect_attrs)은 어느 출처든
//     설정 항목이 되지 않고 Protected 로 (수정제외) 표시한다 (적용 단계 set.go 와 PATCH 본문 검사가 다시 막음).
//  3. 프로파일이 정하는 속성은 비교서버 값으로 따로 설정하지 않는다. 목표값이 다르면 둘 다 설정하지 않고 [충돌].
//     시스템 프로파일 성격 속성이 한 호스트에 여러 개면 프로파일 행 항목만 남기고 나머지는 (수정제외).
//  4. 남의 Pending 이 있는 호스트는 목록(호스트·속성·값)을 보여 주고, 포함하기로 했을 때만 그 목록과 정확히 같은
//     Pending 을 허용한다 (적용 때 새로 생긴 Pending 이 있으면 set.go 가 PENDING_EXISTS 로 쓰지 않음).
//     Dell 은 설정 Job 이 남의 Pending 까지 적용하므로 남의 Pending 이 있으면 그 호스트는 아예 제외한다.
//  5. 적용(applySetPlan)은 set.go 의 runHost 그대로: 재조회·레지스트리 검사·호스트당 PATCH 1회(+Dell Job 최대 1회)·
//     재시도 없음·Pending 재조회 확인·같은 BMC 중복 방지·AUTH_FAIL 차단기. 재부팅·리셋은 하지 않는다.
//     비교서버 값은 레지스트리 형식이 Enumeration·Integer·Boolean 일 때만 보낸다 (문자열·형식 불명은 수정제외).
//  6. 비대화형(bios -auto / -auto-check)은 설정하지 않는다: applySetPlan 은 TUI 흐름(tui_flow.go)에서만 부르며
//     setplan_test.go 가 그 밖의 호출을 막는다.
//  7. BIOS 값 직접 입력(setdirect.go)도 같은 계획(addItems)·적용(applySetPlan → runHost) 경로를 탄다:
//     보호 속성·제외 목록·위험 속성·남의 Pending·모델 재확인·레지스트리 검사·허용목록이 그대로 걸린다.

import (
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// 설정 항목 출처
const (
	SetSrcProfile = "profile" // 프로파일 FAIL (verified=Y)
	SetSrcCompare = "compare" // 전체비교 difference (allcheck_set=on 일 때만)
	SetSrcDirect  = "direct"  // BIOS 값 직접 입력 (TUI 수정 단계, 프로파일과 상관없음 — setdirect.go)
)

// setItem 은 호스트 1대의 속성 1개를 To 값으로 바꾸는 계획입니다 (Protected·Excluded 에서는 바꾸지 않는 차이 항목).
type setItem struct {
	Host   string
	Attr   string
	From   string // 현재값
	To     string // 목표값 (프로파일은 value 원문 A|B — 보낼 값은 적용 때 BMC 가 받는 표기로 고름)
	Source string // SetSrcProfile / SetSrcCompare / SetSrcDirect

	PendingHost bool   // 이 호스트에 남의 Pending 이 있음 → includePending=false 면 이 호스트는 설정하지 않음
	Note        string // 미리보기 참고 (PENDING_NO_JOB, Pending 이 이미 목표값 등)
	Reason      string // 수정제외 사유 (Protected·Excluded 에서만)

	std     string // 프로파일 std_name (전체비교 항목은 SetSrcCompare, 시스템 프로파일로 보이는 속성은 profileStd)
	hostIdx int    // u.Check.Hosts 의 위치 (applySetPlan 이 Host 이름과 함께 다시 확인)
}

// allowed 는 목표값으로 인정하는 값들입니다 (프로파일 A|B, 전체비교는 기준값 하나).
func (it setItem) allowed() []string {
	if it.Source == SetSrcProfile {
		return splitAllowed(it.To)
	}
	return []string{it.To}
}

// setConflict 는 프로파일과 비교서버의 목표값이 달라 설정하지 않는 속성입니다 (미리보기에 [충돌]).
type setConflict struct {
	Host      string
	Attr      string
	ProfileTo string
	CompareTo string
}

// pendingInfo 는 남이 걸어 둔 Pending 이 있는 호스트·속성입니다 (진행 전에 포함 여부를 묻는다).
type pendingInfo struct {
	Host    string
	Attr    string
	Pending string
}

// setPlan 은 "적용할까요?" 직전에 보여 줄 설정 계획입니다.
type setPlan struct {
	Items     []setItem
	Protected []setItem // 보호 속성 (protect.go) — 어떤 경우에도 설정하지 않음 (수정제외, Reason)
	Excluded  []setItem // 그 밖에 설정하지 않는 차이 항목 (수정제외, Reason): UNVERIFIED·제외 목록·위험 속성·충돌·BIOS_VER_DIFF 등
	Conflicts []setConflict
	Pending   []pendingInfo // 남의 Pending 이 있는 호스트 (포함 여부는 적용 때 결정)
	Skipped   []string      // 제외 사유 줄 (verified≠Y, 제외 목록, 위험 속성, 모델·속성목록 불일치, allcheck_set=off 등)

	notes []string     // 항목 줄이 없는 계획·호스트 단위 안내 (확인 화면의 "참고")
	xml   []string     // XML 대상이라 뺀 호스트 줄 (확인 화면의 "제외")
	prot  *protectSet  // 보호 패턴 (내장 ∪ protect_attrs)
	ign   *ignoreSet   // 설정 제외 목록 (내장 기본 ∪ ignore_attrs.txt ∪ exclude_attrs)
	nCmp  map[int]bool // 전체비교 항목이 있는 호스트 (확인 화면 안내용)
}

// splitAllowed 는 프로파일 value 원문(A|B)을 허용값 목록으로 나눕니다 (parseProfile 과 같은 규칙).
func splitAllowed(v string) []string {
	var out []string
	for _, s := range strings.Split(v, "|") {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	return out
}

// attrBlocked 는 PATCH 본문 검사(checkAttributes: 위험 속성명·"@" 키·빈 값)에 걸리면 사유를 돌려줍니다.
// 계획 단계에서 미리 빼야 한 속성 때문에 그 호스트의 PATCH 전체가 거부되지 않습니다.
func attrBlocked(name, val string) string {
	b, _ := json.Marshal(val)
	if err := checkAttributes(map[string]json.RawMessage{name: b}); err != nil {
		return err.Error()
	}
	return ""
}

// isSysProfileAttr 는 속성 이름이 시스템 프로파일 후보 키워드(inspect.go stdKeywords)에 걸리는지 봅니다.
// 전체비교 항목이 시스템 프로파일이면 buildPlan 의 프로파일 종속 규칙(다른 항목은 미룸)을 그대로 타게 합니다.
func isSysProfileAttr(name string) bool {
	n := normAttrName(name)
	for _, k := range stdKeywords {
		if k.Std != profileStd {
			continue
		}
		for _, key := range k.Keys {
			if strings.Contains(n, key) {
				return true
			}
		}
	}
	return false
}

// planVal 은 결과 줄에 넣는 BMC 값 표기입니다 (제어문자 제거·60자, 빈 문자열은 "").
func planVal(s string) string {
	if s == "" {
		return `""`
	}
	return bv(s)
}

// fullVal 은 미리보기·확인 화면의 값 표기입니다 (제어문자만 정리하고 자르지 않음, 빈 문자열은 "").
func fullVal(s string) string {
	if s == "" {
		return `""`
	}
	return sanitizeDetail(s)
}

// setIgnore 는 설정에 쓰는 제외 목록입니다: 내장 기본 목록 ∪ 전체비교에 쓴 목록(ignore_attrs.txt) ∪ bios.conf exclude_attrs.
// ignore_attrs.txt 가 있으면 전체비교는 그 파일만 쓰지만(내장 목록 대체), 설정할 때는 내장 기본 목록을 항상 더한다.
func setIgnore(cfg *Config, cmp *compareResult) *ignoreSet {
	pats := append([]string(nil), defaultIgnore...)
	if cmp != nil && cmp.Ignore != nil {
		pats = append(pats, cmp.Ignore.Pats...)
	}
	if cfg != nil {
		pats = append(pats, cfg.ExcludeAttrs...)
	}
	return newIgnoreSet(pats, "설정 제외 목록 (내장 기본 ∪ ignore_attrs.txt ∪ exclude_attrs)")
}

// note 는 항목 줄이 없는 안내를 제외 사유(Skipped)와 확인 화면 참고(notes)에 함께 남깁니다.
func (p *setPlan) note(line string) {
	p.Skipped = append(p.Skipped, line)
	p.notes = append(p.notes, line)
}

// buildSetPlan 은 점검 결과(u)와 전체비교 결과(cmp, 없으면 nil)로 설정 계획을 만듭니다. BMC 에 접속하지 않습니다.
func buildSetPlan(cfg *Config, u *unifiedRun, cmp *compareResult) *setPlan {
	p := &setPlan{prot: protectFor(cfg), ign: setIgnore(cfg, cmp), nCmp: map[int]bool{}}
	switch {
	case u == nil || u.Check == nil:
		p.note("점검 결과가 없어 설정할 수 없습니다")
		return p
	case u.Check.FromDump != "" || u.fromDump != "":
		p.note("덤프 읽기 모드에서는 설정하지 않습니다")
		return p
	}
	useCmp := cmp != nil && cfg != nil && cfg.AllcheckSet
	if cmp != nil && !useCmp {
		p.note("전체비교 difference 는 설정하지 않음 (bios.conf allcheck_set=off)")
	}
	aligned := cmp != nil && len(cmp.Hosts) == len(u.Check.Hosts)
	if useCmp && !aligned {
		p.note("전체비교 결과와 점검 대상이 맞지 않아 전체비교 difference 는 설정하지 않음")
		useCmp = false
	}
	for i, hc := range u.Check.Hosts {
		var ch *allHost
		if aligned && hc != nil && cmp.Hosts[i] != nil && cmp.Hosts[i].Target == hc.Target {
			ch = cmp.Hosts[i]
		}
		p.addHost(u, i, hc, ch, useCmp)
	}
	return p
}

// protect 는 보호 속성이면 Protected 에 넣고 true 입니다.
func (p *setPlan) protect(si setItem) bool {
	why := p.prot.why(si.Attr)
	if why == "" {
		return false
	}
	for _, x := range p.Protected {
		if x.hostIdx == si.hostIdx && strings.EqualFold(x.Attr, si.Attr) {
			return true // 프로파일·전체비교에 같은 속성이 있으면 한 줄만
		}
	}
	si.Reason = why
	p.Protected = append(p.Protected, si)
	return true
}

// exclude 는 설정하지 않는 차이 항목을 사유와 함께 남깁니다.
func (p *setPlan) exclude(si setItem, why string) {
	si.Reason = why
	p.Excluded = append(p.Excluded, si)
}

// addHost 는 호스트 1대의 항목을 계획에 넣습니다 (보호 속성 → 프로파일 → 전체비교 → 충돌 → 시스템 프로파일 1개 →
// 쓰기 가능 여부 → 남의 Pending).
func (p *setPlan) addHost(u *unifiedRun, i int, hc *hostCheck, ch *allHost, useCmp bool) {
	if hc == nil || hc.Status != "" {
		return // 접속·조회 실패는 N/A (설정할 difference 가 없음)
	}
	if hc.XML {
		line := hc.Target.Hostname + "  XML API 대상 — 점검만 (읽기 전용, 설정 제외)"
		p.Skipped = append(p.Skipped, line)
		p.xml = append(p.xml, line)
		return
	}
	name := hc.Target.Hostname
	skip := func(format string, a ...interface{}) string {
		s := fmt.Sprintf(format, a...)
		p.Skipped = append(p.Skipped, name+"  "+s)
		return s
	}
	hostNote := func(format string, a ...interface{}) string {
		s := fmt.Sprintf(format, a...)
		p.note(name + "  " + s)
		return s
	}

	// 1) 프로파일: verified=Y 인 FAIL·설정 가능한 PENDING_NO_JOB, 남의 Pending 때문에 막힌 PENDING_EXISTS (포함 여부는 따로 묻는다)
	var items []setItem
	for _, it := range hc.Items {
		switch it.Result {
		case StatusFail, StatusPendingNoJob, StatusPendingExists, StatusUnverified:
		default:
			continue // OK·PENDING_OK·MAPPING_MISSING(N/A)
		}
		si := setItem{Host: name, Attr: it.Attr, From: it.Current, To: it.Expected, Source: SetSrcProfile, std: it.Std, hostIdx: i}
		if p.protect(si) {
			skip("%s: 보호 속성 — 설정하지 않음", sanitizeDetail(it.Attr))
			continue
		}
		switch {
		case it.Verified && (hc.settable(it) || it.Result == StatusPendingExists):
		case it.Result == StatusUnverified:
			skip("%s: 프로파일 행이 verified=N (UNVERIFIED) — 설정하지 않음", sanitizeDetail(it.Attr))
			p.exclude(si, "프로파일 행 verified=N (UNVERIFIED)")
			continue
		case it.Result == StatusPendingNoJob:
			skip("%s: PENDING_NO_JOB 이나 verified=Y 가 아니거나 남의 Pending 이 있어 설정하지 않음", sanitizeDetail(it.Attr))
			p.exclude(si, "PENDING_NO_JOB 이나 verified=Y 가 아니거나 남의 Pending 이 있음")
			continue
		default:
			p.exclude(si, it.Result+" 이고 프로파일 행이 verified=N")
			continue
		}
		if why := attrBlocked(it.Attr, splitAllowed(it.Expected)[0]); why != "" {
			skip("%s: 설정 불가 속성 (%s) — 설정하지 않음", sanitizeDetail(it.Attr), why)
			p.exclude(si, "설정 불가 속성 ("+why+")")
			continue
		}
		if it.Result == StatusPendingNoJob {
			si.Note = "PENDING_NO_JOB: Pending=기대값이나 BIOS 설정 Job 없음 → 같은 값을 다시 보내고 Job 생성"
		}
		items = append(items, si)
	}

	// 2) 전체비교: 같은 모델·같은 BIOS 버전 기준과 비교했고 속성 이름 목록이 같은 호스트의 값 차이(DIFF)만
	if ch != nil {
		why := ""
		switch {
		case ch.Status == StatusIsReference || ch.Status == StatusSame:
		case !useCmp:
			why = "bios.conf allcheck_set=off — 전체비교 차이는 설정하지 않음"
		case !ch.compared():
			hostNote("전체비교 대상 아님 (%s) — 비교서버 값으로 설정하지 않음", ch.Status)
		case !ch.sameAttrNames():
			why = hostNote("속성 목록이 비교서버와 달라 설정 제외")
		case ch.Ref == nil || ch.Ref.Snap == nil || ch.Snap == nil || ch.Ref.Snap.Key != ch.Snap.Key || normalizeModel(ch.Snap.Model) == "":
			why = hostNote("비교서버와 같은 모델인지 확인할 수 없어 설정 제외")
		case ch.hasNote(noteBiosVerDiff):
			why = hostNote("BIOS 버전이 비교서버와 달라(%s) 비교서버 값으로 설정 제외", noteBiosVerDiff)
		default:
			items = p.compareItems(name, i, hc, ch, items)
		}
		if why != "" {
			planned := map[string]bool{}
			for _, it := range items {
				planned[strings.ToLower(it.Attr)] = true
			}
			for _, d := range ch.Diffs {
				if d.Status != attrDiff || planned[strings.ToLower(d.Attr)] {
					continue // 프로파일 항목이 이미 정하는 속성은 한 줄만 (수정대상)
				}
				si := setItem{Host: name, Attr: d.Attr, From: d.Host, To: d.Ref, Source: SetSrcCompare, std: SetSrcCompare, hostIdx: i}
				switch {
				case p.protect(si):
				case p.ign.match(d.Attr):
					p.exclude(si, "제외 목록 (내장 기본·ignore_attrs.txt·exclude_attrs)")
				default:
					p.exclude(si, why)
				}
			}
		}
	}

	// 3) 시스템 프로파일 성격 속성은 한 호스트에 하나만 (종속 규칙이 프로파일 1개를 전제로 함)
	items = p.oneProfile(items)
	if len(items) == 0 {
		return
	}
	p.addItems(u, i, hc, items)
}

// addItems 는 호스트 1대의 설정 항목(비어 있지 않음)을 쓰기 가능 여부·남의 Pending 을 본 뒤 계획에 넣습니다
// (addHost 의 4·5단계, 값 직접 입력 addDirectHost 도 같이 씀).
func (p *setPlan) addItems(u *unifiedRun, i int, hc *hostCheck, items []setItem) {
	name := hc.Target.Hostname
	hostNote := func(format string, a ...interface{}) string {
		s := fmt.Sprintf(format, a...)
		p.note(name + "  " + s)
		return s
	}
	excludeAll := func(why string) {
		for _, it := range items {
			p.exclude(it, why)
		}
	}

	// 4) 호스트 단위 쓰기 가능 여부 (적용 때 runHost 가 다시 막지만 미리보기에 먼저 보인다)
	switch {
	case hc.Info == nil || !setVendors[hc.Info.Vendor]:
		excludeAll(hostNote("쓰기 대상 벤더가 아님 — Dell·HPE·Lenovo 만 설정 (%d건 제외)", len(items)))
		return
	case hc.Info.SettingsPath == "":
		excludeAll(hostNote("Bios Settings 경로가 없어 설정할 수 없음 (%d건 제외)", len(items)))
		return
	case normalizeModel(hc.Info.Model) == "":
		excludeAll(hostNote("BMC 가 보고한 모델명이 비어 있어 설정하지 않음 (%d건 제외)", len(items)))
		return
	}

	// 5) 남의 Pending: 우리 표준값(이 호스트에 매핑되는 프로파일 행의 허용값)도 이번 목표값도 아닌 Pending (set.go planHost 와 같은 정의)
	ours := map[string][]string{}
	if prof := u.Check.prof; prof != nil && i < len(u.Snaps) && u.Snaps[i] != nil {
		for _, std := range prof.Items {
			for _, r := range prof.rowsFor(hc.Info.Vendor, hc.Info.Model, std) {
				if n, ok := lookupAttr(u.Snaps[i].Attrs, r.Attr); ok {
					ours[n] = r.Allowed
					break
				}
			}
		}
	}
	for _, it := range items {
		ours[it.Attr] = it.allowed()
	}
	var foreign []pendingInfo
	for k, v := range hc.pendDiff {
		if al, ok := ours[k]; ok && matchAny(al, v) {
			continue
		}
		foreign = append(foreign, pendingInfo{Host: name, Attr: k, Pending: v})
	}
	sort.Slice(foreign, func(a, b int) bool { return foreign[a].Attr < foreign[b].Attr })
	if len(foreign) > 0 && hc.Info.Vendor == "Dell" {
		// Dell 은 BIOS 설정 Job 이 Settings 의 Pending 전체(남의 것 포함)를 적용하므로, 포함 여부를 묻지 않고 제외한다.
		var fl []string
		for _, x := range foreign {
			fl = append(fl, x.Attr+"="+x.Pending)
		}
		hostNote("남의 Pending %d건 (%s) — Dell 은 설정 Job 이 남의 Pending 까지 적용하므로 제외 (%d건)", len(foreign), bv(strings.Join(fl, ", ")), len(items))
		excludeAll("Dell 은 설정 Job 이 남의 Pending 까지 적용하므로 제외")
		return
	}
	p.Pending = append(p.Pending, foreign...)
	for j := range items {
		items[j].PendingHost = len(foreign) > 0
		if v, ok := hc.pendDiff[items[j].Attr]; ok && items[j].Source != SetSrcProfile && matchAny(items[j].allowed(), v) {
			items[j].Note = "Pending 이 이미 목표값 (재부팅 대기)"
		}
		if items[j].Source == SetSrcCompare {
			p.nCmp[i] = true
		}
	}
	p.Items = append(p.Items, items...)
}

// oneProfile 은 시스템 프로파일 성격 항목(std=profileStd)이 둘 이상이면 프로파일 행 항목만 남기고 나머지(비교서버 항목)는
// 수정제외로 뺍니다. 프로파일 행 항목이 없으면 순서를 정할 수 없어 모두 뺍니다.
// (예: 프로파일 WorkloadProfile + 비교서버 PowerProfile=Custom 을 한 PATCH 에 보내면 종속 규칙이 깨진다)
func (p *setPlan) oneProfile(items []setItem) []setItem {
	var profs []string
	keep := -1
	for k, it := range items {
		if it.std == profileStd {
			profs = append(profs, it.Attr)
			if it.Source == SetSrcProfile && keep < 0 {
				keep = k
			}
		}
	}
	if len(profs) < 2 {
		return items
	}
	var out []setItem
	for k, it := range items {
		switch {
		case it.std != profileStd || k == keep:
			out = append(out, it)
		case keep >= 0:
			p.exclude(it, "시스템 프로파일 성격 속성이 여러 개 ("+strings.Join(profs, ", ")+") — 프로파일 항목("+items[keep].Attr+")만 설정, 재부팅 후 재점검")
		default:
			p.exclude(it, "시스템 프로파일 성격 속성이 여러 개 ("+strings.Join(profs, ", ")+")인데 프로파일 행 항목이 없어 순서를 정할 수 없음")
		}
	}
	return out
}

// compareItems 는 전체비교 DIFF 줄을 항목으로 바꿔 items 에 더합니다. 프로파일이 정하는 속성은 비교서버 값으로 따로
// 설정하지 않고, 목표값이 다르면 프로파일 항목까지 빼고 [충돌] 로 남깁니다.
func (p *setPlan) compareItems(name string, i int, hc *hostCheck, ch *allHost, items []setItem) []setItem {
	skip := func(format string, a ...interface{}) {
		p.Skipped = append(p.Skipped, name+"  "+fmt.Sprintf(format, a...))
	}
	for _, d := range ch.Diffs {
		if d.Status != attrDiff {
			continue // ONLY_REF·ONLY_HOST(N/A·없음)는 설정하지 않음 (sameAttrNames 면 없음)
		}
		si := setItem{Host: name, Attr: d.Attr, From: d.Host, To: d.Ref, Source: SetSrcCompare, std: SetSrcCompare, hostIdx: i}
		if p.protect(si) {
			skip("%s: 보호 속성 — 비교서버 값으로 설정하지 않음", sanitizeDetail(d.Attr))
			continue
		}
		if p.ign.match(d.Attr) {
			p.exclude(si, "제외 목록 (내장 기본·ignore_attrs.txt·exclude_attrs)")
			continue
		}
		if why := attrBlocked(d.Attr, d.Ref); why != "" {
			skip("%s: 설정 불가 속성 (%s) — 비교서버 값으로 설정하지 않음", sanitizeDetail(d.Attr), why)
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
		if pi != nil {
			planned := -1
			for k, it := range items {
				if it.Source == SetSrcProfile && strings.EqualFold(it.Attr, d.Attr) {
					planned = k
				}
			}
			switch {
			case !matchAny(splitAllowed(pi.Expected), d.Ref):
				p.Conflicts = append(p.Conflicts, setConflict{Host: name, Attr: d.Attr, ProfileTo: pi.Expected, CompareTo: d.Ref})
				p.exclude(si, "충돌: 프로파일 목표값 "+fullVal(pi.Expected)+" 와 비교서버 값이 다름 — 둘 다 설정하지 않음")
				if planned >= 0 {
					items = append(items[:planned], items[planned+1:]...)
				}
			case planned < 0:
				skip("%s: 프로파일 항목(%s, %s)이 정하는 속성 — 비교서버 값으로 따로 설정하지 않음", sanitizeDetail(d.Attr), pi.Std, pi.Result)
				p.exclude(si, "프로파일 항목("+pi.Std+", "+pi.Result+")이 정하는 속성 — 비교서버 값으로 따로 설정하지 않음")
			}
			continue // 목표값이 같고 프로파일 항목이 있으면 하나만 남김 (출처 프로파일)
		}
		if isSysProfileAttr(d.Attr) {
			si.std = profileStd
		}
		items = append(items, si)
	}
	return items
}

// Empty 는 설정할 항목이 하나도 없는지입니다.
func (p *setPlan) Empty() bool { return len(p.Items) == 0 }

// Preview 는 계획 요약 줄들입니다 (설정 항목, 수정제외, [충돌], Pending, 제외 사유). 값은 자르지 않습니다.
// 항목·수정제외·충돌·Pending·제외 사유가 모두 없으면 nil 입니다.
func (p *setPlan) Preview() []string {
	if p.Empty() && len(p.Protected) == 0 && len(p.Excluded) == 0 && len(p.Conflicts) == 0 && len(p.Pending) == 0 && len(p.Skipped) == 0 {
		return nil
	}
	var out []string
	hosts, pendHosts := map[string]bool{}, map[string]bool{}
	for _, it := range p.Items {
		hosts[it.Host] = true
		if it.PendingHost {
			pendHosts[it.Host] = true
		}
	}
	if p.Empty() {
		out = append(out, "설정할 항목이 없습니다.")
	} else {
		head := fmt.Sprintf("설정할 항목: 호스트 %s대, %s건 (BMC Pending 까지만 설정, 재부팅 없음)", commaInt(len(hosts)), commaInt(len(p.Items)))
		if len(pendHosts) > 0 {
			head += fmt.Sprintf(" — 남의 Pending 이 있는 호스트 %s대 포함 (포함 여부를 따로 묻습니다)", commaInt(len(pendHosts)))
		}
		out = append(out, head)
		for _, it := range p.Items {
			src := "프로파일"
			if it.Source == SetSrcCompare {
				src = "비교서버"
			}
			line := fmt.Sprintf("  %s  %s  %s → %s  (%s)", it.Host, sanitizeDetail(it.Attr), fullVal(it.From), fullVal(it.To), src)
			if it.PendingHost {
				line += "  [남의 Pending]"
			}
			if it.Note != "" {
				line += "  ※ " + it.Note
			}
			out = append(out, line)
		}
	}
	for _, it := range p.Protected {
		out = append(out, fmt.Sprintf("[보호] %s %s %s → %s (수정제외: %s)", it.Host, sanitizeDetail(it.Attr), fullVal(it.From), fullVal(it.To), it.Reason))
	}
	for _, c := range p.Conflicts {
		out = append(out, fmt.Sprintf("[충돌] %s %s 프로파일=%s 비교서버=%s (둘 다 설정하지 않음)", c.Host, sanitizeDetail(c.Attr), fullVal(c.ProfileTo), fullVal(c.CompareTo)))
	}
	if len(p.Pending) > 0 {
		out = append(out, "남의 Pending 이 있는 장비 (포함하면 이 Pending 은 그대로 두고 위 항목만 더함):")
		for _, l := range p.PendingLines() {
			out = append(out, "  "+l)
		}
	}
	if len(p.Skipped) > 0 {
		out = append(out, "제외:")
		for _, s := range p.Skipped {
			out = append(out, "  "+s)
		}
	}
	return out
}

// confirmRows 는 차이 항목 줄을 호스트(점검 순서)별로 묶어 돌려줍니다: 수정대상 먼저, 그다음 수정제외.
func (p *setPlan) confirmRows(withItems bool) []string {
	type row struct {
		idx  int
		line string
	}
	var rows []row
	add := func(it setItem, tag string) {
		rows = append(rows, row{it.hostIdx, fmt.Sprintf("%s  %s  %s -> %s (%s)", it.Host, sanitizeDetail(it.Attr), fullVal(it.From), fullVal(it.To), tag)})
	}
	if withItems {
		for _, it := range p.Items {
			add(it, "수정대상")
		}
	}
	for _, it := range p.Protected {
		add(it, "수정제외: "+it.Reason)
	}
	for _, it := range p.Excluded {
		add(it, "수정제외: "+it.Reason)
	}
	sort.SliceStable(rows, func(a, b int) bool { return rows[a].idx < rows[b].idx })
	out := make([]string, len(rows))
	for k, r := range rows {
		out[k] = r.line
	}
	return out
}

// ConfirmLines 는 적용 전 확인 화면 1 입니다: 차이 항목 전부를 호스트별로 "현재값 -> 변경값 (수정대상)" 과
// "현재값 -> 변경값 (수정제외: 사유)" 로 (값은 자르지 않음), 이어서 남의 Pending 목록, 참고(항목 줄이 없는 제외 사유),
// 제외(XML 대상 호스트). 보여 줄 것이 하나도 없으면 nil 입니다.
func (p *setPlan) ConfirmLines() []string {
	rows := p.confirmRows(true)
	if len(rows) == 0 && len(p.Pending) == 0 && len(p.notes) == 0 && len(p.xml) == 0 {
		return nil
	}
	n := len(p.Protected) + len(p.Excluded)
	out := []string{fmt.Sprintf("수정 전 확인: 수정대상 %s건 · 수정제외 %s건 — 현재값 -> 변경값 (BMC Pending 까지만 설정, 재부팅 없음)",
		commaInt(len(p.Items)), commaInt(n))}
	out = append(out, rows...)
	if len(p.nCmp) > 0 {
		out = append(out, "※ 비교서버 값은 적용 때 레지스트리 형식을 다시 확인해 Enumeration·Integer·Boolean 만 보냅니다 (문자열·형식 불명은 수정제외)")
	}
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
	if len(p.xml) > 0 {
		out = append(out, "", "제외:")
		for _, s := range p.xml {
			out = append(out, "  "+s)
		}
	}
	return out
}

// ExcludedLines 는 확인 화면 2 입니다: 수정제외 항목만 다시 "현재값 -> 변경값 (수정제외: 사유)" 로. 없으면 nil.
func (p *setPlan) ExcludedLines() []string {
	rows := p.confirmRows(false)
	if len(rows) == 0 {
		return nil
	}
	return append([]string{fmt.Sprintf("아래 항목은 변경하지 않습니다 (수정제외 %s건)", commaInt(len(rows)))}, rows...)
}

// PendingLines 는 "Pending 이 있는 장비를 포함할까요?" 질문 전에 보여 줄 줄들입니다.
func (p *setPlan) PendingLines() []string {
	planned := map[string]bool{}
	for _, it := range p.Items {
		planned[it.Host+"\x00"+it.Attr] = true
	}
	var out []string
	for _, x := range p.Pending {
		line := fmt.Sprintf("%s  %s  Pending=%s", x.Host, sanitizeDetail(x.Attr), planVal(x.Pending))
		if planned[x.Host+"\x00"+x.Attr] {
			line += "  (설정할 속성 — 포함하면 이 Pending 값을 덮어씀)"
		}
		out = append(out, line)
	}
	return out
}

// setOutcome 은 적용 결과입니다 (설정 후 Pending 재조회 결과 포함).
type setOutcome struct {
	Lines     []string // 호스트별 결과 줄 (TUI 에 그대로 표시)
	OK        int      // Pending 재조회로 목표값 확인된 호스트 수
	Fail      int      // 실패·미반영 호스트 수
	Cancelled int      // 취소로 시작하지 않은 호스트 수
	Skipped   int      // 남의 Pending 제외·같은 BMC 중복·쓰기 대상 아닌 벤더로 건너뛴 호스트 수
	// Tried 는 PATCH 를 시도한 호스트(성공·실패 모두)의 점검 결과 순번입니다 (u.Check.Hosts 인덱스, 오름차순).
	// 보내기 전에 건너뛴 호스트(취소·제외·중복·막힘)는 없습니다. TUI 가 이 호스트만 다시 점검합니다.
	Tried []int
}

var errPlanMismatch = errors.New("설정 계획이 점검 결과와 맞지 않습니다 (다시 점검하십시오) — 아무것도 설정하지 않음")

// applySetPlan 은 계획을 BMC Pending 까지 적용하고(재부팅 없음) Pending 을 다시 읽어 반영 여부를 확인합니다.
// includePending=false 면 남의 Pending 이 있는 호스트는 건너뜁니다. report 는 호스트별 진행 줄(시작·끝, 한 번에 하나씩
// 불림), cancel 이 닫히면 아직 시작하지 않은 호스트는 건너뜁니다 (진행 중인 호스트는 PATCH·Job 사이에서 끊지 않고 마침).
// 반환값이 nil 이 아니면 결과를 쓸 수 있고, 이때 error 는 결과 파일 기록 경고입니다.
func applySetPlan(rc *runContext, u *unifiedRun, p *setPlan, includePending bool, report func(host, msg string), cancel <-chan struct{}) (*setOutcome, error) {
	switch {
	case rc == nil || rc.Conf == nil || u == nil || u.Check == nil || p == nil:
		return nil, errors.New("설정 계획·점검 결과가 없습니다 — 아무것도 설정하지 않음")
	case u.Check.FromDump != "" || u.fromDump != "":
		return nil, errors.New("덤프 읽기 모드에서는 설정할 수 없습니다")
	}
	out := &setOutcome{}
	if p.Empty() {
		return out, nil
	}
	// 계획을 호스트별로 묶는다 (점검 결과와 하나라도 맞지 않으면 아무것도 보내지 않음)
	pend := map[string]map[string]string{}
	for _, x := range p.Pending {
		if pend[x.Host] == nil {
			pend[x.Host] = map[string]string{}
		}
		pend[x.Host][x.Attr] = x.Pending
	}
	var hosts []*setHost
	byIdx := map[int]*setHost{}
	idxOf := map[*setHost]int{}
	userSkip := map[*setHost]bool{}
	for _, it := range p.Items {
		if it.hostIdx < 0 || it.hostIdx >= len(u.Check.Hosts) || (it.Source != SetSrcProfile && it.Source != SetSrcCompare && it.Source != SetSrcDirect) {
			return nil, errPlanMismatch
		}
		hc := u.Check.Hosts[it.hostIdx]
		if hc == nil || hc.Target.Hostname != it.Host || hc.Status != "" || hc.Info == nil || hc.XML {
			return nil, errPlanMismatch
		}
		h := byIdx[it.hostIdx]
		if h == nil {
			h = &setHost{Target: hc.Target, Info: hc.Info}
			byIdx[it.hostIdx] = h
			idxOf[h] = it.hostIdx
			hosts = append(hosts, h)
		}
		if it.PendingHost {
			// Dell 은 설정 Job 이 남의 Pending 까지 적용하므로 포함하기로 했어도 쓰지 않는다 (계획 단계에서 이미 뺌, 방어 중첩).
			dell := hc.Info.Vendor == "Dell"
			userSkip[h] = !includePending || dell
			if includePending && !dell {
				h.okPend = pend[it.Host]
			}
		}
		h.Items = append(h.Items, &applyItem{Std: it.std, Attr: it.Attr, Expected: it.To, Compare: it.Source == SetSrcCompare, Direct: it.Source == SetSrcDirect})
	}
	e, err := newSetEnv(rc, u.Check, true)
	if err != nil {
		return nil, err
	}
	markDuplicates(hosts)

	var mu sync.Mutex
	say := func(h *setHost, msg string) {
		if report != nil {
			mu.Lock()
			defer mu.Unlock()
			report(h.Target.Hostname, msg)
		}
	}
	var todo []*setHost
	for _, h := range hosts {
		if userSkip[h] {
			why := "남의 Pending 이 있어 포함하지 않기로 함 — 건드리지 않음"
			if h.Info != nil && h.Info.Vendor == "Dell" {
				why = "Dell 은 설정 Job 이 남의 Pending 까지 적용하므로 제외 — 건드리지 않음"
			}
			h.finish(StatusPendingExists, why)
			say(h, hostHead(h, true))
			continue
		}
		todo = append(todo, h)
	}
	sweepHosts(len(todo), rc.Conf.Concurrency, rc.Conf.AuthFailStop,
		func(i int) hostOutcome {
			h := todo[i]
			select {
			case <-cancel:
				h.finish(StatusCanceled, "취소 (Ctrl+X) — 시작하지 않음")
				say(h, hostHead(h, false))
				return outNone
			default:
			}
			say(h, "설정 시작")
			e.runHost(h)
			say(h, hostHead(h, false))
			return outcomeOf(h.authStatus(), h.Stats, h.contacted)
		},
		func(i int) {
			todo[i].finish(StatusSkippedAuthStop, "AUTH_FAIL 차단기(auth_fail_stop)가 작동해 접속하지 않았습니다")
			say(todo[i], hostHead(todo[i], false))
		})

	for _, h := range hosts {
		if h.Tried {
			out.Tried = append(out.Tried, idxOf[h])
		}
		switch hostKind(h, userSkip[h]) {
		case "완료":
			out.OK++
		case "취소":
			out.Cancelled++
		case "제외":
			out.Skipped++
		default:
			out.Fail++
		}
		out.Lines = append(out.Lines, h.Target.Hostname+"  "+hostHead(h, userSkip[h]))
		for _, it := range h.Items {
			out.Lines = append(out.Lines, fmt.Sprintf("    %s → %s  %s (%s)", sanitizeDetail(it.Attr), planVal(it.Expected), it.Status, it.Detail))
		}
	}
	sort.Ints(out.Tried)
	out.Lines = append(out.Lines, fmt.Sprintf("설정 결과: 완료 %d대, 실패·미반영 %d대, 취소 %d대, 제외 %d대", out.OK, out.Fail, out.Cancelled, out.Skipped))
	ferr := writeApplyFiles(u.Check.Dir, hosts)
	out.Lines = append(out.Lines, "설정 결과 파일: "+filepath.ToSlash(u.Check.Dir)+"/ (apply_result.tsv, applied.txt, apply_failed.txt, apply_requests.txt)")
	out.Lines = append(out.Lines, strings.Split(applyNotice, "\n")...)
	if ferr != nil {
		return out, fmt.Errorf("설정 결과 파일 기록 실패: %w", ferr)
	}
	return out, nil
}

// hostKind 는 호스트 결과 분류입니다: 완료(모든 항목이 Pending·현재값으로 목표값 확인) / 취소 / 제외 / 미반영.
func hostKind(h *setHost, userSkip bool) string {
	n := 0
	for _, it := range h.Items {
		switch it.Status {
		case StatusApplied, StatusAlreadyOK:
			n++
		case StatusCanceled:
			return "취소"
		case StatusDuplicateTarget, StatusSetNotSupportedVendor:
			return "제외"
		}
	}
	switch {
	case userSkip:
		return "제외"
	case n == len(h.Items):
		return "완료"
	}
	return "미반영"
}

// hostHead 는 호스트 결과 머리 줄입니다 (report 와 setOutcome.Lines 가 같이 씀).
func hostHead(h *setHost, userSkip bool) string {
	k := hostKind(h, userSkip)
	n := 0
	for _, it := range h.Items {
		if it.Status == StatusApplied || it.Status == StatusAlreadyOK {
			n++
		}
	}
	switch k {
	case "완료":
		return fmt.Sprintf("[완료] Pending 목표값 확인 %d/%d건", n, len(h.Items))
	case "취소":
		return "[취소] 시작하지 않음"
	case "제외":
		return "[제외] " + h.Items[0].Detail
	}
	return fmt.Sprintf("[미반영] 목표값 확인 %d/%d건 — 항목별 사유 확인", n, len(h.Items))
}
