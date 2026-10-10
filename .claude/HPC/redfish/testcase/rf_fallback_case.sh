#!/usr/bin/env bash
# rf_fallback_case.sh — 2순위(예비) 계정 + 설정 표기 선택 기능을 직접 확인해 보는 테스트케이스.
#
# 실제 BMC 에는 접속하지 않습니다. 127.0.0.1 의 포트 19101~19106 에 mock BMC(bin/mockbmc) 를 띄우고
# bin/biostool 로 점검해, 시나리오별 기대 결과를 PASS/FAIL 로 판정합니다. Go 가 필요 없습니다.
#
# 사용법:
#   bash testcase/rf_fallback_case.sh            # 실행 (저장소 어느 위치에서든 가능)
#   bash testcase/rf_fallback_case.sh -v         # 시나리오별 biostool 출력 전체를 보여 줌
#   RF_FORCE_OS6=1 bash testcase/rf_fallback_case.sh   # 커널과 상관없이 *_os6 바이너리로 시험
#
# 종료 코드: 모두 PASS 면 0, FAIL 이 하나라도 있으면 1, 준비 실패는 2.
# /etc/rf_tool/rf.conf 는 건드리지 않습니다 (임시 폴더의 rf.conf 를 -rf-conf 로 지정).
# bash 4.1(RHEL6) 호환: 연관 배열·${var,,} 등 4.x 후반 문법을 쓰지 않습니다.
set -u

VERBOSE=0
[ "${1:-}" = "-v" ] && VERBOSE=1

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT" || exit 2

# ---- 바이너리 선택 (lib.sh 와 같은 규칙: 커널 메이저 < 3 이면 *_os6) ----
kmajor="$(uname -r | cut -d. -f1)"
case "$kmajor" in '' | *[!0-9]*) kmajor=99 ;; esac
SUF=""
if [ "$kmajor" -lt 3 ] || [ "${RF_FORCE_OS6:-}" = "1" ]; then SUF="_os6"; fi
BIOS="$ROOT/bin/biostool$SUF"
MOCK="$ROOT/bin/mockbmc$SUF"
chmod +x "$BIOS" "$MOCK" 2>/dev/null || true
for b in "$BIOS" "$MOCK"; do
  if [ ! -x "$b" ]; then
    echo "오류: $b 이(가) 없거나 실행할 수 없습니다 (bin/ 디렉터리째 내려받았는지 확인)." >&2
    exit 2
  fi
done

# ---- 임시 작업 폴더·종료 정리 ----
W="$(mktemp -d 2>/dev/null || mktemp -d -t rfcase)"
PIDS=""
cleanup() {
  for p in $PIDS; do kill "$p" 2>/dev/null; done
  for p in $PIDS; do wait "$p" 2>/dev/null; done
  rm -rf "$W"
}
trap cleanup EXIT
trap 'exit 2' INT TERM

# ---- 시험용 계정 (mock 전용 값. 실제 계정이 아님) ----
PRIMARY_USER="biosadmin"
PRIMARY_PW="Primary-Test-1"
HPE_FB_USER="hpe_backup"
HPE_FB_PW="HpeBackup-Test-2"
LNV_REAL_USER="lenovo_real"
LNV_REAL_PW="LenovoReal-Test-3"
LNV_FB_USER="lenovo_backup"       # rf.conf 에는 있지만 BMC 에는 없는 계정
LNV_FB_PW="LenovoWrong-Test-4"

TREES="$ROOT/testdata"
RESULTS=""   # 결과 표 (줄 단위 "번호|이름|판정|메모")
NFAIL=0
NSKIP=0

say() { echo "$@"; }
step() { echo; echo "[$1] $2"; }

record() { # <번호> <이름> <PASS|FAIL|SKIP> <메모>
  RESULTS="$RESULTS$1|$2|$3|$4
"
  case "$3" in
    FAIL) NFAIL=$((NFAIL + 1)) ;;
    SKIP) NSKIP=$((NSKIP + 1)) ;;
  esac
  echo "    -> $3  $4"
}

# mock BMC 를 띄운다: start_mock <트리 경로> <포트> <계정> <비밀번호> [추가 옵션...]
start_mock() {
  local dir="$1" port="$2" user="$3" pw="$4"
  shift 4
  MOCKBMC_PASS="$pw" "$MOCK" -dir "$dir" -listen "127.0.0.1:$port" -user "$user" "$@" >"$W/mock_$port.log" 2>&1 &
  PIDS="$PIDS $!"
  local i=0
  while [ $i -lt 50 ]; do
    if grep -q "서빙합니다" "$W/mock_$port.log" 2>/dev/null; then return 0; fi
    sleep 0.1
    i=$((i + 1))
  done
  echo "오류: mock BMC(포트 $port)가 시작되지 않았습니다:" >&2
  cat "$W/mock_$port.log" >&2
  return 1
}

# 리포트의 한 구역(헤더 줄부터 빈 줄 또는 다음 [구역] 전까지)에 호스트가 있는지: in_section <파일> <헤더 정규식> <호스트>
in_section() {
  awk -v hdr="$2" '
    $0 ~ hdr { f = 1; next }
    f && (/^[[:space:]]*$/ || /^\[/) { f = 0 }
    f
  ' "$1" | grep -qF "$3"
}

# 호스트의 result.tsv 상태 값들(3번째 이후 열 어딘가)을 확인: row_has <result.tsv> <호스트> <문자열>
row_has() { grep -F "$2" "$1" | grep -qF "$3"; }

latest_dir() { ls -d "$1"/results/*/ 2>/dev/null | tail -n 1; }

# ---- mock 이 ServiceRoot 비인증 조회를 지원하는지 ----
OPEN_FLAG=""
if "$MOCK" -h 2>&1 | grep -q -- "-open-serviceroot"; then
  OPEN_FLAG="-open-serviceroot"
fi

say "================================================================"
say " 2순위(예비) 계정 + 설정 표기 선택 테스트케이스"
say "================================================================"
say " 바이너리 : bin/biostool$SUF , bin/mockbmc$SUF (Go 불필요, 실제 BMC 접속 없음)"
say " 작업 폴더: 임시 폴더 (종료 시 자동 삭제), /etc/rf_tool/rf.conf 는 건드리지 않음"
if [ -z "$OPEN_FLAG" ]; then
  say " 알림     : 이 mockbmc 에는 -open-serviceroot 플래그가 없어 비인증 ServiceRoot 가 401 입니다."
  say "            -> 시나리오 2(예비 계정 성공)는 벤더 식별이 불가능해 SKIP 됩니다 (mockbmc 에 플래그 추가 후 활성)."
fi

# ---- 입력 파일 준비 ----
step 0 "준비: bios.conf / user.txt / 프로파일 / rf.conf / 암호화된 비밀번호 만들기"
mkdir -p "$W/profiles" "$W/results"
: >"$W/hosts"
cat >"$W/bios.conf" <<EOF
user=$PRIMARY_USER
pass_file=$W/pass.enc
key_file=$W/key.bin
concurrency=5
timeout=5
insecure=true
retries=0
auth_fail_stop=0
result_dir=$W/results
dump_dir=$W/dumps
profile_dir=$W/profiles
EOF
printf '%s' "$PRIMARY_PW" | "$BIOS" encrypt -key "$W/key.bin" -out "$W/pass.enc" >/dev/null || {
  echo "오류: 비밀번호 암호화 실패" >&2
  exit 2
}
cat >"$W/user.txt" <<EOF
127.0.0.1:19101
127.0.0.1:19102
127.0.0.1:19103
127.0.0.1:19104
127.0.0.1:19105
EOF
printf 'vendor\tmodel\tstd_name\tattribute\tvalue\tverified\n' >"$W/profiles/TC.tsv"
printf 'DELL\tR660\tsystem_profile\tSysProfile\tPerfOptimized\tY\n' >>"$W/profiles/TC.tsv"
printf 'DELL\tR660\thyper_threading\tLogicalProc\tEnabled\tY\n' >>"$W/profiles/TC.tsv"
printf 'DELL\tR660\tllc_prefetch\tLlcPrefetch\tEnabled\tY\n' >>"$W/profiles/TC.tsv"
printf 'DELL\tR660\tsub_numa_cluster\tSubNumaCluster\tDisabled\tY\n' >>"$W/profiles/TC.tsv"
printf 'HPE\tDL360 Gen11\thyper_threading\tProcHyperthreading\tEnabled\tY\n' >>"$W/profiles/TC.tsv"
printf 'HPE\tDL360 Gen11\tsystem_profile\tWorkloadProfile\tGeneralPowerEfficientCompute|GeneralThroughputCompute\tY\n' >>"$W/profiles/TC.tsv"
printf 'HPE\tDL360 Gen11\tllc_prefetch\tLlcPrefetch\tEnabled\tY\n' >>"$W/profiles/TC.tsv"
printf 'HPE\tDL360 Gen11\tsub_numa_cluster\tSubNumaClustering\tDisabled\tY\n' >>"$W/profiles/TC.tsv"
printf 'Lenovo\tSR650 V3\thyper_threading\tProcessors_HyperThreading\tEnable\tY\n' >>"$W/profiles/TC.tsv"
printf 'Supermicro\tX12DPi-N6\tsystem_profile\tPowerTechnology\tPerformance\tN\n' >>"$W/profiles/TC.tsv"
cat >"$W/rf.conf" <<EOF
# 시험용 rf.conf (임시 파일)
hpe_user=$HPE_FB_USER
hpe_pass=$HPE_FB_PW
lenovo_user=$LNV_FB_USER
lenovo_pass=$LNV_FB_PW
EOF
chmod 600 "$W/rf.conf"
say "    대상 5대: 127.0.0.1:19101..19105 (19104 는 아무도 듣지 않음)"
say "    1순위 계정: bios.conf 의 user + pass.enc / 2순위: 임시 rf.conf (hpe, lenovo 만)"

# ---- mock 기동 ----
step 0 "mock BMC 기동 (포트 19101~19106)"
start_mock "$TREES/dell-r660" 19101 "$PRIMARY_USER" "$PRIMARY_PW" $OPEN_FLAG || exit 2
start_mock "$TREES/hpe-dl360gen11" 19102 "$HPE_FB_USER" "$HPE_FB_PW" $OPEN_FLAG || exit 2
start_mock "$TREES/lenovo-sr650v3" 19103 "$LNV_REAL_USER" "$LNV_REAL_PW" $OPEN_FLAG || exit 2
start_mock "$TREES/supermicro-x12" 19105 "$PRIMARY_USER" "$PRIMARY_PW" $OPEN_FLAG || exit 2
say "    19101 dell(1순위 계정 유효) / 19102 hpe(예비 계정만 유효) / 19103 lenovo(둘 다 무효)"
say "    19104 (없음) / 19105 supermicro(1순위 유효)"

# ---- 점검 실행 (시나리오 1~5 한 번에) ----
step 1-5 "biostool check 실행 (설정은 하지 않음: -no-prompt)"
OUT="$W/out_check.txt"
"$BIOS" check -profile TC -conf "$W/bios.conf" -user "$W/user.txt" -hosts "$W/hosts" \
  -rf-conf "$W/rf.conf" -no-prompt >"$OUT" 2>&1
if [ "$VERBOSE" = "1" ]; then
  sed 's/^/    | /' "$OUT"
fi
RD="$(latest_dir "$W")"
TSV="$RD/result.tsv"
FB="$RD/fallback_login.txt"
if [ ! -f "$TSV" ]; then
  echo "오류: 결과 파일을 찾을 수 없습니다 ($RD). biostool 출력:" >&2
  cat "$OUT" >&2
  exit 2
fi

step 1 "dell-r660 (19101): 1순위 계정 정상 -> 일반 점검, 2순위 목록에 없어야 함"
if grep -qF "127.0.0.1:19101" "$RD/ok.txt" 2>/dev/null && ! in_section "$OUT" '2순위 계정' "19101" \
  && { [ ! -f "$FB" ] || ! grep -qF "19101" "$FB"; }; then
  record 1 "1순위 정상" PASS "19101 점검 OK, 2순위 목록에 없음"
else
  record 1 "1순위 정상" FAIL "19101 이 OK 가 아니거나 2순위 목록에 있음"
fi

step 2 "hpe-dl360gen11 (19102): 1순위 계정 거부, 예비(hpe_user)만 유효 -> 점검 진행 + 2순위 구역/파일에 표시"
if [ -z "$OPEN_FLAG" ]; then
  record 2 "예비 계정으로 접속" SKIP "mockbmc 에 -open-serviceroot 플래그 추가 후 활성"
elif in_section "$OUT" '2순위 계정' "19102" && [ -f "$FB" ] && grep -qF "19102" "$FB" \
  && ! row_has "$TSV" "127.0.0.1:19102" "AUTH_FAIL" && grep -qF "127.0.0.1:19102" "$RD/ok.txt"; then
  record 2 "예비 계정으로 접속" PASS "19102 가 [2순위 계정] 구역과 fallback_login.txt 에 있고 점검이 진행됨"
else
  record 2 "예비 계정으로 접속" FAIL "2순위 구역/파일에 없거나 AUTH_FAIL"
fi

step 3 "lenovo-sr650v3 (19103): 1순위·예비 모두 무효 -> [계정 오류 AUTH_FAIL]"
if in_section "$OUT" '계정 오류' "19103" && row_has "$TSV" "127.0.0.1:19103" "AUTH_FAIL" \
  && { [ ! -f "$FB" ] || ! grep -qF "19103" "$FB"; }; then
  record 3 "둘 다 실패" PASS "19103 이 [계정 오류 AUTH_FAIL] 에 표시됨"
else
  record 3 "둘 다 실패" FAIL "19103 이 계정 오류 구역에 없음"
fi

step 4 "19104: 아무도 듣지 않음 -> [접속불가] (계정 오류와 구분)"
if in_section "$OUT" '접속불가' "19104" && ! in_section "$OUT" '계정 오류' "19104"; then
  record 4 "접속불가 구분" PASS "19104 가 [접속불가] 에만 표시됨"
else
  record 4 "접속불가 구분" FAIL "19104 가 [접속불가] 에 없거나 계정 오류에 섞임"
fi

step 5 "supermicro-x12 (19105): 1순위 정상, 값이 달라(기대 Performance / 현재 Custom, verified=N) UNVERIFIED 가 나와야 함"
if grep -F "127.0.0.1:19105" "$TSV" | grep -F "PowerTechnology" | grep -qF "UNVERIFIED"; then
  record 5 "값 불일치 표시" PASS "19105 PowerTechnology(기대 Performance, 현재 Custom)가 UNVERIFIED"
else
  record 5 "값 불일치 표시" FAIL "19105 PowerTechnology 가 UNVERIFIED 가 아님"
fi

# ---- 시나리오 6: 표기 선택 ----
step 6 "허용값 표기 선택: 프로파일 'Enabled|Enable', BMC 레지스트리는 Enable/Disable"
cp -R "$TREES/lenovo-sr650v3" "$W/lenovo_tree"
BJ="$W/lenovo_tree/redfish/v1/Systems/1/Bios.json"
sed 's/"Processors_HyperThreading": "Enable"/"Processors_HyperThreading": "Disable"/' "$BJ" >"$BJ.new" && mv "$BJ.new" "$BJ"
printf 'vendor\tmodel\tstd_name\tattribute\tvalue\tverified\n' >"$W/profiles/TC6.tsv"
printf 'LENOVO\tSR650 V3\thyper_threading\tProcessors_HyperThreading\tEnabled|Enable\tY\n' >>"$W/profiles/TC6.tsv"
printf '127.0.0.1:19106\n' >"$W/user6.txt"
start_mock "$W/lenovo_tree" 19106 "$PRIMARY_USER" "$PRIMARY_PW" || exit 2
OUT6="$W/out_dry.txt"
"$BIOS" check -profile TC6 -conf "$W/bios.conf" -user "$W/user6.txt" -hosts "$W/hosts" \
  -rf-conf "$W/rf.conf" -dry-run >"$OUT6" 2>&1
if [ "$VERBOSE" = "1" ]; then
  sed 's/^/    | /' "$OUT6"
fi
if grep -q '"Processors_HyperThreading": *"Enable"' "$OUT6" && ! grep -q '"Processors_HyperThreading": *"Enabled"' "$OUT6"; then
  record 6 "표기 선택" PASS 'dry-run 본문에 "Processors_HyperThreading":"Enable" (Enabled 아님)'
else
  record 6 "표기 선택" FAIL "dry-run 본문에 Enable 이 없거나 Enabled 가 들어감"
fi

# ---- 시나리오 7~10: 1순위 계정 생성(create) / 비밀번호 변경 dry-run ----
DELL_FB_USER="dell_backup"
DELL_FB_PW="DellBackup-Test-5"
cat >"$W/rf_acct.conf" <<EOF
# 계정 생성 시험용 rf.conf (임시 파일)
hpe_user=$HPE_FB_USER
hpe_pass=$HPE_FB_PW
dell_user=$DELL_FB_USER
dell_pass=$DELL_FB_PW
EOF
chmod 600 "$W/rf_acct.conf"
ACCT_OK=1
if ! "$MOCK" -h 2>&1 | grep -q -- "-accounts"; then ACCT_OK=0; fi
if [ -z "$OPEN_FLAG" ]; then ACCT_OK=0; fi

# 한 호스트에 대해 account 명령을 실행하는 도우미: acct_run <파일 접두사> <포트> <추가 인자...> (표준입력은 빈 줄 = 기본 Y)
acct_run() {
  local tag="$1" port="$2"
  shift 2
  printf '127.0.0.1:%s\n' "$port" >"$W/user_$tag.txt"
  printf '\n' | "$BIOS" account "$@" -conf "$W/bios.conf" -user "$W/user_$tag.txt" -hosts "$W/hosts" \
    -rf-conf "$W/rf_acct.conf" >"$W/out_$tag.txt" 2>&1
  if [ "$VERBOSE" = "1" ]; then sed 's/^/    | /' "$W/out_$tag.txt"; fi
}

step 7 "account create: hpe(19107) 에 1순위 계정이 없음 -> 기본 Y 로 생성, 이후 점검은 1순위 계정으로 접속"
if [ "$ACCT_OK" = "0" ]; then
  record 7 "계정 생성" SKIP "mockbmc 에 -accounts / -open-serviceroot 플래그가 없음"
else
  start_mock "$TREES/hpe-dl360gen11" 19107 "$HPE_FB_USER" "$HPE_FB_PW" $OPEN_FLAG -accounts || exit 2
  acct_run c7 19107 create -stdin-ok
  AR="$(latest_dir "$W")/account_result.tsv"
  printf '127.0.0.1:19107\n' >"$W/user7b.txt"
  "$BIOS" check -profile TC -conf "$W/bios.conf" -user "$W/user7b.txt" -hosts "$W/hosts" -rf-conf "$W/rf_acct.conf" -no-prompt >"$W/out_c7b.txt" 2>&1
  if grep -q '\[CREATED\]' "$W/out_c7.txt" && grep -qF "CREATED" "$AR" 2>/dev/null \
    && ! grep -qF "$PRIMARY_PW" "$W/out_c7.txt" "$AR" && ! grep -qF "$HPE_FB_PW" "$W/out_c7.txt" "$AR" \
    && ! grep -q '2순위 계정(rf.conf)으로 접속' "$W/out_c7b.txt"; then
    record 7 "계정 생성" PASS "19107 [CREATED], 비밀번호 비노출, 이후 점검이 1순위 계정으로 접속"
  else
    record 7 "계정 생성" FAIL "CREATED 가 아니거나 비밀번호가 출력됨 / 점검이 여전히 2순위로 접속"
  fi
fi

step 8 "account create: 이미 같은 id 가 있으면 건너뜀 (덮어쓰지 않음)"
if [ "$ACCT_OK" = "0" ]; then
  record 8 "중복 건너뜀" SKIP "mockbmc 에 -accounts 플래그가 없음"
else
  acct_run c8 19107 create -stdin-ok
  if grep -qE 'PRIMARY_OK|SKIP_EXISTS|생성할 호스트가 없습니다' "$W/out_c8.txt" && ! grep -q '\[CREATED\]' "$W/out_c8.txt"; then
    record 8 "중복 건너뜀" PASS "재실행 시 만들지 않음 (1순위 계정으로 이미 접속됨)"
  else
    record 8 "중복 건너뜀" FAIL "이미 있는데 다시 만들려 함"
  fi
fi

step 9 "account create: 빈 슬롯 방식(Dell, POST 불가) -> 슬롯 PATCH 로 생성"
if [ "$ACCT_OK" = "0" ]; then
  record 9 "슬롯 방식" SKIP "mockbmc 에 -accounts 플래그가 없음"
else
  start_mock "$TREES/dell-r660" 19108 "$DELL_FB_USER" "$DELL_FB_PW" $OPEN_FLAG -accounts -account-slots 4 -account-slot-style || exit 2
  acct_run c9 19108 create -stdin-ok
  AR9="$(latest_dir "$W")/account_result.tsv"
  if grep -q '\[CREATED\]' "$W/out_c9.txt" && grep -F "127.0.0.1:19108" "$AR9" | grep -qF "PATCH"; then
    record 9 "슬롯 방식" PASS "19108 가 빈 슬롯 PATCH 로 CREATED"
  else
    record 9 "슬롯 방식" FAIL "슬롯 PATCH 로 생성되지 않음"
  fi
fi

step 10 "account create: 새 계정 로그인 검증 실패 -> 만든 계정만 롤백 (VERIFY_FAIL_ROLLED_BACK)"
if [ "$ACCT_OK" = "0" ]; then
  record 10 "검증 실패 롤백" SKIP "mockbmc 에 -accounts 플래그가 없음"
else
  start_mock "$TREES/hpe-dl360gen11" 19109 "$HPE_FB_USER" "$HPE_FB_PW" $OPEN_FLAG -accounts -account-deny-new-login || exit 2
  acct_run c10 19109 create -stdin-ok
  if grep -q 'VERIFY_FAIL_ROLLED_BACK' "$W/out_c10.txt" && ! grep -q '\[CREATED\]' "$W/out_c10.txt"; then
    record 10 "검증 실패 롤백" PASS "19109 가 VERIFY_FAIL_ROLLED_BACK (만든 계정을 되돌림)"
  else
    record 10 "검증 실패 롤백" FAIL "롤백 표시가 없음"
  fi
fi

step 11 "account change -dry-run: 새 비밀번호 입력 없이 대상·요청 형태만 확인 (쓰기 없음)"
if [ "$ACCT_OK" = "0" ]; then
  record 11 "변경 dry-run" SKIP "mockbmc 에 -accounts 플래그가 없음"
else
  acct_run c11 19107 change -dry-run
  if grep -q 'WOULD_CHANGE' "$W/out_c11.txt" && ! grep -qF "$PRIMARY_PW" "$W/out_c11.txt"; then
    record 11 "변경 dry-run" PASS "19107 WOULD_CHANGE, 비밀번호 비노출 (실제 변경은 터미널에서 직접 입력해야 함)"
  else
    record 11 "변경 dry-run" FAIL "WOULD_CHANGE 가 아니거나 비밀번호가 출력됨"
  fi
fi

# ---- 결과 표 ----
echo
echo "================================================================"
echo " 결과"
echo "================================================================"
printf '%s' "$RESULTS" | while IFS='|' read -r n name res memo; do
  [ -n "$n" ] && printf ' 시나리오 %s  %-5s %s — %s\n' "$n" "$res" "$name" "$memo"
done
echo
if [ "$NFAIL" -eq 0 ]; then
  echo "종합: PASS (SKIP ${NSKIP}건)"
  exit 0
fi
echo "종합: FAIL (${NFAIL}건 실패, SKIP ${NSKIP}건). -v 로 다시 실행하면 biostool 출력 전체를 볼 수 있습니다."
exit 1
