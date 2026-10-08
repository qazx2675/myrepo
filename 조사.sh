#!/usr/bin/env bash
# 조사.sh — list.txt 의 호스트를 실제조사(out/<일시>/<host>.json)와 bios_json 으로 비교한다. (설정이름 존재 여부만)
#   list.txt 입력 -> bash 조사.sh -> results/<일시>/ 에 모델별 결과 저장
#   옵션은 그대로 bios_compare 에 전달: -in out/20261008_155548 -list list.txt -bios-json bios_json -ignore ignore.txt
cd "$(dirname "$0")"
[ -f list.txt ] || { echo "list.txt 가 없습니다 (hostname [model] 한 줄씩, list.txt.example 참고)"; exit 1; }
[ -d bios_json ] || { echo "bios_json 폴더가 없습니다"; exit 1; }

BIN=./bios_compare
if [ ! -x "$BIN" ]; then
  command -v go >/dev/null || { echo "bios_compare 도 go 도 없음 (bash setup.sh 로 빌드)"; exit 1; }
  bash setup.sh >/dev/null || exit 1
fi
exec "$BIN" "$@"
