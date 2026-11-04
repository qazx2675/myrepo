#!/bin/bash
# run_tests.sh - new_setup 종단 시험 (fake_gossh 사용). 랩에서 실행: bash test/run_tests.sh
set -u
HERE=$(cd "$(dirname "$0")" && pwd -P)
ROOT=$(cd "$HERE/.." && pwd -P)
FAKE=$HERE/fake_gossh.sh
chmod +x "$FAKE"
T=$(mktemp -d)
trap 'rm -rf "$T"' EXIT

PASS=0
FAIL=0
ok() { PASS=$((PASS + 1)); echo "PASS: $1"; }
ng() { FAIL=$((FAIL + 1)); echo "FAIL: $1"; [ -n "${2:-}" ] && printf '%s\n' "$2" | sed 's/^/    /'; }
check() { # 이름 기대 실제
	if [ "$2" = "$3" ]; then ok "$1"; else ng "$1" "기대: [$2]"$'\n'"실제: [$3]"; fi
}

echo "== 빌드"
(cd "$ROOT" && go build -o "$T/new_setup" .) || { echo "FAIL: go build 실패"; exit 1; }
BIN=$T/new_setup

TAB=$'\t'
HF=$T/hosts
SC=$T/scenario
printf '%s\n' "# 주석 줄" "h_ok" "h_off" "h_ref" "" "h_disk" "h_err" "h_ok" "h_none" "h_nolog" "h_retryoff" "h_cancel" > "$HF"
printf '%s\t%s\n' h_ok ok h_off off h_ref refused h_disk install_disk h_err install_err \
	h_none install_none h_nolog install_nolog h_retryoff install_retry_off h_cancel cancel > "$SC"

mkconf() { # 파일 [ERROR_PATTERN 줄...]
	local f=$1 p
	shift
	{
		echo "GOSSH=$FAKE"
		echo 'DISK_PATTERN=Disk "" given in clearpart command does not exist'
		for p in "$@"; do echo "ERROR_PATTERN=$p"; done
	} > "$f"
}

EXPECT=$(printf '%s\n' \
	"h_ok${TAB}완료" "h_off${TAB}접속불가" "h_ref${TAB}접속불가" "h_disk${TAB}디스크미인식" \
	"h_err${TAB}설치정지" "h_none${TAB}OS설치중" "h_nolog${TAB}OS설치중" \
	"h_retryoff${TAB}OS설치중" "h_cancel${TAB}접속불가")

export FAKE_GOSSH_SCENARIO=$SC
export FAKE_GOSSH_LOG=$T/gossh_calls.log

echo "== 기본 시나리오 (ERROR_PATTERN 있음)"
mkconf "$T/a.conf" "Fatal error in anaconda"
: > "$FAKE_GOSSH_LOG"
# 이전 실행 잔여물이 이번 결과에 섞이지 않는지 확인용 (h_ok 가 설치중으로 보이면 안 됨)
echo "h_ok" > "${HF}_os_install"
OUT=$("$BIN" -c "$T/a.conf" "$HF" 2> "$T/a.err")
RC=$?
check "종료코드 0" 0 "$RC"
check "stdout 기대값(입력 순서, 중복 제거, 주석 무시, 잔여 파일 무시)" "$EXPECT" "$OUT"
BAD=$(printf '%s\n' "$OUT" | awk -F'\t' 'NF!=2')
check "모든 줄이 탭 1개(2필드)" "" "$BAD"
check "헤더 없음" "h_ok${TAB}완료" "$(printf '%s\n' "$OUT" | head -n1)"
if [ -f "${HF}_new_setup.log" ]; then ok "로그 파일 생성"; else ng "로그 파일 생성"; fi
if [ -e "${HF}_new_setup_retry" ]; then ng "재시도 임시 파일 정리"; else ok "재시도 임시 파일 정리"; fi
RL=$(find "$T" -maxdepth 1 -name 'hosts_new_setup_retry*')
check "재시도 결과 파일 정리" "" "$RL"
if grep -q NS_ERR "$FAKE_GOSSH_LOG"; then ok "2차 명령에 NS_ERR 포함"; else ng "2차 명령에 NS_ERR 포함"; fi
if grep -q NS_DISK "$FAKE_GOSSH_LOG"; then ok "2차 명령에 NS_DISK 포함"; else ng "2차 명령에 NS_DISK 포함"; fi
rm -f "${HF}_os_install" "${HF}_new_setup.log"

echo "== ERROR_PATTERN 없음"
mkconf "$T/b.conf"
: > "$FAKE_GOSSH_LOG"
OUT=$("$BIN" -c "$T/b.conf" "$HF" 2> /dev/null)
RC=$?
check "종료코드 0" 0 "$RC"
check "install_err 가 OS설치중" "h_err${TAB}OS설치중" "$(printf '%s\n' "$OUT" | grep '^h_err')"
if grep -q NS_ERR "$FAKE_GOSSH_LOG"; then ng "2차 명령에 NS_ERR grep 없음"; else ok "2차 명령에 NS_ERR grep 없음"; fi

echo "== 오류 경로"
mkconf "$T/c.conf" "reboot now"
"$BIN" -c "$T/c.conf" "$HF" > /dev/null 2>&1
check "위험어 패턴 -> 종료코드 2" 2 "$?"
"$BIN" -c "$T/a.conf" > /dev/null 2>&1
check "호스트파일 인자 없음 -> 종료코드 1" 1 "$?"
printf 'GOSSH=%s\n' "$T/없는/gossh" > "$T/d.conf"
"$BIN" -c "$T/d.conf" "$HF" > /dev/null 2>&1
check "gossh 없음 -> 종료코드 2" 2 "$?"

echo
echo "결과: PASS=$PASS FAIL=$FAIL"
[ "$FAIL" -eq 0 ]
