#!/usr/bin/env bash
# vmsetup_test.sh — VMsetup/vm_setup.sh 를 vcsim 으로 시험한다(록키 /root/v2work 기준).
# 대화형 입력은 표준입력으로 흘려 넣고, vim 은 가짜 편집기(VM_SETUP_EDITOR)로 바꿔서 템플릿을 채운다.
set -uo pipefail
H=/root/v2work/harness
V=/root/v2work/V2/VMsetup
export VC_PASSWORD=x
T=/tmp/vs; rm -rf $T; mkdir -p $T
pass=0; fail=0
ok() { echo "  [PASS] $*"; pass=$((pass+1)); }
ng() { echo "  [FAIL] $*"; fail=$((fail+1)); }
cleanup() { $H/stop.sh; rm -f $V/tt.txt; rm -rf $V/run_tt; }
trap cleanup EXIT

# ---- 가짜 편집기: 템플릿 종류(마커 줄)에 따라 값을 채운다. FAKE_BAD_FIRST=1 이면 스펙 첫 편집에서 일부러 잘못된 폴더명을 넣는다.
cat > $T/fake_editor.sh <<'EOF'
#!/usr/bin/env bash
f="$1"; cnt_file=/tmp/vs/fake_count; n=$(cat $cnt_file 2>/dev/null || echo 0); echo $((n+1)) > $cnt_file
if grep -q '^# \[스펙 수동 입력\]' "$f"; then
  folder='TST-CAE002-NEW-QRST'; [ "${FAKE_BAD_FIRST:-0}" = 1 ] && [ "$(cat /tmp/vs/fake_spec_n 2>/dev/null || echo 0)" = 0 ] && folder='nofolder'
  echo $(( $(cat /tmp/vs/fake_spec_n 2>/dev/null || echo 0) + 1 )) > /tmp/vs/fake_spec_n
  sed -i -e "s|^folder=.*# \[필수\] 새로|folder=\"$folder\"   # [필수] 새로|" -e 's|^folder=""|folder="'$folder'"|' \
    -e 's|^ht=""|ht="on"|' -e 's|^cpu=""|cpu="2"|' -e 's|^mem=""|mem="1"|' -e 's|^disk=""|disk="1"|' \
    -e 's|^shares-ev01=""|shares-ev01="normal"|' -e 's|^cores=""|cores="2"|' -e 's|^numa=""|numa="2"|' \
    -e 's|^cpu-ev02=""|cpu-ev02="2"|' -e 's|^mem-ev02=""|mem-ev02="1"|' -e 's|^disk-ev02=""|disk-ev02="1"|' -e 's|^shares-ev02=""|shares-ev02="4000"|' "$f"
elif grep -q '^# \[affinity 수동 입력\]' "$f"; then
  sed -i 's/^\(sched\.vcpu\)\([0-9]*\)\(\.affinity\)=""/\1\2\3="\2,9"/' "$f"
elif grep -q '^# \[네트워크 어댑터 수동 지정\]' "$f"; then
  sed -i -e 's|^hostname=""|hostname="'"${FAKE_NIC_HOST:-x}"'"|' -e 's|^portgroup=""|portgroup="'"${FAKE_NIC_PG:-x}"'"|' "$f"
fi
EOF
chmod +x $T/fake_editor.sh

mkspec() { # <SPEC_DIR> <폴더명>  — ev01/ev02 두 개짜리 스펙
  mkdir -p "$1/$2"
  printf 'ht=on\ncpu=2\nmem=1\ndisk=1\nshares-ev01=normal\ncores=2\nnuma=2\ncpu-ev02=2\nmem-ev02=1\ndisk-ev02=1\nshares-ev02=4000\ncores-ev02=2\nnuma-ev02=2\n' > "$1/$2/$2_spec.txt"
}
run_setup() { # <SPEC_DIR> <vc주소> <추가옵션...>  (표준입력 = 답변)
  local sd=$1 vc=$2; shift 2
  $V/vm_setup.sh -u tt -v "$vc" -s "$sd" "$@"
}

echo "== V1 자동 할당(스펙·포트그룹) → 확인 y → 실행"
A=$($H/sim.sh v1 -dc 1 -cluster 0 -host 2)
SD=$T/SPEC1; mkspec $SD TST-CAE001-SAMP48c-QRST
printf 'DC0_H0\nDC0_H1\n' > $V/tt.txt
printf 'DC0_H0 TST-CAE001-SAMP48c-QRST-cae-10-1-2-3 100\nDC0_H1 TST-CAE001-SAMP48c-QRST-cae-10-1-2-3 100\n' > $SD/vswitch_tt.txt
printf 'y\ny\ny\n' | run_setup $SD $A > $T/v1.out 2>&1
$H/bin/vmdump -vc $A -match 'ev0' > $T/v1.dump
[ "$(wc -l < $T/v1.dump)" = 4 ] && ok "BM 2대 x ev 2개 = 4대 생성" || { ng "생성 대수"; tail -20 $T/v1.out; }
grep -q 'nic=.*:TST-CAE001-SAMP48c-QRST-cae-10-1-2-3:' $T/v1.dump && ok "자동 할당된 포트그룹이 어댑터에 연결" || { ng "포트그룹"; cat $T/v1.dump; }
grep -q 'DC0_H1ev02 .*sched.vcpu1.affinity=2,3' $T/v1.dump && ok "affinity 자동 계산(ht=on) 적용" || { ng "affinity"; cat $T/v1.dump; }
grep -q 'DC0_H0ev01 .*sched.mem.lpage.enable1GPage=TRUE' $T/v1.dump && ok "lpage_setting 적용(1GB 페이지 설정)" || { ng "lpage"; cat $T/v1.dump; }
grep -q 'DC0_H0ev02 .*cpuShares=custom/4000' $T/v1.dump && ok "스펙 shares-ev02=4000 적용" || ng "shares"
grep -q '\[완료\]' $T/v1.out && ok "완료 메시지" || ng "완료 메시지"
$H/stop.sh

echo "== V2 스펙 후보 2개(모호) → 수동 선택, 포트그룹 2개는 스펙 폴더명으로 자동 할당"
A=$($H/sim.sh v2 -dc 1 -cluster 0 -host 2)
SD=$T/SPEC2; mkspec $SD TST-CAE001-AAA-QRST; mkspec $SD TST-CAE001-BBB-QRST
printf 'DC0_H0 TST-CAE001-AAA-QRST-cae-10-1-1-1 100\nDC0_H0 TST-CAE001-BBB-QRST-cae-10-1-2-2 200\nDC0_H1 TST-CAE001-AAA-QRST-cae-10-1-1-1 100\n' > $SD/vswitch_tt.txt
# 스펙 표: DC0_H0 는 미할당 → 목록에서 1번(AAA) 선택 → 표 y → VM 표(자동: H0 는 스펙 폴더명과 맞는 AAA 포트그룹) y   (-n: 실행 안 함)
printf '1\ny\ny\n' | run_setup $SD $A -n > $T/v2.out 2>&1
grep -q 'DC0_H0ev01 *DC0_H0 *TST-CAE001-AAA-QRST-cae-10-1-1-1' $T/v2.out && ok "BM 에 포트그룹 2개일 때 스펙 폴더명과 맞는 것을 자동 선택" || { ng "포트그룹 자동 선택"; tail -30 $T/v2.out; }
grep -q '\-n 지정' $T/v2.out && [ "$($H/bin/vmdump -vc $A -match ev0 | wc -l)" = 0 ] && ok "-n: vCenter 를 변경하지 않고 종료" || ng "-n"
$H/stop.sh

echo "== V3 vim 경로: 스펙 수동 입력(오류 후 재편집) + ev 별 affinity(자동/vim) + 포트그룹 수동/vim"
A=$($H/sim.sh v3 -dc 1 -cluster 0 -host 2)
SD=$T/SPEC3; mkdir -p $SD
printf 'DC0_H0 PG-P 100\nDC0_H0 PG-Q 200\nDC0_H1 PG-P 100\n' > $SD/vswitch_tt.txt
echo 0 > $T/fake_spec_n; : > $T/fake_count
export VM_SETUP_EDITOR=$T/fake_editor.sh FAKE_BAD_FIRST=1 FAKE_NIC_HOST=DC0_H0ev02 FAKE_NIC_PG=PG-P
# 스펙: H0 → 0(vim) → 잘못된 폴더명 → 다시 편집 y → 성공 → ev01 affinity Enter(자동) → ev02 affinity 3(vim)
#       H1 → Enter(이전 선택) → 표 y
# VM 표: H0 의 두 VM 미할당 → ev01=2번(PG-Q), ev02=0(vim → PG-P) → 표 y → 실행 y
printf '0\ny\n\n3\n\ny\n2\n0\ny\ny\n' | run_setup $SD $A > $T/v3.out 2>&1
unset FAKE_BAD_FIRST FAKE_NIC_HOST FAKE_NIC_PG VM_SETUP_EDITOR
grep -q "CAE 폴더 규칙" $T/v3.out && ok "잘못된 폴더명은 오류 안내 후 다시 편집" || { ng "폴더명 오류 안내"; grep -n "오류" $T/v3.out | head; }
[ -f $SD/TST-CAE002-NEW-QRST/TST-CAE002-NEW-QRST_spec.txt ] && ok "vim 입력이 SPEC_DIR 아래 새 폴더로 저장" || { ng "새 스펙 폴더"; ls -R $SD; }
grep -q '^affinity-ev02=affinity_ev02.txt' $SD/TST-CAE002-NEW-QRST/TST-CAE002-NEW-QRST_spec.txt && ! grep -q '^affinity-ev01' $SD/TST-CAE002-NEW-QRST/TST-CAE002-NEW-QRST_spec.txt && ok "ev01 자동(키 없음), ev02 vim 입력(파일 지정)" || { ng "affinity 키"; cat $SD/TST-CAE002-NEW-QRST/*_spec.txt; }
grep -q '^sched.vcpu1.affinity=1,9' $SD/TST-CAE002-NEW-QRST/affinity_ev02.txt && ok "affinity 템플릿(vCPU 수만큼 줄) 입력 반영" || { ng "affinity 파일"; cat $SD/TST-CAE002-NEW-QRST/affinity_ev02.txt; }
$H/bin/vmdump -vc $A -match 'ev0' > $T/v3.dump
[ "$(wc -l < $T/v3.dump)" = 4 ] && ok "4대 생성" || { ng "생성 대수"; tail -30 $T/v3.out; }
grep -q 'DC0_H0ev01 .*:PG-Q:' $T/v3.dump && grep -q 'DC0_H0ev02 .*:PG-P:' $T/v3.dump && grep -q 'DC0_H1ev01 .*:PG-P:' $T/v3.dump && ok "수동 선택(ev01=PG-Q) / vim 지정(ev02=PG-P) / 단일 포트그룹 자동(H1)" || { ng "포트그룹 할당"; cat $T/v3.dump | cut -c1-200; }
grep -q 'DC0_H0ev02 .*sched.vcpu1.affinity=1,9' $T/v3.dump && grep -q 'DC0_H1ev01 .*sched.vcpu1.affinity=2,3' $T/v3.dump && ok "affinity: ev02 는 vim 입력값, ev01 은 자동 계산" || { ng "affinity 적용"; cut -c1-260 $T/v3.dump; }
$H/stop.sh

echo "== V4 n → 전체 수동 선택 경로 (-n)"
A=$($H/sim.sh v4 -dc 1 -cluster 0 -host 2)
SD=$T/SPEC4; mkspec $SD TST-CAE001-AAA-QRST; mkspec $SD TST-CAE001-BBB-QRST
printf 'DC0_H0 TST-CAE001-AAA-QRST-cae-10-1-1-1 100\nDC0_H1 TST-CAE001-AAA-QRST-cae-10-1-1-1 100\n' > $SD/vswitch_tt.txt
# 자동(AAA) → n → BM 마다 번호 선택(H0=2번 BBB, H1=Enter 이전 선택) → 표 y → VM 표 n → 4대 Enter → 표 y (-n)
printf 'n\n2\n\ny\nn\n\n\n\n\ny\n' | run_setup $SD $A -n > $T/v4.out 2>&1
grep -c 'TST-CAE001-BBB-QRST' $T/v4.out | grep -qv '^0$' && ok "n 이면 목록에서 다시 선택 (Enter=이전 선택)" || { ng "수동 선택 경로"; tail -30 $T/v4.out; }
grep -q '스펙 1 *: TST-CAE001-BBB-QRST — BM 2대' $T/v4.out && ok "선택한 스펙 하나로 BM 2대 묶임" || { ng "스펙 묶기"; tail -20 $T/v4.out; }

$H/stop.sh

echo "== V5 a<번호>: 한 BM 의 VM 전체에 같은 포트그룹 적용 (-n)"
A=$($H/sim.sh v5 -dc 1 -cluster 0 -host 2)
SD=$T/SPEC5; mkspec $SD TST-CAE001-AAA-QRST
printf 'DC0_H0 TST-CAE001-AAA-QRST-cae-10-1-1-1 100\nDC0_H0 TST-CAE001-AAA-QRST-cae-10-1-2-2 200\nDC0_H1 TST-CAE001-AAA-QRST-cae-10-1-1-1 100\n' > $SD/vswitch_tt.txt
# 스펙 y → H0 의 VM 2대는 미할당 → 첫 VM 에서 a2 (이 BM 전체 = 2번 포트그룹) → 표 y
printf 'y\na2\ny\n' | run_setup $SD $A -n > $T/v5.out 2>&1
[ "$(grep -c 'DC0_H0ev0[12] *DC0_H0 *TST-CAE001-AAA-QRST-cae-10-1-2-2' $T/v5.out)" -ge 2 ] && ok "a2 한 번으로 BM 의 VM 2대 모두 2번 포트그룹" || { ng "a<번호>"; tail -25 $T/v5.out; }

$H/stop.sh

echo "== V6 도메인이 붙은 BM 이름(bm1.example.com): VM 이름은 bm1ev01 — affinity/lpage 는 짧은 이름 목록으로 찾아야 한다"
A=$($H/sim.sh v6 -dc 1 -cluster 0 -host 0 -fqdnHosts bm1.example.com)
SD=$T/SPEC6; mkspec $SD TST-CAE001-AAA-QRST
printf 'bm1.example.com\n' > $V/tt.txt
printf 'bm1.example.com TST-CAE001-AAA-QRST-cae-10-1-1-1 100\n' > $SD/vswitch_tt.txt
printf 'y\ny\ny\n' | run_setup $SD $A > $T/v6.out 2>&1
$H/bin/vmdump -vc $A -match '^bm1ev0' > $T/v6.dump
[ "$(wc -l < $T/v6.dump)" = 2 ] && ok "bm1.example.com → VM bm1ev01/bm1ev02 생성(vm_create 는 . 앞부분 사용)" || { ng "FQDN 생성"; tail -15 $T/v6.out; }
grep -q 'bm1ev01 .*sched.vcpu1.affinity=2,3' $T/v6.dump && ok "affinity_setting 이 짧은 이름 목록으로 VM 을 찾음" || { ng "FQDN affinity"; tail -15 $T/v6.out; }
grep -q 'bm1ev02 .*sched.mem.lpage.enable1GPage=TRUE' $T/v6.dump && ok "lpage_setting 이 짧은 이름 목록으로 VM 을 찾음" || { ng "FQDN lpage"; tail -15 $T/v6.out; }

echo; echo "결과: PASS=$pass FAIL=$fail"
