// nm-backup — 사전 상태 백업 (Pre-flight State Persistence)
//
// 대상 VM 의 현재 네트워크 어댑터 상태(포트그룹/연결여부)를 읽어 state_{user}.json 에
// 남깁니다. 이 파일이 있어야 롤백이 가능하므로, 어떤 변경 작업보다 먼저 실행합니다.
//
// 한 건이라도 준비가 안 된 VM(인벤토리에 없음, worklist 행 없음 등)이 있으면
// 아무것도 쓰지 않고 종료 코드 1 로 끝냅니다. 절반만 백업된 상태로 변경을 시작하면
// 나머지 절반은 롤백할 수 없기 때문입니다.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"vm-network-migration/internal/cli"
	"vm-network-migration/internal/config"
	"vm-network-migration/internal/pool"
	"vm-network-migration/internal/state"
	"vm-network-migration/internal/vsphere"
)

func main() { os.Exit(run()) }

func run() int {
	fs := flag.NewFlagSet("nm-backup", flag.ExitOnError)
	f := cli.Register(fs)
	force := fs.Bool("force", false, "상태 파일이 이미 있어도 덮어씁니다")
	_ = fs.Parse(os.Args[1:])
	if err := f.Resolve(); err != nil {
		return cli.Usage("%v", err)
	}

	// 이미 상태 파일이 있는데 덮어쓰면, 1단계까지 끝난 뒤 백업을 다시 돌린 경우
	// "변경된 뒤 상태"가 원본으로 기록되어 롤백이 무의미해집니다.
	if _, err := os.Stat(f.StateFile); err == nil && !*force {
		return cli.Usage("상태 파일 %s 가 이미 있습니다. 이어서 진행하려면 그대로 두고, "+
			"정말 새로 백업하려면 -force 를 주세요", f.StateFile)
	}

	pass, err := config.Password()
	if err != nil {
		return cli.Usage("%v", err)
	}
	vcenters, err := config.LoadVCenters(f.VCenterFile)
	if err != nil {
		return cli.Usage("%v", err)
	}
	vmNames, err := config.LoadVMList(f.VMFile)
	if err != nil {
		return cli.Usage("%v", err)
	}
	worklist, err := config.LoadWorklist(f.WorklistFile)
	if err != nil {
		return cli.Usage("%v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), f.Timeout)
	defer cancel()

	fmt.Printf("[Step 0] 사전 상태 백업 — vCenter %d대 / 대상 VM %d대 (동시 %d)\n",
		len(vcenters), len(vmNames), f.Concurrency)

	fleet, err := vsphere.ConnectFleet(ctx, vcenters, f.ID, pass, f.Concurrency)
	if err != nil {
		return cli.Usage("%v", err)
	}
	defer fleet.Close(context.Background())

	results := make([]cli.Result, len(vmNames))
	records := make([]*state.Record, len(vmNames))

	pool.Run(f.Concurrency, vmNames, func(i int, name string) {
		rec, msg, err := backupOne(ctx, fleet, worklist, name, f.NicIndex)
		if err != nil {
			results[i] = cli.Result{Name: name, Status: cli.StatusFailed, Message: err.Error()}
			return
		}
		records[i] = rec
		results[i] = cli.Result{Name: name, Status: cli.StatusOK, Message: msg}
	})

	rep := &cli.Report{Step: "사전 상태 백업", Results: results}
	rep.Print()

	if len(rep.Failed()) > 0 {
		fmt.Fprintln(os.Stderr, "\n[중단] 백업하지 못한 VM 이 있어 상태 파일을 만들지 않았습니다.")
		fmt.Fprintln(os.Stderr, "        롤백 불가능한 상태로 변경을 시작하지 않기 위한 의도적인 중단입니다.")
		return rep.Finish(f.FailedFile)
	}

	sf := &state.File{
		User:      f.User,
		CreatedAt: time.Now(),
		NicIndex:  f.NicIndex,
	}
	for _, r := range records {
		sf.Records = append(sf.Records, *r)
	}

	// 백업은 vCenter 를 읽기만 하므로 dry-run 이어도 상태 파일은 실제로 씁니다.
	// 이 파일이 없으면 뒤따르는 단계들이 dry-run 조차 할 수 없기 때문입니다.
	// dry-run 이 진짜 상태 파일을 건드리지 않도록, 호출 측(run.sh)이 -state-file 로
	// 별도 경로를 넘겨줍니다.
	if err := state.Save(f.StateFile, sf); err != nil {
		return cli.Usage("상태 파일 저장 실패: %v", err)
	}
	fmt.Printf("\n[INFO] 상태 파일 저장 완료: %s (%d건)\n", f.StateFile, len(sf.Records))
	return rep.Finish(f.FailedFile)
}

// backupOne 은 VM 한 대의 현재 상태와 목표 포트그룹을 확정합니다.
func backupOne(ctx context.Context, fleet *vsphere.Fleet, worklist []config.WorkEntry,
	name string, nicIndex int) (*state.Record, string, error) {

	sess, info, err := fleet.LookupVM(name)
	if err != nil {
		return nil, "", err
	}

	host, ok := sess.HostByRef(info.HostRef)
	if !ok {
		return nil, "", fmt.Errorf("VM 이 올라가 있는 ESXi 호스트를 찾을 수 없습니다")
	}

	// 목표 포트그룹은 "이 VM 이 올라가 있는 BM 호스트"의 worklist 행으로 정합니다.
	target, err := config.TargetForHost(worklist, host.Name)
	if err != nil {
		return nil, "", err
	}

	nic, err := sess.NIC(ctx, info, nicIndex, 0)
	if err != nil {
		return nil, "", err
	}

	rec := &state.Record{
		VMName:             info.Name,
		VMUUID:             info.UUID,
		VCenter:            sess.Addr,
		BMHost:             host.Name,
		NicKey:             nic.Key,
		NicLabel:           nic.Label,
		OrigPG:             nic.Portgroup,
		OrigConnected:      nic.Connected,
		OrigStartConnected: nic.StartConnected,
		TargetPG:           target.PGName,
		TargetVLAN:         target.VlanID,
	}
	msg := fmt.Sprintf("%s: %s -> %s (VLAN %d, host=%s)",
		nic.Label, orDash(nic.Portgroup), target.PGName, target.VlanID, host.Name)
	return rec, msg, nil
}

func orDash(s string) string {
	if s == "" {
		return "(연결없음)"
	}
	return s
}
