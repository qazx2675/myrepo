#!/usr/bin/env bash
# tests/test_preprocess.sh — 전처리(§4) 회귀 테스트. 인프라 없이 bash 만으로 동작.
set -uo pipefail
cd "$(dirname "$0")/.."

DEBUG_LEVEL=0
LOG_FILE=/dev/null
# shellcheck source=lib/common.sh
. lib/common.sh
# shellcheck source=lib/preprocess.sh
. lib/preprocess.sh

PP_DOMAIN=seccae.com
PP_TAG=cae

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
fail=0

check() { # check <설명> <기대> <실제>
  if [ "$2" = "$3" ]; then
    echo "  ok  $1"
  else
    echo "  FAIL $1"
    echo "       기대: $2"
    echo "       실제: $3"
    fail=1
  fi
}

# ── Case 1/2/3 + FQDN + IP-as-BM ──────────────────────────────────────────
cat >"$tmp/in.txt" <<'EOF'
# Case 1 (4열)
web01 svc 10.20.30.11 30
# Case 2 (3열, 2번째가 IP) — PP_FOLDER 로 폴더명 주입
db01 10.20.30.40 55
# Case 3 (3열, 2번째가 PG명)
app01 EXISTING_PG 77
# 이미 FQDN
mail01.seccae.com PG_MAIL 88
# IP 를 BM 으로
10.0.0.5 rack 10.20.30.99 12
EOF

PP_FOLDER=myfolder
preprocess_vswitch "$tmp/in.txt" "$tmp/out.txt" >/dev/null

got="$(cat "$tmp/out.txt")"
want='web01.seccae.com svc-cae-10-20-30-0 30
db01.seccae.com myfolder-cae-10-20-30-0 55
app01.seccae.com EXISTING_PG 77
mail01.seccae.com PG_MAIL 88
10.0.0.5 rack-cae-10-20-30-0 12'
check "Case 1/2/3 + FQDN + IP-BM" "$want" "$got"

# ── 멱등: 표준화된 출력을 다시 넣어도 그대로 ──────────────────────────────
PP_FOLDER=""
preprocess_vswitch "$tmp/out.txt" "$tmp/out2.txt" >/dev/null
check "멱등성" "$(cat "$tmp/out.txt")" "$(cat "$tmp/out2.txt")"

# ── A2: 열 개수 오류 ─────────────────────────────────────────────────────
echo "onlytwo cols" >"$tmp/bad.txt"
if ( preprocess_vswitch "$tmp/bad.txt" "$tmp/bad.out" >/dev/null 2>&1 ); then
  echo "  FAIL A2(열 개수) — 오류가 나야 하는데 통과함"; fail=1
else
  echo "  ok  A2(열 개수) 오류 감지"
fi

# ── A3: VLAN 이 숫자가 아님 ──────────────────────────────────────────────
echo "host01 PG_X notanumber" >"$tmp/bad2.txt"
if ( preprocess_vswitch "$tmp/bad2.txt" "$tmp/bad2.out" >/dev/null 2>&1 ); then
  echo "  FAIL A3(VLAN) — 오류가 나야 하는데 통과함"; fail=1
else
  echo "  ok  A3(VLAN 비숫자) 오류 감지"
fi

echo
[ "$fail" -eq 0 ] && { echo "전체 통과"; exit 0; } || { echo "실패 있음"; exit 1; }
