// ldap.go - 서버 설정 조사(OS커널·appl·dns·ldap_URI·binddn·h/t)와 ldap_check 실행 (l 키·자동 첫 조사, 계획서 4.6·4.9).
// 결과는 Record.Check 로 state.json 에 저장한다. oklist 와의 비교는 oklist.go 에 있다.
package main

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// ldapCmdVer 는 Check.Ver 의 현재 값이다. 이보다 작은 값으로 저장된 결과는 새 형식이 아니므로 다시 조사한다.
const ldapCmdVer = 4 // 4: OS커널·appl·dns(순서 유지)·ldap_URI·binddn·h/t + ldap_check 원문 (v0.5.0)

// cfgAutoMax 는 수집 회차 하나에서 자동으로 첫 조사하는 서버의 최대 수다(계획서 4.9).
// gossh 는 한 번에 하나(nice 10)만 돌고 회차 안에서 실행되므로, 한 회차가 길어지지 않게 나눠서 조사한다.
// 8000대가 한꺼번에 대상이 되어도 회차(기본 60초)마다 100대씩 차례로 채워진다.
const cfgAutoMax = 100

// 원격 명령 출력 표식
const (
	mkCfgKern = "PM_KERN" // uname -r
	mkCfgHT   = "PM_HT"   // lscpu 의 Thread(s) per core 줄
	mkCfgDNS  = "PM_DNS"  // resolv.conf 의 nameserver 줄(파일 순서)
	mkCfgAppl = "PM_APPL" // auto.appl 의 /appl 줄
	mkCfgLdc  = "PM_LDC"  // openldap/ldap.conf 의 URI·binddn 줄(bindpw 는 읽지 않는다)
	mkCfgLC   = "PM_LC"   // ldap_check 출력 시작
	mkCfgLCE  = "PM_LCE"  // ldap_check 출력 끝
	mkCfgEnd  = "PM_END"  // 명령 끝까지 돌았음(조사 성공 판정)
	ldapConf  = "/etc/openldap/ldap.conf"
)

// ldapUnset 은 ldap_check_sh 가 설정되지 않았을 때 ldap_check결과 칸에 쓰는 값이다.
const ldapUnset = "ldap_check_sh 미설정"

// ldapCmd 는 원격 명령이다. 설정 파일 줄과 ldap_check 출력을 PM_ 표식을 붙여 한 번에 가져온다.
// csh/tcsh 로그인 셸에서도 돌도록 변수·중괄호·$(...)·2> 없이 `;` `&&` 파이프, 작은따옴표만 쓴다.
// 파일이 없을 때 오류 출력이 나오지 않게 `test -f 파일 && grep ...` 로 쓴다. `;` 로 이어 앞 명령이 실패해도 뒤가 돈다.
// 옛 gossh 는 명령 앞뒤의 작은따옴표를 잘라 내므로 명령은 따옴표로 끝나지 않게 echo PM_END 를 맨 끝에 둔다.
// ldap.conf 는 첫 낱말이 정확히 URI·binddn 인 줄만 grep 한다(bindpw 줄은 절대 읽지 않는다).
// sh 가 비어 있으면(ldap_check_sh 미설정) ldap_check 부분을 뺀다.
func ldapCmd(sh string) string {
	pick := func(file, tag string, pats ...string) string {
		g := "grep"
		for _, p := range pats {
			g += " -e '" + p + "'"
		}
		return "test -f " + file + " && " + g + " " + file + " | sed 's/^/" + tag + " /'"
	}
	parts := []string{
		"uname -r | sed 's/^/" + mkCfgKern + " /'",
		"lscpu | grep -i 'per core' | sed 's/^/" + mkCfgHT + " /'",
		pick("/etc/resolv.conf", mkCfgDNS, "^nameserver[[:space:]]"),
		pick("/etc/auto.appl", mkCfgAppl, `^/\?appl[[:space:]]`),
		pick(ldapConf, mkCfgLdc, "^[[:space:]]*URI[[:space:]]", "^[[:space:]]*binddn[[:space:]]"),
	}
	if sh != "" {
		if strings.ContainsAny(sh, " \t'\"") {
			sh = shQuote(sh)
		}
		parts = append(parts, "echo "+mkCfgLC, "bash "+sh, "echo "+mkCfgLCE)
	}
	parts = append(parts, "echo "+mkCfgEnd)
	return strings.Join(parts, "; ")
}

// ldapAcc 는 호스트 한 대의 조사 출력 누적이다.
type ldapAcc struct {
	kern   string
	tpc    int // Thread(s) per core, 못 읽으면 0
	dns    []string
	appl   string
	uris   []string
	binddn string
	bindOK bool
	lc     []string // ldap_check 출력 줄
	inLC   bool
	sawLC  bool
	end    bool // PM_END 를 봤다
}

// uriHost 는 ldap.conf URI 의 값 하나를 호스트 이름만 남긴다: 스킴(ldap:// ldaps:// ldapi://), :포트, 경로·끝 / 를 뗀다.
func uriHost(tok string) string {
	if i := strings.Index(tok, "://"); i >= 0 {
		tok = tok[i+3:]
	}
	if i := strings.Index(tok, "/"); i >= 0 {
		tok = tok[:i]
	}
	if strings.HasPrefix(tok, "[") { // [IPv6]:port
		if i := strings.Index(tok, "]"); i >= 0 {
			return tok[1:i]
		}
		return tok
	}
	if i := strings.Index(tok, ":"); i >= 0 {
		tok = tok[:i]
	}
	return tok
}

// ldcLine 은 ldap.conf 줄(앞의 PM_LDC 를 뗀 것)을 읽어 acc 에 넣는다. 첫 낱말은 대소문자를 구분한다(uri·BINDDN 은 무시).
func (a *ldapAcc) ldcLine(s string) {
	w := strings.Fields(s)
	if len(w) == 0 {
		return
	}
	switch w[0] {
	case "URI":
		for _, t := range w[1:] {
			if h := uriHost(t); h != "" {
				a.uris = append(a.uris, h)
			}
		}
	case "binddn":
		if !a.bindOK {
			a.bindOK = true
			s = strings.TrimSpace(s)
			a.binddn = normCell(strings.TrimSpace(s[len("binddn"):]))
		}
	}
}

// lcClean 은 ldap_check 출력 한 줄의 탭(연속 탭 포함)을 공백 하나로 바꾼다.
func lcClean(s string) string {
	var sb strings.Builder
	tab := false
	for _, r := range s {
		if r == '\t' {
			if !tab {
				sb.WriteByte(' ')
			}
			tab = true
			continue
		}
		tab = false
		sb.WriteRune(r)
	}
	return strings.TrimSpace(sb.String())
}

// parseLdapOutput 은 gossh 출력의 "host: 내용" 줄을 호스트별로 모은다. 모르는 줄은 무시한다.
// gossh 가 종료코드 != 0 일 때 붙이는 `ERROR: ` 접두사는 떼고 읽는다.
// PM_LC 와 PM_LCE 사이 줄은 ldap_check 출력이다(마지막 줄에 줄바꿈이 없으면 "...PM_LCE" 로 붙어 와도 읽는다).
func parseLdapOutput(out string) map[string]*ldapAcc {
	res := map[string]*ldapAcc{}
	for _, line := range strings.Split(out, "\n") {
		host, val, ok := splitHostLine(line)
		if !ok {
			continue
		}
		val = strings.TrimPrefix(val, "ERROR: ")
		x := res[host]
		get := func() *ldapAcc {
			if x == nil {
				x = &ldapAcc{}
				res[host] = x
			}
			return x
		}
		if x != nil && x.inLC {
			switch {
			case val == mkCfgLCE:
				x.inLC = false
			case strings.HasSuffix(val, mkCfgLCE):
				x.lc = append(x.lc, lcClean(strings.TrimSuffix(val, mkCfgLCE)))
				x.inLC = false
			case strings.TrimSpace(val) != "":
				x.lc = append(x.lc, lcClean(val))
			}
			continue
		}
		w := strings.Fields(val)
		if len(w) == 0 {
			continue
		}
		rest := strings.TrimSpace(strings.TrimPrefix(val, w[0]))
		switch w[0] {
		case mkCfgKern:
			if len(w) >= 2 && get().kern == "" {
				x.kern = w[1]
			}
		case mkCfgHT:
			if i := strings.LastIndex(val, ":"); i >= 0 {
				if n, err := strconv.Atoi(strings.TrimSpace(val[i+1:])); err == nil {
					get().tpc = n
				}
			}
		case mkCfgDNS:
			if len(w) >= 3 {
				get().dns = append(get().dns, w[2])
			}
		case mkCfgAppl:
			if len(w) >= 3 && strings.TrimPrefix(w[1], "/") == "appl" && get().appl == "" {
				x.appl = w[len(w)-1]
			}
		case mkCfgLdc:
			get().ldcLine(rest)
		case mkCfgLC:
			get().inLC, x.sawLC = true, true
		case mkCfgEnd:
			get().end = true
		}
	}
	return res
}

// summarize 는 호스트 한 대의 누적을 Check 로 만든다(4.9 칸 규칙). sh 는 ldap_check_sh 설정값이다.
func (a *ldapAcc) summarize(sh string) Check {
	ht := ""
	switch {
	case a.tpc >= 2:
		ht = "ON"
	case a.tpc == 1:
		ht = "OFF"
	}
	lc := strings.Join(a.lc, " ; ")
	switch {
	case sh == "":
		lc = ldapUnset
	case lc == "":
		lc = "(출력 없음)"
	}
	return Check{
		Ver:       ldapCmdVer,
		Kernel:    normCell(a.kern),
		Appl:      normCell(a.appl),
		DNS:       strings.Join(a.dns, " "),
		LdapURI:   strings.Join(a.uris, " "),
		BindDN:    a.binddn,
		HT:        ht,
		LdapCheck: lc,
	}
}

// LdapQuery 는 hosts 에 조사 명령을 gossh 로 한 번 실행한다. 명령 끝(PM_END)까지 응답한 호스트는 res,
// 그렇지 않은 호스트(ssh 실패·무응답)는 fails 에 이유(그 호스트의 첫 stderr 줄, 없으면 "응답 없음")와 함께 넣는다.
func LdapQuery(cfg *Config, hosts []string) (res map[string]Check, fails map[string]string, err error) {
	res, fails = map[string]Check{}, map[string]string{}
	if len(hosts) == 0 {
		return res, fails, nil
	}
	so, se, err := runGosshList(cfg, hosts, nil, ldapCmd(cfg.LdapCheckSh))
	if err != nil {
		return nil, nil, err
	}
	acc := parseLdapOutput(string(so))
	reason := map[string]string{}
	for _, l := range strings.Split(string(se), "\n") {
		if h, v, ok := splitHostLine(l); ok && v != "" {
			if _, seen := reason[h]; !seen {
				reason[h] = strings.TrimPrefix(v, "ERROR: ")
			}
		}
	}
	for _, h := range hosts {
		if a := acc[h]; a != nil && a.end {
			res[h] = a.summarize(cfg.LdapCheckSh)
			continue
		}
		r := reason[h]
		if r == "" {
			r = "응답 없음"
		}
		fails[h] = r
	}
	return res, fails, nil
}

// ldapTarget 은 서버 하나가 `l` 재조사 대상인지 알려준다(계획서 4.9).
// OS 모드(호스트파일 두 번째 칸이 yum 이 아님) 서버는 상태와 관계없이 모두 대상이다(이번 작업은 OS 설치 없이 yum·설정 변경만 한다).
// yum 서버는 yum_update/접속가능만이다.
func ldapTarget(r Record) bool {
	if r.IsYum() {
		return r.Status == StatusYumDone
	}
	return true
}

// autoTarget 은 수집 회차의 자동 첫 조사 대상이 될 수 있는지 알려준다(계획서 4.9).
// OS 모드 서버는 ping 이 닿는 상태(확인전·ping X·완료후꺼짐이 아님)면 상태와 관계없이 대상이다.
// ping X 서버는 자동으로 다시 시도하지 않는다(매 회차 ssh 시간 초과를 쓰지 않게, `l` 로만 조사).
func autoTarget(r Record) bool {
	if r.IsYum() {
		return r.Status == StatusYumDone
	}
	return r.Status != StatusNone && r.Status != StatusPingX && r.Status != StatusOff
}

// ldapTargets 는 `l` 재조사 대상 전체(호스트파일 순서)다. 결과가 있어도 다시 조사한다.
func (c *Collector) ldapTargets() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	var out []string
	for _, h := range c.hosts {
		if ldapTarget(c.recs[h]) {
			out = append(out, h)
		}
	}
	return out
}

// autoTargets 는 자동 첫 조사 대상(현재 형식 결과가 없는 서버, 호스트파일 순서) 중 앞의 max 대다.
func (c *Collector) autoTargets(max int) []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	var out []string
	for _, h := range c.hosts {
		if len(out) >= max {
			break
		}
		if r := c.recs[h]; autoTarget(r) && !r.Check.current() {
			out = append(out, h)
		}
	}
	return out
}

// LDAPPending 은 `l` 재조사 대상 수다.
func (c *Collector) LDAPPending() int { return len(c.ldapTargets()) }

// cfgQuery 는 조사 함수(Deps.Cfg, 없으면 LdapQuery)를 부른다.
func (c *Collector) cfgQuery(hosts []string) (map[string]Check, map[string]string, error) {
	if c.deps.Cfg != nil {
		return c.deps.Cfg(c.cfg, hosts)
	}
	return LdapQuery(c.cfg, hosts)
}

// applyChecks 는 조사 결과를 기록한다. 응답한 서버는 새 값으로 바꾸고, 응답하지 않은 서버는
// 이전 값을 그대로 두고 비고(Note)에만 이유를 남긴다(값을 비우지 않는다). 저장·스냅샷은 하지 않는다.
func (c *Collector) applyChecks(m map[string]Check, fails map[string]string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for h, ck := range m {
		if r, ok := c.recs[h]; ok {
			r.Check = ck
			c.recs[h] = r
		}
	}
	for h, why := range fails {
		if r, ok := c.recs[h]; ok {
			r.Check.Note = "최근 조사 실패: " + why
			c.recs[h] = r
		}
	}
}

// SetChecks 는 조사 결과를 기록하고 저장(바뀐 때만)한 뒤 스냅샷을 내보낸다.
func (c *Collector) SetChecks(m map[string]Check) error {
	return c.setChecks(m, nil)
}

func (c *Collector) setChecks(m map[string]Check, fails map[string]string) error {
	if len(m) == 0 && len(fails) == 0 {
		return nil
	}
	c.applyChecks(m, fails)
	err := c.save()
	c.publish(false)
	return err
}

// LDAPReport 는 `l` 키의 재조사다. 대상 전체를 다시 조사하고 결과 안내 한 줄을 돌려준다.
// 표 자체는 스냅샷(Record.Check)으로 화면이 그린다(포그라운드·데몬 화면 같음).
func (c *Collector) LDAPReport() string {
	t := c.ldapTargets()
	if len(t) == 0 {
		return "조사할 서버가 없습니다"
	}
	res, fails, err := c.cfgQuery(t)
	if err != nil {
		return "점검 실패: " + err.Error()
	}
	serr := c.setChecks(res, fails)
	now := time.Now()
	if c.deps.Now != nil {
		now = c.deps.Now()
	}
	msg := fmt.Sprintf("점검 완료 %s · 응답 %d대 · 무응답 %d대", now.Format("15:04:05"), len(res), len(fails))
	if len(fails) > 0 {
		msg += "(이전 값 유지)"
		var names []string
		for _, h := range t {
			if _, ok := fails[h]; ok {
				names = append(names, h)
				if len(names) == 20 {
					names = append(names, "...")
					break
				}
			}
		}
		msg += ": " + strings.Join(names, " ")
	}
	if serr != nil {
		msg += " (state 저장 실패: " + serr.Error() + ")"
	}
	return msg
}

// ldapRowCells 는 `l` 표·복사용 표의 한 줄 8칸이다. 현재 형식 결과가 없으면 설정 칸은 비우고 ldap_check결과 칸은 (조사 전)이다.
// 마지막 조사가 실패했으면 ldap_check결과 칸 끝에 비고를 덧붙인다.
func ldapRowCells(r Record) [8]string {
	ck := r.Check
	var out [8]string
	out[0] = r.Name
	lc := "(조사 전)"
	if ck.current() {
		v := ck.vals()
		copy(out[1:7], v[:])
		lc = ck.LdapCheck
	}
	if ck.Note != "" {
		lc += " [" + ck.Note + "]"
	}
	out[7] = lc
	return out
}

// ldapHeader 는 `l` 표의 열 제목이다.
var ldapHeader = [8]string{"hostname", "OS커널", "appl", "dns", "ldap_URI", "binddn", "h/t", "ldap_check결과"}

// ldapTable 은 `l` 표의 탭 구분 평문(복사용)이다. 호스트파일 순서로 모든 서버를 쓴다.
func ldapTable(snap *Snapshot) string {
	var sb strings.Builder
	sb.WriteString(strings.Join(ldapHeader[:], "\t"))
	for _, i := range snap.hostOrder() {
		c := ldapRowCells(snap.Records[i])
		for j := range c {
			c[j] = oneLine(c[j])
		}
		sb.WriteString("\n" + strings.Join(c[:], "\t"))
	}
	return sb.String()
}
