// awxprofile.go - conf 의 awx_profile_1~9 ("nodeinfo|OS값|설명") 파싱
package main

import (
	"fmt"
	"strings"
)

// AwxProfile: AWX 실행 프로파일 한 건
type AwxProfile struct {
	No       int
	Nodeinfo string // "y" / "n"
	OS       string
	Desc     string
}

var awxOSChoices = []string{"2024", "2025", "2026", "2026-OPC_MDP", "2026-ECAD_TCAD", "default"}

// parseAwxProfiles: 빈 변수는 조용히 건너뛰고, 형식·값이 틀린 프로파일은 제외하며 사유를 problems 로 돌려준다.
func parseAwxProfiles() (valid []AwxProfile, problems []string) {
	vals := []string{awx_profile_1, awx_profile_2, awx_profile_3, awx_profile_4, awx_profile_5,
		awx_profile_6, awx_profile_7, awx_profile_8, awx_profile_9}
	for i, raw := range vals {
		no := i + 1
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		name := fmt.Sprintf("awx_profile_%d", no)
		f := strings.SplitN(raw, "|", 3)
		if len(f) < 2 {
			problems = append(problems, name+": 형식 오류 (nodeinfo(y/n)|OS값|설명 이어야 함)")
			continue
		}
		ni := strings.ToLower(strings.TrimSpace(f[0]))
		if ni != "y" && ni != "n" {
			problems = append(problems, fmt.Sprintf("%s: nodeinfo 값 %s 은 y 또는 n 이어야 함", name, strings.TrimSpace(f[0])))
			continue
		}
		osv := strings.TrimSpace(f[1])
		okOS := false
		for _, c := range awxOSChoices {
			if osv == c {
				okOS = true
			}
		}
		if !okOS {
			problems = append(problems, fmt.Sprintf("%s: OS 값 %s 은 허용되지 않음 (%s)", name, osv, strings.Join(awxOSChoices, " ")))
			continue
		}
		desc := ""
		if len(f) == 3 {
			desc = strings.TrimSpace(f[2])
		}
		valid = append(valid, AwxProfile{No: no, Nodeinfo: ni, OS: osv, Desc: desc})
	}
	return valid, problems
}
