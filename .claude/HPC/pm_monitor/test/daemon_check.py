#!/usr/bin/env python3
# daemon_check.py - 랩(.58)에서 백그라운드 수집(화면 붙이기·떼기, 재접속, 12시간 자동 종료)을 실제 프로세스로 확인한다
#   사용법: python3 daemon_check.py <install_mn 바이너리> <스텁 gossh> <작업폴더> [소스 폴더]      (root 로 실행)
#   - q·SIGHUP·Ctrl+C 로 화면만 닫으면 수집(데몬)은 계속, 다시 실행하면 이어서 붙음(회차가 이어짐)
#   - 데몬은 터미널과 분리(세션 리더, tty 없음), 같은 호스트파일은 데몬 하나, 화면 둘이 동시에 붙을 수 있음
#   - Q 후 n 은 취소, Q 후 y 는 수집 종료(상태 기록은 남음), --stop·--status
#   - 호스트파일·설정이 바뀌면 다시 시작, 죽은 데몬(소켓만 남음) 뒤 재시작
#   - PM_MAX_RUN_SEC 로 짧게 줄인 시간이 지나면 데몬이 스스로 끝나고 붙은 화면도 닫힘, 기본 만료는 시작 + 12시간
#   - (v0.4.0, 11번대) yum update 모드: 두 번째 칸이 yum 인 서버의 접속가능(기준 커널 기록) -> yum_update/접속가능(커널 변경) ·
#     재부팅됨(커널 동일) · ping 이 끊긴 서버의 재부팅됨, OS 서버와 한 호스트파일에서 같이 동작, y 패널, 다시 실행해도 기준 커널 유지,
#     바이너리(프로그램 버전)가 바뀌면 옛 데몬을 멈추고 새로 시작(기록 유지). 스텁 gossh 의 yum 시나리오(test/stub_gossh.sh 머리말)를 쓴다.
#     (h)용 두 번째 바이너리는 소스 폴더(기본: 이 파일의 상위 폴더, 4번째 인자로 바꿈)에서 -X main.programVersion=... 로 임시 빌드한다
#     (소스 폴더는 건드리지 않음. go 는 PATH 나 /opt/go1.20/bin).
# 결과는 PASS/FAIL 줄로 출력한다.
import fcntl
import os
import pty
import re
import select
import shutil
import signal
import struct
import subprocess
import sys
import termios
import time

BIN, STUB, WD = os.path.abspath(sys.argv[1]), os.path.abspath(sys.argv[2]), os.path.abspath(sys.argv[3])
SRC = os.path.abspath(sys.argv[4]) if len(sys.argv) > 4 else os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
results = []
ANSI = re.compile(rb"\x1b\[[0-9;?]*[A-Za-z]")
HOSTS = "dhosts.txt"


def check(name, ok, detail=""):
    results.append(ok)
    print("%s %s%s" % ("PASS" if ok else "FAIL", name, (" -- " + detail) if detail else ""))
    sys.stdout.flush()


class Session(object):
    def __init__(self, args, rows=40, cols=120, env=None, binary=None):
        e = dict(os.environ)
        e["TERM"] = "xterm"
        e.update(env or {})
        self.pid, self.fd = pty.fork()
        if self.pid == 0:
            os.environ.update(e)
            os.execv(binary or BIN, [binary or BIN] + args)
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


YHOSTS = "yhosts.txt"
YADDR = "172.31.79.14"  # lo 에 붙였다 떼는 주소(127.x 는 떼도 계속 응답하므로 ping 이 끊기는 서버용)
K0, B0 = "4.18.0-513.el8", "2026-11-17 09:00:00"
K1 = "4.18.0-553.el8"


def load_state(path=None):
    """state.json 을 {hostname: 레코드} 로 읽는다(없거나 쓰는 중이면 빈 사전)."""
    import json
    try:
        d = json.load(open(path or (HOSTS + ".pm_state.json")))
    except (IOError, OSError, ValueError):
        return {}
    return dict((r["name"], r) for r in d.get("hosts", []))


def yum_base(st, names):
    """기준 기록(커널·부팅 시각·기준 시각)만 뽑는다. 다시 실행해도 같아야 한다."""
    return dict((n, (st.get(n, {}).get("yum_kernel"), st.get(n, {}).get("yum_boot"), st.get(n, {}).get("yum_since"))) for n in names)


def row_re(host, status):
    return r"%s\s+\S+\s+%s(\s|$)" % (re.escape(host), re.escape(status))


def build_alt_binary(out):
    """소스 폴더를 건드리지 않고 프로그램 버전만 다른 임시 바이너리를 만든다(-X 는 var 문자열에 쓸 수 있다)."""
    go = shutil.which("go") or "/opt/go1.20/bin/go"
    e = dict(os.environ, CGO_ENABLED="0")
    r = subprocess.run([go, "build", "-trimpath", "-ldflags", "-s -w -X main.programVersion=0.4.0-dcheck", "-o", out, "."],
                       cwd=SRC, env=e, stdout=subprocess.PIPE, stderr=subprocess.PIPE, stdin=subprocess.DEVNULL)
    return r.returncode == 0 and os.path.exists(out), (r.stderr.decode()[:200] if r.returncode else "")


def yum_checks():
    """11번대: yum update 모드(계획서 4.8)를 실제 데몬·가상 터미널로 확인한다."""
    global HOSTS
    old_hosts = HOSTS
    HOSTS = YHOSTS
    ys = [("127.3.2.11", "A"), ("127.3.2.12", "B"), ("127.3.2.13", "C"), (YADDR, "D")]  # A 그대로, B 새 커널, C 부팅만, D ping 끊김
    names = [h for h, _ in ys]
    alt = WD + "/install_mn_alt"
    subprocess.run(["ip", "addr", "del", YADDR + "/32", "dev", "lo"], stdout=subprocess.PIPE, stderr=subprocess.PIPE)
    subprocess.run(["ip", "addr", "add", YADDR + "/32", "dev", "lo"], stdout=subprocess.PIPE, stderr=subprocess.PIPE)

    def addr_off():
        subprocess.run(["ip", "addr", "del", YADDR + "/32", "dev", "lo"], stdout=subprocess.PIPE, stderr=subprocess.PIPE)

    def addr_back():
        subprocess.run(["ip", "addr", "add", YADDR + "/32", "dev", "lo"], stdout=subprocess.PIPE, stderr=subprocess.PIPE)

    def ycalls():
        try:
            return len([l for l in open("ygossh.log") if "NS_YUM" in l])
        except (IOError, OSError):
            return 0

    sessions = []

    def new(args, **kw):
        s = Session(args, **kw)
        sessions.append(s)
        return s

    try:
        # 호스트파일: OS 서버(.1 .2, .15 는 두 번째 칸이 yum 이 아니라 OS) + yum 서버 4대(대소문자·탭·여러 칸 공백)
        open(YHOSTS, "w").write("127.3.2.1\n127.3.2.2\n127.3.2.11 yum\n127.3.2.12  YUM\n127.3.2.13\tyum\n%s yum\n127.3.2.15 kickstart\n" % YADDR)
        switch = int(time.time()) + 25  # B·C 는 이 시각 뒤에 재부팅한 것처럼 보인다(스텁 gossh 의 전환 epoch)
        open("ystate.txt", "w").write(
            "127.3.2.1 done\n127.3.2.2 installing\n127.3.2.15 done\n"
            "127.3.2.11 done %s %s\n" % (K0, B0)
            + "127.3.2.12 done %s %s %s 2026-11-17 14:20:00 %d\n" % (K0, B0, K1, switch)
            + "127.3.2.13 done %s %s %s 2026-11-17 14:21:00 %d\n" % (K0, B0, K0, switch)
            + "%s done %s %s\n" % (YADDR, K0, B0))
        open("ypm.conf", "w").write("gossh=%s\ninterval_sec=5\nping_timeout_ms=800\nldap_check_sh=%s/ldap_check.sh\n" % (STUB, WD))
        for f in (YHOSTS + ".pm_state.json", "ygossh.log"):
            if os.path.exists(f):
                os.remove(f)
        run(["--stop", YHOSTS])
        env = {"STUB_GOSSH_STATE": WD + "/ystate.txt", "STUB_GOSSH_LOG": WD + "/ygossh.log", "PM_MAX_RUN_SEC": "300"}
        args = ["-c", "ypm.conf", YHOSTS]

        # ---- (a) 첫 접속: 접속가능 + 기준 커널 기록, (e) OS 서버는 그대로 ----
        s = new(args, env=env)
        ok = s.wait_for(r"회차 1 ", 40)
        fr = s.frame()
        t_first = time.time()
        check("11a yum 서버 4대가 접속가능 + 기준 커널 표시(머리줄 전체 7대 (yum 4대))", ok and "8 접속가능 4" in fr and "전체 7대 (yum 4대)" in fr and ("기준 커널 " + K0) in fr, fr[:300].replace("\n", " | "))
        check("11a 접속가능 행(A~D)이 보임", all(re.search(row_re(h, "접속가능"), fr) for h in names), "")
        st = {}
        for _ in range(20):  # 회차 끝에 저장되므로 잠깐 기다린다
            st = load_state()
            if all(st.get(h, {}).get("yum_kernel") for h in names):
                break
            time.sleep(0.5)
        base0 = yum_base(st, names)
        check("11a state.json 에 mode=yum·기준 커널·부팅 시각·기준 시각 기록", all(st.get(h, {}).get("mode") == "yum" and base0[h][0] == K0 and base0[h][1] == B0 and base0[h][2] for h in names), str(base0.get(names[0])))
        check("11a 전환 시각(스텁) 전에 기준을 기록했음", t_first < switch, "%.1f초 전" % (switch - t_first))
        check("11e 같은 파일의 OS 서버는 그대로(설치완료 2·설치중 1, 기록에 yum 필드 없음, kickstart 칸은 OS)",
              "4 설치완료 2" in fr and "1 설치중 1" in fr and all("mode" not in st.get(h, {}) and "yum_kernel" not in st.get(h, {}) for h in ("127.3.2.1", "127.3.2.2", "127.3.2.15")), "")

        # ---- (d) lo 주소를 떼면 ping 이 끊긴다: ping X 가 아니라 재부팅됨 ----
        addr_off()
        ok = s.wait_for(row_re(YADDR, "재부팅됨"), 30)
        fr = s.frame()
        m = re.search(re.escape(YADDR) + r".*", fr)
        check("11d 기준 기록 뒤 ping 이 끊긴 yum 서버는 ping X 가 아니라 재부팅됨(재부팅 중)", ok and "재부팅 중" in (m.group(0) if m else "") and "5 ping X 0" in fr, m.group(0)[:100] if m else fr[:200])

        # ---- (b)(c) 전환 시각 뒤: B 는 새 커널 -> yum_update/접속가능, C 는 부팅만 -> 재부팅됨 ----
        ok = s.wait_for(row_re("127.3.2.12", "yum_update/접속가능") + r"[\s\S]*", max(10, switch - time.time() + 25))
        fr = s.frame()
        check("11b 새 부팅 시각·새 커널이 되면 yum_update/접속가능(이전 → 현재 커널 표시)", ok and ("%s → %s" % (K0, K1)) in fr and "9 yum_update/접속가능 1" in fr, "")
        check("11c 새 부팅 시각·같은 커널은 재부팅됨(커널 동일 표시)", bool(re.search(row_re("127.3.2.13", "재부팅됨") + r".*커널 동일 " + re.escape(K0), fr)), "")
        c1, r1 = ycalls(), round_of(s)
        time.sleep(16)  # 몇 회차 더: C 는 재부팅됨에 머물고 B 는 그대로, A 는 접속가능
        s.pump(0.5)
        fr = s.frame()
        c2, r2 = ycalls(), round_of(s)
        check("11 gossh: yum 조회(NS_YUM)는 회차당 한 번(회차 %d 늘 때 호출 %d 늘어남)" % (r2 - r1, c2 - c1), r2 - r1 >= 2 and abs((c2 - c1) - (r2 - r1)) <= 1, "")
        check("11c 이후 회차에도 C 는 재부팅됨 유지·B 는 yum_update/접속가능·A 는 접속가능",
              bool(re.search(row_re("127.3.2.13", "재부팅됨"), fr)) and bool(re.search(row_re("127.3.2.12", "yum_update/접속가능"), fr)) and bool(re.search(row_re("127.3.2.11", "접속가능"), fr)), "")
        st = load_state()
        check("11b state.json: B 는 기준 커널 그대로·현재 커널 새 커널·완료 시각 기록, C 는 재부팅 시각 기록",
              st.get("127.3.2.12", {}).get("yum_kernel") == K0 and st["127.3.2.12"].get("yum_now") == K1 and bool(st["127.3.2.12"].get("yum_done_at"))
              and st.get("127.3.2.13", {}).get("yum_kernel") == K0 and st["127.3.2.13"].get("rebooted_at", "0001")[:4] != "0001", "")
        check("11e yum 서버가 섞여도 OS 서버 집계는 그대로(설치완료 2·설치중 1·ping X 0)", "4 설치완료 2" in fr and "1 설치중 1" in fr and "5 ping X 0" in fr, "")

        # ---- (f) y 패널 ----
        s.send(b"y", 0.7)
        fr = s.frame()
        check("11f y: yum 커널 표(제목·열 머리·서버 행·기준/현재 커널)", "[y] yum 커널" in fr and "hostname" in fr and "기준 커널" in fr and "현재 커널" in fr
              and bool(re.search(r"127\.3\.2\.12\s+\S+\s+yum_update/접속가능\s+%s\s+%s" % (re.escape(K0), re.escape(K1)), fr)) and "1-5/5줄" in fr, fr[:250].replace("\n", " | "))
        s.send(b"y", 0.5)
        check("11f y 를 다시 누르면 닫힘(기본 화면 복귀)", "[y] yum 커널" not in s.frame() and "호스트명" in s.frame(), "")
        s.send(b"y", 0.5)
        s.send(b"q", 0.5)
        check("11f 패널에서 q: 기본 화면(종료 아님)", "[y] yum 커널" not in s.frame() and s.pump(0.2) and s.code is None, "")
        base1 = yum_base(load_state(), names)
        pid1 = daemon_pid()
        s.send(b"q", 0.4)
        s.wait_exit()
        s.kill()

        # ---- (g) 다시 실행: 같은 데몬에 붙고 기준 커널 그대로 ----
        s2 = new(args, env=env)
        s2.pump(1.2)
        fr = s2.frame()
        check("11g 다시 실행: 같은 데몬에 붙음(pid 같음)·첫 프레임부터 yum 상태가 보임",
              pid1 is not None and daemon_pid() == pid1 and bool(re.search(row_re("127.3.2.12", "yum_update/접속가능"), fr)) and "전체 7대 (yum 4대)" in fr, "pid %s/%s" % (daemon_pid(), pid1))
        check("11g 다시 실행해도 기준 커널·부팅 시각·기준 시각이 그대로", base1 == base0 and yum_base(load_state(), names) == base0, "")
        s2.send(b"q", 0.4)
        s2.wait_exit()
        s2.kill()

        # ---- (h) 바이너리(프로그램 버전)가 바뀌면 옛 데몬을 멈추고 새로 시작, 기록은 유지 ----
        built, why = build_alt_binary(alt)
        check("11h 프로그램 버전만 다른 임시 바이너리 빌드(-X main.programVersion)", built, why)
        if built:
            s3 = new(args, env=env, binary=alt)
            ok = s3.wait_for(r"회차 1 ", 40)
            pid2 = daemon_pid()
            fr = s3.frame()
            check("11h 다른 버전 바이너리로 실행: 옛 데몬 종료·새 데몬(새 pid)·안내 문구", ok and pid2 is not None and pid2 != pid1 and wait_dead(pid1, 10) and "다시 시작했습니다" in fr, "pid %s -> %s" % (pid1, pid2))
            time.sleep(1)
            check("11h 교체 뒤에도 기준 커널 유지·완료(B)·재부팅됨(C, D) 상태 유지", yum_base(load_state(), names) == base0 and bool(re.search(row_re("127.3.2.12", "yum_update/접속가능"), s3.frame()))
                  and bool(re.search(row_re("127.3.2.13", "재부팅됨"), s3.frame())) and bool(re.search(row_re(YADDR, "재부팅됨"), s3.frame())), "")
            s3.send(b"Q", 0.3)
            s3.send(b"y", 0.5)
            s3.wait_exit(15)
            s3.kill()
        else:
            s3 = None

        # ---- 수집 종료 뒤 다시 실행(원래 바이너리): 새 데몬, 기록 유지. D 의 주소를 되살리면 재부팅됨에 머묾 ----
        addr_back()
        pid_old = daemon_pid()
        s4 = new(args, env=env)
        s4.pump(1.2)
        pid3 = daemon_pid()
        check("11g 수집 종료 뒤 다시 실행: 새 데몬이지만 기준 커널·완료·재부팅됨은 기록에서 이어짐",
              pid3 is not None and pid3 != pid1 and pid_old is None and yum_base(load_state(), names) == base0
              and bool(re.search(row_re("127.3.2.12", "yum_update/접속가능"), s4.frame())) and bool(re.search(row_re("127.3.2.13", "재부팅됨"), s4.frame())), "pid %s" % pid3)
        ok = s4.wait_for(row_re(YADDR, "재부팅됨") + r".*커널 동일", 30)
        check("11d 주소가 돌아와 ssh 가 되어도 재부팅됨 유지(커널 동일 표시, 접속가능으로 되돌아가지 않음)", ok and yum_base(load_state(), names) == base0, "")
        s4.send(b"Q", 0.3)
        s4.send(b"y", 0.5)
        s4.wait_exit(15)
        s4.kill()
        check("11 정리: 데몬 종료·소켓 정리·상태 파일 유지", daemon_pid() is None and os.path.exists(YHOSTS + ".pm_state.json") and not any(f.endswith(".sock") for f in os.listdir("/run/install_mn")), "")
    finally:
        for x in sessions:
            x.kill()
        run(["--stop", YHOSTS])
        subprocess.run(["ip", "addr", "del", YADDR + "/32", "dev", "lo"], stdout=subprocess.PIPE, stderr=subprocess.PIPE)
        for f in (alt,):
            if os.path.exists(f):
                os.remove(f)
        HOSTS = old_hosts


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
    yum_checks()
    n = len([x for x in results if not x])
    print("총 %d 항목 중 FAIL %d" % (len(results), n))
    return 1 if n else 0


if __name__ == "__main__":
    sys.exit(main())
