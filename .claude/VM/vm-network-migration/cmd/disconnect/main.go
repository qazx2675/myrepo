// nm-disconnect — Step 1: 기존 포트그룹 연결 해제
//
// 대상 VM 의 가상 NIC 를 "연결 안 함" 상태로 만듭니다.
// 포트그룹 자체는 다른 VM 도 쓰고 있으므로 절대 삭제하지 않고, 오직 이 VM 의
// 어댑터 설정만 ReconfigVM_Task 로 바꿉니다.
//
// 이미 끊겨 있으면 아무것도 하지 않습니다(멱등).
package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"vm-network-migration/internal/cli"
	"vm-network-migration/internal/state"
	"vm-network-migration/internal/steps"
	"vm-network-migration/internal/vsphere"
)

func main() { os.Exit(run()) }

func run() int {
	fs := flag.NewFlagSet("nm-disconnect", flag.ExitOnError)
	f := cli.Register(fs)
	_ = fs.Parse(os.Args[1:])
	if err := f.Resolve(); err != nil {
		return cli.Usage("%v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), f.Timeout)
	defer cancel()

	fleet, sf, err := steps.Prepare(ctx, f)
	if err != nil {
		return cli.Usage("%v", err)
	}
	defer fleet.Close(context.Background())

	fmt.Printf("[Step 1] 기존 포트그룹 연결 해제 — 대상 %d대 (동시 %d)\n", len(sf.Records), f.Concurrency)

	rep := steps.Run(ctx, "기존 포트그룹 연결 해제", fleet, sf.Records, f.Concurrency,
		func(ctx context.Context, s *vsphere.Session, info *vsphere.VMInfo, rec state.Record) (string, string, error) {
			if f.DryRun {
				return cli.StatusDryRun, fmt.Sprintf("%s 연결 해제 예정 (현재 %s)", rec.NicLabel, rec.OrigPG), nil
			}
			changed, err := s.SetConnected(ctx, info, sf.NicIndex, rec.NicKey, false)
			if err != nil {
				return "", "", err
			}
			if !changed {
				return cli.StatusSkipped, "이미 연결 해제 상태", nil
			}
			return cli.StatusOK, fmt.Sprintf("%s 연결 해제 (이전 %s)", rec.NicLabel, rec.OrigPG), nil
		})

	rep.Print()
	return rep.Finish(f.FailedFile)
}
