#!/usr/bin/env bash
# ==============================================================================
# save_packages.sh
# RHEL 8.10 마운트 이미지(/mnt/rhel810) 및 등록된 yum 레포지토리로부터
# 필수 패키지 및 전체 의존성 RPM 저장 스크립트 (x86_64 전용, 폐쇄망 최적화)
#
# 레포지토리: RHEL810-base (BaseOS), RHEL810-app (AppStream)
# 대상 패키지: nscd, autofs, rpcbind, nfs-utils, tcsh
# ==============================================================================
set -euo pipefail

# 기본 설정값
DEFAULT_MOUNT_DIR="/mnt/rhel810"
MOUNT_DIR="$DEFAULT_MOUNT_DIR"
OUT_DIR="./rhel810_packages"
CREATE_ARCHIVE=0
TARGET_PACKAGES=("nscd" "autofs" "rpcbind" "nfs-utils" "tcsh")

# 핵심 의존성 목록 (오프라인 타깃 서버 최소설치 대비)
# ※ libxml2, glibc는 모든 RHEL 8 기본 설치에 포함되어 있으며 호스트의 최신 errata와
#    충돌(downgrade conflict)을 방지하기 위해 강제 복사 목록에서 제외합니다.
KNOWN_DEPENDENCIES=(
  "libtirpc"
  "libevent"
  "gssproxy"
  "keyutils"
  "quota"
  "quota-nfs"
  "hesiod"
  "libsss_autofs"
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

usage() {
  cat <<EOF
사용법: $0 [옵션]

RHEL 8.10 마운트 이미지(/mnt/rhel810) 및 yum/dnf 레포지토리(RHEL810-base, RHEL810-app)에서
대상 패키지와 전체 의존성을 64비트(x86_64) 전용으로 추출하여 오프라인 설치용 번들로 저장합니다.

대상 패키지:
  - nscd, autofs, rpcbind, nfs-utils, tcsh (및 glibc 등 64비트 하위 의존성 전체)

옵션:
  -m, --mount <경로>      RHEL 8.10 ISO 마운트 디렉터리 (기본값: /mnt/rhel810)
  -o, --output <디렉터리>  패키지 저장 출력 디렉터리 (기본값: ./rhel810_packages)
  -a, --archive           저장 완료 후 .tar.gz 압축 파일 생성
  -h, --help              이 도움말 화면 출력

사용 예시:
  # 기본 마운트 위치(/mnt/rhel810)에서 수집하여 ./rhel810_packages 에 저장
  sudo $0

  # 압축 번들(.tar.gz) 동시 생성
  sudo $0 -a
EOF
}

# 인자 파싱
while [[ $# -gt 0 ]]; do
  case "$1" in
    -m|--mount)  MOUNT_DIR="$2"; shift 2 ;;
    -o|--output) OUT_DIR="$2"; shift 2 ;;
    -a|--archive) CREATE_ARCHIVE=1; shift ;;
    *) echo "알 수 없는 옵션: $1"; exit 1 ;;
  esac
done

# root 권한 확인
if [[ ${EUID:-$(id -u)} -ne 0 ]]; then
  log_error "이 스크립트는 root 권한(sudo)으로 실행해야 합니다."
  exit 1
fi

log_info "=========================================================="
log_info " RHEL 8.10 오프라인 패키지 수집 도구 (x86_64 전용)"
log_info " 대상 패키지: ${TARGET_PACKAGES[*]}"
log_info " 마운트 경로: $MOUNT_DIR"
log_info " 출력 디렉터리: $OUT_DIR"
log_info "=========================================================="

PKG_MGR="dnf"
if ! command -v dnf >/dev/null 2>&1; then
  PKG_MGR="yum"
fi

REPO_ARGS=()
if [[ -d "$MOUNT_DIR/BaseOS" ]]; then
  REPO_ARGS+=("--repofrompath=RHEL810-base,file://$MOUNT_DIR/BaseOS")
fi
if [[ -d "$MOUNT_DIR/AppStream" ]]; then
  REPO_ARGS+=("--repofrompath=RHEL810-app,file://$MOUNT_DIR/AppStream")
fi

REPO_ARGS+=(
  "--disablerepo=*"
  "--enablerepo=RHEL810-base"
  "--enablerepo=RHEL810-app"
  "--nogpgcheck"
  "--setopt=*.gpgcheck=0"
  "--disableplugin=subscription-manager,product-id"
)

RPMS_OUT_DIR="$OUT_DIR/rpms"
mkdir -p "$RPMS_OUT_DIR"

log_info "레포지토리로부터 x86_64 패키지 및 의존성 다운로드 시작..."

DOWNLOADED_SOMETHING=0

# 방법 1: repotrack (반드시 -a x86_64 지정하여 32비트 i686 혼입 차단)
if command -v repotrack >/dev/null 2>&1; then
  log_info "repotrack 도구 감지: 64비트(x86_64) 의존성 전수 수집 시도..."
  set +e
  repotrack -a x86_64 -p "$RPMS_OUT_DIR" \
    -r RHEL810-base -r RHEL810-app \
    "${TARGET_PACKAGES[@]}" 2>&1 | tail -n 5
  if [[ $? -eq 0 ]]; then
    DOWNLOADED_SOMETHING=1
    log_success "repotrack 수집 완료"
  fi
  set -e
fi

# 방법 2: dnf download 또는 yumdownloader (--arch=x86_64,noarch 지정)
if [[ $DOWNLOADED_SOMETHING -ne 1 ]]; then
  if $PKG_MGR download --help >/dev/null 2>&1; then
    log_info "$PKG_MGR download --resolve --alldeps (--arch=x86_64,noarch) 시도..."
    set +e
    $PKG_MGR download -y \
      --arch=x86_64,noarch \
      --destdir="$RPMS_OUT_DIR" \
      --resolve \
      --alldeps \
      "${REPO_ARGS[@]}" \
      "${TARGET_PACKAGES[@]}"
    if [[ $? -eq 0 ]]; then
      DOWNLOADED_SOMETHING=1
      log_success "$PKG_MGR download 수집 완료"
    fi
    set -e
  elif command -v yumdownloader >/dev/null 2>&1; then
    log_info "yumdownloader --resolve --archlist=x86_64,noarch 시도..."
    set +e
    yumdownloader --destdir="$RPMS_OUT_DIR" \
      --resolve \
      --archlist=x86_64,noarch \
      --enablerepo=RHEL810-base \
      --enablerepo=RHEL810-app \
      "${TARGET_PACKAGES[@]}"
    if [[ $? -eq 0 ]]; then
      DOWNLOADED_SOMETHING=1
      log_success "yumdownloader 수집 완료"
    fi
    set -e
  fi
fi

# 방법 3: dnf install/reinstall --downloadonly
if [[ $DOWNLOADED_SOMETHING -ne 1 ]]; then
  log_info "$PKG_MGR install/reinstall --downloadonly 명령으로 수집 시도..."
  set +e
  $PKG_MGR -y "${REPO_ARGS[@]}" reinstall --downloadonly --downloaddir="$RPMS_OUT_DIR" "${TARGET_PACKAGES[@]}" >/dev/null 2>&1 || true
  $PKG_MGR -y "${REPO_ARGS[@]}" install --downloadonly --downloaddir="$RPMS_OUT_DIR" "${TARGET_PACKAGES[@]}" >/dev/null 2>&1 || true
  set -e
fi

# 방법 4: 마운트 디렉터리(/mnt/rhel810)에서 64비트 RPM 직접 탐색 및 복사 (누락 방지)
if [[ -d "$MOUNT_DIR" ]]; then
  log_info "마운트 디렉터리($MOUNT_DIR)에서 64비트 필수 패키지 및 glibc 등 의존성 확인..."
  ALL_SEARCH_PKGS=("${TARGET_PACKAGES[@]}" "${KNOWN_DEPENDENCIES[@]}")

  for pkg in "${ALL_SEARCH_PKGS[@]}"; do
    # 이미 x86_64 또는 noarch 파일이 있는지 확인
    if ! find "$RPMS_OUT_DIR" -maxdepth 1 \( -name "${pkg}-[0-9]*.x86_64.rpm" -o -name "${pkg}-[0-9]*.noarch.rpm" \) 2>/dev/null | grep -q .; then
      found_files=$(find "$MOUNT_DIR" -maxdepth 4 \( -name "${pkg}-[0-9]*.x86_64.rpm" -o -name "${pkg}-[0-9]*.noarch.rpm" \) 2>/dev/null || true)
      if [[ -n "$found_files" ]]; then
        while IFS= read -r f; do
          if [[ -f "$f" ]]; then
            cp -n "$f" "$RPMS_OUT_DIR/"
            log_info "  - 복사됨: $(basename "$f")"
          fi
        done <<< "$found_files"
      fi
    fi
  done
fi

# 32비트(i686) RPM 혼입 원천 차단 (libc.so.6(GLIBC_2.28) 의존성 에러의 원인 제거)
I686_FILES=$(find "$RPMS_OUT_DIR" -maxdepth 1 -name "*.i686.rpm" 2>/dev/null || true)
if [[ -n "$I686_FILES" ]]; then
  log_warn "32비트(i686) RPM 파일 감지: 64비트 시스템 GLIBC 충돌 방지를 위해 배제합니다."
  rm -f "$RPMS_OUT_DIR"/*.i686.rpm
fi

# 필수 5개 패키지 수집 검증
log_info "----------------------------------------------------------"
log_info "필수 패키지 수집 검증"
log_info "----------------------------------------------------------"
MISSING=()
for pkg in "${TARGET_PACKAGES[@]}"; do
  found=$(find "$RPMS_OUT_DIR" -maxdepth 1 \( -name "${pkg}-[0-9]*.x86_64.rpm" -o -name "${pkg}-[0-9]*.noarch.rpm" \) 2>/dev/null | head -n 1 || true)
  if [[ -n "$found" ]]; then
    log_success "  [OK] $pkg -> $(basename "$found")"
  else
    log_error "  [MISSING] $pkg 패키지가 수집되지 않았습니다."
    MISSING+=("$pkg")
  fi
done

if [[ ${#MISSING[@]} -gt 0 ]]; then
  log_error "다음 필수 패키지 수집에 실패하였습니다: ${MISSING[*]}"
  exit 1
fi

RPM_COUNT=$(find "$RPMS_OUT_DIR" -maxdepth 1 -name "*.rpm" | wc -l)
log_success "총 ${RPM_COUNT}개의 64비트 RPM 파일 수집 완료!"

SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
if [[ -f "$SCRIPT_DIR/install_packages.sh" ]]; then
  cp -f "$SCRIPT_DIR/install_packages.sh" "$OUT_DIR/"
  chmod +x "$OUT_DIR/install_packages.sh"
fi

if [[ $CREATE_ARCHIVE -eq 1 ]]; then
  TAR_NAME="$(basename "$OUT_DIR").tar.gz"
  PARENT_DIR=$(dirname "$OUT_DIR")
  DIR_NAME=$(basename "$OUT_DIR")
  log_info "압축 번들 생성 중 ($PARENT_DIR/$TAR_NAME)..."
  tar -czf "$PARENT_DIR/$TAR_NAME" -C "$PARENT_DIR" "$DIR_NAME"
  log_success "압축 번들 완료: $PARENT_DIR/$TAR_NAME"
fi

log_success "패키지 저장이 성공적으로 완료되었습니다! ($(realpath "$OUT_DIR"))"
