#!/bin/bash
# install_mn.sh - install_mn 실행 래퍼 (OS 설치 진행 모니터링 TUI)
# 사용법: install_mn.sh [-c <설정파일>] <호스트파일>
# 같은 폴더의 install_mn_os8 을 실행한다. 없으면 go 가 있을 때 build.sh 로 만든다.
# /usr/local/bin/install_mn 으로 심볼릭 링크해서 써도 된다.

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
BIN=$DIR/install_mn_os8

if [ ! -x "$BIN" ]; then
	if command -v go >/dev/null 2>&1 || [ -x /opt/go1.20/bin/go ] || [ -x /usr/local/go/bin/go ]; then
		echo "install_mn: 실행 파일이 없어 build.sh 로 빌드합니다." >&2
		bash "$DIR/build.sh" >&2 || exit 1
	fi
fi
if [ ! -x "$BIN" ]; then
	echo "install_mn: 실행 파일이 없습니다: $BIN" >&2
	echo "  -> Go 가 있는 서버에서 build.sh 로 빌드하거나, 저장소에서 install_mn_os8 을 다시 받으세요." >&2
	exit 2
fi

# 인자 없음(사용법 출력)은 root 가 아니어도 바이너리가 처리하게 둔다.
if [ "$#" -gt 0 ] && [ "$(id -u)" -ne 0 ]; then
	echo "install_mn: raw ICMP(ping)를 쓰므로 root 로 실행하세요. 예: sudo install_mn <호스트파일>" >&2
	exit 1
fi

exec "$BIN" "$@"
