#!/usr/bin/env bash
# sim.sh <name> [vcsimenv 옵션...] — vcsim을 백그라운드로 띄우고 주소를 /tmp/sim_<name>.addr 에 기록
set -euo pipefail
H=/root/v2work/harness
name=$1; shift
out=/tmp/sim_$name.log
"$H/bin/vcsimenv" "$@" > "$out" 2>&1 &
echo $! > /tmp/sim_$name.pid
for i in $(seq 1 100); do
  if grep -q '^READY' "$out"; then awk '/^READY/{print $2}' "$out" > /tmp/sim_$name.addr; cat /tmp/sim_$name.addr; exit 0; fi
  sleep 0.2
done
echo "vcsim 기동 실패"; cat "$out"; exit 1
