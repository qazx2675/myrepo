#!/usr/bin/env bash
# playground.sh — cross_check 를 손으로 직접 시험해 보는 격리 놀이터(sandbox).
#
# /etc, /var, /usr/local, crontab 을 전혀 건드리지 않고, 한 폴더($CC_PLAY_DIR, 기본 $HOME/cc_playground) 안에서
# 데몬·설정·패스워드·작업 기록·로그를 모두 따로 쓴다. 시험 전용 계정(alice/bob/carol/admin)과 고정 패스워드를 미리 넣어 둔다.
# (패스워드가 화면에 그대로 적혀 있는 시험용이므로 실제 서버의 /etc/cross_check 와 섞어 쓰지 마세요.)
#
#   bash playground.sh start [--ttl 초]   빌드(필요하면) → 폴더·설정·계정·대상 파일 준비 → 데몬을 백그라운드로 기동
#   bash playground.sh stop               놀이터 데몬 중지
#   bash playground.sh restart [--ttl 초] 중지 후 다시 기동
#   bash playground.sh reload             데몬을 죽이지 않고 conf 의 approval_ttl_sec 만 다시 읽게 함(SIGHUP)
#   bash playground.sh ttl <초>           conf 의 approval_ttl_sec 를 바꾸고 바로 반영(실행 중이면 SIGHUP)
#   bash playground.sh status             데몬·승인 유효시간·작업 기록 요약
#   bash playground.sh reset [--hard]     중지 + 작업 기록·로그 삭제(설정·계정 유지). --hard 는 설정·계정·대상 파일도 처음 상태로
#   bash playground.sh env                다른 터미널에서 쓰는 export 줄 출력:  eval "$(bash playground.sh env)"
#   bash playground.sh help               그대로 따라 하는 시험 안내(한국어)
#
# 환경변수: CC_PLAY_DIR(놀이터 폴더), CC_PLAY_BIN(미리 빌드한 cross_check 실행파일), CC_GO(go 실행파일 경로)
# 요구: bash 3 이상, pgrep, (실행파일이 없을 때만) Go 1.20 이상. root 권한은 필요 없다.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)"
CC_ROOT="$(cd "$SCRIPT_DIR/.." && pwd -P)" # cross_check 모듈 폴더(go.mod 가 있는 곳)

# ---- 경로 ---------------------------------------------------------------
PLAY="${CC_PLAY_DIR:-${HOME:-/tmp}/cc_playground}"
case "$PLAY" in
  /*) ;;
  *) PLAY="$(pwd -P)/$PLAY" ;;
esac
PLAY="${PLAY%/}"
case "$PLAY" in
  ""|/etc|/etc/*|/var|/var/*|/usr|/usr/*|/bin|/bin/*|/sbin|/sbin/*|/lib|/lib/*|/lib64|/lib64/*|/boot|/boot/*|/proc|/proc/*|/sys|/sys/*|/dev|/dev/*|/root/.ssh|/root/.ssh/*)
    echo "[오류] 놀이터 폴더로 쓸 수 없는 위치입니다: ${PLAY:-/}  (CC_PLAY_DIR 을 홈 아래 새 폴더로 지정하세요)" >&2
    exit 2 ;;
esac

P_BIN="$PLAY/bin"
P_REAL="$P_BIN/cross_check.bin"   # 실제 실행파일(데몬도 이것)
P_WRAP="$P_BIN/cross_check"       # 환경변수를 알아서 맞춰 주는 래퍼 — PATH 에 두고 그냥 cross_check 로 쓴다
P_ETC="$PLAY/etc"
P_CONF="$P_ETC/cross_check.conf"
P_AUTH="$P_ETC/auth.conf"
P_TARGETS="$PLAY/targets"
P_RUN="$PLAY/run"
P_SOCK="$P_RUN/cc.sock"
P_LOCK="$P_RUN/daemon.lock"
P_STATE_DIR="$PLAY/state"
P_STATE="$P_STATE_DIR/jobs.json"
P_LOG="$PLAY/cross_check.log"

# 시험 전용 계정(화면에 그대로 보이는 가짜 패스워드)
DEMO_USERS="alice,bob,carol"
PW_ALICE="alice123"
PW_BOB="bob12345"
PW_CAROL="carol123"
PW_ADMIN="admin123"

DEFAULT_TTL=120
MIN_TTL=10
MAX_TTL=3600

say() { printf '%s\n' "$*"; }
err() { printf '[오류] %s\n' "$*" >&2; }
die() { err "$*"; exit 1; }

# ---- 도우미 -------------------------------------------------------------
check_ttl() {
  case "${1:-}" in
    ""|*[!0-9]*) die "승인 유효시간은 ${MIN_TTL}~${MAX_TTL} 사이의 정수(초)여야 합니다: '${1:-}'  (예: 120 = 2분, 20 = 20초)" ;;
  esac
  if [ "${#1}" -gt 5 ] || [ "$((10#$1))" -lt "$MIN_TTL" ] || [ "$((10#$1))" -gt "$MAX_TTL" ]; then
    die "승인 유효시간은 ${MIN_TTL}~${MAX_TTL} 사이의 정수(초)여야 합니다: '$1'  (예: 120 = 2분, 20 = 20초)"
  fi
}

# conf 의 approval_ttl_sec (없으면 빈 값)
conf_ttl() {
  [ -f "$P_CONF" ] || return 0
  sed -n 's/^approval_ttl_sec=\([0-9][0-9]*\).*/\1/p' "$P_CONF" | head -1 || true
}

set_conf_ttl() {
  if grep -q '^approval_ttl_sec=' "$P_CONF"; then
    sed "s/^approval_ttl_sec=.*/approval_ttl_sec=$1/" "$P_CONF" > "$P_CONF.tmp"
    cat "$P_CONF.tmp" > "$P_CONF"
    rm -f "$P_CONF.tmp"
  else
    printf 'approval_ttl_sec=%s\n' "$1" >> "$P_CONF"
  fi
}

# 놀이터 데몬 pid 목록 (실행파일 전체 경로로 찾으므로 시스템의 다른 cross_check 데몬과 섞이지 않는다)
daemon_pids() {
  local pat
  pat="$(printf '%s' "$P_REAL" | sed 's|[][\.*^$]|\\&|g') --daemon"
  pgrep -f -- "$pat" 2>/dev/null | grep -vx "$$" || true
}

daemon_up() { [ -n "$(daemon_pids)" ] && [ -S "$P_SOCK" ]; }

# 놀이터 화면 머리말에서 "승인 유효시간: 2분 (approval_ttl_sec=120)" 의 초를 읽는다 = 데몬이 실제로 적용 중인 값
live_ttl() {
  local o
  o="$("$P_WRAP" --user alice 2>&1 < <(printf 'q\n') || true)"
  sed -n 's/.*(approval_ttl_sec=\([0-9][0-9]*\)).*/\1/p' <<<"$o" | head -1 || true
}

wait_live_ttl() { # <기대 초> [최대 0.1초 횟수]
  local want=$1 max=${2:-40} n=0
  while [ "$(live_ttl)" != "$want" ]; do
    n=$((n + 1))
    [ "$n" -gt "$max" ] && return 1
    sleep 0.1
  done
  return 0
}

# ---- 준비 ---------------------------------------------------------------
find_go() {
  if [ -n "${CC_GO:-}" ] && [ -x "${CC_GO}" ]; then echo "$CC_GO"; return; fi
  if [ -x /opt/go1.20/bin/go ]; then echo /opt/go1.20/bin/go; return; fi
  command -v go 2>/dev/null || true
}

ensure_binary() {
  local src="${CC_PLAY_BIN:-$CC_ROOT/bin/cross_check}" go
  if [ -n "${CC_PLAY_BIN:-}" ]; then
    [ -x "$src" ] || die "CC_PLAY_BIN 이 실행파일이 아닙니다: $src"
  else
    # 실행파일이 없거나, 소스(.go)가 더 새로우면 다시 빌드한다
    if [ ! -x "$src" ] || [ -n "$(find "$CC_ROOT/cmd" "$CC_ROOT/internal" -name '*.go' -newer "$src" 2>/dev/null | head -1)" ]; then
      go="$(find_go)"
      if [ -z "$go" ]; then
        [ -x "$src" ] || die "빌드할 Go 를 찾지 못했습니다. Go 1.20 이상을 설치하거나 CC_GO=/경로/go, 또는 미리 빌드한 CC_PLAY_BIN=/경로/cross_check 를 지정하세요."
        say "[경고] Go 가 없어 소스 변경분은 반영하지 못하고 기존 실행파일($src)을 씁니다"
      else
        say "[빌드] $go build → $src  (오프라인: GOPROXY=off)"
        mkdir -p "$CC_ROOT/bin"
        ( cd "$CC_ROOT" && CGO_ENABLED=0 GOPROXY=off "$go" build -o "$src" ./cmd/cross_check ) || die "go build 실패"
      fi
    fi
  fi
  mkdir -p "$P_BIN"
  if [ ! -x "$P_REAL" ] || ! cmp -s "$src" "$P_REAL"; then
    # 실행 중인 파일을 덮어쓰지 않도록 임시 파일 → 이름 바꾸기
    cp "$src" "$P_REAL.new" && chmod 755 "$P_REAL.new" && mv -f "$P_REAL.new" "$P_REAL"
    say "[준비] 실행파일: $P_REAL"
  fi
}

# 환경변수 export 줄 (env 와 래퍼가 같이 쓴다)
env_lines() {
  printf 'export CROSS_CHECK_SOCKET=%q\n' "$P_SOCK"
  printf 'export CROSS_CHECK_CONF=%q\n' "$P_CONF"
  printf 'export CROSS_CHECK_AUTH=%q\n' "$P_AUTH"
  printf 'export CROSS_CHECK_STATE=%q\n' "$P_STATE"
  printf 'export CROSS_CHECK_LOCK=%q\n' "$P_LOCK"
  printf 'export CROSS_CHECK_LOG=%q\n' "$P_LOG"
}

write_wrapper() {
  {
    printf '#!/usr/bin/env bash\n'
    printf '# cc_playground 래퍼(자동 생성) — 놀이터 데몬을 가리키도록 CROSS_CHECK_* 를 맞추고 실제 cross_check 를 실행한다.\n'
    env_lines
    printf 'exec %q "$@"\n' "$P_REAL"
  } > "$P_WRAP.new"
  chmod 755 "$P_WRAP.new"
  mv -f "$P_WRAP.new" "$P_WRAP"
}

write_files() {
  local ttl=$1
  mkdir -p "$P_ETC" "$P_TARGETS" "$P_RUN" "$P_STATE_DIR"
  chmod 700 "$P_STATE_DIR" 2>/dev/null || true
  if [ ! -f "$P_CONF" ]; then
    cat > "$P_CONF" <<EOF
# cc_playground 의 cross_check 설정(놀이터 전용). 고친 뒤:  bash playground.sh reload  (approval_ttl_sec 만) 또는 restart
target_dir=$P_TARGETS
auth=required
users=$DEMO_USERS
# 승인 유효시간(초, 10~3600): 확인자 승인 뒤 이 시간 안에 작업자가 --start 하지 않으면 승인이 자동 취소됩니다
approval_ttl_sec=$ttl
# 본 작업 제한 시간(초, 600~604800): 작업중(--start 후)으로 이 시간 안에 --finish 가 없으면 승인/작업중단(작업자 응답 없음)
work_timeout_sec=86400
EOF
    say "[준비] 설정: $P_CONF  (users=$DEMO_USERS, approval_ttl_sec=$ttl)"
  fi
  if [ ! -f "$P_AUTH" ]; then
    ( umask 077
      cat > "$P_AUTH" <<EOF
# cc_playground 시험용 패스워드 — 가짜 값이며 실제 계정과 무관합니다(놀이터 밖에서 쓰지 마세요).
admin=$PW_ADMIN
alice=$PW_ALICE
bob=$PW_BOB
carol=$PW_CAROL
EOF
    )
    chmod 600 "$P_AUTH"
    say "[준비] 계정: $P_AUTH (0600)  alice/$PW_ALICE  bob/$PW_BOB  carol/$PW_CAROL  admin/$PW_ADMIN"
  fi
  [ -f "$P_TARGETS/on_off_alice.txt" ] || printf '# alice 의 작업 대상\nsvr1ev01\nsvr1ev02\nsvr2ev01\n' > "$P_TARGETS/on_off_alice.txt"
  [ -f "$P_TARGETS/on_off_bob.txt" ]   || printf '# bob 의 작업 대상\nsvr3ev01\nsvr3ev02\n' > "$P_TARGETS/on_off_bob.txt"
  [ -f "$P_TARGETS/on_off_carol.txt" ] || printf '# carol 의 작업 대상\nsvr4ev01\n' > "$P_TARGETS/on_off_carol.txt"
}

# ---- 명령 ---------------------------------------------------------------
cmd_start() {
  local ttl=""
  while [ $# -gt 0 ]; do
    case "$1" in
      --ttl) [ $# -ge 2 ] || die "--ttl 에는 초 단위 값이 필요합니다 (예: --ttl 20)"; ttl="$2"; shift ;;
      --ttl=*) ttl="${1#*=}" ;;
      *) die "알 수 없는 인자: $1  (사용법: bash playground.sh start [--ttl 초])" ;;
    esac
    shift
  done
  [ -z "$ttl" ] || check_ttl "$ttl"
  ttl="${ttl:+$((10#$ttl))}"
  if [ "${#PLAY}" -gt 80 ]; then die "놀이터 경로가 너무 깁니다(Unix 소켓 경로 길이 제한): $PLAY — CC_PLAY_DIR 을 짧게 지정하세요"; fi

  ensure_binary
  write_files "${ttl:-$DEFAULT_TTL}"
  write_wrapper

  if daemon_up; then
    say "[INFO] 놀이터 데몬이 이미 실행 중입니다 (pid $(daemon_pids | tr '\n' ' '))"
    if [ -n "$ttl" ] && [ "$ttl" != "$(conf_ttl)" ]; then cmd_ttl "$ttl"; fi
    return 0
  fi
  if [ -n "$ttl" ]; then set_conf_ttl "$ttl"; fi

  "$P_WRAP" --ensure || die "데몬을 띄우지 못했습니다 — 로그: $P_LOG"
  local n=0
  until daemon_up; do
    n=$((n + 1))
    [ "$n" -gt 50 ] && die "5초 안에 데몬 소켓이 생기지 않았습니다 — 로그: $P_LOG"
    sleep 0.1
  done
  say "[완료] 놀이터 데몬 기동 (pid $(daemon_pids | tr '\n' ' '), 승인 유효시간 $(conf_ttl)초)"
  say "       폴더: $PLAY"
  say "       다음:  bash $SCRIPT_DIR/playground.sh help"
}

cmd_stop() {
  local pids n=0
  pids="$(daemon_pids)"
  if [ -z "$pids" ]; then say "[INFO] 놀이터 데몬이 실행 중이 아닙니다"; return 0; fi
  # shellcheck disable=SC2086
  kill $pids 2>/dev/null || true
  while [ -n "$(daemon_pids)" ] && [ "$n" -lt 50 ]; do n=$((n + 1)); sleep 0.1; done
  pids="$(daemon_pids)"
  if [ -n "$pids" ]; then
    # shellcheck disable=SC2086
    kill -9 $pids 2>/dev/null || true
    sleep 0.2
  fi
  rm -f "$P_SOCK"
  say "[완료] 놀이터 데몬 중지"
}

cmd_reload() {
  daemon_up || die "놀이터 데몬이 실행 중이 아닙니다 (bash playground.sh start)"
  local want
  want="$(conf_ttl)"
  # shellcheck disable=SC2046
  kill -HUP $(daemon_pids)
  if [ -n "$want" ] && wait_live_ttl "$want" 30; then
    say "[완료] SIGHUP: 데몬이 승인 유효시간 ${want}초로 다시 읽었습니다 (실행 중이던 작업은 그대로)"
  else
    say "[경고] conf 의 approval_ttl_sec=${want:-?} 가 반영되지 않았습니다 — 값이 10~3600 범위인지, 로그를 확인하세요:"
    tail -3 "$P_LOG" 2>/dev/null | sed 's/^/        /' || true
    say "       (잘못된 값이면 데몬은 이전 값 $(live_ttl)초를 그대로 씁니다)"
    return 1
  fi
}

cmd_ttl() {
  [ $# -eq 1 ] || die "사용법: bash playground.sh ttl <초>   (예: bash playground.sh ttl 20)"
  check_ttl "$1"
  [ -f "$P_CONF" ] || die "아직 준비되지 않았습니다 — 먼저 bash playground.sh start"
  set_conf_ttl "$((10#$1))"
  say "[완료] $P_CONF : approval_ttl_sec=$((10#$1))"
  if daemon_up; then cmd_reload; else say "       (데몬이 꺼져 있어 다음 start 때 적용됩니다)"; fi
}

cmd_restart() {
  cmd_stop
  cmd_start "$@"
}

cmd_status() {
  say "놀이터 폴더 : $PLAY"
  if daemon_up; then
    say "데몬        : 실행 중 (pid $(daemon_pids | tr '\n' ' '))"
    say "승인 유효시간: 데몬 적용 중 $(live_ttl)초 / conf $(conf_ttl)초   (다르면 bash playground.sh reload)"
  else
    say "데몬        : 꺼져 있음 (bash playground.sh start)"
    [ -f "$P_CONF" ] && say "승인 유효시간: conf $(conf_ttl)초"
  fi
  if [ -f "$P_STATE" ]; then
    local s
    s="$(grep -o '"status": *"[^"]*"' "$P_STATE" 2>/dev/null | sed 's/.*: *"\(.*\)"/\1/' | LC_ALL=C sort | LC_ALL=C uniq -c | sed 's/^ *\([0-9]*\) \(.*\)/\2 \1건/' | tr '\n' ' ' || true)"
    say "작업 기록   : ${s:-없음}"
    s="$(grep -o '"phase": *"[^"]*"' "$P_STATE" 2>/dev/null | sed 's/.*: *"\(.*\)"/\1/' | LC_ALL=C sort | LC_ALL=C uniq -c | sed 's/^ *\([0-9]*\) \(.*\)/승인\/\2 \1건/' | tr '\n' ' ' || true)"
    [ -z "$s" ] || say "  (승인 중) : $s"
  else
    say "작업 기록   : 없음"
  fi
  say "로그        : $P_LOG"
  [ -f "$P_LOG" ] && { say "---- 로그 마지막 5줄"; tail -5 "$P_LOG" | sed 's/^/  /'; }
  return 0
}

cmd_reset() {
  local hard=0
  case "${1:-}" in
    "") ;;
    --hard) hard=1 ;;
    *) die "사용법: bash playground.sh reset [--hard]" ;;
  esac
  cmd_stop
  rm -f "$P_STATE" "$P_STATE".tmp* "$P_LOG" "$P_SOCK"
  if [ "$hard" -eq 1 ]; then
    rm -f "$P_CONF" "$P_AUTH" "$P_TARGETS"/on_off_alice.txt "$P_TARGETS"/on_off_bob.txt "$P_TARGETS"/on_off_carol.txt
    say "[완료] 초기화(--hard): 작업 기록·로그와 설정·계정·대상 파일을 지웠습니다 (다음 start 때 처음 상태로 다시 만듭니다)"
  else
    say "[완료] 초기화: 작업 기록·로그를 지웠습니다 (설정·계정·대상 파일은 그대로). 다시 시작: bash playground.sh start"
  fi
}

cmd_env() {
  env_lines
  printf 'export PATH=%q:"$PATH"\n' "$P_BIN"
}

cmd_help() {
  local self="$SCRIPT_DIR/playground.sh" worker="$SCRIPT_DIR/playground_worker.sh" ttl
  ttl="$(conf_ttl)"; ttl="${ttl:-$DEFAULT_TTL}"
  cat <<EOF
==============================================================================
 cross_check 놀이터(cc_playground) — 손으로 직접 해 보는 시험 안내
==============================================================================
 폴더   : $PLAY   (실제 /etc, /var, crontab 은 건드리지 않습니다)
 승인 유효시간: ${ttl}초   (conf 의 approval_ttl_sec — 아래 "3) 유효시간 바꾸기" 참고)
 시험용 계정  : alice / $PW_ALICE    bob / $PW_BOB    carol / $PW_CAROL    관리자 admin / $PW_ADMIN
 (이 패스워드들은 놀이터 전용 가짜 값입니다.)
 대상 파일 : alice → svr1ev01 svr1ev02 svr2ev01 / bob → svr3ev01 svr3ev02 / carol → svr4ev01

0) 준비 (한 번만)
   bash $self start              # 이미 떠 있으면 그대로 둡니다.  짧은 유효시간: start --ttl 20

1) 기본 흐름 — 터미널 2개를 엽니다 (원격 서버라면 각 터미널에서 먼저 ssh <계정>@<서버> 로 로그인)
   ※ 로그인 셸이 csh/tcsh 이면 로그인 직후 먼저 bash 를 입력해 bash 로 들어가세요(아래 eval 명령은 bash/sh 문법입니다).
   [터미널 1 = 작업자 alice]
     cd $SCRIPT_DIR
     bash playground_worker.sh alice bob
        → "[대기] 작업 <ID> — 확인자 bob 의 승인을 기다립니다" 와 대상 목록(svr1ev01 svr1ev02 svr2ev01)이 나오면 기다립니다.
          (실제로는 이 대상 목록을 메신저 등으로 확인자에게 전달합니다 — 확인자 화면에는 대상이 보이지 않습니다)

   [터미널 2 = 확인자 bob]  — 전체 화면(auto_setup 상태 리포트와 같은 방식, 키 하나로 바로 동작, 2초마다 자동 갱신)
     eval "\$(bash $self env)"     # 이 터미널에서 cross_check 명령을 쓸 수 있게 함
     cross_check --user bob
        → 맨 위 "cross_check 상태 리포트  (user: bob)", 오른쪽에 "갱신 HH:MM:SS  ● 데몬 실행 (pid N)",
          둘째 줄 카운터 "전체 … 대기 … 승인 … 작업중 … 작업완료 … 작업중단 … 중단 …", 아래에 작업 표가 보입니다.
        → 처음부터 "내가 확인자인 작업" 구역이 선택돼 있고, → ← 또는 Tab(Shift+Tab 은 이전)으로 "내 작업(작업자)" 구역과 오갑니다 (a 는 두 구역 함께 보기)
        → ↑↓ 로 alice 의 대기 작업을 고르고 Enter        → "1/3 대상 붙여넣기" 화면
        → 전달받은 대상을 붙여넣기:  svr1ev01 svr1ev02 svr2ev01
             (한 줄에 공백으로, 줄마다 하나씩, 탭 섞어서 — 어느 것이든 됨. 화면에는 "인식한 이름: 3개" 만 보이고 일치 여부는 안 보임)
        → 빈 줄에서 Enter (또는 Ctrl+D)                 → "2/3 패스워드"
        → 패스워드  $PW_BOB  를 치고 Enter (화면에는 * 로만 보임) → "3/3 결과" 에 "[승인 완료]" 와 "승인 완료 후 ${ttl}초 내에 …" 정책
        → Enter 로 목록에 돌아오면 상태가 "승인" 으로 바뀌어 있습니다.  q 로 종료.
        (줄 입력 화면이 필요하면 cross_check --user bob --plain: 번호로 열고 a → 대상 붙여넣기 → 빈 줄 → 패스워드)

   [터미널 1] 승인 알림이 뜨고 "내 작업 리스트", 승인 유효시간(만료 시각)이 나온 뒤
        "본 작업을 시작할까요? [Y/n]" 에서 Enter  →  "작업 수행(시뮬레이션)" → "[완료] … 승인/작업완료" 가 나오면 성공.
        (bob 화면 목록에서 상태가 승인 → 승인/작업중 → 승인/작업완료 로 바뀌는 것이 보입니다.
         호출 스크립트 열에는 자동 감지된 playground_worker.sh 가 보입니다)

2) 시험해 볼 시나리오 (각각 새 작업으로 — 터미널 1 에서 playground_worker.sh 를 다시 실행)
   (a) 틀린 패스워드    : 2/3 에서 아무 틀린 값 → "[X] 인증 실패"(작업은 대기 그대로) → 다시 $PW_BOB
   (b) 붙여넣기 불일치  : 1/3 에서 svr1ev01 svr1ev02 만 붙여넣고 → 패스워드 → "붙여넣은 대상 불일치(빠짐 1·남음 0·중복 0)"
                          → 작업은 중단, 터미널 1 에 같은 사유로 "[중단]" 후 종료(이름은 어디에도 나오지 않고 개수만)
   (c) 확인자가 아닌 carol: cross_check --user carol → 그 작업은 목록에 없음. bob 화면에서 carol 의 패스워드($PW_CAROL)를
                          넣어도 "인증 실패"(사칭 차단)
   (d) 취소             : bob 화면에서 작업을 고르고 c → $PW_BOB → 터미널 1 에 "[중단] … 확인자 bob 가 취소" 후 종료
   (e) 작업자 Ctrl+C    : 승인 전에 터미널 1 에서 Ctrl+C → bob 화면(2초마다 갱신)에서 그 작업이 "중단"(상세: 작업자 스크립트 종료)
   (f) 승인 후 시간 초과 : 유효시간을 짧게 →  bash $self ttl 20
                          터미널 1 에서 승인 후 [Y/n] 에 답하지 말고 20초 넘게 기다렸다가 Enter
                          → "[거부] … 승인 후 20초 내 미진행" 이 들어간 거부 메시지가 나오고 작업 수행은 나오지 않음.
                          (유효시간을 바꾸기 전에 이미 승인된 작업에도 새 값이 적용됩니다)
   (g) 본 작업 전에 No  : 승인 후 [Y/n] 에 n → "승인/작업중단"(작업자가 본 작업 전에 중단)
   (h) 작업 중 Ctrl+C   : "작업 수행(시뮬레이션)" 중에 Ctrl+C → "승인/작업중단"(작업자가 작업을 중단)
                          (천천히 하려면 CC_PLAY_WORK_SEC=10 bash playground_worker.sh alice bob)
   (i) 작업 중 kill -9  : 작업 중에 다른 터미널에서 pkill -9 -f playground_worker.sh → 아무것도 보고되지 않으므로
                          conf 의 work_timeout_sec(기본 86400초 = 24시간) 뒤 데몬이 "승인/작업중단"(작업자 응답 없음)으로 바꿉니다
   (j) users 밖 사용자    : cross_check --user dave          → "users= 목록에 없어 등록할 수 없음"
                          bash playground_worker.sh alice dave → "확인자 dave 는 … users= 목록에 없음"
   (k) 대상 파일 밖의 대상 : bash playground_worker.sh alice bob svr9ev99   → "등록되지 않은 대상" 으로 거부(종료 코드 3)
   (l) 작업자 = 확인자    : bash playground_worker.sh alice alice → 기다리지 않고 즉시 중단
   (m) 관리자 모드       : cross_check --admin → $PW_ADMIN → 머리말에 승인 유효시간·작업 제한, 모든 작업 표(대상 수 포함).
                          Enter 상세(대상 목록) · s 상태 변경(번호 → 사유 → Enter) · d 삭제(y/n) ·
                          x 전체 정리(확인 문구 "전체 정리" 입력) · p 패스워드 재설정 · q 종료
   전체 화면 키(사용자): ↑↓ 이동  ←→/Tab 구역 전환  Enter 상세/승인  a 전체 보기  r 새로고침  c 취소  p 패스워드 변경  ? 도움말  q 종료

3) 유효시간 바꾸기 (승인 후 몇 초 안에 작업을 시작해야 하는지)
   - 간단히:        bash $self ttl 300         # conf 수정 + 데몬에 SIGHUP(재시작 없이 반영)
   - 직접 고치기:   vi $P_CONF   → approval_ttl_sec=300 → bash $self reload
   - 완전히 다시:   bash $self restart --ttl 300   (열려 있던 대기/승인 작업은 '데몬 재시작' 으로 중단됨)
   - 범위는 10~3600초. 5 나 99999 를 넣으면 reload 는 이전 값을 유지하고(로그에 이유), restart 는 기동을 거부합니다.
   - 실제 서버에서는 pkill -HUP -f 'cross_check --daemon' 으로 같은 일을 합니다(놀이터는 이 스크립트를 쓰세요).

4) 관리 명령
   bash $self status          # 데몬·유효시간·작업 기록 요약
   tail -f $P_LOG   # 데몬 로그를 보면서 시험하면 이해가 쉽습니다
   bash $self reset           # 작업 기록·로그 삭제(설정·계정 유지).  reset --hard 는 전부 처음 상태로
   bash $self stop            # 데몬 중지
   bash $self help            # 이 안내

참고: 다른 터미널에서 'cross_check' 를 그냥 치려면  eval "\$(bash $self env)"  를 먼저 실행하세요.
      (또는 $P_WRAP 를 직접 실행해도 됩니다 — 환경변수를 알아서 맞춥니다.)
==============================================================================
EOF
}

# ---- 진입점 ---------------------------------------------------------------
sub="${1:-help}"
[ $# -eq 0 ] || shift
case "$sub" in
  start) cmd_start "$@" ;;
  stop) cmd_stop ;;
  restart) cmd_restart "$@" ;;
  reload) cmd_reload ;;
  ttl) cmd_ttl "$@" ;;
  status) cmd_status ;;
  reset) cmd_reset "$@" ;;
  env) cmd_env ;;
  help|-h|--help) cmd_help ;;
  *) err "알 수 없는 명령: $sub"; cmd_help >&2; exit 2 ;;
esac
