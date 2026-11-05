#!/bin/bash
# 연결/설정/권한 점검. 바이너리가 없으면 자동으로 빌드한 뒤 실행한다.
# 사용 예: bash doctor.sh / bash doctor.sh -user hong
set -e
cd "$(dirname "$0")"

if [ ! -x ./awxkit-doctor ]; then
    echo "awxkit-doctor 바이너리가 없어 먼저 빌드합니다..."
    bash setup.sh
fi

exec ./awxkit-doctor "$@"
