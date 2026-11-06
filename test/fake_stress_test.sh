#!/bin/bash
# fake_stress_test.sh - new_setup -run 시험용 가짜 stress-test (진짜 -batch 모드 흉내)
# 사용: fake_stress_test.sh -user <u> -hosts <목록파일> -steps a|b -batch [-end y,m,d,h,min] [-conf <파일>]
#   -steps 에 b 가 있는데 -end 가 없거나 형식이 틀리면 종료코드 2(진짜 stress-test 와 같음)
#   -conf 파일이 없으면 종료코드 2(진짜와 같음)
#   FAKE_ST_LOG=<파일>      호출 기록(시각은 ns). 줄 형식:
#                           START <steps> <ns> user=<u> hosts=<쉼표목록> batch=<0|1> conf=<값> end=<값|none>  gossh=<환경변수 GOSSH|none>
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
#   FAKE_ST_BUSY=<steps 목록>  이 steps(예: b, ab)의 호출은 [감시] 전에 "[인벤토리사용중] ..." 줄을 찍고 stdin 에서 한 줄을 읽는다.
#                           r 이면 계속(FAKE_ST_BUSY_TIMES 번 반복, 기본 1), 그 밖·EOF 면 `BUSYANS` 기록 뒤 종료코드 1
#                           기록: BUSY <steps> <ns> / BUSYANS <steps> <답>
#   FAKE_ST_BUSY_SLEEP=<초> 인벤토리 사용 중 줄을 찍은 뒤 stdin 을 읽기 전에 쉬는 시간(없으면 바로 읽음)
#   FAKE_YML_DIR=<폴더>     가짜 AWX 인벤토리 소스(진짜 -yaml-state 흉내). 폴더 안 yml(소스 yml 한 줄)·hosts(호스트 한 줄씩)·seq(등록 번호)
#                           * 등록(-steps ...): -end 검사 뒤에 yml=fake_<steps>_<번호>ea.yml, hosts=대상 으로 덮어쓰고 "[등록] yml=<yml> hosts=<쉼표>" 줄을 찍는다
#                             기록: REG <steps> <ns> yml=<yml>
#                           * -yaml-state [-user u] [-conf f]: 현재 값을 "[yaml상태] key=value" 4줄로 찍는다(기록: YAMLSTATE <ns>). FAKE_YS_EXIT=<코드> 면 오류로 끝남
#   FAKE_YML_TAMPER=<steps> 이 steps 의 첫 등록 직후 다른 사람이 소스를 바꾼 것처럼 yml=os_install.yml, hosts=x,y,z 로 덮어쓴다(한 번만)
set -u

user=""
hostsf=""
steps=""
batch=0
conf=""
end=""
yamlstate=0
while [ $# -gt 0 ]; do
	case $1 in
		-yaml-state) yamlstate=1; shift ;;
		-user) user=$2; shift 2 ;;
		-hosts) hostsf=$2; shift 2 ;;
		-steps) steps=$2; shift 2 ;;
		-conf) conf=$2; shift 2 ;;
		-end) end=$2; shift 2 ;;
		-batch) batch=1; shift ;;
		*) echo "fake_stress_test: 알 수 없는 인자 $1" >&2; exit 2 ;;
	esac
done
if [ "$yamlstate" = 1 ]; then
	[ -n "${FAKE_ST_LOG:-}" ] && echo "YAMLSTATE $(date +%s%N)" >> "$FAKE_ST_LOG"
	if [ "${FAKE_YS_EXIT:-0}" != 0 ]; then
		echo "오류: 가짜 AWX 조회 실패" >&2
		exit "$FAKE_YS_EXIT"
	fi
	yml=$(cat "${FAKE_YML_DIR:-/nonexistent}/yml" 2> /dev/null)
	hs=$(sort "${FAKE_YML_DIR:-/nonexistent}/hosts" 2> /dev/null | paste -sd, -)
	n=0
	[ -n "$hs" ] && n=$(printf '%s' "$hs" | tr ',' '\n' | wc -l)
	echo "[yaml상태] yml=$yml"
	echo "[yaml상태] source=11 inventory=7"
	echo "[yaml상태] count=$n"
	echo "[yaml상태] hosts=$hs"
	exit 0
fi
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

log "START $steps $(now) user=$user hosts=$hl batch=$batch conf=${conf:-none} end=${end:-none} gossh=${GOSSH:-none}"
# 진짜 stress-test: -conf 로 준 파일이 없으면 종료코드 2
if [ -n "$conf" ] && [ ! -f "$conf" ]; then
	echo "오류: 지정한 설정 파일이 없습니다: $conf" >&2
	exit 2
fi
[ -n "${FAKE_ST_PIDFILE:-}" ] && echo "$$" >> "$FAKE_ST_PIDFILE"
echo "fake stress-test: steps=$steps hosts=$hl"

# 진짜 stress-test: steps 에 b 가 있으면 -end 필수(연 4자리, 월 1-12, 일 1-31, 시 0-23, 분 0-59)
case $steps in
	*b*)
		if ! printf '%s' "$end" | grep -Eq '^[0-9]{4},[0-9]{1,2},[0-9]{1,2},[0-9]{1,2},[0-9]{1,2}$'; then
			echo "fake_stress_test: -steps b 는 -end <연>,<월>,<일>,<시>,<분> 이 필요합니다(받은 값: '$end')" >&2
			exit 2
		fi
		;;
esac

# 등록(invsync) 흉내: 소스 yml·호스트를 이 묶음의 것으로 바꾸고 [등록] 줄을 찍는다(진짜는 템플릿 인벤토리 확인보다 먼저)
if [ -n "${FAKE_YML_DIR:-}" ]; then
	mkdir -p "$FAKE_YML_DIR"
	seq=$(cat "$FAKE_YML_DIR/seq" 2> /dev/null || echo 0)
	seq=$((seq + 1))
	echo "$seq" > "$FAKE_YML_DIR/seq"
	regyml="fake_${steps}_${seq}ea.yml"
	printf '%s\n' "$regyml" > "$FAKE_YML_DIR/yml"
	printf '%s\n' "${hosts[@]}" > "$FAKE_YML_DIR/hosts"
	echo "[등록] yml=$regyml hosts=$hl"
	log "REG $steps $(now) yml=$regyml"
	if [ "${FAKE_YML_TAMPER:-}" = "$steps" ] && [ ! -e "$FAKE_YML_DIR/tampered" ]; then
		: > "$FAKE_YML_DIR/tampered"
		echo os_install.yml > "$FAKE_YML_DIR/yml"
		printf '%s\n' x y z > "$FAKE_YML_DIR/hosts"
	fi
fi

MARK=${FAKE_MARK_DIR:-}
if [ -n "$MARK" ] && [ -e "$MARK/hang_$steps" ]; then
	# 진짜 stress-test 가 멈춘 것처럼 오래 잔다(프로세스 그룹 SIGTERM 으로 끝나는지 시험)
	sleep 600
	exit 0
fi

# 템플릿 인벤토리 사용 중 흉내: 줄을 찍고 stdin 에서 한 줄 읽기
case "${FAKE_ST_BUSY:-}" in
	*"$steps"*)
		times=${FAKE_ST_BUSY_TIMES:-1}
		while [ "$times" -gt 0 ]; do
			echo "[인벤토리사용중] 템플릿 stress(7) 인벤토리가 hong 입니다(DEFAULT 아님) — 그 인벤토리 사용자에게 DEFAULT 로 바꿔 달라고 요청하세요"
			log "BUSY $steps $(now)"
			[ -n "${FAKE_ST_BUSY_SLEEP:-}" ] && sleep "$FAKE_ST_BUSY_SLEEP"
			ans=""
			read -r ans || true
			log "BUSYANS $steps ${ans:-EOF}"
			if [ "$ans" != "r" ]; then
				echo "인벤토리 사용 중이라 이 묶음을 중단합니다(답: ${ans:-EOF})"
				exit 1
			fi
			times=$((times - 1))
		done
		;;
esac

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
