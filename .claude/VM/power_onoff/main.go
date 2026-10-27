// vm_power_onoff: vCenter 의 VM 을 안전장치(휴먼 에러 방지)를 거쳐 Power Off / Power On / 재부팅한다.
//
// 흐름: user 선택 → vCenter 선택 → 비밀번호 → 기능 선택 → 대상 조회·계획 → (VWP 확인) → 안전장치 5단계 → 실행 → 결과
// 안전장치(cross_check=on, 기본): 1 작업 확인 문구 → 2 확인자 번호 선택 → 3 확인자 승인 대기(cross_check) → 4 YeS(ls=대상 보기) → 5 카운트다운 → --start
// → 실행 → --finish done(끝까지 실행) / aborted(승인 뒤 No·Ctrl+C)
// 안전장치(cross_check=off): 1 작업 확인 문구 → 2 Enter → 3 VM 이름 입력 → 4 YeS(ls=대상 보기) → 5 카운트다운
// 설정 파일은 /etc/vcetner_pwon_off/pwonoff.conf, 대상은 allowed_dir/on_off_<user>.txt (VM 이름만).
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const version = "0.3.1"

var vcenterRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]*$`)

func fatal(format string, a ...any) {
	fmt.Fprintln(os.Stderr, Red("[오류] "+fmt.Sprintf(format, a...)))
	os.Exit(ExitFail)
}

func usage() {
	fmt.Fprintf(os.Stderr, `vm_power_onoff %s — VM Power Off / Power On(순차·일괄) / 재부팅 (안전장치 포함)

사용법:
  vm_power_onoff                       user 를 번호로 선택
  vm_power_onoff -u hong               user 지정
  vm_power_onoff -u hong -v 192.168.0.50   vCenter 를 직접 지정 (Enter = 이 user 의 이전 vCenter)
  vm_power_onoff -u hong -n            계획(대상·VWP 여부)까지만 확인, 전원 작업 없음
  vm_power_onoff -u hong -c kim        교차검증 확인자 지정 (cross_check=on(기본)일 때 2/5 단계 번호 선택을 건너뜀)
  vm_power_onoff --demo                vCenter 없이 샘플 3대로 안전장치만 체험 (기존 방식 1~5단계, 전원 작업 없음)

옵션:
  -u <user>   -v <vCenter>   -id <계정>(기본 설정값)   -c <checker>   -n   --demo   -h   --version

설정: %s  (allowed_dir 에서만 실행 가능)
대상: <allowed_dir>/on_off_<user>.txt  — 한 줄에 VM 이름 하나 (예: svr1ev01), '#' 주석
안전장치 4/5 단계에서 ls 를 입력하면 대상 VM 목록과 대수를 다시 보여 줍니다(취소되지 않음).
교차검증(cross_check)은 기본 on — 끄려면 conf 에 cross_check=off
입력 질문은 conf 의 input_timeout_sec(기본 600초 = 10분) 안에 답해야 합니다(넘기면 취소, 실행 중 질문은 No/중단으로 처리)
`, version, ConfPath)
}

func main() {
	var (
		fUser    = flag.String("u", "", "user")
		fVC      = flag.String("v", "", "vCenter")
		fID      = flag.String("id", "", "vCenter 계정")
		fChecker = flag.String("c", "", "교차검증 확인자 (cross_check=on 일 때만, 기본 on)")
		fPlan    = flag.Bool("n", false, "계획까지만")
		fDemo    = flag.Bool("demo", false, "안전장치 데모")
		fVersion = flag.Bool("version", false, "버전")
	)
	flag.Usage = usage
	flag.Parse()
	if *fVersion {
		fmt.Println("vm_power_onoff", version)
		return
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	in := NewLineReader(os.Stdin)
	out := os.Stdout

	if *fDemo {
		os.Exit(RunDemo(ctx, in, out))
	}
	os.Exit(run(ctx, in, out, *fUser, *fVC, *fID, *fChecker, *fPlan))
}

func run(ctx context.Context, in *LineReader, out io.Writer, optUser, optVC, optID, optChecker string, planOnly bool) int {
	// 0. 설정·허용 경로
	cfg, err := LoadConfig(ConfPath)
	if err != nil {
		fatal("%v", err)
	}
	if err := CheckAllowedDir(cfg); err != nil {
		fatal("%v", err)
	}
	if optID != "" {
		cfg.VCID = optID
	}
	// 교차검증(기본 on): -c 는 cross_check=on 일 때만 의미가 있다. 실제 전원 작업일 때만 실행 파일을 미리 확인한다
	// (설치되지 않았으면 vCenter 를 묻기 전에 바로 멈추고 cross_check=off 로 끄는 방법을 알려 준다).
	if optChecker != "" && !cfg.CrossCheck {
		fmt.Fprintln(out, Yellow("[경고] -c 는 conf 에 cross_check=on 일 때만 쓰입니다. 무시합니다."))
		optChecker = ""
	}
	useCross := crossCheckNeeded(cfg, planOnly)
	if useCross {
		if err := CheckCrossCheckBin(cfg.CrossCheckBin); err != nil {
			fatal("%v", err)
		}
	}

	// 1. user (이 단계부터 사용자 입력은 질문마다 input_timeout_sec 제한 — 넘기면 [취소] 입력 시간 N분 초과, exit 1)
	user, err := pickUser(ctx, in, out, cfg, optUser)
	if err != nil {
		return failInput(out, "", "", "", "user 선택", err) // user 를 모르므로 감사 로그 없음
	}
	if useCross { // 잘못된 -c 나 고를 확인자가 없는 conf 는 vCenter 접속 전에 바로 알린다
		if optChecker != "" {
			if err := ValidateChecker(cfg, user, optChecker); err != nil {
				fatal("%v", err)
			}
		} else if _, err := checkerCandidates(cfg, user); err != nil {
			fatal("%v", err)
		}
	}
	// 2. 대상 파일
	targets, err := LoadTargets(cfg.AllowedDir, user)
	if err != nil {
		fatal("%v", err)
	}
	fmt.Fprintf(out, "[INFO] 대상 파일: %s (%d대)\n", TargetFilePath(cfg.AllowedDir, user), len(targets))

	// 3. vCenter
	runDir := filepath.Join(cfg.AllowedDir, "run_"+user)
	vc, err := pickVCenter(ctx, in, out, cfg, runDir, user, optVC)
	if err != nil {
		return failInput(out, runDir, user, "", "vCenter 선택", err)
	}
	// 4. 비밀번호 (암호 파일 → 직접 입력). VC_PASSWORD/VC_PASS 환경변수는 쓰지 않는다.
	pass, err := GetPassword(cfg, vc, cfg.VCID, hiddenPrompt(ctx, in, cfg.InputTimeout))
	if errors.Is(err, ErrTimeout) {
		return failInput(out, runDir, user, vc, "비밀번호", &GuardError{Reason: inputTimeoutReason(cfg.InputTimeout)})
	}
	if err != nil || pass == "" {
		fatal("vCenter 비밀번호를 얻지 못했습니다: %v", err)
	}
	// 5. 기능
	act, err := pickAction(ctx, in, out, cfg.InputTimeout)
	if err != nil {
		return failInput(out, runDir, user, vc, "기능 선택", err)
	}

	audit := openAuditOrWarn(out, runDir, act)
	defer audit.Close()
	audit.Printf("시작 os계정=%s user=%s vCenter=%s 계정=%s 기능=%s 대상=%d대", osUser(), user, vc, cfg.VCID, act, len(targets))
	audit.Printf("대상: %s", strings.Join(targets, " "))

	// 6. 접속·조회
	fmt.Fprintf(out, "[INFO] vCenter %s 접속 중...\n", vc)
	cli, err := Connect(ctx, vc, cfg.VCID, pass)
	if err != nil {
		audit.Printf("접속 실패: %v", err)
		fatal("%v", err)
	}
	defer cli.Close(context.Background())
	pass = ""
	vms, err := cli.LookupVMs(ctx, targets)
	if err != nil {
		var le *LookupError
		if errors.As(err, &le) {
			printLookupError(out, le)
			audit.Printf("대상 조회 실패: 없음 %v 중복 %v", le.Missing, le.Duplicate)
			return ExitFail
		}
		fatal("%v", err)
	}
	if err := SaveLastVCenter(runDir, vc); err != nil {
		fmt.Fprintf(out, "%s\n", Yellow("[경고] 이전 vCenter 를 기록하지 못했습니다: "+err.Error()))
	}

	// 7. 계획표
	printPlan(out, act, vms)
	if planOnly {
		fmt.Fprintln(out, "[INFO] -n 지정: 계획만 확인하고 종료합니다. (전원 작업 없음)")
		return ExitOK
	}

	// 8. VWP
	if hits := FindVWP(vms); len(hits) > 0 {
		audit.Printf("VWP 해당 %d대/%d대", len(hits), len(vms))
		PrintVWPBanner(out, hits, len(vms), ColorEnabled())
		if err := ConfirmPhrase(ctx, in, out, fmt.Sprintf(" 담당자 확인이 완료되었다면 %s(%s)을 그대로 입력하세요.\n > ", Bold(VWPPhrase), VWPPhraseHint), VWPPhrase, cfg.InputTimeout); err != nil {
			audit.Printf("VWP 확인 취소: %v", err)
			return ExitFail
		}
		audit.Printf("VWP 담당자 확인 통과")
	}

	// 9. 안전장치 1~5단계 (cross_check=on 이면 2단계 확인자 선택, 3단계 확인자 승인 대기, 카운트다운 직후 --start)
	names := make([]string, len(vms))
	for i, v := range vms {
		names[i] = v.Name
	}
	lsCount := 0
	gopts := GuardOpts{ShowTargets: func(w io.Writer) { printTargetsByBM(w, vms) }, OnList: func() { lsCount++ }, StepTimeout: cfg.InputTimeout}
	var gate *CrossCheckGate
	if useCross {
		gate = &CrossCheckGate{Cfg: cfg, CC: newCrossChecker(cfg.CrossCheckBin), Out: out, ErrOut: os.Stderr, Audit: audit,
			User: user, OptChecker: optChecker, Targets: names, InputTimeout: cfg.InputTimeout}
		gate.Hook(&gopts)
	}
	err = RunGuard(ctx, in, out, names, gopts)
	audit.Printf("안전장치 4단계 ls(대상 보기) %d회", lsCount)
	if err != nil {
		audit.Printf("안전장치 취소: %v", err)
		if gate != nil {
			gate.Abort() // 승인 뒤 4/5 No·Ctrl+C, 카운트다운 중 Ctrl+C → 승인/작업중단 (승인 전·--start 거부면 보내지 않음)
		}
		return ExitFail
	}
	if useCross {
		audit.Printf("안전장치 1~5단계 통과 (교차검증 방식)")
	} else {
		audit.Printf("안전장치 1~5단계 통과")
	}

	// 10. 실행
	r := &Runner{Cfg: cfg, Ops: NewGovmomiOps(cli), Log: NewRunLog(out, audit), Ask: askLine(in, out, cfg.InputTimeout)}
	r.WaitBoot = NewBootChecker(cfg, cli.Refresh, func(vm, f string, a ...any) { r.Log.Logf(vm, f, a...) }).WaitBoot
	t0 := time.Now()
	results := executeRun(ctx, r, act, vms, gate)

	// 11. 결과
	return reportResults(ctx, out, audit, act, results, t0)
}

// reportResults 는 11단계: 결과표·감사 로그를 남기고 종료 코드를 정한다(교차검증 결과 보고 성공 여부와는 무관).
func reportResults(ctx context.Context, out io.Writer, audit *Audit, act Action, results []Result, t0 time.Time) int {
	failed := PrintSummary(out, results, ColorEnabled())
	for _, x := range results {
		audit.Printf("결과 %s %s %s %s %s", x.VM, x.Op, x.Status, fmtElapsed(x.Elapsed), x.Note)
	}
	audit.Printf("종료 실패=%d 소요=%s", failed, fmtElapsed(time.Since(t0)))
	if ctx.Err() != nil {
		fmt.Fprintln(out, Yellow("[경고] Ctrl+C 로 중단했습니다. 이미 vCenter 에 요청한 작업은 vCenter 에서 계속 진행될 수 있습니다."))
		return ExitFail
	}
	if audit != nil && audit.Path() != "" {
		fmt.Fprintf(out, "[INFO] 감사 로그: %s\n", audit.Path())
	}
	if failed > 0 {
		return ExitFail
	}
	fmt.Fprintln(out, Green("[완료] "+act.String()))
	return ExitOK
}

// executeRun 은 10단계 실행: Runner 를 돌리고, 교차검증(gate != nil)이면 곧바로 결과를 데몬에 알린다 —
// 끝까지 실행되면 done(VM 별 실패가 있어도), 실행 중 Ctrl+C·종료 신호(ctx 취소)면 aborted. 보고 실패는 종료 코드에 영향 없음.
func executeRun(ctx context.Context, r *Runner, act Action, vms []*VMInfo, gate *CrossCheckGate) []Result {
	results := r.Run(ctx, act, vms)
	if gate != nil {
		gate.ReportRun(ctx)
	}
	return results
}

// 선택 메뉴 ---------------------------------------------------------------

// readChoice 는 메뉴 번호 한 줄을 읽는다. d(> 0) 안에 입력이 없으면 *GuardError("입력 시간 N분 초과").
func readChoice(ctx context.Context, in *LineReader, prompt string, d time.Duration) (string, error) {
	fmt.Print(prompt)
	s, err := in.ReadLineLimit(ctx, d)
	if err != nil {
		if ctx.Err() != nil {
			return "", errors.New("Ctrl+C 로 중단")
		}
		if errors.Is(err, ErrTimeout) {
			return "", &GuardError{Reason: inputTimeoutReason(d)}
		}
		return "", fmt.Errorf("입력을 읽지 못했습니다: %v", err)
	}
	return strings.TrimSpace(s), nil
}

// failInput 은 vCenter 접속 전 입력 단계(user·vCenter 선택, 비밀번호, 기능 선택)의 실패를 처리한다.
// 입력 시간 초과(*GuardError)면 "[취소] 입력 시간 N분 초과" 를 출력하고, user 를 알면(runDir != "") 감사 로그
// (run_<user>/<시각>_unknown.log — 기능을 고르기 전이라 기능 이름 대신 unknown)에 사유를 남긴 뒤 ExitFail.
// 그 밖의 오류는 기존처럼 "[오류] …" 로 바로 종료한다.
func failInput(out io.Writer, runDir, user, vc, stage string, err error) int {
	var ge *GuardError
	if !errors.As(err, &ge) {
		fatal("%v", err)
	}
	fmt.Fprintln(out) // 프롬프트 줄을 끝낸다
	fmt.Fprintln(out, Red("[취소] "+ge.Reason))
	if runDir != "" {
		if a := openAuditOrWarn(out, runDir, Action(0)); a != nil {
			a.Printf("시작 os계정=%s user=%s vCenter=%s 기능=(선택 전)", osUser(), user, vc)
			a.Printf("입력 취소 단계=%s 사유=%s", stage, ge.Reason)
			a.Close()
		}
	}
	return ExitFail
}

// hiddenPrompt 는 에코를 끄고 비밀번호를 공용 LineReader 로 읽는다(d > 0 이면 그 안에 입력이 없을 때 ErrTimeout).
// NewLineReader 의 고루틴이 stdin 을 계속 읽고 있으므로 ReadHidden(os.Stdin 직접 읽기)을 쓰면
// 그 줄을 리더가 가로채 다음 메뉴 입력으로 흘러 들어간다 — 반드시 같은 리더로 읽는다.
func hiddenPrompt(ctx context.Context, in *LineReader, d time.Duration) func(string) (string, error) {
	return func(msg string) (string, error) {
		fmt.Fprint(os.Stderr, msg)
		if isTerminal(os.Stdin) {
			if saved, err := sttyOut("-g"); err == nil && saved != "" && sttyRun("-echo") == nil {
				defer func() { _ = sttyRun(saved); fmt.Fprintln(os.Stderr) }()
			}
		}
		return in.ReadLineLimit(ctx, d)
	}
}

// askLine 은 실행 중(Runner) 질문용. d(> 0) 안에 입력이 없으면 프롬프트 줄을 끝내고 ErrTimeout(Runner 가 No/중단으로 처리).
func askLine(in *LineReader, out io.Writer, d time.Duration) AskFunc {
	return func(ctx context.Context, prompt string) (string, error) {
		fmt.Fprint(out, prompt)
		s, err := in.ReadLineLimit(ctx, d)
		if errors.Is(err, ErrTimeout) {
			fmt.Fprintln(out)
		}
		return s, err
	}
}

func pickUser(ctx context.Context, in *LineReader, out io.Writer, cfg *Config, opt string) (string, error) {
	if opt != "" {
		if err := ValidateUser(cfg, opt); err != nil {
			return "", err
		}
		return opt, nil
	}
	fmt.Fprintln(out, "\n사용할 user 를 선택하세요:")
	for i, u := range cfg.Users {
		fmt.Fprintf(out, "  %d) %s\n", i+1, u)
	}
	s, err := readChoice(ctx, in, "번호 입력: ", cfg.InputTimeout)
	if err != nil {
		return "", err
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < 1 || n > len(cfg.Users) {
		return "", fmt.Errorf("잘못된 선택입니다: %q (1~%d)", s, len(cfg.Users))
	}
	return cfg.Users[n-1], nil
}

func pickVCenter(ctx context.Context, in *LineReader, out io.Writer, cfg *Config, runDir, user, opt string) (string, error) {
	if opt != "" {
		if !vcenterRe.MatchString(opt) {
			return "", fmt.Errorf("vCenter 주소가 올바르지 않습니다: %q", opt)
		}
		return opt, nil
	}
	last := LoadLastVCenter(runDir)
	if last != "" {
		fmt.Fprintf(out, "[INFO] %s 이전 실행 vCenter: %s\n", user, last)
	}
	fmt.Fprintln(out, "\nvCenter 를 선택하세요:")
	for i, v := range cfg.VCenters {
		mark := ""
		if v == last {
			mark = "  [이전 실행]"
		}
		fmt.Fprintf(out, "  %d) %s%s\n", i+1, v, mark)
	}
	fmt.Fprintln(out, "  0) 직접 입력")
	s, err := readChoice(ctx, in, "번호 입력 (Enter = 이전 vCenter): ", cfg.InputTimeout)
	if err != nil {
		return "", err
	}
	switch {
	case s == "" && last != "":
		return last, nil
	case s == "":
		return "", errors.New("이전 실행 vCenter 가 없습니다. 번호를 입력하세요")
	case s == "0":
		v, err := readChoice(ctx, in, "vCenter 주소: ", cfg.InputTimeout)
		if err != nil {
			return "", err
		}
		if !vcenterRe.MatchString(v) {
			return "", fmt.Errorf("vCenter 주소가 올바르지 않습니다: %q", v)
		}
		return v, nil
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < 1 || n > len(cfg.VCenters) {
		return "", fmt.Errorf("잘못된 선택입니다: %q", s)
	}
	return cfg.VCenters[n-1], nil
}

func pickAction(ctx context.Context, in *LineReader, out io.Writer, d time.Duration) (Action, error) {
	fmt.Fprintln(out, "\n수행할 기능을 선택하세요:")
	fmt.Fprintln(out, "  1) Power Off          (Guest 종료, 안 꺼지면 강제 여부 확인)")
	fmt.Fprintln(out, "  2) Power On (순차)    (lpage 사용 BM: 같은 ev 번호를 동시에 켜고 전부 부팅된 뒤 다음 ev)")
	fmt.Fprintln(out, "  3) 일괄 Power On      (OS 설치용: 순서 없이 한 번에)")
	fmt.Fprintln(out, "  4) 재부팅             (전부 병렬: Power Off 완료 확인 후 한 번에 Power On)")
	s, err := readChoice(ctx, in, "번호 입력: ", d)
	if err != nil {
		return 0, err
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < 1 || n > 4 {
		return 0, fmt.Errorf("잘못된 선택입니다: %q (1~4)", s)
	}
	return Action(n), nil
}

// 출력 ---------------------------------------------------------------------

func printLookupError(out io.Writer, le *LookupError) {
	fmt.Fprintln(out, Red("[오류] 대상 VM 을 vCenter 에서 확인할 수 없습니다. 아무 작업도 하지 않았습니다."))
	for _, n := range le.Missing {
		fmt.Fprintf(out, "  - 없음: %s\n", n)
	}
	for n, paths := range le.Duplicate {
		fmt.Fprintf(out, "  - 같은 이름이 %d대: %s\n      %s\n", len(paths), n, strings.Join(paths, "\n      "))
	}
}

func powerLabel(p string) string {
	switch p {
	case "poweredOn":
		return "on"
	case "poweredOff":
		return "off"
	}
	return p
}

func printPlan(out io.Writer, act Action, vms []*VMInfo) {
	fmt.Fprintf(out, "\n%s 계획 — 총 %d대\n", Bold(act.String()), len(vms))
	seqMode := act == ActPowerOnSeq // 순차는 2번 기능만. Off/일괄 On/재부팅은 모두 병렬
	for _, g := range groupByBM(vms) {
		lp := GroupLpage(g.vms)
		tag := ""
		switch {
		case seqMode && lp:
			tag = "lpage 사용중 → ev 단계별 순차 부팅"
		case seqMode:
			tag = "lpage 미사용 → 일괄 부팅"
		case (act == ActPowerOnAll || act == ActReboot) && lp:
			tag = Yellow("lpage 사용중 BM — 동시 부팅 시 메모리 할당 실패 가능")
		}
		parts := make([]string, len(g.vms))
		for i, v := range g.vms {
			parts[i] = fmt.Sprintf("%s(%s)", v.Name, powerLabel(v.Power))
		}
		fmt.Fprintf(out, "  %-12s %s\n", g.key, strings.Join(parts, " "))
		if tag != "" {
			fmt.Fprintf(out, "  %-12s ※ %s\n", "", tag)
		}
		if g.vms[0].Host != "" && g.vms[0].Host != g.vms[0].NameBM {
			fmt.Fprintf(out, "  %-12s %s\n", "", Yellow(fmt.Sprintf("[주의] 이름상 BM(%s) 과 실제 호스트(%s)가 다릅니다", g.vms[0].NameBM, g.vms[0].Host)))
		}
	}
	if seqMode {
		for _, g := range groupByBM(vms) {
			if !GroupLpage(g.vms) {
				fmt.Fprintf(out, "※ %s: lpage 미사용 — 순서 상관없이 한 번에 부팅해도 됩니다.\n", g.key)
			}
		}
	}
	fmt.Fprintln(out)
}

func printTargetsByBM(w io.Writer, vms []*VMInfo) {
	for _, g := range groupByBM(vms) {
		for _, v := range g.vms {
			fmt.Fprintln(w, v.Name)
		}
	}
}
