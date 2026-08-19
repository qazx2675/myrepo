package cli

import (
	"fmt"
	"strconv"
	"strings"
)

// ParseChoices는 "seoul, daejeon, busan" 형태의 conf 값을 트림된 슬라이스로 나눈다.
func ParseChoices(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	choices := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			choices = append(choices, p)
		}
	}
	return choices
}

// ResolveChoice는 flagVal(번호 또는 값 자체)을 choices 중 하나로 확정한다.
// flagVal이 비어 있으면 choices가 있을 때 번호 선택 메뉴를, 없으면 자유 입력을 받는다.
func ResolveChoice(label string, choices []string, flagVal string) (string, error) {
	if flagVal != "" {
		if n, err := strconv.Atoi(flagVal); err == nil && len(choices) > 0 {
			if n < 1 || n > len(choices) {
				return "", fmt.Errorf("%s 번호(%d)가 범위를 벗어났습니다 (1~%d)", label, n, len(choices))
			}
			return choices[n-1], nil
		}
		if len(choices) > 0 && !contains(choices, flagVal) {
			return "", fmt.Errorf("%s 값(%s)이 conf에 정의된 선택지에 없습니다: %v", label, flagVal, choices)
		}
		return flagVal, nil
	}

	if len(choices) == 0 {
		v := PromptLine(fmt.Sprintf("%s 값을 입력하세요: ", label))
		if v == "" {
			return "", fmt.Errorf("%s 값이 입력되지 않았습니다", label)
		}
		return v, nil
	}

	fmt.Printf("%s 선택:\n", label)
	for i, c := range choices {
		fmt.Printf("  %d) %s\n", i+1, c)
	}
	line := PromptLine("번호 선택: ")
	n, err := strconv.Atoi(line)
	if err != nil || n < 1 || n > len(choices) {
		return "", fmt.Errorf("올바른 번호를 선택하지 않았습니다")
	}
	return choices[n-1], nil
}

func contains(list []string, v string) bool {
	for _, item := range list {
		if item == v {
			return true
		}
	}
	return false
}
