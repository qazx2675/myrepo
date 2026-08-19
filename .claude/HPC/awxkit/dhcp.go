package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

const settingChangeWarning = "[!] 설정 변경 작업입니다. 완료 후 랜덤하게 서버 몇 대를 직접 확인해 실제로 변경되었는지 검증하세요."

// runDhcp는 [S3] DHCP 등록 템플릿을 실행하고 최종 상태를 즉시 출력한다.
// infraFlag가 비어 있으면 s3_infra_choices가 설정된 경우 번호 선택 메뉴를, 아니면 자유 입력을 받는다.
func runDhcp(confPath, user, infraFlag string) {
	cfg, client, err := loadConfigAndClient(confPath)
	if err != nil {
		fmt.Printf("[X] 설정 오류: %v\n", err)
		os.Exit(1)
	}
	if cfg.S3Template == "" {
		fmt.Println("[X] conf의 s3_template이 설정되지 않았습니다.")
		os.Exit(1)
	}

	choices := parseChoices(cfg.S3InfraChoices)
	infra, err := resolveChoice("인프라", choices, infraFlag)
	if err != nil {
		fmt.Printf("[X] %v\n", err)
		os.Exit(1)
	}

	t, err := client.ResolveTemplate(cfg.S3Template)
	if err != nil {
		fmt.Printf("[X] 템플릿(%s)을 찾을 수 없습니다: %v\n", cfg.S3Template, err)
		os.Exit(1)
	}

	fmt.Printf("[i] %s 실행 중... (%s=%s)\n", t.Name, cfg.S3InfraKey, infra)
	result, err := client.Launch(t.ID, map[string]interface{}{cfg.S3InfraKey: infra})
	if err != nil {
		fmt.Printf("[X] 실행 요청 실패: %v\n", err)
		appendHistory(cfg, fmt.Sprintf("user=%s action=dhcp infra=%s status=launch_error error=%q", user, infra, err.Error()))
		os.Exit(1)
	}
	if len(result.IgnoredFields) > 0 {
		fmt.Printf("    [!] 일부 값이 무시되었습니다(ignored_fields): %v — 템플릿의 ask_variables_on_launch 설정을 확인하세요.\n", result.IgnoredFields)
	}

	job, err := pollJob(client, result.Job, cfg.PollIntervalSec)
	if err != nil {
		fmt.Printf("[X] 상태 조회 실패: %v\n", err)
		appendHistory(cfg, fmt.Sprintf("user=%s action=dhcp infra=%s job=%d status=poll_error error=%q", user, infra, result.Job, err.Error()))
		os.Exit(1)
	}

	if job.Status != "successful" {
		fmt.Printf("[X] 실패 (status=%s)\n", job.Status)
		printStdoutTail(client, job.ID, 30)
		appendHistory(cfg, fmt.Sprintf("user=%s action=dhcp infra=%s job=%d status=%s", user, infra, job.ID, job.Status))
		fmt.Println(settingChangeWarning)
		os.Exit(1)
	}

	fmt.Printf("[✔] 성공 (job %d)\n", job.ID)
	fmt.Println(settingChangeWarning)
	appendHistory(cfg, fmt.Sprintf("user=%s action=dhcp infra=%s job=%d status=successful", user, infra, job.ID))
}

// parseChoices는 "seoul, daejeon, busan" 형태의 conf 값을 트림된 슬라이스로 나눈다.
func parseChoices(raw string) []string {
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

// resolveChoice는 flagVal(번호 또는 값 자체)을 choices 중 하나로 확정한다.
// flagVal이 비어 있으면 choices가 있을 때 번호 선택 메뉴를, 없으면 자유 입력을 받는다.
func resolveChoice(label string, choices []string, flagVal string) (string, error) {
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
		v := promptLine(fmt.Sprintf("%s 값을 입력하세요: ", label))
		if v == "" {
			return "", fmt.Errorf("%s 값이 입력되지 않았습니다", label)
		}
		return v, nil
	}

	fmt.Printf("%s 선택:\n", label)
	for i, c := range choices {
		fmt.Printf("  %d) %s\n", i+1, c)
	}
	line := promptLine("번호 선택: ")
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
