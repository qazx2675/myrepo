#!/usr/bin/env bash
# setup.sh — vm-verifier 폐쇄망(오프라인) 빌드. vendor/ 안의 의존성만 사용, 인터넷 접속 시도 안 함.
# 주의: vendor/ 디렉토리가 없으면 이 스크립트는 실패한다. 인터넷 되는 PC에서
#   go mod vendor
# 를 먼저 실행해 vendor/를 만들고, 그걸 이 폴더와 함께 폐쇄망으로 옮겨야 한다.
set -euo pipefail
cd "$(dirname "$0")"
if [ ! -d vendor ]; then
  echo "vendor/ 없음 — 인터넷 되는 PC에서 'go mod vendor' 실행 후 vendor/를 이 폴더로 옮기세요." >&2
  exit 1
fi
go build -mod=vendor -o vm-verifier .
echo "빌드 완료: $(pwd)/vm-verifier"
