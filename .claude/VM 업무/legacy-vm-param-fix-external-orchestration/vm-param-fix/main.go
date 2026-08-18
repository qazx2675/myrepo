// vm-param-fix: vm-param-check가 낸 상세 CSV의 FAIL/설정없음 항목을 태그(affinity/lpage/power)로
// 분류하고, 각 태그를 담당하는 기존 외부 도구(affinity_setting/lpage_setting/power_setting)를
// 실행해서 실제 vCenter 설정을 교정한다. (PLAN.md 참고)
//
// 이 도구 자체는 vCenter Reconfigure를 직접 하지 않는다 — 실행 전 안전장치(그룹 동질성 검증,
// VM 전원 OFF 검증, 사용자 확인)만 담당하고 실제 변경은 전부 외부 도구에 위임한다.
package main

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"flag"
)

func main() {
	checkResult := flag.String("checkResult", "", "vm-param-check가 낸 상세 CSV 경로 (필수)")
	vcTargetIP := flag.String("vcTargetIP", "", "vCenter 접속 IP (필수)")
	id := flag.String("id", "lscsystems@vsphere.local", "vCenter 로그인 계정 ID")

	affinityTool := flag.String("affinityTool", "./affinity_setting", "affinity 태그 담당 외부 도구 경로")
	lpageTool := flag.String("lpageTool", "./lpage_setting", "lpage 태그 담당 외부 도구 경로")
	powerTool := flag.String("powerTool", "./power_setting", "power 태그 담당 외부 도구 경로")
	recheckTool := flag.String("recheckTool", "./vm-param-check", "설정 적용 후 재검증에 쓸 vm-param-check 경로")

	affinityEV02File := flag.String("affinityFile", "", "ev02 affinity 기대값 파일 (ev02 affinity FAIL이 있을 때만 필요 — affinity_setting에 그대로 전달됨, ev01은 자동계산이라 불필요)")
	workDir := flag.String("workDir", ".", "생성되는 worklist 파일들을 저장할 디렉토리")
	out := flag.String("out", "", "재검증 CSV 출력 경로 (미지정 시 타임스탬프 자동생성)")

	scale := flag.Int("scale", 0, "테스트용: 실제 vCenter/CSV 없이 N대 규모의 합성 데이터로 태그분류/게이트/dry-run/병렬실행을 시뮬레이션 (가독성·규모 테스트 전용)")
	scaleMismatch := flag.Bool("scaleMismatch", false, "-scale과 함께 사용: VM 1대의 스펙을 일부러 다르게 만들어 동질성 게이트가 대량 환경에서도 잡아내는지 확인")

	flag.Parse()

	if *scale > 0 {
		runScale(*scale, *scaleMismatch, *affinityTool, *lpageTool, *powerTool)
		return
	}

	if *checkResult == "" || *vcTargetIP == "" {
		log.Fatal("-checkResult 와 -vcTargetIP 는 필수입니다")
	}
	password := os.Getenv("VC_PASSWORD")
	if password == "" {
		log.Fatal("인증 정보 로드 실패: VC_PASSWORD 환경 변수가 설정되지 않았습니다 (affinity_setting/lpage_setting/power_setting과 동일한 방식)")
	}

	rows, err := loadCSV(*checkResult)
	if err != nil {
		log.Fatalf("CSV 로드 실패: %v", err)
	}
	g, err := extractGlobalExpect(rows)
	if err != nil {
		log.Fatalf("전역 기대값 복원 실패: %v", err)
	}

	// 태그별로 FAIL/설정없음 항목만 모은다.
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

	if len(affinityVMs) == 0 && len(lpageVMs) == 0 && len(powerHosts) == 0 {
		fmt.Println("자동교정 대상 FAIL/설정없음 항목이 없습니다 (affinity/lpage/power 태그 기준). 종료합니다.")
		if manualCount > 0 {
			fmt.Printf("수동조치 필요 항목 %d건은 이 도구가 다루지 않습니다.\n", manualCount)
		}
		return
	}

	// affinity+lpage로 실제 Reconfigure 되는 VM 전체 집합 — 동질성/전원 검증 대상.
	targetVMs := map[string]bool{}
	for vm := range affinityVMs {
		targetVMs[vm] = true
	}
	for vm := range lpageVMs {
		targetVMs[vm] = true
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	client, err := connect(ctx, *vcTargetIP, *id, password)
	if err != nil {
		log.Fatalf("vCenter 접속 실패: %v", err)
	}
	defer func() { _ = client.Logout(ctx) }()

	specs, err := fetchVMSpecs(ctx, client, targetVMs)
	if err != nil {
		log.Fatalf("VM 스펙 조회 실패: %v", err)
	}

	// [게이트 1] 그룹 동질성 검증 + [게이트 2] 전원 OFF 검증은 서로 독립적이라 동시에 실행한다.
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

	if homogErr != nil {
		log.Fatalf("[동질성 검증 실패] %v", homogErr)
	}
	if powerErr != nil {
		log.Fatalf("[전원 OFF 검증 실패] %v", powerErr)
	}
	fmt.Println("[OK] 그룹 동질성 검증 통과 — 대상 VM 전부 동일 스펙")
	fmt.Println("[OK] 전원 OFF 검증 통과 — 대상 VM 전부 꺼져 있음")

	// worklist 파일 생성
	affinityWorklist := writeWorklist(*workDir, "worklist_affinity.txt", baseHostnames(affinityVMs))
	lpageWorklist := writeWorklist(*workDir, "worklist_lpage.txt", baseHostnames(lpageVMs))
	powerWorklist := writeWorklist(*workDir, "worklist_power.txt", sortedKeys(powerHosts))

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
		args := []string{"-id", *id, "-vcTargetIP", *vcTargetIP, "-worklistFile", affinityWorklist, "-ht", ht}
		if *affinityEV02File != "" {
			args = append(args, "-affinityFile", *affinityEV02File)
		} else if hasEV02(affinityVMs) {
			fmt.Println("[경고] ev02 affinity FAIL이 있는데 -affinityFile이 지정되지 않았습니다 — ev02는 이 실행에서 교정되지 않습니다(ev01 자동계산만 적용됨).")
		}
		planned = append(planned, plannedCmd{"affinity", *affinityTool, args})
	}
	if len(lpageVMs) > 0 {
		// lpage_setting은 ev01/ev02/ev03 전부 선택사항으로 받는다(실측 확인됨) — Cores를
		// 넘긴 그룹만 처리하고, 안 넘긴 그룹은 통째로 건너뛴다. 그래서 대상이 없는 그룹의
		// 플래그는 아예 넘기지 않는다(예전처럼 ev01 값으로 채워 넘기지 않는다).
		if g.Cores == 0 || g.CPU%g.Cores != 0 {
			log.Fatalf("lpage 교정 불가: ev01 vCPU(%d)가 코어수(%d)로 나누어떨어지지 않습니다", g.CPU, g.Cores)
		}
		sockets := strconv.Itoa(g.CPU / g.Cores)
		args := []string{"-id", *id, "-vcTargetIP", *vcTargetIP, "-worklistFile", lpageWorklist,
			"-ev01Cores", strconv.Itoa(g.CPU), "-ev01Sockets", sockets}
		if g.Numa > 0 {
			args = append(args, "-ev01Numa", strconv.Itoa(g.Numa))
		}

		if hasGroup(lpageVMs, "ev02") {
			// ev02는 vm-param-check에 -cores-ev02/-cpu-ev02 등을 줘야만 CSV에 값이 남는다.
			// ev01과 스펙이 다를 수 있으니(그게 이 기능을 넣은 이유) ev01 값을 대신 쓰면 안 된다.
			if g.CPUEV02 == nil || g.CoresEV02 == nil {
				log.Fatal("ev02 VM에 lpage FAIL이 있는데 CSV에 ev02용 vCPU/코어수 기대값이 없습니다 — vm-param-check를 -cores-ev02/-cpu-ev02와 함께 다시 돌려서 CSV를 새로 만드세요.")
			}
			if *g.CoresEV02 == 0 || *g.CPUEV02%*g.CoresEV02 != 0 {
				log.Fatalf("lpage 교정 불가: ev02 vCPU(%d)가 코어수(%d)로 나누어떨어지지 않습니다", *g.CPUEV02, *g.CoresEV02)
			}
			ev02Sockets := strconv.Itoa(*g.CPUEV02 / *g.CoresEV02)
			args = append(args, "-ev02Cores", strconv.Itoa(*g.CPUEV02), "-ev02Sockets", ev02Sockets)
			if g.NumaEV02 != nil && *g.NumaEV02 > 0 {
				args = append(args, "-ev02Numa", strconv.Itoa(*g.NumaEV02))
			}
		}

		if hasGroup(lpageVMs, "ev03") {
			// ev03도 ev02와 동일한 원칙: vm-param-check에 -cores-ev03/-cpu-ev03 등을
			// 줘야만 CSV에 값이 남고, ev01/ev02 값을 대신 쓰지 않는다.
			if g.CPUEV03 == nil || g.CoresEV03 == nil {
				log.Fatal("ev03 VM에 lpage FAIL이 있는데 CSV에 ev03용 vCPU/코어수 기대값이 없습니다 — vm-param-check를 -cores-ev03/-cpu-ev03와 함께 다시 돌려서 CSV를 새로 만드세요.")
			}
			if *g.CoresEV03 == 0 || *g.CPUEV03%*g.CoresEV03 != 0 {
				log.Fatalf("lpage 교정 불가: ev03 vCPU(%d)가 코어수(%d)로 나누어떨어지지 않습니다", *g.CPUEV03, *g.CoresEV03)
			}
			ev03Sockets := strconv.Itoa(*g.CPUEV03 / *g.CoresEV03)
			args = append(args, "-ev03Cores", strconv.Itoa(*g.CPUEV03), "-ev03Sockets", ev03Sockets)
			if g.NumaEV03 != nil && *g.NumaEV03 > 0 {
				args = append(args, "-ev03Numa", strconv.Itoa(*g.NumaEV03))
			}
		}
		planned = append(planned, plannedCmd{"lpage", *lpageTool, args})
	}
	if len(powerHosts) > 0 {
		args := []string{"-id", *id, "-vcTargetIP", *vcTargetIP, "-worklistFile", powerWorklist}
		planned = append(planned, plannedCmd{"power", *powerTool, args})
	}

	fmt.Println("\n=== 실행 예정 (dry-run) ===")
	for _, p := range planned {
		fmt.Printf("[%s] %s %s\n", p.tag, p.name, strings.Join(p.args, " "))
	}
	if manualCount > 0 {
		fmt.Printf("[manual] 자동교정 대상 아님(vCPU/메모리/디스크/Shares/네트워크 등) %d건 — 수동조치 필요\n", manualCount)
	}

	fmt.Print("\n위 내용대로 실제 설정을 변경하시겠습니까? (y/N): ")
	if !confirm() {
		fmt.Println("취소되었습니다. 아무 설정도 변경하지 않았습니다.")
		return
	}

	fmt.Println("\n=== 태그별 도구 실행 (병렬) ===")
	var runWG sync.WaitGroup
	results := make([]error, len(planned))
	for i, p := range planned {
		runWG.Add(1)
		go func(i int, p plannedCmd) {
			defer runWG.Done()
			results[i] = runTool(p.tag, p.name, p.args)
		}(i, p)
	}
	runWG.Wait()

	failed := false
	for i, err := range results {
		if err != nil {
			failed = true
			fmt.Printf("[%s] 실행 실패: %v\n", planned[i].tag, err)
		}
	}
	if failed {
		log.Fatal("일부 도구 실행이 실패했습니다 — 재검증을 건너뜁니다. 위 에러를 확인하세요.")
	}

	fmt.Println("\n=== 재검증: vm-param-check 재실행 ===")
	recheckTargets := writeWorklist(*workDir, "worklist_recheck.txt", allDistinctVMs(rows))
	// vm-param-check는 -f(지정 대상 모드)에서도 -vcenterList 파일이 필요해서, 우리가 쓴
	// -vcTargetIP 하나짜리 목록 파일을 여기서 만들어 넘긴다.
	recheckVCList := writeWorklist(*workDir, "vcenter_recheck.txt", []string{*vcTargetIP})
	recheckOut := *out
	if recheckOut == "" {
		recheckOut = fmt.Sprintf("vm-param-fix_recheck_%s.csv", time.Now().Format("20060102_150405"))
	}
	recheckArgs := []string{
		"-f", recheckTargets,
		"-vcenterList", recheckVCList,
		"--ht=" + boolToOnOff(g.HTOn),
		"--cores=" + strconv.Itoa(g.Cores),
		"--numa=" + strconv.Itoa(g.Numa),
		"--cpu=" + strconv.Itoa(g.CPU),
		"--mem=" + strconv.Itoa(g.MemGB),
		"--disk=" + strconv.Itoa(g.DiskGB),
		"--shares-ev01=" + strconv.Itoa(g.SharesEV01),
		"--out=" + recheckOut,
	}
	if g.SharesEV02 != nil {
		recheckArgs = append(recheckArgs, "--shares-ev02="+strconv.Itoa(*g.SharesEV02))
	}
	// ev02/ev03 전용 하드웨어 기대값도 원래 CSV에 있었으면 재검증에 그대로 실어 보낸다 —
	// 안 실으면 재검증이 그 그룹 항목들을 전부 스킵해버려서 실제로 교정됐는지 확인이 안 된다.
	appendEVArg := func(flag string, v *int) {
		if v != nil {
			recheckArgs = append(recheckArgs, flag+"="+strconv.Itoa(*v))
		}
	}
	appendEVArg("--cores-ev02", g.CoresEV02)
	appendEVArg("--numa-ev02", g.NumaEV02)
	appendEVArg("--cpu-ev02", g.CPUEV02)
	appendEVArg("--mem-ev02", g.MemGBEV02)
	appendEVArg("--disk-ev02", g.DiskGBEV02)
	appendEVArg("--cores-ev03", g.CoresEV03)
	appendEVArg("--numa-ev03", g.NumaEV03)
	appendEVArg("--cpu-ev03", g.CPUEV03)
	appendEVArg("--mem-ev03", g.MemGBEV03)
	appendEVArg("--disk-ev03", g.DiskGBEV03)
	// vm-param-check는 VC_USER/VC_PASS(또는 VCENTER_USER/VCENTER_PASS)를 쓰는데 이 도구는
	// affinity_setting/lpage_setting/power_setting과 맞추려고 VC_PASSWORD+-id를 쓰므로,
	// 재검증 호출 시에만 그 값을 VC_USER/VC_PASS로 변환해 넘긴다.
	recheckEnv := append(os.Environ(), "VC_USER="+*id, "VC_PASS="+password)
	if err := runToolWithEnv("recheck", *recheckTool, recheckArgs, recheckEnv); err != nil {
		log.Fatalf("재검증 실행 실패: %v", err)
	}
	fmt.Printf("\n재검증 완료: %s 확인하세요.\n", recheckOut)
}

func hasEV02(vms map[string]bool) bool {
	return hasGroup(vms, "ev02")
}

// hasGroup은 vms 중 하나라도 groupOf(vm)이 group인 게 있는지 본다.
func hasGroup(vms map[string]bool, group string) bool {
	for vm := range vms {
		if groupOf(vm) == group {
			return true
		}
	}
	return false
}

func baseHostnames(vms map[string]bool) []string {
	seen := map[string]bool{}
	var out []string
	for vm := range vms {
		b := baseHostname(vm)
		if !seen[b] {
			seen[b] = true
			out = append(out, b)
		}
	}
	sort.Strings(out)
	return out
}

func allDistinctVMs(rows []Row) []string {
	seen := map[string]bool{}
	var out []string
	for _, r := range rows {
		if !seen[r.VM] {
			seen[r.VM] = true
			out = append(out, r.VM)
		}
	}
	sort.Strings(out)
	return out
}

func sortedKeys(m map[string]bool) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func writeWorklist(dir, name string, lines []string) string {
	path := dir + string(os.PathSeparator) + name
	f, err := os.Create(path)
	if err != nil {
		log.Fatalf("%s 생성 실패: %v", path, err)
	}
	defer f.Close()
	for _, l := range lines {
		fmt.Fprintln(f, l)
	}
	return path
}

func confirm() bool {
	reader := bufio.NewReader(os.Stdin)
	line, _ := reader.ReadString('\n')
	line = strings.TrimSpace(strings.ToLower(line))
	return line == "y" || line == "yes"
}

func runTool(tag, name string, args []string) error {
	return runToolWithEnv(tag, name, args, nil)
}

func runToolWithEnv(tag, name string, args []string, env []string) error {
	cmd := exec.Command(name, args...)
	cmd.Env = env
	cmd.Stdout = prefixedWriter{tag: tag, w: os.Stdout}
	cmd.Stderr = prefixedWriter{tag: tag, w: os.Stderr}
	fmt.Printf("[%s] 실행: %s %s\n", tag, name, strings.Join(args, " "))
	return cmd.Run()
}

// prefixedWriter는 병렬로 여러 도구를 동시 실행할 때 출력이 섞여도 어느 도구 출력인지
// 구분되도록 각 줄 앞에 [tag]를 붙인다.
type prefixedWriter struct {
	tag string
	w   *os.File
}

func (p prefixedWriter) Write(b []byte) (int, error) {
	for _, line := range strings.Split(strings.TrimRight(string(b), "\n"), "\n") {
		fmt.Fprintf(p.w, "[%s] %s\n", p.tag, line)
	}
	return len(b), nil
}

func boolToOnOff(b bool) string {
	if b {
		return "on"
	}
	return "off"
}
