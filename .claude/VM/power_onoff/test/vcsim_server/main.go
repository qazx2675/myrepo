//go:build vcsim

// vcsim_server: E2E 시험용 vCenter 시뮬레이터. 실제 vCenter 없이 vm_power_onoff 바이너리 전체 흐름을 시험한다.
// VM 4대(svr1ev01~03, svr2ev01)를 만들고 svr1ev02 에 lpage, svr2ev01 은 VWP-Prod 폴더로 옮기고, 전원은 -power(on|off)로 시작한다.
// 사용: go run -tags vcsim ./test/vcsim_server -addr-file /tmp/vcsim.addr   (SIGTERM 으로 종료)
package main

import (
	"context"
	"crypto/tls"
	"flag"
	"fmt"
	"log"
	"net/url"
	"os"
	"os/signal"
	"syscall"

	"github.com/vmware/govmomi"
	"github.com/vmware/govmomi/find"
	"github.com/vmware/govmomi/simulator"
	"github.com/vmware/govmomi/vim25/types"
)

func must(err error) {
	if err != nil {
		log.Fatal(err)
	}
}

func main() {
	addrFile := flag.String("addr-file", "/tmp/vcsim.addr", "host:port 를 쓸 파일")
	power := flag.String("power", "on", "시작 전원 상태 on|off")
	flag.Parse()

	model := simulator.VPX()
	must(model.Create())
	model.Service.TLS = new(tls.Config)
	s := model.Service.NewServer()
	defer s.Close()
	defer model.Remove()

	ctx := context.Background()
	u := *s.URL
	u.User = url.UserPassword("user", "pass")
	c, err := govmomi.NewClient(ctx, &u, true)
	must(err)
	fd := find.NewFinder(c.Client, true)
	dc, err := fd.DefaultDatacenter(ctx)
	must(err)
	fd.SetDatacenter(dc)
	vms, err := fd.VirtualMachineList(ctx, "*")
	must(err)
	names := []string{"svr1ev01", "svr1ev02", "svr1ev03", "svr2ev01"}
	root, err := fd.Folder(ctx, "/DC0/vm")
	must(err)
	vwp, err := root.CreateFolder(ctx, "VWP-Prod")
	must(err)
	for i, n := range names {
		t, err := vms[i].Rename(ctx, n)
		must(err)
		must(t.Wait(ctx))
		if n == "svr1ev02" {
			t, err := vms[i].Reconfigure(ctx, types.VirtualMachineConfigSpec{ExtraConfig: []types.BaseOptionValue{
				&types.OptionValue{Key: "sched.mem.lpage.enable1GPage", Value: "TRUE"}}})
			must(err)
			must(t.Wait(ctx))
		}
		if n == "svr2ev01" {
			t, err := vwp.MoveInto(ctx, []types.ManagedObjectReference{vms[i].Reference()})
			must(err)
			must(t.Wait(ctx))
		}
		if *power == "off" {
			t, err := vms[i].PowerOff(ctx)
			must(err)
			must(t.Wait(ctx))
		}
	}
	must(os.WriteFile(*addrFile, []byte(s.URL.Host), 0o644))
	fmt.Println("vcsim ready:", s.URL.Host)
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig
}
