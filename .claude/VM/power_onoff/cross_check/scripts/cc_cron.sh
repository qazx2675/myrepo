#!/usr/bin/env bash
# cc_cron.sh — root crontab 에 cross_check 데몬 유지용 줄을 멱등으로 관리한다 (root 로 실행).
#   매분: * * * * * /usr/local/bin/cross_check --ensure >/dev/null 2>&1 # cross_check
#   (--ensure 는 데몬이 이미 떠 있으면 즉시 끝나고, 없으면 데몬을 띄운다)
# 사용법:
#   sudo bash cc_cron.sh install     # 정확히 한 줄만 등록 (중복·옛 줄은 정리, 다른 줄은 그대로)
#   sudo bash cc_cron.sh remove      # cross_check 줄만 삭제
#   sudo bash cc_cron.sh status      # 등록 여부 출력 (등록 0, 미등록 1)
# 환경변수: CC_BIN=<cross_check 절대경로> (기본 /usr/local/bin/cross_check)
# 종료 코드: 0 성공, 1 실패/미등록(status), 2 사용법·환경 오류(crontab 없음, root 아님)
set -u

TAG='# cross_check'
BIN="${CC_BIN:-/usr/local/bin/cross_check}"
LINE="* * * * * $BIN --ensure >/dev/null 2>&1 $TAG"

usage() {
  echo "사용법: bash $0 install|remove|status   (CC_BIN=$BIN)" >&2
  exit 2
}

[ $# -eq 1 ] || usage
ACTION="$1"
case "$ACTION" in install|remove|status) ;; -h|--help) usage ;; *) usage ;; esac
case "$BIN" in /*) ;; *) echo "[오류] CC_BIN 은 절대경로여야 합니다: $BIN" >&2; exit 2 ;; esac

if ! command -v crontab >/dev/null 2>&1; then
  echo "[오류] crontab 명령이 없습니다 — cronie 패키지를 설치하고(폐쇄망: 설치 미디어의 cronie RPM) crond 를 켠 뒤 다시 실행하세요" >&2
  echo "        예) dnf install cronie && systemctl enable --now crond" >&2
  exit 2
fi
if [ "$(id -u)" -ne 0 ]; then
  echo "[오류] root crontab 을 다루므로 root 로 실행하세요 (sudo bash $0 $ACTION)" >&2
  exit 2
fi

T="$(mktemp -d)" || { echo "[오류] 임시 디렉터리를 만들 수 없습니다" >&2; exit 1; }
trap 'rm -rf "$T"' EXIT

# 현재 crontab 읽기. crontab 이 아직 없으면 빈 것으로 본다 ("no crontab for root").
if crontab -l > "$T/cur" 2> "$T/err"; then
  :
elif grep -qi 'no crontab' "$T/err"; then
  : > "$T/cur"
else
  echo "[오류] crontab -l 실패: $(cat "$T/err")" >&2
  exit 1
fi

# 우리 줄 = 끝이 ' # cross_check' 이거나, 주석이 아닌 줄에 'cross_check --ensure' 가 있는 줄
OURS_FN='function ours(l) { sub(/\r$/, "", l); return (l ~ / # cross_check$/) || (l !~ /^[ \t]*#/ && l ~ /cross_check --ensure/) }'
split_ours() { # $1 = crontab 파일 → $T/ours (우리 줄), $T/rest (나머지 줄, 순서 그대로)
  awk "$OURS_FN"' ours($0)' "$1" > "$T/ours"
  awk "$OURS_FN"' !ours($0)' "$1" > "$T/rest"
}
split_ours "$T/cur"
N_OURS=$(wc -l < "$T/ours" | tr -d ' ')

write_crontab() { # $1 = 새 crontab 파일
  if ! crontab "$1" 2> "$T/err"; then
    echo "[오류] crontab 쓰기 실패: $(cat "$T/err")" >&2
    exit 1
  fi
}

crond_hint() {
  if command -v pgrep >/dev/null 2>&1 && ! pgrep -x crond >/dev/null 2>&1 && ! pgrep -x cron >/dev/null 2>&1; then
    echo "[경고] crond 가 동작 중이 아닙니다 — 등록해도 실행되지 않습니다 (systemctl enable --now crond / OS6: service crond start; chkconfig crond on)" >&2
  fi
}

case "$ACTION" in
  install)
    if [ "$N_OURS" -eq 1 ] && [ "$(tr -d '\r' < "$T/ours")" = "$LINE" ]; then
      echo "[INFO] root crontab 에 이미 등록되어 있습니다: $LINE"
      crond_hint
      exit 0
    fi
    # awk 출력은 줄마다 줄바꿈으로 끝나므로 그대로 이어 붙이면 된다
    cp "$T/rest" "$T/new"
    echo "$LINE" >> "$T/new"
    write_crontab "$T/new"
    # 확인
    crontab -l 2>/dev/null | tr -d '\r' | grep -cxF -- "$LINE" > "$T/cnt" || true
    if [ "$(cat "$T/cnt")" = "1" ]; then
      [ "$N_OURS" -gt 0 ] && echo "[INFO] 기존 cross_check 줄 $N_OURS 개를 정리했습니다"
      echo "[완료] root crontab 등록: $LINE"
      crond_hint
      exit 0
    fi
    echo "[오류] 등록 후 확인 실패 (crontab -l 에 줄이 정확히 1개가 아님)" >&2
    exit 1
    ;;
  remove)
    if [ "$N_OURS" -eq 0 ]; then
      echo "[INFO] root crontab 에 cross_check 줄이 없습니다"
      exit 0
    fi
    write_crontab "$T/rest"
    crontab -l > "$T/after" 2>/dev/null || : > "$T/after"
    split_ours "$T/after"
    if [ -s "$T/ours" ]; then
      echo "[오류] 삭제 후에도 cross_check 줄이 남아 있습니다" >&2
      exit 1
    fi
    echo "[완료] root crontab 에서 cross_check 줄 $N_OURS 개 삭제"
    exit 0
    ;;
  status)
    if [ "$N_OURS" -eq 0 ]; then
      echo "[INFO] 미등록 (root crontab 에 cross_check 줄 없음)"
      exit 1
    fi
    echo "[INFO] 등록된 줄 ($N_OURS 개):"
    sed 's/^/    /' "$T/ours"
    if [ "$N_OURS" -ne 1 ] || [ "$(tr -d '\r' < "$T/ours")" != "$LINE" ]; then
      echo "[경고] 기대한 줄과 다릅니다 — bash $0 install 로 정리하세요 (기대: $LINE)"
    fi
    crond_hint
    exit 0
    ;;
esac
