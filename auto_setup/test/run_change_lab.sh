#!/bin/bash
# run_change_lab.sh - 랩 서버 자기 자신(.58, Rocky 8)에 "진짜 gossh" 로 접속해 변경 메뉴(v0.7.0) 실동작을 한 번 확인한다.
#   서버 자신의 실제 /etc 와 서비스는 건드리지 않는다 — 대상은 fixture 뿐:
#     IP   : ip_change.conf 의 network_scripts_dir=/tmp/fx_net (fixture ifcfg-eth0)
#     LDAP : 엔진 래퍼가 -root /tmp/fx_root 를 강제 (fixture /etc 트리), -no-restart 없는 호출은 래퍼가 거부
#   흐름 (Go 테스트 TestChangeLabSelf 가 changeState 로 실행 — m 메뉴와 같은 코드):
#     IP 변경 → IP 원복 → LDAP dry-run → LDAP 적용 → LDAP 원복(-no-restart)
#   auto.appl 원복(m → 5)은 하지 않는다: 노드의 절대 경로 /root/auto_appl_restore.sh 를 실행하므로 fixture 로 가둘 수 없다
#   (그 단계는 test/run_change_os.sh 의 가짜 gossh 경로에서 검증).
#   실행 전후로 이 서버의 서비스 시작 시각·실제 설정 파일 상태·auto_setup 데몬을 비교해 "건드리지 않음" 을 확인한다.
#
# 사용: bash test/run_change_lab.sh       (root, 키 인증으로 자기 자신에 gossh 접속 가능해야 함. KEEP_SCRATCH=1 이면 /tmp/fx_* 보존)
#   끝에 "PASS=<n> FAIL=<m>" 요약 줄. 자기 접속이 안 되면 사유를 출력하고 종료코드 3 (이 경우 run_change_os.sh 결과로 대체).

ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd -P)
[[ -d /usr/local/go/bin ]] && export PATH="$PATH:/usr/local/go/bin"
export GOPROXY=off CGO_ENABLED=0 NO_COLOR=1
# shellcheck source=lib_change.sh
. "$ROOT/test/lib_change.sh"

PASS=0 FAIL=0
ok() { PASS=$((PASS + 1)); printf '  [PASS] %s\n' "$1"; }
ng() { FAIL=$((FAIL + 1)); printf '  [FAIL] %s\n' "$1"; [[ -n ${2:-} ]] && printf '         %s\n' "$2"; }

NET=/tmp/fx_net
FXROOT=/tmp/fx_root
CONF=/tmp/fx_conf
FXBIN=/tmp/fx_bin
NODELOG=/tmp/fx_lab_node.log
FXBUILD=/tmp/fx_build
cleanup() { [[ -n ${KEEP_SCRATCH:-} ]] || rm -rf "$NET" "$FXROOT" "$CONF" "$FXBIN" "$NODELOG" "$FXBUILD" /tmp/fx_ip_apply.sh /tmp/fx_ldap_apply.sh /tmp/fx_sys_before.txt /tmp/fx_sys_after.txt /tmp/fx_gotest.out; }
trap cleanup EXIT

[[ $(id -u) -eq 0 ]] || { echo "[X] root 로 실행하세요"; exit 1; }
command -v go > /dev/null 2>&1 || { echo "[X] go 를 찾을 수 없습니다"; exit 1; }
REALGOSSH=$(command -v gossh) || { echo "[X] gossh 가 PATH 에 없습니다 — 랩 실동작 불가"; exit 3; }
HOST=$(hostname -s)
IP=$(hostname -I 2> /dev/null | tr ' ' '\n' | grep -E '^[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+$' | head -n1)
[[ -n $IP && -n $HOST ]] || { echo "[X] hostname/IPv4 를 확인할 수 없습니다"; exit 3; }

echo "== 0. 사전 확인: gossh 로 자기 자신($HOST)에 키 인증 접속 =="
for d in "$NET" "$FXROOT" "$CONF" "$FXBIN" "$FXBUILD"; do
	if [[ -e $d ]]; then echo "[X] $d 가 이미 있습니다 — 이전 실행의 잔재면 직접 지우고 다시 실행하세요 (자동 삭제하지 않음)"; trap - EXIT; exit 1; fi
done
mkdir -p "$FXBIN"
printf '%s\n' "$HOST" > "$FXBIN/self.txt"
out=$("$REALGOSSH" -script -w "$FXBIN/self.txt" -t 10 'echo LABOK' 2>&1 < /dev/null)
if [[ $out != *"$HOST: LABOK"* ]]; then
	echo "[X] gossh 자기 접속 실패: $out"
	echo "    → 랩 실동작을 건너뜁니다 (run_change_os.sh 의 fixture 검증으로 대체)"
	echo "PASS=0 FAIL=0"
	exit 3
fi
ok "gossh -w <자기 자신> 접속 (키 인증, 비밀번호 인자 없음): $out"

echo "== 1. 준비: 엔진 빌드·래퍼·fixture =="
build() { (cd "$1" && GOFLAGS=-buildvcs=false go build -o "$3" "$2") || { echo "[X] 엔진 빌드 실패: $2"; echo "PASS=$PASS FAIL=$((FAIL + 1))"; exit 1; }; }
build "$ROOT/../ip_change" ./cmd/ip-change-engine "$FXBIN/ip-change-engine.real"
build "$ROOT/../ldap_setting" ./cmd/ldap-config-engine "$FXBIN/ldap-config-engine.real"
cat > "$FXBIN/ip-change-engine" << WRAP
#!/bin/bash
# 랩 실동작 래퍼: 임시 스크립트 경로를 /tmp 로 옮기고 -p 를 거부한다 (ifcfg 대상은 conf 의 network_scripts_dir=$NET)
for a in "\$@"; do [ "\$a" = -p ] && { echo "wrapper: -p 금지" >&2; exit 9; }; done
exec $FXBIN/ip-change-engine.real -remote-path /tmp/fx_ip_apply.sh "\$@"
WRAP
cat > "$FXBIN/ldap-config-engine" << WRAP
#!/bin/bash
# 랩 실동작 래퍼: -root $FXROOT 를 강제하고 -no-restart 없는 호출·-p·-print-script 는 거부한다
ok=0
for a in "\$@"; do
	case \$a in
		-no-restart) ok=1 ;;
		-p | -print-script | -root | -remote-path) echo "wrapper: 금지 인자 \$a" >&2; exit 9 ;;
	esac
done
[ \$ok = 1 ] || { echo "wrapper: -no-restart 없음 — 실행 거부" >&2; exit 9; }
exec $FXBIN/ldap-config-engine.real -root $FXROOT -remote-path /tmp/fx_ldap_apply.sh "\$@"
WRAP
cat > "$FXBIN/gossh" << WRAP
#!/bin/bash
# 진짜 gossh 를 그대로 부르되 노드 출력(표준출력)을 $NODELOG 에 남긴다 (SKIP-RESTART/RESTARTED 확인용)
$REALGOSSH "\$@" | tee -a $NODELOG
exit \${PIPESTATUS[0]}
WRAP
chmod 755 "$FXBIN"/ip-change-engine "$FXBIN"/ldap-config-engine "$FXBIN"/gossh
: > "$NODELOG"

# fixture: Rocky 8 형 노드 한 대분 — /tmp/fx_root (LDAP, ROOT) 와 /tmp/fx_net (ifcfg)
chg_fx_host "$FXBUILD" "$HOST" 8 "$IP" nofqdn
mv "$FXBUILD/$HOST" "$FXROOT"
rm -rf "$FXROOT/.fake" "$FXBUILD"
mkdir -p "$NET"
cp -p "$FXROOT/etc/sysconfig/network-scripts/ifcfg-eth0" "$FXROOT/etc/sysconfig/network-scripts/ifcfg-lo" "$NET/"
chg_write_conf "$CONF" "DEMO_BINDPW_alpha_Lab58q" "DEMO_BINDPW_beta_Lab58z" "$NET"
printf '%s\ta1\n' "$HOST" > "$CONF/assets.txt"
chmod 600 "$CONF/ldap_config.conf"
ok "fixture 준비: $NET (ifcfg-eth0 IPADDR=$IP), $FXROOT (Rocky 8 형 /etc 트리), conf=$CONF (퍼미션 $(stat -c %a "$CONF/ldap_config.conf"))"

# 실제 시스템 상태 스냅샷 (건드리지 않았는지 비교용)
SERVICES="sssd chronyd autofs nslcd ntpd NetworkManager network sshd"
sysstate() {
	local s f
	for s in $SERVICES; do
		echo "svc $s $(systemctl show -p ActiveState -p ActiveEnterTimestampMonotonic -p MainPID "$s" 2> /dev/null | paste -sd' ')"
	done
	for f in /etc/sysconfig/network-scripts/* /etc/sssd/sssd.conf /etc/chrony.conf /etc/resolv.conf /etc/openldap/ldap.conf /etc/autofs.conf \
		/etc/autofs_ldap_auth.conf /etc/auto.appl /etc/auto.appl.bak /etc/nslcd.conf /etc/ntp.conf /root/auto_appl_restore.sh /root/ldap_apply.sh /root/ip_change_apply.sh; do
		[[ -e $f ]] && stat -c 'file %n %Y %s' "$f" || echo "file $f (없음)"
	done
	echo "bin $(md5sum /usr/local/bin/auto_setup 2> /dev/null | cut -d' ' -f1)"
	echo "daemons $(pgrep -f 'auto_setup daemon' | sort | paste -sd,)"
}
sysstate > /tmp/fx_sys_before.txt
ok "이 서버 실제 상태 스냅샷 ($(wc -l < /tmp/fx_sys_before.txt)줄: 서비스 시작 시각, /etc 설정 파일, auto_setup 바이너리·데몬)"

echo "== 2. 변경 메뉴 흐름 (진짜 gossh → 자기 자신, fixture 만 대상) =="
(cd "$ROOT" && AS_LAB_HOST="$HOST" AS_LAB_CONF="$CONF" AS_LAB_IP_ENGINE="$FXBIN/ip-change-engine" AS_LAB_LDAP_ENGINE="$FXBIN/ldap-config-engine" \
	AS_LAB_GOSSH="$FXBIN/gossh" AS_LAB_NET="$NET" AS_LAB_ROOT="$FXROOT" go test -count=1 -run 'TestChangeLabSelf$' -v . > /tmp/fx_gotest.out 2>&1)
rc=$?
grep -E ' S6 (PASS|FAIL)' /tmp/fx_gotest.out | sed 's/^[[:space:]]*[A-Za-z0-9_.]*:[0-9]*: /  /' | sed 's/ S6 PASS/ [PASS]/; s/ S6 FAIL/ [FAIL]/'
gp=$(grep -cE ' S6 PASS' /tmp/fx_gotest.out)
gf=$(grep -cE ' S6 FAIL' /tmp/fx_gotest.out)
PASS=$((PASS + gp))
FAIL=$((FAIL + gf))
[[ $rc -ne 0 && $gf -eq 0 ]] && ng "Go 테스트 비정상 종료 (rc=$rc)" "$(tail -n 12 /tmp/fx_gotest.out | tr '\n' ' ')"
[[ $gp -eq 0 ]] && ng "Go 테스트가 점검 줄을 내지 않음" "$(tail -n 8 /tmp/fx_gotest.out | tr '\n' ' ')"

echo "== 3. 사후 확인 =="
sysstate > /tmp/fx_sys_after.txt
if diff -q /tmp/fx_sys_before.txt /tmp/fx_sys_after.txt > /dev/null; then
	ok "서비스 시작 시각·실제 /etc 설정 파일·auto_setup 바이너리·데몬 pid 가 실행 전과 같음 (재시작·변경 없음)"
else
	ng "이 서버의 실제 상태가 달라짐" "$(diff /tmp/fx_sys_before.txt /tmp/fx_sys_after.txt | head -n 6 | tr '\n' '|')"
fi
if grep -q 'APPLY|RESTARTED' "$NODELOG"; then ng "노드 출력에 RESTARTED 가 있음"; else ok "노드 출력에 RESTARTED 0건"; fi
n=$(grep -c 'APPLY|SKIP-RESTART|.*(NO_RESTART)' "$NODELOG")
if [[ $n -gt 0 ]]; then ok "원복에서 서비스를 SKIP-RESTART (NO_RESTART) 로 건너뜀 ($n건)"; else ng "원복의 SKIP-RESTART (NO_RESTART) 줄이 없음"; fi
echo "  -- 노드 출력(진짜 gossh) 발췌 (RESULT/APPLY, DIFF 줄 제외):"
grep -E 'RESULT\||APPLY\|(CHANGED|SKIP-RESTART|RESTORED|INFO)' "$NODELOG" | sed 's/^/     /' | head -n 24
rm -f /tmp/fx_sys_before.txt /tmp/fx_sys_after.txt /tmp/fx_gotest.out

echo
echo "================ 랩 실동작 결과 ================"
echo "PASS=$PASS FAIL=$FAIL"
[[ $FAIL -eq 0 ]]
