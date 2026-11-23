// work.go - 호스트별 작업을 최대 N대 동시에 실행
package main

import "sync"

// RunParallel 은 Skip·Fail 이 없는 대상마다 work 를 최대 n 개 동시에 실행하고 모두 끝날 때까지 기다린다.
// work 는 자기 Target 만 고친다.
func RunParallel(ts []*Target, n int, work func(*Target)) {
	sem := make(chan struct{}, n)
	var wg sync.WaitGroup
	for _, t := range ts {
		if t.Skip != "" || t.Fail != "" {
			continue
		}
		wg.Add(1)
		sem <- struct{}{}
		go func(t *Target) {
			defer wg.Done()
			defer func() { <-sem }()
			work(t)
		}(t)
	}
	wg.Wait()
}
