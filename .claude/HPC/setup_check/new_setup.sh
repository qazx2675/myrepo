#!/bin/bash
# new_setup.sh - OS 버전에 맞는 new_setup 바이너리를 골라 실행하는 래퍼 (bash 4.1 / RHEL6 호환)
#   RHEL/Rocky 6.x  -> bin/new_setup_os6
#   그 외           -> bin/new_setup
# 사용법은 바이너리와 같다:  new_setup.sh [-c <설정파일>] <호스트파일>

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

exec "$BIN" "$@"
