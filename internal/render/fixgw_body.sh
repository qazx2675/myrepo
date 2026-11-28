###############################################################################
# 예전 게이트웨이 고침 본체 (수정계획서 4.8 3단계). Go 엔진(render.go FixGatewayScript)이
# 위 헤더 다음에 이 본체를 붙이고 GW_MAP 표 자리에 아래 표를 채웁니다.
#   <host> <예전게이트웨이> <새게이트웨이> <파일1>,<파일2> <STAMP>   (파일은 노드의 절대 경로, STAMP 는 apply 결과 비고의 BACKUP=<STAMP>)
#
# 동작
#   1. hostname 으로 표에서 이 노드의 줄을 찾음
#   2. 목록의 파일마다 종류를 점검. 아래 두 종류 외에는 건드리지 않고 FAIL (아무것도 쓰기 전에 거절)
#        - /etc/sysconfig/network          : GATEWAY=<예전값> 줄의 값만 고침
#        - .../route-<장치> (백업류 제외)   : 공백으로 나뉜 토큰이 예전 게이트웨이와 같거나
#                                           GATEWAYn=<예전값> 이면 새 값으로 고침
#   3. 바꿀 내용이 있는 파일만 apply 때와 같은 /var/lib/ip_change/backup/<STAMP>/<원래 전체 경로> 에 백업하고
#      같은 디렉토리의 manifest 에 "modified <경로>" 를 덧붙임. 이미 백업본이 있거나 manifest 에
#      같은 경로가 있으면(apply 가 만든 파일 포함) 덮어쓰지 않음 — 그래서 -rollback 한 번에
#      IP 변경과 게이트웨이 고침이 함께 되돌아감. STAMP 디렉토리가 없으면 FAIL
#   4. 고친 뒤 다시 읽어 예전 값이 없고 새 값이 있는지 확인. 틀리면 백업으로 되돌리고 FAIL
#   네트워크 서비스 재시작, nmcli 쓰기 명령은 하지 않습니다.
#
# 출력 (한 줄, gossh 가 "<host>: " 를 앞에 붙임. 필드 값에는 "|" 가 들어가지 않음)
#   성공: RESULT|OK|<hostname>|<고친 파일들(쉼표)>|<STAMP>
#         고칠 내용이 없으면 파일과 STAMP 가 빈 칸 (백업도 만들지 않음)
#   실패: RESULT|FAIL|<hostname>|<사유>
#
# 테스트: ROOT 환경변수(운영에서는 비어 있음)를 노드의 모든 파일 경로 앞에 붙입니다.
###############################################################################

STAMP=""
NODE_HOST="$(hostname -s 2>/dev/null || hostname)"
NODE_FQDN="$(hostname 2>/dev/null || echo "$NODE_HOST")"

ROOT="${ROOT:-}"
BACKUP_BASE="$ROOT/var/lib/ip_change/backup"
BK_DIR=""

OLD_GW=""; NEW_GW=""; FILES=""; FOUND=0
CHANGED_LIST=""
TMPF=""
PRE_DIR=""     # 이 실행이 고치기 전 내용을 따로 보관하는 임시 디렉토리 (실패 시 되돌림용)
trap '[ -z "$TMPF" ] || rm -f "$TMPF"; [ -z "$PRE_DIR" ] || rm -rf "$PRE_DIR"' EXIT

# 결과 필드 값에서 구분자 "|" 와 줄바꿈을 없앱니다
field() { printf '%s' "$1" | tr '|\r\n\t' '/   '; }

# 이번 실행이 고친 파일만 고치기 전 내용으로 되돌립니다 (apply 가 남긴 백업·manifest 는 건드리지 않음)
undo_all()
{
    local p
    [ -n "$PRE_DIR" ] && [ -f "$PRE_DIR/list" ] || return 0
    while read -r p; do
        [ -n "$p" ] && cp -p "$PRE_DIR$p" "$ROOT$p" 2>/dev/null
    done < "$PRE_DIR/list"
    return 0
}

fail_out()
{
    echo "RESULT|FAIL|$(field "$NODE_HOST")|$(field "$1")"
    exit 1
}

fail_undo()
{
    undo_all
    fail_out "$1 (백업으로 되돌림)"
}

is_ipv4()
{
    case "$1" in ''|*[!0-9.]*|.*|*.|*..*) return 1 ;; esac
    [ "$(printf '%s' "$1" | tr -cd '.' | wc -c)" -eq 3 ]
}

#------------------------------------------------------------------------------
# 표에서 이 노드의 줄 찾기
#------------------------------------------------------------------------------

while read -r h o n fl st; do
    [ -n "$h" ] || continue
    case "$h" in \#*) continue ;; esac
    if [ "$h" = "$NODE_HOST" ] || [ "$h" = "$NODE_FQDN" ]; then
        OLD_GW="$o"; NEW_GW="$n"; FILES="$fl"; STAMP="$st"; FOUND=1
        break
    fi
done <<'GWMAPEOF'
__GW_MAP__
GWMAPEOF

[ "$FOUND" = 1 ] || fail_out "대상 목록에 이 hostname($NODE_HOST)이 없습니다"
is_ipv4 "$OLD_GW" || fail_out "예전 게이트웨이 값이 올바르지 않습니다: $OLD_GW"
is_ipv4 "$NEW_GW" || fail_out "새 게이트웨이 값이 올바르지 않습니다: $NEW_GW"
[ -n "$FILES" ] || fail_out "고칠 파일 목록이 비어 있습니다"
case "$STAMP" in ''|*[!0-9]*) fail_out "백업 STAMP 가 올바르지 않습니다(숫자여야 함): $STAMP" ;; esac
BK_DIR="$BACKUP_BASE/$STAMP"
[ -d "$BK_DIR" ] || fail_out "apply 백업 STAMP 디렉토리가 없습니다: $STAMP"
[ ! -f "$BK_DIR/ROLLED_BACK" ] || fail_out "이미 롤백된 STAMP 입니다: $STAMP"

#------------------------------------------------------------------------------
# 파일 종류 점검 (쓰기 전에 모두 점검)
#------------------------------------------------------------------------------

# 종류: network / route. 허용하지 않으면 실패 코드
file_kind()
{
    local p="$1" b
    case "$p" in /*) ;; *) return 1 ;; esac
    case "$p" in *..*|*[[:space:]]*) return 1 ;; esac
    if [ "$p" = /etc/sysconfig/network ]; then echo network; return 0; fi
    b="${p##*/}"
    case "$b" in
        route-*.bak*|route-*~|route-*.orig|route-*.rpm*|route-*.old|route-*.swp) return 1 ;;
        route-?*)
            case "${b#route-}" in *[!A-Za-z0-9_.:-]*) return 1 ;; esac
            echo route; return 0 ;;
    esac
    return 1
}

OLDIFS="$IFS"; IFS=','
set -- $FILES
IFS="$OLDIFS"
for p in "$@"; do
    [ -n "$p" ] || continue
    file_kind "$p" >/dev/null || fail_out "고칠 수 없는 파일입니다(허용: /etc/sysconfig/network, route-<장치>): $p"
    [ -f "$ROOT$p" ] || fail_out "파일이 없습니다: $p"
    [ ! -L "$ROOT$p" ] || fail_out "심볼릭 링크는 고치지 않습니다: $p"
done

#------------------------------------------------------------------------------
# awk: mode=fix 이면 고친 내용을 출력, mode=count 이면 "예전값개수 새값개수" 를 출력
#   network : GATEWAY=<값> 줄의 값이 old 와 같을 때만
#   route   : 토큰이 old 와 같거나, GATEWAYn=<값> 의 값이 old 와 같을 때
#   주석 줄(# 로 시작)은 건드리지 않음. 따옴표와 줄 끝 CR 은 그대로 둠
#------------------------------------------------------------------------------

AWK_PROG='
function unq(v,   f, e) {
    f = substr(v, 1, 1); e = substr(v, length(v), 1)
    if (length(v) >= 2 && f == e && (f == "\"" || f == "\047")) return substr(v, 2, length(v) - 2)
    return v
}
function strip(v) { sub(/^[ \t\r]+/, "", v); sub(/[ \t\r]+$/, "", v); return v }
# 값 문자열 v 가 (따옴표 제외) old 와 같으면 old 자리만 new 로 바꾼 값을, 아니면 v 를 돌려줌
function repval(v,   s, i) {
    s = unq(strip(v))
    if (s != old) { if (s == new) nnew++; return v }
    nold++
    if (mode == "count") return v
    i = index(v, old)
    return substr(v, 1, i - 1) new substr(v, i + length(old))
}
{
    line = $0
    t = line; sub(/^[ \t\r]+/, "", t)
    if (t == "" || substr(t, 1, 1) == "#") { if (mode == "fix") print line; next }
    if (kind == "network") {
        p = index(line, "=")
        if (p > 1) {
            k = strip(substr(line, 1, p - 1))
            if (k == "GATEWAY") line = substr(line, 1, p) repval(substr(line, p + 1))
        }
        if (mode == "fix") print line
        next
    }
    out = ""; rest = line
    while (rest != "") {
        if (match(rest, /^[ \t\r]+/)) { out = out substr(rest, 1, RLENGTH); rest = substr(rest, RLENGTH + 1); continue }
        match(rest, /^[^ \t\r]+/)
        tok = substr(rest, 1, RLENGTH); rest = substr(rest, RLENGTH + 1)
        if (tok == old) { nold++; if (mode == "fix") tok = new }
        else if (tok == new) nnew++
        else if (tok ~ /^GATEWAY[0-9]*=/) {
            p = index(tok, "=")
            tok = substr(tok, 1, p) repval(substr(tok, p + 1))
        }
        out = out tok
    }
    if (mode == "fix") print out
}
END { if (mode == "count") print nold + 0, nnew + 0 }
'

run_awk()   # $1=mode $2=kind $3=file
{
    awk -v mode="$1" -v kind="$2" -v old="$OLD_GW" -v new="$NEW_GW" "$AWK_PROG" "$3"
}

#------------------------------------------------------------------------------
# 백업 + 고침 + 검증
#------------------------------------------------------------------------------

PRE_DIR="$(mktemp -d)" || fail_out "임시 디렉토리 생성 실패"

for p in "$@"; do
    [ -n "$p" ] || continue
    kind="$(file_kind "$p")"
    cnt="$(run_awk count "$kind" "$ROOT$p")" || fail_undo "파일을 읽지 못했습니다: $p"
    n_old="${cnt%% *}"
    [ "$n_old" -gt 0 ] 2>/dev/null || continue     # 고칠 내용이 없으면 건드리지 않음

    # 되돌림용 사본 (실패 시 이 실행이 고친 파일만 복원)
    mkdir -p "$PRE_DIR$(dirname "$p")" || fail_undo "임시 디렉토리 생성 실패"
    cp -p "$ROOT$p" "$PRE_DIR$p" || fail_undo "임시 사본 실패: $p"
    echo "$p" >> "$PRE_DIR/list"

    # apply 의 STAMP 디렉토리에 백업 (이미 백업본이 있거나 apply 가 만든 파일이면 덮어쓰지 않음)
    if [ ! -f "$BK_DIR$p" ] && ! grep -qxF "created $p" "$BK_DIR/manifest" 2>/dev/null; then
        mkdir -p "$BK_DIR$(dirname "$p")" || fail_undo "백업 디렉토리 생성 실패: $BK_DIR"
        cp -p "$ROOT$p" "$BK_DIR$p" || fail_undo "백업 실패: $p"
        echo "modified $p" >> "$BK_DIR/manifest" || fail_undo "manifest 쓰기 실패"
    fi

    TMPF="$(mktemp)" || fail_undo "임시파일 생성 실패"
    run_awk fix "$kind" "$ROOT$p" > "$TMPF" || fail_undo "내용 만들기 실패: $p"
    [ -s "$TMPF" ] || fail_undo "빈 결과입니다: $p"
    cat "$TMPF" > "$ROOT$p" || fail_undo "쓰기 실패: $p"
    rm -f "$TMPF"; TMPF=""

    cnt="$(run_awk count "$kind" "$ROOT$p")" || fail_undo "다시 읽기 실패: $p"
    chk_old="${cnt%% *}"; chk_new="${cnt##* }"
    if [ "$chk_old" != 0 ] || [ "$chk_new" -lt "$n_old" ] 2>/dev/null; then
        fail_undo "검증 실패: $p (예전 값 $chk_old 개 남음, 새 값 $chk_new 개)"
    fi
    CHANGED_LIST="${CHANGED_LIST:+$CHANGED_LIST,}$p"
done

if [ -n "$CHANGED_LIST" ]; then
    echo "RESULT|OK|$(field "$NODE_HOST")|$(field "$CHANGED_LIST")|$STAMP"
else
    echo "RESULT|OK|$(field "$NODE_HOST")||"
fi
exit 0
