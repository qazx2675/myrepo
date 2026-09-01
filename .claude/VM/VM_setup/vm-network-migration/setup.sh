#!/usr/bin/env bash
# setup.sh — 폐쇄망(오프라인) 빌드 스크립트.
# vendor/ 안의 의존성만 사용하므로 인터넷에 접속하지 않습니다.
set -euo pipefail
cd "$(dirname "$0")"

if ! command -v go >/dev/null 2>&1; then
    echo "오류: go 명령을 찾을 수 없습니다. Go 1.21 이상을 설치한 뒤 다시 실행하세요." >&2
    exit 1
fi

export GOFLAGS="-mod=vendor"
export GOPROXY=off

go build -mod=vendor -o vm-network-migration .
echo "빌드 완료: $(pwd)/vm-network-migration"
