package reader

import (
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// numberKey 는 숫자의 정규 표현(유효숫자 15자리)을 돌려준다.
// 텍스트는 그대로 Key 가 되므로 이 표현과 정확히 같은 텍스트만 숫자와 동일해진다.
func numberKey(v float64) string {
	r, err := strconv.ParseFloat(strconv.FormatFloat(v, 'e', 14, 64), 64)
	if err != nil { // 반올림하면 범위를 넘는 최댓값 근처
		r = v
	}
	if r == 0 {
		return "0" // -0 포함
	}
	return strconv.FormatFloat(r, 'f', -1, 64)
}

// rawNumberKey 는 숫자 셀의 저장 문자열을 정규화한다. 숫자가 아니면 그대로 둔다.
func rawNumberKey(s string) string {
	v, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil || math.IsNaN(v) || math.IsInf(v, 0) {
		return s
	}
	return numberKey(v)
}

// calcKey 는 계산 결과를 정규화한다. 결과에는 형식 정보가 없으므로
// excelize 의 숫자 출력 형태일 때만 숫자로 본다("007" 같은 텍스트는 유지).
func calcKey(s string) string {
	v, err := strconv.ParseFloat(s, 64)
	if err != nil || (s != strconv.FormatFloat(v, 'f', -1, 64) && !strings.Contains(s, "E")) {
		return s
	}
	return rawNumberKey(s)
}

// isoDateKey 는 날짜 형식(t="d") 셀의 ISO 8601 문자열을 serial 로 바꾼다.
func isoDateKey(s string, date1904 bool) string {
	epoch := time.Date(1899, 12, 30, 0, 0, 0, 0, time.UTC)
	if date1904 {
		epoch = time.Date(1904, 1, 1, 0, 0, 0, 0, time.UTC)
	}
	for _, layout := range []string{time.RFC3339Nano, "2006-01-02T15:04:05.999999999", "2006-01-02"} {
		if t, err := time.Parse(layout, s); err == nil {
			return numberKey(float64(t.UnixMilli()-epoch.UnixMilli()) / 86400000)
		}
	}
	return s
}

var escapeExp = regexp.MustCompile(`_x[0-9a-fA-F]{4}_`)

// unescape 는 OOXML 의 _xHHHH_ 표기를 원래 문자로 바꾼다.
func unescape(s string) string {
	if !strings.Contains(s, "_x") {
		return s
	}
	return escapeExp.ReplaceAllStringFunc(s, func(m string) string {
		n, _ := strconv.ParseUint(m[2:6], 16, 32)
		return string(rune(n))
	})
}
