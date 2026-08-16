#!/usr/bin/env bash
# run-tests.sh — vcsim(127.0.0.1:54321)을 대상으로 vm-param-check의
# [1] 체크만 / [2] 체크+자동교정(-fix, 바이너리 자체 내장, 외부 도구 불필요) 두 시나리오를
# 실행하고, [미지원] 태그(vcsim이 지원하지 않는 3개 필드)가 정상적으로 붙는지까지 확인한다.
#
# 사전조건: ./testkit/build-all.sh 로 빌드 완료, ./testkit/start-vcsim.sh 로 vcsim 기동 중이어야 함.
# 기대값(-cores/-numa/-cpu/-mem/-disk/-shares-*)은 192.168.0.50을 복제한 레시피의 192ev01/192ev02
# 실측 스펙 기준이다. 다른 vCenter를 복제했다면 이 값들을 그 환경에 맞게 바꿔야 한다.
set -uo pipefail
cd "$(dirname "$0")/.."

: "${VC_USER:?VC_USER 환경변수가 필요합니다 (예: administrator@vsphere.local)}"
: "${VC_PASS:?VC_PASS 환경변수가 필요합니다}"

CHECK="./vm-param-check/vm-param-check"
if [ ! -x "$CHECK" ]; then
  echo "오류: $CHECK 이 없습니다. 먼저 ./testkit/build-all.sh 를 실행하세요." >&2
  exit 1
fi

if ! (exec 3<>/dev/tcp/127.0.0.1/54321) 2>/dev/null; then
  echo "오류: 127.0.0.1:54321(vcsim)이 응답하지 않습니다. 먼저 ./testkit/start-vcsim.sh 를 실행하세요." >&2
  exit 1
fi
exec 3<&- 3>&- 2>/dev/null || true

OUT="testkit/out"
mkdir -p "$OUT"
echo "127.0.0.1:54321" > "$OUT/vcenterlist.txt"

FAIL_COUNT=0

run_case() {
  local name="$1"; shift
  echo
  echo "===== [$name] ====="
  if "$@"; then
    echo "[PASS] $name"
  else
    echo "[FAIL] $name (exit=$?)"
    FAIL_COUNT=$((FAIL_COUNT+1))
  fi
}

# ---- [1] 체크만 ----
check_only() {
  "$CHECK" -vcenterList="$OUT/vcenterlist.txt" \
    -ht=on -cores=1 -numa=2 -cpu=2 -mem=4 -disk=40 -shares-ev01=1000 \
    -cpu-ev02=4 -cores-ev02=1 -numa-ev02=4 -mem-ev02=8 -disk-ev02=80 -shares-ev02=1000 \
    -noColor -out="$OUT/check_only.csv"
}
run_case "1-체크만실행" check_only

# ---- [2] 체크 + 자동교정(-fix) ----
# vm-param-check 자체에 -fix가 내장되어 있어 별도 외부 도구(affinity_setting 등) 없이
# 이 한 줄로 게이트검증 -> dry-run -> 적용 -> 재검증까지 전부 처리된다.
# (y/N) 확인 프롬프트는 stdin으로 "y"를 자동 파이핑해서 무인 실행한다.
check_and_fix() {
  echo y | "$CHECK" -vcenterList="$OUT/vcenterlist.txt" \
    -ht=on -cores=1 -numa=2 -cpu=2 -mem=4 -disk=40 -shares-ev01=1000 \
    -cpu-ev02=4 -cores-ev02=1 -numa-ev02=4 -mem-ev02=8 -disk-ev02=80 -shares-ev02=1000 \
    -noColor -out="$OUT/check_and_fix.csv" -fix
}
run_case "2-체크+자동교정" check_and_fix

# ---- [3] [미지원] 태그 검증 ----
# vcsim이 자체 구현하지 않는 3개 필드가 [설정없음]이 아니라 [미지원]으로 표시되는지 확인.
verify_unsupported_tag() {
  local summary="$OUT/check_only_summary.csv"
  if [ ! -f "$summary" ]; then
    echo "요약 CSV가 없습니다: $summary"
    return 1
  fi
  # 요약 CSV 헤더에 "미지원" 컬럼이 있고, 값이 0보다 큰 행이 하나라도 있어야 정상
  if ! grep -q "미지원" "$summary"; then
    echo "요약 CSV에 '미지원' 컬럼 자체가 없습니다 — vm-param-check 버전을 확인하세요."
    return 1
  fi
  # 상세 CSV에 [미지원] 결과값이 실제로 찍혔는지 확인 (3개 필드 전부)
  local detail="$OUT/check_only.csv"
  local missing=0
  for key in "config.memoryReservationLockedToMax" "config.numaInfo.coresPerNumaNode" "cpuid.coresPerSocket"; do
    if ! grep -q "$key.*미지원" "$detail"; then
      echo "  누락: $key 항목이 [미지원]으로 표시되지 않음"
      missing=1
    fi
  done
  [ "$missing" -eq 0 ]
}
run_case "3-미지원태그검증" verify_unsupported_tag

echo
echo "===== 요약 ====="
if [ "$FAIL_COUNT" -eq 0 ]; then
  echo "전체 통과. 결과 파일: $OUT/"
  exit 0
else
  echo "$FAIL_COUNT 건 실패. 위 로그와 $OUT/ 안의 CSV/로그를 확인하세요."
  exit 1
fi
