#!/bin/bash
# hpcbot — 빌드 스크립트 (manual.md 확인 → vet/test → 정적 빌드 → dist/hpcbot)
set -euo pipefail
cd "$(dirname "$0")"
export GOTOOLCHAIN="${GOTOOLCHAIN:-go1.26.5}"

if [ ! -f data/manual.md ]; then
  echo "[오류] data/manual.md 가 없습니다." >&2
  echo "       매뉴얼 원문(server_operations_manual.md)을 data/manual.md 로 복사한 뒤 다시 실행하세요." >&2
  echo "       (공개 저장소라 매뉴얼은 커밋되지 않습니다)" >&2
  exit 1
fi

go vet ./...
go test ./...
mkdir -p dist
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags "-s -w" -o dist/hpcbot .
echo "빌드 완료: $(pwd)/dist/hpcbot"
