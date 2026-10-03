#!/bin/bash
# build_os6.sh - os6_mgmt(RHEL6) 용 정적 빌드 → auto_setup_os6 (Go 1.20 서버에서 실행)
set -euo pipefail

cd "$(dirname "$0")"

# os6 빌드는 Go 1.20 로: /opt/go1.20 이 있으면 우선 사용, 없으면 PATH 의 go (/usr/local/go/bin 도 탐색)
if [ -x /opt/go1.20/bin/go ]; then
	export PATH="/opt/go1.20/bin:$PATH"
elif ! command -v go >/dev/null 2>&1 && [ -x /usr/local/go/bin/go ]; then
	export PATH="$PATH:/usr/local/go/bin"
fi
if ! command -v go >/dev/null 2>&1; then
	echo "[X] go 를 찾을 수 없습니다 (Go 1.20 서버에서 실행: export PATH=/opt/go1.20/bin:\$PATH)" >&2
	exit 1
fi
echo "[i] $(go version)"

CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o auto_setup_os6 .

echo "[O] 빌드 완료: $(pwd)/auto_setup_os6"
if command -v file >/dev/null 2>&1; then
	file auto_setup_os6
else
	ls -l auto_setup_os6
fi
echo "    os6_mgmt 의 os6_autosetup 경로에 'auto_setup' 이름으로 복사하세요."
