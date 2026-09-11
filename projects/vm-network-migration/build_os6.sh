#!/usr/bin/env bash
###############################################################################
# build_os6.sh — nm-* 를 OS6(RHEL/CentOS 6) 관리서버용으로 빌드
#
#   RHEL 6 는 커널 2.6.32 라 Go 1.21+ 툴체인이 만든 바이너리가 뜨지 않습니다
#   (Go 1.20 이 2.6.32 를 지원하는 마지막 릴리스). 하지만 이 프로젝트가 vendor 한
#   govmomi v0.55.1 은 Go 1.21+ 표준 라이브러리(`slices`, 빌트인 `min`,
#   `reflect.TypeFor`)를 씁니다 — go.mod 를 낮추는 것만으로는(ip_change/ldap_setting
#   과 달리) 부족합니다.
#
#   그래서 이 스크립트는:
#     1. 전체 모듈을 임시 디렉터리로 복사(원본 vendor/ 는 건드리지 않음 — 일반
#        빌드는 계속 최신 govmomi 기능/문법을 그대로 씁니다)
#     2. 그 사본의 vendor/ 안에서 Go 1.21+ 전용 부분 3곳만 동등한 Go 1.20
#        호환 코드로 치환 (아래 patch_* 함수, 각각 원본 문구를 grep 로 먼저
#        확인 — govmomi 버전이 바뀌어 문구가 달라지면 조용히 넘어가지 않고 실패)
#     3. go.mod 와 vendor/modules.txt 의 go 버전을 1.20 으로 낮춤
#     4. Go 1.20 툴체인으로 nm-* 7종을 빌드해 ../../bin_os6/ 에 복사
#
#   사용법:
#     ./build_os6.sh [Go1.20 실행파일 경로]
#     (인자 생략 시 GO_OS6 환경변수 → /opt/go1.20/bin/go → 실패 시 GOTOOLCHAIN
#      자동 다운로드(go1.20.14, 인터넷 필요) 순으로 찾습니다)
###############################################################################
set -euo pipefail
cd "$(dirname "$0")"
ROOT="$(pwd)"
OUT="$ROOT/../../bin_os6"

GO_BIN="${1:-${GO_OS6:-}}"
if [ -z "$GO_BIN" ] && [ -x /opt/go1.20/bin/go ]; then GO_BIN=/opt/go1.20/bin/go; fi
if [ -n "$GO_BIN" ]; then
  [ -x "$GO_BIN" ] || { echo "오류: Go 실행파일이 아닙니다: $GO_BIN" >&2; exit 2; }
  echo ">> Go1.20 툴체인: $GO_BIN"
  GO_ENV=()
else
  echo ">> 고정 Go1.20 실행파일을 못 찾아 GOTOOLCHAIN 자동 다운로드를 시도합니다 (인터넷 필요)."
  GO_BIN="go"
  GO_ENV=(GOTOOLCHAIN=go1.20.14)
fi

T="$(mktemp -d)"
trap 'rm -rf "$T"' EXIT
echo ">> 임시 사본: $T"
cp -r . "$T/"
rm -rf "$T/bin" "$T/.git"

# ── go.mod / vendor/modules.txt 의 go 버전을 1.20 으로 ──────────────────────
grep -q '^go 1\.25\.0$' "$T/go.mod" || { echo "오류: go.mod 의 'go 1.25.0' 줄을 못 찾음 — 버전이 바뀌었으면 이 스크립트도 갱신하세요." >&2; exit 3; }
sed -i 's/^go 1\.25\.0$/go 1.20/' "$T/go.mod"

grep -q '^## explicit; go 1\.25\.0$' "$T/vendor/modules.txt" || { echo "오류: vendor/modules.txt 의 govmomi go 버전 줄을 못 찾음." >&2; exit 3; }
sed -i 's/^## explicit; go 1\.25\.0$/## explicit; go 1.20/' "$T/vendor/modules.txt"

# ── 패치 1/3: internal/helpers.go 의 slices.Contains ────────────────────────
F="$T/vendor/github.com/vmware/govmomi/internal/helpers.go"
grep -q 'return slices.Contains(vsanFS, ds.Summary.Type)' "$F" || { echo "오류: helpers.go 패치 대상 문구를 못 찾음." >&2; exit 3; }
sed -i '/^\t"slices"$/d' "$F"
sed -i 's/return slices.Contains(vsanFS, ds.Summary.Type)/for _, v := range vsanFS { if v == ds.Summary.Type { return true } }\n\treturn false/' "$F"

# ── 패치 2/3: object/option_value_list.go 의 slices.Contains ────────────────
F="$T/vendor/github.com/vmware/govmomi/object/option_value_list.go"
grep -q 'return slices.Contains(strVals, strings.ToLower(tval))' "$F" || { echo "오류: option_value_list.go 패치 대상 문구를 못 찾음." >&2; exit 3; }
sed -i '/^\t"slices"$/d' "$F"
sed -i 's/return slices.Contains(strVals, strings.ToLower(tval))/lv := strings.ToLower(tval)\n\t\tfor _, sv := range strVals { if sv == lv { return true } }\n\t\treturn false/' "$F"

# ── 패치 3/3: vim25/xml 의 빌트인 min() 과 reflect.TypeFor[T]() ─────────────
XD="$T/vendor/github.com/vmware/govmomi/vim25/xml"
F="$XD/typeinfo.go"
grep -q 'minl := min(len(newf.parents), len(oldf.parents))' "$F" || { echo "오류: typeinfo.go 의 min() 문구를 못 찾음." >&2; exit 3; }
sed -i 's/minl := min(len(newf.parents), len(oldf.parents))/minl := len(newf.parents)\n\t\tif len(oldf.parents) < minl { minl = len(oldf.parents) }/' "$F"

for f in "$XD/marshal.go" "$XD/read.go" "$XD/typeinfo.go"; do
  grep -q 'reflect\.TypeFor\[' "$f" && sed -i 's/reflect\.TypeFor\[/typeFor[/g' "$f"
done
cat >>"$F" <<'GOEOF'

// typeFor 는 reflect.TypeFor[T]() (Go 1.22+) 의 대체 구현입니다.
// OS6(Go 1.20) 빌드 호환을 위해 이 저장소가 추가했습니다 — govmomi 원본에는 없습니다.
func typeFor[T any]() reflect.Type {
	return reflect.TypeOf((*T)(nil)).Elem()
}
GOEOF

# ── 빌드 ─────────────────────────────────────────────────────────────────
cd "$T"
mkdir -p "$OUT"
BINS="backup pgcreate disconnect connect verify rollback inventory"
for b in $BINS; do
  printf '  빌드: nm-%-12s' "$b"
  env "${GO_ENV[@]}" GOFLAGS=-mod=vendor GOPROXY=off "$GO_BIN" build -o "$OUT/nm-$b" "./cmd/$b"
  echo "-> bin_os6/nm-$b"
done

echo
echo "완료: $OUT"
ls -1 "$OUT"/nm-*
