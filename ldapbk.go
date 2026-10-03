// ldapbk.go - LDAP 백업/복원 (§14-5): 전달 시점 설정 수집(Backup), READY 직후 bindpw 비교·백업본 적용(Apply)
package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

// 대상 파일·OS 분기는 ldap_check/ldap_check.sh · ldap_setting(apply_body.sh) 과 같다.
//
//	공통        : /etc/openldap/ldap.conf , /etc/resolv.conf
//	인증(auth)  : OS 7 이하 → /etc/nslcd.conf ,  OS 8 이상 → /etc/sssd/sssd.conf (s4 호스트+services 에 nslcd → nslcd.conf)
//	시각(time)  : OS 7 이하 → /etc/ntp.conf   ,  OS 8 이상 → /etc/chrony.conf    (s4 호스트+services 에 ntp  → ntp.conf)
//
// s4 규칙(hostname 접두사)은 ldap_config.conf 의 s4.prefix / s4.services 값이며, auto_setup 은 그 파일을 읽지 않으므로
// 환경변수 AUTO_SETUP_LDAP_S4_PREFIX (비면 s4 규칙 꺼짐), AUTO_SETUP_LDAP_S4_SERVICES (기본 "nslcd,ntp") 로 받는다.
//
// bindpw 는 로그·상태·반환값·meta.json·에러 문자열 어디에도 남기지 않는다(sha256 해시만 비교, 원격에서도 해시만 회수).
// 단 백업 파일 자체(0600)에는 원본 설정이 들어 있고, 복원 때 파일 내용이 base64(2중) 로 gossh 명령줄에 실린다
// (ldap_setting 과 같은 방식).

const (
	ldapConfPath = "/etc/openldap/ldap.conf"
	lbMetaName   = "meta.json"
)

var (
	// ldapRoot: 원격 스크립트의 파일 경로 접두사(ldap_check 의 ROOT 와 같은 용도). 운영은 항상 빈 값 — 테스트 전용.
	// 비어 있지 않으면 서비스 재시작·restorecon 을 실제로 하지 않고 $ROOT/restart.log 에 기록만 한다.
	ldapRoot = ""

	ldapBackupTimeout   = 90 * time.Second // Backup 호출 1회(전체 호스트 배치) 상한
	ldapApplyTimeout    = 45 * time.Second // Apply 의 gossh 호출 1회 상한
	ldapMaxRestoreBytes = 48 * 1024        // 복원 파일 세트 크기 상한(명령줄 길이 제한)
)

func init() { newLdap = func() LdapBackup { return NewRealLdapBackup() } }

// ldapExec: gossh/ssh 실행 (stdout 만 반환, 시간 제한 포함). 테스트는 PATH 의 가짜 gossh/ssh 로 대체한다.
var ldapExec = func(timeout time.Duration, stdin string, name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.WaitDelay = 2 * time.Second
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}
	var out bytes.Buffer
	cmd.Stdout = &out
	err := cmd.Run()
	if ctx.Err() != nil {
		return out.String(), errors.New("시간 초과")
	}
	return out.String(), err
}

// ---- 파일 표 ----

// 백업 이름 → 원격 절대경로 (복원 때도 이 표만 사용 — meta.json 의 경로는 신뢰하지 않는다)
var lbFilePaths = map[string]string{
	"ldap.conf":   ldapConfPath,
	"resolv.conf": "/etc/resolv.conf",
	"nslcd.conf":  "/etc/nslcd.conf",
	"sssd.conf":   "/etc/sssd/sssd.conf",
	"ntp.conf":    "/etc/ntp.conf",
	"chrony.conf": "/etc/chrony.conf",
}

// 복원 순서 (resolv → ldap → 인증 → 시각)
var lbRestoreOrder = []string{"resolv.conf", "ldap.conf", "nslcd.conf", "sssd.conf", "ntp.conf", "chrony.conf"}

var (
	modeRe = regexp.MustCompile(`^[0-7]{3,4}$`)
	nameRe = regexp.MustCompile(`^[A-Za-z0-9._-]{1,32}$`)
	hostRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)
	jobRe  = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)
)

func kindOfName(name string) (class, kind string) {
	switch name {
	case "nslcd.conf":
		return "auth", "nslcd"
	case "sssd.conf":
		return "auth", "sssd"
	case "ntp.conf":
		return "time", "ntp"
	case "chrony.conf":
		return "time", "chrony"
	}
	return "", ""
}

// ---- 원격 스크립트 ----

func s4Config() (prefix, services string) {
	prefix = os.Getenv("AUTO_SETUP_LDAP_S4_PREFIX")
	services = os.Getenv("AUTO_SETUP_LDAP_S4_SERVICES")
	if services == "" {
		services = "nslcd,ntp"
	}
	return
}

// detectScript: HOST/REL/OS/KIND 줄을 내고 AK/TK/AF/TF 변수를 남긴다 (ldap_check.sh 의 판정과 동일)
func detectScript() string {
	p, s := s4Config()
	return `LC_ALL=C; export LC_ALL
R=` + shq(ldapRoot) + `
S4P=` + shq(p) + `
S4S=` + shq(s) + `
H=$(hostname -s 2>/dev/null || hostname)
REL=$(head -n1 "$R/etc/redhat-release" 2>/dev/null)
MAJ=$(sed -n 's/.*release \([0-9][0-9]*\).*/\1/p' "$R/etc/redhat-release" 2>/dev/null | head -n1)
[ -n "$MAJ" ] || MAJ=$(sed -n 's/^VERSION_ID="\{0,1\}\([0-9][0-9]*\).*/\1/p' "$R/etc/os-release" 2>/dev/null | head -n1)
IS4=0
if [ -n "$S4P" ]; then case "$H" in "$S4P"*) IS4=1 ;; esac; fi
s4has() { case ",$S4S," in *",$1,"*) return 0 ;; esac; return 1; }
AK=sssd; TK=chrony
if [ -n "$MAJ" ] && [ "$MAJ" -le 7 ]; then
  AK=nslcd; TK=ntp
else
  if [ "$IS4" = 1 ] && s4has nslcd; then AK=nslcd; fi
  if [ "$IS4" = 1 ] && s4has ntp; then TK=ntp; fi
fi
if [ "$AK" = nslcd ]; then AF=/etc/nslcd.conf; else AF=/etc/sssd/sssd.conf; fi
if [ "$TK" = ntp ]; then TF=/etc/ntp.conf; else TF=/etc/chrony.conf; fi
echo "==HOST $H"
echo "==REL $REL"
echo "==OS $MAJ"
echo "==KIND $AK $TK"
`
}

// bindpwSnippet: 현재 ldap.conf 의 bindpw 해시만 출력 (값은 변수에만 존재, 출력 안 함)
const bindpwSnippet = `BV=$(awk 'tolower($1)=="bindpw"{$1="";sub(/^[ \t]+/,"");print;exit}' "$R` + ldapConfPath + `" 2>/dev/null)
if [ -n "$BV" ]; then echo "==BINDPW $(printf '%s\n' "$BV" | sha256sum | cut -d' ' -f1)"; else echo "==BINDPW none"; fi
BV=
`

func collectScript() string {
	return detectScript() + `for f in ` + ldapConfPath + ` /etc/resolv.conf $AF $TF; do
  [ -f "$R$f" ] || continue
  st=$(stat -c '%a %U %G' "$R$f" 2>/dev/null) || continue
  b=$(base64 -w0 < "$R$f" 2>/dev/null)
  echo "==FILE $f $st $b"
done
`
}

func probeScript() string { return detectScript() + bindpwSnippet }

type lbEntry struct {
	Name, Mode, Owner, Group string
	Data                     []byte
}

func restoreScript(entries []lbEntry) string {
	var sb strings.Builder
	sb.WriteString(detectScript())
	sb.WriteString(`umask 077
put() {
  p="$R$2"; t="$p.as_ldap.$$"
  printf '%s' "$6" | base64 -d > "$t" 2>/dev/null && chmod "$3" "$t" 2>/dev/null && chown "$4:$5" "$t" 2>/dev/null && mv -f "$t" "$p" 2>/dev/null || { rm -f "$t"; echo "==PUT $1 err"; return 1; }
  if [ -n "$R" ]; then echo "restorecon $2" >> "$R/restart.log"; else command -v restorecon >/dev/null 2>&1 && restorecon "$p" >/dev/null 2>&1; fi
  echo "==PUT $1 ok"
}
svc() {
  if [ -n "$R" ]; then echo "restart $1" >> "$R/restart.log"; echo "==SVC $1 ok"; return 0; fi
  if systemctl restart "$1" >/dev/null 2>&1 || service "$1" restart >/dev/null 2>&1; then echo "==SVC $1 ok"; else echo "==SVC $1 err"; fi
}
`)
	for _, e := range entries {
		fmt.Fprintf(&sb, "put %s %s %s %s %s %s", e.Name, shq(lbFilePaths[e.Name]), e.Mode, e.Owner, e.Group,
			shq(base64.StdEncoding.EncodeToString(e.Data)))
		switch c, _ := kindOfName(e.Name); c {
		case "auth":
			sb.WriteString(" && A=1")
		case "time":
			sb.WriteString(" && T=1")
		}
		sb.WriteString("\n")
	}
	sb.WriteString(`if [ "$A" = 1 ]; then svc "$AK"; fi
if [ "$T" = 1 ]; then if [ "$TK" = ntp ]; then svc ntpd; else svc chronyd; fi; fi
`)
	sb.WriteString(bindpwSnippet)
	return "A=0; T=0\n" + sb.String()
}

// buildRemoteCmd: 스크립트를 2중 base64 로 감싼 gossh 명령 한 줄 (따옴표 없음 → csh/tcsh 로그인 셸에서도 동일).
// 바깥 base64 는 두 글자마다 '.' 을 끼워 gossh 위험어 검사(reboot/halt/ddc 등)에 우연히 걸리지 않게 한다.
func buildRemoteCmd(script string) string {
	b64 := base64.StdEncoding.EncodeToString([]byte(script))
	inner := "umask 077; t=$(mktemp /tmp/.as_ldap.XXXXXX) || exit 97; echo " + b64 + " | base64 -d > $t && bash $t </dev/null; rc=$?; rm -f $t; exit $rc"
	return "echo " + dotOut(base64.StdEncoding.EncodeToString([]byte(inner))) + " | tr -d . | base64 -d | bash"
}

func dotOut(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if i > 0 && i%2 == 0 {
			b.WriteByte('.')
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

// ---- gossh 호출 ----

// gosshBatch: hosts 에 같은 명령을 한 번에 실행 (route: local=gossh 직접, os6=os6_mgmt 경유). 호스트별 본문 줄을 돌려준다.
func gosshBatch(route string, hosts []string, cmd string, timeout time.Duration) (map[string][]string, error) {
	var out string
	var err error
	if route == "os6" {
		if os6_mgmt == "" || os6_gossh == "" {
			return nil, errors.New("os6_mgmt/os6_gossh 가 비어 있습니다")
		}
		remote := "f=$(mktemp); cat > $f; " + os6_gossh + " -pm -script -w $f " + shq(cmd) + "; rm -f $f ${f}_*"
		out, err = ldapExec(timeout, hostList(hosts), "ssh", os6_mgmt, remote)
	} else {
		f, e := os.CreateTemp("", "as_ldap_")
		if e != nil {
			return nil, e
		}
		p := f.Name()
		defer cleanupTemp(p)
		_, e = f.WriteString(hostList(hosts))
		f.Close()
		if e != nil {
			return nil, e
		}
		out, err = ldapExec(timeout, "", "gossh", "-pm", "-script", "-w", p, cmd)
	}
	res := splitGossh(out)
	if err != nil && len(res) == 0 {
		if _, ok := err.(*exec.ExitError); !ok {
			return nil, errors.New("gossh 실행 실패")
		}
	}
	return res, nil
}

// splitGossh: "host: 본문" 줄을 호스트별로 모은다 (호스트 자리에 공백이 있는 안내 줄은 무시)
func splitGossh(out string) map[string][]string {
	res := map[string][]string{}
	for _, l := range strings.Split(strings.ReplaceAll(out, "\r", ""), "\n") {
		h, body, ok := strings.Cut(l, ": ")
		if !ok || h == "" || strings.ContainsAny(h, " \t") {
			continue
		}
		res[h] = append(res[h], body)
	}
	return res
}

// ---- 파싱 ----

type lbInfo struct {
	Host, Rel string
	OS        int
	Auth      string
	Time      string
	BindpwSet bool   // ==BINDPW 줄 존재
	Bindpw    string // sha256 hex, "" = 없음
	Put       map[string]bool
	Svc       map[string]bool
	Files     []lbEntry
	Seen      bool // 응답(HOST 줄) 있음
}

func parseInfo(lines []string) lbInfo {
	in := lbInfo{Put: map[string]bool{}, Svc: map[string]bool{}}
	for _, l := range lines {
		switch {
		case strings.HasPrefix(l, "==HOST "):
			in.Host = strings.TrimSpace(l[7:])
			in.Seen = true
		case strings.HasPrefix(l, "==REL "):
			in.Rel = strings.TrimSpace(l[6:])
		case strings.HasPrefix(l, "==OS"):
			n := 0
			if _, err := fmt.Sscanf(strings.TrimSpace(l[4:]), "%d", &n); err == nil {
				in.OS = n
			}
		case strings.HasPrefix(l, "==KIND "):
			f := strings.Fields(l[7:])
			if len(f) == 2 && (f[0] == "sssd" || f[0] == "nslcd") && (f[1] == "chrony" || f[1] == "ntp") {
				in.Auth, in.Time = f[0], f[1]
			}
		case strings.HasPrefix(l, "==BINDPW "):
			v := strings.TrimSpace(l[9:])
			in.BindpwSet = true
			if len(v) == 64 {
				in.Bindpw = v
			}
		case strings.HasPrefix(l, "==PUT "):
			f := strings.Fields(l[6:])
			if len(f) == 2 {
				in.Put[f[0]] = f[1] == "ok"
			}
		case strings.HasPrefix(l, "==SVC "):
			f := strings.Fields(l[6:])
			if len(f) == 2 {
				in.Svc[f[0]] = f[1] == "ok"
			}
		case strings.HasPrefix(l, "==FILE "):
			if e, ok := parseFileLine(l[7:]); ok {
				in.Files = append(in.Files, e)
			}
		}
	}
	return in
}

// parseFileLine: "<경로> <mode> <owner> <group> [b64]" — 경로는 표에 있는 것만 인정
func parseFileLine(s string) (lbEntry, bool) {
	f := strings.Fields(s)
	if len(f) < 4 || len(f) > 5 {
		return lbEntry{}, false
	}
	name := ""
	for n, p := range lbFilePaths {
		if p == f[0] {
			name = n
		}
	}
	if name == "" || !modeRe.MatchString(f[1]) || !nameRe.MatchString(f[2]) || !nameRe.MatchString(f[3]) {
		return lbEntry{}, false
	}
	e := lbEntry{Name: name, Mode: f[1], Owner: f[2], Group: f[3]}
	if len(f) == 5 {
		d, err := base64.StdEncoding.DecodeString(f[4])
		if err != nil {
			return lbEntry{}, false
		}
		e.Data = d
	}
	return e, true
}

// bindpwHash: ldap.conf 내용의 bindpw 해시 (awk 규칙과 동일: 첫 BINDPW 줄, 필드를 공백 하나로 이어붙임). 값이 없으면 ("", false).
func bindpwHash(conf []byte) (string, bool) {
	for _, l := range strings.Split(string(conf), "\n") {
		f := strings.Fields(l)
		if len(f) == 0 || strings.ToLower(f[0]) != "bindpw" {
			continue
		}
		if len(f) == 1 {
			return "", false
		}
		sum := sha256.Sum256([]byte(strings.Join(f[1:], " ") + "\n"))
		return hex.EncodeToString(sum[:]), true
	}
	return "", false
}

// ---- 저장 ----

type ldapMetaFile struct {
	Name  string `json:"name"`
	Path  string `json:"path"`
	Mode  string `json:"mode"`
	Owner string `json:"owner"`
	Group string `json:"group"`
	Size  int    `json:"size"`
}

type ldapMeta struct {
	Job       string         `json:"job"`
	Host      string         `json:"host"`
	Collected string         `json:"collected"`
	Release   string         `json:"release"`
	OSMajor   int            `json:"os_major"`
	Auth      string         `json:"auth"`
	Time      string         `json:"time"`
	Files     []ldapMetaFile `json:"files"`
}

func ldapBakDir() string { return filepath.Join(dataDir(), "ldapbak") }

func ldapHostDir(jobID, host string) (string, bool) {
	if !jobRe.MatchString(jobID) || !hostRe.MatchString(host) {
		return "", false
	}
	return filepath.Join(ldapBakDir(), jobID, host), true
}

func saveBackup(jobID, host string, in lbInfo) error {
	dir, ok := ldapHostDir(jobID, host)
	if !ok {
		return errors.New("잘못된 이름")
	}
	for _, d := range []string{ldapBakDir(), filepath.Dir(dir), dir} {
		if err := os.MkdirAll(d, 0700); err != nil {
			return err
		}
		if err := os.Chmod(d, 0700); err != nil {
			return err
		}
	}
	m := ldapMeta{Job: jobID, Host: host, Collected: time.Now().Format(time.RFC3339),
		Release: in.Rel, OSMajor: in.OS, Auth: in.Auth, Time: in.Time}
	for _, e := range in.Files {
		if err := writeFile0600(filepath.Join(dir, e.Name), e.Data); err != nil {
			return err
		}
		m.Files = append(m.Files, ldapMetaFile{Name: e.Name, Path: lbFilePaths[e.Name], Mode: e.Mode,
			Owner: e.Owner, Group: e.Group, Size: len(e.Data)})
	}
	b, _ := json.MarshalIndent(m, "", "  ")
	return writeFile0600(filepath.Join(dir, lbMetaName), append(b, '\n'))
}

func writeFile0600(p string, data []byte) error {
	if err := os.WriteFile(p, data, 0600); err != nil {
		return err
	}
	return os.Chmod(p, 0600)
}

// loadBackup: meta + 파일 내용 (경로는 표 기준, mode/owner 는 형식 재검증)
func loadBackup(jobID, host string) (ldapMeta, []lbEntry, error) {
	var m ldapMeta
	dir, ok := ldapHostDir(jobID, host)
	if !ok {
		return m, nil, errors.New("잘못된 이름")
	}
	b, err := os.ReadFile(filepath.Join(dir, lbMetaName))
	if err != nil {
		return m, nil, err
	}
	if err := json.Unmarshal(b, &m); err != nil {
		return m, nil, errors.New("meta.json 해석 실패")
	}
	byName := map[string]ldapMetaFile{}
	for _, f := range m.Files {
		if _, known := lbFilePaths[f.Name]; known && modeRe.MatchString(f.Mode) && nameRe.MatchString(f.Owner) && nameRe.MatchString(f.Group) {
			byName[f.Name] = f
		}
	}
	var es []lbEntry
	for _, n := range lbRestoreOrder {
		f, ok := byName[n]
		if !ok {
			continue
		}
		d, err := os.ReadFile(filepath.Join(dir, n))
		if err != nil {
			return m, nil, errors.New("백업 파일 읽기 실패: " + n)
		}
		es = append(es, lbEntry{Name: n, Mode: f.Mode, Owner: f.Owner, Group: f.Group, Data: d})
	}
	return m, es, nil
}

// ---- 구현체 ----

type realLdapBackup struct{}

func NewRealLdapBackup() LdapBackup { return realLdapBackup{} }

func routeOf(j *Job, host string) string {
	if j != nil {
		if h := j.Hosts[host]; h != nil && h.Route == "os6" {
			return "os6"
		}
	}
	return "local"
}

func noneState(reason string) LdapState { return LdapState{Backup: LdapBackupNone, Reason: reason} }

func (realLdapBackup) Backup(job *Job, hosts []string) map[string]LdapState {
	res := map[string]LdapState{}
	if len(hosts) == 0 {
		return res
	}
	groups := map[string][]string{}
	for _, h := range hosts {
		r := routeOf(job, h)
		groups[r] = append(groups[r], h)
	}
	type gres struct {
		out map[string][]string
		err error
	}
	got := map[string]gres{}
	var mu sync.Mutex
	var wg sync.WaitGroup
	cmd := buildRemoteCmd(collectScript())
	for r, hs := range groups {
		wg.Add(1)
		go func(r string, hs []string) {
			defer wg.Done()
			o, err := gosshBatch(r, hs, cmd, ldapBackupTimeout)
			mu.Lock()
			got[r] = gres{o, err}
			mu.Unlock()
		}(r, hs)
	}
	wg.Wait()

	jobID := ""
	if job != nil {
		jobID = job.ID
	}
	for r, hs := range groups {
		g := got[r]
		for _, h := range hs {
			if g.err != nil {
				res[h] = noneState("수집 호출 실패: " + g.err.Error())
				continue
			}
			lines, ok := g.out[h]
			if !ok {
				res[h] = noneState("무응답")
				continue
			}
			in := parseInfo(lines)
			switch {
			case !in.Seen || in.OS == 0 || in.Auth == "":
				res[h] = noneState("OS 판별 불가")
			case !hasFile(in.Files, "ldap.conf"):
				res[h] = noneState("ldap.conf 없음")
			default:
				if err := saveBackup(jobID, h, in); err != nil {
					res[h] = noneState("저장 실패")
				} else {
					res[h] = LdapState{Backup: LdapBackupOK}
				}
			}
		}
	}
	return res
}

func hasFile(es []lbEntry, name string) bool {
	for _, e := range es {
		if e.Name == name {
			return true
		}
	}
	return false
}

// applyFail: 비교/적용을 못 한 경우 — 적용실패(diff, applied=false) 로 표시되어 수동 확인 대상이 된다
func applyFail(reason string) LdapState {
	return LdapState{Backup: LdapBackupOK, Bindpw: BindpwDiff, Reason: reason}
}

func (realLdapBackup) Apply(job *Job, host string) LdapState {
	jobID := ""
	if job != nil {
		jobID = job.ID
	}
	_, entries, err := loadBackup(jobID, host)
	if err != nil || !hasFile(entries, "ldap.conf") {
		return noneState("백업 없음")
	}
	var oldHash string
	oldSet := false
	for _, e := range entries {
		if e.Name == "ldap.conf" {
			oldHash, oldSet = bindpwHash(e.Data)
		}
	}
	if !oldSet {
		return LdapState{Backup: LdapBackupOK, Bindpw: BindpwNA, Reason: "백업에 bindpw 없음"}
	}

	route := routeOf(job, host)
	out, err := gosshBatch(route, []string{host}, buildRemoteCmd(probeScript()), ldapApplyTimeout)
	if err != nil {
		return applyFail("확인 호출 실패(수동 확인)")
	}
	in := parseInfo(out[host])
	if !in.Seen || !in.BindpwSet {
		return applyFail("새 OS 무응답(수동 확인)")
	}
	if in.Bindpw == oldHash {
		return LdapState{Backup: LdapBackupOK, Bindpw: BindpwSame}
	}

	// 다름 → OS 분기 확인(새 OS 기준). 백업에 있는 auth/time 파일 종류가 새 OS 와 다르면 건드리지 않는다.
	if in.OS == 0 || in.Auth == "" {
		return applyFail("OS 판별 불가(수동 확인)")
	}
	total := 0
	for _, e := range entries {
		total += len(e.Data)
		if c, k := kindOfName(e.Name); (c == "auth" && k != in.Auth) || (c == "time" && k != in.Time) {
			return applyFail("OS 분기 불일치(수동 확인)")
		}
	}
	if total > ldapMaxRestoreBytes {
		return applyFail("백업 크기 초과(수동 확인)")
	}
	out, err = gosshBatch(route, []string{host}, buildRemoteCmd(restoreScript(entries)), ldapApplyTimeout)
	if err != nil {
		return applyFail("복원 호출 실패(수동 확인)")
	}
	r := parseInfo(out[host])
	if !r.Seen {
		return applyFail("복원 무응답(수동 확인)")
	}
	var failed []string
	for _, e := range entries {
		if ok, seen := r.Put[e.Name]; !seen || !ok {
			failed = append(failed, e.Name)
		}
	}
	if len(failed) > 0 {
		sort.Strings(failed)
		return applyFail("복원 실패: " + strings.Join(failed, ","))
	}
	if r.Bindpw != oldHash {
		return applyFail("복원 후 bindpw 불일치(수동 확인)")
	}
	st := LdapState{Backup: LdapBackupOK, Bindpw: BindpwDiff, Applied: true}
	var svcFail []string
	for n, ok := range r.Svc {
		if !ok {
			svcFail = append(svcFail, n)
		}
	}
	if len(svcFail) > 0 {
		sort.Strings(svcFail)
		st.Reason = fmt.Sprintf("서비스 재시작 실패: %s", strings.Join(svcFail, ","))
	}
	return st
}
