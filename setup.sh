#!/usr/bin/env bash
# setup.sh — V2 전체 바이너리 빌드 (폐쇄망/오프라인 가능).
#
# 인터넷, git, go 모듈 프록시가 필요 없다. 필요한 것은 Go(1.26.5 이상)와 bash 뿐이다.
# 의존성은 이 폴더의 govendor/ 에 들어 있고, 각 도구의 setup.sh 가 그것을 vendor 로 링크해서
# `go build -mod=vendor` 로 빌드한다. 이 스크립트는 그 setup.sh 들을 한 번에 돌리고 결과를 요약한다.
#
# 사용법:
#   bash setup.sh              # 전체 빌드
#   bash setup.sh vm_create    # 지정한 도구만 (여러 개 가능: bash setup.sh vm_create nic_assign)
#   bash setup.sh -l           # 빌드 대상 목록 보기
#   bash setup.sh -c           # 빌드된 실행파일 지우기 (vendor 링크도 함께 정리)
#   bash setup.sh --os6        # OS6 용 실행파일(bin_os6/*.gz)을 제자리에 풀기 (OS6 서버에서는 자동)
#
# OS6(RHEL/CentOS 6, 커널 2.6.32)에서는 이 폴더의 Go 로 빌드한 실행파일이 뜨지 않고 빌드용 Go 도 설치할 수 없어,
# Go 1.20 으로 미리 빌드해 둔 bin_os6/<이름>.gz 를 풀어서 같은 자리에 놓는다(빌드 서버에서 bash build_os6.sh 로 만든다).
#
# 만들어지는 실행파일:
#   VMsetup/<이름>-source/<이름>                     (vm_create, affinity_setting, lpage_setting, ...)
#   vm-param-check-usability-improvement/vm-param-check/vm-param-check
set -uo pipefail
cd "$(dirname "$0")"
ROOT="$(pwd)"

# ---- 빌드 대상: "이름:폴더:실행파일" ----
TARGETS=(
  "vm_create:VMsetup/vm_create-source:vm_create"
  "vswitch_setting:VMsetup/vswitch_setting-source:vswitch_setting"
  "affinity_setting:VMsetup/affinity_setting-source:affinity_setting"
  "lpage_setting:VMsetup/lpage_setting-source:lpage_setting"
  "nic_assign:VMsetup/nic_assign-source:nic_assign"
  "tag_setting:VMsetup/tag_setting-source:tag_setting"
  "numa_preferht_setting:VMsetup/numa_preferht_setting-source:numa_preferht_setting"
  "license_assign:VMsetup/license_assign-source:license_assign"
  "mac_info:VMsetup/mac_info-source:mac_info"
  "main_conn:VMsetup/main_conn-source:main_conn"
  "vm-param-check:vm-param-check-usability-improvement/vm-param-check:vm-param-check"
)
MIN_GO="1.26.5"   # VMsetup 도구들의 go.mod 기준 (vm-param-check 는 더 낮아도 되지만 한 버전으로 통일)

die() { printf '[오류] %s\n' "$*" >&2; exit 1; }

# OS6 판단: --os6 옵션, VMSETUP_OS6=1, 커널 2.6.x, 또는 /etc/redhat-release 가 release 6
OS6=0
[ "${1:-}" = "--os6" ] && { OS6=1; shift; }
[ "${VMSETUP_OS6:-}" = 1 ] && OS6=1
case "$(uname -r)" in 2.6.*) OS6=1 ;; esac
grep -qs 'release 6\.' /etc/redhat-release && OS6=1

case "${1:-}" in
  -h|--help) sed -n '2,15p' "$0" | sed 's/^# \{0,1\}//'; exit 0 ;;
  -l|--list)
    for t in "${TARGETS[@]}"; do IFS=: read -r name dir bin <<< "$t"; printf '%-22s %s/%s\n' "$name" "$dir" "$bin"; done
    exit 0 ;;
  -c|--clean)
    for t in "${TARGETS[@]}"; do
      IFS=: read -r name dir bin <<< "$t"
      rm -f "$ROOT/$dir/$bin"
      # vendor 가 심볼릭 링크인 경우만 지운다 (license_assign 처럼 자체 vendor 폴더가 실제 파일이면 그대로 둔다)
      [ -L "$ROOT/$dir/vendor" ] && rm -f "$ROOT/$dir/vendor"
    done
    echo "[INFO] 빌드된 실행파일과 vendor 링크를 정리했습니다."
    exit 0 ;;
esac

# ---- 선택한 도구만 빌드 ----
SELECTED=()
if [ "$#" -eq 0 ]; then
  SELECTED=("${TARGETS[@]}")
else
  for want in "$@"; do
    found=""
    for t in "${TARGETS[@]}"; do
      IFS=: read -r name _ _ <<< "$t"
      [ "$name" = "$want" ] && { SELECTED+=("$t"); found=1; break; }
    done
    [ -n "$found" ] || die "알 수 없는 도구: $want (목록: bash setup.sh -l)"
  done
fi

# ---- OS6: 빌드하지 않고 bin_os6/*.gz 를 제자리에 푼다 ----
if [ "$OS6" -eq 1 ]; then
  echo "[INFO] OS6 모드 — bin_os6/ 의 미리 빌드한 실행파일(Go 1.20, 정적 링크)을 설치합니다"
  ok=0; fail=0
  for t in "${SELECTED[@]}"; do
    IFS=: read -r name dir bin <<< "$t"
    printf '%-22s ' "$name"
    if [ -f "$ROOT/bin_os6/$bin.gz" ] && gzip -dc "$ROOT/bin_os6/$bin.gz" > "$ROOT/$dir/$bin.tmp" \
       && chmod +x "$ROOT/$dir/$bin.tmp" && mv -f "$ROOT/$dir/$bin.tmp" "$ROOT/$dir/$bin"; then
      echo "OK   $dir/$bin"; ok=$((ok + 1))
    else
      rm -f "$ROOT/$dir/$bin.tmp"; echo "실패 (bin_os6/$bin.gz 없음 — 빌드 서버에서 bash build_os6.sh)"; fail=$((fail + 1))
    fi
  done
  chmod +x "$ROOT/VMsetup/vm_setup.sh" "$ROOT"/*.sh 2>/dev/null
  echo; echo "[결과] 성공 $ok / 실패 $fail"
  [ "$fail" -eq 0 ] || exit 1
  exit 0
fi

# ---- 사전 점검 ----
command -v go >/dev/null 2>&1 || die "go 가 없습니다. Go ${MIN_GO} 이상을 설치하세요 (PATH 에 go 가 있어야 합니다)."
GO_VER="$(go env GOVERSION 2>/dev/null | sed 's/^go//; s/[^0-9.].*$//')"
[ -n "$GO_VER" ] || GO_VER="$(go version | awk '{print $3}' | sed 's/^go//; s/[^0-9.].*$//')"
# 버전 비교: 정렬해서 MIN_GO 가 앞이거나 같으면 통과
if [ "$(printf '%s\n%s\n' "$MIN_GO" "$GO_VER" | sort -V | head -1)" != "$MIN_GO" ]; then
  die "Go 버전이 낮습니다: $GO_VER (필요: ${MIN_GO} 이상). 폐쇄망에서는 새 Go 를 자동으로 내려받지 못하므로 서버의 Go 를 올려야 합니다."
fi
[ -d "$ROOT/govendor/govmomi-0.55.1-standard" ] && [ -d "$ROOT/govendor/govmomi-0.39.0" ] \
  || die "govendor/ 폴더가 없거나 불완전합니다. 이 스크립트는 V2 폴더 전체(govendor 포함)와 함께 있어야 합니다."

# 오프라인 강제: 모듈 다운로드/새 툴체인 다운로드/체크섬 DB 조회를 모두 막는다
export GOFLAGS="-mod=vendor"
export GOPROXY=off
export GOTOOLCHAIN=local
export GOSUMDB=off
export GO111MODULE=on
export CGO_ENABLED="${CGO_ENABLED:-0}"

echo "[INFO] Go $GO_VER / 오프라인 빌드 (GOPROXY=off, GOFLAGS=-mod=vendor, GOTOOLCHAIN=local)"
echo "[INFO] 대상 ${#SELECTED[@]}개"
echo

ok=(); fail=(); LOGDIR="$(mktemp -d)"
for t in "${SELECTED[@]}"; do
  IFS=: read -r name dir bin <<< "$t"
  printf '%-22s ' "$name"
  [ -d "$ROOT/$dir" ] || { echo "실패 (폴더 없음: $dir)"; fail+=("$name"); continue; }
  if (cd "$ROOT/$dir" && bash setup.sh) >"$LOGDIR/$name.log" 2>&1 && [ -x "$ROOT/$dir/$bin" ]; then
    printf 'OK   %s (%s)\n' "$dir/$bin" "$(du -h "$ROOT/$dir/$bin" | cut -f1)"
    ok+=("$name")
  else
    echo "실패"
    fail+=("$name")
    sed 's/^/    | /' "$LOGDIR/$name.log" | tail -8
  fi
done
rm -rf "$LOGDIR"

# 배포본에서 실행권한이 빠졌을 수 있는 스크립트
[ -f "$ROOT/VMsetup/vm_setup.sh" ] && chmod +x "$ROOT/VMsetup/vm_setup.sh" 2>/dev/null

echo
echo "[결과] 성공 ${#ok[@]} / 실패 ${#fail[@]}"
if [ "${#fail[@]}" -gt 0 ]; then
  echo "[실패] ${fail[*]}"
  exit 1
fi
echo "다음: cd VMsetup && ./vm_setup.sh -u <user> -v <vCenter IP> -n   (계획만 확인)"
