package recipe

import (
	"fmt"

	"vc-test-env/internal/inventory"
)

type vmSnapshot struct {
	numCPU         int32
	coresPerSocket int32
	memoryMB       int32
	values         map[string]string
}

func flatten(inv *inventory.Inventory) map[string]vmSnapshot {
	out := map[string]vmSnapshot{}
	for _, dc := range inv.Datacenters {
		for _, cl := range dc.Clusters {
			for _, h := range cl.Hosts {
				for _, v := range h.VMs {
					out[v.Name] = vmSnapshot{
						numCPU:         v.NumCPU,
						coresPerSocket: v.CoresPerSocket,
						memoryMB:       v.MemoryMB,
						values:         v.Values,
					}
				}
			}
		}
	}
	return out
}

// Diff는 원본과 재생성본 사이의 VM 단위 차이를 사람이 읽을 수 있는 줄로 반환한다.
// 빈 슬라이스면 차이가 없는 것이다.
func Diff(original, rebuilt *inventory.Inventory) []string {
	a := flatten(original)
	b := flatten(rebuilt)

	var lines []string
	for name, av := range a {
		bv, ok := b[name]
		if !ok {
			lines = append(lines, fmt.Sprintf("누락: %s (원본에는 있는데 재생성본에 없음)", name))
			continue
		}
		if av.numCPU != bv.numCPU {
			lines = append(lines, fmt.Sprintf("%s: numCPU 불일치 (%d -> %d)", name, av.numCPU, bv.numCPU))
		}
		if av.coresPerSocket != bv.coresPerSocket {
			lines = append(lines, fmt.Sprintf("%s: coresPerSocket 불일치 (%d -> %d)", name, av.coresPerSocket, bv.coresPerSocket))
		}
		if av.memoryMB != bv.memoryMB {
			lines = append(lines, fmt.Sprintf("%s: memoryMB 불일치 (%d -> %d)", name, av.memoryMB, bv.memoryMB))
		}
		for k, av2 := range av.values {
			if bv2, ok := bv.values[k]; !ok || av2 != bv2 {
				lines = append(lines, fmt.Sprintf("%s: %s 불일치 (%q -> %q)", name, k, av2, bv2))
			}
		}
	}
	for name := range b {
		if _, ok := a[name]; !ok {
			lines = append(lines, fmt.Sprintf("추가: %s (재생성본에만 있음)", name))
		}
	}
	return lines
}
