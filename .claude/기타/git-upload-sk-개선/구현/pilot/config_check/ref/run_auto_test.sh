#!/bin/bash
# run_auto_test.sh - config_check.sh 의 -auto 모드·완료기록 테스트 (gossh 는 스텁, 실서버 접속 없음)
#   사용: bash test/run_auto_test.sh
set -u
HERE="$(cd "$(dirname "$0")/.." && pwd)"
W=$(mktemp -d /tmp/cc_auto_test.XXXXXX)
trap 'rm -rf "$W"' EXIT
pass=0; failn=0
ok()   { pass=$((pass + 1)); echo "[PASS] $1"; }
bad()  { failn=$((failn + 1)); echo "[FAIL] $1"; }
chk()  { if eval "$2"; then ok "$1"; else bad "$1"; fi; }

mkdir -p "$W/bin" "$W/run" "$W/done_local" "$W/asdir"
# gossh 스텁: 체크(run.sh) → 호스트별 INFO 줄(down* 은 _res_off, fail* 은 FAIL 줄, err* 는 ERROR 줄), 설정 명령 → ok,
# 완료기록 원격 호출(bash -c '...')은 AUTO_SETUP_DIR 로 실제 실행
cat > "$W/bin/gossh" <<'STUB'
#!/bin/bash
args=("$@"); f=""; cmd=""
i=0
while [ $i -lt ${#args[@]} ]; do
    case "${args[$i]}" in -w) f="${args[$((i+1))]}"; i=$((i+2)); continue ;; -pm|-script) ;; *) cmd="${args[$i]}" ;; esac
    i=$((i+1))
done
echo "$cmd" >> "$GOSSH_LOG"
case "$cmd" in
    "bash -c '"*) bash -c "$cmd" | sed 's/^/done-host: /'; exit 0 ;;
    *run.sh*)
        while read -r h; do
            case "$h" in
                down*) echo "$h" >> "${f}_res_off" ;;
                fail*) echo "$h: FAIL usb0 interface check"; echo "$h: INFO LDAP infraA std 1" ;;
                err*)  echo "$h: ERROR connect" ;;
                *)     echo "$h: OK"; echo "$h: INFO LDAP infraA std 1" ;;
            esac
        done < "$f" ;;
    *) while read -r h; do echo "$h: ok"; done < "$f" ;;
esac
STUB
chmod +x "$W/bin/gossh"
export PATH="$W/bin:$PATH" GOSSH_LOG="$W/gossh.log" AUTO_SETUP_DIR="$W/asdir"

# 사본: user 선택 스텁·완료기록 변수 주입
cat > "$W/info_mn.sh" <<'S'
echo "1) u1"
S
cat > "$W/info.sh" <<'S'
echo u1
S
make_copy() {   # $1=사본 경로 $2=auto_done_dir $3=auto_done_host
    sed -e "s|^user_info_mn=.*|user_info_mn=\"$W/info_mn.sh\"|" \
        -e "s|^user_info_output=.*|user_info_output=\"$W/info.sh\"|" \
        -e "s|^auto_done_dir=\"\"|auto_done_dir=\"$2\"|" \
        -e "s|^auto_done_host=\"\"|auto_done_host=\"$3\"|" \
        "$HERE/config_check.sh" > "$1"
}

# 1) 인자 오류
make_copy "$W/cc_none.sh" "" ""
(cd "$W/run" && bash "$W/cc_none.sh" -auto u1 /nonexistent </dev/null >"$W/o1" 2>&1); rc=$?
chk "-auto 목록 파일 없음 → 종료코드 1" "[ $rc -eq 1 ] && grep -q '목록 파일이 없습니다' '$W/o1'"
(cd "$W/run" && bash "$W/cc_none.sh" -auto u1 </dev/null >"$W/o1" 2>&1); rc=$?
chk "-auto 인자 부족 → 종료코드 1" "[ $rc -eq 1 ]"

# 2) -auto, 완료기록 변수 비움: 질문 없이 진행, 마커·postapply, 완료기록 없음
printf 'pa01 pa02\nfail01 down01\nerr01\n' > "$W/targets.txt"
: > "$GOSSH_LOG"
(cd "$W/run" && bash "$W/cc_none.sh" -auto u1 "$W/targets.txt" </dev/null >"$W/o2" 2>&1); rc=$?
chk "-auto 정상 종료(stdin 없이)" "[ $rc -eq 0 ]"
chk "-auto 질문 에코 (y / set)" "grep -q '작업을 진행하시겠습니까? (y/n): y (-auto)' '$W/o2' && grep -q '환경설정을 수정하시겠습니까? (y/n/set): set (-auto)' '$W/o2'"
chk "결과 리포트 시작·끝 마커" "grep -qx '############### 결과 리포트 ###############' '$W/o2' && [ \"\$(tail -n1 '$W/o2')\" = '###########################################' ]"
chk "postapply 파일 생성 (OK 대상만)" "[ -s '$W/run/check.res_u1_postapply' ] && grep -q '^pa01:' '$W/run/check.res_u1_postapply' && ! grep -q '^down01' '$W/run/check.res_u1_postapply'"
chk "설정 명령이 setting.sh 까지 실행(set)" "grep -q 'setting.sh' '$W/gossh.log'"
chk "완료기록 변수 비면 기록 없음" "[ -z \"\$(ls -A '$W/asdir' 2>/dev/null)\" ] && ! grep -q '완료기록' '$W/o2'"

# 3) -auto, auto_done_dir
rm -rf "$W/run"/* ; : > "$GOSSH_LOG"
make_copy "$W/cc_dir.sh" "$W/done_local" ""
(cd "$W/run" && bash "$W/cc_dir.sh" -auto u1 "$W/targets.txt" </dev/null >"$W/o3" 2>&1)
chk "auto_done_dir 완료기록 (pa01, pa02, fail01)" "[ -f '$W/done_local/pa01' ] && [ -f '$W/done_local/pa02' ] && [ -f '$W/done_local/fail01' ]"
chk "ERROR·접속불가 호스트는 기록 안 함" "[ ! -e '$W/done_local/err01' ] && [ ! -e '$W/done_local/down01' ]"
chk "기록 형식 'epoch user sha256 config_check'" "grep -Eq '^[0-9]+ u1 [0-9a-f]{64} config_check\$' '$W/done_local/pa01'"
chk "완료기록 INFO 출력" "grep -q 'auto_setup 완료기록 : 3대' '$W/o3'"

# 4) -auto, auto_done_host (gossh 원샷, AUTO_SETUP_DIR/done)
rm -rf "$W/run"/* "$W/asdir"/*; : > "$GOSSH_LOG"
make_copy "$W/cc_host.sh" "" "os8host"
(cd "$W/run" && bash "$W/cc_host.sh" -auto u1 "$W/targets.txt" </dev/null >"$W/o4" 2>&1)
chk "auto_done_host 원격 기록 (done/ 에 3개)" "[ -f '$W/asdir/done/pa01' ] && [ -f '$W/asdir/done/pa02' ] && [ -f '$W/asdir/done/fail01' ] && [ ! -e '$W/asdir/done/err01' ]"
chk "원격 기록 형식" "grep -Eq '^[0-9]+ u1 [0-9a-f]{64} config_check\$' '$W/asdir/done/pa01'"
chk "원격 완료기록 INFO 출력" "grep -q 'auto_setup 완료기록 : 3대 → os8host' '$W/o4'"

# 5) y 분기: p/d 로 시작하는 호스트가 없으면 insert+appl 만 (setting.sh 없음)
rm -rf "$W/run"/*; : > "$GOSSH_LOG"
printf 'ab01 ab02\n' > "$W/t2.txt"
(cd "$W/run" && bash "$W/cc_none.sh" -auto u1 "$W/t2.txt" </dev/null >"$W/o5" 2>&1)
chk "p/d 호스트 없으면 y" "grep -q '환경설정을 수정하시겠습니까? (y/n/set): y (-auto)' '$W/o5' && ! grep -q 'setting.sh' '$W/gossh.log' && grep -q 'appl_change.sh' '$W/gossh.log'"

# 6) 수동 모드(인자 없음)는 기존과 동일: 질문 대기, 마커·postapply 없음
rm -rf "$W/run"/*; : > "$GOSSH_LOG"
printf 'ab01 ab02\n' > "$W/run/u1.txt"
(cd "$W/run" && printf '1\ny\nn\n' | bash "$W/cc_dir.sh" >"$W/o6" 2>&1); rc=$?
chk "수동 모드 정상 종료" "[ $rc -eq 0 ]"
chk "수동 모드: 마커·postapply·완료기록 없음" "! grep -q '결과 리포트' '$W/o6' && [ ! -e '$W/run/check.res_u1_postapply' ] && [ -z \"\$(ls -A '$W/done_local' | grep ab0)\" ]"

echo "=== PASS $pass / FAIL $failn ==="
[ "$failn" -eq 0 ]
