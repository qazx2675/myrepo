#!/usr/bin/env bash
# real_test.sh — 실제 vCenter(vcenter.txt에 지정된 곳)에 -f 지정 대상 모드로 접속해서,
# --affinity-ev01/--affinity-ev02/--affinity-ev03 파일 옵션이 실제로 동작하는지 확인한다.
# testfiles/ 아래 kdh.txt(대상 hostname) + affinity-ev01/02/03.txt(기대 affinity)를 사용한다.
#
# 사용법: VC_USER=... VC_PASS=... ./real_test.sh
set -euo pipefail
cd "$(dirname "$0")"

: "${VC_USER:?VC_USER 환경변수 필요}"
: "${VC_PASS:?VC_PASS 환경변수 필요}"

OUTDIR="real_test_out"
mkdir -p "$OUTDIR"

echo "1) 빌드"
go build -mod=vendor -o vm-param-check .

echo
echo "2) -f 지정 대상 모드 + affinity-ev01/02/03 전부 지정, full 출력(OK 포함)"
LOG="$OUTDIR/real_test_full.log"
./vm-param-check -f testfiles/kdh.txt \
  --ht=on --cores=1 --numa=1 --cpu=2 --mem=4 --disk=40 \
  --shares-ev01=1000 --shares-ev02=1000 \
  --affinity-ev01=testfiles/affinity-ev01.txt \
  --affinity-ev02=testfiles/affinity-ev02.txt \
  --affinity-ev03=testfiles/affinity-ev03.txt \
  --noColor \
  --out="$OUTDIR/real_test.csv" | tee "$LOG"

echo
echo "=== affinity 관련 결과만 뽑아보기 ==="
grep -i affinity "$LOG" || echo "(affinity 항목 없음)"

echo
echo "=== 요약 ==="
echo "전체 로그: $LOG"
echo "CSV: $OUTDIR/real_test.csv , $OUTDIR/real_test_summary.csv"
echo
echo "기대 결과 해설:"
echo "  - 192ev01: --affinity-ev01 파일이 단일코어핀(0/1)이라 실제값(0,1 / 2,3)과 달라 FAIL이 나와야 함"
echo "            -> 파일이 자동계산 대신 실제로 쓰였다는 증거"
echo "  - 192ev02: --affinity-ev02 파일이 실제값과 일치(0,1 / 2,3)해서 OK가 나와야 함"
echo "  - ev03: 대상 VM 자체가 없어서 --affinity-ev03 옵션을 줬어도 체크 자체가 안 나와야 함 (자연 스킵)"
