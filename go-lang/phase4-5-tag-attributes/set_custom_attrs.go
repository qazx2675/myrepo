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

	"github.com/vmware/govmomi"
	"github.com/vmware/govmomi/find"
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
	vcTargetIP := flag.String("vcTargetIP", "", "vCenter 접속 IP")
	hostListFile := flag.String("hostListFile", "hostlist.txt", "BM 호스트명 목록 파일")
	vmCount := flag.Int("vmCount", 1, "BM 호스트 1개당 생성된 VM 수량")
	deptNames := flag.String("deptNames", "", "ev01,ev02,ev03... 순번별 DEPT_NAME 값 (콤마 구분)")
	purposes := flag.String("purposes", "", "ev01,ev02,ev03... 순번별 PURPOSE 값 (콤마 구분)")
	vmTypes := flag.String("vmTypes", "", "ev01,ev02,ev03... 순번별 VM_TYPE 값 (콤마 구분)")
	flag.Parse()

	if *vcTargetIP == "" {
		log.Fatal("필수 파라미터(-vcTargetIP)가 누락되었습니다.")
	}
	if *vmCount < 1 {
		log.Fatal("-vmCount는 1 이상이어야 합니다.")
	}

	splitList := func(raw string) []string {
		if raw == "" {
			return nil
		}
		parts := strings.Split(raw, ",")
		for i := range parts {
			parts[i] = strings.TrimSpace(parts[i])
		}
		return parts
	}
	deptList := splitList(*deptNames)
	purposeList := splitList(*purposes)
	vmTypeList := splitList(*vmTypes)

	validateList := func(name string, list []string) {
		if len(list) < *vmCount {
			log.Fatalf("-%s 값이 부족합니다. vmCount=%d 이면 최소 %d개 필요 (현재 %d개).", name, *vmCount, *vmCount, len(list))
		}
	}
	validateList("deptNames", deptList)
	validateList("purposes", purposeList)
	validateList("vmTypes", vmTypeList)

	vcPassword := os.Getenv("VC_PASSWORD")
	if vcPassword == "" {
		log.Fatal("환경변수 'VC_PASSWORD'가 설정되지 않았습니다.")
	}

	baseDir, _ := os.Getwd()
	hostLines, err := readLines(filepath.Join(baseDir, *hostListFile))
	if err != nil {
		log.Fatalf("파일 로드 실패 (%s): %v", *hostListFile, err)
	}

	type vmTarget struct {
		name string
		idx  int
	}
	var targets []vmTarget
	for _, host := range hostLines {
		for i := 1; i <= *vmCount; i++ {
			targets = append(targets, vmTarget{name: fmt.Sprintf("%sev%02d", host, i), idx: i})
		}
	}

	fmt.Printf("\n[INFO] 사용자 지정 특성 설정 시작 (대상 VM %d대, 접속 계정: %s)\n\n", len(targets), *vcId)

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

	var wg sync.WaitGroup
	var mu sync.Mutex
	var failed []string
	for _, target := range targets {
		wg.Add(1)
		go func(target vmTarget) {
			defer wg.Done()
			vm, err := finder.VirtualMachine(ctx, target.name)
			if err != nil {
				fmt.Printf(" -> [%s] VM을 찾을 수 없음 (SKIP)\n", target.name)
				mu.Lock()
				failed = append(failed, target.name)
				mu.Unlock()
				return
			}
			values := map[string]string{
				"DEPT_NAME": deptList[target.idx-1],
				"PURPOSE":   purposeList[target.idx-1],
				"VM_TYPE":   vmTypeList[target.idx-1],
			}
			for key, value := range values {
				if setErr := vm.SetCustomValue(ctx, key, value); setErr != nil {
					fmt.Printf(" -> [%s] %s 설정 실패: %v\n", target.name, key, setErr)
					mu.Lock()
					failed = append(failed, fmt.Sprintf("%s(%s)", target.name, key))
					mu.Unlock()
					continue
				}
			}
			fmt.Printf(" -> [%s] 설정 완료 (DEPT_NAME=%s, PURPOSE=%s, VM_TYPE=%s)\n",
				target.name, values["DEPT_NAME"], values["PURPOSE"], values["VM_TYPE"])
		}(target)
	}
	wg.Wait()
	if len(failed) > 0 {
		fmt.Printf("\n[일부 실패] %s\n", strings.Join(failed, ", "))
	} else {
		fmt.Println("\n[성공] 모든 VM에 사용자 지정 특성 설정이 완료되었습니다.")
	}
	fmt.Println("vCenter 세션을 안전하게 종료했습니다.")
}
