// ip-change-engine 은 관리 노드에서 실행되며, 대상 목록 파일(hostname 변경될ip)에
// 따라 대상 노드들의 IP·게이트웨이를 gossh 로 일괄 변경합니다.
//
// 노드에 직접 접속하지 않고, 대상 전체를 담은 apply 스크립트 하나를 만들어
// gossh 로 밀어넣습니다. 서비스 재시작은 하지 않습니다.
package main

import (
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"

	"ip-change/internal/color"
	"ip-change/internal/config"
	"ip-change/internal/remote"
	"ip-change/internal/render"
	"ip-change/internal/target"
)

func main() {
	var (
		confPath   = flag.String("config", "./ip_change.conf", "설정 파일 경로 (없어도 기본값으로 동작)")
		targetPath = flag.String("targets", "", "대상 목록 파일 경로 (필수, \"hostname 변경될ip\" 형식)")

		gosshPath   = flag.String("gossh", "gossh", "gossh 실행 파일 경로")
		user        = flag.String("u", "root", "SSH 접속 계정")
		password    = flag.String("p", "", "SSH 접속 비밀번호")
		keyPath     = flag.String("i", "", "SSH 키 파일 경로")
		port        = flag.String("P", "22", "SSH 포트")
		concurrency = flag.Int("c", 0, "gossh 동시 접속 수 (0 이면 gossh 기본값)")
		timeoutSec  = flag.Int("t", 0, "gossh 접속 타임아웃 초 (0 이면 gossh 기본값)")
		remotePath  = flag.String("remote-path", "/root/ip_change_apply.sh", "원격에 떨어뜨릴 스크립트 경로")

		rollback   = flag.Bool("rollback", false, "IP 변경을 되돌립니다 (각 노드의 최근 <ifcfg>.bak.<STAMP> 복원)")
		rollbackTo = flag.String("rollback-to", "", "되돌릴 백업 STAMP 지정 (미지정 시 가장 최근)")
	)
	flag.Parse()

	if *targetPath == "" {
		fmt.Fprintln(os.Stderr, color.BoldRed("오류: -targets 를 지정하십시오."))
		os.Exit(1)
	}

	if err := run(*confPath, *targetPath, *rollback, *rollbackTo, remote.Options{
		GosshPath: *gosshPath, User: *user, Password: *password,
		KeyPath: *keyPath, Port: *port, Concurrency: *concurrency,
		TimeoutSec: *timeoutSec, RemotePath: *remotePath,
	}); err != nil {
		fmt.Fprintln(os.Stderr, color.BoldRed("오류: "+err.Error()))
		os.Exit(1)
	}
}

func run(confPath, targetPath string, rollback bool, rollbackTo string, opts remote.Options) error {
	cfg, err := config.Load(confPath)
	if err != nil {
		return fmt.Errorf("설정 파일: %w", err)
	}

	if rollback {
		return runRollback(cfg, targetPath, rollbackTo, opts)
	}

	entries, err := target.Load(targetPath)
	if err != nil {
		return fmt.Errorf("대상 목록 파일: %w", err)
	}

	hosts := make([]string, 0, len(entries))
	for _, e := range entries {
		hosts = append(hosts, e.Host)
	}
	sort.Strings(hosts)

	fmt.Println(color.BoldCyan(fmt.Sprintf("대상=%d대  network_scripts_dir=%s", len(entries), cfg.NetworkScriptsDir)))
	if cfg.RHEL9Path != "" {
		fmt.Println(color.Cyan("RHEL9 이상 경로(rhel9_path)=" + cfg.RHEL9Path))
	}
	fmt.Println(color.Cyan("작업 대상:"))
	for _, e := range entries {
		fmt.Printf("  %-20s -> %s\n", e.Host, e.NewIP)
	}
	fmt.Println(strings.Repeat("-", 60))

	script, err := render.ApplyScript(entries, cfg)
	if err != nil {
		return err
	}

	hostFile, cleanup, err := remote.WriteHostFile(hosts)
	if err != nil {
		return fmt.Errorf("호스트 목록 임시파일: %w", err)
	}
	defer cleanup()

	cmdline := remote.BuildCommand(script, opts)
	results, raw, runErr := remote.Run(hostFile, cmdline, opts)

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

	byHost := map[string]remote.Result{}
	for _, r := range results {
		byHost[r.Host] = r
	}

	okCount, failCount := 0, 0
	for _, h := range hosts {
		r, reported := byHost[h]
		if !reported {
			fmt.Println(color.BoldRed(fmt.Sprintf("%-20s UNREACHABLE (gossh 응답 없음)", h)))
			failCount++
			continue
		}

		line := r.LastLine()
		display, ok := formatResultLine(h, line)
		if ok {
			fmt.Println(color.Green(display))
			okCount++
		} else {
			fmt.Println(color.BoldRed(display))
			for _, l := range r.Lines {
				if l != line {
					fmt.Println(color.Yellow("    " + l))
				}
			}
			failCount++
		}
	}

	fmt.Println(strings.Repeat("-", 60))
	fmt.Printf("%s %d대   %s %d대\n", color.Green("OK"), okCount, color.BoldRed("FAIL"), failCount)

	if failCount > 0 {
		os.Exit(2)
	}
	return nil
}

// runRollback 은 대상 노드에서 최근(또는 지정 STAMP) ifcfg 백업을 되돌립니다.
func runRollback(cfg *config.Config, targetPath, rollbackTo string, opts remote.Options) error {
	hosts, err := target.LoadHosts(targetPath)
	if err != nil {
		return fmt.Errorf("대상 목록 파일: %w", err)
	}
	sort.Strings(hosts)

	to := rollbackTo
	if to == "" {
		to = "(가장 최근)"
	}
	fmt.Println(color.BoldCyan(fmt.Sprintf("롤백 대상=%d대  network_scripts_dir=%s  STAMP=%s", len(hosts), cfg.NetworkScriptsDir, to)))
	fmt.Println(strings.Repeat("-", 60))

	script, err := render.RollbackScript(cfg, rollbackTo)
	if err != nil {
		return err
	}

	hostFile, cleanup, err := remote.WriteHostFile(hosts)
	if err != nil {
		return fmt.Errorf("호스트 목록 임시파일: %w", err)
	}
	defer cleanup()

	cmdline := remote.BuildCommand(script, opts)
	results, raw, runErr := remote.Run(hostFile, cmdline, opts)

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

	byHost := map[string]remote.Result{}
	for _, r := range results {
		byHost[r.Host] = r
	}

	okCount, failCount := 0, 0
	for _, h := range hosts {
		r, reported := byHost[h]
		if !reported {
			fmt.Println(color.BoldRed(fmt.Sprintf("%-20s UNREACHABLE (gossh 응답 없음)", h)))
			failCount++
			continue
		}
		display, ok := formatRollbackLine(h, r.LastLine())
		if ok {
			fmt.Println(color.Green(display))
			okCount++
		} else {
			fmt.Println(color.BoldRed(display))
			for _, l := range r.Lines {
				if l != r.LastLine() {
					fmt.Println(color.Yellow("    " + l))
				}
			}
			failCount++
		}
	}

	fmt.Println(strings.Repeat("-", 60))
	fmt.Printf("%s %d대   %s %d대\n", color.Green("OK"), okCount, color.BoldRed("FAIL"), failCount)

	if failCount > 0 {
		os.Exit(2)
	}
	return nil
}

// formatRollbackLine 은 rollback_body.sh 의 "RESULT|..." 한 줄을 화면용으로 바꿉니다.
func formatRollbackLine(host, line string) (string, bool) {
	fields := strings.Split(line, "|")
	if len(fields) >= 2 && fields[0] == "RESULT" {
		switch fields[1] {
		case "OK":
			if len(fields) == 6 {
				h, ifcfg, stamp, ip := fields[2], fields[3], fields[4], fields[5]
				return fmt.Sprintf("%-20s 복원 %s -> IPADDR %s   (STAMP %s)", h, ifcfg, ip, stamp), true
			}
		case "FAIL":
			if len(fields) >= 4 {
				return fmt.Sprintf("%-20s FAIL: %s", fields[2], strings.Join(fields[3:], "|")), false
			}
		}
	}
	return fmt.Sprintf("%-20s 알 수 없는 응답: %s", host, line), false
}

// formatResultLine 은 apply_body.sh 가 찍은 "RESULT|..." 기계용 한 줄을
// 화면에 보일 "hostname 기존IP -> 변경IP (GW ...)" 형태로 바꿉니다.
// 두 번째 반환값은 성공(OK) 여부입니다.
func formatResultLine(host, line string) (string, bool) {
	fields := strings.Split(line, "|")
	if len(fields) >= 2 && fields[0] == "RESULT" {
		switch fields[1] {
		case "OK":
			if len(fields) == 6 {
				h, curIP, newIP, gw := fields[2], fields[3], fields[4], fields[5]
				return fmt.Sprintf("%-20s %s -> %s   (GW %s)", h, curIP, newIP, gw), true
			}
		case "FAIL":
			if len(fields) >= 4 {
				h := fields[2]
				reason := strings.Join(fields[3:], "|")
				return fmt.Sprintf("%-20s FAIL: %s", h, reason), false
			}
		}
	}
	return fmt.Sprintf("%-20s 알 수 없는 응답: %s", host, line), false
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
