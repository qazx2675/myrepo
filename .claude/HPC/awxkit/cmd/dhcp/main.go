// awxkit-dhcp는 [S3] DHCP 등록 템플릿을 실행하고 최종 상태를 즉시 출력한다.
// -infra가 비어 있으면 s3_infra_choices가 설정된 경우 번호 선택 메뉴를, 아니면 자유 입력을 받는다.
package main

import (
	"flag"
	"fmt"
	"os"

	"awxkit/cli"
	"awxkit/config"
)

func main() {
	confFlag := flag.String("conf", "", "설정 파일 경로를 직접 지정합니다")
	userFlag := flag.String("user", "", "사용자 식별자를 직접 지정합니다 (AWXKIT_USER 환경변수보다 낮은 우선순위)")
	infraFlag := flag.String("infra", "", "인프라 선택지 번호 또는 값")
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
	if cfg.S3Template == "" {
		fmt.Println("[X] conf의 s3_template이 설정되지 않았습니다.")
		os.Exit(1)
	}

	infra, err := cli.ResolveChoice("인프라", cli.ParseChoices(cfg.S3InfraChoices), *infraFlag)
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
		cli.AppendHistory(cfg, fmt.Sprintf("user=%s action=dhcp infra=%s status=launch_error error=%q", user, infra, err.Error()))
		os.Exit(1)
	}
	if len(result.IgnoredFields) > 0 {
		fmt.Printf("    [!] 일부 값이 무시되었습니다(ignored_fields): %v — 템플릿의 ask_variables_on_launch 설정을 확인하세요.\n", result.IgnoredFields)
	}

	job, err := cli.PollJob(client, result.Job, cfg.PollIntervalSec)
	if err != nil {
		fmt.Printf("[X] 상태 조회 실패: %v\n", err)
		cli.AppendHistory(cfg, fmt.Sprintf("user=%s action=dhcp infra=%s job=%d status=poll_error error=%q", user, infra, result.Job, err.Error()))
		os.Exit(1)
	}

	if job.Status != "successful" {
		fmt.Printf("[X] 실패 (status=%s)\n", job.Status)
		cli.PrintStdoutTail(client, job.ID, 30)
		cli.AppendHistory(cfg, fmt.Sprintf("user=%s action=dhcp infra=%s job=%d status=%s", user, infra, job.ID, job.Status))
		fmt.Println(cli.SettingChangeWarning)
		os.Exit(1)
	}

	fmt.Printf("[✔] 성공 (job %d)\n", job.ID)
	fmt.Println(cli.SettingChangeWarning)
	cli.AppendHistory(cfg, fmt.Sprintf("user=%s action=dhcp infra=%s job=%d status=successful", user, infra, job.ID))
}
