#!/bin/bash
# fake_ssh.sh - new_setup STRESS_REMOTE(B서버 원격 실행) 시험용 가짜 ssh. PATH 맨 앞에 'ssh' 이름으로 두고 쓴다.
# 옵션(-o 값, -T 등)과 대상(root@B)은 무시하고 나머지 인자(원격 명령)를 이 서버에서 sh -c 로 실행한다.
#   FAKE_SSH_LOG=<파일>  호출 기록. 줄 형식:
#                        CALL <대상> <원격 명령>
#                        ASKPASS ok|fail|none   (SSH_ASKPASS_REQUIRE=force 이고 SSH_ASKPASS 가 비밀번호를 내면 ok — 비밀번호 자체는 남기지 않음)
#   FAKE_SSH_EXIT=<코드> 이 값이면 원격 명령을 돌리지 않고 그 코드로 끝난다(255 = 접속 실패 흉내)
set -u

target=""
while [ $# -gt 0 ]; do
	case $1 in
		-o | -p | -i | -l | -F) shift 2 ;;
		-*) shift ;;
		*) target=$1; shift; break ;;
	esac
done
cmd="$*"

log() { [ -n "${FAKE_SSH_LOG:-}" ] && printf '%s\n' "$*" >> "$FAKE_SSH_LOG"; return 0; }
log "CALL $target $cmd"

# 진짜 ssh 처럼 SSH_ASKPASS 로 비밀번호를 받아 본다(제어 터미널 없이 askpass 를 쓰는지 확인)
if [ "${SSH_ASKPASS_REQUIRE:-}" = force ] && [ -n "${SSH_ASKPASS:-}" ]; then
	pw=$("$SSH_ASKPASS" "$target's password: " 2> /dev/null)
	if [ -n "$pw" ]; then log "ASKPASS ok"; else log "ASKPASS fail"; fi
else
	log "ASKPASS none"
fi

if [ -n "${FAKE_SSH_EXIT:-}" ]; then
	echo "ssh: connect to host ${target#*@}: Connection refused" >&2
	exit "$FAKE_SSH_EXIT"
fi

# 원격 쪽은 ssh 의 환경변수를 받지 않는다(askpass 표식이 stress-test 에 넘어가지 않게)
unset SSH_ASKPASS SSH_ASKPASS_REQUIRE NEW_SETUP_ASKPASS
exec sh -c "$cmd"
