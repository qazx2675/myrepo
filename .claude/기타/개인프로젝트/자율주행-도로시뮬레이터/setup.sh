#!/usr/bin/env bash
# setup.sh — 폐쇄망(오프라인) 설치. wheels/ 안의 패키지만 사용, 인터넷 접속 시도 안 함.
set -euo pipefail
cd "$(dirname "$0")"

PY="${PYTHON:-python3}"
if ! command -v "$PY" >/dev/null 2>&1; then
  echo "오류: $PY 를 찾을 수 없습니다. Python 3.11 을 먼저 설치하십시오." >&2
  exit 2
fi
if ! "$PY" -c 'import sys; sys.exit(0 if sys.version_info[:2] == (3, 11) else 1)'; then
  echo "경고: wheels/ 는 Python 3.11 용입니다. 다른 버전이면 설치가 실패할 수 있습니다." >&2
fi

"$PY" -m venv .venv
.venv/bin/pip install --no-index --find-links wheels -r requirements.txt

# 설치 확인: 화면 없이 60초 자동주행 시뮬레이션
.venv/bin/python main.py --headless --seconds 60

echo
echo '완료. 다음으로 진행하십시오:'
echo '  1) .venv/bin/python main.py                 # 창 띄워서 운전'
echo '  2) .venv/bin/python tests/test_sim.py       # 기능 테스트'
