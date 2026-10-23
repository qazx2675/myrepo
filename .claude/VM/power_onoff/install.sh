#!/usr/bin/env bash
# install.sh — vm_power_onoff 를 명령처럼 쓰도록 설치한다 (root 로 실행).
#   /usr/local/bin/vm_power_onoff           실행파일 (같은 폴더의 vm_power_onoff, 없으면 setup.sh 로 먼저 빌드)
#   /etc/vcetner_pwon_off/pwonoff.conf      설정 파일 (이미 있으면 덮어쓰지 않음)
# 설정 디렉터리 이름은 요구사항 그대로 'vcetner' 철자를 쓴다.
# 사용법:
#   sudo bash install.sh [허용디렉터리]      # 허용디렉터리 = conf 의 allowed_dir (기본: 이 폴더의 절대경로)
#   sudo bash install.sh -u                   # 제거 (설정 파일은 남김)
set -euo pipefail
cd "$(dirname "$0")"
HERE="$(pwd -P)"
BIN_DST=/usr/local/bin/vm_power_onoff
CONF_DIR=/etc/vcetner_pwon_off
CONF="$CONF_DIR/pwonoff.conf"

[ "$(id -u)" -eq 0 ] || { echo "[오류] root 로 실행하세요 (sudo bash install.sh)" >&2; exit 1; }

if [ "${1:-}" = "-u" ]; then
  rm -f "$BIN_DST" && echo "[완료] $BIN_DST 삭제 (설정 $CONF_DIR 는 남겨 둠)"
  exit 0
fi
ALLOWED="${1:-$HERE}"
case "$ALLOWED" in /*) ;; *) echo "[오류] 허용디렉터리는 절대경로여야 합니다: $ALLOWED" >&2; exit 1 ;; esac

if [ ! -x ./vm_power_onoff ]; then
  echo "[INFO] 실행파일이 없어 setup.sh 로 먼저 빌드합니다"
  bash ./setup.sh
fi
install -m 755 -o root -g root ./vm_power_onoff "$BIN_DST"
echo "[완료] $BIN_DST"

install -d -m 755 -o root -g root "$CONF_DIR"
if [ -e "$CONF" ]; then
  echo "[INFO] $CONF 가 이미 있어 덮어쓰지 않습니다 (예시: $HERE/pwonoff.conf.example)"
else
  sed "s#^allowed_dir=.*#allowed_dir=$ALLOWED#" pwonoff.conf.example > "$CONF.tmp"
  install -m 644 -o root -g root "$CONF.tmp" "$CONF"; rm -f "$CONF.tmp"
  echo "[완료] $CONF (allowed_dir=$ALLOWED) — users/vcenters 등을 환경에 맞게 고치세요"
fi
if [ ! -d "$ALLOWED" ]; then
  mkdir -p "$ALLOWED" && echo "[완료] 허용디렉터리 생성: $ALLOWED"
fi
echo "[INFO] 대상 파일은 $ALLOWED/on_off_<user>.txt 에 두고, 그 디렉터리에서 vm_power_onoff 를 실행합니다."
