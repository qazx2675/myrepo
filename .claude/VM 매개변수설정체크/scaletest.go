// scaletest.go: -scale=N 모드 전용. vCenter 연결 없이 N대짜리 합성 VM 인벤토리를
// 만들어 콘솔/CSV 출력이 실제 운영 규모(수백~수천 대)에서 어떻게 보이는지 확인하기
// 위한 용도다. -demo(고정 3대, 정확성 데모용)와는 목적이 달라 별도 파일/플래그로 분리했다.
//
// 그룹 비율은 ev01 55% / ev02 20% / ev03 10% / 미분류(기타 BM) 15%로 나누고,
// 각 그룹의 15%는 일부러 FAIL/설정없음이 섞이도록 만들어 실제 인프라에서 흔히
// 보일 법한 "대부분 정상, 일부 문제" 분포를 재현한다. vCenter 연결만 우회할 뿐
// checker/report 파이프라인은 프로덕션과 완전히 동일하게 탄다.
package main

import (
	"fmt"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"vm-param-check/checker"
	"vm-param-check/model"
	"vm-param-check/report"
)

const (
	scaleExpectHT     = true
	scaleExpectCores  = 4
	scaleExpectNuma   = 4
	scaleExpectCPU    = 8
	scaleExpectMemGB  = 32
	scaleExpectDiskGB = 200
	scaleExpectShares = 2000
)

func runScale(n int, out string, onlyFail, noColor bool) {
	fmt.Printf("=== -scale=%d 모드: 실제 vCenter에 연결하지 않고, 합성 VM %d대로 콘솔/CSV 출력 규모를 시뮬레이션합니다 ===\n", n, n)
	fmt.Printf("고정 기대값: ht=on cores=%d numa=%d cpu=%d mem=%dGB disk=%dGB shares-ev01/ev02/ev03=%d (그룹 비율 ev01 55%%/ev02 20%%/ev03 10%%/미분류 15%%, 각 그룹 15%%는 의도적 FAIL 포함)\n\n",
		scaleExpectCores, scaleExpectNuma, scaleExpectCPU, scaleExpectMemGB, scaleExpectDiskGB, scaleExpectShares)

	start := time.Now()
	vms := buildSyntheticVMsN(n)
	shares := checker.SharesExpect{EV01: scaleExpectShares, EV02: intPtr(scaleExpectShares), EV03: intPtr(scaleExpectShares)}
	affinityEV02 := singleCoreAffinityExpect(scaleExpectCPU)
	affinityEV03 := singleCoreAffinityExpect(scaleExpectCPU)
	singleVMMode := len(vms) == 1

	// 실제 운영 경로(main.go)와 동일하게 VM별 체크를 워커풀로 병렬 처리한다 — N이 커질수록
	// (수천대) 이 병렬화의 체감 효과가 커진다.
	findingsPerVM := make([][]model.Finding, len(vms))
	sem := make(chan struct{}, runtime.NumCPU())
	var wg sync.WaitGroup
	for i, vm := range vms {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int, vm model.VMInfo) {
			defer wg.Done()
			defer func() { <-sem }()

			group := classifyGroup(vm.Hostname)
			var f []model.Finding
			f = append(f, checker.CheckFixed(vm)...)
			f = append(f, checker.CheckTopology(vm, checker.CoresExpect{Base: scaleExpectCores}, checker.NumaExpect{Base: scaleExpectNuma}, group, singleVMMode)...)
			f = append(f, checker.CheckHardware(vm, checker.CPUExpect{Base: scaleExpectCPU}, checker.MemExpect{Base: scaleExpectMemGB}, checker.DiskExpect{Base: scaleExpectDiskGB}, shares, group, singleVMMode)...)
			f = append(f, checker.CheckHostPower(vm))
			f = append(f, checker.CheckNetwork(vm)...)

			switch group {
			case "ev01":
				expected := checker.GenerateExpectedAffinityEV01(vm.NumCPU, scaleExpectHT)
				f = append(f, checker.CheckAffinity(vm, expected, "ev01")...)
			case "ev02":
				f = append(f, checker.CheckAffinity(vm, affinityEV02, "ev02")...)
			case "ev03":
				f = append(f, checker.CheckAffinity(vm, affinityEV03, "ev03")...)
			}
			findingsPerVM[i] = f
		}(i, vm)
	}
	wg.Wait()

	var findings []model.Finding
	for _, f := range findingsPerVM {
		findings = append(findings, f...)
	}
	buildElapsed := time.Since(start)

	if onlyFail {
		before := len(report.Summarize(findings))
		findings = report.FilterOnlyFail(findings)
		after := len(report.Summarize(findings))
		fmt.Printf("-onlyFail: PASS인 VM %d대는 결과에서 제외 (총 %d대 중 %d대만 출력)\n", before-after, before, after)
	}

	fmt.Println()
	consoleStart := time.Now()
	report.PrintConsole(os.Stdout, findings, !noColor, !onlyFail)
	consoleElapsed := time.Since(consoleStart)

	base := out
	if base == "" {
		base = fmt.Sprintf("vm-param-check_scale%d_%s.csv", n, time.Now().Format("20060102_150405"))
	}
	summaryPath := strings.TrimSuffix(base, ".csv") + "_summary.csv"

	csvStart := time.Now()
	if err := report.WriteSummaryCSV(summaryPath, findings); err != nil {
		fmt.Fprintf(os.Stderr, "요약 CSV 저장 실패: %v\n", err)
		return
	}
	if err := report.WriteCSV(base, findings); err != nil {
		fmt.Fprintf(os.Stderr, "상세 CSV 저장 실패: %v\n", err)
		return
	}
	csvElapsed := time.Since(csvStart)

	fmt.Printf("\nCSV 저장 완료: 요약=%s, 상세=%s (%d개 항목)\n", summaryPath, base, len(findings))
	fmt.Fprintf(os.Stderr, "[소요시간] VM생성+체크=%v 콘솔출력=%v CSV저장=%v\n", buildElapsed, consoleElapsed, csvElapsed)
}

func singleCoreAffinityExpect(numCPU int) map[string]string {
	expected := map[string]string{}
	for i := 0; i < numCPU; i++ {
		expected[keyFor(i)] = strconv.Itoa(i)
	}
	return expected
}

// buildSyntheticVMsN은 -scale=N개의 합성 VM을 만든다. 인덱스 기준 결정적 패턴이라
// (time.Now/math.rand 미사용) 같은 N에 대해 항상 같은 결과가 재현된다.
func buildSyntheticVMsN(n int) []model.VMInfo {
	vms := make([]model.VMInfo, 0, n)
	for i := 0; i < n; i++ {
		group, label := groupForIndex(i)
		fail := i%7 == 0 // 그룹별 대략 15% 정도가 FAIL 섞인 케이스
		name := fmt.Sprintf("%s-%04d", label, i)
		if fail {
			vms = append(vms, buildScaleFailVM(name, group, i))
		} else {
			vms = append(vms, buildScaleOKVM(name, group))
		}
	}
	return vms
}

// groupForIndex: ev01 55% / ev02 20% / ev03 10% / 미분류 15%
func groupForIndex(i int) (group, label string) {
	switch m := i % 20; {
	case m < 11: // 0..10 -> 11/20 = 55%
		return "ev01", "ev01"
	case m < 15: // 11..14 -> 4/20 = 20%
		return "ev02", "ev02"
	case m < 17: // 15..16 -> 2/20 = 10%
		return "ev03", "ev03"
	default: // 17..19 -> 3/20 = 15%
		return "", "bm"
	}
}

func buildScaleOKVM(name, group string) model.VMInfo {
	// --cpu 기대값은 그룹과 무관하게 전체 VM에 동일하게 적용되므로(계획서 3-4),
	// 그룹별로 vCPU 수를 다르게 하면 "정상" 케이스인데도 항상 FAIL이 나는 모순이 생긴다.
	numCPU := scaleExpectCPU

	vm := model.VMInfo{
		Name:              name,
		Hostname:          name,
		HostnameSource:    "guest.hostName",
		NumCPU:            int32(numCPU),
		NumCoresPerSocket: scaleExpectCores,
		MemoryMB:          scaleExpectMemGB * 1024,
		DiskGB:            float64(scaleExpectDiskGB),
		ExtraConfig:       map[string]string{},
		HostName:          fmt.Sprintf("esxi-%02d", (len(name)*7)%8+1),
		HostPowerPolicy:   "High Performance",
		Networks:          []model.NetworkAdapter{{Portgroup: "prod-portgroup-A", Connected: true}},
	}
	locked := true
	vm.MemoryReservationLockedToMax = &locked
	vm.CPUSharesLevel = "custom"
	vm.CPUShares = int32(scaleExpectShares)
	vm.MemorySharesLevel = "custom"
	vm.MemoryShares = int32(scaleExpectShares)

	vm.ExtraConfig["sched.mem.lpage.enable1GPage"] = "TRUE"
	vm.ExtraConfig["sched.mem.prealloc"] = "TRUE"
	vm.ExtraConfig["sched.mem.prealloc.pinnedMainMem"] = "TRUE"
	vm.ExtraConfig["sched.swap.vmxSwapEnabled"] = "FALSE"
	vm.ExtraConfig["cpuid.coresPerSocket"] = strconv.Itoa(scaleExpectCores)
	vm.ExtraConfig["numa.vcpu.maxPerVirtualNode"] = strconv.Itoa(scaleExpectNuma)
	numaOK := int32(scaleExpectNuma)
	vm.NumaCoresPerNode = &numaOK

	switch group {
	case "ev01":
		for i := 0; i < numCPU; i++ {
			vm.ExtraConfig[keyFor(i)] = strconv.Itoa(2*i) + "," + strconv.Itoa(2*i+1)
		}
	case "ev02", "ev03":
		for i := 0; i < numCPU; i++ {
			vm.ExtraConfig[keyFor(i)] = strconv.Itoa(i)
		}
	}
	return vm
}

func buildScaleFailVM(name, group string, seed int) model.VMInfo {
	vm := buildScaleOKVM(name, group)

	// seed로 FAIL 패턴을 몇 가지로 순환시켜 "전부 똑같은 이유로 FAIL"처럼 보이지 않게 한다.
	switch seed % 4 {
	case 0:
		vm.DiskGB = float64(scaleExpectDiskGB) - 50 // 디스크 용량 FAIL
	case 1:
		vm.HostPowerPolicy = "Balanced" // 호스트 전원정책 FAIL
	case 2:
		vm.ExtraConfig["sched.swap.vmxSwapEnabled"] = "TRUE" // 스케줄러 고정값 FAIL
	case 3:
		vm.CPUShares = int32(scaleExpectShares) / 2 // Shares FAIL
	}

	// affinity 한 개는 값 불일치, 한 개는 아예 설정없음으로 만든다 (그룹이 있을 때만 의미 있음).
	if group != "" {
		delete(vm.ExtraConfig, keyFor(1))
		if group == "ev01" {
			vm.ExtraConfig[keyFor(2)] = "99,100" // 명백히 틀린 값
		} else {
			vm.ExtraConfig[keyFor(2)] = "99"
		}
	}
	return vm
}
