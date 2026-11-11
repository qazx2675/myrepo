#!/usr/bin/env bash
# ==============================================================================
# deploy_packages.sh
#
# 수집된 RPM 패키지 디렉터리와 설치 스크립트(install.sh)를 대상 서버로 scp 전송 후
# 원격에서 패키지 설치 및 서비스 시작을 수행하는 배포 스크립트 / 함수
#
# 포트번호: 8022
# 인증방식: sshpass (평문 패스워드 환경변수/변수 저장)
# ==============================================================================
set -euo pipefail

# ==============================================================================
# [설정 변수] - 사용자 환경에 맞게 패스워드 및 설정을 지정하세요.
# ==============================================================================
SSH_PORT="8022"
SSH_USER="root"
SSH_PASS="your_password_here"   # <-- 대상 서버 공통 비밀번호 (평문 저장)
REMOTE_TMP_DIR="/tmp/rhel810_packages"

# SSH / SCP 공통 보안 및 타임아웃 옵션
SSH_OPTS=(
  -o StrictHostKeyChecking=no
  -o UserKnownHostsFile=/dev/null
  -o ConnectTimeout=10
  -o LogLevel=ERROR
)

# 색상 출력 설정
if [[ -t 1 ]]; then
  C_RESET="\033[0m"
  C_RED="\033[1;31m"
  C_GREEN="\033[1;32m"
  C_YELLOW="\033[1;33m"
  C_BLUE="\033[1;34m"
  C_BOLD="\033[1m"
else
  C_RESET=""
  C_RED=""
  C_GREEN=""
  C_YELLOW=""
  C_BLUE=""
  C_BOLD=""
fi

log_info()    { printf "${C_BLUE}[INFO]${C_RESET} %s\n" "$*"; }
log_success() { printf "${C_GREEN}[SUCCESS]${C_RESET} %s\n" "$*"; }
log_warn()    { printf "${C_YELLOW}[WARN]${C_RESET} %s\n" "$*" >&2; }
log_error()   { printf "${C_RED}[ERROR]${C_RESET} %s\n" "$*" >&2; }

# ==============================================================================
# [핵심 함수] deploy_and_install_packages
# 기존 배포 스크립트에 이 함수 블록을 그대로 복사하여 추가할 수 있습니다.
# ==============================================================================
deploy_and_install_packages() {
  local target_host="$1"
  local script_dir
  script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

  log_info "[${target_host}] 패키지 원격 배포 및 설치 시작 (SSH 포트: ${SSH_PORT})..."

  # 1. 로컬의 RPM 디렉터리 탐색
  local local_rpm_dir=""
  if [[ -d "$script_dir/rhel810_packages/rpms" ]]; then
    local_rpm_dir="$script_dir/rhel810_packages/rpms"
  elif [[ -d "$script_dir/rpms" ]]; then
    local_rpm_dir="$script_dir/rpms"
  elif [[ -d "$script_dir/rhel810_packages" ]]; then
    local_rpm_dir="$script_dir/rhel810_packages"
  else
    log_error "[${target_host}] RPM 디렉터리를 찾을 수 없습니다. ($script_dir/rhel810_packages 또는 rpms 필요)"
    return 1
  fi

  # 2. 로컬의 설치 스크립트 탐색 (install.sh 또는 install_packages.sh)
  local local_install_script=""
  if [[ -f "$script_dir/install.sh" ]]; then
    local_install_script="$script_dir/install.sh"
  elif [[ -f "$script_dir/install_packages.sh" ]]; then
    local_install_script="$script_dir/install_packages.sh"
  elif [[ -f "$script_dir/rhel810_packages/install_packages.sh" ]]; then
    local_install_script="$script_dir/rhel810_packages/install_packages.sh"
  elif [[ -f "$script_dir/rhel810_packages/install.sh" ]]; then
    local_install_script="$script_dir/rhel810_packages/install.sh"
  else
    log_error "[${target_host}] 설치 스크립트(install.sh 또는 install_packages.sh)를 찾을 수 없습니다."
    return 1
  fi

  # 3. sshpass 도구 설치 여부 확인
  if ! command -v sshpass >/dev/null 2>&1; then
    log_error "sshpass 명령어가 설치되어 있지 않습니다. (yum install -y sshpass 필요)"
    return 1
  fi

  # 4. 원격지 임시 디렉터리 준비
  log_info "[${target_host}] 원격 디렉터리 생성: ${REMOTE_TMP_DIR}/rpms"
  if ! sshpass -p "$SSH_PASS" ssh -p "$SSH_PORT" "${SSH_OPTS[@]}" "${SSH_USER}@${target_host}" "mkdir -p ${REMOTE_TMP_DIR}/rpms"; then
    log_error "[${target_host}] SSH 접속 실패 (호스트: ${target_host}, 포트: ${SSH_PORT})"
    return 1
  fi

  # 5. RPM 패키지 파일 전송 (scp)
  log_info "[${target_host}] RPM 패키지 전송 중 (SCP -> ${REMOTE_TMP_DIR}/rpms/)..."
  if ! sshpass -p "$SSH_PASS" scp -P "$SSH_PORT" "${SSH_OPTS[@]}" -r "$local_rpm_dir"/*.rpm "${SSH_USER}@${target_host}:${REMOTE_TMP_DIR}/rpms/"; then
    log_error "[${target_host}] RPM 파일 SCP 전송 실패"
    return 1
  fi

  # 6. install.sh 설치 스크립트 전송 (scp)
  log_info "[${target_host}] 설치 스크립트 전송 중 (SCP -> ${REMOTE_TMP_DIR}/install.sh)..."
  if ! sshpass -p "$SSH_PASS" scp -P "$SSH_PORT" "${SSH_OPTS[@]}" "$local_install_script" "${SSH_USER}@${target_host}:${REMOTE_TMP_DIR}/install.sh"; then
    log_error "[${target_host}] 설치 스크립트 SCP 전송 실패"
    return 1
  fi

  # 7. 원격 서버에서 설치 스크립트 실행 (서비스 자동 시작 -s 옵션 포함)
  log_info "[${target_host}] 원격 패키지 설치 실행 중 (bash install.sh -s)..."
  local remote_cmd="chmod +x ${REMOTE_TMP_DIR}/install.sh && bash ${REMOTE_TMP_DIR}/install.sh -d ${REMOTE_TMP_DIR}/rpms -s"
  
  if ! sshpass -p "$SSH_PASS" ssh -p "$SSH_PORT" "${SSH_OPTS[@]}" "${SSH_USER}@${target_host}" "$remote_cmd"; then
    log_error "[${target_host}] 원격 설치 스크립트 실행 실패"
    return 1
  fi

  log_success "[${target_host}] 원격 패키지 배포 및 설치 완료!"
  return 0
}

# ==============================================================================
# [단독 실행 지원] (예: ./deploy_packages.sh 192.168.1.10 또는 -f hosts.txt)
# ==============================================================================
usage() {
  cat <<EOF
사용법: $0 [호스트IP...] 또는 $0 -f [호스트목록파일]

수집된 RPM 디렉터리와 install.sh를 대상 서버(포트 8022)로 scp 전송 후
원격에서 패키지 설치(nscd, autofs, rpcbind, nfs-utils, tcsh)를 일괄 실행합니다.

설정:
  포트: $SSH_PORT
  계정: $SSH_USER
  비밀번호: 스크립트 내 SSH_PASS 변수 수정 필요

옵션:
  -f <파일>      호스트 목록 파일 (줄바꿈 구분)
  -p <포트>      SSH 포트 번호 지정 (기본값: 8022)
  -h, --help     도움말 출력

예시:
  # 단일 서버에 배포 및 설치
  ./deploy_packages.sh 192.168.1.100

  # 여러 서버에 순차 배포 및 설치
  ./deploy_packages.sh 192.168.1.101 192.168.1.102

  # 파일에 적힌 호스트 목록으로 배포
  ./deploy_packages.sh -f hosts.txt
EOF
}

main() {
  local hosts=()
  local hosts_file=""

  while [[ $# -gt 0 ]]; do
    case "$1" in
      -f)
        [[ $# -ge 2 ]] || { log_error "-f 옵션 뒤에 파일 경로가 필요합니다."; exit 1; }
        hosts_file="$2"
        shift 2
        ;;
      -p)
        [[ $# -ge 2 ]] || { log_error "-p 옵션 뒤에 포트 번호가 필요합니다."; exit 1; }
        SSH_PORT="$2"
        shift 2
        ;;
      -h|--help)
        usage
        exit 0
        ;;
      *)
        hosts+=("$1")
        shift
        ;;
    esac
  done

  # 호스트 파일이 지정된 경우 로드
  if [[ -n "$hosts_file" ]]; then
    if [[ ! -f "$hosts_file" ]]; then
      log_error "호스트 파일을 찾을 수 없습니다: $hosts_file"
      exit 1
    fi
    while IFS= read -r line || [[ -n "$line" ]]; do
      line=$(echo "$line" | sed -e 's/#.*//' -e 's/^[[:space:]]*//' -e 's/[[:space:]]*$//')
      [[ -n "$line" ]] && hosts+=("$line")
    done < "$hosts_file"
  fi

  if [[ ${#hosts[@]} -eq 0 ]]; then
    usage
    exit 1
  fi

  log_info "=========================================================="
  log_info " RHEL 8.10 패키지 원격 SCP 배포 및 자동 설치"
  log_info " 대상 호스트 수: ${#hosts[@]}대"
  log_info " SSH 포트: ${SSH_PORT}"
  log_info "=========================================================="

  local success_cnt=0
  local fail_cnt=0
  local failed_hosts=()

  for h in "${hosts[@]}"; do
    if deploy_and_install_packages "$h"; then
      ((success_cnt++))
    else
      ((fail_cnt++))
      failed_hosts+=("$h")
    fi
    echo ""
  done

  log_info "=========================================================="
  log_info "배포 결과 요약: 성공 ${success_cnt}대 / 실패 ${fail_cnt}대"
  if [[ ${#failed_hosts[@]} -gt 0 ]]; then
    log_error "실패 호스트: ${failed_hosts[*]}"
    exit 1
  fi
  log_success "모든 호스트에 배포 및 설치가 정상 완료되었습니다."
}

# 이 파일이 직접 실행되었을 때만 main 실행 (다른 스크립트에서 source 할 경우 함수만 로드)
if [[ "${BASH_SOURCE[0]}" == "${0}" ]]; then
  main "$@"
fi
