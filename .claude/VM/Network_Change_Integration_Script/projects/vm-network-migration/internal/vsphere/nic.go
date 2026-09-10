package vsphere

import (
	"context"
	"fmt"
	"strings"

	"github.com/vmware/govmomi/object"
	"github.com/vmware/govmomi/vim25/types"
)

// NICState 는 VM 가상 NIC 한 장의 현재 상태입니다.
type NICState struct {
	Key            int32
	Label          string
	Portgroup      string // 연결된 포트그룹 이름 (해석 실패 시 빈 문자열)
	Connected      bool   // 현재 연결됨 (전원 꺼진 VM 은 항상 false)
	StartConnected bool   // 부팅 시 연결
}

// nicAt 은 VM 의 가상 NIC 목록에서 하나를 골라 실제 device 객체와 함께 돌려줍니다.
// key 가 0 이 아니면 device key 로, 그렇지 않으면 순번(index)으로 고릅니다.
func (s *Session) nicAt(ctx context.Context, info *VMInfo, index int, key int32) (types.BaseVirtualEthernetCard, error) {
	vm := object.NewVirtualMachine(s.Client.Client, info.Ref)
	devices, err := vm.Device(ctx)
	if err != nil {
		return nil, fmt.Errorf("장치 목록 조회 실패: %w", err)
	}

	var cards []types.BaseVirtualEthernetCard
	for _, d := range devices {
		if c, ok := d.(types.BaseVirtualEthernetCard); ok {
			cards = append(cards, c)
		}
	}
	if len(cards) == 0 {
		return nil, fmt.Errorf("가상 NIC 가 없습니다")
	}

	if key != 0 {
		for _, c := range cards {
			if c.GetVirtualEthernetCard().Key == key {
				return c, nil
			}
		}
		return nil, fmt.Errorf("NIC key=%d 를 찾을 수 없습니다", key)
	}
	if index < 0 || index >= len(cards) {
		return nil, fmt.Errorf("NIC 순번 %d 가 범위를 벗어났습니다(NIC %d장)", index, len(cards))
	}
	return cards[index], nil
}

// describe 는 device 객체에서 NICState 를 뽑아냅니다.
func (s *Session) describe(c types.BaseVirtualEthernetCard) NICState {
	card := c.GetVirtualEthernetCard()
	st := NICState{Key: card.Key, Portgroup: s.PortgroupName(card.Backing)}
	if card.DeviceInfo != nil {
		st.Label = card.DeviceInfo.GetDescription().Label
	}
	if card.Connectable != nil {
		st.Connected = card.Connectable.Connected
		st.StartConnected = card.Connectable.StartConnected
	}
	return st
}

// NIC 은 VM 의 지정한 NIC 상태를 읽습니다. key 가 0 이면 순번으로 고릅니다.
func (s *Session) NIC(ctx context.Context, info *VMInfo, index int, key int32) (NICState, error) {
	c, err := s.nicAt(ctx, info, index, key)
	if err != nil {
		return NICState{}, err
	}
	return s.describe(c), nil
}

// PortgroupName 은 NIC 백킹에서 포트그룹 이름을 해석합니다.
//
// 표준 vSwitch 포트그룹은 백킹의 DeviceName 이 곧 포트그룹 이름입니다.
// 분산 스위치(DVS)는 포트그룹 MO 를 색인에서 되짚어 이름을 찾습니다.
func (s *Session) PortgroupName(backing types.BaseVirtualDeviceBackingInfo) string {
	switch b := backing.(type) {
	case *types.VirtualEthernetCardNetworkBackingInfo:
		if b.DeviceName != "" {
			return b.DeviceName
		}
		if b.Network != nil {
			return s.netName[*b.Network]
		}
	case *types.VirtualEthernetCardDistributedVirtualPortBackingInfo:
		ref := types.ManagedObjectReference{Type: "DistributedVirtualPortgroup", Value: b.Port.PortgroupKey}
		if n, ok := s.netName[ref]; ok {
			return n
		}
		return b.Port.PortgroupKey
	case *types.VirtualEthernetCardOpaqueNetworkBackingInfo:
		return b.OpaqueNetworkId
	}
	return ""
}

// reconfigureNIC 은 device 를 수정한 뒤 Reconfigure Task 한 번으로 반영합니다.
// mutate 가 false 를 돌려주면 이미 원하는 상태라는 뜻이므로 Task 를 띄우지 않습니다.
func (s *Session) reconfigureNIC(ctx context.Context, info *VMInfo, index int, key int32,
	mutate func(card *types.VirtualEthernetCard) bool) (changed bool, err error) {

	c, err := s.nicAt(ctx, info, index, key)
	if err != nil {
		return false, err
	}
	card := c.GetVirtualEthernetCard()
	if card.Connectable == nil {
		card.Connectable = &types.VirtualDeviceConnectInfo{}
	}
	if !mutate(card) {
		return false, nil
	}

	spec := types.VirtualMachineConfigSpec{
		DeviceChange: []types.BaseVirtualDeviceConfigSpec{
			&types.VirtualDeviceConfigSpec{
				Operation: types.VirtualDeviceConfigSpecOperationEdit,
				Device:    c.(types.BaseVirtualDevice),
			},
		},
	}

	vm := object.NewVirtualMachine(s.Client.Client, info.Ref)
	task, err := vm.Reconfigure(ctx, spec)
	if err != nil {
		return false, fmt.Errorf("Reconfigure 요청 실패: %w", err)
	}
	if err := task.Wait(ctx); err != nil {
		return false, fmt.Errorf("Reconfigure 실패: %w", err)
	}
	return true, nil
}

// SetConnected 는 NIC 의 연결 상태만 바꿉니다. (Step 1 해제 / Step 3-Undo)
// 이미 원하는 상태면 아무것도 하지 않고 changed=false 를 돌려줍니다(멱등).
func (s *Session) SetConnected(ctx context.Context, info *VMInfo, index int, key int32, connected bool) (bool, error) {
	return s.reconfigureNIC(ctx, info, index, key, func(card *types.VirtualEthernetCard) bool {
		if card.Connectable.Connected == connected && card.Connectable.StartConnected == connected {
			return false
		}
		card.Connectable.Connected = connected
		card.Connectable.StartConnected = connected
		return true
	})
}

// SetPortgroup 은 NIC 백킹을 표준 vSwitch 포트그룹으로 교체하고 연결 상태를 지정합니다.
// (Step 3 연결 / Step 1-Undo 원복)
//
// 백킹에는 Network MO 참조 대신 DeviceName 만 넣습니다. 포트그룹은 호스트마다
// 따로 존재하므로, 이름만 주고 vCenter 가 해당 VM 이 있는 호스트의 포트그룹으로
// 해석하게 하는 편이 안전합니다. (방금 만든 포트그룹은 색인에도 아직 없습니다)
func (s *Session) SetPortgroup(ctx context.Context, info *VMInfo, index int, key int32,
	pg string, connected, startConnected bool) (bool, error) {

	return s.reconfigureNIC(ctx, info, index, key, func(card *types.VirtualEthernetCard) bool {
		samePG := s.PortgroupName(card.Backing) == pg
		sameConn := card.Connectable.Connected == connected && card.Connectable.StartConnected == startConnected
		if samePG && sameConn {
			return false
		}
		card.Backing = &types.VirtualEthernetCardNetworkBackingInfo{
			VirtualDeviceDeviceBackingInfo: types.VirtualDeviceDeviceBackingInfo{DeviceName: pg},
		}
		card.Connectable.Connected = connected
		card.Connectable.StartConnected = startConnected
		return true
	})
}

// AddPortGroup 은 호스트의 표준 vSwitch 에 포트그룹을 만듭니다. (Step 2)
// 이미 있으면 성공으로 간주합니다(멱등).
func (s *Session) AddPortGroup(ctx context.Context, h *HostInfo, pg string, vlan int32, vswitch string) (created bool, err error) {
	if h.NetworkSystem == nil {
		return false, fmt.Errorf("호스트 %q 의 networkSystem 을 찾을 수 없습니다", h.Name)
	}
	ns := object.NewHostNetworkSystem(s.Client.Client, *h.NetworkSystem)

	spec := types.HostPortGroupSpec{
		Name:        pg,
		VlanId:      vlan,
		VswitchName: vswitch,
		Policy:      types.HostNetworkPolicy{}, // 빈 정책 = vSwitch 기본 정책 상속
	}
	if err := ns.AddPortGroup(ctx, spec); err != nil {
		if isAlreadyExists(err) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// isAlreadyExists 는 "이미 존재함" 오류인지 판별합니다.
// AlreadyExists fault 는 govmomi 에서 타입 단언이 아니라 문자열로 확인하는 편이
// SOAP fault 래핑 방식에 상관없이 안정적입니다.
func isAlreadyExists(err error) bool {
	msg := err.Error()
	return strings.Contains(msg, "AlreadyExists") || strings.Contains(msg, "already exists")
}
