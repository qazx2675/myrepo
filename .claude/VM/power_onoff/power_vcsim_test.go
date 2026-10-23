//go:build vcsim

package main

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/vmware/govmomi/find"
	"github.com/vmware/govmomi/vim25/types"
)

// 실제 govmomi 구현(Client + govmomiOps)을 vcsim 위에서 돌린다.
// vcsim 은 Tools/Heartbeat/IP 를 채우지 않으므로 부팅 완료 판정(WaitBoot)만 대체한다.

func prepareSimVMs(t *testing.T, ctx context.Context, c *Client, names []string, lpage map[string]bool, off map[string]bool) {
	t.Helper()
	fd := find.NewFinder(c.Govmomi().Client, true)
	dc, err := fd.DefaultDatacenter(ctx)
	if err != nil {
		t.Fatal(err)
	}
	fd.SetDatacenter(dc)
	vms, err := fd.VirtualMachineList(ctx, "*")
	if err != nil || len(vms) < len(names) {
		t.Fatalf("시뮬레이터 VM 부족: %d err=%v", len(vms), err)
	}
	for i, n := range names {
		vm := vms[i]
		task, err := vm.Rename(ctx, n)
		if err != nil {
			t.Fatal(err)
		}
		if err := task.Wait(ctx); err != nil {
			t.Fatal(err)
		}
		if lpage[n] {
			spec := types.VirtualMachineConfigSpec{ExtraConfig: []types.BaseOptionValue{
				&types.OptionValue{Key: "sched.mem.lpage.enable1GPage", Value: "TRUE"}}}
			rt, err := vm.Reconfigure(ctx, spec)
			if err != nil {
				t.Fatal(err)
			}
			if err := rt.Wait(ctx); err != nil {
				t.Fatal(err)
			}
		}
		if off[n] {
			pt, err := vm.PowerOff(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if err := pt.Wait(ctx); err != nil {
				t.Fatal(err)
			}
		}
	}
}

func simRunner(c *Client, answers ...string) *Runner {
	var mu sync.Mutex
	i := 0
	cfg := &Config{Concurrency: 4, ShutdownTimeout: 100 * time.Millisecond, BootTimeout: time.Second,
		BootPollInterval: time.Millisecond, PortCheckTimeout: time.Millisecond}
	return &Runner{Cfg: cfg, Ops: NewGovmomiOps(c), Log: NewRunLog(testWriter{}, nil),
		Ask: func(ctx context.Context, p string) (string, error) {
			mu.Lock()
			defer mu.Unlock()
			if i >= len(answers) {
				return "", context.Canceled
			}
			a := answers[i]
			i++
			return a, nil
		}}
}

type testWriter struct{}

func (testWriter) Write(p []byte) (int, error) { return len(p), nil }

func TestVcsimOnAllAndOff(t *testing.T) {
	ctx := context.Background()
	hp, _ := startSim(t)
	c, err := Connect(ctx, hp, "user", "pass")
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close(ctx)
	names := []string{"svr1ev01", "svr1ev02", "svr2ev01"}
	prepareSimVMs(t, ctx, c, names, nil, map[string]bool{"svr1ev01": true, "svr1ev02": true})

	vms, err := c.LookupVMs(ctx, names)
	if err != nil {
		t.Fatal(err)
	}
	r := simRunner(c)
	rs := r.RunOnAll(ctx, vms)
	if statusOf(rs, "svr1ev01").Status != StOK || statusOf(rs, "svr1ev02").Status != StOK || statusOf(rs, "svr2ev01").Status != StSkip {
		t.Fatalf("일괄 On: %+v", rs)
	}
	for _, v := range vms {
		if err := c.Refresh(ctx, v); err != nil || v.Power != "poweredOn" {
			t.Fatalf("%s=%s err=%v", v.Name, v.Power, err)
		}
	}

	// vcsim 은 Tools 를 채우지 않아 Guest 종료가 불가 → 강제 PowerOff 확인 경로를 탄다.
	r = simRunner(c, "YeS")
	rs = r.RunOff(ctx, vms)
	for _, x := range rs {
		if x.Status != StOK {
			t.Fatalf("Off: %+v", rs)
		}
	}
	for _, v := range vms {
		_ = c.Refresh(ctx, v)
		if v.Power != "poweredOff" {
			t.Fatalf("%s=%s", v.Name, v.Power)
		}
	}
	// 이미 꺼진 VM 을 다시 Off → 스킵
	rs = simRunner(c).RunOff(ctx, vms)
	for _, x := range rs {
		if x.Status != StSkip {
			t.Fatalf("재 Off: %+v", rs)
		}
	}
}

func TestVcsimOffRefusedWithoutYeS(t *testing.T) {
	ctx := context.Background()
	hp, _ := startSim(t)
	c, err := Connect(ctx, hp, "user", "pass")
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close(ctx)
	prepareSimVMs(t, ctx, c, []string{"svr1ev01"}, nil, nil)
	vms, _ := c.LookupVMs(ctx, []string{"svr1ev01"})
	rs := simRunner(c, "yes").RunOff(ctx, vms)
	_ = c.Refresh(ctx, vms[0])
	if rs[0].Status != StFail || vms[0].Power != "poweredOn" {
		t.Fatalf("YeS 가 아니면 끄면 안 됨: %+v power=%s", rs, vms[0].Power)
	}
}

func TestVcsimOnSeqLpageOrderAndReboot(t *testing.T) {
	ctx := context.Background()
	hp, _ := startSim(t)
	c, err := Connect(ctx, hp, "user", "pass")
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close(ctx)
	names := []string{"svr1ev01", "svr1ev02", "svr1ev03", "svr2ev01"}
	prepareSimVMs(t, ctx, c, names, map[string]bool{"svr1ev02": true},
		map[string]bool{"svr1ev01": true, "svr1ev02": true, "svr1ev03": true, "svr2ev01": true})
	vms, err := c.LookupVMs(ctx, names)
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range vms {
		if v.Lpage != (v.Name == "svr1ev02") {
			t.Fatalf("lpage 판정: %s=%v", v.Name, v.Lpage)
		}
		v.Host = "" // vcsim 은 VM 을 임의 호스트에 두므로 이름상 BM 기준으로 묶는다
	}
	// svr1 은 lpage 사용중(하나라도) → 순차. 부팅 확인 시점에 "이전 VM 은 켜짐, 이후 VM 은 꺼짐" 이어야 한다.
	byName := map[string]*VMInfo{}
	for _, v := range vms {
		byName[v.Name] = v
	}
	var mu sync.Mutex
	var order []string
	r := simRunner(c)
	r.WaitBoot = func(ctx context.Context, vm *VMInfo, d time.Duration) BootResult {
		mu.Lock()
		order = append(order, vm.Name)
		mu.Unlock()
		if vm.NameBM == "svr1" {
			for _, other := range []string{"svr1ev01", "svr1ev02", "svr1ev03"} {
				o := &VMInfo{Name: other, Obj: byName[other].Obj}
				_ = c.Refresh(ctx, o)
				var shouldOn bool
				switch vm.Name {
				case "svr1ev01":
					shouldOn = other == "svr1ev01"
				case "svr1ev02":
					shouldOn = other != "svr1ev03"
				default:
					shouldOn = true
				}
				if (o.Power == "poweredOn") != shouldOn {
					t.Errorf("%s 부팅 확인 시점에 %s=%s (순차 위반)", vm.Name, other, o.Power)
				}
			}
		}
		return BootResult{OK: true, Via: "22 직접"}
	}
	rs := r.RunOnSeq(ctx, vms)
	for _, x := range rs {
		if x.Status != StOK {
			t.Fatalf("순차 On: %+v", rs)
		}
	}
	// 재부팅: 전부 켜진 상태에서 Off(강제 YeS) → On(순차)
	vms, _ = c.LookupVMs(ctx, names)
	for _, v := range vms {
		v.Host = ""
	}
	r2 := simRunner(c, "YeS")
	r2.WaitBoot = func(ctx context.Context, vm *VMInfo, d time.Duration) BootResult {
		return BootResult{OK: true, Via: "22 직접"}
	}
	rs = r2.RunReboot(ctx, vms)
	offs, ons := 0, 0
	for _, x := range rs {
		if x.Status != StOK {
			t.Fatalf("재부팅: %+v", rs)
		}
		if x.Op == "Off" {
			offs++
		} else {
			ons++
		}
	}
	if offs != 4 || ons != 4 {
		t.Fatalf("off=%d on=%d", offs, ons)
	}
}
