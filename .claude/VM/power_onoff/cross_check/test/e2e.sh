#!/usr/bin/env bash
# cross_check 격리 E2E 시나리오(Linux).
#
# 사용법: bash test/e2e.sh <cross_check 바이너리 경로>
#
# 빌드는 하지 않는다. 임시 디렉터리 하나 안에서 CROSS_CHECK_* 환경변수로 소켓·설정·비밀번호·작업 기록·잠금·로그
# 경로를 모두 바꿔 실제 데몬(--daemon)을 띄우므로 /etc, /var 의 실제 파일은 건드리지 않는다.
# 각 경우마다 PASS/FAIL 을 찍고, 하나라도 실패하면 0 이 아닌 코드로 끝난다. 끝나면 데몬과 임시 디렉터리를 정리한다.
set -euo pipefail

BIN_ARG=${1:?사용법: bash test/e2e.sh <cross_check 바이너리 경로>}
BIN=$(cd "$(dirname "$BIN_ARG")" && pwd)/$(basename "$BIN_ARG")
if [ ! -x "$BIN" ]; then
	echo "실행 파일이 아님: $BIN" >&2
	exit 2
fi

T=$(mktemp -d /tmp/cc_e2e.XXXXXX)
DPID=""
DPID2="" # 범위 검증 시험용 보조 데몬

daemon_pids() {
	# 이 시험의 잠금 파일에 적힌 pid + 처음 띄운 데몬
	{ cat "$T/run/daemon.lock" 2>/dev/null || true; echo "$DPID"; } | tr -s ' \n' '\n' | grep -E '^[0-9]+$' | sort -u || true
}

cleanup() {
	set +e
	local p
	for p in $(jobs -p); do kill -9 "$p" 2>/dev/null; done
	for p in $(daemon_pids); do kill -TERM "$p" 2>/dev/null; done
	[ -z "$DPID2" ] || kill -9 "$DPID2" 2>/dev/null
	sleep 0.5
	for p in $(daemon_pids); do kill -9 "$p" 2>/dev/null; done
	# --ensure 가 띄운 데몬은 이 시험의 바이너리·소켓으로만 찾는다
	for p in $(pgrep -f -- "$BIN --daemon" 2>/dev/null); do
		if tr '\0' '\n' <"/proc/$p/environ" 2>/dev/null | grep -qx "CROSS_CHECK_SOCKET=$T/run/cc.sock"; then
			kill -9 "$p" 2>/dev/null
		fi
	done
	rm -rf "$T"
}
trap cleanup EXIT

export CROSS_CHECK_SOCKET="$T/run/cc.sock"
export CROSS_CHECK_CONF="$T/etc/cross_check.conf"
export CROSS_CHECK_AUTH="$T/etc/auth.conf"
export CROSS_CHECK_STATE="$T/lib/jobs.json"
export CROSS_CHECK_LOCK="$T/run/daemon.lock"
export CROSS_CHECK_LOG="$T/daemon.log"
export CROSS_CHECK_COLOR=never
unset CROSS_CHECK_TOKEN || true
# 시험 전용: 설정 파일 검증의 approval_ttl_sec 최소값(원래 10초)을 3초로 낮춰 만료 시험을 짧게 한다.
# (아래 "범위 검증" 시험은 이 변수를 지우고 실제 최소 10초가 지켜지는지 따로 본다)
export CROSS_CHECK_TEST_MIN_TTL_SEC=3
# 시험 전용: work_timeout_sec 최소값(원래 600초)을 3초로 낮춰 "작업자 응답 없음"(작업중 kill -9) 시험을 짧게 한다.
export CROSS_CHECK_TEST_MIN_WORK_TIMEOUT_SEC=3
# 시험 전용: wait_timeout_sec 최소값(원래 30초)을 2초로 낮춰 "승인 대기 시간 초과"(대기 방치) 시험을 짧게 한다(7c 에서 SIGHUP 으로 4초로 줄임).
export CROSS_CHECK_TEST_MIN_WAIT_TIMEOUT_SEC=2
unset CROSS_CHECK_CALLER || true

mkdir -p "$T/run" "$T/etc" "$T/lib" "$T/targets" "$T/out"
# users= : 작업자·확인자로 쓸 수 있는 이름(dave 는 목록에 있지만 등록하지 않음, zed 는 목록 밖)
cat >"$CROSS_CHECK_CONF" <<EOF
# e2e 임시 설정
target_dir=$T/targets
auth=required
users=alice,bob,carol,dave
approval_ttl_sec=3
work_timeout_sec=3
wait_timeout_sec=600
EOF
printf 'vm01ev01\nvm02ev01\nvm03ev01\n# 주석\n' >"$T/targets/on_off_alice.txt"
printf 'vm01ev01\n' >"$T/targets/on_off_dave.txt"
printf 'vm01ev01\n' >"$T/targets/on_off_zed.txt"
printf 'vm01ev01\tvm02ev01  # 탭·공백 구분과 주석\n' >"$T/t_alice.txt"
printf 'vm03ev01\n' >"$T/t_alice3.txt"

# 비밀번호는 모두 6자 이상
ALICE_PW='alice pw'
BOB_PW='pw=bob 2'
CAROL_PW='carol_pw'
ADMIN_PW='adm_pw_1'

# admin 항목은 install.sh 처럼 미리 써 둔다(0600). 화면·register 로는 만들 수 없다.
(
	umask 077
	printf '# e2e: install.sh 가 만든 것처럼\nadmin=%s\n' "$ADMIN_PW" >"$CROSS_CHECK_AUTH"
)
chmod 600 "$CROSS_CHECK_AUTH"

# raw <JSON 한 줄> : 데몬 소켓에 요청 한 줄을 보내고 응답 한 줄을 출력(perl 의 IO::Socket::UNIX 사용).
HAVE_PERL=0
if command -v perl >/dev/null 2>&1 && perl -MIO::Socket::UNIX -e 1 2>/dev/null; then HAVE_PERL=1; fi
raw() {
	perl -MIO::Socket::UNIX -e '
		my $s = IO::Socket::UNIX->new(Peer => $ARGV[0]) or die "connect: $!\n";
		print $s $ARGV[1], "\n";
		my $l = <$s>;
		print defined $l ? $l : "(응답 없음)\n";
	' "$CROSS_CHECK_SOCKET" "$1"
}

PASS=0
FAIL=0
pass() {
	echo "PASS  $1"
	PASS=$((PASS + 1))
}
fail() {
	local name=$1 f
	shift
	echo "FAIL  $name"
	for f in "$@"; do
		echo "  ---- $f"
		if [ -f "$f" ]; then sed 's/^/    /' "$f" | tail -40; fi
	done
	FAIL=$((FAIL + 1))
}

# tui <user> <출력파일> <입력 줄...>  → RC
tui() {
	local u=$1 o=$2
	shift 2
	RC=0
	printf '%s\n' "$@" | "$BIN" --user "$u" >"$o" 2>&1 || RC=$?
}

# admin <출력파일> <입력 줄...>  → RC
admin() {
	local o=$1
	shift
	RC=0
	printf '%s\n' "$@" | "$BIN" --admin >"$o" 2>&1 || RC=$?
}

# wait_for <파일> <패턴> [최대 0.1초 횟수]
wait_for() {
	local f=$1 pat=$2 max=${3:-100} n=0
	while ! grep -q -- "$pat" "$f" 2>/dev/null; do
		n=$((n + 1))
		if [ "$n" -gt "$max" ]; then return 1; fi
		sleep 0.1
	done
}

# start_worker <이름> <대상 파일> → WPID, JOB (stdout: $T/out/<이름>.out, stderr: .err)
start_worker() {
	local name=$1 tf=$2
	"$BIN" --run --user alice --checker bob --targets-file "$tf" >"$T/out/$name.out" 2>"$T/out/$name.err" &
	WPID=$!
	JOB=""
	if wait_for "$T/out/$name.err" '\[대기\] 작업'; then
		JOB=$(sed -n 's/^\[대기\] 작업 \([^ ]*\) .*/\1/p' "$T/out/$name.err" | head -1)
	fi
}

# job_row <user> <job id> : 그 사용자 화면(줄 입력 화면)에서 'o <ID>' 로 연 작업 상세(내 역할·상태·사유·호출·시각·대상 줄).
# (0.3.0 부터 목록 표에는 사유 대신 호출 스크립트 열이 있으므로 상태·사유는 상세에서 본다)
job_row() {
	printf 'o %s\n\n\nq\n' "$2" | "$BIN" --user "$1" 2>&1 | sed -nE '/^---- 작업 /,/^(이 작업의 확인자|Enter 를 누르면)/p' || true
}

# wait_row <user> <job id> <패턴> : 작업 상세가 패턴과 맞을 때까지(최대 5초)
wait_row() {
	local n=0
	while ! job_row "$1" "$2" | grep -q -- "$3"; do
		n=$((n + 1))
		if [ "$n" -gt 25 ]; then return 1; fi
		sleep 0.2
	done
}

# approve_as_bob <job id> <출력파일> <붙여넣을 대상...> : bob 이 작업을 열어 a → 대상 붙여넣기(빈 줄로 끝) → 패스워드
approve_as_bob() {
	local id=$1 o=$2
	shift 2
	tui bob "$o" "o $id" a "$@" "" "$BOB_PW" q
}

# raw_ok <JSON> <패턴> : 소켓 직접 요청의 응답이 패턴을 포함하는지(perl 없으면 통과로 본다)
raw_has() {
	[ "$HAVE_PERL" = 1 ] || return 0
	raw "$1" | grep -q -- "$2"
}

echo "== cross_check e2e: $BIN"
echo "== 임시 디렉터리: $T"

# ---- 데몬 기동 ----
"$BIN" --daemon >"$T/daemon.out" 2>&1 &
DPID=$!
n=0
while [ ! -S "$CROSS_CHECK_SOCKET" ]; do
	n=$((n + 1))
	if [ "$n" -gt 50 ] || ! kill -0 "$DPID" 2>/dev/null; then
		fail "데몬 기동" "$T/daemon.out" "$T/daemon.log"
		exit 1
	fi
	sleep 0.1
done
if "$BIN" --ensure; then pass "데몬 기동(--daemon) + --ensure 는 떠 있으면 조용히 0"; else fail "--ensure(데몬 있음)" "$T/daemon.log"; fi

# ---- 1. 첫 실행 등록 ----
tui alice "$T/out/reg_alice.txt" "$ALICE_PW" "다른값" "$ALICE_PW" "$ALICE_PW" q
ok=1
[ "$RC" -eq 0 ] || ok=0
grep -q '두 입력이 서로 다릅니다' "$T/out/reg_alice.txt" || ok=0
grep -q '\[등록 완료\]' "$T/out/reg_alice.txt" || ok=0
if grep -qF "$ALICE_PW" "$T/out/reg_alice.txt"; then ok=0; fi # 비밀번호가 화면에 나오면 안 됨
mode=$(stat -c %a "$CROSS_CHECK_AUTH" 2>/dev/null || echo none)
[ "$mode" = 600 ] || ok=0
if [ $ok = 1 ]; then pass "첫 실행 등록(불일치 → 재입력 → 등록), auth.conf 0600, 비밀번호 비노출"; else fail "첫 실행 등록 (mode=$mode rc=$RC)" "$T/out/reg_alice.txt"; fi

tui bob "$T/out/reg_bob.txt" "$BOB_PW" "$BOB_PW" q
rb=$RC
tui carol "$T/out/reg_carol.txt" "$CAROL_PW" "$CAROL_PW" q
rcarol=$RC
tui alice "$T/out/reg_again.txt" q
if [ "$rb$rcarol$RC" = 000 ] && grep -q '\[등록 완료\]' "$T/out/reg_carol.txt" && ! grep -q '첫 실행' "$T/out/reg_again.txt" &&
	grep -qx "admin=$ADMIN_PW" "$CROSS_CHECK_AUTH"; then
	pass "bob·carol 등록(미리 쓴 admin 줄 유지), 재실행 시 다시 묻지 않음"
else
	fail "추가 등록 ($rb/$rcarol/$RC)" "$T/out/reg_bob.txt" "$T/out/reg_carol.txt" "$T/out/reg_again.txt"
fi

# 6자 미만 비밀번호는 등록 거부(3번 모두 짧으면 exit 1)
tui dave "$T/out/reg_short.txt" "12345" "12345" "abc" "abc" "x1" "x1" q
if [ "$RC" -eq 1 ] && grep -q '6~128자' "$T/out/reg_short.txt" && ! grep -q '^dave=' "$CROSS_CHECK_AUTH"; then
	pass "6자 미만 비밀번호 등록 거부"
else
	fail "짧은 비밀번호 (rc=$RC)" "$T/out/reg_short.txt"
fi

# users= 목록 밖 이름은 등록 불가(가짜 확인자 만들기 차단)
tui zed "$T/out/reg_zed.txt" "zed_pw_1" "zed_pw_1" q
if [ "$RC" -eq 1 ] && grep -q 'users= 목록에 없어' "$T/out/reg_zed.txt" && ! grep -q '^zed=' "$CROSS_CHECK_AUTH"; then
	pass "users= 목록 밖 이름(zed) 등록 거부, exit 1"
else
	fail "목록 밖 등록 (rc=$RC)" "$T/out/reg_zed.txt"
fi

# admin 은 등록할 수 없음: CLI 는 예약 이름으로 막고(exit 2), 데몬도 register admin 을 거부
RC=0
printf 'squat_pw_1\nsquat_pw_1\nq\n' | "$BIN" --user admin >"$T/out/reg_admin_cli.txt" 2>&1 || RC=$?
if [ "$HAVE_PERL" = 1 ]; then
	raw '{"op":"register","user":"admin","password":"squat_pw_1"}' >"$T/out/reg_admin_raw.txt" 2>&1 || true
	raw_ok=0
	grep -q '"ok":false' "$T/out/reg_admin_raw.txt" && grep -q '등록할 수 없음' "$T/out/reg_admin_raw.txt" && raw_ok=1
else
	echo "SKIP  perl 이 없어 소켓 직접 register admin 시험 생략"
	raw_ok=1
fi
if [ "$RC" -eq 2 ] && [ "$raw_ok" = 1 ] && [ "$(grep -c '^admin=' "$CROSS_CHECK_AUTH")" -eq 1 ] && grep -qx "admin=$ADMIN_PW" "$CROSS_CHECK_AUTH"; then
	pass "register admin 거부(CLI exit 2, 데몬 거부), admin 줄은 그대로"
else
	fail "register admin (rc=$RC raw=$raw_ok)" "$T/out/reg_admin_cli.txt" "$T/out/reg_admin_raw.txt"
fi

# ---- 2. 거부되는 run ----
RC=0
"$BIN" --run --user alice --checker alice --targets-file "$T/t_alice.txt" >"$T/out/same.out" 2>"$T/out/same.err" || RC=$?
if [ "$RC" -eq 1 ] && grep -q '동일' "$T/out/same.err" && grep -q '중단으로 기록' "$T/out/same.err" && [ ! -s "$T/out/same.out" ]; then
	pass "작업자=확인자 → 즉시 중단(기록), exit 1"
else
	fail "작업자=확인자 (rc=$RC)" "$T/out/same.err"
fi

RC=0
"$BIN" --run --user dave --cechker bob --targets-file "$T/t_alice3.txt" >"$T/out/dave.out" 2>"$T/out/dave.err" || RC=$?
if [ "$RC" -eq 3 ] && grep -q '미등록' "$T/out/dave.err"; then
	pass "미등록 작업자 거부(--cechker 오타 플래그), exit 3"
else
	fail "미등록 작업자 (rc=$RC)" "$T/out/dave.err"
fi

RC=0
"$BIN" --run --user alice --checker zed --targets-file "$T/t_alice.txt" >"$T/out/zed_chk.out" 2>"$T/out/zed_chk.err" || RC=$?
RC2=0
"$BIN" --run --user zed --checker bob --targets-file "$T/t_alice3.txt" >"$T/out/zed_wrk.out" 2>"$T/out/zed_wrk.err" || RC2=$?
if [ "$RC" -eq 3 ] && grep -q '확인자 zed 는 cross_check.conf 의 users= 목록에 없음' "$T/out/zed_chk.err" && [ ! -s "$T/out/zed_chk.out" ] &&
	[ "$RC2" -eq 3 ] && grep -q '작업자 zed 는' "$T/out/zed_wrk.err"; then
	pass "users= 목록 밖 확인자·작업자(zed) --run 거부, exit 3"
else
	fail "목록 밖 run ($RC/$RC2)" "$T/out/zed_chk.err" "$T/out/zed_wrk.err"
fi

RC=0
printf 'vm01ev01 vm99zz01\n' | "$BIN" --run --user alice --checker bob >"$T/out/outside.out" 2>"$T/out/outside.err" || RC=$?
if [ "$RC" -eq 3 ] && grep -q '등록되지 않은 대상' "$T/out/outside.err" && grep -q 'vm99zz01' "$T/out/outside.err"; then
	pass "on_off_alice.txt 밖의 대상(stdin) 거부, exit 3"
else
	fail "대상 밖 (rc=$RC)" "$T/out/outside.err"
fi

# ---- 3. 정상 흐름: 대기 → 틀린 패스워드 → 제3자 → 승인 → --start ----
start_worker w1 "$T/t_alice.txt"
J1=$JOB
if [ -n "$J1" ] && job_row bob "$J1" | grep -q '대기'; then
	pass "작업 등록 → 대기 (작업 $J1)"
else
	fail "대기 등록" "$T/out/w1.err"
fi

tui bob "$T/out/wrong.txt" "o $J1" a vm02ev01 vm01ev01 "" "틀린비번" "" q
if grep -q '승인 실패: 인증 실패' "$T/out/wrong.txt" && job_row bob "$J1" | grep -q '대기' && kill -0 "$WPID" 2>/dev/null; then
	pass "틀린 패스워드 승인 거부(대기 유지, 작업자 계속 대기)"
else
	fail "틀린 패스워드" "$T/out/wrong.txt"
fi

tui carol "$T/out/carol_open.txt" "o $J1" q
tui bob "$T/out/carol_as_bob.txt" "o $J1" a vm01ev01 vm02ev01 "" "$CAROL_PW" "" q
if grep -q '열 수 없습니다' "$T/out/carol_open.txt" && grep -q '인증 실패' "$T/out/carol_as_bob.txt" && job_row bob "$J1" | grep -q '대기'; then
	pass "지정 확인자가 아닌 carol: 작업을 못 열고, bob 사칭(carol 패스워드)도 거부"
else
	fail "제3자 승인 차단" "$T/out/carol_open.txt" "$T/out/carol_as_bob.txt"
fi

# 확인자에게는 대상이 보이지 않는다(목록·상세·소켓 응답 모두) — 작업자에게는 보인다
tui bob "$T/out/bob_view.txt" "o $J1" "" q
ok=1
grep -q '확인자에게는 표시되지 않음' "$T/out/bob_view.txt" || ok=0
if grep -q 'vm01ev01\|vm02ev01' "$T/out/bob_view.txt"; then ok=0; fi
if [ "$HAVE_PERL" = 1 ]; then
	raw '{"op":"list","user":"bob"}' >"$T/out/bob_raw.txt"
	if grep -q 'vm01ev01\|target' "$T/out/bob_raw.txt"; then ok=0; fi
	raw '{"op":"list","user":"alice"}' | grep -q '"target_count":2' || ok=0
fi
job_row alice "$J1" | grep -q 'vm02ev01' || ok=0
grep -q '위 대상 목록을 확인자 bob 에게 전달하세요' "$T/out/w1.err" || ok=0
if [ $ok = 1 ]; then pass "확인자에게 대상 숨김(화면·소켓 list 응답), 작업자 상세·--run 안내에는 대상 표시"; else fail "확인자 대상 숨김" "$T/out/bob_view.txt" "$T/out/bob_raw.txt"; fi

# 대상 붙여넣기(순서 바꿔·탭 섞어) + 패스워드 → 승인
approve_as_bob "$J1" "$T/out/approve1.txt" "$(printf 'vm02ev01\tvm01ev01')"
RC=0
wait "$WPID" || RC=$?
lines=$(wc -l <"$T/out/w1.out")
if [ "$RC" -eq 0 ] && grep -q '\[승인 완료\]' "$T/out/approve1.txt" && ! grep -q 'vm02ev01' "$T/out/approve1.txt" &&
	grep -q '인식한 이름: 2개' "$T/out/approve1.txt" &&
	grep -qx "CROSS_CHECK_JOB=$J1" "$T/out/w1.out" && grep -qE '^CROSS_CHECK_TOKEN=[0-9a-f]{32}$' "$T/out/w1.out" && [ "$lines" -eq 2 ] &&
	grep -q '\[승인\] 작업' "$T/out/w1.err" && grep -q '내 작업 리스트' "$T/out/w1.err" && grep -q '필수 정책' "$T/out/w1.err" &&
	grep -q '승인 완료 후 3초 내에' "$T/out/w1.err" && grep -q '승인 유효시간: 3초' "$T/out/w1.err" && ! grep -q '2분' "$T/out/w1.err" &&
	grep -q '승인 완료 후 3초 내에' "$T/out/approve1.txt" && ! grep -q '2분' "$T/out/approve1.txt"; then
	pass "확인자 대상 붙여넣기+패스워드 승인 → --run exit 0, stdout 은 CROSS_CHECK_JOB/TOKEN 두 줄, stderr 에 내 작업 리스트·정책(설정한 3초로 표시, 2분 하드코딩 없음), 확인자 승인 화면도 3초"
else
	fail "승인 흐름 (rc=$RC lines=$lines)" "$T/out/approve1.txt" "$T/out/w1.out" "$T/out/w1.err"
fi

RC3=0
"$BIN" --start --job "$J1" --token 00000000000000000000000000000000 >"$T/out/start1c.txt" 2>&1 || RC3=$?
RC=0
(eval "$(cat "$T/out/w1.out")" && export CROSS_CHECK_TOKEN && "$BIN" --start --job "$CROSS_CHECK_JOB") >"$T/out/start1.txt" 2>&1 || RC=$?
RC2=0
(eval "$(cat "$T/out/w1.out")" && "$BIN" --start --job "$CROSS_CHECK_JOB" --token "$CROSS_CHECK_TOKEN") >"$T/out/start1b.txt" 2>&1 || RC2=$?
if [ "$RC" -eq 0 ] && grep -q '\[시작\]' "$T/out/start1.txt" && [ "$RC2" -eq 1 ] && grep -q '이미 시작' "$T/out/start1b.txt" &&
	[ "$RC3" -eq 1 ] && grep -q '인증 실패' "$T/out/start1c.txt"; then
	pass "틀린 token exit 1(인증 실패), --start(env token) exit 0, 사용한 token 으로 두 번째 시작은 exit 1"
else
	fail "--start ($RC/$RC2/$RC3)" "$T/out/start1.txt" "$T/out/start1b.txt" "$T/out/start1c.txt"
fi
if job_row alice "$J1" | grep -q '본 작업 시작' && job_row alice "$J1" | grep -qx '  상태      : 승인/작업중'; then
	pass "--start 후 승인/작업중(본 작업 시작 기록)"
else
	fail "시작 표시"
	job_row alice "$J1"
fi
# 승인 유효시간(3초)이 지나도 시작한 작업은 만료되지 않는다(작업 제한 3초 안에 finish)
RC=0
(eval "$(cat "$T/out/w1.out")" && export CROSS_CHECK_TOKEN && "$BIN" --finish --job "$CROSS_CHECK_JOB" --result done) >"$T/out/fin1.txt" 2>&1 || RC=$?
RC2=0
(eval "$(cat "$T/out/w1.out")" && export CROSS_CHECK_TOKEN && "$BIN" --finish --job "$CROSS_CHECK_JOB" --result aborted) >"$T/out/fin1b.txt" 2>&1 || RC2=$?
RC3=0
(eval "$(cat "$T/out/w1.out")" && export CROSS_CHECK_TOKEN && "$BIN" --start --job "$CROSS_CHECK_JOB") >"$T/out/fin1c.txt" 2>&1 || RC3=$?
if [ "$RC" -eq 0 ] && grep -q '승인/작업완료' "$T/out/fin1.txt" && [ "$RC2" -eq 1 ] && grep -q '이미 끝난 작업' "$T/out/fin1b.txt" &&
	[ "$RC3" -eq 1 ] && grep -q '이미 끝난 작업' "$T/out/fin1c.txt" && job_row alice "$J1" | grep -qx '  상태      : 승인/작업완료'; then
	pass "--finish --result done → 승인/작업완료(exit 0), 끝난 뒤 finish·start 는 exit 1(이미 끝난 작업)"
else
	fail "finish done ($RC/$RC2/$RC3)" "$T/out/fin1.txt" "$T/out/fin1b.txt" "$T/out/fin1c.txt"
fi

# ---- 4. 확인자 취소 ----
start_worker w2 "$T/t_alice3.txt"
J2=$JOB
tui bob "$T/out/cancel.txt" "o $J2" c "$BOB_PW" q
RC=0
wait "$WPID" || RC=$?
if [ -n "$J2" ] && [ "$RC" -eq 1 ] && grep -q '확인자 bob 가 취소' "$T/out/w2.err" && [ ! -s "$T/out/w2.out" ] &&
	job_row alice "$J2" | grep -q '확인자 bob 가 취소' && grep -q '취소 완료' "$T/out/cancel.txt"; then
	pass "확인자 취소 → 작업자 exit 1(사유 출력), 작업자 표에 사유"
else
	fail "확인자 취소 (rc=$RC)" "$T/out/cancel.txt" "$T/out/w2.err"
fi

# ---- 5. 대기 중 작업자 kill -9 / Ctrl+C ----
start_worker w3 "$T/t_alice3.txt"
J3=$JOB
kill -9 "$WPID"
wait "$WPID" 2>/dev/null || true
if [ -n "$J3" ] && wait_row alice "$J3" '작업자 스크립트 종료' && job_row alice "$J3" | grep -q '중단'; then
	pass "대기 중 작업자 kill -9 → 즉시 중단(작업자 스크립트 종료)"
else
	fail "kill -9" "$T/out/w3.err" "$T/daemon.log"
fi

start_worker w4 "$T/t_alice3.txt"
J4=$JOB
kill -INT "$WPID"
RC=0
wait "$WPID" || RC=$?
if [ -n "$J4" ] && [ "$RC" -eq 130 ] && wait_row alice "$J4" '작업자 스크립트 종료'; then
	pass "대기 중 Ctrl+C(SIGINT) → exit 130, 작업 중단"
else
	fail "Ctrl+C (rc=$RC)" "$T/out/w4.err"
fi

# ---- 6. 승인 후 유효시간(시험 설정 3초) 초과 → --start 거부 ----
start_worker w5 "$T/t_alice3.txt"
J5=$JOB
approve_as_bob "$J5" "$T/out/approve5.txt" vm03ev01
RC=0
wait "$WPID" || RC=$?
sleep 4.5
RC2=0
(eval "$(cat "$T/out/w5.out")" && export CROSS_CHECK_TOKEN && "$BIN" --start --job "$CROSS_CHECK_JOB") >"$T/out/start5.txt" 2>&1 || RC2=$?
if [ -n "$J5" ] && [ "$RC" -eq 0 ] && [ "$RC2" -eq 1 ] && grep -q '미진행' "$T/out/start5.txt" &&
	job_row alice "$J5" | grep -q '승인 후 3초 내 미진행' && grep -q '승인 후 3초 내 미진행' "$T/out/start5.txt" && ! grep -q '2분' "$T/out/start5.txt"; then
	pass "승인 후 유효시간(설정 3초) 초과 → --start exit 1, 메시지·작업자 표 사유 모두 '승인 후 3초 내 미진행'"
else
	fail "만료 (run=$RC start=$RC2)" "$T/out/approve5.txt" "$T/out/w5.err" "$T/out/start5.txt"
fi

# ---- 6b. 붙여넣은 대상 불일치 → 중단(개수만 알림) ----
start_worker w7 "$T/t_alice.txt"
J7=$JOB
approve_as_bob "$J7" "$T/out/approve7.txt" vm01ev01
RC=0
wait "$WPID" || RC=$?
if [ -n "$J7" ] && [ "$RC" -eq 1 ] && grep -q '붙여넣은 대상 불일치(빠짐 1·남음 0·중복 0)' "$T/out/approve7.txt" &&
	! grep -q 'vm02ev01' "$T/out/approve7.txt" && grep -q '붙여넣은 대상 불일치(빠짐 1·남음 0·중복 0)' "$T/out/w7.err" &&
	job_row alice "$J7" | grep -qx '  상태      : 중단' && ! grep -q 'vm02ev01' "$T/daemon.log"; then
	pass "붙여넣은 대상 불일치 → 작업 중단, 작업자 exit 1, 확인자·작업자·로그에는 개수만(이름 없음)"
else
	fail "불일치 (rc=$RC)" "$T/out/approve7.txt" "$T/out/w7.err"
fi

# 틀린 패스워드면 붙여넣기가 틀려도 상태 그대로(패스워드를 먼저 확인)
start_worker w7b "$T/t_alice3.txt"
J7B=$JOB
tui bob "$T/out/approve7b.txt" "o $J7B" a vm99zz99 "" "틀린비번" "" q
if [ -n "$J7B" ] && grep -q '인증 실패' "$T/out/approve7b.txt" && ! grep -q '불일치' "$T/out/approve7b.txt" &&
	job_row bob "$J7B" | grep -qx '  상태      : 대기' && kill -0 "$WPID" 2>/dev/null; then
	pass "틀린 패스워드 + 틀린 붙여넣기 → 인증 실패만, 작업은 대기 유지(대조 전)"
else
	fail "패스워드 먼저" "$T/out/approve7b.txt"
fi
kill -INT "$WPID" 2>/dev/null || true
wait "$WPID" 2>/dev/null || true

# ---- 6c. 본 작업 단계: 작업중 → aborted / 시작 전 aborted / 시작 전 done 거부 ----
start_worker w9 "$T/t_alice3.txt"
J9=$JOB
approve_as_bob "$J9" "$T/out/approve9.txt" vm03ev01
wait "$WPID" || true
RA=0
(eval "$(cat "$T/out/w9.out")" && export CROSS_CHECK_TOKEN && "$BIN" --start --job "$CROSS_CHECK_JOB" &&
	"$BIN" --finish --job "$CROSS_CHECK_JOB" --result aborted) >"$T/out/fin9.txt" 2>&1 || RA=$?
start_worker w10 "$T/t_alice3.txt"
J10=$JOB
approve_as_bob "$J10" "$T/out/approve10.txt" vm03ev01
wait "$WPID" || true
RD=0
(eval "$(cat "$T/out/w10.out")" && export CROSS_CHECK_TOKEN && "$BIN" --finish --job "$CROSS_CHECK_JOB" --result done) >"$T/out/fin10a.txt" 2>&1 || RD=$?
RB=0
(eval "$(cat "$T/out/w10.out")" && export CROSS_CHECK_TOKEN && "$BIN" --finish --job "$CROSS_CHECK_JOB" --result aborted) >"$T/out/fin10b.txt" 2>&1 || RB=$?
RS=0
(eval "$(cat "$T/out/w10.out")" && export CROSS_CHECK_TOKEN && "$BIN" --start --job "$CROSS_CHECK_JOB") >"$T/out/fin10c.txt" 2>&1 || RS=$?
if [ "$RA" -eq 0 ] && job_row alice "$J9" | grep -qx '  상태      : 승인/작업중단' && job_row alice "$J9" | grep -q '작업자가 작업을 중단' &&
	[ "$RD" -eq 1 ] && grep -q '시작하지 않아' "$T/out/fin10a.txt" && [ "$RB" -eq 0 ] && [ "$RS" -eq 1 ] &&
	job_row alice "$J10" | grep -qx '  상태      : 승인/작업중단' && job_row alice "$J10" | grep -q '작업자가 본 작업 전에 중단'; then
	pass "작업중 → --finish aborted = 승인/작업중단, 시작 전 done 거부(exit 1)·aborted = 승인/작업중단(본 작업 전에 중단), 그 뒤 start 거부"
else
	fail "작업 단계 ($RA/$RD/$RB/$RS)" "$T/out/fin9.txt" "$T/out/fin10a.txt" "$T/out/fin10b.txt" "$T/out/fin10c.txt"
fi

# ---- 6d. 작업중에 작업자 kill -9 → 작업 제한(시험 3초) 뒤 승인/작업중단(작업자 응답 없음), caller 자동 감지 ----
cat >"$T/work_kill.sh" <<EOS
#!/bin/bash
# e2e: 실제 작업 스크립트처럼 --run → --start → (작업) → --finish. 작업 중에 kill -9 당한다.
out=\$("$BIN" --run --user alice --checker bob --targets-file "\$1") || exit 1
eval "\$out"; export CROSS_CHECK_TOKEN
"$BIN" --start --job "\$CROSS_CHECK_JOB" || exit 1
echo "STARTED \$CROSS_CHECK_JOB"
sleep 300
"$BIN" --finish --job "\$CROSS_CHECK_JOB" --result done
EOS
bash "$T/work_kill.sh" "$T/t_alice3.txt" >"$T/out/wk.out" 2>"$T/out/wk.err" &
WK=$!
JK=""
if wait_for "$T/out/wk.err" '\[대기\] 작업'; then JK=$(sed -n 's/^\[대기\] 작업 \([^ ]*\) .*/\1/p' "$T/out/wk.err" | head -1); fi
approve_as_bob "$JK" "$T/out/approvek.txt" vm03ev01
ok=1
wait_for "$T/out/wk.out" 'STARTED' 50 || ok=0
# 스크립트(bash)를 먼저 죽이고 그 자식(sleep)을 죽인다 — 자식부터 죽이면 스크립트가 다음 줄(--finish)로 넘어간다
KIDS=$(pgrep -P "$WK" 2>/dev/null || true)
kill -9 "$WK" 2>/dev/null || true
# shellcheck disable=SC2086
[ -z "$KIDS" ] || kill -9 $KIDS 2>/dev/null || true
wait "$WK" 2>/dev/null || true
sleep 4.5
if [ $ok = 1 ] && [ -n "$JK" ] && job_row alice "$JK" | grep -qx '  상태      : 승인/작업중단' && job_row alice "$JK" | grep -q '작업자 응답 없음' &&
	job_row alice "$JK" | grep -q '호출      : work_kill.sh' && grep -q '호출: work_kill.sh' "$T/out/wk.err"; then
	pass "작업중 작업자 kill -9 → 승인 유효시간으로는 만료되지 않고 작업 제한(3초) 뒤 승인/작업중단(작업자 응답 없음), caller 자동 감지 = work_kill.sh"
else
	fail "작업 제한 (ok=$ok job=$JK)" "$T/out/wk.err" "$T/out/wk.out" "$T/daemon.log"
	job_row alice "$JK"
fi

# ---- 6e. caller: --caller / CROSS_CHECK_CALLER / 자동(이 시험 스크립트) ----
RC=0
"$BIN" --run --user alice --checker bob --targets-file "$T/t_alice3.txt" --caller 'my_job.sh' >"$T/out/c1.out" 2>"$T/out/c1.err" &
C1=$!
wait_for "$T/out/c1.err" '\[대기\] 작업' || true
JC1=$(sed -n 's/^\[대기\] 작업 \([^ ]*\) .*/\1/p' "$T/out/c1.err" | head -1)
CROSS_CHECK_CALLER='env_job.sh' "$BIN" --run --user alice --checker bob --targets-file "$T/t_alice3.txt" >"$T/out/c2.out" 2>"$T/out/c2.err" &
C2=$!
wait_for "$T/out/c2.err" '\[대기\] 작업' || true
JC2=$(sed -n 's/^\[대기\] 작업 \([^ ]*\) .*/\1/p' "$T/out/c2.err" | head -1)
ok=1
job_row bob "$JC1" | grep -q '호출      : my_job.sh' || ok=0
job_row bob "$JC2" | grep -q '호출      : env_job.sh' || ok=0
job_row alice "$J7" | grep -q '호출      : e2e.sh' || ok=0 # start_worker 는 이 스크립트(bash e2e.sh)가 직접 부름
printf 'q\n' | "$BIN" --user bob 2>&1 | grep -q 'my_job.sh' || ok=0 # 목록 표의 호출 스크립트 열
kill -INT "$C1" "$C2" 2>/dev/null || true
wait "$C1" 2>/dev/null || true
wait "$C2" 2>/dev/null || true
if [ $ok = 1 ]; then pass "caller 기록: --caller my_job.sh, CROSS_CHECK_CALLER=env_job.sh, 자동 감지 e2e.sh (상세·목록 표)"; else
	fail "caller" "$T/out/c1.err" "$T/out/c2.err"
	job_row bob "$JC1"
	job_row alice "$J7"
fi

# ---- 6f. ping 의 pid, 줄 입력 화면 머리말 ----
if raw_has '{"op":"ping"}' "\"pid\":$DPID" && raw_has '{"op":"ping"}' '"work_timeout_sec":3'; then
	pass "ping 응답에 pid(데몬 $DPID)·work_timeout_sec(3)"
else
	fail "ping pid"
	raw '{"op":"ping"}' || true
fi

# ---- 7. 관리자 ----
admin "$T/out/adm_bad.txt" x y z
RA=$RC
admin "$T/out/adm_list.txt" "$ADMIN_PW" 1 6 q
if [ "$RA" -eq 1 ] && grep -q '인증 실패' "$T/out/adm_bad.txt" && [ "$RC" -eq 0 ] && grep -q "$J1" "$T/out/adm_list.txt" &&
	grep -q '작업별 대상' "$T/out/adm_list.txt" && grep -q '조회 기능이 없어' "$T/out/adm_list.txt"; then
	pass "관리자: 틀린 패스워드 3회 → exit 1, 올바른 패스워드로 전체 목록(대상 포함)"
else
	fail "관리자 ($RA/$RC)" "$T/out/adm_bad.txt" "$T/out/adm_list.txt"
fi

# ---- 7b. 승인 유효시간(approval_ttl_sec): 화면 표시 · SIGHUP 다시 읽기 · 범위 검증 ----
# 이 데몬은 conf 의 approval_ttl_sec=3 으로 떠 있다(위 3·6번이 이미 3초 만료를 확인). 여기서는 값이 화면·핑으로 보이는지,
# 데몬을 죽이지 않고 SIGHUP 으로 값만 바꿀 수 있는지(잘못된 값은 기존 유지), 그리고 최소 10초/최대 3600초 검증을 본다.
conf_ttl() { sed -i "s/^approval_ttl_sec=.*/approval_ttl_sec=$1/" "$CROSS_CHECK_CONF"; }
# ttl_is <표시 문구> <초> : 사용자 화면 머리말이 그 값이 될 때까지(최대 3초)
ttl_is() {
	local n=0 o want="승인 유효시간: $1 (approval_ttl_sec=$2)"
	# 파이프 끝에서 grep -q 가 먼저 닫으면 SIGPIPE(pipefail)로 오판할 수 있어 출력을 변수로 받아 비교한다
	while o=$(printf 'q\n' | "$BIN" --user alice 2>&1 || true); [ "${o#*"$want"}" = "$o" ]; do
		n=$((n + 1))
		if [ "$n" -gt 30 ]; then return 1; fi
		sleep 0.1
	done
}

admin "$T/out/ttl_adm.txt" "$ADMIN_PW" q
ok=1
ttl_is 3초 3 || ok=0
grep -q '승인 유효시간: 3초 (approval_ttl_sec=3)' "$T/out/ttl_adm.txt" || ok=0
if [ "$HAVE_PERL" = 1 ]; then pr=$(raw '{"op":"ping"}'); case $pr in *'"ttl_sec":3'*) ;; *) ok=0 ;; esac; fi
if [ $ok = 1 ]; then pass "사용자·관리자 화면 머리말과 ping(ttl_sec)에 설정한 승인 유효시간(3초) 표시"; else fail "유효시간 표시" "$T/out/ttl_adm.txt"; fi

conf_ttl 7
kill -HUP "$DPID"
ok=1
ttl_is 7초 7 || ok=0
kill -0 "$DPID" 2>/dev/null || ok=0
if [ "$HAVE_PERL" = 1 ]; then pr=$(raw '{"op":"ping"}'); case $pr in *'"ttl_sec":7'*) ;; *) ok=0 ;; esac; fi
grep -q '3초(3초) → 7초(7초)' "$T/daemon.log" || ok=0
for bad in abc 99999 0; do
	conf_ttl "$bad"
	kill -HUP "$DPID"
	sleep 0.4
	ttl_is 7초 7 || ok=0 # 잘못된 값이면 기존(7초) 유지
done
kill -0 "$DPID" 2>/dev/null || ok=0
grep -q '승인 유효시간 7초 유지' "$T/daemon.log" || ok=0
conf_ttl 3
kill -HUP "$DPID"
ttl_is 3초 3 || ok=0
if [ $ok = 1 ]; then pass "SIGHUP: 데몬 유지한 채 approval_ttl_sec 만 다시 읽음(3→7초), 잘못된 값(abc/99999/0)은 기존 값 유지+로그, 다시 3초로 복귀"; else fail "SIGHUP 다시 읽기" "$T/daemon.log"; fi

# 범위 검증: 최소 10 · 최대 3600 (시험용 최소값 변수를 지우고 실제 규칙으로 확인). 데몬은 뜨지 않고 exit 3 + 한국어 안내
mkdir -p "$T/run2" "$T/lib2"
ttl_conf="$T/etc/ttl2.conf"
ok=1
for v in 5 9 99999 3601 0 -1 abc; do
	printf 'target_dir=%s\nusers=alice,bob\napproval_ttl_sec=%s\n' "$T/targets" "$v" >"$ttl_conf"
	RC=0
	env -u CROSS_CHECK_TEST_MIN_TTL_SEC CROSS_CHECK_CONF="$ttl_conf" CROSS_CHECK_SOCKET="$T/run2/cc.sock" CROSS_CHECK_LOCK="$T/run2/daemon.lock" \
		CROSS_CHECK_STATE="$T/lib2/jobs.json" CROSS_CHECK_LOG="$T/daemon2.log" "$BIN" --daemon >"$T/out/badttl.txt" 2>&1 || RC=$?
	if [ "$RC" -ne 3 ] || ! grep -q 'approval_ttl_sec 는 10~3600 사이의 정수' "$T/out/badttl.txt" || [ -S "$T/run2/cc.sock" ]; then
		ok=0
		echo "  (approval_ttl_sec=$v 가 거부되지 않음 rc=$RC)"
		cat "$T/out/badttl.txt"
	fi
done
if [ $ok = 1 ]; then pass "approval_ttl_sec 5·9·99999·3601·0·-1·abc 는 데몬 기동 거부(exit 3, '10~3600 사이의 정수')"; else fail "범위 검증(거부)"; fi

# 경계값 10 은 허용: 보조 데몬을 따로 띄워 화면 머리말에서 10초 확인
printf 'target_dir=%s\nusers=alice,bob\napproval_ttl_sec=10\n' "$T/targets" >"$ttl_conf"
env -u CROSS_CHECK_TEST_MIN_TTL_SEC CROSS_CHECK_CONF="$ttl_conf" CROSS_CHECK_SOCKET="$T/run2/cc.sock" CROSS_CHECK_LOCK="$T/run2/daemon.lock" \
	CROSS_CHECK_STATE="$T/lib2/jobs.json" CROSS_CHECK_LOG="$T/daemon2.log" "$BIN" --daemon >"$T/out/ttl10.txt" 2>&1 &
DPID2=$!
n=0
while [ ! -S "$T/run2/cc.sock" ] && [ "$n" -lt 50 ]; do
	n=$((n + 1))
	sleep 0.1
done
RC=0
printf 'q\n' | CROSS_CHECK_SOCKET="$T/run2/cc.sock" "$BIN" --user alice >"$T/out/ttl10_hdr.txt" 2>&1 || RC=$?
if [ "$RC" -eq 0 ] && grep -q '승인 유효시간: 10초 (approval_ttl_sec=10)' "$T/out/ttl10_hdr.txt"; then
	pass "approval_ttl_sec=10(최솟값)은 허용되고 화면에 10초로 표시"
else
	fail "최솟값 10 (rc=$RC)" "$T/out/ttl10.txt" "$T/out/ttl10_hdr.txt" "$T/daemon2.log"
fi
kill "$DPID2" 2>/dev/null || true
wait "$DPID2" 2>/dev/null || true
DPID2=""

# ---- 7c. 승인 대기 제한(wait_timeout_sec): 화면 표시 · SIGHUP · 대기 방치 → 중단(--run exit 1) · 승인된 작업 영향 없음 · 범위 ----
# 이 데몬은 conf 의 wait_timeout_sec=600(기본값과 같음)으로 떠 있다. SIGHUP 으로 4초로 줄여 방치된 대기 작업이 중단되는지 본다.
conf_wait() { sed -i "s/^wait_timeout_sec=.*/wait_timeout_sec=$1/" "$CROSS_CHECK_CONF"; }
# wait_is <표시 문구> <초> : 사용자 화면(줄 입력) 머리말이 그 값이 될 때까지(최대 3초)
wait_is() {
	local n=0 o want="승인 대기 제한: $1 (wait_timeout_sec=$2)"
	while o=$(printf 'q\n' | "$BIN" --user alice 2>&1 || true); [ "${o#*"$want"}" = "$o" ]; do
		n=$((n + 1))
		if [ "$n" -gt 30 ]; then return 1; fi
		sleep 0.1
	done
}
ok=1
wait_is 10분 600 || ok=0
admin "$T/out/wait_adm.txt" "$ADMIN_PW" q
grep -q '승인 대기 제한: 10분 (wait_timeout_sec=600)' "$T/out/wait_adm.txt" || ok=0
raw_has '{"op":"ping"}' '"wait_timeout_sec":600' || ok=0
if [ $ok = 1 ]; then pass "사용자·관리자 화면 머리말과 ping(wait_timeout_sec)에 승인 대기 제한(10분) 표시"; else fail "대기 제한 표시" "$T/out/wait_adm.txt"; fi

conf_wait 4
kill -HUP "$DPID"
ok=1
wait_is 4초 4 || ok=0
grep -q '승인 대기 제한 시간 10분(600초) → 4초(4초)' "$T/daemon.log" || ok=0
if [ $ok = 1 ]; then pass "SIGHUP: wait_timeout_sec 다시 읽음(600→4초) + 로그"; else fail "SIGHUP wait_timeout_sec" "$T/daemon.log"; fi

# 대기 방치: 아무도 승인하지 않으면 4초 뒤 데몬이 중단 → --run 은 사유를 받고 exit 1, stdout 의 JOB/TOKEN 없음
start_worker wwait "$T/t_alice3.txt"
JW=$JOB
WWP=$WPID
RC=0
n=0
while kill -0 "$WWP" 2>/dev/null && [ "$n" -lt 100 ]; do
	n=$((n + 1))
	sleep 0.1
done
wait "$WWP" || RC=$?
ok=1
[ -n "$JW" ] || ok=0
[ "$RC" -eq 1 ] || ok=0
grep -q '승인 대기 제한 4초 — [0-9][0-9]:[0-9][0-9] 까지 (이 시각까지 승인되지 않으면 자동 중단)' "$T/out/wwait.err" || ok=0
grep -q "\[중단\] 작업 $JW — 승인 대기 시간 초과(4초)" "$T/out/wwait.err" || ok=0
[ ! -s "$T/out/wwait.out" ] || ok=0
job_row alice "$JW" | grep -q '승인 대기 시간 초과(4초)' || ok=0
job_row alice "$JW" | grep -qx '  상태      : 중단' || ok=0
if [ $ok = 1 ]; then pass "대기 방치 → 4초 뒤 중단(승인 대기 시간 초과(4초)), --run exit 1·안내문에 'HH:MM 까지'·stdout 비어 있음"; else
	fail "대기 방치 (rc=$RC job=$JW)" "$T/out/wwait.err" "$T/out/wwait.out" "$T/daemon.log"
	job_row alice "$JW"
fi

# 승인된 작업은 대기 제한의 영향을 받지 않음: 바로 승인 → --run exit 0 → (승인 유효시간 3초 안에) --start → --finish done
# → 대기 제한(4초)보다 오래 기다려도 승인/작업완료 그대로(작업 제한 3초와 겹치지 않게 바로 끝낸다)
start_worker wwok "$T/t_alice3.txt"
JWO=$JOB
approve_as_bob "$JWO" "$T/out/approve_wwok.txt" vm03ev01
RC=0
wait "$WPID" || RC=$?
RC2=0
(eval "$(cat "$T/out/wwok.out")" && export CROSS_CHECK_TOKEN && "$BIN" --start --job "$CROSS_CHECK_JOB" &&
	"$BIN" --finish --job "$CROSS_CHECK_JOB" --result done) >"$T/out/start_wwok.txt" 2>&1 || RC2=$?
sleep 4.5 # 대기 제한(4초)보다 오래
ok=1
[ "$RC" -eq 0 ] && [ "$RC2" -eq 0 ] || ok=0
job_row alice "$JWO" | grep -qx '  상태      : 승인/작업완료' || ok=0
! job_row alice "$JWO" | grep -q '대기 시간 초과' || ok=0
if [ $ok = 1 ]; then pass "제한 안에 승인된 작업은 대기 제한(4초)이 지나도 그대로(승인 → 작업중 → 승인/작업완료)"; else
	fail "승인된 작업 영향 (run=$RC start/finish=$RC2)" "$T/out/approve_wwok.txt" "$T/out/wwok.err" "$T/out/start_wwok.txt"
	job_row alice "$JWO"
fi

# 잘못된 값(시험 최소 2초 미만·범위 밖)은 기존(4초) 유지, 그 뒤 600 으로 복귀
ok=1
for bad in 1 86401 abc; do
	conf_wait "$bad"
	kill -HUP "$DPID"
	sleep 0.4
	wait_is 4초 4 || ok=0
done
grep -q '승인 대기 제한 4초 유지' "$T/daemon.log" || ok=0
conf_wait 600
kill -HUP "$DPID"
wait_is 10분 600 || ok=0
kill -0 "$DPID" 2>/dev/null || ok=0
if [ $ok = 1 ]; then pass "SIGHUP: 잘못된 wait_timeout_sec(1/86401/abc)는 기존 값 유지+로그, 다시 600초로 복귀"; else fail "wait_timeout_sec 잘못된 값" "$T/daemon.log"; fi

# 범위 검증: 최소 30 · 최대 86400 (시험용 최소값 변수를 지우고 실제 규칙으로). 데몬은 뜨지 않고 exit 3
ok=1
for v in 29 86401 0 abc; do
	printf 'target_dir=%s\nusers=alice,bob\nwait_timeout_sec=%s\n' "$T/targets" "$v" >"$ttl_conf"
	RC=0
	env -u CROSS_CHECK_TEST_MIN_WAIT_TIMEOUT_SEC CROSS_CHECK_CONF="$ttl_conf" CROSS_CHECK_SOCKET="$T/run2/cc.sock" CROSS_CHECK_LOCK="$T/run2/daemon.lock" \
		CROSS_CHECK_STATE="$T/lib2/jobs.json" CROSS_CHECK_LOG="$T/daemon2.log" "$BIN" --daemon >"$T/out/badwait.txt" 2>&1 || RC=$?
	if [ "$RC" -ne 3 ] || ! grep -q 'wait_timeout_sec 는 30~86400 사이의 정수' "$T/out/badwait.txt" || [ -S "$T/run2/cc.sock" ]; then
		ok=0
		echo "  (wait_timeout_sec=$v 가 거부되지 않음 rc=$RC)"
		cat "$T/out/badwait.txt"
	fi
done
if [ $ok = 1 ]; then pass "wait_timeout_sec 29·86401·0·abc 는 데몬 기동 거부(exit 3, '30~86400 사이의 정수')"; else fail "wait_timeout_sec 범위 검증"; fi

# ---- 8. 데몬 강제 종료 → 동시 --ensure 3개 → 하나만 기동, 재시작 처리 ----
# 작업중인 작업 하나(J8: 시작 직후 데몬이 죽음 → 재시작 때 승인/작업중단). 작업 제한(3초)·승인 유효시간(3초) 안에 데몬을 죽인다
start_worker w8 "$T/t_alice3.txt"
J8=$JOB
start_worker w6 "$T/t_alice3.txt"
J6=$JOB
W6=$WPID
approve_as_bob "$J8" "$T/out/approve8.txt" vm03ev01
RC8=0
wait_for "$T/out/w8.out" 'CROSS_CHECK_TOKEN=' 50 || RC8=9
(eval "$(cat "$T/out/w8.out")" && export CROSS_CHECK_TOKEN && "$BIN" --start --job "$CROSS_CHECK_JOB") >"$T/out/start8.txt" 2>&1 || RC8=$?
approve_as_bob "$J6" "$T/out/approve6.txt" vm03ev01
RC=0
wait "$W6" || RC=$?
for p in $(daemon_pids); do kill -9 "$p" 2>/dev/null || true; done
wait "$DPID" 2>/dev/null || true
DPID=""
r1=0 r2=0 r3=0
"$BIN" --ensure >"$T/out/ens1.txt" 2>&1 &
e1=$!
"$BIN" --ensure >"$T/out/ens2.txt" 2>&1 &
e2=$!
"$BIN" --ensure >"$T/out/ens3.txt" 2>&1 &
e3=$!
wait "$e1" || r1=$?
wait "$e2" || r2=$?
wait "$e3" || r3=$?
sleep 0.5
cnt=0
for p in $(pgrep -f -- "$BIN --daemon" || true); do
	if tr '\0' '\n' <"/proc/$p/environ" 2>/dev/null | grep -qx "CROSS_CHECK_SOCKET=$CROSS_CHECK_SOCKET"; then cnt=$((cnt + 1)); fi
done
if [ "$RC" -eq 0 ] && [ "$r1$r2$r3" = 000 ] && [ "$cnt" -eq 1 ] && "$BIN" --ensure; then
	pass "데몬 kill -9 후 동시 --ensure 3개 → 모두 exit 0, 데몬은 1개"
else
	fail "--ensure 재기동 (run=$RC ensure=$r1$r2$r3 daemons=$cnt)" "$T/out/ens1.txt" "$T/daemon.log"
fi
if job_row alice "$J6" | grep -q '데몬 재시작' && job_row alice "$J6" | grep -qx '  상태      : 중단' &&
	job_row alice "$J1" | grep -qx '  상태      : 승인/작업완료' && [ "$RC8" -eq 0 ] &&
	job_row alice "$J8" | grep -qx '  상태      : 승인/작업중단' && job_row alice "$J8" | grep -q '데몬 재시작'; then
	pass "재시작: 미시작 승인 건은 중단(데몬 재시작), 작업중이던 건은 승인/작업중단(데몬 재시작), 끝난 건(작업완료)은 그대로"
else
	fail "재시작 처리 (rc8=$RC8)" "$T/daemon.log" "$T/out/start8.txt"
	job_row alice "$J6"
	job_row alice "$J1"
	job_row alice "$J8"
fi

echo
echo "== 결과: PASS $PASS, FAIL $FAIL"
if [ "$FAIL" -ne 0 ]; then
	echo "== 데몬 로그(마지막 40줄)"
	tail -40 "$T/daemon.log" 2>/dev/null || true
	exit 1
fi
exit 0
