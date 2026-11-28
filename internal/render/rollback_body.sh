###############################################################################
# 롤백 본체. (Go 엔진이 위 헤더에 NETWORK_SCRIPTS_DIR / RHEL9_PATH / ROLLBACK_TO 를 붙입니다)
#
# 동작 (수정계획서 4.9)
#   [새 방식] /var/lib/ip_change/backup/<STAMP>/ 이 있으면
#     1. 가장 최근 STAMP 디렉토리(ROLLBACK_TO 가 있으면 그 STAMP)를 고름.
#        이미 롤백한 STAMP 는 ROLLED_BACK 표시 파일이 있어 최근 선택에서 건너뜀
#        (ROLLBACK_TO 로 롤백된 STAMP 를 지정하면 FAIL)
#     2. manifest 의 각 줄 처리
#          modified <경로> : 백업 파일을 원래 경로로 복사 (cp -p 로 권한/소유자 유지)
#          created  <경로> : 만들어진 파일을 STAMP 디렉토리 안으로 "<이름>.removed" 로 옮김
#     3. 다시 읽어 확인 (복원본은 백업과 비교, 만든 파일은 없어졌는지 확인)
#   [옛 방식] STAMP 디렉토리가 하나도 없으면 (ROLLBACK_TO 가 있으면 그 STAMP 디렉토리가 없으면)
#     1. OS 버전으로 검색 경로 결정 (apply_body.sh 와 동일한 규칙)
#     2. 그 경로의 ifcfg-* 중 "<ifcfg>.bak.<STAMP>" 백업이 있는 파일을 찾음
#          - ROLLBACK_TO 가 지정되면 그 STAMP 백업만, 아니면 STAMP 가 가장 큰(최근) 백업
#     3. 최신 STAMP 를 가진 ifcfg 가 둘 이상이면 실패 (-rollback-to 로 지정)
#     4. 백업 내용을 원래 ifcfg 로 되돌리고 IPADDR 로 검증. 백업 파일(.bak.*)은 남김
#   네트워크 서비스 재시작, nmcli 쓰기 명령은 하지 않습니다.
#
# 출력 형식 (한 줄, gossh 가 "<host>: " 를 앞에 붙임. 필드 값에는 "|" 가 들어가지 않음)
#   [새 방식] 성공: RESULT|OK|<hostname>|<STAMP>|<복원한 파일 수>|<옮겨 지운 파일 수>|<비고>
#             비고 예: "복원=/etc/a,/etc/b; 제거=/etc/c" (없으면 빈 칸)
#   [옛 방식] 성공: RESULT|OK|<hostname>|<ifcfg>|<백업STAMP>|<복원된IPADDR>
#   실패(공통): RESULT|FAIL|<hostname>|<사유>
#   두 방식은 3번째 칸 뒤의 의미가 다릅니다. 새 방식은 4번째 칸이 STAMP(숫자),
#   옛 방식은 4번째 칸이 "/" 로 시작하는 파일 경로입니다.
#
# 테스트: ROOT 환경변수(운영에서는 비어 있음)를 노드의 모든 파일 경로 앞에 붙입니다.
###############################################################################

NODE_HOST="$(hostname -s 2>/dev/null || hostname)"

ROOT="${ROOT:-}"
NETWORK_SCRIPTS_DIR="${NETWORK_SCRIPTS_DIR:-}"
RHEL9_PATH="${RHEL9_PATH:-}"
ROLLBACK_TO="${ROLLBACK_TO:-}"

BACKUP_BASE="$ROOT/var/lib/ip_change/backup"

# 결과 필드 값에서 구분자 "|" 와 줄바꿈을 없앱니다
field() { printf '%s' "$1" | tr '|\r\n\t' '/   '; }

fail_out()
{
    echo "RESULT|FAIL|$(field "$NODE_HOST")|$(field "$1")"
    exit 1
}

detect_os_major()
{
    local v=""
    if [ -f "$ROOT/etc/redhat-release" ]; then
        v="$(sed -n 's/.*release \([0-9][0-9]*\).*/\1/p' "$ROOT/etc/redhat-release" | head -n1)"
    fi
    if [ -z "$v" ] && [ -f "$ROOT/etc/os-release" ]; then
        v="$(sed -n 's/^VERSION_ID="\{0,1\}\([0-9][0-9]*\).*/\1/p' "$ROOT/etc/os-release" | head -n1)"
    fi
    echo "$v"
}

# ROLLBACK_TO 는 숫자만 허용합니다 (경로 조작 방지)
case "$ROLLBACK_TO" in *[!0-9]*) fail_out "ROLLBACK_TO 는 숫자 STAMP 여야 합니다: $ROLLBACK_TO" ;; esac

#------------------------------------------------------------------------------
# 새 방식
#------------------------------------------------------------------------------

# 롤백할 STAMP 를 고릅니다. 없으면 빈 문자열
pick_stamp()
{
    local d s best=""
    if [ -n "$ROLLBACK_TO" ]; then
        [ -d "$BACKUP_BASE/$ROLLBACK_TO" ] && echo "$ROLLBACK_TO"
        return 0
    fi
    for d in "$BACKUP_BASE"/*; do
        [ -d "$d" ] || continue
        s="${d##*/}"
        case "$s" in ''|*[!0-9]*) continue ;; esac
        [ ! -f "$d/ROLLED_BACK" ] || continue     # 이미 롤백한 STAMP 는 건너뜀
        if [ -z "$best" ] || [ "$s" -gt "$best" ] 2>/dev/null; then best="$s"; fi
    done
    echo "$best"
}

rollback_new()
{
    local stamp="$1" bk="$BACKUP_BASE/$1" mf="$BACKUP_BASE/$1/manifest"
    local act p n_rest=0 n_rem=0 rest_list="" rem_list="" miss_list="" note=""

    [ -f "$mf" ] || fail_out "manifest 가 없습니다: $bk"

    # 1차: 먼저 모든 항목을 점검합니다 (중간에 실패해서 반쯤 되돌려지는 일을 줄임)
    while read -r act p; do
        [ -n "$act" ] || continue
        case "$p" in /*) ;; *) fail_out "manifest 경로가 올바르지 않습니다: $p" ;; esac
        case "$p" in *..*) fail_out "manifest 경로가 올바르지 않습니다: $p" ;; esac
        case "$act" in
            modified) [ -f "$bk$p" ] || fail_out "백업 파일이 없습니다: $bk$p" ;;
            created)  ;;
            *) fail_out "manifest 동작을 알 수 없습니다: $act" ;;
        esac
    done < "$mf"

    # 2차: 실제 되돌리기
    while read -r act p; do
        [ -n "$act" ] || continue
        case "$act" in
            modified)
                cp -p "$bk$p" "$ROOT$p" || fail_out "복원 실패: $p"
                n_rest=$((n_rest + 1)); rest_list="${rest_list:+$rest_list,}$p"
                ;;
            created)
                if [ -e "$ROOT$p" ]; then
                    mkdir -p "$bk$(dirname "$p")" || fail_out "백업 디렉토리 생성 실패: $bk"
                    mv -f "$ROOT$p" "$bk$p.removed" || fail_out "만든 파일 제거 실패: $p"
                    n_rem=$((n_rem + 1)); rem_list="${rem_list:+$rem_list,}$p"
                else
                    miss_list="${miss_list:+$miss_list,}$p"
                fi
                ;;
        esac
    done < "$mf"

    # 3차: 다시 읽어 확인
    while read -r act p; do
        [ -n "$act" ] || continue
        case "$act" in
            modified) cmp -s "$bk$p" "$ROOT$p" || fail_out "복원 검증 실패: $p" ;;
            created)  [ ! -e "$ROOT$p" ] || fail_out "만든 파일이 남아 있습니다: $p" ;;
        esac
    done < "$mf"

    # 롤백 완료 표시 (다음 -rollback 이 같은 STAMP 를 다시 고르지 않도록)
    : > "$bk/ROLLED_BACK" 2>/dev/null

    [ -n "$rest_list" ] && note="복원=$rest_list"
    [ -n "$rem_list" ] && note="${note:+$note; }제거=$rem_list"
    [ -n "$miss_list" ] && note="${note:+$note; }이미 없음=$miss_list"
    echo "RESULT|OK|$(field "$NODE_HOST")|$stamp|$n_rest|$n_rem|$(field "$note")"
    exit 0
}

NEW_STAMP="$(pick_stamp)"
# (명령 치환 안에서는 exit 가 결과 줄을 가로채므로 여기서 확인합니다)
if [ -n "$ROLLBACK_TO" ] && [ -f "$BACKUP_BASE/$ROLLBACK_TO/ROLLED_BACK" ]; then
    fail_out "STAMP=$ROLLBACK_TO 는 이미 롤백했습니다 (다시 되돌릴 수 없습니다)"
fi
if [ -n "$NEW_STAMP" ]; then
    rollback_new "$NEW_STAMP"
fi

#------------------------------------------------------------------------------
# 옛 방식: <ifcfg>.bak.<STAMP>
#------------------------------------------------------------------------------

SEARCH_DIR="$ROOT$NETWORK_SCRIPTS_DIR"
OS_MAJOR="$(detect_os_major)"
if [ -n "$RHEL9_PATH" ] && [ -n "$OS_MAJOR" ] && [ "$OS_MAJOR" -ge 9 ] 2>/dev/null; then
    SEARCH_DIR="$ROOT$RHEL9_PATH"
fi

if [ ! -d "$SEARCH_DIR" ]; then
    fail_out "설정 경로가 없습니다: $SEARCH_DIR"
fi

# 백업 후보 수집  (STAMP<TAB>ifcfg<TAB>bak)
CAND="$(mktemp)" || fail_out "임시파일 생성 실패"
trap 'rm -f "$CAND"' EXIT

for f in "$SEARCH_DIR"/ifcfg-*; do
    [ -f "$f" ] || continue
    case "$f" in *.bak.*) continue ;; esac
    for b in "$f".bak.*; do
        [ -f "$b" ] || continue
        stamp="${b##*.bak.}"
        case "$stamp" in ''|*[!0-9]*) continue ;; esac
        if [ -n "$ROLLBACK_TO" ] && [ "$stamp" != "$ROLLBACK_TO" ]; then
            continue
        fi
        printf '%s\t%s\t%s\n' "$stamp" "$f" "$b" >>"$CAND"
    done
done

if [ ! -s "$CAND" ]; then
    if [ -n "$ROLLBACK_TO" ]; then
        fail_out "STAMP=$ROLLBACK_TO 백업을 찾지 못했습니다: $BACKUP_BASE, $SEARCH_DIR"
    fi
    fail_out "복원할 백업이 없습니다: $BACKUP_BASE, $SEARCH_DIR(.bak.*)"
fi

TOP_STAMP="$(cut -f1 "$CAND" | sort -rn | head -n1)"
N="$(awk -F'\t' -v s="$TOP_STAMP" '$1==s' "$CAND" | wc -l)"
if [ "$N" -ne 1 ]; then
    fail_out "가장 최근 백업 STAMP($TOP_STAMP)를 가진 ifcfg 가 여러 개입니다 — -rollback-to 로 지정하십시오"
fi

LINE="$(awk -F'\t' -v s="$TOP_STAMP" '$1==s' "$CAND")"
IFCFG="$(printf '%s\n' "$LINE" | cut -f2)"
BAK="$(printf '%s\n' "$LINE" | cut -f3)"

# 복원 + 검증
cat "$BAK" > "$IFCFG" || fail_out "복원 쓰기 실패: $IFCFG"

now_ip="$(grep -i '^[[:space:]]*IPADDR[[:space:]]*=' "$IFCFG" | tail -n1 | cut -d= -f2- | tr -d '"'"'"' \t')"
bak_ip="$(grep -i '^[[:space:]]*IPADDR[[:space:]]*=' "$BAK"   | tail -n1 | cut -d= -f2- | tr -d '"'"'"' \t')"

if [ "$now_ip" != "$bak_ip" ]; then
    fail_out "복원 검증 실패 ($IFCFG 의 IPADDR=$now_ip, 기대=$bak_ip)"
fi

echo "RESULT|OK|$(field "$NODE_HOST")|$IFCFG|$TOP_STAMP|$now_ip"
exit 0
