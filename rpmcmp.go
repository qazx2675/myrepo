package main

// -rpm : 비교서버와 대상 서버들의 설치 rpm 패키지 "이름.아키텍처" 목록을 비교한다(버전은 무시).
// 대상 서버에서는 목록만 뽑고(기존 명령 실행 흐름), 비교는 로컬에서 해서 보고서 파일로 저장한다.

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
)

// gpg-pubkey 는 서버마다 달라서 비교에서 뺀다. (root 쉘이 csh 계열일 수 있어 LC_ALL= 같은 sh 전용 문법은 쓰지 않는다. 집합 비교라 정렬 순서는 무관)
const rpmListCmd = `rpm -qa --qf '%{NAME}.%{ARCH}\n' | grep -v '^gpg-pubkey\.' | sort -u`

func parsePkgSet(text string) map[string]bool {
	set := map[string]bool{}
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "gpg-pubkey.") {
			continue
		}
		set[line] = true
	}
	return set
}

func diffPkgs(ref, cur map[string]bool) (missing, extra []string) {
	for p := range ref {
		if !cur[p] {
			missing = append(missing, p)
		}
	}
	for p := range cur {
		if !ref[p] {
			extra = append(extra, p)
		}
	}
	sort.Strings(missing)
	sort.Strings(extra)
	return
}

// planRPM은 비교서버를 물어보고 그 서버의 패키지 목록을 읽어, 대상 서버에서 실행할 명령과 기준 목록을 돌려준다.
func planRPM(user string, auth []ssh.AuthMethod, port string, timeout time.Duration) (cmd, refHost string, ref map[string]bool) {
	refHost = promptLine("비교서버 입력 (rpm 패키지 목록 기준) : ")
	if refHost == "" {
		fmt.Fprintln(os.Stderr, "비교서버가 입력되지 않았습니다.")
		os.Exit(1)
	}
	out, err := fetchRemoteCmd(refHost, rpmListCmd, user, auth, port, timeout)
	if err != nil {
		fmt.Fprintf(os.Stderr, "비교서버(%s)에서 rpm 목록을 읽을 수 없습니다: %v\n", refHost, err)
		os.Exit(1)
	}
	ref = parsePkgSet(string(out))
	if len(ref) == 0 {
		fmt.Fprintf(os.Stderr, "[가드] 비교서버(%s)의 rpm 패키지 목록이 비어 있어 중단합니다.\n", refHost)
		os.Exit(1)
	}
	return rpmListCmd, refHost, ref
}

type rpmGroup struct {
	status  string // 동일 / 차이 / 확인불가 / 접속불가
	missing []string
	extra   []string
	hosts   []string
}

// writeRPMReport는 결과를 비교해 보고서(txt), 탭 구분 txt(엑셀용), 차이 호스트 목록을 저장하고 화면에 한 줄 요약을 찍는다.
func writeRPMReport(refHost string, ref map[string]bool, hosts []string, outputs map[string]string,
	unreachable []string, reportFile, tabFile, diffFile string, start time.Time) {

	unreach := map[string]bool{}
	for _, h := range unreachable {
		unreach[h] = true
	}
	var groups []*rpmGroup
	byKey := map[string]*rpmGroup{}
	var diffHosts []string
	var tab strings.Builder
	tab.WriteString("\xEF\xBB\xBF") // UTF-8 BOM: 엑셀에서 한글이 깨지지 않게
	tab.WriteString("호스트\t상태\t구분\t패키지\n")
	cnt := map[string]int{}

	add := func(status string, missing, extra []string, h string) {
		key := status + "\x00" + strings.Join(missing, "\x01") + "\x02" + strings.Join(extra, "\x01")
		g := byKey[key]
		if g == nil {
			g = &rpmGroup{status: status, missing: missing, extra: extra}
			byKey[key] = g
			groups = append(groups, g)
		}
		g.hosts = append(g.hosts, h)
		cnt[status]++
	}

	for _, h := range hosts {
		switch {
		case unreach[h]:
			add("접속불가", nil, nil, h)
			tab.WriteString(h + "\t접속불가\t\t\n")
		case outputs[h] == "":
			add("확인불가", nil, nil, h)
			tab.WriteString(h + "\t확인불가\t\t\n")
		default:
			missing, extra := diffPkgs(ref, parsePkgSet(outputs[h]))
			if len(missing) == 0 && len(extra) == 0 {
				add("동일", nil, nil, h)
				tab.WriteString(h + "\t동일\t\t\n")
				continue
			}
			add("차이", missing, extra, h)
			diffHosts = append(diffHosts, h)
			for _, p := range missing {
				tab.WriteString(h + "\t차이\t부족(비교서버에만 있음)\t" + p + "\n")
			}
			for _, p := range extra {
				tab.WriteString(h + "\t차이\t추가(대상에만 있음)\t" + p + "\n")
			}
		}
	}

	joinHosts := func(hs []string) string { return strings.Join(compressHosts(hs), ",") }
	var b strings.Builder
	line := strings.Repeat("=", 60)
	b.WriteString(line + "\n RPM 패키지 비교 보고서 (버전 무시, 이름.아키텍처 기준)\n" + line + "\n")
	fmt.Fprintf(&b, " 비교서버 : %s   (패키지 %d개)\n", refHost, len(ref))
	fmt.Fprintf(&b, " 실행시각 : %s\n", start.Format("2006-01-02 15:04:05"))
	fmt.Fprintf(&b, " 대상     : 총 %d대 = 동일 %d / 차이 %d / 확인불가 %d / 접속불가 %d\n",
		len(hosts), cnt["동일"], cnt["차이"], cnt["확인불가"], cnt["접속불가"])
	b.WriteString(strings.Repeat("-", 60) + "\n\n[1] 호스트별 요약 (동일 제외)\n")

	width := 0
	for _, g := range groups {
		if g.status == "동일" {
			continue
		}
		if w := displayWidth(joinHosts(g.hosts)); w > width {
			width = w
		}
	}
	if width > 40 {
		width = 40
	}
	fmt.Fprintf(&b, " %s  %-8s %5s %5s\n", padRight("호스트", width), "상태", "부족", "추가")
	for _, g := range groups {
		if g.status == "동일" {
			continue
		}
		note := ""
		if len(g.hosts) > 1 {
			note = fmt.Sprintf("   (%d대, 결과 동일)", len(g.hosts))
		}
		if g.status == "차이" {
			fmt.Fprintf(&b, " %s  %-8s %5d %5d%s\n", padRight(joinHosts(g.hosts), width), g.status, len(g.missing), len(g.extra), note)
		} else {
			fmt.Fprintf(&b, " %s  %-8s %5s %5s%s\n", padRight(joinHosts(g.hosts), width), g.status, "-", "-", note)
		}
	}
	b.WriteString("\n[2] 차이 상세 (같은 결과끼리 묶음)\n")
	if cnt["차이"] == 0 {
		b.WriteString("\n   차이 나는 호스트가 없습니다.\n")
	}
	for _, g := range groups {
		if g.status != "차이" {
			continue
		}
		fmt.Fprintf(&b, "\n■ %s  (%d대)\n", joinHosts(g.hosts), len(g.hosts))
		for _, sec := range []struct {
			title string
			list  []string
		}{{"비교서버에만 있음(부족)", g.missing}, {"대상에만 있음(추가)", g.extra}} {
			if len(sec.list) == 0 {
				fmt.Fprintf(&b, "   - %s : 없음\n", sec.title)
				continue
			}
			fmt.Fprintf(&b, "   - %s :\n", sec.title)
			for _, p := range sec.list {
				b.WriteString("       " + p + "\n")
			}
		}
	}
	sec := 3
	for _, st := range []string{"확인불가", "접속불가", "동일"} {
		title := map[string]string{"확인불가": "확인불가 (접속됐으나 rpm 목록을 받지 못함)", "접속불가": "접속불가", "동일": "동일"}[st]
		for _, g := range groups {
			if g.status == st {
				fmt.Fprintf(&b, "\n[%d] %s\n   %s\n", sec, title, joinHosts(g.hosts))
				sec++
			}
		}
	}

	if err := os.WriteFile(reportFile, []byte(b.String()), 0644); err != nil {
		fmt.Fprintf(os.Stderr, "보고서 저장 실패 (%s): %v\n", reportFile, err)
	}
	if err := os.WriteFile(tabFile, []byte(tab.String()), 0644); err != nil {
		fmt.Fprintf(os.Stderr, "탭 구분 파일 저장 실패 (%s): %v\n", tabFile, err)
	}
	writeHostsToFile(diffFile, diffHosts)

	fmt.Printf("RPM 비교 (비교서버 %s, 패키지 %d개): 동일 %d / 차이 %d / 확인불가 %d / 접속불가 %d\n",
		refHost, len(ref), cnt["동일"], cnt["차이"], cnt["확인불가"], cnt["접속불가"])
	fmt.Printf("  보고서       -> %s\n  엑셀용(탭)   -> %s\n", reportFile, tabFile)
	if len(diffHosts) > 0 {
		fmt.Printf("  차이 호스트  -> %s\n", diffFile)
	}
}
