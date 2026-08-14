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
	"strconv"
	"strings"
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
	cores := flag.Int("cores", 0, "기대값: 소켓당 코어 수 (필수)")
	numa := flag.Int("numa", 0, "기대값: NUMA 노드당 최대 vCPU 수 (필수)")
	cpu := flag.Int("cpu", 0, "기대값: vCPU 수 (필수)")
	mem := flag.Int("mem", 0, "기대값: 메모리 GB (필수)")
	disk := flag.Int("disk", 0, "기대값: 디스크 총량 GB (필수)")

	sharesEV01 := flag.Int("shares-ev01", 0, "기대값: ev01 그룹 CPU Shares(ratio) (필수)")
	sharesEV02Str := flag.String("shares-ev02", "", "기대값: ev02 그룹 CPU Shares(ratio) (옵션, 안 주면 ev02 shares 체크 스킵)")
	sharesEV03Str := flag.String("shares-ev03", "", "기대값: ev03 그룹 CPU Shares(ratio) (옵션, 안 주면 ev03 shares 체크 스킵)")

	affinityEV02Path := flag.String("affinity-ev02", "", "ev02 그룹 기대 affinity 파일 (옵션, 안 주면 ev02 affinity 체크 스킵)")
	affinityEV03Path := flag.String("affinity-ev03", "", "ev03 그룹 기대 affinity 파일 (옵션, 안 주면 ev03 affinity 체크 스킵)")

	out := flag.String("out", "", "상세 CSV 출력 경로 (미지정 시 vm-param-check_<타임스탬프>.csv 자동 생성). 같은 이름에 _summary가 붙은 요약 CSV가 하나 더 생성됨")
	onlyFail := flag.Bool("onlyFail", false, "PASS(문제 없음)인 VM은 콘솔/CSV 모두에서 제외하고, FAIL/설정없음이 있는 VM만 출력 (대수 많을 때 가독성용)")
	noColor := flag.Bool("noColor", false, "콘솔 출력에서 ANSI 컬러(FAIL=빨강/설정없음=노랑/PASS=초록)를 끔 — 컬러 미지원 터미널이나 파일로 리다이렉트할 때 사용")

	demo := flag.Bool("demo", false, "vCenter에 연결하지 않고, affinity 항목이 많은 8~16vCPU급 가짜 VM 3대(OK/FAIL/개수불일치 케이스)로 콘솔+CSV 출력을 보여주는 데모 모드. 실제 인프라를 전혀 건드리지 않음. 이 모드에서는 다른 모든 플래그를 무시하고 고정된 데모 기대값을 사용함")

	flag.Parse()

	if *demo {
		runDemo(*out, *onlyFail, *noColor)
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

	var affinityEV02, affinityEV03 map[string]string
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

	var allVMs []model.VMInfo
	for _, addr := range vcenters {
		fmt.Printf("[%s] 접속 중...\n", addr)
		client, err := vcenter.Connect(ctx, addr, vcUser, vcPass)
		if err != nil {
			fmt.Printf("[%s] 접속 실패, 이 vCenter는 건너뜁니다: %v\n", addr, err)
			continue
		}

		vms, err := vcenter.FetchVMs(ctx, client, addr, targetNames)
		_ = client.Logout(ctx)
		if err != nil {
			fmt.Printf("[%s] VM 조회 실패, 이 vCenter는 건너뜁니다: %v\n", addr, err)
			continue
		}
		fmt.Printf("[%s] VM %d대 조회됨\n", addr, len(vms))
		allVMs = append(allVMs, vms...)
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

	var allFindings []model.Finding
	for _, vm := range allVMs {
		group := classifyGroup(vm.Hostname)

		allFindings = append(allFindings, checker.CheckFixed(vm)...)
		allFindings = append(allFindings, checker.CheckTopology(vm, *cores, *numa)...)
		allFindings = append(allFindings, checker.CheckHardware(vm, *cpu, *mem, *disk, shares, group, singleVMMode)...)
		allFindings = append(allFindings, checker.CheckHostPower(vm))
		allFindings = append(allFindings, checker.CheckNetwork(vm)...)

		switch group {
		case "ev01":
			expected := checker.GenerateExpectedAffinityEV01(vm.NumCPU, *ht == "on")
			allFindings = append(allFindings, checker.CheckAffinity(vm, expected, "ev01")...)
		case "ev02":
			if !singleVMMode && affinityEV02 != nil {
				allFindings = append(allFindings, checker.CheckAffinity(vm, affinityEV02, "ev02")...)
			}
		case "ev03":
			if !singleVMMode && affinityEV03 != nil {
				allFindings = append(allFindings, checker.CheckAffinity(vm, affinityEV03, "ev03")...)
			}
		}
	}

	if *onlyFail {
		before := len(report.Summarize(allFindings))
		allFindings = report.FilterOnlyFail(allFindings)
		after := len(report.Summarize(allFindings))
		fmt.Printf("-onlyFail: PASS인 VM %d대는 결과에서 제외 (총 %d대 중 %d대만 출력)\n", before-after, before, after)
	}

	fmt.Println()
	report.PrintConsole(os.Stdout, allFindings, !*noColor)

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

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
