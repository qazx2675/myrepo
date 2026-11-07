#!/usr/bin/env python3
# func_check.py - 랩(.58)에서 install_mn 의 기능·TUI 를 실제 프로세스로 확인한다 (계획서 7.1~7.3)
#   사용법: python3 func_check.py <install_mn 바이너리> <스텁 gossh> <작업폴더>      (root 로 실행)
#   - 사용법 출력·종료코드 2
#   - 상태 7가지: 설치중·지연(delay_min=1)·디스크미인식·설치완료·ping X·완료후꺼짐·재부팅됨
#     (완료후꺼짐·재부팅됨은 lo 에 붙인 가짜 주소 172.31.99.1 을 뺐다 붙이며 만든다. 끝나면 지운다)
#   - 키 c r d l / 숫자 q, 창 크기 변경(SIGWINCH), q·Ctrl+C·SIGTERM 종료 뒤 터미널(termios) 복구
# 화면은 마지막 프레임(\x1b[H 이후)에서 이스케이프를 지운 글자로 본다. 결과는 PASS/FAIL 줄로 출력한다.
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
FAKE = "172.31.99.1"
results = []


def check(name, ok, detail=""):
    results.append(ok)
    print("%s %s%s" % ("PASS" if ok else "FAIL", name, (" -- " + detail) if detail else ""))
    sys.stdout.flush()


ANSI = re.compile(rb"\x1b\[[0-9;?]*[A-Za-z]")


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
                self.pump(0.2)
                return True
        return False

    def tty_cooked(self):
        a = termios.tcgetattr(self.fd)
        return bool(a[3] & termios.ICANON) and bool(a[3] & termios.ECHO)

    def kill(self):
        try:
            os.kill(self.pid, 9)
            os.waitpid(self.pid, 0)
        except OSError:
            pass


def sh(cmd):
    return subprocess.call(cmd, shell=True, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)


def main():
    os.makedirs(WD, exist_ok=True)
    os.chdir(WD)
    # ---- 사용법 ----
    r = subprocess.run([BIN], stdout=subprocess.PIPE, stderr=subprocess.PIPE)
    check("2 사용법: 인자 없음 -> 종료코드 2 + 사용법 출력", r.returncode == 2 and "사용법" in r.stderr.decode(), "code=%d" % r.returncode)
    r = subprocess.run([BIN, "/no/such/file"], stdout=subprocess.PIPE, stderr=subprocess.PIPE)
    check("2 사용법: 없는 파일 -> 종료코드 2", r.returncode == 2 and "사용법" in r.stderr.decode(), "code=%d" % r.returncode)
    r = subprocess.run([BIN, "-x", "a"], stdout=subprocess.PIPE, stderr=subprocess.PIPE)
    check("2 사용법: 모르는 옵션 -> 종료코드 2", r.returncode == 2, "code=%d" % r.returncode)

    # ---- 준비 ----
    hosts = ["127.2.1.1", "127.2.1.2", "127.2.2.1", "127.2.2.2", "127.2.3.1", "192.0.2.1", FAKE]
    open("fhosts.txt", "w").write("\n".join(hosts) + "\n")
    open("fstate.txt", "w").write(
        "127.2.1.1 installing\n127.2.1.2 disk\n127.2.2.1 installing\n127.2.2.2 done\n127.2.3.1 done\n%s done\n" % FAKE)
    open("ldap_check.sh", "w").write("#!/bin/bash\n")
    open("fpm.conf", "w").write(
        "gossh=%s\ninterval_sec=10\ndelay_min=1\nping_timeout_ms=800\nldap_check_sh=%s/ldap_check.sh\n"
        "vendor=^127\\.2\\.1\\.=D\nvendor=^127\\.2\\.2\\.=sds\nvendor_other=기타\n" % (STUB, WD))
    for f in ("fhosts.txt.pm_state.json",):
        if os.path.exists(f):
            os.remove(f)
    sh("ip addr del %s/32 dev lo" % FAKE)
    sh("ip addr add %s/32 dev lo" % FAKE)
    env = {"STUB_GOSSH_STATE": WD + "/fstate.txt"}
    s = Session(["-c", "fpm.conf", "fhosts.txt"], env=env)
    try:
        ok = s.wait_for(r"회차 1 · 대기중", 40)
        f = s.frame()
        check("1 첫 화면 회차 1 완료", ok, f.split("\n")[3] if ok else f[:200])
        check("1 상태: 설치중", bool(re.search(r"1 설치중 2", f)), re.search(r"1 설치중 \d+", f) and re.search(r"1 설치중 \d+", f).group(0) or "")
        check("1 상태: 디스크미인식", bool(re.search(r"3 디스크미인식 1", f)))
        check("1 상태: 설치완료(2대 + 가짜주소 = 3)", bool(re.search(r"4 설치완료 3", f)), re.search(r"4 설치완료 \d+", f) and re.search(r"4 설치완료 \d+", f).group(0) or "")
        check("1 상태: ping X", bool(re.search(r"5 ping X 1", f)))
        # 표 내용
        check("1 표에 벤더·상태 열", "127.2.1.2" in f and "디스크미인식" in f and "sds" in f, "")
        # 스크롤·검색·필터
        s.send(b"\x1b[B\x1b[B\x1b[A")
        s.send(b"/")
        check("3 '/' 검색 입력 모드", "Enter 적용" in s.frame())
        s.send(b"127.2.2")
        s.send(b"\r")
        fr = s.frame()
        check("3 검색 적용: 127.2.2.x 만 보임", "127.2.2.1" in fr and "127.2.1.1" not in fr.split("---")[-1], "")
        s.send(b"/")
        for _ in range(8):
            s.send(b"\x7f", 0.1)
        s.send(b"\r")
        s.send(b"3")
        fr = s.frame()
        check("3 필터 3: 디스크미인식만", "127.2.1.2" in fr and "127.2.3.1" not in fr.split("---")[-1], "")
        s.send(b"0")
        check("3 필터 0: 해제", "127.2.3.1" in s.frame())
        # d, l
        s.send(b"d", 1.0)
        check("1 d: dhcp_server 미설정 표시", "dhcp_server 미설정" in s.frame())
        s.send(b"d")
        s.send(b"l")
        check("1 l: LDAP 점검 표(OK/ON)", s.wait_for(r"127\.2\.2\.2\s+OK\s+OK\s+OK\s+ON", 20), "")
        s.send(b"l")
        # 지연 (delay_min=1): 설치중으로 처음 본 뒤 60초
        t_wait = 80
        check("1 상태: 지연(delay_min=1, 최대 %ds 기다림)" % t_wait, s.wait_for(r"2 지연 [12]", t_wait), re.search(r"2 지연 \d+", s.frame()) and re.search(r"2 지연 \d+", s.frame()).group(0) or "")
        s.send(b"r", 0.8)
        fr = s.frame()
        check("1 r: 지연 점검요청 코멘트(벤더 D/sds)", "OS 설치가 지연되고 있습니다" in fr and "점검부탁드립니다" in fr, "")
        s.send(b"r")
        s.send(b"c", 1.0)
        s.wait_for(r"설치현황", 15)
        fr = s.frame()
        check("1 c: 설치현황 코멘트 + 디스크 미인식 벤더 블록", "설치현황" in fr and "디스크 미인식" in fr, "")
        s.send(b"c")
        # 완료후꺼짐: 가짜 주소 제거 후 c
        sh("ip addr del %s/32 dev lo" % FAKE)
        s.send(b"c", 1.0)
        s.wait_for(r"완료후꺼짐 1대", 20)
        fr = s.frame()
        check("1 상태: 완료후꺼짐(c 로 판정)", "완료후꺼짐 1대" in fr and bool(re.search(r"7 완료후꺼짐 1", fr)), "")
        s.send(b"c")
        # 재부팅됨: 주소 복구 후 c
        sh("ip addr add %s/32 dev lo" % FAKE)
        s.send(b"c", 1.0)
        s.wait_for(r"재부팅됨 1대", 20)
        fr = s.frame()
        check("1 상태: 재부팅됨(c 로 판정, 완료 수에 포함)", "재부팅됨 1대" in fr and bool(re.search(r"6 재부팅됨 1", fr)), "")
        s.send(b"c")
        # 창 크기 변경
        fcntl.ioctl(s.fd, termios.TIOCSWINSZ, struct.pack("HHHH", 30, 80, 0, 0))
        s.buf = b""
        os.kill(s.pid, signal.SIGWINCH)
        s.pump(1.0)
        lines = s.frame().split("\n")
        wide = max(len(l.encode("utf-8").decode("utf-8")) for l in lines)  # 글자 수(한글은 폭 2)
        check("3 창 크기 변경(SIGWINCH): 80x30 으로 다시 그림", len(lines) <= 30 and len(lines) >= 20 and s.buf.count(b"\x1b[2J") >= 1, "줄 수 %d" % len(lines))
        # 정상 종료 q
        s.send(b"q", 0.5)
        ex = s.wait_exit()
        check("3 q 종료: 코드 0, 대체화면 복구 시퀀스, 터미널 cooked 복구", ex and s.code == 0 and b"\x1b[?1049l" in s.buf and s.tty_cooked(), "code=%s" % s.code)
    finally:
        s.kill()
        sh("ip addr del %s/32 dev lo" % FAKE)

    # ---- Ctrl+C / SIGTERM / SIGHUP ----
    for name, how in (("Ctrl+C", b"\x03"), ("SIGTERM", signal.SIGTERM), ("SIGHUP", signal.SIGHUP)):
        s = Session(["-c", "fpm.conf", "fhosts.txt"], env=env)
        try:
            s.wait_for(r"회차 1", 20)
            if isinstance(how, bytes):
                s.send(how, 0.3)
            else:
                os.kill(s.pid, how)
            ex = s.wait_exit()
            check("3 %s 종료: 터미널 복구" % name, ex and s.code == 0 and b"\x1b[?1049l" in s.buf and b"\x1b[?25h" in s.buf and s.tty_cooked(), "code=%s" % s.code)
        finally:
            s.kill()

    # ---- state 유지: 다시 실행하면 이력이 남는다 ----
    s = Session(["-c", "fpm.conf", "fhosts.txt"], env=env)
    try:
        s.pump(0.5)
        check("4 다시 실행해도 state.json 이력 유지(첫 화면부터 설치완료 표시)", bool(re.search(r"4 설치완료 [1-9]", s.frame())), re.search(r"4 설치완료 \d+", s.frame()) and re.search(r"4 설치완료 \d+", s.frame()).group(0) or "")
        s.send(b"q", 0.3)
        s.wait_exit()
    finally:
        s.kill()
    for f in ("fhosts.txt.pm_state.json",):
        if os.path.exists(f):
            os.remove(f)
    n = len([x for x in results if not x])
    print("총 %d 항목 중 FAIL %d" % (len(results), n))
    return 1 if n else 0


if __name__ == "__main__":
    sys.exit(main())
