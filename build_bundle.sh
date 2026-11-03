#!/usr/bin/env bash
# RHEL 8.10 / 9.6 오프라인 로컬 repo 번들 빌더 (RHEL 9.x 관리서버에서 root로 실행)
# 사용법: ./build_bundle.sh [-8 <RHEL8.10 ISO|디렉터리>] [-9 <RHEL9.6 ISO|디렉터리>] [-o <출력디렉터리>]
set -euo pipefail

ISO_RHEL8=""
ISO_RHEL9=""
OUT_DIR="./bundles"
PACKAGES=(autofs nscd rpcbind sssd tcsh nfs-utils)
VER8="8.10"
VER9="9.6"
GPG_KEY="/etc/pki/rpm-gpg/RPM-GPG-KEY-redhat-release"

WORK=""
MOUNTS=()
declare -A SRC # 메이저 버전 -> BaseOS/AppStream을 가진 디렉터리

log() { printf '[%s] %s\n' "$(date +%T)" "$*"; }
warn() { printf '경고: %s\n' "$*" >&2; }
die() { printf '오류: %s\n' "$*" >&2; exit 1; }

usage() {
  cat <<EOF
사용법: $0 [-8 <RHEL8.10 ISO 또는 디렉터리>] [-9 <RHEL9.6 ISO 또는 디렉터리>] [-o <출력디렉터리>]
  둘 중 최소 하나는 필요하며, 지정한 버전만 빌드합니다. (기본 출력: ./bundles)
EOF
}

# 종료 시 정리: umount 후 임시 디렉터리 삭제
cleanup() {
  local m busy=0
  set +e
  for m in "${MOUNTS[@]}"; do
    umount "$m" 2>/dev/null || umount -l "$m" 2>/dev/null
    mountpoint -q "$m" && busy=1
  done
  if [[ -n $WORK && -d $WORK ]]; then
    if ((busy)); then
      warn "umount 실패, 임시 디렉터리를 남깁니다: $WORK"
    else
      rm -rf "$WORK"
    fi
  fi
}

# 소스(ISO 또는 디렉터리)를 준비하고 SRC[메이저]에 저장
prepare_src() {
  local major=$1 ver=$2 src=$3 dir sub
  [[ -e $src ]] || die "RHEL $ver 소스를 찾을 수 없습니다: $src"
  if [[ -f $src ]]; then
    dir="$WORK/mnt$major"
    mkdir -p "$dir"
    log "ISO 마운트: $src"
    mount -o loop,ro "$src" "$dir" || die "ISO 마운트 실패: $src"
    MOUNTS+=("$dir")
  else
    dir=$(realpath "$src")
  fi
  [[ $dir =~ [[:space:],] ]] && die "경로에 공백/쉼표가 있으면 안 됩니다: $dir"
  for sub in BaseOS AppStream; do
    [[ -d $dir/$sub/repodata ]] ||
      die "RHEL $ver 소스에 $sub/repodata 가 없습니다: $dir (RHEL 설치 ISO 또는 압축 해제 디렉터리를 지정하세요)"
  done
  check_version "$dir" "$ver"
  SRC[$major]=$dir
}

# .discinfo / media.repo의 버전 문자열 확인 (불일치 시 경고만)
check_version() {
  local dir=$1 ver=$2 f seen=0 ok=0
  for f in "$dir/.discinfo" "$dir/media.repo"; do
    [[ -r $f ]] || continue
    seen=1
    if grep -qF "$ver" "$f"; then ok=1; fi
  done
  if ((seen && !ok)); then
    warn "소스 버전이 $ver 와 다를 수 있습니다: $dir"
  fi
}

have_prereqs() {
  dnf download --help >/dev/null 2>&1 && command -v createrepo_c >/dev/null 2>&1
}

# dnf download(dnf-plugins-core), createrepo_c가 없으면 ISO repo에서 설치
ensure_prereqs() {
  have_prereqs && return 0
  local host_major src="" k key d id
  local -a pkgs=() args=()
  host_major=$(rpm -E %rhel)
  src=${SRC[$host_major]:-}
  if [[ -z $src ]]; then
    for k in "${!SRC[@]}"; do src=${SRC[$k]}; break; done
    warn "호스트(RHEL $host_major)와 같은 메이저 ISO가 없어 다른 버전 소스로 설치합니다."
  fi
  dnf download --help >/dev/null 2>&1 || pkgs+=(dnf-plugins-core)
  command -v createrepo_c >/dev/null 2>&1 || pkgs+=(createrepo_c)
  log "필수 패키지 설치: ${pkgs[*]}"

  key=$GPG_KEY
  [[ -r $key ]] || key="$src/RPM-GPG-KEY-redhat-release"
  [[ -r $key ]] || warn "GPG 키를 찾을 수 없어 gpgcheck 없이 설치합니다."

  # shellcheck disable=SC2054 # 쉼표는 dnf 인자 값의 일부임
  args=(-y --disablerepo='*' --disableplugin=subscription-manager,product-id)
  for d in BaseOS AppStream; do
    id="prereq-${d,,}"
    args+=("--repofrompath=$id,file://$src/$d" "--enablerepo=$id")
    if [[ -r $key ]]; then
      args+=("--setopt=$id.gpgcheck=1" "--setopt=$id.gpgkey=file://$key")
    else
      args+=("--setopt=$id.gpgcheck=0")
    fi
  done
  dnf install "${args[@]}" "${pkgs[@]}" || die "필수 패키지 설치 실패: ${pkgs[*]}"
  have_prereqs || die "dnf download 또는 createrepo_c 를 사용할 수 없습니다."
}

# 빈 installroot 기준으로 전체 의존성 클로저 다운로드
# (dnf download --resolve --alldeps 는 sssd-common 의 조건부 의존성 "(libsss_autofs = V if autofs)" 를
#  놓치므로, 실제 트랜잭션을 해결하는 install --downloadonly 를 사용한다)
download_pkgs() {
  local major=$1 stage=$2 src=${SRC[$1]}
  local root="$WORK/root$major" cache="$WORK/cache$major"
  local b="el$major-baseos" a="el$major-appstream"
  mkdir -p "$root" "$cache" "$WORK/empty-repos" "$stage/Packages"
  # 호스트 설정(proxy/exclude 등)을 쓰지 않도록 최소 설정 파일 사용
  printf '[main]\n' >"$WORK/dnf.conf"
  # cachedir는 dnf 버전에 따라 installroot 하위로 붙을 수 있으나 어느 쪽이든 WORK 아래라 무방
  dnf -y --config="$WORK/dnf.conf" \
    --installroot="$root" --releasever="$major" \
    --setopt=reposdir="$WORK/empty-repos" --setopt=cachedir="$cache" \
    --setopt=module_platform_id="platform:el$major" \
    --setopt=install_weak_deps=0 \
    --disableplugin=subscription-manager,product-id \
    --repofrompath="$b,file://$src/BaseOS" --repofrompath="$a,file://$src/AppStream" \
    --setopt="$b.gpgcheck=0" --setopt="$a.gpgcheck=0" \
    install --downloadonly --downloaddir "$stage/Packages" "${PACKAGES[@]}" ||
    die "RHEL $major 패키지 다운로드(의존성 해결) 실패"
  rm -rf "$root" "$cache"
}

# 요청한 패키지가 모두 받아졌는지 확인
verify_pkgs() {
  local dir=$1 p bad=0
  for p in "${PACKAGES[@]}"; do
    if ! compgen -G "$dir/$p-[0-9]*.rpm" >/dev/null; then
      warn "패키지 누락: $p"
      bad=1
    fi
  done
  ((!bad)) || die "필수 패키지가 누락되었습니다."
}

# repodata 생성 후 tar.gz + sha256 작성 (기존 번들은 덮어씀)
pack_bundle() {
  local ver=$1 stage=$2 name="localrepo-rhel$1.tar.gz" count size
  createrepo_c --quiet --compress-type=gz "$stage" || die "createrepo_c 실패"
  tar -C "$WORK/stage" -czf "$WORK/$name" "rhel$ver"
  mv -f "$WORK/$name" "$OUT_DIR/$name"
  (cd "$OUT_DIR" && sha256sum "$name" >"$name.sha256")
  count=$(find "$stage/Packages" -name '*.rpm' | wc -l)
  size=$(du -h "$OUT_DIR/$name" | cut -f1)
  log "RHEL $ver 완료: rpm ${count}개, 번들 ${size} -> $OUT_DIR/$name"
}

build_one() {
  local major=$1 ver=$2 stage="$WORK/stage/rhel$2"
  log "RHEL $ver 번들 빌드 시작"
  download_pkgs "$major" "$stage"
  verify_pkgs "$stage/Packages"
  pack_bundle "$ver" "$stage"
}

main() {
  local opt
  while getopts '8:9:o:h' opt; do
    case $opt in
      8) ISO_RHEL8=$OPTARG ;;
      9) ISO_RHEL9=$OPTARG ;;
      o) OUT_DIR=$OPTARG ;;
      *) usage; exit 1 ;;
    esac
  done
  [[ -n $ISO_RHEL8 || -n $ISO_RHEL9 ]] || { usage; die "RHEL 8.10 또는 9.6 소스가 하나 이상 필요합니다."; }
  ((EUID == 0)) || die "root 권한으로 실행해야 합니다."

  OUT_DIR=$(realpath -m "$OUT_DIR")
  mkdir -p "$OUT_DIR"
  WORK=$(mktemp -d -p "$OUT_DIR" .work.XXXXXX)
  trap cleanup EXIT
  trap 'exit 130' INT TERM

  if [[ -n $ISO_RHEL8 ]]; then prepare_src 8 "$VER8" "$ISO_RHEL8"; fi
  if [[ -n $ISO_RHEL9 ]]; then prepare_src 9 "$VER9" "$ISO_RHEL9"; fi
  ensure_prereqs
  if [[ -n $ISO_RHEL8 ]]; then build_one 8 "$VER8"; fi
  if [[ -n $ISO_RHEL9 ]]; then build_one 9 "$VER9"; fi
  log "모든 작업이 끝났습니다."
}

main "$@"
