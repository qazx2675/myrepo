#!/usr/bin/env bash
# install.sh — vm_power_onoff 와 cross_check(교차검증 데몬)를 명령처럼 쓰도록 설치한다 (root 로 실행, 여러 번 실행해도 됨).
#   /usr/local/bin/vm_power_onoff           실행파일 (같은 폴더의 vm_power_onoff, 없으면 setup.sh 로 먼저 빌드)
#   /etc/vcetner_pwon_off/pwonoff.conf      설정 파일 (이미 있으면 덮어쓰지 않음)
#   /usr/local/bin/cross_check              실행파일 (cross_check/bin/cross_check, 없으면 setup.sh 로 먼저 빌드)
#   /etc/cross_check/cross_check.conf       데몬 설정 (없을 때만 생성: target_dir=allowed_dir, users=pwonoff.conf 의 users)
#   /etc/cross_check/auth.conf              user=패스워드 (0600, 없을 때만 생성: admin=<무작위 16자>, 그때 한 번만 화면 출력)
#   /var/lib/cross_check (0700)             작업 기록,  /etc/tmpfiles.d/cross_check.conf  (/run/cross_check 재부팅 후 생성)
#   root crontab                            * * * * * /usr/local/bin/cross_check --ensure  (데몬이 죽어 있으면 매분 다시 띄움)
# 설정 디렉터리 이름은 요구사항 그대로 'vcetner' 철자를 쓴다.
# 사용법:
#   sudo bash install.sh [허용디렉터리]      # 허용디렉터리 = conf 의 allowed_dir (기본: 이 폴더의 절대경로)
#   sudo bash install.sh --enable-cross-check [허용디렉터리]   # pwonoff.conf 의 cross_check=on 으로 (기본은 그대로 둠)
#   sudo bash install.sh --skip-vm-power-onoff [허용디렉터리]  # vm_power_onoff 실행파일은 설치하지 않음 (cross_check 만)
#   sudo bash install.sh -u                   # 제거: 실행파일·crontab 줄 삭제, 데몬 중지 (설정·비밀번호·작업 기록은 남김)
# 시험용 경로 바꾸기(기본은 실제 위치): CC_ETC CC_STATE CC_BIN_DIR CC_SOCKET_DIR CC_PWONOFF_DIR CC_TMPFILES_DIR CC_LOG
#   CC_CRON_BIN(crontab 에 적을 실행파일, 기본 $CC_BIN_DIR/cross_check)  CC_SKIP_CRON=1  CC_SKIP_START=1
set -euo pipefail
cd "$(dirname "$0")"
HERE="$(pwd -P)"

CC_BIN_DIR="${CC_BIN_DIR:-/usr/local/bin}"
CC_ETC="${CC_ETC:-/etc/cross_check}"
CC_STATE="${CC_STATE:-/var/lib/cross_check}"
CC_SOCKET_DIR="${CC_SOCKET_DIR:-/var/run/cross_check}"
CC_PWONOFF_DIR="${CC_PWONOFF_DIR:-/etc/vcetner_pwon_off}"
CC_TMPFILES_DIR="${CC_TMPFILES_DIR:-/etc/tmpfiles.d}"
CC_LOG="${CC_LOG:-}"
CC_SKIP_CRON="${CC_SKIP_CRON:-0}"
CC_SKIP_START="${CC_SKIP_START:-0}"

BIN_DST="$CC_BIN_DIR/vm_power_onoff"
CONF_DIR="$CC_PWONOFF_DIR"
CONF="$CONF_DIR/pwonoff.conf"
CC_SRC="$HERE/cross_check/bin/cross_check"
CC_DST="$CC_BIN_DIR/cross_check"
CC_CRON_BIN="${CC_CRON_BIN:-$CC_DST}"
CC_CONF="$CC_ETC/cross_check.conf"
CC_AUTH="$CC_ETC/auth.conf"
CC_SOCK="$CC_SOCKET_DIR/cross_check.sock"
CC_CRON_SH="$HERE/cross_check/scripts/cc_cron.sh"
CC_PW_SH="$HERE/cross_check/scripts/cc_admin_pw.sh"

[ "$(id -u)" -eq 0 ] || { echo "[오류] root 로 실행하세요 (sudo bash install.sh)" >&2; exit 1; }

UNINSTALL=0
ENABLE_CC=0
SKIP_VM=0
ALLOWED=""
while [ $# -gt 0 ]; do
  case "$1" in
    -u) UNINSTALL=1 ;;
    --enable-cross-check) ENABLE_CC=1 ;;
    --skip-vm-power-onoff) SKIP_VM=1 ;;
    -h|--help) sed -n '2,19p' "$0" | sed 's/^# \{0,1\}//'; exit 0 ;;
    -*) echo "[오류] 알 수 없는 옵션: $1 (bash install.sh -h)" >&2; exit 1 ;;
    *)
      [ -z "$ALLOWED" ] || { echo "[오류] 허용디렉터리는 하나만 지정합니다: $1" >&2; exit 1; }
      ALLOWED="$1" ;;
  esac
  shift
done

# 데몬에 넘길 경로 환경변수 (기본 위치가 아닐 때만 — 시험용)
cc_env() {
  local e=()
  [ "$CC_ETC" != /etc/cross_check ] && e+=("CROSS_CHECK_CONF=$CC_CONF" "CROSS_CHECK_AUTH=$CC_AUTH")
  [ "$CC_STATE" != /var/lib/cross_check ] && e+=("CROSS_CHECK_STATE=$CC_STATE/jobs.json")
  [ "$CC_SOCKET_DIR" != /var/run/cross_check ] && e+=("CROSS_CHECK_SOCKET=$CC_SOCK")
  [ -n "$CC_LOG" ] && e+=("CROSS_CHECK_LOG=$CC_LOG")
  env ${e[@]+"${e[@]}"} "$@"
}

# 데몬 = 이름이 정확히 cross_check(pgrep -x) 이고 인자에 --daemon 이 있는 프로세스.
# (같은 이름의 사용자 화면 cross_check --user 등은 건드리지 않는다)
daemon_pids() {
  local p
  for p in $(pgrep -x cross_check 2>/dev/null || true); do
    case " $(ps -o args= -p "$p" 2>/dev/null || true) " in *" --daemon "*) echo "$p" ;; esac
  done
}
daemon_running() { [ -n "$(daemon_pids)" ]; }

stop_daemon() {
  local pids i=0
  pids="$(daemon_pids)"
  [ -n "$pids" ] || return 0
  # shellcheck disable=SC2086  # pid 목록을 단어로 나눠 넘긴다
  kill $pids 2>/dev/null || true
  while daemon_running && [ "$i" -lt 25 ]; do sleep 0.2; i=$((i + 1)); done
  pids="$(daemon_pids)"
  if [ -n "$pids" ]; then
    # shellcheck disable=SC2086
    kill -9 $pids 2>/dev/null || true
    sleep 0.2
  fi
  if daemon_running; then echo "[경고] cross_check 데몬이 종료되지 않습니다 (pgrep -x cross_check)" >&2; return 1; fi
  echo "[완료] cross_check 데몬 중지"
}

cron_sh() { CC_BIN="$CC_CRON_BIN" bash "$CC_CRON_SH" "$@"; }

if [ "$UNINSTALL" -eq 1 ]; then
  rm -f "$BIN_DST" && echo "[완료] $BIN_DST 삭제 (설정 $CONF_DIR 는 남겨 둠)"
  if [ "$CC_SKIP_CRON" = 1 ]; then
    echo "[INFO] CC_SKIP_CRON=1 — crontab 은 건드리지 않음"
  elif ! cron_sh remove; then
    echo "[경고] crontab 의 cross_check 줄을 지우지 못했습니다 — crontab -e 로 직접 지우십시오" >&2
  fi
  stop_daemon || true
  rm -f "$CC_DST" && echo "[완료] $CC_DST 삭제 (설정 $CC_ETC, 작업 기록 $CC_STATE 는 남겨 둠)"
  exit 0
fi

ALLOWED="${ALLOWED:-$HERE}"
case "$ALLOWED" in /*) ;; *) echo "[오류] 허용디렉터리는 절대경로여야 합니다: $ALLOWED" >&2; exit 1 ;; esac

# 실행파일이 없으면 setup.sh 로 먼저 빌드 (setup.sh 는 --no-install 이라 여기로 다시 들어오지 않는다)
NEED_BUILD=0
[ "$SKIP_VM" -eq 0 ] && [ ! -x ./vm_power_onoff ] && NEED_BUILD=1
[ ! -x "$CC_SRC" ] && NEED_BUILD=1
if [ "$NEED_BUILD" -eq 1 ]; then
  echo "[INFO] 실행파일이 없어 setup.sh 로 먼저 빌드합니다"
  if [ "$SKIP_VM" -eq 1 ]; then bash ./setup.sh --no-install --skip-vm-power-onoff; else bash ./setup.sh --no-install; fi
fi

# ── vm_power_onoff ─────────────────────────────────────────────
if [ "$SKIP_VM" -eq 1 ]; then
  echo "[INFO] --skip-vm-power-onoff: $BIN_DST 는 설치하지 않습니다"
else
  install -d -m 755 "$CC_BIN_DIR"
  install -m 755 -o root -g root ./vm_power_onoff "$BIN_DST"
  echo "[완료] $BIN_DST"
fi

install -d -m 755 -o root -g root "$CONF_DIR"
if [ -e "$CONF" ]; then
  echo "[INFO] $CONF 가 이미 있어 덮어쓰지 않습니다 (예시: $HERE/pwonoff.conf.example)"
else
  sed "s#^allowed_dir=.*#allowed_dir=$ALLOWED#" pwonoff.conf.example > "$CONF.tmp"
  install -m 644 -o root -g root "$CONF.tmp" "$CONF"; rm -f "$CONF.tmp"
  echo "[완료] $CONF (allowed_dir=$ALLOWED) — users/vcenters 등을 환경에 맞게 고치세요"
fi
if [ ! -d "$ALLOWED" ]; then
  mkdir -p "$ALLOWED" && echo "[완료] 허용디렉터리 생성: $ALLOWED"
fi
echo "[INFO] 대상 파일은 $ALLOWED/on_off_<user>.txt 에 두고, 그 디렉터리에서 vm_power_onoff 를 실행합니다."

# pwonoff.conf 의 cross_check 키: --enable-cross-check 일 때만 on 으로 (그 외에는 그대로)
CC_STATE_KEY="$(sed -n 's/^cross_check=\([^#[:space:]]*\).*/\1/p' "$CONF" | tr -d '\r' | head -1)"
if [ "$ENABLE_CC" -eq 1 ]; then
  if [ "$CC_STATE_KEY" = on ]; then
    echo "[INFO] $CONF 에 이미 cross_check=on"
  elif grep -q '^cross_check=' "$CONF"; then
    sed -i 's/^cross_check=.*/cross_check=on/' "$CONF"
    echo "[완료] $CONF : cross_check=on (교차검증 사용)"
  else
    printf 'cross_check=on\n' >> "$CONF"
    echo "[완료] $CONF : cross_check=on 줄 추가 (교차검증 사용)"
  fi
  CC_STATE_KEY=on
fi

# ── cross_check ────────────────────────────────────────────────
if [ ! -x "$CC_SRC" ]; then
  echo "[경고] $CC_SRC 가 없어 cross_check(교차검증)는 설치하지 않았습니다." >&2
  echo "       OS6 이면 Go 1.20 빌드 서버에서 bash build_os6.sh 로 bin_os6/cross_check.gz 를 만든 뒤 bash setup.sh 를 다시 실행하십시오." >&2
  exit 0
fi

install -d -m 755 "$CC_BIN_DIR"
CC_CHANGED=1
if [ -f "$CC_DST" ] && cmp -s "$CC_SRC" "$CC_DST"; then CC_CHANGED=0; fi
install -m 755 -o root -g root "$CC_SRC" "$CC_DST"
if [ "$CC_CHANGED" -eq 1 ]; then echo "[완료] $CC_DST"; else echo "[INFO] $CC_DST 는 이미 최신"; fi

install -d -m 755 -o root -g root "$CC_ETC"
install -d -m 700 -o root -g root "$CC_STATE"
echo "[완료] $CC_ETC (0755), $CC_STATE (0700)"

if [ -d "$CC_TMPFILES_DIR" ]; then
  RUNDIR="$CC_SOCKET_DIR"
  case "$RUNDIR" in /var/run/*) RUNDIR="/run/${RUNDIR#/var/run/}" ;; esac
  TMPF="$CC_TMPFILES_DIR/cross_check.conf"
  TMPL="d $RUNDIR 0755 root root -"
  if [ -f "$TMPF" ] && [ "$(cat "$TMPF")" = "$TMPL" ]; then
    echo "[INFO] $TMPF 이미 있음"
  else
    printf '%s\n' "$TMPL" > "$TMPF.tmp" && chmod 644 "$TMPF.tmp" && mv -f "$TMPF.tmp" "$TMPF"
    echo "[완료] $TMPF ($TMPL)"
  fi
fi

# cross_check.conf — 없을 때만 (users 는 설치된 pwonoff.conf, 없으면 예시에서)
if [ -e "$CC_CONF" ]; then
  echo "[INFO] $CC_CONF 가 이미 있어 덮어쓰지 않습니다"
else
  USERS_SRC="$CONF"; [ -f "$USERS_SRC" ] || USERS_SRC="$HERE/pwonoff.conf.example"
  CC_USERS="$(sed -n 's/^users=//p' "$USERS_SRC" | head -1 | sed 's/#.*//' | tr -d ' \t\r')"
  # target_dir = vm_power_onoff 가 실제로 쓰는 allowed_dir (설치된 pwonoff.conf 값, 없으면 이번 허용디렉터리)
  CC_TARGET="$(sed -n 's/^allowed_dir=//p' "$CONF" 2>/dev/null | head -1 | sed 's/#.*//' | tr -d ' \t\r')"
  case "$CC_TARGET" in /*) ;; *) CC_TARGET="$ALLOWED" ;; esac
  cat > "$CC_CONF.tmp" <<EOF
# $CC_CONF — cross_check 데몬 설정 (install.sh 가 처음 한 번만 만든다. 이후에는 덮어쓰지 않음)
# 형식: 키=값, '#' 주석. 모르는 키·중복 키는 데몬 시작 오류.

# 작업자의 대상 파일 <target_dir>/on_off_<user>.txt 위치 (vm_power_onoff 의 allowed_dir 과 같게)
target_dir=$CC_TARGET

# 등록·작업이 허용된 사용자 (관리자가 관리, 쉼표 구분). 처음 값은 $USERS_SRC 의 users
users=$CC_USERS

# 승인·취소·관리자 기능에 패스워드 요구 (required | off)
auth=required

# 승인 후 이 시간(초) 안에 본 작업(--start)을 시작하지 않으면 승인 취소
approval_ttl_sec=120
EOF
  install -m 644 -o root -g root "$CC_CONF.tmp" "$CC_CONF"; rm -f "$CC_CONF.tmp"
  echo "[완료] $CC_CONF (target_dir=$CC_TARGET, users=$CC_USERS)"
fi

# auth.conf — 없을 때만. admin 패스워드는 만들 때 한 번만 화면에 보인다.
if [ -e "$CC_AUTH" ]; then
  if [ -n "$(find "$CC_AUTH" -perm /077 2>/dev/null)" ] || [ "$(stat -c %u "$CC_AUTH")" != 0 ]; then
    chown root:root "$CC_AUTH"; chmod 600 "$CC_AUTH"
    echo "[완료] $CC_AUTH 권한을 0600 root:root 로 조정"
  fi
  grep -q '^admin=' "$CC_AUTH" || echo "[경고] $CC_AUTH 에 admin= 줄이 없습니다 — 관리자 기능(--admin)을 쓸 수 없습니다" >&2
  echo "[INFO] $CC_AUTH 가 이미 있어 그대로 둡니다 (관리자 패스워드 분실 시 이 파일의 admin= 줄 확인)"
else
  ADMIN_PW="$(bash "$CC_PW_SH" 16)"
  ( umask 077; printf 'admin=%s\n' "$ADMIN_PW" > "$CC_AUTH.tmp" )
  chown root:root "$CC_AUTH.tmp"; chmod 600 "$CC_AUTH.tmp"; mv -f "$CC_AUTH.tmp" "$CC_AUTH"
  echo
  echo "  ################################################################"
  echo "  #  cross_check 관리자(admin) 패스워드 — 지금 한 번만 표시합니다"
  echo "  #"
  echo "  #      admin 패스워드:  $ADMIN_PW"
  echo "  #"
  echo "  #  분실 시 $CC_AUTH 를 확인 (root 만 읽기 가능)"
  echo "  ################################################################"
  echo
  unset ADMIN_PW
fi

# crontab — 매분 --ensure (데몬이 없으면 다시 띄움)
if [ "$CC_SKIP_CRON" = 1 ]; then
  echo "[INFO] CC_SKIP_CRON=1 — crontab 등록 건너뜀"
elif ! cron_sh install; then
  echo "[경고] crontab 등록 실패 — 데몬이 죽으면 자동으로 다시 뜨지 않습니다 (위 메시지 확인 후 bash $CC_CRON_SH install)" >&2
fi

# 데몬 기동 및 확인 (실행파일이 바뀌었으면 다시 띄운다)
if [ "$CC_SKIP_START" = 1 ]; then
  echo "[INFO] CC_SKIP_START=1 — 데몬 기동 건너뜀"
else
  if [ "$CC_CHANGED" -eq 1 ] && daemon_running; then
    echo "[INFO] 실행파일이 바뀌어 데몬을 다시 띄웁니다"
    stop_daemon || true
  fi
  cc_env "$CC_DST" --ensure || echo "[경고] cross_check --ensure 가 실패했습니다 (종료 코드 $?)" >&2
  i=0
  while [ "$i" -lt 25 ] && ! { [ -S "$CC_SOCK" ] && daemon_running; }; do sleep 0.2; i=$((i + 1)); done
  if [ -S "$CC_SOCK" ] && daemon_running; then
    echo "[완료] cross_check 데몬 동작 중 (소켓 $CC_SOCK)"
  else
    echo "[경고] 5초 안에 데몬 소켓($CC_SOCK)이 생기지 않았습니다 — 로그(${CC_LOG:-/var/log/cross_check.log})를 확인하십시오. crontab 이 매분 다시 시도합니다" >&2
  fi
fi

echo
echo "남은 수동 작업:"
echo "  1) $CONF 의 users / vcenters 를 환경에 맞게 수정 (교차검증 사용자 목록은 $CC_CONF 의 users)"
echo "  2) 각 사람이 한 번씩 등록: cross_check --user <이름>   (선착순 등록이므로 모두 가능한 빨리)"
echo "  3) 관리자 화면: cross_check --admin   (admin 패스워드: $CC_AUTH 의 admin= 줄)"
if [ "$CC_STATE_KEY" = on ]; then
  echo "  4) 교차검증 사용 중 (pwonoff.conf cross_check=on)"
else
  echo "  4) 교차검증은 꺼져 있음 — 켜려면: sudo bash $HERE/install.sh --enable-cross-check   (또는 $CONF 의 cross_check=on)"
fi
