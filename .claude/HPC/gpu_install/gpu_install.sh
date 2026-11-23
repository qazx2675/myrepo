#!/bin/bash
# gpu_install.sh - list 의 GPU 서버에 NVIDIA 드라이버(.run)와 DCGM 4 를 설치한다.
# 실제 작업은 같은 폴더의 gpu_install_os8(Go 바이너리)가 하고, 이 스크립트는 인자만 넘긴다.
# 사용법: bash gpu_install.sh [autofs경로(기본 AUTOFS_PATH)] [list(기본 ./list)] [-p 동시대수(기본 10)]

AUTOFS_PATH=""   # GPU 설치파일(.run)이 있는 autofs 디렉토리 (인자를 안 주면 이 값을 씀)
MOUNT_USER=""    # 마운트 판정용 계정: 대상 서버에서 /user/${MOUNT_USER} 가 붙는지 확인 (비우면 전부 scp)
GOSSH=""         # gossh 경로 (비우면 PATH 에서 찾음)

DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
BIN="$DIR/gpu_install_os8"

usage() {
    echo "사용법: bash gpu_install.sh [autofs경로] [list파일] [-p 동시대수]"
    echo "  autofs경로  기본값은 스크립트 맨 위의 AUTOFS_PATH"
    echo "  list파일    기본값 ./list (줄마다 '호스트 [드라이버버전]')"
    echo "  -p N        동시 처리 대수 (기본 10)"
}

# 위치 인자(autofs경로, list)와 옵션(-p N)을 나눈다. 옵션은 어느 위치에나 올 수 있다.
POS=()
OPTS=()
while [ $# -gt 0 ]; do
    case "$1" in
        -h|--help) usage; exit 0 ;;
        -p)
            if [ $# -lt 2 ]; then echo "-p 옵션에 값이 필요합니다" >&2; exit 2; fi
            OPTS+=("-p" "$2"); shift 2 ;;
        *) POS+=("$1"); shift ;;
    esac
done

# 첫 번째 위치 인자가 비어 있으면 AUTOFS_PATH 를 쓴다.
AUTOFS="${POS[0]:-$AUTOFS_PATH}"
if [ -z "$AUTOFS" ]; then
    usage
    echo "autofs 경로가 없습니다. 인자로 주거나 스크립트 맨 위 AUTOFS_PATH 를 채우세요." >&2
    exit 2
fi
LIST="${POS[1]:-}"

if [ ! -f "$BIN" ]; then
    echo "실행 파일이 없습니다: $BIN" >&2
    exit 2
fi
# zip·Windows 복사로 실행 권한이 빠진 경우
if [ ! -x "$BIN" ] && ! chmod +x "$BIN" 2>/dev/null; then
    echo "실행 권한이 없습니다: $BIN (chmod +x 로 권한을 주세요)" >&2
    exit 2
fi

ARGS=("$AUTOFS")
[ -n "$LIST" ] && ARGS+=("$LIST")
ARGS+=("${OPTS[@]}")
[ -n "$MOUNT_USER" ] && ARGS+=("-mount-user" "$MOUNT_USER")
[ -n "$GOSSH" ] && ARGS+=("-gossh" "$GOSSH")

# fail_list, logs/ 는 실행한 곳(현재 디렉토리)에 생긴다. 종료코드는 Go 의 것을 그대로 쓴다.
exec "$BIN" "${ARGS[@]}"
