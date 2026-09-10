// Package pool 은 고루틴 수를 제한한 채로 작업을 병렬 실행하는 워커 풀입니다.
//
// vCenter API 는 세션/요청이 몰리면 급격히 느려지거나 거절하기 때문에,
// 대상 VM 수만큼 고루틴을 무작정 띄우지 않고 세마포어로 동시 실행 개수를 묶습니다.
package pool

import "sync"

// Run 은 items 를 최대 concurrency 개씩 동시에 fn 으로 처리합니다.
// fn 은 여러 고루틴에서 동시에 불리므로 공유 상태를 만지려면 호출 측에서 잠가야 합니다.
// 결과를 인덱스별 슬라이스에 쓰는 방식이면 잠금 없이 안전합니다.
func Run[T any](concurrency int, items []T, fn func(i int, item T)) {
	if concurrency < 1 {
		concurrency = 1
	}
	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup

	for i, item := range items {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int, item T) {
			defer wg.Done()
			defer func() { <-sem }()
			fn(i, item)
		}(i, item)
	}
	wg.Wait()
}
