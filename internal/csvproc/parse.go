package csvproc

import (
	"regexp"
	"strings"
)

var reRackToken = regexp.MustCompile(`(?i)^X\d+Y\d+$`)

// ParseFlexibleCsvOrTsv 는 JS parseFlexibleCsvOrTsv 와 동일하게 헤더 유무/탭·쉼표를 자동 감지해 Row 로 변환한다.
// (EUC-KR 디코딩은 외부 의존성 없이 지원하지 않는다. UTF-8 만 지원 — GO_MIGRATION_PROGRESS.md 메모 참고)
func ParseFlexibleCsvOrTsv(text string) []Row {
	t := strings.TrimSpace(text)
	t = strings.TrimLeft(t, "\uFEFF\u00A0")
	if t == "" {
		return nil
	}
	var lines []string
	for _, l := range strings.Split(strings.ReplaceAll(t, "\r\n", "\n"), "\n") {
		if strings.TrimSpace(l) != "" {
			lines = append(lines, strings.TrimRight(l, "\r"))
		}
	}
	if len(lines) == 0 {
		return nil
	}
	delim := ","
	if strings.Contains(lines[0], "\t") {
		delim = "\t"
	}
	first := strings.Split(lines[0], delim)
	for i := range first {
		first[i] = strings.TrimSpace(first[i])
	}
	tok := func(i int) string {
		if i < len(first) {
			return first[i]
		}
		return ""
	}

	maxCols := len(first)
	for _, l := range lines {
		if c := strings.Count(l, delim) + 1; c > maxCols {
			maxCols = c
		}
	}

	var headers []string
	dataStart := 1
	switch {
	case reRackToken.MatchString(tok(2)):
		headers = []string{"센터이름", "모델코드", "랙라벨", "hostname", "몇U", "수동지정U"}[:minInt(6, maxInt(5, maxCols))]
		dataStart = 0
	case reRackToken.MatchString(tok(1)):
		headers = []string{"모델코드", "랙라벨", "hostname", "몇U", "수동지정U"}[:minInt(5, maxInt(4, maxCols))]
		dataStart = 0
	default:
		for i, h := range first {
			h = strings.TrimLeft(strings.TrimSpace(h), "\uFEFF\u00A0")
			if h == "" && i >= 4 {
				h = "수동지정U"
			}
			headers = append(headers, h)
		}
	}

	rows := make([]Row, 0, len(lines))
	for _, line := range lines[dataStart:] {
		parts := strings.Split(line, delim)
		r := Row{Keys: headers, V: make(map[string]string, len(headers))}
		for i, h := range headers {
			if i < len(parts) {
				r.V[h] = strings.Trim(strings.TrimSpace(parts[i]), `"`)
			} else {
				r.V[h] = ""
			}
		}
		rows = append(rows, r)
	}
	return rows
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
