#!/bin/bash
# mn(디렉터리 안내) 설치 / 제거 스크립트
# 사용법: ./install.sh [--uninstall]   (root 로 실행)
#
# 테스트용 환경변수 (기본값은 실제 시스템 경로):
#   INSTALL_BIN       (기본 /usr/local/bin)
#   INSTALL_PROFILED  (기본 /etc/profile.d)
#   INSTALL_CONF_DIR  (기본 /etc/dir)

set -u

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
BIN_DIR="${INSTALL_BIN:-/usr/local/bin}"
PROFILED_DIR="${INSTALL_PROFILED:-/etc/profile.d}"
CONF_DIR="${INSTALL_CONF_DIR:-/etc/dir}"

if [ "$(id -u)" -ne 0 ]; then
    echo "오류: root 권한으로 실행해야 합니다. (sudo ./install.sh)" >&2
    exit 1
fi

case "${1:-}" in
    --uninstall)
        for f in "$BIN_DIR/mn" "$PROFILED_DIR/mn.sh" "$PROFILED_DIR/mn.csh"; do
            if [ -e "$f" ]; then
                rm -f "$f" && echo "제거: $f"
            else
                echo "없음: $f"
            fi
        done
        echo "$CONF_DIR/dir.conf 는 건드리지 않았습니다."
        exit 0
        ;;
    "")
        ;;
    *)
        echo "사용법: $0 [--uninstall]" >&2
        exit 1
        ;;
esac

for f in mn profile.d/mn.sh profile.d/mn.csh; do
    if [ ! -f "$SCRIPT_DIR/$f" ]; then
        echo "오류: 설치 파일이 없습니다: $SCRIPT_DIR/$f" >&2
        exit 1
    fi
done

mkdir -p "$BIN_DIR" "$PROFILED_DIR" || exit 1

install -m 755 "$SCRIPT_DIR/mn" "$BIN_DIR/mn" || exit 1
echo "설치: $BIN_DIR/mn"
install -m 644 "$SCRIPT_DIR/profile.d/mn.sh" "$PROFILED_DIR/mn.sh" || exit 1
echo "설치: $PROFILED_DIR/mn.sh"
install -m 644 "$SCRIPT_DIR/profile.d/mn.csh" "$PROFILED_DIR/mn.csh" || exit 1
echo "설치: $PROFILED_DIR/mn.csh"

mkdir -p "$CONF_DIR" && chmod 755 "$CONF_DIR" || exit 1
if [ -e "$CONF_DIR/dir.conf" ]; then
    echo "dir.conf 이미 존재: 유지"
elif [ -f "$SCRIPT_DIR/dir.conf.sample" ]; then
    install -m 644 "$SCRIPT_DIR/dir.conf.sample" "$CONF_DIR/dir.conf" || exit 1
    echo "생성: $CONF_DIR/dir.conf (dir.conf.sample 복사)"
else
    echo "# mn 디렉터리 안내 설정 파일 (dir.conf.sample 이 없어 빈 파일로 생성됨)" > "$CONF_DIR/dir.conf" || exit 1
    chmod 644 "$CONF_DIR/dir.conf"
    echo "생성: $CONF_DIR/dir.conf (dir.conf.sample 없음: 빈 파일)"
fi

echo "새 로그인 쉘에서 mn 사용 (또는 source /etc/profile.d/mn.csh / mn.sh)"
