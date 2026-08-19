#!/bin/bash
# [S4] 인프라·OS 버전·Boot Mode·Splunk 설치 여부를 선택해 PXE 등록 템플릿을 실행하고
# 완료 후 등록된 호스트 수를 리포트한다. 바이너리가 없으면 자동으로 빌드한 뒤 실행한다.
# 사용 예: bash pxe.sh -infra 1 -os rocky-9.2 -boot uefi -splunk true / bash pxe.sh (대화형)
set -e
cd "$(dirname "$0")"

if [ ! -x ./awxkit-pxe ]; then
    echo "awxkit-pxe 바이너리가 없어 먼저 빌드합니다..."
    bash setup.sh
fi

exec ./awxkit-pxe "$@"
