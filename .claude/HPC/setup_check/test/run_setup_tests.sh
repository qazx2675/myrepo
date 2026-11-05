#!/bin/bash
# run_setup_tests.sh - setup.sh / new_setup.sh(첫 실행 제안) 비대화형 시험. 답을 파이프로 넣는다.
# 사용법: bash test/run_setup_tests.sh
# 가짜 파일은 모두 임시 폴더 아래에 만들고 끝나면 지운다.

HERE=$(cd "$(dirname "$0")/.." && pwd -P)
T=$(mktemp -d "${TMPDIR:-/tmp}/setup_tests.XXXXXX") || exit 2
trap 'rm -rf "$T"' EXIT

PASS=0
FAIL=0
ok() { # 이름 조건결과(0=참)
	if [ "$2" -eq 0 ]; then
		PASS=$((PASS + 1))
		echo "  ok   $1"
	else
		FAIL=$((FAIL + 1))
		echo "  FAIL $1"
	fi
}
has() { grep -qF -e "$2" "$1"; }

# 도구 폴더: setup.sh + 샘플을 복사해 그 옆의 new_setup.conf 를 기본 경로로 쓴다
TOOL=$T/tool
mkdir -p "$TOOL"
cp "$HERE/setup.sh" "$HERE/new_setup.conf.sample" "$TOOL/"
CONF=$TOOL/new_setup.conf
SAMPLE=$HERE/new_setup.conf.sample

# 가짜 대상들
FAKE=$T/fake
mkdir -p "$FAKE/gossh_dir" "$FAKE/route" "$FAKE/st"
printf '#!/bin/sh\nexit 0\n' >"$FAKE/gossh_dir/gossh"
chmod +x "$FAKE/gossh_dir/gossh"
printf '#!/bin/bash\ncase $1 in -auto-check) echo chk;; -auto) echo fix;; esac\n' >"$FAKE/cc.sh"
printf '#!/bin/bash\n# 구버전: 옵션 없음\necho old\n' >"$FAKE/cc_old.sh"
printf '#!/bin/sh\nexit 0\n' >"$FAKE/st/stress-test"
chmod +x "$FAKE/st/stress-test"
printf 'x=1\n' >"$FAKE/st.conf"
: >"$FAKE/route/info_mn.sh"
: >"$FAKE/route/info.sh"
mkdir -p "$FAKE/route_bad"

# 입력 N 줄: 질문 순서 = GOSSH OS_CHECK_SH OS_CHECK_EXCLUDE STRESS_TEST STRESS_TEST_CONF USER_ROUTE 고급?
#   고급 y 면 INTEL_LOG INTEL_OK INTEL_LINE AMD_LOG AMD_OK AMD_LINE WAIT_START WAIT_INTERVAL WAIT_MAX GOSSH_TO CHECK_TO STRESS_TO
enters() { local i; for ((i = 0; i < $1; i++)); do echo; done; }

OUT=$T/out.txt
RCV() { cat "$T/rc"; }
run() { # 표준입력 = 답. 인자 = setup.sh 인자. 종료 코드는 RC
	bash "$TOOL/setup.sh" "$@" >"$OUT" 2>&1
	RC=$?
	echo "$RC" >"$T/rc"
}

echo "[1] 설정 파일이 없을 때 샘플에서 새로 만들기"
rm -f "$CONF" "$CONF.bak"
enters 8 | run --yes
ok "종료 코드 0" "$(RCV)"
[ -f "$CONF" ]
ok "new_setup.conf 생성" $?
cmp -s "$CONF" "$SAMPLE"
ok "모두 Enter 면 샘플과 내용이 같다(주석 유지)" $?
[ ! -e "$CONF.bak" ]
ok "새로 만들 때는 .bak 이 없다" $?
has "$OUT" "새 설정 파일을 만듭니다"
ok "새 파일 안내 출력" $?

echo "[2] 값 지정 (샘플의 주석 줄 풀기, 주석·줄 수 유지)"
rm -f "$CONF" "$CONF.bak"
LINES_BEFORE=$(wc -l <"$SAMPLE")
{
	echo "$FAKE/gossh_dir"
	echo "$FAKE/cc.sh"
	echo "asdf|qwer"
	echo "$FAKE/st/stress-test"
	echo "$FAKE/st.conf"
	echo "$FAKE/route"
	echo n
} | run --yes
ok "종료 코드 0" "$(RCV)"
has "$CONF" "GOSSH=$FAKE/gossh_dir"
ok "GOSSH 기록" $?
has "$CONF" "OS_CHECK_SH=$FAKE/cc.sh"
ok "OS_CHECK_SH 기록" $?
has "$CONF" "OS_CHECK_EXCLUDE=asdf|qwer"
ok "OS_CHECK_EXCLUDE 기록" $?
has "$CONF" "STRESS_TEST=$FAKE/st/stress-test"
ok "STRESS_TEST 기록" $?
[ "$(grep -c '^#STRESS_TEST=' "$CONF")" -eq 0 ] && [ "$(grep -c '^STRESS_TEST=' "$CONF")" -eq 1 ]
ok "#STRESS_TEST= 줄을 풀어 한 번만 쓴다" $?
has "$CONF" "USER_ROUTE=$FAKE/route"
ok "USER_ROUTE 기록" $?
[ "$(wc -l <"$CONF")" -eq "$LINES_BEFORE" ]
ok "줄 수가 샘플과 같다(제자리 치환)" $?
has "$CONF" "# 설정체크 제외 hostname 문자열"
ok "주석 보존" $?
has "$OUT" "→"
ok "변경 요약(이전 → 새 값) 출력" $?
if [ "$(grep -c '경고' "$OUT")" -eq 0 ]; then r=0; else r=1; fi
ok "올바른 값이면 경고가 없다" "$r"

echo "[3] 기본값 유지(Enter) 다시 실행 — 변경 없음·파일 그대로"
cp "$CONF" "$T/before.conf"
enters 8 | run --yes
ok "종료 코드 0" "$(RCV)"
cmp -s "$CONF" "$T/before.conf"
ok "파일 내용 그대로" $?
[ ! -e "$CONF.bak" ]
ok "변경이 없으면 .bak 도 만들지 않는다" $?
has "$OUT" "변경 없음"
ok "변경 없음 안내" $?
has "$OUT" "현재: $FAKE/gossh_dir"
ok "현재 값을 기본값으로 보여 준다" $?

echo "[4] '-' 로 비우기"
{
	echo "-"
	enters 5
	echo n
} | run --yes
ok "종료 코드 0" "$(RCV)"
grep -q '^GOSSH=$' "$CONF"
ok "GOSSH 비워짐" $?
has "$CONF" "OS_CHECK_SH=$FAKE/cc.sh"
ok "다른 값은 유지" $?
has "$CONF" "GOSSH=" && has "$OUT" "GOSSH: $FAKE/gossh_dir → (비어 있음)"
ok "요약에 비움 표시" $?
cmp -s "$CONF.bak" "$T/before.conf"
ok "이전 파일이 .bak 으로 남음" $?

echo "[5] 잘못된 경로 경고 (확인 없이 --yes: 값 유지)"
cp "$CONF" "$T/before5.conf"
{
	echo "/no/such/gossh"
	echo "$FAKE/cc_old.sh"
	echo
	echo "$FAKE/st"
	echo "/no/such.conf"
	echo "$FAKE/route_bad"
	echo n
} | run --yes
ok "종료 코드 0(경고는 치명적이지 않다)" "$(RCV)"
has "$OUT" "gossh 를 찾을 수 없습니다: /no/such/gossh"
ok "GOSSH 경고" $?
has "$OUT" "'-auto-check' 문자열이 없습니다"
ok "OS_CHECK_SH -auto-check 경고" $?
has "$OUT" "'-auto' 처리 구문을 찾지 못했습니다"
ok "OS_CHECK_SH -auto 경고" $?
has "$OUT" "일반 파일이 아닙니다(바이너리 파일 경로여야 합니다)"
ok "STRESS_TEST 폴더 경고" $?
has "$OUT" "설정 파일이 없습니다: /no/such.conf"
ok "STRESS_TEST_CONF 경고" $?
has "$OUT" "폴더에 info_mn.sh 가 없습니다"
ok "USER_ROUTE info_mn.sh 경고" $?
has "$CONF" "GOSSH=/no/such/gossh"
ok "경고가 있어도 입력값을 그대로 저장" $?

echo "[6] 경고 후 다시 입력 (--yes 없이: 다시 입력 → 올바른 값)"
cp "$T/before5.conf" "$CONF"
{
	echo "/no/such/gossh" # 경고
	echo y               # 다시 입력
	echo "$FAKE/gossh_dir/gossh"
	enters 5 # 나머지 5개 질문 유지
	echo n   # 고급 안 함
	echo y   # 저장
	echo n   # stress-test 설정 이어하기(있으면)
} | run
ok "종료 코드 0" "$(RCV)"
has "$OUT" "다시 입력할까요?"
ok "다시 입력 질문" $?
has "$CONF" "GOSSH=$FAKE/gossh_dir/gossh"
ok "다시 입력한 값 저장" $?

echo "[7] 경고 후 n = 입력값 유지"
cp "$T/before5.conf" "$CONF"
{
	echo "/no/such/gossh2"
	echo n
	enters 5
	echo n
	echo y
	echo n
} | run
ok "종료 코드 0" "$(RCV)"
has "$CONF" "GOSSH=/no/such/gossh2"
ok "n 이면 입력값 그대로" $?

echo "[8] 고급 설정: CPU 문구·시간"
rm -f "$CONF" "$CONF.bak"
{
	enters 6
	echo y
	echo "/data/intel.log" # INTEL_LOG
	echo "Passed"          # INTEL_OK
	echo "Summary"         # INTEL_LINE (샘플의 #CPU_INTEL_LINE= 풀기)
	echo "-"               # AMD_LOG 비움
	enters 2               # AMD_OK AMD_LINE 유지
	echo "50m"             # WAIT_START
	echo "5"               # WAIT_INTERVAL (분)
	echo "abc"             # WAIT_MAX 경고
	echo "1h30m"           # GOSSH_TIMEOUT
	echo "0"               # CHECK_TIMEOUT 경고(0)
	echo "30s"             # STRESS_TIMEOUT
} | run --yes
ok "종료 코드 0" "$(RCV)"
has "$CONF" "CPU_INTEL_LOG=/data/intel.log"
ok "CPU_INTEL_LOG" $?
has "$CONF" "CPU_INTEL_OK=Passed"
ok "CPU_INTEL_OK" $?
has "$CONF" "CPU_INTEL_LINE=Summary"
ok "CPU_INTEL_LINE (주석 줄 풀기)" $?
grep -q '^CPU_AMD_LOG=$' "$CONF"
ok "CPU_AMD_LOG 비움" $?
has "$CONF" "CPU_WAIT_START=50m"
ok "CPU_WAIT_START" $?
has "$CONF" "CPU_WAIT_INTERVAL=5"
ok "숫자만(분) 허용" $?
has "$OUT" "시간으로 읽을 수 없습니다(예: 47m, 30s, 1h30m, 숫자만 쓰면 분): abc"
ok "잘못된 시간 경고" $?
has "$OUT" "0 보다 커야 합니다: 0"
ok "0 시간 경고" $?
has "$CONF" "GOSSH_TIMEOUT=1h30m"
ok "Go 복합 시간 허용" $?
if [ "$(grep -Ec '경고: .*: (50m|5|1h30m|30s)$' "$OUT")" -eq 0 ]; then r=0; else r=1; fi
ok "올바른 시간에는 경고 없음" "$r"

echo "[9] 저장 확인 거부 / 원자적 저장·백업 / 임시 파일 정리"
cp "$CONF" "$T/before9.conf"
{
	echo "$FAKE/gossh_dir"
	enters 5
	echo n
	echo n # 저장 거부
} | run
ok "저장 거부 시 종료 코드 1" "$(( $(RCV) == 1 ? 0 : 1 ))"
cmp -s "$CONF" "$T/before9.conf"
ok "거부하면 설정 파일 그대로" $?
{
	echo "$FAKE/gossh_dir"
	enters 5
	echo n
	echo y
	echo n
} | run
ok "저장 승인 종료 코드 0" "$(RCV)"
cmp -s "$CONF.bak" "$T/before9.conf"
ok ".bak 은 저장 직전 내용" $?
r=0
for f in "$TOOL"/new_setup.conf.*; do
	case $f in *.bak | *.sample) ;; *) [ -e "$f" ] && r=1 ;; esac
done
ok "임시 파일이 남지 않는다" "$r"

echo "[10] 인자·파일 오류 = 종료 코드 2"
enters 1 | run --bogus
ok "알 수 없는 인자" "$(( $(RCV) == 2 ? 0 : 1 ))"
enters 1 | run -c
ok "-c 값 없음" "$(( $(RCV) == 2 ? 0 : 1 ))"
enters 1 | run -c "$T"
ok "-c 가 폴더" "$(( $(RCV) == 2 ? 0 : 1 ))"
enters 1 | run -c "$T/nodir/x.conf"
ok "-c 상위 폴더 없음" "$(( $(RCV) == 2 ? 0 : 1 ))"
mkdir -p "$T/bare"
cp "$HERE/setup.sh" "$T/bare/"
enters 1 | bash "$T/bare/setup.sh" >"$OUT" 2>&1
ok "설정도 샘플도 없음" "$((($? == 2) ? 0 : 1))"
bash "$TOOL/setup.sh" --help >"$OUT" 2>&1
ok "--help 종료 코드 0" "$?"

echo "[11] -c 로 다른 경로 지정"
rm -f "$T/other.conf"
{
	echo "$FAKE/gossh_dir"
	enters 5
	echo n
} | run -c "$T/other.conf" --yes
ok "종료 코드 0" "$(RCV)"
has "$T/other.conf" "GOSSH=$FAKE/gossh_dir"
ok "지정한 파일에 저장" $?

echo "[12] stress-test 설정 이어하기"
cat >"$FAKE/st/setup.sh" <<EOF
#!/bin/bash
echo "\$@" >"$T/st_args.txt"
EOF
rm -f "$T/st_args.txt"
{
	enters 3
	echo "$FAKE/st/stress-test"
	enters 2
	echo n
	echo y     # 저장
	echo y     # 이어서
	echo alice # 사용자
} | run -c "$T/st.conf"
ok "종료 코드 0" "$(RCV)"
has "$OUT" "stress-test 설정도 이어서 할까요? (Y/n)"
ok "이어하기 질문" $?
[ "$(cat "$T/st_args.txt" 2>/dev/null)" = "--user alice" ]
ok "stress-test/setup.sh --user alice 호출" $?
rm -f "$FAKE/st/setup.sh" "$T/st_args.txt"
{
	enters 3
	echo "$FAKE/st/stress-test"
	enters 2
	echo n
	echo y
} | run -c "$T/st2.conf"
has "$OUT" "stress-test 설정은 그 도구 폴더에서 따로 하세요"
ok "setup.sh 가 없으면 안내만 하고 건너뜀" $?
[ ! -e "$T/st_args.txt" ]
ok "호출하지 않음" $?

echo "[13] new_setup.sh 래퍼: 터미널이 아니면 첫 실행 제안을 건너뜀"
W=$T/wrap
mkdir -p "$W/bin" "$T/cwd"
cp "$HERE/new_setup.sh" "$HERE/setup.sh" "$HERE/new_setup.conf.sample" "$W/"
printf '#!/bin/bash\necho "RAN $*"\n' >"$W/bin/new_setup"
chmod +x "$W/bin/new_setup"
(cd "$T/cwd" && bash "$W/new_setup.sh" hosts.txt </dev/null >"$OUT" 2>&1)
ok "래퍼가 원래 명령을 실행" "$(grep -q '^RAN hosts.txt$' "$OUT" && echo 0 || echo 1)"
if grep -q '설정할까요' "$OUT"; then r=1; else r=0; fi
ok "비대화형이면 질문하지 않는다" "$r"
[ ! -e "$W/new_setup.conf" ]
ok "설정 파일을 만들지 않는다(기본값 사용)" $?
# 파이프 표준입력(터미널 아님)도 같다
echo y | (cd "$T/cwd" && bash "$W/new_setup.sh" hosts.txt >"$OUT" 2>&1)
if grep -q '설정할까요' "$OUT"; then r=1; else r=0; fi
ok "파이프 입력이어도 질문하지 않는다" "$r"

if command -v script >/dev/null 2>&1; then
	echo "[14] 래퍼: 터미널에서 첫 실행 제안 (script 로 가짜 tty)"
	(cd "$T/cwd" && printf 'n\n' | script -qec "bash $W/new_setup.sh hosts.txt" /dev/null >"$OUT" 2>&1)
	has "$OUT" "설정 파일이 없습니다. 지금 설정할까요? (Y/n)"
	ok "tty 에서는 질문한다" $?
	has "$OUT" "RAN hosts.txt"
	ok "n 이면 질문 후 원래 명령 계속" $?
	[ ! -e "$W/new_setup.conf" ]
	ok "n 이면 설정 파일을 만들지 않는다" $?
	(cd "$T/cwd" && printf 'n\n' | script -qec "bash $W/new_setup.sh -c x.conf hosts.txt" /dev/null >"$OUT" 2>&1)
	if grep -q '설정할까요' "$OUT"; then r=1; else r=0; fi
	ok "-c 가 있으면 질문하지 않는다" "$r"
	touch "$W/new_setup.conf"
	(cd "$T/cwd" && printf 'n\n' | script -qec "bash $W/new_setup.sh hosts.txt" /dev/null >"$OUT" 2>&1)
	if grep -q '설정할까요' "$OUT"; then r=1; else r=0; fi
	ok "설정 파일이 있으면 질문하지 않는다" "$r"
	rm -f "$W/new_setup.conf"
	(cd "$T/cwd" && { printf 'y\n'; for _ in $(seq 1 7); do printf '\n'; done; printf 'y\n'; } | script -qec "bash $W/new_setup.sh hosts.txt" /dev/null >"$OUT" 2>&1)
	[ -f "$W/new_setup.conf" ] && has "$OUT" "RAN hosts.txt"
	ok "y 면 setup.sh 를 돌려 설정 파일을 만들고 원래 명령 계속" $?
fi

echo
echo "통과 $PASS / 실패 $FAIL"
[ "$FAIL" -eq 0 ]
