#!/usr/bin/env bash
# e2e-test.sh — 가짜 vCenter(fake-vcenter)로 vm-ip-change 를 처음부터 끝까지 돌리고
# 결과를 자동으로 검증합니다. 무인 실행, 약 20초 x 3. 마지막 줄이 PASS/FAIL.
#
# ev02 우선순위 확인(§3 4단계)은 대상 목록에 ev01/ev02 중 무엇이 있는지에 따라
# 코드 경로가 달라지므로(짝을 list.txt 안에서 찾는지, vCenter 인벤토리에서만
# 찾는지, 애초에 짝이 없는지), 세 가지 대상 구성을 모두 돌립니다:
#   - both      : list.txt 에 ev01/ev02 둘 다 있음
#   - ev02-only : list.txt 에 ev02 만 있음(ev01 은 vCenter 엔 있지만 변경 대상 아님 — 가장 흔한 형태)
#   - ev01-only : ev02 짝 자체가 없음(우선순위 확인이 전혀 동작하면 안 됨)
#
# 각 구성에서 검증하는 것:
#   1) vm-ip-change 종료코드가 1 (시나리오에 실패/전원꺼짐/없는 호스트가 섞여 있으므로)
#   2) vm-ip-change 가 [OK] 로 보고한 수 == 가짜 vCenter 가 실제로 IP 가 바뀐 것을 확인한 수
#   3) 기대와 다른 IP 가 들어간 VM 0, 반쯤 바뀐 VM 0
#
# (ev01 이 짝의 부하 때문에 ev02 를 실제로 후순위로 미루는지는 재확인 주기가 2분이라
# 이 무인 시험 예산(~20초)엔 안 맞습니다 — 사용법.txt [6]의 수동 시험으로 확인하십시오.)
set -euo pipefail
cd "$(dirname "$0")"

bash setup.sh >/dev/null

run_case() {
  local mode=$1 port=$2 out=$3
  rm -rf "$out" "$out".*.log

  ./bin/fake-vcenter -addr "127.0.0.1:$port" -out "$out" -vms 20 -min 200ms -max 800ms \
    -slow 0 -fail 10 -off 5 -missing 1 -busy-ev01 0 -pair-mode "$mode" -seed 7 >"$out.fake.log" 2>&1 &
  local fake=$!

  for _ in $(seq 60); do [ -s "$out/list.txt" ] && break; sleep 1; done
  if [ ! -s "$out/list.txt" ]; then
    kill "$fake" 2>/dev/null || true
    cat "$out.fake.log"; echo "FAIL($mode): 가짜 vCenter 가 뜨지 않음"; return 1
  fi

  export VC_USER=administrator@vsphere.local VC_PASSWORD='VMware1!' GUEST_USER=root GUEST_PASSWORD=guestpass
  set +e
  ./bin/vm-ip-change -vcenter "$out/vcenter.txt" -list "$out/list.txt" >"$out.client.log" 2>&1
  local rc=$?
  set -e

  kill -INT "$fake"
  wait "$fake" || true

  val() { sed -n "s/^$1[^:]*: //p" "$out/report.txt"; }
  local ok_client ok_fake wrong half
  ok_client=$(grep -c '^\[OK\]' "$out.client.log" || true)
  ok_fake=$(val '정상 변경')
  wrong=$(val 'IP 가 기대값과 다름')
  half=$(val 'IP 만 설정되고')

  echo "  [$mode] 종료코드 $rc, [OK] $ok_client / 가짜 vCenter 확인 $ok_fake, 다른 IP $wrong, 반쯤 변경 $half"
  if [ "$rc" = 1 ] && [ "$ok_client" = "$ok_fake" ] && [ "$ok_fake" -gt 0 ] && [ "$wrong" = 0 ] && [ "$half" = 0 ]; then
    return 0
  fi
  echo "--- vm-ip-change 출력 ($out.client.log) ---"; cat "$out.client.log"
  echo "--- 검증 결과 ($out/report.txt) ---"; cat "$out/report.txt"
  return 1
}

fail=0
port=${PORT:-18444}
for mode in both ev02-only ev01-only; do
  if ! run_case "$mode" "$port" "testrun-e2e-$mode"; then
    fail=1
  fi
  port=$((port + 1))
done

if [ "$fail" = 0 ]; then
  echo PASS
else
  echo FAIL
  exit 1
fi
