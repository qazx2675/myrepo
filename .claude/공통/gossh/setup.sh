#!/usr/bin/env bash
# setup.sh — 소스 빌드(인터넷이 되는 곳 전용).
# 폐쇄망에는 vendor/ 가 없으므로 이 스크립트로 빌드할 수 없다. 폐쇄망에서는 빌드하지 말고
# 저장소의 사전 빌드 바이너리(gossh_os6 = RHEL/CentOS 6, gossh_os8 = RHEL/Rocky 8)를 쓴다.
set -euo pipefail
cd "$(dirname "$0")"
go mod download
CGO_ENABLED=0 go build -o gossh .
echo "빌드 완료: $(pwd)/gossh"
