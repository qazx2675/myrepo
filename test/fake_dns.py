#!/usr/bin/env python3
# 테스트용 가짜 DNS 서버 (UDP). run_tests.sh 의 "실제 dig/nslookup" 케이스가 쓴다 — 01 의 DNS 점검이 두 도구의 진짜 출력을 해석하는지 확인하는 용도.
#
# 사용: python3 fake_dns.py <포트> <존파일> [<pid파일>]    (127.0.0.1 에서만 듣는다)
# 존파일 한 줄: "<이름> <A|PTR> <값>"   값 = ip[,ip..] (A) | 이름 (PTR) | NX (NXDOMAIN) | NODATA | SERVFAIL | DROP (응답 안 함)
#   목록에 없는 이름은 NXDOMAIN("*" A DROP 처럼 이름에 *를 쓰면 없는 이름의 기본 응답). 이름은 소문자·끝의 '.' 없이 적는다. '#' 으로 시작하는 줄은 주석.
import os
import socket
import struct
import sys

TYPES = {"A": 1, "PTR": 12}


def read_name(msg, off):
    labels = []
    while True:
        n = msg[off]
        if n == 0:
            return ".".join(labels).lower(), off + 1
        labels.append(msg[off + 1:off + 1 + n].decode("ascii", "replace"))
        off += 1 + n


def enc_name(name):
    out = b""
    for lab in name.strip(".").split("."):
        if lab:
            out += bytes([len(lab)]) + lab.encode("ascii")
    return out + b"\x00"


def load(path):
    zone = {}
    with open(path) as f:
        for line in f:
            line = line.strip()
            if not line or line.startswith("#"):
                continue
            name, typ, val = line.split(None, 2)
            zone[(name.lower().rstrip("."), TYPES[typ])] = val.strip()
    return zone


def main():
    port, zfile = int(sys.argv[1]), sys.argv[2]
    if len(sys.argv) > 3:
        with open(sys.argv[3], "w") as f:
            f.write(str(os.getpid()))
    zone = load(zfile)
    s = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
    s.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
    s.bind(("127.0.0.1", port))
    while True:
        msg, addr = s.recvfrom(512)
        if len(msg) < 12:
            continue
        qid = msg[:2]
        qn, off = read_name(msg, 12)
        qtype, qclass = struct.unpack("!HH", msg[off:off + 4])
        question = msg[12:off + 4]
        val = zone.get((qn, qtype), zone.get(("*", qtype), "NX"))
        if val == "DROP":
            continue
        rcode, answers = 0, b""
        if val == "NX":
            rcode = 3
        elif val == "SERVFAIL":
            rcode = 2
        elif val == "NODATA":
            pass
        elif qtype == 1:
            ips = val.split(",")
            for ip in ips:
                answers += b"\xc0\x0c" + struct.pack("!HHIH", 1, 1, 60, 4) + socket.inet_aton(ip)
        else:
            rd = enc_name(val)
            answers = b"\xc0\x0c" + struct.pack("!HHIH", 12, 1, 60, len(rd)) + rd
        an = 0 if rcode or val == "NODATA" else (len(val.split(",")) if qtype == 1 else 1)
        hdr = qid + struct.pack("!HHHHH", 0x8180 | rcode, 1, an, 0, 0)
        s.sendto(hdr + question + answers, addr)


if __name__ == "__main__":
    main()
