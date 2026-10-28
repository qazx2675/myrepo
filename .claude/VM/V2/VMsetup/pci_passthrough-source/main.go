// pci_passthrough: worklist 의 BM(ESXi 호스트)마다 <BM짧은이름>evNN VM 에 PCI 패스스루 장치를 1개씩 배분해
// 패스스루를 켜고(필요한 장치만) → 활성 확인 → VM 끄기(켜져 있으면) → 연결 → 전원 ON 까지 한다.
//
// -ev01Pci ~ -ev99Pci 에 vendor 를 준 ev 만 처리한다. 장치는 PCI 주소 순으로 ev 오름차순에 1개(function)씩 준다.
// 호스트끼리는 독립이라 워커풀로 병렬 처리한다(-concurrency). 한 호스트 안의 VM 은 차례로 처리한다.
//
// 연결 backing 의 SystemId 는 반드시 EnvironmentBrowser.QueryConfigTarget(host).PciPassthrough[].SystemId
// (= ESXi 의 `esxcli system uuid get` 값)를 쓴다. host.hardware.systemInfo.uuid(하드웨어/BIOS UUID)를 넣으면
// VM 전원이 켜지지 않는다 (2026-10-28 사용자 확인 원인). PowerCLI Add-PassthroughDevice 와 같은 방식이다.
//
// 전원 ON 전에는 VM 의 패스스루 장치 전부(이미 붙어 있던 것 포함)와 설정을 점검해, 고칠 것을 ReconfigVM 한 번에 고친다:
// backing SystemId/VendorId/DeviceId 를 QueryConfigTarget 값으로 교정, 메모리 100% 예약(lockedToMax),
// 큰 BAR 장치(NVIDIA 등)의 64bit MMIO. 바꿀 것이 없으면 ReconfigVM 하지 않는다. 켜져 있는 VM 은 건드리지 않는다.
// 전원 ON 이 실패하면 vCenter 메시지 전체와 사전 점검 결과(진단)를 보여 준다.
//
// 종료코드: 0 전부 성공/스킵, 3 문제가 "호스트 재부팅 필요" 뿐, 1 그 밖의 실패가 하나라도 있음.

package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"text/tabwriter"
	"time"

	"github.com/vmware/govmomi"
	"github.com/vmware/govmomi/object"
	"github.com/vmware/govmomi/property"
	"github.com/vmware/govmomi/task"
	"github.com/vmware/govmomi/view"
	"github.com/vmware/govmomi/vim25"
	"github.com/vmware/govmomi/vim25/methods"
	"github.com/vmware/govmomi/vim25/mo"
	"github.com/vmware/govmomi/vim25/types"
)

// maxGroup 은 ev 그룹 수(ev01~ev99)다. VM 이름이 ev%02d 두 자리라 99가 상한 (vm_create 와 같음).
const maxGroup = 99

var bomPrefix = string(rune(0xFEFF))

// 결과 상태. 요약 표에는 이 영문 코드가 그대로 나온다 (vm_setup 등에서 grep 하기 쉽게).
const (
	stOK     = "OK"
	stSkip   = "SKIP"
	stReboot = "REBOOT"
	stFail   = "FAIL"
)

// evReq 는 vendor 값이 있는 ev 그룹 하나다.
type evReq struct {
	ev     string // "ev01" ~ "ev99"
	vendor string
}

// vmResult 는 VM 1대의 처리 결과(요약 표 한 줄)다.
type vmResult struct {
	BM, VM, Vendor, Device, Status, Note string
}

// ---------------------------------------------------------------------------
// vendor 판정 — vm-param-check 와 같은 규칙
//
// 아래 MatchPCIVendor / parsePCIVendorID / isAllDigits / isHexString 은
// V2/vm-param-check-usability-improvement/vm-param-check/checker/pci.go 의 같은 이름 함수를 그대로 옮긴 것이다.
// 두 복사본은 반드시 똑같아야 한다 (pci_passthrough 가 붙인 장치를 vm-param-check 가 같은 규칙으로 OK 판정).
// 한쪽을 고치면 다른 쪽도 똑같이 고칠 것.

const (
	vendorIDMellanox uint16 = 0x15b3 // 5555
	vendorIDNvidia   uint16 = 0x10de // 4318
)

// MatchPCIVendor는 스펙의 pci-evNN 값(vendor)이 패스스루 장치 1개와 맞는지 판정한다.
//
// 규칙은 대소문자를 구분하지 않으며 위에서부터 먼저 맞는 것을 쓴다:
//
//  1. "mellanox"        -> VendorId 0x15b3 (5555)
//  2. "nvidia"          -> VendorId 0x10de (4318)
//  3. 10진 숫자만       -> VendorId 가 그 10진수와 같음      (예: 5555)
//  4. "0x" 로 시작하는 16진수, 또는 영문자(a-f)가 들어간 4자리 16진수
//     -> VendorId 가 그 16진수와 같음                       (예: 0x15b3, 15b3)
//  5. 그 밖의 값        -> 장치 이름(DeviceName)의 부분 문자열(대소문자 무시)
//
// VendorId 는 govmomi 에서 int16 이라 0x8086 처럼 최상위 비트가 켜진 값은 음수로 온다.
// 호출하는 쪽이 uint16 으로 바꿔서 넘기므로 여기서는 부호 문제가 없다.
func MatchPCIVendor(vendor string, vendorID uint16, deviceName string) bool {
	v := strings.ToLower(strings.TrimSpace(vendor))
	if v == "" {
		return false
	}
	switch v {
	case "mellanox":
		return vendorID == vendorIDMellanox
	case "nvidia":
		return vendorID == vendorIDNvidia
	}
	if id, ok := parsePCIVendorID(v); ok {
		return uint64(vendorID) == id
	}
	return strings.Contains(strings.ToLower(deviceName), v)
}

// parsePCIVendorID는 v(소문자, 공백 제거됨)가 VendorId 숫자 표기이면 그 값을 돌려준다.
// 10진 숫자만 / 0x 16진수 / a-f 가 들어간 4자리 16진수만 숫자로 보고, 나머지는 ok=false 다
// (그러면 장치 이름 부분 문자열로 찾는다). 값이 너무 커서 읽지 못하는 숫자도 어떤 VendorId 와도 안 맞게 ok=true, 최댓값을 준다.
func parsePCIVendorID(v string) (uint64, bool) {
	if strings.HasPrefix(v, "0x") {
		n, err := strconv.ParseUint(v[2:], 16, 64)
		if err != nil {
			return 0, false
		}
		return n, true
	}
	if isAllDigits(v) {
		n, err := strconv.ParseUint(v, 10, 64)
		if err != nil {
			return ^uint64(0), true // 64비트를 넘는 숫자 — 어떤 VendorId 와도 같지 않다
		}
		return n, true
	}
	if len(v) == 4 && isHexString(v) { // 위에서 10진 숫자만인 경우는 걸렀으므로 a-f 가 하나는 들어 있다
		n, _ := strconv.ParseUint(v, 16, 64)
		return n, true
	}
	return 0, false
}

func isAllDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func isHexString(s string) bool {
	for _, r := range s {
		if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'f') {
			return false
		}
	}
	return s != ""
}

// ---------------------------------------------------------------------------

func readLines(path string) ([]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	var lines []string
	scanner := bufio.NewScanner(file)
	first := true
	for scanner.Scan() {
		line := scanner.Text()
		if first {
			line = strings.TrimPrefix(line, bomPrefix)
			first = false
		}
		line = strings.TrimSpace(line)
		if line != "" && !strings.HasPrefix(line, "#") {
			lines = append(lines, line)
		}
	}
	return lines, scanner.Err()
}

// shortName은 호스트 이름의 첫 '.' 앞부분이다(FQDN이 아니면 그대로).
func shortName(name string) string {
	return strings.Split(name, ".")[0]
}

func main() {
	vcId := flag.String("id", "lscsystems@vsphere.local", "vCenter 로그인 계정 ID")
	vcTargetIP := flag.String("vcTargetIP", "", "vCenter 접속 IP (필수)")
	worklistFile := flag.String("worklistFile", "worklist.txt", "작업 대상 BM 목록 파일 (한 줄에 하나, 첫 칸만 사용)")
	concurrency := flag.Int("concurrency", 20, "동시에 처리할 호스트 수")
	mmioSizeGB := flag.Int("mmioSizeGB", 0, "큰 BAR 장치(NVIDIA 등)를 붙인 VM 의 pciPassthru.64bitMMIOSizeGB 최솟값 (2의 거듭제곱). 0 이면 자동: 장치 수×32 를 2의 거듭제곱으로 올림, 최소 64")

	// -ev01Pci ~ -ev99Pci: 이름 규칙이 같아서 반복문으로 등록한다(vm_create 의 -ev01Cpu 와 같은 모양).
	pciFlags := make([]*string, maxGroup+1) // 1-based
	for n := 1; n <= maxGroup; n++ {
		name := fmt.Sprintf("ev%02d", n)
		pciFlags[n] = flag.String(name+"Pci", "", fmt.Sprintf("[%s] 패스스루로 붙일 장치 vendor (mellanox, nvidia, VendorId 10진/16진, 그 밖은 장치 이름 일부). 비우면 %s 은 처리하지 않음", name, name))
	}
	collapseEvUsage(regexp.MustCompile(`^ev(\d{2})`))
	flag.Parse()

	if *vcTargetIP == "" {
		log.Fatal("필수 파라미터(-vcTargetIP)가 누락되었습니다.")
	}
	if *concurrency < 1 {
		log.Fatalf("-concurrency 값이 올바르지 않습니다: %d (1 이상)", *concurrency)
	}
	if n := *mmioSizeGB; n < 0 || n&(n-1) != 0 {
		log.Fatalf("-mmioSizeGB 값이 올바르지 않습니다: %d (0=자동, 또는 2의 거듭제곱 GB. 예: 64, 128, 256)", n)
	}
	var evs []evReq
	for n := 1; n <= maxGroup; n++ {
		if v := strings.TrimSpace(*pciFlags[n]); v != "" {
			evs = append(evs, evReq{ev: fmt.Sprintf("ev%02d", n), vendor: v})
		}
	}
	if len(evs) == 0 {
		log.Fatal("최소 하나의 그룹은 지정해야 합니다 (-ev01Pci ~ -ev99Pci 중 하나 이상).")
	}
	vcPassword := os.Getenv("VC_PASSWORD")
	if vcPassword == "" {
		log.Fatal("환경변수 'VC_PASSWORD'가 설정되지 않았습니다.")
	}

	baseDir, _ := os.Getwd()
	lines, err := readLines(filepath.Join(baseDir, *worklistFile))
	if err != nil {
		log.Fatalf("파일 로드 실패 (%s): %v", *worklistFile, err)
	}
	var bms []string
	seen := make(map[string]bool)
	for _, line := range lines {
		bm := strings.Fields(line)[0]
		if !seen[bm] {
			seen[bm] = true
			bms = append(bms, bm)
		}
	}
	if len(bms) == 0 {
		log.Fatalf("%s 에 BM 이 없습니다.", *worklistFile)
	}

	var evDesc []string
	for _, e := range evs {
		evDesc = append(evDesc, e.ev+"="+e.vendor)
	}
	fmt.Printf("\n[INFO] PCI 패스스루 연결 시작: BM %d대, %s (접속 계정: %s, 동시 처리 호스트 %d)\n",
		len(bms), strings.Join(evDesc, " "), *vcId, *concurrency)

	// 호스트마다 VM 전원 끄기·연결·켜기 Task 를 기다리므로 전체 시간 상한을 둔다 (vm_create 와 같음).
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Minute)
	defer cancel()

	u := &url.URL{Scheme: "https", Host: *vcTargetIP, Path: "/sdk"}
	u.User = url.UserPassword(*vcId, vcPassword)
	client, err := govmomi.NewClient(ctx, u, true)
	if err != nil {
		log.Fatalf("vCenter 접속 실패: %v", err)
	}
	defer client.Logout(ctx)

	// 호스트 이름은 한 번에 모아 온다. 장치·VM 같은 무거운 정보는 대상 호스트만 따로 읽는다.
	m := view.NewManager(client.Client)
	v, err := m.CreateContainerView(ctx, client.ServiceContent.RootFolder, []string{"HostSystem"}, true)
	if err != nil {
		log.Fatalf("[에러] 인벤토리 뷰 생성 실패: %v", err)
	}
	var allHosts []mo.HostSystem
	err = v.Retrieve(ctx, []string{"HostSystem"}, []string{"name"}, &allHosts)
	_ = v.Destroy(ctx)
	if err != nil {
		log.Fatalf("[에러] 호스트 목록 수집 실패: %v", err)
	}

	hostByName := make(map[string]*mo.HostSystem, len(allHosts))
	hostByShort := make(map[string][]*mo.HostSystem, len(allHosts))
	for i := range allHosts {
		if _, dup := hostByName[allHosts[i].Name]; !dup {
			hostByName[allHosts[i].Name] = &allHosts[i]
		}
		short := shortName(allHosts[i].Name)
		hostByShort[short] = append(hostByShort[short], &allHosts[i])
	}
	// 정확한 이름이 있으면 그것, 없으면 짧은 이름(첫 '.' 앞)끼리 비교해서 하나뿐일 때만 쓴다 (power_setting 과 같은 규칙).
	lookupHost := func(bm string) (*mo.HostSystem, string) {
		if h := hostByName[bm]; h != nil {
			return h, ""
		}
		cands := hostByShort[shortName(bm)]
		switch len(cands) {
		case 0:
			return nil, fmt.Sprintf("호스트 '%s' 를 vCenter 전체 인벤토리에서 찾을 수 없습니다", bm)
		case 1:
			return cands[0], ""
		default:
			return nil, fmt.Sprintf("호스트 '%s' 와 짧은 이름이 같은 호스트가 %d대 있어 어느 쪽인지 정할 수 없습니다 (vCenter에 보이는 이름 그대로 적어주세요)", bm, len(cands))
		}
	}

	pc := property.DefaultCollector(client.Client)
	results := make([][]vmResult, len(bms))
	var printMu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, *concurrency)
	for i, bm := range bms {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int, bm string) {
			defer wg.Done()
			defer func() { <-sem }()
			var out strings.Builder
			defer func() {
				printMu.Lock()
				fmt.Print(out.String())
				printMu.Unlock()
			}()

			h, lookupErr := lookupHost(bm)
			if h == nil {
				fmt.Fprintf(&out, "[에러] %s\n", lookupErr)
				results[i] = failAll(bm, evs, lookupErr)
				return
			}
			hp := &hostProc{ctx: ctx, c: client.Client, pc: pc, ref: h.Self, name: h.Name, bm: bm, evs: evs, mmioGB: *mmioSizeGB, out: &out}
			results[i] = hp.run()
		}(i, bm)
	}
	wg.Wait()

	// ---- 요약 ----
	var all []vmResult
	for _, rs := range results {
		all = append(all, rs...)
	}
	count := map[string]int{}
	rebootHosts := map[string][]string{} // BM -> 재부팅 뒤 활성이 될 장치
	var rebootOrder []string
	for _, r := range all {
		count[r.Status]++
		if r.Status == stReboot {
			if _, ok := rebootHosts[r.BM]; !ok {
				rebootOrder = append(rebootOrder, r.BM)
			}
			rebootHosts[r.BM] = append(rebootHosts[r.BM], r.Device)
		}
	}

	fmt.Println("\n======================================================================")
	fmt.Println(" [PCI 패스스루 결과 요약]")
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, " BM\tVM\tvendor\t장치\t결과\t비고")
	for _, r := range all {
		dev := r.Device
		if dev == "" {
			dev = "-"
		}
		fmt.Fprintf(w, " %s\t%s\t%s\t%s\t%s\t%s\n", r.BM, r.VM, r.Vendor, dev, r.Status, r.Note)
	}
	w.Flush()
	fmt.Println("======================================================================")
	for _, bm := range rebootOrder {
		fmt.Printf("[경고] 호스트 재부팅 필요: %s (장치 %s) — 재부팅 후 같은 명령을 다시 실행하면 이어서 연결·전원 ON 합니다.\n",
			bm, strings.Join(rebootHosts[bm], ", "))
	}
	fmt.Printf("\n[INFO] PCI 패스스루 완료 — 성공 %d / 스킵(이미 연결·켜짐) %d / 재부팅 필요 %d / 실패 %d\n",
		count[stOK], count[stSkip], count[stReboot], count[stFail])

	code := 0
	if count[stFail] > 0 {
		code = 1
	} else if count[stReboot] > 0 {
		code = 3
	}
	if code != 0 {
		// defer 가 os.Exit 로 건너뛰어지므로 세션 종료를 여기서 직접 한다.
		client.Logout(ctx)
		os.Exit(code)
	}
}

// failAll 은 호스트 단위 오류로 그 BM 의 ev 전부를 실패로 기록한다.
func failAll(bm string, evs []evReq, why string) []vmResult {
	var rs []vmResult
	for _, e := range evs {
		rs = append(rs, vmResult{BM: bm, VM: shortName(bm) + e.ev, Vendor: e.vendor, Status: stFail, Note: why})
	}
	return rs
}

// ---------------------------------------------------------------------------
// 호스트 1대 처리

type hostProc struct {
	ctx  context.Context
	c    *vim25.Client
	pc   *property.Collector
	ref  types.ManagedObjectReference
	name string // vCenter 에 보이는 호스트 이름
	bm   string // worklist 에 적힌 이름
	evs  []evReq
	sys  types.ManagedObjectReference // HostPciPassthruSystem
	// mmioGB 는 -mmioSizeGB 값(0 이면 자동)이다
	mmioGB int
	out    *strings.Builder
}

// vmProps 는 VM 에서 읽는 속성이다. 전원 ON 전 점검(planBoot)에 메모리·펌웨어·extraConfig 가 필요하다.
var vmProps = []string{"name", "runtime.powerState", "config.hardware", "config.firmware", "config.extraConfig",
	"config.memoryAllocation", "config.memoryReservationLockedToMax", "environmentBrowser"}

// vmPlan 은 VM 1대에 대한 배분 결과다.
type vmPlan struct {
	res      *vmResult
	vm       *mo.VirtualMachine
	devID    string // 배분한 장치 (비면 배분 못 함 → res 가 이미 FAIL)
	attached bool   // 이미 VM 에 붙어 있던 장치를 그대로 쓰는 경우
}

func (p *hostProc) logf(format string, a ...interface{}) {
	fmt.Fprintf(p.out, format, a...)
}

// result 는 VM 결과 한 줄을 출력하고 기록한다.
func (p *hostProc) result(pl *vmPlan, status, note string) {
	pl.res.Status, pl.res.Note = status, note
	label := map[string]string{stOK: "성공", stSkip: "스킵", stReboot: "재부팅 필요", stFail: "실패"}[status]
	dev := ""
	if pl.res.Device != "" {
		dev = pl.res.Device + " "
	}
	p.logf("  -> [%s] %s: %s%s\n", pl.res.VM, label, dev, note)
}

// passthroughIDs 는 VM 에 붙은 PCI 패스스루 장치(일반 backing)의 PCI 주소 목록이다.
func passthroughIDs(vm *mo.VirtualMachine) []string {
	if vm.Config == nil {
		return nil
	}
	var ids []string
	for _, d := range vm.Config.Hardware.Device {
		pt, ok := d.(*types.VirtualPCIPassthrough)
		if !ok {
			continue
		}
		if b, ok := pt.Backing.(*types.VirtualPCIPassthroughDeviceBackingInfo); ok && b.Id != "" {
			ids = append(ids, b.Id)
		}
	}
	return ids
}

// readPassthruInfo 는 호스트의 pciPassthruInfo 를 PCI 주소 -> 정보 맵으로 읽는다.
func (p *hostProc) readPassthruInfo(sys types.ManagedObjectReference) (map[string]types.HostPciPassthruInfo, error) {
	var ps mo.HostPciPassthruSystem
	if err := p.pc.RetrieveOne(p.ctx, sys, []string{"pciPassthruInfo"}, &ps); err != nil {
		return nil, err
	}
	m := make(map[string]types.HostPciPassthruInfo, len(ps.PciPassthruInfo))
	for _, b := range ps.PciPassthruInfo {
		if b == nil {
			continue
		}
		i := b.GetHostPciPassthruInfo()
		m[i.Id] = *i
	}
	return m, nil
}

func (p *hostProc) run() []vmResult {
	short := shortName(p.bm)

	// 1. 수집: PCI 장치, 업링크(vmnic) PCI 주소, 패스스루 상태, 호스트 위 VM 과 그 패스스루 장치
	var h mo.HostSystem
	err := p.pc.RetrieveOne(p.ctx, p.ref, []string{"name", "hardware.pciDevice", "config.network.pnic", "configManager.pciPassthruSystem", "vm"}, &h)
	if err != nil {
		why := fmt.Sprintf("호스트 '%s' 정보 조회 실패: %v", p.name, err)
		p.logf("[에러] %s\n", why)
		return failAll(p.bm, p.evs, why)
	}
	if h.Hardware == nil || h.Config == nil || h.Config.Network == nil || h.ConfigManager.PciPassthruSystem == nil {
		why := fmt.Sprintf("호스트 '%s' 의 PCI 장치/네트워크/패스스루 시스템을 조회할 수 없습니다 (연결 끊김/미지원)", p.name)
		p.logf("[에러] %s\n", why)
		return failAll(p.bm, p.evs, why)
	}
	sysRef := *h.ConfigManager.PciPassthruSystem
	p.sys = sysRef

	devByID := map[string]types.HostPciDevice{}
	devs := append([]types.HostPciDevice(nil), h.Hardware.PciDevice...)
	sort.Slice(devs, func(i, j int) bool { return devs[i].Id < devs[j].Id }) // PCI 주소 순 (고정 길이 16진이라 문자열 순서 = 주소 순서)
	for _, d := range devs {
		devByID[d.Id] = d
	}
	// 호스트 업링크로 쓰는 물리 NIC(관리망 vmnic 등)는 절대 패스스루로 켜지 않는다 (켜면 호스트 네트워크가 끊김).
	pnicByPci := map[string]string{}
	for _, pn := range h.Config.Network.Pnic {
		if pn.Pci != "" {
			pnicByPci[pn.Pci] = pn.Device
		}
	}
	ptInfo, err := p.readPassthruInfo(sysRef)
	if err != nil {
		why := fmt.Sprintf("호스트 '%s' 패스스루 상태(pciPassthruInfo) 조회 실패: %v", p.name, err)
		p.logf("[에러] %s\n", why)
		return failAll(p.bm, p.evs, why)
	}

	var vms []mo.VirtualMachine
	if len(h.Vm) > 0 {
		if err := p.pc.Retrieve(p.ctx, h.Vm, vmProps, &vms); err != nil {
			why := fmt.Sprintf("호스트 '%s' 의 VM 정보 조회 실패: %v", p.name, err)
			p.logf("[에러] %s\n", why)
			return failAll(p.bm, p.evs, why)
		}
	}
	vmByName := map[string]*mo.VirtualMachine{}
	usedBy := map[string]string{} // PCI 주소 -> 그 장치를 붙이고 있는 VM 이름
	for i := range vms {
		vmByName[vms[i].Name] = &vms[i]
		for _, id := range passthroughIDs(&vms[i]) {
			usedBy[id] = vms[i].Name
		}
	}

	// 2. 배분: ev 오름차순. 이미 vendor 에 맞는 장치가 붙어 있으면 그대로 쓰고, 아니면 남는 장치 중 PCI 주소가 가장 앞선 것.
	plans := make([]*vmPlan, len(p.evs))
	taken := map[string]bool{} // 이번 실행에서 다른 ev 에 준 장치
	for i, e := range p.evs {
		pl := &vmPlan{res: &vmResult{BM: p.bm, VM: short + e.ev, Vendor: e.vendor}}
		plans[i] = pl
		vm := vmByName[pl.res.VM]
		if vm == nil {
			p.result(pl, stFail, fmt.Sprintf("VM 없음 (호스트 %s 위에 %s 이 없습니다)", p.name, pl.res.VM))
			continue
		}
		pl.vm = vm
		for _, id := range passthroughIDs(vm) {
			if d, ok := devByID[id]; ok && MatchPCIVendor(e.vendor, uint16(d.VendorId), d.DeviceName) {
				pl.devID, pl.attached = id, true
				break
			}
		}
		if pl.devID == "" {
			var matched, otherVM, uplink, incapable int
			for _, d := range devs {
				if !MatchPCIVendor(e.vendor, uint16(d.VendorId), d.DeviceName) {
					continue
				}
				matched++
				if _, isPnic := pnicByPci[d.Id]; isPnic {
					uplink++
					continue
				}
				if (usedBy[d.Id] != "" && usedBy[d.Id] != vm.Name) || taken[d.Id] {
					otherVM++
					continue
				}
				if info, ok := ptInfo[d.Id]; !ok || !info.PassthruCapable {
					incapable++
					continue
				}
				pl.devID = d.Id
				break
			}
			if pl.devID == "" {
				p.result(pl, stFail, fmt.Sprintf("사용 가능한 %s 장치 없음 (일치 %d개: 다른 VM 사용 %d, 호스트 업링크(vmnic) %d, 패스스루 불가 %d)",
					e.vendor, matched, otherVM, uplink, incapable))
				continue
			}
		}
		taken[pl.devID] = true
		d := devByID[pl.devID]
		pl.res.Device = pl.devID
		pl.res.Note = d.DeviceName
	}

	// 3. 활성화: 이번에 새로 배분한 장치 중 아직 패스스루가 꺼진 것만 켠다 (같은 벤더라도 남는 장치는 건드리지 않음).
	var toEnable []types.BaseHostPciPassthruConfig
	var enableIDs []string
	for _, pl := range plans {
		if pl.devID == "" || pl.attached || ptInfo[pl.devID].PassthruEnabled {
			continue
		}
		if _, isPnic := pnicByPci[pl.devID]; isPnic { // 배분 단계에서 걸렀지만 한 번 더 막는다
			p.result(pl, stFail, "호스트 업링크(vmnic) 장치라 패스스루로 켜지 않습니다")
			pl.devID = ""
			continue
		}
		toEnable = append(toEnable, &types.HostPciPassthruConfig{Id: pl.devID, PassthruEnabled: true})
		enableIDs = append(enableIDs, pl.devID)
	}
	if len(toEnable) > 0 {
		_, err := methods.UpdatePassthruConfig(p.ctx, p.c, &types.UpdatePassthruConfig{This: sysRef, Config: toEnable})
		if err != nil {
			p.logf("  -> [%s] 패스스루 활성화 실패 (%s): %v\n", p.name, strings.Join(enableIDs, ", "), err)
			for _, pl := range plans {
				for _, id := range enableIDs {
					if pl.devID == id && pl.res.Status == "" {
						p.result(pl, stFail, fmt.Sprintf("패스스루 활성화 실패: %v", err))
						pl.devID = ""
					}
				}
			}
		} else {
			p.logf("  -> [%s] 패스스루 켬: %s\n", p.name, strings.Join(enableIDs, ", "))
		}
		if ptInfo, err = p.readPassthruInfo(sysRef); err != nil {
			why := fmt.Sprintf("패스스루 활성화 뒤 상태 재조회 실패: %v", err)
			for _, pl := range plans {
				if pl.devID != "" && pl.res.Status == "" {
					p.result(pl, stFail, why)
				}
			}
			return collect(plans)
		}
	}

	// 4~5. VM 마다: 활성 확인 → (켜져 있으면 끄기) → 연결 → 전원 ON
	for _, pl := range plans {
		if pl.devID == "" || pl.res.Status != "" {
			continue
		}
		p.attachAndPowerOn(pl, ptInfo[pl.devID])
	}
	return collect(plans)
}

func collect(plans []*vmPlan) []vmResult {
	rs := make([]vmResult, len(plans))
	for i, pl := range plans {
		rs[i] = *pl.res
	}
	return rs
}

// attachAndPowerOn 은 배분이 끝난 VM 1대를 점검·연결·전원 ON 한다.
//
// 순서: (이미 연결됨 + 켜짐 → 건드리지 않음) → 장치 활성 확인 → 전원 ON 전 점검(planBoot, VM 은 아직 그대로)
// → 켜져 있으면 끄기 → 고칠 것을 ReconfigVM 한 번에 → 전원 ON → 실패하면 진단.
// 점검에서 막히면(BIOS 펌웨어, QueryConfigTarget 에 없는 장치 등) VM 을 끄지도 켜지도 않는다.
func (p *hostProc) attachAndPowerOn(pl *vmPlan, info types.HostPciPassthruInfo) {
	vm := pl.vm
	on := vm.Runtime.PowerState == types.VirtualMachinePowerStatePoweredOn
	if pl.attached && on {
		// 켜져 있는 VM 은 이미 부팅된 것이라 전원을 껐다 켜지 않는다. 다음 전원 ON 때 걸릴 것만 알려 준다.
		note := "이미 연결됨, 전원 켜짐"
		if warn := p.checkOnly(vm); warn != "" {
			note += " — [주의] 켜진 VM 이라 그대로 둠, 다음 전원 ON 전에 확인: " + warn
		}
		p.result(pl, stSkip, note)
		return
	}
	if !info.PassthruActive {
		why := "패스스루 켜짐, 아직 활성 아님 — 호스트 재부팅 필요"
		if !info.PassthruEnabled {
			why = "패스스루가 꺼져 있음 — 호스트 패스스루 설정 확인"
			if pl.attached {
				p.result(pl, stFail, "VM 에 붙어 있으나 "+why)
				return
			}
		}
		p.result(pl, stReboot, why)
		return
	}

	addID := "" // 새로 붙일 장치 (이미 붙어 있으면 비움)
	if !pl.attached {
		addID = pl.devID
	}
	qct, err := queryPassthroughTargets(p.ctx, p.c, vm, p.ref)
	if err != nil {
		p.result(pl, stFail, err.Error())
		return
	}
	spec, changes, err := planBoot(vm, qct, addID, p.mmioGB, false)
	if err != nil {
		p.result(pl, stFail, "전원 ON 전 점검: "+err.Error())
		return
	}

	vmObj := object.NewVirtualMachine(p.c, vm.Self)
	var steps []string
	if pl.attached {
		steps = append(steps, "이미 연결됨")
	}
	if on { // 여기까지 왔으면 아직 연결 전이다 (연결됨+켜짐은 위에서 스킵)
		t, err := vmObj.PowerOff(p.ctx)
		if err == nil {
			_, err = t.WaitForResult(p.ctx)
		}
		if err != nil {
			p.result(pl, stFail, "연결 전 전원 끄기 실패: "+faultText(err))
			return
		}
		steps = append(steps, "전원 끔")
	}
	if len(changes) > 0 {
		err := reconfigure(p.ctx, vmObj, spec)
		if err != nil && hasEdit(spec) {
			// backing 수정(Edit)을 받지 않는 환경이면 그 장치를 빼고 새로 붙이는 방식으로 한 번 더 한다.
			p.logf("  -> [%s] 장치 backing 수정(Edit) 실패, 제거 후 다시 연결로 재시도: %s\n", vm.Name, faultText(err))
			spec, changes, _ = planBoot(vm, qct, addID, p.mmioGB, true)
			changes = append(changes, "Edit 대신 제거 후 다시 연결")
			err = reconfigure(p.ctx, vmObj, spec)
		}
		if err != nil {
			p.result(pl, stFail, strings.Join(append(steps, "설정 변경(ReconfigVM) 실패: "+faultText(err)), " → "))
			return
		}
		steps = append(steps, "설정 변경["+strings.Join(changes, "; ")+"]")
	}
	if err := powerOnAndReport(p.ctx, vmObj); err != nil {
		diag := p.diagnose(vm.Self)
		p.logf("  -> [%s] 전원 ON 실패 — vCenter 메시지: %s\n", vm.Name, err)
		for _, d := range diag {
			p.logf("       진단: %s\n", d)
		}
		p.result(pl, stFail, strings.Join(append(steps, "전원 ON 실패 (진단: "+strings.Join(diag, "; ")+")"), " → "))
		return
	}
	steps = append(steps, "전원 ON")
	p.result(pl, stOK, strings.Join(steps, " → "))
}

// checkOnly 는 켜져 있는 VM 을 건드리지 않고, 다음 전원 ON 때 걸릴 설정을 한 줄로 돌려준다 (없으면 "").
func (p *hostProc) checkOnly(vm *mo.VirtualMachine) string {
	qct, err := queryPassthroughTargets(p.ctx, p.c, vm, p.ref)
	if err != nil {
		return err.Error()
	}
	_, changes, err := planBoot(vm, qct, "", p.mmioGB, false)
	if err != nil {
		return err.Error()
	}
	return strings.Join(changes, "; ")
}

// diagnose 는 전원 ON 이 실패한 뒤 VM·호스트 상태를 다시 읽어 알려진 실패 원인을 찾는다.
// 전원 ON 전 점검(planBoot)과 같은 규칙에, 장치 활성(호스트 재부팅 필요)과 다른 VM 사용 여부를 더 본다.
func (p *hostProc) diagnose(ref types.ManagedObjectReference) []string {
	var vm mo.VirtualMachine
	if err := p.pc.RetrieveOne(p.ctx, ref, vmProps, &vm); err != nil {
		return []string{"VM 상태 재조회 실패: " + err.Error()}
	}
	ids := passthroughIDs(&vm)
	var diag []string

	// 1. 장치 활성 — 켜졌지만 활성이 아니면 호스트 재부팅이 필요하다
	if pt, err := p.readPassthruInfo(p.sys); err == nil {
		for _, id := range ids {
			switch i, ok := pt[id]; {
			case !ok:
				diag = append(diag, fmt.Sprintf("장치 %s 가 이 호스트에 없음 (다른 호스트에서 붙인 장치?)", id))
			case !i.PassthruEnabled:
				diag = append(diag, fmt.Sprintf("장치 %s 패스스루 꺼짐", id))
			case !i.PassthruActive:
				diag = append(diag, fmt.Sprintf("장치 %s 패스스루 켜짐·활성 아님 → 호스트 재부팅 필요", id))
			}
		}
	}
	// 2. backing(SystemId·Vendor/DeviceId)·메모리 예약·64bit MMIO·펌웨어
	if qct, err := queryPassthroughTargets(p.ctx, p.c, &vm, p.ref); err == nil {
		if _, changes, err := planBoot(&vm, qct, "", p.mmioGB, false); err != nil {
			diag = append(diag, err.Error())
		} else {
			for _, c := range changes {
				diag = append(diag, "설정이 맞지 않음: "+c)
			}
		}
	}
	// 3. 같은 장치를 쓰는 켜진 다른 VM
	var h mo.HostSystem
	if err := p.pc.RetrieveOne(p.ctx, p.ref, []string{"vm"}, &h); err == nil && len(h.Vm) > 0 {
		var vms []mo.VirtualMachine
		if err := p.pc.Retrieve(p.ctx, h.Vm, []string{"name", "runtime.powerState", "config.hardware.device"}, &vms); err == nil {
			for i := range vms {
				o := &vms[i]
				if o.Self == ref || o.Runtime.PowerState != types.VirtualMachinePowerStatePoweredOn {
					continue
				}
				for _, oid := range passthroughIDs(o) {
					for _, id := range ids {
						if oid == id {
							diag = append(diag, fmt.Sprintf("장치 %s 를 켜져 있는 다른 VM '%s' 가 쓰고 있음", id, o.Name))
						}
					}
				}
			}
		}
	}
	if len(diag) == 0 {
		diag = append(diag, "사전 점검(장치 활성·SystemId·Vendor/DeviceId·메모리 예약·64bit MMIO·펌웨어·다른 VM 사용)으로는 원인을 찾지 못함 — vCenter 메시지를 확인하세요")
	}
	return diag
}

// ---------------------------------------------------------------------------
// 전원 ON 전 점검 — 정상 부팅에 필요한 VM 설정

// largeBARVendors 는 BAR(장치 메모리 창)가 커서 64bit MMIO 가 있어야 전원이 켜지는 장치의 VendorId 다.
// GPU·가속기 벤더를 더하려면 여기에만 한 줄 넣으면 된다 (예: AMD GPU 0x1002).
var largeBARVendors = map[uint16]string{
	vendorIDNvidia: "NVIDIA",
}

// 64bit MMIO 크기 자동 계산 (README "64bit MMIO" 참고):
// 큰 BAR 장치 1개에 32GB 를 잡고(VRAM 16GB 이하 GPU 의 BAR1 16GB + BAR0/BAR3 를 담는 2의 거듭제곱),
// 합을 2의 거듭제곱으로 올리되 최소 64GB 로 한다. -mmioSizeGB 로 최솟값을 바꿀 수 있다.
const (
	mmioPerDeviceGB = 32
	mmioMinGB       = 64
)

// mmioSizeFor 는 큰 BAR 장치 n개에 필요한 pciPassthru.64bitMMIOSizeGB 다 (n×32 를 2의 거듭제곱으로 올림, 최소 64).
func mmioSizeFor(n int) int {
	size := mmioMinGB
	for size < n*mmioPerDeviceGB {
		size *= 2
	}
	return size
}

// queryPassthroughTargets 는 VM 의 EnvironmentBrowser.QueryConfigTarget(host).PciPassthrough 를 장치 Id -> 정보로 돌려준다.
// 패스스루가 활성(Active)인 장치만 나오며, SystemId 는 ESXi system uuid(`esxcli system uuid get` 값)다.
func queryPassthroughTargets(ctx context.Context, c *vim25.Client, vm *mo.VirtualMachine, host types.ManagedObjectReference) (map[string]*types.VirtualMachinePciPassthroughInfo, error) {
	if vm.EnvironmentBrowser.Value == "" {
		return nil, fmt.Errorf("VM 의 EnvironmentBrowser 를 알 수 없어 장치 정보(QueryConfigTarget)를 조회하지 못했습니다")
	}
	eb := object.NewEnvironmentBrowser(c, vm.EnvironmentBrowser)
	ct, err := eb.QueryConfigTarget(ctx, object.NewHostSystem(c, host))
	if err != nil {
		return nil, fmt.Errorf("QueryConfigTarget 실패: %v", err)
	}
	m := map[string]*types.VirtualMachinePciPassthroughInfo{}
	if ct != nil {
		for _, b := range ct.PciPassthrough {
			if b == nil {
				continue
			}
			i := b.GetVirtualMachinePciPassthroughInfo()
			// 같은 Id 가 SR-IOV 정보로도 나오면 일반 패스스루 항목을 쓴다
			if _, plain := b.(*types.VirtualMachinePciPassthroughInfo); plain || m[i.PciDevice.Id] == nil {
				m[i.PciDevice.Id] = i
			}
		}
	}
	return m, nil
}

// newBacking 은 QueryConfigTarget 항목으로 패스스루 backing 을 만든다. 하드웨어 UUID 는 어디에도 쓰지 않는다.
func newBacking(info *types.VirtualMachinePciPassthroughInfo) *types.VirtualPCIPassthroughDeviceBackingInfo {
	return &types.VirtualPCIPassthroughDeviceBackingInfo{
		VirtualDeviceDeviceBackingInfo: types.VirtualDeviceDeviceBackingInfo{
			DeviceName:    info.PciDevice.DeviceName,
			UseAutoDetect: types.NewBool(false),
		},
		Id:       info.PciDevice.Id,
		DeviceId: fmt.Sprintf("%x", uint16(info.PciDevice.DeviceId)),
		SystemId: info.SystemId,
		VendorId: info.PciDevice.VendorId,
	}
}

// backingDiff 는 VM 에 붙어 있는 backing 이 QueryConfigTarget 값과 다른 점을 돌려준다 (같으면 nil).
func backingDiff(b *types.VirtualPCIPassthroughDeviceBackingInfo, info *types.VirtualMachinePciPassthroughInfo) []string {
	var d []string
	if !strings.EqualFold(b.SystemId, info.SystemId) {
		old := b.SystemId
		if old == "" {
			old = "(비어 있음)"
		}
		d = append(d, fmt.Sprintf("SystemId 교정 (%s → %s)", old, info.SystemId))
	}
	if uint16(b.VendorId) != uint16(info.PciDevice.VendorId) {
		d = append(d, fmt.Sprintf("VendorId 교정 (%04x → %04x)", uint16(b.VendorId), uint16(info.PciDevice.VendorId)))
	}
	if id, ok := parseHexID(b.DeviceId); !ok || id != uint16(info.PciDevice.DeviceId) {
		d = append(d, fmt.Sprintf("DeviceId 교정 (%q → %x)", b.DeviceId, uint16(info.PciDevice.DeviceId)))
	}
	return d
}

// parseHexID 는 backing.DeviceId 같은 16진 문자열("101b", "0x101b")을 읽는다.
func parseHexID(s string) (uint16, bool) {
	s = strings.TrimPrefix(strings.ToLower(strings.TrimSpace(s)), "0x")
	v, err := strconv.ParseUint(s, 16, 16)
	return uint16(v), err == nil
}

// extraValue 는 extraConfig 에서 key(대소문자 무시) 값을 돌려준다 (없으면 "").
func extraValue(opts []types.BaseOptionValue, key string) string {
	for _, o := range opts {
		if v := o.GetOptionValue(); strings.EqualFold(v.Key, key) {
			s, _ := v.Value.(string)
			return strings.TrimSpace(s)
		}
	}
	return ""
}

// planBoot 는 전원 ON 이 되도록 VM 설정에서 고칠 것을 ReconfigVM 스펙 하나로 모은다. VM 은 바꾸지 않는다.
//
//  1. 이미 붙어 있는 패스스루 장치 전부: backing 의 SystemId/VendorId/DeviceId 가 QueryConfigTarget(현재 호스트) 값과
//     다르면 교정한다(예전 스크립트가 하드웨어 UUID 를 넣은 VM, 다른 호스트에서 붙인 VM). 기본은 그 장치의 backing 만
//     바꾸고(Edit, 게스트에서 보이는 PCI 슬롯 유지), replace=true 면 장치를 빼고 새로 붙인다.
//     장치가 QueryConfigTarget 에 없으면(패스스루 비활성·재부팅 대기·이 호스트에 없는 장치) 오류다.
//  2. addID 가 있으면 그 장치를 QueryConfigTarget 값으로 새로 붙인다.
//  3. 큰 BAR 장치(largeBARVendors)가 있으면 EFI 펌웨어인지 보고(아니면 오류), 64bit MMIO 를 켠다.
//  4. 메모리 예약이 메모리 크기보다 작고 lockedToMax 도 아니면 lockedToMax=true (예약 100%).
//
// changes 가 비면 고칠 것이 없다(ReconfigVM 하지 않음). 오류면 전원을 켜도 안 되는 상태다.
func planBoot(vm *mo.VirtualMachine, qct map[string]*types.VirtualMachinePciPassthroughInfo, addID string, mmioGB int, replace bool) (types.VirtualMachineConfigSpec, []string, error) {
	var spec types.VirtualMachineConfigSpec
	var changes []string
	cfg := vm.Config
	if cfg == nil {
		return spec, nil, fmt.Errorf("VM 설정(config)을 읽지 못했습니다")
	}
	lookup := func(id string) (*types.VirtualMachinePciPassthroughInfo, error) {
		info := qct[id]
		if info == nil {
			return nil, fmt.Errorf("장치 %s 가 이 호스트의 QueryConfigTarget 에 없습니다 (패스스루 꺼짐·호스트 재부팅 대기, 또는 다른 호스트에서 붙인 장치)", id)
		}
		if info.SystemId == "" {
			return nil, fmt.Errorf("QueryConfigTarget 이 장치 %s 의 SystemId 를 주지 않았습니다", id)
		}
		return info, nil
	}
	var large []string // 큰 BAR 장치 "Id(벤더)"
	countLarge := func(info *types.VirtualMachinePciPassthroughInfo) {
		if name, ok := largeBARVendors[uint16(info.PciDevice.VendorId)]; ok {
			large = append(large, info.PciDevice.Id+"("+name+")")
		}
	}
	key := int32(-100) // 새 장치 임시 키

	// 1. 이미 붙어 있는 장치
	for _, d := range cfg.Hardware.Device {
		pt, ok := d.(*types.VirtualPCIPassthrough)
		if !ok {
			continue
		}
		b, ok := pt.Backing.(*types.VirtualPCIPassthroughDeviceBackingInfo)
		if !ok || b.Id == "" {
			continue // 동적 DirectPath 등 다른 backing 은 건드리지 않는다
		}
		info, err := lookup(b.Id)
		if err != nil {
			return spec, nil, fmt.Errorf("VM 에 붙어 있는 %v", err)
		}
		countLarge(info)
		diff := backingDiff(b, info)
		if len(diff) == 0 {
			continue
		}
		if replace {
			nd := &types.VirtualPCIPassthrough{VirtualDevice: types.VirtualDevice{Key: key, Backing: newBacking(info)}}
			key--
			spec.DeviceChange = append(spec.DeviceChange,
				&types.VirtualDeviceConfigSpec{Operation: types.VirtualDeviceConfigSpecOperationRemove, Device: pt},
				&types.VirtualDeviceConfigSpec{Operation: types.VirtualDeviceConfigSpecOperationAdd, Device: nd})
		} else {
			ed := *pt
			ed.Backing = newBacking(info)
			spec.DeviceChange = append(spec.DeviceChange,
				&types.VirtualDeviceConfigSpec{Operation: types.VirtualDeviceConfigSpecOperationEdit, Device: &ed})
		}
		changes = append(changes, b.Id+" "+strings.Join(diff, ", "))
	}

	// 2. 새로 붙일 장치
	if addID != "" {
		info, err := lookup(addID)
		if err != nil {
			return spec, nil, err
		}
		countLarge(info)
		nd := &types.VirtualPCIPassthrough{VirtualDevice: types.VirtualDevice{Key: key, Backing: newBacking(info)}}
		spec.DeviceChange = append(spec.DeviceChange,
			&types.VirtualDeviceConfigSpec{Operation: types.VirtualDeviceConfigSpecOperationAdd, Device: nd})
		changes = append(changes, fmt.Sprintf("연결 %s (SystemId=%s)", addID, info.SystemId))
	}

	// 3. 큰 BAR 장치 → EFI 펌웨어 + 64bit MMIO
	if len(large) > 0 {
		if fw := strings.ToLower(cfg.Firmware); fw != string(types.GuestOsDescriptorFirmwareTypeEfi) {
			if fw == "" {
				fw = "bios"
			}
			return spec, nil, fmt.Errorf("펌웨어가 %s 라 큰 BAR 장치 %s 에 필요한 64bit MMIO 를 쓸 수 없습니다. 펌웨어를 바꾸면 설치된 OS 가 부팅되지 않으므로 자동으로 바꾸지 않습니다 — OS 설치 전이면 EFI 로 바꾼 뒤(vm_create -firmware=efi 로 다시 만들기 또는 govc vm.change -firmware efi) 다시 실행하세요",
				fw, strings.Join(large, ", "))
		}
		need := mmioGB
		if need == 0 {
			need = mmioSizeFor(len(large))
		}
		var opts []types.BaseOptionValue
		if !strings.EqualFold(extraValue(cfg.ExtraConfig, "pciPassthru.use64bitMMIO"), "TRUE") {
			opts = append(opts, &types.OptionValue{Key: "pciPassthru.use64bitMMIO", Value: "TRUE"})
		}
		curStr := extraValue(cfg.ExtraConfig, "pciPassthru.64bitMMIOSizeGB")
		if cur, err := strconv.Atoi(curStr); err != nil || cur < need { // 이미 더 큰 값이면 그대로 둔다
			opts = append(opts, &types.OptionValue{Key: "pciPassthru.64bitMMIOSizeGB", Value: strconv.Itoa(need)})
		} else {
			need = cur
		}
		if len(opts) > 0 {
			if curStr == "" {
				curStr = "없음"
			}
			spec.ExtraConfig = opts
			changes = append(changes, fmt.Sprintf("64bit MMIO 켬 (pciPassthru.64bitMMIOSizeGB=%d, 이전 %s)", need, curStr))
		}
	}

	// 4. 메모리 100% 예약
	memMB := int64(cfg.Hardware.MemoryMB)
	var resv int64
	if cfg.MemoryAllocation != nil && cfg.MemoryAllocation.Reservation != nil {
		resv = *cfg.MemoryAllocation.Reservation
	}
	locked := cfg.MemoryReservationLockedToMax != nil && *cfg.MemoryReservationLockedToMax
	if !locked && resv < memMB {
		spec.MemoryReservationLockedToMax = types.NewBool(true)
		changes = append(changes, fmt.Sprintf("메모리 100%% 예약 (예약 %dMB → %dMB, lockedToMax)", resv, memMB))
	}
	return spec, changes, nil
}

// reconfigure 는 ReconfigVM 을 하고 끝날 때까지 기다린다.
func reconfigure(ctx context.Context, vmObj *object.VirtualMachine, spec types.VirtualMachineConfigSpec) error {
	t, err := vmObj.Reconfigure(ctx, spec)
	if err == nil {
		_, err = t.WaitForResult(ctx)
	}
	return err
}

// hasEdit 는 스펙에 장치 수정(Edit)이 있는지 본다.
func hasEdit(spec types.VirtualMachineConfigSpec) bool {
	for _, dc := range spec.DeviceChange {
		if dc.GetVirtualDeviceConfigSpec().Operation == types.VirtualDeviceConfigSpecOperationEdit {
			return true
		}
	}
	return false
}

// powerOnAndReport 는 VM 을 켜고 끝날 때까지 기다린다. 실패하면 vCenter 가 준 원인 메시지 전체를 담은 오류를 돌려준다.
func powerOnAndReport(ctx context.Context, vmObj *object.VirtualMachine) error {
	t, err := vmObj.PowerOn(ctx)
	if err == nil {
		_, err = t.WaitForResult(ctx)
	}
	if err != nil {
		return errors.New(faultText(err))
	}
	return nil
}

// faultText 는 Task 오류의 fault 종류·메시지와, 있으면 세부 사유(FaultMessage)를 한 줄로 만든다.
func faultText(err error) string {
	var te task.Error
	if !errors.As(err, &te) || te.LocalizedMethodFault == nil {
		return err.Error()
	}
	msg := te.LocalizedMethodFault.LocalizedMessage
	if f := te.LocalizedMethodFault.Fault; f != nil {
		if kind := strings.TrimPrefix(fmt.Sprintf("%T", f), "*types."); msg != "" && !strings.Contains(msg, kind) {
			msg = "[" + kind + "] " + msg
		}
		var extra []string
		for _, fm := range f.GetMethodFault().FaultMessage {
			if fm.Message != "" && !strings.Contains(msg, fm.Message) {
				extra = append(extra, fm.Message)
			}
		}
		if len(extra) > 0 {
			msg += " (" + strings.Join(extra, "; ") + ")"
		}
	}
	if msg == "" {
		return err.Error()
	}
	return msg
}

// collapseEvUsage: ev02~ev99 옵션은 번호만 다르고 내용이 같아서, 도움말(-h)에는 02 하나를
// "02~99"로 바꿔 보여주고 03~99는 생략한다. re 의 첫 캡처 그룹이 ev 번호(두 자리)다.
func collapseEvUsage(re *regexp.Regexp) {
	flag.Usage = func() {
		out := flag.CommandLine.Output()
		fs := flag.NewFlagSet(os.Args[0], flag.ContinueOnError)
		fs.SetOutput(out)
		flag.VisitAll(func(f *flag.Flag) {
			name, usage := f.Name, f.Usage
			if m := re.FindStringSubmatchIndex(name); m != nil {
				n, _ := strconv.Atoi(name[m[2]:m[3]])
				if n > 2 {
					return
				}
				if n == 2 {
					name = name[:m[2]] + "02~99" + name[m[3]:]
					usage = strings.NewReplacer("ev02", "ev02~ev99", "EV02", "EV02~EV99").Replace(usage)
				}
			}
			fs.Var(f.Value, name, usage)
			fs.Lookup(name).DefValue = f.DefValue
		})
		fmt.Fprintf(out, "Usage of %s:\n", os.Args[0])
		fs.PrintDefaults()
	}
}
