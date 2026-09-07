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
printf '  빌드: ldap-config-engine ... '
CGO_ENABLED=0 go build -o bin/ldap-config-engine ./cmd/ldap-config-engine
echo '-> bin/ldap-config-engine'

chmod +x scripts/*.sh
echo
echo '완료. 다음으로 진행하십시오:'
echo '  1) conf/ldap_config.conf.sample 을 conf/ldap_config.conf 로 복사해 실제 값으로 채우기'
echo '  2) conf/assets.txt 에 "hostname<TAB>site" 목록 채우기'
echo '  3) ./scripts/deploy_ldap.sh -infra <인프라명> -dry-run'
