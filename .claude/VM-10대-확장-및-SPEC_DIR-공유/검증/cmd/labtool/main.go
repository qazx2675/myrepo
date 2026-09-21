// labtool: home-test 랩 정리/보조 도구. 안전장치: 대상 이름을 코드로 제한한다(vcenter, 192ev01, 192ev02 는 절대 건드리지 않음).
//   labtool <vc> mkdc V2TEST-DC | rmdc V2TEST-DC | rmpg <host> <pg...> | rmvm | poweron <vm> | poweroff <vm>
package main

import (
	"context"
	"fmt"
	"log"
	"net/url"
	"os"
	"regexp"
	"strings"

	"github.com/vmware/govmomi"
	"github.com/vmware/govmomi/find"
	"github.com/vmware/govmomi/object"
	"github.com/vmware/govmomi/view"
	"github.com/vmware/govmomi/vim25/mo"
	"github.com/vmware/govmomi/vim25/types"
)

var allowedVM = regexp.MustCompile(`^192ev(0[3-9]|10)$`) // 이번 테스트가 만드는 VM 이름만

func main() {
	if len(os.Args) < 3 {
		log.Fatal("usage")
	}
	ctx := context.Background()
	u := &url.URL{Scheme: "https", Host: os.Args[1], Path: "/sdk", User: url.UserPassword(os.Getenv("VC_ID"), os.Getenv("VC_PASSWORD"))}
	c, err := govmomi.NewClient(ctx, u, true)
	if err != nil {
		log.Fatal(err)
	}
	defer c.Logout(ctx)
	cmd, args := os.Args[2], os.Args[3:]
	f := find.NewFinder(c.Client, true)

	vmByName := func() map[string]*object.VirtualMachine {
		v, _ := view.NewManager(c.Client).CreateContainerView(ctx, c.ServiceContent.RootFolder, []string{"VirtualMachine"}, true)
		defer v.Destroy(ctx)
		var vms []mo.VirtualMachine
		if err := v.Retrieve(ctx, []string{"VirtualMachine"}, []string{"name"}, &vms); err != nil {
			log.Fatal(err)
		}
		m := map[string]*object.VirtualMachine{}
		for _, vm := range vms {
			m[vm.Name] = object.NewVirtualMachine(c.Client, vm.Self)
		}
		return m
	}
	wait := func(t *object.Task, err error) {
		if err == nil {
			err = t.Wait(ctx)
		}
		if err != nil {
			log.Fatal(err)
		}
	}

	switch cmd {
	case "mkdc":
		if args[0] != "V2TEST-DC" {
			log.Fatal("허용된 이름은 V2TEST-DC 뿐")
		}
		if _, err := object.NewRootFolder(c.Client).CreateDatacenter(ctx, args[0]); err != nil {
			log.Fatal(err)
		}
		fmt.Println("데이터센터 생성:", args[0])
	case "rmdc":
		if args[0] != "V2TEST-DC" {
			log.Fatal("허용된 이름은 V2TEST-DC 뿐")
		}
		dc, err := f.Datacenter(ctx, args[0])
		if err != nil {
			log.Fatal(err)
		}
		wait(dc.Destroy(ctx))
		fmt.Println("데이터센터 삭제:", args[0])
	case "rmpg":
		host, err := f.HostSystem(ctx, "/HPC/host/*/"+args[0])
		if err != nil {
			host, err = f.HostSystem(ctx, args[0])
		}
		if err != nil {
			log.Fatal(err)
		}
		ns, err := host.ConfigManager().NetworkSystem(ctx)
		if err != nil {
			log.Fatal(err)
		}
		for _, pg := range args[1:] {
			if !strings.HasPrefix(pg, "V2T-") {
				log.Fatalf("V2T- 로 시작하는 포트그룹만 삭제합니다: %s", pg)
			}
			if err := ns.RemovePortGroup(ctx, pg); err != nil {
				fmt.Println("포트그룹 삭제 실패:", pg, err)
			} else {
				fmt.Println("포트그룹 삭제:", pg)
			}
		}
	case "rmvm":
		for name, vm := range vmByName() {
			if !allowedVM.MatchString(name) {
				continue
			}
			ps, _ := vm.PowerState(ctx)
			if ps == types.VirtualMachinePowerStatePoweredOn {
				wait(vm.PowerOff(ctx))
			}
			wait(vm.Destroy(ctx))
			fmt.Println("VM 삭제:", name)
		}
	case "poweron", "poweroff":
		if !allowedVM.MatchString(args[0]) {
			log.Fatalf("허용되지 않은 VM: %s", args[0])
		}
		vm := vmByName()[args[0]]
		if vm == nil {
			log.Fatal("VM 없음")
		}
		if cmd == "poweron" {
			wait(vm.PowerOn(ctx))
		} else {
			wait(vm.PowerOff(ctx))
		}
		fmt.Println(cmd, args[0], "완료")
	default:
		log.Fatal("unknown")
	}
}
