#!/bin/bash
# build_os6.sh - os6_mgmt(RHEL6) 용 정적 빌드 → auto_setup_os6 (Go 1.20 서버에서 실행)
set -euo pipefail

cd "$(dirname "$0")"

CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o auto_setup_os6 .

echo "[O] 빌드 완료: $(pwd)/auto_setup_os6"
if command -v file >/dev/null 2>&1; then
	file auto_setup_os6
else
	ls -l auto_setup_os6
fi
echo "    os6_mgmt 의 os6_autosetup 경로에 'auto_setup' 이름으로 복사하세요."
