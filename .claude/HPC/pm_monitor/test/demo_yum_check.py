#!/usr/bin/env python3
# demo_yum_check.py - demo/demo.sh 를 가상 터미널(120x40)에서 실제로 돌려 v0.4.0 yum 시나리오와 정리(cleanup)를 확인한다 (랩 .58, root)
#   사용법: python3 demo_yum_check.py <demo.sh 경로> [-n 대수=27]       (소요: 대수 27 약 4분, 8000 약 6분)
#   안내 화면 -> Enter -> 첫 화면(접속가능 기준 커널) -> y 표 -> (시작 100초 뒤) r 의 yum 지연 블록 3종·y 표의 새 커널·재부팅됨 ->
#   8·9 필터 -> q(화면만 나감, 수집 계속) -> 다시 붙기(Enter) -> e(수집 종료·정리) 순으로 가며 PASS/FAIL 을 출력한다.
#   끝난 뒤 데몬·/tmp/<이름>·/etc/hosts 표시·lo 주소가 남지 않았는지도 본다. 파이썬 3.6 호환.
import fcntl
import os
import pty
import re
import select
import struct
import subprocess
import sys
import termios
import time

demo = os.path.abspath(sys.argv[1])
n = int(sys.argv[sys.argv.index("-n") + 1]) if "-n" in sys.argv else 27
NAME = "pm_yumchk"
WORK = "/tmp/" + NAME
TOP = os.path.dirname(os.path.dirname(demo))
BIN = TOP + "/install_mn_os8"
ANSI = re.compile(rb"\x1b\[[0-9;?]*[A-Za-z]")
results = []
want_yum = 6 if n == 27 else max(6, n * 5 // 100)
YSW = 90 if n <= 1000 else 240  # demo.sh 의 yum 전환 시각(초)과 같아야 한다


def check(name, ok, detail=""):
    results.append(ok)
    print("%s %s%s" % ("PASS" if ok else "FAIL", name, (" -- " + detail) if detail else ""))
    sys.stdout.flush()


def sh(cmd):
    return subprocess.run(cmd, shell=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE).stdout.decode("utf-8", "replace")


class P(object):
    def __init__(self, cmd):
        self.pid, self.fd = pty.fork()
        if self.pid == 0:
            os.environ["TERM"] = "xterm"
            os.execvp(cmd[0], cmd)
        fcntl.ioctl(self.fd, termios.TIOCSWINSZ, struct.pack("HHHH", 40, 120, 0, 0))
        self.buf = b""
        self.code = None
        self.union = ""  # 패널을 넘기며 본 화면 글자를 모두 모은다

    def pump(self, wait=0.3):
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

    def text(self):
        return ANSI.sub(b"", self.buf).decode("utf-8", "replace").replace("\r\n", "\n")

    def frame(self):
        i = self.buf.rfind(b"\x1b[H")
        f = self.buf[i + 3:] if i >= 0 else self.buf
        return ANSI.sub(b"", f).decode("utf-8", "replace").replace("\r\n", "\n")

    def send(self, data, wait=0.5):
        os.write(self.fd, data)
        self.pump(wait)

    def wait_for(self, pat, timeout=60, frame=True):
        end = time.time() + timeout
        while time.time() < end:
            self.pump(0.3)
            if re.search(pat, self.frame() if frame else self.text()):
                return True
        return False

    def scan(self, key, pages, pat_list):
        """패널을 한 쪽씩 넘기며(PgDn) pat_list 의 글자가 모두 나오는지 본다. 찾은 것 집합을 돌려준다."""
        found = set()
        for _ in range(pages):
            fr = self.frame()
            for p in pat_list:
                if p in fr:
                    found.add(p)
            if len(found) == len(pat_list):
                break
            self.send(b"\x1b[6~", 0.25)
        return found

    def wait_exit(self, timeout=20):
        end = time.time() + timeout
        while time.time() < end:
            self.pump(0.2)
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


def daemon_here():
    return "pid " in sh("cd %s 2>/dev/null && %s --status hosts.txt" % (WORK, BIN))


def main():
    lo_before = sorted(sh("ip -o addr show dev lo").split("\n"))
    hosts_before = sh("grep -c '# %s ' /etc/hosts" % NAME).strip()
    t_start = time.time()
    p = P(["bash", demo, "-n", str(n), "-name", NAME])
    try:
        ok = p.wait_for(r"Enter 를 누르면 시작합니다", 90, frame=False)
        g = p.text()
        check("1 안내 화면: yum update 서버 시간표·키(y)·필터 8·9 안내", ok and "yum update 서버" in g and "y  yum 커널 표" in g and "8 접속가능" in g and re.search(r"yum %d대" % want_yum, g) is not None, "구성: " + (re.search(r"구성: .*", g).group(0) if re.search(r"구성: .*", g) else "?"))
        p.send(b"\n", 0.2)
        t0 = time.time()  # 시나리오 시계 시작(Enter)
        # 큰 데모는 첫 회차가 간격보다 길어 '대기중' 이 순간이다 -> 기준 커널을 기록한 yum 서버가 90% 넘게 접속가능으로 보일 때까지 기다린다
        ok = p.wait_for(r"8 접속가능 (\d+)", 240)
        for _ in range(100):
            m = re.search(r"8 접속가능 (\d+)", p.frame())
            if m and int(m.group(1)) >= want_yum * 0.9:
                break
            p.pump(1.0)
        fr = p.frame()
        m = re.search(r"8 접속가능 (\d+)", fr)
        check("2 첫 화면: 머리줄에 yum %d대·접속가능이 yum 서버 수의 90%% 이상(기준 커널 기록, 큰 데모는 몇 회차 걸림, %.0f초)" % (want_yum, time.time() - t0),
              ok and m is not None and int(m.group(1)) >= want_yum * 0.9 and ("(yum %d대)" % want_yum) in fr, fr[:200].replace("\n", " | "))
        sc = sh("cat %s/scenario.log" % WORK)
        check("2 시나리오 로그에 yum 계획(새 커널·같은 커널·ping 끊김·그대로)", "yum 서버 %d대" % want_yum in sc and "새 커널" in sc, sc.strip().split("\n")[-1][:150])
        st = sh("cat %s/state | grep -c '^[^ ]* done 4.18.0-513.el8.x86_64 2026-11-17 09:00:00 4.18.0-553' ; true" % WORK).strip()
        check("2 스텁 상태에 새 커널 전환 칸이 덧붙음(전환 epoch)", st.isdigit() and int(st) >= 1, "새 커널 %s대" % st)

        # ---- y 표(처음: 전부 접속가능) ----
        p.send(b"y", 1.0)
        fr = p.frame()
        rows = len(re.findall(r"\S+\s+\S+\s+접속가능\s+4\.18\.0-513\S*\s+4\.18\.0-513", fr))
        check("3 y: 표 제목·열 머리·접속가능 행(기준=현재 커널)", "[y] yum 커널" in fr and "hostname" in fr and rows >= min(want_yum, 20), "행 %d" % rows)
        p.send(b"y", 0.5)
        check("3 y 다시: 닫힘", "[y] yum 커널" not in p.frame())

        # ---- 90초 뒤 상태 변화 + yum 지연(1분) ----
        left = YSW + (15 if n <= 1000 else 70) - (time.time() - t0)  # 큰 데모는 한 회차가 30초 넘게 걸려 변화가 화면에 오기까지 더 걸린다
        if left > 0:
            p.pump(left)
        p.wait_for(r"9 yum_update/접속가능 [1-9]", 90)
        fr = p.frame()
        m9 = re.search(r"9 yum_update/접속가능 (\d+)", fr)
        m6 = re.search(r"6 재부팅됨 (\d+)", fr)
        m8 = re.search(r"8 접속가능 (\d+)", fr)
        c9, c6, c8 = (int(m.group(1)) if m else 0 for m in (m9, m6, m8))
        check("4 90초 뒤: 새 커널 재부팅 서버가 yum_update/접속가능, 같은 커널·ping 끊김은 재부팅됨, 나머지는 접속가능",
              c9 >= 1 and c6 >= 2 and c8 >= 1 and c9 + c6 + c8 >= want_yum * 0.95, "9=%d 6=%d 8=%d (yum %d대)" % (c9, c6, c8, want_yum))
        p.send(b"y", 1.0)
        fr = p.frame()
        found = set()
        want = ["yum_update/접속가능", "재부팅 중", "커널 동일", "4.18.0-553.el8"]
        for _ in range(40 if n > 27 else 1):
            fr = p.frame()
            for w in want:
                if w in fr:
                    found.add(w)
            if len(found) == len(want) or n == 27:
                break
            p.send(b"\x1b[6~", 0.2)
        check("4 y 표: 새 커널·재부팅 중(ping 끊김)·커널 동일 행이 보임", len(found) == len(want), "찾음 %s" % sorted(found))
        p.send(b"q", 0.5)

        # ---- r: yum 지연 블록 3종 ----
        p.send(b"r", 3.0)
        pats = ["재부팅이 확인되지 않고 있습니다", "재부팅은 되었으나 커널이 변경되지 않았습니다", "재부팅한 뒤 접속되지 않고 있습니다"]
        found = p.scan(b"r", 80 if n > 27 else 6, pats)
        check("5 r: yum 지연 블록 3종(재부팅 미확인·커널 동일·재부팅 뒤 접속 안 됨)", len(found) == 3, "찾음 %d/3" % len(found))
        p.send(b"q", 0.5)

        # ---- 8·9 필터 ----
        p.send(b"9", 0.7)
        fr = p.frame()
        rows = fr.split("호스트명")[-1]
        check("6 필터 9: yum_update/접속가능 서버만 나옴", "yum_update/접속가능" in rows and "재부팅됨" not in rows and "OS 설치중" not in rows, (re.search(r"필터: \S+", fr) or re.search(r"$", fr)).group(0))
        p.send(b"8", 0.7)
        fr = p.frame()
        rows = fr.split("호스트명")[-1]
        check("6 필터 8: 접속가능 서버만 나옴(yum_update 완료·재부팅됨 없음)", "접속가능" in rows and "yum_update/접속가능" not in rows and "재부팅됨" not in rows, (re.search(r"필터: \S+", fr) or re.search(r"$", fr)).group(0))
        p.send(b"0", 0.5)

        # ---- q: 화면만 나감(수집 계속), 다시 붙기, e: 정리 ----
        p.send(b"q", 0.5)
        ok = p.wait_for(r"Enter = 다시 붙기", 20, frame=False)
        check("7 q: 화면만 닫히고 데모는 '다시 붙기/e' 안내", ok and "백그라운드에서 계속" in p.text(), "")
        check("7 q 뒤에도 수집 데몬이 살아 있음(데모 폴더 기준 --status)", daemon_here(), "")
        p.buf = b""
        p.send(b"\n", 0.5)
        ok = p.wait_for(r"회차 \d+ ", 60)
        fr = p.frame()
        mr = re.search(r"회차 (\d+) ", fr)
        check("7 다시 붙기: 같은 수집에 이어 붙음(회차 2 이상, yum 상태 유지)", ok and mr is not None and int(mr.group(1)) >= 2 and ("(yum %d대)" % want_yum) in fr and "9 yum_update/접속가능" in fr, "회차 %s" % (mr.group(1) if mr else "?"))
        p.send(b"q", 0.5)
        ok = p.wait_for(r"Enter = 다시 붙기", 20, frame=False)
        p.send(b"e\n", 0.5)
        ex = p.wait_exit(40)
        tx = p.text()
        check("8 e: 수집 종료·정리 후 종료코드 0", ok and ex and p.code == 0 and "정리했습니다" in tx, "code=%s" % p.code)
    finally:
        p.kill()
    time.sleep(1.0)
    # ---- 정리 확인 ----
    check("9 정리: /tmp/%s 없음" % NAME, not os.path.exists(WORK), "")
    check("9 정리: /etc/hosts 에 데모 표시 없음", sh("grep -c '# %s ' /etc/hosts" % NAME).strip() == "0" or hosts_before != "0", sh("grep -n '# %s ' /etc/hosts" % NAME)[:100])
    lo_after = sorted(sh("ip -o addr show dev lo").split("\n"))
    check("9 정리: lo 주소가 시작 전과 같음(OFF·yum OFF 주소 제거)", lo_before == lo_after, str(set(lo_after) ^ set(lo_before))[:200])
    st = sh("%s --status" % BIN)
    check("9 정리: 이 데모의 수집 데몬 없음(--status 에 %s 없음)" % NAME, NAME not in st, st.replace("\n", " | ")[:200])
    procs = ""
    for _ in range(15):  # 끝나는 중인 자식 프로세스(gossh 스텁 등)를 잠깐 기다린다
        procs = sh("ls -l /proc/[0-9]*/cwd 2>/dev/null | grep %s" % WORK)
        if procs.strip() == "":
            break
        time.sleep(1)
    check("9 정리: 데모 폴더에서 도는 프로세스 없음", procs.strip() == "", procs[:100])
    n_fail = len([x for x in results if not x])
    print("총 %d 항목 중 FAIL %d (대수 %d, %.0f초)" % (len(results), n_fail, n, time.time() - t_start))
    return 1 if n_fail else 0


if __name__ == "__main__":
    sys.exit(main())
