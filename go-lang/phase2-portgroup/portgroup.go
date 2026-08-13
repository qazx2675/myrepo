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
	"strconv"
	"strings"
	"sync"

	"github.com/vmware/govmomi"
	"github.com/vmware/govmomi/object"
	"github.com/vmware/govmomi/view"
	"github.com/vmware/govmomi/vim25/mo"
	"github.com/vmware/govmomi/vim25/types"
)

type VSwitchConfig struct {
	BMHost, PGName string
	VlanId         int32
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

// portgroup.go - Phase2 vSwitch 포트그룹 초고속 일괄 생성 (호스트 단위 병렬화)
func main() {
	vcId := flag.String("id", "lscsystems@vsphere.local", "vCenter 로그인 계정 ID")
	vcTargetIP := flag.String("vcTargetIP", "", "vCenter 접속 IP")
	worklistFile := flag.String("worklistFile", "worklist.txt", "vSwitch 및 포트그룹 설정 파일")
	targetVSwitch := flag.String("targetVSwitch", "vSwitch0", "타겟 가상 스위치")
	concurrency := flag.Int("concurrency", 24, "호스트 동시 처리 수 (500대 규모 권장: 16~32)")
	flag.Parse()

	if *vcTargetIP == "" {
		log.Fatal("필수 파라미터(-vcTargetIP)가 누락되었습니다.")
	}
	vcPassword := os.Getenv("VC_PASSWORD")
	if vcPassword == "" {
		log.Fatal("환경변수 'VC_PASSWORD'가 설정되지 않았습니다.")
	}
	if *concurrency < 1 {
		*concurrency = 1
	}

	baseDir, _ := os.Getwd()
	vswitchLines, err := readLines(filepath.Join(baseDir, *worklistFile))
	if err != nil {
		log.Fatalf("파일 로드 실패 (%s): %v", *worklistFile, err)
	}

	var parsedConfigs []VSwitchConfig
	var uniqueHosts []string
	uniqueMap := make(map[string]bool)
	for _, line := range vswitchLines {
		parts := strings.Fields(line)
		if len(parts) >= 3 {
			vlanId, _ := strconv.Atoi(parts[2])
			parsedConfigs = append(parsedConfigs, VSwitchConfig{BMHost: parts[0], PGName: parts[1], VlanId: int32(vlanId)})
			if !uniqueMap[parts[0]] {
				uniqueMap[parts[0]] = true
				uniqueHosts = append(uniqueHosts, parts[0])
			}
		}
	}
	configsByHost := make(map[string][]VSwitchConfig, len(uniqueHosts))
	for _, cfg := range parsedConfigs {
		configsByHost[cfg.BMHost] = append(configsByHost[cfg.BMHost], cfg)
	}

	fmt.Printf("\n[INFO] 단독 실행: vSwitch 포트 그룹 일괄 생성 (Phase 2) 시작 (접속 계정: %s)\n", *vcId)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	u := &url.URL{Scheme: "https", Host: *vcTargetIP, Path: "/sdk"}
	u.User = url.UserPassword(*vcId, vcPassword)
	client, err := govmomi.NewClient(ctx, u, true)
	if err != nil {
		log.Fatalf("vCenter 접속 실패: %v", err)
	}
	defer client.Logout(ctx)

	m := view.NewManager(client.Client)
	v, err := m.CreateContainerView(ctx, client.ServiceContent.RootFolder, []string{"HostSystem"}, true)
	if err != nil {
		log.Fatalf("[에러] 인벤토리 뷰 생성 실패: %v", err)
	}
	defer v.Destroy(ctx)
	var allHosts []mo.HostSystem
	if err := v.Retrieve(ctx, []string{"HostSystem"}, []string{"name", "configManager.networkSystem"}, &allHosts); err != nil {
		log.Fatalf("[에러] 호스트 정보 일괄 수집 실패: %v", err)
	}
	hostByName := make(map[string]*mo.HostSystem, len(allHosts))
	for i := range allHosts {
		hostByName[allHosts[i].Name] = &allHosts[i]
	}

	logs := make([][]string, len(uniqueHosts))
	var wg sync.WaitGroup
	sem := make(chan struct{}, *concurrency)
	for hi, bmHost := range uniqueHosts {
		wg.Add(1)
		sem <- struct{}{}
		go func(hi int, bmHost string) {
			defer wg.Done()
			defer func() { <-sem }()
			var out []string
			targetHs := hostByName[bmHost]
			if targetHs == nil || targetHs.ConfigManager.NetworkSystem == nil {
				logs[hi] = out
				return
			}
			netSys := object.NewHostNetworkSystem(client.Client, *targetHs.ConfigManager.NetworkSystem)
			var matched []VSwitchConfig
			matched = append(matched, configsByHost[bmHost]...)
			if targetHs.Name != bmHost {
				matched = append(matched, configsByHost[targetHs.Name]...)
			}
			for _, cfg := range matched {
				spec := types.HostPortGroupSpec{Name: cfg.PGName, VlanId: cfg.VlanId, VswitchName: *targetVSwitch, Policy: types.HostNetworkPolicy{}}
				if addErr := netSys.AddPortGroup(ctx, spec); addErr != nil {
					if strings.Contains(addErr.Error(), "AlreadyExists") {
						out = append(out, fmt.Sprintf(" -> [%s] 스킵: 포트그룹(%s) 이미 존재", bmHost, cfg.PGName))
					} else {
						out = append(out, fmt.Sprintf(" -> [%s] 실패: 포트그룹(%s) 생성 에러: %v", bmHost, cfg.PGName, addErr))
					}
				} else {
					out = append(out, fmt.Sprintf(" -> [%s] 성공: 포트그룹(%s) / VLAN(%d) 생성 완료", bmHost, cfg.PGName, cfg.VlanId))
				}
			}
			logs[hi] = out
		}(hi, bmHost)
	}
	wg.Wait()
	for _, lines := range logs {
		for _, l := range lines {
			fmt.Println(l)
		}
	}
	fmt.Println("\n[INFO] 포트 그룹 생성 작업 완료")
	fmt.Println("vCenter 세션을 안전하게 종료했습니다.")
}
