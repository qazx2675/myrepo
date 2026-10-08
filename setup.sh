#!/usr/bin/env bash
# setup.sh — 폐쇄망(오프라인) 빌드. 외부 의존성이 없어(표준 라이브러리만) vendor/ 에 받을 패키지가 없고, 인터넷 접속 시도 안 함.
set -euo pipefail
cd "$(dirname "$0")"

if ! command -v go >/dev/null 2>&1; then
  echo "오류: go 를 찾을 수 없습니다. Go 툴체인을 먼저 설치하십시오." >&2
  echo "      (Go 없이 쓰려면 저장소의 빌드 완료 바이너리 bios_compare 를 그대로 사용하십시오.)" >&2
  exit 2
fi

export GOPROXY=off
export GOFLAGS=-mod=vendor

echo "빌드: $(go version)"
CGO_ENABLED=0 go build -mod=vendor -trimpath -ldflags "-s -w" -o bios_compare .

echo
echo '완료. 다음으로 진행하십시오:'
echo '  1) out/<일시>/ 에 실제조사 hostname.json 배치 (collect 도구 결과)'
echo '  2) list.txt 에 hostname [model] 입력'
echo '  3) bash 조사.sh'
