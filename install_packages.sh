#!/usr/bin/env bash
# ==============================================================================
# install_packages.sh
# RHEL 8.x 오프라인(폐쇄망) 환경에서 저장된 패키지 및 의존성 일괄 설치 스크립트
#
# (폐쇄망 환경용: x86_64 전용, GPG 검증 제외, 32비트 패키지 충돌 방지)
# 대상 패키지: nscd, autofs, rpcbind, nfs-utils, tcsh
# ==============================================================================
set -euo pipefail

RPM_DIR=""
START_SERVICES=0
TARGET_PACKAGES=("nscd" "autofs" "rpcbind" "nfs-utils" "tcsh")
TARGET_SERVICES=("rpcbind" "autofs" "nscd")

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
EOF
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    -d|--dir) RPM_DIR="$2"; shift 2 ;;
    -s|--start-services) START_SERVICES=1; shift ;;
    -h|--help) usage; exit 0 ;;
    *) echo "알 수 없는 옵션: $1"; exit 1 ;;
  esac
done

if [[ ${EUID:-$(id -u)} -ne 0 ]]; then
  log_error "이 스크립트는 root 권한(sudo)으로 실행해야 합니다."
  exit 1
fi

# OS 확인
if [[ -f /etc/redhat-release ]]; then
  OS_INFO=$(cat /etc/redhat-release)
  log_info "현재 OS: $OS_INFO"
  if grep -qE "release 7(\.[0-9]+)?" /etc/redhat-release; then
    log_error "경고: 현재 OS는 RHEL 7입니다. RHEL 8.10 패키지(GLIBC 2.28 요구)는 RHEL 7에 직접 설치할 수 없습니다."
    log_error "대상 서버가 RHEL 8 계열인지 확인해 주십시오."
    exit 1
  fi
fi

# RPM 디렉터리 자동 탐색
SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
if [[ -z "$RPM_DIR" ]]; then
  if [[ -d "$SCRIPT_DIR/rpms" && $(find "$SCRIPT_DIR/rpms" -maxdepth 1 -name "*.rpm" 2>/dev/null | head -n 1) ]]; then
    RPM_DIR="$SCRIPT_DIR/rpms"
  elif [[ $(find "$SCRIPT_DIR" -maxdepth 1 -name "*.rpm" 2>/dev/null | head -n 1) ]]; then
    RPM_DIR="$SCRIPT_DIR"
  elif [[ -d "./rpms" ]]; then
    RPM_DIR="./rpms"
  else
    RPM_DIR="."
  fi
fi

RPM_DIR=$(realpath "$RPM_DIR")

# 32비트(i686) RPM 파일 자동 정리 (libc.so.6(GLIBC_2.28) 오류의 주원인 차단)
if compgen -G "$RPM_DIR/*.i686.rpm" >/dev/null; then
  log_warn "32비트(i686) RPM 파일이 감지되어 배제합니다 (64비트 GLIBC 의존성 충돌 방지)..."
  rm -f "$RPM_DIR"/*.i686.rpm
fi

RPM_COUNT=$(find "$RPM_DIR" -maxdepth 1 -name "*.rpm" | wc -l)
if [[ $RPM_COUNT -eq 0 ]]; then
  log_error "지정된 디렉터리에 .rpm 파일이 없습니다: $RPM_DIR"
  exit 1
fi

log_info "RPM 디렉터리: $RPM_DIR (총 ${RPM_COUNT}개 64비트 RPM)"

log_info "폐쇄망 오프라인 패키지 설치 시작 (--nogpgcheck)..."
INSTALL_SUCCESS=0

# dnf 또는 yum으로 로컬 트랜잭션 설치
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

# 실패 시 rpm 명령어로 직접 시도
if [[ $INSTALL_SUCCESS -ne 1 ]]; then
  log_warn "dnf/yum 트랜잭션 설치 실패. rpm 서명 무시 모드(--nosignature)로 직접 설치 시도..."
  log_info "실행 명령어: rpm -Uvh --replacepkgs --nosignature $RPM_DIR/*.rpm"
  if rpm -Uvh --replacepkgs --nosignature "$RPM_DIR"/*.rpm; then
    INSTALL_SUCCESS=1
  else
    log_error "패키지 설치에 실패했습니다. 상단 의존성 에러 메시지를 확인하세요."
    exit 1
  fi
fi

log_success "패키지 설치 완료!"

# 설치 여부 검증
ALL_OK=1
for pkg in "${TARGET_PACKAGES[@]}"; do
  if rpm -q "$pkg" >/dev/null 2>&1; then
    log_success "  [OK] $pkg : $(rpm -q "$pkg")"
  else
    log_error "  [FAIL] $pkg : 미설치"
    ALL_OK=0
  fi
done

# tcsh 동작 확인
if command -v tcsh >/dev/null 2>&1; then
  log_success "  [OK] tcsh 실행 확인: $(command -v tcsh)"
else
  log_warn "tcsh 실행 바이너리를 찾을 수 없습니다."
fi

# 서비스 활성화
if [[ $START_SERVICES -eq 1 ]]; then
  log_info "데몬 서비스 활성화(enable) 및 시작(start)..."
  for svc in "${TARGET_SERVICES[@]}"; do
    systemctl enable "${svc}.service" 2>/dev/null || true
    systemctl restart "${svc}.service" 2>/dev/null || systemctl start "${svc}.service" 2>/dev/null || true
  done
fi

log_info "=========================================================="
printf "%-15s %-32s %-12s\n" "PACKAGE" "INSTALLED VERSION" "STATUS"
printf "%s\n" "--------------------------------------------------------------"
for pkg in "${TARGET_PACKAGES[@]}"; do
  if rpm -q "$pkg" >/dev/null 2>&1; then
    printf "%-15s %-32s ${C_GREEN}%-12s${C_RESET}\n" "$pkg" "$(rpm -q "$pkg" --queryformat '%{VERSION}-%{RELEASE}')" "INSTALLED"
  else
    printf "%-15s %-32s ${C_RED}%-12s${C_RESET}\n" "$pkg" "-" "FAILED"
  fi
done
log_info "=========================================================="
