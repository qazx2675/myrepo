#!/usr/bin/env bash
# stop-vcsim.sh — start-vcsim.sh로 띄운 vcsim 백그라운드 프로세스를 종료한다.
set -uo pipefail
cd "$(dirname "$0")/.."

PIDFILE="testkit/out/vcsim.pid"
if [ ! -f "$PIDFILE" ]; then
  echo "PID 파일이 없습니다 ($PIDFILE) — 실행 중인 vcsim이 없거나 이미 정리된 상태입니다."
  exit 0
fi

PID="$(cat "$PIDFILE")"
if kill -0 "$PID" 2>/dev/null; then
  kill "$PID"
  echo "vcsim 종료 신호 전송 (PID $PID)"
else
  echo "PID $PID 프로세스가 이미 종료되어 있습니다."
fi
rm -f "$PIDFILE"
