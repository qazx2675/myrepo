#!/bin/bash
# build.sh - install_mn 빌드 (os8 = RHEL8, linux/amd64 정적 바이너리 → install_mn_os8)
#   bash build.sh       go vet + go test 를 먼저 돌리고 빌드
#   bash build.sh -n    vet·test 없이 빌드만
# os6 용은 만들지 않는다(2026-11-04 결정). 표준 라이브러리만 쓰므로 vendor/ 가 필요 없다.
# 소스를 바꿨다면 install_mn_os8 을 재빌드해 함께 커밋한다(회사에는 Go 가 없다).
set -eu
cd "$(dirname "$0")"

# go 가 PATH 에 없으면 흔한 설치 위치를 찾는다
if ! command -v go >/dev/null 2>&1; then
	for d in /opt/go1.20/bin /usr/local/go/bin /opt/go/bin; do
		if [ -x "$d/go" ]; then
			PATH="$d:$PATH"
			export PATH
			break
		fi
	done
fi
if ! command -v go >/dev/null 2>&1; then
	echo "[X] go 를 찾을 수 없습니다. (예: export PATH=/opt/go1.20/bin:\$PATH)" >&2
	exit 1
fi
echo "[i] $(go version)"

if [ "${1:-}" != "-n" ]; then
	echo "[i] go vet"
	go vet ./...
	echo "[i] go test"
	go test ./...
fi

CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags "-s -w" -o install_mn_os8 .
chmod +x install_mn_os8
echo "[O] 빌드 완료: $(pwd)/install_mn_os8"
ls -l install_mn_os8
