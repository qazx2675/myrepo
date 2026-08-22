#!/usr/bin/env bash
# 폐쇄망 vCenter-PowerShell 실습 환경 설치 스크립트 (Phase 2 산출물)
# offline-package-structure.md의 번들 레이아웃을 전제로 동작한다.
set -euo pipefail

BUNDLE_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PS_INSTALL_DIR="/opt/microsoft/powershell/7"
PS_SYMLINK="/usr/bin/pwsh"
PS_MODULE_DIR="${PS_INSTALL_DIR}/Modules"
VCTEST_INSTALL_DIR="/opt/vc-test-env"

log()  { echo "[setup] $*"; }
fail() { echo "[setup][ERROR] $*" >&2; exit 1; }

# ---------- 1. 설치 전 검증 ----------
[ "$(id -u)" -eq 0 ] || fail "root 권한(sudo)으로 실행해야 합니다."

ARCH="$(uname -m)"
case "$ARCH" in
  x86_64)  PS_ARCH="x64" ;;
  aarch64) PS_ARCH="arm64" ;;
  *) fail "지원하지 않는 아키텍처: $ARCH" ;;
esac
log "아키텍처 확인: $ARCH ($PS_ARCH)"

[ -r /etc/os-release ] || fail "/etc/os-release를 읽을 수 없어 OS 확인 불가"
. /etc/os-release
log "OS 확인: ${PRETTY_NAME:-unknown}"

if command -v pwsh >/dev/null 2>&1; then
  log "기존 pwsh 발견: $(command -v pwsh) — 설치 계속 진행하되 덮어씁니다."
fi

PS_TARBALL="$(ls "${BUNDLE_DIR}"/powershell/powershell-*-linux-"${PS_ARCH}".tar.gz 2>/dev/null | head -n1 || true)"
[ -n "$PS_TARBALL" ] || fail "번들 안에서 powershell-*-linux-${PS_ARCH}.tar.gz 를 찾을 수 없습니다."

for lib in libicu libssl; do
  ldconfig -p | grep -q "$lib" || log "경고: $lib 관련 라이브러리를 찾지 못했습니다. PowerShell 실행 시 오류가 나면 이 패키지를 먼저 설치하세요."
done

[ -d "${BUNDLE_DIR}/modules/VMware.PowerCLI" ] || fail "번들 안에 modules/VMware.PowerCLI 가 없습니다."
[ -d "${BUNDLE_DIR}/modules/PSReadLine" ]      || fail "번들 안에 modules/PSReadLine 이 없습니다."
[ -x "${BUNDLE_DIR}/vc-test-env/vc-test-env" ] || log "경고: vc-test-env 실행 바이너리가 없습니다 — 실습 환경 연동 단계는 건너뜁니다."

# ---------- 2. PowerShell 바이너리 배치 ----------
log "PowerShell 배치: $PS_INSTALL_DIR"
mkdir -p "$PS_INSTALL_DIR"
tar xzf "$PS_TARBALL" -C "$PS_INSTALL_DIR"
ln -sf "${PS_INSTALL_DIR}/pwsh" "$PS_SYMLINK"
chmod +x "${PS_INSTALL_DIR}/pwsh"

# ---------- 3. 모듈 배치 ----------
log "PowerCLI / PSReadLine 모듈 배치: $PS_MODULE_DIR"
mkdir -p "$PS_MODULE_DIR"
cp -r "${BUNDLE_DIR}/modules/VMware.PowerCLI" "$PS_MODULE_DIR/"
cp -r "${BUNDLE_DIR}/modules/PSReadLine"      "$PS_MODULE_DIR/"

# ---------- 4. PowerCLI 기본 설정 ----------
log "PowerCLI 기본 설정 적용 (인증서 경고 무시, CEIP 비활성화)"
pwsh -NoProfile -Command '
  Import-Module VMware.PowerCLI -ErrorAction Stop
  Set-PowerCLIConfiguration -InvalidCertificateAction Ignore -ParticipateInCEIP $false -Confirm:$false | Out-Null
'

# ---------- 5. 커스텀 프로필 모듈 배치 + $PROFILE 등록 ----------
PROFILE_MODULE_DIR="${PS_INSTALL_DIR}/vcenter-profile-modules"
log "자동완성/권고 모듈 배치: $PROFILE_MODULE_DIR"
mkdir -p "$PROFILE_MODULE_DIR"
cp "${BUNDLE_DIR}/profile/VCenterAdvisory.psm1"   "$PROFILE_MODULE_DIR/"
cp "${BUNDLE_DIR}/profile/VCenterCompleters.psm1" "$PROFILE_MODULE_DIR/"

ALL_USERS_PROFILE="${PS_INSTALL_DIR}/profile.ps1"
MARKER="# >>> vcenter-powershell-autocompletion (setup.sh가 등록) >>>"
if ! grep -qsF "$MARKER" "$ALL_USERS_PROFILE" 2>/dev/null; then
  {
    echo "$MARKER"
    echo "Import-Module '${PROFILE_MODULE_DIR}/VCenterAdvisory.psm1'"
    echo "Import-Module '${PROFILE_MODULE_DIR}/VCenterCompleters.psm1'"
    echo "Set-PSReadLineOption -PredictionSource HistoryAndPlugin -PredictionViewStyle ListView"
    echo "# <<< vcenter-powershell-autocompletion <<<"
  } >> "$ALL_USERS_PROFILE"
  log "\$PROFILE(all users)에 초기화 로직 등록 완료: $ALL_USERS_PROFILE"
else
  log "\$PROFILE에 이미 등록되어 있어 건너뜀"
fi

# ---------- 6. vc-test-env 배치 (있는 경우) ----------
if [ -x "${BUNDLE_DIR}/vc-test-env/vc-test-env" ]; then
  log "vc-test-env 배치: $VCTEST_INSTALL_DIR"
  mkdir -p "$VCTEST_INSTALL_DIR"
  cp "${BUNDLE_DIR}/vc-test-env/vc-test-env" "$VCTEST_INSTALL_DIR/"
  cp "${BUNDLE_DIR}/vc-test-env/export_vcsim_env.sh" "$VCTEST_INSTALL_DIR/" 2>/dev/null || true
  ln -sf "${VCTEST_INSTALL_DIR}/vc-test-env" /usr/local/bin/vc-test-env
fi

log "설치 완료. 새 셸에서 'pwsh' 실행 후 Get-VM 등을 입력해 안내 메시지를 확인하세요."
