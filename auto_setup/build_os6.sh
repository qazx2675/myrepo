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

# v0.7.0: 변경 엔진(ip-change-engine, ldap-config-engine)도 같은 Go 로 정적 빌드 (소스가 없으면 건너뜀)
# 결과: ../ip_change/bin/ip-change-engine_os6, ../ldap_setting/bin/ldap-config-engine_os6
# build_engine_os6 <소스 디렉터리> <cmd 디렉터리 = 엔진 이름>
build_engine_os6() {
	local src="$1" name="$2"
	if [ ! -d "$src/cmd/$name" ]; then
		echo "[i] 건너뜀: 엔진 소스 없음 ($src)"
		return 0
	fi
	mkdir -p "$src/bin"
	(cd "$src" && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o "bin/${name}_os6" "./cmd/$name")
	chmod +x "$src/bin/${name}_os6"
	echo "[O] 빌드 완료: $src/bin/${name}_os6"
}
build_engine_os6 ../ip_change ip-change-engine
build_engine_os6 ../ldap_setting ldap-config-engine
