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
	"sync"
	"sync/atomic"

	"github.com/vmware/govmomi"
	"github.com/vmware/govmomi/find"
	"github.com/vmware/govmomi/vim25/methods"
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

func applyPowerPolicy(ctx context.Context, client *govmomi.Client, finder *find.Finder, hostName string) (msg string, ok bool) {
	hostObj, err := finder.HostSystem(ctx, hostName)
	if err != nil {
		return fmt.Sprintf("[%s] 조건에 맞는 물리 호스트를 인벤토리에서 찾을 수 없습니다.", hostName), false
	}
	var hostMo mo.HostSystem
	if err := hostObj.Properties(ctx, hostObj.Reference(), []string{"configManager"}, &hostMo); err != nil || hostMo.ConfigManager.PowerSystem == nil {
		return fmt.Sprintf("[%s] 실패: 전원 관리 시스템 속성을 불러올 수 없습니다.", hostName), false
	}
	powerSystemRef := *hostMo.ConfigManager.PowerSystem
	var powerSys mo.HostPowerSystem
	if err := client.PropertyCollector().RetrieveOne(ctx, powerSystemRef, []string{"capability", "info"}, &powerSys); err != nil {
		return fmt.Sprintf("[%s] 실패: 전원 관리 시스템 속성을 불러올 수 없습니다.", hostName), false
	}
	if powerSys.Info.CurrentPolicy.ShortName == "static" {
		return fmt.Sprintf("[%s] 스킵: 이미 고성능(High Performance) 정책이 적용되어 있습니다.", hostName), true
	}
	var highPerfPolicy *types.HostPowerPolicy
	for _, policy := range powerSys.Capability.AvailablePolicy {
		if policy.ShortName == "static" {
			p := policy
			highPerfPolicy = &p
			break
		}
	}
	if highPerfPolicy == nil {
		return fmt.Sprintf("[%s] 실패: 하드웨어 모델이 고성능 정책을 지원하지 않거나 인식하지 못합니다.", hostName), false
	}
	req := types.ConfigurePowerPolicy{This: powerSystemRef, Key: highPerfPolicy.Key}
	if _, err := methods.ConfigurePowerPolicy(ctx, client.Client, &req); err != nil {
		return fmt.Sprintf("[%s] 실패: 전원 정책 주입 중 오류 발생 - %v", hostName, err), false
	}
	return fmt.Sprintf("[%s] 성공: 고성능(High Performance) 전원 정책 주입 완료", hostName), true
}

// power_policy.go - Phase4-3 ESXi 물리 호스트 전원 정책(High Performance) 일괄 적용 (동시성 제한)
func main() {
	vcId := flag.String("id", "lscsystems@vsphere.local", "vCenter 로그인 계정 ID")
	vcTargetIP := flag.String("vcTargetIP", "", "vCenter 접속 IP (필수)")
	worklistFile := flag.String("worklistFile", "worklist_bm.txt", "작업 대상 호스트 목록 파일")
	concurrency := flag.Int("concurrency", 10, "동시 처리 개수")
	flag.Parse()

	if *vcTargetIP == "" {
		log.Fatal("필수 파라미터가 누락되었습니다. (-vcTargetIP 확인)")
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

	baseFinder := find.NewFinder(client.Client, true)
	dc, err := baseFinder.DefaultDatacenter(ctx)
	if err != nil {
		log.Fatalf("기본 데이터센터 조회 실패: %v", err)
	}

	var cleanNames []string
	for _, hostName := range hostlistLines {
		if c := strings.TrimSpace(hostName); c != "" {
			cleanNames = append(cleanNames, c)
		}
	}
	if len(cleanNames) == 0 {
		fmt.Println("작업할 물리 호스트가 없어 스크립트를 종료합니다.")
		return
	}

	var wg sync.WaitGroup
	var mu sync.Mutex
	var successCnt, failCnt int64
	sem := make(chan struct{}, *concurrency)
	results := make([]string, len(cleanNames))
	for i, hostName := range cleanNames {
		wg.Add(1)
		sem <- struct{}{}
		go func(idx int, name string) {
			defer wg.Done()
			defer func() { <-sem }()
			finder := find.NewFinder(client.Client, true)
			finder.SetDatacenter(dc)
			msg, ok := applyPowerPolicy(ctx, client, finder, name)
			mu.Lock()
			results[idx] = msg
			mu.Unlock()
			if ok {
				atomic.AddInt64(&successCnt, 1)
			} else {
				atomic.AddInt64(&failCnt, 1)
			}
		}(i, hostName)
	}
	wg.Wait()
	for _, r := range results {
		fmt.Println(r)
	}
	fmt.Printf("\n처리 완료: 총 %d대 (성공/스킵 %d, 실패 %d)\n", len(cleanNames), successCnt, failCnt)
}
