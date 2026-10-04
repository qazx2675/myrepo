package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

func main() {
	fmt.Println("=====================================================")
	fmt.Println("    서버 운영 및 관리 챗봇 (HPC/업무 프로세스 가이드)   ")
	fmt.Println("=====================================================")
	fmt.Println("안내: 영타로 입력된 한글을 변환하려면 앞에 '/h '를 붙여주세요.")
	fmt.Println("예시: /h xptmxm -> 테스트")
	fmt.Println("종료하려면 'exit' 또는 'quit'를 입력하세요.")
	fmt.Println("=====================================================")

	// Optional: read JEV API KEY from environment
	apiKey := os.Getenv("JEV_API_KEY")
	if apiKey == "" {
		fmt.Println("[System] JEV_API_KEY가 설정되지 않아 로컬 키워드 매칭(Fallback) 모드로 동작합니다.")
	} else {
		fmt.Println("[System] Typesafe JEV API 모드로 동작합니다.")
	}

	scanner := bufio.NewScanner(os.Stdin)
	for {
		fmt.Print("\n질문 입력 > ")
		if !scanner.Scan() {
			break
		}
		
		input := strings.TrimSpace(scanner.Text())
		
		if input == "exit" || input == "quit" {
			fmt.Println("챗봇을 종료합니다.")
			break
		}

		if input == "" {
			continue
		}

		// Handle /h prefix for English to Hangul conversion
		if strings.HasPrefix(input, "/h ") {
			engStr := strings.TrimPrefix(input, "/h ")
			converted := ConvertEngToHangul(engStr)
			fmt.Printf("[자동 변환됨]: %s\n", converted)
			input = converted
		}

		fmt.Println("\n[답변] 분석 중입니다...")
		
		// Use JEV to find intent
		intent := GetJevIntent(input, apiKey)
		
		// Fetch the response
		response := GetManualResponse(intent)
		
		fmt.Println("-----------------------------------------------------")
		fmt.Println(response)
		fmt.Println("-----------------------------------------------------")
	}
}
