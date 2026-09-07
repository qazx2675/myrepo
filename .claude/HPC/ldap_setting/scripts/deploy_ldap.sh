#!/bin/bash
###############################################################################
# deploy_ldap.sh — 관리 노드에서 실행하는 배포·검증 래퍼
#
# 흐름
#   1. 작업 계정 선택 (select_user_context)
#   2. Go 설정 엔진 실행 → gossh 로 대상 노드에 설정 적용
#   3. 체크 스크립트를 전 노드에서 실행해 결과 집계
#
# 체크 스크립트 실행 경로
#   기본은 autofs 공유 경로(/user/asdf/ldap_check.sh)입니다.
#   마운트가 안 된 노드가 있으면 스크립트를 base64 로 /root 에 밀어 넣고 거기서 돌립니다.
#   (gossh 에는 파일 전송 기능이 없어 scp 대신 이 방식을 씁니다)
###############################################################################

set -u
cd "$(dirname "$0")" || exit 2

ENGINE="../bin/ldap-config-engine"
CHECK_LOCAL="${CHECK_LOCAL:-../../ldap_check/ldap_check.sh}"
CONFIG="${CONFIG:-../conf/ldap_config.conf}"
ASSETS="${ASSETS:-../conf/assets.txt}"
SHARED_CHECK="${SHARED_CHECK:-/user/asdf/ldap_check.sh}"
FALLBACK_CHECK="/root/ldap_check.sh"

INFRA=""
DRYRUN=0

usage()
{
    cat <<EOF
사용법: $0 -infra <인프라명> [옵션]

  -infra <이름>   대상 인프라 (필수)
  -dry-run        실제로 바꾸지 않고 바뀔 내용만 확인
  -config <경로>  설정 파일 (기본 $CONFIG)
  -assets <경로>  자산현황 파일 (기본 $ASSETS)
  -check-only     설정 적용을 건너뛰고 검증만 수행
  -h              이 도움말

환경변수 CONFIG / ASSETS / SHARED_CHECK 로도 지정할 수 있습니다.
EOF
}

CHECK_ONLY=0
while [ $# -gt 0 ]; do
    case "$1" in
        -infra)      INFRA="${2:-}"; shift 2 ;;
        -dry-run)    DRYRUN=1; shift ;;
        -config)     CONFIG="${2:-}"; shift 2 ;;
        -assets)     ASSETS="${2:-}"; shift 2 ;;
        -check-only) CHECK_ONLY=1; shift ;;
        -h|--help)   usage; exit 0 ;;
        *)           echo "알 수 없는 옵션: $1"; usage; exit 2 ;;
    esac
done

###############################################################################
# 1. 작업 계정 선택
#
#   여기서 고른 이름으로 대상 목록 파일 {user}.txt 를 찾습니다.
#   폐쇄망 환경에 맞는 계정 목록으로 아래 case 문을 채워서 쓰십시오.
###############################################################################

select_user_context()
{
    echo "=== LDAP 설정 자동화 ==="
    echo "작업을 수행할 권한 계정을 선택하십시오:"
    echo "  1) admin_user1"
    echo "  2) operator_user2"
    echo "  3) system_manager"
    printf "선택 (번호): "
    read -r USER_CHOICE

    case "$USER_CHOICE" in
        1) RUN_USER="admin_user1" ;;
        2) RUN_USER="operator_user2" ;;
        3) RUN_USER="system_manager" ;;
        *) echo "잘못된 선택입니다. default 로 진행합니다."; RUN_USER="default" ;;
    esac

    # [폐쇄망 전용] 권한 상승·환경변수 선언이 필요하면 이 아래에 추가하십시오.

    echo ">> 세션 권한: [$RUN_USER]"
}

###############################################################################
# 사전 점검
###############################################################################

if [ -z "$INFRA" ]; then
    echo "오류: -infra 를 지정하십시오."
    usage
    exit 2
fi

for f in "$CONFIG" "$ASSETS"; do
    if [ ! -f "$f" ]; then
        echo "오류: 파일이 없습니다: $f"
        exit 2
    fi
done

if ! command -v gossh >/dev/null 2>&1; then
    echo "오류: gossh 를 찾을 수 없습니다."
    exit 2
fi

select_user_context

TARGETS="${RUN_USER}.txt"
if [ ! -f "$TARGETS" ]; then
    echo "안내: 대상 목록 $TARGETS 이 없어 자산현황($ASSETS)의 호스트를 사용합니다."
    TARGETS="$(mktemp)"
    cut -f1 "$ASSETS" | sed '/^[[:space:]]*$/d; /^#/d' > "$TARGETS"
    TARGETS_TMP=1
fi

###############################################################################
# 2. 설정 적용
###############################################################################

if [ "$CHECK_ONLY" = "0" ]; then
    if [ ! -x "$ENGINE" ]; then
        echo "오류: 설정 엔진이 없습니다: $ENGINE  (setup.sh 로 먼저 빌드하십시오)"
        exit 2
    fi

    echo
    echo ">> 설정 엔진 실행 (인프라=$INFRA)"
    ENGINE_ARGS="-config $CONFIG -assets $ASSETS -infra $INFRA"
    [ "$DRYRUN" = "1" ] && ENGINE_ARGS="$ENGINE_ARGS -dry-run"

    # shellcheck disable=SC2086
    "$ENGINE" $ENGINE_ARGS
    ENGINE_RC=$?
    if [ "$ENGINE_RC" != "0" ]; then
        echo "경고: 설정 엔진이 실패를 보고했습니다 (rc=$ENGINE_RC). 검증은 계속합니다."
    fi
fi

###############################################################################
# 3. 전 노드 검증
#
#   공유 경로가 마운트된 노드는 거기서, 아닌 노드는 /root 로 밀어 넣고 실행합니다.
#   한 번의 gossh 호출로 두 경우를 모두 처리합니다.
###############################################################################

echo
echo ">> 전 노드 검증 실행"

if [ ! -f "$CHECK_LOCAL" ]; then
    echo "오류: 검증 스크립트를 찾을 수 없습니다: $CHECK_LOCAL"
    echo "      같은 저장소의 ../ldap_check/ 를 함께 내려받았는지 확인하십시오."
    echo "      다른 위치에 있으면 CHECK_LOCAL=<경로> 로 지정하십시오."
    exit 2
fi

CHECK_B64="$(base64 -w0 < "$CHECK_LOCAL")"
CONF_B64="$(base64 -w0 < "$CONFIG")"

# 공유 경로에 스크립트가 있으면 그걸 쓰고, 없으면 /root 로 복원해서 실행합니다.
REMOTE_CMD="umask 077;
if [ -f '$SHARED_CHECK' ]; then
    LDAP_CONFIG=\$(dirname '$SHARED_CHECK')/ldap_config.conf bash '$SHARED_CHECK';
else
    echo '$CHECK_B64' | base64 -d > $FALLBACK_CHECK &&
    echo '$CONF_B64' | base64 -d > /root/ldap_config.conf &&
    chmod 600 /root/ldap_config.conf &&
    LDAP_CONFIG=/root/ldap_config.conf bash $FALLBACK_CHECK;
    rc=\$?;
    rm -f $FALLBACK_CHECK /root/ldap_config.conf;
    exit \$rc;
fi"

RESULT_FILE="$(mktemp)"
gossh -script -w "$TARGETS" "$REMOTE_CMD" > "$RESULT_FILE" 2>&1

echo
echo "----- 검증 결과 -----"
cat "$RESULT_FILE"

echo
echo "----- 요약 -----"
OK_CNT=$(grep -c "	OK	" "$RESULT_FILE" 2>/dev/null || echo 0)
NG_CNT=$(grep -c "	FAIL	" "$RESULT_FILE" 2>/dev/null || echo 0)
NG_HOST=$(grep "	FAIL	" "$RESULT_FILE" 2>/dev/null | cut -d: -f1 | sort -u)

echo "OK 항목   : $OK_CNT"
echo "FAIL 항목 : $NG_CNT"
if [ -n "$NG_HOST" ]; then
    echo "FAIL 이 발생한 호스트:"
    printf '%s\n' "$NG_HOST" | sed 's/^/  /'
fi

rm -f "$RESULT_FILE"
[ "${TARGETS_TMP:-0}" = "1" ] && rm -f "$TARGETS"

echo
echo "=============================================================="
echo " ★ 설정을 변경했다면, 대상 서버 중 무작위로 몇 대에 직접 접속해"
echo "   설정이 실제로 반영됐는지 반드시 눈으로 확인하십시오."
echo "   본 도구의 결과는 참고용(보조 도구)입니다."
echo "=============================================================="

[ "$NG_CNT" -gt 0 ] && exit 1
exit 0
