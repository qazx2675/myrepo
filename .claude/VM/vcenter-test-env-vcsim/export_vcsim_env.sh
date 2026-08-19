#!/bin/bash
# 다른 서버로 vcsim 환경 (바이너리 + 추출된 레시피) 복사 및 세팅 스크립트

if [ "$#" -ne 1 ]; then
    echo "사용법: bash $0 <대상서버IP>"
    echo "예시: bash $0 192.168.0.60"
    exit 1
fi

TARGET_IP=$1
TARGET_USER="root"
VCSIM_DIR=$(basename "$PWD")

echo "========================================"
echo " vcsim 환경 이관 스크립트"
echo " 대상 서버: $TARGET_USER@$TARGET_IP"
echo "========================================"

echo "[1/4] 전송을 위한 임시 압축 파일을 생성합니다..."
# 현재 폴더(vcsim)와 사용자 홈의 레시피 폴더(.vc-test-env)를 압축
# -C 옵션을 통해 경로를 이동하며 압축하여 압축 해제 시 구조가 유지되도록 함
tar -czvf /tmp/vcsim_with_recipes.tar.gz -C "$PWD/../" "$VCSIM_DIR" -C "$HOME" ".vc-test-env" 

if [ $? -ne 0 ]; then
    echo "압축 실패! ~/.vc-test-env 폴더가 존재하는지 확인해주세요 (최소 1회 이상 vc-test-env를 실행해야 생성됩니다)."
    exit 1
fi

echo "[2/4] 대상 서버($TARGET_IP)로 압축 파일을 전송합니다."
echo " (대상 서버의 비밀번호 입력이 필요할 수 있습니다)"
scp /tmp/vcsim_with_recipes.tar.gz $TARGET_USER@$TARGET_IP:/root/

if [ $? -ne 0 ]; then
    echo "파일 전송 실패!"
    exit 1
fi

echo "[3/4] 대상 서버에서 압축을 해제하고 환경을 세팅합니다."
echo " (대상 서버의 비밀번호 입력이 다시 필요할 수 있습니다)"
ssh $TARGET_USER@$TARGET_IP "cd /root && tar -xzvf vcsim_with_recipes.tar.gz && rm -f vcsim_with_recipes.tar.gz"

if [ $? -ne 0 ]; then
    echo "원격 압축 해제 실패!"
    exit 1
fi

echo "[4/4] 로컬 임시 파일을 정리합니다..."
rm -f /tmp/vcsim_with_recipes.tar.gz

echo "========================================"
echo "이관 완료! 대상 서버($TARGET_IP)에 접속하신 후 아래 경로에서 바로 실행할 수 있습니다."
echo "경로: /root/$VCSIM_DIR"
echo "명령어: ./vc-test-env"
