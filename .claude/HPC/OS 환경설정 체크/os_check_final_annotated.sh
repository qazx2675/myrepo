#!/bin/bash
# OS 점검 및 환경설정 자동화 스크립트 (gossh 기반)
#
# 이번에 반영된 변경사항 요약:
#   1) report_ldap_info : LDAP 값이 전부 동일하면 1줄만, 2종류 이상이면 케이스별 파일로 분리
#   2) report_splunk_info : SPLUNK 값이 전부 동일하면 1줄만, 2종류 이상이면 "값: N대" 카운트
#   4) run_check_script : (target_list, output_file) 파라미터화 → 재사용 가능하게 변경
#   5) run_post_apply_check (신규) : 설정 적용 후 SETTING_TARGET_LIST를 재점검
#   6) report_setting_check_fail (신규) : 재점검 결과 파일에서 FAIL만 grep
# 그 외 로직(접두사 분류, 메시지 케이스 6~9, cleanup 등)은 원본 그대로입니다.
# mock 관련 코드(가짜 gossh, select_user 더미값)는 실 서비스 스크립트가 아니므로
# 이 파일에는 포함하지 않았습니다 — select_user/get_dhcp_info/check_ev_extra는
# 다시 TODO 상태로 되돌려 두었습니다.
#
# 이후 추가 반영된 변경사항:
#   7) report_ldap_info / report_splunk_info : 출력 형식을 "INFO ldap <값>" /
#      "INFO HPC Splunk <값>" / "FAIL ... confirmation required"로 변경.
#      LDAP 값은 공백으로 구분된 여러 토큰이어도 첫 토큰만 화면에 표시.
#   8) parse_pm_result : "===== 분류 요약 =====" 화면 출력 블록 제거 (아래 단계에서
#      이미 자체 요약을 출력하므로 중복). 배열 계산 로직은 그대로.
#   9) run_os_check : gossh -pm 옵션 순서 수정 — "-w ... -pm"이면 정상 동작하지
#      않아 "-pm -w ..."로 변경.
#  10) apply_os_setting/apply_extra_setting : 실행되는 스크립트 목록은 그대로 두고,
#      로그에 "(y: 기본 환경설정)"/"(set: 추가 환경설정)" 표시를 붙여 y/set이 각각
#      무엇을 실행하는지 화면에서 구분되게 함.
#  11) main() : TARGET_LIST(${user}.txt)를 gossh -w에 원본 그대로 넘기고 있어서,
#      파일에 빈 줄이 섞여 있으면 gossh가 이를 빈 호스트명 타겟으로 인식해 접속을
#      시도하고 그 실패 라인이 PM_RAW에 남아 ping/refused/anaconda 패턴에 우연히
#      걸리면서 DOWN_HOSTS에 유령 항목이 잡히는 버그가 있었음(실제 대수보다 접속
#      가능+불가 합이 커지는 증상). 빈 줄/CR을 제거한 사본을 만들어 TARGET_LIST가
#      그 사본을 가리키도록 수정 — 이후 모든 gossh -w 호출과 ALL_HOSTS 계산이 항상
#      같은 정제된 목록을 보게 됨.
#  12) run_info_check (신규) / report_ldap_info / report_splunk_info : check.res_${user}
#      (run.sh 결과)는 LDAP/SPLUNK가 정상이면 "OK"만 찍혀서 실제 값을 알 수 없었음.
#      그래서 이제 check.res_${user}를 grep하지 않고, LDAP/SPLUNK 실제 값을 조사하는
#      별도 스크립트(INFO_CHECK_SH, 경로는 [수정필요])를 gossh로 따로 실행해
#      INFO_CHECK_FILE(check.res_${user}_info)을 만들고, 두 report 함수가 그 파일을 본다.
#
# 이후 추가 반영된 변경사항 (2026-08-26):
#  13) build_check_target_list (신규) : 접속 불가(DOWN_HOSTS) / svrauto 접근 불가
#      (NOSVRAUTO_HOSTS) 호스트를 OS 체크(run_check_script)/정보 조사(run_info_check)
#      대상에서 제외. 기존에는 이미 접속 불가로 분류된 호스트까지 다시 gossh로
#      접속 시도하느라 전체 수행 시간이 늘어지고, 그 결과가 check.res_*에도 섞여
#      들어갔음.
#  14) report_ldap_info : grep -i "ldap"에 "FAIL ldap_site ..."처럼 LDAP 정보와
#      무관한 항목까지 걸려 들어오던 것을, "INFO ldap ..."/"FAIL ldap ..." 두
#      케이스만 인정하도록 필터 추가.
#  15) check_lacp/report_lacp_info 삭제 : LACP는 OS 체크스크립트에 없어도 되는
#      내용이라는 요청에 따라 관련 함수와 main() 호출부를 제거.
#  16) report_kernel_info (신규) : 대상 서버 커널 버전을 출력. 전체 동일하면 1줄 +
#      "전체 동일" 문구만, 아니면 값별 "값 : N대"와 대상 호스트 목록을 출력.
#  17) build_message_case6to9 : "모든 서버 정상" 케이스에 대상 호스트 목록
#      (print_horizontal) 출력 추가 — 다른 케이스와 동일한 형식으로 통일.
#  18) report_dhcp_info : 판단 기준을 DOWN_HOSTS 전체 → PINGX_HOSTS(ping 불가,
#      파워오프로 추정)로 좁힘. ssh 거부(REFUSED)/설치중(ANACONDA)만 있는 경우는
#      네트워크가 살아있는 상태라 DHCP 조사가 불필요하므로 메시지 자체가 안 뜨도록 함.

RUN_SH_DIR="/path/to/check"
SETTING_DIR="/path/to/setting"
RCLOCAL_SH="/path/to/setting/rclocal.sh"

# [신규] [수정필요] check.res_${user}(run.sh 결과)는 LDAP/SPLUNK 설정에 문제가 없으면
# 그냥 "OK"만 찍혀서 실제 값(어떤 LDAP infra/SPLUNK type인지)을 알 수 없다는 문제가
# 있었다. 그래서 LDAP/SPLUNK 실제 값은 check.res_${user}에서 grep하는 대신, 이 값을
# 조사해서 출력해주는 별도 스크립트를 gossh로 직접 실행해서 얻는다. 아래 경로를
# 실제 조사 스크립트 경로로 채워 넣으세요.
INFO_CHECK_SH="/path/to/check/info_check.sh"

CLASSIFY_CMD="hostname"

# [수정필요] 기존에 쓰시던 user 선택 로직을 여기에 그대로 붙여넣으세요.
# 결과값이 반드시 전역 변수 `user`에 들어가야 합니다 (main()의 user 공백 체크가
# 이 값을 그대로 사용합니다).
select_user() {
    user=""   # <-- 여기에 기존 user 선택 함수 붙여넣기 (결과값이 user 에 들어가면 됨)
}

TARGET_LIST=""            # ${user}.txt를 빈 줄/CR 제거해서 정제한 사본을 가리킨다 (main 참고)
CLEAN_TARGET_LIST=""      # 위 정제 사본의 실제 파일 경로 (cleanup에서 삭제용)
CHECK_RES_FILE=""         # check.res_${user} (최초 OS 체크 결과)
INFO_CHECK_FILE=""        # check.res_${user}_info (INFO_CHECK_SH 실행 결과 — LDAP/SPLUNK 실제값 조사용)
CHECK_TARGET_LIST=""      # OS 체크/정보 조사 대상만 추린 임시 목록 파일 (접속가능 + svrauto 정상)
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

# [신규] 출력 가독성을 위한 색상 처리 — 불가/FAIL=빨강, 정상/완료(INFO)=초록,
# 그 외 경고/확인필요=노랑. 터미널이 아닌 곳(파일 리다이렉트 등)으로 출력할 때는
# 이스케이프 문자가 그대로 섞여 나오지 않도록 자동으로 색을 끈다(NO_COLOR=1로도
# 강제로 끌 수 있음).
if [ -t 1 ] && [ -z "${NO_COLOR:-}" ]; then
    C_RED='\033[0;31m'; C_GREEN='\033[0;32m'; C_YELLOW='\033[0;33m'; C_RESET='\033[0m'
else
    C_RED=''; C_GREEN=''; C_YELLOW=''; C_RESET=''
fi
red()    { printf '%b%s%b\n' "${C_RED}"    "$*" "${C_RESET}"; }
green()  { printf '%b%s%b\n' "${C_GREEN}"  "$*" "${C_RESET}"; }
yellow() { printf '%b%s%b\n' "${C_YELLOW}" "$*" "${C_RESET}"; }

# [신규] 이미 "FAIL ..."/"INFO ..."로 시작하는 완성 문자열(ldap/splunk 리포트 값 등)을
# 내용에 따라 자동으로 색칠한다.
color_by_status() {
    case "$1" in
        FAIL*) red "$1" ;;
        INFO*) green "$1" ;;
        *) yellow "$1" ;;
    esac
}

# [수정금지] 접두사 분류 기준. 이번 요청과 무관하니 절대 건드리지 마세요.
is_prefix_host() {
    [[ "$1" =~ ^(s2h|s3h|s4h|sh|c|h) ]]
}

# [수정됨] gossh -pm 옵션은 -w(대상목록)보다 앞에 와야 정상 동작한다(뒤에 두면 명령이
# 정상 실행되지 않음) — 그래서 "gossh -w ... "cmd" -pm"에서 "gossh -pm -w ... "cmd""로 순서를 바꿨다.
run_os_check() {
    PM_RAW="/tmp/pm_raw_${user}.$$"

    green "[INFO] gossh 분류 점검 실행 중..."
    gossh -pm -w "${TARGET_LIST}" "${CLASSIFY_CMD}" > "${PM_RAW}" 2>&1

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

# [수정금지] UP/DOWN, PREFIX_UP/PREFIX_DOWN/NONPREFIX_HOSTS 분류 로직 자체는
# build_message_case6to9 등 여러 함수가 이 결과(배열)에 의존하므로 건드리지 마세요.
# [수정됨] "===== 분류 요약 =====" 화면 출력 블록은 제거했습니다 — 아래 작업 단계(OS
# 체크/설정 적용)에서 이미 자체 요약을 출력하므로 여기서 한 번 더 보여줄 필요가 없다는
# 요청 반영. 배열 계산(위) 자체는 그대로 남아있어 다른 함수들에 영향 없습니다.
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

    green "[INFO] ${RUN_SH_DIR}/run.sh 실행 중..."
    gossh -w "${target_list}" "bash ${RUN_SH_DIR}/run.sh" -script > "${output_file}" 2>&1
    green "[INFO] 체크 결과 저장 완료 : ${output_file}"
    echo
}

# [신규] check.res_${user}(run.sh 결과)는 LDAP/SPLUNK가 정상이면 "OK"만 찍혀서 실제
# 값(어떤 infra/type인지)을 알 수 없어, report_ldap_info/report_splunk_info가 더 이상
# check.res_${user}를 보지 않고 이 함수가 만드는 INFO_CHECK_FILE을 대신 본다.
# run_check_script와 동일한 (target_list, output_file) 파라미터 패턴이지만 실행하는
# 스크립트가 다르므로(INFO_CHECK_SH) 별도 함수로 뺐다.
# [연계] main()에서 최초 OS 체크(run_check_script) 직후, report_ldap_info/
# report_splunk_info 호출 전에 실행되어야 합니다.
run_info_check() {
    local target_list="$1"
    local output_file="$2"

    green "[INFO] ${INFO_CHECK_SH} 실행 중 (LDAP/SPLUNK 정보 조사)..."
    gossh -w "${target_list}" "bash ${INFO_CHECK_SH}" -script > "${output_file}" 2>&1
    green "[INFO] LDAP/SPLUNK 정보 조사 결과 저장 완료 : ${output_file}"
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
    green "[INFO] 설정 적용 후 재점검 실행 중..."
    run_check_script "${SETTING_TARGET_LIST}" "${POST_APPLY_CHECK_FILE}"
}

# [수정됨] LDAP 값이 전부 동일하면 1줄만 출력합니다. 값이 2종류 이상이면
# 케이스별로 별도 파일에 떨어뜨리고, 화면에는 요약(케이스 수 / 파일명 / 값 /
# 대상 호스트)만 출력합니다.
# [수정됨] 출력 형식을 "INFO ldap <값>"/"FAIL ldap ..."으로 변경했습니다. 값은
# "infra site"처럼 공백으로 구분된 여러 토큰일 수 있는데, 요청에 따라 첫 번째
# 토큰(예: infra)만 보여줍니다 — 파일에 저장되는 원본 값(value: ...)은 전체를 그대로 둡니다.
# [수정필요] 아래 3가지는 이번에 임의로 정한 것이라 확인/조정이 필요합니다:
#   - 파일명 규칙: ldap_case{N}_${user} — N은 값이 등장한 순서 기준(빈도순 아님)
#   - 저장 위치: 스크립트 실행 위치(cwd)에 그대로 생성됨. 별도 결과 디렉토리로
#     보내야 한다면 outfile 경로를 수정하세요.
#   - 파일 내부 포맷: "value: ..." / "hosts: ..." 2줄 — 다른 도구가 이 파일을
#     파싱해서 쓴다면 포맷이 맞는지 확인 필요.
# [연계] 이 함수 안에서만 쓰이는 로컬 상태이고, 다른 함수와의 연계는 없습니다.
# [수정됨] check.res_${user}(run.sh 결과)는 LDAP이 정상이면 "OK"만 찍혀서 실제 값을
# 알 수 없어, 이제 CHECK_RES_FILE이 아니라 별도로 조사한 INFO_CHECK_FILE(run_info_check
# 실행 결과)에서 값을 가져옵니다.
# [수정됨] INFO_CHECK_SH의 실제 출력이 "hostname : INFO ldap infra" 형태로, host와
# 값 사이가 공백 1칸이 아니라 콜론(:)으로 구분되고(콜론 앞뒤 공백 유무는 호스트마다
# 다를 수 있음), 값 자체에 이미 "INFO ldap ..."/"FAIL ldap ..."이 완성된 문자열로
# 들어있습니다. 그래서 더 이상 값을 잘라 재조합하지 않고, 콜론 뒤 텍스트를 있는
# 그대로 출력합니다(ldap_display_token 재가공 제거 — 이중으로 "INFO ldap"이
# 붙던 버그 수정).
# [수정됨] "infra" 뒤에 인프라와 무관한 텍스트가 더 붙어 나오는 경우가 있어, 앞 3
# 토큰(예: "INFO ldap infra")만 남기고 나머지는 버립니다.
report_ldap_info() {
    [ -f "${INFO_CHECK_FILE}" ] || return 0

    echo "===== LDAP 정보 (접속 가능 서버) ====="

    local line host value
    declare -A ldap_hosts_by_value
    local case_order=()

    while IFS= read -r line; do
        if [[ "${line}" =~ ^([^[:space:]]+)[[:space:]]*:[[:space:]]*(.*)$ ]]; then
            host="${BASH_REMATCH[1]}"
            value="${BASH_REMATCH[2]}"
        else
            host=$(echo "${line}" | awk '{print $1}')
            value="${line#"${host}" }"
        fi
        contains "${host}" "${UP_HOSTS[@]}" || continue

        # [수정됨] grep -i "ldap"에는 "FAIL ldap_site ..."처럼 LDAP 정보와 무관한
        # 항목까지 같이 걸려 들어왔다. LDAP 정보로 인정하는 것은 "INFO ldap ..." /
        # "FAIL ldap ..." 두 케이스뿐이므로(ldap 뒤가 공백이거나 줄 끝), 그 외
        # 토큰(ldap_site 등)은 여기서 버린다. 이후 처리 규칙은 동일하다.
        [[ "${value}" =~ ^(INFO|FAIL)[[:space:]]+[Ll][Dd][Aa][Pp]([[:space:]]|$) ]] || continue

        set -- ${value}
        value="${1:-}"
        [ -n "${2:-}" ] && value="${value} ${2}"
        [ -n "${3:-}" ] && value="${value} ${3}"

        if [ -z "${ldap_hosts_by_value[${value}]:-}" ]; then
            case_order+=("${value}")
        fi
        ldap_hosts_by_value["${value}"]="${ldap_hosts_by_value[${value}]:-} ${host}"
    done < <(grep -i "ldap" "${INFO_CHECK_FILE}")

    if [ ${#case_order[@]} -eq 0 ]; then
        red "FAIL ldap infra confirmation required"
    elif [ ${#case_order[@]} -eq 1 ]; then
        color_by_status "${case_order[0]}"
    else
        yellow "[경고] LDAP 값이 ${#case_order[@]}종류로 서로 다릅니다 — 케이스별 파일로 분리합니다."
        local idx=1
        local outfile
        for value in "${case_order[@]}"; do
            outfile="ldap_case${idx}_${user}"   # [수정필요] 네이밍/저장 위치 확인
            {
                echo "value: ${value}"
                echo "hosts:${ldap_hosts_by_value[${value}]}"
            } > "${outfile}"
            case "${value}" in
                FAIL*) red    "  - case${idx} (${outfile}) : ${value}  =>  대상:${ldap_hosts_by_value[${value}]}" ;;
                INFO*) green  "  - case${idx} (${outfile}) : ${value}  =>  대상:${ldap_hosts_by_value[${value}]}" ;;
                *)     yellow "  - case${idx} (${outfile}) : ${value}  =>  대상:${ldap_hosts_by_value[${value}]}" ;;
            esac
            idx=$((idx + 1))
        done
    fi

    echo "====================================="
    echo
}

# [수정됨] SPLUNK 값이 전부 동일하면 1줄만 출력합니다. 값이 2종류 이상이면
# 각 항목(고유값)별로 "값: N대" 형태로 대수만 집계해서 출력합니다
# (LDAP과 달리 파일로는 분리하지 않음 — 요청하신 그대로입니다).
# [수정됨] 출력 형식을 "INFO HPC Splunk <값>"/"FAIL Splunk type confirmation required"로 변경.
# UP_HOSTS 필터를 안 거는 것(주석 처리된 contains 라인)은 원본 그대로
# 유지했습니다.
# [연계] 다른 함수와의 연계는 없습니다.
# [수정됨] check.res_${user}(run.sh 결과)는 SPLUNK가 정상이면 "OK"만 찍혀서 실제 값을
# 알 수 없어, 이제 CHECK_RES_FILE이 아니라 별도로 조사한 INFO_CHECK_FILE(run_info_check
# 실행 결과)에서 값을 가져옵니다.
# [신규] splunk_display_value는 원본 값이 "splunk typeA"처럼 grep 키워드(splunk)가
# 라벨로 남아있는 경우 그 라벨만 제거한다("typeA") — LDAP과 달리 나머지 값은
# 전부(첫 토큰만이 아니라) 그대로 보여준다.
splunk_display_value() {
    local v="$1"
    echo "${v#[Ss][Pp][Ll][Uu][Nn][Kk] }"
}

report_splunk_info() {
    [ -f "${INFO_CHECK_FILE}" ] || return 0

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
    done < <(grep -i "splunk" "${INFO_CHECK_FILE}")

    if [ ${#case_order[@]} -eq 0 ]; then
        red "FAIL Splunk type confirmation required"
    elif [ ${#case_order[@]} -eq 1 ]; then
        green "INFO HPC Splunk $(splunk_display_value "${case_order[0]}")"
    else
        for value in "${case_order[@]}"; do
            green "INFO HPC Splunk $(splunk_display_value "${value}") (${splunk_count[${value}]}대)"
        done
    fi

    echo "======================="
    echo
}

# [신규] 대상 서버들의 커널 버전을 출력합니다. 모든 대상의 커널 버전이 동일하면
# 그 버전 1줄과 "전체 동일" 문구만 출력하고, 2종류 이상이면 값별로 "값 : N대"와
# 해당 호스트 목록을 출력합니다 (SPLUNK와 동일한 집계 방식, 파일 분리는 하지 않음).
# [수정필요] 커널 버전 값은 check.res_${user}(run.sh 결과, CHECK_RES_FILE)에
# "hostname kernel_version" 형태(공백 또는 콜론 구분)로 한 줄씩 있다고 가정하고
# grep -i "kernel"로 뽑아옵니다. run.sh 결과의 실제 라인 포맷이 다르면 파싱 부분을
# 맞게 조정해주세요.
# [연계] 다른 함수와의 연계는 없습니다.
report_kernel_info() {
    [ -f "${CHECK_RES_FILE}" ] || return 0

    echo "===== 커널 버전 ====="

    local line host value
    declare -A kernel_hosts_by_value
    local case_order=()

    while IFS= read -r line; do
        if [[ "${line}" =~ ^([^[:space:]]+)[[:space:]]*:[[:space:]]*(.*)$ ]]; then
            host="${BASH_REMATCH[1]}"
            value="${BASH_REMATCH[2]}"
        else
            host=$(echo "${line}" | awk '{print $1}')
            value="${line#"${host}" }"
        fi

        if [ -z "${kernel_hosts_by_value[${value}]:-}" ]; then
            case_order+=("${value}")
        fi
        kernel_hosts_by_value["${value}"]="${kernel_hosts_by_value[${value}]:-} ${host}"
    done < <(grep -i "kernel" "${CHECK_RES_FILE}")

    if [ ${#case_order[@]} -eq 0 ]; then
        yellow "FAIL kernel version confirmation required"
    elif [ ${#case_order[@]} -eq 1 ]; then
        green "INFO kernel ${case_order[0]} (전체 동일)"
    else
        yellow "[경고] 커널 버전이 ${#case_order[@]}종류로 서로 다릅니다."
        for value in "${case_order[@]}"; do
            yellow "  - ${value} : $(( $(echo "${kernel_hosts_by_value[${value}]}" | wc -w) ))대  =>  대상:${kernel_hosts_by_value[${value}]}"
        done
    fi

    echo "======================"
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
        green "(FAIL 항목 없음)"
    else
        red "${found}"
    fi
    echo "====================================="
    echo
}

# [신규] 접속 불가(DOWN_HOSTS) 또는 svrauto 계정 접근 불가(NOSVRAUTO_HOSTS) 호스트를
# OS 체크/정보 조사 대상에서 제외한 목록을 만든다.
# 기존에는 run_check_script/run_info_check에 원본 TARGET_LIST를 그대로 넘겨서, 이미
# 접속 불가로 분류된 호스트까지 gossh가 다시 붙어보고 타임아웃 날 때까지 기다리느라
# 전체 수행 시간이 길어지고, 그 실패 출력이 결과 파일(check.res_*)에도 섞여 들어갔다.
# 분류 단계(parse_pm_result)에서 이미 알고 있는 정보를 그대로 활용해 대상에서 뺀다.
# [연계] main()에서 run_os_check 직후 호출되며, 결과 파일(CHECK_TARGET_LIST)을
# run_check_script / run_info_check가 target_list 인자로 받습니다.
build_check_target_list() {
    CHECK_TARGET_LIST="/tmp/check_target_${user}.$$"
    : > "${CHECK_TARGET_LIST}"

    local h
    for h in "${UP_HOSTS[@]}"; do
        contains "$h" "${NOSVRAUTO_HOSTS[@]}" && continue
        echo "$h" >> "${CHECK_TARGET_LIST}"
    done

    local cnt
    cnt=$(grep -cv '^[[:space:]]*$' "${CHECK_TARGET_LIST}")
    green "[INFO] 체크 대상 (접속가능 + svrauto 정상) : ${cnt} 대"
    if [ ${#DOWN_HOSTS[@]} -gt 0 ]; then
        yellow "[INFO] 접속 불가로 제외 (${#DOWN_HOSTS[@]} 대) : $(print_horizontal "${DOWN_HOSTS[@]}")"
    fi
    if [ ${#NOSVRAUTO_HOSTS[@]} -gt 0 ]; then
        yellow "[INFO] svrauto 접근 불가로 제외 (${#NOSVRAUTO_HOSTS[@]} 대) : $(print_horizontal "${NOSVRAUTO_HOSTS[@]}")"
    fi
    echo

    [ "${cnt}" -eq 0 ] && return 1
    return 0
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
    green "[INFO] 설정 적용 대상 (접속가능 + svrauto 정상) : ${cnt} 대"
    echo "$(print_horizontal $(cat "${SETTING_TARGET_LIST}"))"
    echo

    [ "${cnt}" -eq 0 ] && return 1
    return 0
}

# [수정금지] 기본 환경설정 적용 로직(호출하는 스크립트 3종) 자체는 이번 요청과 무관합니다.
# [수정됨] y/set 둘 다 이 함수를 거쳐가는데 화면상 뭘 실행하는지 구분이 안 된다는
# 지적이 있어, "(y: 기본 환경설정)" 표시를 각 단계 로그에 붙였습니다 — 실행되는
# 스크립트 목록 자체(setting_insert.sh/rclocal.sh/appl_change.sh)는 그대로입니다.
# [연계] main()에서 이 함수 호출 직후 run_post_apply_check가 이어서 호출됩니다.
apply_os_setting() {
    green "[INFO] (y: 기본 환경설정) setting_insert.sh 실행..."
    gossh -w "${SETTING_TARGET_LIST}" "bash ${SETTING_DIR}/setting_insert.sh" -script

    green "[INFO] (y: 기본 환경설정) rclocal.sh 실행..."
    gossh -w "${SETTING_TARGET_LIST}" "bash ${RCLOCAL_SH}" -script

    green "[INFO] (y: 기본 환경설정) appl_change.sh 실행..."
    gossh -w "${SETTING_TARGET_LIST}" "bash ${SETTING_DIR}/appl_change.sh" -script

    green "[INFO] 기본 환경설정 적용 완료 (setting_insert.sh + rclocal.sh + appl_change.sh)"
    echo
}

# [수정금지] 추가 환경설정 적용 로직 자체는 이번 요청과 무관합니다.
# [수정됨] apply_os_setting과 동일하게, set에서만 추가로 도는 setting.sh 단계임을
# 로그에 "(set: 추가 환경설정)"으로 명시했습니다.
# [연계] main()에서 이 함수 호출 직후 run_post_apply_check가 이어서 호출됩니다.
apply_extra_setting() {
    apply_os_setting

    green "[INFO] (set: 추가 환경설정) setting.sh 실행..."
    gossh -w "${SETTING_TARGET_LIST}" "bash ${SETTING_DIR}/setting.sh" -script

    green "[INFO] 추가 환경설정 적용 완료 (기본 환경설정 3종 + setting.sh)"
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

# [수정됨] "모든 서버 정상" 케이스(전체 ALL_HOSTS 정상, DOWN_HOSTS 없음)에서
# 다른 케이스(PREFIX_DOWN/PREFIX_UP)와 달리 대상 호스트 목록(print_horizontal)이
# 빠져 있어서 추가했습니다. 그 외 문구/조건문은 원본 그대로입니다.
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
        print_horizontal "${ALL_HOSTS[@]}"
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

# [수정됨] DHCP 정보는 "파워오프로 추정되는 서버"(ping 자체가 안 되는 PINGX_HOSTS)가
# 있을 때만 필요하다는 요청에 따라, 판단 기준을 DOWN_HOSTS 전체에서 PINGX_HOSTS로
# 좁혔습니다. REFUSED_HOSTS(ssh 거부 — 네트워크는 살아있음)나 ANACONDA_HOSTS(설치
# 진행중 — 마찬가지로 네트워크 살아있음)만 있는 경우는 DHCP 조사가 필요 없어
# 이 메시지 자체가 출력되지 않습니다. get_dhcp_info 호출 대상도 ALL_HOSTS에서
# PINGX_HOSTS로 좁혔습니다 (get_dhcp_info 구현 자체는 여전히 TODO 상태).
report_dhcp_info() {
    [ ${#PINGX_HOSTS[@]} -eq 0 ] && return 0

    echo "-------------------------------------------"
    echo "dhcp정보는 아래와 같이 등록되어있습니다."
    get_dhcp_info "${PINGX_HOSTS[@]}"
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

# [수정됨] CLEAN_TARGET_LIST(정제된 TARGET_LIST 사본) 삭제 추가.
cleanup() {
    [ -n "${PM_RAW}" ] && rm -f "${PM_RAW}"
    [ -n "${CHECK_TARGET_LIST}" ] && rm -f "${CHECK_TARGET_LIST}"
    [ -n "${SETTING_TARGET_LIST}" ] && rm -f "${SETTING_TARGET_LIST}"
    [ -n "${CLEAN_TARGET_LIST}" ] && rm -f "${CLEAN_TARGET_LIST}"
}
trap cleanup EXIT

main() {
    select_user   # [수정필요] select_user 본문을 채우기 전엔 아래 공백 체크에서 바로 종료됩니다.

    if [ -z "${user}" ]; then
        red "[ERROR] user 값이 비어 있습니다."
        exit 1
    fi

    TARGET_LIST="${user}.txt"
    CHECK_RES_FILE="check.res_${user}"
    INFO_CHECK_FILE="check.res_${user}_info"

    if [ ! -f "${TARGET_LIST}" ]; then
        red "[ERROR] 대상 목록 파일이 없습니다 : ${TARGET_LIST}"
        exit 1
    fi

    # [신규] TARGET_LIST 원본에 빈 줄/CR이 섞여 있으면 gossh -w가 이를 빈 호스트명
    # 타겟으로 인식해서 접속을 시도하고, 그 실패 결과 라인이 PM_RAW에 남아 ping/refused/
    # anaconda 패턴에 우연히 걸리면서 DOWN_HOSTS에 유령 항목이 잡히는 문제가 있었다
    # (실제 대수보다 접속가능+접속불가 합이 더 크게 나오는 증상의 원인). ALL_HOSTS는
    # 이미 이 필터를 적용해서 만들고 있었는데 gossh에 실제로 넘기는 파일은 원본
    # 그대로였던 게 불일치의 원인 — 정제된 사본을 만들어 TARGET_LIST가 이후부터 그
    # 사본을 가리키게 해서, gossh 호출과 ALL_HOSTS 계산이 항상 같은 정제된 목록을
    # 보도록 통일한다.
    CLEAN_TARGET_LIST="/tmp/target_clean_${user}.$$"
    grep -v '^[[:space:]]*$' "${TARGET_LIST}" | tr -d '\r' > "${CLEAN_TARGET_LIST}"
    TARGET_LIST="${CLEAN_TARGET_LIST}"

    echo "user        : ${user}"
    echo "target list : ${TARGET_LIST}"
    echo

    # [수정금지] OS 체크 진행 여부 프롬프트/분기 자체는 이번 요청과 무관합니다.
    # [수정됨] run_check_script 호출부에 (target_list, output_file) 인자 추가.
    # [수정됨] LDAP/SPLUNK 실제 값은 check.res_${user}로는 안 나와서(정상이면 OK만
    # 찍힘), run_info_check로 INFO_CHECK_SH를 따로 실행해 INFO_CHECK_FILE을 만들고
    # report_ldap_info/report_splunk_info가 그 파일을 보도록 함.
    read -rp "OS 체크를 진행하시겠습니까? (y/n) : " ans_check
    case "${ans_check}" in
        y|Y)
            run_os_check
            # [수정됨] 접속 불가 / svrauto 접근 불가 호스트는 체크 대상에서 제외한
            # 목록(CHECK_TARGET_LIST)으로 실행한다 (기존에는 TARGET_LIST 전체).
            if build_check_target_list; then
                run_check_script "${CHECK_TARGET_LIST}" "${CHECK_RES_FILE}"
                run_info_check "${CHECK_TARGET_LIST}" "${INFO_CHECK_FILE}"
                report_ldap_info
                report_splunk_info
                report_kernel_info
            else
                yellow "[WARN] 체크 대상이 없어 OS 체크/정보 조사를 건너뜁니다."
            fi
            ;;
        *)
            yellow "[INFO] OS 체크를 진행하지 않고 종료합니다."
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
                yellow "[WARN] 설정 적용 대상이 없어 건너뜁니다."
            fi
            ;;
        set|SET)
            if filter_svrauto_targets; then
                apply_extra_setting
                run_post_apply_check
                report_setting_check_fail "${POST_APPLY_CHECK_FILE}"
            else
                yellow "[WARN] 설정 적용 대상이 없어 건너뜁니다."
            fi
            ;;
        *)
            yellow "[INFO] 환경설정 수정은 건너뜁니다."
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
