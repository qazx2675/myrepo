package render

// 노드 스크립트 시험용 도구 모음.
//
// 안전 원칙: 스크립트는 반드시 ROOT=<임시 디렉토리> 로, PATH 맨 앞에 가짜 ip/hostname/nmcli
// (그리고 서비스 재시작류 가짜)를 둔 채로만 실행합니다. runScript 는 ROOT 가 비어 있으면
// 스크립트를 실행하지 않고 시험을 중단합니다. 가짜 명령이 예상 밖 형태로 불리면 그 사실을 남기고
// 시험을 실패시킵니다 (예: 쓰기용 nmcli, systemctl, reboot).

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"testing"

	"ip-change/internal/config"
	"ip-change/internal/target"
)

const (
	testHost = "node01"
	oldIP    = "192.168.0.58"
	newIP    = "172.16.5.58"
	newGW    = "172.16.5.1"
	macEns   = "00:0c:29:aa:bb:cc"

	legacyDir  = "/etc/sysconfig/network-scripts"
	keyfileDir = "/etc/NetworkManager/system-connections"
	ifcfgPath  = legacyDir + "/ifcfg-ens160"
	kfPath     = keyfileDir + "/ens160.nmconnection"
	backupBase = "/var/lib/ip_change/backup"

	osRocky8 = "Rocky Linux release 8.10 (Green Obsidian)"
	osRHEL9  = "Red Hat Enterprise Linux release 9.4 (Plow)"
	osRHEL10 = "Red Hat Enterprise Linux release 10.0 (Coughlan)"
)

// TestMain 은 시험이 이 호스트의 실제 /var/lib/ip_change 를 만들거나 바꾸지 않았는지 끝에서 확인합니다.
func TestMain(m *testing.M) {
	before := liveState()
	code := m.Run()
	if after := liveState(); after != before {
		fmt.Fprintf(os.Stderr, "실제 /var/lib/ip_change 가 바뀌었습니다: %q -> %q\n", before, after)
		if code == 0 {
			code = 1
		}
	}
	os.Exit(code)
}

func liveState() string {
	st, err := os.Stat("/var/lib/ip_change")
	if err != nil {
		return "없음"
	}
	return "있음 " + st.ModTime().String()
}

// ---- 가짜 명령 ----

const fakeHostname = "#!/bin/sh\n" + `case "$1" in
  -s) echo "${FAKE_HOSTNAME%%.*}" ;;
  "") echo "$FAKE_HOSTNAME" ;;
  *) echo "hostname $*" >> "$FAKE_DIR/unexpected"; exit 99 ;;
esac
`

const fakeIP = "#!/bin/sh\n" + `args="$*"
case "$args" in
  "-4 -o addr show") cat "$FAKE_DIR/addr4" ;;
  "-4 route show default") cat "$FAKE_DIR/route4" 2>/dev/null ;;
  "-o link show dev "*) cat "$FAKE_DIR/link.${args##* }" 2>/dev/null ;;
  "-o -d link show dev "*) cat "$FAKE_DIR/linkd.${args##* }" 2>/dev/null ;;
  *) echo "ip $args" >> "$FAKE_DIR/unexpected"; exit 99 ;;
esac
`

// nmcli 는 읽기 전용 조회만 흉내 냅니다. 그 밖(쓰기 명령 포함)은 기록하고 실패시킵니다.
const fakeNmcli = "#!/bin/sh\n" + `args="$*"
case "$args" in
  "-t -f RUNNING general"|"-t -f RUNNING nm")
    if [ -f "$FAKE_DIR/nm_running" ]; then echo running; else echo "not running"; fi ;;
  "-t -f GENERAL.CONNECTION device show "*) cat "$FAKE_DIR/nm_dev.${args##* }" 2>/dev/null ;;
  "-t -f NAME,UUID,DEVICE,FILENAME connection show") cat "$FAKE_DIR/nm_list" 2>/dev/null ;;
  *) echo "nmcli $args" >> "$FAKE_DIR/unexpected"; exit 98 ;;
esac
`

const fakeDanger = "#!/bin/sh\n" + `echo "$(basename "$0") $*" >> "$FAKE_DIR/unexpected"
exit 97
`

// fakeAwk 는 편집용 awk 의 결과를 일부러 바꾸지 않아 "쓴 뒤 검증 실패" 경로를 만듭니다.
func fakeAwk(realAwk string) string {
	return "#!/bin/sh\n" + `if [ -n "${E_SETS:-}" ] && [ -n "${FAKE_AWK_BREAK:-}" ]; then
  for last; do :; done
  cat "$last"
  exit 0
fi
exec ` + realAwk + ` "$@"
`
}

// ---- 시험 환경 ----

type lab struct {
	t                    *testing.T
	root, bin, fake, tmp string
	env                  map[string]string
	n                    int
}

func newLab(t *testing.T) *lab {
	t.Helper()
	if runtime.GOOS != "linux" {
		t.Skip("노드 스크립트 시험은 리눅스에서만 합니다")
	}
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash 가 없는 환경이라 건너뜁니다")
	}
	base := t.TempDir()
	l := &lab{t: t, root: filepath.Join(base, "root"), bin: filepath.Join(base, "bin"),
		fake: filepath.Join(base, "fake"), tmp: filepath.Join(base, "tmp")}
	for _, d := range []string{l.root, l.bin, l.fake, l.tmp} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	l.writeBin("hostname", fakeHostname)
	l.writeBin("ip", fakeIP)
	l.writeBin("nmcli", fakeNmcli)
	for _, n := range []string{"systemctl", "service", "ifup", "ifdown", "reboot", "shutdown", "poweroff", "halt", "init", "telinit"} {
		l.writeBin(n, fakeDanger)
	}
	l.env = map[string]string{
		"SSH_CONNECTION": "192.168.0.10 51234 " + oldIP + " 22",
		"FAKE_HOSTNAME":  testHost,
	}
	l.setOS(osRocky8)
	l.setAddr(addrLine("ens160", oldIP, "24"))
	l.setRoute("default via 192.168.0.1 dev ens160 proto static metric 100")
	l.setLink("ens160", macEns, true)
	return l
}

func (l *lab) writeBin(name, content string) {
	l.t.Helper()
	if err := os.WriteFile(filepath.Join(l.bin, name), []byte(content), 0o755); err != nil {
		l.t.Fatal(err)
	}
}

func (l *lab) writeFake(name, content string) {
	l.t.Helper()
	if err := os.WriteFile(filepath.Join(l.fake, name), []byte(content), 0o644); err != nil {
		l.t.Fatal(err)
	}
}

// addrLine 은 "ip -4 -o addr show" 의 한 줄 모양입니다.
func addrLine(dev, ip, prefix string) string {
	return fmt.Sprintf("2: %s    inet %s/%s brd 192.168.0.255 scope global noprefixroute %s\\       valid_lft forever preferred_lft forever", dev, ip, prefix, dev)
}

const loLine = "1: lo    inet 127.0.0.1/8 scope host lo\\       valid_lft forever preferred_lft forever"

func (l *lab) setAddr(lines ...string) {
	l.writeFake("addr4", loLine+"\n"+strings.Join(lines, "\n")+"\n")
}

func (l *lab) setRoute(s string) { l.writeFake("route4", s+"\n") }

// setLink 는 "ip -o link show dev X" 와 "ip -o -d link show dev X" 출력을 만듭니다.
func (l *lab) setLink(dev, mac string, up bool, extra ...string) {
	flags, state := "<BROADCAST,MULTICAST,UP,LOWER_UP>", "UP"
	if !up {
		flags, state = "<NO-CARRIER,BROADCAST,MULTICAST,UP>", "DOWN"
	}
	base := fmt.Sprintf("3: %s: %s mtu 1500 qdisc mq state %s mode DEFAULT group default qlen 1000\\    link/ether %s brd ff:ff:ff:ff:ff:ff", dev, flags, state, mac)
	l.writeFake("link."+dev, base+"\n")
	l.writeFake("linkd."+dev, base+" promiscuity 0 minmtu 68 maxmtu 16110 "+strings.Join(extra, " ")+"\n")
}

func (l *lab) setOS(release string) { l.put("/etc/redhat-release", release+"\n", 0o644) }

// setNM 은 NetworkManager 가 실행 중이고 dev 의 프로필 파일이 file 이라고 가짜 nmcli 가 답하게 합니다.
func (l *lab) setNM(dev, uuid, file string) {
	l.writeFake("nm_running", "1\n")
	l.writeFake("nm_dev."+dev, "GENERAL.CONNECTION:"+dev+"\n")
	l.writeFake("nm_list", fmt.Sprintf("%s:%s:%s:%s\n", dev, uuid, dev, file))
}

func (l *lab) breakAwk() {
	l.t.Helper()
	real, err := exec.LookPath("awk")
	if err != nil {
		l.t.Skip("awk 를 찾지 못했습니다")
	}
	l.writeBin("awk", fakeAwk(real))
	l.env["FAKE_AWK_BREAK"] = "1"
}

// ---- 노드 파일 ----

func (l *lab) abs(p string) string { return filepath.Join(l.root, p) }

func (l *lab) put(p, content string, perm os.FileMode) {
	l.t.Helper()
	full := l.abs(p)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		l.t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), perm); err != nil {
		l.t.Fatal(err)
	}
	if err := os.Chmod(full, perm); err != nil {
		l.t.Fatal(err)
	}
}

func (l *lab) read(p string) string {
	l.t.Helper()
	b, err := os.ReadFile(l.abs(p))
	if err != nil {
		l.t.Fatalf("파일을 읽지 못했습니다: %v", err)
	}
	return string(b)
}

func (l *lab) exists(p string) bool {
	_, err := os.Lstat(l.abs(p))
	return err == nil
}

func (l *lab) perm(p string) os.FileMode {
	l.t.Helper()
	st, err := os.Stat(l.abs(p))
	if err != nil {
		l.t.Fatal(err)
	}
	return st.Mode().Perm()
}

// snapshot 은 ROOT 아래 파일(withDirs 이면 디렉토리도)의 권한과 내용 해시를 모읍니다.
func (l *lab) snapshot(skipBackup, withDirs bool) map[string]string {
	l.t.Helper()
	m := map[string]string{}
	err := filepath.WalkDir(l.root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(l.root, p)
		if rel == "." {
			return nil
		}
		if skipBackup && (rel == "var/lib/ip_change" || strings.HasPrefix(rel, "var/lib/ip_change/")) {
			return nil
		}
		if d.IsDir() {
			if withDirs {
				m[rel] = "dir"
			}
			return nil
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		st, _ := d.Info()
		m[rel] = fmt.Sprintf("%04o %x", st.Mode().Perm(), sha256.Sum256(b))
		return nil
	})
	if err != nil {
		l.t.Fatal(err)
	}
	return m
}

func diffSnap(a, b map[string]string) []string {
	var d []string
	for k, v := range a {
		if w, ok := b[k]; !ok {
			d = append(d, "사라짐: "+k)
		} else if v != w {
			d = append(d, "바뀜: "+k)
		}
	}
	for k := range b {
		if _, ok := a[k]; !ok {
			d = append(d, "생김: "+k)
		}
	}
	sort.Strings(d)
	return d
}

func wantSameTree(t *testing.T, a, b map[string]string, what string) {
	t.Helper()
	if d := diffSnap(a, b); len(d) > 0 {
		t.Errorf("%s: 파일이 다릅니다: %v", what, d)
	}
}

// ---- 스크립트 실행 ----

// runScript 는 스크립트를 bash 로 실행합니다. ROOT 가 비어 있거나 PATH 맨 앞에 가짜
// ip/hostname/nmcli 가 없으면 실행하지 않고 시험을 중단합니다 (실제 노드 파일 보호).
func runScript(t *testing.T, scriptPath string, env map[string]string) (string, int) {
	t.Helper()
	root := env["ROOT"]
	if root == "" || root == "/" || !filepath.IsAbs(root) {
		t.Fatalf("ROOT 가 비어 있거나 올바르지 않아 스크립트를 실행하지 않습니다: %q", root)
	}
	first := strings.Split(env["PATH"], ":")[0]
	for _, c := range []string{"ip", "hostname", "nmcli"} {
		if _, err := os.Stat(filepath.Join(first, c)); err != nil {
			t.Fatalf("PATH 맨 앞에 가짜 %s 가 없어 스크립트를 실행하지 않습니다: %v", c, err)
		}
	}
	cmd := exec.Command("bash", scriptPath)
	for k, v := range env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	var so, se bytes.Buffer
	cmd.Stdout, cmd.Stderr = &so, &se
	code := 0
	if err := cmd.Run(); err != nil {
		ee, ok := err.(*exec.ExitError)
		if !ok {
			t.Fatal(err)
		}
		code = ee.ExitCode()
	}
	if u, err := os.ReadFile(filepath.Join(env["FAKE_DIR"], "unexpected")); err == nil && len(u) > 0 {
		t.Fatalf("스크립트가 예상 밖 명령을 불렀습니다:\n%s", u)
	}
	if se.Len() > 0 {
		t.Errorf("표준에러 출력이 있습니다:\n%s", se.String())
	}
	return so.String(), code
}

func (l *lab) run(script string) (string, int) {
	l.t.Helper()
	l.n++
	p := filepath.Join(l.tmp, fmt.Sprintf("s%d.sh", l.n))
	if err := os.WriteFile(p, []byte(script), 0o755); err != nil {
		l.t.Fatal(err)
	}
	env := map[string]string{
		"PATH": l.bin + ":/usr/bin:/bin", "ROOT": l.root, "FAKE_DIR": l.fake,
		"TMPDIR": l.tmp, "HOME": l.tmp, "LC_ALL": "C",
	}
	for k, v := range l.env {
		env[k] = v
	}
	return runScript(l.t, p, env)
}

func defaultCfg() *config.Config {
	return &config.Config{NetworkScriptsDir: config.DefaultNetworkScriptsDir}
}

func (l *lab) apply(opts ApplyOptions) (string, int) {
	l.t.Helper()
	s, err := ApplyScriptWithOptions([]target.Entry{{Host: testHost, NewIP: newIP}}, defaultCfg(), opts)
	if err != nil {
		l.t.Fatal(err)
	}
	return l.run(s)
}

func (l *lab) rollback(stamp string) (string, int) {
	l.t.Helper()
	s, err := RollbackScript(defaultCfg(), stamp)
	if err != nil {
		l.t.Fatal(err)
	}
	return l.run(s)
}

func (l *lab) fixGateway(items ...GatewayFix) (string, int) {
	l.t.Helper()
	s, err := FixGatewayScript(items)
	if err != nil {
		l.t.Fatal(err)
	}
	return l.run(s)
}

// ---- 결과 줄 ----

type result struct{ f []string }

func parseResult(t *testing.T, out string) result {
	t.Helper()
	last := ""
	for _, ln := range strings.Split(out, "\n") {
		if strings.HasPrefix(ln, "RESULT|") {
			last = ln
		}
	}
	if last == "" {
		t.Fatalf("RESULT 줄이 없습니다:\n%s", out)
	}
	return result{strings.Split(last, "|")}
}

// applyResult 는 apply 결과(필드 11개)를 돌려줍니다.
func applyResult(t *testing.T, out string) result {
	t.Helper()
	r := parseResult(t, out)
	if len(r.f) != 11 {
		t.Fatalf("apply 결과 필드가 11개가 아닙니다 (%d): %v", len(r.f), r.f)
	}
	return r
}

func (r result) status() string  { return r.f[1] }
func (r result) dev() string     { return r.f[6] }
func (r result) mode() string    { return r.f[7] }
func (r result) osLabel() string { return r.f[8] }
func (r result) file() string    { return r.f[9] }
func (r result) note() string    { return r.f[10] }

var stampRe = regexp.MustCompile(`BACKUP=(\d+)`)

func stampOf(t *testing.T, r result) string {
	t.Helper()
	m := stampRe.FindStringSubmatch(r.note())
	if m == nil {
		t.Fatalf("비고에 BACKUP=<STAMP> 가 없습니다: %q", r.note())
	}
	return m[1]
}

// wantResult 는 상태, 장치, 방식, OS, 파일과 IP 세 칸을 한 번에 확인합니다.
func wantResult(t *testing.T, r result, status, dev, mode, osLabel, file string) {
	t.Helper()
	if r.status() != status || r.dev() != dev || r.mode() != mode || r.osLabel() != osLabel || r.file() != file {
		t.Errorf("결과 = 상태 %q 장치 %q 방식 %q OS %q 파일 %q\n기대 = 상태 %q 장치 %q 방식 %q OS %q 파일 %q\n비고: %s",
			r.status(), r.dev(), r.mode(), r.osLabel(), r.file(), status, dev, mode, osLabel, file, r.note())
	}
	if r.f[3] != oldIP || r.f[4] != newIP || r.f[5] != newGW {
		t.Errorf("IP/게이트웨이 = %q %q %q", r.f[3], r.f[4], r.f[5])
	}
}

func wantText(t *testing.T, got, want, what string) {
	t.Helper()
	if got != want {
		t.Errorf("%s\n--- 실제\n%s\n--- 기대\n%s", what, got, want)
	}
}

func join(lines ...string) string { return strings.Join(lines, "\n") + "\n" }
