package main

import (
	"context"
	"fmt"

	"github.com/vmware/govmomi"
	"github.com/vmware/govmomi/object"
	"github.com/vmware/govmomi/vim25/types"
)

// 결과 상태값
const (
	StatusSuccess = "SUCCESS"
	StatusSkipped = "SKIPPED"
	StatusFailed  = "FAILED"
	StatusDryRun  = "DRY-RUN"
)

// Result 는 VM 한 대의 처리 결과입니다. 리포트 CSV 와 롤백 CSV 의 원본이 됩니다.
type Result struct {
	VCenter    string
	Datacenter string
	VMName     string
	NicKey     int32
	NicName    string
	FromPG     string
	ToPG       string
	Status     string
	Message    string
}

// migrateOptions 는 VM 한 대를 처리할 때 필요한 설정 묶음입니다.
type migrateOptions struct {
	ToPortgroup string
	FromFilter  string // 비어 있지 않으면 이 포트그룹에 붙은 NIC 만 대상
	NicIndex    int    // FromFilter 가 비었을 때 사용할 NIC 순번
	NicKey      int32  // UseNicKey 가 true 면 이 device key 의 NIC 만 대상(롤백 모드용)
	UseNicKey   bool
	Backing     types.BaseVirtualDeviceBackingInfo
	DryRun      bool
}

// migrateVM 은 VM 한 대의 NIC 백킹을 신규 포트그룹으로 교체합니다.
//
// NIC 를 끊었다가 다시 붙이지 않고 Reconfigure 한 번으로 백킹만 바꾸기 때문에
// 게스트 입장에서는 순단으로 끝나며, 중간에 실패해도 NIC 가 끊긴 채로 남지 않습니다.
func migrateVM(ctx context.Context, c *govmomi.Client, ni *networkIndex, ref types.ManagedObjectReference, vmName string, opt migrateOptions) Result {
	res := Result{VMName: vmName, ToPG: opt.ToPortgroup}

	vm := object.NewVirtualMachine(c.Client, ref)

	devices, power, err := fetchDevices(ctx, vm)
	if err != nil {
		res.Status = StatusFailed
		res.Message = fmt.Sprintf("하드웨어 정보 조회 실패: %v", err)
		return res
	}

	cards := ethernetCards(devices)
	if len(cards) == 0 {
		res.Status = StatusFailed
		res.Message = "가상 NIC 가 없습니다"
		return res
	}

	// 어떤 NIC 를 바꿀지 고릅니다.
	var target types.BaseVirtualEthernetCard
	if opt.UseNicKey {
		for _, c := range cards {
			if c.GetVirtualEthernetCard().Key == opt.NicKey {
				target = c
				break
			}
		}
		if target == nil {
			res.Status = StatusFailed
			res.Message = fmt.Sprintf("NIC key=%d 를 찾을 수 없습니다", opt.NicKey)
			return res
		}
	} else if opt.FromFilter != "" {
		for _, c := range cards {
			if ni.portgroupName(c.GetVirtualEthernetCard().Backing) == opt.FromFilter {
				target = c
				break
			}
		}
		if target == nil {
			res.Status = StatusSkipped
			res.Message = fmt.Sprintf("'%s' 에 연결된 NIC 가 없습니다", opt.FromFilter)
			return res
		}
	} else {
		if opt.NicIndex < 0 || opt.NicIndex >= len(cards) {
			res.Status = StatusFailed
			res.Message = fmt.Sprintf("NIC 순번 %d 가 범위를 벗어났습니다(NIC %d개)", opt.NicIndex, len(cards))
			return res
		}
		target = cards[opt.NicIndex]
	}

	card := target.GetVirtualEthernetCard()
	res.NicKey = card.Key
	if card.DeviceInfo != nil {
		res.NicName = card.DeviceInfo.GetDescription().Label
	}
	res.FromPG = ni.portgroupName(card.Backing)

	// 이미 목표 포트그룹이면 건드리지 않습니다(재실행 안전).
	if res.FromPG == opt.ToPortgroup {
		res.Status = StatusSkipped
		res.Message = "이미 목표 포트그룹에 연결되어 있습니다"
		return res
	}

	if opt.DryRun {
		res.Status = StatusDryRun
		res.Message = fmt.Sprintf("변경 예정: %s -> %s (NIC key=%d, 전원=%s)", res.FromPG, opt.ToPortgroup, card.Key, power)
		return res
	}

	// 백킹 교체. 전원이 켜져 있을 때만 Connected 를 만질 수 있습니다.
	card.Backing = opt.Backing
	if card.Connectable == nil {
		card.Connectable = &types.VirtualDeviceConnectInfo{}
	}
	card.Connectable.StartConnected = true
	if power == types.VirtualMachinePowerStatePoweredOn {
		card.Connectable.Connected = true
	}

	spec := types.VirtualMachineConfigSpec{
		DeviceChange: []types.BaseVirtualDeviceConfigSpec{
			&types.VirtualDeviceConfigSpec{
				Operation: types.VirtualDeviceConfigSpecOperationEdit,
				Device:    target.(types.BaseVirtualDevice),
			},
		},
	}

	task, err := vm.Reconfigure(ctx, spec)
	if err != nil {
		res.Status = StatusFailed
		res.Message = fmt.Sprintf("Reconfigure 요청 실패: %v", err)
		return res
	}
	if err := task.Wait(ctx); err != nil {
		res.Status = StatusFailed
		res.Message = fmt.Sprintf("Reconfigure 완료 대기 실패: %v", err)
		return res
	}

	// 검증: 같은 NIC key 를 다시 찾아 백킹이 실제로 목표 포트그룹인지 확인합니다.
	after, powerAfter, err := fetchDevices(ctx, vm)
	if err != nil {
		res.Status = StatusFailed
		res.Message = fmt.Sprintf("변경 후 하드웨어 재조회 실패: %v", err)
		return res
	}
	var verified *types.VirtualEthernetCard
	for _, c := range ethernetCards(after) {
		if c.GetVirtualEthernetCard().Key == card.Key {
			verified = c.GetVirtualEthernetCard()
			break
		}
	}
	if verified == nil {
		res.Status = StatusFailed
		res.Message = fmt.Sprintf("검증 실패: NIC key=%d 를 다시 찾을 수 없습니다", card.Key)
		return res
	}
	nowPG := ni.portgroupName(verified.Backing)
	if nowPG != opt.ToPortgroup {
		res.Status = StatusFailed
		res.Message = fmt.Sprintf("검증 실패: 현재 포트그룹이 '%s' 입니다(기대값 '%s')", nowPG, opt.ToPortgroup)
		return res
	}
	if powerAfter == types.VirtualMachinePowerStatePoweredOn &&
		(verified.Connectable == nil || !verified.Connectable.Connected) {
		res.Status = StatusFailed
		res.Message = "검증 실패: 포트그룹은 바뀌었으나 NIC 가 연결(Connected) 상태가 아닙니다"
		return res
	}

	res.Status = StatusSuccess
	if powerAfter == types.VirtualMachinePowerStatePoweredOn {
		res.Message = fmt.Sprintf("%s -> %s (연결됨)", res.FromPG, opt.ToPortgroup)
	} else {
		res.Message = fmt.Sprintf("%s -> %s (전원 꺼짐: 부팅 시 연결)", res.FromPG, opt.ToPortgroup)
	}
	return res
}
