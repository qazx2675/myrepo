#!/usr/bin/env bash
# list.txt 에 hostname 입력 -> bash collect.sh -> out/<hostname>.json
# 일회성 수집용: 계정은 아래에 평문 하드코딩.
USER_ID="admin"
USER_PW="password"

cd "$(dirname "$0")"
[ -f list.txt ] || { echo "list.txt 가 없습니다 (hostname 한 줄씩)"; exit 1; }

# 바이너리 선택: 같은 폴더의 미리 빌드된 것 우선, 없으면 go build
case "$(uname -r)" in
  2.6.*) BIN=./biosdump_os6 ;;
  *)     BIN=./biosdump ;;
esac
if [ ! -x "$BIN" ]; then
  command -v go >/dev/null || { echo "$BIN 도 go 도 없음"; exit 1; }
  go build -o biosdump . || exit 1
  BIN=./biosdump
fi

"$BIN" -list list.txt -out "out/$(date +%Y%m%d_%H%M%S)" -user "$USER_ID" -pass "$USER_PW"
