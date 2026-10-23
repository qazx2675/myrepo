package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/vmware/govmomi"
	"github.com/vmware/govmomi/object"
	"github.com/vmware/govmomi/property"
	"github.com/vmware/govmomi/session"
	"github.com/vmware/govmomi/view"
	"github.com/vmware/govmomi/vim25"
	"github.com/vmware/govmomi/vim25/mo"
	"github.com/vmware/govmomi/vim25/soap"
	"github.com/vmware/govmomi/vim25/types"
)

// Client 는 vCenter 세션이다.
type Client struct {
	c *govmomi.Client
}

// Govmomi 는 내부 govmomi 클라이언트를 돌려준다.
func (c *Client) Govmomi() *govmomi.Client { return c.c }

// Connect 는 vCenter 에 로그인한다. vcenter 는 host, host:port 또는 http(s)://… 형식.
// 인증서 검증은 끄고(VMsetup 도구와 동일) keepalive 를 켠다.
func Connect(ctx context.Context, vcenter, user, pass string) (*Client, error) {
	raw := strings.TrimSpace(vcenter)
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("vCenter 주소 오류: %w", err)
	}
	if u.Path == "" || u.Path == "/" {
		u.Path = "/sdk"
	}
	u.User = url.UserPassword(user, pass)

	sc := soap.NewClient(u, true)
	vc, err := vim25.NewClient(ctx, sc)
	if err != nil {
		return nil, fmt.Errorf("vCenter 접속 실패: %w", err)
	}
	vc.RoundTripper = session.KeepAlive(vc.RoundTripper, 5*time.Minute)
	gc := &govmomi.Client{Client: vc, SessionManager: session.NewManager(vc)}
	if err := gc.Login(ctx, u.User); err != nil {
		return nil, fmt.Errorf("vCenter 로그인 실패: %w", err)
	}
	return &Client{c: gc}, nil
}

// Close 는 로그아웃한다.
func (c *Client) Close(ctx context.Context) {
	if c == nil || c.c == nil {
		return
	}
	_ = c.c.Logout(ctx)
}

// LookupError 는 이름 조회 실패(없음/중복)를 한꺼번에 담는다.
type LookupError struct {
	Missing   []string
	Duplicate map[string][]string // 이름 -> 경로들
}

func (e *LookupError) Error() string {
	var parts []string
	if len(e.Missing) > 0 {
		parts = append(parts, "vCenter 에 없는 VM: "+strings.Join(e.Missing, ", "))
	}
	var names []string
	for n := range e.Duplicate {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		parts = append(parts, fmt.Sprintf("이름 중복 VM: %s (%s)", n, strings.Join(e.Duplicate[n], " , ")))
	}
	return strings.Join(parts, "; ")
}

var lookupNameRe = regexp.MustCompile(`^([A-Za-z0-9][A-Za-z0-9._-]*)ev([0-9]{2})$`)

// splitVMName 은 svr1ev01 -> (svr1, 1). 규칙에 안 맞으면 ok=false.
func splitVMName(name string) (bm string, ev int, ok bool) {
	m := lookupNameRe.FindStringSubmatch(name)
	if m == nil {
		return "", 0, false
	}
	n, _ := strconv.Atoi(m[2])
	return m[1], n, true
}

var vmProps = []string{
	"name", "parent", "runtime.host", "runtime.powerState", "config.extraConfig",
	"config.hardware.device", "guest.toolsRunningStatus", "guest.net", "guest.ipAddress",
	"guestHeartbeatStatus",
}

var refreshProps = []string{
	"runtime.powerState", "guest.toolsRunningStatus", "guest.net", "guest.ipAddress", "guestHeartbeatStatus",
}

// LookupVMs 는 이름이 정확히 같은 VM 을 찾는다. 없거나 중복이면 전부 모아 *LookupError.
func (c *Client) LookupVMs(ctx context.Context, names []string) ([]*VMInfo, error) {
	vc := c.c.Client
	m := view.NewManager(vc)
	root := vc.ServiceContent.RootFolder

	// 전체 VM 을 한 번에 가져온다.
	all, err := retrieveAll[mo.VirtualMachine](ctx, m, root, "VirtualMachine", vmProps)
	if err != nil {
		return nil, err
	}
	byName := map[string][]*mo.VirtualMachine{}
	for i := range all {
		byName[all[i].Name] = append(byName[all[i].Name], &all[i])
	}

	// 폴더/데이터센터(경로용), 분산 포트그룹(key->이름)도 각각 한 번에.
	folders, err := retrieveAll[mo.Folder](ctx, m, root, "Folder", []string{"name", "parent"})
	if err != nil {
		return nil, err
	}
	dcs, err := retrieveAll[mo.Datacenter](ctx, m, root, "Datacenter", []string{"name", "parent"})
	if err != nil {
		return nil, err
	}
	dvpgs, err := retrieveAll[mo.DistributedVirtualPortgroup](ctx, m, root, "DistributedVirtualPortgroup", []string{"name", "config.key"})
	if err != nil {
		return nil, err
	}
	type node struct {
		name   string
		parent *types.ManagedObjectReference
	}
	nodes := map[types.ManagedObjectReference]node{}
	for _, f := range folders {
		nodes[f.Reference()] = node{f.Name, f.Parent}
	}
	for _, d := range dcs {
		nodes[d.Reference()] = node{d.Name, d.Parent}
	}
	pgKey := map[string]string{}
	for _, p := range dvpgs {
		pgKey[p.Config.Key] = p.Name
	}
	pathOf := func(vm *mo.VirtualMachine) string {
		var parts []string
		for ref := vm.Parent; ref != nil; {
			n, ok := nodes[*ref]
			if !ok || n.parent == nil { // 최상위 root 폴더는 제외
				break
			}
			parts = append([]string{n.name}, parts...)
			ref = n.parent
		}
		return strings.Join(parts, "/")
	}

	// 없음/중복 판정: 전부 모아서 한 번에 반환
	le := &LookupError{Duplicate: map[string][]string{}}
	for _, n := range names {
		switch l := byName[n]; len(l) {
		case 0:
			le.Missing = append(le.Missing, n)
		case 1:
		default:
			if _, seen := le.Duplicate[n]; !seen {
				for _, v := range l {
					le.Duplicate[n] = append(le.Duplicate[n], pathOf(v)+"/"+v.Name)
				}
			}
		}
	}
	if len(le.Missing) > 0 || len(le.Duplicate) > 0 {
		return nil, le
	}

	// 호스트 이름
	hostRefs := map[types.ManagedObjectReference]bool{}
	for _, n := range names {
		if h := byName[n][0].Runtime.Host; h != nil {
			hostRefs[*h] = true
		}
	}
	hostName := map[types.ManagedObjectReference]string{}
	if len(hostRefs) > 0 {
		var refs []types.ManagedObjectReference
		for r := range hostRefs {
			refs = append(refs, r)
		}
		var hs []mo.HostSystem
		if err := property.DefaultCollector(vc).Retrieve(ctx, refs, []string{"name"}, &hs); err != nil {
			return nil, err
		}
		for _, h := range hs {
			hostName[h.Reference()] = h.Name
		}
	}

	out := make([]*VMInfo, 0, len(names))
	for _, n := range names {
		v := byName[n][0]
		info := &VMInfo{
			Name:       v.Name,
			FolderPath: pathOf(v),
			Obj:        object.NewVirtualMachine(vc, v.Reference()),
		}
		if bm, ev, ok := splitVMName(v.Name); ok {
			info.NameBM, info.Ev = bm, ev
		}
		if h := v.Runtime.Host; h != nil {
			info.Host = shortHost(hostName[*h])
		}
		info.PortGroups = portGroups(v, pgKey)
		if v.Config != nil {
			for _, o := range v.Config.ExtraConfig {
				if bv := o.GetOptionValue(); bv != nil && bv.Key == "sched.mem.lpage.enable1GPage" {
					info.Lpage = strings.EqualFold(strings.TrimSpace(fmt.Sprint(bv.Value)), "TRUE")
				}
			}
		}
		applyRuntime(info, v)
		out = append(out, info)
	}
	return out, nil
}

func retrieveAll[T any](ctx context.Context, m *view.Manager, root types.ManagedObjectReference, kind string, props []string) ([]T, error) {
	v, err := m.CreateContainerView(ctx, root, []string{kind}, true)
	if err != nil {
		return nil, err
	}
	defer v.Destroy(ctx)
	var dst []T
	if err := v.Retrieve(ctx, []string{kind}, props, &dst); err != nil {
		return nil, err
	}
	return dst, nil
}

// shortHost 는 FQDN 이면 첫 '.' 앞만 남긴다(IP 는 그대로).
func shortHost(h string) string {
	if i := strings.Index(h, "."); i > 0 && net.ParseIP(h) == nil {
		return h[:i]
	}
	return h
}

func portGroups(v *mo.VirtualMachine, pgKey map[string]string) []string {
	if v.Config == nil {
		return nil
	}
	var out []string
	for _, d := range object.VirtualDeviceList(v.Config.Hardware.Device).SelectByType((*types.VirtualEthernetCard)(nil)) {
		switch b := d.GetVirtualDevice().Backing.(type) {
		case *types.VirtualEthernetCardNetworkBackingInfo:
			if b.DeviceName != "" {
				out = append(out, b.DeviceName)
			}
		case *types.VirtualEthernetCardDistributedVirtualPortBackingInfo:
			key := b.Port.PortgroupKey
			if n, ok := pgKey[key]; ok {
				out = append(out, n)
			} else if key != "" {
				out = append(out, key)
			}
		}
	}
	return out
}

// applyRuntime 은 전원/Tools/heartbeat/IP 를 채운다.
func applyRuntime(info *VMInfo, v *mo.VirtualMachine) {
	info.Power = string(v.Runtime.PowerState)
	info.ToolsRunning = false
	info.IPs = nil
	if g := v.Guest; g != nil {
		info.ToolsRunning = g.ToolsRunningStatus == string(types.VirtualMachineToolsRunningStatusGuestToolsRunning)
		seen := map[string]bool{}
		add := func(s string) {
			ip := net.ParseIP(s)
			if ip == nil || ip.To4() == nil || strings.HasPrefix(s, "169.254.") || seen[s] {
				return
			}
			seen[s] = true
			info.IPs = append(info.IPs, s)
		}
		add(g.IpAddress)
		for _, n := range g.Net {
			for _, s := range n.IpAddress {
				add(s)
			}
		}
	}
	info.Heartbeat = string(v.GuestHeartbeatStatus)
}

// Refresh 는 전원/Tools/heartbeat/IP 만 최신화한다.
func (c *Client) Refresh(ctx context.Context, vm *VMInfo) error {
	if vm == nil || vm.Obj == nil {
		return errors.New("VM 객체 없음")
	}
	var mv mo.VirtualMachine
	if err := vm.Obj.Properties(ctx, vm.Obj.Reference(), refreshProps, &mv); err != nil {
		return err
	}
	applyRuntime(vm, &mv)
	return nil
}

// GetPassword 는 secret_lib 에서 비밀번호를 꺼내고, 실패하면 prompt 로 묻는다.
// (VC_PASSWORD 등 환경변수는 읽지 않는다.)
func GetPassword(cfg *Config, vcenter, id string, prompt func(msg string) (string, error)) (string, error) {
	if cfg != nil && cfg.SecretLib != "" {
		out, err := exec.Command("bash", "-c", `. "$1" && secret_get vcenter "$2"`, "_", cfg.SecretLib, id).Output()
		if err == nil {
			if p := strings.TrimRight(string(out), "\r\n"); p != "" {
				return p, nil
			}
		}
	}
	if prompt == nil {
		return "", errors.New("비밀번호를 얻을 수 없습니다")
	}
	return prompt(fmt.Sprintf("vCenter %s 비밀번호: ", id))
}

func isTerminal(f *os.File) bool {
	st, err := f.Stat()
	return err == nil && st.Mode()&os.ModeCharDevice != 0
}

// ReadHidden 은 비밀번호 한 줄을 읽는다(터미널이면 에코를 끄고, 끝나면/시그널 시 복구).
// 주의: os.Stdin 을 직접 읽는다. 공용 LineReader(ui.go)가 이미 stdin 을 읽고 있는 프로세스에서는 쓰지 말 것
// (main.go 의 hiddenPrompt 를 쓴다). 단독 사용 시험용으로만 남겨 둔다.
func ReadHidden(msg string) (string, error) {
	fmt.Fprint(os.Stderr, msg)
	if isTerminal(os.Stdin) {
		if saved, err := sttyOut("-g"); err == nil && saved != "" && sttyRun("-echo") == nil {
			restore := func() { _ = sttyRun(saved) }
			ch := make(chan os.Signal, 1)
			signal.Notify(ch, os.Interrupt, syscall.SIGTERM)
			defer signal.Stop(ch)
			go func() {
				if _, ok := <-ch; ok {
					restore()
					fmt.Fprintln(os.Stderr)
					os.Exit(1)
				}
			}()
			defer func() { restore(); fmt.Fprintln(os.Stderr) }()
		}
	}
	return readLine(os.Stdin)
}

func sttyRun(args ...string) error {
	cmd := exec.Command("stty", args...)
	cmd.Stdin = os.Stdin
	return cmd.Run()
}

func sttyOut(args ...string) (string, error) {
	cmd := exec.Command("stty", args...)
	cmd.Stdin = os.Stdin
	b, err := cmd.Output()
	return strings.TrimSpace(string(b)), err
}

// readLine 은 한 바이트씩 읽어 줄 끝까지 읽는다(뒤 입력을 삼키지 않음).
func readLine(f *os.File) (string, error) {
	var sb strings.Builder
	buf := make([]byte, 1)
	for {
		n, err := f.Read(buf)
		if n > 0 {
			if buf[0] == '\n' {
				break
			}
			sb.WriteByte(buf[0])
		}
		if err != nil {
			if sb.Len() > 0 {
				break
			}
			return "", err
		}
	}
	return strings.TrimRight(sb.String(), "\r"), nil
}

// LastVCenterFile 은 마지막 선택 vCenter 기록 파일 경로다.
func LastVCenterFile(runDir string) string { return filepath.Join(runDir, "last_vcenter") }

// LoadLastVCenter 는 마지막으로 쓴 vCenter 를 읽는다(없으면 "").
func LoadLastVCenter(runDir string) string {
	b, err := os.ReadFile(LastVCenterFile(runDir))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

// SaveLastVCenter 는 vCenter 를 기록한다(디렉터리 0700, 파일 0600).
func SaveLastVCenter(runDir, vcenter string) error {
	if err := os.MkdirAll(runDir, 0o700); err != nil {
		return err
	}
	return os.WriteFile(LastVCenterFile(runDir), []byte(vcenter+"\n"), 0o600)
}
