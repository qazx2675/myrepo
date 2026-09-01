#!/usr/bin/env bash
#
# 자산 조사 실행 래퍼 (B 서버에서 실행)
#
#   ./run_survey.sh
#
# 흐름:
#   1) conf [asset_filter] 로 원본 표1(source)을 걸러 [input].asset_file 생성
#      (source 가 비어 있으면 필터 없이 기존 asset_file 사용)
#   2) B 서버에서 survey 실행 (전체 대상)
#   3) 결과에서 '타임아웃' / '접속불가' 호스트 추출 (DNS 미등록은 제외)
#   4) conf [server_a].enabled = true 면 그 목록을 A 서버로 보내 1회 재조사
#   5) B + A 결과를 hostname 기준으로 병합. 재조사분은 A 값으로 교체한다.
#      -> result_YYYYMMDD_HHMM.tsv (+ _sdc_ / _vm_) 를 이 폴더에 생성.
#      모든 결과 파일에 hostname 중복 없음.
#
# 설정은 conf/conf.toml 하나만 사용한다.
#
set -euo pipefail

# ── 디버그 모드 ───────────────────────────────────────────────────────────
#   ./run_survey.sh --debug   또는   DEBUG=1 ./run_survey.sh
#   - 각 단계의 값/경로/명령을 [debug] 로 출력
#   - 임시 작업 디렉토리와 A 실행폴더를 지우지 않고 남긴다
DEBUG="${DEBUG:-0}"
for a in "$@"; do
  case "$a" in
    --debug|-d) DEBUG=1 ;;
    *) echo "[error] 알 수 없는 인자: $a (사용법: $0 [--debug])" >&2; exit 1 ;;
  esac
done
dbg() { (( DEBUG )) && echo "[debug] $*" >&2 || true; }

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
BIN="${SCRIPT_DIR}/survey"
CONF="${SCRIPT_DIR}/conf/conf.toml"
TS="$(date +%Y%m%d_%H%M)"

[[ -x "$BIN" ]]  || { echo "[error] 바이너리 없음: $BIN" >&2
                      echo "        빌드: (cd \"$SCRIPT_DIR\" && go build -o survey ./cmd/survey)" >&2; exit 1; }
[[ -f "$CONF" ]] || { echo "[error] 설정 파일 없음: $CONF" >&2; exit 1; }

# ── conf 읽기 (Go 가 무시하는 bash 전용 섹션 포함) ──────────────────────────
conf_get() { # <section> <key>
  awk -v s="[$1]" -v k="$2" '
    /^[[:space:]]*#/            { next }
    /^[[:space:]]*\[/           { cur=$0; sub(/[[:space:]]+$/,"",cur); next }
    cur==s && $0 ~ ("^[[:space:]]*" k "[[:space:]]*=") {
      line=$0
      sub(/^[^=]*=[[:space:]]*/,"",line)
      if      (line ~ /^"/)  { sub(/^"/,"",line);  sub(/".*/,"",line) }
      else if (line ~ /^'\''/) { sub(/^'\''/,"",line); sub(/'\''.*/,"",line) }
      else                  { sub(/[[:space:]]*#.*/,"",line); sub(/[[:space:]]+$/,"",line) }
      print line; exit
    }' "$CONF"
}

ASSET_FILE="$(conf_get input asset_file)"
F_SOURCE="$(conf_get asset_filter source)"
F_INCLUDE="$(conf_get asset_filter include)"
F_EXCLUDE="$(conf_get asset_filter exclude)"
A_ENABLED="$(conf_get server_a enabled)"
A_HOST="$(conf_get server_a host)"
A_USER="$(conf_get server_a user)"
A_DIR="$(conf_get server_a dir)"
A_BIN="$(conf_get server_a bin)"; A_BIN="${A_BIN:-survey}"
A_GOSSH="$(conf_get server_a gossh_bin)"
A_USER="${A_USER:-root}"

[[ -n "$ASSET_FILE" ]] || { echo "[error] conf [input].asset_file 이 필요합니다" >&2; exit 1; }

dbg "conf              = $CONF"
dbg "[input].asset_file= $ASSET_FILE"
dbg "[gossh].bin       = $(conf_get gossh bin)   (B 서버용)"
dbg "[asset_filter].source = ${F_SOURCE:-(없음 → 필터 생략)}"
dbg "[server_a].enabled= ${A_ENABLED:-(없음)}"
dbg "[server_a].host   = ${A_HOST:-(없음)}"
dbg "[server_a].user   = $A_USER"
dbg "[server_a].dir    = ${A_DIR:-(없음)}"
dbg "[server_a].bin    = $A_BIN"
dbg "[server_a].gossh_bin = ${A_GOSSH:-(없음 → B 의 [gossh].bin 사용)}"

WORK="$(mktemp -d "${TMPDIR:-/tmp}/survey.XXXXXX")"
ok=0
finish() {
  if (( ok )) && ! (( DEBUG )); then rm -rf "$WORK"
  else echo "[info] 작업 디렉토리 보존: $WORK" >&2; fi
}
trap finish EXIT
dbg "작업 디렉토리     = $WORK"

# ── 1) 자산 필터링 ────────────────────────────────────────────────────────
if [[ -n "$F_SOURCE" ]]; then
  [[ -f "$F_SOURCE" ]] || { echo "[error] [asset_filter].source 없음: $F_SOURCE" >&2; exit 1; }
  t="$WORK/asset.filtered"
  cp "$F_SOURCE" "$t"
  if [[ -n "$F_INCLUDE" ]]; then grep -E  -- "$F_INCLUDE" "$t" > "$t.x" || true; mv "$t.x" "$t"; fi
  if [[ -n "$F_EXCLUDE" ]]; then grep -vE -- "$F_EXCLUDE" "$t" > "$t.x" || true; mv "$t.x" "$t"; fi
  [[ -s "$t" ]] || { echo "[error] 필터 결과가 비었습니다 (include/exclude 확인)" >&2; exit 1; }
  mkdir -p "$(dirname "$ASSET_FILE")"
  mv "$t" "$ASSET_FILE"
  echo "[info] 필터 결과 $(wc -l < "$ASSET_FILE")행 -> $ASSET_FILE" >&2
else
  [[ -f "$ASSET_FILE" ]] || { echo "[error] 자산 파일 없음: $ASSET_FILE" >&2; exit 1; }
  echo "[info] [asset_filter].source 미설정 — 필터 없이 $ASSET_FILE 사용" >&2
fi

# ── 2) B 조사 ────────────────────────────────────────────────────────────
B_OUT="$WORK/b"; mkdir -p "$B_OUT"
( cd "$B_OUT" && "$BIN" )
shopt -s nullglob; B_FILES=( "$B_OUT"/result_*.tsv ); shopt -u nullglob
(( ${#B_FILES[@]} )) || { echo "[error] B 조사 결과 파일이 없습니다" >&2; exit 1; }
B_MAIN=""
for f in "${B_FILES[@]}"; do
  case "$(basename "$f")" in
    result_sdc_*|result_vm_*) ;;
    *) B_MAIN="$f" ;;
  esac
done
[[ -n "$B_MAIN" ]] || { echo "[error] B 메인 결과 파일을 찾지 못했습니다" >&2; exit 1; }

# ── 3) 재조사 대상 (타임아웃/접속불가, DNS 미등록 제외) ────────────────────
TO_HOSTS="$WORK/to_hosts.txt"
awk -F'\t' 'NR>1 { n=$7
  if (n ~ /(^|; )타임아웃(;|$)/ || n ~ /(^|; )접속불가(;|$)/) print $1
}' "$B_MAIN" | sort -u > "$TO_HOSTS"
N_TO=$(wc -l < "$TO_HOSTS")

# B 결과의 특이사항 분포 — 재조사 대상이 0대일 때 왜 그런지 바로 보이게 한다
echo "[info] B 특이사항 분포:" >&2
awk -F'\t' 'NR>1 { print ($7=="" ? "(정상)" : $7) }' "$B_MAIN" \
  | sort | uniq -c | sort -rn | head -15 | sed 's/^/         /' >&2

echo "[info] A 재조사 대상 ${N_TO}대 (타임아웃/접속불가만, DNS 미등록 제외)" >&2
dbg "B 메인 결과 파일  = $B_MAIN"
dbg "재조사 호스트 목록 = $TO_HOSTS"
(( DEBUG && N_TO > 0 )) && head -5 "$TO_HOSTS" | sed 's/^/[debug]   /' >&2

TO_ASSETS="$WORK/timeout_assets.txt"
: > "$TO_ASSETS"
if (( N_TO > 0 )); then
  # 원본 표1 행(자산ID<TAB>hostname<TAB>상태<TAB>위치)으로 복원해서 넘긴다.
  # 구분자는 survey(asset.go)와 동일하게 판단한다: 탭이 있으면 탭, 없으면 공백.
  awk 'NR==FNR { h[$1]=1; next }
       {
         line=$0
         sub(/^[[:space:]]+/,"",line); sub(/[[:space:]]+$/,"",line)
         if (line=="" || line ~ /^#/) next
         if (index(line,"\t")) split(line, f, "\t"); else split(line, f, /[ \t]+/)
         g=f[2]; gsub(/^[[:space:]]+|[[:space:]]+$/,"",g)
         if (g in h) print
       }' "$TO_HOSTS" "$ASSET_FILE" > "$TO_ASSETS"
  N_MATCH=$(wc -l < "$TO_ASSETS")
  if (( N_MATCH < N_TO )); then
    echo "[warn] 재조사 대상 ${N_TO}대 중 ${N_MATCH}대만 자산 파일에서 찾았습니다" >&2
    echo "       $ASSET_FILE 의 2번째 열이 hostname 인지 확인하세요" >&2
  fi
fi

# ── 4) A 조사 ────────────────────────────────────────────────────────────
# 전제: A·B 가 [server_a].dir 을 auto mount 로 공유(같은 절대경로). 파일은 직접
# 만들고, 대상망 접근이 되는 A 에서 '실행'만 ssh 로 한다. (scp 불필요)
A_OUT="$WORK/a"; mkdir -p "$A_OUT"
if [[ "$A_ENABLED" == "true" && -s "$TO_ASSETS" ]]; then
  : "${A_HOST:?[server_a].host 필요}" "${A_DIR:?[server_a].dir 필요}"
  SSH=(ssh -o BatchMode=yes -o StrictHostKeyChecking=accept-new -o ConnectTimeout=10)
  SRC_BIN="${A_DIR}/${A_BIN}"
  [[ -e "$SRC_BIN" ]]              || echo "[warn] A 바이너리 없음: $SRC_BIN" >&2
  [[ ! -e "$SRC_BIN" || -x "$SRC_BIN" ]] \
                                   || echo "[warn] A 바이너리에 실행권한 없음: $SRC_BIN (chmod +x 필요)" >&2
  [[ -f "${A_DIR}/conf/conf.toml" ]] || echo "[warn] A conf 없음: ${A_DIR}/conf/conf.toml" >&2
  if [[ -x "$SRC_BIN" && -f "${A_DIR}/conf/conf.toml" ]]; then
    # dir 아래 임시 실행폴더: 재조사 전용 conf([input].asset_file 만 교체) + 바이너리 사본
    A_RUN="$(mktemp -d "${A_DIR}/.resurvey.XXXXXX")"
    mkdir -p "${A_RUN}/conf"
    cp -p "$SRC_BIN" "${A_RUN}/${A_BIN}"
    [[ -d "${A_DIR}/scripts" ]] && cp -rp "${A_DIR}/scripts" "${A_RUN}/"
    cp "$TO_ASSETS" "${A_RUN}/resurvey_list.txt"
    # A 전용 conf: [input].asset_file 은 재조사 목록으로, [gossh].bin 은
    # gossh_bin 이 설정돼 있으면 A 쪽 경로로 바꾼다. 나머지는 B conf 그대로.
    awk -v af="${A_RUN}/resurvey_list.txt" -v gb="$A_GOSSH" '
      /^[[:space:]]*\[/ { sec=$0; sub(/^[[:space:]]+/,"",sec); sub(/[[:space:]]+$/,"",sec) }
      sec=="[input]" && /^[[:space:]]*asset_file[[:space:]]*=/ { print "asset_file = \"" af "\""; next }
      gb!="" && sec=="[gossh]" && /^[[:space:]]*bin[[:space:]]*=/ { print "bin = \"" gb "\""; next }
      { print }
    ' "${A_DIR}/conf/conf.toml" > "${A_RUN}/conf/conf.toml"
    rm -f "${A_RUN}"/result_*.tsv

    dbg "A 실행폴더        = $A_RUN"
    dbg "A conf 확인:"
    (( DEBUG )) && grep -nE '^[[:space:]]*(bin|asset_file)[[:space:]]*=' "${A_RUN}/conf/conf.toml" \
                   | sed 's/^/[debug]   /' >&2
    dbg "A 재조사 목록     = $(wc -l < "${A_RUN}/resurvey_list.txt")행"

    # A 에 gossh 가 실제로 있는지 먼저 확인 (없으면 전 호스트 'gossh 실행 실패')
    A_GOSSH_EFF="${A_GOSSH:-$(conf_get gossh bin)}"
    if ! "${SSH[@]}" "${A_USER}@${A_HOST}" "test -x '${A_GOSSH_EFF}'"; then
      echo "[warn] A 서버에 gossh 가 없거나 실행권한 없음: ${A_GOSSH_EFF}" >&2
      echo "       [server_a].gossh_bin 을 A 기준 절대경로로 지정하세요" >&2
    else
      dbg "A gossh 확인 OK   = $A_GOSSH_EFF"
    fi

    dbg "A 실행 명령       = ssh ${A_USER}@${A_HOST} \"cd '${A_RUN}' && ./'${A_BIN}'\""
    if "${SSH[@]}" "${A_USER}@${A_HOST}" "cd '${A_RUN}' && ./'${A_BIN}'" >&2; then
      shopt -s nullglob; A_FILES=( "${A_RUN}"/result_*.tsv ); shopt -u nullglob
      (( ${#A_FILES[@]} )) && cp "${A_FILES[@]}" "$A_OUT/"
      echo "[info] A 결과 파일 ${#A_FILES[@]}개 수집" >&2
    else
      echo "[warn] A 실행 실패 — B 결과만으로 진행" >&2
    fi
    if (( DEBUG )); then echo "[info] A 실행폴더 보존: $A_RUN" >&2; else rm -rf "$A_RUN"; fi
  else
    echo "[warn] 위 사유로 A 조사 생략 — B 결과만으로 진행" >&2
  fi
elif [[ "$A_ENABLED" == "true" ]]; then
  if (( N_TO > 0 )); then
    echo "[error] 재조사 대상 ${N_TO}대인데 자산 파일에서 한 행도 찾지 못했습니다 — A 생략" >&2
    echo "        $ASSET_FILE 의 2번째 열이 hostname 인지 확인하세요" >&2
  else
    echo "[info] 재조사 대상 없음 — A 생략" >&2
  fi
else
  echo "[info] [server_a].enabled != true — A 생략" >&2
fi

# ── 5) 병합 -> 최종 결과 파일 ────────────────────────────────────────────
OUT_MAIN="${SCRIPT_DIR}/result_${TS}.tsv"
OUT_SDC="${SCRIPT_DIR}/result_sdc_${TS}.tsv"
OUT_VM="${SCRIPT_DIR}/result_vm_${TS}.tsv"

shopt -s nullglob
ALL_B=( "$B_OUT"/result_*.tsv )
ALL_A=( "$A_OUT"/result_*.tsv )
shopt -u nullglob

# B 를 먼저(전체), 그다음 A(재조사 대상만) 를 인자로 넘긴다.
# hostname 키로 1행만 유지: B 행을 기본값으로 두고, 재조사 대상은 A 행으로 교체.
# 최종적으로 각 행의 출처 파일 종류(main/sdc/vm)에 따라 분리 저장 -> 파일 간 중복 없음.
awk -F'\t' -v OFS='\t' \
    -v nb="${#ALL_B[@]}" -v tofile="$TO_HOSTS" \
    -v om="$OUT_MAIN" -v os="$OUT_SDC" -v ov="$OUT_VM" '
  BEGIN{
    while ((getline h < tofile) > 0) { gsub(/[[:space:]]/,"",h); if (h!="") re[h]=1 }
  }
  function catof(fn,  b){ b=fn; sub(/.*\//,"",b)
    if (b ~ /^result_sdc_/) return "sdc"
    if (b ~ /^result_vm_/)  return "vm"
    return "main"
  }
  FNR==1 { fi++; src=(fi<=nb ? "B" : "A"); cat=catof(FILENAME); header=$0; next }
  {
    host=$1
    if (src=="B") {
      if (!(host in seen)) { seen[host]=1; ord[++n]=host; row[host]=$0; c[host]=cat }
    } else if (host in re) {
      if (!(host in seen)) { seen[host]=1; ord[++n]=host }
      row[host]=$0; c[host]=cat
    }
  }
  END{
    if (header=="") exit
    print header > om; print header > os; print header > ov
    for (i=1;i<=n;i++){ h=ord[i]
      f = (c[h]=="sdc") ? os : (c[h]=="vm") ? ov : om
      print row[h] > f
    }
  }
' "${ALL_B[@]}" ${ALL_A[@]+"${ALL_A[@]}"}

# 데이터가 없는(헤더뿐인) 부가 파일은 만들지 않는다
for f in "$OUT_SDC" "$OUT_VM"; do
  [[ -f "$f" ]] && (( $(wc -l < "$f") <= 1 )) && rm -f "$f"
done

echo "[info] 완료:" >&2
for f in "$OUT_MAIN" "$OUT_SDC" "$OUT_VM"; do
  [[ -f "$f" ]] && printf '  %s  (%d행)\n' "$f" "$(( $(wc -l < "$f") - 1 ))" >&2
done

ok=1
