#!/usr/bin/env bash
# gpu-power-limit.service 설정이 정상적으로 적용되었는지 일회성으로 점검하는 스크립트.
# gossh 등으로 여러 서버에 배포 후 일괄 실행하는 용도이므로,
# 결과는 사람이 읽기보다 grep/파싱하기 쉬운 "OK\t설명" / "FAIL\t설명" 형식으로 한 줄씩 출력한다.

set -uo pipefail

# ── 환경에 맞게 채워야 하는 변수 ─────────────────────────────
# GPU 최대 전력의 75%로 설정한 목표 power limit (W 단위, 정수).
# 예: 최대 1100W GPU라면 825
TARGET_POWER_LIMIT_W=825

# systemd service 이름 (확장자 .service 제외)
SERVICE_NAME="gpu-power-limit"

# service 유닛 파일 경로 (보통 고정값, 서버마다 다르면 수정)
SERVICE_FILE="/etc/systemd/system/${SERVICE_NAME}.service"

# 실제 power limit을 설정하는 스크립트의 절대 경로.
# service 파일의 ExecStart= 에 적힌 경로와 동일해야 한다.
SET_SCRIPT_PATH=""
# ─────────────────────────────────────────────────────────

report() {
    # $1: OK 또는 FAIL, $2: 설명
    printf '%s\t%s\n' "$1" "$2"
}

# 1) service 유닛 파일 존재 확인
if [[ -f "$SERVICE_FILE" ]]; then
    report OK "service file exists (${SERVICE_FILE})"
else
    report FAIL "service file missing (${SERVICE_FILE})"
fi

# 2) service 파일이 지정한 설정 스크립트를 가리키는지 확인
if [[ -n "$SET_SCRIPT_PATH" ]]; then
    if [[ -f "$SERVICE_FILE" ]] && grep -qF "$SET_SCRIPT_PATH" "$SERVICE_FILE"; then
        report OK "ExecStart references ${SET_SCRIPT_PATH}"
    else
        report FAIL "ExecStart does not reference ${SET_SCRIPT_PATH}"
    fi
fi

# 3) service active 상태 확인
if systemctl is-active --quiet "$SERVICE_NAME"; then
    report OK "service is active (${SERVICE_NAME})"
else
    report FAIL "service is not active (${SERVICE_NAME})"
fi

# 4) service enable 상태 확인 (재부팅 후에도 유지되는지)
if systemctl is-enabled --quiet "$SERVICE_NAME"; then
    report OK "service is enabled (${SERVICE_NAME})"
else
    report FAIL "service is not enabled (${SERVICE_NAME})"
fi

# 5) nvidia-smi 설치 확인
if ! command -v nvidia-smi >/dev/null 2>&1; then
    report FAIL "nvidia-smi not found"
    exit 1
fi

# 6) 각 GPU별 실제 power limit이 목표값과 일치하는지 확인
gpu_count=$(nvidia-smi --query-gpu=count --format=csv,noheader,nounits 2>/dev/null | head -n1)
if [[ -z "$gpu_count" || "$gpu_count" -eq 0 ]]; then
    report FAIL "no GPU detected"
    exit 1
fi

while IFS=, read -r idx power_limit; do
    idx=$(echo "$idx" | xargs)
    power_limit_w=$(echo "$power_limit" | xargs | cut -d'.' -f1)
    if [[ "$power_limit_w" -eq "$TARGET_POWER_LIMIT_W" ]]; then
        report OK "GPU ${idx} power limit = ${power_limit_w}W (target ${TARGET_POWER_LIMIT_W}W)"
    else
        report FAIL "GPU ${idx} power limit = ${power_limit_w}W (target ${TARGET_POWER_LIMIT_W}W)"
    fi
done < <(nvidia-smi --query-gpu=index,power.limit --format=csv,noheader,nounits)
