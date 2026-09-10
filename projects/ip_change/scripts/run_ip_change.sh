#!/bin/bash
###############################################################################
# run_ip_change.sh — 관리 노드에서 실행하는 IP 변경 래퍼
#
# 흐름
#   1. 작업 계정 선택 (select_user_context) → RUN_USER 결정
#   2. conf/${RUN_USER}.txt ("hostname 변경될ip") 를 대상 목록으로 Go 엔진 실행
#   3. 엔진이 gossh 로 대상 노드에 IP/게이트웨이 변경을 적용하고 결과를 출력
#
# select_user_context() 는 의도적으로 비워둔 자리입니다(작성자가 채울 부분).
###############################################################################

set -u
cd "$(dirname "$0")" || exit 2

ENGINE="../bin/ip-change-engine"
CONFIG="${CONFIG:-../conf/ip_change.conf}"

RUN_USER=""

usage()
{
    cat <<EOF
사용법: $0 [옵션]

  -config <경로>  설정 파일 (기본 $CONFIG)
  -h              이 도움말

환경변수 CONFIG 로도 설정 파일을 지정할 수 있습니다.
gossh 접속 정보(계정/비밀번호/키/포트 등)는 GOSSH_ARGS 환경변수로 넘기십시오.
  예) GOSSH_ARGS="-i ~/.ssh/id_rsa" ./run_ip_change.sh
EOF
}

while [ $# -gt 0 ]; do
    case "$1" in
        -config) CONFIG="${2:-}"; shift 2 ;;
        -h|--help) usage; exit 0 ;;
        *) echo "알 수 없는 옵션: $1" >&2; usage; exit 2 ;;
    esac
done

###############################################################################
# 작업 계정 선택 — 여기를 채우십시오.
#
# 선택 결과로 RUN_USER 를 정하면, conf/${RUN_USER}.txt 를 대상 목록으로 씁니다.
# (참고: conf/testuser.txt.sample 을 conf/<RUN_USER>.txt 로 복사해 채워두십시오)
###############################################################################

select_user_context()
{
    :
}

select_user_context

if [ -z "$RUN_USER" ]; then
    echo "오류: select_user_context() 에서 RUN_USER 를 설정해야 합니다." >&2
    exit 2
fi

TARGETS="../conf/${RUN_USER}.txt"
if [ ! -f "$TARGETS" ]; then
    echo "오류: 대상 목록 파일이 없습니다: $TARGETS" >&2
    exit 2
fi

if [ ! -x "$ENGINE" ]; then
    echo "오류: $ENGINE 이 없습니다. ./setup.sh 를 먼저 실행하십시오." >&2
    exit 2
fi

echo ">> 작업 계정: [$RUN_USER]  대상 목록: $TARGETS"

# shellcheck disable=SC2086
exec "$ENGINE" -config "$CONFIG" -targets "$TARGETS" ${GOSSH_ARGS:-}
