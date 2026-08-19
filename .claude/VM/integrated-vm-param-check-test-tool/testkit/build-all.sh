#!/usr/bin/env bash
# build-all.sh — vm-param-check / vc-test-env 두 도구를 오프라인(vendor/) 빌드.
# 폐쇄망 서버에서 이 폴더를 통째로 옮긴 뒤 가장 먼저 실행하는 스크립트.
set -euo pipefail
cd "$(dirname "$0")/.."   # testkit/ -> integrated-vm-param-check-test-tool/

echo "[1/2] vm-param-check 빌드 중..."
( cd vm-param-check && go build -mod=vendor -o vm-param-check . )
echo "  -> vm-param-check/vm-param-check 생성됨"

echo "[2/2] vc-test-env 빌드 중..."
( cd vc-test-env && go build -mod=vendor -o vc-test-env . )
echo "  -> vc-test-env/vc-test-env 생성됨"

echo
echo "빌드 완료. 다음: testkit/run-tests.sh 로 vcsim 기동 + 체크/교정 테스트 실행"
