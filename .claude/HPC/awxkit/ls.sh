#!/bin/bash
# 조회 가능한 템플릿 목록을 출력한다. 바이너리가 없으면 자동으로 빌드한 뒤 실행한다.
# 사용 예: bash ls.sh
set -e
cd "$(dirname "$0")"

if [ ! -x ./awxkit-ls ]; then
    echo "awxkit-ls 바이너리가 없어 먼저 빌드합니다..."
    bash setup.sh
fi

exec ./awxkit-ls "$@"
