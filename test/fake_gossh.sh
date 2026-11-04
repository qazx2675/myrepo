#!/bin/bash
# fake_gossh.sh - new_setup 시험용 가짜 gossh
#   FAKE_GOSSH_SCENARIO=<파일>  각 줄 "host<TAB>시나리오" (없는 호스트는 ok)
#   FAKE_GOSSH_LOG=<파일>       (선택) 받은 호출과 원격 명령을 이어 붙여 기록
# 시나리오: ok off refused install_disk install_err install_none install_nolog install_retry_off cancel
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

scn() { # 호스트의 시나리오 (기본 ok)
	local v=""
	if [ -n "${FAKE_GOSSH_SCENARIO:-}" ] && [ -f "$FAKE_GOSSH_SCENARIO" ]; then
		v=$(awk -F'\t' -v h="$1" '$1==h {print $2; exit}' "$FAKE_GOSSH_SCENARIO")
	fi
	echo "${v:-ok}"
}

while IFS= read -r line || [ -n "$line" ]; do
	h=$(printf '%s' "$line" | sed 's/^[[:space:]]*//; s/[[:space:]]*$//')
	case $h in '' | '#'*) continue ;; esac
	s=$(scn "$h")
	if [ "$pm" = 1 ]; then
		case $s in
			ok) echo "$h:  13:00:00 up 1 day, load average: 0.00, 0.01, 0.05" ;;
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
