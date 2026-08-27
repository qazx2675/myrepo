package main

import (
	"os"
	"strings"
)

var tsvHeader = []string{"hostname", "위치", "상태", "설정값", "인프라망", "appl설정유무", "특이사항"}

// normalizeField 는 셀 구분(탭)을 깨뜨리지 않도록 필드 내부의 탭/개행을 스페이스로 바꾼다.
func normalizeField(s string) string {
	r := strings.NewReplacer("\t", " ", "\r", " ", "\n", " ")
	return strings.TrimSpace(r.Replace(s))
}

// WriteTSV 는 헤더 1줄 + 행별 탭 구분 데이터를 path 에 저장한다.
func WriteTSV(path string, rows [][]string) error {
	var b strings.Builder
	b.WriteString(strings.Join(tsvHeader, "\t"))
	b.WriteByte('\n')
	for _, row := range rows {
		cells := make([]string, len(row))
		for i, c := range row {
			cells[i] = normalizeField(c)
		}
		b.WriteString(strings.Join(cells, "\t"))
		b.WriteByte('\n')
	}
	return os.WriteFile(path, []byte(b.String()), 0o644)
}
