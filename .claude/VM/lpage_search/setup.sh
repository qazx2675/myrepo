#!/usr/bin/env bash
# setup.sh — 폐쇄망(오프라인) 빌드. 외부 의존성이 없어(Go 표준 라이브러리만 사용)
# 인터넷 접속 없이 go.mod만으로 바로 빌드된다.
set -euo pipefail
cd "$(dirname "$0")"
go build -o lpage_search main.go
echo "빌드 완료: $(pwd)/lpage_search"
