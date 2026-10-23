package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeOps : vCenter 없이 전원 동작을 흉내 낸다. 호출 순서를 events 에 남긴다.
type fakeOps struct {
	mu        sync.Mutex
	events    []string
	offAfter  map[string]int // ShutdownGuest 후 Refresh N번째에 꺼짐 (없으면 안 꺼짐)
	refreshes map[string]int
	shutdown  map[string]bool
	onErr     map[string]error
}

func newFake() *fakeOps {
	return &fakeOps{offAfter: map[string]int{}, refreshes: map[string]int{}, shutdown: map[string]bool{}, onErr: map[string]error{}}
}

func (f *fakeOps) ev(s string) { f.mu.Lock(); f.events = append(f.events, s); f.mu.Unlock() }

func (f *fakeOps) PowerOn(ctx context.Context, vm *VMInfo) error {
	if e := f.onErr[vm.Name]; e != nil {
		return e
	}
	f.ev("on:" + vm.Name)
	f.mu.Lock()
	f.shutdown[vm.Name] = false // 켜졌으니 이전 종료 요청은 무효
	vm.Power = "poweredOn"
	f.mu.Unlock()
	return nil
}

func (f *fakeOps) ShutdownGuest(ctx context.Context, vm *VMInfo) error {
	f.ev("shutdown:" + vm.Name)
	f.mu.Lock()
	f.shutdown[vm.Name] = true
	f.mu.Unlock()
	return nil
}

func (f *fakeOps) PowerOff(ctx context.Context, vm *VMInfo) error {
	f.ev("poweroff:" + vm.Name)
	f.mu.Lock()
	vm.Power = "poweredOff"
	f.mu.Unlock()
	return nil
}

func (f *fakeOps) Refresh(ctx context.Context, vm *VMInfo) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.refreshes[vm.Name]++
	if f.shutdown[vm.Name] && f.refreshes[vm.Name] >= f.offAfter[vm.Name] {
		vm.Power = "poweredOff"
	}
	return nil
}

func testRunner(f *fakeOps, answers ...string) (*Runner, *bytes.Buffer, *[]string) {
	var out bytes.Buffer
	var prompts []string
	var mu sync.Mutex
	i := 0
	cfg := &Config{Concurrency: 4, ShutdownTimeout: 50 * time.Millisecond, BootTimeout: time.Second,
		BootPollInterval: time.Millisecond, PortCheckTimeout: time.Millisecond}
	r := &Runner{Cfg: cfg, Ops: f, Log: NewRunLog(&out, nil)}
	r.WaitBoot = func(ctx context.Context, vm *VMInfo, d time.Duration) BootResult {
		f.ev("boot:" + vm.Name)
		return BootResult{OK: true, Via: "22 직접"}
	}
	r.Ask = func(ctx context.Context, prompt string) (string, error) {
		mu.Lock()
		defer mu.Unlock()
		prompts = append(prompts, prompt)
		if i >= len(answers) {
			return "", errors.New("EOF")
		}
		a := answers[i]
		i++
		return a, nil
	}
	return r, &out, &prompts
}

func vmsOf(power string, lpage bool, names ...string) []*VMInfo {
	var vs []*VMInfo
	for _, n := range names {
		bm, ev, _ := splitForTest(n)
		vs = append(vs, &VMInfo{Name: n, NameBM: bm, Ev: ev, Power: power, Lpage: lpage, ToolsRunning: true, Heartbeat: "green", IPs: []string{"10.0.0.1"}})
	}
	return vs
}

func splitForTest(n string) (string, int, bool) {
	i := strings.LastIndex(n, "ev")
	var ev int
	fmt.Sscanf(n[i+2:], "%d", &ev)
	return n[:i], ev, true
}

func idx(events []string, s string) int {
	for i, e := range events {
		if e == s {
			return i
		}
	}
	return -1
}

func statusOf(rs []Result, vm string) Result {
	for _, r := range rs {
		if r.VM == vm {
			return r
		}
	}
	return Result{}
}

// 순차 부팅: 같은 ev 번호는 BM 에 상관없이 한 단계. 단계 안은 동시에 PowerOn, 단계 사이는 전부 부팅 완료 후.
func TestOnSeqWavesAcrossBMs(t *testing.T) {
	f := newFake()
	r, _, _ := testRunner(f)
	vms := vmsOf("poweredOff", true, "svr1ev03", "svr1ev01", "svr1ev02", "svr2ev01", "svr2ev02")
	rs := r.RunOnSeq(context.Background(), vms)
	for _, x := range rs {
		if x.Status != StOK {
			t.Fatalf("%+v", x)
		}
	}
	last := func(prefix string) int { // 해당 접두어 이벤트 중 마지막 위치
		m := -1
		for i, e := range f.events {
			if strings.HasPrefix(e, prefix) {
				m = i
			}
		}
		return m
	}
	first := func(prefix string) int {
		for i, e := range f.events {
			if strings.HasPrefix(e, prefix) {
				return i
			}
		}
		return 1 << 30
	}
	// ev01(svr1ev01, svr2ev01) 둘 다 PowerOn 이 어느 부팅 확인보다 먼저 = 동시 요청
	if !(last("on:svr1ev01") < first("boot:") && last("on:svr2ev01") < first("boot:")) {
		t.Fatalf("ev01 은 전부 먼저 켜고 나서 기다려야 함: %v", f.events)
	}
	// ev02 PowerOn 은 ev01 부팅 확인이 모두 끝난 뒤, ev03 은 ev02 부팅 확인이 모두 끝난 뒤
	if !(last("boot:svr1ev01") < first("on:svr1ev02") && last("boot:svr2ev01") < first("on:svr2ev02") &&
		last("boot:svr1ev01") < first("on:svr2ev02") && last("boot:svr2ev01") < first("on:svr1ev02")) {
		t.Fatalf("ev02 는 ev01 전부 부팅된 뒤: %v", f.events)
	}
	if !(last("boot:svr1ev02") < first("on:svr1ev03") && last("boot:svr2ev02") < first("on:svr1ev03")) {
		t.Fatalf("ev03 는 ev02 전부 부팅된 뒤: %v", f.events)
	}
}

func TestOnSeqNoLpageBatch(t *testing.T) {
	f := newFake()
	r, out, _ := testRunner(f)
	vms := vmsOf("poweredOff", false, "svr2ev01", "svr2ev02")
	rs := r.RunOnSeq(context.Background(), vms)
	for _, x := range rs {
		if x.Status != StOK {
			t.Fatalf("%+v", x)
		}
	}
	if idx(f.events, "boot:svr2ev01") >= 0 {
		t.Fatalf("lpage 미사용은 부팅 확인 없이 일괄이어야 함: %v", f.events)
	}
	if !strings.Contains(out.String(), "lpage 미사용 — 한 번에 부팅해도 됩니다") {
		t.Fatalf("안내 메시지 없음: %q", out.String())
	}
}

// 미사용 BM 은 순차 단계를 기다리지 않고 한 번에 켜지고, 사용 BM 의 단계와 섞이지 않는다.
func TestOnSeqMixedLpageAndBatch(t *testing.T) {
	f := newFake()
	r, _, _ := testRunner(f)
	vms := append(vmsOf("poweredOff", true, "svr1ev01", "svr1ev02"), vmsOf("poweredOff", false, "svr9ev01", "svr9ev02")...)
	rs := r.RunOnSeq(context.Background(), vms)
	for _, x := range rs {
		if x.Status != StOK {
			t.Fatalf("%+v", x)
		}
	}
	if idx(f.events, "boot:svr9ev01") >= 0 || idx(f.events, "on:svr9ev02") < 0 {
		t.Fatalf("미사용 BM 은 부팅 확인 없이 전부 켜짐: %v", f.events)
	}
}

func TestOnSeqBootTimeoutSkipQuitContinue(t *testing.T) {
	cases := []struct {
		ans                     string
		ev02Status, other02     string
		ev03Status              string
		ev03Powered, wantSecond bool
	}{
		{"s", StFail, StOK, StOK, true, false},    // 건너뛰고 다음 ev 단계
		{"q", StFail, StOK, StFail, false, false}, // 순차 부팅 중단: ev03 는 켜지지 않음
		{"c", StOK, StOK, StOK, true, true},       // 계속 대기 → 두 번째 대기에서 성공
	}
	for _, c := range cases {
		f := newFake()
		r, _, prompts := testRunner(f, c.ans)
		stuckOnce := true
		r.WaitBoot = func(ctx context.Context, vm *VMInfo, d time.Duration) BootResult {
			f.ev("boot:" + vm.Name)
			if vm.Name == "svr1ev02" && stuckOnce {
				if c.wantSecond {
					stuckOnce = false
				}
				return BootResult{Detail: "Tools: running / 22: 응답없음"}
			}
			return BootResult{OK: true, Via: "22 직접"}
		}
		vms := vmsOf("poweredOff", true, "svr1ev01", "svr1ev02", "svr2ev02", "svr1ev03")
		rs := r.RunOnSeq(context.Background(), vms)
		if len(*prompts) != 1 || !strings.Contains((*prompts)[0], "계속 대기(c)") {
			t.Fatalf("[%s] 질문은 단계당 한 번만: %v", c.ans, *prompts)
		}
		if got := statusOf(rs, "svr1ev02").Status; got != c.ev02Status {
			t.Fatalf("[%s] svr1ev02=%s %+v", c.ans, got, rs)
		}
		if got := statusOf(rs, "svr2ev02").Status; got != c.other02 { // 같은 단계의 다른 VM 은 정상
			t.Fatalf("[%s] svr2ev02=%s %+v", c.ans, got, rs)
		}
		if got := statusOf(rs, "svr1ev03").Status; got != c.ev03Status {
			t.Fatalf("[%s] svr1ev03=%s %+v", c.ans, got, rs)
		}
		if (idx(f.events, "on:svr1ev03") >= 0) != c.ev03Powered {
			t.Fatalf("[%s] svr1ev03 PowerOn 여부: %v", c.ans, f.events)
		}
	}
}

func TestOnSeqPowerOnFailureAsks(t *testing.T) {
	f := newFake()
	f.onErr["svr1ev02"] = errors.New("boom")
	r, _, prompts := testRunner(f, "q")
	vms := vmsOf("poweredOff", true, "svr1ev01", "svr1ev02", "svr1ev03")
	rs := r.RunOnSeq(context.Background(), vms)
	if len(*prompts) != 1 || !strings.Contains((*prompts)[0], "다음 ev 단계로 계속(s)") {
		t.Fatalf("PowerOn 실패 질문: %v", *prompts)
	}
	if statusOf(rs, "svr1ev02").Status != StFail || statusOf(rs, "svr1ev03").Status != StFail {
		t.Fatalf("%+v", rs)
	}
}

func TestOnSeqAlreadyOn(t *testing.T) {
	f := newFake()
	r, _, _ := testRunner(f)
	vms := vmsOf("poweredOn", true, "svr1ev01")
	rs := r.RunOnSeq(context.Background(), vms)
	if rs[0].Status != StSkip || idx(f.events, "on:svr1ev01") >= 0 || idx(f.events, "boot:svr1ev01") < 0 {
		t.Fatalf("이미 켜진 VM 은 PowerOn 없이 부팅 확인만: %+v %v", rs, f.events)
	}
}

func TestOnAll(t *testing.T) {
	f := newFake()
	r, _, _ := testRunner(f)
	vms := append(vmsOf("poweredOff", true, "svr1ev01", "svr1ev02"), vmsOf("poweredOn", false, "svr2ev01")...)
	vms = append(vms, vmsOf("suspended", false, "svr3ev01")...)
	rs := r.RunOnAll(context.Background(), vms)
	if statusOf(rs, "svr1ev01").Status != StOK || statusOf(rs, "svr2ev01").Status != StSkip || statusOf(rs, "svr3ev01").Status != StFail {
		t.Fatalf("%+v", rs)
	}
	for _, e := range f.events {
		if strings.HasPrefix(e, "boot:") {
			t.Fatalf("일괄 On 은 부팅 확인 안 함: %v", f.events)
		}
	}
}

func TestOffGuestShutdown(t *testing.T) {
	f := newFake()
	f.offAfter["svr1ev01"], f.offAfter["svr1ev02"] = 1, 3
	r, _, prompts := testRunner(f)
	vms := vmsOf("poweredOn", true, "svr1ev01", "svr1ev02")
	rs := r.RunOff(context.Background(), vms)
	if statusOf(rs, "svr1ev01").Status != StOK || statusOf(rs, "svr1ev02").Status != StOK || len(*prompts) != 0 {
		t.Fatalf("%+v prompts=%v", rs, *prompts)
	}
}

func TestOffForceOnlyWithYeS(t *testing.T) {
	for _, tc := range []struct {
		ans  string
		want string
	}{{"YeS", StOK}, {"yes", StFail}, {"No", StFail}, {"", StFail}} {
		f := newFake()
		r, out, prompts := testRunner(f, tc.ans)
		vms := vmsOf("poweredOn", true, "svr1ev01", "svr1ev02")
		vms[1].ToolsRunning = false // Tools 없음 → Guest 종료 불가
		f.offAfter["svr1ev01"] = 1
		rs := r.RunOff(context.Background(), vms)
		if statusOf(rs, "svr1ev01").Status != StOK {
			t.Fatalf("정상 종료는 질문 없이 성공: %+v", rs)
		}
		if len(*prompts) != 1 || !strings.Contains((*prompts)[0], "강제 PowerOff 하시겠습니까? (YeS/No): ") ||
			!strings.Contains(out.String(), "VMware Tools 미실행") {
			t.Fatalf("[%s] 질문/목록: %v %q", tc.ans, *prompts, out.String())
		}
		if got := statusOf(rs, "svr1ev02").Status; got != tc.want {
			t.Fatalf("[%s] svr1ev02=%s", tc.ans, got)
		}
		if (idx(f.events, "poweroff:svr1ev02") >= 0) != (tc.want == StOK) {
			t.Fatalf("[%s] 강제 PowerOff 호출 여부: %v", tc.ans, f.events)
		}
	}
}

func TestOffTimeoutToForce(t *testing.T) {
	f := newFake() // ShutdownGuest 는 되지만 안 꺼짐
	f.offAfter["svr1ev01"] = 1 << 30
	r, out, prompts := testRunner(f, "No")
	vms := vmsOf("poweredOn", true, "svr1ev01")
	rs := r.RunOff(context.Background(), vms)
	if rs[0].Status != StFail || len(*prompts) != 1 || !strings.Contains(out.String(), "안에 끝나지 않음") {
		t.Fatalf("%+v %v %q", rs, *prompts, out.String())
	}
}

func TestRebootStopsWhenOffFails(t *testing.T) {
	f := newFake()
	r, _, _ := testRunner(f, "No")
	vms := vmsOf("poweredOn", true, "svr1ev01")
	vms[0].ToolsRunning = false
	rs := r.RunReboot(context.Background(), vms)
	if rs[0].Status != StFail || idx(f.events, "on:svr1ev01") >= 0 {
		t.Fatalf("Off 실패 시 On 으로 가면 안 됨: %+v %v", rs, f.events)
	}
}

func TestRebootOffThenOn(t *testing.T) {
	f := newFake()
	f.offAfter["svr1ev01"] = 1
	r, _, _ := testRunner(f)
	vms := vmsOf("poweredOn", true, "svr1ev01")
	rs := r.RunReboot(context.Background(), vms)
	if len(rs) != 2 || rs[0].Op != "Off" || rs[1].Op != "On" || rs[1].Status != StOK {
		t.Fatalf("%+v", rs)
	}
	if !(idx(f.events, "shutdown:svr1ev01") < idx(f.events, "on:svr1ev01")) {
		t.Fatalf("순서: %v", f.events)
	}
}

// 재부팅은 lpage BM 이어도 순차가 아니라 병렬(부팅 완료 확인 없이 한 번에 On)이어야 한다.
func TestRebootParallelEvenWithLpage(t *testing.T) {
	f := newFake()
	f.offAfter["svr1ev01"], f.offAfter["svr1ev02"], f.offAfter["svr1ev03"] = 1, 1, 1
	r, _, _ := testRunner(f)
	vms := vmsOf("poweredOn", true, "svr1ev01", "svr1ev02", "svr1ev03")
	rs := r.RunReboot(context.Background(), vms)
	if len(rs) != 6 {
		t.Fatalf("%+v", rs)
	}
	for _, e := range f.events {
		if strings.HasPrefix(e, "boot:") {
			t.Fatalf("재부팅은 순차(부팅 완료 확인)를 쓰면 안 됨: %v", f.events)
		}
	}
	// 모든 shutdown 이 모든 on 보다 먼저 (Off 단계가 끝난 뒤 On 일괄)
	lastShutdown, firstOn := -1, 1<<30
	for i, e := range f.events {
		if strings.HasPrefix(e, "shutdown:") {
			lastShutdown = i
		}
		if strings.HasPrefix(e, "on:") && i < firstOn {
			firstOn = i
		}
	}
	if lastShutdown > firstOn {
		t.Fatalf("Off 가 끝나기 전에 On 시작: %v", f.events)
	}
}

// Off 는 대상 전부에 종료 요청을 한꺼번에 보낸다(하나 끝나길 기다리지 않음).
func TestOffRequestsAllBeforeWaiting(t *testing.T) {
	f := newFake()
	for _, n := range []string{"svr1ev01", "svr1ev02", "svr1ev03"} {
		f.offAfter[n] = 1
	}
	r, _, _ := testRunner(f)
	r.Cfg.Concurrency = 3
	vms := vmsOf("poweredOn", true, "svr1ev01", "svr1ev02", "svr1ev03")
	r.RunOff(context.Background(), vms)
	n := 0
	for _, e := range f.events {
		if strings.HasPrefix(e, "shutdown:") {
			n++
		}
	}
	if n != 3 {
		t.Fatalf("%v", f.events)
	}
}

func TestCanceledMarksNotRun(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	f := newFake()
	r, _, _ := testRunner(f)
	rs := r.RunOnAll(ctx, vmsOf("poweredOff", true, "svr1ev01"))
	if rs[0].Status != StFail || rs[0].Note != notRun {
		t.Fatalf("%+v", rs)
	}
}

func TestPrintSummary(t *testing.T) {
	var b bytes.Buffer
	n := PrintSummary(&b, []Result{{VM: "a", Op: "On", Status: StOK, Elapsed: 65 * time.Second}, {VM: "b", Op: "On", Status: StFail, Note: "x"}}, false)
	if n != 1 || !strings.Contains(b.String(), "01:05") || !strings.Contains(b.String(), "성공 1 / 스킵 0 / 실패 1") {
		t.Fatalf("%d %q", n, b.String())
	}
}

func TestRunLogPauseBuffers(t *testing.T) {
	var out bytes.Buffer
	l := NewRunLog(&out, nil)
	l.Pause()
	l.Logf("a", "hello")
	if out.Len() != 0 {
		t.Fatal("Pause 중에는 출력하지 않아야 함")
	}
	l.Resume()
	if !strings.Contains(out.String(), "[a] hello") {
		t.Fatalf("%q", out.String())
	}
}
