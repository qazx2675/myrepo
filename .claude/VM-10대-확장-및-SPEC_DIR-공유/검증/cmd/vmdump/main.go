// vmdump: vCenter(또는 vcsim)의 VM 설정을 이름순으로 한 줄씩 덤프한다. 변경 전/후 결과 비교(diff)용.
// 사용: VC_PASSWORD=x vmdump -vc 127.0.0.1:port [-match 정규식]
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strings"

	"github.com/vmware/govmomi"
	"github.com/vmware/govmomi/property"
	"github.com/vmware/govmomi/view"
	"github.com/vmware/govmomi/vim25/mo"
	"github.com/vmware/govmomi/vim25/types"
)

func main() {
	vc := flag.String("vc", "", "vCenter 주소")
	id := flag.String("id", "user", "계정")
	match := flag.String("match", ".", "VM 이름 정규식")
	flag.Parse()
	re := regexp.MustCompile(*match)

	ctx := context.Background()
	u := &url.URL{Scheme: "https", Host: *vc, Path: "/sdk", User: url.UserPassword(*id, os.Getenv("VC_PASSWORD"))}
	c, err := govmomi.NewClient(ctx, u, true)
	if err != nil {
		log.Fatal(err)
	}
	defer c.Logout(ctx)

	v, err := view.NewManager(c.Client).CreateContainerView(ctx, c.ServiceContent.RootFolder, []string{"VirtualMachine"}, true)
	if err != nil {
		log.Fatal(err)
	}
	var vms []mo.VirtualMachine
	if err := v.Retrieve(ctx, []string{"VirtualMachine"}, []string{"name", "config", "runtime.host", "parent"}, &vms); err != nil {
		log.Fatal(err)
	}
	pc := property.DefaultCollector(c.Client)
	name := func(ref *types.ManagedObjectReference) string {
		if ref == nil {
			return "-"
		}
		var e mo.ManagedEntity
		if err := pc.RetrieveOne(ctx, *ref, []string{"name"}, &e); err != nil {
			return ref.Value
		}
		return e.Name
	}

	var lines []string
	for _, vm := range vms {
		if !re.MatchString(vm.Name) || vm.Config == nil {
			continue
		}
		cfg := vm.Config
		var b strings.Builder
		fmt.Fprintf(&b, "%s host=%s folder=%s cpu=%d mem=%d fw=%s guest=%s lock=%v",
			vm.Name, name(vm.Runtime.Host), name(vm.Parent), cfg.Hardware.NumCPU, cfg.Hardware.MemoryMB, cfg.Firmware, cfg.GuestId,
			deref(cfg.MemoryReservationLockedToMax))
		if a := cfg.CpuAllocation; a != nil && a.Shares != nil {
			fmt.Fprintf(&b, " cpuShares=%s/%d", a.Shares.Level, a.Shares.Shares)
		}
		if a := cfg.MemoryAllocation; a != nil {
			if a.Shares != nil {
				fmt.Fprintf(&b, " memShares=%s/%d", a.Shares.Level, a.Shares.Shares)
			}
			if a.Reservation != nil {
				fmt.Fprintf(&b, " memResv=%d", *a.Reservation)
			}
		}
		var ex []string
		for _, o := range cfg.ExtraConfig {
			ov := o.GetOptionValue()
			if strings.HasPrefix(ov.Key, "sched.") || strings.HasPrefix(ov.Key, "numa.") || strings.HasPrefix(ov.Key, "cpuid.") {
				ex = append(ex, fmt.Sprintf("%s=%v", ov.Key, ov.Value))
			}
		}
		sort.Strings(ex)
		fmt.Fprintf(&b, " extra=[%s]", strings.Join(ex, ","))
		if cfg.BootOptions != nil {
			fmt.Fprintf(&b, " secureBoot=%v bootOrder=%d", deref(cfg.BootOptions.EfiSecureBootEnabled), len(cfg.BootOptions.BootOrder))
		}
		for _, d := range cfg.Hardware.Device {
			switch dev := d.(type) {
			case *types.VirtualDisk:
				thin := false
				if bk, ok := dev.Backing.(*types.VirtualDiskFlatVer2BackingInfo); ok {
					thin = deref(bk.ThinProvisioned)
				}
				fmt.Fprintf(&b, " disk=%dKB/thin=%v", dev.CapacityInKB, thin)
			case types.BaseVirtualEthernetCard:
				card := dev.GetVirtualEthernetCard()
				pg := ""
				if bk, ok := card.Backing.(*types.VirtualEthernetCardNetworkBackingInfo); ok {
					pg = bk.DeviceName
				}
				conn := ""
				if card.Connectable != nil {
					conn = fmt.Sprintf("conn=%v/start=%v", card.Connectable.Connected, card.Connectable.StartConnected)
				}
				fmt.Fprintf(&b, " nic=%T:%s:%s", dev, pg, conn)
			case *types.ParaVirtualSCSIController:
				b.WriteString(" pvscsi")
			}
		}
		lines = append(lines, b.String())
	}
	sort.Strings(lines)
	for _, l := range lines {
		fmt.Println(l)
	}
}

func deref(p *bool) bool { return p != nil && *p }
