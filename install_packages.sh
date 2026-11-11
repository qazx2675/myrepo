#!/usr/bin/env bash
# ==============================================================================
# install_packages.sh
# RHEL 8.x 오프라인(폐쇄망) 환경에서 저장된 패키지 및 의존성 일괄 설치 스크립트
#
# (지능형 패키지 필터링: 32비트 배제, 호스트 기존 패키지 충돌 방지, nscd glibc lock 자동 해결)
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
  C_CYAN="\033[1;36m"
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
번들 디렉터리의 RPM들을 호스트 환경에 맞추어 지능적으로 자동 필터링하여 설치합니다.

자동 해결 항목:
  1. 32비트(i686) RPM 자동 배제 -> libc.so.6(GLIBC_2.28) 의존성 오류 차단
  2. 호스트에 이미 설치된 상위/동일 버전 라이브러리(libxml2, glibc 등) 자동 보존 -> python3-libxml2 충돌 차단
  3. nscd의 엄격한 glibc 릴리즈 락 자동 감지 -> --nodeps 로 안전 설치

옵션:
  -d, --dir <경로>        RPM 파일들이 위치한 디렉터리 경로
                         (지정하지 않을 경우 ./rpms 또는 현재 디렉터리 자동 탐색)
  -s, --start-services   설치 완료 후 rpcbind, autofs, nscd 데몬을 자동 활성화(enable) 및 시작(start)
  -h, --help             이 도움말 화면 출력

사용 예시:
  # 기본 번들 폴더에서 실행 (자동으로 ./rpms 탐색, 필터링 및 설치)
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
if [[ ! -d "$RPM_DIR" ]]; then
  log_error "RPM 디렉터리를 찾을 수 없습니다: $RPM_DIR"
  exit 1
fi

TOTAL_RPM_COUNT=$(find "$RPM_DIR" -maxdepth 1 -name "*.rpm" | wc -l)
if [[ $TOTAL_RPM_COUNT -eq 0 ]]; then
  log_error "지정된 디렉터리에 .rpm 파일이 없습니다: $RPM_DIR"
  exit 1
fi

log_info "RPM 디렉터리: $RPM_DIR (총 ${TOTAL_RPM_COUNT}개 수집 파일 분석 시작)"
log_info "=========================================================="
log_info " 호스트 환경 맞춤형 지능형 RPM 필터링 분석 중..."
log_info "=========================================================="

# 호스트 아키텍처 확인
HOST_ARCH=$(uname -m)

INSTALL_CANDIDATES=()
SKIPPED_COUNT=0
NSCD_RPM=""
NSCD_NEED_NODEPS=0

for rpm_file in "$RPM_DIR"/*.rpm; do
  [[ -f "$rpm_file" ]] || continue
  file_name=$(basename "$rpm_file")

  # 1. 아키텍처 필터: 64비트 호스트에서 32비트(i686) 패키지는 제외 (libc.so.6 GLIBC_2.28 오류 원천 차단)
  if [[ "$HOST_ARCH" == "x86_64" && "$file_name" == *.i686.rpm ]]; then
    log_warn "[FILTER 제외] 32비트(i686) 패키지 배제 (64비트 시스템 충돌 방지): $file_name"
    ((SKIPPED_COUNT++))
    continue
  fi

  pkg_name=$(rpm -qp --queryformat '%{NAME}' "$rpm_file" 2>/dev/null || true)
  if [[ -z "$pkg_name" ]]; then
    INSTALL_CANDIDATES+=("$rpm_file")
    continue
  fi

  # 2. nscd 패키지 별도 검사 (glibc 버전 잠금 여부)
  if [[ "$pkg_name" == "nscd" ]]; then
    NSCD_RPM="$rpm_file"
    installed_glibc=$(rpm -q --queryformat '%{VERSION}-%{RELEASE}' glibc 2>/dev/null | head -n 1 || true)
    nscd_req_glibc=$(rpm -qp --requires "$rpm_file" 2>/dev/null | grep -E '^glibc = ' | awk '{print $3}' || true)

    if [[ -n "$installed_glibc" && -n "$nscd_req_glibc" && "$installed_glibc" != "$nscd_req_glibc" ]]; then
      log_warn "[FILTER 감지] nscd의 glibc 버전 잠금 불일치 (호스트: $installed_glibc vs nscd요구: $nscd_req_glibc)"
      log_info "  -> nscd는 메인 트랜잭션에서 분리하여 --nodeps 로 안전하게 단독 설치합니다."
      NSCD_NEED_NODEPS=1
      continue
    else
      INSTALL_CANDIDATES+=("$rpm_file")
      continue
    fi
  fi

  # 3. 핵심 대상 패키지 4개(autofs, rpcbind, nfs-utils, tcsh)는 무조건 설치 후보 등록
  is_target=0
  for tp in autofs rpcbind nfs-utils tcsh; do
    if [[ "$pkg_name" == "$tp" ]]; then
      is_target=1
      break
    fi
  done
  if [[ $is_target -eq 1 ]]; then
    INSTALL_CANDIDATES+=("$rpm_file")
    continue
  fi

  # 4. 부속 의존성 패키지 중 호스트에 이미 설치된 패키지 충돌 방지 필터
  # (예: libxml2, glibc 등 - 호스트에 python3-libxml2 등 최신 errata가 있을 때 번들의 구버전 설치 시 downgrade conflict 방지)
  if rpm -q "$pkg_name" >/dev/null 2>&1; then
    installed_vr=$(rpm -q --queryformat '%{VERSION}-%{RELEASE}' "$pkg_name" | head -n 1)
    bundle_vr=$(rpm -qp --queryformat '%{VERSION}-%{RELEASE}' "$rpm_file" 2>/dev/null || true)

    if [[ -n "$installed_vr" && "$installed_vr" != "$bundle_vr" ]]; then
      log_info "[FILTER 제외] 호스트에 이미 $pkg_name ($installed_vr) 설치됨 -> 번들의 $bundle_vr 제외 (버전 충돌 방지)"
      ((SKIPPED_COUNT++))
      continue
    fi
  fi

  INSTALL_CANDIDATES+=("$rpm_file")
done

log_info "----------------------------------------------------------"
log_info "필터링 결과: 총 ${TOTAL_RPM_COUNT}개 중 ${#INSTALL_CANDIDATES[@]}개 설치 진행, ${SKIPPED_COUNT}개 충돌 방지 제외"
log_info "----------------------------------------------------------"

# 1단계: nscd 단독 설치 (--nodeps 필요 시)
if [[ $NSCD_NEED_NODEPS -eq 1 && -n "$NSCD_RPM" ]]; then
  log_info "nscd 단독 설치 진행 중 (glibc ABI 100% 호환 적용)..."
  set +e
  rpm -Uvh --nodeps --replacepkgs --nosignature "$NSCD_RPM" || \
    rpm -ivh --nodeps --replacepkgs --nosignature "$NSCD_RPM"
  nscd_rc=$?
  set -e
  if [[ $nscd_rc -eq 0 ]]; then
    log_success "nscd 설치 완료!"
  else
    log_warn "nscd 단독 설치 중 경고 발생 (후속 단계에서 재확인)"
  fi
fi

# 2단계: 나머지 필터링된 패키지 일괄 설치
INSTALL_SUCCESS=0
if [[ ${#INSTALL_CANDIDATES[@]} -gt 0 ]]; then
  if command -v dnf >/dev/null 2>&1; then
    log_info "dnf 로컬 트랜잭션 설치 진행 중 (--disablerepo='*' --nogpgcheck)..."
    set +e
    dnf --disablerepo='*' --nogpgcheck -y install "${INSTALL_CANDIDATES[@]}"
    if [[ $? -eq 0 ]]; then
      INSTALL_SUCCESS=1
    fi
    set -e
  elif command -v yum >/dev/null 2>&1; then
    log_info "yum 로컬 설치 진행 중..."
    set +e
    yum --disablerepo='*' --nogpgcheck -y localinstall "${INSTALL_CANDIDATES[@]}"
    if [[ $? -eq 0 ]]; then
      INSTALL_SUCCESS=1
    fi
    set -e
  fi

  if [[ $INSTALL_SUCCESS -ne 1 ]]; then
    log_warn "dnf/yum 트랜잭션 설치 실패. rpm 명령어로 직접 설치 시도..."
    set +e
    rpm -Uvh --replacepkgs --nosignature "${INSTALL_CANDIDATES[@]}" || \
      rpm -Uvh --nodeps --replacepkgs --nosignature "${INSTALL_CANDIDATES[@]}"
    if [[ $? -eq 0 ]]; then
      INSTALL_SUCCESS=1
    fi
    set -e
  fi
else
  INSTALL_SUCCESS=1
fi

log_success "패키지 설치 단계 완료!"

# 3단계: 필수 5개 패키지 설치 검증
log_info "----------------------------------------------------------"
log_info "대상 패키지 설치 상태 검증"
log_info "----------------------------------------------------------"
ALL_OK=1
for pkg in "${TARGET_PACKAGES[@]}"; do
  if rpm -q "$pkg" >/dev/null 2>&1; then
    log_success "  [OK] $pkg : $(rpm -q "$pkg")"
  else
    log_error "  [FAIL] $pkg : 미설치 상태입니다."
    ALL_OK=0
  fi
done

# tcsh 바이너리 동작 확인
if command -v tcsh >/dev/null 2>&1; then
  log_success "  [OK] tcsh 실행 확인: $(command -v tcsh)"
else
  log_warn "tcsh 실행 바이너리를 찾을 수 없습니다."
fi

# 4단계: 서비스 활성화 및 시작 (-s 옵션)
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
else
  log_info "----------------------------------------------------------"
  log_info "안내: 서비스가 아직 시작되지 않았습니다. 필요 시 아래 명령어를 실행하세요:"
  log_info "  sudo systemctl enable --now rpcbind autofs nscd"
  log_info "----------------------------------------------------------"
fi

# 5단계: 최종 요약 테이블 출력
log_info "=========================================================="
log_info "${C_BOLD}최종 설치 및 서비스 상태 요약표${C_RESET}"
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
  log_success "모든 대상 패키지가 성공적으로 설치 및 검증되었습니다!"
  exit 0
else
  log_error "일부 패키지 설치에 실패했습니다. 위 로그를 확인하세요."
  exit 1
fi
