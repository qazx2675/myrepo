// gossh.go - pdsh 스타일 [숫자-숫자] 확장을 지원하는 SSH 병렬 실행 도구 (킥스타트 설치중 감지 포함)
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

var hostRangeRegex = regexp.MustCompile(`(.*?)\[(\d+)-(\d+)\](.*)`)

func expandHostLine(line string) []string {
	matches := hostRangeRegex.FindStringSubmatch(line)
	if matches == nil {
		return []string{line}
	}
	prefix, startStr, endStr, suffix := matches[1], matches[2], matches[3], matches[4]
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
		results = append(results, expandHostLine(fmt.Sprintf(formatStr, prefix, i, suffix))...)
	}
	return results
}

func writeHostsToFile(filename string, hosts []string) {
	if len(hosts) == 0 {
		return
	}
	file, err := os.Create(filename)
	if err != nil {
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
		paths = []string{filepath.Join(home, ".ssh", "id_ed25519"), filepath.Join(home, ".ssh", "id_ecdsa"), filepath.Join(home, ".ssh", "id_rsa")}
	}
	for _, p := range paths {
		key, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		if signer, err := ssh.ParsePrivateKey(key); err == nil {
			signers = append(signers, signer)
		}
	}
	if len(signers) == 0 {
		return nil, fmt.Errorf("사용 가능한 SSH 키를 찾을 수 없습니다")
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

// isAnacondaRunning: 커널 comm 정보 및 /mnt/sysimage 존재 여부로 킥스타트 설치중 여부 판별 (ps 외부명령 의존 제거)
func isAnacondaRunning(client *ssh.Client) bool {
	sess, err := client.NewSession()
	if err != nil {
		return false
	}
	defer sess.Close()
	checkCmd := `sh -c 'if grep -q anaconda /proc/*/comm 2>/dev/null || [ -d /mnt/sysimage ]; then echo 1; else echo 0; fi'`
	out, _ := sess.CombinedOutput(checkCmd)
	outStr := strings.TrimSpace(string(out))
	if outStr == "" {
		return false
	}
	lines := strings.Split(outStr, "\n")
	return strings.TrimSpace(lines[len(lines)-1]) == "1"
}

func runSSHCommand(host, command, user string, authMethods []ssh.AuthMethod, port string, timeout time.Duration, pmMode bool,
	wg *sync.WaitGroup, sem chan struct{}, successCount *int, failedHosts, refusedHosts, osInstallHosts, noSvrAutoHosts *[]string, mu *sync.Mutex) {
	defer wg.Done()
	sem <- struct{}{}
	defer func() { <-sem }()

	target := host
	if !strings.Contains(target, ":") {
		target = host + ":" + port
	}
	config := &ssh.ClientConfig{User: user, Auth: authMethods, HostKeyCallback: ssh.InsecureIgnoreHostKey(), Timeout: timeout}

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

	if isAnacondaRunning(client) {
		printPdshStyle(host, "OS 설치중 (anaconda 프로세스 및 마운트 감지)", nil)
		mu.Lock()
		*osInstallHosts = append(*osInstallHosts, host)
		mu.Unlock()
		return
	}

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

	if pmMode {
		sess2, err := client.NewSession()
		if err == nil {
			checkPmCmd := `sh -c 'if [ -d /user/svrauto ]; then echo 1; else echo 0; fi'`
			out2, _ := sess2.CombinedOutput(checkPmCmd)
			sess2.Close()
			lines := strings.Split(strings.TrimSpace(string(out2)), "\n")
			lastLine := ""
			if len(lines) > 0 {
				lastLine = strings.TrimSpace(lines[len(lines)-1])
			}
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

func main() {
	hostFile := flag.String("w", "", "호스트 목록 파일 경로")
	user := flag.String("u", "root", "SSH 접속 계정")
	password := flag.String("p", "", "SSH 접속 비밀번호")
	keyPath := flag.String("i", "", "SSH 키 파일 경로")
	port := flag.String("P", "22", "SSH 포트")
	concurrency := flag.Int("c", 1000, "동시 접속 수")
	timeoutSec := flag.Int("t", 15, "접속 타임아웃 초")
	dangerConfirm := flag.Bool("dnlgjawkrdjqghkrdls", false, "위험 작업 강제 실행 확인 옵션")
	scriptMode := flag.Bool("script", false, "작업 요약 출력 숨김")
	pmMode := flag.Bool("pm", false, "/user/svrauto 마운트 상태 추가 점검")
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
	command := strings.Trim(strings.Join(args, " "), "\"'")

	lowerCmd := strings.ToLower(command)
	isDangerous := strings.Contains(lowerCmd, "reboot") || strings.Contains(lowerCmd, "poweroff") ||
		strings.Contains(lowerCmd, "shutdown") || strings.Contains(lowerCmd, "halt") ||
		strings.Contains(lowerCmd, "init 0") || strings.Contains(lowerCmd, "init 6") || strings.Contains(lowerCmd, "ddc")
	if isDangerous && !*dangerConfirm {
		fmt.Println("[경고] 위험 작업(시스템 종료/재부팅)이 감지되었습니다! '-dnlgjawkrdjqghkrdls' 옵션을 추가하세요.")
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
				if token = strings.TrimSpace(token); token != "" {
					hosts = append(hosts, expandHostLine(token)...)
				}
			}
		}
	}
	if len(hosts) == 0 {
		os.Exit(0)
	}
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
		fmt.Printf("[주의] 실행 명령어: %s / 대상 호스트 %d대. 정말로 실행하시겠습니까? (y/N): ", command, len(hosts))
		reader := bufio.NewReader(os.Stdin)
		response, _ := reader.ReadString('\n')
		if strings.ToLower(strings.TrimSpace(response)) != "y" {
			fmt.Println("작업이 취소되었습니다.")
			os.Exit(0)
		}
	}

	var wg sync.WaitGroup
	sem := make(chan struct{}, *concurrency)
	timeout := time.Duration(*timeoutSec) * time.Second
	var mu sync.Mutex
	var successCount int
	var failedHosts, refusedHosts, osInstallHosts, noSvrAutoHosts []string
	startTime := time.Now()

	for _, host := range hosts {
		wg.Add(1)
		go runSSHCommand(host, command, *user, authMethods, *port, timeout, *pmMode, &wg, sem, &successCount, &failedHosts, &refusedHosts, &osInstallHosts, &noSvrAutoHosts, &mu)
	}
	wg.Wait()

	writeHostsToFile(cleanHostFile+"_res_off", failedHosts)
	writeHostsToFile(cleanHostFile+"_res_refsed", refusedHosts)
	writeHostsToFile(cleanHostFile+"_os_install", osInstallHosts)
	writeHostsToFile(cleanHostFile+"_nosvrauto", noSvrAutoHosts)

	if !*scriptMode {
		fmt.Printf("\n================= 작업 요약 =================\n")
		fmt.Printf("총 대상 서버 : %d 대 (소요시간: %v)\n", len(hosts), time.Since(startTime))
		fmt.Printf(" 정상 접속 가능 : %d 대\n", successCount)
		fmt.Printf(" 접속 불가(Timeout 등) : %d 대\n", len(failedHosts))
		fmt.Printf(" Refused(포트 닫힘) : %d 대\n", len(refusedHosts))
	}
}
