#!/bin/bash
# update_deploy.sh — 이미 쓰고 있는 배포 경로의 vm-param-check를 최신으로 "제자리 갱신"한다.
#
# 회사 서버처럼 사용자 파일(01.vm_setting_check_insert.sh, vcenter.txt, SPEC_DIR/ 등)이 같이
# 놓여 있는 배포 경로에서 실행하는 것을 전제로 한다. 원칙은 세 가지:
#
#   1) 사용자 파일은 건드리지 않는다.
#      새 버전에 들어 있는 파일만 그 자리에서 덮어쓰고, 배포 경로에만 있는 파일은 옮기지도
#      지우지도 않는다. (예전 버전은 배포 폴더를 통째로 .bak으로 옮겨서 사용자 파일이
#      배포 경로에서 사라졌다.)
#   2) 빌드가 되는 걸 확인한 뒤에만 배포 경로를 바꾼다.
#      임시 폴더에서 먼저 빌드해 보고, 실패하면 배포 경로는 손대지 않은 채 중단한다.
#   3) 덮어쓰는 파일은 백업한다.
#      바뀌는 파일의 이전 버전을 <배포경로>.update_backup.<시각>/ 에 복사해 두므로 되돌릴 수 있다.
#
# 소스는 이 도구 폴더만 담은 독립 브랜치(vm-param-check-standalone)에서 받는다. 이 브랜치에는
# vendor/(오프라인 빌드용 의존성)가 실제 파일로 들어 있다 — master는 vendor를 공유 폴더에
# 두고 setup.sh가 심볼릭 링크를 거는 구조라서, master에서 도구 폴더만 복사하면 빌드가 안 된다.
#
# 사용법:
#   bash update_deploy.sh                       # 기본 경로에 갱신
#   bash update_deploy.sh /배포/경로/vm-param-check   # 배포 경로 직접 지정
#   bash update_deploy.sh -n [경로]              # --dry-run: 무엇이 바뀔지만 보여주고 종료
#
# 환경변수(선택):
#   REPO_URL     소스 저장소 주소 (기본: GitHub public 저장소. 사내 미러/토큰 포함 주소로 교체 가능)
#   REPO_BRANCH  받을 브랜치 (기본: vm-param-check-standalone)
#
# 필요한 것: git, go, GitHub(또는 REPO_URL) 접속. 빌드는 vendor/만 쓰므로 인터넷 없이 된다.
#
# 사용자 파일 취급:
#   - 저장소에 없는 파일            -> 그대로 둔다 (01.vm_setting_check_insert.sh, 대상 목록 *.txt 등)
#   - 저장소에 같은 이름이 생겨도 절대 덮어쓰지 않는 것 -> 01.*, vcenter.txt, SPEC_DIR/, *.csv, *.log
#   - 값을 채워 쓰는 템플릿(vm_setting_check_insert.sh, folder_setup.sh, testfiles/*)
#                                   -> 배포본과 다르면 덮어쓰지 않고 <이름>.new 로 새 버전만 옆에 둔다

set -euo pipefail

REPO_URL="${REPO_URL:-https://github.com/qazx2675/myrepo.git}"
REPO_BRANCH="${REPO_BRANCH:-vm-param-check-standalone}"
TOOL_SUBDIR="vm-param-check"   # 브랜치 루트 안의 도구 폴더
DEFAULT_DEPLOY_PATH="/root/vm-param-check-usability-improvement/vm-param-check"

DRY_RUN=0
if [ "${1:-}" = "-n" ] || [ "${1:-}" = "--dry-run" ]; then
    DRY_RUN=1
    shift
fi
DEPLOY_PATH="${1:-$DEFAULT_DEPLOY_PATH}"
DEPLOY_PATH="${DEPLOY_PATH%/}"

die() {
    echo "[오류] $*" >&2
    exit 1
}

# 저장소에 같은 이름의 파일이 생기더라도 절대 덮어쓰지 않는 사용자 파일 (도구 폴더 기준 경로)
is_never_touch() {
    case "$1" in
        01.*|*/01.*|vcenter.txt|SPEC_DIR|SPEC_DIR/*|*.csv|*.log|*.bak|*.new) return 0 ;;
    esac
    return 1
}

# 사용자가 값을 채워 쓰는 템플릿 — 배포본과 다르면 덮어쓰지 않고 .new 로 옆에 둔다
is_user_editable() {
    case "$1" in
        vm_setting_check_insert.sh|folder_setup.sh|testfiles/*) return 0 ;;
    esac
    return 1
}

command -v git >/dev/null 2>&1 || die "git이 없습니다."
command -v go >/dev/null 2>&1 || die "go가 없습니다. 빌드에 필요합니다."

# 엉뚱한 폴더(예: 도구가 없는 기존 폴더, 한 단계 위 폴더)에 파일을 뿌리지 않도록 확인한다.
if [ -e "$DEPLOY_PATH" ]; then
    [ -d "$DEPLOY_PATH" ] || die "$DEPLOY_PATH 가 폴더가 아닙니다."
    if [ -n "$(ls -A "$DEPLOY_PATH" 2>/dev/null)" ] \
        && [ ! -f "$DEPLOY_PATH/go.mod" ] && [ ! -e "$DEPLOY_PATH/vm-param-check" ]; then
        die "$DEPLOY_PATH 는 비어 있지 않은데 vm-param-check 배포본(go.mod 또는 vm-param-check 실행파일)이 없습니다. 경로가 맞는지 확인하세요 (도구 폴더 자체를 지정해야 합니다)."
    fi
fi

TMP_DIR="$(mktemp -d /tmp/vm-param-check-update.XXXXXX)"
trap 'rm -rf "$TMP_DIR"' EXIT

echo "[1/5] 새 버전 받는 중... ($REPO_URL @ $REPO_BRANCH)"
git clone --depth 1 --single-branch --branch "$REPO_BRANCH" -q "$REPO_URL" "$TMP_DIR/repo" \
    || die "저장소를 받지 못했습니다. 접속/브랜치 이름($REPO_BRANCH)을 확인하세요."

SRC_DIR="$TMP_DIR/repo/$TOOL_SUBDIR"
[ -d "$SRC_DIR" ] || die "받은 브랜치 안에 $TOOL_SUBDIR/ 폴더가 없습니다 — 저장소 구조가 바뀌었을 수 있습니다."
{ [ -f "$SRC_DIR/main.go" ] && [ -f "$SRC_DIR/go.mod" ]; } || die "$TOOL_SUBDIR/ 안에 main.go 또는 go.mod가 없습니다."
[ -f "$SRC_DIR/vendor/modules.txt" ] || die "$TOOL_SUBDIR/vendor/ 가 없습니다 — 이 브랜치로는 오프라인 빌드를 할 수 없어 중단합니다."

echo "[2/5] 임시 폴더에서 빌드 확인 중... (배포 경로는 아직 건드리지 않았습니다)"
NEW_BIN="$TMP_DIR/vm-param-check.new"
(cd "$SRC_DIR" && GOPROXY=off go build -mod=vendor -o "$NEW_BIN" .) \
    || die "새 버전 빌드에 실패했습니다. 배포 경로는 아무것도 바뀌지 않았습니다."

echo "[3/5] 바뀔 내용 확인 중..."
ADD=(); CHG=(); KEEP=(); SKIP=()
VENDOR_IS_LINK=0
[ -L "$DEPLOY_PATH/vendor" ] && VENDOR_IS_LINK=1   # 링크 너머(공유 vendor)를 덮어쓰지 않기 위해

while IFS= read -r -d '' rel; do
    dest="$DEPLOY_PATH/$rel"
    if is_never_touch "$rel"; then
        SKIP+=("$rel"); continue
    fi
    if [ "$VENDOR_IS_LINK" = 1 ] && [[ "$rel" == vendor/* ]]; then
        SKIP+=("$rel"); continue
    fi
    if [ ! -e "$dest" ]; then
        ADD+=("$rel")
    elif cmp -s "$SRC_DIR/$rel" "$dest"; then
        :   # 이미 같음
    elif is_user_editable "$rel"; then
        KEEP+=("$rel")
    else
        CHG+=("$rel")
    fi
done < <(cd "$SRC_DIR" && find . -type f -printf '%P\0')

print_list() {   # $1 제목, 이후 항목들 (많으면 앞부분만)
    local title="$1"; shift
    [ "$#" -gt 0 ] || return 0
    echo "  $title ($#개)"
    local n=0 item
    for item in "$@"; do
        n=$((n + 1))
        [ "$n" -le 30 ] || { echo "    ... 외 $(($# - 30))개"; break; }
        echo "    $item"
    done
}
print_list "새로 추가" ${ADD[@]+"${ADD[@]}"}
print_list "갱신(이전 버전은 백업)" ${CHG[@]+"${CHG[@]}"}
print_list "직접 편집하는 파일이라 덮어쓰지 않음 -> 새 버전은 <이름>.new 로 저장" ${KEEP[@]+"${KEEP[@]}"}
[ "${#SKIP[@]}" -eq 0 ] || echo "  사용자 파일 패턴이라 건너뜀 (${#SKIP[@]}개)"

# 배포 경로에만 있는 파일(=사용자 파일)을 눈으로 확인할 수 있게 보여준다. 이 파일들은 건드리지 않는다.
if [ -d "$DEPLOY_PATH" ]; then
    OWN=()
    for f in "$DEPLOY_PATH"/* "$DEPLOY_PATH"/.[!.]*; do
        [ -e "$f" ] || continue
        name="$(basename "$f")"
        [ "$name" = "vm-param-check" ] && continue
        [ -e "$SRC_DIR/$name" ] || OWN+=("$name")
    done
    [ "${#OWN[@]}" -eq 0 ] || echo "  그대로 두는 사용자 파일: ${OWN[*]}"
fi

HAVE_BIN=0; [ -e "$DEPLOY_PATH/vm-param-check" ] && HAVE_BIN=1
if [ "${#ADD[@]}" -eq 0 ] && [ "${#CHG[@]}" -eq 0 ] && [ "$HAVE_BIN" = 1 ]; then
    echo
    echo "이미 최신입니다 — 바뀌는 파일이 없어 아무것도 하지 않았습니다."
    if [ "${#KEEP[@]}" -gt 0 ]; then
        echo "  (직접 편집하는 파일의 새 버전은 저장소에서 확인하세요: ${KEEP[*]})"
    fi
    exit 0
fi

if [ "$DRY_RUN" = 1 ]; then
    echo
    echo "[dry-run] 여기까지 확인만 했습니다. 아무것도 바꾸지 않았습니다."
    exit 0
fi

BACKUP_DIR="${DEPLOY_PATH}.update_backup.$(date +%Y%m%d%H%M%S)"

echo "[4/5] 덮어쓰는 파일 백업 중..."
BACKED_UP=0
for rel in ${CHG[@]+"${CHG[@]}"} $( [ "$HAVE_BIN" = 1 ] && echo vm-param-check ); do
    mkdir -p "$(dirname "$BACKUP_DIR/$rel")"
    cp -p "$DEPLOY_PATH/$rel" "$BACKUP_DIR/$rel"
    BACKED_UP=$((BACKED_UP + 1))
done
echo "  백업 $BACKED_UP개 -> ${BACKUP_DIR}$( [ "$BACKED_UP" -gt 0 ] || echo ' (백업할 기존 파일 없음)')"

echo "[5/5] 반영 중..."
mkdir -p "$DEPLOY_PATH"
for rel in ${ADD[@]+"${ADD[@]}"} ${CHG[@]+"${CHG[@]}"}; do
    mkdir -p "$(dirname "$DEPLOY_PATH/$rel")"
    cp -p "$SRC_DIR/$rel" "$DEPLOY_PATH/$rel.update.$$"
    mv -f "$DEPLOY_PATH/$rel.update.$$" "$DEPLOY_PATH/$rel"
done
for rel in ${KEEP[@]+"${KEEP[@]}"}; do
    cp -p "$SRC_DIR/$rel" "$DEPLOY_PATH/$rel.new"
done
# 실행 중인 바이너리를 덮어쓰다 깨지지 않도록 옆에 만든 뒤 한 번에 바꾼다.
install -m 755 "$NEW_BIN" "$DEPLOY_PATH/vm-param-check.update.$$"
mv -f "$DEPLOY_PATH/vm-param-check.update.$$" "$DEPLOY_PATH/vm-param-check"

echo
echo "완료. 배포 경로: $DEPLOY_PATH"
echo "  새로 추가 ${#ADD[@]}개 / 갱신 ${#CHG[@]}개 / 편집 파일 보존(.new) ${#KEEP[@]}개 / 실행파일 재빌드 완료"
echo "  확인: cd $DEPLOY_PATH && ./vm-param-check -demo"
if [ "$BACKED_UP" -gt 0 ]; then
    echo "  되돌리기: cp -a $BACKUP_DIR/. $DEPLOY_PATH/   (문제 없으면 백업 폴더는 직접 삭제)"
fi
if [ "${#KEEP[@]}" -gt 0 ]; then
    echo "  새 버전과 다른 점 보기: diff $DEPLOY_PATH/${KEEP[0]} $DEPLOY_PATH/${KEEP[0]}.new"
fi
