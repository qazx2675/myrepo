#!/bin/bash
# run_e2e.sh - auto_setup 목업 E2E (계획서 §9-3): 실제 auto_setup 바이너리 + 가짜 gossh/ssh/wall + 01/os_check 복사본
#
# 방식: 스크래치(mktemp -d) + AUTO_SETUP_DIR + PATH 스텁. 원본 01/os_check 는 읽기만 하고 복사본만 사용.
#       빈 변수 4개는 스크래치 복사본 빌드 시 -ldflags -X 로만 주입(소스는 빈 값 유지).
#       ICMP 는 로컬에서 결정적인 주소를 쓴다: 127.0.0.x(항상 up) / 192.0.2.1(RFC5737 TEST-NET, 항상 down → os6 경로).
#       호스트 이름 자리에 IP 를 그대로 쓰므로 DNS/hosts 수정이 필요 없다. (root 필요: raw ICMP)
# 실행: bash test/run_e2e.sh      (환경변수 KEEP_SCRATCH=1 이면 스크래치 보존)

ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd -P)
# shellcheck source=lib_e2e.sh
. "$ROOT/test/lib_e2e.sh"

[[ -d /usr/local/go/bin ]] && export PATH="$PATH:/usr/local/go/bin"
ORIG_PATH=$PATH

PASS_N=0; FAIL_N=0
FAILS=()
ok()  { PASS_N=$((PASS_N + 1)); printf '  [PASS] %s\n' "$1"; }
bad() { FAIL_N=$((FAIL_N + 1)); FAILS+=("$1"); printf '  [FAIL] %s\n' "$1"; [[ -n $2 ]] && printf '         %s\n' "$2"; }
t_eq() { # 설명 실제 기대
	if [[ $2 == "$3" ]]; then ok "$1"; else bad "$1" "기대=[$3] 실제=[$2]"; fi
}
t_has() { # 설명 파일 정규식
	if grep -Eq -- "$3" "$2" 2>/dev/null; then ok "$1"; else bad "$1" "패턴 없음 /$3/ in $2"; fi
}
t_hasF() { # 설명 파일 고정문자열
	if grep -Fq -- "$3" "$2" 2>/dev/null; then ok "$1"; else bad "$1" "문자열 없음 [$3] in $2"; fi
}
t_no() { # 설명 파일 정규식
	if grep -Eq -- "$3" "$2" 2>/dev/null; then bad "$1" "있으면 안 되는 패턴 /$3/"; else ok "$1"; fi
}
wait_for() { # 초 명령...
	local t=$1 i
	shift
	for ((i = 0; i < t * 2; i++)); do
		"$@" > /dev/null 2>&1 && return 0
		sleep 0.5
	done
	return 1
}

if [[ $(id -u) -ne 0 ]]; then
	echo "[SKIP] root 가 아니면 raw ICMP 를 열 수 없어 E2E 를 건너뜁니다 (root 로 실행하세요)"
	exit 0
fi
if ! command -v go > /dev/null 2>&1; then
	echo "[X] go 를 찾을 수 없습니다 (PATH 확인)"
	exit 1
fi

S=$(mktemp -d /tmp/as_e2e.XXXXXX)
cleanup() {
	pkill -KILL -f "$S/" 2> /dev/null
	if [[ -n $KEEP_SCRATCH ]]; then
		echo "KEEP_SCRATCH: $S"
	else
		rm -rf "$S"
	fi
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

e2e_paths
AS="$S/as"
export AUTO_SETUP_DIR="$AS"
mkdir -p "$AS"

daemon_pids() { pgrep -f "^$S/auto_setup daemon$"; }
daemon_count() { daemon_pids | wc -l; }
has_jobs() { ls "$AS"/jobs/*.json > /dev/null 2>&1; }
has_done() { ls "$AS"/jobs/done/*.json > /dev/null 2>&1; }
queue_empty() { [[ -z $(ls "$AS/queue" 2> /dev/null) ]]; }

echo "== 준비: 스텁 · 빌드 (스크래치 $S)"
e2e_make_stubs || { echo "[X] 스텁 준비 실패"; exit 1; }
if ! e2e_build; then echo "[X] 빌드 실패"; exit 1; fi
AUTOSETUP="$S/auto_setup"

echo "== 0) 소스 무결성 · 빌드"
n=$(grep -cE '^[[:space:]]+(os6_mgmt|os6_gossh|os6_autosetup|os_check_sh)[[:space:]]+= ""' "$ROOT/main.go")
t_eq "소스 main.go 의 빈 변수 4개는 빈 값 그대로" "$n" 4
rc=0; "$AUTOSETUP" > /dev/null 2>&1 < /dev/null || rc=$?
t_eq "인자 없이 실행 → 상태 리포트(비 tty 면 plain), exit 0" "$rc" 0
t_has "인자 없이 실행 → 사용법이 아니라 리포트 출력" <("$AUTOSETUP" 2>&1 < /dev/null) '^ auto_setup 상태 리포트'
rc=0; "$AUTOSETUP" -h > /dev/null 2>&1 || rc=$?
t_eq "-h → 사용법 출력 exit 0" "$rc" 0
t_has "-h → 사용법 출력" <("$AUTOSETUP" -h 2>&1) '^사용법: auto_setup'
rc=0; "$AUTOSETUP" bogus > /dev/null 2>&1 || rc=$?
t_eq "잘못된 인자 → 사용법 출력 후 exit 1" "$rc" 1
t_has "잘못된 인자 → 사용법 출력(stderr)" <("$AUTOSETUP" bogus 2>&1) '^사용법: auto_setup'

# ============================================================
echo "== c) os_check 복사본 -auto : 프롬프트 없이 끝까지, p/d → set / 아니면 y"
e2e_scen c
printf 's2h02\n' > "$E2E_SCEN/refused_hosts"

printf 'pd01\ns2h01\ns2h02\n' > "$S/c1.list"
e2e_run_oc "$S/c1" "$S/c1.list"
O="$S/c1/out.log"
t_eq "c1(p/d 포함) 종료코드" "$RCOC" 0
t_hasF "c1 OS 체크 자동 y" "$O" "OS 체크를 진행하시겠습니까? (y/n) : y (-auto)"
t_hasF "c1 환경설정 set" "$O" "OS 환경설정을 수정하시겠습니까? (y/n/set) : set (-auto)"
t_no "c1 프롬프트에서 막혀 종료하지 않음" "$O" '진행하지 않고 종료|건너뜁니다'
for f in setting_insert rclocal appl_change; do
	t_hasF "c1 gossh 호출: $f.sh" "$E2E_SCEN/gossh.log" "$f.sh]"
done
t_hasF "c1 gossh 호출: setting.sh (set 만)" "$E2E_SCEN/gossh.log" "setting.sh]"
t_hasF "c1 담당자 문구(접두사 호스트 불가)" "$O" "하기서버 1대는 OS 배포이후 올라오지않는 것 같습니다."
t_hasF "c1 요약 refused=1" "$O" "refused=1"
t_eq "c1 postapply 결과 파일에 접속 가능 2대만" "$(grep -c . "$S/c1/check.res_${E2E_USER}_postapply")" 2

: > "$E2E_SCEN/gossh.log"
printf 's2h01\nc01\ne01\n' > "$S/c2.list"
e2e_run_oc "$S/c2" "$S/c2.list"
O="$S/c2/out.log"
t_eq "c2(p/d 없음) 종료코드" "$RCOC" 0
t_hasF "c2 환경설정 y" "$O" "OS 환경설정을 수정하시겠습니까? (y/n/set) : y (-auto)"
for f in setting_insert rclocal appl_change; do
	t_hasF "c2 gossh 호출: $f.sh" "$E2E_SCEN/gossh.log" "$f.sh]"
done
t_no "c2 setting.sh 미호출 (3종만)" "$E2E_SCEN/gossh.log" 'setting\.sh\]'
t_hasF "c2 전원 접속 가능 → 설치 완료 문구" "$O" "3대 OS 설치 완료하였습니다."

printf 'P01\n' > "$S/c3.list"
e2e_run_oc "$S/c3" "$S/c3.list"
t_hasF "c3 대문자 P 도 p/d 로 판정 → set" "$S/c3/out.log" "(y/n/set) : set (-auto)"

(cd "$S" && env PATH="$BIN:$ORIG_PATH" bash "$OC/os_check_final_annotated.sh" -auto < /dev/null > "$S/c4.log" 2>&1)
rc=$?
t_eq "c4 -auto 인자 부족 → exit 1" "$rc" 1
t_hasF "c4 오류 메시지" "$S/c4.log" "[ERROR] -auto/-auto-check <user> <호스트목록파일>"

# ============================================================
echo "== a) 01 복사본 실행(스텁 02) → queue 파일 → 데몬 수거"
e2e_scen flow
printf '127.0.0.2\n' > "$E2E_SCEN/fail_hosts"
e2e_run_01 127.0.0.1 127.0.0.2 192.0.2.1
t_eq "01 종료코드 0" "$RC01" 0
t_has "01 출력: 02 호출 후 auto_setup 전달 로그" "$S/out01.txt" 'auto_setup 전달 : 3대'
t_has "01 출력: 기존 흐름 끝까지(대상 확인 생략/완료)" "$S/out01.txt" '완료'
QF=$(ls "$AS"/queue/*.job 2> /dev/null | head -1)
t_eq "queue 파일 1개" "$(ls "$AS"/queue/*.job 2> /dev/null | wc -l)" 1
t_has "queue 파일명 규칙 <epoch>_<user>_<pid>.job" <(basename "$QF") "^[0-9]+_${E2E_USER}_[0-9]+\.job$"
t_eq "queue 내용: user 줄" "$(sed -n 1p "$QF")" "user=$E2E_USER"
t_has "queue 내용: time 줄" <(sed -n 2p "$QF") '^time=[0-9]+$'
t_eq "queue 내용: 호스트명 3줄" "$(sed -n '3,$p' "$QF" | paste -sd' ')" "127.0.0.1 127.0.0.2 192.0.2.1"

export PATH="$BIN:$ORIG_PATH"   # 이후 기동하는 데몬이 가짜 gossh/ssh/wall 을 보게 함

echo "== f) ensure 중복 방지 → 수거"
"$AUTOSETUP" ensure; rc1=$?
"$AUTOSETUP" ensure; rc2=$?
sleep 1
t_eq "ensure 두 번 종료코드 0 0" "$rc1 $rc2" "0 0"
t_eq "ensure 두 번 → 데몬 1개" "$(daemon_count)" 1
if wait_for 20 has_jobs && wait_for 20 queue_empty; then ok "데몬이 queue 수거 → jobs/ 생성, queue 비움"; else bad "데몬이 queue 수거 → jobs/ 생성, queue 비움" "queue=$(ls "$AS/queue") jobs=$(ls "$AS/jobs")"; fi
JOBF=$(ls "$AS"/jobs/*.json 2> /dev/null | head -1)
JOBID=$(basename "$JOBF" .json)
if wait_for 20 grep -q '"route": "os6"' "$JOBF"; then ok "경로 판별 결과(route=os6) 가 jobs json 에 저장됨"; else bad "경로 판별 결과(route=os6) 가 jobs json 에 저장됨" "$(grep '"route"' "$JOBF" | paste -sd' ')"; fi
t_has "job id 형식 <날짜>-<시각>-<user>" <(echo "$JOBID") "^[0-9]{8}-[0-9]{6}-${E2E_USER}$"
t_eq "jobs json: 호스트 3개 · user" "$(grep -c '"seen_down"' "$JOBF")|$(grep -c "\"user\": \"$E2E_USER\"" "$JOBF")" "3|1"

echo "== f) kill -9 → ensure → 재기동 · 이어하기(상태 복원)"
OLD=$(daemon_pids | head -1)
kill -9 "$OLD"
sleep 1
t_eq "kill -9 후 데몬 0개" "$(daemon_count)" 0
"$AUTOSETUP" ensure
if wait_for 10 pgrep -f "^$S/auto_setup daemon$"; then
	NEW=$(daemon_pids | head -1)
	if [[ -n $NEW && $NEW != "$OLD" ]]; then ok "ensure 로 새 데몬 기동 (pid 변경)"; else bad "ensure 로 새 데몬 기동 (pid 변경)" "old=$OLD new=$NEW"; fi
else
	bad "ensure 로 새 데몬 기동 (pid 변경)" "데몬 없음"
fi
sleep 1
t_has "재기동 로그: job 복원" "$AS/auto_setup.log" '복원: job 1건'
t_eq "재기동 후에도 데몬 1개" "$(daemon_count)" 1

echo "== b) 실제 ICMP(127.0.0.x up / 192.0.2.1 down→os6 경로) → READY → run (최대 ~3분 대기)"
if wait_for 180 has_done; then ok "전원 READY → 즉시 1회 run → 처리완료 → job 이 jobs/done 으로 이동"; else bad "전원 READY → run → job 종료" "log: $(tail -5 "$AS/auto_setup.log" | paste -sd'|')"; fi
sleep 1
t_hasF "경로 판별 로그: local 2대, os6 1대" "$AS/auto_setup.log" "경로 판별: local 2대, os6 1대"
t_eq "READY 로그 3건" "$(grep -c '^[0-9: -]* READY: ' "$AS/auto_setup.log")" 3
t_eq "run 시작 1회 (1차, 3대)" "$(grep -c 'run 시작: job .* 1차 3대' "$AS/auto_setup.log")" 1
t_has "job 종료 로그(처리완료 3)" "$AS/auto_setup.log" 'job 종료: .* \(처리완료 3, 실패 0\)'
DONE=$AS/jobs/done/$JOBID.json
t_eq "done job id 가 kill 전과 동일(이어하기)" "$(basename "$(ls "$AS"/jobs/done/*.json 2> /dev/null | head -1)" .json)" "$JOBID"

# ============================================================
echo "== d) code 파일 · wall 알림 끔(로그로 확인)"
WALL=$E2E_SCEN/wall.log
t_eq "wall 알림 끔(wall.log 없음)" "$([[ -s $WALL ]] && echo 있음 || echo 없음)" 없음
CODE=$(ls "$AS/codes" 2> /dev/null | head -1 | sed 's/.txt$//')
t_has "code: 4자리" <(echo "$CODE") '^[0-9]{4}$'
t_has "로그: run 완료 code 와 처리 3대" "$AS/auto_setup.log" "run 완료: job .* code $CODE 처리 3대"
t_eq "codes/ 에 파일 1개, 이름=로그의 code" "$(ls "$AS/codes" 2> /dev/null)" "$CODE.txt"
CF=$AS/codes/$CODE.txt
# os6_mgmt 만 채워도 autofs 폴백(os6OSCheckPath)으로 2차 체크가 켜지므로, 1차 구성 검사는 2차 섹션을 뗀 사본으로 한다 (2차는 run_e2e2.sh)
sed -e '/^### 2차 체크/,$d' "$CF" | sed -e '/./,$!d' | tac | sed -e '/./,$!d' | tac > "$S/code_first.txt"
CF1=$S/code_first.txt
t_eq "done json: 3대 모두 processed=code" "$(grep -c "\"processed\": \"$CODE\"" "$DONE")" 3
t_eq "done json: run 1건(1차=전체 3대)" "$(grep -c '"code"' "$DONE")" 1

L_REPORT=$(grep -n '^############### 결과 리포트 ###############$' "$CF" | head -1 | cut -d: -f1)
L_SET=$(grep -n '^설정체크 (설정 수정 후 재점검)$' "$CF" | head -1 | cut -d: -f1)
L_LDAP=$(grep -n '^===== LDAP 정보' "$CF" | head -1 | cut -d: -f1)
L_SPL=$(grep -n '^===== SPLUNK 정보' "$CF" | head -1 | cut -d: -f1)
L_KER=$(grep -n '^===== 커널 버전' "$CF" | head -1 | cut -d: -f1)
L_INF=$(grep -n '^===== SDS infra 커널 버전' "$CF" | head -1 | cut -d: -f1)
L_SRC=$(grep -n '^원본 : ' "$CF" | head -1 | cut -d: -f1)
ORDER_OK=1
prev=0
for v in "$L_REPORT" "$L_SET" "$L_LDAP" "$L_SPL" "$L_KER" "$L_INF" "$L_SRC"; do
	if [[ -z $v || $v -le $prev ]]; then ORDER_OK=0; fi
	prev=${v:-0}
done
t_eq "code 파일 구성 순서: 결과리포트→설정체크→LDAP→SPLUNK→커널→infra커널→원본" "$ORDER_OK" 1
t_eq "code 파일 첫 줄 = 작업 모드(설정체크 + 설정수정)" "$(sed -n 1p "$CF")" "작업 : 설정체크 + 설정수정"
t_eq "code 파일 셋째 줄 = 결과 리포트 머리줄" "$(sed -n 3p "$CF")" "############### 결과 리포트 ###############"
t_eq "code 파일 마지막 줄 = 원본 경로" "$(tail -1 "$CF1")" "원본 : $AS/runs/$CODE/os_check.log"
t_hasF "담당자 문구(전원 설치 완료)" "$CF" "3대 OS 설치 완료하였습니다."
t_hasF "담당자 문구: 호스트 가로 나열" "$CF" "127.0.0.1 127.0.0.2 192.0.2.1"
EXP_SET="설정체크 (설정 수정 후 재점검)
127.0.0.2: FAIL selinux enforcing
NO FAIL : 127.0.0.1 192.0.2.1 (2대)"
t_eq "설정체크 블록: FAIL 줄 + NO FAIL 한 줄" "$(sed -n '/^설정체크 (설정 수정 후 재점검)$/,/^$/p' "$CF1" | sed '/^$/d')" "$EXP_SET"
t_hasF "LDAP 요약" "$CF" "INFO ldap infra1"
t_hasF "SPLUNK 요약(대수 포함)" "$CF" "INFO SDS Splunk typeA (3대)"
t_hasF "커널 요약" "$CF" "INFO kernel 5.14.0-1 (전체 동일)"
t_eq "auto_setup code <code> 출력 = codes 파일 내용" "$("$AUTOSETUP" code "$CODE")" "$(cat "$CF")"
rc=0; out=$("$AUTOSETUP" code 0000 2>&1) || rc=$?
t_eq "없는 code → '[X] code 없음' + exit 1" "$out|$rc" "[X] code 없음|1"
t_eq "status: 진행 중인 작업 없음" "$("$AUTOSETUP" status)" "진행 중인 작업 없음"
t_eq "run 디렉터리: targets.txt 3줄" "$(grep -c . "$AS/runs/$CODE/targets.txt")" 3
if [[ -L $AS/runs/$CODE/dhcp.sh ]]; then ok "run 디렉터리: dhcp.sh 심볼릭 링크"; else bad "run 디렉터리: dhcp.sh 심볼릭 링크" ""; fi
t_hasF "os_check 로그: -auto 프롬프트 없이 환경설정 y(IP 호스트는 p/d 아님)" "$AS/runs/$CODE/os_check.log" "(y/n/set) : y (-auto)"

# ============================================================
echo "== e) os6 경로: 로컬 ping 실패 호스트 → ssh 스텁(os6_mgmt) probe / gossh 래퍼"
SSHLOG=$E2E_SCEN/ssh.log
t_has "probe 세션: ssh -o BatchMode=yes -o ServerAliveInterval=30 <os6_mgmt> '<dir>/auto_setup' probe -i '10s'" "$SSHLOG" \
	"^ssh \[-o\] \[BatchMode=yes\] \[-o\] \[ServerAliveInterval=30\] \[$E2E_MGMT\] \['$OS6/auto_setup' probe -i '10s'\]$"
t_eq "probe 로 보낸 목록 = os6 호스트만(host ip)" "$(sort -u "$E2E_SCEN/probe.log" | paste -sd'|')" "probe-list: 192.0.2.1 192.0.2.1"
t_hasF "준비확인(os6): ssh 로 gossh -pm -script uptime" "$SSHLOG" "-pm -script -w \$f 'cat /proc/uptime'"
t_eq "준비확인 gossh: local 은 127.0.0.x 2대만" "$(grep '^local hosts=.* cmd=cat /proc/uptime$' "$E2E_SCEN/gossh.hosts" | head -1)" "local hosts=127.0.0.1 127.0.0.2 cmd=cat /proc/uptime"
t_eq "준비확인 gossh: os6 는 192.0.2.1 만" "$(grep '^os6 hosts=.* cmd=cat /proc/uptime$' "$E2E_SCEN/gossh.hosts" | head -1)" "os6 hosts=192.0.2.1 cmd=cat /proc/uptime"
if [[ -x $AS/bin/gossh ]]; then ok "os6 gossh 래퍼 생성(0755)"; else bad "os6 gossh 래퍼 생성(0755)" ""; fi
t_has "os_check 가 래퍼 경유: 원격(os6) gossh 분류 호출" "$E2E_SCEN/gossh.log" '^os6 \[-pm\] \[-w\] \[[^]]+\] \[hostname\]$'
t_has "os_check 가 래퍼 경유: 원격(os6) run.sh 호출(명령 한 인자 보존)" "$E2E_SCEN/gossh.log" "^os6 \[-w\] \[[^]]+\] \[bash $S/check/run.sh\] \[-script\]$"
t_eq "os6 호스트 결과가 os_check 결과에 포함(postapply 3대)" "$(grep -c . "$AS/runs/$CODE/check.res_${E2E_USER}_postapply")" 3
t_hasF "postapply: os6 호스트 192.0.2.1" "$AS/runs/$CODE/check.res_${E2E_USER}_postapply" "192.0.2.1: OK"

list="$S/q_hosts"
echo "127.0.0.1" > "$list"
tricky="echo \"it's a \$b\" ; ls 'x y' \\ *"
env PATH="$BIN:$ORIG_PATH" E2E_SCEN="$E2E_SCEN" "$AS/bin/gossh" -pm -w "$list" "$tricky" -script > /dev/null 2>&1
t_hasF "래퍼 인자 quoting 보존(공백·따옴표·\$·백슬래시·*)" "$E2E_SCEN/gossh.log" "[$tricky] [-script]"

# ============================================================
echo "== f) 정리 확인"
pkill -KILL -f "^$S/auto_setup daemon$" 2> /dev/null
sleep 1
t_eq "종료 후 데몬 0개" "$(daemon_count)" 0

echo
echo "================ E2E 결과: PASS=$PASS_N FAIL=$FAIL_N ================"
if [[ $FAIL_N -ne 0 ]]; then
	printf ' - %s\n' "${FAILS[@]}"
	echo "--- auto_setup.log (끝 20줄) ---"
	tail -20 "$AS/auto_setup.log" 2> /dev/null
	exit 1
fi
exit 0
