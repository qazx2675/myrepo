// hpcbot — 키워드 점수 엔진 (정규화·매칭·점수·판정·영타 자동 감지)
package main

import (
	"slices"
	"sort"
	"strings"
	"unicode"
)

// 판정 모드
const (
	ModeAnswer     = "answer"
	ModeCandidates = "candidates"
	ModeNoMatch    = "nomatch"
)

// Hit 은 블록 하나의 점수다.
type Hit struct {
	ID    string  `json:"id"`
	Score float64 `json:"score"`
}

// Verdict 는 질문 1건의 판정 결과다.
type Verdict struct {
	Mode      string // answer | candidates | nomatch
	Top       []Hit  // 점수 > 0 인 상위 3개 (내림차순)
	Cands     []Hit  // candidates 모드의 후보 (최대 3)
	Input     string // 원문
	Converted string // 영타 변환본 (변환하지 않았으면 "")
	Auto      bool   // 자동 감지로 변환했는지 (false 이고 Converted != "" 이면 /h)
}

// ID 는 answer 모드의 1위 블록 id 다 (그 외 모드는 "").
func (v Verdict) ID() string {
	if v.Mode == ModeAnswer && len(v.Top) > 0 {
		return v.Top[0].ID
	}
	return ""
}

type kwEntry struct {
	runes []rune
	asc   bool // 영문·숫자로만 된 키워드 (경계 조건 적용)
	w     map[string]float64
}

// Matcher 는 로드된 KB 로 만든 점수 엔진이다 (읽기 전용, 동시 사용 가능).
type Matcher struct {
	d         *Data
	kws       []kwEntry // 긴 것부터
	order     []string  // 활성 intent id (kb 순서)
	req       map[string][]string
	protected map[string]bool
}

// normalize 는 소문자 + 공백 제거 룬열과 위치별 boundaryBefore 를 만든다.
func normalize(s string) ([]rune, []bool) {
	var c []rune
	var bb []bool
	space := true // i==0 도 경계
	for _, r := range s {
		if unicode.IsSpace(r) {
			space = true
			continue
		}
		c = append(c, unicode.ToLower(r))
		bb = append(bb, space)
		space = false
	}
	return c, bb
}

func isAlnumASCII(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')
}

func allAlnumASCII(rs []rune) bool {
	for _, r := range rs {
		if !isAlnumASCII(r) {
			return false
		}
	}
	return len(rs) > 0
}

// NewMatcher 는 Data 의 활성 블록과 키워드 가중치로 엔진을 만든다.
func NewMatcher(d *Data) *Matcher {
	m := &Matcher{d: d, req: map[string][]string{}}
	stop := map[string]bool{}
	for _, s := range d.KB.Stopwords {
		stop[canonKW(s)] = true
	}
	active := map[string]bool{}
	for _, it := range d.ActiveIntents() {
		active[it.ID] = true
		m.order = append(m.order, it.ID)
		for _, r := range it.RequireAny {
			m.req[it.ID] = append(m.req[it.ID], canonKW(r))
		}
	}
	for kw, ws := range d.KB.Keywords {
		kw = canonKW(kw)
		if kw == "" || stop[kw] {
			continue
		}
		w := map[string]float64{}
		for id, v := range ws {
			if active[id] && v > 0 {
				w[id] = v
			}
		}
		if len(w) == 0 {
			continue
		}
		rs := []rune(kw)
		m.kws = append(m.kws, kwEntry{runes: rs, asc: allAlnumASCII(rs), w: w})
	}
	sort.Slice(m.kws, func(i, j int) bool {
		if len(m.kws[i].runes) != len(m.kws[j].runes) {
			return len(m.kws[i].runes) > len(m.kws[j].runes)
		}
		return string(m.kws[i].runes) < string(m.kws[j].runes)
	})
	m.protected = ProtectedWords(&d.KB, d.Manual)
	return m
}

func canonKW(s string) string {
	return strings.Join(strings.Fields(strings.ToLower(s)), "")
}

func index(c, k []rune, from int) int {
	for i := from; i+len(k) <= len(c); i++ {
		j := 0
		for j < len(k) && c[i+j] == k[j] {
			j++
		}
		if j == len(k) {
			return i
		}
	}
	return -1
}

// found 는 질문에서 찾은 키워드 목록이다 (긴 것 우선, 겹침 제외, 키워드당 1회).
func (m *Matcher) found(text string) []*kwEntry {
	c, bb := normalize(text)
	used := make([]bool, len(c))
	var out []*kwEntry
	for i := range m.kws {
		e := &m.kws[i]
		for from := 0; ; {
			p := index(c, e.runes, from)
			if p < 0 {
				break
			}
			from = p + 1
			end := p + len(e.runes)
			if e.asc {
				if p > 0 && !bb[p] && isAlnumASCII(c[p-1]) {
					continue
				}
				if end < len(c) && !bb[end] && isAlnumASCII(c[end]) {
					continue
				}
			}
			overlap := false
			for k := p; k < end; k++ {
				if used[k] {
					overlap = true
					break
				}
			}
			if overlap {
				continue
			}
			for k := p; k < end; k++ {
				used[k] = true
			}
			out = append(out, e)
			break
		}
	}
	return out
}

// Score 는 점수 > 0 인 블록을 점수 내림차순(동점은 kb 순서)으로 돌려준다.
func (m *Matcher) Score(text string) []Hit {
	sum := map[string]float64{}
	for _, e := range m.found(text) {
		for id, w := range e.w {
			sum[id] += w
		}
	}
	if len(sum) == 0 {
		return nil
	}
	c, _ := normalize(text)
	flat := string(c)
	var hits []Hit
	for _, id := range m.order {
		s, ok := sum[id]
		if !ok {
			continue
		}
		if rq := m.req[id]; len(rq) > 0 {
			okReq := false
			for _, r := range rq {
				if strings.Contains(flat, r) {
					okReq = true
					break
				}
			}
			if !okReq {
				continue
			}
		}
		hits = append(hits, Hit{ID: id, Score: s})
	}
	sort.SliceStable(hits, func(i, j int) bool { return hits[i].Score > hits[j].Score })
	return hits
}

// Judge 는 변환 없이 한 문장을 판정한다 (§6-5-4).
func (m *Matcher) Judge(text string) Verdict {
	hits := m.Score(text)
	v := Verdict{Input: text, Mode: ModeNoMatch}
	if len(hits) > 3 {
		v.Top = slices.Clone(hits[:3])
	} else {
		v.Top = hits
	}
	th := m.d.KB.Thresholds
	if len(hits) == 0 || hits[0].Score < th.MinScore {
		return v
	}
	s1 := hits[0].Score
	if len(hits) > 1 && hits[1].Score/s1 >= th.AmbigRatio {
		v.Mode = ModeCandidates
		for _, h := range hits {
			if len(v.Cands) == 3 || h.Score < s1*th.CandRatio {
				break
			}
			v.Cands = append(v.Cands, h)
		}
		return v
	}
	v.Mode = ModeAnswer
	return v
}

func modeRank(mode string) int {
	switch mode {
	case ModeAnswer:
		return 2
	case ModeCandidates:
		return 1
	}
	return 0
}

func (v Verdict) top1() float64 {
	if len(v.Top) == 0 {
		return 0
	}
	return v.Top[0].Score
}

// Query 는 입력 한 줄을 판정한다: `/h ` 강제 변환, 아니면 자동 감지 (§6-4).
func (m *Matcher) Query(input string) Verdict {
	input = strings.TrimSpace(input)
	if input == "/h" || strings.HasPrefix(input, "/h ") || strings.HasPrefix(input, "/h\t") {
		conv := ConvertTokens(strings.TrimSpace(strings.TrimPrefix(input, "/h")), m.protected)
		v := m.Judge(conv)
		v.Input, v.Converted = input, conv
		return v
	}
	v := m.Judge(input)
	v.Input = input
	if v.Mode == ModeAnswer {
		return v
	}
	conv, changed := AutoConvert(input, m.protected)
	if !changed {
		return v
	}
	w := m.Judge(conv)
	if modeRank(w.Mode) > modeRank(v.Mode) || (modeRank(w.Mode) == modeRank(v.Mode) && w.top1() > v.top1()) {
		w.Input, w.Converted, w.Auto = input, conv, true
		return w
	}
	return v
}
