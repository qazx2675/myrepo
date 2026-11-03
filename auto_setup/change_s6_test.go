package main

// change_s6_test.go - S6: 변경 메뉴(m)를 "실제 엔진 + 가짜 gossh(실제 노드 스크립트를 fixture 루트에서 실행)" 로 끝까지 돌려 본다.
// 환경변수가 없으면 스킵된다 (test/run_change_os.sh, test/run_change_lab.sh 가 설정해서 부른다).
//
//	TestChangeS6World  AS_CHANGE_ENGINES=<엔진 디렉터리> AS_S6_WORLD=<nodes/ conf/ 가 있는 디렉터리> AS_S6_GOSSH=<test/fake_gossh.sh 사본>
//	                   OS 7/8/9 혼합 노드 9대에 IP+LDAP 변경 → auto.appl 원복 → 원복을 흘리고, 결과·fixture·기록·로그를 점검한다.
//	TestChangeLabSelf  AS_LAB_HOST AS_LAB_CONF AS_LAB_IP_ENGINE AS_LAB_LDAP_ENGINE AS_LAB_GOSSH AS_LAB_NET AS_LAB_ROOT
//	                   랩 서버 자기 자신에 진짜 gossh 로 접속 — IP 는 network_scripts_dir=<fixture>, LDAP 은 -root <fixture> 만 대상.
//
// 점검 결과는 " S6 PASS <설명>" / " S6 FAIL <설명>" 줄로 남긴다 (스크립트가 집계).

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

type s6T struct{ t *testing.T }

func (s s6T) chk(ok bool, format string, a ...interface{}) {
	s.t.Helper()
	msg := fmt.Sprintf(format, a...)
	if ok {
		s.t.Logf("S6 PASS %s", msg)
	} else {
		s.t.Errorf("S6 FAIL %s", msg)
	}
}

// s6Screens: 흐름 중 그려진 화면들 (비밀값 노출 점검용)
type s6Screens struct {
	mu sync.Mutex
	v  []string
}

func (s *s6Screens) add(c *changeState) {
	x := renderChange(c, 100, 0, false)
	s.mu.Lock()
	s.v = append(s.v, x)
	s.mu.Unlock()
}

func s6Wait(t *testing.T, c *changeState, sc *s6Screens) {
	t.Helper()
	deadline := time.Now().Add(90 * time.Second)
	for time.Now().Before(deadline) {
		c.tick()
		sc.add(c)
		if c.job == nil {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("변경 작업이 90초 안에 끝나지 않음")
}

// s6Run: user → (infra) → 입력(자동 채움이면 빈 입력) → (dry-run) → 미리보기 → yes → 결과
func s6Run(t *testing.T, c *changeState, sc *s6Screens, user, infraNo, input string) {
	t.Helper()
	chgKeys(c, user+"\n")
	if infraNo != "" {
		chgKeys(c, infraNo+"\n")
	}
	if input != "" {
		chgKeys(c, input)
	}
	chgKeys(c, "\x04")
	if c.step == csDry {
		s6Wait(t, c, sc)
	}
	if c.step != csPreview {
		t.Fatalf("미리보기가 아님: step=%d msg=%q", c.step, c.msg)
	}
	sc.add(c)
	chgKeys(c, "yes\n")
	s6Wait(t, c, sc)
	if c.step != csResult {
		t.Fatalf("결과 화면이 아님: step=%d", c.step)
	}
	sc.add(c)
}

func s6Copy(t *testing.T, src, dst string) {
	t.Helper()
	if out, err := exec.Command("cp", "-a", src, dst).CombinedOutput(); err != nil {
		t.Fatalf("cp -a %s %s: %v %s", src, dst, err, out)
	}
}

func s6Read(p string) string {
	b, _ := os.ReadFile(p)
	return string(b)
}

func s6Has(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

func s6Glob(p string) bool {
	m, _ := filepath.Glob(p)
	return len(m) > 0
}

// s6DirsEqual: 두 디렉터리 트리의 모든 파일이 같음 (diff -rq)
func s6DirsEqual(a, b string) bool {
	return exec.Command("diff", "-rq", a, b).Run() == nil
}

func TestChangeS6World(t *testing.T) {
	eng, world, gossh := os.Getenv("AS_CHANGE_ENGINES"), os.Getenv("AS_S6_WORLD"), os.Getenv("AS_S6_GOSSH")
	if eng == "" || world == "" || gossh == "" {
		t.Skip("AS_CHANGE_ENGINES / AS_S6_WORLD / AS_S6_GOSSH 미설정 (test/run_change_os.sh 가 설정)")
	}
	needBash(t)
	s := s6T{t}
	r := newChgRig(t)
	work := r.dir
	orig := filepath.Join(world, "nodes")
	nodes := filepath.Join(work, "nodes")
	conf := filepath.Join(work, "confw")
	logs := filepath.Join(work, "fake")
	s6Copy(t, orig, nodes)
	s6Copy(t, filepath.Join(world, "conf"), conf)
	bin := filepath.Join(work, "bin")
	if err := os.MkdirAll(bin, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bin, "ssh"), []byte(chgStubSSH), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+":"+os.Getenv("PATH"))
	t.Setenv("FAKE_ROOTS", nodes)
	t.Setenv("FAKE_LOGDIR", logs)
	t.Setenv("FAKE_UNGUARD", "1") // ROOT 보호를 끈다 — -no-restart 만이 재시작을 막고 있음을 본다
	kv, err := readFlatConf(filepath.Join(conf, "ldap_config.conf"))
	if err != nil {
		t.Fatal(err)
	}
	secrets := []string{kv["infra.alpha.bindpw"], kv["infra.beta.bindpw"]}
	if secrets[0] == "" || secrets[1] == "" || !strings.HasPrefix(secrets[0], "DEMO_BINDPW_") {
		t.Fatalf("시험 bindpw 를 conf 에서 읽지 못함: %v", secrets)
	}

	var mu sync.Mutex
	var calls []changeCmd
	r.env.Exec = func(c changeCmd, cancel <-chan struct{}, onLine func(string)) changeOut {
		mu.Lock()
		calls = append(calls, c)
		mu.Unlock()
		return realChangeExec(c, cancel, onLine)
	}
	r.env.ConfDir = conf
	r.env.IPEngine, r.env.LDAPEngine = filepath.Join(eng, "ip-change-engine"), filepath.Join(eng, "ldap-config-engine")
	r.env.Gossh, r.env.OS6Gossh, r.env.OS6Mgmt = gossh, gossh, "os6mgmt"
	r.env.Tracked["rh8-n01"] = "20261012-0900-alice/infra_inventory-test.yml" // 진행 중 job 의 호스트
	before, _ := filepath.Glob(filepath.Join(os.TempDir(), "as_change_*"))
	sc := &s6Screens{}

	hosts := []string{"rh7-n01", "rh7-n02", "rh8-n01", "rh8-n02", "rh9-n01", "rh9-n02", "s4rh8-n01", "rh8-dead", "rh8-noasset"}
	newIP := func(h string) string {
		return strings.Replace(strings.TrimSpace(s6Read(filepath.Join(orig, h, ".fake", "ips"))), "10.20.", "10.30.", 1)
	}
	file := func(h, rel string) string { return s6Read(filepath.Join(nodes, h, rel)) }
	same := func(h, rel string) bool {
		return s6Read(filepath.Join(nodes, h, rel)) == s6Read(filepath.Join(orig, h, rel))
	}
	bak := func(h, rel string) bool { return s6Glob(filepath.Join(nodes, h, rel) + ".bak.*") }
	netRel := "etc/sysconfig/network-scripts/ifcfg-eth0"

	// ---- 0. LDAP dry-run 미리보기만 (kindLDAP): 아무것도 바뀌지 않아야 한다 ----
	c := newChangeState(r.env, kindLDAP)
	chgKeys(c, "user1\n1\n"+strings.Join(hosts, "\n")+"\n\x04")
	s6Wait(t, c, sc)
	s.chk(c.step == csPreview, "LDAP dry-run 이 끝나 미리보기 화면")
	for _, h := range []string{"rh7-n01", "rh7-n02", "rh8-n01", "rh9-n01", "rh9-n02", "s4rh8-n01"} {
		d := c.dry[h]
		s.chk(d != nil && d.LDAPResult == rsChanged, "dry-run [%s] CHANGED (바뀔 예정): %+v", h, d)
	}
	s.chk(c.dry["rh7-n02"] != nil && c.dry["rh7-n02"].Route == "os6" && c.dryOS6["rh7-n02"], "dry-run [rh7-n02] os8 무응답 → os6 경유로 조회")
	s.chk(c.dry["rh8-dead"] != nil && c.dry["rh8-dead"].LDAPResult == rsUnreach, "dry-run [rh8-dead] 응답 없음")
	s.chk(c.dry["rh8-noasset"] != nil && c.dry["rh8-noasset"].LDAPResult == rsSkipAsset, "dry-run [rh8-noasset] 자산현황 없음으로 건너뜀")
	s.chk(strings.Contains(renderChange(c, 100, 0, false), "진행 중 job"), "미리보기에 진행 중 job 경고 (rh8-n01)")
	s.chk(s6DirsEqual(orig, nodes), "dry-run 후 모든 노드 fixture 가 그대로")
	chgKeys(c, "\x1b")

	// ---- 1. IP+LDAP 변경 (kindBoth) ----
	var in strings.Builder
	for _, h := range hosts {
		fmt.Fprintf(&in, "%s %s\n", h, newIP(h))
	}
	c = newChangeState(r.env, kindBoth)
	s6Run(t, c, sc, "user1", "1", in.String())
	res := chgRes(c)
	want := map[string][3]string{ // IP 결과, LDAP 결과, 경로
		"rh7-n01":     {rsOK, rsOK, "os8"},
		"rh7-n02":     {rsOK, rsOK, "os6"},
		"rh8-n01":     {rsOK, rsOK, "os8"},
		"rh8-n02":     {rsFail, rsSkipIP, "os8"},
		"rh9-n01":     {rsOK, rsOK, "os8"},
		"rh9-n02":     {rsOK, rsOK, "os8"},
		"s4rh8-n01":   {rsOK, rsOK, "os8"},
		"rh8-dead":    {rsUnreach, rsSkipIP, "os6"},
		"rh8-noasset": {rsOK, rsSkipAsset, "os8"},
	}
	for _, h := range hosts {
		x, w := res[h], want[h]
		s.chk(x != nil && x.IPResult == w[0] && x.LDAPResult == w[1] && x.Route == w[2],
			"IP+LDAP [%s] IP=%s LDAP=%s 경로=%s (실제: %s)", h, w[0], w[1], w[2], fmt.Sprintf("%v", x))
	}
	s.chk(res["rh8-dead"] != nil && strings.Contains(res["rh8-dead"].Detail, "os8·os6 경유 모두 응답 없음"), "[rh8-dead] 사유: os8·os6 경유 모두 응답 없음")
	s.chk(res["rh8-n02"] != nil && strings.Contains(res["rh8-n02"].Detail, "ifcfg"), "[rh8-n02] 사유에 ifcfg (ifcfg 없음)")
	for _, h := range []string{"rh7-n01", "rh7-n02", "rh8-n01", "rh9-n01", "rh9-n02", "s4rh8-n01"} {
		s.chk(res[h] != nil && res[h].ApplPending, "[%s] LDAP 적용 OK → auto.appl 대기 표시", h)
	}
	for _, h := range []string{"rh7-n01", "rh7-n02", "rh8-n01", "rh9-n01", "rh9-n02", "s4rh8-n01", "rh8-noasset"} {
		s.chk(strings.Contains(file(h, netRel), "IPADDR="+newIP(h)+"\n") && bak(h, netRel), "[%s] ifcfg-eth0 IPADDR=%s + 백업", h, newIP(h))
	}
	s.chk(same("rh8-n02", netRel) && !bak("rh8-n02", netRel), "[rh8-n02] ifcfg 변경 없음")
	s.chk(strings.Contains(file("rh7-n01", "etc/nslcd.conf"), "uri ldap://") && !strings.Contains(file("rh7-n01", "etc/nslcd.conf"), "old.example"),
		"RHEL7 [rh7-n01] nslcd.conf 갱신")
	s.chk(strings.Contains(file("rh7-n01", "etc/ntp.conf"), "server 10.20.1.10 iburst"), "RHEL7 [rh7-n01] ntp.conf 갱신")
	for _, h := range []string{"rh8-n01", "rh9-n01", "rh9-n02"} {
		s.chk(strings.Contains(file(h, "etc/sssd/sssd.conf"), "ldap_uri = ldap://10.10.1.2") && bak(h, "etc/sssd/sssd.conf"), "RHEL8/9 [%s] sssd.conf 갱신 + 백업", h)
		s.chk(strings.Contains(file(h, "etc/chrony.conf"), "server 10.20.1.10 iburst") && !strings.Contains(file(h, "etc/chrony.conf"), "pool"), "RHEL8/9 [%s] chrony.conf 갱신", h)
	}
	s.chk(strings.Contains(file("s4rh8-n01", "etc/nslcd.conf"), "uri ldap://10.10.1.20/") && strings.Contains(file("s4rh8-n01", "etc/ntp.conf"), "server 10.20.1.10 iburst"),
		"s4 접두 [s4rh8-n01] RHEL8 이어도 nslcd.conf·ntp.conf 갱신")
	s.chk(!strings.Contains(file("rh7-n01", "etc/openldap/ldap.conf"), "OLD_NODE_PW_DEMO") && strings.Contains(file("rh7-n01", "etc/openldap/ldap.conf"), "BINDPW "+secrets[0]),
		"노드 ldap.conf 에는 새 bindpw 가 적용됨 (노드 파일은 정상 — 화면·기록에만 없어야 함)")

	// ---- 2. auto.appl 원복 (kindApplRestore): 대상은 기록으로 자동 채워짐 ----
	c = newChangeState(r.env, kindApplRestore)
	s6Run(t, c, sc, "user1", "", "")
	res = chgRes(c)
	for _, h := range []string{"rh7-n01", "rh7-n02", "rh8-n01", "rh9-n01", "rh9-n02", "s4rh8-n01"} {
		s.chk(res[h] != nil && res[h].ApplResult == rsOK, "auto.appl 원복 [%s] OK (경로 %v)", h, res[h] != nil && res[h].Route != "")
		s.chk(s6Has(filepath.Join(nodes, h, "etc/auto.appl")) && !s6Has(filepath.Join(nodes, h, "etc/auto.appl.bak")), "[%s] auto.appl 반영·대기 파일 소멸", h)
	}
	s.chk(res["rh7-n02"] != nil && res["rh7-n02"].Route == "os6", "auto.appl 원복 [rh7-n02] 처음부터 os6 경유 (기록의 경로)")
	pend, _ := changeAutofill(r.env.StateDir, "user1", "appl")
	s.chk(len(pend) == 0, "auto.appl 원복 후 기록의 대기 표시가 모두 해제됨: %v", pend)

	// ---- 3. IP+LDAP 원복 (kindRollback 3): -no-restart ----
	c = newChangeState(r.env, kindRollback)
	chgKeys(c, "3")
	s6Run(t, c, sc, "user1", "", "")
	res = chgRes(c)
	for _, h := range []string{"rh7-n01", "rh7-n02", "rh8-n01", "rh9-n01", "rh9-n02", "s4rh8-n01"} {
		s.chk(res[h] != nil && res[h].IPResult == rsOK && res[h].LDAPResult == rsOK, "원복 [%s] IP·LDAP OK (%v)", h, res[h])
	}
	s.chk(res["rh8-noasset"] != nil && res["rh8-noasset"].IPResult == rsOK && res["rh8-noasset"].LDAPResult != rsFail, "원복 [rh8-noasset] IP OK (LDAP 은 백업 없음)")
	bad := []string{}
	for _, h := range []string{"rh7-n01", "rh7-n02", "rh8-n01", "rh9-n01", "rh9-n02", "s4rh8-n01", "rh8-noasset"} {
		for _, rel := range []string{netRel, "etc/openldap/ldap.conf", "etc/autofs_ldap_auth.conf", "etc/autofs.conf", "etc/resolv.conf",
			"etc/auto.appl", "etc/nslcd.conf", "etc/ntp.conf", "etc/sssd/sssd.conf", "etc/chrony.conf"} {
			if s6Has(filepath.Join(orig, h, rel)) && !same(h, rel) {
				bad = append(bad, h+":"+rel)
			}
		}
	}
	s.chk(len(bad) == 0, "원복 후 IP·LDAP 파일이 모두 원본과 같음 %v", bad)

	// ---- 서비스 재시작·gossh 인자·비밀값·임시 파일 ----
	nodeLog := s6Read(filepath.Join(logs, "node.log"))
	s.chk(!strings.Contains(nodeLog, "APPLY|RESTARTED"), "노드 로그에 RESTARTED 0건 (원복 포함, ROOT 보호를 끈 상태)")
	s.chk(!s6Has(filepath.Join(logs, "systemctl.log")) || strings.TrimSpace(s6Read(filepath.Join(logs, "systemctl.log"))) == "",
		"systemctl·sss_cache·service·nmcli 호출 기록 0건")
	s.chk(strings.Contains(nodeLog, "SKIP-RESTART|") && strings.Contains(nodeLog, "(NO_RESTART)"), "원복이 서비스를 SKIP-RESTART (NO_RESTART) 로 건너뜀")
	nL, noRestartAll, os6Wrapper, bannedArg := 0, true, 0, ""
	mu.Lock()
	for _, cc := range calls {
		a := " " + strings.Join(cc.Args, " ") + " "
		if filepath.Base(cc.Name) == "ldap-config-engine" {
			nL++
			if !strings.Contains(a, " -no-restart ") {
				noRestartAll = false
			}
		}
		if strings.HasSuffix(argAfter(cc.Args, "-gossh"), "gossh_os6.sh") {
			os6Wrapper++
		}
		for _, b := range []string{" -p ", " -print-script ", " -monitor "} {
			if strings.Contains(a, b) {
				bannedArg = b
			}
		}
	}
	mu.Unlock()
	s.chk(nL > 0 && noRestartAll, "ldap-config-engine 호출 %d회 모두 -no-restart 포함", nL)
	s.chk(os6Wrapper > 0, "os6 경유 호출이 래퍼(gossh_os6.sh)로 이뤄짐 (%d회)", os6Wrapper)
	s.chk(bannedArg == "", "엔진 호출에 -p / -print-script / -monitor 없음 (%q)", bannedArg)
	gl := s6Read(filepath.Join(logs, "gossh.log"))
	s.chk(!strings.Contains(gl, "-p ***") && strings.Contains(gl, "kind=ip") && strings.Contains(gl, "kind=ldap"), "gossh 호출 기록에 -p 없음")

	hay := map[string]string{"gossh.log": gl}
	sc.mu.Lock()
	for i, v := range sc.v {
		hay[fmt.Sprintf("screen%d", i)] = v
	}
	sc.mu.Unlock()
	hay["auto_setup.log 요약"] = strings.Join(r.logs, "\n")
	for _, c := range calls {
		hay["엔진 인자 "+c.Name] = strings.Join(c.Args, " ")
	}
	_ = filepath.Walk(r.env.StateDir, func(p string, fi os.FileInfo, err error) error {
		if err == nil && !fi.IsDir() {
			hay[p] = s6Read(p)
		}
		return nil
	})
	leaked := ""
	for name, v := range hay {
		for _, sec := range secrets {
			if strings.Contains(v, sec) {
				leaked += " " + name
			}
		}
	}
	s.chk(leaked == "", "화면·기록(%d)·auto_setup 로그·엔진 인자·gossh 호출 기록 어디에도 bindpw 값이 없음 (노출:%s)", len(hay), leaked)
	recs, _ := filepath.Glob(filepath.Join(r.env.StateDir, "changes", "user1", "*.json"))
	s.chk(len(recs) >= 3, "user1 실행 기록 %d개 (both·appl·rollback-both)", len(recs))
	after, _ := filepath.Glob(filepath.Join(os.TempDir(), "as_change_*"))
	s.chk(len(after) <= len(before), "임시 디렉터리(as_change_*)가 남지 않음")
}

// TestChangeLabSelf: 랩 서버 자기 자신(.58)에 진짜 gossh 로 접속해 IP 변경·LDAP 변경·원복을 한 번씩.
// 대상은 fixture 뿐이다 — IP: ip_change.conf 의 network_scripts_dir(<fixture>), LDAP: 엔진 래퍼가 붙이는 -root <fixture>.
// 서비스 재시작이 없음은 래퍼가 -no-restart 없는 호출을 거부하는 것과, 호출한 쪽(run_change_lab.sh)의 서비스 시작 시각 비교로 본다.
func TestChangeLabSelf(t *testing.T) {
	host := os.Getenv("AS_LAB_HOST")
	if host == "" {
		t.Skip("AS_LAB_HOST 미설정 (test/run_change_lab.sh 가 설정)")
	}
	needBash(t)
	s := s6T{t}
	conf, ipEng, ldEng := os.Getenv("AS_LAB_CONF"), os.Getenv("AS_LAB_IP_ENGINE"), os.Getenv("AS_LAB_LDAP_ENGINE")
	gossh, net, root := os.Getenv("AS_LAB_GOSSH"), os.Getenv("AS_LAB_NET"), os.Getenv("AS_LAB_ROOT")
	if conf == "" || ipEng == "" || ldEng == "" || gossh == "" || net == "" || root == "" {
		t.Fatal("AS_LAB_* 환경변수가 모두 필요함")
	}
	if !strings.HasPrefix(net, "/tmp/") || !strings.HasPrefix(root, "/tmp/") {
		t.Fatalf("fixture 가 /tmp 아래가 아님: %s %s", net, root)
	}
	r := newChgRig(t)
	r.env.Exec = realChangeExec
	r.env.ConfDir = conf
	r.env.IPEngine, r.env.LDAPEngine = ipEng, ldEng
	r.env.Gossh, r.env.OS6Gossh, r.env.OS6Mgmt = gossh, "", "" // os6 경유 없음 (진짜 gossh 경로만)
	kv, _ := readFlatConf(filepath.Join(conf, "ldap_config.conf"))
	secret := kv["infra.alpha.bindpw"]
	s.chk(strings.HasPrefix(secret, "DEMO_BINDPW_"), "랩 시험 bindpw 가 DEMO_BINDPW_ 접두 값")
	sc := &s6Screens{}
	ifcfg := filepath.Join(net, "ifcfg-eth0")
	origIfcfg := s6Read(ifcfg)
	n58 := "10.77.0.58"

	// IP 변경 → 원복 (진짜 gossh)
	c := newChangeState(r.env, kindIP)
	s6Run(t, c, sc, "labuser", "", host+" "+n58+"\n")
	x := chgRes(c)[host]
	s.chk(x != nil && x.IPResult == rsOK && x.NewIP == n58 && x.Route == "os8", "IP 변경 [%s] OK → %s (진짜 gossh): %+v", host, n58, x)
	s.chk(strings.Contains(s6Read(ifcfg), "IPADDR="+n58) && strings.Contains(s6Read(ifcfg), "GATEWAY=10.77.0.1") && s6Glob(ifcfg+".bak.*"),
		"fixture ifcfg-eth0 이 IPADDR=%s GATEWAY=10.77.0.1 + 백업", n58)
	c = newChangeState(r.env, kindRollback)
	chgKeys(c, "1")
	s6Run(t, c, sc, "labuser", "", "")
	x = chgRes(c)[host]
	s.chk(x != nil && x.IPResult == rsOK, "IP 원복 [%s] OK: %+v", host, x)
	s.chk(s6Read(ifcfg) == origIfcfg, "fixture ifcfg-eth0 이 원본과 같음")

	// LDAP dry-run → 적용 → auto.appl 원복 → 원복
	files := []string{"etc/openldap/ldap.conf", "etc/autofs.conf", "etc/resolv.conf", "etc/sssd/sssd.conf", "etc/chrony.conf", "etc/auto.appl", "etc/autofs_ldap_auth.conf"}
	snap := map[string]string{}
	for _, f := range files {
		snap[f] = s6Read(filepath.Join(root, f))
	}
	c = newChangeState(r.env, kindLDAP)
	chgKeys(c, "labuser\n1\n"+host+"\n\x04")
	s6Wait(t, c, sc)
	d := c.dry[host]
	s.chk(c.step == csPreview && d != nil && d.LDAPResult == rsChanged, "LDAP dry-run [%s] CHANGED: %+v", host, d)
	same := true
	for _, f := range files {
		if s6Read(filepath.Join(root, f)) != snap[f] {
			same = false
		}
	}
	s.chk(same, "dry-run 후 fixture 가 그대로")
	chgKeys(c, "yes\n")
	s6Wait(t, c, sc)
	sc.add(c)
	x = chgRes(c)[host]
	s.chk(c.step == csResult && x != nil && x.LDAPResult == rsOK && x.ApplPending, "LDAP 적용 [%s] OK (auto.appl 대기): %+v", host, x)
	s.chk(strings.Contains(s6Read(filepath.Join(root, "etc/sssd/sssd.conf")), "ldap_uri = ldap://10.10.1.20/") &&
		strings.Contains(s6Read(filepath.Join(root, "etc/chrony.conf")), "server 10.20.1.10 iburst") &&
		s6Has(filepath.Join(root, "etc/auto.appl.bak")),
		"fixture 의 sssd.conf·chrony.conf 갱신 (Rocky 8 → sssd/chrony), auto.appl 은 .bak 대기")
	// auto.appl 원복(m → 5)은 여기서 하지 않는다: 진짜 gossh 로는 노드의 절대 경로 /root/auto_appl_restore.sh 를 실행하고 그 스크립트가
	// 절대 경로 /etc/auto.appl* 을 쓰므로 fixture 로 가둘 수 없다(.58 자신의 /etc 를 건드릴 수 있음). fixture 로 가둔 검증은 TestChangeS6World.
	c = newChangeState(r.env, kindRollback)
	chgKeys(c, "2")
	s6Run(t, c, sc, "labuser", "", "")
	x = chgRes(c)[host]
	s.chk(x != nil && x.LDAPResult == rsOK, "LDAP 원복 [%s] OK (-no-restart): %+v", host, x)
	bad := []string{}
	for _, f := range files {
		if s6Read(filepath.Join(root, f)) != snap[f] {
			bad = append(bad, f)
		}
	}
	s.chk(len(bad) == 0, "원복 후 fixture 파일이 원본과 같음 %v", bad)

	leaked := ""
	sc.mu.Lock()
	for _, v := range sc.v {
		if strings.Contains(v, secret) {
			leaked = "화면"
		}
	}
	sc.mu.Unlock()
	if strings.Contains(strings.Join(r.logs, "\n"), secret) {
		leaked += " 로그"
	}
	_ = filepath.Walk(r.env.StateDir, func(p string, fi os.FileInfo, err error) error {
		if err == nil && !fi.IsDir() && strings.Contains(s6Read(p), secret) {
			leaked += " " + p
		}
		return nil
	})
	s.chk(leaked == "", "화면·로그·기록에 bindpw 값 없음 (%s)", leaked)
}
