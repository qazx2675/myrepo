// vCenter VM 설정 및 PCI 점검 도구 (PowerShell -> Go 변환)
//
// 빌드:
// go mod init vmcheck
// go get github.com/vmware/govmomi
// go build -o vmcheck .
//
// 실행:
// ./vmcheck -vc 10.0.0.10 -out My_Report.csv
// ./vmcheck -vc 10.0.0.10 -out My_Report.csv -dir /path/to/script/root
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
	BMHostname     string
	VMName         string
	VMCPU          string
	VMMemory       string
	CorePerSocket  string
	NumaMaxPerNode string
	Enable1GPage   string
	Prealloc       string
	PinnedMainMem  string
	VMXSwpEnabled  string
	MemoryReser    string
	MemoryShares   string
	CPUShares      string
	PCIVendor      string
}

var csvHeader = []string{
	"BM_hostname", "vm_name", "vm_cpu", "vm_memory", "corePerSocket",
	"numa_vcpu_maxPerVirtualNode", "enable1GPage", "prealloc", "pinnedMainMem",
	"vmxSwpEnabled", "MemoryReser", "MemoryShares", "CpuShares", "pci_vendor",
}

func (r VMReport) Row() []string {
	return []string{
		r.BMHostname, r.VMName, r.VMCPU, r.VMMemory, r.CorePerSocket,
		r.NumaMaxPerNode, r.Enable1GPage, r.Prealloc, r.PinnedMainMem,
		r.VMXSwpEnabled, r.MemoryReser, r.MemoryShares, r.CPUShares, r.PCIVendor,
	}
}

func main() {
	var (
		vcTargetIP     = flag.String("vc", "", "[필수] vCenter 주소(IP 또는 FQDN)")
		outputFileName = flag.String("out", "VM_Check_Report.csv", "결과 CSV 파일 이름")
		baseDir        = flag.String("dir", "", "worklist.txt / 인증 파일이 위치한 디렉터리")
		plainPassword  = flag.String("password", "", "직접 전달할 비밀번호 (미지정 시 VC_PASSWORD 사용)")
	)
	flag.Parse()

	if strings.TrimSpace(*vcTargetIP) == "" {
		fmt.Fprintln(os.Stderr, "[오류] -vc 파라미터는 필수입니다.")
		flag.Usage()
		os.Exit(1)
	}

	scriptDir := strings.TrimSpace(*baseDir)
	if scriptDir == "" {
		if cwd, err := os.Getwd(); err == nil {
			scriptDir = cwd
		} else {
			scriptDir = "."
		}
	}

	outName := filepath.Base(strings.TrimSpace(*outputFileName))
	if outName == "" || outName == "." || outName == string(filepath.Separator) {
		outName = "VM_Check_Report.csv"
	}
	if filepath.Ext(outName) == "" {
		outName += ".csv"
	}

	password, err := loadPassword(scriptDir, *plainPassword)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[오류] 인증 정보 로드 실패 : %v\n", err)
		os.Exit(1)
	}

	worklistPath := filepath.Join(scriptDir, "worklist.txt")
	hostlistLines, err := loadWorklist(worklistPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[오류] worklist.txt 파일을 찾을 수 없습니다: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("-> [CHECKBOX] worklist.txt 로드 완료 (대상 물리 서버: %d대)\n", len(hostlistLines))
	fmt.Println("vCenter API 전용 고속 인벤토리 쿼리를 시작합니다.")

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
		fmt.Fprintln(os.Stderr, "[경고] worklist.txt의 호스트를 인벤토리에서 찾을 수 없습니다.")
		os.Exit(1)
	}

	var reportResults []VMReport

	for _, h := range targetHosts {
		bmHost := h.Name
		hostID := h.Self.Value

		var myVMs []mo.VirtualMachine
		for _, vm := range allVMs {
			if vm.Runtime.Host != nil && vm.Runtime.Host.Value == hostID {
				myVMs = append(myVMs, vm)
			}
		}

		fmt.Printf("[%s] 분석 중... (발견된 VM: %d대)\n", bmHost, len(myVMs))
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

	csvOutputPath := filepath.Join(scriptDir, outName)
	if err := writeCSV(csvOutputPath, reportResults); err != nil {
		fmt.Fprintf(os.Stderr, "[오류] CSV 저장 실패: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("점검 완료! CSV 파일(%s)에 저장되었습니다.\n", csvOutputPath)
}

func buildReport(bmHost string, vm mo.VirtualMachine) VMReport {
	r := VMReport{
		BMHostname: bmHost, VMName: vm.Name, VMCPU: "N/A", VMMemory: "N/A",
		CorePerSocket: "N/A", MemoryShares: "N/A", CPUShares: "N/A", PCIVendor: "N/A",
		MemoryReser: "0.00 GB",
	}

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
			ov := opt.GetOptionValue()
			if ov == nil {
				continue
			}
			extra[ov.Key] = fmt.Sprintf("%v", ov.Value)
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
		cpuAlloc := vm.ResourceConfig.CpuAllocation
		if cpuAlloc.Shares != nil {
			r.CPUShares = fmt.Sprintf("%s (%d)", cpuAlloc.Shares.Level, cpuAlloc.Shares.Shares)
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
			deviceName, backingID = b.DeviceName, b.Id
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

func loadPassword(scriptDir, override string) (string, error) {
	if strings.TrimSpace(override) != "" {
		return override, nil
	}

	keyPath := filepath.Join(scriptDir, "Create_VM_pwsh_dir", "password_dir", "secret_VMw.key")
	encPath := filepath.Join(scriptDir, "Create_VM_pwsh_dir", "password_dir", "password_VMw.enc")

	keyBytes, keyErr := os.ReadFile(keyPath)
	encBytes, encErr := os.ReadFile(encPath)

	if keyErr == nil && encErr == nil {
		pw, err := decryptPowerShellSecureString(string(encBytes), keyBytes)
		if err == nil {
			return pw, nil
		}
		if env := os.Getenv("VC_PASSWORD"); env != "" {
			return env, nil
		}
		return "", fmt.Errorf("암호 복호화 실패: %w", err)
	}

	if env := os.Getenv("VC_PASSWORD"); env != "" {
		return env, nil
	}
	if keyErr != nil {
		return "", keyErr
	}
	return "", encErr
}

func decryptPowerShellSecureString(enc string, key []byte) (string, error) {
	enc = strings.TrimSpace(enc)
	if len(enc) <= len(psSecureStringMagic) || !strings.EqualFold(enc[:len(psSecureStringMagic)], psSecureStringMagic) {
		return "", errors.New("PowerShell SecureString 형식이 아닙니다")
	}

	payload, err := base64.StdEncoding.DecodeString(enc[len(psSecureStringMagic):])
	if err != nil {
		return "", fmt.Errorf("base64 디코딩 실패: %w", err)
	}

	parts := strings.Split(utf16LEToString(payload), "|")
	if len(parts) != 3 {
		return "", errors.New("SecureString 내부 구조가 예상과 다릅니다")
	}

	iv, err := base64.StdEncoding.DecodeString(parts[1])
	if err != nil {
		return "", fmt.Errorf("IV 디코딩 실패: %w", err)
	}
	cipherText, err := hex.DecodeString(parts[2])
	if err != nil {
		return "", fmt.Errorf("암호문 디코딩 실패: %w", err)
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("AES 키 오류: %w", err)
	}
	if len(cipherText) == 0 || len(cipherText)%block.BlockSize() != 0 {
		return "", errors.New("암호문 길이가 블록 크기의 배수가 아닙니다")
	}
	if len(iv) != block.BlockSize() {
		return "", errors.New("IV 길이가 올바르지 않습니다")
	}

	plain := make([]byte, len(cipherText))
	cipher.NewCBCDecrypter(block, iv).CryptBlocks(plain, cipherText)

	pad := int(plain[len(plain)-1])
	if pad > 0 && pad <= block.BlockSize() && pad <= len(plain) {
		plain = plain[:len(plain)-pad]
	}
	return utf16LEToString(plain), nil
}

func utf16LEToString(b []byte) string {
	if len(b)%2 != 0 {
		b = b[:len(b)-1]
	}
	u := make([]uint16, len(b)/2)
	for i := range u {
		u[i] = uint16(b[i*2]) | uint16(b[i*2+1])<<8
	}
	return string(utf16.Decode(u))
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
