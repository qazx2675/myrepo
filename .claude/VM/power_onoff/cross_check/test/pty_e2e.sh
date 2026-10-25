#!/usr/bin/env bash
# cross_check 전체 화면 TUI 를 실제 pty 에서 시험한다(Linux, python3 의 pty·termios 모듈 사용).
#
# 사용법: bash test/pty_e2e.sh <cross_check 바이너리 경로> [화면 기록을 남길 폴더]
#
# e2e.sh 처럼 임시 디렉터리 하나 안에서 CROSS_CHECK_* 환경변수로 데몬·설정·패스워드·기록을 따로 띄우므로 /etc, /var 는 건드리지 않는다.
# 시나리오: alice 가 --run 으로 대기 → 실제 pty 에서 cross_check --user bob(전체 화면)을 띄워 ↓↑ → (작업 전환) → Enter →
# 대상 붙여넣기(붙여넣기 구간 표시 ESC[200~ … ESC[201~ 안에 빈 줄·탭 섞음) → 빈 줄 Enter → 패스워드 → 결과 → q.
# 확인: 작업이 승인되고 alice 의 --run 이 0 으로 끝나는지, 화면에 패스워드·대상 목록(데몬이 준 것)이 없는지,
#       종료 뒤 pty 의 termios 가 시작 전과 같고 화면 복원 시퀀스로 끝나는지. 화면 기록(붙여넣은 뒤·승인 뒤)을 남긴다.
set -euo pipefail

BIN_ARG=${1:?사용법: bash test/pty_e2e.sh <cross_check 바이너리 경로> [기록 폴더]}
BIN=$(cd "$(dirname "$BIN_ARG")" && pwd)/$(basename "$BIN_ARG")
OUTDIR=${2:-}
command -v python3 >/dev/null 2>&1 || { echo "SKIP  python3 이 없어 pty 시험을 할 수 없음"; exit 0; }

T=$(mktemp -d /tmp/cc_pty.XXXXXX)
DPID=""
WPID=""
cleanup() {
	set +e
	[ -z "$WPID" ] || kill -9 "$WPID" 2>/dev/null
	[ -z "$DPID" ] || kill -TERM "$DPID" 2>/dev/null
	sleep 0.3
	[ -z "$DPID" ] || kill -9 "$DPID" 2>/dev/null
	rm -rf "$T"
}
trap cleanup EXIT

export CROSS_CHECK_SOCKET="$T/run/cc.sock" CROSS_CHECK_CONF="$T/etc/cross_check.conf" CROSS_CHECK_AUTH="$T/etc/auth.conf"
export CROSS_CHECK_STATE="$T/lib/jobs.json" CROSS_CHECK_LOCK="$T/run/daemon.lock" CROSS_CHECK_LOG="$T/daemon.log"
unset CROSS_CHECK_TOKEN CROSS_CHECK_COLOR NO_COLOR || true
mkdir -p "$T/run" "$T/etc" "$T/lib" "$T/targets"
printf 'target_dir=%s\nauth=required\nusers=alice,bob\napproval_ttl_sec=120\n' "$T/targets" >"$CROSS_CHECK_CONF"
(umask 077; printf 'admin=adm_pw_1\nalice=alice123\nbob=bob12345\n' >"$CROSS_CHECK_AUTH")
printf 'svr1ev01\nsvr1ev02\nsvr2ev01\n' >"$T/targets/on_off_alice.txt"

"$BIN" --daemon >"$T/daemon.out" 2>&1 &
DPID=$!
for _ in $(seq 50); do [ -S "$CROSS_CHECK_SOCKET" ] && break; sleep 0.1; done
[ -S "$CROSS_CHECK_SOCKET" ] || { echo "FAIL  데몬 기동"; cat "$T/daemon.out"; exit 1; }

"$BIN" --run --user alice --checker bob --targets-file "$T/targets/on_off_alice.txt" >"$T/w.out" 2>"$T/w.err" &
WPID=$!
JOB=""
for _ in $(seq 50); do
	JOB=$(sed -n 's/^\[대기\] 작업 \([^ ]*\) .*/\1/p' "$T/w.err" | head -1)
	[ -n "$JOB" ] && break
	sleep 0.1
done
[ -n "$JOB" ] || { echo "FAIL  작업 등록"; cat "$T/w.err"; exit 1; }
echo "== pty 시험: 작업 $JOB (alice → bob), 바이너리 $BIN"

RC=0
python3 - "$BIN" "$T" "$JOB" <<'PY' || RC=$?
import os, sys, pty, termios, fcntl, struct, select, time, subprocess, re

binp, tdir, job = sys.argv[1], sys.argv[2], sys.argv[3]
master, slave = pty.openpty()
fcntl.ioctl(slave, termios.TIOCSWINSZ, struct.pack("HHHH", 30, 100, 0, 0))  # 30줄 100칸
before = termios.tcgetattr(slave)

def child_setup():
    os.setsid()
    fcntl.ioctl(0, termios.TIOCSCTTY, 0)

env = dict(os.environ, TERM="xterm-256color", LANG="ko_KR.UTF-8")
p = subprocess.Popen([binp, "--user", "bob"], stdin=slave, stdout=slave, stderr=slave, env=env,
                     preexec_fn=child_setup, close_fds=True)
buf = b""

def pump(t=0.2):
    global buf
    end = time.time() + t
    while True:
        left = end - time.time()
        if left <= 0:
            return
        r, _, _ = select.select([master], [], [], left)
        if not r:
            return
        try:
            d = os.read(master, 65536)
        except OSError:
            return
        if not d:
            return
        buf += d

def screen():
    """마지막 프레임(ESC[H 부터)을 글자만 남긴 줄들로"""
    s = buf.decode("utf-8", "replace")
    i = s.rfind("\x1b[H")
    s = s[i:] if i >= 0 else s
    s = re.sub(r"\x1b\[[0-9;?]*[A-Za-z~]", "", s)
    return "\n".join(l.rstrip() for l in s.replace("\r", "").split("\n"))

def wait_for(pat, t=5.0):
    end = time.time() + t
    while time.time() < end:
        pump(0.1)
        if pat in screen():
            return True
    print("FAIL  화면에 %r 가 나오지 않음. 마지막 화면:\n%s" % (pat, screen()))
    p.kill()
    sys.exit(1)

def send(b, pause=0.15):
    os.write(master, b)
    pump(pause)

fails = []
def check(cond, name):
    print(("PASS  " if cond else "FAIL  ") + name)
    if not cond:
        fails.append(name)

wait_for("cross_check 상태 리포트")
wait_for("● 데몬 실행 (pid ")
check(buf.startswith(b"\x1b[?1049h\x1b[?25l\x1b[?2004h"), "대체 화면·커서 숨김·붙여넣기 구간 표시로 시작")
check("(user: bob)" in screen() and "↑↓ 이동  ←→ 작업 전환" in screen(), "머리말 (user: bob)·키 안내 줄")
cur = termios.tcgetattr(slave)
check((cur[3] & termios.ICANON) == 0 and (cur[3] & termios.ECHO) == 0 and (cur[3] & termios.ISIG) == 0, "실행 중 raw 모드(ICANON·ECHO·ISIG 꺼짐)")
send(b"\x1b[B"); send(b"\x1b[A")                # ↓↑ (목록 이동)
send(b"\x1b[C")                                 # → 내가 확인자인 작업
wait_for("[내가 확인자인 작업 1]")
check(job in screen() and "pty_e2e.sh" in screen(), "확인자 구역에 작업 %s (호출 스크립트 pty_e2e.sh)" % job)
check("svr1ev01" not in screen(), "확인자 목록에 대상 이름 없음")
send(b"\r")                                     # Enter → 승인 1/3
wait_for("1/3 대상 붙여넣기")
# 붙여넣기(ESC[200~ … ESC[201~): 빈 줄·탭·공백이 섞여도 붙여넣은 빈 줄로는 끝나지 않는다
send(b"\x1b[200~svr1ev02\r\n\r\nsvr2ev01\tsvr1ev01 \r\n\x1b[201~", 0.4)
wait_for("인식한 이름: 3개")
after_paste = screen()
check("1/3" in after_paste and "일치" not in after_paste.split("인식한 이름")[1].split("\n")[0], "붙여넣은 뒤에도 1/3 단계, 이름 수만 표시(판정 없음)")
send(b"\r")                                     # 빈 줄 Enter = 붙여넣기 끝
wait_for("2/3 패스워드")
send(b"bob1234")
wait_for("패스워드 > *******_")
send(b"5\r", 0.5)                               # 패스워드 bob12345 + Enter
wait_for("3/3 결과")
wait_for("[승인 완료] 작업 " + job)
after_approve = screen()
send(b"\r")                                     # 목록으로
wait_for("승인")
send(b"\x03")                                   # Ctrl+C: 끝나지 않고 안내만
wait_for("종료는 q 를 누르세요")
check(p.poll() is None, "Ctrl+C 로는 끝나지 않음")
send(b"q", 0.5)
try:
    code = p.wait(5)
except subprocess.TimeoutExpired:
    p.kill()
    code = -1
pump(0.3)
check(code == 0, "q → 종료 코드 0 (실제 %r)" % code)
check(buf.endswith(b"\x1b[?2004l\x1b[?25h\x1b[?1049l"), "화면 복원 시퀀스로 끝남(붙여넣기 표시 끔·커서 보임·대체 화면 나감)")
after = termios.tcgetattr(slave)
check(after == before, "종료 뒤 termios 가 시작 전과 같음")
raw = buf.decode("utf-8", "replace")
check("bob12345" not in raw and "bob1234" not in raw, "패스워드가 화면 출력에 없음")
with open(os.path.join(tdir, "after_paste.txt"), "w") as f:
    f.write(after_paste + "\n")
with open(os.path.join(tdir, "after_approve.txt"), "w") as f:
    f.write(after_approve + "\n")
sys.exit(1 if fails else 0)
PY

ok=$RC
WRC=0
wait "$WPID" || WRC=$?
WPID=""
st=$(printf 'o %s\n\nq\n' "$JOB" | "$BIN" --user alice 2>&1 | sed -n 's/^  상태      : //p' | head -1)
if [ "$WRC" -eq 0 ] && [ "$st" = "승인" ] && grep -q "CROSS_CHECK_JOB=$JOB" "$T/w.out"; then
	echo "PASS  작업 $JOB 상태 = 승인, alice 의 --run 종료 코드 0 (JOB/TOKEN 출력)"
else
	echo "FAIL  최종 상태 (run=$WRC 상태=$st)"
	cat "$T/w.err"
	ok=1
fi
if [ -n "$OUTDIR" ]; then
	mkdir -p "$OUTDIR"
	cp "$T/after_paste.txt" "$OUTDIR/pty_after_paste.txt" 2>/dev/null || true
	cp "$T/after_approve.txt" "$OUTDIR/pty_after_approve.txt" 2>/dev/null || true
	echo "== 화면 기록: $OUTDIR/pty_after_paste.txt, $OUTDIR/pty_after_approve.txt"
fi
if [ "$ok" -ne 0 ]; then
	echo "== 결과: FAIL"
	exit 1
fi
echo "== 결과: PASS"
