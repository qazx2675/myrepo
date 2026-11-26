package diff

import (
	"cmp"
	"slices"
)

// link 는 이전·최신의 짝이다. 첨자(또는 열 번호)이며 없는 쪽은 -1 이다.
type link struct{ o, n int }

type weighted struct {
	o, n int
	w    float64
}

// chain 은 o 와 n 이 모두 엄격히 증가하는 부분열 중 가중치 합이 가장 큰 것의 첨자를 돌려준다.
// 같은 o 를 가진 후보가 여럿이어도 된다. O(k log k).
func chain(c []weighted) []int {
	if len(c) == 0 {
		return nil
	}
	order := make([]int, len(c))
	maxN := 0
	for i := range c {
		order[i] = i
		maxN = max(maxN, c[i].n)
	}
	// 같은 o 안에서는 n 내림차순이어야 서로 이어지지 않는다.
	slices.SortFunc(order, func(a, b int) int {
		return cmp.Or(cmp.Compare(c[a].o, c[b].o), cmp.Compare(c[b].n, c[a].n))
	})
	type best struct {
		sum float64
		at  int // 첨자+1, 0 은 없음
	}
	tree := make([]best, maxN+2) // Fenwick: n 접두 구간의 최댓값
	prev := make([]int, len(c))
	top := best{}
	for _, i := range order {
		b := best{}
		for k := c[i].n; k > 0; k -= k & -k {
			if tree[k].sum > b.sum {
				b = tree[k]
			}
		}
		prev[i] = b.at
		b = best{b.sum + c[i].w, i + 1}
		for k := c[i].n + 1; k < len(tree); k += k & -k {
			if b.sum > tree[k].sum {
				tree[k] = b
			}
		}
		if b.sum > top.sum {
			top = b
		}
	}
	var out []int
	for at := top.at; at > 0; at = prev[at-1] {
		out = append(out, at-1)
	}
	slices.Reverse(out)
	return out
}

// layout 은 좌우 배치 순서(links 의 첨자)를 돌려준다. inChain 은 상대 순서가 유지된 짝이다.
func layout(links []link, inChain []bool) []int {
	var co, cn []int
	for i, l := range links {
		if inChain[i] {
			co = append(co, l.o)
			cn = append(cn, l.n)
		}
	}
	slices.Sort(co)
	slices.Sort(cn)
	type key struct{ gap, class, pos, at int }
	keys := make([]key, len(links))
	for i, l := range links {
		k := key{at: i}
		switch {
		case inChain[i]:
			k.gap, _ = slices.BinarySearch(co, l.o)
			k.class = 2
		case l.n < 0:
			k.gap, _ = slices.BinarySearch(co, l.o)
			k.pos = l.o
		default:
			k.gap, _ = slices.BinarySearch(cn, l.n)
			k.class, k.pos = 1, l.n
		}
		keys[i] = k
	}
	slices.SortFunc(keys, func(a, b key) int {
		return cmp.Or(cmp.Compare(a.gap, b.gap), cmp.Compare(a.class, b.class), cmp.Compare(a.pos, b.pos))
	})
	out := make([]int, len(keys))
	for i, k := range keys {
		out[i] = k.at
	}
	return out
}
