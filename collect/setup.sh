#!/usr/bin/env bash
# setup.sh — 폐쇄망(오프라인) 빌드. 외부 의존성이 없어(표준 라이브러리만) vendor/ 에 받을 패키지가 없고, 인터넷 접속 시도 안 함.
set -euo pipefail
cd "$(dirname "$0")"

if ! command -v go >/dev/null 2>&1; then
  echo "오류: go 를 찾을 수 없습니다. Go 툴체인을 먼저 설치하십시오." >&2
  echo "      (Go 없이 쓰려면 저장소의 빌드 완료 바이너리 biosdump 를 그대로 사용하십시오.)" >&2
  exit 2
fi

export GOPROXY=off
export GOFLAGS=-mod=vendor

echo "빌드: $(go version)"
CGO_ENABLED=0 go build -mod=vendor -trimpath -ldflags "-s -w" -o biosdump .

# RHEL6(커널 3 미만)에서는 Go 1.24 이상으로 만든 바이너리가 동작하지 않는다.
kmajor="$(uname -r | cut -d. -f1)"
case "$kmajor" in '' | *[!0-9]*) kmajor=99 ;; esac
if [ "$kmajor" -lt 3 ]; then
  echo "주의: 이 시스템은 커널 3 미만(RHEL6)입니다. Go 1.20.x 로 빌드한 뒤 biosdump_os6 이름으로 복사하십시오:" >&2
  echo "      cp biosdump biosdump_os6" >&2
fi

echo
echo '완료. 다음으로 진행하십시오:'
echo '  1) collect.sh 맨 위의 USER_ID / USER_PW 에 BMC 계정 입력'
echo '  2) list.txt 에 hostname 입력 (한 줄에 하나)'
echo '  3) bash collect.sh'
