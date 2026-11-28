package render

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"

	"ip-change/internal/config"
	"ip-change/internal/target"
)

func TestApplyScriptEmbedsHostMap(t *testing.T) {
	entries := []target.Entry{
		{Host: "hostname1", NewIP: "3.3.3.3"},
		{Host: "hostname2", NewIP: "2.2.2.2"},
	}
	cfg := &config.Config{NetworkScriptsDir: "/etc/sysconfig/network-scripts"}

	script, err := ApplyScript(entries, cfg)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"hostname1 3.3.3.3\n", "hostname2 2.2.2.2\n",
		"NETWORK_SCRIPTS_DIR='/etc/sysconfig/network-scripts'", "FORCED_DIR=''", "CHECK_ONLY='0'", "BOND_MODE='active-backup'"} {
		if !strings.Contains(script, want) {
			t.Errorf("스크립트에 %q 가 없습니다", want)
		}
	}
	if strings.Contains(script, hostMapPlaceholder) {
		t.Errorf("placeholder was not substituted")
	}
	if !strings.HasPrefix(script, "#!/bin/bash\n") {
		t.Errorf("첫 줄이 #!/bin/bash 가 아닙니다")
	}
}

func TestApplyScriptOptionsAndErrors(t *testing.T) {
	entries := []target.Entry{{Host: "h", NewIP: "1.2.3.4"}}
	cfg := &config.Config{NetworkScriptsDir: "/etc/sysconfig/network-scripts"}

	s, err := ApplyScriptWithOptions(entries, cfg, ApplyOptions{CheckOnly: true, BondMode: "LACP"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(s, "CHECK_ONLY='1'") || !strings.Contains(s, "BOND_MODE='802.3ad'") {
		t.Errorf("옵션이 머리글에 들어가지 않았습니다")
	}
	if _, err := ApplyScriptWithOptions(entries, cfg, ApplyOptions{BondMode: "round-robin"}); err == nil {
		t.Errorf("알 수 없는 bond 모드를 받아들였습니다")
	}
	if _, err := ApplyScript(nil, cfg); err == nil {
		t.Errorf("대상이 없는데 오류가 아닙니다")
	}
}

func TestNormalizeBondMode(t *testing.T) {
	for in, want := range map[string]string{"": "active-backup", "active-backup": "active-backup", " Active-Backup ": "active-backup",
		"lacp": "802.3ad", "LACP": "802.3ad", "802.3ad": "802.3ad"} {
		if got, err := NormalizeBondMode(in); err != nil || got != want {
			t.Errorf("NormalizeBondMode(%q) = %q, %v; 기대 %q", in, got, err, want)
		}
	}
	if _, err := NormalizeBondMode("balance-rr"); err == nil {
		t.Errorf("balance-rr 는 오류여야 합니다")
	}
}

func TestForcedDir(t *testing.T) {
	cases := map[string]string{
		"":                                    "",
		config.DefaultNetworkScriptsDir:       "",
		config.DefaultNetworkScriptsDir + "/": "",
		"/etc/other":                          "/etc/other",
	}
	for in, want := range cases {
		if got := ForcedDir(&config.Config{NetworkScriptsDir: in}); got != want {
			t.Errorf("ForcedDir(%q) = %q, 기대 %q", in, got, want)
		}
	}
	s, _ := ApplyScript([]target.Entry{{Host: "h", NewIP: "1.2.3.4"}}, &config.Config{NetworkScriptsDir: "/etc/other"})
	if !strings.Contains(s, "FORCED_DIR='/etc/other'") {
		t.Errorf("강제 경로가 머리글에 없습니다")
	}
}

func TestRollbackScriptHeader(t *testing.T) {
	s, err := RollbackScript(&config.Config{NetworkScriptsDir: "/etc/x"}, "20260101000000")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"ROLLBACK_TO='20260101000000'", "NETWORK_SCRIPTS_DIR='/etc/x'"} {
		if !strings.Contains(s, want) {
			t.Errorf("롤백 스크립트에 %q 가 없습니다", want)
		}
	}
}

func TestFixGatewayScriptValidation(t *testing.T) {
	ok := GatewayFix{Host: "h", OldGW: "1.1.1.1", NewGW: "2.2.2.1", Files: []string{"/etc/sysconfig/network"}, Stamp: "20260101000000"}
	s, err := FixGatewayScript([]GatewayFix{ok})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(s, "h 1.1.1.1 2.2.2.1 /etc/sysconfig/network 20260101000000\n") || strings.Contains(s, gwMapPlaceholder) {
		t.Errorf("표가 올바르게 채워지지 않았습니다")
	}
	bad := []GatewayFix{}
	for _, mut := range []func(g *GatewayFix){
		func(g *GatewayFix) { g.Host = "" },
		func(g *GatewayFix) { g.OldGW = "" },
		func(g *GatewayFix) { g.NewGW = "" },
		func(g *GatewayFix) { g.Files = nil },
		func(g *GatewayFix) { g.Stamp = "" },
		func(g *GatewayFix) { g.Stamp = "2026-01-01" },
		func(g *GatewayFix) { g.Files = []string{"/etc/a b"} },
		func(g *GatewayFix) { g.Host = "h\nx" },
	} {
		g := ok
		mut(&g)
		bad = append(bad, g)
	}
	for _, g := range bad {
		if _, err := FixGatewayScript([]GatewayFix{g}); err == nil {
			t.Errorf("잘못된 항목을 받아들였습니다: %+v", g)
		}
	}
	if _, err := FixGatewayScript(nil); err == nil {
		t.Errorf("항목이 없는데 오류가 아닙니다")
	}
}

// 내장 스크립트에는 CR 이 없어야 합니다 (Windows 에서 CRLF 로 저장되면 노드 bash 가 깨집니다).
func TestEmbeddedScriptsHaveNoCR(t *testing.T) {
	for name, body := range map[string]string{"apply": applyBody, "rollback": rollbackBody, "fixgw": fixgwBody} {
		if strings.Contains(body, "\r") {
			t.Errorf("%s 스크립트에 CR 이 있습니다 (LF 로 저장해야 합니다)", name)
		}
	}
}

// RHEL 6 ~ 10 의 bash/awk/sed 에서 돌아야 하므로 GNU 확장이나 gawk 전용 기능을 쓰지 않는지 훑어봅니다.
func TestEmbeddedScriptsAvoidGNUOnly(t *testing.T) {
	bad := regexp.MustCompile("\\b(gensub|strftime|systime|asorti?|PROCINFO|IGNORECASE|FPAT|BEGINFILE|ENDFILE)\\b|sed\\s+-i|grep\\s+-[a-zA-Z]*P|readlink\\s+-f|date\\s+-d|sort\\s+-V|mktemp\\s+-p|\\$\\{[A-Za-z_]+(,,|\\^\\^)")
	for name, body := range map[string]string{"apply": applyBody, "rollback": rollbackBody, "fixgw": fixgwBody} {
		for i, ln := range strings.Split(body, "\n") {
			if strings.HasPrefix(strings.TrimSpace(ln), "#") {
				continue
			}
			if m := bad.FindString(ln); m != "" {
				t.Errorf("%s:%d GNU 전용 구문 %q: %s", name, i+1, m, ln)
			}
		}
	}
}

// 세 스크립트를 실제로 렌더링해 bash -n 으로 문법만 검사합니다 (실행하지 않습니다).
func TestRenderedScriptsPassBashSyntaxCheck(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("리눅스에서만 합니다")
	}
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash 가 없는 환경이라 건너뜁니다")
	}
	cfg := &config.Config{NetworkScriptsDir: "/etc/sysconfig/network-scripts"}
	entries := []target.Entry{{Host: "h", NewIP: "10.0.0.5"}}
	apply, _ := ApplyScript(entries, cfg)
	check, _ := ApplyScriptWithOptions(entries, cfg, ApplyOptions{CheckOnly: true, BondMode: "lacp"})
	rb, _ := RollbackScript(cfg, "")
	rb2, _ := RollbackScript(cfg, "20260101000000")
	fix, _ := FixGatewayScript([]GatewayFix{{Host: "h", OldGW: "1.1.1.1", NewGW: "10.0.0.1", Files: []string{"/etc/sysconfig/network"}, Stamp: "20260101000000"}})
	for name, body := range map[string]string{"apply": apply, "apply-check": check, "rollback": rb, "rollback-to": rb2, "fixgw": fix} {
		p := filepath.Join(t.TempDir(), name+".sh")
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		if out, err := exec.Command("bash", "-n", p).CombinedOutput(); err != nil {
			t.Errorf("%s: bash -n 실패: %v\n%s", name, err, out)
		}
	}
}

// 자리표시자는 본체에 딱 한 번, 줄 전체로만 있어야 합니다 (주석에 적으면 Replace 가 주석을 먼저 채웁니다).
func TestPlaceholdersAppearOnceAsOwnLine(t *testing.T) {
	for name, c := range map[string]struct{ body, ph string }{
		"apply": {applyBody, hostMapPlaceholder}, "fixgw": {fixgwBody, gwMapPlaceholder},
	} {
		if n := strings.Count(c.body, c.ph); n != 1 {
			t.Errorf("%s: %s 가 %d 번 있습니다 (1번이어야 합니다)", name, c.ph, n)
		}
		if !strings.Contains(c.body, "\n"+c.ph+"\n") {
			t.Errorf("%s: %s 가 한 줄 전체가 아닙니다", name, c.ph)
		}
	}
}
