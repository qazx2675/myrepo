package main

// stress-ng 종료 시각 5개 값(연,월,일,시,분): 검증·대화형 입력·-end 파싱·survey type 변환

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// endTime 은 입력한 종료 시각이다. Vals 는 입력한 글자 그대로(survey 가 text 면 그대로 보냄).
type endTime struct {
	Vals [5]string
	T    time.Time
}

var digitsRe = regexp.MustCompile(`^[0-9]+$`)

// checkEndField 는 i 번째(0=연 … 4=분) 값 하나를 검사한다.
func checkEndField(i int, s string, now time.Time) error {
	if !digitsRe.MatchString(s) {
		return fmt.Errorf("숫자만 입력하세요: %q", s)
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return fmt.Errorf("숫자가 너무 큽니다: %q", s)
	}
	switch i {
	case 0:
		if len(s) != 4 || n < now.Year() {
			return fmt.Errorf("연도는 4자리, %d 이상이어야 합니다: %q", now.Year(), s)
		}
	case 1:
		if n < 1 || n > 12 {
			return fmt.Errorf("월은 1-12 입니다: %q", s)
		}
	case 2:
		if n < 1 || n > 31 {
			return fmt.Errorf("일은 1-31 입니다: %q", s)
		}
	case 3:
		if n > 23 {
			return fmt.Errorf("시는 0-23 입니다: %q", s)
		}
	case 4:
		if n > 59 {
			return fmt.Errorf("분은 0-59 입니다: %q", s)
		}
	}
	return nil
}

// parseEnd 는 5개 값을 검사해 종료 시각을 만든다. 없는 날짜(2월 30일 등)·지금 이전 시각은 오류.
func parseEnd(vals []string, now time.Time) (*endTime, error) {
	if len(vals) != 5 {
		return nil, fmt.Errorf("종료 시각은 연,월,일,시,분 5개 값이어야 합니다 (%d개)", len(vals))
	}
	e := &endTime{}
	var n [5]int
	for i, v := range vals {
		v = strings.TrimSpace(v)
		if err := checkEndField(i, v, now); err != nil {
			return nil, err
		}
		e.Vals[i] = v
		n[i], _ = strconv.Atoi(v)
	}
	e.T = time.Date(n[0], time.Month(n[1]), n[2], n[3], n[4], 0, 0, now.Location())
	if e.T.Day() != n[2] {
		return nil, fmt.Errorf("없는 날짜입니다: %s-%s-%s", e.Vals[0], e.Vals[1], e.Vals[2])
	}
	if !e.T.After(now) {
		return nil, fmt.Errorf("종료 시각 %s 이 지금(%s)보다 뒤여야 합니다", e.T.Format("2006-01-02 15:04"), now.Format("2006-01-02 15:04"))
	}
	return e, nil
}

// parseEndFlag 는 -end "<연>,<월>,<일>,<시>,<분>" 을 읽는다.
func parseEndFlag(s string, now time.Time) (*endTime, error) {
	return parseEnd(strings.Split(s, ","), now)
}

func (e *endTime) String() string {
	return strings.Join(e.Vals[:], ",") + " (" + e.T.Format("2006-01-02 15:04") + ")"
}

// askEnd 는 대화형으로 키 하나씩 묻는다 (기본 제안 없음). 값마다 범위를 검사하고,
// 합친 시각이 지금 이전이거나 없는 날짜면 처음부터 다시 묻는다. 입력이 끊기면 오류.
func (a *app) askEnd() (*endTime, error) {
	keys := a.cfg.StressEndKeys
	a.logf("%s stress-ng 종료 시각을 입력하세요 (survey %s)", markInfo, strings.Join(keys, ","))
	for {
		vals := make([]string, 5)
		for i, k := range keys {
			for {
				fmt.Fprintf(a.out, "%s: ", k)
				line, err := a.in.ReadString('\n')
				v := strings.TrimSpace(line)
				if err != nil && v == "" {
					return nil, fmt.Errorf("종료 시각 입력이 끊겼습니다")
				}
				if e := checkEndField(i, v, nowFn()); e != nil {
					a.logf("  → %v", e)
					continue
				}
				vals[i] = v
				break
			}
		}
		e, err := parseEnd(vals, nowFn())
		if err == nil {
			a.logf("%s 종료 시각: %s", markOK, e)
			return e, nil
		}
		a.logf("  → %v — 처음부터 다시 입력하세요", err)
	}
}

// endVars 는 종료 시각을 survey 변수로 바꾼다. survey type 이 integer/float 면 숫자, 아니면 입력한 글자 그대로.
func endVars(keys []string, e *endTime, types map[string]string) map[string]interface{} {
	res := map[string]interface{}{}
	for i, k := range keys {
		v := e.Vals[i]
		switch types[k] {
		case "integer":
			n, _ := strconv.Atoi(v) // 검증에서 숫자만 통과
			res[k] = n
		case "float":
			f, _ := strconv.ParseFloat(v, 64)
			res[k] = f
		default:
			res[k] = v
		}
	}
	return res
}
