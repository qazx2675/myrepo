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

echo "[1/4] 빌드"
go build -o auto_setup .

echo "[2/4] /usr/local/bin/auto_setup 설치"
install -m 0755 auto_setup /usr/local/bin/auto_setup

echo "[3/4] cron 등록 (매분 ensure)"
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

echo "[4/4] tmpfiles 정리 제외 (/etc/tmpfiles.d/auto_setup.conf)"
conf=/etc/tmpfiles.d/auto_setup.conf
want='x /tmp/auto_setup'
if [ -f "$conf" ] && grep -qxF "$want" "$conf"; then
	echo "    이미 등록됨"
else
	mkdir -p /etc/tmpfiles.d
	printf '%s\n' "$want" > "$conf"
	echo "    등록 완료"
fi

echo "[O] 설치 완료. 최대 1분 안에 cron 이 데몬을 기동합니다."
