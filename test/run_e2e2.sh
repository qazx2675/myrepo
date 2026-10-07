#!/bin/bash
# run_e2e2.sh - auto_setup 2차 목업 E2E (계획서 §16): 양방향 동일성·완료기록·요청/TUI·데몬 제어·LDAP·2차 체크
#
# 방식: run_e2e.sh 와 같다 — 스크래치(mktemp -d) + AUTO_SETUP_DIR + PATH 스텁(gossh/ssh/wall), 원본 01/os_check 는 읽기만 하고 복사본만 사용,
#       빈 변수는 스크래치 복사본 빌드 시 -ldflags -X 로만 주입(소스는 빈 값 유지). ICMP 는 127.0.0.x(항상 up) / 192.0.2.1(항상 down → os6).
#       원격 클라이언트 모드는 "스텁 gossh 가 같은 머신의 서버용 바이너리를 호출" 하는 방식으로 검증한다. (root 필요: raw ICMP)
# 실행: bash test/run_e2e2.sh [시나리오...]   시나리오: a 양방향 b 완료기록 c 요청·TUI d 데몬제어 e LDAP f 2차체크 (기본 전부)
#       KEEP_SCRATCH=1 이면 스크래치 보존

ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd -P)
# shellcheck source=lib_e2e.sh
. "$ROOT/test/lib_e2e.sh"

[[ -d /usr/local/go/bin ]] && export PATH="$PATH:/usr/local/go/bin"
ORIG_PATH=$PATH

PASS_N=0; FAIL_N=0; SKIP_N=0
FAILS=()
ok()  { PASS_N=$((PASS_N + 1)); printf '  [PASS] %s\n' "$1"; }
bad() { FAIL_N=$((FAIL_N + 1)); FAILS+=("$1"); printf '  [FAIL] %s\n' "$1"; [[ -n $2 ]] && printf '         %s\n' "$2"; }
skip() { SKIP_N=$((SKIP_N + 1)); printf '  [SKIP] %s\n' "$1"; }
t_eq() { # 설명 실제 기대
	if [[ $2 == "$3" ]]; then ok "$1"; else bad "$1" "기대=[$3] 실제=[$2]"; fi
}
t_has() { # 설명 파일 정규식
	if grep -Eq -- "$3" "$2" 2> /dev/null; then ok "$1"; else bad "$1" "패턴 없음 /$3/ in $2"; fi
}
t_hasF() { # 설명 파일 고정문자열
	if grep -Fq -- "$3" "$2" 2> /dev/null; then ok "$1"; else bad "$1" "문자열 없음 [$3] in $2"; fi
}
t_no() { # 설명 파일 정규식
	if grep -Eq -- "$3" "$2" 2> /dev/null; then bad "$1" "있으면 안 되는 패턴 /$3/ in $2"; else ok "$1"; fi
}
t_same() { # 설명 파일1 파일2 (프로세스 치환도 가능 — 한 번 읽어 임시파일로 비교)
	cat "$2" > "$S/.ts1" 2> /dev/null; cat "$3" > "$S/.ts2" 2> /dev/null
	if cmp -s "$S/.ts1" "$S/.ts2"; then ok "$1"; else bad "$1" "차이: $(diff "$S/.ts1" "$S/.ts2" | head -6 | paste -sd'|')"; fi
}
wait_for() { # 초 명령...
	local t=$1 i
	shift
	for ((i = 0; i < t * 2; i++)); do
		"$@" > /dev/null 2>&1 && return 0
		sleep 0.5
	done
	return 1
}

wait_cond() { # 초 "표현식" (호출 시점의 지역변수를 eval 로 평가)
	local t=$1 i
	shift
	for ((i = 0; i < t * 2; i++)); do
		eval "$*" > /dev/null 2>&1 && return 0
		sleep 0.5
	done
	return 1
}

if [[ $(id -u) -ne 0 ]]; then
	echo "[SKIP] root 가 아니면 raw ICMP 를 열 수 없어 E2E 를 건너뜁니다 (root 로 실행하세요)"
	exit 0
fi
if ! command -v go > /dev/null 2>&1; then
	echo "[X] go 를 찾을 수 없습니다 (PATH 확인)"
	exit 1
fi

WANT=("$@")
want() { # 시나리오 글자
	local w
	[[ ${#WANT[@]} -eq 0 ]] && return 0
	for w in "${WANT[@]}"; do [[ $w == "$1" ]] && return 0; done
	return 1
}
for w in "${WANT[@]}"; do
	[[ $w =~ ^[a-f]$ ]] || { echo "사용법: bash test/run_e2e2.sh [a|b|c|d|e|f ...]"; exit 1; }
done

S=$(mktemp -d /tmp/as_e2e2.XXXXXX)
kill_all() { pkill -KILL -f "^$S/auto_setup[A-Za-z0-9_]* daemon$" 2> /dev/null; }
cleanup() {
	kill_all
	pkill -KILL -f "$S/" 2> /dev/null
	if [[ -n $KEEP_SCRATCH ]]; then
		echo "KEEP_SCRATCH: $S"
	else
		rm -rf "$S"
	fi
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

e2e_paths
unset AUTO_SETUP_NOW
export AUTO_SETUP_DIR="$S/as_default"   # 실수로 환경변수를 빠뜨려도 운영 /tmp/auto_setup 을 건드리지 않게

echo "== 준비: 스텁 · 빌드 (스크래치 $S)"
e2e_make_stubs || { echo "[X] 스텁 준비 실패"; exit 1; }
if ! e2e_build; then echo "[X] 빌드 실패"; exit 1; fi
if ! e2e_build_variants; then echo "[X] 변형 빌드 실패"; exit 1; fi
e2e_make_rgossh
BASE="$S/auto_setup"; SEC="$S/auto_setup_2nd"; CLI="$S/auto_setup_client"
STUBPATH="$BIN:$ORIG_PATH"
NOWV=""

# ---- 도우미 ----
lcl() { local d=$1; shift; env AUTO_SETUP_DIR="$d" AUTO_SETUP_NOW="$NOWV" NO_COLOR=1 "$BASE" "$@"; }
rem() { local d=$1; shift; env AUTO_SETUP_DIR="$d" AUTO_SETUP_NOW="$NOWV" NO_COLOR=1 PATH="$S/binr:$ORIG_PATH" RGOSSH_LOG="$E2E_SCEN/rgossh.log" "$CLI" "$@"; }
# 같은 상태 디렉터리에 같은 명령 → 로컬 vs 원격 클라이언트: stdout/stderr/종료코드가 모두 같아야 한다
cmp_lr() { # 설명 디렉터리 인자...
	local label=$1 d=$2
	shift 2
	lcl "$d" "$@" > "$S/l.out" 2> "$S/l.err"; echo $? > "$S/l.rc"
	rem "$d" "$@" > "$S/r.out" 2> "$S/r.err"; echo $? > "$S/r.rc"
	if cmp -s "$S/l.out" "$S/r.out" && cmp -s "$S/l.err" "$S/r.err" && cmp -s "$S/l.rc" "$S/r.rc"; then
		ok "$label (stdout $(wc -c < "$S/l.out")B, rc=$(cat "$S/l.rc"))"
	else
		bad "$label" "stdout: $(diff "$S/l.out" "$S/r.out" | head -3 | paste -sd'|') stderr: $(diff "$S/l.err" "$S/r.err" | head -2 | paste -sd'|') rc: $(cat "$S/l.rc") vs $(cat "$S/r.rc")"
	fi
}
dpid() { head -1 "$1/auto_setup.pid" 2> /dev/null | awk '{print $1}'; }
dalive() { local p; p=$(dpid "$1"); [[ -n $p ]] && kill -0 "$p" 2> /dev/null; }
dstart() { env AUTO_SETUP_DIR="$2" "$1" ensure; }   # 바이너리 디렉터리
dkill9() { local p; p=$(dpid "$1"); [[ -n $p ]] && kill -9 "$p"; sleep 1; }
dcount() { pgrep -f "^$S/auto_setup[A-Za-z0-9_]* daemon$" | wc -l; }
hasjob() { ls "$1"/jobs/*.json > /dev/null 2>&1; }
hasdone() { ls "$1"/jobs/done/*.json > /dev/null 2>&1; }
# queue 파일 직접 작성(원자): qjob 디렉터리 전달시각 호스트...
qjob() {
	local d=$1 t=$2 f
	shift 2
	mkdir -p "$d/queue"
	f="$d/queue/${t}_${E2E_USER}_$$.job"
	{ echo "user=$E2E_USER"; echo "time=$t"; printf '%s\n' "$@"; } > "$f.tmp" && mv "$f.tmp" "$f"
}
logn() { grep -cF -- "$2" "$1/auto_setup.log" 2> /dev/null; }
# lastlog 상태디렉터리 : 가장 최근 데몬 기동 이후의 로그만 (로그 1회 규칙은 데몬 프로세스 단위)
lastlog() { tail -n +"$(grep -n 'daemon 시작' "$1/auto_setup.log" | tail -1 | cut -d: -f1)" "$1/auto_setup.log"; }
# jfield 파일 호스트 정규식 : job JSON 의 호스트 블록에서 정규식에 맞는 줄을 공백·쉼표 없이 이어붙여 출력 (JSON 들여쓰기 1칸)
jfield() {
	awk -v h="  \"$2\": {" -v re="$3" '$0 == h { f = 1; next } f && /^  }/ { exit } f && $0 ~ re { gsub(/[ ,]/, ""); printf "%s", $0 }' "$1"
}

# ============================================================
sc_a() {
	echo "== a) 양방향 동일성(§16-3): status|snapshot|code|리포트 로컬 == 원격 클라이언트(스텁 gossh), 요청·완료·취소, 01 전달"
	e2e_scen a
	local N A c1 c2 a
	N=$(date +%s); NOWV=$N
	A="$S/a_state"; rm -rf "$A"; e2e_sample_state "$A" "$N"
	cmp_lr "a1 status" "$A" status
	cmp_lr "a2 snapshot(JSON)" "$A" snapshot
	cmp_lr "a3 code 4821" "$A" code 4821
	cmp_lr "a4 code 0000(없음, rc=1)" "$A" code 0000
	cmp_lr "a5 인자 없음 → plain 리포트" "$A"
	cmp_lr "a6 --plain" "$A" --plain
	t_hasF "a6 리포트에 그룹 gpu.yml" "$S/l.out" "gpu.yml"
	t_has "a6 리포트에 정체 1·실패 2(진행 job 합계)" "$S/l.out" '정체 1'
	t_eq "a7 snapshot 은 상태 디렉터리를 바꾸지 않음(요청·완료기록 디렉터리 비어 있음)" "$(find "$A/requests" "$A/done" -type f | wc -l)" 0
	rem "$A" status > "$S/r.out" 2> /dev/null
	t_hasF "a8 원격 gossh 호출 기록(호스트=os8.mgmt)" "$E2E_SCEN/rgossh.log" "os8.mgmt sh -c "

	# 변경 명령: 같은 샘플의 사본 2개에 로컬/원격을 각각 적용 → 출력·결과 파일 동일
	c1="$S/a_c1"; c2="$S/a_c2"
	rm -rf "$c1" "$c2"; cp -a "$A" "$c1"; cp -a "$A" "$c2"
	lcl "$c1" cancel 20261003-100000-bob > "$S/l.out" 2> "$S/l.err"; echo $? > "$S/l.rc"
	rem "$c2" cancel 20261003-100000-bob > "$S/r.out" 2> "$S/r.err"; echo $? > "$S/r.rc"
	t_same "a9 cancel 출력(stdout/stderr/rc) 동일" <(cat "$S/l.out" "$S/l.err" "$S/l.rc") <(cat "$S/r.out" "$S/r.err" "$S/r.rc")
	t_eq "a9 cancel 결과: 둘 다 jobs/done 으로 이동" "$([[ -f $c1/jobs/done/20261003-100000-bob.json && -f $c2/jobs/done/20261003-100000-bob.json && ! -e $c1/jobs/20261003-100000-bob.json && ! -e $c2/jobs/20261003-100000-bob.json ]] && echo moved)" moved
	rm -rf "$c1" "$c2"; cp -a "$A" "$c1"; cp -a "$A" "$c2"
	lcl "$c1" request manual-run "$E2E_CLOSED_JOB" ok.yml 2> "$S/l.err" | sed 's/[0-9]\{10\}_/E_/' > "$S/l.out"
	rem "$c2" request manual-run "$E2E_CLOSED_JOB" ok.yml 2> "$S/r.err" | sed 's/[0-9]\{10\}_/E_/' > "$S/r.out"
	t_same "a10 request manual-run 출력 동일(epoch 제외)" "$S/l.out" "$S/r.out"
	t_same "a10 request 파일 내용 동일" <(cat "$c1"/requests/*_manual-run.req) <(cat "$c2"/requests/*_manual-run.req)
	t_eq "a10 request 파일 내용" "$(cat "$c2"/requests/*_manual-run.req | paste -sd'|')" "jobid=$E2E_CLOSED_JOB|yml=ok.yml"
	rm -rf "$c1" "$c2"; cp -a "$A" "$c1"; cp -a "$A" "$c2"
	lcl "$c1" "done" h1.lab > "$S/l.out" 2> "$S/l.err"
	rem "$c2" "done" h1.lab > "$S/r.out" 2> "$S/r.err"
	t_same "a11 done 출력(stdout) 동일" "$S/l.out" "$S/r.out"
	t_same "a11 done 출력(stderr 경고) 동일" "$S/l.err" "$S/r.err"
	t_same "a11 done 기록 동일(epoch 제외)" <(awk '{$1="E"; print}' "$c1/done/h1.lab") <(awk '{$1="E"; print}' "$c2/done/h1.lab")
	t_has "a11 done 기록 형식 'epoch user - manual'" "$c2/done/h1.lab" '^[0-9]+ [^ ]+ - manual$'

	# 원격 거부·오류
	for a in --start --stop --restart start stop restart daemon ensure; do
		rem "$A" "$a" > "$S/r.out" 2> "$S/r.err"; echo $? > "$S/r.rc"
		t_eq "a12 원격 모드 '$a' 거부 (rc=1 + 메시지)" "$(cat "$S/r.rc")|$(grep -c '데몬은 os8_mgmt 에서만' "$S/r.err")" "1|1"
	done
	env AUTO_SETUP_DIR="$A" PATH=/usr/bin:/bin NO_COLOR=1 "$CLI" status > "$S/r.out" 2> "$S/r.err"; echo $? > "$S/r.rc"
	t_eq "a13 gossh 가 없으면 원격 실패 메시지 + rc=1" "$(cat "$S/r.rc")|$(grep -c '원격 실행 실패' "$S/r.err")" "1|1"

	# 01 전달: 로컬 queue vs os8_mgmt 원샷(스텁 gossh) → 같은 내용, 데몬이 같은 job 으로 수거
	local QA="$S/a_q_local" QB="$S/a_q_remote" fa fb hosts=(127.0.0.31 127.0.0.32 127.0.0.33)
	rm -rf "$QA" "$QB"; mkdir -p "$QA" "$QB"
	unset E2E_01_HOST
	AUTO_SETUP_DIR="$QA" e2e_run_01 "${hosts[@]}"
	t_eq "a14 01(로컬 전달) 종료코드" "$RC01" 0
	t_has "a14 01 로그: 로컬 queue 경로" "$S/out01.txt" "auto_setup 전달 : 3대 \($QA/queue/"
	E2E_01_HOST=os8.mgmt AUTO_SETUP_DIR="$QB" e2e_run_01 "${hosts[@]}"
	t_eq "a15 01(os8_mgmt 원샷 전달) 종료코드" "$RC01" 0
	t_has "a15 01 로그: auto_setup 전달 → os8.mgmt" "$S/out01.txt" 'auto_setup 전달 : 3대 → os8.mgmt'
	t_no "a15 01 경고(전달 실패) 없음" "$S/out01.txt" 'auto_setup 전달 실패'
	t_hasF "a15 01 스텁 gossh 가 원격 전달 호출을 받음" "$W01/.stublog/calls.log" "gossh-remote os8.mgmt"
	fa=$(ls "$QA"/queue/*.job 2> /dev/null | head -1); fb=$(ls "$QB"/queue/*.job 2> /dev/null | head -1)
	t_eq "a15 원격 전달 queue 파일 1개(이름 규칙)" "$(find "$QB/queue" -maxdepth 1 -name '*.job' | grep -cE "/[0-9]+_${E2E_USER}_[0-9]+\.job$")|$(find "$QB/queue" -maxdepth 1 -name '.*' -type f | wc -l)" "1|0"
	if [[ -n $fa && -n $fb ]]; then
		t_same "a15 queue 내용 동일(time 제외)" <(sed 's/^time=.*/time=T/' "$fa") <(sed 's/^time=.*/time=T/' "$fb")
	else
		bad "a15 queue 내용 동일" "queue 파일 없음 local=[$fa] remote=[$fb]"
	fi
	if [[ -n $fa && -n $fb ]]; then
		export E2E_SCEN
		PATH="$STUBPATH" env AUTO_SETUP_DIR="$QA" "$BASE" ensure
		PATH="$STUBPATH" env AUTO_SETUP_DIR="$QB" "$BASE" ensure
		if wait_for 30 hasjob "$QA" && wait_for 30 hasjob "$QB" && wait_cond 10 '[[ -z $(ls "$QA/queue" "$QB/queue" 2> /dev/null | grep -v : | grep .) ]]'; then
			ok "a16 두 데몬이 로컬/원격 전달 queue 를 모두 수거 → jobs/ 생성, queue 비움"
		else
			bad "a16 두 데몬 수거" "A=$(ls "$QA/queue" "$QA/jobs" 2> /dev/null | paste -sd' ') B=$(ls "$QB/queue" "$QB/jobs" 2> /dev/null | paste -sd' ')"
		fi
		t_same "a16 수거된 job 의 호스트·user 동일" \
			<(grep -hoE '^  "[^"]+": {|"user": "[^"]*"' "$QA"/jobs/*.json | sort) <(grep -hoE '^  "[^"]+": {|"user": "[^"]*"' "$QB"/jobs/*.json | sort)
		t_eq "a16 수거된 호스트 3대" "$(grep -c '"seen_down"' "$QB"/jobs/*.json)" 3
		kill_all
		sleep 1
	fi
	NOWV=""
}

# ============================================================
sc_b() {
	echo "== b) 완료기록(§16-4): os_check 복사본이 기록 → 데몬 인정·제외·applied 이동·중복 실행 안 함 / 오래된 기록 거부 / done 수동"
	e2e_scen b
	export PATH="$STUBPATH"
	local B="$S/b_state" NOW SUB h O1 JOBID DONEJ
	NOW=$(date +%s); SUB=$((NOW - 120))
	rm -rf "$B"; mkdir -p "$B/done"
	printf '127.0.0.3\n127.0.0.4\n' > "$E2E_SCEN/bootold_hosts"   # 아직 새 OS 아님(uptime 큼)
	# 오래된 기록 2건(데몬이 부팅 시각을 알기 전에 미리 둠): 전달 이전 / 부팅 이전
	printf '%s %s %s %s\n' $((SUB - 100)) "$E2E_USER" aaa os_check > "$B/done/127.0.0.5"
	printf '%s %s %s %s\n' $((SUB + 30)) "$E2E_USER" bbb os_check > "$B/done/127.0.0.6"
	qjob "$B" "$SUB" 127.0.0.1 127.0.0.2 127.0.0.3 127.0.0.4 127.0.0.5 127.0.0.6 127.0.0.7
	e2e_make_oc_var "$S/ocd" "$B/done" "" || return
	e2e_make_oc_var "$S/och" "" "os8.mgmt" || return

	dstart "$BASE" "$B"
	if wait_for 30 hasjob "$B"; then ok "b0 데몬이 queue 수거(7대)"; else bad "b0 queue 수거" "$(tail -3 "$B/auto_setup.log" 2> /dev/null | paste -sd'|')"; fi
	JOBID=$(basename "$(ls "$B"/jobs/*.json | head -1)" .json)
	dkill9 "$B"
	env AUTO_SETUP_DIR="$B" "$BASE" ensure   # 기동 직후 1회 준비확인 → 부팅 시각(BootAt) 기록, 새 OS 호스트는 READY
	wait_for 30 grep -q 'READY: 127.0.0.6' "$B/auto_setup.log"
	wait_for 20 grep -qF '완료기록 거부: 127.0.0.6' "$B/auto_setup.log"
	sleep 12   # 데몬 5초 주기를 두 번 넘겨도 로그가 1회인지 확인
	t_eq "b1 READY: 5대(127.0.0.3/4 는 uptime 큼 → 미READY)" "$(grep -c 'READY: ' "$B/auto_setup.log")" 5
	t_eq "b2 전달 이전 기록 거부 로그 1회(데몬 기동당)" "$(lastlog "$B" | grep -c '완료기록 거부: 127.0.0.5 .*전달 이전 기록')" 1
	t_eq "b2 부팅 이전 기록 거부 로그 1회(데몬 기동당, 12초 동안 반복 없음)" "$(lastlog "$B" | grep -c '완료기록 거부: 127.0.0.6 .*마지막 부팅 이전 기록')" 1
	t_eq "b2 거부된 기록은 done/ 에 그대로(applied 로 가지 않음)" "$(ls "$B/done" | paste -sd' ')" "127.0.0.5 127.0.0.6"

	# os_check 복사본(auto_done_dir 채움) 이 다른 경로에서 정상 종료 → 기록 생성
	sleep 2
	printf '127.0.0.2\n' > "$S/b_l2.list"
	OC_SCRIPT="$S/ocd/os_check_final_annotated.sh" e2e_run_oc "$S/b_oc2" "$S/b_l2.list"
	t_eq "b3 os_check(auto_done_dir) 종료코드 0" "$RCOC" 0
	t_has "b3 os_check 출력: 완료기록 INFO" "$S/b_oc2/out.log" "\[INFO\] auto_setup 완료기록 : 1대 → $B/done"
	if wait_for 30 grep -qF '완료기록 인정: 127.0.0.2' "$B/auto_setup.log"; then ok "b4 데몬이 완료기록 인정 (부팅 이후 기록)"; else bad "b4 완료기록 인정" "$(grep '완료기록' "$B/auto_setup.log" | tail -3 | paste -sd'|')"; fi
	t_eq "b4 인정된 기록은 done/applied/ 로 이동" "$([[ -f $B/done/applied/127.0.0.2 && ! -e $B/done/127.0.0.2 ]] && echo moved)" moved
	t_has "b4 기록 형식 'epoch user sha256 os_check'" "$B/done/applied/127.0.0.2" "^[0-9]+ $E2E_USER [0-9a-f]{64} os_check$"
	# auto_done_host 변형 (os8_mgmt 원샷 기록, 스텁 gossh 가 로컬 done/ 에 씀)
	printf '127.0.0.7\n' > "$S/b_l7.list"
	AUTO_SETUP_DIR="$B" OC_SCRIPT="$S/och/os_check_final_annotated.sh" e2e_run_oc "$S/b_oc7" "$S/b_l7.list"
	t_has "b5 os_check(auto_done_host) 출력: 완료기록 INFO → os8.mgmt" "$S/b_oc7/out.log" '\[INFO\] auto_setup 완료기록 : 1대 → os8.mgmt'
	if wait_for 30 grep -qF '완료기록 인정: 127.0.0.7' "$B/auto_setup.log"; then ok "b5 원샷 기록도 데몬이 인정"; else bad "b5 원샷 기록 인정" "$(grep '127.0.0.7' "$B/auto_setup.log" | tail -3 | paste -sd'|')"; fi
	# 수동 완료
	env AUTO_SETUP_DIR="$B" "$BASE" "done" 127.0.0.4 > "$S/b_done.out" 2> "$S/b_done.err"
	t_eq "b6 auto_setup done 종료코드" "$?" 0
	t_hasF "b6 수동 완료 경고(부팅 시각 검증 없음)" "$S/b_done.err" "부팅 시각 검증 없이"
	if wait_for 30 grep -qF '수동 완료 처리(부팅 시각 검증 없음): 127.0.0.4' "$B/auto_setup.log"; then ok "b6 데몬이 수동 완료 인정(경고 로그)"; else bad "b6 수동 완료 인정" "$(tail -3 "$B/auto_setup.log" | paste -sd'|')"; fi
	t_eq "b6 run 이 아직 시작되지 않음(127.0.0.3 미READY 로 대기 중)" "$(logn "$B" 'run 시작')" 0

	# 127.0.0.3 도 새 OS 로 → 재기동 → 남은 4대(1,3,5,6)만 run, 2·4·7 은 제외
	: > "$E2E_SCEN/bootold_hosts"
	dkill9 "$B"
	env AUTO_SETUP_DIR="$B" "$BASE" ensure
	if wait_for 90 hasdone "$B"; then ok "b7 전원 처리 → run 1회 → job 종료(jobs/done)"; else bad "b7 run/종료" "$(tail -4 "$B/auto_setup.log" | paste -sd'|')"; fi
	sleep 1
	DONEJ="$B/jobs/done/$JOBID.json"
	t_eq "b7 run 시작 1회(중복 실행 안 함)" "$(logn "$B" 'run 시작')" 1
	t_has "b7 run 대상 4대(완료기록·수동 완료 호스트 제외)" "$B/auto_setup.log" 'run 시작: job .* 1차 4대'
	O1=$(ls -d "$B"/runs/*/ | head -1)
	t_eq "b7 run 의 targets.txt = 1·3·5·6" "$(paste -sd' ' "${O1}targets.txt")" "127.0.0.1 127.0.0.3 127.0.0.5 127.0.0.6"
	t_eq "b7 runs/ 디렉터리는 1개(다른 run 없음)" "$(ls "$B/runs" | wc -l)" 1
	n4=$(grep -c '^run hosts=127.0.0.1 127.0.0.3 127.0.0.5 127.0.0.6$' "$E2E_SCEN/order.log"); n2=$(grep -c '^run hosts=127.0.0.2$' "$E2E_SCEN/order.log")
	t_eq "b7 데몬 run 의 os_check 호출은 4대 목록으로만(사전·재점검 = 외부 os_check 1대 run 과 같은 횟수, 중복 실행 없음)" "$([[ $n4 -ge 1 && $n4 -eq $n2 ]] && echo same-count)" same-count
	t_eq "b7 run.sh 호출은 [4대 목록 / 127.0.0.2 / 127.0.0.7] 뿐(2·4·7 이 데몬 run 에 섞이지 않고 127.0.0.4 는 아예 안 돎)" "$(grep '^run hosts=' "$E2E_SCEN/order.log" | grep -cvxE 'run hosts=(127\.0\.0\.1 127\.0\.0\.3 127\.0\.0\.5 127\.0\.0\.6|127\.0\.0\.2|127\.0\.0\.7)')" 0
	for h in 127.0.0.2 127.0.0.7; do
		t_eq "b8 job: $h processed=external, done_src=external" "$(jfield "$DONEJ" "$h" '"(processed|done_src)":')" '"processed":"external""done_src":"external"'
	done
	t_eq "b8 job: 127.0.0.4 processed=manual, done_src=manual" "$(jfield "$DONEJ" 127.0.0.4 '"(processed|done_src)":')" '"processed":"manual""done_src":"manual"'
	t_eq "b8 job: run 처리 4대 done_src=run" "$(grep -c '"done_src": "run"' "$DONEJ")" 4
	t_has "b8 wall: run 처리 호스트만(4대)" "$E2E_SCEN/wall.log" '^127\.0\.0\.1 127\.0\.0\.3 127\.0\.0\.5 127\.0\.0\.6 \(4대\)$'
	# 처리 후 거부된 기록은 그대로 남아 있어도 더는 로그가 늘지 않는다
	t_eq "b9 마지막 데몬: 거부 로그 2건(5·6 각 1회) — run 이 끝난 뒤에도 늘지 않음" "$(lastlog "$B" | grep -c '완료기록 거부')" 2
	kill_all
	sleep 1
	t_eq "b10 데몬 정리" "$(dcount)" 0
}

# ============================================================
sc_c() {
	echo "== c) 요청 채널·TUI: manual-run 완료 여부와 무관하게 수락(미완료 그룹은 접속불가 알림) → 데몬 수동 run → code·snapshot, 원격 request, pty TUI 스모크"
	e2e_scen c
	export PATH="$STUBPATH"
	local C="$S/c_state" J=$E2E_CLOSED_JOB CODE F N
	rm -rf "$C"; e2e_sample_closed "$C"
	dstart "$BASE" "$C"
	wait_for 10 dalive "$C"
	# 사용법 검사
	env AUTO_SETUP_DIR="$C" "$BASE" request manual-run only-one > /dev/null 2> "$S/c.err"
	t_eq "c0 인자 부족 request → 사용법, rc=1" "$?|$(grep -c '^사용법: auto_setup' "$S/c.err")" "1|1"
	# 미완료 그룹(bad.yml: 실패 호스트 포함)도 수락 → 수동 run → 응답 없는 호스트는 wall 에 접속불가로 알림
	env AUTO_SETUP_DIR="$C" "$BASE" request manual-run "$J" bad.yml > "$S/c.out" 2>&1
	t_eq "c1 request(bad.yml) 등록 rc" "$?" 0
	t_has "c1 출력: 요청 등록" "$S/c.out" '\[O\] 요청 등록: [0-9]+_manual-run\.req'
	if wait_cond 40 '[[ $(ls "$C/codes" 2> /dev/null | wc -l) -ge 1 ]]'; then ok "c1 미완료 그룹 수락 → 수동 run 실행"; else bad "c1 미완료 그룹 수동 run" "$(tail -4 "$C/auto_setup.log" | paste -sd'|')"; fi
	wait_for 20 grep -q '수동 run 완료' "$C/auto_setup.log"
	t_eq "c1 미완료 그룹은 거부되지 않음(rejected 0)" "$(ls "$C/requests/rejected" 2> /dev/null | wc -l)" 0
	t_hasF "c1 wall: 미완료 그룹 수동 run 알림(스텁은 dk3 처리됨)" "$E2E_SCEN/wall.log" "dk3 (1대)"
	t_eq "c1 requests/ 큐 비움" "$(find "$C/requests" -maxdepth 1 -name '*.req' | wc -l)" 0
	# 알 수 없는 job → 거부
	env AUTO_SETUP_DIR="$C" "$BASE" request manual-run no-such-job x.yml > /dev/null 2>&1
	wait_for 20 grep -q 'reason=job 없음' "$C"/requests/rejected/*.req
	t_has "c2 없는 job → reason=job 없음" <(cat "$C"/requests/rejected/*.req) '^reason=job 없음$'
	# 완료된 그룹(ok.yml) → 수락 → 수동 run
	env AUTO_SETUP_DIR="$C" "$BASE" request manual-run "$J" ok.yml > /dev/null 2>&1
	if wait_cond 40 '[[ $(ls "$C/codes" 2> /dev/null | wc -l) -ge 2 ]]'; then ok "c3 수락 → 데몬 수동 run 실행(스텁 os_check) → code 생성"; else bad "c3 수동 run" "$(tail -4 "$C/auto_setup.log" | paste -sd'|')"; fi
	wait_cond 20 '[[ $(grep -c "수동 run 완료" "$C/auto_setup.log") -ge 2 ]]'
	CODE=$(ls -t "$C/codes" | head -1 | sed 's/\.txt$//')
	t_hasF "c3 로그: 요청 수락·수동 run 시작·완료" "$C/auto_setup.log" "요청 수락:"
	t_has "c3 수동 run 시작 로그(그룹 ok.yml 2대)" "$C/auto_setup.log" "수동 run 시작: job $J 그룹 ok.yml 2대"
	t_eq "c3 수동 run targets = 그룹 호스트 전체" "$(paste -sd' ' "$C/runs/$CODE/targets.txt")" "dk1 dk2"
	t_hasF "c3 code 파일: 결과 리포트 머리줄" "$C/codes/$CODE.txt" "############### 결과 리포트 ###############"
	t_eq "c3 요청 파일(active)은 run 이 끝나면 삭제" "$(ls "$C/requests/active" 2> /dev/null | wc -l)" 0
	t_eq "c3 wall 1회, 호스트 가로 + 2대" "$(grep -c '^dk1 dk2 (2대)$' "$E2E_SCEN/wall.log")" 1
	t_has "c3 snapshot 반영: runs 에 manual=true, yml=ok.yml, code" <(lcl "$C" snapshot) "\"code\": \"$CODE\""
	lcl "$C" snapshot > "$S/c.snap"
	t_hasF "c3 snapshot: manual true" "$S/c.snap" '"manual": true'
	t_hasF "c3 snapshot: yml ok.yml" "$S/c.snap" '"yml": "ok.yml"'
	t_eq "c3 종료 job 의 processed 는 그대로(수동 run 은 처리 상태를 바꾸지 않음)" "$(grep -c '"processed": "4821"' "$C/jobs/done/$J.json")" 2
	lcl "$C" code "$CODE" > "$S/c.code"
	t_same "c3 auto_setup code <code> = codes 파일" "$S/c.code" "$C/codes/$CODE.txt"
	# 원격 클라이언트 모드로 같은 요청 → 두 번째 수동 run
	N=$(ls "$C/codes" | wc -l)
	rem "$C" request manual-run "$J" ok.yml > "$S/c.out" 2>&1
	t_eq "c4 원격 request rc" "$?" 0
	t_has "c4 원격 출력도 로컬과 같은 형식" "$S/c.out" '^\[O\] 요청 등록: [0-9]+_manual-run\.req$'
	if wait_cond 40 '[[ $(ls "$C/codes" | wc -l) -gt $N ]]'; then ok "c4 원격 요청 → 수동 run → code 2번째 생성"; else bad "c4 원격 요청 수동 run" "$(tail -4 "$C/auto_setup.log" | paste -sd'|')"; fi
	wait_cond 20 '[[ $(grep -c "수동 run 완료" "$C/auto_setup.log") -eq 2 ]]'
	t_eq "c4 수동 run 은 동시에 1개씩(순차) 완료 2건" "$(grep -c '수동 run 완료' "$C/auto_setup.log")" 2
	kill_all
	sleep 1
	t_eq "c5 데몬 정리" "$(dcount)" 0

	# pty TUI 스모크 (python3 pty): 화면1 → Enter(화면2) → q 로 종료
	local A="$S/c_tui" T
	N=$(date +%s)
	rm -rf "$A"; e2e_sample_state "$A" "$N"
	if ! command -v python3 > /dev/null 2>&1; then
		skip "c6 pty TUI 스모크 (python3 없음)"
		return
	fi
	cat > "$S/tui_smoke.py" <<'PY'
import fcntl, os, pty, re, select, struct, sys, termios, time
binp, d, now = sys.argv[1:4]
pid, fd = pty.fork()
if pid == 0:
    fcntl.ioctl(0, termios.TIOCSWINSZ, struct.pack("HHHH", 40, 120, 0, 0))
    env = dict(os.environ, TERM="xterm", AUTO_SETUP_DIR=d, AUTO_SETUP_NOW=now)
    env.pop("NO_COLOR", None)
    os.execve(binp, [binp], env)
buf = b""
def pump(sec):
    global buf
    end = time.time() + sec
    while time.time() < end:
        r, _, _ = select.select([fd], [], [], 0.2)
        if r:
            try:
                x = os.read(fd, 65536)
            except OSError:
                return
            if not x:
                return
            buf += x
def plain():
    return re.sub(rb"\x1b\[[0-9;?]*[A-Za-z]", b"", buf).decode("utf-8", "replace")
pump(2.0)
s1 = plain()
print("S1_GROUPS", "gpu.yml" in s1 and "cpu.yml" in s1)
print("S1_COLOR", b"\x1b[" in buf)
buf = b""
os.write(fd, b"\r")
pump(1.5)
s2 = plain()
print("S2_HOSTS", ("g1" in s2 and "g2" in s2) or ("c1" in s2 and "c2" in s2))
exited = None
for _ in range(4):
    try:
        os.write(fd, b"q")
    except OSError:
        pass
    pump(1.0)
    for _w in range(10):
        try:
            p, st = os.waitpid(pid, os.WNOHANG)
        except ChildProcessError:
            exited = 0
            break
        if p:
            exited = os.WEXITSTATUS(st) if os.WIFEXITED(st) else 99
            break
        time.sleep(0.1)
    if exited is not None:
        break
print("EXIT_RC", exited)
if exited is None:
    os.kill(pid, 9)
PY
	T=$(env PATH="$ORIG_PATH" timeout 40 python3 "$S/tui_smoke.py" "$BASE" "$A" "$N" 2> "$S/tui.err")
	t_eq "c6 TUI 화면1: 그룹 행(gpu.yml/cpu.yml) 표시" "$(grep '^S1_GROUPS' <<< "$T")" "S1_GROUPS True"
	t_eq "c6 TUI 색(ANSI) 사용" "$(grep '^S1_COLOR' <<< "$T")" "S1_COLOR True"
	t_eq "c6 TUI Enter → 화면2: 호스트 표 표시" "$(grep '^S2_HOSTS' <<< "$T")" "S2_HOSTS True"
	t_eq "c6 TUI q 로 종료(rc=0)" "$(grep '^EXIT_RC' <<< "$T")" "EXIT_RC 0"
}

# ============================================================
sc_d() {
	echo "== d) 데몬 제어: --start(중복 안내)/--restart(pid 변경)/--stop(pid 파일 정리), 원격 모드 거부"
	e2e_scen d
	local D="$S/d_state" p1 p2 out
	rm -rf "$D"; mkdir -p "$D"
	export PATH="$STUBPATH"
	out=$(env AUTO_SETUP_DIR="$D" "$BASE" --start 2>&1); echo "$?" > "$S/d.rc"
	p1=$(dpid "$D")
	t_eq "d1 --start 종료코드 0" "$(cat "$S/d.rc")" 0
	t_has "d1 --start 출력: 데몬 기동 (pid N)" <(echo "$out") "^\[O\] 데몬 기동 \(pid $p1\)$"
	t_eq "d1 데몬 1개 · pid 파일의 프로세스 생존" "$(dcount)|$(dalive "$D" && echo alive)" "1|alive"
	t_eq "d1 pid 파일 형식 '<pid> <기동 epoch>'" "$(grep -cE '^[0-9]+ [0-9]+$' "$D/auto_setup.pid")" 1
	lcl "$D" snapshot > "$S/d.snap"
	t_hasF "d1 snapshot: daemon.running true" "$S/d.snap" '"running": true'
	out=$(env AUTO_SETUP_DIR="$D" "$BASE" --start 2>&1); echo "$?" > "$S/d.rc"
	t_eq "d2 --start 중복: 안내 + rc 0 + pid 불변" "$(cat "$S/d.rc")|$(grep -c "이미 실행 중 (pid $p1)" <<< "$out")|$(dpid "$D")|$(dcount)" "0|1|$p1|1"
	env AUTO_SETUP_DIR="$D" "$BASE" ensure
	t_eq "d3 ensure 와 공존: 데몬 여전히 1개" "$(dcount)" 1
	out=$(env AUTO_SETUP_DIR="$D" "$BASE" --restart 2>&1); echo "$?" > "$S/d.rc"
	p2=$(dpid "$D")
	t_eq "d4 --restart rc 0 · pid 변경 · 데몬 1개" "$(cat "$S/d.rc")|$([[ -n $p2 && $p2 != "$p1" ]] && echo changed)|$(dcount)" "0|changed|1"
	t_has "d4 --restart 출력: 종료 후 기동" <(echo "$out") "데몬 종료 \(pid $p1\)"
	t_hasF "d4 로그: stop/start 기록" "$D/auto_setup.log" "stop: 데몬 종료 pid=$p1"
	out=$(env AUTO_SETUP_DIR="$D" "$BASE" stop 2>&1); echo "$?" > "$S/d.rc"
	t_eq "d5 stop(단어형) rc 0 · 프로세스 종료 · pid 파일 정리" "$(cat "$S/d.rc")|$(dcount)|$([[ -e $D/auto_setup.pid ]] && echo remains || echo clean)" "0|0|clean"
	t_has "d5 stop 출력: 데몬 종료 (pid N)" <(echo "$out") "데몬 종료 \(pid $p2\)"
	t_hasF "d5 데몬 종료 로그" "$D/auto_setup.log" "daemon 종료 pid=$p2"
	out=$(env AUTO_SETUP_DIR="$D" "$BASE" --stop 2>&1); echo "$?" > "$S/d.rc"
	t_eq "d6 이미 중지 상태에서 --stop: 안내 + rc 0" "$(cat "$S/d.rc")|$(grep -c '데몬이 실행 중이 아닙니다' <<< "$out")" "0|1"
	lcl "$D" snapshot > "$S/d.snap"
	t_hasF "d6 snapshot: daemon.running false" "$S/d.snap" '"running": false'
	out=$(env AUTO_SETUP_DIR="$D" "$BASE" restart 2>&1); echo "$?" > "$S/d.rc"
	t_eq "d7 중지 상태에서 restart(단어형) → 기동만(rc 0, 데몬 1개)" "$(cat "$S/d.rc")|$(dcount)" "0|1"
	env AUTO_SETUP_DIR="$D" "$BASE" --stop > /dev/null 2>&1
	t_eq "d7 정리 후 데몬 0 · pid 파일 없음" "$(dcount)|$([[ -e $D/auto_setup.pid ]] && echo remains || echo clean)" "0|clean"
	env AUTO_SETUP_DIR="$D" "$BASE" --start extra > /dev/null 2> "$S/d.err"
	t_eq "d8 잘못된 인자(--start extra) → 사용법, rc 1, 데몬 안 뜸" "$?|$(grep -c '^사용법: auto_setup' "$S/d.err")|$(dcount)" "1|1|0"
	# 원격 모드 거부
	local a
	for a in --start --stop --restart; do
		env AUTO_SETUP_DIR="$D" PATH="$S/binr:$ORIG_PATH" "$CLI" "$a" > /dev/null 2> "$S/d.err"; echo $? > "$S/d.rc"
		t_eq "d9 원격 모드 $a 거부 (rc 1, 데몬 안 뜸, pid 파일 없음)" "$(cat "$S/d.rc")|$(grep -c '\[X\] 데몬은 os8_mgmt 에서만' "$S/d.err")|$(dcount)|$([[ -e $D/auto_setup.pid ]] && echo remains || echo clean)" "1|1|0|clean"
	done
}

# ============================================================
sc_e() {
	echo "== e) LDAP: 전달 시점 백업(0700/0600) → READY 후 binddn uid same(생략)/diff(공유경로 복원)/변수 비면 수동 복원 필요 → run, 호출 순서·비밀 문자열 미노출"
	e2e_scen e
	export PATH="$STUBPATH"
	local E="$S/e_state" E2="$S/e2_state" R="$S/ldaproot" SHR="$S/auto_setup_share" SHD="$S/ldapshare"
	local NOW SUB JOBID h pw1="OLDPW_Zq81xV" pw2="OLDPW_k39Lm2" pwn="NEWPW_q7Tt0w" pws="NEWPW_s11same" f CODE
	local DONEJ BAK
	NOW=$(date +%s); SUB=$((NOW - 120))
	rm -rf "$E" "$E2" "$R" "$SHD"; mkdir -p "$E" "$SHD"
	export E2E_LDAP_ROOT="$R" E2E_SHARE_DIR="$SHD"
	mk_tree() { # 호스트 binddn-uid bindpw resolv-번호 : 가짜 루트 파일 세트 (RHEL8 → sssd + chrony)
		local d="$R/$1"
		mkdir -p "$d/etc/openldap" "$d/etc/sssd"
		echo "Rocky Linux release 8.9 (Green Obsidian)" > "$d/etc/redhat-release"
		printf 'URI ldap://ldap.lab\nBASE dc=lab\nbinddn uid=%s,ou=user,dc=lab\nbindpw %s\n' "$2" "$3" > "$d/etc/openldap/ldap.conf"
		printf 'nameserver 10.0.0.%s\n' "$4" > "$d/etc/resolv.conf"
		printf '[sssd]\nservices = nss\n' > "$d/etc/sssd/sssd.conf"; chmod 600 "$d/etc/sssd/sssd.conf"
		printf 'server ntp.lab\n' > "$d/etc/chrony.conf"
	}
	mk_tree 127.0.0.11 svc11 "$pw1" 1
	mk_tree 127.0.0.12 svc12 "$pw2" 2
	mkdir -p "$R/127.0.0.13/etc"; echo "Rocky Linux release 8.9" > "$R/127.0.0.13/etc/redhat-release"   # ldap.conf 없음 → 백업 없음
	qjob "$E" "$SUB" 127.0.0.11 127.0.0.12 127.0.0.13
	env AUTO_SETUP_DIR="$E" "$SHR" ensure
	wait_for 30 hasjob "$E"
	JOBID=$(basename "$(ls "$E"/jobs/*.json | head -1)" .json)
	if wait_cond 40 '[[ $(grep -c "\"backup\"" "$E"/jobs/*.json) -ge 3 ]]'; then ok "e0 전달 시점 LDAP 백업 수집(3대 판정)"; else bad "e0 백업 수집" "$(tail -4 "$E/auto_setup.log" | paste -sd'|')"; fi
	t_has "e1 로그: LDAP 백업 3대 중 2대" "$E/auto_setup.log" "LDAP 백업: job $JOBID 3대 중 2대"
	t_eq "e1 호스트별 ldap 상태: 11·12 ok, 13 none" "$(grep -oE '"backup": "[a-z]+"' "$E"/jobs/*.json | sort | uniq -c | awk '{print $1 $3 $4}' | paste -sd' ')" '1"none" 2"ok"'
	BAK="$E/ldapbak/$JOBID"
	t_eq "e2 백업 디렉터리 권한: ldapbak·job·호스트 0700" "$(stat -c %a "$E/ldapbak" "$BAK" "$BAK/127.0.0.12" | paste -sd' ')" "700 700 700"
	t_eq "e2 백업 파일 권한 0600 전부" "$(stat -c %a "$BAK/127.0.0.12"/* | sort -u | paste -sd' ')" "600"
	t_eq "e2 백업 파일 세트: ldap/resolv/sssd/chrony + meta" "$(ls "$BAK/127.0.0.12" | paste -sd' ')" "chrony.conf ldap.conf meta.json resolv.conf sssd.conf"
	t_hasF "e2 백업 파일에는 원본 설정이 들어 있음(0600)" "$BAK/127.0.0.12/ldap.conf" "bindpw $pw2"
	t_eq "e2 백업 없음 호스트(13)는 디렉터리 없음" "$([[ -e $BAK/127.0.0.13 ]] && echo exists || echo none)" none
	t_eq "e2 백업 전 비교·복원 호출 없음" "$(grep -cE 'ldap-(probe|restore)' "$E2E_SCEN/order.log")" 0

	# 재설치 흉내: 12 번의 새 OS 는 binddn uid·bindpw·설정이 달라짐, 11 번은 binddn uid 가 같음(bindpw 는 달라도 같다고 봐야 함)
	# → 재기동(기동 직후 1회 준비확인 → READY)
	dkill9 "$E"
	mk_tree 127.0.0.11 svc11 "$pws" 1
	mk_tree 127.0.0.12 svcnew12 "$pwn" 9
	printf '[new]\n' > "$R/127.0.0.12/etc/sssd/sssd.conf"
	env AUTO_SETUP_DIR="$E" "$SHR" ensure
	if wait_for 90 hasdone "$E"; then ok "e3 READY → LDAP 확인 → run → job 종료"; else bad "e3 LDAP→run 흐름" "$(tail -6 "$E/auto_setup.log" | paste -sd'|')"; fi
	sleep 1
	DONEJ="$E/jobs/done/$JOBID.json"
	# 호출 순서: collect(전달 시점) → probe(11,12) → restore(12) → run
	f="$S/e_order.txt"; awk '{print $1}' "$E2E_SCEN/order.log" | uniq > "$f"
	t_eq "e4 호출 순서: collect → probe → restore → run" "$(paste -sd' ' "$f")" "ldap-collect ldap-probe ldap-restore run"
	t_eq "e4 probe 는 11·12 두 대(13 은 백업 없음 → 생략)" "$(grep '^ldap-probe' "$E2E_SCEN/order.log" | awk '{print $2}' | sort | paste -sd' ')" "127.0.0.11 127.0.0.12"
	t_eq "e4 restore 는 12(diff) 에만 1회, 11(same) 은 적용 생략" "$(grep '^ldap-restore' "$E2E_SCEN/order.log" | paste -sd' ')" "ldap-restore 127.0.0.12"
	t_has "e4 run 호출 시점에 LDAP 단계 완료(run 대상 3대)" "$E2E_SCEN/order.log" '^run hosts=127.0.0.11 127.0.0.12 127.0.0.13$'
	t_eq "e5 11: binddn 동일(same) / 적용 안 함" "$(jfield "$DONEJ" 127.0.0.11 '"(bindpw|applied)":')" '"bindpw":"same""applied":false'
	t_eq "e5 12: binddn 상이(diff) / 적용 완료" "$(jfield "$DONEJ" 127.0.0.12 '"(bindpw|applied)":')" '"bindpw":"diff""applied":true'
	t_hasF "e5 로그: 12 LDAP 복원" "$E/auto_setup.log" "LDAP 확인: 127.0.0.12 (job $JOBID) binddn=diff applied=true"
	t_hasF "e5 로그: 11 동일(생략)" "$E/auto_setup.log" "LDAP 확인: 127.0.0.11 (job $JOBID) binddn=same applied=false"
	t_eq "e6 복원 결과: 12 의 ldap.conf 가 백업본으로 돌아옴(binddn uid·bindpw)" "$(grep -c "^bindpw $pw2\$" "$R/127.0.0.12/etc/openldap/ldap.conf")|$(grep -c "$pwn" "$R/127.0.0.12/etc/openldap/ldap.conf")|$(grep -c '^binddn uid=svc12,' "$R/127.0.0.12/etc/openldap/ldap.conf")" "1|0|1"
	t_eq "e6 복원 파일 권한 유지(sssd.conf 600)" "$(stat -c %a "$R/127.0.0.12/etc/sssd/sssd.conf")" 600
	t_eq "e6 복원 시 서비스 재시작: sssd·chronyd" "$(grep '^restart' "$R/127.0.0.12/restart.log" | sort | paste -sd' ')" "restart chronyd restart sssd"
	t_hasF "e6 restorecon 호출" "$R/127.0.0.12/restart.log" "restorecon /etc/openldap/ldap.conf"
	t_eq "e6 same 호스트(11)는 건드리지 않음(restart.log 없음, bindpw 는 새 OS 값 그대로)" "$([[ -e $R/127.0.0.11/restart.log ]] && echo touched || echo untouched)|$(grep -c "$pws" "$R/127.0.0.11/etc/openldap/ldap.conf")" "untouched|1"
	# 공유경로: 복원 호출 시점에 디렉터리 0700·파일 0600(4개), 끝난 뒤 흔적 없음
	t_eq "e6s 공유경로 관찰: 디렉터리 2개 700 · 파일 4개 600" "$(awk '{ n = gsub(/\//, "/", $2); if (n == 2) { fl++; if ($1 != 600) b++ } else { d++; if ($1 != 700) b++ } } END { print d + 0 "|" fl + 0 "|" b + 0 }' "$E2E_SCEN/share.stat")" "2|4|0"
	t_eq "e6s 복원 뒤 공유경로 비어 있음(임시 디렉터리 즉시 삭제)" "$(find "$SHD" -mindepth 1 | wc -l)" 0
	CODE=$(ls "$E/codes" | head -1 | sed 's/\.txt$//')

	# 비밀 문자열 미노출: 로그·codes·runs·job JSON·wall·CLI 출력·gossh 호출 기록 어디에도 없어야 한다
	lcl "$E" status > "$S/e.status" 2>&1
	lcl "$E" snapshot > "$S/e.snap" 2>&1
	lcl "$E" --plain > "$S/e.plain" 2>&1
	lcl "$E" code "$CODE" > "$S/e.code" 2>&1
	local leaks=0 x
	for x in "$E/auto_setup.log" "$S/e.status" "$S/e.snap" "$S/e.plain" "$S/e.code" "$E2E_SCEN/wall.log" "$E2E_SCEN/gossh.log" "$E2E_SCEN/order.log"; do
		if grep -qE 'OLDPW_|NEWPW_' "$x" 2> /dev/null; then leaks=$((leaks + 1)); bad "e7 비밀 문자열 노출: $x"; fi
	done
	if grep -rlE 'OLDPW_|NEWPW_' "$E/codes" "$E/runs" "$E/jobs" "$E/queue" "$E/requests" "$E/done" 2> /dev/null | grep -q .; then
		leaks=$((leaks + 1)); bad "e7 비밀 문자열 노출(codes/runs/jobs)" "$(grep -rlE 'OLDPW_|NEWPW_' "$E/codes" "$E/runs" "$E/jobs" 2> /dev/null | head -3 | paste -sd' ')"
	fi
	[[ $leaks -eq 0 ]] && ok "e7 비밀 문자열(bindpw 값)이 로그·codes·runs·jobs·wall·status·snapshot·리포트·code·gossh 기록 어디에도 없음"
	# binddn uid 값도 기본 로그에는 남기지 않는다(호스트별 same/diff 만)
	t_no "e7 기본 로그에 binddn uid 값 없음" "$E/auto_setup.log" 'svc11|svc12|svcnew12'
	# gossh 호출 기록에는 gossh 인자(명령줄)가 전부 있다: 파일 내용·bindpw·bindpw 단어가 없고 공유경로의 base64 본문도 없다
	t_no "e7 gossh 명령줄 기록에 bindpw 단어·파일 내용 없음" "$E2E_SCEN/gossh.log" 'bindpw|BINDPW|authtok'
	t_eq "e7 (대조) 백업 파일(0600) 에는 원본이 있어 grep 이 동작함" "$(grep -rlE 'OLDPW_' "$E/ldapbak" | wc -l)" 2

	# ---- 변수 ldap_share_dir 가 비어 있는 빌드(BASE): diff 호스트는 복원하지 않고 "수동 복원 필요" 표시 ----
	: > "$E2E_SCEN/order.log"; : > "$E2E_SCEN/gossh.log"; : > "$E2E_SCEN/share.stat"
	rm -rf "$R/127.0.0.12/restart.log" "$R/127.0.0.11/restart.log"
	mk_tree 127.0.0.11 svc11 "$pw1" 1
	mk_tree 127.0.0.12 svc12 "$pw2" 2
	NOW=$(date +%s); SUB=$((NOW - 120))
	mkdir -p "$E2"
	qjob "$E2" "$SUB" 127.0.0.11 127.0.0.12
	env AUTO_SETUP_DIR="$E2" "$BASE" ensure
	wait_for 30 hasjob "$E2"
	JOBID=$(basename "$(ls "$E2"/jobs/*.json | head -1)" .json)
	if wait_cond 40 '[[ $(grep -c "\"backup\"" "$E2"/jobs/*.json) -ge 2 ]]'; then ok "e8 (변수 비움) 전달 시점 LDAP 백업 수집"; else bad "e8 백업 수집" "$(tail -4 "$E2/auto_setup.log" | paste -sd'|')"; fi
	dkill9 "$E2"
	mk_tree 127.0.0.12 svcnew12 "$pwn" 9
	env AUTO_SETUP_DIR="$E2" "$BASE" ensure
	if wait_for 90 hasdone "$E2"; then ok "e8 (변수 비움) READY → LDAP 확인 → run → job 종료"; else bad "e8 흐름" "$(tail -6 "$E2/auto_setup.log" | paste -sd'|')"; fi
	sleep 1
	DONEJ="$E2/jobs/done/$JOBID.json"
	t_eq "e8 12: diff / 적용 안 함(applied=false)" "$(jfield "$DONEJ" 127.0.0.12 '"(bindpw|applied)":')" '"bindpw":"diff""applied":false'
	t_hasF "e8 12: 사유 = 수동 복원 필요(ldap_share_dir 미설정) + 백업 경로" "$DONEJ" "수동 복원 필요(ldap_share_dir 미설정, 백업: $E2/ldapbak/$JOBID/127.0.0.12)"
	t_hasF "e8 로그: 12 수동 복원 필요" "$E2/auto_setup.log" "LDAP 확인: 127.0.0.12 (job $JOBID) binddn=diff applied=false (수동 복원 필요(ldap_share_dir 미설정"
	t_eq "e8 복원 호출 없음: probe 만 (restore 0회)" "$(grep -c '^ldap-restore' "$E2E_SCEN/order.log")|$(grep -c '^ldap-probe' "$E2E_SCEN/order.log")" "0|2"
	t_eq "e8 12 의 ldap.conf 는 새 OS 값 그대로(복원 안 함)" "$(grep -c "$pwn" "$R/127.0.0.12/etc/openldap/ldap.conf")|$([[ -e $R/127.0.0.12/restart.log ]] && echo touched || echo untouched)" "1|untouched"
	lcl "$E2" --plain > "$S/e2.plain" 2>&1
	lcl "$E2" snapshot > "$S/e2.snap" 2>&1
	t_hasF "e8 snapshot 비고에 LDAP 수동 복원 필요" "$S/e2.snap" "LDAP 수동 복원 필요"
	t_eq "e8 공유경로는 쓰지 않음(빈 채)" "$(find "$SHD" -mindepth 1 | wc -l)" 0
	leaks=0
	for x in "$E2/auto_setup.log" "$S/e2.plain" "$S/e2.snap" "$DONEJ" "$E2E_SCEN/gossh.log" "$E2E_SCEN/order.log"; do
		if grep -qE 'OLDPW_|NEWPW_' "$x" 2> /dev/null; then leaks=$((leaks + 1)); bad "e8 비밀 문자열 노출: $x"; fi
	done
	[[ $leaks -eq 0 ]] && ok "e8 (변수 비움) 비밀 문자열이 로그·job·snapshot·plain·gossh 기록에 없음"
	unset E2E_LDAP_ROOT E2E_SHARE_DIR
	kill_all
	sleep 1
	t_eq "e9 데몬 정리" "$(dcount)" 0
}

# ============================================================
# 2차 체크 공통 시나리오: 바이너리 $1, 상태 디렉터리 $2 → 데몬 기동·run 까지. CODE 를 FCODE 로 설정
f_flow() {
	local bin=$1 F=$2 NO6=${3:-} SUB NOW H6=192.0.2.1 N6=1
	NOW=$(date +%s); SUB=$((NOW - 120))
	rm -rf "$F"; mkdir -p "$F"
	printf '127.0.0.22\n127.0.0.24\n192.0.2.1\n' > "$E2E_SCEN/fail_hosts"
	printf '127.0.0.23\n' > "$E2E_SCEN/refused_hosts"
	printf '127.0.0.24\n' > "$E2E_SCEN/fail2_hosts"
	[[ -n $NO6 ]] && { H6=; N6=0; }
	qjob "$F" "$SUB" 127.0.0.21 127.0.0.22 127.0.0.23 127.0.0.24 $H6
	env AUTO_SETUP_DIR="$F" "$bin" ensure
	wait_for 30 hasjob "$F"
	wait_for 30 grep -q "경로 판별: local 4대, os6 $N6대" "$F/auto_setup.log"
	dkill9 "$F"
	env AUTO_SETUP_DIR="$F" "$bin" ensure
	wait_for 150 grep -q "run 완료: job" "$F/auto_setup.log"
	sleep 2
	sleep 1
	FCODE=$(ls "$F/codes" 2> /dev/null | head -1 | sed 's/\.txt$//')
}

sc_f() {
	echo "== f) 2차 체크(§14-6): os6_mgmt/os6_os_check_sh 채운 빌드 + 스텁 ssh → 1차 FAIL·접속불가(route=local)만 2차, code 병합 / 변수 비면 1차와 동일"
	export PATH="$STUBPATH"
	local F1="$S/f_state_2nd" F2="$S/f_state_base" C1 C2 FCODE
	e2e_scen f2nd
	f_flow "$SEC" "$F1"
	C1=$F1/codes/$FCODE.txt
	if [[ -n $FCODE ]]; then ok "f1 1차+2차 흐름 완료 (code $FCODE)"; else bad "f1 흐름" "$(tail -5 "$F1/auto_setup.log" | paste -sd'|')"; return; fi
	t_hasF "f1 로그: 2차 체크 시작 3대(127.0.0.22/23/24)" "$F1/auto_setup.log" "2차 체크 시작: job "
	t_has "f1 2차 대상 = route=local 의 FAIL·접속불가 3대 (OK 127.0.0.21·os6 경유 192.0.2.1 제외)" "$F1/auto_setup.log" '2차 체크 시작: job [^ ]+ 3대 \(127\.0\.0\.22 127\.0\.0\.23 127\.0\.0\.24\)'
	t_has "f1 로그: 2차 체크 완료 OK 2대" "$F1/auto_setup.log" '2차 체크 완료: job [^ ]+ 3대 중 OK 2대'
	t_eq "f2 code: 2차 섹션 머리줄 1개" "$(grep -c '^### 2차 체크 (os6_mgmt)$' "$C1")" 1
	t_hasF "f2 code: 2차 대상 줄" "$C1" "대상 : 127.0.0.22 127.0.0.23 127.0.0.24 (3대)"
	t_hasF "f2 code: 2차 설정체크 FAIL 줄(24)" "$C1" "127.0.0.24: FAIL selinux enforcing"
	EXP="최종 판정 (1차 OK 또는 2차 OK)
FAIL : 127.0.0.24 192.0.2.1 (2대)
NO FAIL : 127.0.0.21 127.0.0.22 127.0.0.23 (3대)"
	t_eq "f2 code: 최종 판정(FAIL / NO FAIL 병합)" "$(sed -n '/^최종 판정/,/^$/p' "$C1" | sed '/^$/d')" "$EXP"
	t_eq "f2 code 구성: 1차 블록 뒤에 2차 섹션 → 마지막 줄은 2차 원본 경로" "$(tail -1 "$C1")" "원본 : $F1/runs/$FCODE/second/os_check.log"
	t_eq "f2 1차 원본 줄은 2차 섹션 앞에 그대로" "$(grep -c "^원본 : $F1/runs/$FCODE/os_check.log\$" "$C1")" 1
	t_eq "f3 runs/<code>/second/ 에 targets·log·결과 회수" "$(ls "$F1/runs/$FCODE/second" | paste -sd' ')" "check.res_${E2E_USER} check.res_${E2E_USER}_info check.res_${E2E_USER}_kernel check.res_${E2E_USER}_postapply os_check.log targets.txt"
	t_eq "f3 2차 targets.txt" "$(paste -sd' ' "$F1/runs/$FCODE/second/targets.txt")" "127.0.0.22 127.0.0.23 127.0.0.24"
	t_has "f3 ssh 호출: os6_mgmt 로 목록 전달 + os_check -auto (2차 체크)" "$E2E_SCEN/ssh.log" "^ssh \[-o\] \[BatchMode=yes\] \[-o\] \[ConnectTimeout=10\] \[$E2E_MGMT\] \[d=.*-auto '$E2E_USER' targets.txt"
	DONEJ=$(ls "$F1"/jobs/done/*.json 2> /dev/null | head -1)
	t_eq "f4 job 호스트의 2차 상태: 22·23 ok, 24 fail" "$(grep -oE '"second": "[a-z]+"' "$DONEJ" | sort | uniq -c | awk '{print $1 $3 $4}' | paste -sd' ')" '1"fail" 2"ok"'
	t_eq "f4 최종 처리: 5대 모두 processed(1차 OK 또는 2차 판정) → job 종료" "$(grep -c '"processed": "[0-9]\{4\}"' "$DONEJ")" 5
	t_eq "f4 wall 1회" "$(grep -c '^=====WALL=====$' "$E2E_SCEN/wall.log")" 1

	# os6_mgmt 가 비면(os6 경유 호스트 없음, os6OSCheckPath()="") 2차 체크 없음. (os6_mgmt 만 채우면 autofs 폴백으로 2차 활성 — f7)
	e2e_scen fbase
	f_flow "$S/auto_setup_no6" "$F2" no6
	C2=$F2/codes/$FCODE.txt
	if [[ -n $FCODE ]]; then ok "f5 변수 비운 빌드 흐름 완료 (code $FCODE)"; else bad "f5 흐름" "$(tail -5 "$F2/auto_setup.log" | paste -sd'|')"; return; fi
	t_eq "f5 code 에 2차 섹션 없음" "$(grep -c '2차 체크' "$C2")" 0
	t_eq "f5 2차용 ssh 호출 없음" "$(cat "$E2E_SCEN/ssh.log" 2> /dev/null | grep -c -- " -auto ")" 0
	t_no "f5 로그에 2차 체크 없음" "$F2/auto_setup.log" '2차 체크'
	t_eq "f5 2차 상태 필드 없음" "$(cat "$F2"/jobs/*.json "$F2"/jobs/done/*.json 2> /dev/null | grep -c "\"second\"")" 0
	kill_all
	sleep 1
	t_eq "f6 데몬 정리" "$(dcount)" 0

	# autofs 동일 경로: os6_mgmt 만 채우고 os6_os_check_sh 는 비움 → os_check_sh 로 폴백, awx_dir → dhcp.sh 가 awx 경로에서 링크
	local F3="$S/f_state_autofs" C3
	e2e_scen fautofs
	f_flow "$S/auto_setup_autofs" "$F3"
	C3=$F3/codes/$FCODE.txt
	if [[ -n $FCODE ]]; then ok "f7 autofs 빌드 흐름 완료 (code $FCODE)"; else bad "f7 흐름" "$(tail -5 "$F3/auto_setup.log" | paste -sd'|')"; return; fi
	t_has "f7 2차 체크 완료 (os6_os_check_sh 비어도 os_check_sh 로 폴백)" "$F3/auto_setup.log" '2차 체크 완료: job [^ ]+ 3대 중 OK 2대'
	t_eq "f7 code: 2차 섹션 있음" "$(grep -c '^### 2차 체크 (os6_mgmt)$' "$C3")" 1
	t_has "f7 ssh 명령에 os_check_sh 경로(autofs 동일 경로)" "$E2E_SCEN/ssh.log" "bash '$OC/os_check_final_annotated.sh' -auto"
	t_hasF "f7 ssh 명령에 awx dhcp 후보(awxkit/dhcp.sh)" "$E2E_SCEN/ssh.log" "$S/awx/awxkit/dhcp.sh"
	t_hasF "f7 ssh 명령에 awx dhcp 후보(awx_dir/dhcp.sh)" "$E2E_SCEN/ssh.log" "$S/awx/dhcp.sh"
	t_hasF "f7 ssh 명령에 os_check 옆 dhcp.sh 후보" "$E2E_SCEN/ssh.log" "$OC/dhcp.sh"
	t_eq "f7 로컬 run 의 dhcp.sh 링크 = awx 경로" "$(readlink "$F3/runs/$FCODE/dhcp.sh")" "$S/awx/awxkit/dhcp.sh"
	kill_all
	sleep 1
	t_eq "f8 데몬 정리" "$(dcount)" 0
}

want a && sc_a
want b && sc_b
want c && sc_c
want d && sc_d
want e && sc_e
want f && sc_f

kill_all
sleep 1
echo "== 정리 확인"
t_eq "종료 후 데몬 0개" "$(dcount)" 0

echo
echo "================ E2E2 결과: PASS=$PASS_N FAIL=$FAIL_N SKIP=$SKIP_N ================"
if [[ $FAIL_N -ne 0 ]]; then
	printf ' - %s\n' "${FAILS[@]}"
	exit 1
fi
exit 0
