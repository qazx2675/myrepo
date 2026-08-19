package main

import (
	"fmt"
	"os"
	"strings"
	"time"

	"awxkit/awx"
	"awxkit/config"
)

// loadConfigAndClient는 conf 파일을 읽고 그 값으로 AWX 클라이언트를 생성한다.
// doctor/ls/survey 등 AWX에 접속하는 모든 명령이 공용으로 사용한다.
func loadConfigAndClient(confPath string) (*config.Config, *awx.Client, error) {
	cfg, err := config.Load(confPath)
	if err != nil {
		return nil, nil, err
	}
	if cfg.AWXURL == "" || cfg.Username == "" || cfg.Password == "" {
		return nil, nil, fmt.Errorf("awx_url / username / password 중 비어있는 값이 있습니다")
	}
	client := awx.NewClient(cfg.AWXURL, cfg.Username, cfg.Password, cfg.InsecureTLS, 10*time.Second)
	return cfg, client, nil
}

// appendHistory는 실행 이력을 cfg.HistoryFile에 한 줄 추가한다.
func appendHistory(cfg *config.Config, line string) {
	if cfg.HistoryFile == "" {
		return
	}
	f, err := os.OpenFile(cfg.HistoryFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		fmt.Printf("[!] 이력 파일 기록 실패 (%s): %v\n", cfg.HistoryFile, err)
		return
	}
	defer f.Close()
	fmt.Fprintf(f, "%s %s\n", time.Now().Format(time.RFC3339), line)
}

// isTerminalStatus는 AWX Job 상태가 더 이상 바뀌지 않는 최종 상태인지 반환한다.
func isTerminalStatus(status string) bool {
	switch status {
	case "successful", "failed", "error", "canceled":
		return true
	default:
		return false
	}
}

// pollJob은 Job이 최종 상태가 될 때까지 intervalSec 간격으로 상태를 조회한다.
// 스피너 없이 상태가 바뀔 때만 한 줄을 출력한다 (ETX 원격 터미널 환경 고려).
func pollJob(client *awx.Client, jobID int, intervalSec int) (*awx.Job, error) {
	if intervalSec <= 0 {
		intervalSec = 3
	}
	last := ""
	for {
		job, err := client.GetJob(jobID)
		if err != nil {
			return nil, err
		}
		if job.Status != last {
			fmt.Printf("    [job %d] %s\n", jobID, job.Status)
			last = job.Status
		}
		if isTerminalStatus(job.Status) {
			return job, nil
		}
		time.Sleep(time.Duration(intervalSec) * time.Second)
	}
}

// printStdoutTail은 Job stdout의 마지막 n줄을 출력한다 (실패 원인 확인용).
func printStdoutTail(client *awx.Client, jobID int, n int) {
	stdout, err := client.GetJobStdout(jobID)
	if err != nil {
		fmt.Printf("    (stdout 조회 실패: %v)\n", err)
		return
	}
	lines := strings.Split(stdout, "\n")
	start := 0
	if len(lines) > n {
		start = len(lines) - n
	}
	fmt.Printf("    --- stdout 마지막 %d줄 (job %d) ---\n", len(lines)-start, jobID)
	for _, l := range lines[start:] {
		fmt.Printf("    %s\n", l)
	}
}
