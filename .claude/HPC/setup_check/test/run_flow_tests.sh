#!/bin/bash
# run_flow_tests.sh - new_setup -run 전체 흐름 시험 (fake gossh / config_check / stress-test). 랩에서 실행: bash test/run_flow_tests.sh
# 시간 값은 초 단위로 줄여(CPU_WAIT_START=3s 등) 2분 안에 끝낸다. 비대화형(tty 아님)으로 -run 을 띄우고,
# state.json 의 질문 번호를 읽어 answer.json 으로 답한다.
set -u
HERE=$(cd "$(dirname "$0")" && pwd -P)
ROOT=$(cd "$HERE/.." && pwd -P)
FAKE=$HERE/fake_gossh.sh
FAKECC=$HERE/fake_config_check.sh
FAKEST=$HERE/fake_stress_test.sh
chmod +x "$FAKE" "$FAKECC" "$FAKEST"
T=$(mktemp -d)

cleanup() {
	local f p
	for f in "$T"/*/hosts_run/worker.pid; do
		[ -f "$f" ] || continue
		p=$(tr -d '[:space:]' < "$f")
		[ -n "$p" ] && kill -9 "$p" 2> /dev/null
	done
	for f in "$T"/*/st.pids; do
		[ -f "$f" ] || continue
		while read -r p; do kill -9 "$p" 2> /dev/null; done < "$f"
	done
	rm -rf "$T"
}
trap cleanup EXIT

PASS=0
FAIL=0
ok() { PASS=$((PASS + 1)); echo "PASS: $1"; }
ng() { FAIL=$((FAIL + 1)); echo "FAIL: $1"; [ -n "${2:-}" ] && printf '%s\n' "$2" | sed 's/^/    /'; }
check() { # 이름 기대 실제
	if [ "$2" = "$3" ]; then ok "$1"; else ng "$1" "기대: [$2]"$'\n'"실제: [$3]"; fi
}
fileok() { if [ -f "$2" ]; then ok "$1"; else ng "$1" "$2 없음"; fi; } # 이름 파일
has() { # 이름 파일 정규식(grep -E)
	if grep -Eq -- "$3" "$2" 2> /dev/null; then ok "$1"; else ng "$1" "$2 에서 [$3] 를 찾지 못함"; fi
}

echo "== 빌드"
(cd "$ROOT" && go build -o "$T/new_setup" .) || { echo "FAIL: go build 실패"; exit 1; }
BIN=$T/new_setup

GB=$T/gb
mkdir -p "$GB"
{
	echo '#!/bin/bash'
	echo "exec bash \"$FAKE\" \"\$@\""
} > "$GB/gossh"
chmod +x "$GB/gossh"

# ---------------------------------------------------------------- 도우미
S=""
# scn_new <이름> : 시험 폴더·환경 준비. 호스트·시나리오는 호출한 쪽이 채운다.
scn_new() {
	S=$T/$1
	mkdir -p "$S/mark" "$S/ccstate"
	: > "$S/scenario"
	: > "$S/cc_scenario"
	export FAKE_MARK_DIR=$S/mark FAKE_CC_STATE=$S/ccstate FAKE_ST_LOG=$S/st.log FAKE_ST_PIDFILE=$S/st.pids
	export FAKE_CC_LOG=$S/cc.log FAKE_GOSSH_LOG=$S/gossh.log FAKE_GOSSH_SCENARIO=$S/scenario FAKE_CC_SCENARIO=$S/cc_scenario
	unset FAKE_ST_REBOOT FAKE_ST_EXIT FAKE_ST_DELAY
	: > "$S/st.log"
	: > "$S/st.pids"
}

# mkconf <추가 줄...> : $S/conf (시간 값을 초 단위로)
mkconf() {
	{
		echo "GOSSH=$GB/gossh"
		echo "OS_CHECK_SH=$FAKECC"
		echo "STRESS_TEST=$FAKEST"
		echo "STRESS_TEST_CONF=$S/st.conf"
		echo "CPU_WAIT_START=3s"
		echo "CPU_WAIT_INTERVAL=1s"
		echo "CPU_WAIT_MAX=5s"
		echo "GOSSH_TIMEOUT=20s"
		echo "CHECK_TIMEOUT=20s"
		echo "STRESS_TIMEOUT=30s"
		for l in "$@"; do echo "$l"; done
	} > "$S/conf"
}

# 장치 한 줄: host 시나리오 [key=value ...]
addscn() { printf '%s\n' "$*" >> "$S/scenario"; }

RUND=""
# start_run : 비대화형으로 -run 을 띄운다(상태 폴더 $S/hosts_run, 안내는 start.out/start.err)
start_run() {
	RUND=$S/hosts_run
	(cd "$S" && "$BIN" -run -c "$S/conf" -user tester "$S/hosts" < /dev/null > "$S/start.out" 2> "$S/start.err")
	return $?
}

top_stage() { sed -n 's/^  "stage": \([0-9]*\),*$/\1/p' "$1/state.json" 2> /dev/null | head -n1; }
is_stopped() { grep -q '^  "stopped": true' "$1/state.json" 2> /dev/null; }
# qinfo <상태폴더> : 질문이 있으면 "id 단계 선택키들" (없으면 빈 출력)
qinfo() {
	awk '
	/^  "question": null/ {exit}
	/^  "question": \{/ {q=1; next}
	q && /^    "id":/ {gsub(/[",]/, "", $2); id=$2}
	q && /^    "stage":/ {gsub(/,/, "", $2); st=$2}
	q && /^        "key":/ {gsub(/[",]/, "", $2); keys=keys $2}
	q && /^  \}/ {print id, st, keys; exit}
	' "$1/state.json" 2> /dev/null
}

# drive <상태폴더> <제한초> <선택함수> : 질문이 나오면 `선택함수 단계 선택키들 질문번호` 가 낸 글자로 답한다.
# 5단계에 이르거나 중지되면 0, 제한 시간이 지나면 1. 받은 질문은 <상태폴더>.asked 에 "단계:선택키들=답" 으로 쌓는다.
drive() {
	local dir=$1 end=$((SECONDS + $2)) pick=$3 last="" info id st keys ans
	while [ "$SECONDS" -lt "$end" ]; do
		if [ "$(top_stage "$dir")" = 5 ] || is_stopped "$dir"; then return 0; fi
		info=$(qinfo "$dir")
		if [ -n "$info" ]; then
			read -r id st keys <<< "$info"
			if [ "$id" != "$last" ]; then
				ans=$($pick "$st" "$keys" "$id")
				[ -n "$ans" ] || ans=c
				echo "$st:$keys=$ans" >> "$dir.asked"
				printf '{"id":"%s","choice":"%s"}\n' "$id" "$ans" > "$dir/.answer.tmp" && mv "$dir/.answer.tmp" "$dir/answer.json"
				last=$id
			fi
		fi
		sleep 0.2
	done
	return 1
}
pick_c() { echo c; }

# wait_for <제한초> <명령...> : 명령이 성공할 때까지 기다린다
wait_for() {
	local end=$((SECONDS + $1))
	shift
	while [ "$SECONDS" -lt "$end" ]; do
		"$@" && return 0
		sleep 0.2
	done
	return 1
}

# 결과표 열 값: rowf <result.txt> <호스트> <열번호>
rowf() { awk -F'|' -v h="$2" -v n="$3" '$1==h {print $n; exit}' "$1"; }
alive() { kill -0 "$1" 2> /dev/null; }
nlines() { grep -c -- "$2" "$1" 2> /dev/null; }
# st_hosts <steps> : st.log 의 START 줄에서 steps 별 hosts 값(호출마다 한 줄)
st_hosts() { sed -n "s/^START $1 [0-9]* user=[^ ]* hosts=\\([^ ]*\\) .*/\\1/p" "$S/st.log"; }
st_ns() { # <종류 START|MON|END> <steps> <n번째>
	awk -v k="$1" -v s="$2" -v n="$3" '$1==k && $2==s {c++; if (c==n) {print $3; exit}}' "$S/st.log"
}

# ================================================================ A. 전체 흐름
echo "== A. 전체 흐름 (설정수정 → CPU/stress-ng 분리 → 재부팅 감지 → 모두 O)"
scn_new A
printf '%s\n' h_off h_fix h_cpu h_reb h_both h_done > "$S/hosts"
addscn h_off off
addscn h_fix ok cpu=nolog stress=0
addscn h_cpu ok cpu=nolog stress=0
addscn h_reb ok cpu=nolog stress=0
addscn h_both ok stress=0
addscn h_done ok
printf 'h_fix\tfail\n' > "$S/cc_scenario"
mkconf
export FAKE_ST_REBOOT=h_reb

start_run
check "A: -run 시작 종료코드 0" 0 "$?"
has "A: 비대화형 상태 출력(결과표 머리)" "$S/start.out" '^hostname\|상태\|설정체크'
has "A: 워커 pid 기록" "$RUND/worker.pid" '^[0-9]+'
fileok "A: state.json 생성" "$RUND/state.json"
drive "$RUND" 80 pick_c
check "A: 5단계(완료)까지 진행" 5 "$(top_stage "$RUND")"
check "A: 질문 순서(1→2→3→4, 선택지)" "1:crxs=c 2:crs=c 3:crs=c 4:crs=c" "$(tr '\n' ' ' < "$RUND.asked" | sed 's/ $//')"

# 설정수정: -auto 와 선택한 사용자, 대상은 설정체크 X 인 h_fix 뿐
check "A: config_check -auto 1회" 1 "$(nlines "$FAKE_CC_LOG" 'argv=-auto tester')"
check "A: -auto 대상은 h_fix" "LIST=h_fix" "$(grep -A4 'argv=-auto tester' "$FAKE_CC_LOG" | grep '^LIST=')"
has "A: 판정은 -auto-check" "$FAKE_CC_LOG" 'argv=-auto-check new_setup'

# stress-test 호출
check "A: stress-test 호출 3회" 3 "$(nlines "$S/st.log" '^START')"
check "A: CPU(-steps a) 대상" "h_fix,h_cpu,h_reb" "$(st_hosts a)"
check "A: stress-ng 대상(CPU 대상과 겹치지 않음 → 3단계 h_both, 4단계 CPU 대상)" "h_both
h_fix,h_cpu,h_reb" "$(st_hosts b)"
has "A: -user tester -batch -conf 전달" "$S/st.log" "^START a [0-9]+ user=tester hosts=.* batch=1 conf=$S/st.conf"
A_MON=$(st_ns MON a 1)
B_START=$(st_ns START b 1)
if [ -n "$A_MON" ] && [ -n "$B_START" ] && [ "$B_START" -ge "$A_MON" ]; then
	ok "A: 첫 stress-ng 는 CPU [감시] 줄 뒤에 시작"
else
	ng "A: 첫 stress-ng 는 CPU [감시] 줄 뒤에 시작" "MON=$A_MON START_b=$B_START"
fi
B2_START=$(st_ns START b 2)
A_END=$(st_ns END a 1)
if [ -n "$A_END" ] && [ -n "$B2_START" ] && [ "$B2_START" -ge "$A_END" ]; then
	ok "A: 4단계 stress-ng 는 CPU stress-test 종료 뒤"
else
	ng "A: 4단계 stress-ng 는 CPU stress-test 종료 뒤" "END_a=$A_END START_b2=$B2_START"
fi

# 결과
R=$RUND/result.txt
fileok "A: result.txt 저장" "$R"
BAD=$(awk -F'|' 'NR>1 && $2=="완료" && ($3!="O" || $4!="O" || $5!="O")' "$R" 2> /dev/null)
check "A: 접속 가능 호스트 3열이 모두 O" "" "$BAD"
check "A: 완료 호스트 수 5" 5 "$(awk -F'|' 'NR>1 && $2=="완료"' "$R" | wc -l)"
check "A: h_off 접속불가 표시" "접속불가|-|-|-" "$(rowf "$R" h_off 2)|$(rowf "$R" h_off 3)|$(rowf "$R" h_off 4)|$(rowf "$R" h_off 5)"
check "A: h_reb 비고 재부팅" "실행도중 재부팅됨" "$(rowf "$R" h_reb 8)"
check "A: h_cpu 비고 없음" "" "$(rowf "$R" h_cpu 8)"
if grep -q 'h_off' "$S/st.log" "$FAKE_CC_LOG"; then ng "A: 접속불가 호스트는 작업 대상이 아님"; else ok "A: 접속불가 호스트는 작업 대상이 아님"; fi
AWP=$(tr -d '[:space:]' < "$RUND/worker.pid")
if wait_for 8 bash -c "! kill -0 $AWP 2> /dev/null"; then ok "A: 워커 종료"; else ng "A: 워커 종료"; fi
(cd "$S" && "$BIN" -run -c "$S/conf" -user tester "$S/hosts" < /dev/null > "$S/again.out" 2> "$S/again.err")
has "A: 끝난 작업 재실행 시 결과 파일 안내" "$S/again.out" '결과 파일:'

# ================================================================ G. 설정체크 '-'
echo "== G. 설정체크 '-' 판정"
scn_new G1
printf '%s\n' h_a > "$S/hosts"
addscn h_a ok cpu=nolog stress=0
mkconf "OS_CHECK_SH=$T/없는/config_check.sh"
start_run
pick_s() { echo s; }
drive "$RUND" 20 pick_s
check "G: OS_CHECK_SH 있고 실행 실패('-') → x 가 선택지에 있음(통과 아님)" "1:crxs=s" "$(cat "$RUND.asked")"
check "G: 중지 처리" "yes" "$(is_stopped "$RUND" && echo yes || echo no)"
scn_new G2
printf '%s\n' h_a > "$S/hosts"
addscn h_a ok cpu=nolog stress=0
mkconf "OS_CHECK_SH="
start_run
drive "$RUND" 20 pick_s
check "G: OS_CHECK_SH 비면 '-' 는 통과(제외 대상 없음)" "1:crs=s" "$(cat "$RUND.asked")"

# ================================================================ C. stress-test 멈춤 → 시간초과
echo "== C. stress-test 멈춤 → 시간초과 질문 → 재시도"
scn_new C
printf '%s\n' h_off h_cpu > "$S/hosts"
addscn h_off off
addscn h_cpu ok cpu=nolog stress=0
mkconf "STRESS_TIMEOUT=4s"
: > "$S/mark/hang_a"
pick_c_timeout() {
	if [ "$1" = 3 ] && [ ! -e "$S/seen_timeout" ]; then
		: > "$S/seen_timeout"
		rm -f "$S/mark/hang_a"
		echo r
	else
		echo c
	fi
}
start_run
drive "$RUND" 70 pick_c_timeout
check "C: 질문 순서(시간초과 → 재시도)" "1:crs=c 3:rs=r 3:crs=c 4:crs=c" "$(tr '\n' ' ' < "$RUND.asked" | sed 's/ $//')"
has "C: 질문 문구에 시간초과" "$RUND/worker.log" '시간초과'
P1=$(head -n1 "$S/st.pids")
if [ -n "$P1" ] && ! alive "$P1"; then ok "C: 멈춘 stress-test 프로세스가 정리됨"; else ng "C: 멈춘 stress-test 프로세스가 정리됨" "pid=$P1"; fi
check "C: 재시도로 stress-test a 2회 호출" 2 "$(nlines "$S/st.log" '^START a')"
check "C: 최종 CPU O" "O" "$(rowf "$RUND/result.txt" h_cpu 4)"

# ================================================================ D. 중지 요청(stop.req) → 실행 중 명령 종료
echo "== D. stop.req 로 실행 중인 stress-test 종료"
scn_new D
printf '%s\n' h_cpu > "$S/hosts"
addscn h_cpu ok cpu=nolog stress=0
mkconf "STRESS_TIMEOUT=60s"
: > "$S/mark/hang_a"
start_run
drive "$RUND" 40 pick_c &
DPID=$!
if wait_for 30 test -s "$S/st.pids"; then ok "D: 멈춘 stress-test 시작됨"; else ng "D: 멈춘 stress-test 시작됨"; fi
P1=$(head -n1 "$S/st.pids")
WP=$(tr -d '[:space:]' < "$RUND/worker.pid")
: > "$RUND/stop.req"
if wait_for 15 is_stopped "$RUND"; then ok "D: 중지 상태 기록"; else ng "D: 중지 상태 기록"; fi
if [ -n "$P1" ] && wait_for 8 bash -c "! kill -0 $P1 2> /dev/null"; then ok "D: 실행 중이던 stress-test 가 죽음"; else ng "D: 실행 중이던 stress-test 가 죽음" "pid=$P1"; fi
if wait_for 8 bash -c "! kill -0 $WP 2> /dev/null"; then ok "D: 워커 종료"; else ng "D: 워커 종료"; fi
fileok "D: 중지 시에도 result.txt 저장" "$RUND/result.txt"
if [ -e "$RUND/stop.req" ]; then ng "D: stop.req 정리"; else ok "D: stop.req 정리"; fi
wait "$DPID" 2> /dev/null

# ================================================================ E. 워커 kill -9 → 이어서 진행
echo "== E. 워커 kill -9 후 다시 붙어 이어서 진행"
scn_new E
printf '%s\n' h_off h_cpu h_reb > "$S/hosts"
addscn h_off off
addscn h_cpu ok cpu=nolog stress=0
addscn h_reb ok cpu=nolog stress=0
mkconf
start_run
drive "$RUND" 70 pick_c &
DPID=$!
launched() { grep -q '^  "cpu_launch": "20' "$RUND/state.json" 2> /dev/null && grep -q '^MON a' "$S/st.log"; }
if wait_for 30 launched; then ok "E: CPU launch 기록됨"; else ng "E: CPU launch 기록됨"; fi
WP=$(tr -d '[:space:]' < "$RUND/worker.pid")
kill -9 "$WP" 2> /dev/null
wait_for 5 bash -c "! kill -0 $WP 2> /dev/null"
check "E: 워커 죽음" "dead" "$(alive "$WP" && echo alive || echo dead)"
check "E: 아직 5단계가 아님" 3 "$(top_stage "$RUND")"
(cd "$S" && echo y | "$BIN" -run -c "$S/conf" -user tester "$S/hosts" > "$S/resume.out" 2> "$S/resume.err")
has "E: 재기동 안내" "$S/resume.err" '워커 재기동'
wait "$DPID"
check "E: 5단계(완료)까지 진행" 5 "$(top_stage "$RUND")"
check "E: CPU 는 다시 launch 하지 않음(a 1회)" 1 "$(nlines "$S/st.log" '^START a')"
check "E: stress-ng 단계 1회" 1 "$(nlines "$S/st.log" '^START b')"
has "E: 재기동 로그" "$RUND/worker.log" '재기동'
BAD=$(awk -F'|' 'NR>1 && $2=="완료" && ($3!="O" || $4!="O" || $5!="O")' "$RUND/result.txt" 2> /dev/null)
check "E: 최종 3열 모두 O" "" "$BAD"

echo
echo "결과: PASS $PASS, FAIL $FAIL"
[ "$FAIL" -eq 0 ]
