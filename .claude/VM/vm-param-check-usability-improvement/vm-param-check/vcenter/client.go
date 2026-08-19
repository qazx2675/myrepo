// Package vcenter는 govmomi 연결과 VM/호스트 벌크 조회를 담당한다.
// power-policy 프로젝트에서 검증한 "ContainerView + PropertyCollector 벌크 조회"
// 패턴을 그대로 재사용해서, VM이 많아도 호스트당 API 왕복이 늘어나지 않게 한다.
package vcenter

import (
	"context"
	"fmt"
	"net/url"
	"sync"

	"github.com/vmware/govmomi"
	"github.com/vmware/govmomi/property"
	"github.com/vmware/govmomi/view"
	"github.com/vmware/govmomi/vim25/mo"
	"github.com/vmware/govmomi/vim25/types"

	"vm-param-check/model"
)

// Connect는 vCenter에 로그인한다. insecure(자체서명 인증서 허용)는 기존 govmomi 도구들과 동일하게 true 고정.
func Connect(ctx context.Context, address, user, pass string) (*govmomi.Client, error) {
	u := &url.URL{Scheme: "https", Host: address, Path: "/sdk"}
	u.User = url.UserPassword(user, pass)
	return govmomi.NewClient(ctx, u, true)
}

// hostPowerInfo는 ESXi 호스트 1대의 이름 + 현재 전원 정책을 담는다.
type hostPowerInfo struct {
	Name      string
	ShortName string // "static"|"dynamic"|"low"|"custom" — 실제 판정은 이 값으로 해야 함
}

// powerPolicyDisplayName은 ShortName -> 사람이 읽는 이름 매핑.
// CurrentPolicy.Name은 지역화 리소스 키("PowerPolicy.static.name")를 그대로 반환하고
// 실제 번역된 문자열이 아니라서(실측 확인됨), CSV/콘솔 표시용으로 직접 매핑해서 쓴다.
var powerPolicyDisplayName = map[string]string{
	"static":  "High Performance",
	"dynamic": "Balanced",
	"low":     "Low Power",
	"custom":  "Custom",
}

func displayPowerPolicy(shortName string) string {
	if name, ok := powerPolicyDisplayName[shortName]; ok {
		return name
	}
	return shortName
}

// fetchHostPower는 전체 ESXi 호스트의 이름+전원정책을 딱 1번의 벌크 호출로 가져와
// host moref 기준 맵으로 반환한다. (power-policy 프로젝트와 동일한 벌크조회 패턴)
//
// 이전에는 configManager.powerSystem을 통해 HostPowerSystem 객체를 별도로 벌크
// 조회했는데, vcsim(govmomi 시뮬레이터)에는 이 객체가 실제로 등록돼 있지 않아
// "ManagedObjectNotFound" 계열 오류로 실패한다(실 vCenter에서는 문제 없었음, vcsim
// 테스트 중 확인됨). HostSystem 자체의 config.powerSystemInfo 프로퍼티로 읽으면
// 실 vCenter/vcsim 양쪽에서 동일하게 동작하고, 벌크 호출도 1번으로 줄어든다.
func fetchHostPower(ctx context.Context, client *govmomi.Client) (map[types.ManagedObjectReference]hostPowerInfo, error) {
	m := view.NewManager(client.Client)
	cv, err := m.CreateContainerView(ctx, client.ServiceContent.RootFolder, []string{"HostSystem"}, true)
	if err != nil {
		return nil, fmt.Errorf("호스트 ContainerView 생성 실패: %w", err)
	}
	defer cv.Destroy(ctx)

	var hosts []mo.HostSystem
	if err := cv.Retrieve(ctx, []string{"HostSystem"}, []string{"name", "config.powerSystemInfo"}, &hosts); err != nil {
		return nil, fmt.Errorf("호스트 벌크 조회 실패: %w", err)
	}

	result := map[types.ManagedObjectReference]hostPowerInfo{}
	for _, h := range hosts {
		if h.Config == nil {
			continue
		}
		shortName := h.Config.PowerSystemInfo.CurrentPolicy.ShortName
		if shortName == "" {
			continue
		}
		result[h.Self] = hostPowerInfo{Name: h.Name, ShortName: shortName}
	}
	return result, nil
}

// fetchDVPortgroupNames는 분산 포트그룹(key -> name) 맵을 만든다.
// vSphere에서 DVPortgroup의 moref Value가 곧 PortgroupKey와 같다는 관례에 의존한다
// (실측 환경에서 분산 스위치를 안 쓰면 이 맵은 비어 있어도 무방 — 표준 vSwitch 포트그룹은
// VirtualEthernetCardNetworkBackingInfo.DeviceName으로 바로 이름이 나온다).
func fetchDVPortgroupNames(ctx context.Context, client *govmomi.Client) (map[string]string, error) {
	m := view.NewManager(client.Client)
	cv, err := m.CreateContainerView(ctx, client.ServiceContent.RootFolder, []string{"DistributedVirtualPortgroup"}, true)
	if err != nil {
		return nil, fmt.Errorf("DVPortgroup ContainerView 생성 실패: %w", err)
	}
	defer cv.Destroy(ctx)

	var pgs []mo.DistributedVirtualPortgroup
	if err := cv.Retrieve(ctx, []string{"DistributedVirtualPortgroup"}, []string{"name"}, &pgs); err != nil {
		return nil, fmt.Errorf("DVPortgroup 벌크 조회 실패: %w", err)
	}

	result := map[string]string{}
	for _, pg := range pgs {
		result[pg.Self.Value] = pg.Name
	}
	return result, nil
}

// vmProps는 체크에 필요한 VM 속성 목록이다. config.hardware/extraConfig처럼 VM 1대당
// 크기가 큰 속성이 섞여 있어서, 대상이 정해져 있을 때 인벤토리 전체에 대해 이걸 받아오면
// 대상 대수와 무관하게 인벤토리 규모에 비례해 느려진다 (retrieveVMProps 참고).
var vmProps = []string{
	"name",
	"guest.hostName",
	"config.hardware",
	"config.extraConfig",
	"config.memoryReservationLockedToMax",
	"config.cpuAllocation",
	"config.memoryAllocation",
	"config.numaInfo",
	"runtime.host",
	"runtime.powerState",
	"parent", // VM이 속한 인벤토리 폴더 — 폴더명 기반 스펙 자동매칭에서 쓴다
}

// fetchFolderNames는 VM들의 부모 폴더 이름을 한 번의 벌크 호출로 가져온다.
// VM의 parent가 Folder가 아닌 경우(vApp 소속 등)는 폴더명을 알 수 없으므로 그냥 건너뛴다.
func fetchFolderNames(ctx context.Context, client *govmomi.Client, vms []mo.VirtualMachine) (map[types.ManagedObjectReference]string, error) {
	seen := map[types.ManagedObjectReference]bool{}
	var refs []types.ManagedObjectReference
	for _, vm := range vms {
		if vm.Parent == nil || vm.Parent.Type != "Folder" || seen[*vm.Parent] {
			continue
		}
		seen[*vm.Parent] = true
		refs = append(refs, *vm.Parent)
	}
	if len(refs) == 0 {
		return map[types.ManagedObjectReference]string{}, nil
	}

	var folders []mo.Folder
	if err := property.DefaultCollector(client.Client).Retrieve(ctx, refs, []string{"name"}, &folders); err != nil {
		return nil, fmt.Errorf("VM 폴더 이름 조회 실패: %w", err)
	}

	result := map[types.ManagedObjectReference]string{}
	for _, f := range folders {
		result[f.Self] = f.Name
	}
	return result, nil
}

// retrieveVMProps는 체크 대상 VM의 속성을 조회한다.
//
// targetNames가 nil인 전체 순회 모드에서는 어차피 전부 필요하므로 기존처럼 1회 벌크조회한다.
// targetNames가 있는 지정 대상 모드에서는 2단계로 나눈다:
//
//	1차: 이름 계열 속성(name, guest.hostName)만 가볍게 벌크조회해서 대상 VM의 moref를 추린다.
//	2차: 그 moref들에 대해서만 PropertyCollector로 무거운 속성(vmProps)을 조회한다.
//
// 이렇게 나누지 않고 인벤토리 전체를 vmProps로 받은 뒤 클라이언트에서 걸러내면, 대상이
// 2대뿐이어도 인벤토리에 있는 모든 VM의 하드웨어/ExtraConfig를 전부 전송받게 된다.
func retrieveVMProps(ctx context.Context, client *govmomi.Client, targetNames map[string]bool) ([]mo.VirtualMachine, error) {
	m := view.NewManager(client.Client)
	cv, err := m.CreateContainerView(ctx, client.ServiceContent.RootFolder, []string{"VirtualMachine"}, true)
	if err != nil {
		return nil, fmt.Errorf("VM ContainerView 생성 실패: %w", err)
	}
	defer cv.Destroy(ctx)

	if targetNames == nil {
		var vms []mo.VirtualMachine
		if err := cv.Retrieve(ctx, []string{"VirtualMachine"}, vmProps, &vms); err != nil {
			return nil, fmt.Errorf("VM 벌크 조회 실패: %w", err)
		}
		return vms, nil
	}

	var index []mo.VirtualMachine
	if err := cv.Retrieve(ctx, []string{"VirtualMachine"}, []string{"name", "guest.hostName"}, &index); err != nil {
		return nil, fmt.Errorf("VM 이름 목록 조회 실패: %w", err)
	}

	var refs []types.ManagedObjectReference
	for _, vm := range index {
		guestHostName := ""
		if vm.Guest != nil {
			guestHostName = vm.Guest.HostName
		}
		if targetNames[vm.Name] || targetNames[guestHostName] {
			refs = append(refs, vm.Self)
		}
	}
	// 이 vCenter에 대상 VM이 하나도 없는 건 정상 상황이다(대상이 다른 vCenter에 있음).
	// PropertyCollector.Retrieve는 빈 목록을 에러로 처리하므로 여기서 먼저 빠져나간다.
	if len(refs) == 0 {
		return nil, nil
	}

	var vms []mo.VirtualMachine
	if err := property.DefaultCollector(client.Client).Retrieve(ctx, refs, vmProps, &vms); err != nil {
		return nil, fmt.Errorf("VM 속성 조회 실패: %w", err)
	}
	return vms, nil
}

// FetchVMs는 대상 vCenter의 VM 전체(또는 targetNames로 제한한 VM만)를 조회해서
// model.VMInfo 슬라이스로 변환한다.
// targetNames가 nil이면 인벤토리 전체(전체 순회 모드), non-nil이면 그 이름 집합만 포함(단일/지정 대상 모드).
//
// ContainerView는 client.ServiceContent.RootFolder(vCenter 최상위 — 모든 Datacenter의
// 부모)를 recursive=true로 순회하므로, VM/HostSystem이 어느 Datacenter에 속해 있든
// 상관없이 전부 조회된다 (Datacenter로 범위를 좁히지 않는 govmomi 표준 패턴).
func FetchVMs(ctx context.Context, client *govmomi.Client, vcenterAddr string, targetNames map[string]bool) ([]model.VMInfo, error) {
	// 호스트 전원정책과 분산포트그룹 이름은 서로 무관한 벌크조회라 동시에 실행한다.
	var hostPower map[types.ManagedObjectReference]hostPowerInfo
	var dvPortgroups map[string]string
	var hpErr, dvErr error
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		hostPower, hpErr = fetchHostPower(ctx, client)
	}()
	go func() {
		defer wg.Done()
		dvPortgroups, dvErr = fetchDVPortgroupNames(ctx, client)
	}()
	wg.Wait()
	if hpErr != nil {
		return nil, hpErr
	}
	if dvErr != nil {
		return nil, dvErr
	}

	vms, err := retrieveVMProps(ctx, client, targetNames)
	if err != nil {
		return nil, err
	}

	folderNames, err := fetchFolderNames(ctx, client, vms)
	if err != nil {
		return nil, err
	}

	var result []model.VMInfo
	for _, vm := range vms {
		if vm.Config == nil {
			continue // 템플릿/일부 특수 상태 등 config 없는 항목은 스킵
		}
		guestHostName := ""
		if vm.Guest != nil {
			guestHostName = vm.Guest.HostName
		}

		info := model.VMInfo{
			Name:      vm.Name,
			VCenter:   vcenterAddr,
			Ref:       vm.Self,
			PoweredOn: vm.Runtime.PowerState == types.VirtualMachinePowerStatePoweredOn,
		}

		if vm.Parent != nil {
			info.Folder = folderNames[*vm.Parent]
		}

		// VMware Tools가 없거나 hostname을 보고하지 않으면 guest.hostName 자체가 nil이라
		// VM 이름으로 폴백한다 (중첩 가상화 테스트 환경처럼 Tools 미설치 VM에서 발생).
		if guestHostName != "" {
			info.Hostname = guestHostName
			info.HostnameSource = "guest.hostName"
		} else {
			info.Hostname = vm.Name
			info.HostnameSource = "config.name(fallback)"
		}

		if vm.Config.Hardware.NumCPU != 0 {
			info.NumCPU = vm.Config.Hardware.NumCPU
		}
		info.NumCoresPerSocket = vm.Config.Hardware.NumCoresPerSocket
		info.MemoryMB = vm.Config.Hardware.MemoryMB

		info.ExtraConfig = map[string]string{}
		for _, ov := range vm.Config.ExtraConfig {
			if opt, ok := ov.(*types.OptionValue); ok {
				if s, ok := opt.Value.(string); ok {
					info.ExtraConfig[opt.Key] = s
				} else {
					info.ExtraConfig[opt.Key] = fmt.Sprintf("%v", opt.Value)
				}
			}
		}

		info.MemoryReservationLockedToMax = vm.Config.MemoryReservationLockedToMax

		if vm.Config.NumaInfo != nil {
			info.NumaCoresPerNode = vm.Config.NumaInfo.CoresPerNumaNode
		}

		if vm.Config.CpuAllocation != nil {
			if vm.Config.CpuAllocation.Shares != nil {
				info.CPUSharesLevel = string(vm.Config.CpuAllocation.Shares.Level)
				info.CPUShares = vm.Config.CpuAllocation.Shares.Shares
			}
		}
		if vm.Config.MemoryAllocation != nil {
			if vm.Config.MemoryAllocation.Shares != nil {
				info.MemorySharesLevel = string(vm.Config.MemoryAllocation.Shares.Level)
				info.MemoryShares = vm.Config.MemoryAllocation.Shares.Shares
			}
		}

		var diskKB float64
		for _, dev := range vm.Config.Hardware.Device {
			switch d := dev.(type) {
			case *types.VirtualDisk:
				diskKB += float64(d.CapacityInKB)
			default:
				if nic, ok := dev.(types.BaseVirtualEthernetCard); ok {
					card := nic.GetVirtualEthernetCard()
					connected := card.Connectable != nil && card.Connectable.Connected
					info.Networks = append(info.Networks, model.NetworkAdapter{
						Portgroup: resolvePortgroupName(card.Backing, dvPortgroups),
						Connected: connected,
					})
				}
			}
		}
		info.DiskGB = diskKB / 1024 / 1024

		if vm.Runtime.Host != nil {
			if hp, ok := hostPower[*vm.Runtime.Host]; ok {
				info.HostName = hp.Name
				info.HostPowerPolicy = displayPowerPolicy(hp.ShortName)
			}
		}

		result = append(result, info)
	}

	return result, nil
}

// resolvePortgroupName은 네트워크 어댑터의 backing 정보에서 포트그룹 이름을 뽑아낸다.
// 표준 vSwitch면 DeviceName이 바로 포트그룹 이름이고, 분산 스위치면 PortgroupKey를
// dvPortgroups 맵으로 이름 변환한다(맵에 없으면 key를 그대로 노출해서 최소한 추적은 가능하게 함).
func resolvePortgroupName(backing types.BaseVirtualDeviceBackingInfo, dvPortgroups map[string]string) string {
	switch b := backing.(type) {
	case *types.VirtualEthernetCardNetworkBackingInfo:
		return b.DeviceName
	case *types.VirtualEthernetCardDistributedVirtualPortBackingInfo:
		if b.Port.PortgroupKey != "" {
			if name, ok := dvPortgroups[b.Port.PortgroupKey]; ok {
				return name
			}
			return fmt.Sprintf("(분산포트그룹, key=%s, 이름 미해석)", b.Port.PortgroupKey)
		}
		return "(분산포트그룹, key 없음)"
	default:
		return "(알 수 없는 네트워크 backing 타입)"
	}
}
