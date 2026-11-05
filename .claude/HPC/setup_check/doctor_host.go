package main

// new_setup -doctor 의 호스트 단건 점검과 진입점.
// 임시 폴더(os.TempDir)에 호스트 한 줄짜리 목록을 만들어 gossh 로 읽기 전용 명령만 보내고, 끝나면 폴더를 지운다.

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// resDesc 는 gossh 결과 파일 접미사별 뜻.
var resDesc = map[string]string{
	"_res_off":    "접속 불가(접속 타임아웃·DNS 실패·세션 실패)",
	"_res_refsed": "접속 거부(포트 닫힘)",
	"_res_cancel": "취소됨",
	"_os_install": "OS설치중 후보(-pm 이 설치 중 표식을 감지)",
	"_nosvrauto":  "/user/svrauto 마운트 없음(-pm 점검)",
}

var cshErrRe = regexp.MustCompile(`Illegal variable name|Unmatched|Missing \}|Badly placed|Undefined variable|Event not found|Too many arguments|Ambiguous output redirect|Invalid null command|Variable syntax`)

// docExtraCmd 는 단건 점검 전용 읽기 명령: 부팅 시각·uptime·CPU 로그 파일 ls -l. csh 에서도 안전한 조각만 쓴다.
func docExtraCmd(c config) string {
	s := "grep btime /proc/stat; "
	for _, p := range []string{c.CPUIntelLog, c.CPUAMDLog} {
		if p == "" {
			continue
		}
		f := logFileExpr(p)
		s += "[ -f " + f + " ] && ls -l " + f + "; [ -f " + f + " ] || echo " + mkMissing + " " + shQuote(p) + "; "
	}
	// ★ 명령이 작은따옴표로 끝나면 csh 로그인 셸에서 Unmatched 오류가 나므로(랩 .60 에서 확인) 따옴표 없는 uptime 으로 끝낸다.
	return s + "uptime"
}

// valueLines 는 gossh 출력의 "host: 내용" 줄에서 내용만 뽑는다(호스트 한 대짜리 출력용).
func valueLines(out string) []string {
	var l []string
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimRight(line, "\r")
		if i := strings.Index(line, ": "); i > 0 {
			l = append(l, line[i+2:])
		}
	}
	return l
}

func shortCmd(s string) string { return "'" + s + "'" }

func docHost(r *docReport, a doctorArgs, d doctorDeps, env *docEnvInfo) {
	r.head("3. 호스트 단건 점검: " + a.host)
	conf := env.conf
	if env.gossh == "" {
		r.bad("위 '1. 환경 점검' 의 gossh 문제를 먼저 해결하세요", "gossh 를 쓸 수 없어 호스트 점검을 할 수 없습니다")
		return
	}
	infoCmd, retryCmd, extraCmd := buildInfoCmd(conf), buildRemoteCmd(conf.DiskPatterns, conf.ErrPatterns), docExtraCmd(conf)
	for _, c := range []string{infoCmd, retryCmd, extraCmd} {
		if isDangerousCommand(c) {
			r.bad("위 '1. 환경 점검' 의 위험어 항목을 먼저 고치세요", "원격 명령에 gossh 위험어가 있어 보낼 수 없습니다: %s", c)
			return
		}
	}
	dir, err := os.MkdirTemp("", "new_setup_doctor")
	if err != nil {
		r.bad("임시 폴더(TMPDIR)에 쓸 수 있는지 확인하세요", "임시 폴더를 만들 수 없습니다: %v", err)
		return
	}
	defer os.RemoveAll(dir)
	hf := filepath.Join(dir, "host.txt")
	if err := os.WriteFile(hf, []byte(a.host+"\n"), 0o644); err != nil {
		r.bad("", "임시 호스트 파일을 쓸 수 없습니다: %v", err)
		return
	}
	r.info("gossh %s 로 읽기 전용 명령만 보냅니다(임시 호스트 파일 사용, 끝나면 삭제). 시간 제한 GOSSH_TIMEOUT=%s", env.gossh, conf.GosshTimeout)

	run := func(title string, args []string, cmd string) (string, bool) {
		r.info("%s\ngossh %s", title, strings.Join(args[:len(args)-1], " ")+" "+shortCmd(cmd))
		out, all, err := d.gossh(env.gossh, args, conf.GosshTimeout)
		if err != nil {
			r.bad("GOSSH_TIMEOUT 을 늘리거나 gossh 를 직접 실행해 원인을 확인하세요", "gossh 실행 실패/시간초과: %v", err)
			return "", false
		}
		r.info("gossh 원본 출력(stdout+stderr):")
		r.block(strings.Split(strings.TrimRight(all, "\r\n"), "\n"), 60)
		if m := cshErrRe.FindString(all); m != "" {
			r.bad("대상 장비의 root 로그인 셸이 csh/tcsh 일 때 깨지는 구문입니다. 설정 문구에 !, 변수, 중괄호가 없는지 보고, 문제 구문을 개발자에게 알려 주세요",
				"원격 셸 오류로 보이는 출력이 있습니다: %q", m)
		}
		return out, true
	}

	// --- 1차
	out1, ok := run("[1차] 정보수집 + 접속 확인(-pm), 실제 판정과 같은 명령", []string{"-w", hf, "-pm", "-script", infoCmd}, infoCmd)
	if !ok {
		return
	}
	off := map[string]bool{}
	for _, s := range []string{"_res_off", "_res_refsed", "_res_cancel"} {
		for h := range readHostSet(hf + s) {
			off[h] = true
		}
	}
	cand := readHostSet(hf + "_os_install")
	var present []string
	for _, s := range resSuffixes {
		if b, err := os.ReadFile(hf + s); err == nil && strings.TrimSpace(string(b)) != "" {
			present = append(present, s+" = "+resDesc[s])
		}
	}
	if len(present) > 0 {
		r.info("gossh 결과 파일(이 호스트가 들어간 것):\n%s", strings.Join(present, "\n"))
	} else {
		r.info("gossh 결과 파일: 없음(_res_off 등에 들어가지 않음 = 접속됨)")
	}
	if len(off) > 0 {
		r.bad("1) `ping "+a.host+"` 와 이름 풀이(`getent hosts "+a.host+"`) 확인\n2) 22 포트: `ssh -o ConnectTimeout=5 root@"+a.host+" true` 로 직접 접속\n3) 방화벽·sshd 상태, root 키 인증(gossh -i/-u 옵션)·호스트 키 확인\n4) _res_refsed 면 sshd 가 안 떠 있거나 포트 차단, _res_off 면 응답 없음(전원·망·DNS)",
			"접속불가: gossh 가 이 호스트에 접속하지 못했습니다(상태 접속불가, 이후 열은 모두 '-')")
		r.block([]string{header, formatRow(a.host, stUnreach, "-", nil, "")}, 0)
		return
	}
	values := valueLines(out1)
	if len(values) == 0 {
		r.warn("gossh 가 접속은 했으나 출력이 비었습니다. 위 원본 출력과 `<호스트파일>_new_setup.log` 를 보고, 대상 장비에서 `grep -m1 vendor_id /proc/cpuinfo` 가 도는지 확인하세요", "1차 출력에 이 호스트의 줄이 없습니다")
	}
	info := parseInfoOutput(out1)
	h := info[a.host]
	if h == nil && len(info) == 1 {
		for _, v := range info {
			h = v
		}
	}
	if h == nil {
		h = &hostInfo{}
	}

	// --- 2차
	retryOut, ok2 := run("[2차] anaconda 로그 확인(실제 판정은 1차에서 설치중 후보(_os_install)일 때만 사용)", []string{"-w", hf, "-script", retryCmd}, retryCmd)
	retry := map[string]map[string]bool{}
	if ok2 {
		retry = parseRetryOutput(retryOut)
		if len(retry) == 1 && retry[a.host] == nil {
			for _, v := range retry {
				retry = map[string]map[string]bool{a.host: v}
			}
		}
	}

	// --- 부팅 시각·uptime·CPU 로그
	extraOut, ok3 := run("[추가] 부팅 시각·uptime·CPU 테스트 로그 파일", []string{"-w", hf, "-script", extraCmd}, extraCmd)
	if ok3 {
		docExtra(r, valueLines(extraOut), d.now())
	}

	// --- 판정 설명
	status := classify([]string{a.host}, off, cand, retry)[0]
	r.head("호스트 판정 근거")
	switch {
	case cand[a.host]:
		mk := markerList(retry[a.host])
		r.info("상태 = %s\n1차에서 _os_install 에 들어가 설치중 후보 → 2차 표식: %s\n우선순위 NS_DISK(디스크미인식) > NS_ERR(설치정지) > 그 외 OS설치중(표식이 없으면 OS설치중)", status, mk)
		r.warn("설치가 끝났는지(anaconda 종료), /tmp/anaconda.log 와 DISK_PATTERN·ERROR_PATTERN 문구를 장비에서 확인하세요", "이 호스트는 OS 설치중 계열(%s)로 판정됩니다", status)
	default:
		r.ok("상태 = %s — 접속됨, 설치중 후보 아님(2차 결과는 판정에 쓰이지 않음)", status)
	}
	if status != stDone {
		r.block([]string{header, formatRow(a.host, status, "-", nil, "")}, 0)
		return
	}

	checkVal := docCheckExplain(r, a, d, env, status)
	r.info("%s", explainCPU(h, conf))
	r.info("stress-ng = %s — /var/log 아래 messages*(*.gz 제외)에서 stress-ng 문자열 %s(NS_STRESS). 있으면 O, 없으면 NOTRUN(아직 안 돌림)",
		h.stressResult(), map[bool]string{true: "발견", false: "없음"}[h.stress])
	r.info("H/T = %s — lscpu 'Thread(s) per core' = %s (2 이상 ON, 1 OFF, 못 읽으면 -)", h.htResult(), orDash(h.tpc))
	r.info("OS = %s — uname -r 커널 버전", h.osResult())
	if h.vendor == "" {
		r.warn("대상 장비에서 `grep -m1 vendor_id /proc/cpuinfo` 를 직접 실행해 보세요(가상머신·셸 문제일 수 있음)", "vendor_id 를 읽지 못해 CPU 열이 X 로 나옵니다")
	}
	r.info("이 호스트의 결과 한 줄(실제 출력 형식):")
	r.block([]string{header, formatRow(a.host, status, checkVal, h, "")}, 0)
}

func orDash(n int) string {
	if n <= 0 {
		return "-"
	}
	return strconv.Itoa(n)
}

func markerList(m map[string]bool) string {
	var l []string
	for _, k := range []string{mkNoLog, mkDisk, mkErr, mkDone} {
		if m[k] {
			l = append(l, k)
		}
	}
	if len(l) == 0 {
		return "(없음 — 2차 응답 없음)"
	}
	return strings.Join(l, ", ")
}

// docExtra 는 부팅 시각·uptime·로그 ls 출력을 설명한다.
func docExtra(r *docReport, vals []string, now time.Time) {
	var logs []string
	for _, v := range vals {
		f := strings.Fields(v)
		switch {
		case len(f) == 2 && f[0] == "btime":
			if n, err := strconv.ParseInt(f[1], 10, 64); err == nil {
				bt := time.Unix(n, 0)
				r.info("부팅 시각(btime %d) = %s (%s) — CPU 감시는 이 값이 바뀌면 '실행도중 재부팅됨' 으로 봅니다", n, tm(bt), agoText(now, bt))
				if now.Sub(bt) < 10*time.Minute {
					r.info("방금 부팅한 장비입니다(10분 이내)")
				}
			}
		case strings.Contains(v, " up ") || strings.Contains(v, "load average"):
			r.info("uptime: %s", strings.TrimSpace(v))
		case len(f) > 0 && f[0] == mkMissing:
			logs = append(logs, "없음: "+strings.TrimSpace(strings.TrimPrefix(v, mkMissing)))
		case len(f) >= 9:
			logs = append(logs, v)
		}
	}
	if len(logs) > 0 {
		r.info("CPU 테스트 로그 파일(없으면 해당 벤더의 CPU 열이 NOTRUN):\n%s", strings.Join(logs, "\n"))
	}
}

// explainCPU 는 CPU테스트 열 값과 근거를 설명한다.
func explainCPU(h *hostInfo, conf config) string {
	var b strings.Builder
	fmt.Fprintf(&b, "CPU테스트 = %s — ", h.cpuResult())
	yn := func(v bool) string { return map[bool]string{true: "있음", false: "없음"}[v] }
	switch h.vendor {
	case "GenuineIntel":
		fmt.Fprintf(&b, "벤더 GenuineIntel → Intel 로그 %s 에서 문구 %q %s\n", conf.CPUIntelLog, conf.CPUIntelOK, lineRule(conf.CPUIntelLine, conf.CPUIntelOK))
		fmt.Fprintf(&b, "표식 NS_CPU_INTEL %s, NS_CPU_INTEL_NOLOG(로그 파일 없음) %s", yn(h.cpuIntel), yn(h.noIntel))
	case "AuthenticAMD":
		fmt.Fprintf(&b, "벤더 AuthenticAMD → AMD 로그 %s 에서 문구 %q %s\n", conf.CPUAMDLog, conf.CPUAMDOK, lineRule(conf.CPUAMDLine, conf.CPUAMDOK))
		fmt.Fprintf(&b, "표식 NS_CPU_AMD %s, NS_CPU_AMD_NOLOG(로그 파일 없음) %s", yn(h.cpuAMD), yn(h.noAMD))
	default:
		b.WriteString("벤더를 알 수 없음(vendor_id 줄이 GenuineIntel/AuthenticAMD 가 아님) → X")
	}
	b.WriteString("\n판정: 해당 벤더 표식이 있으면 O, 로그 파일이 없으면 NOTRUN(부하테스트 아직 안 함), 그 외 X")
	return b.String()
}

func lineRule(line, ok string) string {
	if line == "" {
		return "가 로그 어디에든 있으면 통과"
	}
	return fmt.Sprintf("— %q 가 든 줄이 1개 이상이고 그 줄이 모두 %q 를 포함하면 통과", line, ok)
}

// docCheckExplain 은 설정체크 열을 설명하고(옵션이 있으면 -auto-check 를 실제로 돌려) 열 값을 돌려준다.
func docCheckExplain(r *docReport, a doctorArgs, d doctorDeps, env *docEnvInfo, status string) string {
	conf := env.conf
	if conf.OSCheckSh == "" {
		r.info("설정체크 = - — OS_CHECK_SH 가 비어 있어 설정체크를 하지 않습니다")
		return "-"
	}
	script := expandPath(conf.OSCheckSh)
	r.info("설정체크 열은 관리서버에서 이 명령으로 만듭니다(임시 폴더, targets.txt 에 이 호스트 한 줄):\nbash %s %s new_setup targets.txt\n결과 파일 check.res_new_setup 의 이 호스트 줄 중 첫 토큰이 OK/FAIL/INFO 같은 결과 줄만 세어 FAIL 이 있으면 X, 줄이 있고 FAIL 이 없으면 O, 결과 줄이 없으면 '%s'(체크 경로 미마운트 등). OS_CHECK_EXCLUDE 에 든 문구가 호스트명에 있으면 O",
		script, ccCheck, noOutput)
	if !a.check {
		r.info("실제 값을 보려면 -check 를 주세요(config_check.sh -auto-check 만 실행, 수정 모드는 실행하지 않음). 이번에는 열 값을 '-' 로 표시합니다")
		return "-"
	}
	if env.gossh == "" {
		return "-"
	}
	res, err := d.checkRun(conf, script, env.gossh, []string{a.host})
	if err != nil {
		r.bad("OS_CHECK_SH 점검 항목(환경 점검)과 CHECK_TIMEOUT 을 확인하세요", "설정체크(-auto-check) 실행 실패: %v", err)
		return "-"
	}
	v := res[a.host]
	if v == "" {
		v = "-"
	}
	switch v {
	case "O":
		r.ok("설정체크 = O (-auto-check 실행 결과)")
	case "X":
		r.warn("config_check.sh -auto-check 의 FAIL 항목을 장비에서 확인하세요(수정은 -run 의 설정수정 단계)", "설정체크 = X — config_check 결과에 FAIL 이 있습니다")
	default:
		r.warn("체크 스크립트가 쓰는 경로(/user/svrauto 등)가 이 관리서버와 장비에서 마운트돼 있는지 확인하세요", "설정체크 = %s — 이 호스트의 체크 결과 줄이 없습니다", v)
	}
	return v
}

// ---------------------------------------------------------------------------
// 진입
// ---------------------------------------------------------------------------

var hostNameRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

// validateDoctorArgs 는 인자 조합을 검사한다(오류면 종료코드 2).
func validateDoctorArgs(a doctorArgs, nargs int) error {
	switch {
	case nargs > 1:
		return fmt.Errorf("호스트파일은 하나만 줄 수 있습니다(옵션은 호스트파일 앞에 쓰세요)")
	case a.host != "" && a.hostFile != "":
		return fmt.Errorf("-host 와 호스트파일은 함께 쓸 수 없습니다")
	case a.host != "" && !hostNameRe.MatchString(a.host):
		return fmt.Errorf("-host 값이 호스트 이름 하나가 아닙니다: %q (공백·괄호·범위 표기 불가)", a.host)
	case a.check && a.host == "":
		return fmt.Errorf("-check 는 -host 와 함께만 쓸 수 있습니다")
	}
	return nil
}

// runDoctor 는 보고서 전체와 종료코드(0/1)를 만든다.
func runDoctor(a doctorArgs, d doctorDeps) (string, int) {
	r := &docReport{}
	fmt.Fprintf(&r.b, "new_setup 점검 보고서 (읽기 전용) — %s\n", tm(d.now()))
	var o []string
	if a.conf != "" {
		o = append(o, "-c "+a.conf)
	}
	if a.host != "" {
		o = append(o, "-host "+a.host)
	}
	if a.check {
		o = append(o, "-check")
	}
	if a.hostFile != "" {
		o = append(o, a.hostFile)
	}
	fmt.Fprintf(&r.b, "점검 대상: %s\n범례: [O] 정상  [!] 주의  [X] 오류  [i] 참고\n", orEmpty(strings.Join(o, " ")))
	if a.user == "" && a.hostFile != "" {
		dir := filepath.Join(d.cwd, filepath.Base(strings.TrimPrefix(a.hostFile, "^"))+"_run")
		if st, err := loadState(dir); err == nil {
			a.user = st.User
		}
	}
	env := docEnv(r, a, d)
	if a.hostFile != "" {
		docRun(r, a, d, env)
	}
	if a.host != "" {
		docHost(r, a, d, env)
	}
	return r.finish()
}

func hasDoctorFlag(args []string) bool {
	for _, a := range args {
		if a == "-doctor" || a == "--doctor" || a == "-doctor=true" || a == "--doctor=true" {
			return true
		}
	}
	return false
}

// doctorMain 은 `new_setup -doctor ...` 진입점.
func doctorMain(args []string) int {
	fs := flag.NewFlagSet("new_setup -doctor", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	fs.Bool("doctor", true, "읽기 전용 점검 보고서")
	confPath := fs.String("c", "", "설정 파일 경로 (기본: 현재 경로 → 실행 파일 옆 new_setup.conf)")
	host := fs.String("host", "", "호스트 한 대를 gossh 로 점검하고 열 판정 근거를 설명")
	user := fs.String("user", "", "stress-test conf(<user>_setting.conf)를 찾을 사용자(기본: 작업 상태의 user, STRESS_USER, USER)")
	check := fs.Bool("check", false, "-host 와 함께: config_check.sh -auto-check 도 실행(수정 모드는 실행 안 함)")
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "사용법: new_setup -doctor [-c <설정파일>] [-user <사용자>] [<호스트파일> | -host <호스트> [-check]]")
		fmt.Fprintln(os.Stderr, "  읽기 전용 점검. 설정·상태 파일·원격 호스트를 바꾸지 않습니다. 보고서는 ./new_setup_doctor_<시각>.txt 에도 저장됩니다.")
		fmt.Fprintln(os.Stderr, "  종료코드: 0 문제 없음, 1 문제 있음, 2 인자 오류")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return 0
		}
		return 2
	}
	a := doctorArgs{conf: *confPath, user: *user, host: *host, check: *check}
	if fs.NArg() >= 1 {
		a.hostFile = fs.Arg(0)
	}
	if err := validateDoctorArgs(a, fs.NArg()); err != nil {
		fmt.Fprintf(os.Stderr, "오류: %v\n", err)
		fs.Usage()
		return 2
	}
	d := realDoctorDeps()
	text, code := runDoctor(a, d)
	text = maskSecrets(text)
	fmt.Print(text)
	name := "new_setup_doctor_" + d.now().Format("20060102_150405") + ".txt"
	if err := os.WriteFile(filepath.Join(d.cwd, name), []byte(text), 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "경고: 보고서 파일을 저장하지 못했습니다: %v\n", err)
	} else {
		fmt.Fprintf(os.Stderr, "보고서 저장: %s\n", filepath.Join(d.cwd, name))
	}
	return code
}
