// apply.go: 교정 계획을 실제 vCenter Reconfigure로 적용한다.
// VM 1대당 Reconfigure를 정확히 한 번만 호출한다 — 고급설정/CPU 토폴로지/affinity를
// 도구별로 따로 부르면 같은 VM을 두세 번 리컨피그하게 되고, 그만큼 실패 지점도 늘어난다.
package fixer

import (
	"context"
	"fmt"
	"sync"

	"github.com/vmware/govmomi"
	"github.com/vmware/govmomi/object"
	"github.com/vmware/govmomi/vim25/types"
)

// Result는 VM 1대의 적용 결과다.
type Result struct {
	VM  string
	Err error
}

// Apply는 plan.Fixes를 동시에 concurrency대까지 적용한다.
// clients는 vCenter 주소 -> 로그인된 클라이언트 맵이다 — VMFix.Vcenter로 어느 클라이언트를
// 쓸지 찾는다(대상 VM들이 여러 vCenter에 걸쳐 있을 수 있어서 클라이언트 하나로는 부족하다).
// 결과는 plan.Fixes와 같은 순서로 돌려줘서 로그가 매번 같은 순서로 나오게 한다.
func Apply(ctx context.Context, clients map[string]*govmomi.Client, plan *Plan, concurrency int) []Result {
	if concurrency < 1 {
		concurrency = 1
	}

	results := make([]Result, len(plan.Fixes))
	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup

	for i, fix := range plan.Fixes {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int, fix VMFix) {
			defer wg.Done()
			defer func() { <-sem }()
			client, ok := clients[fix.Vcenter]
			if !ok {
				results[i] = Result{VM: fix.VM, Err: fmt.Errorf("%s에 대한 vCenter 연결이 없습니다", fix.Vcenter)}
				return
			}
			results[i] = Result{VM: fix.VM, Err: applyOne(ctx, client, fix)}
		}(i, fix)
	}
	wg.Wait()

	return results
}

func applyOne(ctx context.Context, client *govmomi.Client, fix VMFix) error {
	spec := types.VirtualMachineConfigSpec{ExtraConfig: fix.ExtraConfig}

	if fix.NumCPUs != nil {
		spec.NumCPUs = *fix.NumCPUs
	}
	if fix.NumCoresPerSocket != nil {
		spec.NumCoresPerSocket = *fix.NumCoresPerSocket
	}
	if fix.CoresPerNumaNode != nil {
		// 설정 편집 > VM 옵션 > CPU 토폴로지 > "NUMA 노드당 코어 수" UI가 읽고 쓰는 필드.
		// ExtraConfig의 numa.vcpu.maxPerVirtualNode와는 별개라 둘 다 써야 한다.
		spec.VirtualNuma = &types.VirtualMachineVirtualNuma{CoresPerNumaNode: fix.CoresPerNumaNode}
	}

	vm := object.NewVirtualMachine(client.Client, fix.Ref)
	task, err := vm.Reconfigure(ctx, spec)
	if err != nil {
		return fmt.Errorf("Reconfigure 요청 실패: %w", err)
	}
	if err := task.Wait(ctx); err != nil {
		return fmt.Errorf("Reconfigure 작업 실패: %w", err)
	}
	return nil
}
