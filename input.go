package main

// stress-ng 종료 시각(5개 값) 입력의 검사. TUI(한 칸씩 입력할 때)와 워커(받은 답을 다시 확인할 때)가 같은 함수를 쓴다.
// stress-test -batch 의 -end 검사와 같은 규칙: 연 4자리(올해 이상), 월 1-12, 일 1-31, 시 0-23, 분 0-59, 합친 시각은 지금보다 뒤.
// 입력한 글자는 그대로(예: "07") stress-test 로 넘긴다.

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// endFields 는 입력 순서대로의 키와 화면 설명.
var endFields = []inputField{
	{Key: "end_year", Label: "연도(4자리)"},
	{Key: "end_month", Label: "월(1-12)"},
	{Key: "end_day", Label: "일(1-31)"},
	{Key: "end_hour", Label: "시(0-23)"},
	{Key: "end_min", Label: "분(0-59)"},
}

// checkEndField 는 키 하나의 값 s 를 검사한다(now 는 올해·시간대 기준). 틀리면 사용자에게 보일 사유를 돌려준다.
func checkEndField(key, s string, now time.Time) error {
	if s == "" {
		return fmt.Errorf("값을 입력하세요")
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return fmt.Errorf("숫자만 입력하세요")
		}
	}
	n, _ := strconv.Atoi(s)
	switch key {
	case "end_year":
		if len(s) != 4 {
			return fmt.Errorf("연도는 4자리로 입력하세요(예: %d)", now.Year())
		}
		if n < now.Year() {
			return fmt.Errorf("연도는 올해(%d) 이상이어야 합니다", now.Year())
		}
	case "end_month":
		if n < 1 || n > 12 {
			return fmt.Errorf("월은 1-12 입니다")
		}
	case "end_day":
		if n < 1 || n > 31 {
			return fmt.Errorf("일은 1-31 입니다")
		}
	case "end_hour":
		if n > 23 {
			return fmt.Errorf("시는 0-23 입니다")
		}
	case "end_min":
		if n > 59 {
			return fmt.Errorf("분은 0-59 입니다")
		}
	default:
		return fmt.Errorf("알 수 없는 입력 항목: %s", key)
	}
	return nil
}

// endTime 은 다섯 값으로 시각을 만든다(없는 날짜는 오류).
func endTime(vals map[string]string, loc *time.Location) (time.Time, error) {
	var n [5]int
	for i, f := range endFields {
		v, err := strconv.Atoi(vals[f.Key])
		if err != nil {
			return time.Time{}, fmt.Errorf("%s 값이 없습니다", f.Key)
		}
		n[i] = v
	}
	t := time.Date(n[0], time.Month(n[1]), n[2], n[3], n[4], 0, 0, loc)
	if t.Year() != n[0] || int(t.Month()) != n[1] || t.Day() != n[2] {
		return time.Time{}, fmt.Errorf("%d-%02d-%02d 는 없는 날짜입니다", n[0], n[1], n[2])
	}
	return t, nil
}

// checkEndAll 은 다섯 값을 모두 검사한다(각 값의 범위 + 없는 날짜 + 합친 시각이 now 보다 뒤).
func checkEndAll(vals map[string]string, now time.Time) error {
	for _, f := range endFields {
		if err := checkEndField(f.Key, vals[f.Key], now); err != nil {
			return fmt.Errorf("%s: %v", f.Key, err)
		}
	}
	t, err := endTime(vals, now.Location())
	if err != nil {
		return err
	}
	if !t.After(now) {
		return fmt.Errorf("종료 시각 %s 이(가) 지금보다 뒤여야 합니다", t.Format("2006-01-02 15:04"))
	}
	return nil
}

// endArg 는 stress-test -end 에 넘길 "연,월,일,시,분"(입력한 글자 그대로).
func endArg(vals map[string]string) string {
	p := make([]string, len(endFields))
	for i, f := range endFields {
		p[i] = vals[f.Key]
	}
	return strings.Join(p, ",")
}

// endText 는 화면용 "2026-11-07 18:00" (endArg 형식 "연,월,일,시,분" 에서).
func endText(arg string) string {
	p := strings.Split(arg, ",")
	if len(p) != 5 {
		return arg
	}
	return fmt.Sprintf("%s-%s-%s %s:%s", p[0], p[1], p[2], p[3], p[4])
}
