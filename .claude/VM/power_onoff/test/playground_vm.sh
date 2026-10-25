#!/usr/bin/env bash
# playground_vm.sh — vm_power_onoff 와 cross_check 를 손으로 같이 시험하는 격리 놀이터 (빌드 서버 192.168.0.58, root).
# 실제 vCenter/ESXi 는 쓰지 않는다: 가짜 vCenter(vcsim)에 VM svr1ev01·svr1ev02(lpage)·svr1ev03·svr2ev01(VWP) 가 있다.
# cross_check 데몬은 cross_check/test/playground.sh 의 놀이터($HOME/cc_playground)를 그대로 쓴다.
#
#   bash playground_vm.sh start [on|off]  cross_check 놀이터 기동 → 빌드 → vcsim 기동(VM 시작 전원 on|off, 기본 on) → 임시 conf 설치
#   bash playground_vm.sh run [옵션]       작업자 alice 로 vm_power_onoff 실행 (기본 -u alice -c bob, 옵션은 그대로 덧붙임)
#   bash playground_vm.sh status          vcsim·conf·작업 기록 요약
#   bash playground_vm.sh stop            vcsim 중지 + /etc/vcetner_pwon_off/pwonoff.conf 원상복구 (cross_check 놀이터는 그대로)
#
# 주의: /etc/vcetner_pwon_off/pwonoff.conf 를 시험용으로 바꾼다(없었으면 stop 때 삭제, 있었으면 백업해 두었다가 복원).
#       vCenter 비밀번호 질문에는 아무 값(예: pw)이나 입력하면 된다. 실제 장비에는 아무것도 하지 않는다.
set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)"
PO_ROOT="$(cd "$HERE/.." && pwd -P)"
CC_PLAY_SH="$PO_ROOT/cross_check/test/playground.sh"
PLAY="${CC_PLAY_DIR:-${HOME:-/root}/cc_playground}"
VM_DIR="${VM_PLAY_DIR:-${HOME:-/root}/vm_playground}"
CONF_DIR=/etc/vcetner_pwon_off
CONF="$CONF_DIR/pwonoff.conf"
BAK="$VM_DIR/pwonoff.conf.bak"
ADDR="$VM_DIR/vcsim.addr"
GO="${GO:-/opt/go1.26.5/bin/go}"

say() { printf '%s\n' "$*"; }
die() { printf '[오류] %s\n' "$*" >&2; exit 1; }
[ "$(id -u)" -eq 0 ] || die "root 로 실행하세요"

sim_pid() { pgrep -f -- "$VM_DIR/vcsim_server" 2>/dev/null | grep -vx "$$" | head -1 || true; }

cmd_start() {
  local power="${1:-on}"
  case "$power" in on|off) ;; *) die "전원 상태는 on 또는 off: $power" ;; esac
  [ -x "$GO" ] || die "Go 를 찾지 못했습니다: $GO  (GO=/경로/go 로 지정)"
  mkdir -p "$VM_DIR"

  say "[1/4] cross_check 놀이터 기동"
  bash "$CC_PLAY_SH" start | sed 's/^/      /'

  say "[2/4] 빌드 (오프라인)"
  ( cd "$PO_ROOT" && GOFLAGS=-mod=mod GOPROXY=off GONOSUMDB='*' "$GO" build -o "$VM_DIR/vm_power_onoff" . \
      && GOFLAGS=-mod=mod GOPROXY=off GONOSUMDB='*' "$GO" build -tags vcsim -o "$VM_DIR/vcsim_server" ./test/vcsim_server ) \
    || die "go build 실패"

  say "[3/4] vcsim(가짜 vCenter) 기동, VM 시작 전원: $power"
  if [ -n "$(sim_pid)" ]; then kill "$(sim_pid)" 2>/dev/null || true; sleep 1; fi
  rm -f "$ADDR"
  nohup "$VM_DIR/vcsim_server" -addr-file "$ADDR" -power "$power" >"$VM_DIR/vcsim.log" 2>&1 &
  local n=0
  while [ ! -s "$ADDR" ]; do
    n=$((n + 1)); [ "$n" -gt 100 ] && die "vcsim 이 뜨지 않음 ($VM_DIR/vcsim.log)"
    sleep 0.1
  done

  say "[4/4] 시험용 conf 설치 ($CONF)"
  mkdir -p "$CONF_DIR"
  if [ -f "$CONF" ] && [ ! -f "$BAK" ] && ! grep -q '^# playground_vm.sh' "$CONF"; then cp -p "$CONF" "$BAK"; fi
  [ -f "$BAK" ] || : > "$VM_DIR/.no_original_conf"
  cat > "$CONF" <<EOF
# playground_vm.sh 가 만든 시험용 설정 (stop 하면 원래대로 돌아간다)
allowed_dir=$PLAY/targets
users=alice,bob,carol
vcenters=$(cat "$ADDR")
vc_id=user
cross_check=on
cross_check_bin=$PLAY/bin/cross_check
concurrency=4
shutdown_timeout=5
boot_timeout=5
boot_poll_interval=1
port_check_timeout=1
EOF
  chmod 644 "$CONF"
  # alice 의 대상은 vcsim 에 실제로 있는 VM 이름이어야 한다
  printf '# alice 의 작업 대상 (vcsim)\nsvr1ev01\nsvr1ev02\nsvr2ev01\n' > "$PLAY/targets/on_off_alice.txt"

  say
  say "[완료] vcsim: $(cat "$ADDR")  /  VM: svr1ev01 svr1ev02(lpage) svr1ev03 svr2ev01(VWP)  /  alice 대상: svr1ev01 svr1ev02 svr2ev01"
  say "       작업자:  bash $HERE/playground_vm.sh run"
  say "       확인자:  eval \"\$(bash $PLAY_SH_ENV)\"; cross_check --user bob"
}

cmd_run() {
  [ -s "$ADDR" ] && [ -n "$(sim_pid)" ] || die "vcsim 이 떠 있지 않습니다. 먼저: bash $HERE/playground_vm.sh start"
  [ -x "$VM_DIR/vm_power_onoff" ] || die "vm_power_onoff 가 없습니다. 먼저 start"
  cd "$PLAY/targets"   # allowed_dir 에서만 실행된다
  if [ "$#" -eq 0 ]; then set -- -u alice -c bob; fi
  exec "$VM_DIR/vm_power_onoff" -v "$(cat "$ADDR")" "$@"
}

cmd_status() {
  say "vcsim      : $([ -n "$(sim_pid)" ] && echo "실행 중 (pid $(sim_pid), $(cat "$ADDR" 2>/dev/null))" || echo '중지')"
  say "conf       : $([ -f "$CONF" ] && { grep -q '^# playground_vm.sh' "$CONF" && echo '시험용 conf 적용 중' || echo '다른 conf (원본)'; } || echo '없음')"
  bash "$CC_PLAY_SH" status | sed 's/^/cross_check : /'
}

cmd_stop() {
  local p; p="$(sim_pid)"
  if [ -n "$p" ]; then kill "$p" 2>/dev/null || true; say "vcsim 중지"; fi
  if [ -f "$CONF" ] && grep -q '^# playground_vm.sh' "$CONF"; then
    if [ -f "$BAK" ]; then cp -p "$BAK" "$CONF"; rm -f "$BAK"; say "conf 원상복구"
    else rm -f "$CONF"; rmdir "$CONF_DIR" 2>/dev/null || true; say "시험용 conf 삭제"; fi
  fi
  rm -f "$VM_DIR/.no_original_conf" "$ADDR"
  say "[완료] cross_check 놀이터 데몬은 그대로입니다 (끄려면: bash $CC_PLAY_SH stop)"
}

PLAY_SH_ENV="$CC_PLAY_SH env"
case "${1:-help}" in
  start) shift; cmd_start "$@" ;;
  run) shift; cmd_run "$@" ;;
  status) cmd_status ;;
  stop) cmd_stop ;;
  *) sed -n '2,14p' "$0" ;;
esac
