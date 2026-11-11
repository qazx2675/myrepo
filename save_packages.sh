#!/usr/bin/env bash
# ==============================================================================
# save_packages.sh
# RHEL 8.10 마운트 이미지(/mnt/rhel810) 및 등록된 yum 레포지토리로부터
# 필수 패키지 및 전체 의존성 RPM 저장 스크립트
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
KNOWN_DEPENDENCIES=(
  "libtirpc"
  "libevent"
  "gssproxy"
  "keyutils"
  "quota"
  "quota-nfs"
  "hesiod"
  "libxml2"
  "libsss_autofs"
)

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

RHEL 8.10 마운트 이미지(/mnt/rhel810) 및 yum/dnf 레포지토리(RHEL810-base, RHEL810-app)에서
대상 패키지와 전체 의존성을 추출하여 오프라인 설치용 독립 번들 디렉터리로 저장합니다.

대상 패키지:
  - nscd, autofs, rpcbind, nfs-utils, tcsh (총 5개 및 하위 의존성 전체)

사용 레포지토리:
  - RHEL810-base (BaseOS)
  - RHEL810-app  (AppStream)

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

  # 마운트 경로 및 출력 디렉터리 직접 지정
  sudo $0 -m /mnt/rhel810 -o ./my_rhel8_bundle -a
EOF
}

# 인자 파싱
while [[ $# -gt 0 ]]; do
  case "$1" in
    -m|--mount)
      [[ $# -ge 2 ]] || { log_error "-m 옵션에는 경로가 필요합니다."; exit 1; }
      MOUNT_DIR="$2"
      shift 2
      ;;
    -o|--output)
      [[ $# -ge 2 ]] || { log_error "-o 옵션에는 디렉터리 경로가 필요합니다."; exit 1; }
      OUT_DIR="$2"
      shift 2
      ;;
    -a|--archive)
      CREATE_ARCHIVE=1
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
log_info " RHEL 8.10 오프라인 패키지 수집 도구"
log_info " 대상 패키지: ${TARGET_PACKAGES[*]}"
log_info " 마운트 경로: $MOUNT_DIR"
log_info " 출력 디렉터리: $OUT_DIR"
log_info "=========================================================="

# 패키지 매니저 확인
PKG_MGR="dnf"
if ! command -v dnf >/dev/null 2>&1; then
  if command -v yum >/dev/null 2>&1; then
    PKG_MGR="yum"
  else
    log_error "시스템에 'dnf' 또는 'yum' 명령어가 없습니다."
    exit 1
  fi
fi

# 레포지토리 인자 구성 (RHEL810-base, RHEL810-app)
REPO_ARGS=()
BASEOS_PATH=""
APPSTREAM_PATH=""

# 1. 마운트 디렉터리 확인
if [[ -d "$MOUNT_DIR" ]]; then
  if [[ -d "$MOUNT_DIR/BaseOS" ]]; then
    BASEOS_PATH="$MOUNT_DIR/BaseOS"
  elif [[ -d "$MOUNT_DIR/baseos" ]]; then
    BASEOS_PATH="$MOUNT_DIR/baseos"
  fi

  if [[ -d "$MOUNT_DIR/AppStream" ]]; then
    APPSTREAM_PATH="$MOUNT_DIR/AppStream"
  elif [[ -d "$MOUNT_DIR/appstream" ]]; then
    APPSTREAM_PATH="$MOUNT_DIR/appstream"
  fi
fi

# 2. 시스템 레포지토리 등록 상태 확인
SYSTEM_REPOS=$($PKG_MGR repolist all 2>/dev/null || true)
HAS_SYS_BASE=0
HAS_SYS_APP=0
if echo "$SYSTEM_REPOS" | grep -qi "RHEL810-base"; then
  HAS_SYS_BASE=1
fi
if echo "$SYSTEM_REPOS" | grep -qi "RHEL810-app"; then
  HAS_SYS_APP=1
fi

# 3. 레포지토리 인자 조립
if [[ -n "$BASEOS_PATH" && -n "$APPSTREAM_PATH" ]]; then
  log_info "마운트 디렉터리로부터 레포지토리 연결:"
  log_info "  - RHEL810-base : $BASEOS_PATH"
  log_info "  - RHEL810-app  : $APPSTREAM_PATH"
  REPO_ARGS+=(
    "--repofrompath=RHEL810-base,file://$BASEOS_PATH"
    "--repofrompath=RHEL810-app,file://$APPSTREAM_PATH"
    "--setopt=RHEL810-base.gpgcheck=0"
    "--setopt=RHEL810-app.gpgcheck=0"
  )
fi

REPO_ARGS+=(
  "--disablerepo=*"
  "--enablerepo=RHEL810-base"
  "--enablerepo=RHEL810-app"
  "--disableplugin=subscription-manager,product-id"
)

# 출력 디렉터리 생성
RPMS_OUT_DIR="$OUT_DIR/rpms"
mkdir -p "$RPMS_OUT_DIR"

log_info "----------------------------------------------------------"
log_info "레포지토리로부터 패키지 및 의존성 다운로드 시작"
log_info "----------------------------------------------------------"

DOWNLOADED_SOMETHING=0

# 방법 1: repotrack 사용 (yum-utils가 있는 경우 - 가장 확실한 의존성 전수 수집)
if command -v repotrack >/dev/null 2>&1; then
  log_info "repotrack 도구 감지: repotrack으로 전체 의존성 수집 시도..."
  set +e
  repotrack -p "$RPMS_OUT_DIR" \
    -r RHEL810-base -r RHEL810-app \
    "${TARGET_PACKAGES[@]}" 2>&1 | tail -n 5
  if [[ $? -eq 0 ]]; then
    DOWNLOADED_SOMETHING=1
    log_success "repotrack 수집 완료"
  fi
  set -e
fi

# 방법 2: dnf download 또는 yumdownloader 사용
if [[ $DOWNLOADED_SOMETHING -ne 1 ]]; then
  if $PKG_MGR download --help >/dev/null 2>&1; then
    log_info "$PKG_MGR download --resolve --alldeps 명령으로 다운로드 시도..."
    set +e
    $PKG_MGR download -y \
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
    log_info "yumdownloader --resolve 명령으로 다운로드 시도..."
    set +e
    yumdownloader --destdir="$RPMS_OUT_DIR" \
      --resolve \
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

# 방법 3: dnf/yum reinstall/install --downloadonly
if [[ $DOWNLOADED_SOMETHING -ne 1 ]]; then
  log_info "$PKG_MGR install/reinstall --downloadonly 명령으로 수집 시도..."
  set +e
  # 이미 설치된 경우를 대비해 reinstall 먼저 시도 후 install 실행
  $PKG_MGR -y "${REPO_ARGS[@]}" reinstall --downloadonly --downloaddir="$RPMS_OUT_DIR" "${TARGET_PACKAGES[@]}" >/dev/null 2>&1 || true
  $PKG_MGR -y "${REPO_ARGS[@]}" install --downloadonly --downloaddir="$RPMS_OUT_DIR" "${TARGET_PACKAGES[@]}" >/dev/null 2>&1 || true
  set -e
fi

# 방법 4: 마운트 디렉터리(/mnt/rhel810)에서 직접 탐색 및 복사 (직접 보완)
if [[ -d "$MOUNT_DIR" ]]; then
  log_info "마운트 디렉터리($MOUNT_DIR)에서 필수 패키지 및 주요 의존성 RPM 직접 복사 확인..."
  ALL_SEARCH_PKGS=("${TARGET_PACKAGES[@]}" "${KNOWN_DEPENDENCIES[@]}")

  for pkg in "${ALL_SEARCH_PKGS[@]}"; do
    # 이미 RPMS_OUT_DIR에 해당 패키지 파일이 있는지 확인
    if ! find "$RPMS_OUT_DIR" -maxdepth 1 -name "${pkg}-[0-9]*.rpm" 2>/dev/null | grep -q .; then
      found_files=$(find "$MOUNT_DIR" -maxdepth 4 -name "${pkg}-[0-9]*.rpm" 2>/dev/null || true)
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

# 필수 5개 패키지 수집 검증
log_info "----------------------------------------------------------"
log_info "필수 패키지 수집 검증"
log_info "----------------------------------------------------------"
MISSING=()
for pkg in "${TARGET_PACKAGES[@]}"; do
  found=$(find "$RPMS_OUT_DIR" -maxdepth 1 -name "${pkg}-[0-9]*.rpm" 2>/dev/null | head -n 1 || true)
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
log_success "총 ${RPM_COUNT}개의 RPM 파일이 성공적으로 준비되었습니다."

# createrepo_c 가 있으면 로컬 repodata 생성
if command -v createrepo_c >/dev/null 2>&1; then
  log_info "createrepo_c 로 로컬 repodata 메타데이터 생성 중..."
  createrepo_c --quiet "$RPMS_OUT_DIR" || true
fi

# SHA256 체크섬 파일 생성
log_info "무결성 검증용 SHA256 체크섬(checksums.sha256) 생성 중..."
(
  cd "$RPMS_OUT_DIR"
  sha256sum ./*.rpm > "$OUT_DIR/checksums.sha256"
)

# 패키지 매니페스트 생성
log_info "패키지 매니페스트(package_manifest.txt) 작성 중..."
{
  printf "# RHEL 8.10 Saved Packages Manifest\n"
  printf "# Generated: %s\n" "$(date '+%Y-%m-%d %H:%M:%S')"
  printf "# Source Mount: %s\n" "$MOUNT_DIR"
  printf "# Repositories: RHEL810-base, RHEL810-app\n\n"
  printf "%-30s %-25s %-10s\n" "PACKAGE NAME" "VERSION-RELEASE" "ARCH"
  printf "%s\n" "----------------------------------------------------------------------"
  for rpm_file in "$RPMS_OUT_DIR"/*.rpm; do
    rpm -qp --queryformat "%-30{NAME} %-25{VERSION}-%{RELEASE} %-10{ARCH}\n" "$rpm_file" 2>/dev/null || \
      printf "%-65s (info unavailable)\n" "$(basename "$rpm_file")"
  done
} > "$OUT_DIR/package_manifest.txt"

# 설치 스크립트 번들링
SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
if [[ -f "$SCRIPT_DIR/install_packages.sh" ]]; then
  log_info "오프라인 설치 스크립트(install_packages.sh)를 번들에 복사 중..."
  cp -f "$SCRIPT_DIR/install_packages.sh" "$OUT_DIR/"
  chmod +x "$OUT_DIR/install_packages.sh"
fi

# 압축 번들 생성 옵션 (-a / --archive)
if [[ $CREATE_ARCHIVE -eq 1 ]]; then
  TAR_NAME="$(basename "$OUT_DIR").tar.gz"
  PARENT_DIR=$(dirname "$OUT_DIR")
  DIR_NAME=$(basename "$OUT_DIR")
  log_info "압축 번들 생성 중 ($PARENT_DIR/$TAR_NAME)..."
  tar -czf "$PARENT_DIR/$TAR_NAME" -C "$PARENT_DIR" "$DIR_NAME"
  (cd "$PARENT_DIR" && sha256sum "$TAR_NAME" > "$TAR_NAME.sha256")
  ARCHIVE_SIZE=$(du -h "$PARENT_DIR/$TAR_NAME" | cut -f1)
  log_success "압축 번들 완료: $PARENT_DIR/$TAR_NAME (${ARCHIVE_SIZE})"
fi

log_info "=========================================================="
log_success "패키지 저장 작업이 성공적으로 완료되었습니다!"
log_info "저장 경로: $(realpath "$OUT_DIR")"
log_info "총 파일 수: ${RPM_COUNT}개 RPM"
log_info ""
log_info "다른 서버(폐쇄망/오프라인)로 배포하는 방법:"
log_info "  1. 저장된 디렉터리($OUT_DIR) 또는 압축본을 대상 서버로 복사:"
log_info "     scp -r $OUT_DIR user@<대상서버_IP>:/tmp/"
log_info "  2. 대상 서버에서 설치 실행:"
log_info "     cd /tmp/$(basename "$OUT_DIR")"
log_info "     sudo ./install_packages.sh -s"
log_info "=========================================================="
