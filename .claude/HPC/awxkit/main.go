package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"strings"

	"awxkit/config"
)

const usage = `awxkit - AWX 템플릿 실행 CLI (터미널 전용)

사용법:
  awxkit [-conf <경로>] [-user <사용자>] <명령> [옵션...]
  awxkit                          (인자 없이 실행하면 대화형 메뉴)

명령:
  doctor    설정/연결/권한을 점검합니다
  ls        템플릿 목록을 조회합니다
  survey <ID|이름>   템플릿의 survey 정의(변수명·선택지)를 조회합니다
  nodeinfo  [S1] NodeInfo 템플릿 실행 (다음 단계 예정)
  invsync   [S2] 인벤토리 동기화 (다음 단계 예정)
  dhcp      [S3] DHCP 등록 (다음 단계 예정)
  pxe       [S4] PXE 등록 (다음 단계 예정)
`

func main() {
	confFlag := flag.String("conf", "", "설정 파일 경로를 직접 지정합니다")
	userFlag := flag.String("user", "", "사용자 식별자를 직접 지정합니다 (AWXKIT_USER 환경변수보다 낮은 우선순위)")
	flag.Usage = func() { fmt.Fprint(os.Stderr, usage) }
	flag.Parse()

	args := flag.Args()

	user := config.ResolveUser(*userFlag)
	confPath, err := config.ResolvePath(*confFlag, user)

	var cmd string
	if len(args) > 0 {
		cmd = args[0]
	} else {
		cmd = runMenu()
	}
	if cmd == "" {
		return
	}

	switch cmd {
	case "help", "-h", "--help":
		fmt.Print(usage)
		return
	case "doctor":
		if err != nil {
			fmt.Fprintf(os.Stderr, "[X] 설정 파일을 찾을 수 없습니다: %v\n", err)
			os.Exit(1)
		}
		runDoctor(confPath)
	case "ls":
		if err != nil {
			fmt.Fprintf(os.Stderr, "[X] 설정 파일을 찾을 수 없습니다: %v\n", err)
			os.Exit(1)
		}
		runLs(confPath)
	case "survey":
		if err != nil {
			fmt.Fprintf(os.Stderr, "[X] 설정 파일을 찾을 수 없습니다: %v\n", err)
			os.Exit(1)
		}
		var templateArg string
		if len(args) > 1 {
			templateArg = args[1]
		}
		runSurvey(confPath, templateArg)
	case "nodeinfo", "invsync", "dhcp", "pxe":
		fmt.Printf("[!] '%s' 명령은 다음 단계에서 구현될 예정입니다. PLAN.md를 참고하세요.\n", cmd)
		os.Exit(1)
	default:
		fmt.Fprintf(os.Stderr, "알 수 없는 명령입니다: %s\n\n%s", cmd, usage)
		os.Exit(1)
	}
}

// runMenu는 인자 없이 실행했을 때 보여주는 대화형 번호 선택 메뉴다.
// ETX 원격 터미널 환경을 고려해 스피너/화면 재그리기 없이 줄 단위로만 출력한다.
func runMenu() string {
	options := []struct {
		label string
		cmd   string
	}{
		{"연결/설정 점검 (doctor)", "doctor"},
		{"템플릿 목록 조회 (ls)", "ls"},
		{"survey 정의 조회 (survey)", "survey"},
		{"[S1] NodeInfo 실행 (nodeinfo)", "nodeinfo"},
		{"[S2] 인벤토리 동기화 (invsync)", "invsync"},
		{"[S3] DHCP 등록 (dhcp)", "dhcp"},
		{"[S4] PXE 등록 (pxe)", "pxe"},
	}

	fmt.Println("=== awxkit ===")
	for i, o := range options {
		fmt.Printf("  %d) %s\n", i+1, o.label)
	}
	fmt.Println("  0) 종료")
	fmt.Print("번호 선택: ")

	reader := bufio.NewReader(os.Stdin)
	line, _ := reader.ReadString('\n')
	var choice int
	if _, err := fmt.Sscanf(line, "%d", &choice); err != nil || choice <= 0 || choice > len(options) {
		return ""
	}
	return options[choice-1].cmd
}

// promptLine은 프롬프트를 출력하고 표준입력에서 한 줄을 읽어 반환한다.
func promptLine(prompt string) string {
	fmt.Print(prompt)
	reader := bufio.NewReader(os.Stdin)
	line, _ := reader.ReadString('\n')
	return strings.TrimSpace(line)
}
