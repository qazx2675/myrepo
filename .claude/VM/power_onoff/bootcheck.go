package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"time"
)

// 부팅 완료 판정(계획서 8장).
//  1차 vCenter : VMware Tools 실행 + heartbeat green + IPv4 확보
//  2차 22 포트 : 이 서버에서 직접 연결
//  3차 22 포트 : 직접 안 되면 os6_mgmt 를 거쳐 확인 (os6_host 가 채워진 경우만)

// BootResult 는 WaitBoot 의 결과다.
type BootResult struct {
	OK      bool
	Via     string // OK 일 때: "22 직접" / "22 os6_mgmt 경유"
	Elapsed time.Duration
	Detail  string // 실패 때 마지막으로 본 상태
}

// BootChecker 는 의존 동작을 함수로 받아 시험할 수 있게 했다.
type BootChecker struct {
	Cfg     *Config
	Refresh func(ctx context.Context, vm *VMInfo) error
	Direct  func(ctx context.Context, ip string, timeout time.Duration) bool
	ViaOS6  func(ctx context.Context, ip string) (ok bool, sshErr error)
	Log     func(vm, format string, a ...any)

	viaOnly sync.Map // ip -> true : 직접 접속이 막힌 대역(이후 os6 경로만 사용)
}

// NewBootChecker 는 실제 동작(vCenter 조회, TCP 접속, ssh 경유)을 연결한다.
func NewBootChecker(cfg *Config, refresh func(ctx context.Context, vm *VMInfo) error, log func(vm, format string, a ...any)) *BootChecker {
	b := &BootChecker{Cfg: cfg, Refresh: refresh, Log: log}
	b.Direct = dialPort22
	b.ViaOS6 = func(ctx context.Context, ip string) (bool, error) { return checkViaOS6(ctx, cfg, ip) }
	return b
}

func (b *BootChecker) logf(vm, format string, a ...any) {
	if b.Log != nil {
		b.Log(vm, format, a...)
	}
}

func dialPort22(ctx context.Context, ip string, timeout time.Duration) bool {
	d := net.Dialer{Timeout: timeout}
	c, err := d.DialContext(ctx, "tcp", net.JoinHostPort(ip, "22"))
	if err != nil {
		return false
	}
	c.Close()
	return true
}

var hostRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

// checkViaOS6 는 os6_mgmt 에서 ip:22 로 /dev/tcp 연결을 해 본다. ssh 자체가 실패하면(종료코드 255) sshErr 로 구분한다.
func checkViaOS6(ctx context.Context, cfg *Config, ip string) (bool, error) {
	if cfg.OS6Host == "" {
		return false, nil
	}
	if net.ParseIP(ip) == nil || net.ParseIP(ip).To4() == nil { // 명령 주입 방지: IPv4 만
		return false, fmt.Errorf("잘못된 IP: %q", ip)
	}
	if !hostRe.MatchString(cfg.OS6Host) || !hostRe.MatchString(cfg.OS6User) {
		return false, fmt.Errorf("os6_host/os6_user 값이 올바르지 않습니다")
	}
	secs := int(cfg.PortCheckTimeout / time.Second)
	if secs < 1 {
		secs = 1
	}
	remote := fmt.Sprintf("timeout %d bash -c '</dev/tcp/%s/22'", secs, ip)
	cmd := exec.CommandContext(ctx, "ssh", "-o", "BatchMode=yes", "-o", "ConnectTimeout=5",
		"-o", "StrictHostKeyChecking=accept-new", "-o", "LogLevel=ERROR",
		cfg.OS6User+"@"+cfg.OS6Host, remote)
	err := cmd.Run()
	if err == nil {
		return true, nil
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		if ee.ExitCode() == 255 {
			return false, fmt.Errorf("os6_mgmt(%s) 접속 실패", cfg.OS6Host)
		}
		return false, nil // 접속은 됐고 22 포트만 응답 없음
	}
	return false, fmt.Errorf("ssh 실행 실패: %v", err)
}

// WaitBoot 는 vm 이 부팅 완료될 때까지 BootPollInterval 간격으로 확인한다. timeout 이 지나면 실패로 돌려준다.
func (b *BootChecker) WaitBoot(ctx context.Context, vm *VMInfo, timeout time.Duration) BootResult {
	start := time.Now()
	deadline := start.Add(timeout)
	var lastPort string
	for {
		if err := b.Refresh(ctx, vm); err != nil && ctx.Err() == nil {
			b.logf(vm.Name, "상태 조회 실패: %v", err)
		}
		ready := vm.ToolsRunning && vm.Heartbeat == "green" && len(vm.IPs) > 0
		if ready {
			ok, via, note := b.checkPort(ctx, vm)
			if ok {
				return BootResult{OK: true, Via: via, Elapsed: time.Since(start)}
			}
			lastPort = note
		} else {
			lastPort = ""
		}
		if ctx.Err() != nil {
			return BootResult{Elapsed: time.Since(start), Detail: "중단됨 (" + b.state(vm, lastPort) + ")"}
		}
		if time.Now().Add(b.Cfg.BootPollInterval).After(deadline) {
			return BootResult{Elapsed: time.Since(start), Detail: b.state(vm, lastPort)}
		}
		select {
		case <-ctx.Done():
		case <-time.After(b.Cfg.BootPollInterval):
		}
	}
}

func (b *BootChecker) state(vm *VMInfo, port string) string {
	tools := "미실행"
	if vm.ToolsRunning {
		tools = "running"
	}
	hb := vm.Heartbeat
	if hb == "" {
		hb = "없음"
	}
	ip := "없음"
	if len(vm.IPs) > 0 {
		ip = strings.Join(vm.IPs, ",")
	}
	s := fmt.Sprintf("Tools: %s / heartbeat: %s / IP: %s", tools, hb, ip)
	if port != "" {
		s += " / 22: " + port
	}
	return s
}

// checkPort 는 IP 후보마다 22 포트를 확인한다. 직접 → (막혔으면) os6_mgmt 경유 순서.
func (b *BootChecker) checkPort(ctx context.Context, vm *VMInfo) (ok bool, via, note string) {
	note = "응답없음"
	for _, ip := range vm.IPs {
		if p := net.ParseIP(ip); p == nil || p.To4() == nil {
			continue
		}
		if _, only := b.viaOnly.Load(ip); !only {
			if b.Direct(ctx, ip, b.Cfg.PortCheckTimeout) {
				return true, "22 직접", ""
			}
		}
		if b.Cfg.OS6Host == "" || b.ViaOS6 == nil {
			continue
		}
		okv, err := b.ViaOS6(ctx, ip)
		if okv {
			b.viaOnly.Store(ip, true)
			return true, "22 os6_mgmt 경유", ""
		}
		if err != nil {
			note = err.Error() // VM 응답없음과 구분
		}
	}
	return false, "", note
}
