// vcsimenv: V2 회귀/확장 검증용 vcsim 실행기.
// 데이터센터 N개, 클러스터/독립 호스트, VM, 그리고 호스트·VM을 여러 단계 폴더 안으로 옮긴
// 인벤토리를 만든 뒤 "READY <url>"을 찍고 SIGTERM까지 대기한다.
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
	"strings"
	"syscall"

	"github.com/vmware/govmomi"
	"github.com/vmware/govmomi/find"
	"github.com/vmware/govmomi/object"
	"github.com/vmware/govmomi/simulator"
	"github.com/vmware/govmomi/vim25/types"
)

func main() {
	dc := flag.Int("dc", 1, "데이터센터 수")
	cluster := flag.Int("cluster", 1, "데이터센터당 클러스터 수")
	clusterHost := flag.Int("clusterHost", 2, "클러스터당 호스트 수")
	host := flag.Int("host", 1, "데이터센터당 독립 호스트 수")
	machine := flag.Int("machine", 0, "호스트(리소스풀)당 기본 VM 수")
	fqdn := flag.String("fqdnHosts", "", "쉼표로 구분한 이름의 독립 호스트를 첫 데이터센터에 추가 (예: bm1.example.com,bm2.example.com)")
	nest := flag.Int("nest", 0, "호스트/클러스터와 VM을 이 깊이만큼 중첩 폴더(N1/N2/...) 안으로 옮김")
	addr := flag.String("addr", "127.0.0.1:0", "listen 주소")
	staticPower := flag.Bool("staticPower", false, "모든 호스트의 전원 정책을 High Performance(static)로 둔다 (vcsim 기본은 Balanced(dynamic))")
	flag.Parse()

	m := simulator.VPX()
	m.Datacenter = *dc
	m.Cluster = *cluster
	m.ClusterHost = *clusterHost
	m.Host = *host
	m.Machine = *machine
	m.Autostart = false
	defer m.Remove()
	if err := m.Create(); err != nil {
		log.Fatal(err)
	}
	m.Service.Listen = &url.URL{Host: *addr}
	m.Service.TLS = new(tls.Config)
	s := m.Service.NewServer()
	defer s.Close()

	ctx := context.Background()
	if *fqdn != "" {
		c, err := govmomi.NewClient(ctx, s.URL, true)
		if err != nil {
			log.Fatal(err)
		}
		f := find.NewFinder(c.Client, true)
		dcs, err := f.DatacenterList(ctx, "*")
		if err != nil {
			log.Fatal(err)
		}
		f.SetDatacenter(dcs[0])
		folders, err := dcs[0].Folders(ctx)
		if err != nil {
			log.Fatal(err)
		}
		for _, name := range strings.Split(*fqdn, ",") {
			spec := types.HostConnectSpec{HostName: name, UserName: "user", Password: "pass", Force: true}
			task, err := folders.HostFolder.AddStandaloneHost(ctx, spec, true, nil, nil)
			if err == nil {
				err = task.Wait(ctx)
			}
			if err != nil {
				log.Fatalf("호스트 추가 실패 %s: %v", name, err)
			}
			// 추가한 호스트에는 데이터스토어가 없어 vm_create 가 건너뛰므로 로컬 데이터스토어를 붙인다.
			h, err := f.HostSystem(ctx, name)
			if err != nil {
				log.Fatal(err)
			}
			dss, err := h.ConfigManager().DatastoreSystem(ctx)
			if err != nil {
				log.Fatal(err)
			}
			if _, err := dss.CreateLocalDatastore(ctx, "ds-"+strings.Split(name, ".")[0], os.TempDir()); err != nil {
				log.Fatalf("데이터스토어 추가 실패 %s: %v", name, err)
			}
		}
		_ = c.Logout(ctx)
	}
	if *nest > 0 {
		c, err := govmomi.NewClient(ctx, s.URL, true)
		if err != nil {
			log.Fatal(err)
		}
		f := find.NewFinder(c.Client, true)
		dcs, err := f.DatacenterList(ctx, "*")
		if err != nil {
			log.Fatal(err)
		}
		for _, d := range dcs {
			folders, err := d.Folders(ctx)
			if err != nil {
				log.Fatal(err)
			}
			for _, pair := range []struct {
				root  *object.Folder
				kinds []string
			}{
				{folders.HostFolder, []string{"ClusterComputeResource", "ComputeResource"}},
				{folders.VmFolder, []string{"VirtualMachine"}},
			} {
				children, err := pair.root.Children(ctx)
				if err != nil {
					log.Fatal(err)
				}
				leaf := pair.root
				for i := 1; i <= *nest; i++ {
					leaf, err = leaf.CreateFolder(ctx, fmt.Sprintf("N%d", i))
					if err != nil {
						log.Fatal(err)
					}
				}
				for _, ch := range children {
					for _, k := range pair.kinds {
						if ch.Reference().Type != k {
							continue
						}
						// vcsim은 클러스터의 폴더 이동을 지원하지 않는다 — 옮길 수 있는 것만 옮기고 나머지는 알린다.
						task, err := leaf.MoveInto(ctx, []types.ManagedObjectReference{ch.Reference()})
						if err == nil {
							err = task.Wait(ctx)
						}
						if err != nil {
							fmt.Fprintf(os.Stderr, "MOVE-SKIP %s %s: %v\n", k, ch.Reference().Value, err)
						}
					}
				}
			}
		}
		_ = c.Logout(ctx)
	}

	if *staticPower {
		// vcsim 은 HostPowerSystem 을 구현하지 않아 API 로 바꿀 수 없다 — 시뮬레이터 객체를 직접 고친다(READY 전, 클라이언트 접속 전).
		for _, e := range m.Map().All("HostSystem") {
			h := e.(*simulator.HostSystem)
			cfg := *h.Config // 호스트끼리 기본 설정을 공유할 수 있어 복사본을 고친다
			cfg.PowerSystemInfo = &types.PowerSystemInfo{CurrentPolicy: types.HostPowerPolicy{
				Key: 1, Name: "PowerPolicy.static.name", ShortName: "static", Description: "PowerPolicy.static.description"}}
			h.Config = &cfg
		}
	}

	fmt.Printf("READY %s\n", s.URL.Host)
	os.Stdout.Sync()
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGTERM, syscall.SIGINT)
	<-sig
}
