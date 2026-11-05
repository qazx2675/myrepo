#!/bin/bash
# new_setup.sh - OS 버전에 맞는 new_setup 바이너리를 골라 실행하는 래퍼 (bash 4.1 / RHEL6 호환)
#   RHEL/Rocky 6.x  -> bin/new_setup_os6
#   그 외           -> bin/new_setup
# 사용법은 바이너리와 같다:  new_setup.sh [-c <설정파일>] [-er <장애목록파일>] <호스트파일>
#                         new_setup.sh -run [-c <설정파일>] [-er <장애목록파일>] [-user <사용자>] <호스트파일>   (설정수정·CPU·stress-ng 자동 진행 TUI)

# 심볼릭 링크를 따라 실제 폴더를 찾는다
SELF=$0
while [ -h "$SELF" ]; do
	LINK=$(readlink "$SELF")
	case $LINK in
		/*) SELF=$LINK ;;
		*) SELF=$(dirname "$SELF")/$LINK ;;
	esac
done
DIR=$(cd "$(dirname "$SELF")" && pwd -P)

BIN=$DIR/bin/new_setup
if [ -r /etc/redhat-release ] && grep -qE 'release 6(\.|[[:space:]]|$)' /etc/redhat-release; then
	BIN=$DIR/bin/new_setup_os6
fi

if [ ! -x "$BIN" ]; then
	echo "new_setup: 실행 파일이 없습니다: $BIN" >&2
	echo "  -> build.sh 로 빌드하거나(os6 는 'build.sh os6'), 저장소에서 bin/ 폴더를 다시 받으세요." >&2
	exit 2
fi

# 첫 실행: 설정 파일(new_setup.conf)이 new_setup 이 찾는 곳(현재 경로·래퍼 폴더·bin/)에 없고,
# -c 도 없고, 터미널에서 실행 중이면 setup.sh 로 지금 만들지 묻는다. 터미널이 아니면(파이프·cron·리다이렉트) 조용히 기본값으로 진행한다.
# NEW_SETUP_NO_PROMPT=1 이면 묻지 않는다.
if [ "$#" -gt 0 ] && [ -z "${NEW_SETUP_NO_PROMPT:-}" ] && [ -t 0 ] && [ -t 2 ] && [ -f "$DIR/setup.sh" ]; then
	HAVE_C=0
	for A in "$@"; do
		case $A in
			-c | --c | -c=* | --c=* | -h | -help | --help) HAVE_C=1 ;;
		esac
	done
	if [ "$HAVE_C" -eq 0 ] && [ ! -f ./new_setup.conf ] && [ ! -f "$DIR/new_setup.conf" ] && [ ! -f "$DIR/bin/new_setup.conf" ]; then
		printf '설정 파일이 없습니다. 지금 설정할까요? (Y/n) ' >&2
		ANS=
		read -r ANS
		case $ANS in
			"" | [Yy] | [Yy][Ee][Ss])
				bash "$DIR/setup.sh" || echo "new_setup: 설정을 마치지 않았습니다. 기본값으로 계속합니다." >&2
				;;
		esac
	fi
fi

exec "$BIN" "$@"
