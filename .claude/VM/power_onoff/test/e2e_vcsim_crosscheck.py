#!/usr/bin/env python3
# e2e_vcsim_crosscheck.py — 교차검증 방식(cross_check=on, 기본) 안전장치를 vcsim + 격리된 실제 cross_check 데몬으로 끝까지 시험한다 (Python 3.6+).
# 직접 실행하지 말고 test/e2e_vcsim_crosscheck.sh 로 실행 (빌드·conf 준비·정리 포함).
#   python3 test/e2e_vcsim_crosscheck.py <vm_power_onoff> <vcsim_server> <allowed_dir>   (환경변수 CC_BIN=<cross_check 실행 파일>)
# 데몬은 임시 폴더에서만 돈다(/etc/cross_check·/var·crontab 을 건드리지 않음).
# 확인자 승인은 TUI(cross_check --user other)의 줄 입력 화면에 파이프로: o <작업> → a → 작업자에게 받은 대상 붙여넣기(빈 줄로 끝) → 패스워드.
# 흐름: 1/5 코드 → 2/5 확인자 번호 → 3/5 승인 대기(확인자 TUI 승인) → 4/5 ls·YeS → 5/5 카운트다운 → --start → 전원 작업 → --finish
# 각 시나리오가 끝나면 격리 데몬의 작업 기록(jobs.json)에서 status/phase/caller/reason 을 확인한다:
#   끝까지 실행(일부 VM 실패 포함) → 승인/작업완료, 승인 뒤 No·SIGINT → 승인/작업중단, 확인자 취소·승인 유효시간 초과 → 중단
import json, os, re, shutil, signal, subprocess, sys, tempfile, time

import e2e_driver as d  # 같은 폴더의 기존 E2E 도구(Proc/Sim/write_conf …)를 그대로 쓴다

CC = os.environ["CC_BIN"]
ISO = tempfile.mkdtemp(prefix="cc_vcsim_iso_")
STATE = ISO + "/lib/jobs.json"
TESTER_PW, OTHER_PW = "tester_pw_1", "other_pw_1"
CALLER = "vm_power_onoff"  # --run --caller 로 넘어오는 실제 실행 파일 이름(빌드한 바이너리 이름)
TRANSCRIPT = os.environ.get("CC_E2E_TRANSCRIPT", "")  # 주면 승인 시나리오의 화면 출력을 여기에 저장
CCENV = dict(os.environ)
CCENV.update({
    "CROSS_CHECK_SOCKET": ISO + "/run/cc.sock", "CROSS_CHECK_CONF": ISO + "/etc/cross_check.conf",
    "CROSS_CHECK_AUTH": ISO + "/etc/auth.conf", "CROSS_CHECK_STATE": STATE,
    "CROSS_CHECK_LOCK": ISO + "/run/daemon.lock", "CROSS_CHECK_LOG": ISO + "/daemon.log",
    "CROSS_CHECK_COLOR": "never"})
for k in ("CROSS_CHECK_TOKEN", "CROSS_CHECK_TEST_MIN_TTL_SEC", "CROSS_CHECK_CALLER"):
    CCENV.pop(k, None)
# 자식(vm_power_onoff → cross_check --run/--start/--finish)이 같은 격리 데몬을 쓰도록 상속
os.environ.update({k: v for k, v in CCENV.items() if k.startswith("CROSS_CHECK_")})
for k in ("CROSS_CHECK_TOKEN", "CROSS_CHECK_CALLER"):
    os.environ.pop(k, None)

dproc = None
results = d.results


def daemon_start(ttl):
    """격리 데몬을 approval_ttl_sec=ttl 로 (다시) 띄운다. 10초 미만은 시험 전용 하한(CROSS_CHECK_TEST_MIN_TTL_SEC)으로만 허용."""
    global dproc
    daemon_stop()
    for sub in ("run", "etc", "lib"):
        os.makedirs(os.path.join(ISO, sub), exist_ok=True)
    open(ISO + "/etc/cross_check.conf", "w").write(
        "target_dir=%s\nauth=required\nusers=tester,other\napproval_ttl_sec=%d\n" % (d.WORK, ttl))
    if not os.path.exists(ISO + "/etc/auth.conf"):
        fd = os.open(ISO + "/etc/auth.conf", os.O_WRONLY | os.O_CREAT, 0o600)
        os.write(fd, b"admin=adm_pw_1\n")
        os.close(fd)
    os.chmod(ISO + "/etc/auth.conf", 0o600)
    env = dict(CCENV)
    if ttl < 10:
        env["CROSS_CHECK_TEST_MIN_TTL_SEC"] = str(ttl)
    dproc = subprocess.Popen([CC, "--daemon"], env=env, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
    for _ in range(50):
        if os.path.exists(ISO + "/run/cc.sock"):
            break
        time.sleep(0.1)
    assert os.path.exists(ISO + "/run/cc.sock"), "격리 데몬이 뜨지 않음"


def daemon_stop():
    global dproc
    if dproc is not None:
        dproc.terminate()
        try:
            dproc.wait(10)
        except Exception:
            dproc.kill()
        dproc = None
    try:
        os.remove(ISO + "/run/cc.sock")
    except OSError:
        pass


def tui(user, lines):
    """사용자 화면(줄 입력 모드: stdin 이 파이프)에 lines 를 차례로 넣는다."""
    p = subprocess.run([CC, "--user", user], input=("\n".join(lines) + "\n").encode(), env=CCENV,
                       stdout=subprocess.PIPE, stderr=subprocess.STDOUT, timeout=30)
    return p.returncode, p.stdout.decode("utf-8", "replace")


def register():
    for u, pw in (("tester", TESTER_PW), ("other", OTHER_PW)):
        rc, out = tui(u, [pw, pw, "q"])
        assert "[등록 완료]" in out, out


def wait_job_id(timeout=20):
    end = time.time() + timeout
    out = ""
    while time.time() < end:
        rc, out = tui("other", ["q"])
        m = re.search(r"^ +\[\d+\] +(\S+) .*대기", out, re.M)
        if m:
            return m.group(1)
        time.sleep(0.5)
    raise AssertionError("대기 중인 작업이 확인자 화면에 안 보임\n" + out)


def approve(jid, targets=("svr1ev01",), pw=OTHER_PW):
    """확인자 승인: 작업 열기 → a → 작업자에게 받은 대상 붙여넣기(빈 줄로 끝) → 패스워드."""
    rc, out = tui("other", ["o " + jid, "a"] + list(targets) + ["", pw, "q"])
    assert "[승인 완료]" in out, out
    return out


def job_record(jid):
    try:
        with open(STATE) as f:
            jobs = json.load(f).get("jobs") or []
    except (OSError, ValueError):
        return None
    for j in jobs:
        if j.get("id") == jid:
            return j
    return None


def assert_job(jid, status, phase="", reason=None, reason_re=None, caller=CALLER, timeout=10):
    """격리 데몬의 jobs.json 에서 작업 jid 의 status/phase/caller/reason 을 확인한다(기록될 때까지 잠깐 기다림)."""
    end = time.time() + timeout
    j = None
    while time.time() < end:
        j = job_record(jid)
        if j and j.get("status") == status and j.get("phase", "") == phase:
            break
        time.sleep(0.2)
    assert j is not None, "jobs.json 에 작업 %s 없음" % jid
    got = (j.get("status"), j.get("phase", ""))
    assert got == (status, phase), "작업 %s 상태 %r 기대, 실제 %r: %r" % (jid, (status, phase), got, j)
    assert j.get("caller") == caller, "작업 %s caller %r 기대, 실제 %r" % (jid, caller, j.get("caller"))
    if reason is not None:
        assert j.get("reason", "") == reason, "작업 %s 사유 %r 기대, 실제 %r" % (jid, reason, j.get("reason"))
    if reason_re is not None:
        assert re.search(reason_re, j.get("reason", "")), "작업 %s 사유가 %r 와 맞지 않음: %r" % (jid, reason_re, j.get("reason"))
    return j


def audit_files():
    found = set()
    for root, _, files in os.walk(os.path.join(d.WORK, "run_tester")):
        found.update(os.path.join(root, f) for f in files)
    return found


def new_audit(before):
    """이 시나리오에서 새로 생긴 감사 로그만 이어 붙인다(이전 시나리오 기록과 섞이지 않게)."""
    return "".join(open(f, errors="replace").read() for f in sorted(audit_files() - before))


def to_wait(menu=True, targets=("svr1ev01",), action=1, power="on"):
    """1/5 코드 → 2/5 확인자(menu=True 면 번호 메뉴에서 1번=other, 아니면 -c other) → 3/5 승인 대기 까지."""
    sim = d.Sim(power)
    d.write_conf(sim.addr, "cross_check_bin=%s\n" % CC, cc="on")
    d.write_targets(list(targets))
    args = [d.BIN, "-u", "tester", "-v", sim.addr] + ([] if menu else ["-c", "other"])
    before = audit_files()
    pr = d.Proc(args)
    pr.audit_before = before
    d.login(pr, action)
    d.through_code(pr, list(targets))
    if menu:
        pr.expect(r"\[2/5\] 확인자를 선택하세요 \(교차검증 승인자, 작업자 tester 제외\):\n  1\) other\n번호 입력: ")
        pr.send("9\n")  # 범위 밖 → 다시 묻는다(남은 기회 2번)
        pr.expect(r"남은 기회 2번")
        pr.expect(r"번호 입력: ")
        pr.send("1\n")
        pr.expect(r"확인자: other")
    else:
        pr.expect(r"\[2/5\] 확인자: other \(-c\)")
    pr.expect(r"\[3/5\] 교차검증 — 확인자 other 의 승인을 받습니다")
    pr.expect(r"승인을 기다리는 중", 30)
    assert "이내):" not in pr.buf and "1분(60초)" not in pr.buf, "교차검증 방식인데 VM 이름 입력 단계가 나옴:\n" + pr.buf
    return sim, pr


def no_token(pr):
    assert "CROSS_CHECK_TOKEN=" not in pr.buf and "CROSS_CHECK_JOB=" not in pr.buf and not re.search(r"[0-9a-f]{32}", pr.buf), \
        "토큰 노출: " + pr.buf[-800:]


def to_countdown(pr, jid, targets=("svr1ev01",), ls=0):
    """승인 → 4/5 YeS → 5/5 카운트다운 시작까지."""
    approve(jid, targets)
    pr.expect(r"교차검증 승인됨 \(확인자 other, 작업 ID %s\)" % re.escape(jid), 20)
    d.answer_yes(pr, list(targets), "YeS", ls=ls)
    pr.expect(r"\[5/5\] \(Ctrl\+C 로 취소 가능\)\n10", 20)


def assert_reported(pr, result):
    au = new_audit(pr.audit_before)
    assert "교차검증 결과 보고: %s job=" % result in au, "감사 로그에 결과 보고(%s) 없음:\n%s" % (result, au[-1200:])
    assert "결과 보고 실패" not in au and "[경고] 교차검증 결과" not in pr.buf, "결과 보고 실패:\n" + pr.buf[-800:]
    label = "승인/작업완료" if result == "done" else "승인/작업중단"
    assert "[INFO] 교차검증 결과 보고: %s" % label in pr.buf, "화면에 결과 보고 없음:\n" + pr.buf[-800:]
    return au


def assert_not_reported(pr):
    au = new_audit(pr.audit_before)
    assert "교차검증 결과 보고" not in au and "교차검증 결과 보고" not in pr.buf, "--finish 를 보내면 안 됨:\n" + au[-1200:]
    return au


@d.scenario("cc: 번호선택→승인→ls→YeS→OFF → 작업완료")
def s_cc_approve():
    sim, pr = to_wait(menu=True)
    try:
        jid = wait_job_id()
        time.sleep(1.0)
        assert "교차검증 승인됨" not in pr.buf and "[4/5]" not in pr.buf, "승인 전에 진행함:\n" + pr.buf[-600:]
        d.check_states("on", ["svr1ev01"])  # 승인 전에는 그대로 켜져 있음
        assert_job(jid, "대기")
        to_countdown(pr, jid, ls=1)  # "[5/5] (Ctrl+C 로 취소 가능)\n10" 까지 확인함
        pr.expect(r" \.\. 9 \.\. 8 \.\. 7 \.\. 6 \.\. 5 \.\. 4 \.\. 3 \.\. 2 \.\. 1\n", 20)
        pr.expect(r"교차검증 통과 — 전원 작업을 시작합니다", 20)
        assert_job(jid, "승인", "작업중")
        pr.expect(r"강제 PowerOff 하시겠습니까\? \(YeS/No\): ", 40)
        pr.send("YeS\n")
        pr.expect(r"합계 1건: 성공 1 / 스킵 0 / 실패 0", 30)
        assert pr.wait(10) == 0
        no_token(pr)
        d.check_states("off", ["svr1ev01"])
        logs = assert_reported(pr, "done")
        for w in ("교차검증 확인자 선택 확인자=other 방법=번호(2번째 입력)", "교차검증 승인 확인자=other job=" + jid,
                  "안전장치 4단계 ls(대상 보기) 1회", "교차검증 시작 허용 확인자=other job=" + jid, "안전장치 1~5단계 통과 (교차검증 방식)",
                  "교차검증 결과 보고: done job=" + jid):
            assert w in logs, "감사 로그에 %r 없음:\n%s" % (w, logs[-1200:])
        assert TESTER_PW not in logs and OTHER_PW not in logs
        j = assert_job(jid, "승인", "작업완료")
        assert j.get("started_at") and j.get("finished_at"), j
        if TRANSCRIPT:
            open(TRANSCRIPT, "w").write(pr.buf)
    finally:
        sim.close()


@d.scenario("cc: 승인→YeS→강제OFF No(VM 실패) → 작업완료")
def s_cc_vm_fail_done():
    # vcsim 에서는 Guest 종료로 꺼지지 않아 강제 PowerOff 질문이 나온다 → No 면 그 VM 은 실패.
    # 스크립트는 끝까지 실행됐으므로 결과는 작업완료(VM 별 성공 여부는 보지 않음), 종료 코드는 실패 VM 때문에 1.
    sim, pr = to_wait(menu=False, targets=("svr1ev01", "svr1ev03"))
    try:
        jid = wait_job_id()
        to_countdown(pr, jid, targets=("svr1ev01", "svr1ev03"))
        pr.expect(r"교차검증 통과 — 전원 작업을 시작합니다", 20)
        pr.expect(r"강제 PowerOff 하시겠습니까\? \(YeS/No\): ", 40)
        pr.send("No\n")
        pr.expect(r"합계 2건: 성공 0 / 스킵 0 / 실패 2", 30)
        assert pr.wait(10) == 1
        no_token(pr)
        d.check_states("on", ["svr1ev01", "svr1ev03"])
        assert_reported(pr, "done")
        assert_job(jid, "승인", "작업완료")
    finally:
        sim.close()


@d.scenario("cc: 확인자 취소 → exit 1, 중단")
def s_cc_cancel():
    sim, pr = to_wait(menu=False)
    try:
        jid = wait_job_id()
        tui("other", ["o " + jid, "c", OTHER_PW, "q"])
        pr.expect(r"\[취소\] 교차검증 미승인", 20)
        assert pr.wait(10) == 1
        assert "확인자 other 가 취소" in pr.buf, pr.buf[-600:]
        assert "[4/5]" not in pr.buf and "강제 PowerOff" not in pr.buf and "합계" not in pr.buf, "취소 후 진행:\n" + pr.buf[-600:]
        d.check_states("on", ["svr1ev01"])
        assert_not_reported(pr)
        assert_job(jid, "중단", reason="확인자 other 가 취소")
    finally:
        sim.close()


@d.scenario("cc: 붙여넣은 대상 불일치 → 중단")
def s_cc_mismatch():
    sim, pr = to_wait(menu=False)
    try:
        jid = wait_job_id()
        rc, out = tui("other", ["o " + jid, "a", "svr1ev99", "", OTHER_PW, "q"])
        assert "[승인 완료]" not in out, out
        pr.expect(r"\[취소\] 교차검증 미승인", 20)
        assert pr.wait(10) == 1
        assert "[4/5]" not in pr.buf and "합계" not in pr.buf, pr.buf[-600:]
        d.check_states("on", ["svr1ev01"])
        assert_not_reported(pr)
        assert_job(jid, "중단", reason_re=r"붙여넣은 대상 불일치")
    finally:
        sim.close()


@d.scenario("cc: 승인 후 4/5 에 yes → 작업중단")
def s_cc_lower_yes():
    sim, pr = to_wait(menu=False)
    try:
        jid = wait_job_id()
        approve(jid)
        pr.expect(r"교차검증 승인됨", 20)
        d.answer_yes(pr, ["svr1ev01"], "yes")
        pr.expect(r"\[취소\] YeS 미입력 \(입력값: yes\)", 10)
        assert pr.wait(10) == 1
        assert "[5/5]" not in pr.buf and "교차검증 통과" not in pr.buf and "합계" not in pr.buf, pr.buf[-600:]
        au = assert_reported(pr, "aborted")
        assert "교차검증 승인 확인자=other" in au and "교차검증 시작" not in au, "yes 인데 --start 를 부름:\n" + au
        d.check_states("on", ["svr1ev01"])
        assert_job(jid, "승인", "작업중단", reason="작업자가 본 작업 전에 중단")
    finally:
        sim.close()


@d.scenario("cc: 틀린 패스워드 거부 후 대기 유지 → 승인 → No → 작업중단")
def s_cc_wrongpw():
    sim, pr = to_wait(menu=False)
    try:
        jid = wait_job_id()
        rc, out = tui("other", ["o " + jid, "a", "svr1ev01", "", "틀린비번", "", "q"])
        assert "인증 실패" in out, out
        time.sleep(1.0)
        assert "교차검증 승인됨" not in pr.buf
        assert_job(jid, "대기")
        approve(jid)
        pr.expect(r"교차검증 승인됨", 20)
        d.answer_yes(pr, ["svr1ev01"], "No")
        pr.expect(r"\[취소\] YeS 미입력 \(입력값: No\)", 10)
        assert pr.wait(10) == 1
        d.check_states("on", ["svr1ev01"])
        assert_reported(pr, "aborted")
        assert_job(jid, "승인", "작업중단", reason="작업자가 본 작업 전에 중단")
        no_token(pr)
    finally:
        sim.close()


@d.scenario("cc: 승인→YeS→카운트다운 중 SIGINT → 작업중단")
def s_cc_sigint_countdown():
    sim, pr = to_wait(menu=False)
    try:
        jid = wait_job_id()
        to_countdown(pr, jid)
        pr.expect(r" \.\. 8", 10)
        pr.p.send_signal(signal.SIGINT)
        pr.expect(r"\[취소\] Ctrl\+C 로 중단", 10)
        assert pr.wait(20) == 1
        assert "교차검증 통과" not in pr.buf and "강제 PowerOff" not in pr.buf and "합계" not in pr.buf, pr.buf[-800:]
        au = assert_reported(pr, "aborted")
        assert "교차검증 시작" not in au, "카운트다운 중 Ctrl+C 인데 --start 를 부름:\n" + au
        d.check_states("on", ["svr1ev01"])
        j = assert_job(jid, "승인", "작업중단", reason="작업자가 본 작업 전에 중단")
        assert not j.get("started_at"), j
        no_token(pr)
    finally:
        sim.close()


@d.scenario("cc: 본 작업 중(강제OFF 질문) SIGINT → 작업중단")
def s_cc_sigint_running():
    sim, pr = to_wait(menu=False)
    try:
        jid = wait_job_id()
        to_countdown(pr, jid)
        pr.expect(r"교차검증 통과 — 전원 작업을 시작합니다", 20)
        pr.expect(r"강제 PowerOff 하시겠습니까\? \(YeS/No\): ", 40)
        assert_job(jid, "승인", "작업중")
        pr.p.send_signal(signal.SIGINT)
        pr.expect(r"Ctrl\+C 로 중단했습니다", 20)
        assert pr.wait(20) == 1
        assert_reported(pr, "aborted")
        j = assert_job(jid, "승인", "작업중단", reason="작업자가 작업을 중단")
        assert j.get("started_at") and j.get("finished_at"), j
        no_token(pr)
    finally:
        sim.close()


@d.scenario("cc: 4/5 에서 승인 유효시간(3초) 초과 → --start 거부, 중단")
def s_cc_expire():
    daemon_start(3)
    sim, pr = to_wait(menu=False)
    try:
        jid = wait_job_id()
        approve(jid)
        pr.expect(r"교차검증 승인됨", 20)
        pr.expect(d.YES_PROMPT, 10)
        time.sleep(4.5)  # 4/5 에서 머무는 사이 승인 유효시간(3초)이 지난다
        pr.send("ls\n")  # ls 는 여전히 취소 없이 목록만
        pr.expect(r"총 1대\n", 10)
        pr.expect(d.YES_PROMPT, 10)
        pr.send("YeS\n")
        pr.expect(r"10 \.\. 9 .* \.\. 1\n", 20)
        pr.expect(r"\[취소\] 교차검증 승인 유효시간 초과 또는 거부", 20)
        assert pr.wait(10) == 1
        assert "교차검증 통과" not in pr.buf and "강제 PowerOff" not in pr.buf and "합계" not in pr.buf, pr.buf[-800:]
        assert "사유: " in pr.buf, "거부 사유 한 줄이 없음:\n" + pr.buf[-800:]
        assert "교차검증 시작 거부 확인자=other" in new_audit(pr.audit_before)
        assert_not_reported(pr)  # 데몬이 이미 중단으로 기록 → --finish 를 보내지 않는다
        no_token(pr)
        d.check_states("on", ["svr1ev01"])
        assert_job(jid, "중단", reason_re=r"^승인 후 .+ 내 미진행$")
    finally:
        sim.close()
        daemon_start(120)


@d.scenario("cc: 데몬 없음 → 3/5 에서 취소, 전원 작업 없음")
def s_cc_nodaemon():
    daemon_stop()
    sim = d.Sim("on")
    try:
        d.write_conf(sim.addr, "cross_check_bin=%s\n" % CC, cc="on")
        d.write_targets(["svr1ev01"])
        pr = d.Proc([d.BIN, "-u", "tester", "-v", sim.addr, "-c", "other"])
        d.login(pr, 1)
        d.through_code(pr, ["svr1ev01"])
        pr.expect(r"\[취소\] 교차검증 미승인", 30)
        assert pr.wait(10) == 1
        assert "[4/5]" not in pr.buf and "강제 PowerOff" not in pr.buf
        d.check_states("on", ["svr1ev01"])
    finally:
        sim.close()


def main():
    try:
        daemon_start(120)
        register()
        for fn in [s_cc_approve, s_cc_vm_fail_done, s_cc_cancel, s_cc_mismatch, s_cc_lower_yes, s_cc_wrongpw,
                   s_cc_sigint_countdown, s_cc_sigint_running, s_cc_expire, s_cc_nodaemon]:
            fn()
    finally:
        daemon_stop()
        shutil.rmtree(ISO, ignore_errors=True)
    fails = [n for n, ok in results if not ok]
    print("\nCC E2E PASS=%d FAIL=%d" % (len(results) - len(fails), len(fails)))
    sys.exit(1 if fails else 0)


if __name__ == "__main__":
    main()
