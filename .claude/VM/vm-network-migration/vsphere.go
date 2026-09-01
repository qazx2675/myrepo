package main

import (
	"context"
	"fmt"
	"net/url"
	"sort"

	"github.com/vmware/govmomi"
	"github.com/vmware/govmomi/find"
	"github.com/vmware/govmomi/object"
	"github.com/vmware/govmomi/view"
	"github.com/vmware/govmomi/vim25/mo"
	"github.com/vmware/govmomi/vim25/types"
)

// connectVCenter 는 vCenter SOAP 세션을 엽니다.
func connectVCenter(ctx context.Context, cfg *Config) (*govmomi.Client, error) {
	u, err := url.Parse(fmt.Sprintf("https://%s/sdk", cfg.Host))
	if err != nil {
		return nil, err
	}
	u.User = url.UserPassword(cfg.User, cfg.Password)
	return govmomi.NewClient(ctx, u, cfg.Insecure)
}

// vmEntry 는 인벤토리에서 찾은 VM 한 대의 요약 정보입니다.
type vmEntry struct {
	Ref        types.ManagedObjectReference
	PowerState types.VirtualMachinePowerState
}

// buildVMIndex 는 데이터센터 전체를 훑어 VM 이름 -> MoRef 색인을 만듭니다.
// 폴더 깊이에 상관없이 찾을 수 있고, Finder 를 고루틴에서 공유하지 않아도 됩니다.
// 같은 이름의 VM 이 두 대 이상이면 어느 쪽인지 특정할 수 없으므로 dup 목록으로 돌려줍니다.
func buildVMIndex(ctx context.Context, c *govmomi.Client, dc *object.Datacenter) (map[string]vmEntry, []string, error) {
	m := view.NewManager(c.Client)
	cv, err := m.CreateContainerView(ctx, dc.Reference(), []string{"VirtualMachine"}, true)
	if err != nil {
		return nil, nil, err
	}
	defer cv.Destroy(ctx)

	var vms []mo.VirtualMachine
	if err := cv.Retrieve(ctx, []string{"VirtualMachine"}, []string{"name", "runtime.powerState"}, &vms); err != nil {
		return nil, nil, err
	}

	idx := make(map[string]vmEntry, len(vms))
	dupSet := make(map[string]bool)
	for _, v := range vms {
		if _, exists := idx[v.Name]; exists {
			dupSet[v.Name] = true
			continue
		}
		idx[v.Name] = vmEntry{Ref: v.Self, PowerState: v.Runtime.PowerState}
	}
	var dups []string
	for n := range dupSet {
		dups = append(dups, n)
	}
	sort.Strings(dups)
	return idx, dups, nil
}

// networkIndex 는 NIC 백킹 정보를 사람이 읽는 포트그룹 이름으로 되돌리기 위한 색인입니다.
type networkIndex struct {
	dvpgKeyToName map[string]string
}

// buildNetworkIndex 는 분산 포트그룹(DVPG)의 key -> name 대응표를 만듭니다.
// 표준 vSwitch 포트그룹은 백킹에 이름이 그대로 들어 있어 색인이 필요 없습니다.
func buildNetworkIndex(ctx context.Context, c *govmomi.Client, dc *object.Datacenter) (*networkIndex, error) {
	ni := &networkIndex{dvpgKeyToName: map[string]string{}}

	m := view.NewManager(c.Client)
	cv, err := m.CreateContainerView(ctx, dc.Reference(), []string{"DistributedVirtualPortgroup"}, true)
	if err != nil {
		return nil, err
	}
	defer cv.Destroy(ctx)

	var pgs []mo.DistributedVirtualPortgroup
	if err := cv.Retrieve(ctx, []string{"DistributedVirtualPortgroup"}, []string{"name", "key"}, &pgs); err != nil {
		return nil, err
	}
	for _, pg := range pgs {
		ni.dvpgKeyToName[pg.Key] = pg.Name
	}
	return ni, nil
}

// portgroupName 은 NIC 백킹에서 현재 연결된 포트그룹 이름을 뽑아냅니다.
// 표준 vSwitch / 분산 스위치(DVS) / NSX opaque network 를 모두 처리합니다.
func (ni *networkIndex) portgroupName(b types.BaseVirtualDeviceBackingInfo) string {
	switch bb := b.(type) {
	case *types.VirtualEthernetCardNetworkBackingInfo:
		return bb.DeviceName
	case *types.VirtualEthernetCardDistributedVirtualPortBackingInfo:
		if n, ok := ni.dvpgKeyToName[bb.Port.PortgroupKey]; ok {
			return n
		}
		return "dvportgroup:" + bb.Port.PortgroupKey
	case *types.VirtualEthernetCardOpaqueNetworkBackingInfo:
		return "opaque:" + bb.OpaqueNetworkId
	case nil:
		return ""
	}
	return "unknown"
}

// resolveBacking 은 포트그룹 이름으로 NIC 에 붙일 백킹 정보를 만듭니다.
// 고루틴을 띄우기 전에 한 번만 호출해서 결과를 공유합니다(읽기 전용).
func resolveBacking(ctx context.Context, finder *find.Finder, pgName string) (types.BaseVirtualDeviceBackingInfo, error) {
	net, err := finder.Network(ctx, pgName)
	if err != nil {
		return nil, fmt.Errorf("포트그룹 '%s' 조회 실패: %w", pgName, err)
	}
	backing, err := net.EthernetCardBackingInfo(ctx)
	if err != nil {
		return nil, fmt.Errorf("포트그룹 '%s' 백킹 정보 생성 실패: %w", pgName, err)
	}
	return backing, nil
}

// ethernetCards 는 VM 의 가상 NIC 목록을 device key 오름차순으로 돌려줍니다.
// 순서를 고정해야 -nic-index 와 검증 단계가 같은 NIC 를 가리킵니다.
func ethernetCards(devices object.VirtualDeviceList) []types.BaseVirtualEthernetCard {
	var cards []types.BaseVirtualEthernetCard
	for _, d := range devices {
		if nic, ok := d.(types.BaseVirtualEthernetCard); ok {
			cards = append(cards, nic)
		}
	}
	sort.Slice(cards, func(i, j int) bool {
		return cards[i].GetVirtualEthernetCard().Key < cards[j].GetVirtualEthernetCard().Key
	})
	return cards
}

// fetchDevices 는 VM 의 현재 하드웨어 목록과 전원 상태를 다시 읽어옵니다.
func fetchDevices(ctx context.Context, vm *object.VirtualMachine) (object.VirtualDeviceList, types.VirtualMachinePowerState, error) {
	var mvm mo.VirtualMachine
	err := vm.Properties(ctx, vm.Reference(), []string{"config.hardware.device", "runtime.powerState"}, &mvm)
	if err != nil {
		return nil, "", err
	}
	if mvm.Config == nil {
		return nil, "", fmt.Errorf("VM 설정 정보를 읽을 수 없습니다(config 가 비어 있음)")
	}
	return object.VirtualDeviceList(mvm.Config.Hardware.Device), mvm.Runtime.PowerState, nil
}
