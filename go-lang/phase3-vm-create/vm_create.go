package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"log"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/vmware/govmomi"
	"github.com/vmware/govmomi/find"
	"github.com/vmware/govmomi/object"
	"github.com/vmware/govmomi/vim25/mo"
	"github.com/vmware/govmomi/vim25/types"
)

type VMSpec struct{ Cpu, Mem, Disk, Share int; Dept, Purpose, Type string }
type PendingConfig struct {
	Name   string
	Config VMSpec
}

func readLines(path string) ([]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	var lines []string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line != "" && !strings.HasPrefix(line, "#") {
			lines = append(lines, line)
		}
	}
	return lines, scanner.Err()
}

func main() {
	vcId := flag.String("id", "lscsystems@vsphere.local", "vCenter 로그인 계정 ID")
	vcTargetIP := flag.String("vcTargetIP", "", "vCenter 접속 IP (필수)")
	folderName := flag.String("folderName", "", "네트워크 어댑터 할당을 위한 폴더/포트그룹명 (필수)")
	worklistFile := flag.String("worklistFile", "worklist.txt", "작업 대상 호스트 목록 파일")
	vmCount := flag.Int("vmCount", 2, "생성할 VM 개수 (1~3)")
	ev01Cpu := flag.Int("ev01Cpu", 0, "EV01 CPU")
	ev01Mem := flag.Int("ev01Mem", 0, "EV01 Mem")
	ev01Disk := flag.Int("ev01Disk", 0, "EV01 Disk")
	ev01Share := flag.Int("ev01Share", 0, "EV01 Share")
	ev01DeptName := flag.String("ev01DeptName", "HomeLab", "EV01 Dept")
	ev01Purpose := flag.String("ev01Purpose", "Test", "EV01 Purpose")
	ev01VmType := flag.String("ev01VmType", "EV01_Node", "EV01 Type")
	ev02Cpu := flag.Int("ev02Cpu", 0, "EV02 CPU")
	ev02Mem := flag.Int("ev02Mem", 0, "EV02 Mem")
	ev02Disk := flag.Int("ev02Disk", 0, "EV02 Disk")
	ev02Share := flag.Int("ev02Share", 0, "EV02 Share")
	ev02DeptName := flag.String("ev02DeptName", "HomeLab", "EV02 Dept")
	ev02Purpose := flag.String("ev02Purpose", "Test", "EV02 Purpose")
	ev02VmType := flag.String("ev02VmType", "EV02_Node", "EV02 Type")
	ev03Cpu := flag.Int("ev03Cpu", 1, "EV03 CPU")
	ev03Mem := flag.Int("ev03Mem", 1, "EV03 Mem")
	ev03Disk := flag.Int("ev03Disk", 20, "EV03 Disk")
	ev03Share := flag.Int("ev03Share", 1000, "EV03 Share")
	ev03DeptName := flag.String("ev03DeptName", "HomeLab", "EV03 Dept")
	ev03Purpose := flag.String("ev03Purpose", "Test", "EV03 Purpose")
	ev03VmType := flag.String("ev03VmType", "EV03_Node", "EV03 Type")
	flag.Parse()

	if *vcTargetIP == "" || *folderName == "" || *ev01Cpu == 0 || *ev02Cpu == 0 {
		log.Fatal("필수 파라미터가 누락되었습니다. (vcTargetIP, folderName, EV01/02 Specs)")
	}
	if *vmCount > 3 || *vmCount < 1 {
		log.Fatal("1~3대 동적 생성만 지원합니다.")
	}

	vmConfigs := map[int]VMSpec{
		1: {Cpu: *ev01Cpu, Mem: *ev01Mem, Disk: *ev01Disk, Share: *ev01Share, Dept: *ev01DeptName, Purpose: *ev01Purpose, Type: *ev01VmType},
		2: {Cpu: *ev02Cpu, Mem: *ev02Mem, Disk: *ev02Disk, Share: *ev02Share, Dept: *ev02DeptName, Purpose: *ev02Purpose, Type: *ev02VmType},
		3: {Cpu: *ev03Cpu, Mem: *ev03Mem, Disk: *ev03Disk, Share: *ev03Share, Dept: *ev03DeptName, Purpose: *ev03Purpose, Type: *ev03VmType},
	}

	vcPassword := os.Getenv("VC_PASSWORD")
	if vcPassword == "" {
		log.Fatal("인증 로드 실패: VC_PASSWORD 환경 변수가 설정되지 않았습니다.")
	}

	baseDir, _ := os.Getwd()
	serverList, err := readLines(filepath.Join(baseDir, *worklistFile))
	if err != nil {
		log.Fatalf("worklist 파일 읽기 실패: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	u := &url.URL{Scheme: "https", Host: *vcTargetIP, Path: "/sdk"}
	u.User = url.UserPassword(*vcId, vcPassword)
	client, err := govmomi.NewClient(ctx, u, true)
	if err != nil {
		log.Fatalf("오류: vCenter 접속 실패: %v", err)
	}
	defer client.Logout(ctx)

	cfm, err := object.GetCustomFieldsManager(client.Client)
	if err == nil && cfm != nil {
		fields, _ := cfm.Field(ctx)
		for _, reqAttr := range []string{"DEPT_NAME", "PURPOSE", "VM_TYPE"} {
			exists := false
			for _, f := range fields {
				if f.Name == reqAttr {
					exists = true
					break
				}
			}
			if !exists {
				cfm.Add(ctx, reqAttr, "VirtualMachine", nil, nil)
			}
		}
	}

	var safeVmNames []string
	for _, bmHost := range serverList {
		baseName := strings.Split(bmHost, ".")[0]
		for i := 1; i <= *vmCount; i++ {
			safeVmNames = append(safeVmNames, regexp.QuoteMeta(fmt.Sprintf("%sev%02d", baseName, i)))
		}
	}

	finder := find.NewFinder(client.Client, true)
	dc, _ := finder.DefaultDatacenter(ctx)
	finder.SetDatacenter(dc)

	var targetNetwork object.NetworkReference
	netList, err := finder.NetworkList(ctx, "*"+*folderName+"*")
	if err == nil && len(netList) > 0 {
		targetNetwork = netList[0]
	}

	vmFilterPattern := "^(" + strings.Join(safeVmNames, "|") + ")$"
	regexMatcher := regexp.MustCompile(vmFilterPattern)
	existingVms, _ := finder.VirtualMachineList(ctx, "*")
	existingVmNames := make(map[string]bool)
	for _, vm := range existingVms {
		if regexMatcher.MatchString(vm.Name()) {
			existingVmNames[vm.Name()] = true
		}
	}

	var creationTasks []*object.Task
	var pendingConfigs []PendingConfig
	for _, bmHost := range serverList {
		baseName := strings.Split(bmHost, ".")[0]
		hostObj, err := finder.HostSystem(ctx, bmHost)
		if err != nil {
			continue
		}
		var host mo.HostSystem
		if err := hostObj.Properties(ctx, hostObj.Reference(), []string{"datastore"}, &host); err != nil || len(host.Datastore) == 0 {
			continue
		}
		var bestDs mo.Datastore
		maxFreeSpace := int64(-1)
		for _, dsRef := range host.Datastore {
			var ds mo.Datastore
			tempDsObj := object.NewDatastore(client.Client, dsRef)
			if err := tempDsObj.Properties(ctx, tempDsObj.Reference(), []string{"summary"}, &ds); err == nil && ds.Summary.FreeSpace > maxFreeSpace {
				maxFreeSpace, bestDs = ds.Summary.FreeSpace, ds
			}
		}
		if maxFreeSpace == -1 {
			continue
		}
		hostRP, _ := hostObj.ResourcePool(ctx)
		folder, _ := finder.DefaultFolder(ctx)

		for i := 1; i <= *vmCount; i++ {
			vmName := fmt.Sprintf("%sev%02d", baseName, i)
			cfg := vmConfigs[i]
			if existingVmNames[vmName] {
				continue
			}
			spec := types.VirtualMachineConfigSpec{
				Name: vmName, GuestId: "rhel8_64Guest", NumCPUs: int32(cfg.Cpu), MemoryMB: int64(cfg.Mem * 1024),
				Files: &types.VirtualMachineFileInfo{VmPathName: fmt.Sprintf("[%s]", bestDs.Summary.Name)},
			}
			scsi := &types.ParaVirtualSCSIController{VirtualSCSIController: types.VirtualSCSIController{
				SharedBus: types.VirtualSCSISharingNoSharing, VirtualController: types.VirtualController{BusNumber: 0, VirtualDevice: types.VirtualDevice{Key: -1}}}}
			disk := &types.VirtualDisk{VirtualDevice: types.VirtualDevice{Key: -2, ControllerKey: -1, UnitNumber: types.NewInt32(0),
				Backing: &types.VirtualDiskFlatVer2BackingInfo{VirtualDeviceFileBackingInfo: types.VirtualDeviceFileBackingInfo{FileName: fmt.Sprintf("[%s]", bestDs.Summary.Name)},
					DiskMode: string(types.VirtualDiskModePersistent), ThinProvisioned: types.NewBool(false), EagerlyScrub: types.NewBool(false)}},
				CapacityInKB: int64(cfg.Disk) * 1024 * 1024}

			var nic *types.VirtualVmxnet3
			if targetNetwork != nil {
				if backing, err := targetNetwork.EthernetCardBackingInfo(ctx); err == nil {
					nic = &types.VirtualVmxnet3{VirtualVmxnet: types.VirtualVmxnet{VirtualEthernetCard: types.VirtualEthernetCard{
						VirtualDevice: types.VirtualDevice{Key: -3, Backing: backing}, AddressType: string(types.VirtualEthernetCardMacTypeGenerated)}}}
				}
			}
			spec.DeviceChange = []types.BaseVirtualDeviceConfigSpec{
				&types.VirtualDeviceConfigSpec{Operation: types.VirtualDeviceConfigSpecOperationAdd, Device: scsi},
				&types.VirtualDeviceConfigSpec{Operation: types.VirtualDeviceConfigSpecOperationAdd, FileOperation: types.VirtualDeviceConfigSpecFileOperationCreate, Device: disk},
			}
			if nic != nil {
				spec.DeviceChange = append(spec.DeviceChange, &types.VirtualDeviceConfigSpec{Operation: types.VirtualDeviceConfigSpecOperationAdd, Device: nic})
			}
			if task, err := folder.CreateVM(ctx, spec, hostRP, hostObj); err == nil {
				creationTasks = append(creationTasks, task)
				pendingConfigs = append(pendingConfigs, PendingConfig{Name: vmName, Config: cfg})
			}
		}
	}
	for _, task := range creationTasks {
		_ = task.Wait(ctx)
	}

	if len(pendingConfigs) > 0 {
		var configTasks []*object.Task
		fields, _ := cfm.Field(ctx)
		fieldMap := make(map[string]int32)
		for _, f := range fields {
			fieldMap[f.Name] = f.Key
		}
		for _, pending := range pendingConfigs {
			targetVM, err := finder.VirtualMachine(ctx, pending.Name)
			if err != nil {
				continue
			}
			cfg := pending.Config
			spec := types.VirtualMachineConfigSpec{}
			spec.MemoryReservationLockedToMax = types.NewBool(true)
			spec.BootOptions = &types.VirtualMachineBootOptions{EfiSecureBootEnabled: types.NewBool(false)}
			spec.CpuAllocation = &types.ResourceAllocationInfo{Shares: &types.SharesInfo{Level: types.SharesLevelCustom, Shares: int32(cfg.Share)}}
			spec.MemoryAllocation = &types.ResourceAllocationInfo{Reservation: types.NewInt64(int64(cfg.Mem * 1024)), Shares: &types.SharesInfo{Level: types.SharesLevelCustom, Shares: int32(cfg.Share)}}
			spec.ExtraConfig = []types.BaseOptionValue{
				&types.OptionValue{Key: "sched.mem.pin", Value: "TRUE"},
				&types.OptionValue{Key: "sched.mem.prealloc", Value: "TRUE"},
				&types.OptionValue{Key: "sched.mem.prealloc.pinnedMainMem", Value: "TRUE"},
				&types.OptionValue{Key: "sched.swap.vmxSwapEnabled", Value: "FALSE"},
			}
			if task, err := targetVM.Reconfigure(ctx, spec); err == nil {
				configTasks = append(configTasks, task)
			}
			if key, ok := fieldMap["DEPT_NAME"]; ok {
				_ = cfm.Set(ctx, targetVM.Reference(), key, cfg.Dept)
			}
			if key, ok := fieldMap["PURPOSE"]; ok {
				_ = cfm.Set(ctx, targetVM.Reference(), key, cfg.Purpose)
			}
			if key, ok := fieldMap["VM_TYPE"]; ok {
				_ = cfm.Set(ctx, targetVM.Reference(), key, cfg.Type)
			}
		}
		for _, task := range configTasks {
			_ = task.Wait(ctx)
		}
		fmt.Println("[INFO] VM 생성 및 세부 속성 주입이 완료되었습니다!")
	} else {
		fmt.Println("[INFO] 새로 생성할 VM이 없거나 이미 모두 생성되어 있습니다.")
	}
}
