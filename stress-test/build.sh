#!/bin/bash
# stress-test 빌드 (표준 라이브러리만 사용, 정적 바이너리)
cd "$(dirname "${BASH_SOURCE[0]}")" || exit 1
CGO_ENABLED=0 go build -o stress-test . && echo "[✔] 빌드 완료: $(pwd)/stress-test"
