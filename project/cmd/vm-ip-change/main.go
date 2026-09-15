// vm-ip-change 는 vcenter.txt 에 적힌 vCenter 들을 순회하며 list.txt 에 적힌
// "호스트네임 새IP" 대상을 찾아, VMware Tools Guest Operations API 로 게스트
// 안에서 직접 IP 를 재설정합니다. vCenter API 경로만 쓰므로 대상 VM 에
// 네트워크로 접속하지 못하는 상황(IP 오할당으로 인한 접속 불가)에서도 동작합니다.
//
// 대상은 최대 maxConcurrent 대까지 동시에 처리합니다(순차 처리 시 CPU 를
// 과점유당해 게스트 명령이 느린 VM 하나가 뒤의 모든 VM 을 막는 문제를 피하기
// 위함). 느린 VM 에 대해서도 별도 타임아웃 없이 끝까지 기다립니다 — 병렬화로
// 이미 다른 VM 을 막지 않으므로, 여기서 포기시키는 것보다 실제로 끝날 때까지
// 기다려 정상 처리하는 쪽을 선택했습니다.
//
// Ctrl+C(SIGINT) 를 누르면 아직 시작하지 않은 대상은 취소하고, 이미 IP 를
// 바꿨지만 아직 완료(연결 재기동)되지 않은 대상은 원래 설정으로 되돌립니다.
// 이미 [OK] 로 끝난 대상은 그대로 둡니다. 이 처리 중에도 각 게스트 명령은
// 끝까지 기다리므로(위와 동일한 이유), 즉시 강제 종료하려면 별도 터미널에서
// pkill 로 이 프로세스를 종료하십시오(SIGTERM은 가로채지 않으므로 즉시 종료됨).
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"

	"github.com/vmware/govmomi"
	"github.com/vmware/govmomi/vim25/mo"

	"vm-ip-change/internal/status"
	"vm-ip-change/internal/target"
	"vm-ip-change/internal/tui"
	"vm-ip-change/internal/vsphere"
)

// maxConcurrent 는 동시에 처리할 VM 수입니다. vCenter/ESXi 에 걸리는 API 부하를
// 감안해 고정값으로 둡니다.
const maxConcurrent = 16

// located 는 대상 VM 이 어느 vCenter 접속(client)의 어느 VM 인지를 담습니다.
type located struct {
	client *govmomi.Client
	vm     mo.VirtualMachine
}

func main() {
	vcenterFile := flag.String("vcenter", "vcenter.txt", "대상 vCenter 목록 파일")
	listFile := flag.String("list", "list.txt", "작업 대상(호스트네임/새IP) 목록 파일")
	flag.Parse()

	vcUser := os.Getenv("VC_USER")
	vcPass := os.Getenv("VC_PASSWORD")
	guestUser := os.Getenv("GUEST_USER")
	guestPass := os.Getenv("GUEST_PASSWORD")
	if vcUser == "" || vcPass == "" || guestUser == "" || guestPass == "" {
		fmt.Fprintln(os.Stderr, "환경변수 VC_USER, VC_PASSWORD, GUEST_USER, GUEST_PASSWORD 가 모두 필요합니다")
		os.Exit(1)
	}

	vcenters, err := target.LoadVCenters(*vcenterFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}
	entries, err := target.LoadEntries(*listFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}

	fmt.Println("작업 대상:")
	for _, e := range entries {
		fmt.Printf("  %-24s -> %s (GW %s)\n", e.Hostname, e.NewIP, target.Gateway(e.NewIP))
	}
	fmt.Printf("동시 처리: 최대 %d대\n", maxConcurrent)
	fmt.Println(strings.Repeat("-", 60))

	ctx := context.Background()

	// vCenter 들을 순서대로 뒤져 각 호스트가 어느 vCenter/VM 에 있는지 찾는다
	// (먼저 찾은 vCenter 에서 처리하고, 다음 vCenter 에서는 아직 못 찾은 대상만
	// 계속 찾는다 — 기존 순차 버전과 동일한 규칙).
	remaining := make(map[string]bool, len(entries))
	for _, e := range entries {
		remaining[e.Hostname] = true
	}

	locations := make(map[string]located, len(entries))
	var clients []*govmomi.Client
	defer func() {
		for _, c := range clients {
			c.Logout(ctx)
		}
	}()

	for _, addr := range vcenters {
		if len(remaining) == 0 {
			break
		}
		c, err := vsphere.Connect(ctx, addr, vcUser, vcPass)
		if err != nil {
			fmt.Printf("[FAIL] %s: %v\n", addr, err)
			continue
		}
		clients = append(clients, c)

		vms, err := vsphere.FindVMs(ctx, c, remaining)
		if err != nil {
			fmt.Printf("[FAIL] %s: %v\n", addr, err)
			continue
		}
		for host, vm := range vms {
			locations[host] = located{client: c, vm: vm}
			delete(remaining, host)
		}
	}

	// 진행률 추적기를 만들고, 이 시점에 이미 실패가 확정된 대상(못 찾음/전원
	// 꺼짐/Tools 미실행)은 워커를 띄우지 않고 바로 실패로 기록한다.
	vmStatus := make(map[string]*status.VM, len(entries))
	var trackedVMs []*status.VM
	var runnable []target.Entry

	for _, e := range entries {
		sv := &status.VM{Hostname: e.Hostname, NewIP: e.NewIP, Gateway: target.Gateway(e.NewIP)}
		vmStatus[e.Hostname] = sv
		trackedVMs = append(trackedVMs, sv)

		loc, ok := locations[e.Hostname]
		if !ok {
			sv.Finish(status.OutcomeFailed, fmt.Errorf("어느 vCenter 에서도 찾지 못했습니다"))
			continue
		}
		if err := vsphere.IsReady(loc.vm); err != nil {
			sv.Finish(status.OutcomeFailed, err)
			continue
		}
		runnable = append(runnable, e)
	}

	tracker := status.NewTracker(trackedVMs)

	// Ctrl+C: 새 작업은 막고, 이미 설정을 바꾼 뒤 아직 끝나지 않은 작업만
	// 되돌린다. SIGTERM 은 가로채지 않으므로 pkill 로는 즉시 종료된다.
	var cancelRequested atomic.Bool
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT)
	go func() {
		<-sigCh
		cancelRequested.Store(true)
		fmt.Fprintln(os.Stderr, "\nCtrl+C 감지: 새 작업 시작을 멈추고, 이미 IP 를 바꾼 뒤 아직 끝나지 않은 작업만 원래대로 되돌립니다. 즉시 종료하려면 다른 터미널에서 pkill 로 강제 종료하세요.")
	}()

	done := make(chan struct{})
	renderDone := make(chan struct{})
	go func() {
		tui.Run(tracker, done)
		close(renderDone)
	}()

	sem := make(chan struct{}, maxConcurrent)
	var wg sync.WaitGroup
	for _, e := range runnable {
		e := e
		loc := locations[e.Hostname]
		sv := vmStatus[e.Hostname]

		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			runOne(ctx, loc.client, loc.vm, e, guestUser, guestPass, sv, &cancelRequested)
		}()
	}
	wg.Wait()
	close(done)
	<-renderDone // tui.Run 이 최종 결과표를 다 찍을 때까지 기다린다.

	failCount := 0
	for _, v := range trackedVMs {
		if v.Snapshot().Outcome != status.OutcomeDone {
			failCount++
		}
	}
	if failCount > 0 {
		os.Exit(1)
	}
}

// runOne 은 한 VM 의 3단계(연결 확인 -> IP 설정 적용 -> 연결 재기동)를 순서대로
// 실행하며 각 단계 사이에서 cancelRequested 를 확인합니다. 이미 설정을 바꾼
// 뒤 취소가 감지되면 되돌리기(vsphere.Revert)를 실행합니다.
func runOne(ctx context.Context, c *govmomi.Client, vm mo.VirtualMachine, e target.Entry, guestUser, guestPass string, sv *status.VM, cancelRequested *atomic.Bool) {
	id := target.SanitizeID(e.Hostname)
	gw := target.Gateway(e.NewIP)

	if cancelRequested.Load() {
		sv.Finish(status.OutcomeCancelled, nil)
		return
	}

	sv.SetPhase(status.PhaseChecking)
	if err := vsphere.CheckConnection(ctx, c, vm.Reference(), guestUser, guestPass, id); err != nil {
		sv.Finish(status.OutcomeFailed, err)
		return
	}

	if cancelRequested.Load() {
		// 아직 게스트 설정을 바꾸지 않았으므로 되돌릴 것이 없다.
		sv.Finish(status.OutcomeCancelled, nil)
		return
	}

	sv.SetPhase(status.PhaseApplying)
	if err := vsphere.ApplyIP(ctx, c, vm.Reference(), guestUser, guestPass, id, e.NewIP, gw); err != nil {
		sv.Finish(status.OutcomeFailed, err)
		return
	}

	if cancelRequested.Load() {
		revertAndFinish(ctx, c, vm, guestUser, guestPass, id, sv)
		return
	}

	sv.SetPhase(status.PhaseRestarting)
	if err := vsphere.RestartConnection(ctx, c, vm.Reference(), guestUser, guestPass, id); err != nil {
		sv.Finish(status.OutcomeFailed, err)
		return
	}

	// RestartConnection 이 성공하면 그 안에서 되돌리기 스크립트도 함께 지워지므로
	// (vsphere.go 참고) 이 시점부터는 취소돼도 되돌릴 수 없다 — 완료로 처리한다.
	sv.Finish(status.OutcomeDone, nil)
}

func revertAndFinish(ctx context.Context, c *govmomi.Client, vm mo.VirtualMachine, guestUser, guestPass, id string, sv *status.VM) {
	sv.SetPhase(status.PhaseReverting)
	if err := vsphere.Revert(ctx, c, vm.Reference(), guestUser, guestPass, id); err != nil {
		sv.Finish(status.OutcomeFailed, fmt.Errorf("되돌리기 실패(수동 확인 필요): %w", err))
		return
	}
	sv.Finish(status.OutcomeRolledBack, nil)
}
