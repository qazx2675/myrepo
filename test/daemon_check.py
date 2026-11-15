#!/usr/bin/env python3
# daemon_check.py - 랩(.58)에서 백그라운드 수집(화면 붙이기·떼기, 재접속, 12시간 자동 종료)을 실제 프로세스로 확인한다
#   사용법: python3 daemon_check.py <install_mn 바이너리> <스텁 gossh> <작업폴더>      (root 로 실행)
#   - q·SIGHUP·Ctrl+C 로 화면만 닫으면 수집(데몬)은 계속, 다시 실행하면 이어서 붙음(회차가 이어짐)
#   - 데몬은 터미널과 분리(세션 리더, tty 없음), 같은 호스트파일은 데몬 하나, 화면 둘이 동시에 붙을 수 있음
#   - Q 후 n 은 취소, Q 후 y 는 수집 종료(상태 기록은 남음), --stop·--status
#   - 호스트파일·설정이 바뀌면 다시 시작, 죽은 데몬(소켓만 남음) 뒤 재시작
#   - PM_MAX_RUN_SEC 로 짧게 줄인 시간이 지나면 데몬이 스스로 끝나고 붙은 화면도 닫힘, 기본 만료는 시작 + 12시간
# 결과는 PASS/FAIL 줄로 출력한다.
import fcntl
import os
import pty
import re
import select
import signal
import struct
import subprocess
import sys
import termios
import time

BIN, STUB, WD = os.path.abspath(sys.argv[1]), os.path.abspath(sys.argv[2]), os.path.abspath(sys.argv[3])
results = []
ANSI = re.compile(rb"\x1b\[[0-9;?]*[A-Za-z]")
HOSTS = "dhosts.txt"


def check(name, ok, detail=""):
    results.append(ok)
    print("%s %s%s" % ("PASS" if ok else "FAIL", name, (" -- " + detail) if detail else ""))
    sys.stdout.flush()


class Session(object):
    def __init__(self, args, rows=40, cols=120, env=None):
        e = dict(os.environ)
        e["TERM"] = "xterm"
        e.update(env or {})
        self.pid, self.fd = pty.fork()
        if self.pid == 0:
            os.environ.update(e)
            os.execv(BIN, [BIN] + args)
        fcntl.ioctl(self.fd, termios.TIOCSWINSZ, struct.pack("HHHH", rows, cols, 0, 0))
        self.buf = b""
        self.code = None

    def pump(self, wait=0.2):
        end = time.time() + wait
        while time.time() < end:
            r, _, _ = select.select([self.fd], [], [], 0.05)
            if r:
                try:
                    b = os.read(self.fd, 65536)
                except OSError:
                    return False
                if not b:
                    return False
                self.buf += b
        return True

    def frame(self):
        i = self.buf.rfind(b"\x1b[H")
        f = self.buf[i + 3:] if i >= 0 else self.buf
        f = ANSI.sub(b"", f)
        return f.decode("utf-8", "replace").replace("\r\n", "\n")

    def text(self):
        return ANSI.sub(b"", self.buf).decode("utf-8", "replace")

    def wait_for(self, pat, timeout=30):
        end = time.time() + timeout
        while time.time() < end:
            self.pump(0.2)
            if re.search(pat, self.frame()):
                return True
        return False

    def send(self, data, wait=0.4):
        os.write(self.fd, data)
        self.pump(wait)

    def wait_exit(self, timeout=10):
        end = time.time() + timeout
        while time.time() < end:
            self.pump(0.1)
            p, st = os.waitpid(self.pid, os.WNOHANG)
            if p:
                self.code = os.WEXITSTATUS(st) if os.WIFEXITED(st) else -os.WTERMSIG(st)
                self.pump(0.3)
                return True
        return False

    def kill(self):
        try:
            os.kill(self.pid, 9)
            os.waitpid(self.pid, 0)
        except OSError:
            pass


def run(args, env=None):
    e = dict(os.environ)
    e.update(env or {})
    return subprocess.run([BIN] + args, stdout=subprocess.PIPE, stderr=subprocess.PIPE, env=e, stdin=subprocess.DEVNULL)


def status_out():
    return run(["--status", HOSTS]).stdout.decode()


def daemon_pid():
    m = re.search(r"pid (\d+)", status_out())
    return int(m.group(1)) if m else None


def alive(pid):
    try:
        os.kill(pid, 0)
        return os.path.exists("/proc/%d" % pid) and open("/proc/%d/stat" % pid).read().split()[2] != "Z"
    except OSError:
        return False


def wait_dead(pid, timeout=15):
    end = time.time() + timeout
    while time.time() < end:
        if not alive(pid):
            return True
        time.sleep(0.2)
    return False


def round_of(s):
    m = re.search(r"회차 (\d+)", s.frame())
    return int(m.group(1)) if m else 0


def main():
    os.makedirs(WD, exist_ok=True)
    os.chdir(WD)
    hosts = ["127.3.1.%d" % i for i in range(1, 7)]
    open(HOSTS, "w").write("\n".join(hosts) + "\n")
    open("dstate.txt", "w").write("127.3.1.1 done\n127.3.1.2 done\n127.3.1.3 installing\n127.3.1.4 installing\n127.3.1.5 disk\n127.3.1.6 done\n")
    open("ldap_check.sh", "w").write("#!/bin/bash\n")
    conf = "gossh=%s\ninterval_sec=5\nping_timeout_ms=800\nldap_check_sh=%s/ldap_check.sh\n" % (STUB, WD)
    open("dpm.conf", "w").write(conf)
    for f in (HOSTS + ".pm_state.json",):
        if os.path.exists(f):
            os.remove(f)
    run(["--stop", HOSTS])
    env = {"STUB_GOSSH_STATE": WD + "/dstate.txt"}
    args = ["-c", "dpm.conf", HOSTS]

    # ---- 터미널이 아니면 데몬을 만들지 않는다 ----
    r = run(args, env)
    check("0 tty 가 아니면 안내하고 1 로 끝남, 데몬 안 만듦", r.returncode == 1 and "터미널" in r.stderr.decode() and daemon_pid() is None, "code=%d" % r.returncode)

    # ---- 붙기: 데몬 시작, 머리줄·푸터 ----
    s = Session(args, env=env)
    pid1 = None
    try:
        ok = s.wait_for(r"회차 1 · 대기중", 40)
        fr = s.frame()
        check("1 첫 화면: 회차 1 완료", ok, fr[:150])
        check("1 푸터: q 나가기(수집 계속) · Q 수집 종료", "q 나가기(수집 계속)" in fr and "Q 수집 종료" in fr)
        m = re.search(r"자동 종료 (\d\d):(\d\d)", fr)
        want = time.localtime(time.time() + 12 * 3600)
        okt = bool(m) and abs((int(m.group(1)) * 60 + int(m.group(2))) - (want.tm_hour * 60 + want.tm_min)) <= 2
        check("1 머리줄: 자동 종료 = 지금 + 12시간", okt, m.group(0) if m else "없음")
        pid1 = daemon_pid()
        check("1 --status: 데몬 pid·남은 시간 표시", pid1 is not None and "남은 11h" in status_out(), status_out().replace("\n", " | ")[:200])
        stat = open("/proc/%d/stat" % pid1).read().rsplit(")", 1)[1].split() if pid1 else []
        # stat 필드(")" 뒤): state ppid pgrp session tty_nr ...
        check("1 데몬은 터미널과 분리(세션 리더·tty 없음)", bool(stat) and stat[3] == str(pid1) and stat[4] == "0", " ".join(stat[:5]))
        check("1 설치완료 3대(상태 파일 이전에도 화면에 보임)", bool(re.search(r"4 설치완료 3", fr)))

        # ---- q: 화면만 닫힘 ----
        s.send(b"q", 0.4)
        ex = s.wait_exit()
        txt = s.text()
        check("2 q: 종료코드 0, '수집은 백그라운드에서 계속' 안내", ex and s.code == 0 and "백그라운드에서 계속" in txt and "--stop" in txt, "code=%s" % s.code)
        time.sleep(1.0)
        check("2 q 뒤에도 데몬이 살아 있음", pid1 is not None and alive(pid1))
    finally:
        s.kill()

    # ---- 재접속: 이어서 붙음(회차가 이어짐), 데몬은 그대로 ----
    time.sleep(7)  # 간격 5초 + 회차 시간
    s = Session(args, env=env)
    try:
        s.pump(1.0)
        r2 = round_of(s)
        check("3 다시 실행: 같은 데몬에 붙음(회차 2 이상 · pid 같음)", r2 >= 2 and daemon_pid() == pid1, "회차 %d pid %s/%s" % (r2, daemon_pid(), pid1))
        check("3 다시 실행: 첫 프레임부터 설치완료 3대", bool(re.search(r"4 설치완료 3", s.frame())))
        s.send(b"q", 0.3)
        s.wait_exit()
    finally:
        s.kill()

    # ---- 화면 둘 동시, SIGHUP·Ctrl+C 로 닫아도 수집 계속 ----
    a, b = Session(args, env=env), Session(args, env=env)
    try:
        a.pump(1.0)
        b.pump(1.0)
        ra0, rb0 = round_of(a), round_of(b)
        check("4 화면 두 개가 동시에 붙음", ra0 >= 1 and rb0 >= 1, "%d %d" % (ra0, rb0))
        ok = a.wait_for(r"회차 %d " % (ra0 + 1), 25) and b.wait_for(r"회차 %d " % (rb0 + 1), 25)
        check("4 두 화면 모두 새 회차를 받아 갱신", ok, "%d %d" % (round_of(a), round_of(b)))
        os.kill(a.pid, signal.SIGHUP)  # ssh 연결이 끊긴 것과 같다
        ea = a.wait_exit()
        b.send(b"\x03", 0.3)
        eb = b.wait_exit()
        check("4 SIGHUP·Ctrl+C: 화면만 종료(코드 0)", ea and eb and a.code == 0 and b.code == 0, "%s %s" % (a.code, b.code))
        time.sleep(0.8)
        check("4 두 화면이 모두 닫혀도 데몬 생존", alive(pid1))
    finally:
        a.kill()
        b.kill()

    # ---- Q 취소 / Q y 종료 ----
    s = Session(args, env=env)
    try:
        s.pump(1.0)
        s.send(b"Q", 0.4)
        check("5 Q: 종료 확인 안내", "수집을 완전히 종료할까요?" in s.frame(), "")
        s.send(b"n", 0.4)
        check("5 Q 후 n: 취소(화면·데몬 그대로)", "수집을 완전히 종료할까요?" not in s.frame() and alive(pid1), "")
        s.send(b"Q", 0.3)
        s.send(b"y", 0.5)
        ex = s.wait_exit(15)
        check("5 Q 후 y: '수집을 종료했습니다' 출력·코드 0", ex and s.code == 0 and "수집을 종료했습니다" in s.text(), "code=%s" % s.code)
        check("5 Q 후 y: 데몬 종료·소켓 정리", wait_dead(pid1) and "없습니다" in status_out() and not any(f.endswith(".sock") for f in os.listdir("/run/install_mn")), "")
        check("5 Q 후 y: 상태 파일은 남음", os.path.exists(HOSTS + ".pm_state.json"))
    finally:
        s.kill()

    # ---- 종료 뒤 다시 실행: 새 데몬, 기록은 이어짐 ----
    s = Session(args, env=env)
    try:
        s.pump(1.0)
        pid2 = daemon_pid()
        check("6 종료 뒤 다시 실행: 새 데몬 + 첫 프레임부터 기록(설치완료 3)", pid2 is not None and pid2 != pid1 and bool(re.search(r"4 설치완료 3", s.frame())), "pid %s" % pid2)
        s.send(b"q", 0.3)
        s.wait_exit()

        # ---- 호스트파일이 바뀌면 다시 시작 ----
        open(HOSTS, "a").write("127.3.1.7\n")
        s2 = Session(args, env=env)
        try:
            ok = s2.wait_for(r"전체 7대", 25)
            pid3 = daemon_pid()
            check("7 호스트파일 변경: 수집을 다시 시작(새 pid, 7대, 안내 문구)", ok and pid3 is not None and pid3 != pid2 and "다시 시작했습니다" in s2.frame(), "pid %s -> %s" % (pid2, pid3))
            s2.send(b"q", 0.3)
            s2.wait_exit()
        finally:
            s2.kill()

        # ---- 데몬이 죽은 뒤(소켓만 남음) 다시 시작 ----
        pid3 = daemon_pid()
        os.kill(pid3, signal.SIGKILL)
        wait_dead(pid3)
        stale = [f for f in os.listdir("/run/install_mn") if f.endswith(".sock")]
        s3 = Session(args, env=env)
        try:
            ok = s3.wait_for(r"회차 1 ", 40)
            pid4 = daemon_pid()
            check("8 데몬이 강제 종료돼 소켓이 남아도 다시 실행하면 새로 시작", ok and pid4 is not None and pid4 != pid3 and len(stale) >= 1, "pid %s -> %s, 남은 소켓 %d" % (pid3, pid4, len(stale)))
            s3.send(b"q", 0.3)
            s3.wait_exit()
        finally:
            s3.kill()
    finally:
        s.kill()
    run(["--stop", HOSTS])
    check("9 --stop: 실행 중이 아니어도 0", run(["--stop", HOSTS]).returncode == 0)

    # ---- 12시간 만료(여기서는 PM_MAX_RUN_SEC=10 으로 줄임) ----
    s = Session(args, env=dict(env, PM_MAX_RUN_SEC="10"))
    try:
        s.pump(1.0)
        pid5 = daemon_pid()
        check("10 만료 시험: 남은 시간이 1분 미만으로 표시", pid5 is not None and "남은 0m" in status_out(), status_out().replace("\n", " | ")[-80:])
        ex = s.wait_exit(25)
        check("10 만료되면 붙은 화면이 닫히고 안내(코드 0)", ex and s.code == 0 and "수집이 끝나 화면을 닫았습니다" in s.text(), "code=%s" % s.code)
        check("10 만료되면 데몬 종료·소켓 정리·상태 파일 유지", pid5 is not None and wait_dead(pid5, 5) and "없습니다" in status_out() and os.path.exists(HOSTS + ".pm_state.json"), "")
    finally:
        s.kill()
    # 만료 뒤 다시 실행하면 새 12시간으로 시작
    s = Session(args, env=env)
    try:
        s.pump(1.0)
        check("10 만료 뒤 다시 실행: 새 데몬이 새 12시간으로 시작", daemon_pid() is not None and "남은 11h" in status_out())
        s.send(b"Q", 0.3)
        s.send(b"y", 0.5)
        s.wait_exit(15)
    finally:
        s.kill()
    n = len([x for x in results if not x])
    print("총 %d 항목 중 FAIL %d" % (len(results), n))
    return 1 if n else 0


if __name__ == "__main__":
    sys.exit(main())
