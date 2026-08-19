#!/bin/bash
# [S1] ${user}.txt의 모든 hostname을 한 번에 넣어 NodeInfo 템플릿을 실행하고 결과를 저장한다.
# 바이너리가 없으면 자동으로 빌드한 뒤 실행한다.
# 사용 예: bash nodeinfo.sh / bash nodeinfo.sh -hosts ./retry_list.txt
set -e
cd "$(dirname "$0")"

if [ ! -x ./awxkit-nodeinfo ]; then
    echo "awxkit-nodeinfo 바이너리가 없어 먼저 빌드합니다..."
    bash setup.sh
fi

exec ./awxkit-nodeinfo "$@"
