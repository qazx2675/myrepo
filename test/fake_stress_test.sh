#!/bin/bash
# fake_stress_test.sh - new_setup -run 시험용 가짜 stress-test (진짜 -batch 모드 흉내)
# 사용: fake_stress_test.sh -user <u> -hosts <목록파일> -steps a|b -batch [-end y,m,d,h,min] [-conf <파일>] [-ignore-offline]
#   -ignore-offline 을 받으면 FAKE_ST_LOG 에 "IGNOREOFF <steps>" 줄을 남긴다(동작은 같음)
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
#   --- -auto 막힘 대응 시험용(모두 환경 변수로 켬, 안 켜면 출력·동작이 예전과 같음) ---
#   FAKE_ST_EVENTS=1        진짜 stress-test 의 이벤트 줄을 흉내: [감시] 줄 바로 앞에 "[job] stage=<a|b> id=<번호>"(번호는 $FAKE_MARK_DIR/jobseq),
#                           끝날 때 "[결과] stage=<a|b> done=<쉼표> notrun=<쉼표> nomon=<쉼표>"(stage 는 -steps 값). 없으면 new_setup 은 launch 시점 O 로 대체
#   FAKE_ST_NG_NOTRUN='h1,h2'  -steps b 에서 이 호스트는 [결과] 의 notrun 에 넣고 ng_<호스트> 표식을 만들지 않는다(부하가 안 오른 것). 나머지는 done
#   FAKE_ST_NG_NOTRUN_ONCE=1   위 notrun 을 호스트마다 첫 launch 한 번만 적용(두 번째부터 정상) → 재launch 뒤 O 가 되는 것을 시험. 안 켜면 계속 notrun(3회 뒤 포기)
#   FAKE_ST_EXCLUDE='h1,h2' 이 호스트는 launch 에서 뺀 것처럼 시작할 때 "[제외] stage=<s> hosts=<쉼표> reason=접속불가" 줄을 찍고 이후 감시·표식에서 제외
#                           (START 기록의 hosts= 에는 받은 목록 그대로 남김)
#   FAKE_ST_EXCLUDE_ONCE=1  위 [제외] 를 호스트마다 첫 launch 한 번만 적용(다음 launch 부터는 접속이 복구된 것으로 정상 처리)
#   FAKE_ST_ENV_FAIL=<n>    처음 n 번의 launch(-steps 가 a 든 b 든, $FAKE_MARK_DIR/envfail_count 로 셈)는 "오류: AWX 조회 실패(가짜)" 를 찍고 종료코드 2 로 끝남
#                           ([job]/[감시] 전 → 환경 실패). 기록: ENVFAIL <steps> <ns>
#   FAKE_ST_DELCHECK_FAIL=1 인벤토리 삭제 확인 중지 흉내: 삭제 전과 같은 호스트가 남았다는 오류를 찍고 종료코드 2([job]/[감시] 전, 등록도 안 함). 기록: DELCHECKFAIL <steps> <ns>
set -u

user=""
hostsf=""
steps=""
batch=0
conf=""
end=""
yamlstate=0
ignoreoff=0
while [ $# -gt 0 ]; do
	case $1 in
		-yaml-state) yamlstate=1; shift ;;
		-user) user=$2; shift 2 ;;
		-hosts) hostsf=$2; shift 2 ;;
		-steps) steps=$2; shift 2 ;;
		-conf) conf=$2; shift 2 ;;
		-end) end=$2; shift 2 ;;
		-batch) batch=1; shift ;;
		-ignore-offline) ignoreoff=1; shift ;;
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

hl_in=$hl
excluded=()
if [ -n "${FAKE_ST_EXCLUDE:-}" ]; then # 접속체크로 뺀 호스트: START 기록(받은 목록)에는 남기고 이후 감시·표식에서는 뺀다
	kept=()
	for h in ${hosts[@]+"${hosts[@]}"}; do
		case ",$FAKE_ST_EXCLUDE," in *",$h,"*) ;; *) kept+=("$h"); continue ;; esac
		if [ "${FAKE_ST_EXCLUDE_ONCE:-}" = 1 ] && [ -n "${FAKE_MARK_DIR:-}" ]; then # 호스트마다 첫 launch 한 번만 뺀다(다음엔 접속 복구)
			[ -e "$FAKE_MARK_DIR/excl_done_$h" ] && { kept+=("$h"); continue; }
			: > "$FAKE_MARK_DIR/excl_done_$h"
		fi
		excluded+=("$h")
	done
	hosts=(${kept[@]+"${kept[@]}"})
	hl=$(IFS=,; echo "${hosts[*]-}")
fi
csv() { local IFS=,; echo "$*"; }

now() { date +%s%N; }
log() { [ -n "${FAKE_ST_LOG:-}" ] && echo "$*" >> "$FAKE_ST_LOG"; return 0; }

log "START $steps $(now) user=$user hosts=$hl_in batch=$batch conf=${conf:-none} end=${end:-none} gossh=${GOSSH:-none}"
[ "$ignoreoff" = 1 ] && log "IGNOREOFF $steps"
# 진짜 stress-test: -conf 로 준 파일이 없으면 종료코드 2
if [ -n "$conf" ] && [ ! -f "$conf" ]; then
	echo "오류: 지정한 설정 파일이 없습니다: $conf" >&2
	exit 2
fi
[ -n "${FAKE_ST_PIDFILE:-}" ] && echo "$$" >> "$FAKE_ST_PIDFILE"
echo "fake stress-test: steps=$steps hosts=$hl_in"
[ "${#excluded[@]}" -gt 0 ] && echo "[제외] stage=$steps hosts=$(csv "${excluded[@]}") reason=접속불가"

# 진짜 stress-test: steps 에 b 가 있으면 -end 필수(연 4자리, 월 1-12, 일 1-31, 시 0-23, 분 0-59)
case $steps in
	*b*)
		if ! printf '%s' "$end" | grep -Eq '^[0-9]{4},[0-9]{1,2},[0-9]{1,2},[0-9]{1,2},[0-9]{1,2}$'; then
			echo "fake_stress_test: -steps b 는 -end <연>,<월>,<일>,<시>,<분> 이 필요합니다(받은 값: '$end')" >&2
			exit 2
		fi
		;;
esac

# 환경 실패 흉내(등록·[job]·[감시] 전에 끝남)
if [ -n "${FAKE_ST_ENV_FAIL:-}" ] && [ -n "${FAKE_MARK_DIR:-}" ]; then
	efn=$(cat "$FAKE_MARK_DIR/envfail_count" 2> /dev/null || echo 0)
	if [ "$efn" -lt "$FAKE_ST_ENV_FAIL" ]; then
		echo $((efn + 1)) > "$FAKE_MARK_DIR/envfail_count"
		log "ENVFAIL $steps $(now)"
		echo "오류: AWX 조회 실패(가짜)" >&2
		exit 2
	fi
fi
if [ "${FAKE_ST_DELCHECK_FAIL:-}" = 1 ]; then
	log "DELCHECKFAIL $steps $(now)"
	echo "오류: 인벤토리 삭제 확인 실패: 삭제 전과 같은 호스트가 남아 있습니다(가짜)" >&2
	exit 2
fi

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
if [ "${FAKE_ST_EVENTS:-}" = 1 ]; then
	jseq=$(cat "${MARK:-/nonexistent}/jobseq" 2> /dev/null || echo 0)
	jseq=$((jseq + 1))
	[ -n "$MARK" ] && echo "$jseq" > "$MARK/jobseq"
	echo "[job] stage=$steps id=$jseq"
fi
echo "[감시] $steps 경과 1/60초 기준(3) 미만 ${#hosts[@]}대: $hl"
log "MON $steps $(now)"

# stress-ng 부하가 안 오른 호스트(notrun): -steps b 에서만. ONCE 면 호스트마다 첫 launch 한 번만
notrun_l=()
if [ "$steps" = b ] && [ -n "${FAKE_ST_NG_NOTRUN:-}" ]; then
	for h in ${hosts[@]+"${hosts[@]}"}; do
		case ",$FAKE_ST_NG_NOTRUN," in *",$h,"*) ;; *) continue ;; esac
		if [ "${FAKE_ST_NG_NOTRUN_ONCE:-}" = 1 ] && [ -n "$MARK" ]; then
			[ -e "$MARK/notrun_done_$h" ] && continue
			: > "$MARK/notrun_done_$h"
		fi
		notrun_l+=("$h")
	done
fi

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
			for h in ${hosts[@]+"${hosts[@]}"}; do
				case " ${notrun_l[*]-} " in *" $h "*) continue ;; esac
				: > "$MARK/ng_$h"
			done
		fi
		sleep 1
		;;
esac
if [ "${FAKE_ST_EVENTS:-}" = 1 ]; then
	done_l=()
	for h in ${hosts[@]+"${hosts[@]}"}; do
		case " ${notrun_l[*]-} " in *" $h "*) ;; *) done_l+=("$h") ;; esac
	done
	echo "[결과] stage=$steps done=$(csv ${done_l[@]+"${done_l[@]}"}) notrun=$(csv ${notrun_l[@]+"${notrun_l[@]}"}) nomon="
fi
log "END $steps $(now)"
exit "${FAKE_ST_EXIT:-0}"
