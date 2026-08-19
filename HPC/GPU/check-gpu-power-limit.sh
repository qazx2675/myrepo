#!/bin/bash

################################################################################
# GPU Power Capping 설정 검증 스크립트
# 용도: nvidia-persistenced를 통한 Power Limit 75% 설정 검증
# 사용: sudo ./check-gpu-power-limit.sh [GPU_INDEX]
################################################################################

set -o pipefail

# 색상 정의
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# 설정값
TARGET_POWER_LIMIT=75
SERVICE_FILE="/etc/systemd/system/gpu-power-limit.service"
EXPECTED_POWER_LIMITS=("75" "75%")

# 통계
TOTAL_CHECKS=0
PASSED_CHECKS=0
FAILED_CHECKS=0
WARNING_CHECKS=0

################################################################################
# 함수: 메시지 출력
################################################################################

log_pass() {
    echo -e "${GREEN}✓ PASS${NC}: $1"
    ((PASSED_CHECKS++))
    ((TOTAL_CHECKS++))
}

log_fail() {
    echo -e "${RED}✗ FAIL${NC}: $1"
    ((FAILED_CHECKS++))
    ((TOTAL_CHECKS++))
}

log_warn() {
    echo -e "${YELLOW}⚠ WARN${NC}: $1"
    ((WARNING_CHECKS++))
    ((TOTAL_CHECKS++))
}

log_info() {
    echo -e "${BLUE}ℹ INFO${NC}: $1"
}

log_header() {
    echo ""
    echo "=============================================================================="
    echo "  $1"
    echo "=============================================================================="
}

################################################################################
# 함수: 사전 조건 확인
################################################################################

check_prerequisites() {
    log_header "1. 사전 조건 확인"

    # root 권한 확인
    if [[ $EUID -ne 0 ]]; then
        log_fail "root 권한이 필요합니다 (sudo 사용)"
        return 1
    else
        log_pass "root 권한 확인"
    fi

    # nvidia-smi 확인
    if ! command -v nvidia-smi &> /dev/null; then
        log_fail "nvidia-smi가 설치되어 있지 않습니다"
        return 1
    else
        log_pass "nvidia-smi 명령어 존재"
        NVIDIA_VERSION=$(nvidia-smi --version | head -n 1)
        log_info "버전: $NVIDIA_VERSION"
    fi

    # nvidia-settings 확인
    if ! command -v nvidia-settings &> /dev/null; then
        log_warn "nvidia-settings가 설치되어 있지 않습니다 (선택사항)"
    else
        log_pass "nvidia-settings 명령어 존재"
    fi

    # systemctl 확인
    if ! command -v systemctl &> /dev/null; then
        log_fail "systemctl이 설치되어 있지 않습니다"
        return 1
    else
        log_pass "systemctl 명령어 존재"
    fi

    # GPU 감지 확인
    GPU_COUNT=$(nvidia-smi --query-gpu=count --format=csv,noheader | head -n 1)
    if [[ -z "$GPU_COUNT" ]]; then
        log_fail "GPU를 감지할 수 없습니다"
        return 1
    else
        log_pass "GPU 감지: $GPU_COUNT개의 GPU 발견"
    fi

    return 0
}

################################################################################
# 함수: Service 파일 검증
################################################################################

check_service_file() {
    log_header "2. Service 파일 검증"

    # 파일 존재 확인
    if [[ ! -f "$SERVICE_FILE" ]]; then
        log_fail "Service 파일이 존재하지 않음: $SERVICE_FILE"
        return 1
    else
        log_pass "Service 파일 존재: $SERVICE_FILE"
    fi

    # 파일 권한 확인
    FILE_PERMS=$(stat -c "%a" "$SERVICE_FILE")
    log_info "파일 권한: $FILE_PERMS"
    if [[ "$FILE_PERMS" == "644" ]] || [[ "$FILE_PERMS" == "755" ]]; then
        log_pass "파일 권한 정상"
    else
        log_warn "파일 권한 확인: $FILE_PERMS (권장: 644)"
    fi

    # 파일 내용 확인
    log_info "Service 파일 내용:"
    echo "---"
    cat "$SERVICE_FILE"
    echo "---"

    # PowerLimit 설정값 확인
    if grep -q "nvidia-smi.*power.limit" "$SERVICE_FILE" || \
       grep -q "nvidia-smi.*pl" "$SERVICE_FILE"; then
        log_pass "Power Limit 설정 명령어 발견"

        # 실제 설정값 추출
        POWER_LIMIT_CMD=$(grep -o "nvidia-smi.*-pl [0-9]*" "$SERVICE_FILE" | head -n 1 | grep -o "[0-9]*$")
        if [[ -n "$POWER_LIMIT_CMD" ]]; then
            log_info "설정된 Power Limit: ${POWER_LIMIT_CMD}W"
        fi
    else
        log_warn "Power Limit 설정 명령어를 찾을 수 없습니다"
    fi

    # ExecStart 필드 확인
    if grep -q "^ExecStart=" "$SERVICE_FILE"; then
        log_pass "ExecStart 필드 존재"
    else
        log_fail "ExecStart 필드가 없습니다"
    fi

    # [Unit] 섹션 확인
    if grep -q "^\[Unit\]" "$SERVICE_FILE"; then
        log_pass "[Unit] 섹션 존재"
    else
        log_warn "[Unit] 섹션이 없습니다"
    fi

    # [Service] 섹션 확인
    if grep -q "^\[Service\]" "$SERVICE_FILE"; then
        log_pass "[Service] 섹션 존재"
    else
        log_fail "[Service] 섹션이 없습니다"
    fi

    # [Install] 섹션 확인
    if grep -q "^\[Install\]" "$SERVICE_FILE"; then
        log_pass "[Install] 섹션 존재"
    else
        log_warn "[Install] 섹션이 없습니다"
    fi
}

################################################################################
# 함수: Service 상태 검증
################################################################################

check_service_status() {
    log_header "3. Service 상태 검증"

    # Service 활성화 상태 확인
    if systemctl is-enabled gpu-power-limit.service &> /dev/null; then
        log_pass "Service 활성화 상태: enabled"
    else
        log_fail "Service 활성화 상태: disabled"
    fi

    # Service 실행 상태 확인
    if systemctl is-active --quiet gpu-power-limit.service; then
        log_pass "Service 실행 상태: running"
    else
        SERVICE_STATE=$(systemctl is-active gpu-power-limit.service)
        log_fail "Service 실행 상태: $SERVICE_STATE"
    fi

    # Service 상태 상세 정보
    log_info "Service 상세 정보:"
    systemctl status gpu-power-limit.service --no-pager || true

    # 최근 로그 확인
    log_info "최근 Service 로그:"
    journalctl -u gpu-power-limit.service -n 10 --no-pager || true
}

################################################################################
# 함수: GPU Power Limit 실제 적용 검증
################################################################################

check_gpu_power_limit() {
    log_header "4. GPU Power Limit 실제 적용 검증"

    local gpu_index=$1
    local all_pass=true

    # 특정 GPU만 확인하는 경우
    if [[ -n "$gpu_index" ]]; then
        check_single_gpu "$gpu_index" || all_pass=false
    else
        # 모든 GPU 확인
        for ((i = 0; i < GPU_COUNT; i++)); do
            check_single_gpu "$i" || all_pass=false
        done
    fi

    return $([ "$all_pass" = true ] && echo 0 || echo 1)
}

check_single_gpu() {
    local gpu_index=$1

    log_info "--- GPU $gpu_index 검사 ---"

    # Power Limit 확인
    if ! POWER_LIMIT=$(nvidia-smi -i "$gpu_index" --query-gpu=power.limit --format=csv,noheader 2>/dev/null); then
        log_fail "GPU $gpu_index: Power Limit 조회 실패"
        return 1
    fi

    # Power Limit 값 파싱
    POWER_LIMIT_VALUE=$(echo "$POWER_LIMIT" | grep -o "[0-9.]*" | head -n 1)
    log_info "GPU $gpu_index 현재 Power Limit: $POWER_LIMIT"

    # Power Draw 확인 (참고용)
    POWER_DRAW=$(nvidia-smi -i "$gpu_index" --query-gpu=power.draw --format=csv,noheader 2>/dev/null || echo "N/A")
    log_info "GPU $gpu_index 현재 Power Draw: $POWER_DRAW"

    # Power Limit의 Default Max 확인
    MAX_POWER_LIMIT=$(nvidia-smi -i "$gpu_index" --query-gpu=power.max_limit --format=csv,noheader 2>/dev/null || echo "N/A")
    log_info "GPU $gpu_index Max Power Limit: $MAX_POWER_LIMIT"

    # 기대값 계산 (Max Power Limit의 75%)
    if [[ "$MAX_POWER_LIMIT" != "N/A" ]]; then
        MAX_VALUE=$(echo "$MAX_POWER_LIMIT" | grep -o "[0-9.]*" | head -n 1)
        EXPECTED_VALUE=$(echo "scale=1; $MAX_VALUE * 0.75" | bc 2>/dev/null || echo "N/A")
        log_info "GPU $gpu_index 기대값 (75%): ${EXPECTED_VALUE}W"

        # 실제값과 기대값 비교 (오차범위: ±5%)
        if [[ "$EXPECTED_VALUE" != "N/A" ]]; then
            DIFF=$(echo "scale=1; $POWER_LIMIT_VALUE - $EXPECTED_VALUE" | bc 2>/dev/null || echo "0")
            DIFF_ABS=$(echo "${DIFF#-}")

            if (( $(echo "$DIFF_ABS <= $MAX_VALUE * 0.05" | bc -l) )); then
                log_pass "GPU $gpu_index Power Limit 설정 정상 (오차: ${DIFF}W)"
            else
                log_fail "GPU $gpu_index Power Limit 설정 불일치 (설정: ${POWER_LIMIT_VALUE}W, 기대: ${EXPECTED_VALUE}W)"
                return 1
            fi
        fi
    else
        log_warn "GPU $gpu_index Max Power Limit을 확인할 수 없습니다"
    fi
}

################################################################################
# 함수: nvidia-persistenced 상태 확인
################################################################################

check_nvidia_persistenced() {
    log_header "5. nvidia-persistenced 상태 확인"

    # 프로세스 확인
    if pgrep -x "nvidia-persistenced" > /dev/null; then
        log_pass "nvidia-persistenced 실행 중"
        PS_INFO=$(ps aux | grep nvidia-persistenced | grep -v grep)
        log_info "프로세스 정보: $PS_INFO"
    else
        log_warn "nvidia-persistenced가 실행 중이지 않습니다"
    fi

    # systemd service 확인 (있는 경우)
    if systemctl list-unit-files | grep -q nvidia-persistenced; then
        if systemctl is-active --quiet nvidia-persistenced; then
            log_pass "nvidia-persistenced service 실행 중"
        else
            log_warn "nvidia-persistenced service 활성화되지 않음"
        fi
    else
        log_info "nvidia-persistenced systemd service 없음 (자동 시작됨)"
    fi
}

################################################################################
# 함수: 종합 보고서
################################################################################

print_summary() {
    log_header "검증 완료 - 종합 보고서"

    echo ""
    echo "총 검사 항목: $TOTAL_CHECKS"
    echo -e "  ${GREEN}통과: $PASSED_CHECKS${NC}"
    echo -e "  ${RED}실패: $FAILED_CHECKS${NC}"
    echo -e "  ${YELLOW}경고: $WARNING_CHECKS${NC}"
    echo ""

    # 최종 상태 결정
    if [[ $FAILED_CHECKS -eq 0 ]]; then
        echo -e "${GREEN}✓ 모든 검증을 통과했습니다!${NC}"
        echo ""
        log_info "GPU Power Limit 75% 설정이 정상적으로 적용되었습니다."
        return 0
    elif [[ $FAILED_CHECKS -le 2 ]]; then
        echo -e "${YELLOW}⚠ 일부 검증에서 경고 또는 실패가 있습니다.${NC}"
        echo ""
        echo "필요한 조치:"
        echo "1. Service 파일 내용 확인"
        echo "2. systemctl daemon-reload && systemctl restart gpu-power-limit.service 실행"
        echo "3. nvidia-smi -i [GPU_INDEX] -pl [POWER_LIMIT_VALUE] 명령어로 수동 적용"
        return 1
    else
        echo -e "${RED}✗ 여러 검증 항목에서 실패했습니다.${NC}"
        echo ""
        echo "필요한 조치:"
        echo "1. Service 파일 재설정"
        echo "2. systemctl daemon-reload 실행"
        echo "3. systemctl enable --now gpu-power-limit.service 실행"
        return 2
    fi
}

################################################################################
# 메인 함수
################################################################################

main() {
    echo ""
    echo "╔════════════════════════════════════════════════════════════════════════════╗"
    echo "║           GPU Power Capping 설정 검증 스크립트                            ║"
    echo "║                  nvidia-persistenced Power Limit 75%                       ║"
    echo "╚════════════════════════════════════════════════════════════════════════════╝"
    echo ""
    echo "시작 시간: $(date '+%Y-%m-%d %H:%M:%S')"
    echo ""

    # 인자 처리
    GPU_INDEX="${1:-}"

    # 사전 조건 확인
    if ! check_prerequisites; then
        echo ""
        echo -e "${RED}✗ 사전 조건 확인 실패${NC}"
        exit 2
    fi

    # 각 검증 수행
    check_service_file
    check_service_status
    check_gpu_power_limit "$GPU_INDEX"
    check_nvidia_persistenced

    # 종합 보고서 출력
    RESULT=$(print_summary)
    echo "$RESULT"

    echo ""
    echo "종료 시간: $(date '+%Y-%m-%d %H:%M:%S')"
    echo ""

    if [[ $FAILED_CHECKS -eq 0 ]]; then
        return 0
    elif [[ $FAILED_CHECKS -le 2 ]]; then
        return 1
    else
        return 2
    fi
}

# 스크립트 실행
main "$@"
EXIT_CODE=$?

exit $EXIT_CODE
