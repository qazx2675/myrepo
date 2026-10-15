package main

// setplan.go — 프로파일 difference + 전체비교 difference 를 합친 설정 계획과 적용 (계획서_v0.2_TUI.md 흐름 6, 단계 7).
// 단계 6(TUI 흐름)과 단계 7(설정 엔진)이 함께 쓰는 API 입니다.
//
// 안전장치 (set.go 의 겹겹 방어를 그대로 타고, 계획 단계에서 한 번 더 거른다):
//  1. 계획(buildSetPlan)은 BMC 에 접속하지 않는다. 프로파일은 verified=Y 인 FAIL(·설정 가능한 PENDING_NO_JOB)만,
//     전체비교는 allcheck_set=on 이고 같은 모델 기준과 비교했고 속성 이름 목록이 같은(sameAttrNames) 호스트의
//     값 차이(DIFF)만. 제외 목록(ignore ∪ exclude_attrs)·위험 속성(checkAttributes)은 미리 빼서 미리보기에 보인다.
//  2. 프로파일이 정하는 속성은 비교서버 값으로 따로 설정하지 않는다. 목표값이 다르면 둘 다 설정하지 않고 [충돌].
//  3. 남의 Pending 이 있는 호스트는 목록(호스트·속성·값)을 보여 주고, 포함하기로 했을 때만 그 목록과 정확히 같은
//     Pending 을 허용한다 (적용 때 새로 생긴 Pending 이 있으면 set.go 가 PENDING_EXISTS 로 쓰지 않음).
//  4. 적용(applySetPlan)은 set.go 의 runHost 그대로: 재조회·레지스트리 검사·호스트당 PATCH 1회(+Dell Job 최대 1회)·
//     재시도 없음·Pending 재조회 확인·같은 BMC 중복 방지·AUTH_FAIL 차단기. 재부팅·리셋은 하지 않는다.
//  5. 비대화형(bios -auto / -auto-check)은 설정하지 않는다: applySetPlan 은 TUI 흐름(tui_flow.go)에서만 부르며
//     setplan_test.go 가 그 밖의 호출을 막는다.

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
)

// setItem 은 호스트 1대의 속성 1개를 To 값으로 바꾸는 계획입니다.
type setItem struct {
	Host   string
	Attr   string
	From   string // 현재값
	To     string // 목표값 (프로파일은 value 원문 A|B — 보낼 값은 적용 때 BMC 가 받는 표기로 고름)
	Source string // SetSrcProfile / SetSrcCompare

	PendingHost bool   // 이 호스트에 남의 Pending 이 있음 → includePending=false 면 이 호스트는 설정하지 않음
	Note        string // 미리보기 참고 (PENDING_NO_JOB, Pending 이 이미 목표값 등)

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

// setPlan 은 "수정할까요?" 직전에 보여 줄 설정 계획입니다.
type setPlan struct {
	Items     []setItem
	Conflicts []setConflict
	Pending   []pendingInfo // 남의 Pending 이 있는 호스트 (포함 여부는 적용 때 결정)
	Skipped   []string      // 제외 사유 줄 (verified≠Y, 제외 목록, 위험 속성, 모델·속성목록 불일치, allcheck_set=off 등)
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

// planVal 은 미리보기·결과 줄에 넣는 BMC 값 표기입니다 (제어문자 제거·60자, 빈 문자열은 "").
func planVal(s string) string {
	if s == "" {
		return `""`
	}
	return bv(s)
}

// buildSetPlan 은 점검 결과(u)와 전체비교 결과(cmp, 없으면 nil)로 설정 계획을 만듭니다. BMC 에 접속하지 않습니다.
func buildSetPlan(cfg *Config, u *unifiedRun, cmp *compareResult) *setPlan {
	p := &setPlan{}
	switch {
	case u == nil || u.Check == nil:
		p.Skipped = append(p.Skipped, "점검 결과가 없어 설정할 수 없습니다")
		return p
	case u.Check.FromDump != "" || u.fromDump != "":
		p.Skipped = append(p.Skipped, "덤프 읽기 모드에서는 설정하지 않습니다")
		return p
	}
	useCmp := cmp != nil && cfg != nil && cfg.AllcheckSet
	if cmp != nil && !useCmp {
		p.Skipped = append(p.Skipped, "전체비교 difference 는 설정하지 않음 (bios.conf allcheck_set=off)")
	}
	if useCmp && len(cmp.Hosts) != len(u.Check.Hosts) {
		p.Skipped = append(p.Skipped, "전체비교 결과와 점검 대상이 맞지 않아 전체비교 difference 는 설정하지 않음")
		useCmp = false
	}
	for i, hc := range u.Check.Hosts {
		var ch *allHost
		if useCmp && cmp.Hosts[i] != nil && cmp.Hosts[i].Target == hc.Target {
			ch = cmp.Hosts[i]
		}
		var ign *ignoreSet
		if cmp != nil {
			ign = cmp.Ignore
		}
		p.addHost(u, i, hc, ch, ign)
	}
	return p
}

// addHost 는 호스트 1대의 항목을 계획에 넣습니다 (프로파일 → 전체비교 → 충돌 → 쓰기 가능 여부 → 남의 Pending).
func (p *setPlan) addHost(u *unifiedRun, i int, hc *hostCheck, ch *allHost, ign *ignoreSet) {
	if hc == nil || hc.Status != "" {
		return // 접속·조회 실패는 N/A (설정할 difference 가 없음)
	}
	name := hc.Target.Hostname
	skip := func(format string, a ...interface{}) {
		p.Skipped = append(p.Skipped, name+"  "+fmt.Sprintf(format, a...))
	}

	// 1) 프로파일: verified=Y 인 FAIL·설정 가능한 PENDING_NO_JOB, 남의 Pending 때문에 막힌 PENDING_EXISTS (포함 여부는 따로 묻는다)
	var items []setItem
	for _, it := range hc.Items {
		switch {
		case it.Verified && (hc.settable(it) || it.Result == StatusPendingExists):
		case it.Result == StatusUnverified:
			skip("%s: 프로파일 행이 verified=N (UNVERIFIED) — 설정하지 않음", sanitizeDetail(it.Attr))
			continue
		case it.Result == StatusPendingNoJob:
			skip("%s: PENDING_NO_JOB 이나 verified=Y 가 아니거나 남의 Pending 이 있어 설정하지 않음", sanitizeDetail(it.Attr))
			continue
		default:
			continue // OK·PENDING_OK·MAPPING_MISSING(N/A)
		}
		if why := attrBlocked(it.Attr, splitAllowed(it.Expected)[0]); why != "" {
			skip("%s: 설정 불가 속성 (%s) — 설정하지 않음", sanitizeDetail(it.Attr), why)
			continue
		}
		si := setItem{Host: name, Attr: it.Attr, From: it.Current, To: it.Expected, Source: SetSrcProfile, std: it.Std, hostIdx: i}
		if it.Result == StatusPendingNoJob {
			si.Note = "PENDING_NO_JOB: Pending=기대값이나 BIOS 설정 Job 없음 → 같은 값을 다시 보내고 Job 생성"
		}
		items = append(items, si)
	}

	// 2) 전체비교: 같은 모델 기준과 비교했고 속성 이름 목록이 같은 호스트의 값 차이(DIFF)만
	if ch != nil {
		switch {
		case ch.Status == StatusIsReference || ch.Status == StatusSame:
		case !ch.compared():
			skip("전체비교 대상 아님 (%s) — 비교서버 값으로 설정하지 않음", ch.Status)
		case !ch.sameAttrNames():
			skip("속성 목록이 비교서버와 달라 설정 제외")
		case ch.Ref == nil || ch.Ref.Snap == nil || ch.Snap == nil || ch.Ref.Snap.Key != ch.Snap.Key || normalizeModel(ch.Snap.Model) == "":
			skip("비교서버와 같은 모델인지 확인할 수 없어 설정 제외")
		default:
			items = p.compareItems(name, i, hc, ch, ign, items)
		}
	}
	if len(items) == 0 {
		return
	}

	// 3) 호스트 단위 쓰기 가능 여부 (적용 때 runHost 가 다시 막지만 미리보기에 먼저 보인다)
	switch {
	case hc.Info == nil || !setVendors[hc.Info.Vendor]:
		skip("쓰기 대상 벤더가 아님 — Dell·HPE·Lenovo 만 설정 (%d건 제외)", len(items))
		return
	case hc.Info.SettingsPath == "":
		skip("Bios Settings 경로가 없어 설정할 수 없음 (%d건 제외)", len(items))
		return
	case normalizeModel(hc.Info.Model) == "":
		skip("BMC 가 보고한 모델명이 비어 있어 설정하지 않음 (%d건 제외)", len(items))
		return
	}

	// 4) 남의 Pending: 우리 표준값(이 호스트에 매핑되는 프로파일 행의 허용값)도 이번 목표값도 아닌 Pending (set.go planHost 와 같은 정의)
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
	p.Pending = append(p.Pending, foreign...)
	for j := range items {
		items[j].PendingHost = len(foreign) > 0
		if v, ok := hc.pendDiff[items[j].Attr]; ok && items[j].Source == SetSrcCompare && matchAny(items[j].allowed(), v) {
			items[j].Note = "Pending 이 이미 목표값 (재부팅 대기)"
		}
	}
	p.Items = append(p.Items, items...)
}

// compareItems 는 전체비교 DIFF 줄을 항목으로 바꿔 items 에 더합니다. 프로파일이 정하는 속성은 비교서버 값으로 따로
// 설정하지 않고, 목표값이 다르면 프로파일 항목까지 빼고 [충돌] 로 남깁니다.
func (p *setPlan) compareItems(name string, i int, hc *hostCheck, ch *allHost, ign *ignoreSet, items []setItem) []setItem {
	skip := func(format string, a ...interface{}) {
		p.Skipped = append(p.Skipped, name+"  "+fmt.Sprintf(format, a...))
	}
	for _, d := range ch.Diffs {
		if d.Status != attrDiff || ign.match(d.Attr) {
			continue // ONLY_REF·ONLY_HOST(N/A·없음)와 제외 목록은 설정하지 않음 (sameAttrNames 면 ONLY_* 는 없음)
		}
		if why := attrBlocked(d.Attr, d.Ref); why != "" {
			skip("%s: 설정 불가 속성 (%s) — 비교서버 값으로 설정하지 않음", sanitizeDetail(d.Attr), why)
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
				if planned >= 0 {
					items = append(items[:planned], items[planned+1:]...)
				}
			case planned < 0:
				skip("%s: 프로파일 항목(%s, %s)이 정하는 속성 — 비교서버 값으로 따로 설정하지 않음", sanitizeDetail(d.Attr), pi.Std, pi.Result)
			}
			continue // 목표값이 같고 프로파일 항목이 있으면 하나만 남김 (출처 프로파일)
		}
		std := SetSrcCompare
		if isSysProfileAttr(d.Attr) {
			std = profileStd
		}
		items = append(items, setItem{Host: name, Attr: d.Attr, From: d.Host, To: d.Ref, Source: SetSrcCompare, std: std, hostIdx: i})
	}
	return items
}

// Empty 는 설정할 항목이 하나도 없는지입니다.
func (p *setPlan) Empty() bool { return len(p.Items) == 0 }

// Preview 는 TUI 미리보기에 그대로 보여 줄 줄들입니다 (설정 항목, [충돌], Pending, 제외 사유).
// 항목·충돌·Pending·제외 사유가 모두 없으면 nil 입니다.
func (p *setPlan) Preview() []string {
	if p.Empty() && len(p.Conflicts) == 0 && len(p.Pending) == 0 && len(p.Skipped) == 0 {
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
			line := fmt.Sprintf("  %s  %s  %s → %s  (%s)", it.Host, sanitizeDetail(it.Attr), planVal(it.From), planVal(it.To), src)
			if it.PendingHost {
				line += "  [남의 Pending]"
			}
			if it.Note != "" {
				line += "  ※ " + it.Note
			}
			out = append(out, line)
		}
	}
	for _, c := range p.Conflicts {
		out = append(out, fmt.Sprintf("[충돌] %s %s 프로파일=%s 비교서버=%s (둘 다 설정하지 않음)", c.Host, sanitizeDetail(c.Attr), planVal(c.ProfileTo), planVal(c.CompareTo)))
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
	userSkip := map[*setHost]bool{}
	for _, it := range p.Items {
		if it.hostIdx < 0 || it.hostIdx >= len(u.Check.Hosts) || (it.Source != SetSrcProfile && it.Source != SetSrcCompare) {
			return nil, errPlanMismatch
		}
		hc := u.Check.Hosts[it.hostIdx]
		if hc == nil || hc.Target.Hostname != it.Host || hc.Status != "" || hc.Info == nil {
			return nil, errPlanMismatch
		}
		h := byIdx[it.hostIdx]
		if h == nil {
			h = &setHost{Target: hc.Target, Info: hc.Info}
			byIdx[it.hostIdx] = h
			hosts = append(hosts, h)
		}
		if it.PendingHost {
			userSkip[h] = !includePending
			if includePending {
				h.okPend = pend[it.Host]
			}
		}
		h.Items = append(h.Items, &applyItem{Std: it.std, Attr: it.Attr, Expected: it.To, Compare: it.Source == SetSrcCompare})
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
			h.finish(StatusPendingExists, "남의 Pending 이 있어 포함하지 않기로 함 — 건드리지 않음")
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
