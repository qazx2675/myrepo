#!/bin/bash
# manual_check.sh - auto_setup 단계별 대화형 점검 (설명 출력 → 실행/안내 → "확인할 것" → Enter 대기)
#
# 사용: bash test/manual_check.sh [-y] [-s N] [--install] [PING_UP [PING_DOWN]]
#   -y         Enter 대기 없이 연속 실행 (wall/설치처럼 사용자 확인이 필요한 단계는 여전히 묻고, 답이 없으면 건너뜀)
#   -s N       N 단계부터 시작 (1~17)
#   --install  9단계(설치 점검: setup.sh 실행)를 수행 (그래도 실행 전 yes 확인을 받음)
#   PING_UP / PING_DOWN : 4단계 실제 ICMP 점검에 쓸 "항상 응답하는 / 응답하지 않는" 주소.
#                         인자 또는 환경변수로 지정. 기본 127.0.0.1(up) / 192.0.2.1(down, TEST-NET)
# 안전: 기본 AUTO_SETUP_DIR=/tmp/auto_setup_manual (운영 /tmp/auto_setup 과 분리), 바이너리는 프로젝트의 ./auto_setup.
#       /usr/local/bin · cron · tmpfiles 는 --install 일 때만, 확인(yes) 후에만 건드린다.
# 단계: 1 빌드·vet·gofmt·단위테스트  2 CLI  3 ensure·kill 후 재기동  4 실제 ICMP  5 01→queue→jobs
#       6 os_check -auto  7 목업 전체 흐름(run_e2e.sh)  8 wall 수신(선택)  9 설치 점검(--install)  10 정리
#       (2차) 11 상태 TUI·--plain 리포트·요청  12 데몬 --start/--restart/--stop  13 양방향 동일성(원격 클라이언트·스텁 gossh)
#             14 완료기록  15 LDAP 백업/복원(스텁)  16 2차 체크  17 정리

ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd -P)
# shellcheck source=lib_e2e.sh
. "$ROOT/test/lib_e2e.sh"

[[ -d /usr/local/go/bin ]] && export PATH="$PATH:/usr/local/go/bin"

YES=""; START=1; INSTALL=""
while [[ $# -gt 0 ]]; do
	case $1 in
		-y) YES=1; shift ;;
		-s) START=$2; shift 2 ;;
		--install) INSTALL=1; shift ;;
		-h|--help) sed -n '2,17p' "${BASH_SOURCE[0]}" | sed 's/^# \{0,1\}//'; exit 0 ;;
		*) break ;;
	esac
done
if ! [[ $START =~ ^[0-9]+$ ]] || [[ $START -lt 1 || $START -gt 17 ]]; then
	echo "[X] -s 는 1~17 사이 숫자여야 합니다"; exit 1
fi
PING_UP=${1:-${PING_UP:-127.0.0.1}}
PING_DOWN=${2:-${PING_DOWN:-192.0.2.1}}

export AUTO_SETUP_DIR="${AUTO_SETUP_DIR:-/tmp/auto_setup_manual}"
AS="$AUTO_SETUP_DIR"
if [[ $AS == /tmp/auto_setup || $AS == /tmp/auto_setup/ ]]; then
	echo "[X] 운영 경로(/tmp/auto_setup) 는 점검에 쓸 수 없습니다. AUTO_SETUP_DIR 을 다른 값으로 두세요."
	exit 1
fi
BIN_AS="$ROOT/auto_setup"
S=""

if [[ -t 1 ]]; then C_G=$'\033[32m'; C_R=$'\033[31m'; C_Y=$'\033[33m'; C_B=$'\033[1m'; C_0=$'\033[0m'; else C_G=""; C_R=""; C_Y=""; C_B=""; C_0=""; fi

RES_N=(); RES_S=(); RES_M=()
FAIL_COUNT=0

summary() {
	local i tag
	echo
	echo "${C_B}================ 점검 요약 ================${C_0}"
	for i in "${!RES_N[@]}"; do
		case ${RES_S[i]} in
			PASS) tag="${C_G}[PASS]${C_0}" ;;
			FAIL) tag="${C_R}[FAIL]${C_0}" ;;
			*) tag="${C_Y}[확인]${C_0}" ;;
		esac
		printf '%s %s단계 %s\n' "$tag" "${RES_N[i]}" "${RES_M[i]}"
	done
	echo "FAIL ${FAIL_COUNT}건"
}

cleanup_exit() {
	pkill -KILL -f "^$BIN_AS daemon$" 2> /dev/null
	if [[ -n $S && -d $S ]]; then
		pkill -KILL -f "$S/" 2> /dev/null
		rm -rf "$S"
	fi
}
trap cleanup_exit EXIT
trap 'echo; echo "[중단됨]"; exit 130' INT
trap 'exit 143' TERM

CUR=0
CUR_TITLE=""
step() { # 번호 제목 : 이 단계를 실행해야 하면 0, 건너뛰면 1
	CUR=$1; CUR_TITLE=$2
	if (( CUR < START )); then return 1; fi
	echo
	echo "${C_B}-------- [${CUR}/17] ${CUR_TITLE} --------${C_0}"
	return 0
}
say() { printf '  %s\n' "$*"; }
checkpoint() { printf '  %s확인할 것:%s %s\n' "$C_Y" "$C_0" "$*"; }
# result PASS|FAIL|CHECK 메시지
result() {
	local tag
	case $1 in
		PASS) tag="${C_G}[PASS]${C_0}" ;;
		FAIL) tag="${C_R}[FAIL]${C_0}"; FAIL_COUNT=$((FAIL_COUNT + 1)) ;;
		*) tag="${C_Y}[확인]${C_0}" ;;
	esac
	printf '  %s %s\n' "$tag" "$2"
	RES_N+=("$CUR"); RES_S+=("$1"); RES_M+=("$CUR_TITLE - $2")
}
pause() {
	local a=""
	[[ -n $YES ]] && return 0
	read -r -p "  >> Enter: 다음 단계 (q: 중단) " a || true
	if [[ $a == q ]]; then summary; exit 0; fi
}
# 보여주고 실행 (프로젝트 루트에서)
run() {
	printf '  $ %s\n' "$*"
	(cd "$ROOT" && bash -c "$*") 2>&1 | sed 's/^/    /'
	return "${PIPESTATUS[0]}"
}
# cli 기대rc 기대문구 인자... : auto_setup 실행 결과 확인 (RC_STEP=1 이면 실패)
cli() {
	local want_rc=$1 want_txt=$2 out rc=0
	shift 2
	out=$("$BIN_AS" "$@" 2>&1) || rc=$?
	printf '  $ auto_setup %s      (exit=%s)\n' "$*" "$rc"
	if [[ -n $out ]]; then printf '%s\n' "$out" | head -4 | sed 's/^/    /'; fi
	if [[ $rc -ne $want_rc || $out != *"$want_txt"* ]]; then
		printf '    (기대: exit=%s, "%s" 포함)\n' "$want_rc" "$want_txt"
		RC_STEP=1
	fi
}
daemon_pids() { pgrep -f "^$BIN_AS daemon$"; }
daemon_count() { daemon_pids | wc -l; }
wait_for() { # 초 명령...
	local t=$1 i
	shift
	for ((i = 0; i < t * 2; i++)); do
		"$@" > /dev/null 2>&1 && return 0
		sleep 0.5
	done
	return 1
}
has_jobs() { ls "$AS"/jobs/*.json > /dev/null 2>&1; }

ensure_scratch() {
	if [[ -z $S ]]; then
		S=$(mktemp -d /tmp/as_manual.XXXXXX)
		e2e_make_stubs || { echo "[X] 스텁 준비 실패"; exit 1; }
	fi
}
ensure_binary() {
	if [[ ! -x $BIN_AS ]]; then
		echo "  (./auto_setup 이 없어 먼저 빌드합니다)"
		(cd "$ROOT" && go build -o auto_setup .) || { echo "[X] 빌드 실패"; exit 1; }
	fi
}

echo "${C_B}auto_setup 단계별 점검${C_0}  (프로젝트: $ROOT)"
echo "  데이터 경로 AUTO_SETUP_DIR = $AS   (운영 /tmp/auto_setup 과 분리)"
echo "  ICMP 점검 주소: UP=$PING_UP  DOWN=$PING_DOWN"
[[ -n $YES ]] && echo "  -y : Enter 대기 없이 연속 실행" || echo "  각 단계 끝에서 Enter 를 누르면 다음으로 넘어갑니다."
mkdir -p "$AS"

# ============================================================
if step 1 "빌드 · vet · gofmt · 단위 테스트"; then
	say "무엇을 하나: 소스를 컴파일해 ./auto_setup 을 만들고, go vet / gofmt / go test 로 코드 상태를 점검합니다."
	rc=0
	run "go build -o auto_setup ." || rc=1
	run "go vet ./..." || rc=1
	printf '  $ gofmt -l .\n'
	fm=$(cd "$ROOT" && gofmt -l .)
	if [[ -z $fm ]]; then echo "    (출력 없음)"; else echo "$fm" | sed 's/^/    /'; rc=1; fi
	run "go test ./..." || rc=1
	checkpoint "go vet 에 경고가 없고, gofmt -l 출력이 비어 있고, go test 가 'ok' 로 끝나는지"
	if [[ $rc -eq 0 ]]; then result PASS "빌드·vet·gofmt·단위테스트 통과"; else result FAIL "위 출력에서 실패 항목 확인"; fi
	pause
fi

# ============================================================
if step 2 "CLI 사용법 · code 없음 · status"; then
	ensure_binary
	say "무엇을 하나: 데몬 없이 되는 CLI 를 확인합니다 (인자 없음=상태 리포트 / -h / 잘못된 명령·인자 / 없는 code / status)."
	RC_STEP=0
	cli 0 "auto_setup 상태 리포트"
	cli 0 "사용법: auto_setup" -h
	cli 1 "사용법: auto_setup" code
	cli 1 "사용법: auto_setup" bogus
	cli 1 "[X] code 없음" code 0000
	cli 0 "" status
	checkpoint "인자 없음(비 tty)은 '상태 리포트' 텍스트 표 exit=0, -h 는 '사용법' exit=0, 잘못된 명령/인자는 '사용법' exit=1, code 0000 은 '[X] code 없음', status 는 exit=0"
	if [[ $RC_STEP -eq 0 ]]; then result PASS "CLI 오류 처리·종료코드 정상"; else result FAIL "기대와 다른 출력/종료코드 (위 '기대' 줄 참고)"; fi
	pause
fi

# ============================================================
if step 3 "ensure 중복 방지 · kill -9 후 재기동"; then
	ensure_binary
	say "무엇을 하나: ensure 를 두 번 실행해도 데몬이 1개만 뜨는지, kill -9 로 죽인 뒤 ensure 하면 다시 뜨는지 봅니다."
	say "(cron 이 매분 ensure 를 호출하는 동작을 손으로 흉내 내는 것입니다)"
	rc=0
	printf '  $ auto_setup ensure   (2회)\n'
	"$BIN_AS" ensure; "$BIN_AS" ensure
	sleep 1
	n=$(daemon_count)
	printf '  데몬 프로세스 수: %s  (pid: %s)\n' "$n" "$(daemon_pids | paste -sd' ')"
	[[ $n -eq 1 ]] || rc=1
	old=$(daemon_pids | head -1)
	if [[ -n $old ]]; then
		printf '  $ kill -9 %s\n' "$old"
		kill -9 "$old"
		sleep 1
		n=$(daemon_count)
		printf '  kill 직후 데몬 수: %s\n' "$n"
		[[ $n -eq 0 ]] || rc=1
	else
		rc=1
	fi
	printf '  $ auto_setup ensure\n'
	"$BIN_AS" ensure
	if wait_for 10 pgrep -f "^$BIN_AS daemon$"; then
		new=$(daemon_pids | head -1)
		printf '  재기동 후 pid: %s (이전 %s)\n' "$new" "$old"
		[[ -n $new && $new != "$old" && $(daemon_count) -eq 1 ]] || rc=1
	else
		rc=1
	fi
	printf '  --- 로그 (%s/auto_setup.log 끝 5줄) ---\n' "$AS"
	tail -5 "$AS/auto_setup.log" 2> /dev/null | sed 's/^/    /'
	checkpoint "ensure 2회 후 데몬이 1개, kill -9 후 0개, 다시 ensure 하면 새 pid 로 1개가 되는지"
	if [[ $rc -eq 0 ]]; then result PASS "ensure 중복 방지·재기동 정상"; else result FAIL "데몬 수/pid 가 기대와 다름"; fi
	pause
fi

# ============================================================
if step 4 "실제 ICMP 감시 (probe)"; then
	ensure_binary
	say "무엇을 하나: 실제 raw ICMP 로 두 주소를 한 번 ping 합니다 (auto_setup probe 가 데몬과 같은 icmp.go 를 사용)."
	say "  UP=$PING_UP (응답해야 함)   DOWN=$PING_DOWN (응답이 없어야 함)"
	say "  다른 주소로 점검: PING_UP=<응답하는 IP> PING_DOWN=<없는 IP> bash test/manual_check.sh -s 4"
	if [[ $(id -u) -ne 0 ]]; then
		say "root 가 아니라 raw ICMP 소켓을 열 수 없습니다 (sudo 또는 root 로 다시 실행하세요)."
		result CHECK "root 아님 - ICMP 점검 건너뜀"
	else
		printf '  $ printf "%%s\\n%%s\\n" %s %s | auto_setup probe -i 1s\n' "$PING_UP" "$PING_DOWN"
		out=$(printf '%s\n%s\n' "$PING_UP" "$PING_DOWN" | "$BIN_AS" probe -i 1s 2>&1)
		printf '%s\n' "$out" | sed 's/^/    /'
		checkpoint "'$PING_UP up' 와 '$PING_DOWN down' 이 나오는지"
		if grep -qx "$PING_UP up" <<< "$out" && grep -qx "$PING_DOWN down" <<< "$out"; then
			result PASS "ICMP: $PING_UP up / $PING_DOWN down"
		else
			result FAIL "ICMP 결과가 기대($PING_UP up, $PING_DOWN down)와 다름 - 주소/방화벽 확인"
		fi
	fi
	pause
fi

# ============================================================
if step 5 "01 전달 → queue → jobs 수거 (스텁 02)"; then
	ensure_binary
	ensure_scratch
	e2e_scen m5
	say "무엇을 하나: 01 스크립트 복사본(02 는 성공하는 스텁)을 돌려 02 성공 직후 queue 파일이 남는지,"
	say "  데몬이 그것을 jobs/ 로 수거하는지 봅니다. 원본 01 은 읽기만 하고 복사본만 실행합니다."
	say "  (queue 파일이 눈에 보이도록 먼저 점검용 데몬을 내립니다 - 데몬이 죽어 있어도 queue 파일은 남고, ensure 후 수거됩니다)"
	pkill -KILL -f "^$BIN_AS daemon$" 2> /dev/null
	sleep 1
	rc=0
	printf '  $ (01 복사본 실행)  호스트: %s %s\n' "$PING_UP" "$PING_DOWN"
	e2e_run_01 "$PING_UP" "$PING_DOWN"
	printf '    01 종료코드=%s, 전달 로그: %s\n' "$RC01" "$(grep 'auto_setup 전달' "$S/out01.txt" | head -1 | sed 's/\x1b\[[0-9;]*m//g')"
	[[ $RC01 -eq 0 ]] || rc=1
	QF=$(ls "$AS"/queue/*.job 2> /dev/null | head -1)
	printf '  $ ls %s/queue\n' "$AS"
	ls "$AS/queue" | sed 's/^/    /'
	if [[ -n $QF ]]; then
		printf '  $ cat <queue 파일>\n'
		sed 's/^/    /' "$QF"
	else
		rc=1
	fi
	printf '  $ auto_setup ensure   (데몬이 5초 안에 수거)\n'
	"$BIN_AS" ensure
	if wait_for 30 has_jobs && [[ -z $(ls "$AS/queue" 2> /dev/null) ]]; then
		printf '  $ ls %s/jobs\n' "$AS"
		ls "$AS/jobs" | sed 's/^/    /'
		printf '  $ auto_setup status\n'
		"$BIN_AS" status | sed 's/^/    /'
		JID=$(basename "$(ls "$AS"/jobs/*.json | head -1)" .json)
		printf '  $ auto_setup cancel %s   (점검용 작업 정리)\n' "$JID"
		"$BIN_AS" cancel "$JID" | sed 's/^/    /'
		[[ -f $AS/jobs/done/$JID.json ]] || rc=1
	else
		rc=1
	fi
	checkpoint "queue 에 user=/time=/호스트 줄이 담긴 .job 파일이 생기고, 잠시 뒤 queue 가 비며 jobs/<날짜-시각-user>.json 이 생기는지 (status 에 호스트 2대)"
	if [[ $rc -eq 0 ]]; then result PASS "01 전달 → queue → jobs 수거 → cancel 정상"; else result FAIL "queue/jobs 흐름이 기대와 다름"; fi
	pause
fi

# ============================================================
if step 6 "os_check -auto 비대화형 (스텁 gossh)"; then
	ensure_scratch
	e2e_scen m6
	say "무엇을 하나: os_check 복사본을 -auto 로 실행합니다. 프롬프트 없이 끝까지 가야 하고,"
	say "  목록에 p/d 로 시작하는 호스트가 있으면 'set'(setting.sh 까지), 없으면 'y'(3종만) 로 진행합니다. (gossh 는 가짜)"
	rc=0
	printf 'pd01\ns2h01\ns2h02\n' > "$S/m6a.list"
	printf 's2h02\n' > "$E2E_SCEN/refused_hosts"
	printf '  $ os_check -auto %s m6a.list   (pd01 s2h01 s2h02, s2h02 는 접속 거부로 가장)\n' "$E2E_USER"
	e2e_run_oc "$S/m6a" "$S/m6a.list"
	grep -F '(-auto)' "$S/m6a/out.log" | sed 's/^/    /'
	sed -n '/결과 리포트/,$p' "$S/m6a/out.log" | head -12 | sed 's/^/    /'
	[[ $RCOC -eq 0 ]] || rc=1
	grep -Fq "(y/n/set) : set (-auto)" "$S/m6a/out.log" || rc=1
	grep -Fq 'setting.sh]' "$E2E_SCEN/gossh.log" || rc=1
	: > "$E2E_SCEN/gossh.log"
	printf 's2h01\nc01\ne01\n' > "$S/m6b.list"
	printf '  $ os_check -auto %s m6b.list   (s2h01 c01 e01 - p/d 없음)\n' "$E2E_USER"
	e2e_run_oc "$S/m6b" "$S/m6b.list"
	grep -F '(-auto)' "$S/m6b/out.log" | sed 's/^/    /'
	[[ $RCOC -eq 0 ]] || rc=1
	grep -Fq "(y/n/set) : y (-auto)" "$S/m6b/out.log" || rc=1
	if grep -q 'setting\.sh\]' "$E2E_SCEN/gossh.log"; then rc=1; fi
	for f in setting_insert rclocal appl_change; do grep -Fq "$f.sh]" "$E2E_SCEN/gossh.log" || rc=1; done
	checkpoint "첫 실행: '... : set (-auto)', 결과 리포트에 s2h02 불가 담당자 문구 / 둘째 실행: '... : y (-auto)' 이고 setting.sh 호출 없음"
	if [[ $rc -eq 0 ]]; then result PASS "-auto: 프롬프트 없이 완료, p/d→set·아니면 y"; else result FAIL "-auto 동작이 기대와 다름 ($S/m6a/out.log)"; fi
	pause
fi

# ============================================================
if step 7 "목업 전체 흐름 (test/run_e2e.sh)"; then
	ensure_scratch
	say "무엇을 하나: 실제 auto_setup 바이너리 + 가짜 gossh/ssh/wall 로 01 → queue → 감시 → READY → os_check → code/wall 까지"
	say "  전체를 자동 검증합니다 (kill -9 재기동, os6 경로 포함). 약 1분 걸립니다."
	printf '  $ bash test/run_e2e.sh\n'
	bash "$ROOT/test/run_e2e.sh" > "$S/e2e.out" 2>&1
	rc=$?
	grep -E '^(  \[FAIL\]|=====)' "$S/e2e.out" | sed 's/^/    /'
	checkpoint "마지막 줄이 'E2E 결과: PASS=N FAIL=0' 인지 (FAIL 이 있으면 위에 항목이 나열됨)"
	if grep -q '^\[SKIP\]' "$S/e2e.out"; then
		result CHECK "root 가 아니어서 E2E 건너뜀 - root 로 다시 실행"
	elif [[ $rc -eq 0 ]]; then
		result PASS "run_e2e.sh 전 시나리오 통과 ($(grep -o 'PASS=[0-9]*' "$S/e2e.out" | tail -1))"
	else
		result FAIL "run_e2e.sh 실패 (rc=$rc) - bash test/run_e2e.sh 로 직접 확인"
	fi
	pause
fi

# ============================================================
if step 8 "wall 수신 (선택, 사용자 확인)"; then
	say "무엇을 하나: 실제 wall 로 현재 서버에 로그인한 모든 터미널에 테스트 메시지를 보냅니다."
	say "  (다른 사람이 접속 중이면 그 사람 화면에도 뜹니다. 필요할 때만 yes 라고 답하세요.)"
	ans=""
	if [[ -n $YES ]]; then
		say "-y 모드: 사람이 확인할 수 없으므로 전송하지 않습니다. (직접 점검: bash test/manual_check.sh -s 8)"
	else
		read -r -p "  wall 테스트 메시지를 전송할까요? (yes 입력 시 전송, 그 외 건너뜀) " ans || ans=""
	fi
	if [[ $ans == yes ]]; then
		printf '  $ echo "[auto_setup 점검] wall 수신 테스트" | wall\n'
		echo "[auto_setup 점검] wall 수신 테스트 - 이 메시지는 무시하세요" | wall
		checkpoint "이 터미널(및 다른 접속 터미널)에 위 메시지가 표시되었는지"
		ans2=""
		read -r -p "  메시지가 표시되었나요? (y/n) " ans2 || ans2=""
		if [[ $ans2 == y ]]; then result PASS "wall 수신 확인"; else result FAIL "wall 수신 안 됨 (n 응답 또는 무응답)"; fi
	else
		result CHECK "wall 테스트 건너뜀"
	fi
	pause
fi

# ============================================================
if step 9 "설치 점검 (--install 일 때만)"; then
	if [[ -z $INSTALL ]]; then
		say "건너뜁니다. 이 단계는 /usr/local/bin·root crontab·/etc/tmpfiles.d 를 바꾸므로 --install 옵션이 있을 때만 수행합니다."
		say "  수행하려면: bash test/manual_check.sh -s 9 --install"
		result CHECK "설치 점검 건너뜀 (--install 없음)"
	else
		say "무엇을 하나: setup.sh 를 실행해 빌드→/usr/local/bin/auto_setup 설치→cron(매분 ensure)→tmpfiles 제외 등록을 확인합니다."
		say "  멱등성 확인을 위해 두 번 실행합니다. 제거 방법은 사용법.txt 의 '제거' 항목을 보세요."
		say "  변경되는 것: /usr/local/bin/auto_setup, root crontab 1줄, /etc/tmpfiles.d/auto_setup.conf"
		ans=""
		read -r -p "  setup.sh 를 실행할까요? (yes 입력 시 실행, 그 외 건너뜀) " ans || ans=""
		if [[ $ans == yes ]]; then
			rc=0
			run "bash setup.sh" || rc=1
			run "bash setup.sh" || rc=1
			n=$(crontab -l 2> /dev/null | grep -cF '/usr/local/bin/auto_setup ensure')
			printf '  crontab 의 ensure 줄 수: %s\n' "$n"
			[[ $n -eq 1 ]] || rc=1
			[[ -x /usr/local/bin/auto_setup ]] || rc=1
			grep -qxF 'x /tmp/auto_setup' /etc/tmpfiles.d/auto_setup.conf 2> /dev/null || rc=1
			checkpoint "두 번 실행해도 crontab 줄이 1개, /usr/local/bin/auto_setup 존재, '이미 등록됨' 이 두 번째 실행에 표시"
			if [[ $rc -eq 0 ]]; then result PASS "setup.sh 설치·멱등 확인"; else result FAIL "setup.sh 결과 확인 필요 (root 권한/cron 설치 여부)"; fi
		else
			result CHECK "설치 점검 건너뜀 (확인 없음)"
		fi
	fi
	pause
fi

# ============================================================
if step 10 "정리"; then
	say "무엇을 하나: 점검용 데몬을 종료하고, 점검 데이터($AS)와 임시 스크래치를 삭제합니다. (운영 /tmp/auto_setup 은 건드리지 않음)"
	pkill -KILL -f "^$BIN_AS daemon$" 2> /dev/null
	sleep 1
	if [[ -n $S && -d $S ]]; then
		pkill -KILL -f "$S/" 2> /dev/null
		rm -rf "$S"
	fi
	S=""
	rm -rf "$AS"
	rc=0
	n=$(daemon_count)
	printf '  점검용 데몬 수: %s\n' "$n"
	[[ $n -eq 0 ]] || rc=1
	if [[ -e $AS ]]; then rc=1; fi
	if ls -d /tmp/as_manual.* > /dev/null 2>&1; then rc=1; fi
	checkpoint "점검용 데몬 0개, $AS 없음, /tmp/as_manual.* 없음 (./auto_setup 바이너리는 남겨 둠)"
	if [[ $rc -eq 0 ]]; then result PASS "정리 완료 (잔여 프로세스·디렉터리 없음)"; else result FAIL "정리 후에도 남은 것이 있음"; fi
fi

# ============================================================
# 2차 단계 (11~17): 기존 1~10 번호·동작은 그대로 두고 뒤에 추가
# ============================================================

# e2e2_step 시나리오글자 : test/run_e2e2.sh <글자> 실행 → 결과 줄 표시, E2E2_RC=0|skip|<rc>, E2E2_SUM="PASS=N FAIL=M"
e2e2_step() {
	local l=$1 out="$AS/e2e2_$1.out"
	mkdir -p "$AS"
	printf '  $ bash test/run_e2e2.sh %s\n' "$l"
	bash "$ROOT/test/run_e2e2.sh" "$l" > "$out" 2>&1
	E2E2_RC=$?
	grep -E '^  \[(PASS|FAIL|SKIP)\]' "$out" | head -80 | sed 's/^/    /'
	grep -E 'E2E2 결과' "$out" | sed 's/^/    /'
	E2E2_SUM=$(grep -o 'PASS=[0-9]* FAIL=[0-9]*' "$out" | tail -1)
	if grep -q '^\[SKIP\]' "$out"; then E2E2_RC=skip; fi
}
e2e2_result() { # 통과 메시지
	if [[ $E2E2_RC == skip ]]; then result CHECK "root 가 아니어서 건너뜀 - root 로 다시 실행"
	elif [[ $E2E2_RC -eq 0 ]]; then result PASS "$1 ($E2E2_SUM)"
	else result FAIL "$1 실패 ($E2E2_SUM) - bash test/run_e2e2.sh 로 직접 확인"; fi
}

# ============================================================
if step 11 "상태 TUI · --plain 리포트 · 요청 (샘플 상태)"; then
	ensure_binary
	SD="$AS/sample"
	rm -rf "$SD"
	e2e_sample_state "$SD" "$(date +%s)"
	say "무엇을 하나: 모든 단계가 보이는 가짜 상태를 만들어 'auto_setup'(상태 리포트)를 확인합니다. 데몬 없이 상태 파일만 읽습니다."
	say "  샘플 위치: $SD   (작업 3개: alice=gpu.yml·cpu.yml 2그룹, bob=그룹 줄 없는 구버전, carol=종료된 작업 ok.yml·bad.yml)"
	rc=0
	printf '  $ AUTO_SETUP_DIR=%s auto_setup --plain\n' "$SD"
	out=$(AUTO_SETUP_DIR="$SD" "$BIN_AS" --plain 2>&1) || rc=1
	printf '%s\n' "$out" | sed 's/^/    /'
	for k in 'gpu.yml' 'cpu.yml' '\(all\)' 'ok.yml' 'bad.yml' '정체 1 +실패 1'; do
		grep -qE "$k" <<< "$out" || { printf '    (기대: 출력에 "%s")\n' "$k"; rc=1; }
	done
	checkpoint "상단: 전체 N 완료 N 진행 N 정체 1 실패 1 + 갱신 시각 + 데몬 중지 / 표: 그룹별 호스트 수·단계별 건수·최장경과 (gpu.yml 정체, cpu.yml 실패 포함, carol 은 '(종료)')"
	if [[ -z $YES ]]; then
		say "이제 TUI 를 직접 엽니다. (다른 터미널에서 직접 열려면:  AUTO_SETUP_DIR=$SD $BIN_AS )"
		say "  화면1: 상단 총계 · 그룹 행(이름·infra/os/boot/splunk·호스트 수·단계 막대·최장 경과)"
		say "  화면2(Enter): 호스트별 표 - g3 는 1시간을 넘어 빨간 '정체', c2 는 실패, ok.yml 에서는 하단 [c] 수동 실행이 활성"
		say "  키: ↑↓ 행 · Enter 상세 · ←→ 작업 전환/돌아가기 · f 정체·실패만 · c 수동 실행(y/n, 데몬이 없으니 요청 파일만 남음) · r 새로고침 · ? 도움말 · Esc/q 돌아가기·종료"
		checkpoint "↑↓ ←→ Enter c q 를 눌러 보고, 색(완료 초록·진행 청록·대기 회색·경고 노랑·정체/실패 빨강, 끄기: NO_COLOR=1)이 맞는지"
		ans=""
		read -r -p "  >> Enter: 이 터미널에서 TUI 열기 (q 로 닫으면 돌아옴, s: 건너뛰기) " ans || ans=s
		if [[ $ans != s ]]; then
			AUTO_SETUP_DIR="$SD" "$BIN_AS"
			ans2=""
			read -r -p "  TUI 화면이 위 '확인할 것' 대로였나요? (y/n) " ans2 || ans2=""
			[[ $ans2 == y ]] || rc=1
		else
			say "(TUI 직접 확인 건너뜀)"
		fi
	else
		say "-y 모드: TUI 는 사람이 직접 봐야 하므로 --plain 만 자동 확인합니다. (직접 점검: bash test/manual_check.sh -s 11)"
	fi
	say "자동 스모크: manual-run 요청 거부/수락 → 수동 run → code, 원격 request, pty 로 TUI 화면1→Enter→q (run_e2e2.sh c)"
	e2e2_step c
	[[ $E2E2_RC == 0 || $E2E2_RC == skip ]] || rc=1
	if [[ $rc -ne 0 ]]; then result FAIL "리포트/TUI/요청 확인 실패 (위 출력 참고)"
	elif [[ $E2E2_RC == skip ]]; then result CHECK "리포트 확인 통과, 요청·TUI 자동 스모크는 root 가 아니어서 건너뜀"
	else result PASS "--plain 리포트 + 요청/TUI 스모크 통과 ($E2E2_SUM)"; fi
	pause
fi

# ============================================================
if step 12 "데몬 --start / --restart / --stop"; then
	ensure_binary
	ensure_scratch
	CD="$AS/ctl"
	rm -rf "$CD"; mkdir -p "$CD"
	say "무엇을 하나: 별도 데이터 디렉터리($CD)에서 데몬을 --start(중복 안내) → --restart(pid 변경) → --stop(pid 파일 정리) 합니다."
	say "  원격 클라이언트 빌드(-X main.os8_mgmt)는 --start/--stop/--restart 를 거부해야 합니다."
	CTL_OUT=""; CTL_RC=0
	ctl() { printf '  $ auto_setup %s\n' "$*"; CTL_OUT=$(AUTO_SETUP_DIR="$CD" "$BIN_AS" "$@" 2>&1); CTL_RC=$?; printf '%s\n' "$CTL_OUT" | sed 's/^/    /'; }
	cpid() { head -1 "$CD/auto_setup.pid" 2> /dev/null | awk '{print $1}'; }
	calive() { local p; p=$(cpid); [[ -n $p ]] && kill -0 "$p" 2> /dev/null; }
	rc=0
	ctl --start; p1=$(cpid)
	[[ $CTL_RC -eq 0 && $CTL_OUT == *"데몬 기동"* ]] && calive || { rc=1; echo "    (기대: 기동 + pid 파일 + 프로세스 생존)"; }
	ctl --start
	[[ $CTL_RC -eq 0 && $CTL_OUT == *"이미 실행 중"* && $(cpid) == "$p1" ]] || { rc=1; echo "    (기대: '이미 실행 중' 안내, pid 불변)"; }
	ctl --restart; p2=$(cpid)
	[[ $CTL_RC -eq 0 && -n $p2 && $p2 != "$p1" ]] && calive || { rc=1; echo "    (기대: pid 변경 + 생존)"; }
	ctl --stop
	if [[ $CTL_RC -ne 0 ]] || calive || [[ -e $CD/auto_setup.pid ]]; then rc=1; echo "    (기대: 프로세스 종료 + pid 파일 삭제)"; fi
	ctl --stop
	[[ $CTL_RC -eq 0 && $CTL_OUT == *"실행 중이 아닙니다"* ]] || { rc=1; echo "    (기대: '실행 중이 아닙니다' 안내, rc 0)"; }
	if (cd "$ROOT" && go build -ldflags "-X main.os8_mgmt=os8.mgmt" -o "$S/auto_setup_client" .); then
		for a in --start --stop --restart; do
			printf '  $ (원격 클라이언트 빌드) auto_setup %s\n' "$a"
			o=$(AUTO_SETUP_DIR="$CD" "$S/auto_setup_client" "$a" 2>&1); r=$?
			printf '%s\n' "$o" | sed 's/^/    /'
			[[ $r -eq 1 && $o == *"os8_mgmt 에서만"* ]] || { rc=1; echo "    (기대: rc=1 + '데몬은 os8_mgmt 에서만')"; }
		done
	else
		rc=1; echo "    (원격 클라이언트 빌드 실패)"
	fi
	checkpoint "--start 두 번째는 '이미 실행 중 (pid N)', --restart 후 pid 가 바뀜, --stop 후 pid 파일이 사라짐, 원격 빌드는 거부"
	if [[ $rc -eq 0 ]]; then result PASS "--start/--restart/--stop·원격 거부 정상"; else result FAIL "데몬 제어 결과가 기대와 다름"; fi
	pause
fi

# ============================================================
if step 13 "양방향 동일성 (원격 클라이언트 모드 · 스텁 gossh)"; then
	ensure_binary
	say "무엇을 하나: 같은 상태 디렉터리에 대해 status/snapshot/code/리포트를 (1) 로컬로, (2) 원격 클라이언트 모드(os8_mgmt 채운 빌드 + 스텁 gossh 가"
	say "  로컬 바이너리를 호출)로 실행해 stdout/stderr/종료코드가 같은지 비교합니다. 01 의 os8_mgmt 전달도 로컬 전달과 같은 queue 인지 봅니다."
	e2e2_step a
	checkpoint "a1~a6(status·snapshot·code·리포트) 가 모두 PASS(= 로컬과 원격 출력 diff 0), a15 01 원샷 전달, a16 데몬 수거 PASS"
	e2e2_result "양방향 동일성·요청/완료/취소·01 전달"
	pause
fi

# ============================================================
if step 14 "완료기록 (os_check 기록 → 데몬 인정·제외)"; then
	ensure_binary
	say "무엇을 하나: os_check 복사본(auto_done_dir/auto_done_host 채움)이 정상 종료 시 남긴 완료기록을 데몬이 인정해 run 에서 제외하고 done/applied/ 로"
	say "  옮기는지, 전달 이전·부팅 이전 기록은 거부하는지(로그 1회), auto_setup done <host> 수동 완료가 되는지 봅니다."
	e2e2_step b
	checkpoint "b2 오래된 기록 거부(로그 1회) · b4 인정+applied 이동 · b6 수동 완료 · b7 run 대상 4대(2·4·7 제외, run 1회)"
	e2e2_result "완료기록 인정/거부/수동 완료"
	pause
fi

# ============================================================
if step 15 "LDAP 백업/복원 (스텁 gossh)"; then
	ensure_binary
	say "무엇을 하나: 전달 시점 백업(디렉터리 0700·파일 0600) → READY 후 bindpw same(생략)/diff(백업본 복원) → os_check run 순서를 스텁으로 검증하고,"
	say "  테스트용 비밀 문자열이 로그·codes·job·wall·status·snapshot·리포트·gossh 기록 어디에도 없는지 grep 합니다."
	e2e2_step e
	checkpoint "e2 권한 700/600 · e4 호출 순서 collect→probe→restore→run · e5 same/diff · e7 비밀 문자열 미노출 PASS"
	e2e2_result "LDAP 백업/복원 흐름·bindpw 미노출"
	pause
fi

# ============================================================
if step 16 "2차 체크 (os6_mgmt, 스텁 ssh)"; then
	ensure_binary
	say "무엇을 하나: os6_mgmt/os6_os_check_sh 를 채운 빌드로 1차 FAIL·접속불가 호스트(route=local)만 2차 체크가 되어 code 에"
	say "  '### 2차 체크 (os6_mgmt)' 섹션과 최종 판정이 붙는지, 변수를 비운 빌드는 code 가 1차와 같은지 봅니다."
	e2e2_step f
	checkpoint "f1 2차 대상 3대 · f2 code 2차 섹션과 최종 판정(FAIL/NO FAIL) · f5 변수 비운 빌드는 2차 섹션 없음"
	e2e2_result "2차 체크 대상 선별·code 병합·변수 비움 동일"
	pause
fi

# ============================================================
if step 17 "정리 (2차 단계 포함)"; then
	say "무엇을 하나: 점검용 데몬을 종료하고, 점검 데이터($AS 아래 sample·ctl 포함)와 임시 스크래치를 삭제합니다. (운영 /tmp/auto_setup 은 건드리지 않음)"
	pkill -KILL -f "^$BIN_AS daemon$" 2> /dev/null
	sleep 1
	if [[ -n $S && -d $S ]]; then
		pkill -KILL -f "$S/" 2> /dev/null
		rm -rf "$S"
	fi
	S=""
	rm -rf "$AS"
	rc=0
	n=$(daemon_count)
	printf '  점검용 데몬 수: %s\n' "$n"
	[[ $n -eq 0 ]] || rc=1
	if [[ -e $AS ]]; then rc=1; fi
	if ls -d /tmp/as_manual.* /tmp/as_e2e2.* > /dev/null 2>&1; then rc=1; fi
	checkpoint "점검용 데몬 0개, $AS 없음, /tmp/as_manual.*·/tmp/as_e2e2.* 없음 (./auto_setup 바이너리는 남겨 둠)"
	if [[ $rc -eq 0 ]]; then result PASS "정리 완료 (잔여 프로세스·디렉터리 없음)"; else result FAIL "정리 후에도 남은 것이 있음"; fi
fi

summary
[[ $FAIL_COUNT -eq 0 ]] || exit 1
exit 0
