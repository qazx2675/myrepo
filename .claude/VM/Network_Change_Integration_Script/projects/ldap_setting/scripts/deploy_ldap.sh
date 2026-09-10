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

되돌리기 (적용 시 남긴 <파일>.bak.<시점> 을 복원합니다)

  -list-backups        각 노드에 남아 있는 백업 시점 목록 조회 (변경 없음)
  -rollback            가장 최근 시점으로 되돌리고 검증까지 수행
  -rollback-to <시점>  지정한 시점(숫자 14자리)으로 되돌리고 검증까지 수행

  되돌리기에는 -infra 가 필요 없습니다. 노드에 남아 있는 백업만 보고 판단합니다.
  -dry-run 을 함께 주면 무엇이 복원될지만 보여줍니다.

환경변수 CONFIG / ASSETS / SHARED_CHECK / CHECK_LOCAL 로도 지정할 수 있습니다.
EOF
}

CHECK_ONLY=0
ROLLBACK_ARG=""
while [ $# -gt 0 ]; do
    case "$1" in
        -infra)         INFRA="${2:-}"; shift 2 ;;
        -dry-run)       DRYRUN=1; shift ;;
        -config)        CONFIG="${2:-}"; shift 2 ;;
        -assets)        ASSETS="${2:-}"; shift 2 ;;
        -check-only)    CHECK_ONLY=1; shift ;;
        -list-backups)  ROLLBACK_ARG="-list-backups"; shift ;;
        -rollback)      ROLLBACK_ARG="-rollback"; shift ;;
        -rollback-to)   ROLLBACK_ARG="-rollback-to ${2:-}"; shift 2 ;;
        -h|--help)      usage; exit 0 ;;
        *)              echo "알 수 없는 옵션: $1"; usage; exit 2 ;;
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

# 되돌리기는 인프라 값을 쓰지 않으므로 -infra 를 요구하지 않습니다.
if [ -z "$ROLLBACK_ARG" ] && [ -z "$INFRA" ]; then
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
# 2-R. 되돌리기 (지정된 경우 여기서 처리)
#
#   -list-backups 는 조회만 하고 끝냅니다.
#   -rollback / -rollback-to 는 되돌린 뒤 아래 검증 단계로 넘어갑니다.
###############################################################################

if [ -n "$ROLLBACK_ARG" ]; then
    if [ ! -x "$ENGINE" ]; then
        echo "오류: 설정 엔진이 없습니다: $ENGINE  (setup.sh 로 먼저 빌드하십시오)"
        exit 2
    fi

    RB_ARGS="-assets $ASSETS $ROLLBACK_ARG"
    [ "$DRYRUN" = "1" ] && RB_ARGS="$RB_ARGS -dry-run"

    echo
    # shellcheck disable=SC2086
    "$ENGINE" $RB_ARGS
    RB_RC=$?

    case "$ROLLBACK_ARG" in
        -list-backups)
            # 조회만 하고 끝냅니다. 검증할 것이 없습니다.
            [ "${TARGETS_TMP:-0}" = "1" ] && rm -f "$TARGETS"
            exit $RB_RC
            ;;
    esac

    if [ "$RB_RC" != "0" ]; then
        echo "경고: 되돌리기가 실패를 보고했습니다 (rc=$RB_RC). 검증은 계속합니다."
    fi

    # 되돌린 뒤 상태를 확인하려면 인프라 기준값이 필요합니다.
    # -infra 없이 되돌리기만 한 경우 검증은 건너뜁니다.
    if [ -z "$INFRA" ]; then
        echo
        echo "안내: -infra 가 없어 되돌린 뒤 검증은 건너뜁니다."
        echo "      검증하려면 -infra <이름> 을 함께 주거나 -check-only 로 다시 실행하십시오."
        echo
        echo "=============================================================="
        echo " ★ 되돌리기 후에는 대상 서버 중 무작위로 몇 대에 직접 접속해"
        echo "   설정이 실제로 복원됐는지 반드시 눈으로 확인하십시오."
        echo "=============================================================="
        [ "${TARGETS_TMP:-0}" = "1" ] && rm -f "$TARGETS"
        exit $RB_RC
    fi

    CHECK_ONLY=1        # 아래 적용 단계는 건너뛰고 검증만 수행
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
#
# 이 스크립트 자체를 통째로 base64 로 감싸 "echo <b64> | base64 -d | bash" 형태로
# 보냅니다 — 대상 계정의 로그인 셸이 무엇인지 알 수 없는데, 위 내용에는 따옴표와
# "rc=$?" 같은 bash 전용 문법이 들어 있어 로그인 셸이 csh/tcsh 면 그대로 gossh 에
# 넘겼을 때 "Command not found"/"Undefined variable" 로 깨집니다 (engine 쪽
# internal/remote.BuildCommand 에서 실제로 겪은 문제와 동일. ARCHITECTURE.md 11번).
REMOTE_SCRIPT="umask 077;
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
REMOTE_SCRIPT_B64="$(printf '%s' "$REMOTE_SCRIPT" | base64 -w0)"
REMOTE_CMD="echo $REMOTE_SCRIPT_B64 | base64 -d | bash"

RESULT_FILE="$(mktemp)"
gossh -script -w "$TARGETS" "$REMOTE_CMD" > "$RESULT_FILE" 2>&1

echo
echo "----- 검증 결과 -----"
cat "$RESULT_FILE"

echo
echo "----- 요약 -----"
# ldap_check.sh 는 한 줄 요약만 찍습니다: 정상 "INFO<TAB>LDAP<TAB>..." /
# 실패 "FAIL<TAB>LDAP<TAB>UNDEFINED". exit code 가 0 이 아니면 gossh 가 그 줄
# 앞에 "ERROR: " 를 더 붙이므로, 접두사와 무관하게 INFO/FAIL 토큰만 찾습니다.
OK_CNT=$(grep -c $'INFO	LDAP' "$RESULT_FILE" 2>/dev/null || echo 0)
NG_CNT=$(grep -c $'FAIL	LDAP' "$RESULT_FILE" 2>/dev/null || echo 0)
NG_HOST=$(grep $'FAIL	LDAP' "$RESULT_FILE" 2>/dev/null | cut -d: -f1 | sort -u)

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
