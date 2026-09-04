package main

import (
	"bufio"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"sort"
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

// ★ 가독성용 ANSI 색상. -script 모드에서는 다른 도구가 결과를 파싱/파이프하는 용도라
// 이스케이프 코드가 섞이면 안 되므로 colorEnabled를 false로 두고 그대로 원문을 출력한다.
const (
	colorReset  = "\033[0m"
	colorRed    = "\033[31m"
	colorGreen  = "\033[32m"
	colorYellow = "\033[33m"
	colorCyanB  = "\033[1;36m"
)

var colorEnabled bool

func colorize(color, s string) string {
	if !colorEnabled {
		return s
	}
	return color + s + colorReset
}

// ★ pdsh 스타일 "플래그+값 붙여쓰기"를 -w에 한해 지원한다.
// "-w^file", "-wfile" 처럼 공백/등호 없이 붙어 있는 경우를 Go flag 패키지가 이해하는
// "-w" "값" 두 토큰으로 분리한다. "-w=value"(Go 관용 표기)와 "-w" 단독, "-w ^file"
// (이미 공백으로 분리되어 있는 경우)은 그대로 둔다.
func preprocessArgs(args []string) []string {
	var out []string
	for _, a := range args {
		if strings.HasPrefix(a, "-w") && len(a) > 2 && a[2] != '=' {
			out = append(out, "-w", a[2:])
			continue
		}
		out = append(out, a)
	}
	return out
}

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

// splitTrailingDigits는 호스트명 끝의 연속된 숫자(있다면)와 그 앞부분을 나눈다.
// 0-padding(예: "0001")을 그대로 보존하기 위해 문자열 그대로 반환한다.
func splitTrailingDigits(host string) (prefix string, numStr string, ok bool) {
	i := len(host)
	for i > 0 && host[i-1] >= '0' && host[i-1] <= '9' {
		i--
	}
	if i == len(host) {
		return "", "", false
	}
	return host[:i], host[i:], true
}

// compressHosts는 expandHostLine의 역방향이다: "esxi0001", "esxi0002", "esxi0003"처럼
// 접두어+자릿수가 같고 번호가 연속인 호스트들을 "esxi[0001-0003]" 하나로 압축한다.
// 결과 토큰은 expandHostLine이 그대로 다시 풀 수 있는 형태라, 이 함수의 출력을 그대로
// 파일에 저장해서 -w로 다시 넣어도 동작한다.
func compressHosts(hosts []string) []string {
	type groupKey struct {
		prefix string
		width  int
	}

	groups := map[groupKey][]int{}
	var groupOrder []groupKey
	seenGroup := map[groupKey]bool{}
	var singles []string

	for _, h := range hosts {
		prefix, numStr, ok := splitTrailingDigits(h)
		if !ok {
			singles = append(singles, h)
			continue
		}
		n, err := strconv.Atoi(numStr)
		if err != nil {
			singles = append(singles, h)
			continue
		}
		key := groupKey{prefix: prefix, width: len(numStr)}
		if !seenGroup[key] {
			seenGroup[key] = true
			groupOrder = append(groupOrder, key)
		}
		groups[key] = append(groups[key], n)
	}

	var out []string
	for _, key := range groupOrder {
		nums := groups[key]
		sort.Ints(nums)
		i := 0
		for i < len(nums) {
			j := i
			for j+1 < len(nums) && nums[j+1] == nums[j]+1 {
				j++
			}
			if j > i {
				out = append(out, fmt.Sprintf("%s[%0*d-%0*d]", key.prefix, key.width, nums[i], key.width, nums[j]))
			} else {
				out = append(out, fmt.Sprintf("%s%0*d", key.prefix, key.width, nums[i]))
			}
			i = j + 1
		}
	}
	out = append(out, singles...)
	return out
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
	// ★ 연속된 호스트명은 esxi[0001-0003] 형태로 압축해서 저장한다. -w로 그대로
	// 다시 읽어도 expandHostLine이 풀어주므로 재실행 가능하다.
	for _, token := range compressHosts(hosts) {
		file.WriteString(token + "\n")
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

// renderResultLines는 printPdshStyle이 host 접두어 없이 찍을 본문 줄들을 그대로 만들어준다.
// -b(묶어 출력) 모드에서 호스트별 결과를 비교하기 위해 printPdshStyle과 별도로 필요하다.
func renderResultLines(output string, err error) string {
	output = strings.TrimSpace(output)

	if err != nil {
		if output != "" {
			var lines []string
			for _, line := range strings.Split(output, "\n") {
				lines = append(lines, "ERROR: "+strings.TrimSpace(line))
			}
			return strings.Join(lines, "\n")
		}
		return fmt.Sprintf("ERROR: %v", err)
	}

	if output == "" {
		return ""
	}

	var lines []string
	for _, line := range strings.Split(output, "\n") {
		lines = append(lines, strings.TrimSpace(line))
	}
	return strings.Join(lines, "\n")
}

func printPdshStyle(host string, output string, err error) {
	text := renderResultLines(output, err)
	if text == "" {
		return
	}
	color := colorGreen
	if err != nil {
		color = colorRed
	}
	for _, line := range strings.Split(text, "\n") {
		fmt.Println(colorize(color, fmt.Sprintf("%s: %s", host, line)))
	}
}

// printBunched는 clush -b와 동일하게, 결과 본문이 완전히 같은 호스트끼리 묶어서
// 한 번만 출력한다. hosts 순서대로 훑으면서 처음 보는 결과 내용마다 그룹을 만든다.
func printBunched(hosts []string, outputs map[string]string) {
	type group struct {
		text  string
		hosts []string
	}
	var groups []*group
	seen := map[string]*group{}

	for _, h := range hosts {
		text, ok := outputs[h]
		if !ok || text == "" {
			continue
		}
		g, exists := seen[text]
		if !exists {
			g = &group{text: text}
			seen[text] = g
			groups = append(groups, g)
		}
		g.hosts = append(g.hosts, h)
	}

	divider := strings.Repeat("-", 20)
	for _, g := range groups {
		fmt.Println(colorize(colorCyanB, divider))
		fmt.Println(colorize(colorCyanB, strings.Join(compressHosts(g.hosts), ",")))
		fmt.Println(colorize(colorCyanB, divider))
		fmt.Println(g.text)
	}
}

// printUnreachableGroup은 -b 모드에서 접속 자체가 안 된 호스트(타임아웃/Refused)를
// 별도 그룹으로 묶어서 보여준다. 이 호스트들은 세션이 아예 생성되지 않아 결과 본문이
// 없으므로(printBunched의 내용 비교 대상이 아님) 접속불가라는 이유 하나로만 묶는다.
func printUnreachableGroup(failedHosts, refusedHosts []string) {
	unreachable := append(append([]string{}, failedHosts...), refusedHosts...)
	if len(unreachable) == 0 {
		return
	}
	divider := strings.Repeat("-", 20)
	fmt.Println(colorize(colorRed, divider))
	fmt.Println(colorize(colorRed, strings.Join(compressHosts(unreachable), ",")))
	fmt.Println(colorize(colorRed, divider))
	fmt.Println(colorize(colorRed, "접속불가 (Timeout/Refused)"))
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

func runSSHCommand(host string, command string, user string, authMethods []ssh.AuthMethod, port string, timeout time.Duration, pmMode bool, bunchMode bool, bunchOutputs map[string]string, wg *sync.WaitGroup, sem chan struct{}, successCount *int, failedHosts *[]string, refusedHosts *[]string, osInstallHosts *[]string, noSvrAutoHosts *[]string, mu *sync.Mutex) {
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
		fmt.Println(colorize(colorYellow, fmt.Sprintf("%s: OS 설치중 (~/.profile anaconda 감지)", host)))
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
	if bunchMode {
		text := renderResultLines(string(output), err)
		mu.Lock()
		bunchOutputs[host] = text
		mu.Unlock()
	} else {
		printPdshStyle(host, string(output), err)
	}

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
const autofsSafeConcurrency = 350

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
	// ★ 실행 파일 이름이 pdsh면 -script 기본값을 true로 (필요하면 -script=false로 명시적 해제 가능)
	defaultScriptMode := filepath.Base(os.Args[0]) == "pdsh"
	scriptMode := flag.Bool("script", defaultScriptMode, "작업 요약 출력 숨김 (순수 결과만 출력). 실행 파일 이름이 pdsh면 기본값 true")
	pmMode := flag.Bool("pm", false, "/user/svrauto 마운트 상태 추가 점검 및 OS설치중 감지")
	bMode := flag.Bool("b", false, "clush 스타일: 결과가 동일한 호스트끼리 묶어서 출력 (-script와 함께 쓰면 무시되고 호스트별로 출력)")

	// ★ pdsh 스타일 "-w^file"/"-wfile" 붙여쓰기 지원을 위해 flag.Parse() 대신 전처리한 인자로 파싱
	flag.CommandLine.Parse(preprocessArgs(os.Args[1:]))

	colorEnabled = !*scriptMode

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
	// ★ [버그 수정] 예전에는 여기서 strings.Trim(command, "\"'")로 앞뒤 따옴표를 무조건
	// 제거했는데, 이러면 명령어 끝이 실제로 따옴표로 끝나는 경우(예: grep 'asdf')까지
	// 그 따옴표를 잘라내버려서 명령어가 깨졌다(실제로 재현: `cat test |grep 'asdf'`가
	// `cat test |grep 'asdf`로 깨짐). 쉘이 넘겨준 인자에는 이미 실제 따옴표가 없으므로
	// 이 처리 자체가 불필요해서 제거했다.

	// ★ autofs 안전장치: /user/ 경로가 명령어에 포함되어 있으면 병렬 수를 350으로 강제.
	// -cf 로 명시적으로 병렬 수를 지정한 경우에만 이 제한을 무시하고 지정값을 그대로 쓴다.
	effectiveConcurrency := *concurrency
	if *forceConcurrency > 0 {
		effectiveConcurrency = *forceConcurrency
	} else if isAutofsUserPath(command) {
		effectiveConcurrency = autofsSafeConcurrency
		fmt.Println(colorize(colorYellow, fmt.Sprintf("[안전장치] 명령어에 \"/user/\" 경로가 감지되어 병렬 실행 수를 %d대로 자동 제한합니다. (원래 지정값 무시: -c %d)", autofsSafeConcurrency, *concurrency)))
		fmt.Println(colorize(colorYellow, "           이 경로가 autofs 마운트가 아니거나 더 높은 병렬 수가 필요하면 -cf <숫자> 옵션으로 강제 지정하세요."))
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
		fmt.Println(colorize(colorRed, "================================================================"))
		fmt.Println(colorize(colorRed, " [경고] 위험 작업(시스템 종료/재부팅)이 감지되었습니다!"))
		fmt.Println(colorize(colorRed, "================================================================"))
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

	// ★ -b는 -script와 같이 오면 무시한다(순수 결과 모드에서는 묶어 보여주는 요약형 출력이
	// 목적과 안 맞음). bunchMode가 false면 기존과 동일하게 즉시 호스트별로 출력한다.
	bunchMode := *bMode && !*scriptMode
	bunchOutputs := map[string]string{}

	startTime := time.Now()

	for _, host := range hosts {
		wg.Add(1)
		go runSSHCommand(host, command, *user, authMethods, *port, timeout, *pmMode, bunchMode, bunchOutputs, &wg, sem, &successCount, &failedHosts, &refusedHosts, &osInstallHosts, &noSvrAutoHosts, &mu)
	}

	wg.Wait()

	if bunchMode {
		printBunched(hosts, bunchOutputs)
		printUnreachableGroup(failedHosts, refusedHosts)
	}

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
		fmt.Println(colorize(colorCyanB, "\n================= 작업 요약 ================="))
		fmt.Printf("총 대상 서버 : %d 대 (소요시간: %v)\n", len(hosts), time.Since(startTime))
		fmt.Printf(" 동시 접속 수 : %d\n", effectiveConcurrency)
		fmt.Println(colorize(colorGreen, fmt.Sprintf(" 정상 접속 가능 : %d 대", successCount)))

		// OS 설치 중 출력 (-pm 옵션을 준 경우에만 기록되므로 바로 출력)
		if len(osInstallHosts) > 0 {
			fmt.Println(colorize(colorYellow, fmt.Sprintf(" OS 설치중 : %d 대", len(osInstallHosts))))
			for _, h := range osInstallHosts {
				fmt.Printf("   - %s\n", h)
			}
			fmt.Printf("  -> %s 에 목록 저장됨\n", osInstallFilename)
		}

		// ★ pm 옵션을 사용했고, 미접근 서버가 존재하는 경우 출력
		if *pmMode && len(noSvrAutoHosts) > 0 {
			fmt.Println(colorize(colorYellow, fmt.Sprintf(" svrauto미접근 : 접속가능 %d대 중 %d대", successCount, len(noSvrAutoHosts))))
			fmt.Printf("  -> %s 에 목록 저장됨\n", noSvrAutoFilename)
		}

		failLine := fmt.Sprintf(" 접속 불가(Timeout 등) : %d 대", len(failedHosts))
		if len(failedHosts) > 0 {
			fmt.Println(colorize(colorRed, failLine))
			fmt.Printf("  -> %s 에 목록 저장됨\n", offFilename)
		} else {
			fmt.Println(failLine)
		}

		refusedLine := fmt.Sprintf(" Refused(포트 닫힘) : %d 대", len(refusedHosts))
		if len(refusedHosts) > 0 {
			fmt.Println(colorize(colorRed, refusedLine))
			fmt.Printf("  -> %s 에 목록 저장됨\n", refusedFilename)
		} else {
			fmt.Println(refusedLine)
		}
		fmt.Println(colorize(colorCyanB, "============================================="))
	}
}
