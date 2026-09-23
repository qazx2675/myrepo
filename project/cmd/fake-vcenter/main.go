// fake-vcenter 는 vm-ip-change 를 실제 vCenter/VM 없이 시험하기 위한 가짜
// vCenter 입니다. govmomi 의 vcsim(simulator)으로 VM 을 원하는 수만큼 만들고,
// 게스트 명령 실행(Guest Operations)만 직접 흉내 냅니다 — 도커나 실제 게스트
// 없이, 명령마다 정해진 시간이 지나면 "끝남"으로 응답합니다.
//
// 시나리오:
//   - 보통 VM: 게스트 명령 1개가 -min ~ -max 사이 임의 시간에 끝남
//   - 느린 VM(-slow %): 게스트 명령 1개가 -slow-delay 걸림 (물리 코어를 다른
//     VM 에 과점유당해 느려진 VM 흉내 — 60초 이후 진행 화면 확인용)
//   - 실패 VM(-fail %): "IP 설정 적용" 단계 명령이 exit 1 로 끝남
//   - 전원 꺼진 VM(-off %), vCenter 에 없는 호스트(-missing 개)
//
// VM 은 물리서버(BM) 하나를 공유하는 두 대씩 짝지어 "host0001ev01"/
// "host0001ev02" 로 이름 붙입니다(실제 명명 규칙과 동일). BM 은 같은 이름
// ("host0001")의 ESXi 호스트(HostSystem)로 만들고 -cores 개 물리 코어를 줍니다.
// -busy-ev01 %: 그 중 일부 짝의 ev01 이 BM 물리 코어 이상의 load average 를 쓰는
// 것으로 흉내 냅니다 — vm-ip-change 가 그 짝의 ev02 대상을 후순위로 미루고 2분마다
// 재확인하는지 보는 시나리오용입니다. -busy-ev01-for 를 0(기본값)으로 두면 부하가
// 고정이라 절대 안 풀립니다(도중에 상태를 보려면 vm-ip-change 목록 화면에서 's' 로
// 저장하거나 Ctrl+C 로 중단). 양수를 주면 그 시간이 지난 뒤 부하가 내려가므로,
// 다음 2분 재확인에서 그 ev02 대상의 우선순위가 다시 올라가 처리되는 것까지
// 실제로 볼 수 있습니다.
//
// 시작하면 -out 폴더에 vcenter.txt/list.txt 를 만들고, 종료(Ctrl+C) 시 각 VM
// 에 실제로 어떤 IP 가 적용됐는지 검증 결과를 출력하고 report.txt 로 저장합니다.
package main

import (
	"crypto/tls"
	"flag"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/vmware/govmomi/simulator"
	"github.com/vmware/govmomi/vim25/methods"
	"github.com/vmware/govmomi/vim25/mo"
	"github.com/vmware/govmomi/vim25/soap"
	"github.com/vmware/govmomi/vim25/types"
)

const (
	vcUser    = "administrator@vsphere.local"
	vcPass    = "VMware1!"
	guestUser = "root"
	guestPass = "guestpass"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:18443", "가짜 vCenter 가 들을 주소(host:port)")
	n := flag.Int("vms", 50, "가짜 VM 수")
	out := flag.String("out", "testrun", "vcenter.txt/list.txt/report.txt 를 둘 폴더")
	minD := flag.Duration("min", time.Second, "보통 VM 의 게스트 명령 1개 최소 소요시간")
	maxD := flag.Duration("max", 4*time.Second, "보통 VM 의 게스트 명령 1개 최대 소요시간")
	slowPct := flag.Int("slow", 10, "느린 VM 비율(%)")
	slowD := flag.Duration("slow-delay", 40*time.Second, "느린 VM 의 게스트 명령 1개 소요시간")
	failPct := flag.Int("fail", 5, "IP 설정 적용 단계에서 실패하는 VM 비율(%)")
	offPct := flag.Int("off", 0, "전원 꺼진 VM 비율(%)")
	missing := flag.Int("missing", 0, "list.txt 에만 넣고 vCenter 에는 없는 호스트 수")
	seed := flag.Int64("seed", 1, "난수 시드(같은 값이면 같은 VM 이 느림/실패로 뽑힘)")
	cores := flag.Int("cores", 8, "BM(ESXi 호스트) 물리 코어 수")
	busyPct := flag.Int("busy-ev01", 0, "ev01 이 짝의 BM 물리 코어 이상을 쓰는 것으로 흉내 낼 짝 비율(%) — 해당 ev02 대상은 계속 후순위로 밀림")
	busyFor := flag.Duration("busy-ev01-for", 0, "그 ev01 들이 과점유 상태로 있을 시간(0 이면 영원히 — 기본값). 지나면 부하가 내려가 다음 재확인(2분 주기)에서 그 ev02 짝의 우선순위가 다시 올라가는 걸 볼 수 있음")
	pairMode := flag.String("pair-mode", "both", `list.txt 에 짝(ev01/ev02) 중 어느 쪽을 넣을지: "both"(둘 다, 기본값) / "ev02-only"(ev02 만 — ev01 은 vCenter 에는 있지만 변경 대상 아님) / "ev01-only"(ev01 만 — ev02 자체가 없음)`)
	flag.Parse()

	if *n < 1 || *minD > *maxD {
		log.Fatal("-vms 는 1 이상, -min 은 -max 이하여야 합니다")
	}
	switch *pairMode {
	case "both", "ev02-only", "ev01-only":
	default:
		log.Fatalf("-pair-mode 는 both/ev02-only/ev01-only 중 하나여야 합니다: %q", *pairMode)
	}
	rnd := rand.New(rand.NewSource(*seed))

	// "ev01-only" 는 ev02 짝 자체가 없는 시나리오라 VM 마다 BM 하나를 혼자 쓴다.
	// 그 외("both"/"ev02-only")는 ev01+ev02 가 BM 하나를 공유한다(list.txt 에
	// 어느 쪽을 넣을지만 다르다 — 아래 참고).
	perBM := 2
	if *pairMode == "ev01-only" {
		perBM = 1
	}
	numPairs := (*n + perBM - 1) / perBM

	m := simulator.VPX()
	m.Datacenter = 1
	m.Host = 0
	m.Cluster = 1
	m.ClusterHost = numPairs
	m.Machine = *n
	if err := m.Create(); err != nil {
		log.Fatalf("시뮬레이터 생성 실패: %v", err)
	}
	defer m.Remove()

	st := &state{procs: map[int64]*proc{}, vms: map[string]*vmState{}, rnd: rnd,
		minD: *minD, maxD: *maxD, slowD: *slowD, cores: *cores,
		bootedAt: time.Now(), busyFor: *busyFor}

	// VM 이름을 test-vm0001.. 대신 host0001ev01/host0001ev02.. 로 바꾼다(짝 VM 이
	// BM 하나를 공유하는 실제 명명 규칙과 동일). 시나리오(느림/실패/전원꺼짐)도
	// 여기서 배정한다.
	reg := m.Map()
	var vms []*simulator.VirtualMachine
	for _, e := range reg.All("VirtualMachine") {
		vms = append(vms, e.(*simulator.VirtualMachine))
	}
	sort.Slice(vms, func(i, j int) bool { return vmIndex(vms[i].Name) < vmIndex(vms[j].Name) })

	var hosts []*simulator.HostSystem
	for _, e := range reg.All("HostSystem") {
		hosts = append(hosts, e.(*simulator.HostSystem))
	}
	sort.Slice(hosts, func(i, j int) bool { return hosts[i].Name < hosts[j].Name })

	var list []string
	for i, vm := range vms {
		pair := i / perBM
		bmName := fmt.Sprintf("host%04d", pair+1)
		suffix := "ev01"
		if perBM == 2 && i%perBM == 1 {
			suffix = "ev02"
		}
		name := bmName + suffix
		vm.Name = name
		if vm.Config != nil {
			vm.Config.Name = name
		}
		if vm.Guest == nil {
			vm.Guest = new(types.GuestInfo)
		}
		vm.Guest.ToolsRunningStatus = string(types.VirtualMachineToolsRunningStatusGuestToolsRunning)
		vm.Summary.Guest = &types.VirtualMachineGuestSummary{ToolsRunningStatus: vm.Guest.ToolsRunningStatus}

		vs := &vmState{name: name, expect: testIP(i)}
		switch r := rnd.Intn(100); {
		case r < *offPct:
			vs.off = true
			vm.Runtime.PowerState = types.VirtualMachinePowerStatePoweredOff
			vm.Summary.Runtime.PowerState = types.VirtualMachinePowerStatePoweredOff
		case r < *offPct+*failPct:
			vs.fail = true
		case r < *offPct+*failPct+*slowPct:
			vs.slow = true
		}
		if suffix == "ev01" && perBM == 2 && rnd.Intn(100) < *busyPct {
			vs.busy = true
		}
		st.vms[vm.Reference().Value] = vs

		// "ev02-only" 는 list.txt(변경 대상)에 ev02 만 넣는다 — ev01 은 vCenter
		// 인벤토리엔 있지만(짝 부하 확인용) 그 자신은 변경 대상이 아닌, 실제로
		// 가장 흔한 사용 형태다.
		if *pairMode != "ev02-only" || suffix == "ev02" {
			list = append(list, name+" "+vs.expect)
		}
	}
	for i := 0; i < *missing; i++ {
		list = append(list, fmt.Sprintf("missing-vm%02d 10.99.0.%d", i+1, i+2))
	}

	// BM(ESXi 호스트)도 같은 이름 규칙("host0001")으로 바꾸고 물리 코어 수를 준다.
	// vm-ip-change 는 실행 중인 호스트가 아니라 이름으로만 BM 을 찾으므로(실제
	// 배치와 무관), VM 과 이름만 맞으면 된다.
	for i, h := range hosts {
		if i >= numPairs {
			break
		}
		h.Name = fmt.Sprintf("host%04d", i+1)
		if h.Summary.Hardware == nil {
			h.Summary.Hardware = new(types.HostHardwareSummary)
		}
		h.Summary.Hardware.NumCpuCores = int16(*cores)
	}

	// 게스트 명령 실행기를 가짜로 바꿔 끼운다(원래는 docker exec 로 동작).
	pmRef := types.ManagedObjectReference{Type: "GuestProcessManager", Value: "guestOperationsProcessManager"}
	if reg.Get(pmRef) == nil {
		log.Fatal("시뮬레이터에 GuestProcessManager 가 없습니다")
	}
	reg.Put(&procMgr{GuestProcessManager: mo.GuestProcessManager{Self: pmRef}, st: st})

	// uptime 결과(부하평균) 다운로드도 가짜로 처리한다: InitiateFileTransferFromGuest 는
	// 실제 파일 대신 /fake-loadavg 로 리다이렉트하고, 그 경로는 아래에서 대상 VM 의
	// vs.busy 여부만 보고 load average 문자열을 즉석에서 만들어 돌려준다.
	fmRef := types.ManagedObjectReference{Type: "GuestFileManager", Value: "guestOperationsFileManager"}
	if reg.Get(fmRef) == nil {
		log.Fatal("시뮬레이터에 GuestFileManager 가 없습니다")
	}
	reg.Put(&fileMgr{GuestFileManager: mo.GuestFileManager{Self: fmRef}, st: st, addr: *addr})

	m.Service.Listen = &url.URL{Host: *addr, User: url.UserPassword(vcUser, vcPass)}
	m.Service.TLS = new(tls.Config)
	m.Service.HandleFunc("/fake-loadavg", st.serveLoadAverage)
	s := m.Service.NewServer()
	defer s.Close()

	if err := os.MkdirAll(*out, 0o755); err != nil {
		log.Fatal(err)
	}
	writeFile(filepath.Join(*out, "vcenter.txt"), s.URL.Host+"\n")
	writeFile(filepath.Join(*out, "list.txt"), strings.Join(list, "\n")+"\n")

	fmt.Printf("가짜 vCenter 실행 중: https://%s/sdk  (VM %d대, BM %d대, BM당 물리코어 %d, pair-mode %s)\n", s.URL.Host, len(vms), numPairs, *cores, *pairMode)
	fmt.Printf("  느린 VM %d대(명령당 %s), 실패 VM %d대, 전원꺼짐 %d대, 없는 호스트 %d개, 부하 과점유 ev01 %d대\n",
		st.count(func(v *vmState) bool { return v.slow }), *slowD,
		st.count(func(v *vmState) bool { return v.fail }),
		st.count(func(v *vmState) bool { return v.off }), *missing,
		st.count(func(v *vmState) bool { return v.busy }))
	fmt.Printf("  입력 파일: %s, %s\n", filepath.Join(*out, "vcenter.txt"), filepath.Join(*out, "list.txt"))
	fmt.Println("\n다른 터미널에서 아래를 붙여넣고 vm-ip-change 를 실행하세요:")
	fmt.Printf("  export VC_USER='%s' VC_PASSWORD='%s' GUEST_USER='%s' GUEST_PASSWORD='%s'\n",
		vcUser, vcPass, guestUser, guestPass)
	fmt.Printf("  ./bin/vm-ip-change -vcenter %s -list %s\n",
		filepath.Join(*out, "vcenter.txt"), filepath.Join(*out, "list.txt"))
	fmt.Println("\n시험이 끝나면 이 터미널에서 Ctrl+C → 검증 결과 출력")

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig

	rep := st.report()
	fmt.Print("\n" + rep)
	writeFile(filepath.Join(*out, "report.txt"), rep)
	fmt.Printf("(저장: %s)\n", filepath.Join(*out, "report.txt"))
}

// testIP 는 i 번째 VM 에 줄 새 IP 입니다(10.10.<i/250>.<i%250+2>).
func testIP(i int) string { return fmt.Sprintf("10.10.%d.%d", i/250, i%250+2) }

// vmIndex 는 vcsim 기본 이름(DC0_C0_RP0_VM12)의 끝 숫자입니다.
func vmIndex(name string) int {
	i := strings.LastIndex(name, "VM")
	n, _ := strconv.Atoi(name[i+2:])
	return n
}

func writeFile(path, s string) {
	if err := os.WriteFile(path, []byte(s), 0o644); err != nil {
		log.Fatal(err)
	}
}

// ---- 가짜 게스트 명령 실행기 ----

type vmState struct {
	name, expect          string
	slow, fail, off, busy bool
	applied               string // "IP 설정 적용"으로 설정된 IP
	up                    bool   // "연결 재기동"까지 끝났는지
	calls, failCalls      int
}

type proc struct {
	vm        string
	stage     string
	ip        string
	start     time.Time
	end       time.Time
	exit      int32
	committed bool
}

type state struct {
	mu    sync.Mutex
	next  int64
	procs map[int64]*proc
	vms   map[string]*vmState // key: VM moref

	rnd               *rand.Rand
	minD, maxD, slowD time.Duration
	cores             int

	bootedAt time.Time // 이 값 기준으로 busyFor 가 지났는지 판단(0 이면 영원히 과점유)
	busyFor  time.Duration
}

var ipRe = regexp.MustCompile(`ipv4\.addresses ([0-9.]+)/24`)

// stageOf 는 vm-ip-change 가 보낸 게스트 스크립트가 어느 단계인지 판별합니다.
// "연결 확인" 스크립트는 되돌리기 스크립트 본문(nmcli con up 포함)을 품고 있으므로
// 가장 먼저 걸러야 한다.
func stageOf(script string) string {
	switch {
	case strings.Contains(script, "uptime >"):
		return "uptime"
	case strings.Contains(script, "con show --active"):
		return "check"
	case strings.Contains(script, "ipv4.method manual"):
		return "apply"
	case strings.Contains(script, "nmcli con up"):
		return "up"
	default:
		return "other"
	}
}

func (s *state) start(vmRef, script string) int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	vs := s.vms[vmRef]
	stage := stageOf(script)
	d := s.minD
	if s.maxD > s.minD {
		d += time.Duration(s.rnd.Int63n(int64(s.maxD - s.minD)))
	}
	if vs != nil && vs.slow {
		d = s.slowD
	}
	if stage == "uptime" {
		d = 200 * time.Millisecond // uptime 자체는 가볍다 — 느린 VM 시나리오와 무관
	}
	p := &proc{vm: vmRef, stage: stage, start: time.Now()}
	p.end = p.start.Add(d)
	if m := ipRe.FindStringSubmatch(script); m != nil {
		p.ip = m[1]
	}
	if vs != nil {
		if stage != "uptime" {
			vs.calls++ // uptime 폴링은 이 VM 자신의 IP 변경 진행 여부와 무관하므로 계산에서 뺀다
		}
		if vs.fail && stage == "apply" {
			p.exit = 1
		}
	}
	s.next++
	s.procs[s.next] = p
	return s.next
}

// serveLoadAverage 는 vm-ip-change 의 LoadAverage1 이 내려받는 "uptime" 결과
// 파일을 가짜로 돌려준다. vm-ip-change 가 실제로 파일을 만들지 않으므로(게스트
// 명령 자체를 실행하지 않음), 요청받은 VM 의 vs.busy 여부(+ busyFor 로 정한
// 과점유 지속시간)만 보고 즉석에서 load average 문자열을 만든다.
func (s *state) serveLoadAverage(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	vs := s.vms[r.URL.Query().Get("vm")]
	cores := s.cores
	busyNow := vs != nil && vs.busy && (s.busyFor <= 0 || time.Since(s.bootedAt) < s.busyFor)
	s.mu.Unlock()

	load := 0.10
	if busyNow {
		load = float64(cores) * 2 // 물리 코어의 두 배 — 항상 "과점유"로 판정되게
	}
	fmt.Fprintf(w, " 12:00:00 up 1 day,  1 user,  load average: %.2f, %.2f, %.2f\n", load, load, load)
}

// commit 은 끝난 게스트 명령의 효과(IP 설정/연결 재기동)를 VM 상태에 반영합니다.
// 호출자가 s.mu 를 잡고 있어야 합니다.
func (s *state) commit(now time.Time) {
	for _, p := range s.procs {
		if p.committed || now.Before(p.end) {
			continue
		}
		p.committed = true
		vs := s.vms[p.vm]
		if vs == nil {
			continue
		}
		if p.exit != 0 {
			vs.failCalls++
			continue
		}
		switch p.stage {
		case "apply":
			vs.applied = p.ip
		case "up":
			vs.up = true
		}
	}
}

func (s *state) count(f func(*vmState) bool) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, v := range s.vms {
		if f(v) {
			n++
		}
	}
	return n
}

func (s *state) report() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.commit(time.Now())

	var vs []*vmState
	for _, v := range s.vms {
		vs = append(vs, v)
	}
	sort.Slice(vs, func(i, j int) bool { return vs[i].name < vs[j].name })

	var ok, half, wrong, failed, off, checkedOnly []string
	untouched := 0
	for _, v := range vs {
		switch {
		case v.off:
			off = append(off, v.name)
		case v.applied != "" && v.applied != v.expect:
			wrong = append(wrong, fmt.Sprintf("%s(기대 %s, 실제 %s)", v.name, v.expect, v.applied))
		case v.applied != "" && v.up:
			ok = append(ok, v.name)
		case v.applied != "":
			half = append(half, v.name)
		case v.failCalls > 0:
			failed = append(failed, v.name)
		case v.calls > 0:
			checkedOnly = append(checkedOnly, v.name)
		default:
			untouched++
		}
	}

	var b strings.Builder
	b.WriteString("=== fake-vcenter 검증 결과 ===\n")
	fmt.Fprintf(&b, "정상 변경(IP 일치 + 연결 재기동): %d\n", len(ok))
	line := func(title string, xs []string) {
		fmt.Fprintf(&b, "%s: %d\n", title, len(xs))
		for _, x := range xs {
			fmt.Fprintf(&b, "  %s\n", x)
		}
	}
	line("IP 가 기대값과 다름(있으면 버그)", wrong)
	line("IP 만 설정되고 재기동 안 됨(도중 중단 — revert.sh 로 복구 대상)", half)
	line("실패로 설정 안 됨(시나리오상 실패 VM)", failed)
	line("연결 확인만 하고 중단(변경 없음)", checkedOnly)
	fmt.Fprintf(&b, "전원 꺼짐(시나리오): %d\n", len(off))
	fmt.Fprintf(&b, "게스트 명령을 한 번도 안 받은 VM(시작 안 함): %d\n", untouched)
	b.WriteString("※ 게스트 명령은 vm-ip-change 가 종료돼도 게스트 안에서 끝까지 실행되므로,\n" +
		"  종료 직후 '실행 중'이던 명령도 끝난 것으로 반영돼 있습니다(실제 VM 과 동일).\n")
	return b.String()
}

type procMgr struct {
	mo.GuestProcessManager
	st *state
}

func (m *procMgr) StartProgramInGuest(ctx *simulator.Context, req *types.StartProgramInGuest) soap.HasFault {
	body := new(methods.StartProgramInGuestBody)
	auth, ok := req.Auth.(*types.NamePasswordAuthentication)
	if !ok || auth.Username != guestUser || auth.Password != guestPass {
		body.Fault_ = simulator.Fault("게스트 로그인 실패", new(types.InvalidGuestLogin))
		return body
	}
	spec := req.Spec.(*types.GuestProgramSpec)
	body.Res = &types.StartProgramInGuestResponse{Returnval: m.st.start(req.Vm.Value, spec.Arguments)}
	return body
}

func (m *procMgr) ListProcessesInGuest(ctx *simulator.Context, req *types.ListProcessesInGuest) soap.HasFault {
	body := &methods.ListProcessesInGuestBody{Res: new(types.ListProcessesInGuestResponse)}
	m.st.mu.Lock()
	defer m.st.mu.Unlock()
	now := time.Now()
	m.st.commit(now)
	for _, pid := range req.Pids {
		p, ok := m.st.procs[pid]
		if !ok || p.vm != req.Vm.Value {
			continue
		}
		info := types.GuestProcessInfo{Name: "bash", Pid: pid, Owner: guestUser, StartTime: p.start}
		if !now.Before(p.end) {
			end := p.end
			info.EndTime = &end
			info.ExitCode = p.exit
		}
		body.Res.Returnval = append(body.Res.Returnval, info)
	}
	return body
}

// fileMgr 는 GuestFileManager 를 가짜로 바꿔 끼운 것입니다. LoadAverage1 이 uptime
// 결과를 내려받으려고 부르는 InitiateFileTransferFromGuest 만 처리하면 되므로,
// 실제 게스트 파일시스템 대신 /fake-loadavg(state.serveLoadAverage) 로 보낸다.
type fileMgr struct {
	mo.GuestFileManager
	st   *state
	addr string
}

func (m *fileMgr) InitiateFileTransferFromGuest(ctx *simulator.Context, req *types.InitiateFileTransferFromGuest) soap.HasFault {
	body := &methods.InitiateFileTransferFromGuestBody{
		Res: &types.InitiateFileTransferFromGuestResponse{
			Returnval: types.FileTransferInformation{
				Url: fmt.Sprintf("https://%s/fake-loadavg?vm=%s", m.addr, req.Vm.Value),
			},
		},
	}
	return body
}
