#!/bin/bash
# 폐쇄망 환경을 위한 AWX 조작 툴 빌드 스크립트

echo "AWX 조작 툴 빌드를 시작합니다..."

# 의존성이 포함된 vendor 폴더를 사용하여 오프라인 빌드 진행
GOFLAGS=-mod=vendor go build -o awxkit main.go

if [ $? -eq 0 ]; then
    echo "빌드 완료: awxkit 실행 파일이 생성되었습니다."
else
    echo "빌드 실패!"
    exit 1
fi
