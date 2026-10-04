// numa_preferht_setting: -f로 지정한 VM 이름 목록에 numa.vcpu.preferHT=TRUE를 병렬(워커풀)로
// 적용한다. 모든 VM에 공통 적용되는 단일 설정값이라 ev01/ev02/ev03 같은 그룹 구분이 없다.
// 대상 VM이 켜져 있으면 적용하지 않고 건너뛴다(NUMA 관련 설정은 꺼진 상태에서만 반영이
// 보장되는 값이라, 전원 상태를 명령 실행 전에 반드시 확인한다).

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
	"sync"

	"github.com/vmware/govmomi"
	"github.com/vmware/govmomi/find"
	"github.com/vmware/govmomi/object"
	"github.com/vmware/govmomi/property"
	"github.com/vmware/govmomi/vim25/mo"
	"github.com/vmware/govmomi/vim25/types"
)

const defaultConcurrency = 20
const preferHTKey = "numa.vcpu.preferHT"
const preferHTValue = "TRUE"

// vmJob: 워커풀에 넘기는 작업 단위 (VM 1대 = Reconfigure 전송 + Wait 를 한 워커가 전부 처리)
type vmJob struct {
	vmName string
	vm     *object.VirtualMachine
}

// vmResult: 워커풀 처리 결과 (성공/실패/스킵 집계 및 재조회 검증용)
type vmResult struct {
	vmName  string
	skipped bool
	failed  bool
}

// printMu: 여러 고루틴이 동시에 fmt.Printf 를 호출할 때 줄이 섞이지 않도록 보호
var printMu sync.Mutex

func safePrintf(format string, args ...interface{}) {
	printMu.Lock()
	defer printMu.Unlock()
	fmt.Printf(format, args...)
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

// resolvePath: 절대경로면 그대로, 상대경로면 baseDir 기준
func resolvePath(baseDir, p string) string {
	if filepath.IsAbs(p) {
		return p
	}
	return filepath.Join(baseDir, p)
}

func main() {
	vcId := flag.String("id", "lscsystems@vsphere.local", "vCenter 로그인 계정 ID")
	vcTargetIP := flag.String("vc", "", "vCenter 접속 IP (필수)")
	targetsFile := flag.String("f", "", "대상 VM 이름 목록 파일 (한 줄에 하나, '#' 주석 가능, 필수)")
	concurrency := flag.Int("concurrency", defaultConcurrency, "동시 처리 개수 제한 (VM 목록 조회 / Reconfigure 전송+대기 전 구간에 적용)")

	flag.Parse()

	if *vcTargetIP == "" {
		log.Fatal("필수 파라미터가 누락되었습니다. (-vc 확인)")
	}
	if strings.TrimSpace(*targetsFile) == "" {
		log.Fatal("필수 파라미터가 누락되었습니다. (-f 확인)")
	}

	vcPassword := os.Getenv("VC_PASSWORD")
	if vcPassword == "" {
		log.Fatal("인증 정보 로드 실패: VC_PASSWORD 환경 변수가 설정되지 않았습니다.")
	}

	baseDir, err := os.Getwd()
	if err != nil {
		baseDir = "."
	}

	targetsPath := resolvePath(baseDir, *targetsFile)
	if _, statErr := os.Stat(targetsPath); os.IsNotExist(statErr) {
		log.Fatalf("%s 파일을 찾을 수 없습니다: %s", *targetsFile, targetsPath)
	}
	vmNames, err := readLines(targetsPath)
	if err != nil {
		log.Fatalf("%s 파일 읽기 실패: %v", *targetsFile, err)
	}
	if len(vmNames) == 0 {
		log.Fatalf("%s 에 대상 VM 이름이 없습니다.", *targetsFile)
	}

	fmt.Printf("병렬(워커풀) 방식으로 %s=%s 일괄 적용을 시작합니다.\n", preferHTKey, preferHTValue)
	fmt.Printf("  접속 계정 : %s\n", *vcId)
	fmt.Printf("  vCenter   : %s\n", *vcTargetIP)
	fmt.Printf("  대상 VM   : %d대 (동시 처리 %d개 제한)\n", len(vmNames), *concurrency)
	fmt.Printf("  조건      : 전원이 꺼져 있는 VM에만 적용 (켜져 있으면 스킵)\n\n")

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

	// ---- 대상 VM 이름 집합 생성 (worklist+접미사 조합 없이 -f에 적힌 이름 그대로 사용) ----
	safeVmNames := make([]string, 0, len(vmNames))
	for _, name := range vmNames {
		clean := strings.TrimSpace(name)
		if clean == "" {
			continue
		}
		safeVmNames = append(safeVmNames, regexp.QuoteMeta(clean))
	}
	if len(safeVmNames) == 0 {
		log.Fatal("생성된 대상 VM 이름이 없습니다. -f 파일 내용을 확인하세요.")
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
		log.Fatal("-f 목록과 매칭되는 VM 을 vCenter 에서 찾지 못했습니다.")
	}

	// ---- 전원 상태 배치 조회 (단일 배치 호출로 대상 VM 전부의 켜짐/꺼짐 여부를 한 번에 확인) ----
	refs := make([]types.ManagedObjectReference, 0, len(targetVmMap))
	for _, vm := range targetVmMap {
		refs = append(refs, vm.Reference())
	}
	pc := property.DefaultCollector(client.Client)
	var powerProps []mo.VirtualMachine
	if retErr := pc.Retrieve(ctx, refs, []string{"name", "runtime.powerState"}, &powerProps); retErr != nil {
		log.Fatalf("전원 상태 배치 조회 실패: %v", retErr)
	}
	poweredOffSet := make(map[string]bool, len(powerProps))
	for _, vp := range powerProps {
		poweredOffSet[vp.Name] = vp.Runtime.PowerState == types.VirtualMachinePowerStatePoweredOff
	}

	// ---- 작업 목록 구성 ----
	var jobs []vmJob
	var preSkipped int
	for _, name := range vmNames {
		clean := strings.TrimSpace(name)
		if clean == "" {
			continue
		}
		vm, ok := targetVmMap[clean]
		if !ok {
			safePrintf("[%s] 경고: 대상 VM 이 존재하지 않습니다. (PASS)\n", clean)
			preSkipped++
			continue
		}
		if !poweredOffSet[clean] {
			safePrintf("[%s] 전원 ON 상태 — 스킵 (PASS)\n", clean)
			preSkipped++
			continue
		}
		jobs = append(jobs, vmJob{vmName: clean, vm: vm})
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

			spec := types.VirtualMachineConfigSpec{
				ExtraConfig: []types.BaseOptionValue{
					&types.OptionValue{Key: preferHTKey, Value: preferHTValue},
				},
			}

			task, taskErr := j.vm.Reconfigure(ctx, spec)
			if taskErr != nil {
				safePrintf("[%s] Reconfigure 명령 전송 실패: %v\n", j.vmName, taskErr)
				results <- vmResult{vmName: j.vmName, skipped: true}
				return
			}

			safePrintf("[%s] %s=%s 병렬 설정 명령 전송 완료\n", j.vmName, preferHTKey, preferHTValue)

			if waitErr := task.Wait(ctx); waitErr != nil {
				safePrintf("[%s] 작업 실패: %v\n", j.vmName, waitErr)
				results <- vmResult{vmName: j.vmName, failed: true}
				return
			}

			results <- vmResult{vmName: j.vmName}
		}(j)
	}

	jobWg.Wait()
	close(results)

	var failed, skipped int
	successVmNames := make([]string, 0, len(jobs))
	for r := range results {
		switch {
		case r.skipped:
			skipped++
		case r.failed:
			failed++
		default:
			successVmNames = append(successVmNames, r.vmName)
		}
	}
	skipped += preSkipped

	// ---- 실제 적용여부 배치 검증 (config.extraConfig 재조회, Task 성공분만 대상, 단일 배치 호출) ----
	var mismatched int
	if len(successVmNames) > 0 {
		verifyRefs := make([]types.ManagedObjectReference, 0, len(successVmNames))
		for _, name := range successVmNames {
			if vm, ok := targetVmMap[name]; ok {
				verifyRefs = append(verifyRefs, vm.Reference())
			}
		}

		var verifyProps []mo.VirtualMachine
		if retErr := pc.Retrieve(ctx, verifyRefs, []string{"name", "config.extraConfig"}, &verifyProps); retErr != nil {
			fmt.Printf("경고: 실제 적용여부 재조회 실패 (%v) - 적용 확인을 건너뜁니다.\n", retErr)
		} else {
			actualMap := make(map[string]string, len(verifyProps))
			for _, vp := range verifyProps {
				if vp.Config == nil {
					continue
				}
				for _, ov := range vp.Config.ExtraConfig {
					if opt, ok := ov.(*types.OptionValue); ok && opt.Key == preferHTKey {
						actualMap[vp.Name] = fmt.Sprintf("%v", opt.Value)
					}
				}
			}

			for _, name := range successVmNames {
				actual, ok := actualMap[name]
				if !ok || actual != preferHTValue {
					fmt.Printf("[%s] 실제 적용 불일치: %s(기대=%s,실제=%s)\n", name, preferHTKey, preferHTValue, actual)
					mismatched++
				} else {
					fmt.Printf("[%s] 실제 적용 확인 완료 (%s=%s)\n", name, preferHTKey, actual)
				}
			}
		}
	}

	fmt.Printf("\n완료: 성공 %d / 실패 %d / 스킵 %d / 적용불일치 %d\n",
		len(successVmNames)-mismatched, failed, skipped, mismatched)
	if failed > 0 || skipped > 0 || mismatched > 0 {
		os.Exit(2)
	}
	fmt.Println("모든 VM 의 numa.vcpu.preferHT 설정이 정상 적용되었습니다 (재조회로 검증 완료).")
}
