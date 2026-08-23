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
	"github.com/vmware/govmomi/object"
	"github.com/vmware/govmomi/view"
	"github.com/vmware/govmomi/vim25/mo"
)

// 파일의 각 줄을 읽어 배열로 반환 (BM 호스트명 목록)
func readLines(path string) ([]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	var lines []string
	scanner := bufio.NewScanner(file)
	first := true
	for scanner.Scan() {
		line := scanner.Text()
		if first {
			// 메모장 등에서 저장된 UTF-8 BOM 제거 (겉보기엔 똑같아 보여도 문자열이 달라지는 주범)
			line = strings.TrimPrefix(line, "\ufeff")
			first = false
		}
		line = strings.TrimSpace(line)
		if line != "" && !strings.HasPrefix(line, "#") {
			lines = append(lines, line)
		}
	}
	return lines, scanner.Err()
}

func main() {
	// =========================================================================
	// 1. 파라미터(Flag) 설정
	// =========================================================================
	vcId := flag.String("id", "lscsystems@vsphere.local", "vCenter 로그인 계정 ID")
	vcTargetIP := flag.String("vcTargetIP", "", "vCenter 접속 IP")
	hostListFile := flag.String("hostListFile", "hostlist.txt", "BM 호스트명 목록 파일")
	vmCount := flag.Int("vmCount", 1, "BM 호스트 1개당 생성된 VM 수량 (ev01, ev02... 순으로 대상 지정)")

	// ev01, ev02, ev03... 순번마다 콤마(,)로 구분해서 각각 다른 값을 받는다.
	// 예) -vmCount=2 이면 앞의 2개 값만 필수이고, 3번째(ev03) 값은 없어도 된다.
	deptNames := flag.String("deptNames", "", "ev01,ev02,ev03... 순번별 DEPT_NAME 값 (콤마로 구분)")
	purposes := flag.String("purposes", "", "ev01,ev02,ev03... 순번별 PURPOSE 값 (콤마로 구분)")
	vmTypes := flag.String("vmTypes", "", "ev01,ev02,ev03... 순번별 VM_TYPE 값 (콤마로 구분)")
	flag.Parse()

	if *vcTargetIP == "" {
		log.Fatal("필수 파라미터(-vcTargetIP)가 누락되었습니다.")
	}
	if *vmCount < 1 {
		log.Fatal("-vmCount는 1 이상이어야 합니다.")
	}

	// splitList는 콤마로 구분된 값을 잘라 순번(1,2,3...)별 값으로 매핑한다.
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

	// vmCount만큼만 필수로 존재하는지 검증 (그 이후 순번은 필요 없으므로 검사하지 않음)
	validateList := func(name string, list []string) {
		if len(list) < *vmCount {
			log.Fatalf("-%s 값이 부족합니다. vmCount=%d 이면 최소 %d개의 값이 필요합니다 (현재 %d개).", name, *vmCount, *vmCount, len(list))
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

	// =========================================================================
	// 2. 대상 VM 이름 목록 생성 (BM 호스트명 + ev01, ev02, ev03 ...)
	//    각 VM은 순번(idx)을 함께 갖고 있어서, 그 순번에 맞는 속성 값을 사용한다.
	// =========================================================================
	type vmTarget struct {
		name string
		idx  int // 1부터 시작 (ev01 -> 1)
	}

	var targets []vmTarget
	for _, host := range hostLines {
		for i := 1; i <= *vmCount; i++ {
			targets = append(targets, vmTarget{
				name: fmt.Sprintf("%sev%02d", host, i),
				idx:  i,
			})
		}
	}

	fmt.Printf("\n[INFO] 사용자 지정 특성 설정 시작 (대상 VM %d대, 접속 계정: %s)\n\n", len(targets), *vcId)

	// =========================================================================
	// 3. vCenter 접속
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

	// =========================================================================
	// 4. vCenter 루트부터 모든 폴더/데이터센터를 재귀적으로 탐색해서
	//    전체 VM 목록(이름 -> ManagedObjectReference)을 한 번에 인덱싱한다.
	//    (데이터센터를 지정할 필요가 없어지고, "please specify a datacenter" 에러도 사라짐)
	// =========================================================================
	viewMgr := view.NewManager(client.Client)
	cv, err := viewMgr.CreateContainerView(ctx, client.ServiceContent.RootFolder, []string{"VirtualMachine"}, true)
	if err != nil {
		log.Fatalf("전체 VM 탐색용 ContainerView 생성 실패: %v", err)
	}
	defer cv.Destroy(ctx)

	var allVMs []mo.VirtualMachine
	if err := cv.Retrieve(ctx, []string{"VirtualMachine"}, []string{"name"}, &allVMs); err != nil {
		log.Fatalf("전체 VM 목록 조회 실패: %v", err)
	}

	vmIndex := make(map[string][]mo.VirtualMachine)
	for _, v := range allVMs {
		vmIndex[v.Name] = append(vmIndex[v.Name], v)
	}
	fmt.Printf("[INFO] vCenter 전체에서 VM %d대 인덱싱 완료\n\n", len(allVMs))

	// =========================================================================
	// 5. VM별 사용자 지정 특성 병렬 설정
	// =========================================================================
	var wg sync.WaitGroup
	var mu sync.Mutex
	var failed []string

	for _, target := range targets {
		wg.Add(1)
		go func(target vmTarget) {
			defer wg.Done()

			matches, found := vmIndex[target.name]
			if !found || len(matches) == 0 {
				fmt.Printf("  -> [%s] VM을 찾을 수 없음 (SKIP)\n", target.name)
				mu.Lock()
				failed = append(failed, target.name)
				mu.Unlock()
				return
			}
			if len(matches) > 1 {
				fmt.Printf("  -> [%s] 동일한 이름의 VM이 %d개 존재해 모호함 (SKIP)\n", target.name, len(matches))
				mu.Lock()
				failed = append(failed, target.name+"(중복)")
				mu.Unlock()
				return
			}

			vm := object.NewVirtualMachine(client.Client, matches[0].Self)

			// 순번(idx)에 해당하는 값 사용 (ev01 -> 리스트의 1번째 값)
			values := map[string]string{
				"DEPT_NAME": deptList[target.idx-1],
				"PURPOSE":   purposeList[target.idx-1],
				"VM_TYPE":   vmTypeList[target.idx-1],
			}

			// 사용자 지정 특성(Custom Attribute)이 vCenter에 미리 정의되어 있어야 함
			// (없으면 govmomi가 자동으로 필드를 생성하지 않고 에러를 반환할 수 있음)
			for key, value := range values {
				if setErr := vm.SetCustomValue(ctx, key, value); setErr != nil {
					fmt.Printf("  -> [%s] %s 설정 실패: %v\n", target.name, key, setErr)
					mu.Lock()
					failed = append(failed, fmt.Sprintf("%s(%s)", target.name, key))
					mu.Unlock()
					continue
				}
			}
			fmt.Printf("  -> [%s] 설정 완료 (DEPT_NAME=%s, PURPOSE=%s, VM_TYPE=%s)\n", target.name, values["DEPT_NAME"], values["PURPOSE"], values["VM_TYPE"])
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
