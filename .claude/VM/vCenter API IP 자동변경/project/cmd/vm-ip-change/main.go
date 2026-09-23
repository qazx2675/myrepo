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
// Ctrl+C 는 5초 안에 3번 눌러야 즉시 종료됩니다(1~2번은 안내만 표시 — 실수로
// 한 번 눌러 작업이 끊기지 않게). 종료 시 되돌리기는 하지 않으며, IP 를 바꾸던
// 중이던 대상과 수동 복구용 게스트 스크립트 경로를 출력합니다(internal/tui).
// SIGTERM(pkill)은 가로채지 않으므로 그대로 즉시 종료됩니다.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

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

// pairRecheckInterval 은 ev02 대상이 짝(ev01)의 부하 때문에 후순위로 밀렸을 때,
// 다시 부하를 확인하는 주기입니다.
const pairRecheckInterval = 2 * time.Minute

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

	// ev02 대상은 짝(ev01)의 부하를 봐서 후순위로 미룰 수 있다(아래 pairOf/bmOf).
	// ev01 은 위치(client+vm, uptime 을 돌릴 곳)를, BM(ESXi 호스트) 이름은 물리
	// 코어 수를 얻는 데 쓴다. hostname 규칙상 BM "hostname01" 의 두 VM 은 항상
	// "hostname01ev01"/"hostname01ev02" 이므로 짝과 BM 이름은 문자열만으로 정해진다.
	pairOf := make(map[string]string) // ev02 hostname -> ev01 hostname
	bmOf := make(map[string]string)   // ev02 hostname -> BM(ESXi 호스트) 이름
	for _, e := range entries {
		if pair, ok := target.PairHostname(e.Hostname); ok {
			pairOf[e.Hostname] = pair
			remaining[pair] = true
		}
		if bm, ok := target.BareMetalName(e.Hostname); ok {
			bmOf[e.Hostname] = bm
		}
	}
	bmRemaining := make(map[string]bool, len(bmOf))
	for _, bm := range bmOf {
		bmRemaining[bm] = true
	}
	bmHosts := make(map[string]mo.HostSystem, len(bmRemaining))

	locations := make(map[string]located, len(entries))
	var clients []*govmomi.Client
	defer func() {
		for _, c := range clients {
			c.Logout(ctx)
		}
	}()

	for _, addr := range vcenters {
		if len(remaining) == 0 && len(bmRemaining) == 0 {
			break
		}
		c, err := vsphere.Connect(ctx, addr, vcUser, vcPass)
		if err != nil {
			fmt.Printf("[FAIL] %s: %v\n", addr, err)
			continue
		}
		clients = append(clients, c)

		if len(remaining) > 0 {
			vms, err := vsphere.FindVMs(ctx, c, remaining)
			if err != nil {
				fmt.Printf("[FAIL] %s: %v\n", addr, err)
			} else {
				for host, vm := range vms {
					locations[host] = located{client: c, vm: vm}
					delete(remaining, host)
				}
			}
		}
		if len(bmRemaining) > 0 {
			hosts, err := vsphere.FindHosts(ctx, c, bmRemaining)
			if err != nil {
				fmt.Printf("[FAIL] %s: %v\n", addr, err)
			} else {
				for name, h := range hosts {
					bmHosts[name] = h
					delete(bmRemaining, name)
				}
			}
		}
	}

	// ev02 hostname -> 짝(ev01)이 확인해야 할 BM 물리 코어 수. 못 찾았으면 0 —
	// waitForPairIdle 이 이 경우 미루지 않고 바로 진행한다.
	pairCoreCount := make(map[string]int, len(bmOf))
	for host, bm := range bmOf {
		if h, ok := bmHosts[bm]; ok {
			pairCoreCount[host] = vsphere.PhysicalCoreCount(h)
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

	// Ctrl+C 는 tui 가 센다(5초 안에 3번이면 즉시 종료). raw 모드(60초 이후)에서는
	// Ctrl+C 가 시그널이 아니라 키 입력으로 들어오므로 그쪽도 tui 가 같이 처리한다.
	// SIGTERM 은 가로채지 않으므로 pkill 로는 즉시 종료된다.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT)

	done := make(chan struct{})
	renderDone := make(chan struct{})
	go func() {
		tui.Run(tracker, done, sigCh)
		close(renderDone)
	}()

	sem := make(chan struct{}, maxConcurrent)
	var wg sync.WaitGroup
	for _, e := range runnable {
		e := e
		loc := locations[e.Hostname]
		sv := vmStatus[e.Hostname]
		pairHost, hasPair := pairOf[e.Hostname]
		coreCount := pairCoreCount[e.Hostname]

		wg.Add(1)
		go func() {
			defer wg.Done()
			// ev02 대상만 대상: 짝(ev01)이 BM 물리 코어 이상을 쓰는 동안은
			// 워커 슬롯을 잡지 않고 기다린다(아래 참고).
			waitForPairIdle(ctx, locations, pairHost, hasPair, coreCount, guestUser, guestPass)
			sem <- struct{}{}
			defer func() { <-sem }()
			runOne(ctx, loc.client, loc.vm, e, guestUser, guestPass, sv)
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

// waitForPairIdle 은 ev02 대상이 워커 슬롯을 배정받기 직전, 짝(ev01)의 부하를
// 딱 한 번 확인합니다. ev01 이 BM 물리 코어 이상의 load average 를 쓰고 있으면
// 워커 슬롯을 잡지 않은 채(다른 대상이 그 슬롯을 바로 쓸 수 있게) 2분 뒤 다시
// 확인하고, 그 재확인이 곧 "배정 직전 확인"이 되어 통과하면 바로 워커를 잡는다.
// 짝을 못 찾았거나 BM 물리 코어 수를 못 읽었으면(coreCount<=0) 미루지 않고
// 그냥 진행한다 — 이 스케줄링은 성능 최적화일 뿐이라, 알 수 없는 상태 때문에
// 대상 처리 자체를 막지는 않는다.
func waitForPairIdle(ctx context.Context, locations map[string]located, pairHost string, hasPair bool, coreCount int, guestUser, guestPass string) {
	if !hasPair || coreCount <= 0 {
		return
	}
	pairLoc, ok := locations[pairHost]
	if !ok {
		return
	}
	for {
		load, err := vsphere.LoadAverage1(ctx, pairLoc.client, pairLoc.vm.Reference(), guestUser, guestPass)
		if err != nil || load < float64(coreCount) {
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(pairRecheckInterval):
		}
	}
}

// runOne 은 한 VM 의 3단계(연결 확인 -> IP 설정 적용 -> 연결 재기동)를 순서대로
// 실행합니다.
func runOne(ctx context.Context, c *govmomi.Client, vm mo.VirtualMachine, e target.Entry, guestUser, guestPass string, sv *status.VM) {
	id := target.SanitizeID(e.Hostname)
	gw := target.Gateway(e.NewIP)

	sv.SetPhase(status.PhaseChecking)
	if err := vsphere.CheckConnection(ctx, c, vm.Reference(), guestUser, guestPass, id); err != nil {
		sv.Finish(status.OutcomeFailed, err)
		return
	}

	sv.SetPhase(status.PhaseApplying)
	if err := vsphere.ApplyIP(ctx, c, vm.Reference(), guestUser, guestPass, id, e.NewIP, gw); err != nil {
		sv.Finish(status.OutcomeFailed, err)
		return
	}

	sv.SetPhase(status.PhaseRestarting)
	if err := vsphere.RestartConnection(ctx, c, vm.Reference(), guestUser, guestPass, id); err != nil {
		sv.Finish(status.OutcomeFailed, err)
		return
	}

	sv.Finish(status.OutcomeDone, nil)
}
