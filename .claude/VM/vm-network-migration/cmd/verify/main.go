// nm-verify — Step 4: 연결성 검증
//
// 각 VM 의 NIC 가 (1) 계획된 포트그룹에 붙어 있는지 (2) 연결 상태가 의도대로인지
// API 로 다시 읽어 확인합니다. 변경을 수행한 바이너리가 스스로 "성공"이라고 한 것과
// 별개로, 실제 인벤토리 상태를 새로 조회해서 대조하는 것이 이 단계의 목적입니다.
//
// 전원이 꺼진 VM 은 Connectable.Connected 가 항상 false 이므로, 이 경우엔
// StartConnected(부팅 시 연결)만 확인합니다.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/vmware/govmomi/vim25/types"

	"vm-network-migration/internal/cli"
	"vm-network-migration/internal/color"
	"vm-network-migration/internal/state"
	"vm-network-migration/internal/steps"
	"vm-network-migration/internal/vsphere"
)

func main() { os.Exit(run()) }

func run() int {
	fs := flag.NewFlagSet("nm-verify", flag.ExitOnError)
	f := cli.Register(fs)
	_ = fs.Parse(os.Args[1:])
	if err := f.Resolve(); err != nil {
		return cli.Usage("%v", err)
	}

	// 검증은 "실제로 바뀌었는지"를 확인하는 단계입니다. dry-run 은 아무것도 바꾸지
	// 않으므로 검증할 대상이 없고, 그대로 돌리면 전부 불일치로 나와 오해를 부릅니다.
	if f.DryRun {
		fmt.Println(color.BoldCyan("[Step 4]") + " 연결성 검증 — dry-run 에서는 검증할 변경이 없으므로 건너뜁니다.")
		return cli.ExitOK
	}

	ctx, cancel := context.WithTimeout(context.Background(), f.Timeout)
	defer cancel()

	fleet, sf, err := steps.Prepare(ctx, f)
	if err != nil {
		return cli.Usage("%v", err)
	}
	defer fleet.Close(context.Background())

	fmt.Printf("%s 연결성 검증 — 대상 %d대 (동시 %d)\n", color.BoldCyan("[Step 4]"), len(sf.Records), f.Concurrency)

	rep := steps.Run(ctx, "연결성 검증", fleet, sf.Records, f.Concurrency,
		func(ctx context.Context, s *vsphere.Session, info *vsphere.VMInfo, rec state.Record) (string, string, error) {
			nic, err := s.NIC(ctx, info, sf.NicIndex, rec.NicKey)
			if err != nil {
				return "", "", err
			}
			if nic.Portgroup != rec.TargetPG {
				return "", "", fmt.Errorf("포트그룹 불일치: 기대 %q / 실제 %q", rec.TargetPG, nic.Portgroup)
			}

			// 이름이 맞는 것만으로는 부족합니다. vCenter 는 존재하지 않는 포트그룹
			// 이름도 NIC 설정에 그대로 받아주기 때문에, 그 호스트에 실제로 그
			// 포트그룹이 있는지까지 확인해야 검증이 의미를 갖습니다.
			host, ok := s.HostByRef(info.HostRef)
			if !ok {
				return "", "", fmt.Errorf("VM 이 올라가 있는 ESXi 호스트를 찾을 수 없습니다")
			}
			if !s.HostHasPortgroup(host, rec.TargetPG) {
				return "", "", fmt.Errorf("포트그룹 %q 가 호스트 %s 에 실제로 존재하지 않습니다"+
					"(NIC 설정에만 이름이 적혀 있는 상태)", rec.TargetPG, host.Name)
			}

			// 원래 끊겨 있던 NIC 는 끊긴 채로 남아 있는 것이 정상입니다.
			if !rec.OrigStartConnected {
				return cli.StatusOK,
					fmt.Sprintf("%s (원래 미연결 NIC — 포트그룹만 확인)", rec.TargetPG), nil
			}
			if info.PowerState == types.VirtualMachinePowerStatePoweredOn {
				if !nic.Connected {
					return "", "", fmt.Errorf("%s 에는 붙었으나 연결되지 않음(connected=false)", rec.TargetPG)
				}
				return cli.StatusOK, fmt.Sprintf("%s / connected", rec.TargetPG), nil
			}
			if !nic.StartConnected {
				return "", "", fmt.Errorf("%s 에는 붙었으나 부팅 시 연결이 꺼져 있음", rec.TargetPG)
			}
			return cli.StatusOK,
				fmt.Sprintf("%s / startConnected (전원 %s)", rec.TargetPG, info.PowerState), nil
		})

	rep.Print()
	code := rep.Finish(f.FailedFile)
	if code == cli.ExitOK {
		fmt.Println(color.Cyan("[INFO]") + " 모든 대상 VM 이 계획된 포트그룹에 정상 연결되었습니다.")
	}
	return code
}
