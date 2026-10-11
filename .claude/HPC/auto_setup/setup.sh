#!/bin/bash
# setup.sh - auto_setup 빌드 → /usr/local/bin 설치 → cron 매분 ensure · tmpfiles 정리 제외 등록 (멱등)
set -euo pipefail

cd "$(dirname "$0")"

if [ "$(id -u)" -ne 0 ]; then
	echo "[X] root 로 실행하세요 (raw ICMP, /usr/local/bin, cron 등록 필요)" >&2
	exit 1
fi
# PATH 에 go 가 없으면 흔한 설치 위치(/usr/local/go, /opt/go*)를 찾아 PATH 에 추가
if ! command -v go >/dev/null 2>&1; then
	for d in /usr/local/go/bin /opt/go*/bin /usr/lib/golang/bin; do
		if [ -x "$d/go" ]; then
			export PATH="$PATH:$d"
			echo "[i] go 를 PATH 에서 찾지 못해 $d 를 사용합니다"
			break
		fi
	done
fi
if ! command -v go >/dev/null 2>&1; then
	echo "[X] go 를 찾을 수 없습니다 (PATH 확인, 예: export PATH=\$PATH:/usr/local/go/bin)" >&2
	exit 1
fi

echo "[1/6] 빌드"
go build -o auto_setup .

echo "[2/6] /usr/local/bin/auto_setup 설치"
install -m 0755 auto_setup /usr/local/bin/auto_setup
if [ -f conf/auto_setup.conf ]; then
	mkdir -p /etc/auto_setup
	install -m 0600 conf/auto_setup.conf /etc/auto_setup/auto_setup.conf
	echo "    conf/auto_setup.conf → /etc/auto_setup/auto_setup.conf"
fi

echo "[3/6] cron 등록 (매분 ensure)"
cron_line='* * * * * /usr/local/bin/auto_setup ensure >/dev/null 2>&1'
current="$(crontab -l 2>/dev/null || true)"
if printf '%s\n' "$current" | grep -qF '/usr/local/bin/auto_setup ensure'; then
	echo "    이미 등록됨"
else
	{
		if [ -n "$current" ]; then
			printf '%s\n' "$current"
		fi
		printf '%s\n' "$cron_line"
	} | crontab -
	echo "    등록 완료"
fi

echo "[4/6] tmpfiles 정리 제외 (/etc/tmpfiles.d/auto_setup.conf)"
conf=/etc/tmpfiles.d/auto_setup.conf
want='x /tmp/auto_setup'
if [ -f "$conf" ] && grep -qxF "$want" "$conf"; then
	echo "    이미 등록됨"
else
	mkdir -p /etc/tmpfiles.d
	printf '%s\n' "$want" > "$conf"
	echo "    등록 완료"
fi

# ---- v0.7.0: TUI 변경 메뉴(m)용 엔진 + conf 샘플 (선택 기능 — 소스가 없거나 빌드가 안 돼도 위 설치는 그대로 유효) ----
base="$(cd .. && pwd)"

echo "[5/6] 변경 엔진 빌드·설치 (ip-change-engine, ldap-config-engine)"
# build_engine <소스 디렉터리> <cmd 디렉터리> <설치 이름>: 폐쇄망 빌드(GOPROXY=off, 정적), 실패해도 설치 전체를 중단하지 않음
build_engine() {
	local src="$1" cmd="$2" name="$3" tmp
	if [ ! -d "$src/cmd/$cmd" ]; then
		echo "    건너뜀: 소스 없음 ($src) — 변경 메뉴를 쓰려면 저장소 전체에서 실행하거나 $name 을 /usr/local/bin 에 직접 두세요"
		return 0
	fi
	tmp="$(mktemp /tmp/.as_engine.XXXXXX)"
	if (cd "$src" && GOPROXY=off CGO_ENABLED=0 go build -o "$tmp" "./cmd/$cmd"); then
		install -m 0755 "$tmp" "/usr/local/bin/$name"
		echo "    $name → /usr/local/bin/$name"
	else
		echo "[!] $name 빌드 실패 — 건너뜁니다 (Go 버전과 $src/go.mod 의 go 지시자를 확인하세요)" >&2
	fi
	rm -f "$tmp"
}
build_engine "$base/ip_change" ip-change-engine ip-change-engine
build_engine "$base/ldap_setting" ldap-config-engine ldap-config-engine

echo "[6/6] 변경 conf 샘플 설치 (/etc/auto_setup — 이미 있는 파일은 덮어쓰지 않음)"
# install_sample <샘플 경로> <대상 파일 이름> <모드>
install_sample() {
	local sample="$1" dst="/etc/auto_setup/$2" mode="$3"
	if [ ! -f "$sample" ]; then
		echo "    건너뜀: 샘플 없음 ($sample)"
		return 0
	fi
	if [ -e "$dst" ] || [ -L "$dst" ]; then
		echo "    $dst 이미 있음 (유지)"
		return 0
	fi
	mkdir -p /etc/auto_setup
	install -m "$mode" "$sample" "$dst"
	echo "    $2 설치 (모드 $mode) — 실제 값으로 수정하세요"
}
install_sample "$base/ip_change/conf/ip_change.conf.sample" ip_change.conf 0644
install_sample "$base/ldap_setting/conf/ldap_config.conf.sample" ldap_config.conf 0600
install_sample "$base/ldap_setting/conf/assets.txt.sample" assets.txt 0644
if [ -f /etc/auto_setup/ldap_config.conf ]; then
	m="$(stat -c '%a' /etc/auto_setup/ldap_config.conf 2>/dev/null || true)"
	if [ -n "$m" ] && [ "$m" != 600 ]; then
		echo "[!] /etc/auto_setup/ldap_config.conf 권한이 $m 입니다 — bindpw 가 들어가므로 chmod 600 을 권장합니다" >&2
	fi
fi

echo "[O] 설치 완료. 최대 1분 안에 cron 이 데몬을 기동합니다."
