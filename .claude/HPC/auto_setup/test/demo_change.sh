#!/bin/bash
# demo_change.sh - 변경 메뉴(m: IP 변경 · LDAP 변경 · IP+LDAP 변경 · 원복 · auto.appl 원복)를 "모의"로 눌러 보는 데모 환경 (v0.7.0).
#   이번 소스로 빌드한 auto_setup 과 실제 엔진(ip-change-engine, ldap-config-engine)을 쓰고, gossh 만 가짜(test/fake_gossh.sh)다.
#   가짜 gossh 는 엔진이 보낸 "진짜 노드 스크립트"를 호스트별 fixture 루트 아래에서 로컬로 실행한다 — 실제 서버·실제 /etc·
#   운영 /tmp/auto_setup · /usr/local/bin/auto_setup · 다른 데모(/tmp/as_demo_flow 등)는 건드리지 않는다. root 필요(raw ICMP, 데몬).
#
# 사용:
#   bash test/demo_change.sh          # 빌드·fixture·conf·가짜 job·데몬 준비 후 사용법 출력      (DEMO_DIR=/tmp/xxx 로 위치 변경, 기본 /tmp/as_demo_change)
#   source /tmp/as_demo_change/env.sh # 현재 셸에 환경변수·auto_setup 함수 설정
#   auto_setup --admin                # 화면(TUI) — m 키로 변경 메뉴 (--admin 이 아니어도 데모 conf 가 두 메뉴를 켬)
#   bash test/demo_change.sh --stop   # 데몬 종료 + 정리
#   bash test/demo_change.sh --help   # 이 설명
#
# 가짜 노드 (OS 7/8/9 혼합):
#   rh7-n01 RHEL7 정상 / rh7-n02 RHEL7 os8 접속 불가→os6_mgmt 경유 성공 / rh8-n01 Rocky8 정상(진행 중 job 의 호스트 → 경고)
#   rh8-n02 Rocky8 ifcfg 없음→IP 변경 FAIL / rh9-n01 RHEL9 정상 / rh9-n02 RHEL9 os-release 만 / s4rh8-n01 Rocky8 s4 접두→nslcd·ntp
#   rh8-dead 완전 불통 / rh8-noasset 정상이지만 assets.txt 에 없음(LDAP 은 자산현황 없음으로 건너뜀)
# 화면에서 해 볼 것: m → 1(IP 변경) → user 번호(1 alice, 2 bob) → 입력창에 "hostname 새IP" 줄들 → Ctrl+D → 미리보기 → yes + Enter
#   → 결과.  m → 2(LDAP) → infra 번호(1 alpha) → hostname 들 → Ctrl+D(dry-run) → yes.  m → 3 은 IP→LDAP, m → 4 는 원복, m → 5 는 auto.appl 원복.
#   원복·auto.appl 원복은 user 의 이전 기록으로 대상이 자동 채워진다. 실행 중 Ctrl+X → y 로 취소.
# 결과 확인: $DEMO_DIR/nodes/<호스트>/etc/... (바뀐 파일·.bak.<STAMP> 백업), $DEMO_DIR/fake/{gossh.log,node.log,systemctl.log}
#   (systemctl.log 는 서비스 재시작 호출 기록 — 변경 메뉴만 쓰면 비어 있어야 한다), $DEMO_DIR/data/changes/<user>/*.json (기록)

ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd -P)
S=${DEMO_DIR:-/tmp/as_demo_change}

case ${1:-} in
	-h | --help)
		sed -n '2,/^$/{/^#/s/^# \{0,1\}//p}' "${BASH_SOURCE[0]}"
		exit 0
		;;
	--stop)
		case $S in / | "" | /tmp | /tmp/) echo "[X] DEMO_DIR 이 위험한 경로입니다: [$S]"; exit 1 ;; esac
		pkill -KILL -f "^$S/auto_setup daemon$" 2> /dev/null
		pkill -KILL -f "$S/" 2> /dev/null
		rm -rf "$S"
		echo "[O] 데모 환경 정리: $S"
		exit 0
		;;
esac
if [[ $(id -u) -ne 0 ]]; then echo "[X] root 로 실행하세요 (raw ICMP)"; exit 1; fi
case $S in / | "" | /tmp | /tmp/) echo "[X] DEMO_DIR 이 위험한 경로입니다: [$S]"; exit 1 ;; esac
[[ -d /usr/local/go/bin ]] && export PATH="$PATH:/usr/local/go/bin"
command -v go > /dev/null 2>&1 || { echo "[X] go 를 찾을 수 없습니다"; exit 1; }
export GOPROXY=off CGO_ENABLED=0

# shellcheck source=lib_e2e.sh
. "$ROOT/test/lib_e2e.sh"
# shellcheck source=lib_change.sh
. "$ROOT/test/lib_change.sh"

pkill -KILL -f "^$S/auto_setup daemon$" 2> /dev/null
rm -rf "$S"
mkdir -p "$S"
e2e_paths
export E2E_SCEN="$S/scen"
mkdir -p "$E2E_SCEN"
e2e_make_stubs || { echo "[X] 스텁 준비 실패"; exit 1; }
e2e_build || { echo "[X] auto_setup 빌드 실패"; exit 1; }

# ---- 가짜 gossh: e2e 스텁은 gossh_e2e 로 옮기고(엔진 스크립트가 아닌 명령용 대체 경로), 그 자리에 test/fake_gossh.sh ----
for d in "$BIN" "$OS6"; do
	mv "$d/gossh" "$d/gossh_e2e" || exit 1
	cp "$ROOT/test/fake_gossh.sh" "$d/gossh" && chmod +x "$d/gossh" || exit 1
done

# ---- 엔진 두 개를 이번 소스로 빌드 (데모 bin 에만 — /usr/local/bin 은 건드리지 않음) ----
build_engine() { # <소스 디렉터리> <패키지> <출력>
	[[ -d $1 ]] || { echo "[X] 엔진 소스를 찾을 수 없습니다: $1"; return 1; }
	(cd "$1" && GOFLAGS=-buildvcs=false go build -o "$3" "$2") || { echo "[X] 엔진 빌드 실패: $2"; return 1; }
}
build_engine "$ROOT/../ip_change" ./cmd/ip-change-engine "$BIN/ip-change-engine" || exit 1
build_engine "$ROOT/../ldap_setting" ./cmd/ldap-config-engine "$BIN/ldap-config-engine" || exit 1

# ---- 가짜 노드 · 변경용 conf ----
chg_make_nodes "$S/nodes"
chg_write_conf "$S/conf" "DEMO_BINDPW_alpha_Qx7mK2" "DEMO_BINDPW_beta_Zk3pR9"
mkdir -p "$S/fake"

# ---- user 메뉴 (demo_flow 의 userroute 방식: alice / bob) ----
UR="$S/userroute"
AWXD="$S/awx"
mkdir -p "$UR" "$AWXD"
cat > "$UR/info_mn.sh" << 'STUB'
#!/bin/bash
echo "1) alice"
echo "2) bob"
STUB
cat > "$UR/info.sh" << 'STUB'
#!/bin/bash
case $1 in 1) echo alice ;; 2) echo bob ;; esac
STUB
chmod +x "$UR/info_mn.sh" "$UR/info.sh"
# 01 스크립트는 user_route 만 읽히면 되므로 최소 스텁 (awx_dir/01.AWX_nodeinfo_V2.sh)
printf '#!/bin/bash\n# 데모용 최소 스텁: auto_setup 이 user_route 만 읽는다\nuser_route="%s"\n' "$UR" > "$AWXD/01.AWX_nodeinfo_V2.sh"

cat > "$S/conf/auto_setup.conf" << CONF
# 데모 conf (AUTO_SETUP_CONF=$S/conf) — 변경 conf(ip_change.conf, ldap_config.conf, assets.txt)도 이 디렉터리에 있다
awx_dir=$AWXD
enable_ip_change=y
enable_ldap_change=y
ip_engine=$BIN/ip-change-engine
ldap_engine=$BIN/ldap-config-engine
CONF

# ---- 가짜 job (최소): 변경 대상 rh8-n01 이 진행 중 job 의 호스트 → 미리보기에 빨간 경고 ----
D="$S/data"
mkdir -p "$D/jobs" "$D/jobs/done" "$D/queue" "$D/codes" "$D/runs" "$D/bin" "$D/done" "$D/requests"
N=$(date +%s)
ts() { date -d "@$1" +%Y%m%d%H%M%S; }
J1=$(date -d "@$((N - 600))" +%Y%m%d-%H%M%S)-alice
YA="infra_inventory-$(ts $((N - 580)))_2ea.yml"
cat > "$D/jobs/$J1.json" << JSON
{"id":"$J1","user":"alice","submitted":$((N - 600)),"first_ready":$((N + 100000)),"late_first_ready":0,"first_run_done":false,
"hosts":{
"rh8-n01":$(_hj 127.0.0.61 local true 0 "" 2 0 installing $((N - 900)) $((N - 1800)) ''),
"demo-peer01":$(_hj 127.0.0.62 local true 0 "" 2 0 installing $((N - 900)) $((N - 1800)) '')},
"runs":[],
"groups":{"$YA":{"infra":"I1","os":"RHEL8","boot":"UEFI","splunk":"-","hosts":["rh8-n01","demo-peer01"]}},"all_yml":"infra_inventory-$(ts $((N - 600)))_all.yml"}
JSON
# 두 호스트는 데몬의 점검(가짜 gossh_e2e)에 어떤 경로로도 응답하지 않음 → job 이 계속 "설치중(진행 중)" 으로 남는다
# (엔진 호출은 fake_gossh.sh 의 fixture 로 처리되므로 이 목록과 무관하다)
printf 'rh8-n01\n127.0.0.61\ndemo-peer01\n127.0.0.62\n' > "$E2E_SCEN/silent_hosts"

# ---- 환경 파일 ----
cat > "$S/env.sh" << ENV
# source $S/env.sh : 변경 메뉴 데모 환경 (이 셸에서만 유효)
export AUTO_SETUP_DIR="$D"
export AUTO_SETUP_CONF="$S/conf"
export E2E_SCEN="$E2E_SCEN" E2E_MGMT="$E2E_MGMT"
export FAKE_ROOTS="$S/nodes" FAKE_LOGDIR="$S/fake"
export PATH="$BIN:\$PATH"
auto_setup() { "$S/auto_setup" "\$@"; }
echo "데모 환경 설정됨: AUTO_SETUP_DIR=$D  (auto_setup --admin → $S/auto_setup)"
ENV

# ---- 데몬 기동 (데모 디렉터리에서만) ----
env AUTO_SETUP_DIR="$D" AUTO_SETUP_CONF="$S/conf" E2E_SCEN="$E2E_SCEN" FAKE_ROOTS="$S/nodes" FAKE_LOGDIR="$S/fake" \
	PATH="$BIN:$PATH" "$S/auto_setup" ensure > /dev/null 2>&1
sleep 1
cat << MSG

[O] 변경 메뉴 데모 환경 준비 완료: $S
    진행 중 job ($J1): $YA — 호스트 rh8-n01, demo-peer01 (rh8-n01 을 변경 대상에 넣으면 미리보기에 진행 중 job 경고)

  시작:
    source $S/env.sh
    auto_setup --admin        # 화면. m 키 → 메뉴 (IP/LDAP/둘 다/원복/auto.appl 원복)

  가짜 노드 9대 (OS 7/8/9 혼합):
    rh7-n01 RHEL7 정상            rh7-n02 RHEL7 os8 접속 불가 → os6_mgmt 경유로 성공
    rh8-n01 Rocky8 정상 (job 호스트)  rh8-n02 Rocky8 ifcfg 없음 → IP 변경 FAIL (IP+LDAP 이면 LDAP 건너뜀)
    rh9-n01 RHEL9 정상            rh9-n02 RHEL9 os-release 만 있음
    s4rh8-n01 Rocky8 s4 접두 → nslcd/ntp   rh8-dead 완전 불통
    rh8-noasset 정상, assets.txt 에 없음 → LDAP 은 "자산현황 없음" 으로 건너뜀

  입력 예 (m → 3 IP+LDAP 변경 → user 1 → infra 1 → 아래 붙여넣기 → Ctrl+D → 미리보기 확인 → yes):
    rh7-n01 10.30.7.11
    rh7-n02 10.30.7.12
    rh8-n01 10.30.8.11
    rh8-n02 10.30.8.12
    rh9-n01 10.30.9.11
    rh9-n02 10.30.9.12
    s4rh8-n01 10.30.8.21
    rh8-dead 10.30.8.99
    rh8-noasset 10.30.8.31
  이어서 m → 5(auto.appl 원복) / m → 4(원복 → 3 IP+LDAP) 를 눌러 보세요 (대상은 기록으로 자동 채워짐).

  확인:
    cat $S/nodes/rh8-n01/etc/sysconfig/network-scripts/ifcfg-eth0
    ls $S/nodes/rh7-n01/etc/ | grep bak          # .bak.<STAMP> 백업
    cat $S/fake/systemctl.log                   # 비어 있어야 함 (서비스 재시작 없음)
    ls $D/changes/alice/                         # 실행 기록 (bindpw 없음)
  정리:
    bash $ROOT/test/demo_change.sh --stop
MSG
