// ldap.go - LDAP·DNS·NTP·autofs·H/T 점검 (l 키, 계획서 4.6).
// 설치완료·재부팅됨 서버 중 결과가 비어 있는 서버만 gossh 로 점검하고, 결과는 state.json 에 저장한다.
package main

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// ldapCmd 는 원격 명령이다. ldap_check 뒤에 H/T 줄(setup_check buildInfoCmd 와 같은 lscpu | grep -i 'per core')을 붙인다.
// csh/tcsh 로그인 셸에서도 돌도록 변수 대입·중괄호·exit 없이 `;` 와 파이프, 작은따옴표만 쓴다.
// `;` 로 이어 ldap_check 가 실패(종료코드 1)해도 H/T 가 나온다.
func ldapCmd(sh string) string {
	if strings.ContainsAny(sh, " \t'\"") {
		sh = shQuote(sh)
	}
	return "bash " + sh + "; lscpu | grep -i 'per core'"
}

// ldapAcc 는 호스트 한 대의 ldap_check·H/T 출력 누적이다.
type ldapAcc struct {
	n       [3][2]int // [LDAP, DNS·NTP, autofs][OK, FAIL] 줄 수
	unknown bool      // /unknown (infra-mismatch)
	note    string
	one     int // 한 줄 요약 형식: 0 없음, 1 INFO(정상), 2 FAIL
	tpc     int // Thread(s) per core, 못 읽으면 0
}

const (
	catLDAP = iota
	catDNS
	catAutofs
)

// fileCat 은 ldap_check 파일 이름이 속한 열을 돌려준다(없으면 -1).
func fileCat(name string) int {
	switch {
	case name == "ldap.conf", name == "sssd.conf", name == "nslcd.conf":
		return catLDAP
	case name == "resolv.conf", name == "chrony.conf", name == "ntp.conf":
		return catDNS
	case strings.HasPrefix(name, "autofs"), strings.HasPrefix(name, "auto.appl"):
		return catAutofs
	}
	return -1
}

// parseLdapOutput 은 gossh 출력의 "host: 내용" 줄을 호스트별로 모은다. 모르는 줄은 무시한다.
// 줄 형식: 파일별 `<OK|FAIL>\t/<infra>\t<file>[\t부가]`, 한 줄 요약 `INFO\tLDAP\t<infra>\t<site>` /
// `FAIL\tLDAP\tUNDEFINED`(현재 ldap_check.sh), H/T `Thread(s) per core:  2`.
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
		switch {
		case (f[0] == "OK" || f[0] == "FAIL") && len(f) >= 3 && strings.HasPrefix(f[1], "/"):
			x := get()
			fail := 0
			if f[0] == "FAIL" {
				fail = 1
			}
			name := strings.Fields(f[2] + " ")[0]
			if f[1] == "/unknown" {
				x.unknown = true
				if name == "infra-mismatch" {
					x.note = "인프라 불일치 (" + strings.TrimSpace(strings.Join(f[3:], " ")) + ")"
				}
			} else if c := fileCat(name); c >= 0 {
				x.n[c][fail]++
			}
		case f[0] == "INFO" && len(f) >= 2 && f[1] == "LDAP":
			get().one = 1
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

// summarize 는 호스트 한 대의 누적을 Check 로 만든다(4.6 요약 규칙).
//
//	열별: FAIL 줄이 있으면 FAIL, 모두 OK 면 OK, 줄이 없으면 -
//	/unknown(인프라 불일치) 또는 FAIL LDAP UNDEFINED: 세 열 모두 FAIL, Note 에 원인
//	INFO LDAP: 세 열 모두 OK
//	H/T: 2 이상 ON, 1 OFF, 모르면 -
func (a *ldapAcc) summarize() Check {
	var v [3]string
	for i := range v {
		switch {
		case a.n[i][1] > 0:
			v[i] = "FAIL"
		case a.n[i][0] > 0:
			v[i] = "OK"
		default:
			v[i] = "-"
		}
	}
	note := ""
	switch {
	case a.unknown || a.one == 2:
		v = [3]string{"FAIL", "FAIL", "FAIL"}
		note = a.note
		if note == "" {
			note = "인프라 판별 불가 (/unknown)"
		}
	case a.one == 1:
		v = [3]string{"OK", "OK", "OK"}
	}
	ht := "-"
	switch {
	case a.tpc >= 2:
		ht = "ON"
	case a.tpc == 1:
		ht = "OFF"
	}
	return Check{LDAP: v[0], DNSNTP: v[1], Autofs: v[2], HT: ht, Note: note}
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
		if r := c.recs[h]; ldapDone(r.Status) && r.Check == (Check{}) {
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
// 비고가 있는 줄에만 맨 끝에 비고 열을 붙인다.
func ldapTable(snap *Snapshot) string {
	var recs []Record
	n, empty := 0, 0
	for _, r := range snap.Records {
		if !ldapDone(r.Status) {
			continue
		}
		n++
		if r.Check == (Check{}) {
			empty++
			continue
		}
		recs = append(recs, r)
	}
	sort.Slice(recs, func(i, j int) bool { return recs[i].Name < recs[j].Name })
	var sb strings.Builder
	sb.WriteString("hostname\tLDAP\tDNS · NTP\tautofs\tH/T\n")
	for _, r := range recs {
		ck := r.Check
		fmt.Fprintf(&sb, "%s\t%s\t%s\t%s\t%s", r.Name, ck.LDAP, ck.DNSNTP, ck.Autofs, ck.HT)
		if ck.Note != "" {
			sb.WriteString("\t" + ck.Note)
		}
		sb.WriteByte('\n')
	}
	if n == 0 {
		sb.WriteString("(설치완료 서버가 없습니다)\n")
	} else if empty > 0 {
		fmt.Fprintf(&sb, "(점검 결과 없음 %d대 - ssh 실패 등, 다시 열면 재시도)\n", empty)
	}
	return strings.TrimRight(sb.String(), "\n")
}
