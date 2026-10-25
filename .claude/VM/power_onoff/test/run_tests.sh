#!/usr/bin/env bash
# run_tests.sh — 전체 시험: go vet, 단위 테스트, vcsim 통합 테스트, 안전장치 시나리오(--demo), E2E(vcsim 위 전체 흐름: 기존 방식 + 교차검증 방식).
# 빌드 서버(192.168.0.58, root)에서 실행한다. 로컬에서:
#   REMOTE_DIR=/root/pod_all bash dev/sync_test.sh build
#   ssh root@192.168.0.58 'bash /root/pod_all/test/run_tests.sh'
# 마지막 줄: PASS=N FAIL=M  (FAIL 이 있으면 종료코드 1)
set -uo pipefail
cd "$(dirname "$0")/.."
GO="${GO:-/opt/go1.26.5/bin/go}"; export GOFLAGS="${GOFLAGS:--mod=mod}" GOPROXY=off GONOSUMDB='*'
pass=0; fail=0
step() { # step <이름> <명령...>
  local name="$1"; shift
  printf '== %-34s ' "$name"
  if out="$("$@" 2>&1)"; then echo OK; pass=$((pass + 1))
  else echo FAIL; echo "$out" | tail -25 | sed 's/^/   | /'; fail=$((fail + 1)); fi
}

gofmt_check() { local o; o="$("$("$GO" env GOROOT)/bin/gofmt" -l ./*.go ./test/vcsim_server/*.go)"; [ -z "$o" ] || { echo "서식이 다른 파일(gofmt -w 필요):"; echo "$o"; return 1; }; }
step "gofmt (서식)"               gofmt_check
step "go vet (-tags vcsim)"      "$GO" vet -tags vcsim ./...
step "go test (단위)"             "$GO" test -count=1 .
step "go test -race (단위)"       "$GO" test -race -count=1 .
step "go test -tags vcsim"       "$GO" test -tags vcsim -count=1 .
"$GO" build -o ./vm_power_onoff . || { echo "빌드 실패"; exit 1; }
step "안전장치 시나리오(--demo)"   bash test/guard_scenarios.sh ./vm_power_onoff
if [ "$(id -u)" -eq 0 ] && command -v python3 >/dev/null; then
  step "E2E (vcsim 전체 흐름)"    bash test/e2e_vcsim.sh
  step "E2E (교차검증 방식, 격리 데몬)" bash test/e2e_vcsim_crosscheck.sh
else
  echo "== E2E: root + python3 가 없어 건너뜀"
fi
echo
echo "PASS=$pass FAIL=$fail"
[ "$fail" -eq 0 ]
