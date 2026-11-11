#!/usr/bin/env bash
# ==============================================================================
# install_packages.sh
# RHEL 8.x 오프라인(폐쇄망) 환경에서 저장된 패키지 및 의존성 일괄 설치 스크립트
#
# (폐쇄망 환경용: 호스트 기존 패키지 버전 충돌 방지, nscd glibc lock 자동 해결)
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
사전 저장된 패키지(RPM)들을 버전 충돌 없이 일괄 설치하고 서비스를 검증합니다.

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

# 1. 32비트(i686) RPM 파일 자동 배제
if compgen -G "$RPM_DIR/*.i686.rpm" >/dev/null; then
  log_warn "32비트(i686) RPM 파일 감지: 64비트 시스템 충돌 방지를 위해 배제합니다."
  rm -f "$RPM_DIR"/*.i686.rpm
fi

# 2. 호스트에 이미 설치된 핵심 라이브러리(libxml2, glibc 등) 구버전 덮어쓰기/충돌 방지
# (호스트에 python3-libxml2 등 최신 errata 버전이 설치되어 있을 경우 구버전 libxml2 설치 시 충돌 발생)
if rpm -q libxml2 >/dev/null 2>&1; then
  installed_libxml2=$(rpm -q --queryformat '%{VERSION}-%{RELEASE}' libxml2)
  for f in "$RPM_DIR"/libxml2-[0-9]*.rpm; do
    if [[ -f "$f" ]]; then
      pkg_libxml2=$(rpm -qp --queryformat '%{VERSION}-%{RELEASE}' "$f" 2>/dev/null || true)
      if [[ -n "$pkg_libxml2" && "$pkg_libxml2" != "$installed_libxml2" ]]; then
        log_info "호스트에 이미 libxml2 ($installed_libxml2)가 설치되어 있어 패키지 목록에서 제외합니다 ($pkg_libxml2 충돌 방지)."
        rm -f "$f"
      fi
    fi
  done
fi

# glibc 역시 호스트에 이미 설치되어 있으면 ISO 구버전 glibc는 제외
if rpm -q glibc >/dev/null 2>&1; then
  installed_glibc=$(rpm -q --queryformat '%{VERSION}-%{RELEASE}' glibc | head -n 1)
  for gpkg in glibc glibc-common glibc-all-langpacks; do
    for f in "$RPM_DIR"/${gpkg}-[0-9]*.rpm; do
      if [[ -f "$f" ]]; then
        pkg_ver=$(rpm -qp --queryformat '%{VERSION}-%{RELEASE}' "$f" 2>/dev/null || true)
        if [[ -n "$pkg_ver" && "$pkg_ver" != "$installed_glibc" ]]; then
          log_info "호스트에 이미 $gpkg ($installed_glibc)가 설치되어 있어 구버전($pkg_ver) 설치를 건너뜁니다."
          rm -f "$f"
        fi
      fi
    done
  done
fi

RPM_COUNT=$(find "$RPM_DIR" -maxdepth 1 -name "*.rpm" | wc -l)
if [[ $RPM_COUNT -eq 0 ]]; then
  log_error "지정된 디렉터리에 설치할 .rpm 파일이 없습니다: $RPM_DIR"
  exit 1
fi

log_info "RPM 디렉터리: $RPM_DIR (총 ${RPM_COUNT}개 RPM 대상)"
log_info "----------------------------------------------------------"
log_info "폐쇄망 오프라인 패키지 설치 시작"
log_info "----------------------------------------------------------"

# 3. nscd 특수 처리 (nscd는 glibc = %{version}-%{release} 엄격 잠금이 걸려있음)
NSCD_RPM=$(find "$RPM_DIR" -maxdepth 1 -name "nscd-[0-9]*.rpm" 2>/dev/null | head -n 1 || true)
if [[ -n "$NSCD_RPM" ]]; then
  installed_glibc=$(rpm -q --queryformat '%{VERSION}-%{RELEASE}' glibc | head -n 1)
  nscd_req_glibc=$(rpm -qp --requires "$NSCD_RPM" 2>/dev/null | grep -E '^glibc = ' | awk '{print $3}' || true)

  log_info "nscd 패키지 검사: $NSCD_RPM"
  log_info "  - 호스트 glibc 버전 : $installed_glibc"
  log_info "  - nscd 요구 glibc  : $nscd_req_glibc"

  if [[ -n "$installed_glibc" && -n "$nscd_req_glibc" && "$installed_glibc" != "$nscd_req_glibc" ]]; then
    log_warn "glibc 버전 차이 감지! nscd를 --nodeps 옵션으로 안전하게 단독 설치합니다 (ABI 100% 호환)..."
    rpm -Uvh --nodeps --replacepkgs --nosignature "$NSCD_RPM" || rpm -ivh --nodeps --replacepkgs --nosignature "$NSCD_RPM" || true
  fi
fi

# 4. 나머지 패키지 일괄 설치
REMAINING_RPMS=()
while IFS= read -r f; do
  [[ -f "$f" ]] && REMAINING_RPMS+=("$f")
done < <(find "$RPM_DIR" -maxdepth 1 -name "*.rpm")

INSTALL_SUCCESS=0
if command -v dnf >/dev/null 2>&1; then
  log_info "dnf 로컬 설치 진행 중..."
  set +e
  dnf --disablerepo='*' --nogpgcheck -y install "${REMAINING_RPMS[@]}"
  if [[ $? -eq 0 ]]; then
    INSTALL_SUCCESS=1
  fi
  set -e
elif command -v yum >/dev/null 2>&1; then
  log_info "yum 로컬 설치 진행 중..."
  set +e
  yum --disablerepo='*' --nogpgcheck -y localinstall "${REMAINING_RPMS[@]}"
  if [[ $? -eq 0 ]]; then
    INSTALL_SUCCESS=1
  fi
  set -e
fi

if [[ $INSTALL_SUCCESS -ne 1 ]]; then
  log_warn "dnf/yum 트랜잭션 설치 실패. rpm 명령어로 직접 설치를 진행합니다..."
  set +e
  rpm -Uvh --replacepkgs --nosignature "${REMAINING_RPMS[@]}" || \
    rpm -Uvh --nodeps --replacepkgs --nosignature "${REMAINING_RPMS[@]}"
  if [[ $? -eq 0 ]]; then
    INSTALL_SUCCESS=1
  fi
  set -e
fi

log_success "패키지 설치 단계 완료!"

# 5. 설치 여부 검증
log_info "----------------------------------------------------------"
log_info "대상 패키지 설치 검증"
log_info "----------------------------------------------------------"
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

# 6. 서비스 활성화 및 시작
if [[ $START_SERVICES -eq 1 ]]; then
  log_info "----------------------------------------------------------"
  log_info "데몬 서비스 활성화(enable) 및 시작(start)..."
  log_info "----------------------------------------------------------"
  for svc in "${TARGET_SERVICES[@]}"; do
    if systemctl list-unit-files "${svc}.service" >/dev/null 2>&1; then
      log_info "서비스 활성화 및 재시작: ${svc}.service"
      systemctl enable "${svc}.service" 2>/dev/null || true
      systemctl restart "${svc}.service" 2>/dev/null || systemctl start "${svc}.service" 2>/dev/null || true
    fi
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
  log_success "모든 대상 패키지가 성공적으로 설치되었습니다!"
  exit 0
else
  log_error "일부 패키지 설치에 실패했습니다. 위 로그를 확인하세요."
  exit 1
fi
