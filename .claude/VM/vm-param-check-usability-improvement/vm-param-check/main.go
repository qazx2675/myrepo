// main.go: vm-param-check — vCenter의 VM들이 고성능(High Performance) 설정 기준을
// 만족하는지 체크해서 콘솔 요약 + CSV 상세 로그를 산출하고, -fix 옵션을 주면 그 자리에서
// FAIL/설정없음 항목을 게이트 검증 후 자동 교정하고 재검증까지 마친다. (PLAN.md 참고)
//
// 파이프라인 (통합):
//
//	[1] 정상값 입력 + vCenter 체크 -> [2] CSV 생성(-user 접미사) -> [3] -fix 없으면 종료
//	-> [4] 게이트(그룹 동질성/전원 OFF) -> [5] dry-run 확인 -> [6] 실제 적용 -> [7] 재검증 CSV
//
// 실행 모드 (계획서 2장):
//   - 전체 순회 모드 (기본): -vcenterList로 지정된 모든 vCenter의 VM 인벤토리 전체를 체크
//   - 단일/지정 대상 모드: -f=<파일>로 체크 대상 BM(VM) hostname 목록을 주면 그것들만 체크
//     (예: -f kdh.txt, 파일에는 hostname을 한 줄에 하나씩)
package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/vmware/govmomi"

	"vm-param-check/checker"
	"vm-param-check/config"
	"vm-param-check/fixer"
	"vm-param-check/model"
	"vm-param-check/report"
	"vm-param-check/vcenter"
)

func main() {
	vcenterListPath := flag.String("vcenterList", "vcenter.txt", "전체 순회 모드에서 사용할 vCenter 목록 파일 (한 줄에 하나)")
	targetsPath := flag.String("f", "", "단일/지정 대상 모드: 체크할 BM(VM) hostname 목록 파일 (한 줄에 하나, '#' 주석 가능. 예: -f kdh.txt). 지정 시 vcenterList의 vCenter들 안에서 이 hostname들만 체크. 미지정 시 인벤토리 전체를 체크(전체 순회 모드)")

	ht := flag.String("ht", "", "HT(하이퍼스레딩) 상태: on | off (필수, ev01 affinity 자동계산에 사용)")
	preferHT := flag.String("preferHT", "", "기대값: numa.vcpu.preferHT — 모든 VM에 공통 적용(그룹 구분 없음). SPEC_DIR 스펙 파일 또는 이 플래그로 값(예: TRUE)이 주어졌을 때만 체크하고, 주어지지 않으면 이 항목은 아예 체크하지 않음(출력 없음)")
	cores := flag.Int("cores", 0, "기대값: 소켓당 코어 수 — ev01 및 미분류 VM에 적용 (필수)")
	numa := flag.Int("numa", 0, "기대값: NUMA 노드당 최대 vCPU 수 — ev01 및 미분류 VM에 적용 (필수)")
	cpu := flag.Int("cpu", 0, "기대값: vCPU 수 — ev01 및 미분류 VM에 적용 (필수)")
	mem := flag.Int("mem", 0, "기대값: 메모리 GB — ev01 및 미분류 VM에 적용 (필수)")
	diskStr := flag.String("disk", "", "기대값: 디스크 총량 GB — ev01 및 미분류 VM에 적용 (필수). 쉼표로 여러 개를 주면 그 중 하나와 맞으면 OK (예: -disk=1024,1026)")

	coresEV02Str := flag.String("cores-ev02", "", "기대값: ev02 그룹 소켓당 코어 수 (옵션, 안 주면 ev02 코어수 체크 스킵)")
	coresEV03Str := flag.String("cores-ev03", "", "기대값: ev03 그룹 소켓당 코어 수 (옵션, 안 주면 ev03 코어수 체크 스킵)")
	numaEV02Str := flag.String("numa-ev02", "", "기대값: ev02 그룹 NUMA 노드당 최대 vCPU 수 (옵션, 안 주면 ev02 NUMA 체크 스킵)")
	numaEV03Str := flag.String("numa-ev03", "", "기대값: ev03 그룹 NUMA 노드당 최대 vCPU 수 (옵션, 안 주면 ev03 NUMA 체크 스킵)")
	cpuEV02Str := flag.String("cpu-ev02", "", "기대값: ev02 그룹 vCPU 수 (옵션, 안 주면 ev02 vCPU 체크 스킵)")
	cpuEV03Str := flag.String("cpu-ev03", "", "기대값: ev03 그룹 vCPU 수 (옵션, 안 주면 ev03 vCPU 체크 스킵)")
	memEV02Str := flag.String("mem-ev02", "", "기대값: ev02 그룹 메모리 GB (옵션, 안 주면 ev02 메모리 체크 스킵)")
	memEV03Str := flag.String("mem-ev03", "", "기대값: ev03 그룹 메모리 GB (옵션, 안 주면 ev03 메모리 체크 스킵)")
	diskEV02Str := flag.String("disk-ev02", "", "기대값: ev02 그룹 디스크 총량 GB (옵션, 안 주면 ev02 디스크 체크 스킵). -disk와 동일하게 쉼표로 여러 개 허용")
	diskEV03Str := flag.String("disk-ev03", "", "기대값: ev03 그룹 디스크 총량 GB (옵션, 안 주면 ev03 디스크 체크 스킵). -disk와 동일하게 쉼표로 여러 개 허용")

	sharesEV01Str := flag.String("shares-ev01", "", "기대값: ev01 그룹 CPU Shares (필수). ratio 숫자(예: 4000) 또는 'normal', 쉼표로 여러 개 나열 가능(예: 4000,normal) — 그 중 하나만 맞아도 OK. normal은 CPU/메모리 Shares Level이 normal인지를 본다")
	sharesEV02Str := flag.String("shares-ev02", "", "기대값: ev02 그룹 CPU Shares (옵션, 안 주면 ev02 shares 체크 스킵). -shares-ev01과 동일하게 ratio 숫자/'normal'을 쉼표로 여러 개 허용")
	sharesEV03Str := flag.String("shares-ev03", "", "기대값: ev03 그룹 CPU Shares (옵션, 안 주면 ev03 shares 체크 스킵). -shares-ev01과 동일하게 ratio 숫자/'normal'을 쉼표로 여러 개 허용")

	affinityEV01Path := flag.String("affinity-ev01", "", "ev01 그룹 기대 affinity 파일 (옵션. 안 주면 기존과 동일하게 -ht/-cores 기반 자동계산을 사용. 주면 파일값으로 대체)")
	affinityEV02Path := flag.String("affinity-ev02", "", "ev02 그룹 기대 affinity 파일 (옵션, 안 주면 ev02 affinity 체크 스킵)")
	affinityEV03Path := flag.String("affinity-ev03", "", "ev03 그룹 기대 affinity 파일 (옵션, 안 주면 ev03 affinity 체크 스킵)")

	specRoot := flag.String("specRoot", "", "스펙 정의 파일들이 모여 있는 루트 경로. 지정하면 체크 대상 VM이 속한 vCenter 인벤토리 폴더 이름을 조회해서, 같은 스펙으로 간주되는 하위 디렉터리의 '<디렉터리명>_spec.txt'를 찾아 기대값 옵션(-cpu/-cores/-numa/...)을 자동으로 채운다. 직접 준 옵션이 항상 우선하며, 적용 전에 확인을 한 번 받는다")

	out := flag.String("out", "", "상세 CSV 출력 경로 (미지정 시 vm-param-check_<타임스탬프>.csv 자동 생성). 같은 이름에 _summary가 붙은 요약 CSV가 하나 더 생성됨")
	user := flag.String("user", "", "CSV 파일명에 붙일 접미사 (예: -out=result.csv -user=kdh -> result_kdh.csv, result_kdh_summary.csv). 여러 사람이 동시에 실행할 때 파일명 충돌 방지용")
	onlyFail := flag.Bool("onlyFail", false, "PASS(문제 없음)인 VM은 '상세'와 상세 CSV에서 제외하고, FAIL/설정없음이 있는 VM만 출력 (대수 많을 때 가독성용). VM별 요약(화면/요약 CSV)에는 PASS 서버도 그대로 나온다")
	noColor := flag.Bool("noColor", false, "콘솔 출력에서 ANSI 컬러(FAIL=빨강/설정없음=노랑/PASS=초록)를 끔 — 컬러 미지원 터미널이나 파일로 리다이렉트할 때 사용")

	fix := flag.Bool("fix", false, "체크 완료 후 FAIL/설정없음 항목을 게이트 검증 -> 확인 -> 자동교정 -> 재검증까지 이어서 진행 (미지정 시 기존과 동일하게 체크+CSV까지만)")
	yes := flag.Bool("yes", false, "-specRoot로 자동매칭된 스펙에 대한 확인 프롬프트(y/N)를 생략한다. 실제 설정을 변경하는 -fix의 최종 확인은 이 옵션과 무관하게 항상 물어본다(실수로 무인 변경되는 경로를 만들지 않기 위함)")
	fixConcurrency := flag.Int("fixConcurrency", 20, "-fix 적용 시 동시 Reconfigure 처리 개수")
	fixOut := flag.String("fixOut", "", "-fix 재검증 CSV 경로 (미지정 시 원본 상세 CSV 이름 기준 '_recheck_<타임스탬프>' 자동 생성)")

	initFolder := flag.String("initFolder", "", "vCenter에 연결하지 않고, -specRoot 아래에 이 이름의 스펙 디렉터리와 '<이름>_spec.txt' 스캐폴드를 만들고 종료한다. 이름은 CAE 폴더 규칙(레코드 4개, 2번째가 CAE<숫자> 또는 LSI<숫자>)을 따라야 한다. -template을 같이 주면 그 스펙의 옵션 값을 그대로 복사해서 채운다(안 주면 빈 틀만 생성)")
	template := flag.String("template", "", "-initFolder와 함께 사용: 값을 그대로 복사해올 기존 vCenter 폴더 이름(또는 그 폴더가 매칭되는 스펙)")

	demo := flag.Bool("demo", false, "vCenter에 연결하지 않고, affinity 항목이 많은 8~16vCPU급 가짜 VM 3대(OK/FAIL/개수불일치 케이스)로 콘솔+CSV 출력을 보여주는 데모 모드. 실제 인프라를 전혀 건드리지 않음. 이 모드에서는 다른 모든 플래그를 무시하고 고정된 데모 기대값을 사용함")
	scale := flag.Int("scale", 0, "테스트용: vCenter 연결 없이 N대 규모의 합성 VM으로 콘솔+CSV 출력이 대량 환경에서 어떻게 보이는지 시뮬레이션 (가독성 테스트 전용, -demo와 별개, 실제 인프라 미접속)")

	flag.Parse()

	// 사용자가 실제로 준 플래그만 기록해둔다. -specRoot로 스펙을 자동 적용할 때
	// "직접 준 옵션이 항상 우선" 규칙을 판정하는 기준이 된다(flag.Visit는 설정된 것만 순회).
	setFlags := map[string]bool{}
	flag.Visit(func(f *flag.Flag) { setFlags[f.Name] = true })

	// 대상이 여러 스펙에 걸쳐 있으면 스펙별로 기대값을 따로 만들어야 해서, 스펙을 적용하기
	// 전의 플래그 값을 기억해둔다(스펙 A를 적용한 값이 스펙 B에 새어 들어가지 않게 되돌리는 용도).
	baseFlagValues := map[string]string{}
	flag.VisitAll(func(f *flag.Flag) { baseFlagValues[f.Name] = f.Value.String() })

	if *initFolder != "" {
		runInitFolder(*specRoot, *initFolder, *template)
		return
	}

	if *demo {
		runDemo(*out, *user, *onlyFail, *noColor)
		return
	}

	if *scale > 0 {
		runScale(*scale, *out, *user, *onlyFail, *noColor)
		return
	}

	// 기대값(정상값)이 다 들어왔는지 검사. -specRoot를 쓰면 vCenter에서 폴더명을 읽어와야
	// 값이 정해지므로, 그때는 스펙을 적용한 뒤에 같은 검사를 한다(아래 참고).
	requireExpectFlags := func() {
		if *ht != "on" && *ht != "off" {
			log.Fatal("-ht=on 또는 -ht=off 필수")
		}
		if *cores == 0 || *numa == 0 || *cpu == 0 || *mem == 0 || *diskStr == "" || *sharesEV01Str == "" {
			log.Fatal("-cores/-numa/-cpu/-mem/-disk/-shares-ev01 은 모두 필수입니다 (-specRoot로 자동으로 채울 수도 있습니다)")
		}
	}
	if *specRoot == "" {
		requireExpectFlags()
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
	//
	// -fix를 쓰면 체크가 끝난 뒤에도 같은 연결로 실제 교정 Reconfigure + 재검증 재조회까지
	// 이어서 써야 해서, 여기서는(기존과 달리) 곧바로 Logout하지 않고 클라이언트를 들고 있다가
	// main() 종료 직전에 한꺼번에 정리한다.
	type vcResult struct {
		client *govmomi.Client
		vms    []model.VMInfo
		err    error
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
			if err != nil {
				_ = client.Logout(ctx)
				results[i] = vcResult{err: fmt.Errorf("VM 조회 실패: %w", err)}
				return
			}
			results[i] = vcResult{client: client, vms: vms}
		}(i, addr)
	}
	wg.Wait()

	clientsByAddr := map[string]*govmomi.Client{}
	defer func() {
		for _, c := range clientsByAddr {
			_ = c.Logout(ctx)
		}
	}()

	var allVMs []model.VMInfo
	var failedVCenters []string
	for i, addr := range vcenters {
		r := results[i]
		if r.err != nil {
			fmt.Printf("[%s] %v, 이 vCenter는 건너뜁니다\n", addr, r.err)
			failedVCenters = append(failedVCenters, addr)
			continue
		}
		fmt.Printf("[%s] VM %d대 조회됨\n", addr, len(r.vms))
		allVMs = append(allVMs, r.vms...)
		clientsByAddr[addr] = r.client
	}

	// -f로 지정한 대상 중 어느 vCenter에서도 못 찾은 게 있으면 반드시 알린다.
	// 이걸 조용히 넘기면 "체크가 통과했다"고 보이는데 실은 검사조차 안 된 서버가 남는다.
	missingTargets := findMissingTargets(targetNames, allVMs)
	warnMissing := func() {
		if len(missingTargets) == 0 && len(failedVCenters) == 0 {
			return
		}
		fmt.Println("\n*** 경고: 요청한 대상을 전부 체크하지 못했습니다 ***")
		if len(missingTargets) > 0 {
			fmt.Printf("  어느 vCenter에서도 찾지 못한 대상 %d대: %s\n",
				len(missingTargets), strings.Join(missingTargets, ", "))
		}
		if len(failedVCenters) > 0 {
			fmt.Printf("  조회에 실패해 건너뛴 vCenter %d개: %s\n",
				len(failedVCenters), strings.Join(failedVCenters, ", "))
			fmt.Println("  (이 vCenter에 있던 대상은 검사되지 않았습니다 — 위 목록에 없더라도 안심할 수 없습니다)")
		}
		fmt.Println("*** 위 대상들은 이번 결과에 포함되어 있지 않습니다 ***")
	}
	warnMissing()

	if len(allVMs) == 0 {
		fmt.Println("체크 대상 VM이 없어 종료합니다.")
		return
	}

	// buildExpect는 "지금 플래그에 들어있는 값"으로 기대값 한 벌을 만든다. 스펙을 바꿔가며
	// 여러 번 부를 수 있어야 해서 함수로 뺐다(대상이 여러 스펙에 걸쳐 있는 경우).
	buildExpect := func() expectSet {
		requireExpectFlags()

		// shares-ev01/02/03는 쉼표로 여러 개(ratio 숫자 또는 'normal' 혼합) 허용.
		sharesEV01, err := parseSharesListFlag("shares-ev01", *sharesEV01Str)
		if err != nil {
			log.Fatal(err)
		}
		sharesEV02, err := parseSharesListFlag("shares-ev02", *sharesEV02Str)
		if err != nil {
			log.Fatal(err)
		}
		sharesEV03, err := parseSharesListFlag("shares-ev03", *sharesEV03Str)
		if err != nil {
			log.Fatal(err)
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
		// 디스크는 허용값을 여러 개 줄 수 있어서 목록으로 읽는다.
		diskBase, err := parseIntListFlag("disk", *diskStr)
		if err != nil {
			log.Fatal(err)
		}
		diskEV02, err := parseIntListFlag("disk-ev02", *diskEV02Str)
		if err != nil {
			log.Fatal(err)
		}
		diskEV03, err := parseIntListFlag("disk-ev03", *diskEV03Str)
		if err != nil {
			log.Fatal(err)
		}

		e := expectSet{
			Cores:    checker.CoresExpect{Base: *cores, EV02: coresEV02, EV03: coresEV03},
			Numa:     checker.NumaExpect{Base: *numa, EV02: numaEV02, EV03: numaEV03},
			CPU:      checker.CPUExpect{Base: *cpu, EV02: cpuEV02, EV03: cpuEV03},
			Mem:      checker.MemExpect{Base: *mem, EV02: memEV02, EV03: memEV03},
			Disk:     checker.DiskExpect{Base: diskBase, EV02: diskEV02, EV03: diskEV03},
			Shares:   checker.SharesExpect{EV01: sharesEV01, EV02: sharesEV02, EV03: sharesEV03},
			HTOn:     *ht == "on",
			PreferHT: *preferHT,
		}
		if *affinityEV01Path != "" {
			m, err := config.LoadAffinityFile(*affinityEV01Path)
			if err != nil {
				log.Fatalf("-affinity-ev01 파일 읽기 실패: %v", err)
			}
			e.AffinityEV01 = m
		}
		if *affinityEV02Path != "" {
			m, err := config.LoadAffinityFile(*affinityEV02Path)
			if err != nil {
				log.Fatalf("-affinity-ev02 파일 읽기 실패: %v", err)
			}
			e.AffinityEV02 = m
		}
		if *affinityEV03Path != "" {
			m, err := config.LoadAffinityFile(*affinityEV03Path)
			if err != nil {
				log.Fatalf("-affinity-ev03 파일 읽기 실패: %v", err)
			}
			e.AffinityEV03 = m
		}
		return e
	}

	// 기대값은 VM마다 다를 수 있다 — -specRoot를 쓰면 VM이 속한 폴더의 스펙을 따르기 때문에,
	// 대상이 여러 vCenter/여러 폴더에 걸쳐 서로 다른 스펙이어도 각자 자기 스펙으로 체크된다.
	var expectByVM map[string]expectSet
	if *specRoot != "" {
		expectByVM = applyFolderSpecs(*specRoot, allVMs, setFlags, baseFlagValues, *yes, buildExpect)
	} else {
		e := buildExpect()
		expectByVM = map[string]expectSet{}
		for _, vm := range allVMs {
			expectByVM[vm.Name] = e
		}
	}

	singleVMMode := len(allVMs) == 1
	if singleVMMode {
		fmt.Println("조사 대상이 VM 1개뿐입니다 — 계획서 3-0 규칙에 따라 ev02/ev03 관련 체크(affinity/shares)는 옵션이 있어도 스킵합니다.")
	}

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
			findingsPerVM[i] = evaluateVM(vm, expectByVM[vm.Name], singleVMMode)
		}(i, vm)
	}
	checkWG.Wait()

	var allFindings []model.Finding
	for _, f := range findingsPerVM {
		allFindings = append(allFindings, f...)
	}

	// -fix용 교정 계획은 -onlyFail로 걸러지기 전의 원본 findings를 써야 한다(대상 VM의
	// OK 항목도 계획 산출에 필요할 수 있어서). CSV/콘솔 출력에만 -onlyFail 필터를 적용한다.
	fixSourceFindings := allFindings

	displayFindings := allFindings
	if *onlyFail {
		before := len(report.Summarize(displayFindings))
		displayFindings = report.FilterOnlyFail(displayFindings)
		after := len(report.Summarize(displayFindings))
		fmt.Printf("-onlyFail: PASS인 VM %d대는 상세에서 제외 (총 %d대 중 %d대만 출력, 요약에는 전부 나옵니다)\n", before-after, before, after)
	}

	fmt.Println()
	report.PrintConsole(os.Stdout, displayFindings, allFindings, !*noColor, !*onlyFail)

	// 요약(화면/CSV)은 -onlyFail이어도 PASS 서버까지 전부 보여준다 — 어떤 서버가 검사됐고
	// 이상 없었는지가 요약에서 사라지면 안 된다. 상세만 문제 항목으로 좁힌다.
	detailPath, summaryPath := deriveOutputPaths(*out, *user)
	if err := report.WriteSummaryCSV(summaryPath, allFindings); err != nil {
		log.Fatalf("요약 CSV 저장 실패: %v", err)
	}
	if err := report.WriteCSV(detailPath, displayFindings); err != nil {
		log.Fatalf("상세 CSV 저장 실패: %v", err)
	}
	fmt.Printf("\nCSV 저장 완료: 요약=%s, 상세=%s (%d개 항목)\n", summaryPath, detailPath, len(displayFindings))

	if !*fix {
		warnMissing() // 결과 출력이 길어서 위쪽 경고가 묻히기 쉬우므로 맨 끝에 한 번 더
		return
	}

	runFix(ctx, clientsByAddr, allVMs, fixSourceFindings, detailPath, *fixOut, *fixConcurrency, expectByVM)

	// 교정/재검증 로그가 길어서 위쪽 경고가 묻히기 쉬우므로 맨 끝에 한 번 더 알린다.
	warnMissing()
}

// expectSet은 VM 1대를 판정하는 데 필요한 기대값 한 벌이다.
// -specRoot를 쓰면 VM이 속한 폴더의 스펙에 따라 VM마다 이 값이 달라질 수 있어서 묶어 두었다.
type expectSet struct {
	Cores  checker.CoresExpect
	Numa   checker.NumaExpect
	CPU    checker.CPUExpect
	Mem    checker.MemExpect
	Disk   checker.DiskExpect
	Shares checker.SharesExpect

	AffinityEV01 map[string]string
	AffinityEV02 map[string]string
	AffinityEV03 map[string]string

	HTOn bool

	// PreferHT는 numa.vcpu.preferHT 기대값(예: "TRUE")이다. 모든 VM에 공통 적용되는
	// 단일 값이라 그룹별 구조체가 아니다. 빈 문자열이면 이 항목은 체크하지 않는다.
	PreferHT string
}

// findMissingTargets는 -f로 요청했는데 어느 vCenter에서도 나오지 않은 이름을 돌려준다.
// FetchVMs가 VM 이름과 guest hostname 둘 다로 매칭하므로 여기서도 둘 다 찾은 걸로 친다.
func findMissingTargets(targetNames map[string]bool, found []model.VMInfo) []string {
	if targetNames == nil {
		return nil // 전체 순회 모드에는 "요청한 대상"이라는 개념이 없다
	}
	seen := map[string]bool{}
	for _, vm := range found {
		seen[vm.Name] = true
		seen[vm.Hostname] = true
	}
	var missing []string
	for name := range targetNames {
		if !seen[name] {
			missing = append(missing, name)
		}
	}
	sort.Strings(missing)
	return missing
}

// evaluateVM은 VM 1대에 대해 계획서 3장의 모든 체크 항목을 수행해 findings를 만든다.
// 최초 체크와 -fix 재검증이 완전히 같은 판정 로직을 쓰도록 공유 함수로 뺐다 —
// 두 군데 로직이 갈라지면 "교정했는데 재검증 기준이 달라서 결과가 다르게 나오는" 문제가 생긴다.
func evaluateVM(vm model.VMInfo, e expectSet, singleVMMode bool) []model.Finding {
	coresExpect, numaExpect, cpuExpect := e.Cores, e.Numa, e.CPU
	memExpect, diskExpect, shares := e.Mem, e.Disk, e.Shares
	affinityEV01, affinityEV02, affinityEV03 := e.AffinityEV01, e.AffinityEV02, e.AffinityEV03
	htOn := e.HTOn

	// ev01/ev02/ev03 그룹은 vCenter에 등록된 VM 이름(vm.Name) 기준으로 정한다. 게스트 OS가
	// 보고하는 hostname(vm.Hostname)은 클론/구성 실수로 vCenter 이름과 어긋날 수 있어(예:
	// 두 VM의 내부 hostname이 서로 바뀌어 설정됨) 그룹 판정 기준으로 쓰면 엉뚱한 스펙으로
	// 체크되는 사고가 난다 — 대상 지정(-f)도 vm.Name 기준이라 일관성도 맞는다.
	group := classifyGroup(vm.Name)
	if hostnameGroup := classifyGroup(vm.Hostname); hostnameGroup != "" && hostnameGroup != group {
		fmt.Printf("  [경고] %s: vCenter 이름 기준 그룹(%q)과 게스트 hostname(%s) 기준 그룹(%q)이 다릅니다 — vCenter 이름 기준으로 체크합니다. 게스트 OS의 hostname이 실제와 다르게 설정되어 있을 수 있으니 확인하세요\n",
			vm.Name, group, vm.Hostname, hostnameGroup)
	}
	// vcsim (127.0.0.1:54321)에서는 일부 필드를 지원하지 않으므로 플래그 계산
	isVcsim := strings.HasPrefix(vm.VCenter, "127.0.0.1")
	var f []model.Finding
	f = append(f, checker.CheckFixed(vm)...)
	f = append(f, checker.CheckTopology(vm, coresExpect, numaExpect, group, singleVMMode, isVcsim)...)
	f = append(f, checker.CheckPreferHT(vm, e.PreferHT)...)
	f = append(f, checker.CheckHardware(vm, cpuExpect, memExpect, diskExpect, shares, group, singleVMMode, isVcsim)...)
	f = append(f, checker.CheckHostPower(vm))
	f = append(f, checker.CheckNetwork(vm)...)

	switch group {
	case "ev01":
		if affinityEV01 != nil {
			f = append(f, checker.CheckAffinity(vm, affinityEV01, "ev01")...)
		} else {
			expected := checker.GenerateExpectedAffinityEV01(vm.NumCPU, htOn)
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
	return f
}

// runFix는 통합 파이프라인의 [4]~[7] 단계 — 게이트 검증 -> dry-run -> 확인 -> 적용 -> 재검증.
func runFix(ctx context.Context, clientsByAddr map[string]*govmomi.Client, allVMs []model.VMInfo, findings []model.Finding,
	originalDetailPath, fixOutFlag string, concurrency int,
	expectByVM map[string]expectSet) {

	plan, err := fixer.BuildPlan(findings, allVMs)
	if err != nil {
		log.Fatalf("교정 계획 산출 실패: %v", err)
	}

	if len(plan.Fixes) == 0 {
		fmt.Println("\n자동교정 대상 FAIL/설정없음 항목이 없습니다.")
		if len(plan.Manual) > 0 {
			fmt.Printf("수동조치 필요 항목 %d건은 이 도구가 다루지 않습니다(메모리/디스크/Shares/네트워크/호스트 전원정책 등).\n", len(plan.Manual))
		}
		return
	}

	targets := plan.TargetVMs()
	if err := fixer.CheckGates(targets, allVMs); err != nil {
		log.Fatalf("%v", err)
	}
	fmt.Println("\n[OK] 그룹 동질성 검증 통과 — 대상 VM 전부 동일 스펙")
	fmt.Println("[OK] 전원 OFF 검증 통과 — 대상 VM 전부 꺼져 있음")

	fmt.Println()
	fixer.Describe(os.Stdout, plan)

	// 여기부터는 실제 vCenter 설정을 바꾸는 단계라, -yes를 줬더라도 반드시 사람에게 묻는다.
	// (무인 실행으로 stdin이 비어 있으면 confirmYes가 false라서 아무것도 바꾸지 않고 끝난다 —
	// 실수로 설정이 바뀌는 쪽보다 아무것도 안 바뀌는 쪽이 항상 안전하다.)
	fmt.Print("\n위 내용대로 실제 설정을 변경하시겠습니까? (y/N): ")
	if !confirmYes() {
		fmt.Println("취소되었습니다. 아무 설정도 변경하지 않았습니다.")
		return
	}

	fmt.Println("\n=== 설정 적용 ===")
	results := fixer.Apply(ctx, clientsByAddr, plan, concurrency)
	var failedVMs []string
	for _, r := range results {
		if r.Err != nil {
			failedVMs = append(failedVMs, r.VM)
			fmt.Printf("[%s] 적용 실패: %v\n", r.VM, r.Err)
		} else {
			fmt.Printf("[%s] 적용 완료\n", r.VM)
		}
	}
	if len(failedVMs) > 0 {
		log.Fatalf("%d대 적용 실패 — 재검증을 건너뜁니다: %s", len(failedVMs), strings.Join(failedVMs, ", "))
	}

	fmt.Println("\n=== 재검증 ===")
	vcAddrByVM := map[string]string{}
	for _, vm := range allVMs {
		vcAddrByVM[vm.Name] = vm.VCenter
	}
	recheckFindings, err := recheckVMs(ctx, clientsByAddr, vcAddrByVM, targets, expectByVM)
	if err != nil {
		log.Fatalf("재검증 실패: %v", err)
	}

	report.PrintConsole(os.Stdout, recheckFindings, recheckFindings, true, true)

	recheckDetail, recheckSummary := deriveRecheckPaths(originalDetailPath, fixOutFlag)
	if err := report.WriteSummaryCSV(recheckSummary, recheckFindings); err != nil {
		log.Fatalf("재검증 요약 CSV 저장 실패: %v", err)
	}
	if err := report.WriteCSV(recheckDetail, recheckFindings); err != nil {
		log.Fatalf("재검증 상세 CSV 저장 실패: %v", err)
	}

	stillBroken := 0
	for _, f := range recheckFindings {
		if f.Result == "FAIL" || f.Result == "설정없음" {
			stillBroken++
		}
	}
	if stillBroken > 0 {
		fmt.Printf("\n[경고] 재검증 CSV=%s 에 아직 FAIL/설정없음 %d건이 남아있습니다 — 직접 확인하세요.\n", recheckDetail, stillBroken)
	} else {
		fmt.Printf("\n재검증 완료: 교정 대상 항목이 전부 OK입니다. 요약=%s, 상세=%s\n", recheckSummary, recheckDetail)
	}
}

// recheckVMs는 교정을 적용한 VM들만 골라 vCenter에서 다시 조회하고, evaluateVM으로
// 최초 체크와 동일한 기준으로 재판정한다. 대상이 여러 vCenter에 걸쳐 있을 수 있어서
// vcAddrByVM으로 vCenter별 이름 집합을 나눠 그 vCenter에서만 조회한다.
func recheckVMs(ctx context.Context, clientsByAddr map[string]*govmomi.Client, vcAddrByVM map[string]string, targets []string,
	expectByVM map[string]expectSet) ([]model.Finding, error) {

	namesByAddr := map[string]map[string]bool{}
	for _, name := range targets {
		addr := vcAddrByVM[name]
		if namesByAddr[addr] == nil {
			namesByAddr[addr] = map[string]bool{}
		}
		namesByAddr[addr][name] = true
	}

	var recheckedVMs []model.VMInfo
	for addr, names := range namesByAddr {
		client, ok := clientsByAddr[addr]
		if !ok {
			return nil, fmt.Errorf("%s에 대한 vCenter 연결이 없습니다", addr)
		}
		vms, err := vcenter.FetchVMs(ctx, client, addr, names)
		if err != nil {
			return nil, fmt.Errorf("%s 재조회 실패: %w", addr, err)
		}
		recheckedVMs = append(recheckedVMs, vms...)
	}

	// 재검증 대상은 곧 교정된 VM 전체이므로, "VM 1개뿐이면 ev02/ev03 스킵" 규칙도
	// 최초 체크와 동일하게 재검증 시점의 대상 수 기준으로 다시 판단한다.
	singleVMMode := len(recheckedVMs) == 1

	var findings []model.Finding
	for _, vm := range recheckedVMs {
		findings = append(findings, evaluateVM(vm, expectByVM[vm.Name], singleVMMode)...)
	}
	return findings, nil
}

// specSettableFlags는 스펙 파일이 지정할 수 있는 플래그 목록이다.
// 스펙 파일이 -fix나 -out 같은 동작 플래그까지 건드릴 수 있으면 파일 하나로 실제 설정 변경이
// 시작될 수도 있어서, 기대값(정상값) 관련 플래그로만 제한한다.
var specSettableFlags = map[string]bool{
	"ht": true, "preferHT": true, "cores": true, "numa": true, "cpu": true, "mem": true, "disk": true,
	"shares-ev01": true, "shares-ev02": true, "shares-ev03": true,
	"cores-ev02": true, "cores-ev03": true,
	"numa-ev02": true, "numa-ev03": true,
	"cpu-ev02": true, "cpu-ev03": true,
	"mem-ev02": true, "mem-ev03": true,
	"disk-ev02": true, "disk-ev03": true,
	"affinity-ev01": true, "affinity-ev02": true, "affinity-ev03": true,
}

// folderResolution은 VM 1대에 대해 "어느 스펙을 쓸지"를 정한 근거다.
// Actual과 Resolved가 다르면 vCenter 폴더명 자체로는 스펙을 못 찾아서(Task 폴더 등)
// 다른 방법(포트그룹 파싱/수동 입력)으로 알아냈다는 뜻이라, 화면에 그 사실을 함께 보여준다.
type folderResolution struct {
	Actual   string // VM이 실제로 속한 vCenter 인벤토리 폴더
	Resolved string // 스펙 매칭에 실제로 쓴 폴더명 (CAE 규칙을 만족)
	Source   string // "vCenter 폴더" | "포트그룹 파싱" | "수동 입력"
}

// applyFolderSpecs는 체크 대상 VM들이 속한 vCenter 인벤토리 폴더 이름으로 스펙 파일을 찾아
// VM별 기대값을 만든다. 대상이 여러 vCenter/여러 폴더에 걸쳐 서로 다른 스펙이더라도,
// 각 VM은 자기가 속한 폴더의 스펙으로 체크된다(폴더별로 나눠 실행할 필요 없음).
//
// VM이 속한 폴더가 CAE 규칙과 안 맞으면(예: 임시 작업용 "Task" 폴더) resolveVMFolders가
// 포트그룹명 파싱 → 실패 시 대화형 입력으로 실제 스펙 폴더를 알아낸다.
//
// 사용자가 직접 준 플래그는 어느 스펙에서도 덮어쓰지 않는다(수동 우선).
// 무엇이 적용될지 전부 보여준 뒤 확인을 받고, 아니라고 하면 아무것도 하지 않고 종료한다.
func applyFolderSpecs(specRoot string, vms []model.VMInfo, setFlags map[string]bool,
	baseFlagValues map[string]string, autoYes bool, buildExpect func() expectSet) map[string]expectSet {

	resolutions := resolveVMFolders(specRoot, vms, autoYes)

	vmsByResolved := map[string][]string{}
	for _, vm := range vms {
		r := resolutions[vm.Name]
		vmsByResolved[r.Resolved] = append(vmsByResolved[r.Resolved], vm.Name)
	}
	resolvedFolders := make([]string, 0, len(vmsByResolved))
	for f := range vmsByResolved {
		resolvedFolders = append(resolvedFolders, f)
	}
	sort.Strings(resolvedFolders) // 출력 순서가 실행할 때마다 달라지지 않게

	// 스펙을 찾는다. 여러 (resolve된)폴더가 같은 스펙으로 모이는 건 정상이다(차수만 다른 경우 등).
	matchByResolved := map[string]*config.SpecMatch{}
	resolvedBySpec := map[string][]string{}
	for _, folder := range resolvedFolders {
		m, err := config.FindSpec(specRoot, folder)
		if err != nil {
			log.Fatalf("스펙 자동매칭 실패: %v", err)
		}
		if m == nil {
			log.Fatalf("폴더 %q에 해당하는 스펙 디렉터리를 %s 아래에서 찾지 못했습니다", folder, specRoot)
		}
		matchByResolved[folder] = m
		resolvedBySpec[m.SpecFile] = append(resolvedBySpec[m.SpecFile], folder)
	}

	specFiles := make([]string, 0, len(resolvedBySpec))
	for sf := range resolvedBySpec {
		specFiles = append(specFiles, sf)
	}
	sort.Strings(specFiles)

	fmt.Println("\n=== 폴더명 기반 스펙 자동매칭 ===")
	expectBySpec := map[string]expectSet{}
	for _, specFile := range specFiles {
		match := matchByResolved[resolvedBySpec[specFile][0]]
		fmt.Printf("\n[스펙] %s\n", specFile)
		printResolutionGroups(resolutions, vms, specFile, matchByResolved)

		// 앞 스펙에서 채운 값이 남아 새어 들어가지 않도록, 매번 사용자가 준 상태로 되돌린 뒤 적용한다.
		restoreFlags(baseFlagValues)
		applySpecOptions(match, setFlags)
		expectBySpec[specFile] = buildExpect()
	}

	if autoYes {
		fmt.Println("\n-yes 지정됨 — 위 스펙 그대로 진행합니다.")
	} else {
		fmt.Print("\n위 스펙으로 진행할까요? (y/N): ")
		if !confirmYes() {
			fmt.Println("사용자가 취소했습니다. 아무것도 하지 않고 종료합니다.")
			os.Exit(0)
		}
	}

	expectByVM := map[string]expectSet{}
	for _, vm := range vms {
		specFile := matchByResolved[resolutions[vm.Name].Resolved].SpecFile
		expectByVM[vm.Name] = expectBySpec[specFile]
	}
	return expectByVM
}

// printResolutionGroups는 한 스펙으로 모인 VM들을, 실제 vCenter 폴더별로 묶어서 보여준다.
// 폴더명 그대로 매칭된 VM과 예외 경로(포트그룹/수동)로 매칭된 VM을 구분해서 표시한다.
func printResolutionGroups(resolutions map[string]folderResolution, vms []model.VMInfo, specFile string, matchByResolved map[string]*config.SpecMatch) {
	vmsByActual := map[string][]string{}
	sourceByActual := map[string]string{}
	for _, vm := range vms {
		r := resolutions[vm.Name]
		if matchByResolved[r.Resolved].SpecFile != specFile {
			continue
		}
		vmsByActual[r.Actual] = append(vmsByActual[r.Actual], vm.Name)
		sourceByActual[r.Actual] = r.Source
	}
	actuals := make([]string, 0, len(vmsByActual))
	for a := range vmsByActual {
		actuals = append(actuals, a)
	}
	sort.Strings(actuals)
	for _, a := range actuals {
		if sourceByActual[a] == "vCenter 폴더" {
			fmt.Printf("  vCenter 폴더: %s  (VM: %s)\n", a, strings.Join(vmsByActual[a], ", "))
		} else {
			fmt.Printf("  vCenter 폴더: %s  (VM: %s)  ※ CAE 규칙과 안 맞아 %s으로 스펙 결정\n",
				a, strings.Join(vmsByActual[a], ", "), sourceByActual[a])
		}
	}
}

// resolveVMFolders는 VM별로 실제 스펙 매칭에 쓸 폴더명을 정한다.
// vm.Folder가 CAE 규칙을 만족하면 그대로 쓰고, 아니면(Task 폴더 등) 포트그룹명 파싱을
// 시도하고, 그래도 안 되면 사람에게 물어본다(-yes면 물어볼 수 없으므로 바로 중단).
func resolveVMFolders(specRoot string, vms []model.VMInfo, autoYes bool) map[string]folderResolution {
	result := map[string]folderResolution{}
	var noFolder []string
	for _, vm := range vms {
		if vm.Folder == "" {
			noFolder = append(noFolder, vm.Name)
			continue
		}
		if _, ok := config.NormalizeFolderName(vm.Folder); ok {
			result[vm.Name] = folderResolution{Actual: vm.Folder, Resolved: vm.Folder, Source: "vCenter 폴더"}
			continue
		}
		resolved, source := resolveExceptionFolder(specRoot, vm, autoYes)
		result[vm.Name] = folderResolution{Actual: vm.Folder, Resolved: resolved, Source: source}
	}
	if len(noFolder) > 0 {
		log.Fatalf("인벤토리 폴더를 알 수 없는 VM이 있어 스펙을 자동으로 정할 수 없습니다: %s", strings.Join(noFolder, ", "))
	}
	return result
}

// resolveExceptionFolder는 vm.Folder가 CAE 규칙과 안 맞을 때(예: Task 폴더)의 폴백이다.
//  1. 이 VM의 포트그룹 이름들 중 "<폴더명>-cae-옥텟-옥텟-옥텟-옥텟" 패턴에서 폴더명을 뽑아,
//     그 폴더명으로 스펙이 실제로 있는지 확인한다. 유일하게 하나만 있으면 그걸로 확정한다.
//  2. 후보가 없거나 여럿이면(자동으로 하나를 고를 근거가 없으면) 사람에게 직접 물어본다.
func resolveExceptionFolder(specRoot string, vm model.VMInfo, autoYes bool) (string, string) {
	seen := map[string]bool{}
	var candidates []string
	for _, n := range vm.Networks {
		folder, ok := config.ExtractFolderFromPortgroup(n.Portgroup)
		if !ok || seen[folder] {
			continue
		}
		seen[folder] = true
		if m, err := config.FindSpec(specRoot, folder); err == nil && m != nil {
			candidates = append(candidates, folder)
		}
	}
	if len(candidates) == 1 {
		fmt.Printf("  [포트그룹 유추] %s: vCenter 폴더 %q는 CAE 규칙과 안 맞지만, 포트그룹명에서 %q 를 찾아 그 스펙으로 진행합니다\n",
			vm.Name, vm.Folder, candidates[0])
		return candidates[0], "포트그룹 파싱"
	}
	if len(candidates) > 1 {
		sort.Strings(candidates)
		fmt.Printf("  [주의] %s: 포트그룹명에서 스펙 후보가 여러 개(%s)라 자동으로 고를 수 없습니다 — 직접 입력받습니다\n",
			vm.Name, strings.Join(candidates, ", "))
	}

	if autoYes {
		log.Fatalf("%s: vCenter 폴더 %q가 CAE 규칙에 안 맞고 포트그룹명으로도 스펙을 확정하지 못했습니다. "+
			"-yes로는 이럴 때 물어볼 수 없으니, -yes 없이 다시 실행해서 직접 골라주세요", vm.Name, vm.Folder)
	}
	return promptFolderName(specRoot, vm)
}

// promptFolderName은 자동으로 못 정했을 때 사람에게 직접 스펙 폴더명을 입력받는다.
// '?'를 입력하면 -specRoot 아래 사용 가능한 스펙 디렉터리 목록을 보여준다.
func promptFolderName(specRoot string, vm model.VMInfo) (string, string) {
	fmt.Printf("\n%s: vCenter 폴더 %q가 CAE 규칙과 안 맞고(예: Task 폴더), 포트그룹명으로도 스펙을 자동으로 못 찾았습니다.\n", vm.Name, vm.Folder)
	reader := bufio.NewReader(os.Stdin)
	for attempt := 1; attempt <= 3; attempt++ {
		fmt.Print("  이 VM에 적용할 스펙의 vCenter 폴더명을 입력하세요 (목록을 보려면 ? 입력): ")
		line, _ := reader.ReadString('\n')
		line = strings.TrimSpace(line)
		if line == "?" {
			printAvailableSpecs(specRoot)
			continue
		}
		if line == "" {
			continue
		}
		m, err := config.FindSpec(specRoot, line)
		if err != nil {
			fmt.Printf("    %v\n", err)
			continue
		}
		if m == nil {
			fmt.Printf("    %q 에 해당하는 스펙을 찾지 못했습니다. 다시 입력해주세요.\n", line)
			continue
		}
		return line, "수동 입력"
	}
	log.Fatalf("%s: 3번 시도해도 유효한 스펙을 정하지 못해 중단합니다", vm.Name)
	panic("unreachable")
}

// printAvailableSpecs는 -specRoot 바로 아래에 있는 스펙 디렉터리 이름을 나열한다.
func printAvailableSpecs(specRoot string) {
	entries, err := os.ReadDir(specRoot)
	if err != nil {
		fmt.Printf("    %s 읽기 실패: %v\n", specRoot, err)
		return
	}
	fmt.Println("    사용 가능한 스펙 디렉터리:")
	for _, e := range entries {
		if e.IsDir() {
			fmt.Printf("      - %s\n", e.Name())
		}
	}
}

// restoreFlags는 모든 플래그를 스펙 적용 전(= 사용자가 준) 값으로 되돌린다.
func restoreFlags(baseFlagValues map[string]string) {
	flag.VisitAll(func(f *flag.Flag) {
		v, ok := baseFlagValues[f.Name]
		if !ok {
			return
		}
		if err := f.Value.Set(v); err != nil {
			log.Fatalf("플래그 -%s 를 %q 로 되돌리지 못했습니다: %v", f.Name, v, err)
		}
	})
}

// applySpecOptions는 스펙 파일의 옵션을 플래그에 적용한다. 사용자가 직접 준 플래그는 건너뛴다.
func applySpecOptions(match *config.SpecMatch, setFlags map[string]bool) {
	for _, opt := range match.Options {
		f := flag.Lookup(opt.Name)
		if f == nil {
			log.Fatalf("%s 에 알 수 없는 옵션 이름이 있습니다: %q", match.SpecFile, opt.Name)
		}
		if !specSettableFlags[opt.Name] {
			log.Fatalf("%s 의 -%s 는 스펙 파일에서 지정할 수 없는 옵션입니다 (기대값 관련 옵션만 허용)", match.SpecFile, opt.Name)
		}
		if setFlags[opt.Name] {
			fmt.Printf("    [수동 우선] -%s: 직접 주신 %q 를 사용합니다 (스펙값 %q 는 무시)\n", opt.Name, f.Value.String(), opt.Value)
			continue
		}
		value := opt.Value
		// affinity 파일은 스펙 파일과 같은 폴더에 두는 게 자연스러워서, 상대경로는 그쪽 기준으로 푼다.
		if strings.HasPrefix(opt.Name, "affinity-") && !filepath.IsAbs(value) {
			value = filepath.Join(match.SpecDir, value)
		}
		if err := flag.Set(opt.Name, value); err != nil {
			log.Fatalf("%s 의 -%s=%s 적용 실패: %v", match.SpecFile, opt.Name, opt.Value, err)
		}
		fmt.Printf("    [스펙적용] -%s=%s\n", opt.Name, value)
	}
}

// runInitFolder는 vCenter에 연결하지 않고 -specRoot 아래에 신규 스펙 디렉터리 틀을 만든다.
func runInitFolder(specRoot, folderName, templateFolder string) {
	if specRoot == "" {
		log.Fatal("-initFolder는 -specRoot와 함께 써야 합니다 (어느 스펙 루트 아래에 만들지 알 수 없습니다)")
	}
	specFile, err := config.InitFolder(specRoot, folderName, templateFolder)
	if err != nil {
		log.Fatalf("폴더 셋업 실패: %v", err)
	}
	if templateFolder != "" {
		fmt.Printf("스펙 생성 완료(템플릿=%s): %s\n", templateFolder, specFile)
	} else {
		fmt.Printf("스펙 생성 완료(빈 틀): %s\n필요한 값을 채운 뒤 -specRoot=%s 로 체크해보세요.\n", specFile, specRoot)
	}
}

func confirmYes() bool {
	reader := bufio.NewReader(os.Stdin)
	line, _ := reader.ReadString('\n')
	line = strings.TrimSpace(strings.ToLower(line))
	return line == "y" || line == "yes"
}

// deriveOutputPaths는 -out/-user 값으로부터 상세/요약 CSV 두 경로를 만든다.
// -out 미지정 시 타임스탬프 기반 파일명을 만들고, -user 지정 시 확장자 앞에 "_<user>"를
// 끼워넣는다(예: result.csv -user=kdh -> result_kdh.csv). 이후 ".csv" 앞에 "_summary"를
// 끼워넣어 요약 CSV 경로를 만든다.
func deriveOutputPaths(out, user string) (detailPath, summaryPath string) {
	base := out
	if base == "" {
		base = fmt.Sprintf("vm-param-check_%s.csv", time.Now().Format("20060102_150405"))
	}
	base = withUserSuffix(base, user)
	detailPath = base
	if strings.HasSuffix(base, ".csv") {
		summaryPath = strings.TrimSuffix(base, ".csv") + "_summary.csv"
	} else {
		summaryPath = base + "_summary"
	}
	return detailPath, summaryPath
}

// deriveRecheckPaths는 재검증 CSV 경로를 만든다. -fixOut이 있으면 그대로 쓰고(사용자가
// 이미 원하는 이름을 정한 것이므로 -user를 다시 끼워넣지 않는다), 없으면 원본 상세 CSV
// 이름(이미 -user가 반영되어 있음) 기준으로 "_recheck_<타임스탬프>"를 붙인다.
func deriveRecheckPaths(originalDetailPath, fixOut string) (detailPath, summaryPath string) {
	base := fixOut
	if base == "" {
		ts := time.Now().Format("20060102_150405")
		if strings.HasSuffix(originalDetailPath, ".csv") {
			base = strings.TrimSuffix(originalDetailPath, ".csv") + "_recheck_" + ts + ".csv"
		} else {
			base = originalDetailPath + "_recheck_" + ts
		}
	}
	detailPath = base
	if strings.HasSuffix(base, ".csv") {
		summaryPath = strings.TrimSuffix(base, ".csv") + "_summary.csv"
	} else {
		summaryPath = base + "_summary"
	}
	return detailPath, summaryPath
}

func withUserSuffix(base, user string) string {
	if user == "" {
		return base
	}
	if strings.HasSuffix(base, ".csv") {
		return strings.TrimSuffix(base, ".csv") + "_" + user + ".csv"
	}
	return base + "_" + user
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

// parseIntListFlag는 쉼표로 구분된 정수 목록을 읽는다(디스크 허용값용).
// 값이 비면 nil을 돌려주므로, ev02/ev03에서는 그대로 "옵션 없음"이 된다.
func parseIntListFlag(flagName, val string) ([]int, error) {
	if val == "" {
		return nil, nil
	}
	var out []int
	for _, tok := range strings.Split(val, ",") {
		tok = strings.TrimSpace(tok)
		if tok == "" {
			return nil, fmt.Errorf("-%s 에 빈 값이 있습니다: %q", flagName, val)
		}
		v, err := strconv.Atoi(tok)
		if err != nil {
			return nil, fmt.Errorf("-%s 값이 정수가 아닙니다(%q): %w", flagName, tok, err)
		}
		out = append(out, v)
	}
	return out, nil
}

// parseSharesListFlag는 쉼표로 구분된 shares 허용값 목록을 읽는다(-shares-evNN용).
// 각 항목은 정수(ratio) 또는 'normal'일 수 있고, 섞어서 나열해도 된다(예: "4000,normal").
// 값이 비면 nil을 돌려주므로, ev02/ev03에서는 그대로 "옵션 없음"이 된다.
func parseSharesListFlag(flagName, val string) ([]checker.SharesItem, error) {
	if val == "" {
		return nil, nil
	}
	var out []checker.SharesItem
	for _, tok := range strings.Split(val, ",") {
		tok = strings.TrimSpace(tok)
		if tok == "" {
			return nil, fmt.Errorf("-%s 에 빈 값이 있습니다: %q", flagName, val)
		}
		if strings.EqualFold(tok, "normal") {
			out = append(out, checker.SharesItem{Normal: true})
			continue
		}
		v, err := strconv.Atoi(tok)
		if err != nil {
			return nil, fmt.Errorf("-%s 값은 ratio 숫자 또는 'normal' 이어야 합니다(%q): %w", flagName, tok, err)
		}
		out = append(out, checker.SharesItem{Ratio: v})
	}
	return out, nil
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
