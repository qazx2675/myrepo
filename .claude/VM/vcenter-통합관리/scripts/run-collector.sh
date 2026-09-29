#!/usr/bin/env bash
# 랩/Linux 용 수집기 실행 래퍼. bin/linux/vcportal-collector 가 없으면 go build 로 만든 뒤 인자를 그대로 전달한다.
#   ./scripts/run-collector.sh --conf /path/vcportal.conf [--check]
set -euo pipefail
root="$(cd "$(dirname "$0")/.." && pwd)"
bin="$root/bin/linux/vcportal-collector"

if [ ! -x "$bin" ]; then
  echo "vcportal-collector 를 빌드합니다..."
  mkdir -p "$root/bin/linux"
  (cd "$root" && CGO_ENABLED=0 go build -mod=vendor -trimpath -o bin/linux/vcportal-collector ./cmd/vcportal-collector)
fi
exec "$bin" "$@"
