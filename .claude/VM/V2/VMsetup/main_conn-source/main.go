package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/vmware/govmomi"
	"github.com/vmware/govmomi/find"
	"github.com/vmware/govmomi/object"
	"github.com/vmware/govmomi/task"
	"github.com/vmware/govmomi/view"
	"github.com/vmware/govmomi/vim25/mo"
	"github.com/vmware/govmomi/vim25/types"
)

const defaultConcurrency = 20

// printMu: 여러 고루틴이 동시에 fmt.Printf 를 호출할 때 줄이 섞이지 않도록 보호
var printMu sync.Mutex

func safePrintf(format string, a ...interface{}) {
	printMu.Lock()
	defer printMu.Unlock()
	fmt.Printf(format, a...)
}

// 파일의 각 줄을 읽어 배열로 반환
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

// addHost는 지정된 위치(클러스터 또는 폴더)에 호스트 하나를 등록한다.
// 자가서명 인증서(폐쇄망 환경) 때문에 SSLVerifyFault가 나면,
// 에러에 담긴 실제 thumbprint를 꺼내 spec에 채운 뒤 자동으로 한 번 재시도한다.
//
// spec은 값(value)으로 전달받는다 — 함수 내부에서 spec.SslThumbprint를 채워도
// 호출자(고루틴)가 들고 있는 원본에는 영향이 없고, 각 고루틴은 자신만의 spec
// 복사본을 갖게 되어 동시 호출 간 데이터 레이스가 없다.
func addHost(ctx context.Context, host string, spec types.HostConnectSpec, targetCluster *object.ClusterComputeResource, targetFolder *object.Folder) error {
	runOnce := func(s types.HostConnectSpec) (*object.Task, error) {
		if targetCluster != nil {
			return targetCluster.AddHost(ctx, s, true, nil, nil)
		}
		return targetFolder.AddStandaloneHost(ctx, s, true, nil, nil)
	}

	t, err := runOnce(spec)
	if err != nil {
		return fmt.Errorf("task 발급 실패: %w", err)
	}

	_, waitErr := t.WaitForResult(ctx, nil)
	if waitErr == nil {
		return nil
	}

	// SSLVerifyFault인지 확인 (인증서 미신뢰로 인한 등록 실패)
	var taskErr task.Error
	if errors.As(waitErr, &taskErr) {
		if sslFault, ok := taskErr.Fault().(*types.SSLVerifyFault); ok {
			safePrintf("  -> [%s] SSL 인증서 미신뢰 감지, thumbprint(%s) 적용 후 재시도\n", host, sslFault.Thumbprint)

			spec.SslThumbprint = sslFault.Thumbprint

			t2, err2 := runOnce(spec)
			if err2 != nil {
				return fmt.Errorf("재시도 task 발급 실패: %w", err2)
			}
			if _, waitErr2 := t2.WaitForResult(ctx, nil); waitErr2 != nil {
				return fmt.Errorf("재시도 실패: %w", waitErr2)
			}
			return nil
		}
	}

	return fmt.Errorf("등록 실패: %w", waitErr)
}

func main() {
	// =========================================================================
	// 1. 파라미터(Flag) 설정
	// =========================================================================
	vcId := flag.String("id", "lscsystems@vsphere.local", "vCenter 로그인 계정 ID")
	vcTargetIP := flag.String("vcTargetIP", "", "vCenter 접속 IP")
	folderName := flag.String("folderName", "", "데이터센터 내 대상 폴더 또는 클러스터 이름 (데이터센터 자체를 대상으로 하려면 -datacenter와 동일한 값을 지정하거나 비워두면 해당 데이터센터의 기본 HostFolder 사용)")
	datacenterName := flag.String("datacenter", "", "데이터센터 이름 (데이터센터가 여러 개면 필수, 1개뿐이면 생략 가능)")
	worklistFile := flag.String("worklistFile", "worklist.txt", "VM 대상 목록 파일")
	concurrency := flag.Int("concurrency", defaultConcurrency, "동시 처리 개수 제한 (등록 여부 확인 + 호스트 등록 전송+대기 전 구간에 적용)")
	domain := flag.String("domain", "saccae.com", "도메인 없이 적힌 호스트 이름을 FQDN 으로 조회하지 못할 때 뒤에 붙일 도메인 (빈 값이면 붙이지 않음)")
	flag.Parse()

	if *vcTargetIP == "" || *folderName == "" {
		log.Fatal("필수 파라미터(-vcTargetIP, -folderName)가 누락되었습니다.")
	}

	if *concurrency < 1 {
		log.Fatalf("-concurrency 값이 올바르지 않습니다: %d (1 이상)", *concurrency)
	}

	// 환경 변수에서 비밀번호 로드 (보안)
	vcPassword := os.Getenv("VC_PASSWORD")
	esxiPassword := os.Getenv("ESXI_PASSWORD")

	if vcPassword == "" || esxiPassword == "" {
		log.Fatal("환경변수 'VC_PASSWORD' 또는 'ESXI_PASSWORD'가 설정되지 않았습니다.")
	}

	baseDir, _ := os.Getwd()
	serverLines, err := readLines(filepath.Join(baseDir, *worklistFile))
	if err != nil {
		log.Fatalf("파일 로드 실패 (%s): %v", *worklistFile, err)
	}

	fmt.Printf("\n[INFO] 단독 실행: ESXi 호스트 병렬 등록 (Phase 1) 시작 (접속 계정: %s, 동시 처리 제한: %d)\n", *vcId, *concurrency)

	// =========================================================================
	// 2. vCenter 접속
	// =========================================================================
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	u := &url.URL{Scheme: "https", Host: *vcTargetIP, Path: "/sdk"}
	u.User = url.UserPassword(*vcId, vcPassword)

	client, err := govmomi.NewClient(ctx, u, true)
	if err != nil {
		log.Fatalf("vCenter 접속 실패: %v", err)
	}
	defer client.Logout(ctx)
	finder := find.NewFinder(client.Client, true)

	// =========================================================================
	// 3. 데이터센터 선택 (버그 수정: ClusterComputeResource/Folder/HostSystem
	// 검색은 내부적으로 데이터센터 컨텍스트(f.dc)가 반드시 필요하다. 이 컨텍스트를
	// 먼저 확정하고 SetDatacenter로 지정해야, 이후의 클러스터/폴더/호스트 검색이
	// "please specify a datacenter" 에러 없이 정상적으로 동작한다.
	// (기존 버전은 SetDatacenter를 한 번도 호출하지 않아 -folderName에 클러스터나
	// 폴더 이름을 넣으면 실제로 존재해도 항상 "위치를 찾을 수 없습니다"로 실패했고,
	// 이미 등록된 호스트 여부 확인도 항상 실패로 판정되는 문제가 있었다.)
	// =========================================================================
	var selectedDC *object.Datacenter
	if *datacenterName != "" {
		dc, dcErr := finder.Datacenter(ctx, *datacenterName)
		if dcErr != nil {
			log.Fatalf("데이터센터 '%s'를 찾을 수 없습니다: %v", *datacenterName, dcErr)
		}
		selectedDC = dc
	} else {
		dcs, dcErr := finder.DatacenterList(ctx, "*")
		if dcErr != nil || len(dcs) == 0 {
			log.Fatalf("데이터센터 목록 조회 실패: %v", dcErr)
		}
		if len(dcs) == 1 {
			selectedDC = dcs[0]
		} else {
			var names []string
			for _, dc := range dcs {
				names = append(names, dc.Name())
			}
			log.Fatalf("데이터센터가 %d개 존재하여 자동 선택이 불가합니다. -datacenter 옵션으로 지정하세요. (목록: %s)", len(dcs), strings.Join(names, ", "))
		}
	}
	finder.SetDatacenter(selectedDC)
	fmt.Printf("[INFO] 데이터센터 [%s] 사용\n", selectedDC.Name())

	// =========================================================================
	// 4. 대상 위치(Location) 자동 탐색 — 이제 데이터센터 컨텍스트가 설정된
	// 상태이므로 클러스터/폴더 검색이 정상적으로 동작한다.
	// =========================================================================
	var targetFolder *object.Folder
	var targetCluster *object.ClusterComputeResource

	if cluster, err := finder.ClusterComputeResource(ctx, *folderName); err == nil {
		targetCluster = cluster
		fmt.Printf("[INFO] 대상 감지 완료: 클러스터 [%s]\n", cluster.Name())
	} else if folder, err := finder.Folder(ctx, *folderName); err == nil {
		targetFolder = folder
		fmt.Printf("[INFO] 대상 감지 완료: 폴더 [%s]\n", folder.Name())
	} else if strings.EqualFold(*folderName, selectedDC.Name()) {
		folders, foldersErr := selectedDC.Folders(ctx)
		if foldersErr != nil {
			log.Fatalf("데이터센터 [%s]의 폴더 조회 실패: %v", selectedDC.Name(), foldersErr)
		}
		targetFolder = folders.HostFolder
		fmt.Printf("[INFO] 대상 감지 완료: 데이터센터 [%s] (내부 HostFolder 사용)\n", selectedDC.Name())
	} else {
		log.Fatalf("[오류] 데이터센터 '%s' 내에서 '%s' 위치(클러스터/폴더)를 찾을 수 없습니다.", selectedDC.Name(), *folderName)
	}

	// =========================================================================
	// 5. 이미 등록된 호스트인지 확인 — 이 데이터센터의 호스트 이름을 한 번에 가져와 비교한다.
	// worklist 에 짧은 이름(esxi01)을 적어도 vCenter 에 FQDN(esxi01.domain)으로 등록돼 있으면
	// 같은 호스트로 본다(이름이 정확히 같거나, 첫 '.' 앞부분이 같으면). 못 알아보면 이미 있는
	// 호스트를 다시 등록하려 들어 중복이 생긴다.
	// =========================================================================
	registered, regErr := registeredHostNames(ctx, client, selectedDC)
	if regErr != nil {
		log.Fatalf("등록된 호스트 목록 조회 실패: %v", regErr)
	}
	existsResults := make([]bool, len(serverLines))
	for i, host := range serverLines {
		existsResults[i] = registered[host] || registered[shortName(host)]
	}

	// 등록 여부 확인이 다 끝난 뒤, worklist 순서 그대로 출력 + targets 구성.
	// (고루틴 안에서 바로 출력하면 완료 순서대로 뒤섞여 나오므로, 순서를
	// 보존하기 위해 여기서 한 번에 정리한다.)
	var targets []string
	for i, host := range serverLines {
		if existsResults[i] {
			fmt.Printf("  -> [%s] 이미 등록됨 (PASS)\n", host)
			continue
		}
		targets = append(targets, host)
	}

	if len(targets) == 0 {
		fmt.Println("\n[안내] 새로 등록할 호스트가 없습니다.")
		fmt.Println("vCenter 세션을 안전하게 종료했습니다.")
		return
	}

	// =========================================================================
	// 6. 호스트 병렬 등록 (thumbprint 자동 재시도 포함, 동시성 제한 적용)
	// =========================================================================
	fmt.Printf("\n[INFO] 등록 대상 호스트 %d대 — 동시 %d개 제한으로 등록을 시작합니다.\n", len(targets), *concurrency)

	var wg sync.WaitGroup
	var mu sync.Mutex
	var failed []string
	sem := make(chan struct{}, *concurrency)

	for _, host := range targets {
		wg.Add(1)
		sem <- struct{}{}

		go func(host string) {
			defer wg.Done()
			defer func() { <-sem }()

			fqdn, how := registrationName(host, *domain)
			safePrintf("  -> [%s] 등록 Task 발급 중... (등록 이름 %s — %s)\n", host, fqdn, how)
			// spec은 고루틴 로컬 변수 — 각 호출마다 독립된 값이라
			// 여러 고루틴이 동시에 등록을 진행해도 서로의 spec을 건드리지 않는다.
			spec := types.HostConnectSpec{
				HostName: fqdn,
				UserName: "root",
				Password: esxiPassword,
				Force:    true,
			}

			if err := addHost(ctx, host, spec, targetCluster, targetFolder); err != nil {
				safePrintf("  -> [%s] 등록 실패: %v\n", host, err)
				mu.Lock()
				failed = append(failed, host)
				mu.Unlock()
				return
			}
			safePrintf("  -> [%s] 등록 완료\n", host)
		}(host)
	}
	wg.Wait()

	if len(failed) > 0 {
		fmt.Printf("\n[일부 실패] 등록 실패 호스트: %s\n", strings.Join(failed, ", "))
	} else {
		fmt.Println("\n[성공] 호스트 등록이 완료되었습니다.")
	}

	fmt.Println("vCenter 세션을 안전하게 종료했습니다.")
}

// shortName은 호스트 이름의 첫 '.' 앞부분을 돌려준다(IP 주소는 그대로).
func shortName(host string) string {
	if net.ParseIP(host) != nil {
		return host
	}
	return strings.SplitN(host, ".", 2)[0]
}

// registeredHostNames는 데이터센터에 등록된 모든 호스트의 이름과 짧은 이름을 모은다.
func registeredHostNames(ctx context.Context, client *govmomi.Client, dc *object.Datacenter) (map[string]bool, error) {
	m := view.NewManager(client.Client)
	v, err := m.CreateContainerView(ctx, dc.Reference(), []string{"HostSystem"}, true)
	if err != nil {
		return nil, err
	}
	defer v.Destroy(ctx)
	var hosts []mo.HostSystem
	if err := v.Retrieve(ctx, []string{"HostSystem"}, []string{"name"}, &hosts); err != nil {
		return nil, err
	}
	names := make(map[string]bool, len(hosts)*2)
	for _, h := range hosts {
		names[h.Name] = true
		names[shortName(h.Name)] = true
	}
	return names, nil
}

// registrationName은 vCenter 에 등록할 때 쓸 호스트 이름을 정한다.
// 이미 FQDN 이거나 IP 이면 그대로 쓴다. 도메인 없는 이름이면 먼저 이 서버에서 FQDN 을 조회하고
// (이름 → IP → 역조회로 첫 라벨이 같은 이름), 조회하지 못하면 -domain 을 붙인다.
func registrationName(host, domain string) (string, string) {
	if net.ParseIP(host) != nil || strings.Contains(host, ".") {
		return host, "입력한 이름 그대로"
	}
	// IPv6 는 쓰지 않는 환경이라 IPv4 주소만 조회한다(AAAA 응답이 섞여 엉뚱한 이름이 나오는 것 방지).
	if ips, err := net.DefaultResolver.LookupIP(context.Background(), "ip4", host); err == nil {
		for _, ip := range ips {
			a := ip.String()
			names, err := net.LookupAddr(a)
			if err != nil {
				continue
			}
			for _, n := range names {
				n = strings.TrimSuffix(n, ".")
				if strings.Contains(n, ".") && strings.EqualFold(strings.SplitN(n, ".", 2)[0], host) {
					return n, "FQDN 조회"
				}
			}
		}
	}
	if domain != "" {
		return host + "." + strings.TrimPrefix(domain, "."), "도메인 " + domain + " 붙임"
	}
	return host, "도메인 없이 등록"
}
