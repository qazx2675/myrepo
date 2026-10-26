#!/usr/bin/env bash
# update.sh — 새 버전 ZIP 으로 설치 폴더를 갱신합니다 (현장 설정·비밀번호·대상 목록·결과는 그대로 둠).
#
# 사용법:
#   bash update.sh                # /tmp 에서 가장 최근 *.zip 을 사용
#   bash update.sh /tmp/x.zip     # ZIP 을 직접 지정 (압축을 이미 풀었으면 그 폴더도 가능)
#
# 하는 일:
#   1) ZIP 을 /tmp 아래 임시 폴더에 풀고 그 안의 redfish 폴더(lib.sh + bios_check.sh 가 있는 곳)를 찾는다
#   2) 설치 폴더 전체를 백업 (results/, dumps/ 제외) -> BACKUP_DIR/V2_<일시>.tgz
#   3) 아래 KEEP 항목은 건드리지 않고, 나머지 항목만 새 버전으로 바꾼다 (새 버전에 없는 기존 항목은 그대로 둠)
#   4) profiles/ 는 파일별로: 현장에서 고치지 않은 파일만 새 버전으로 바꾸고,
#      고친 파일(verified=Y 등)은 그대로 두고 새 버전을 <이름>.new 로 옆에 둔다 (*.tsv 가 아니라 읽히지 않음)
set -euo pipefail

TARGET="/root/server_vm_python/redhfish/V2"   # 설치 폴더 (고정)
BACKUP_DIR="/root/server_vm_python/redhfish/backup"
# 현장에서만 만드는 항목 — 새 버전으로 덮지 않는다
KEEP="bios.conf pass.enc pass2.enc rf.enc key.bin user.txt diff.txt ignore_attrs.txt refs results dumps"
PROFILE_SUMS=".update_profiles.sha" # 마지막 업데이트로 설치한 profiles 의 sha256 (현장 수정 여부 판단용)

work="" # ZIP 을 푼 임시 폴더 (끝나면 지움)
cleanup() { [ -n "$work" ] && rm -rf "$work"; return 0; }

main() {
  local src_arg="${1:-}" src zip
  trap cleanup EXIT

  if [ ! -d "$TARGET" ] || [ ! -f "$TARGET/lib.sh" ]; then
    echo "오류: 설치 폴더 $TARGET 이(가) 없거나 redfish 폴더가 아닙니다 (lib.sh 없음)." >&2
    exit 1
  fi

  # ---- 1) 새 버전 찾기 ----
  if [ -n "$src_arg" ] && [ -d "$src_arg" ]; then
    src="$(find_src "$src_arg")"
  else
    zip="$src_arg"
    if [ -z "$zip" ]; then
      zip="$(newest_redfish_zip)"
      if [ -z "$zip" ]; then
        echo "오류: /tmp 에 redfish ZIP(lib.sh + bios_check.sh 가 들어 있는 *.zip)이 없습니다. bash update.sh <ZIP 경로> 로 지정하십시오." >&2
        exit 1
      fi
    fi
    if [ ! -f "$zip" ]; then
      echo "오류: $zip 이(가) 없습니다." >&2
      exit 1
    fi
    if ! command -v unzip >/dev/null 2>&1; then
      echo "오류: unzip 명령이 없습니다." >&2
      exit 1
    fi
    work="$(mktemp -d /tmp/redfish_update.XXXXXX)"
    unzip -q "$zip" -d "$work"
    src="$(find_src "$work")"
  fi
  if [ -z "$src" ]; then
    echo "오류: ${zip:-$src_arg} 안에서 redfish 폴더(lib.sh + bios_check.sh)를 찾지 못했습니다." >&2
    exit 1
  fi
  if [ "$(cd "$src" && pwd -P)" = "$(cd "$TARGET" && pwd -P)" ]; then
    echo "오류: 새 버전 폴더가 설치 폴더와 같습니다." >&2
    exit 1
  fi

  echo "새 버전 : ${zip:-$src_arg}"
  echo "          ($src)"
  echo "설치 폴더: $TARGET"
  echo "버전    : $(ver "$TARGET") -> $(ver "$src")"
  echo "그대로 둠: $KEEP (+ profiles/ 의 현장 수정 파일)"
  if [ -t 0 ]; then
    printf '진행할까요? [Y/n] : '
    local ans=""
    read -r ans || ans=""
    case "$ans" in n | N | no | NO) echo "취소했습니다."; exit 0 ;; esac
  fi

  # ---- 2) 백업 ----
  mkdir -p "$BACKUP_DIR"
  local stamp backup
  stamp="$(date +%Y%m%d_%H%M%S)"
  backup="$BACKUP_DIR/V2_$stamp.tgz"
  tar czf "$backup" -C "$(dirname "$TARGET")" --exclude="$(basename "$TARGET")/results" \
    --exclude="$(basename "$TARGET")/dumps" "$(basename "$TARGET")"
  chmod 600 "$backup"
  echo "백업    : $backup  (되돌리기: 설치 폴더를 지우고 tar xzf $backup -C $(dirname "$TARGET"))"

  # ---- 3) KEEP·profiles 를 뺀 나머지 교체 ----
  local p name n_rep=0 newnames="/"
  shopt -s dotglob nullglob
  for p in "$src"/*; do
    name="$(basename "$p")"
    case "$name" in "" | . | ..) continue ;; esac
    newnames="$newnames$name/"
    if is_keep "$name" || [ "$name" = "profiles" ]; then
      continue
    fi
    rm -rf "${TARGET:?}/$name"
    mv "$p" "$TARGET/$name"
    n_rep=$((n_rep + 1))
  done
  echo "교체    : $n_rep 개 항목"

  # ---- 4) profiles ----
  update_profiles "$src/profiles" "$TARGET/profiles"

  # 새 버전에 없는 기존 항목 (지우지 않음)
  local extra=""
  for p in "$TARGET"/*; do
    name="$(basename "$p")"
    case "$newnames" in *"/$name/"*) continue ;; esac
    is_keep "$name" && continue
    case "$name" in profiles | "$PROFILE_SUMS") continue ;; esac
    extra="$extra $name"
  done
  shopt -u dotglob nullglob
  [ -n "$extra" ] && echo "참고    : 새 버전에 없는 기존 항목(그대로 둠):$extra"

  # bios.conf 에 없는 새 설정 키 (없으면 기본값으로 동작하므로 바꿀 때만 추가)
  if [ -f "$TARGET/bios.conf" ] && [ -f "$TARGET/bios.conf.example" ]; then
    local k newkeys=""
    for k in $(sed -n 's/^[[:space:]]*\([a-z_][a-z_0-9]*\)[[:space:]]*=.*/\1/p' "$TARGET/bios.conf.example" | sort -u); do
      grep -qE "^[[:space:]]*${k}[[:space:]]*=" "$TARGET/bios.conf" || newkeys="$newkeys $k"
    done
    [ -n "$newkeys" ] && echo "참고    : bios.conf 에 없는 설정 키(기본값 사용, 필요하면 bios.conf.example 참고해 추가):$newkeys"
  fi
  echo
  echo "완료. 확인: cd $TARGET && bash bios.sh -h"
  echo "주의: 도구 결과는 참고용입니다. 설정을 바꾼 뒤에는 랜덤한 서버 몇 대를 직접 확인하십시오."
}

# newest_redfish_zip: /tmp 의 *.zip 중 가장 최근이면서 redfish 폴더(lib.sh + bios_check.sh)가 들어 있는 것.
# 다른 ZIP(예: 장비 도구 압축)이 더 최근이어도 건너뛴다. 풀지 않고 목록만 본다.
newest_redfish_zip() {
  local z list
  command -v unzip >/dev/null 2>&1 || return 0
  while IFS= read -r z; do
    [ -n "$z" ] || continue
    list="$(unzip -Z1 "$z" 2>/dev/null || true)"
    if printf '%s\n' "$list" | grep -qE '(^|/)lib\.sh$' && printf '%s\n' "$list" | grep -qE '(^|/)bios_check\.sh$'; then
      echo "$z"
      return 0
    fi
  done < <(ls -t /tmp/*.zip 2>/dev/null || true)
  return 0
}

# find_src <폴더>: lib.sh 와 bios_check.sh 가 함께 있는 가장 얕은 폴더
find_src() {
  local list f d
  list="$(find "$1" -name lib.sh -type f 2>/dev/null | awk -F/ '{print NF "\t" $0}' | sort -n | cut -f2- || true)"
  while IFS= read -r f; do
    [ -n "$f" ] || continue
    d="$(dirname "$f")"
    if [ -f "$d/bios_check.sh" ]; then
      echo "$d"
      return 0
    fi
  done <<<"$list"
  return 0
}

is_keep() {
  local k
  for k in $KEEP; do [ "$1" = "$k" ] && return 0; done
  return 1
}

ver() { # CHANGELOG 의 첫 버전 줄
  local v=""
  [ -f "$1/CHANGELOG.md" ] && v="$(grep -m1 '^## ' "$1/CHANGELOG.md" | sed 's/^## *//' || true)"
  echo "${v:-알 수 없음}"
}

sum_of() { sha256sum "$1" | cut -d' ' -f1; }

# update_profiles <새 profiles> <설치 profiles>
update_profiles() {
  local new="$1" cur="$2" sums="$TARGET/$PROFILE_SUMS" tmp f name s_new s_cur s_old
  [ -d "$new" ] || return 0
  mkdir -p "$cur"
  tmp="$(mktemp)"
  for f in "$new"/*; do
    [ -f "$f" ] || continue
    name="$(basename "$f")"
    s_new="$(sum_of "$f")"
    if [ ! -f "$cur/$name" ]; then
      cp -p "$f" "$cur/$name"
      echo "profiles: $name  (새 파일 추가)"
    else
      s_cur="$(sum_of "$cur/$name")"
      s_old="$(awk -v n="$name" '$2 == n {print $1}' "$sums" 2>/dev/null || true)"
      if [ "$s_cur" = "$s_new" ]; then
        :
      elif [ -n "$s_old" ] && [ "$s_cur" = "$s_old" ]; then
        cp -p "$f" "$cur/$name"
        echo "profiles: $name  (새 버전으로 교체 — 현장 수정 없음)"
      else
        cp -p "$f" "$cur/$name.new"
        echo "profiles: $name  (현장에서 고친 파일이라 그대로 둠 -> 새 버전은 $name.new, diff 로 비교해 필요한 줄만 옮기십시오)"
        continue
      fi
    fi
    rm -f "$cur/$name.new"
    echo "$s_new $name" >>"$tmp"
  done
  mv "$tmp" "$sums"
  chmod 644 "$sums"
}

main "$@"
exit $?
