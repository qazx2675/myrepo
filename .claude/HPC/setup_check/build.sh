#!/bin/bash
# build.sh - new_setup 빌드
#   ./build.sh       로컬 go 로 bin/new_setup (linux/amd64) 빌드
#   ./build.sh os6   192.168.0.60 의 /opt/go1.20/bin/go 로 bin/new_setup_os6 빌드 (ssh root 필요)
set -eu
cd "$(dirname "$0")"
mkdir -p bin

if [ "${1:-}" = "os6" ]; then
	HOST=root@192.168.0.60
	GO=/opt/go1.20/bin/go
	REMOTE=/root/work/new_setup_build
	echo "[os6] 소스를 $HOST:$REMOTE 로 복사"
	ssh "$HOST" "rm -rf $REMOTE && mkdir -p $REMOTE"
	tar czf - main.go go.mod | ssh "$HOST" "tar xzf - -C $REMOTE"
	echo "[os6] 원격 빌드"
	# 원격 root 셸이 csh 일 수 있어(랩 .60) VAR=값 접두어가 안 먹으므로 sh -c 로 감싼다
	ssh "$HOST" "sh -c 'cd $REMOTE && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 $GO build -o new_setup_os6 .'"
	echo "[os6] 결과를 bin/new_setup_os6 로 가져오기"
	scp "$HOST:$REMOTE/new_setup_os6" bin/new_setup_os6
	chmod +x bin/new_setup_os6
	echo "완료: bin/new_setup_os6"
	exit 0
fi

CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o bin/new_setup .
chmod +x bin/new_setup
echo "완료: bin/new_setup"
