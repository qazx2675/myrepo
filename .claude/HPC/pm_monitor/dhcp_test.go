package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// stubSSH 는 스텁 ssh 를 임시 폴더에 복사하고 sshBin 을 그쪽으로 바꾼다(테스트가 끝나면 되돌린다).
func stubSSH(t *testing.T) {
	t.Helper()
	src, err := os.ReadFile(filepath.Join("test", "stub_ssh.sh"))
	if err != nil {
		t.Fatal(err)
	}
	stub := filepath.Join(t.TempDir(), "stub_ssh.sh")
	if err := os.WriteFile(stub, src, 0o755); err != nil {
		t.Fatal(err)
	}
	old := sshBin
	sshBin = stub
	t.Cleanup(func() { sshBin = old })
}

func TestDHCPQueryStub(t *testing.T) {
	stubSSH(t)
	cfg := defaultConfig()
	cfg.DHCPServer = "dhcp01"
	got, err := DHCPQuery(cfg, []string{"svr1", "svr2", "svr3"})
	if err != nil {
		t.Fatal(err)
	}
	want := "NICE: 10\n" +
		"ARGS: -o ConnectTimeout=10 dhcp01 cat > /root/off_server_list_" + currentUser() +
		"; bash /root/a.sh; rm -f /root/off_server_list_" + currentUser() + "\n" +
		"STDIN:\nsvr1\nsvr2\nsvr3"
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestDHCPQueryUnsetAndEmpty(t *testing.T) {
	stubSSH(t)
	cfg := defaultConfig()
	if got, err := DHCPQuery(cfg, []string{"a"}); err != nil || got != "dhcp_server 미설정" {
		t.Errorf("미설정: %q %v", got, err)
	}
	cfg.DHCPServer = "dhcp01"
	if got, err := DHCPQuery(cfg, nil); err != nil || got != "ping X 서버가 없습니다." {
		t.Errorf("빈 목록: %q %v", got, err)
	}
}

func TestDHCPQueryFail(t *testing.T) {
	stubSSH(t)
	t.Setenv("STUB_SSH_FAIL", "1")
	cfg := defaultConfig()
	cfg.DHCPServer = "dhcp01"
	got, err := DHCPQuery(cfg, []string{"a"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "ssh: connect to host failed") || !strings.HasSuffix(got, "(ssh 종료 코드 255)") {
		t.Errorf("실패 출력: %q", got)
	}
}

func TestDHCPReportUsesPingXFromSnapshot(t *testing.T) {
	stubSSH(t)
	cfg := testCfg()
	cfg.DHCPServer = "dhcp01"
	col, err := NewCollector(cfg, []string{"b", "a", "c", "d"}, filepath.Join(t.TempDir(), "s.json"), defaultDeps())
	if err != nil {
		t.Fatal(err)
	}
	col.mu.Lock()
	col.recs["a"] = Record{Name: "a", Status: StatusPingX}
	col.recs["b"] = Record{Name: "b", Status: StatusPingX}
	col.recs["c"] = Record{Name: "c", Status: StatusInstalling}
	col.mu.Unlock()
	col.publish(false)
	out := col.DHCPReport()
	if !strings.HasSuffix(out, "STDIN:\na\nb") {
		t.Errorf("ping X 목록이 정렬돼 전달되지 않음: %q", out)
	}
	if col.ReportBusy('d') != "DHCP 조회중..." {
		t.Errorf("busy = %q", col.ReportBusy('d'))
	}
}
