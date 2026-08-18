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
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/vmware/govmomi"
	"github.com/vmware/govmomi/find"
	"github.com/vmware/govmomi/object"
	"github.com/vmware/govmomi/property"
	"github.com/vmware/govmomi/view"
	"github.com/vmware/govmomi/vim25/mo"
	"github.com/vmware/govmomi/vim25/types"
)

type VMSpec struct {
	Cpu        int
	Mem        int
	Disk       int
	ShareLevel types.SharesLevel
	Share      int32
}

type PendingConfig struct {
	Name   string
	Config VMSpec
}

type hostPrep struct {
	Index      int
	BMHost     string
	OK         bool
	BaseName   string
	HostObj    *object.HostSystem
	ResPool    *object.ResourcePool
	Folder     *object.Folder
	DSName     string
	PGName     string
	HasNetwork bool
}

var bomPrefix = string(rune(0xFEFF))

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

func loadHostgroupMap(path string) (map[string]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	splitter := regexp.MustCompile(`[,\s]+`)
	result := make(map[string]string)
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := splitter.Split(line, -1)
		if len(fields) < 2 {
			fmt.Printf("[경고] 매핑 파일의 형식이 잘못된 줄 (무시됨): %q\n", line)
			continue
		}
		result[fields[0]] = fields[1]
	}
	return result, scanner.Err()
}

// parseShareValue: Share 플래그 값을 파싱한다.
// "nomal"(오탈자 허용) 또는 "normal"이면 vCenter의 Normal 공유 레벨을 사용하고,
// 그 외에는 숫자로 파싱해 Custom 레벨의 공유값으로 사용한다.
func parseShareValue(flagName, raw string) (types.SharesLevel, int32) {
	v := strings.TrimSpace(raw)
	if strings.EqualFold(v, "nomal") || strings.EqualFold(v, "normal") {
		return types.SharesLevelNormal, 0
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		log.Fatalf("-%s 값이 올바르지 않습니다: %q (숫자 또는 'nomal'만 허용)", flagName, raw)
	}
	return types.SharesLevelCustom, int32(n)
}

func main() {
	vcId := flag.String("id", "lscsystems@vsphere.local", "vCenter 로그인 계정 ID")
	vcTargetIP := flag.String("vcTargetIP", "", "vCenter 접속 IP (필수)")
	worklistFile := flag.String("worklistFile", "worklist.txt", "작업 대상 호스트 목록 파일")
	vmCount := flag.Int("vmCount", 2, "생성할 VM 개수 (1~3)")
	mapFile := flag.String("mapFile", "hostgroup.txt", "\"BM hostgroup이름\" 형식의 네트워크 매핑 파일 (다른 이름 지정 가능)")
	firmware := flag.String("firmware", "efi", "펌웨어 타입 (bios 또는 efi) - 정상 부팅되는 서버가 EFI(권장) 확인됨")
	datacenterName := flag.String("datacenter", "", "데이터센터 이름 (데이터센터가 여러 개면 필수, 1개뿐이면 생략 가능)")

	prepConc := flag.Int("prepConcurrency", 16, "호스트 사전 조사 동시 처리 수 (500대 규모 권장: 12~24)")
	taskConc := flag.Int("taskConcurrency", 24, "vCenter 작업(Task) 동시 실행 수 (500대 규모 권장: 16~32)")

	ev01Cpu := flag.Int("ev01Cpu", 0, "EV01 CPU")
	ev01Mem := flag.Int("ev01Mem", 0, "EV01 Mem")
	ev01Disk := flag.Int("ev01Disk", 0, "EV01 Disk")
	ev01Share := flag.String("ev01Share", "0", "EV01 Share (숫자 또는 'nomal')")

	ev02Cpu := flag.Int("ev02Cpu", 0, "EV02 CPU")
	ev02Mem := flag.Int("ev02Mem", 0, "EV02 Mem")
	ev02Disk := flag.Int("ev02Disk", 0, "EV02 Disk")
	ev02Share := flag.String("ev02Share", "0", "EV02 Share (숫자 또는 'nomal')")

	ev03Cpu := flag.Int("ev03Cpu", 1, "EV03 CPU")
	ev03Mem := flag.Int("ev03Mem", 1, "EV03 Mem")
	ev03Disk := flag.Int("ev03Disk", 20, "EV03 Disk")
	ev03Share := flag.String("ev03Share", "1000", "EV03 Share (숫자 또는 'nomal')")

	flag.Parse()

	if *vcTargetIP == "" || *ev01Cpu == 0 || *ev02Cpu == 0 {
		log.Fatal("필수 파라미터가 누락되었습니다. (vcTargetIP, EV01/02 Specs)")
	}

	if *vmCount > 3 || *vmCount < 1 {
		log.Fatal("1~3대 동적 생성만 지원합니다.")
	}

	if *firmware != "bios" && *firmware != "efi" {
		log.Fatal("-firmware 값은 bios 또는 efi 여야 합니다.")
	}

	if *prepConc < 1 {
		*prepConc = 1
	}
	if *taskConc < 1 {
		*taskConc = 1
	}

	ev01Level, ev01ShareVal := parseShareValue("ev01Share", *ev01Share)
	ev02Level, ev02ShareVal := parseShareValue("ev02Share", *ev02Share)
	ev03Level, ev03ShareVal := parseShareValue("ev03Share", *ev03Share)

	vmConfigs := map[int]VMSpec{
		1: {Cpu: *ev01Cpu, Mem: *ev01Mem, Disk: *ev01Disk, ShareLevel: ev01Level, Share: ev01ShareVal},
		2: {Cpu: *ev02Cpu, Mem: *ev02Mem, Disk: *ev02Disk, ShareLevel: ev02Level, Share: ev02ShareVal},
		3: {Cpu: *ev03Cpu, Mem: *ev03Mem, Disk: *ev03Disk, ShareLevel: ev03Level, Share: ev03ShareVal},
	}

	vcPassword := os.Getenv("VC_PASSWORD")
	if vcPassword == "" {
		log.Fatal("인증 로드 실패: VC_PASSWORD 환경 변수가 설정되지 않았습니다.")
	}

	baseDir, _ := os.Getwd()
	worklistPath := filepath.Join(baseDir, *worklistFile)
	if _, err := os.Stat(worklistPath); os.IsNotExist(err) {
		log.Fatalf("%s 없음", *worklistFile)
	}

	serverList, err := readLines(worklistPath)
	if err != nil {
		log.Fatalf("worklist 파일 읽기 실패: %v", err)
	}

	hostgroupMap, err := loadHostgroupMap(filepath.Join(baseDir, *mapFile))
	if err != nil {
		log.Fatalf("매핑 파일 로드 실패 (%s): %v", *mapFile, err)
	}
	fmt.Printf("[INFO] 매핑 파일(%s) 로드 완료 (%d건)\n", *mapFile, len(hostgroupMap))

	fmt.Printf("\n[INFO] 동적 VM 생성 (Phase 3) 시작 (접속 계정: %s)\n", *vcId)
	fmt.Printf("[INFO] 대상 물리 호스트 %d대 / 조사 동시성 %d / 작업 동시성 %d\n",
		len(serverList), *prepConc, *taskConc)

	// 개선 4: 전역 타임아웃 추가 (500대 규모에서 어딘가 멈춰도 무한정 걸리지 않도록)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Minute)
	defer cancel()

	u := &url.URL{Scheme: "https", Host: *vcTargetIP, Path: "/sdk"}
	u.User = url.UserPassword(*vcId, vcPassword)

	client, err := govmomi.NewClient(ctx, u, true)
	if err != nil {
		log.Fatalf("오류: vCenter 접속 실패: %v", err)
	}
	defer client.Logout(ctx)

	var safeVmNames []string
	for _, bmHost := range serverList {
		baseName := strings.Split(bmHost, ".")[0]
		for i := 1; i <= *vmCount; i++ {
			vmName := fmt.Sprintf("%sev%02d", baseName, i)
			safeVmNames = append(safeVmNames, regexp.QuoteMeta(vmName))
		}
	}

	baseFinder := find.NewFinder(client.Client, true)

	var selectedDC *object.Datacenter
	if *datacenterName != "" {
		dc, dcErr := baseFinder.Datacenter(ctx, *datacenterName)
		if dcErr != nil {
			log.Fatalf("데이터센터 '%s'를 찾을 수 없습니다: %v", *datacenterName, dcErr)
		}
		selectedDC = dc
		baseFinder.SetDatacenter(dc)
		fmt.Printf("[INFO] 데이터센터 [%s] 사용\n", dc.Name())
	} else {
		dcs, dcErr := baseFinder.DatacenterList(ctx, "*")
		if dcErr != nil || len(dcs) == 0 {
			log.Fatalf("데이터센터 목록 조회 실패: %v", dcErr)
		}
		if len(dcs) == 1 {
			selectedDC = dcs[0]
			baseFinder.SetDatacenter(dcs[0])
			fmt.Printf("[INFO] 데이터센터가 1개뿐이라 자동 선택: [%s]\n", dcs[0].Name())
		} else {
			var names []string
			for _, dc := range dcs {
				names = append(names, dc.Name())
			}
			log.Fatalf("데이터센터가 %d개 존재하여 자동 선택이 불가합니다. -datacenter 옵션으로 지정하세요. (목록: %s)", len(dcs), strings.Join(names, ", "))
		}
	}

	newFinder := func() *find.Finder {
		f := find.NewFinder(client.Client, true)
		f.SetDatacenter(selectedDC)
		return f
	}

	defaultFolder, _ := baseFinder.DefaultFolder(ctx)

	vmFilterPattern := "^(" + strings.Join(safeVmNames, "|") + ")$"
	regexMatcher := regexp.MustCompile(vmFilterPattern)

	mgr := view.NewManager(client.Client)

	// 개선 1: 재귀 Finder 순회(VirtualMachineList) 대신 ContainerView로
	// VM 이름만 1회 배치 조회한다.
	vmView, err := mgr.CreateContainerView(ctx, selectedDC.Reference(), []string{"VirtualMachine"}, true)
	if err != nil {
		log.Fatalf("VM 인벤토리 조회용 ContainerView 생성 실패: %v", err)
	}
	var allVMs []mo.VirtualMachine
	if err := vmView.Retrieve(ctx, []string{"VirtualMachine"}, []string{"name"}, &allVMs); err != nil {
		log.Fatalf("VM 인벤토리 조회 실패: %v", err)
	}
	_ = vmView.Destroy(ctx)

	existingVmNames := make(map[string]bool)
	for _, vm := range allVMs {
		if regexMatcher.MatchString(vm.Name) {
			existingVmNames[vm.Name] = true
		}
	}

	// 개선 2: goroutine 안에서 finder.HostSystem()을 매번 호출하는 대신,
	// 전체 HostSystem을 ContainerView로 1회 배치 조회해 맵으로 만들어둔다.
	// worklist의 항목이 FQDN이든 짧은 이름이든 조회되도록 둘 다 키로 등록한다.
	hostView, err := mgr.CreateContainerView(ctx, selectedDC.Reference(), []string{"HostSystem"}, true)
	if err != nil {
		log.Fatalf("호스트 인벤토리 조회용 ContainerView 생성 실패: %v", err)
	}
	var allHosts []mo.HostSystem
	if err := hostView.Retrieve(ctx, []string{"HostSystem"}, []string{"name", "datastore"}, &allHosts); err != nil {
		log.Fatalf("호스트 인벤토리 조회 실패: %v", err)
	}
	_ = hostView.Destroy(ctx)

	hostByName := make(map[string]mo.HostSystem, len(allHosts)*2)
	for _, h := range allHosts {
		hostByName[h.Name] = h
		shortName := strings.Split(h.Name, ".")[0]
		hostByName[shortName] = h
	}

	pc := property.DefaultCollector(client.Client)
	preps := make([]hostPrep, len(serverList))

	var wgPrep sync.WaitGroup
	semPrep := make(chan struct{}, *prepConc)

	// 개선 3: 사전조사 진행 상황을 goroutine이 끝나는 즉시 출력한다.
	// (기존처럼 Logs에 모았다가 wgPrep.Wait() 이후 한꺼번에 출력하지 않음)
	total := len(serverList)
	var doneCount int32
	progressLogger := log.New(os.Stdout, "", 0)

	for idx, bmHost := range serverList {
		wgPrep.Add(1)
		semPrep <- struct{}{}

		go func(idx int, bmHost string) {
			defer wgPrep.Done()
			defer func() { <-semPrep }()
			defer func() {
				n := atomic.AddInt32(&doneCount, 1)
				progressLogger.Printf("[%d/%d] %s 조사 완료", n, total, bmHost)
			}()

			p := hostPrep{Index: idx, BMHost: bmHost}
			p.BaseName = strings.Split(bmHost, ".")[0]

			host, found := hostByName[bmHost]
			if !found || len(host.Datastore) == 0 {
				preps[idx] = p
				return
			}

			hostObj := object.NewHostSystem(client.Client, host.Reference())

			var dsList []mo.Datastore
			if err := pc.Retrieve(ctx, host.Datastore, []string{"summary"}, &dsList); err != nil {
				preps[idx] = p
				return
			}

			dsByRef := make(map[types.ManagedObjectReference]mo.Datastore, len(dsList))
			for _, d := range dsList {
				dsByRef[d.Self] = d
			}

			var bestDs mo.Datastore
			maxFreeSpace := int64(-1)
			for _, dsRef := range host.Datastore {
				d, found := dsByRef[dsRef]
				if !found {
					continue
				}
				if d.Summary.FreeSpace > maxFreeSpace {
					maxFreeSpace = d.Summary.FreeSpace
					bestDs = d
				}
			}
			if maxFreeSpace == -1 {
				preps[idx] = p
				return
			}

			hostRP, _ := hostObj.ResourcePool(ctx)

			pgName, hasNetwork := hostgroupMap[bmHost]
			if !hasNetwork || pgName == "" {
				progressLogger.Printf("[경고] [%s] 매핑 파일에 hostgroup이 없음 — 어댑터 없이 생성됩니다.", bmHost)
			} else {
				progressLogger.Printf("[INFO] [%s] 네트워크 선택: %s", bmHost, pgName)
			}

			p.OK = true
			p.HostObj = hostObj
			p.ResPool = hostRP
			p.Folder = defaultFolder
			p.DSName = bestDs.Summary.Name
			p.PGName = pgName
			p.HasNetwork = hasNetwork

			preps[idx] = p
		}(idx, bmHost)
	}
	wgPrep.Wait()

	type createJob struct {
		Prep   *hostPrep
		Idx    int
		VMName string
		Cfg    VMSpec
	}

	var jobs []createJob
	for i := range preps {
		p := &preps[i]
		if !p.OK {
			continue
		}
		for n := 1; n <= *vmCount; n++ {
			vmName := fmt.Sprintf("%sev%02d", p.BaseName, n)
			if existingVmNames[vmName] {
				continue
			}
			jobs = append(jobs, createJob{Prep: p, Idx: n, VMName: vmName, Cfg: vmConfigs[n]})
		}
	}

	pendingConfigs := make([]PendingConfig, len(jobs))
	pendingOK := make([]bool, len(jobs))

	if len(jobs) > 0 {
		fmt.Printf("[INFO] 생성 대상 VM %d대 — 생성 작업을 시작합니다.\n", len(jobs))

		var wgCreate sync.WaitGroup
		semCreate := make(chan struct{}, *taskConc)

		for ji := range jobs {
			wgCreate.Add(1)
			semCreate <- struct{}{}

			go func(ji int) {
				defer wgCreate.Done()
				defer func() { <-semCreate }()

				j := jobs[ji]
				p := j.Prep
				cfg := j.Cfg

				spec := types.VirtualMachineConfigSpec{
					Name:     j.VMName,
					GuestId:  "rhel8_64Guest",
					Firmware: *firmware,
					NumCPUs:  int32(cfg.Cpu),
					MemoryMB: int64(cfg.Mem * 1024),
					Files: &types.VirtualMachineFileInfo{
						VmPathName: fmt.Sprintf("[%s]", p.DSName),
					},
				}

				scsi := &types.ParaVirtualSCSIController{
					VirtualSCSIController: types.VirtualSCSIController{
						SharedBus: types.VirtualSCSISharingNoSharing,
						VirtualController: types.VirtualController{
							BusNumber: 0,
							VirtualDevice: types.VirtualDevice{
								Key: -1,
							},
						},
					},
				}

				disk := &types.VirtualDisk{
					VirtualDevice: types.VirtualDevice{
						Key:           -2,
						ControllerKey: -1,
						UnitNumber:    types.NewInt32(0),
						Backing: &types.VirtualDiskFlatVer2BackingInfo{
							VirtualDeviceFileBackingInfo: types.VirtualDeviceFileBackingInfo{
								FileName: fmt.Sprintf("[%s]", p.DSName),
							},
							DiskMode:        string(types.VirtualDiskModePersistent),
							ThinProvisioned: types.NewBool(false),
							EagerlyScrub:    types.NewBool(false),
						},
					},
					CapacityInKB: int64(cfg.Disk) * 1024 * 1024,
				}

				var nic *types.VirtualVmxnet3
				if p.HasNetwork && p.PGName != "" {
					backing := &types.VirtualEthernetCardNetworkBackingInfo{
						VirtualDeviceDeviceBackingInfo: types.VirtualDeviceDeviceBackingInfo{
							DeviceName: p.PGName,
						},
					}
					nic = &types.VirtualVmxnet3{
						VirtualVmxnet: types.VirtualVmxnet{
							VirtualEthernetCard: types.VirtualEthernetCard{
								VirtualDevice: types.VirtualDevice{
									Key:     -3,
									Backing: backing,
									Connectable: &types.VirtualDeviceConnectInfo{
										StartConnected:    true,
										AllowGuestControl: true,
									},
								},
								AddressType:      string(types.VirtualEthernetCardMacTypeGenerated),
								WakeOnLanEnabled: types.NewBool(true),
							},
						},
					}
				}

				spec.DeviceChange = []types.BaseVirtualDeviceConfigSpec{
					&types.VirtualDeviceConfigSpec{
						Operation: types.VirtualDeviceConfigSpecOperationAdd,
						Device:    scsi,
					},
					&types.VirtualDeviceConfigSpec{
						Operation:     types.VirtualDeviceConfigSpecOperationAdd,
						FileOperation: types.VirtualDeviceConfigSpecFileOperationCreate,
						Device:        disk,
					},
				}

				if nic != nil {
					spec.DeviceChange = append(spec.DeviceChange, &types.VirtualDeviceConfigSpec{
						Operation: types.VirtualDeviceConfigSpecOperationAdd,
						Device:    nic,
					})
				}

				task, err := p.Folder.CreateVM(ctx, spec, p.ResPool, p.HostObj)
				if err != nil {
					return
				}

				_ = task.Wait(ctx)

				pendingConfigs[ji] = PendingConfig{Name: j.VMName, Config: cfg}
				pendingOK[ji] = true
			}(ji)
		}
		wgCreate.Wait()
	}

	var confirmed []PendingConfig
	for i, ok := range pendingOK {
		if ok {
			confirmed = append(confirmed, pendingConfigs[i])
		}
	}

	if len(confirmed) > 0 {
		fmt.Printf("[INFO] 리소스 설정 대상 VM %d대 — 설정 작업을 시작합니다.\n", len(confirmed))

		var wgCfg sync.WaitGroup
		semCfg := make(chan struct{}, *taskConc)

		for ci := range confirmed {
			wgCfg.Add(1)
			semCfg <- struct{}{}

			go func(ci int) {
				defer wgCfg.Done()
				defer func() { <-semCfg }()

				pending := confirmed[ci]

				finder := newFinder()
				targetVM, err := finder.VirtualMachine(ctx, pending.Name)
				if err != nil {
					return
				}

				cfg := pending.Config
				spec := types.VirtualMachineConfigSpec{}

				spec.MemoryReservationLockedToMax = types.NewBool(true)
				spec.BootOptions = &types.VirtualMachineBootOptions{
					EfiSecureBootEnabled: types.NewBool(false),
				}

				var vmProps mo.VirtualMachine
				if propErr := targetVM.Properties(ctx, targetVM.Reference(), []string{"config.hardware.device"}, &vmProps); propErr == nil {
					devices := object.VirtualDeviceList(vmProps.Config.Hardware.Device)
					var bootOrder []types.BaseVirtualMachineBootOptionsBootableDevice

					disks := devices.SelectByType((*types.VirtualDisk)(nil))
					if len(disks) > 0 {
						diskKey := disks[0].GetVirtualDevice().Key
						bootOrder = append(bootOrder, &types.VirtualMachineBootOptionsBootableDiskDevice{DeviceKey: diskKey})
					}

					nics := devices.SelectByType((*types.VirtualEthernetCard)(nil))
					if len(nics) > 0 {
						nicKey := nics[0].GetVirtualDevice().Key
						bootOrder = append(bootOrder, &types.VirtualMachineBootOptionsBootableEthernetDevice{DeviceKey: nicKey})
					}

					if len(bootOrder) > 0 {
						spec.BootOptions.BootOrder = bootOrder
					}
				}

				spec.CpuAllocation = &types.ResourceAllocationInfo{
					Shares: &types.SharesInfo{
						Level:  cfg.ShareLevel,
						Shares: cfg.Share,
					},
				}

				spec.MemoryAllocation = &types.ResourceAllocationInfo{
					Reservation: types.NewInt64(int64(cfg.Mem * 1024)),
					Shares: &types.SharesInfo{
						Level:  cfg.ShareLevel,
						Shares: cfg.Share,
					},
				}

				extraConfig := []types.BaseOptionValue{
					&types.OptionValue{Key: "sched.mem.pin", Value: "TRUE"},
					&types.OptionValue{Key: "sched.mem.prealloc", Value: "TRUE"},
					&types.OptionValue{Key: "sched.mem.prealloc.pinnedMainMem", Value: "TRUE"},
					&types.OptionValue{Key: "sched.swap.vmxSwapEnabled", Value: "FALSE"},
				}
				spec.ExtraConfig = extraConfig

				task, err := targetVM.Reconfigure(ctx, spec)
				if err != nil {
					return
				}
				_ = task.Wait(ctx)
			}(ci)
		}
		wgCfg.Wait()

		fmt.Println("[INFO] VM 생성 및 리소스 설정이 완료되었습니다!")
	} else {
		fmt.Println("[INFO] 새로 생성할 VM이 없거나 이미 모두 생성되어 있습니다.")
	}
}
