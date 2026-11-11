#!/bin/bash
# test.sh - new_setup -run 화면(TUI) 조작감 데모. 가짜 서버(가짜 gossh·config_check·stress-test)로
# 실제 흐름(1→5단계, 질문, stress-ng 종료 시각 입력)을 그대로 돌리므로 실제 서버가 없어도 키 조작을 써 볼 수 있다.
#
#   bash test.sh            호스트 60대로 시작
#   bash test.sh 8000       호스트 8000대로 시작 (스크롤 감각 확인)
#   KEEP=1 bash test.sh     끝난 뒤 데모 폴더를 지우지 않음(worker.log·state.json 구경)
#   NEW_SETUP=/경로 bash test.sh   다른 바이너리로 시험 (기본: bin/new_setup, 없거나 실행 안 되면 go build)
#
# 실제 서버·AWX·원격 서버에는 아무것도 하지 않는다. 데모 폴더(기본 /tmp/new_setup_demo.$USER)는 매번 새로 만들고 끝나면 지운다.
set -u
HERE=$(cd "$(dirname "$0")" && pwd -P)
N=${1:-60}
case $N in '' | *[!0-9]*) echo "사용: bash test.sh [호스트수]" >&2; exit 2 ;; esac
[ "$N" -ge 1 ] || { echo "호스트수는 1 이상" >&2; exit 2; }
D=${DEMO_DIR:-/tmp/new_setup_demo.${USER:-x}}
FAKEDIR=$HERE/test

if [ ! -t 0 ] || [ ! -t 1 ]; then
	echo "터미널(tty)에서 실행하세요: bash test.sh" >&2
	exit 2
fi

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

# 호스트·시나리오: 대부분 정상, 일부는 접속불가·CPU 미완료·stress-ng 미완료·설정 불합격
: > "$D/hosts"; : > "$D/scenario"; : > "$D/cc_scenario"
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
CONF

export FAKE_MARK_DIR=$D/mark FAKE_CC_STATE=$D/ccstate FAKE_ST_LOG=$D/st.log FAKE_ST_PIDFILE=$D/st.pids
export FAKE_CC_LOG=$D/cc.log FAKE_GOSSH_LOG=$D/gossh.log FAKE_GOSSH_SCENARIO=$D/scenario FAKE_CC_SCENARIO=$D/cc_scenario
export FAKE_YML_DIR=$D/yml FAKE_ST_DELAY=${FAKE_ST_DELAY:-2}

cd "$D" || exit 1
"$BIN" -run -c "$D/conf" -user demo "$D/hosts"
rc=$?

cat <<'TIP'

[데모 종료] 가짜 서버로 돌린 연습입니다. 실제 서버에는 아무 일도 하지 않았습니다.
  다시 해 보기: bash test.sh [호스트수]      종료 시각 입력 칸: 3·4단계 stress-ng 질문에서 나옵니다
TIP
exit $rc
