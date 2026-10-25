#!/usr/bin/env bash
# test/guard_scenarios.sh — vm_power_onoff --demo 에 파이프 입력으로 안전장치 취소/통과 시나리오를 확인한다.
# --demo 는 데몬이 없으므로 기존 방식(2/5 Enter, 3/5 VM 이름 입력)이다. 4/5 의 ls(대상 보기)도 확인한다.
#   bash test/guard_scenarios.sh [바이너리 경로(기본 ./vm_power_onoff)]
# 결과: PASS=N FAIL=M, FAIL 이 있으면 exit 1
set -u
BIN="${1:-./vm_power_onoff}"
[ -x "$BIN" ] || { echo "실행 파일 없음: $BIN" >&2; exit 2; }

export PWONOFF_COLOR=never PWONOFF_TEST_CODE=ABC123 PWONOFF_TEST_TICK_MS=10
unset PWONOFF_TEST_INPUT_TIMEOUT
PASS=0; FAIL=0
OK_HOSTS='web-prd-01\nweb-prd-02\nwas-prd-01\n'

# check 이름 기대종료코드 기대문구 출력 실제종료코드
check() {
  local name="$1" want_rc="$2" want_msg="$3" out="$4" rc="$5"
  if [ "$rc" = "$want_rc" ] && grep -qF -- "$want_msg" <<<"$out"; then
    PASS=$((PASS+1)); echo "[PASS] $name"
  else
    FAIL=$((FAIL+1)); echo "[FAIL] $name (rc=$rc, 기대 rc=$want_rc, 기대 문구: $want_msg)"
    sed 's/^/    | /' <<<"$out" | tail -15
  fi
}

# run 이름 기대종료코드 기대문구 printf형식입력
run() {
  local out rc
  out=$(printf "$4" | "$BIN" --demo 2>&1); rc=$?
  check "$1" "$2" "$3" "$out" "$rc"
}

run "코드 틀림"        1 "[취소] 확인 코드 불일치"   "abc123\n\n${OK_HOSTS}YeS\n"
run "호스트 누락"      1 "누락: was-prd-01"          "ABC123\n\nweb-prd-01 web-prd-02\n\nYeS\n"
run "호스트 초과"      1 "대상 아님: db-prd-01"      "ABC123\n\nweb-prd-01 web-prd-02 was-prd-01 db-prd-01\nYeS\n"
run "호스트 중복"      1 "중복: web-prd-01(2회)"     "ABC123\n\nweb-prd-01 web-prd-01 web-prd-02 was-prd-01\nYeS\n"
run "대소문자 틀림"    1 "[취소] 호스트 목록 불일치" "ABC123\n\nWEB-PRD-01 web-prd-02 was-prd-01\nYeS\n"
run "yes 입력"         1 "[취소] YeS 미입력 (입력값: yes)" "ABC123\n\n${OK_HOSTS}yes\n"
run "YES 입력"         1 "[취소] YeS 미입력 (입력값: YES)" "ABC123\n\n${OK_HOSTS}YES\n"
run "No 입력"          1 "[취소] YeS 미입력 (입력값: No)"  "ABC123\n\n${OK_HOSTS}No\n"
run "정상 YeS"         0 "[DEMO] 실제 전원 작업은 하지 않습니다" "ABC123\n\nwas-prd-01 web-prd-01\tweb-prd-02\nYeS\n"
run "카운트다운 한 줄" 0 "10 .. 9 .. 8 .. 7 .. 6 .. 5 .. 4 .. 3 .. 2 .. 1" "ABC123\n\n${OK_HOSTS}YeS\n"
run "데모: 교차검증 방식 안내" 0 "cross_check=on(기본)이면 2/5 는 확인자 번호 선택" "ABC123\n\n${OK_HOSTS}YeS\n"
run "4/5 ls 후 YeS 통과" 0 "[DEMO] 실제 전원 작업은 하지 않습니다" "ABC123\n\n${OK_HOSTS}ls\nYeS\n"
run "4/5 LS 후 yes 취소" 1 "[취소] YeS 미입력 (입력값: yes)" "ABC123\n\n${OK_HOSTS} LS \nyes\n"

# 4/5 ls 두 번: 목록·총 3대 가 1단계 + 2번 = 3번 나오고, 취소 없이 다시 묻는다
out=$(printf "ABC123\n\n${OK_HOSTS}ls\nls\nYeS\n" | "$BIN" --demo 2>&1); rc=$?
n=$(grep -c '^총 3대$' <<<"$out"); q=$(grep -o 'ls=대상 보기): ' <<<"$out" | wc -l)
if [ "$rc" = 0 ] && [ "$n" = 3 ] && [ "$q" = 3 ]; then
  PASS=$((PASS+1)); echo "[PASS] 4/5 ls 2회 (목록 3번, 질문 3번)"
else
  FAIL=$((FAIL+1)); echo "[FAIL] 4/5 ls 2회 (rc=$rc, 총 3대 $n번, 질문 $q번 — 기대 0/3/3)"
  sed 's/^/    | /' <<<"$out" | tail -15
fi

# 입력 시간 초과: 3단계에서 입력 없이 stdin 을 열어 둔다
out=$( { printf 'ABC123\n\n'; sleep 4; } | PWONOFF_TEST_INPUT_TIMEOUT=2 "$BIN" --demo 2>&1 ); rc=$?
check "입력 시간 초과" 1 "[취소] 입력 시간 2초 초과" "$out" "$rc"

# 카운트다운 중 SIGINT
tmp=$(mktemp)
pidf=$(mktemp)
# 바이너리 자신의 PID 를 남기려고 bash -c 안에서 exec 한다
{ printf "ABC123\n\n${OK_HOSTS}YeS\n"; sleep 5; } |
  PWONOFF_TEST_TICK_MS=500 bash -c 'echo $$ >"$1"; exec "$2" --demo' _ "$pidf" "$BIN" >"$tmp" 2>&1 &
job=$!
sleep 2
kill -INT "$(cat "$pidf")" 2>/dev/null
wait "$job"; rc=$?   # 파이프라인 종료코드 = 마지막(바이너리)
out=$(cat "$tmp"); rm -f "$tmp" "$pidf"
check "카운트다운 중 SIGINT" 1 "[취소] Ctrl+C 로 중단" "$out" "$rc"
if grep -qF " .. 1" <<<"$out"; then
  FAIL=$((FAIL+1)); echo "[FAIL] SIGINT 후에도 카운트다운이 끝까지 진행"
fi

echo "PASS=$PASS FAIL=$FAIL"
[ "$FAIL" -eq 0 ]
