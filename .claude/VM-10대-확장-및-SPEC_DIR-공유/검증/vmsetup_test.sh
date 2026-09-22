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
cleanup() { $H/stop.sh; rm -f $V/tt.txt $V/../vcenter.txt; rm -rf $V/run_tt; }
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

mkspec() { # <SPEC_DIR> <폴더명> [noaff] — ev01/ev02 두 개짜리 스펙. ev01·ev02 가 같은 affinity 파일(affinity_ev01.txt)을 쓴다
  mkdir -p "$1/$2"
  printf 'ht=on\ncpu=2\nmem=1\ndisk=1\nshares-ev01=normal\ncores=2\nnuma=2\ncpu-ev02=2\nmem-ev02=1\ndisk-ev02=1\nshares-ev02=4000\ncores-ev02=2\nnuma-ev02=2\n' > "$1/$2/$2_spec.txt"
  [ "${3:-}" = noaff ] && return
  printf 'sched.vcpu0.affinity=0,1\nsched.vcpu1.affinity=2,3\n' > "$1/$2/affinity_ev01.txt"
  printf 'affinity-ev01=affinity_ev01.txt\naffinity-ev02=affinity_ev01.txt\n' >> "$1/$2/$2_spec.txt"
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
grep -q 'DC0_H1ev02 .*sched.vcpu1.affinity=2,3' $T/v1.dump && grep -q 'DC0_H1ev01 .*sched.vcpu1.affinity=2,3' $T/v1.dump && ok "ev01·ev02 가 같은 affinity 파일(affinity_ev01.txt)로 적용" || { ng "affinity"; cat $T/v1.dump; }
grep -q 'DC0_H0ev01 .*sched.mem.lpage.enable1GPage=TRUE' $T/v1.dump && ok "lpage_setting 적용(1GB 페이지 설정)" || { ng "lpage"; cat $T/v1.dump; }
grep -q 'DC0_H0ev02 .*cpuShares=custom/4000' $T/v1.dump && ok "스펙 shares-ev02=4000 적용" || ng "shares"
grep -q '\[완료\]' $T/v1.out && ok "완료 메시지" || ng "완료 메시지"
printf 'y\ny\ny\n' | run_setup $SD $A > $T/v1b.out 2>&1; rc=$?
[ $rc -eq 0 ] && grep -q '이미 존재' $T/v1b.out && grep -q '이미 모두' $T/v1b.out && grep -q '\[완료\]' $T/v1b.out && ok "재실행: 이미 있는 포트그룹/VM 은 건너뛰고 정상 완료" || { ng "재실행 rc=$rc"; tail -20 $T/v1b.out; }
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

echo "== V3 vim 경로: 스펙 수동 입력(오류 후 재편집) + ev 별 affinity(ev01 vim / ev02 같은 파일) + 포트그룹 수동/vim"
A=$($H/sim.sh v3 -dc 1 -cluster 0 -host 2)
SD=$T/SPEC3; mkdir -p $SD
printf 'DC0_H0 PG-P 100\nDC0_H0 PG-Q 200\nDC0_H1 PG-P 100\n' > $SD/vswitch_tt.txt
echo 0 > $T/fake_spec_n; : > $T/fake_count
export VM_SETUP_EDITOR=$T/fake_editor.sh FAKE_BAD_FIRST=1 FAKE_NIC_HOST=DC0_H0ev02 FAKE_NIC_PG=PG-P
# 스펙: H0 → 0(vim) → 잘못된 폴더명 → 다시 편집 y → 성공 → ev01 affinity 3(vim) → ev02 affinity Enter(직전 ev 와 같은 파일)
#       H1 → Enter(이전 선택) → 표 y
# VM 표: H0 의 두 VM 미할당 → ev01=2번(PG-Q), ev02=0(vim → PG-P) → 표 y → 실행 y
printf '0\ny\n3\n\n\ny\n2\n0\ny\ny\n' | run_setup $SD $A > $T/v3.out 2>&1
unset FAKE_BAD_FIRST FAKE_NIC_HOST FAKE_NIC_PG VM_SETUP_EDITOR
NS=$SD/TST-CAE002-NEW-QRST
grep -q "CAE 폴더 규칙" $T/v3.out && ok "잘못된 폴더명은 오류 안내 후 다시 편집" || { ng "폴더명 오류 안내"; grep -n "오류" $T/v3.out | head; }
[ -f $NS/TST-CAE002-NEW-QRST_spec.txt ] && ok "vim 입력이 SPEC_DIR 아래 새 폴더로 저장" || { ng "새 스펙 폴더"; ls -R $SD; }
grep -q '^affinity-ev01=affinity_ev01.txt' $NS/*_spec.txt && grep -q '^affinity-ev02=affinity_ev01.txt' $NS/*_spec.txt && [ ! -e $NS/affinity_ev02.txt ] && ok "ev01 vim 입력 파일을 ev02 가 그대로 사용(공통 파일, 복사본 없음)" || { ng "affinity 키"; cat $NS/*_spec.txt; ls $NS; }
grep -q '^sched.vcpu1.affinity=1,9' $NS/affinity_ev01.txt && ok "affinity 템플릿(vCPU 수만큼 줄) 입력 반영" || { ng "affinity 파일"; cat $NS/affinity_ev01.txt; }
grep -q '선택 (2/3)' $T/v3.out && ! grep -q '1) 자동' $T/v3.out && ok "affinity 선택지에 자동 없음, ev01 은 파일/vim 만" || { ng "affinity 선택지"; grep -n 'affinity 지정' -A4 $T/v3.out | head -12; }
$H/bin/vmdump -vc $A -match 'ev0' > $T/v3.dump
[ "$(wc -l < $T/v3.dump)" = 4 ] && ok "4대 생성" || { ng "생성 대수"; tail -30 $T/v3.out; }
grep -q 'DC0_H0ev01 .*:PG-Q:' $T/v3.dump && grep -q 'DC0_H0ev02 .*:PG-P:' $T/v3.dump && grep -q 'DC0_H1ev01 .*:PG-P:' $T/v3.dump && ok "수동 선택(ev01=PG-Q) / vim 지정(ev02=PG-P) / 단일 포트그룹 자동(H1)" || { ng "포트그룹 할당"; cat $T/v3.dump | cut -c1-200; }
grep -q 'DC0_H0ev02 .*sched.vcpu1.affinity=1,9' $T/v3.dump && grep -q 'DC0_H1ev01 .*sched.vcpu1.affinity=1,9' $T/v3.dump && ok "affinity: ev01·ev02 모두 vim 입력값 적용" || { ng "affinity 적용"; cut -c1-260 $T/v3.dump; }
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
$H/stop.sh

echo "== V6b vCenter 에는 FQDN(bm1.example.com), 파일에는 짧은 이름(bm1) — 포트그룹/VM 모두 만들어져야 한다"
A=$($H/sim.sh v6b -dc 1 -cluster 0 -host 0 -fqdnHosts bm1.example.com)
SD=$T/SPEC6b; mkspec $SD TST-CAE001-AAA-QRST
printf 'bm1\n' > $V/tt.txt
printf 'bm1 TST-CAE001-AAA-QRST-cae-10-1-1-1 100\n' > $SD/vswitch_tt.txt
printf 'y\ny\ny\n' | run_setup $SD $A > $T/v6b.out 2>&1; rc=$?
$H/bin/vmdump -vc $A -match '^bm1ev0' > $T/v6b.dump
[ $rc -eq 0 ] && grep -q 'bm1\] 성공: 포트그룹' $T/v6b.out && [ "$(grep -c ':TST-CAE001-AAA-QRST-cae-10-1-1-1:' $T/v6b.dump)" = 2 ] && ok "짧은 이름으로 FQDN 호스트에 포트그룹 생성 + VM 2대 어댑터 연결" || { ng "짧은 이름 → FQDN rc=$rc"; tail -25 $T/v6b.out; cat $T/v6b.dump; }
$H/stop.sh

echo "== V7 vCenter: vcenter.txt 번호 선택 + user 별 이전 실행 기억"
A=$($H/sim.sh v7 -dc 1 -cluster 0 -host 1)
SD=$T/SPEC7; mkspec $SD TST-CAE001-AAA-QRST
printf 'DC0_H0\n' > $V/tt.txt
printf 'DC0_H0 TST-CAE001-AAA-QRST-cae-10-1-1-1 100\n' > $SD/vswitch_tt.txt
rm -rf $V/run_tt
printf '# 주석 줄\n10.9.9.9\n%s\n' "$A" > $V/../vcenter.txt
# 스펙 y → 포트그룹 y → vCenter 2번 → 실행 y
printf 'y\ny\n2\ny\n' | $V/vm_setup.sh -u tt -s $SD > $T/v7.out 2>&1
grep -q '=== vCenter 선택' $T/v7.out && grep -q "  2) $A" $T/v7.out && grep -q "vCenter        : $A" $T/v7.out && ok "vcenter.txt 목록(주석 제외)에서 번호로 선택 → 실행 계획에 표시" || { ng "vCenter 번호 선택"; grep -n 'vCenter' $T/v7.out; }
[ "$(awk '{print $1}' $V/run_tt/last_vcenter 2>/dev/null)" = "$A" ] && [ "$($H/bin/vmdump -vc $A -match ev0 | wc -l)" = 2 ] && ok "실행 후 run_tt/last_vcenter 에 기억 + VM 생성" || { ng "기억/생성"; cat $V/run_tt/last_vcenter; tail -10 $T/v7.out; }
# 두 번째 실행: 이전 실행이 출력되고 Enter 로 그대로 선택 → 마지막 확인 n(취소)
printf 'y\ny\n\nn\n' | $V/vm_setup.sh -u tt -s $SD > $T/v7b.out 2>&1
grep -q "\[INFO\] tt 이전 실행 vCenter: $A (" $T/v7b.out && grep -q "  2) $A   <- 이전 실행" $T/v7b.out && grep -q "vCenter        : $A" $T/v7b.out && grep -q '취소했습니다' $T/v7b.out && ok "재실행: 이전 실행 vCenter 출력 + 목록 표시 + Enter = 이전 실행" || { ng "이전 실행 기억"; grep -n 'vCenter\|이전' $T/v7b.out; }
# -n 은 vCenter 를 묻지 않는다
printf 'y\ny\n' | $V/vm_setup.sh -u tt -s $SD -n > $T/v7c.out 2>&1
! grep -q '=== vCenter 선택' $T/v7c.out && grep -q '\-n 지정' $T/v7c.out && ok "-n 은 vCenter 선택을 묻지 않음" || { ng "-n vCenter"; tail -10 $T/v7c.out; }
# V2 폴더에 vcenter.txt 가 없으면 vm-param-check 폴더의 것을 쓴다(원래 파일이 있으면 건드리지 않음)
CV=$V/../vm-param-check-usability-improvement/vm-param-check/vcenter.txt
if [ ! -e $CV ]; then
  rm -f $V/../vcenter.txt; printf '%s\n' "$A" > $CV
  printf 'y\ny\n1\nn\n' | $V/vm_setup.sh -u tt -s $SD > $T/v7d.out 2>&1
  grep -q "vm-param-check/vcenter.txt" $T/v7d.out && grep -q "vCenter        : $A" $T/v7d.out && ok "V2 폴더에 없으면 vm-param-check 폴더의 vcenter.txt 사용" || { ng "vcenter.txt 대체 경로"; grep -n 'vCenter' $T/v7d.out; }
  rm -f $CV
fi
$H/stop.sh

echo "== V8 vim 스펙의 affinity '2) 기존 파일': 다른 폴더 파일은 복사, 같은 폴더 파일은 그대로 가리킴 (-n)"
SD=$T/SPEC8; mkspec $SD TST-CAE001-AAA-QRST
printf 'DC0_H0\n' > $V/tt.txt
printf 'DC0_H0 PG-P 100\n' > $SD/vswitch_tt.txt
export VM_SETUP_EDITOR=$T/fake_editor.sh
# 스펙 0(vim) → ev01: 2 → 1번(AAA/affinity_ev01.txt, 복사) → ev02: 2 → 2번(새 폴더의 affinity_ev01.txt, 참조) → 표 y → 포트그룹 표 y
printf '0\n2\n1\n2\n2\ny\ny\n' | $V/vm_setup.sh -u tt -v 127.0.0.1:1 -s $SD -n > $T/v8.out 2>&1
unset VM_SETUP_EDITOR
NS=$SD/TST-CAE002-NEW-QRST
cmp -s $NS/affinity_ev01.txt $SD/TST-CAE001-AAA-QRST/affinity_ev01.txt && grep -q '^affinity-ev01=affinity_ev01.txt' $NS/*_spec.txt && grep -q '^affinity-ev02=affinity_ev01.txt' $NS/*_spec.txt && [ ! -e $NS/affinity_ev02.txt ] && ok "다른 폴더 파일 → 새 폴더로 복사, 같은 폴더 파일 → 복사 없이 공통 사용" || { ng "기존 파일 선택"; ls $NS; cat $NS/*_spec.txt; tail -20 $T/v8.out; }
grep -q '\-affinityFile01=.*affinity_ev01.txt -affinityFile02=.*affinity_ev01.txt' $T/v8.out && ok "실행 계획: -affinityFile01/02 가 같은 파일" || { ng "계획 affinity"; grep -n affinity_setting $T/v8.out; }

echo "== V9 affinity 가 없는 스펙은 자동 할당하지 않고 이유를 알린다"
SD=$T/SPEC9; mkspec $SD TST-CAE001-AAA-QRST noaff
printf 'DC0_H0 TST-CAE001-AAA-QRST-cae-10-1-1-1 100\n' > $SD/vswitch_tt.txt
: | $V/vm_setup.sh -u tt -v 127.0.0.1:1 -s $SD -n > $T/v9.out 2>&1
grep -q '자동 매칭된 스펙 TST-CAE001-AAA-QRST 을(를) 쓸 수 없습니다 — affinity-ev01 이 스펙에 없습니다' $T/v9.out && ok "affinity-ev01 없음 → 경고 후 미할당" || { ng "affinity 없는 스펙"; head -20 $T/v9.out; }

echo "== V10 vm_create 가 호스트를 못 찾으면 vm_setup 이 멈춘다([완료] 없음, affinity 실행 안 함)"
A=$($H/sim.sh v10 -dc 1 -cluster 0 -host 1)
SD=$T/SPEC10; mkspec $SD TST-CAE001-AAA-QRST
printf 'DC0_H0\nDC0_H9\n' > $V/tt.txt
printf 'DC0_H0 TST-CAE001-AAA-QRST-cae-10-1-1-1 100\n' > $SD/vswitch_tt.txt
# H9 는 포트그룹이 없어 스펙 수동 1번 → 표 y → 포트그룹 표 y → 실행 y
printf '1\ny\ny\ny\n' | run_setup $SD $A > $T/v10.out 2>&1; rc=$?
[ $rc -ne 0 ] && grep -q '\[DC0_H9\] vCenter에서 호스트를 찾을 수 없습니다' $T/v10.out && grep -q '실패: .*vm_create' $T/v10.out && ! grep -q '\[완료\]' $T/v10.out && ! grep -q 'affinity_setting-source/affinity_setting -vc' $T/v10.out && ok "없는 호스트 → vm_create 종료코드 1 → vm_setup 중단" || { ng "중단 rc=$rc"; tail -20 $T/v10.out; }
[ "$($H/bin/vmdump -vc $A -match '^DC0_H0ev0' | wc -l)" = 2 ] && ok "찾은 호스트(DC0_H0)의 VM 은 만들어짐 — 고친 뒤 재실행하면 건너뜀" || ng "H0 VM"

echo; echo "결과: PASS=$pass FAIL=$fail"
