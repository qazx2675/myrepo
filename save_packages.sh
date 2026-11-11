#!/usr/bin/env bash
# ==============================================================================
# save_packages.sh
# RHEL 8.10 마운트 이미지(/mnt/rhel810)에서 필수 패키지 및 전체 의존성 RPM 저장 스크립트
#
# 대상 패키지: nscd, autofs, rpcbind, nfs-utils, tcsh
# ==============================================================================
set -euo pipefail

# 기본 설정값
DEFAULT_MOUNT_DIR="/mnt/rhel810"
MOUNT_DIR="$DEFAULT_MOUNT_DIR"
OUT_DIR="./rhel810_packages"
CREATE_ARCHIVE=0
TARGET_PACKAGES=("nscd" "autofs" "rpcbind" "nfs-utils" "tcsh")

# 색상 출력 설정
if [[ -t 1 ]]; then
  C_RESET="\033[0m"
  C_RED="\033[1;31m"
  C_GREEN="\033[1;32m"
  C_YELLOW="\033[1;33m"
  C_BLUE="\033[1;34m"
  C_CYAN="\033[1;36m"
else
  C_RESET=""
  C_RED=""
  C_GREEN=""
  C_YELLOW=""
  C_BLUE=""
  C_CYAN=""
fi

log_info()    { printf "${C_BLUE}[INFO]${C_RESET} %s\n" "$*"; }
log_success() { printf "${C_GREEN}[SUCCESS]${C_RESET} %s\n" "$*"; }
log_warn()    { printf "${C_YELLOW}[WARN]${C_RESET} %s\n" "$*" >&2; }
log_error()   { printf "${C_RED}[ERROR]${C_RESET} %s\n" "$*" >&2; }

usage() {
  cat <<EOF
사용법: $0 [옵션]

RHEL 8.10 설치 이미지(/mnt/rhel810)에서 대상 패키지와 전체 의존성을 추출하여
오프라인 설치용 독립 번들 디렉터리로 저장합니다.

대상 패키지:
  - nscd, autofs, rpcbind, nfs-utils, tcsh (총 5개 및 하위 의존성 전체)

옵션:
  -m, --mount <경로>     RHEL 8.10 ISO 마운트 디렉터리 (기본값: /mnt/rhel810)
  -o, --output <디렉터리> 패키지 저장 출력 디렉터리 (기본값: ./rhel810_packages)
  -a, --archive          저장 완료 후 .tar.gz 압축 파일 생성
  -h, --help             이 도움말 화면 출력

사용 예시:
  # 기본 마운트 위치(/mnt/rhel810)에서 추출하여 ./rhel810_packages 에 저장
  sudo $0

  # 마운트 경로와 출력 디렉터리 지정 및 tar.gz 압축본 생성
  sudo $0 -m /mnt/rhel810 -o ./my_bundle -a
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
log_info " RHEL 8.10 오프라인 패키지 추출 도구"
log_info " 대상 패키지: ${TARGET_PACKAGES[*]}"
log_info " 마운트 경로: $MOUNT_DIR"
log_info " 출력 디렉터리: $OUT_DIR"
log_info "=========================================================="

# 마운트 디렉터리 검증
if [[ ! -d "$MOUNT_DIR" ]]; then
  log_error "마운트 디렉터리를 찾을 수 없습니다: $MOUNT_DIR"
  log_error "다음 명령어로 RHEL 8.10 이미지를 먼저 마운트해 주십시오:"
  log_error "  mkdir -p /mnt/rhel810"
  log_error "  mount -o loop /path/to/rhel-8.10-x86_64-dvd.iso /mnt/rhel810"
  exit 1
fi

# 레포지토리 위치 탐색 (BaseOS / AppStream)
REPO_SOURCES=()
if [[ -d "$MOUNT_DIR/BaseOS/repodata" && -d "$MOUNT_DIR/AppStream/repodata" ]]; then
  REPO_SOURCES+=("BaseOS" "$MOUNT_DIR/BaseOS")
  REPO_SOURCES+=("AppStream" "$MOUNT_DIR/AppStream")
elif [[ -d "$MOUNT_DIR/repodata" ]]; then
  REPO_SOURCES+=("RHEL8" "$MOUNT_DIR")
else
  # repodata 하위 디렉터리 자동 검색
  while IFS= read -r rdir; do
    parent_dir=$(dirname "$rdir")
    base_name=$(basename "$parent_dir")
    REPO_SOURCES+=("$base_name" "$parent_dir")
  done < <(find "$MOUNT_DIR" -maxdepth 2 -type d -name "repodata" 2>/dev/null)
fi

if [[ ${#REPO_SOURCES[@]} -eq 0 ]]; then
  log_error "$MOUNT_DIR 내에서 유효한 repodata(RPM 메타데이터)를 찾을 수 없습니다."
  log_error "RHEL 8.10 정규 DVD ISO 또는 BaseOS/AppStream이 포함되어 있는지 확인하세요."
  exit 1
fi

log_info "발견된 레포지토리 소스:"
for ((i=0; i<${#REPO_SOURCES[@]}; i+=2)); do
  log_info "  - ${REPO_SOURCES[i]}: ${REPO_SOURCES[i+1]}"
done

# dnf 도구 확인
if ! command -v dnf >/dev/null 2>&1; then
  log_error "시스템에 'dnf' 명령어가 설치되어 있지 않습니다."
  exit 1
fi

# 임시 작업 디렉터리 생성
WORK_DIR=$(mktemp -d /tmp/rhel810_pkg_save.XXXXXX)
RPMS_OUT_DIR="$OUT_DIR/rpms"
mkdir -p "$RPMS_OUT_DIR"

cleanup() {
  local exit_code=$?
  if [[ -d "$WORK_DIR" ]]; then
    rm -rf "$WORK_DIR"
  fi
  if [[ $exit_code -ne 0 ]]; then
    log_error "작업 도중 오류가 발생하여 중단되었습니다."
  fi
  exit "$exit_code"
}
trap cleanup EXIT INT TERM

ROOT_DIR="$WORK_DIR/root"
CACHE_DIR="$WORK_DIR/cache"
EMPTY_REPOS="$WORK_DIR/empty-repos"
mkdir -p "$ROOT_DIR" "$CACHE_DIR" "$EMPTY_REPOS"

# 호스트 레포/플러그인 간섭을 방지하는 격리 dnf 설정 생성
cat <<EOF > "$WORK_DIR/dnf.conf"
[main]
cachedir=$CACHE_DIR
reposdir=$EMPTY_REPOS
keepcache=0
debuglevel=2
pkgpolicy=newest
install_weak_deps=0
EOF

# DNF 레포지토리 인자 구성
DNF_REPO_ARGS=()
for ((i=0; i<${#REPO_SOURCES[@]}; i+=2)); do
  name="${REPO_SOURCES[i],,}"
  path="${REPO_SOURCES[i+1]}"
  repo_id="media-$name"
  DNF_REPO_ARGS+=(
    "--repofrompath=$repo_id,file://$path"
    "--setopt=$repo_id.gpgcheck=0"
    "--enablerepo=$repo_id"
  )
done

log_info "빈 installroot 기반으로 대상 패키지 및 전체 의존성 클로저 다운로드를 시작합니다..."
log_info "(호스트에 이미 설치된 패키지와 무관하게, 다른 클린 시스템 설치에 필요한 전체 의존성을 수집합니다)"

set +e
dnf -y \
  --config="$WORK_DIR/dnf.conf" \
  --installroot="$ROOT_DIR" \
  --releasever="8" \
  --setopt=module_platform_id="platform:el8" \
  --disableplugin=subscription-manager,product-id \
  "${DNF_REPO_ARGS[@]}" \
  install --downloadonly --downloaddir="$RPMS_OUT_DIR" \
  "${TARGET_PACKAGES[@]}"
dnf_rc=$?
set -e

if [[ $dnf_rc -ne 0 ]]; then
  log_warn "installroot 트랜잭션 다운로드에 실패하여, dnf download fallback 방식으로 재시도합니다..."
  
  # dnf download (dnf-plugins-core) 시도
  set +e
  dnf -y \
    --config="$WORK_DIR/dnf.conf" \
    --setopt=module_platform_id="platform:el8" \
    --disableplugin=subscription-manager,product-id \
    "${DNF_REPO_ARGS[@]}" \
    download --destdir="$RPMS_OUT_DIR" --resolve --alldeps \
    "${TARGET_PACKAGES[@]}"
  fallback_rc=$?
  set -e
  
  if [[ $fallback_rc -ne 0 ]]; then
    log_error "패키지 다운로드 실패. ISO 내 대상 패키지가 존재하는지 확인하세요."
    exit 1
  fi
fi

# 필수 5개 패키지 다운로드 검증
log_info "필수 패키지 수집 검증 중..."
MISSING=()
for pkg in "${TARGET_PACKAGES[@]}"; do
  found=$(find "$RPMS_OUT_DIR" -maxdepth 1 -name "${pkg}-[0-9]*.rpm" | head -n 1)
  if [[ -n "$found" ]]; then
    log_success "  [OK] $pkg -> $(basename "$found")"
  else
    log_error "  [MISSING] $pkg 패키지가 수집되지 않았습니다."
    MISSING+=("$pkg")
  fi
done

if [[ ${#MISSING[@]} -gt 0 ]]; then
  log_error "다음 필수 패키지가 누락되어 작업을 중단합니다: ${MISSING[*]}"
  exit 1
fi

RPM_COUNT=$(find "$RPMS_OUT_DIR" -maxdepth 1 -name "*.rpm" | wc -l)
log_success "총 ${RPM_COUNT}개의 RPM 파일(의존성 포함)이 저장되었습니다."

# createrepo_c 가 설치되어 있으면 로컬 repo 메타데이터 생성
if command -v createrepo_c >/dev/null 2>&1; then
  log_info "createrepo_c 로 로컬 repodata 메타데이터 생성 중..."
  createrepo_c --quiet "$RPMS_OUT_DIR" || log_warn "repodata 생성 실패 (dnf install *.rpm 직접 설치에는 영향 없음)"
elif [[ -f "$MOUNT_DIR/AppStream/Packages/createrepo_c"* ]]; then
  log_info "마운트 미디어 내 createrepo_c 탐색됨 (필요시 수동 구성 가능)"
fi

# SHA256 체크섬 파일 생성
log_info "무결성 검증용 SHA256 체크섬(checksums.sha256) 생성 중..."
(
  cd "$RPMS_OUT_DIR"
  sha256sum ./*.rpm > "$OUT_DIR/checksums.sha256"
)

# 패키지 목록 매니페스트 생성
log_info "패키지 매니페스트(package_manifest.txt) 작성 중..."
{
  printf "# RHEL 8.10 Saved Packages Manifest\n"
  printf "# Generated: %s\n" "$(date '+%Y-%m-%d %H:%M:%S')"
  printf "# Source Mount: %s\n\n" "$MOUNT_DIR"
  printf "%-30s %-25s %-10s\n" "PACKAGE NAME" "VERSION-RELEASE" "ARCH"
  printf "%s\n" "----------------------------------------------------------------------"
  for rpm_file in "$RPMS_OUT_DIR"/*.rpm; do
    rpm -qp --queryformat "%-30{NAME} %-25{VERSION}-%{RELEASE} %-10{ARCH}\n" "$rpm_file" 2>/dev/null || \
      printf "%-65s (info unavailable)\n" "$(basename "$rpm_file")"
  done
} > "$OUT_DIR/package_manifest.txt"

# 설치 스크립트(install_packages.sh)를 출력 디렉터리에 번들링
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
log_info "  1. 저장된 디렉터리($OUT_DIR) 또는 압축본을 대상 서버로 복사 (scp, sftp, USB 등):"
log_info "     scp -r $OUT_DIR user@<대상서버_IP>:/tmp/"
log_info "  2. 대상 서버에서 설치 실행:"
log_info "     cd /tmp/$(basename "$OUT_DIR")"
log_info "     sudo ./install_packages.sh -s"
log_info "=========================================================="
