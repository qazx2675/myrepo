#!/bin/bash

# 사용법: ./바이너리셋업.sh [설치경로]
# 예: ./바이너리셋업.sh /usr/local/bin

TARGET_DIR="${1:-/usr/local/bin}"

echo "========================================"
echo " VM 업무 관련 바이너리 설치 스크립트"
echo " 대상 경로: $TARGET_DIR"
echo "========================================"

if [ ! -d "$TARGET_DIR" ]; then
    echo "해당 경로가 존재하지 않아 생성합니다: $TARGET_DIR"
    mkdir -p "$TARGET_DIR"
    if [ $? -ne 0 ]; then
        echo "오류: 경로를 생성할 수 없습니다. 권한이 필요할 수 있습니다 (sudo 필요)."
        exit 1
    fi
fi

# 현재 스크립트가 위치한 디렉토리의 절대 경로 확인 (.claude)
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
VM_DIR="$SCRIPT_DIR/VM 업무"

if [ ! -d "$VM_DIR" ]; then
    echo "오류: '$VM_DIR' 폴더를 찾을 수 없습니다."
    exit 1
fi

echo "바이너리 검색 및 복사를 시작합니다..."
count=0

# VM 업무 폴더 내에서 .sh, .bat, .ps1, .exe 파일을 제외한 모든 실행 가능한 파일을 찾아 복사합니다.
find "$VM_DIR" -type f -executable ! -name "*.sh" ! -name "*.bat" ! -name "*.ps1" ! -name "*.exe" | while read -r BIN_PATH; do
    # vendor 디렉토리 등 의존성 폴더 내부의 실행파일은 제외
    if [[ "$BIN_PATH" == *"/vendor/"* ]] || [[ "$BIN_PATH" == *"/.git/"* ]]; then
        continue
    fi

    BIN_NAME=$(basename "$BIN_PATH")
    
    echo "복사 중: $BIN_NAME -> $TARGET_DIR/$BIN_NAME"
    cp -f "$BIN_PATH" "$TARGET_DIR/"
    if [ $? -eq 0 ]; then
        count=$((count+1))
    else
        echo "오류: '$BIN_NAME' 복사 실패. 권한이 필요할 수 있습니다 (sudo ./바이너리셋업.sh)."
    fi
done

echo "========================================"
echo "완료: '$TARGET_DIR' 경로에 복사가 완료되었습니다."
echo "이제 터미널 어디서든 해당 바이너리를 전역 명령어로 사용할 수 있습니다."
