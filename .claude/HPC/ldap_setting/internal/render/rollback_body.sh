###############################################################################
# 롤백 본체. (Go 엔진이 위 헤더에 MODE/STAMP 를, 앞에 lib_common.sh 를 붙입니다)
#
# 적용 시 남긴 <파일>.bak.<STAMP> 를 되돌립니다.
# 한 번의 적용 실행이 남긴 백업은 전부 같은 STAMP 를 쓰므로, STAMP 하나가
# "그 적용 이전 상태" 를 정확히 가리킵니다.
#
# MODE
#   list    이 노드에 남아 있는 백업 시점 목록만 출력. 아무것도 바꾸지 않음
#   latest  가장 최근 시점으로 되돌림
#   stamp   STAMP 로 지정한 시점으로 되돌림
#
# 되돌린 파일에 대응하는 서비스만 재시작합니다.
#
# 주의: 적용이 '새로 만든' 파일(원래 없던 파일)은 백업이 없어 되돌릴 수 없습니다.
#       삭제는 위험하므로 하지 않고 NO-BACKUP 으로 보고만 합니다.
###############################################################################

# 되돌림 대상 후보. nslcd/sssd 와 ntp/chrony 는 둘 다 후보에 넣고,
# 실제로 백업이 있는 것만 처리합니다. (그 노드가 어느 쪽을 쓰는지와 무관하게 정확)
TARGETS="/etc/openldap/ldap.conf
/etc/autofs_ldap_auth.conf
/etc/autofs.conf
/etc/nslcd.conf
/etc/sssd/sssd.conf
/etc/resolv.conf
/etc/ntp.conf
/etc/chrony.conf
/etc/auto.appl"

#------------------------------------------------------------------------------
# 이 노드에 남아 있는 백업 시점(STAMP) 전체를 새 것부터 나열
#------------------------------------------------------------------------------

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

#------------------------------------------------------------------------------
# MODE=list : 시점별로 어떤 파일이 백업되어 있는지 보여주기만 함
#------------------------------------------------------------------------------

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

#------------------------------------------------------------------------------
# 되돌릴 시점 결정
#------------------------------------------------------------------------------

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

#------------------------------------------------------------------------------
# 복원
#
# 되돌리기 자체는 새 백업을 만들지 않습니다.
# 같은 시점으로 두 번 되돌리면 두 번째는 SAME 이 되어 아무것도 하지 않습니다.
#------------------------------------------------------------------------------

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
        # 원본 퍼미션/소유자를 유지하고 내용만 되돌립니다.
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

# 적용은 했지만 백업이 없어 되돌릴 수 없는 파일을 알려줍니다.
# (적용이 새로 만들어낸 파일. 삭제는 하지 않습니다)
while IFS= read -r t; do
    [ -z "$t" ] && continue
    if [ -f "$ROOT$t" ] && [ ! -f "$ROOT$t.bak.$STAMP" ]; then
        case "$t" in
            /etc/autofs_ldap_auth.conf|/etc/auto.appl|/etc/nslcd.conf)
                log NO-BACKUP "$t (이 시점에 백업 없음. 적용이 새로 만든 파일일 수 있어 삭제하지 않습니다)"
                ;;
        esac
    fi
done <<TARGETS_EOF2
$TARGETS
TARGETS_EOF2

###############################################################################
# 되돌린 파일에 대응하는 서비스만 재시작 (표는 lib_common.sh 에 있습니다)
###############################################################################

restart_for_changed

###############################################################################
# 결과 한 줄 요약
###############################################################################

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
