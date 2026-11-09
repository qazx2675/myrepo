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
	unset FAKE_ST_REBOOT FAKE_ST_EXIT FAKE_ST_DELAY FAKE_ST_BUSY FAKE_ST_BUSY_TIMES FAKE_ST_BUSY_SLEEP BADFIRST FAKE_YML_TAMPER FAKE_YS_EXIT
	export FAKE_YML_DIR=$S/yml
	: > "$S/st.log"
	: > "$S/st.pids"
	: > "$S/st.conf"
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
# qinfo <상태폴더> : 질문이 있으면 "id 단계 선택키들 [I]" (없으면 빈 출력, 입력 항목이 있는 질문이면 끝에 I)
qinfo() {
	awk '
	/^  "question": null/ {exit}
	/^  "question": \{/ {q=1; next}
	q && /^    "id":/ {gsub(/[",]/, "", $2); id=$2}
	q && /^    "stage":/ {gsub(/,/, "", $2); st=$2}
	q && /^    "choices":/ {sec="c"}
	q && /^    "inputs":/ {sec="i"; inp="I"}
	q && sec=="c" && /^        "key":/ {gsub(/[",]/, "", $2); keys=keys $2}
	q && /^  \}/ {print id, st, keys, inp; exit}
	' "$1/state.json" 2> /dev/null
}

# 종료 시각 입력 질문에 답할 값(내일 이후의 미래 시각, 앞자리 0 은 그대로)
ENDDATE=$(date -d '+2 days' '+%Y %m %d %H %M')
read -r EY EM ED EH EN <<< "$ENDDATE"
ENDARG="$EY,$EM,$ED,$EH,$EN"
ENDVALS=$(printf '{"end_year":"%s","end_month":"%s","end_day":"%s","end_hour":"%s","end_min":"%s"}' "$EY" "$EM" "$ED" "$EH" "$EN")
BADVALS='{"end_year":"2020","end_month":"1","end_day":"1","end_hour":"1","end_min":"1"}'
# send_answer <상태폴더> <질문번호> <선택> [values JSON] : answer.json 을 원자적으로 쓴다
send_answer() {
	local dir=$1 id=$2 ch=$3 vals=${4:-}
	if [ -n "$vals" ]; then
		printf '{"id":"%s","choice":"%s","values":%s}\n' "$id" "$ch" "$vals" > "$dir/.answer.tmp"
	else
		printf '{"id":"%s","choice":"%s"}\n' "$id" "$ch" > "$dir/.answer.tmp"
	fi
	mv "$dir/.answer.tmp" "$dir/answer.json"
}

# drive <상태폴더> <제한초> <선택함수> : 질문이 나오면 `선택함수 단계 선택키들 질문번호` 가 낸 글자로 답한다.
# 입력 항목이 있는 질문(종료 시각)은 c 이면 ENDVALS 값을 담아 답한다(BADFIRST=1 이면 먼저 틀린 값을 보내 무시되는지 본다).
# 5단계에 이르거나 중지되면 0, 제한 시간이 지나면 1. 받은 질문은 <상태폴더>.asked 에 "단계:선택키들=답" 으로 쌓는다.
drive() {
	local dir=$1 end=$((SECONDS + $2)) pick=$3 last="" info id st keys inp ans
	while [ "$SECONDS" -lt "$end" ]; do
		if [ "$(top_stage "$dir")" = 5 ] || is_stopped "$dir"; then return 0; fi
		info=$(qinfo "$dir")
		if [ -n "$info" ]; then
			inp=""
			read -r id st keys inp <<< "$info"
			if [ "$id" != "$last" ]; then
				ans=$($pick "$st" "$keys" "$id")
				[ -n "$ans" ] || ans=c
				echo "$st:$keys=$ans" >> "$dir.asked"
				if [ "$inp" = I ] && [ "$ans" = c ]; then
					if [ -n "${BADFIRST:-}" ]; then
						send_answer "$dir" "$id" c "$BADVALS"
						wait_for 10 grep -q '무시한 답: 입력값 오류' "$dir/worker.log"
					fi
					send_answer "$dir" "$id" c "$ENDVALS"
				else
					send_answer "$dir" "$id" "$ans"
				fi
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
check "A: 질문 순서(1→2→3→4, 선택지; 3·4단계는 stress-ng 종료 시각 입력 질문이 먼저)" "1:crxs=c 2:crs=c 3:cks=c 3:crs=c 4:cks=c 4:crs=c" "$(tr '\n' ' ' < "$RUND.asked" | sed 's/ $//')"

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
has "A: new_setup 의 gossh 경로를 GOSSH 로 넘김" "$S/st.log" "^START a [0-9]+ .* gossh=$GB/gossh$"
has "A: stress-ng(-steps b)에는 -end 전달" "$S/st.log" "^START b [0-9]+ user=tester .* end=$ENDARG gossh=.*$"
check "A: CPU(-steps a)에는 -end 없음" 1 "$(nlines "$S/st.log" '^START a .* end=none gossh=.*$')"
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
check "C: 질문 순서(시간초과 → 재시도; 4단계는 종료 시각 질문이 먼저)" "1:crs=c 3:rs=r 3:crs=c 4:cks=c 4:crs=c" "$(tr '\n' ' ' < "$RUND.asked" | sed 's/ $//')"
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

# ================================================================ H. stress-ng 종료 시각 입력 → stress-test -end
echo "== H. 종료 시각 입력 질문(answer.json values) → stress-test -end, 틀린 값은 무시"
scn_new H
printf '%s\n' h_ng > "$S/hosts"
addscn h_ng ok stress=0
mkconf
export BADFIRST=1
start_run
drive "$RUND" 60 pick_c
check "H: 5단계(완료)까지 진행" 5 "$(top_stage "$RUND")"
check "H: 질문 순서(종료 시각 입력 질문이 stress-ng 실행보다 먼저)" "1:crs=c 3:cks=c 3:crs=c" "$(tr '\n' ' ' < "$RUND.asked" | sed 's/ $//')"
has "H: 틀린 값(연도 2020)은 무시" "$RUND/worker.log" '무시한 답: 입력값 오류'
check "H: stress-test b 에 -end 전달" "$ENDARG" "$(sed -n 's/^START b [0-9]* .* end=\([^ ]*\) .*$/\1/p' "$S/st.log" | head -n1)"
has "H: state.json 에 stress_end 기록" "$RUND/state.json" "\"stress_end\": \"$ENDARG\""
check "H: 최종 stress-ng O" "O" "$(rowf "$RUND/result.txt" h_ng 5)"
unset BADFIRST

# ================================================================ I. 인벤토리 사용 중 → r, r → 계속
echo "== I. 인벤토리 사용 중 질문(단계 그대로) → r, r → 계속"
pick_busy_r() { case $2 in rx) echo r ;; *) echo c ;; esac; }
scn_new I
printf '%s\n' h_ng > "$S/hosts"
addscn h_ng ok stress=0
mkconf
export FAKE_ST_BUSY=b FAKE_ST_BUSY_TIMES=2
start_run
drive "$RUND" 60 pick_busy_r
check "I: 5단계(완료)까지 진행" 5 "$(top_stage "$RUND")"
check "I: 질문 순서(인벤토리 질문 r 두 번은 3단계 그대로)" "1:crs=c 3:cks=c 3:rx=r 3:rx=r 3:crs=c" "$(tr '\n' ' ' < "$RUND.asked" | sed 's/ $//')"
check "I: stress-test 가 r 두 번을 stdin 으로 받음" "b:r b:r" "$(sed -n 's/^BUSYANS \(b\) \(.*\)$/\1:\2/p' "$S/st.log" | tr '\n' ' ' | sed 's/ $//')"
check "I: 인벤토리 사용 중 줄 2회" 2 "$(nlines "$S/st.log" '^BUSY b')"
check "I: [감시] 줄까지 갔음(계속 진행)" 1 "$(nlines "$S/st.log" '^MON b')"
check "I: stress-test 1회만 실행(묶음 재시작 아님)" 1 "$(nlines "$S/st.log" '^START b')"
check "I: 최종 stress-ng O" "O" "$(rowf "$RUND/result.txt" h_ng 5)"

# ================================================================ J. 인벤토리 사용 중 → x → 이 묶음 중단(미실행)
echo "== J. 인벤토리 사용 중 질문 → x → 그 묶음 중단, 단계 질문에 안내"
pick_busy_x() { case $2 in rx) echo x ;; rxs) echo s ;; *) echo c ;; esac; }
scn_new J
printf '%s\n' h_ng > "$S/hosts"
addscn h_ng ok stress=0
mkconf
export FAKE_ST_BUSY=b
start_run
drive "$RUND" 60 pick_busy_x
check "J: 질문 순서(x → 3단계 질문에 x 선택지)" "1:crs=c 3:cks=c 3:rx=x 3:rxs=s" "$(tr '\n' ' ' < "$RUND.asked" | sed 's/ $//')"
check "J: stress-test 가 x 를 받고 중단" "b:x" "$(sed -n 's/^BUSYANS \(b\) \(.*\)$/\1:\2/p' "$S/st.log")"
check "J: [감시] 줄 없음(묶음 미실행)" 0 "$(nlines "$S/st.log" '^MON b')"
has "J: 종료코드 1 안내가 질문에 붙음" "$RUND/worker.log" '미실행/실패 호스트가 있습니다'
check "J: stress-ng 열 NOTRUN 그대로" "NOTRUN" "$(rowf "$RUND/result.txt" h_ng 5)"
check "J: 중지 처리" "yes" "$(is_stopped "$RUND" && echo yes || echo no)"

# ================================================================ K. 인벤토리 사용 중 질문 대기 중 중지(stop.req)
echo "== K. 인벤토리 사용 중 질문 대기 중 중지 요청"
QID=""
# wait_q <상태폴더> <제한초> <선택키들> : 그 선택키들의 질문이 뜰 때까지 기다려 번호를 QID 에 둔다
wait_q() {
	local end=$((SECONDS + $2)) info id st keys inp
	while [ "$SECONDS" -lt "$end" ]; do
		info=$(qinfo "$1")
		if [ -n "$info" ]; then
			inp=""
			read -r id st keys inp <<< "$info"
			if [ "$keys" = "$3" ]; then QID=$id; return 0; fi
		fi
		sleep 0.2
	done
	return 1
}
scn_new K
printf '%s\n' h_ng > "$S/hosts"
addscn h_ng ok stress=0
mkconf
export FAKE_ST_BUSY=b
start_run
wait_q "$RUND" 30 crs && send_answer "$RUND" "$QID" c
if wait_q "$RUND" 30 cks; then ok "K: 종료 시각 입력 질문"; else ng "K: 종료 시각 입력 질문"; fi
send_answer "$RUND" "$QID" c "$ENDVALS"
if wait_q "$RUND" 30 rx; then ok "K: 인벤토리 사용 중 질문(3단계 그대로)"; else ng "K: 인벤토리 사용 중 질문"; fi
check "K: 단계는 3 그대로" 3 "$(top_stage "$RUND")"
KWP=$(tr -d '[:space:]' < "$RUND/worker.pid")
: > "$RUND/stop.req"
if wait_for 15 is_stopped "$RUND"; then ok "K: 중지 상태 기록"; else ng "K: 중지 상태 기록"; fi
P1=$(head -n1 "$S/st.pids")
if [ -n "$P1" ] && wait_for 8 bash -c "! kill -0 $P1 2> /dev/null"; then ok "K: 대기 중이던 stress-test 가 끝남"; else ng "K: 대기 중이던 stress-test 가 끝남" "pid=$P1"; fi
if wait_for 8 bash -c "! kill -0 $KWP 2> /dev/null"; then ok "K: 워커 종료"; else ng "K: 워커 종료"; fi
fileok "K: 중지 시에도 result.txt 저장" "$RUND/result.txt"
check "K: 질문이 남지 않음" "" "$(qinfo "$RUND")"

# ================================================================ L. 질문 대기 중에는 STRESS_TIMEOUT 이 멈춘다
echo "== L. 인벤토리 사용 중 질문에 오래 답하지 않아도 시간초과로 죽지 않음 → 답하면 새 제한으로 계속"
scn_new L
printf '%s\n' h_ng > "$S/hosts"
addscn h_ng ok stress=0
mkconf "STRESS_TIMEOUT=4s"
export FAKE_ST_BUSY=b
start_run
wait_q "$RUND" 30 crs && send_answer "$RUND" "$QID" c
wait_q "$RUND" 30 cks && send_answer "$RUND" "$QID" c "$ENDVALS"
if wait_q "$RUND" 30 rx; then ok "L: 인벤토리 사용 중 질문"; else ng "L: 인벤토리 사용 중 질문"; fi
LQ=$QID
sleep 7 # STRESS_TIMEOUT(4초)보다 오래 답하지 않는다
P1=$(head -n1 "$S/st.pids")
if [ -n "$P1" ] && alive "$P1"; then ok "L: 7초 지나도 stress-test 가 살아 있음(제한 일시정지)"; else ng "L: stress-test 가 죽음" "pid=$P1"; fi
wait_q "$RUND" 5 rx
check "L: 질문이 그대로 대기 중" "$LQ" "$QID"
send_answer "$RUND" "$LQ" r
q_gone() { [ "$(qinfo "$RUND" | cut -d' ' -f1)" != "$LQ" ]; }
wait_for 10 q_gone # 같은 질문에 drive 가 다시 답하지 않게 워커가 받아 갈 때까지 기다린다
drive "$RUND" 40 pick_c
check "L: 답한 뒤 5단계(완료)까지 진행" 5 "$(top_stage "$RUND")"
if grep -q '시간초과' "$RUND/worker.log"; then ng "L: 시간초과가 나지 않아야 함"; else ok "L: 시간초과 없음"; fi
check "L: 최종 stress-ng O" "O" "$(rowf "$RUND/result.txt" h_ng 5)"

# ================================================================ M. 실행 중 설정을 고치고 재시도하면 반영
echo "== M. STRESS_TEST_CONF 상대경로(설정 파일 폴더 기준) 없음 → 오류 질문 → 설정에서 비우고 r → 다시 읽어 진행"
# 첫 r/s 질문에서 설정 파일의 STRESS_TEST_CONF 를 지우고 r
pick_fixconf() {
	case $2 in
		rs) sed -i '/^STRESS_TEST_CONF=/d' "$S/conf"; echo r ;;
		*) echo c ;;
	esac
}
scn_new M
printf '%s\n' h_m > "$S/hosts"
addscn h_m ok cpu=nolog stress=0
mkconf "STRESS_TEST_CONF=stress.conf"
start_run
drive "$RUND" 60 pick_fixconf
check "M: 첫 호출은 설정 파일 폴더 기준 절대경로" "conf=$S/stress.conf" "$(sed -n 's/^START a [0-9]* .* \(conf=[^ ]*\) .*/\1/p' "$S/st.log" | head -n1)"
check "M: 재시도 호출은 -conf 없음(고친 설정 반영)" "conf=none" "$(sed -n 's/^START a [0-9]* .* \(conf=[^ ]*\) .*/\1/p' "$S/st.log" | sed -n 2p)"
has "M: 워커가 설정을 다시 읽음" "$RUND/worker.log" '설정 파일 다시 읽음'
check "M: 5단계(완료)까지 진행" 5 "$(top_stage "$RUND")"
check "M: 최종 CPU O" "O" "$(rowf "$RUND/result.txt" h_m 4)"

# ================================================================ N. CPU X 호스트 재실행 질문: y
echo "== N. CPU X 호스트 중 부하 낮은 것만 묻고 재실행(y), 부하 높은 것은 건너뜀"
pick_y() {
	case $2 in
		yns) echo y ;;
		*) echo c ;;
	esac
}
scn_new N
printf '%s\n' h_low h_high h_ok > "$S/hosts"
addscn h_low ok cpu=ng stress=0 load=0.5
addscn h_high ok cpu=ng stress=0 load=5
addscn h_ok ok
mkconf
start_run
drive "$RUND" 80 pick_y
check "N: 5단계(완료)까지 진행" 5 "$(top_stage "$RUND")"
check "N: 질문 순서(부하 단계에서 y/n/s 질문이 먼저)" "1:crs=c 3:yns=y 3:crs=c 4:cks=c 4:crs=c" "$(tr '\n' ' ' < "$RUND.asked" | sed 's/ $//')"
check "N: 부하 확인(cat /proc/loadavg) 호출" 1 "$(grep -c '^cat /proc/loadavg$' "$FAKE_GOSSH_LOG")"
check "N: CPU(-steps a) 대상은 부하 낮은 h_low 뿐" "h_low" "$(st_hosts a)"
if grep -q 'h_high' "$S/st.log"; then ng "N: 부하 높은 h_high 는 stress-test 대상이 아님"; else ok "N: 부하 높은 h_high 는 stress-test 대상이 아님"; fi
has "N: 질문에 부하 표시" "$RUND/worker.log" 'h_low\(0\.50\)'
has "N: 건너뜀 기록" "$RUND/worker.log" '낮음 1, 높음\(아직 도는 중\) 1, 응답 없음 0'
R=$RUND/result.txt
check "N: h_low CPU O (재실행됨)" "O" "$(rowf "$R" h_low 4)"
check "N: h_high CPU X 그대로" "X" "$(rowf "$R" h_high 4)"
check "N: h_low 비고 없음(재실행 표시 꺼짐)" "" "$(rowf "$R" h_low 8)"
has "N: state.json 에 cpu_retry 가 남지 않음" "$RUND/state.json" '^  "stage": 5'
if grep -q '"cpu_retry": true' "$RUND/state.json"; then ng "N: cpu_retry 표시가 끝난 뒤 꺼짐"; else ok "N: cpu_retry 표시가 끝난 뒤 꺼짐"; fi

# ================================================================ O. CPU X 호스트 재실행 질문: n → 할 일 없으면 부하 단계 건너뜀
echo "== O. n → 모두 제외(X 그대로), 돌릴 것이 없으면 부하 단계를 질문 없이 넘어감"
pick_n() {
	case $2 in
		yns) echo n ;;
		*) echo c ;;
	esac
}
scn_new O
printf '%s\n' h_low h_high > "$S/hosts"
addscn h_low ok cpu=ng load=0.5
addscn h_high ok cpu=ng load=5
mkconf
start_run
drive "$RUND" 40 pick_n
check "O: 5단계(완료)까지 진행" 5 "$(top_stage "$RUND")"
check "O: 질문은 판정과 y/n 질문뿐" "1:crs=c 3:yns=n" "$(tr '\n' ' ' < "$RUND.asked" | sed 's/ $//')"
check "O: stress-test 호출 없음" 0 "$(nlines "$S/st.log" '^START')"
check "O: h_low CPU X 그대로" "X" "$(rowf "$RUND/result.txt" h_low 4)"
OWP=$(tr -d '[:space:]' < "$RUND/worker.pid")
wait_for 8 bash -c "! kill -0 $OWP 2> /dev/null"

# ================================================================ P. -new: 이전 상태를 .old-<시각> 으로 보관하고 처음부터
echo "== P. -new (끝난 상태는 보관 후 새로 시작, 워커가 살아 있으면 거절)"
(cd "$S" && "$BIN" -run -new -c "$S/conf" -user tester "$S/hosts" < /dev/null > "$S/new.out" 2> "$S/new.err")
check "P: -new 시작 종료코드 0" 0 "$?"
OLDS=$(ls -d "$S"/hosts_run.old-* 2> /dev/null | wc -l)
check "P: 이전 상태 폴더 보관 1개" 1 "$OLDS"
OLD1=$(ls -d "$S"/hosts_run.old-* 2> /dev/null | head -n1)
check "P: 보관본은 끝난 상태(5단계) 그대로" 5 "$(top_stage "$OLD1")"
has "P: 보관 위치 안내" "$S/new.err" '이전 상태를 지우지 않고 보관했습니다'
if wait_q "$RUND" 30 crs; then ok "P: 새 작업이 1단계 질문부터 시작"; else ng "P: 새 작업이 1단계 질문부터 시작"; fi
PWP=$(tr -d '[:space:]' < "$RUND/worker.pid")
(cd "$S" && "$BIN" -run -new -c "$S/conf" -user tester "$S/hosts" < /dev/null > "$S/new2.out" 2> "$S/new2.err")
check "P: 워커가 살아 있으면 -new 거절(종료코드 2)" 2 "$?"
has "P: 거절 안내" "$S/new2.err" 'S 로 작업을 중지'
check "P: 거절 시 보관 폴더가 늘지 않음" 1 "$(ls -d "$S"/hosts_run.old-* 2> /dev/null | wc -l)"
check "P: 거절 시 진행 중 작업의 상태가 그대로" 1 "$(top_stage "$RUND")"
send_answer "$RUND" "$QID" s
wait_for 15 is_stopped "$RUND"
wait_for 8 bash -c "! kill -0 $PWP 2> /dev/null"
(cd "$S" && "$BIN" -new -c "$S/conf" "$S/hosts" < /dev/null > /dev/null 2> "$S/new3.err")
check "P: -new 는 -run 없이 쓰면 오류(종료코드 1)" 1 "$?"

# ================================================================ Q. -auto: 질문 없이 끝까지
echo "== Q. -run -auto (설정수정 자동 → CPU 묶음 → stress-ng 종료 시각 자동 → 늦게 접속된 호스트도 처리 → 질문 없이 완료)"
pick_none() { echo s; } # -auto 는 질문을 내면 안 된다(나오면 .asked 에 남고 중지)
scn_new Q
printf '%s\n' h_off h_fix h_cpu h_both h_done > "$S/hosts"
addscn h_off off
addscn h_fix ok cpu=nolog stress=0
addscn h_cpu ok cpu=nolog stress=0
addscn h_both ok stress=0
addscn h_done ok
printf 'h_fix\tfail\n' > "$S/cc_scenario"
mkconf "AUTO_CPU_WAIT=3s" "AUTO_CPU_INTERVAL=1s" "AUTO_CPU_MAX=3s" "AUTO_RECHECK=4s" "AUTO_LIMIT=60s"
(cd "$S" && "$BIN" -run -auto -c "$S/conf" -user tester "$S/hosts" < /dev/null > "$S/start.out" 2> "$S/start.err")
check "Q: -run -auto 시작 종료코드 0" 0 "$?"
RUND=$S/hosts_run
has "Q: state.json 에 auto 상태" "$RUND/state.json" '^  "auto": \{'
# 첫 CPU 묶음이 뜨면 접속불가였던 h_off 를 살린다(⑥ 재판정으로 대기열 뒤에 붙는지)
if wait_for 30 grep -q '^START a' "$S/st.log"; then ok "Q: 첫 CPU 묶음 launch"; else ng "Q: 첫 CPU 묶음 launch"; fi
sed -i 's/^h_off off$/h_off ok cpu=nolog stress=0/' "$S/scenario"
drive "$RUND" 70 pick_none
check "Q: 5단계(완료)까지 진행" 5 "$(top_stage "$RUND")"
if [ -s "$RUND.asked" ]; then ng "Q: 질문 없음" "$(cat "$RUND.asked")"; else ok "Q: 질문 없음"; fi
check "Q: config_check -auto 1회(h_fix)" "LIST=h_fix" "$(grep -A4 'argv=-auto tester' "$FAKE_CC_LOG" | grep '^LIST=')"
check "Q: CPU 묶음 2개(대기열 순서: 처음부터 O 인 h_cpu, 수정 뒤 붙은 h_fix / 늦게 붙은 h_off)" "h_cpu,h_fix
h_off" "$(st_hosts a)"
check "Q: stress-ng 3번(h_both 먼저, CPU O 가 된 묶음 차례로)" "h_both
h_fix,h_cpu
h_off" "$(st_hosts b)"
QEND=$(date -d '+2 days' '+%Y,%m,%d,00,00')
check "Q: stress-ng 종료 시각은 +2일 00:00 자동" 3 "$(nlines "$S/st.log" "^START b .* end=$QEND gossh=")"
R=$RUND/result.txt
fileok "Q: result.txt 저장" "$R"
check "Q: 모든 호스트 완료|O|O|O" "" "$(awk -F'|' '/^$/{exit} NR>1 && ($2!="완료" || $3!="O" || $4!="O" || $5!="O")' "$R" 2> /dev/null)"
check "Q: 결과 행 5개" 5 "$(awk '/^$/{exit} NR>1' "$R" | wc -l)"
has "Q: result.txt 에 대수 요약" "$R" '^완료\(OOO\) 5대[|]남음 0대[|]포기 0대$'
has "Q: 끝 사유 기록" "$RUND/worker.log" '자동 진행 끝\(할 일이 끝남\): 모두 O 5대, 남음 0대, 포기 0대'
check "Q: launch 직전 yaml 확인 4회(첫 launch 제외, 바뀐 것 없어 질문 없음)" 4 "$(grep -c '^YAMLSTATE' "$S/st.log")"
check "Q: 등록 5회(CPU 2 + stress-ng 3)" 5 "$(grep -c '^REG' "$S/st.log")"
has "Q: stress-ng launch 즉시 O 표시" "$RUND/state.json" '"ng_launched": true'
QWP=$(tr -d '[:space:]' < "$RUND/worker.pid")
if wait_for 10 bash -c "! kill -0 $QWP 2> /dev/null"; then ok "Q: 워커 종료"; else ng "Q: 워커 종료"; fi
(cd "$S" && "$BIN" -auto -c "$S/conf" "$S/hosts" < /dev/null > /dev/null 2> "$S/auto2.err")
check "Q: -auto 는 -run 없이 쓰면 오류(종료코드 1)" 1 "$?"

# ================================================================ R. -auto: launch 직전 yaml 바뀜 질문(y 로 덮어쓰고 계속 / s 로 중지)
echo "== R. -run -auto (첫 launch 는 묻지 않고, 다른 사람이 인벤토리를 바꾸면 CPU launch 직전에 y/s 질문 → 기다리는 동안 재판정 계속 → y 로 덮어쓰고 완료 / s 로 중지)"
q_up() { [ -n "$(qinfo "$1")" ]; }
q_clear() { [ -z "$(qinfo "$1")" ]; }
host_done() { grep -A4 "\"host\": \"$2\"" "$1/state.json" | grep -q '"status": "완료"'; }
setup_r() { # <이름> : h_c 는 stress-ng 대상(먼저 launch, 직후 인벤토리가 바뀜), h_a 는 CPU 대상, h_off 는 처음에 접속불가
	scn_new "$1"
	printf '%s\n' h_off h_a h_c > "$S/hosts"
	addscn h_off off
	addscn h_a ok cpu=nolog stress=0
	addscn h_c ok stress=0
	mkconf "AUTO_CPU_WAIT=3s" "AUTO_CPU_INTERVAL=1s" "AUTO_CPU_MAX=3s" "AUTO_RECHECK=2s" "AUTO_LIMIT=60s"
	export FAKE_YML_TAMPER=b
	(cd "$S" && "$BIN" -run -auto -c "$S/conf" -user tester "$S/hosts" < /dev/null > "$S/start.out" 2> "$S/start.err")
	RUND=$S/hosts_run
}
setup_r R
check "R: -run -auto 시작 종료코드 0" 0 "$?"
if wait_for 40 q_up "$RUND"; then ok "R: yaml 바뀜 질문이 나옴"; else ng "R: yaml 바뀜 질문이 나옴" "$(tail -5 "$RUND/worker.log")"; fi
read -r RQ _ RKEYS _ <<< "$(qinfo "$RUND")"
check "R: 선택지는 y(덮어쓰고 계속)·s(중지)" ys "$RKEYS"
has "R: 질문에 바뀐 yml 이름" "$RUND/state.json" 'os_install.yml'
has "R: 질문에 우리가 등록한 yml 이름" "$RUND/state.json" 'fake_b_1ea.yml'
has "R: 질문에 호스트 수(지금 3대 / 우리 1대)" "$RUND/state.json" '지금 3대 / 우리가 등록한 1대'
check "R: 첫 launch(stress-ng)는 묻지 않고 실행, CPU 는 아직 launch 전" "1 0" "$(nlines "$S/st.log" '^START b') $(nlines "$S/st.log" '^START a')"
# 답을 기다리는 동안에도 접속불가였던 h_off 의 10분(시험은 2초) 재판정이 계속된다
sed -i 's/^h_off off$/h_off ok cpu=nolog stress=0/' "$S/scenario"
if wait_for 30 host_done "$RUND" h_off; then ok "R: 질문 대기 중에도 재판정이 계속됨(h_off 완료)"; else ng "R: 질문 대기 중에도 재판정이 계속됨(h_off 완료)"; fi
if q_up "$RUND"; then ok "R: 그동안 질문은 그대로"; else ng "R: 그동안 질문은 그대로"; fi
check "R: 답 전에는 CPU launch 안 함" 0 "$(nlines "$S/st.log" '^START a')"
send_answer "$RUND" "$RQ" y
if wait_for 30 q_clear "$RUND"; then ok "R: y 답을 받아 질문이 걷힘"; else ng "R: y 답을 받아 질문이 걷힘"; fi
drive "$RUND" 70 pick_none
check "R: 5단계(완료)까지 진행" 5 "$(top_stage "$RUND")"
if [ -s "$RUND.asked" ]; then ng "R: 그 밖의 질문 없음" "$(cat "$RUND.asked")"; else ok "R: 그 밖의 질문 없음"; fi
has "R: 답 y 기록" "$RUND/worker.log" "답 $RQ: y"
has "R: 덮어쓰기 전 다른 작업의 호스트를 로그에 남김" "$RUND/worker.log" '지금 인벤토리 호스트 3대\(y 로 덮어쓰기 전 기록\): x y z'
check "R: y 뒤 CPU 묶음 1회(대기열 전체 h_a,h_off)" "h_a,h_off" "$(st_hosts a)"
check "R: 등록 3회(stress-ng, CPU 묶음, stress-ng)" "fake_b_1ea.yml fake_a_2ea.yml fake_b_3ea.yml" "$(sed -n 's/^REG [ab] [0-9]* yml=//p' "$S/st.log" | tr '\n' ' ' | sed 's/ $//')"
check "R: 끝난 뒤 AWX 소스는 마지막으로 등록한 우리 yml" fake_b_3ea.yml "$(cat "$S/yml/yml")"
check "R: 질문은 걷혀 있음" "" "$(qinfo "$RUND")"
R=$RUND/result.txt
check "R: 모든 호스트 완료|O|O|O" "" "$(awk -F'|' '/^$/{exit} NR>1 && ($2!="완료" || $3!="O" || $4!="O" || $5!="O")' "$R" 2> /dev/null)"

setup_r S
check "S: -run -auto 시작 종료코드 0" 0 "$?"
if wait_for 40 q_up "$RUND"; then ok "S: yaml 바뀜 질문이 나옴"; else ng "S: yaml 바뀜 질문이 나옴" "$(tail -5 "$RUND/worker.log")"; fi
read -r SQ _ <<< "$(qinfo "$RUND")"
send_answer "$RUND" "$SQ" s
if wait_for 30 is_stopped "$RUND"; then ok "S: s 로 작업 중지"; else ng "S: s 로 작업 중지"; fi
check "S: CPU 는 launch 하지 않음" 0 "$(nlines "$S/st.log" '^START a')"
check "S: 질문은 걷혀 있음" "" "$(qinfo "$RUND")"
SWP=$(tr -d '[:space:]' < "$RUND/worker.pid")
if wait_for 10 bash -c "! kill -0 $SWP 2> /dev/null"; then ok "S: 워커 종료"; else ng "S: 워커 종료"; fi

# ================================================================ X. B서버 원격 실행(STRESS_REMOTE, 가짜 ssh)
echo "== X. STRESS_REMOTE: stress-test 를 ssh 로 B 에서 실행(가짜 ssh 는 이 서버에서 sh -c), 목록 복사·askpass·-ignore-offline"
FAKESSH=$HERE/fake_ssh.sh
chmod +x "$FAKESSH"
SB=$T/sshb
mkdir -p "$SB"
{
	echo '#!/bin/bash'
	echo "exec bash \"$FAKESSH\" \"\$@\""
} > "$SB/ssh"
chmod +x "$SB/ssh"
XRD=/tmp/new_setup_tester
rm -rf "$XRD"
scn_new X
export FAKE_SSH_LOG=$S/ssh.log
: > "$S/ssh.log"
printf '%s\n' h_cpu > "$S/hosts"
addscn h_cpu ok cpu=nolog stress=0
# 원격 모드에서는 상대경로가 거절된다
mkconf "STRESS_REMOTE=b-test" "STRESS_TEST=stress-test/stress-test"
start_run
check "X: 원격 모드 STRESS_TEST 상대경로 → 시작 거절(종료코드 2)" 2 "$?"
has "X: 거절 안내" "$S/start.err" 'STRESS_REMOTE 를 쓰면 STRESS_TEST 는 B서버의 절대경로'
rm -rf "$S/hosts_run"
mkconf "STRESS_REMOTE=b-test" "STRESS_IGNORE_OFFLINE=y"
(cd "$S" && PATH="$SB:$PATH" "$BIN" -run -c "$S/conf" -user tester "$S/hosts" < /dev/null > "$S/start.out" 2> "$S/start.err")
check "X: -run 시작 종료코드 0" 0 "$?"
RUND=$S/hosts_run
drive "$RUND" 80 pick_c
check "X: 5단계(완료)까지 진행" 5 "$(top_stage "$RUND")"
check "X: CPU(-steps a) launch 1회(대상 h_cpu)" "h_cpu" "$(st_hosts a)"
has "X: CPU 목록 파일을 B 의 /tmp/new_setup_<user>/ 로 복사" "$XRD/cpu.txt" '^h_cpu$'
has "X: stress-test 는 복사한 목록을 받음" "$S/ssh.log" "^CALL root@b-test .*-hosts.*$XRD/cpu\\.txt"
has "X: 원격 pid 파일 기록" "$XRD/a.pid" '^[0-9]+$'
has "X: worker.log 의 원격 명령 줄" "$RUND/worker.log" "^\\+ ssh root@b-test \\(cd $HERE && $FAKEST -user tester -hosts $XRD/cpu\\.txt -steps a -batch -conf $S/st\\.conf -ignore-offline\\)$"
has "X: -ignore-offline 전달(CPU)" "$S/st.log" '^IGNOREOFF a$'
has "X: B 에는 A 의 gossh 경로를 넘기지 않음" "$S/st.log" '^START a [0-9]+ .* gossh=none$'
has "X: ssh 가 SSH_ASKPASS 로 비밀번호를 받음" "$S/ssh.log" '^ASKPASS ok$'
if grep -q 'ASKPASS \(fail\|none\)' "$S/ssh.log"; then ng "X: 모든 ssh 호출이 askpass 사용" "$(grep ASKPASS "$S/ssh.log" | sort | uniq -c)"; else ok "X: 모든 ssh 호출이 askpass 사용"; fi
if grep -rqF 'abc135!!' "$RUND" "$S/ssh.log" "$S/st.log" "$S/start.out" "$S/start.err"; then ng "X: 비밀번호가 로그·상태에 없음"; else ok "X: 비밀번호가 로그·상태에 없음"; fi
XWP=$(tr -d '[:space:]' < "$RUND/worker.pid")
if wait_for 8 bash -c "! kill -0 $XWP 2> /dev/null"; then ok "X: 워커 종료"; else ng "X: 워커 종료"; fi
unset FAKE_SSH_LOG
rm -rf "$XRD"

echo
echo "결과: PASS $PASS, FAIL $FAIL"
[ "$FAIL" -eq 0 ]
