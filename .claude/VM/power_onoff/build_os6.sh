#!/usr/bin/env bash
# build_os6.sh — vm_power_onoff 를 OS6(RHEL/CentOS 6, 커널 2.6.32)용으로 빌드해 bin_os6/vm_power_onoff.gz 로 만든다.
# V2/build_os6.sh 와 같은 방식이다(Go 1.20 이 2.6.32 를 지원하는 마지막 릴리스, vendor 한 govmomi 의 Go 1.21+ 전용
# 부분을 임시 사본에서만 같은 동작의 코드로 치환, 정적 빌드 CGO_ENABLED=0). 원본 vendor/소스는 건드리지 않는다.
#
# 사용법 (Go 1.20 이 있는 빌드 서버, 예: 192.168.0.60 의 /opt/go1.20):
#   bash build_os6.sh [Go1.20 실행파일]        # 기본: $GO_OS6 → /opt/go1.20/bin/go
#   bash build_os6.sh --cross-check-only [Go1.20 실행파일]   # cross_check 만 (vendor 불필요)
# 이어서 cross_check(교차검증, 표준 라이브러리만) 도 같은 Go 1.20 으로 정적 빌드해 bin_os6/cross_check.gz 로 만든다.
# OS6 서버에서는 빌드하지 않고 bash setup.sh 가 bin_os6/vm_power_onoff.gz, bin_os6/cross_check.gz 를 풀어 쓴다.
# (vendor/ 는 이 폴더 안에 들어 있다 — 폴더만 받아도 빌드된다)
set -euo pipefail
cd "$(dirname "$0")"
CC_ONLY=0
if [ "${1:-}" = "--cross-check-only" ]; then CC_ONLY=1; shift; fi
ROOT="$(pwd)"; OUT="$ROOT/bin_os6"; BIN=vm_power_onoff
VENDOR_SRC="$ROOT/vendor"
GO_BIN="${1:-${GO_OS6:-/opt/go1.20/bin/go}}"
[ -x "$GO_BIN" ] || { echo "[오류] Go 1.20 실행파일이 없습니다: $GO_BIN" >&2; exit 2; }
"$GO_BIN" version | grep -q 'go1\.20' || { echo "[오류] Go 1.20 이 아닙니다: $("$GO_BIN" version)" >&2; exit 2; }
[ "$CC_ONLY" -eq 1 ] || [ -d "$VENDOR_SRC" ] || { echo "[오류] vendor 원본이 없습니다: $VENDOR_SRC" >&2; exit 2; }

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
T="$(mktemp -d)"; trap 'rm -rf "$T"' EXIT
if [ "$CC_ONLY" -eq 0 ]; then
# 소스만 복사(vendor 링크·빌드 결과·테스트 제외) → vendor 는 실제 파일로 복사
tar cf - --exclude=./vendor --exclude="./$BIN" --exclude=./bin_os6 --exclude=./cross_check --exclude=./dev --exclude='./run_*' --exclude='*_test.go' . | (cd "$T" && tar xf -)
mkdir -p "$T/vendor"; cp -r "$VENDOR_SRC/." "$T/vendor/"
# 윈도우에서 받은 사본이면 CRLF 라 치환이 안 맞는다 — 임시 사본의 줄끝만 LF 로
find "$T" -type f \( -name '*.go' -o -name go.mod -o -name modules.txt \) -exec sed -i 's/\r$//' {} +
sed -i 's/^go 1\.[0-9.]*$/go 1.20/' "$T/go.mod"
sed -i 's/^## explicit; go 1\.[0-9.]*$/## explicit; go 1.20/' "$T/vendor/modules.txt"
patch_govmomi "$T/vendor"
if (cd "$T" && GOTOOLCHAIN=local CGO_ENABLED=0 GOFLAGS=-mod=vendor GOPROXY=off GOSUMDB=off \
      "$GO_BIN" build -trimpath -ldflags "-s -w" -o "$T/$BIN" .) > "$T/build.log" 2>&1; then
  gzip -9 -n -c "$T/$BIN" > "$OUT/$BIN.gz"
  printf 'OK   bin_os6/%s.gz (%s) — %s\n' "$BIN" "$(du -h "$OUT/$BIN.gz" | cut -f1)" "$("$GO_BIN" version)"
else
  echo "실패"; sed 's/^/    | /' "$T/build.log" | head -30; exit 1
fi
fi

# ── cross_check (별도 모듈 ./cross_check, 표준 라이브러리만 → vendor·패치 불필요) ──
CC_BIN=cross_check
CC_SRC="$ROOT/cross_check"
[ -f "$CC_SRC/go.mod" ] && [ -d "$CC_SRC/cmd/cross_check" ] || { echo "[오류] cross_check 소스가 없습니다: $CC_SRC/cmd/cross_check" >&2; exit 2; }
CT="$T/cc"; mkdir -p "$CT"
(cd "$CC_SRC" && tar cf - --exclude=./bin --exclude=./test --exclude='*_test.go' .) | (cd "$CT" && tar xf -)
find "$CT" -type f \( -name '*.go' -o -name go.mod \) -exec sed -i 's/\r$//' {} +
sed -i 's/^go 1\.[0-9.]*$/go 1.20/' "$CT/go.mod"
if (cd "$CT" && GOTOOLCHAIN=local CGO_ENABLED=0 GOOS=linux GOARCH=amd64 GOFLAGS=-mod=mod GOPROXY=off GOSUMDB=off GOWORK=off \
      "$GO_BIN" build -trimpath -ldflags "-s -w" -o "$T/$CC_BIN" ./cmd/cross_check) > "$T/build_cc.log" 2>&1; then
  gzip -9 -n -c "$T/$CC_BIN" > "$OUT/$CC_BIN.gz"
  printf 'OK   bin_os6/%s.gz (%s) — %s\n' "$CC_BIN" "$(du -h "$OUT/$CC_BIN.gz" | cut -f1)" "$("$GO_BIN" version)"
else
  echo "실패 (cross_check)"; sed 's/^/    | /' "$T/build_cc.log" | head -30; exit 1
fi
