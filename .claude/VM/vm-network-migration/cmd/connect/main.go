// nm-connect — Step 3: 신규 포트그룹 연결
//
// 상태 파일에 기록된 목표 포트그룹으로 NIC 백킹을 교체하고 다시 연결합니다.
// 이미 목표 포트그룹에 연결돼 있으면 아무것도 하지 않습니다(멱등).
//
// 연결 상태는 백업 시점의 원래 값을 따릅니다. 원래 꺼져 있던 NIC 를 이 작업 때문에
// 새로 켜 버리면 이관이 아니라 설정 변경이 되기 때문입니다.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"vm-network-migration/internal/cli"
	"vm-network-migration/internal/color"
	"vm-network-migration/internal/state"
	"vm-network-migration/internal/steps"
	"vm-network-migration/internal/vsphere"
)

func main() { os.Exit(run()) }

func run() int {
	fs := flag.NewFlagSet("nm-connect", flag.ExitOnError)
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

	fmt.Printf("%s 신규 포트그룹 연결 — 대상 %d대 (동시 %d)\n", color.BoldCyan("[Step 3]"), len(sf.Records), f.Concurrency)

	rep := steps.Run(ctx, "신규 포트그룹 연결", fleet, sf.Records, f.Concurrency,
		func(ctx context.Context, s *vsphere.Session, info *vsphere.VMInfo, rec state.Record) (string, string, error) {
			if f.DryRun {
				return cli.StatusDryRun,
					fmt.Sprintf("%s -> %s 연결 예정", rec.OrigPG, rec.TargetPG), nil
			}
			// 원래 연결돼 있던 NIC 만 다시 연결합니다.
			changed, err := s.SetPortgroup(ctx, info, sf.NicIndex, rec.NicKey,
				rec.TargetPG, rec.OrigConnected, rec.OrigStartConnected)
			if err != nil {
				return "", "", err
			}

			// 백킹 교체만으로는 "연결됨" 체크가 켜지지 않는 경우가 있어,
			// 원래 연결돼 있던 NIC 는 연결 상태까지 실제로 확인하고 맞춥니다.
			// 백업 당시 전원이 꺼져 있었으면 OrigConnected 는 항상 false 로
			// 기록되므로, 부팅 시 연결(StartConnected)도 함께 봅니다.
			fixed := false
			if rec.OrigConnected || rec.OrigStartConnected {
				fixed, err = s.EnsureConnected(ctx, info, sf.NicIndex, rec.NicKey)
				if err != nil {
					return "", "", err
				}
			}

			if !changed && !fixed {
				return cli.StatusSkipped, fmt.Sprintf("이미 %s 에 연결됨", rec.TargetPG), nil
			}
			if fixed {
				return cli.StatusOK, fmt.Sprintf("%s -> %s (연결됨 재설정)", rec.OrigPG, rec.TargetPG), nil
			}
			return cli.StatusOK, fmt.Sprintf("%s -> %s", rec.OrigPG, rec.TargetPG), nil
		})

	rep.Print()
	return rep.Finish(f.FailedFile)
}
