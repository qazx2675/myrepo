package main

import (
	"fmt"
	"sync"
)

// sweep.go 는 check 와 dump 가 공유하는 호스트 순회(워커풀)와 AUTH_FAIL 차단기입니다.
//
// 전 호스트가 같은 공통 계정을 쓰므로, 비밀번호가 틀린 채로 수천 대에 동시에 로그인하면 계정이 잠깁니다.
// 그래서 conf 의 auth_fail_stop(N, 기본 3, 0 이면 끔)에 따라 다음처럼 움직입니다.
//
//   - 웜업: 처음 N 개 대상은 한 대씩(직렬) 처리합니다. 그 사이 로그인에 한 번이라도 성공하면
//     바로 병렬(Concurrency)로 전환합니다. N 개 모두 AUTH_FAIL 이면 즉시 멈춥니다.
//     (BMC 에 접속하지 않은 대상 — 이름 해석 실패 — 은 웜업 개수에 넣지 않습니다.)
//   - 병렬 전환 후에도 AUTH_FAIL 누적이 N 이상이면 새 호스트 시작을 멈춥니다.
//     이미 진행 중인 호스트는 끝까지 처리하므로, 그 사이 AUTH_FAIL 이 N 을 조금 넘을 수 있습니다.
//   - 멈춘 뒤 남은 호스트는 접속하지 않고 SKIPPED_AUTH_STOP 으로 기록합니다 (retry.txt 로 재실행).

// StatusSkippedAuthStop 은 AUTH_FAIL 차단기가 작동해 접속하지 않은 호스트의 상태입니다.
const StatusSkippedAuthStop = "SKIPPED_AUTH_STOP"

// hostOutcome 은 호스트 1대 처리 결과를 차단기 관점으로 줄인 값입니다.
type hostOutcome int

const (
	outNone     hostOutcome = iota // BMC 에 접속하지 않음 (이름 해석 실패 등) — 집계에서 제외
	outOther                       // 접속했으나 로그인 여부를 알 수 없음 (UNREACHABLE, TIMEOUT 등)
	outLoggedIn                    // 로그인 성공 (AUTH_FAIL 아님)
	outAuthFail                    // AUTH_FAIL
)

// outcomeOf 는 호스트 결과의 상태와 호출 통계로 hostOutcome 을 정합니다. contacted 는 접속을 시도했는지입니다.
func outcomeOf(status string, st CallStats, contacted bool) hostOutcome {
	switch {
	case status == StatusAuthFail:
		return outAuthFail
	case !contacted:
		return outNone
	case st.AuthMode != "": // 세션 생성 성공 또는 Basic 폴백까지 진행
		return outLoggedIn
	}
	return outOther
}

// sweepHosts 는 호스트 n 대를 처리합니다. work(i) 는 i 번째를 처리하고 결과를 돌려주며,
// 차단기가 멈춘 뒤 도착한 호스트는 work 대신 skip(i) 이 불립니다 (둘 다 여러 고루틴에서 불릴 수 있으며
// 각각 i 번째 칸에만 써야 합니다). authStop 이 0 이면 차단기 없이 workers 개 워커로 전부 처리합니다.
func sweepHosts(n, workers, authStop int, work func(i int) hostOutcome, skip func(i int)) {
	next, authFails := 0, 0
	if authStop > 0 {
		attempts, loggedIn := 0, false
		for next < n && !loggedIn && attempts < authStop {
			o := work(next)
			next++
			switch o {
			case outNone:
				continue
			case outLoggedIn:
				loggedIn = true
			case outAuthFail:
				authFails++
			}
			attempts++
		}
		if authFails >= authStop {
			for ; next < n; next++ {
				skip(next)
			}
			return
		}
	}

	if workers > n-next {
		workers = n - next
	}
	if workers < 1 {
		workers = 1
	}
	var mu sync.Mutex
	stopped := false
	idx := make(chan int)
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range idx {
				mu.Lock()
				st := stopped
				mu.Unlock()
				if st {
					skip(i)
					continue
				}
				if work(i) == outAuthFail && authStop > 0 {
					mu.Lock()
					authFails++
					if authFails >= authStop {
						stopped = true
					}
					mu.Unlock()
				}
			}
		}()
	}
	for ; next < n; next++ {
		idx <- next
	}
	close(idx)
	wg.Wait()
}

// authStopWarning 은 차단기가 작동했을 때의 경고문입니다 (skipped 가 0 이면 빈 문자열).
// fails 는 실제 AUTH_FAIL 호스트 수, skipped 는 접속하지 않고 건너뛴 호스트 수, rerun 은 재실행 방법입니다.
func authStopWarning(fails, skipped int, rerun string) string {
	if skipped == 0 {
		return ""
	}
	return fmt.Sprintf("계정 잠금 방지를 위해 %d대에서 AUTH_FAIL → 나머지 %d대 중단. 계정/비밀번호 확인 후 %s", fails, skipped, rerun)
}

// 경고문 끝의 재실행 안내 (check 는 retry.txt 가 있고, dump 는 index.tsv 의 상태로 찾는다).
const (
	authStopRerunCheck = "retry.txt 로 재실행"
	authStopRerunDump  = "해당 대상만 다시 실행 (index.tsv 의 SKIPPED_AUTH_STOP)"
)
