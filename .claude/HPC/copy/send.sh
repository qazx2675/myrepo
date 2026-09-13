#!/bin/bash
# send.sh — {user}_copy.txt 를 AWX로 전송. 바이너리가 없으면 먼저 빌드한다.
# 사용 예: bash send.sh -user hong / bash send.sh -file mytext.txt
set -e
cd "$(dirname "$0")"

if [ ! -x ./bin/copy-send ]; then
    echo "copy-send 바이너리가 없어 먼저 빌드합니다..."
    bash setup.sh
fi

exec ./bin/copy-send "$@"
