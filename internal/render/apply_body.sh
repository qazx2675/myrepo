###############################################################################
# 설정 적용 본체. Go 엔진(render.go)이 위 헤더에 아래 변수와 HOST_MAP 을 붙입니다.
#   NETWORK_SCRIPTS_DIR  conf 의 network_scripts_dir (참고용. 강제 경로는 FORCED_DIR 로 판단)
#   RHEL9_PATH           conf 의 rhel9_path. 값이 있고 OS 가 9 이상이면 이 경로만 찾음(강제 경로)
#   FORCED_DIR           conf 에 network_scripts_dir 를 기본값과 다르게 적었을 때만 값이 있음.
#                        값이 있으면 이 경로만 찾음(강제 경로). 비어 있으면 자동으로 찾음
#   CHECK_ONLY           1 이면 바꿀 내용만 출력하고 아무 파일도 쓰지 않음 (-check)
#   BOND_MODE            새 bond 프로필의 모드 (active-backup 또는 802.3ad)
#
# 동작 (수정계획서 4.1 ~ 4.10)
#   1. hostname 으로 HOST_MAP 에서 이 노드의 변경될 IP 를 찾음. 게이트웨이 = 변경 IP 의 끝자리 1
#   2. /etc/redhat-release (없으면 /etc/os-release) 로 OS 버전 확인
#   3. 장치 선택: SSH_CONNECTION 3번째 값(접속을 받은 IP)을 가진 장치.
#      없으면 기본 경로(default route) 장치. SSH 세션이 지나지 않는 usb0 등은 고르지 않음
#   4. 설정 파일 찾기: IP 값이 아니라 장치 이름으로 찾음 (두 번째 실행에서도 같은 파일)
#      강제 경로 -> nmcli 조회(읽기 전용) -> ifcfg 스캔(DEVICE/파일명/HWADDR)
#      -> keyfile 스캔(interface-name/mac-address). 백업류 파일은 제외
#   5. 이미 바뀐 파일(새 IP, 새 게이트웨이, 고정 방식)은 손대지 않음 (백업도 안 함)
#   6. 백업: /var/lib/ip_change/backup/<STAMP>/<원래 전체 경로> + manifest
#   7. 필요한 줄만 고침 (DHCP 면 고정으로 바꿈). 파일이 없으면 새 프로필을 만듦
#   8. 다시 읽어 검증. IP/게이트웨이/방식이 틀리면 백업으로 되돌리고 FAIL
#   9. 재부팅 후 유지 점검 (ONBOOT/autoconnect, 경쟁 프로필, 링크, 예전 게이트웨이) -> OK/WARN
#   네트워크 서비스 재시작, nmcli 쓰기 명령, 실행 중인 인터페이스 변경은 하지 않음
#
# 출력 (한 줄. gossh 가 "<host>: " 를 앞에 붙임. 필드 값에는 "|" 가 들어가지 않음)
#   RESULT|<상태>|<host>|<기존IP>|<변경IP>|<게이트웨이>|<장치>|<방식>|<OS>|<파일>|<비고>
#   상태: OK, WARN, FAIL, CHECK     방식: static, dhcp->static, new-file, already
#   비고는 "; " 로 구분. 기계용 표시:
#     BACKUP=<STAMP>          백업을 만든 경우
#     OLDGW=<예전 게이트웨이>  GWFIX 와 함께 나옴
#     GWFIX=<파일1>,<파일2>   예전 게이트웨이가 남은 파일 (엔진이 나중에 고칠지 물음)
#   -check 이면 RESULT 앞에 "PLAN|<파일>|<내용>" 줄들을 출력
#
# 테스트: ROOT 환경변수(운영에서는 비어 있음)를 노드의 모든 파일 경로 앞에 붙입니다.
#   ip, hostname, nmcli 는 이름으로 부르므로 PATH 앞에 가짜를 두면 됩니다.
###############################################################################

STAMP="$(date +%Y%m%d%H%M%S)"
NODE_HOST="$(hostname -s 2>/dev/null || hostname)"
NODE_FQDN="$(hostname 2>/dev/null || echo "$NODE_HOST")"

ROOT="${ROOT:-}"
NETWORK_SCRIPTS_DIR="${NETWORK_SCRIPTS_DIR:-}"
RHEL9_PATH="${RHEL9_PATH:-}"
FORCED_DIR="${FORCED_DIR:-}"
CHECK_ONLY="${CHECK_ONLY:-0}"
BOND_MODE="${BOND_MODE:-active-backup}"

LEGACY_DIR="$ROOT/etc/sysconfig/network-scripts"
KEYFILE_DIR="$ROOT/etc/NetworkManager/system-connections"
BACKUP_BASE="$ROOT/var/lib/ip_change/backup"
BK_DIR="$BACKUP_BASE/$STAMP"

NEW_IP=""; GATEWAY=""; CUR_IP=""; CUR_PREFIX=""; DEV=""; MODE=""
OS_MAJOR=""; OS_LABEL=""; TARGET_FILE=""; TARGET_FMT=""
WARN_TXT=""; NOTE_TXT=""; BACKED_UP=0; OLD_FILE_GW=""; MATCHES=""

#------------------------------------------------------------------------------
# 공통 도구
#------------------------------------------------------------------------------

add_warn() { WARN_TXT="${WARN_TXT:+$WARN_TXT; }$1"; }
add_note() { NOTE_TXT="${NOTE_TXT:+$NOTE_TXT; }$1"; }

# 결과 필드 값에서 구분자 "|" 와 줄바꿈을 없앱니다
field() { printf '%s' "$1" | tr '|\r\n\t' '/   '; }

# ROOT 를 뗀 노드 기준 경로
nodepath()
{
    local p="$1"
    if [ -n "$ROOT" ]; then p="${p#"$ROOT"}"; fi
    printf '%s' "$p"
}

emit()
{
    local note="$WARN_TXT"
    if [ -n "$NOTE_TXT" ]; then note="${note:+$note; }$NOTE_TXT"; fi
    echo "RESULT|$1|$(field "$NODE_HOST")|$(field "$CUR_IP")|$(field "$NEW_IP")|$(field "$GATEWAY")|$(field "$DEV")|$(field "$MODE")|$(field "$OS_LABEL")|$(field "$(nodepath "$TARGET_FILE")")|$(field "$note")"
}

fail_out()
{
    WARN_TXT="$1${WARN_TXT:+; $WARN_TXT}"
    emit FAIL
    exit 1
}

# ifcfg 파일을 "KEY=값" 줄로 정리 (주석 제외, 따옴표/앞뒤 공백/CR 제거, 키는 대문자)
ifcfg_dump()
{
    awk 'BEGIN { q = sprintf("%c", 39) }
    {
        sub(/\r$/, ""); l = $0; sub(/^[ \t]+/, "", l)
        if (l == "" || substr(l, 1, 1) == "#") next
        p = index(l, "="); if (p < 2) next
        k = substr(l, 1, p - 1); sub(/[ \t]+$/, "", k)
        if (k ~ /[^A-Za-z0-9_]/) next
        v = substr(l, p + 1); sub(/^[ \t]+/, "", v); sub(/[ \t]+$/, "", v)
        f = substr(v, 1, 1); e = substr(v, length(v), 1)
        if (length(v) >= 2 && f == e && (f == "\"" || f == q)) v = substr(v, 2, length(v) - 2)
        print toupper(k) "=" v
    }' "$1" 2>/dev/null
}

# keyfile 을 "섹션.키=값" 줄로 정리
kf_dump()
{
    awk '{
        sub(/\r$/, ""); l = $0; sub(/^[ \t]+/, "", l); sub(/[ \t]+$/, "", l)
        c = substr(l, 1, 1)
        if (l == "" || c == "#" || c == ";") next
        if (c == "[") { s = l; sub(/^\[/, "", s); sub(/\].*$/, "", s); next }
        p = index(l, "="); if (p < 2) next
        k = substr(l, 1, p - 1); sub(/[ \t]+$/, "", k)
        v = substr(l, p + 1); sub(/^[ \t]+/, "", v)
        print s "." k "=" v
    }' "$1" 2>/dev/null
}

# dump 결과($1)에서 키($2)의 값 (같은 키가 여럿이면 마지막 값)
dget()
{
    printf '%s\n' "$1" | awk -v k="$2" '{ p = index($0, "="); if (substr($0, 1, p - 1) == k) { v = substr($0, p + 1); f = 1 } } END { if (f) print v }'
}

# dump 결과($1)에 키($2)가 있는지
dhas()
{
    printf '%s\n' "$1" | awk -v k="$2" '{ p = index($0, "="); if (substr($0, 1, p - 1) == k) f = 1 } END { exit !f }'
}

# 설정 파일로 쓰지 않는 이름 (백업류, 패키지 잔여 파일, 숨김 파일, 백업 디렉토리)
is_ignored()
{
    case "${1##*/}" in
        *.bak|*.bak.*|*.backup|*.backup.*|*.rpmnew|*.rpmsave|*.rpmorig|*.orig|*.old|*.rej|*.swp|*.tmp|*~|.*) return 0 ;;
    esac
    case "$1" in "$BACKUP_BASE"/*) return 0 ;; esac
    return 1
}

fmt_of()
{
    case "${1##*/}" in ifcfg-*) echo ifcfg ;; *) echo keyfile ;; esac
}

prefix2mask()
{
    awk -v p="$1" 'BEGIN {
        if (p !~ /^[0-9]+$/ || p + 0 > 32) exit 1
        m = ""
        for (i = 0; i < 4; i++) {
            b = p - i * 8
            if (b >= 8) o = 255; else if (b <= 0) o = 0; else o = 256 - 2 ^ (8 - b)
            m = m (i ? "." : "") o
        }
        print m
    }'
}

mask2prefix()
{
    awk -v m="$1" 'BEGIN {
        if (split(m, a, ".") != 4) exit 1
        c = 0
        for (i = 1; i <= 4; i++) { o = a[i] + 0; while (o > 0) { c += o % 2; o = int(o / 2) } }
        print c
    }'
}

# "키=값" 줄 목록($1)에서 IP 키 하나 고르기: 값이 새 IP -> 현재 IP -> 키 $2 -> 첫 키
pick_key()
{
    printf '%s\n' "$1" | awk -v a="$NEW_IP" -v b="$CUR_IP" -v pref="$2" 'NF {
        p = index($0, "="); k = substr($0, 1, p - 1); ip = substr($0, p + 1); sub(/\/.*/, "", ip)
        if (ip == a && ka == "") ka = k
        if (ip == b && kb == "") kb = k
        if (k == pref && kp == "") kp = k
        if (kf == "") kf = k
    } END { if (ka != "") print ka; else if (kb != "") print kb; else if (kp != "") print kp; else print kf }'
}

#------------------------------------------------------------------------------
# 1. HOST_MAP 에서 이 노드의 변경될 IP 찾기 (hostname -s, hostname 둘 다 매칭 시도)
#------------------------------------------------------------------------------

while read -r m_host m_ip; do
    [ -z "$m_host" ] && continue
    if [ "$m_host" = "$NODE_HOST" ] || [ "$m_host" = "$NODE_FQDN" ]; then
        NEW_IP="$m_ip"
        break
    fi
done <<'HOSTMAPEOF'
__HOST_MAP__
HOSTMAPEOF

if [ -z "$NEW_IP" ]; then
    fail_out "대상 목록에 이 hostname($NODE_HOST)이 없습니다"
fi

GATEWAY="$(echo "$NEW_IP" | awk -F. '{print $1"."$2"."$3".1"}')"

#------------------------------------------------------------------------------
# 2. OS 버전 (결과 표시용 이름과 메이저 버전)
#------------------------------------------------------------------------------

detect_os()
{
    local rel="" name="" ver=""
    if [ -f "$ROOT/etc/redhat-release" ]; then
        rel="$(head -n1 "$ROOT/etc/redhat-release" | tr -d '\r')"
        ver="$(echo "$rel" | sed -n 's/.*release \([0-9][0-9.]*\).*/\1/p')"
        case "$rel" in
            "Red Hat"*) name="RHEL" ;;
            *) name="${rel%% *}" ;;
        esac
    fi
    if [ -z "$ver" ] && [ -f "$ROOT/etc/os-release" ]; then
        ver="$(sed -n 's/^VERSION_ID="\{0,1\}\([0-9][0-9.]*\).*/\1/p' "$ROOT/etc/os-release" | head -n1)"
        name="$(sed -n 's/^ID="\{0,1\}\([A-Za-z0-9_-]*\).*/\1/p' "$ROOT/etc/os-release" | head -n1)"
        [ "$name" = "rhel" ] && name="RHEL"
    fi
    OS_MAJOR="${ver%%.*}"
    OS_LABEL="${name:-OS} ${ver:-?}"
}
detect_os

#------------------------------------------------------------------------------
# 3. 대상 장치 선택 (4.2)
#------------------------------------------------------------------------------

# "장치 IP 프리픽스" (scope global 인 IPv4 만)
ADDR4="$(ip -4 -o addr show 2>/dev/null | awk '{
    d = $2; sub(/:$/, "", d); sub(/@.*/, "", d); ip = ""; pf = ""; sc = ""
    for (i = 3; i < NF; i++) {
        if ($i == "inet") { n = split($(i + 1), a, "/"); ip = a[1]; pf = (n > 1) ? a[2] : "32" }
        if ($i == "scope") sc = $(i + 1)
    }
    if (ip != "" && sc == "global") print d, ip, pf
}')"

SSH_SRV_IP="$(echo "${SSH_CONNECTION:-}" | awk '{print $3}')"
SEL=""
if [ -n "$SSH_SRV_IP" ]; then
    SEL="$(echo "$ADDR4" | awk -v ip="$SSH_SRV_IP" '$2 == ip { print; exit }')"
fi
if [ -z "$SEL" ]; then
    DEF_DEV="$(ip -4 route show default 2>/dev/null | awk '{ for (i = 1; i < NF; i++) if ($i == "dev") { print $(i + 1); exit } }')"
    DEF_DEV="${DEF_DEV%%@*}"
    if [ -n "$DEF_DEV" ]; then
        SEL="$(echo "$ADDR4" | awk -v d="$DEF_DEV" '$1 == d { print; exit }')"
    fi
    if [ -z "$SEL" ]; then
        fail_out "대상 장치를 찾지 못했습니다 (SSH 접속 IP=${SSH_SRV_IP:-없음}, 기본 경로 장치=${DEF_DEV:-없음})"
    fi
    if [ -n "$SSH_SRV_IP" ]; then
        add_note "SSH 접속 IP($SSH_SRV_IP)를 가진 장치가 없어 기본 경로 장치를 사용"
    else
        add_note "SSH_CONNECTION 이 없어 기본 경로 장치를 사용"
    fi
fi
read -r DEV CUR_IP CUR_PREFIX <<< "$SEL"
DEV="${DEV%%@*}"

LIVE_MASK="$(prefix2mask "$CUR_PREFIX")"
[ -z "$LIVE_MASK" ] && fail_out "현재 프리픽스($CUR_PREFIX)를 넷마스크로 바꿀 수 없습니다"

LIVE_GW="$(ip -4 route show default 2>/dev/null | awk -v d="$DEV" '{
    dv = ""; gw = ""
    for (i = 1; i < NF; i++) { if ($i == "dev") dv = $(i + 1); if ($i == "via") gw = $(i + 1) }
    sub(/@.*/, "", dv)
    if (dv == d && gw != "") { print gw; exit }
}')"

LINK_LINE="$(ip -o link show dev "$DEV" 2>/dev/null | head -n1)"
MAC="$(echo "$LINK_LINE" | awk '{ for (i = 1; i < NF; i++) if ($i == "link/ether") { print tolower($(i + 1)); exit } }')"
LINK_OK=0
if echo "$LINK_LINE" | grep -q 'LOWER_UP' &&
   echo "$LINK_LINE" | awk '{ for (i = 1; i < NF; i++) if ($i == "state") s = $(i + 1) } END { exit (s != "UP") }'; then
    LINK_OK=1
fi

#------------------------------------------------------------------------------
# 4. 설정 파일 찾기 (4.3)
#------------------------------------------------------------------------------

# NetworkManager 실행 여부 (읽기 전용 조회)
NM_RUN=0
if command -v nmcli >/dev/null 2>&1; then
    nm_s="$(nmcli -t -f RUNNING general 2>/dev/null | head -n1)"
    [ "$nm_s" = "running" ] || nm_s="$(nmcli -t -f RUNNING nm 2>/dev/null | head -n1)"
    if [ "$nm_s" = "running" ]; then NM_RUN=1; fi
fi

# 강제 경로 (conf 에 적은 경우만)
FORCED=""
if [ -n "$RHEL9_PATH" ] && [ -n "$OS_MAJOR" ] && [ "$OS_MAJOR" -ge 9 ] 2>/dev/null; then
    FORCED="$ROOT$RHEL9_PATH"
elif [ -n "$FORCED_DIR" ]; then
    FORCED="$ROOT$FORCED_DIR"
fi
if [ -n "$FORCED" ] && [ ! -d "$FORCED" ]; then
    fail_out "설정 경로가 없습니다: $(nodepath "$FORCED")"
fi

# ifcfg 파일 $1 이 장치 $2 (MAC $3) 의 것인지. $4 는 비교할 파일 이름(기본은 실제 이름)
ifcfg_is_for()
{
    local f="$1" dev="$2" mac="$3" name="${4:-}" d dv hw
    [ -z "$name" ] && name="${f##*/}"
    d="$(ifcfg_dump "$f")"
    dv="$(dget "$d" DEVICE)"
    if [ -n "$dv" ]; then
        [ "$dv" = "$dev" ]
        return
    fi
    [ "$name" = "ifcfg-$dev" ] && return 0
    hw="$(dget "$d" HWADDR | tr 'A-F' 'a-f')"
    [ -n "$mac" ] && [ "$hw" = "$mac" ] && [ -z "$(dget "$d" MASTER)" ] &&
        [ -z "$(dget "$d" TEAM_MASTER)" ] && [ -z "$(dget "$d" BRIDGE)" ]
}

# keyfile $1 이 장치 $2 (MAC $3) 의 것인지
kf_is_for()
{
    local f="$1" dev="$2" mac="$3" d ifn m
    d="$(kf_dump "$f")"
    ifn="$(dget "$d" connection.interface-name)"
    if [ -n "$ifn" ]; then
        [ "$ifn" = "$dev" ]
        return
    fi
    m="$(dget "$d" ethernet.mac-address)"
    [ -z "$m" ] && m="$(dget "$d" 802-3-ethernet.mac-address)"
    m="$(echo "$m" | tr 'A-F' 'a-f' | sed 's/;$//')"
    [ -n "$mac" ] && [ "$m" = "$mac" ] && [ -z "$(dget "$d" connection.master)" ] &&
        [ -z "$(dget "$d" connection.controller)" ]
}

# 부팅 시 시작하는 프로필인지 (ONBOOT 가 no 가 아니고 autoconnect 가 false 가 아님)
boot_on()
{
    local v
    if [ "$(fmt_of "$1")" = ifcfg ]; then
        v="$(dget "$(ifcfg_dump "$1")" ONBOOT | tr 'A-Z' 'a-z')"
        [ "$v" != "no" ]
    else
        v="$(dget "$(kf_dump "$1")" connection.autoconnect | tr 'A-Z' 'a-z')"
        [ "$v" != "false" ]
    fi
}

# 디렉토리 $1 에서 장치 $3 (MAC $4) 의 파일 경로를 한 줄씩 출력
#   $2: ifcfg (ifcfg-* 만), keyfile (ifcfg-* 외 전부), mixed (ifcfg-* 와 *.nmconnection)
scan_dir()
{
    local dir="$1" kind="$2" dev="$3" mac="$4" f name
    [ -d "$dir" ] || return 0
    for f in "$dir"/*; do
        [ -f "$f" ] || continue
        is_ignored "$f" && continue
        name="${f##*/}"
        case "$name" in
            ifcfg-lo) continue ;;
            ifcfg-*)
                [ "$kind" = keyfile ] && continue
                ifcfg_is_for "$f" "$dev" "$mac" && echo "$f"
                ;;
            route-*|route6-*|rule-*|rule6-*|keys-*|readme*|README*) continue ;;
            *)
                [ "$kind" = ifcfg ] && continue
                if [ "$kind" = mixed ]; then
                    case "$name" in *.nmconnection) ;; *) continue ;; esac
                fi
                kf_is_for "$f" "$dev" "$mac" && echo "$f"
                ;;
        esac
    done
}

# 장치 $1 의 파일을 찾을 곳 전체에서 찾기
scan_all()
{
    if [ -n "$FORCED" ]; then
        scan_dir "$FORCED" mixed "$1" "$2"
    else
        scan_dir "$LEGACY_DIR" ifcfg "$1" "$2"
        scan_dir "$KEYFILE_DIR" keyfile "$1" "$2"
    fi
}

NM_FILE=""
if [ -z "$FORCED" ] && [ "$NM_RUN" = 1 ]; then
    nm_conn="$(nmcli -t -f GENERAL.CONNECTION device show "$DEV" 2>/dev/null | head -n1 | sed 's/^GENERAL\.CONNECTION://')"
    [ "$nm_conn" = "--" ] && nm_conn=""
    nm_fn="$(nmcli -t -f NAME,UUID,DEVICE,FILENAME connection show 2>/dev/null | awk -v d="$DEV" -v n="$nm_conn" '
        BEGIN { E = sprintf("%c", 1) }
        {
            line = $0; gsub(/\\:/, E, line)
            c = split(line, f, ":")
            for (i = 1; i <= c; i++) gsub(E, ":", f[i])
            if (f[3] == d && byd == "") byd = f[4]
            if (n != "" && f[1] == n && byn == "") byn = f[4]
        }
        END { if (byd != "") print byd; else print byn }')"
    case "$nm_fn" in
        /run/*)
            add_note "NetworkManager 프로필이 메모리에만 있음($nm_fn)"
            nm_fn=""
            ;;
    esac
    if [ -n "$nm_fn" ] && [ -f "$ROOT$nm_fn" ]; then
        NM_FILE="$ROOT$nm_fn"
    fi
fi

MATCHES="$(scan_all "$DEV" "$MAC")"

if [ -n "$NM_FILE" ]; then
    TARGET_FILE="$NM_FILE"
elif [ -n "$MATCHES" ]; then
    n_match="$(printf '%s\n' "$MATCHES" | grep -c .)"
    if [ "$n_match" -eq 1 ]; then
        TARGET_FILE="$MATCHES"
    else
        # 후보가 여럿: IP 가 현재 IP/새 IP 인 파일 -> 이름이 장치 이름인 파일 -> 첫 파일
        by_ip=""; by_name=""; first=""
        while IFS= read -r f; do
            [ -z "$f" ] && continue
            [ -z "$first" ] && first="$f"
            case "$(fmt_of "$f")" in
                ifcfg) d="$(ifcfg_dump "$f")"; v="$(printf '%s\n' "$d" | awk -F= '$1 ~ /^IPADDR[0-9]*$/ { print $2 }')" ;;
                *) d="$(kf_dump "$f")"; v="$(printf '%s\n' "$d" | awk '{ p = index($0, "="); k = substr($0, 1, p - 1); v = substr($0, p + 1); sub(/\/.*/, "", v) } k ~ /^ipv4\.address(es)?[0-9]+$/ { print v }')" ;;
            esac
            if [ -z "$by_ip" ] && printf '%s\n' "$v" | grep -qxF -e "$CUR_IP" -e "$NEW_IP"; then by_ip="$f"; fi
            case "${f##*/}" in "ifcfg-$DEV"|"$DEV.nmconnection") [ -z "$by_name" ] && by_name="$f" ;; esac
        done <<< "$MATCHES"
        TARGET_FILE="${by_ip:-${by_name:-$first}}"
    fi
fi

#------------------------------------------------------------------------------
# 5. 파일의 현재 값 읽기
#------------------------------------------------------------------------------

# 파일 $2 (형식 $1) 의 값을 S_* 변수에 읽습니다
load_state()
{
    local fmt="$1" f="$2" d keys v m rest bp
    S_METHOD=""; S_IPKEY=""; S_IP=""; S_IDX=""; S_PREFIX=""; S_HASMASK=0
    S_GW=""; S_GWKEY=""; S_GW2=""; S_LINEGW=0; S_BOOT=yes; S_MAXIDX=0
    if [ "$fmt" = ifcfg ]; then
        d="$(ifcfg_dump "$f")"
        keys="$(printf '%s\n' "$d" | awk '{ p = index($0, "="); k = substr($0, 1, p - 1) }
            k ~ /^IPADDR[0-9]*$/ { if (!(k in v)) o[++n] = k; v[k] = substr($0, p + 1) }
            END { for (i = 1; i <= n; i++) if (v[o[i]] != "") print o[i] "=" v[o[i]] }')"
        S_IPKEY="$(pick_key "$keys" IPADDR)"
        if [ -n "$S_IPKEY" ]; then
            S_IP="$(dget "$d" "$S_IPKEY")"
            S_IDX="${S_IPKEY#IPADDR}"
        fi
        bp="$(dget "$d" BOOTPROTO | tr 'A-Z' 'a-z')"
        case "$bp" in
            dhcp|bootp) S_METHOD=dhcp ;;
            "") if [ -z "$S_IPKEY" ]; then S_METHOD=dhcp; else S_METHOD=static; fi ;;
            none|static) S_METHOD=static ;;
            *) S_METHOD="$bp" ;;
        esac
        v="$(dget "$d" "PREFIX$S_IDX")"
        [ -z "$v" ] && v="$(dget "$d" PREFIX)"
        if [ -n "$v" ]; then
            S_PREFIX="$v"
        else
            m="$(dget "$d" "NETMASK$S_IDX")"
            [ -z "$m" ] && m="$(dget "$d" NETMASK)"
            [ -n "$m" ] && S_PREFIX="$(mask2prefix "$m")"
        fi
        if [ -n "$S_IDX" ] && dhas "$d" "GATEWAY$S_IDX"; then
            S_GWKEY="GATEWAY$S_IDX"
        elif dhas "$d" GATEWAY; then
            S_GWKEY=GATEWAY
        fi
        [ -n "$S_GWKEY" ] && S_GW="$(dget "$d" "$S_GWKEY")"
        [ "$(dget "$d" ONBOOT | tr 'A-Z' 'a-z')" = "no" ] && S_BOOT=no
    else
        d="$(kf_dump "$f")"
        keys="$(printf '%s\n' "$d" | awk '{ p = index($0, "="); k = substr($0, 1, p - 1) }
            k ~ /^ipv4\.address(es)?[0-9]+$/ { sub(/^ipv4\./, "", k); print k "=" substr($0, p + 1) }')"
        S_MAXIDX="$(printf '%s\n' "$keys" | awk -F= '{ k = $1; gsub(/[^0-9]/, "", k); if (k + 0 > m) m = k + 0 } END { print m + 0 }')"
        S_IPKEY="$(pick_key "$keys" "")"
        if [ -n "$S_IPKEY" ]; then
            v="$(dget "$d" "ipv4.$S_IPKEY")"
            S_IP="${v%%/*}"
            case "$v" in */*) rest="${v#*/}" ;; *) rest="" ;; esac
            S_PREFIX="${rest%%,*}"
            case "$rest" in *,*) S_GW="${rest#*,}"; S_LINEGW=1 ;; esac
        fi
        if dhas "$d" ipv4.gateway; then
            S_GWKEY=gateway
            if [ "$S_LINEGW" = 1 ]; then S_GW2="$(dget "$d" ipv4.gateway)"; else S_GW="$(dget "$d" ipv4.gateway)"; fi
        fi
        v="$(dget "$d" ipv4.method)"
        case "$v" in
            manual) S_METHOD=static ;;
            auto|"") S_METHOD=dhcp ;;
            *) S_METHOD="$v" ;;
        esac
        [ "$(dget "$d" connection.autoconnect | tr 'A-Z' 'a-z')" = "false" ] && S_BOOT=no
    fi
    [ -n "$S_PREFIX" ] && S_HASMASK=1
    return 0
}

# 읽은 값이 새 IP, 새 게이트웨이, 고정 방식, 넷마스크를 모두 갖췄는지
state_has_new()
{
    [ "$S_METHOD" = static ] && [ "$S_IP" = "$NEW_IP" ] && [ "$S_GW" = "$GATEWAY" ] &&
        { [ -z "$S_GW2" ] || [ "$S_GW2" = "$GATEWAY" ]; } && [ "$S_HASMASK" = 1 ]
}

#------------------------------------------------------------------------------
# 6. 백업 / 되돌리기 (4.9)
#------------------------------------------------------------------------------

backup_modified()
{
    local np
    np="$(nodepath "$1")"
    mkdir -p "$BK_DIR$(dirname "$np")" || return 1
    chmod 700 "$BK_DIR" 2>/dev/null
    cp -p "$1" "$BK_DIR$np" || return 1
    echo "modified $np" >> "$BK_DIR/manifest" || return 1
    BACKED_UP=1
}

backup_created()
{
    mkdir -p "$BK_DIR" || return 1
    chmod 700 "$BK_DIR" 2>/dev/null
    echo "created $(nodepath "$1")" >> "$BK_DIR/manifest" || return 1
    BACKED_UP=1
}

# 이번 실행의 manifest 대로 되돌립니다 (modified: 백업 복사, created: 백업 디렉토리로 옮김)
restore_all()
{
    local act p
    [ -f "$BK_DIR/manifest" ] || return 0
    while read -r act p; do
        case "$act" in
            modified) cp -p "$BK_DIR$p" "$ROOT$p" ;;
            created)
                if [ -e "$ROOT$p" ]; then
                    mkdir -p "$BK_DIR$(dirname "$p")" && mv -f "$ROOT$p" "$BK_DIR$p.removed"
                fi
                ;;
        esac
    done < "$BK_DIR/manifest"
}

#------------------------------------------------------------------------------
# 7. 파일 고치기 (4.4 ifcfg, 4.5 keyfile)
#    SETS: "키=값" 줄 목록. 있는 키는 그 줄만 바꾸고, 없는 키는 ANCHOR 키 줄 바로 아래에
#    넣음 (ANCHOR 줄이 없으면 ifcfg 는 파일 끝, keyfile 은 섹션 끝). 줄 끝은 LF 로 저장
#------------------------------------------------------------------------------

AWK_IFCFG_EDIT='
function keyof(l,   p, k) {
    sub(/^[ \t]+/, "", l)
    if (substr(l, 1, 1) == "#") return ""
    p = index(l, "="); if (p < 2) return ""
    k = substr(l, 1, p - 1); sub(/[ \t]+$/, "", k)
    if (k ~ /[^A-Za-z0-9_]/) return ""
    return toupper(k)
}
function quote_of(l,   p, v) {
    p = index(l, "="); v = substr(l, p + 1); sub(/^[ \t]+/, "", v)
    return (substr(v, 1, 1) == "\"") ? "\"" : ""
}
function flush(   j, u) {
    for (j = 1; j <= cnt; j++) {
        u = toupper(ord[j])
        if (!(u in seen)) { print ord[j] "=" val[u]; seen[u] = 1 }
    }
}
BEGIN {
    n = split(ENVIRON["E_SETS"], a, "\n")
    for (i = 1; i <= n; i++) {
        if (a[i] == "") continue
        p = index(a[i], "="); k = substr(a[i], 1, p - 1)
        ord[++cnt] = k; val[toupper(k)] = substr(a[i], p + 1)
    }
    anchor = toupper(ENVIRON["E_ANCHOR"])
}
{ sub(/\r$/, ""); L[++nl] = $0 }
END {
    last = 0
    for (i = 1; i <= nl; i++) {
        K[i] = keyof(L[i])
        if (K[i] != "" && (K[i] in val)) seen[K[i]] = 1
        if (anchor != "" && K[i] == anchor) last = i
    }
    for (i = 1; i <= nl; i++) {
        if (K[i] != "" && (K[i] in val)) { q = quote_of(L[i]); print K[i] "=" q val[K[i]] q }
        else print L[i]
        if (i == last) flush()
    }
    if (last == 0) flush()
}'

AWK_KF_EDIT='
function flush(   j) {
    for (j = 1; j <= cnt; j++) if (!(ord[j] in seen)) { print ord[j] "=" val[ord[j]]; seen[ord[j]] = 1 }
}
BEGIN {
    n = split(ENVIRON["E_SETS"], a, "\n")
    for (i = 1; i <= n; i++) {
        if (a[i] == "") continue
        p = index(a[i], "="); k = substr(a[i], 1, p - 1)
        ord[++cnt] = k; val[k] = substr(a[i], p + 1)
    }
    sect = ENVIRON["E_SECT"]; anchor = ENVIRON["E_ANCHOR"]
}
{ sub(/\r$/, ""); L[++nl] = $0 }
END {
    cur = ""; secstart = 0; secend = 0; last = 0
    for (i = 1; i <= nl; i++) {
        l = L[i]; sub(/^[ \t]+/, "", l); sub(/[ \t]+$/, "", l); K[i] = ""
        c = substr(l, 1, 1)
        if (c == "[") {
            s = l; sub(/^\[/, "", s); sub(/\].*$/, "", s); cur = s
            if (s == sect && !secstart) { secstart = i; secend = i }
            continue
        }
        if (cur != sect || l == "" || c == "#" || c == ";") continue
        secend = i
        p = index(l, "="); if (p < 2) continue
        k = substr(l, 1, p - 1); sub(/[ \t]+$/, "", k); K[i] = k
        if (k in val) seen[k] = 1
        if (anchor != "" && k == anchor) last = i
    }
    ins = last
    if (ins == 0 && secstart > 0) ins = secend
    for (i = 1; i <= nl; i++) {
        if (K[i] != "" && (K[i] in val)) print K[i] "=" val[K[i]]
        else print L[i]
        if (i == ins) flush()
    }
    if (ins == 0) {
        if (nl > 0 && L[nl] != "") print ""
        print "[" sect "]"
        flush()
    }
}'

SETS=""; ANCHOR=""
add_set() { SETS="$SETS$1"$'\n'; }

# 바꿀 내용(SETS, ANCHOR, MODE) 정하기. load_state 를 먼저 불러야 함
plan_edit()
{
    local k prefix
    SETS=""; ANCHOR=""
    if [ -n "$S_IP" ] && [ "$S_IP" != "$CUR_IP" ] && [ "$S_IP" != "$NEW_IP" ]; then
        add_note "파일 IP($S_IP)와 현재 IP($CUR_IP)가 다름"
    fi
    if [ "$TARGET_FMT" = ifcfg ]; then
        k="${S_IPKEY:-IPADDR}"
        if [ "$S_METHOD" = dhcp ]; then
            MODE="dhcp->static"
            add_set "BOOTPROTO=none"
            add_set "$k=$NEW_IP"
            if [ -n "$(dget "$(ifcfg_dump "$TARGET_FILE")" "PREFIX$S_IDX")" ]; then
                add_set "PREFIX$S_IDX=$CUR_PREFIX"
            else
                add_set "NETMASK$S_IDX=$LIVE_MASK"
            fi
            add_set "${S_GWKEY:-GATEWAY}=$GATEWAY"
            ANCHOR=BOOTPROTO
        else
            MODE=static
            add_set "$k=$NEW_IP"
            [ "$S_HASMASK" = 1 ] || add_set "NETMASK$S_IDX=$LIVE_MASK"
            add_set "${S_GWKEY:-GATEWAY}=$GATEWAY"
            if [ -n "$S_IPKEY" ]; then ANCHOR="$S_IPKEY"; else ANCHOR=BOOTPROTO; fi
        fi
    else
        if [ "$S_METHOD" = dhcp ]; then
            MODE="dhcp->static"
            # 현재 IP 를 가진 주소 키가 없으면 비어 있는 다음 번호를 씀 (다른 주소는 그대로 둠)
            if [ -n "$S_IPKEY" ] && { [ "$S_IP" = "$CUR_IP" ] || [ "$S_IP" = "$NEW_IP" ]; }; then
                k="$S_IPKEY"
            else
                k="address$((S_MAXIDX + 1))"
            fi
            add_set "method=manual"
            add_set "$k=$NEW_IP/$CUR_PREFIX"
            add_set "gateway=$GATEWAY"
            ANCHOR=method
        else
            MODE=static
            k="${S_IPKEY:-address1}"
            prefix="${S_PREFIX:-$CUR_PREFIX}"
            if [ "$S_LINEGW" = 1 ]; then
                add_set "$k=$NEW_IP/$prefix,$GATEWAY"
            else
                add_set "$k=$NEW_IP/$prefix"
            fi
            if [ -n "$S_GWKEY" ] || [ "$S_LINEGW" != 1 ]; then add_set "gateway=$GATEWAY"; fi
            if [ -n "$S_IPKEY" ]; then ANCHOR="$S_IPKEY"; else ANCHOR=method; fi
        fi
    fi
}

apply_edit()
{
    local tmp rc
    tmp="$(mktemp 2>/dev/null)" || return 1
    if [ "$TARGET_FMT" = ifcfg ]; then
        E_SETS="$SETS" E_ANCHOR="$ANCHOR" awk "$AWK_IFCFG_EDIT" "$TARGET_FILE" > "$tmp"
    else
        E_SETS="$SETS" E_ANCHOR="$ANCHOR" E_SECT=ipv4 awk "$AWK_KF_EDIT" "$TARGET_FILE" > "$tmp"
    fi
    rc=$?
    if [ "$rc" -ne 0 ] || [ ! -s "$tmp" ]; then
        rm -f "$tmp"
        return 1
    fi
    # cat > 로 덮어써서 원래 파일의 권한(keyfile 600)과 소유자를 유지
    cat "$tmp" > "$TARGET_FILE" || { rm -f "$tmp"; return 1; }
    rm -f "$tmp"
    return 0
}

#------------------------------------------------------------------------------
# 8. 새 프로필 만들기 (4.6, 형식은 잠정 - 수정계획서 D16)
#------------------------------------------------------------------------------

# 장치 종류: ethernet, bond, vlan, bridge, team, other
dev_kind()
{
    local d="$1" ld
    ld="$(ip -o -d link show dev "$d" 2>/dev/null | head -n1)"
    if [ -d "$ROOT/sys/class/net/$d/bonding" ] || echo "$ld" | grep -qw 'bond'; then echo bond; return; fi
    if [ -d "$ROOT/sys/class/net/$d/bridge" ] || echo "$ld" | grep -qw 'bridge'; then echo bridge; return; fi
    if [ -f "$ROOT/proc/net/vlan/$d" ] || echo "$ld" | grep -qE '(^| )vlan (protocol|id) '; then echo vlan; return; fi
    if echo "$ld" | grep -qw 'team'; then echo team; return; fi
    if echo "$ld" | grep -qwE 'macvlan|macvtap|ipvlan|vxlan|geneve|tun|veth|dummy|gre|ipip|sit|vrf|wireguard|infiniband'; then echo other; return; fi
    if echo "$ld" | grep -q 'link/ether'; then echo ethernet; return; fi
    echo other
}

VL_PARENT=""; VL_ID=""
vlan_info()
{
    VL_PARENT="$(ip -o link show dev "$DEV" 2>/dev/null | head -n1 | awk '{ n = $2; sub(/:$/, "", n); if (index(n, "@")) { sub(/^[^@]*@/, "", n); print n } }')"
    VL_ID="$(ip -o -d link show dev "$DEV" 2>/dev/null | head -n1 | awk '{
        for (i = 1; i < NF; i++) if ($i == "vlan") for (j = i + 1; j < NF && j <= i + 3; j++) if ($j == "id") { print $(j + 1); exit }
    }')"
    if [ -f "$ROOT/proc/net/vlan/$DEV" ]; then
        [ -z "$VL_ID" ] && VL_ID="$(awk '{ for (i = 1; i < NF; i++) if ($i == "VID:") { print $(i + 1); exit } }' "$ROOT/proc/net/vlan/$DEV")"
        [ -z "$VL_PARENT" ] && VL_PARENT="$(awk '$1 == "Device:" { print $2; exit }' "$ROOT/proc/net/vlan/$DEV")"
    fi
    case "$DEV" in
        *.*)
            [ -z "$VL_PARENT" ] && VL_PARENT="${DEV%.*}"
            [ -z "$VL_ID" ] && VL_ID="${DEV##*.}"
            ;;
    esac
    [ -n "$VL_PARENT" ] && [ -n "$VL_ID" ]
}

gen_ifcfg_main()
{
    echo "DEVICE=$DEV"
    echo "NAME=$DEV"
    case "$1" in
        ethernet) echo "TYPE=Ethernet" ;;
        bond)
            echo "TYPE=Bond"
            echo "BONDING_MASTER=yes"
            echo "BONDING_OPTS=\"mode=$BOND_MODE miimon=100\""
            ;;
        vlan)
            echo "TYPE=Vlan"
            echo "VLAN=yes"
            echo "PHYSDEV=$VL_PARENT"
            echo "VLAN_ID=$VL_ID"
            ;;
    esac
    echo "ONBOOT=yes"
    echo "BOOTPROTO=none"
    echo "IPADDR=$NEW_IP"
    echo "NETMASK=$LIVE_MASK"
    echo "GATEWAY=$GATEWAY"
}

gen_ifcfg_port()
{
    echo "DEVICE=$1"
    echo "NAME=$1"
    echo "TYPE=Ethernet"
    echo "ONBOOT=yes"
    echo "BOOTPROTO=none"
    echo "MASTER=$DEV"
    echo "SLAVE=yes"
}

gen_kf_main()
{
    local t=ethernet
    [ "$1" = bond ] && t=bond
    [ "$1" = vlan ] && t=vlan
    echo "[connection]"
    echo "id=$DEV"
    echo "type=$t"
    echo "interface-name=$DEV"
    echo "autoconnect=true"
    echo ""
    if [ "$1" = bond ]; then
        echo "[bond]"
        echo "mode=$BOND_MODE"
        echo "miimon=100"
        echo ""
    fi
    if [ "$1" = vlan ]; then
        echo "[vlan]"
        echo "parent=$VL_PARENT"
        echo "id=$VL_ID"
        echo ""
    fi
    echo "[ipv4]"
    echo "method=manual"
    echo "address1=$NEW_IP/$CUR_PREFIX"
    echo "gateway=$GATEWAY"
    echo ""
    echo "[ipv6]"
    echo "method=ignore"
}

gen_kf_port()
{
    echo "[connection]"
    echo "id=$DEV-port-$1"
    echo "type=ethernet"
    echo "interface-name=$1"
    echo "controller=$DEV"
    echo "port-type=bond"
    echo "autoconnect=true"
}

# 새 파일 하나 쓰기: $1 경로, $2 형식, $3.. 내용 생성 함수와 인자
write_new()
{
    local path="$1" fmt="$2"
    shift 2
    backup_created "$path" || return 1
    if [ "$fmt" = keyfile ]; then
        ( umask 077; "$@" > "$path" ) || return 1
        chmod 600 "$path" || return 1
    else
        ( umask 022; "$@" > "$path" ) || return 1
    fi
}

# 만들 파일의 내용을 PLAN 줄로 출력 (-check)
print_plan()
{
    local path="$1"
    shift
    "$@" | while IFS= read -r l; do echo "PLAN|$(field "$(nodepath "$path")")|$(field "$l")"; done
}

NEW_KIND=""
create_new()
{
    local ndir nfmt main p ppath ports="" new_ports=""
    MODE=new-file
    NEW_KIND="$(dev_kind "$DEV")"
    case "$NEW_KIND" in
        ethernet|bond) ;;
        vlan) vlan_info || fail_out "VLAN 의 부모 장치나 ID 를 알 수 없어 새 프로필을 만들지 않았습니다" ;;
        *) fail_out "새 프로필을 만들 수 없는 장치 유형입니다 ($NEW_KIND). 아무것도 쓰지 않았습니다" ;;
    esac
    if [ -z "$OS_MAJOR" ]; then
        fail_out "OS 버전을 알 수 없어 새 프로필 형식을 정할 수 없습니다. 아무것도 쓰지 않았습니다"
    fi
    if [ "$OS_MAJOR" -ge 10 ] 2>/dev/null; then
        nfmt=keyfile; ndir="${FORCED:-$KEYFILE_DIR}"
        main="$ndir/$DEV.nmconnection"
    else
        nfmt=ifcfg; ndir="${FORCED:-$LEGACY_DIR}"
        main="$ndir/ifcfg-$DEV"
    fi
    if [ -e "$main" ]; then
        fail_out "만들 파일이 이미 있습니다 (다른 장치용으로 보임): $(nodepath "$main")"
    fi

    if [ "$NEW_KIND" = bond ]; then
        ports="$(cat "$ROOT/sys/class/net/$DEV/bonding/slaves" 2>/dev/null)"
        [ -z "$ports" ] && add_warn "bond 포트 목록을 읽지 못해 포트 파일을 만들지 않음"
        for p in $ports; do
            [ -n "$(scan_all "$p" "")" ] && continue
            if [ "$nfmt" = keyfile ]; then ppath="$ndir/$DEV-port-$p.nmconnection"; else ppath="$ndir/ifcfg-$p"; fi
            if [ -e "$ppath" ]; then
                fail_out "만들 포트 파일이 이미 있습니다: $(nodepath "$ppath")"
            fi
            new_ports="$new_ports $p"
        done
    fi

    TARGET_FILE="$main"; TARGET_FMT="$nfmt"

    if [ "$CHECK_ONLY" = 1 ]; then
        if [ "$nfmt" = keyfile ]; then
            print_plan "$main" gen_kf_main "$NEW_KIND"
        else
            print_plan "$main" gen_ifcfg_main "$NEW_KIND"
        fi
        for p in $new_ports; do
            if [ "$nfmt" = keyfile ]; then
                print_plan "$ndir/$DEV-port-$p.nmconnection" gen_kf_port "$p"
            else
                print_plan "$ndir/ifcfg-$p" gen_ifcfg_port "$p"
            fi
        done
        return 0
    fi

    mkdir -p "$ndir" || fail_out "디렉토리를 만들지 못했습니다: $(nodepath "$ndir")"
    if [ "$nfmt" = keyfile ]; then
        write_new "$main" keyfile gen_kf_main "$NEW_KIND" || { restore_all; fail_out "새 파일 쓰기 실패: $(nodepath "$main")"; }
    else
        write_new "$main" ifcfg gen_ifcfg_main "$NEW_KIND" || { restore_all; fail_out "새 파일 쓰기 실패: $(nodepath "$main")"; }
    fi
    for p in $new_ports; do
        if [ "$nfmt" = keyfile ]; then
            ppath="$ndir/$DEV-port-$p.nmconnection"
            write_new "$ppath" keyfile gen_kf_port "$p" || { restore_all; fail_out "새 파일 쓰기 실패: $(nodepath "$ppath")"; }
        else
            ppath="$ndir/ifcfg-$p"
            write_new "$ppath" ifcfg gen_ifcfg_port "$p" || { restore_all; fail_out "새 파일 쓰기 실패: $(nodepath "$ppath")"; }
        fi
    done
    [ -n "$new_ports" ] && add_note "포트 파일 생성:$new_ports"
    return 0
}

#------------------------------------------------------------------------------
# 9. 실행: 이미 바뀜 / 고치기 / 새로 만들기, 그리고 검증
#------------------------------------------------------------------------------

if [ -z "$TARGET_FILE" ]; then
    create_new
else
    TARGET_FMT="$(fmt_of "$TARGET_FILE")"
    load_state "$TARGET_FMT" "$TARGET_FILE"
    OLD_FILE_GW="$S_GW"
    case "$S_METHOD" in
        static|dhcp) ;;
        *) fail_out "지원하지 않는 주소 방식입니다 ($S_METHOD). 아무것도 쓰지 않았습니다" ;;
    esac
    if state_has_new; then
        MODE=already
    else
        plan_edit
        if [ "$CHECK_ONLY" = 1 ]; then
            printf '%s' "$SETS" | while IFS= read -r l; do
                [ -n "$l" ] && echo "PLAN|$(field "$(nodepath "$TARGET_FILE")")|$(field "$l")"
            done
        else
            backup_modified "$TARGET_FILE" || fail_out "백업 실패: $(nodepath "$TARGET_FILE")"
            apply_edit || { restore_all; fail_out "쓰기 실패, 백업으로 되돌림: $(nodepath "$TARGET_FILE")"; }
        fi
    fi
fi

# 쓴 뒤 다시 읽어 검증. 틀리면 되돌리고 FAIL
if [ "$CHECK_ONLY" != 1 ] && [ "$MODE" != already ]; then
    load_state "$TARGET_FMT" "$TARGET_FILE"
    if ! state_has_new; then
        restore_all
        fail_out "검증 실패, 백업으로 되돌림 (IP=$S_IP 게이트웨이=$S_GW 방식=$S_METHOD 넷마스크=${S_PREFIX:-없음})"
    fi
fi

#------------------------------------------------------------------------------
# 10. 재부팅 후 유지 점검 (4.7). 하나라도 걸리면 WARN
#------------------------------------------------------------------------------

# 3) 부팅 시 시작
if [ -f "$TARGET_FILE" ]; then
    load_state "$TARGET_FMT" "$TARGET_FILE"
    [ "$S_BOOT" = no ] && add_warn "부팅 시 시작하지 않는 프로필 (ONBOOT=no 또는 autoconnect=false)"
fi

# 4) 경쟁 프로필: 같은 장치의 다른 파일 중 부팅 시 시작하는 것 (설정 디렉토리의 옛 .bak.* 포함)
COMP=""
while IFS= read -r f; do
    [ -z "$f" ] && continue
    [ "$f" = "$TARGET_FILE" ] && continue
    boot_on "$f" && COMP="${COMP:+$COMP,}$(nodepath "$f")"
done <<< "$MATCHES"
TDIR="$(dirname "$TARGET_FILE")"
SEEN_DIRS=""
for dir in "$TDIR" "$LEGACY_DIR" "$KEYFILE_DIR"; do
    case " $SEEN_DIRS " in *" $dir "*) continue ;; esac
    SEEN_DIRS="${SEEN_DIRS:-} $dir"
    [ -d "$dir" ] || continue
    for f in "$dir"/*; do
        [ -f "$f" ] || continue
        name="${f##*/}"
        case "$name" in *.bak.*) ;; *) continue ;; esac
        case "$name" in
            ifcfg-*) ifcfg_is_for "$f" "$DEV" "$MAC" "${name%%.bak*}" || continue ;;
            *) kf_is_for "$f" "$DEV" "$MAC" || continue ;;
        esac
        boot_on "$f" && COMP="${COMP:+$COMP,}$(nodepath "$f")"
    done
done
[ -n "$COMP" ] && add_warn "경쟁 프로필: $COMP"

# 5) 링크 상태
[ "$LINK_OK" = 1 ] || add_warn "link down"

# 새 bond 프로필의 모드와 현재 bond 모드 비교
if [ "$MODE" = new-file ] && [ "$NEW_KIND" = bond ]; then
    live_mode="$(awk '{print $1; exit}' "$ROOT/sys/class/net/$DEV/bonding/mode" 2>/dev/null)"
    if [ -n "$live_mode" ] && [ "$live_mode" != "$BOND_MODE" ]; then
        add_warn "bond 모드 불일치 (현재 $live_mode, 파일 $BOND_MODE)"
    fi
fi

# 6) 예전 게이트웨이: 바꾸기 전 파일의 게이트웨이, 없으면 장치의 현재 기본 경로 게이트웨이
OLD_GW=""
if [ -n "$OLD_FILE_GW" ] && [ "$OLD_FILE_GW" != "$GATEWAY" ]; then
    OLD_GW="$OLD_FILE_GW"
elif [ -n "$LIVE_GW" ] && [ "$LIVE_GW" != "$GATEWAY" ]; then
    OLD_GW="$LIVE_GW"
fi
GWFIX=""
if [ -n "$OLD_GW" ]; then
    nf="$ROOT/etc/sysconfig/network"
    if [ -f "$nf" ] && [ "$(dget "$(ifcfg_dump "$nf")" GATEWAY)" = "$OLD_GW" ]; then
        GWFIX="$(nodepath "$nf")"
    fi
    for rf in "$LEGACY_DIR/route-$DEV" "$TDIR/route-$DEV"; do
        case ",$GWFIX," in *",$(nodepath "$rf"),"*) continue ;; esac
        if [ -f "$rf" ] && grep -v '^[[:space:]]*#' "$rf" | grep -qwF "$OLD_GW"; then
            GWFIX="${GWFIX:+$GWFIX,}$(nodepath "$rf")"
        fi
    done
    if [ -n "$GWFIX" ]; then
        add_warn "예전 게이트웨이($OLD_GW)가 남아 있음"
        add_note "OLDGW=$OLD_GW"
        add_note "GWFIX=$GWFIX"
    fi
fi

if [ "$NM_RUN" = 1 ]; then
    add_note "재부팅 전 nmcli 로 이 프로필을 건드리지 마십시오"
fi
if [ "$BACKED_UP" = 1 ]; then
    add_note "BACKUP=$STAMP"
fi

if [ "$CHECK_ONLY" = 1 ]; then
    emit CHECK
elif [ -n "$WARN_TXT" ]; then
    emit WARN
else
    emit OK
fi
exit 0
