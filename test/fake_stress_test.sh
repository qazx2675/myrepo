#!/bin/bash
# fake_stress_test.sh - new_setup -run 시험용 가짜 stress-test (진짜 -batch 모드 흉내)
# 사용: fake_stress_test.sh -user <u> -hosts <목록파일> -steps a|b -batch [-conf <파일>]
#   FAKE_ST_LOG=<파일>      호출 기록(시각은 ns). 줄 형식:
#                           START <steps> <ns> user=<u> hosts=<쉼표목록> batch=<0|1> conf=<값>
#                           MON <steps> <ns>        ([감시] 줄을 낸 시각)
#                           END <steps> <ns>
#   FAKE_ST_PIDFILE=<파일>  시작할 때 자기 pid 를 이어 붙임(시험이 kill 여부를 확인)
#   FAKE_ST_DELAY=<초>      시작 뒤 [감시] 줄까지(기본 1)
#   FAKE_MARK_DIR=<폴더>    fake_gossh.sh 가 읽는 표식 폴더
#                             -steps a : 감시 줄 직후 cpu_<호스트> 를 만들고(CPU 성공), 1초 뒤 FAKE_ST_REBOOT 의 호스트에 reboot_<호스트>
#                             -steps b : 감시 줄 직후 ng_<호스트> 를 만든다(stress-ng 성공)
#                             hang_<steps> 파일이 있으면 [감시] 줄 전에 멈춘다(시간초과·중지 시험)
#   FAKE_ST_REBOOT=<호스트...>  -steps a 중 부팅 시각이 바뀌는 호스트(공백 구분)
#   FAKE_ST_EXIT=<코드>     종료 코드(기본 0)
set -u

user=""
hostsf=""
steps=""
batch=0
conf=""
while [ $# -gt 0 ]; do
	case $1 in
		-user) user=$2; shift 2 ;;
		-hosts) hostsf=$2; shift 2 ;;
		-steps) steps=$2; shift 2 ;;
		-conf) conf=$2; shift 2 ;;
		-batch) batch=1; shift ;;
		*) echo "fake_stress_test: 알 수 없는 인자 $1" >&2; exit 2 ;;
	esac
done
if [ -z "$user" ] || [ -z "$hostsf" ] || [ -z "$steps" ] || [ ! -f "$hostsf" ]; then
	echo "fake_stress_test: -user -hosts -steps 필요(목록 파일 없음?)" >&2
	exit 2
fi

hosts=()
while IFS= read -r line || [ -n "$line" ]; do
	h=$(printf '%s' "$line" | sed 's/^[[:space:]]*//; s/[[:space:]]*$//')
	case $h in '' | '#'*) continue ;; esac
	hosts+=("$h")
done < "$hostsf"
hl=$(IFS=,; echo "${hosts[*]-}")

now() { date +%s%N; }
log() { [ -n "${FAKE_ST_LOG:-}" ] && echo "$*" >> "$FAKE_ST_LOG"; return 0; }

log "START $steps $(now) user=$user hosts=$hl batch=$batch conf=${conf:-none}"
[ -n "${FAKE_ST_PIDFILE:-}" ] && echo "$$" >> "$FAKE_ST_PIDFILE"
echo "fake stress-test: steps=$steps hosts=$hl"

MARK=${FAKE_MARK_DIR:-}
if [ -n "$MARK" ] && [ -e "$MARK/hang_$steps" ]; then
	# 진짜 stress-test 가 멈춘 것처럼 오래 잔다(프로세스 그룹 SIGTERM 으로 끝나는지 시험)
	sleep 600
	exit 0
fi

sleep "${FAKE_ST_DELAY:-1}"
echo "[감시] $steps 경과 1/60초 기준(3) 미만 ${#hosts[@]}대: $hl"
log "MON $steps $(now)"

case $steps in
	a)
		if [ -n "$MARK" ]; then
			for h in ${hosts[@]+"${hosts[@]}"}; do : > "$MARK/cpu_$h"; done
		fi
		sleep 1
		if [ -n "$MARK" ]; then
			for h in ${FAKE_ST_REBOOT:-}; do : > "$MARK/reboot_$h"; done
		fi
		sleep 1
		;;
	b)
		if [ -n "$MARK" ]; then
			for h in ${hosts[@]+"${hosts[@]}"}; do : > "$MARK/ng_$h"; done
		fi
		sleep 1
		;;
esac
log "END $steps $(now)"
exit "${FAKE_ST_EXIT:-0}"
