#!/bin/bash
# OS 점검 및 환경설정 자동화 스크립트 (gossh 기반)
#
# 이번에 반영된 변경사항 요약:
#   1) report_ldap_info : LDAP 값이 전부 동일하면 1줄만, 2종류 이상이면 케이스별 파일로 분리
#   2) report_splunk_info : SPLUNK 값이 전부 동일하면 1줄만, 2종류 이상이면 "값: N대" 카운트
#   3) check_lacp : bond_mode=802.3ad 인 호스트만 코멘트 출력 (실사용 로직 반영, 그 외는 TODO)
#   4) run_check_script : (target_list, output_file) 파라미터화 → 재사용 가능하게 변경
#   5) run_post_apply_check (신규) : 설정 적용 후 SETTING_TARGET_LIST를 재점검
#   6) report_setting_check_fail (신규) : 재점검 결과 파일에서 FAIL만 grep
# 그 외 로직(접두사 분류, 메시지 케이스 6~9, cleanup 등)은 원본 그대로입니다.
# mock 관련 코드(가짜 gossh, select_user 더미값)는 실 서비스 스크립트가 아니므로
# 이 파일에는 포함하지 않았습니다 — select_user/get_dhcp_info/check_ev_extra는
# 다시 TODO 상태로 되돌려 두었습니다.

RUN_SH_DIR="/path/to/check"
SETTING_DIR="/path/to/setting"
RCLOCAL_SH="/path/to/setting/rclocal.sh"

CLASSIFY_CMD="hostname"

# [수정필요] 기존에 쓰시던 user 선택 로직을 여기에 그대로 붙여넣으세요.
# 결과값이 반드시 전역 변수 `user`에 들어가야 합니다 (main()의 user 공백 체크가
# 이 값을 그대로 사용합니다).
select_user() {
    user=""   # <-- 여기에 기존 user 선택 함수 붙여넣기 (결과값이 user 에 들어가면 됨)
}

TARGET_LIST=""            # ${user}.txt
CHECK_RES_FILE=""         # check.res_${user} (최초 OS 체크 결과)
SETTING_TARGET_LIST=""    # 설정 적용 대상만 추린 임시 목록 파일
# [연계] 설정 적용(apply_os_setting/apply_extra_setting) 완료 후 run_post_apply_check가
# 이 변수에 재점검 결과 파일 경로를 채웁니다. report_setting_check_fail은 이 변수를
# 인자로 받아서 사용합니다.
POST_APPLY_CHECK_FILE=""  # check.res_${user}_postapply (설정 적용 후 재점검 결과)
PM_RAW=""                 # gossh -pm 원본 출력 임시 파일

ALL_HOSTS=()
NOSVRAUTO_HOSTS=()
PINGX_HOSTS=()
REFUSED_HOSTS=()
ANACONDA_HOSTS=()
DOWN_HOSTS=()
UP_HOSTS=()

PREFIX_UP=()
PREFIX_DOWN=()
NONPREFIX_HOSTS=()

# [수정금지] 이번 요청과 무관한 기존 유틸리티 함수입니다.
print_horizontal() {
    local arr=("$@")
    if [ ${#arr[@]} -eq 0 ]; then
        echo "(없음)"
    else
        echo "${arr[*]}"
    fi
}

# [수정금지] 이번 요청과 무관한 기존 유틸리티 함수입니다.
contains() {
    local needle="$1"; shift
    local i
    for i in "$@"; do
        [ "$i" == "$needle" ] && return 0
    done
    return 1
}

# [수정금지] 접두사 분류 기준. 이번 요청과 무관하니 절대 건드리지 마세요.
is_prefix_host() {
    [[ "$1" =~ ^(s2h|s3h|s4h|sh|c|h) ]]
}

# [수정금지] gossh -pm 호출 및 결과 출력 로직 자체는 이번 요청과 무관합니다.
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

# [수정금지] 분류 패턴/기준. 이번 요청과 무관하니 손대지 마세요.
PAT_NOSVRAUTO="svrauto"
PAT_PINGX="ping"
PAT_REFUSED="refused"
PAT_ANACONDA="anaconda"

# [수정금지] UP/DOWN, PREFIX_UP/PREFIX_DOWN/NONPREFIX_HOSTS 분류 로직 전체.
# 이번 요청과 무관하며, build_message_case6to9 등 여러 함수가 이 결과에 의존하므로
# 절대 건드리지 마세요.
parse_pm_result() {
    local h

    mapfile -t ALL_HOSTS < <(grep -v '^[[:space:]]*$' "${TARGET_LIST}" | tr -d '\r')

    mapfile -t NOSVRAUTO_HOSTS < <(grep -i "${PAT_NOSVRAUTO}" "${PM_RAW}" | awk '{print $1}' | sort -u)
    mapfile -t PINGX_HOSTS     < <(grep -i "${PAT_PINGX}"     "${PM_RAW}" | awk '{print $1}' | sort -u)
    mapfile -t REFUSED_HOSTS   < <(grep -i "${PAT_REFUSED}"   "${PM_RAW}" | awk '{print $1}' | sort -u)
    mapfile -t ANACONDA_HOSTS  < <(grep -i "${PAT_ANACONDA}"  "${PM_RAW}" | awk '{print $1}' | sort -u)

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
    echo "- ping 불가 서버            : $(print_horizontal "${PINGX_HOSTS[@]}")"
    echo "- 22 port refused 서버      : $(print_horizontal "${REFUSED_HOSTS[@]}")"
    echo "- anaconda 설치 진행중 서버 : $(print_horizontal "${ANACONDA_HOSTS[@]}")"
    echo "---------------------"
    echo "- 접속 가능 : ${#UP_HOSTS[@]} 대"
    echo "- 접속 불가 : ${#DOWN_HOSTS[@]} 대"
    echo "====================="
    echo
}

# [수정됨] target_list / output_file 을 인자로 받도록 일반화했습니다.
# 기존에는 TARGET_LIST/CHECK_RES_FILE 전역 변수를 직접 참조했지만, 설정 적용 후
# 재점검(다른 대상 목록 · 다른 결과 파일)에도 동일 로직을 재사용하기 위해
# 파라미터로 뺐습니다. 동작 자체(gossh -w ... -script 호출 방식)는 그대로입니다.
# [연계] main()의 최초 OS 체크 단계에서 run_check_script "${TARGET_LIST}" "${CHECK_RES_FILE}"
# 로 호출되고, run_post_apply_check가 이 함수를 재사용해서
# run_check_script "${SETTING_TARGET_LIST}" "${POST_APPLY_CHECK_FILE}" 로도 호출합니다.
run_check_script() {
    local target_list="$1"
    local output_file="$2"

    echo "[INFO] ${RUN_SH_DIR}/run.sh 실행 중..."
    gossh -w "${target_list}" "bash ${RUN_SH_DIR}/run.sh" -script > "${output_file}" 2>&1
    echo "[INFO] 체크 결과 저장 완료 : ${output_file}"
    echo
}

# [신규] 설정 적용(apply_os_setting/apply_extra_setting) 완료 후, 설정 적용
# 대상(SETTING_TARGET_LIST)만 골라 run_check_script를 재사용해 재점검합니다.
# 결과 파일은 최초 OS체크 결과(check.res_${user})와 겹치지 않도록
# "_postapply" 접미사를 붙였습니다.
# [수정필요] 파일명 규칙(check.res_${user}_postapply)은 이번에 임의로 정한
# 것이라, 기존에 쓰시던 명명 규칙/보관 위치(예: 특정 로그 디렉토리)가 따로
# 있다면 맞게 바꿔주세요.
# [연계] 이 함수가 만든 POST_APPLY_CHECK_FILE을 report_setting_check_fail이
# 그대로 인자로 받아서 사용합니다. main()에서 apply_os_setting/apply_extra_setting
# 바로 뒤, report_setting_check_fail 호출 바로 앞에 위치해야 합니다.
run_post_apply_check() {
    POST_APPLY_CHECK_FILE="check.res_${user}_postapply"
    echo "[INFO] 설정 적용 후 재점검 실행 중..."
    run_check_script "${SETTING_TARGET_LIST}" "${POST_APPLY_CHECK_FILE}"
}

# [수정됨] LDAP 값이 전부 동일하면 1줄만 출력합니다. 값이 2종류 이상이면
# 케이스별로 별도 파일에 떨어뜨리고, 화면에는 요약(케이스 수 / 파일명 / 값 /
# 대상 호스트)만 출력합니다.
# [수정필요] 아래 3가지는 이번에 임의로 정한 것이라 확인/조정이 필요합니다:
#   - 파일명 규칙: ldap_case{N}_${user} — N은 값이 등장한 순서 기준(빈도순 아님)
#   - 저장 위치: 스크립트 실행 위치(cwd)에 그대로 생성됨. 별도 결과 디렉토리로
#     보내야 한다면 outfile 경로를 수정하세요.
#   - 파일 내부 포맷: "value: ..." / "hosts: ..." 2줄 — 다른 도구가 이 파일을
#     파싱해서 쓴다면 포맷이 맞는지 확인 필요.
# [연계] 이 함수 안에서만 쓰이는 로컬 상태이고, 다른 함수와의 연계는 없습니다.
report_ldap_info() {
    [ -f "${CHECK_RES_FILE}" ] || return 0

    echo "===== LDAP 정보 (접속 가능 서버) ====="

    local line host value
    declare -A ldap_hosts_by_value
    local case_order=()

    while IFS= read -r line; do
        host=$(echo "${line}" | awk '{print $1}')
        contains "${host}" "${UP_HOSTS[@]}" || continue
        value="${line#"${host}" }"
        if [ -z "${ldap_hosts_by_value[${value}]:-}" ]; then
            case_order+=("${value}")
        fi
        ldap_hosts_by_value["${value}"]="${ldap_hosts_by_value[${value}]:-} ${host}"
    done < <(grep -i "ldap" "${CHECK_RES_FILE}")

    if [ ${#case_order[@]} -eq 0 ]; then
        echo "(해당 없음)"
    elif [ ${#case_order[@]} -eq 1 ]; then
        echo "${case_order[0]}"
    else
        echo "[경고] LDAP 값이 ${#case_order[@]}종류로 서로 다릅니다 — 케이스별 파일로 분리합니다."
        local idx=1
        local outfile
        for value in "${case_order[@]}"; do
            outfile="ldap_case${idx}_${user}"   # [수정필요] 네이밍/저장 위치 확인
            {
                echo "value: ${value}"
                echo "hosts:${ldap_hosts_by_value[${value}]}"
            } > "${outfile}"
            echo "  - case${idx} (${outfile}) : ${value}  =>  대상:${ldap_hosts_by_value[${value}]}"
            idx=$((idx + 1))
        done
    fi

    echo "====================================="
    echo
}

# [수정됨] SPLUNK 값이 전부 동일하면 1줄만 출력합니다. 값이 2종류 이상이면
# 각 항목(고유값)별로 "값: N대" 형태로 대수만 집계해서 출력합니다
# (LDAP과 달리 파일로는 분리하지 않음 — 요청하신 그대로입니다).
# UP_HOSTS 필터를 안 거는 것(주석 처리된 contains 라인)은 원본 그대로
# 유지했습니다.
# [연계] 다른 함수와의 연계는 없습니다.
report_splunk_info() {
    [ -f "${CHECK_RES_FILE}" ] || return 0

    echo "===== SPLUNK 정보 ====="

    local line host value
    declare -A splunk_count
    local case_order=()

    while IFS= read -r line; do
        host=$(echo "${line}" | awk '{print $1}')
        # contains "${host}" "${UP_HOSTS[@]}" || continue
        value="${line#"${host}" }"
        if [ -z "${splunk_count[${value}]:-}" ]; then
            case_order+=("${value}")
            splunk_count["${value}"]=0
        fi
        splunk_count["${value}"]=$(( splunk_count["${value}"] + 1 ))
    done < <(grep -i "splunk" "${CHECK_RES_FILE}")

    if [ ${#case_order[@]} -eq 0 ]; then
        echo "(해당 없음)"
    elif [ ${#case_order[@]} -eq 1 ]; then
        echo "${case_order[0]}"
    else
        for value in "${case_order[@]}"; do
            echo "${value}: ${splunk_count[${value}]}대"
        done
    fi

    echo "======================="
    echo
}

# [신규] "환경설정 점검결과"는 최초 OS 체크가 아니라, 설정 적용
# (apply_os_setting/apply_extra_setting) 완료 후의 재점검 결과를 가리키는
#것으로 확인되어, 대상 파일을 인자로 받는 형태로 만들었습니다.
# [수정필요] FAIL 판정 기준이 정말 "FAIL" 문자열 포함 여부(grep -i FAIL)가
# 맞는지, 그리고 대상 파일이 정말 run_post_apply_check가 만드는
# check.res_${user}_postapply(=run.sh 재실행 결과)가 맞는지 확인 필요합니다.
# (다른 형태의 점검 결과 파일을 가리키신 거라면 target_file 인자만 바꿔서
# 재사용 가능하도록 만들어 뒀습니다.)
# [연계] main()에서 run_post_apply_check 바로 다음에
# report_setting_check_fail "${POST_APPLY_CHECK_FILE}" 형태로 호출됩니다.
report_setting_check_fail() {
    local target_file="$1"
    [ -f "${target_file}" ] || return 0

    echo "===== 환경설정 점검결과 (FAIL) ====="
    local found
    found=$(grep -i "FAIL" "${target_file}")
    if [ -z "${found}" ]; then
        echo "(FAIL 항목 없음)"
    else
        echo "${found}"
    fi
    echo "====================================="
    echo
}

# [수정됨] LACP(bond_mode=802.3ad)를 실제로 쓰는 호스트만 check.res_${user}
# (최초 OS 체크 결과)에서 걸러서 코멘트를 출력합니다. LACP를 안 쓰거나 정보가
# 없는 호스트는 아무 출력도 하지 않습니다 (요청하신 그대로).
# [수정필요] check.res_${user} 안에 실제로 bond_mode=... 형태의 항목이
# run.sh 점검 결과에 포함되는지, 그리고 "802.3ad"가 LACP를 뜻하는 유일한
# 값인지(다른 LACP 모드 표기가 있는지) 확인이 필요합니다.
check_lacp() {
    local hosts=("$@")
    [ -f "${CHECK_RES_FILE}" ] || return 0

    local h line bond_value
    for h in "${hosts[@]}"; do
        line=$(grep -i "bond_mode" "${CHECK_RES_FILE}" | awk -v host="$h" '$1==host {print; exit}')
        [ -z "${line}" ] && continue
        bond_value="${line#"${h}" }"
        if [[ "${bond_value}" == *"802.3ad"* ]]; then
            echo "${h} : LACP(${bond_value}) 사용 중"
        fi
    done
}

# [수정금지] report_lacp_info 자체는 이번 요청과 무관 (내부에서 호출하는
# check_lacp만 바뀜).
report_lacp_info() {
    [ ${#UP_HOSTS[@]} -eq 0 ] && return 0

    echo "===== LACP 체크 결과 ====="
    check_lacp "${UP_HOSTS[@]}"
    echo "=========================="
    echo
}

# [수정금지] 설정 적용 대상 필터링 로직. 이번 요청과 무관합니다.
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

# [수정금지] 기본 환경설정 적용 로직 자체는 이번 요청과 무관합니다.
# [연계] main()에서 이 함수 호출 직후 run_post_apply_check가 이어서 호출됩니다.
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

# [수정금지] 추가 환경설정 적용 로직 자체는 이번 요청과 무관합니다.
# [연계] main()에서 이 함수 호출 직후 run_post_apply_check가 이어서 호출됩니다.
apply_extra_setting() {
    apply_os_setting

    echo "[INFO] setting.sh 추가 실행..."
    gossh -w "${SETTING_TARGET_LIST}" "bash ${SETTING_DIR}/setting.sh" -script

    echo "[INFO] 추가 환경설정 적용 완료"
    echo
}

# [수정필요] DHCP 정보 조회 로직 미구현 (TODO). report_dhcp_info가
# ALL_HOSTS를 인자로 넘겨 호출합니다. 결과값을 어떻게 보여줄지(echo로 바로
# 출력할지, 별도 변수/파일에 담을지)는 기존에 쓰시던 방식에 맞춰 구현해주세요.
get_dhcp_info() {
    local hosts=("$@")
    :
}

# [수정필요] ev 호스트 추가 점검 로직 미구현 (TODO). report_ev_hosts가
# ALL_HOSTS 중 이름에 "ev"가 포함된 호스트만 걸러서 인자로 넘겨 호출합니다.
check_ev_extra() {
    local hosts=("$@")
    :
}

# [수정금지] 메시지 케이스 6~9 문구/조건문. 이번 요청과 전혀 무관하니
# 절대 손대지 마세요.
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

# [수정금지] 이번 요청과 무관 (get_dhcp_info 호출부만 TODO 상태).
report_dhcp_info() {
    [ ${#DOWN_HOSTS[@]} -eq 0 ] && return 0

    echo "-------------------------------------------"
    echo "dhcp정보는 아래와 같이 등록되어있습니다."
    get_dhcp_info "${ALL_HOSTS[@]}"
    echo "-------------------------------------------"
    echo
}

# [수정금지] 이번 요청과 무관합니다.
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

# [수정금지] 이번 요청과 무관합니다.
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

# [수정금지] 이번 요청과 무관 (check_ev_extra 호출부만 TODO 상태).
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

# [수정금지] 이번 요청과 무관합니다.
cleanup() {
    [ -n "${PM_RAW}" ] && rm -f "${PM_RAW}"
    [ -n "${SETTING_TARGET_LIST}" ] && rm -f "${SETTING_TARGET_LIST}"
}
trap cleanup EXIT

main() {
    select_user   # [수정필요] select_user 본문을 채우기 전엔 아래 공백 체크에서 바로 종료됩니다.

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

    echo "user        : ${user}"
    echo "target list : ${TARGET_LIST}"
    echo

    # [수정금지] OS 체크 진행 여부 프롬프트/분기 자체는 이번 요청과 무관합니다.
    # [수정됨] run_check_script 호출부에 (target_list, output_file) 인자 추가.
    read -rp "OS 체크를 진행하시겠습니까? (y/n) : " ans_check
    case "${ans_check}" in
        y|Y)
            run_os_check
            run_check_script "${TARGET_LIST}" "${CHECK_RES_FILE}"
            report_ldap_info
            report_splunk_info
            report_lacp_info
            ;;
        *)
            echo "[INFO] OS 체크를 진행하지 않고 종료합니다."
            exit 0
            ;;
    esac

    # [수정됨] apply_os_setting/apply_extra_setting 직후 run_post_apply_check로
    # 재점검하고, 그 결과 파일을 report_setting_check_fail에 넘겨서 FAIL만 출력.
    # (기존에는 이 블록에 설정 적용만 있었고 재점검/FAIL 리포트가 없었습니다.)
    read -rp "OS 환경설정을 수정하시겠습니까? (y/n/set) : " ans_set
    case "${ans_set}" in
        y|Y)
            if filter_svrauto_targets; then
                apply_os_setting
                run_post_apply_check
                report_setting_check_fail "${POST_APPLY_CHECK_FILE}"
            else
                echo "[WARN] 설정 적용 대상이 없어 건너뜁니다."
            fi
            ;;
        set|SET)
            if filter_svrauto_targets; then
                apply_extra_setting
                run_post_apply_check
                report_setting_check_fail "${POST_APPLY_CHECK_FILE}"
            else
                echo "[WARN] 설정 적용 대상이 없어 건너뜁니다."
            fi
            ;;
        *)
            echo "[INFO] 환경설정 수정은 건너뜁니다."
            ;;
    esac

    # [수정금지] 결과 리포트(케이스 6~9 등) 블록 자체는 이번 요청과 무관합니다.
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
