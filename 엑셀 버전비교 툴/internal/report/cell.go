package report

import (
	"fmt"
	"strconv"
	"strings"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/xuri/excelize/v2"

	"excel-diff/internal/describe"
)

// Excel 한도. 시험에서 분할을 확인하려고 낮출 수 있게 변수로 둔다.
var (
	maxRows  = excelize.TotalRows
	maxCols  = excelize.MaxColumns
	maxLinks = excelize.TotalSheetHyperlinks // 시트당
)

const (
	cutMark    = "…(잘림)"
	cutListMax = 20 // 요약에 위치를 적는 축약 셀 수
)

// xmlSafe 는 XML 에 쓸 수 없는 문자를 "⟨U+0001⟩" 표기로 바꾼다.
func xmlSafe(s string) string {
	bad := func(r rune) bool {
		return r < 0x20 && r != '\t' && r != '\n' && r != '\r' || r == 0xFFFE || r == 0xFFFF
	}
	if utf8.ValidString(s) && strings.IndexFunc(s, bad) < 0 {
		return s
	}
	var b strings.Builder
	for _, r := range s {
		if bad(r) {
			fmt.Fprintf(&b, "⟨U+%04X⟩", r)
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// length 는 Excel 이 세는 글자 수(UTF-16 단위)이다.
func length(s string) int {
	n := 0
	for _, r := range s {
		n += utf16.RuneLen(r)
	}
	return n
}

// head 는 s 앞부분에서 limit(UTF-16 단위) 안에 드는 바이트 수이다.
func head(s string, limit int) int {
	n := 0
	for i, r := range s {
		if n += utf16.RuneLen(r); n > limit {
			return i
		}
	}
	return len(s)
}

// chunks 는 s 를 셀 한도 이하 조각으로 나눈다. 이어 붙이면 s 가 된다.
func chunks(s string) []string {
	var out []string
	for len(s) > describe.Limit {
		k := head(s, describe.Limit)
		out = append(out, s[:k])
		s = s[k:]
	}
	if s != "" || len(out) == 0 {
		out = append(out, s)
	}
	return out
}

// number 는 s 를 숫자 셀로 써도 화면 표시가 같은지 확인한다. width 는 열 너비이다.
// General 형식은 11자를 넘거나 열이 좁으면 다르게 보이므로 그때는 문자열로 둔다.
func number(s string, width float64) (float64, bool) {
	if s == "" || len(s) > 11 || float64(len(s)) > width-1 || s == "-0" || (s[0] != '-' && (s[0] < '0' || s[0] > '9')) {
		return 0, false
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil || strconv.FormatFloat(v, 'f', -1, 64) != s {
		return 0, false
	}
	// "-Inf" 는 위 검사를 통과한다.
	if v-v != 0 {
		return 0, false
	}
	return v, true
}

// fill 은 셀 배경색의 뜻이다.
type fill uint8

const (
	fillNone fill = iota
	fillModified
	fillAdded
	fillDeleted
	fillMoved
	fillUnknown
	fillHeader
	fillSeparator
)

var fillColors = [...]string{"", "FFF2A8", "C6EFCE", "FFC7CE", "BDD7EE", "F4B183", "D9D9D9", "808080"}

type styleKey struct {
	fill                     fill
	hidden, bold, wrap, link bool
}

func (w *writer) style(k styleKey) int {
	if k == (styleKey{}) {
		return 0
	}
	if id, ok := w.styles[k]; ok {
		return id
	}
	s := &excelize.Style{}
	if k.fill != fillNone {
		s.Fill = excelize.Fill{Type: "pattern", Pattern: 1, Color: []string{fillColors[k.fill]}}
	}
	font := excelize.Font{Family: "맑은 고딕", Size: 11, Bold: k.bold, Italic: k.hidden}
	if k.hidden {
		font.Color = "6B6B6B"
	}
	if k.link {
		font.Color, font.Underline = "0563C1", "single"
	}
	if k.bold || k.hidden || k.link {
		s.Font = &font
	}
	if k.wrap {
		s.Alignment = &excelize.Alignment{WrapText: true, Vertical: "top"}
	}
	id, err := w.f.NewStyle(s)
	if err != nil && w.err == nil {
		w.err = err
	}
	w.styles[k] = id
	return id
}

// text 는 문자열 셀이다(수식으로 해석되지 않는다). 한도를 넘으면 줄이고 요약에 알린다.
// 빈 문자열은 서식만 있는 빈 셀이며, 서식도 없으면 nil 이다.
func (w *writer) text(sheet string, col, row int, s string, k styleKey) any {
	id := w.style(k)
	if s == "" {
		if id == 0 {
			return nil
		}
		return excelize.Cell{StyleID: id}
	}
	s = xmlSafe(s)
	if len(s) > describe.Limit && length(s) > describe.Limit {
		s = s[:head(s, describe.Limit-length(cutMark))] + cutMark
		if w.cut++; w.cut <= cutListMax {
			name, _ := excelize.CoordinatesToCellName(col, row)
			w.cutAt = append(w.cutAt, sheet+" "+name)
		}
	}
	return excelize.Cell{StyleID: id, Value: s}
}
