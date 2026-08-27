package main

import (
	"os/exec"
	"strings"
)

// InfraNet 은 conf 에 지정된 인프라망 조사 스크립트를 `<script> <hostname>` 으로 실행하고
// stdout 첫 비어있지 않은 줄을 인프라망 값으로 돌려준다.
// scriptPath 가 비어 있으면 빈 값을 돌려준다(오류 아님).
func InfraNet(scriptPath, hostname string) (string, error) {
	if strings.TrimSpace(scriptPath) == "" {
		return "", nil
	}
	out, err := exec.Command(scriptPath, hostname).Output()
	if err != nil {
		return "", err
	}
	for _, l := range strings.Split(strings.ReplaceAll(string(out), "\r\n", "\n"), "\n") {
		if l = strings.TrimSpace(l); l != "" {
			return l, nil
		}
	}
	return "", nil
}

// ApplStatus 는 설정값의 mountpoint 이름을 conf 정상 위치와 대조하고,
// 그 정상 위치가 표1의 위치와 같은지로 O/X 를 판정한다.
func ApplStatus(mounts []MountRule, configValue, assetLocation string) (mark, note string) {
	if configValue == "" {
		return "X", ""
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
