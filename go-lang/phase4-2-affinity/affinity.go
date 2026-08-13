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

	"github.com/vmware/govmomi"
	"github.com/vmware/govmomi/find"
	"github.com/vmware/govmomi/object"
	"github.com/vmware/govmomi/vim25/mo"
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

// affinity.go - Phase4-2 CPU 어피니티 비동기(Task) 일괄 할당 (HT ON/OFF 옵션 포함)
func main() {
	vcId := flag.String("id", "lscsystems@vsphere.local", "vCenter 로그인 계정 ID")
	vcTargetIP := flag.String("vcTargetIP", "", "vCenter 접속 IP (필수)")
	worklistFile := flag.String("worklistFile", "worklist.txt", "작업 대상 호스트 목록 파일")
	affinityFile := flag.String("affinityFile", "affinity.txt", "고정 Affinity 설정 파일")
	htMode := flag.String("ht", "ON", "Hyper-Threading 모드 (ON: 0=0,1 / OFF: 0=0)")
	flag.Parse()

	if *vcTargetIP == "" {
		log.Fatal("필수 파라미터가 누락되었습니다. (-vcTargetIP 확인)")
	}
	htOn := strings.ToUpper(strings.TrimSpace(*htMode)) != "OFF"
	vcPassword := os.Getenv("VC_PASSWORD")
	if vcPassword == "" {
		log.Fatal("인증 정보 로드 실패: VC_PASSWORD 환경 변수가 설정되지 않았습니다.")
	}

	baseDir, _ := os.Getwd()
	hostlistLines, err := readLines(filepath.Join(baseDir, *worklistFile))
	if err != nil {
		log.Fatalf("worklist 파일 읽기 실패: %v", err)
	}
	affinityMap := make(map[string]string)
	if affinityLines, err := readLines(filepath.Join(baseDir, *affinityFile)); err == nil {
		for _, line := range affinityLines {
			if parts := strings.SplitN(line, "=", 2); len(parts) == 2 {
				affinityMap[strings.TrimSpace(parts[0])] = strings.TrimSpace(parts[1])
			}
		}
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

	var allTasks []*object.Task
	for _, baseHost := range hostlistLines {
		cleanHost := strings.TrimSpace(baseHost)
		if cleanHost == "" {
			continue
		}
		vmName01, vmName02 := cleanHost+"ev01", cleanHost+"ev02"

		if vm01, ok := targetVmMap[vmName01]; ok {
			var vmProps mo.VirtualMachine
			if err := vm01.Properties(ctx, vm01.Reference(), []string{"config.hardware.numCPU"}, &vmProps); err == nil && vmProps.Config != nil {
				numCpu := int(vmProps.Config.Hardware.NumCPU)
				var extraConfig01 []types.BaseOptionValue
				for i := 0; i < numCpu; i++ {
					var val string
					if htOn {
						val = fmt.Sprintf("%d,%d", i*2, i*2+1)
					} else {
						val = fmt.Sprintf("%d", i)
					}
					extraConfig01 = append(extraConfig01, &types.OptionValue{Key: fmt.Sprintf("sched.vcpu%d.affinity", i), Value: val})
				}
				if task, err := vm01.Reconfigure(ctx, types.VirtualMachineConfigSpec{ExtraConfig: extraConfig01}); err == nil {
					allTasks = append(allTasks, task)
				}
			}
		}
		if vm02, ok := targetVmMap[vmName02]; ok && len(affinityMap) > 0 {
			var extraConfig02 []types.BaseOptionValue
			for key, val := range affinityMap {
				extraConfig02 = append(extraConfig02, &types.OptionValue{Key: key, Value: val})
			}
			if task, err := vm02.Reconfigure(ctx, types.VirtualMachineConfigSpec{ExtraConfig: extraConfig02}); err == nil {
				allTasks = append(allTasks, task)
			}
		}
	}
	for _, task := range allTasks {
		_ = task.Wait(ctx)
	}
	fmt.Println("모든 VM의 어피니티 설정이 완벽하게 적용되었습니다!")
}
