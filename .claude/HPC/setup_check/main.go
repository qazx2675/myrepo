package main

// new_setup — 신규 OS 배포 후 설치 상태 판정 도구.
// gossh 를 두 번 호출해(1차: -pm uptime, 2차: 설치중 후보의 anaconda.log 확인)
// 장비마다 "hostname<TAB>상태" 한 줄을 입력 순서대로 stdout 에 낸다. 안내·요약은 전부 stderr.
// 표준 라이브러리만 사용(Go 1.20, RHEL6 빌드 호환).

import (
	"bytes"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"unicode"
)

const (
	stDone      = "완료"
	stNoDisk    = "디스크미인식"
	stStopped   = "설치정지"
	stInstall   = "OS설치중"
	stUnreach   = "접속불가"
	defaultDisk = `Disk "" given in clearpart command does not exist`
)

// ★ 2차 원격 명령이 찍는 표식. MOTD 등 다른 출력과 섞여도 구분되도록 접두어를 둔다.
const (
	mkNoLog = "NS_NOLOG"
	mkDisk  = "NS_DISK"
	mkErr   = "NS_ERR"
	mkDone  = "NS_DONE" // 명령 끝까지 실행됐다는 표식(문구가 하나도 없어도 응답으로 센다)
)

// ---------------------------------------------------------------------------
// 호스트 파싱 (gossh 와 동일 규칙)
// ---------------------------------------------------------------------------

var hostRangeRegex = regexp.MustCompile(`(.*?)\[([^\]]+)\](.*)`)

// expandHostLine 은 node[01-10], a[1,3,5-7], 괄호 여러 개(재귀)를 gossh 와 똑같이 펼친다.
func expandHostLine(line string) []string {
	m := hostRangeRegex.FindStringSubmatch(line)
	if m == nil {
		return []string{line}
	}
	prefix, content, suffix := m[1], m[2], m[3]

	var results []string
	for _, item := range strings.Split(content, ",") {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if idx := strings.Index(item, "-"); idx > 0 {
			startStr, endStr := item[:idx], item[idx+1:]
			start, err1 := strconv.Atoi(startStr)
			end, err2 := strconv.Atoi(endStr)
			if err1 != nil || err2 != nil {
				results = append(results, expandHostLine(prefix+item+suffix)...)
				continue
			}
			if start > end {
				start, end = end, start
			}
			padLen := 0
			if len(startStr) > 1 && startStr[0] == '0' {
				padLen = len(startStr)
			}
			for i := start; i <= end; i++ {
				results = append(results, expandHostLine(prefix+formatNum(i, padLen)+suffix)...)
			}
			continue
		}
		results = append(results, expandHostLine(prefix+item+suffix)...)
	}
	return results
}

func formatNum(n, width int) string {
	if width > 0 {
		return fmt.Sprintf("%0*d", width, n)
	}
	return strconv.Itoa(n)
}

// parseHostLines 는 호스트파일 내용을 호스트 목록으로 만든다.
// 줄 앞뒤 공백 제거, 빈 줄·'#' 줄 무시, 쉼표/공백 분리, 범위 확장, 먼저 나온 순서로 중복 제거.
func parseHostLines(text string) []string {
	var hosts []string
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		for _, tok := range strings.FieldsFunc(line, func(r rune) bool {
			return r == ',' || unicode.IsSpace(r)
		}) {
			hosts = append(hosts, expandHostLine(tok)...)
		}
	}
	seen := map[string]bool{}
	var uniq []string
	for _, h := range hosts {
		if !seen[h] {
			seen[h] = true
			uniq = append(uniq, h)
		}
	}
	return uniq
}

// ---------------------------------------------------------------------------
// 설정 파일
// ---------------------------------------------------------------------------

type config struct {
	Gossh        string
	DiskPatterns []string
	ErrPatterns  []string
}

// parseConf 는 KEY=VALUE 줄을 읽는다. '#' 주석·빈 줄 무시, 값은 첫 '=' 뒤 전체(앞뒤 공백만 제거).
// DISK_PATTERN 이 하나도 없으면 기본 문구 1개를 쓴다.
func parseConf(text string) config {
	var c config
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		i := strings.Index(line, "=")
		if i < 0 {
			continue
		}
		key := strings.TrimSpace(line[:i])
		val := strings.TrimSpace(line[i+1:])
		switch key {
		case "GOSSH":
			c.Gossh = val
		case "DISK_PATTERN":
			if val != "" {
				c.DiskPatterns = append(c.DiskPatterns, val)
			}
		case "ERROR_PATTERN":
			if val != "" {
				c.ErrPatterns = append(c.ErrPatterns, val)
			}
		}
	}
	if len(c.DiskPatterns) == 0 {
		c.DiskPatterns = []string{defaultDisk}
	}
	return c
}

// ---------------------------------------------------------------------------
// 2차 원격 명령
// ---------------------------------------------------------------------------

// shQuote 는 sh 작은따옴표 인용: ' → '\”
func shQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func grepArgs(patterns []string) string {
	var b strings.Builder
	for _, p := range patterns {
		b.WriteString("-e " + shQuote(p) + " ")
	}
	return b.String()
}

// buildRemoteCmd 는 로그에서 문구를 고정 문자열로 찾아 표식만 찍는 명령을 만든다.
// ★ ERROR_PATTERN 이 비면 NS_ERR 줄 자체를 넣지 않는다(빈 -e ” 는 모든 줄에 일치해 버린다).
// ★ root 로그인 셸이 csh/tcsh 여도 돌도록 변수($f)·중괄호 블록·exit 를 쓰지 않고 `;` `&&` `||` 만 쓴다.
func buildRemoteCmd(disk, errp []string) string {
	const f = "/tmp/anaconda.log"
	s := `[ -r ` + f + ` ] || echo ` + mkNoLog + `; ` +
		`[ -r ` + f + ` ] && grep -qF ` + grepArgs(disk) + f + ` && echo ` + mkDisk + `;`
	if len(errp) > 0 {
		s += ` [ -r ` + f + ` ] && grep -qF ` + grepArgs(errp) + f + ` && echo ` + mkErr + `;`
	}
	return s + " echo " + mkDone
}

// ---------------------------------------------------------------------------
// gossh 위험어 검사 (gossh danger.go 의 isDangerousCommand 와 같은 규칙을 복사)
// ★ gossh 가 거부할 명령을 보내 놓고 결과를 잘못 판정하지 않도록, 실행 전에 미리 걸러 낸다.
// ---------------------------------------------------------------------------

var legacyDangerWords = []string{"reboot", "poweroff", "shutdown", "halt", "init 0", "init 6", "ddc"}

const (
	dgCmdStart = "(?:^|[;|&(`'\"{\\n]|\\$\\()\\s*" +
		"(?:(?:\\w+=\\S*\\s+)|(?:(?:sudo|xargs|exec|nohup|time|env|nice|command)\\s+(?:-\\S+\\s+)*))*" +
		"(?:[^\\s;|&()`'\"]*/)?"
	dgEnd  = "(?:$|[\\s;|&()`'\"])"
	dgArgs = "(?:[^;|&\\n]*\\s)?"
)

var dangerPatterns = []*regexp.Regexp{
	regexp.MustCompile(dgCmdStart + `kexec` + dgEnd),
	regexp.MustCompile(dgCmdStart + `systemctl\s+(?:-\S+\s+)*(?:kexec|rescue|emergency)` + dgEnd),
	regexp.MustCompile(dgCmdStart + `systemctl\s+(?:-\S+\s+)*isolate\s+(?:rescue|emergency)(?:\.target)?` + dgEnd),
	regexp.MustCompile(dgCmdStart + `(?:tel)?init\s+1` + dgEnd),
	regexp.MustCompile(dgCmdStart + `ipmitool\s+` + dgArgs + `power\s+(?:off|reset|cycle)` + dgEnd),
	regexp.MustCompile(`(?:^|[\s;|&()` + "`" + `'"])(?:\S*/)?sysrq-trigger` + dgEnd),
	regexp.MustCompile(dgCmdStart + `mkfs(?:\.[a-z0-9]+)?` + dgEnd),
	regexp.MustCompile(dgCmdStart + `dd\s+` + dgArgs + `of=/dev/`),
	regexp.MustCompile(dgCmdStart + `(?:wipefs|fdisk|parted|sgdisk|shred|lvremove|vgremove|pvremove)` + dgEnd),
	regexp.MustCompile(dgCmdStart + `rm\s+(?:-\S+\s+)*?(?:-[a-z]*r[a-z]*|--recursive)\s+(?:-\S+\s+)*/\*?` + dgEnd),
	regexp.MustCompile(dgCmdStart + `ifdown` + dgEnd),
	regexp.MustCompile(dgCmdStart + `nmcli\s+` + dgArgs + `down` + dgEnd),
	regexp.MustCompile(dgCmdStart + `ip\s+(?:-\S+\s+)*link\s+set\s+` + dgArgs + `down` + dgEnd),
	regexp.MustCompile(dgCmdStart + `systemctl\s+(?:-\S+\s+)*stop\s+(?:\S+\s+)*?(?:sshd|network|networkmanager)(?:\.service)?` + dgEnd),
}

func isDangerousCommand(cmd string) bool {
	lower := strings.ToLower(cmd)
	for _, w := range legacyDangerWords {
		if strings.Contains(lower, w) {
			return true
		}
	}
	for _, re := range dangerPatterns {
		if re.MatchString(lower) {
			return true
		}
	}
	return false
}

// dangerousPatterns 는 위험어에 걸리는 문구 목록을 돌려준다(문구 단독 검사).
func dangerousPatterns(patterns []string) []string {
	var bad []string
	for _, p := range patterns {
		if isDangerousCommand(p) {
			bad = append(bad, p)
		}
	}
	return bad
}

// ---------------------------------------------------------------------------
// 판정
// ---------------------------------------------------------------------------

// parseRetryOutput 은 2차 gossh stdout 의 "host: MARKER" 줄을 호스트별 표식 집합으로 만든다.
// 표식이 아닌 줄(MOTD 등)은 무시한다.
func parseRetryOutput(out string) map[string]map[string]bool {
	res := map[string]map[string]bool{}
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimRight(line, "\r")
		i := strings.Index(line, ": ")
		if i <= 0 {
			continue
		}
		host, val := line[:i], strings.TrimSpace(line[i+2:])
		if val != mkNoLog && val != mkDisk && val != mkErr && val != mkDone {
			continue
		}
		if res[host] == nil {
			res[host] = map[string]bool{}
		}
		res[host][val] = true
	}
	return res
}

// retryStatus 는 설치중 후보 한 대의 2차 표식으로 상태를 정한다.
// 우선순위: 디스크미인식 > 설치정지 > OS설치중. 표식이 하나도 없으면(2차 접속 실패 등) OS설치중.
func retryStatus(markers map[string]bool) string {
	switch {
	case markers[mkDisk]:
		return stNoDisk
	case markers[mkErr]:
		return stStopped
	}
	return stInstall
}

// classify 는 호스트 순서대로 최종 상태를 정한다.
// off: 접속불가 집합, cand: 설치중 후보 집합, retry: 2차 표식. 접속불가가 후보보다 우선한다.
func classify(hosts []string, off, cand map[string]bool, retry map[string]map[string]bool) []string {
	out := make([]string, len(hosts))
	for i, h := range hosts {
		switch {
		case off[h]:
			out[i] = stUnreach
		case cand[h]:
			out[i] = retryStatus(retry[h])
		default:
			out[i] = stDone
		}
	}
	return out
}

func toSet(l []string) map[string]bool {
	m := make(map[string]bool, len(l))
	for _, h := range l {
		m[h] = true
	}
	return m
}

// readHostSet 은 gossh 결과 파일을 집합으로 읽는다. 파일이 없으면 0대.
func readHostSet(path string) map[string]bool {
	b, err := os.ReadFile(path)
	if err != nil {
		return map[string]bool{}
	}
	return toSet(parseHostLines(string(b)))
}

// ---------------------------------------------------------------------------
// gossh 실행
// ---------------------------------------------------------------------------

// resolveGosshPath 는 설정의 GOSSH 값을 실제 실행 경로로 바꾼다.
// ★ 셸 변수처럼 GOSSH="/path/gossh" 로 따옴표를 쓰거나 ~/ 로 시작하거나 폴더(/path/gossh/)만
// 적으면 그대로는 exec 가 실패하므로(no such file or directory / permission denied) 여기서 풀어 준다.
func resolveGosshPath(v string) string {
	if len(v) >= 2 && (v[0] == '"' || v[0] == '\'') && v[len(v)-1] == v[0] {
		v = strings.TrimSpace(v[1 : len(v)-1])
	}
	if strings.HasPrefix(v, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			v = filepath.Join(home, v[2:])
		}
	}
	if fi, err := os.Stat(v); err == nil && fi.IsDir() {
		v = filepath.Join(v, "gossh")
	}
	return v
}

// runGossh 는 gossh 를 실행해 stdout+stderr 를 로그에 이어 붙이고 stdout 을 돌려준다.
// 비정상 종료(ExitError)는 오류로 보지 않는다(결과 파일로 판정). 실행 자체가 안 되면 오류.
func runGossh(gossh string, args []string, log io.Writer) (string, error) {
	var stdout bytes.Buffer
	cmd := exec.Command(gossh, args...)
	cmd.Stdout = io.MultiWriter(log, &stdout)
	cmd.Stderr = log
	err := cmd.Run()
	if _, ok := err.(*exec.ExitError); ok {
		fmt.Fprintf(os.Stderr, "경고: gossh 가 비정상 종료했습니다(%v). 결과 파일 기준으로 계속 판정합니다.\n", err)
		err = nil
	}
	return stdout.String(), err
}

var resSuffixes = []string{"_res_off", "_res_refsed", "_os_install", "_nosvrauto", "_res_cancel"}

func fail(code int, format string, a ...interface{}) {
	fmt.Fprintf(os.Stderr, format+"\n", a...)
	os.Exit(code)
}

func main() {
	fs := flag.NewFlagSet("new_setup", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	confPath := fs.String("c", "", "설정 파일 경로 (기본: 실행 파일 옆 new_setup.conf)")
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "사용법: new_setup [-c <설정파일>] <호스트파일>")
	}
	if err := fs.Parse(os.Args[1:]); err != nil {
		os.Exit(1)
	}
	if fs.NArg() != 1 {
		fs.Usage()
		os.Exit(1)
	}
	hostFile := fs.Arg(0)
	// ★ gossh 와 같이 앞의 '^' 를 뗀 이름으로 결과 파일 이름을 만든다.
	cleanHostFile := strings.TrimPrefix(hostFile, "^")

	// 설정 읽기: -c 지정 시 반드시 있어야 하고, 기본 경로는 없으면 기본값 사용.
	var conf config
	cp := *confPath
	explicit := cp != ""
	if !explicit {
		if exe, err := os.Executable(); err == nil {
			cp = filepath.Join(filepath.Dir(exe), "new_setup.conf")
		}
	}
	if b, err := os.ReadFile(cp); err == nil {
		conf = parseConf(string(b))
	} else if explicit {
		fail(2, "오류: 설정 파일을 읽을 수 없습니다: %v", err)
	} else {
		conf = parseConf("")
	}

	remoteCmd := buildRemoteCmd(conf.DiskPatterns, conf.ErrPatterns)
	// ★ gossh 는 위험어가 든 명령을 거부하므로, 시작 전에 설정 문구를 검사해 알려 준다.
	bad := dangerousPatterns(append(append([]string{}, conf.DiskPatterns...), conf.ErrPatterns...))
	if len(bad) == 0 && isDangerousCommand(remoteCmd) {
		bad = []string{remoteCmd}
	}
	if len(bad) > 0 {
		fail(2, "오류: 설정 문구에 gossh 위험어(reboot, poweroff, shutdown, halt, init 0, init 6, ddc 등)가 들어 있어 실행할 수 없습니다: %s", strings.Join(bad, " | "))
	}

	gossh := resolveGosshPath(conf.Gossh)
	if gossh == "" {
		p, err := exec.LookPath("gossh")
		if err != nil {
			fail(2, "오류: gossh 를 찾을 수 없습니다. 설정 파일의 GOSSH= 에 경로를 쓰거나 PATH 에 추가하세요.")
		}
		gossh = p
	} else if _, err := os.Stat(gossh); err != nil {
		fail(2, "오류: 설정 파일의 GOSSH 경로에 gossh 가 없습니다: %s", gossh)
	}

	raw, err := os.ReadFile(cleanHostFile)
	if err != nil {
		fail(2, "오류: 호스트 파일을 읽을 수 없습니다: %v", err)
	}
	hosts := parseHostLines(string(raw))
	if len(hosts) == 0 {
		fmt.Fprintf(os.Stderr, "경고: %s 에 등록된 호스트가 없습니다.\n", cleanHostFile)
		return
	}

	logPath := cleanHostFile + "_new_setup.log"
	logf, err := os.Create(logPath) // 시작 시 비움
	if err != nil {
		fail(2, "오류: 로그 파일을 만들 수 없습니다: %v", err)
	}
	defer logf.Close()

	// ★ 실제 gossh 는 시작할 때 결과 파일을 항상 지우지는 않는다(랩에서 지난 실행의 _os_install 이 남아 이번 결과에 섞임을 확인).
	//   그래서 이 도구가 1차 실행 전에 직접 지운다.
	for _, s := range resSuffixes {
		os.Remove(cleanHostFile + s)
	}
	// --- 1차 ---
	fmt.Fprintf(os.Stderr, "[1/2] 전체 %d대 접속 확인 중...\n", len(hosts))
	if _, err := runGossh(gossh, []string{"-w", hostFile, "-pm", "-script", "uptime"}, logf); err != nil {
		fail(2, "오류: gossh 실행 실패: %v", err)
	}
	// ★ 접속불가 = 타임아웃·DNS 실패·세션 실패(_res_off), 포트 닫힘(_res_refsed), 취소(_res_cancel)
	off := map[string]bool{}
	for _, s := range []string{"_res_off", "_res_refsed", "_res_cancel"} {
		for h := range readHostSet(cleanHostFile + s) {
			off[h] = true
		}
	}
	candSet := readHostSet(cleanHostFile + "_os_install")
	var cands []string // 입력 순서 유지
	for _, h := range hosts {
		if candSet[h] && !off[h] {
			cands = append(cands, h)
		}
	}

	// --- 2차 ---
	retry := map[string]map[string]bool{}
	if len(cands) > 0 {
		fmt.Fprintf(os.Stderr, "[2/2] 설치중 후보 %d대 로그 확인 중...\n", len(cands))
		retryFile := cleanHostFile + "_new_setup_retry"
		if err := os.WriteFile(retryFile, []byte(strings.Join(cands, "\n")+"\n"), 0o644); err != nil {
			fail(2, "오류: 임시 파일을 만들 수 없습니다: %v", err)
		}
		out, err := runGossh(gossh, []string{"-w", retryFile, "-script", remoteCmd}, logf)
		os.Remove(retryFile)
		for _, s := range resSuffixes {
			os.Remove(retryFile + s)
		}
		if err != nil {
			fail(2, "오류: gossh 2차 실행 실패: %v", err)
		}
		retry = parseRetryOutput(out)
		for _, h := range cands {
			if len(retry[h]) == 0 {
				fmt.Fprintf(os.Stderr, "경고: %s 2차 확인에 응답이 없어 OS설치중으로 둡니다.\n", h)
			}
		}
	}

	statuses := classify(hosts, off, candSet, retry)
	var w bytes.Buffer
	count := map[string]int{}
	for i, h := range hosts {
		w.WriteString(h + "\t" + statuses[i] + "\n")
		count[statuses[i]]++
	}
	os.Stdout.Write(w.Bytes())

	fmt.Fprintf(os.Stderr, "요약: 총 %d대 — 완료 %d, 디스크미인식 %d, 설치정지 %d, OS설치중 %d, 접속불가 %d (gossh 로그: %s)\n",
		len(hosts), count[stDone], count[stNoDisk], count[stStopped], count[stInstall], count[stUnreach], logPath)
}
