package main

// resultline.go — 화면(색상 줄·TUI)·CSV 가 함께 쓰는 결과 줄 1개와 태그·그룹 규칙 (계획서_v0.2_TUI.md).
// 프로파일 점검(hostCheck)과 전체비교(biosSnap 차이)를 모두 이 형식으로 바꾼 뒤 출력한다.

import (
	"sort"
	"strings"
)

// 화면·CSV 태그 3종 (계획서 "출력 줄 형식")
const (
	TagOK   = "OK"
	TagDiff = "difference"
	TagNA   = "N/A"
)

// 결과 줄 종류
const (
	KindProfile = "profile" // 프로파일에 등록된 항목 점검
	KindCompare = "compare" // 비교서버(또는 refs JSON) 대비 BIOS 전체 비교
)

// naValue 는 값이 없음(결과의 "-" 또는 빈 값)을 화면·CSV 에 보여 주는 글자입니다.
const naValue = "(N/A)"

// noneValue 는 기준(비교서버)에 속성 자체가 없음을 뜻합니다.
const noneValue = "(없음)"

// ResultLine 은 호스트 1대 × 속성 1개의 결과입니다. 호스트 단위 오류는 Attr 가 "-" 인 줄 1개입니다.
type ResultLine struct {
	Kind        string // KindProfile / KindCompare
	Tag         string // TagOK / TagDiff / TagNA
	Host        string
	IP          string
	Vendor      string
	Model       string
	BiosVersion string
	MgmtModel   string // BMC 관리 컨트롤러 모델 (예: "iLO 5") — TUI 탭·CSV 그룹 기준. 모르면 "-"
	MgmtVersion string // result.tsv mgmt_version 과 같은 값 (예: "iLO 5 2.99")
	Attr        string
	Expected    string // 기준값 (프로파일 기대값 원문 또는 비교서버 값)
	Current     string // 대상 현재값
	Note        string // 원래 상태명 (FAIL, UNVERIFIED, ONLY_REF, UNREACHABLE ...)
	HostError   bool   // 호스트 단위 오류 줄 (접속·조회 실패)
	RefHost     string // 비교한 기준 호스트 hostname (전체비교만. 기준 자신이면 자기 자신, 프로파일 점검은 빈 값)
	RefIP       string // 기준 호스트 IP
}

// tagFor 는 원래 상태명을 태그 3종으로 묶습니다. 모르는 상태(호스트 오류 등)는 N/A.
func tagFor(status string) string {
	switch status {
	case StatusOK, StatusPendingOK, StatusSame:
		return TagOK
	case StatusFail, StatusUnverified, StatusPendingExists, StatusPendingNoJob, attrOnlyHost, attrDiff:
		return TagDiff
	default: // StatusMappingMissing, StatusNoDump, attrOnlyRef, 접속·조회 실패
		return TagNA
	}
}

// showValue 는 결과의 "-"·빈 값을 (N/A) 로 바꿉니다. 프로파일 파일 양식은 건드리지 않고 표시만 바꾼다.
func showValue(v string) string {
	if v == "" || v == "-" {
		return naValue
	}
	return v
}

// LineGroup 은 TUI 탭 1개(= CSV wide 파일 1쌍)입니다: 같은 벤더 안에서 관리 컨트롤러 모델별로 나누되
// BIOS 속성 이름 목록이 같으면 하나로 합친다.
type LineGroup struct {
	Vendor string
	Name   string   // 관리 컨트롤러 모델 이름. 합쳐졌으면 "iLO 4+iLO 5" 처럼 + 로 잇는다
	Mgmt   []string // 합쳐진 관리 컨트롤러 모델들
	Hosts  []string // 입력 순서
	Lines  []ResultLine
}

// VendorGroups 는 한 벤더의 그룹들입니다.
type VendorGroups struct {
	Vendor string
	Groups []LineGroup
}

// groupLines 는 줄을 벤더 → 관리 컨트롤러 그룹으로 나눕니다. 벤더·그룹 순서는 처음 나온 순서이고,
// 같은 벤더 안에서 속성 이름 집합(호스트 오류 줄 제외)이 같은 관리 컨트롤러 모델끼리는 한 그룹으로 합친다.
func groupLines(lines []ResultLine) []VendorGroups {
	type mg struct {
		mgmt  string
		attrs map[string]bool
		lines []ResultLine
		hosts []string
		seen  map[string]bool
	}
	var vorder []string
	byV := map[string][]*mg{}
	for _, l := range lines {
		v := l.Vendor
		if v == "" {
			v = "-"
		}
		m := l.MgmtModel
		if m == "" {
			m = "-"
		}
		if _, ok := byV[v]; !ok {
			vorder = append(vorder, v)
		}
		var g *mg
		for _, x := range byV[v] {
			if x.mgmt == m {
				g = x
				break
			}
		}
		if g == nil {
			g = &mg{mgmt: m, attrs: map[string]bool{}, seen: map[string]bool{}}
			byV[v] = append(byV[v], g)
		}
		g.lines = append(g.lines, l)
		if !g.seen[l.Host] {
			g.seen[l.Host] = true
			g.hosts = append(g.hosts, l.Host)
		}
		if !l.HostError && l.Attr != "" && l.Attr != "-" {
			g.attrs[l.Attr] = true
		}
	}
	attrKey := func(s map[string]bool) string {
		k := make([]string, 0, len(s))
		for a := range s {
			k = append(k, a)
		}
		sort.Strings(k)
		return strings.Join(k, "\x00")
	}
	var out []VendorGroups
	for _, v := range vorder {
		vg := VendorGroups{Vendor: v}
		idx := map[string]int{} // 속성 집합 키 → vg.Groups 위치
		for _, g := range byV[v] {
			k := attrKey(g.attrs)
			if len(g.attrs) == 0 { // 오류 호스트만 있는 관리 컨트롤러는 따로 둔다
				k = "\x01" + g.mgmt
			}
			if i, ok := idx[k]; ok {
				lg := &vg.Groups[i]
				lg.Mgmt = append(lg.Mgmt, g.mgmt)
				lg.Name = strings.Join(lg.Mgmt, "+")
				lg.Hosts = append(lg.Hosts, g.hosts...)
				lg.Lines = append(lg.Lines, g.lines...)
				continue
			}
			idx[k] = len(vg.Groups)
			vg.Groups = append(vg.Groups, LineGroup{Vendor: v, Name: g.mgmt, Mgmt: []string{g.mgmt}, Hosts: g.hosts, Lines: g.lines})
		}
		out = append(out, vg)
	}
	return out
}
