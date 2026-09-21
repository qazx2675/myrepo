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
	BMHost string
	PGName string
	VlanId int32
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
	// =========================================================================
	// 1. 파라미터(Flag) 설정 (ID 파라미터 추가)
	// =========================================================================
	vcId := flag.String("id", "lscsystems@vsphere.local", "vCenter 로그인 계정 ID")
	vcTargetIP := flag.String("vcTargetIP", "", "vCenter 접속 IP")
	worklistFile := flag.String("worklistFile", "worklist.txt", "vSwitch 및 포트그룹 설정 파일")
	targetVSwitch := flag.String("targetVSwitch", "vSwitch0", "타겟 가상 스위치")
	concurrency := flag.Int("concurrency", 20, "동시에 처리할 호스트 수 (같은 호스트의 포트그룹 여러 개는 호스트 안에서 순서대로 생성)")
	flag.Parse()

	if *vcTargetIP == "" {
		log.Fatal("필수 파라미터(-vcTargetIP)가 누락되었습니다.")
	}
	if *concurrency < 1 {
		log.Fatalf("-concurrency 값이 올바르지 않습니다: %d (1 이상)", *concurrency)
	}

	vcPassword := os.Getenv("VC_PASSWORD")

	if vcPassword == "" {
		log.Fatal("환경변수 'VC_PASSWORD'가 설정되지 않았습니다.")
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
			parsedConfigs = append(parsedConfigs, VSwitchConfig{
				BMHost: parts[0],
				PGName: parts[1],
				VlanId: int32(vlanId),
			})

			if !uniqueMap[parts[0]] {
				uniqueMap[parts[0]] = true
				uniqueHosts = append(uniqueHosts, parts[0])
			}
		}
	}

	fmt.Printf("\n[INFO] 단독 실행: vSwitch 포트 그룹 일괄 생성 (Phase 2) 시작 (접속 계정: %s)\n", *vcId)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	u := &url.URL{Scheme: "https", Host: *vcTargetIP, Path: "/sdk"}
	// 파라미터로 받은 계정 ID(*vcId) 사용
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
	err = v.Retrieve(ctx, []string{"HostSystem"}, []string{"name", "configManager.networkSystem"}, &allHosts)
	if err != nil {
		log.Fatalf("[에러] 호스트 정보 일괄 수집 실패: %v", err)
	}

	hostByName := make(map[string]*mo.HostSystem, len(allHosts))
	for i := range allHosts {
		if _, dup := hostByName[allHosts[i].Name]; !dup {
			hostByName[allHosts[i].Name] = &allHosts[i] // 예전 순차 탐색과 같이 먼저 나온 호스트를 쓴다
		}
	}

	// 호스트끼리는 서로 독립이라 워커풀로 동시에 처리한다(예전에는 호스트를 하나씩 순차 처리).
	// 한 호스트의 포트그룹 여러 개는 같은 HostNetworkSystem을 건드리므로 호스트 안에서는 순서대로 만든다.
	// 출력은 호스트별로 모아서 한 번에 찍어, 여러 호스트의 줄이 섞이지 않게 한다(순서는 완료순).
	var printMu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, *concurrency)
	for _, bmHost := range uniqueHosts {
		wg.Add(1)
		sem <- struct{}{}
		go func(bmHost string) {
			defer wg.Done()
			defer func() { <-sem }()
			var out strings.Builder
			defer func() {
				printMu.Lock()
				fmt.Print(out.String())
				printMu.Unlock()
			}()

			targetHs := hostByName[bmHost]
			if targetHs == nil {
				fmt.Fprintf(&out, "[에러] 호스트 '%s' 를 vCenter 전체 인벤토리에서 찾을 수 없습니다.\n", bmHost)
				return
			}

			// 구조체 값에 대한 nil 검사 제거, 하위 포인터 속성만 검사
			if targetHs.ConfigManager.NetworkSystem == nil {
				fmt.Fprintf(&out, "[에러] 호스트 '%s' 네트워크 시스템(vSwitch)을 구성할 수 없습니다.\n", bmHost)
				return
			}

			netSys := object.NewHostNetworkSystem(client.Client, *targetHs.ConfigManager.NetworkSystem)

			for _, cfg := range parsedConfigs {
				if cfg.BMHost == bmHost || cfg.BMHost == targetHs.Name {
					spec := types.HostPortGroupSpec{
						Name:        cfg.PGName,
						VlanId:      cfg.VlanId,
						VswitchName: *targetVSwitch,
						Policy:      types.HostNetworkPolicy{},
					}

					err := netSys.AddPortGroup(ctx, spec)
					if err != nil {
						if strings.Contains(err.Error(), "AlreadyExists") {
							fmt.Fprintf(&out, "  -> [%s] 스킵: 포트그룹(%s) 이미 존재\n", bmHost, cfg.PGName)
						} else {
							fmt.Fprintf(&out, "  -> [%s] 실패: 포트그룹(%s) 생성 에러: %v\n", bmHost, cfg.PGName, err)
						}
					} else {
						fmt.Fprintf(&out, "  -> [%s] 성공: 포트그룹(%s) / VLAN(%d) 생성 완료\n", bmHost, cfg.PGName, cfg.VlanId)
					}
				}
			}
		}(bmHost)
	}
	wg.Wait()

	fmt.Println("\n[INFO] 포트 그룹 생성 작업 완료")
	fmt.Println("vCenter 세션을 안전하게 종료했습니다.")
}
