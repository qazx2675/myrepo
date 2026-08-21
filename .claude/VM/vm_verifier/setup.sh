#!/usr/bin/env bash
# setup.sh — vm-verifier 빌드
set -euo pipefail
cd "$(dirname "$0")"
go build -o vm-verifier .
echo "빌드 완료: $(pwd)/vm-verifier"
