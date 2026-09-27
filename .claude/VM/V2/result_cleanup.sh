#!/usr/bin/env bash
# result_cleanup.sh — 실행 결과물 중 1시간 이상 수정이 없는 것만 삭제 (crontab 매시 실행용)
#
# 대상 (이 스크립트가 있는 V2 루트 기준 + 망변경 폴더):
#   VMsetup/run_<user>/                      실행 폴더 안의 모든 결과물 (last_vcenter 제외)
#   vm-param-check-usability-improvement/vm-param-check/
#                                            *.csv, *.log, scale_test_out/, real_test_out/
#   $NCI_DIR/results, logs, work             망변경 결과 파일 (접두어가 알려진 결과물만)
#   $NCI_DIR/incidents/<이름>/               인시던트 폴더 통째로
#
# 절대 건드리지 않는 것:
#   <user>.txt, vswitch_<user>.txt, vcenter.txt, SPEC_DIR/, secret/, conf/, *.conf,
#   run_<user>/last_vcenter(다음 실행의 "이전 vCenter"), 실행 파일·소스, tmp
#   → 위 대상 경로 밖은 스캔 자체를 하지 않는다.
#
# 판정: mtime(수정시간) 단독. atime 은 relatime/noatime/NFS 마운트에서 믿을 수 없고,
#       find 로 훑기만 해도 갱신되는 마운트가 있어 쓰지 않는다.
#
# 사용법:
#   DRY_RUN=1 ./result_cleanup.sh          지울 목록만 출력 (먼저 이것으로 확인)
#   ./result_cleanup.sh                    실제 삭제
#   NCI_DIR=/경로 ./result_cleanup.sh      망변경 폴더 지정 (없거나 비우면 그 부분은 건너뜀)
#
# crontab (매시 정각):
#   0 * * * * NCI_DIR=/망변경/경로 /bin/bash /V2루트/result_cleanup.sh >>/V2루트/result_cleanup.log 2>&1

set -uo pipefail

V2_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
NCI_DIR="${NCI_DIR:-}"
AGE_MIN="${AGE_MIN:-60}"
DRY_RUN="${DRY_RUN:-0}"

echo "=== $(date '+%F %T') result_cleanup (기준 ${AGE_MIN}분, DRY_RUN=$DRY_RUN) ==="

del_file() {
  if [ "$DRY_RUN" = 1 ]; then echo "[DRY_RUN] 삭제 예정: $1"
  else rm -f -- "$1" && echo "삭제: $1"; fi
}
del_dir() {
  if [ "$DRY_RUN" = 1 ]; then echo "[DRY_RUN] 폴더 삭제 예정: $1"
  else rm -rf -- "$1" && echo "폴더 삭제: $1"; fi
}
# 폴더 안에 AGE_MIN 안에 수정된 파일이 하나라도 있으면 0(최근), 없으면 1
has_recent() {
  find "$1" -type f -mmin "-$AGE_MIN" -print -quit | grep -q . && return 0
  # 파일이 하나도 없으면 폴더 자체 mtime 으로 판단 (막 만들어진 폴더 보호)
  find "$1" -type f -print -quit | grep -q . && return 1
  find "$1" -maxdepth 0 -mmin "-$AGE_MIN" -print -quit | grep -q .
}

# ── 1) VMsetup/run_<user>/ ──────────────────────────────────────────────────
# 실행 중인 run 폴더(최근 수정 파일 있음)는 통째로 건너뛴다 — 진행 중 파일을 지우지 않도록.
VMSETUP="$V2_DIR/VMsetup"
if [ -d "$VMSETUP" ]; then
  while IFS= read -r -d '' rd; do
    has_recent "$rd" && continue
    while IFS= read -r -d '' f; do
      [ "$(basename "$f")" = last_vcenter ] && continue
      del_file "$f"
    done < <(find "$rd" -type f -mmin "+$AGE_MIN" -print0)
    [ "$DRY_RUN" = 1 ] || find "$rd" -mindepth 1 -type d -empty -delete
  done < <(find "$VMSETUP" -mindepth 1 -maxdepth 1 -type d -name 'run_*' -print0)
fi

# ── 2) vm-param-check 결과 ──────────────────────────────────────────────────
VPC="$V2_DIR/vm-param-check-usability-improvement/vm-param-check"
if [ -d "$VPC" ]; then
  while IFS= read -r -d '' f; do del_file "$f"; done < \
    <(find "$VPC" -maxdepth 1 -type f \( -name '*.csv' -o -name '*.log' \) -mmin "+$AGE_MIN" -print0)
  for od in scale_test_out real_test_out; do
    [ -d "$VPC/$od" ] || continue
    while IFS= read -r -d '' f; do del_file "$f"; done < \
      <(find "$VPC/$od" -type f -mmin "+$AGE_MIN" -print0)
  done
fi

# ── 3) 망변경 통합 스크립트 (NCI_DIR) ───────────────────────────────────────
# 결과 접두어가 알려진 파일만 지운다. 그 외(<user>.txt, vswitch_<user>.txt, vcenter.txt,
# *.conf, *.sample, .gitkeep 등)는 모두 보호.
nci_deletable() {
  case "$1" in
    vswitch_*.std.txt) return 0 ;;
    vswitch_*|vcenter.txt|*.conf|*.sample|.gitkeep) return 1 ;;
    on_off_*.txt|ip_ok_*.txt|ldap_ok_*.txt|failed_*.txt|pg_targets_*.txt) return 0 ;;
    *.bak.*|*.tsv|*.log) return 0 ;;
  esac
  return 1
}
if [ -n "$NCI_DIR" ]; then
  if [ ! -f "$NCI_DIR/change.sh" ]; then
    echo "[경고] NCI_DIR 에 change.sh 가 없어 망변경 정리는 건너뜀: $NCI_DIR"
  else
    for sub in results logs work; do
      [ -d "$NCI_DIR/$sub" ] || continue
      while IFS= read -r -d '' f; do
        nci_deletable "$(basename "$f")" && del_file "$f"
      done < <(find "$NCI_DIR/$sub" -maxdepth 1 -type f -mmin "+$AGE_MIN" -print0)
    done
    if [ -d "$NCI_DIR/incidents" ]; then
      while IFS= read -r -d '' inc; do
        has_recent "$inc" || del_dir "$inc"
      done < <(find "$NCI_DIR/incidents" -mindepth 1 -maxdepth 1 -type d -print0)
    fi
  fi
fi
