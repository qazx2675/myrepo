#!/bin/bash
# apply_auto_mode.sh - auto_mode.patch(-auto·완료기록)를 회사 사본에 적용. 기존 주석·코멘트 문구는 수정하지 않는다.
#   사용: bash apply_auto_mode.sh [config_check.sh 경로]   (기본: ./config_check.sh)
#   1) <대상>.bak 백업  2) patch 적용  3) 삭제된 줄 검사: 주석(#)·벤더 코멘트 문구가 삭제/변경되면 원상 복구 후 중단
#   4) bash -n 문법 확인. 실패 hunk 가 있으면 원상 복구(대상은 바뀌지 않음).
set -u
HERE="$(cd "$(dirname "$0")" && pwd)"
TARGET="${1:-config_check.sh}"
PATCH="$HERE/auto_mode.patch"
[ -f "$TARGET" ] || { echo "[X] 대상 파일이 없습니다: $TARGET" >&2; exit 1; }
[ -f "$PATCH" ]  || { echo "[X] 패치 파일이 없습니다: $PATCH" >&2; exit 1; }
command -v patch >/dev/null 2>&1 || { echo "[X] patch 명령이 없습니다" >&2; exit 1; }

W=$(mktemp -d /tmp/apply_auto.XXXXXX); trap 'rm -rf "$W"' EXIT
tr -d '\r' < "$TARGET" > "$W/orig"          # CRLF 사본도 처리 (적용 결과는 LF)
tr -d '\r' < "$PATCH"  > "$W/p"
cp "$W/orig" "$W/new"
if ! patch -s "$W/new" < "$W/p" >"$W/patch.out" 2>&1; then
    cat "$W/patch.out" >&2
    echo "[X] 패치 적용 실패 — $TARGET 은 변경하지 않았습니다 (회사 사본의 해당 부분이 저장소와 다름)" >&2
    exit 1
fi
# 삭제·변경된 원본 줄 중 주석/코멘트 문구가 있으면 거부
removed=$(diff "$W/orig" "$W/new" | grep '^< ' | sed 's/^< //')
bad=$(printf '%s\n' "$removed" | grep -E '^[[:space:]]*#|담당자|감사합니다|점검부탁|서버 ' || true)
if [ -n "$bad" ]; then
    echo "[X] 주석/코멘트 줄이 바뀌게 되어 중단했습니다 — $TARGET 은 변경하지 않았습니다:" >&2
    printf '%s\n' "$bad" >&2
    exit 1
fi
bash -n "$W/new" || { echo "[X] 적용 결과 문법 오류 — $TARGET 은 변경하지 않았습니다" >&2; exit 1; }

cp "$TARGET" "$TARGET.bak"
cat "$W/new" > "$TARGET"
n=$(printf '%s\n' "$removed" | grep -c . || true)
echo "[O] 적용 완료: $TARGET (백업: $TARGET.bak)"
echo "    기존 줄 중 바뀐 것: ${n}줄 (모두 user 선택·질문 코드), 주석·코멘트 문구 변경 0건"
echo "    다음: 상단 auto_done_dir / auto_done_host 를 채우고 auto_setup 의 os_check_sh 를 이 파일로 지정"
