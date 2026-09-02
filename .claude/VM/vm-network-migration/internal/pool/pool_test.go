package pool

import (
	"sync"
	"sync/atomic"
	"testing"
)

func TestRunProcessesEveryItem(t *testing.T) {
	items := make([]int, 100)
	for i := range items {
		items[i] = i
	}
	got := make([]int, len(items))

	Run(8, items, func(i int, item int) {
		got[i] = item * 2 // 인덱스별로 쓰므로 잠금이 필요 없습니다
	})

	for i := range items {
		if got[i] != i*2 {
			t.Fatalf("[%d] got %d want %d", i, got[i], i*2)
		}
	}
}

func TestRunRespectsConcurrencyLimit(t *testing.T) {
	const limit = 4
	var mu sync.Mutex
	var cur, max int

	items := make([]int, 200)
	Run(limit, items, func(int, int) {
		mu.Lock()
		cur++
		if cur > max {
			max = cur
		}
		mu.Unlock()

		mu.Lock()
		cur--
		mu.Unlock()
	})

	if max > limit {
		t.Fatalf("동시 실행 수가 제한을 넘었습니다: max=%d limit=%d", max, limit)
	}
}

func TestRunClampsBadConcurrency(t *testing.T) {
	var n int32
	items := make([]int, 10)
	Run(0, items, func(int, int) { atomic.AddInt32(&n, 1) })
	if n != 10 {
		t.Fatalf("concurrency=0 이어도 전부 처리해야 합니다: %d", n)
	}
}

func TestRunEmpty(t *testing.T) {
	Run(4, []int{}, func(int, int) { t.Fatal("빈 목록인데 호출됐습니다") })
}
