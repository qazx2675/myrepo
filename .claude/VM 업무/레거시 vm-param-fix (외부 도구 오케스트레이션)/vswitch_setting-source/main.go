package main

import (
	"context"
	"flag"
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

// TargetHost 매핑 구조체
type HostTarget struct {
	HostName  string
	GroupName string
}

func main() {
	var (
		vcenterURL  = flag.String("url", "https://administrator@vsphere.local:Password!@vcenter.example.local/sdk", "vCenter URL")
		clusterName = flag.String("cluster", "Production-Cluster", "Cluster Name")
		// 병렬 동시 처리수 (500대 규모 권장: 16~32)
		concurrency = flag.Int("concurrency", 24, "병렬 동시 처리수 (500대 규모 권장: 16~32)")
	)
	flag.Parse()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	u, err := soap.ParseURL(*vcenterURL)
	if err != nil {
		panic(err)
	}

	client, err := govmomi.NewClient(ctx, u, true)
	if err != nil {
		panic(err)
	}
	defer client.Logout(ctx)

	finder := find.NewFinder(client.Client, false)
	dc, err := finder.DefaultDatacenter(ctx)
	if err != nil {
		panic(err)
	}
	finder.SetDatacenter(dc)

	cluster, err := finder.ClusterComputeResource(ctx, *clusterName)
	if err != nil {
		panic(err)
	}

	// 대상 호스트 목록 (예: 500대 규모의 HostGroup 매핑 데이터)
	hostList := []HostTarget{
		{HostName: "esxi-node-001.local", GroupName: "Compute-HG-A"},
		{HostName: "esxi-node-002.local", GroupName: "Compute-HG-A"},
		{HostName: "esxi-node-003.local", GroupName: "Compute-HG-B"},
		// ... 500대 노드 데이터
	}

	// HostGroup별 호스트 MOR 리스트 분류
	groupMap := make(map[string][]types.ManagedObjectReference)
	var mapMu sync.Mutex

	var wg sync.WaitGroup
	semaphore := make(chan struct{}, *concurrency)

	fmt.Printf("[INFO] 호스트 인벤토리 탐색 및 매핑 시작 (동시 처리 수: %d)...\n", *concurrency)

	for _, target := range hostList {
		wg.Add(1)
		go func(t HostTarget) {
			defer wg.Done()
			semaphore <- struct{}{}
			defer func() { <-semaphore }()

			h, err := finder.HostSystem(ctx, t.HostName)
			if err != nil {
				fmt.Printf("[ERROR] 호스트 검색 실패 (%s): %v\n", t.HostName, err)
				return
			}

			mapMu.Lock()
			groupMap[t.GroupName] = append(groupMap[t.GroupName], h.Reference())
			mapMu.Unlock()
		}(target)
	}
	wg.Wait()

	// 클러스터 DRS/HostGroup Spec 구성 및 일괄 적용
	fmt.Println("[INFO] 클러스터 HostGroup 구성 적용 시작...")
	if err := applyClusterHostGroups(ctx, client, cluster, groupMap); err != nil {
		fmt.Printf("[FAIL] HostGroup 설정 적용 실패: %v\n", err)
		return
	}

	fmt.Println("[SUCCESS] 모든 호스트 그룹 병렬 적용 완료.")
}

func applyClusterHostGroups(ctx context.Context, client *govmomi.Client, cluster *object.ClusterComputeResource, groupMap map[string][]types.ManagedObjectReference) error {
	var groupSpecs []types.ClusterGroupSpec

	for gName, hosts := range groupMap {
		gSpec := types.ClusterGroupSpec{
			ArrayUpdateSpec: types.ArrayUpdateSpec{
				Operation: types.ArrayUpdateOperationAdd,
			},
			Info: &types.ClusterHostGroup{
				ClusterGroupInfo: types.ClusterGroupInfo{
					Name: gName,
				},
				Host: hosts,
			},
		}
		groupSpecs = append(groupSpecs, gSpec)
	}

	configSpec := types.ClusterConfigSpecEx{
		GroupSpec: groupSpecs,
	}

	req := types.ReconfigureComputeResource_Task{
		This: cluster.Reference(),
		Spec: &configSpec,
	}

	res, err := methods.ReconfigureComputeResource_Task(ctx, client.Client, &req)
	if err != nil {
		return err
	}

	task := object.NewTask(client.Client, res.Returnval)
	return task.Wait(ctx)
}
