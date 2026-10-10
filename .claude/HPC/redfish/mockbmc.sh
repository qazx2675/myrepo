#!/usr/bin/env bash
# mockbmc.sh — mock BMC 서버 실행 (개발·검증 전용, 실제 BMC 가 아님).
#
# 사용법:
#   MOCKBMC_PASS='비밀번호' bash mockbmc.sh dell-r660            # testdata/dell-r660 을 127.0.0.1:8443 에
#   MOCKBMC_PASS='비밀번호' bash mockbmc.sh hpe-dl360gen11 8444  # 포트 지정 (여러 개 동시에 가능)
#   MOCKBMC_PASS='비밀번호' bash mockbmc.sh -dir testdata/x -listen 127.0.0.1:9000 -user admin -v
#
# 첫 인자가 이름이면 -dir testdata/<이름> -listen 127.0.0.1:<포트|8443> 로 바꿔 mockbmc 에 전달하고,
# -로 시작하면 인자를 그대로 전달합니다. 종료는 Ctrl-C (집계 한 줄 출력).
# 빌드 완료 bin/mockbmc (RHEL6 은 bin/mockbmc_os6) 가 있으면 빌드 없이 그대로 쓰고, 없을 때만 go 로 빌드합니다.
set -euo pipefail
cd "$(dirname "$0")"

case "${1:-}" in
  -h | --help)
    sed -n '2,10p' "$0" | sed 's/^# \{0,1\}//'
    exit 0
    ;;
esac

# RHEL6 (커널 2.x) 는 Go 1.20 으로 빌드해야 한다 (build_os6.sh 와 같은 경로).
kmajor="$(uname -r | cut -d. -f1)"
case "$kmajor" in '' | *[!0-9]*) kmajor=99 ;; esac
if [ "$kmajor" -lt 3 ]; then
  export PATH=/opt/go1.20/bin:$PATH
fi

BIN=bin/mockbmc
if [ "$kmajor" -lt 3 ]; then
  BIN=bin/mockbmc_os6
fi
if [ -f "$BIN" ] && [ ! -x "$BIN" ]; then
  chmod +x "$BIN" 2>/dev/null || true
fi
if [ ! -x "$BIN" ]; then
  if ! command -v go >/dev/null 2>&1; then
    echo "오류: go 를 찾을 수 없어 mockbmc 를 빌드하지 못했습니다." >&2
    exit 2
  fi
  mkdir -p bin
  CGO_ENABLED=0 go build -trimpath -o "$BIN" ./cmd/mockbmc >&2
fi

if [ $# -gt 0 ] && [ "${1#-}" = "$1" ]; then
  name="$1"
  shift
  port=8443
  case "${1:-}" in '' | *[!0-9]*) ;; *)
    port="$1"
    shift
    ;;
  esac
  exec "$BIN" -dir "testdata/$name" -listen "127.0.0.1:$port" "$@"
fi
exec "$BIN" "$@"
