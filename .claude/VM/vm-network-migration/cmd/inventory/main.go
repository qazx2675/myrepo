// nm-inventory — vCenter 인벤토리 진단 덤프 (변경 없음)
//
// vCenter 가 실제로 보고하는 VM/ESXi 호스트 이름을 그대로 출력합니다.
// "상위폴더가 둘 이상인 환경에서 일부 VM 을 못 찾는다" 같은 증상의 원인이
// 이름 표기 불일치(FQDN vs short 등)인지 확인할 때 씁니다.
//
// 대상 목록 파일(-user 파생 경로)을 읽지 않으므로 -user 없이 동작합니다.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"vm-network-migration/internal/config"
	"vm-network-migration/internal/vsphere"
)

func main() { os.Exit(run()) }

func run() int {
	fs := flag.NewFlagSet("nm-inventory", flag.ExitOnError)
	vcenterFile := fs.String("vcenter-file", "vcenter.txt", "vCenter 주소 목록 파일 (한 줄에 하나)")
	id := fs.String("id", "lscsystems@vsphere.local", "vCenter 로그인 계정 ID")
	concurrency := fs.Int("concurrency", 8, "vCenter 동시 접속 수")
	_ = fs.Parse(os.Args[1:])

	pass, err := config.Password()
	if err != nil {
		fmt.Fprintln(os.Stderr, "오류:", err)
		return 2
	}
	vcenters, err := config.LoadVCenters(*vcenterFile)
	if err != nil {
		fmt.Fprintln(os.Stderr, "오류:", err)
		return 2
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	fleet, err := vsphere.ConnectFleet(ctx, vcenters, *id, pass, *concurrency)
	if err != nil {
		fmt.Fprintln(os.Stderr, "오류:", err)
		return 2
	}
	defer fleet.Close(context.Background())

	fleet.WriteInventory(os.Stdout)
	return 0
}
