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
	)
	flag.Parse()

	if *targetPath == "" {
		fmt.Fprintln(os.Stderr, color.BoldRed("오류: -targets 를 지정하십시오."))
		os.Exit(1)
	}

	if err := run(*confPath, *targetPath, remote.Options{
		GosshPath: *gosshPath, User: *user, Password: *password,
		KeyPath: *keyPath, Port: *port, Concurrency: *concurrency,
		TimeoutSec: *timeoutSec, RemotePath: *remotePath,
	}); err != nil {
		fmt.Fprintln(os.Stderr, color.BoldRed("오류: "+err.Error()))
		os.Exit(1)
	}
}

func run(confPath, targetPath string, opts remote.Options) error {
	cfg, err := config.Load(confPath)
	if err != nil {
		return fmt.Errorf("설정 파일: %w", err)
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
		fields := strings.Fields(line)
		if len(fields) == 3 && fields[1] != "FAIL" {
			fmt.Println(color.Green(line))
			okCount++
		} else {
			fmt.Println(color.BoldRed(line))
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

func indent(s string) string {
	var b strings.Builder
	for _, l := range strings.Split(strings.TrimRight(s, "\n"), "\n") {
		b.WriteString("    ")
		b.WriteString(l)
		b.WriteString("\n")
	}
	return strings.TrimRight(b.String(), "\n")
}
