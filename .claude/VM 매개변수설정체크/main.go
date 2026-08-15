// main.go: vm-param-check — vCenter의 VM들이 고성능(High Performance) 설정 기준을
// 만족하는지 체크해서 콘솔 요약 + CSV 상세 로그를 산출한다. (PLAN.md 참고)
//
// 실행 모드 (계획서 2장):
//   - 전체 순회 모드 (기본): -vcenterList로 지정된 모든 vCenter의 VM 인벤토리 전체를 체크
//   - 단일/지정 대상 모드: -f=<파일>로 체크 대상 BM(VM) hostname 목록을 주면 그것들만 체크
//     (예: -f kdh.txt, 파일에는 hostname을 한 줄에 하나씩)
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"vm-param-check/checker"
	"vm-param-check/config"
	"vm-param-check/model"
	"vm-param-check/report"
	"vm-param-check/vcenter"
)

func main() {
	vcenterListPath := flag.String("vcenterList", "vcenter.txt", "전체 순회 모드에서 사용할 vCenter 목록 파일 (한 줄에 하나)")
	targetsPath := flag.String("f", "", "단일/지정 대상 모드: 체크할 BM(VM) hostname 목록 파일 (한 줄에 하나, '#' 주석 가능. 예: -f kdh.txt). 지정 시 vcenterList의 vCenter들 안에서 이 hostname들만 체크. 미지정 시 인벤토리 전체를 체크(전체 순회 모드)")

	ht := flag.String("ht", "", "HT(하이퍼스레딩) 상태: on | off (필수, ev01 affinity 자동계산에 사용)")
	cores := flag.Int("cores", 0, "기대값: 소켓당 코어 수 — ev01 및 미분류 VM에 적용 (필수)")
	numa := flag.Int("numa", 0, "기대값: NUMA 노드당 최대 vCPU 수 — ev01 및 미분류 VM에 적용 (필수)")
	cpu := flag.Int("cpu", 0, "기대값: vCPU 수 — ev01 및 미분류 VM에 적용 (필수)")
	mem := flag.Int("mem", 0, "기대값: 메모리 GB — ev01 및 미분류 VM에 적용 (필수)")
	disk := flag.Int("disk", 0, "기대값: 디스크 총량 GB — ev01 및 미분류 VM에 적용 (필수)")

	coresEV02Str := flag.String("cores-ev02", "", "기대값: ev02 그룹 소켓당 코어 수 (옵션, 안 주면 ev02 코어수 체크 스킵)")
	coresEV03Str := flag.String("cores-ev03", "", "기대값: ev03 그룹 소켓당 코어 수 (옵션, 안 주면 ev03 코어수 체크 스킵)")
	numaEV02Str := flag.String("numa-ev02", "", "기대값: ev02 그룹 NUMA 노드당 최대 vCPU 수 (옵션, 안 주면 ev02 NUMA 체크 스킵)")
	numaEV03Str := flag.String("numa-ev03", "", "기대값: ev03 그룹 NUMA 노드당 최대 vCPU 수 (옵션, 안 주면 ev03 NUMA 체크 스킵)")
	cpuEV02Str := flag.String("cpu-ev02", "", "기대값: ev02 그룹 vCPU 수 (옵션, 안 주면 ev02 vCPU 체크 스킵)")
	cpuEV03Str := flag.String("cpu-ev03", "", "기대값: ev03 그룹 vCPU 수 (옵션, 안 주면 ev03 vCPU 체크 스킵)")
	memEV02Str := flag.String("mem-ev02", "", "기대값: ev02 그룹 메모리 GB (옵션, 안 주면 ev02 메모리 체크 스킵)")
	memEV03Str := flag.String("mem-ev03", "", "기대값: ev03 그룹 메모리 GB (옵션, 안 주면 ev03 메모리 체크 스킵)")
	diskEV02Str := flag.String("disk-ev02", "", "기대값: ev02 그룹 디스크 총량 GB (옵션, 안 주면 ev02 디스크 체크 스킵)")
	diskEV03Str := flag.String("disk-ev03", "", "기대값: ev03 그룹 디스크 총량 GB (옵션, 안 주면 ev03 디스크 체크 스킵)")

	sharesEV01 := flag.Int("shares-ev01", 0, "기대값: ev01 그룹 CPU Shares(ratio) (필수)")
	sharesEV02Str := flag.String("shares-ev02", "", "기대값: ev02 그룹 CPU Shares(ratio) (옵션, 안 주면 ev02 shares 체크 스킵)")
	sharesEV03Str := flag.String("shares-ev03", "", "기대값: ev03 그룹 CPU Shares(ratio) (옵션, 안 주면 ev03 shares 체크 스킵)")

	affinityEV01Path := flag.String("affinity-ev01", "", "ev01 그룹 기대 affinity 파일 (옵션. 안 주면 기존과 동일하게 -ht/-cores 기반 자동계산을 사용. 주면 파일값으로 대체)")
	affinityEV02Path := flag.String("affinity-ev02", "", "ev02 그룹 기대 affinity 파일 (옵션, 안 주면 ev02 affinity 체크 스킵)")
	affinityEV03Path := flag.String("affinity-ev03", "", "ev03 그룹 기대 affinity 파일 (옵션, 안 주면 ev03 affinity 체크 스킵)")

	out := flag.String("out", "", "상세 CSV 출력 경로 (미지정 시 vm-param-check_<타임스탬프>.csv 자동 생성). 같은 이름에 _summary가 붙은 요약 CSV가 하나 더 생성됨")
	onlyFail := flag.Bool("onlyFail", false, "PASS(문제 없음)인 VM은 콘솔/CSV 모두에서 제외하고, FAIL/설정없음이 있는 VM만 출력 (대수 많을 때 가독성용)")
	noColor := flag.Bool("noColor", false, "콘솔 출력에서 ANSI 컬러(FAIL=빨강/설정없음=노랑/PASS=초록)를 끔 — 컬러 미지원 터미널이나 파일로 리다이렉트할 때 사용")

	demo := flag.Bool("demo", false, "vCenter에 연결하지 않고, affinity 항목이 많은 8~16vCPU급 가짜 VM 3대(OK/FAIL/개수불일치 케이스)로 콘솔+CSV 출력을 보여주는 데모 모드. 실제 인프라를 전혀 건드리지 않음. 이 모드에서는 다른 모든 플래그를 무시하고 고정된 데모 기대값을 사용함")
	scale := flag.Int("scale", 0, "테스트용: vCenter 연결 없이 N대 규모의 합성 VM으로 콘솔+CSV 출력이 대량 환경에서 어떻게 보이는지 시뮬레이션 (가독성 테스트 전용, -demo와 별개, 실제 인프라 미접속)")

	flag.Parse()

	if *demo {
		runDemo(*out, *onlyFail, *noColor)
		return
	}

	if *scale > 0 {
		runScale(*scale, *out, *onlyFail, *noColor)
		return
	}

	if *ht != "on" && *ht != "off" {
		log.Fatal("-ht=on 또는 -ht=off 필수")
	}
	if *cores == 0 || *numa == 0 || *cpu == 0 || *mem == 0 || *disk == 0 || *sharesEV01 == 0 {
		log.Fatal("-cores/-numa/-cpu/-mem/-disk/-shares-ev01 은 모두 필수입니다")
	}

	var sharesEV02, sharesEV03 *int
	if *sharesEV02Str != "" {
		v, err := strconv.Atoi(*sharesEV02Str)
		if err != nil {
			log.Fatalf("-shares-ev02 값이 정수가 아닙니다: %v", err)
		}
		sharesEV02 = &v
	}
	if *sharesEV03Str != "" {
		v, err := strconv.Atoi(*sharesEV03Str)
		if err != nil {
			log.Fatalf("-shares-ev03 값이 정수가 아닙니다: %v", err)
		}
		sharesEV03 = &v
	}

	coresEV02, err := parseOptionalIntFlag("cores-ev02", *coresEV02Str)
	if err != nil {
		log.Fatal(err)
	}
	coresEV03, err := parseOptionalIntFlag("cores-ev03", *coresEV03Str)
	if err != nil {
		log.Fatal(err)
	}
	numaEV02, err := parseOptionalIntFlag("numa-ev02", *numaEV02Str)
	if err != nil {
		log.Fatal(err)
	}
	numaEV03, err := parseOptionalIntFlag("numa-ev03", *numaEV03Str)
	if err != nil {
		log.Fatal(err)
	}
	cpuEV02, err := parseOptionalIntFlag("cpu-ev02", *cpuEV02Str)
	if err != nil {
		log.Fatal(err)
	}
	cpuEV03, err := parseOptionalIntFlag("cpu-ev03", *cpuEV03Str)
	if err != nil {
		log.Fatal(err)
	}
	memEV02, err := parseOptionalIntFlag("mem-ev02", *memEV02Str)
	if err != nil {
		log.Fatal(err)
	}
	memEV03, err := parseOptionalIntFlag("mem-ev03", *memEV03Str)
	if err != nil {
		log.Fatal(err)
	}
	diskEV02, err := parseOptionalIntFlag("disk-ev02", *diskEV02Str)
	if err != nil {
		log.Fatal(err)
	}
	diskEV03, err := parseOptionalIntFlag("disk-ev03", *diskEV03Str)
	if err != nil {
		log.Fatal(err)
	}

	var affinityEV01, affinityEV02, affinityEV03 map[string]string
	if *affinityEV01Path != "" {
		m, err := config.LoadAffinityFile(*affinityEV01Path)
		if err != nil {
			log.Fatalf("-affinity-ev01 파일 읽기 실패: %v", err)
		}
		affinityEV01 = m
	}
	if *affinityEV02Path != "" {
		m, err := config.LoadAffinityFile(*affinityEV02Path)
		if err != nil {
			log.Fatalf("-affinity-ev02 파일 읽기 실패: %v", err)
		}
		affinityEV02 = m
	}
	if *affinityEV03Path != "" {
		m, err := config.LoadAffinityFile(*affinityEV03Path)
		if err != nil {
			log.Fatalf("-affinity-ev03 파일 읽기 실패: %v", err)
		}
		affinityEV03 = m
	}

	var targetNames map[string]bool
	if *targetsPath != "" {
		names, err := config.LoadLines(*targetsPath)
		if err != nil {
			log.Fatalf("-f 파일 읽기 실패: %v", err)
		}
		targetNames = map[string]bool{}
		for _, n := range names {
			targetNames[n] = true
		}
	}

	vcenters, err := config.LoadLines(*vcenterListPath)
	if err != nil {
		log.Fatalf("%s 읽기 실패: %v (전체 순회 모드에는 vCenter 목록 파일이 필요합니다)", *vcenterListPath, err)
	}
	if len(vcenters) == 0 {
		log.Fatalf("%s 에 유효한 vCenter 주소가 없습니다", *vcenterListPath)
	}

	vcUser := firstNonEmpty(os.Getenv("VC_USER"), os.Getenv("VCENTER_USER"))
	vcPass := firstNonEmpty(os.Getenv("VC_PASS"), os.Getenv("VCENTER_PASS"))
	if vcUser == "" || vcPass == "" {
		log.Fatal("인증 정보 로드 실패: VC_USER/VC_PASS (또는 VCENTER_USER/VCENTER_PASS) 환경 변수가 설정되지 않았습니다")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// vCenter별 접속+조회는 서로 완전히 독립적이라 동시에 실행한다(vcenterList가 여러 개일 때
	// 순차 접속 대기시간을 없애기 위함). 결과는 vcenters 순서대로 모아서, 콘솔 로그 순서가
	// 매번 달라지지 않고(결정적) 재현 가능하게 유지한다.
	type vcResult struct {
		vms []model.VMInfo
		err error
	}
	results := make([]vcResult, len(vcenters))
	var wg sync.WaitGroup
	for i, addr := range vcenters {
		wg.Add(1)
		go func(i int, addr string) {
			defer wg.Done()
			client, err := vcenter.Connect(ctx, addr, vcUser, vcPass)
			if err != nil {
				results[i] = vcResult{err: fmt.Errorf("접속 실패: %w", err)}
				return
			}
			vms, err := vcenter.FetchVMs(ctx, client, addr, targetNames)
			_ = client.Logout(ctx)
			if err != nil {
				results[i] = vcResult{err: fmt.Errorf("VM 조회 실패: %w", err)}
				return
			}
			results[i] = vcResult{vms: vms}
		}(i, addr)
	}
	wg.Wait()

	var allVMs []model.VMInfo
	for i, addr := range vcenters {
		r := results[i]
		if r.err != nil {
			fmt.Printf("[%s] %v, 이 vCenter는 건너뜁니다\n", addr, r.err)
			continue
		}
		fmt.Printf("[%s] VM %d대 조회됨\n", addr, len(r.vms))
		allVMs = append(allVMs, r.vms...)
	}

	if len(allVMs) == 0 {
		fmt.Println("체크 대상 VM이 없어 종료합니다.")
		return
	}

	singleVMMode := len(allVMs) == 1
	if singleVMMode {
		fmt.Println("조사 대상이 VM 1개뿐입니다 — 계획서 3-0 규칙에 따라 ev02/ev03 관련 체크(affinity/shares)는 옵션이 있어도 스킵합니다.")
	}

	shares := checker.SharesExpect{EV01: *sharesEV01, EV02: sharesEV02, EV03: sharesEV03}
	coresExpect := checker.CoresExpect{Base: *cores, EV02: coresEV02, EV03: coresEV03}
	numaExpect := checker.NumaExpect{Base: *numa, EV02: numaEV02, EV03: numaEV03}
	cpuExpect := checker.CPUExpect{Base: *cpu, EV02: cpuEV02, EV03: cpuEV03}
	memExpect := checker.MemExpect{Base: *mem, EV02: memEV02, EV03: memEV03}
	diskExpect := checker.DiskExpect{Base: *disk, EV02: diskEV02, EV03: diskEV03}

	// VM별 체크는 서로 데이터를 공유하지 않는 순수 함수 호출이라 워커풀로 동시에 처리한다.
	// 결과는 VM 인덱스별 슬롯에 쓰고 마지막에 순서대로 이어붙여서, 병렬 처리와 무관하게
	// CSV/콘솔 출력 순서가 항상 결정적으로 재현되게 한다.
	findingsPerVM := make([][]model.Finding, len(allVMs))
	sem := make(chan struct{}, runtime.NumCPU())
	var checkWG sync.WaitGroup
	for i, vm := range allVMs {
		checkWG.Add(1)
		sem <- struct{}{}
		go func(i int, vm model.VMInfo) {
			defer checkWG.Done()
			defer func() { <-sem }()

			group := classifyGroup(vm.Hostname)
			var f []model.Finding
			f = append(f, checker.CheckFixed(vm)...)
			f = append(f, checker.CheckTopology(vm, coresExpect, numaExpect, group, singleVMMode)...)
			f = append(f, checker.CheckHardware(vm, cpuExpect, memExpect, diskExpect, shares, group, singleVMMode)...)
			f = append(f, checker.CheckHostPower(vm))
			f = append(f, checker.CheckNetwork(vm)...)

			switch group {
			case "ev01":
				if affinityEV01 != nil {
					f = append(f, checker.CheckAffinity(vm, affinityEV01, "ev01")...)
				} else {
					expected := checker.GenerateExpectedAffinityEV01(vm.NumCPU, *ht == "on")
					f = append(f, checker.CheckAffinity(vm, expected, "ev01")...)
				}
			case "ev02":
				if !singleVMMode && affinityEV02 != nil {
					f = append(f, checker.CheckAffinity(vm, affinityEV02, "ev02")...)
				}
			case "ev03":
				if !singleVMMode && affinityEV03 != nil {
					f = append(f, checker.CheckAffinity(vm, affinityEV03, "ev03")...)
				}
			}
			findingsPerVM[i] = f
		}(i, vm)
	}
	checkWG.Wait()

	var allFindings []model.Finding
	for _, f := range findingsPerVM {
		allFindings = append(allFindings, f...)
	}

	if *onlyFail {
		before := len(report.Summarize(allFindings))
		allFindings = report.FilterOnlyFail(allFindings)
		after := len(report.Summarize(allFindings))
		fmt.Printf("-onlyFail: PASS인 VM %d대는 결과에서 제외 (총 %d대 중 %d대만 출력)\n", before-after, before, after)
	}

	fmt.Println()
	report.PrintConsole(os.Stdout, allFindings, !*noColor, !*onlyFail)

	detailPath, summaryPath := deriveOutputPaths(*out)
	if err := report.WriteSummaryCSV(summaryPath, allFindings); err != nil {
		log.Fatalf("요약 CSV 저장 실패: %v", err)
	}
	if err := report.WriteCSV(detailPath, allFindings); err != nil {
		log.Fatalf("상세 CSV 저장 실패: %v", err)
	}
	fmt.Printf("\nCSV 저장 완료: 요약=%s, 상세=%s (%d개 항목)\n", summaryPath, detailPath, len(allFindings))
}

// deriveOutputPaths는 -out 값 하나로부터 상세/요약 CSV 두 경로를 만든다.
// -out 미지정 시 타임스탬프 기반 파일명을 만들고, 지정 시 ".csv" 앞에 "_summary"를 끼워넣는다.
func deriveOutputPaths(out string) (detailPath, summaryPath string) {
	base := out
	if base == "" {
		base = fmt.Sprintf("vm-param-check_%s.csv", time.Now().Format("20060102_150405"))
	}
	detailPath = base
	if strings.HasSuffix(base, ".csv") {
		summaryPath = strings.TrimSuffix(base, ".csv") + "_summary.csv"
	} else {
		summaryPath = base + "_summary"
	}
	return detailPath, summaryPath
}

// classifyGroup은 3-0 규칙대로 hostname에 포함된 문자열로 그룹을 정한다.
// 여러 문자열이 동시에 포함될 일은 없다고 가정하고 ev01 -> ev02 -> ev03 순으로 첫 매치를 채택한다.
func classifyGroup(hostname string) string {
	h := strings.ToLower(hostname)
	switch {
	case strings.Contains(h, "ev01"):
		return "ev01"
	case strings.Contains(h, "ev02"):
		return "ev02"
	case strings.Contains(h, "ev03"):
		return "ev03"
	default:
		return ""
	}
}

// parseOptionalIntFlag는 "-cores-ev02" 같은 옵션 문자열 플래그를 *int로 바꾼다.
// 빈 문자열이면 nil(옵션 자체가 안 주어짐, 해당 그룹 체크 스킵)을 반환한다.
func parseOptionalIntFlag(flagName, val string) (*int, error) {
	if val == "" {
		return nil, nil
	}
	v, err := strconv.Atoi(val)
	if err != nil {
		return nil, fmt.Errorf("-%s 값이 정수가 아닙니다: %w", flagName, err)
	}
	return &v, nil
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
