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

// ratio_normal.go - VM CPU/Memory Shares(Ratio)를 '일반(Normal)'으로 일괄 변경
// 사용: export VC_PASSWORD=... ; go run main.go -vcTargetIP=192.168.x.x -worklistFile=worklist.txt
func main() {
	vcId := flag.String("id", "administrator@vsphere.local", "vCenter 로그인 계정 ID")
	vcTargetIP := flag.String("vcTargetIP", "", "vCenter 접속 IP (필수)")
	worklistFile := flag.String("worklistFile", "worklist.txt", "작업 대상 VM 목록 파일")
	flag.Parse()

	if *vcTargetIP == "" {
		log.Fatal("필수 파라미터가 누락되었습니다. vCenter IP를 입력해 주세요 (-vcTargetIP).")
	}
	vcPassword := os.Getenv("VC_PASSWORD")
	if vcPassword == "" {
		log.Fatal("인증 정보 로드 실패: VC_PASSWORD 환경 변수가 설정되지 않았습니다.")
	}

	baseDir, _ := os.Getwd()
	vmList, err := readLines(filepath.Join(baseDir, *worklistFile))
	if err != nil {
		log.Fatalf("worklist 파일 읽기 실패: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	u := &url.URL{Scheme: "https", Host: *vcTargetIP, Path: "/sdk"}
	u.User = url.UserPassword(*vcId, vcPassword)
	client, err := govmomi.NewClient(ctx, u, true)
	if err != nil {
		log.Fatalf("vCenter 접속 오류: %v", err)
	}
	defer client.Logout(ctx)

	finder := find.NewFinder(client.Client, true)
	dc, _ := finder.DefaultDatacenter(ctx)
	finder.SetDatacenter(dc)

	var allTasks []*object.Task
	for _, vmName := range vmList {
		if vmName == "" {
			continue
		}
		vm, err := finder.VirtualMachine(ctx, vmName)
		if err != nil {
			fmt.Printf("[%s] 대상 VM을 찾을 수 없습니다: %v\n", vmName, err)
			continue
		}
		spec := types.VirtualMachineConfigSpec{
			CpuAllocation:    &types.ResourceAllocationInfo{Shares: &types.SharesInfo{Level: types.SharesLevelNormal}},
			MemoryAllocation: &types.ResourceAllocationInfo{Shares: &types.SharesInfo{Level: types.SharesLevelNormal}},
		}
		if task, err := vm.Reconfigure(ctx, spec); err == nil {
			allTasks = append(allTasks, task)
			fmt.Printf("[%s] Ratio '일반(Normal)' 변경 작업이 큐에 추가됨\n", vmName)
		} else {
			fmt.Printf("[%s] 설정 변경 실패: %v\n", vmName, err)
		}
	}
	for _, task := range allTasks {
		_ = task.Wait(ctx)
	}
	fmt.Println("모든 대상 VM의 Ratio 설정이 '일반(Normal)'으로 완벽하게 변경되었습니다!")
}
