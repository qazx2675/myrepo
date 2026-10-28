// pci-passthrough-enable enables PCI passthrough on the ESXi hosts backing
// the VMs listed in a worklist file, matching devices by vendor name/id.
// Host processing is parallelized with a bounded worker pool.
package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"

	"github.com/vmware/govmomi"
	"github.com/vmware/govmomi/property"
	"github.com/vmware/govmomi/view"
	"github.com/vmware/govmomi/vim25/methods"
	"github.com/vmware/govmomi/vim25/mo"
	"github.com/vmware/govmomi/vim25/soap"
	"github.com/vmware/govmomi/vim25/types"
)

func connectVCenter(ctx context.Context, vcTargetIP string) (*govmomi.Client, error) {
	username := os.Getenv("VCENTER_USER")
	password := os.Getenv("VCENTER_PASS")
	if username == "" || password == "" {
		return nil, fmt.Errorf("VCENTER_USER / VCENTER_PASS 환경변수가 설정되어 있지 않습니다")
	}
	u, err := soap.ParseURL(vcTargetIP)
	if err != nil {
		return nil, err
	}
	u.User = url.UserPassword(username, password)
	return govmomi.NewClient(ctx, u, true)
}

func readWorklist(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line != "" && !strings.HasPrefix(line, "#") {
			lines = append(lines, line)
		}
	}
	return lines, scanner.Err()
}

func matchesVendor(dev types.HostPciDevice, pciVendor string) bool {
	switch strings.ToLower(pciVendor) {
	case "mellanox":
		return dev.VendorId == 5555
	case "nvidia":
		return dev.VendorId == 4318
	}
	if n, err := strconv.Atoi(pciVendor); err == nil {
		return int(dev.VendorId) == n
	}
	lc := strings.ToLower(pciVendor)
	return strings.Contains(strings.ToLower(dev.Id), lc) ||
		strings.Contains(strings.ToLower(dev.VendorName), lc) ||
		strings.Contains(strings.ToLower(dev.DeviceName), lc)
}

func main() {
	vcTargetIP := flag.String("vcTargetIP", "", "vCenter IP")
	pciVendor := flag.String("pciVendor", "", "PCI 벤더 (Mellanox, NVIDIA, VendorId 숫자, 또는 키워드)")
	worklistFile := flag.String("worklistFile", "worklist_pci.txt", "VM 목록 파일")
	workers := flag.Int("workers", 4, "동시 처리할 호스트 수")
	flag.Parse()

	if *vcTargetIP == "" || *pciVendor == "" {
		fmt.Fprintln(os.Stderr, "usage: -vcTargetIP, -pciVendor 는 필수입니다")
		os.Exit(1)
	}

	ctx := context.Background()
	client, err := connectVCenter(ctx, *vcTargetIP)
	if err != nil {
		fmt.Fprintln(os.Stderr, "vCenter 접속 실패:", err)
		os.Exit(1)
	}
	defer client.Logout(ctx)
	fmt.Println("-> vCenter 접속 완료")

	names, err := readWorklist(*worklistFile)
	if err != nil {
		fmt.Fprintln(os.Stderr, "worklist 로드 실패:", err)
		os.Exit(1)
	}

	mgr := view.NewManager(client.Client)
	vmView, err := mgr.CreateContainerView(ctx, client.Client.ServiceContent.RootFolder, []string{"VirtualMachine"}, true)
	if err != nil {
		fmt.Fprintln(os.Stderr, "container view 생성 실패:", err)
		os.Exit(1)
	}
	defer vmView.Destroy(ctx)

	var vms []mo.VirtualMachine
	if err := vmView.Retrieve(ctx, []string{"VirtualMachine"}, []string{"name", "runtime"}, &vms); err != nil {
		fmt.Fprintln(os.Stderr, "VM 조회 실패:", err)
		os.Exit(1)
	}

	// worklist에 매칭되는 VM들의 소속 호스트를 중복 없이 수집
	hostSet := map[string]types.ManagedObjectReference{}
	for _, name := range names {
		re := regexp.MustCompile("^" + regexp.QuoteMeta(name) + `(\.|$)`)
		for _, vm := range vms {
			if re.MatchString(vm.Name) && vm.Runtime.Host != nil {
				hostSet[vm.Runtime.Host.Value] = *vm.Runtime.Host
			}
		}
	}

	if len(hostSet) == 0 {
		fmt.Println("worklist에 매칭되는 VM/호스트가 없습니다.")
		return
	}
	fmt.Printf("대상 호스트: %d개\n", len(hostSet))

	pc := property.DefaultCollector(client.Client)
	sem := make(chan struct{}, *workers)
	var wg sync.WaitGroup

	for _, hostRef := range hostSet {
		wg.Add(1)
		sem <- struct{}{}
		go func(hostRef types.ManagedObjectReference) {
			defer wg.Done()
			defer func() { <-sem }()

			var host mo.HostSystem
			if err := pc.RetrieveOne(ctx, hostRef, []string{"name", "hardware.pciDevice", "configManager"}, &host); err != nil {
				fmt.Printf("[%s] 호스트 정보 조회 실패: %v\n", hostRef.Value, err)
				return
			}

			var matched []types.HostPciDevice
			for _, dev := range host.Hardware.PciDevice {
				if matchesVendor(dev, *pciVendor) {
					matched = append(matched, dev)
				}
			}
			if len(matched) == 0 {
				fmt.Printf("[%s] 대상 벤더 장치 없음, 스킵\n", host.Name)
				return
			}

			if host.ConfigManager.PciPassthruSystem == nil {
				fmt.Printf("[%s] PciPassthruSystem 을 찾을 수 없음\n", host.Name)
				return
			}

			var toEnable []types.BaseHostPciPassthruConfig
			for _, dev := range matched {
				toEnable = append(toEnable, &types.HostPciPassthruConfig{
					Id:              dev.Id,
					PassthruEnabled: true,
				})
			}

			req := &types.UpdatePassthruConfig{
				This:   *host.ConfigManager.PciPassthruSystem,
				Config: toEnable,
			}
			if _, err := methods.UpdatePassthruConfig(ctx, client.Client, req); err != nil {
				fmt.Printf("[%s] 패스스루 활성화 실패: %v\n", host.Name, err)
				return
			}
			fmt.Printf("[%s] 패스스루 활성화 완료 (%d개 장치) — 장치에 따라 호스트 재부팅이 필요할 수 있습니다\n", host.Name, len(matched))
		}(hostRef)
	}

	wg.Wait()
	fmt.Println("모든 작업이 완료되었습니다.")
}
