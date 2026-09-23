#!/usr/bin/env bash
# e2e-test.sh — 가짜 vCenter(fake-vcenter)로 vm-ip-change 를 처음부터 끝까지 돌리고
# 결과를 자동으로 검증합니다. 무인 실행, 약 20초. 마지막 줄이 PASS/FAIL.
#
# 검증 항목:
#   1) vm-ip-change 종료코드가 1 (시나리오에 실패/전원꺼짐/없는 호스트가 섞여 있으므로)
#   2) vm-ip-change 가 [OK] 로 보고한 수 == 가짜 vCenter 가 실제로 IP 가 바뀐 것을 확인한 수
#   3) 기대와 다른 IP 가 들어간 VM 0, 반쯤 바뀐 VM 0
set -euo pipefail
cd "$(dirname "$0")"

bash setup.sh >/dev/null

out=testrun-e2e
port=${PORT:-18444}
rm -rf "$out" "$out".*.log

./bin/fake-vcenter -addr "127.0.0.1:$port" -out "$out" -vms 20 -min 200ms -max 800ms \
  -slow 0 -fail 10 -off 5 -missing 1 -seed 7 >"$out.fake.log" 2>&1 &
fake=$!
trap 'kill "$fake" 2>/dev/null || true' EXIT

for _ in $(seq 60); do [ -s "$out/list.txt" ] && break; sleep 1; done
[ -s "$out/list.txt" ] || { cat "$out.fake.log"; echo "FAIL: 가짜 vCenter 가 뜨지 않음"; exit 1; }

export VC_USER=administrator@vsphere.local VC_PASSWORD='VMware1!' GUEST_USER=root GUEST_PASSWORD=guestpass
set +e
./bin/vm-ip-change -vcenter "$out/vcenter.txt" -list "$out/list.txt" >"$out.client.log" 2>&1
rc=$?
set -e

kill -INT "$fake"
wait "$fake" || true
trap - EXIT

val() { sed -n "s/^$1[^:]*: //p" "$out/report.txt"; }
ok_client=$(grep -c '^\[OK\]' "$out.client.log" || true)
ok_fake=$(val '정상 변경')
wrong=$(val 'IP 가 기대값과 다름')
half=$(val 'IP 만 설정되고')

echo "vm-ip-change 종료코드 $rc, [OK] $ok_client / 가짜 vCenter 확인 $ok_fake, 다른 IP $wrong, 반쯤 변경 $half"
if [ "$rc" = 1 ] && [ "$ok_client" = "$ok_fake" ] && [ "$ok_fake" -gt 0 ] && [ "$wrong" = 0 ] && [ "$half" = 0 ]; then
  echo PASS
else
  echo "--- vm-ip-change 출력 ($out.client.log) ---"; cat "$out.client.log"
  echo "--- 검증 결과 ($out/report.txt) ---"; cat "$out/report.txt"
  echo FAIL
  exit 1
fi
