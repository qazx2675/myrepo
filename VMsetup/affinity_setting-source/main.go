// vm_affinity_bulk: worklist 기반으로 ev01~ev99 VM에 affinity 설정 파일을 일괄(병렬 워커풀) 적용. -vm_cnt 로 대상 VM 개수, -concurrency 로 동시 처리 개수 제어

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

	"github.com/vmware/govmomi"
	"github.com/vmware/govmomi/find"
	"github.com/vmware/govmomi/object"
	"github.com/vmware/govmomi/property"
	"github.com/vmware/govmomi/vim25/mo"
	"github.com/vmware/govmomi/vim25/types"
)

const maxVMCount = 99 // ev01~ev99 (VM 이름이 ev%02d 두 자리)
const defaultConcurrency = 20

// optionPair: 파일 순서를 보존하기 위해 map 대신 slice 사용 (로그 재현성 확보)
type optionPair struct {
	Key   string
	Value string
}

// affinitySpec: affinity 설정 파일 1개를 파싱한 결과
type affinitySpec struct {
	fileName string
	pairs    []optionPair
}

// vmJob: 워커풀에 넘기는 작업 단위 (VM 1대 = Reconfigure 전송 + Wait 를 한 워커가 전부 처리)
type vmJob struct {
	vmName string
	vm     *object.VirtualMachine
	specI  *affinitySpec
}

// vmResult: 워커풀 처리 결과 (성공/실패/스킵 집계 및 재조회 검증용)
type vmResult struct {
	vmName   string
	expected []optionPair
	skipped  bool
	failed   bool
	message  string
}

// printMu: 여러 고루틴이 동시에 fmt.Printf 를 호출할 때 줄이 섞이지 않도록 보호
var printMu sync.Mutex

func safePrintf(format string, a ...interface{}) {
	printMu.Lock()
	defer printMu.Unlock()
	fmt.Printf(format, a...)
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

// loadAffinitySpec: key=value 형식 파싱. 같은 파일을 여러 ev 에 지정해도 ev 마다 따로 읽으므로 문제없다.
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
		return nil, fmt.Errorf("AUTO(1:1 자동 계산)는 삭제된 기능입니다 — sched.vcpuN.affinity=값 줄로 적어주세요")
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

// buildExtraConfig: spec 을 vCenter ExtraConfig 로 변환 (파일 순서 그대로)
func buildExtraConfig(spec *affinitySpec) []types.BaseOptionValue {
	var extraConfig []types.BaseOptionValue
	for _, p := range spec.pairs {
		extraConfig = append(extraConfig, &types.OptionValue{Key: p.Key, Value: p.Value})
	}
	return extraConfig
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
	vmCnt := flag.Int("vm_cnt", 2, "호스트당 대상 VM 개수 (1=ev01, 2=ev01~ev02, ... 99=ev01~ev99)")
	// -affinityFile01 ~ -affinityFile99: 이름 규칙이 같아서 반복문으로 등록한다(기존 01~03 이름 그대로).
	suffixes := make([]string, maxVMCount)
	flagNames := make([]string, maxVMCount)
	filePtrs := make([]*string, maxVMCount)
	for i := 0; i < maxVMCount; i++ {
		suffixes[i] = fmt.Sprintf("ev%02d", i+1)
		name := fmt.Sprintf("affinityFile%02d", i+1)
		flagNames[i] = "-" + name
		filePtrs[i] = flag.String(name, "", suffixes[i]+" affinity 설정 파일 (-vm_cnt 범위의 ev 는 필수. 여러 ev 에 같은 파일을 지정해도 된다)")
	}
	affinityLegacy := flag.String("affinityFile", "", "[구버전 호환] -affinityFile02 미지정 시 ev02 설정 파일로 사용")
	// -ht 는 AUTO(1:1 자동 계산)에만 쓰였다. 기능을 삭제했지만 예전 명령줄이 깨지지 않도록 받아서 무시한다.
	htMode := flag.String("ht", "", "[사용 안 함] AUTO 자동 계산 삭제로 무시된다 (예전 명령줄 호환용)")
	concurrency := flag.Int("concurrency", defaultConcurrency, "동시 처리 개수 제한 (VM 목록 조회 / Reconfigure 전송+대기 전 구간에 적용)")
	collapseEvUsage(regexp.MustCompile(`^affinityFile(\d{2})$`))

	flag.Parse()

	if *vcTargetIP == "" {
		log.Fatal("필수 파라미터가 누락되었습니다. (-vcTargetIP 확인)")
	}

	if *vmCnt < 1 || *vmCnt > maxVMCount {
		log.Fatalf("-vm_cnt 값이 올바르지 않습니다: %d (1~%d 만 허용)", *vmCnt, maxVMCount)
	}

	if *concurrency < 1 {
		log.Fatalf("-concurrency 값이 올바르지 않습니다: %d (1 이상)", *concurrency)
	}

	if *htMode != "" {
		fmt.Println("알림: -ht 는 AUTO(1:1 자동 계산) 삭제로 더 이상 쓰지 않습니다 — 무시합니다.")
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
	if *filePtrs[1] == "" && *affinityLegacy != "" {
		*filePtrs[1] = *affinityLegacy
	}

	// ---- vm_cnt 기준 필수 파라미터 검증 ----
	fileFlags := make([]string, maxVMCount)
	for i, p := range filePtrs {
		fileFlags[i] = *p
	}

	specs := make([]*affinitySpec, *vmCnt)
	for i := 0; i < *vmCnt; i++ {
		if strings.TrimSpace(fileFlags[i]) == "" {
			// 예전에는 파일이 없으면 -ht 로 1:1 자동 계산했지만, ev 마다 CPU 0번부터 잡혀 VM 끼리 겹치므로 삭제했다.
			log.Fatalf("%s affinity 파일이 필요합니다 (%s). 자동 계산은 삭제됐습니다 — 여러 ev 에 같은 파일을 지정해도 됩니다.", suffixes[i], flagNames[i])
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

	fmt.Printf("병렬(워커풀) 방식 어피니티 일괄 할당을 시작합니다. (동시 처리 제한: %d)\n", *concurrency)
	fmt.Printf("  접속 계정 : %s\n", *vcId)
	fmt.Printf("  vCenter   : %s\n", *vcTargetIP)
	fmt.Printf("  대상 호스트: %d대 / VM 개수: %d (%s)\n",
		len(hostlistLines), *vmCnt, strings.Join(suffixes[:*vmCnt], ", "))
	for i := 0; i < *vmCnt; i++ {
		fmt.Printf("  %s 설정  : %s [%d개 항목]\n", suffixes[i], specs[i].fileName, len(specs[i].pairs))
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

	// ---- 전체 데이터센터 순회하며 VM 목록 수집 (데이터센터별 동시 조회, -concurrency 제한 적용) ----
	bootstrapFinder := find.NewFinder(client.Client, true)
	dcList, err := bootstrapFinder.DatacenterList(ctx, "*")
	if err != nil || len(dcList) == 0 {
		log.Fatalf("데이터센터 조회 실패: %v", err)
	}

	var listMu sync.Mutex
	var listWg sync.WaitGroup
	dcSem := make(chan struct{}, *concurrency)
	targetVmMap := make(map[string]*object.VirtualMachine)

	for _, dc := range dcList {
		listWg.Add(1)
		dcSem <- struct{}{}
		go func(dc *object.Datacenter) {
			defer listWg.Done()
			defer func() { <-dcSem }()

			dcFinder := find.NewFinder(client.Client, true)
			dcFinder.SetDatacenter(dc)
			vms, listErr := dcFinder.VirtualMachineList(ctx, "*")
			if listErr != nil {
				// 해당 DC 에 VM 이 없는 경우도 에러로 반환되므로 치명적으로 다루지 않음
				return
			}
			listMu.Lock()
			for _, vm := range vms {
				if regexMatcher.MatchString(vm.Name()) {
					targetVmMap[vm.Name()] = vm
				}
			}
			listMu.Unlock()
		}(dc)
	}
	listWg.Wait()

	if len(targetVmMap) == 0 {
		log.Fatal("worklist 와 매칭되는 VM 을 vCenter 에서 찾지 못했습니다.")
	}

	// ---- 작업 목록 구성 (호스트 x vm_cnt 매트릭스를 평탄화) ----
	var jobs []vmJob
	var preSkipped int
	for _, baseHost := range hostlistLines {
		cleanHost := strings.TrimSpace(baseHost)
		if cleanHost == "" {
			continue
		}
		for i := 0; i < *vmCnt; i++ {
			vmName := cleanHost + suffixes[i]
			vm, ok := targetVmMap[vmName]
			if !ok {
				safePrintf("[%s] 경고: 대상 VM 이 존재하지 않습니다. (PASS)\n", vmName)
				preSkipped++
				continue
			}
			jobs = append(jobs, vmJob{vmName: vmName, vm: vm, specI: specs[i]})
		}
	}

	if len(jobs) == 0 {
		fmt.Println("\n실행할 작업이 없습니다.")
		if preSkipped > 0 {
			os.Exit(2)
		}
		return
	}

	fmt.Printf("\n총 %d개의 설정 작업을 동시 %d개 제한으로 처리합니다 (전송+완료대기를 워커 단위로 병렬 수행).\n", len(jobs), *concurrency)

	// ---- 워커풀: 각 워커가 Reconfigure 전송 + Wait 완료까지 한 VM 단위로 전부 처리 ----
	sem := make(chan struct{}, *concurrency)
	results := make(chan vmResult, len(jobs))
	var jobWg sync.WaitGroup

	for _, j := range jobs {
		jobWg.Add(1)
		sem <- struct{}{}
		go func(j vmJob) {
			defer jobWg.Done()
			defer func() { <-sem }()

			extraConfig := buildExtraConfig(j.specI)

			expectedPairs := make([]optionPair, 0, len(extraConfig))
			for _, ov := range extraConfig {
				if opt, ok := ov.(*types.OptionValue); ok {
					expectedPairs = append(expectedPairs, optionPair{Key: opt.Key, Value: fmt.Sprintf("%v", opt.Value)})
				}
			}

			spec := types.VirtualMachineConfigSpec{ExtraConfig: extraConfig}

			task, taskErr := j.vm.Reconfigure(ctx, spec)
			if taskErr != nil {
				safePrintf("[%s] Reconfigure 명령 전송 실패: %v\n", j.vmName, taskErr)
				results <- vmResult{vmName: j.vmName, skipped: true}
				return
			}

			safePrintf("[%s] %s 기반 병렬 설정 명령 전송 완료 (%d개 항목)\n",
				j.vmName, j.specI.fileName, len(j.specI.pairs))

			if waitErr := task.Wait(ctx); waitErr != nil {
				safePrintf("[%s] 작업 실패: %v\n", j.vmName, waitErr)
				results <- vmResult{vmName: j.vmName, failed: true}
				return
			}

			results <- vmResult{vmName: j.vmName, expected: expectedPairs}
		}(j)
	}

	jobWg.Wait()
	close(results)

	var failed, skipped int
	successVmNames := make([]string, 0, len(jobs))
	expectedByName := make(map[string][]optionPair, len(jobs))
	for r := range results {
		switch {
		case r.skipped:
			skipped++
		case r.failed:
			failed++
		default:
			successVmNames = append(successVmNames, r.vmName)
			expectedByName[r.vmName] = r.expected
		}
	}
	skipped += preSkipped

	// ---- 실제 적용여부 배치 검증 (config.extraConfig 재조회, Task 성공분만 대상, 단일 배치 호출) ----
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

			for _, name := range successVmNames {
				actual, ok := actualMap[name]
				if !ok {
					fmt.Printf("[%s] 실제 적용 확인 실패: 재조회 결과 없음\n", name)
					mismatched++
					continue
				}
				var bad []string
				for _, exp := range expectedByName[name] {
					if actual[exp.Key] != exp.Value {
						bad = append(bad, fmt.Sprintf("%s(기대=%s,실제=%s)", exp.Key, exp.Value, actual[exp.Key]))
					}
				}
				if len(bad) > 0 {
					fmt.Printf("[%s] 실제 적용 불일치: %s\n", name, strings.Join(bad, ", "))
					mismatched++
				} else {
					fmt.Printf("[%s] 실제 적용 확인 완료 (%d개 항목 일치)\n", name, len(expectedByName[name]))
				}
			}
		}
	}

	fmt.Printf("\n완료: 성공 %d / 실패 %d / 스킵 %d / 적용불일치 %d\n",
		len(successVmNames)-mismatched, failed, skipped, mismatched)
	if failed > 0 || skipped > 0 || mismatched > 0 {
		os.Exit(2)
	}
	fmt.Println("모든 VM 의 어피니티 설정이 정상 적용되었습니다 (재조회로 검증 완료).")
}

// collapseEvUsage: ev02~ev99 옵션은 번호만 다르고 내용이 같아서, 도움말(-h)에는 02 하나를
// "02~99"로 바꿔 보여주고 03~99는 생략한다. re 의 첫 캡처 그룹이 ev 번호(두 자리)다.
func collapseEvUsage(re *regexp.Regexp) {
	flag.Usage = func() {
		out := flag.CommandLine.Output()
		fs := flag.NewFlagSet(os.Args[0], flag.ContinueOnError)
		fs.SetOutput(out)
		flag.VisitAll(func(f *flag.Flag) {
			name, usage := f.Name, f.Usage
			if m := re.FindStringSubmatchIndex(name); m != nil {
				n, _ := strconv.Atoi(name[m[2]:m[3]])
				if n > 2 {
					return
				}
				if n == 2 {
					name = name[:m[2]] + "02~99" + name[m[3]:]
					usage = strings.NewReplacer("ev02", "ev02~ev99", "EV02", "EV02~EV99").Replace(usage)
				}
			}
			fs.Var(f.Value, name, usage)
			fs.Lookup(name).DefValue = f.DefValue
		})
		fmt.Fprintf(out, "Usage of %s:\n", os.Args[0])
		fs.PrintDefaults()
	}
}
