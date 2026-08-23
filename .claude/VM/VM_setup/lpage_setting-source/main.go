// vm_lpage_bulk: worklist 기반으로 ev01~ev03 VM에 HugePage/CPU 토폴로지(소켓당 코어/NUMA) 설정을
// 병렬(워커풀) 적용. ev01/ev02/ev03는 전부 선택사항이며, Cores를 지정한 그룹만 처리한다.
// -concurrency 로 동시 처리 개수 제어

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
	"github.com/vmware/govmomi/vim25/types"
)

const defaultConcurrency = 20

// evGroup: ev01/ev02/ev03 그룹 하나의 설정. cores==0이면 이 그룹은 아예 처리하지 않는다(선택사항).
type evGroup struct {
	suffix           string // "ev01" | "ev02" | "ev03"
	cores            int
	sockets          int
	numa             int
	coresPerSocket   int
	coresPerNumaNode int
}

// vmJob: 워커풀에 넘기는 작업 단위 (VM 1대 = Reconfigure 전송 + Wait 를 한 워커가 전부 처리)
type vmJob struct {
	vmName           string
	vm               *object.VirtualMachine
	cores            int
	coresPerSocket   int
	numa             int
	coresPerNumaNode int
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
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line != "" && !strings.HasPrefix(line, "#") {
			lines = append(lines, line)
		}
	}
	return lines, scanner.Err()
}

func main() {
	vcId := flag.String("id", "lscsystems@vsphere.local", "vCenter 로그인 계정 ID")
	vcTargetIP := flag.String("vcTargetIP", "", "vCenter 접속 IP (필수)")
	worklistFile := flag.String("worklistFile", "worklist.txt", "작업 대상 호스트 목록 파일")

	ev01Cores := flag.Int("ev01Cores", 0, "[ev01] 코어 수 (미지정 시 ev01 그룹은 처리하지 않음)")
	ev01Sockets := flag.Int("ev01Sockets", 0, "[ev01] 소켓 수 (-ev01Cores 지정 시 필수)")
	ev01Numa := flag.Int("ev01Numa", 0, "[ev01] NUMA 노드 수 (0이면 토폴로지 NUMA 설정 생략)")

	ev02Cores := flag.Int("ev02Cores", 0, "[ev02] 코어 수 (미지정 시 ev02 그룹은 처리하지 않음)")
	ev02Sockets := flag.Int("ev02Sockets", 0, "[ev02] 소켓 수 (-ev02Cores 지정 시 필수)")
	ev02Numa := flag.Int("ev02Numa", 0, "[ev02] NUMA 노드 수 (0이면 토폴로지 NUMA 설정 생략)")

	ev03Cores := flag.Int("ev03Cores", 0, "[ev03] 코어 수 (미지정 시 ev03 그룹은 처리하지 않음)")
	ev03Sockets := flag.Int("ev03Sockets", 0, "[ev03] 소켓 수 (-ev03Cores 지정 시 필수)")
	ev03Numa := flag.Int("ev03Numa", 0, "[ev03] NUMA 노드 수 (0이면 토폴로지 NUMA 설정 생략)")

	applyTopology := flag.Bool("applyTopology", true, "설정 편집 > CPU 토폴로지(소켓당 코어 수/NUMA 노드) 적용 여부")
	concurrency := flag.Int("concurrency", defaultConcurrency, "동시 처리 개수 제한 (Reconfigure 전송+대기 전 구간에 적용)")

	flag.Parse()

	if *vcTargetIP == "" {
		log.Fatal("필수 파라미터가 누락되었습니다. (-vcTargetIP 확인)")
	}
	if *concurrency < 1 {
		log.Fatalf("-concurrency 값이 올바르지 않습니다: %d (1 이상)", *concurrency)
	}

	rawGroups := []evGroup{
		{suffix: "ev01", cores: *ev01Cores, sockets: *ev01Sockets, numa: *ev01Numa},
		{suffix: "ev02", cores: *ev02Cores, sockets: *ev02Sockets, numa: *ev02Numa},
		{suffix: "ev03", cores: *ev03Cores, sockets: *ev03Sockets, numa: *ev03Numa},
	}

	// ev01/ev02/ev03 전부 선택사항 — Cores를 안 주면 그 그룹은 통째로 건너뛴다(필수 입력값 아님).
	var groups []evGroup
	for _, g := range rawGroups {
		if g.cores == 0 {
			continue
		}
		if g.sockets == 0 {
			log.Fatalf("[%s] -%sCores를 지정했으면 -%sSockets도 필요합니다.", g.suffix, g.suffix, g.suffix)
		}
		if g.cores%g.sockets != 0 {
			log.Fatalf("[%s] 코어 수(%d)가 소켓 수(%d)로 나누어떨어지지 않습니다.", g.suffix, g.cores, g.sockets)
		}
		if g.numa > 0 && g.cores%g.numa != 0 {
			log.Fatalf("[%s] 코어 수(%d)가 NUMA 노드 수(%d)로 나누어떨어지지 않습니다.", g.suffix, g.cores, g.numa)
		}
		g.coresPerSocket = g.cores / g.sockets
		if g.numa > 0 {
			g.coresPerNumaNode = g.cores / g.numa
		}
		groups = append(groups, g)
	}

	if len(groups) == 0 {
		log.Fatal("최소 하나의 그룹은 지정해야 합니다 (-ev01Cores / -ev02Cores / -ev03Cores 중 하나 이상 + 대응하는 Sockets).")
	}

	vcPassword := os.Getenv("VC_PASSWORD")
	if vcPassword == "" {
		log.Fatal("인증 정보 로드 실패: VC_PASSWORD 환경 변수가 설정되지 않았습니다.")
	}

	baseDir, _ := os.Getwd()
	worklistPath := filepath.Join(baseDir, *worklistFile)

	if _, err := os.Stat(worklistPath); os.IsNotExist(err) {
		log.Fatalf("%s 파일을 찾을 수 없습니다.", *worklistFile)
	}

	hostlistLines, err := readLines(worklistPath)
	if err != nil {
		log.Fatalf("worklist 파일 읽기 실패: %v", err)
	}

	groupNames := make([]string, len(groups))
	for i, g := range groups {
		groupNames[i] = g.suffix
	}
	fmt.Printf("병렬(워커풀) 방식 성능 최적화(VMX) 파라미터 주입을 시작합니다. (접속 계정: %s, 대상 그룹: %s, 동시 처리 제한: %d)\n",
		*vcId, strings.Join(groupNames, ","), *concurrency)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	u := &url.URL{Scheme: "https", Host: *vcTargetIP, Path: "/sdk"}
	u.User = url.UserPassword(*vcId, vcPassword)

	client, err := govmomi.NewClient(ctx, u, true)
	if err != nil {
		log.Fatalf("작업 중 오류 발생: %v", err)
	}
	defer func() {
		_ = client.Logout(ctx)
	}()

	finder := find.NewFinder(client.Client, true)
	dc, _ := finder.DefaultDatacenter(ctx)
	finder.SetDatacenter(dc)

	var safeVmNames []string
	for _, baseHost := range hostlistLines {
		cleanHost := strings.TrimSpace(baseHost)
		if cleanHost == "" {
			continue
		}
		for _, g := range groups {
			safeVmNames = append(safeVmNames, regexp.QuoteMeta(cleanHost+g.suffix))
		}
	}

	if len(safeVmNames) == 0 {
		fmt.Println("\n실행할 작업이 없습니다.")
		return
	}

	vmFilterPattern := "^(" + strings.Join(safeVmNames, "|") + ")$"
	regexMatcher := regexp.MustCompile(vmFilterPattern)

	allVms, err := finder.VirtualMachineList(ctx, "*")
	if err != nil {
		log.Fatalf("VM 목록 조회 중 오류 발생: %v", err)
	}

	targetVmMap := make(map[string]*object.VirtualMachine)
	for _, vm := range allVms {
		if regexMatcher.MatchString(vm.Name()) {
			targetVmMap[vm.Name()] = vm
		}
	}

	// ---- 작업 목록 구성 (호스트 x 지정된 그룹만 평탄화) ----
	var jobs []vmJob
	for _, baseHost := range hostlistLines {
		cleanHost := strings.TrimSpace(baseHost)
		if cleanHost == "" {
			continue
		}

		foundAny := false
		for _, g := range groups {
			vmName := cleanHost + g.suffix
			vm, ok := targetVmMap[vmName]
			if !ok {
				continue
			}
			foundAny = true
			jobs = append(jobs, vmJob{
				vmName: vmName, vm: vm,
				cores: g.cores, coresPerSocket: g.coresPerSocket,
				numa: g.numa, coresPerNumaNode: g.coresPerNumaNode,
			})
		}
		if !foundAny {
			safePrintf("[%s] 경고: 대상 VM이 존재하지 않습니다. (PASS)\n", cleanHost)
		}
	}

	if len(jobs) == 0 {
		fmt.Println("\n실행할 작업이 없습니다.")
		return
	}

	fmt.Printf("\n총 %d개의 성능 최적화 작업을 동시 %d개 제한으로 처리합니다 (전송+완료대기를 워커 단위로 병렬 수행).\n", len(jobs), *concurrency)

	// ---- 워커풀: 각 워커가 Reconfigure 전송 + Wait 완료까지 한 VM 단위로 전부 처리 ----
	sem := make(chan struct{}, *concurrency)
	var wg sync.WaitGroup
	var mu sync.Mutex
	var succeeded, failedCnt int

	for _, j := range jobs {
		wg.Add(1)
		sem <- struct{}{}
		go func(j vmJob) {
			defer wg.Done()
			defer func() { <-sem }()

			spec := types.VirtualMachineConfigSpec{}
			coresStr := strconv.Itoa(j.coresPerSocket)
			spec.ExtraConfig = []types.BaseOptionValue{
				&types.OptionValue{Key: "sched.mem.lpage.enable1GPage", Value: "TRUE"},
				&types.OptionValue{Key: "sched.mem.pin", Value: "TRUE"},
				&types.OptionValue{Key: "sched.mem.prealloc", Value: "TRUE"},
				&types.OptionValue{Key: "sched.mem.prealloc.pinnedMainMem", Value: "TRUE"},
				&types.OptionValue{Key: "sched.swap.vmxSwapEnabled", Value: "FALSE"},
				&types.OptionValue{Key: "numa.vcpu.maxPerVirtualNode", Value: coresStr},
			}

			topoMsg := ""
			if *applyTopology {
				spec.NumCPUs = int32(j.cores)
				cps := int32(j.coresPerSocket)
				spec.NumCoresPerSocket = &cps
				topoMsg = fmt.Sprintf(", 토폴로지(vCPU %d / 소켓당 코어 %d", j.cores, j.coresPerSocket)

				if j.coresPerNumaNode > 0 {
					cpn := int32(j.coresPerNumaNode)
					spec.VirtualNuma = &types.VirtualMachineVirtualNuma{
						CoresPerNumaNode: &cpn,
					}
					topoMsg += fmt.Sprintf(" / NUMA 노드 %d", j.numa)
				}
				topoMsg += ")"
			}

			task, taskErr := j.vm.Reconfigure(ctx, spec)
			if taskErr != nil {
				safePrintf("[%s] 설정 요청 실패: %v\n", j.vmName, taskErr)
				mu.Lock()
				failedCnt++
				mu.Unlock()
				return
			}

			safePrintf("[%s] HugePage 및 코어당 소켓(%s)%s 병렬 주입 요청 전송 완료\n", j.vmName, coresStr, topoMsg)

			if waitErr := task.Wait(ctx); waitErr != nil {
				safePrintf("[%s] 작업 실패: %v\n", j.vmName, waitErr)
				mu.Lock()
				failedCnt++
				mu.Unlock()
				return
			}

			safePrintf("[%s] 완료 확인됨\n", j.vmName)
			mu.Lock()
			succeeded++
			mu.Unlock()
		}(j)
	}

	wg.Wait()

	fmt.Printf("\n완료: 성공 %d / 실패 %d\n", succeeded, failedCnt)
	if failedCnt > 0 {
		os.Exit(2)
	}
	fmt.Println("모든 VM의 VMX 성능 파라미터가 완벽하게 주입되었습니다!")
}
