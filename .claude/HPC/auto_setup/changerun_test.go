package main

import (
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
)

// ---- 가짜 세계: 엔진·gossh 출력 흉내 (실제 엔진의 출력 형식 그대로) ----

const (
	chgSecret     = "S3cr3t-BindPW-xyz" // ldap_config.conf infra.alpha.bindpw
	chgSecret2    = "Other#Pw'9-beta"   // infra.beta.bindpw
	chgNodeSecret = "old-node-pw-777"   // 노드에 있던 기존 값 (conf 에 없음 — 키 패턴 마스킹으로만 가려짐)
	chgDashes     = "------------------------------------------------------------"
)

type chgCall struct {
	name  string
	args  []string
	env   []string
	files map[string]string // 인자로 넘긴 파일·래퍼 내용
	via   string            // os8 | os6
	kind  string            // ip | ipRb | ldapDry | ldap | ldapRb | appl
	hosts []string
}

type chgWorld struct {
	mu               sync.Mutex
	calls            []chgCall
	os8Down, os6Down map[string]bool
	ipFail           map[string]string // host → 사유
	ldap             map[string]string // host → OK|NOCHANGE|FAIL|SKIP (기본 OK)
	appl             map[string]string // host → ok|nofile|noscript (기본 ok)
	gate             chan struct{}     // non-nil: 출력 줄을 낸 뒤 gate 가 닫히거나 cancel 될 때까지 대기
	block            bool              // true: cancel 될 때까지 대기 후 Killed
	color            bool              // OK 줄에 ANSI 색
	engineErr        string            // non-empty: 종료 1 + 표준에러 "오류: …"
}

func newChgWorld() *chgWorld {
	return &chgWorld{os8Down: map[string]bool{}, os6Down: map[string]bool{}, ipFail: map[string]string{},
		ldap: map[string]string{}, appl: map[string]string{}}
}

func argAfter(args []string, flag string) string {
	for i := 0; i+1 < len(args); i++ {
		if args[i] == flag {
			return args[i+1]
		}
	}
	return ""
}

func hasArg(args []string, a string) bool {
	for _, x := range args {
		if x == a {
			return true
		}
	}
	return false
}

func (w *chgWorld) exec(c changeCmd, cancel <-chan struct{}, onLine func(string)) changeOut {
	call := chgCall{name: c.Name, args: append([]string(nil), c.Args...), env: c.Env, files: map[string]string{}}
	g := argAfter(c.Args, "-gossh")
	base := filepath.Base(c.Name)
	switch base {
	case "ip-change-engine":
		call.kind = "ip"
		if hasArg(c.Args, "-rollback") {
			call.kind = "ipRb"
		}
	case "ldap-config-engine":
		call.kind = "ldap"
		if hasArg(c.Args, "-dry-run") {
			call.kind = "ldapDry"
		}
		if hasArg(c.Args, "-rollback") {
			call.kind = "ldapRb"
		}
	default:
		call.kind, g = "appl", c.Name
	}
	call.via = "os8"
	if strings.HasSuffix(g, "gossh_os6.sh") {
		call.via = "os6"
		b, _ := os.ReadFile(g)
		call.files[g] = string(b)
	}
	for _, f := range []string{"-targets", "-host-file", "-w"} {
		if p := argAfter(c.Args, f); p != "" {
			b, _ := os.ReadFile(p)
			call.files[p] = string(b)
			for _, l := range strings.Split(string(b), "\n") {
				if fs := strings.Fields(l); len(fs) > 0 {
					call.hosts = append(call.hosts, fs[0])
				}
			}
		}
	}
	w.mu.Lock()
	w.calls = append(w.calls, call)
	down := w.os8Down
	if call.via == "os6" {
		down = w.os6Down
	}
	w.mu.Unlock()

	var o changeOut
	switch call.kind {
	case "ip", "ipRb":
		o = w.ipOut(call, down)
	case "appl":
		o = w.applOut(call, down)
	default:
		o = w.ldapOut(call, down)
	}
	for _, l := range strings.Split(o.Stdout+o.Stderr, "\n") {
		if l != "" && onLine != nil {
			onLine(l)
		}
	}
	if w.block {
		<-cancel
		return changeOut{RC: -1, Killed: true, Stdout: "대상=1대\n"}
	}
	if w.gate != nil {
		select {
		case <-w.gate:
		case <-cancel:
			return changeOut{RC: -1, Killed: true}
		}
	}
	return o
}

func (w *chgWorld) ipOut(c chgCall, down map[string]bool) changeOut {
	if w.engineErr != "" {
		return changeOut{RC: 1, Stderr: "오류: " + w.engineErr + "\n"}
	}
	hs := append([]string(nil), c.hosts...)
	sort.Strings(hs)
	var b strings.Builder
	b.WriteString(fmt.Sprintf("대상=%d대  network_scripts_dir=/etc/sysconfig/network-scripts\n", len(hs)))
	if c.kind == "ip" {
		b.WriteString("작업 대상:\n")
		for _, l := range strings.Split(c.files[argAfter(c.args, "-targets")], "\n") {
			if f := strings.Fields(l); len(f) == 2 {
				fmt.Fprintf(&b, "  %-20s -> %s\n", f[0], f[1])
			}
		}
	}
	b.WriteString(chgDashes + "\n")
	alldown := true
	for _, h := range hs {
		if !down[h] {
			alldown = false
		}
	}
	if alldown {
		b.WriteString("gossh 출력이 비어 있습니다.\n")
		return changeOut{RC: 2, Stdout: b.String()}
	}
	fail := 0
	ip := map[string]string{}
	for _, l := range strings.Split(c.files[argAfter(c.args, "-targets")], "\n") {
		if f := strings.Fields(l); len(f) == 2 {
			ip[f[0]] = f[1]
		}
	}
	for _, h := range hs {
		switch {
		case down[h]:
			fmt.Fprintf(&b, "%-20s UNREACHABLE (gossh 응답 없음)\n", h)
			fail++
		case w.ipFail[h] != "":
			fmt.Fprintf(&b, "%-20s FAIL: %s\n    BINDPW %s\n    추가 상세\n", h, w.ipFail[h], chgSecret)
			fail++
		case c.kind == "ipRb":
			fmt.Fprintf(&b, "%-20s 복원 ifcfg-eth0 -> IPADDR 10.0.0.9   (STAMP 20261012000000)\n", h)
		default:
			line := fmt.Sprintf("%-20s 10.0.0.9 -> %s   (GW %s)", h, ip[h], ipGateway(ip[h]))
			if w.color {
				line = "\x1b[32m" + line + "\x1b[0m"
			}
			b.WriteString(line + "\n")
		}
	}
	b.WriteString(chgDashes + "\n")
	fmt.Fprintf(&b, "OK %d대   FAIL %d대\n", len(hs)-fail, fail)
	rc := 0
	if fail > 0 {
		rc = 2
	}
	return changeOut{RC: rc, Stdout: b.String()}
}

func (w *chgWorld) ldapOut(c chgCall, down map[string]bool) changeOut {
	if w.engineErr != "" {
		return changeOut{RC: 1, Stderr: "오류: " + w.engineErr + "\n"}
	}
	hs := append([]string(nil), c.hosts...)
	sort.Strings(hs)
	var b, e strings.Builder
	var skip []string
	var run []string
	for _, h := range hs {
		if w.ldap[h] == "SKIP" && c.kind != "ldapRb" {
			skip = append(skip, h)
			continue
		}
		run = append(run, h)
	}
	if len(skip) > 0 {
		fmt.Fprintf(&e, "경고: 다음 호스트는 자산현황(/x/assets.txt)에 없고 default_site 도 없어 건너뜁니다: %s\n", strings.Join(skip, ", "))
	}
	alldown := len(run) > 0
	for _, h := range run {
		if !down[h] {
			alldown = false
		}
	}
	if c.kind == "ldapRb" {
		fmt.Fprintf(&b, "되돌리기(가장 최근 시점)  대상=%d대  모드=적용\n", len(run))
		if alldown {
			return changeOut{RC: 1, Stdout: b.String(), Stderr: "오류: gossh 출력이 비어 있습니다\n"}
		}
	} else {
		mode := "적용"
		if c.kind == "ldapDry" {
			mode = "DRY-RUN (실제 변경 없음)"
		}
		fmt.Fprintf(&b, "인프라=%s  대상=%d대  사이트=a1  모드=%s\n", argAfter(c.args, "-infra"), len(run), mode)
	}
	b.WriteString(strings.Repeat("-", 70) + "\n")
	if c.kind != "ldapRb" {
		fmt.Fprintf(&b, "[a1] %d대 → gossh 전송\n", len(run))
		if alldown {
			b.WriteString("[a1] gossh 출력이 비어 있습니다.\n")
		}
	}
	bad := 0
	for _, h := range run {
		st := w.ldap[h]
		if st == "" {
			st = "OK"
		}
		switch {
		case alldown:
			bad++
		case down[h]:
			fmt.Fprintf(&b, "  %-24s UNREACHABLE (gossh 응답 없음)\n", h)
			bad++
		case st == "FAIL":
			fmt.Fprintf(&b, "  %-24s FAIL\n", h)
			fmt.Fprintf(&b, "      APPLY|INFO|host=%s os=8 infra=alpha site=a1\n", h)
			b.WriteString("      APPLY|FAIL|/etc/sssd/sssd.conf 에 도메인 섹션이 없습니다\n")
			fmt.Fprintf(&b, "      DIFF|-BINDPW %s\n      DIFF|+BINDPW %s\n", chgNodeSecret, chgSecret)
			fmt.Fprintf(&b, "      DIFF|+ldap_default_authtok = %s\n", chgSecret)
			fmt.Fprintf(&b, "      RESULT|%s|FAIL|alpha|a1\n", h)
			bad++
		default:
			fmt.Fprintf(&b, "  %-24s %s\n", h, st)
		}
	}
	b.WriteString(strings.Repeat("-", 70) + "\n")
	rc := 0
	if bad > 0 {
		rc = 2
	}
	return changeOut{RC: rc, Stdout: b.String(), Stderr: e.String()}
}

func (w *chgWorld) applOut(c chgCall, down map[string]bool) changeOut {
	var b strings.Builder
	for _, h := range c.hosts {
		if down[h] {
			continue
		}
		switch w.appl[h] {
		case "nofile":
			fmt.Fprintf(&b, "%s: APPL|MSG|원복할 파일이 없습니다: /etc/auto.appl.bak\n%s: APPL|RC|1\n", h, h)
		case "noscript":
			fmt.Fprintf(&b, "%s: APPL|NOSCRIPT\n", h)
		default:
			fmt.Fprintf(&b, "%s: APPL|MSG|원복 완료: /etc/auto.appl\n%s: APPL|RC|0\n", h, h)
		}
	}
	return changeOut{RC: 0, Stdout: b.String()}
}

func (w *chgWorld) callsOf(kind string) []chgCall {
	w.mu.Lock()
	defer w.mu.Unlock()
	var out []chgCall
	for _, c := range w.calls {
		if c.kind == kind {
			out = append(out, c)
		}
	}
	return out
}

// ---- 테스트 리그 ----

type chgRig struct {
	env  *changeEnv
	w    *chgWorld
	dir  string
	logs []string
	mu   sync.Mutex
}

func newChgRig(t *testing.T) *chgRig {
	t.Helper()
	r := &chgRig{w: newChgWorld(), dir: t.TempDir()}
	conf := filepath.Join(r.dir, "conf")
	if err := os.MkdirAll(conf, 0700); err != nil {
		t.Fatal(err)
	}
	ldapConf := "# 테스트\ninfra.alpha.dns=1.1.1.1\ninfra.alpha.ntp=2.2.2.2\ninfra.alpha.binddn=uid=svc,ou=user\n" +
		"infra.alpha.bindpw=" + chgSecret + "\ninfra.alpha.uri1=ldap://a/\n" +
		"infra.alpha.site.a1.storage=st1\ninfra.alpha.site.a1.mountpoint=/m\ninfra.alpha.site.a1.uri_order=uri1\n" +
		"infra.beta.dns=1.1.1.1\ninfra.beta.ntp=2.2.2.2\ninfra.beta.binddn=uid=svc\ninfra.beta.bindpw=" + chgSecret2 + "\n" +
		"infra.beta.uri1=ldap://b/\ninfra.beta.site.a1.storage=st1\ninfra.beta.site.a1.mountpoint=/m\ninfra.beta.site.a1.uri_order=uri1\n"
	if err := os.WriteFile(filepath.Join(conf, "ldap_config.conf"), []byte(ldapConf), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(conf, "assets.txt"), []byte("h1\ta1\nh2\ta1\nh3\ta2\nbm9\ta3\n"), 0644); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 12, 9, 30, 0, 0, time.Local)
	var clk sync.Mutex
	r.env = &changeEnv{
		Now: func() time.Time {
			clk.Lock()
			defer clk.Unlock()
			now = now.Add(time.Second)
			return now
		},
		Exec: r.w.exec, StateDir: filepath.Join(r.dir, "state"),
		Log: func(f string, a ...interface{}) {
			r.mu.Lock()
			r.logs = append(r.logs, fmt.Sprintf(f, a...))
			r.mu.Unlock()
		},
		Tracked: map[string]string{}, Routes: map[string]string{},
		OS6Mgmt: "os6mgmt", OS6Gossh: "/opt/gossh/gossh", Gossh: "gossh",
		IPEngine: "/x/ip-change-engine", LDAPEngine: "/x/ldap-config-engine", ConfDir: conf,
		IPOn: true, LDAPOn: true,
	}
	return r
}

// chgKeys: 문자열을 키로 ('\n' Enter, 0x1b Esc, 그 밖은 rune — 0x04 Ctrl+D, 0x18 Ctrl+X, 0x7f Backspace)
func chgKeys(c *changeState, s string) (closed bool) {
	for _, r := range s {
		ev := keyEv{k: kRune, r: r}
		switch r {
		case '\n':
			ev = keyEv{k: kEnter}
		case 0x1b:
			ev = keyEv{k: kEsc}
		}
		if c.handleKey(ev) {
			closed = true
		}
	}
	return
}

func chgWait(t *testing.T, c *changeState) {
	t.Helper()
	waitFor(t, "변경 작업 종료", func() bool { c.tick(); return c.job == nil })
}

// chgRun: user → (infra) → 입력 → (dry-run) → yes → 결과까지
func chgRun(t *testing.T, r *chgRig, kind changeKind, user, infraNo, input string) *changeState {
	t.Helper()
	c := newChangeState(r.env, kind)
	chgKeys(c, user+"\n")
	if infraNo != "" {
		chgKeys(c, infraNo+"\n")
	}
	if input != "" {
		chgKeys(c, input)
	}
	chgKeys(c, "\x04")
	if c.step == csDry {
		chgWait(t, c)
	}
	if c.step != csPreview {
		t.Fatalf("미리보기가 아님: step=%d msg=%q", c.step, c.msg)
	}
	chgKeys(c, "yes\n")
	chgWait(t, c)
	if c.step != csResult {
		t.Fatalf("결과 화면이 아님: step=%d", c.step)
	}
	return c
}

func chgRes(c *changeState) map[string]*chgHost {
	m := map[string]*chgHost{}
	for _, h := range c.results {
		m[h.Host] = h
	}
	return m
}

// ---- 경유 폴백 ----

func TestChangeRoutingOS8ThenOS6(t *testing.T) {
	r := newChgRig(t)
	r.w.os8Down["h2"] = true
	r.env.Routes["h3"] = "os6" // 이미 os6 로 알려진 호스트
	c := chgRun(t, r, kindIP, "user1", "", "h1 10.1.1.5\nh2 10.1.1.6\nh3 10.1.1.7\n")
	calls := r.w.callsOf("ip")
	if len(calls) != 2 {
		t.Fatalf("호출 2번(os8 → os6) 이어야 함: %d", len(calls))
	}
	if calls[0].via != "os8" || strings.Join(calls[0].hosts, ",") != "h1,h2" {
		t.Errorf("1차 os8: via=%s hosts=%v (h3 는 처음부터 os6)", calls[0].via, calls[0].hosts)
	}
	got := append([]string(nil), calls[1].hosts...)
	sort.Strings(got)
	if calls[1].via != "os6" || strings.Join(got, ",") != "h2,h3" {
		t.Errorf("2차 os6: via=%s hosts=%v (os8 응답 없는 h2 + 알려진 h3 만)", calls[1].via, got)
	}
	res := chgRes(c)
	for h, want := range map[string]string{"h1": "os8", "h2": "os6", "h3": "os6"} {
		if res[h].IPResult != rsOK || res[h].Route != want {
			t.Errorf("%s: %+v (route %s 기대)", h, res[h], want)
		}
	}
	if res["h1"].OldIP != "10.0.0.9" || res["h1"].NewIP != "10.1.1.5" || res["h1"].GW != "10.1.1.1" {
		t.Errorf("IP 파싱: %+v", res["h1"])
	}
	// 래퍼: 0700 임시 파일, 비밀값·-p 없음, 작업 뒤 삭제
	for p, body := range calls[1].files {
		if strings.HasSuffix(p, "gossh_os6.sh") {
			if !strings.Contains(body, "BatchMode=yes") || strings.Contains(body, chgSecret) {
				t.Errorf("래퍼 내용 이상:\n%s", body)
			}
			if _, err := os.Stat(p); !os.IsNotExist(err) {
				t.Errorf("래퍼가 남아 있음: %s", p)
			}
			if _, err := os.Stat(filepath.Dir(p)); !os.IsNotExist(err) {
				t.Errorf("임시 디렉터리가 남아 있음: %s", filepath.Dir(p))
			}
		}
	}
	for _, cl := range r.w.calls {
		if hasArg(cl.args, "-p") || hasArg(cl.args, "-monitor") || hasArg(cl.args, "-print-script") {
			t.Errorf("금지 인자: %v", cl.args)
		}
		if !hasArg(cl.env, "NO_COLOR=1") {
			t.Errorf("NO_COLOR 없음: %v", cl.env)
		}
	}
}

func TestChangeRoutingNoOS6NoRetry(t *testing.T) {
	r := newChgRig(t)
	r.env.OS6Mgmt = ""
	r.w.os8Down["h2"] = true
	r.env.Routes["h1"] = "os6" // os6 설정이 없으면 무시하고 os8
	c := chgRun(t, r, kindIP, "user1", "", "h1 10.1.1.5\nh2 10.1.1.6\n")
	if n := len(r.w.calls); n != 1 || r.w.calls[0].via != "os8" {
		t.Fatalf("os6 설정이 없으면 os8 한 번만: %d", n)
	}
	res := chgRes(c)
	if res["h2"].IPResult != rsUnreach || res["h2"].Route != "os8" || res["h1"].IPResult != rsOK {
		t.Errorf("결과: h1=%+v h2=%+v", res["h1"], res["h2"])
	}
}

func TestChangeRoutingBothRoutesDown(t *testing.T) {
	r := newChgRig(t)
	r.w.os8Down["h1"], r.w.os6Down["h1"] = true, true
	c := chgRun(t, r, kindIP, "user1", "", "h1 10.1.1.5\n")
	h := chgRes(c)["h1"]
	if h.IPResult != rsUnreach || !strings.Contains(h.Detail, "os8·os6") {
		t.Errorf("둘 다 응답 없음: %+v", h)
	}
}

// ---- IP+LDAP ----

func TestChangeBothSkipsLDAPOnIPFail(t *testing.T) {
	r := newChgRig(t)
	r.w.ipFail["h2"] = "ifcfg 를 찾지 못했습니다"
	r.w.os8Down["h3"] = true
	r.w.ldap["h3"] = "OK"
	c := chgRun(t, r, kindBoth, "user1", "1", "h1 10.1.1.5\nh2 10.1.1.6\nh3 10.1.1.7\n")
	ld := r.w.callsOf("ldap")
	if len(ld) != 2 {
		t.Fatalf("LDAP 적용 호출 2번 (h1 os8, h3 os6): %d", len(ld))
	}
	// h3 는 IP 를 os6 경유로 했으므로 LDAP 도 처음부터 os6 → os8 호출에는 h1 만, h2(IP 실패)는 어디에도 없음
	if strings.Join(ld[0].hosts, ",") != "h1" || ld[0].via != "os8" || strings.Join(ld[1].hosts, ",") != "h3" || ld[1].via != "os6" {
		t.Errorf("LDAP 대상: %v via %s / %v via %s", ld[0].hosts, ld[0].via, ld[1].hosts, ld[1].via)
	}
	if !hasArg(ld[0].args, ldapNoRestart) || argAfter(ld[0].args, "-infra") != "alpha" {
		t.Errorf("LDAP 인자: %v", ld[0].args)
	}
	res := chgRes(c)
	if res["h2"].IPResult != rsFail || res["h2"].LDAPResult != rsSkipIP {
		t.Errorf("h2: %+v", res["h2"])
	}
	if res["h1"].LDAPResult != rsOK || !res["h1"].ApplPending || res["h1"].Site != "a1" {
		t.Errorf("h1: %+v", res["h1"])
	}
	if res["h3"].IPResult != rsOK || res["h3"].LDAPResult != rsOK || res["h3"].Route != "os6" {
		t.Errorf("h3: %+v", res["h3"])
	}
	if !strings.Contains(res["h2"].Detail, "ifcfg 를 찾지 못했습니다") || strings.Contains(res["h2"].Detail, chgSecret) {
		t.Errorf("h2 detail: %q", res["h2"].Detail)
	}
	out := stripANSI(renderChange(c, 100, 0, false))
	if !strings.Contains(out, "건너뜀(IP 실패)") {
		t.Errorf("결과 화면에 건너뜀(IP 실패) 없음:\n%s", out)
	}
}

func TestChangeBothAllIPFailNoLDAP(t *testing.T) {
	r := newChgRig(t)
	r.w.ipFail["h1"], r.w.ipFail["h2"] = "x", "y"
	c := chgRun(t, r, kindBoth, "user1", "1", "h1 10.1.1.5\nh2 10.1.1.6\n")
	if n := len(r.w.callsOf("ldap")); n != 0 {
		t.Errorf("IP 전부 실패면 LDAP 실행 안 함: %d", n)
	}
	for _, h := range c.results {
		if h.LDAPResult != rsSkipIP {
			t.Errorf("%s: %+v", h.Host, h)
		}
	}
}

// ---- LDAP 원복: -no-restart 고정 ----

func TestChangeLDAPArgsAlwaysNoRestart(t *testing.T) {
	for _, mode := range []string{"dry", "apply", "rollback"} {
		a := ldapEngineArgs(mode, "/etc/auto_setup", "alpha", "/tmp/h", "gossh")
		if !hasArg(a, "-no-restart") {
			t.Errorf("%s: -no-restart 없음: %v", mode, a)
		}
		for _, bad := range []string{"-p", "-print-script", "-monitor", "-u", "-root"} {
			if hasArg(a, bad) {
				t.Errorf("%s: 금지 인자 %s: %v", mode, bad, a)
			}
		}
	}
	rb := ldapEngineArgs("rollback", "/c", "alpha", "/tmp/h", "g")
	if !hasArg(rb, "-rollback") || hasArg(rb, "-infra") || hasArg(rb, "-dry-run") {
		t.Errorf("원복 인자: %v", rb)
	}
	if d := ldapEngineArgs("dry", "/c", "alpha", "/tmp/h", "g"); !hasArg(d, "-dry-run") || argAfter(d, "-infra") != "alpha" {
		t.Errorf("dry 인자: %v", d)
	}
	if a := ipEngineArgs("/c", "/t", "g", true); !hasArg(a, "-rollback") || argAfter(a, "-targets") != "/t" || hasArg(a, "-p") {
		t.Errorf("IP 원복 인자: %v", a)
	}
}

// ---- 출력 파싱 ----

func TestChangeParseIPOut(t *testing.T) {
	out := "대상=3대  network_scripts_dir=/x\n작업 대상:\n  h1                   -> 10.1.1.5\n" + chgDashes + "\n" +
		"\x1b[32mh1                   10.0.0.9 -> 10.1.1.5   (GW 10.1.1.1)\x1b[0m\n" +
		"\x1b[1m\x1b[31mh2                   FAIL: ifcfg 없음\x1b[0m\n\x1b[33m    상세1\x1b[0m\n" +
		"h3                   UNREACHABLE (gossh 응답 없음)\n" +
		"averyveryverylonghostname01  -> 10.1.1.9   (GW 10.1.1.1)\n" + chgDashes + "\nOK 1대   FAIL 2대\n"
	m := parseIPOut(changeOut{RC: 2, Stdout: out}, []string{"h1", "h2", "h3", "averyveryverylonghostname01", "h9"}, false)
	if e := m["h1"]; e.st != rsOK || e.old != "10.0.0.9" || e.new != "10.1.1.5" || e.gw != "10.1.1.1" {
		t.Errorf("h1: %+v", e)
	}
	if e := m["h2"]; e.st != rsFail || e.detail != "ifcfg 없음 / 상세1" {
		t.Errorf("h2: %+v", e)
	}
	if m["h3"].st != rsUnreach {
		t.Errorf("h3: %+v", m["h3"])
	}
	if e := m["averyveryverylonghostname01"]; e.st != rsOK || e.old != "" || e.new != "10.1.1.9" {
		t.Errorf("기존 IP 없는 OK: %+v", e)
	}
	if _, ok := m["h9"]; ok {
		t.Error("출력에 없는 호스트는 결과 없음(→ 응답 없음)")
	}
	rb := parseIPOut(changeOut{Stdout: chgDashes + "\nh1                   복원 ifcfg-eth0 -> IPADDR 10.0.0.9   (STAMP 20261012000000)\n"}, []string{"h1"}, true)
	if e := rb["h1"]; e.st != rsOK || e.new != "10.0.0.9" || !strings.Contains(e.detail, "STAMP 20261012000000") {
		t.Errorf("원복 OK: %+v", e)
	}
	// 종료 1 = 엔진 오류 → 전부 FAIL (재시도 없음), gossh 출력 없음(종료 2) → 결과 없음(응답 없음)
	ee := parseIPOut(changeOut{RC: 1, Stderr: "오류: -targets 를 지정하십시오.\n"}, []string{"h1"}, false)
	if ee["h1"].st != rsFail || !strings.Contains(ee["h1"].detail, "-targets") {
		t.Errorf("엔진 오류: %+v", ee["h1"])
	}
	if e := parseIPOut(changeOut{RC: 2, Stdout: chgDashes + "\ngossh 출력이 비어 있습니다.\n"}, []string{"h1"}, false); len(e) != 0 {
		t.Errorf("gossh 출력 없음: %+v", e)
	}
	if e := parseIPOut(changeOut{RC: -1, StartErr: os.ErrNotExist}, []string{"h1"}, false); e["h1"].st != rsFail {
		t.Errorf("시작 실패: %+v", e)
	}
}

func TestChangeParseLDAPOut(t *testing.T) {
	mask := &secretMask{vals: []string{chgSecret}}
	out := "인프라=alpha  대상=4대  사이트=a1  모드=적용\n" + strings.Repeat("-", 70) + "\n[a1] 4대 → gossh 전송\n" +
		"  h1                       OK\n  h2                       NOCHANGE\n  h3                       FAIL\n" +
		"      APPLY|INFO|host=h3\n      APPLY|FAIL|도메인 섹션 없음\n      DIFF|+BINDPW " + chgSecret + "\n      RESULT|h3|FAIL|alpha|a1\n" +
		"  h4                       UNREACHABLE (gossh 응답 없음)\n  h5                       NORESULT\n      이상한 줄 bindpw " + chgNodeSecret + "\n" +
		strings.Repeat("-", 70) + "\nFAIL         1대\n"
	o := changeOut{RC: 2, Stdout: mask.apply(out), Stderr: "경고: 다음 호스트는 자산현황(/x/a.txt)에 없고 default_site 도 없어 건너뜁니다: h6, h7\n"}
	m := parseLDAPOut(o, []string{"h1", "h2", "h3", "h4", "h5", "h6"})
	want := map[string]string{"h1": rsOK, "h2": rsNoChange, "h3": rsFail, "h4": rsUnreach, "h5": rsNoResult, "h6": rsSkipAsset}
	for h, st := range want {
		if m[h].st != st {
			t.Errorf("%s: %+v (기대 %s)", h, m[h], st)
		}
	}
	if m["h3"].detail != "도메인 섹션 없음" {
		t.Errorf("FAIL 상세는 APPLY|FAIL 줄만: %q", m["h3"].detail)
	}
	if strings.Contains(m["h5"].detail, chgNodeSecret) || !strings.Contains(m["h5"].detail, "****") {
		t.Errorf("NORESULT 상세 마스킹: %q", m["h5"].detail)
	}
	// 원복: gossh 출력 없음(종료 1) → 결과 없음, 다른 종료 1 → 전부 FAIL
	if e := parseLDAPOut(changeOut{RC: 1, Stderr: "오류: gossh 출력이 비어 있습니다\n"}, []string{"h1"}); len(e) != 0 {
		t.Errorf("원복 gossh 출력 없음: %+v", e)
	}
	e := parseLDAPOut(changeOut{RC: 1, Stderr: "오류: 자산현황 파일: open /x: no such file\n"}, []string{"h1"})
	if e["h1"].st != rsFail || !strings.Contains(e["h1"].detail, "자산현황") {
		t.Errorf("엔진 오류: %+v", e)
	}
}

func TestChangeParseApplOut(t *testing.T) {
	out := "h1: APPL|MSG|원복 완료: /etc/auto.appl\nh1: APPL|RC|0\nh2: APPL|NOSCRIPT\n" +
		"h3: APPL|MSG|원복할 파일이 없습니다: /etc/auto.appl.bak\nh3: APPL|RC|1\nh4: APPL|MSG|원복 실패: x\nh4: APPL|RC|1\nzz: APPL|RC|0\n"
	m := parseApplOut(changeOut{Stdout: out}, []string{"h1", "h2", "h3", "h4", "h5"})
	want := map[string]string{"h1": rsOK, "h2": rsFail, "h3": rsNoPending, "h4": rsFail}
	for h, st := range want {
		if m[h].st != st {
			t.Errorf("%s: %+v", h, m[h])
		}
	}
	if _, ok := m["h5"]; ok {
		t.Error("응답 없는 호스트는 결과 없음")
	}
	if _, ok := m["zz"]; ok {
		t.Error("대상이 아닌 호스트 무시")
	}
	// 원격 스크립트: 재시작 명령 없음, 위험어 없음
	for _, bad := range []string{"systemctl", "service ", "restart", "reboot", "halt"} {
		if strings.Contains(applRemoteScript, bad) {
			t.Errorf("auto.appl 원복 스크립트에 %q", bad)
		}
	}
	cmd := buildRemoteCmd(applRemoteScript)
	inner := strings.TrimSuffix(strings.TrimPrefix(cmd, "echo "), " | tr -d . | base64 -d | bash")
	b, err := base64.StdEncoding.DecodeString(strings.ReplaceAll(inner, ".", ""))
	if err != nil || !strings.Contains(string(b), "base64 -d") {
		t.Errorf("원격 명령 감싸기: %v", err)
	}
}

// ---- 래퍼 이스케이프 (bash 필요) ----

func TestChangeOS6WrapperEscape(t *testing.T) {
	needBash(t)
	if runtime.GOOS == "windows" {
		t.Skip("bash 래퍼는 리눅스에서만")
	}
	dir := t.TempDir()
	out := filepath.Join(dir, "out")
	bin := filepath.Join(dir, "bin")
	gdir := filepath.Join(dir, "g dir $x")
	for _, d := range []string{out, bin, gdir} {
		if err := os.MkdirAll(d, 0755); err != nil {
			t.Fatal(err)
		}
	}
	gossh := `#!/bin/bash
: > "$OUTDIR/args"
for a in "$@"; do printf '[%s]\n' "$a" >> "$OUTDIR/args"; done
w=""; p=""
for a in "$@"; do [ "$p" = -w ] && w=$a; p=$a; done
cat "$w" > "$OUTDIR/hosts"
echo "h1: hello"
exit 3
`
	ssh := `#!/bin/bash
[ "$1" = -o ] && [ "$2" = BatchMode=yes ] || { echo "BatchMode 없음" >&2; exit 9; }
printf '%s' "$3" > "$OUTDIR/mgmt"
exec bash -c "$4"
`
	if err := os.WriteFile(filepath.Join(gdir, "gossh"), []byte(gossh), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bin, "ssh"), []byte(ssh), 0755); err != nil {
		t.Fatal(err)
	}
	wp := filepath.Join(dir, "gossh_os6.sh")
	if err := os.WriteFile(wp, []byte(chgOS6WrapperScript("root@os6 'mgmt'", filepath.Join(gdir, "gossh"))), 0700); err != nil {
		t.Fatal(err)
	}
	hf := filepath.Join(dir, "hosts.txt")
	if err := os.WriteFile(hf, []byte("h1\nh2 x\n"), 0600); err != nil {
		t.Fatal(err)
	}
	args := []string{"-pm", "-script", "-w", hf, "-u", "root", "echo ab cd | tr -d ' ' | base64 -d | bash",
		`it's "q" $HOME \back`, "semi;colon && $(touch " + filepath.Join(out, "pwned") + ")", "", "탭\t줄\n바꿈"}
	cmd := exec.Command(wp, args...)
	cmd.Env = append(os.Environ(), "PATH="+bin+":"+os.Getenv("PATH"), "OUTDIR="+out)
	so, err := cmd.Output()
	if ee, ok := err.(*exec.ExitError); !ok || ee.ExitCode() != 3 {
		t.Fatalf("원격 종료코드가 전달되어야 함(3): %v", err)
	}
	if string(so) != "h1: hello\n" {
		t.Errorf("표준출력: %q", so)
	}
	got, _ := os.ReadFile(filepath.Join(out, "args"))
	lines := strings.Split(strings.TrimSuffix(string(got), "\n"), "\n")
	var want []string
	for i, a := range args {
		if i == 3 {
			continue // -w 파일은 os6_mgmt 의 mktemp 경로로 바뀜
		}
		want = append(want, "["+a+"]")
	}
	joined := strings.Join(lines, "\n")
	for _, w := range want {
		if !strings.Contains(joined, w) {
			t.Errorf("인자 %q 가 그대로 전달되지 않음:\n%s", w, joined)
		}
	}
	if strings.Contains(joined, "["+hf+"]") {
		t.Errorf("로컬 -w 경로가 원격에 그대로 전달됨")
	}
	if h, _ := os.ReadFile(filepath.Join(out, "hosts")); string(h) != "h1\nh2 x\n" {
		t.Errorf("호스트 파일 전달: %q", h)
	}
	if m, _ := os.ReadFile(filepath.Join(out, "mgmt")); string(m) != "root@os6 'mgmt'" {
		t.Errorf("os6_mgmt: %q", m)
	}
	if _, err := os.Stat(filepath.Join(out, "pwned")); err == nil {
		t.Error("인자 안의 $(...) 가 실행됨 — 이스케이프 실패")
	}
	// -w 없으면 실패
	cmd = exec.Command(wp, "-script", "x")
	cmd.Env = append(os.Environ(), "PATH="+bin+":"+os.Getenv("PATH"), "OUTDIR="+out)
	if err := cmd.Run(); err == nil {
		t.Error("-w 없으면 실패해야 함")
	}
}

// ---- 실제 실행기: 취소 시 프로세스 그룹 종료 ----

func TestChangeRealExecCancelKillsGroup(t *testing.T) {
	needBash(t)
	if runtime.GOOS == "windows" {
		t.Skip()
	}
	dir := t.TempDir()
	pidf := filepath.Join(dir, "child")
	cancel := make(chan struct{})
	var lines []string
	done := make(chan changeOut, 1)
	go func() {
		done <- realChangeExec(changeCmd{Name: "bash", Args: []string{"-c", "echo start; sleep 60 & echo $! > " + pidf + "; wait"}},
			cancel, func(l string) { lines = append(lines, l) })
	}()
	waitFor(t, "자식 시작", func() bool { _, err := os.Stat(pidf); return err == nil })
	close(cancel)
	var o changeOut
	select {
	case o = <-done:
	case <-time.After(oneoffKillGap + 3*time.Second):
		t.Fatal("취소 후 끝나지 않음")
	}
	if !o.Killed || !strings.Contains(o.Stdout, "start") {
		t.Errorf("취소 결과: %+v", o)
	}
	b, _ := os.ReadFile(pidf)
	pid := strings.TrimSpace(string(b))
	waitFor(t, "손자 프로세스 종료", func() bool { _, err := os.Stat("/proc/" + pid); return err != nil })
	o = realChangeExec(changeCmd{Name: filepath.Join(dir, "없음")}, make(chan struct{}), nil)
	if o.StartErr == nil {
		t.Error("없는 실행 파일은 StartErr")
	}
}

// ---- 실제 엔진 + 가짜 gossh/ssh (AS_CHANGE_ENGINES=<ip-change-engine·ldap-config-engine 디렉터리> 일 때만) ----

const chgStubGossh = `#!/bin/bash
# 가짜 gossh: 엔진이 보낸 원격 명령을 풀어 종류를 알아내고 호스트별 결과 줄을 낸다 (원격 실행 없음)
w=""; cmd=""; prev=""
for a in "$@"; do [ "$prev" = -w ] && w=$a; prev=$a; cmd=$a; done
printf '%s|%s\n' "${FAKE_VIA:-os8}" "$*" | cut -c1-200 >> "$FAKE_LOG"
inner=$(printf '%s' "$cmd" | sed -e 's/^echo //' -e 's/ | tr -d .*$//' | tr -d ' ' | base64 -d 2>/dev/null)
script=$(printf '%s' "$inner" | sed -n 's/^umask 077; echo \([A-Za-z0-9+/=]*\) | base64 -d.*/\1/p' | base64 -d 2>/dev/null)
down=" $FAKE_DOWN "; [ "$FAKE_VIA" = os6 ] && down=" $FAKE_DOWN6 "
while read -r h; do
  [ -z "$h" ] && continue
  case "$down" in *" $h "*) echo "$h: ERROR: 접속 실패" >&2; continue ;; esac
  case "$inner$script" in
  *TOP_STAMP*) echo "$h: RESULT|OK|$h|ifcfg-eth0|20261012000000|10.0.0.9" ;;
  *ip_change_apply.sh*) echo "$h: RESULT|OK|$h|10.0.0.9|10.9.9.9|10.9.9.1" ;;
  *"MODE='latest'"*)
    # 원복 스크립트에 NO_RESTART=1 이 없으면(= 서비스 재시작) FAIL 로 보고 → 테스트 실패
    case "$script" in
    *"NO_RESTART=1"*) echo "$h: RESULT|$h|OK|20261012000000|restored:/etc/openldap/ldap.conf" ;;
    *) echo "$h: RESULT|$h|FAIL|no-restart-missing" ;;
    esac ;;
  *ldap_apply.sh*)
    if [ "$h" = "$FAKE_FAIL" ]; then
      echo "$h: APPLY|FAIL|도메인 섹션 없음"; echo "$h: DIFF|+BINDPW $FAKE_SECRET"; echo "$h: RESULT|$h|FAIL|alpha|a1"
    else
      echo "$h: APPLY|CHANGED|/etc/openldap/ldap.conf"; echo "$h: RESULT|$h|OK|alpha|a1|changed:/etc/openldap/ldap.conf"
    fi ;;
  *) echo "$h: 알 수 없는 명령" ;;
  esac
done < "$w"
`

const chgStubSSH = `#!/bin/bash
[ "$1" = -o ] && [ "$2" = BatchMode=yes ] || exit 9
export FAKE_VIA=os6
exec bash -c "$4"
`

func TestChangeRealEngines(t *testing.T) {
	eng := os.Getenv("AS_CHANGE_ENGINES")
	if eng == "" {
		t.Skip("AS_CHANGE_ENGINES 미설정 (실제 엔진 통합 확인은 S6 에서 본격적으로)")
	}
	needBash(t)
	r := newChgRig(t)
	bin := filepath.Join(r.dir, "bin")
	os.MkdirAll(bin, 0755)
	os.WriteFile(filepath.Join(bin, "gossh"), []byte(chgStubGossh), 0755)
	os.WriteFile(filepath.Join(bin, "ssh"), []byte(chgStubSSH), 0755)
	os.WriteFile(filepath.Join(r.env.ConfDir, "assets.txt"), []byte("h1\ta1\nh2\ta1\nh3\ta1\n"), 0644)
	logf := filepath.Join(r.dir, "gossh.log")
	t.Setenv("PATH", bin+":"+os.Getenv("PATH"))
	t.Setenv("FAKE_LOG", logf)
	t.Setenv("FAKE_DOWN", "h2")
	t.Setenv("FAKE_FAIL", "h3")
	t.Setenv("FAKE_SECRET", chgSecret)
	r.env.Exec = realChangeExec
	r.env.IPEngine, r.env.LDAPEngine = filepath.Join(eng, "ip-change-engine"), filepath.Join(eng, "ldap-config-engine")
	r.env.Gossh, r.env.OS6Gossh = filepath.Join(bin, "gossh"), filepath.Join(bin, "gossh")
	before, _ := filepath.Glob(filepath.Join(os.TempDir(), "as_change_*"))

	c := chgRun(t, r, kindBoth, "user1", "1", "h1 10.9.9.9\nh2 10.9.9.8\nh3 10.9.9.7\n")
	res := chgRes(c)
	if h := res["h1"]; h.IPResult != rsOK || h.NewIP != "10.9.9.9" || h.OldIP != "10.0.0.9" || h.LDAPResult != rsOK || h.Route != "os8" || !h.ApplPending {
		t.Errorf("h1: %+v", h)
	}
	if h := res["h2"]; h.IPResult != rsOK || h.LDAPResult != rsOK || h.Route != "os6" {
		t.Errorf("h2 (os8 접속 실패 → os6): %+v", h)
	}
	if h := res["h3"]; h.IPResult != rsOK || h.LDAPResult != rsFail || !strings.Contains(h.Detail, "도메인 섹션 없음") {
		t.Errorf("h3: %+v", h)
	}
	// LDAP 원복 (-no-restart 가 실제 엔진에 받아들여지고, 원격 스크립트에 NO_RESTART=1 이 들어감)
	c = newChangeState(r.env, kindRollback)
	chgKeys(c, "2user1\n\x04yes\n")
	chgWait(t, c)
	for _, h := range c.results {
		if h.LDAPResult != rsOK {
			t.Errorf("LDAP 원복 %s: %+v", h.Host, h)
		}
	}
	lg, _ := os.ReadFile(logf)
	if strings.Contains(string(lg), " -p ") {
		t.Errorf("gossh 에 -p 가 전달됨:\n%s", lg)
	}
	hay := []string{string(lg), renderChange(c, 100, 0, false)}
	filepath.Walk(r.env.StateDir, func(p string, fi os.FileInfo, err error) error {
		if err == nil && !fi.IsDir() {
			b, _ := os.ReadFile(p)
			hay = append(hay, string(b))
		}
		return nil
	})
	for _, h := range hay {
		if strings.Contains(h, chgSecret) {
			t.Errorf("비밀값 노출:\n%s", h)
		}
	}
	after, _ := filepath.Glob(filepath.Join(os.TempDir(), "as_change_*"))
	if len(after) > len(before) {
		t.Errorf("임시 디렉터리가 남음: %v", after)
	}
}
