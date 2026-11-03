#!/usr/bin/env bash
# deploy_repo.sh - 관리서버에서 다수 RHEL VM(8.10 / 9.6)에 오프라인 로컬 yum repo를 구성한다.
# 패키지 설치는 하지 않으며, 설치 가능 여부만 dnf --assumeno 로 검증한다.
#
# 사용법: ./deploy_repo.sh [-f hosts_file] [-b bundle_dir] [-j parallel] [-p port]
set -euo pipefail

# ---- 기본값 (환경변수로 덮어쓰기 가능) ----
: "${HOSTS_FILE:=./hosts.txt}"
: "${BUNDLE_DIR:=./bundles}"
: "${PARALLEL:=10}"
: "${SSH_PORT:=8022}"
: "${SSH_USER:=root}"
: "${REPO_BASE:=/opt/localrepo}"
: "${LOG_DIR:=./logs}"
: "${GPGKEY:=file:///etc/pki/rpm-gpg/RPM-GPG-KEY-redhat-release}"
PKGS=(autofs nscd rpcbind sssd tcsh nfs-utils)

SUPPORTED_VERSIONS=(8.10 9.6)
SSH_OPTS=(
  -o StrictHostKeyChecking=no
  -o UserKnownHostsFile=/dev/null
  -o ConnectTimeout=10
  -o LogLevel=ERROR
  -o NumberOfPasswordPrompts=1
)

HOSTS=()
RESULT_DIR=""
RESULT_FILE=""

# ---- 공통 유틸 ----
log() { printf '[%s] %s\n' "$(date '+%H:%M:%S')" "$*"; }
err() { printf '오류: %s\n' "$*" >&2; }

usage() {
  cat <<'USAGE'
사용법: ./deploy_repo.sh [-f hosts_file] [-b bundle_dir] [-j parallel] [-p port]
  -f  대상 호스트 목록 파일 (기본 ./hosts.txt)
  -b  번들 디렉터리 (기본 ./bundles)
  -j  동시 처리 호스트 수 (기본 10)
  -p  SSH 포트 (기본 8022)
USAGE
}

parse_args() {
  local opt
  while getopts "f:b:j:p:h" opt; do
    case "$opt" in
      f) HOSTS_FILE=$OPTARG ;;
      b) BUNDLE_DIR=$OPTARG ;;
      j) PARALLEL=$OPTARG ;;
      p) SSH_PORT=$OPTARG ;;
      h) usage; exit 0 ;;
      *) usage >&2; exit 2 ;;
    esac
  done
  if ! [[ $PARALLEL =~ ^[1-9][0-9]*$ ]]; then
    err "-j 값은 1 이상의 정수여야 합니다: $PARALLEL"; exit 2
  fi
  if ! [[ $SSH_PORT =~ ^[0-9]+$ ]]; then
    err "-p 값은 숫자여야 합니다: $SSH_PORT"; exit 2
  fi
}

# ---- 사전 점검 ----
check_prereq() {
  local cmd
  for cmd in sshpass ssh scp sha256sum; do
    if ! command -v "$cmd" >/dev/null 2>&1; then
      err "필수 명령이 없습니다: $cmd"; exit 1
    fi
  done
  if (( BASH_VERSINFO[0] < 4 || (BASH_VERSINFO[0] == 4 && BASH_VERSINFO[1] < 3) )); then
    err "bash 4.3 이상이 필요합니다."; exit 1
  fi
}

# 호스트 목록 로드: 주석/빈 줄 제외, 공백/CR 제거, 중복 제거
load_hosts() {
  local line
  local -A seen=()
  if [[ ! -f $HOSTS_FILE ]]; then
    err "호스트 파일이 없습니다: $HOSTS_FILE"; exit 1
  fi
  while IFS= read -r line || [[ -n $line ]]; do
    line=${line//$'\r'/}
    line=${line#"${line%%[![:space:]]*}"}
    line=${line%"${line##*[![:space:]]}"}
    if [[ -z $line || $line == \#* ]]; then
      continue
    fi
    if [[ -z ${seen[$line]:-} ]]; then
      seen[$line]=1
      HOSTS+=("$line")
    fi
  done < "$HOSTS_FILE"
  if (( ${#HOSTS[@]} == 0 )); then
    err "대상 호스트가 없습니다: $HOSTS_FILE"; exit 1
  fi
}

bundle_path() { printf '%s/localrepo-rhel%s.tar.gz' "$BUNDLE_DIR" "$1"; }

# 번들 존재 확인 및 sha256 로컬 검증(1회)
check_bundles() {
  local ver tgz sha
  for ver in "${SUPPORTED_VERSIONS[@]}"; do
    tgz=$(bundle_path "$ver")
    sha="$tgz.sha256"
    if [[ ! -f $tgz ]]; then
      err "번들 파일이 없습니다: $tgz"; exit 1
    fi
    if [[ -f $sha ]]; then
      if ! (cd "$BUNDLE_DIR" && sha256sum -c "$(basename "$sha")" >/dev/null); then
        err "sha256 검증 실패: $tgz"; exit 1
      fi
      log "sha256 검증 OK: $(basename "$tgz")"
    else
      log "경고: sha256 파일이 없어 검증을 건너뜁니다: $sha"
    fi
  done
}

ask_password() {
  local pw=""
  read -r -s -p "대상 VM 공통 SSH 비밀번호: " pw
  echo >&2
  if [[ -z $pw ]]; then
    err "비밀번호가 비어 있습니다."; exit 1
  fi
  export SSHPASS=$pw
}

# ---- 원격 실행 래퍼 (비밀번호는 SSHPASS 환경변수로만 전달) ----
rssh() {
  local host=$1; shift
  sshpass -e ssh -p "$SSH_PORT" "${SSH_OPTS[@]}" "${SSH_USER}@${host}" "$@"
}

rscp() {
  sshpass -e scp -P "$SSH_PORT" "${SSH_OPTS[@]}" "$1" "${SSH_USER}@${2}:${3}"
}

# 원격에서 bash -s 로 스크립트(stdin)를 실행한다. 인자는 %q 로 안전하게 인용
remote_bash() {
  local host=$1 script=$2 args
  shift 2
  printf -v args '%q ' "$@"
  rssh "$host" "bash -s -- $args" <<<"$script"
}

# ---- 원격 스크립트: 번들 압축 해제 (멱등) ----
read -r -d '' REMOTE_EXTRACT <<'REMOTE_EXTRACT_EOF' || true
set -euo pipefail
VER=$1; BASE=$2; TARBALL=$3
trap 'rm -f "$TARBALL"' EXIT
mkdir -p "$BASE"
rm -rf "${BASE:?}/rhel${VER}"
tar -xzf "$TARBALL" -C "$BASE"
if [[ ! -f "$BASE/rhel${VER}/repodata/repomd.xml" ]]; then
  echo "[ERR] 압축 해제 후 repodata 를 찾을 수 없음: $BASE/rhel${VER}"
  exit 1
fi
echo "[원격] 압축 해제 완료: $BASE/rhel${VER}"
REMOTE_EXTRACT_EOF

# ---- 원격 스크립트: repo 구성 및 검증 (멱등) ----
read -r -d '' REMOTE_CONFIG <<'REMOTE_CONFIG_EOF' || true
set -euo pipefail
VER=$1; BASE=$2; GPGKEY=$3; PKGS_STR=$4
TS=$(date +%Y%m%d%H%M%S)
REPO_ID="localrepo-rhel${VER}"
SUDOERS_LINE='sudoers: ldap files'
NS=/etc/nsswitch.conf

log() { echo "[원격] $*"; }
die() { echo "[ERR] $*"; exit 1; }

# a. 기존 repo 파일을 /root 로 이동 (local.repo 제외)
move_old_repos() {
  local f name
  shopt -s nullglob
  for f in /etc/yum.repos.d/*.repo; do
    name=${f##*/}
    if [[ $name == local.repo ]]; then
      continue
    fi
    if [[ -e /root/$name ]]; then
      mv -f "/root/$name" "/root/$name.$TS"
    fi
    mv -f "$f" "/root/$name"
    log "repo 이동: $f -> /root/$name"
  done
  shopt -u nullglob
}

# b. local.repo 작성 (덮어쓰기)
write_local_repo() {
  printf '%s\n' \
    "[${REPO_ID}]" \
    "name=Local Repo RHEL ${VER}" \
    "baseurl=file://${BASE}/rhel${VER}" \
    "enabled=1" \
    "gpgcheck=1" \
    "gpgkey=${GPGKEY}" > /etc/yum.repos.d/local.repo
  chmod 644 /etc/yum.repos.d/local.repo
  log "local.repo 작성 완료"
}

# c. subscription-manager 플러그인 비활성화 (변경이 있을 때만 백업)
disable_plugin_file() {
  local f=$1 tmp
  tmp=$(mktemp)
  awk '
    function flush() { if (inmain && !done) { print "enabled=0"; done = 1 } }
    /^[[:space:]]*\[/ {
      flush()
      inmain = ($0 ~ /^[[:space:]]*\[main\][[:space:]]*$/)
      if (inmain) hasmain = 1
      print
      next
    }
    inmain && /^[[:space:]]*enabled[[:space:]]*=/ {
      if (!done) { print "enabled=0"; done = 1 }
      next
    }
    { print }
    END {
      flush()
      if (!hasmain) { print "[main]"; print "enabled=0" }
    }
  ' "$f" > "$tmp"
  if cmp -s "$f" "$tmp"; then
    log "플러그인 이미 비활성화됨: $f"
  else
    cp -p "$f" "$f.bak.$TS"
    cat "$tmp" > "$f"
    log "플러그인 비활성화: $f (백업 $f.bak.$TS)"
  fi
  rm -f "$tmp"
}

disable_subscription_plugin() {
  local c real found=0
  local -A seen=()
  for c in /etc/dnf/plugins/subscription-manager.conf /etc/yum/pluginconf.d/subscription-manager.conf; do
    if [[ ! -e $c ]]; then
      continue
    fi
    found=1
    real=$(readlink -f "$c")
    if [[ -n ${seen[$real]:-} ]]; then
      continue
    fi
    seen[$real]=1
    disable_plugin_file "$real"
  done
  if (( found == 0 )); then
    log "subscription-manager 플러그인 설정 파일 없음 (건너뜀)"
  fi
}

# d. nsswitch.conf 의 sudoers 줄을 마지막 줄 하나로 정리
fix_nsswitch() {
  local last cnt tmp
  last=$(grep -v '^[[:space:]]*$' "$NS" | tail -n 1 || true)
  cnt=$(grep -c '^[[:space:]]*sudoers[[:space:]]*:' "$NS" || true)
  if [[ $last == "$SUDOERS_LINE" && $cnt == 1 ]]; then
    log "nsswitch.conf 이미 올바름 (변경 없음)"
    return 0
  fi
  cp -p "$NS" "$NS.bak.$TS"
  tmp=$(mktemp)
  { grep -v '^[[:space:]]*sudoers[[:space:]]*:' "$NS" || true; } \
    | awk '{ a[NR] = $0 }
           END { n = NR; while (n > 0 && a[n] ~ /^[[:space:]]*$/) n--
                 for (i = 1; i <= n; i++) print a[i] }' > "$tmp"
  printf '%s\n' "$SUDOERS_LINE" >> "$tmp"
  cat "$tmp" > "$NS"
  rm -f "$tmp"
  log "nsswitch.conf 수정 완료 (백업 $NS.bak.$TS)"
}

# e-1. repolist 에 우리 repo 만 있는지 확인
verify_repolist() {
  local ids
  dnf clean all || die "dnf clean all 실패"
  ids=$(LC_ALL=C dnf -q repolist --enabled | awk '
    $1 == "repo" && $2 == "id" { next }
    NF { sub(/^[!*]/, "", $1); print $1 }') || die "dnf repolist 실패"
  log "활성 repo: $(echo "$ids" | tr '\n' ' ')"
  if [[ $ids != "$REPO_ID" ]]; then
    die "repolist 가 로컬 repo($REPO_ID) 단독이 아님: $(echo "$ids" | tr '\n' ' ')"
  fi
}

# e-2. dnf --assumeno 로 의존성 해결 여부 확인
verify_resolve() {
  local -a pkgs
  local out rc=0
  read -r -a pkgs <<<"$PKGS_STR"
  out=$(LC_ALL=C dnf --assumeno install "${pkgs[@]}" 2>&1) || rc=$?
  echo "$out"
  if echo "$out" | grep -Eqi 'No match for argument|nothing provides|^Error:|^Problem|Unable to find a match|Failed to download'; then
    die "의존성 해결 실패 (dnf 출력 참조)"
  fi
  if (( rc == 0 )); then
    log "의존성 해결 OK (rc=0)"
  elif echo "$out" | grep -Eq 'Operation aborted|작업이 취소'; then
    log "의존성 해결 OK (assumeno 로 중단됨)"
  else
    die "dnf --assumeno 비정상 종료 (rc=$rc)"
  fi
}

# e-3. nsswitch 마지막 줄 확인
verify_nsswitch() {
  local last
  last=$(tail -n 1 "$NS")
  if [[ $last != "$SUDOERS_LINE" ]]; then
    die "nsswitch.conf 마지막 줄이 다름: $last"
  fi
  log "nsswitch.conf 마지막 줄 OK"
}

move_old_repos
write_local_repo
disable_subscription_plugin
fix_nsswitch
verify_repolist
verify_resolve
verify_nsswitch
log "구성 완료"
REMOTE_CONFIG_EOF

# ---- 호스트별 처리 ----
fail() {
  printf 'FAIL|%s\n' "$1" > "$RESULT_FILE"
  log "실패: $1"
  exit 1
}

host_flow() {
  local host=$1 rel ver tgz remote_tgz out reason
  log "시작: $host"

  rel=$(rssh "$host" 'cat /etc/redhat-release' </dev/null) \
    || fail "SSH 접속 또는 /etc/redhat-release 읽기 실패"
  if ! [[ $rel =~ release[[:space:]]+([0-9]+\.[0-9]+) ]]; then
    fail "버전 인식 실패: $rel"
  fi
  ver=${BASH_REMATCH[1]}
  case "$ver" in
    8.10|9.6) log "감지된 버전: $ver" ;;
    *) fail "지원하지 않는 버전: $ver" ;;
  esac

  tgz=$(bundle_path "$ver")
  remote_tgz="/tmp/$(basename "$tgz")"
  rscp "$tgz" "$host" "$remote_tgz" </dev/null || fail "번들 전송(scp) 실패"
  log "번들 전송 완료: $remote_tgz"

  remote_bash "$host" "$REMOTE_EXTRACT" "$ver" "$REPO_BASE" "$remote_tgz" 2>&1 \
    || fail "번들 압축 해제 실패"

  if out=$(remote_bash "$host" "$REMOTE_CONFIG" "$ver" "$REPO_BASE" "$GPGKEY" "${PKGS[*]}" 2>&1); then
    printf '%s\n' "$out"
  else
    printf '%s\n' "$out"
    reason=$(printf '%s\n' "$out" | grep '^\[ERR\]' | tail -n 1 | sed 's/^\[ERR\] *//' || true)
    fail "${reason:-원격 구성/검증 실패 (로그 참조)}"
  fi

  printf 'OK|rhel%s\n' "$ver" > "$RESULT_FILE"
  log "성공: $host"
}

# 백그라운드 잡 진입점: 결과 파일이 없으면 예기치 않은 실패로 기록
host_job() {
  local host=$1
  RESULT_FILE="$RESULT_DIR/$(safe_name "$host")"
  # shellcheck disable=SC2064
  trap "[[ -s '$RESULT_FILE' ]] || echo 'FAIL|예기치 않은 오류 (로그 참조)' > '$RESULT_FILE'" EXIT
  host_flow "$host"
}

safe_name() { printf '%s' "${1//[^A-Za-z0-9._-]/_}"; }

# ---- 병렬 실행 ----
run_all() {
  local host
  RESULT_DIR="$LOG_DIR/.results"
  rm -rf "$RESULT_DIR"
  mkdir -p "$RESULT_DIR"
  trap 'jobs -p | xargs -r kill 2>/dev/null; exit 130' INT TERM

  for host in "${HOSTS[@]}"; do
    while (( $(jobs -rp | wc -l) >= PARALLEL )); do
      wait -n || true
    done
    host_job "$host" > "$LOG_DIR/$(safe_name "$host").log" 2>&1 &
  done
  wait || true
}

# ---- 요약 ----
print_summary() {
  local host res status reason ok=0 ng=0
  local -a fails=()
  for host in "${HOSTS[@]}"; do
    res="$RESULT_DIR/$(safe_name "$host")"
    if [[ -s $res ]]; then
      IFS='|' read -r status reason < "$res"
    else
      status=FAIL; reason="결과 없음"
    fi
    if [[ $status == OK ]]; then
      ok=$((ok + 1))
    else
      ng=$((ng + 1))
      fails+=("$host|$reason")
    fi
  done

  echo
  echo "================ 실행 요약 ================"
  printf '성공 %d / 실패 %d (전체 %d)\n' "$ok" "$ng" "${#HOSTS[@]}"
  if (( ng > 0 )); then
    echo "---- 실패 호스트 ----"
    local item
    for item in "${fails[@]}"; do
      printf '%-30s %s\n' "${item%%|*}" "${item#*|}"
      printf '%-30s 로그: %s/%s.log\n' "" "$LOG_DIR" "$(safe_name "${item%%|*}")"
    done
  fi
  echo "호스트별 로그 디렉터리: $LOG_DIR"
  echo "==========================================="
  FAIL_COUNT=$ng
}

main() {
  FAIL_COUNT=0
  parse_args "$@"
  check_prereq
  load_hosts
  check_bundles
  mkdir -p "$LOG_DIR"
  ask_password
  log "대상 ${#HOSTS[@]}대, 동시 ${PARALLEL}대, 포트 ${SSH_PORT}"
  run_all
  print_summary
  if (( FAIL_COUNT > 0 )); then
    exit 1
  fi
}

main "$@"
