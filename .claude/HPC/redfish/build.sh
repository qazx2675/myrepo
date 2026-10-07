#!/usr/bin/env bash
# build.sh — os8(RHEL8) 용 정적 바이너리 빌드: bin/biostool
#
# 사용법: bash build.sh        (RHEL6 용은 build_os6.sh)
set -euo pipefail
cd "$(dirname "$0")"

if ! command -v go >/dev/null 2>&1; then
  echo "오류: go 를 찾을 수 없습니다. 빌드 완료 바이너리(bin/biostool)를 사용하십시오." >&2
  exit 2
fi

mkdir -p bin
echo "빌드: $(go version)"
CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o bin/biostool ./cmd/biostool
echo "-> bin/biostool"
