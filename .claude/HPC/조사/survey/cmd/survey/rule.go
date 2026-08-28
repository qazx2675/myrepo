package main

import (
	"regexp"
	"strings"
)

// firstNonEmptyLine 은 여러 줄 문자열에서 첫 비어있지 않은(trim 후) 줄을 돌려준다.
func firstNonEmptyLine(s string) string {
	for _, l := range strings.Split(strings.ReplaceAll(s, "\r\n", "\n"), "\n") {
		if l = strings.TrimSpace(l); l != "" {
			return l
		}
	}
	return ""
}

// matchInfraLines 는 여러 줄 중 re 가 처음으로 매칭되는 줄의 값을 돌려준다.
// 출력이 "FAIL ldap_site\nINFO ldap infra" 처럼 여러 줄일 때, 조건에 맞는 줄만 고른다.
// re 가 nil 이면 첫 비어있지 않은 줄 전체.
func matchInfraLines(re *regexp.Regexp, lines []string) string {
	for _, l := range lines {
		if l = strings.TrimSpace(l); l == "" {
			continue
		}
		if v := applyInfraRegex(re, l); v != "" {
			return v
		}
	}
	return ""
}

// applyInfraRegex 는 인프라망 스크립트 출력값 한 줄에 conf 정규식(캡처 그룹 1)을 적용한다.
//   - re 가 nil 이면 출력값 그대로
//   - 매칭 안 되면 "" (인프라 미조사로 취급)
func applyInfraRegex(re *regexp.Regexp, out string) string {
	if out == "" {
		return ""
	}
	if re == nil {
		return out
	}
	m := re.FindStringSubmatch(out)
	if m == nil {
		return ""
	}
	if len(m) >= 2 {
		return strings.TrimSpace(m[1])
	}
	return strings.TrimSpace(m[0])
}

// ApplStatus 는 설정값(비어있지 않음)의 mountpoint 이름을 conf 정상 위치와 대조하고,
// 그 정상 위치가 표1의 위치와 같은지로 O/X 를 판정한다.
// 설정값이 비어 있는 경우의 처리는 호출부(main)에서 한다.
func ApplStatus(mounts []MountRule, configValue, assetLocation string) (mark, note string) {
	if configValue == "" {
		return "", ""
	}
	mp := configValue
	if i := strings.Index(mp, ":"); i >= 0 {
		mp = mp[:i]
	}
	mp = strings.TrimSpace(mp)
	for _, m := range mounts {
		if strings.TrimSpace(m.Name) == mp {
			if strings.TrimSpace(m.Location) == strings.TrimSpace(assetLocation) {
				return "O", ""
			}
			return "X", ""
		}
	}
	return "X", "mountpoint 미정의"
}
