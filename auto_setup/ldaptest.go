// ldaptest.go - auto_setup ldap-test: list 의 서버마다 LDAP 설정 백업 + RHEL7/RHEL8 용 변환 세트를 ./out/<호스트>/ 에 만들고,
// target 을 주면 그 백업으로 target 을 auto_setup 과 같은 규칙으로 비교하고 필요하면 복원한다 (ldaptest/test 래퍼, ldaptest/README.md).
// 수집·저장·변환·비교·복원은 ldapbk.go 의 collectHosts·writeBackupFiles·loadBackupDir·convertEntriesFor·compareEntries·
// restoreEntries 를 그대로 쓴다(별도 로직 없음). 화면·files.txt·notes.txt 에는 파일 내용(bindpw·secret)을 싣지 않으며,
// 결과 디렉터리는 0700, 파일은 0600 이다(백업·변환 파일에는 bindpw 가 있다).
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

const ldapTestUsage = `사용법: auto_setup ldap-test [-out <dir>] [-y] <list파일> [target서버]
  list 파일의 서버마다 LDAP 관련 설정을 수집해 <out>/<호스트>/ 에 저장한다 (기본 ./out, 같은 호스트는 덮어씀)
    backup/  수집한 원본 + meta.json (auto_setup 전달 시점 백업과 같은 형식)
    rhel7/   RHEL7(nslcd/ntp) 용 세트 + files.txt(원격 경로·mode·owner) + notes.txt(변환 내역)
    rhel8/   RHEL8(sssd/chrony) 용 세트 + files.txt + notes.txt
  target 을 주면 list 의 서버(정확히 1대) 백업으로 target 을 auto_setup 과 같은 규칙으로 비교하고, 필요하면 복원한다
  (target 의 설정 파일을 덮어쓰고 서비스를 재시작함 — yes 확인, -y 면 생략. ldap_share_dir 필요)
  list 파일: 한 줄에 호스트 하나 (공백·쉼표·탭 구분 가능, # 주석·빈 줄 무시, 중복 제거)
  종료코드: 0 모두 성공, 1 일부 실패, 2 사용법 오류
`

const ltJob = "ldap-test" // meta.json 의 job 값

// ltKinds: 변환 세트 — 디렉터리 이름, 인증·시각 종류 (convertEntriesFor 인자)
var ltKinds = [2]struct{ dir, auth, time string }{{"rhel7", "nslcd", "ntp"}, {"rhel8", "sssd", "chrony"}}

// ltResult: 호스트 한 대의 결과. 사유 문자열이 "" 이면 OK (사유에는 gossh 접속 오류 문구·파일 이름만 있고 비밀값 없음)
type ltResult struct {
	host, dir  string
	os         int
	auth, time string
	backup     string
	kinds      [2]string // rhel7, rhel8
	entries    []lbEntry // backup/ 에서 다시 읽은 세트 (target 모드용)
}

func (r ltResult) ok() bool { return r.backup == "" && r.kinds[0] == "" && r.kinds[1] == "" }

func ltOK(why string) string {
	if why == "" {
		return "OK"
	}
	return "FAIL(" + why + ")"
}

// line: 화면 요약 한 줄 — '호스트  OS<n>  auth/time  backup …  rhel7 …  rhel8 …  → <dir>'
func (r ltResult) line() string {
	osS, kind := "OS?", "-/-"
	if r.os > 0 {
		osS = fmt.Sprintf("OS%d", r.os)
	}
	if r.auth != "" {
		kind = r.auth + "/" + r.time
	}
	return fmt.Sprintf("%-20s %-4s %-12s backup %s  rhel7 %s  rhel8 %s  → %s", r.host, osS, kind,
		ltOK(r.backup), ltOK(r.kinds[0]), ltOK(r.kinds[1]), r.dir)
}

func ltUsage(w io.Writer, msg string) int {
	if msg != "" {
		fmt.Fprintln(w, "[X] "+msg)
	}
	fmt.Fprint(w, ldapTestUsage)
	return 2
}

// cmdLdapTest: auto_setup ldap-test [-out <dir>] [-y] <list파일> [target서버]
func cmdLdapTest(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	outDir, yes := "out", false
	var pos []string
	for i := 0; i < len(args); i++ {
		switch a := args[i]; {
		case a == "-y":
			yes = true
		case a == "-out":
			if i+1 >= len(args) || args[i+1] == "" {
				return ltUsage(stderr, "-out 뒤에 디렉터리가 필요합니다")
			}
			i++
			outDir = args[i]
		case a == "-h" || a == "--help":
			fmt.Fprint(stdout, ldapTestUsage)
			return 0
		case strings.HasPrefix(a, "-"):
			return ltUsage(stderr, "알 수 없는 옵션: "+a)
		default:
			pos = append(pos, a)
		}
	}
	if len(pos) < 1 || len(pos) > 2 {
		return ltUsage(stderr, "")
	}
	hosts, err := readHostList(pos[0])
	if err != nil {
		return ltUsage(stderr, err.Error())
	}
	target := ""
	if len(pos) == 2 {
		target = pos[1]
		if !hostRe.MatchString(target) {
			return ltUsage(stderr, "잘못된 target 서버 이름: "+target)
		}
		if len(hosts) != 1 {
			return ltUsage(stderr, fmt.Sprintf("target 을 주면 list 에 서버가 정확히 1대여야 합니다 (현재 %d대) — 백업으로 쓸 서버 1대만 적은 list 를 쓰세요", len(hosts)))
		}
	}
	if err := os.MkdirAll(outDir, 0700); err != nil {
		fmt.Fprintln(stderr, "[X] 결과 디렉터리 생성 실패:", err)
		return 1
	}

	// 수집: auto_setup 전달 시점 백업과 같은 collectHosts (기본 local=os8 gossh, 무응답이면 os6_mgmt 경유)
	got := collectHosts(hosts, func(string) string { return "local" })
	var res []ltResult
	nFail, nb, n7, n8 := 0, 0, 0, 0
	for _, h := range hosts {
		r := ltHost(outDir, h, got[h])
		res = append(res, r)
		fmt.Fprintln(stdout, r.line())
		if !r.ok() {
			nFail++
		}
		if r.backup != "" {
			nb++
		}
		if r.kinds[0] != "" {
			n7++
		}
		if r.kinds[1] != "" {
			n8++
		}
	}
	fmt.Fprintf(stdout, "합계: %d대 — 성공 %d, 실패 %d (backup 실패 %d, rhel7 실패 %d, rhel8 실패 %d) → %s\n",
		len(hosts), len(hosts)-nFail, nFail, nb, n7, n8, outDir)
	code := 0
	if nFail > 0 {
		code = 1
	}
	if target != "" {
		if c := ltTarget(stdin, stdout, res[0], target, yes); c != 0 {
			code = c
		}
	}
	return code
}

// readHostList: list 파일 — 공백·쉼표·탭 구분, # 뒤는 주석, 빈 줄 무시, 중복 제거(처음 순서 유지), hostRe 로 검증
func readHostList(p string) ([]string, error) {
	b, err := os.ReadFile(p)
	if err != nil {
		return nil, fmt.Errorf("list 파일을 읽을 수 없습니다: %s", p)
	}
	seen := map[string]bool{}
	var hosts []string
	for n, line := range strings.Split(string(b), "\n") {
		if i := strings.IndexByte(line, '#'); i >= 0 {
			line = line[:i]
		}
		for _, h := range strings.FieldsFunc(line, func(r rune) bool { return r == ' ' || r == '\t' || r == ',' || r == '\r' }) {
			if !hostRe.MatchString(h) {
				return nil, fmt.Errorf("list %d행: 잘못된 호스트 이름 %q", n+1, h)
			}
			if !seen[h] {
				seen[h] = true
				hosts = append(hosts, h)
			}
		}
	}
	if len(hosts) == 0 {
		return nil, fmt.Errorf("list 파일에 호스트가 없습니다: %s", p)
	}
	return hosts, nil
}

// ltMkdir: 0700 디렉터리 (umask 와 무관하게)
func ltMkdir(d string) error {
	if err := os.MkdirAll(d, 0700); err != nil {
		return err
	}
	return os.Chmod(d, 0700)
}

// ltHost: 호스트 한 대 — <outDir>/<호스트>/ 를 비우고 다시 만든 뒤 backup/·rhel7/·rhel8/ 을 쓴다.
// 판정은 auto_setup Backup 과 같다(OS 판별 불가·ldap.conf 없음이면 백업 실패). 변환 세트는 저장한 backup/ 을
// auto_setup 복원과 같은 loadBackupDir 로 다시 읽어 만든다.
func ltHost(outDir, host string, c lbCollected) ltResult {
	r := ltResult{host: host, dir: filepath.Join(outDir, host)}
	fail := func(why string) ltResult {
		r.backup = why
		r.kinds = [2]string{"백업 없음", "백업 없음"}
		return r
	}
	if err := os.RemoveAll(r.dir); err != nil {
		return fail("이전 결과 삭제 실패")
	}
	if err := ltMkdir(r.dir); err != nil {
		return fail("디렉터리 생성 실패")
	}
	if !c.OK {
		return fail(c.Why)
	}
	in := c.Info
	r.os, r.auth, r.time = in.OS, in.Auth, in.Time
	switch {
	case !in.Seen || in.OS == 0 || in.Auth == "":
		return fail("OS 판별 불가")
	case !hasFile(in.Files, "ldap.conf"):
		return fail("ldap.conf 없음")
	}
	bak := filepath.Join(r.dir, "backup")
	if err := ltMkdir(bak); err != nil {
		return fail("저장 실패")
	}
	if err := writeBackupFiles(bak, ltJob, host, in); err != nil {
		return fail("저장 실패")
	}
	m, es, err := loadBackupDir(bak)
	if err != nil {
		return fail("백업 읽기 실패: " + backupReadWhy(err))
	}
	r.entries = es
	for i, k := range ltKinds {
		r.kinds[i] = ltWriteKind(filepath.Join(r.dir, k.dir), m, es, k.auth, k.time)
	}
	return r
}

// ltWriteKind: convertEntriesFor(es, auth, tk) 결과를 dir 에 쓰고 files.txt(이름·원격 경로·mode·owner·group·크기)와
// notes.txt(변환 내역)를 남긴다. 반환: "" = OK, 아니면 실패 사유(파일 이름만).
func ltWriteKind(dir string, m ldapMeta, es []lbEntry, auth, tk string) string {
	set, conv, err := convertEntriesFor(es, auth, tk)
	if err != nil {
		return err.Error()
	}
	if err := ltMkdir(dir); err != nil {
		return "디렉터리 생성 실패"
	}
	var fl strings.Builder
	fmt.Fprintf(&fl, "# %s → %s/%s 용 파일 세트 (원본 OS%d %s/%s, 수집 %s)\n# 이름\t원격 경로\tmode\towner\tgroup\t크기\n",
		m.Host, auth, tk, m.OSMajor, m.Auth, m.Time, m.Collected)
	for _, e := range set {
		if err := writeFile0600(filepath.Join(dir, e.Name), e.Data); err != nil {
			return "저장 실패"
		}
		fmt.Fprintf(&fl, "%s\t%s\t%s\t%s\t%s\t%d\n", e.Name, lbFilePaths[e.Name], e.Mode, e.Owner, e.Group, len(e.Data))
	}
	note := "변환 없음(원본 그대로)"
	if len(conv) > 0 {
		note = "변환: " + strings.Join(conv, ", ")
	}
	if writeFile0600(filepath.Join(dir, "files.txt"), []byte(fl.String())) != nil ||
		writeFile0600(filepath.Join(dir, "notes.txt"), []byte(note+"\n")) != nil {
		return "저장 실패"
	}
	return ""
}

// ltDirection: 변환 내역의 방향 (model.go ldapRestoredNote 와 같은 기준)
func ltDirection(conv string) string {
	to8 := strings.Contains(conv, "→sssd.conf") || strings.Contains(conv, "→chrony.conf")
	to7 := strings.Contains(conv, "→nslcd.conf") || strings.Contains(conv, "→ntp.conf")
	switch {
	case to8 && !to7:
		return "7→8"
	case to7 && !to8:
		return "8→7"
	}
	return "7/8 혼합"
}

// ltSvcOrder: 서비스 표시 순서 (복원 스크립트의 재시작 순서)
var ltSvcOrder = map[string]int{"sssd": 0, "nslcd": 0, "autofs": 1, "chronyd": 2, "ntpd": 2, "nscd": 3}

// ltTarget: src 백업으로 target 을 compareEntries → (필요하면 확인 후) restoreEntries. auto_setup request ldap-restore 와 같은 규칙.
func ltTarget(stdin io.Reader, w io.Writer, src ltResult, target string, yes bool) int {
	fmt.Fprintf(w, "\n== target %s : %s 의 백업으로 비교 (auto_setup 과 같은 규칙) ==\n", target, src.host)
	if src.backup != "" {
		fmt.Fprintf(w, "[X] %s 백업 실패로 비교하지 않음: %s\n", src.host, src.backup)
		return 1
	}
	st, p, route := compareEntries("local", target, filepath.Join(src.dir, "backup"), src.entries)
	if route == "os6" {
		fmt.Fprintln(w, "경로: os8 무응답 → os6_mgmt 경유")
	}
	if p == nil {
		if st.Bindpw == BindpwSame {
			fmt.Fprintln(w, "비교: 같음 (binddn uid·인증/시각 종류가 백업과 같음) — 복원하지 않음")
			return 0
		}
		fmt.Fprintln(w, "비교: 복원하지 않음 — "+st.Reason)
		return 1
	}
	why, conv := "binddn uid 다름", ""
	for _, n := range p.notes {
		switch {
		case n == "새 OS binddn 없음" || n == "OS 종류 변경":
			why = n
		case strings.HasPrefix(n, "변환 "):
			conv = strings.TrimPrefix(n, "변환 ")
		}
	}
	fmt.Fprintln(w, "비교: 복원 필요 — "+why)
	if conv == "" {
		fmt.Fprintln(w, "변환: 없음(백업과 같은 종류)")
	} else {
		fmt.Fprintf(w, "변환: %s (%s)\n", ltDirection(conv), conv)
	}
	fmt.Fprintln(w, "복원할 파일:")
	for _, e := range p.restore {
		fmt.Fprintf(w, "  %-22s %-26s %s %s:%s\n", e.Name, lbFilePaths[e.Name], e.Mode, e.Owner, e.Group)
	}
	if !yes {
		fmt.Fprintf(w, "%s 의 위 파일을 덮어쓰고 서비스(sssd|nslcd autofs chronyd|ntpd nscd)를 재시작합니다. 계속하려면 yes 입력: ", target)
		line, _ := bufio.NewReader(stdin).ReadString('\n')
		if strings.TrimSpace(line) != "yes" {
			fmt.Fprintln(w, "\n취소 — 대상에 아무것도 쓰지 않았습니다")
			return 1
		}
	}
	st = restoreEntries(target, p)
	svcErr := false
	if r := p.done; r != nil && r.Seen {
		if r.Share != "ok" {
			fmt.Fprintln(w, "공유경로: FAIL(대상이 읽지 못함)")
		} else {
			fmt.Fprintln(w, "파일:")
			for _, e := range p.restore {
				if r.Put[e.Name] {
					fmt.Fprintf(w, "  %-22s OK\n", e.Name)
				} else {
					fmt.Fprintf(w, "  %-22s FAIL\n", e.Name)
				}
			}
		}
		var svcs []string
		for n := range r.Svc {
			svcs = append(svcs, n)
		}
		for n := range r.SvcSkip {
			svcs = append(svcs, n)
		}
		sort.Slice(svcs, func(i, j int) bool {
			if ltSvcOrder[svcs[i]] != ltSvcOrder[svcs[j]] {
				return ltSvcOrder[svcs[i]] < ltSvcOrder[svcs[j]]
			}
			return svcs[i] < svcs[j]
		})
		if len(svcs) > 0 {
			var sb []string
			for _, n := range svcs {
				switch ok, seen := r.Svc[n]; {
				case r.SvcSkip[n]:
					sb = append(sb, n+" skip")
				case seen && ok:
					sb = append(sb, n+" ok")
				default:
					sb = append(sb, n+" err")
					svcErr = true
				}
			}
			fmt.Fprintln(w, "서비스 재시작: "+strings.Join(sb, ", "))
		}
		if r.BindUID == p.oldUID {
			fmt.Fprintln(w, "복원 후 binddn uid: 백업과 같음")
		} else {
			fmt.Fprintln(w, "복원 후 binddn uid: 백업과 다름")
		}
	}
	if !st.Applied {
		fmt.Fprintln(w, "결과: 복원 실패 — "+st.Reason)
		return 1
	}
	if st.Reason != "" {
		fmt.Fprintln(w, "결과: 복원 완료 ("+st.Reason+")")
	} else {
		fmt.Fprintln(w, "결과: 복원 완료")
	}
	if svcErr {
		return 1
	}
	return 0
}
