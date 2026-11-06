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
	unset FAKE_ST_REBOOT FAKE_ST_EXIT FAKE_ST_DELAY FAKE_ST_BUSY FAKE_ST_BUSY_TIMES FAKE_ST_BUSY_SLEEP BADFIRST
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
check "A: 질문 순서(1→2→3→4, 선택지; 3·4단계는 stress-ng 종료 시각 입력 질문이 먼저)" "1:crxs=c 2:crs=c 3:cs=c 3:crs=c 4:cs=c 4:crs=c" "$(tr '\n' ' ' < "$RUND.asked" | sed 's/ $//')"

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
check "C: 질문 순서(시간초과 → 재시도; 4단계는 종료 시각 질문이 먼저)" "1:crs=c 3:rs=r 3:crs=c 4:cs=c 4:crs=c" "$(tr '\n' ' ' < "$RUND.asked" | sed 's/ $//')"
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
check "H: 질문 순서(종료 시각 입력 질문이 stress-ng 실행보다 먼저)" "1:crs=c 3:cs=c 3:crs=c" "$(tr '\n' ' ' < "$RUND.asked" | sed 's/ $//')"
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
check "I: 질문 순서(인벤토리 질문 r 두 번은 3단계 그대로)" "1:crs=c 3:cs=c 3:rx=r 3:rx=r 3:crs=c" "$(tr '\n' ' ' < "$RUND.asked" | sed 's/ $//')"
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
check "J: 질문 순서(x → 3단계 질문에 x 선택지)" "1:crs=c 3:cs=c 3:rx=x 3:rxs=s" "$(tr '\n' ' ' < "$RUND.asked" | sed 's/ $//')"
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
if wait_q "$RUND" 30 cs; then ok "K: 종료 시각 입력 질문"; else ng "K: 종료 시각 입력 질문"; fi
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
wait_q "$RUND" 30 cs && send_answer "$RUND" "$QID" c "$ENDVALS"
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

echo
echo "결과: PASS $PASS, FAIL $FAIL"
[ "$FAIL" -eq 0 ]
