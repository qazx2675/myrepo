#!/usr/bin/env bash
# setup.sh — 폐쇄망(오프라인) 빌드.
# 인터넷 접속을 시도하지 않고 저장소 안의 것만으로 빌드합니다.
set -euo pipefail
cd "$(dirname "$0")"

if ! command -v go >/dev/null 2>&1; then
  echo "오류: go 를 찾을 수 없습니다. Go 툴체인을 먼저 설치하십시오." >&2
  exit 2
fi

export GOPROXY=off
export GOFLAGS=-mod=mod
# 이 프로젝트는 표준 라이브러리만 사용하므로 vendor/ 가 없어도 오프라인 빌드됩니다.
if [ -d vendor ]; then
  export GOFLAGS=-mod=vendor
fi

mkdir -p bin
printf '  빌드: ip-change-engine ... '
CGO_ENABLED=0 go build -o bin/ip-change-engine ./cmd/ip-change-engine
echo '-> bin/ip-change-engine'

chmod +x scripts/*.sh
echo
echo '완료. 다음으로 진행하십시오:'
echo '  1) conf/ip_change.conf.sample 을 conf/ip_change.conf 로 복사해 필요 시 값 채우기 (없어도 기본값으로 동작)'
echo '  2) conf/testuser.txt.sample 을 conf/<계정명>.txt 로 복사해 "hostname 변경될ip" 목록 채우기'
echo '  3) scripts/run_ip_change.sh 의 select_user_context() 를 실제 계정 목록으로 채우기'
echo '  4) ./scripts/run_ip_change.sh'
