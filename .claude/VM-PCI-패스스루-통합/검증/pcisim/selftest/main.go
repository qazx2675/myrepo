// selftest — pcisim 이 의도대로 흉내 내는지 확인하는 점검 프로그램 (selftest.sh 가 부른다).
// pcisim 은 -rebootDevs=0000:3b:00.1 로 떠 있어야 한다 (bm01 의 3b:00.1 은 재부팅 뒤 활성).
package main

import (
	"context"
	"crypto/tls"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/vmware/govmomi"
	"github.com/vmware/govmomi/find"
	"github.com/vmware/govmomi/object"
	"github.com/vmware/govmomi/property"
	"github.com/vmware/govmomi/vim25/methods"
	"github.com/vmware/govmomi/vim25/mo"
	"github.com/vmware/govmomi/vim25/types"
)

var (
	ctx   = context.Background()
	fails int
)

func check(name string, ok bool, detail string) {
	if ok {
		fmt.Printf("PASS %s\n", name)
		return
	}
	fails++
	fmt.Printf("FAIL %s — %s\n", name, detail)
}

type env struct {
	c       *govmomi.Client
	pc      *property.Collector
	finder  *find.Finder
	cluster *object.ClusterComputeResource
	pool    *object.ResourcePool
	folder  *object.Folder
	base    *url.URL
}

func (e *env) host(name string) (*object.HostSystem, mo.HostSystem) {
	h, err := e.finder.HostSystem(ctx, name)
	if err != nil {
		log.Fatalf("호스트 %s: %v", name, err)
	}
	var m mo.HostSystem
	err = e.pc.RetrieveOne(ctx, h.Reference(), []string{"name", "hardware.pciDevice", "hardware.systemInfo.uuid", "configManager", "config.network.pnic"}, &m)
	if err != nil {
		log.Fatalf("호스트 %s 속성: %v", name, err)
	}
	return h, m
}

func (e *env) passthru(m mo.HostSystem) map[string]types.HostPciPassthruInfo {
	var sys mo.HostPciPassthruSystem
	if err := e.pc.RetrieveOne(ctx, *m.ConfigManager.PciPassthruSystem, []string{"pciPassthruInfo"}, &sys); err != nil {
		log.Fatalf("pciPassthruInfo: %v", err)
	}
	out := map[string]types.HostPciPassthruInfo{}
	for _, b := range sys.PciPassthruInfo {
		i := b.GetHostPciPassthruInfo()
		out[i.Id] = *i
	}
	return out
}

func (e *env) enable(m mo.HostSystem, ids ...string) {
	req := &types.UpdatePassthruConfig{This: *m.ConfigManager.PciPassthruSystem}
	for _, id := range ids {
		req.Config = append(req.Config, &types.HostPciPassthruConfig{Id: id, PassthruEnabled: true})
	}
	if _, err := methods.UpdatePassthruConfig(ctx, e.c.Client, req); err != nil {
		log.Fatalf("UpdatePassthruConfig: %v", err)
	}
}

func (e *env) configTarget(h *object.HostSystem) *types.ConfigTarget {
	var cr mo.ClusterComputeResource
	if err := e.pc.RetrieveOne(ctx, e.cluster.Reference(), []string{"environmentBrowser"}, &cr); err != nil {
		log.Fatalf("environmentBrowser: %v", err)
	}
	req := &types.QueryConfigTarget{This: *cr.EnvironmentBrowser}
	if h != nil {
		ref := h.Reference()
		req.Host = &ref
	}
	res, err := methods.QueryConfigTarget(ctx, e.c.Client, req)
	if err != nil {
		log.Fatalf("QueryConfigTarget: %v", err)
	}
	return res.Returnval
}

func (e *env) createVM(name string, h *object.HostSystem, memMB int32) *object.VirtualMachine {
	spec := types.VirtualMachineConfigSpec{
		Name: name, GuestId: "rhel8_64Guest", NumCPUs: 2, MemoryMB: int64(memMB),
		Files: &types.VirtualMachineFileInfo{VmPathName: "[LocalDS_0]"},
	}
	t, err := e.folder.CreateVM(ctx, spec, e.pool, h)
	if err != nil {
		log.Fatalf("CreateVM %s: %v", name, err)
	}
	r, err := t.WaitForResult(ctx)
	if err != nil {
		log.Fatalf("CreateVM %s: %v", name, err)
	}
	return object.NewVirtualMachine(e.c.Client, r.Result.(types.ManagedObjectReference))
}

// addPCI 는 패스스루 장치를 붙이고 메모리 예약 등을 함께 바꾼다.
func addPCI(vm *object.VirtualMachine, b types.VirtualPCIPassthroughDeviceBackingInfo, spec types.VirtualMachineConfigSpec) error {
	dev := &types.VirtualPCIPassthrough{VirtualDevice: types.VirtualDevice{Key: -100, Backing: &b}}
	spec.DeviceChange = append(spec.DeviceChange, &types.VirtualDeviceConfigSpec{
		Operation: types.VirtualDeviceConfigSpecOperationAdd, Device: dev,
	})
	return reconfig(vm, spec)
}

func reconfig(vm *object.VirtualMachine, spec types.VirtualMachineConfigSpec) error {
	t, err := vm.Reconfigure(ctx, spec)
	if err != nil {
		return err
	}
	return t.Wait(ctx)
}

func powerOn(vm *object.VirtualMachine) error {
	t, err := vm.PowerOn(ctx)
	if err != nil {
		return err
	}
	return t.Wait(ctx)
}

func backingFor(m mo.HostSystem, id, systemID string) types.VirtualPCIPassthroughDeviceBackingInfo {
	for _, d := range m.Hardware.PciDevice {
		if d.Id == id {
			return types.VirtualPCIPassthroughDeviceBackingInfo{
				Id: d.Id, DeviceId: fmt.Sprintf("%x", uint16(d.DeviceId)), VendorId: d.VendorId, SystemId: systemID,
			}
		}
	}
	log.Fatalf("장치 없음: %s", id)
	return types.VirtualPCIPassthroughDeviceBackingInfo{}
}

func ctSystemID(ct *types.ConfigTarget, id string) (string, bool) {
	for _, b := range ct.PciPassthrough {
		if p, ok := b.(*types.VirtualMachinePciPassthroughInfo); ok && p.PciDevice.Id == id {
			return p.SystemId, true
		}
	}
	return "", false
}

func errText(err error) string {
	if err == nil {
		return "<성공>"
	}
	return err.Error()
}

func (e *env) reboot(host string) string {
	tr := &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}
	u := url.URL{Scheme: e.base.Scheme, Host: e.base.Host, Path: "/pcisim/reboot", RawQuery: "host=" + url.QueryEscape(host)}
	resp, err := (&http.Client{Transport: tr}).Get(u.String())
	if err != nil {
		log.Fatalf("reboot: %v", err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return strings.TrimSpace(string(b))
}

func main() {
	rawURL := flag.String("url", os.Getenv("GOVC_URL"), "pcisim SDK URL")
	user := flag.String("username", os.Getenv("GOVC_USERNAME"), "계정")
	pass := flag.String("password", os.Getenv("GOVC_PASSWORD"), "비밀번호")
	flag.Parse()

	u, err := url.Parse(*rawURL)
	if err != nil || u.Host == "" {
		log.Fatalf("-url 값 확인: %q", *rawURL)
	}
	base := *u
	u.User = url.UserPassword(*user, *pass)
	c, err := govmomi.NewClient(ctx, u, true)
	if err != nil {
		log.Fatalf("접속 실패: %v", err)
	}
	e := &env{c: c, pc: property.DefaultCollector(c.Client), finder: find.NewFinder(c.Client), base: &base}
	dc, err := e.finder.Datacenter(ctx, "DC0")
	if err != nil {
		log.Fatal(err)
	}
	e.finder.SetDatacenter(dc)
	if e.cluster, err = e.finder.ClusterComputeResource(ctx, "DC0_C0"); err != nil {
		log.Fatal(err)
	}
	if e.pool, err = e.cluster.ResourcePool(ctx); err != nil {
		log.Fatal(err)
	}
	folders, _ := dc.Folders(ctx)
	e.folder = folders.VmFolder

	// 1. 장치·관리망 NIC·패스스루 초기 상태
	h1, m1 := e.host("bm01.saccae.com")
	check("bm01 hardware.pciDevice 4개", len(m1.Hardware.PciDevice) == 4, fmt.Sprint(len(m1.Hardware.PciDevice)))
	vmnic0 := ""
	for _, p := range m1.Config.Network.Pnic {
		if p.Device == "vmnic0" {
			vmnic0 = p.Pci
		}
	}
	check("bm01 vmnic0 pci=0000:18:00.0", vmnic0 == "0000:18:00.0", vmnic0)
	pt := e.passthru(m1)
	check("bm01 pciPassthruInfo 4개, 전부 꺼짐", len(pt) == 4 && !pt["0000:3b:00.0"].PassthruEnabled && pt["0000:3b:00.0"].PassthruCapable, fmt.Sprint(pt))

	// 2. 활성화: 3b:00.0 은 바로 활성, 3b:00.1 은 재부팅 뒤 활성
	e.enable(m1, "0000:3b:00.0", "0000:3b:00.1")
	pt = e.passthru(m1)
	check("3b:00.0 enable → active=true", pt["0000:3b:00.0"].PassthruEnabled && pt["0000:3b:00.0"].PassthruActive, fmt.Sprint(pt["0000:3b:00.0"]))
	check("3b:00.1 enable → active=false (재부팅 필요)", pt["0000:3b:00.1"].PassthruEnabled && !pt["0000:3b:00.1"].PassthruActive, fmt.Sprint(pt["0000:3b:00.1"]))

	// 3. QueryConfigTarget: 활성 장치만, SystemId 는 hardware uuid 와 다름
	ct := e.configTarget(h1)
	sid, ok := ctSystemID(ct, "0000:3b:00.0")
	_, ok1 := ctSystemID(ct, "0000:3b:00.1")
	check("QueryConfigTarget PciPassthrough 에 3b:00.0 있음, 3b:00.1 없음", ok && !ok1 && len(ct.PciPassthrough) == 1, fmt.Sprint(len(ct.PciPassthrough)))
	check("ConfigTarget SystemId ≠ hardware.systemInfo.uuid", sid != "" && sid != m1.Hardware.SystemInfo.Uuid, sid+" / "+m1.Hardware.SystemInfo.Uuid)
	check("vcsim 기본 ConfigTarget 내용 유지 (Datastore)", len(ct.Datastore) > 0, "datastore 없음")

	resv := func(mb int64) *types.ResourceAllocationInfo { return &types.ResourceAllocationInfo{Reservation: &mb} }
	locked := true

	// 4. 틀린 SystemId(hardware uuid) + 메모리 예약 없음 → 실패
	vmBad := e.createVM("pt-bad", h1, 1024)
	err = addPCI(vmBad, backingFor(m1, "0000:3b:00.0", m1.Hardware.SystemInfo.Uuid), types.VirtualMachineConfigSpec{})
	check("ReconfigVM 로 VirtualPCIPassthrough 추가", err == nil, errText(err))
	err = powerOn(vmBad)
	check("SystemId 틀림 → 전원 실패", err != nil && strings.Contains(err.Error(), "systemId does not match"), errText(err))
	check("메모리 예약 없음 → 같은 메시지에 원인", err != nil && strings.Contains(err.Error(), "memory reservation"), errText(err))
	fmt.Printf("     메시지: %s\n", errText(err))

	// 5. 올바른 값 + 메모리 100% 예약 → 성공
	vm1 := e.createVM("pt-vm1", h1, 1024)
	err = addPCI(vm1, backingFor(m1, "0000:3b:00.0", sid), types.VirtualMachineConfigSpec{MemoryAllocation: resv(1024)})
	check("pt-vm1 장치 추가", err == nil, errText(err))
	err = powerOn(vm1)
	check("올바른 backing + 예약 → 전원 성공", err == nil, errText(err))

	// 6. 이미 다른 VM 이 쓰는 장치 → 실패
	vm2 := e.createVM("pt-vm2", h1, 2048)
	_ = addPCI(vm2, backingFor(m1, "0000:3b:00.0", sid), types.VirtualMachineConfigSpec{MemoryReservationLockedToMax: &locked})
	err = powerOn(vm2)
	check("이미 사용 중 → 전원 실패", err != nil && strings.Contains(err.Error(), "already in use by VM 'pt-vm1'"), errText(err))

	// 7. 켜졌지만 활성 아님(재부팅 필요) → 실패, 재부팅 뒤 성공
	vm3 := e.createVM("pt-vm3", h1, 2048)
	_ = addPCI(vm3, backingFor(m1, "0000:3b:00.1", sid), types.VirtualMachineConfigSpec{MemoryReservationLockedToMax: &locked})
	err = powerOn(vm3)
	check("활성 아님 → 전원 실패", err != nil && strings.Contains(err.Error(), "host reboot required"), errText(err))
	fmt.Printf("     재부팅: %s\n", e.reboot("bm01"))
	pt = e.passthru(m1)
	check("재부팅 뒤 3b:00.1 active=true", pt["0000:3b:00.1"].PassthruActive, fmt.Sprint(pt["0000:3b:00.1"]))
	_, ok1 = ctSystemID(e.configTarget(h1), "0000:3b:00.1")
	check("재부팅 뒤 QueryConfigTarget 에 3b:00.1", ok1, "없음")
	err = powerOn(vm3)
	check("재부팅 뒤 전원 성공", err == nil, errText(err))

	// 8. NVIDIA: 64bit MMIO 없으면 실패, 넣으면 성공
	e.enable(m1, "0000:af:00.0")
	vm4 := e.createVM("pt-gpu", h1, 4096)
	_ = addPCI(vm4, backingFor(m1, "0000:af:00.0", sid), types.VirtualMachineConfigSpec{MemoryReservationLockedToMax: &locked})
	err = powerOn(vm4)
	check("NVIDIA 64bit MMIO 없음 → 전원 실패", err != nil && strings.Contains(err.Error(), "use64bitMMIO"), errText(err))
	// 8-1. MMIO 를 켰지만 BIOS 펌웨어 + 창 16GB(BAR 합 16GB+48MB 보다 작음) → 실패
	err = reconfig(vm4, types.VirtualMachineConfigSpec{Firmware: "bios", ExtraConfig: []types.BaseOptionValue{
		&types.OptionValue{Key: "pciPassthru.use64bitMMIO", Value: "TRUE"},
		&types.OptionValue{Key: "pciPassthru.64bitMMIOSizeGB", Value: "16"},
	}})
	check("extraConfig 추가 (bios, 16GB)", err == nil, errText(err))
	err = powerOn(vm4)
	check("NVIDIA 64bit MMIO + BIOS 펌웨어 → 전원 실패", err != nil && strings.Contains(err.Error(), "requires EFI firmware"), errText(err))
	check("NVIDIA 64bit MMIO 창 16GB → 전원 실패", err != nil && strings.Contains(err.Error(), "window too small"), errText(err))
	// 8-2. EFI + 64GB → 성공
	err = reconfig(vm4, types.VirtualMachineConfigSpec{Firmware: "efi", ExtraConfig: []types.BaseOptionValue{
		&types.OptionValue{Key: "pciPassthru.64bitMMIOSizeGB", Value: "64"},
	}})
	check("extraConfig 변경 (efi, 64GB)", err == nil, errText(err))
	err = powerOn(vm4)
	check("NVIDIA 64bit MMIO + EFI + 64GB → 전원 성공", err == nil, errText(err))

	// 9. bm02: DeviceId 틀림 + 다른 호스트 SystemId → 실패
	h2, m2 := e.host("bm02.saccae.com")
	e.enable(m2, "0000:3b:00.0")
	sid2, _ := ctSystemID(e.configTarget(h2), "0000:3b:00.0")
	check("호스트마다 ESXi system uuid 다름", sid2 != "" && sid2 != sid, sid2)
	// host 없이 부르면 클러스터 호스트 전부 (bm01 활성 3개 + bm02 활성 1개)
	all := e.configTarget(nil)
	check("host 없는 QueryConfigTarget → 두 호스트 장치 4개", len(all.PciPassthrough) == 4, fmt.Sprint(len(all.PciPassthrough)))
	vm5 := e.createVM("pt-vm5", h2, 1024)
	b := backingFor(m2, "0000:3b:00.0", sid)
	b.DeviceId = "1017"
	_ = addPCI(vm5, b, types.VirtualMachineConfigSpec{MemoryReservationLockedToMax: &locked})
	err = powerOn(vm5)
	check("DeviceId 틀림 → 전원 실패", err != nil && strings.Contains(err.Error(), "deviceId and vendorId do not match"), errText(err))
	check("다른 호스트 SystemId → 전원 실패", err != nil && strings.Contains(err.Error(), "systemId does not match"), errText(err))

	// 10. 패스스루 장치 없는 VM 은 그대로 켜짐
	vm6 := e.createVM("pt-plain", h2, 1024)
	check("패스스루 없는 VM 전원 성공", powerOn(vm6) == nil, "실패")

	// 11. 전원 정책 (vm_setup 의 power_setting 단계용 최소 흉내): 처음 Balanced → 고성능(key=1) 으로 변경
	var pw mo.HostSystem
	err = e.pc.RetrieveOne(ctx, h2.Reference(), []string{"configManager.powerSystem", "config.powerSystemInfo", "config.powerSystemCapability"}, &pw)
	if err != nil || pw.ConfigManager.PowerSystem == nil || pw.Config == nil || pw.Config.PowerSystemInfo == nil {
		check("전원 정책 조회", false, errText(err))
	} else {
		check("전원 정책 처음엔 Balanced(dynamic)", pw.Config.PowerSystemInfo.CurrentPolicy.ShortName == "dynamic", pw.Config.PowerSystemInfo.CurrentPolicy.ShortName)
		_, err = methods.ConfigurePowerPolicy(ctx, c.Client, &types.ConfigurePowerPolicy{This: *pw.ConfigManager.PowerSystem, Key: 1})
		check("ConfigurePowerPolicy key=1 성공", err == nil, errText(err))
		_ = e.pc.RetrieveOne(ctx, h2.Reference(), []string{"config.powerSystemInfo"}, &pw)
		check("전원 정책 → 고성능(static)", pw.Config != nil && pw.Config.PowerSystemInfo != nil && pw.Config.PowerSystemInfo.CurrentPolicy.ShortName == "static", fmt.Sprint(pw.Config))
	}

	if fails > 0 {
		fmt.Printf("결과: FAIL %d건\n", fails)
		os.Exit(1)
	}
	fmt.Println("결과: 전부 PASS")
}
