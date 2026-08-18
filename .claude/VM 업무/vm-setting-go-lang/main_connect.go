package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/vmware/govmomi"
	"github.com/vmware/govmomi/find"
	"github.com/vmware/govmomi/object"
	"github.com/vmware/govmomi/task"
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
	// 5. 이미 등록된 호스트인지 병렬로 확인
	// =========================================================================
	// existsResults는 serverLines와 같은 순서/길이로 미리 슬롯을 만들어 두고
	// 고루틴이 자신의 인덱스에만 쓰도록 해서 인덱스 슬라이스 쓰기 충돌을 없앤다.
	// (map을 여러 고루틴이 동시에 쓰면 레이스가 나지만, 인덱스가 겹치지 않는
	// 슬라이스 쓰기는 안전하다.)
	existsResults := make([]bool, len(serverLines))

	var wgCheck sync.WaitGroup
	semCheck := make(chan struct{}, *concurrency)

	for i, host := range serverLines {
		wgCheck.Add(1)
		semCheck <- struct{}{}

		go func(i int, host string) {
			defer wgCheck.Done()
			defer func() { <-semCheck }()

			// finder는 고루틴마다 새로 만든다 — find.Finder는 동시 사용에
			// 안전하다고 문서화되어 있지 않고, 내부적으로 폴더/데이터센터
			// 캐시를 갖고 있어 공유 시 레이스 위험이 있기 때문이다.
			// SetDatacenter도 반드시 각자 다시 호출해야 한다 (finder 인스턴스별 상태).
			f := find.NewFinder(client.Client, true)
			f.SetDatacenter(selectedDC)
			_, hostErr := f.HostSystem(ctx, host)
			existsResults[i] = hostErr == nil
		}(i, host)
	}
	wgCheck.Wait()

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

			safePrintf("  -> [%s] 등록 Task 발급 중...\n", host)
			// spec은 고루틴 로컬 변수 — 각 호출마다 독립된 값이라
			// 여러 고루틴이 동시에 등록을 진행해도 서로의 spec을 건드리지 않는다.
			spec := types.HostConnectSpec{
				HostName: host,
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
