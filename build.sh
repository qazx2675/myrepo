#!/bin/bash
# 리눅스 빌드: bin/awx_copy (업로더), 선택적으로 dist/awx_fetch.exe (윈도우 다운로더)
# 표준 라이브러리만 쓴다. go 1.20 이상. RHEL6(커널 2.6.32)용은 go 1.20 으로 빌드한다.
set -eu
cd "$(dirname "$(readlink -f "$0")")"
export CGO_ENABLED=0
LDF="-s -w"
mkdir -p bin dist
echo "go: $(go version)"
go vet ./...
go build -trimpath -ldflags "$LDF" -o bin/awx_copy .
echo "빌드: bin/awx_copy"
if [ "${1:-}" = "all" ]; then
    GOOS=windows GOARCH=amd64 go build -trimpath -ldflags "$LDF -H windowsgui" -o dist/awx_fetch.exe .  # cmd 창 없음
    echo "빌드: dist/awx_fetch.exe"
fi
chmod 755 bin/awx_copy
echo "설치: sudo ./install.sh  (→ /usr/local/bin/awx_copy)"
