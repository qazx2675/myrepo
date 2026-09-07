#!/bin/bash
###############################################################################
# run.sh — 빌드된 ldap-config-engine 바이너리 실행을 돕는 대화형 스크립트
#
# deploy_ldap.sh 와 달리 -infra 를 명령행 인자로 미리 정해두지 않고, 실행 중에
# 메뉴로 골라 정합니다. 검증(gossh 로 ldap_check.sh 실행)은 하지 않습니다 —
# 적용까지만 하고, 검증은 deploy_ldap.sh -check-only 또는 ldap_check 를 쓰십시오.
#
# 흐름
#   1. 작업 계정 선택 (select_user_context) → {user}.txt 가 작업 대상이 됩니다.
#      {user}.txt 형식은 conf/assets.txt 와 동일합니다: "hostname<TAB>site" 한 줄에 하나.
#   2. ldap_config.conf 에 정의된 인프라 목록에서 대상 인프라를 메뉴로 선택합니다.
#   3. DRY-RUN 으로 먼저 무엇이 바뀔지 보여주고, 확인을 받은 뒤에만 실제로 적용합니다.
#      사이트별 설정은 엔진이 작업 대상 파일의 site 컬럼을 보고 알아서 나눠 처리합니다.
###############################################################################

set -u
cd "$(dirname "$0")" || exit 2

ENGINE="../bin/ldap-config-engine"
CONFIG="${CONFIG:-../conf/ldap_config.conf}"

###############################################################################
# 1. 작업 계정 선택
#
#   여기서 고른 이름으로 작업 대상 파일 {user}.txt 를 이 스크립트와 같은 디렉터리
#   (scripts/) 에서 찾습니다. 파일 형식은 conf/assets.txt 와 동일합니다.
#
#   [실환경 전용 로직 적용부] 실제 계정 목록으로 아래를 채워서 쓰십시오.
#   지금은 골격만 있고 목록이 비어 있어, 무엇을 입력하든 그 값이 그대로
#   계정명(=파일명)으로 쓰입니다.
###############################################################################

select_user_context()
{
    echo "=== LDAP 설정 자동화 — 실행 계정 선택 ==="
    echo "작업을 수행할 계정을 선택하십시오:"

    # TODO: 실제 환경의 계정 목록으로 채우십시오. 예:
    #   echo "  1) admin_user1"
    #   echo "  2) operator_user2"

    printf "선택 (번호 또는 계정명 직접 입력): "
    read -r USER_CHOICE

    case "$USER_CHOICE" in
        # TODO: 위 목록에 맞춰 번호 → 계정명 매핑을 채우십시오. 예:
        #   1) RUN_USER="admin_user1" ;;
        #   2) RUN_USER="operator_user2" ;;
        *) RUN_USER="$USER_CHOICE" ;;
    esac

    if [ -z "$RUN_USER" ]; then
        echo "오류: 계정을 지정하지 않았습니다."
        exit 2
    fi

    echo ">> 작업 대상 파일: ${RUN_USER}.txt"
}

###############################################################################
# 2. 대상 인프라 선택
#
#   ldap_config.conf 에 실제로 정의된 infra.<이름>.* 키를 훑어 메뉴로 보여줍니다.
#   목록에 없는 인프라는 애초에 고를 수 없으므로, 오타로 다른 인프라 값을
#   잘못 적용하는 사고를 막습니다.
###############################################################################

select_infra()
{
    local list name i=0

    list="$(sed -n 's/^[[:space:]]*infra\.\([^.]*\)\..*$/\1/p' "$CONFIG" | sort -u)"
    if [ -z "$list" ]; then
        echo "오류: $CONFIG 에 정의된 인프라가 없습니다."
        exit 2
    fi

    echo
    echo "=== 대상 인프라 선택 ==="
    INFRA_ARR=()
    while IFS= read -r name; do
        [ -z "$name" ] && continue
        i=$((i+1))
        INFRA_ARR+=("$name")
        echo "  $i) $name"
    done <<EOF
$list
EOF

    printf "선택 (번호): "
    read -r INFRA_CHOICE

    case "$INFRA_CHOICE" in
        ''|*[!0-9]*)
            echo "오류: 번호를 숫자로 입력하십시오."
            exit 2
            ;;
    esac
    if [ "$INFRA_CHOICE" -lt 1 ] || [ "$INFRA_CHOICE" -gt "$i" ]; then
        echo "오류: 목록에 없는 번호입니다."
        exit 2
    fi

    INFRA="${INFRA_ARR[$((INFRA_CHOICE-1))]}"
    echo ">> 선택된 인프라: $INFRA"
}

###############################################################################
# 사전 점검
###############################################################################

if [ ! -x "$ENGINE" ]; then
    echo "오류: 설정 엔진이 없습니다: $ENGINE"
    echo "      ../setup.sh 로 먼저 빌드하십시오."
    exit 2
fi

if [ ! -f "$CONFIG" ]; then
    echo "오류: 설정 파일이 없습니다: $CONFIG"
    exit 2
fi

select_user_context

ASSETS="${RUN_USER}.txt"
if [ ! -f "$ASSETS" ]; then
    echo "오류: 작업 대상 파일이 없습니다: $ASSETS"
    echo "      \"hostname<TAB>site\" 형식으로 이 디렉터리(scripts/)에 만들어 두십시오."
    echo "      (형식 예시는 ../conf/assets.txt.sample 참고)"
    exit 2
fi

select_infra

###############################################################################
# 작업 대상 요약
###############################################################################

TOTAL_HOSTS="$(grep -c $'\t' "$ASSETS" 2>/dev/null || echo 0)"
SITES_IN_FILE="$(cut -f2 "$ASSETS" 2>/dev/null | sed '/^[[:space:]]*$/d' | sort -u | tr '\n' ' ')"

echo
echo "=============================================================="
echo " 계정        : $RUN_USER"
echo " 대상 파일   : $ASSETS  (${TOTAL_HOSTS}줄)"
echo " 대상 사이트 : $SITES_IN_FILE"
echo " 대상 인프라 : $INFRA"
echo "=============================================================="

###############################################################################
# 3. DRY-RUN 으로 먼저 확인
###############################################################################

echo
echo "########## DRY-RUN — 무엇이 바뀔지 먼저 확인합니다 ##########"
"$ENGINE" -config "$CONFIG" -assets "$ASSETS" -infra "$INFRA" -dry-run
DRY_RC=$?

if [ "$DRY_RC" != "0" ]; then
    echo
    echo "경고: DRY-RUN 이 일부 실패를 보고했습니다 (rc=$DRY_RC)."
    echo "      위 내용을 확인하십시오. 그래도 진행할 수는 있습니다."
fi

###############################################################################
# 4. 확인 후에만 실제 적용
###############################################################################

echo
echo "위 DRY-RUN 결과대로 실제로 적용하시겠습니까?"
echo "이 작업은 인증(LDAP/sssd/nslcd)과 이름해석(DNS) 설정을 바꾸고 관련 서비스를 재시작합니다."
printf "계속하려면 정확히 'yes' 를 입력하십시오: "
read -r CONFIRM

if [ "$CONFIRM" != "yes" ]; then
    echo "취소되었습니다. 아무것도 바뀌지 않았습니다."
    exit 0
fi

echo
echo "########## 실제 적용 ##########"
"$ENGINE" -config "$CONFIG" -assets "$ASSETS" -infra "$INFRA"
APPLY_RC=$?

echo
echo "=============================================================="
echo " ★ 적용이 끝났습니다. 대상 서버 중 무작위로 몇 대에 직접 접속해"
echo "   설정이 실제로 반영됐는지 반드시 눈으로 확인하십시오."
echo
echo "   검증하려면:"
echo "     bash ../../ldap_check/ldap_check.sh   (노드에서 직접)"
echo "     또는 ./deploy_ldap.sh -infra $INFRA -check-only"
echo
echo "   되돌리려면:"
echo "     $ENGINE -assets $ASSETS -rollback -dry-run"
echo "=============================================================="

exit $APPLY_RC
