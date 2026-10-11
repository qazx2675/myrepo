#!/usr/bin/env bash
###############################################################################
# update.sh — 실 서버에 배포된 ldap_setting 을 새 버전 코드로 갱신
#
# 이 디렉터리는 git 저장소에서 통째로 복사돼 실 서버(예: /root/ldap_setting)에서
# 독립적으로 운영될 수 있습니다. 새 버전을 받으면 이 스크립트로 코드만 갱신하고,
# conf/ldap_config.conf 와 conf/assets.txt (실제 운영값)는 절대 건드리지 않습니다.
#
# 사용법
#   ./update.sh <새 버전 디렉터리 경로>
#
#   예) git clone 으로 최신 코드를 어딘가에 받아 놓고
#       ./update.sh ~/myrepo/.claude/HPC/ldap_setting
#
# 동작
#   1. conf/ldap_config.conf, conf/assets.txt 를 임시 위치에 백업
#   2. 새 버전 디렉터리의 내용을 현재 디렉터리로 복사 (bin/, .git 은 제외)
#   3. 백업해 둔 conf/ldap_config.conf, conf/assets.txt 를 그대로 복원
#
# 이후 반드시 ./setup.sh 로 바이너리를 다시 빌드해야 합니다 (bin/ 은 갱신 대상에서
# 제외되며, 새 코드는 재빌드 전까지 반영되지 않습니다).
###############################################################################

set -u
cd "$(dirname "$0")" || exit 2

SRC="${1:-}"
if [ -z "$SRC" ]; then
    echo "사용법: $0 <새 버전 디렉터리 경로>"
    exit 2
fi
if [ ! -d "$SRC" ]; then
    echo "오류: 소스 디렉터리가 없습니다: $SRC"
    exit 2
fi

# 실수로 자기 자신을 소스로 지정하는 경우를 막습니다.
SRC_ABS="$(cd "$SRC" && pwd)"
DST_ABS="$(pwd)"
if [ "$SRC_ABS" = "$DST_ABS" ]; then
    echo "오류: 소스와 대상이 같은 디렉터리입니다."
    exit 2
fi

if [ ! -f "$SRC/go.mod" ] || [ ! -d "$SRC/cmd" ]; then
    echo "오류: $SRC 가 ldap_setting 소스로 보이지 않습니다 (go.mod 또는 cmd/ 없음)."
    exit 2
fi

echo ">> 소스: $SRC_ABS"
echo ">> 대상: $DST_ABS"
echo ">> conf/ldap_config.conf, conf/assets.txt 는 건드리지 않습니다."
echo

###############################################################################
# 1. 실제 운영값 파일 백업
###############################################################################

KEEP="$(mktemp -d)"
trap 'rm -rf "$KEEP"' EXIT

KEEP_FILES="conf/ldap_config.conf conf/assets.txt"

for f in $KEEP_FILES; do
    if [ -f "$f" ]; then
        mkdir -p "$KEEP/$(dirname "$f")"
        cp -p "$f" "$KEEP/$f"
        echo "  보관: $f"
    fi
done

###############################################################################
# 2. 코드 동기화
#
#   bin/ (빌드 산출물)과 .git 은 대상에서 제외합니다.
#   rsync 가 있으면 그걸 쓰고, 없는 폐쇄망 환경에서는 find+cp 로 대체합니다.
###############################################################################

echo
echo ">> 코드 동기화 중..."

if command -v rsync >/dev/null 2>&1; then
    rsync -a --delete \
        --exclude 'bin/' \
        --exclude '.git/' \
        --exclude 'conf/ldap_config.conf' \
        --exclude 'conf/assets.txt' \
        "$SRC_ABS/" "$DST_ABS/"
else
    echo "   (rsync 없음 → find+cp 로 대체)"
    ( cd "$SRC_ABS" && find . -mindepth 1 \( -path './bin' -o -path './.git' \) -prune -o -print ) \
    | while IFS= read -r item; do
        rel="${item#./}"
        [ -z "$rel" ] && continue
        if [ -d "$SRC_ABS/$rel" ]; then
            mkdir -p "$DST_ABS/$rel"
        else
            mkdir -p "$DST_ABS/$(dirname "$rel")"
            cp -p "$SRC_ABS/$rel" "$DST_ABS/$rel"
        fi
    done
fi

###############################################################################
# 3. 실제 운영값 파일 복원
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

chmod +x setup.sh test_all.sh update.sh scripts/*.sh 2>/dev/null

###############################################################################
echo
echo "=============================================================="
echo " 완료. conf/ldap_config.conf, conf/assets.txt 는 그대로입니다."
echo
echo " 다음을 확인하십시오:"
echo "   1) ./setup.sh 로 바이너리 재빌드 (필수 — 안 하면 새 코드가 반영 안 됨)"
echo "   2) diff conf/ldap_config.conf.sample conf/ldap_config.conf"
echo "      → 새로 추가된 키가 있으면 직접 채워 넣으십시오"
echo "   3) CHANGELOG.md 로 이번 버전에서 바뀐 내용 확인"
echo "   4) go test ./... && ./test_all.sh 로 정상 동작 확인"
echo "=============================================================="
###############################################################################
