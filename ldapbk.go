// ldapbk.go - LDAP 백업/복원 (§14-5): 전달 시점 설정 수집(Backup), READY 직후 binddn uid 비교(Probe), 명령·자동 복원(Restore)
package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// 대상 파일·OS 분기는 ldap_check/ldap_check.sh · ldap_setting(apply_body.sh) 과 같다.
//
//	공통        : /etc/openldap/ldap.conf , /etc/resolv.conf , /etc/autofs.conf , /etc/autofs_ldap_auth.conf (없는 파일은 건너뜀)
//	인증(auth)  : OS 7 이하 → /etc/nslcd.conf ,  OS 8 이상 → /etc/sssd/sssd.conf (s4 호스트+services 에 nslcd → nslcd.conf)
//	시각(time)  : OS 7 이하 → /etc/ntp.conf   ,  OS 8 이상 → /etc/chrony.conf    (s4 호스트+services 에 ntp  → ntp.conf)
//
// s4 규칙(hostname 접두사)은 ldap_config.conf 의 s4.prefix / s4.services 값이며, auto_setup 은 그 파일을 읽지 않으므로
// 환경변수 AUTO_SETUP_LDAP_S4_PREFIX (비면 s4 규칙 꺼짐), AUTO_SETUP_LDAP_S4_SERVICES (기본 "nslcd,ntp") 로 받는다.
//
// 비교 기준은 ldap.conf 의 binddn 줄에서 뽑은 uid 값뿐이다(예: "binddn uid=asdf,ou=user,..." → asdf).
// bindpw 는 원격에서 읽지도 출력하지도 않으며(확인 명령은 uid 한 줄만 출력), 로그·상태·반환값·meta.json·에러 문자열에도 남기지 않는다.
// 복원 조건은 binddn uid 다름 · 백업의 auth/time 종류가 새 OS 와 다름(7→8) · 새 OS 에 binddn uid 없음 중 하나이며,
// 7→8 은 백업 nslcd.conf→sssd.conf, ntp.conf→chrony.conf 로 변환해 쓴다(nslcdToSSSD/ntpToChrony, 반대 방향은 수동 확인).
// READY 직후 기본은 비교만(Probe) 하고 복원이 필요하면 '복원 대기(명령 필요)' 로 둔다 — 복원은 auto_setup request ldap-restore(TUI l)
// 명령으로만 하며, conf ldap_auto_restore=y 면 READY 직후 자동으로 복원(Restore)한다.
// 복원은 gossh 명령줄에 파일 내용을 싣지 않는다: os8_mgmt 가 autofs 공유경로(ldap_share_dir)에 백업 파일을 0600 으로 잠시 두고,
// 대상 서버는 명령줄로 받은 "경로·mode·owner·group" 만으로 그 파일을 cat 해서 제자리에 쓴다(끝나면 즉시 삭제).
// 백업 파일 수집은 gossh 응답(stdout)으로 받으며 명령(cat)에는 비밀이 없고, 저장은 0700/0600 이다.

const (
	ldapConfPath = "/etc/openldap/ldap.conf"
	lbMetaName   = "meta.json"
)

var (
	// ldapRoot: 원격 스크립트의 파일 경로 접두사(ldap_check 의 ROOT 와 같은 용도). 운영은 항상 빈 값 — 테스트 전용.
	// 비어 있지 않으면 서비스 재시작·restorecon 을 실제로 하지 않고 $ROOT/restart.log 에 기록만 한다.
	ldapRoot = ""

	ldapBackupTimeout = 90 * time.Second // Backup 호출 1회(전체 호스트 배치) 상한
	ldapApplyTimeout  = 45 * time.Second // Probe·Restore 의 gossh 호출 1회 상한
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
		return out.String(), errLdapTimeout
	}
	return out.String(), err
}

var errLdapTimeout = errors.New("시간 초과")

// ---- 파일 표 ----

// 백업 이름 → 원격 절대경로 (복원 때도 이 표만 사용 — meta.json 의 경로는 신뢰하지 않는다)
var lbFilePaths = map[string]string{
	"ldap.conf":             ldapConfPath,
	"autofs.conf":           "/etc/autofs.conf",
	"autofs_ldap_auth.conf": "/etc/autofs_ldap_auth.conf",
	"resolv.conf":           "/etc/resolv.conf",
	"nslcd.conf":            "/etc/nslcd.conf",
	"sssd.conf":             "/etc/sssd/sssd.conf",
	"ntp.conf":              "/etc/ntp.conf",
	"chrony.conf":           "/etc/chrony.conf",
}

// 복원 순서 (resolv → ldap → autofs → 인증 → 시각)
var lbRestoreOrder = []string{"resolv.conf", "ldap.conf", "autofs.conf", "autofs_ldap_auth.conf", "nslcd.conf", "sssd.conf", "ntp.conf", "chrony.conf"}

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

// binddnSnippet: 현재 ldap.conf 의 binddn uid 값만 한 줄 출력 (bindpw 는 읽지 않음). 규칙은 Go 의 binddnUID 와 같다:
// 첫 번째로 uid= 를 가진 binddn 줄(키워드 대소문자 무관), 따옴표 제거, uid= 뒤 ',' 앞까지.
const binddnSnippet = `BU=$(awk 'tolower($1)=="binddn"{s=$0;sub(/^[ \t]*[^ \t]+[ \t]+/,"",s);gsub(/\r/,"",s);q=sprintf("%c",39);gsub("[\"" q "]","",s);n=split(s,a,",");for(i=1;i<=n;i++){x=a[i];sub(/^[ \t]+/,"",x);if(tolower(substr(x,1,4))=="uid="){v=substr(x,5);sub(/[ \t]+$/,"",v);if(v!=""){print v;exit}}}}' "$R` + ldapConfPath + `" 2>/dev/null | head -n1)
if [ -n "$BU" ]; then echo "==BINDDN v $BU"; else echo "==BINDDN none"; fi
BU=
`

func collectScript() string {
	return detectScript() + `for f in ` + ldapConfPath + ` /etc/resolv.conf /etc/autofs.conf /etc/autofs_ldap_auth.conf $AF $TF; do
  [ -f "$R$f" ] || continue
  st=$(stat -c '%a %U %G' "$R$f" 2>/dev/null) || continue
  b=$(base64 -w0 < "$R$f" 2>/dev/null)
  echo "==FILE $f $st $b"
done
`
}

func probeScript() string { return detectScript() + binddnSnippet }

type lbEntry struct {
	Name, Mode, Owner, Group string
	Data                     []byte
}

// restoreScript: 공유경로(shareDir/<파일명>)의 백업 파일을 대상에 복원하는 스크립트. 파일 내용은 들어 있지 않고
// 경로·mode·owner·group·서비스명만 있다(= 명령줄에 비밀이 없다).
func restoreScript(shareDir string, entries []lbEntry) string {
	var sb strings.Builder
	sb.WriteString(detectScript())
	sb.WriteString("for s in")
	for _, e := range entries {
		sb.WriteString(" " + shq(shareDir+"/"+e.Name))
	}
	sb.WriteString(`; do
  [ -r "$s" ] || { echo "==SHARE err"; exit 3; }
done
echo "==SHARE ok"
umask 077
put() {
  p="$R$2"; t="$p.as_ldap.$$"
  cat "$6" > "$t" 2>/dev/null && chmod "$3" "$t" 2>/dev/null && chown "$4:$5" "$t" 2>/dev/null && mv -f "$t" "$p" 2>/dev/null || { rm -f "$t"; echo "==PUT $1 err"; return 1; }
  if [ -n "$R" ]; then echo "restorecon $2" >> "$R/restart.log"; else command -v restorecon >/dev/null 2>&1 && restorecon "$p" >/dev/null 2>&1; fi
  echo "==PUT $1 ok"
}
svc() {
  if [ -n "$R" ]; then echo "restart $1" >> "$R/restart.log"; echo "==SVC $1 ok"; return 0; fi
  if command -v systemctl >/dev/null 2>&1; then
    systemctl list-unit-files "$1.service" 2>/dev/null | grep -q "^$1\.service" || { echo "==SVC $1 skip"; return 0; }
    o=$(systemctl restart "$1" 2>&1) && { echo "==SVC $1 ok"; return 0; }
    case "$o" in *"not found"*|*"could not be found"*|*"not loaded"*) echo "==SVC $1 skip"; return 0 ;; esac
    echo "==SVC $1 err"; return 0
  fi
  [ -x "/etc/init.d/$1" ] || { echo "==SVC $1 skip"; return 0; }
  if service "$1" restart >/dev/null 2>&1; then echo "==SVC $1 ok"; else echo "==SVC $1 err"; fi
}
`)
	for _, e := range entries {
		fmt.Fprintf(&sb, "put %s %s %s %s %s %s", e.Name, shq(lbFilePaths[e.Name]), e.Mode, e.Owner, e.Group,
			shq(shareDir+"/"+e.Name))
		sb.WriteString(" && W=1")
		sb.WriteString("\n")
	}
	// 파일을 하나라도 썼으면 새 OS 종류 기준 고정 순서로 재시작 (설치 안 된 유닛은 svc 가 skip)
	sb.WriteString(`if [ "$W" = 1 ]; then
  if [ "$TK" = ntp ]; then TS=ntpd; else TS=chronyd; fi
  svc "$AK"; svc autofs; svc "$TS"; svc nscd
fi
`)
	sb.WriteString(binddnSnippet)
	return "W=0\n" + sb.String()
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
	res, _, err := gosshBatchWhy(route, hosts, cmd, timeout)
	return res, err
}

// gosshBatchWhy: gosshBatch + 호스트별 실패 사유(gossh 의 "SSH 접속 실패…"·"ERROR: …" 줄, parseGossh 의 Why).
// 사유는 gossh 가 찍은 접속 오류 문구뿐이며 원격 설정 파일 내용은 들어가지 않는다.
// 시간 제한은 local·os6 모두 sshLimit(timeout) (conf ssh_timeout 반영), 출력 없이 시간을 넘기면 "시간 초과" 오류.
func gosshBatchWhy(route string, hosts []string, cmd string, timeout time.Duration) (map[string][]string, map[string]string, error) {
	var out string
	var err error
	if route == "os6" {
		if os6_mgmt == "" || os6_gossh == "" {
			return nil, nil, errors.New("os6_mgmt/os6_gossh 가 비어 있습니다")
		}
		// 원격은 bash 가 해석하도록 remoteBash 로 감싼다 (os6_mgmt 로그인 셸이 csh 여도 동작)
		remote := remoteTmpSh("f", false, false) + "cat > $f; " + shq(os6_gossh) + " -pm -script -w $f " + shq(cmd) + "; rm -f $f ${f}_*"
		out, err = ldapExec(sshLimit(timeout), hostList(hosts), "ssh", os6_mgmt, remoteBash(remote))
	} else {
		f, e := os.CreateTemp("", "as_ldap_")
		if e != nil {
			return nil, nil, e
		}
		p := f.Name()
		defer cleanupTemp(p)
		_, e = f.WriteString(hostList(hosts))
		f.Close()
		if e != nil {
			return nil, nil, e
		}
		out, err = ldapExec(sshLimit(timeout), "", "gossh", "-pm", "-script", "-w", p, cmd)
	}
	g := parseGossh(out, hosts)
	logGosshUnparsed("LDAP", g, false) // 수집 응답에는 설정 파일 base64 가 있어 원본 줄은 남기지 않는다
	res := g.Lines
	if route == "os6" && len(res) == 0 {
		logSSHReason("LDAP", out) // os6_mgmt ssh 자체가 실패했으면 원인을 한 번 로그에
	}
	if err != nil && len(res) == 0 {
		if err == errLdapTimeout {
			return nil, g.Why, err
		}
		if _, ok := err.(*exec.ExitError); !ok {
			return nil, g.Why, errors.New("gossh 실행 실패")
		}
	}
	return res, g.Why, nil
}

// ---- 파싱 ----

type lbInfo struct {
	Host, Rel string
	OS        int
	Auth      string
	Time      string
	BindSet   bool   // ==BINDDN 줄 존재
	BindUID   string // binddn 의 uid 값, "" = 없음/파싱 실패
	Share     string // ==SHARE ok|err (복원 스크립트의 공유경로 읽기 확인)
	Put       map[string]bool
	Svc       map[string]bool
	SvcSkip   map[string]bool // 설치 안 된 유닛 (실패 아님)
	Files     []lbEntry
	Seen      bool // 응답(HOST 줄) 있음
}

func parseInfo(lines []string) lbInfo {
	in := lbInfo{Put: map[string]bool{}, Svc: map[string]bool{}, SvcSkip: map[string]bool{}}
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
		case strings.HasPrefix(l, "==BINDDN "):
			in.BindSet = true
			if v, ok := strings.CutPrefix(l[9:], "v "); ok {
				in.BindUID = strings.TrimSpace(v)
			}
		case strings.HasPrefix(l, "==SHARE "):
			in.Share = strings.TrimSpace(l[8:])
		case strings.HasPrefix(l, "==PUT "):
			f := strings.Fields(l[6:])
			if len(f) == 2 {
				in.Put[f[0]] = f[1] == "ok"
			}
		case strings.HasPrefix(l, "==SVC "):
			f := strings.Fields(l[6:])
			if len(f) == 2 && f[1] == "skip" {
				in.SvcSkip[f[0]] = true
			} else if len(f) == 2 {
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

// binddnUID: ldap.conf 내용에서 binddn 의 uid 값 (binddnSnippet 의 awk 규칙과 동일).
// "binddn uid=asdf,ou=user,dc=x" → asdf. 키워드 대소문자 무관, 따옴표 제거, 첫 uid= 구성요소, 없으면 ("", false).
func binddnUID(conf []byte) (string, bool) {
	rp := strings.NewReplacer("\r", "", "\"", "", "'", "")
	for _, l := range strings.Split(string(conf), "\n") {
		l = strings.TrimLeft(l, " \t")
		i := strings.IndexAny(l, " \t")
		if i < 0 || !strings.EqualFold(l[:i], "binddn") {
			continue
		}
		for _, x := range strings.Split(rp.Replace(strings.TrimLeft(l[i:], " \t")), ",") {
			x = strings.TrimLeft(x, " \t")
			if len(x) >= 4 && strings.EqualFold(x[:4], "uid=") {
				if v := strings.TrimRight(x[4:], " \t"); v != "" {
					return v, true
				}
			}
		}
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
	return writeBackupFiles(dir, jobID, host, in)
}

// writeBackupFiles: 수집한 파일(0600)과 meta.json 을 dir(호출자가 0700 으로 만들어 둠)에 쓴다 — saveBackup 과 ldap-test 가 같이 쓴다
func writeBackupFiles(dir, jobID, host string, in lbInfo) error {
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
	dir, ok := ldapHostDir(jobID, host)
	if !ok {
		return ldapMeta{}, nil, fs.ErrNotExist // 잘못된 호스트 이름 — 백업이 있을 수 없음
	}
	return loadBackupDir(dir)
}

// loadBackupDir: dir 의 meta.json·백업 파일을 읽는다 — loadBackup 과 ldap-test 가 같이 쓴다
func loadBackupDir(dir string) (ldapMeta, []lbEntry, error) {
	var m ldapMeta
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

// 백업 수집에서 호스트 응답을 받지 못한 사유 (daemon.go ldapBackup 이 "켜져 있는지 아직 모름" 으로 본다)
const (
	ldapNoResp   = "무응답"
	ldapCallFail = "수집 호출 실패"
)

func noneState(reason string) LdapState { return LdapState{Backup: LdapBackupNone, Reason: reason} }

func (realLdapBackup) Backup(job *Job, hosts []string) map[string]LdapState {
	res := map[string]LdapState{}
	if len(hosts) == 0 {
		return res
	}
	jobID := ""
	if job != nil {
		jobID = job.ID
	}
	got := collectHosts(hosts, func(h string) string { return routeOf(job, h) })
	for _, h := range hosts {
		c := got[h]
		if c.OS6 && job != nil && job.Hosts[h] != nil {
			// os8 무응답·os6_mgmt 응답 → 이후 확인·복원도 같은 경로
			job.Hosts[h].Route = "os6"
			logf("경로 전환: %s local → os6 (LDAP 백업 os8 무응답, os6_mgmt 응답, job %s)", h, job.ID)
		}
		if !c.OK {
			res[h] = noneState(c.Why)
			continue
		}
		in := c.Info
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
	return res
}

// lbCollected: collectHosts 의 호스트 한 대분 결과 — OK 면 Info(parseInfo 결과, 파일 내용 포함), 아니면 Why(무응답·호출 실패 사유,
// gossh 접속 오류 문구만 — 비밀값 없음). OS6: local(os8) 에서 응답이 없어 os6_mgmt 경유로 받은 경우(호출자가 이후 경로를 os6 로 바꾼다).
type lbCollected struct {
	Info lbInfo
	OK   bool
	Why  string
	OS6  bool
}

// collectHosts: 호스트들의 LDAP 관련 파일을 gossh 로 수집한다(경로별 배치 1회씩 병렬, local 에서 응답이 없는 호스트는
// os6_mgmt 경유로 한 번 더). job·ldapbak 저장과 무관 — Backup 과 ldap-test 가 같이 쓴다. route 는 호스트별 경로(local|os6).
func collectHosts(hosts []string, route func(host string) string) map[string]lbCollected {
	groups := map[string][]string{}
	for _, h := range hosts {
		r := route(h)
		groups[r] = append(groups[r], h)
	}
	type gres struct {
		out map[string][]string
		why map[string]string // 호스트별 gossh 접속 실패 사유
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
			o, why, err := gosshBatchWhy(r, hs, cmd, ldapBackupTimeout)
			mu.Lock()
			got[r] = gres{o, why, err}
			mu.Unlock()
		}(r, hs)
	}
	wg.Wait()

	// os8 에서 수집이 안 된 local 호스트(ssh 불가·호출 실패)는 os6_mgmt 경유로 한 번 더 수집한다 (os8 과 같은 결과를 얻도록).
	fb := map[string][]string{}
	if g, ok := got["local"]; ok && os6_mgmt != "" && os6_gossh != "" {
		var miss []string
		for _, h := range groups["local"] {
			if _, seen := g.out[h]; g.err != nil || !seen {
				miss = append(miss, h)
			}
		}
		if len(miss) > 0 {
			if o, err := gosshBatch("os6", miss, cmd, ldapBackupTimeout); err == nil {
				for _, h := range miss {
					if l, seen := o[h]; seen {
						fb[h] = l
					}
				}
			}
		}
	}

	res := map[string]lbCollected{}
	for r, hs := range groups {
		g := got[r]
		for _, h := range hs {
			lines, ok := g.out[h]
			gerr := g.err
			l, viaOS6 := fb[h]
			if viaOS6 {
				lines, ok, gerr = l, true, nil
			}
			switch {
			case gerr != nil:
				res[h] = lbCollected{Why: ldapCallFail + ": " + gerr.Error()}
			case !ok:
				why := ldapNoResp
				if w := g.why[h]; w != "" { // 예: "무응답: SSH 접속 실패: …" (gossh 접속 오류 문구만)
					why += ": " + w
				}
				res[h] = lbCollected{Why: why}
			default:
				res[h] = lbCollected{Info: parseInfo(lines), OK: true, OS6: viaOS6}
			}
		}
	}
	return res
}

// moveBackup: ldapbak/<fromJob>/<host> → ldapbak/<toJob>/<host> (같은 호스트 재전달 때 전달 시점 백업 이어받기, daemon.go ldapInherit)
func moveBackup(fromJob, toJob, host string) error {
	src, ok1 := ldapHostDir(fromJob, host)
	dst, ok2 := ldapHostDir(toJob, host)
	if !ok1 || !ok2 {
		return errors.New("잘못된 이름")
	}
	if _, err := os.Stat(filepath.Join(src, lbMetaName)); err != nil {
		return errors.New("이전 백업 없음")
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0700); err != nil {
		return err
	}
	if err := os.Chmod(filepath.Dir(dst), 0700); err != nil {
		return err
	}
	return os.Rename(src, dst)
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

// checkFail: 비교 전에 실패한 경우(확인 호출 실패·새 OS 무응답·OS 판별 불가·공유경로 오류) — binddn 이 다른지 아직 모르므로
// diff 가 아니라 unknown 으로 기록한다 (비고 'LDAP 확인실패'). 비교 후(복원 중) 실패는 applyFail.
func checkFail(reason string) LdapState {
	return LdapState{Backup: LdapBackupOK, Bindpw: BindpwUnknown, Reason: reason}
}

// backupReadWhy: loadBackup 오류의 사유 문자열 — 파일 경로는 빼고 원인만 (비밀값 없음)
func backupReadWhy(err error) string {
	var pe *fs.PathError
	if errors.As(err, &pe) {
		return pe.Op + ": " + pe.Err.Error()
	}
	return err.Error()
}

// ldapManualPrefix: 자동 복원을 못 해 사람이 해야 하는 경우 Reason 의 머리말 (model.go 비고가 이 값으로 구분)
const ldapManualPrefix = "수동 복원 필요"

// ldapPendingPrefix: 비교 결과 복원이 필요하지만 명령(request ldap-restore)을 기다리는 경우 Reason 의 머리말 (Bindpw=diff, Applied=false)
const ldapPendingPrefix = "복원 대기(명령 필요)"

// pendingState: 복원 대기 — Reason 은 머리말 + 복원 이유·변환 예정 내역(있으면, 비밀값 없음)
func pendingState(notes []string) LdapState {
	r := ldapPendingPrefix
	if len(notes) > 0 {
		r += ": " + strings.Join(notes, "; ")
	}
	return LdapState{Backup: LdapBackupOK, Bindpw: BindpwDiff, Reason: r}
}

// ldapPending: 복원 대기(명령 필요) 상태인지
func ldapPending(l *LdapState) bool {
	return l != nil && l.Backup == LdapBackupOK && l.Bindpw == BindpwDiff && !l.Applied && strings.HasPrefix(l.Reason, ldapPendingPrefix)
}

// ---- RHEL7↔8 변환 (7→8: 백업 nslcd.conf → sssd.conf, ntp.conf → chrony.conf / 8→7: sssd.conf → nslcd.conf, chrony.conf → ntp.conf) ----

// lbConvert: 백업 파일 이름 → 종류가 다른 OS 에 쓸 파일 이름·mode 와 변환 함수. 양방향: 7→8(nslcd→sssd, ntp→chrony),
// 8→7(sssd→nslcd, chrony→ntp). 변환한 파일은 root:root 로 lbFilePaths 의 새 경로에 쓰고, 원래 파일은 새 OS 에 쓰지 않는다.
var lbConvert = map[string]struct {
	to, mode string
	fn       func([]byte) ([]byte, error)
}{
	"nslcd.conf":  {"sssd.conf", "600", nslcdToSSSD},
	"ntp.conf":    {"chrony.conf", "644", ntpToChrony},
	"sssd.conf":   {"nslcd.conf", "600", sssdToNslcd},
	"chrony.conf": {"ntp.conf", "644", chronyToNtp},
}

// lbUnquote: 값 양끝의 같은 따옴표(" 또는 ') 한 쌍을 벗긴다
func lbUnquote(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 && (s[0] == '"' || s[0] == '\'') && s[len(s)-1] == s[0] {
		return s[1 : len(s)-1]
	}
	return s
}

// nslcdMaps: nslcd.conf "base <맵> <DN>" 의 맵 이름
var nslcdMaps = map[string]bool{"aliases": true, "ethers": true, "group": true, "hosts": true, "netgroup": true,
	"networks": true, "passwd": true, "protocols": true, "rpc": true, "services": true, "shadow": true}

// nslcdToSSSD: nslcd.conf 내용으로 sssd.conf(RHEL8, [domain/default] id/auth/chpass=ldap)를 만든다.
// 키는 대소문자 무관, # 로 시작하는 줄은 주석, 값 양끝 따옴표는 벗긴다. uri 는 여러 개(한 줄·여러 줄)면 쉼표로 잇고,
// base(일반)·base passwd|shadow → ldap_user_search_base · base group → ldap_group_search_base (각각 첫 값),
// ssl start_tls · tls_reqcert · tls_cacertdir · tls_cacertfile 을 옮긴다. map/filter 등 그 밖의 키는 무시한다.
// uri·binddn·bindpw 가 없으면 오류(오류 문자열에는 키 이름만 있고 값은 없다).
func nslcdToSSSD(src []byte) ([]byte, error) {
	var uris []string
	var base, userBase, shadowBase, groupBase, binddn, bindpw, reqcert, cadir, cafile string
	startTLS := false
	first := func(p *string, v string) {
		if *p == "" {
			*p = v
		}
	}
	for _, l := range strings.Split(string(src), "\n") {
		l = strings.TrimSpace(l)
		if l == "" || l[0] == '#' {
			continue
		}
		i := strings.IndexAny(l, " \t")
		if i < 0 {
			continue
		}
		val := strings.TrimSpace(l[i+1:])
		switch strings.ToLower(l[:i]) {
		case "uri":
			for _, u := range strings.Fields(val) {
				if u = strings.Trim(u, "\"'"); !strings.Contains(u, "://") {
					return nil, errors.New("uri 형식")
				}
				uris = append(uris, u)
			}
		case "base":
			if f := strings.Fields(val); len(f) >= 2 && nslcdMaps[strings.ToLower(f[0])] {
				dn := lbUnquote(strings.TrimSpace(val[len(f[0]):]))
				switch strings.ToLower(f[0]) {
				case "passwd":
					first(&userBase, dn)
				case "shadow":
					first(&shadowBase, dn)
				case "group":
					first(&groupBase, dn)
				}
			} else {
				first(&base, lbUnquote(val))
			}
		case "binddn":
			first(&binddn, lbUnquote(val))
		case "bindpw":
			first(&bindpw, lbUnquote(val))
		case "ssl":
			if strings.EqualFold(lbUnquote(val), "start_tls") {
				startTLS = true
			}
		case "tls_reqcert":
			reqcert = strings.ToLower(lbUnquote(val))
		case "tls_cacertdir":
			cadir = lbUnquote(val)
		case "tls_cacertfile":
			cafile = lbUnquote(val)
		}
	}
	switch {
	case len(uris) == 0:
		return nil, errors.New("uri 없음")
	case binddn == "":
		return nil, errors.New("binddn 없음")
	case bindpw == "":
		return nil, errors.New("bindpw 없음")
	}
	first(&userBase, shadowBase)
	ldaps := false
	for _, u := range uris {
		if strings.HasPrefix(strings.ToLower(u), "ldaps://") {
			ldaps = true
		}
	}
	var b strings.Builder
	b.WriteString("# generated by auto_setup from backup nslcd.conf (RHEL7 -> RHEL8)\n")
	b.WriteString("[sssd]\nservices = nss, pam\ndomains = default\n\n[nss]\nhomedir_substring = /home\n\n[pam]\n\n")
	b.WriteString("[domain/default]\nid_provider = ldap\nauth_provider = ldap\nchpass_provider = ldap\n")
	b.WriteString("ldap_uri = " + strings.Join(uris, ", ") + "\n")
	kv := func(k, v string) {
		if v != "" {
			b.WriteString(k + " = " + v + "\n")
		}
	}
	kv("ldap_search_base", base)
	kv("ldap_user_search_base", userBase)
	kv("ldap_group_search_base", groupBase)
	if startTLS {
		kv("ldap_id_use_start_tls", "True")
	}
	kv("ldap_tls_reqcert", reqcert)
	kv("ldap_tls_cacertdir", cadir)
	kv("ldap_tls_cacert", cafile)
	if !startTLS && !ldaps {
		// nslcd 는 평문 ldap:// 로도 인증했지만 sssd 는 TLS 없으면 인증을 거부한다 → 기존 동작 유지
		kv("ldap_auth_disable_tls_never_use_in_production", "true")
	}
	kv("ldap_default_bind_dn", binddn)
	kv("ldap_default_authtok_type", "password")
	kv("ldap_default_authtok", bindpw)
	kv("cache_credentials", "True")
	return []byte(b.String()), nil
}

// ntpToChrony: ntp.conf 의 server/pool 줄(키 대소문자 무관, # 뒤는 주석)로 chrony.conf(RHEL8 기본 줄 포함)를 만든다.
// 옵션은 chrony 에도 있는 iburst·burst·prefer·noselect·xleave·minpoll N·maxpoll N·version N 만 유지하고,
// -4/-6·key 등 나머지와 127.127.x.x(ntpd 로컬 시계 드라이버), restrict·driftfile 등 다른 줄은 버린다. server/pool 이 없으면 오류.
func ntpToChrony(src []byte) ([]byte, error) {
	var b strings.Builder
	b.WriteString("# generated by auto_setup from backup ntp.conf (RHEL7 -> RHEL8)\n")
	n := 0
	for _, l := range strings.Split(string(src), "\n") {
		if i := strings.IndexByte(l, '#'); i >= 0 {
			l = l[:i]
		}
		f := strings.Fields(l)
		if len(f) < 2 {
			continue
		}
		key := strings.ToLower(f[0])
		if key != "server" && key != "pool" {
			continue
		}
		args := f[1:]
		for len(args) > 0 && (args[0] == "-4" || args[0] == "-6") {
			args = args[1:]
		}
		if len(args) == 0 {
			continue
		}
		host := lbUnquote(args[0])
		if host == "" || strings.HasPrefix(host, "127.127.") {
			continue
		}
		line := key + " " + host
		for i := 1; i < len(args); i++ {
			switch o := strings.ToLower(args[i]); o {
			case "iburst", "burst", "prefer", "noselect", "xleave":
				line += " " + o
			case "minpoll", "maxpoll", "version", "key", "mode", "ttl":
				if i+1 < len(args) {
					if _, err := strconv.Atoi(args[i+1]); err == nil && (o == "minpoll" || o == "maxpoll" || o == "version") {
						line += " " + o + " " + args[i+1]
					}
					i++
				}
			}
		}
		b.WriteString(line + "\n")
		n++
	}
	if n == 0 {
		return nil, errors.New("server 없음")
	}
	b.WriteString("driftfile /var/lib/chrony/drift\nmakestep 1.0 3\nrtcsync\nlogdir /var/log/chrony\n")
	return []byte(b.String()), nil
}

// sssdToNslcd: sssd.conf 내용으로 nslcd.conf(RHEL7, 기본 줄 uid nslcd / gid ldap 포함)를 만든다 — nslcdToSSSD 의 반대 방향.
// [sssd] domains 의 첫 번째 도메인(domains 가 없으면 첫 [domain/*] 절)의 키만 본다. 절·키 이름은 대소문자 무관, # 또는 ; 로
// 시작하는 줄은 주석, 같은 키는 첫 값. ldap_uri(쉼표 구분) → uri 한 줄(공백 구분), ldap_search_base → base,
// ldap_user_search_base → base passwd, ldap_group_search_base → base group (sssd 확장 형식 "DN?범위?필터" 는 DN 만),
// ldap_default_bind_dn → binddn, ldap_default_authtok → bindpw, ldap_id_use_start_tls=true → ssl start_tls,
// ldap_tls_reqcert·ldap_tls_cacertdir·ldap_tls_cacert → tls_reqcert·tls_cacertdir·tls_cacertfile. 그 밖의 키는 무시한다.
// 도메인·uri·binddn·bindpw 가 없거나 authtok 종류가 password 가 아니면 오류(오류 문자열에는 키 이름만 있고 값은 없다).
func sssdToNslcd(src []byte) ([]byte, error) {
	secs := map[string]map[string]string{} // 절(소문자) → 키(소문자) → 값
	var doms []string                      // [domain/*] 절 등장 순서
	sec := ""
	for _, l := range strings.Split(string(src), "\n") {
		l = strings.TrimSpace(l)
		if l == "" || l[0] == '#' || l[0] == ';' {
			continue
		}
		if l[0] == '[' {
			if i := strings.IndexByte(l, ']'); i > 0 {
				sec = strings.ToLower(strings.TrimSpace(l[1:i]))
				if secs[sec] == nil {
					secs[sec] = map[string]string{}
					if strings.HasPrefix(sec, "domain/") {
						doms = append(doms, sec)
					}
				}
			}
			continue
		}
		i := strings.IndexByte(l, '=')
		if i <= 0 || sec == "" {
			continue
		}
		if k := strings.ToLower(strings.TrimSpace(l[:i])); secs[sec][k] == "" {
			secs[sec][k] = strings.TrimSpace(l[i+1:])
		}
	}
	dom := ""
	if f := strings.FieldsFunc(secs["sssd"]["domains"], func(r rune) bool { return r == ',' || r == ' ' || r == '\t' }); len(f) > 0 {
		dom = "domain/" + strings.ToLower(f[0])
	} else if len(doms) > 0 {
		dom = doms[0]
	}
	d := secs[dom]
	if d == nil {
		return nil, errors.New("domain 없음")
	}
	var uris []string
	for _, u := range strings.Split(d["ldap_uri"], ",") {
		if u = strings.TrimSpace(u); u == "" {
			continue
		}
		if !strings.Contains(u, "://") {
			return nil, errors.New("uri 형식")
		}
		uris = append(uris, u)
	}
	binddn, bindpw := d["ldap_default_bind_dn"], d["ldap_default_authtok"]
	switch {
	case len(uris) == 0:
		return nil, errors.New("uri 없음")
	case binddn == "":
		return nil, errors.New("binddn 없음")
	case bindpw == "":
		return nil, errors.New("bindpw 없음")
	case d["ldap_default_authtok_type"] != "" && !strings.EqualFold(d["ldap_default_authtok_type"], "password"):
		return nil, errors.New("authtok 종류")
	}
	dnOnly := func(s string) string {
		if i := strings.IndexByte(s, '?'); i >= 0 {
			s = s[:i]
		}
		return strings.TrimSpace(s)
	}
	var b strings.Builder
	b.WriteString("# generated by auto_setup from backup sssd.conf (RHEL8 -> RHEL7)\nuid nslcd\ngid ldap\n")
	b.WriteString("uri " + strings.Join(uris, " ") + "\n")
	kv := func(k, v string) {
		if v != "" {
			b.WriteString(k + " " + v + "\n")
		}
	}
	kv("base", dnOnly(d["ldap_search_base"]))
	if v := dnOnly(d["ldap_user_search_base"]); v != "" {
		kv("base passwd", v)
	}
	if v := dnOnly(d["ldap_group_search_base"]); v != "" {
		kv("base group", v)
	}
	kv("binddn", binddn)
	kv("bindpw", bindpw)
	if strings.EqualFold(d["ldap_id_use_start_tls"], "true") {
		kv("ssl", "start_tls")
	}
	kv("tls_reqcert", strings.ToLower(d["ldap_tls_reqcert"]))
	kv("tls_cacertdir", d["ldap_tls_cacertdir"])
	kv("tls_cacertfile", d["ldap_tls_cacert"])
	return []byte(b.String()), nil
}

// chronyArgOpts: 값 하나를 받는 chrony server/pool 옵션 (ntp 로 옮기지 않으므로 값까지 버린다)
var chronyArgOpts = map[string]bool{"key": true, "port": true, "ntsport": true, "maxdelay": true, "maxdelayratio": true,
	"maxdelaydevratio": true, "mindelay": true, "asymmetry": true, "offset": true, "minsamples": true, "maxsamples": true,
	"filter": true, "presend": true, "minstratum": true, "polltarget": true, "maxsources": true, "certset": true, "extfield": true}

// chronyToNtp: chrony.conf 의 server/pool 줄(키 대소문자 무관, # ; ! % 로 시작하는 줄과 # 뒤는 주석)로 ntp.conf(RHEL7 기본 줄
// driftfile·restrict 포함)를 만든다 — ntpToChrony 의 반대 방향. 옵션은 ntp 에도 있는 iburst·burst·prefer·noselect·xleave·
// minpoll N·maxpoll N·version N 만 유지하고 그 밖의 chrony 옵션(값이 있으면 값까지)과 refclock·makestep 등 다른 줄은 버린다.
// server/pool 이 없으면 오류.
func chronyToNtp(src []byte) ([]byte, error) {
	var b strings.Builder
	b.WriteString("# generated by auto_setup from backup chrony.conf (RHEL8 -> RHEL7)\n")
	b.WriteString("driftfile /var/lib/ntp/drift\nrestrict default nomodify notrap nopeer noquery\nrestrict 127.0.0.1\nrestrict ::1\n")
	n := 0
	for _, l := range strings.Split(string(src), "\n") {
		if i := strings.IndexByte(l, '#'); i >= 0 {
			l = l[:i]
		}
		f := strings.Fields(l)
		if len(f) < 2 || strings.ContainsAny(f[0][:1], ";!%") {
			continue
		}
		key := strings.ToLower(f[0])
		if key != "server" && key != "pool" {
			continue
		}
		line := key + " " + f[1]
		for i := 2; i < len(f); i++ {
			switch o := strings.ToLower(f[i]); {
			case o == "iburst" || o == "burst" || o == "prefer" || o == "noselect" || o == "xleave":
				line += " " + o
			case o == "minpoll" || o == "maxpoll" || o == "version":
				if i+1 < len(f) {
					if _, err := strconv.Atoi(f[i+1]); err == nil {
						line += " " + o + " " + f[i+1]
					}
					i++
				}
			case chronyArgOpts[o]:
				i++
			}
		}
		b.WriteString(line + "\n")
		n++
	}
	if n == 0 {
		return nil, errors.New("server 없음")
	}
	return []byte(b.String()), nil
}

// stageShare: 백업 파일 세트를 공유경로(<ldap_share_dir>/.as_ldap_<랜덤>/<호스트>/)에 0700/0600 으로 쓴다.
// root 는 호출자가 사용 후 지워야 하는 임시 디렉터리, dir 은 파일이 있는 호스트 디렉터리.
func stageShare(host string, entries []lbEntry) (root, dir string, err error) {
	var rb [16]byte
	if _, err = rand.Read(rb[:]); err != nil {
		return "", "", errors.New("임시 경로 생성 실패")
	}
	root = filepath.Join(ldap_share_dir, ".as_ldap_"+hex.EncodeToString(rb[:]))
	if err = os.Mkdir(root, 0700); err != nil {
		return "", "", errors.New("공유경로 쓰기 실패")
	}
	dir = filepath.Join(root, host)
	if err = os.Chmod(root, 0700); err == nil {
		if err = os.Mkdir(dir, 0700); err == nil {
			err = os.Chmod(dir, 0700)
		}
	}
	if err != nil {
		return root, "", errors.New("공유경로 쓰기 실패")
	}
	for _, e := range entries {
		if err = writeFile0600(filepath.Join(dir, e.Name), e.Data); err != nil {
			return root, "", errors.New("공유경로 쓰기 실패")
		}
	}
	return root, dir, nil
}

// lbPlan: 비교 결과 복원이 필요할 때의 복원 계획 — 새 OS 에 쓸 파일 세트(7↔8 변환 포함)와 경로. 원격에는 아직 아무것도 쓰지 않은 상태
type lbPlan struct {
	route   string
	oldUID  string
	restore []lbEntry
	notes   []string // 복원 이유·변환 내역 (비밀값 없음)
	done    *lbInfo  // restoreEntries 가 받은 대상 응답(파일별 PUT·서비스·binddn uid — ldap-test 표시용, 파일 내용 없음)
}

// ldapCompare: job 의 ldapbak 백업을 읽어 compareEntries 로 비교만 한다(읽기 전용). 대상이 os6_mgmt 경유로만 응답했으면
// job 의 경로를 os6 로 바꾼다(이후 복원도 같은 경로). 복원이 필요 없거나 할 수 없으면 (최종 상태, nil), 필요하면 (_, 계획).
func ldapCompare(job *Job, host string) (LdapState, *lbPlan) {
	jobID := ""
	if job != nil {
		jobID = job.ID
	}
	_, entries, err := loadBackup(jobID, host)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) { // 백업 디렉터리·meta.json 자체가 없음
			return noneState("백업 파일 없음"), nil
		}
		// 백업은 있었는데 읽지 못함 — Backup=ok 를 none 으로 덮지 않고 확인실패로 둔다
		return checkFail("백업 읽기 실패: " + backupReadWhy(err)), nil
	}
	dir, _ := ldapHostDir(jobID, host)
	from := routeOf(job, host)
	st, p, route := compareEntries(from, host, dir, entries)
	if from == "local" && route == "os6" && job != nil && job.Hosts[host] != nil {
		job.Hosts[host].Route = "os6"
		logf("경로 전환: %s local → os6 (LDAP 확인 os8 무응답, os6_mgmt 응답, job %s)", host, job.ID)
	}
	return st, p
}

// compareEntries: 백업 파일 세트(entries)를 대상 호스트와 비교만 한다(읽기 전용) — 백업 ldap.conf 의 binddn uid·auth/time 종류를
// 새 OS(probe: OS 판별·binddn uid 한 줄, gossh 1회 + local 무응답이면 os6_mgmt 경유 1회)와 비교하고, 복원이 필요하면 복원 세트를
// 메모리에서만 준비한다(convertEntriesFor: 7↔8 변환 포함). 공유경로·대상 파일·서비스는 건드리지 않는다.
// bakDir 은 수동 복원 안내에 보일 백업 위치. job·ldapbak 과 무관 — ldapCompare 와 ldap-test 가 같이 쓴다.
// 반환: 복원이 필요 없거나 할 수 없으면 (최종 상태, nil), 필요하면 (_, 계획), 그리고 대상이 응답한 경로(local|os6).
func compareEntries(route, host, bakDir string, entries []lbEntry) (LdapState, *lbPlan, string) {
	if !hasFile(entries, "ldap.conf") {
		return noneState("백업 없음"), nil, route
	}
	var oldUID string
	oldOK := false
	for _, e := range entries {
		if e.Name == "ldap.conf" {
			oldUID, oldOK = binddnUID(e.Data)
		}
	}
	if !oldOK {
		return LdapState{Backup: LdapBackupOK, Bindpw: BindpwNA, Reason: "binddn 확인불가"}, nil, route
	}

	out, err := gosshBatch(route, []string{host}, buildRemoteCmd(probeScript()), ldapApplyTimeout)
	if route == "local" && os6_mgmt != "" && os6_gossh != "" && (err != nil || !parseInfo(out[host]).Seen) {
		// os8 에서 안 닿으면 os6_mgmt 경유로 한 번 더 (응답하면 이후 복원도 같은 경로)
		if o6, e6 := gosshBatch("os6", []string{host}, buildRemoteCmd(probeScript()), ldapApplyTimeout); e6 == nil && parseInfo(o6[host]).Seen {
			out, err, route = o6, nil, "os6"
		}
	}
	if err != nil {
		return checkFail("확인 호출 실패(수동 확인)"), nil, route
	}
	in := parseInfo(out[host])
	if !in.Seen || !in.BindSet {
		return checkFail("새 OS 무응답(수동 확인)"), nil, route
	}
	// 복원 조건: (a) binddn uid 가 백업과 다름 (b) 백업의 auth/time 파일 종류가 새 OS 와 다름(예: 7→8, 8→7) (c) 새 OS 에 binddn uid 없음.
	// uid 같고 종류도 같으면 생략. 새 OS 판별이 안 되면 (b) 는 모르므로 uid 만 본다.
	kindKnown := in.OS != 0 && in.Auth != ""
	kindChanged := false
	for _, e := range entries {
		if c, k := kindOfName(e.Name); kindKnown && ((c == "auth" && k != in.Auth) || (c == "time" && k != in.Time)) {
			kindChanged = true
		}
	}
	if in.BindUID == oldUID && !kindChanged {
		return LdapState{Backup: LdapBackupOK, Bindpw: BindpwSame}, nil, route
	}
	var notes []string // 복원 이유(uid 가 다르지 않은데 복원하는 경우)·변환 내역, 비밀값 없음
	switch in.BindUID {
	case "":
		notes = append(notes, "새 OS binddn 없음")
	case oldUID:
		notes = append(notes, "OS 종류 변경")
	}

	// 복원 대상 → 공유경로가 없으면 자동 복원 안 함(백업 위치만 안내)
	if ldap_share_dir == "" {
		return applyFail(ldapManualPrefix + "(ldap_share_dir 미설정, 백업: " + bakDir + ")"), nil, route
	}
	if !filepath.IsAbs(ldap_share_dir) || strings.ContainsAny(ldap_share_dir, "\r\n") {
		return checkFail("ldap_share_dir 경로 오류(수동 확인)"), nil, route
	}

	// OS 분기 확인(새 OS 기준). 백업의 auth/time 파일 종류가 새 OS 와 다르면 방향에 맞게 변환해(7→8: nslcd→sssd, ntp→chrony /
	// 8→7: sssd→nslcd, chrony→ntp) 새 OS 경로에 쓰고, 원래 파일은 새 OS 에 쓰지 않는다.
	if !kindKnown {
		return checkFail("OS 판별 불가(수동 확인)"), nil, route
	}
	restore, conv, err := convertEntriesFor(entries, in.Auth, in.Time)
	if err != nil {
		return applyFail(err.Error() + "(수동 확인)"), nil, route // 오류에는 파일 이름만 있다(변환 함수의 키 이름도 남기지 않음)
	}
	if len(conv) > 0 {
		notes = append(notes, "변환 "+strings.Join(conv, ","))
	}
	return LdapState{}, &lbPlan{route: route, oldUID: oldUID, restore: restore, notes: notes}, route
}

// convertEntriesFor: 백업 파일 세트를 auth(sssd|nslcd)·timeKind(chrony|ntp) 종류 OS 용 파일 세트로 바꾼다(순서 유지).
// 공통 파일과 종류가 같은 auth/time 파일은 그대로, 종류가 다른 파일은 lbConvert 로 변환해 root:root·표의 mode 로 새 이름에 둔다
// (원래 파일은 결과에 넣지 않는다). conv 는 "nslcd.conf→sssd.conf" 같은 변환 내역.
// 오류 문자열에는 파일 이름만 있다(값·키 이름 없음): "변환 실패: <새 파일>", "OS 분기 불일치", "OS 종류 오류".
func convertEntriesFor(entries []lbEntry, auth, timeKind string) ([]lbEntry, []string, error) {
	if (auth != "sssd" && auth != "nslcd") || (timeKind != "chrony" && timeKind != "ntp") {
		return nil, nil, errors.New("OS 종류 오류")
	}
	out := make([]lbEntry, 0, len(entries))
	var conv []string
	for _, e := range entries {
		c, k := kindOfName(e.Name)
		want := auth
		if c == "time" {
			want = timeKind
		}
		if c == "" || k == want {
			out = append(out, e)
			continue
		}
		cv, ok := lbConvert[e.Name]
		if !ok || cv.to != want+".conf" {
			return nil, nil, errors.New("OS 분기 불일치")
		}
		data, err := cv.fn(e.Data)
		if err != nil {
			return nil, nil, errors.New("변환 실패: " + cv.to)
		}
		out = append(out, lbEntry{Name: cv.to, Mode: cv.mode, Owner: "root", Group: "root", Data: data})
		conv = append(conv, e.Name+"→"+cv.to)
	}
	return out, conv, nil
}

// Probe: 비교만 — READY 직후 기본 동작(conf ldap_auto_restore 꺼짐). 복원이 필요하면 쓰지 않고 '복원 대기(명령 필요)' 로 돌려준다
// (복원은 auto_setup request ldap-restore · TUI l 로). 복원이 필요 없거나 할 수 없으면 Restore 와 같은 상태.
func (realLdapBackup) Probe(job *Job, host string) LdapState {
	st, p := ldapCompare(job, host)
	if p == nil {
		return st
	}
	return pendingState(p.notes)
}

// Restore: 비교 후 필요하면 복원 — conf ldap_auto_restore=y 의 READY 직후, 또는 request ldap-restore 처리 때
func (realLdapBackup) Restore(job *Job, host string) LdapState {
	cmp, p := ldapCompare(job, host)
	if p == nil {
		return cmp
	}
	return restoreEntries(host, p)
}

// restoreEntries: 계획(p)대로 복원 — 파일 세트를 공유경로에 잠시 두고(stageShare) 대상이 가져가 제자리에 쓴 뒤 서비스 재시작,
// 복원 후 binddn uid 확인. 공유 임시 디렉터리는 성공·실패 모두 즉시 지운다. job 과 무관 — Restore 와 ldap-test 가 같이 쓴다.
func restoreEntries(host string, p *lbPlan) LdapState {
	route, oldUID, restore, notes := p.route, p.oldUID, p.restore, p.notes
	var out map[string][]string
	root, dir, err := stageShare(host, restore)
	if root != "" {
		defer os.RemoveAll(root) // 성공·실패 모두 공유 임시 디렉터리 즉시 삭제
	}
	if err != nil {
		return applyFail(err.Error() + "(수동 확인)")
	}
	out, err = gosshBatch(route, []string{host}, buildRemoteCmd(restoreScript(dir, restore)), ldapApplyTimeout)
	if err != nil {
		return applyFail("복원 호출 실패(수동 확인)")
	}
	r := parseInfo(out[host])
	p.done = &r
	if !r.Seen {
		return applyFail("복원 무응답(수동 확인)")
	}
	if r.Share != "ok" {
		return applyFail("공유경로 접근 실패(수동 확인)")
	}
	var failed []string
	for _, e := range restore {
		if ok, seen := r.Put[e.Name]; !seen || !ok {
			failed = append(failed, e.Name)
		}
	}
	if len(failed) > 0 {
		sort.Strings(failed)
		return applyFail("복원 실패: " + strings.Join(failed, ","))
	}
	if r.BindUID != oldUID {
		return applyFail("복원 후 binddn 불일치(수동 확인)")
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
		notes = append(notes, fmt.Sprintf("서비스 재시작 실패: %s", strings.Join(svcFail, ",")))
	}
	st.Reason = strings.Join(notes, "; ")
	return st
}
