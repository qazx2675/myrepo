// pcisim — govmomi simulator(vcsim, VPX 모델)에 PCI 패스스루 흉내를 더한 검증용 서버.
// 랩 vCenter 가 꺼져 있어서 pci_passthrough / vm-param-check 를 이 서버로 검증한다. (테스트 전용, 배포하지 않음)
//
// vcsim 에 없는 것을 아래처럼 채운다.
//   - 호스트 hardware.pciDevice (Mellanox 2개, NVIDIA GPU 1개, 관리망 Intel NIC 1개)
//   - config.network.pnic 의 vmnic0 PCI 주소를 Intel NIC(0000:18:00.0)로 바꾼다 (관리망 장치 보호 확인용)
//   - configManager.pciPassthruSystem (HostPciPassthruSystem: pciPassthruInfo, UpdatePassthruConfig)
//   - '재부팅 후 활성' 장치 흉내 (-rebootDevs) 와 호스트 재부팅 HTTP 엔드포인트 (/pcisim/reboot?host=)
//   - EnvironmentBrowser.QueryConfigTarget(host) 의 PciPassthrough (SystemId = 호스트별 ESXi system uuid)
//   - PowerOnVM_Task 검증 (SystemId·활성·Vendor/DeviceId·메모리 예약·중복 사용·64bit MMIO 와 그 EFI 펌웨어·창 크기)
package main

import (
	"context"
	"crypto/tls"
	"flag"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"

	"github.com/google/uuid"
	"github.com/vmware/govmomi/simulator"
	"github.com/vmware/govmomi/vim25/methods"
	"github.com/vmware/govmomi/vim25/mo"
	"github.com/vmware/govmomi/vim25/soap"
	"github.com/vmware/govmomi/vim25/types"
)

// 관리망 vmnic0 로 쓰는 장치 (패스스루로 켜면 안 됨)
const mgmtNicID = "0000:18:00.0"

// NVIDIA VendorId. 큰 BAR GPU 라서 64bit MMIO 가 없으면 전원이 켜지지 않게 흉내 낸다.
const nvidiaVendor = 0x10de

// nvidiaBARMB 는 GPU 1개의 BAR 합(MB)이다. RTX 3060(12GB) 에 Resizable BAR 가 켜진 경우:
// BAR0 16MB + BAR1 16GB(VRAM 12GB 를 담는 2의 거듭제곱) + BAR3 32MB.
// 64bit MMIO 창(pciPassthru.64bitMMIOSizeGB)이 GPU 수×이 값보다 작으면 전원이 켜지지 않게 한다.
const nvidiaBARMB = 16 + 16*1024 + 32

// u16 은 PCI ID(16bit 부호 없는 값)를 govmomi 의 int16 필드에 넣는다.
// 0x8000 이상(예: Intel 0x8086)은 실 vCenter 처럼 2의 보수 음수로 들어간다 (0x8086 → -32634).
func u16(v uint16) int16 { return int16(v) }

// pciTemplate 은 모든 호스트에 똑같이 넣는 PCI 장치 목록이다 (PCI 주소 순).
func pciTemplate() []types.HostPciDevice {
	return []types.HostPciDevice{
		{Id: "0000:18:00.0", ClassId: u16(0x0200), Bus: 0x18, Slot: 0, Function: 0,
			VendorId: u16(0x8086), VendorName: "Intel Corporation",
			DeviceId: u16(0x1572), DeviceName: "Ethernet Controller X710 for 10GbE SFP+",
			SubVendorId: u16(0x8086), SubDeviceId: u16(0x0007)},
		{Id: "0000:3b:00.0", ClassId: u16(0x0207), Bus: 0x3b, Slot: 0, Function: 0,
			VendorId: u16(0x15b3), VendorName: "Mellanox Technologies",
			DeviceId: u16(0x101b), DeviceName: "MT28908 Family [ConnectX-6]",
			SubVendorId: u16(0x15b3), SubDeviceId: u16(0x0006)},
		{Id: "0000:3b:00.1", ClassId: u16(0x0207), Bus: 0x3b, Slot: 0, Function: 1,
			VendorId: u16(0x15b3), VendorName: "Mellanox Technologies",
			DeviceId: u16(0x101b), DeviceName: "MT28908 Family [ConnectX-6]",
			SubVendorId: u16(0x15b3), SubDeviceId: u16(0x0006)},
		{Id: "0000:af:00.0", ClassId: u16(0x0300), Bus: 0xaf, Slot: 0, Function: 0,
			VendorId: u16(0x10de), VendorName: "NVIDIA Corporation",
			DeviceId: u16(0x2503), DeviceName: "GA106 [GeForce RTX 3060]",
			SubVendorId: u16(0x10de), SubDeviceId: u16(0x0000)},
	}
}

// hostState 는 호스트 1대의 패스스루 상태다. sim.mu 로 보호한다.
type hostState struct {
	name     string
	host     *simulator.HostSystem
	sys      *passthruSystem
	systemID string // ESXi system uuid (esxcli system uuid get 값). hardware.systemInfo.uuid 와 다르다
	devs     map[string]types.HostPciDevice
	enabled  map[string]bool
	active   map[string]bool
	reboot   map[string]bool // 켠 뒤 호스트를 재부팅해야 활성이 되는 장치
}

type sim struct {
	mu      sync.Mutex
	reg     *simulator.Registry
	byRef   map[types.ManagedObjectReference]*hostState
	byName  map[string]*hostState
	pending map[string]string // "호스트/장치ID" -> 켜는 중인 VM 이름 (전원 켜기 Task 가 끝날 때까지)
}

// passthruInfo 는 현재 상태로 pciPassthruInfo 값을 만든다. s.mu 를 잡은 채로 부른다.
func (h *hostState) passthruInfo() []types.BaseHostPciPassthruInfo {
	var out []types.BaseHostPciPassthruInfo
	for _, d := range pciTemplate() {
		out = append(out, &types.HostPciPassthruInfo{
			Id:              d.Id,
			DependentDevice: d.Id,
			PassthruCapable: true,
			PassthruEnabled: h.enabled[d.Id],
			PassthruActive:  h.active[d.Id],
		})
	}
	return out
}

// publish 는 바뀐 패스스루 상태를 PropertyCollector 에 알린다 (pciPassthruSystem.pciPassthruInfo, host config.pciPassthruInfo).
func (s *sim) publish(ctx *simulator.Context, h *hostState, info []types.BaseHostPciPassthruInfo) {
	ctx.Map.Update(ctx, h.sys, []types.PropertyChange{{Name: "pciPassthruInfo", Val: info}})
	ctx.Map.Update(ctx, h.host, []types.PropertyChange{{Name: "config.pciPassthruInfo", Val: info}})
}

// ---------------------------------------------------------------------------
// HostPciPassthruSystem

type passthruSystem struct {
	mo.HostPciPassthruSystem
	s *sim
	h *hostState
}

func (p *passthruSystem) UpdatePassthruConfig(ctx *simulator.Context, req *types.UpdatePassthruConfig) soap.HasFault {
	body := &methods.UpdatePassthruConfigBody{}
	s, h := p.s, p.h

	s.mu.Lock()
	for _, c := range req.Config {
		cfg := c.GetHostPciPassthruConfig()
		if _, ok := h.devs[cfg.Id]; !ok {
			s.mu.Unlock()
			log.Printf("[PASSTHRU] 거부 host=%s id=%s 사유=없는 장치", h.name, cfg.Id)
			body.Fault_ = simulator.Fault(fmt.Sprintf("Device %s was not found on host %s", cfg.Id, h.name),
				&types.InvalidArgument{InvalidProperty: "id"})
			return body
		}
	}
	for _, c := range req.Config {
		cfg := c.GetHostPciPassthruConfig()
		h.enabled[cfg.Id] = cfg.PassthruEnabled
		if !h.reboot[cfg.Id] {
			h.active[cfg.Id] = cfg.PassthruEnabled
		}
		log.Printf("[PASSTHRU] host=%s id=%s enabled=%v active=%v 재부팅필요=%v",
			h.name, cfg.Id, h.enabled[cfg.Id], h.active[cfg.Id], h.reboot[cfg.Id] && h.enabled[cfg.Id] != h.active[cfg.Id])
		if cfg.Id == mgmtNicID && cfg.PassthruEnabled {
			log.Printf("[경고] MGMT_NIC_PASSTHRU host=%s id=%s — 관리망 vmnic0 장치를 패스스루로 켰습니다 (실장비라면 호스트 네트워크가 끊김)", h.name, cfg.Id)
		}
	}
	info := h.passthruInfo()
	s.mu.Unlock()

	s.publish(ctx, h, info)
	body.Res = &types.UpdatePassthruConfigResponse{}
	return body
}

// ---------------------------------------------------------------------------
// HostPowerSystem — vm_setup 의 power_setting 단계가 종단 실행에서 막히지 않게 하는 최소 흉내.
// (vcsim 은 configManager.powerSystem 참조만 있고 객체가 없다.) 정책 목록과 ConfigurePowerPolicy 만 있다.

type powerSystem struct {
	mo.HostPowerSystem
	host *simulator.HostSystem
}

var powerPolicies = []types.HostPowerPolicy{
	{Key: 1, Name: "High Performance", ShortName: "static", Description: "Do not use any power management features"},
	{Key: 2, Name: "Balanced", ShortName: "dynamic", Description: "Reduce energy consumption with minimal performance compromise"},
	{Key: 3, Name: "Low Power", ShortName: "low", Description: "Reduce energy consumption at the cost of lower performance"},
	{Key: 4, Name: "Custom", ShortName: "custom", Description: "User-defined power management policy"},
}

func (p *powerSystem) ConfigurePowerPolicy(ctx *simulator.Context, req *types.ConfigurePowerPolicy) soap.HasFault {
	body := &methods.ConfigurePowerPolicyBody{}
	for _, pol := range powerPolicies {
		if pol.Key == req.Key {
			info := types.PowerSystemInfo{CurrentPolicy: pol}
			ctx.Map.Update(ctx, p, []types.PropertyChange{{Name: "info", Val: info}})
			ctx.Map.Update(ctx, p.host, []types.PropertyChange{{Name: "config.powerSystemInfo", Val: info}})
			log.Printf("[POWER] host=%s 전원 정책 key=%d (%s)", p.host.Name, pol.Key, pol.ShortName)
			body.Res = &types.ConfigurePowerPolicyResponse{}
			return body
		}
	}
	body.Fault_ = simulator.Fault("Invalid power policy key", &types.InvalidArgument{InvalidProperty: "key"})
	return body
}

// reboot 는 호스트 재부팅을 흉내 낸다: 활성 상태를 설정(enabled)에 맞춘다.
func (s *sim) rebootHandler(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Query().Get("host")
	s.mu.Lock()
	h := s.lookup(name)
	if h == nil {
		s.mu.Unlock()
		http.Error(w, fmt.Sprintf("호스트 없음: %q\n", name), http.StatusNotFound)
		return
	}
	var changed []string
	for id := range h.devs {
		if h.active[id] != h.enabled[id] {
			changed = append(changed, fmt.Sprintf("%s active=%v", id, h.enabled[id]))
		}
		h.active[id] = h.enabled[id]
	}
	info := h.passthruInfo()
	s.mu.Unlock()

	ctx := &simulator.Context{Context: context.Background(), Map: s.reg}
	s.publish(ctx, h, info)
	sort.Strings(changed)
	log.Printf("[REBOOT] host=%s 바뀐 장치=%v", h.name, changed)
	fmt.Fprintf(w, "OK %s 재부팅 완료 (바뀐 장치: %s)\n", h.name, strings.Join(changed, ", "))
}

// stateHandler 는 호스트별 장치 상태를 한 줄씩 보여 준다 (pcisim.sh status 용).
func (s *sim) stateHandler(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var names []string
	for n := range s.byName {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		h := s.byName[n]
		fmt.Fprintf(w, "%s host=%s pciPassthruSystem=%s esxSystemUuid=%s hwUuid=%s\n",
			h.name, h.host.Self.Value, h.sys.Self.Value, h.systemID, h.host.Hardware.SystemInfo.Uuid)
		for _, d := range pciTemplate() {
			fmt.Fprintf(w, "  %s %-22s enabled=%-5v active=%-5v reboot=%v\n",
				d.Id, d.VendorName, h.enabled[d.Id], h.active[d.Id], h.reboot[d.Id])
		}
	}
}

// lookup 은 호스트 이름(FQDN 또는 짧은 이름)으로 찾는다. s.mu 를 잡은 채로 부른다.
func (s *sim) lookup(name string) *hostState {
	if h := s.byName[name]; h != nil {
		return h
	}
	for n, h := range s.byName {
		if strings.SplitN(n, ".", 2)[0] == strings.SplitN(name, ".", 2)[0] {
			return h
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// EnvironmentBrowser.QueryConfigTarget — vcsim 기본 결과에 PciPassthrough 를 더한다

type envBrowser struct {
	*simulator.EnvironmentBrowser
	s *sim
}

// Get 은 PropertyCollector 가 감싼 객체 대신 원래 객체를 읽게 한다.
func (b *envBrowser) Get() mo.Reference { return b.EnvironmentBrowser }

// unwrap 은 Map.Handler 가 세션 레지스트리에 잠깐 넣은 감싼 객체를 뺀다.
func unwrap(ctx *simulator.Context, ref types.ManagedObjectReference) {
	if ctx.Session != nil {
		ctx.Session.Remove(ctx, ref)
	}
}

func (b *envBrowser) QueryConfigTarget(ctx *simulator.Context, req *types.QueryConfigTarget) soap.HasFault {
	unwrap(ctx, b.Self)
	res := b.EnvironmentBrowser.QueryConfigTarget(ctx, req)
	body, ok := res.(*methods.QueryConfigTargetBody)
	if !ok || body.Res == nil || body.Res.Returnval == nil {
		return res
	}
	// host 를 지정하면 그 호스트만, 안 하면 이 EnvironmentBrowser 가 맡은 호스트 전부
	// (VM 의 것이면 VM 이 있는 호스트, 클러스터 것이면 클러스터 호스트 전부. govc device.pci.ls 가 host 없이 VM 것을 부른다)
	var hostRefs []types.ManagedObjectReference
	if req.Host != nil {
		hostRefs = append(hostRefs, *req.Host)
	} else {
		for _, e := range ctx.Map.All("VirtualMachine") {
			if vm, ok := e.(*simulator.VirtualMachine); ok && vm.EnvironmentBrowser == b.Self && vm.Runtime.Host != nil {
				hostRefs = append(hostRefs, *vm.Runtime.Host)
			}
		}
		for _, e := range ctx.Map.All("ClusterComputeResource") {
			if c, ok := e.(*simulator.ClusterComputeResource); ok && c.EnvironmentBrowser != nil && *c.EnvironmentBrowser == b.Self {
				hostRefs = append(hostRefs, c.Host...)
			}
		}
	}
	b.s.mu.Lock()
	var extra []types.BaseVirtualMachinePciPassthroughInfo
	seen := map[types.ManagedObjectReference]bool{} // vcsim 은 VM 과 클러스터가 같은 EnvironmentBrowser 를 쓴다
	for _, hr := range hostRefs {
		h := b.s.byRef[hr]
		if h == nil || seen[hr] {
			continue
		}
		seen[hr] = true
		for _, d := range pciTemplate() {
			if !h.active[d.Id] {
				continue
			}
			extra = append(extra, &types.VirtualMachinePciPassthroughInfo{
				VirtualMachineTargetInfo: types.VirtualMachineTargetInfo{Name: d.Id + " | " + d.DeviceName},
				PciDevice:                d,
				SystemId:                 h.systemID,
			})
		}
	}
	b.s.mu.Unlock()

	t := *body.Res.Returnval // 공유 값일 수 있으니 복사해서 고친다
	t.PciPassthrough = append(append([]types.BaseVirtualMachinePciPassthroughInfo(nil), t.PciPassthrough...), extra...)
	body.Res.Returnval = &t
	return body
}

// ---------------------------------------------------------------------------
// PowerOnVM_Task 검증

type pciVM struct {
	*simulator.VirtualMachine
	s          *sim
	rejectEdit bool // -rejectPciEdit
}

type pciUse struct {
	label   string // pciPassthru0(0000:3b:00.0)
	key     string // 호스트ref/장치ID
	backing *types.VirtualPCIPassthroughDeviceBackingInfo
}

// passthroughDevs 는 VM 의 VirtualPCIPassthrough(일반 backing) 목록이다.
func passthroughDevs(vm *simulator.VirtualMachine, host types.ManagedObjectReference) []pciUse {
	var out []pciUse
	if vm.Config == nil {
		return nil
	}
	n := 0
	for _, d := range vm.Config.Hardware.Device {
		p, ok := d.(*types.VirtualPCIPassthrough)
		if !ok {
			continue
		}
		b, ok := p.Backing.(*types.VirtualPCIPassthroughDeviceBackingInfo)
		if !ok {
			continue
		}
		out = append(out, pciUse{
			label:   fmt.Sprintf("pciPassthru%d(%s)", n, b.Id),
			key:     host.Value + "/" + b.Id,
			backing: b,
		})
		n++
	}
	return out
}

func extraConfig(vm *simulator.VirtualMachine, key string) string {
	for _, o := range vm.Config.ExtraConfig {
		v := o.GetOptionValue()
		if strings.EqualFold(v.Key, key) {
			s, _ := v.Value.(string)
			return strings.TrimSpace(s)
		}
	}
	return ""
}

func extraConfigTrue(vm *simulator.VirtualMachine, key string) bool {
	return strings.EqualFold(extraConfig(vm, key), "TRUE")
}

// parseHexID 는 backing.DeviceId 같은 16진 문자열("101b", "0x101b")을 읽는다.
func parseHexID(s string) (uint16, bool) {
	s = strings.TrimPrefix(strings.ToLower(strings.TrimSpace(s)), "0x")
	v, err := strconv.ParseUint(s, 16, 16)
	return uint16(v), err == nil
}

// check 는 전원 켜기를 막을 사유를 모은다. s.mu 를 잡은 채로 부른다.
func (s *sim) check(vm *simulator.VirtualMachine, h *hostState, uses []pciUse) []string {
	var reasons []string
	gpus := 0 // 64bit MMIO 창 크기 판정용 NVIDIA 장치 수
	for _, u := range uses {
		b := u.backing
		dev, found := h.devs[b.Id]
		if b.SystemId != h.systemID {
			reasons = append(reasons, fmt.Sprintf("%s: The systemId does not match the current system (backing systemId=%q, host %s ESXi system uuid=%q)",
				u.label, b.SystemId, h.name, h.systemID))
		}
		if !found {
			reasons = append(reasons, fmt.Sprintf("%s: Device %s was not found on host %s", u.label, b.Id, h.name))
			continue
		}
		if !h.active[b.Id] {
			why := "passthrough is not enabled"
			if h.enabled[b.Id] {
				why = "passthruEnabled=true but passthruActive=false — host reboot required"
			}
			reasons = append(reasons, fmt.Sprintf("%s: Failed to register the device %s due to unavailable hardware or software support (%s)",
				u.label, b.Id, why))
		}
		did, ok := parseHexID(b.DeviceId)
		if b.VendorId != dev.VendorId || !ok || did != uint16(dev.DeviceId) {
			reasons = append(reasons, fmt.Sprintf("%s: The deviceId and vendorId do not match the device currently at %s (backing vendorId=%d deviceId=%q, host vendorId=%d deviceId=%q)",
				u.label, b.Id, b.VendorId, b.DeviceId, dev.VendorId, fmt.Sprintf("%x", uint16(dev.DeviceId))))
		}
		if uint16(dev.VendorId) == nvidiaVendor {
			gpus++
			if !extraConfigTrue(vm, "pciPassthru.use64bitMMIO") {
				reasons = append(reasons, fmt.Sprintf("%s: PCI passthrough device %s has large BARs and needs 64-bit MMIO (set pciPassthru.use64bitMMIO=TRUE)",
					u.label, b.Id))
			}
		}
		if other := s.inUseBy(vm, u.key); other != "" {
			reasons = append(reasons, fmt.Sprintf("%s: Device %s is already in use by VM '%s'", u.label, b.Id, other))
		}
	}
	// 64bit MMIO 를 켠 GPU VM: EFI 펌웨어여야 하고, MMIO 창이 GPU BAR 합보다 커야 한다.
	if gpus > 0 && extraConfigTrue(vm, "pciPassthru.use64bitMMIO") {
		if fw := vm.Config.Firmware; !strings.EqualFold(fw, string(types.GuestOsDescriptorFirmwareTypeEfi)) {
			reasons = append(reasons, fmt.Sprintf("64-bit MMIO for PCI passthrough requires EFI firmware (firmware=%q)", fw))
		}
		sizeGB, _ := strconv.Atoi(extraConfig(vm, "pciPassthru.64bitMMIOSizeGB"))
		if need := gpus * nvidiaBARMB; sizeGB*1024 < need {
			reasons = append(reasons, fmt.Sprintf("64-bit MMIO window too small: pciPassthru.64bitMMIOSizeGB=%d, the BARs of %d GPU(s) need %d MB",
				sizeGB, gpus, need))
		}
	}

	memMB := int64(vm.Config.Hardware.MemoryMB)
	var resv int64
	if vm.Config.MemoryAllocation != nil && vm.Config.MemoryAllocation.Reservation != nil {
		resv = *vm.Config.MemoryAllocation.Reservation
	}
	locked := vm.Config.MemoryReservationLockedToMax != nil && *vm.Config.MemoryReservationLockedToMax
	if !locked && resv < memMB {
		reasons = append(reasons, fmt.Sprintf("Invalid memory setting: memory reservation (sched.mem.min) should be equal to memsize(%d). (reservation=%d, memoryReservationLockedToMax=false)",
			memMB, resv))
	}
	return reasons
}

// inUseBy 는 같은 호스트의 같은 장치를 이미 쓰는(켜져 있거나 켜는 중인) 다른 VM 이름을 돌려준다.
func (s *sim) inUseBy(self *simulator.VirtualMachine, key string) string {
	if n, ok := s.pending[key]; ok && n != self.Name {
		return n
	}
	for _, e := range s.reg.All("VirtualMachine") {
		o, ok := e.(*simulator.VirtualMachine)
		if !ok || o.Self == self.Self || o.Runtime.Host == nil ||
			o.Runtime.PowerState != types.VirtualMachinePowerStatePoweredOn {
			continue
		}
		for _, u := range passthroughDevs(o, *o.Runtime.Host) {
			if u.key == key {
				return o.Name
			}
		}
	}
	return ""
}

func (v *pciVM) Get() mo.Reference { return v.VirtualMachine }

func (v *pciVM) PowerOnVMTask(ctx *simulator.Context, req *types.PowerOnVM_Task) soap.HasFault {
	unwrap(ctx, v.Self)
	s, vm := v.s, v.VirtualMachine
	var hostRef types.ManagedObjectReference
	if vm.Runtime.Host != nil {
		hostRef = *vm.Runtime.Host
	}

	s.mu.Lock()
	h := s.byRef[hostRef]
	uses := passthroughDevs(vm, hostRef)
	if len(uses) == 0 || h == nil {
		s.mu.Unlock()
		hn := hostRef.Value
		if h != nil {
			hn = h.name
		}
		log.Printf("[POWERON] 허용 vm=%s host=%s (패스스루 장치 없음)", vm.Name, hn)
		return vm.PowerOnVMTask(ctx, req)
	}
	reasons := s.check(vm, h, uses)
	var keys []string
	if len(reasons) == 0 {
		for _, u := range uses {
			s.pending[u.key] = vm.Name
			keys = append(keys, u.key)
		}
	}
	s.mu.Unlock()

	var labels []string
	for _, u := range uses {
		labels = append(labels, u.label)
	}

	if len(reasons) > 0 {
		msg := "Module 'DevicePowerOn' power on failed. " + strings.Join(reasons, "; ")
		log.Printf("[POWERON] 거부 vm=%s host=%s 장치=%v 사유=%s", vm.Name, h.name, labels, strings.Join(reasons, " | "))
		return &methods.PowerOnVM_TaskBody{
			Res: &types.PowerOnVM_TaskResponse{Returnval: failedTask(ctx, vm, msg, reasons)},
		}
	}

	log.Printf("[POWERON] 허용 vm=%s host=%s 장치=%v", vm.Name, h.name, labels)
	res := vm.PowerOnVMTask(ctx, req)
	release := func() {
		s.mu.Lock()
		for _, k := range keys {
			delete(s.pending, k)
		}
		s.mu.Unlock()
	}
	if b, ok := res.(*methods.PowerOnVM_TaskBody); ok && b.Res != nil {
		if t, ok := ctx.Map.Get(b.Res.Returnval).(*simulator.Task); ok {
			go func() { t.Wait(); release() }()
			return res
		}
	}
	release()
	return res
}

// ReconfigVMTask 는 패스스루 backing 의 deviceName 을 비우고 저장한다 — 실 ESXi 8.0.3 이 그렇게 돌려준다(실측).
// 이름이 없어도 도구들이 장치명 기준 vendor 를 판정하는지 보려는 것이다.
// -rejectPciEdit 이면 패스스루 장치의 Edit(backing 수정)을 거부한다: 실 vCenter 가 Edit 을 받지 않는 경우
// pci_passthrough 가 "제거 후 다시 연결"로 넘어가는지 확인하는 용도다.
func (v *pciVM) ReconfigVMTask(ctx *simulator.Context, req *types.ReconfigVM_Task) soap.HasFault {
	unwrap(ctx, v.Self)
	for _, dc := range req.Spec.DeviceChange {
		ds := dc.GetVirtualDeviceConfigSpec()
		if p, ok := ds.Device.(*types.VirtualPCIPassthrough); ok {
			if b, ok := p.Backing.(*types.VirtualPCIPassthroughDeviceBackingInfo); ok {
				b.DeviceName = ""
			}
		}
		if _, ok := ds.Device.(*types.VirtualPCIPassthrough); ok && v.rejectEdit && ds.Operation == types.VirtualDeviceConfigSpecOperationEdit {
			msg := "Editing the backing of a PCI passthrough device is not supported (pcisim -rejectPciEdit)"
			log.Printf("[RECONFIG] 거부 vm=%s 사유=패스스루 장치 Edit (-rejectPciEdit)", v.Name)
			return &methods.ReconfigVM_TaskBody{
				Res: &types.ReconfigVM_TaskResponse{Returnval: failTask(ctx, v.VirtualMachine, "reconfigure", &types.NotSupported{}, msg)},
			}
		}
	}
	return v.VirtualMachine.ReconfigVMTask(ctx, req)
}

// failedTask 는 실패로 끝난 powerOn Task 를 만든다.
func failedTask(ctx *simulator.Context, vm *simulator.VirtualMachine, msg string, reasons []string) types.ManagedObjectReference {
	fault := &types.GenericVmConfigFault{Reason: msg}
	for _, r := range reasons {
		fault.FaultMessage = append(fault.FaultMessage, types.LocalizableMessage{Key: "msg.pcisim.powerOn", Message: r})
	}
	return failTask(ctx, vm, "powerOn", fault, msg)
}

// failTask 는 fault 로 실패하는 Task 를 만든다.
// vcsim Task 는 오류 메시지(LocalizedMessage)를 "*types.XXX" 로만 채우므로,
// 끝난 뒤 실 ESXi 처럼 사람이 읽는 메시지로 바꿔 둔다 (Task 참조를 돌려주기 전이라 클라이언트는 바뀐 값만 본다).
func failTask(ctx *simulator.Context, vm *simulator.VirtualMachine, name string, fault types.BaseMethodFault, msg string) types.ManagedObjectReference {
	task := simulator.CreateTask(vm, name, func(*simulator.Task) (types.AnyType, types.BaseMethodFault) {
		return nil, fault
	})
	ref := task.Run(ctx)
	task.Wait()
	ctx.Map.Update(ctx, task, []types.PropertyChange{
		{Name: "info.error", Val: types.LocalizedMethodFault{Fault: fault, LocalizedMessage: msg}},
	})
	return ref
}

// ---------------------------------------------------------------------------

// parseRebootDevs 는 "-rebootDevs" 값을 호스트별 목록으로 푼다.
// "0000:3b:00.1" 은 모든 호스트, "bm02.saccae.com/0000:3b:00.0" 은 그 호스트만.
func parseRebootDevs(v string) (all map[string]bool, perHost map[string]map[string]bool) {
	all, perHost = map[string]bool{}, map[string]map[string]bool{}
	for _, f := range strings.Split(v, ",") {
		f = strings.TrimSpace(f)
		if f == "" {
			continue
		}
		if i := strings.LastIndex(f, "/"); i >= 0 {
			host, id := f[:i], f[i+1:]
			if perHost[host] == nil {
				perHost[host] = map[string]bool{}
			}
			perHost[host][id] = true
		} else {
			all[f] = true
		}
	}
	return
}

func main() {
	listen := flag.String("l", "127.0.0.1:8989", "listen 주소 (host:port)")
	username := flag.String("username", "lscsystems@vsphere.local", "로그인 계정")
	password := flag.String("password", "pcisim-pass", "로그인 비밀번호 (govc 는 GOVC_PASSWORD 와 같은 이름의 파일이 있으면 그 파일 내용을 읽으므로 실행파일 이름 pcisim 은 피한다)")
	hostsFlag := flag.String("hosts", "bm01.saccae.com,bm02.saccae.com", "만들 호스트 이름 (쉼표 구분, 클러스터 DC0_C0 안에 만든다)")
	rebootDevs := flag.String("rebootDevs", "", "켠 뒤 호스트 재부팅이 있어야 활성이 되는 장치 (쉼표 구분). 예: 0000:3b:00.1 또는 bm02.saccae.com/0000:3b:00.0")
	rejectPciEdit := flag.Bool("rejectPciEdit", false, "ReconfigVM 에서 패스스루 장치의 Edit(backing 수정)을 거부한다 (제거 후 다시 연결 경로 시험용)")
	flag.Parse()

	var hostNames []string
	for _, n := range strings.Split(*hostsFlag, ",") {
		if n = strings.TrimSpace(n); n != "" {
			hostNames = append(hostNames, n)
		}
	}
	if len(hostNames) == 0 {
		log.Fatal("-hosts 값이 비어 있습니다.")
	}
	rbAll, rbHost := parseRebootDevs(*rebootDevs)
	for hn := range rbHost {
		found := false
		for _, n := range hostNames {
			found = found || n == hn
		}
		if !found {
			log.Fatalf("-rebootDevs 의 호스트 %q 가 -hosts 에 없습니다.", hn)
		}
	}

	model := simulator.VPX()
	model.Host = 0 // 단독 호스트 없이 클러스터 DC0_C0 에 호스트를 모은다
	model.ClusterHost = len(hostNames)
	model.Machine = 0 // 기본 VM 은 만들지 않는다
	if err := model.Create(); err != nil {
		log.Fatalf("모델 생성 실패: %v", err)
	}
	defer model.Remove()

	reg := model.Map()
	s := &sim{
		reg:     reg,
		byRef:   map[types.ManagedObjectReference]*hostState{},
		byName:  map[string]*hostState{},
		pending: map[string]string{},
	}

	// 호스트 이름을 -hosts 값으로 바꾸고 PCI 장치·패스스루 시스템을 붙인다.
	var hosts []*simulator.HostSystem
	for _, e := range reg.All("HostSystem") {
		hosts = append(hosts, e.(*simulator.HostSystem))
	}
	sort.Slice(hosts, func(i, j int) bool { return hosts[i].Name < hosts[j].Name })
	for i, hs := range hosts {
		name := hostNames[i]
		hs.Name = name
		hs.Summary.Config.Name = name

		devs := pciTemplate()
		hs.Hardware.PciDevice = devs
		for j := range hs.Config.Network.Pnic {
			if hs.Config.Network.Pnic[j].Device == "vmnic0" {
				hs.Config.Network.Pnic[j].Pci = mgmtNicID
			}
		}

		h := &hostState{
			name:     name,
			host:     hs,
			systemID: uuid.NewSHA1(uuid.NameSpaceOID, []byte("pcisim-esx-system:"+name)).String(),
			devs:     map[string]types.HostPciDevice{},
			enabled:  map[string]bool{},
			active:   map[string]bool{},
			reboot:   map[string]bool{},
		}
		for _, d := range devs {
			h.devs[d.Id] = d
			h.reboot[d.Id] = rbAll[d.Id] || rbHost[name][d.Id]
		}
		if h.systemID == hs.Hardware.SystemInfo.Uuid {
			log.Fatalf("ESXi system uuid 가 hardware uuid 와 같습니다: %s", name)
		}

		sys := &passthruSystem{s: s, h: h}
		sys.Self = types.ManagedObjectReference{Type: "HostPciPassthruSystem", Value: "pciPassthruSystem-" + hs.Self.Value}
		h.sys = sys
		sys.PciPassthruInfo = h.passthruInfo()
		reg.Put(sys)
		ref := sys.Self
		hs.ConfigManager.PciPassthruSystem = &ref
		hs.Config.PciPassthruInfo = h.passthruInfo()

		pw := &powerSystem{host: hs}
		pw.Self = types.ManagedObjectReference{Type: "HostPowerSystem", Value: "powerSystem-" + hs.Self.Value}
		pw.Capability = types.PowerSystemCapability{AvailablePolicy: powerPolicies}
		pw.Info = types.PowerSystemInfo{CurrentPolicy: powerPolicies[1]} // 처음엔 Balanced
		reg.Put(pw)
		pwRef := pw.Self
		hs.ConfigManager.PowerSystem = &pwRef
		hs.Config.PowerSystemCapability = &pw.Capability
		hs.Config.PowerSystemInfo = &pw.Info

		s.byRef[hs.Self] = h
		s.byName[name] = h
		var rb []string
		for id, v := range h.reboot {
			if v {
				rb = append(rb, id)
			}
		}
		sort.Strings(rb)
		log.Printf("[INIT] host=%s moref=%s pciPassthruSystem=%s esxSystemUuid=%s hwUuid=%s 재부팅필요장치=%v",
			name, hs.Self.Value, sys.Self.Value, h.systemID, hs.Hardware.SystemInfo.Uuid, rb)
	}

	// PowerOnVM_Task, QueryConfigTarget 는 감싼 객체로 처리한다 (나머지 메서드는 vcsim 그대로).
	// vcsim 은 로그인한 호출이면 Map.Handler 결과 대신 session.Get(ref) 를 쓰고, session.Get 은 세션 레지스트리에
	// 없으면 전역 객체를 돌려준다. 그래서 감싼 객체를 세션 레지스트리에 잠깐 넣고, 감싼 메서드 첫 줄에서 뺀다(unwrap).
	reg.Handler = func(ctx *simulator.Context, m *simulator.Method) (mo.Reference, types.BaseMethodFault) {
		var w mo.Reference
		switch m.Name {
		case "PowerOnVM_Task":
			if vm, ok := ctx.Map.Get(m.This).(*simulator.VirtualMachine); ok {
				w = &pciVM{VirtualMachine: vm, s: s}
			}
		case "ReconfigVM_Task":
			if vm, ok := ctx.Map.Get(m.This).(*simulator.VirtualMachine); ok {
				w = &pciVM{VirtualMachine: vm, s: s, rejectEdit: *rejectPciEdit}
			}
		case "QueryConfigTarget":
			if b, ok := ctx.Map.Get(m.This).(*simulator.EnvironmentBrowser); ok {
				w = &envBrowser{EnvironmentBrowser: b, s: s}
			}
		}
		if w != nil && ctx.Session != nil {
			ctx.Session.Put(w)
		}
		return w, nil
	}

	model.Service.TLS = new(tls.Config)
	model.Service.Listen = &url.URL{Host: *listen, User: url.UserPassword(*username, *password)}
	model.Service.HandleFunc("/pcisim/reboot", s.rebootHandler)
	model.Service.HandleFunc("/pcisim/state", s.stateHandler)
	server := model.Service.NewServer()

	u := *server.URL
	u.User = nil
	log.Printf("[READY] pcisim %s (계정 %s) 호스트=%v", u.String(), *username, hostNames)
	fmt.Printf("export GOVC_URL=%s GOVC_USERNAME=%s GOVC_PASSWORD=%s GOVC_INSECURE=1\n", u.String(), *username, *password)

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig
	log.Printf("[STOP] 종료")
	server.Close()
}
