// ldap.go - LDAP·DNS·NTP·autofs·H/T 점검 (l 키, 계획서 4.6).
// 설치완료·재부팅됨 서버 중 결과가 비어 있는 서버만 gossh 로 점검하고, 결과는 state.json 에 저장한다.
package main

import (
	"bytes"
	"fmt"
	"net"
	"reflect"
	"sort"
	"strconv"
	"strings"
)

// ldapCmdVer 는 Check.Ver 의 현재 값이다. 이보다 작은 값으로 저장된 결과는 새 형식이 아니므로 다시 조사한다.
const ldapCmdVer = 3 // 3: NTP 호스트 이름을 노드에서 IP 로 풀어 비교

// ldapCmd 는 원격 명령이다. ldap_check 와 H/T 줄(setup_check buildInfoCmd 와 같은 lscpu | grep -i 'per core')에 더해
// DNS·NTP·autofs 원본 줄을 PM_DNS / PM_NTPC / PM_NTPN / PM_APPL 표식을 붙여 한 번에 가져온다.
// csh/tcsh 로그인 셸에서도 돌도록 변수·중괄호·$(...)·2> 없이 `;` `&&` 파이프, 작은따옴표만 쓴다.
// 파일이 없을 때 오류 출력이 나오지 않게 `test -f 파일 && grep ...` 로 쓴다. `;` 로 이어 앞 명령이 실패해도 뒤가 돈다.
// 옛 gossh 는 명령 앞뒤의 작은따옴표를 잘라 내므로 명령은 따옴표로 끝나지 않게 PM_CHRONY(echo)를 맨 끝에 둔다.
// chrony.conf 가 있으면 PM_CHRONY 를 찍어 두고, 없을 때만 ntp.conf 줄(PM_NTPN)을 쓰는 판단은 Go 에서 한다.
func ldapCmd(sh string) string {
	if strings.ContainsAny(sh, " \t'\"") {
		sh = shQuote(sh)
	}
	pick := func(file, tag string, pats ...string) string {
		g := "grep"
		for _, p := range pats {
			g += " -e '" + p + "'"
		}
		return "test -f " + file + " && " + g + " " + file + " | sed 's/^/" + tag + " /'"
	}
	// resolve 는 NTP 설정의 server·pool 두 번째 칸(이름 또는 IP)을 그 노드에서 getent hosts 로 IP 로 푼다.
	// DNS 가 느린 노드에서 l 점검 전체가 묶이지 않도록 이름 하나당 5초(timeout)로 끊는다(못 풀면 비고에 '풀이 실패').
	// 출력 "<tag> <ip> <정식이름> [별칭...]". xargs -r 이라 대상이 없으면 getent 를 돌리지 않는다(인자 없는 getent 는 hosts 전체를 출력).
	resolve := func(file, tag string) string {
		return "test -f " + file + " && grep -e '^server[[:space:]]' -e '^pool[[:space:]]' " + file +
			" | sed 's/^[a-z]*[[:space:]]*//' | sed 's/[[:space:]].*//' | xargs -r -n1 timeout 5 getent hosts | sed 's/^/" + tag + " /'"
	}
	return strings.Join([]string{
		"bash " + sh,
		"lscpu | grep -i 'per core'",
		pick("/etc/resolv.conf", "PM_DNS", "^nameserver[[:space:]]"),
		pick("/etc/chrony.conf", "PM_NTPC", "^server[[:space:]]", "^pool[[:space:]]"),
		pick("/etc/ntp.conf", "PM_NTPN", "^server[[:space:]]", "^pool[[:space:]]"),
		resolve("/etc/chrony.conf", "PM_NTPRC"),
		resolve("/etc/ntp.conf", "PM_NTPRN"),
		pick("/etc/auto.appl", "PM_APPL", `^/\?appl[[:space:]]`, `^/\?wappl[[:space:]]`),
		"test -f /etc/chrony.conf && echo PM_CHRONY",
	}, "; ")
}

// ldapAcc 는 호스트 한 대의 ldap_check·H/T·DNS·NTP·autofs 출력 누적이다.
type ldapAcc struct {
	unknown  bool   // /unknown (infra-mismatch)
	note     string // ldap_check 쪽 원인
	one      int    // 한 줄 요약 형식: 0 없음, 1 INFO/OK(정상), 2 FAIL
	infra    string // LDAP 칸에 쓸 인프라 이름
	fileFail bool   // 파일별 형식: LDAP 파일(ldap/sssd/nslcd.conf)에 FAIL 이 있다
	tpc      int    // Thread(s) per core, 못 읽으면 0
	dns      []string
	ntpC     []string   // chrony.conf 의 server·pool
	ntpN     []string   // ntp.conf 의 server·pool
	chrony   bool       // chrony.conf 가 있다
	resC     [][]string // chrony.conf 이름 풀이(getent hosts): [ip, 이름, 별칭...]
	resN     [][]string // ntp.conf 이름 풀이
	appl     string
	wappl    string
}

// isLdapFile 은 ldap_check 파일별 형식의 LDAP 열 파일 이름인지 알려준다.
func isLdapFile(name string) bool {
	return name == "ldap.conf" || name == "sssd.conf" || name == "nslcd.conf"
}

// parseLdapOutput 은 gossh 출력의 "host: 내용" 줄을 호스트별로 모은다. 모르는 줄은 무시한다.
// 줄 형식: 한 줄 요약 `INFO|OK\tLDAP\t<infra>[\t<site>]` / `FAIL\tLDAP\tUNDEFINED`(현재 ldap_check.sh),
// 파일별(옛 형식) `<OK|FAIL>\t/<infra>\t<file>[\t부가]`, H/T `Thread(s) per core:  2`,
// 조사 명령의 `PM_DNS nameserver <ip>` `PM_NTPC|PM_NTPN server <ip> ...` `PM_CHRONY` `PM_APPL <key> ... <마운트>`.
// gossh 가 종료코드 != 0 일 때 붙이는 `ERROR: ` 접두사는 떼고 읽는다.
func parseLdapOutput(out string) map[string]*ldapAcc {
	res := map[string]*ldapAcc{}
	for _, line := range strings.Split(out, "\n") {
		host, val, ok := splitHostLine(line)
		if !ok {
			continue
		}
		val = strings.TrimPrefix(val, "ERROR: ")
		f := strings.Split(val, "\t")
		get := func() *ldapAcc {
			if res[host] == nil {
				res[host] = &ldapAcc{}
			}
			return res[host]
		}
		w := strings.Fields(val)
		switch {
		case len(w) >= 1 && w[0] == "PM_CHRONY":
			get().chrony = true
		case len(w) >= 3 && w[0] == "PM_DNS":
			x := get()
			x.dns = append(x.dns, w[2])
		case len(w) >= 3 && w[0] == "PM_NTPC":
			x := get()
			x.ntpC = append(x.ntpC, w[2])
		case len(w) >= 3 && w[0] == "PM_NTPN":
			x := get()
			x.ntpN = append(x.ntpN, w[2])
		case len(w) >= 3 && w[0] == "PM_NTPRC":
			x := get()
			x.resC = append(x.resC, w[1:])
		case len(w) >= 3 && w[0] == "PM_NTPRN":
			x := get()
			x.resN = append(x.resN, w[1:])
		case len(w) >= 3 && w[0] == "PM_APPL":
			x := get()
			v := w[len(w)-1]
			switch strings.TrimPrefix(w[1], "/") {
			case "appl":
				if x.appl == "" {
					x.appl = v
				}
			case "wappl":
				if x.wappl == "" {
					x.wappl = v
				}
			}
		case (f[0] == "OK" || f[0] == "FAIL") && len(f) >= 3 && strings.HasPrefix(f[1], "/"):
			x := get()
			name := strings.Fields(f[2] + " ")[0]
			if f[1] == "/unknown" {
				x.unknown = true
				if name == "infra-mismatch" {
					x.note = "인프라 불일치 (" + strings.TrimSpace(strings.Join(f[3:], " ")) + ")"
				}
			} else if isLdapFile(name) {
				if f[0] == "FAIL" {
					x.fileFail = true
				} else if name == "ldap.conf" || x.infra == "" {
					x.infra = f[1][1:]
				}
			}
		case (f[0] == "INFO" || f[0] == "OK") && len(f) >= 3 && f[1] == "LDAP":
			x := get()
			x.one, x.infra = 1, strings.TrimSpace(f[2])
		case f[0] == "FAIL" && len(f) >= 2 && f[1] == "LDAP":
			x := get()
			x.one = 2
			x.note = strings.TrimSpace("ldap_check " + strings.Join(f[2:], " "))
		case strings.Contains(strings.ToLower(val), "per core"):
			if i := strings.LastIndex(val, ":"); i >= 0 {
				if n, err := strconv.Atoi(strings.TrimSpace(val[i+1:])); err == nil {
					get().tpc = n
				}
			}
		}
	}
	return res
}

// ipLess 는 IP 를 숫자 오름차순으로, IP 가 아닌 문자열은 그 뒤에 사전순으로 비교한다.
func ipLess(a, b string) bool {
	ia, ib := net.ParseIP(a), net.ParseIP(b)
	switch {
	case ia != nil && ib != nil:
		return bytes.Compare(ia.To16(), ib.To16()) < 0
	case ia != nil:
		return true
	case ib != nil:
		return false
	}
	return a < b
}

// sortedSet 은 중복을 없애고 ipLess 순으로 정렬한 집합이다.
func sortedSet(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	sort.Slice(out, func(i, j int) bool { return ipLess(out[i], out[j]) })
	return out
}

// resolveNames 는 NTP 설정 값 중 IP 가 아닌 이름을 노드의 getent hosts 결과(res: [ip, 정식이름, 별칭...])로 IP 로 바꾼다.
// 이름은 대소문자를 무시하고, 짧은 이름과 FQDN 의 앞부분이 같아도 맞는 것으로 본다. 못 푼 이름은 그대로 두고 unres 로 알려준다.
func resolveNames(vals []string, res [][]string) (out, unres []string) {
	for _, v := range vals {
		if net.ParseIP(v) != nil {
			out = append(out, v)
			continue
		}
		ip := ""
		for _, r := range res {
			for _, nm := range r[1:] {
				if sameHost(nm, v) {
					ip = r[0]
					break
				}
			}
			if ip != "" {
				break
			}
		}
		if ip == "" {
			out = append(out, v)
			unres = append(unres, v)
			continue
		}
		out = append(out, ip)
	}
	return out, unres
}

// sameHost 는 두 호스트 이름이 같은지(대소문자 무시, 끝의 '.' 무시, 짧은 이름 = FQDN 첫 마디) 알려준다.
func sameHost(a, b string) bool {
	a, b = strings.ToLower(strings.TrimSuffix(a, ".")), strings.ToLower(strings.TrimSuffix(b, "."))
	if a == b {
		return true
	}
	return strings.HasPrefix(a, b+".") || strings.HasPrefix(b, a+".")
}

// joinOrDash 는 "/" 로 이은 값이고 비었으면 "-" 다.
func joinOrDash(s []string) string {
	if len(s) == 0 {
		return "-"
	}
	return strings.Join(s, "/")
}

// summarize 는 호스트 한 대의 누적을 Check 로 만든다(4.6 열 규칙).
func (a *ldapAcc) summarize() Check {
	var notes []string
	ldap := "-"
	switch {
	case a.unknown:
		ldap = "FAIL"
		n := a.note
		if n == "" {
			n = "인프라 판별 불가 (/unknown)"
		}
		notes = append(notes, n)
	case a.one == 2:
		ldap = "FAIL"
		notes = append(notes, a.note)
	case a.fileFail:
		ldap = "FAIL"
	case a.infra != "":
		ldap = a.infra
	}
	// DNS · NTP: 순서·중복을 무시한 집합 비교
	ntp, res := a.ntpC, a.resC
	if !a.chrony {
		ntp, res = a.ntpN, a.resN
	}
	ntp, unres := resolveNames(ntp, res)
	if len(unres) > 0 {
		notes = append(notes, "NTP 이름 풀이 실패 "+strings.Join(unres, "/"))
	}
	d, n := sortedSet(a.dns), sortedSet(ntp)
	dn := "다름"
	switch {
	case len(d) == 0 && len(n) == 0:
		dn = "-"
	case reflect.DeepEqual(d, n):
		dn = strings.Join(d, "/")
	default:
		notes = append(notes, "DNS "+joinOrDash(d)+" NTP "+joinOrDash(n))
	}
	// autofs: appl·wappl 마운트 위치
	af := "-"
	switch {
	case a.appl != "" && a.wappl != "" && a.appl != a.wappl:
		af = "다름"
		notes = append(notes, "/appl "+a.appl+" /wappl "+a.wappl)
	case a.appl != "":
		af = a.appl
	case a.wappl != "":
		af = a.wappl
	}
	ht := "-"
	switch {
	case a.tpc >= 2:
		ht = "ON"
	case a.tpc == 1:
		ht = "OFF"
	}
	return Check{Ver: ldapCmdVer, LDAP: ldap, DNSNTP: dn, Autofs: af, HT: ht, Note: strings.Join(notes, "; ")}
}

// LdapQuery 는 hosts 에 ldap_check 와 H/T 를 gossh 로 실행해 응답한 호스트의 Check 를 돌려준다.
// 아무 출력도 없는 호스트(ssh 실패 등)는 결과에 넣지 않는다(다음에 다시 점검).
func LdapQuery(cfg *Config, hosts []string) (map[string]Check, error) {
	if len(hosts) == 0 {
		return map[string]Check{}, nil
	}
	so, se, err := runGosshList(cfg, hosts, nil, ldapCmd(cfg.LdapCheckSh))
	if err != nil {
		return nil, err
	}
	want := make(map[string]bool, len(hosts))
	for _, h := range hosts {
		want[h] = true
	}
	res := map[string]Check{}
	for h, a := range parseLdapOutput(string(so) + "\n" + string(se)) {
		if want[h] {
			res[h] = a.summarize()
		}
	}
	return res, nil
}

// ldapDone 은 점검 대상이 될 수 있는 상태(설치완료·재부팅됨)인지 알려준다. 완료후꺼짐은 응답이 없으므로 제외한다.
func ldapDone(s Status) bool { return s == StatusDone || s == StatusRebooted }

// ldapTargets 는 설치완료·재부팅됨이면서 결과가 비어 있는 서버(호스트파일 순서)다.
func (c *Collector) ldapTargets() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	var out []string
	for _, h := range c.hosts {
		if r := c.recs[h]; ldapDone(r.Status) && !r.Check.current() {
			out = append(out, h)
		}
	}
	return out
}

// LDAPPending 은 아직 점검하지 않은 서버 수다.
func (c *Collector) LDAPPending() int { return len(c.ldapTargets()) }

// SetChecks 는 점검 결과를 기록하고 저장(바뀐 때만)한 뒤 스냅샷을 내보낸다.
func (c *Collector) SetChecks(m map[string]Check) error {
	if len(m) == 0 {
		return nil
	}
	c.mu.Lock()
	for h, ck := range m {
		if r, ok := c.recs[h]; ok {
			r.Check = ck
			c.recs[h] = r
		}
	}
	c.mu.Unlock()
	err := c.save()
	c.publish(false)
	return err
}

// LDAPReport 는 `l` 패널 본문이다. 결과가 없는 완료 서버만 점검하고, 완료 서버 전체(저장된 값 포함)를 표로 돌려준다.
func (c *Collector) LDAPReport() string {
	if c.cfg.LdapCheckSh == "" {
		return "ldap_check_sh 미설정"
	}
	var errMsg string
	if t := c.ldapTargets(); len(t) > 0 {
		res, err := LdapQuery(c.cfg, t)
		if err == nil {
			err = c.SetChecks(res)
		}
		if err != nil {
			errMsg = "점검 실패: " + err.Error()
		}
	}
	out := ldapTable(c.Snapshot())
	if errMsg != "" {
		out = errMsg + "\n" + out
	}
	return out
}

// ldapTable 은 완료 서버(설치완료·재부팅됨) 중 결과가 있는 서버의 탭 구분 표다(hostname 순).
// 머리줄에 비고 칸이 항상 있고, 비고가 없는 줄은 빈 칸으로 둔다.
func ldapTable(snap *Snapshot) string {
	var recs []Record
	n, empty := 0, 0
	for _, r := range snap.Records {
		if !ldapDone(r.Status) {
			continue
		}
		n++
		if !r.Check.current() {
			empty++
			continue
		}
		recs = append(recs, r)
	}
	sort.Slice(recs, func(i, j int) bool { return recs[i].Name < recs[j].Name })
	var sb strings.Builder
	sb.WriteString("hostname\tLDAP\tDNS · NTP\tautofs\tH/T\t비고\n")
	for _, r := range recs {
		ck := r.Check
		fmt.Fprintf(&sb, "%s\t%s\t%s\t%s\t%s\t%s\n", r.Name, ck.LDAP, ck.DNSNTP, ck.Autofs, ck.HT, ck.Note)
	}
	if n == 0 {
		sb.WriteString("(설치완료 서버가 없습니다)\n")
	} else if empty > 0 {
		fmt.Fprintf(&sb, "(점검 결과 없음 %d대 - ssh 실패 등, 다시 열면 재시도)\n", empty)
	}
	return strings.TrimRight(sb.String(), "\n")
}
