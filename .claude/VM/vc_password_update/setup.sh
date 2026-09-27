#!/usr/bin/env bash
# setup.sh — 폐쇄망(오프라인) 빌드.
# vendor/ 안의 의존성만 사용하며 인터넷 접속을 시도하지 않습니다.
set -euo pipefail
cd "$(dirname "$0")"

# 공통 govendor 로 이동한 의존성을 이 디렉터리 이름의 vendor/ 로 연결한다
# (실제 파일은 ../../공통/govendor/govmomi-0.55.1-vc-password-update 에 있음;
#  ssoadmin/sts 패키지가 필요해 다른 프로젝트의 govmomi vendor 사본과는 별도로 관리)
rm -rf vendor 2>/dev/null; ln -s "../../공통/govendor/govmomi-0.55.1-vc-password-update" vendor

if ! command -v go >/dev/null 2>&1; then
  echo "오류: go 를 찾을 수 없습니다. Go 툴체인을 먼저 설치하세요." >&2
  exit 2
fi

if ! command -v openssl >/dev/null 2>&1; then
  echo "오류: openssl 을 찾을 수 없습니다 (비밀번호 복호화에 필요)." >&2
  exit 2
fi

if [ ! -d vendor ]; then
  echo "오류: vendor/ 디렉터리가 없습니다. 저장소를 통째로 내려받았는지 확인하세요." >&2
  exit 2
fi

export GOFLAGS=-mod=vendor
export GOPROXY=off

go build -mod=vendor -o vc_password_update .

echo
echo "빌드 완료: $(pwd)/vc_password_update"
echo "다음으로 진행하십시오: README.md 의 '사용 방법' 참고 (run.sh 로 실행 권장)"
