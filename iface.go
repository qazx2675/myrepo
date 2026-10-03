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
	// 2차(§14-6) 추가: postapply 에 FAIL 줄이 있는 호스트 (2차 대상 선별용, Runner 가 채움).
	// 비어 있어도 1차 동작은 같다(FAIL 호스트도 postapply 에 나오면 processed).
	Failed []string
}

// code 생성·파일·wall 포함
type Runner interface {
	Run(j *Job, hosts []string, hasOS6 bool) (RunResult, error)
}

type Notifier interface{ Wall(msg string) }

// ---- 2차(§14-5) LDAP 백업/복원 훅 — 실제 구현은 ldapbk.go(단계 O) ----
//
// 데몬이 메인 루프에서 직렬로 호출한다 → 구현 안에서 시간 제한 필수(호출 1회 수십 초 이내 반환).
// LdapState 에는 bindpw·binddn uid 값 자체를 절대 넣지 않는다(binddn uid 비교 결과 same/diff/na 만).
//
//	Backup: job 수거 후 첫 ping 에서 응답(ping O)한, 아직 seen_down 아닌 호스트 목록으로 job 당 1회.
//	        반환 map 에 없는 호스트·Backup=="" 은 데몬이 {backup:none} 으로 기록(이후 LDAP 단계 생략).
//	        이미 seen_down 인 호스트는 호출 없이 {backup:none}.
//	Apply : READY 판정 직후(os_check run 전), backup=ok 이고 아직 비교 안 한(Bindpw=="") 호스트마다 1회.
//	        반환값이 host.ldap 이 된다(Backup 이 비어 있으면 기존 "ok" 유지). 실패는 Reason 에 사유, run 은 진행.
//
// 구현 연결: ldapbk.go 에 `func init() { newLdap = func() LdapBackup { return NewRealLdapBackup() } }`.
type LdapBackup interface {
	Backup(job *Job, hosts []string) map[string]LdapState
	Apply(job *Job, host string) LdapState
}

// nopLdap: 기본값(구현 전). 데몬은 nopLdap 이면 LDAP 단계를 통째로 건너뛴다(host.ldap 기록 없음).
type nopLdap struct{}

func (nopLdap) Backup(*Job, []string) map[string]LdapState { return nil }
func (nopLdap) Apply(*Job, string) LdapState               { return LdapState{} }

// ---- 2차(§14-6) 이중 체크 훅 — 실제 구현은 second.go(단계 P) ----

// SecondResult: os6_mgmt 경유 2차 체크 결과.
//
//	Processed: 2차 postapply 에 나온 호스트(1차 processed 규칙과 동일) → 데몬이 processed 로 합침
//	Fail     : Processed 중 2차에서도 FAIL 줄이 남은 호스트 (host.second="fail" 표시용)
type SecondResult struct {
	Processed []string
	Fail      []string
}

// Second: 1차 run 직후 같은 run 고루틴에서 1회 호출(동시 1개 유지). targets 는 selectSecondTargets 결과(비어 있으면 호출 안 함).
// first.Code 의 codes/<code>.txt 에 2차 섹션을 이어 붙이는 것은 구현 몫. j 는 run 시작 시점 사본(수정해도 데몬에 반영 안 됨).
// 데몬은 os6_mgmt·os6_os_check_sh 가 비었거나 nopSecond 이면 호출하지 않는다.
//
// 구현 연결: second.go 에 `func init() { newSecond = func() Second { return NewRealSecond() } }`.
type Second interface {
	Run(j *Job, first RunResult, targets []string) (SecondResult, error)
}

// nopSecond: 기본값(구현 전)
type nopSecond struct{}

func (nopSecond) Run(*Job, RunResult, []string) (SecondResult, error) { return SecondResult{}, nil }
