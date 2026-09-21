// labinfo: 랩 vCenter 인벤토리(읽기 전용) — 데이터센터/폴더/호스트 자원/데이터스토어/포트그룹/VM 상태
package main

import (
	"context"
	"fmt"
	"log"
	"net/url"
	"os"

	"github.com/vmware/govmomi"
	"github.com/vmware/govmomi/view"
	"github.com/vmware/govmomi/vim25/mo"
)

func main() {
	ctx := context.Background()
	u := &url.URL{Scheme: "https", Host: os.Args[1], Path: "/sdk", User: url.UserPassword(os.Getenv("VC_ID"), os.Getenv("VC_PASSWORD"))}
	c, err := govmomi.NewClient(ctx, u, true)
	if err != nil {
		log.Fatal(err)
	}
	defer c.Logout(ctx)
	m := view.NewManager(c.Client)
	get := func(kind string, props []string, dst interface{}) {
		v, err := m.CreateContainerView(ctx, c.ServiceContent.RootFolder, []string{kind}, true)
		if err != nil {
			log.Fatal(err)
		}
		defer v.Destroy(ctx)
		if err := v.Retrieve(ctx, []string{kind}, props, dst); err != nil {
			log.Fatal(err)
		}
	}
	var dcs []mo.Datacenter
	get("Datacenter", []string{"name"}, &dcs)
	for _, d := range dcs {
		fmt.Printf("DC %s\n", d.Name)
	}
	var hosts []mo.HostSystem
	get("HostSystem", []string{"name", "summary.hardware", "summary.quickStats", "config.network.portgroup", "runtime.connectionState"}, &hosts)
	for _, h := range hosts {
		hw := h.Summary.Hardware
		fmt.Printf("HOST %s state=%s cpu=%dcores mem=%dMB used=%dMB\n", h.Name, h.Runtime.ConnectionState, hw.NumCpuCores, hw.MemorySize/1024/1024, h.Summary.QuickStats.OverallMemoryUsage)
		if h.Config != nil {
			for _, pg := range h.Config.Network.Portgroup {
				fmt.Printf("  PG %s vlan=%d vswitch=%s\n", pg.Spec.Name, pg.Spec.VlanId, pg.Spec.VswitchName)
			}
		}
	}
	var dss []mo.Datastore
	get("Datastore", []string{"name", "summary"}, &dss)
	for _, d := range dss {
		fmt.Printf("DS %s free=%dGB cap=%dGB\n", d.Name, d.Summary.FreeSpace>>30, d.Summary.Capacity>>30)
	}
	var vms []mo.VirtualMachine
	get("VirtualMachine", []string{"name", "runtime.powerState", "config.hardware.numCPU", "config.hardware.memoryMB", "parent"}, &vms)
	for _, v := range vms {
		var cpu int32
		var mem int32
		if v.Config != nil {
			cpu, mem = v.Config.Hardware.NumCPU, v.Config.Hardware.MemoryMB
		}
		fmt.Printf("VM %s %s cpu=%d mem=%dMB\n", v.Name, v.Runtime.PowerState, cpu, mem)
	}
}
