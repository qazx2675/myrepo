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
	"strings"

	"github.com/vmware/govmomi"
	"github.com/vmware/govmomi/find"
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

func main() {
	vcId := flag.String("id", "lscsystems@vsphere.local", "vCenter 로그인 계정 ID")
	vcTargetIP := flag.String("vcTargetIP", "", "vCenter 접속 IP (필수)")
	worklistFile := flag.String("worklistFile", "worklist.txt", "작업 대상 호스트 목록 파일")
	arg1 := flag.String("arg1", "", "배포 인자 1 (필수)")
	argInt := flag.Int("argInt", 0, "배포 정수 인자 (필수)")
	argStr := flag.String("argStr", "", "배포 문자 인자 (필수)")

	flag.Parse()

	if *vcTargetIP == "" || *arg1 == "" || *argStr == "" {
		log.Fatal("필수 파라미터가 누락되었습니다. (-vcTargetIP, -arg1, -argStr 확인)")
	}

	vcPassword := os.Getenv("VC_PASSWORD")
	if vcPassword == "" {
		log.Fatal("인증 정보 로드 실패: VC_PASSWORD 환경 변수가 설정되지 않았습니다.")
	}

	baseDir, _ := os.Getwd()
	worklistPath := filepath.Join(baseDir, *worklistFile)

	if _, err := os.Stat(worklistPath); os.IsNotExist(err) {
		log.Fatalf("작업에 필요한 %s 파일을 찾을 수 없습니다.", *worklistFile)
	}

	serverList, err := readLines(worklistPath)
	if err != nil {
		log.Fatalf("worklist 파일 읽기 실패: %v", err)
	}

	fmt.Printf("[Phase 4] 백그라운드 MAC/IP 추출 및 프로비저닝 리스트 생성을 시작합니다. (접속 계정: %s)\n", *vcId)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	u := &url.URL{Scheme: "https", Host: *vcTargetIP, Path: "/sdk"}
	u.User = url.UserPassword(*vcId, vcPassword)

	client, err := govmomi.NewClient(ctx, u, true)
	if err != nil {
		log.Fatalf("오류: vCenter 접속 실패: %v", err)
	}
	defer func() {
		_ = client.Logout(ctx)
	}()

	finder := find.NewFinder(client.Client, true)
	dc, _ := finder.DefaultDatacenter(ctx)
	finder.SetDatacenter(dc)

	var safeVmPatterns []string
	for _, bmHost := range serverList {
		baseName := strings.Split(bmHost, ".")[0]
		pattern := regexp.QuoteMeta(baseName) + `ev\d+`
		safeVmPatterns = append(safeVmPatterns, pattern)
	}

	vmFilterPattern := "^(" + strings.Join(safeVmPatterns, "|") + ")$"
	regexMatcher := regexp.MustCompile(vmFilterPattern)

	allVms, err := finder.VirtualMachineList(ctx, "*")
	if err != nil {
		log.Fatalf("VM 목록을 가져오는 중 오류 발생: %v", err)
	}

	var formattedResults []string

	for _, vm := range allVms {
		if regexMatcher.MatchString(vm.Name()) {
			var vmProps mo.VirtualMachine
			err := vm.Properties(ctx, vm.Reference(), []string{"config.hardware.device"}, &vmProps)
			if err != nil {
				continue
			}

			mac := "MAC_NOT_FOUND"
			for _, dev := range vmProps.Config.Hardware.Device {
				if nic, ok := dev.(types.BaseVirtualEthernetCard); ok {
					mac = nic.GetVirtualEthernetCard().MacAddress
					break
				}
			}

			ip := fmt.Sprintf("%s_DNS_AND_TOOLS_NOT_FOUND", vm.Name())
			line := fmt.Sprintf("VM VM %s %s %s %s eth0 sda sda5 %d %s uefi", *arg1, vm.Name(), ip, mac, *argInt, *argStr)
			formattedResults = append(formattedResults, line)
		}
	}

	safeIpName := strings.ReplaceAll(*vcTargetIP, ".", "_")
	outputFileName := fmt.Sprintf("Provisioning_List_%s.txt", safeIpName)
	outputPath := filepath.Join(baseDir, outputFileName)

	file, err := os.Create(outputPath)
	if err != nil {
		log.Fatalf("파일 생성 실패: %v", err)
	}
	defer file.Close()

	fmt.Println("\n================ [ 최종 생성 리스트 (Kickstart) ] ================")
	for _, res := range formattedResults {
		fmt.Println(res)
		_, _ = file.WriteString(res + "\n")
	}
	fmt.Println("====================================================================")
	fmt.Printf("\nOS 설치 배포용 텍스트 파일 저장 완료: %s\n", outputPath)
}
