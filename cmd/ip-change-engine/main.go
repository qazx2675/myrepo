// ip-change-engine 은 관리 노드에서 실행되며, 대상 목록 파일(hostname 변경될ip)에
// 따라 대상 노드들의 IP·게이트웨이를 gossh 로 일괄 변경합니다.
//
// 노드에 직접 접속하지 않고, 대상 전체를 담은 apply 스크립트 하나를 만들어
// gossh 로 밀어넣습니다. 서비스 재시작은 하지 않습니다.
//
// 사용법: ip-change-engine_os8 [옵션] <목록파일>   (옵션은 목록파일 앞뒤 어디에나 둘 수 있음)
package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"ip-change/internal/color"
	"ip-change/internal/config"
	"ip-change/internal/monitor"
	"ip-change/internal/remote"
	"ip-change/internal/render"
	"ip-change/internal/target"
)

const (
	noteNeedPassword   = "키 인증 실패(패스워드 필요) — -p 옵션으로 다시 실행하십시오"
	notePasswordFailed = "키 인증과 패스워드 인증 모두 실패했습니다"
	noteNoResponse     = "gossh 응답 없음"
)

// valueFlags 는 값을 받는 옵션입니다 (옵션 순서 정리에 씁니다).
var valueFlags = map[string]bool{
	"config": true, "targets": true, "gossh": true, "u": true, "i": true,
	"P": true, "c": true, "t": true, "remote-path": true,
	"rollback-to": true, "bond-mode": true,
}

// runOpts 는 명령줄에서 읽은 실행 설정입니다.
type runOpts struct {
	conf        string
	list        string
	rollback    bool
	rollbackTo  string
	check       bool
	fixGateway  bool
	bondMode    string
	askPassword bool
	withMonitor bool
	remote      remote.Options
}

func usage() {
	out := flag.CommandLine.Output()
	fmt.Fprintln(out, "사용법: ip-change-engine_os8 [옵션] <목록파일>")
	fmt.Fprintln(out, "  목록파일: 한 줄에 \"hostname 변경될ip\" (옵션은 목록파일 앞뒤 어디에나 둘 수 있음)")
	fmt.Fprintln(out, "  예)  ip-change-engine_os8 -check list     (바꿀 내용만 보고, 아무것도 쓰지 않음)")
	fmt.Fprintln(out, "       ip-change-engine_os8 list            (IP 변경)")
	fmt.Fprintln(out, "       ip-change-engine_os8 -p list         (키 인증이 안 되는 노드는 패스워드를 물어봄)")
	fmt.Fprintln(out, "       ip-change-engine_os8 -rollback list  (되돌리기)")
	fmt.Fprintln(out, "옵션:")
	flag.PrintDefaults()
}

func main() {
	var (
		confPath   = flag.String("config", "", "설정 파일 경로 (생략 시 ./ip_change.conf, 실행파일 옆 conf/ip_change.conf; 없어도 동작)")
		targetPath = flag.String("targets", "", "대상 목록 파일 경로 (목록파일을 인자로 주는 것과 같음)")

		gosshPath   = flag.String("gossh", "", "gossh 실행 파일 경로 (생략 시 환경변수 IP_CHANGE_GOSSH, 실행파일 옆, PATH 순서로 찾음)")
		user        = flag.String("u", "root", "SSH 접속 계정")
		askPassword = flag.Bool("p", false, "키 인증이 실패한 노드에 쓸 SSH 패스워드를 물어봄 (화면에 보이지 않게 입력)")
		keyPath     = flag.String("i", "", "SSH 키 파일 경로")
		port        = flag.String("P", "22", "SSH 포트")
		concurrency = flag.Int("c", 0, "gossh 동시 접속 수 (0 이면 gossh 기본값)")
		timeoutSec  = flag.Int("t", 0, "gossh 접속 타임아웃 초 (0 이면 gossh 기본값)")
		remotePath  = flag.String("remote-path", "/root/ip_change_apply.sh", "원격에 떨어뜨릴 스크립트 경로")

		check      = flag.Bool("check", false, "바꿀 내용만 보여주고 노드에는 아무것도 쓰지 않음")
		fixGateway = flag.Bool("fix-gateway", false, "예전 게이트웨이가 남은 파일을 묻지 않고 함께 고침")
		bondMode   = flag.String("bond-mode", "", "새로 만드는 bond 의 모드 (active-backup 또는 lacp, 기본 active-backup)")

		rollback   = flag.Bool("rollback", false, "IP 변경을 되돌립니다 (각 노드의 최근 백업 STAMP 복원)")
		rollbackTo = flag.String("rollback-to", "", "되돌릴 백업 STAMP 지정 (미지정 시 가장 최근)")

		withMonitor = flag.Bool("monitor", false, "60초가 지나도 안 끝나면 /dev/tty 에 호스트별 진행 화면을 띄우고, Ctrl+C 는 5초 안에 3번 눌러야 종료 (통합 스크립트가 켬)")
	)
	flag.Usage = usage
	flag.CommandLine.Parse(reorderArgs(os.Args[1:]))

	list := *targetPath
	if list != "" && flag.NArg() > 0 {
		fatal("목록파일을 -targets 와 인자로 함께 지정했습니다. 하나만 지정하십시오.")
	}
	if list == "" {
		switch flag.NArg() {
		case 0:
			fmt.Fprintln(os.Stderr, color.BoldRed("오류: 목록파일을 지정하십시오."))
			usage()
			os.Exit(1)
		case 1:
			list = flag.Arg(0)
		default:
			fatal("목록파일은 하나만 지정할 수 있습니다: " + strings.Join(flag.Args(), " "))
		}
	}
	if *check && *rollback {
		fatal("-check 와 -rollback 은 함께 쓸 수 없습니다.")
	}

	ro := runOpts{
		conf: *confPath, list: list, rollback: *rollback, rollbackTo: *rollbackTo,
		check: *check, fixGateway: *fixGateway, bondMode: *bondMode,
		askPassword: *askPassword, withMonitor: *withMonitor,
		remote: remote.Options{
			GosshPath: *gosshPath, User: *user, KeyPath: *keyPath, Port: *port,
			Concurrency: *concurrency, TimeoutSec: *timeoutSec, RemotePath: *remotePath,
		},
	}
	if err := run(ro); err != nil {
		fatal(err.Error())
	}
}

func fatal(msg string) {
	fmt.Fprintln(os.Stderr, color.BoldRed("오류: "+msg))
	os.Exit(1)
}

// reorderArgs 는 옵션을 앞으로, 옵션이 아닌 인자(목록파일)를 뒤로 모읍니다.
// 표준 flag 패키지는 첫 번째 일반 인자에서 옵션 읽기를 멈추기 때문에,
// "list -check" 처럼 옵션이 목록파일 뒤에 와도 되도록 순서를 바꿉니다.
func reorderArgs(args []string) []string {
	var opts, rest, afterDD []string
	sawDD := false
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			sawDD, afterDD = true, args[i+1:]
			break
		}
		if len(a) < 2 || a[0] != '-' {
			rest = append(rest, a)
			continue
		}
		opts = append(opts, a)
		name := strings.TrimLeft(a, "-")
		if strings.Contains(name, "=") {
			continue
		}
		if valueFlags[name] && i+1 < len(args) {
			i++
			opts = append(opts, args[i])
		}
	}
	if sawDD {
		// "--" 뒤는 모두 위치 인자입니다 (이름이 "-" 로 시작하는 목록파일도 읽을 수 있게 "--" 를 남깁니다)
		opts = append(opts, "--")
		rest = append(rest, afterDD...)
	}
	return append(opts, rest...)
}

// findConfig 는 설정 파일 경로를 정합니다. 지정하지 않으면 ./ip_change.conf,
// 실행파일 옆 conf/ip_change.conf, 실행파일 옆 ip_change.conf 순서로 찾고,
// 아무것도 없으면 "" (기본값으로 동작) 입니다.
func findConfig(explicit string) string {
	if explicit != "" {
		return explicit
	}
	cands := []string{"./ip_change.conf"}
	if exe, err := os.Executable(); err == nil {
		if r, err := filepath.EvalSymlinks(exe); err == nil {
			exe = r
		}
		dir := filepath.Dir(exe)
		cands = append(cands, filepath.Join(dir, "conf", "ip_change.conf"), filepath.Join(dir, "ip_change.conf"))
	}
	for _, c := range cands {
		if st, err := os.Stat(c); err == nil && !st.IsDir() {
			return c
		}
	}
	return ""
}

func run(ro runOpts) error {
	cfg, err := config.Load(findConfig(ro.conf))
	if err != nil {
		return fmt.Errorf("설정 파일: %w", err)
	}
	gossh, err := remote.FindGossh(ro.remote.GosshPath)
	if err != nil {
		return err
	}
	ro.remote.GosshPath = gossh

	if ro.rollback {
		return runRollback(cfg, ro)
	}
	return runApply(cfg, ro)
}

// nodeRow 는 apply 결과 한 줄(노드 한 대)입니다.
type nodeRow struct {
	Host     string // 목록 파일의 hostname (gossh 가 앞에 붙이는 이름)
	NodeHost string // 노드가 스스로 보고한 hostname
	Status   string // OK / WARN / FAIL / CHECK
	OldIP    string
	NewIP    string
	Gateway  string
	Dev      string
	Mode     string
	OS       string
	File     string
	Note     string
	Plan     []string
}

// findResultLine 은 노드 출력에서 마지막 "RESULT|" 줄을 찾습니다.
func findResultLine(lines []string) (string, bool) {
	for i := len(lines) - 1; i >= 0; i-- {
		if strings.HasPrefix(lines[i], "RESULT|") {
			return lines[i], true
		}
	}
	return "", false
}

// noRowNote 는 RESULT 줄이 없는 노드(접속 실패 등)의 사유입니다.
func noRowNote(r remote.Result, reported bool) string {
	switch {
	case r.Note != "":
		return r.Note
	case r.AuthFailed():
		if r.PasswordTried {
			return notePasswordFailed
		}
		return noteNeedPassword
	case len(r.Errs) > 0:
		return strings.Join(r.Errs, " / ")
	case len(r.Lines) > 0:
		return "응답 형식 오류: " + r.Lines[len(r.Lines)-1]
	}
	return noteNoResponse
}

// parseNode 는 노드 한 대의 gossh 출력을 표 한 줄로 바꿉니다.
// apply_body.sh 의 RESULT 줄은 필드 11개이고, FAIL 은 짧게 4개 필드일 수도 있습니다.
func parseNode(host string, r remote.Result, reported bool) nodeRow {
	row := nodeRow{Host: host, NodeHost: host}
	for _, l := range r.Lines {
		if strings.HasPrefix(l, "PLAN|") {
			p := strings.SplitN(l, "|", 3)
			if len(p) == 3 {
				row.Plan = append(row.Plan, p[1]+": "+p[2])
			}
		}
	}
	line, ok := findResultLine(r.Lines)
	if !ok {
		row.Status = "FAIL"
		row.Note = noRowNote(r, reported)
		return row
	}
	f := strings.SplitN(line, "|", 11)
	status := f[1]
	switch status {
	case "OK", "WARN", "FAIL", "CHECK":
	default:
		row.Status = "FAIL"
		row.Note = "알 수 없는 응답: " + line
		return row
	}
	row.Status = status
	if len(f) >= 11 {
		row.NodeHost, row.OldIP, row.NewIP, row.Gateway = f[2], f[3], f[4], f[5]
		row.Dev, row.Mode, row.OS, row.File, row.Note = f[6], f[7], f[8], f[9], f[10]
		return row
	}
	if status == "FAIL" && len(f) >= 4 {
		row.NodeHost = f[2]
		row.Note = strings.Join(f[3:], "|")
		return row
	}
	row.Status = "FAIL"
	row.Note = "알 수 없는 응답: " + line
	return row
}

// noteValue 는 비고("; " 로 구분된 항목)에서 "key=값" 의 값을 꺼냅니다.
func noteValue(note, key string) string {
	for _, part := range strings.Split(note, "; ") {
		part = strings.TrimSpace(part)
		if strings.HasPrefix(part, key+"=") {
			return strings.TrimPrefix(part, key+"=")
		}
	}
	return ""
}

func (n nodeRow) cells() []string {
	ips := "-"
	if n.OldIP != "" || n.NewIP != "" {
		ips = orDash(n.OldIP) + " -> " + orDash(n.NewIP)
	}
	return []string{n.Host, n.Status, ips, orDash(n.Dev), orDash(n.Mode), orDash(n.OS), orDash(n.File), n.Note}
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

// ---- 표 그리기 ----

func runeWidth(r rune) int {
	if r >= 0x1100 && (r <= 0x115f || (r >= 0x2e80 && r <= 0xa4cf) || (r >= 0xac00 && r <= 0xd7a3) ||
		(r >= 0xf900 && r <= 0xfaff) || (r >= 0xfe30 && r <= 0xfe6f) || (r >= 0xff00 && r <= 0xff60) ||
		(r >= 0xffe0 && r <= 0xffe6)) {
		return 2
	}
	return 1
}

// dispWidth 는 터미널에서 차지하는 칸 수입니다 (한글은 2칸).
func dispWidth(s string) int {
	w := 0
	for _, r := range s {
		w += runeWidth(r)
	}
	return w
}

func padTo(s string, w int) string {
	if d := w - dispWidth(s); d > 0 {
		return s + strings.Repeat(" ", d)
	}
	return s
}

func colorStatus(s string) string {
	switch strings.TrimSpace(s) {
	case "OK":
		return color.Green(s)
	case "WARN":
		return color.Yellow(s)
	case "FAIL":
		return color.BoldRed(s)
	case "CHECK":
		return color.Cyan(s)
	}
	return s
}

// tableText 는 머리글과 행으로 표를 만듭니다. 마지막 칸은 채우지 않습니다.
// statusCol 칸은 colorize 가 true 일 때만 색을 입힙니다.
func tableText(head []string, rows [][]string, statusCol int, colorize bool) string {
	w := make([]int, len(head))
	for i, h := range head {
		w[i] = dispWidth(h)
	}
	for _, r := range rows {
		for i, c := range r {
			if dw := dispWidth(c); dw > w[i] {
				w[i] = dw
			}
		}
	}
	line := func(cells []string, isHead bool) string {
		var b strings.Builder
		for i, c := range cells {
			if i > 0 {
				b.WriteString(" | ")
			}
			if i == len(cells)-1 {
				b.WriteString(c)
				break
			}
			p := padTo(c, w[i])
			if colorize && !isHead && i == statusCol {
				p = colorStatus(p)
			}
			b.WriteString(p)
		}
		return b.String()
	}
	var b strings.Builder
	b.WriteString(line(head, true))
	b.WriteString("\n")
	total := 0
	for _, x := range w {
		total += x + 3
	}
	b.WriteString(strings.Repeat("-", total-3))
	b.WriteString("\n")
	for _, r := range rows {
		b.WriteString(line(r, false))
		b.WriteString("\n")
	}
	return b.String()
}

var applyHead = []string{"hostname", "결과", "기존IP -> 새IP", "장치", "방식", "OS", "설정파일", "비고"}

// ---- apply ----

func runApply(cfg *config.Config, ro runOpts) error {
	entries, err := target.Load(ro.list)
	if err != nil {
		return fmt.Errorf("대상 목록 파일: %w", err)
	}
	if _, err := render.NormalizeBondMode(ro.bondMode); err != nil {
		return err
	}

	hosts := make([]string, 0, len(entries))
	for _, e := range entries {
		hosts = append(hosts, e.Host)
	}
	sort.Strings(hosts)

	mode := "IP 변경"
	if ro.check {
		mode = "점검(-check, 아무것도 쓰지 않음)"
	}
	header := fmt.Sprintf("%s  대상=%d대  목록=%s", mode, len(entries), ro.list)
	fmt.Println(color.BoldCyan(header))
	if forced := render.ForcedDir(cfg); forced != "" {
		fmt.Println(color.Cyan("강제 경로(network_scripts_dir)=" + forced))
	}
	if cfg.RHEL9Path != "" {
		fmt.Println(color.Cyan("RHEL9 이상 경로(rhel9_path)=" + cfg.RHEL9Path))
	}
	fmt.Println(color.Cyan("작업 대상:"))
	for _, e := range entries {
		fmt.Printf("  %-20s -> %s\n", e.Host, e.NewIP)
	}
	fmt.Println(strings.Repeat("-", 60))

	script, err := render.ApplyScriptWithOptions(entries, cfg, render.ApplyOptions{CheckOnly: ro.check, BondMode: ro.bondMode})
	if err != nil {
		return err
	}

	runner := &remote.Runner{Opts: ro.remote, AskPassword: ro.askPassword}
	if ro.withMonitor {
		runner.Opts.Monitor = monitor.New("IP 변경", hosts, ipMonitorLabel, os.Stdout)
		runner.Opts.Monitor.Start()
	}
	results, raw, runErr := runner.Run(hosts, script)
	runner.Opts.Monitor.Stop() // 아래에서 os.Exit 할 수 있으므로 defer 가 아니라 여기서 화면 원복
	runner.Opts.Monitor = nil

	if len(results) == 0 {
		if runErr != nil {
			fmt.Println(color.BoldRed(fmt.Sprintf("gossh 오류: %v", runErr)))
		}
		if strings.TrimSpace(raw) != "" {
			fmt.Println(indent(raw))
		}
	}

	rows := buildRows(hosts, results)
	rep := applyReport{header: header, rows: rows, check: ro.check}
	fmt.Print(rep.text(true))

	// 예전 게이트웨이 고침 (수정계획서 4.8)
	gw := maybeFixGateway(runner, rows, ro)
	if len(gw) > 0 {
		fmt.Println()
		fmt.Print(gwText(gw, true))
	}

	path := "./" + resultFileName(ro.check, time.Now())
	if err := os.WriteFile(path, []byte(rep.text(false)+gwTextOrEmpty(gw)), 0o644); err != nil {
		fmt.Println(color.Yellow("결과 파일을 쓰지 못했습니다: " + err.Error()))
	} else {
		fmt.Println("결과 파일: " + path)
	}

	if rep.count("FAIL") > 0 || gwFailed(gw) {
		os.Exit(2)
	}
	return nil
}

func resultFileName(check bool, t time.Time) string {
	prefix := "ip_change_result_"
	if check {
		prefix = "ip_change_check_"
	}
	return prefix + t.Format("20060102_150405") + ".txt"
}

func buildRows(hosts []string, results []remote.Result) []nodeRow {
	byHost := map[string]remote.Result{}
	for _, r := range results {
		byHost[r.Host] = r
	}
	rows := make([]nodeRow, 0, len(hosts))
	for _, h := range hosts {
		r, reported := byHost[h]
		rows = append(rows, parseNode(h, r, reported))
	}
	return rows
}

type applyReport struct {
	header string
	rows   []nodeRow
	check  bool
}

func (a applyReport) count(status string) int {
	n := 0
	for _, r := range a.rows {
		if r.Status == status {
			n++
		}
	}
	return n
}

// text 는 표 + (점검 시) 계획 + 요약 문자열입니다. colorize 가 false 면 결과 파일용입니다.
func (a applyReport) text(colorize bool) string {
	var b strings.Builder
	cells := make([][]string, 0, len(a.rows))
	for _, r := range a.rows {
		cells = append(cells, r.cells())
	}
	if !colorize {
		fmt.Fprintf(&b, "%s\n실행 시각: %s\n\n", a.header, time.Now().Format("2006-01-02 15:04:05"))
	}
	b.WriteString(tableText(applyHead, cells, 1, colorize))

	if a.check {
		b.WriteString("\n변경 계획 (-check: 노드에는 아무것도 쓰지 않았습니다)\n")
		any := false
		for _, r := range a.rows {
			if len(r.Plan) == 0 {
				continue
			}
			any = true
			fmt.Fprintf(&b, "[%s]\n", r.Host)
			for _, p := range r.Plan {
				b.WriteString("  " + p + "\n")
			}
		}
		if !any {
			b.WriteString("  (계획 없음)\n")
		}
	}

	b.WriteString("\n")
	lab := func(name string, n int, paint func(string) string) string {
		s := fmt.Sprintf("%s %d", name, n)
		if colorize {
			return paint(s)
		}
		return s
	}
	parts := []string{fmt.Sprintf("총 %d대", len(a.rows)), lab("OK", a.count("OK"), color.Green),
		lab("WARN", a.count("WARN"), color.Yellow), lab("FAIL", a.count("FAIL"), color.BoldRed)}
	if a.check {
		parts = append(parts, lab("CHECK", a.count("CHECK"), color.Cyan))
	}
	b.WriteString(strings.Join(parts, "   ") + "\n")
	return b.String()
}

// ---- 예전 게이트웨이 고침 ----

// gwRow 는 게이트웨이 고침 결과 한 줄입니다.
type gwRow struct {
	Host, Status, Files, Stamp, Note string
}

var gwHead = []string{"hostname", "결과", "고친 파일", "STAMP", "비고"}

func gwFailed(rows []gwRow) bool {
	for _, r := range rows {
		if r.Status == "FAIL" {
			return true
		}
	}
	return false
}

func gwText(rows []gwRow, colorize bool) string {
	cells := make([][]string, 0, len(rows))
	for _, r := range rows {
		cells = append(cells, []string{r.Host, r.Status, orDash(r.Files), orDash(r.Stamp), r.Note})
	}
	return "예전 게이트웨이 고침 결과\n" + tableText(gwHead, cells, 1, colorize)
}

func gwTextOrEmpty(rows []gwRow) string {
	if len(rows) == 0 {
		return ""
	}
	return "\n" + gwText(rows, false)
}

// gwCandidates 는 비고에 BACKUP/OLDGW/GWFIX 가 모두 있는 노드를 고칠 대상으로 모읍니다.
func gwCandidates(rows []nodeRow) ([]render.GatewayFix, []string) {
	var items []render.GatewayFix
	var hosts []string
	for _, r := range rows {
		if r.Status != "OK" && r.Status != "WARN" {
			continue
		}
		oldGW, files, stamp := noteValue(r.Note, "OLDGW"), noteValue(r.Note, "GWFIX"), noteValue(r.Note, "BACKUP")
		if oldGW == "" || files == "" || stamp == "" || r.Gateway == "" {
			continue
		}
		items = append(items, render.GatewayFix{
			Host: r.NodeHost, OldGW: oldGW, NewGW: r.Gateway,
			Files: strings.Split(files, ","), Stamp: stamp,
		})
		hosts = append(hosts, r.Host)
	}
	return items, hosts
}

func stdinIsTerminal() bool {
	st, err := os.Stdin.Stat()
	return err == nil && st.Mode()&os.ModeCharDevice != 0
}

// maybeFixGateway 는 4.8 의 질문/실행을 처리하고, 실행했으면 결과 표 행을 돌려줍니다.
func maybeFixGateway(runner *remote.Runner, rows []nodeRow, ro runOpts) []gwRow {
	if ro.check {
		return nil
	}
	items, hosts := gwCandidates(rows)
	if len(items) == 0 {
		return nil
	}
	if !ro.fixGateway {
		if !stdinIsTerminal() {
			return nil
		}
		fmt.Println()
		fmt.Println(color.Yellow("예전 게이트웨이가 남은 파일이 있습니다:"))
		for _, it := range items {
			fmt.Printf("  %-20s %s -> %s\n", it.Host, it.OldGW, it.NewGW)
			for _, f := range it.Files {
				fmt.Printf("      %s\n", f)
			}
		}
		fmt.Print("이 파일들의 옛 게이트웨이도 함께 바꿀까요? [y/N] ")
		ans, _ := bufio.NewReader(os.Stdin).ReadString('\n')
		ans = strings.ToLower(strings.TrimSpace(ans))
		if ans != "y" && ans != "yes" {
			return nil
		}
	}

	script, err := render.FixGatewayScript(items)
	if err != nil {
		return []gwRow{{Host: "-", Status: "FAIL", Note: err.Error()}}
	}
	sort.Strings(hosts)
	results, raw, runErr := runner.Run(hosts, script)
	if len(results) == 0 {
		if runErr != nil {
			fmt.Println(color.BoldRed(fmt.Sprintf("gossh 오류: %v", runErr)))
		}
		if strings.TrimSpace(raw) != "" {
			fmt.Println(indent(raw))
		}
	}
	byHost := map[string]remote.Result{}
	for _, r := range results {
		byHost[r.Host] = r
	}
	var out []gwRow
	for _, h := range hosts {
		out = append(out, parseGWResult(h, byHost[h]))
	}
	return out
}

// parseGWResult 는 fixgw_body.sh 의 결과 줄을 읽습니다.
//
//	RESULT|OK|host|files|STAMP   /   RESULT|FAIL|host|reason
func parseGWResult(host string, r remote.Result) gwRow {
	line, ok := findResultLine(r.Lines)
	if !ok {
		return gwRow{Host: host, Status: "FAIL", Note: noRowNote(r, true)}
	}
	f := strings.Split(line, "|")
	switch {
	case len(f) >= 5 && f[1] == "OK":
		note := ""
		if f[3] == "" {
			note = "고칠 내용 없음"
		}
		return gwRow{Host: host, Status: "OK", Files: f[3], Stamp: f[4], Note: note}
	case len(f) >= 4 && f[1] == "FAIL":
		return gwRow{Host: host, Status: "FAIL", Note: strings.Join(f[3:], "|")}
	}
	return gwRow{Host: host, Status: "FAIL", Note: "알 수 없는 응답: " + line}
}

// ---- rollback ----

type rbRow struct {
	Host, Status, Stamp, Restored, Removed, Note string
}

var rbHead = []string{"hostname", "결과", "STAMP", "복원", "제거", "비고"}

// parseRollback 는 rollback_body.sh 의 결과 줄을 읽습니다.
//
//	새 방식: RESULT|OK|host|STAMP|복원수|제거수|비고
//	옛 방식: RESULT|OK|host|ifcfg|STAMP|IPADDR
//	실패   : RESULT|FAIL|host|사유
func parseRollback(host string, r remote.Result, reported bool) rbRow {
	line, ok := findResultLine(r.Lines)
	if !ok {
		return rbRow{Host: host, Status: "FAIL", Stamp: "-", Restored: "-", Removed: "-", Note: noRowNote(r, reported)}
	}
	f := strings.Split(line, "|")
	if len(f) >= 4 && f[1] == "FAIL" {
		return rbRow{Host: f[2], Status: "FAIL", Stamp: "-", Restored: "-", Removed: "-", Note: strings.Join(f[3:], "|")}
	}
	if len(f) >= 7 && f[1] == "OK" {
		return rbRow{Host: f[2], Status: "OK", Stamp: f[3], Restored: f[4], Removed: f[5], Note: strings.Join(f[6:], "|")}
	}
	if len(f) == 6 && f[1] == "OK" {
		return rbRow{Host: f[2], Status: "OK", Stamp: f[4], Restored: "1", Removed: "0",
			Note: fmt.Sprintf("옛 방식: %s (IPADDR=%s)", f[3], f[5])}
	}
	return rbRow{Host: host, Status: "FAIL", Stamp: "-", Restored: "-", Removed: "-", Note: "알 수 없는 응답: " + line}
}

// runRollback 은 대상 노드에서 최근(또는 지정 STAMP) 백업을 되돌립니다.
func runRollback(cfg *config.Config, ro runOpts) error {
	hosts, err := target.LoadHosts(ro.list)
	if err != nil {
		return fmt.Errorf("대상 목록 파일: %w", err)
	}
	sort.Strings(hosts)

	to := ro.rollbackTo
	if to == "" {
		to = "(가장 최근)"
	}
	fmt.Println(color.BoldCyan(fmt.Sprintf("롤백 대상=%d대  STAMP=%s", len(hosts), to)))
	fmt.Println(strings.Repeat("-", 60))

	script, err := render.RollbackScript(cfg, ro.rollbackTo)
	if err != nil {
		return err
	}
	runner := &remote.Runner{Opts: ro.remote, AskPassword: ro.askPassword}
	results, raw, runErr := runner.Run(hosts, script)
	if len(results) == 0 {
		if runErr != nil {
			fmt.Println(color.BoldRed(fmt.Sprintf("gossh 오류: %v", runErr)))
		}
		if strings.TrimSpace(raw) != "" {
			fmt.Println(indent(raw))
		}
	}
	byHost := map[string]remote.Result{}
	for _, r := range results {
		byHost[r.Host] = r
	}

	var cells [][]string
	okCount, failCount := 0, 0
	for _, h := range hosts {
		r, reported := byHost[h]
		row := parseRollback(h, r, reported)
		if row.Status == "OK" {
			okCount++
		} else {
			failCount++
		}
		cells = append(cells, []string{h, row.Status, row.Stamp, row.Restored, row.Removed, row.Note})
	}
	fmt.Print(tableText(rbHead, cells, 1, true))
	fmt.Println()
	fmt.Printf("총 %d대   %s %d   %s %d\n", len(hosts), color.Green("OK"), okCount, color.BoldRed("FAIL"), failCount)

	if failCount > 0 {
		os.Exit(2)
	}
	return nil
}

// ipMonitorLabel 은 진행 화면에 보일 호스트 상태입니다(RESULT 줄 기준).
func ipMonitorLabel(lines []string) (string, bool) {
	line, ok := findResultLine(lines)
	if !ok {
		return "실패", true
	}
	f := strings.SplitN(line, "|", 3)
	switch f[1] {
	case "OK":
		return "완료(OK)", false
	case "WARN":
		return "완료(WARN)", false
	case "CHECK":
		return "점검 완료", false
	}
	return "실패", true
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
