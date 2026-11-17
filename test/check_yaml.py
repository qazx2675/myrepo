#!/usr/bin/env python3
# PyYAML(AWX 와 같은 파서)이 awx_copy v2 블록의 chunk_text 를 기대 값으로 읽는지 확인한다.
# 사용: AWX_COPY_DUMP=/tmp/d go test -run TestDumpForPyYAML . && python3 test/check_yaml.py /tmp/d
import glob, json, os, sys

import yaml

# PyYAML 5 미만은 U+10000 이상 문자를 거부한다. AWX 24.6.1 은 받는다(실측). 구버전이면 그런 파일은 건너뛴다.
old = int(yaml.__version__.split(".")[0]) < 5
d = sys.argv[1]
ok = bad = skip = 0
for y in sorted(glob.glob(os.path.join(d, "*.yaml"))):
    with open(y, encoding="utf-8", newline="") as f:
        src = f.read()
    if old and any(ord(c) > 0xFFFF for c in src):
        skip += 1
        continue
    doc = yaml.safe_load(src)
    with open(y[:-5] + ".json", encoding="utf-8") as f:
        exp = json.load(f)
    got = doc["awx_copy"]["chunk_text"]
    if got == exp:
        ok += 1
    else:
        bad += 1
        print("DIFF", os.path.basename(y), repr(got)[:120], repr(exp)[:120])
print("pyyaml %s ok=%d bad=%d skip(U+10000 이상, 구버전)=%d" % (yaml.__version__, ok, bad, skip))
sys.exit(1 if bad else 0)
