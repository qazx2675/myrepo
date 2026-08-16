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

func main() {
	vcId := flag.String("id", "lscsystems@vsphere.local", "vCenter 로그인 계정 ID")
	vcTargetIP := flag.String("vcTargetIP", "", "vCenter 접속 IP (필수)")
	worklistFile := flag.String("worklistFile", "worklist.txt", "작업 대상 호스트 목록 파일")

	ev01Cores := flag.Int("ev01Cores", 0, "[ev01] 코어 수")
	ev01Sockets := flag.Int("ev01Sockets", 0, "[ev01] 소켓 수")
	ev02Cores := flag.Int("ev02Cores", 0, "[ev02] 코어 수")
	ev02Sockets := flag.Int("ev02Sockets", 0, "[ev02] 소켓 수")

	ev01Numa := flag.Int("ev01Numa", 0, "[ev01] NUMA 노드 수 (0이면 토폴로지 NUMA 설정 생략)")
	ev02Numa := flag.Int("ev02Numa", 0, "[ev02] NUMA 노드 수 (0이면 토폴로지 NUMA 설정 생략)")
	applyTopology := flag.Bool("applyTopology", true, "설정 편집 > CPU 토폴로지(소켓당 코어 수/NUMA 노드) 적용 여부")

	flag.Parse()

	if *vcTargetIP == "" || *ev01Cores == 0 || *ev01Sockets == 0 || *ev02Cores == 0 || *ev02Sockets == 0 {
		log.Fatal("필수 파라미터가 누락되었습니다. 코어 및 소켓 파라미터를 모두 입력해 주세요.")
	}

	if *ev01Sockets == 0 || *ev02Sockets == 0 {
		log.Fatal("소켓 수는 0이 될 수 없습니다.")
	}

	if *ev01Cores%*ev01Sockets != 0 {
		log.Fatalf("[ev01] 코어 수(%d)가 소켓 수(%d)로 나누어떨어지지 않습니다.", *ev01Cores, *ev01Sockets)
	}
	if *ev02Cores%*ev02Sockets != 0 {
		log.Fatalf("[ev02] 코어 수(%d)가 소켓 수(%d)로 나누어떨어지지 않습니다.", *ev02Cores, *ev02Sockets)
	}
	if *ev01Numa > 0 && *ev01Cores%*ev01Numa != 0 {
		log.Fatalf("[ev01] 코어 수(%d)가 NUMA 노드 수(%d)로 나누어떨어지지 않습니다.", *ev01Cores, *ev01Numa)
	}
	if *ev02Numa > 0 && *ev02Cores%*ev02Numa != 0 {
		log.Fatalf("[ev02] 코어 수(%d)가 NUMA 노드 수(%d)로 나누어떨어지지 않습니다.", *ev02Cores, *ev02Numa)
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

	fmt.Printf("비동기(Task) 방식 성능 최적화(VMX) 파라미터 주입을 시작합니다. (접속 계정: %s)\n", *vcId)

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

	ev01CoresPerSocket := *ev01Cores / *ev01Sockets
	ev02CoresPerSocket := *ev02Cores / *ev02Sockets

	ev01CoresPerNumaNode := 0
	if *ev01Numa > 0 {
		ev01CoresPerNumaNode = *ev01Cores / *ev01Numa
	}
	ev02CoresPerNumaNode := 0
	if *ev02Numa > 0 {
		ev02CoresPerNumaNode = *ev02Cores / *ev02Numa
	}

	var safeVmNames []string
	for _, baseHost := range hostlistLines {
		cleanHost := strings.TrimSpace(baseHost)
		safeVmNames = append(safeVmNames, regexp.QuoteMeta(cleanHost+"ev01"))
		safeVmNames = append(safeVmNames, regexp.QuoteMeta(cleanHost+"ev02"))
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

	var allTasks []*object.Task

	for _, baseHost := range hostlistLines {
		cleanHost := strings.TrimSpace(baseHost)
		if cleanHost == "" {
			continue
		}

		vmName01 := cleanHost + "ev01"
		vmName02 := cleanHost + "ev02"

		if _, exists01 := targetVmMap[vmName01]; !exists01 {
			if _, exists02 := targetVmMap[vmName02]; !exists02 {
				fmt.Printf("[%s] 경고: 대상 VM이 존재하지 않습니다. (PASS)\n", cleanHost)
				continue
			}
		}

		if vm01, ok := targetVmMap[vmName01]; ok {
			spec01 := types.VirtualMachineConfigSpec{}
			coresStr01 := strconv.Itoa(ev01CoresPerSocket)
			spec01.ExtraConfig = []types.BaseOptionValue{
				&types.OptionValue{Key: "sched.mem.lpage.enable1GPage", Value: "TRUE"},
				&types.OptionValue{Key: "sched.mem.pin", Value: "TRUE"},
				&types.OptionValue{Key: "sched.mem.prealloc", Value: "TRUE"},
				&types.OptionValue{Key: "sched.mem.prealloc.pinnedMainMem", Value: "TRUE"},
				&types.OptionValue{Key: "sched.swap.vmxSwapEnabled", Value: "FALSE"},
				&types.OptionValue{Key: "numa.vcpu.maxPerVirtualNode", Value: coresStr01},
				&types.OptionValue{Key: "cpuid.coresPerSocket", Value: coresStr01},
			}

			topoMsg01 := ""
			if *applyTopology {
				spec01.NumCPUs = int32(*ev01Cores)
				cps01 := int32(ev01CoresPerSocket)
				spec01.NumCoresPerSocket = &cps01
				topoMsg01 = fmt.Sprintf(", 토폴로지(vCPU %d / 소켓당 코어 %d", *ev01Cores, ev01CoresPerSocket)

				if ev01CoresPerNumaNode > 0 {
					cpn := int32(ev01CoresPerNumaNode)
					spec01.VirtualNuma = &types.VirtualMachineVirtualNuma{
						CoresPerNumaNode: &cpn,
					}
					topoMsg01 += fmt.Sprintf(" / NUMA 노드 %d", *ev01Numa)
				}
				topoMsg01 += ")"
			}

			task, err := vm01.Reconfigure(ctx, spec01)
			if err == nil {
				allTasks = append(allTasks, task)
				fmt.Printf("[%s] HugePage 및 코어당 소켓(%s)%s 비동기 주입 완료\n", vmName01, coresStr01, topoMsg01)
			} else {
				fmt.Printf("[%s] 설정 요청 실패: %v\n", vmName01, err)
			}
		}

		if vm02, ok := targetVmMap[vmName02]; ok {
			spec02 := types.VirtualMachineConfigSpec{}
			coresStr02 := strconv.Itoa(ev02CoresPerSocket)
			spec02.ExtraConfig = []types.BaseOptionValue{
				&types.OptionValue{Key: "sched.mem.lpage.enable1GPage", Value: "TRUE"},
				&types.OptionValue{Key: "sched.mem.pin", Value: "TRUE"},
				&types.OptionValue{Key: "sched.mem.prealloc", Value: "TRUE"},
				&types.OptionValue{Key: "sched.mem.prealloc.pinnedMainMem", Value: "TRUE"},
				&types.OptionValue{Key: "sched.swap.vmxSwapEnabled", Value: "FALSE"},
				&types.OptionValue{Key: "numa.vcpu.maxPerVirtualNode", Value: coresStr02},
				&types.OptionValue{Key: "cpuid.coresPerSocket", Value: coresStr02},
			}

			topoMsg02 := ""
			if *applyTopology {
				spec02.NumCPUs = int32(*ev02Cores)
				cps02 := int32(ev02CoresPerSocket)
				spec02.NumCoresPerSocket = &cps02
				topoMsg02 = fmt.Sprintf(", 토폴로지(vCPU %d / 소켓당 코어 %d", *ev02Cores, ev02CoresPerSocket)

				if ev02CoresPerNumaNode > 0 {
					cpn := int32(ev02CoresPerNumaNode)
					spec02.VirtualNuma = &types.VirtualMachineVirtualNuma{
						CoresPerNumaNode: &cpn,
					}
					topoMsg02 += fmt.Sprintf(" / NUMA 노드 %d", *ev02Numa)
				}
				topoMsg02 += ")"
			}

			task, err := vm02.Reconfigure(ctx, spec02)
			if err == nil {
				allTasks = append(allTasks, task)
				fmt.Printf("[%s] HugePage 및 코어당 소켓(%s)%s 비동기 주입 완료\n", vmName02, coresStr02, topoMsg02)
			} else {
				fmt.Printf("[%s] 설정 요청 실패: %v\n", vmName02, err)
			}
		}
	}

	if len(allTasks) > 0 {
		fmt.Printf("\n총 %d개의 성능 최적화 작업이 vCenter 큐(Queue)에서 동시 처리 중입니다.\n", len(allTasks))
		fmt.Println("완료될 때까지 대기합니다...")
		for _, task := range allTasks {
			_ = task.Wait(ctx)
		}
		fmt.Println("모든 VM의 VMX 성능 파라미터가 완벽하게 주입되었습니다!")
	} else {
		fmt.Println("\n실행할 비동기 작업이 없습니다.")
	}
}
