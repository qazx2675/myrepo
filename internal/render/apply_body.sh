###############################################################################
# 계정 수동 마운트 설정 적용 본체.
# (Go 엔진이 앞에 lib_common.sh 를 붙이고, heredoc 자리에 계정 목록을 넣습니다)
#
# 적용 대상 4개 파일:
#   1. /etc/auto.master
#      - `/user /etc/auto.home` 항목이 이미 있으면(앞뒤·중간 공백/탭 무관) 제외
#      - 없으면 가장 아랫줄에 `/user /etc/auto.home` 추가
#   2. /etc/passwd
#      - `계정:x:UID:GID:r계정:/user/계정:/bin/csh`
#      - 기존 동일 계정명이 있으면 해당 줄을 새 정보로 갱신, 없으면 맨 아래에 추가
#   3. /etc/group
#      - `계정:x:UID:`
#      - 기존 동일 그룹명(계정명)이 있으면 해당 줄을 새 정보로 갱신, 없으면 맨 아래에 추가
#   4. /etc/auto.home
#      - `계정 -rw,hard,actimeo=1,proto=tcp,vers=3 스토리지/계정`
#      - 기존 동일 계정 키가 있으면 해당 줄을 새 정보로 갱신, 없으면 맨 아래에 추가
#
# 파일 내용이 실제로 바뀔 때만 `<파일>.bak.<STAMP>` 백업을 생성하고 교체합니다.
# `/etc/auto.master` 또는 `/etc/auto.home` 이 바뀐 경우에만 autofs 를 리로드/재시작합니다.
###############################################################################

STAMP="$(date +%Y%m%d%H%M%S)"

#------------------------------------------------------------------------------
# 공통 : 파일 커밋 (내용이 다를 때만 .bak.<STAMP> 백업 후 교체)
#
# $1 = 논리 경로 (/etc/...)  $2 = 새 내용이 들어있는 임시파일  $3 = 새로 생성 시 기본 권한(기본 0644)
#------------------------------------------------------------------------------

commit_file()
{
    local logical="$1"
    local tmp="$2"
    local mode="${3:-0644}"
    local real="$ROOT$logical"

    # 안전장치: 원본에 내용이 있었는데 결과가 비었다면 편집 오류이므로 반영하지 않음
    if [ -s "$real" ] && [ ! -s "$tmp" ]; then
        rm -f "$tmp"
        fail "생성 결과가 비어 있어 반영하지 않았습니다: $logical"
        return 1
    fi

    if [ -f "$real" ] && cmp -s "$real" "$tmp"; then
        rm -f "$tmp"
        log SAME "$logical"
        return 0
    fi

    if [ "$DRYRUN" = "1" ]; then
        log WOULD-CHANGE "$logical"
        if [ -f "$real" ]; then
            diff -u "$real" "$tmp" 2>/dev/null | sed 's/^/DIFF|/' | head -40
        else
            sed 's/^/DIFF|+/' "$tmp" | head -40
        fi
        rm -f "$tmp"
        mark_changed "$logical"
        return 0
    fi

    mkdir -p "$(dirname "$real")" || { fail "디렉터리 생성 실패: $logical"; rm -f "$tmp"; return 1; }

    # 백업은 한 번의 실행에서 파일당 정확히 한 번만 생성
    if [ -f "$real" ] && [ ! -f "$real.bak.$STAMP" ]; then
        cp -p "$real" "$real.bak.$STAMP" || { fail "백업 실패: $logical"; rm -f "$tmp"; return 1; }
    fi

    if [ -f "$real" ]; then
        cat "$tmp" > "$real" || { fail "쓰기 실패: $logical"; rm -f "$tmp"; return 1; }
        rm -f "$tmp"
    else
        mv "$tmp" "$real" || { fail "쓰기 실패: $logical"; return 1; }
        chmod "$mode" "$real" 2>/dev/null || true
    fi

    mark_changed "$logical"
    log CHANGED "$logical"
    return 0
}

#------------------------------------------------------------------------------
# 1. /etc/auto.master
#    `/user /etc/auto.home` 이 공백/탭과 무관하게 이미 존재하는지 확인하고,
#    없으면 가장 아랫줄에 추가합니다.
#------------------------------------------------------------------------------

apply_auto_master()
{
    local logical="/etc/auto.master"
    local real="$ROOT$logical"
    local tmp; tmp="$(mktemp)"

    if [ -f "$real" ]; then
        awk '
            BEGIN { found = 0 }
            {
                print $0
                line = $0
                sub(/^[ \t]+/, "", line)
                if (line !~ /^#/ && line != "") {
                    n = split(line, f, /[ \t]+/)
                    if (n >= 2 && f[1] == "/user" && f[2] == "/etc/auto.home") {
                        found = 1
                    }
                }
            }
            END {
                if (!found) {
                    print "/user /etc/auto.home"
                }
            }
        ' "$real" > "$tmp"
    else
        printf '/user /etc/auto.home\n' > "$tmp"
    fi

    commit_file "$logical" "$tmp" 0644
}

#------------------------------------------------------------------------------
# 2. 콜론(:) 구분 파일(/etc/passwd, /etc/group)에서 첫 필드(계정명) 기준 갱신 또는 추가
#    $1 = 현재 작업 임시파일  $2 = 계정명  $3 = 기록할 전체 줄
#------------------------------------------------------------------------------

upsert_colon_entry()
{
    local file="$1" key="$2" newline="$3"
    local next_tmp; next_tmp="$(mktemp)"

    if [ -f "$file" ]; then
        AWK_K="$key" AWK_LINE="$newline" awk '
            BEGIN { k = ENVIRON["AWK_K"]; nl = ENVIRON["AWK_LINE"]; done = 0 }
            {
                idx = index($0, ":")
                if (idx > 0 && substr($0, 1, idx - 1) == k) {
                    if (!done) {
                        print nl
                        done = 1
                    }
                    next
                }
                print $0
            }
            END {
                if (!done) {
                    print nl
                }
            }
        ' "$file" > "$next_tmp"
    else
        printf '%s\n' "$newline" > "$next_tmp"
    fi

    mv -f "$next_tmp" "$file"
}

#------------------------------------------------------------------------------
# 3. 공백/탭 구분 파일(/etc/auto.home)에서 첫 필드(계정명) 기준 갱신 또는 추가
#    $1 = 현재 작업 임시파일  $2 = 계정명  $3 = 기록할 전체 줄
#------------------------------------------------------------------------------

upsert_autohome_entry()
{
    local file="$1" key="$2" newline="$3"
    local next_tmp; next_tmp="$(mktemp)"

    if [ -f "$file" ]; then
        AWK_K="$key" AWK_LINE="$newline" awk '
            BEGIN { k = ENVIRON["AWK_K"]; nl = ENVIRON["AWK_LINE"]; done = 0 }
            {
                line = $0
                sub(/^[ \t]+/, "", line)
                if (line !~ /^#/ && line != "") {
                    n = split(line, f, /[ \t]+/)
                    if (n >= 1 && f[1] == k) {
                        if (!done) {
                            print nl
                            done = 1
                        }
                        next
                    }
                }
                print $0
            }
            END {
                if (!done) {
                    print nl
                }
            }
        ' "$file" > "$next_tmp"
    else
        printf '%s\n' "$newline" > "$next_tmp"
    fi

    mv -f "$next_tmp" "$file"
}

###############################################################################
# 실행 본체
###############################################################################

apply_auto_master

PASSWD_TMP="$(mktemp)"
GROUP_TMP="$(mktemp)"
AUTOHOME_TMP="$(mktemp)"

if [ -f "$ROOT/etc/passwd" ]; then
    cp -f "$ROOT/etc/passwd" "$PASSWD_TMP"
else
    : > "$PASSWD_TMP"
fi

if [ -f "$ROOT/etc/group" ]; then
    cp -f "$ROOT/etc/group" "$GROUP_TMP"
else
    : > "$GROUP_TMP"
fi

if [ -f "$ROOT/etc/auto.home" ]; then
    cp -f "$ROOT/etc/auto.home" "$AUTOHOME_TMP"
else
    : > "$AUTOHOME_TMP"
fi

ACCOUNT_COUNT=0
while read -r acc_user acc_id acc_storage; do
    [ -z "$acc_user" ] && continue
    ACCOUNT_COUNT=$((ACCOUNT_COUNT + 1))

    pw_line="${acc_user}:x:${acc_id}:${acc_id}:r${acc_user}:/user/${acc_user}:/bin/csh"
    gr_line="${acc_user}:x:${acc_id}:"
    ah_line="${acc_user} -rw,hard,actimeo=1,proto=tcp,vers=3 ${acc_storage}/${acc_user}"

    upsert_colon_entry    "$PASSWD_TMP"   "$acc_user" "$pw_line"
    upsert_colon_entry    "$GROUP_TMP"    "$acc_user" "$gr_line"
    upsert_autohome_entry "$AUTOHOME_TMP" "$acc_user" "$ah_line"
done <<'ACCOUNTS_EOF'
__ACCOUNTS_DATA__
ACCOUNTS_EOF

commit_file /etc/passwd    "$PASSWD_TMP"   0644
commit_file /etc/group     "$GROUP_TMP"    0644
commit_file /etc/auto.home "$AUTOHOME_TMP" 0644

###############################################################################
# 검증 (DRYRUN 이 아닐 때 실제 반영 여부 확인)
###############################################################################

if [ "$DRYRUN" != "1" ] && [ "$FAILED" = "0" ]; then
    if ! awk '
        {
            line = $0; sub(/^[ \t]+/, "", line)
            if (line !~ /^#/ && line != "") {
                n = split(line, f, /[ \t]+/)
                if (n >= 2 && f[1] == "/user" && f[2] == "/etc/auto.home") found = 1
            }
        }
        END { exit(found ? 0 : 1) }
    ' "$ROOT/etc/auto.master" 2>/dev/null; then
        fail "/etc/auto.master 에 /user /etc/auto.home 검증 실패"
    fi

    while read -r acc_user acc_id acc_storage; do
        [ -z "$acc_user" ] && continue
        pw_line="${acc_user}:x:${acc_id}:${acc_id}:r${acc_user}:/user/${acc_user}:/bin/csh"
        gr_line="${acc_user}:x:${acc_id}:"
        ah_line="${acc_user} -rw,hard,actimeo=1,proto=tcp,vers=3 ${acc_storage}/${acc_user}"

        grep -Fqx "$pw_line" "$ROOT/etc/passwd"    2>/dev/null || fail "/etc/passwd 계정($acc_user) 검증 실패"
        grep -Fqx "$gr_line" "$ROOT/etc/group"     2>/dev/null || fail "/etc/group 계정($acc_user) 검증 실패"
        grep -Fqx "$ah_line" "$ROOT/etc/auto.home" 2>/dev/null || fail "/etc/auto.home 계정($acc_user) 검증 실패"
    done <<'VERIFY_EOF'
__ACCOUNTS_DATA__
VERIFY_EOF
fi

###############################################################################
# autofs 서비스 리로드/재시작 (변경이 있을 때만)
###############################################################################

if [ "$FAILED" = "0" ]; then
    reload_or_restart_autofs
fi

###############################################################################
# 결과 한 줄 요약
###############################################################################

if [ "$FAILED" = "1" ]; then
    echo "RESULT|$NODE_HOST|FAIL|계정 설정 적용 중 오류 발생"
    exit 1
fi

if [ -z "$CHANGED" ]; then
    echo "RESULT|$NODE_HOST|NOCHANGE|accounts=${ACCOUNT_COUNT}"
else
    echo "RESULT|$NODE_HOST|OK|accounts=${ACCOUNT_COUNT}|changed:$(echo $CHANGED | tr ' ' ',')"
fi
exit 0
