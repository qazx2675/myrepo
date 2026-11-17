#!/usr/bin/env bash
# setup.sh — 폐쇄망(오프라인) 빌드. vendor/ 안의 의존성만 사용, 인터넷 접속 시도 안 함.
# 이 프로젝트는 표준 라이브러리만 쓰므로 vendor/ 가 비어 있어도 된다.
set -euo pipefail
cd "$(dirname "$0")"

if ! command -v go >/dev/null 2>&1; then
  echo "오류: go 를 찾을 수 없습니다. Go 툴체인을 먼저 설치하십시오." >&2
  exit 2
fi

export GOPROXY=off
export GOFLAGS=-mod=vendor
export CGO_ENABLED=0

mkdir -p bin
go build -mod=vendor -trimpath -ldflags "-s -w" -o bin/awx_copy .
chmod 755 bin/awx_copy

echo
echo '완료. 다음으로 진행하십시오:'
echo '  1) sudo ./install.sh                      # /usr/local/bin/awx_copy 설치'
echo '  2) 사용자 설정 배포: /etc/awx_copy/{user}.conf (소유자=해당 사용자, 600)'
echo '  3) awx_copy -u {user} {파일}              # 업로드'
echo
echo '주의: 설정 변경 뒤에는 랜덤한 서버 몇 대를 직접 확인해 실제로 변경되었는지 확인하십시오.'
