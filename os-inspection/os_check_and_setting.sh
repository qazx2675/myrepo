#!/bin/bash
# OS 점검 및 환경설정 자동화 스크립트 (gossh 기반)
#==============================================================================
# [수정필요 1] 경로 변수
# - 아래 3개 경로를 실제 환경에 맞게 수정하세요.
# - RUN_SH_DIR : run.sh 가 있는 디렉토리
# - SETTING_DIR : setting_insert.sh / appl_change.sh / setting.sh 공통 디렉토리
# - RCLOCAL_SH : rclocal.sh 전체 경로 (GPU 구분은 스크립트 내부에서 처리됨)
#==============================================================================
RUN_SH_DIR="/path/to/check"
SETTING_DIR="/path/to/setting"
RCLOCAL_SH="/path/to/setting/rclocal.sh"

#==============================================================================
# [수정필요 2] 분류 메시지용 명령어
# gossh 의 -pm 옵션은 명령어와 무관하게 4대 분류 메시지를 붙여줍니다.
# 따라서 run.sh 를 두 번 돌리지 않도록 가벼운 명령(hostname)으로 분류만 받습니다.
# run.sh 자체를 -pm 으로 돌리고 싶으면 아래를 다음과 같이 바꾸세요:
# CLASSIFY_CMD="bash ${RUN_SH_DIR}/run.sh"
#==============================================================================
CLASSIFY_CMD="hostname"

#==============================================================================
# [수정필요 3] user 선택 함수
#==============================================================================
select_user() {
    user="" # <-- 여기에 기존 user 선택 함수 붙여넣기 (결과값이 user 에 들어가면 됨)
}

#==============================================================================
# 전역 변수 (수정 불필요)
#==============================================================================
TARGET_LIST=""          # ${user}.txt
CHECK_RES_FILE=""        # check.res_${user}
SETTING_TARGET_LIST=""   # 설정 적용 대상만 추린 임시 목록 파일
PM_RAW=""                # gossh -pm 원본 출력 임시 파일

ALL_HOSTS=()        # 전체 대상 호스트
NOSVRAUTO_HOSTS=()  # /user/svrauto 안 붙는 서버
PINGX_HOSTS=()      # ping 불가 서버
REFUSED_HOSTS=()    # 22 port refused 서버
ANACONDA_HOSTS=()   # anaconda 설치 진행중 서버
DOWN_HOSTS=()       # 접속 불가 서버 (ping X + refused + anaconda)
UP_HOSTS=()         # 접속 가능 서버

PREFIX_UP=()        # c|h|sh|s2h|s3h|s4h 이면서 접속 가능
PREFIX_DOWN=()      # c|h|sh|s2h|s3h|s4h 이면서 접속 불가
NONPREFIX_HOSTS=()  # 위 접두사에 해당하지 않는 호스트 전체

#==============================================================================
# 공통 유틸
#==============================================================================
print_horizontal() {
    local arr=("$@")
    if [ ${#arr[@]} -eq 0 ]; then
        echo "(없음)"
    else
        echo "${arr[*]}"
    fi
}

contains() {
    local needle="$1"; shift
    local i
    for i in "$@"; do
        [ "$i" == "$needle" ] && return 0
    done
    return 1
}

is_prefix_host() {
    [[ "$1" =~ ^(s2h|s3h|s4h|sh|c|h) ]]
}

#==============================================================================
# 1) OS 배포 상태 점검 : gossh -pm 으로 4대 분류 메시지 수집
#==============================================================================
run_os_check() {
    PM_RAW="/tmp/pm_raw_${user}.$$"
    echo "[INFO] gossh 분류 점검 실행 중..."
    gossh -w "${TARGET_LIST}" "${CLASSIFY_CMD}" -pm > "${PM_RAW}" 2>&1
    echo
    echo "===== gossh 분류 결과 원본 ====="
    cat "${PM_RAW}"
    echo "================================"
    echo
    parse_pm_result
}

#==============================================================================
# [수정필요 4] gossh -pm 출력 파싱
#==============================================================================
PAT_NOSVRAUTO="svrauto"
PAT_PINGX="ping"
PAT_REFUSED="refused"
PAT_ANACONDA="anaconda"

parse_pm_result() {
    local h
    mapfile -t ALL_HOSTS < <(grep -v '^[[:space:]]*$' "${TARGET_LIST}" | tr -d '\r')
    mapfile -t NOSVRAUTO_HOSTS < <(grep -i "${PAT_NOSVRAUTO}" "${PM_RAW}" | awk '{print $1}' | sort -u)
    mapfile -t PINGX_HOSTS < <(grep -i "${PAT_PINGX}" "${PM_RAW}" | awk '{print $1}' | sort -u)
    mapfile -t REFUSED_HOSTS < <(grep -i "${PAT_REFUSED}" "${PM_RAW}" | awk '{print $1}' | sort -u)
    mapfile -t ANACONDA_HOSTS < <(grep -i "${PAT_ANACONDA}" "${PM_RAW}" | awk '{print $1}' | sort -u)

    DOWN_HOSTS=()
    for h in "${PINGX_HOSTS[@]}" "${REFUSED_HOSTS[@]}" "${ANACONDA_HOSTS[@]}"; do
        [ -z "$h" ] && continue
        contains "$h" "${DOWN_HOSTS[@]}" || DOWN_HOSTS+=("$h")
    done

    UP_HOSTS=()
    for h in "${ALL_HOSTS[@]}"; do
        contains "$h" "${DOWN_HOSTS[@]}" || UP_HOSTS+=("$h")
    done

    PREFIX_UP=(); PREFIX_DOWN=(); NONPREFIX_HOSTS=()
    for h in "${ALL_HOSTS[@]}"; do
        if is_prefix_host "$h"; then
            if contains "$h" "${DOWN_HOSTS[@]}"; then
                PREFIX_DOWN+=("$h")
            else
                PREFIX_UP+=("$h")
            fi
        else
            NONPREFIX_HOSTS+=("$h")
        fi
    done

    echo "===== 분류 요약 ====="
    echo "- /user/svrauto 안 붙는 서버 : $(print_horizontal "${NOSVRAUTO_HOSTS[@]}")"
    echo "- ping 불가 서버 : $(print_horizontal "${PINGX_HOSTS[@]}")"
    echo "- 22 port refused 서버 : $(print_horizontal "${REFUSED_HOSTS[@]}")"
    echo "- anaconda 설치 진행중 서버 : $(print_horizontal "${ANACONDA_HOSTS[@]}")"
    echo "---------------------"
    echo "- 접속 가능 : ${#UP_HOSTS[@]} 대"
    echo "- 접속 불가 : ${#DOWN_HOSTS[@]} 대"
    echo "====================="
    echo
}

#==============================================================================
# 2) 체크 스크립트 실행 : run.sh (-script 로 결과값만 수집)
#==============================================================================
run_check_script() {
    echo "[INFO] ${RUN_SH_DIR}/run.sh 실행 중..."
    gossh -w "${TARGET_LIST}" "bash ${RUN_SH_DIR}/run.sh" -script > "${CHECK_RES_FILE}" 2>&1
    echo "[INFO] 체크 결과 저장 완료 : ${CHECK_RES_FILE}"
    echo
}

#==============================================================================
# 3) 설정 적용 대상 필터링 (접속 가능 AND /user/svrauto 마운트 정상)
#==============================================================================
filter_svrauto_targets() {
    SETTING_TARGET_LIST="/tmp/setting_target_${user}.$$"
    : > "${SETTING_TARGET_LIST}"
    local h
    for h in "${UP_HOSTS[@]}"; do
        contains "$h" "${NOSVRAUTO_HOSTS[@]}" && continue
        echo "$h" >> "${SETTING_TARGET_LIST}"
    done
    local cnt
    cnt=$(grep -cv '^[[:space:]]*$' "${SETTING_TARGET_LIST}")
    echo "[INFO] 설정 적용 대상 (접속가능 + svrauto 정상) : ${cnt} 대"
    echo "$(print_horizontal $(cat "${SETTING_TARGET_LIST}"))"
    echo
    [ "${cnt}" -eq 0 ] && return 1
    return 0
}

#==============================================================================
# 4) OS 환경설정 적용 (y 선택 시)
#==============================================================================
apply_os_setting() {
    echo "[INFO] setting_insert.sh 실행..."
    gossh -w "${SETTING_TARGET_LIST}" "bash ${SETTING_DIR}/setting_insert.sh" -script
    echo "[INFO] rclocal.sh 실행..."
    gossh -w "${SETTING_TARGET_LIST}" "bash ${RCLOCAL_SH}" -script
    echo "[INFO] appl_change.sh 실행..."
    gossh -w "${SETTING_TARGET_LIST}" "bash ${SETTING_DIR}/appl_change.sh" -script
    echo "[INFO] 기본 환경설정 적용 완료"
    echo
}

#==============================================================================
# 5) 추가 설정 적용 (set 선택 시 : 4번 전체 + setting.sh)
#==============================================================================
apply_extra_setting() {
    apply_os_setting
    echo "[INFO] setting.sh 추가 실행..."
    gossh -w "${SETTING_TARGET_LIST}" "bash ${SETTING_DIR}/setting.sh" -script
    echo "[INFO] 추가 환경설정 적용 완료"
    echo
}

#==============================================================================
# [수정필요 5] DHCP 정보 수집 함수
#==============================================================================
get_dhcp_info() {
    local hosts=("$@")
    :
}

#==============================================================================
# [수정필요 6] ev 호스트 추가 점검 함수
#==============================================================================
check_ev_extra() {
    local hosts=("$@")
    :
}

#==============================================================================
# 6~9) 담당자 안내 메시지 생성
#==============================================================================
build_message_case6to9() {
    if [ ${#PREFIX_DOWN[@]} -gt 0 ]; then
        echo "-------------------------------------------"
        echo "안녕하세요 드***담당자 님"
        echo "하기서버 ${#PREFIX_DOWN[@]}대는 OS 배포이후 올라오지않는 것 같습니다."
        echo "점검부탁드립니다."
        echo "나머지서버들은 OS 설치 완료하였습니다."
        print_horizontal "${PREFIX_UP[@]}"
        echo "-------------------------------------------"
        echo
    fi
    if [ ${#PREFIX_UP[@]} -gt 0 ] && [ ${#PREFIX_DOWN[@]} -eq 0 ]; then
        echo "-------------------------------------------"
        echo "안녕하세요 드***담당자 님"
        echo "하기서버 ${#PREFIX_UP[@]}대 OS 설치 완료하였습니다."
        echo "점검 필요하신지 확인부탁드립니다."
        print_horizontal "${PREFIX_UP[@]}"
        echo "-------------------------------------------"
        echo
    fi
    if [ ${#ALL_HOSTS[@]} -gt 0 ] && [ ${#DOWN_HOSTS[@]} -eq 0 ]; then
        echo "-------------------------------------------"
        echo "안녕하세요. C* 담당자 님"
        echo "${#ALL_HOSTS[@]}대 OS 설치 완료하였습니다."
        echo "감사합니다."
        echo "-------------------------------------------"
        echo
    fi
    if [ ${#ALL_HOSTS[@]} -gt 0 ] && [ ${#UP_HOSTS[@]} -eq 0 ]; then
        echo "-------------------------------------------"
        echo "안녕하세요. 00 엔지니어 님"
        echo "하기서버 ${#NONPREFIX_HOSTS[@]}대 OS 배포이후 올라오지 않는 것 같습니다."
        echo "점검 부탁드립니다."
        echo "-------------------------------------------"
        echo
    fi
}

report_dhcp_info() {
    [ ${#DOWN_HOSTS[@]} -eq 0 ] && return 0
    echo "-------------------------------------------"
    echo "dhcp정보는 아래와 같이 등록되어있습니다."
    get_dhcp_info "${ALL_HOSTS[@]}"
    echo "-------------------------------------------"
    echo
}

check_usb0_required() {
    [ -f "${CHECK_RES_FILE}" ] || return 0
    local usb0_hosts=()
    mapfile -t usb0_hosts < <(grep -i "usb0" "${CHECK_RES_FILE}" | awk '{print $1}' | sort -u)
    [ ${#usb0_hosts[@]} -eq 0 ] && return 0
    echo "-------------------------------------------"
    echo "하기서버들은 usb0 disabled 가 필요합니다."
    print_horizontal "${usb0_hosts[@]}"
    echo "-------------------------------------------"
    echo
}

check_pice_bios() {
    local h
    local pice_hosts=()
    for h in "${ALL_HOSTS[@]}"; do
        [[ "$h" =~ ^pice ]] && pice_hosts+=("$h")
    done
    [ ${#pice_hosts[@]} -eq 0 ] && return 0
    echo "-------------------------------------------"
    echo "안녕하세요. L** 엔지니어 님"
    echo "하기서버 ${#pice_hosts[@]}대 BIOS 점검 부탁드립니다."
    echo "감사합니다."
    print_horizontal "${pice_hosts[@]}"
    echo "-------------------------------------------"
    echo
}

report_ev_hosts() {
    local h
    local ev_hosts=()
    for h in "${ALL_HOSTS[@]}"; do
        [[ "$h" == *ev* ]] && ev_hosts+=("$h")
    done
    [ ${#ev_hosts[@]} -eq 0 ] && return 0
    echo "-------------------------------------------"
    echo "[ev 호스트 추가 점검]"
    check_ev_extra "${ev_hosts[@]}"
    echo "-------------------------------------------"
    echo
}

cleanup() {
    [ -n "${PM_RAW}" ] && rm -f "${PM_RAW}"
    [ -n "${SETTING_TARGET_LIST}" ] && rm -f "${SETTING_TARGET_LIST}"
}
trap cleanup EXIT

main() {
    select_user
    if [ -z "${user}" ]; then
        echo "[ERROR] user 값이 비어 있습니다."
        exit 1
    fi
    TARGET_LIST="${user}.txt"
    CHECK_RES_FILE="check.res_${user}"
    if [ ! -f "${TARGET_LIST}" ]; then
        echo "[ERROR] 대상 목록 파일이 없습니다 : ${TARGET_LIST}"
        exit 1
    fi
    echo "user : ${user}"
    echo "target list : ${TARGET_LIST}"
    echo

    read -rp "OS 체크를 진행하시겠습니까? (y/n) : " ans_check
    case "${ans_check}" in
        y|Y) run_os_check; run_check_script ;;
        *) echo "[INFO] OS 체크를 진행하지 않고 종료합니다."; exit 0 ;;
    esac

    read -rp "OS 환경설정을 수정하시겠습니까? (y/n/set) : " ans_set
    case "${ans_set}" in
        y|Y)
            if filter_svrauto_targets; then apply_os_setting; else echo "[WARN] 설정 적용 대상이 없어 건너뜁니다."; fi ;;
        set|SET)
            if filter_svrauto_targets; then apply_extra_setting; else echo "[WARN] 설정 적용 대상이 없어 건너뜁니다."; fi ;;
        *) echo "[INFO] 환경설정 수정은 건너뜁니다." ;;
    esac

    echo
    echo "############### 결과 리포트 ###############"
    echo
    build_message_case6to9
    report_dhcp_info
    check_usb0_required
    check_pice_bios
    report_ev_hosts
    echo "###########################################"
}

main "$@"
