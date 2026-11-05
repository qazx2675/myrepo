package main

// new_setup — 신규 OS 배포 후 설치 상태 판정 도구.
// gossh 를 두 번 호출해(1차: -pm 정보수집, 2차: 설치중 후보의 anaconda.log 확인),
// 완료 장비는 config_check.sh -auto-check 로 설정체크까지 해서
// 장비마다 "hostname 상태 설정체크 CPU stress-ng H/T OS" 7열("|" 구분, 첫 줄은 제목) 한 줄을 입력 순서대로 stdout 에 낸다. 안내·요약은 전부 stderr.
// 표준 라이브러리만 사용(Go 1.20, RHEL6 빌드 호환).

import (
	"bytes"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
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
	// v2
	OSCheckSh      string
	OSCheckExclude string
	CPUIntelLog    string
	CPUIntelOK     string
	CPUAMDLog      string
	CPUAMDOK       string
	CPUIntelLine   string // 비어 있지 않으면 이 문자열이 든 줄만 보고, 그 줄이 모두 OK 문구를 포함해야 O
	CPUAMDLine     string
}

const (
	defaultIntelLog = "/var/cpudiag/result.log"
	defaultIntelOK  = "Completed successfully. No issues detected."
	// ★ AMD: 현장 확인 방식 "grep Result /var/cpudiag/results.json 결과가 모두 PASS 면 정상"과 같게 한다.
	defaultAMDLog  = "/var/cpudiag/results.json"
	defaultAMDOK   = "PASS"
	defaultAMDLine = "Result"
)

// parseConf 는 KEY=VALUE 줄을 읽는다. '#' 주석·빈 줄 무시, 값은 첫 '=' 뒤 전체(앞뒤 공백만 제거).
// DISK_PATTERN 이 하나도 없으면 기본 문구 1개를 쓴다. CPU_INTEL_LOG/OK, CPU_AMD_LOG/OK/LINE 은 기본값이 있다.
// ★ CPU_* 값은 GOSSH 처럼 앞뒤 따옴표를 떼어 준다(CPU_AMD_LOG="/path" 로 적어도 동작).
func parseConf(text string) config {
	c := config{CPUIntelLog: defaultIntelLog, CPUIntelOK: defaultIntelOK,
		CPUAMDLog: defaultAMDLog, CPUAMDOK: defaultAMDOK, CPUAMDLine: defaultAMDLine}
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
		case "OS_CHECK_SH":
			c.OSCheckSh = val
		case "OS_CHECK_EXCLUDE":
			c.OSCheckExclude = val
		case "CPU_INTEL_LOG":
			c.CPUIntelLog = expandPath(val)
		case "CPU_INTEL_OK":
			c.CPUIntelOK = unquote(val)
		case "CPU_INTEL_LINE":
			c.CPUIntelLine = unquote(val)
		case "CPU_AMD_LOG":
			c.CPUAMDLog = expandPath(val)
		case "CPU_AMD_OK":
			c.CPUAMDOK = unquote(val)
		case "CPU_AMD_LINE":
			c.CPUAMDLine = unquote(val)
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

// ★ 1차 원격 명령이 찍는 표식과 파싱 키워드(v2).
const (
	mkCPUIntel = "NS_CPU_INTEL"
	mkCPUAMD   = "NS_CPU_AMD"
	mkNoLogSfx = "_NOLOG" // NS_CPU_INTEL_NOLOG / NS_CPU_AMD_NOLOG: CPU 테스트 로그 파일이 없음
	mkStress   = "NS_STRESS"
	mkKernel   = "NS_KERNEL"
)

// buildInfoCmd 는 1차 정보수집 명령을 만든다(CPU 벤더·CPU 테스트·stress-ng·H/T·커널).
// ★ csh/tcsh 로그인 셸에서도 돌도록 변수 대입·중괄호·exit·'!'·글롭 없이 `;` `&&` 작은따옴표 백틱만 쓴다.
// ★ 각 조각은 `;` 로 이어 앞 조각이 실패(grep 불일치 등)해도 다음 조각이 실행된다.
// CPU 로그/문구가 하나라도 비면 그 벤더의 CPU 테스트 줄은 넣지 않는다(빈 -e ” 는 모든 줄에 일치).
func buildInfoCmd(c config) string {
	s := "grep -m1 vendor_id /proc/cpuinfo; "
	s += cpuCheckCmd(c.CPUIntelLog, c.CPUIntelOK, c.CPUIntelLine, mkCPUIntel)
	s += cpuCheckCmd(c.CPUAMDLog, c.CPUAMDOK, c.CPUAMDLine, mkCPUAMD)
	s += "grep -rqs --include='messages*' --exclude='*.gz' stress-ng /var/log && echo " + mkStress + "; "
	s += "lscpu | grep -i 'per core'; "
	s += "echo " + mkKernel + " `uname -r`"
	return s
}

// cpuCheckCmd 는 CPU 테스트 로그 확인 조각을 만든다(대소문자 무시). 로그/문구가 비면 "".
//
//	로그 파일이 없으면 <표식>_NOLOG
//	line 이 비면: 로그 어디에든 ok 문구가 있으면 표식
//	line 이 있으면: line 이 든 줄이 1개 이상이고 그 줄이 모두 ok 문구를 포함하면 표식
//	  (예: AMD results.json 의 "Result" 줄이 전부 PASS)
//
// ★ awk 프로그램은 작은따옴표 안에만 중괄호·== 가 있어 csh 에서도 안전하다. 값은 awk 문자열로 넣는다(-v 대입 안 씀).
func cpuCheckCmd(log, ok, line, mark string) string {
	if log == "" || ok == "" {
		return ""
	}
	f := logFileExpr(log)
	s := "[ -f " + f + " ] || echo " + mark + mkNoLogSfx + "; "
	if line == "" {
		return s + "grep -qiF -e " + shQuote(ok) + " " + f + " && echo " + mark + "; "
	}
	prog := "index(tolower($0)," + awkStr(strings.ToLower(line)) + "){n++; if (index(tolower($0)," +
		awkStr(strings.ToLower(ok)) + ")) p++} END{if (n>0 && n==p) print " + awkStr(mark) + "}"
	return s + "awk " + shQuote(prog) + " " + f + "; "
}

// awkStr 은 awk 문자열 리터럴로 만든다.
func awkStr(s string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s) + `"`
}

// logFileExpr 는 원격 명령에 넣을 로그 경로. 파일 이름에 * 나 ? 가 있으면 그 폴더에서 이름이 맞는
// 가장 최근 파일을 쓴다. ★ csh 는 일치하는 파일이 없는 글롭에서 명령 줄을 멈추므로 셸 글롭 대신
// `ls -t 폴더 | grep -x 정규식` 으로 고른다(폴더 부분에는 와일드카드를 쓸 수 없음, 공백 없는 경로만).
func logFileExpr(p string) string {
	dir, base := path.Dir(p), path.Base(p)
	if !strings.ContainsAny(base, "*?") {
		return shQuote(p)
	}
	re := strings.NewReplacer(`\`, `\\`, ".", `\.`, "[", `\[`, "]", `\]`, "^", `\^`, "$", `\$`, "*", ".*", "?", ".").Replace(base)
	return dir + "/`ls -t " + shQuote(dir) + " | grep -m1 -x -e " + shQuote(re) + "`"
}

// hostInfo 는 1차 출력에서 뽑은 장비 한 대의 정보.
type hostInfo struct {
	vendor   string // GenuineIntel / AuthenticAMD / ""
	cpuIntel bool
	cpuAMD   bool
	noIntel  bool // Intel CPU 테스트 로그 없음
	noAMD    bool // AMD CPU 테스트 로그 없음
	stress   bool
	tpc      int    // Thread(s) per core, 못 읽으면 0
	kernel   string // uname -r, 못 읽으면 ""
}

// parseInfoOutput 은 1차 gossh stdout 의 "host: 내용" 줄을 호스트별 정보로 만든다. 모르는 줄은 무시한다.
func parseInfoOutput(out string) map[string]*hostInfo {
	res := map[string]*hostInfo{}
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimRight(line, "\r")
		i := strings.Index(line, ": ")
		if i <= 0 {
			continue
		}
		host, val := line[:i], strings.TrimSpace(line[i+2:])
		h := res[host]
		if h == nil {
			h = &hostInfo{}
			res[host] = h
		}
		switch {
		case val == mkCPUIntel:
			h.cpuIntel = true
		case val == mkCPUAMD:
			h.cpuAMD = true
		case val == mkCPUIntel+mkNoLogSfx:
			h.noIntel = true
		case val == mkCPUAMD+mkNoLogSfx:
			h.noAMD = true
		case val == mkStress:
			h.stress = true
		case strings.HasPrefix(val, "vendor_id"):
			if j := strings.Index(val, ":"); j >= 0 {
				v := strings.TrimSpace(val[j+1:])
				if v == "GenuineIntel" || v == "AuthenticAMD" {
					h.vendor = v
				}
			}
		case strings.HasPrefix(val, "Thread(s) per core:"):
			if n, err := strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(val, "Thread(s) per core:"))); err == nil {
				h.tpc = n
			}
		case strings.HasPrefix(val, mkKernel+" "):
			h.kernel = strings.TrimSpace(strings.TrimPrefix(val, mkKernel+" "))
		}
	}
	return res
}

func ox(b bool) string {
	if b {
		return "O"
	}
	return "X"
}

// cpuResult: Intel 이고 NS_CPU_INTEL, 또는 AMD 이고 NS_CPU_AMD 면 O.
// 그 벤더의 CPU 테스트 로그 파일이 없으면 NOTRUN(부하테스트 아직 안 함), 그 외 X.
func (h *hostInfo) cpuResult() string {
	if h == nil {
		return "X"
	}
	switch {
	case (h.vendor == "GenuineIntel" && h.cpuIntel) || (h.vendor == "AuthenticAMD" && h.cpuAMD):
		return "O"
	case (h.vendor == "GenuineIntel" && h.noIntel) || (h.vendor == "AuthenticAMD" && h.noAMD):
		return cpuNotRun
	}
	return "X"
}

const cpuNotRun = "NOTRUN"

func (h *hostInfo) stressResult() string {
	return ox(h != nil && h.stress)
}

// htResult: 2 이상 ON, 1 OFF, 못 읽으면 '-'.
func (h *hostInfo) htResult() string {
	switch {
	case h == nil || h.tpc <= 0:
		return "-"
	case h.tpc >= 2:
		return "ON"
	}
	return "OFF"
}

func (h *hostInfo) osResult() string {
	if h == nil || h.kernel == "" {
		return "-"
	}
	return h.kernel
}

// ★ 열 구분자는 '|'. 터미널에서 복사하면 탭이 공백으로 바뀌는 경우가 있어, 엑셀에서는
// 데이터 > 텍스트 나누기 > 구분 기호 '|' 로 나눈다(사용자 요청).
const sep = "|"

// header 는 출력 맨 위 제목 줄(8열).
const header = "hostname" + sep + "상태" + sep + "설정체크" + sep + "CPU테스트" + sep + "stress-ng" + sep + "H/T" + sep + "OS" + sep + "비고"

// formatRow 는 8열('|' 구분) 한 줄을 만든다. 완료가 아니면 3~7열은 모두 '-'.
// check 는 설정체크 열 값(O/X/출력없음/-), note 는 비고(장애 목록의 사유, 없으면 빈칸).
func formatRow(host, status, check string, h *hostInfo, note string) string {
	if status != stDone {
		return strings.Join([]string{host, status, "-", "-", "-", "-", "-", note}, sep)
	}
	return strings.Join([]string{host, status, check, h.cpuResult(), h.stressResult(), h.htResult(), h.osResult(), note}, sep)
}

// defaultErrorList 는 -er 를 주지 않았을 때 현재 경로에서 찾는 장애 목록 파일 이름.
const defaultErrorList = "error_list.txt"

// parseErrorList 는 "hostname 장애사유" 줄들을 읽어 호스트(소문자) → 비고 맵을 만든다.
//   - 첫 공백(스페이스/탭) 앞이 호스트, 뒤 전체가 사유. 빈 줄·# 주석·사유 없는 줄은 건너뜀
//   - 같은 호스트가 여러 줄이면 ", " 로 이어 붙임(사용자 결정)
//   - ★ 사유 안의 '|' 는 열이 밀리지 않도록 '/' 로 바꾼다. 윈도우 메모장 파일의 BOM·CR 도 뗀다.
func parseErrorList(text string) map[string]string {
	m := map[string]string{}
	text = strings.TrimPrefix(text, "\xef\xbb\xbf") // UTF-8 BOM
	for _, ln := range strings.Split(text, "\n") {
		ln = strings.TrimSpace(ln)
		if ln == "" || strings.HasPrefix(ln, "#") {
			continue
		}
		i := strings.IndexAny(ln, " \t")
		if i < 0 {
			continue
		}
		host := strings.ToLower(ln[:i])
		why := strings.ReplaceAll(strings.TrimSpace(ln[i+1:]), sep, "/")
		if why == "" {
			continue
		}
		if m[host] != "" {
			m[host] += ", " + why
		} else {
			m[host] = why
		}
	}
	return m
}

// ---------------------------------------------------------------------------
// 설정체크 (config_check.sh -auto-check)
// ---------------------------------------------------------------------------

const noOutput = "출력없음"

// isResultToken 은 run.sh 결과 줄의 머리말(OK, FAIL, INFO 등 영문 대문자로만 된 단어)인지 본다.
func isResultToken(s string) bool {
	for _, r := range s {
		if r < 'A' || r > 'Z' {
			return false
		}
	}
	return s != ""
}

// parseCheckRes 는 check.res_<user> 내용과 제외 문자열로 호스트별 설정체크 값을 정한다.
// 호스트 줄 중 "host: " 뒤 첫 토큰이 FAIL 이면 X, 줄이 있고 FAIL 이 없으면 O, 줄이 없으면 출력없음.
// 제외 문자열(| 로 이은 것) 중 하나라도 hostname 에 포함되면 O.
// ★ 체크 결과 줄(첫 토큰이 OK/FAIL/INFO 처럼 대문자 단어)만 센다. 체크 스크립트 경로가 마운트되지 않으면
// "bash: …: No such file or directory" 같은 셸 오류 줄만 남는데, 이를 결과로 세면 FAIL 이 없어 O 로 잘못 나온다.
func parseCheckRes(text, exclude string, hosts []string) map[string]string {
	lines := map[string]int{}
	failed := map[string]bool{}
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimRight(line, "\r")
		i := strings.Index(line, ": ")
		if i <= 0 {
			continue
		}
		host := line[:i]
		f := strings.Fields(line[i+2:])
		if len(f) == 0 || !isResultToken(f[0]) {
			continue
		}
		lines[host]++
		if len(f) > 0 && f[0] == "FAIL" {
			failed[host] = true
		}
	}
	var ex []string
	for _, e := range strings.Split(exclude, "|") {
		if e = strings.TrimSpace(e); e != "" {
			ex = append(ex, e)
		}
	}
	res := map[string]string{}
	for _, h := range hosts {
		switch {
		case lines[h] == 0:
			res[h] = noOutput
		case failed[h]:
			res[h] = "X"
		default:
			res[h] = "O"
		}
		for _, e := range ex {
			if strings.Contains(h, e) {
				res[h] = "O"
				break
			}
		}
	}
	return res
}

// runConfigCheck 는 완료 장비에 대해 config_check.sh -auto-check 를 임시 폴더에서 돌려 호스트별 값을 돌려준다.
// 실행하지 못하면(-auto-check 미지원 등) nil 과 사유를 돌려주고, 호출한 쪽이 열을 '-' 로 둔다.
func runConfigCheck(conf config, script, gossh string, hosts []string, log io.Writer) (map[string]string, error) {
	b, err := os.ReadFile(script)
	if err != nil {
		return nil, fmt.Errorf("OS_CHECK_SH 를 읽을 수 없습니다: %v", err)
	}
	// ★ -auto-check 를 모르는 구버전에 인자를 주면 사용자 선택 메뉴에서 멈추므로 실행하지 않는다(auto_setup 과 같은 검사).
	if !strings.Contains(string(b), "-auto-check") {
		return nil, fmt.Errorf("OS_CHECK_SH 가 -auto-check 를 지원하지 않습니다(구버전)")
	}
	if abs, err := filepath.Abs(script); err == nil {
		script = abs
	}
	dir, err := os.MkdirTemp("", "new_setup_cc")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	tmp := filepath.Join(dir, "tmp")
	if err := os.MkdirAll(tmp, 0o755); err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(dir, "targets.txt"), []byte(strings.Join(hosts, "\n")+"\n"), 0o644); err != nil {
		return nil, err
	}
	// 체크 스크립트가 현재 폴더의 dhcp.sh 를 쓰므로 원본 위치로 링크(없으면 생략)
	if src := filepath.Join(filepath.Dir(script), "dhcp.sh"); fileExists(src) {
		_ = os.Symlink(src, filepath.Join(dir, "dhcp.sh"))
	}
	gdir := filepath.Dir(gossh)
	if abs, err := filepath.Abs(gdir); err == nil {
		gdir = abs
	}
	cmd := exec.Command("bash", script, "-auto-check", "new_setup", "targets.txt")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "TMPDIR="+tmp, "PATH="+gdir+string(os.PathListSeparator)+os.Getenv("PATH"))
	cmd.Stdout = log
	cmd.Stderr = log
	if err := cmd.Run(); err != nil {
		if _, ok := err.(*exec.ExitError); !ok {
			return nil, err
		}
		fmt.Fprintf(os.Stderr, "경고: 설정체크 스크립트가 비정상 종료했습니다(%v). 결과 파일 기준으로 계속 판정합니다.\n", err)
	}
	res, _ := os.ReadFile(filepath.Join(dir, "check.res_new_setup")) // 없으면 빈 내용 → 출력없음
	return parseCheckRes(string(res), conf.OSCheckExclude, hosts), nil
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
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

// expandPath 는 설정 값의 양끝 따옴표를 벗기고 앞의 ~/ 를 홈 폴더로 바꾼다(GOSSH·OS_CHECK_SH 공통).
func expandPath(v string) string {
	v = unquote(v)
	if strings.HasPrefix(v, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			v = filepath.Join(home, v[2:])
		}
	}
	return v
}

// unquote 는 값 전체를 감싼 따옴표("…" 또는 '…')를 뗀다.
func unquote(v string) string {
	if len(v) >= 2 && (v[0] == '"' || v[0] == '\'') && v[len(v)-1] == v[0] {
		v = strings.TrimSpace(v[1 : len(v)-1])
	}
	return v
}

// resolveGosshPath 는 설정의 GOSSH 값을 실제 실행 경로로 바꾼다.
// ★ 셸 변수처럼 GOSSH="/path/gossh" 로 따옴표를 쓰거나 ~/ 로 시작하거나 폴더(/path/gossh/)만
// 적으면 그대로는 exec 가 실패하므로(no such file or directory / permission denied) 여기서 풀어 준다.
func resolveGosshPath(v string) string {
	v = expandPath(v)
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
	confPath := fs.String("c", "", "설정 파일 경로 (기본: 현재 경로 → 실행 파일 옆 new_setup.conf)")
	errPath := fs.String("er", "", "장애 목록 파일(줄마다 \"hostname 장애사유\", 비고 열에 표시) (기본: 현재 경로의 "+defaultErrorList+", 없으면 생략)")
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "사용법: new_setup [-c <설정파일>] [-er <장애목록파일>] <호스트파일>")
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

	// 설정 읽기: -c 지정 시 반드시 있어야 한다. 생략하면 현재 경로 → 실행 파일 폴더 → (bin/ 이면) 그 위
	// 폴더 순서로 new_setup.conf 를 찾고, 어디에도 없으면 기본값 사용.
	// ★ 래퍼(new_setup.sh)로 실행하면 실제 실행 파일은 bin/ 안이라, 래퍼 옆의 설정도 찾도록 위 폴더를 본다.
	var conf config
	cp := *confPath
	if cp != "" {
		b, err := os.ReadFile(cp)
		if err != nil {
			fail(2, "오류: 설정 파일을 읽을 수 없습니다: %v", err)
		}
		conf = parseConf(string(b))
	} else {
		cands := []string{"new_setup.conf"}
		if exe, err := os.Executable(); err == nil {
			if r, err := filepath.EvalSymlinks(exe); err == nil {
				exe = r
			}
			d := filepath.Dir(exe)
			cands = append(cands, filepath.Join(d, "new_setup.conf"))
			if filepath.Base(d) == "bin" {
				cands = append(cands, filepath.Join(filepath.Dir(d), "new_setup.conf"))
			}
		}
		conf = parseConf("")
		for _, c := range cands {
			if b, err := os.ReadFile(c); err == nil {
				conf = parseConf(string(b))
				fmt.Fprintf(os.Stderr, "설정 파일: %s\n", c)
				break
			}
		}
	}

	// 장애 목록: -er 지정 시 반드시 있어야 한다. 생략하면 현재 경로의 error_list.txt 를 있으면 쓴다.
	notes := map[string]string{}
	if *errPath != "" {
		b, err := os.ReadFile(*errPath)
		if err != nil {
			fail(2, "오류: 장애 목록 파일을 읽을 수 없습니다: %v", err)
		}
		notes = parseErrorList(string(b))
	} else if b, err := os.ReadFile(defaultErrorList); err == nil {
		notes = parseErrorList(string(b))
		fmt.Fprintf(os.Stderr, "장애 목록: %s\n", defaultErrorList)
	}

	remoteCmd := buildRemoteCmd(conf.DiskPatterns, conf.ErrPatterns)
	infoCmd := buildInfoCmd(conf)
	// ★ gossh 는 위험어가 든 명령을 거부하므로, 시작 전에 설정 문구를 검사해 알려 준다.
	pats := append(append([]string{}, conf.DiskPatterns...), conf.ErrPatterns...)
	pats = append(pats, conf.CPUIntelLog, conf.CPUIntelOK, conf.CPUAMDLog, conf.CPUAMDOK)
	bad := dangerousPatterns(pats)
	if len(bad) == 0 && isDangerousCommand(remoteCmd) {
		bad = []string{remoteCmd}
	}
	if len(bad) == 0 && isDangerousCommand(infoCmd) {
		bad = []string{infoCmd}
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
	fmt.Fprintf(os.Stderr, "[1/3] 전체 %d대 접속 확인·정보 수집 중...\n", len(hosts))
	infoOut, err := runGossh(gossh, []string{"-w", hostFile, "-pm", "-script", infoCmd}, logf)
	if err != nil {
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
		fmt.Fprintf(os.Stderr, "[2/3] 설치중 후보 %d대 로그 확인 중...\n", len(cands))
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
	count := map[string]int{}
	var doneHosts []string
	for i, h := range hosts {
		count[statuses[i]]++
		if statuses[i] == stDone {
			doneHosts = append(doneHosts, h)
		}
	}

	// --- 설정체크 (완료 장비만, OS_CHECK_SH 설정 시) ---
	check := map[string]string{} // 없으면 '-'
	if conf.OSCheckSh != "" && len(doneHosts) > 0 {
		fmt.Fprintf(os.Stderr, "[3/3] 완료 %d대 설정체크 중...\n", len(doneHosts))
		res, err := runConfigCheck(conf, expandPath(conf.OSCheckSh), gossh, doneHosts, logf)
		if err != nil {
			fmt.Fprintf(os.Stderr, "경고: 설정체크를 건너뜁니다(열은 '-'): %v\n", err)
		} else {
			check = res
		}
	}

	info := parseInfoOutput(infoOut)
	var w bytes.Buffer
	w.WriteString(header + "\n")
	cc := map[string]int{}
	for i, h := range hosts {
		c := "-"
		if statuses[i] == stDone && check[h] != "" {
			c = check[h]
		}
		cc[c]++
		w.WriteString(formatRow(h, statuses[i], c, info[h], notes[strings.ToLower(h)]) + "\n")
	}
	os.Stdout.Write(w.Bytes())

	fmt.Fprintf(os.Stderr, "요약: 총 %d대 — 완료 %d, 디스크미인식 %d, 설치정지 %d, OS설치중 %d, 접속불가 %d (gossh 로그: %s)\n",
		len(hosts), count[stDone], count[stNoDisk], count[stStopped], count[stInstall], count[stUnreach], logPath)
	if conf.OSCheckSh != "" {
		fmt.Fprintf(os.Stderr, "설정체크: O %d, X %d, 출력없음 %d\n", cc["O"], cc["X"], cc[noOutput])
	}
}
