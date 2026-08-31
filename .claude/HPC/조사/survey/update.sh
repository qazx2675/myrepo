#!/usr/bin/env bash
#
# 폐쇄망 증분 업데이트
#
#   사용법: ./update.sh [대상디렉토리]
#     - 이 스크립트가 있는 디렉토리(= 새로 받은 버전)의 파일을
#       대상디렉토리(기본 /root/HPC/조사)로 "변경분만" 복사한다.
#     - 내용이 같은 파일은 건너뛴다. 새 파일은 추가한다.
#     - 아래 파일은 절대 건드리지 않는다(사용자 데이터/설정/결과):
#         conf/conf.toml, result_*.tsv, asset_list.txt, list.txt,
#         survey / survey-rhel6 (바이너리)
#     - 대상에만 있는 오래된 *.go 는 빌드 깨짐 방지를 위해 제거한다.
#
#   흐름 예:
#     tar xzf survey-new.tar.gz -C /tmp/survey-new
#     /tmp/survey-new/update.sh /root/HPC/조사
#     (cd /root/HPC/조사 && go build -o survey ./cmd/survey)
#
set -euo pipefail

SRC="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
DST="${1:-/root/HPC/조사}"

if [[ "$SRC" == "$DST" ]]; then
  echo "[error] 대상이 이 스크립트 위치와 같습니다. 새 버전을 다른 경로에 풀고 실행하세요." >&2
  exit 1
fi
if [[ ! -d "$DST" ]]; then
  echo "[error] 대상 디렉토리가 없습니다: $DST" >&2
  echo "        최초 설치라면 압축을 풀어 통째로 복사하세요." >&2
  exit 1
fi

preserve() {
  case "$1" in
    conf/conf.toml)            return 0 ;;
    result_*.tsv)              return 0 ;;
    asset_list.txt|list.txt)   return 0 ;;
    survey|survey.exe)         return 0 ;;
    survey-rhel6)              return 0 ;;  # A(RHEL6)용 바이너리
    update.sh)                 return 0 ;;  # 마지막에 따로 처리
  esac
  return 1
}

added=0 changed=0 removed=0

# 1) SRC -> DST : 새 파일 추가 / 변경 파일 덮어쓰기
cd "$SRC"
while IFS= read -r -d '' f; do
  rel="${f#./}"
  preserve "$rel" && continue
  dstf="$DST/$rel"
  if [[ ! -e "$dstf" ]]; then
    mkdir -p "$(dirname "$dstf")"
    cp -p "$f" "$dstf"
    echo "  + $rel"
    added=$((added + 1))
  elif ! cmp -s "$f" "$dstf"; then
    cp -p "$f" "$dstf"
    echo "  ~ $rel"
    changed=$((changed + 1))
  fi
done < <(find . -type f -not -path './.git/*' -print0)

# 1-b) conf 는 보존하지만, 배포본과 달라졌으면 알림 (수동 반영 필요)
conf_drift=0
if [[ -f "$SRC/conf/conf.toml" && -f "$DST/conf/conf.toml" ]] \
   && ! cmp -s "$SRC/conf/conf.toml" "$DST/conf/conf.toml"; then
  conf_drift=1
  echo "  ! conf/conf.toml 이 새 배포본과 다릅니다 (보존됨, 자동 반영 안 함)"
  echo "    차이:"
  diff -u "$DST/conf/conf.toml" "$SRC/conf/conf.toml" | sed 's/^/      /' || true
fi

# 2) 대상에만 있는 오래된 Go 소스 제거 (빌드 깨짐 방지)
cd "$DST"
while IFS= read -r -d '' f; do
  rel="${f#./}"
  if [[ ! -e "$SRC/$rel" ]]; then
    rm -f "$f"
    echo "  - $rel (오래된 소스 제거)"
    removed=$((removed + 1))
  fi
done < <(find ./cmd -type f -name '*.go' -print0 2>/dev/null || true)

# 3) update.sh 자신 갱신
if ! cmp -s "$SRC/update.sh" "$DST/update.sh" 2>/dev/null; then
  cp -p "$SRC/update.sh" "$DST/update.sh"
  echo "  ~ update.sh"
fi

echo ""
echo "완료: 추가 $added / 변경 $changed / 제거 $removed   (대상: $DST)"
echo "보존됨: conf/conf.toml, result_*.tsv, asset_list.txt, survey, survey-rhel6"
if (( conf_drift )); then
  echo "[주의] conf/conf.toml 변경분은 위 diff 를 보고 직접 반영하세요 (update.sh 는 conf 를 덮지 않음)."
fi
if (( added + changed + removed > 0 )); then
  echo "재빌드:  (cd \"$DST\" && go build -o survey ./cmd/survey)"
fi
