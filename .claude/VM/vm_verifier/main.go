// main.go: vm-verifier — VM 생성 직후(OS 설치/파워온 전) 작업자가 수동 실행해서,
// vCenter vNIC MAC과 DHCP 예약 MAC을 대조해 오설치(DHCP MAC 오기입으로 인한 역설치)를
// 탐지한다. (PLAN.md 참고)
//
// -vcenterList로 지정된 모든 vCenter를 병렬로 조회하고, -f 파일에 나열된 BM 접두어마다
// vCenter에 실제 등록된 {prefix}ev\d+ 패턴의 VM을 전부(자동 개수 파악) 찾아 병렬로 검증한다.
// DHCP 대역 파일은 -subnet 옵션 없이, 각 hostname을 DNS로 조회해 IP의 앞 3옥텟으로 자동 결정한다.
//
// 실행 예:
//
//	VC_USER=administrator@vsphere.local VC_PASS='...' \
//	  ./vm-verifier -vcenterList vcenter.txt -f targets.txt
//	  ./vm-verifier -vcenterList vcenter.txt -f targets.txt -failonly   # 이상 있는 것만 출력
package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"time"

	"vm-verifier/auditlog"
	"vm-verifier/dhcp"
	"vm-verifier/vc"
	"vm-verifier/verify"
)

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

// readLines는 파일을 한 줄에 하나씩 읽는다. 빈 줄과 '#' 주석은 무시한다.
func readLines(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var lines []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		lines = append(lines, line)
	}
	return lines, sc.Err()
}

// group은 BM 접두어 하나와 그 접두어에 실제로 매칭된 VM(hostname) 목록이다.
type group struct {
	prefix    string
	hostnames []string
}

type job struct {
	hostname string
	vmInfo   vc.VMInfo
	dhcpRec  dhcp.Record
	dhcpErr  error
	swapNote string
}

func main() {
	vcenterListPath := flag.String("vcenterList", "vcenter.txt", "vCenter 주소 목록 파일 (한 줄에 하나)")
	targetsPath := flag.String("f", "", "검증할 BM 접두어 목록 파일 (한 줄에 하나, '#' 주석 가능, 필수)")
	dhcpRoot := flag.String("dhcp-root", "/user/caedhcp", "DHCP 설정 파일 루트 경로")
	failOnly := flag.Bool("failonly", false, "PASS는 출력하지 않고 FAIL만 출력. 전부 PASS면 요약 한 줄만 출력")
	flag.Parse()

	if *targetsPath == "" {
		log.Fatal("-f <대상 목록 파일> 은 필수입니다")
	}

	vcUser := firstNonEmpty(os.Getenv("VC_USER"), os.Getenv("VCENTER_USER"))
	vcPass := firstNonEmpty(os.Getenv("VC_PASS"), os.Getenv("VCENTER_PASS"))
	if vcUser == "" || vcPass == "" {
		log.Fatal("VC_USER/VC_PASS (또는 VCENTER_USER/VCENTER_PASS) 환경변수가 필요합니다")
	}

	// PLAN.md 4장: DHCP 루트 디렉토리 자체가 없으면 무조건 검증 실패(Block). 우회 통과 금지.
	if info, err := os.Stat(*dhcpRoot); err != nil || !info.IsDir() {
		log.Fatalf("[BLOCK] DHCP 루트 디렉토리 로드 실패(%s)", *dhcpRoot)
	}

	vcAddrs, err := readLines(*vcenterListPath)
	if err != nil {
		log.Fatalf("vCenter 목록 로드 실패(%s): %v", *vcenterListPath, err)
	}
	targets, err := readLines(*targetsPath)
	if err != nil {
		log.Fatalf("대상 목록 로드 실패(%s): %v", *targetsPath, err)
	}

	// 1) 모든 vCenter를 병렬로 조회해서 VM 정보를 한데 합친다.
	allVMs := fetchAllVMs(vcAddrs, vcUser, vcPass)

	// 2) 접두어마다 실제 등록된 VM을 개수 제한 없이 전부 찾는다.
	exitFail := false
	var groups []group
	for _, prefix := range targets {
		re := regexp.MustCompile("^" + regexp.QuoteMeta(prefix) + `ev\d+$`)
		var matched []string
		for name := range allVMs {
			if re.MatchString(name) {
				matched = append(matched, name)
			}
		}
		if len(matched) == 0 {
			exitFail = true
			fmt.Printf("[%s*] FAIL — 조회된 vCenter들 안에 해당 접두어의 VM이 하나도 없음\n", prefix)
			auditlog.Write(".", time.Now(), verify.Result{Hostname: prefix + "*", Status: verify.Fail,
				Detail: "해당 접두어의 VM이 하나도 없음"})
			continue
		}
		groups = append(groups, group{prefix, matched})
	}

	// 3) 그룹별로 DHCP 레코드를 (DNS로 대역 자동 판별해서) 병렬로 읽고, 형제끼리 MAC이
	//    뒤바뀐 교차 설치(역설치)가 있는지 대조한 뒤 검증 작업 목록을 만든다.
	jobs := buildJobs(groups, allVMs, *dhcpRoot)

	if runJobs(jobs, *failOnly) {
		exitFail = true
	}

	if *failOnly && !exitFail && len(jobs) > 0 {
		fmt.Printf("검증 완료 — 이상 없음 (VM %d대, MAC 주소가 모두 DHCP 등록 정보와 일치)\n", len(jobs))
	}

	if exitFail {
		os.Exit(1)
	}
}

// fetchAllVMs는 vcAddrs의 모든 vCenter를 병렬로 접속해 VM 정보를 조회하고 하나로 합친다.
// 이름이 여러 vCenter에 걸쳐 중복되면 마지막으로 조회된 값으로 덮어써진다(README 알려진 제약).
func fetchAllVMs(vcAddrs []string, vcUser, vcPass string) map[string]vc.VMInfo {
	type vcResult struct {
		addr string
		vms  map[string]vc.VMInfo
		err  error
	}
	resCh := make(chan vcResult, len(vcAddrs))
	var wg sync.WaitGroup
	for _, addr := range vcAddrs {
		wg.Add(1)
		go func(addr string) {
			defer wg.Done()
			ctx := context.Background()
			client, err := vc.Connect(ctx, addr, vcUser, vcPass)
			if err != nil {
				resCh <- vcResult{addr, nil, err}
				return
			}
			defer client.Logout(ctx)
			vms, err := vc.FetchAll(ctx, client)
			resCh <- vcResult{addr, vms, err}
		}(addr)
	}
	wg.Wait()
	close(resCh)

	allVMs := make(map[string]vc.VMInfo)
	origin := make(map[string]string) // VM 이름 -> 그 값을 채운 vCenter 주소 (중복 탐지용)
	for r := range resCh {
		if r.err != nil {
			log.Printf("[경고] vCenter %s 조회 실패, 이 vCenter는 건너뜀: %v", r.addr, r.err)
			continue
		}
		for name, info := range r.vms {
			if prevAddr, dup := origin[name]; dup && prevAddr != r.addr {
				// vCenter 간 VM 이름은 보통 고유하지만(호스트가 unique), 혹시 겹치면 조용히
				// 덮어쓰지 않고 빨간 깜빡임 경고로 눈에 띄게 알린다.
				fmt.Printf("\033[5;31m[경고] VM 이름 중복: %s 가 %s 와 %s 양쪽 vCenter에 모두 존재함 — 마지막 조회 값(%s)으로 덮어씀\033[0m\n",
					name, prevAddr, r.addr, r.addr)
			}
			allVMs[name] = info
			origin[name] = r.addr
		}
	}
	return allVMs
}

// buildJobs는 그룹별로 DHCP 레코드를 병렬 조회하고, 같은 그룹 형제끼리 MAC이 뒤바뀐
// 교차 설치(예: ev01이 ev02의 MAC으로, ev02가 ev01의 MAC으로 배포됨)를 탐지해 job을 만든다.
func buildJobs(groups []group, allVMs map[string]vc.VMInfo, dhcpRoot string) []job {
	concurrency := runtime.NumCPU()
	if concurrency > 16 {
		concurrency = 16
	}
	if concurrency < 1 {
		concurrency = 1
	}
	sem := make(chan struct{}, concurrency)

	var jobs []job
	for _, g := range groups {
		type resolved struct {
			hostname string
			rec      dhcp.Record
			err      error
		}
		results := make([]resolved, len(g.hostnames))
		var wg sync.WaitGroup
		for i, h := range g.hostnames {
			wg.Add(1)
			sem <- struct{}{}
			go func(i int, h string) {
				defer wg.Done()
				defer func() { <-sem }()
				rec, err := dhcp.Resolve(dhcpRoot, h)
				results[i] = resolved{h, rec, err}
			}(i, h)
		}
		wg.Wait()

		for i, res := range results {
			swapNote := ""
			if res.err == nil && !verify.ContainsFold(allVMs[res.hostname].MACs, res.rec.MAC) {
				// 자기 자신의 DHCP MAC과는 안 맞음 — 같은 그룹 형제 중 누구의 MAC과 맞는지 확인.
				for j, sib := range results {
					if j == i || sib.err != nil {
						continue
					}
					if verify.ContainsFold(allVMs[res.hostname].MACs, sib.rec.MAC) {
						swapNote = fmt.Sprintf("%s의 실제 MAC이 %s의 DHCP 예약 MAC과 일치 — 교차 설치(역설치) 의심",
							res.hostname, sib.hostname)
						break
					}
				}
			}
			jobs = append(jobs, job{
				hostname: res.hostname,
				vmInfo:   allVMs[res.hostname],
				dhcpRec:  res.rec,
				dhcpErr:  res.err,
				swapNote: swapNote,
			})
		}
	}
	return jobs
}

// runJobs는 검증 대상들을 worker pool(무제한 goroutine 금지)로 병렬 실행한다.
// failOnly면 PASS 결과는 콘솔에 출력하지 않는다(감사 로그는 원래도 FAIL만 기록).
// 콘솔 출력/감사 로그는 공유 자원이라 mutex로 보호한다.
func runJobs(jobs []job, failOnly bool) bool {
	concurrency := runtime.NumCPU()
	if concurrency > 16 {
		concurrency = 16
	}
	if concurrency < 1 {
		concurrency = 1
	}
	sem := make(chan struct{}, concurrency)

	var wg sync.WaitGroup
	var mu sync.Mutex
	exitFail := false
	now := time.Now()

	for _, j := range jobs {
		wg.Add(1)
		sem <- struct{}{}
		go func(j job) {
			defer wg.Done()
			defer func() { <-sem }()

			result := verify.Check(j.hostname, j.dhcpRec, j.dhcpErr, j.swapNote, j.vmInfo)

			mu.Lock()
			if !failOnly || result.Status == verify.Fail {
				fmt.Printf("%s : %s — %s\n", result.Hostname, result.Status, result.Detail)
			}
			if err := auditlog.Write(".", now, result); err != nil {
				log.Printf("감사 로그 기록 실패(치명적이지 않음): %v", err)
			}
			if result.Status == verify.Fail {
				exitFail = true
			}
			mu.Unlock()
		}(j)
	}
	wg.Wait()
	return exitFail
}
