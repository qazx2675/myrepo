package main

import (
	"encoding/csv"
	"os"
	"strconv"
)

// report 는 결과를 result.csv 에 기록합니다.

// Result 는 대상 한 대의 처리 결과입니다.
type Result struct {
	IP         string
	FoundIndex int    // 성공한 후보의 인덱스(0부터), 접속불가면 -1
	Status     string // "성공" / "접속불가"
	Applied    bool   // 새 비밀번호 적용 여부
}

// writeCSV 는 결과 목록을 CSV 로 씁니다.
// 컬럼: ip, found_password_index, status, applied_new_password
func writeCSV(path string, results []Result) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	w := csv.NewWriter(f)
	defer w.Flush()

	if err := w.Write([]string{"ip", "found_password_index", "status", "applied_new_password"}); err != nil {
		return err
	}
	for _, r := range results {
		idx := ""
		if r.FoundIndex >= 0 {
			idx = strconv.Itoa(r.FoundIndex)
		}
		applied := "아니오"
		if r.Applied {
			applied = "예"
		}
		if err := w.Write([]string{r.IP, idx, r.Status, applied}); err != nil {
			return err
		}
	}
	return w.Error()
}
