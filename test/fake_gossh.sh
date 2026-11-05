#!/bin/bash
# fake_gossh.sh - new_setup 시험용 가짜 gossh
#   FAKE_GOSSH_SCENARIO=<파일>  각 줄 "host<TAB>시나리오[<TAB>key=value ...]" (없는 호스트는 ok)
#   FAKE_GOSSH_LOG=<파일>       (선택) 받은 호출과 원격 명령을 이어 붙여 기록
# 시나리오: ok off refused install_disk install_err install_none install_nolog install_retry_off cancel
# 시나리오 파일 3번째 열 이후(탭/공백 구분)에 key=value 로 ok 장비의 1차 정보수집 출력을 흉내 낸다.
#   vendor=intel|amd|none  cpu=ok|ng|nolog  stress=1|0  tpc=1|2|none  kernel=<값>
#   기본: vendor=intel cpu=ok stress=1 tpc=2 kernel=4.18.0-test
#   FAKE_MARK_DIR=<폴더>  (선택) cpu_<호스트>/ng_<호스트> 파일이 있으면 그 호스트의 CPU/stress-ng 를 성공으로, reboot_<호스트> 가 있으면
#   "grep btime" 명령에 다른 부팅 시각을 낸다(-run 시험용).
#   NS_CPU_INTEL / NS_CPU_AMD 는 받은 원격 명령에 해당 표식이 있을 때만 낸다(진짜 grep 처럼).
# 호출에 -pm 이 있으면 1차, 없으면 2차로 본다. 진짜 gossh 와 같이 시작할 때 결과 파일 5개를 지우고
# 내용이 있는 것만 만든다. 호스트 목록은 한 줄에 하나(빈 줄·# 무시, 범위 확장은 흉내 내지 않음).
set -u

pm=0
hf=""
cmd=""
while [ $# -gt 0 ]; do
	case $1 in
		-pm) pm=1; shift ;;
		-script) shift ;;
		-w) hf=${2:-}; shift 2 ;;
		-w?*) hf=${1#-w}; shift ;;
		-u | -i | -p | -P | -c | -t) shift 2 ;;
		-*) shift ;;
		*) cmd=$1; shift ;;
	esac
done
hf=${hf#^}
if [ -z "$hf" ] || [ ! -f "$hf" ]; then
	echo "fake_gossh: -w 호스트 목록 파일 없음: $hf" >&2
	exit 1
fi

# 기록 (2차의 원격 명령 내용을 시험에서 확인하려고)
if [ -n "${FAKE_GOSSH_LOG:-}" ]; then
	pass=2
	[ "$pm" = 1 ] && pass=1
	{
		echo "=== CALL pass=$pass hostfile=$hf"
		printf '%s\n' "$cmd"
	} >> "$FAKE_GOSSH_LOG"
fi

# 시작할 때 결과 파일 5개 삭제 (진짜 gossh 동작)
for s in _res_off _res_refsed _os_install _nosvrauto _res_cancel; do rm -f "${hf}${s}"; done

SCN_S=ok
SCN_OPTS=()
scn() { # 호스트의 시나리오(기본 ok)는 SCN_S, 3열 이후 key=value 는 SCN_OPTS 에 담는다
	local l="" a=()
	SCN_S=ok
	SCN_OPTS=()
	if [ -n "${FAKE_GOSSH_SCENARIO:-}" ] && [ -f "$FAKE_GOSSH_SCENARIO" ]; then
		l=$(awk -v h="$1" '$1==h {print; exit}' "$FAKE_GOSSH_SCENARIO")
	fi
	read -r -a a <<< "$l"
	SCN_S=${a[1]:-ok}
	[ "${#a[@]}" -gt 2 ] && SCN_OPTS=("${a[@]:2}")
	return 0
}

info() { # 호스트 [key=value ...]: ok 장비의 1차 정보수집 출력
	local h=$1 kv vendor=intel cpu=ok stress=1 tpc=2 kernel=4.18.0-test
	shift
	for kv in "$@"; do
		case $kv in
			vendor=*) vendor=${kv#vendor=} ;;
			cpu=*) cpu=${kv#cpu=} ;;
			stress=*) stress=${kv#stress=} ;;
			tpc=*) tpc=${kv#tpc=} ;;
			kernel=*) kernel=${kv#kernel=} ;;
		esac
	done
	# FAKE_MARK_DIR: 시험 중 상태 변화 표식 (cpu_<호스트>=CPU 성공, ng_<호스트>=stress-ng 성공, reboot_<호스트>=btime 변경)
	local bt=1700000000
	if [ -n "${FAKE_MARK_DIR:-}" ]; then
		[ -e "$FAKE_MARK_DIR/cpu_$h" ] && cpu=ok
		[ -e "$FAKE_MARK_DIR/ng_$h" ] && stress=1
		[ -e "$FAKE_MARK_DIR/reboot_$h" ] && bt=1700009999
	fi
	case $cmd in *"grep btime"*) echo "$h: btime $bt" ;; esac
	echo "$h:  13:00:00 up 1 day, load average: 0.00, 0.01, 0.05"
	case $vendor in
		intel) printf '%s: vendor_id\t: GenuineIntel\n' "$h" ;;
		amd) printf '%s: vendor_id\t: AuthenticAMD\n' "$h" ;;
	esac
	if [ "$cpu" = ok ]; then
		case $vendor in
			intel) case $cmd in *NS_CPU_INTEL*) echo "$h: NS_CPU_INTEL" ;; esac ;;
			amd) case $cmd in *NS_CPU_AMD*) echo "$h: NS_CPU_AMD" ;; esac ;;
		esac
	fi
	# cpu=nolog: CPU 테스트 로그 파일 없음 (진짜 명령의 "[ -f 로그 ] || echo <표식>_NOLOG" 흉내)
	if [ "$cpu" = nolog ]; then
		case $vendor in
			intel) case $cmd in *NS_CPU_INTEL_NOLOG*) echo "$h: NS_CPU_INTEL_NOLOG" ;; esac ;;
			amd) case $cmd in *NS_CPU_AMD_NOLOG*) echo "$h: NS_CPU_AMD_NOLOG" ;; esac ;;
		esac
	fi
	if [ "$stress" = 1 ]; then echo "$h: NS_STRESS"; fi
	if [ "$tpc" != none ]; then echo "$h: Thread(s) per core:    $tpc"; fi
	echo "$h: NS_KERNEL $kernel"
}

while IFS= read -r line || [ -n "$line" ]; do
	h=$(printf '%s' "$line" | sed 's/^[[:space:]]*//; s/[[:space:]]*$//')
	case $h in '' | '#'*) continue ;; esac
	scn "$h"
	s=$SCN_S
	if [ "$pm" = 1 ]; then
		case $s in
			ok) info "$h" ${SCN_OPTS[@]+"${SCN_OPTS[@]}"} ;;
			off) echo "$h: SSH 접속 실패: i/o timeout" >&2; echo "$h" >> "${hf}_res_off" ;;
			refused) echo "$h: dial tcp: connection refused" >&2; echo "$h" >> "${hf}_res_refsed" ;;
			cancel) echo "$h: 취소됨" >&2; echo "$h" >> "${hf}_res_cancel" ;;
			install_*) echo "$h: OS 설치중 (~/.profile anaconda 감지)" >&2; echo "$h" >> "${hf}_os_install" ;;
		esac
	else
		case $s in
			install_disk) echo "$h: NS_DISK"; echo "$h: NS_DONE" ;;
			install_err) case $cmd in *NS_ERR*) echo "$h: NS_ERR" ;; esac; echo "$h: NS_DONE" ;;  # 명령에 grep 이 없으면 진짜처럼 안 나옴
			install_nolog) echo "$h: NS_NOLOG"; echo "$h: NS_DONE" ;;
			install_none) echo "$h: NS_DONE" ;;
			install_retry_off) echo "$h: SSH 접속 실패: i/o timeout" >&2; echo "$h" >> "${hf}_res_off" ;;
		esac
	fi
done < "$hf"
exit 0
