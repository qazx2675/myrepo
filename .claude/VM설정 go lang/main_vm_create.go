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

type VMSpec struct {
	Cpu   int
	Mem   int
	Disk  int
	Share int
}

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
	first := true
	for scanner.Scan() {
		line := scanner.Text()
		if first {
			line = strings.TrimPrefix(line, string([]byte{0xEF, 0xBB, 0xBF})) // BOM 제거
			first = false
		}
		line = strings.TrimSpace(line)
		if line != "" && !strings.HasPrefix(line, "#") {
			lines = append(lines, line)
		}
	}
	return lines, scanner.Err()
}

// loadHostgroupMap은 "BM hostgroup이름" 형식의 파일을 읽어
// BM 호스트명 -> hostgroup(포트그룹) 이름 맵으로 만든다.
// 공백 또는 콤마 구분을 모두 허용한다.
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

func main() {
	// =========================================================================
	// 1. 파라미터(Flag) 설정
	// =========================================================================
	vcId := flag.String("id", "lscsystems@vsphere.local", "vCenter 로그인 계정 ID")
	vcTargetIP := flag.String("vcTargetIP", "", "vCenter 접속 IP (필수)")
	worklistFile := flag.String("worklistFile", "worklist.txt", "작업 대상 호스트 목록 파일")
	vmCount := flag.Int("vmCount", 2, "생성할 VM 개수 (1~3)")
	mapFile := flag.String("mapFile", "hostgroup.txt", "\"BM hostgroup이름\" 형식의 네트워크 매핑 파일 (다른 이름 지정 가능)")
	firmware := flag.String("firmware", "efi", "펌웨어 타입 (bios 또는 efi) - 정상 부팅되는 서버가 EFI(권장) 확인됨")
	datacenterName := flag.String("datacenter", "", "데이터센터 이름 (데이터센터가 여러 개면 필수, 1개뿐이면 생략 가능)")

	ev01Cpu := flag.Int("ev01Cpu", 0, "EV01 CPU")
	ev01Mem := flag.Int("ev01Mem", 0, "EV01 Mem")
	ev01Disk := flag.Int("ev01Disk", 0, "EV01 Disk")
	ev01Share := flag.Int("ev01Share", 0, "EV01 Share")

	ev02Cpu := flag.Int("ev02Cpu", 0, "EV02 CPU")
	ev02Mem := flag.Int("ev02Mem", 0, "EV02 Mem")
	ev02Disk := flag.Int("ev02Disk", 0, "EV02 Disk")
	ev02Share := flag.Int("ev02Share", 0, "EV02 Share")

	ev03Cpu := flag.Int("ev03Cpu", 1, "EV03 CPU")
	ev03Mem := flag.Int("ev03Mem", 1, "EV03 Mem")
	ev03Disk := flag.Int("ev03Disk", 20, "EV03 Disk")
	ev03Share := flag.Int("ev03Share", 1000, "EV03 Share")

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

	vmConfigs := map[int]VMSpec{
		1: {Cpu: *ev01Cpu, Mem: *ev01Mem, Disk: *ev01Disk, Share: *ev01Share},
		2: {Cpu: *ev02Cpu, Mem: *ev02Mem, Disk: *ev02Disk, Share: *ev02Share},
		3: {Cpu: *ev03Cpu, Mem: *ev03Mem, Disk: *ev03Disk, Share: *ev03Share},
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

	ctx, cancel := context.WithCancel(context.Background())
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

	finder := find.NewFinder(client.Client, true)

	if *datacenterName != "" {
		dc, dcErr := finder.Datacenter(ctx, *datacenterName)
		if dcErr != nil {
			log.Fatalf("데이터센터 '%s'를 찾을 수 없습니다: %v", *datacenterName, dcErr)
		}
		finder.SetDatacenter(dc)
		fmt.Printf("[INFO] 데이터센터 [%s] 사용\n", dc.Name())
	} else {
		dcs, dcErr := finder.DatacenterList(ctx, "*")
		if dcErr != nil || len(dcs) == 0 {
			log.Fatalf("데이터센터 목록 조회 실패: %v", dcErr)
		}
		if len(dcs) == 1 {
			finder.SetDatacenter(dcs[0])
			fmt.Printf("[INFO] 데이터센터가 1개뿐이라 자동 선택: [%s]\n", dcs[0].Name())
		} else {
			var names []string
			for _, dc := range dcs {
				names = append(names, dc.Name())
			}
			log.Fatalf("데이터센터가 %d개 존재하여 자동 선택이 불가합니다. -datacenter 옵션으로 지정하세요. (목록: %s)", len(dcs), strings.Join(names, ", "))
		}
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
		err = hostObj.Properties(ctx, hostObj.Reference(), []string{"datastore"}, &host)
		if err != nil || len(host.Datastore) == 0 {
			continue
		}

		var bestDs mo.Datastore
		maxFreeSpace := int64(-1)

		for _, dsRef := range host.Datastore {
			var ds mo.Datastore
			tempDsObj := object.NewDatastore(client.Client, dsRef)
			err := tempDsObj.Properties(ctx, tempDsObj.Reference(), []string{"summary"}, &ds)
			if err == nil && ds.Summary.FreeSpace > maxFreeSpace {
				maxFreeSpace = ds.Summary.FreeSpace
				bestDs = ds
			}
		}

		if maxFreeSpace == -1 {
			continue
		}

		hostRP, _ := hostObj.ResourcePool(ctx)
		folder, _ := finder.DefaultFolder(ctx)

		// 이 BM에 대응하는 hostgroup(포트그룹) 이름을 매핑 파일에서 찾는다
		pgName, hasNetwork := hostgroupMap[bmHost]
		if !hasNetwork || pgName == "" {
			fmt.Printf("[경고] [%s] 매핑 파일에 hostgroup이 없음 — 어댑터 없이 생성됩니다.\n", bmHost)
		} else {
			fmt.Printf("[INFO] [%s] 네트워크 선택: %s\n", bmHost, pgName)
		}

		for i := 1; i <= *vmCount; i++ {
			vmName := fmt.Sprintf("%sev%02d", baseName, i)
			cfg := vmConfigs[i]

			if !existingVmNames[vmName] {
				// 1. 기본 VM 스펙
				spec := types.VirtualMachineConfigSpec{
					Name:     vmName,
					GuestId:  "rhel8_64Guest",
					Firmware: *firmware,
					NumCPUs:  int32(cfg.Cpu),
					MemoryMB: int64(cfg.Mem * 1024),
					Files: &types.VirtualMachineFileInfo{
						VmPathName: fmt.Sprintf("[%s]", bestDs.Summary.Name),
					},
				}

				// =========================================================================
				// 2. SCSI 컨트롤러: VMware Paravirtual 유형으로 설정
				// =========================================================================
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

				// =========================================================================
				// 3. 디스크 생성: Thick Provision Lazy Zeroed 적용
				// =========================================================================
				disk := &types.VirtualDisk{
					VirtualDevice: types.VirtualDevice{
						Key:           -2,
						ControllerKey: -1,
						UnitNumber:    types.NewInt32(0),
						Backing: &types.VirtualDiskFlatVer2BackingInfo{
							VirtualDeviceFileBackingInfo: types.VirtualDeviceFileBackingInfo{
								FileName: fmt.Sprintf("[%s]", bestDs.Summary.Name),
							},
							DiskMode:        string(types.VirtualDiskModePersistent),
							ThinProvisioned: types.NewBool(false), // Thick 할당
							EagerlyScrub:    types.NewBool(false), // Lazy Zeroed (느리게 비워짐)
						},
					},
					CapacityInKB: int64(cfg.Disk) * 1024 * 1024,
				}

				// =========================================================================
				// 4. NIC: 매핑 파일에서 찾은 hostgroup(포트그룹) 이름을 그대로 DeviceName으로 사용
				// (vCenter Network 오브젝트 조회 없이 PowerCLI와 동일한 방식)
				// =========================================================================
				var nic *types.VirtualVmxnet3
				if hasNetwork && pgName != "" {
					backing := &types.VirtualEthernetCardNetworkBackingInfo{
						VirtualDeviceDeviceBackingInfo: types.VirtualDeviceDeviceBackingInfo{
							DeviceName: pgName,
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
										// Connected는 생성 시점(파워온 전)에는 의미가 없어 제외
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

				task, err := folder.CreateVM(ctx, spec, hostRP, hostObj)
				if err == nil {
					creationTasks = append(creationTasks, task)
					pendingConfigs = append(pendingConfigs, PendingConfig{
						Name:   vmName,
						Config: cfg,
					})
				}
			}
		}
	}

	if len(creationTasks) > 0 {
		for _, task := range creationTasks {
			_ = task.Wait(ctx)
		}
	}

	if len(pendingConfigs) > 0 {
		var configTasks []*object.Task

		for _, pending := range pendingConfigs {
			targetVM, err := finder.VirtualMachine(ctx, pending.Name)
			if err != nil {
				continue
			}

			cfg := pending.Config
			spec := types.VirtualMachineConfigSpec{}

			spec.MemoryReservationLockedToMax = types.NewBool(true)
			spec.BootOptions = &types.VirtualMachineBootOptions{
				EfiSecureBootEnabled: types.NewBool(false),
			}

			spec.CpuAllocation = &types.ResourceAllocationInfo{
				Shares: &types.SharesInfo{
					Level:  types.SharesLevelCustom,
					Shares: int32(cfg.Share),
				},
			}

			spec.MemoryAllocation = &types.ResourceAllocationInfo{
				Reservation: types.NewInt64(int64(cfg.Mem * 1024)),
				Shares: &types.SharesInfo{
					Level:  types.SharesLevelCustom,
					Shares: int32(cfg.Share),
				},
			}

			extraConfig := []types.BaseOptionValue{
				&types.OptionValue{Key: "sched.mem.pin", Value: "TRUE"},
				&types.OptionValue{Key: "sched.mem.prealloc", Value: "TRUE"},
				&types.OptionValue{Key: "sched.mem.prealloc.pinnedMainMem", Value: "TRUE"},
				&types.OptionValue{Key: "sched.swap.vmxSwapEnabled", Value: "FALSE"},
			}
			spec.ExtraConfig = extraConfig

			// VM Reconfigure 작업 수행 (CPU/메모리 예약 및 공유 설정)
			task, err := targetVM.Reconfigure(ctx, spec)
			if err == nil {
				configTasks = append(configTasks, task)
			}
		}

		for _, task := range configTasks {
			_ = task.Wait(ctx)
		}

		fmt.Println("[INFO] VM 생성 및 리소스 설정이 완료되었습니다!")
	} else {
		fmt.Println("[INFO] 새로 생성할 VM이 없거나 이미 모두 생성되어 있습니다.")
	}
}
