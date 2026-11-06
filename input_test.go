package main

import (
	"strings"
	"testing"
	"time"
)

var inNow = time.Date(2026, 11, 5, 9, 0, 0, 0, time.UTC)

func TestCheckEndField(t *testing.T) {
	cases := []struct {
		key, val string
		bad      string // 오류에 들어갈 글자(빈 값 = 통과)
	}{
		{"end_year", "2026", ""}, {"end_year", "2030", ""}, {"end_year", "2025", "올해"}, {"end_year", "26", "4자리"},
		{"end_year", "20267", "4자리"}, {"end_year", "", "입력"}, {"end_year", "20a6", "숫자"}, {"end_year", "-1", "숫자"},
		{"end_month", "1", ""}, {"end_month", "12", ""}, {"end_month", "07", ""}, {"end_month", "0", "1-12"}, {"end_month", "13", "1-12"},
		{"end_day", "1", ""}, {"end_day", "31", ""}, {"end_day", "0", "1-31"}, {"end_day", "32", "1-31"},
		{"end_hour", "0", ""}, {"end_hour", "00", ""}, {"end_hour", "23", ""}, {"end_hour", "24", "0-23"},
		{"end_min", "0", ""}, {"end_min", "59", ""}, {"end_min", "60", "0-59"},
		{"end_xxx", "1", "알 수 없는"},
	}
	for _, c := range cases {
		err := checkEndField(c.key, c.val, inNow)
		switch {
		case c.bad == "" && err != nil:
			t.Errorf("%s=%q: 통과해야 함: %v", c.key, c.val, err)
		case c.bad != "" && (err == nil || !strings.Contains(err.Error(), c.bad)):
			t.Errorf("%s=%q: 오류 %q 기대, got %v", c.key, c.val, c.bad, err)
		}
	}
}

func evals(y, mo, d, h, mi string) map[string]string {
	return map[string]string{"end_year": y, "end_month": mo, "end_day": d, "end_hour": h, "end_min": mi}
}

func TestCheckEndAll(t *testing.T) {
	if err := checkEndAll(evals("2026", "11", "05", "09", "01"), inNow); err != nil {
		t.Errorf("1분 뒤는 통과: %v", err)
	}
	if err := checkEndAll(evals("2026", "11", "5", "9", "0"), inNow); err == nil || !strings.Contains(err.Error(), "지금보다 뒤") {
		t.Errorf("같은 시각은 거절: %v", err)
	}
	if err := checkEndAll(evals("2026", "11", "05", "08", "59"), inNow); err == nil {
		t.Error("과거는 거절")
	}
	if err := checkEndAll(evals("2027", "02", "31", "10", "00"), inNow); err == nil || !strings.Contains(err.Error(), "없는 날짜") {
		t.Errorf("없는 날짜: %v", err)
	}
	if err := checkEndAll(evals("2026", "13", "01", "10", "00"), inNow); err == nil || !strings.Contains(err.Error(), "end_month") {
		t.Errorf("월 범위: %v", err)
	}
	if err := checkEndAll(map[string]string{"end_year": "2026"}, inNow); err == nil {
		t.Error("값이 모자라면 거절")
	}
}

func TestEndArgAndText(t *testing.T) {
	v := evals("2026", "11", "07", "08", "05")
	if got := endArg(v); got != "2026,11,07,08,05" {
		t.Errorf("endArg=%q", got)
	}
	if got := endText("2026,11,07,08,05"); got != "2026-11-07 08:05" {
		t.Errorf("endText=%q", got)
	}
	if got := endText("x"); got != "x" {
		t.Errorf("endText(x)=%q", got)
	}
}
