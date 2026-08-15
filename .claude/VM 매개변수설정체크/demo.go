// demo.go: -demo 모드 전용 합성(synthetic) 테스트 데이터.
//
// 실제 운영 vCenter/VM 설정은 절대 건드리지 않으면서, affinity 항목이 많은
// 8~16 vCPU급 VM에서 OK/FAIL/설정없음(개수 불일치 포함)이 실제로 어떻게 찍히는지
// 보여주기 위한 용도다. vCenter 연결 자체를 하지 않고, 이 파일이 만든 가짜
// model.VMInfo를 그대로 checker/report 파이프라인에 태운다 — 즉 프로덕션과
// 완전히 동일한 코드 경로(FetchVMs만 우회)로 결과를 만들어내므로, 여기서 보이는
// 동작은 실제 vCenter 데이터에 대해서도 그대로 재현된다.
//
// -demo 모드는 --cores/--numa/--cpu/... 같은 실행 플래그를 전부 무시하고
// 아래 고정된 기대값(demoExpect*)만 사용한다 — 매번 같은 결과가 재현되도록
// 일부러 사용자 입력에 의존하지 않게 만들었다.
package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"vm-param-check/checker"
	"vm-param-check/model"
	"vm-param-check/report"
)

// runDemo는 -demo 모드의 진입점. vCenter 연결 없이 BuildSyntheticVMs()가 만든 가짜
// VM들을 그대로 checker/report 파이프라인에 태워서 콘솔+CSV를 출력한다.
// -onlyFail/-noColor는 실제 모드와 동일하게 여기서도 존중한다 — 데모라고 이 두 플래그의
// 동작 자체가 달라지면 데모를 봐도 실제 동작을 확인한 게 아니게 되기 때문이다.
func runDemo(out string, onlyFail, noColor bool) {
	fmt.Println("=== -demo 모드: 실제 vCenter에 연결하지 않고, 아래 고정된 기대값으로 합성 VM 3대를 체크합니다 ===")
	fmt.Printf("고정 기대값: ht=on cores=%d numa=%d cpu=%d mem=%dGB disk=%dGB shares-ev01/ev02=%d\n\n",
		demoExpectCores, demoExpectNuma, demoExpectCPU, demoExpectMemGB, demoExpectDiskGB, demoExpectShares)

	vms := BuildSyntheticVMs()
	shares := checker.SharesExpect{EV01: demoExpectShares, EV02: intPtr(demoExpectShares)}
	affinityEV02 := demoAffinityEV02()
	singleVMMode := len(vms) == 1 // 합성 데이터는 항상 3대라 여기선 사실상 false

	var findings []model.Finding
	for _, vm := range vms {
		group := classifyGroup(vm.Hostname)

		findings = append(findings, checker.CheckFixed(vm)...)
		findings = append(findings, checker.CheckTopology(vm, checker.CoresExpect{Base: demoExpectCores}, checker.NumaExpect{Base: demoExpectNuma}, group, singleVMMode)...)
		findings = append(findings, checker.CheckHardware(vm, checker.CPUExpect{Base: demoExpectCPU}, checker.MemExpect{Base: demoExpectMemGB}, checker.DiskExpect{Base: demoExpectDiskGB}, shares, group, singleVMMode)...)
		findings = append(findings, checker.CheckHostPower(vm))
		findings = append(findings, checker.CheckNetwork(vm)...)

		switch group {
		case "ev01":
			expected := checker.GenerateExpectedAffinityEV01(vm.NumCPU, demoExpectHT)
			findings = append(findings, checker.CheckAffinity(vm, expected, "ev01")...)
		case "ev02":
			findings = append(findings, checker.CheckAffinity(vm, affinityEV02, "ev02")...)
		}
	}

	if onlyFail {
		before := len(report.Summarize(findings))
		findings = report.FilterOnlyFail(findings)
		after := len(report.Summarize(findings))
		fmt.Printf("-onlyFail: PASS인 VM %d대는 결과에서 제외 (총 %d대 중 %d대만 출력)\n", before-after, before, after)
	}

	fmt.Println()
	report.PrintConsole(os.Stdout, findings, !noColor, !onlyFail)

	base := out
	if base == "" {
		base = fmt.Sprintf("vm-param-check_demo_%s.csv", time.Now().Format("20060102_150405"))
	}
	summaryPath := strings.TrimSuffix(base, ".csv") + "_summary.csv"
	if err := report.WriteSummaryCSV(summaryPath, findings); err != nil {
		fmt.Fprintf(os.Stderr, "요약 CSV 저장 실패: %v\n", err)
		return
	}
	if err := report.WriteCSV(base, findings); err != nil {
		fmt.Fprintf(os.Stderr, "상세 CSV 저장 실패: %v\n", err)
		return
	}
	fmt.Printf("\nCSV 저장 완료: 요약=%s, 상세=%s (%d개 항목)\n", summaryPath, base, len(findings))
}

func intPtr(v int) *int { return &v }

const (
	demoExpectHT     = true // ht=on
	demoExpectCores  = 4
	demoExpectNuma   = 4
	demoExpectCPU    = 8
	demoExpectMemGB  = 32
	demoExpectDiskGB = 200
	demoExpectShares = 2000
)

// demoAffinityEV02는 ev02 그룹의 "파일에서 읽은 기대값"을 흉내낸 것이다.
// 실제 -affinity-ev02 파일 대신 코드에 직접 박아둠 (16개 vCPU 전부에 대한 기대값).
func demoAffinityEV02() map[string]string {
	expected := map[string]string{}
	for i := 0; i < 16; i++ {
		expected[keyFor(i)] = strconv.Itoa(i) // 단일 코어 고정 핀 패턴 (ev02는 자동계산이 아니라 파일 기반이라 임의 패턴)
	}
	return expected
}

func keyFor(i int) string { return "sched.vcpu" + strconv.Itoa(i) + ".affinity" }

// BuildSyntheticVMs는 -demo 모드에서 쓸 가짜 VM 3대를 만든다.
//  1. demo-ev01-allok    — ev01, 8vCPU, 전 항목 OK (정상 케이스 데모)
//  2. demo-ev01-fail     — ev01, 8vCPU, affinity 일부 불일치 + 개수 부족 + 기타 설정 FAIL 섞음
//  3. demo-ev02-countgap — ev02, 16vCPU, affinity 파일과 비교해서 뒷부분 3개가 통째로 "설정없음"
func BuildSyntheticVMs() []model.VMInfo {
	return []model.VMInfo{
		buildAllOKVM(),
		buildFailVM(),
		buildEV02CountGapVM(),
	}
}

func buildAllOKVM() model.VMInfo {
	vm := model.VMInfo{
		Name:              "demo-ev01-allok",
		Hostname:          "demo-ev01-allok",
		HostnameSource:    "guest.hostName",
		NumCPU:            demoExpectCPU,
		NumCoresPerSocket: demoExpectCores,
		MemoryMB:          demoExpectMemGB * 1024,
		DiskGB:            float64(demoExpectDiskGB),
		ExtraConfig:       map[string]string{},
		HostName:          "demo-esxi-01",
		HostPowerPolicy:   "High Performance",
		Networks:          []string{"prod-portgroup-A"},
	}
	locked := true
	vm.MemoryReservationLockedToMax = &locked
	vm.CPUSharesLevel = "custom"
	vm.CPUShares = demoExpectShares
	vm.MemorySharesLevel = "custom"
	vm.MemoryShares = demoExpectShares

	vm.ExtraConfig["sched.mem.lpage.enable1GPage"] = "TRUE"
	vm.ExtraConfig["sched.mem.prealloc"] = "TRUE"
	vm.ExtraConfig["sched.mem.prealloc.pinnedMainMem"] = "TRUE"
	vm.ExtraConfig["sched.swap.vmxSwapEnabled"] = "FALSE"
	vm.ExtraConfig["cpuid.coresPerSocket"] = strconv.Itoa(demoExpectCores)
	vm.ExtraConfig["numa.vcpu.maxPerVirtualNode"] = strconv.Itoa(demoExpectNuma)
	numaOK := int32(demoExpectNuma)
	vm.NumaCoresPerNode = &numaOK // config.numaInfo.coresPerNumaNode도 정답으로 맞춰둠

	for i := 0; i < demoExpectCPU; i++ {
		vm.ExtraConfig[keyFor(i)] = strconv.Itoa(2*i) + "," + strconv.Itoa(2*i+1) // HT ON 페어, 전부 정답
	}
	return vm
}

func buildFailVM() model.VMInfo {
	vm := model.VMInfo{
		Name:              "demo-ev01-fail",
		Hostname:          "demo-ev01-fail",
		HostnameSource:    "guest.hostName",
		NumCPU:            demoExpectCPU,
		NumCoresPerSocket: demoExpectCores, // UI 값은 맞음
		MemoryMB:          demoExpectMemGB * 1024,
		DiskGB:            150, // 기대값(200)과 다르게 일부러 FAIL
		ExtraConfig:       map[string]string{},
		HostName:          "demo-esxi-02",
		HostPowerPolicy:   "Balanced", // 기대값(High Performance)과 다르게 일부러 FAIL
		Networks:          []string{"prod-portgroup-B"},
	}
	locked := false // Reserve all guest memory 꺼짐 -> FAIL
	vm.MemoryReservationLockedToMax = &locked
	vm.CPUSharesLevel = "custom"
	vm.CPUShares = 1000 // 기대값(2000)과 다르게 FAIL
	vm.MemorySharesLevel = "custom"
	vm.MemoryShares = demoExpectShares // 이건 맞음(OK) — CPU/메모리가 서로 다르게 틀릴 수 있다는 것도 보여줌

	vm.ExtraConfig["sched.mem.lpage.enable1GPage"] = "TRUE"
	vm.ExtraConfig["sched.mem.prealloc"] = "TRUE"
	vm.ExtraConfig["sched.mem.prealloc.pinnedMainMem"] = "TRUE"
	vm.ExtraConfig["sched.swap.vmxSwapEnabled"] = "TRUE" // 기대값(FALSE)과 다르게 FAIL
	vm.ExtraConfig["cpuid.coresPerSocket"] = "2"          // Advanced Config만 틀리고 UI값(NumCoresPerSocket)은 맞는 케이스
	vm.ExtraConfig["numa.vcpu.maxPerVirtualNode"] = strconv.Itoa(demoExpectNuma)
	numaWrong := int32(2) // config.numaInfo.coresPerNumaNode는 틀림(기대값 4) — Advanced Config만 틀린 코어수와 같은 패턴을 NUMA에도 재현
	vm.NumaCoresPerNode = &numaWrong

	// affinity: 0,1번은 정답 / 2번은 값이 틀림 / 3번은 아예 없음(설정없음) / 4~7번은 정답
	vm.ExtraConfig[keyFor(0)] = "0,1"
	vm.ExtraConfig[keyFor(1)] = "2,3"
	vm.ExtraConfig[keyFor(2)] = "5,6" // 정답은 4,5
	// keyFor(3) 없음 -> 설정없음
	for i := 4; i < demoExpectCPU; i++ {
		vm.ExtraConfig[keyFor(i)] = strconv.Itoa(2*i) + "," + strconv.Itoa(2*i+1)
	}
	return vm
}

func buildEV02CountGapVM() model.VMInfo {
	vm := model.VMInfo{
		Name:              "demo-ev02-countgap",
		Hostname:          "demo-ev02-countgap",
		HostnameSource:    "guest.hostName",
		NumCPU:            16,
		NumCoresPerSocket: demoExpectCores,
		MemoryMB:          demoExpectMemGB * 1024,
		DiskGB:            float64(demoExpectDiskGB),
		ExtraConfig:       map[string]string{},
		HostName:          "demo-esxi-01",
		HostPowerPolicy:   "High Performance",
		Networks:          []string{"prod-portgroup-A"},
	}
	locked := true
	vm.MemoryReservationLockedToMax = &locked
	vm.CPUSharesLevel = "custom"
	vm.CPUShares = demoExpectShares
	vm.MemorySharesLevel = "custom"
	vm.MemoryShares = demoExpectShares

	vm.ExtraConfig["sched.mem.lpage.enable1GPage"] = "TRUE"
	vm.ExtraConfig["sched.mem.prealloc"] = "TRUE"
	vm.ExtraConfig["sched.mem.prealloc.pinnedMainMem"] = "TRUE"
	vm.ExtraConfig["sched.swap.vmxSwapEnabled"] = "FALSE"
	vm.ExtraConfig["cpuid.coresPerSocket"] = strconv.Itoa(demoExpectCores)
	vm.ExtraConfig["numa.vcpu.maxPerVirtualNode"] = strconv.Itoa(demoExpectNuma)
	numaOK2 := int32(demoExpectNuma)
	vm.NumaCoresPerNode = &numaOK2

	// ev02 기대 파일(demoAffinityEV02)은 vcpu0~15 전부 "단일 코어" 패턴을 요구하는데,
	// 실제로는 vcpu0~12(13개)까지만 정확히 설정돼 있고 13~15는 통째로 빠져 있는 상황을 재현.
	for i := 0; i < 13; i++ {
		vm.ExtraConfig[keyFor(i)] = strconv.Itoa(i)
	}
	return vm
}
