#!/usr/bin/env bash
# setup.sh — 폐쇄망(오프라인) 빌드. vendor/ 안의 의존성만 사용, 인터넷 접속 시도 안 함.
# 이 도구 자체는 affinity_setting/lpage_setting/power_setting 세 외부 바이너리를
# 실행 시점에 호출한다 (기본 경로: 같은 디렉토리의 ./affinity_setting 등, -affinityTool/
# -lpageTool/-powerTool로 재정의 가능) — 이 setup.sh는 vm-param-fix 오케스트레이터
# 본체만 빌드한다. affinity_setting/lpage_setting은 각각 ../affinity_setting-source/,
# ../lpage_setting-source/ 의 setup.sh로 별도 빌드해야 한다.
set -euo pipefail
cd "$(dirname "$0")"
go build -mod=vendor -o vm-param-fix .
echo "빌드 완료: $(pwd)/vm-param-fix"
