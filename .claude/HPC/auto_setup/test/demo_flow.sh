#!/bin/bash
# demo_flow.sh - 화면(TUI)의 수동 실행(c 설정체크+설정수정 / t 설정체크만)·결과 화면·g 재확인 진행을 "모의"로 눌러 보는 데모 환경.
#   실제 서버·운영 /tmp/auto_setup 은 건드리지 않는다: 스크래치 디렉터리 + 가짜 gossh/ssh(PATH 스텁) + 복사본 os_check(-auto / -auto-check) 로 동작.
#   호스트 이름은 127.0.0.x (ping 항상 응답). root 필요(raw ICMP).
#
# 사용:
#   bash test/demo_flow.sh            # 환경 준비(빌드·가짜 작업·스텁) + 데몬 기동 후 사용법 출력
#   source /tmp/as_demo_flow/env.sh   # 현재 셸에 환경변수·auto_setup 별칭 설정
#   auto_setup                        # 화면 (TUI)
#   bash test/demo_flow.sh --stop     # 데몬 종료 + 정리
#
# 화면에서 해 볼 것 (그룹 호스트표에서):
#   t  설정체크만     → y → 결과 화면에 FAIL/접속불가 호스트가 표시됨 (호스트 상태는 그대로, 완료 처리 안 됨)
#   c  설정체크+수정  → y → 결과 화면 (정상 호스트는 완료 처리됨)
#   g  재확인         → 진행 화면: 1/3 os8 확인 → 2/3 os6_mgmt 경유 → 3/3 최종 결과, 호스트별 어디서 안 됐는지
#   v  가장 최근 수동 실행 결과 다시 보기

ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd -P)
S=${DEMO_DIR:-/tmp/as_demo_flow}

if [[ $1 == --stop ]]; then
	pkill -KILL -f "^$S/auto_setup daemon$" 2> /dev/null
	pkill -KILL -f "$S/" 2> /dev/null
	rm -rf "$S"
	echo "[O] 데모 환경 정리: $S"
	exit 0
fi
if [[ $(id -u) -ne 0 ]]; then echo "[X] root 로 실행하세요 (raw ICMP)"; exit 1; fi
[[ -d /usr/local/go/bin ]] && export PATH="$PATH:/usr/local/go/bin"
command -v go > /dev/null 2>&1 || { echo "[X] go 를 찾을 수 없습니다"; exit 1; }

# shellcheck source=lib_e2e.sh
. "$ROOT/test/lib_e2e.sh"
pkill -KILL -f "^$S/auto_setup daemon$" 2> /dev/null
rm -rf "$S"
mkdir -p "$S"
e2e_paths
export E2E_SCEN="$S/scen"
mkdir -p "$E2E_SCEN"
e2e_make_stubs || { echo "[X] 스텁 준비 실패"; exit 1; }
e2e_build || { echo "[X] 빌드 실패"; exit 1; }

# ---- 시나리오: 호스트별 동작 ----
# 127.0.0.12 : 설정체크에서 FAIL (selinux)            127.0.0.14 : 접속불가(ping 불가 — 체크 결과 없음)
# 127.0.0.21 : os8 에서는 무응답, os6_mgmt 경유로는 응답  127.0.0.22 : 어디서도 무응답
printf '127.0.0.12\n' > "$E2E_SCEN/fail_hosts"
printf '127.0.0.14\n' > "$E2E_SCEN/pingx_hosts"
printf '127.0.0.21\n' > "$E2E_SCEN/os8silent_hosts"
printf '127.0.0.22\n' > "$E2E_SCEN/silent_hosts"

# ---- 가짜 작업 (오늘 / 어제) ----
D="$S/data"
mkdir -p "$D/jobs" "$D/jobs/done" "$D/queue" "$D/codes" "$D/runs" "$D/bin" "$D/done" "$D/requests"
N=$(date +%s)
Y=$((N - 86400))
ts() { date -d "@$1" +%Y%m%d%H%M%S; }
J1=$(date -d "@$((N - 3600))" +%Y%m%d-%H%M%S)-alice
J2=$(date -d "@$((Y - 3600))" +%Y%m%d-%H%M%S)-bob
YA="infra_inventory-$(ts $((N - 3500)))_4ea.yml"   # 오늘 A: 수동 실행 대상 (READY·FAIL·접속불가·완료)
YB="infra_inventory-$(ts $((N - 3400)))_3ea.yml"   # 오늘 B: g 재확인 대상 (os6 경유로만 응답·접속불가·설치중)
YC="infra_inventory-$(ts $((Y - 3500)))_2ea.yml"   # 어제
cat > "$D/jobs/$J1.json" <<JSON
{"id":"$J1","user":"alice","submitted":$((N - 3600)),"first_ready":$((N + 100000)),"late_first_ready":0,"first_run_done":false,
"hosts":{
"127.0.0.11":$(_hj 127.0.0.11 local true $((N - 600)) "" 0 0 ready $((N - 600)) $((N - 2400)) ''),
"127.0.0.12":$(_hj 127.0.0.12 local true $((N - 600)) "" 0 0 ready $((N - 600)) $((N - 2400)) ''),
"127.0.0.13":$(_hj 127.0.0.13 local true $((N - 1500)) 4821 0 0 done $((N - 1000)) $((N - 3000)) ',"done_src":"run"'),
"127.0.0.14":$(_hj 127.0.0.14 local true $((N - 600)) "" 0 0 ready $((N - 600)) $((N - 2400)) ''),
"127.0.0.21":$(_hj 127.0.0.21 os6 true 0 "" 2 0 installing $((N - 900)) $((N - 1800)) ''),
"127.0.0.22":$(_hj 127.0.0.22 local true 0 "" 2 0 installing $((N - 900)) $((N - 1800)) ''),
"127.0.0.23":$(_hj 127.0.0.23 local true 0 "" 2 0 deploying $((N - 120)) $((N - 120)) '')},
"runs":[{"code":"4821","hosts":["127.0.0.13"],"at":$((N - 1000))}],
"groups":{"$YA":{"infra":"I1","os":"RHEL8","boot":"UEFI","splunk":"typeA","hosts":["127.0.0.11","127.0.0.12","127.0.0.13","127.0.0.14"]},
"$YB":{"infra":"I2","os":"RHEL8","boot":"UEFI","splunk":"-","hosts":["127.0.0.21","127.0.0.22","127.0.0.23"]}},"all_yml":"infra_inventory-$(ts $((N - 3600)))_all.yml"}
JSON
cat > "$D/jobs/$J2.json" <<JSON
{"id":"$J2","user":"bob","submitted":$((Y - 3600)),"first_ready":$((Y - 600)),"late_first_ready":0,"first_run_done":true,
"hosts":{
"127.0.0.31":$(_hj 127.0.0.31 local true $((Y - 1500)) 4800 0 0 done $((Y - 1000)) $((Y - 3000)) ',"done_src":"run"'),
"127.0.0.32":$(_hj 127.0.0.32 local true $((Y - 1500)) "" 0 3 failed $((Y - 900)) $((Y - 3000)) '')},
"runs":[{"code":"4800","hosts":["127.0.0.31"],"at":$((Y - 1000))}],
"groups":{"$YC":{"infra":"I3","os":"RHEL7","boot":"BIOS","splunk":"-","hosts":["127.0.0.31","127.0.0.32"]}},"all_yml":"infra_inventory-$(ts $((Y - 3600)))_all.yml"}
JSON
printf '작업 : 설정체크 + 설정수정\n\n(샘플 이전 결과)\n원본 : /tmp/none\n' > "$D/codes/4821.txt"

# ---- 환경 파일 ----
cat > "$S/env.sh" <<ENV
# source $S/env.sh : 데모 환경 (이 셸에서만 유효)
export AUTO_SETUP_DIR="$D"
export E2E_SCEN="$E2E_SCEN"
export PATH="$BIN:\$PATH"
alias auto_setup='$S/auto_setup'
echo "데모 환경 설정됨: AUTO_SETUP_DIR=$D  (auto_setup → $S/auto_setup)"
ENV

# ---- 데몬 기동 ----
env AUTO_SETUP_DIR="$D" E2E_SCEN="$E2E_SCEN" PATH="$BIN:$PATH" "$S/auto_setup" ensure > /dev/null 2>&1
sleep 1
cat <<MSG

[O] 데모 환경 준비 완료: $S
    오늘 job ($J1): 그룹 A $YA / 그룹 B $YB
    어제 job ($J2): 그룹 $YC

  시작:
    source $S/env.sh
    auto_setup            # 화면. 맨 위 그룹(오늘 A)에서 Enter → t / c / g / v 를 눌러 보세요

  시나리오 (오늘 A, 호스트 4대):
    127.0.0.11 정상(READY) / 127.0.0.12 설정체크 FAIL / 127.0.0.13 이미 완료 / 127.0.0.14 접속불가(체크 결과 없음)
    t → 결과에 FAIL·접속불가가 나오지만 호스트는 그대로(완료 처리 안 됨),  c → 정상 호스트가 완료 처리됨
  시나리오 (오늘 B, g 재확인):
    127.0.0.21 os8 무응답·os6 경유 응답 / 127.0.0.22 어디서도 무응답 / 127.0.0.23 os8 에서 응답
    g → 진행 화면에 1/3 os8 → 2/3 os6_mgmt → 3/3 최종 결과, 호스트별로 어디서 안 됐는지 표시
  정리:
    bash $ROOT/test/demo_flow.sh --stop
MSG
