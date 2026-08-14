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
// 자가서명 인증서(폐쇄망 환경) 때문에 SSLVerifyFault가 나면, 에러에 담긴 실제 thumbprint를
// 꺼내 spec에 채운 뒤 자동으로 한 번 재시도한다.
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
	var taskErr task.Error
	if errors.As(waitErr, &taskErr) {
		if sslFault, ok := taskErr.Fault().(*types.SSLVerifyFault); ok {
			fmt.Printf(" -> [%s] SSL 인증서 미신뢰 감지, thumbprint(%s) 적용 후 재시도\n", host, sslFault.Thumbprint)
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
	vcId := flag.String("id", "lscsystems@vsphere.local", "vCenter 로그인 계정 ID")
	vcTargetIP := flag.String("vcTargetIP", "", "vCenter 접속 IP")
	folderName := flag.String("folderName", "", "대상 폴더, 클러스터, 또는 데이터센터 이름")
	worklistFile := flag.String("worklistFile", "worklist.txt", "VM 대상 목록 파일")
	flag.Parse()

	if *vcTargetIP == "" || *folderName == "" {
		log.Fatal("필수 파라미터(-vcTargetIP, -folderName)가 누락되었습니다.")
	}

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
	fmt.Printf("\n[INFO] 단독 실행: ESXi 호스트 병렬 등록 (Phase 1) 시작 (접속 계정: %s)\n", *vcId)

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

	var targetFolder *object.Folder
	var targetCluster *object.ClusterComputeResource
	if cluster, err := finder.ClusterComputeResource(ctx, *folderName); err == nil {
		targetCluster = cluster
		fmt.Printf("[INFO] 대상 감지 완료: 클러스터 [%s]\n", cluster.Name())
	} else if folder, err := finder.Folder(ctx, *folderName); err == nil {
		targetFolder = folder
		fmt.Printf("[INFO] 대상 감지 완료: 폴더 [%s]\n", folder.Name())
	} else if dc, err := finder.Datacenter(ctx, *folderName); err == nil {
		folders, _ := dc.Folders(ctx)
		targetFolder = folders.HostFolder
		fmt.Printf("[INFO] 대상 감지 완료: 데이터센터 [%s] (내부 HostFolder 사용)\n", dc.Name())
	} else {
		log.Fatalf("[오류] vCenter 내에 '%s' 위치를 찾을 수 없습니다.", *folderName)
	}

	var targets []string
	for _, host := range serverLines {
		if _, err := finder.HostSystem(ctx, host); err == nil {
			fmt.Printf(" -> [%s] 이미 등록됨 (PASS)\n", host)
			continue
		}
		targets = append(targets, host)
	}
	if len(targets) == 0 {
		fmt.Println("\n[안내] 새로 등록할 호스트가 없습니다.")
		fmt.Println("vCenter 세션을 안전하게 종료했습니다.")
		return
	}
	var wg sync.WaitGroup
	var mu sync.Mutex
	var failed []string
	for _, host := range targets {
		wg.Add(1)
		go func(host string) {
			defer wg.Done()
			fmt.Printf(" -> [%s] 등록 Task 발급 중...\n", host)
			spec := types.HostConnectSpec{HostName: host, UserName: "root", Password: esxiPassword, Force: true}
			if err := addHost(ctx, host, spec, targetCluster, targetFolder); err != nil {
				fmt.Printf(" -> [%s] 등록 실패: %v\n", host, err)
				mu.Lock()
				failed = append(failed, host)
				mu.Unlock()
				return
			}
			fmt.Printf(" -> [%s] 등록 완료\n", host)
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
