package model

import (
	"fmt"
	"strings"
)

// MaxGroup은 호스트 1대당 VM 그룹(ev01~evNN)의 최대 개수다. 예전에는 ev01~ev03 고정이었다.
// 그룹 이름이 ev%02d 두 자리라 99가 상한이다(ev100 은 "ev10"을 포함해 ev10으로 잘못 판정됨).
const MaxGroup = 99

// GroupName은 n(1~MaxGroup)번째 그룹 이름을 돌려준다. 예: 1 -> "ev01", 99 -> "ev99".
func GroupName(n int) string {
	return fmt.Sprintf("ev%02d", n)
}

// GroupNames는 ev01~ev99 전체를 순서대로 돌려준다.
func GroupNames() []string {
	names := make([]string, MaxGroup)
	for i := range names {
		names[i] = GroupName(i + 1)
	}
	return names
}

// ClassifyGroup은 VM 이름에 포함된 "evNN"으로 그룹을 정한다(대소문자 무시).
// 여러 개가 동시에 포함될 일은 없다고 가정하고 ev01 -> ev99 순으로 첫 매치를 채택한다
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
