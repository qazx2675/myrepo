#!/usr/bin/env bash
# setup.sh — 폐쇄망(오프라인) 빌드. 외부 의존성이 없어(표준 라이브러리만) vendor/ 가 필요 없고, 인터넷 접속 시도 안 함.
set -euo pipefail
cd "$(dirname "$0")"

if ! command -v go >/dev/null 2>&1; then
  echo "오류: go 를 찾을 수 없습니다. Go 툴체인을 먼저 설치하십시오." >&2
  echo "      (Go 없이 쓰려면 저장소의 빌드 완료 바이너리 bin/biostool, bin/biostool_os6 를 그대로 사용하십시오.)" >&2
  exit 2
fi

export GOPROXY=off
export GOFLAGS=-mod=vendor

mkdir -p bin
echo "빌드: $(go version)"
CGO_ENABLED=0 go build -mod=vendor -trimpath -ldflags "-s -w" -o bin/biostool ./cmd/biostool

# RHEL6(커널 3 미만)에서는 Go 1.24 이상으로 만든 바이너리가 동작하지 않는다.
kmajor="$(uname -r | cut -d. -f1)"
case "$kmajor" in '' | *[!0-9]*) kmajor=99 ;; esac
if [ "$kmajor" -lt 3 ]; then
  echo "주의: 이 시스템은 커널 3 미만(RHEL6)입니다. bin/biostool 은 동작하지 않을 수 있으니" >&2
  echo "      Go 1.20.x 로 'bash build_os6.sh' 를 실행해 bin/biostool_os6 를 쓰십시오." >&2
fi

echo
echo '완료. 다음으로 진행하십시오:'
echo '  1) cp bios.conf.example bios.conf   # user= 에 BMC 계정 ID 입력'
echo '  2) bash encrypt.sh                  # BMC 비밀번호 암호화 -> pass.enc + key.bin'
echo '  3) cp user.txt.example user.txt     # 점검할 대상(IP / hostname-m / hostname)을 한 줄씩'
echo '  4) bash bios_check.sh --profile VM -dry-run   # 설정 없이 점검만 (처음 쓰는 모델은 FIRST_RUN.md 먼저)'
