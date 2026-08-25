package cli

import (
	"fmt"
	"strings"
	"time"

	"awxkit/awx"
)

// IsTerminalStatus는 AWX Job/InventoryUpdate 상태가 더 이상 바뀌지 않는 최종 상태인지 반환한다.
func IsTerminalStatus(status string) bool {
	switch status {
	case "successful", "failed", "error", "canceled":
		return true
	default:
		return false
	}
}

// PollJob은 Job이 최종 상태가 될 때까지 intervalSec 간격으로 상태를 조회한다.
// 스피너 없이 상태가 바뀔 때만 한 줄을 출력한다 (ETX 원격 터미널 환경 고려).
func PollJob(client *awx.Client, jobID int, intervalSec int) (*awx.Job, error) {
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
			fmt.Printf("    [job %d] %s\n", jobID, ColorStatus(job.Status))
			last = job.Status
		}
		if IsTerminalStatus(job.Status) {
			return job, nil
		}
		time.Sleep(time.Duration(intervalSec) * time.Second)
	}
}

// PollInventoryUpdate는 인벤토리 동기화가 최종 상태가 될 때까지 intervalSec 간격으로 상태를 조회한다.
func PollInventoryUpdate(client *awx.Client, updateID int, intervalSec int) (*awx.InventoryUpdate, error) {
	if intervalSec <= 0 {
		intervalSec = 3
	}
	last := ""
	for {
		upd, err := client.GetInventoryUpdate(updateID)
		if err != nil {
			return nil, err
		}
		if upd.Status != last {
			fmt.Printf("    [inventory_update %d] %s\n", updateID, ColorStatus(upd.Status))
			last = upd.Status
		}
		if IsTerminalStatus(upd.Status) {
			return upd, nil
		}
		time.Sleep(time.Duration(intervalSec) * time.Second)
	}
}

// PrintStdoutTail은 Job stdout의 마지막 n줄을 출력한다 (실패 원인 확인용).
func PrintStdoutTail(client *awx.Client, jobID int, n int) {
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
