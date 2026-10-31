#!/bin/bash
# gossh/pdsh 배포(업데이트) 스크립트.
# 이 스크립트가 놓인 서버(대상서버) 위에서 직접 실행해서, 저장소에 이미 들어있는
# 사전 빌드 바이너리(OS6용 gossh_os6, OS8용 gossh_os8)를 시스템 경로로 배포한다. (빌드는 하지 않는다)
#
# ★ bash는 변수명에 한글을 쓸 수 없어서(영문/숫자/밑줄만 허용) 변수명은 영문으로 뒀다.
# 아래 4개 값만 채우면 된다.

# 대상서버: 이 스크립트를 실행해도 되는 서버의 호스트명 또는 IP (여기 외 서버에서 실행하면 거부됨)
TARGET_SERVER=""
# 복사될위치1: OS6용 빌드(gossh_os6)가 "gossh"라는 이름으로 복사될 경로
DEST1=""
# 복사될위치2: OS8용 빌드(gossh_os8)가 "gossh"라는 이름으로 복사될 경로
DEST2="/usr/local/bin"
# 복사될위치3: OS8용 빌드(gossh_os8)가 "pdsh"라는 이름으로 복사될 경로
DEST3="/usr/bin/"

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"

for name in TARGET_SERVER DEST1 DEST2 DEST3; do
    if [ -z "${!name}" ]; then
        echo "변수 ${name} 값이 비어 있습니다. update.sh 상단에서 먼저 채워주세요." >&2
        exit 1
    fi
done

# ★ 이 스크립트는 대상서버 위에서만 실행 가능하다. 다른 서버에서 실수로 돌려서
# 엉뚱한 곳에 배포되는 사고를 막기 위해 호스트명/IP를 확인한다.
current_host="$(hostname -f 2>/dev/null || hostname)"
current_short_host="$(hostname)"
current_ips="$(hostname -I 2>/dev/null)"
if [ "$current_host" != "$TARGET_SERVER" ] && [ "$current_short_host" != "$TARGET_SERVER" ] \
    && ! echo " $current_ips " | grep -qw "$TARGET_SERVER"; then
    echo "이 스크립트는 대상서버(${TARGET_SERVER})에서만 실행할 수 있습니다." >&2
    echo "  현재 호스트: ${current_host} / IP: ${current_ips}" >&2
    exit 1
fi

for bin in gossh_os6 gossh_os8; do
    if [ ! -f "$bin" ]; then
        echo "${bin}(사전 빌드 바이너리)를 찾을 수 없습니다: ${SCRIPT_DIR}/${bin}" >&2
        exit 1
    fi
done

# ★ 강제 복사: 실행 중인 바이너리 경로에 그냥 덮어쓰면 "Text file busy"가 날 수 있다.
# 기존 파일을 지우고 새로 복사하면, 이미 실행 중인 프로세스는 지워지기 전 inode를 그대로
# 붙들고 계속 실행되고, 새로 실행되는 프로세스부터 새 바이너리를 쓰게 되어 안전하다.
force_copy() {
    local src="$1" dst="$2"
    if [ ! -f "$src" ]; then
        echo "원본 파일이 없습니다: $src" >&2
        exit 1
    fi
    mkdir -p "$(dirname "$dst")"
    rm -f "$dst"
    cp -f "$src" "$dst"
    chmod 755 "$dst"
    echo "  -> $dst"
}

echo "[1/3] 복사될위치1 (OS6 빌드 -> gossh): ${DEST1}"
force_copy "${SCRIPT_DIR}/gossh_os6" "${DEST1%/}/gossh"

echo "[2/3] 복사될위치2 (OS8 빌드 -> gossh): ${DEST2}"
force_copy "${SCRIPT_DIR}/gossh_os8" "${DEST2%/}/gossh"

echo "[3/3] 복사될위치3 (OS8 빌드 -> pdsh): ${DEST3}"
force_copy "${SCRIPT_DIR}/gossh_os8" "${DEST3%/}/pdsh"

echo "완료."
