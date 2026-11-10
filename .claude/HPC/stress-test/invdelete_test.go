package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 가짜 ssh 가 받은 인자를 파일에 한 줄씩 남긴다.
func fakeDeleteSSH(t *testing.T, body string) string {
	t.Helper()
	rec := filepath.Join(t.TempDir(), "ssh_calls")
	writeScript(t, sshBin, `echo "$*" >> "`+rec+`"`+"\n"+body+"\n")
	return rec
}

func TestInventoryDeleteRuns(t *testing.T) {
	a, _, _ := newTestApp(t)
	a.user = "hong"
	a.cfg.InventoryDeleteHost = "delhost01"
	rec := fakeDeleteSSH(t, "exit 0")
	if err := a.inventoryDelete(); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(rec)
	if got, want := strings.TrimSpace(string(b)), "-o BatchMode=yes delhost01 bash /root/server/delhost_hong"; got != want {
		t.Errorf("ssh 인자 = %q, want %q", got, want)
	}
}

func TestInventoryDeleteEmptyHostSkips(t *testing.T) {
	a, _, _ := newTestApp(t)
	rec := fakeDeleteSSH(t, "exit 0")
	if err := a.inventoryDelete(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(rec); err == nil {
		t.Error("inventory_delete_host 가 비었는데 ssh 를 불렀습니다")
	}
}

func TestInventoryDeleteFail(t *testing.T) {
	a, _, _ := newTestApp(t)
	a.cfg.InventoryDeleteHost = "delhost01"
	fakeDeleteSSH(t, "echo no such file >&2; exit 3")
	err := a.inventoryDelete()
	if err == nil || !strings.Contains(err.Error(), "inventory_delete 실패") || !strings.Contains(err.Error(), "no such file") {
		t.Errorf("err = %v", err)
	}
}

func TestNeedRegister(t *testing.T) {
	a := &app{targets: []*target{{Host: "n1", Res: map[string]string{stageCPU: resOffline}}}}
	if a.needRegister() {
		t.Error("전부 접속불가인데 등록 대상이 있다고 했습니다")
	}
	a.targets = append(a.targets, &target{Host: "n2", Res: map[string]string{stageCPU: resNotRun}})
	if !a.needRegister() {
		t.Error("미실행 대상이 있는데 등록 대상이 없다고 했습니다")
	}
}

func TestConfigInventoryDeleteHost(t *testing.T) {
	c, err := parseConfig("inventory_delete_host = delhost01 # 주석\n", "x.conf")
	if err != nil || c.InventoryDeleteHost != "delhost01" {
		t.Errorf("got %q, %v", c.InventoryDeleteHost, err)
	}
	if c, _ := parseConfig("inventory_delete_host =\n", "x.conf"); c.InventoryDeleteHost != "" {
		t.Errorf("빈 값이어야 합니다: %q", c.InventoryDeleteHost)
	}
}
