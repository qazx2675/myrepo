#!/usr/bin/env python3
# pty_drive.py - install_mn 을 가상 터미널(120x40, TERM=xterm)에서 실행하고 키를 보내는 시험 도구 (랩 부하 시험용)
#
# 사용법: pty_drive.py <초> <로그> <pid파일> <키 모드> -- <명령> [인자...]
#   키 모드 load : 30초마다 스크롤·검색·필터·c·r·d·l 을 돌려 가며 누르고 끝에 q
#   키 모드 none : 키 없이 <초> 동안만 둔 뒤 q
# 로그: 화면 출력 전체(<로그>.raw), 키별 반응 시간과 요약(<로그>)
import os
import pty
import re
import select
import struct
import sys
import termios
import fcntl
import time

secs = int(sys.argv[1])
logp, pidp, mode = sys.argv[2], sys.argv[3], sys.argv[4]
cmd = sys.argv[sys.argv.index("--") + 1:]

pid, fd = pty.fork()
if pid == 0:
    os.environ["TERM"] = "xterm"
    os.execvp(cmd[0], cmd)
fcntl.ioctl(fd, termios.TIOCSWINSZ, struct.pack("HHHH", 40, 120, 0, 0))
with open(pidp, "w") as f:
    f.write(str(pid))

raw = open(logp + ".raw", "wb")
log = open(logp, "w")
start = time.time()
frames = 0
maxround = 0
lastbytes = b""
alive = True


def drain(wait):
    """wait 초까지 출력을 읽는다. 처음 데이터가 온 시각(없으면 None)을 돌려준다."""
    global frames, maxround, alive, lastbytes
    first = None
    end = time.time() + wait
    while True:
        left = end - time.time()
        if left <= 0:
            break
        r, _, _ = select.select([fd], [], [], min(left, 0.05))
        if not r:
            continue
        try:
            b = os.read(fd, 65536)
        except OSError:
            alive = False
            break
        if not b:
            alive = False
            break
        if first is None:
            first = time.time()
        raw.write(b)
        frames += b.count(b"\x1b[H")
        lastbytes = (lastbytes + b)[-4096:]
        for m in re.finditer(rb"\xed\x9a\x8c\xec\xb0\xa8 (\d+)", b):  # "회차 N"
            maxround = max(maxround, int(m.group(1)))
    return first


lat = []


def send(label, data, wait=1.0):
    t0 = time.time()
    os.write(fd, data)
    first = drain(wait)
    ms = None if first is None else (first - t0) * 1000
    lat.append((label, ms))
    log.write("t=%4.0fs key %-10s -> %s\n" % (t0 - start, label, "반응 없음(화면 변화 없음)" if ms is None else "%.0f ms" % ms))
    log.flush()


DOWN, UP, PGDN, HOME, END = b"\x1b[B", b"\x1b[A", b"\x1b[6~", b"\x1b[H", b"\x1b[F"
cycle = 0
drain(3)
while alive and time.time() - start < secs - (65 if mode == "load" else 5):  # 사이클 하나가 약 60초
    if mode == "load":
        c0 = time.time()
        cycle += 1
        for i in range(5):
            send("down", DOWN, 0.4)
        send("pgdn", PGDN, 0.5)
        send("end", END, 0.5)
        send("up", UP, 0.5)
        send("home", HOME, 0.5)
        send("/", b"/", 0.5)
        for ch in b"127.1.1":
            send("type", bytes([ch]), 0.2)
        send("enter", b"\r", 0.5)
        send("/", b"/", 0.5)
        for _ in range(7):
            send("bs", b"\x7f", 0.2)
        send("enter", b"\r", 0.5)
        send("3", b"3", 0.5)
        send("0", b"0", 0.5)
        send("c", b"c", 0.8)
        drain(6)  # 완료 서버 ping
        send("c", b"c", 0.8)
        send("r", b"r", 0.5)
        send("r", b"r", 0.5)
        send("d", b"d", 0.8)
        send("d", b"d", 0.5)
        send("l", b"l", 0.8)
        drain(8)  # 첫 사이클은 gossh 점검이 있다
        send("l", b"l", 0.8)
        rest = 30 - (time.time() - c0)
        if rest > 0:
            drain(min(rest, max(secs - 5 - (time.time() - start), 0)))
    else:
        drain(1)
if alive:
    drain(max(secs - (time.time() - start), 0))
    send("q", b"q", 3)
# 종료 대기
code = None
end = time.time() + 10
while time.time() < end:
    drain(0.2)
    p, st = os.waitpid(pid, os.WNOHANG)
    if p:
        code = os.waitstatus_to_exitcode(st) if hasattr(os, "waitstatus_to_exitcode") else (st >> 8 if os.WIFEXITED(st) else -(st & 0x7f))
        break
if code is None:
    os.kill(pid, 9)
    os.waitpid(pid, 0)
    code = "강제종료(10초 안에 안 끝남)"
drain(0.2)
ms = [m for _, m in lat if m is not None]
log.write("--- 요약 ---\n")
log.write("실행 %.0f초, 종료코드 %s, 화면 그리기(\\x1b[H) %d번, 마지막 본 회차 %d\n" % (time.time() - start, code, frames, maxround))
log.write("키 %d개, 화면이 반응한 것 %d개, 반응 시간 최대 %.0f ms, 평균 %.0f ms, 100ms 초과 %d개\n" % (
    len(lat), len(ms), max(ms) if ms else 0, sum(ms) / len(ms) if ms else 0, len([m for m in ms if m > 100])))
log.write("터미널 복구 시퀀스(\\x1b[?1049l) 출력: %s\n" % ("예" if b"\x1b[?1049l" in lastbytes else "아니오"))
log.close()
raw.close()
