#!/bin/bash

################################################################################
# GPU Power Limit 자동 수정 스크립트
# 용도: 검증 실패 시 자동으로 설정 복구
# 사용: sudo ./fix-gpu-power-limit.sh [POWER_LIMIT_VALUE]
################################################################################

set -e

# 색상 정의
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

# 설정값
SERVICE_FILE="/etc/systemd/system/gpu-power-limit.service"
POWER_LIMIT="${1:-75}"
TEMP_SERVICE="/tmp/gpu-power-limit.service.tmp"

# 함수: 로그 출력
log_info() {
    echo -e "${BLUE}ℹ INFO${NC}: $1"
}

log_pass() {
    echo -e "${GREEN}✓ PASS${NC}: $1"
}

log_warn() {
    echo -e "${YELLOW}⚠ WARN${NC}: $1"
}

log_fail() {
    echo -e "${RED}✗ FAIL${NC}: $1"
}

log_header() {
    echo ""
    echo "=============================================================================="
    echo "  $1"
    echo "=============================================================================="
}

# 함수: 권한 확인
check_root() {
    if [[ $EUID -ne 0 ]]; then
        log_fail "이 스크립트는 root 권한이 필요합니다"
        echo "사용법: sudo $0 [POWER_LIMIT_VALUE]"
        exit 1
    fi
}

# 함수: nvidia-smi 확인
check_nvidia_smi() {
    if ! command -v nvidia-smi &> /dev/null; then
        log_fail "nvidia-smi가 설치되어 있지 않습니다"
        exit 1
    fi
    log_pass "nvidia-smi 확인"
}

# 함수: 현재 설정 확인
check_current_setting() {
    log_header "1. 현재 설정 확인"

    if [[ ! -f "$SERVICE_FILE" ]]; then
        log_warn "Service 파일이 존재하지 않습니다: $SERVICE_FILE"
        return 1
    fi

    log_info "현재 Service 파일 내용:"
    echo "---"
    cat "$SERVICE_FILE"
    echo "---"

    return 0
}

# 함수: Service 파일 생성/수정
create_service_file() {
    log_header "2. Service 파일 생성/수정"

    log_info "새 Service 파일을 생성합니다..."
    log_info "Power Limit: ${POWER_LIMIT}W"

    # 임시 파일에 내용 작성
    cat > "$TEMP_SERVICE" << EOF
[Unit]
Description=NVIDIA GPU Power Limit (${POWER_LIMIT}W) Persistence
After=nvidia-persistenced.service
Documentation=https://docs.nvidia.com/deploy/dynamic-power-management/

[Service]
Type=oneshot
ExecStart=/usr/bin/nvidia-smi -pl ${POWER_LIMIT}
RemainAfterExit=yes
StandardOutput=journal
StandardError=journal

[Install]
WantedBy=multi-user.target
EOF

    # 파일 소유권 및 권한 설정
    chown root:root "$TEMP_SERVICE"
    chmod 644 "$TEMP_SERVICE"

    # 기존 파일 백업
    if [[ -f "$SERVICE_FILE" ]]; then
        BACKUP_FILE="${SERVICE_FILE}.backup.$(date +%s)"
        log_info "기존 파일을 백업합니다: $BACKUP_FILE"
        cp "$SERVICE_FILE" "$BACKUP_FILE"
    fi

    # 새 파일 적용
    mv "$TEMP_SERVICE" "$SERVICE_FILE"
    log_pass "Service 파일 업데이트 완료"

    log_info "생성된 Service 파일:"
    echo "---"
    cat "$SERVICE_FILE"
    echo "---"
}

# 함수: Systemd 데몬 재로드
reload_systemd() {
    log_header "3. Systemd 데몬 재로드"

    log_info "systemctl daemon-reload 실행 중..."
    if systemctl daemon-reload; then
        log_pass "Systemd 데몬 재로드 완료"
    else
        log_fail "Systemd 데몬 재로드 실패"
        return 1
    fi
}

# 함수: Service 활성화
enable_service() {
    log_header "4. Service 활성화 및 시작"

    log_info "Service 활성화 중..."
    if systemctl enable gpu-power-limit.service; then
        log_pass "Service 활성화 완료"
    else
        log_fail "Service 활성화 실패"
        return 1
    fi

    log_info "Service 시작 중..."
    if systemctl start gpu-power-limit.service; then
        log_pass "Service 시작 완료"
    else
        log_fail "Service 시작 실패"
        return 1
    fi
}

# 함수: 적용 확인
verify_power_limit() {
    log_header "5. Power Limit 적용 확인"

    # GPU 수 확인
    GPU_COUNT=$(nvidia-smi --query-gpu=count --format=csv,noheader | head -n 1)
    log_info "감지된 GPU 수: $GPU_COUNT"

    local all_ok=true

    for ((i = 0; i < GPU_COUNT; i++)); do
        log_info "GPU $i 확인..."

        POWER_LIMIT_CURRENT=$(nvidia-smi -i "$i" --query-gpu=power.limit --format=csv,noheader)
        MAX_POWER_LIMIT=$(nvidia-smi -i "$i" --query-gpu=power.max_limit --format=csv,noheader || echo "N/A")
        POWER_DRAW=$(nvidia-smi -i "$i" --query-gpu=power.draw --format=csv,noheader)

        log_info "  현재 Power Limit: $POWER_LIMIT_CURRENT"
        log_info "  Max Power Limit: $MAX_POWER_LIMIT"
        log_info "  Power Draw: $POWER_DRAW"

        # 설정값 확인
        POWER_LIMIT_VALUE=$(echo "$POWER_LIMIT_CURRENT" | grep -o "[0-9.]*" | head -n 1)
        EXPECTED=$(echo "$POWER_LIMIT" | grep -o "[0-9.]*" | head -n 1)

        if [[ "$POWER_LIMIT_VALUE" == "$EXPECTED" ]]; then
            log_pass "GPU $i Power Limit 설정 정상"
        else
            log_warn "GPU $i Power Limit 값이 다릅니다 (설정: ${POWER_LIMIT_VALUE}W, 기대: ${EXPECTED}W)"
            all_ok=false
        fi
    done

    if [[ "$all_ok" == "true" ]]; then
        return 0
    else
        return 1
    fi
}

# 함수: Service 상태 확인
check_service_status() {
    log_header "6. Service 상태 확인"

    log_info "Service 상태:"
    systemctl status gpu-power-limit.service --no-pager || true

    log_info "최근 로그:"
    journalctl -u gpu-power-limit.service -n 5 --no-pager || true
}

# 함수: 최종 요약
print_summary() {
    log_header "수정 작업 완료"

    echo ""
    echo "다음 단계:"
    echo "1. 적용 확인: sudo systemctl status gpu-power-limit.service"
    echo "2. 상세 검증: sudo ./check-gpu-power-limit.sh"
    echo "3. 로그 확인: journalctl -u gpu-power-limit.service -f"
    echo ""
    echo "문제 발생 시:"
    echo "1. 백업 복원: sudo cp ${SERVICE_FILE}.backup.* ${SERVICE_FILE}"
    echo "2. 수동 설정: sudo nvidia-smi -pl ${POWER_LIMIT}"
    echo "3. 재부팅: sudo reboot"
    echo ""
}

# 메인 함수
main() {
    echo ""
    echo "╔════════════════════════════════════════════════════════════════════════════╗"
    echo "║              GPU Power Limit 자동 수정 스크립트                           ║"
    echo "║                    nvidia-persistenced 설정 복구                           ║"
    echo "╚════════════════════════════════════════════════════════════════════════════╝"
    echo ""

    check_root
    check_nvidia_smi
    check_current_setting || true
    create_service_file
    reload_systemd
    enable_service

    sleep 1

    verify_power_limit
    check_service_status
    print_summary

    echo -e "${GREEN}✓ 수정 완료!${NC}"
    echo ""
}

# 스크립트 실행
main "$@"
