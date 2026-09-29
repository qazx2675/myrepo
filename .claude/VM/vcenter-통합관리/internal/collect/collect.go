// Package collect 는 vCenter 1대에서 인벤토리를 읽어 DATA_SCHEMA.md 형식으로 만든다.
package collect

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/vmware/govmomi"
	"github.com/vmware/govmomi/property"
	"github.com/vmware/govmomi/view"
	"github.com/vmware/govmomi/vim25/mo"
	"github.com/vmware/govmomi/vim25/types"

	"vcportal/internal/conf"
)

type Counts struct {
	Hosts    int `json:"hosts"`
	VMs      int `json:"vms"`
	Clusters int `json:"clusters"`
}

// Result 는 vCenter 1대의 수집 결과. 그대로 캐시 파일로도 저장된다.
type Result struct {
	InstanceUUID string          `json:"instanceUuid"`
	Version      string          `json:"version"`
	Build        string          `json:"build"`
	CollectedAt  string          `json:"collectedAt"`
	Counts       Counts          `json:"counts"`
	Index        [][]string      `json:"index"`  // index.js 행
	Detail       json.RawMessage `json:"detail"` // data/<id>.js 의 JSON 부분
}

// Connect 는 vCenter 에 로그인한다(자체 서명 인증서 허용).
func Connect(ctx context.Context, vc conf.VCenter, user, pass string) (*govmomi.Client, error) {
	u, err := url.Parse(vc.URL)
	if err != nil {
		return nil, err
	}
	u.Path = "/sdk"
	u.User = url.UserPassword(user, pass)
	return govmomi.NewClient(ctx, u, true)
}

// Logout 은 ctx 가 이미 만료되었어도 시도할 수 있도록 별도 타임아웃을 쓴다.
func Logout(c *govmomi.Client) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = c.Logout(ctx)
}

var viewKinds = []string{"Folder", "Datacenter", "ComputeResource", "ClusterComputeResource", "HostSystem", "VirtualMachine", "Datastore", "Network"}

var propSpecs = []types.PropertySpec{
	{Type: "Folder", PathSet: []string{"name", "childEntity"}},
	{Type: "Datacenter", PathSet: []string{"name", "hostFolder"}},
	{Type: "ComputeResource", PathSet: []string{"name", "host"}},
	{Type: "ClusterComputeResource", PathSet: []string{"overallStatus", "summary", "configurationEx", "datastore", "network"}},
	{Type: "HostSystem", PathSet: []string{"name", "overallStatus", "runtime.connectionState", "runtime.powerState",
		"runtime.inMaintenanceMode", "runtime.bootTime", "summary.hardware", "summary.quickStats",
		"summary.config.product", "datastore", "network"}},
	{Type: "VirtualMachine", PathSet: []string{"name", "runtime.host", "runtime.powerState", "runtime.connectionState",
		"overallStatus", "config.template", "config.guestFullName", "config.guestId", "config.version", "config.annotation",
		"config.hardware.numCPU", "config.hardware.memoryMB", "config.hardware.device",
		"guest.hostName", "guest.ipAddress", "guest.net", "guest.toolsRunningStatus", "guest.toolsVersionStatus2",
		"guest.toolsVersion", "summary.quickStats", "summary.storage", "datastore", "network"}},
	{Type: "Datastore", PathSet: []string{"name", "summary"}},
	{Type: "Network", PathSet: []string{"name"}},
	{Type: "DistributedVirtualPortgroup", PathSet: []string{"key"}},
}

// Collect 는 vCenter 1대를 수집한다.
func Collect(ctx context.Context, vc conf.VCenter, user, pass string, name string) (*Result, error) {
	c, err := Connect(ctx, vc, user, pass)
	if err != nil {
		return nil, fmt.Errorf("로그인 실패: %w", err)
	}
	defer Logout(c) // 로그아웃 시 세션의 view 도 함께 정리된다.

	about := c.ServiceContent.About
	root := c.ServiceContent.RootFolder

	cv, err := view.NewManager(c.Client).CreateContainerView(ctx, root, viewKinds, true)
	if err != nil {
		return nil, fmt.Errorf("ContainerView 생성 실패: %w", err)
	}
	req := types.RetrieveProperties{SpecSet: []types.PropertyFilterSpec{{
		PropSet: propSpecs,
		ObjectSet: []types.ObjectSpec{{
			Obj:       cv.Reference(),
			Skip:      types.NewBool(true),
			SelectSet: []types.BaseSelectionSpec{&types.TraversalSpec{Type: "ContainerView", Path: "view"}},
		}},
	}}}
	pc := property.DefaultCollector(c.Client)
	res, err := pc.RetrieveProperties(ctx, req, 500)
	if err != nil {
		return nil, fmt.Errorf("속성 조회 실패: %w", err)
	}
	var rootFolder mo.Folder
	if err := pc.RetrieveOne(ctx, root, []string{"childEntity"}, &rootFolder); err != nil {
		return nil, fmt.Errorf("rootFolder 조회 실패: %w", err)
	}

	b := newBuilder(vc.ID)
	if err := b.load(res.Returnval); err != nil {
		return nil, err
	}
	b.hidden[root.Value] = true
	rootChildren := b.sortIDs(b.walkAll(rootFolder.ChildEntity, ""))

	data := map[string]any{
		"id": vc.ID, "name": name, "url": vc.URL,
		"instanceUuid": about.InstanceUuid, "version": about.Version, "build": about.Build,
		"collectedAt":  time.Now().Format(time.RFC3339),
		"rootChildren": rootChildren,
		"objects":      b.objects,
		"datastores":   b.datastores,
		"networks":     b.networks,
	}
	detail, err := json.Marshal(data)
	if err != nil {
		return nil, err
	}
	return &Result{
		InstanceUUID: about.InstanceUuid, Version: about.Version, Build: about.Build,
		CollectedAt: data["collectedAt"].(string),
		Counts:      b.counts, Index: b.index, Detail: detail,
	}, nil
}

type builder struct {
	folders   map[string]mo.Folder
	dcs       map[string]mo.Datacenter
	crs       map[string]mo.ComputeResource
	clusters  map[string]mo.ClusterComputeResource
	hosts     map[string]mo.HostSystem
	vmsByHost map[string][]mo.VirtualMachine
	dvKey     map[string]string // dvPortgroup key -> 이름
	hidden    map[string]bool   // 트리에서 숨길 폴더

	objects    map[string]map[string]any
	datastores map[string]any
	networks   map[string]any
	index      [][]string
	vcID       string
	counts     Counts
}

func newBuilder(vcID string) *builder {
	return &builder{vcID: vcID,
		folders: map[string]mo.Folder{}, dcs: map[string]mo.Datacenter{}, crs: map[string]mo.ComputeResource{},
		clusters: map[string]mo.ClusterComputeResource{}, hosts: map[string]mo.HostSystem{},
		vmsByHost: map[string][]mo.VirtualMachine{}, dvKey: map[string]string{}, hidden: map[string]bool{},
		objects: map[string]map[string]any{}, datastores: map[string]any{}, networks: map[string]any{},
	}
}

func (b *builder) load(contents []types.ObjectContent) error {
	for _, oc := range contents {
		o, err := mo.ObjectContentToType(oc)
		if err != nil {
			return fmt.Errorf("속성 해석 실패(%s): %w", oc.Obj, err)
		}
		switch v := o.(type) {
		case mo.Folder:
			b.folders[v.Self.Value] = v
		case mo.Datacenter:
			b.dcs[v.Self.Value] = v
			b.hidden[v.HostFolder.Value] = true
		case mo.ClusterComputeResource:
			b.clusters[v.Self.Value] = v
		case mo.ComputeResource:
			b.crs[v.Self.Value] = v
		case mo.HostSystem:
			b.hosts[v.Self.Value] = v
		case mo.VirtualMachine:
			if v.Runtime.Host != nil {
				h := v.Runtime.Host.Value
				b.vmsByHost[h] = append(b.vmsByHost[h], v)
			}
		case mo.Datastore:
			m := map[string]any{"name": v.Name}
			s := v.Summary
			setStr(m, "type", s.Type)
			m["capacity"] = s.Capacity
			m["free"] = s.FreeSpace
			b.datastores[v.Self.Value] = m
		case mo.DistributedVirtualPortgroup:
			b.dvKey[v.Key] = v.Name
			b.networks[v.Self.Value] = map[string]any{"name": v.Name}
		case mo.Network:
			b.networks[v.Self.Value] = map[string]any{"name": v.Name}
		}
	}
	return nil
}

func setStr(m map[string]any, k, v string) {
	if v != "" {
		m[k] = v
	}
}

func ids(refs []types.ManagedObjectReference) []string {
	out := []string{}
	for _, r := range refs {
		out = append(out, r.Value)
	}
	return out
}

func (b *builder) walkAll(refs []types.ManagedObjectReference, parent string) []string {
	var out []string
	for _, r := range refs {
		out = append(out, b.walk(r, parent)...)
	}
	return out
}

// walk 는 ref 를 트리에 추가하고 parent 바로 아래에 붙을 id 들을 돌려준다(숨김 노드는 자식이 올라간다).
func (b *builder) walk(ref types.ManagedObjectReference, parent string) []string {
	id := ref.Value
	switch ref.Type {
	case "Folder":
		f, ok := b.folders[id]
		if !ok {
			return nil
		}
		if b.hidden[id] {
			return b.walkAll(f.ChildEntity, parent)
		}
		o := map[string]any{"type": "Folder", "name": f.Name, "parent": parent}
		b.objects[id] = o
		o["children"] = b.sortIDs(b.walkAll(f.ChildEntity, id))
		return []string{id}
	case "Datacenter":
		d, ok := b.dcs[id]
		if !ok {
			return nil
		}
		o := map[string]any{"type": "Datacenter", "name": d.Name, "parent": parent}
		b.objects[id] = o
		o["children"] = b.sortIDs(b.walk(d.HostFolder, id))
		return []string{id}
	case "ClusterComputeResource":
		cl, ok := b.clusters[id]
		if !ok {
			return nil
		}
		return []string{b.cluster(cl, parent)}
	case "ComputeResource":
		cr, ok := b.crs[id]
		if !ok {
			return nil
		}
		return b.walkAll(cr.Host, parent) // 단독 호스트의 ComputeResource 는 숨긴다
	case "HostSystem":
		h, ok := b.hosts[id]
		if !ok {
			return nil
		}
		return []string{b.host(h, parent)}
	}
	return nil
}

func (b *builder) cluster(cl mo.ClusterComputeResource, parent string) string {
	id := cl.Self.Value
	o := map[string]any{"type": "ClusterComputeResource", "name": cl.Name, "parent": parent}
	b.objects[id] = o
	hosts := b.walkAll(cl.Host, id)
	o["children"] = b.sortIDs(hosts)
	var usedCPU, usedMem int64
	for _, h := range hosts {
		usedCPU += toInt64(b.objects[h]["cpuMhzUsed"])
		usedMem += toInt64(b.objects[h]["memUsedMB"])
	}
	if cl.Summary != nil {
		if s := cl.Summary.GetComputeResourceSummary(); s != nil {
			o["numHosts"] = s.NumHosts
			o["numEffectiveHosts"] = s.NumEffectiveHosts
			o["totalCpuMhz"] = s.TotalCpu
			o["totalMemMB"] = s.TotalMemory >> 20
		}
	}
	o["usedCpuMhz"] = usedCPU
	o["usedMemMB"] = usedMem
	if ex, ok := cl.ConfigurationEx.(*types.ClusterConfigInfoEx); ok {
		if ex.DrsConfig.Enabled != nil {
			o["drsEnabled"] = *ex.DrsConfig.Enabled
		}
		if ex.DasConfig.Enabled != nil {
			o["haEnabled"] = *ex.DasConfig.Enabled
		}
	}
	setStr(o, "overallStatus", string(cl.OverallStatus))
	o["datastores"] = ids(cl.Datastore)
	o["networks"] = ids(cl.Network)
	b.counts.Clusters++
	b.index = append(b.index, []string{b.vcID, "C", id, cl.Name, "", "", ""})
	return id
}

func toInt64(v any) int64 {
	switch n := v.(type) {
	case int64:
		return n
	case int32:
		return int64(n)
	case int:
		return int64(n)
	}
	return 0
}

func (b *builder) host(h mo.HostSystem, parent string) string {
	id := h.Self.Value
	o := map[string]any{"type": "HostSystem", "name": h.Name, "parent": parent}
	b.objects[id] = o
	setStr(o, "connectionState", string(h.Runtime.ConnectionState))
	setStr(o, "powerState", string(h.Runtime.PowerState))
	o["inMaintenance"] = h.Runtime.InMaintenanceMode
	setStr(o, "overallStatus", string(h.OverallStatus))
	if hw := h.Summary.Hardware; hw != nil {
		setStr(o, "vendor", hw.Vendor)
		setStr(o, "model", hw.Model)
		setStr(o, "cpuModel", hw.CpuModel)
		o["cpuSockets"] = hw.NumCpuPkgs
		o["cpuCores"] = hw.NumCpuCores
		o["cpuThreads"] = hw.NumCpuThreads
		o["cpuMhzTotal"] = int64(hw.CpuMhz) * int64(hw.NumCpuCores)
		o["memTotalMB"] = hw.MemorySize >> 20
	}
	q := h.Summary.QuickStats
	o["cpuMhzUsed"] = int64(q.OverallCpuUsage)
	o["memUsedMB"] = int64(q.OverallMemoryUsage)
	if p := h.Summary.Config.Product; p != nil {
		setStr(o, "esxVersion", p.Version)
		setStr(o, "esxBuild", p.Build)
		setStr(o, "esxFullName", p.FullName)
	}
	if bt := h.Runtime.BootTime; bt != nil {
		o["bootTime"] = bt.Local().Format(time.RFC3339)
	}
	if q.Uptime > 0 {
		o["uptimeSec"] = q.Uptime
	}
	o["datastores"] = ids(h.Datastore)
	o["networks"] = ids(h.Network)
	b.counts.Hosts++
	b.index = append(b.index, []string{b.vcID, "H", id, h.Name, "", "", ""})

	vms := b.vmsByHost[id]
	sort.Slice(vms, func(i, j int) bool { return vms[i].Name < vms[j].Name })
	children := []string{}
	for _, vm := range vms {
		children = append(children, b.vm(vm, id, h.Name))
	}
	o["children"] = children
	return id
}

func (b *builder) vm(v mo.VirtualMachine, host, hostName string) string {
	id := v.Self.Value
	o := map[string]any{"type": "VirtualMachine", "name": v.Name, "parent": host, "host": host}
	b.objects[id] = o
	setStr(o, "powerState", string(v.Runtime.PowerState))
	setStr(o, "connectionState", string(v.Runtime.ConnectionState))
	setStr(o, "overallStatus", string(v.OverallStatus))
	ips := []string{}
	guestHost := ""
	if g := v.Guest; g != nil {
		guestHost = g.HostName
		ips = guestIPs(g)
		setStr(o, "toolsRunning", g.ToolsRunningStatus)
		setStr(o, "toolsVersionStatus", g.ToolsVersionStatus2)
		setStr(o, "toolsVersion", g.ToolsVersion)
	}
	setStr(o, "guestHostName", guestHost)
	o["ips"] = ips
	disks := []map[string]any{}
	nics := []map[string]any{}
	if cfg := v.Config; cfg != nil {
		o["template"] = cfg.Template
		setStr(o, "guestFullName", cfg.GuestFullName)
		setStr(o, "guestId", cfg.GuestId)
		setStr(o, "hwVersion", cfg.Version)
		o["annotation"] = cfg.Annotation
		o["numCpu"] = cfg.Hardware.NumCPU
		o["memMB"] = cfg.Hardware.MemoryMB
		devs := append([]types.BaseVirtualDevice{}, cfg.Hardware.Device...)
		sort.SliceStable(devs, func(i, j int) bool { return devs[i].GetVirtualDevice().Key < devs[j].GetVirtualDevice().Key })
		for _, d := range devs {
			switch dev := d.(type) {
			case *types.VirtualDisk:
				disks = append(disks, b.disk(dev))
			default:
				if nic, ok := d.(types.BaseVirtualEthernetCard); ok {
					nics = append(nics, b.nic(d, nic.GetVirtualEthernetCard()))
				}
			}
		}
	}
	o["disks"] = disks
	o["nics"] = nics
	q := v.Summary.QuickStats
	o["cpuUsageMhz"] = q.OverallCpuUsage
	o["guestMemUsageMB"] = q.GuestMemoryUsage
	o["hostMemUsageMB"] = q.HostMemoryUsage
	if s := v.Summary.Storage; s != nil {
		o["storageCommitted"] = s.Committed
		o["storageUncommitted"] = s.Uncommitted
	}
	o["datastores"] = ids(v.Datastore)
	o["networks"] = ids(v.Network)
	b.counts.VMs++
	b.index = append(b.index, []string{b.vcID, "V", id, v.Name, strings.Join(ips, " "), hostName, guestHost})
	return id
}

func label(d types.BaseVirtualDevice) string {
	if info := d.GetVirtualDevice().DeviceInfo; info != nil {
		return info.GetDescription().Label
	}
	return ""
}

func (b *builder) disk(d *types.VirtualDisk) map[string]any {
	m := map[string]any{"label": label(d)}
	size := d.CapacityInBytes
	if size == 0 {
		size = d.CapacityInKB * 1024
	}
	m["capacityBytes"] = size
	if fb, ok := d.Backing.(types.BaseVirtualDeviceFileBackingInfo); ok {
		f := fb.GetVirtualDeviceFileBackingInfo()
		setStr(m, "file", f.FileName)
		if f.Datastore != nil {
			m["datastore"] = f.Datastore.Value
		}
	}
	if flat, ok := d.Backing.(*types.VirtualDiskFlatVer2BackingInfo); ok && flat.ThinProvisioned != nil {
		m["thin"] = *flat.ThinProvisioned
	}
	return m
}

func (b *builder) nic(d types.BaseVirtualDevice, c *types.VirtualEthernetCard) map[string]any {
	m := map[string]any{"label": label(d)}
	switch bk := c.Backing.(type) {
	case *types.VirtualEthernetCardNetworkBackingInfo:
		setStr(m, "network", bk.DeviceName)
	case *types.VirtualEthernetCardDistributedVirtualPortBackingInfo:
		n, ok := b.dvKey[bk.Port.PortgroupKey]
		if !ok {
			n = bk.Port.PortgroupKey
		}
		setStr(m, "network", n)
	}
	setStr(m, "mac", c.MacAddress)
	if c.Connectable != nil {
		m["connected"] = c.Connectable.Connected
	}
	return m
}

// guestIPs 는 guest.net 의 IP 를 IPv4 우선으로 중복 없이 모으고, 없으면 guest.ipAddress 를 쓴다.
func guestIPs(g *types.GuestInfo) []string {
	var v4, other []string
	seen := map[string]bool{}
	add := func(s string) {
		if s == "" || seen[s] {
			return
		}
		seen[s] = true
		if ip := net.ParseIP(s); ip != nil && ip.To4() != nil {
			v4 = append(v4, s)
		} else {
			other = append(other, s)
		}
	}
	for _, n := range g.Net {
		for _, s := range n.IpAddress {
			add(s)
		}
	}
	if len(seen) == 0 {
		add(g.IpAddress)
	}
	return append(append([]string{}, v4...), other...)
}

var rank = map[string]int{"Folder": 0, "Datacenter": 0, "ClusterComputeResource": 1, "HostSystem": 2, "VirtualMachine": 3}

// sortIDs 는 폴더 → 클러스터 → 호스트 → VM, 그룹 안에서는 이름 오름차순으로 정렬한다.
func (b *builder) sortIDs(l []string) []string {
	if l == nil {
		return []string{}
	}
	sort.SliceStable(l, func(i, j int) bool {
		a, c := b.objects[l[i]], b.objects[l[j]]
		ra, rc := rank[a["type"].(string)], rank[c["type"].(string)]
		if ra != rc {
			return ra < rc
		}
		return a["name"].(string) < c["name"].(string)
	})
	return l
}
