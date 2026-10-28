#!/bin/bash
# update.sh - 회사 사본 config_check.sh 의 LDAP 추출이 `INFO<TAB>SLSI<TAB>VIP<TAB>1.1.1.1<TAB>(...)` 줄도 알아보게 고친다.
#   사용: bash update.sh <config_check.sh 경로>      (경로는 필수. 예: bash update.sh "/실사용경로/config_check.sh")
#   추가되는 줄은 최대 2줄이고 기존 줄은 하나도 바꾸지 않는다.
#     1) (필수) run_check 의 awk 에서 `nt = split(b, tk,` 뒤 첫 `for (j = 2; j < nt + 1; j++) {` 줄 바로 앞에
#        `if (s == "INFO" && toupper(tk[2]) == "SLSI" ...` 한 줄 (INFO SLSI [VIP] <IP> → .ldap 에 "SLSI VIP"/"SLSI")
#     2) (있으면) `Splunk/LDAP 은 키워드 바로 다음 토큰을 값으로 쓴다` 주석 아래에 설명 주석 한 줄. 그 주석이 없으면 건너뜀
#   줄 번호·diff/patch 에 기대지 않고 내용(공백·CR 을 뺀 줄)으로 위치를 찾으므로 벤더명 변경·들여쓰기(탭/공백)·빈 줄·후행 공백이
#   달라도 된다. CRLF 사본은 CRLF 그대로 두고 추가 줄에도 CR 을 붙인다 (나머지 바이트는 그대로).
#   안전장치: 이미 적용된 사본은 그대로 둔다 / 위치는 정확히 1곳이어야 함 / 적용 결과를 원본과 diff 해 삭제·변경된 줄이 없고
#   추가가 1~2줄인지 확인 / bash -n 문법 확인(CRLF 는 CR 을 뺀 복사본으로). 하나라도 실패하면 대상은 바뀌지 않는다.
#   적용 시 <대상>.bak.<YYYYmmddHHMMSS> 로 백업한다.
set -u
TARGET="${1:-}"
if [ -z "$TARGET" ]; then
    echo "사용: bash $0 <config_check.sh 경로>" >&2
    echo "  예: bash $0 \"/실사용경로/config_check.sh\"" >&2
    exit 1
fi
[ -f "$TARGET" ] || { echo "[X] 대상 파일이 없습니다: $TARGET" >&2
                      echo "사용: bash $0 <config_check.sh 경로>" >&2; exit 1; }

W=$(mktemp -d) || exit 1; trap 'rm -rf "$W"' EXIT
# awk 가 CR 을 건드리지 않게(일부 awk 는 CRLF 의 CR 을 떼어 버림) CR 을 \001 로 바꾼 사본으로 읽고, 출력은 다시 CR 로 되돌린다
tr '\r' '\001' < "$TARGET" > "$W/in"

# 공백·탭·CR 을 모두 뺀 줄 비교용 awk 함수 (벤더 사본의 들여쓰기·후행 공백·CRLF 차이를 무시)
STRIP='function st(s) { gsub(/[ \t\001]/, "", s); return s }'

# 이미 적용됐는지
if awk "$STRIP"'
    index(st($0), "toupper(tk[2])==\"SLSI\"") { found = 1; exit }
    END { exit !found }' "$W/in"; then
    echo "[O] 이미 적용되어 있습니다: $TARGET (변경 없음)"
    exit 0
fi

# 구조 확인: LDAP 키워드 규칙이 있어야 올바른 파일
if ! awk "$STRIP"'
    index(st($0), "k==\"ldap\"") { found = 1; exit }
    END { exit !found }' "$W/in"; then
    echo "[X] LDAP 키워드 규칙(k == \"ldap\")이 없어 config_check.sh 구조가 아닙니다 — $TARGET 은 변경하지 않았습니다" >&2
    exit 1
fi

# 앵커 개수: `nt = split(b, tk,` 뒤에 나오는 `for (j = 2; j < nt + 1; j++) {` 줄
n=$(awk "$STRIP"'
    { s = st($0) }
    index(s, "nt=split(b,tk,") { seen = 1 }
    seen && s ~ /^for\(j=2;j<nt\+1;j\+\+\)\{/ { c++ }
    END { print c + 0 }' "$W/in")
if [ "$n" != 1 ]; then
    echo "[X] 넣을 위치(nt = split(b, tk, 뒤의 for (j = 2; j < nt + 1; j++) { 줄)를 ${n}개 찾았습니다 (1개여야 함) — $TARGET 은 변경하지 않았습니다" >&2
    exit 1
fi

# 추가할 줄 (환경변수로 넘김: awk -v 는 백슬래시를 먼저 해석해 \t 가 바뀜)
CODE='if (s == "INFO" && toupper(tk[2]) == "SLSI" && !(h in lv)) { lv[h] = 1; print h "\t" (toupper(tk[3]) == "VIP" ? "SLSI VIP" : "SLSI") > (P ".ldap") }   # LDAP: INFO SLSI [VIP] <IP> ...'
CMT='# LDAP 은 "INFO SLSI [VIP] <IP> ..." 줄도 값으로 쓴다 (VIP 가 있으면 "SLSI VIP", 없으면 "SLSI").'

CODE="$CODE" CMT="$CMT" awk "$STRIP"'
    function ind(raw,   m) { match(raw, /^[ \t]*/); return substr(raw, 1, RLENGTH) }
    function cr(raw) { return (raw ~ /\001$/) ? "\001" : "" }
    {
        s = st($0)
        if (!cdone && index($0, "Splunk/LDAP 은 키워드 바로 다음 토큰을 값으로 쓴다")) {
            print
            printf "%s%s%s\n", ind($0), ENVIRON["CMT"], cr($0)
            cdone = 1; next
        }
        if (index(s, "nt=split(b,tk,")) seen = 1
        if (seen && !done && s ~ /^for\(j=2;j<nt\+1;j\+\+\)\{/) {
            printf "%s%s%s\n", ind($0), ENVIRON["CODE"], cr($0)
            done = 1
        }
        print
    }
    END { exit (done ? (cdone ? 0 : 4) : 3) }' "$W/in" > "$W/new0"
rc=$?
tr '\001' '\r' < "$W/new0" > "$W/new"
if [ "$rc" = 4 ]; then
    echo "[!] 설명 주석 위치(Splunk/LDAP 은 키워드 바로 다음 토큰을 값으로 쓴다)를 찾지 못해 주석 줄은 건너뜁니다 (코드 줄만 적용)"
elif [ "$rc" != 0 ]; then
    echo "[X] 적용 중 오류(코드 ${rc}) — $TARGET 은 변경하지 않았습니다" >&2
    exit 1
fi

# 삭제·변경된 줄이 없고 추가가 1~2줄인지
d=$(diff "$TARGET" "$W/new")
removed=$(printf '%s\n' "$d" | grep -c '^<')
added=$(printf '%s\n' "$d" | grep -c '^>')
if [ "$removed" != 0 ] || [ "$added" -lt 1 ] || [ "$added" -gt 2 ]; then
    echo "[X] 삭제 ${removed}줄 / 추가 ${added}줄 — 추가만 1~2줄이어야 해서 중단했습니다. $TARGET 은 변경하지 않았습니다" >&2
    exit 1
fi

# 문법 확인 (CR 이 있으면 bash 가 못 읽으므로 CR 을 뺀 복사본으로)
tr -d '\r' < "$W/new" > "$W/new.lf"
bash -n "$W/new.lf" || { echo "[X] 적용 결과 문법 오류 — $TARGET 은 변경하지 않았습니다" >&2; exit 1; }

BAK="$TARGET.bak.$(date +%Y%m%d%H%M%S)"
cp -p "$TARGET" "$BAK" || { echo "[X] 백업 실패 — $TARGET 은 변경하지 않았습니다" >&2; exit 1; }
cat "$W/new" > "$TARGET"       # cat > 로 덮어써서 권한·소유자 유지
echo "[O] 적용 완료: $TARGET (백업: $BAK)"
echo "    추가된 줄 ${added}개 (삭제·변경 0):"
printf '%s\n' "$d" | grep '^>' | tr -d '\r' | sed 's/^> /    + /'
