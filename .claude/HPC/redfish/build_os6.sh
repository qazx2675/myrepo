#!/usr/bin/env bash
# build_os6.sh — os6(RHEL6) 용 정적 바이너리 빌드: bin/biostool_os6  (.60 에서 실행)
#
# RHEL6 커널(2.6.32)에서는 Go 1.24 이상으로 만든 바이너리가 동작하지 않으므로
# /opt/go1.20 의 Go 1.20.x 로 빌드해야 합니다.
#
# 사용법: bash build_os6.sh
set -euo pipefail
cd "$(dirname "$0")"

export PATH=/opt/go1.20/bin:$PATH

if ! command -v go >/dev/null 2>&1; then
  echo "오류: go 를 찾을 수 없습니다 (/opt/go1.20/bin 확인)." >&2
  exit 2
fi

gover="$(go version)"
echo "빌드: $gover"
case "$gover" in
  *" go1.20."*) ;;
  *)
    echo "경고: Go 1.20.x 가 아닙니다. 이 바이너리는 RHEL6(커널 2.6.32)에서 동작하지 않을 수 있습니다." >&2
    ;;
esac

mkdir -p bin
CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o bin/biostool_os6 ./cmd/biostool
echo "-> bin/biostool_os6"
