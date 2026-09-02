// Package cli 는 단계별 바이너리들이 공유하는 플래그/결과 출력/종료 코드 규약입니다.
//
// 모든 바이너리가 같은 플래그 이름과 같은 종료 코드를 쓰게 해서, run.sh 가
// 단계마다 다르게 분기하지 않아도 되도록 합니다.
package cli

import (
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"
)

// DefaultID 는 -id 를 주지 않았을 때 쓰는 vCenter 로그인 계정입니다.
// VM_setup 아래 다른 도구들과 같은 기본값을 씁니다.
const DefaultID = "lscsystems@vsphere.local"

// timeout 은 전체 작업 제한 시간입니다. 대상이 수백 대여도 넉넉한 값이라
// 옵션으로 열지 않고 상수로 둡니다.
const timeout = 30 * time.Minute

// 종료 코드 규약.
//
//	0  전부 성공(또는 이미 원하는 상태라 건드릴 필요 없음)
//	1  일부/전부 실패 — run.sh 가 이걸 보고 롤백을 시작합니다
//	2  설정/입력 오류 — VM 을 하나도 건드리지 않았습니다
const (
	ExitOK     = 0
	ExitFailed = 1
	ExitUsage  = 2
)

// Flags 는 모든 단계 바이너리가 공통으로 받는 옵션입니다.
//
// 파일 경로는 대부분 -user 에서 파생되므로 개별 플래그로 열지 않습니다.
// -state-file 만 예외인데, dry-run 이 실제 상태 파일을 건드리지 않도록
// run.sh 가 임시 경로로 돌려야 하기 때문입니다.
type Flags struct {
	User         string
	ID           string
	VCenterFile  string
	VMFile       string
	WorklistFile string
	StateFile    string
	FailedFile   string
	NicIndex     int
	Concurrency  int
	Timeout      time.Duration
	DryRun       bool
}

// Register 는 공통 플래그를 등록합니다. 반환된 Flags 는 Parse 후에 Resolve 를 불러야 합니다.
func Register(fs *flag.FlagSet) *Flags {
	f := &Flags{}
	fs.StringVar(&f.User, "user", "", "작업 대상 사용자 토큰. 나머지 파일 경로의 기본값을 결정합니다 (필수)")
	fs.StringVar(&f.ID, "id", DefaultID, "vCenter 로그인 계정 ID")
	fs.StringVar(&f.VCenterFile, "vcenter-file", "vcenter.txt", "vCenter 주소 목록 파일 (한 줄에 하나)")
	fs.StringVar(&f.StateFile, "state-file", "", "롤백용 상태 파일 (기본: state_{user}.json)")
	fs.IntVar(&f.NicIndex, "nic-index", 0, "대상 가상 NIC 순번 (0 = 네트워크 어댑터 1)")
	fs.IntVar(&f.Concurrency, "concurrency", 8, "동시에 처리할 VM 수 (vCenter 부하 조절)")
	fs.BoolVar(&f.DryRun, "dry-run", false, "실제 변경 없이 무엇이 바뀔지만 출력")
	return f
}

// Resolve 는 -user 로부터 나머지 파일 경로를 정하고 값을 검증합니다.
func (f *Flags) Resolve() error {
	if f.User == "" {
		return fmt.Errorf("-user 는 필수입니다 (예: -user=hong)")
	}
	if strings.ContainsAny(f.User, `/\ `) {
		return fmt.Errorf("-user 에 경로 구분자나 공백을 쓸 수 없습니다: %q", f.User)
	}
	if strings.TrimSpace(f.ID) == "" {
		return fmt.Errorf("-id 가 비어 있습니다")
	}
	f.VMFile = f.User + ".txt"
	f.WorklistFile = "vswitch_" + f.User + ".txt"
	f.FailedFile = "failed_" + f.User + ".txt"
	f.Timeout = timeout
	if f.StateFile == "" {
		f.StateFile = "state_" + f.User + ".json"
	}
	if f.Concurrency < 1 {
		f.Concurrency = 1
	}
	return nil
}

// 결과 상태값.
const (
	StatusOK      = "성공"
	StatusSkipped = "스킵"
	StatusFailed  = "실패"
	StatusDryRun  = "예정"
)

// Result 는 VM(또는 호스트) 한 건의 처리 결과입니다.
type Result struct {
	Name    string // VM 이름 또는 호스트 이름
	Status  string
	Message string
}

// Report 는 한 단계의 결과 전체입니다.
type Report struct {
	Step    string
	Results []Result
}

// Print 는 건별 결과를 순서대로 출력합니다.
func (r *Report) Print() {
	for _, res := range r.Results {
		fmt.Printf("  [%s] %-40s %s\n", res.Status, res.Name, res.Message)
	}
}

// Failed 는 실패한 항목 이름을 정렬해서 돌려줍니다.
func (r *Report) Failed() []string {
	var out []string
	for _, res := range r.Results {
		if res.Status == StatusFailed {
			out = append(out, res.Name)
		}
	}
	sort.Strings(out)
	return out
}

// Finish 는 요약을 출력하고, 실패가 있으면 실패 목록 파일을 남긴 뒤 종료 코드를 정합니다.
//
// 실패 목록 파일은 run.sh 가 "실패한 VM 만 골라 롤백"할 때 그대로 넘겨받는 입력입니다.
func (r *Report) Finish(failedFile string) int {
	var ok, skip, fail, dry int
	for _, res := range r.Results {
		switch res.Status {
		case StatusOK:
			ok++
		case StatusSkipped:
			skip++
		case StatusFailed:
			fail++
		case StatusDryRun:
			dry++
		}
	}
	fmt.Printf("\n[요약] %s — 전체 %d건 / 성공 %d / 스킵 %d / 실패 %d",
		r.Step, len(r.Results), ok, skip, fail)
	if dry > 0 {
		fmt.Printf(" / 변경예정 %d", dry)
	}
	fmt.Println()

	failed := r.Failed()
	if len(failed) == 0 {
		// 이전 실행의 실패 목록이 남아 있으면 오해를 부르므로 지웁니다.
		_ = os.Remove(failedFile)
		return ExitOK
	}

	body := strings.Join(failed, "\n") + "\n"
	if err := os.WriteFile(failedFile, []byte(body), 0o600); err != nil {
		fmt.Fprintf(os.Stderr, "[경고] 실패 목록 파일(%s) 저장 실패: %v\n", failedFile, err)
	} else {
		fmt.Printf("[INFO] 실패한 VM %d건을 %s 에 기록했습니다.\n", len(failed), failedFile)
	}
	return ExitFailed
}

// Usage 는 설정/입력 오류를 stderr 로 알리고 종료 코드 2 를 돌려줍니다.
func Usage(format string, args ...any) int {
	fmt.Fprintf(os.Stderr, "오류: "+format+"\n", args...)
	return ExitUsage
}
