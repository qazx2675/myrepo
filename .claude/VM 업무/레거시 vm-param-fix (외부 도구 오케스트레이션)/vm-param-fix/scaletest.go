// scaletest.go: -scale=N 모드 전용. 실제 vCenter/CSV 없이 N대 규모의 합성 데이터로
// 태그분류 → 동질성/전원 게이트 → dry-run → 확인 → 병렬 실행까지 전체 파이프라인이
// 대량 환경에서 어떻게 보이고 얼마나 걸리는지 확인하기 위한 용도다. (vm-param-check의
// -scale 모드와 같은 목적 — 실제 인프라를 건드리지 않는 규모 테스트 전용)
//
// vCenter에 붙지 않으므로 재검증(vm-param-check 재실행) 단계는 생략한다.
package main

import (
	"fmt"
	"os"
	"sync"
	"time"
)

const (
	scaleCPU     = 2
	scaleCores   = 1
	scaleNuma    = 1
	scaleMemGB   = 4
	scaleDiskGB  = 40
	scaleShares  = 1000
)

func runScale(n int, mismatch bool, affinityTool, lpageTool, powerTool string) {
	fmt.Printf("=== -scale=%d 모드: 실제 vCenter/CSV 없이 합성 데이터 %d대로 vm-param-fix 파이프라인을 시뮬레이션합니다 ===\n", n, n)
	if mismatch {
		fmt.Println("(-scaleMismatch: VM 1대의 스펙을 의도적으로 다르게 만들어 동질성 게이트 검증)")
	}

	start := time.Now()
	rows, specs := buildSyntheticScale(n, mismatch)
	buildElapsed := time.Since(start)

	g, err := extractGlobalExpect(rows)
	if err != nil {
		fmt.Println("전역 기대값 복원 실패:", err)
		return
	}

	affinityVMs := map[string]bool{}
	lpageVMs := map[string]bool{}
	powerHosts := map[string]bool{}
	manualCount := 0
	for _, r := range rows {
		if r.Result != "FAIL" && r.Result != "설정없음" {
			continue
		}
		switch classifyTag(r) {
		case TagAffinity:
			affinityVMs[r.VM] = true
		case TagLpage:
			lpageVMs[r.VM] = true
		case TagPower:
			if h, ok := hostFromNote(r.Note); ok {
				powerHosts[h] = true
			}
		default:
			manualCount++
		}
	}

	targetVMs := map[string]bool{}
	for vm := range affinityVMs {
		targetVMs[vm] = true
	}
	for vm := range lpageVMs {
		targetVMs[vm] = true
	}
	fmt.Printf("[규모] 대상 VM %d대 (affinity %d대, lpage %d대, power대상 호스트 %d개, manual %d건), 합성데이터 생성 %v\n",
		len(targetVMs), len(affinityVMs), len(lpageVMs), len(powerHosts), manualCount, buildElapsed)

	gateStart := time.Now()
	var homogErr, powerErr error
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		homogErr = checkHomogeneity(targetVMs, specs)
	}()
	go func() {
		defer wg.Done()
		powerErr = checkAllPoweredOff(targetVMs, specs)
	}()
	wg.Wait()
	gateElapsed := time.Since(gateStart)
	fmt.Printf("[규모] 동질성+전원 게이트 병렬 검증 소요시간: %v\n", gateElapsed)

	if homogErr != nil {
		fmt.Println("[동질성 검증 실패]", homogErr)
		return
	}
	if powerErr != nil {
		fmt.Println("[전원 OFF 검증 실패]", powerErr)
		return
	}
	fmt.Println("[OK] 그룹 동질성 검증 통과")
	fmt.Println("[OK] 전원 OFF 검증 통과")

	workDir := os.TempDir()
	affinityWorklist := writeWorklist(workDir, "scale_worklist_affinity.txt", baseHostnames(affinityVMs))
	lpageWorklist := writeWorklist(workDir, "scale_worklist_lpage.txt", baseHostnames(lpageVMs))
	powerWorklist := writeWorklist(workDir, "scale_worklist_power.txt", sortedKeys(powerHosts))
	fmt.Printf("[규모] worklist 파일 크기: affinity %d줄, lpage %d줄, power %d줄\n",
		len(baseHostnames(affinityVMs)), len(baseHostnames(lpageVMs)), len(powerHosts))

	type plannedCmd struct {
		tag  string
		name string
		args []string
	}
	var planned []plannedCmd
	if len(affinityVMs) > 0 {
		ht := "OFF"
		if g.HTOn {
			ht = "ON"
		}
		planned = append(planned, plannedCmd{"affinity", affinityTool,
			[]string{"-id", "scale-test", "-vcTargetIP", "127.0.0.1", "-worklistFile", affinityWorklist, "-ht", ht}})
	}
	if len(lpageVMs) > 0 {
		planned = append(planned, plannedCmd{"lpage", lpageTool,
			[]string{"-id", "scale-test", "-vcTargetIP", "127.0.0.1", "-worklistFile", lpageWorklist,
				"-ev01Cores", itoa(g.CPU), "-ev01Sockets", itoa(g.CPU / g.Cores),
				"-ev02Cores", itoa(g.CPU), "-ev02Sockets", itoa(g.CPU / g.Cores)}})
	}
	if len(powerHosts) > 0 {
		planned = append(planned, plannedCmd{"power", powerTool,
			[]string{"-id", "scale-test", "-vcTargetIP", "127.0.0.1", "-worklistFile", powerWorklist}})
	}

	fmt.Println("\n=== 실행 예정 (dry-run, worklist 파일 경로만 표시) ===")
	for _, p := range planned {
		fmt.Printf("[%s] %s (worklist %s)\n", p.tag, p.name, p.args[5])
	}

	fmt.Println("\n=== 태그별 도구 병렬 실행 (mock) ===")
	execStart := time.Now()
	var runWG sync.WaitGroup
	for _, p := range planned {
		runWG.Add(1)
		go func(p plannedCmd) {
			defer runWG.Done()
			if err := runTool(p.tag, p.name, p.args); err != nil {
				fmt.Printf("[%s] 실행 실패: %v\n", p.tag, err)
			}
		}(p)
	}
	runWG.Wait()
	execElapsed := time.Since(execStart)
	fmt.Printf("\n[규모] 태그별 도구 병렬 실행 소요시간: %v\n", execElapsed)

	fmt.Println("\n(스케일 테스트 모드라 실제 vCenter가 없어 재검증 단계는 생략합니다)")
	fmt.Printf("\n=== 총 소요시간: %v ===\n", time.Since(start))
}

func itoa(n int) string {
	return fmt.Sprintf("%d", n)
}

// buildSyntheticScale은 N대(짝수, 홀수면 마지막 1대 버림)를 base host 기준 ev01+ev02
// 쌍으로 만든다. 전부 동일 스펙(cpu=2,cores=1,mem=4,disk=40,shares=1000)에 전원 꺼짐 —
// mismatch=true면 3번째 ev01 하나만 vCPU를 다르게 만들어 동질성 게이트를 테스트한다.
// 모든 VM은 cpuid.coresPerSocket/sched.vcpu0.affinity가 설정없음이라 lpage+affinity 태그가
// 항상 걸리고, 호스트 20개당 1개꼴로 host power policy FAIL도 섞는다.
func buildSyntheticScale(n int, mismatch bool) ([]Row, map[string]VMSpec) {
	hostCount := n / 2
	var rows []Row
	specs := map[string]VMSpec{}

	for i := 0; i < hostCount; i++ {
		base := fmt.Sprintf("scale%04d", i)
		for _, grp := range []string{"ev01", "ev02"} {
			vm := base + grp
			cpu := scaleCPU
			if mismatch && i == 2 && grp == "ev01" {
				cpu = scaleCPU * 2 // 의도적 불일치
			}

			specs[vm] = VMSpec{
				Name:              vm,
				NumCPU:            int32(cpu),
				NumCoresPerSocket: scaleCores,
				MemoryMB:          scaleMemGB * 1024,
				DiskGB:            scaleDiskGB,
				CPUSharesLevel:    "custom",
				CPUShares:         scaleShares,
				PoweredOn:         false,
				Found:             true,
			}

			rows = append(rows,
				Row{VM: vm, Source: "-", Key: "config.hardware.numCPU", Expected: itoa(scaleCPU), Actual: itoa(cpu), Result: resultOf(scaleCPU, cpu)},
				Row{VM: vm, Source: "-", Key: "cpuid.coresPerSocket", Expected: itoa(scaleCores), Result: "설정없음"},
				Row{VM: vm, Source: "-", Key: "numa.vcpu.maxPerVirtualNode", Expected: itoa(scaleNuma), Result: "설정없음"},
				Row{VM: vm, Source: "-", Key: "config.hardware.memoryMB (GB 환산)", Expected: itoa(scaleMemGB), Actual: itoa(scaleMemGB), Result: "OK"},
				Row{VM: vm, Source: "-", Key: "disk total capacity (GB 환산, 반올림)", Expected: itoa(scaleDiskGB), Actual: itoa(scaleDiskGB), Result: "OK"},
				Row{VM: vm, Source: grp, Key: "cpuAllocation.shares (CPU Shares Ratio)", Expected: itoa(scaleShares), Actual: itoa(scaleShares), Result: "OK"},
				Row{VM: vm, Source: grp, Key: "sched.vcpu0.affinity", Expected: "0,1", Result: "설정없음"},
			)
		}

		if i%20 == 0 {
			host := fmt.Sprintf("esxi-%03d", i/20)
			rows = append(rows, Row{VM: base + "ev01", Source: "host", Key: "host power policy",
				Expected: "High Performance", Actual: "Balanced", Result: "FAIL", Note: "host=" + host})
		}
	}
	return rows, specs
}

func resultOf(expected, actual int) string {
	if expected == actual {
		return "OK"
	}
	return "FAIL"
}
