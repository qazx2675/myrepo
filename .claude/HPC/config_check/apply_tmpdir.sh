#!/bin/bash
# apply_tmpdir.sh - 회사 사본의 작업 디렉터리(config_check.XXXXXX)를 /tmp 바로 아래가 아니라 TMPDIR(없으면 /tmp/auto_setup/tmp)에 만들게 바꾼다.
#   사용: bash apply_tmpdir.sh [config_check.sh 경로]   (기본: ./config_check.sh)
#   바뀌는 기존 줄은 `TMP=$(mktemp -d …config_check.XXXXXX) || exit 1` 한 줄뿐이고, 그 앞에 TMPDIR 기본값 블록, 그 뒤 trap 아래에
#   HUP 트랩 한 줄을 넣는다. 주석·벤더 코멘트 문구는 건드리지 않는다. 이미 적용된 사본은 그대로 둔다.
#   1) <대상>.bak 백업  2) 적용  3) 삭제된 줄 검사(대상 한 줄 외에는 없어야 함)  4) bash -n 문법 확인. 실패하면 대상은 바뀌지 않는다.
set -u
TARGET="${1:-config_check.sh}"
[ -f "$TARGET" ] || { echo "[X] 대상 파일이 없습니다: $TARGET" >&2; exit 1; }
if grep -q 'AUTO_SETUP_DIR:-/tmp/auto_setup}/tmp' "$TARGET"; then
    echo "[O] 이미 적용되어 있습니다: $TARGET (변경 없음)"
    exit 0
fi

W=$(mktemp -d) || exit 1; trap 'rm -rf "$W"' EXIT
tr -d '\r' < "$TARGET" > "$W/orig"          # CRLF 사본도 처리 (적용 결과는 LF)
re='^TMP=\$\(mktemp -d ("\$\{TMPDIR:-/tmp\}/|/tmp/)config_check\.XXXXXX"?\) \|\| exit 1'
n=$(grep -cE "$re" "$W/orig")
if [ "$n" != 1 ]; then
    echo "[X] 바꿀 줄(TMP=\$(mktemp -d …config_check.XXXXXX) || exit 1)을 ${n}개 찾았습니다 (1개여야 함) — $TARGET 은 변경하지 않았습니다" >&2
    exit 1
fi
RE="$re" awk '   # 정규식은 환경변수로 (awk -v 는 백슬래시를 먼저 해석해 식이 바뀜)
    !done && $0 ~ ENVIRON["RE"] {
        print "# 작업 디렉터리는 /tmp 바로 아래가 아니라 auto_setup 데이터 디렉터리의 tmp 에 (auto_setup 이 실행하면 TMPDIR 로 지정됨). 못 쓰면 /tmp"
        print "if [ -z \"${TMPDIR:-}\" ]; then"
        print "    _t=${AUTO_SETUP_DIR:-/tmp/auto_setup}/tmp"
        print "    mkdir -p \"$_t\" 2>/dev/null; chmod 1777 \"$_t\" 2>/dev/null"
        print "    [ -d \"$_t\" ] && [ -w \"$_t\" ] && TMPDIR=$_t"
        print "fi"
        print "TMP=$(mktemp -d \"${TMPDIR:-/tmp}/config_check.XXXXXX\") || exit 1"
        done = 1; next
    }
    done && !hup && $0 ~ /^trap .*exit 130.* INT TERM[[:space:]]*$/ {
        print
        print "trap '\''exit 129'\'' HUP   # 터미널·ssh 가 끊겨도 작업 디렉터리를 지운다"
        hup = 1; next
    }
    { print }
    END { if (!done || !hup) exit 3 }
' "$W/orig" > "$W/new"
if [ $? -ne 0 ]; then
    echo "[X] 적용 위치(TMP 줄 뒤의 trap … INT TERM 줄)를 찾지 못했습니다 — $TARGET 은 변경하지 않았습니다" >&2
    exit 1
fi
removed=$(diff "$W/orig" "$W/new" | grep '^< ' | sed 's/^< //')
if [ "$(printf '%s\n' "$removed" | grep -c .)" != 1 ] || ! printf '%s\n' "$removed" | grep -qE "$re"; then
    echo "[X] TMP 줄 외의 줄이 바뀌게 되어 중단했습니다 — $TARGET 은 변경하지 않았습니다:" >&2
    printf '%s\n' "$removed" >&2
    exit 1
fi
bash -n "$W/new" || { echo "[X] 적용 결과 문법 오류 — $TARGET 은 변경하지 않았습니다" >&2; exit 1; }

cp "$TARGET" "$TARGET.bak"
cat "$W/new" > "$TARGET"
echo "[O] 적용 완료: $TARGET (백업: $TARGET.bak)"
echo "    바뀐 기존 줄: 1줄 (TMP=\$(mktemp …)), 추가: TMPDIR 기본값 6줄 + HUP 트랩 1줄, 주석·코멘트 문구 변경 0건"
echo "    다음: 실행해 보면 작업 디렉터리가 /tmp/auto_setup/tmp/config_check.XXXXXX 에 생기고 끝나면 지워집니다"
