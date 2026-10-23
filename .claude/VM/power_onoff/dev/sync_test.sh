#!/usr/bin/env bash
# dev/sync_test.sh — 로컬 작업 폴더를 빌드 서버(192.168.0.58)로 동기화한 뒤 거기서 go 명령을 실행한다.
#   환경변수: REMOTE_DIR=/root/xxx (병렬 작업자끼리 겹치지 않게), SYNC_FILES="파일 목록" (기본: 전체)
#   bash dev/sync_test.sh vet            # go vet ./...
#   bash dev/sync_test.sh test [인자..]   # go test ./... (-tags vcsim 등은 인자로)
#   bash dev/sync_test.sh run [인자..]    # go run . 인자
# 모듈 캐시(오프라인, GOPROXY=off)의 govmomi 0.55.1 을 쓰므로 vcsim(simulator) 테스트도 여기서 돈다.
set -euo pipefail
cd "$(dirname "$0")/.."
HOST="${BUILD_HOST:-192.168.0.58}"; REMOTE="${REMOTE_DIR:-/root/power_onoff_dev}"; GO=/opt/go1.26.5/bin/go
SSH=(ssh -o BatchMode=yes -o LogLevel=ERROR "root@$HOST")
"${SSH[@]}" "rm -rf $REMOTE && mkdir -p $REMOTE"
FILES="${SYNC_FILES:-.}"   # 병렬 작업 시 자기 파일만: SYNC_FILES="go.mod go.sum main.go types.go ui.go guard.go guard_test.go"
tar --exclude=.git --exclude=vendor --anchored --exclude='./run_*' -cf - $FILES | "${SSH[@]}" "tar -mxf - -C $REMOTE"
cmd="${1:-vet}"; shift || true
case "$cmd" in
  vet)  rc="$GO vet $*  ./..." ;;
  test) rc="$GO test $* ./..." ;;
  run)  rc="$GO run . $*" ;;
  build) rc="$GO build -o vm_power_onoff . && ls -l vm_power_onoff" ;;
  *) echo "usage: $0 vet|test|run|build [args]" >&2; exit 2 ;;
esac
# 원격 로그인 셸이 csh 라서 bash -s 로 스크립트를 넘긴다
"${SSH[@]}" bash -s <<REMOTE_EOF
set -e
cd $REMOTE
export GOFLAGS=-mod=mod GOPROXY=off GONOSUMDB='*'
$rc
REMOTE_EOF
