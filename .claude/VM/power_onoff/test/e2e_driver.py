#!/usr/bin/env python3
# e2e_driver.py — vm_power_onoff 바이너리를 vcsim(시뮬레이터) 위에서 처음부터 끝까지 돌려 본다 (Python 3.6+).
# 직접 실행하지 말고 test/e2e_vcsim.sh 로 실행 (빌드·conf 준비·정리 포함).
# 여기 시나리오는 기존 방식(cross_check=off) 안전장치를 쓴다 — 실제 cross_check 데몬이 필요 없다.
# 교차검증 방식(cross_check=on, 기본)은 test/e2e_vcsim_crosscheck.py 가 이 파일을 import 해서 시험한다.
import os, re, select, signal, subprocess, sys, time

BIN, SIM, WORK, CONF = sys.argv[1], sys.argv[2], sys.argv[3], "/etc/vcetner_pwon_off/pwonoff.conf"
TARGETS = ["svr1ev01", "svr1ev02", "svr1ev03", "svr2ev01"]
ADDR_FILE = "/tmp/vcsim_e2e.addr"
results = []


class Proc:
    def __init__(self, args, cwd=None):
        self.p = subprocess.Popen(args, cwd=cwd or WORK, stdin=subprocess.PIPE, stdout=subprocess.PIPE,
                                  stderr=subprocess.STDOUT, bufsize=0)
        self.buf = ""
        self.pos = 0

    def _fill(self, timeout):
        r, _, _ = select.select([self.p.stdout], [], [], timeout)
        if not r:
            return None
        d = os.read(self.p.stdout.fileno(), 65536)
        if not d:
            return b""
        self.buf += d.decode("utf-8", "replace")
        return d

    def expect(self, pat, timeout=30):
        i, m = self.expect_any([pat], timeout)
        return m

    def expect_any(self, pats, timeout=30):
        """여러 패턴 중 먼저 나오는 것의 (index, match)."""
        end = time.time() + timeout
        rxs = [re.compile(p) for p in pats]
        while True:
            best = None
            for i, rx in enumerate(rxs):
                m = rx.search(self.buf, self.pos)
                if m and (best is None or m.start() < best[1].start()):
                    best = (i, m)
            if best:
                self.pos = best[1].end()
                return best
            left = end - time.time()
            if left <= 0:
                raise AssertionError("expect 시간 초과: %r\n--- 출력 끝 ---\n%s" % (pats, self.buf[-1500:]))
            d = self._fill(min(left, 1.0))
            if d == b"":
                raise AssertionError("프로세스가 끝남(EOF), 기대: %r\n--- 출력 ---\n%s" % (pats, self.buf[-1500:]))

    def send(self, s):
        self.p.stdin.write(s.encode())
        self.p.stdin.flush()

    def wait(self, timeout=30):
        end = time.time() + timeout
        while self.p.poll() is None and time.time() < end:
            self._fill(0.2)
        if self.p.poll() is None:
            self.p.kill()
            raise AssertionError("종료하지 않음\n" + self.buf[-1500:])
        while self._fill(0.1):
            pass
        return self.p.returncode


class Sim:
    def __init__(self, power):
        if os.path.exists(ADDR_FILE):
            os.remove(ADDR_FILE)
        self.p = subprocess.Popen([SIM, "-addr-file", ADDR_FILE, "-power", power],
                                  stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        for _ in range(150):
            if os.path.exists(ADDR_FILE) and open(ADDR_FILE).read().strip():
                self.addr = open(ADDR_FILE).read().strip()
                return
            time.sleep(0.1)
        raise AssertionError("vcsim 서버가 뜨지 않음")

    def close(self):
        self.p.send_signal(signal.SIGTERM)
        try:
            self.p.wait(5)
        except Exception:
            self.p.kill()


def write_conf(addr, extra="", cc="off"):
    """cc: "off"(기존 방식, 기본) / "on" / None(키를 쓰지 않음 = 프로그램 기본값 on)."""
    cc_line = "" if cc is None else "cross_check=%s\n" % cc
    open(CONF, "w").write(
        "allowed_dir=%s\nusers=tester,other\nvcenters=%s\nvc_id=user\nconcurrency=4\n"
        "shutdown_timeout=3\nboot_timeout=2\nboot_poll_interval=1\nport_check_timeout=1\n%s%s" % (WORK, addr, cc_line, extra))
    os.chmod(CONF, 0o644)


def write_targets(names=TARGETS):
    f = os.path.join(WORK, "on_off_tester.txt")
    if os.path.lexists(f):
        os.remove(f)
    open(f, "w").write("# e2e\n" + "\n".join(names) + "\n")


def scenario(name):
    def deco(fn):
        def run():
            sys.stdout.write("%-38s " % name)
            sys.stdout.flush()
            try:
                fn()
                print("PASS")
                results.append((name, True))
            except Exception as e:  # noqa
                print("FAIL\n    " + str(e).replace("\n", "\n    "))
                results.append((name, False))
        return run
    return deco


def start(args_extra, power="on", targets=TARGETS):
    sim = Sim(power)
    write_conf(sim.addr)
    write_targets(targets)
    pr = Proc([BIN, "-u", "tester", "-v", sim.addr] + args_extra)
    return sim, pr


def login(pr, action):
    pr.expect(r"비밀번호: ")
    pr.send("pw\n")
    pr.expect(r"번호 입력: ")
    pr.send("%d\n" % action)


def through_vwp(pr):
    pr.expect(r"vwp대상 장비가 포함되어있습니다")
    m = pr.expect(r"코드를 입력하세요: ([A-Z0-9]{6})")
    pr.send(m.group(1) + "\n")


# 4/5 단계 질문 (두 방식 공통). 강제 PowerOff 질문 "(YeS/No): " 과는 다르다.
YES_PROMPT = r"\[4/5\] 정말 작업을 진행하시겠습니까\? \(YeS/No, ls=대상 보기\): "


def through_code(pr, names):
    """1/5: 확인 코드를 그대로 입력하고 대상 목록·총 N대 를 확인한다."""
    m = pr.expect(r"\[1/5\] 확인 코드: ([A-Z0-9]{6})")
    pr.expect(r"입력하세요: ")
    pr.send(m.group(1) + "\n")
    pr.expect(r"총 %d대" % len(names))


def answer_yes(pr, names, ans="YeS", ls=0):
    """4/5: ls 를 ls 번 보내 목록·총 N대 가 다시 나오는지(취소 없이 다시 묻는지) 확인한 뒤 ans 를 보낸다."""
    pr.expect(YES_PROMPT)
    for _ in range(ls):
        pr.send("ls\n")
        pr.expect(r"작업 대상 호스트 목록:\n(?:.*\n)*?총 %d대\n" % len(names))
        pr.expect(YES_PROMPT)
    pr.send(ans + "\n")


def through_guard(pr, names=TARGETS, ans="YeS", ls=0):
    """기존 방식(cross_check=off): 코드 → Enter → VM 이름 입력 → (ls) → YeS."""
    through_code(pr, names)
    pr.expect(r"\[2/5\]")
    pr.send("\n")
    pr.expect(r"이내\):")
    pr.send(" ".join(reversed(names)) + "\n")  # 순서 무관, 공백 구분
    pr.expect(r"호스트 목록 일치")
    answer_yes(pr, names, ans, ls)


def check_states(power_expect, names=TARGETS):
    """현재 전원 상태를 -n 계획 출력으로 확인한다(vcsim 서버는 시나리오 안에서 계속 떠 있어야 함)."""
    pr = Proc([BIN, "-u", "tester", "-v", open(ADDR_FILE).read().strip(), "-n"])
    login(pr, 2)
    pr.expect(r"-n 지정")
    assert pr.wait(10) == 0
    for n in names:
        assert "%s(%s)" % (n, power_expect) in pr.buf, "%s 전원 %s 기대:\n%s" % (n, power_expect, pr.buf[-800:])


@scenario("plan_only(-n) 전원 변화 없음")
def s_plan():
    sim, pr = start(["-n"])
    try:
        login(pr, 2)
        pr.expect(r"Power On\(순차\) 계획 — 총 4대")
        pr.expect(r"-n 지정")
        assert pr.wait(10) == 0
        assert "svr1ev01(on)" in pr.buf and "svr2ev01(on)" in pr.buf, pr.buf
        assert "lpage 사용중" in pr.buf and "lpage 미사용" in pr.buf, pr.buf
    finally:
        sim.close()


@scenario("off: VWP+안전장치(ls 2회)+강제OFF(YeS)")
def s_off():
    sim, pr = start([], power="on")
    try:
        login(pr, 1)
        pr.expect(r"Power Off 계획 — 총 4대")
        through_vwp(pr)
        through_guard(pr, ls=2)
        pr.expect(r"10 \.\. 9 \.\. 8 \.\. 7 \.\. 6 \.\. 5 \.\. 4 \.\. 3 \.\. 2 \.\. 1", 20)
        pr.expect(r"강제 PowerOff 하시겠습니까\? \(YeS/No\): ", 30)
        pr.send("YeS\n")
        pr.expect(r"합계 4건: 성공 4 / 스킵 0 / 실패 0")
        assert pr.wait(10) == 0
        check_states("off")
        rd = os.path.join(WORK, "run_tester")
        assert os.path.isdir(rd) and os.listdir(rd), "감사 로그 없음"
        assert "안전장치 4단계 ls(대상 보기) 2회" in read_audit(), "감사 로그에 ls 횟수 없음"
    finally:
        sim.close()


def read_audit(user="tester"):
    """allowed_dir/run_<user>/ 아래 감사 로그를 모두 이어 붙여 돌려준다."""
    logs, rd = "", os.path.join(WORK, "run_" + user)
    for root, _, files in os.walk(rd):
        for f in sorted(files):
            logs += open(os.path.join(root, f), errors="replace").read()
    return logs


@scenario("off: 강제 OFF 질문에 yes → 끄지 않음")
def s_off_refuse():
    sim, pr = start([], power="on", targets=["svr1ev01"])
    try:
        login(pr, 1)
        through_guard(pr, ["svr1ev01"])
        pr.expect(r"강제 PowerOff 하시겠습니까\? \(YeS/No\): ", 40)
        pr.send("yes\n")
        pr.expect(r"실패 1")
        assert pr.wait(10) == 1
        check_states("on", ["svr1ev01"])
    finally:
        sim.close()


@scenario("on_all(OS설치용) 일괄 부팅")
def s_on_all():
    sim, pr = start([], power="off")
    try:
        login(pr, 3)
        pr.expect(r"일괄 Power On\(OS 설치용\) 계획 — 총 4대")
        through_vwp(pr)
        through_guard(pr)
        pr.expect(r"합계 4건: 성공 4 / 스킵 0 / 실패 0", 40)
        assert pr.wait(10) == 0
        check_states("on")
    finally:
        sim.close()


@scenario("on_seq: lpage BM 부팅 미완료 → q")
def s_on_seq():
    sim, pr = start([], power="off")
    try:
        login(pr, 2)
        through_vwp(pr)
        through_guard(pr)
        asked = 0
        while True:
            i, _ = pr.expect_any([r"계속 대기\(c\) / 이 VM 건너뛰고 다음 ev 단계\(s\) / 순차 부팅 중단\(q\): ", r"합계 \d+건"], 60)
            if i == 1:
                break
            asked += 1
            pr.send("q\n")
        assert asked >= 1, "lpage BM 순차 부팅에서 질문이 나와야 함"
        assert pr.wait(10) == 1, pr.buf[-800:]
        assert "lpage 사용중" in pr.buf, pr.buf
    finally:
        sim.close()


@scenario("취소: 확인 코드 불일치")
def s_wrong_code():
    sim, pr = start([], power="on", targets=["svr1ev01"])
    try:
        login(pr, 1)
        pr.expect(r"\[1/5\] 확인 코드: ")
        pr.expect(r"입력하세요: ")
        pr.send("ZZZZZZ\n")
        pr.expect(r"\[취소\] 확인 코드 불일치")
        assert pr.wait(10) == 1
        check_states("on", ["svr1ev01"])
    finally:
        sim.close()


@scenario("취소: 호스트 목록 불일치(중복 입력)")
def s_wrong_hosts():
    sim, pr = start([], power="on", targets=["svr1ev01", "svr1ev02"])
    try:
        login(pr, 1)
        m = pr.expect(r"\[1/5\] 확인 코드: ([A-Z0-9]{6})")
        pr.expect(r"입력하세요: ")
        pr.send(m.group(1) + "\n")
        pr.expect(r"\[2/5\]")
        pr.send("\n")
        pr.expect(r"이내\):")
        pr.send("svr1ev01 svr1ev01\n")
        pr.expect(r"\[취소\] 호스트 목록 불일치")
        assert pr.wait(10) == 1
    finally:
        sim.close()


@scenario("취소: YeS 아님")
def s_not_yes():
    sim, pr = start([], power="on", targets=["svr1ev01"])
    try:
        login(pr, 1)
        through_guard(pr, ["svr1ev01"], ans="yes")
        pr.expect(r"\[취소\] YeS 미입력")
        assert pr.wait(10) == 1
        check_states("on", ["svr1ev01"])
    finally:
        sim.close()


@scenario("취소: 대상 VM 이 vCenter 에 없음")
def s_missing_vm():
    sim, pr = start([], power="on", targets=["svr1ev01", "nopeev01"])
    try:
        login(pr, 1)
        pr.expect(r"없음: nopeev01")
        assert pr.wait(10) == 1
        assert "합계" not in pr.buf
    finally:
        sim.close()


@scenario("실행불가: 허용 경로 밖")
def s_outside():
    sim = Sim("on")
    try:
        write_conf(sim.addr)
        write_targets()
        pr = Proc([BIN, "-u", "tester", "-v", sim.addr], cwd="/tmp")
        assert pr.wait(10) == 1 and "에서만 실행할 수 있습니다" in pr.buf, pr.buf
    finally:
        sim.close()


@scenario("실행불가: 목록에 없는 user")
def s_bad_user():
    sim = Sim("on")
    try:
        write_conf(sim.addr)
        pr = Proc([BIN, "-u", "hacker", "-v", sim.addr])
        assert pr.wait(10) == 1 and "[오류]" in pr.buf, pr.buf
    finally:
        sim.close()


@scenario("실행불가: 대상 파일이 심볼릭 링크")
def s_symlink():
    sim = Sim("on")
    try:
        write_conf(sim.addr)
        f = os.path.join(WORK, "on_off_tester.txt")
        if os.path.lexists(f):
            os.remove(f)
        open("/tmp/real_targets.txt", "w").write("svr1ev01\n")
        os.symlink("/tmp/real_targets.txt", f)
        pr = Proc([BIN, "-u", "tester", "-v", sim.addr])
        assert pr.wait(10) == 1 and "[오류]" in pr.buf, pr.buf
        os.remove(f)
    finally:
        sim.close()


@scenario("실행불가: conf 가 other 쓰기 가능")
def s_conf_perm():
    sim = Sim("on")
    try:
        write_conf(sim.addr)
        write_targets()
        os.chmod(CONF, 0o666)
        pr = Proc([BIN, "-u", "tester", "-v", sim.addr])
        assert pr.wait(10) == 1 and "[오류]" in pr.buf, pr.buf
    finally:
        os.chmod(CONF, 0o644)
        sim.close()


@scenario("실행불가: 기본 on 인데 cross_check 미설치")
def s_cc_missing():
    sim = Sim("on")
    try:
        write_conf(sim.addr, "cross_check_bin=/nonexistent/cross_check\n", cc=None)  # 키 없음 = 기본 on
        write_targets(["svr1ev01"])
        pr = Proc([BIN, "-u", "tester", "-v", sim.addr])
        assert pr.wait(10) == 1, pr.buf
        assert ("cross_check 가 설치되어 있지 않습니다: /nonexistent/cross_check — bash setup.sh 로 설치하거나 "
                "pwonoff.conf 에 cross_check=off 를 지정하세요") in pr.buf, pr.buf
        assert "비밀번호" not in pr.buf and "[1/5]" not in pr.buf, "vCenter/안전장치 전에 멈춰야 함:\n" + pr.buf
        # -n(계획만)은 교차검증을 쓰지 않으므로 cross_check 없이도 된다
        pr = Proc([BIN, "-u", "tester", "-v", sim.addr, "-n"])
        login(pr, 1)
        pr.expect(r"-n 지정")
        assert pr.wait(10) == 0
    finally:
        sim.close()


@scenario("user/vCenter/기능 번호 선택 + 비밀번호 유출 없음")
def s_menus():
    sim = Sim("on")
    try:
        write_conf(sim.addr)
        write_targets(["svr1ev01"])
        pr = Proc([BIN, "-n"])
        pr.expect(r"1\) tester")
        pr.expect(r"번호 입력: ")
        pr.send("1\n")
        pr.expect(r"1\) %s" % re.escape(sim.addr))
        pr.expect(r"번호 입력 .*: ")
        pr.send("1\n")
        login(pr, 3)
        pr.expect(r"-n 지정")
        assert pr.wait(10) == 0
        assert "잘못된 선택" not in pr.buf and "pw" not in pr.buf.split("비밀번호:")[-1], pr.buf
    finally:
        sim.close()


def main():
    for fn in [s_plan, s_menus, s_off, s_off_refuse, s_on_all, s_on_seq, s_wrong_code, s_wrong_hosts, s_not_yes,
               s_missing_vm, s_outside, s_bad_user, s_symlink, s_conf_perm, s_cc_missing]:
        fn()
    fails = [n for n, ok in results if not ok]
    print("\nE2E PASS=%d FAIL=%d" % (len(results) - len(fails), len(fails)))
    sys.exit(1 if fails else 0)


if __name__ == "__main__":
    main()
