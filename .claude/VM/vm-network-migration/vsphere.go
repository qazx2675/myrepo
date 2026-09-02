package main

import (
	"context"
	"fmt"
	"net/url"
	"sort"
	"sync"

	"github.com/vmware/govmomi"
	"github.com/vmware/govmomi/find"
	"github.com/vmware/govmomi/object"
	"github.com/vmware/govmomi/view"
	"github.com/vmware/govmomi/vim25/mo"
	"github.com/vmware/govmomi/vim25/types"
)

// connectVCenter 는 vCenter SOAP 세션을 엽니다.
// 자체서명 인증서 환경이 전제라 insecure 는 기존 govmomi 도구들과 동일하게 true 고정입니다.
func connectVCenter(ctx context.Context, address, user, pass string) (*govmomi.Client, error) {
	u := &url.URL{Scheme: "https", Host: address, Path: "/sdk"}
	u.User = url.UserPassword(user, pass)
	return govmomi.NewClient(ctx, u, true)
}

// dcIndex 는 데이터센터 하나의 조회 결과입니다.
type dcIndex struct {
	Name string
	DC   *object.Datacenter
	VMs  map[string]types.ManagedObjectReference // VM 이름 -> MoRef
	Dups []string                                // 이 데이터센터 안에서 이름이 겹치는 VM
	Net  *networkIndex
}

// vcConn 은 vCenter 한 대의 접속과 그 안의 데이터센터별 색인입니다.
type vcConn struct {
	Addr   string
	Client *govmomi.Client
	DCs    []*dcIndex
	Err    error
}

// Close 는 세션을 정리합니다.
func (v *vcConn) Close(ctx context.Context) {
	if v.Client != nil {
		_ = v.Client.Logout(ctx)
	}
}

// surveyVCenter 는 vCenter 한 대에 접속해서 모든 데이터센터의 VM/네트워크 색인을 만듭니다.
// 데이터센터가 여러 개면 데이터센터끼리도 동시에 조회합니다.
func surveyVCenter(ctx context.Context, address, user, pass string) *vcConn {
	res := &vcConn{Addr: address}

	client, err := connectVCenter(ctx, address, user, pass)
	if err != nil {
		res.Err = fmt.Errorf("접속 실패: %w", err)
		return res
	}
	res.Client = client

	finder := find.NewFinder(client.Client, true)
	dcs, err := finder.DatacenterList(ctx, "*")
	if err != nil {
		res.Err = fmt.Errorf("데이터센터 목록 조회 실패: %w", err)
		return res
	}
	if len(dcs) == 0 {
		res.Err = fmt.Errorf("데이터센터가 없습니다")
		return res
	}

	idx := make([]*dcIndex, len(dcs))
	errs := make([]error, len(dcs))
	var wg sync.WaitGroup
	for i, dc := range dcs {
		wg.Add(1)
		go func(i int, dc *object.Datacenter) {
			defer wg.Done()
			one, err := surveyDatacenter(ctx, client, dc)
			idx[i], errs[i] = one, err
		}(i, dc)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			res.Err = fmt.Errorf("데이터센터 %s 조회 실패: %w", dcs[i].Name(), err)
			return res
		}
	}
	res.DCs = idx
	return res
}

// surveyDatacenter 는 데이터센터 하나의 VM 이름 색인과 네트워크 색인을 만듭니다.
// VM 조회와 네트워크 조회도 동시에 실행합니다.
func surveyDatacenter(ctx context.Context, c *govmomi.Client, dc *object.Datacenter) (*dcIndex, error) {
	out := &dcIndex{Name: dc.Name(), DC: dc}

	var (
		wg            sync.WaitGroup
		vmErr, netErr error
		vms           map[string]types.ManagedObjectReference
		dups          []string
		net           *networkIndex
	)

	wg.Add(2)
	go func() {
		defer wg.Done()
		vms, dups, vmErr = buildVMIndex(ctx, c, dc)
	}()
	go func() {
		defer wg.Done()
		net, netErr = buildNetworkIndex(ctx, c, dc)
	}()
	wg.Wait()

	if vmErr != nil {
		return nil, vmErr
	}
	if netErr != nil {
		return nil, netErr
	}
	out.VMs, out.Dups, out.Net = vms, dups, net
	return out, nil
}

// buildVMIndex 는 데이터센터 전체를 훑어 VM 이름 -> MoRef 색인을 만듭니다.
// 폴더 깊이에 상관없이 찾을 수 있고, Finder 를 고루틴에서 공유하지 않아도 됩니다.
func buildVMIndex(ctx context.Context, c *govmomi.Client, dc *object.Datacenter) (map[string]types.ManagedObjectReference, []string, error) {
	m := view.NewManager(c.Client)
	cv, err := m.CreateContainerView(ctx, dc.Reference(), []string{"VirtualMachine"}, true)
	if err != nil {
		return nil, nil, err
	}
	defer cv.Destroy(ctx)

	var vms []mo.VirtualMachine
	if err := cv.Retrieve(ctx, []string{"VirtualMachine"}, []string{"name"}, &vms); err != nil {
		return nil, nil, err
	}

	idx := make(map[string]types.ManagedObjectReference, len(vms))
	dupSet := make(map[string]bool)
	for _, v := range vms {
		if _, exists := idx[v.Name]; exists {
			dupSet[v.Name] = true
			continue
		}
		idx[v.Name] = v.Self
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

// resolveBacking 은 특정 데이터센터에서 포트그룹 이름으로 NIC 백킹 정보를 만듭니다.
// 고루틴을 띄우기 전에 미리 확인해 두는 용도입니다(결과는 읽기 전용으로 공유).
func resolveBacking(ctx context.Context, c *govmomi.Client, dc *object.Datacenter, pgName string) (types.BaseVirtualDeviceBackingInfo, error) {
	// Finder 는 동시 사용에 안전하지 않으므로 호출할 때마다 새로 만듭니다.
	finder := find.NewFinder(c.Client, true)
	finder.SetDatacenter(dc)

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
