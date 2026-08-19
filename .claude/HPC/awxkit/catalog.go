package main

import (
	"fmt"
	"os"
	"strings"
)

// runLs는 조회 가능한 템플릿 목록을 표 형태로 출력한다.
func runLs(confPath string) {
	_, client, err := loadConfigAndClient(confPath)
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
	fmt.Println("survey 정의가 필요하면: awxkit survey <ID 또는 이름>")
}

// runSurvey는 지정한 템플릿의 survey 문항(변수명·선택지)을 조회해 출력한다.
// templateArg가 비어 있으면 표준입력으로 물어본다.
func runSurvey(confPath, templateArg string) {
	if templateArg == "" {
		templateArg = promptLine("템플릿 ID 또는 이름: ")
	}
	if templateArg == "" {
		fmt.Println("[X] 템플릿을 지정하지 않았습니다.")
		os.Exit(1)
	}

	_, client, err := loadConfigAndClient(confPath)
	if err != nil {
		fmt.Printf("[X] 설정 오류: %v\n", err)
		os.Exit(1)
	}

	t, err := client.ResolveTemplate(templateArg)
	if err != nil {
		fmt.Printf("[X] 템플릿(%s)을 찾을 수 없습니다: %v\n", templateArg, err)
		os.Exit(1)
	}

	if !t.SurveyEnabled {
		fmt.Printf("[i] %s (ID: %d) 템플릿은 survey가 비활성화되어 있습니다.\n", t.Name, t.ID)
		if t.AskVariablesOnLaunch {
			fmt.Println("    extra_vars는 허용되지만, 변수명은 플레이북/현장 문서를 참고해 conf에 직접 채워야 합니다.")
		}
		return
	}

	spec, err := client.GetSurveySpec(t.ID)
	if err != nil {
		fmt.Printf("[X] survey 정의 조회 실패: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("[✔] %s (ID: %d) — survey %d개 문항\n", t.Name, t.ID, len(spec.Spec))
	for i, q := range spec.Spec {
		required := ""
		if q.Required {
			required = " (필수)"
		}
		fmt.Printf("  %d) %-20s var=%-20s%s\n", i+1, q.QuestionName, q.Variable, required)
		if choices := formatChoices(q.Choices); choices != "" {
			fmt.Printf("     선택지: [%s]\n", choices)
		}
		if q.Default != nil && q.Default != "" {
			fmt.Printf("     기본값: %v\n", q.Default)
		}
	}
	fmt.Println("\n위 var= 값을 conf의 관련 key(s3_infra_key, s4_osver_key 등)에 그대로 사용하세요.")
}

// formatChoices는 AWX가 줄바꿈 구분 문자열 또는 배열 어느 쪽으로 주더라도 "a | b | c" 형태로 만든다.
func formatChoices(choices interface{}) string {
	switch v := choices.(type) {
	case nil:
		return ""
	case string:
		v = strings.TrimSpace(v)
		if v == "" {
			return ""
		}
		return strings.Join(strings.Split(v, "\n"), " | ")
	case []interface{}:
		parts := make([]string, 0, len(v))
		for _, item := range v {
			parts = append(parts, fmt.Sprintf("%v", item))
		}
		return strings.Join(parts, " | ")
	default:
		return fmt.Sprintf("%v", v)
	}
}
