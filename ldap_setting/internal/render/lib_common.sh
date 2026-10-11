###############################################################################
# 공통 라이브러리 — apply_body.sh 와 rollback_body.sh 양쪽에서 씁니다.
#
# 서비스 재시작 표가 두 곳으로 갈라지면 반드시 어긋나므로 여기 한 곳에만 둡니다.
#
# 환경변수
#   ROOT   기록 대상 루트. 기본 "" (= 실제 /etc). 테스트 시 /tmp/fixture 등을 넣음
#   DRYRUN 1 이면 아무것도 바꾸지 않고 무엇이 바뀔지만 보고
###############################################################################

ROOT="${ROOT:-}"
DRYRUN="${DRYRUN:-0}"

CHANGED=""
FAILED=0

log()  { echo "APPLY|$1|$2"; }
fail() { echo "APPLY|FAIL|$1"; FAILED=1; }

changed_has()
{
    case " $CHANGED " in
        *" $1 "*) return 0 ;;
    esac
    return 1
}

# 같은 파일을 여러 번 손대도 목록에는 한 번만 남깁니다.
mark_changed()
{
    changed_has "$1" || CHANGED="$CHANGED $1"
}

#------------------------------------------------------------------------------
# 이 노드 정보 판정
#------------------------------------------------------------------------------

NODE_HOST="$(hostname -s 2>/dev/null || hostname)"

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

#------------------------------------------------------------------------------
# 서비스 재시작
#
#   ldap.conf / resolv.conf 는 대응 서비스가 없습니다(라이브러리·리졸버 설정).
#------------------------------------------------------------------------------

restart_svc()
{
    local svc="$1"
    if [ "$DRYRUN" = "1" ]; then
        log WOULD-RESTART "$svc"
        return 0
    fi
    # ROOT 가 지정된 테스트 모드에서는 실제 서비스를 절대 건드리지 않습니다.
    # (fixture 디렉터리에 쓰면서 이 노드의 진짜 sssd 를 재시작하면 안 됩니다)
    if [ -n "$ROOT" ]; then
        log SKIP-RESTART "$svc (ROOT=$ROOT 테스트 모드)"
        return 0
    fi
    if ! systemctl is-enabled "$svc" >/dev/null 2>&1 && ! systemctl is-active "$svc" >/dev/null 2>&1; then
        log SKIP-RESTART "$svc (미설치 또는 비활성)"
        return 0
    fi
    if systemctl restart "$svc" >/dev/null 2>&1; then
        log RESTARTED "$svc"
    else
        fail "서비스 재시작 실패: $svc"
    fi
}

# CHANGED 목록을 보고 대응 서비스만 재시작합니다.
restart_for_changed()
{
    local need_autofs=0

    changed_has /etc/autofs.conf            && need_autofs=1
    changed_has /etc/autofs_ldap_auth.conf  && need_autofs=1
    changed_has /etc/auto.appl              && need_autofs=1

    if changed_has /etc/nslcd.conf; then
        restart_svc nslcd
    fi

    if changed_has /etc/sssd/sssd.conf; then
        if [ "$DRYRUN" != "1" ] && [ -z "$ROOT" ]; then
            sss_cache -E >/dev/null 2>&1
        fi
        restart_svc sssd
    fi

    if [ "$need_autofs" = "1" ]; then
        restart_svc autofs
    fi

    if changed_has /etc/ntp.conf; then
        restart_svc ntpd
    fi

    if changed_has /etc/chrony.conf; then
        restart_svc chronyd
    fi
}
