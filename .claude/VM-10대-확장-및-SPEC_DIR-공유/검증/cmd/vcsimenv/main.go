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
	nest := flag.Int("nest", 0, "호스트/클러스터와 VM을 이 깊이만큼 중첩 폴더(N1/N2/...) 안으로 옮김")
	addr := flag.String("addr", "127.0.0.1:0", "listen 주소")
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

	fmt.Printf("READY %s\n", s.URL.Host)
	os.Stdout.Sync()
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGTERM, syscall.SIGINT)
	<-sig
}
