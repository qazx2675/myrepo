#!/usr/bin/env bash
# selftest.sh — pcisim 자체 점검. 새로 띄워서(-rebootDevs=0000:3b:00.1) selftest 를 돌리고 끈다.
# 이미 떠 있는 pcisim 은 먼저 끈다 (상태가 섞이면 결과가 달라짐).
set -euo pipefail
cd "$(dirname "$0")"

bash pcisim.sh stop >/dev/null || true
bash pcisim.sh start -rebootDevs=0000:3b:00.1 >/dev/null
eval "$(bash pcisim.sh env)"
trap 'bash pcisim.sh stop >/dev/null' EXIT

rc=0
go run ./selftest || rc=1

echo "--- pcisim.log 전원 판정"
grep '\[POWERON\]' pcisim.log | sed 's/^/  /' | cut -c1-220
if grep -q 'MGMT_NIC_PASSTHRU' pcisim.log; then
  echo "FAIL 관리망 NIC 를 패스스루로 켠 기록이 있음"; rc=1
fi
exit $rc
