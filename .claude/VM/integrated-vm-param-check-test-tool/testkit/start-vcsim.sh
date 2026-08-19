#!/usr/bin/env bash
# start-vcsim.sh — vc-test-env로 vcsim을 백그라운드로 기동한다 (고정 포트 127.0.0.1:54321).
#
# 캐시된 레시피(vc-test-env/~/.vc-test-env/recipes/)가 있으면 그걸 그대로 쓰고, 없으면
# -vc로 지정한 실 vCenter에서 새로 추출한다. 최초 1회는 실 vCenter 접속이 필요하지만,
# 그 이후로는 레시피가 캐시되어 인터넷/실vCenter 접속 없이도 계속 재사용 가능하다.
#
# 사용법:
#   ./start-vcsim.sh                       # 캐시된 레시피 중 최근 것을 사용 (history 기준)
#   ./start-vcsim.sh -vc=192.168.0.50      # 특정 vCenter의 캐시 레시피 사용 (없으면 실접속 추출)
#   ./start-vcsim.sh -vc=192.168.0.50 -refresh   # 캐시 무시하고 다시 추출
set -euo pipefail
cd "$(dirname "$0")/.."

BIN="./vc-test-env/vc-test-env"
if [ ! -x "$BIN" ]; then
  echo "오류: $BIN 이 없습니다. 먼저 ./testkit/build-all.sh 를 실행하세요." >&2
  exit 1
fi

LOGDIR="testkit/out"
mkdir -p "$LOGDIR"
PIDFILE="$LOGDIR/vcsim.pid"

if [ -f "$PIDFILE" ] && kill -0 "$(cat "$PIDFILE")" 2>/dev/null; then
  echo "이미 실행 중인 vcsim이 있습니다 (PID $(cat "$PIDFILE")). 먼저 ./testkit/stop-vcsim.sh 로 종료하세요."
  exit 1
fi

echo "vcsim을 백그라운드로 기동합니다..."
nohup "$BIN" "$@" > "$LOGDIR/vcsim.log" 2>&1 &
echo $! > "$PIDFILE"

echo -n "127.0.0.1:54321 응답 대기 중"
for i in $(seq 1 30); do
  if (exec 3<>/dev/tcp/127.0.0.1/54321) 2>/dev/null; then
    exec 3<&- 3>&-
    echo " -> 기동 완료 (PID $(cat "$PIDFILE"))"
    echo "로그: $LOGDIR/vcsim.log"
    exit 0
  fi
  echo -n "."
  sleep 1
done

echo
echo "오류: 30초 내에 vcsim이 응답하지 않았습니다. $LOGDIR/vcsim.log 를 확인하세요." >&2
exit 1
