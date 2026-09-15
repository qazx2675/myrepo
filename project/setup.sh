#!/usr/bin/env bash
# setup.sh — 폐쇄망(오프라인) 빌드.
# 인터넷 접속을 시도하지 않고 저장소 안 vendor/ 만으로 빌드합니다.
set -euo pipefail
cd "$(dirname "$0")"

if ! command -v go >/dev/null 2>&1; then
  echo "오류: go 를 찾을 수 없습니다. Go 툴체인을 먼저 설치하십시오." >&2
  exit 2
fi

export GOPROXY=off
export GOFLAGS=-mod=vendor

mkdir -p bin
printf '  빌드: vm-ip-change ... '
go build -o bin/vm-ip-change ./cmd/vm-ip-change
echo '-> bin/vm-ip-change'

echo
echo '완료. 다음으로 진행하십시오:'
echo '  1) vcenter.txt.example 을 vcenter.txt 로 복사해 대상 vCenter 주소 채우기'
echo '  2) list.txt.example 을 list.txt 로 복사해 "호스트네임 새IP" 목록 채우기'
echo '  3) 환경변수 VC_USER / VC_PASSWORD / GUEST_USER / GUEST_PASSWORD 설정'
echo '  4) ./bin/vm-ip-change'
