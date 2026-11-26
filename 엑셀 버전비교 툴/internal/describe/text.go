package describe

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf16"
	"unicode/utf8"

	"excel-diff/internal/diff"
	"excel-diff/internal/reader"
)

const (
	emptyText   = "(빈 값)"
	unknownText = "값 확인 불가"
	quoteMax    = 100 // 설명에 그대로 인용하는 최대 글자 수. 전체 값은 이전값·최신값에 있다
)

// note 는 값 변화의 종류와, 화면 표시만으로는 구별되지 않는지를 담는다.
type note struct {
	label  string // "공백만 다름" 등, 없으면 ""
	spaces bool   // 공백을 ␣ 로 보여야 함
	oldKey bool   // 이전값에 실제값을 병기
	newKey bool
}

// values 는 이전값·최신값 칸의 문자열이다.
func values(ch diff.CellChange) (before, after string, n note) {
	o, c := ch.Old, ch.New
	if ch.Kind == diff.CellModified {
		n.label, n.spaces = difference(o.Key, c.Key)
		same := visible(o.Display, n.spaces) == visible(c.Display, n.spaces)
		n.oldKey, n.newKey = same, same
	}
	// 표시 문자열이 비어 있으면(표시형식으로 가림) 실제값을 보여야 한다.
	n.oldKey = n.oldKey || o != nil && !o.Unknown && o.Display == ""
	n.newKey = n.newKey || c != nil && !c.Unknown && c.Display == ""
	value := func(cell *reader.Cell, key bool) string {
		switch {
		case cell == nil:
			return ""
		case key:
			return cell.Display + " (실제값 " + cell.Key + ")"
		}
		return cell.Display
	}
	return value(o, n.oldKey), value(c, n.newKey), n
}

// phrase 는 설명의 "'전' → '후' [종류]" 부분이다.
func phrase(ch diff.CellChange, n note) string {
	o, c := ch.Old, ch.New
	if o != nil && o.Unknown && c != nil && c.Unknown {
		return unknownText + "(전후 비교 불가)"
	}
	diffAt := 0 // 처음 달라지는 글자 위치
	if o != nil && c != nil {
		x, y := o.Display, c.Display
		if x == y {
			x, y = o.Key, c.Key
		}
		for _, r := range x {
			s, size := utf8.DecodeRuneInString(y)
			if size == 0 || r != s {
				break
			}
			y = y[size:]
			diffAt++
		}
	}
	quote := func(cell *reader.Cell, key bool) string {
		switch {
		case cell == nil:
			return emptyText
		case cell.Unknown:
			return unknownText
		}
		out := "'" + visible(window(cell.Display, diffAt), n.spaces) + "'" + total(cell.Display)
		if key {
			out += "(실제값 " + visible(window(cell.Key, diffAt), n.spaces) + total(cell.Key) + ")"
		}
		return out
	}
	out := quote(o, n.oldKey) + " → " + quote(c, n.newKey)
	if n.label != "" {
		out += " [" + n.label + "]"
	}
	return out
}

// listed 는 추가·삭제 행·열의 값 목록에 쓰는 표기이다.
func listed(c *reader.Cell) string {
	switch {
	case c.Unknown:
		return unknownText
	case c.Display == "":
		return "'' (실제값 " + c.Key + ")"
	}
	return "'" + c.Display + "'"
}

func dropSpace(r rune) rune {
	if unicode.IsSpace(r) {
		return -1
	}
	return r
}

func onlyBreaks(r rune) rune {
	if r == '\n' || r == '\r' {
		return r
	}
	return -1
}

func dropBreaks(r rune) rune {
	if r == '\n' || r == '\r' {
		return -1
	}
	return r
}

// difference 는 두 값이 공백·줄바꿈·대소문자만 다른지 알려준다.
func difference(a, b string) (label string, spaces bool) {
	if strings.EqualFold(a, b) {
		return "대소문자만 다름", false
	}
	x, y := strings.Map(dropSpace, a), strings.Map(dropSpace, b)
	if !strings.EqualFold(x, y) {
		return "", false
	}
	var kinds []string
	if x != y {
		kinds = append(kinds, "대소문자")
	}
	if !strings.EqualFold(strings.Map(dropBreaks, a), strings.Map(dropBreaks, b)) {
		kinds = append(kinds, "공백")
	}
	if strings.Map(onlyBreaks, a) != strings.Map(onlyBreaks, b) {
		kinds = append(kinds, "줄바꿈")
	}
	return strings.Join(kinds, "·") + "만 다름", true
}

func invisible(r rune, spaces bool) bool {
	if r == ' ' {
		return spaces
	}
	return unicode.IsSpace(r) || unicode.IsControl(r) || unicode.Is(unicode.Cf, r)
}

// visible 은 보이지 않는 문자를 기호로 바꾼다. spaces 면 일반 공백도 ␣ 로 바꾼다.
func visible(s string, spaces bool) string {
	if strings.IndexFunc(s, func(r rune) bool { return invisible(r, spaces) }) < 0 {
		return s
	}
	var b strings.Builder
	for _, r := range s {
		switch {
		case !invisible(r, spaces):
			b.WriteRune(r)
		case r == ' ':
			b.WriteRune('␣')
		case r == '\n':
			b.WriteRune('↵')
		case r == '\r':
			b.WriteRune('␍')
		case r == '\t':
			b.WriteRune('⇥')
		default:
			fmt.Fprintf(&b, "⟨U+%04X⟩", r)
		}
	}
	return b.String()
}

// window 는 긴 값에서 처음 달라지는 글자(at) 주변만 남긴다.
func window(s string, at int) string {
	if len(s) <= quoteMax || utf8.RuneCountInString(s) <= quoteMax {
		return s
	}
	r := []rune(s)
	from := max(0, min(at-quoteMax/3, len(r)-quoteMax))
	out := string(r[from : from+quoteMax])
	if from > 0 {
		out = "…" + out
	}
	if from+quoteMax < len(r) {
		out += "…"
	}
	return out
}

// total 은 window 로 줄인 값의 전체 길이 표기이다.
func total(s string) string {
	if n := utf8.RuneCountInString(s); n > quoteMax {
		return fmt.Sprintf("(총 %d자)", n)
	}
	return ""
}

// short 는 머리글·행 이름표를 n 글자로 줄인다.
func short(s string, n int) string {
	if utf8.RuneCountInString(s) > n {
		s = string([]rune(s)[:n]) + "…"
	}
	return visible(s, false)
}

// length 는 Excel 이 세는 글자 수(UTF-16 단위)이다.
func length(s string) int {
	n := 0
	for _, r := range s {
		n += utf16.RuneLen(r)
	}
	return n
}

func fits(s string) bool { return len(s) <= Limit || length(s) <= Limit }

// split 은 s 를 limit 이하 조각으로 나눈다. 이어 붙이면 s 가 된다.
// 조각의 뒤쪽 절반에 줄바꿈이나 ", " 가 있으면 그 뒤에서 자른다.
func split(s string, limit int) []string {
	var out []string
	for s != "" {
		end, n := len(s), 0
		for i, r := range s {
			if n += utf16.RuneLen(r); n > limit {
				end = i
				break
			}
		}
		if end < len(s) {
			if k := strings.LastIndexByte(s[:end], '\n'); k >= end/2 {
				end = k + 1
			} else if k := strings.LastIndex(s[:end], ", "); k >= end/2 {
				end = k + 2
			}
		}
		out = append(out, s[:end])
		s = s[end:]
	}
	return out
}
