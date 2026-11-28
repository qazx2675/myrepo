#!/bin/bash
###############################################################################
# run_ip_change.sh — 관리 노드에서 실행하는 IP 변경 래퍼
#
# 흐름
#   1. 작업 계정 선택 (select_user_context) → RUN_USER 결정
#   2. conf/${RUN_USER}.txt ("hostname 변경될ip") 를 대상 목록(위치 인자)으로 Go 엔진 실행
#   3. 엔진이 gossh 로 대상 노드의 설정 파일만 바꾸고(재시작·재부팅 없음) 결과를 출력
#
# 넘겨주는 옵션: -check(바꿀 내용만 보기), -fix-gateway, -bond-mode, -p(패스워드 묻기)
#
# select_user_context() 는 의도적으로 비워둔 자리입니다(작성자가 채울 부분).
###############################################################################

set -u
cd "$(dirname "$0")" || exit 2

ENGINE="../bin/ip-change-engine"
CONFIG="${CONFIG:-../conf/ip_change.conf}"

RUN_USER=""
PASS_OPTS=()      # 엔진에 그대로 넘길 옵션 (-check, -fix-gateway, -bond-mode, -p)

usage()
{
    cat <<EOF
사용법: $0 [옵션]

  -config <경로>  설정 파일 (기본 $CONFIG, 파일이 없으면 생략하고 엔진 기본값 사용)
  -check          바꿀 내용만 보여주고 노드에는 아무것도 쓰지 않음
  -fix-gateway    예전 게이트웨이가 남은 파일(/etc/sysconfig/network, route-<장치>)도 묻지 않고 고침
  -bond-mode <m>  새로 만드는 bond 모드 (active-backup 또는 lacp, 기본 active-backup)
  -p              키 인증이 실패한 노드에 쓸 SSH 패스워드를 물어봄 (화면에 보이지 않게 입력)
  -h              이 도움말

환경변수 CONFIG 로도 설정 파일을 지정할 수 있습니다.
gossh 접속 정보(계정/비밀번호/키/포트 등)는 GOSSH_ARGS 환경변수로 넘기십시오.
  예) GOSSH_ARGS="-i ~/.ssh/id_rsa" ./run_ip_change.sh
EOF
}

while [ $# -gt 0 ]; do
    case "$1" in
        -config) CONFIG="${2:-}"; shift 2 ;;
        -check|-fix-gateway|-p) PASS_OPTS+=("$1"); shift ;;
        -bond-mode)
            if [ $# -lt 2 ]; then echo "오류: -bond-mode 에 값(active-backup 또는 lacp)이 필요합니다." >&2; exit 2; fi
            PASS_OPTS+=("$1" "$2"); shift 2 ;;
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

# 설정 파일은 선택 사항입니다. 있을 때만 넘깁니다.
# 인자 배열은 항상 원소가 하나 이상이라 bash 4.1(RHEL 6)에서도 set -u 에 걸리지 않습니다.
ENGINE_ARGS=("$TARGETS")
if [ -n "$CONFIG" ] && [ -f "$CONFIG" ]; then
    ENGINE_ARGS+=(-config "$CONFIG")
fi
ENGINE_ARGS+=("${PASS_OPTS[@]+"${PASS_OPTS[@]}"}")

# shellcheck disable=SC2086
exec "$ENGINE" "${ENGINE_ARGS[@]}" ${GOSSH_ARGS:-}
