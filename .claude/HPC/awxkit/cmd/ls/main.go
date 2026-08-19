// awxkit-ls는 조회 가능한 템플릿 목록을 표 형태로 출력한다.
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
	flag.Parse()

	user := config.ResolveUser(*userFlag)
	confPath, err := config.ResolvePath(*confFlag, user)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[X] 설정 파일을 찾을 수 없습니다: %v\n", err)
		os.Exit(1)
	}

	_, client, err := cli.LoadConfigAndClient(confPath)
	if err != nil {
		fmt.Printf("[X] 설정 오류: %v\n", err)
		os.Exit(1)
	}

	templates, err := client.ListJobTemplates()
	if err != nil {
		fmt.Printf("[X] 템플릿 목록 조회 실패: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("총 %d개 템플릿\n", len(templates))
	fmt.Println("--------------------------------------------------------------------")
	for _, t := range templates {
		survey := "-"
		if t.SurveyEnabled {
			survey = "survey 있음"
		}
		vars := "extra_vars 비허용"
		if t.AskVariablesOnLaunch {
			vars = "extra_vars 허용"
		}
		fmt.Printf("  ID: %-5d | %-30s | %-16s | %s\n", t.ID, t.Name, vars, survey)
	}
	fmt.Println("--------------------------------------------------------------------")
	fmt.Println("survey 정의가 필요하면: bash survey.sh <ID 또는 이름>")
}
