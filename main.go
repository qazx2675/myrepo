// awx_copy: AWX 인벤토리 variables 를 중계로 써서 텍스트 파일을 옮기는 도구.
// 업로더(리눅스): awx_copy -u {user} [filename] [-csv]
// 다운로더(윈도우): awx_fetch.exe [옵션]   (또는 awx_copy --fetch)
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const version = "2.0.0"

type options struct {
	cfgPath       string
	userName      string
	listUsers     bool
	outDir        string
	fetch         bool
	restore       bool
	measure       bool
	measureMaxMiB int
	snapshot      bool
	status        bool
	noPause       bool
	rootPasswd    bool
	cli           bool
	noBrowser     bool
	version       bool
	help          bool
	positional    []string
}

func usage() {
	fmt.Print(`awx_copy ` + version + ` - AWX 인벤토리 variables 를 중계로 텍스트 파일 전송

사용법 (리눅스):
  awx_copy -u {user} {filename} [-csv]   업로드 (type 기본 txt, -csv 면 csv). 끝나면 기록 화면.
  awx_copy -u {user}                     내 업로드 기록 화면
  awx_copy                               설정 선택 화면 → 기록 화면
  awx_copy -u root                       root 비밀번호 확인 뒤 전체 사용자 기록 (AWX 계정 아님)
  awx_copy --root-passwd                 root 기록 조회 비밀번호 변경 (root 만)

관리/점검:
  --restore          남은 awx_copy 블록을 원본으로 복구
  --status           variables 길이/SHA-256/블록 여부 (GET 만)
  --snapshot         작업 템플릿/인벤토리 소스 modified 출력 (GET 만)
  --measure-limits [--measure-max-mib 64]   본문 한도 측정
  다운로더(윈도우): awx_fetch.exe            창 앱 (conf\{user}.conf, 받은 파일 관리)
                   awx_fetch.exe --cli [--user 이름] [--out 폴더]   창 없이 1건 수신 (출력: awx_fetch.log)

설정파일 선택:
  -u, --user <이름>  설정 폴더의 <이름>.conf
  --config <경로>    그 파일만 사용
  --list-users       읽을 수 있는 설정파일 목록
설정 폴더: 리눅스 /etc/awx_copy/{user}.conf (소유자=해당 사용자, 권한 600), 윈도우 실행파일 옆 conf\
업로드 기록: 리눅스 /tmp/.awx_copy/{user}.history.jsonl
`)
}

func parseArgs(args []string) (options, error) {
	o := options{measureMaxMiB: 64}
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "-h" || a == "--help":
			o.help = true
			continue
		case a == "-u":
			if i+1 >= len(args) {
				return o, errors.New("-u 에 사용자 이름이 필요합니다")
			}
			i++
			o.userName = args[i]
			continue
		case strings.HasPrefix(a, "-u="):
			o.userName = a[3:]
			continue
		case a == "-csv" || a == "-txt":
			o.positional = append(o.positional, a) // 두 번째 위치 인자로 다룬다
			continue
		case a == "-" || !strings.HasPrefix(a, "-"):
			o.positional = append(o.positional, a)
			continue
		case !strings.HasPrefix(a, "--") || a == "--":
			return o, fmt.Errorf("알 수 없는 옵션 %s", a)
		}
		name, val, hasVal := strings.Cut(a[2:], "=")
		need := func() (string, error) {
			if hasVal {
				return val, nil
			}
			if i+1 >= len(args) {
				return "", fmt.Errorf("--%s 에 값이 필요합니다", name)
			}
			i++
			return args[i], nil
		}
		var err error
		switch name {
		case "config":
			o.cfgPath, err = need()
		case "user":
			o.userName, err = need()
		case "list-users":
			o.listUsers = true
		case "out":
			o.outDir, err = need()
		case "fetch":
			o.fetch = true
		case "restore":
			o.restore = true
		case "measure-limits":
			o.measure = true
		case "measure-max-mib":
			var s string
			if s, err = need(); err == nil {
				o.measureMaxMiB, err = strconv.Atoi(s)
			}
		case "snapshot":
			o.snapshot = true
		case "status":
			o.status = true
		case "no-pause":
			o.noPause = true
		case "root-passwd":
			o.rootPasswd = true
		case "cli":
			o.cli = true
		case "no-browser":
			o.noBrowser = true
		case "version":
			o.version = true
		default:
			return o, fmt.Errorf("알 수 없는 옵션 --%s", name)
		}
		if err != nil {
			return o, err
		}
	}
	return o, nil
}

// uploadArgs 는 위치 인자에서 파일과 type 을 정한다. 두 번째 인자 -csv 면 csv, 없으면 txt.
func uploadArgs(pos []string) (file, typ string, err error) {
	if len(pos) == 0 {
		return "", "", nil
	}
	if len(pos) > 2 {
		return "", "", fmt.Errorf("인자가 너무 많습니다: %s", strings.Join(pos, " "))
	}
	file, typ = pos[0], "txt"
	if file == "-csv" || file == "-txt" {
		return "", "", errors.New("파일 이름이 먼저 와야 합니다: awx_copy -u {user} {filename} [-csv]")
	}
	if len(pos) == 2 {
		switch strings.ToLower(pos[1]) {
		case "-csv", "csv":
			typ = "csv"
		case "-txt", "txt":
			typ = "txt"
		default:
			return "", "", fmt.Errorf("두 번째 인자는 -csv 만 쓸 수 있습니다: %q", pos[1])
		}
	}
	return file, typ, nil
}

func main() {
	code := run(os.Args)
	os.Exit(code)
}

func run(argv []string) int {
	o, err := parseArgs(argv[1:])
	exe := strings.ToLower(filepath.Base(argv[0]))
	fetchMode := o.fetch || strings.Contains(exe, "fetch")
	if runtime.GOOS == "windows" && err == nil && fetchMode && !o.cli && !o.help && !o.version && !o.restore &&
		!o.status && !o.snapshot && !o.measure && !o.listUsers {
		return runGUI(o.noBrowser) // 창 앱
	}
	if runtime.GOOS == "windows" && !hasConsoleWindow() {
		// -H windowsgui 빌드의 CLI 모드: 부모 콘솔에 붙이고, 출력은 awx_fetch.log(UTF-8)에도 남긴다.
		finish := teeCLIOutput(filepath.Join(exeDir(), "awx_fetch.log"))
		defer finish()
	}
	setupConsole()
	if err != nil {
		fmt.Fprintln(os.Stderr, "오류:", err)
		fmt.Fprintln(os.Stderr, "도움말: awx_copy --help")
		return 2
	}
	if o.help {
		usage()
		return 0
	}
	if o.version {
		fmt.Println("awx_copy", version, runtime.GOOS+"/"+runtime.GOARCH, runtime.Version())
		return 0
	}
	pause := false // 창 앱이 기본이라 Enter 대기는 쓰지 않는다 (--no-pause 는 호환용으로 받는다)
	if pause {
		defer func() {
			fmt.Print("\nEnter 키를 누르면 창을 닫습니다...")
			_, _ = stdinR.ReadString('\n')
		}()
	}
	win := runtime.GOOS == "windows"
	if !win && o.rootPasswd {
		return runRootPasswd(os.Stdout)
	}
	if !win && o.userName == "root" && o.cfgPath == "" {
		if len(o.positional) > 0 {
			fmt.Fprintln(os.Stderr, "오류: -u root 는 기록 조회 전용입니다. 업로드는 각 사용자 설정으로 하세요.")
			return 2
		}
		return runRootView(os.Stdout)
	}

	sel := newSelector()
	if o.listUsers {
		sel.listUsers(os.Stdout)
		return 0
	}
	file, typ, err := uploadArgs(o.positional)
	if err != nil && !fetchMode {
		fmt.Fprintln(os.Stderr, "오류:", err)
		return 2
	}
	cfg, code, err := sel.resolveConfig(o.cfgPath, o.userName)
	if err != nil {
		fmt.Fprintln(os.Stderr, "오류:", err)
		return code
	}
	c := newClient(cfg)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	switch {
	case o.restore:
		err = runRestore(ctx, c, cfg, os.Stdout)
	case o.measure:
		err = measureLimits(ctx, c, o.measureMaxMiB, os.Stdout)
	case o.snapshot:
		err = snapshot(ctx, c, os.Stdout)
	case o.status:
		var v string
		if v, err = c.GetVars(ctx); err == nil {
			b, _ := parseBlock(v)
			fmt.Printf("variables len=%d sha256=%s block=%v session=%s state=%s\n", len(v), sha256Hex([]byte(v)), hasBlock(v), b["session"], b["state"])
		}
	case fetchMode:
		_, err = fetchFile(ctx, c, cfg, o.outDir, os.Stdout)
	case file == "" && win:
		usage()
		return 2
	case file == "":
		showUserHistory(os.Stdout, confUser(cfg))
		return 0
	default:
		tty := stdoutIsTerminal()
		rec, uerr := doUpload(ctx, c, cfg, confUser(cfg), file, typ, os.Stdout, tty)
		if herr := appendHistory(histDir, rec); herr != nil {
			fmt.Fprintln(os.Stderr, "경고: 기록 저장 실패:", herr)
		}
		stop() // 기록 화면에서는 Ctrl+C 를 기본 동작으로
		if uerr != nil && !errors.Is(uerr, context.Canceled) {
			fmt.Fprintln(os.Stderr, "오류:", uerr)
		}
		if !win && tty && stdinIsTerminal() {
			showUserHistory(os.Stdout, confUser(cfg))
		}
		if uerr != nil {
			if errors.Is(uerr, context.Canceled) {
				fmt.Fprintln(os.Stderr, "중단됨 (Ctrl+C). variables 원복을 시도했습니다. 남아 있으면 --restore 를 실행하세요.")
				return 130
			}
			return 1
		}
		return 0
	}
	if err != nil {
		if errors.Is(err, context.Canceled) {
			fmt.Fprintln(os.Stderr, "중단됨 (Ctrl+C). variables 원복을 시도했습니다. 남아 있으면 --restore 를 실행하세요.")
			return 130
		}
		fmt.Fprintln(os.Stderr, "오류:", err)
		return 1
	}
	return 0
}

// teeCLIOutput 은 표준 출력/오류를 파이프로 바꿔 부모 콘솔(있으면)과 로그 파일에 함께 쓴다.
// 돌려준 함수를 종료 전에 불러 남은 출력을 비운다.
func teeCLIOutput(logPath string) func() {
	console := attachParentConsole()
	var outs []io.Writer
	if console {
		outs = append(outs, os.Stdout)
	}
	lf := &fileLog{path: logPath}
	outs = append(outs, lf)
	r, w, err := os.Pipe()
	if err != nil {
		return func() {}
	}
	os.Stdout, os.Stderr = w, w
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = io.Copy(io.MultiWriter(outs...), r)
	}()
	return func() {
		_ = w.Close()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
		}
	}
}

// doUpload 는 업로드하고 기록 한 줄을 만든다. tty 면 진행 막대를 쓴다.
func doUpload(ctx context.Context, c *Client, cfg *Config, user, file, typ string, out io.Writer, tty bool) (histRec, error) {
	start := time.Now()
	abs, aerr := filepath.Abs(file)
	if aerr != nil {
		abs = file
	}
	log := out
	var sink progressSink
	var prog *ttyProgress
	if tty {
		prog = newTTYProgress(out, filepath.Base(file))
		log, sink = prog, prog
	}
	st, err := sendFileP(ctx, c, cfg, file, typ, log, sink)
	if prog != nil {
		prog.End()
	}
	rec := histRec{
		TS: start.Format(time.RFC3339), User: user, File: abs, Type: typ,
		Size: st.Size, Chunks: st.Chunks, Seconds: time.Since(start).Seconds(),
		Result: resultSuccess, Message: "수신 확인(SHA-256 일치)",
	}
	switch {
	case err == nil:
	case errors.Is(err, context.Canceled):
		rec.Result, rec.Message = resultAborted, "Ctrl+C 중단, variables 원복 시도"
	default:
		rec.Result, rec.Message = resultFailed, oneLine(err.Error(), 300)
	}
	return rec, err
}

// showUserHistory 는 한 사용자의 기록 화면이다.
func showUserHistory(out io.Writer, user string) {
	rs, err := loadHistory(histDir, user)
	if err != nil {
		fmt.Fprintln(os.Stderr, "경고: 기록을 읽을 수 없습니다:", err)
	}
	header, rows := historyRows(rs, false)
	tty := stdoutIsTerminal() && stdinIsTerminal()
	showTable(out, tty, fmt.Sprintf("업로드 기록: %s (%s, 최근 순)", user, histFile(histDir, user)), header, rows)
}

// runRootView 는 -u root 기록 조회다.
func runRootView(out io.Writer) int {
	want, err := readRootHash(filepath.Join(etcDir, rootConfName))
	if err != nil {
		fmt.Fprintln(os.Stderr, "오류:", err)
		return 1
	}
	if err := rootLogin(stdinR, out, readPassword, want); err != nil {
		fmt.Fprintln(os.Stderr, "오류:", err)
		return 1
	}
	rs, bad := loadAllHistory(histDir)
	header, rows := historyRows(rs, true)
	title := fmt.Sprintf("전체 업로드 기록 (%s, 최근 순)", histDir)
	if bad > 0 {
		title += fmt.Sprintf(" - 읽지 못한 파일 %d개", bad)
	}
	showTable(out, stdoutIsTerminal() && stdinIsTerminal(), title, header, rows)
	return 0
}

// runRootPasswd 는 root.conf 의 해시를 바꾼다. root 만 실행할 수 있다.
func runRootPasswd(out io.Writer) int {
	if euid() != 0 {
		fmt.Fprintln(os.Stderr, "오류: --root-passwd 는 root 만 실행할 수 있습니다")
		return 1
	}
	read := func(prompt string) (string, error) {
		fmt.Fprint(out, prompt)
		pw, err := readPassword(stdinR, out)
		fmt.Fprintln(out)
		return pw, err
	}
	p1, err := read("새 root 기록 조회 비밀번호: ")
	if err != nil || len(p1) < 8 {
		fmt.Fprintln(os.Stderr, "오류: 8자 이상 입력하세요")
		return 1
	}
	p2, err := read("한 번 더: ")
	if err != nil || p1 != p2 {
		fmt.Fprintln(os.Stderr, "오류: 두 입력이 다릅니다")
		return 1
	}
	if err := os.MkdirAll(etcDir, 0o700); err != nil {
		fmt.Fprintln(os.Stderr, "오류:", err)
		return 1
	}
	if err := writeRootHash(filepath.Join(etcDir, rootConfName), p1); err != nil {
		fmt.Fprintln(os.Stderr, "오류:", err)
		return 1
	}
	fmt.Fprintln(out, "변경했습니다 (해시만 저장).")
	return 0
}
