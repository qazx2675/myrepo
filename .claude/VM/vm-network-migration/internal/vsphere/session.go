// Package vsphere 는 vCenter 접속과 인벤토리 색인, VM 가상 NIC 조작을 담당합니다.
//
// 여러 vCenter 를 동시에 다룰 수 있고(Fleet), VM 이 어느 vCenter 에 있는지는
// 이름/UUID 색인으로 자동 판별합니다.
package vsphere

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"github.com/vmware/govmomi"
	"github.com/vmware/govmomi/view"
	"github.com/vmware/govmomi/vim25/mo"
	"github.com/vmware/govmomi/vim25/types"

	"vm-network-migration/internal/pool"
)

// VMInfo 는 색인에 담긴 VM 한 대의 위치 정보입니다.
type VMInfo struct {
	Name       string
	UUID       string
	Ref        types.ManagedObjectReference
	HostRef    *types.ManagedObjectReference
	PowerState types.VirtualMachinePowerState
}

// HostInfo 는 ESXi 호스트 한 대입니다. 포트그룹 생성에 networkSystem 이 필요합니다.
type HostInfo struct {
	Name          string
	Ref           types.ManagedObjectReference
	NetworkSystem *types.ManagedObjectReference
	Networks      []types.ManagedObjectReference // 이 호스트가 볼 수 있는 네트워크(포트그룹)
}

// Session 은 vCenter 한 대와의 연결 + 그 vCenter 의 인벤토리 색인입니다.
type Session struct {
	Addr   string
	Client *govmomi.Client

	vmByName map[string]*VMInfo // key: 소문자 VM 이름
	vmByUUID map[string]*VMInfo
	hosts    map[types.ManagedObjectReference]*HostInfo
	hostByNm map[string]*HostInfo // key: 소문자 호스트 이름
	netName  map[types.ManagedObjectReference]string
}

// Connect 는 vCenter 에 접속하고 VM/호스트/네트워크 색인을 한 번에 만듭니다.
//
// 인증서 검증은 하지 않습니다(insecure). 폐쇄망 자체서명 인증서 환경을 전제로 합니다.
func Connect(ctx context.Context, addr, user, pass string) (*Session, error) {
	u := &url.URL{Scheme: "https", Host: addr, Path: "/sdk"}
	u.User = url.UserPassword(user, pass)

	c, err := govmomi.NewClient(ctx, u, true)
	if err != nil {
		return nil, fmt.Errorf("%s 접속 실패: %w", addr, err)
	}

	s := &Session{
		Addr:     addr,
		Client:   c,
		vmByName: map[string]*VMInfo{},
		vmByUUID: map[string]*VMInfo{},
		hosts:    map[types.ManagedObjectReference]*HostInfo{},
		hostByNm: map[string]*HostInfo{},
		netName:  map[types.ManagedObjectReference]string{},
	}
	if err := s.index(ctx); err != nil {
		c.Logout(ctx)
		return nil, fmt.Errorf("%s 인벤토리 조회 실패: %w", addr, err)
	}
	return s, nil
}

// Close 는 vCenter 세션을 정리합니다.
func (s *Session) Close(ctx context.Context) {
	if s.Client != nil {
		_ = s.Client.Logout(ctx)
	}
}

// index 는 VM / HostSystem / Network 를 각각 한 번의 일괄 조회로 읽어 색인합니다.
// VM 마다 개별 조회하면 대상이 많을수록 왕복 횟수가 선형으로 늘어나기 때문입니다.
func (s *Session) index(ctx context.Context) error {
	m := view.NewManager(s.Client.Client)
	root := s.Client.ServiceContent.RootFolder

	v, err := m.CreateContainerView(ctx, root, []string{"VirtualMachine", "HostSystem", "Network"}, true)
	if err != nil {
		return err
	}
	defer v.Destroy(ctx)

	var vms []mo.VirtualMachine
	if err := v.Retrieve(ctx, []string{"VirtualMachine"},
		[]string{"name", "config.uuid", "runtime.host", "runtime.powerState"}, &vms); err != nil {
		return fmt.Errorf("VM 목록 조회 실패: %w", err)
	}
	for i := range vms {
		vm := &vms[i]
		info := &VMInfo{
			Name:    vm.Name,
			Ref:     vm.Self,
			HostRef: vm.Runtime.Host,
		}
		info.PowerState = vm.Runtime.PowerState
		if vm.Config != nil {
			info.UUID = vm.Config.Uuid
		}
		key := strings.ToLower(vm.Name)
		if _, dup := s.vmByName[key]; dup {
			// 같은 vCenter 안에 동명 VM 이 있으면 이름만으로는 특정할 수 없습니다.
			// nil 을 넣어 "모호함"으로 표시하고 조회 시점에 에러로 알립니다.
			s.vmByName[key] = nil
		} else {
			s.vmByName[key] = info
		}
		if info.UUID != "" {
			s.vmByUUID[info.UUID] = info
		}
	}

	var hosts []mo.HostSystem
	if err := v.Retrieve(ctx, []string{"HostSystem"},
		[]string{"name", "configManager.networkSystem", "network"}, &hosts); err != nil {
		return fmt.Errorf("호스트 목록 조회 실패: %w", err)
	}
	for i := range hosts {
		h := &hosts[i]
		info := &HostInfo{Name: h.Name, Ref: h.Self, NetworkSystem: h.ConfigManager.NetworkSystem, Networks: h.Network}
		s.hosts[h.Self] = info
		s.hostByNm[strings.ToLower(h.Name)] = info
	}

	var nets []mo.Network
	if err := v.Retrieve(ctx, []string{"Network"}, []string{"name"}, &nets); err != nil {
		return fmt.Errorf("네트워크 목록 조회 실패: %w", err)
	}
	for i := range nets {
		s.netName[nets[i].Self] = nets[i].Name
	}
	return nil
}

// HostByRef 는 VM 이 올라가 있는 호스트를 색인에서 찾습니다.
func (s *Session) HostByRef(ref *types.ManagedObjectReference) (*HostInfo, bool) {
	if ref == nil {
		return nil, false
	}
	h, ok := s.hosts[*ref]
	return h, ok
}

// HostHasPortgroup 은 그 호스트에 해당 이름의 포트그룹이 실제로 있는지 봅니다.
//
// vCenter 는 NIC 백킹의 DeviceName 에 존재하지 않는 포트그룹 이름을 넣어도
// (특히 전원이 꺼진 VM 이면) 그대로 받아줍니다. 그래서 "설정에 이름이 적혀 있다"는
// 것만으로는 검증이 되지 않고, 인벤토리에 그 포트그룹이 실제로 있는지 따로 봐야 합니다.
func (s *Session) HostHasPortgroup(h *HostInfo, name string) bool {
	for _, ref := range h.Networks {
		if s.netName[ref] == name {
			return true
		}
	}
	return false
}

// HostByName 은 이름으로 ESXi 호스트를 찾습니다. (포트그룹 생성용)
func (s *Session) HostByName(name string) (*HostInfo, bool) {
	h, ok := s.hostByNm[strings.ToLower(name)]
	return h, ok
}

// Fleet 은 vcenter.txt 에 적힌 모든 vCenter 세션 묶음입니다.
type Fleet struct {
	Sessions []*Session
}

// ConnectFleet 은 모든 vCenter 에 병렬로 접속합니다.
//
// 한 대라도 실패하면 전체를 중단합니다. 일부만 붙은 채로 진행하면 그 vCenter 에 있는
// VM 이 "찾을 수 없음"으로 오판되어, 멀쩡한 VM 을 건드리지 않은 채 실패로 기록하거나
// 반대로 다른 vCenter 의 동명 VM 을 잘못 건드릴 수 있습니다.
func ConnectFleet(ctx context.Context, addrs []string, user, pass string, concurrency int) (*Fleet, error) {
	sessions := make([]*Session, len(addrs))
	errs := make([]error, len(addrs))

	pool.Run(concurrency, addrs, func(i int, addr string) {
		s, err := Connect(ctx, addr, user, pass)
		sessions[i], errs[i] = s, err
	})

	var failed []string
	for _, err := range errs {
		if err != nil {
			failed = append(failed, err.Error())
		}
	}
	if len(failed) > 0 {
		for _, s := range sessions {
			if s != nil {
				s.Close(ctx)
			}
		}
		return nil, fmt.Errorf("vCenter 접속/조회 실패로 중단합니다(VM 은 건드리지 않았습니다):\n  - %s",
			strings.Join(failed, "\n  - "))
	}

	f := &Fleet{}
	for _, s := range sessions {
		f.Sessions = append(f.Sessions, s)
	}
	return f, nil
}

// Close 는 모든 세션을 정리합니다.
func (f *Fleet) Close(ctx context.Context) {
	for _, s := range f.Sessions {
		s.Close(ctx)
	}
}

// LookupVM 은 이름으로 VM 을 찾습니다. 여러 vCenter 에 동명 VM 이 있으면 에러입니다.
func (f *Fleet) LookupVM(name string) (*Session, *VMInfo, error) {
	var (
		hitS []*Session
		hitV []*VMInfo
	)
	key := strings.ToLower(name)
	for _, s := range f.Sessions {
		info, ok := s.vmByName[key]
		if !ok {
			continue
		}
		if info == nil {
			return nil, nil, fmt.Errorf("VM %q 가 %s 안에 여러 개 있습니다(이름으로 특정 불가)", name, s.Addr)
		}
		hitS = append(hitS, s)
		hitV = append(hitV, info)
	}
	switch len(hitV) {
	case 0:
		return nil, nil, fmt.Errorf("VM %q 를 어느 vCenter 에서도 찾을 수 없습니다", name)
	case 1:
		return hitS[0], hitV[0], nil
	default:
		addrs := make([]string, 0, len(hitS))
		for _, s := range hitS {
			addrs = append(addrs, s.Addr)
		}
		return nil, nil, fmt.Errorf("VM %q 가 여러 vCenter(%s)에 있습니다", name, strings.Join(addrs, ", "))
	}
}

// LookupVMByUUID 는 UUID 로 VM 을 찾고, 없으면 이름으로 한 번 더 시도합니다.
// 롤백처럼 "예전에 백업해 둔 VM 을 다시 찾는" 상황에서 이름이 바뀌었을 수 있어
// UUID 를 우선합니다.
func (f *Fleet) LookupVMByUUID(uuid, fallbackName string) (*Session, *VMInfo, error) {
	if uuid != "" {
		for _, s := range f.Sessions {
			if info, ok := s.vmByUUID[uuid]; ok {
				return s, info, nil
			}
		}
	}
	if fallbackName == "" {
		return nil, nil, fmt.Errorf("UUID %q 에 해당하는 VM 을 찾을 수 없습니다", uuid)
	}
	return f.LookupVM(fallbackName)
}
