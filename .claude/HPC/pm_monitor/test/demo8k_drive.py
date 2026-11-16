#!/usr/bin/env python3
# demo8k_drive.py - demo.sh -n 8000 을 가상 터미널(120x40)에서 실행하고 키를 눌러 반응 시간과 패널 화면을 기록한다 (랩 검증용)
#
# 사용법: demo8k_drive.py <로그접두어> -- <명령> [인자...]
#   <로그접두어>.log   키별 반응 시간과 요약,  <로그접두어>.raw  화면 출력 전체,
#   <로그접두어>.snap  키마다 마지막으로 그려진 화면(ANSI 제거, 가시 텍스트)
import fcntl
import os
import pty
import re
import select
import struct
import sys
import termios
import time

pre = sys.argv[1]
cmd = sys.argv[sys.argv.index("--") + 1:]
pid, fd = pty.fork()
if pid == 0:
    os.environ["TERM"] = "xterm"
    os.execvp(cmd[0], cmd)
fcntl.ioctl(fd, termios.TIOCSWINSZ, struct.pack("HHHH", 40, 120, 0, 0))
raw = open(pre + ".raw", "wb")
log = open(pre + ".log", "w")
snap = open(pre + ".snap", "w")
start = time.time()
alive = True
tui_t = None
rounds = 0
lat = []
ANSI = re.compile(rb"\x1b\[[0-9;?]*[A-Za-z]")
allraw = bytearray()


def drain(wait):
    global alive, tui_t, rounds
    first, buf = None, b""
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
        allraw.extend(b)
        buf += b
        if tui_t is None and b"\x1b[?1049h" in b:
            tui_t = time.time() - start
        for m in re.finditer(rb"\xed\x9a\x8c\xec\xb0\xa8 (\d+)", b):
            rounds = max(rounds, int(m.group(1)))
    return first, buf


def send(label, data, wait=0.6, shot=False):
    t0 = time.time()
    os.write(fd, data)
    first, buf = drain(wait)
    ms = None if first is None else (first - t0) * 1000
    lat.append((label, ms))
    log.write("t=%5.1fs key %-8s -> %s\n" % (t0 - start, label, "반응 없음" if ms is None else "%.0f ms" % ms))
    log.flush()
    if shot:
        parts = bytes(allraw[-300000:]).split(b"\x1b[H")
        last = parts[-1] if parts[-1].endswith(b"\x1b[J") or len(parts) < 2 else parts[-2]
        txt = ANSI.sub(b"", last).decode("utf-8", "replace")
        snap.write("===== %s (t=%.0fs) =====\n%s\n" % (label, t0 - start, txt.replace("\r\n", "\n").replace("\r", "\n")))
        snap.flush()


def wait_until(t):
    left = t - (time.time() - start)
    if left > 0:
        drain(left)


PGDN, END, HOME, ESC = b"\x1b[6~", b"\x1b[F", b"\x1b[H", b"\x1b"
DOWN, UP = b"\x1b[B", b"\x1b[A"
# 안내 화면은 -y 라 바로 TUI 가 뜬다. 첫 화면까지 기다린다.
while tui_t is None and time.time() - start < 120:
    drain(0.2)
log.write("TUI 첫 화면까지 %.1f초 (demo.sh 시작부터: 호스트 생성·/etc/hosts·lo 주소 포함)\n" % (tui_t or -1))
drain(4)
send("first", b"", 0.1, shot=True)
if os.environ.get("D8K_SCAN"):
    # 훑기 모드: 완료 서버 30초 뒤 l 패널을 끝까지 넘겨 FAIL·OFF·다름 줄을 .raw 에 남긴다
    wait_until(100)
    send("l", b"l", 25, shot=True)
    for _ in range(110):
        send("pgdn", PGDN, 0.12)
    send("end", END, 0.5, shot=True)
    send("q(l)", b"q", 0.5)
    send("q-quit", b"q", 3)
    sys.exit(0)
for i in range(30):
    send("pgdn", PGDN, 0.25, shot=(i == 29))
send("end", END, 0.5, shot=True)
send("up", UP, 0.3)
send("home", HOME, 0.5, shot=True)
send("/", b"/", 0.4)
for ch in b"h0123":
    send("type", bytes([ch]), 0.2)
send("enter", b"\r", 0.5, shot=True)
send("esc", ESC, 0.5, shot=True)
send("0", b"0", 0.5)
send("3", b"3", 0.6, shot=True)
send("0", b"0", 0.6)
wait_until(95)  # 지연(60초 뒤)이 생긴 뒤
send("c", b"c", 25, shot=True)  # 완료 서버 ping 이 끝나길 기다린다
send("down", DOWN, 0.4)
send("pgdn", PGDN, 0.5, shot=True)
send("q(c)", b"q", 0.5, shot=True)
send("r", b"r", 12, shot=True)
send("pgdn", PGDN, 0.5, shot=True)
send("q(r)", b"q", 0.5)
send("d", b"d", 3, shot=True)
send("q(d)", b"q", 0.5)
send("l", b"l", 20, shot=True)
send("pgdn", PGDN, 0.5, shot=True)
send("q(l)", b"q", 0.5)
wait_until(190)  # 90초 OFF 서버 꺼짐, 150초 다시 켜짐 이후
send("c", b"c", 25, shot=True)
send("q(c)", b"q", 0.5)
send("l", b"l", 10, shot=True)
send("q(l)", b"q", 0.5)
wait_until(240)
send("q-quit", b"q", 3)
code = None
end = time.time() + 20
while time.time() < end:
    drain(0.2)
    p, st = os.waitpid(pid, os.WNOHANG)
    if p:
        code = os.waitstatus_to_exitcode(st) if hasattr(os, 'waitstatus_to_exitcode') else (st >> 8 if os.WIFEXITED(st) else -(st & 0x7f))
        break
if code is None:
    log.write("종료 안 함: q 후 20초 지나도 살아 있음 (정리는 따로)\n")
ms = [m for _, m in lat if m is not None]
log.write("--- 요약 ---\n실행 %.0f초, 종료코드 %s, 마지막 본 회차 %d\n" % (time.time() - start, code, rounds))
log.write("키 %d개, 반응 %d개, 최대 %.0f ms, 평균 %.0f ms, 100ms 초과 %d개\n" % (
    len(lat), len(ms), max(ms) if ms else 0, sum(ms) / len(ms) if ms else 0, len([m for m in ms if m > 100])))
log.write("반응 없음: %s\n" % [l for l, m in lat if m is None])
log.close()
raw.close()
snap.close()
