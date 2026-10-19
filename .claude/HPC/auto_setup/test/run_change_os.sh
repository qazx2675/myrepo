#!/bin/bash
# run_change_os.sh - 변경 메뉴(v0.7.0) 의 RHEL 7/8/9 판정 검증: 실제 엔진 스크립트를 OS 별 fixture(+가짜 gossh)로 실행해 단언한다.
#   fixture = test/lib_change.sh 의 가짜 노드 9대(OS 7/8/9 혼합, redhat-release / os-release, s4 접두 호스트 포함).
#   실제 /etc·서비스는 건드리지 않는다 (가짜 gossh 가 /etc 를 호스트 루트 아래로 옮겨 실행, systemctl 은 호출만 기록하는 shim).
#
#   1. IP   : RHEL 7·8·9 모두 /etc/sysconfig/network-scripts 의 ifcfg 탐색·IPADDR/GATEWAY 갱신(+.bak.<STAMP>), 재시작 호출 없음,
#             원복이 백업을 복원. RHEL 9 에서도 rhel9_path 없이 동작 (rhel9_path 를 주면 9 만 그 경로를 쓰는지로 OS 판정도 확인).
#   2. LDAP : RHEL7 → nslcd.conf·ntp.conf (sssd·chrony 아님), RHEL 8/9 → sssd.conf·chrony.conf, s4 접두는 8 에서도 nslcd/ntp.
#             apply 는 서비스를 재시작하지 않고, -no-restart 원복도 RESTARTED·systemctl restart 가 0건 (대조군: -no-restart 없는 원복은 재시작).
#             이 검증은 lib_common.sh 의 "ROOT 가 있으면 재시작 건너뜀" 보호를 끄고(FAKE_UNGUARD=1) 해서, -no-restart 만이 막고 있음을 본다.
#   3. bindpw 미노출: auto_setup 쪽(화면·기록·로그·임시 파일·gossh 인자) 교차 점검 — Go 테스트 TestChangeS6World 를 같은 fixture 로 실행.
#
# 사용: bash test/run_change_os.sh      (KEEP_SCRATCH=1 이면 작업 디렉터리 보존)   — 끝에 "PASS=<n> FAIL=<m>" 요약 줄
# 필요: go (엔진·auto_setup 테스트 빌드), bash, root 불필요

ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd -P)
[[ -d /usr/local/go/bin ]] && export PATH="$PATH:/usr/local/go/bin"
command -v go > /dev/null 2>&1 || { echo "[X] go 를 찾을 수 없습니다"; echo "PASS=0 FAIL=1"; exit 1; }
export GOPROXY=off CGO_ENABLED=0 NO_COLOR=1
# shellcheck source=lib_change.sh
. "$ROOT/test/lib_change.sh"

W=$(mktemp -d /tmp/as_s6_os.XXXXXX)
trap '[[ -n ${KEEP_SCRATCH:-} ]] || rm -rf "$W"' EXIT

PASS=0 FAIL=0
ok() { PASS=$((PASS + 1)); printf '  [PASS] %s\n' "$1"; }
ng() { FAIL=$((FAIL + 1)); printf '  [FAIL] %s\n' "$1"; [[ -n ${2:-} ]] && printf '         %s\n' "$2"; }
chk() { local d=$1; shift; if "$@" > /dev/null 2>&1; then ok "$d"; else ng "$d" "조건 실패: $*"; fi; }
chkn() { local d=$1; shift; if "$@" > /dev/null 2>&1; then ng "$d" "있으면 안 됨: $*"; else ok "$d"; fi; }

BIN=$W/bin
mkdir -p "$BIN" "$W/os6"
build() { (cd "$1" && GOFLAGS=-buildvcs=false go build -o "$3" "$2") || { echo "[X] 엔진 빌드 실패: $2"; echo "PASS=0 FAIL=1"; exit 1; }; }
build "$ROOT/../ip_change" ./cmd/ip-change-engine "$BIN/ip-change-engine"
build "$ROOT/../ldap_setting" ./cmd/ldap-config-engine "$BIN/ldap-config-engine"
IPENG=$BIN/ip-change-engine
LENG=$BIN/ldap-config-engine
cp "$ROOT/test/fake_gossh.sh" "$BIN/gossh"
cp "$ROOT/test/fake_gossh.sh" "$W/os6/gossh"
chmod +x "$BIN/gossh" "$W/os6/gossh"
G=$BIN/gossh
G6=$W/os6/gossh

BPW_A="DEMO_BINDPW_alpha_os7Kq1"
BPW_B="DEMO_BINDPW_beta_os9Zr4"
chg_write_conf "$W/conf" "$BPW_A" "$BPW_B"
CONF=$W/conf

# new_world <이름> [both]: 노드 fixture 와 원본 사본(.orig), 기록 디렉터리를 새로 만들고 FAKE_* 를 가리킨다
new_world() {
	local d=$W/$1
	rm -rf "$d"
	mkdir -p "$d"
	chg_make_nodes "$d/nodes" "${2:-}"
	cp -a "$d/nodes" "$d/orig"
	NODES=$d/nodes ORIG=$d/orig LOGD=$d/fake OUT=$d
	export FAKE_ROOTS=$NODES FAKE_LOGDIR=$LOGD
	unset FAKE_VIA
	mkdir -p "$LOGD"
}
same() { cmp -s "$NODES/$1/$2" "$ORIG/$1/$2"; }     # <호스트> <상대경로>: 원본과 같음
nobak() { ! ls "$NODES/$1/$2".bak.* > /dev/null 2>&1; } # <호스트> <상대경로>: 백업 없음 (= 건드리지 않음)
hasbak() { ls "$NODES/$1/$2".bak.* > /dev/null 2>&1; }
newip() { local ip; ip=$(cat "$ORIG/$1/.fake/ips"); echo "${ip/10.20./10.30.}"; }
nlog() { grep -E -- "$1" "$LOGD/node.log"; }

IPOK="rh7-n01 rh8-n01 rh9-n01 rh9-n02 s4rh8-n01 rh8-noasset"
NET=etc/sysconfig/network-scripts

echo "== 1. IP 변경 (ip-change-engine, ifcfg) — RHEL 7·8·9 =="
new_world ip
: > "$OUT/targets.txt"
for h in $CHG_HOSTS; do printf '%s %s\n' "$h" "$(newip "$h")" >> "$OUT/targets.txt"; done
"$IPENG" -config "$CONF/ip_change.conf" -targets "$OUT/targets.txt" -gossh "$G" > "$OUT/ip_apply.out" 2>&1
chk "rhel9_path 없이 실행 (ip_change.conf 에 rhel9_path 줄 없음)" test -z "$(grep -E '^[[:space:]]*rhel9_path' "$CONF/ip_change.conf")"
chk "엔진 헤더가 network_scripts_dir=/etc/sysconfig/network-scripts" grep -q 'network_scripts_dir=/etc/sysconfig/network-scripts' "$OUT/ip_apply.out"
chkn "RHEL9 이상 경로 줄 없음" grep -q 'RHEL9 이상 경로' "$OUT/ip_apply.out"
for h in $IPOK; do
	n=$(newip "$h")
	o=$(cat "$ORIG/$h/.fake/ips")
	os=$(sed -n 's/^VERSION_ID="\([0-9]*\).*/\1/p' "$ORIG/$h/etc/os-release")
	f=$NODES/$h/$NET/ifcfg-eth0
	chk "IP [$h RHEL$os] ifcfg-eth0 IPADDR=$n GATEWAY=${n%.*}.1" bash -c "grep -qx 'IPADDR=$n' '$f' && grep -qx 'GATEWAY=${n%.*}.1' '$f'"
	chk "IP [$h] 백업 ifcfg-eth0.bak.<STAMP> 이 원본과 같음" bash -c "b=\$(ls '$f'.bak.* | head -1) && cmp -s \"\$b\" '$ORIG/$h/$NET/ifcfg-eth0'"
	chk "IP [$h] ifcfg-lo 는 그대로" same "$h" "$NET/ifcfg-lo"
	chk "IP [$h] 엔진 출력 '$o -> $n'" grep -Eq "^$h +$o -> $n +\(GW ${n%.*}\.1\)" "$OUT/ip_apply.out"
done
chk "IP [rh8-n02] ifcfg 없음 → FAIL (ifcfg 파일을 찾지 못했습니다)" grep -Eq '^rh8-n02 +FAIL: .*ifcfg' "$OUT/ip_apply.out"
chk "IP [rh8-n02] 아무것도 바뀌지 않음" diff -rq "$NODES/rh8-n02" "$ORIG/rh8-n02"
chk "IP [rh8-dead] UNREACHABLE" grep -Eq '^rh8-dead +UNREACHABLE' "$OUT/ip_apply.out"
chk "IP [rh7-n02] os8 에서 UNREACHABLE (os8_down)" grep -Eq '^rh7-n02 +UNREACHABLE' "$OUT/ip_apply.out"
printf 'rh7-n02 %s\n' "$(newip rh7-n02)" > "$OUT/t6.txt"
"$IPENG" -config "$CONF/ip_change.conf" -targets "$OUT/t6.txt" -gossh "$G6" > "$OUT/ip_apply6.out" 2>&1
chk "IP [rh7-n02] os6 경유 gossh 로는 변경 성공" grep -Eq "^rh7-n02 +10.20.7.12 -> $(newip rh7-n02)" "$OUT/ip_apply6.out"
chk "네트워크 재시작·서비스 호출 0건 (systemctl/service/nmcli/ifup/ifdown shim 기록 없음)" test ! -s "$LOGD/systemctl.log"

: > "$OUT/rb.txt"
for h in $IPOK rh7-n02; do echo "$h" >> "$OUT/rb.txt"; done
"$IPENG" -config "$CONF/ip_change.conf" -targets "$OUT/rb.txt" -gossh "$G" -rollback > "$OUT/ip_rb.out" 2>&1
printf 'rh7-n02\n' > "$OUT/rb6.txt"
"$IPENG" -config "$CONF/ip_change.conf" -targets "$OUT/rb6.txt" -gossh "$G6" -rollback > "$OUT/ip_rb6.out" 2>&1
for h in $IPOK rh7-n02; do
	chk "IP 원복 [$h] ifcfg-eth0 이 원본과 같음" same "$h" "$NET/ifcfg-eth0"
done
chk "IP 원복 [rh9-n01] 엔진 출력 '복원 ... -> IPADDR 10.20.9.11'" grep -Eq '^rh9-n01 +복원 .* -> IPADDR 10\.20\.9\.11' "$OUT/ip_rb.out"
chk "IP 원복 후에도 .bak 백업은 남음" hasbak rh8-n01 "$NET/ifcfg-eth0"
chk "IP 원복에서도 재시작·서비스 호출 0건" test ! -s "$LOGD/systemctl.log"

# OS 판정 확인: rhel9_path 를 주면 OS 메이저 9 이상(redhat-release / os-release)인 호스트만 그 경로를 본다
new_world ip9
printf '# 판정 확인용\nrhel9_path=/etc/sysconfig/rhel9-ifcfg\n' > "$OUT/ip9.conf"
printf 'rh7-n01 %s\nrh8-n01 %s\nrh9-n01 %s\nrh9-n02 %s\n' "$(newip rh7-n01)" "$(newip rh8-n01)" "$(newip rh9-n01)" "$(newip rh9-n02)" > "$OUT/t9.txt"
"$IPENG" -config "$OUT/ip9.conf" -targets "$OUT/t9.txt" -gossh "$G" > "$OUT/ip9.out" 2>&1
chk "OS 판정 [rh7-n01 RHEL7] rhel9_path 를 쓰지 않고 network-scripts 로 변경" grep -Eq '^rh7-n01 +10\.20\.7\.11 ->' "$OUT/ip9.out"
chk "OS 판정 [rh8-n01 RHEL8] rhel9_path 를 쓰지 않고 network-scripts 로 변경" grep -Eq '^rh8-n01 +10\.20\.8\.11 ->' "$OUT/ip9.out"
chk "OS 판정 [rh9-n01 RHEL9, redhat-release] rhel9_path 를 봄 (경로 없음 FAIL)" grep -Eq '^rh9-n01 +FAIL: 설정 경로가 없습니다' "$OUT/ip9.out"
chk "OS 판정 [rh9-n02 RHEL9, os-release 만] rhel9_path 를 봄 (경로 없음 FAIL)" grep -Eq '^rh9-n02 +FAIL: 설정 경로가 없습니다' "$OUT/ip9.out"

echo "== 2. LDAP 변경 (ldap-config-engine) — nslcd/ntp ↔ sssd/chrony 판정, 서비스 재시작 없음 =="
new_world ldap both      # 모든 호스트에 nslcd·ntp·sssd·chrony 파일을 둬서 "어느 쪽을 건드렸나" 가 판정 근거가 된다
export FAKE_UNGUARD=1    # ROOT 보호를 끄고 shim systemctl 로 재시작을 기록 → -no-restart 만이 재시작을 막는 상황
: > "$OUT/hosts.txt"
for h in $CHG_HOSTS; do echo "$h" >> "$OUT/hosts.txt"; done
LCOM=(-config "$CONF/ldap_config.conf" -assets "$CONF/assets.txt" -host-file "$OUT/hosts.txt" -gossh "$G")

"$LENG" "${LCOM[@]}" -infra alpha -dry-run -no-restart > "$OUT/l_dry.out" 2> "$OUT/l_dry.err"
chk "dry-run: 아무 파일도 바뀌지 않음" diff -rq "$NODES" "$ORIG"
chk "dry-run: rh7-n01 nslcd.conf 이 바뀔 예정 (WOULD-CHANGE)" nlog '^rh7-n01: APPLY\|WOULD-CHANGE\|/etc/nslcd.conf'
chk "dry-run: rh8-n01 sssd.conf 이 바뀔 예정 (WOULD-CHANGE)" nlog '^rh8-n01: APPLY\|WOULD-CHANGE\|/etc/sssd/sssd.conf'
chk "dry-run: 자산현황에 없는 rh8-noasset 은 건너뜀 경고" grep -q 'rh8-noasset' "$OUT/l_dry.err"

: > "$LOGD/node.log"
"$LENG" "${LCOM[@]}" -infra alpha > "$OUT/l_apply.out" 2> "$OUT/l_apply.err"   # 엔진 기본 호출 (-no-restart 없음): apply 는 어차피 재시작하지 않는다
OSOF() { sed -n 's/^VERSION_ID="\([0-9]*\).*/\1/p' "$ORIG/$1/etc/os-release"; }
for h in rh7-n01; do
	chk "LDAP [$h RHEL$(OSOF $h)] nslcd.conf 변경 + 백업" bash -c "grep -q '^uri ldap://10.10.1.20/' '$NODES/$h/etc/nslcd.conf' && ls '$NODES/$h/etc/nslcd.conf'.bak.* > /dev/null"
	chk "LDAP [$h RHEL$(OSOF $h)] ntp.conf 변경 (server 10.20.1.10 iburst)" grep -q '^server 10.20.1.10 iburst' "$NODES/$h/etc/ntp.conf"
	chk "LDAP [$h] sssd.conf 는 그대로 (백업도 없음)" bash -c "cmp -s '$NODES/$h/etc/sssd/sssd.conf' '$ORIG/$h/etc/sssd/sssd.conf' && ! ls '$NODES/$h/etc/sssd/sssd.conf'.bak.* > /dev/null 2>&1"
	chk "LDAP [$h] chrony.conf 는 그대로 (백업도 없음)" bash -c "cmp -s '$NODES/$h/etc/chrony.conf' '$ORIG/$h/etc/chrony.conf' && ! ls '$NODES/$h/etc/chrony.conf'.bak.* > /dev/null 2>&1"
	chk "LDAP [$h] 노드 로그 auth=nslcd time=ntp" nlog "^$h: APPLY\|INFO\|host=$h os=7 s4=0 auth=nslcd time=ntp"
done
for h in rh8-n01 rh9-n01 rh9-n02; do
	v=$(OSOF "$h")
	chk "LDAP [$h RHEL$v] sssd.conf 변경 + 백업" bash -c "grep -q '^ldap_uri = ldap://10.10.1.2[01]/' '$NODES/$h/etc/sssd/sssd.conf' && ls '$NODES/$h/etc/sssd/sssd.conf'.bak.* > /dev/null"
	chk "LDAP [$h RHEL$v] chrony.conf 변경 (server 10.20.1.10 iburst, pool 줄 제거)" bash -c "grep -q '^server 10.20.1.10 iburst' '$NODES/$h/etc/chrony.conf' && ! grep -q '^pool' '$NODES/$h/etc/chrony.conf'"
	chk "LDAP [$h] nslcd.conf 는 그대로 (백업도 없음)" bash -c "cmp -s '$NODES/$h/etc/nslcd.conf' '$ORIG/$h/etc/nslcd.conf' && ! ls '$NODES/$h/etc/nslcd.conf'.bak.* > /dev/null 2>&1"
	chk "LDAP [$h] ntp.conf 는 그대로 (백업도 없음)" bash -c "cmp -s '$NODES/$h/etc/ntp.conf' '$ORIG/$h/etc/ntp.conf' && ! ls '$NODES/$h/etc/ntp.conf'.bak.* > /dev/null 2>&1"
	chk "LDAP [$h] 노드 로그 auth=sssd time=chrony" nlog "^$h: APPLY\|INFO\|host=$h os=$v s4=0 auth=sssd time=chrony"
done
h=s4rh8-n01
chk "LDAP [$h RHEL8, s4 접두] nslcd.conf 변경 (8 이어도 nslcd)" grep -q '^uri ldap://10.10.1.20/' "$NODES/$h/etc/nslcd.conf"
chk "LDAP [$h RHEL8, s4 접두] ntp.conf 변경 (8 이어도 ntp)" grep -q '^server 10.20.1.10 iburst' "$NODES/$h/etc/ntp.conf"
chk "LDAP [$h] sssd.conf·chrony.conf 는 그대로" bash -c "cmp -s '$NODES/$h/etc/sssd/sssd.conf' '$ORIG/$h/etc/sssd/sssd.conf' && cmp -s '$NODES/$h/etc/chrony.conf' '$ORIG/$h/etc/chrony.conf'"
chk "LDAP [$h] 노드 로그 s4=1 auth=nslcd time=ntp" nlog "^$h: APPLY\|INFO\|host=$h os=8 s4=1 auth=nslcd time=ntp"
chk "LDAP [rh8-noasset] 자산현황에 없어 건너뜀 (아무것도 바뀌지 않음)" diff -rq "$NODES/rh8-noasset" "$ORIG/rh8-noasset"
chk "LDAP [rh7-n02] os8 접속 불가 → UNREACHABLE" grep -Eq '^ +rh7-n02 +UNREACHABLE' "$OUT/l_apply.out"
chk "LDAP [rh8-dead] UNREACHABLE" grep -Eq '^ +rh8-dead +UNREACHABLE' "$OUT/l_apply.out"
chk "apply: 서비스 재시작 0건 (노드 로그에 RESTARTED 없음)" bash -c "! grep -q 'APPLY|RESTARTED' '$LOGD/node.log'"
chk "apply: systemctl·sss_cache 호출 기록 0건" test ! -s "$LOGD/systemctl.log"
chk "apply: 노드가 '서비스 재시작 안 함' 을 알림" nlog '^rh8-n01: APPLY\|INFO\|서비스 재시작 안 함'
chk "apply: auto.appl 은 /etc/auto.appl.bak 에 대기 (rh9-n01)" bash -c "test -f '$NODES/rh9-n01/etc/auto.appl.bak' && test ! -f '$NODES/rh9-n01/etc/auto.appl'"

ldapfiles="etc/openldap/ldap.conf etc/autofs_ldap_auth.conf etc/autofs.conf etc/resolv.conf etc/auto.appl etc/nslcd.conf etc/ntp.conf etc/sssd/sssd.conf etc/chrony.conf"
LROLL=(-config "$CONF/ldap_config.conf" -assets "$CONF/assets.txt" -host-file "$OUT/hosts.txt" -gossh "$G" -rollback)
: > "$LOGD/node.log"
"$LENG" "${LROLL[@]}" -no-restart > "$OUT/l_rb.out" 2> "$OUT/l_rb.err"
bad=""
for h in rh7-n01 rh8-n01 rh9-n01 rh9-n02 s4rh8-n01; do
	for f in $ldapfiles; do
		[[ -f $ORIG/$h/$f ]] && { cmp -s "$NODES/$h/$f" "$ORIG/$h/$f" || bad="$bad $h:$f"; }
	done
done
chk "원복(-no-restart): 모든 LDAP 파일이 원본과 같음${bad:+ (다른 것:$bad)}" test -z "$bad"
chk "원복(-no-restart): RESTARTED 0건" bash -c "! grep -q 'APPLY|RESTARTED' '$LOGD/node.log'"
chk "원복(-no-restart): systemctl restart·sss_cache 호출 0건" test ! -s "$LOGD/systemctl.log"
chk "원복(-no-restart): [rh7-n01] nslcd·ntpd·autofs 는 SKIP-RESTART (NO_RESTART)" bash -c \
	"grep -q '^rh7-n01: APPLY|SKIP-RESTART|nslcd (NO_RESTART)' '$LOGD/node.log' && grep -q '^rh7-n01: APPLY|SKIP-RESTART|ntpd (NO_RESTART)' '$LOGD/node.log' && grep -q '^rh7-n01: APPLY|SKIP-RESTART|autofs (NO_RESTART)' '$LOGD/node.log'"
chk "원복(-no-restart): [rh8-n01] sssd·chronyd 는 SKIP-RESTART (NO_RESTART)" bash -c \
	"grep -q '^rh8-n01: APPLY|SKIP-RESTART|sssd (NO_RESTART)' '$LOGD/node.log' && grep -q '^rh8-n01: APPLY|SKIP-RESTART|chronyd (NO_RESTART)' '$LOGD/node.log'"
chk "원복(-no-restart): [s4rh8-n01] nslcd·ntpd 는 SKIP-RESTART (NO_RESTART)" bash -c \
	"grep -q '^s4rh8-n01: APPLY|SKIP-RESTART|nslcd (NO_RESTART)' '$LOGD/node.log' && grep -q '^s4rh8-n01: APPLY|SKIP-RESTART|ntpd (NO_RESTART)' '$LOGD/node.log'"

# 대조군: 다시 적용한 뒤 -no-restart 없이 원복 → 재시작이 나와야 한다 (그래야 위 0건이 의미가 있다)
"$LENG" "${LCOM[@]}" -infra alpha > "$OUT/l_apply2.out" 2> "$OUT/l_apply2.err"
chk "대조군 준비: 재적용으로 rh8-n01 sssd.conf 가 다시 바뀜" grep -q '^ldap_uri = ldap://10.10.1.2[01]/' "$NODES/rh8-n01/etc/sssd/sssd.conf"
: > "$LOGD/node.log"
: > "$LOGD/systemctl.log"
"$LENG" "${LROLL[@]}" > "$OUT/l_rb_ctl.out" 2> "$OUT/l_rb_ctl.err"
chk "대조군(-no-restart 없음): 노드 로그에 RESTARTED 가 나옴" grep -q 'APPLY|RESTARTED' "$LOGD/node.log"
chk "대조군: [rh7-n01] systemctl restart nslcd·ntpd" bash -c "grep -q '^rh7-n01 systemctl restart nslcd' '$LOGD/systemctl.log' && grep -q '^rh7-n01 systemctl restart ntpd' '$LOGD/systemctl.log'"
chk "대조군: [rh8-n01] systemctl restart sssd·chronyd + sss_cache -E" bash -c \
	"grep -q '^rh8-n01 systemctl restart sssd' '$LOGD/systemctl.log' && grep -q '^rh8-n01 systemctl restart chronyd' '$LOGD/systemctl.log' && grep -q '^rh8-n01 sss_cache -E' '$LOGD/systemctl.log'"
chk "대조군: [s4rh8-n01] systemctl restart nslcd·ntpd (sssd 아님)" bash -c \
	"grep -q '^s4rh8-n01 systemctl restart nslcd' '$LOGD/systemctl.log' && grep -q '^s4rh8-n01 systemctl restart ntpd' '$LOGD/systemctl.log' && ! grep -q '^s4rh8-n01 systemctl restart sssd' '$LOGD/systemctl.log'"
unset FAKE_UNGUARD

echo "== 3. bindpw 미노출 교차 점검 (Go: TestChangeS6World, 같은 fixture·가짜 gossh·실제 엔진) =="
unset FAKE_ROOTS FAKE_LOGDIR FAKE_UNGUARD
GW=$W/gotest
rm -rf "$GW"
chg_make_nodes "$GW/nodes"      # 현실적인 구성(OS 별로 필요한 파일만)
chg_write_conf "$GW/conf" "$BPW_A" "$BPW_B"
(cd "$ROOT" && AS_CHANGE_ENGINES="$BIN" AS_S6_WORLD="$GW" AS_S6_GOSSH="$G" go test -count=1 -run 'TestChangeS6World$' -v . > "$W/gotest.out" 2>&1)
rc=$?
grep -E ' S6 (PASS|FAIL)' "$W/gotest.out" | sed 's/^[[:space:]]*[A-Za-z0-9_.]*:[0-9]*: /  /' | sed 's/ S6 PASS/ [PASS]/; s/ S6 FAIL/ [FAIL]/'
gp=$(grep -cE ' S6 PASS' "$W/gotest.out")
gf=$(grep -cE ' S6 FAIL' "$W/gotest.out")
PASS=$((PASS + gp))
FAIL=$((FAIL + gf))
if [[ $rc -ne 0 && $gf -eq 0 ]]; then
	ng "Go 테스트 비정상 종료 (rc=$rc)" "$(tail -n 15 "$W/gotest.out" | tr '\n' ' ')"
fi
if [[ $gp -eq 0 ]]; then ng "Go 테스트가 S6 점검 줄을 내지 않음 (스킵됐거나 빌드 실패)" "$(tail -n 8 "$W/gotest.out" | tr '\n' ' ')"; fi
chkn "이 스크립트 자신의 출력·작업 파일(Go 테스트 제외)에 테스트 bindpw 값이 없음" \
	grep -rqF -e "$BPW_A" -e "$BPW_B" "$W/ip" "$W/ip9" "$W/ldap/fake" "$W/ldap/l_dry.out" "$W/ldap/l_dry.err" "$W/ldap/l_apply.out" "$W/ldap/l_apply.err" \
	"$W/ldap/l_rb.out" "$W/ldap/l_rb.err" "$W/ldap/l_rb_ctl.out" "$W/ldap/l_rb_ctl.err"

echo
echo "================ 변경 메뉴 OS 판정 검증 결과 ================"
echo "PASS=$PASS FAIL=$FAIL"
[[ $FAIL -eq 0 ]]
