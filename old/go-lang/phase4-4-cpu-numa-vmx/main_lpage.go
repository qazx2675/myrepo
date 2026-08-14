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

	"github.com/vmware/govmomi"
	"github.com/vmware/govmomi/find"
	"github.com/vmware/govmomi/object"
	"github.com/vmware/govmomi/vim25/types"
)

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

// main_lpage.go - Phase4-4 HugePage/CPU 토폴로지(소켓당 코어)+NUMA 노드 성능 파라미터 비동기 일괄 주입
func main() {
	vcId := flag.String("id", "lscsystems@vsphere.local", "vCenter 로그인 계정 ID")
	vcTargetIP := flag.String("vcTargetIP", "", "vCenter 접속 IP (필수)")
	worklistFile := flag.String("worklistFile", "worklist.txt", "작업 대상 호스트 목록 파일")
	ev01Cores := flag.Int("ev01Cores", 0, "[ev01] 코어 수")
	ev01Sockets := flag.Int("ev01Sockets", 0, "[ev01] 소켓 수")
	ev02Cores := flag.Int("ev02Cores", 0, "[ev02] 코어 수")
	ev02Sockets := flag.Int("ev02Sockets", 0, "[ev02] 소켓 수")
	ev01Numa := flag.Int("ev01Numa", 0, "[ev01] NUMA 노드 수 (0이면 생략)")
	ev02Numa := flag.Int("ev02Numa", 0, "[ev02] NUMA 노드 수 (0이면 생략)")
	applyTopology := flag.Bool("applyTopology", true, "CPU 토폴로지(소켓당 코어/NUMA) 적용 여부")
	flag.Parse()

	if *vcTargetIP == "" || *ev01Cores == 0 || *ev01Sockets == 0 || *ev02Cores == 0 || *ev02Sockets == 0 {
		log.Fatal("필수 파라미터가 누락되었습니다.")
	}
	if *ev01Cores%*ev01Sockets != 0 || *ev02Cores%*ev02Sockets != 0 {
		log.Fatal("코어 수가 소켓 수로 나누어떨어지지 않습니다.")
	}

	vcPassword := os.Getenv("VC_PASSWORD")
	if vcPassword == "" {
		log.Fatal("인증 정보 로드 실패: VC_PASSWORD 환경 변수가 설정되지 않았습니다.")
	}
	baseDir, _ := os.Getwd()
	hostlistLines, err := readLines(filepath.Join(baseDir, *worklistFile))
	if err != nil {
		log.Fatalf("worklist 파일 읽기 실패: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	u := &url.URL{Scheme: "https", Host: *vcTargetIP, Path: "/sdk"}
	u.User = url.UserPassword(*vcId, vcPassword)
	client, err := govmomi.NewClient(ctx, u, true)
	if err != nil {
		log.Fatalf("작업 중 오류 발생: %v", err)
	}
	defer func() { _ = client.Logout(ctx) }()

	finder := find.NewFinder(client.Client, true)
	dc, _ := finder.DefaultDatacenter(ctx)
	finder.SetDatacenter(dc)

	ev01CoresPerSocket, ev02CoresPerSocket := *ev01Cores / *ev01Sockets, *ev02Cores / *ev02Sockets
	ev01CoresPerNuma, ev02CoresPerNuma := 0, 0
	if *ev01Numa > 0 {
		ev01CoresPerNuma = *ev01Cores / *ev01Numa
	}
	if *ev02Numa > 0 {
		ev02CoresPerNuma = *ev02Cores / *ev02Numa
	}

	var safeVmNames []string
	for _, baseHost := range hostlistLines {
		cleanHost := strings.TrimSpace(baseHost)
		safeVmNames = append(safeVmNames, regexp.QuoteMeta(cleanHost+"ev01"), regexp.QuoteMeta(cleanHost+"ev02"))
	}
	regexMatcher := regexp.MustCompile("^(" + strings.Join(safeVmNames, "|") + ")$")
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

	buildSpec := func(cores, coresPerSocket, coresPerNuma int) types.VirtualMachineConfigSpec {
		spec := types.VirtualMachineConfigSpec{}
		coresStr := strconv.Itoa(coresPerSocket)
		spec.ExtraConfig = []types.BaseOptionValue{
			&types.OptionValue{Key: "sched.mem.lpage.enable1GPage", Value: "TRUE"},
			&types.OptionValue{Key: "sched.mem.pin", Value: "TRUE"},
			&types.OptionValue{Key: "sched.mem.prealloc", Value: "TRUE"},
			&types.OptionValue{Key: "sched.mem.prealloc.pinnedMainMem", Value: "TRUE"},
			&types.OptionValue{Key: "sched.swap.vmxSwapEnabled", Value: "FALSE"},
			&types.OptionValue{Key: "numa.vcpu.maxPerVirtualNode", Value: coresStr},
		}
		if *applyTopology {
			spec.NumCPUs = int32(cores)
			cps := int32(coresPerSocket)
			spec.NumCoresPerSocket = &cps
			if coresPerNuma > 0 {
				cpn := int32(coresPerNuma)
				spec.VirtualNuma = &types.VirtualMachineVirtualNuma{CoresPerNumaNode: &cpn}
			}
		}
		return spec
	}

	var allTasks []*object.Task
	for _, baseHost := range hostlistLines {
		cleanHost := strings.TrimSpace(baseHost)
		if cleanHost == "" {
			continue
		}
		if vm01, ok := targetVmMap[cleanHost+"ev01"]; ok {
			if task, err := vm01.Reconfigure(ctx, buildSpec(*ev01Cores, ev01CoresPerSocket, ev01CoresPerNuma)); err == nil {
				allTasks = append(allTasks, task)
			}
		}
		if vm02, ok := targetVmMap[cleanHost+"ev02"]; ok {
			if task, err := vm02.Reconfigure(ctx, buildSpec(*ev02Cores, ev02CoresPerSocket, ev02CoresPerNuma)); err == nil {
				allTasks = append(allTasks, task)
			}
		}
	}
	for _, task := range allTasks {
		_ = task.Wait(ctx)
	}
	fmt.Println("모든 VM의 VMX 성능 파라미터가 완벽하게 주입되었습니다!")
}
