#!/usr/bin/env bash
# build_os6.sh — V2 도구 전체를 OS6(RHEL/CentOS 6)용으로 빌드해 bin_os6/ 에 넣는다.
#
# RHEL 6 는 커널 2.6.32 라 Go 1.21+ 로 만든 실행파일이 뜨지 않는다(Go 1.20 이 2.6.32 를 지원하는
# 마지막 릴리스). 그런데 vendor 한 govmomi 는 Go 1.21+ 표준 라이브러리(slices, 빌트인 min,
# reflect.TypeFor)를 쓴다. 그래서 도구마다 임시 사본을 만들어:
#   1. vendor 를 실제 파일로 복사(원본 govendor/ 는 건드리지 않음 — 일반 빌드는 그대로)
#   2. go.mod / vendor/modules.txt 의 go 버전을 1.20 으로 낮춤
#   3. govmomi 의 Go 1.21+ 전용 부분을 같은 동작의 Go 1.20 코드로 치환(패치 대상 문구를 먼저 확인)
#   4. Go 1.20 으로 정적 빌드(CGO_ENABLED=0) → bin_os6/<이름>.gz (저장소 크기 때문에 gzip)
# (.claude/VM/Network_Change_Integration_Script/projects/vm-network-migration/build_os6.sh 와 같은 방식)
#
# 사용법 (Go 1.20 이 있는 빌드 서버, 예: 192.168.0.60 의 /opt/go1.20):
#   bash build_os6.sh [Go1.20 실행파일]        # 기본: $GO_OS6 → /opt/go1.20/bin/go
# OS6 서버에서는 빌드하지 않고 bash setup.sh 가 bin_os6/ 의 실행파일을 제자리에 복사한다.
set -euo pipefail
cd "$(dirname "$0")"
ROOT="$(pwd)"; OUT="$ROOT/bin_os6"
GO_BIN="${1:-${GO_OS6:-/opt/go1.20/bin/go}}"
[ -x "$GO_BIN" ] || { echo "[오류] Go 1.20 실행파일이 없습니다: $GO_BIN" >&2; exit 2; }
"$GO_BIN" version | grep -q 'go1\.20' || { echo "[오류] Go 1.20 이 아닙니다: $("$GO_BIN" version)" >&2; exit 2; }

# setup.sh 와 같은 대상 ("이름:폴더:실행파일")
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

need() { grep -q "$2" "$1" || { echo "[오류] 패치 대상 문구를 못 찾음: $1 — govmomi 버전이 바뀌었으면 이 스크립트도 갱신하세요." >&2; exit 3; }; }

# patch_govmomi <vendor 폴더> — Go 1.21+ 전용 부분만 Go 1.20 코드로 바꾼다(파일이 있을 때만)
patch_govmomi() {
  local V="$1/github.com/vmware/govmomi" F XD f
  [ -d "$V" ] || return 0
  F="$V/internal/helpers.go"
  if [ -f "$F" ] && grep -q '"slices"' "$F"; then
    need "$F" 'return slices.Contains(vsanFS, ds.Summary.Type)'
    sed -i '/^\t"slices"$/d' "$F"
    sed -i 's/return slices.Contains(vsanFS, ds.Summary.Type)/for _, v := range vsanFS { if v == ds.Summary.Type { return true } }\n\treturn false/' "$F"
  fi
  F="$V/object/option_value_list.go"
  if [ -f "$F" ] && grep -q '"slices"' "$F"; then
    need "$F" 'return slices.Contains(strVals, strings.ToLower(tval))'
    sed -i '/^\t"slices"$/d' "$F"
    sed -i 's/return slices.Contains(strVals, strings.ToLower(tval))/lv := strings.ToLower(tval)\n\t\tfor _, sv := range strVals { if sv == lv { return true } }\n\t\treturn false/' "$F"
  fi
  XD="$V/vim25/xml"; F="$XD/typeinfo.go"
  if [ -f "$F" ] && grep -q 'minl := min(' "$F"; then
    sed -i 's/minl := min(len(newf.parents), len(oldf.parents))/minl := len(newf.parents)\n\t\tif len(oldf.parents) < minl { minl = len(oldf.parents) }/' "$F"
  fi
  if [ -f "$F" ] && grep -q 'reflect\.TypeFor\[' "$XD"/*.go; then
    for f in "$XD"/*.go; do sed -i 's/reflect\.TypeFor\[/typeFor[/g' "$f"; done
    cat >> "$F" <<'GOEOF'

// typeFor 는 reflect.TypeFor[T]() (Go 1.22+) 의 대체 구현이다.
// OS6(Go 1.20) 빌드용으로 build_os6.sh 가 임시 사본에만 넣는다 — govmomi 원본에는 없다.
func typeFor[T any]() reflect.Type {
	return reflect.TypeOf((*T)(nil)).Elem()
}
GOEOF
  fi
}

mkdir -p "$OUT"
ok=0; fail=0
for t in "${TARGETS[@]}"; do
  IFS=: read -r name dir bin <<< "$t"
  printf '%-22s ' "$name"
  T="$(mktemp -d)"
  # 소스만 복사(vendor 링크·빌드 결과 제외) → vendor 는 setup.sh 가 가리키는 곳에서 실제 파일로 복사
  (cd "$ROOT/$dir" && tar cf - --exclude=./vendor --exclude="./$bin" .) | (cd "$T" && tar xf -)
  vsrc="$(sed -n 's/^.*ln -s "\([^"]*\)" vendor.*$/\1/p' "$ROOT/$dir/setup.sh" | head -1)"
  mkdir -p "$T/vendor"
  if [ -n "$vsrc" ]; then cp -r "$ROOT/$dir/$vsrc/." "$T/vendor/"
  else cp -rL "$ROOT/$dir/vendor/." "$T/vendor/"; fi
  # 윈도우에서 받은 사본이면 CRLF 라 아래 치환이 안 맞는다 — 임시 사본의 줄끝만 LF 로
  find "$T" -type f \( -name '*.go' -o -name go.mod -o -name modules.txt \) -exec sed -i 's/\r$//' {} +
  sed -i 's/^go 1\.[0-9.]*$/go 1.20/' "$T/go.mod"
  sed -i 's/^## explicit; go 1\.[0-9.]*$/## explicit; go 1.20/' "$T/vendor/modules.txt"
  patch_govmomi "$T/vendor"
  if (cd "$T" && GOTOOLCHAIN=local CGO_ENABLED=0 GOFLAGS=-mod=vendor GOPROXY=off GOSUMDB=off \
        "$GO_BIN" build -trimpath -ldflags "-s -w" -o "$T/$bin" .) > "$T/build.log" 2>&1; then
    # 저장소 크기를 줄이려고 gzip 으로 둔다(13MB → 약 4MB). OS6 서버에서 setup.sh 가 풀어 쓴다.
    gzip -9 -n -c "$T/$bin" > "$OUT/$bin.gz"
    printf 'OK   bin_os6/%s.gz (%s)\n' "$bin" "$(du -h "$OUT/$bin.gz" | cut -f1)"; ok=$((ok + 1))
  else
    echo "실패"; sed 's/^/    | /' "$T/build.log" | head -20; fail=$((fail + 1))
  fi
  rm -rf "$T"
done
echo
echo "[결과] 성공 $ok / 실패 $fail  ($("$GO_BIN" version))"
[ "$fail" -eq 0 ]
