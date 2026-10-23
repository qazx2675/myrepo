package main

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func fastCfg() *Config {
	c := defaultConfigForTest()
	c.BootPollInterval = time.Millisecond
	c.PortCheckTimeout = time.Millisecond
	return c
}

// defaultConfigForTest 는 config.go 의 defaultConfig 에 의존하지 않고 시험용 값을 만든다.
func defaultConfigForTest() *Config {
	return &Config{Concurrency: 4, ShutdownTimeout: time.Second, BootTimeout: time.Second,
		BootPollInterval: time.Millisecond, PortCheckTimeout: time.Millisecond}
}

func readyVM() *VMInfo {
	return &VMInfo{Name: "svr1ev01", Power: "poweredOn", ToolsRunning: true, Heartbeat: "green", IPs: []string{"10.0.0.5"}}
}

func TestWaitBootDirect(t *testing.T) {
	vm := readyVM()
	b := &BootChecker{Cfg: fastCfg(),
		Refresh: func(ctx context.Context, v *VMInfo) error { return nil },
		Direct:  func(ctx context.Context, ip string, d time.Duration) bool { return ip == "10.0.0.5" }}
	r := b.WaitBoot(context.Background(), vm, time.Second)
	if !r.OK || r.Via != "22 직접" {
		t.Fatalf("직접 22 성공이어야 함: %+v", r)
	}
}

func TestWaitBootToolsFirstThenPort(t *testing.T) {
	vm := &VMInfo{Name: "svr1ev01"}
	n := 0
	b := &BootChecker{Cfg: fastCfg(),
		Refresh: func(ctx context.Context, v *VMInfo) error {
			n++
			if n == 3 { // 3번째 폴링에 Tools/IP 확보
				v.ToolsRunning, v.Heartbeat, v.IPs = true, "green", []string{"10.0.0.5"}
			}
			return nil
		},
		Direct: func(ctx context.Context, ip string, d time.Duration) bool { return n >= 4 }}
	r := b.WaitBoot(context.Background(), vm, time.Second)
	if !r.OK || n < 4 {
		t.Fatalf("Tools 확보 후 포트 열릴 때까지 기다려야 함: n=%d %+v", n, r)
	}
}

func TestWaitBootViaOS6Sticky(t *testing.T) {
	cfg := fastCfg()
	cfg.OS6Host = "os6mgmt01"
	vm := readyVM()
	directCalls, viaCalls := 0, 0
	b := &BootChecker{Cfg: cfg,
		Refresh: func(ctx context.Context, v *VMInfo) error { return nil },
		Direct:  func(ctx context.Context, ip string, d time.Duration) bool { directCalls++; return false },
		ViaOS6:  func(ctx context.Context, ip string) (bool, error) { viaCalls++; return viaCalls >= 2, nil }}
	r := b.WaitBoot(context.Background(), vm, time.Second)
	if !r.OK || r.Via != "22 os6_mgmt 경유" {
		t.Fatalf("os6 경유 성공이어야 함: %+v", r)
	}
	if directCalls != 1 { // 첫 폴링 1회 직접 실패 후, os6 성공 시점부터 sticky. 성공 전까지는 매번 직접도 시도하므로 최대 2
		if directCalls > 2 {
			t.Fatalf("직접 시도가 너무 많음: %d", directCalls)
		}
	}
	// 같은 IP 로 다시 확인하면 직접 시도 없이 os6 만
	before := directCalls
	ok, _, _ := b.checkPort(context.Background(), vm)
	if !ok || directCalls != before {
		t.Fatalf("sticky 후에는 직접 시도 없이 os6 만 써야 함 (direct %d→%d)", before, directCalls)
	}
}

func TestWaitBootTimeoutDetail(t *testing.T) {
	cfg := fastCfg()
	cfg.OS6Host = "os6mgmt01"
	vm := readyVM()
	b := &BootChecker{Cfg: cfg,
		Refresh: func(ctx context.Context, v *VMInfo) error { return nil },
		Direct:  func(ctx context.Context, ip string, d time.Duration) bool { return false },
		ViaOS6: func(ctx context.Context, ip string) (bool, error) {
			return false, errors.New("os6_mgmt(os6mgmt01) 접속 실패")
		}}
	r := b.WaitBoot(context.Background(), vm, 20*time.Millisecond)
	if r.OK || r.Detail == "" {
		t.Fatalf("시간 초과 실패여야 함: %+v", r)
	}
	if want := "os6_mgmt(os6mgmt01) 접속 실패"; !strings.Contains(r.Detail, want) {
		t.Fatalf("ssh 실패가 VM 응답없음과 구분되어야 함: %q", r.Detail)
	}
}

func TestWaitBootNotReadyDetail(t *testing.T) {
	vm := &VMInfo{Name: "svr1ev01", Power: "poweredOn"}
	b := &BootChecker{Cfg: fastCfg(),
		Refresh: func(ctx context.Context, v *VMInfo) error { return nil },
		Direct: func(ctx context.Context, ip string, d time.Duration) bool {
			t.Fatal("Tools 없는데 포트 확인함")
			return false
		}}
	r := b.WaitBoot(context.Background(), vm, 10*time.Millisecond)
	if r.OK || !strings.Contains(r.Detail, "Tools: 미실행") {
		t.Fatalf("%+v", r)
	}
}

func TestWaitBootCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	b := &BootChecker{Cfg: fastCfg(), Refresh: func(ctx context.Context, v *VMInfo) error { return nil }}
	r := b.WaitBoot(ctx, &VMInfo{Name: "x"}, time.Second)
	if r.OK || !strings.Contains(r.Detail, "중단") {
		t.Fatalf("%+v", r)
	}
}

func TestCheckViaOS6RejectsBadInput(t *testing.T) {
	cfg := fastCfg()
	cfg.OS6Host, cfg.OS6User = "ok-host", "root"
	if _, err := checkViaOS6(context.Background(), cfg, "1.2.3.4; reboot"); err == nil {
		t.Fatal("IP 에 명령이 섞여 있으면 거부해야 함")
	}
	cfg.OS6Host = "-oProxyCommand=x"
	if _, err := checkViaOS6(context.Background(), cfg, "1.2.3.4"); err == nil {
		t.Fatal("'-' 로 시작하는 호스트는 거부해야 함")
	}
	cfg.OS6Host = ""
	if ok, err := checkViaOS6(context.Background(), cfg, "1.2.3.4"); ok || err != nil {
		t.Fatal("os6_host 가 비면 조용히 false")
	}
}
