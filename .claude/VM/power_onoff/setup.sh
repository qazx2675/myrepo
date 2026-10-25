#!/usr/bin/env bash
# setup.sh — 폐쇄망(오프라인) 빌드 + 설치까지 한 번에. vendor/ 안의 의존성만 사용, 인터넷 접속 시도 안 함.
#   bash setup.sh                       # 빌드 → ./vm_power_onoff, ./cross_check/bin/cross_check
#                                       #   root(또는 sudo -n 가능)이면 이어서 install.sh 까지 자동 실행
#   bash setup.sh [허용디렉터리]        # install.sh 에 넘길 allowed_dir (기본: 이 폴더의 절대경로)
#   bash setup.sh --no-install          # 빌드만 (install.sh 실행 안 함)
#   bash setup.sh --disable-cross-check # install.sh 에 넘김: pwonoff.conf 의 cross_check=off (교차검증 끄기, 0.3.0 부터 기본 on)
#   bash setup.sh --enable-cross-check  # install.sh 에 넘김: 기본 on 이라 효과 없음(안내만, 예전 스크립트 호환용)
#   bash setup.sh --approval-ttl 300    # install.sh 에 넘김: 새로 만드는 cross_check.conf 의 approval_ttl_sec(10~3600초, 기본 120=2분)
#   bash setup.sh --skip-vm-power-onoff # vm_power_onoff 빌드·설치를 건너뛰고 cross_check 만 (Go 1.26.5 미만 서버 등)
#   bash setup.sh --os6                 # OS6(RHEL/CentOS 6)용: bin_os6/*.gz 를 풀어 쓴다 (OS6 서버에서는 자동)
# Go 버전: vm_power_onoff 는 1.26.5 이상, cross_check 는 표준 라이브러리만 써서 1.20 이상이면 된다.
# 참고: vcsim 시험(go test -tags vcsim)은 vendor 에 없는 모듈이 필요 없도록 vendor 에 simulator 까지 포함되어 있다.
set -euo pipefail
cd "$(dirname "$0")"
HERE="$(pwd -P)"

usage() { sed -n '2,14p' "$0" | sed 's/^# \{0,1\}//'; }

OS6=0
NO_INSTALL=0
ENABLE_CC=0
DISABLE_CC=0
SKIP_VM=0
APPROVAL_TTL=""
APPROVAL_TTL_GIVEN=0
ALLOWED=""
while [ $# -gt 0 ]; do
  case "$1" in
    --os6) OS6=1 ;;
    --no-install) NO_INSTALL=1 ;;
    --enable-cross-check) ENABLE_CC=1 ;;
    --disable-cross-check) DISABLE_CC=1 ;;
    --skip-vm-power-onoff) SKIP_VM=1 ;;
    --approval-ttl)
      [ $# -ge 2 ] || { echo "오류: --approval-ttl 에는 초 단위 값이 필요합니다 (예: --approval-ttl 300)" >&2; exit 2; }
      APPROVAL_TTL="$2"; APPROVAL_TTL_GIVEN=1; shift ;;
    --approval-ttl=*) APPROVAL_TTL="${1#*=}"; APPROVAL_TTL_GIVEN=1 ;;
    -h|--help) usage; exit 0 ;;
    -*) echo "오류: 알 수 없는 옵션: $1" >&2; usage >&2; exit 2 ;;
    *)
      [ -z "$ALLOWED" ] || { echo "오류: 허용디렉터리는 하나만 지정합니다: $1" >&2; exit 2; }
      ALLOWED="$1" ;;
  esac
  shift
done

# OS6(커널 2.6.32)는 이 Go 로 만든 실행파일이 뜨지 않아 Go 1.20 으로 미리 빌드한 실행파일을 푼다
case "$(uname -r)" in 2.6.*) OS6=1 ;; esac
grep -qs 'release 6\.' /etc/redhat-release && OS6=1

CC_DIR="$HERE/cross_check"
CC_OUT="$CC_DIR/bin/cross_check"

# 빌드가 끝난 뒤: install.sh 자동 실행 또는 안내
finish() {
  local args=()
  [ -n "$ALLOWED" ] && args+=("$ALLOWED")
  [ "$ENABLE_CC" -eq 1 ] && args+=(--enable-cross-check)
  [ "$DISABLE_CC" -eq 1 ] && args+=(--disable-cross-check)
  [ "$SKIP_VM" -eq 1 ] && args+=(--skip-vm-power-onoff)
  [ "$APPROVAL_TTL_GIVEN" -eq 1 ] && args+=(--approval-ttl "$APPROVAL_TTL")
  local cmd="sudo bash $HERE/install.sh${args[*]+ ${args[*]}}"
  echo
  if [ "$NO_INSTALL" -eq 1 ]; then
    echo '완료(빌드만). 다음으로 진행하십시오:'
    echo "  1) $cmd        # /usr/local/bin, /etc/vcetner_pwon_off, /etc/cross_check, crontab, 데몬 기동"
    manual_steps 2
    return 0
  fi
  if [ "$(id -u)" -eq 0 ]; then
    echo "[INFO] root 로 실행 중 — install.sh 를 이어서 실행합니다"
    bash ./install.sh ${args[@]+"${args[@]}"}
  elif command -v sudo >/dev/null 2>&1 && sudo -n true 2>/dev/null; then
    echo "[INFO] sudo 사용 가능 — sudo bash install.sh 를 이어서 실행합니다"
    sudo -n bash ./install.sh ${args[@]+"${args[@]}"}
  else
    echo '[INFO] root 가 아니고 sudo 를 비밀번호 없이 쓸 수 없어 설치는 하지 않았습니다. 다음을 직접 실행하십시오:'
    echo "  1) $cmd"
    manual_steps 2
  fi
}

# manual_steps <시작번호> — 설치 뒤 사람이 해야 하는 일
manual_steps() {
  local n="$1"
  echo "  $n) /etc/vcetner_pwon_off/pwonoff.conf 의 users / vcenters 를 환경에 맞게 수정"; n=$((n + 1))
  echo "  $n) allowed_dir 에 on_off_<user>.txt 를 만들고 그 디렉터리에서 vm_power_onoff 실행"; n=$((n + 1))
  echo "  $n) 교차검증은 기본 on — 각 사람이 한 번씩 등록: cross_check --user <이름>   (선착순 등록이므로 빨리)"; n=$((n + 1))
  echo "     (안전장치 2/5 확인자 번호 선택, 3/5 VM 이름 입력 대신 확인자 승인. 끄려면 pwonoff.conf 에 cross_check=off 또는 install.sh --disable-cross-check)"
  echo "  $n) 관리자(admin) 패스워드는 /etc/cross_check/auth.conf 에 있음 (install.sh 가 처음 한 번만 화면에 출력)"
}

# unpack_cc_os6 — bin_os6/cross_check.gz 를 cross_check/bin/cross_check 로 푼다. 없으면 경고만.
unpack_cc_os6() {
  mkdir -p "$CC_DIR/bin"
  if [ -f bin_os6/cross_check.gz ] && gzip -dc bin_os6/cross_check.gz > "$CC_OUT.tmp"; then
    mv -f "$CC_OUT.tmp" "$CC_OUT" && chmod 755 "$CC_OUT"
    echo "설치 완료: $CC_OUT"
  else
    rm -f "$CC_OUT.tmp"
    echo "[경고] bin_os6/cross_check.gz 가 없거나 풀 수 없습니다 — cross_check(교차검증)는 설치되지 않습니다." >&2
    echo "       Go 1.20 빌드 서버에서 bash build_os6.sh 로 만든 뒤 다시 실행하십시오. (vm_power_onoff 는 계속 진행)" >&2
  fi
}

if [ "$OS6" -eq 1 ]; then
  echo "[INFO] OS6 모드 — bin_os6/vm_power_onoff.gz, bin_os6/cross_check.gz 를 풀어 설치합니다"
  if [ "$SKIP_VM" -eq 1 ]; then
    echo "[INFO] --skip-vm-power-onoff: vm_power_onoff 는 건너뜁니다"
  elif [ -f bin_os6/vm_power_onoff.gz ] && gzip -dc bin_os6/vm_power_onoff.gz > vm_power_onoff.tmp; then
    mv -f vm_power_onoff.tmp vm_power_onoff && chmod 755 vm_power_onoff
    echo "설치 완료: $(pwd)/vm_power_onoff"
  else
    rm -f vm_power_onoff.tmp
    echo "오류: bin_os6/vm_power_onoff.gz 가 없거나 풀 수 없습니다 (Go 1.20 빌드 서버에서 bash build_os6.sh)" >&2
    exit 1
  fi
  unpack_cc_os6
  finish
  exit 0
fi

if ! command -v go >/dev/null 2>&1; then
  echo "오류: go 를 찾을 수 없습니다. Go 툴체인(vm_power_onoff 1.26.5 이상, cross_check 만이면 1.20 이상)을 먼저 설치하십시오." >&2
  exit 2
fi

export GOPROXY=off
export GOFLAGS=-mod=vendor

if [ "$SKIP_VM" -eq 1 ]; then
  echo "[INFO] --skip-vm-power-onoff: vm_power_onoff 빌드를 건너뜁니다"
else
  go build -mod=vendor -o vm_power_onoff .
  echo
  echo "빌드 완료: $(pwd)/vm_power_onoff"
fi

# cross_check: 별도 모듈(표준 라이브러리만, vendor 불필요). Go 1.20 이상.
GOV="$(go env GOVERSION 2>/dev/null || true)"   # 예: go1.25.3
GO_MINOR="$(printf '%s\n' "$GOV" | sed -n 's/^go1\.\([0-9][0-9]*\).*/\1/p')"
if [ -n "$GO_MINOR" ] && [ "$GO_MINOR" -lt 20 ]; then
  echo "오류: cross_check 는 Go 1.20 이상이 필요합니다 (현재 $GOV)" >&2
  exit 2
fi
mkdir -p "$CC_DIR/bin"
(cd "$CC_DIR" && GOPROXY=off GOFLAGS=-mod=mod GOWORK=off GOTOOLCHAIN=local GOSUMDB=off \
   go build -o bin/cross_check ./cmd/cross_check)
echo "빌드 완료: $CC_OUT"

finish
