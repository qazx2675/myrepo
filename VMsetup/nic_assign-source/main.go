// nic_assign: 이미 만들어진 VM의 "네트워크 어댑터 1"을 지정한 표준 vSwitch 포트그룹으로 바꾸고,
// "연결됨(Connected)"과 "전원을 켤 때 연결(StartConnected)"을 체크한다. 대상은 할당표 파일
// (한 줄에 "VM이름 포트그룹")로 받고, VM끼리는 워커풀로 병렬 처리한다.
//
// 연결 상태 반영 방식은 vm-network-migration Step 3(nm-connect)의 SetPortgroup -> EnsureConnectState를
// 그대로 따른다: 백킹 교체와 연결 상태를 한 번에 보낸 뒤, API로 다시 읽어 실제로 체크됐는지 확인하고
// 아니면 연결 상태만 다시 보낸다(전원이 켜진 VM에서 "연결됨"이 빠지는 경우가 있어서).
// 전원이 꺼진 VM은 vSphere 특성상 "연결됨"을 켤 수 없으므로 "전원을 켤 때 연결"만 확인한다.
package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"log"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/vmware/govmomi"
	"github.com/vmware/govmomi/object"
	"github.com/vmware/govmomi/view"
	"github.com/vmware/govmomi/vim25/mo"
	"github.com/vmware/govmomi/vim25/types"
)

const (
	connectAttempts     = 5
	connectPollInterval = 2 * time.Second
)

type assignment struct {
	VM string
	PG string
}

type result struct {
	VM      string
	Changed bool
	Err     error
	Note    string
}

var bomPrefix = string(rune(0xFEFF))

// loadAssignments는 "VM이름 포트그룹"(공백 또는 콤마 구분) 줄을 읽는다. '#' 주석/빈 줄은 무시.
// 같은 VM이 두 번 나오면 어느 쪽인지 모호하므로 에러다.
func loadAssignments(path string) ([]assignment, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	splitter := regexp.MustCompile(`[,\s]+`)
	seen := map[string]int{}
	var out []assignment
	sc := bufio.NewScanner(f)
	lineNo := 0
	for sc.Scan() {
		lineNo++
		line := sc.Text()
		if lineNo == 1 {
			line = strings.TrimPrefix(line, bomPrefix)
		}
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := splitter.Split(line, -1)
		if len(fields) != 2 {
			return nil, fmt.Errorf("%s:%d 형식 오류 (\"VM이름 포트그룹\" 두 칸이어야 함): %q", path, lineNo, line)
		}
		if prev, dup := seen[fields[0]]; dup {
			return nil, fmt.Errorf("%s:%d VM %q 가 %d번째 줄에도 있습니다 — 한 VM은 한 줄만 적어주세요", path, lineNo, fields[0], prev)
		}
		seen[fields[0]] = lineNo
		out = append(out, assignment{VM: fields[0], PG: fields[1]})
	}
	return out, sc.Err()
}

// firstNIC는 "네트워크 어댑터 1"(장치 목록의 첫 번째 이더넷 카드)을 돌려준다.
func firstNIC(devs []types.BaseVirtualDevice) types.BaseVirtualEthernetCard {
	for _, d := range devs {
		if c, ok := d.(types.BaseVirtualEthernetCard); ok {
			return c
		}
	}
	return nil
}

func backingPG(card *types.VirtualEthernetCard) string {
	if b, ok := card.Backing.(*types.VirtualEthernetCardNetworkBackingInfo); ok {
		return b.DeviceName
	}
	return ""
}

// readNIC는 VM의 전원 상태와 네트워크 어댑터 1을 다시 읽는다.
func readNIC(ctx context.Context, vm *object.VirtualMachine) (types.BaseVirtualEthernetCard, bool, error) {
	var m mo.VirtualMachine
	if err := vm.Properties(ctx, vm.Reference(), []string{"config.hardware.device", "runtime.powerState"}, &m); err != nil {
		return nil, false, fmt.Errorf("장치 목록 조회 실패: %w", err)
	}
	if m.Config == nil {
		return nil, false, fmt.Errorf("VM 설정을 읽지 못했습니다")
	}
	nic := firstNIC(m.Config.Hardware.Device)
	if nic == nil {
		return nil, false, fmt.Errorf("네트워크 어댑터가 없습니다 (이 도구는 기존 어댑터 1만 변경하고 새로 추가하지 않습니다)")
	}
	return nic, m.Runtime.PowerState == types.VirtualMachinePowerStatePoweredOn, nil
}

func reconfigureNIC(ctx context.Context, vm *object.VirtualMachine, nic types.BaseVirtualEthernetCard) error {
	spec := types.VirtualMachineConfigSpec{
		DeviceChange: []types.BaseVirtualDeviceConfigSpec{
			&types.VirtualDeviceConfigSpec{
				Operation: types.VirtualDeviceConfigSpecOperationEdit,
				Device:    nic.(types.BaseVirtualDevice),
			},
		},
	}
	task, err := vm.Reconfigure(ctx, spec)
	if err != nil {
		return fmt.Errorf("Reconfigure 요청 실패: %w", err)
	}
	if err := task.Wait(ctx); err != nil {
		return fmt.Errorf("Reconfigure 실패: %w", err)
	}
	return nil
}

// assign은 VM 1대의 네트워크 어댑터 1을 pg로 바꾸고 연결 상태를 보장한다. 이미 원하는 상태면 아무것도 안 한다(멱등).
func assign(ctx context.Context, vm *object.VirtualMachine, pg string) (changed bool, note string, err error) {
	nic, poweredOn, err := readNIC(ctx, vm)
	if err != nil {
		return false, "", err
	}
	matches := func(c *types.VirtualEthernetCard, on bool) bool {
		if backingPG(c) != pg || c.Connectable == nil || !c.Connectable.StartConnected {
			return false
		}
		return !on || c.Connectable.Connected
	}

	card := nic.GetVirtualEthernetCard()
	if !matches(card, poweredOn) {
		card.Backing = &types.VirtualEthernetCardNetworkBackingInfo{
			VirtualDeviceDeviceBackingInfo: types.VirtualDeviceDeviceBackingInfo{DeviceName: pg},
		}
		if card.Connectable == nil {
			card.Connectable = &types.VirtualDeviceConnectInfo{}
		}
		card.Connectable.StartConnected = true
		card.Connectable.AllowGuestControl = true
		card.Connectable.Connected = poweredOn
		if err := reconfigureNIC(ctx, vm, nic); err != nil {
			return false, "", err
		}
		changed = true
	}

	// EnsureConnectState: 실제로 체크가 반영됐는지 다시 읽어 확인하고, 아니면 연결 상태만 다시 보낸다.
	for i := 0; ; i++ {
		nic, poweredOn, err = readNIC(ctx, vm)
		if err != nil {
			return changed, "", err
		}
		card = nic.GetVirtualEthernetCard()
		if matches(card, poweredOn) {
			break
		}
		if backingPG(card) != pg {
			return changed, "", fmt.Errorf("포트그룹이 %q 로 바뀌지 않았습니다(현재 %q) — BM에 포트그룹이 있는지 확인하세요", pg, backingPG(card))
		}
		if i >= connectAttempts {
			return changed, "", fmt.Errorf("포트그룹에는 붙었지만 %d회 재시도 후에도 '연결됨'/'전원을 켤 때 연결'이 체크되지 않았습니다", connectAttempts)
		}
		card.Connectable.StartConnected = true
		if poweredOn {
			card.Connectable.Connected = true
		}
		if err := reconfigureNIC(ctx, vm, nic); err != nil {
			return changed, "", fmt.Errorf("연결 상태 반영 실패: %w", err)
		}
		changed = true
		select {
		case <-ctx.Done():
			return changed, "", ctx.Err()
		case <-time.After(connectPollInterval):
		}
	}

	if poweredOn {
		note = "연결됨+전원 켤 때 연결"
	} else {
		note = "전원 꺼짐: 전원 켤 때 연결만 체크(연결됨은 전원을 켜면 적용)"
	}
	return changed, note, nil
}

func main() {
	vcId := flag.String("id", "lscsystems@vsphere.local", "vCenter 로그인 계정 ID")
	vcTargetIP := flag.String("vcTargetIP", "", "vCenter 접속 IP (필수)")
	mapFile := flag.String("mapFile", "nic_map.txt", "할당표 파일 — 한 줄에 \"VM이름 포트그룹\" (공백 또는 콤마 구분, '#' 주석)")
	concurrency := flag.Int("concurrency", 20, "동시에 처리할 VM 수")
	flag.Parse()

	if *vcTargetIP == "" {
		log.Fatal("필수 파라미터(-vcTargetIP)가 누락되었습니다.")
	}
	if *concurrency < 1 {
		log.Fatalf("-concurrency 값이 올바르지 않습니다: %d (1 이상)", *concurrency)
	}
	vcPassword := os.Getenv("VC_PASSWORD")
	if vcPassword == "" {
		log.Fatal("인증 정보 로드 실패: VC_PASSWORD 환경 변수가 설정되지 않았습니다.")
	}

	assigns, err := loadAssignments(*mapFile)
	if err != nil {
		log.Fatalf("할당표 읽기 실패: %v", err)
	}
	if len(assigns) == 0 {
		log.Fatalf("%s 에 대상이 없습니다.", *mapFile)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Minute)
	defer cancel()

	u := &url.URL{Scheme: "https", Host: *vcTargetIP, Path: "/sdk"}
	u.User = url.UserPassword(*vcId, vcPassword)
	client, err := govmomi.NewClient(ctx, u, true)
	if err != nil {
		log.Fatalf("vCenter 접속 실패: %v", err)
	}
	defer client.Logout(ctx)

	fmt.Printf("[INFO] 네트워크 어댑터 1 포트그룹 할당 시작 (대상 VM %d대, 동시 처리 %d, 접속 계정: %s)\n", len(assigns), *concurrency, *vcId)

	// 데이터센터 수·폴더 깊이와 무관하게 VM 이름을 1회 배치 조회한다.
	v, err := view.NewManager(client.Client).CreateContainerView(ctx, client.ServiceContent.RootFolder, []string{"VirtualMachine"}, true)
	if err != nil {
		log.Fatalf("VM 조회용 ContainerView 생성 실패: %v", err)
	}
	var all []mo.VirtualMachine
	err = v.Retrieve(ctx, []string{"VirtualMachine"}, []string{"name"}, &all)
	_ = v.Destroy(ctx)
	if err != nil {
		log.Fatalf("VM 목록 조회 실패: %v", err)
	}
	byName := map[string][]types.ManagedObjectReference{}
	for _, vm := range all {
		byName[vm.Name] = append(byName[vm.Name], vm.Self)
	}

	results := make([]result, len(assigns))
	var wg sync.WaitGroup
	sem := make(chan struct{}, *concurrency)
	for i, a := range assigns {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int, a assignment) {
			defer wg.Done()
			defer func() { <-sem }()
			r := result{VM: a.VM}
			refs := byName[a.VM]
			switch {
			case len(refs) == 0:
				r.Err = fmt.Errorf("VM을 찾을 수 없습니다")
			case len(refs) > 1:
				r.Err = fmt.Errorf("같은 이름의 VM이 %d대 있어 모호합니다", len(refs))
			default:
				r.Changed, r.Note, r.Err = assign(ctx, object.NewVirtualMachine(client.Client, refs[0]), a.PG)
			}
			results[i] = r
		}(i, a)
	}
	wg.Wait()

	sort.SliceStable(results, func(i, j int) bool { return results[i].VM < results[j].VM })
	pgByVM := map[string]string{}
	for _, a := range assigns {
		pgByVM[a.VM] = a.PG
	}
	failed := 0
	for _, r := range results {
		switch {
		case r.Err != nil:
			failed++
			fmt.Printf("  -> [%s] 실패: %v\n", r.VM, r.Err)
		case r.Changed:
			fmt.Printf("  -> [%s] 변경: 포트그룹=%s (%s)\n", r.VM, pgByVM[r.VM], r.Note)
		default:
			fmt.Printf("  -> [%s] 이미 적용됨: 포트그룹=%s (%s)\n", r.VM, pgByVM[r.VM], r.Note)
		}
	}
	if failed > 0 {
		fmt.Printf("\n[일부 실패] %d대 실패 (전체 %d대)\n", failed, len(results))
		os.Exit(1)
	}
	fmt.Printf("\n[INFO] 완료: %d대 모두 포트그룹 할당 및 연결 상태 확인 완료\n", len(results))
}
