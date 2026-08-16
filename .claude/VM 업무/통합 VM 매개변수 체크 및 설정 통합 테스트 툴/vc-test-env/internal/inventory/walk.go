// Package inventory는 vCenter/vcsim(둘 다 같은 API를 노출하므로 동일 코드로 동작)를
// 순회해서 구조 + 추적 필드 값을 뽑아낸다. extract와 tree가 이 결과를 공유한다.
package inventory

import (
	"context"
	"fmt"

	"github.com/vmware/govmomi"
	"github.com/vmware/govmomi/property"
	"github.com/vmware/govmomi/view"
	"github.com/vmware/govmomi/vim25/mo"
	"github.com/vmware/govmomi/vim25/types"

	"vc-test-env/internal/fields"
)

type VM struct {
	Name           string            `json:"name"`
	NumCPU         int32             `json:"numCPU"`
	CoresPerSocket int32             `json:"coresPerSocket"`
	MemoryMB       int32             `json:"memoryMB"`
	DiskGB         []float64         `json:"diskGB,omitempty"`
	Networks       []NetworkAdapter  `json:"networks,omitempty"`
	CPUAffinity    []int32           `json:"cpuAffinity,omitempty"`
	Values         map[string]string `json:"values,omitempty"` // fields.VMFields 결과
}

// NetworkAdapter는 VM에 붙은 네트워크 어댑터 1개 — 포트그룹 이름 + 커넥트/디스커넥트 상태.
// vm-param-check의 model.NetworkAdapter와 같은 구조를 따른다(디스커넥트여도 포트그룹은
// 항상 조사되어야 한다는 요구사항 때문에 이름만이 아니라 상태까지 재현해야 함).
type NetworkAdapter struct {
	Portgroup string `json:"portgroup"`
	Connected bool   `json:"connected"`
}

type Host struct {
	Name        string `json:"name"`
	PowerPolicy string `json:"powerPolicy,omitempty"`
	VMs         []VM   `json:"vms,omitempty"`
}

type Cluster struct {
	Name  string `json:"name"`
	Hosts []Host `json:"hosts,omitempty"`
}

// PortGroup은 1차 범위에선 이름만 기록한다(VLAN ID/멤버 호스트는 확장 대상).
type PortGroup struct {
	Name string `json:"name"`
}

type Datacenter struct {
	Name       string      `json:"name"`
	VMFolders  []string    `json:"vmFolders,omitempty"`
	NetFolders []string    `json:"netFolders,omitempty"`
	Clusters   []Cluster   `json:"clusters,omitempty"`
	Networks   []PortGroup `json:"networks,omitempty"`
}

type Inventory struct {
	Datacenters []Datacenter `json:"datacenters,omitempty"`
}

// Walk은 root(보통 ServiceContent.RootFolder)부터 인벤토리 전체를 순회한다.
// vCenter/vcsim 어느 쪽에 붙어도 동일하게 동작한다.
func Walk(ctx context.Context, c *govmomi.Client) (*Inventory, error) {
	m := view.NewManager(c.Client)
	pc := property.DefaultCollector(c.Client)

	dcView, err := m.CreateContainerView(ctx, c.ServiceContent.RootFolder, []string{"Datacenter"}, true)
	if err != nil {
		return nil, fmt.Errorf("데이터센터 조회 실패: %w", err)
	}
	defer dcView.Destroy(ctx)

	var dcs []mo.Datacenter
	if err := dcView.Retrieve(ctx, []string{"Datacenter"}, []string{"name", "vmFolder", "networkFolder", "hostFolder"}, &dcs); err != nil {
		return nil, fmt.Errorf("데이터센터 속성 조회 실패: %w", err)
	}

	inv := &Inventory{}
	for _, dc := range dcs {
		d := Datacenter{Name: dc.Name}

		if names, err := childFolderNames(ctx, pc, dc.VmFolder); err == nil {
			d.VMFolders = names
		}
		if names, err := childFolderNames(ctx, pc, dc.NetworkFolder); err == nil {
			d.NetFolders = names
		}

		clusters, err := walkClusters(ctx, m, pc, dc.HostFolder)
		if err != nil {
			return nil, err
		}
		d.Clusters = clusters

		nets, err := walkNetworks(ctx, m, dc.Reference())
		if err != nil {
			return nil, err
		}
		d.Networks = nets

		inv.Datacenters = append(inv.Datacenters, d)
	}
	return inv, nil
}

func childFolderNames(ctx context.Context, pc *property.Collector, ref types.ManagedObjectReference) ([]string, error) {
	var folder mo.Folder
	if err := pc.RetrieveOne(ctx, ref, []string{"childEntity"}, &folder); err != nil {
		return nil, fmt.Errorf("폴더 조회 실패: %w", err)
	}
	var names []string
	for _, child := range folder.ChildEntity {
		if child.Type != "Folder" {
			continue
		}
		var f mo.Folder
		if err := pc.RetrieveOne(ctx, child, []string{"name"}, &f); err == nil {
			names = append(names, f.Name)
		}
	}
	return names, nil
}

// walkClusters는 DRS/HA 클러스터(ClusterComputeResource)와 독립형(standalone)
// 호스트(ComputeResource)를 함께 조회한다 — vSphere API에서 이 둘은 별도 kind라서
// 하나만 조회하면 독립형 호스트가 누락된다. 독립형 호스트는 "클러스터 1개짜리"로 취급한다.
func walkClusters(ctx context.Context, m *view.Manager, pc *property.Collector, root types.ManagedObjectReference) ([]Cluster, error) {
	cv, err := m.CreateContainerView(ctx, root, []string{"ComputeResource", "ClusterComputeResource"}, true)
	if err != nil {
		return nil, fmt.Errorf("클러스터 조회 실패: %w", err)
	}
	defer cv.Destroy(ctx)

	var crs []mo.ComputeResource
	if err := cv.Retrieve(ctx, []string{"ComputeResource", "ClusterComputeResource"}, []string{"name", "host"}, &crs); err != nil {
		return nil, fmt.Errorf("클러스터 속성 조회 실패: %w", err)
	}

	var clusters []Cluster
	for _, cr := range crs {
		cl := Cluster{Name: cr.Name}
		for _, hostRef := range cr.Host {
			h, err := walkHost(ctx, pc, hostRef)
			if err != nil {
				return nil, err
			}
			cl.Hosts = append(cl.Hosts, *h)
		}
		clusters = append(clusters, cl)
	}
	return clusters, nil
}

func walkHost(ctx context.Context, pc *property.Collector, ref types.ManagedObjectReference) (*Host, error) {
	var host mo.HostSystem
	if err := pc.RetrieveOne(ctx, ref, []string{"name", "vm", "config.powerSystemInfo"}, &host); err != nil {
		return nil, fmt.Errorf("호스트 속성 조회 실패: %w", err)
	}

	h := &Host{Name: host.Name}
	if host.Config != nil {
		h.PowerPolicy = host.Config.PowerSystemInfo.CurrentPolicy.ShortName
	}

	for _, vmRef := range host.Vm {
		v, err := walkVM(ctx, pc, vmRef)
		if err != nil {
			return nil, err
		}
		h.VMs = append(h.VMs, *v)
	}
	return h, nil
}

func walkVM(ctx context.Context, pc *property.Collector, ref types.ManagedObjectReference) (*VM, error) {
	var vm mo.VirtualMachine
	if err := pc.RetrieveOne(ctx, ref, []string{"name", "config"}, &vm); err != nil {
		return nil, fmt.Errorf("VM 속성 조회 실패: %w", err)
	}
	if vm.Config == nil {
		return &VM{Name: vm.Name}, nil
	}

	coresPerSocket := int32(1)
	if vm.Config.Hardware.NumCoresPerSocket != nil {
		coresPerSocket = *vm.Config.Hardware.NumCoresPerSocket
	}

	v := &VM{
		Name:           vm.Name,
		NumCPU:         vm.Config.Hardware.NumCPU,
		CoresPerSocket: coresPerSocket,
		MemoryMB:       vm.Config.Hardware.MemoryMB,
		Values:         map[string]string{},
	}

	if vm.Config.CpuAffinity != nil {
		v.CPUAffinity = vm.Config.CpuAffinity.AffinitySet
	}

	for _, dev := range vm.Config.Hardware.Device {
		if disk, ok := dev.(*types.VirtualDisk); ok {
			v.DiskGB = append(v.DiskGB, float64(disk.CapacityInKB)/1024/1024)
			continue
		}
		// VirtualE1000/VirtualVmxnet3 등 실제 어댑터는 전부 *types.VirtualEthernetCard가
		// 아니라 그 하위 구체 타입이라 BaseVirtualEthernetCard 인터페이스로 잡아야 한다
		// (구체 타입 스위치로는 하나도 안 걸림 — 실제로 확인함, vm-param-check와 동일 패턴).
		if nic, ok := dev.(types.BaseVirtualEthernetCard); ok {
			d := nic.GetVirtualEthernetCard()
			connected := d.Connectable != nil && d.Connectable.Connected
			if nb, ok := d.Backing.(*types.VirtualEthernetCardNetworkBackingInfo); ok {
				v.Networks = append(v.Networks, NetworkAdapter{Portgroup: nb.DeviceName, Connected: connected})
			} else if dvb, ok := d.Backing.(*types.VirtualEthernetCardDistributedVirtualPortBackingInfo); ok && dvb.Port.PortgroupKey != "" {
				v.Networks = append(v.Networks, NetworkAdapter{Portgroup: dvb.Port.PortgroupKey, Connected: connected})
			}
		}
	}

	for _, f := range fields.VMFields {
		if val, ok := f.Extract(vm); ok {
			v.Values[f.Key] = val
		}
	}

	return v, nil
}

func walkNetworks(ctx context.Context, m *view.Manager, root types.ManagedObjectReference) ([]PortGroup, error) {
	nv, err := m.CreateContainerView(ctx, root, []string{"Network"}, true)
	if err != nil {
		return nil, fmt.Errorf("네트워크 조회 실패: %w", err)
	}
	defer nv.Destroy(ctx)

	var nets []mo.Network
	if err := nv.Retrieve(ctx, []string{"Network"}, []string{"name"}, &nets); err != nil {
		return nil, fmt.Errorf("네트워크 속성 조회 실패: %w", err)
	}

	var pgs []PortGroup
	for _, n := range nets {
		pgs = append(pgs, PortGroup{Name: n.Name})
	}
	return pgs, nil
}
