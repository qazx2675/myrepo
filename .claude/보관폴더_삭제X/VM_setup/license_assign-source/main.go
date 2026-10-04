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
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/vmware/govmomi"
	"github.com/vmware/govmomi/license"
	"github.com/vmware/govmomi/view"
	"github.com/vmware/govmomi/vim25/mo"
)

// 평가판 호스트 정보를 담을 구조체
type UnlicensedHost struct {
	MoRef string
	Name  string
	Cores int32
}

// 텍스트 파일 라인 리더 (worklist.txt)
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
	// 1. 파라미터(Flag) 설정
	// =========================================================================
	vcTargetIP := flag.String("vcTargetIP", "", "vCenter 접속 IP (필수)")
	vcId := flag.String("id", "lscsystems@vsphere.local", "vCenter 로그인 계정 ID")
	worklistFile := flag.String("worklistFile", "worklist.txt", "작업 대상 호스트 목록 파일명 (파라미터 입력)")
	flag.Parse()

	if *vcTargetIP == "" {
		log.Fatal("[오류] 필수 파라미터가 누락되었습니다. (-vcTargetIP)")
	}

	// 비밀번호는 환경 변수에서 로드 (보안 표준화)
	vcPassword := os.Getenv("VC_PASSWORD")
	if vcPassword == "" {
		log.Fatal("[오류] 인증 로드 실패: VC_PASSWORD 환경 변수가 설정되지 않았습니다.")
	}

	// 2. worklist.txt 로드
	baseDir, _ := os.Getwd()
	worklistPath := filepath.Join(baseDir, *worklistFile)
	if _, err := os.Stat(worklistPath); os.IsNotExist(err) {
		log.Fatalf("[오류] %s 파일을 찾을 수 없습니다.", *worklistFile)
	}

	hostlistLines, err := readLines(worklistPath)
	if err != nil {
		log.Fatalf("[오류] 호스트 목록 파일 읽기 실패: %v", err)
	}

	fmt.Printf("\nvCenter(%s)에 접속 중입니다...\n", *vcTargetIP)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 3. vCenter 접속 및 라이선스 매니저 로드
	u := &url.URL{Scheme: "https", Host: *vcTargetIP, Path: "/sdk"}
	u.User = url.UserPassword(*vcId, vcPassword)

	client, err := govmomi.NewClient(ctx, u, true)
	if err != nil {
		log.Fatalf("[오류] vCenter 접속 실패: %v", err)
	}
	defer client.Logout(ctx)

	// 라이선스 매니저 및 어사인먼트 매니저 초기화
	licMgr := license.NewManager(client.Client)
	assignmentMgr, err := licMgr.AssignmentManager(ctx)
	if err != nil {
		log.Fatalf("[오류] License Assignment Manager 로드 실패: %v", err)
	}

	// =========================================================================
	// 4. 물리 호스트 인벤토리 일괄 조회
	// =========================================================================
	m := view.NewManager(client.Client)
	v, err := m.CreateContainerView(ctx, client.ServiceContent.RootFolder, []string{"HostSystem"}, true)
	if err != nil {
		log.Fatalf("[오류] ContainerView 생성 실패: %v", err)
	}
	defer v.Destroy(ctx)

	var allHosts []mo.HostSystem
	err = v.Retrieve(ctx, []string{"HostSystem"}, []string{"name", "hardware.cpuInfo"}, &allHosts)
	if err != nil {
		log.Fatalf("[오류] HostSystem 정보 조회 실패: %v", err)
	}

	var targetHosts []mo.HostSystem
	for _, line := range hostlistLines {
		cleanName := strings.Split(strings.TrimSpace(line), ".")[0]
		for _, host := range allHosts {
			hostClean := strings.Split(host.Name, ".")[0]
			if strings.EqualFold(hostClean, cleanName) || host.Name == line {
				targetHosts = append(targetHosts, host)
			}
		}
	}

	if len(targetHosts) == 0 {
		fmt.Println("[경고] worklist.txt의 호스트를 인벤토리에서 찾을 수 없습니다.")
		os.Exit(1)
	}

	// =========================================================================
	// 5. [Step 1] 기존 라이선스 점검 및 평가판 필터링
	// =========================================================================
	var totalCoresNeeded int32 = 0
	var unlicensedHosts []UnlicensedHost
	skippedCount := 0

	fmt.Println("\n[1단계: 각 물리 호스트의 현재 라이선스 상태 점검]")

	evalRegex := regexp.MustCompile(`(?i)eval`)

	for _, host := range targetHosts {
		var cores int32 = 0

		if host.Hardware != nil {
			cores = int32(host.Hardware.CpuInfo.NumCpuCores)
		}

		// [완벽수정] 조회는 QueryAssigned 를 사용합니다.
		assignments, err := assignmentMgr.QueryAssigned(ctx, host.Reference().Value)

		isEval := true
		licName := ""

		if err == nil && len(assignments) > 0 {
			licInfo := assignments[0].AssignedLicense
			// 평가판 키이거나 이름에 eval이 들어있지 않으면 정식으로 간주
			if licInfo.LicenseKey != "00000-00000-00000-00000-00000" && !evalRegex.MatchString(licInfo.EditionKey) {
				isEval = false
				licName = licInfo.Name
			}
		}

		if isEval {
			totalCoresNeeded += cores
			unlicensedHosts = append(unlicensedHosts, UnlicensedHost{
				MoRef: host.Reference().Value,
				Name:  host.Name,
				Cores: cores,
			})
			fmt.Printf(" -> [작업 대상] %s (평가판 상태 / 필요 코어: %d 개)\n", host.Name, cores)
		} else {
			fmt.Printf(" -> [스킵] %s (이미 할당됨: %s)\n", host.Name, licName)
			skippedCount++
		}
	}

	fmt.Println("\n======================================================================")
	fmt.Println(" [점검 결과 요약]")
	fmt.Printf(" - 총 검색된 호스트 : %d 대\n", len(targetHosts))
	fmt.Printf(" - 정식 라이선스 보유 (스킵) : %d 대\n", skippedCount)
	fmt.Printf(" - 평가판 (작업 필요) : %d 대\n", len(unlicensedHosts))
	fmt.Printf(" - 작업을 위해 필요한 총 물리 코어 수 : %d 개\n", totalCoresNeeded)
	fmt.Println("======================================================================")

	// 평가판 호스트가 없으면 종료
	if len(unlicensedHosts) == 0 {
		fmt.Println("\n모든 장비에 정식 라이선스가 이미 등록되어 있습니다. 작업을 종료합니다.")
		os.Exit(0)
	}

	// =========================================================================
	// 6. [Step 2 & 3] 라이선스 선택 및 할당 (부족 시 추가 선택 스마트 루프)
	// =========================================================================
	reader := bufio.NewReader(os.Stdin)

	for len(unlicensedHosts) > 0 {
		// 매 루프마다 라이선스 잔여량을 새로고침 하기 위해 모 객체 조회
		var licMo mo.LicenseManager
		err = client.RetrieveOne(ctx, *client.ServiceContent.LicenseManager, []string{"licenses"}, &licMo)
		if err != nil {
			log.Fatalf("[오류] 실시간 라이선스 목록 가져오기 실패: %v", err)
		}
		licenses := licMo.Licenses

		fmt.Println("\n[현재 vCenter에 등록된 라이선스 목록]")

		// 표 형태로 깔끔하게 출력
		w := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', tabwriter.TabIndent)
		fmt.Fprintln(w, "Index(번호)\tName(제품명)\tKey(라이선스)\tTotal(총량)\tUsed(사용중)\tAvail(잔여)\t")

		for i, lic := range licenses {
			avail := lic.Total - lic.Used
			fmt.Fprintf(w, "[%d]\t%s\t%s\t%d\t%d\t%d\t\n", i, lic.Name, lic.LicenseKey, lic.Total, lic.Used, avail)
		}
		w.Flush()

		// 잔여 호스트/코어 현황 브리핑
		var remainCoreCount int32 = 0
		for _, uh := range unlicensedHosts {
			remainCoreCount += uh.Cores
		}
		fmt.Printf("-> 현재 라이선스가 필요한 남은 호스트: %d대 (필요 코어: %d 개)\n", len(unlicensedHosts), remainCoreCount)

		// 입력 프롬프트
		fmt.Print("\n적용할 라이선스 번호(Index)를 입력하세요 (취소하려면 'q' 또는 'Q' 입력): ")
		input, _ := reader.ReadString('\n')
		input = strings.TrimSpace(input)

		if strings.ToLower(input) == "q" {
			fmt.Println("라이선스 할당 작업을 중단합니다.")
			break
		}

		selIndexInt, err := strconv.Atoi(input)
		if err != nil || selIndexInt < 0 || selIndexInt >= len(licenses) {
			fmt.Println("[오류] 잘못된 번호입니다. 다시 입력해주세요.")
			continue
		}

		chosenLic := licenses[selIndexInt]
		currentAvail := chosenLic.Total - chosenLic.Used

		fmt.Printf("\n[%s] 라이선스를 호스트에 적용 시작...\n", chosenLic.Name)

		var nextRoundHosts []UnlicensedHost

		// 호스트 순회하며 라이선스 깎아가며 할당
		for _, hostObj := range unlicensedHosts {
			if currentAvail >= hostObj.Cores {
				// [완벽수정] 할당은 Update 를 사용합니다.
				_, err := assignmentMgr.Update(ctx, hostObj.MoRef, chosenLic.LicenseKey, hostObj.Name)
				if err != nil {
					fmt.Printf(" [실패] %s - %v\n", hostObj.Name, err)
					nextRoundHosts = append(nextRoundHosts, hostObj)
				} else {
					fmt.Printf(" [할당 완료] %s (소모 코어: %d)\n", hostObj.Name, hostObj.Cores)
					currentAvail -= hostObj.Cores
				}
			} else {
				// 잔여량이 부족하면 다음 루프를 위해 남겨둠
				nextRoundHosts = append(nextRoundHosts, hostObj)
			}
		}

		// 처리되지 못한 호스트 리스트 갱신
		unlicensedHosts = nextRoundHosts

		if len(unlicensedHosts) > 0 {
			fmt.Printf("\n[알림] 선택하신 라이선스의 잔여 용량이 부족하여 %d대의 호스트가 남았습니다.\n", len(unlicensedHosts))
			fmt.Println("나머지 호스트에 적용할 다른 라이선스를 이어서 선택해주세요.")
		}
	}

	if len(unlicensedHosts) == 0 {
		fmt.Println("\n모든 작업이 성공적으로 완료되었습니다!")
	}
}
