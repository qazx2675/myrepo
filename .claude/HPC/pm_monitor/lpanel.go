// lpanel.go - `l` 패널(서버 설정 표, 계획서 4.9) 렌더. 스냅샷의 Record.Check·Cmp 로 그린다(포그라운드·데몬 화면 같음).
// 8000대에서도 가볍게: prepare 는 줄 목록(인덱스)과 칸 폭만 계산하고, 줄 문자열은 보이는 줄만 만든다.
package main

import (
	"fmt"
	"strings"
)

// lFiltered 는 `m` 필터(oklist 와 다른 서버만)를 통과하는지 알려준다.
func lFiltered(m uint8) bool { return m&cmpOn != 0 && m&cmpDiffMask != 0 }

// prepareLDAP 는 `l` 패널의 머리 줄(안내·oklist 집계·필터 안내·열 제목)과 표 줄 목록·칸 폭을 정한다.
func (v *ViewState) prepareLDAP(snap Snapshot, w int) {
	var head []string
	if t := v.Reports[panelLDAP]; t != "" {
		head = append(head, wrapText(t, w-1)...)
	}
	if snap.Cfg.HasOK {
		s := fmt.Sprintf(" oklist %d대 · 조사 %d대 · 다름 %d대", snap.Cfg.OK, snap.Cfg.Checked, snap.Cfg.Diff)
		if snap.Cfg.Warn > 0 {
			s += fmt.Sprintf(" · 경고 %d줄", snap.Cfg.Warn)
		}
		head = append(head, s)
		if v.OnlyDiff {
			head = append(head, " oklist 와 다른 서버만 보기 (m 다시 누르면 전체)")
		}
	}
	order := snap.hostOrder()
	rows := make([]int, 0, len(order))
	var wd [8]int
	for j, h := range ldapHeader {
		wd[j] = strWidth(h)
	}
	for _, k := range order {
		if v.OnlyDiff && !lFiltered(snap.cmpOf(k)) {
			continue
		}
		rows = append(rows, k)
		c, _ := lCells(snap, k)
		for j := 0; j < 7; j++ { // 마지막 칸(ldap_check결과)은 채우지 않으므로 폭이 필요 없다
			if n := strWidth(cleanText(c[j])); n > wd[j] {
				wd[j] = n
			}
		}
	}
	v.lW, v.lRows = wd, rows
	head = append(head, v.lLine(ldapHeader, 0, w))
	if len(rows) == 0 {
		if v.OnlyDiff {
			head = append(head, " (oklist 와 다른 서버가 없습니다)")
		} else {
			head = append(head, " (서버가 없습니다)")
		}
	}
	v.pLines = head
}

// panelLen 은 열린 패널의 전체 줄 수다(l 패널은 머리 줄 + 표 줄).
func (v *ViewState) panelLen() int {
	if v.Panel == panelLDAP {
		return len(v.pLines) + len(v.lRows)
	}
	return len(v.pLines)
}

// panelLine 은 열린 패널의 i 번째 줄(폭 w 에 맞춘 것)이다.
func (v *ViewState) panelLine(snap Snapshot, i, w int) string {
	if i < len(v.pLines) {
		return truncW(v.pLines[i], w)
	}
	c, m := lCells(snap, v.lRows[i-len(v.pLines)])
	return v.lLine(c, m, w)
}

// lCellMax 는 화면에서 설정 칸(OS커널~h/t) 하나의 최대 폭이다. 긴 binddn 등이 뒤 칸(h/t)을 화면 밖으로 밀지 않게
// 넘으면 … 로 줄인다(전체 값은 res 파일·state.json 에 있다).
const lCellMax = 24

// lCells 는 Records[k] 의 표 칸과 비교 비트다. oklist 와 다른데 조사값이 빈 칸은 보이도록 (없음)으로 쓴다.
func lCells(snap Snapshot, k int) ([8]string, uint8) {
	c, m := ldapRowCells(snap.Records[k]), snap.cmpOf(k)
	for j := 1; j <= cfgN; j++ {
		if c[j] == "" && m&cmpOn != 0 && m&(1<<(j-1)) != 0 {
			c[j] = "(없음)"
		}
		c[j] = truncW(cleanText(c[j]), lCellMax)
	}
	return c, m
}

// lLine 은 표 한 줄이다: 한 칸 들여 쓰고 칸 사이 공백 두 칸, 마지막 칸은 채우지 않는다.
// m 에 cmpOn 이 있으면 oklist 와 다른 칸(OS커널~h/t)을 빨간색으로 칠한다. 폭 w 를 넘으면 … 로 자른다(색 코드는 폭에 넣지 않음).
func (v *ViewState) lLine(cells [8]string, m uint8, w int) string {
	var sb strings.Builder
	sb.WriteByte(' ')
	used := 1
	pad := 0 // 앞 칸 뒤에 아직 쓰지 않은 공백(줄 끝 공백을 남기지 않으려고 미룬다)
	for j, c := range cells {
		c = cleanText(c)
		cw := strWidth(c)
		if c != "" {
			if used+pad+cw > w { // 이 칸에서 넘친다: 남은 폭에 맞춰 자르고 끝
				if left := w - used - pad; left > 0 {
					sb.WriteString(strings.Repeat(" ", pad))
					t := truncW(c, left)
					sb.WriteString(v.cellPaint(j, m, t))
				}
				return sb.String()
			}
			sb.WriteString(strings.Repeat(" ", pad))
			sb.WriteString(v.cellPaint(j, m, c))
			used += pad + cw
			pad = 0
		}
		if j < len(cells)-1 {
			pad += v.lW[j] - cw + 2
		}
	}
	return sb.String()
}

// cellPaint 는 j 번째 칸(1~6 이 비교 칸)이 oklist 와 다르면 빨간색으로 칠한다.
func (v *ViewState) cellPaint(j int, m uint8, t string) string {
	if m&cmpOn != 0 && j >= 1 && j <= cfgN && m&(1<<(j-1)) != 0 {
		return paint(v.Color, stRed, t)
	}
	return t
}
