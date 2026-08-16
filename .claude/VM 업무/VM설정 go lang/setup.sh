#!/usr/bin/env bash
# setup.sh — 폐쇄망(오프라인) 빌드. vendor/ 안의 의존성만 사용, 인터넷 접속 시도 안 함.
# 주의: 이 디렉토리는 파일명이 다른 독립 프로그램 2개라서 go build .(디렉토리 전체)는 쓸 수 없다
# (README.md 참고) — 반드시 파일명을 지정해서 각각 빌드해야 한다.
set -euo pipefail
cd "$(dirname "$0")"
go build -mod=vendor -o vm_affinity_bulk main_affinity.go
go build -mod=vendor -o vm_lpage_bulk main_lpage.go
echo "빌드 완료: $(pwd)/vm_affinity_bulk, $(pwd)/vm_lpage_bulk"
