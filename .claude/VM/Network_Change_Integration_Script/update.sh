#!/usr/bin/env bash
###############################################################################
# update.sh — 실 서버(다른 부서)에 배포된 통합 스크립트를 새 버전 코드로 갱신
#
# 이 폴더(Network_Change_Integration_Script)는 git 저장소에서 통째로 복사돼
# 독립적으로 운영됩니다(projects/ 아래에 하위 3프로젝트 소스를 자체 보관).
# 새 버전을 받으면 이 스크립트로 코드만 갱신하고, 아래 운영값 파일은 절대
# 건드리지 않습니다:
#     integration.conf, conf/*.conf, vcenter.txt, 루트의 <계정>.txt / vswitch_<계정>.txt,
#     bin/, projects/*/bin/, logs/ work/ results/ incidents/ 내용
#
# 사용법 (기존에 change.sh 를 실행하던 배포 폴더에서)
#   bash update.sh <새 버전 Network_Change_Integration_Script 경로>
#
# 동작
#   1. 운영값 파일을 임시 위치에 백업
#   2. 새 버전 디렉터리 내용을 현재 디렉터리로 동기화 (bin/ · .git · 운영값 제외)
#   3. 백업해 둔 운영값 파일을 그대로 복원
#   ※ 자동 롤백 없음 — 실행 전 배포 폴더를 통째로 백업해 두십시오.
#
# 이후 반드시 ./setup.sh 로 바이너리를 다시 빌드해야 합니다.
###############################################################################
set -u
cd "$(dirname "$0")" || exit 2

SRC="${1:-}"
if [ -z "$SRC" ]; then
    echo "사용법: bash update.sh <새 버전 Network_Change_Integration_Script 경로>"
    exit 2
fi
if [ ! -d "$SRC" ]; then
    echo "오류: 소스 디렉터리가 없습니다: $SRC"
    exit 2
fi

SRC_ABS="$(cd "$SRC" && pwd)"
DST_ABS="$(pwd)"
if [ "$SRC_ABS" = "$DST_ABS" ]; then
    echo "오류: 소스와 대상이 같은 디렉터리입니다. 새 버전을 다른 경로에 풀고 실행하세요."
    exit 2
fi
if [ ! -f "$SRC/change.sh" ] || [ ! -d "$SRC/projects" ]; then
    echo "오류: $SRC 가 통합 스크립트 소스로 보이지 않습니다 (change.sh 또는 projects/ 없음)."
    exit 2
fi

echo ">> 소스: $SRC_ABS"
echo ">> 대상: $DST_ABS"
echo

###############################################################################
# 1. 운영값 파일 백업
###############################################################################

KEEP="$(mktemp -d)"
trap 'rm -rf "$KEEP"' EXIT

# 루트 워크리스트(*.txt, 단 .sample 제외) + 명시적 운영 파일
KEEP_FILES="integration.conf conf/ip_change.conf conf/ldap_config.conf conf/assets.txt vcenter.txt"
for t in ./*.txt; do
    [ -f "$t" ] || continue
    case "$t" in *.sample|./vcenter.txt) continue ;; esac
    KEEP_FILES="$KEEP_FILES ${t#./}"
done

for f in $KEEP_FILES; do
    if [ -f "$f" ]; then
        mkdir -p "$KEEP/$(dirname "$f")"
        cp -p "$f" "$KEEP/$f"
        echo "  보관: $f"
    fi
done

###############################################################################
# 2. 코드 동기화  (bin/ · projects/*/bin/ · .git 제외. rsync 없으면 find+cp)
###############################################################################

echo
echo ">> 코드 동기화 중..."

if command -v rsync >/dev/null 2>&1; then
    rsync -a --delete \
        --exclude '.git/' \
        --exclude '/bin/' \
        --exclude 'projects/*/bin/' \
        --exclude '/logs/*' --exclude '/work/*' \
        --exclude '/results/*' --exclude '/incidents/*' \
        --exclude '/integration.conf' \
        --exclude '/conf/*.conf' \
        --exclude '/vcenter.txt' \
        --exclude '/*.txt' \
        "$SRC_ABS/" "$DST_ABS/"
else
    echo "   (rsync 없음 → find+cp 로 대체, 오래된 소스 제거는 cmd/ internal/ lib/ 한정)"
    ( cd "$SRC_ABS" && find . -mindepth 1 \
        \( -path './.git' -o -path './bin' -o -path './projects/*/bin' \) -prune -o -print ) \
    | while IFS= read -r item; do
        rel="${item#./}"
        [ -z "$rel" ] && continue
        case "$rel" in
            integration.conf|conf/*.conf|vcenter.txt) continue ;;
            *.txt) case "$rel" in projects/*) ;; *) continue ;; esac ;;
        esac
        if [ -d "$SRC_ABS/$rel" ]; then
            mkdir -p "$DST_ABS/$rel"
        else
            mkdir -p "$DST_ABS/$(dirname "$rel")"
            cp -p "$SRC_ABS/$rel" "$DST_ABS/$rel"
        fi
    done
    # 배포본에만 있는 오래된 소스 제거 (빌드/소스 깨짐 방지)
    for base in lib projects/ip_change projects/ldap_setting projects/vm-network-migration; do
        [ -d "$DST_ABS/$base" ] || continue
        ( cd "$DST_ABS/$base" && find . -type f \( -name '*.go' -o -name '*.sh' \) -print ) \
        | while IFS= read -r rel; do
            r="${rel#./}"
            [ -e "$SRC_ABS/$base/$r" ] || { rm -f "$DST_ABS/$base/$r"; echo "  - $base/$r (오래된 소스 제거)"; }
        done
    done
fi

###############################################################################
# 3. 운영값 파일 복원
###############################################################################

echo
echo ">> 운영값 파일 복원 중..."
for f in $KEEP_FILES; do
    if [ -f "$KEEP/$f" ]; then
        mkdir -p "$(dirname "$f")"
        cp -p "$KEEP/$f" "$f"
        echo "  복원: $f"
    fi
done

chmod +x change.sh setup.sh update.sh lib/*.sh tests/*.sh projects/*/setup.sh projects/*/update.sh 2>/dev/null

echo
echo "=============================================================="
echo " 완료. 운영값 파일(integration.conf, conf/*.conf, vcenter.txt, 워크리스트)은 그대로입니다."
echo
echo " 다음을 확인하십시오:"
echo "   1) ./setup.sh 로 바이너리 재빌드 (필수 — 안 하면 새 코드가 반영 안 됨)"
echo "   2) diff integration.conf.sample integration.conf  → 새 키가 있으면 채워 넣기"
echo "   3) CHANGELOG.md 로 이번 버전 변경 내용 확인"
echo "=============================================================="
