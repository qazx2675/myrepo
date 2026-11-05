// stress-test 는 HPC 서버 부하테스트(cpu / stress-ng)를 AWX 로 실행하고 1분 로드로 실행 여부를 감시한다.
//
//	stress-test -user <u> -hosts <file> -steps <ab> [-retry N] [-er file] [-conf file] [-batch]
//
// 종료코드: 0 전부 실행, 1 미실행/실패 있음, 2 설정·입력 오류.
package main

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	stageCPU    = "cpu"
	stageStress = "stress"
)

// 출력 마커 (터미널일 때만 색)
var (
	markOK   = "[✔]"
	markFail = "[X]"
	markWarn = "[!]"
	markInfo = "[i]"
)

func isTerminal(f *os.File) bool {
	fi, err := f.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}

func setColor() {
	markOK = "\033[32m[✔]\033[0m"
	markFail = "\033[31m[X]\033[0m"
	markWarn = "\033[33m[!]\033[0m"
}

// app 은 한 번의 실행 상태다.
type app struct {
	cfg         *Config
	client      *awxClient
	user        string
	workDir     string
	gossh       *gosshRunner
	interactive bool
	retry       int
	targets     []*target
	out         io.Writer
	in          *bufio.Reader
}

func (a *app) logf(format string, args ...interface{}) {
	fmt.Fprintf(a.out, format+"\n", args...)
}

// pollUnit 은 poll_interval 의 단위다 (테스트에서 줄인다).
var pollUnit = time.Second

func (a *app) pollInterval() time.Duration {
	return time.Duration(a.cfg.PollInterval) * pollUnit
}

func usage(fs *flag.FlagSet) func() {
	return func() {
		fmt.Fprintln(os.Stderr, "사용법: stress-test -user <사용자> -hosts <입력파일> -steps <ab> [-retry N] [-er <장애목록>] [-conf <설정파일>] [-batch]")
		fmt.Fprintln(os.Stderr, "  -steps : a=cpu, b=stress (적은 순서대로 실행, 예: a, b, ab, ba)")
		fmt.Fprintln(os.Stderr, "  종료코드: 0 전부 실행, 1 미실행/실패 있음, 2 설정·입력 오류")
		fs.PrintDefaults()
	}
}

func main() {
	os.Exit(run(os.Args[1:]))
}

func fail2(format string, a ...interface{}) int {
	fmt.Fprintf(os.Stderr, "오류: "+format+"\n", a...)
	return 2
}

func run(args []string) int {
	fs := flag.NewFlagSet("stress-test", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	userFlag := fs.String("user", "", "사용자 식별자 (없으면 STRESS_USER → $USER)")
	hostsFlag := fs.String("hosts", "", "입력 파일 (hostname 한 줄 또는 12/13필드 설치양식)")
	stepsFlag := fs.String("steps", "", "실행할 단계: a=cpu, b=stress (예: ab, ba)")
	retryFlag := fs.Int("retry", 0, "비대화형에서 미실행 호스트 자동 재실행 횟수")
	erFlag := fs.String("er", "", "장애 목록 파일 (줄마다 \"hostname 장애사유\") (기본: 현재 경로의 "+defaultErrorList+", 없으면 생략)")
	confFlag := fs.String("conf", "", "설정 파일 경로 (기본: ./conf → ~/.stress-test → <바이너리 위치>/conf 의 ${user}_setting.conf)")
	batchFlag := fs.Bool("batch", false, "비대화형 실행 (프롬프트 없음, 감시는 한 줄 요약)")
	fs.Usage = usage(fs)
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() > 0 {
		return fail2("알 수 없는 인자: %s", strings.Join(fs.Args(), " "))
	}
	if *retryFlag < 0 {
		return fail2("-retry 는 0 이상이어야 합니다")
	}
	steps, err := parseSteps(*stepsFlag)
	if err != nil {
		return fail2("%v", err)
	}
	if *hostsFlag == "" {
		return fail2("-hosts 로 입력 파일을 지정하세요")
	}
	user := resolveUser(*userFlag)
	if user == "" || !userRe.MatchString(user) {
		return fail2("사용자를 정할 수 없거나 형식이 올바르지 않습니다 (%q). -user 또는 STRESS_USER 를 확인하세요", user)
	}
	confPath, err := resolveConfPath(*confFlag, user)
	if err != nil {
		return fail2("%v", err)
	}
	cfg, err := loadConfig(confPath)
	if err != nil {
		return fail2("%v", err)
	}
	if err := cfg.validate(*stepsFlag); err != nil {
		return fail2("%v", err)
	}

	raw, err := os.ReadFile(*hostsFlag)
	if err != nil {
		return fail2("입력 파일을 읽을 수 없습니다: %v", err)
	}
	targets, errs := parseHosts(string(raw))
	if len(errs) > 0 {
		return fail2("입력 파일 %s 형식 오류:\n  %s", *hostsFlag, strings.Join(errs, "\n  "))
	}
	if len(targets) == 0 {
		return fail2("입력 파일 %s 에 호스트가 없습니다", *hostsFlag)
	}

	notes := map[string]string{}
	if *erFlag != "" {
		b, err := os.ReadFile(*erFlag)
		if err != nil {
			return fail2("장애 목록 파일을 읽을 수 없습니다: %v", err)
		}
		notes = parseErrorList(string(b))
	} else if b, err := os.ReadFile(defaultErrorList); err == nil {
		notes = parseErrorList(string(b))
	}
	for _, t := range targets {
		if n := notes[strings.ToLower(t.Host)]; n != "" {
			t.Notes = append(t.Notes, n)
		}
	}

	gosshPath, err := resolveGossh(cfg.Gossh)
	if err != nil {
		return fail2("%v", err)
	}
	if err := os.MkdirAll(cfg.OutputDir, 0o755); err != nil {
		return fail2("출력 폴더를 만들 수 없습니다: %v", err)
	}
	workDir, err := os.MkdirTemp(cfg.OutputDir, ".stress_work_")
	if err != nil {
		return fail2("작업 폴더를 만들 수 없습니다: %v", err)
	}
	defer os.RemoveAll(workDir)
	logPath := filepath.Join(cfg.OutputDir, user+"_stress_gossh.log")
	logf, err := os.Create(logPath)
	if err != nil {
		return fail2("gossh 로그 파일을 만들 수 없습니다: %v", err)
	}
	defer logf.Close()

	interactive := !*batchFlag && isTerminal(os.Stdin) && isTerminal(os.Stdout)
	if isTerminal(os.Stdout) {
		setColor()
	}
	a := &app{
		cfg:         cfg,
		client:      newAWXClient(cfg.AWXURL, cfg.Username, cfg.Password, cfg.InsecureTLS, 10*time.Second),
		user:        user,
		workDir:     workDir,
		gossh:       &gosshRunner{path: gosshPath, workDir: workDir, log: logf},
		interactive: interactive,
		retry:       *retryFlag,
		targets:     targets,
		out:         os.Stdout,
	}
	if interactive {
		a.in = bufio.NewReader(os.Stdin)
	}
	a.logf("%s user=%s  설정=%s  대상 %d대  단계=%s  gossh 로그=%s", markInfo, user, confPath, len(targets), strings.Join(steps, ","), logPath)
	appendHistory(cfg, fmt.Sprintf("user=%s action=start hosts=%s steps=%s count=%d", user, *hostsFlag, *stepsFlag, len(targets)))

	if err := a.checkVendors(); err != nil {
		return fail2("%v", err)
	}
	a.initResults(steps)
	if err := a.checkCPUTemplates(steps); err != nil {
		return fail2("%v", err)
	}
	for _, s := range steps {
		a.runStage(s)
	}
	return a.finish(steps)
}

// checkVendors 는 gossh 로 vendor_id 를 읽어 접속 여부와 CPU 벤더를 정한다.
func (a *app) checkVendors() error {
	var hosts []string
	for _, t := range a.targets {
		if t.DNSFail {
			a.logf("%s DNS 조회 실패로 제외: %s", markWarn, t.Host)
			continue
		}
		hosts = append(hosts, t.Host)
	}
	if len(hosts) == 0 {
		return nil
	}
	a.logf("%s 접속체크 %d대 (%s)", markInfo, len(hosts), vendorCmd)
	out, off, err := a.gossh.run(hosts, vendorCmd)
	if err != nil {
		return err
	}
	vendors := parseVendors(out, off, hosts)
	cnt := map[string]int{}
	for _, t := range a.targets {
		if t.DNSFail {
			continue
		}
		t.Vendor = vendors[t.Host]
		if t.Vendor == "" {
			t.Offline = true
			cnt["접속불가"]++
		} else {
			cnt[t.Vendor]++
		}
	}
	a.logf("%s 접속체크 결과: Intel %d, AMD %d, unknown %d, 접속불가 %d", markInfo, cnt[vendorIntel], cnt[vendorAMD], cnt[vendorUnknown], cnt["접속불가"])
	return nil
}

// initResults 는 단계별 시작 값을 채운다.
func (a *app) initResults(steps []string) {
	for _, t := range a.targets {
		for _, s := range steps {
			switch {
			case t.DNSFail:
				t.Res[s] = resDNSFail
			case t.Offline:
				t.Res[s] = resOffline
			case s == stageCPU && t.Vendor == vendorUnknown:
				t.Res[s] = resNone
			default:
				t.Res[s] = resNotRun
			}
		}
	}
}

// checkCPUTemplates 는 cpu 단계에서 실제로 대상이 있는 벤더의 템플릿이 설정돼 있는지 확인한다.
func (a *app) checkCPUTemplates(steps []string) error {
	if !contains(steps, stageCPU) {
		return nil
	}
	for _, t := range a.targets {
		if t.Res[stageCPU] != resNotRun {
			continue
		}
		if t.Vendor == vendorIntel && a.cfg.CPUIntel.Template == "" {
			return fmt.Errorf("Intel 대상이 있는데 설정 파일(%s)의 cpu_intel_template 가 비어 있습니다", a.cfg.SourcePath)
		}
		if t.Vendor == vendorAMD && a.cfg.CPUAMD.Template == "" {
			return fmt.Errorf("AMD 대상이 있는데 설정 파일(%s)의 cpu_amd_template 가 비어 있습니다", a.cfg.SourcePath)
		}
	}
	return nil
}

func contains(l []string, s string) bool {
	for _, v := range l {
		if v == s {
			return true
		}
	}
	return false
}

// batch 는 yaml 하나·템플릿 하나로 실행하는 묶음이다.
type batch struct {
	name string // stress / intel / amd (yaml 이름에 쓰임)
	tc   TemplateConf
	list []*target
}

// split 은 단계별로 묶음을 나눈다. cpu 는 Intel → AMD 순, 없는 벤더는 건너뜀.
func (a *app) split(stage string, list []*target) []batch {
	if stage == stageStress {
		return []batch{{name: "stress", tc: a.cfg.Stress, list: list}}
	}
	var intel, amd []*target
	for _, t := range list {
		if t.Vendor == vendorIntel {
			intel = append(intel, t)
		} else if t.Vendor == vendorAMD {
			amd = append(amd, t)
		}
	}
	var out []batch
	if len(intel) > 0 {
		out = append(out, batch{name: "intel", tc: a.cfg.CPUIntel, list: intel})
	}
	if len(amd) > 0 {
		out = append(out, batch{name: "amd", tc: a.cfg.CPUAMD, list: amd})
	}
	return out
}

// yamlName 은 등록용 yaml 이름이다. 재실행은 ${user}_retryN_<묶음>.yaml.
func yamlName(user, name string, round int) string {
	if round == 0 {
		return fmt.Sprintf("%s_%s.yaml", user, name)
	}
	return fmt.Sprintf("%s_retry%d_%s.yaml", user, round, name)
}

func stageLabel(stage string) string {
	if stage == stageStress {
		return "stress-ng"
	}
	return "cpu"
}

// runStage 는 한 단계를 실행한다: 묶음별 등록·launch → 감시 → (필요하면) 미실행 재실행.
func (a *app) runStage(stage string) {
	var pending []*target
	for _, t := range a.targets {
		if t.Res[stage] == resNotRun {
			pending = append(pending, t)
		}
	}
	a.logf("\n===== %s 단계: 대상 %d대 =====", stageLabel(stage), len(pending))
	for round := 0; len(pending) > 0; round++ {
		var launched []*target
		for _, b := range a.split(stage, pending) {
			if a.launchBatch(stage, b, round) {
				launched = append(launched, b.list...)
			}
		}
		if len(launched) == 0 {
			return
		}
		title := stageLabel(stage)
		if round > 0 {
			title += fmt.Sprintf(" 재실행%d", round)
		}
		remaining := monitorLoad(a.out, title, launched, a.checkLoads, a.cfg.MonitorSec, a.cfg.MonitorInterval, a.cfg.LoadThreshold, a.interactive)
		rem := map[*target]bool{}
		for _, t := range remaining {
			rem[t] = true
		}
		for _, t := range launched {
			if rem[t] {
				t.Res[stage] = resNotRun
			} else {
				t.Res[stage] = resDone
			}
		}
		a.logf("%s %s 감시 종료: 실행 %d대, 미실행 %d대", markInfo, title, len(launched)-len(remaining), len(remaining))
		appendHistory(a.cfg, fmt.Sprintf("user=%s action=monitor stage=%s round=%d done=%d notrun=%d", a.user, stage, round, len(launched)-len(remaining), len(remaining)))
		if len(remaining) == 0 || !a.askRetry(len(remaining), round) {
			return
		}
		pending = remaining
	}
}

// askRetry 는 대화형이면 묻고, 비대화형이면 -retry 횟수만큼 자동으로 재실행한다.
func (a *app) askRetry(n, round int) bool {
	if a.interactive {
		fmt.Fprintf(a.out, "미실행 %d대를 재실행할까요? (y/N): ", n)
		line, _ := a.in.ReadString('\n')
		ans := strings.ToLower(strings.TrimSpace(line))
		return ans == "y" || ans == "yes"
	}
	if round < a.retry {
		a.logf("%s 미실행 %d대 자동 재실행 (%d/%d)", markInfo, n, round+1, a.retry)
		return true
	}
	return false
}

// checkLoads 는 감시용 loadChecker 다.
func (a *app) checkLoads(hosts []string) (map[string]float64, error) {
	out, _, err := a.gossh.run(hosts, loadCmd)
	if err != nil {
		return nil, err
	}
	return parseLoads(out), nil
}

// launchBatch 는 묶음 하나를 등록·launch·poll 한다. 실패하면 묶음 전체를 실패로 표시하고 false.
func (a *app) launchBatch(stage string, b batch, round int) bool {
	failAll := func(note string, err error) bool {
		a.logf("%s %s: %v", markFail, note, err)
		appendHistory(a.cfg, fmt.Sprintf("user=%s action=launch stage=%s batch=%s round=%d status=fail error=%q", a.user, stage, b.name, round, note+": "+err.Error()))
		for _, t := range b.list {
			t.Res[stage] = resFail
			t.addNote(note)
		}
		return false
	}
	label := fmt.Sprintf("%s(%s)", stageLabel(stage), b.name)
	a.logf("%s %s 묶음 %d대 등록 시작", markInfo, label, len(b.list))
	yml, err := a.register(yamlName(a.user, b.name, round), b.list)
	if err != nil {
		return failAll(label+" 등록 실패", err)
	}
	tmpl, err := a.client.resolveTemplate(b.tc.Template)
	if err != nil {
		return failAll(label+" 템플릿 조회 실패", err)
	}
	vars := map[string]interface{}{}
	kv, _ := parseKeyValues(b.tc.ExtraVars) // validate 에서 이미 검사
	for k, v := range kv {
		vars[k] = v
	}
	hosts := make([]string, len(b.list))
	for i, t := range b.list {
		hosts[i] = t.Host
	}
	vars[b.tc.HostnameKey] = strings.Join(hosts, "\n")
	job, err := a.client.launch(tmpl.ID, vars)
	if err != nil {
		return failAll(label+" launch 실패", err)
	}
	a.logf("%s 템플릿 %s(%d) 실행: job %d", markInfo, tmpl.Name, tmpl.ID, job)
	st, err := a.client.pollJob(job, a.pollInterval(), a.logf)
	if err != nil {
		return failAll(fmt.Sprintf("%s 실패(job %d)", label, job), err)
	}
	if st != "successful" {
		a.logf("    --- stdout 마지막 20줄 (job %d) ---\n%s", job, a.client.stdoutTail(job, 20))
		return failAll(fmt.Sprintf("%s 실패(job %d)", label, job), fmt.Errorf("status=%s", st))
	}
	appendHistory(a.cfg, fmt.Sprintf("user=%s action=launch stage=%s batch=%s round=%d yml=%s job=%d status=successful", a.user, stage, b.name, round, yml, job))
	return true
}

func vendorLabel(t *target) string {
	if t.Vendor == "" {
		return resNone
	}
	return t.Vendor
}

// resultLines 는 결과표(| 구분, 첫 줄 제목)를 입력 순서대로 만든다.
func resultLines(targets []*target) []string {
	out := []string{"hostname|CPU벤더|cpu|stress-ng|비고"}
	cell := func(t *target, s string) string {
		if v, ok := t.Res[s]; ok {
			return v
		}
		return resNone
	}
	for _, t := range targets {
		out = append(out, strings.Join([]string{t.Host, vendorLabel(t), cell(t, stageCPU), cell(t, stageStress), strings.Join(t.Notes, ", ")}, "|"))
	}
	return out
}

// hasProblem 은 미실행·실패가 하나라도 있는지 본다.
func hasProblem(targets []*target) bool {
	for _, t := range targets {
		for _, v := range t.Res {
			if v == resNotRun || v == resFail {
				return true
			}
		}
	}
	return false
}

// finish 는 결과표를 출력·저장하고 종료코드를 돌려준다.
func (a *app) finish(steps []string) int {
	lines := resultLines(a.targets)
	text := strings.Join(lines, "\n") + "\n"
	a.logf("\n===== 결과 =====")
	fmt.Fprint(a.out, text)
	path := filepath.Join(a.cfg.OutputDir, a.user+"_stress_result.txt")
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		a.logf("%s 결과 파일 저장 실패: %v", markFail, err)
	} else {
		a.logf("%s 결과 저장: %s", markOK, path)
	}
	code := 0
	if hasProblem(a.targets) {
		code = 1
	}
	appendHistory(a.cfg, fmt.Sprintf("user=%s action=finish steps=%s exit=%d result=%s", a.user, strings.Join(steps, ","), code, path))
	return code
}
