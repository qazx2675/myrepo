// ldap-config-engine 은 관리 노드에서 실행되며, 자산현황 파일의 사이트 지정에 따라
// 대상 노드들의 LDAP/DNS/NTP/autofs 설정을 gossh 로 일괄 적용합니다.
//
// 노드에 직접 접속하지 않고, 사이트별 apply 스크립트를 만들어 gossh 로 밀어넣습니다.
package main

import (
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"

	"ldap-automation/internal/asset"
	"ldap-automation/internal/config"
	"ldap-automation/internal/remote"
	"ldap-automation/internal/render"
)

func main() {
	var (
		confPath     = flag.String("config", "./ldap_config.conf", "설정 파일 경로")
		assetPath    = flag.String("assets", "./assets.txt", "자산현황 파일 경로 (hostname<TAB>site)")
		infraName    = flag.String("infra", "", "대상 인프라 이름 (필수). 실수 방지를 위해 기본값 없음")
		onlySite     = flag.String("site", "", "이 사이트만 처리 (비우면 전체)")
		onlyHost     = flag.String("host", "", "이 호스트 하나만 처리 (비우면 전체)")
		hostListPath = flag.String("host-file", "", "이 파일에 나열된 호스트만 처리 (한 줄에 hostname 하나, site 정보 없음). 사이트는 항상 -assets 에서 조회합니다")
		dryRun       = flag.Bool("dry-run", false, "실제로 바꾸지 않고 바뀔 내용만 보고")
		printScript  = flag.Bool("print-script", false, "apply 스크립트만 표준출력으로 찍고 종료 (-site 필요)")
		root         = flag.String("root", "", "원격에서 기록할 루트 (테스트용. 비우면 실제 /etc)")
		defaultSite  = flag.String("default-site", "", "자산현황에서 못 찾은 호스트에 적용할 site (conf 의 default_site 를 덮어씀)")

		listBackups = flag.Bool("list-backups", false, "각 노드에 남아 있는 백업 시점 목록만 조회 (변경 없음)")
		rollback    = flag.Bool("rollback", false, "가장 최근 백업 시점으로 되돌리기")
		rollbackTo  = flag.String("rollback-to", "", "지정한 백업 시점(숫자 14자리)으로 되돌리기")

		gosshPath   = flag.String("gossh", "gossh", "gossh 실행 파일 경로")
		user        = flag.String("u", "root", "SSH 접속 계정")
		password    = flag.String("p", "", "SSH 접속 비밀번호")
		keyPath     = flag.String("i", "", "SSH 키 파일 경로")
		port        = flag.String("P", "22", "SSH 포트")
		concurrency = flag.Int("c", 0, "gossh 동시 접속 수 (0 이면 gossh 기본값)")
		timeoutSec  = flag.Int("t", 0, "gossh 접속 타임아웃 초 (0 이면 gossh 기본값)")
		remotePath  = flag.String("remote-path", "/root/ldap_apply.sh", "원격에 떨어뜨릴 스크립트 경로")
	)
	flag.Parse()

	// 되돌리기 방식은 셋 중 하나만 지정할 수 있습니다.
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
		fmt.Fprintln(os.Stderr, "오류: -list-backups / -rollback / -rollback-to 중 하나만 지정하십시오.")
		os.Exit(1)
	}

	if err := run(opts{
		confPath: *confPath, assetPath: *assetPath, infraName: *infraName,
		onlySite: *onlySite, onlyHost: *onlyHost, hostListPath: *hostListPath, dryRun: *dryRun,
		printScript: *printScript, root: *root, defaultSite: *defaultSite,
		rbMode: rbMode, rbStamp: *rollbackTo,
		remote: remote.Options{
			GosshPath: *gosshPath, User: *user, Password: *password,
			KeyPath: *keyPath, Port: *port, Concurrency: *concurrency,
			TimeoutSec: *timeoutSec, RemotePath: *remotePath,
			Root: *root, DryRun: *dryRun,
		},
	}); err != nil {
		fmt.Fprintln(os.Stderr, "오류:", err)
		os.Exit(1)
	}
}

type opts struct {
	confPath     string
	assetPath    string
	infraName    string
	onlySite     string
	onlyHost     string
	hostListPath string
	dryRun       bool
	printScript  bool
	root         string
	defaultSite  string
	rbMode       render.RollbackMode
	rbStamp      string
	remote       remote.Options
}

func run(o opts) error {
	// 되돌리기는 인프라·사이트 값이 필요 없습니다.
	// 노드에 남아 있는 백업 파일만 보고 판단하므로 설정 파일도 읽지 않습니다.
	if o.rbMode != "" {
		return runRollback(o)
	}

	cfg, err := config.Load(o.confPath)
	if err != nil {
		return fmt.Errorf("설정 파일: %w", err)
	}

	// -default-site 플래그는 conf 의 default_site 를 덮어씁니다.
	// (통합 스크립트가 운영자 conf 를 건드리지 않고 값을 넘길 때 씀)
	if o.defaultSite != "" {
		cfg.DefaultSite = o.defaultSite
	}

	if o.infraName == "" {
		return fmt.Errorf("-infra 를 지정하십시오. 정의된 인프라: %s",
			strings.Join(cfg.InfraNames(), ", "))
	}
	in, ok := cfg.Infras[o.infraName]
	if !ok {
		return fmt.Errorf("인프라 %q 가 설정에 없습니다. 정의된 인프라: %s",
			o.infraName, strings.Join(cfg.InfraNames(), ", "))
	}

	// default_site 는 자산현황에 없는 호스트에 조용히 적용되므로, 값이 틀렸으면
	// 한 대도 건드리기 전에 여기서 멈춰야 합니다.
	if cfg.DefaultSite != "" {
		if _, ok := in.Sites[cfg.DefaultSite]; !ok {
			return fmt.Errorf("default_site %q 가 인프라 %q 에 정의되어 있지 않습니다 (정의된 사이트: %s)",
				cfg.DefaultSite, in.Name, strings.Join(in.SiteNames(), ", "))
		}
	}

	// -print-script 는 자산현황 없이도 동작합니다(스크립트 확인·테스트용).
	if o.printScript {
		if o.onlySite == "" {
			return fmt.Errorf("-print-script 에는 -site 가 필요합니다. 정의된 사이트: %s",
				strings.Join(in.SiteNames(), ", "))
		}
		s, err := render.ApplyScript(in, cfg.S4, o.onlySite)
		if err != nil {
			return err
		}
		fmt.Print(s)
		return nil
	}

	targets, err := loadTargets(o, cfg.DefaultSite, false)
	if err != nil {
		return err
	}

	// 자산현황의 사이트가 설정에 있는지 먼저 전부 확인합니다.
	// 한 대라도 모르는 사이트면 아무것도 건드리지 않고 멈춥니다.
	bySite := asset.GroupBySite(targets)
	sites := make([]string, 0, len(bySite))
	for s := range bySite {
		if _, ok := in.Sites[s]; !ok {
			return fmt.Errorf("자산현황의 사이트 %q 가 인프라 %q 에 정의되어 있지 않습니다 (정의된 사이트: %s)",
				s, in.Name, strings.Join(in.SiteNames(), ", "))
		}
		sites = append(sites, s)
	}
	sort.Strings(sites)

	mode := "적용"
	if o.dryRun {
		mode = "DRY-RUN (실제 변경 없음)"
	}
	fmt.Printf("인프라=%s  대상=%d대  사이트=%s  모드=%s\n",
		in.Name, len(targets), strings.Join(sites, ","), mode)
	if o.root != "" {
		fmt.Printf("경고: -root=%s 로 실제 /etc 가 아닌 곳에 기록합니다 (테스트 모드)\n", o.root)
	}
	fmt.Println(strings.Repeat("-", 70))

	totals := map[string]int{}
	for _, site := range sites {
		hosts := bySite[site]
		sort.Strings(hosts)

		script, err := render.ApplyScript(in, cfg.S4, site)
		if err != nil {
			return err
		}

		hostFile, cleanup, err := remote.WriteHostFile(hosts)
		if err != nil {
			return fmt.Errorf("호스트 목록 임시파일: %w", err)
		}

		fmt.Printf("[%s] %d대 → gossh 전송\n", site, len(hosts))
		cmdline := remote.BuildCommand(script, o.remote)
		results, raw, runErr := remote.Run(hostFile, cmdline, o.remote)
		cleanup()

		if len(results) == 0 {
			fmt.Printf("[%s] gossh 출력이 비어 있습니다.\n", site)
			if runErr != nil {
				fmt.Printf("[%s] gossh 오류: %v\n", site, runErr)
			}
			if strings.TrimSpace(raw) != "" {
				fmt.Println(indent(raw))
			}
			totals["NORESULT"] += len(hosts)
			continue
		}

		reported := map[string]bool{}
		for _, r := range results {
			st := r.Summary()
			reported[r.Host] = true
			totals[st]++
			fmt.Printf("  %-24s %s\n", r.Host, st)
			if st == "FAIL" || st == "NORESULT" {
				for _, l := range r.Lines {
					fmt.Printf("      %s\n", l)
				}
			}
		}
		for _, h := range hosts {
			if !reported[h] {
				totals["UNREACHABLE"]++
				fmt.Printf("  %-24s UNREACHABLE (gossh 응답 없음)\n", h)
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

	if !o.dryRun {
		fmt.Println()
		fmt.Println("★ 적용이 끝났습니다. 대상 서버 중 무작위로 몇 대에 직접 접속해")
		fmt.Println("  설정이 실제로 반영됐는지 반드시 눈으로 확인하십시오.")
	}

	if totals["FAIL"] > 0 || totals["UNREACHABLE"] > 0 || totals["NORESULT"] > 0 {
		os.Exit(2)
	}
	return nil
}

// loadTargets 는 자산현황(-assets)을 유일한 사이트 판정 기준으로 읽고,
// -site / -host / -host-file 필터를 적용합니다.
//
// -host-file 은 "어떤 호스트를 고를지" 만 정할 뿐, site 는 절대 이 파일에서
// 가져오지 않습니다 — site 는 항상 -assets(자산현황) 또는 defaultSite 에서 옵니다.
//
// -host-file 의 호스트가 자산현황에 없으면 아래 순서로 fallback 합니다.
//  1. 이름 그대로 조회
//  2. ev01~ev03 접미사를 떼고 BM 이름으로 재조회 (site 만 BM 것을 쓰고,
//     SSH 대상 이름은 원래 VM 이름을 유지)
//  3. defaultSite 적용 (경고 출력)
//  4. keepUnmatched 면 site 없이 그대로 포함(롤백용), 아니면 건너뜀
func loadTargets(o opts, defaultSite string, keepUnmatched bool) ([]asset.Entry, error) {
	entries, err := asset.Load(o.assetPath)
	if err != nil {
		return nil, fmt.Errorf("자산현황 파일: %w", err)
	}

	var targets []asset.Entry
	for _, e := range entries {
		if o.onlySite != "" && e.Site != o.onlySite {
			continue
		}
		if o.onlyHost != "" && e.Host != o.onlyHost {
			continue
		}
		targets = append(targets, e)
	}

	if o.hostListPath != "" {
		wanted, err := asset.LoadHostList(o.hostListPath)
		if err != nil {
			return nil, fmt.Errorf("작업 대상 목록(-host-file): %w", err)
		}
		// byHost: -site/-host 필터가 걸린 대상 (정확 일치용)
		byHost := map[string]asset.Entry{}
		for _, e := range targets {
			byHost[e.Host] = e
		}
		// byAll: 필터 전 전체 자산현황 (ev 축약 BM 조회용 — 필터를 우회하는
		// 이유는, host-file 이 "이 호스트를 처리하라" 고 이미 명시했기 때문)
		byAll := map[string]asset.Entry{}
		for _, e := range entries {
			byAll[e.Host] = e
		}

		var filtered []asset.Entry
		var viaEV, viaDefault, missing []string
		for _, h := range wanted {
			if e, ok := byHost[h]; ok {
				filtered = append(filtered, e)
				continue
			}
			if base, ok := asset.StripEV(h); ok {
				if e, ok := byAll[base]; ok {
					filtered = append(filtered, asset.Entry{Host: h, Site: e.Site, Line: e.Line})
					viaEV = append(viaEV, h+"→"+base)
					continue
				}
			}
			if defaultSite != "" {
				filtered = append(filtered, asset.Entry{Host: h, Site: defaultSite})
				viaDefault = append(viaDefault, h)
				continue
			}
			if keepUnmatched {
				filtered = append(filtered, asset.Entry{Host: h})
				continue
			}
			missing = append(missing, h)
		}
		if len(viaEV) > 0 {
			fmt.Fprintf(os.Stderr,
				"정보: 자산현황에 VM 이 없어 BM 기준으로 site 판정: %s\n", strings.Join(viaEV, ", "))
		}
		if len(viaDefault) > 0 {
			fmt.Fprintf(os.Stderr,
				"경고: 자산현황에서 못 찾아 default_site(%q) 적용 (%d대): %s\n",
				defaultSite, len(viaDefault), strings.Join(viaDefault, ", "))
		}
		if len(missing) > 0 {
			fmt.Fprintf(os.Stderr,
				"경고: 다음 호스트는 자산현황(%s)에 없고 default_site 도 없어 건너뜁니다: %s\n",
				o.assetPath, strings.Join(missing, ", "))
		}
		targets = filtered
	}

	if len(targets) == 0 {
		return nil, fmt.Errorf("조건에 맞는 대상 호스트가 없습니다")
	}
	return targets, nil
}

// runRollback 은 백업 조회 또는 되돌리기를 수행합니다.
//
// 적용과 달리 사이트별로 스크립트가 갈리지 않으므로 gossh 를 한 번만 호출합니다.
func runRollback(o opts) error {
	script, err := render.RollbackScript(o.rbMode, o.rbStamp)
	if err != nil {
		return err
	}

	// -print-script 는 자산현황 없이도 동작합니다(스크립트 확인·테스트용).
	if o.printScript {
		fmt.Print(script)
		return nil
	}

	// 롤백은 site 가 필요 없으므로, 자산현황에 없는 호스트도 대상에 남깁니다.
	// (적용 때 default_site 로만 포함됐던 호스트가 롤백에서 빠지면, 그 호스트만
	//  원복이 안 되는 구멍이 생깁니다.)
	targets, err := loadTargets(o, "", true)
	if err != nil {
		return err
	}

	hosts := make([]string, 0, len(targets))
	for _, e := range targets {
		hosts = append(hosts, e.Host)
	}
	sort.Strings(hosts)

	switch o.rbMode {
	case render.RollbackList:
		fmt.Printf("백업 시점 조회  대상=%d대\n", len(hosts))
	case render.RollbackLatest:
		fmt.Printf("되돌리기(가장 최근 시점)  대상=%d대  모드=%s\n", len(hosts), modeLabel(o.dryRun))
	case render.RollbackStamp:
		fmt.Printf("되돌리기(시점 %s)  대상=%d대  모드=%s\n", o.rbStamp, len(hosts), modeLabel(o.dryRun))
	}
	if o.root != "" {
		fmt.Printf("경고: -root=%s 로 실제 /etc 가 아닌 곳을 대상으로 합니다 (테스트 모드)\n", o.root)
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
			fmt.Printf("gossh 오류: %v\n", runErr)
		}
		if strings.TrimSpace(raw) != "" {
			fmt.Println(indent(raw))
		}
		return fmt.Errorf("gossh 출력이 비어 있습니다")
	}

	totals := map[string]int{}
	reported := map[string]bool{}
	for _, r := range results {
		reported[r.Host] = true
		st := r.Summary()
		totals[st]++

		if o.rbMode == render.RollbackList {
			// BACKUP|<시점>|<파일들> 줄만 보여줍니다.
			fmt.Printf("  %s\n", r.Host)
			shown := false
			for _, l := range r.Lines {
				if strings.HasPrefix(l, "BACKUP|") {
					f := strings.SplitN(l, "|", 3)
					fmt.Printf("      %s   %s\n", f[1], f[2])
					shown = true
				}
			}
			if !shown {
				fmt.Printf("      (백업 없음)\n")
			}
			continue
		}

		fmt.Printf("  %-24s %s\n", r.Host, st)
		if st == "FAIL" || st == "NORESULT" {
			for _, l := range r.Lines {
				fmt.Printf("      %s\n", l)
			}
		}
	}
	for _, h := range hosts {
		if !reported[h] {
			totals["UNREACHABLE"]++
			fmt.Printf("  %-24s UNREACHABLE (gossh 응답 없음)\n", h)
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

	if o.rbMode != render.RollbackList && !o.dryRun {
		fmt.Println()
		fmt.Println("★ 되돌리기가 끝났습니다. 대상 서버 중 무작위로 몇 대에 직접 접속해")
		fmt.Println("  설정이 실제로 복원됐는지 반드시 눈으로 확인하십시오.")
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
