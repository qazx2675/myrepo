package main

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/vmware/govmomi"
	"github.com/vmware/govmomi/find"
	"github.com/vmware/govmomi/object"
	"github.com/vmware/govmomi/vim25/methods"
	"github.com/vmware/govmomi/vim25/soap"
	"github.com/vmware/govmomi/vim25/types"
)

type HostTarget struct {
	IP       string
	Username string
	Password string
}

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	vcenterURL := "https://administrator@vsphere.local:YourPassword@vcenter.example.local/sdk"
	clusterName := "Production-Cluster"

	u, err := soap.ParseURL(vcenterURL)
	if err != nil {
		panic(err)
	}

	// vCenter 클라이언트 생성 (Insecure 옵션 활성화)
	client, err := govmomi.NewClient(ctx, u, true)
	if err != nil {
		panic(err)
	}
	defer client.Logout(ctx)

	// 예전에는 DefaultDatacenter()라서 데이터센터가 2개 이상이면 여기서 종료했다.
	// 데이터센터를 전부 돌며 같은 이름의 클러스터를 찾는다(여러 곳에 있으면 모호하므로 중단).
	finder := find.NewFinder(client.Client, false)
	dcs, err := finder.DatacenterList(ctx, "*")
	if err != nil {
		panic(err)
	}
	var cluster *object.ClusterComputeResource
	for _, dc := range dcs {
		finder.SetDatacenter(dc)
		c, findErr := finder.ClusterComputeResource(ctx, clusterName)
		if findErr != nil {
			continue
		}
		if cluster != nil {
			panic(fmt.Sprintf("클러스터 %q가 여러 데이터센터에 있어 어느 쪽인지 정할 수 없습니다", clusterName))
		}
		cluster = c
	}
	if cluster == nil {
		panic(fmt.Sprintf("클러스터 %q를 어느 데이터센터에서도 찾지 못했습니다", clusterName))
	}

	// 등록 대상 Bare Metal (ESXi) 호스트 목록
	hosts := []HostTarget{
		{"192.168.10.101", "root", "HostPass1!"},
		{"192.168.10.102", "root", "HostPass2!"},
		{"192.168.10.103", "root", "HostPass3!"},
	}

	var wg sync.WaitGroup
	// 병렬 처리 워커 수 제어 (동시 5대)
	semaphore := make(chan struct{}, 5)

	for _, h := range hosts {
		wg.Add(1)
		go func(target HostTarget) {
			defer wg.Done()
			semaphore <- struct{}{}
			defer func() { <-semaphore }()

			if err := addHostWithSSLAutoTrust(ctx, client, cluster, target); err != nil {
				fmt.Printf("[FAIL] 호스트 %s 등록 실패: %v\n", target.IP, err)
			} else {
				fmt.Printf("[SUCCESS] 호스트 %s 병렬 등록 완료\n", target.IP)
			}
		}(h)
	}

	wg.Wait()
	fmt.Println("모든 호스트 병렬 등록 작업이 완료되었습니다.")
}

func addHostWithSSLAutoTrust(ctx context.Context, client *govmomi.Client, cluster *object.ClusterComputeResource, target HostTarget) error {
	spec := types.HostConnectSpec{
		HostName: target.IP,
		UserName: target.Username,
		Password: target.Password,
		Force:    true,
	}

	// 1차 등록 시도
	req := types.AddHost_Task{
		This: cluster.Reference(),
		Spec: spec,
	}

	res, err := methods.AddHost_Task(ctx, client.Client, &req)
	if err != nil {
		// SSL 미신뢰 오류(SSLVerifyFault) 감지
		if soap.IsSoapFault(err) {
			fault := soap.ToSoapFault(err)
			if sslFault, ok := fault.Detail.Fault.(*types.SSLVerifyFault); ok {
				fmt.Printf("[WARN] %s: 미신뢰 SSL 감지 (Thumbprint: %s). 지문 주입 후 재시도합니다.\n", target.IP, sslFault.Thumbprint)

				// 감지된 SSL Thumbprint 주입 후 2차 재시도
				spec.SslThumbprint = sslFault.Thumbprint
				req.Spec = spec

				res, err = methods.AddHost_Task(ctx, client.Client, &req)
				if err != nil {
					return fmt.Errorf("SSL 지문 주입 후 재시도 실패: %w", err)
				}
			} else {
				return fmt.Errorf("SOAP 오류 발생: %w", err)
			}
		} else {
			return fmt.Errorf("연결 초기 오류: %w", err)
		}
	}

	// Task 완료 대기
	task := object.NewTask(client.Client, res.Returnval)
	return task.Wait(ctx)
}
