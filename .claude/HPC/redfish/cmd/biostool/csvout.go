package main

// csvout.go — 결과 줄(ResultLine)을 CSV 로 저장한다 (계획서_v0.2_TUI.md "CSV").
// 엑셀에서 바로 열도록 UTF-8 BOM, 파일은 0600. 색이 없으므로 difference 칸은 "[DIFF] " 접두어로 표시한다.

import (
	"encoding/csv"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// diffMark 는 wide_hosts CSV 에서 difference 칸 앞에 붙이는 표시입니다.
const diffMark = "[DIFF] "

// csvLineHeader 는 long.csv 헤더입니다.
var csvLineHeader = []string{"태그", "Hostname", "IP", "Vendor", "Model", "BIOS", "관리컨트롤러", "Attribute", "기준값", "현재값", "비고"}

// csvFileName 은 벤더·그룹 이름을 파일 이름 조각으로 쓸 수 있게 바꿉니다 (허용 글자 밖은 "_").
func csvFileName(s string) string {
	n := unsafeFileChars.ReplaceAllString(s, "_")
	if n == "" {
		n = "-"
	}
	return n
}

// csvSafe 는 엑셀 수식 주입을 막습니다: 칸이 =, +, @, TAB, CR 로 시작하거나 "-" 로 시작하는데 음수(-5, -1.5)가
// 아니면 앞에 ' 를 붙입니다. "-" 하나는 그대로 둡니다. 고정 헤더는 이런 글자로 시작하지 않으므로 바뀌지 않습니다.
func csvSafe(s string) string {
	if s == "" {
		return s
	}
	switch s[0] {
	case '=', '+', '@', '\t', '\r':
		return "'" + s
	case '-':
		if len(s) > 1 && !isPlainNumber(s[1:]) {
			return "'" + s
		}
	}
	return s
}

// isPlainNumber 는 s 가 숫자로 시작하고 숫자와 '.' 만으로 된 글자인지 봅니다 ("-1+1" 같은 식은 아님).
func isPlainNumber(s string) bool {
	if s == "" || s[0] < '0' || s[0] > '9' {
		return false
	}
	for i := 0; i < len(s); i++ {
		if (s[i] < '0' || s[i] > '9') && s[i] != '.' {
			return false
		}
	}
	return true
}

// writeCSVFile 은 BOM + CSV 를 path 에 씁니다 (0600). 모든 칸은 csvSafe 를 거칩니다 (BMC·호스트에서 온 값이
// 헤더에도 들어가므로 — 속성 이름·호스트 이름 열). rows 자체는 바꾸지 않습니다.
func writeCSVFile(path string, rows [][]string) error {
	safe := make([][]string, len(rows))
	for i, r := range rows {
		safe[i] = make([]string, len(r))
		for j, c := range r {
			safe[i][j] = csvSafe(c)
		}
	}
	rows = safe
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	if _, err := f.Write([]byte("\xef\xbb\xbf")); err != nil {
		f.Close()
		return err
	}
	w := csv.NewWriter(f)
	if err := w.WriteAll(rows); err != nil { // WriteAll 이 Flush 까지 한다
		f.Close()
		return err
	}
	return f.Close()
}

// mgmtText 는 CSV 에 쓸 관리 컨트롤러 표기입니다 (버전이 있으면 "iLO 5 2.99", 없으면 모델).
func mgmtText(l ResultLine) string {
	if l.MgmtVersion != "" && l.MgmtVersion != "-" {
		return l.MgmtVersion
	}
	if l.MgmtModel != "" {
		return l.MgmtModel
	}
	return "-"
}

// isAttrLine 은 속성 1개에 대한 줄인지 봅니다 (호스트 단위 오류 줄은 아님).
func isAttrLine(l ResultLine) bool {
	return !l.HostError && l.Attr != "" && l.Attr != "-"
}

// csvCell 은 현재값 칸의 글자입니다. N/A 는 (N/A), mark 가 true 이면 difference 앞에 "[DIFF] " 를 붙입니다.
func csvCell(l ResultLine, mark bool) string {
	if l.Tag == TagNA {
		return naValue
	}
	v := showValue(l.Current)
	if mark && l.Tag == TagDiff {
		return diffMark + v
	}
	return v
}

// hostTally 는 호스트 1대의 집계입니다.
type hostTally struct {
	first ResultLine // 호스트 정보(IP·벤더·모델·BIOS·관리망)를 가져올 첫 줄
	diff  int
	na    int
	diffs []string // "attr: 현재값 / 기준값"
	errNo string   // 호스트 오류 줄의 비고
}

// tallyHosts 는 줄들을 호스트별로 집계합니다 (호스트 순서는 처음 나온 순서).
// 호스트 오류 줄은 N/A 1건으로 센다.
func tallyHosts(lines []ResultLine) ([]string, map[string]*hostTally) {
	var order []string
	m := map[string]*hostTally{}
	for _, l := range lines {
		t := m[l.Host]
		if t == nil {
			t = &hostTally{first: l}
			m[l.Host] = t
			order = append(order, l.Host)
		}
		if l.HostError {
			t.na++
			t.errNo = l.Note
			continue
		}
		switch l.Tag {
		case TagDiff:
			t.diff++
			t.diffs = append(t.diffs, l.Attr+": "+showValue(l.Current)+" / "+showValue(l.Expected))
		case TagNA:
			t.na++
		}
	}
	return order, m
}

// summaryRows 는 summary.csv 의 행들입니다 (헤더 포함).
func summaryRows(lines []ResultLine) [][]string {
	rows := [][]string{{"Hostname", "IP", "Vendor", "Model", "BIOS", "관리컨트롤러", "불일치 건수", "N/A 건수", "불일치 속성"}}
	order, m := tallyHosts(lines)
	for _, h := range order {
		t := m[h]
		detail := strings.Join(t.diffs, ", ")
		switch {
		case t.errNo != "":
			detail = t.errNo
		case t.diff == 0 && t.na == 0:
			detail = "모두 동일"
		}
		f := t.first
		rows = append(rows, []string{h, f.IP, f.Vendor, f.Model, f.BiosVersion, mgmtText(f),
			strconv.Itoa(t.diff), strconv.Itoa(t.na), detail})
	}
	return rows
}

// groupIdx 는 그룹의 줄을 (호스트, 속성) 으로 찾게 하고, 속성 순서(처음 나온 순서)와 속성별 대표 기준값을 담습니다.
type groupIdx struct {
	attrs    []string
	at       map[string]ResultLine // 호스트 + "\x00" + 속성 → 줄
	expected map[string]string     // 속성 → 가장 많이 나온 기준값 (같은 수면 먼저 나온 값)
}

func newGroupIdx(g LineGroup) *groupIdx {
	ix := &groupIdx{at: map[string]ResultLine{}, expected: map[string]string{}}
	seen := map[string]bool{}
	vals := map[string][]string{}      // 속성 → 기준값 후보(나온 순서)
	cnt := map[string]map[string]int{} // 속성 → 기준값 → 횟수
	for _, l := range g.Lines {
		if !isAttrLine(l) {
			continue
		}
		if !seen[l.Attr] {
			seen[l.Attr] = true
			ix.attrs = append(ix.attrs, l.Attr)
			cnt[l.Attr] = map[string]int{}
		}
		k := l.Host + "\x00" + l.Attr
		if _, ok := ix.at[k]; !ok {
			ix.at[k] = l
		}
		v := showValue(l.Expected)
		if cnt[l.Attr][v] == 0 {
			vals[l.Attr] = append(vals[l.Attr], v)
		}
		cnt[l.Attr][v]++
	}
	for a, vs := range vals {
		best := vs[0]
		for _, v := range vs[1:] {
			if cnt[a][v] > cnt[a][best] {
				best = v
			}
		}
		ix.expected[a] = best
	}
	return ix
}

// wideHostsRows 는 wide_hosts CSV 행들입니다: 헤더, "기준" 행, 호스트마다 현재값 행.
func wideHostsRows(g LineGroup) [][]string {
	ix := newGroupIdx(g)
	attrs, at := ix.attrs, ix.at
	_, tally := tallyHosts(g.Lines)
	head := append([]string{"구분", "Hostname", "IP", "Vendor", "Model", "BIOS", "불일치 건수"}, attrs...)
	refHost, refIP := refLabel(g.Lines)
	exp := append([]string{"기준", refHost, refIP, "", "", "", ""}, make([]string, len(attrs))...)
	for i, a := range attrs {
		exp[7+i] = ix.expected[a]
	}
	rows := [][]string{head, exp}
	for _, h := range g.Hosts {
		t := tally[h]
		if t == nil {
			continue
		}
		f := t.first
		row := []string{"대상", h, f.IP, f.Vendor, f.Model, f.BiosVersion, strconv.Itoa(t.diff)}
		for _, a := range attrs {
			if l, ok := at[h+"\x00"+a]; ok {
				row = append(row, csvCell(l, true))
			} else {
				row = append(row, naValue)
			}
		}
		rows = append(rows, row)
	}
	return rows
}

// wideAttrsRows 는 wide_attrs CSV 행들입니다: 헤더, 속성마다 (차이 대수, 기준값, 호스트별 현재값) 행.
func wideAttrsRows(g LineGroup) [][]string {
	ix := newGroupIdx(g)
	attrs, at := ix.attrs, ix.at
	head := append([]string{"Attribute", "차이", "기준값"}, g.Hosts...)
	rows := [][]string{head}
	for _, a := range attrs {
		row := []string{a, "", ix.expected[a]}
		n := 0
		for _, h := range g.Hosts {
			l, ok := at[h+"\x00"+a]
			if !ok {
				row = append(row, naValue)
				continue
			}
			if l.Tag == TagDiff {
				n++
			}
			row = append(row, csvCell(l, false))
		}
		if n > 0 {
			row[1] = strconv.Itoa(n) + "대"
		}
		rows = append(rows, row)
	}
	return rows
}

// longRows 는 long.csv 행들입니다 (줄 하나가 행 하나).
func longRows(lines []ResultLine) [][]string {
	rows := make([][]string, 0, len(lines)+1)
	rows = append(rows, csvLineHeader)
	for _, l := range lines {
		rows = append(rows, []string{l.Tag, l.Host, l.IP, l.Vendor, l.Model, l.BiosVersion, mgmtText(l),
			l.Attr, showValue(l.Expected), showValue(l.Current), l.Note})
	}
	return rows
}

// writeResultCSVs 는 dir 에 <prefix>summary.csv, <prefix>wide_hosts_<벤더>_<그룹>.csv,
// <prefix>wide_attrs_<벤더>_<그룹>.csv, <prefix>long.csv 를 쓰고 쓴 파일 경로를 돌려줍니다.
// prefix 로 프로파일 결과("profile_")와 전체비교 결과("compare_")를 한 폴더에 함께 둘 수 있습니다.
func writeResultCSVs(dir, prefix string, lines []ResultLine) ([]string, error) {
	return writeResultCSVsCancel(dir, prefix, lines, nil)
}

// writeResultCSVsCancel 은 writeResultCSVs 에 취소를 더한 것입니다 (TUI Ctrl+X): cancel 이 닫히면 다음 파일부터 쓰지 않고
// 오류를 돌려줍니다 (이미 쓴 파일은 그대로 둔다). nil 이면 취소 없음.
func writeResultCSVsCancel(dir, prefix string, lines []ResultLine, cancel <-chan struct{}) ([]string, error) {
	var paths []string
	write := func(name string, rows func() [][]string) error {
		if chanClosed(cancel) {
			return fmt.Errorf("CSV 저장 취소 (Ctrl+X) — %s 부터 쓰지 않음", name)
		}
		p := filepath.Join(dir, name)
		if err := writeCSVFile(p, rows()); err != nil {
			return fmt.Errorf("CSV 저장 %s: %w", name, err)
		}
		paths = append(paths, p)
		return nil
	}
	if err := write(prefix+"summary.csv", func() [][]string { return summaryRows(lines) }); err != nil {
		return paths, err
	}
	for _, vg := range groupLines(lines) {
		for _, g := range vg.Groups {
			g := g
			tail := csvFileName(g.Vendor) + "_" + csvFileName(g.Name) + ".csv"
			if err := write(prefix+"wide_hosts_"+tail, func() [][]string { return wideHostsRows(g) }); err != nil {
				return paths, err
			}
			if err := write(prefix+"wide_attrs_"+tail, func() [][]string { return wideAttrsRows(g) }); err != nil {
				return paths, err
			}
		}
	}
	if err := write(prefix+"long.csv", func() [][]string { return longRows(lines) }); err != nil {
		return paths, err
	}
	return paths, nil
}

// refLabel 은 그룹에서 비교에 쓰인 기준 호스트들의 hostname·IP 입니다 (처음 나온 순서, 여러 개면 " / " 로 잇는다).
// 기준 호스트를 알 수 없으면(프로파일 점검 등) 빈 값입니다.
func refLabel(lines []ResultLine) (host, ip string) {
	seen := map[string]bool{}
	var hs, ips []string
	for _, l := range lines {
		if l.RefHost == "" || seen[l.RefHost+"\x00"+l.RefIP] {
			continue
		}
		seen[l.RefHost+"\x00"+l.RefIP] = true
		hs = append(hs, l.RefHost)
		ips = append(ips, l.RefIP)
	}
	return strings.Join(hs, " / "), strings.Join(ips, " / ")
}
