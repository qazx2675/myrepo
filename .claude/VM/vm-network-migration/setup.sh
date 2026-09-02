#!/usr/bin/env bash
# setup.sh — 폐쇄망(오프라인) 빌드.
# vendor/ 안의 의존성만 사용하며 인터넷 접속을 시도하지 않습니다.
set -euo pipefail
cd "$(dirname "$0")"

if ! command -v go >/dev/null 2>&1; then
  echo "오류: go 를 찾을 수 없습니다. Go 툴체인을 먼저 설치하세요." >&2
  exit 2
fi

if [ ! -d vendor ]; then
  echo "오류: vendor/ 디렉터리가 없습니다. 저장소를 통째로 내려받았는지 확인하세요." >&2
  exit 2
fi

mkdir -p bin

# 단계별로 독립된 바이너리를 만듭니다. run.sh 는 bin/ 아래에서 이들을 찾습니다.
BINS="backup pgcreate disconnect connect verify rollback"

export GOFLAGS=-mod=vendor
export GOPROXY=off

for b in $BINS; do
  printf '  빌드: nm-%-12s' "$b"
  go build -mod=vendor -o "bin/nm-$b" "./cmd/$b"
  echo "-> bin/nm-$b"
done

echo
echo "빌드 완료: $(pwd)/bin"
ls -1 bin
