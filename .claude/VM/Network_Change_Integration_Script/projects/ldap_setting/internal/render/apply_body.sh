###############################################################################
# 설정 적용 본체. (Go 엔진이 위 헤더에 사이트별 값을, 앞에 lib_common.sh 를 붙입니다)
#
# 동작
#   1. 이 노드의 RHEL 메이저 버전과 hostname 으로 nslcd/sssd, ntp/chrony 를 결정
#   2. 대상 파일의 '해당 키만' 갱신 (전체 덮어쓰기 아님). 바뀌면 .bak.<STAMP> 백업
#   3. 실제로 내용이 바뀐 파일에 대응하는 서비스만 재시작
#
# 한 번의 실행이 남기는 백업은 전부 같은 STAMP 를 씁니다.
# rollback 은 이 STAMP 를 복원 단위로 사용합니다.
###############################################################################

STAMP="$(date +%Y%m%d%H%M%S)"


OS_MAJOR="$(detect_os_major)"
if [ -z "$OS_MAJOR" ]; then
    fail "OS 버전을 판정할 수 없습니다 (redhat-release / os-release 없음)"
    echo "RESULT|$NODE_HOST|FAIL|os-detect"
    exit 1
fi

# s4 접두사 예외
IS_S4=0
if [ "$S4_ENABLED" = "1" ] && [ -n "$S4_PREFIX" ]; then
    case "$NODE_HOST" in
        "$S4_PREFIX"*) IS_S4=1 ;;
    esac
fi

# 인증 백엔드 결정: RHEL 7 이하는 nslcd, 8 이상은 sssd. 단 s4 예외면 nslcd 강제.
if [ "$OS_MAJOR" -le 7 ]; then
    AUTH_BACKEND="nslcd"
elif [ "$IS_S4" = "1" ] && [ "$S4_NSLCD" = "1" ]; then
    AUTH_BACKEND="nslcd"
else
    AUTH_BACKEND="sssd"
fi

# 시각 동기화 결정: RHEL 7 이하는 ntp, 8 이상은 chrony. 단 s4 예외면 ntp 강제.
if [ "$OS_MAJOR" -le 7 ]; then
    TIME_BACKEND="ntp"
elif [ "$IS_S4" = "1" ] && [ "$S4_NTP" = "1" ]; then
    TIME_BACKEND="ntp"
else
    TIME_BACKEND="chrony"
fi

log INFO "host=$NODE_HOST os=$OS_MAJOR s4=$IS_S4 auth=$AUTH_BACKEND time=$TIME_BACKEND infra=$INFRA site=$SITE"

#------------------------------------------------------------------------------
# 공통 : 파일 커밋 (내용이 다를 때만 백업 후 교체)
#
# $1 = 논리 경로 (/etc/...)  $2 = 새 내용이 들어있는 임시파일
#------------------------------------------------------------------------------

commit_file()
{
    local logical="$1"
    local tmp="$2"
    local real="$ROOT$logical"

    # 안전장치: 원본에 내용이 있었는데 결과가 비었다면 편집 로직이 깨진 것이므로
    # 절대 반영하지 않습니다. (awk 오류 등으로 파일을 날려먹는 사고 방지)
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
        fi
        rm -f "$tmp"
        mark_changed "$logical"
        return 0
    fi

    mkdir -p "$(dirname "$real")" || { fail "디렉터리 생성 실패: $logical"; rm -f "$tmp"; return 1; }

    # ★ 백업은 이 실행에서 파일당 딱 한 번만 뜹니다.
    #   한 파일을 여러 번 고치기 때문에(ldap.conf 의 URI/BINDDN/BINDPW 등), 매번 덮어쓰면
    #   백업이 '이미 일부 수정된 상태' 가 되어 롤백해도 원본으로 돌아가지 않습니다.
    if [ -f "$real" ] && [ ! -f "$real.bak.$STAMP" ]; then
        cp -p "$real" "$real.bak.$STAMP" || { fail "백업 실패: $logical"; rm -f "$tmp"; return 1; }
    fi

    # 원본 퍼미션/소유자를 유지하고 내용만 바꿉니다.
    if [ -f "$real" ]; then
        cat "$tmp" > "$real" || { fail "쓰기 실패: $logical"; rm -f "$tmp"; return 1; }
        rm -f "$tmp"
    else
        mv "$tmp" "$real" || { fail "쓰기 실패: $logical"; return 1; }
    fi

    mark_changed "$logical"
    log CHANGED "$logical"
    return 0
}

#------------------------------------------------------------------------------
# 공통 : "KEY value" 형태 지시자 갱신 (ldap.conf, nslcd.conf)
#
# 해당 키로 시작하는 줄을 새 값으로 바꾸고, 없으면 파일 끝에 추가합니다.
# 다른 줄은 건드리지 않습니다.
#
# $1 = 논리 경로  $2 = 키  $3 = 값
#------------------------------------------------------------------------------

set_directive()
{
    local logical="$1" key="$2" val="$3"
    local real="$ROOT$logical"
    local tmp; tmp="$(mktemp)"

    if [ -f "$real" ]; then
        # 값에 백슬래시가 들어가도 깨지지 않도록 -v 대신 환경변수로 넘깁니다.
        # (awk 는 -v 값의 \n, \t, \[ 같은 이스케이프를 해석해버립니다)
        AWK_K="$key" AWK_V="$val" awk '
            BEGIN { k = ENVIRON["AWK_K"]; v = ENVIRON["AWK_V"]; done = 0 }
            {
                # 주석이 아니고 첫 필드가 키와 같으면 교체 (대소문자 무시)
                line = $0
                sub(/^[ \t]+/, "", line)
                first = line
                sub(/[ \t].*$/, "", first)
                if (tolower(first) == tolower(k) && line !~ /^#/) {
                    if (!done) { print k " " v; done = 1 }
                    next
                }
                print
            }
            END { if (!done) print k " " v }
        ' "$real" > "$tmp"
    else
        printf '%s %s\n' "$key" "$val" > "$tmp"
    fi

    commit_file "$logical" "$tmp"
}

#------------------------------------------------------------------------------
# 공통 : "key = value" 형태 갱신 (autofs.conf)
#
# $1 = 논리 경로  $2 = 키  $3 = 값(따옴표 포함해서 넘김)
#------------------------------------------------------------------------------

set_kv()
{
    local logical="$1" key="$2" val="$3"
    local real="$ROOT$logical"
    local tmp; tmp="$(mktemp)"

    if [ ! -f "$real" ]; then
        fail "파일이 없습니다: $logical"
        rm -f "$tmp"
        return 1
    fi

    AWK_K="$key" AWK_V="$val" awk '
        BEGIN { k = ENVIRON["AWK_K"]; v = ENVIRON["AWK_V"]; done = 0 }
        {
            line = $0
            sub(/^[ \t]+/, "", line)
            if (line ~ /^#/) { print; next }
            split(line, a, "=")
            lk = a[1]
            gsub(/[ \t]+$/, "", lk)
            if (lk == k) {
                if (!done) { print k " = " v; done = 1 }
                next
            }
            print
        }
        END { if (!done) print k " = " v }
    ' "$real" > "$tmp"

    commit_file "$logical" "$tmp"
}

#------------------------------------------------------------------------------
# 공통 : ini 섹션 안의 key 갱신 (sssd.conf 의 [domain/...])
#
# $1 = 논리 경로  $2 = 섹션 이름 접두사(정규식 아님, 예: "[domain/")  $3 = 키  $4 = 값
#------------------------------------------------------------------------------

set_ini_in_section()
{
    local logical="$1" secpfx="$2" key="$3" val="$4"
    local real="$ROOT$logical"
    local tmp; tmp="$(mktemp)"

    if [ ! -f "$real" ]; then
        fail "파일이 없습니다: $logical"
        rm -f "$tmp"
        return 1
    fi

    # 섹션 판정은 정규식이 아니라 문자열 접두사 비교로 합니다.
    # (정규식을 -v 로 넘기면 awk 가 \[ 를 [ 로 바꿔 문자클래스가 깨집니다)
    AWK_P="$secpfx" AWK_K="$key" AWK_V="$val" awk '
        BEGIN { p = ENVIRON["AWK_P"]; k = ENVIRON["AWK_K"]; v = ENVIRON["AWK_V"]
                inSec = 0; done = 0; found = 0 }
        {
            head = $0
            sub(/^[ \t]+/, "", head)
        }
        substr(head, 1, 1) == "[" {
            # 섹션을 벗어나는 시점에 키가 없었으면 여기서 추가
            if (inSec && !done) { print k " = " v; done = 1 }
            inSec = (substr(head, 1, length(p)) == p) ? 1 : 0
            if (inSec) found = 1
            print
            next
        }
        {
            if (inSec) {
                line = $0
                sub(/^[ \t]+/, "", line)
                if (line !~ /^[#;]/) {
                    split(line, a, "=")
                    lk = a[1]
                    gsub(/[ \t]+$/, "", lk)
                    if (lk == k) {
                        if (!done) { print k " = " v; done = 1 }
                        next
                    }
                }
            }
            print
        }
        END {
            if (inSec && !done) { print k " = " v; done = 1 }
            if (!found) { print "__NO_SECTION__" > "/dev/stderr" }
        }
    ' "$real" > "$tmp" 2>"$tmp.err"

    if grep -q __NO_SECTION__ "$tmp.err" 2>/dev/null; then
        rm -f "$tmp" "$tmp.err"
        fail "$logical 에 도메인 섹션이 없습니다 (sssd 미구성 노드로 판단)"
        return 1
    fi
    rm -f "$tmp.err"

    commit_file "$logical" "$tmp"
}

#------------------------------------------------------------------------------
# 공통 : 특정 지시자로 시작하는 줄 전체를 목록으로 교체
#        (resolv.conf 의 nameserver, ntp/chrony 의 server)
#
# $1 = 논리 경로  $2 = 지시자  $3 = 값 목록(줄바꿈 구분)  $4 = 값 뒤에 붙일 옵션
# $5 = (선택) 같이 걷어낼 다른 지시자들. 공백 구분.
#      예) chrony.conf 는 server 뿐 아니라 pool 줄도 NTP 소스이므로 함께 제거해야
#          체크 스크립트의 기준값 비교가 맞습니다.
#------------------------------------------------------------------------------

set_line_list()
{
    local logical="$1" key="$2" list="$3" opts="$4" also="${5:-}"
    local real="$ROOT$logical"
    local tmp; tmp="$(mktemp)"
    local first_seen=0 line stripped k hit

    emit_list()
    {
        local item
        printf '%s\n' "$list" | while IFS= read -r item; do
            [ -z "$item" ] && continue
            if [ -n "$opts" ]; then
                printf '%s %s %s\n' "$key" "$item" "$opts"
            else
                printf '%s %s\n' "$key" "$item"
            fi
        done
    }

    if [ -f "$real" ]; then
        # 기존 지시자 줄은 모두 걷어내되, 첫 번째가 있던 자리에 새 목록을 넣습니다.
        while IFS= read -r line || [ -n "$line" ]; do
            stripped="${line#"${line%%[![:space:]]*}"}"
            hit=0
            for k in $key $also; do
                case "$stripped" in
                    "$k "*|"$k	"*) hit=1; break ;;
                esac
            done
            if [ "$hit" = "1" ]; then
                if [ "$first_seen" = "0" ]; then
                    first_seen=1
                    emit_list
                fi
            else
                printf '%s\n' "$line"
            fi
        done < "$real" > "$tmp"
    fi

    if [ "$first_seen" = "0" ]; then
        emit_list >> "$tmp"
    fi

    commit_file "$logical" "$tmp"
}

###############################################################################
# 1. /etc/openldap/ldap.conf
###############################################################################

set_directive /etc/openldap/ldap.conf URI     "$URI_LINE"
set_directive /etc/openldap/ldap.conf BINDDN  "$BINDDN"
set_directive /etc/openldap/ldap.conf BINDPW  "$BINDPW"

###############################################################################
# 2. /etc/autofs_ldap_auth.conf   (XML 한 덩어리라 전체 생성)
###############################################################################

apply_autofs_auth()
{
    local tmp; tmp="$(mktemp)"
    cat > "$tmp" <<XMLEOF
<?xml version="1.0" ?>
<autofs_ldap_sasl_conf
    usetls="no"
    tlsrequired="no"
    authrequired="simple"
    user="$BINDDN"
    secret="$BINDPW"
/>
XMLEOF
    commit_file /etc/autofs_ldap_auth.conf "$tmp"
    # 자격증명 파일이라 퍼미션을 조입니다.
    [ "$DRYRUN" = "1" ] || chmod 600 "$ROOT/etc/autofs_ldap_auth.conf" 2>/dev/null
}
apply_autofs_auth

###############################################################################
# 3. /etc/autofs.conf
###############################################################################

set_kv /etc/autofs.conf ldap_uri "\"$URI_LINE\""

###############################################################################
# 4. 인증 백엔드 : nslcd.conf 또는 sssd.conf
###############################################################################

if [ "$AUTH_BACKEND" = "nslcd" ]; then
    set_directive /etc/nslcd.conf uri     "$URI_LINE"
    set_directive /etc/nslcd.conf binddn  "$BINDDN"
    set_directive /etc/nslcd.conf bindpw  "$BINDPW"
    [ "$DRYRUN" = "1" ] || chmod 600 "$ROOT/etc/nslcd.conf" 2>/dev/null
else
    set_ini_in_section /etc/sssd/sssd.conf '[domain/' ldap_uri              "$URI_LINE"
    set_ini_in_section /etc/sssd/sssd.conf '[domain/' ldap_default_bind_dn  "$BINDDN"
    set_ini_in_section /etc/sssd/sssd.conf '[domain/' ldap_default_authtok  "$BINDPW"
    [ "$DRYRUN" = "1" ] || chmod 600 "$ROOT/etc/sssd/sssd.conf" 2>/dev/null
fi

###############################################################################
# 5. /etc/resolv.conf
###############################################################################

set_line_list /etc/resolv.conf nameserver "$DNS_LIST" ""

###############################################################################
# 6. 시각 동기화 : ntp.conf 또는 chrony.conf
###############################################################################

if [ "$TIME_BACKEND" = "ntp" ]; then
    set_line_list /etc/ntp.conf server "$NTP_LIST" "iburst" "pool"
else
    set_line_list /etc/chrony.conf server "$NTP_LIST" "iburst" "pool"
fi

###############################################################################
# 7. /etc/auto.appl
#
# 이 파일은 우리가 전적으로 관리하는 파일이라, 기존 내용을 보존하며 고치지
# 않고 통째로 덮어씁니다(위 다른 파일들과 다른 방식입니다 — autofs.conf/
# sssd.conf 처럼 운영자가 손댄 다른 설정과 섞여 있지 않기 때문입니다).
#
# /wappl 은 site 에 wappl_mount 가 설정된 경우에만 줄을 추가합니다(선택 항목).
###############################################################################

apply_auto_appl()
{
    local logical=/etc/auto.appl
    local real="$ROOT$logical"
    local tmp; tmp="$(mktemp)"

    {
        printf '/appl%s-ro,hard,tcp,vers=3%s%s:%s\n' "$TAB" "$TAB" "$APPL_STORAGE" "$APPL_MOUNT"
        if [ -n "$WAPPL_MOUNT" ]; then
            printf '/wappl%s-rw,hard,tcp,vers=3%s%s:%s\n' "$TAB" "$TAB" "$APPL_STORAGE" "$WAPPL_MOUNT"
        fi
    } > "$tmp"

    # 덮어쓰기 전, 기존 파일이 있으면 고정 이름으로도 한 벌 남깁니다.
    # (아래 commit_file 이 만드는 .bak.$STAMP 백업과는 별개로, 요청에 따라
    #  항상 같은 이름 하나로 최근 이전 내용을 바로 찾아볼 수 있게 합니다.
    #  되돌리기(rollback)는 여전히 .bak.$STAMP 를 씁니다 — 이 파일은 참고용입니다.)
    if [ -f "$real" ] && [ "$DRYRUN" != "1" ]; then
        cp -p "$real" "${ROOT}/etc/auto.appl_back" || { fail "백업 실패: /etc/auto.appl_back"; rm -f "$tmp"; return 1; }
    fi

    commit_file "$logical" "$tmp"
}
apply_auto_appl

###############################################################################
# 8. 바뀐 파일에 대응하는 서비스만 재시작 (표는 lib_common.sh 에 있습니다)
###############################################################################

restart_for_changed

###############################################################################
# 9. 결과 한 줄 요약
###############################################################################

if [ "$FAILED" = "1" ]; then
    echo "RESULT|$NODE_HOST|FAIL|$INFRA|$SITE"
    exit 1
fi

if [ -z "$CHANGED" ]; then
    echo "RESULT|$NODE_HOST|NOCHANGE|$INFRA|$SITE"
else
    echo "RESULT|$NODE_HOST|OK|$INFRA|$SITE|changed:$(echo $CHANGED | tr ' ' ',')"
fi
exit 0
