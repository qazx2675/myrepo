#!/usr/bin/env python3
# PyYAML(AWX 와 같은 파서)이 awx_copy v2 블록의 chunk_text 를 기대 값으로 읽는지 확인한다.
# 사용: AWX_COPY_DUMP=/tmp/d go test -run TestDumpForPyYAML . && python3 test/check_yaml.py /tmp/d
import glob, json, os, sys

import yaml

d = sys.argv[1]
ok = bad = 0
for y in sorted(glob.glob(os.path.join(d, "*.yaml"))):
    with open(y, encoding="utf-8", newline="") as f:
        doc = yaml.safe_load(f.read())
    with open(y[:-5] + ".json", encoding="utf-8") as f:
        exp = json.load(f)
    got = doc["awx_copy"]["chunk_text"]
    if got == exp:
        ok += 1
    else:
        bad += 1
        print("DIFF", os.path.basename(y), repr(got)[:120], repr(exp)[:120])
print("pyyaml ok=%d bad=%d" % (ok, bad))
sys.exit(1 if bad else 0)
