package main

// xmodel.go — 모델이 다른 대상끼리 비교 (TUI 전체 설정 비교 단계에서 사용자가 y 로 고른 때만).
//
// 전체비교(CompareCancel)는 기준과 모델 키가 같은 대상만 비교하고 나머지는 NO_REFERENCE 로 남긴다. 이 파일은 그 결과를
// 바꾸지 않고, 기준(B)과 모델이 다른 대상(A)의 속성을 나란히 놓은 참고용 CSV(compare_xmodel_*.csv)만 따로 만든다.
// 설정 계획·적용(setplan/set)은 이 결과를 읽지 않는다 — 모델이 다르면 같은 이름의 속성도 뜻·값이 다를 수 있다.
//
//	xmodelPlanFor(cmp, snaps)                     물을지 판단 + 안내 문구용 대수 (기준이 1대·읽힘·모델이 다른 대상이 있을 때만)
//	buildXModel(b, as, ign, prot)                 A 모델 1종 ↔ B 비교 (속성 합집합, 제외 속성 뺌, 속성별 판정)
//	(*xmodelCmp).Rows(bHost)                      CSV 행 (헤더 1줄 + 속성별 1줄)

import (
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// xmodel 판정 값 중 모델 이름이 붙지 않는 것.
const (
	xmSame = "같음"
	xmDiff = "다름"
)

// xmodelCmp 는 A 모델 1종과 기준 B 1대의 비교입니다.
type xmodelCmp struct {
	B     *biosSnap
	A     []*biosSnap // 같은 모델 키의 읽힌 대상 (입력 순서)
	AName string      // 짧은 모델 이름 (B 와 같으면 벤더 포함)
	BName string
	Attrs []xmodelAttr
	// 판정별 속성 수. Excluded 는 제외(무시·보호) 속성 수 (합집합 기준).
	Same, Diff, OnlyA, OnlyB, Excluded int
}

type xmodelAttr struct {
	Name    string
	Verdict string
	B       string // 없으면 "-"
	A       []string
}

// xmodelNames 는 두 모델의 짧은 이름입니다 (Model 앞뒤 공백 제거). 같으면 벤더를 붙인 label() 로 바꿔 구분한다.
func xmodelNames(b, a *biosSnap) (bn, an string) {
	bn, an = strings.TrimSpace(b.Model), strings.TrimSpace(a.Model)
	if bn == an || bn == "" || an == "" {
		bn, an = b.label(), a.label()
	}
	return bn, an
}

// buildXModel 은 as(모두 같은 모델, b 와는 다른 모델)를 b 와 비교합니다. ign·prot 에 걸린 속성은 Excluded 로만 센다.
func buildXModel(b *biosSnap, as []*biosSnap, ign *ignoreSet, prot *protectSet) *xmodelCmp {
	c := &xmodelCmp{B: b, A: as}
	c.BName, c.AName = xmodelNames(b, as[0])
	seen := map[string]bool{}
	var names []string
	add := func(m map[string]string) {
		for n := range m {
			if !seen[n] {
				seen[n] = true
				names = append(names, n)
			}
		}
	}
	add(b.Attrs)
	for _, a := range as {
		add(a.Attrs)
	}
	sort.Strings(names)
	for _, n := range names {
		if ign.match(n) || prot.match(n) != "" {
			c.Excluded++
			continue
		}
		bv, bok := b.Attrs[n]
		at := xmodelAttr{Name: n, B: "-", A: make([]string, len(as))}
		if bok {
			at.B = bv
		}
		anyA, allSame := false, true
		for i, a := range as {
			av, aok := a.Attrs[n]
			at.A[i] = "-"
			if !aok {
				allSame = false
				continue
			}
			at.A[i] = av
			anyA = true
			if !bok || av != bv {
				allSame = false
			}
		}
		switch {
		case bok && !anyA:
			at.Verdict = c.BName + "에만"
			c.OnlyB++
		case !bok:
			at.Verdict = c.AName + "에만"
			c.OnlyA++
		case allSame:
			at.Verdict = xmSame
			c.Same++
		default:
			at.Verdict = xmDiff
			c.Diff++
		}
		c.Attrs = append(c.Attrs, at)
	}
	return c
}

// hostCol 은 CSV 열 머리글에 쓰는 대상 이름입니다 (관리 IP, 없으면 hostname).
func hostCol(t Target) string {
	if t.IP != "" {
		return t.IP
	}
	return t.Hostname
}

// Rows 는 CSV 행입니다: Attribute | 판정 | <B모델> 비교서버(<bHost>) | <A 호스트 IP>...
func (c *xmodelCmp) Rows(bHost string) [][]string {
	head := []string{"Attribute", "판정", c.BName + " 비교서버(" + bHost + ")"}
	for _, a := range c.A {
		head = append(head, hostCol(a.Target))
	}
	rows := [][]string{head}
	for _, at := range c.Attrs {
		rows = append(rows, append([]string{at.Name, at.Verdict, at.B}, at.A...))
	}
	return rows
}

// Summary 는 TUI 안내용 요약입니다: "같음 120 · 다름 8 · X에만 5 · Y에만 12 (제외 30)".
func (c *xmodelCmp) Summary() string {
	return fmt.Sprintf("%s %d · %s %d · %s에만 %d · %s에만 %d (제외 %d)",
		xmSame, c.Same, xmDiff, c.Diff, c.AName, c.OnlyA, c.BName, c.OnlyB, c.Excluded)
}

var xmodelSafe = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

// FileName 은 compare_xmodel_<A 모델>_vs_<B 모델>.csv 입니다 (영숫자 . _ - 만 남김).
func (c *xmodelCmp) FileName() string {
	s := func(v string) string { return strings.Trim(xmodelSafe.ReplaceAllString(v, "_"), "_") }
	return "compare_xmodel_" + s(c.AName) + "_vs_" + s(c.BName) + ".csv"
}

// xmodelPlan 은 모델이 다른 대상 비교를 물을 때의 재료입니다.
type xmodelPlan struct {
	B      *biosSnap
	BHost  string        // 기준 이름 (비교서버 hostname 또는 JSON 파일 이름)
	Groups [][]*biosSnap // 모델 키별 (다른 모델 대상, 처음 나온 순서)
	NA     int           // 다른 모델 대상 수
	NB     int           // 기준 1 + 같은 모델 대상 수
}

// xmodelPlanFor 는 기준이 1대(비교서버 1대 또는 JSON)이고 읽혔으며 모델이 다른 읽힌 대상이 있을 때만 계획을 돌려줍니다 (아니면 nil).
// diff.txt·여러 대 기준은 모델마다 이미 기준이 따로 있으므로 묻지 않는다.
func xmodelPlanFor(cmp *compareResult, snaps []*biosSnap) *xmodelPlan {
	if cmp == nil || cmp.Ref.DiffFile != "" || len(cmp.Refs) != 1 || cmp.Refs[0].Snap == nil || !cmp.Refs[0].Snap.ok() {
		return nil
	}
	if cmp.Ref.JSONPath == "" && len(cmp.Ref.Hosts) == 0 {
		return nil
	}
	r := cmp.Refs[0]
	p := &xmodelPlan{B: r.Snap, BHost: r.Target.Hostname, NB: 1}
	if cmp.Ref.JSONPath != "" {
		p.BHost = filepath.Base(cmp.Ref.JSONPath)
	}
	idx := map[string]int{}
	for _, s := range snaps {
		if !s.ok() {
			continue
		}
		switch {
		case s.Key == p.B.Key:
			if targetID(s.Target) != r.ID { // 기준 자신이 대상에도 있으면 한 번만
				p.NB++
			}
		default:
			i, ok := idx[s.Key]
			if !ok {
				i = len(p.Groups)
				idx[s.Key] = i
				p.Groups = append(p.Groups, nil)
			}
			p.Groups[i] = append(p.Groups[i], s)
			p.NA++
		}
	}
	if p.NA == 0 {
		return nil
	}
	return p
}

// Question 은 y/n 질문 문구입니다.
func (p *xmodelPlan) Question() string {
	var as []string
	for _, g := range p.Groups {
		as = append(as, fmt.Sprintf("%s %d대", g[0].label(), len(g)))
	}
	return fmt.Sprintf("모델이 다른 대상이 있습니다 (%s, %s %d대). 모델이 달라도 비교할까요? (y/N)",
		strings.Join(as, ", "), p.B.label(), p.NB)
}
