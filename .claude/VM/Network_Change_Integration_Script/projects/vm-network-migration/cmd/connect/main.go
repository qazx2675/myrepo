// nm-connect — Step 3: 신규 포트그룹 연결
//
// 상태 파일에 기록된 목표 포트그룹으로 NIC 백킹을 교체하고, vSphere 편집
// 설정 화면에서 수동으로 하는 것과 동일하게 "연결됨" 과 "전원을 켤 때 연결"
// 을 모두 체크한 상태로 맞춥니다(전원이 꺼진 VM 은 "연결됨" 이 항상 false 라
// "전원을 켤 때 연결" 만 확인합니다). 이미 그 상태면 아무것도 하지 않습니다(멱등).
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
			// 신규 포트그룹으로 옮긴 NIC 는 항상 "연결됨" + "전원을 켤 때 연결"
			// 을 켠 상태로 맞춥니다. 원래 꺼져 있던 NIC 라도 이관 후에는 새
			// 네트워크로 정상 통신해야 하므로, 백업 시점 값(OrigConnected 등)
			// 을 그대로 따르지 않고 vSphere 편집 설정에서 수동으로 체크하는
			// 것과 동일한 목표 상태를 씁니다.
			changed, err := s.SetPortgroup(ctx, info, sf.NicIndex, rec.NicKey,
				rec.TargetPG, true, true)
			if err != nil {
				return "", "", err
			}

			// 백킹 교체와 연결 상태 변경을 한 Reconfigure 로 같이 보내면
			// vCenter 가 백킹만 반영하고 "연결됨"/"전원을 켤 때 연결" 은 반영을
			// 놓치는 경우가 있어, 실제로 반영됐는지 API 로 다시 읽어 확인하고
			// 아니면 연결 상태만 따로 한 번 더 보냅니다.
			fixed, err := s.EnsureConnectState(ctx, info, sf.NicIndex, rec.NicKey, true, true)
			if err != nil {
				return "", "", err
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
