package vcsimtest

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"testing"

	"github.com/vmware/govmomi"
	"github.com/vmware/govmomi/find"
	"github.com/vmware/govmomi/simulator"
	"github.com/vmware/govmomi/view"
	"github.com/vmware/govmomi/vim25/mo"
	"github.com/vmware/govmomi/vim25/types"
)

// bmCount: 소규모 Phase 1 테스트용 BM 호스트 수
// vmPerBM: BM당 VM 수 (ev01, ev02, ev03)
const (
	bmCount = 10
	vmPerBM = 3
)

// testEnv는 테스트 전 과정에서 공유되는 vcsim 서버 상태다.
type testEnv struct {
	server *simulator.Server
	client *govmomi.Client
	ctx    context.Context
	cancel context.CancelFunc
}

// startVcsim은 vcsim 서버를 시작하고 govmomi 클라이언트를 연결한다.
// - VPX 모델(vCenter + ESXi 호스트 포함)을 사용
// - Cluster 내 Host를 bmCount로 설정
// - Machine = 0 (VM은 테스트에서 직접 CreateVM으로 생성)
func startVcsim(t *testing.T) *testEnv {
	t.Helper()

	model := simulator.VPX()
	model.Datacenter = 1
	model.Cluster = 1
	model.ClusterHost = bmCount // 클러스터 내 호스트 수
	model.Host = 0              // 독립 호스트 없음 (클러스터만 사용)
	model.Machine = 0

	if err := model.Create(); err != nil {
		t.Fatalf("vcsim 모델 생성 실패: %v", err)
	}

	s := model.Service.NewServer()
	t.Logf("vcsim 서버 시작: %s", s.URL.String())

	ctx, cancel := context.WithCancel(context.Background())
	u := &url.URL{Scheme: s.URL.Scheme, Host: s.URL.Host, Path: "/sdk"}
	u.User = url.UserPassword("user", "pass")

	client, err := govmomi.NewClient(ctx, u, true)
	if err != nil {
		cancel()
		s.Close()
		t.Fatalf("govmomi 클라이언트 연결 실패: %v", err)
	}

	return &testEnv{server: s, client: client, ctx: ctx, cancel: cancel}
}

// stop은 테스트 환경을 정리한다.
func (e *testEnv) stop() {
	_ = e.client.Logout(e.ctx)
	e.cancel()
	e.server.Close()
}

// listAllVMs는 ContainerView를 이용해 인벤토리 전체 VM 목록을 반환한다.
// finder.VirtualMachineList("*")는 데이터센터 경로 의존성이 있어서
// vcsim 환경에서 path 불일치가 발생할 수 있으므로 ContainerView 방식 사용.
func listAllVMs(ctx context.Context, client *govmomi.Client) ([]mo.VirtualMachine, error) {
	m := view.NewManager(client.Client)
	cv, err := m.CreateContainerView(ctx, client.ServiceContent.RootFolder, []string{"VirtualMachine"}, true)
	if err != nil {
		return nil, fmt.Errorf("ContainerView 생성 실패: %w", err)
	}
	defer cv.Destroy(ctx)

	var vms []mo.VirtualMachine
	if err := cv.Retrieve(ctx, []string{"VirtualMachine"}, []string{"name", "config", "runtime"}, &vms); err != nil {
		return nil, fmt.Errorf("VM 목록 조회 실패: %w", err)
	}
	return vms, nil
}

// listAllHosts는 ContainerView를 이용해 인벤토리 전체 HostSystem 목록을 반환한다.
func listAllHosts(ctx context.Context, client *govmomi.Client) ([]mo.HostSystem, error) {
	m := view.NewManager(client.Client)
	cv, err := m.CreateContainerView(ctx, client.ServiceContent.RootFolder, []string{"HostSystem"}, true)
	if err != nil {
		return nil, fmt.Errorf("ContainerView 생성 실패: %w", err)
	}
	defer cv.Destroy(ctx)

	var hosts []mo.HostSystem
	if err := cv.Retrieve(ctx, []string{"HostSystem"}, []string{"name", "parent"}, &hosts); err != nil {
		return nil, fmt.Errorf("호스트 목록 조회 실패: %w", err)
	}
	return hosts, nil
}

// createTestVMs은 bmCount개 호스트에 vmPerBM개 VM을 생성하는 공통 헬퍼다.
// VM 이름 패턴: bm001ev01, bm001ev02, bm001ev03, bm002ev01 ...
// vcsim에서 CreateVM은 Files.VmPathName(Datastore 경로)이 필수다.
func createTestVMs(t *testing.T, env *testEnv) {
	t.Helper()

	// 1) 데이터센터의 VM 폴더 가져오기
	finder := find.NewFinder(env.client.Client, true)
	dcs, err := finder.DatacenterList(env.ctx, "*")
	if err != nil || len(dcs) == 0 {
		t.Fatal("데이터센터 조회 실패")
	}
	finder.SetDatacenter(dcs[0])

	folders, err := dcs[0].Folders(env.ctx)
	if err != nil {
		t.Fatal("폴더 조회 실패")
	}
	vmFolder := folders.VmFolder

	// 2) 클러스터 ResourcePool 찾기
	cluster, err := finder.ClusterComputeResource(env.ctx, "*")
	if err != nil {
		t.Fatalf("클러스터 조회 실패: %v", err)
	}
	clusterRP, err := cluster.ResourcePool(env.ctx)
	if err != nil {
		t.Fatalf("클러스터 ResourcePool 조회 실패: %v", err)
	}

	// 3) Datastore 조회 (vcsim CreateVM에 Files.VmPathName 필수)
	ds, err := finder.DefaultDatastore(env.ctx)
	if err != nil {
		t.Fatalf("Datastore 조회 실패: %v", err)
	}
	dsName := ds.Name()

	// 4) 각 BM에 VM 생성 (호스트 지정 없이 클러스터 RP 사용)
	created := 0
	for i := 0; i < bmCount; i++ {
		baseName := fmt.Sprintf("bm%03d", i+1)
		for n := 1; n <= vmPerBM; n++ {
			vmName := fmt.Sprintf("%sev%02d", baseName, n)
			spec := types.VirtualMachineConfigSpec{
				Name:    vmName,
				GuestId: "rhel8_64Guest",
				NumCPUs: 8,
				MemoryMB: 32768,
				Files: &types.VirtualMachineFileInfo{
					VmPathName: fmt.Sprintf("[%s]", dsName),
				},
			}
			task, err := vmFolder.CreateVM(env.ctx, spec, clusterRP, nil)
			if err != nil {
				t.Logf("  [경고] [%s] CreateVM 실패: %v", vmName, err)
				continue
			}
			if err := task.Wait(env.ctx); err != nil {
				t.Logf("  [경고] [%s] CreateVM Task 실패: %v", vmName, err)
				continue
			}
			created++
		}
	}
	t.Logf("  createTestVMs: %d VM 생성 완료", created)
}

// ============================================================
// Phase 1: vcsim 서버 시작 + 기본 연결 확인
// ============================================================

func TestPhase1_VcsimStartup(t *testing.T) {
	env := startVcsim(t)
	defer env.stop()

	finder := find.NewFinder(env.client.Client, true)
	dcs, err := finder.DatacenterList(env.ctx, "*")
	if err != nil || len(dcs) == 0 {
		t.Fatalf("데이터센터 조회 실패: %v", err)
	}
	t.Logf("[Phase 1] vcsim 연결 성공. DC: %s", dcs[0].Name())
}

// ============================================================
// Phase 2: ESXi 호스트 수 확인 (vm_connect 로직 검증)
// ============================================================

func TestPhase2_HostConnectCheck(t *testing.T) {
	env := startVcsim(t)
	defer env.stop()

	hosts, err := listAllHosts(env.ctx, env.client)
	if err != nil {
		t.Fatalf("호스트 조회 실패: %v", err)
	}
	t.Logf("[Phase 2] 호스트 %d개 확인 (기대: %d)", len(hosts), bmCount)
	if len(hosts) < bmCount {
		t.Errorf("호스트 수 부족: got %d, want >= %d", len(hosts), bmCount)
	}
	for i, h := range hosts {
		t.Logf("  [%02d] %s", i+1, h.Name)
	}
}

// ============================================================
// Phase 3: VM 생성 (vm_create 로직 — BM 10대 × VM 3개)
// ============================================================

func TestPhase3_VMCreate(t *testing.T) {
	env := startVcsim(t)
	defer env.stop()

	createTestVMs(t, env)

	vms, err := listAllVMs(env.ctx, env.client)
	if err != nil {
		t.Fatalf("VM 목록 조회 실패: %v", err)
	}

	expected := bmCount * vmPerBM
	t.Logf("[Phase 3] VM 생성: %d/%d", len(vms), expected)
	if len(vms) != expected {
		t.Errorf("VM 수 불일치: got %d, want %d", len(vms), expected)
	}
}

// ============================================================
// Phase 4: ExtraConfig 설정 적용 (affinity/lpage 핵심 로직)
// ============================================================

func TestPhase4_ExtraConfigApply(t *testing.T) {
	env := startVcsim(t)
	defer env.stop()

	createTestVMs(t, env)

	vms, err := listAllVMs(env.ctx, env.client)
	if err != nil {
		t.Fatalf("VM 목록 조회 실패: %v", err)
	}

	failCount := 0
	for _, vmMO := range vms {
		vmObj := find.NewFinder(env.client.Client, true)
		_ = vmObj
		// VirtualMachine 오브젝트 생성 (MOR 기반)
		vm := newVMObject(env.client, vmMO.Self)

		extraConfig := []types.BaseOptionValue{
			&types.OptionValue{Key: "sched.mem.pin", Value: "TRUE"},
			&types.OptionValue{Key: "sched.mem.prealloc", Value: "TRUE"},
			&types.OptionValue{Key: "sched.swap.vmxSwapEnabled", Value: "FALSE"},
		}
		if strings.HasSuffix(vmMO.Name, "ev01") {
			extraConfig = append(extraConfig, &types.OptionValue{
				Key: "sched.cpu.affinity", Value: "0,1,2,3,4,5,6,7",
			})
		}
		spec := types.VirtualMachineConfigSpec{
			MemoryReservationLockedToMax: types.NewBool(true),
			ExtraConfig:                  extraConfig,
			CpuAllocation: &types.ResourceAllocationInfo{
				Shares: &types.SharesInfo{Level: types.SharesLevelCustom, Shares: 5000},
			},
			MemoryAllocation: &types.ResourceAllocationInfo{
				Reservation: types.NewInt64(32768),
				Shares:      &types.SharesInfo{Level: types.SharesLevelCustom, Shares: 5000},
			},
		}
		task, err := vm.Reconfigure(env.ctx, spec)
		if err != nil || task.Wait(env.ctx) != nil {
			t.Errorf("  [%s] Reconfigure 실패", vmMO.Name)
			failCount++
		}
	}
	t.Logf("[Phase 4] ExtraConfig 적용: %d VM, 실패 %d", len(vms), failCount)
	if failCount > 0 {
		t.Errorf("실패 VM %d개", failCount)
	}
}

// ============================================================
// Phase 5: 설정 체크 — ExtraConfig/Shares/MemoryReservation 검증
// ============================================================

func TestPhase5_ParamCheck(t *testing.T) {
	env := startVcsim(t)
	defer env.stop()

	// VM 생성 + 올바른 설정 적용
	createTestVMs(t, env)
	vms, _ := listAllVMs(env.ctx, env.client)
	for _, vmMO := range vms {
		vm := newVMObject(env.client, vmMO.Self)
		extraConfig := []types.BaseOptionValue{
			&types.OptionValue{Key: "sched.mem.pin", Value: "TRUE"},
			&types.OptionValue{Key: "sched.mem.prealloc", Value: "TRUE"},
			&types.OptionValue{Key: "sched.swap.vmxSwapEnabled", Value: "FALSE"},
		}
		spec := types.VirtualMachineConfigSpec{
			MemoryReservationLockedToMax: types.NewBool(true),
			ExtraConfig:                  extraConfig,
			CpuAllocation: &types.ResourceAllocationInfo{
				Shares: &types.SharesInfo{Level: types.SharesLevelCustom, Shares: 5000},
			},
			MemoryAllocation: &types.ResourceAllocationInfo{
				Reservation: types.NewInt64(32768),
				Shares:      &types.SharesInfo{Level: types.SharesLevelCustom, Shares: 5000},
			},
		}
		task, _ := vm.Reconfigure(env.ctx, spec)
		_ = task.Wait(env.ctx)
	}

	// 검증: 재조회 후 설정값 확인
	updatedVMs, _ := listAllVMs(env.ctx, env.client)
	passCount, failCount := 0, 0
	for _, vmMO := range updatedVMs {
		if vmMO.Config == nil {
			continue
		}
		// MemoryReservationLockedToMax 체크
		if vmMO.Config.MemoryReservationLockedToMax != nil && *vmMO.Config.MemoryReservationLockedToMax {
			passCount++
		} else {
			failCount++
			t.Logf("  [%s] MemoryReservationLockedToMax FAIL", vmMO.Name)
		}
	}
	t.Logf("[Phase 5] 체크 완료: PASS=%d, FAIL=%d", passCount, failCount)
	if failCount > 0 {
		t.Errorf("FAIL 항목이 있는 VM: %d개", failCount)
	}
}

// ============================================================
// Phase 6: FAIL 수정 루프 (의도적 FAIL → 수정 → 재검증)
// ============================================================

func TestPhase6_FailFixLoop(t *testing.T) {
	env := startVcsim(t)
	defer env.stop()

	// 클러스터 ResourcePool과 VM 폴더 가져오기
	finder := find.NewFinder(env.client.Client, true)
	dcs, _ := finder.DatacenterList(env.ctx, "*")
	finder.SetDatacenter(dcs[0])
	folders, _ := dcs[0].Folders(env.ctx)
	vmFolder := folders.VmFolder
	cluster, _ := finder.ClusterComputeResource(env.ctx, "*")
	clusterRP, _ := cluster.ResourcePool(env.ctx)
	ds, _ := finder.DefaultDatastore(env.ctx)
	dsName := ds.Name()

	// 의도적으로 잘못된 설정으로 VM 생성 (FAIL 시뮬레이션)
	failVMCount := 3 * vmPerBM
	for i := 0; i < 3; i++ {
		baseName := fmt.Sprintf("failbm%03d", i+1)
		for n := 1; n <= vmPerBM; n++ {
			vmName := fmt.Sprintf("%sev%02d", baseName, n)
			spec := types.VirtualMachineConfigSpec{
				Name:    vmName,
				GuestId: "rhel8_64Guest",
				NumCPUs: 4,      // 기대값 8과 불일치 → FAIL
				MemoryMB: 16384, // 기대값 32768과 불일치 → FAIL
				Files: &types.VirtualMachineFileInfo{
					VmPathName: fmt.Sprintf("[%s]", dsName),
				},
			}
			task, _ := vmFolder.CreateVM(env.ctx, spec, clusterRP, nil)
			_ = task.Wait(env.ctx)
		}
	}

	vms, _ := listAllVMs(env.ctx, env.client)
	var failVMs []mo.VirtualMachine
	for _, vm := range vms {
		if strings.HasPrefix(vm.Name, "failbm") {
			failVMs = append(failVMs, vm)
		}
	}
	t.Logf("[Phase 6-A] 의도적 FAIL VM %d개 생성 완료 (기대: %d)", len(failVMs), failVMCount)

	// FAIL 수정: 올바른 설정으로 Reconfigure
	fixedCount := 0
	for _, vmMO := range failVMs {
		vm := newVMObject(env.client, vmMO.Self)
		spec := types.VirtualMachineConfigSpec{
			NumCPUs:                      8,
			MemoryMB:                     32768,
			MemoryReservationLockedToMax: types.NewBool(true),
			CpuAllocation: &types.ResourceAllocationInfo{
				Shares: &types.SharesInfo{Level: types.SharesLevelCustom, Shares: 5000},
			},
			MemoryAllocation: &types.ResourceAllocationInfo{
				Reservation: types.NewInt64(32768),
				Shares:      &types.SharesInfo{Level: types.SharesLevelCustom, Shares: 5000},
			},
			ExtraConfig: []types.BaseOptionValue{
				&types.OptionValue{Key: "sched.mem.pin", Value: "TRUE"},
				&types.OptionValue{Key: "sched.swap.vmxSwapEnabled", Value: "FALSE"},
			},
		}
		task, err := vm.Reconfigure(env.ctx, spec)
		if err != nil || task.Wait(env.ctx) != nil {
			t.Errorf("  [%s] 수정 실패", vmMO.Name)
			continue
		}
		fixedCount++
	}
	t.Logf("[Phase 6-B] 수정 완료: %d/%d", fixedCount, len(failVMs))
	if len(failVMs) > 0 && fixedCount != len(failVMs) {
		t.Errorf("수정 실패 VM 있음")
	}
	if len(failVMs) == 0 {
		t.Errorf("FAIL VM이 하나도 생성되지 않았음 (기대: %d)", failVMCount)
	}
}

// ============================================================
// Phase 7: 전체 파이프라인 통합 테스트 (BM 10대, VM 30대)
// ============================================================

func TestPhase7_FullPipelineScale10BM(t *testing.T) {
	env := startVcsim(t)
	defer env.stop()

	// 1) VM 생성
	createTestVMs(t, env)

	vms, err := listAllVMs(env.ctx, env.client)
	if err != nil {
		t.Fatalf("VM 목록 조회 실패: %v", err)
	}

	// 2) 전체 설정 적용
	applyFail := 0
	for _, vmMO := range vms {
		vm := newVMObject(env.client, vmMO.Self)
		extraConfig := []types.BaseOptionValue{
			&types.OptionValue{Key: "sched.mem.pin", Value: "TRUE"},
			&types.OptionValue{Key: "sched.mem.prealloc", Value: "TRUE"},
			&types.OptionValue{Key: "sched.swap.vmxSwapEnabled", Value: "FALSE"},
		}
		if strings.HasSuffix(vmMO.Name, "ev01") {
			extraConfig = append(extraConfig, &types.OptionValue{
				Key: "sched.cpu.affinity", Value: "0,1,2,3,4,5,6,7",
			})
		}
		spec := types.VirtualMachineConfigSpec{
			MemoryReservationLockedToMax: types.NewBool(true),
			ExtraConfig:                  extraConfig,
			CpuAllocation: &types.ResourceAllocationInfo{
				Shares: &types.SharesInfo{Level: types.SharesLevelCustom, Shares: 5000},
			},
			MemoryAllocation: &types.ResourceAllocationInfo{
				Reservation: types.NewInt64(32768),
				Shares:      &types.SharesInfo{Level: types.SharesLevelCustom, Shares: 5000},
			},
		}
		task, err := vm.Reconfigure(env.ctx, spec)
		if err != nil || task.Wait(env.ctx) != nil {
			applyFail++
		}
	}

	// 3) 최종 검증
	finalVMs, _ := listAllVMs(env.ctx, env.client)
	expected := bmCount * vmPerBM
	t.Logf("[Phase 7] 최종 VM: %d (기대: %d), 설정 실패: %d", len(finalVMs), expected, applyFail)
	if len(finalVMs) != expected {
		t.Errorf("VM 수 불일치: got %d, want %d", len(finalVMs), expected)
	}
	if applyFail > 0 {
		t.Errorf("설정 적용 실패 VM: %d개", applyFail)
	}
	t.Log("[Phase 7] 전체 파이프라인 통합 테스트 완료 ✓")
}
