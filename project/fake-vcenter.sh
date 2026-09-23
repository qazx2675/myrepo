#!/usr/bin/env bash
# fake-vcenter.sh — vm-ip-change 시험용 가짜 vCenter 실행 래퍼.
# bin/fake-vcenter 가 없으면 vendor/ 만으로(오프라인) 먼저 빌드하고, 인자는 그대로 넘깁니다.
#   ./fake-vcenter.sh -h            옵션 보기
#   ./fake-vcenter.sh -vms 1000     VM 1000대 시나리오
set -euo pipefail
cd "$(dirname "$0")"

if [ ! -x bin/fake-vcenter ]; then
  if ! command -v go >/dev/null 2>&1; then
    echo "오류: bin/fake-vcenter 가 없고 go 도 없습니다. ./setup.sh 를 먼저 실행하세요." >&2
    exit 2
  fi
  mkdir -p bin
  GOPROXY=off GOFLAGS=-mod=vendor go build -o bin/fake-vcenter ./cmd/fake-vcenter
fi

exec ./bin/fake-vcenter "$@"
