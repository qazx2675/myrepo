#!/usr/bin/env bash
# setup.sh — 폐쇄망(오프라인) 정적 빌드 스크립트.
# 외부 네트워크 접속 없이 Go 표준 라이브러리만으로 정적 바이너리를 빌드합니다.
set -euo pipefail
cd "$(dirname "$0")"

if ! command -v go >/dev/null 2>&1; then
  if [ -x /usr/local/go/bin/go ]; then
    export PATH="$PATH:/usr/local/go/bin"
  elif [ -x /opt/go1.20/bin/go ]; then
    export PATH="/opt/go1.20/bin:$PATH"
  else
    echo "오류: go 를 찾을 수 없습니다. Go 툴체인을 먼저 설치하거나 내장된 bin/account-mount-engine 바이너리를 그대로 사용하십시오." >&2
    exit 2
  fi
fi

export GOPROXY=off
export GOFLAGS=-mod=mod
if [ -d vendor ]; then
  export GOFLAGS=-mod=vendor
fi

mkdir -p bin
printf '  빌드: account-mount-engine ... '
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o bin/account-mount-engine ./cmd/account-mount-engine
echo '-> bin/account-mount-engine'

chmod +x run.sh setup.sh 2>/dev/null || true
echo
echo '완료. 다음 순서로 진행하십시오:'
echo '  1) cp info.txt.example info.txt     (계정 UID/GID 스토리지 목록 작성)'
echo '  2) cp hosts.txt.example hosts.txt   (작업 대상 호스트 목록 작성)'
echo '  3) ./run.sh -dry-run                (변경 예정 사항 먼저 확인)'
echo '  4) ./run.sh                         (실제 일괄 적용)'

