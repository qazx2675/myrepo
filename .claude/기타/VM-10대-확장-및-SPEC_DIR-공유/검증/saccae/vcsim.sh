#!/usr/bin/env bash
# vcsim.sh start|stop|restart|status — saccae 검증용 가상 vCenter (https://vcsim.saccae.com, .58:443)
# 데이터센터 DC0 에 독립 호스트 hostname0000.saccae.com ~ hostname0099.saccae.com (100대, 호스트마다 로컬 데이터스토어,
# 전원 정책 High Performance) + 실패 테스트 전용 hostname9000.saccae.com (데이터스토어 없음).
# vcsim 은 상태를 메모리에만 둔다 — stop/restart 하면 만든 VM·포트그룹은 모두 사라지고 빈 호스트 100대로 돌아간다.
# 로그인은 lscsystems@vsphere.local / Saccae1! 만 받는다 (VCSIM_USER / VCSIM_PASS 로 바꿀 수 있음).
set -u
D="$(cd "$(dirname "$0")" && pwd)"
BIN="$D/bin/vcsimenv"; PID="$D/vcsim.pid"; LOG="$D/vcsim.log"; DATA="$D/data"
ADDR="${VCSIM_ADDR:-192.168.0.58:443}"; N="${VCSIM_HOSTS:-100}"
VUSER="${VCSIM_USER:-lscsystems@vsphere.local}"; VPASS="${VCSIM_PASS:-Saccae1!}"

running() { [ -f "$PID" ] && kill -0 "$(cat "$PID")" 2>/dev/null; }

start() {
  if running; then echo "이미 실행 중입니다 (pid $(cat "$PID"))"; return 0; fi
  [ -x "$BIN" ] || { echo "$BIN 이 없습니다 — install.sh 를 먼저 실행하세요."; return 1; }
  local hosts="" i
  for i in $(seq 0 $((N - 1))); do hosts+="$(printf 'hostname%04d.saccae.com' "$i"),"; done
  hosts+="hostname9000.saccae.com,"   # 실패 테스트 전용: 데이터스토어 없는 BM (fail_nods)
  # VM 파일이 쌓이지 않게 실행마다 빈 데이터 폴더를 쓴다(vcsim 은 TMPDIR 아래에 데이터스토어를 만든다)
  rm -rf "$DATA"; mkdir -p "$DATA"
  TMPDIR="$DATA" setsid nohup "$BIN" -dc 1 -cluster 0 -host 0 -fqdnHosts "${hosts%,}" -noDsHosts hostname9000.saccae.com -staticPower -username "$VUSER" -password "$VPASS" -addr "$ADDR" > "$LOG" 2>&1 < /dev/null &
  echo $! > "$PID"
  for i in $(seq 1 600); do
    if grep -q '^READY' "$LOG"; then echo "vcsim 시작: https://vcsim.saccae.com (호스트 ${N}대, pid $(cat "$PID"))"; return 0; fi
    running || break
    sleep 0.2
  done
  echo "vcsim 시작 실패 — $LOG:"; cat "$LOG"; rm -f "$PID"; return 1
}

stop() {
  if ! running; then echo "실행 중이 아닙니다."; rm -f "$PID"; return 0; fi
  local p; p="$(cat "$PID")"; kill "$p"
  for _ in $(seq 1 50); do kill -0 "$p" 2>/dev/null || break; sleep 0.2; done
  kill -0 "$p" 2>/dev/null && kill -9 "$p"
  rm -f "$PID" && rm -rf "$DATA"; echo "vcsim 중지 (만든 VM·포트그룹은 모두 사라짐)"
}

case "${1:-}" in
  start) start ;;
  stop) stop ;;
  restart) stop; start ;;
  status) if running; then echo "실행 중 (pid $(cat "$PID")) — $(grep '^READY' "$LOG")"; else echo "중지됨"; exit 1; fi ;;
  *) echo "사용법: $0 start|stop|restart|status"; exit 2 ;;
esac
