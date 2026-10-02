// main.go - auto_setup: OS 설치(01) → OS 설정체크(os_check) 자동 연계 데몬/CLI
package main

import (
	"fmt"
	"os"
	"runtime"
	"time"
)

// 환경별로 채우는 값. 커밋·배포 소스에서는 항상 빈 문자열로 둔다.
var (
	os6_mgmt      = "" // 모든 대역 접근 가능한 서버 (비우면 os6 경로 미사용)
	os6_gossh     = "" // os6_mgmt 위 gossh 실행 파일 전체 경로
	os6_autosetup = "" // os6_mgmt 위 auto_setup(os6 빌드) 이 있는 디렉터리
	os_check_sh   = "" // 이 서버의 os_check_final_annotated.sh 전체 경로
)

// 조정 가능한 값 (빈 변수 아님)
var (
	pingInterval  = 10 * time.Second
	downMisses    = 2 // 연속 무응답 횟수 → ping X
	checkInterval = 30 * time.Second
	bootWait      = 7 * time.Minute // 최초 READY 후 대기
	lateWait      = 3 * time.Minute // 늦게 올라온 서버 묶음 대기
	queuePoll     = 5 * time.Second
)

const usageText = `사용법: auto_setup <명령>
  daemon              포그라운드 데몬 (보통 ensure 가 백그라운드로 기동)
  ensure              데몬이 없으면 백그라운드로 기동 (cron 매분)
  code NNNN           code 에 해당하는 결과 코멘트 출력
  status              작업별 진행 상태 출력
  cancel <jobid>      무기한 감시 중인 작업 종료
  probe [-i 10s]      (os6_mgmt 쪽) stdin 호스트 목록 ping 감시
`

func main() {
	runtime.GOMAXPROCS(1)
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	if len(args) == 0 {
		fmt.Fprint(os.Stderr, usageText)
		return 1
	}
	switch args[0] {
	case "daemon":
		return runDaemon()
	case "ensure":
		return runEnsure()
	case "code":
		if len(args) != 2 {
			fmt.Fprint(os.Stderr, usageText)
			return 1
		}
		return runCode(args[1])
	case "status":
		return runStatus()
	case "cancel":
		if len(args) != 2 {
			fmt.Fprint(os.Stderr, usageText)
			return 1
		}
		return runCancel(args[1])
	case "probe":
		return runProbe(args[1:])
	}
	fmt.Fprint(os.Stderr, usageText)
	return 1
}

// runCode: codes/NNNN.txt 출력
func runCode(code string) int {
	b, err := readCode(code)
	if err != nil {
		fmt.Println("[X] code 없음")
		return 1
	}
	fmt.Print(string(b))
	return 0
}

// runStatus: 작업별 진행 상태 출력
func runStatus() int {
	jobs, err := loadJobs()
	if err != nil {
		fmt.Fprintln(os.Stderr, "[X] jobs 읽기 실패:", err)
		return 1
	}
	if len(jobs) == 0 {
		fmt.Println("진행 중인 작업 없음")
		return 0
	}
	for _, j := range jobs {
		fmt.Print(formatJobStatus(j))
	}
	return 0
}

// runCancel: 작업을 jobs/done 으로 이동
func runCancel(id string) int {
	if err := cancelJob(id); err != nil {
		fmt.Fprintln(os.Stderr, "[X]", err)
		return 1
	}
	logf("cancel %s", id)
	fmt.Println("[O] 작업 종료:", id)
	return 0
}
