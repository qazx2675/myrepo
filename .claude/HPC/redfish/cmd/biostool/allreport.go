package main

import (
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"strings"
)

// allreport.go 는 allcheck 의 터미널 리포트입니다 (report.go 의 painter·색 규칙을 그대로 씁니다).
// 수천 대여도 읽을 수 있게 호스트 상세·특이사항·오류는 상한을 두고, 넘치는 분량은 결과 파일 경로로 안내합니다.
// 출력에는 호스트·모델·속성·값만 나오며 비밀번호·토큰은 어디에도 없습니다.

// allReportOpts 는 리포트 출력 옵션입니다.
type allReportOpts struct {
	ListMax int  // 차이 상세·특이사항·오류에 나열할 최대 호스트 수
	DiffMax int  // 호스트당 보여 줄 차이 속성 수 (속성별 집계의 줄 수 상한이기도 함)
	Color   bool // ANSI 색 (TTY 이고 NO_COLOR 가 없을 때)
}

// allGroup 은 같은 모델 키로 묶은 결과입니다 (기준이 있는 모델만 만들어집니다).
type allGroup struct {
	Key, Label string
	Refs       []*allRef  // 실제로 쓰인 기준 (쓰인 것이 없으면 이 모델의 기준 전부)
	Hosts      []*allHost // 비교한 대상 (SAME/DIFF)
	Same, Diff int
	Self       int // 기준 자신이라 비교하지 않은 대상 수
}

// groups 는 모델 그룹을 기준(diff.txt) 순서로 돌려줍니다.
func (r *allRun) groups() []*allGroup {
	idx := map[string]*allGroup{}
	var out []*allGroup
	for _, ref := range r.Refs {
		if !ref.Snap.ok() || idx[ref.Snap.Key] != nil {
			continue
		}
		g := &allGroup{Key: ref.Snap.Key, Label: ref.Snap.label()}
		idx[g.Key] = g
		out = append(out, g)
	}
	used := map[*allRef]bool{}
	for _, h := range r.Hosts {
		switch h.Status {
		case StatusSame, StatusDiff:
			g := idx[h.Snap.Key]
			g.Hosts = append(g.Hosts, h)
			used[h.Ref] = true
			if h.Status == StatusSame {
				g.Same++
			} else {
				g.Diff++
			}
		case StatusIsReference:
			idx[h.Snap.Key].Self++
		}
	}
	for _, g := range out {
		for _, ref := range r.Refs {
			if ref.Snap.ok() && ref.Snap.Key == g.Key && (len(g.Hosts) == 0 || used[ref]) {
				g.Refs = append(g.Refs, ref)
			}
		}
	}
	return out
}

func refText(refs []*allRef) string {
	parts := make([]string, len(refs))
	for i, r := range refs {
		parts[i] = fmt.Sprintf("%s (BIOS %s)", r.Target.Hostname, dash(r.Snap.BiosVersion))
	}
	return strings.Join(parts, ", ")
}

// writeAllReport 는 all_bios_check 의 터미널 리포트를 출력합니다.
func writeAllReport(w io.Writer, r *allRun, o allReportOpts) {
	p := painter{o.Color}
	t := r.tally()
	fmt.Fprintf(w, "== BIOS 전체 비교 (대상 %s대, 기준 %d대 / %s) ==\n", commaInt(len(r.Hosts)), len(r.Refs), r.Now.Format("2006-01-02 15:04"))

	line := func(label string, n int, color func(string) string, tail string) {
		s := fmt.Sprintf("%-16s %7s", label, commaInt(n))
		if color != nil && n > 0 {
			s = color(s)
		}
		fmt.Fprintln(w, s+tail)
	}
	line("완전 일치", t[StatusSame], p.green, "")
	line("차이 있음", t[StatusDiff], p.red, "  → "+filepath.ToSlash(filepath.Join(r.Dir, "all_diff.tsv")))
	if t[StatusIsReference] > 0 {
		line("기준 자신", t[StatusIsReference], nil, "  (같은 모델의 다른 기준이 없어 비교 안 함)")
	}
	known := map[string]bool{StatusSame: true, StatusDiff: true, StatusIsReference: true, StatusNoReference: true, StatusRefUnreachable: true}
	var unclass []string
	for _, st := range []string{StatusNoReference, StatusRefUnreachable} {
		if t[st] > 0 {
			unclass = append(unclass, fmt.Sprintf("%s %d", st, t[st]))
		}
	}
	if len(unclass) > 0 {
		n := t[StatusNoReference] + t[StatusRefUnreachable]
		line("분류 불가", n, p.yellow, "  ("+strings.Join(unclass, ", ")+")")
	}
	var otherN int
	var otherParts []string
	for _, st := range otherStatuses(t, known) {
		otherN += t[st]
		otherParts = append(otherParts, fmt.Sprintf("%s %d", st, t[st]))
	}
	if otherN > 0 {
		line("기타 오류", otherN, p.yellow, "  ("+strings.Join(otherParts, ", ")+")")
	}
	if r.FromDump != "" {
		fmt.Fprintln(w, p.cyan("(덤프 읽기 모드: BMC 에 접속하지 않았습니다)"))
	}
	if warn := authStopWarning(r.authFails(), t[StatusSkippedAuthStop], authStopRerunCheck); warn != "" {
		fmt.Fprintln(w, p.boldRed("!! "+warn))
	}

	groups := r.groups()
	writeAllGroups(w, groups, o, r, p)
	writeAllAgg(w, groups, o, p)
	writeAllSpecial(w, r, o, p)
	writeAllRefErrors(w, r, p)
	writeAllUnclassified(w, r, o, p)

	fmt.Fprintln(w)
	fmt.Fprintf(w, "%s 결과 파일: %s/  (all_diff.tsv 차이 전체, summary.tsv 호스트별 요약·특이사항, retry.txt 재시도 대상, run_info.txt)\n",
		p.cyan("*"), filepath.ToSlash(r.Dir))
	if r.Retry != nil {
		writeRetryMergeSection(w, r.Retry, r.Dir, p)
	}
}

// writeAllGroups 는 모델 그룹별 요약과, 차이 있는 호스트의 속성 목록입니다.
func writeAllGroups(w io.Writer, groups []*allGroup, o allReportOpts, r *allRun, p painter) {
	for _, g := range groups {
		fmt.Fprintln(w)
		same := fmt.Sprintf("%d대", g.Same)
		diff := fmt.Sprintf("%d대", g.Diff)
		if g.Same > 0 {
			same = p.green(same)
		}
		if g.Diff > 0 {
			diff = p.red(diff)
		}
		self := ""
		if g.Self > 0 {
			self = fmt.Sprintf(" (기준 자신 %d대 제외)", g.Self)
		}
		fmt.Fprintf(w, "%s 기준: %s / 대상 %d대 / 완전 일치 %s / 차이 있음 %s%s\n",
			p.cyan("["+g.Label+"]"), refText(g.Refs), len(g.Hosts), same, diff, self)

		width, shown := 0, 0
		for _, h := range g.Hosts {
			if h.Status == StatusDiff && len(h.Target.Hostname) > width {
				width = len(h.Target.Hostname)
			}
		}
		if width > 40 {
			width = 40
		}
		for _, h := range g.Hosts {
			if h.Status != StatusDiff {
				continue
			}
			if shown == o.ListMax {
				fmt.Fprintf(w, "  … 외 %d대의 차이는 %s 참고\n", g.Diff-shown, filepath.ToSlash(filepath.Join(r.Dir, "all_diff.tsv")))
				break
			}
			shown++
			head := fmt.Sprintf("DIFF %d, ONLY_REF %d, ONLY_HOST %d", h.NDiff, h.NOnlyRef, h.NOnlyHost)
			if h.hasNote(noteBiosVerDiff) {
				head += "  [" + noteBiosVerDiff + "]"
			}
			fmt.Fprintf(w, "  %s  %s\n", padTo(h.Target.Hostname, width), p.red(head))
			aw := 0
			for i, d := range h.Diffs {
				if i == o.DiffMax {
					break
				}
				if len(d.Attr) > aw {
					aw = len(d.Attr)
				}
			}
			if aw > 40 {
				aw = 40
			}
			for i, d := range h.Diffs {
				if i == o.DiffMax {
					fmt.Fprintf(w, "      … 외 %d건, all_diff.tsv 참고\n", len(h.Diffs)-i)
					break
				}
				tag := ""
				if d.Status != attrDiff {
					tag = "  (" + d.Status + ")"
				}
				fmt.Fprintf(w, "      %s  기준=%s → 대상=%s%s\n", padTo(d.Attr, aw), clip(d.refCell(), 60), clip(d.hostCell(), 60), tag)
			}
		}
	}
}

// aggEntry 는 속성 1개가 몇 대에서 다른지의 집계입니다.
type aggEntry struct {
	Attr     string
	Hosts    int
	ByStatus map[string]int
	RefVals  map[string]int
	HostVals map[string]int
}

// topVals 는 값 분포를 "값(대수)" 로 많은 순 n 개까지 보여 줍니다.
func topVals(m map[string]int, n int) string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if m[keys[i]] != m[keys[j]] {
			return m[keys[i]] > m[keys[j]]
		}
		return keys[i] < keys[j]
	})
	var parts []string
	for i, k := range keys {
		if i == n {
			parts = append(parts, fmt.Sprintf("… 외 %d종", len(keys)-n))
			break
		}
		parts = append(parts, fmt.Sprintf("%s(%d)", clip(k, 30), m[k]))
	}
	return strings.Join(parts, ", ")
}

// aggregate 는 그룹 안에서 속성별로 다른 호스트 수를 셉니다 (많은 순, 같으면 이름순).
func (g *allGroup) aggregate() []*aggEntry {
	m := map[string]*aggEntry{}
	for _, h := range g.Hosts {
		for _, d := range h.Diffs {
			e := m[d.Attr]
			if e == nil {
				e = &aggEntry{Attr: d.Attr, ByStatus: map[string]int{}, RefVals: map[string]int{}, HostVals: map[string]int{}}
				m[d.Attr] = e
			}
			e.Hosts++
			e.ByStatus[d.Status]++
			e.RefVals[d.refCell()]++
			e.HostVals[d.hostCell()]++
		}
	}
	out := make([]*aggEntry, 0, len(m))
	for _, e := range m {
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Hosts != out[j].Hosts {
			return out[i].Hosts > out[j].Hosts
		}
		return out[i].Attr < out[j].Attr
	})
	return out
}

// writeAllAgg 는 속성별 집계입니다. 대상의 과반(2대 이상)에서 같은 속성이 다르면 기준 호스트의 값이 예외일 수 있다고 알립니다.
func writeAllAgg(w io.Writer, groups []*allGroup, o allReportOpts, p painter) {
	has := false
	for _, g := range groups {
		if g.Diff > 0 {
			has = true
		}
	}
	if !has {
		return
	}
	fmt.Fprintln(w)
	fmt.Fprintln(w, p.yellow("[속성별 집계]")+" 같은 속성이 몇 대에서 다른가 (모델 그룹별, 많은 순)")
	for _, g := range groups {
		if g.Diff == 0 {
			continue
		}
		aggs := g.aggregate()
		fmt.Fprintf(w, "  [%s] 비교 %d대 중\n", g.Label, len(g.Hosts))
		aw, flagged := 0, false
		for i, e := range aggs {
			if i < o.DiffMax && len(e.Attr) > aw {
				aw = len(e.Attr)
			}
		}
		if aw > 40 {
			aw = 40
		}
		for i, e := range aggs {
			if i == o.DiffMax {
				fmt.Fprintf(w, "    … 외 %d개 속성, all_diff.tsv 참고\n", len(aggs)-i)
				break
			}
			mark := " "
			if e.Hosts*2 > len(g.Hosts) && e.Hosts >= 2 {
				mark, flagged = p.boldRed("!"), true
			}
			mix := ""
			if len(e.ByStatus) > 1 || e.ByStatus[attrDiff] == 0 {
				var parts []string
				for _, st := range []string{attrDiff, attrOnlyRef, attrOnlyHost} {
					if e.ByStatus[st] > 0 {
						parts = append(parts, fmt.Sprintf("%s %d", st, e.ByStatus[st]))
					}
				}
				mix = "  [" + strings.Join(parts, ", ") + "]"
			}
			fmt.Fprintf(w, "   %s%s  %d대 (%d%%)  기준 %s → 대상 %s%s\n", mark, padTo(e.Attr, aw), e.Hosts, e.Hosts*100/len(g.Hosts),
				topVals(e.RefVals, 2), topVals(e.HostVals, 3), mix)
		}
		if flagged {
			fmt.Fprintf(w, "    %s 표시: 대상 과반에서 같은 속성이 다릅니다. 기준 호스트(%s)의 값이 예외(비정상)일 수 있으니 기준을 다시 확인하십시오.\n",
				p.boldRed("!"), strings.Join(refNames(g.Refs), ", "))
		}
	}
}

func refNames(refs []*allRef) []string {
	out := make([]string, len(refs))
	for i, r := range refs {
		out[i] = r.Target.Hostname
	}
	return out
}

// noteGroup 은 같은 특이사항(코드·모델·상세)을 가진 호스트 묶음입니다.
type noteGroup struct {
	Code, Label, Detail string
	Hosts               []string
}

// writeAllSpecial 은 특이사항 목록입니다: BIOS 버전 불일치, 기준 다수 선택 사유, diff.txt 모델 칸 불일치.
func writeAllSpecial(w io.Writer, r *allRun, o allReportOpts, p painter) {
	idx := map[string]*noteGroup{}
	var groups []*noteGroup
	count := map[string]int{}
	for _, h := range r.Hosts {
		for _, n := range h.Notes {
			label := ""
			if h.Snap != nil && h.Snap.ok() {
				label = h.Snap.label()
			}
			key := n.Code + "\x00" + label + "\x00" + n.Detail
			g := idx[key]
			if g == nil {
				g = &noteGroup{Code: n.Code, Label: label, Detail: n.Detail}
				idx[key] = g
				groups = append(groups, g)
			}
			g.Hosts = append(g.Hosts, h.Target.Hostname)
			count[n.Code]++
		}
	}
	var mismatches []*allRef
	for _, ref := range r.Refs {
		if len(ref.Notes) > 0 {
			mismatches = append(mismatches, ref)
		}
	}
	if len(groups) == 0 && len(mismatches) == 0 {
		return
	}
	fmt.Fprintln(w)
	fmt.Fprintln(w, p.yellow("[특이사항]"))
	titles := []struct{ code, text string }{
		{noteBiosVerDiff, "대상 BIOS 버전이 기준과 다름 (비교는 그대로 진행함)"},
		{noteMultiRef, "같은 모델의 기준이 여러 개라 하나를 골라 비교함"},
	}
	for _, tt := range titles {
		if count[tt.code] == 0 {
			continue
		}
		fmt.Fprintf(w, "  %s  %s: %d대\n", p.yellow(tt.code), tt.text, count[tt.code])
		shown := 0
		for _, g := range groups {
			if g.Code != tt.code {
				continue
			}
			if shown == o.ListMax {
				fmt.Fprintf(w, "    … 외 그룹은 %s 의 notes 참고\n", filepath.ToSlash(filepath.Join(r.Dir, "summary.tsv")))
				break
			}
			shown++
			fmt.Fprintf(w, "    [%s] %s  %d대\n", g.Label, g.Detail, len(g.Hosts))
			names := g.Hosts
			more := 0
			if len(names) > o.ListMax {
				names, more = names[:o.ListMax], len(names)-o.ListMax
			}
			for _, l := range wrapNames(names, "      ", reportWidth) {
				fmt.Fprintln(w, l)
			}
			if more > 0 {
				fmt.Fprintf(w, "      … 외 %d대\n", more)
			}
		}
		if tt.code == noteBiosVerDiff {
			fmt.Fprintln(w, "    ※ BIOS 버전이 다르면 속성 목록 자체가 달라 ONLY_REF / ONLY_HOST 가 늘 수 있습니다 (버전 차이일 뿐 설정 오류가 아닐 수 있음).")
		}
	}
	if len(mismatches) > 0 {
		fmt.Fprintf(w, "  %s  diff.txt 에 적은 모델이 BMC 가 보고한 실제 모델과 다름 (실제 모델로 분류함): %d대\n", p.yellow(noteDiffTxtModelMismatch), len(mismatches))
		for i, ref := range mismatches {
			if i == o.ListMax {
				fmt.Fprintf(w, "    … 외 %d대\n", len(mismatches)-i)
				break
			}
			fmt.Fprintf(w, "    %s: %s\n", ref.Target.Hostname, ref.Notes[0].Detail)
		}
	}
}

// writeAllRefErrors 는 읽지 못한 기준 호스트입니다 (기준이 빠지면 그 모델의 대상이 REF_UNREACHABLE 이 됩니다).
func writeAllRefErrors(w io.Writer, r *allRun, p painter) {
	var bad []*allRef
	for _, ref := range r.Refs {
		if !ref.Snap.ok() {
			bad = append(bad, ref)
		}
	}
	if len(bad) == 0 {
		return
	}
	fmt.Fprintln(w)
	fmt.Fprintln(w, p.boldRed("[기준 호스트 오류]")+" 읽지 못한 기준은 비교에서 빠집니다 (diff.txt 는 매번 다시 읽으므로 복구 후 재실행)")
	for _, ref := range bad {
		d := ""
		if ref.Snap.Detail != "" {
			d = "  " + ref.Snap.Detail
		}
		fmt.Fprintf(w, "  %s  %s%s\n", padTo(ref.Target.Hostname, 20), ref.Snap.Status, d)
	}
}

// writeAllUnclassified 는 분류할 수 없거나 오류가 난 대상 목록입니다.
func writeAllUnclassified(w io.Writer, r *allRun, o allReportOpts, p painter) {
	t := r.tally()
	known := map[string]bool{StatusSame: true, StatusDiff: true, StatusIsReference: true, StatusNoReference: true, StatusRefUnreachable: true}
	sts := append([]string{StatusNoReference, StatusRefUnreachable}, otherStatuses(t, known)...)
	first, retry := true, 0
	for _, st := range sts {
		if t[st] == 0 {
			continue
		}
		if first {
			fmt.Fprintln(w)
			fmt.Fprintln(w, p.yellow("[분류 불가·오류]"))
			first = false
		}
		var hs []*allHost
		for _, h := range r.Hosts {
			if h.Status == st {
				hs = append(hs, h)
			}
		}
		if isRetryStatus(st) || st == StatusRefUnreachable {
			retry += len(hs)
		}
		fmt.Fprintf(w, "  %-16s %s대\n", st, commaInt(len(hs)))
		if st == StatusNoReference {
			// 모델별로 묶어 어떤 모델의 기준을 diff.txt 에 추가해야 하는지 보여 준다.
			byLabel := map[string][]string{}
			var labels []string
			for _, h := range hs {
				l := h.Snap.label()
				if _, ok := byLabel[l]; !ok {
					labels = append(labels, l)
				}
				byLabel[l] = append(byLabel[l], h.Target.Hostname)
			}
			for _, l := range labels {
				fmt.Fprintf(w, "    [%s] %d대: %s\n", l, len(byLabel[l]), namesText(byLabel[l], o.ListMax))
			}
			fmt.Fprintln(w, "    이 모델의 정상 설정값을 가진 대표 hostname 을 diff.txt 에 추가한 뒤 다시 실행하십시오.")
			continue
		}
		names := make([]string, len(hs))
		detail := ""
		for i, h := range hs {
			names[i] = h.Target.Hostname
			if detail == "" && h.Detail != "" {
				detail = h.Detail
			}
		}
		fmt.Fprintf(w, "    %s\n", namesText(names, o.ListMax))
		switch st {
		case StatusRefUnreachable:
			fmt.Fprintf(w, "    %s\n", detail)
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
			p.cyan("*"), retry, filepath.ToSlash(filepath.Join(r.Dir, "retry.txt")))
	}
}

// namesText 는 이름을 공백으로 이어 붙이되 limit 개를 넘으면 "… 외 N대" 를 붙입니다.
func namesText(names []string, limit int) string {
	if len(names) <= limit {
		return strings.Join(names, " ")
	}
	return strings.Join(names[:limit], " ") + fmt.Sprintf(" … 외 %d대", len(names)-limit)
}
