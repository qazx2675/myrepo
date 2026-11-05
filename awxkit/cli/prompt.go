package cli

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

// 여러 번 호출해도 한 번에 붙여넣은 입력이 버려지지 않도록 reader를 공유한다.
var stdinReader = bufio.NewReader(os.Stdin)

// PromptLine은 프롬프트를 출력하고 표준입력에서 한 줄을 읽어 반환한다.
func PromptLine(prompt string) string {
	fmt.Print(prompt)
	line, _ := stdinReader.ReadString('\n')
	return strings.TrimSpace(line)
}

// PromptYesNo는 프롬프트를 출력하고 y/yes(대소문자 무관) 입력일 때만 true를 반환한다.
func PromptYesNo(prompt string) bool {
	ans := strings.ToLower(PromptLine(prompt))
	return ans == "y" || ans == "yes"
}
