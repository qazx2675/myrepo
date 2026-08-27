package main

import (
	"bytes"
	"os/exec"
	"regexp"
	"strings"
)

// InfraNet 은 conf 에 지정된 인프라망 조사 스크립트를 `<script> <hostname>` 으로 실행하고
// stdout 첫 비어있지 않은 줄에 conf 정규식(re)을 적용해 인프라망 값을 뽑는다.
//   - scriptPath 가 비어 있으면 빈 값(오류 아님)
//   - re 가 nil 이면 첫 줄 전체를 값으로 사용
//   - re 가 있으나 매칭되지 않으면 빈 값 (인프라 미조사로 취급)
//   - 스크립트가 0이 아닌 코드로 끝나도 stdout 이 있으면 그 출력을 사용한다
func InfraNet(scriptPath string, re *regexp.Regexp, hostname string) (string, error) {
	if strings.TrimSpace(scriptPath) == "" {
		return "", nil
	}
	cmd := exec.Command(scriptPath, hostname)
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	runErr := cmd.Run()

	line := firstNonEmptyLine(stdout.String())
	if line == "" {
		if runErr != nil {
			return "", runErr
		}
		return "", nil
	}
	return applyInfraRegex(re, line), nil
}

func firstNonEmptyLine(s string) string {
	for _, l := range strings.Split(strings.ReplaceAll(s, "\r\n", "\n"), "\n") {
		if l = strings.TrimSpace(l); l != "" {
			return l
		}
	}
	return ""
}

func applyInfraRegex(re *regexp.Regexp, line string) string {
	if line == "" {
		return ""
	}
	if re == nil {
		return line
	}
	m := re.FindStringSubmatch(line)
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
