#!/usr/bin/env bash
# setup.sh — 폐쇄망(오프라인) 빌드. vendor/ 안의 의존성만 사용, 인터넷 접속 시도 안 함.
set -euo pipefail
cd "$(dirname "$0")"

# 공통 govendor 로 이동한 의존성을 이 디렉터리 이름의 vendor/ 로 연결한다
# (실제 파일은 ../../../공통/govendor/govmomi-0.55.1-standard 에 있음; 여러 프로젝트가 동일 govmomi 버전을 공유)
rm -rf vendor 2>/dev/null; ln -s "../../../공통/govendor/govmomi-0.55.1-standard" vendor
go build -mod=vendor -o tag_setting main.go
echo "빌드 완료: $(pwd)/tag_setting"
