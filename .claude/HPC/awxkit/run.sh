#!/bin/bash
# awxkit을 쉽게 실행하기 위한 래퍼 스크립트.
# 바이너리(awxkit)가 없으면 setup.sh로 먼저 빌드한 뒤, 이 스크립트에 넘긴 인자를 그대로 전달해 실행한다.
# 사용 예: bash run.sh doctor / bash run.sh nodeinfo / bash run.sh
set -e
cd "$(dirname "$0")"

if [ ! -x ./awxkit ]; then
    echo "awxkit 바이너리가 없어 먼저 빌드합니다..."
    bash setup.sh
fi

exec ./awxkit "$@"
