// power_setting: worklist 의 BM(ESXi 호스트)에 전원 관리 정책을 "고성능(High Performance, static)"으로 설정.
// 호스트끼리는 독립이라 워커풀로 병렬 처리한다(-concurrency). 이미 고성능이면 건너뛴다.
// worklist 의 이름은 FQDN 이든 hostname(첫 '.' 앞)만이든 찾는다 (vswitch_setting 과 같은 규칙).

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
	"github.com/vmware/govmomi/view"
	"github.com/vmware/govmomi/vim25/methods"
	"github.com/vmware/govmomi/vim25/mo"
	"github.com/vmware/govmomi/vim25/types"
)

// vSphere 전원 정책 중 "고성능"의 shortName/이름 (key 는 보통 1).
// 이름은 vm-param-check 의 host power policy 체크 기대값과 같다.
const (
	highPerfShortName = "static"
	highPerfName      = "High Performance"
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
	worklistFile := flag.String("worklistFile", "worklist.txt", "작업 대상 BM 목록 파일 (한 줄에 하나, 첫 칸만 사용)")
	concurrency := flag.Int("concurrency", 20, "동시에 처리할 호스트 수")
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
	lines, err := readLines(filepath.Join(baseDir, *worklistFile))
	if err != nil {
		log.Fatalf("파일 로드 실패 (%s): %v", *worklistFile, err)
	}
	var bms []string
	seen := make(map[string]bool)
	for _, line := range lines {
		bm := strings.Fields(line)[0]
		if !seen[bm] {
			seen[bm] = true
			bms = append(bms, bm)
		}
	}
	if len(bms) == 0 {
		log.Fatalf("%s 에 BM 이 없습니다.", *worklistFile)
	}

	fmt.Printf("\n[INFO] BM 전원 정책 고성능 설정 시작: %d대 (접속 계정: %s)\n", len(bms), *vcId)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	u := &url.URL{Scheme: "https", Host: *vcTargetIP, Path: "/sdk"}
	u.User = url.UserPassword(*vcId, vcPassword)
	client, err := govmomi.NewClient(ctx, u, true)
	if err != nil {
		log.Fatalf("vCenter 접속 실패: %v", err)
	}
	defer client.Logout(ctx)

	// 호스트 정보는 한 번에 모아 온다(호스트 수천 대여도 호출 1번 — 호스트마다 조회하지 않음).
	m := view.NewManager(client.Client)
	v, err := m.CreateContainerView(ctx, client.ServiceContent.RootFolder, []string{"HostSystem"}, true)
	if err != nil {
		log.Fatalf("[에러] 인벤토리 뷰 생성 실패: %v", err)
	}
	defer v.Destroy(ctx)

	var allHosts []mo.HostSystem
	err = v.Retrieve(ctx, []string{"HostSystem"}, []string{
		"name", "configManager.powerSystem",
		"config.powerSystemInfo", "config.powerSystemCapability",
	}, &allHosts)
	if err != nil {
		log.Fatalf("[에러] 호스트 정보 일괄 수집 실패: %v", err)
	}

	hostByName := make(map[string]*mo.HostSystem, len(allHosts))
	hostByShort := make(map[string][]*mo.HostSystem, len(allHosts))
	for i := range allHosts {
		if _, dup := hostByName[allHosts[i].Name]; !dup {
			hostByName[allHosts[i].Name] = &allHosts[i]
		}
		short := strings.Split(allHosts[i].Name, ".")[0]
		hostByShort[short] = append(hostByShort[short], &allHosts[i])
	}
	// 정확한 이름이 있으면 그것, 없으면 짧은 이름(첫 '.' 앞)끼리 비교해서 하나뿐일 때만 쓴다.
	lookupHost := func(bm string) (*mo.HostSystem, string) {
		if h := hostByName[bm]; h != nil {
			return h, ""
		}
		cands := hostByShort[strings.Split(bm, ".")[0]]
		switch len(cands) {
		case 0:
			return nil, fmt.Sprintf("[에러] 호스트 '%s' 를 vCenter 전체 인벤토리에서 찾을 수 없습니다.\n", bm)
		case 1:
			return cands[0], ""
		default:
			return nil, fmt.Sprintf("[에러] 호스트 '%s' 와 짧은 이름이 같은 호스트가 %d대 있어 어느 쪽인지 정할 수 없습니다 (vCenter에 보이는 이름 그대로 적어주세요).\n", bm, len(cands))
		}
	}

	var okCount, skipCount, failCount int32
	var printMu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, *concurrency)
	for _, bm := range bms {
		wg.Add(1)
		sem <- struct{}{}
		go func(bm string) {
			defer wg.Done()
			defer func() { <-sem }()
			var out strings.Builder
			defer func() {
				printMu.Lock()
				fmt.Print(out.String())
				printMu.Unlock()
			}()

			h, lookupErr := lookupHost(bm)
			if h == nil {
				fmt.Fprint(&out, lookupErr)
				atomic.AddInt32(&failCount, 1)
				return
			}
			if h.ConfigManager.PowerSystem == nil || h.Config == nil {
				fmt.Fprintf(&out, "[에러] 호스트 '%s' 전원 관리 시스템을 조회할 수 없습니다 (연결 끊김/미지원).\n", h.Name)
				atomic.AddInt32(&failCount, 1)
				return
			}

			// 고성능 정책 key 는 호스트가 알려주는 목록에서 shortName 으로 찾고, 목록이 없으면 1 을 쓴다.
			key := int32(1)
			if c := h.Config.PowerSystemCapability; c != nil {
				for _, p := range c.AvailablePolicy {
					if p.ShortName == highPerfShortName || p.Name == highPerfName {
						key = p.Key
					}
				}
			}
			if info := h.Config.PowerSystemInfo; info != nil && info.CurrentPolicy.Key == key {
				fmt.Fprintf(&out, "  -> [%s] 스킵: 이미 고성능(%s)\n", h.Name, info.CurrentPolicy.ShortName)
				atomic.AddInt32(&skipCount, 1)
				return
			}

			_, err := methods.ConfigurePowerPolicy(ctx, client.Client, &types.ConfigurePowerPolicy{
				This: *h.ConfigManager.PowerSystem,
				Key:  key,
			})
			if err != nil {
				fmt.Fprintf(&out, "  -> [%s] 실패: 전원 정책 변경 에러: %v\n", h.Name, err)
				atomic.AddInt32(&failCount, 1)
				return
			}
			fmt.Fprintf(&out, "  -> [%s] 성공: 전원 정책 고성능(key=%d) 설정 완료\n", h.Name, key)
			atomic.AddInt32(&okCount, 1)
		}(bm)
	}
	wg.Wait()

	fmt.Printf("\n[INFO] 전원 정책 설정 완료 — 성공 %d / 스킵(이미 고성능) %d / 실패 %d\n",
		atomic.LoadInt32(&okCount), atomic.LoadInt32(&skipCount), atomic.LoadInt32(&failCount))
	if atomic.LoadInt32(&failCount) > 0 {
		// defer 가 os.Exit 로 건너뛰어지므로 세션 종료를 여기서 직접 한다.
		v.Destroy(ctx)
		client.Logout(ctx)
		os.Exit(1)
	}
}
