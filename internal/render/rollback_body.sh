###############################################################################
# 롤백 본체. (Go 엔진이 위 헤더에 MODE/STAMP 를, 앞에 lib_common.sh 를 붙입니다)
#
# 적용 시 남긴 <파일>.bak.<STAMP> 를 되돌립니다.
#
# MODE
#   list    이 노드에 남아 있는 백업 시점 목록만 출력. 아무것도 바꾸지 않음
#   latest  가장 최근 시점으로 되돌림
#   stamp   STAMP 로 지정한 시점으로 되돌림
###############################################################################

TARGETS="/etc/auto.master
/etc/passwd
/etc/group
/etc/auto.home"

all_stamps()
{
    local logical real f
    printf '%s\n' "$TARGETS" | while IFS= read -r logical; do
        [ -z "$logical" ] && continue
        real="$ROOT$logical"
        for f in "$real".bak.*; do
            [ -f "$f" ] || continue
            echo "${f##*.bak.}"
        done
    done | sort -ru
}

if [ "$MODE" = "list" ]; then
    stamps="$(all_stamps)"
    if [ -z "$stamps" ]; then
        echo "RESULT|$NODE_HOST|NOBACKUP"
        exit 0
    fi
    while IFS= read -r s; do
        [ -z "$s" ] && continue
        files=""
        while IFS= read -r logical; do
            [ -z "$logical" ] && continue
            [ -f "$ROOT$logical.bak.$s" ] && files="$files,$logical"
        done <<LIST_TARGETS
$TARGETS
LIST_TARGETS
        echo "BACKUP|$s|${files#,}"
    done <<LIST_STAMPS
$stamps
LIST_STAMPS
    echo "RESULT|$NODE_HOST|LISTED"
    exit 0
fi

if [ "$MODE" = "latest" ]; then
    STAMP="$(all_stamps | head -n1)"
    if [ -z "$STAMP" ]; then
        echo "RESULT|$NODE_HOST|NOBACKUP"
        exit 0
    fi
fi

if [ -z "$STAMP" ]; then
    fail "되돌릴 시점(STAMP)이 지정되지 않았습니다"
    echo "RESULT|$NODE_HOST|FAIL|rollback"
    exit 1
fi

log INFO "host=$NODE_HOST mode=$MODE stamp=$STAMP"

FOUND=0

restore_one()
{
    local logical="$1"
    local real="$ROOT$logical"
    local bak="$real.bak.$STAMP"

    if [ ! -f "$bak" ]; then
        return 0
    fi
    FOUND=$((FOUND+1))

    if [ -f "$real" ] && cmp -s "$real" "$bak"; then
        log SAME "$logical"
        return 0
    fi

    if [ "$DRYRUN" = "1" ]; then
        log WOULD-RESTORE "$logical"
        diff -u "$real" "$bak" 2>/dev/null | sed 's/^/DIFF|/' | head -40
        mark_changed "$logical"
        return 0
    fi

    if [ -f "$real" ]; then
        if ! cat "$bak" > "$real"; then
            fail "복원 실패: $logical"
            return 1
        fi
    else
        if ! cp -p "$bak" "$real"; then
            fail "복원 실패: $logical"
            return 1
        fi
    fi

    mark_changed "$logical"
    log RESTORED "$logical"
    return 0
}

while IFS= read -r t; do
    [ -z "$t" ] && continue
    restore_one "$t"
done <<TARGETS_EOF
$TARGETS
TARGETS_EOF

if [ "$FOUND" = "0" ]; then
    echo "RESULT|$NODE_HOST|NOBACKUP|$STAMP"
    exit 0
fi

if [ "$FAILED" = "0" ]; then
    reload_or_restart_autofs
fi

if [ "$FAILED" = "1" ]; then
    echo "RESULT|$NODE_HOST|FAIL|$STAMP"
    exit 1
fi

if [ -z "$CHANGED" ]; then
    echo "RESULT|$NODE_HOST|NOCHANGE|$STAMP"
else
    echo "RESULT|$NODE_HOST|OK|$STAMP|restored:$(echo $CHANGED | tr ' ' ',')"
fi
exit 0
