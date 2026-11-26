package report

import (
	"fmt"
	"strings"
	"unicode"

	"github.com/xuri/excelize/v2"
)

const (
	summaryName = "요약"
	logName     = "변경내역"
)

// namer 는 겹치지 않는 시트 이름을 만든다. Excel 은 대소문자를 구별하지 않는다.
type namer map[string]bool

func newNamer(reserved ...string) namer {
	n := namer{}
	for _, s := range reserved {
		n[fold(s)] = true
	}
	return n
}

func fold(s string) string { return strings.ToLower(strings.ToUpper(s)) }

// name 은 base+suffix 를 31자 이하의 쓸 수 있는 이름으로 만든다. 접미사는 줄이지 않는다.
// 이름이 겹치면 접미사 앞에 "~2" 부터 번호를 넣는다.
func (n namer) name(base, suffix string) string {
	base = strings.Map(func(r rune) rune {
		if strings.ContainsRune(":\\/?*[]", r) || unicode.IsControl(r) || r == 0xFFFE || r == 0xFFFF {
			return '_'
		}
		return r
	}, base)
	if strings.HasPrefix(base, "'") {
		base = "_" + base[1:]
	}
	for i := 1; ; i++ {
		tail := suffix
		if i > 1 {
			tail = fmt.Sprintf("~%d%s", i, suffix)
		}
		s := base[:head(base, excelize.MaxSheetNameLength-length(tail))] + tail
		if strings.HasSuffix(s, "'") {
			s = s[:len(s)-1] + "_"
		}
		if !n[fold(s)] {
			n[fold(s)] = true
			return s
		}
	}
}
