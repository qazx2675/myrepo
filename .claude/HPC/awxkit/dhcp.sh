#!/bin/bash
# [S3] 인프라를 선택해 DHCP 등록 템플릿을 실행하고 최종 상태를 출력한다.
# 바이너리가 없으면 자동으로 빌드한 뒤 실행한다.
# 사용 예: bash dhcp.sh -infra 1 / bash dhcp.sh (대화형)
set -e
cd "$(dirname "$0")"

if [ ! -x ./awxkit-dhcp ]; then
    echo "awxkit-dhcp 바이너리가 없어 먼저 빌드합니다..."
    bash setup.sh
fi

exec ./awxkit-dhcp "$@"
