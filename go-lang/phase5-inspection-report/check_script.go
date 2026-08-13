package main

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"encoding/base64"
	"encoding/csv"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"text/tabwriter"
	"unicode/utf16"

	"github.com/vmware/govmomi"
	"github.com/vmware/govmomi/view"
	"github.com/vmware/govmomi/vim25/mo"
	"github.com/vmware/govmomi/vim25/types"
)

const vcUser = "lscsystems@vsphere.local"
const psSecureStringMagic = "76492d1116743f0423413b16050a5345"

type VMReport struct {
	BMHostname, VMName, VMCPU, VMMemory, CorePerSocket, NumaMaxPerNode      string
	Enable1GPage, Prealloc, PinnedMainMem, VMXSwpEnabled                    string
	MemoryReser, MemoryShares, CPUShares, PCIVendor                        string
}

var csvHeader = []string{
	"BM_hostname", "vm_name", "vm_cpu", "vm_memory", "corePerSocket",
	"numa_vcpu_maxPerVirtualNode", "enable1GPage", "prealloc", "pinnedMainMem",
	"vmxSwpEnabled", "MemoryReser", "MemoryShares", "CpuShares", "pci_vendor",
}

func (r VMReport) Row() []string {
	return []string{r.BMHostname, r.VMName, r.VMCPU, r.VMMemory, r.CorePerSocket,
		r.NumaMaxPerNode, r.Enable1GPage, r.Prealloc, r.PinnedMainMem,
		r.VMXSwpEnabled, r.MemoryReser, r.MemoryShares, r.CPUShares, r.PCIVendor}
}

// [수정] dir/out/password 플래그 제거, -f(점검 대상 파일) 추가. 출력파일명은 res_<입력파일명>.csv 로 자동 생성.
func main() {
	var (
		vcTargetIP = flag.String("vc", "", "[필수] vCenter 주소(IP 또는 FQDN)")
		inputFile  = flag.String("f", "", "[필수] 점검할 호스트 목록 파일 (예: list.txt)")
	)
	flag.Parse()

	if strings.TrimSpace(*vcTargetIP) == "" || strings.TrimSpace(*inputFile) == "" {
		fmt.Fprintln(os.Stderr, "[오류] -vc 및 -f 파라미터는 필수입니다.")
		flag.Usage()
		os.Exit(1)
	}

	password := os.Getenv("VC_PASSWORD")
	if strings.TrimSpace(password) == "" {
		fmt.Fprintln(os.Stderr, "[오류] VC_PASSWORD 환경변수가 설정되지 않았습니다.")
		os.Exit(1)
	}

	hostlistLines, err := loadWorklist(*inputFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[오류] 지정한 %s 파일을 읽을 수 없습니다: %v\n", *inputFile, err)
		os.Exit(1)
	}
	baseName := filepath.Base(*inputFile)
	ext := filepath.Ext(baseName)
	nameWithoutExt := strings.TrimSuffix(baseName, ext)
	outName := fmt.Sprintf("res_%s.csv", nameWithoutExt)

	fmt.Printf("-> [CHECKBOX] %s 로드 완료 (대상 물리 서버: %d대)\n", *inputFile, len(hostlistLines))

	ctx := context.Background()
	u, err := url.Parse(fmt.Sprintf("https://%s/sdk", *vcTargetIP))
	if err != nil {
		fmt.Fprintf(os.Stderr, "[오류] vCenter URL 생성 실패: %v\n", err)
		os.Exit(1)
	}
	u.User = url.UserPassword(vcUser, password)

	client, err := govmomi.NewClient(ctx, u, true)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[오류] vCenter 접속 실패: %v\n", err)
		os.Exit(1)
	}
	defer func() { _ = client.Logout(context.Background()) }()

	viewMgr := view.NewManager(client.Client)

	hostView, err := viewMgr.CreateContainerView(ctx, client.ServiceContent.RootFolder, []string{"HostSystem"}, true)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[오류] HostSystem 뷰 생성 실패: %v\n", err)
		os.Exit(1)
	}
	defer hostView.Destroy(context.Background())

	var allHosts []mo.HostSystem
	if err := hostView.Retrieve(ctx, []string{"HostSystem"}, []string{"name"}, &allHosts); err != nil {
		fmt.Fprintf(os.Stderr, "[오류] HostSystem 조회 실패: %v\n", err)
		os.Exit(1)
	}

	vmView, err := viewMgr.CreateContainerView(ctx, client.ServiceContent.RootFolder, []string{"VirtualMachine"}, true)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[오류] VirtualMachine 뷰 생성 실패: %v\n", err)
		os.Exit(1)
	}
	defer vmView.Destroy(context.Background())

	var allVMs []mo.VirtualMachine
	vmProps := []string{"name", "runtime.host", "config.hardware", "config.extraConfig", "resourceConfig"}
	if err := vmView.Retrieve(ctx, []string{"VirtualMachine"}, vmProps, &allVMs); err != nil {
		fmt.Fprintf(os.Stderr, "[오류] VirtualMachine 조회 실패: %v\n", err)
		os.Exit(1)
	}

	var targetHosts []mo.HostSystem
	for _, line := range hostlistLines {
		cleanName := strings.Split(strings.TrimSpace(line), ".")[0]
		if cleanName == "" {
			continue
		}
		re, err := regexp.Compile(`(?i)^` + regexp.QuoteMeta(cleanName) + `(\.|$)`)
		if err != nil {
			continue
		}
		for _, h := range allHosts {
			if re.MatchString(h.Name) {
				targetHosts = append(targetHosts, h)
			}
		}
	}

	if len(targetHosts) == 0 {
		fmt.Fprintf(os.Stderr, "[경고] %s 에 등록된 호스트를 인벤토리에서 찾을 수 없습니다.\n", *inputFile)
		os.Exit(1)
	}

	var reportResults []VMReport
	for _, h := range targetHosts {
		bmHost, hostID := h.Name, h.Self.Value
		var myVMs []mo.VirtualMachine
		for _, vm := range allVMs {
			if vm.Runtime.Host != nil && vm.Runtime.Host.Value == hostID {
				myVMs = append(myVMs, vm)
			}
		}
		if len(myVMs) == 0 {
			continue
		}
		for _, vm := range myVMs {
			reportResults = append(reportResults, buildReport(bmHost, vm))
		}
	}

	if len(reportResults) == 0 {
		fmt.Fprintln(os.Stderr, "\n[오류] 수집된 리포트 결과가 0건입니다.")
		os.Exit(1)
	}

	printTable(reportResults)
	if err := writeCSV(outName, reportResults); err != nil {
		fmt.Fprintf(os.Stderr, "[오류] CSV 저장 실패: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("점검 완료! CSV 파일(%s)이 현재 경로에 저장되었습니다.\n", outName)
}

func buildReport(bmHost string, vm mo.VirtualMachine) VMReport {
	r := VMReport{BMHostname: bmHost, VMName: vm.Name, VMCPU: "N/A", VMMemory: "N/A",
		CorePerSocket: "N/A", MemoryShares: "N/A", CPUShares: "N/A", PCIVendor: "N/A", MemoryReser: "0.00 GB"}

	if vm.Config != nil {
		hw := vm.Config.Hardware
		r.VMCPU = fmt.Sprintf("%d", hw.NumCPU)
		if hw.NumCoresPerSocket > 0 {
			r.CorePerSocket = fmt.Sprintf("%d", hw.NumCoresPerSocket)
		}
		if hw.MemoryMB > 0 {
			r.VMMemory = fmt.Sprintf("%.2f GB", float64(hw.MemoryMB)/1024.0)
		}
	}

	extra := map[string]string{}
	if vm.Config != nil {
		for _, opt := range vm.Config.ExtraConfig {
			if ov := opt.GetOptionValue(); ov != nil {
				extra[ov.Key] = fmt.Sprintf("%v", ov.Value)
			}
		}
	}
	r.Enable1GPage = extraOrDefault(extra, "sched.mem.lpage.enable1GPage", "FALSE")
	r.Prealloc = extraOrDefault(extra, "sched.mem.prealloc", "FALSE")
	r.PinnedMainMem = extraOrDefault(extra, "sched.mem.prealloc.pinnedMainMem", "FALSE")
	r.VMXSwpEnabled = extraOrDefault(extra, "sched.swap.vmxSwapEnabled", "TRUE")
	r.NumaMaxPerNode = extraOrDefault(extra, "numa.vcpu.maxPerVirtualNode", "N/A")

	if vm.ResourceConfig != nil {
		memAlloc := vm.ResourceConfig.MemoryAllocation
		if memAlloc.Reservation != nil {
			r.MemoryReser = fmt.Sprintf("%.2f GB", float64(*memAlloc.Reservation)/1024.0)
		}
		if memAlloc.Shares != nil {
			r.MemoryShares = fmt.Sprintf("%s (%d)", memAlloc.Shares.Level, memAlloc.Shares.Shares)
		}
		if cs := vm.ResourceConfig.CpuAllocation.Shares; cs != nil {
			r.CPUShares = fmt.Sprintf("%s (%d)", cs.Level, cs.Shares)
		}
	}

	if vm.Config != nil {
		var vendorList []string
		for _, dev := range vm.Config.Hardware.Device {
			if name, ok := extractPCIName(dev); ok {
				vendorList = append(vendorList, name)
			}
		}
		if len(vendorList) > 0 {
			r.PCIVendor = strings.Join(vendorList, " | ")
		}
	}
	return r
}

func extractPCIName(dev types.BaseVirtualDevice) (string, bool) {
	var deviceName, backingID string
	switch d := dev.(type) {
	case *types.VirtualPCIPassthrough:
		switch b := d.Backing.(type) {
		case *types.VirtualPCIPassthroughDeviceBackingInfo:
			deviceName, backingID = b.DeviceName, b.Id
		case *types.VirtualPCIPassthroughDynamicBackingInfo:
			deviceName = b.DeviceName
			if strings.TrimSpace(b.CustomLabel) != "" && strings.TrimSpace(deviceName) == "" {
				deviceName = b.CustomLabel
			}
		}
	case *types.VirtualSriovEthernetCard:
		if b, ok := d.Backing.(*types.VirtualEthernetCardNetworkBackingInfo); ok {
			deviceName = b.DeviceName
		}
		if d.SriovBacking != nil && d.SriovBacking.PhysicalFunctionBacking != nil {
			if strings.TrimSpace(deviceName) == "" {
				deviceName = d.SriovBacking.PhysicalFunctionBacking.DeviceName
			}
			backingID = d.SriovBacking.PhysicalFunctionBacking.Id
		}
	default:
		return "", false
	}
	if v := strings.TrimSpace(deviceName); v != "" {
		return v, true
	}
	if vd := dev.GetVirtualDevice(); vd != nil && vd.DeviceInfo != nil {
		if s := strings.TrimSpace(vd.DeviceInfo.GetDescription().Summary); s != "" {
			return s, true
		}
	}
	if v := strings.TrimSpace(backingID); v != "" {
		return "PCI_Addr:" + v, true
	}
	return "Unknown_PCI_Device", true
}

func extraOrDefault(m map[string]string, key, def string) string {
	if v, ok := m[key]; ok && strings.TrimSpace(v) != "" {
		return v
	}
	return def
}

func loadWorklist(path string) ([]string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var lines []string
	for _, l := range strings.Split(strings.ReplaceAll(string(raw), "\r\n", "\n"), "\n") {
		if strings.TrimSpace(l) != "" {
			lines = append(lines, l)
		}
	}
	return lines, nil
}

func printTable(rows []VMReport) {
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, strings.Join(csvHeader, "\t"))
	for _, r := range rows {
		fmt.Fprintln(w, strings.Join(r.Row(), "\t"))
	}
	_ = w.Flush()
}

func writeCSV(path string, rows []VMReport) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := f.Write([]byte{0xEF, 0xBB, 0xBF}); err != nil {
		return err
	}
	w := csv.NewWriter(f)
	if err := w.Write(csvHeader); err != nil {
		return err
	}
	for _, r := range rows {
		if err := w.Write(r.Row()); err != nil {
			return err
		}
	}
	w.Flush()
	return w.Error()
}

var _ = errors.New
var _ = aes.NewCipher
var _ = cipher.NewCBCDecrypter
var _ = base64.StdEncoding
var _ = hex.DecodeString
var _ = utf16.Decode
