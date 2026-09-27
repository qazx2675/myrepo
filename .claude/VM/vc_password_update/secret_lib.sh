# secret_lib.sh — 암호화해 둔 vCenter / ESXi 비밀번호를 읽는 함수 (source 해서 쓴다)
#
# 저장 위치: V2 폴더의 secret/ (VMSETUP_SECRET_DIR 로 바꿀 수 있음)
#   secret/key              암호화 키 (passwd_update.sh 가 처음 한 번 만든다, root 만 읽기)
#   secret/vcenter_<계정>.enc  vCenter 계정 비밀번호
#   secret/esxi_<계정>.enc     ESXi 계정 비밀번호
# 다른 서버에서 쓰려면 secret/ 폴더(키 포함)를 통째로 복사한다.
#
# 암호화: openssl enc -aes-256-cbc -md sha256 -salt -a (키 파일로 암호 유도)
#   OS6 의 openssl 1.0.1 과 OS8 의 1.1.1 이 같은 방식으로 풀도록 -md sha256 을 고정하고
#   1.1.1+ 에만 있는 -pbkdf2 는 쓰지 않는다(1.1.1 이 내는 "deprecated key derivation" 경고는 버린다).
# 키 파일이 있으면 누구든 풀 수 있다 — 파일 권한(600)으로 막는다. 평문을 스크립트/환경변수에 두지 않는 것이 목적.

SECRET_LIB_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SECRET_DIR="${VMSETUP_SECRET_DIR:-$SECRET_LIB_DIR/secret}"

# secret_file <vcenter|esxi> <계정> — 암호 파일 경로 (계정의 / 는 _ 로)
secret_file() { printf '%s/%s_%s.enc' "$SECRET_DIR" "$1" "$(printf '%s' "$2" | tr '/' '_')"; }

# secret_get <vcenter|esxi> <계정> — 비밀번호를 표준출력으로. 없거나 못 풀면 1
secret_get() {
  local f; f="$(secret_file "$1" "$2")"
  [ -r "$SECRET_DIR/key" ] && [ -r "$f" ] || return 1
  openssl enc -d -aes-256-cbc -md sha256 -a -pass "file:$SECRET_DIR/key" -in "$f" 2>/dev/null
}

# secret_put <vcenter|esxi> <계정> <비밀번호> — 암호화해서 저장(키가 없으면 만든다)
secret_put() {
  local f tmp; f="$(secret_file "$1" "$2")"
  mkdir -p "$SECRET_DIR" && chmod 700 "$SECRET_DIR" || return 1
  if [ ! -s "$SECRET_DIR/key" ]; then
    (umask 077; openssl rand -base64 48 | tr -d '\n' > "$SECRET_DIR/key" && echo >> "$SECRET_DIR/key") || return 1
  fi
  tmp="$(mktemp "$SECRET_DIR/.tmp.XXXXXX")" || return 1
  if printf '%s' "$3" | openssl enc -aes-256-cbc -md sha256 -salt -a -pass "file:$SECRET_DIR/key" -out "$tmp" 2>/dev/null; then
    chmod 600 "$tmp" && mv -f "$tmp" "$f"
  else
    rm -f "$tmp"; return 1
  fi
}
