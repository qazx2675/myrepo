#!/bin/bash
# 폐쇄망 환경을 위한 AWX 조작 툴 빌드 스크립트
# cmd/ 아래의 단계별 하위 명령마다 독립된 awxkit-<단계명> 바이너리를 만든다.

echo "AWX 조작 툴 빌드를 시작합니다..."

# 의존성이 포함된 vendor 폴더를 사용하여 오프라인 빌드 진행
FAILED=0
for dir in cmd/*/; do
    name=$(basename "$dir")
    echo "  - awxkit-${name} 빌드 중..."
    GOFLAGS=-mod=vendor go build -o "awxkit-${name}" "./cmd/${name}"
    if [ $? -ne 0 ]; then
        echo "빌드 실패: awxkit-${name}"
        FAILED=1
    fi
done

if [ $FAILED -eq 0 ]; then
    echo "빌드 완료: awxkit-* 실행 파일들이 생성되었습니다."
else
    exit 1
fi
