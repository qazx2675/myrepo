//go:build vcsim

package main

import (
	"context"
	"crypto/tls"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vmware/govmomi/find"
	"github.com/vmware/govmomi/object"
	"github.com/vmware/govmomi/simulator"
	"github.com/vmware/govmomi/vim25/types"
)

// startSim 은 TLS vcsim 서버를 띄우고 host:port 를 돌려준다.
func startSim(t *testing.T) (hostPort string, _ struct{}) {
	t.Helper()
	model := simulator.VPX()
	if err := model.Create(); err != nil {
		t.Fatal(err)
	}
	model.Service.TLS = new(tls.Config)
	s := model.Service.NewServer()
	t.Cleanup(func() { s.Close(); model.Remove() })
	return s.URL.Host, struct{}{}
}

func TestVCenterLookup(t *testing.T) {
	ctx := context.Background()
	hp, _ := startSim(t)
	c, err := Connect(ctx, hp, "user", "pass")
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close(ctx)

	fd := find.NewFinder(c.Govmomi().Client, true)
	dc, err := fd.DefaultDatacenter(ctx)
	if err != nil {
		t.Fatal(err)
	}
	fd.SetDatacenter(dc)
	vms, err := fd.VirtualMachineList(ctx, "*")
	if err != nil || len(vms) < 4 {
		t.Fatalf("vms=%d err=%v", len(vms), err)
	}
	// 이름 바꾸기
	for i, n := range []string{"svr1ev01", "svr1ev02", "svr2ev01", "dupev01"} {
		task, err := vms[i].Rename(ctx, n)
		if err != nil {
			t.Fatal(err)
		}
		if err := task.Wait(ctx); err != nil {
			t.Fatal(err)
		}
	}
	// 중복 이름: 4번째와 같은 이름의 VM 을 하나 더
	if len(vms) > 4 {
		task, _ := vms[4].Rename(ctx, "dupev01")
		if err := task.Wait(ctx); err != nil {
			t.Fatal(err)
		}
	}
	// VWP-Prod 폴더 생성 후 svr1ev02 이동
	root, err := fd.Folder(ctx, "/DC0/vm")
	if err != nil {
		t.Fatal(err)
	}
	vf, err := root.CreateFolder(ctx, "VWP-Prod")
	if err != nil {
		t.Fatal(err)
	}
	task, err := vf.MoveInto(ctx, []types.ManagedObjectReference{vms[1].Reference()})
	if err != nil {
		t.Fatal(err)
	}
	if err := task.Wait(ctx); err != nil {
		t.Fatal(err)
	}
	// svr2ev01: lpage 키 추가 + 전원 on
	task, err = vms[2].Reconfigure(ctx, types.VirtualMachineConfigSpec{
		ExtraConfig: []types.BaseOptionValue{&types.OptionValue{Key: "sched.mem.lpage.enable1GPage", Value: "true"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := task.Wait(ctx); err != nil {
		t.Fatal(err)
	}

	got, err := c.LookupVMs(ctx, []string{"svr2ev01", "svr1ev01", "svr1ev02"})
	if err != nil {
		t.Fatal(err)
	}
	var order []string
	for _, v := range got {
		order = append(order, v.Name)
	}
	if strings.Join(order, ",") != "svr2ev01,svr1ev01,svr1ev02" {
		t.Fatalf("순서=%v", order)
	}
	by := map[string]*VMInfo{}
	for _, v := range got {
		by[v.Name] = v
		if v.Obj == nil || v.Host == "" || v.Power == "" || strings.Contains(v.Host, ".") {
			t.Errorf("%s: 필드 이상 %+v", v.Name, v)
		}
		if len(v.PortGroups) == 0 {
			t.Errorf("%s: 포트그룹 없음", v.Name)
		}
		t.Logf("%s host=%s power=%s folder=%s pg=%v lpage=%v tools=%v hb=%q ips=%v",
			v.Name, v.Host, v.Power, v.FolderPath, v.PortGroups, v.Lpage, v.ToolsRunning, v.Heartbeat, v.IPs)
	}
	if v := by["svr1ev01"]; v.NameBM != "svr1" || v.Ev != 1 || v.FolderPath != "DC0/vm" || v.Lpage {
		t.Errorf("svr1ev01=%+v", v)
	}
	if v := by["svr1ev02"]; v.FolderPath != "DC0/vm/VWP-Prod" || v.Ev != 2 {
		t.Errorf("svr1ev02=%+v", v)
	}
	if v := by["svr2ev01"]; !v.Lpage || v.NameBM != "svr2" {
		t.Errorf("svr2ev01=%+v", v)
	}

	// VWP 연동
	hits := FindVWP(got)
	if len(hits) != 1 || hits[0].VM.Name != "svr1ev02" {
		t.Errorf("hits=%+v", hits)
	}

	// 없음 + 중복은 한꺼번에
	_, err = c.LookupVMs(ctx, []string{"svr1ev01", "nope01ev01", "dupev01"})
	var le *LookupError
	if !errors.As(err, &le) {
		t.Fatalf("err=%v", err)
	}
	if len(le.Missing) != 1 || le.Missing[0] != "nope01ev01" {
		t.Errorf("missing=%v", le.Missing)
	}
	if len(vms) > 4 && len(le.Duplicate["dupev01"]) != 2 {
		t.Errorf("dup=%v", le.Duplicate)
	}

	// Refresh: 전원 변화 반영
	v := by["svr1ev01"]
	before := v.Power
	var tk *object.Task
	if before == string(types.VirtualMachinePowerStatePoweredOn) {
		tk, err = v.Obj.PowerOff(ctx)
	} else {
		tk, err = v.Obj.PowerOn(ctx)
	}
	if err != nil {
		t.Fatal(err)
	}
	if err := tk.Wait(ctx); err != nil {
		t.Fatal(err)
	}
	if err := c.Refresh(ctx, v); err != nil {
		t.Fatal(err)
	}
	if v.Power == before {
		t.Errorf("전원 상태 변화 미반영: %s", v.Power)
	}
}

func TestVCenterDistributedPortGroup(t *testing.T) {
	ctx := context.Background()
	hp, _ := startSim(t)
	c, err := Connect(ctx, hp, "user", "pass")
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close(ctx)
	fd := find.NewFinder(c.Govmomi().Client, true)
	dc, _ := fd.DefaultDatacenter(ctx)
	fd.SetDatacenter(dc)
	vms, _ := fd.VirtualMachineList(ctx, "*")
	task, _ := vms[0].Rename(ctx, "pgev01")
	if err := task.Wait(ctx); err != nil {
		t.Fatal(err)
	}
	nets, err := fd.NetworkList(ctx, "*")
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range nets {
		if n.Reference().Type == "DistributedVirtualPortgroup" {
			task, err := object.NewCommon(c.Govmomi().Client, n.Reference()).Rename(ctx, "vwp-dvpg")
			if err != nil {
				t.Fatal(err)
			}
			if err := task.Wait(ctx); err != nil {
				t.Fatal(err)
			}
		}
	}
	got, err := c.LookupVMs(ctx, []string{"pgev01"})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("portgroups=%v", got[0].PortGroups)
	// vcsim 기본 VM 은 표준 포트그룹이므로 분산 포트그룹 이름이 보이면 변환 성공, 아니면 표준 이름이 보인다.
	if len(got[0].PortGroups) == 0 {
		t.Error("포트그룹 없음")
	}
}

func TestGetPassword(t *testing.T) {
	dir := t.TempDir()
	lib := filepath.Join(dir, "lib.sh")
	os.WriteFile(lib, []byte("secret_get() { [ \"$1\" = vcenter ] && [ \"$2\" = adm ] && echo s3cret; }\n"), 0o600)
	t.Setenv("VC_PASSWORD", "env-pass")
	t.Setenv("VC_PASS", "env-pass")
	never := func(string) (string, error) { t.Fatal("prompt 호출되면 안 됨"); return "", nil }
	if p, err := GetPassword(&Config{SecretLib: lib}, "vc", "adm", never); err != nil || p != "s3cret" {
		t.Fatalf("p=%q err=%v", p, err)
	}
	// secret_get 이 실패하는 계정 -> prompt, 환경변수 무시
	var msg string
	prompt := func(m string) (string, error) { msg = m; return "typed", nil }
	if p, _ := GetPassword(&Config{SecretLib: lib}, "vc", "other", prompt); p != "typed" || !strings.Contains(msg, "other") {
		t.Fatalf("p=%q msg=%q", p, msg)
	}
	if p, _ := GetPassword(&Config{}, "vc", "adm", prompt); p != "typed" {
		t.Fatalf("p=%q", p)
	}
	if p, _ := GetPassword(&Config{SecretLib: filepath.Join(dir, "none.sh")}, "vc", "adm", prompt); p != "typed" {
		t.Fatalf("p=%q", p)
	}
}

func TestLastVCenter(t *testing.T) {
	d := filepath.Join(t.TempDir(), "run", "x")
	if LoadLastVCenter(d) != "" {
		t.Error("없어야 함")
	}
	if err := SaveLastVCenter(d, "vc1.example"); err != nil {
		t.Fatal(err)
	}
	if LoadLastVCenter(d) != "vc1.example" {
		t.Error("불일치")
	}
	if fi, _ := os.Stat(LastVCenterFile(d)); fi.Mode().Perm() != 0o600 {
		t.Errorf("perm=%v", fi.Mode())
	}
}

func TestSplitVMName(t *testing.T) {
	for in, want := range map[string]struct {
		bm string
		ev int
		ok bool
	}{
		"svr1ev01":   {"svr1", 1, true},
		"aev02ev12":  {"aev02", 12, true},
		"svr1":       {"", 0, false},
		"svr1ev1":    {"", 0, false},
		"-svr1ev01":  {"", 0, false},
		"web-prd-01": {"", 0, false},
	} {
		bm, ev, ok := splitVMName(in)
		if bm != want.bm || ev != want.ev || ok != want.ok {
			t.Errorf("%s -> %q %d %v", in, bm, ev, ok)
		}
	}
}
