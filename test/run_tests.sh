#!/bin/bash
# run_tests.sh - new_setup 종단 시험 (fake_gossh 사용). 랩에서 실행: bash test/run_tests.sh
set -u
HERE=$(cd "$(dirname "$0")" && pwd -P)
ROOT=$(cd "$HERE/.." && pwd -P)
FAKE=$HERE/fake_gossh.sh
FAKECC=$HERE/fake_config_check.sh
chmod +x "$FAKE" "$FAKECC"
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

SEP='|'   # 출력 열 구분자
HF=$T/hosts
SC=$T/scenario
printf '%s\n' "# 주석 줄" "h_ok" "h_off" "h_ref" "" "h_disk" "h_err" "h_ok" "h_none" "h_nolog" "h_retryoff" "h_cancel" > "$HF"
printf '%s\t%s\n' h_ok ok h_off off h_ref refused h_disk install_disk h_err install_err \
	h_none install_none h_nolog install_nolog h_retryoff install_retry_off h_cancel cancel > "$SC"

# gossh 대역은 이름이 gossh 인 래퍼로 둔다 (config_check 안에서 PATH 로 찾아지는지 확인용)
GB=$T/gb
mkdir -p "$GB"
{
	echo '#!/bin/bash'
	echo "exec bash \"$FAKE\" \"\$@\""
} > "$GB/gossh"
chmod +x "$GB/gossh"

mkconf() { # 파일 [ERROR_PATTERN 줄...]
	local f=$1 p
	shift
	{
		echo "GOSSH=$GB/gossh"
		echo 'DISK_PATTERN=Disk "" given in clearpart command does not exist'
		for p in "$@"; do echo "ERROR_PATTERN=$p"; done
	} > "$f"
}

# 8열: host 상태 설정체크 CPU stress-ng H/T OS 비고. 완료가 아니면 3~7열은 '-'
D5="-${SEP}-${SEP}-${SEP}-${SEP}-${SEP}" # 끝의 빈 칸은 비고
OKROW="h_ok${SEP}완료${SEP}-${SEP}O${SEP}O${SEP}ON${SEP}4.18.0-test${SEP}" # OS_CHECK_SH 없음 -> 설정체크 '-'
HEADER="hostname|상태|설정체크|CPU테스트|stress-ng|H/T|OS|비고"
EXPECT=$(printf '%s\n' "$HEADER" \
	"$OKROW" "h_off${SEP}접속불가${SEP}$D5" "h_ref${SEP}접속불가${SEP}$D5" "h_disk${SEP}디스크미인식${SEP}$D5" \
	"h_err${SEP}설치정지${SEP}$D5" "h_none${SEP}OS설치중${SEP}$D5" "h_nolog${SEP}OS설치중${SEP}$D5" \
	"h_retryoff${SEP}OS설치중${SEP}$D5" "h_cancel${SEP}접속불가${SEP}$D5")

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
BAD=$(printf '%s\n' "$OUT" | awk -F'|' 'NF!=8')
check "모든 줄이 8필드" "" "$BAD"
check "첫 줄은 제목 줄" "$HEADER" "$(printf '%s\n' "$OUT" | head -n1)"
check "둘째 줄부터 결과" "$OKROW" "$(printf '%s\n' "$OUT" | sed -n 2p)"
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
check "install_err 가 OS설치중" "h_err${SEP}OS설치중${SEP}$D5" "$(printf '%s\n' "$OUT" | grep '^h_err')"
if grep -q NS_ERR "$FAKE_GOSSH_LOG"; then ng "2차 명령에 NS_ERR grep 없음"; else ok "2차 명령에 NS_ERR grep 없음"; fi

echo "== 현재 경로의 new_setup.conf 자동 사용 (-c 생략)"
OUT_C=$("$BIN" -c "$T/a.conf" "$HF" 2> /dev/null)
mkdir -p "$T/cwd"
cp "$T/a.conf" "$T/cwd/new_setup.conf"
OUT=$(cd "$T/cwd" && "$BIN" "$HF" 2> "$T/cwd.err")
check "-c 생략 결과가 -c ./new_setup.conf 와 동일" "$OUT_C" "$OUT"
if grep -q "설정 파일: new_setup.conf" "$T/cwd.err"; then ok "사용한 설정 파일 표시"; else ng "사용한 설정 파일 표시"; fi
OUT=$(cd "$T/cwd" && "$BIN" -c "$T/b.conf" "$HF" 2> /dev/null)
check "-c 가 현재 경로 설정보다 우선" "h_err${SEP}OS설치중${SEP}$D5" "$(printf '%s\n' "$OUT" | grep '^h_err')"
rm -rf "$T/cwd" "${HF}_new_setup.log"

echo "== 오류 경로"
mkconf "$T/c.conf" "reboot now"
"$BIN" -c "$T/c.conf" "$HF" > /dev/null 2>&1
check "위험어 패턴 -> 종료코드 2" 2 "$?"
"$BIN" -c "$T/a.conf" > /dev/null 2>&1
check "호스트파일 인자 없음 -> 종료코드 1" 1 "$?"
printf 'GOSSH=%s\n' "$T/없는/gossh" > "$T/d.conf"
"$BIN" -c "$T/d.conf" "$HF" > /dev/null 2>&1
check "gossh 없음 -> 종료코드 2" 2 "$?"

# ---------------------------------------------------------------- v2
fld() { # 출력 호스트 열번호
	printf '%s\n' "$1" | awk -F'|' -v h="$2" -v n="$3" '$1==h {print $n; exit}'
}
SC2=$T/scenario2
HF2=$T/hosts2
CCS=$T/cc_scenario
CCLOG=$T/cc.log
printf '%s\n' a_intel b_intelng c_amd d_none e_off h_skipme_fail g_nolog > "$HF2"
{
	printf 'a_intel\tok\n'
	printf 'b_intelng\tok\tcpu=ng\tstress=0\ttpc=1\tkernel=5.14.0-x\n'
	printf 'c_amd\tok\tvendor=amd cpu=ok tpc=2 kernel=5.14.0-amd\n'
	printf 'd_none\tok\tvendor=none\ttpc=none\tstress=0\tkernel=3.10.0-d\n'
	printf 'e_off\toff\n'
	printf 'h_skipme_fail\tok\n'
	printf 'g_nolog\tok\tcpu=nolog\n'
} > "$SC2"
printf 'a_intel\tpass\nb_intelng\tfail\nc_amd\tnone\nh_skipme_fail\tfail\n' > "$CCS"
export FAKE_GOSSH_SCENARIO=$SC2
export FAKE_GOSSH_LOG=$T/gossh_calls2.log
export FAKE_CC_SCENARIO=$CCS
export FAKE_CC_LOG=$CCLOG

mkconf2() { # 파일 OS_CHECK_SH값 [추가 conf 줄...]
	local f=$1 sh=$2 p
	shift 2
	mkconf "$f"
	{
		echo "OS_CHECK_SH=$sh"
		echo 'OS_CHECK_EXCLUDE=skipme|zzz'
		for p in "$@"; do echo "$p"; done
	} >> "$f"
}

echo "== v2: CPU(AMD 기본 규칙)·stress-ng·H/T·OS·설정체크"
mkconf2 "$T/v2a.conf" "$FAKECC"
: > "$FAKE_GOSSH_LOG"
rm -f "$CCLOG"
mkdir -p "$T/work2"
cp "$HF2" "$T/work2/hosts2"
OUT=$(cd "$T/work2" && "$BIN" -c "$T/v2a.conf" "$T/work2/hosts2" 2> "$T/v2a.err")
check "종료코드 0" 0 "$?"
BAD=$(printf '%s\n' "$OUT" | awk -F'|' 'NF!=8')
check "모든 줄이 8필드" "" "$BAD"
check "입력 순서" "hostname a_intel b_intelng c_amd d_none e_off h_skipme_fail g_nolog" "$(printf '%s\n' "$OUT" | cut -d'|' -f1 | tr '\n' ' ' | sed 's/ $//')"
check "미완료 장비 5열 모두 -" "e_off${SEP}접속불가${SEP}$D5" "$(printf '%s\n' "$OUT" | grep '^e_off')"
check "CPU Intel ok -> O" O "$(fld "$OUT" a_intel 4)"
check "CPU Intel ng -> X" X "$(fld "$OUT" b_intelng 4)"
check "CPU AMD(기본 규칙: results.json Result 줄 모두 PASS) -> O" O "$(fld "$OUT" c_amd 4)"
if grep -q "results.json" "$FAKE_GOSSH_LOG"; then ok "기본 설정에 AMD results.json 확인 포함"; else ng "기본 설정에 AMD results.json 확인 포함"; fi
check "CPU 테스트 로그 없음 -> NOTRUN" NOTRUN "$(fld "$OUT" g_nolog 4)"
check "stress-ng 1 -> O" O "$(fld "$OUT" a_intel 5)"
check "stress-ng 0 -> NOTRUN" NOTRUN "$(fld "$OUT" b_intelng 5)"
check "H/T 2 -> ON" ON "$(fld "$OUT" a_intel 6)"
check "H/T 1 -> OFF" OFF "$(fld "$OUT" b_intelng 6)"
check "H/T 줄 없음 -> -" - "$(fld "$OUT" d_none 6)"
check "벤더 없음 -> CPU X" X "$(fld "$OUT" d_none 4)"
check "OS 값 (기본)" 4.18.0-test "$(fld "$OUT" a_intel 7)"
check "OS 값 (지정)" 5.14.0-x "$(fld "$OUT" b_intelng 7)"
# KERNEL_NAME: 같은 커널은 표시 이름, 없는 커널은 그대로
cp "$T/v2a.conf" "$T/v2k.conf"
printf 'KERNEL_NAME=4.18.0-test=RHEL8.10 OPC/Test\n' >> "$T/v2k.conf"
OUTK=$(cd "$T/work2" && FAKE_CC_LOG=$T/cc_k.log "$BIN" -c "$T/v2k.conf" "$T/work2/hosts2" 2> /dev/null)
check "KERNEL_NAME 표시 이름" "RHEL8.10 OPC/Test" "$(fld "$OUTK" a_intel 7)"
check "KERNEL_NAME 에 없는 커널은 그대로" 5.14.0-x "$(fld "$OUTK" b_intelng 7)"
check "설정체크 pass -> O" O "$(fld "$OUT" a_intel 3)"
check "설정체크 fail -> X" X "$(fld "$OUT" b_intelng 3)"
check "설정체크 출력 없음 -> 출력없음" 출력없음 "$(fld "$OUT" c_amd 3)"
check "시나리오에 없는 호스트는 pass -> O" O "$(fld "$OUT" d_none 3)"
check "OS_CHECK_EXCLUDE 포함 호스트는 fail 이어도 O" O "$(fld "$OUT" h_skipme_fail 3)"
check "config_check 목록 = 완료 장비만" "LIST=a_intel b_intelng c_amd d_none h_skipme_fail g_nolog" "$(grep '^LIST=' "$CCLOG")"
CCPWD=$(sed -n 's/^PWD=//p' "$CCLOG" | head -n1)
CCTMP=$(sed -n 's/^TMPDIR=//p' "$CCLOG" | head -n1)
if [ -n "$CCPWD" ] && [ "$CCPWD" != "$T/work2" ]; then ok "config_check 작업 폴더가 호스트파일 폴더와 다름"; else ng "config_check 작업 폴더가 호스트파일 폴더와 다름" "PWD=$CCPWD"; fi
case $CCTMP in "$CCPWD"/*) ok "TMPDIR 이 임시 폴더 아래" ;; *) ng "TMPDIR 이 임시 폴더 아래" "PWD=$CCPWD TMPDIR=$CCTMP" ;; esac
if [ -n "$CCPWD" ] && [ ! -e "$CCPWD" ]; then ok "임시 폴더 삭제됨"; else ng "임시 폴더 삭제됨" "$CCPWD"; fi
check "config_check 안에서 gossh 가 PATH 로 찾아짐" "GOSSH=$GB/gossh" "$(grep '^GOSSH=' "$CCLOG" | head -n1)"
if grep -q -e '-auto-check new_setup ' "$CCLOG"; then ok "인자 -auto-check new_setup <목록>"; else ng "인자 -auto-check new_setup <목록>" "$(grep '^=== CC' "$CCLOG")"; fi

echo "== v2: AMD 설정 있음"
mkconf2 "$T/v2b.conf" "$FAKECC" "CPU_AMD_LOG=/var/cpudiag/amd.log" "CPU_AMD_OK=amd passed"
: > "$FAKE_GOSSH_LOG"
OUT=$(cd "$T/work2" && "$BIN" -c "$T/v2b.conf" "$T/work2/hosts2" 2> /dev/null)
check "CPU AMD(설정 있음, ok) -> O" O "$(fld "$OUT" c_amd 4)"
check "Intel 은 그대로" O "$(fld "$OUT" a_intel 4)"
if grep -q NS_CPU_AMD "$FAKE_GOSSH_LOG"; then ok "AMD 설정 시 명령에 NS_CPU_AMD 포함"; else ng "AMD 설정 시 명령에 NS_CPU_AMD 포함"; fi

echo "== v2: AMD 확인 끔 (CPU_AMD_LOG= 빈 값)"
mkconf2 "$T/v2d.conf" "$FAKECC" "CPU_AMD_LOG="
: > "$FAKE_GOSSH_LOG"
OUT=$(cd "$T/work2" && "$BIN" -c "$T/v2d.conf" "$T/work2/hosts2" 2> /dev/null)
check "CPU AMD(확인 끔) -> X" X "$(fld "$OUT" c_amd 4)"
if grep -q NS_CPU_AMD "$FAKE_GOSSH_LOG"; then ng "AMD 끄면 명령에 NS_CPU_AMD 없음"; else ok "AMD 끄면 명령에 NS_CPU_AMD 없음"; fi

echo "== v2: OS_CHECK_SH 비움"
mkconf2 "$T/v2c.conf" ""
rm -f "$CCLOG"
OUT=$(cd "$T/work2" && "$BIN" -c "$T/v2c.conf" "$T/work2/hosts2" 2> /dev/null)
check "OS_CHECK_SH 비면 설정체크 -" - "$(fld "$OUT" a_intel 3)"
check "다른 열은 정상" O "$(fld "$OUT" a_intel 4)"
if [ -e "$CCLOG" ]; then ng "config_check 실행 안 함"; else ok "config_check 실행 안 함"; fi

echo "== v2: -auto-check 미지원 스크립트"
sed 's/-auto-check/-xxxx-check/g' "$FAKECC" > "$T/old_cc.sh"
mkconf2 "$T/v2d.conf" "$T/old_cc.sh"
rm -f "$CCLOG"
OUT=$(cd "$T/work2" && "$BIN" -c "$T/v2d.conf" "$T/work2/hosts2" 2> "$T/v2d.err")
check "종료코드 0" 0 "$?"
check "미지원이면 설정체크 -" - "$(fld "$OUT" a_intel 3)"
if [ -e "$CCLOG" ]; then ng "미지원 스크립트는 실행 안 함"; else ok "미지원 스크립트는 실행 안 함"; fi
rm -rf "$T/work2"


echo "== v4: 장애 목록(-er) -> 비고 열"
mkdir -p "$T/er"
# 탭 구분(공백 구분 줄은 무시), 같은 호스트 여러 줄, 대소문자, '|' 포함 사유, 주석·빈 줄·사유 없는 줄, CRLF
printf '# 주석\r\nh_ok\t메모리 불량\r\n\r\nH_OFF\t전원 | 케이블\r\nh_ok\t디스크 교체 예정\r\nh_disk\r\nh_ref 공백 구분은 무시\r\n' > "$T/er/list.txt"
OUT=$("$BIN" -c "$T/a.conf" -er "$T/er/list.txt" "$HF" 2> /dev/null)
check "종료코드 0" 0 "$?"
BAD=$(printf '%s\n' "$OUT" | awk -F'|' 'NF!=8')
check "모든 줄이 8필드" "" "$BAD"
check "여러 줄은 이어 붙임" "메모리 불량, 디스크 교체 예정" "$(fld "$OUT" h_ok 8)"
check "대소문자 무시, '|' 는 '/'" "전원 / 케이블" "$(fld "$OUT" h_off 8)"
check "사유 없는 줄은 빈칸" "" "$(fld "$OUT" h_disk 8)"
check "공백으로만 구분한 줄은 무시(빈칸)" "" "$(fld "$OUT" h_ref 8)"
check "제목 줄 마지막은 비고" "비고" "$(printf '%s\n' "$OUT" | head -n1 | cut -d'|' -f8)"
cp "$T/er/list.txt" "$T/er/error_list.txt"
OUT2=$(cd "$T/er" && "$BIN" -c "$T/a.conf" "$HF" 2> "$T/er.err")
check "-er 생략 시 현재 경로 error_list.txt 자동 사용" "$OUT" "$OUT2"
if grep -q "장애 목록: error_list.txt" "$T/er.err"; then ok "사용한 장애 목록 표시"; else ng "사용한 장애 목록 표시"; fi
"$BIN" -c "$T/a.conf" -er "$T/er/없음.txt" "$HF" > /dev/null 2>&1
check "-er 파일 없음 -> 종료코드 2" 2 "$?"
rm -rf "$T/er" "${HF}_new_setup.log"
echo
echo "결과: PASS=$PASS FAIL=$FAIL"
[ "$FAIL" -eq 0 ]
