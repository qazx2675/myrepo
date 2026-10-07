package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// report.go 는 check 의 터미널 리포트(계획서 4장 형식)와 Y/N 확인 프롬프트입니다.
// 출력에는 호스트·모델·속성·값만 나오며 비밀번호·토큰은 어디에도 없습니다.

// reportOpts 는 리포트 출력 옵션입니다.
type reportOpts struct {
	ListMax int  // [OK] 호스트 이름을 나열할 최대 대수 (넘으면 개수 + ok.txt 경로)
	FailMax int  // [FAIL 상세]·[설정 불가] 에 보여 줄 최대 줄 수 (넘으면 fail.tsv 안내)
	Color   bool // ANSI 색 (TTY 이고 NO_COLOR 가 없을 때)
}

// okListMaxLines 는 [OK] 이름 나열이 이 줄 수 이상이 되면 개수와 파일 경로만 보여 줍니다.
const okListMaxLines = 20

// reportWidth 는 [OK] 이름을 이어 붙이는 한 줄의 폭입니다.
const reportWidth = 100

// painter 는 의존성 없는 ANSI 색입니다. 꺼져 있으면 문자열을 그대로 돌려줍니다.
type painter struct{ on bool }

func (p painter) paint(code, s string) string {
	if !p.on {
		return s
	}
	return "\x1b[" + code + "m" + s + "\x1b[0m"
}

func (p painter) green(s string) string   { return p.paint("32", s) }
func (p painter) red(s string) string     { return p.paint("31", s) }
func (p painter) boldRed(s string) string { return p.paint("1;31", s) }
func (p painter) yellow(s string) string  { return p.paint("33", s) }
func (p painter) cyan(s string) string    { return p.paint("36", s) }

// useColor 는 표준출력이 터미널이고 NO_COLOR/TERM=dumb 가 아닐 때만 true 입니다.
func useColor() bool {
	if os.Getenv("NO_COLOR") != "" || os.Getenv("TERM") == "dumb" {
		return false
	}
	fi, err := os.Stdout.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}

// commaInt 는 1180 → "1,180" 입니다.
func commaInt(n int) string {
	s := fmt.Sprint(n)
	neg := strings.HasPrefix(s, "-")
	if neg {
		s = s[1:]
	}
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	if neg {
		s = "-" + s
	}
	return s
}

// wrapNames 는 이름을 공백 두 칸으로 이어 width 안에서 줄을 나눕니다.
func wrapNames(names []string, indent string, width int) []string {
	var lines []string
	cur := indent
	for _, n := range names {
		if strings.TrimSpace(cur) != "" && len(cur)+2+len(n) > width {
			lines = append(lines, cur)
			cur = indent
		}
		if strings.TrimSpace(cur) != "" {
			cur += "  "
		}
		cur += n
	}
	if strings.TrimSpace(cur) != "" {
		lines = append(lines, cur)
	}
	return lines
}

func modelLabel(info *SystemInfo) string {
	if info == nil {
		return "-"
	}
	return strings.TrimSpace(info.Vendor + " " + info.Model)
}

// otherErrOrder 는 [기타 오류] 의 상태 표시 순서입니다 (여기에 없는 상태는 뒤에 이름순).
var otherErrOrder = []string{
	StatusUnreachable, StatusTimeout, StatusBMCError, StatusAuthFail, StatusSkippedAuthStop, StatusUnsupported,
	StatusNoHostsEntry, StatusHTTPError, StatusNoDump,
}

// reportOrder 는 상단 집계의 호스트 상태 표시 순서입니다.
var reportOrder = []string{
	StatusOK, StatusPendingOK, StatusFail, StatusPendingNoJob, StatusUnverified, StatusPendingExists, StatusMappingMissing,
}

// writeReport 는 계획서 4장 형식의 리포트를 출력합니다.
func writeReport(w io.Writer, run *checkRun, o reportOpts) {
	p := painter{o.Color}
	hosts, _ := run.tally()
	total := len(run.Hosts)
	fmt.Fprintf(w, "== BIOS 점검 결과 (profile=%s, 대상 %s / %s) ==\n",
		run.Profile, commaInt(total), run.Now.Format("2006-01-02 15:04"))

	known := map[string]bool{}
	for _, st := range reportOrder {
		known[st] = true
		n := hosts[st]
		if n == 0 && st != StatusOK && st != StatusPendingOK && st != StatusFail {
			continue
		}
		line := fmt.Sprintf("%-16s %7s", st, commaInt(n))
		switch {
		case st == StatusOK && n > 0:
			line = p.green(line)
		case st == StatusFail && n > 0:
			line = p.red(line)
		case n > 0 && st != StatusOK && st != StatusPendingOK:
			line = p.yellow(line)
		}
		if st == StatusOK {
			line += "  → " + filepath.ToSlash(filepath.Join(run.Dir, "ok.txt")) + " (OK·PENDING_OK 호스트 이름)"
		}
		fmt.Fprintln(w, line)
	}
	var otherN int
	var otherParts []string
	for _, st := range otherStatuses(hosts, known) {
		otherN += hosts[st]
		otherParts = append(otherParts, fmt.Sprintf("%s %d", st, hosts[st]))
	}
	if otherN > 0 {
		fmt.Fprintln(w, p.yellow(fmt.Sprintf("%-16s %7s", "기타 오류", commaInt(otherN)))+"  ("+strings.Join(otherParts, ", ")+")")
	}
	if run.FromDump != "" {
		fmt.Fprintln(w, p.cyan("(덤프 읽기 모드: BMC 에 접속하지 않았고, 설정도 할 수 없습니다)"))
	}
	if warn := authStopWarning(hosts[StatusAuthFail], hosts[StatusSkippedAuthStop], authStopRerunCheck); warn != "" {
		fmt.Fprintln(w, p.boldRed("!! "+warn))
	}
	if n := dellJobUnknown(run); n > 0 {
		fmt.Fprintln(w, p.yellow(fmt.Sprintf("!! Dell PENDING_OK %d건은 BIOS 설정 Job 을 확인하지 못했습니다. Dell 은 iDRAC Job Queue 에 BIOS 설정 Job 이 없으면 재부팅해도 반영되지 않을 수 있음 — 확인 필요", n)))
	}

	writeOKSection(w, run, o, p)
	writeFailSection(w, run, o, p)
	writeBlockedSection(w, run, o, p)
	writeOtherSection(w, run, p)

	fmt.Fprintln(w)
	fmt.Fprintf(w, "%s 결과 파일: %s/  (result.tsv 전체, fail.tsv FAIL·설정 불가 행, retry.txt 재시도 대상, run_info.txt)\n",
		p.cyan("*"), filepath.ToSlash(run.Dir))
}

// dellJobUnknown 은 BIOS 설정 Job 존재를 확인하지 못한 Dell PENDING_OK 항목 수입니다.
func dellJobUnknown(run *checkRun) int {
	n := 0
	for _, h := range run.Hosts {
		for _, it := range h.Items {
			if it.Result == StatusPendingOK && it.JobUnknown {
				n++
			}
		}
	}
	return n
}

// otherStatuses 는 reportOrder 에 없는 호스트 상태를 표시 순서대로 돌려줍니다.
func otherStatuses(hosts map[string]int, known map[string]bool) []string {
	var out []string
	seen := map[string]bool{}
	for _, st := range otherErrOrder {
		if hosts[st] > 0 {
			out = append(out, st)
			seen[st] = true
		}
	}
	var rest []string
	for st := range hosts {
		if !known[st] && !seen[st] && hosts[st] > 0 {
			rest = append(rest, st)
		}
	}
	sort.Strings(rest)
	return append(out, rest...)
}

func writeOKSection(w io.Writer, run *checkRun, o reportOpts, p painter) {
	var names []string
	for _, h := range run.Hosts {
		if h.Status == "" && h.allGood() {
			n := h.Target.Hostname
			if h.hasStatus(StatusPendingOK) {
				n += "(재부팅 대기)"
			}
			names = append(names, n)
		}
	}
	if len(names) == 0 {
		return
	}
	fmt.Fprintln(w)
	fmt.Fprintf(w, "%s %s대\n", p.green("[OK]"), commaInt(len(names)))
	var lines []string
	if len(names) <= o.ListMax {
		lines = wrapNames(names, "  ", reportWidth)
	}
	if lines == nil || len(lines) >= okListMaxLines {
		fmt.Fprintf(w, "  호스트가 많아 이름은 생략합니다 → %s\n", filepath.ToSlash(filepath.Join(run.Dir, "ok.txt")))
		return
	}
	for _, l := range lines {
		fmt.Fprintln(w, l)
	}
}

// padTo 는 s 를 최소 n 칸(ASCII 기준)으로 맞춥니다.
func padTo(s string, n int) string {
	if len(s) >= n {
		return s
	}
	return s + strings.Repeat(" ", n-len(s))
}

// modelWidth 는 모델 표시(벤더 + 모델)의 최대 폭입니다 (열 맞춤용, 상한 40).
func modelWidth(run *checkRun) int {
	n := 0
	for _, h := range run.Hosts {
		if h.Status == "" {
			if l := len(modelLabel(h.Info)); l > n {
				n = l
			}
		}
	}
	if n > 40 {
		n = 40
	}
	return n
}

func hostNameWidth(run *checkRun) int {
	n := 0
	for _, h := range run.Hosts {
		if l := len(h.Target.Hostname); l > n {
			n = l
		}
	}
	if n > 40 {
		n = 40
	}
	return n
}

func writeFailSection(w io.Writer, run *checkRun, o reportOpts, p painter) {
	mw := modelWidth(run)
	type row struct{ host, label string }
	var rows []row
	hostSet := map[string]bool{}
	for _, h := range run.Hosts {
		if h.Status != "" {
			continue
		}
		for _, it := range h.Items {
			if !h.settable(it) {
				continue
			}
			hostSet[h.Target.Hostname] = true
			extra := ""
			if it.Result == StatusPendingNoJob {
				extra = "  " + StatusPendingNoJob + "(Pending=기대값, BIOS 설정 Job 없음 → Y 면 같은 값 PATCH + Job)"
			}
			rows = append(rows, row{h.Target.Hostname, fmt.Sprintf("%s  %-16s %s  기대=%s  현재=%s%s",
				padTo(modelLabel(h.Info), mw), it.Std, it.Attr, it.Expected, it.Current, extra)})
		}
	}
	if len(rows) == 0 {
		return
	}
	fmt.Fprintln(w)
	fmt.Fprintf(w, "%s %d건 (호스트 %d대)\n", p.red("[FAIL 상세]"), len(rows), len(hostSet))
	width := hostNameWidth(run)
	for i, r := range rows {
		if i == o.FailMax {
			fmt.Fprintf(w, "  ... 외 %d건 → %s\n", len(rows)-i, filepath.ToSlash(filepath.Join(run.Dir, "fail.tsv")))
			break
		}
		fmt.Fprintf(w, "  %s  %s\n", padTo(r.host, width), r.label)
	}
}

// writeBlockedSection 은 UNVERIFIED / PENDING_EXISTS / MAPPING_MISSING / (설정 불가) PENDING_NO_JOB 항목을 사유와 함께 보여 줍니다.
func writeBlockedSection(w io.Writer, run *checkRun, o reportOpts, p painter) {
	mw := modelWidth(run)
	type row struct{ host, text string }
	var rows []row
	hostSet := map[string]bool{}
	for _, h := range run.Hosts {
		if h.Status != "" {
			continue
		}
		for _, it := range h.Items {
			var text string
			switch it.Result {
			case StatusUnverified:
				text = fmt.Sprintf("%s  %-16s %s  UNVERIFIED  기대=%s  현재=%s  (%s)", padTo(modelLabel(h.Info), mw), it.Std, it.Attr, it.Expected, it.Current, it.Reason)
			case StatusPendingExists:
				extra := ""
				if h.Jobs >= 0 {
					extra = fmt.Sprintf(", Job %d건(정보용)", h.Jobs)
				}
				text = fmt.Sprintf("%s  %-16s %s  PENDING_EXISTS  기대=%s  현재=%s  (%s%s)", padTo(modelLabel(h.Info), mw), it.Std, it.Attr, it.Expected, it.Current, it.Reason, extra)
			case StatusMappingMissing:
				text = fmt.Sprintf("%s  %-16s MAPPING_MISSING  (%s)  %s", padTo(modelLabel(h.Info), mw), it.Std, it.Reason, candidateText(h, it.Std))
			case StatusPendingNoJob:
				if h.settable(it) {
					continue // [FAIL 상세] 에 나온다
				}
				why := "verified=N"
				if h.Foreign > 0 {
					why = fmt.Sprintf("다른 속성 Pending %d건", h.Foreign)
				}
				text = fmt.Sprintf("%s  %-16s %s  PENDING_NO_JOB  기대=%s  현재=%s  (%s; %s 이라 설정 안 함)", padTo(modelLabel(h.Info), mw), it.Std, it.Attr, it.Expected, it.Current, it.Reason, why)
			default:
				continue
			}
			hostSet[h.Target.Hostname] = true
			rows = append(rows, row{h.Target.Hostname, strings.TrimRight(text, " ")})
		}
	}
	if len(rows) == 0 {
		return
	}
	fmt.Fprintln(w)
	fmt.Fprintf(w, "%s %d건 (호스트 %d대)\n", p.yellow("[설정 불가]"), len(rows), len(hostSet))
	width := hostNameWidth(run)
	for i, r := range rows {
		if i == o.FailMax {
			fmt.Fprintf(w, "  ... 외 %d건 → %s\n", len(rows)-i, filepath.ToSlash(filepath.Join(run.Dir, "fail.tsv")))
			break
		}
		fmt.Fprintf(w, "  %s  %s\n", padTo(r.host, width), r.text)
	}
	if run.MMModels > 0 {
		fmt.Fprintf(w, "  %s MAPPING_MISSING 모델 %d종의 덤프·요약: %s (summary.txt 를 보고 프로파일에 행을 추가하십시오)\n",
			p.cyan("*"), run.MMModels, filepath.ToSlash(filepath.Join(run.Dir, "mapping_missing")))
	} else if run.FromDump != "" {
		for _, h := range run.Hosts {
			if h.hasStatus(StatusMappingMissing) {
				fmt.Fprintf(w, "  %s 덤프 읽기 모드라 MAPPING_MISSING 덤프는 저장하지 않습니다\n", p.cyan("*"))
				break
			}
		}
	}
}

// candidateText 는 MAPPING_MISSING 항목의 `후보: Attr=값, ...` 입니다 (판정하지 않고 알려 주기만 함).
func candidateText(h *hostCheck, std string) string {
	for _, m := range h.Cands {
		if m.Std != std {
			continue
		}
		if len(m.Cands) == 0 {
			return "후보: 없음"
		}
		var parts []string
		for i, c := range m.Cands {
			if i == maxShownCands-2 {
				parts = append(parts, fmt.Sprintf("... 외 %d개", len(m.Cands)-i))
				break
			}
			s := c.Name + "=" + clip(c.Value, 40)
			if c.Reg != nil {
				if len(c.Reg.Values) > 0 {
					vals := c.Reg.Values
					if len(vals) > 6 {
						vals = append(append([]string(nil), vals[:6]...), "...")
					}
					s += " [허용값 " + strings.Join(vals, "|") + "]"
				}
				if c.Reg.ReadOnly {
					s += " [ReadOnly]"
				}
			}
			parts = append(parts, s)
		}
		return "후보: " + strings.Join(parts, ", ")
	}
	return ""
}

func writeOtherSection(w io.Writer, run *checkRun, p painter) {
	hosts, _ := run.tally()
	known := map[string]bool{}
	for _, st := range reportOrder {
		known[st] = true
	}
	sts := otherStatuses(hosts, known)
	if len(sts) == 0 {
		return
	}
	fmt.Fprintln(w)
	fmt.Fprintln(w, p.yellow("[기타 오류]"))
	retry := 0
	for _, st := range sts {
		var names []string
		detail := ""
		for _, h := range run.Hosts {
			if h.Status == st {
				names = append(names, h.Target.Hostname)
				if detail == "" && h.Detail != "" {
					detail = h.Detail
				}
			}
		}
		if isRetryStatus(st) {
			retry += len(names)
		}
		shown := names
		more := ""
		if len(shown) > 10 {
			more = fmt.Sprintf(" ... 외 %d대", len(shown)-10)
			shown = shown[:10]
		}
		fmt.Fprintf(w, "  %-14s %s대  %s%s\n", st, commaInt(len(names)), strings.Join(shown, " "), more)
		switch st {
		case StatusAuthFail:
			fmt.Fprintln(w, "    계정 잠금을 막으려고 재시도하지 않았습니다. 계정/비밀번호를 확인한 뒤 해당 대상만 다시 실행하십시오.")
		case StatusSkippedAuthStop:
			fmt.Fprintln(w, "    AUTH_FAIL 차단기(auth_fail_stop)가 작동해 접속하지 않았습니다. 계정/비밀번호를 확인한 뒤 retry.txt 로 다시 실행하십시오.")
		case StatusHTTPError, StatusUnsupported:
			if detail != "" {
				fmt.Fprintf(w, "    예: %s\n", detail)
			}
		case StatusNoHostsEntry:
			fmt.Fprintln(w, "    /etc/hosts 에 이름-m 항목이 없습니다 (-hosts 로 다른 파일 지정 가능).")
		case StatusNoDump:
			fmt.Fprintln(w, "    -from-dump 폴더에 호스트 이름과 같은 덤프 폴더가 없습니다.")
		}
	}
	if retry > 0 {
		fmt.Fprintf(w, "  %s 재시도 대상 %d대 → %s (그대로 -user 로 넘겨 다시 실행할 수 있습니다)\n",
			p.cyan("*"), retry, filepath.ToSlash(filepath.Join(run.Dir, "retry.txt")))
	}
}

// failItem 은 Pending 으로 설정할 FAIL 항목 1개입니다 (단계 6 의 입력).
type failItem struct {
	Target   Target
	Info     *SystemInfo // Settings 경로·Manager 경로 등 설정에 필요한 정보
	Std      string
	Attr     string // BMC 의 실제 속성 이름
	Expected string // 프로파일 value 원문 (A|B 면 첫 값이 설정값)
	Current  string
}

// failItems 는 설정 가능한(FAIL, 설정 가능한 PENDING_NO_JOB) 항목을 호스트 입력 순서대로 모읍니다.
func (r *checkRun) failItems() []failItem {
	var out []failItem
	for _, h := range r.Hosts {
		if h.Status != "" {
			continue
		}
		for _, it := range h.Items {
			if h.settable(it) {
				out = append(out, failItem{Target: h.Target, Info: h.Info, Std: it.Std, Attr: it.Attr, Expected: it.Expected, Current: it.Current})
			}
		}
	}
	return out
}

// applyFails 는 승인된 FAIL 항목을 Pending 으로 설정하는 후크입니다 (구현은 set.go 의 doApply).
// 테스트가 바꿔 쓸 수 있도록 변수입니다.
var applyFails = doApply

// confirmApply 는 FAIL(설정 가능)이 있으면 Y/N 을 묻고, Y 일 때만 applyFails 를 호출합니다.
// 입력은 한 줄이며 EOF·빈 입력·Y/y 가 아닌 모든 입력은 N 입니다.
// -no-prompt 이거나 덤프 읽기 모드면 묻지 않습니다.
func confirmApply(rc *runContext, run *checkRun, in io.Reader, w io.Writer, noPrompt bool, o reportOpts) error {
	p := painter{o.Color}
	fails := run.failItems()
	if len(fails) == 0 {
		return nil
	}
	hostSet := map[string]bool{}
	for _, f := range fails {
		hostSet[f.Target.Hostname] = true
	}
	switch {
	case run.FromDump != "":
		fmt.Fprintf(w, "\nFAIL %d건(호스트 %d대) — 덤프 읽기 모드에서는 설정할 수 없어 묻지 않습니다.\n", len(fails), len(hostSet))
		return nil
	case noPrompt:
		fmt.Fprintf(w, "\nFAIL %d건(호스트 %d대) — -no-prompt 이므로 묻지 않고 종료합니다 (아무것도 설정하지 않음).\n", len(fails), len(hostSet))
		return nil
	}
	// 실제 적용 가능 건수: 쓰기 대상이 아닌 벤더·같은 BMC 중복 대상은 뺀다 (UNVERIFIED 는 처음부터 fails 에 없음).
	nApp, nAppHosts, nVendor, nDup := 0, 0, 0, 0
	for _, h := range applyHosts(fails) {
		switch {
		case h.DupOf != "":
			nDup += len(h.Items)
		case h.Info == nil || !setVendors[h.Info.Vendor]:
			nVendor += len(h.Items)
		default:
			nApp += len(h.Items)
			nAppHosts++
		}
	}
	if nVendor+nDup > 0 {
		fmt.Fprintf(w, "\n※ FAIL %d건(호스트 %d대) 중 %d건은 설정하지 않습니다 (쓰기 대상이 아닌 벤더 %d건, 같은 BMC 중복 대상 %d건 — 결과 파일에 사유 기록)\n",
			len(fails), len(hostSet), nVendor+nDup, nVendor, nDup)
	}
	fmt.Fprintf(w, "\n%s", p.cyan(fmt.Sprintf("FAIL %d건(호스트 %d대)을 Pending 으로 설정하시겠습니까? 재부팅은 하지 않습니다. (Y/N): ", nApp, nAppHosts)))
	line, _ := bufio.NewReader(in).ReadString('\n')
	if !strings.HasSuffix(line, "\n") {
		fmt.Fprintln(w) // EOF 로 끝났으면 프롬프트 줄을 닫는다
	}
	if ans := strings.TrimSpace(line); ans != "Y" && ans != "y" {
		fmt.Fprintln(w, "설정하지 않고 종료합니다.")
		return nil
	}
	return applyFails(rc, run, fails, w, o)
}

// stdinIsTerminal 은 표준입력이 터미널(문자 장치)인지 봅니다. 테스트가 바꿔 쓸 수 있도록 변수입니다.
var stdinIsTerminal = func(f *os.File) bool {
	fi, err := f.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}

// msgNonInteractive 는 비대화형 입력이라 Y/N 을 읽지 않을 때의 안내입니다.
const msgNonInteractive = "비대화형 입력에서는 설정하지 않습니다. 필요하면 -stdin-ok"

// confirmApplyStdin 은 cmdCheck 의 Y/N 확인입니다. 표준입력이 터미널이 아니면(파이프·파일, `yes |` 등)
// -stdin-ok 없이는 입력을 읽지 않고 N 으로 처리합니다 (미리 입력된 y 로 의도치 않게 설정되는 것을 막음).
// 그 밖에는 confirmApply 와 같습니다.
func confirmApplyStdin(rc *runContext, run *checkRun, in *os.File, stdinOK bool, w io.Writer, noPrompt bool, o reportOpts) error {
	if !noPrompt && run.FromDump == "" && !stdinOK && !stdinIsTerminal(in) {
		if fails := run.failItems(); len(fails) > 0 {
			fmt.Fprintf(w, "\nFAIL %d건 — %s (아무것도 설정하지 않음).\n", len(fails), msgNonInteractive)
			return nil
		}
	}
	return confirmApply(rc, run, in, w, noPrompt, o)
}
