#!/usr/bin/env bash
# ==============================================================================
# install_packages.sh
# RHEL 8.x 오프라인(폐쇄망) 환경에서 저장된 패키지 및 의존성 일괄 설치 스크립트
#
# (폐쇄망 환경용: GPG 검증 제외, 체크섬 불필요, 로컬 RPM 일괄 무결격 설치)
# 대상 패키지: nscd, autofs, rpcbind, nfs-utils, tcsh
# ==============================================================================
set -euo pipefail

# 기본 설정값
RPM_DIR=""
START_SERVICES=0
TARGET_PACKAGES=("nscd" "autofs" "rpcbind" "nfs-utils" "tcsh")
TARGET_SERVICES=("rpcbind" "autofs" "nscd")

# 색상 출력 설정
if [[ -t 1 ]]; then
  C_RESET="\033[0m"
  C_RED="\033[1;31m"
  C_GREEN="\033[1;32m"
  C_YELLOW="\033[1;33m"
  C_BLUE="\033[1;34m"
  C_CYAN="\033[1;36m"
  C_BOLD="\033[1m"
else
  C_RESET=""
  C_RED=""
  C_GREEN=""
  C_YELLOW=""
  C_BLUE=""
  C_CYAN=""
  C_BOLD=""
fi

log_info()    { printf "${C_BLUE}[INFO]${C_RESET} %s\n" "$*"; }
log_success() { printf "${C_GREEN}[SUCCESS]${C_RESET} %s\n" "$*"; }
log_warn()    { printf "${C_YELLOW}[WARN]${C_RESET} %s\n" "$*" >&2; }
log_error()   { printf "${C_RED}[ERROR]${C_RESET} %s\n" "$*" >&2; }

usage() {
  cat <<EOF
사용법: $0 [옵션]

외부 인터넷이나 저장소 연결 및 GPG 키가 없는 폐쇄망 RHEL 8 시스템에서
사전 저장된 패키지(RPM)들을 일괄 설치하고 서비스를 검증합니다.

대상 패키지:
  - nscd, autofs, rpcbind, nfs-utils, tcsh

옵션:
  -d, --dir <경로>        RPM 파일들이 위치한 디렉터리 경로
                         (지정하지 않을 경우 ./rpms 또는 현재 디렉터리 자동 탐색)
  -s, --start-services   설치 완료 후 rpcbind, autofs, nscd 데몬을 자동 활성화(enable) 및 시작(start)
  -h, --help             이 도움말 화면 출력

사용 예시:
  # 기본 번들 폴더에서 실행 (자동으로 ./rpms 탐색 및 설치)
  sudo $0

  # 서비스 자동 활성화 및 시작 포함 실행
  sudo $0 -s

  # RPM 디렉터리 직접 지정 실행
  sudo $0 -d /path/to/rpms -s
EOF
}

# 인자 파싱
while [[ $# -gt 0 ]]; do
  case "$1" in
    -d|--dir)
      [[ $# -ge 2 ]] || { log_error "-d 옵션에는 디렉터리 경로가 필요합니다."; exit 1; }
      RPM_DIR="$2"
      shift 2
      ;;
    -s|--start-services)
      START_SERVICES=1
      shift
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      log_error "알 수 없는 옵션: $1"
      usage
      exit 1
      ;;
  esac
done

# root 권한 확인
if [[ ${EUID:-$(id -u)} -ne 0 ]]; then
  log_error "이 스크립트는 root 권한(sudo)으로 실행해야 합니다."
  exit 1
fi

log_info "=========================================================="
log_info " RHEL 8.x 폐쇄망 오프라인 패키지 자동 설치 도구"
log_info " 대상 패키지: ${TARGET_PACKAGES[*]}"
log_info " 서비스 자동 시작 여부: $([[ $START_SERVICES -eq 1 ]] && echo '활성화 (YES)' || echo '미활성화 (NO, -s 옵션으로 활성화 가능)')"
log_info "=========================================================="

# OS 확인
if [[ -f /etc/redhat-release ]]; then
  OS_INFO=$(cat /etc/redhat-release)
  log_info "현재 OS: $OS_INFO"
fi

# RPM 디렉터리 자동 탐색
SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
if [[ -z "$RPM_DIR" ]]; then
  if [[ -d "$SCRIPT_DIR/rpms" && $(find "$SCRIPT_DIR/rpms" -maxdepth 1 -name "*.rpm" | head -n 1) ]]; then
    RPM_DIR="$SCRIPT_DIR/rpms"
  elif [[ $(find "$SCRIPT_DIR" -maxdepth 1 -name "*.rpm" | head -n 1) ]]; then
    RPM_DIR="$SCRIPT_DIR"
  elif [[ -d "./rpms" ]]; then
    RPM_DIR="./rpms"
  else
    RPM_DIR="."
  fi
fi

RPM_DIR=$(realpath "$RPM_DIR")
if [[ ! -d "$RPM_DIR" ]]; then
  log_error "RPM 디렉터리를 찾을 수 없습니다: $RPM_DIR"
  exit 1
fi

RPM_COUNT=$(find "$RPM_DIR" -maxdepth 1 -name "*.rpm" | wc -l)
if [[ $RPM_COUNT -eq 0 ]]; then
  log_error "지정된 디렉터리에 .rpm 파일이 없습니다: $RPM_DIR"
  exit 1
fi

log_info "RPM 디렉터리: $RPM_DIR (총 ${RPM_COUNT}개 RPM 탐색됨)"

# 패키지 설치 진행 (GPG 검증 비활성화)
log_info "----------------------------------------------------------"
log_info "폐쇄망 오프라인 패키지 설치 시작 (--nogpgcheck)"
log_info "----------------------------------------------------------"

INSTALL_SUCCESS=0
if command -v dnf >/dev/null 2>&1; then
  log_info "실행 명령어: dnf --disablerepo='*' --nogpgcheck -y install $RPM_DIR/*.rpm"
  if dnf --disablerepo='*' --nogpgcheck -y install "$RPM_DIR"/*.rpm; then
    INSTALL_SUCCESS=1
  fi
elif command -v yum >/dev/null 2>&1; then
  log_info "실행 명령어: yum --disablerepo='*' --nogpgcheck -y localinstall $RPM_DIR/*.rpm"
  if yum --disablerepo='*' --nogpgcheck -y localinstall "$RPM_DIR"/*.rpm; then
    INSTALL_SUCCESS=1
  fi
fi

if [[ $INSTALL_SUCCESS -ne 1 ]]; then
  log_warn "dnf/yum 트랜잭션 설치 실패. rpm 명령어로 서명 검증 없이 직접 설치/업그레이드를 시도합니다..."
  log_info "실행 명령어: rpm -Uvh --replacepkgs --nosignature $RPM_DIR/*.rpm"
  if rpm -Uvh --replacepkgs --nosignature "$RPM_DIR"/*.rpm; then
    INSTALL_SUCCESS=1
  else
    log_error "rpm 직접 설치 또한 실패하였습니다. 출력된 의존성 오류를 확인하세요."
    exit 1
  fi
fi

log_success "패키지 설치 트랜잭션 완료!"

# 설치 결과 및 버전 검증
log_info "----------------------------------------------------------"
log_info "설치 상태 및 버전 검증"
log_info "----------------------------------------------------------"

ALL_OK=1
for pkg in "${TARGET_PACKAGES[@]}"; do
  if rpm -q "$pkg" >/dev/null 2>&1; then
    pkg_ver=$(rpm -q "$pkg")
    log_success "  [OK] $pkg : $pkg_ver"
  else
    log_error "  [FAIL] $pkg : 설치되지 않았습니다."
    ALL_OK=0
  fi
done

# tcsh 쉘 동작 검증
if command -v tcsh >/dev/null 2>&1; then
  tcsh_path=$(command -v tcsh)
  tcsh_v=$(tcsh --version 2>&1 | head -n 1 || true)
  log_success "  [OK] tcsh 실행 확인: $tcsh_path ($tcsh_v)"
else
  log_error "  [FAIL] tcsh 바이너리를 찾을 수 없습니다."
  ALL_OK=0
fi

# 서비스 활성화 및 시작 처리
if [[ $START_SERVICES -eq 1 ]]; then
  log_info "----------------------------------------------------------"
  log_info "데몬 서비스 활성화(enable) 및 시작(start) 처리"
  log_info "----------------------------------------------------------"
  for svc in "${TARGET_SERVICES[@]}"; do
    if systemctl list-unit-files "${svc}.service" >/dev/null 2>&1; then
      log_info "서비스 활성화 및 재시작: ${svc}.service"
      systemctl enable "${svc}.service" 2>/dev/null || true
      systemctl restart "${svc}.service" 2>/dev/null || systemctl start "${svc}.service" 2>/dev/null || true
    else
      log_warn "${svc}.service 유닛을 찾을 수 없습니다."
    fi
  done
else
  log_info "----------------------------------------------------------"
  log_info "참고: 서비스가 자동 시작되지 않았습니다. 필요시 수동으로 시작하세요:"
  log_info "  sudo systemctl enable --now rpcbind autofs nscd"
  log_info "----------------------------------------------------------"
fi

# 최종 요약 출력 테이블
log_info "=========================================================="
log_info "${C_BOLD}최종 설치 및 서비스 상태 요약표${C_RESET}"
log_info "=========================================================="
printf "%-15s %-32s %-12s\n" "PACKAGE" "INSTALLED VERSION" "STATUS"
printf "%s\n" "--------------------------------------------------------------"
for pkg in "${TARGET_PACKAGES[@]}"; do
  if rpm -q "$pkg" >/dev/null 2>&1; then
    ver_str=$(rpm -q "$pkg" --queryformat "%{VERSION}-%{RELEASE}")
    printf "%-15s %-32s ${C_GREEN}%-12s${C_RESET}\n" "$pkg" "$ver_str" "INSTALLED"
  else
    printf "%-15s %-32s ${C_RED}%-12s${C_RESET}\n" "$pkg" "-" "FAILED"
  fi
done

printf "\n%-15s %-15s %-15s\n" "SERVICE" "ENABLED" "ACTIVE"
printf "%s\n" "--------------------------------------------------------------"
for svc in "${TARGET_SERVICES[@]}"; do
  enabled="N/A"
  active="N/A"
  if systemctl is-enabled "${svc}.service" >/dev/null 2>&1; then
    enabled="enabled"
  elif systemctl list-unit-files "${svc}.service" >/dev/null 2>&1; then
    enabled="disabled"
  fi

  if systemctl is-active "${svc}.service" >/dev/null 2>&1; then
    active="active(running)"
  elif systemctl list-unit-files "${svc}.service" >/dev/null 2>&1; then
    active="inactive"
  fi
  printf "%-15s %-15s %-15s\n" "$svc" "$enabled" "$active"
done
log_info "=========================================================="

if [[ $ALL_OK -eq 1 ]]; then
  log_success "모든 패키지가 성공적으로 설치 및 검증되었습니다!"
  exit 0
else
  log_error "일부 패키지 설치 또는 검증에 실패하였습니다. 위 로그를 확인하세요."
  exit 1
fi
