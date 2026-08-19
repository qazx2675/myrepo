#!/bin/bash
# [S2] 인벤토리 소스 동기화를 트리거하고 등록된 호스트 목록을 보여준다.
# 바이너리가 없으면 자동으로 빌드한 뒤 실행한다.
# 사용 예: bash invsync.sh
set -e
cd "$(dirname "$0")"

if [ ! -x ./awxkit-invsync ]; then
    echo "awxkit-invsync 바이너리가 없어 먼저 빌드합니다..."
    bash setup.sh
fi

exec ./awxkit-invsync "$@"
