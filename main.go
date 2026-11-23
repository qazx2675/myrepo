// gpu_install - list 의 GPU 서버에 NVIDIA 드라이버(.run)와 DCGM 4 를 설치한다 (PLAN.md 참고).
package main

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// errHostsFailed 는 실패한 호스트가 있을 때 돌려준다(목록은 이미 출력했으므로 종료코드 1 만 낸다).
var errHostsFailed = errors.New("실패한 호스트가 있습니다")

func main() {
	if err := run(os.Args[1:]); err != nil {
		if errors.Is(err, errHostsFailed) {
			os.Exit(1)
		}
		fmt.Fprintln(os.Stderr, "오류:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	cfg, err := ParseArgs(args)
	if err != nil {
		return err
	}
	entries, err := LoadList(cfg.ListFile)
	if err != nil {
		return err
	}
	in := bufio.NewReader(os.Stdin)
	if err := ResolveVersions(cfg.AutofsPath, entries, in, os.Stdout); err != nil {
		return err
	}
	if !ConfirmStart(in, os.Stdout, entries) {
		fmt.Println("취소했습니다.")
		return nil
	}
	if cfg.MountUser == "" {
		fmt.Println("MOUNT_USER 가 비어 있어 모든 서버를 scp 방식으로 처리합니다.")
	}
	fmt.Printf("현황 수집 중 (%d대)...\n", len(entries))
	sts, err := Survey(cfg, entries)
	if err != nil {
		return err
	}
	ts := BuildTargets(entries, sts)
	PrintStatus(os.Stdout, ts)
	if err := AskReinstall(in, os.Stdout, ts); err != nil {
		return err
	}
	MarkDcgmRepair(os.Stdout, ts)
	cfg.LogDir = filepath.Join("logs", time.Now().Format("20060102_150405"))
	if err := os.MkdirAll(cfg.LogDir, 0o755); err != nil {
		return err
	}
	fmt.Printf("호스트별 로그: %s\n", cfg.LogDir)
	RunParallel(ts, cfg.Parallel, func(t *Target) { installHost(cfg, t) })
	fmt.Println()
	PrintResults(os.Stdout, ts)
	fs := Failed(ts)
	if err := WriteFailLists(os.Stdout, fs); err != nil {
		return err
	}
	if len(fs) > 0 {
		return errHostsFailed
	}
	return nil
}
