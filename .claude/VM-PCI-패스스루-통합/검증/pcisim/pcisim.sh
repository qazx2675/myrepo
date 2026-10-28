#!/usr/bin/env bash
# pcisim.sh — pcisim(PCI 패스스루 흉내 vcsim) 시작/중지/상태/호스트 재부팅. 검증 전용 (배포하지 않음).
#
# 사용:
#   pcisim.sh start [pcisim 옵션...]   예) pcisim.sh start -rebootDevs=0000:3b:00.1
#   pcisim.sh stop
#   pcisim.sh status
#   pcisim.sh reboot <호스트>          예) pcisim.sh reboot bm01.saccae.com
#   pcisim.sh env                      govc/도구용 환경변수 export 줄 출력 (eval "$(pcisim.sh env)")
#
# 접속 주소·계정·호스트는 환경변수로 바꾼다 (reboot/env 도 같은 값을 쓰므로 -l 등은 옵션 대신 여기서 바꿀 것):
#   PCISIM_LISTEN(127.0.0.1:8989) PCISIM_USER(lscsystems@vsphere.local) PCISIM_PASS(pcisim-pass)
#   PCISIM_HOSTS(bm01.saccae.com,bm02.saccae.com)
set -euo pipefail
cd "$(dirname "$0")"

LISTEN=${PCISIM_LISTEN:-127.0.0.1:8989}
VC_USER=${PCISIM_USER:-lscsystems@vsphere.local}
VC_PASS=${PCISIM_PASS:-pcisim-pass}
HOSTS=${PCISIM_HOSTS:-bm01.saccae.com,bm02.saccae.com}
PIDF=./pcisim.pid
LOG=./pcisim.log

running() { [ -f "$PIDF" ] && kill -0 "$(cat "$PIDF")" 2>/dev/null; }

print_env() {
  echo "export GOVC_URL=https://$LISTEN/sdk GOVC_USERNAME='$VC_USER' GOVC_PASSWORD='$VC_PASS' GOVC_INSECURE=1"
  echo "export VC_PASSWORD='$VC_PASS'   # V2 도구: -vcTargetIP=$LISTEN -id=$VC_USER"
}

case "${1:-}" in
  start)
    shift
    if running; then echo "이미 실행 중 (pid $(cat "$PIDF"))"; exit 0; fi
    if [ ! -x ./pcisim ] || [ main.go -nt ./pcisim ]; then
      echo "[빌드] go build -o pcisim ."
      go build -o pcisim .
    fi
    nohup ./pcisim -l "$LISTEN" -username "$VC_USER" -password "$VC_PASS" -hosts "$HOSTS" "$@" >"$LOG" 2>&1 &
    echo $! >"$PIDF"
    for _ in $(seq 1 50); do
      grep -q '\[READY\]' "$LOG" 2>/dev/null && break
      running || { echo "시작 실패:"; cat "$LOG"; rm -f "$PIDF"; exit 1; }
      sleep 0.2
    done
    grep -q '\[READY\]' "$LOG" || { echo "시작 대기 시간 초과:"; cat "$LOG"; exit 1; }
    echo "pcisim 시작 (pid $(cat "$PIDF"), 로그 $(pwd)/pcisim.log)"
    grep '\[INIT\]' "$LOG" | sed 's/^/  /'
    print_env
    ;;
  stop)
    if running; then kill "$(cat "$PIDF")"; echo "pcisim 중지 (pid $(cat "$PIDF"))"; else echo "실행 중이 아님"; fi
    rm -f "$PIDF"
    ;;
  status)
    if ! running; then echo "실행 중이 아님"; exit 1; fi
    echo "실행 중 (pid $(cat "$PIDF")) https://$LISTEN/sdk"
    curl -sk "https://$LISTEN/pcisim/state"
    ;;
  reboot)
    [ -n "${2:-}" ] || { echo "사용: $0 reboot <호스트>"; exit 2; }
    curl -sfk "https://$LISTEN/pcisim/reboot?host=$2" || { echo "재부팅 실패: $2 (호스트 이름 확인, pcisim 실행 여부 확인)"; exit 1; }
    ;;
  env)
    print_env
    ;;
  *)
    sed -n '2,13p' "$0" | sed 's/^# \{0,1\}//'
    exit 2
    ;;
esac
