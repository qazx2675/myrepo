// main.go - auto_setup: OS 설치(01) → OS 설정체크(os_check) 자동 연계 데몬/CLI
package main

import (
	"fmt"
	"os"
	"runtime"
	"strings"
	"time"
)

// 환경별로 채우는 값. 커밋·배포 소스에서는 항상 빈 문자열로 둔다.
var (
	os6_mgmt      = "" // 모든 대역 접근 가능한 서버 (비우면 os6 경로 미사용)
	os6_gossh     = "" // os6_mgmt 위 gossh 실행 파일 전체 경로
	os6_autosetup = "" // os6_mgmt 위 auto_setup(os6 빌드) 이 있는 디렉터리
	os_check_sh   = "" // 이 서버의 os_check_final_annotated.sh 전체 경로 (autofs 공유 경로면 os8_mgmt·os6_mgmt 동일)
	awx_dir       = "" // awx 스크립트 경로: awxkit/dhcp.sh 등이 있는 awx_script 디렉터리 (autofs 로 os8_mgmt·os6_mgmt 동일 경로)

	// 2차 (§14-1)
	os8_mgmt        = "" // 비어 있지 않으면 원격 클라이언트 모드 (os6 빌드 때 -ldflags -X 로 채움)
	os8_autosetup   = "" // os8_mgmt 위 auto_setup 경로 (비면 /usr/local/bin/auto_setup)
	os6_os_check_sh = "" // os6_mgmt 위 os_check 경로 (2차 체크용). 비우면 os_check_sh 와 같은 경로(autofs 공유)로 간주, os6_mgmt 도 비면 2차 체크 생략, "-" 면 2차 체크 끄기
	ldap_share_dir  = "" // LDAP 복원용 autofs 공유 경로: os8_mgmt·대상 서버가 모두 보는 곳, 비우면 자동 복원 안 함(수동 복원 안내). 대상 root 가 읽을 수 있어야 함(no_root_squash)
)

// 조정 가능한 값 (빈 변수 아님)
var (
	pingInterval  = 10 * time.Second
	downMisses    = 2 // 연속 무응답 횟수 → ping X
	checkInterval = 30 * time.Second
	bootWait      = 7 * time.Minute // 최초 READY 후 대기
	lateWait      = 3 * time.Minute // 늦게 올라온 서버 묶음 대기
	queuePoll     = 5 * time.Second
	installStuck  = 60 * time.Minute // seen_down 후 이 시간을 넘겨도 READY 가 안 되면 정체(stuck)
)

const usageText = `사용법: auto_setup [명령]
  (인자 없음)         현재 상태 리포트 (tty 면 TUI, 아니면 텍스트 표)
  --plain             텍스트 표로 출력
  -h, --help          이 도움말
  --start | start     데몬 기동 (이미 실행 중이면 안내)
  --stop | stop       데몬 종료
  --restart | restart 데몬 재기동
  status              작업별 진행 상태 출력
  snapshot            상태 스냅샷(JSON) 출력
  code NNNN           code 에 해당하는 결과 코멘트 출력
  cancel <jobid>      무기한 감시 중인 작업 종료
  done <host...>      호스트를 수동 완료 처리 (부팅 시각 검증 없음)
  done --job <jobid> [--yml <그룹>]  job(또는 그 그룹)의 미완료 호스트 전부를 수동 완료 처리
  request manual-run <jobid> <yml>   수동 OS 체크 실행 요청
  request cancel <jobid>             작업 종료 요청
  request refresh                    즉시 ping·준비확인 요청 (TUI r 키와 같음)
  daemon              포그라운드 데몬 (보통 ensure 가 백그라운드로 기동)
  ensure              데몬이 없으면 백그라운드로 기동 (cron 매분)
  probe [-i 10s]      (os6_mgmt 쪽) stdin 호스트 목록 ping 감시
`

func main() {
	runtime.GOMAXPROCS(1)
	loadConf(confPaths())
	os.Exit(run(os.Args[1:]))
}

func usageFail() int {
	fmt.Fprint(os.Stderr, usageText)
	return 1
}

func run(args []string) int {
	if len(args) == 0 {
		return cmdReport(false)
	}
	switch args[0] {
	case "-h", "--help", "help":
		fmt.Print(usageText)
		return 0
	case "--plain":
		if len(args) != 1 {
			return usageFail()
		}
		return cmdReport(true)
	case "--start", "start", "--stop", "stop", "--restart", "restart":
		if len(args) != 1 {
			return usageFail()
		}
		if os8_mgmt != "" {
			msgErr(remoteOnlyMsg)
			return 1
		}
		return cmdDaemonCtl(strings.TrimPrefix(args[0], "--"))
	case "daemon", "ensure":
		if os8_mgmt != "" {
			msgErr(remoteOnlyMsg)
			return 1
		}
		if args[0] == "daemon" {
			return runDaemon()
		}
		return runEnsure()
	case "probe":
		return runProbe(args[1:])
	case "status", "snapshot", "code", "cancel", "done", "request":
		if !relayArgsOK(args) {
			return usageFail()
		}
		if os8_mgmt != "" {
			return relayRemote(args)
		}
		switch args[0] {
		case "status":
			return runStatus()
		case "snapshot":
			return cmdSnapshot()
		case "code":
			return runCode(args[1])
		case "cancel":
			return runCancel(args[1])
		case "done":
			return cmdDone(args[1:])
		}
		return cmdRequest(args[1:])
	}
	return usageFail()
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
