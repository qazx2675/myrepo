#!/usr/bin/env bash
# e2e_vcsim_crosscheck.sh — 교차검증 방식(cross_check=on, 기본) 안전장치를 vcsim + 격리된 실제 cross_check 데몬으로 끝까지 시험한다.
# (root, Linux, Python3. 빌드 서버(192.168.0.58)에서 실행)
#   1/5 작업 확인 문구 → 2/5 확인자 번호 → 3/5 확인자 TUI 승인(대상 붙여넣기 + 패스워드) → 4/5 ls·YeS → 5/5 카운트다운 → --start → Power Off → --finish
#   + VM 실패(강제 OFF No) / 확인자 취소 / 대상 불일치 / 4/5 에 yes·No / 카운트다운·본 작업 중 SIGINT / 카운트다운 중 승인 유효시간(6초) 초과 / 데몬 없음
#   + 계속 대기 금지: 4/5 방치 → 승인 마감에 자동 취소(작업중단) / 강제 OFF 질문 방치(input_timeout_sec=30) → No 처리(작업완료) / 3/5 승인 대기 방치 → 데몬 wait_timeout_sec(3초) 중단
#   각 시나리오 뒤 격리 데몬의 jobs.json 에서 상태를 확인한다(승인/작업완료 · 승인/작업중단 · 중단, caller=vm_power_onoff)
# 데몬은 임시 폴더에서만 돈다(/etc/cross_check·/var·crontab 미사용). /etc/vcetner_pwon_off/pwonoff.conf 는 임시로 바꾸고 끝나면 복원한다.
# GO(vm_power_onoff 빌드), GO_CC(cross_check 빌드, 기본 GO), GOFLAGS 를 환경변수로 바꿀 수 있다.
# CC_E2E_TRANSCRIPT=<파일> 을 주면 승인 시나리오의 화면 출력을 저장한다.
set -euo pipefail
cd "$(dirname "$0")/.."
GO="${GO:-/opt/go1.26.5/bin/go}"; GO_CC="${GO_CC:-$GO}"
export GOFLAGS="${GOFLAGS:--mod=mod}" GOPROXY=off GONOSUMDB='*'
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
# cross_check 는 의존성 없는 별도 모듈(vendor 없음)이라 -mod=mod 로 빌드한다
(cd cross_check && GOFLAGS=-mod=mod "$GO_CC" build -o "$TMP/cross_check" ./cmd/cross_check)
CC_BIN="$TMP/cross_check" python3 test/e2e_vcsim_crosscheck.py "$TMP/vm_power_onoff" "$TMP/vcsim_server" "$WORK"
