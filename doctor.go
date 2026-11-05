package main

// new_setup -doctor — 읽기 전용 문제 진단.
//   1. 환경 점검(항상): 바이너리·설정·gossh·config_check·stress-test·USER_ROUTE·쓰기 권한·위험어
//   2. -run 작업 진단(호스트파일을 주고 ./<호스트파일>_run/ 이 있을 때): state.json·워커·질문·CPU 시간표·호스트·worker.log·stress-test 이력
//   3. 호스트 단건 점검(-host): 실제 1차·2차 명령과 btime·uptime·ls 를 gossh 로 한 대에만 보내고 열 판정 근거를 설명
// 설정·상태 폴더 파일·원격 호스트를 바꾸지 않는다(원격에는 읽기 전용 명령만 보낸다). 보고서는 화면과 ./new_setup_doctor_<시각>.txt 에 낸다.
// 종료코드: 0 문제 없음, 1 문제([!]·[X]) 있음, 2 인자 오류.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"runtime/debug"
	"strconv"
	"strings"
	"time"
)

// 보고서 줄 머리표
const (
	lvOK   = "[O]"
	lvWarn = "[!]"
	lvBad  = "[X]"
	lvInfo = "[i]"
)

const (
	docHeartbeatStale = 15 * time.Second // 이보다 오래 heartbeat 가 없으면 워커가 멈춘 것으로 본다
	docLaunchSlow     = 10 * time.Minute // CPU 등록·launch 가 이보다 오래 걸리면 알린다
	docTailBytes      = 512 * 1024       // 로그 파일은 끝 이만큼만 읽는다
	docHelpTimeout    = 10 * time.Second // gossh -h 시간 제한
	docMaxRows        = 40               // 호스트 표 최대 줄 수
	mkMissing         = "NS_MISSING"     // 단건 점검: 로그 파일이 없음
)

type docItem struct{ Level, Msg, Fix string }

// docReport 는 보고서 본문과 문제 목록.
type docReport struct {
	b        strings.Builder
	Problems []docItem
}

func (r *docReport) head(title string) { fmt.Fprintf(&r.b, "\n== %s ==\n", title) }

// line 은 한 항목을 쓴다(여러 줄이면 둘째 줄부터 들여쓴다).
func (r *docReport) line(level, msg string) {
	lines := strings.Split(strings.TrimRight(msg, "\n"), "\n")
	r.b.WriteString(level + " " + lines[0] + "\n")
	for _, l := range lines[1:] {
		r.b.WriteString("    " + l + "\n")
	}
}

func (r *docReport) ok(f string, a ...interface{})   { r.line(lvOK, fmt.Sprintf(f, a...)) }
func (r *docReport) info(f string, a ...interface{}) { r.line(lvInfo, fmt.Sprintf(f, a...)) }

// problem 은 문제 항목을 쓰고 요약에 올린다.
func (r *docReport) problem(level, fix, f string, a ...interface{}) {
	msg := fmt.Sprintf(f, a...)
	r.line(level, msg)
	if fix != "" {
		r.b.WriteString("    -> 해결: " + strings.ReplaceAll(fix, "\n", "\n       ") + "\n")
	}
	r.Problems = append(r.Problems, docItem{level, strings.SplitN(msg, "\n", 2)[0], fix})
}

func (r *docReport) warn(fix, f string, a ...interface{}) { r.problem(lvWarn, fix, f, a...) }
func (r *docReport) bad(fix, f string, a ...interface{})  { r.problem(lvBad, fix, f, a...) }

// block 은 원문 여러 줄을 "    | " 머리로 그대로 보인다(max 줄 초과 시 끝만).
func (r *docReport) block(lines []string, max int) {
	if max > 0 && len(lines) > max {
		fmt.Fprintf(&r.b, "    | ... (앞 %d줄 생략)\n", len(lines)-max)
		lines = lines[len(lines)-max:]
	}
	for _, l := range lines {
		r.b.WriteString("    | " + strings.TrimRight(l, "\r") + "\n")
	}
	if len(lines) == 0 {
		r.b.WriteString("    | (출력 없음)\n")
	}
}

// finish 는 요약을 붙여 보고서 전체와 종료코드를 돌려준다.
func (r *docReport) finish() (string, int) {
	var nb, nw int
	for _, p := range r.Problems {
		if p.Level == lvBad {
			nb++
		} else {
			nw++
		}
	}
	r.head("요약")
	if len(r.Problems) == 0 {
		r.ok("문제가 발견되지 않았습니다.")
		return r.b.String(), 0
	}
	fmt.Fprintf(&r.b, "문제 %d건 (%s 오류 %d, %s 주의 %d)\n", len(r.Problems), lvBad, nb, lvWarn, nw)
	for i, p := range r.Problems {
		fmt.Fprintf(&r.b, "%d. %s %s\n", i+1, p.Level, p.Msg)
		if p.Fix != "" {
			r.b.WriteString("     -> 해결: " + strings.ReplaceAll(p.Fix, "\n", "\n        ") + "\n")
		}
	}
	return r.b.String(), 1
}

// ---------------------------------------------------------------------------
// 주입 가능한 외부 동작
// ---------------------------------------------------------------------------

type doctorArgs struct {
	conf     string // -c
	user     string // -user (stress-test conf 이름 추정용)
	host     string // -host 단건 점검
	hostFile string // 호스트파일 인자(-run 진단)
	check    bool   // -check: 단건 점검에서 config_check -auto-check 실행 허용
}

type doctorDeps struct {
	now        func() time.Time
	cwd        string
	loadConf   func(explicit string) (config, string, error)
	exePath    func() (string, error)
	lookPath   func(string) (string, error)
	alive      func(dir string) bool
	pidRunning func(pid int) bool
	gosshHelp  func(path string) (string, error)
	// gossh 는 gossh 를 한 번 돌려 stdout 과 stdout+stderr 를 돌려준다(비정상 종료는 오류가 아님, 실행 불가·시간 초과만 오류).
	gossh    func(path string, args []string, timeout time.Duration) (stdout, all string, err error)
	checkRun func(conf config, script, gossh string, hosts []string) (map[string]string, error)
}

func realDoctorDeps() doctorDeps {
	wd, _ := os.Getwd()
	return doctorDeps{
		now: time.Now, cwd: wd, loadConf: loadConf, exePath: os.Executable, lookPath: exec.LookPath,
		alive: workerAlive, pidRunning: procRunning,
		gosshHelp: func(path string) (string, error) {
			out, err := runCmd(exec.Command(path, "-h"), docHelpTimeout)
			if _, ok := err.(*exec.ExitError); ok {
				err = nil // 도움말은 종료코드 0 이 아닐 수 있다
			}
			return out, err
		},
		gossh: func(path string, args []string, timeout time.Duration) (string, string, error) {
			var so, se bytes.Buffer
			cmd := exec.Command(path, args...)
			cmd.Stdout, cmd.Stderr = &so, &se
			all, err := runCmd(cmd, timeout)
			if _, ok := err.(*exec.ExitError); ok {
				err = nil
			}
			return so.String(), all, err
		},
		checkRun: func(conf config, script, gossh string, hosts []string) (map[string]string, error) {
			return runConfigCheck(conf, script, gossh, hosts, ccCheck, "new_setup", io.Discard)
		},
	}
}

// procRunning 은 /proc/<pid> 가 있는지(리눅스 외에는 항상 false).
func procRunning(pid int) bool {
	if pid <= 0 {
		return false
	}
	_, err := os.Stat("/proc/" + strconv.Itoa(pid))
	return err == nil
}

// ---------------------------------------------------------------------------
// 표시 도우미
// ---------------------------------------------------------------------------

func tm(t time.Time) string {
	if t.IsZero() {
		return "-"
	}
	return t.Format("2006-01-02 15:04:05")
}

// koDur 는 시간 길이를 한글로(예: 2시간 5분, 3분 10초, 12초).
func koDur(d time.Duration) string {
	if d < 0 {
		d = -d
	}
	s := int(d.Round(time.Second) / time.Second)
	switch {
	case s >= 3600:
		return fmt.Sprintf("%d시간 %d분", s/3600, s%3600/60)
	case s >= 60:
		return fmt.Sprintf("%d분 %d초", s/60, s%60)
	}
	return fmt.Sprintf("%d초", s)
}

// agoText 는 t 가 now 보다 앞이면 "N 전", 뒤면 "N 후".
func agoText(now, t time.Time) string {
	if t.IsZero() {
		return "-"
	}
	if d := now.Sub(t); d >= 0 {
		return koDur(d) + " 전"
	}
	return koDur(t.Sub(now)) + " 후"
}

func orEmpty(s string) string {
	if s == "" {
		return "(비어 있음)"
	}
	return s
}

// readTail 은 파일 끝 max 바이트를 읽는다.
func readTail(path string, max int64) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return "", err
	}
	if fi.Size() > max {
		if _, err := f.Seek(fi.Size()-max, io.SeekStart); err != nil {
			return "", err
		}
	}
	b, err := io.ReadAll(f)
	return string(b), err
}

func lastLines(text string, n int) []string {
	text = strings.TrimRight(text, "\r\n")
	if text == "" {
		return nil
	}
	l := strings.Split(text, "\n")
	if len(l) > n {
		l = l[len(l)-n:]
	}
	return l
}

var (
	secretKV  = regexp.MustCompile(`(?i)((?:pass(?:word|wd|phrase)?|pwd|secret|token|api[_-]?key)[A-Za-z0-9_]*\s*[=:]\s*)("[^"]*"|'[^']*'|\S+)`)
	secretOpt = regexp.MustCompile(`(?i)(--(?:pass(?:word|wd)?|token|secret)[ =])(\S+)`)
)

// maskSecrets 는 비밀번호처럼 보이는 값을 가린다.
func maskSecrets(s string) string {
	s = secretKV.ReplaceAllString(s, "${1}****")
	return secretOpt.ReplaceAllString(s, "${1}****")
}

// ---------------------------------------------------------------------------
// 1. 환경 점검
// ---------------------------------------------------------------------------

type docEnvInfo struct {
	conf    config
	gossh   string // 확인된 gossh 경로("" = 사용 불가)
	history string // stress-test history_file 절대 경로("" = 모름)
}

func buildStamp() string {
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return ""
	}
	var t, rev, mod string
	for _, s := range bi.Settings {
		switch s.Key {
		case "vcs.time":
			t = s.Value
		case "vcs.revision":
			rev = s.Value
		case "vcs.modified":
			mod = s.Value
		}
	}
	if t == "" {
		return ""
	}
	out := "소스 커밋 시각 " + t
	if rev != "" {
		if len(rev) > 8 {
			rev = rev[:8]
		}
		out += ", 커밋 " + rev
	}
	if mod == "true" {
		out += " (커밋 안 된 수정 포함)"
	}
	return out
}

// execProblem 은 경로 하나가 파일이고(필요하면 실행 가능한지) 확인해 문제 사유를 돌려준다("" = 정상).
func execProblem(p string, needExec bool) string {
	fi, err := os.Stat(p)
	switch {
	case err != nil:
		return "파일이 없거나 접근할 수 없습니다 (" + err.Error() + ")"
	case fi.IsDir():
		return "폴더입니다(파일이어야 함)"
	case needExec && runtime.GOOS != "windows" && fi.Mode()&0o111 == 0:
		return "실행 권한이 없습니다"
	}
	return ""
}

var autoRe = regexp.MustCompile(`(?m)(^|[^\w-])-auto($|[^\w-])`)

func docEnv(r *docReport, a doctorArgs, d doctorDeps) *docEnvInfo {
	r.head("1. 환경 점검")
	env := &docEnvInfo{}

	// --- 실행 파일
	if exe, err := d.exePath(); err == nil {
		if res, err := filepath.EvalSymlinks(exe); err == nil {
			exe = res
		}
		if fi, err := os.Stat(exe); err == nil {
			r.info("실행 파일: %s\n크기 %d 바이트, 파일 수정 시각 %s", exe, fi.Size(), tm(fi.ModTime()))
		} else {
			r.info("실행 파일: %s", exe)
		}
	}
	if s := buildStamp(); s != "" {
		r.info("빌드 정보: %s (%s %s/%s)", s, runtime.Version(), runtime.GOOS, runtime.GOARCH)
	} else {
		r.info("빌드 시각 정보가 없습니다(%s %s/%s). 위 파일 수정 시각을 빌드 시각으로 보세요", runtime.Version(), runtime.GOOS, runtime.GOARCH)
	}
	r.info("실행 사용자 %s, 현재 경로 %s", orEmpty(os.Getenv("USER")), d.cwd)

	// --- 설정 파일
	conf, confPath, err := d.loadConf(a.conf)
	switch {
	case err != nil:
		r.bad("-c 로 준 설정 파일 경로·권한을 확인하세요", "설정 파일 오류: %v (기본값으로 계속 점검합니다)", err)
		conf = parseConf("")
	case confPath == "":
		r.info("설정 파일 없음 → 기본값 사용 (찾는 순서: 현재 경로, 실행 파일 폴더, bin 이면 그 위 폴더의 new_setup.conf)")
	default:
		r.ok("설정 파일: %s", confPath)
	}
	env.conf = conf
	for _, w := range conf.Warnings {
		r.warn("설정 파일의 해당 값을 고치세요", "설정 경고: %s", w)
	}
	r.info("설정값:\n%s", confDump(conf))

	// --- gossh
	env.gossh = docGossh(r, conf, d)

	// --- OS_CHECK_SH
	docCheckScript(r, conf, d)

	// --- stress-test
	user := a.user
	if user == "" {
		user = os.Getenv("STRESS_USER")
	}
	if user == "" {
		user = os.Getenv("USER")
	}
	env.history = docStress(r, conf, user, d)

	// --- USER_ROUTE
	if conf.UserRoute == "" {
		r.info("USER_ROUTE 비어 있음 → -run 의 user 는 -user 또는 환경변수 USER 로 정함")
	} else {
		var miss []string
		for _, n := range []string{"info_mn.sh", "info.sh"} {
			if p := filepath.Join(conf.UserRoute, n); execProblem(p, false) != "" {
				miss = append(miss, p)
			}
		}
		if len(miss) > 0 {
			r.bad("USER_ROUTE 폴더와 그 안의 info_mn.sh·info.sh 를 확인하세요", "USER_ROUTE 스크립트가 없습니다: %s", strings.Join(miss, ", "))
		} else {
			r.ok("USER_ROUTE: %s (info_mn.sh, info.sh 있음)", conf.UserRoute)
		}
	}

	// --- 현재 경로 쓰기(-run 상태 폴더·결과 파일이 여기에 생긴다)
	if f, err := os.CreateTemp(d.cwd, ".new_setup_doctor_probe"); err != nil {
		r.bad("쓰기 가능한 경로로 이동해서 실행하세요(-run 상태 폴더 ./<호스트파일>_run 과 gossh 결과 파일이 현재 경로에 생김)", "현재 경로에 쓸 수 없습니다: %v", err)
	} else {
		f.Close()
		os.Remove(f.Name())
		r.ok("현재 경로 쓰기 가능: %s", d.cwd)
	}

	// --- 위험어·csh 호환
	docDanger(r, conf)
	return env
}

func confDump(c config) string {
	rows := [][2]string{
		{"GOSSH", c.Gossh}, {"OS_CHECK_SH", c.OSCheckSh}, {"OS_CHECK_EXCLUDE", c.OSCheckExclude},
		{"CPU_INTEL_LOG", c.CPUIntelLog}, {"CPU_INTEL_OK", c.CPUIntelOK}, {"CPU_INTEL_LINE", c.CPUIntelLine},
		{"CPU_AMD_LOG", c.CPUAMDLog}, {"CPU_AMD_OK", c.CPUAMDOK}, {"CPU_AMD_LINE", c.CPUAMDLine},
		{"STRESS_TEST", c.StressTest}, {"STRESS_TEST_CONF", c.StressTestConf}, {"USER_ROUTE", c.UserRoute},
		{"CPU_WAIT_START", c.CPUWaitStart.String()}, {"CPU_WAIT_INTERVAL", c.CPUWaitInt.String()}, {"CPU_WAIT_MAX", c.CPUWaitMax.String()},
		{"GOSSH_TIMEOUT", c.GosshTimeout.String()}, {"CHECK_TIMEOUT", c.CheckTimeout.String()}, {"STRESS_TIMEOUT", c.StressTimeout.String()},
	}
	var l []string
	for _, kv := range rows {
		l = append(l, fmt.Sprintf("%-18s = %s", kv[0], orEmpty(kv[1])))
	}
	for _, p := range c.DiskPatterns {
		l = append(l, fmt.Sprintf("%-18s = %s", "DISK_PATTERN", p))
	}
	for _, p := range c.ErrPatterns {
		l = append(l, fmt.Sprintf("%-18s = %s", "ERROR_PATTERN", p))
	}
	return strings.Join(l, "\n")
}

// docGossh 는 gossh 경로를 풀고 실행해 본다. 사용할 수 있으면 경로를, 아니면 ""(문제는 보고서에 기록).
func docGossh(r *docReport, conf config, d doctorDeps) string {
	g := resolveGosshPath(conf.Gossh)
	src := "설정 GOSSH"
	if g == "" {
		p, err := d.lookPath("gossh")
		if err != nil {
			r.bad("설정 파일의 GOSSH= 에 gossh 경로를 쓰거나 PATH 에 추가하세요", "gossh 를 찾을 수 없습니다(설정 GOSSH 가 비어 있고 PATH 에도 없음)")
			return ""
		}
		g, src = p, "PATH"
	}
	if why := execProblem(g, true); why != "" {
		r.bad("GOSSH 경로를 고치거나 chmod +x 로 실행 권한을 주세요", "gossh(%s) 사용 불가: %s — %s", src, g, why)
		return ""
	}
	out, err := d.gosshHelp(g)
	if err != nil {
		r.bad("gossh 를 직접 실행해 보세요: "+g+" -h", "gossh(%s) 를 실행할 수 없습니다: %s — %v", src, g, err)
		return ""
	}
	var lack []string
	for _, f := range []string{"-w", "-pm", "-script"} {
		if !regexp.MustCompile(`(?m)^\s*` + regexp.QuoteMeta(f) + `(\s|$)`).MatchString(out) {
			lack = append(lack, f)
		}
	}
	if len(lack) > 0 {
		r.warn("new_setup 이 쓰는 옵션을 지원하는 gossh 로 바꾸세요", "gossh(%s) 도움말에 필요한 옵션이 없습니다: %s (%s)", src, strings.Join(lack, " "), g)
		return g
	}
	r.ok("gossh(%s): %s — 실행됨, -w -pm -script 지원", src, g)
	return g
}

// docCheckScript 는 OS_CHECK_SH(config_check.sh)를 점검한다.
func docCheckScript(r *docReport, conf config, d doctorDeps) {
	if conf.OSCheckSh == "" {
		r.info("OS_CHECK_SH 비어 있음 → 설정체크 안 함(설정체크 열은 '-', -run 설정수정 단계 사용 불가)")
		return
	}
	script := expandPath(conf.OSCheckSh)
	if why := execProblem(script, false); why != "" {
		r.bad("OS_CHECK_SH 경로를 고치세요(체크 스크립트는 관리서버에서 bash 로 실행됨)", "OS_CHECK_SH 사용 불가: %s — %s", script, why)
		return
	}
	b, err := os.ReadFile(script)
	if err != nil {
		r.bad("읽기 권한을 확인하세요", "OS_CHECK_SH 를 읽을 수 없습니다: %v", err)
		return
	}
	text := string(b)
	if !strings.Contains(text, "-auto-check") {
		r.bad("-auto-check 를 지원하는 새 config_check.sh 로 교체하세요(구버전에 인자를 주면 메뉴에서 멈추므로 실행하지 않고 설정체크 열이 '-' 가 됨)", "OS_CHECK_SH(%s)가 -auto-check 를 지원하지 않습니다(구버전)", script)
	} else {
		r.ok("OS_CHECK_SH: %s — -auto-check 지원", script)
	}
	if !autoRe.MatchString(text) {
		r.warn("-run 설정수정 단계를 쓰려면 -auto <user> <목록> 을 지원하는 config_check.sh 가 필요합니다", "OS_CHECK_SH 에서 -auto 인자를 찾지 못했습니다(-run 의 설정수정에 필요)")
	} else {
		r.ok("OS_CHECK_SH: -auto 지원(-run 설정수정)")
	}
	if _, err := d.lookPath("bash"); err != nil {
		r.bad("bash 를 설치하거나 PATH 에 추가하세요", "bash 를 찾을 수 없습니다(config_check.sh 를 bash 로 실행함)")
	}
	if p := filepath.Join(filepath.Dir(script), "dhcp.sh"); fileExists(p) {
		r.info("같은 폴더의 dhcp.sh 를 작업 폴더로 링크해 사용: %s", p)
	}
	if conf.OSCheckExclude != "" {
		r.info("OS_CHECK_EXCLUDE=%s → 설정체크 결과에서 이 문구가 든 항목은 무시", conf.OSCheckExclude)
	}
}

// docStress 는 STRESS_TEST 바이너리와 그 conf 를 점검하고 history_file 절대 경로를 돌려준다.
func docStress(r *docReport, conf config, user string, d doctorDeps) string {
	if conf.StressTest == "" {
		r.info("STRESS_TEST 비어 있음 → -run 부하 단계(CPU·stress-ng) 사용 불가")
		return ""
	}
	bin, err := filepath.Abs(conf.StressTest)
	if err != nil {
		bin = conf.StressTest
	}
	if why := execProblem(bin, true); why != "" {
		r.bad("STRESS_TEST 경로를 고치거나 chmod +x 로 실행 권한을 주세요", "STRESS_TEST 사용 불가: %s — %s", bin, why)
		return ""
	}
	r.ok("STRESS_TEST: %s (실행 가능, 직접 실행하지는 않음)", bin)
	binDir := filepath.Dir(bin)
	// 워커는 stress-test 를 바이너리 폴더에서 실행하므로 ./conf 는 <바이너리 폴더>/conf 이다.
	name := user + "_setting.conf"
	var cands []string
	if conf.StressTestConf != "" {
		c, _ := filepath.Abs(conf.StressTestConf)
		if execProblem(c, false) != "" {
			r.bad("STRESS_TEST_CONF 경로를 고치세요(stress-test -conf 로 넘김)", "STRESS_TEST_CONF 파일이 없습니다: %s", c)
			return ""
		}
		cands = []string{c}
	} else {
		if user == "" {
			r.warn("-user 로 사용자를 알려 주세요", "stress-test conf 를 찾을 user 를 알 수 없습니다")
			return ""
		}
		cands = []string{filepath.Join(binDir, "conf", name)}
		if home, err := os.UserHomeDir(); err == nil {
			cands = append(cands, filepath.Join(home, ".stress-test", name))
		}
	}
	for _, c := range cands {
		if execProblem(c, false) != "" {
			continue
		}
		if conf.StressTestConf != "" {
			r.ok("stress-test conf: %s (STRESS_TEST_CONF)", c)
		} else {
			r.ok("stress-test conf: %s (user=%s, 탐색 순서 ./conf → ~/.stress-test → <바이너리 폴더>/conf)", c, user)
		}
		b, err := os.ReadFile(c)
		if err != nil {
			r.warn("conf 읽기 권한을 확인하세요", "stress-test conf 를 읽을 수 없습니다: %v", err)
			return ""
		}
		h := stressHistoryPath(string(b), binDir)
		r.info("stress-test 이력 파일(history_file): %s", h)
		return h
	}
	r.bad("conf/sample_setting.conf 를 복사해 "+name+" 을 만들거나 STRESS_TEST_CONF 로 경로를 지정하세요",
		"stress-test conf(%s)를 찾지 못했습니다. 확인한 경로: %s", name, strings.Join(cands, ", "))
	return ""
}

// stressHistoryPath 는 stress-test conf 본문에서 history_file 을 읽는다(기본 ./stress-test_history.log, 상대 경로는 바이너리 폴더 기준).
func stressHistoryPath(text, binDir string) string {
	p := "./stress-test_history.log"
	for _, line := range strings.Split(text, "\n") {
		if i := strings.Index(line, "#"); i >= 0 {
			line = line[:i]
		}
		k, v, ok := strings.Cut(line, "=")
		if ok && strings.TrimSpace(k) == "history_file" && strings.TrimSpace(v) != "" {
			p = unquote(strings.TrimSpace(v))
		}
	}
	if !filepath.IsAbs(p) {
		p = filepath.Join(binDir, p)
	}
	return p
}

// docDanger 는 설정 문구의 gossh 위험어와 csh 에서 깨지는 문자를 점검한다.
func docDanger(r *docReport, conf config) {
	pats := append(append([]string{}, conf.DiskPatterns...), conf.ErrPatterns...)
	pats = append(pats, conf.CPUIntelLog, conf.CPUIntelOK, conf.CPUIntelLine, conf.CPUAMDLog, conf.CPUAMDOK, conf.CPUAMDLine)
	bad := dangerousPatterns(pats)
	cmds := map[string]string{
		"1차 정보수집 명령": buildInfoCmd(conf),
		"2차 확인 명령":   buildRemoteCmd(conf.DiskPatterns, conf.ErrPatterns),
		"CPU 감시 명령":  probeCmd(conf),
	}
	for _, n := range []string{"1차 정보수집 명령", "2차 확인 명령", "CPU 감시 명령"} {
		if isDangerousCommand(cmds[n]) {
			bad = append(bad, n+": "+cmds[n])
		}
	}
	if len(bad) > 0 {
		r.bad("해당 문구에서 reboot, poweroff, shutdown, halt, init 0, init 6, ddc 등 gossh 위험어를 빼세요(gossh 가 명령을 거부함)",
			"설정 문구에 gossh 위험어가 있습니다: %s", strings.Join(bad, " | "))
	} else {
		r.ok("설정 문구에 gossh 위험어 없음(1차·2차·감시 명령 포함)")
	}
	var bang []string
	for _, p := range pats {
		if strings.Contains(p, "!") {
			bang = append(bang, p)
		}
	}
	if len(bang) > 0 {
		r.warn("'!' 가 없는 문구로 바꾸세요(csh/tcsh 로그인 셸은 작은따옴표 안에서도 !를 이력 치환함 — 랩 .60 처럼 root 셸이 csh 인 장비에서 깨짐)",
			"설정 문구에 '!' 가 있습니다: %s", strings.Join(bang, " | "))
	}
	if len(conf.ErrPatterns) == 0 {
		r.info("ERROR_PATTERN 없음 → '설치정지' 상태는 판정하지 않음(정상일 수 있음)")
	}
	for _, c := range []struct{ k, v string }{
		{"CPU_INTEL_LOG", conf.CPUIntelLog}, {"CPU_INTEL_OK", conf.CPUIntelOK},
		{"CPU_AMD_LOG", conf.CPUAMDLog}, {"CPU_AMD_OK", conf.CPUAMDOK},
	} {
		if c.v == "" {
			r.warn(c.k+" 에 값을 쓰세요(또는 줄을 지우면 기본값)", "%s 가 비어 있어 해당 벤더의 CPU 테스트 확인을 건너뜁니다(CPU 열이 항상 X)", c.k)
		}
	}
}

// ---------------------------------------------------------------------------
// 2. -run 작업 진단
// ---------------------------------------------------------------------------

func readPID(path string) int {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	n, _ := strconv.Atoi(strings.TrimSpace(string(b)))
	return n
}

func docRun(r *docReport, a doctorArgs, d doctorDeps, env *docEnvInfo) {
	r.head("2. -run 작업 진단")
	clean := strings.TrimPrefix(a.hostFile, "^")
	var hosts []string
	if raw, err := os.ReadFile(clean); err != nil {
		r.bad("호스트 파일 경로를 확인하세요", "호스트 파일을 읽을 수 없습니다: %v", err)
	} else if hosts = parseHostLines(string(raw)); len(hosts) == 0 {
		r.warn("호스트 파일에 한 줄에 hostname 하나씩 쓰세요", "호스트 파일에 등록된 호스트가 없습니다: %s", clean)
	} else {
		r.ok("호스트 파일: %s (%d대)", clean, len(hosts))
	}
	dir := filepath.Join(d.cwd, filepath.Base(clean)+"_run")
	if _, err := os.Stat(dir); err != nil {
		r.info("상태 폴더가 없습니다: %s\n이 경로에서 `new_setup -run %s` 를 실행한 적이 없다는 뜻입니다(다른 경로에서 실행했다면 그 경로로 이동해서 다시 점검하세요).", dir, a.hostFile)
		return
	}
	r.info("상태 폴더: %s", dir)
	docDirList(r, dir, d.now())

	st, err := loadState(dir)
	if err != nil {
		if os.IsNotExist(err) {
			r.bad("-run 이 처음 시작되기 전에 실패했거나 state.json 이 지워졌습니다. worker.log 를 확인하고, 상태 폴더를 지운 뒤 다시 -run 하세요", "state.json 이 없습니다")
		} else {
			r.bad("상태 폴더를 백업(mv)한 뒤 지우고 다시 -run 하세요(진행 상황은 복구되지 않음)", "state.json 을 읽을 수 없습니다: %v", err)
		}
		docWorkerLog(r, dir, nil, d)
		return
	}
	now := d.now()
	fin := st.Finished()
	r.info("작업: user=%s, 생성 %s (%s), 호스트 %d대\n호스트파일 인자=%s, 실행 경로=%s",
		st.User, tm(st.Created), agoText(now, st.Created), len(st.Hosts), st.HostArg, st.WorkDir)
	if st.Version != stateVersion {
		r.warn("같은 버전의 new_setup 으로 다시 실행하세요", "state.json 버전(%d)이 이 바이너리의 버전(%d)과 다릅니다", st.Version, stateVersion)
	}
	if st.WorkDir != "" && st.WorkDir != d.cwd {
		r.warn("작업을 시작한 경로로 이동해 같은 명령을 실행하세요: cd "+st.WorkDir, "현재 경로(%s)가 작업을 시작한 경로(%s)와 다릅니다", d.cwd, st.WorkDir)
	}
	if st.ConfPath != "" && execProblem(st.ConfPath, false) != "" {
		r.warn("작업에 기록된 설정 파일이 없습니다. 같은 위치에 복원하세요(워커는 이 경로의 설정을 읽음)", "state 에 기록된 설정 파일을 읽을 수 없습니다: %s", st.ConfPath)
	}
	if len(hosts) > 0 {
		if diff := hostDiff(hosts, st.Hosts); diff != "" {
			r.warn("호스트 파일이 바뀌었습니다. 새 목록으로 하려면 상태 폴더를 지우고 다시 -run 하세요", "호스트 파일의 호스트가 작업 상태와 다릅니다: %s", diff)
		}
	}

	// 단계·진행
	stageLine := fmt.Sprintf("단계 %d %s", st.Stage, stageName(st.Stage))
	switch {
	case st.Stopped:
		stageLine += " — 중지됨"
	case st.Stage == stageFinish:
		stageLine += " — 완료"
	}
	r.info("%s\nbusy=%v, 상세: %s\n마지막 기록 %s (%s)", stageLine, st.Busy, orEmpty(st.Detail), tm(st.Updated), agoText(now, st.Updated))

	docWorker(r, st, dir, a, d, now, fin)
	docQuestion(r, st, dir, a, d, fin)
	docStopReq(r, st, dir, a, d, fin)
	docTimeline(r, st, dir, env, d, now, fin)
	if st.LastError != "" {
		r.bad(lastErrorAdvice(st.LastError), "last_error: %s", st.LastError)
	}
	docHostRows(r, st, fin)
	docWorkerLog(r, dir, st, d)
	docHistory(r, st, env, now)
}

// hostDiff 는 호스트 파일과 상태의 호스트 집합이 다르면 차이를 설명한다.
func hostDiff(file []string, rows []hostRow) string {
	have, want := map[string]bool{}, toSet(file)
	var more, less []string
	for _, r := range rows {
		have[r.Host] = true
		if !want[r.Host] {
			more = append(more, r.Host)
		}
	}
	for _, h := range file {
		if !have[h] {
			less = append(less, h)
		}
	}
	var p []string
	if len(less) > 0 {
		p = append(p, "파일에만 있음 "+strings.Join(trimList(less, 5), ","))
	}
	if len(more) > 0 {
		p = append(p, "작업에만 있음 "+strings.Join(trimList(more, 5), ","))
	}
	return strings.Join(p, "; ")
}

func trimList(l []string, n int) []string {
	if len(l) <= n {
		return l
	}
	return append(append([]string{}, l[:n]...), fmt.Sprintf("외 %d대", len(l)-n))
}

func docDirList(r *docReport, dir string, now time.Time) {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	var l []string
	for _, e := range ents {
		fi, err := e.Info()
		if err != nil {
			continue
		}
		if e.IsDir() {
			l = append(l, fmt.Sprintf("%-14s 폴더", e.Name()+"/"))
			continue
		}
		l = append(l, fmt.Sprintf("%-14s %8d바이트  %s (%s)", e.Name(), fi.Size(), tm(fi.ModTime()), agoText(now, fi.ModTime())))
	}
	r.info("상태 폴더 파일:\n%s", strings.Join(l, "\n"))
}

func lastErrorAdvice(e string) string {
	switch {
	case strings.Contains(e, "gossh"):
		return "설정 파일의 GOSSH= 경로나 PATH 를 고친 뒤 같은 명령으로 다시 실행하세요(워커 재기동)"
	case strings.Contains(e, "설정 파일"):
		return "state 에 기록된 설정 파일 경로를 복원하거나 상태 폴더를 지우고 -c 로 다시 시작하세요"
	case strings.Contains(e, "작업 경로"):
		return "작업을 시작한 경로가 지워졌거나 접근할 수 없습니다. 경로를 복원하거나 새 경로에서 다시 시작하세요"
	case strings.Contains(e, "시간초과"):
		return "GOSSH_TIMEOUT·CHECK_TIMEOUT·STRESS_TIMEOUT 을 늘리거나 해당 명령(gossh/config_check/stress-test)이 멈춘 원인을 확인한 뒤 TUI 에서 r(재시도)"
	case strings.Contains(e, "OS_CHECK_SH"), strings.Contains(e, "설정수정"):
		return "OS_CHECK_SH 가 -auto 를 지원하는지 확인하세요(위 환경 점검 참고)"
	case strings.Contains(e, "STRESS_TEST"), strings.Contains(e, "stress-test"):
		return "STRESS_TEST 경로·conf 를 확인하고 worker.log 의 stress-test 출력을 보세요(위 환경 점검 참고)"
	}
	return "worker.log 의 해당 시각 줄을 확인하고, 원인을 고친 뒤 TUI 에서 r(재시도)"
}

func docWorker(r *docReport, st *runState, dir string, a doctorArgs, d doctorDeps, now time.Time, fin bool) {
	alive := d.alive(dir)
	filePID := readPID(filepath.Join(dir, pidFile))
	pid := st.WorkerPID
	if pid == 0 {
		pid = filePID
	}
	r.info("워커: lock %s, worker.pid=%d(프로세스 %s), state.worker_pid=%d",
		map[bool]string{true: "잡혀 있음(살아 있음)", false: "안 잡힘"}[alive], filePID,
		map[bool]string{true: "있음", false: "없음"}[d.pidRunning(filePID)], st.WorkerPID)
	rerun := "같은 경로(" + d.cwd + ")에서 같은 명령으로 다시 실행하세요: new_setup -run " + a.hostFile + "\n→ '이어서 진행할까요?' 에 y 를 답하면 저장된 단계부터 이어서 진행합니다(진행 중이던 명령은 다시 실행하지 않고 재체크부터)"
	switch {
	case fin && !alive:
		r.info("작업이 끝났습니다(%s). 결과: %s", st.Detail, filepath.Join(dir, resultFile))
	case fin:
		r.info("작업은 끝났는데 워커가 아직 정리 중입니다(%s)", st.Detail)
	case !alive:
		extra := ""
		if st.Busy {
			extra = " — 외부 명령(판정·설정수정·stress-test 등)을 실행하던 중에 죽었습니다"
		}
		r.bad(rerun, "워커가 죽어 있습니다: %s 단계 진행 중이었음%s", stageName(st.Stage), extra)
		if d.pidRunning(pid) {
			r.warn("lock 파일이 지워졌거나 다른 사용자의 프로세스일 수 있습니다. `ps -p "+strconv.Itoa(pid)+" -o pid,etime,cmd` 로 확인하세요", "lock 은 안 잡혀 있는데 pid %d 프로세스는 있습니다", pid)
		}
	default:
		if st.Heartbeat.IsZero() {
			r.warn("워커가 막 시작됐다면 잠시 뒤 다시 점검하세요", "heartbeat 기록이 없습니다")
		} else if age := now.Sub(st.Heartbeat); age > docHeartbeatStale {
			r.bad("워커(pid "+strconv.Itoa(pid)+")가 멈춘(hang) 듯합니다. `kill -9 "+strconv.Itoa(pid)+"` 후 남은 외부 명령을 `ps -ef | grep -E 'gossh|stress-test|config_check'` 로 확인하고,\n"+rerun,
				"워커는 살아 있으나(lock 잡힘, pid %d) heartbeat 가 %s 동안 갱신되지 않았습니다(정상은 3초마다 갱신, 기준 %s)", pid, koDur(age), koDur(docHeartbeatStale))
		} else {
			r.ok("워커 정상: 살아 있고 heartbeat %s 전 (pid %d)", koDur(age), pid)
		}
	}
}

func docQuestion(r *docReport, st *runState, dir string, a doctorArgs, d doctorDeps, fin bool) {
	ans := readAnswer(dir)
	q := st.Question
	if q == nil {
		if ans.exists {
			r.info("answer.json 이 남아 있지만 대기 중인 질문은 없습니다(워커가 읽고 지울 것: %s)", ans.desc())
		}
		return
	}
	var ch []string
	for _, c := range q.Choices {
		ch = append(ch, c.Key+" "+c.Label)
	}
	now := d.now()
	fix := "TUI 에서 답하세요: new_setup -run " + a.hostFile + "   (키: " + strings.Join(ch, " / ") + ")"
	if !d.alive(dir) && !fin {
		fix = "워커가 죽어 있으니 먼저 같은 명령으로 다시 실행하세요(이어서 진행 y). 재기동하면 이 질문은 다시 나옵니다: new_setup -run " + a.hostFile
	}
	r.warn(fix, "질문 %s 에 대한 답을 기다리는 중입니다(%s 단계, %s 질문)\n%s\n선택지: %s", q.ID, stageName(q.Stage), agoText(now, q.Asked), q.Text, strings.Join(ch, " / "))
	switch {
	case !ans.exists:
	case ans.err != nil:
		r.warn("answer.json 을 지우고 TUI 에서 다시 답하세요", "answer.json 을 해석할 수 없습니다(%v)", ans.err)
	case ans.a.ID != q.ID:
		r.warn("TUI 에서 현재 질문("+q.ID+")에 다시 답하세요", "answer.json 의 질문 id(%s)가 현재 질문(%s)과 달라 워커가 무시합니다", ans.a.ID, q.ID)
	case !d.alive(dir):
		r.warn("워커를 다시 실행하세요(위 안내). 답은 질문이 다시 나오면 새로 해야 합니다", "답(%s)이 기록돼 있지만 처리할 워커가 없습니다", ans.a.Choice)
	default:
		r.info("답(%s)이 기록돼 있어 워커가 1초 안에 처리합니다", ans.a.Choice)
	}
}

type answerRead struct {
	exists bool
	a      answer
	err    error
	mod    time.Time
}

func (x answerRead) desc() string {
	if x.err != nil {
		return "해석 불가"
	}
	return "id=" + x.a.ID + " choice=" + x.a.Choice
}

// readAnswer 는 answer.json 을 지우지 않고 읽는다(워커의 takeAnswer 와 달리 읽기 전용).
func readAnswer(dir string) answerRead {
	p := filepath.Join(dir, answerFile)
	fi, err := os.Stat(p)
	if err != nil {
		return answerRead{}
	}
	x := answerRead{exists: true, mod: fi.ModTime()}
	b, err := os.ReadFile(p)
	if err != nil {
		x.err = err
		return x
	}
	x.err = json.Unmarshal(b, &x.a)
	return x
}

func docStopReq(r *docReport, st *runState, dir string, a doctorArgs, d doctorDeps, fin bool) {
	fi, err := os.Stat(filepath.Join(dir, stopReqFile))
	if err != nil {
		return
	}
	when := tm(fi.ModTime())
	switch alive := d.alive(dir); {
	case fin:
		r.info("stop.req 가 남아 있습니다(%s). 작업이 이미 끝났으므로 무시해도 됩니다", when)
	case alive:
		r.warn("워커가 곧 중지합니다(실행 중인 외부 명령은 SIGTERM 으로 끝냄, 보통 몇 초 이내). 중지가 안 되면 heartbeat 상태를 보세요", "중지 요청(stop.req, %s)이 있습니다", when)
	default:
		r.warn("이어서 하려면 `rm "+filepath.Join(dir, stopReqFile)+"` 로 stop.req 를 지운 뒤 같은 명령으로 다시 실행하세요(그대로 두면 재기동 즉시 중지됨). 중지가 목적이었다면 그대로 두면 됩니다",
			"중지 요청(stop.req, %s)이 남아 있는데 워커가 없습니다", when)
	}
}

func docTimeline(r *docReport, st *runState, dir string, env *docEnvInfo, d doctorDeps, now time.Time, fin bool) {
	if st.CPULaunch.IsZero() && len(st.CPUList) == 0 {
		return
	}
	alive := d.alive(dir)
	if st.CPULaunch.IsZero() {
		r.info("CPU 대상 %d대: [%s], launch 시각 아직 없음 (CPU 등록·launch 진행 중이거나 실패)", len(st.CPUList), strings.Join(trimList(st.CPUList, 8), " "))
		if !fin && st.Stage == stageLoad && st.Busy && now.Sub(st.Updated) > docLaunchSlow {
			r.warn("worker.log 의 stress-test 출력(AWX 등록·launch 오류)을 확인하세요. 오래 걸리면 STRESS_TIMEOUT 이후 '시간초과' 질문이 나옵니다", "CPU 등록·launch 가 %s 동안 끝나지 않았습니다([감시] 줄이 아직 안 나옴)", koDur(now.Sub(st.Updated)))
		}
		return
	}
	r.info("CPU 시간표: launch %s (%s)\n확인 시작 %s (%s)\n마감 %s (%s)\n다음 확인 %s (%s), 감시 끝남=%v",
		tm(st.CPULaunch), agoText(now, st.CPULaunch), tm(st.CheckStart), agoText(now, st.CheckStart),
		tm(st.Deadline), agoText(now, st.Deadline), tm(st.NextCheck), agoText(now, st.NextCheck), st.CPUWaitDone)
	if st.CPUWaitDone || fin {
		return
	}
	switch {
	case !st.Deadline.IsZero() && now.After(st.Deadline) && alive:
		r.warn("worker.log 에서 'CPU 감시 확인 실패'·gossh 지연을 확인하세요. 워커 heartbeat 가 정상이면 곧 질문이 나옵니다", "CPU 완료 확인 마감(%s)이 지났는데 감시가 끝나지 않았습니다", tm(st.Deadline))
	case !st.Deadline.IsZero() && now.After(st.Deadline):
		r.info("CPU 완료 확인 마감이 지났습니다. 워커를 다시 실행하면 바로 재체크한 뒤 질문합니다")
	case alive && !st.NextCheck.IsZero() && now.Sub(st.NextCheck) > 2*env.conf.CPUWaitInt+docHeartbeatStale:
		r.warn("gossh 응답 지연일 수 있습니다. worker.log 의 'CPU 감시 확인 실패' 줄과 GOSSH_TIMEOUT 을 확인하세요", "다음 확인 시각(%s)이 %s 지났는데 감시가 진행되지 않았습니다", tm(st.NextCheck), koDur(now.Sub(st.NextCheck)))
	default:
		r.ok("CPU 감시 진행 중 — 다음 확인 %s", agoText(now, st.NextCheck))
	}
}

func docHostRows(r *docReport, st *runState, fin bool) {
	if len(st.Hosts) == 0 {
		return
	}
	cnt := map[string]int{}
	for _, h := range st.Hosts {
		cnt[h.Status]++
	}
	r.info("호스트 %d대 — 완료 %d, 디스크미인식 %d, 설치정지 %d, OS설치중 %d, 접속불가 %d",
		len(st.Hosts), cnt[stDone], cnt[stNoDisk], cnt[stStopped], cnt[stInstall], cnt[stUnreach])
	var rows []string
	var reboot, noresp, notOK int
	for i := range st.Hosts {
		h := &st.Hosts[i]
		var why []string
		if h.Excluded {
			why = append(why, "제외")
		}
		if h.Rebooted {
			why = append(why, prgRebooted)
			reboot++
		}
		if h.NoResp {
			why = append(why, prgNoResp)
			noresp++
		}
		if h.Running != "" {
			why = append(why, h.Running)
		}
		if h.Status != stDone {
			why = append(why, "상태 "+h.Status)
		} else if !h.Excluded {
			bad := false
			if !checkPass(st.CheckOff, h.Check) {
				why = append(why, "설정체크 "+h.Check)
				bad = true
			}
			if h.CPU != "O" {
				why = append(why, "CPU "+h.CPU)
				bad = true
			}
			if h.Stress != "O" {
				why = append(why, "stress-ng "+h.Stress)
				bad = true
			}
			if bad {
				notOK++
			}
		}
		if len(why) > 0 {
			rows = append(rows, fmt.Sprintf("%-20s %s", h.Host, strings.Join(why, ", ")))
		}
	}
	if len(rows) > 0 {
		show := rows
		if len(show) > docMaxRows {
			show = append(append([]string{}, rows[:docMaxRows]...), fmt.Sprintf("... 외 %d대", len(rows)-docMaxRows))
		}
		r.info("O 가 아니거나 특이사항이 있는 호스트 %d대:\n%s", len(rows), strings.Join(show, "\n"))
	}
	if reboot > 0 {
		r.warn("CPU 테스트가 중간에 끊겼을 수 있습니다. 해당 호스트의 부팅 이유(`last -x | head`, 전원·BMC 로그)를 확인하고 TUI 에서 r(재시도)", "CPU 테스트 중 재부팅된 호스트 %d대", reboot)
	}
	if noresp > 0 {
		r.warn("호스트 전원·네트워크를 확인하세요(`ping`, `ssh`). 다시 응답하면 다음 확인 때 자동으로 풀립니다. 호스트 단건 점검: new_setup -doctor -host <호스트>", "CPU 테스트 중 응답 없는 호스트 %d대", noresp)
	}
	if fin && notOK > 0 {
		r.warn("O 가 아닌 열의 원인을 확인하세요(호스트 단건 점검: new_setup -doctor -host <호스트>). 다시 하려면 상태 폴더를 백업 후 지우고 -run", "작업이 끝났지만 세 열이 모두 O 가 아닌 호스트가 %d대 있습니다", notOK)
	}
}

// docLogRules 는 worker.log 에서 찾는 문구와 설명.
var docLogRules = []struct{ sub, level, msg, fix string }{
	{"워커 시작 실패", lvBad, "워커가 시작 준비에서 실패했습니다", "바로 아래 줄의 사유(GOSSH·설정 파일·작업 경로)를 고친 뒤 같은 명령으로 다시 실행하세요"},
	{"시간초과", lvWarn, "외부 명령이 시간 제한을 넘겼습니다", "GOSSH_TIMEOUT·CHECK_TIMEOUT·STRESS_TIMEOUT 을 늘리거나 멈춘 원인을 확인한 뒤 TUI 에서 r(재시도)"},
	{"launch 되지 않았", lvBad, "CPU 테스트가 launch 되지 않았습니다", "stress-test 출력(AWX 등록·템플릿·conf)을 worker.log 에서 확인하세요. stress-test 단독 실행으로도 재현해 보세요"},
	{"판정 실패", lvWarn, "판정(gossh) 실패가 있었습니다", "gossh 경로·네트워크를 확인하세요(환경 점검, 호스트 단건 점검 참고)"},
	{"설정수정 실패", lvWarn, "설정수정(config_check -auto) 실패가 있었습니다", "OS_CHECK_SH 가 -auto 를 지원하는지, CHECK_TIMEOUT 이 충분한지 확인하세요"},
	{"CPU 감시 확인 실패", lvWarn, "CPU 감시 확인(gossh)이 실패한 적이 있습니다", "gossh 응답·GOSSH_TIMEOUT 을 확인하세요"},
	{"stress-test 오류 종료", lvWarn, "stress-test 가 오류 종료했습니다", "worker.log 의 stress-test 출력을 확인하세요"},
}

func docWorkerLog(r *docReport, dir string, st *runState, d doctorDeps) {
	p := filepath.Join(dir, workerLog)
	text, err := readTail(p, docTailBytes)
	if err != nil {
		r.info("worker.log 를 읽을 수 없습니다: %v", err)
		return
	}
	recent := lastLines(text, 200)
	joined := strings.Join(recent, "\n")
	for _, ru := range docLogRules {
		if !strings.Contains(joined, ru.sub) {
			continue
		}
		line := ""
		for i := len(recent) - 1; i >= 0; i-- {
			if strings.Contains(recent[i], ru.sub) {
				line = recent[i]
				break
			}
		}
		msg := "worker.log(최근 200줄)에서 발견: " + ru.msg + "\n" + line
		if ru.level == lvBad {
			r.bad(ru.fix, "%s", msg)
		} else {
			r.warn(ru.fix, "%s", msg)
		}
	}
	// 부하 단계에서 stress-test 를 불렀는데 [감시] 줄이 없으면 launch 에 못 간 것이다.
	if st != nil && st.Stage >= stageLoad && len(st.CPUList) > 0 && strings.Contains(text, "부하 단계 시작") &&
		strings.Contains(text, "+ (cd ") && !strings.Contains(text, stressMonitorMark) && st.CPULaunch.IsZero() {
		r.warn("stress-test 출력(worker.log 의 '+ (cd ...' 줄 뒤)에서 AWX 등록·launch 오류를 확인하세요. stress-test 를 단독으로 실행해 같은 증상인지 보세요",
			"worker.log 에 stress-test 의 %s 줄이 없습니다 — CPU 가 아직 launch 되지 않았습니다", stressMonitorMark)
	}
	r.info("worker.log 마지막 30줄(%s):", p)
	r.block(lastLines(text, 30), 0)
}

func docHistory(r *docReport, st *runState, env *docEnvInfo, now time.Time) {
	if env == nil || env.history == "" {
		r.info("stress-test 이력: history_file 위치를 알 수 없어 건너뜁니다(STRESS_TEST/conf 점검 참고)")
		return
	}
	text, err := readTail(env.history, docTailBytes)
	if err != nil {
		r.info("stress-test 이력 파일을 읽을 수 없습니다(%s): %v", env.history, err)
		return
	}
	var mine []string
	failSince := 0
	for _, l := range strings.Split(text, "\n") {
		if !strings.Contains(l+" ", "user="+st.User+" ") {
			continue
		}
		mine = append(mine, l)
		if f := strings.Fields(l); len(f) > 0 && strings.Contains(l, "status=fail") {
			if t, err := time.Parse(time.RFC3339, f[0]); err == nil && !t.Before(st.Created) {
				failSince++
			}
		}
	}
	if len(mine) == 0 {
		r.info("stress-test 이력(%s)에 user=%s 줄이 없습니다 — 이 user 로 stress-test 가 한 번도 안 돌았습니다", env.history, st.User)
		return
	}
	r.info("stress-test 이력(%s) 중 user=%s 마지막 10줄:", env.history, st.User)
	r.block(lastLines(strings.Join(mine, "\n"), 10), 0)
	if failSince > 0 {
		r.warn("이력의 error= 내용(AWX 템플릿·인벤토리·권한)을 확인하세요", "이 작업 시작 이후 stress-test launch 실패 기록이 %d건 있습니다", failSince)
	}
}
