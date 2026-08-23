package main

import (
	"bufio"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
)

// ★ pdsh 스타일의 [숫자-숫자] 패턴을 찾는 정규표현식
var hostRangeRegex = regexp.MustCompile(`(.*?)\[(\d+)-(\d+)\](.*)`)

// ★ /user/ 로 시작하는 경로(autofs 마운트 경로)가 명령어에 포함되어 있는지 확인
var autofsUserPathRegex = regexp.MustCompile(`(^|[\s"'])/user/`)

// ★ esxi[0001-0020] 패턴을 자동으로 확장해 주는 함수
func expandHostLine(line string) []string {
	matches := hostRangeRegex.FindStringSubmatch(line)
	if matches == nil {
		return []string{line}
	}

	prefix := matches[1]
	startStr := matches[2]
	endStr := matches[3]
	suffix := matches[4]

	start, _ := strconv.Atoi(startStr)
	end, _ := strconv.Atoi(endStr)

	if start > end {
		start, end = end, start
	}

	padLen := 0
	if strings.HasPrefix(startStr, "0") {
		padLen = len(startStr)
	}

	formatStr := "%s%d%s"
	if padLen > 0 {
		formatStr = fmt.Sprintf("%%s%%0%dd%%s", padLen)
	}

	var results []string
	for i := start; i <= end; i++ {
		hostPart := fmt.Sprintf(formatStr, prefix, i, suffix)
		results = append(results, expandHostLine(hostPart)...)
	}
	return results
}

func writeHostsToFile(filename string, hosts []string) {
	if len(hosts) == 0 {
		return
	}
	file, err := os.Create(filename)
	if err != nil {
		fmt.Printf("결과 파일 생성 실패 (%s): %v\n", filename, err)
		return
	}
	defer file.Close()
	for _, host := range hosts {
		file.WriteString(host + "\n")
	}
}

func getAuthMethods(keyPath string, password string) ([]ssh.AuthMethod, error) {
	if password != "" {
		return []ssh.AuthMethod{ssh.Password(password)}, nil
	}

	var signers []ssh.Signer
	var paths []string

	home, _ := os.UserHomeDir()

	if keyPath != "" {
		if strings.HasPrefix(keyPath, "~/") {
			paths = []string{filepath.Join(home, keyPath[2:])}
		} else {
			paths = []string{keyPath}
		}
	} else {
		paths = []string{
			filepath.Join(home, ".ssh", "id_ed25519"),
			filepath.Join(home, ".ssh", "id_ecdsa"),
			filepath.Join(home, ".ssh", "id_rsa"),
		}
	}

	for _, p := range paths {
		key, err := os.ReadFile(p)
		if err != nil {
			continue
		}

		signer, err := ssh.ParsePrivateKey(key)
		if err != nil {
			continue
		}
		signers = append(signers, signer)
	}

	if len(signers) == 0 {
		return nil, fmt.Errorf("사용 가능한 SSH 키를 찾을 수 없습니다 (~/.ssh/ 하위 확인)")
	}

	return []ssh.AuthMethod{ssh.PublicKeys(signers...)}, nil
}

func printPdshStyle(host string, output string, err error) {
	output = strings.TrimSpace(output)

	if err != nil {
		if output != "" {
			for _, line := range strings.Split(output, "\n") {
				fmt.Printf("%s: ERROR: %s\n", host, strings.TrimSpace(line))
			}
		} else {
			fmt.Printf("%s: ERROR: %v\n", host, err)
		}
		return
	}

	if output == "" {
		return
	}

	for _, line := range strings.Split(output, "\n") {
		fmt.Printf("%s: %s\n", host, strings.TrimSpace(line))
	}
}

// ★ [수정] ~/.profile 파일 내에 anaconda 문자열이 있는지 확인
func isAnacondaRunning(client *ssh.Client) bool {
	sess, err := client.NewSession()
	if err != nil {
		return false
	}
	defer sess.Close()

	// ~/.profile 에 anaconda 가 있으면 1, 없거나 파일이 없으면 0 출력
	checkCmd := `sh -c 'if grep -q anaconda ~/.profile 2>/dev/null; then echo 1; else echo 0; fi'`
	out, _ := sess.CombinedOutput(checkCmd)

	outStr := strings.TrimSpace(string(out))
	if outStr == "" {
		return false
	}

	// SSH 로그인 배너(MOTD)를 무시하고 무조건 마지막 줄의 '1' 또는 '0'만 추출
	lines := strings.Split(outStr, "\n")
	lastLine := strings.TrimSpace(lines[len(lines)-1])

	return lastLine == "1"
}

func runSSHCommand(host string, command string, user string, authMethods []ssh.AuthMethod, port string, timeout time.Duration, pmMode bool, wg *sync.WaitGroup, sem chan struct{}, successCount *int, failedHosts *[]string, refusedHosts *[]string, osInstallHosts *[]string, noSvrAutoHosts *[]string, mu *sync.Mutex) {
	defer wg.Done()
	sem <- struct{}{}
	defer func() { <-sem }()

	target := host
	if !strings.Contains(target, ":") {
		target = host + ":" + port
	}

	config := &ssh.ClientConfig{
		User:            user,
		Auth:            authMethods,
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         timeout,
	}

	// 1. SSH 접속 시도
	client, err := ssh.Dial("tcp", target, config)
	if err != nil {
		printPdshStyle(host, "", fmt.Errorf("SSH 접속 실패: %v", err))
		mu.Lock()
		if strings.Contains(strings.ToLower(err.Error()), "connection refused") {
			*refusedHosts = append(*refusedHosts, host)
		} else {
			*failedHosts = append(*failedHosts, host)
		}
		mu.Unlock()
		return
	}
	defer client.Close()

	// 2. ★ [수정] -pm 옵션이 있을 때만 ~/.profile 기반으로 OS 설치 중인지 검사
	if pmMode && isAnacondaRunning(client) {
		printPdshStyle(host, "OS 설치중 (~/.profile anaconda 감지)", nil)
		mu.Lock()
		*osInstallHosts = append(*osInstallHosts, host)
		mu.Unlock()
		return // 설치 중이면 명령 실행하지 않고 종료
	}

	// 3. 메인 명령어 실행용 세션 생성
	session, err := client.NewSession()
	if err != nil {
		printPdshStyle(host, "", fmt.Errorf("세션 생성 실패: %v", err))
		mu.Lock()
		*failedHosts = append(*failedHosts, host)
		mu.Unlock()
		return
	}
	defer session.Close()

	output, err := session.CombinedOutput(command)
	printPdshStyle(host, string(output), err)

	mu.Lock()
	*successCount++
	mu.Unlock()

	// ★ -pm 옵션: svrauto 디렉토리 마운트(존재) 여부 체크
	if pmMode {
		sess2, err := client.NewSession()
		if err == nil {
			checkPmCmd := `sh -c 'if [ -d /user/svrauto ]; then echo 1; else echo 0; fi'`
			out2, _ := sess2.CombinedOutput(checkPmCmd)
			sess2.Close()

			outStr2 := strings.TrimSpace(string(out2))
			lines := strings.Split(outStr2, "\n")
			lastLine := ""
			if len(lines) > 0 {
				lastLine = strings.TrimSpace(lines[len(lines)-1])
			}

			// 디렉토리가 없어서 0이 나오면 목록에 추가
			if lastLine != "1" {
				mu.Lock()
				*noSvrAutoHosts = append(*noSvrAutoHosts, host)
				mu.Unlock()
			}
		} else {
			mu.Lock()
			*noSvrAutoHosts = append(*noSvrAutoHosts, host)
			mu.Unlock()
		}
	}
}

// ★ autofs 안전장치: 명령어에 "/user/..." 로 시작하는 경로가 포함되어 있으면 true.
// "echo 'user'" 처럼 단어 중간에 user가 들어간 경우는 대상이 아니고,
// 반드시 "/user/" 형태의 경로여야만 감지된다.
const autofsSafeConcurrency = 500

func isAutofsUserPath(command string) bool {
	return autofsUserPathRegex.MatchString(command)
}

func main() {
	hostFile := flag.String("w", "", "호스트 목록 파일 경로")
	user := flag.String("u", "root", "SSH 접속 계정")
	password := flag.String("p", "", "SSH 접속 비밀번호")
	keyPath := flag.String("i", "", "SSH 키 파일 경로")
	port := flag.String("P", "22", "SSH 포트")
	concurrency := flag.Int("c", 1000, "동시 접속 수")
	forceConcurrency := flag.Int("cf", 0, "동시 접속 수 강제 지정 (autofs /user/ 경로 감지로 인한 자동 제한을 무시)")
	timeoutSec := flag.Int("t", 15, "접속 타임아웃 초 (기본 15초)")
	dangerConfirm := flag.Bool("dnlgjawkrdjqghkrdls", false, "위험 작업 강제 실행 확인 옵션")
	scriptMode := flag.Bool("script", false, "작업 요약 출력 숨김 (순수 결과만 출력)")
	pmMode := flag.Bool("pm", false, "/user/svrauto 마운트 상태 추가 점검 및 OS설치중 감지")

	flag.Parse()

	args := flag.Args()
	if *hostFile == "" || len(args) == 0 {
		fmt.Println("사용법: ./gossh -w kdh.txt cat /etc/os-release")
		os.Exit(1)
	}

	authMethods, err := getAuthMethods(*keyPath, *password)
	if err != nil {
		log.Fatalf("SSH 인증 설정 오류: %v\n", err)
	}

	cleanHostFile := strings.TrimPrefix(*hostFile, "^")

	command := strings.Join(args, " ")
	command = strings.Trim(command, "\"'")

	// ★ autofs 안전장치: /user/ 경로가 명령어에 포함되어 있으면 병렬 수를 500으로 강제.
	// -cf 로 명시적으로 병렬 수를 지정한 경우에만 이 제한을 무시하고 지정값을 그대로 쓴다.
	effectiveConcurrency := *concurrency
	if *forceConcurrency > 0 {
		effectiveConcurrency = *forceConcurrency
	} else if isAutofsUserPath(command) {
		effectiveConcurrency = autofsSafeConcurrency
		fmt.Printf("[안전장치] 명령어에 \"/user/\" 경로가 감지되어 병렬 실행 수를 %d대로 자동 제한합니다. (원래 지정값 무시: -c %d)\n", autofsSafeConcurrency, *concurrency)
		fmt.Printf("           이 경로가 autofs 마운트가 아니거나 더 높은 병렬 수가 필요하면 -cf <숫자> 옵션으로 강제 지정하세요.\n")
	}

	lowerCmd := strings.ToLower(command)
	isDangerous := strings.Contains(lowerCmd, "reboot") ||
		strings.Contains(lowerCmd, "poweroff") ||
		strings.Contains(lowerCmd, "shutdown") ||
		strings.Contains(lowerCmd, "halt") ||
		strings.Contains(lowerCmd, "init 0") ||
		strings.Contains(lowerCmd, "init 6") ||
		strings.Contains(lowerCmd, "ddc")

	if isDangerous && !*dangerConfirm {
		fmt.Println("================================================================")
		fmt.Println(" [경고] 위험 작업(시스템 종료/재부팅)이 감지되었습니다!")
		fmt.Println("================================================================")
		fmt.Printf(" 감지된 명령어 : %s\n", command)
		fmt.Println(" 실행을 원하신다면 명령어에 '-dnlgjawkrdjqghkrdls' 옵션을 추가하세요.")
		fmt.Println(" 예시) ./gossh -dnlgjawkrdjqghkrdls -w kdh.txt reboot")
		os.Exit(1)
	}

	file, err := os.Open(cleanHostFile)
	if err != nil {
		log.Fatalf("호스트 파일을 열 수 없습니다: %v\n", err)
	}
	defer file.Close()

	var hosts []string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line != "" && !strings.HasPrefix(line, "#") {
			for _, token := range strings.Split(line, ",") {
				token = strings.TrimSpace(token)
				if token != "" {
					expanded := expandHostLine(token)
					hosts = append(hosts, expanded...)
				}
			}
		}
	}

	if len(hosts) == 0 {
		fmt.Printf("경고: %s 파일에 등록된 호스트가 없습니다.\n", cleanHostFile)
		os.Exit(0)
	}

	// 고유 호스트 중복 제거
	hostMap := make(map[string]bool)
	var uniqueHosts []string
	for _, h := range hosts {
		if !hostMap[h] {
			hostMap[h] = true
			uniqueHosts = append(uniqueHosts, h)
		}
	}
	hosts = uniqueHosts

	if *dangerConfirm {
		fmt.Println("\n================================================================")
		fmt.Printf(" [주의] 위험 작업 옵션이 활성화되었습니다. 실행 명령어: %s\n", command)
		fmt.Printf(" 대상 호스트 (총 %d대):\n", len(hosts))
		fmt.Println("================================================================")
		for _, h := range hosts {
			fmt.Printf(" - %s\n", h)
		}
		fmt.Println("================================================================")
		fmt.Println(" 작업대상이 맞는지 다시한번더 확인하세요. 실수를 하게되면 회사 전체직원의 100만원이 증발됩니다.")
		fmt.Print("정말로 위 서버들에 명령을 실행하시겠습니까? (y/N): ")

		reader := bufio.NewReader(os.Stdin)
		response, _ := reader.ReadString('\n')
		response = strings.ToLower(strings.TrimSpace(response))

		if response != "y" && response != "yes" {
			fmt.Println("작업이 취소되었습니다.")
			os.Exit(0)
		}
		fmt.Println("\n승인되었습니다. 작업을 시작합니다...")
	}

	var wg sync.WaitGroup
	sem := make(chan struct{}, effectiveConcurrency)
	timeout := time.Duration(*timeoutSec) * time.Second

	var mu sync.Mutex
	var successCount int
	var failedHosts []string
	var refusedHosts []string
	var osInstallHosts []string
	var noSvrAutoHosts []string // ★ /user/svrauto 미접근 호스트 기록

	startTime := time.Now()

	for _, host := range hosts {
		wg.Add(1)
		go runSSHCommand(host, command, *user, authMethods, *port, timeout, *pmMode, &wg, sem, &successCount, &failedHosts, &refusedHosts, &osInstallHosts, &noSvrAutoHosts, &mu)
	}

	wg.Wait()

	// 결과 파일 생성 및 저장
	offFilename := cleanHostFile + "_res_off"
	refusedFilename := cleanHostFile + "_res_refsed"
	osInstallFilename := cleanHostFile + "_os_install"
	noSvrAutoFilename := cleanHostFile + "_nosvrauto"

	writeHostsToFile(offFilename, failedHosts)
	writeHostsToFile(refusedFilename, refusedHosts)
	writeHostsToFile(osInstallFilename, osInstallHosts)
	writeHostsToFile(noSvrAutoFilename, noSvrAutoHosts)

	// -script 옵션이 없을 때만 요약 출력
	if !*scriptMode {
		fmt.Printf("\n================= 작업 요약 =================\n")
		fmt.Printf("총 대상 서버 : %d 대 (소요시간: %v)\n", len(hosts), time.Since(startTime))
		fmt.Printf(" 동시 접속 수 : %d\n", effectiveConcurrency)
		fmt.Printf(" 정상 접속 가능 : %d 대\n", successCount)

		// OS 설치 중 출력 (-pm 옵션을 준 경우에만 기록되므로 바로 출력)
		if len(osInstallHosts) > 0 {
			fmt.Printf(" OS 설치중 : %d 대\n", len(osInstallHosts))
			for _, h := range osInstallHosts {
				fmt.Printf("   - %s\n", h)
			}
			fmt.Printf("  -> %s 에 목록 저장됨\n", osInstallFilename)
		}

		// ★ pm 옵션을 사용했고, 미접근 서버가 존재하는 경우 출력
		if *pmMode && len(noSvrAutoHosts) > 0 {
			fmt.Printf(" svrauto미접근 : 접속가능 %d대 중 %d대\n", successCount, len(noSvrAutoHosts))
			fmt.Printf("  -> %s 에 목록 저장됨\n", noSvrAutoFilename)
		}

		fmt.Printf(" 접속 불가(Timeout 등) : %d 대", len(failedHosts))
		if len(failedHosts) > 0 {
			fmt.Printf("\n  -> %s 에 목록 저장됨", offFilename)
		}

		fmt.Printf("\n Refused(포트 닫힘) : %d 대", len(refusedHosts))
		if len(refusedHosts) > 0 {
			fmt.Printf("\n  -> %s 에 목록 저장됨", refusedFilename)
		}
		fmt.Printf("\n=============================================\n")
	}
}
