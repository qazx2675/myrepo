// awxkit-pxe는 [S4] PXE 등록 템플릿을 인프라·OS 버전·Boot Mode·Splunk 설치 여부
// 4개 옵션으로 실행하고, 완료 후 대상 인벤토리의 전체 호스트 수를 리포트한다.
package main

import (
	"flag"
	"fmt"
	"os"
	"strconv"

	"awxkit/cli"
	"awxkit/config"
)

func main() {
	confFlag := flag.String("conf", "", "설정 파일 경로를 직접 지정합니다")
	userFlag := flag.String("user", "", "사용자 식별자를 직접 지정합니다 (AWXKIT_USER 환경변수보다 낮은 우선순위)")
	infraFlag := flag.String("infra", "", "인프라 선택지 번호 또는 값")
	osFlag := flag.String("os", "", "OS 버전 선택지 번호 또는 값")
	bootFlag := flag.String("boot", "", "Boot Mode 선택지 번호 또는 값")
	splunkFlag := flag.String("splunk", "", "Splunk 설치 여부 선택지 번호 또는 값")
	flag.Parse()

	user := config.ResolveUser(*userFlag)
	confPath, err := config.ResolvePath(*confFlag, user)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[X] 설정 파일을 찾을 수 없습니다: %v\n", err)
		os.Exit(1)
	}

	cfg, client, err := cli.LoadConfigAndClient(confPath)
	if err != nil {
		fmt.Printf("[X] 설정 오류: %v\n", err)
		os.Exit(1)
	}
	if cfg.S4Template == "" {
		fmt.Println("[X] conf의 s4_template이 설정되지 않았습니다.")
		os.Exit(1)
	}

	infra, err := cli.ResolveChoice("인프라", cli.ParseChoices(cfg.S4InfraChoices), *infraFlag)
	if err != nil {
		fmt.Printf("[X] %v\n", err)
		os.Exit(1)
	}
	osVer, err := cli.ResolveChoice("OS 버전", cli.ParseChoices(cfg.S4OSVerChoices), *osFlag)
	if err != nil {
		fmt.Printf("[X] %v\n", err)
		os.Exit(1)
	}
	bootMode, err := cli.ResolveChoice("Boot Mode", cli.ParseChoices(cfg.S4BootModeChoices), *bootFlag)
	if err != nil {
		fmt.Printf("[X] %v\n", err)
		os.Exit(1)
	}
	splunk, err := cli.ResolveChoice("Splunk 설치 여부", cli.ParseChoices(cfg.S4SplunkChoices), *splunkFlag)
	if err != nil {
		fmt.Printf("[X] %v\n", err)
		os.Exit(1)
	}

	t, err := client.ResolveTemplate(cfg.S4Template)
	if err != nil {
		fmt.Printf("[X] 템플릿(%s)을 찾을 수 없습니다: %v\n", cfg.S4Template, err)
		os.Exit(1)
	}

	extraVars := map[string]interface{}{
		cfg.S4InfraKey:    infra,
		cfg.S4OSVerKey:    osVer,
		cfg.S4BootModeKey: bootMode,
		cfg.S4SplunkKey:   splunk,
	}
	fmt.Printf("[i] %s 실행 중... (%s=%s, %s=%s, %s=%s, %s=%s)\n", t.Name,
		cfg.S4InfraKey, infra, cfg.S4OSVerKey, osVer, cfg.S4BootModeKey, bootMode, cfg.S4SplunkKey, splunk)

	histParams := fmt.Sprintf("infra=%s os=%s boot=%s splunk=%s", infra, osVer, bootMode, splunk)

	result, err := client.Launch(t.ID, extraVars)
	if err != nil {
		fmt.Printf("[X] 실행 요청 실패: %v\n", err)
		cli.AppendHistory(cfg, fmt.Sprintf("user=%s action=pxe %s status=launch_error error=%q", user, histParams, err.Error()))
		os.Exit(1)
	}
	if len(result.IgnoredFields) > 0 {
		fmt.Printf("    [!] 일부 값이 무시되었습니다(ignored_fields): %v — 템플릿의 ask_variables_on_launch 설정을 확인하세요.\n", result.IgnoredFields)
	}

	job, err := cli.PollJob(client, result.Job, cfg.PollIntervalSec)
	if err != nil {
		fmt.Printf("[X] 상태 조회 실패: %v\n", err)
		cli.AppendHistory(cfg, fmt.Sprintf("user=%s action=pxe %s job=%d status=poll_error error=%q", user, histParams, result.Job, err.Error()))
		os.Exit(1)
	}
	if job.Status != "successful" {
		fmt.Printf("[X] 실패 (status=%s)\n", job.Status)
		cli.PrintStdoutTail(client, job.ID, 30)
		cli.AppendHistory(cfg, fmt.Sprintf("user=%s action=pxe %s job=%d status=%s", user, histParams, job.ID, job.Status))
		os.Exit(1)
	}
	fmt.Printf("[✔] 성공 (job %d)\n", job.ID)
	cli.AppendHistory(cfg, fmt.Sprintf("user=%s action=pxe %s job=%d status=successful", user, histParams, job.ID))

	if cfg.S4Inventory == "" {
		fmt.Println("[i] conf의 s4_inventory가 설정되지 않아 등록 완료 호스트 수 집계는 건너뜁니다.")
		return
	}
	inventoryID, err := strconv.Atoi(cfg.S4Inventory)
	if err != nil {
		fmt.Printf("[X] s4_inventory(%s)는 숫자 ID여야 합니다.\n", cfg.S4Inventory)
		os.Exit(1)
	}
	count, err := client.CountInventoryHosts(inventoryID)
	if err != nil {
		fmt.Printf("[X] 인벤토리(%d) 호스트 수 조회 실패: %v\n", inventoryID, err)
		os.Exit(1)
	}
	fmt.Printf("총 %d대의 호스트가 등록 완료되었습니다.\n", count)
}
