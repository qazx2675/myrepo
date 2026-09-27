#!/usr/bin/env bash
# setup.sh — 폐쇄망(오프라인) 빌드. vendor/ 안의 의존성만 사용, 인터넷 접속 시도 안 함.
set -euo pipefail
cd "$(dirname "$0")"
go build -mod=vendor -o vm-portal ./cmd/server
echo "빌드 완료: $(pwd)/vm-portal"
