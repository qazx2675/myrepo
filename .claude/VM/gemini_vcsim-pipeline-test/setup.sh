#!/usr/bin/env bash
# setup.sh — 폐쇄망(오프라인) 테스트 준비. vendor/ 안의 의존성만 사용, 인터넷 접속 시도 안 함.
# 이 프로젝트는 실행 바이너리가 없고 `go test`로 직접 돌리는 통합 테스트 프레임워크다
# (README.md 참고). 이 스크립트는 공통 govendor 심볼릭 링크만 걸어준다 — 실제 테스트는
# 아래 안내대로 go test 명령을 직접 실행한다.
set -euo pipefail
cd "$(dirname "$0")"

# 공통 govendor 로 이동한 의존성을 이 디렉터리 이름의 vendor/ 로 연결한다
# (실제 파일은 ../../공통/govendor/govmomi-0.55.1-vcsim 에 있음; 여러 프로젝트가 동일 govmomi 버전을 공유)
rm -rf vendor 2>/dev/null; ln -s "../../공통/govendor/govmomi-0.55.1-vcsim" vendor

echo "vendor/ 연결 완료. 다음으로 테스트를 실행하세요:"
echo "  go test -mod=vendor -v -timeout 180s ./..."
