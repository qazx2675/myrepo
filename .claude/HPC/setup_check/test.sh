#!/bin/bash
# test.sh - new_setup -run 화면(TUI) 데모. 가짜 서버(가짜 gossh·config_check·stress-test)로 실제 흐름을 그대로 돌리므로
# 실제 서버가 없어도 키 조작과 -auto 의 새 동작을 직접 써 볼 수 있다.
#
#   bash test.sh              메뉴가 뜸(한 글자 + Enter, 그냥 Enter = 2)
#   bash test.sh 1 [호스트수]  1) 조작감 데모: -run 화면(기본 60대, 8000 이면 스크롤 감각 확인)
#   bash test.sh 2            2) -auto 데모: 초 단위로 줄인 가짜 서버로 -run -auto — 환경 실패 재시도 간격 증가·CPU 벤더 미인식 포기·
#                                설정수정 계속 불합격 포기·stress-ng 미실행 재launch·접속불가 제외 호스트 복귀를 한 화면에서 봄(몇 분)
#   bash test.sh 3            3) -auto 인벤토리 사용 중 질문: r/x 질문이 뜨는 동안에도 나머지 호스트가 계속 진행되는 것을 봄(질문에 r 로 답)
#   bash test.sh 4            4) -auto 삭제 확인 중지(시뮬레이션): stress-test 가 인벤토리 삭제 확인 실패로 launch 전에 멈춰도
#                                포기하지 않고 환경 실패로 재시도하는 것을 봄(약 1분 반 뒤 스스로 끝남)
#   bash test.sh 8000         (하위 호환) 첫 인자가 4 보다 큰 숫자면 메뉴 1 + 그 호스트 수. 1~4 는 메뉴 번호이므로 호스트 수 1~4대는 `bash test.sh 1 3` 처럼 씀
#   KEEP=1 bash test.sh       끝난 뒤 데모 폴더를 지우지 않음(worker.log·state.json 구경)
#   NEW_SETUP=/경로 bash test.sh   다른 바이너리로 시험 (기본: bin/new_setup, 없거나 실행 안 되면 go build)
#
# 실제 서버·AWX·원격 서버에는 아무것도 하지 않는다. 데모 폴더(기본 /tmp/new_setup_demo.$USER)는 매번 새로 만들고 끝나면 지운다.
set -u
HERE=$(cd "$(dirname "$0")" && pwd -P)
D=${DEMO_DIR:-/tmp/new_setup_demo.${USER:-x}}
FAKEDIR=$HERE/test

if [ ! -t 0 ] || [ ! -t 1 ]; then
	echo "터미널(tty)에서 실행하세요: bash test.sh" >&2
	exit 2
fi

# ---- 모드·호스트 수 정하기
MODE=""
N=60
usage() { echo "사용: bash test.sh [1|2|3|4] [호스트수(1번만)]   또는 bash test.sh <5 이상의 호스트수>" >&2; exit 2; }
if [ $# -ge 1 ]; then
	case $1 in '' | *[!0-9]*) usage ;; esac
	if [ "$1" -ge 1 ] && [ "$1" -le 4 ]; then
		MODE=$1
		if [ $# -ge 2 ]; then
			case $2 in '' | *[!0-9]*) usage ;; esac
			N=$2
		fi
	else
		MODE=1 # 하위 호환: bash test.sh 8000
		N=$1
	fi
fi
if [ -z "$MODE" ]; then
	cat <<'MENU'
new_setup 데모 — 가짜 서버로 돌립니다(실제 서버에는 아무 일도 하지 않음)
  1) 조작감 데모            -run 화면 키 조작 (호스트 60대)
  2) -auto 데모             환경 실패 재시도·CPU 벤더 미인식 포기·설정수정 포기·stress-ng 재launch·제외 호스트 복귀 (몇 분)
  3) -auto 인벤토리 사용 중   r/x 질문이 떠 있는 동안에도 나머지가 계속 진행됨
  4) -auto 삭제 확인 중지     stress-test 가 launch 전에 멈춰도 포기하지 않고 환경 실패로 재시도(시뮬레이션)
MENU
	printf '번호를 누르세요 [2]: '
	key=""
	IFS= read -r -n1 key
	echo
	case $key in 1 | 2 | 3 | 4) MODE=$key ;; *) MODE=2 ;; esac
fi
[ "$N" -ge 1 ] || { echo "호스트수는 1 이상" >&2; exit 2; }

# ---- 이전 데모 흔적 정리
pkill -f "$D/hosts_run" 2> /dev/null
rm -rf "$D" "$D.bin"

# ---- 바이너리
BIN=${NEW_SETUP:-$HERE/bin/new_setup}
# 압축을 풀면서 실행 권한이 빠진 경우: 복사본에 권한을 준다
if [ -f "$BIN" ] && [ ! -x "$BIN" ]; then
	mkdir -p "$D.bin" && cp "$BIN" "$D.bin/new_setup" && chmod +x "$D.bin/new_setup" && BIN=$D.bin/new_setup
fi
if [ ! -x "$BIN" ]; then
	command -v go > /dev/null 2>&1 || { echo "bin/new_setup 을 실행할 수 없고 go 도 없습니다" >&2; exit 1; }
	echo "bin/new_setup 을 쓸 수 없어 go build 합니다..."
	mkdir -p "$D.bin" && (cd "$HERE" && go build -o "$D.bin/new_setup" .) || { echo "go build 실패" >&2; exit 1; }
	BIN=$D.bin/new_setup
fi

# ---- 정리: 워커·가짜 stress-test 를 끄고 폴더를 지운다
cleanup() {
	local f p
	for f in "$D"/hosts_run/worker.pid; do
		[ -f "$f" ] || continue
		p=$(tr -d '[:space:]' < "$f")
		[ -n "$p" ] && kill "$p" 2> /dev/null
	done
	if [ -f "$D/st.pids" ]; then
		while read -r p; do [ -n "$p" ] && kill -9 "$p" 2> /dev/null; done < "$D/st.pids"
	fi
	sleep 0.3
	if [ -n "${KEEP:-}" ]; then
		echo "데모 폴더 유지: $D"
	else
		rm -rf "$D" "$D.bin"
	fi
}
trap cleanup EXIT
trap 'exit 130' INT TERM

# ---- 데모 폴더·가짜 환경
mkdir -p "$D/mark" "$D/ccstate" "$D/gb"
chmod +x "$FAKEDIR"/fake_*.sh 2> /dev/null
printf '#!/bin/bash\nexec bash "%s" "$@"\n' "$FAKEDIR/fake_gossh.sh" > "$D/gb/gossh"
chmod +x "$D/gb/gossh"
: > "$D/st.log"; : > "$D/st.pids"; : > "$D/st.conf"
: > "$D/hosts"; : > "$D/scenario"; : > "$D/cc_scenario"

# 모드별 설정 줄(conf 에 덧붙임)·-run 인자·끝난 뒤 안내
EXTRA_CONF=""
RUN_ARGS=""
TIP=""
AUTO_CONF="AUTO_CPU_WAIT=4s
AUTO_CPU_INTERVAL=3s
AUTO_CPU_MAX=8s
AUTO_RECHECK=6s"

case $MODE in
	1)
		# 호스트·시나리오: 대부분 정상, 일부는 접속불가·CPU 미완료·stress-ng 미완료·설정 불합격
		i=1
		while [ "$i" -le "$N" ]; do
			h=$(printf 'demo-node-%04d' "$i")
			echo "$h" >> "$D/hosts"
			if [ $((i % 23)) -eq 0 ]; then
				printf '%s\toff\n' "$h" >> "$D/scenario"
			elif [ $((i % 37)) -eq 0 ]; then
				printf '%s\trefused\n' "$h" >> "$D/scenario"
			elif [ $((i % 5)) -eq 0 ]; then
				printf '%s\tok\tcpu=ng stress=0\n' "$h" >> "$D/scenario"
			elif [ $((i % 7)) -eq 0 ]; then
				printf '%s\tok\tstress=0\n' "$h" >> "$D/scenario"
			fi
			[ $((i % 11)) -eq 0 ] && printf '%s\tfail\n' "$h" >> "$D/cc_scenario"
			i=$((i + 1))
		done
		TIP="  다시 해 보기: bash test.sh 1 [호스트수]      종료 시각 입력 칸: 3·4단계 stress-ng 질문에서 나옵니다"
		;;
	2)
		# -auto 데모 13대: 이름별 역할
		#   01 벤더 미인식(vendor=other)                       → CPU 벤더 미인식 으로 바로 포기
		#   02 설정수정이 끝내 불합격(수정 흉내 표식을 막아 둠)    → 설정수정 3회 뒤 포기
		#   03 stress-ng 첫 launch 에서 부하 안 오름(notrun)     → 한 번 미실행 뒤 다시 launch 해서 O
		#   04 첫 launch 에서 접속불가로 제외됨                  → 환경 실패로 대기열 뒤로 갔다가 다시 launch
		#   05~08 CPU·stress-ng 모두 필요, 09~11 stress-ng 만 필요, 12·13 처음부터 정상
		i=1
		while [ "$i" -le 13 ]; do
			h=$(printf 'auto-node-%02d' "$i")
			echo "$h" >> "$D/hosts"
			case $i in
				1) printf '%s\tok\tvendor=other\n' "$h" >> "$D/scenario" ;;
				2)
					printf '%s\tok\tcpu=nolog stress=0\n' "$h" >> "$D/scenario"
					printf '%s\tfail\n' "$h" >> "$D/cc_scenario"
					# fixed_<호스트> 는 -auto 의 수정 흉내(touch)인데, 가리키는 폴더가 없어 만들어지지 않는다 → 계속 불합격
					ln -s "$D/ccstate/nodir/x" "$D/ccstate/fixed_$h"
					;;
				3 | 9 | 10 | 11) printf '%s\tok\tstress=0\n' "$h" >> "$D/scenario" ;;
				4 | 5 | 6 | 7 | 8) printf '%s\tok\tcpu=nolog stress=0\n' "$h" >> "$D/scenario" ;;
			esac
			i=$((i + 1))
		done
		EXTRA_CONF="$AUTO_CONF
AUTO_LIMIT=10m"
		RUN_ARGS="-auto"
		export FAKE_ST_EVENTS=1 FAKE_ST_ENV_FAIL=3 FAKE_ST_NG_NOTRUN=auto-node-03 FAKE_ST_NG_NOTRUN_ONCE=1
		export FAKE_ST_EXCLUDE=auto-node-04 FAKE_ST_EXCLUDE_ONCE=1
		TIP="[이 데모에서 볼 것] (v 키로 결과 판, l 키로 로그 창)
  - 처음 3번의 launch 가 \"AWX 조회 실패(가짜)\" 로 끝남 → 마지막오류 칸에 \`환경 실패 n회\`, 재시도 간격 3초→6초→12초로 늘어남(포기·시도 횟수 증가 없음)
  - auto-node-01: \`CPU 벤더 미인식\` 으로 바로 포기(CPU 테스트를 돌리지 않음)
  - auto-node-02: 설정수정이 계속 불합격 → 3회 뒤 포기
  - auto-node-03: stress-ng launch 직후 \`stress-ng 실행중\`(O 아님) → [결과] notrun → 한 번 미실행 뒤 다시 launch → O
  - auto-node-04: 접속불가로 제외 → 대기열 뒤로 갔다가 다시 launch → 완료
  - 나머지 호스트는 완료(OOO). 12시간 제한 대신 이 데모는 10분 제한 — 할 일이 끝나면 스스로 끝납니다
  다시 해 보기: bash test.sh 2"
		;;
	3)
		# -auto 인벤토리 사용 중: CPU launch 가 사용 중 질문을 내는 동안 stress-ng 대상은 계속 진행된다
		i=1
		while [ "$i" -le 8 ]; do
			h=$(printf 'busy-node-%02d' "$i")
			echo "$h" >> "$D/hosts"
			case $i in
				1 | 2 | 3) printf '%s\tok\tcpu=nolog stress=0\n' "$h" >> "$D/scenario" ;; # CPU 묶음(사용 중 질문이 걸림)
				4 | 5 | 6) printf '%s\tok\tstress=0\n' "$h" >> "$D/scenario" ;;           # stress-ng 만(질문과 상관없이 진행)
			esac
			i=$((i + 1))
		done
		EXTRA_CONF="$AUTO_CONF
AUTO_LIMIT=10m"
		RUN_ARGS="-auto"
		export FAKE_ST_EVENTS=1 FAKE_ST_BUSY=a FAKE_ST_BUSY_TIMES=1
		TIP="[이 데모에서 볼 것]
  - CPU 묶음 launch 에서 \"인벤토리 사용 중\" 질문(r/x)이 뜸. 바로 답하지 말고 몇 초 지켜보세요:
    그동안 busy-node-04~06 의 stress-ng 가 \`stress-ng 실행중\` → O 로 계속 진행됩니다(질문이 -auto 를 멈추지 않음)
  - r 로 답하면 CPU 묶음이 이어서 launch 되고, 할 일이 끝나면 스스로 끝납니다(x 로 답하면 이 묶음은 환경 실패로 보고 다시 시도)
  다시 해 보기: bash test.sh 3"
		;;
	4)
		# -auto 삭제 확인 중지(시뮬레이션): 진짜 stress-test 는 인벤토리 삭제 스크립트가 삭제하지 않았으면(전후 호스트가 같으면) 등록 전에 종료코드 2 로 멈춘다
		i=1
		while [ "$i" -le 6 ]; do
			h=$(printf 'del-node-%02d' "$i")
			echo "$h" >> "$D/hosts"
			case $i in
				1 | 2) printf '%s\tok\tcpu=nolog stress=0\n' "$h" >> "$D/scenario" ;;
				3 | 4) printf '%s\tok\tstress=0\n' "$h" >> "$D/scenario" ;;
			esac
			i=$((i + 1))
		done
		EXTRA_CONF="$AUTO_CONF
AUTO_LIMIT=90s"
		RUN_ARGS="-auto"
		export FAKE_ST_EVENTS=1 FAKE_ST_DELCHECK_FAIL=1
		TIP="[이 데모에서 볼 것 — 가짜 stress-test 로 한 시뮬레이션입니다]
  - 모든 launch 가 \"인벤토리 삭제 확인 실패: 삭제 전과 같은 호스트가 남아 있습니다(가짜)\" 로 launch 전에 멈춤(종료코드 2)
  - 호스트는 포기되지 않고(시도 횟수 증가 없음) 마지막오류 칸에 \`환경 실패 n회\`, 재시도 간격이 3초→6초→12초→…로 늘어남
  - 제한 시간(90초)이 지나면 새 작업 없이 스스로 끝남. 실제 stress-test 에서는 이 상황이 인벤토리 삭제 스크립트가 안 먹었다는 뜻이므로 사람이 확인해야 합니다
  다시 해 보기: bash test.sh 4"
		;;
esac

cat > "$D/conf" <<CONF
GOSSH=$D/gb/gossh
OS_CHECK_SH=$FAKEDIR/fake_config_check.sh
STRESS_TEST=$FAKEDIR/fake_stress_test.sh
STRESS_TEST_CONF=$D/st.conf
CPU_WAIT_START=3s
CPU_WAIT_INTERVAL=1s
CPU_WAIT_MAX=8s
GOSSH_TIMEOUT=20s
CHECK_TIMEOUT=20s
STRESS_TIMEOUT=60s
$EXTRA_CONF
CONF

export FAKE_MARK_DIR=$D/mark FAKE_CC_STATE=$D/ccstate FAKE_ST_LOG=$D/st.log FAKE_ST_PIDFILE=$D/st.pids
export FAKE_CC_LOG=$D/cc.log FAKE_GOSSH_LOG=$D/gossh.log FAKE_GOSSH_SCENARIO=$D/scenario FAKE_CC_SCENARIO=$D/cc_scenario
export FAKE_YML_DIR=$D/yml FAKE_ST_DELAY=${FAKE_ST_DELAY:-2}

cd "$D" || exit 1
# shellcheck disable=SC2086
"$BIN" -run $RUN_ARGS -c "$D/conf" -user demo "$D/hosts"
rc=$?

echo
echo "[데모 종료] 가짜 서버로 돌린 연습입니다. 실제 서버에는 아무 일도 하지 않았습니다."
[ "$MODE" = 4 ] && echo "  (4번은 stress-test 삭제 확인 실패를 가짜로 꾸민 시뮬레이션입니다)"
[ -n "$TIP" ] && printf '%s\n' "$TIP"
exit $rc
