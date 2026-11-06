#!/usr/bin/env bash
###############################################################################
# run.sh — 관리 노드에서 실행하는 계정 수동 마운트 배포·롤백 래퍼 스크립트
#
# 흐름
#   1. bin/account-mount-engine 바이너리가 없으면 자동 빌드(setup.sh)
#   2. 입력 파일(info.txt, hosts.txt) 확인 (또는 select_user_context 활용 가능)
#   3. Go 엔진을 호출해 gossh 로 대상 노드에 일괄 적용 / Dry-Run / 롤백 수행
###############################################################################

set -euo pipefail
cd "$(dirname "$0")"

ENGINE="./bin/account-mount-engine"
INFO_FILE="${INFO_FILE:-./info.txt}"
HOSTS_FILE="${HOSTS_FILE:-./hosts.txt}"
RUN_USER=""

usage() {
  cat <<'EOF'
사용법:
  ./run.sh [옵션]

기본 실행 (info.txt 와 hosts.txt 사용):
  ./run.sh                        # 대상 호스트 전체에 계정 및 autofs 설정 적용
  ./run.sh -dry-run               # 실제 파일 변경 없이 바뀔 내용(diff)만 확인
  ./run.sh -host testserver0001   # 특정 호스트 1대에만 적용
  ./run.sh -local                 # gossh 없이 현재 로컬 서버에 직접 적용

되돌리기 (적용 시 생성된 <파일>.bak.<STAMP> 복원):
  ./run.sh -list-backups          # 각 노드의 백업 시점(STAMP) 목록 조회
  ./run.sh -rollback -dry-run     # 가장 최근 백업 복원 예정 내용 확인
  ./run.sh -rollback              # 가장 최근 백업으로 복원
  ./run.sh -rollback-to <STAMP>   # 지정한 시점(14자리 STAMP)으로 복원

주요 옵션 (bin/account-mount-engine 에 그대로 전달됨):
  -info <경로>         계정 정보 파일 경로 (기본: ./info.txt)
  -hosts <경로>        대상 호스트 목록 파일 경로 (기본: ./hosts.txt)
  -host <호스트>       단일 대상 호스트 지정 (-hosts 대신 사용)
  -local               로컬 서버 직접 적용 모드
  -dry-run             변경 없이 diff 만 출력
  -print-script        노드에서 실행될 bash 스크립트 전문 출력 후 종료
  -root <경로>         테스트용 루트 디렉터리 지정 (비우면 실제 /etc)
  -gossh <경로>        gossh 실행 파일 경로 (기본: gossh)
  -u <계정>            SSH 접속 계정 (기본: root)
  -p <비밀번호>        SSH 접속 비밀번호
  -i <키경로>          SSH 개인키 경로
  -P <포트>            SSH 포트 (기본: 22)
  -c <동시접속수>      gossh 동시 접속 수
  -t <초>              gossh 타임아웃(초)
  -h, --help           이 도움말 출력
EOF
}

###############################################################################
# 작업 계정 선택 (선택 사항 — 필요 시 내부를 채워 {RUN_USER}.txt 를 대상으로 사용)
###############################################################################
select_user_context() {
  :
}

for a in "$@"; do
  if [ "$a" = "-h" ] || [ "$a" = "--help" ]; then
    usage
    exit 0
  fi
done

if [ ! -x "$ENGINE" ]; then
  echo ">> 바이너리($ENGINE)가 없어 빌드를 먼저 수행합니다..."
  ./setup.sh
fi

select_user_context
if [ -n "$RUN_USER" ] && [ -f "${RUN_USER}.txt" ]; then
  HOSTS_FILE="${RUN_USER}.txt"
fi

# 사용자가 명시적으로 -info 또는 -hosts 를 넘겼는지 확인
has_info_arg=0
has_hosts_arg=0
for a in "$@"; do
  case "$a" in
    -info|--info) has_info_arg=1 ;;
    -hosts|--hosts|-host|--host|-local|--local|-print-script|--print-script) has_hosts_arg=1 ;;
  esac
done

EXTRA_ARGS=()
if [ "$has_info_arg" = "0" ]; then
  EXTRA_ARGS+=("-info" "$INFO_FILE")
fi
if [ "$has_hosts_arg" = "0" ]; then
  EXTRA_ARGS+=("-hosts" "$HOSTS_FILE")
fi

exec "$ENGINE" "${EXTRA_ARGS[@]}" "$@"
