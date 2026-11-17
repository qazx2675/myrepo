#!/bin/bash
# 업로더 E2E: test/e2e_send.sh <config> <파일폴더>
# 파일마다 ./awx_copy 를 실행하고 시간, 전후 variables 상태를 기록한다.
# 다운로더(awx_fetch.exe)는 윈도우에서 같은 수만큼 반복 실행해 둔다.
set -u
CONF="$1"; DIR="$2"
ROOT="$(cd "$(dirname "$(readlink -f "$0")")/.." && pwd)"
AC="$ROOT/awx_copy"
BEFORE=$("$AC" --status --config "$CONF")
echo "시작 전: $BEFORE"
fail=0
for f in "$DIR"/*; do
    typ=txt; case "$f" in *.csv) typ=csv ;; esac
    t0=$(date +%s.%N)
    if "$AC" "$f" "$typ" --config "$CONF" > /tmp/awx_e2e_send.log 2>&1; then rc=0; else rc=$?; fail=1; fi
    t1=$(date +%s.%N)
    st=$("$AC" --status --config "$CONF")
    same=NO; [ "$st" = "$BEFORE" ] && same=YES
    printf 'RESULT\t%s\trc=%d\t%.2fs\tchunks=%s\trestored=%s\n' "$(basename "$f")" "$rc" "$(echo "$t1 - $t0" | bc)" \
        "$(grep -c '전송 중' /tmp/awx_e2e_send.log)" "$same"
    [ $rc -ne 0 ] && cat /tmp/awx_e2e_send.log
    sleep 1
done
echo "종료 후: $("$AC" --status --config "$CONF")"
exit $fail
