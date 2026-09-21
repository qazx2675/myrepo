package model

import (
	"fmt"
	"strings"
)

// MaxGroup은 호스트 1대당 VM 그룹(ev01~evNN)의 최대 개수다. 예전에는 ev01~ev03 고정이었다.
const MaxGroup = 10

// GroupName은 n(1~MaxGroup)번째 그룹 이름을 돌려준다. 예: 1 -> "ev01", 10 -> "ev10".
func GroupName(n int) string {
	return fmt.Sprintf("ev%02d", n)
}

// GroupNames는 ev01~ev10 전체를 순서대로 돌려준다.
func GroupNames() []string {
	names := make([]string, MaxGroup)
	for i := range names {
		names[i] = GroupName(i + 1)
	}
	return names
}

// ClassifyGroup은 VM 이름에 포함된 "evNN"으로 그룹을 정한다(대소문자 무시).
// 여러 개가 동시에 포함될 일은 없다고 가정하고 ev01 -> ev10 순으로 첫 매치를 채택한다
// (예전 ev01 -> ev02 -> ev03 순서를 그대로 늘린 것). 어디에도 안 맞으면 "".
func ClassifyGroup(name string) string {
	h := strings.ToLower(name)
	for _, g := range GroupNames() {
		if strings.Contains(h, g) {
			return g
		}
	}
	return ""
}
