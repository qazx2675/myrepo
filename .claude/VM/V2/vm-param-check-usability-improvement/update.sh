#!/bin/bash
# update.sh — 폐쇄망용 업데이트. 이 패키지의 payload/ 에 들어 있는 "바뀐 파일"만
# 지금 사용 중인 디렉토리에 반영한다. 그 외 파일은 하나도 건드리지 않는다.
#
# 인터넷, git, go 가 필요 없다. bash 와 기본 명령(cp, cmp, sha256sum, find)만 있으면 된다.
#
# 사용법 (패키지를 압축 푼 폴더 안에서):
#   bash update.sh "/내가/사용중인/디렉토리"        # 반영
#   bash update.sh -n "/내가/사용중인/디렉토리"     # 미리보기(아무것도 안 바꿈)
#
# 사용 중인 디렉토리 = ./vm-param-check 실행파일이 있는 곳
#   (01.vm_setting_check_insert.sh, vcenter.txt, SPEC_DIR/ 등이 같이 있는 그 디렉토리)
#
# 하는 일:
#   1) 패키지가 깨지지 않았는지 SHA256SUMS 로 확인
#   2) payload/ 의 파일 중 사용 중인 디렉토리와 다른 것만 골라냄
#   3) 새 vm-param-check 가 이 서버에서 실제로 실행되는지 먼저 시험(-demo, vCenter 접속 안 함)
#      -> 실행이 안 되면 아무것도 바꾸지 않고 중단
#   4) 바뀌는 파일은 <디렉토리>.update_backup.<시각>/ 에 백업한 뒤 교체
#
# 절대 건드리지 않는 것: 01.*, vcenter.txt, SPEC_DIR/, *.csv, *.log
# (payload에 없는 파일은 처음부터 대상이 아니다. 위 이름은 payload에 들어 있어도 건너뛴다.)

set -euo pipefail

die() {
    echo "[오류] $*" >&2
    exit 1
}

DRY_RUN=0
if [ "${1:-}" = "-n" ] || [ "${1:-}" = "--dry-run" ]; then
    DRY_RUN=1
    shift
fi
[ "$#" -eq 1 ] || die '사용법: bash update.sh [-n] "/내가/사용중인/디렉토리"'

TARGET="${1%/}"
[ -n "$TARGET" ] || die "디렉토리가 비어 있습니다."

HERE="$(cd "$(dirname "$0")" && pwd)"
PAYLOAD="$HERE/payload"

[ -d "$PAYLOAD" ] || die "payload/ 폴더가 없습니다. 패키지를 압축 푼 폴더 안에서 실행하세요."
[ -d "$TARGET" ] || die "$TARGET 폴더가 없습니다."
if [ ! -e "$TARGET/vm-param-check" ] && [ ! -f "$TARGET/main.go" ] && [ ! -f "$TARGET/go.mod" ]; then
    die "$TARGET 에 vm-param-check 실행파일이 없습니다. 사용 중인 디렉토리가 맞는지 확인하세요."
fi

# 저장소/배포본에 같은 이름이 있어도 절대 덮어쓰지 않는 사용자 파일
is_never_touch() {
    case "$1" in
        01.*|*/01.*|vcenter.txt|SPEC_DIR|SPEC_DIR/*|*.csv|*.log|*.bak) return 0 ;;
    esac
    return 1
}

echo "[1/4] 패키지 확인 중..."
if [ -f "$HERE/SHA256SUMS" ]; then
    (cd "$HERE" && sha256sum -c --quiet SHA256SUMS >/dev/null 2>&1) \
        || die "패키지 파일이 손상되었습니다(SHA256SUMS 불일치). USB/nfs로 다시 복사해 오세요. 아무것도 바꾸지 않았습니다."
    echo "  무결성 확인 완료"
else
    echo "  (SHA256SUMS 가 없어 무결성 확인은 건너뜁니다)"
fi
[ -f "$HERE/VERSION.txt" ] && sed 's/^/  /' "$HERE/VERSION.txt"

echo "[2/4] 바뀔 파일 확인 중... ($TARGET)"
ADD=(); CHG=(); SKIP=(); SAME=0; NEED_SMOKE=0
while IFS= read -r -d '' rel; do
    if is_never_touch "$rel"; then
        SKIP+=("$rel"); continue
    fi
    dest="$TARGET/$rel"
    if [ ! -e "$dest" ]; then
        ADD+=("$rel")
    elif cmp -s "$PAYLOAD/$rel" "$dest"; then
        SAME=$((SAME + 1)); continue
    else
        CHG+=("$rel")
    fi
    if [ "$rel" = "vm-param-check" ]; then NEED_SMOKE=1; fi
done < <(cd "$PAYLOAD" && find . -type f -printf '%P\0')

for rel in ${ADD[@]+"${ADD[@]}"}; do echo "  새로 추가: $rel"; done
for rel in ${CHG[@]+"${CHG[@]}"}; do echo "  교체(이전 파일은 백업): $rel"; done
[ "${#SKIP[@]}" -eq 0 ] || echo "  사용자 파일 이름이라 건너뜀: ${SKIP[*]}"

if [ "${#ADD[@]}" -eq 0 ] && [ "${#CHG[@]}" -eq 0 ]; then
    echo
    echo "이미 최신입니다 — 바뀌는 파일이 없어 아무것도 하지 않았습니다."
    exit 0
fi

# 새 실행파일이 이 서버에서 돌아가는지 먼저 시험한다(서버 OS가 달라 실행이 안 되는 경우를 미리 잡는다).
# -demo 는 vCenter에 접속하지 않고, 결과 CSV는 임시 폴더에 쓰이므로 사용 중인 디렉토리에는 영향이 없다.
if [ "$NEED_SMOKE" = 1 ]; then
    echo "[3/4] 새 실행파일이 이 서버에서 실행되는지 시험 중... (vCenter 접속 안 함)"
    SMOKE_DIR="$(mktemp -d /tmp/vpc-update.XXXXXX)"
    trap 'rm -rf "$SMOKE_DIR"' EXIT
    cp "$PAYLOAD/vm-param-check" "$SMOKE_DIR/vm-param-check"
    chmod 755 "$SMOKE_DIR/vm-param-check"
    (cd "$SMOKE_DIR" && ./vm-param-check -demo -noColor >"$SMOKE_DIR/out.txt" 2>&1) \
        && grep -q -- '-demo 모드' "$SMOKE_DIR/out.txt" \
        || die "새 vm-param-check 가 이 서버에서 실행되지 않습니다. 아무것도 바꾸지 않았습니다. (출력: $(head -c 300 "$SMOKE_DIR/out.txt" 2>/dev/null | tr '\n' ' '))"
    echo "  실행 확인 완료"
else
    echo "[3/4] (실행파일 변경 없음 — 실행 시험 생략)"
fi

if [ "$DRY_RUN" = 1 ]; then
    echo
    echo "[dry-run] 확인만 했습니다. 아무것도 바꾸지 않았습니다."
    exit 0
fi

BACKUP_DIR="${TARGET}.update_backup.$(date +%Y%m%d%H%M%S)"
echo "[4/4] 백업 후 교체 중..."
for rel in ${CHG[@]+"${CHG[@]}"}; do
    mkdir -p "$(dirname "$BACKUP_DIR/$rel")"
    cp -p "$TARGET/$rel" "$BACKUP_DIR/$rel"
done
for rel in ${ADD[@]+"${ADD[@]}"} ${CHG[@]+"${CHG[@]}"}; do
    mkdir -p "$(dirname "$TARGET/$rel")"
    # 실행 중인 파일을 덮어쓰다 깨지지 않도록 옆에 만든 뒤 한 번에 바꾼다
    cp -p "$PAYLOAD/$rel" "$TARGET/$rel.update.$$"
    if [ "$rel" = "vm-param-check" ]; then chmod 755 "$TARGET/$rel.update.$$"; fi
    mv -f "$TARGET/$rel.update.$$" "$TARGET/$rel"
done

echo
echo "완료. 사용 중인 디렉토리: $TARGET"
echo "  새로 추가 ${#ADD[@]}개 / 교체 ${#CHG[@]}개 — 이 외의 파일은 전혀 건드리지 않았습니다."
if [ "${#CHG[@]}" -gt 0 ]; then
    echo "  이전 파일 백업: $BACKUP_DIR"
    echo "  되돌리기: cp -a \"$BACKUP_DIR\"/. \"$TARGET\"/   (문제 없으면 백업 폴더는 직접 삭제)"
fi
