#!/usr/bin/env bash
# scale_test.sh — vm-param-check을 대규모(N대) 합성 VM으로 돌려서 콘솔/CSV 출력이
# 실제 운영 규모에서 어떻게 보이는지 바로 확인하기 위한 테스트 스크립트.
# vCenter에는 연결하지 않는다 (-scale=N, 실제 인프라 무관).
#
# 사용법: ./scale_test.sh [N]   (기본 N=1000)
set -euo pipefail
cd "$(dirname "$0")"

N="${1:-1000}"
OUTDIR="scale_test_out"
mkdir -p "$OUTDIR"

echo "1) 빌드 (오프라인, vendor 사용)"
go build -mod=vendor -o vm-param-check .

echo
echo "2) 전체 출력(색상 포함) — 콘솔 그대로: ${N}대"
FULL_LOG="$OUTDIR/scale_${N}_full.log"
./vm-param-check -scale="$N" --out="$OUTDIR/scale_${N}.csv" > "$FULL_LOG"
FULL_LINES=$(wc -l < "$FULL_LOG")
echo "   -> 콘솔 전체 출력 라인 수: ${FULL_LINES}줄 (저장: ${FULL_LOG})"

echo
echo "3) -onlyFail 적용 — 문제 있는 VM만: ${N}대"
FAIL_LOG="$OUTDIR/scale_${N}_onlyfail.log"
./vm-param-check -scale="$N" --onlyFail --noColor --out="$OUTDIR/scale_${N}_onlyfail.csv" > "$FAIL_LOG"
FAIL_LINES=$(wc -l < "$FAIL_LOG")
echo "   -> -onlyFail 콘솔 출력 라인 수: ${FAIL_LINES}줄 (저장: ${FAIL_LOG})"

echo
echo "=== [미리보기] 전체 출력 상단 40줄 ==="
head -n 40 "$FULL_LOG"
echo "... (중략) ..."
echo "=== [미리보기] 전체 출력 하단 20줄 ==="
tail -n 20 "$FULL_LOG"

echo
echo "=== [미리보기] -onlyFail 출력 상단 60줄 ==="
head -n 60 "$FAIL_LOG"

echo
echo "=== CSV 크기 ==="
ls -la "$OUTDIR"/scale_${N}*.csv 2>/dev/null | awk '{print "  " $NF, "(" $5 " bytes)"}'
wc -l "$OUTDIR"/scale_${N}.csv "$OUTDIR"/scale_${N}_summary.csv 2>/dev/null

echo
echo "=== 요약 ==="
echo "VM 대수: ${N}"
echo "전체 콘솔 출력: ${FULL_LINES}줄 -> ${FULL_LOG}"
echo "onlyFail 콘솔 출력: ${FAIL_LINES}줄 -> ${FAIL_LOG}"
echo "직접 보려면: less -R ${FULL_LOG}  또는  less -R ${FAIL_LOG}"
