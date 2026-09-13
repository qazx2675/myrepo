#!/usr/bin/env bash
# setup.sh — ETX(폐쇄망) 송신측 오프라인 빌드.
# 인터넷 접속을 시도하지 않고 저장소 안의 것만으로 copy-send를 빌드합니다.
# (copy-widget은 Windows 전용이라 Windows에서 build.bat 로 따로 빌드합니다.)
set -euo pipefail
cd "$(dirname "$0")"

if ! command -v go >/dev/null 2>&1; then
  echo "오류: go 를 찾을 수 없습니다. Go 툴체인을 먼저 설치하십시오." >&2
  exit 2
fi

export GOPROXY=off
# go.mod가 요구하는 버전보다 로컬 go가 낮으면 자동으로 툴체인을 인터넷에서
# 내려받으려고 시도하는데(GOTOOLCHAIN=auto가 기본값), 폐쇄망에서는 이게 조용히
# 실패합니다. go.mod의 go 버전을 이 host에 실제 설치된 버전 이하로 맞춰두고,
# 여기서도 명시적으로 local로 고정해 이중으로 막습니다.
export GOTOOLCHAIN=local
# 이 프로젝트는 표준 라이브러리만 사용하므로 vendor/ 없이도 오프라인 빌드됩니다.

mkdir -p bin
printf '  빌드: copy-send ... '
CGO_ENABLED=0 go build -o bin/copy-send ./cmd/copy-send
echo '-> bin/copy-send'

chmod +x send.sh
echo
echo '완료. 다음으로 진행하십시오:'
echo '  1) conf/copy_setting.conf.sample 을 conf/copy_setting.conf 로 복사해 실제 값으로 채우기'
echo '     (Windows 쪽 conf/copy_setting.conf 와 반드시 같은 내용이어야 합니다)'
echo '  2) {user}_copy.txt 파일 준비'
echo '  3) ./send.sh -user <계정명>'
