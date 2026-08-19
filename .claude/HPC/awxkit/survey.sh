#!/bin/bash
# 템플릿의 survey 정의(변수명·선택지)를 조회한다. 바이너리가 없으면 자동으로 빌드한 뒤 실행한다.
# 사용 예: bash survey.sh pxe-register / bash survey.sh 24 / bash survey.sh (대화형)
set -e
cd "$(dirname "$0")"

if [ ! -x ./awxkit-survey ]; then
    echo "awxkit-survey 바이너리가 없어 먼저 빌드합니다..."
    bash setup.sh
fi

exec ./awxkit-survey "$@"
