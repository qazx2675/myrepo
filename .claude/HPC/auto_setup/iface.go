// iface.go - 데몬이 의존하는 인터페이스 계약 (테스트에서 가짜 주입)
package main

import "time"

type Clock interface{ Now() time.Time }

type realClock struct{}

func (realClock) Now() time.Time { return time.Now() }

// Route: "local" | "os6"
type PingTarget struct{ Host, IP, Route string }

// Known=false: 정보 없음(os6 세션 끊김 등) → 상태 불변
type PingResult struct{ Up, Known bool }

// 키=Host. 호출 1회=ping 주기 1회(블록, 수초 내 반환)
type Pinger interface {
	Ping(targets []PingTarget) map[string]PingResult
}

type CheckResult struct {
	Responded, Anaconda bool
	Uptime              float64
}

// 키=Host
type Checker interface {
	Check(route string, hosts []string) (map[string]CheckResult, error)
}

type RunResult struct {
	Processed []string
	Abnormal  bool
	Code      string
}

// code 생성·파일·wall 포함
type Runner interface {
	Run(j *Job, hosts []string, hasOS6 bool) (RunResult, error)
}

type Notifier interface{ Wall(msg string) }
