#!/usr/bin/env bash
# install.sh — 192.168.0.58(록키)에 saccae 검증 환경을 만든다. root 로 실행, 여러 번 실행해도 된다.
#   1) /etc/hosts 에 *.saccae.com 이름 등록 (hosts.saccae 블록)
#   2) V2 브랜치를 $DEST(/home/saccae) 에 받아 오프라인 빌드 (이미 있으면 git pull)
#   3) 검증용 SPEC_DIR 스펙 + BM 목록(saccae/vm06/vm08/vm20/vm99, fail_*) + vcenter.txt + 사용법 복사 (같은 이름 파일은 덮어씀)
#   4) 가상 vCenter(vcsim) 빌드 후 (재)시작 — 호스트 hostname0000~0099.saccae.com, 계정 lscsystems@vsphere.local
#   5) 가상 vCenter 비밀번호를 암호 파일로 등록(passwd_update.sh) — 이미 등록돼 있으면 그대로
set -euo pipefail
SRC="$(cd "$(dirname "$0")" && pwd)"; VERIFY="$(dirname "$SRC")"
DEST="${DEST:-/home/saccae}"; BRANCH="${BRANCH:-V2}"; REPO="${REPO:-https://github.com/qazx2675/myrepo.git}"
VENDOR="${VCSIM_VENDOR:-$VERIFY/../../공통/govendor/govmomi-0.55.1-vcsim}"
VCSIM_PASS="${VCSIM_PASS:-Saccae1!}"
[ "$(id -u)" = 0 ] || { echo "root 로 실행하세요."; exit 1; }

echo "== 1) /etc/hosts"
sed -i '/^# >>> saccae 검증 환경/,/^# <<< saccae 검증 환경/d' /etc/hosts
cat "$SRC/hosts.saccae" >> /etc/hosts
getent hosts vcsim.saccae.com vcenter.saccae.com
# 터미널 종류(TERM)가 비었거나 dumb 로 들어오면(일부 ssh 클라이언트) 색이 안 나오므로 로그인 셸에서 xterm-256color 로 잡는다
cat > /etc/profile.d/saccae-term.sh <<'EOF'
# saccae 검증 환경 (install.sh 가 만듦): TERM 이 비었거나 dumb 인 대화형 터미널이면 색을 쓸 수 있게 xterm-256color 로
if [ -t 1 ] && { [ -z "${TERM:-}" ] || [ "$TERM" = dumb ]; }; then export TERM=xterm-256color; fi
EOF

echo "== 2) V2 코드 ($DEST, $BRANCH 브랜치)"
if [ -d "$DEST/.git" ]; then git -C "$DEST" pull --ff-only
else git clone -b "$BRANCH" --single-branch "$REPO" "$DEST"; fi
(cd "$DEST" && ./setup.sh)

echo "== 3) 검증 데이터"
cp -r "$SRC/SPEC_DIR/." "$DEST/SPEC_DIR/"
cp "$SRC"/VMsetup/*.txt "$DEST/VMsetup/"
cp "$SRC/vcenter.txt" "$SRC/vcenter_vcsim.txt" "$SRC/사용법.txt" "$SRC/실패테스트사용법.txt" "$DEST/"
ls "$DEST/SPEC_DIR"

echo "== 4) 가상 vCenter (vcsim)"
mkdir -p "$DEST/testenv/bin"
[ -d "$VENDOR" ] || { echo "vcsim 의존성 폴더가 없습니다: $VENDOR (VCSIM_VENDOR 로 지정 가능)"; exit 1; }
(cd "$VERIFY" && ln -sfn "$VENDOR" vendor && go build -mod=vendor -o "$DEST/testenv/bin/vcsimenv" ./cmd/vcsimenv)
cp "$SRC/vcsim.sh" "$SRC/fail_inject.sh" "$DEST/testenv/"; chmod +x "$DEST/testenv/"*.sh
VCSIM_PASS="$VCSIM_PASS" "$DEST/testenv/vcsim.sh" restart

echo "== 5) 비밀번호 암호 파일 ($DEST/secret)"
. "$DEST/secret_lib.sh"
if secret_get vcenter lscsystems@vsphere.local >/dev/null; then
  echo "이미 등록돼 있습니다 — 바꾸려면: $DEST/passwd_update.sh"
else
  printf '%s\n%s\n' "$VCSIM_PASS" "$VCSIM_PASS" | "$DEST/passwd_update.sh" 2>/dev/null
  printf 'EsxiRoot1!\nEsxiRoot1!\n' | "$DEST/passwd_update.sh" -esxi 2>/dev/null   # ESXi 는 등록 예시 (가상 vCenter 에서는 쓰지 않음)
fi
"$DEST/passwd_update.sh" -list

echo; echo "완료 — 사용법: $DEST/사용법.txt, 실패 테스트: $DEST/실패테스트사용법.txt"
