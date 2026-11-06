// account-mount-engine 은 관리 노드에서 실행되며, info.txt 에 기록된
// 계정 수동 마운트 정보를 읽어 대상 호스트들(-hosts 또는 -host)에 gossh 로
// 일괄 적용합니다.
//
// 적용 내용:
//   1. /etc/auto.master : `/user /etc/auto.home` 이 없으면 가장 아랫줄에 추가 (공백 무관 중복 제외)
//   2. /etc/passwd      : `계정:x:UID:GID:r계정:/user/계정:/bin/csh` 추가 또는 갱신
//   3. /etc/group       : `계정:x:UID:` 추가 또는 갱신
//   4. /etc/auto.home   : `계정 -rw,hard,actimeo=1,proto=tcp,vers=3 스토리지/계정` 추가 또는 갱신
//   5. 변경 전 `<파일>.bak.<STAMP>` 백업 생성 및 autofs 설정 변경 시 autofs 리로드/재시작
package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"account-mount/internal/color"
	"account-mount/internal/info"
	"account-mount/internal/remote"
	"account-mount/internal/render"
)

func main() {
	var (
		infoPath    = flag.String("info", "./info.txt", "계정 마운트 정보 파일 경로 (\"계정 UID/GID 스토리지\" 줄바꿈 단위)")
		hostsPath   = flag.String("hosts", "./hosts.txt", "작업 대상 호스트 목록 파일 경로 (한 줄에 hostname 또는 IP 하나)")
		onlyHost    = flag.String("host", "", "단일 대상 호스트 지정 (지정 시 -hosts 파일 대신 이 호스트만 처리)")
		localMode   = flag.Bool("local", false, "gossh 없이 현재 로컬 서버에 직접 적용")
		dryRun      = flag.Bool("dry-run", false, "실제로 파일을 수정하지 않고 변경 예정 내용(diff)만 확인")
		printScript = flag.Bool("print-script", false, "생성될 적용 스크립트를 표준출력으로 찍고 종료")
		root        = flag.String("root", "", "원격(또는 로컬)에서 기록할 루트 경로 (테스트용, 비우면 실제 /etc)")

		listBackups = flag.Bool("list-backups", false, "각 노드에 남아 있는 백업 시점(STAMP) 목록 조회")
		rollback    = flag.Bool("rollback", false, "가장 최근 백업 시점으로 되돌리기")
		rollbackTo  = flag.String("rollback-to", "", "지정한 백업 시점(14자리 STAMP)으로 되돌리기")

		gosshPath   = flag.String("gossh", "gossh", "gossh 실행 파일 경로")
		user        = flag.String("u", "root", "SSH 접속 계정")
		password    = flag.String("p", "", "SSH 접속 비밀번호")
		keyPath     = flag.String("i", "", "SSH 키 파일 경로")
		port        = flag.String("P", "22", "SSH 포트")
		concurrency = flag.Int("c", 0, "gossh 동시 접속 수 (0 이면 gossh 기본값)")
		timeoutSec  = flag.Int("t", 0, "gossh 접속 타임아웃 초 (0 이면 gossh 기본값)")
		remotePath  = flag.String("remote-path", "/root/account_mount_apply.sh", "원격에 임시로 생성할 스크립트 경로")
	)
	flag.Parse()

	var rbMode render.RollbackMode
	rbCount := 0
	if *listBackups {
		rbMode, rbCount = render.RollbackList, rbCount+1
	}
	if *rollback {
		rbMode, rbCount = render.RollbackLatest, rbCount+1
	}
	if *rollbackTo != "" {
		rbMode, rbCount = render.RollbackStamp, rbCount+1
	}
	if rbCount > 1 {
		fmt.Fprintln(os.Stderr, color.BoldRed("오류: -list-backups / -rollback / -rollback-to 중 하나만 지정하십시오."))
		os.Exit(1)
	}

	if err := run(options{
		infoPath:    *infoPath,
		hostsPath:   *hostsPath,
		onlyHost:    *onlyHost,
		localMode:   *localMode,
		dryRun:      *dryRun,
		printScript: *printScript,
		root:        *root,
		rbMode:      rbMode,
		rbStamp:     *rollbackTo,
		remote: remote.Options{
			GosshPath:   *gosshPath,
			User:        *user,
			Password:    *password,
			KeyPath:     *keyPath,
			Port:        *port,
			Concurrency: *concurrency,
			TimeoutSec:  *timeoutSec,
			RemotePath:  *remotePath,
			Root:        *root,
			DryRun:      *dryRun,
		},
	}); err != nil {
		fmt.Fprintln(os.Stderr, color.BoldRed("오류: "+err.Error()))
		os.Exit(1)
	}
}

type options struct {
	infoPath    string
	hostsPath   string
	onlyHost    string
	localMode   bool
	dryRun      bool
	printScript bool
	root        string
	rbMode      render.RollbackMode
	rbStamp     string
	remote      remote.Options
}

func run(o options) error {
	if o.rbMode != "" {
		return runRollback(o)
	}

	accounts, err := info.LoadAccounts(o.infoPath)
	if err != nil {
		return fmt.Errorf("계정 정보 파일: %w", err)
	}

	script, err := render.ApplyScript(accounts)
	if err != nil {
		return err
	}

	if o.printScript {
		fmt.Print(script)
		return nil
	}

	if o.localMode {
		return runLocal(script, o, accounts)
	}

	hosts, err := resolveHosts(o)
	if err != nil {
		return err
	}

	mode := "적용"
	if o.dryRun {
		mode = "DRY-RUN (실제 변경 없음)"
	}
	fmt.Println(color.BoldCyan(fmt.Sprintf("대상 호스트=%d대  등록 계정=%d건  모드=%s", len(hosts), len(accounts), mode)))
	if o.root != "" {
		fmt.Println(color.Yellow(fmt.Sprintf("경고: -root=%s 로 실제 /etc 가 아닌 경로에 기록합니다 (테스트 모드)", o.root)))
	}
	fmt.Println(color.Cyan("적용 계정 목록 (info.txt):"))
	for _, a := range accounts {
		fmt.Printf("  - 계정=%-14s UID/GID=%-8s 스토리지=%s/%s\n", a.User, a.ID, a.Storage, a.User)
	}
	fmt.Println(strings.Repeat("-", 70))

	hostFile, cleanup, err := remote.WriteHostFile(hosts)
	if err != nil {
		return fmt.Errorf("호스트 목록 임시파일: %w", err)
	}
	defer cleanup()

	cmdline := remote.BuildCommand(script, o.remote)
	results, raw, runErr := remote.Run(hostFile, cmdline, o.remote)
	if len(results) == 0 {
		fmt.Println(color.BoldRed("gossh 출력이 비어 있습니다."))
		if runErr != nil {
			fmt.Println(color.BoldRed(fmt.Sprintf("gossh 오류: %v", runErr)))
		}
		if strings.TrimSpace(raw) != "" {
			fmt.Println(indent(raw))
		}
		os.Exit(2)
	}

	return printResults(hosts, results, o.dryRun, false)
}

func runRollback(o options) error {
	script, err := render.RollbackScript(o.rbMode, o.rbStamp)
	if err != nil {
		return err
	}

	if o.printScript {
		fmt.Print(script)
		return nil
	}

	if o.localMode {
		return runLocal(script, o, nil)
	}

	hosts, err := resolveHosts(o)
	if err != nil {
		return err
	}

	switch o.rbMode {
	case render.RollbackList:
		fmt.Println(color.BoldCyan(fmt.Sprintf("백업 시점 조회  대상=%d대", len(hosts))))
	case render.RollbackLatest:
		fmt.Println(color.BoldCyan(fmt.Sprintf("되돌리기(가장 최근 시점)  대상=%d대  모드=%s", len(hosts), modeLabel(o.dryRun))))
	case render.RollbackStamp:
		fmt.Println(color.BoldCyan(fmt.Sprintf("되돌리기(시점 %s)  대상=%d대  모드=%s", o.rbStamp, len(hosts), modeLabel(o.dryRun))))
	}
	if o.root != "" {
		fmt.Println(color.Yellow(fmt.Sprintf("경고: -root=%s 로 실제 /etc 가 아닌 곳을 대상으로 합니다 (테스트 모드)", o.root)))
	}
	fmt.Println(strings.Repeat("-", 70))

	hostFile, cleanup, err := remote.WriteHostFile(hosts)
	if err != nil {
		return fmt.Errorf("호스트 목록 임시파일: %w", err)
	}
	defer cleanup()

	results, raw, runErr := remote.Run(hostFile, remote.BuildCommand(script, o.remote), o.remote)
	if len(results) == 0 {
		if runErr != nil {
			fmt.Println(color.BoldRed(fmt.Sprintf("gossh 오류: %v", runErr)))
		}
		if strings.TrimSpace(raw) != "" {
			fmt.Println(indent(raw))
		}
		return fmt.Errorf("gossh 출력이 비어 있습니다")
	}

	if o.rbMode == render.RollbackList {
		for _, r := range results {
			fmt.Printf("  %s\n", color.Cyan(r.Host))
			shown := false
			for _, l := range r.Lines {
				if strings.HasPrefix(l, "BACKUP|") {
					f := strings.SplitN(l, "|", 3)
					if len(f) == 3 {
						fmt.Printf("      %s   %s\n", f[1], f[2])
						shown = true
					}
				}
			}
			if !shown {
				fmt.Printf("      (백업 없음)\n")
			}
		}
		return nil
	}

	return printResults(hosts, results, o.dryRun, true)
}

func runLocal(script string, o options, accounts []info.Account) error {
	f, err := os.CreateTemp("", "account-mount-local-*.sh")
	if err != nil {
		return err
	}
	tmpPath := filepath.Clean(f.Name())
	defer os.Remove(tmpPath)

	if _, err := f.WriteString(script); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	_ = os.Chmod(tmpPath, 0o700)

	cmd := exec.Command("bash", tmpPath)
	env := os.Environ()
	if o.root != "" {
		env = append(env, "ROOT="+o.root)
	}
	if o.dryRun {
		env = append(env, "DRYRUN=1")
	}
	cmd.Env = env
	out, runErr := cmd.CombinedOutput()

	lines := strings.Split(strings.TrimRight(string(out), "\r\n"), "\n")
	res := remote.Result{Host: "localhost", Lines: lines}
	_ = runErr
	_ = accounts
	return printResults([]string{"localhost"}, []remote.Result{res}, o.dryRun, o.rbMode != "")
}

func resolveHosts(o options) ([]string, error) {
	if strings.TrimSpace(o.onlyHost) != "" {
		return []string{strings.TrimSpace(o.onlyHost)}, nil
	}
	hosts, err := info.LoadHosts(o.hostsPath)
	if err != nil {
		return nil, fmt.Errorf("대상 호스트 파일: %w", err)
	}
	sort.Strings(hosts)
	return hosts, nil
}

func printResults(hosts []string, results []remote.Result, dryRun bool, isRollback bool) error {
	byHost := map[string]remote.Result{}
	for _, r := range results {
		byHost[r.Host] = r
	}

	totals := map[string]int{}
	for _, h := range hosts {
		r, reported := byHost[h]
		if !reported {
			totals["UNREACHABLE"]++
			fmt.Println(color.BoldRed(fmt.Sprintf("  %-24s UNREACHABLE (gossh 응답 없음)", h)))
			continue
		}

		st := r.Summary()
		totals[st]++

		resLine := r.ResultLine()
		detail := ""
		if parts := strings.Split(resLine, "|"); len(parts) > 3 {
			detail = " (" + strings.Join(parts[3:], " ") + ")"
		}

		switch st {
		case "OK":
			fmt.Println(color.Green(fmt.Sprintf("  %-24s %s%s", h, st, detail)))
		case "NOCHANGE", "NOBACKUP":
			fmt.Println(color.Cyan(fmt.Sprintf("  %-24s %s%s", h, st, detail)))
		default:
			fmt.Println(color.BoldRed(fmt.Sprintf("  %-24s %s%s", h, st, detail)))
		}

		if dryRun || st == "FAIL" || st == "NORESULT" {
			for _, l := range r.Lines {
				if strings.HasPrefix(l, "RESULT|") {
					continue
				}
				if strings.HasPrefix(l, "DIFF|") {
					fmt.Println("      " + strings.TrimPrefix(l, "DIFF|"))
				} else {
					fmt.Println(color.Yellow("      " + l))
				}
			}
		}
	}

	fmt.Println(strings.Repeat("-", 70))
	keys := make([]string, 0, len(totals))
	for k := range totals {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		fmt.Printf("%-12s %d대\n", k, totals[k])
	}

	if !dryRun {
		fmt.Println()
		if isRollback {
			fmt.Println("★ 되돌리기가 끝났습니다. 대상 서버 중 무작위로 몇 대에 직접 접속해")
			fmt.Println("  설정이 실제로 복원됐는지 반드시 눈으로 확인하십시오.")
		} else {
			fmt.Println("★ 적용이 끝났습니다. 대상 서버 중 무작위로 몇 대에 직접 접속해")
			fmt.Println("  설정이 의도대로 반영되었는지 반드시 교차 검증하십시오.")
		}
	}

	if totals["FAIL"] > 0 || totals["UNREACHABLE"] > 0 || totals["NORESULT"] > 0 {
		os.Exit(2)
	}
	return nil
}

func modeLabel(dryRun bool) string {
	if dryRun {
		return "DRY-RUN (실제 변경 없음)"
	}
	return "적용"
}

func indent(s string) string {
	var b strings.Builder
	for _, l := range strings.Split(strings.TrimRight(s, "\n"), "\n") {
		b.WriteString("    ")
		b.WriteString(l)
		b.WriteString("\n")
	}
	return strings.TrimRight(b.String(), "\n")
}
