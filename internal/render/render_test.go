package render

import (
	"os"
	"os/exec"
	"path/filepath"
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
	if !strings.Contains(script, "hostname1 3.3.3.3\n") {
		t.Errorf("host map missing hostname1 line:\n%s", script)
	}
	if !strings.Contains(script, "hostname2 2.2.2.2\n") {
		t.Errorf("host map missing hostname2 line:\n%s", script)
	}
	if strings.Contains(script, hostMapPlaceholder) {
		t.Errorf("placeholder was not substituted")
	}
	if !strings.Contains(script, "NETWORK_SCRIPTS_DIR='/etc/sysconfig/network-scripts'") {
		t.Errorf("NETWORK_SCRIPTS_DIR not rendered:\n%s", script)
	}
}

// bash 가 있는 환경(리눅스 빌드 서버)에서만 실제 스크립트를 실행해 왕복 검증합니다.
// hostname/getent 는 PATH 에 가짜 실행파일을 앞세워 위조합니다(ldap_setting 의
// test_all.sh 와 같은 기법).
func TestApplyScriptEndToEnd(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash 가 없는 환경이라 건너뜁니다")
	}

	work := t.TempDir()
	fixtureDir := filepath.Join(work, "network-scripts")
	if err := os.MkdirAll(fixtureDir, 0o755); err != nil {
		t.Fatal(err)
	}
	ifcfg := filepath.Join(fixtureDir, "ifcfg-eth0")
	if err := os.WriteFile(ifcfg, []byte("DEVICE=eth0\nBOOTPROTO=none\nIPADDR=192.168.1.50\nGATEWAY=192.168.1.1\nONBOOT=yes\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	fakebin := filepath.Join(work, "fakebin")
	if err := os.MkdirAll(fakebin, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFake(t, filepath.Join(fakebin, "hostname"), "#!/bin/sh\necho testnode\n")
	writeFake(t, filepath.Join(fakebin, "getent"), "#!/bin/sh\necho '192.168.1.50 testnode'\n")

	entries := []target.Entry{{Host: "testnode", NewIP: "192.168.1.99"}}
	cfg := &config.Config{NetworkScriptsDir: fixtureDir}
	script, err := ApplyScript(entries, cfg)
	if err != nil {
		t.Fatal(err)
	}

	scriptPath := filepath.Join(work, "apply.sh")
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command("bash", scriptPath)
	cmd.Env = append(os.Environ(), "PATH="+fakebin+":"+os.Getenv("PATH"))
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("script failed: %v\noutput:\n%s", err, out)
	}

	got := strings.TrimSpace(string(out))
	want := "RESULT|OK|testnode|192.168.1.50|192.168.1.99|192.168.1.1"
	if got != want {
		t.Fatalf("output = %q, want %q", got, want)
	}

	updated, err := os.ReadFile(ifcfg)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(updated), "IPADDR=192.168.1.99\n") {
		t.Errorf("IPADDR not updated:\n%s", updated)
	}
	if !strings.Contains(string(updated), "GATEWAY=192.168.1.1\n") {
		t.Errorf("GATEWAY not updated:\n%s", updated)
	}

	matches, _ := filepath.Glob(ifcfg + ".bak.*")
	if len(matches) != 1 {
		t.Errorf("want exactly 1 backup file, got %d: %v", len(matches), matches)
	} else {
		backup, err := os.ReadFile(matches[0])
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(backup), "IPADDR=192.168.1.50\n") {
			t.Errorf("backup does not contain original IPADDR:\n%s", backup)
		}
	}
}

// getent hosts 가 IPv6 을 IPv4 보다 먼저 돌려주는 노드에서도 IPv4 만 골라 쓰는지 확인합니다.
func TestApplyScriptEndToEndSkipsIPv6(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash 가 없는 환경이라 건너뜁니다")
	}

	work := t.TempDir()
	fixtureDir := filepath.Join(work, "network-scripts")
	if err := os.MkdirAll(fixtureDir, 0o755); err != nil {
		t.Fatal(err)
	}
	ifcfg := filepath.Join(fixtureDir, "ifcfg-eth0")
	if err := os.WriteFile(ifcfg, []byte("DEVICE=eth0\nBOOTPROTO=none\nIPADDR=192.168.1.50\nGATEWAY=192.168.1.1\nONBOOT=yes\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	fakebin := filepath.Join(work, "fakebin")
	if err := os.MkdirAll(fakebin, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFake(t, filepath.Join(fakebin, "hostname"), "#!/bin/sh\necho testnode\n")
	// 실제 랩에서 관찰된 상황 재현: getent hosts 가 IPv6(fe80::...) 을 먼저 찍고
	// IPv4 는 그 뒤에 옵니다.
	writeFake(t, filepath.Join(fakebin, "getent"), "#!/bin/sh\nprintf 'fe80::20c:29ff:fe3f:8ac1 testnode\\n192.168.1.50 testnode\\n'\n")

	entries := []target.Entry{{Host: "testnode", NewIP: "192.168.1.99"}}
	cfg := &config.Config{NetworkScriptsDir: fixtureDir}
	script, err := ApplyScript(entries, cfg)
	if err != nil {
		t.Fatal(err)
	}

	scriptPath := filepath.Join(work, "apply.sh")
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command("bash", scriptPath)
	cmd.Env = append(os.Environ(), "PATH="+fakebin+":"+os.Getenv("PATH"))
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("script failed: %v\noutput:\n%s", err, out)
	}

	got := strings.TrimSpace(string(out))
	want := "RESULT|OK|testnode|192.168.1.50|192.168.1.99|192.168.1.1"
	if got != want {
		t.Fatalf("output = %q, want %q (IPv6 을 걸러내지 못했을 가능성)", got, want)
	}
}

func TestApplyScriptEndToEndUnknownHost(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash 가 없는 환경이라 건너뜁니다")
	}

	work := t.TempDir()
	fakebin := filepath.Join(work, "fakebin")
	if err := os.MkdirAll(fakebin, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFake(t, filepath.Join(fakebin, "hostname"), "#!/bin/sh\necho othernode\n")

	entries := []target.Entry{{Host: "testnode", NewIP: "192.168.1.99"}}
	cfg := &config.Config{NetworkScriptsDir: work}
	script, err := ApplyScript(entries, cfg)
	if err != nil {
		t.Fatal(err)
	}

	scriptPath := filepath.Join(work, "apply.sh")
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command("bash", scriptPath)
	cmd.Env = append(os.Environ(), "PATH="+fakebin+":"+os.Getenv("PATH"))
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("want non-zero exit for unknown host, output:\n%s", out)
	}
	got := strings.TrimSpace(string(out))
	if !strings.HasPrefix(got, "RESULT|FAIL|othernode|") {
		t.Fatalf("output = %q, want FAIL for unmapped host", got)
	}
}

func writeFake(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}
}
