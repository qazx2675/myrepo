// nm-pgcreate — Step 2: 신규 포트그룹 생성
//
// vswitch_{user}.txt 의 (BM호스트, 포트그룹, VLAN) 조합을 읽어, 각 ESXi 호스트의
// 표준 vSwitch 에 포트그룹을 만듭니다. 이미 있으면 성공으로 간주합니다(멱등).
//
// 이 단계는 state 파일을 보지 않고 worklist 만 봅니다. 포트그룹 생성 자체는
// 호스트 단위 작업이고, 어느 VM 이 그 포트그룹을 쓸지와 무관하기 때문입니다.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"vm-network-migration/internal/cli"
	"vm-network-migration/internal/config"
	"vm-network-migration/internal/pool"
	"vm-network-migration/internal/vsphere"
)

func main() { os.Exit(run()) }

func run() int {
	fs := flag.NewFlagSet("nm-pgcreate", flag.ExitOnError)
	f := cli.Register(fs)
	vswitch := fs.String("target-vswitch", "vSwitch0", "포트그룹을 만들 대상 표준 가상 스위치")
	_ = fs.Parse(os.Args[1:])

	if f.ShowVersion {
		fmt.Println("nm-pgcreate", cli.Version)
		return cli.ExitOK
	}
	if err := f.Resolve(); err != nil {
		return cli.Usage("%v", err)
	}

	user, pass, err := config.Credentials()
	if err != nil {
		return cli.Usage("%v", err)
	}
	vcenters, err := config.LoadVCenters(f.VCenterFile)
	if err != nil {
		return cli.Usage("%v", err)
	}
	worklist, err := config.LoadWorklist(f.WorklistFile)
	if err != nil {
		return cli.Usage("%v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), f.Timeout)
	defer cancel()

	fmt.Printf("[Step 2] 신규 포트그룹 생성 — 항목 %d건 / vSwitch=%s (동시 %d)\n",
		len(worklist), *vswitch, f.Concurrency)

	fleet, err := vsphere.ConnectFleet(ctx, vcenters, user, pass, f.Concurrency)
	if err != nil {
		return cli.Usage("%v", err)
	}
	defer fleet.Close(context.Background())

	results := make([]cli.Result, len(worklist))

	pool.Run(f.Concurrency, worklist, func(i int, e config.WorkEntry) {
		label := fmt.Sprintf("%s / %s", e.BMHost, e.PGName)

		sess, host, err := lookupHost(fleet, e.BMHost)
		if err != nil {
			results[i] = cli.Result{Name: label, Status: cli.StatusFailed, Message: err.Error()}
			return
		}
		if f.DryRun {
			results[i] = cli.Result{Name: label, Status: cli.StatusDryRun,
				Message: fmt.Sprintf("%s 에 VLAN %d 로 생성 예정", *vswitch, e.VlanID)}
			return
		}
		created, err := sess.AddPortGroup(ctx, host, e.PGName, e.VlanID, *vswitch)
		switch {
		case err != nil:
			results[i] = cli.Result{Name: label, Status: cli.StatusFailed, Message: err.Error()}
		case created:
			results[i] = cli.Result{Name: label, Status: cli.StatusOK,
				Message: fmt.Sprintf("VLAN %d 생성 완료", e.VlanID)}
		default:
			results[i] = cli.Result{Name: label, Status: cli.StatusSkipped, Message: "이미 존재"}
		}
	})

	rep := &cli.Report{Step: "신규 포트그룹 생성", Results: results}
	rep.Print()
	return rep.Finish(f.FailedFile)
}

// lookupHost 는 여러 vCenter 중 해당 ESXi 호스트를 가진 곳을 찾습니다.
func lookupHost(fleet *vsphere.Fleet, name string) (*vsphere.Session, *vsphere.HostInfo, error) {
	var (
		hitS []*vsphere.Session
		hitH []*vsphere.HostInfo
	)
	for _, s := range fleet.Sessions {
		if h, ok := s.HostByName(name); ok {
			hitS = append(hitS, s)
			hitH = append(hitH, h)
		}
	}
	switch len(hitH) {
	case 0:
		return nil, nil, fmt.Errorf("ESXi 호스트 %q 를 어느 vCenter 에서도 찾을 수 없습니다", name)
	case 1:
		return hitS[0], hitH[0], nil
	default:
		return nil, nil, fmt.Errorf("ESXi 호스트 %q 가 여러 vCenter 에 등록되어 있습니다", name)
	}
}
