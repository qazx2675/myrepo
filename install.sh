#!/bin/bash
# awx_copy 설치 (root 로 실행)
#   ./install.sh [바이너리 경로]     기본: 이 스크립트 옆 bin/awx_copy
#
# 하는 일:
#   1) 바이너리를 /usr/local/bin/awx_copy (root:root 755) 로 복사
#   2) /etc/awx_copy (root:root 755) 생성
#      755 이유: 일반 사용자가 자기 {user}.conf 를 읽고 선택 목록을 보려면 폴더 탐색·목록 권한이 필요하다.
#      conf 파일은 600(소유자=해당 사용자)이라 다른 사용자는 내용을 읽지 못한다.
#      대신 폴더 목록으로 사용자 이름(파일명)은 모두에게 보인다. root.conf 는 root 600 그대로다.
#   3) /etc/awx_copy/root.conf 가 없으면 기본 root 기록 조회 비밀번호의 SHA-256 해시로 생성 (600)
#      평문은 이 스크립트에도 없다. 설치 뒤 'awx_copy --root-passwd' 로 바꾼다.
#   4) /tmp/.awx_copy (1777) 생성 (업로드 기록)
# 사용자 설정 /etc/awx_copy/{user}.conf 는 관리자가 따로 배포한다 (소유자=해당 사용자, 600).
set -eu
if [ "$(id -u)" != "0" ]; then
    echo "오류: root 로 실행하세요" >&2
    exit 1
fi
DIR="$(cd "$(dirname "$(readlink -f "$0")")" && pwd)"
BIN="${1:-$DIR/bin/awx_copy}"
if [ ! -f "$BIN" ]; then
    echo "오류: 바이너리가 없습니다: $BIN (./build.sh 로 빌드하세요)" >&2
    exit 1
fi
ETC=/etc/awx_copy
# 기본 비밀번호의 SHA-256 (평문은 저장하지 않는다)
DEFAULT_ROOT_SHA256=de27ad6167310d667c33d6e6f3fd2050eaa4941bc5cf5a2c820c5a35f3a292a0

install -m 755 -o root -g root "$BIN" /usr/local/bin/awx_copy.new
mv -f /usr/local/bin/awx_copy.new /usr/local/bin/awx_copy
echo "설치: /usr/local/bin/awx_copy ($(/usr/local/bin/awx_copy --version))"

install -d -m 755 -o root -g root "$ETC"
chmod 755 "$ETC"
echo "폴더: $ETC (755, 파일명=사용자 이름은 모두에게 보임)"

if [ ! -e "$ETC/root.conf" ]; then
    umask 077
    printf '# awx_copy 기록 조회용 root 비밀번호 SHA-256 (평문 저장 금지)\npassword_sha256 = %s\n' "$DEFAULT_ROOT_SHA256" > "$ETC/root.conf"
    chown root:root "$ETC/root.conf"
    chmod 600 "$ETC/root.conf"
    echo "생성: $ETC/root.conf (기본 비밀번호 해시). 'awx_copy --root-passwd' 로 바꾸세요."
else
    echo "유지: $ETC/root.conf"
fi

install -d -m 1777 /tmp/.awx_copy 2>/dev/null || true
chmod 1777 /tmp/.awx_copy 2>/dev/null || true
echo "기록: /tmp/.awx_copy (1777)"
echo "다음: /etc/awx_copy/{user}.conf 배포 (chown {user} ; chmod 600)"
