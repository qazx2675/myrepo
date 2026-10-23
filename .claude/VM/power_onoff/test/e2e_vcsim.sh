#!/usr/bin/env bash
# e2e_vcsim.sh — vm_power_onoff 바이너리를 vcsim 위에서 처음부터 끝까지 시험한다 (root, Linux, Python3, 모듈 캐시의 govmomi 필요).
# 빌드 서버(192.168.0.58)에서 실행한다. 로컬에서는:
#   REMOTE_DIR=/root/pod_all bash dev/sync_test.sh build    # 소스 동기화 + 빌드
#   ssh root@192.168.0.58 'bash /root/pod_all/test/e2e_vcsim.sh'
# /etc/vcetner_pwon_off/pwonoff.conf 를 임시로 바꾸므로 끝나면 원래 것을 복원한다.
set -euo pipefail
cd "$(dirname "$0")/.."
GO="${GO:-/opt/go1.26.5/bin/go}"; export GOFLAGS=-mod=mod GOPROXY=off GONOSUMDB='*'
[ "$(id -u)" -eq 0 ] || { echo "root 로 실행하세요" >&2; exit 1; }
TMP="$(mktemp -d)"; WORK="$TMP/allowed"; mkdir -p "$WORK"
CONF_DIR=/etc/vcetner_pwon_off; CONF="$CONF_DIR/pwonoff.conf"; BAK="$TMP/conf.bak"
HAD_DIR=0; [ -d "$CONF_DIR" ] && HAD_DIR=1
[ -f "$CONF" ] && cp -p "$CONF" "$BAK"
cleanup() {
  if [ -f "$BAK" ]; then cp -p "$BAK" "$CONF"; else rm -f "$CONF"; [ "$HAD_DIR" -eq 0 ] && rmdir "$CONF_DIR" 2>/dev/null || true; fi
  rm -rf "$TMP"
}
trap cleanup EXIT
mkdir -p "$CONF_DIR"
"$GO" build -o "$TMP/vm_power_onoff" .
"$GO" build -tags vcsim -o "$TMP/vcsim_server" ./test/vcsim_server
python3 test/e2e_driver.py "$TMP/vm_power_onoff" "$TMP/vcsim_server" "$WORK"
