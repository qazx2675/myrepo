// port22.go - 22번 포트 확인 (ping 응답 없음 vs 응답 있는데 22 거부 vs 22 열림 구분)
package main

import (
	"errors"
	"net"
	"strconv"
	"sync"
	"syscall"
	"time"
)

// PortState 는 22번 포트 확인 결과다.
type PortState int

const (
	PortTimeout PortState = iota // 응답 없음/기타 오류
	PortOpen                     // 연결 성공
	PortRefused                  // 연결 거부(ECONNREFUSED)
)

// sshPort 는 확인할 포트다(테스트에서 바꿀 수 있게 변수로 둔다).
var sshPort = 22

// CheckPort22 는 hosts 의 22번 포트에 TCP 연결을 시도한다. 동시 연결은 parallel 개까지.
func CheckPort22(hosts []string, parallel int, timeout time.Duration) map[string]PortState {
	if parallel <= 0 {
		parallel = 100
	}
	res := make(map[string]PortState, len(hosts))
	var mu sync.Mutex
	sem := make(chan struct{}, parallel)
	var wg sync.WaitGroup
	for _, h := range hosts {
		wg.Add(1)
		sem <- struct{}{}
		go func(h string) {
			defer wg.Done()
			defer func() { <-sem }()
			st := checkOne(h, timeout)
			mu.Lock()
			res[h] = st
			mu.Unlock()
		}(h)
	}
	wg.Wait()
	return res
}

func checkOne(host string, timeout time.Duration) PortState {
	c, err := net.DialTimeout("tcp", net.JoinHostPort(host, strconv.Itoa(sshPort)), timeout)
	if err == nil {
		c.Close()
		return PortOpen
	}
	if errors.Is(err, syscall.ECONNREFUSED) {
		return PortRefused
	}
	return PortTimeout
}
