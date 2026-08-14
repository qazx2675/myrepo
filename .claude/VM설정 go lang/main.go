// vm_affinity_bulk: worklist 기반으로 ev01~ev03 VM에 affinity 설정 파일을 일괄(비동기 Task) 적용. -vm_cnt 로 대상 VM 개수 제어

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
	"github.com/vmware/govmomi/property"
	"github.com/vmware/govmomi/vim25/mo"
	"github.com/vmware/govmomi/vim25/types"
)

const maxVMCount = 3

// optionPair: 파일 순서를 보존하기 위해 map 대신 slice 사용 (로그 재현성 확보)
type optionPair struct {
	Key   string
	Value string
}

// affinitySpec: affinity 설정 파일 1개를 파싱한 결과
type affinitySpec struct {
	fileName string
	auto     bool // AUTO 모드: vCPU 수 기준 1:1 매핑을 코드가 생성 (-ht 반영)
	pairs    []optionPair
}

// vmTask: 전송한 Reconfigure Task 와 대상 VM 이름 (실패 집계 및 적용여부 검증용)
type vmTask struct {
	vmName   string
	task     *object.Task
	expected []optionPair
}

func readLines(path string) ([]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var lines []string
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(strings.TrimPrefix(scanner.Text(), string([]byte{0xEF, 0xBB, 0xBF})))
		if line != "" && !strings.HasPrefix(line, "#") {
			lines = append(lines, line)
		}
	}
	return lines, scanner.Err()
}

// stripQuotes: 양끝이 짝이 맞는 큰따옴표/작은따옴표로 감싸져 있으면 제거
func stripQuotes(s string) string {
	if len(s) >= 2 {
		first, last := s[0], s[len(s)-1]
		if (first == '"' && last == '"') || (first == '\'' && last == '\'') {
			return s[1 : len(s)-1]
		}
	}
	return s
}

// loadAffinitySpec: key=value 형식 파싱. 파일 내용이 "AUTO" 한 줄이면 1:1 자동 생성 모드
func loadAffinitySpec(path string) (*affinitySpec, error) {
	lines, err := readLines(path)
	if err != nil {
		return nil, err
	}
	if len(lines) == 0 {
		return nil, fmt.Errorf("유효한 설정값이 없습니다 (빈 파일 또는 주석만 존재)")
	}

	spec := &affinitySpec{fileName: filepath.Base(path)}

	if len(lines) == 1 && strings.EqualFold(lines[0], "AUTO") {
		spec.auto = true
		return spec, nil
	}

	for i, line := range lines {
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("%d번째 줄 형식 오류 (key=value 아님): %s", i+1, line)
		}
		key := stripQuotes(strings.TrimSpace(parts[0]))
		val := stripQuotes(strings.TrimSpace(parts[1]))
		if key == "" {
			return nil, fmt.Errorf("%d번째 줄 key 가 비어 있습니다: %s", i+1, line)
		}
		spec.pairs = append(spec.pairs, optionPair{Key: key, Value: val})
	}

	if len(spec.pairs) == 0 {
		return nil, fmt.Errorf("파싱된 설정값이 없습니다")
	}
	return spec, nil
}

// buildExtraConfig: spec 을 vCenter ExtraConfig 로 변환. AUTO 모드는 numCPU 기준 1:1 매핑 생성
func buildExtraConfig(spec *affinitySpec, numCPU int, htOn bool) ([]types.BaseOptionValue, error) {
	var extraConfig []types.BaseOptionValue

	if spec.auto {
		if numCPU <= 0 {
			return nil, fmt.Errorf("AUTO 모드인데 vCPU 수를 확인할 수 없습니다")
		}
		for i := 0; i < numCPU; i++ {
			key := fmt.Sprintf("sched.vcpu%d.affinity", i)
			var val string
			if htOn {
				val = fmt.Sprintf("%d,%d", i*2, i*2+1)
			} else {
				val = fmt.Sprintf("%d", i)
			}
			extraConfig = append(extraConfig, &types.OptionValue{Key: key, Value: val})
		}
		return extraConfig, nil
	}

	for _, p := range spec.pairs {
		extraConfig = append(extraConfig, &types.OptionValue{Key: p.Key, Value: p.Value})
	}
	return extraConfig, nil
}

// resolvePath: 절대경로면 그대로, 상대경로면 baseDir 기준
func resolvePath(baseDir, p string) string {
	if filepath.IsAbs(p) {
		return p
	}
	return filepath.Join(baseDir, p)
}

func main() {
	vcId := flag.String("id", "lscsystems@vsphere.local", "vCenter 로그인 계정 ID")
	vcTargetIP := flag.String("vcTargetIP", "", "vCenter 접속 IP (필수)")
	worklistFile := flag.String("worklistFile", "worklist.txt", "작업 대상 호스트 목록 파일")
	vmCnt := flag.Int("vm_cnt", 2, "호스트당 대상 VM 개수 (1=ev01, 2=ev01~ev02, 3=ev01~ev03)")
	affinityFile01 := flag.String("affinityFile01", "", "ev01 affinity 설정 파일 (vm_cnt>=1 이면 필수)")
	affinityFile02 := flag.String("affinityFile02", "", "ev02 affinity 설정 파일 (vm_cnt>=2 이면 필수)")
	affinityFile03 := flag.String("affinityFile03", "", "ev03 affinity 설정 파일 (vm_cnt>=3 이면 필수)")
	affinityLegacy := flag.String("affinityFile", "", "[구버전 호환] -affinityFile02 미지정 시 ev02 설정 파일로 사용")
	htMode := flag.String("ht", "ON", "Hyper-Threading 모드 (설정 파일 내용이 AUTO 인 경우에만 사용 / ON: 0=0,1 / OFF: 0=0)")

	flag.Parse()

	if *vcTargetIP == "" {
		log.Fatal("필수 파라미터가 누락되었습니다. (-vcTargetIP 확인)")
	}

	if *vmCnt < 1 || *vmCnt > maxVMCount {
		log.Fatalf("-vm_cnt 값이 올바르지 않습니다: %d (1~%d 만 허용)", *vmCnt, maxVMCount)
	}

	htOn := true
	switch strings.ToUpper(strings.TrimSpace(*htMode)) {
	case "ON":
		htOn = true
	case "OFF":
		htOn = false
	default:
		log.Fatalf("-ht 값이 올바르지 않습니다: %s (ON 또는 OFF만 허용)", *htMode)
	}

	vcPassword := os.Getenv("VC_PASSWORD")
	if vcPassword == "" {
		log.Fatal("인증 정보 로드 실패: VC_PASSWORD 환경 변수가 설정되지 않았습니다.")
	}

	baseDir, err := os.Getwd()
	if err != nil {
		baseDir = "."
	}

	// 구버전 -affinityFile 은 ev02 슬롯으로 흡수
	if *affinityFile02 == "" && *affinityLegacy != "" {
		*affinityFile02 = *affinityLegacy
	}

	// ---- vm_cnt 기준 필수 파라미터 검증 ----
	fileFlags := []string{*affinityFile01, *affinityFile02, *affinityFile03}
	flagNames := []string{"-affinityFile01", "-affinityFile02", "-affinityFile03"}
	suffixes := []string{"ev01", "ev02", "ev03"}

	specs := make([]*affinitySpec, *vmCnt)
	for i := 0; i < *vmCnt; i++ {
		if strings.TrimSpace(fileFlags[i]) == "" {
			log.Fatalf("-vm_cnt=%d 이므로 %s (%s) 파라미터는 필수입니다.", *vmCnt, flagNames[i], suffixes[i])
		}

		path := resolvePath(baseDir, fileFlags[i])
		if _, statErr := os.Stat(path); os.IsNotExist(statErr) {
			log.Fatalf("%s 파일을 찾을 수 없습니다: %s", suffixes[i], path)
		}

		spec, loadErr := loadAffinitySpec(path)
		if loadErr != nil {
			log.Fatalf("%s 설정 파일 오류 (%s): %v", suffixes[i], path, loadErr)
		}
		specs[i] = spec
	}

	// vm_cnt 범위를 넘어선 파라미터가 들어오면 무시됨을 알림
	for i := *vmCnt; i < maxVMCount; i++ {
		if strings.TrimSpace(fileFlags[i]) != "" {
			fmt.Printf("알림: -vm_cnt=%d 이므로 %s (%s) 설정은 무시됩니다.\n", *vmCnt, flagNames[i], suffixes[i])
		}
	}

	// ---- worklist 로드 ----
	worklistPath := resolvePath(baseDir, *worklistFile)
	if _, statErr := os.Stat(worklistPath); os.IsNotExist(statErr) {
		log.Fatalf("%s 파일을 찾을 수 없습니다: %s", *worklistFile, worklistPath)
	}

	hostlistLines, err := readLines(worklistPath)
	if err != nil {
		log.Fatalf("worklist 파일 읽기 실패: %v", err)
	}
	if len(hostlistLines) == 0 {
		log.Fatalf("%s 에 작업 대상 호스트가 없습니다.", *worklistFile)
	}

	htLabel := "ON"
	if !htOn {
		htLabel = "OFF"
	}

	needCPUInfo := false
	for _, s := range specs {
		if s != nil && s.auto {
			needCPUInfo = true
		}
	}

	fmt.Printf("비동기(Task) 방식 어피니티 일괄 할당을 시작합니다.\n")
	fmt.Printf("  접속 계정 : %s\n", *vcId)
	fmt.Printf("  vCenter   : %s\n", *vcTargetIP)
	fmt.Printf("  대상 호스트: %d대 / VM 개수: %d (%s)\n",
		len(hostlistLines), *vmCnt, strings.Join(suffixes[:*vmCnt], ", "))
	for i := 0; i < *vmCnt; i++ {
		mode := fmt.Sprintf("%d개 항목", len(specs[i].pairs))
		if specs[i].auto {
			mode = fmt.Sprintf("AUTO 1:1 (HT=%s)", htLabel)
		}
		fmt.Printf("  %s 설정  : %s [%s]\n", suffixes[i], specs[i].fileName, mode)
	}
	fmt.Println()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	u := &url.URL{Scheme: "https", Host: *vcTargetIP, Path: "/sdk"}
	u.User = url.UserPassword(*vcId, vcPassword)

	client, err := govmomi.NewClient(ctx, u, true)
	if err != nil {
		log.Fatalf("vCenter 접속 실패: %v", err)
	}
	defer func() {
		_ = client.Logout(context.Background())
	}()

	// ---- 대상 VM 이름 집합 생성 ----
	var safeVmNames []string
	for _, baseHost := range hostlistLines {
		cleanHost := strings.TrimSpace(baseHost)
		if cleanHost == "" {
			continue
		}
		for i := 0; i < *vmCnt; i++ {
			safeVmNames = append(safeVmNames, regexp.QuoteMeta(cleanHost+suffixes[i]))
		}
	}
	if len(safeVmNames) == 0 {
		log.Fatal("생성된 대상 VM 이름이 없습니다. worklist 내용을 확인하세요.")
	}

	regexMatcher := regexp.MustCompile("^(" + strings.Join(safeVmNames, "|") + ")$")

	// ---- 전체 데이터센터 순회하며 VM 목록 수집 ----
	finder := find.NewFinder(client.Client, true)
	dcList, err := finder.DatacenterList(ctx, "*")
	if err != nil || len(dcList) == 0 {
		log.Fatalf("데이터센터 조회 실패: %v", err)
	}

	targetVmMap := make(map[string]*object.VirtualMachine)
	for _, dc := range dcList {
		finder.SetDatacenter(dc)
		vms, listErr := finder.VirtualMachineList(ctx, "*")
		if listErr != nil {
			// 해당 DC 에 VM 이 없는 경우도 에러로 반환되므로 치명적으로 다루지 않음
			continue
		}
		for _, vm := range vms {
			if regexMatcher.MatchString(vm.Name()) {
				targetVmMap[vm.Name()] = vm
			}
		}
	}

	if len(targetVmMap) == 0 {
		log.Fatal("worklist 와 매칭되는 VM 을 vCenter 에서 찾지 못했습니다.")
	}

	// ---- AUTO 모드가 있을 때만 numCPU 배치 조회 (property.Collector) ----
	cpuMap := make(map[string]int, len(targetVmMap))
	if needCPUInfo {
		refs := make([]types.ManagedObjectReference, 0, len(targetVmMap))
		for _, vm := range targetVmMap {
			refs = append(refs, vm.Reference())
		}

		pc := property.DefaultCollector(client.Client)
		var vmProps []mo.VirtualMachine
		if retErr := pc.Retrieve(ctx, refs, []string{"name", "config.hardware.numCPU"}, &vmProps); retErr != nil {
			log.Fatalf("vCPU 정보 배치 조회 실패: %v", retErr)
		}
		for i := range vmProps {
			if vmProps[i].Config != nil {
				cpuMap[vmProps[i].Name] = int(vmProps[i].Config.Hardware.NumCPU)
			}
		}
	}

	// ---- Reconfigure Task 전송 ----
	var allTasks []vmTask
	var skipped int

	for _, baseHost := range hostlistLines {
		cleanHost := strings.TrimSpace(baseHost)
		if cleanHost == "" {
			continue
		}

		for i := 0; i < *vmCnt; i++ {
			vmName := cleanHost + suffixes[i]
			vm, ok := targetVmMap[vmName]
			if !ok {
				fmt.Printf("[%s] 경고: 대상 VM 이 존재하지 않습니다. (PASS)\n", vmName)
				skipped++
				continue
			}

			extraConfig, buildErr := buildExtraConfig(specs[i], cpuMap[vmName], htOn)
			if buildErr != nil {
				fmt.Printf("[%s] 설정 생성 실패: %v (PASS)\n", vmName, buildErr)
				skipped++
				continue
			}

			// 재조회 검증용으로 우리가 보낸 key=value 를 그대로 보관
			expectedPairs := make([]optionPair, 0, len(extraConfig))
			for _, ov := range extraConfig {
				if opt, ok := ov.(*types.OptionValue); ok {
					expectedPairs = append(expectedPairs, optionPair{Key: opt.Key, Value: fmt.Sprintf("%v", opt.Value)})
				}
			}

			spec := types.VirtualMachineConfigSpec{ExtraConfig: extraConfig}

			task, taskErr := vm.Reconfigure(ctx, spec)
			if taskErr != nil {
				fmt.Printf("[%s] Reconfigure 명령 전송 실패: %v\n", vmName, taskErr)
				skipped++
				continue
			}

			allTasks = append(allTasks, vmTask{vmName: vmName, task: task, expected: expectedPairs})

			if specs[i].auto {
				fmt.Printf("[%s] AUTO 1:1 코어 비동기 설정 명령 전송 완료 (HT=%s, vCPU=%d)\n",
					vmName, htLabel, cpuMap[vmName])
			} else {
				fmt.Printf("[%s] %s 기반 비동기 설정 명령 전송 완료 (%d개 항목)\n",
					vmName, specs[i].fileName, len(specs[i].pairs))
			}
		}
	}

	// ---- Task 완료 대기 및 결과 집계 ----
	if len(allTasks) == 0 {
		fmt.Println("\n실행할 비동기 작업이 없습니다.")
		if skipped > 0 {
			os.Exit(2)
		}
		return
	}

	fmt.Printf("\n총 %d개의 설정 작업이 vCenter 백그라운드에서 동시 처리 중입니다.\n", len(allTasks))
	fmt.Println("모든 작업이 완료될 때까지 안전하게 대기합니다...")

	var failed int
	successVmNames := make([]string, 0, len(allTasks))
	for _, t := range allTasks {
		if waitErr := t.task.Wait(ctx); waitErr != nil {
			fmt.Printf("[%s] 작업 실패: %v\n", t.vmName, waitErr)
			failed++
			continue
		}
		successVmNames = append(successVmNames, t.vmName)
	}

	// ---- 실제 적용여부 배치 검증 (config.extraConfig 재조회, Task 성공분만 대상) ----
	var mismatched int
	if len(successVmNames) > 0 {
		nameSet := make(map[string]bool, len(successVmNames))
		verifyRefs := make([]types.ManagedObjectReference, 0, len(successVmNames))
		for _, name := range successVmNames {
			nameSet[name] = true
			if vm, ok := targetVmMap[name]; ok {
				verifyRefs = append(verifyRefs, vm.Reference())
			}
		}

		pc := property.DefaultCollector(client.Client)
		var verifyProps []mo.VirtualMachine
		if retErr := pc.Retrieve(ctx, verifyRefs, []string{"name", "config.extraConfig"}, &verifyProps); retErr != nil {
			fmt.Printf("경고: 실제 적용여부 재조회 실패 (%v) - 적용 확인을 건너뜁니다.\n", retErr)
		} else {
			actualMap := make(map[string]map[string]string, len(verifyProps))
			for _, vp := range verifyProps {
				if vp.Config == nil {
					continue
				}
				m := make(map[string]string, len(vp.Config.ExtraConfig))
				for _, ov := range vp.Config.ExtraConfig {
					if opt, ok := ov.(*types.OptionValue); ok {
						m[opt.Key] = fmt.Sprintf("%v", opt.Value)
					}
				}
				actualMap[vp.Name] = m
			}

			for _, t := range allTasks {
				if !nameSet[t.vmName] {
					continue
				}
				actual, ok := actualMap[t.vmName]
				if !ok {
					fmt.Printf("[%s] 실제 적용 확인 실패: 재조회 결과 없음\n", t.vmName)
					mismatched++
					continue
				}
				var bad []string
				for _, exp := range t.expected {
					if actual[exp.Key] != exp.Value {
						bad = append(bad, fmt.Sprintf("%s(기대=%s,실제=%s)", exp.Key, exp.Value, actual[exp.Key]))
					}
				}
				if len(bad) > 0 {
					fmt.Printf("[%s] 실제 적용 불일치: %s\n", t.vmName, strings.Join(bad, ", "))
					mismatched++
				} else {
					fmt.Printf("[%s] 실제 적용 확인 완료 (%d개 항목 일치)\n", t.vmName, len(t.expected))
				}
			}
		}
	}

	fmt.Printf("\n완료: 성공 %d / 실패 %d / 스킵 %d / 적용불일치 %d\n",
		len(allTasks)-failed-mismatched, failed, skipped, mismatched)
	if failed > 0 || skipped > 0 || mismatched > 0 {
		os.Exit(2)
	}
	fmt.Println("모든 VM 의 어피니티 설정이 정상 적용되었습니다 (재조회로 검증 완료).")
}
