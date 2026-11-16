#!/bin/bash
# pack_demo.sh - 테스트 서버(VM)에 복사해서 바로 체험할 수 있는 묶음(pm_monitor_demo.tar.gz)을 만든다.
# 사용법: bash demo/pack_demo.sh [출력폴더]      (기본: 저장소의 pm_monitor 폴더)
# 들어 있는 것: install_mn_os8(바이너리), demo/demo.sh, test/stub_gossh.sh, pm_monitor.conf.sample, 체험방법.txt
set -eu
HERE="$(cd "$(dirname "$(readlink -f "$0")")" && pwd)"
TOP="$(cd "$HERE/.." && pwd)"
OUT="${1:-$TOP}"
STAGE="$(mktemp -d)"
trap 'rm -rf "$STAGE"' EXIT
D="$STAGE/pm_monitor_demo"
mkdir -p "$D/demo" "$D/test"
for f in install_mn_os8 demo/demo.sh test/stub_gossh.sh pm_monitor.conf.sample 체험방법.txt; do
  [ -f "$TOP/$f" ] || { echo "없는 파일: $TOP/$f" >&2; exit 1; }
  cp "$TOP/$f" "$D/$f"
done
chmod +x "$D/install_mn_os8" "$D/demo/demo.sh" "$D/test/stub_gossh.sh"
tar -C "$STAGE" -czf "$OUT/pm_monitor_demo.tar.gz" pm_monitor_demo
echo "만들었습니다: $OUT/pm_monitor_demo.tar.gz"
