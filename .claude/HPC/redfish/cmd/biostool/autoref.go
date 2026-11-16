package main

// autoref.go — 전체비교 기준 "대표서버 자동 지정" (수정계획서_대표서버_기준맞추기.md 3.A·4).
//
// 이미 읽은 스냅샷(unifiedRun.Snaps)만으로 묶음마다 대표 1대를 고릅니다 — BMC 에 다시 접속하지 않습니다.
//   - 묶음: 읽기에 성공한(snap.ok()) Redfish 스냅샷을 태그 모드면 (태그, 모델 키), 아니면 모델 키로 나눔 (XML 대상은 읽기 전용이라 빠짐)
//   - 대표 선정 순서 (고정, 같은 입력 → 같은 결과):
//     (a) 묶음에서 가장 많은 BIOS 버전의 호스트만
//     (b) 프로파일 점검 difference 0건인 호스트 우선 (하나도 없거나 점검 결과를 모르면 건너뜀)
//     (c) 다수결 일치 수가 가장 큰 호스트 — 속성마다 묶음에서 가장 많은 값을 구하고 그 값과 같은 속성 수.
//         제외 목록(unifiedRun.Ignore)·고유값 모양 값은 비교(compareAttrs)와 같은 기준으로 세지 않음
//     (d) 동률이면 입력(user.txt) 순서
//   - 대표 자신은 비교에서 REFERENCE (classify 가 같은 태그·같은 모델의 다른 기준이 없으면 그렇게 처리), 1대뿐인 묶음도 그 1대가 REFERENCE.
//   - 사람이 묶음별로 다른 호스트를 고르면(pick) 그 호스트가 대표가 됩니다 (다시 접속하지 않음).
//   - compare_refs.tsv (묶음·대표·대수·BIOS 버전·선정 사유) 를 결과 폴더에 씁니다 (writeAutoRefs).

import (
	"fmt"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

// autoRefsFile 은 대표서버 선정 결과 파일 이름입니다 (결과 폴더).
const autoRefsFile = "compare_refs.tsv"

// autoBundle 은 대표 1대를 고르는 묶음입니다.
type autoBundle struct {
	Tag     string // 태그 모드의 태그 ("" 면 태그 없음, 태그 모드가 아니면 늘 "")
	Key     string // 모델 키 (소문자 벤더|normalizeModel)
	Name    string // 사람이 읽는 이름: "[gold] R660" (태그 모드가 아니면 "R660")
	Idx     []int  // 묶음 구성원의 u.Snaps 번호 (입력 순서)
	Scores  []int  // Idx 와 같은 순서: 다수결 일치 수
	Total   int    // 다수결을 낸 속성 수 (일치 수의 분모)
	Rep     int    // 대표의 u.Snaps 번호
	Reason  string // 선정 사유 (BIOS 1.8.2 12/14대 · 다수결 일치 498/500)
	Manual  bool   // 사람이 직접 고른 대표
	AutoRep int    // 규칙대로 골랐을 때의 대표 (Manual 이면 직접 고르기 전 값)
}

func autoBundleID(tagMode bool, tag, key string) string {
	if tagMode {
		return tag + "\x00" + key
	}
	return key
}

// ID 는 직접 고르기(pick)·재선택에서 이 묶음을 가리키는 키입니다.
func (b *autoBundle) ID(tagMode bool) string { return autoBundleID(tagMode, b.Tag, b.Key) }

// autoPlan 은 대표서버 자동 지정 결과입니다 (묶음은 처음 나온 입력 순서).
type autoPlan struct {
	TagMode bool
	Bundles []*autoBundle
	snaps   []*biosSnap // 대표 이름·BIOS 를 찾는 u.Snaps (같은 슬라이스)
}

// RepSnap 은 묶음 대표의 스냅샷입니다.
func (p *autoPlan) RepSnap(b *autoBundle) *biosSnap { return p.snaps[b.Rep] }

// Choosable 은 대표를 바꿀 수 있는 묶음(구성원 2대 이상)이 있는지입니다.
func (p *autoPlan) Choosable() bool {
	for _, b := range p.Bundles {
		if len(b.Idx) > 1 {
			return true
		}
	}
	return false
}

// Announce 는 비교 직후 안내 줄입니다: "대표서버: [gold] R660 → host0001 · [silver] DL360 Gen11 → host0101 외 3묶음".
// 길면(표시 폭 100칸 가까이) 앞쪽만 보이고 나머지는 "외 N묶음" 으로 줄입니다. 전체 목록은 compare_refs.tsv.
func (p *autoPlan) Announce() string {
	const maxLen = 100
	var parts []string
	n := 0
	for i, b := range p.Bundles {
		part := b.Name + " → " + p.RepSnap(b).Target.Hostname
		if i > 0 && n+utf8.RuneCountInString(part)+3 > maxLen {
			break
		}
		parts = append(parts, part)
		n += utf8.RuneCountInString(part) + 3
	}
	s := "대표서버: " + strings.Join(parts, " · ")
	if rest := len(p.Bundles) - len(parts); rest > 0 {
		s += fmt.Sprintf(" 외 %d묶음", rest)
	}
	return s
}

// autoPlanFor 는 읽은 스냅샷으로 묶음과 대표를 정합니다. pick 은 묶음 ID → 대표로 삼을 호스트의 targetID (직접 고른 것,
// 그 묶음의 구성원이 아니면 무시). BMC 에 접속하지 않습니다.
func (u *unifiedRun) autoPlanFor(pick map[string]string) *autoPlan {
	tagMode := u.tagMode()
	p := &autoPlan{TagMode: tagMode, snaps: u.Snaps}
	byID := map[string]*autoBundle{}
	for i, s := range u.Snaps {
		if s == nil || !s.ok() {
			continue
		}
		tag := ""
		if tagMode {
			tag = s.Target.Tag
		}
		id := autoBundleID(tagMode, tag, s.Key)
		b := byID[id]
		if b == nil {
			b = &autoBundle{Tag: tag, Key: s.Key, Name: autoBundleName(tagMode, tag, s)}
			byID[id] = b
			p.Bundles = append(p.Bundles, b)
		}
		b.Idx = append(b.Idx, i)
	}
	for _, b := range p.Bundles {
		u.scoreBundle(b)
		u.pickRep(b, pick[b.ID(tagMode)])
	}
	return p
}

func autoBundleName(tagMode bool, tag string, s *biosSnap) string {
	model := strings.TrimSpace(s.Model)
	if model == "" {
		model = s.Key
	}
	if !tagMode {
		return model
	}
	if tag == "" {
		return "[태그 없음] " + model
	}
	return "[" + tag + "] " + model
}

// scoreBundle 은 묶음의 다수결을 셉니다: 속성마다 가장 많은 값(동수면 입력 순서가 앞선 호스트의 값)을 구하고
// 구성원마다 그 값과 같은 속성 수를 Scores 에 채웁니다. 제외 목록에 걸린 속성·고유값 모양 값은 세지 않습니다.
func (u *unifiedRun) scoreBundle(b *autoBundle) {
	type vote struct{ n, first int }
	votes := map[string]map[string]*vote{}
	ignored := map[string]bool{} // 속성 이름 → 제외 여부 (호스트마다 같은 이름을 다시 검사하지 않음)
	skip := func(name, v string) bool {
		ig, ok := ignored[name]
		if !ok {
			ig = u.Ignore.match(name)
			ignored[name] = ig
		}
		if ig {
			return true
		}
		return looksUniqueAttr(name, v)
	}
	for order, si := range b.Idx {
		for name, v := range u.Snaps[si].Attrs {
			if skip(name, v) {
				continue
			}
			m := votes[name]
			if m == nil {
				m = map[string]*vote{}
				votes[name] = m
			}
			e := m[v]
			if e == nil {
				e = &vote{first: order}
				m[v] = e
			}
			e.n++
		}
	}
	major := make(map[string]string, len(votes))
	for name, m := range votes {
		var best string
		var bv *vote
		for v, e := range m {
			if bv == nil || e.n > bv.n || (e.n == bv.n && e.first < bv.first) {
				best, bv = v, e
			}
		}
		major[name] = best
	}
	b.Total = len(votes)
	b.Scores = make([]int, len(b.Idx))
	for k, si := range b.Idx {
		for name, v := range u.Snaps[si].Attrs {
			if mv, ok := major[name]; ok && mv == v {
				b.Scores[k]++
			}
		}
	}
}

// profileDiffs 는 호스트의 프로파일 점검 difference 항목 수입니다. 점검 결과를 모르면(점검 결과 없음·읽기 오류) -1.
func (u *unifiedRun) profileDiffs(si int) int {
	if u.Check == nil || si >= len(u.Check.Hosts) || u.Check.Hosts[si] == nil || u.Check.Hosts[si].Status != "" {
		return -1
	}
	n := 0
	for _, it := range u.Check.Hosts[si].Items {
		if tagFor(it.Result) == TagDiff {
			n++
		}
	}
	return n
}

// pickRep 은 4장의 순서로 대표를 정하고 선정 사유를 만듭니다. manual 이 이 묶음 구성원의 targetID 면 그 호스트를 대표로 씁니다.
func (u *unifiedRun) pickRep(b *autoBundle, manual string) {
	// 확정 순서: (1) 다수결 일치 점수가 가장 큰 호스트. 동점이면 (2) 가장 많은 BIOS 버전의 호스트
	// (동수면 입력 순서가 앞선 호스트의 버전) → (3) 프로파일 difference 0건 호스트 → (4) 입력 순서.
	// (2)·(3) 은 동점 후보 안에서 해당하는 호스트가 하나도 없으면 건너뜁니다.
	ver := func(k int) string { return strings.TrimSpace(u.Snaps[b.Idx[k]].BiosVersion) }
	type verN struct{ n, first int }
	vers := map[string]*verN{}
	for k := range b.Idx {
		v := ver(k)
		if vers[v] == nil {
			vers[v] = &verN{first: k}
		}
		vers[v].n++
	}
	var major string
	var mv *verN
	for v, e := range vers {
		if mv == nil || e.n > mv.n || (e.n == mv.n && e.first < mv.first) {
			major, mv = v, e
		}
	}
	top := b.Scores[0]
	for _, s := range b.Scores {
		if s > top {
			top = s
		}
	}
	var cand []int // b.Idx 안의 위치
	for k := range b.Idx {
		if b.Scores[k] == top {
			cand = append(cand, k)
		}
	}
	var sameVer []int
	for _, k := range cand {
		if ver(k) == major {
			sameVer = append(sameVer, k)
		}
	}
	if len(sameVer) > 0 {
		cand = sameVer
	}
	var zero []int
	for _, k := range cand {
		if u.profileDiffs(b.Idx[k]) == 0 {
			zero = append(zero, k)
		}
	}
	if len(zero) > 0 {
		cand = zero
	}
	best := cand[0] // 입력 순서가 가장 앞선 호스트 (b.Idx 는 입력 순서)
	b.AutoRep = b.Idx[best]
	b.Rep, b.Manual = b.AutoRep, false
	b.Reason = autoReason(b, best, ver(best), vers[ver(best)].n, u.profileDiffs(b.AutoRep) == 0)
	if manual == "" {
		return
	}
	for k, si := range b.Idx {
		if targetID(u.Snaps[si].Target) != manual || si == b.AutoRep {
			continue
		}
		b.Rep, b.Manual = si, true
		b.Reason = fmt.Sprintf("직접 지정 · BIOS %s · 다수결 일치 %d/%d (자동 선정은 %s)",
			dash(strings.TrimSpace(u.Snaps[si].BiosVersion)), b.Scores[k], b.Total, u.Snaps[b.AutoRep].Target.Hostname)
		return
	}
}

func autoReason(b *autoBundle, k int, ver string, nVer int, profOK bool) string {
	if len(b.Idx) == 1 {
		return fmt.Sprintf("묶음에 1대뿐 (비교 대상 없음) · BIOS %s", dash(ver))
	}
	s := fmt.Sprintf("BIOS %s %d/%d대 · 다수결 일치 %d/%d", dash(ver), nVer, len(b.Idx), b.Scores[k], b.Total)
	if profOK {
		s += " · 프로파일 difference 0건"
	}
	return s
}

// autoRefs 는 대표들을 비교 기준(allRef)으로 만듭니다. 이미 읽은 스냅샷을 그대로 쓰므로 BMC 에 접속하지 않고,
// 기준의 태그는 대표 자신의 태그입니다 (classify 가 같은 태그의 대표만 고름).
func (p *autoPlan) autoRefs() []*allRef {
	refs := make([]*allRef, 0, len(p.Bundles))
	for _, b := range p.Bundles {
		s := p.RepSnap(b)
		in := s.Target.Input
		if in == "" {
			in = s.Target.Hostname
		}
		refs = append(refs, &allRef{Entry: refEntry{Input: in, Tag: s.Target.Tag}, Target: s.Target, ID: targetID(s.Target), Order: len(refs), Snap: s})
	}
	return refs
}

// writeAutoRefs 는 결과 폴더에 compare_refs.tsv 를 씁니다 (묶음·대표·대수·BIOS 버전·선정 사유). 경로를 돌려줍니다.
func writeAutoRefs(dir string, p *autoPlan) (string, error) {
	path := filepath.Join(dir, autoRefsFile)
	err := writeTSVRows(path, []string{"bundle", "representative", "count", "bios_version", "reason"}, func(emit func(cols ...string)) {
		for _, b := range p.Bundles {
			s := p.RepSnap(b)
			emit(b.Name, s.Target.Hostname, fmt.Sprint(len(b.Idx)), dash(strings.TrimSpace(s.BiosVersion)), b.Reason)
		}
	})
	return path, err
}
