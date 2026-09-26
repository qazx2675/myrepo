#!/usr/bin/env bash
# vmsetup_test.sh — VMsetup/vm_setup.sh 를 vcsim 으로 시험한다(록키 /root/v2work 기준).
# 대화형 입력은 표준입력으로 흘려 넣고, vim 은 가짜 편집기(VM_SETUP_EDITOR)로 바꿔서 템플릿을 채운다.
set -uo pipefail
H=/root/v2work/harness
V=/root/v2work/V2/VMsetup
export VC_PASSWORD=x
# vm_setup.sh 가 시작할 때 매번 물어보는 "설치 정보"(OS 버전/인프라, 환경변수로는 못 건너뜀) —
# 아래 모든 입력 앞에 이 두 줄(INSTALL_ANS)을 붙여서 표준입력으로 넣는다.
INSTALL_ANS=$'8.10\nvmsetup-test\n'
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
run_setup() { # <SPEC_DIR> <vc주소> <추가옵션...>  (표준입력 = 답변, INSTALL_ANS 는 자동으로 앞에 붙는다)
  local sd=$1 vc=$2; shift 2
  { printf '%s' "$INSTALL_ANS"; cat; } | $V/vm_setup.sh -u tt -v "$vc" -s "$sd" "$@"
}

echo "== V1 자동 할당(스펙·포트그룹) → 확인 y → 실행"
A=$($H/sim.sh v1 -dc 1 -cluster 0 -host 2)
SD=$T/SPEC1; mkspec $SD TST-CAE001-SAMP48c-QRST
printf 'DC0_H0\nDC0_H1\n' > $V/tt.txt
printf 'DC0_H0 TST-CAE001-SAMP48c-QRST-cae-10-1-2-3 100\nDC0_H1 TST-CAE001-SAMP48c-QRST-cae-10-1-2-3 100\n' > $SD/vswitch_tt.txt
printf 'y\n\ny\n' | run_setup $SD $A > $T/v1.out 2>&1
$H/bin/vmdump -vc $A -match 'ev0' > $T/v1.dump
[ "$(wc -l < $T/v1.dump)" = 4 ] && ok "BM 2대 x ev 2개 = 4대 생성" || { ng "생성 대수"; tail -20 $T/v1.out; }
grep -q 'nic=.*:TST-CAE001-SAMP48c-QRST-cae-10-1-2-3:' $T/v1.dump && ok "자동 할당된 포트그룹이 어댑터에 연결" || { ng "포트그룹"; cat $T/v1.dump; }
grep -q 'DC0_H1ev02 .*sched.vcpu1.affinity=2,3' $T/v1.dump && grep -q 'DC0_H1ev01 .*sched.vcpu1.affinity=2,3' $T/v1.dump && ok "ev01·ev02 가 같은 affinity 파일(affinity_ev01.txt)로 적용" || { ng "affinity"; cat $T/v1.dump; }
grep -q 'DC0_H0ev01 .*sched.mem.lpage.enable1GPage=TRUE' $T/v1.dump && ok "lpage_setting 적용(1GB 페이지 설정)" || { ng "lpage"; cat $T/v1.dump; }
grep -q 'DC0_H0ev02 .*cpuShares=custom/4000' $T/v1.dump && ok "스펙 shares-ev02=4000 적용" || ng "shares"
grep -q '\[완료\]' $T/v1.out && ok "완료 메시지" || ng "완료 메시지"
printf 'y\n\ny\n' | run_setup $SD $A > $T/v1b.out 2>&1; rc=$?
[ $rc -eq 0 ] && grep -q '이미 존재' $T/v1b.out && grep -q '이미 모두' $T/v1b.out && grep -q '\[완료\]' $T/v1b.out && ok "재실행: 이미 있는 포트그룹/VM 은 건너뛰고 정상 완료" || { ng "재실행 rc=$rc"; tail -20 $T/v1b.out; }
$H/stop.sh

echo "== V2 스펙 후보 2개(모호) → 수동 선택, 포트그룹 2개는 스펙 폴더명으로 자동 할당"
A=$($H/sim.sh v2 -dc 1 -cluster 0 -host 2)
SD=$T/SPEC2; mkspec $SD TST-CAE001-AAA-QRST; mkspec $SD TST-CAE001-BBB-QRST
printf 'DC0_H0 TST-CAE001-AAA-QRST-cae-10-1-1-1 100\nDC0_H0 TST-CAE001-BBB-QRST-cae-10-1-2-2 200\nDC0_H1 TST-CAE001-AAA-QRST-cae-10-1-1-1 100\n' > $SD/vswitch_tt.txt
# DC0_H0 는 미할당 → 목록에서 1번(AAA) 선택 → VM 표(자동: H0 는 스펙 폴더명과 맞는 AAA 포트그룹) y → CAE 번호 Enter   (-n: 실행 안 함)
printf '1\ny\n\n' | run_setup $SD $A -n > $T/v2.out 2>&1
grep -q 'DC0_H0ev01 *DC0_H0 *TST-CAE001-AAA-QRST *TST-CAE001-AAA-QRST-cae-10-1-1-1' $T/v2.out && ok "BM 에 포트그룹 2개일 때 스펙 폴더명과 맞는 것을 자동 선택" || { ng "포트그룹 자동 선택"; tail -30 $T/v2.out; }
grep -q '\-n 지정' $T/v2.out && [ "$($H/bin/vmdump -vc $A -match ev0 | wc -l)" = 0 ] && ok "-n: vCenter 를 변경하지 않고 종료" || ng "-n"
$H/stop.sh

echo "== V3 vim 경로: 스펙 수동 입력(오류 후 재편집) + ev 별 affinity(ev01 vim / ev02 같은 파일) + 포트그룹 수동/vim"
A=$($H/sim.sh v3 -dc 1 -cluster 0 -host 2)
SD=$T/SPEC3; mkdir -p $SD
printf 'DC0_H0 PG-P 100\nDC0_H0 PG-Q 200\nDC0_H1 PG-P 100\n' > $SD/vswitch_tt.txt
echo 0 > $T/fake_spec_n; : > $T/fake_count
export VM_SETUP_EDITOR=$T/fake_editor.sh FAKE_BAD_FIRST=1 FAKE_NIC_HOST=DC0_H0ev02 FAKE_NIC_PG=PG-P
# 스펙: H0 → 0(vim) → 잘못된 폴더명 → 다시 편집 y → 성공 → ev01 affinity 3(vim) → ev02 affinity Enter(직전 ev 와 같은 파일)
#       H1 → Enter(이전 선택)   (BM→스펙 표 확인은 없다)
# VM 표: H0 의 두 VM 미할당 → ev01=2번(PG-Q), ev02=0(vim → PG-P) → 표 y → 실행 y
printf '0\ny\n3\n\n\n2\n0\ny\ny\n' | run_setup $SD $A > $T/v3.out 2>&1
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

echo "== V4 BM→스펙 확인 표 없음 (VM 표에 스펙 열) + VM 표 n → 수동 선택 (-n)"
A=$($H/sim.sh v4 -dc 1 -cluster 0 -host 2)
SD=$T/SPEC4; mkspec $SD TST-CAE001-AAA-QRST; mkspec $SD TST-CAE001-BBB-QRST
printf 'DC0_H0 TST-CAE001-AAA-QRST-cae-10-1-1-1 100\nDC0_H1 TST-CAE001-AAA-QRST-cae-10-1-1-1 100\n' > $SD/vswitch_tt.txt
# 자동(AAA) → VM 표 n → 4대 Enter(현재값 유지) → 표 y → CAE 번호 Enter (-n)
printf 'n\n\n\n\n\ny\n\n' | run_setup $SD $A -n > $T/v4.out 2>&1
! grep -q '스펙 할당이 맞습니까' $T/v4.out && ! grep -q '=== BM → 스펙' $T/v4.out && grep -q 'VM → 스펙 / 포트그룹' $T/v4.out && grep -q 'DC0_H1ev02 *DC0_H1 *TST-CAE001-AAA-QRST ' $T/v4.out && ok "BM→스펙 확인 단계 없음, VM 표에 스펙 열" || { ng "VM 표 스펙 열"; head -30 $T/v4.out; }
grep -q '스펙 1 *: TST-CAE001-AAA-QRST — BM 2대' $T/v4.out && grep -q '\-n 지정' $T/v4.out && ok "VM 표 n → 수동 선택(Enter 유지) 후 계획까지" || { ng "스펙 묶기"; tail -20 $T/v4.out; }

$H/stop.sh

echo "== V5 a<번호>: 한 BM 의 VM 전체에 같은 포트그룹 적용 (-n)"
A=$($H/sim.sh v5 -dc 1 -cluster 0 -host 2)
SD=$T/SPEC5; mkspec $SD TST-CAE001-AAA-QRST
printf 'DC0_H0 TST-CAE001-AAA-QRST-cae-10-1-1-1 100\nDC0_H0 TST-CAE001-AAA-QRST-cae-10-1-2-2 200\nDC0_H1 TST-CAE001-AAA-QRST-cae-10-1-1-1 100\n' > $SD/vswitch_tt.txt
# H0 의 VM 2대는 미할당 → 첫 VM 에서 a2 (이 BM 전체 = 2번 포트그룹) → 표 y → CAE 번호 Enter
printf 'a2\ny\n\n' | run_setup $SD $A -n > $T/v5.out 2>&1
[ "$(grep -c 'DC0_H0ev0[12] *DC0_H0 *TST-CAE001-AAA-QRST *TST-CAE001-AAA-QRST-cae-10-1-2-2' $T/v5.out)" -ge 2 ] && ok "a2 한 번으로 BM 의 VM 2대 모두 2번 포트그룹" || { ng "a<번호>"; tail -25 $T/v5.out; }

$H/stop.sh

echo "== V6 도메인이 붙은 BM 이름(bm1.example.com): VM 이름은 bm1ev01 — affinity/lpage 는 짧은 이름 목록으로 찾아야 한다"
A=$($H/sim.sh v6 -dc 1 -cluster 0 -host 0 -fqdnHosts bm1.example.com)
SD=$T/SPEC6; mkspec $SD TST-CAE001-AAA-QRST
printf 'bm1.example.com\n' > $V/tt.txt
printf 'bm1.example.com TST-CAE001-AAA-QRST-cae-10-1-1-1 100\n' > $SD/vswitch_tt.txt
printf 'y\n\ny\n' | run_setup $SD $A > $T/v6.out 2>&1
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
printf 'y\n\ny\n' | run_setup $SD $A > $T/v6b.out 2>&1; rc=$?
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
# VM 표 y → CAE 번호 Enter → vCenter 2번 → 실행 y
printf '%sy\n\n2\ny\n' "$INSTALL_ANS" | $V/vm_setup.sh -u tt -s $SD > $T/v7.out 2>&1
grep -q '=== vCenter 선택' $T/v7.out && grep -q "  2) $A" $T/v7.out && grep -q "vCenter        : $A" $T/v7.out && ok "vcenter.txt 목록(주석 제외)에서 번호로 선택 → 실행 계획에 표시" || { ng "vCenter 번호 선택"; grep -n 'vCenter' $T/v7.out; }
[ "$(awk '{print $1}' $V/run_tt/last_vcenter 2>/dev/null)" = "$A" ] && [ "$($H/bin/vmdump -vc $A -match ev0 | wc -l)" = 2 ] && ok "실행 후 run_tt/last_vcenter 에 기억 + VM 생성" || { ng "기억/생성"; cat $V/run_tt/last_vcenter; tail -10 $T/v7.out; }
# 두 번째 실행: 이전 실행이 출력되고 Enter 로 그대로 선택 → 마지막 확인 n(취소)
printf '%sy\n\n\nn\n' "$INSTALL_ANS" | $V/vm_setup.sh -u tt -s $SD > $T/v7b.out 2>&1
grep -q "\[INFO\] tt 이전 실행 vCenter: $A (" $T/v7b.out && grep -q "  2) $A   <- 이전 실행" $T/v7b.out && grep -q "vCenter        : $A" $T/v7b.out && grep -q '취소했습니다' $T/v7b.out && ok "재실행: 이전 실행 vCenter 출력 + 목록 표시 + Enter = 이전 실행" || { ng "이전 실행 기억"; grep -n 'vCenter\|이전' $T/v7b.out; }
# -n 은 vCenter 를 묻지 않는다
printf '%sy\n\n' "$INSTALL_ANS" | $V/vm_setup.sh -u tt -s $SD -n > $T/v7c.out 2>&1
! grep -q '=== vCenter 선택' $T/v7c.out && grep -q '\-n 지정' $T/v7c.out && ok "-n 은 vCenter 선택을 묻지 않음" || { ng "-n vCenter"; tail -10 $T/v7c.out; }
# V2 폴더에 vcenter.txt 가 없으면 vm-param-check 폴더의 것을 쓴다(원래 파일이 있으면 건드리지 않음)
CV=$V/../vm-param-check-usability-improvement/vm-param-check/vcenter.txt
if [ ! -e $CV ]; then
  rm -f $V/../vcenter.txt; printf '%s\n' "$A" > $CV
  printf '%sy\n\n1\nn\n' "$INSTALL_ANS" | $V/vm_setup.sh -u tt -s $SD > $T/v7d.out 2>&1
  grep -q "vm-param-check/vcenter.txt" $T/v7d.out && grep -q "vCenter        : $A" $T/v7d.out && ok "V2 폴더에 없으면 vm-param-check 폴더의 vcenter.txt 사용" || { ng "vcenter.txt 대체 경로"; grep -n 'vCenter' $T/v7d.out; }
  rm -f $CV
fi
$H/stop.sh

echo "== V8 vim 스펙의 affinity '2) 기존 파일': 다른 폴더 파일은 복사, 같은 폴더 파일은 그대로 가리킴 (-n)"
SD=$T/SPEC8; mkspec $SD TST-CAE001-AAA-QRST
printf 'DC0_H0\n' > $V/tt.txt
printf 'DC0_H0 PG-P 100\n' > $SD/vswitch_tt.txt
export VM_SETUP_EDITOR=$T/fake_editor.sh
# 스펙 0(vim) → ev01: 2 → 1번(AAA/affinity_ev01.txt, 복사) → ev02: 2 → 2번(새 폴더의 affinity_ev01.txt, 참조) → VM 표 y
printf '%s0\n2\n1\n2\n2\ny\n' "$INSTALL_ANS" | $V/vm_setup.sh -u tt -v 127.0.0.1:1 -s $SD -n > $T/v8.out 2>&1
unset VM_SETUP_EDITOR
NS=$SD/TST-CAE002-NEW-QRST
cmp -s $NS/affinity_ev01.txt $SD/TST-CAE001-AAA-QRST/affinity_ev01.txt && grep -q '^affinity-ev01=affinity_ev01.txt' $NS/*_spec.txt && grep -q '^affinity-ev02=affinity_ev01.txt' $NS/*_spec.txt && [ ! -e $NS/affinity_ev02.txt ] && ok "다른 폴더 파일 → 새 폴더로 복사, 같은 폴더 파일 → 복사 없이 공통 사용" || { ng "기존 파일 선택"; ls $NS; cat $NS/*_spec.txt; tail -20 $T/v8.out; }
grep -q '\-affinityFile01=.*affinity_ev01.txt -affinityFile02=.*affinity_ev01.txt' $T/v8.out && ok "실행 계획: -affinityFile01/02 가 같은 파일" || { ng "계획 affinity"; grep -n affinity_setting $T/v8.out; }

echo "== V9 affinity 가 없는 스펙: 지금 추가할지 묻고, n 이면 자동 할당하지 않고 이유를 알린다"
SD=$T/SPEC9; mkspec $SD TST-CAE001-AAA-QRST noaff
printf 'DC0_H0 TST-CAE001-AAA-QRST-cae-10-1-1-1 100\n' > $SD/vswitch_tt.txt
printf '%sn\n' "$INSTALL_ANS" | $V/vm_setup.sh -u tt -v 127.0.0.1:1 -s $SD -n > $T/v9.out 2>&1
grep -q '지금 ev 별 affinity 를 골라 이 스펙에 추가할까요' $T/v9.out && grep -q '자동 매칭된 스펙 TST-CAE001-AAA-QRST 을(를) 쓸 수 없습니다 — affinity-ev01 이 스펙에 없습니다' $T/v9.out && ok "affinity-ev01 없음 → 경고 후 미할당" || { ng "affinity 없는 스펙"; head -20 $T/v9.out; }

echo "== V10 vm_create 가 호스트를 못 찾으면 vm_setup 이 멈춘다([완료] 없음, affinity 실행 안 함)"
A=$($H/sim.sh v10 -dc 1 -cluster 0 -host 1)
SD=$T/SPEC10; mkspec $SD TST-CAE001-AAA-QRST
printf 'DC0_H0\nDC0_H9\n' > $V/tt.txt
printf 'DC0_H0 TST-CAE001-AAA-QRST-cae-10-1-1-1 100\n' > $SD/vswitch_tt.txt
# H9 는 포트그룹이 없어 스펙 수동 1번 → VM 표 y → CAE 번호 Enter → 실행 y
printf '1\ny\n\ny\n' | run_setup $SD $A > $T/v10.out 2>&1; rc=$?
[ $rc -ne 0 ] && grep -q '\[DC0_H9\] vCenter에서 호스트를 찾을 수 없습니다' $T/v10.out && grep -q '실패: .*vm_create' $T/v10.out && ! grep -q '\[완료\]' $T/v10.out && ! grep -q 'affinity_setting-source/affinity_setting -vc' $T/v10.out && ok "없는 호스트 → vm_create 종료코드 1 → vm_setup 중단" || { ng "중단 rc=$rc"; tail -20 $T/v10.out; }
[ "$($H/bin/vmdump -vc $A -match '^DC0_H0ev0' | wc -l)" = 2 ] && ok "찾은 호스트(DC0_H0)의 VM 은 만들어짐 — 고친 뒤 재실행하면 건너뜀" || ng "H0 VM"

echo "== V11 ev01~ev12 스펙(템플릿 틀 밖 ev11/ev12 포함, 모두 같은 affinity 파일) → 실행"
A=$($H/sim.sh v11 -dc 1 -cluster 0 -host 2)
SD=$T/SPEC11; mkspec $SD TST-CAE001-AAA-QRST
S11=$SD/TST-CAE001-AAA-QRST/TST-CAE001-AAA-QRST_spec.txt
for i in $(seq 3 12); do n=$(printf %02d $i); printf 'cpu-ev%s=2\nmem-ev%s=1\ndisk-ev%s=1\nshares-ev%s=normal\ncores-ev%s=2\naffinity-ev%s=affinity_ev01.txt\n' $n $n $n $n $n $n >> $S11; done
printf 'DC0_H0\nDC0_H1\n' > $V/tt.txt
printf 'DC0_H0 TST-CAE001-AAA-QRST-cae-10-1-1-1 100\nDC0_H1 TST-CAE001-AAA-QRST-cae-10-1-1-1 100\n' > $SD/vswitch_tt.txt
printf 'y\n\ny\n' | run_setup $SD $A > $T/v11.out 2>&1; rc=$?
$H/bin/vmdump -vc $A -match 'ev[0-9][0-9]$' > $T/v11.dump
[ $rc -eq 0 ] && [ "$(wc -l < $T/v11.dump)" = 24 ] && grep -q '\[완료\]' $T/v11.out && ok "BM 2대 x ev 12개 = 24대 생성 + 완료" || { ng "ev12 rc=$rc $(wc -l < $T/v11.dump)대"; tail -20 $T/v11.out; }
grep -q 'DC0_H1ev12 .*sched.vcpu1.affinity=2,3' $T/v11.dump && grep -q 'DC0_H1ev12 .*lpage' $T/v11.dump && ok "ev12 에 affinity(ev01 과 같은 파일)/lpage 적용" || { ng "ev12 설정"; grep ev12 $T/v11.dump; }
$H/stop.sh

echo "== V12 -u 없이 실행: user 번호 메뉴 (0) list = 각 user 의 BM 목록)"
SD=$T/SPEC12; mkspec $SD TST-CAE001-AAA-QRST
printf 'DC0_H0\nDC0_H1\n' > $V/tt.txt
printf 'DC0_H0 TST-CAE001-AAA-QRST-cae-10-1-1-1 100\nDC0_H1 TST-CAE001-AAA-QRST-cae-10-1-1-1 100\n' > $SD/vswitch_tt.txt
printf '0\n' | $V/vm_setup.sh -v 127.0.0.1:1 -s $SD -n > $T/v12.out 2>&1   # 0 → 목록 → 다시 메뉴 → 입력 끝
grep -q '=== user 선택' $T/v12.out && grep -q '  0) list' $T/v12.out && grep -Eq '^  [0-9]+\) tt$' $T/v12.out && grep -q 'tt  (BM 2대, 포트그룹 파일 vswitch_tt.txt)' $T/v12.out && grep -q 'DC0_H0 DC0_H1' $T/v12.out && ok "user 메뉴 + 0) list 로 BM 목록 보기" || { ng "user 메뉴"; head -20 $T/v12.out; }
n=$(grep -E '^  [0-9]+\) tt$' $T/v12.out | head -1 | sed 's/^ *\([0-9]*\)).*/\1/')
printf '%s\n%sy\n\n' "$n" "$INSTALL_ANS" | $V/vm_setup.sh -v 127.0.0.1:1 -s $SD -n > $T/v12b.out 2>&1
grep -q '\[INFO\] user tt — BM 2대' $T/v12b.out && grep -q '\-n 지정' $T/v12b.out && ok "번호로 user 선택 → 계획까지" || { ng "user 번호 선택"; tail -10 $T/v12b.out; }

echo "== V13 CAE 번호: 번호를 빼고 스펙 매칭 + 실행 중 번호 변경(포트그룹 이름) + 주석처리하면 꺼짐 (-n)"
SD=$T/SPEC13; mkspec $SD TST-CAE001-AAA-QRST
printf 'DC0_H0\n' > $V/tt.txt
printf 'DC0_H0 TST-CAE050-AAA-QRST-cae-10-1-1-1 100\n' > $SD/vswitch_tt.txt
printf '%sy\ny\n777\n' "$INSTALL_ANS" | $V/vm_setup.sh -u tt -v 127.0.0.1:1 -s $SD -n > $T/v13.out 2>&1
grep -q 'DC0_H0ev01 *DC0_H0 *TST-CAE001-AAA-QRST ' $T/v13.out && ok "포트그룹 TST-CAE050 → 스펙 TST-CAE001 (번호 무시 매칭)" || { ng "번호 무시 매칭"; head -20 $T/v13.out; }
grep -q '^DC0_H0 TST-CAE777-AAA-QRST-cae-10-1-1-1 100$' $V/run_tt/vswitch.txt && grep -q '^DC0_H0ev02 TST-CAE777-AAA-QRST-cae-10-1-1-1$' $V/run_tt/hostgroup.txt && ok "CAE 번호 777 로 변경 → BM 포트그룹·VM 어댑터 이름에 반영" || { ng "번호 변경"; cat $V/run_tt/vswitch.txt $V/run_tt/hostgroup.txt; }
grep -q '^# 숫자변경기능' $V/vm_setup.sh && sed 's/^ask_cae_number$/# ask_cae_number/' $V/vm_setup.sh > $V/vm_setup_nocae.sh && chmod +x $V/vm_setup_nocae.sh
printf '%sy\n' "$INSTALL_ANS" | $V/vm_setup_nocae.sh -u tt -v 127.0.0.1:1 -s $SD -n > $T/v13b.out 2>&1
! grep -q 'CAE 번호를 바꾸시겠습니까' $T/v13b.out && grep -q '\-n 지정' $T/v13b.out && ok "'숫자변경기능' 아래 호출을 주석처리하면 질문 없이 진행" || { ng "기능 끄기"; tail -8 $T/v13b.out; }
rm -f $V/vm_setup_nocae.sh

echo "== V14 affinity 설정값 출력(내용이 같으면 한 번) + 생성 뒤 vm-param-check 스펙 체크"
[ "$(grep -c '^     \[ev01~ev02\] affinity_ev01.txt$' $T/v1.out)" = 1 ] && grep -q '^        sched.vcpu1.affinity=2,3$' $T/v1.out && ok "ev01·ev02 같은 파일 → 한 번만 출력(내용 포함)" || { ng "affinity 출력"; grep -n -A4 'affinity 설정값' $T/v1.out; }
SD=$T/SPEC14; mkspec $SD TST-CAE001-AAA-QRST
cp $SD/TST-CAE001-AAA-QRST/affinity_ev01.txt $SD/TST-CAE001-AAA-QRST/affinity_copy.txt
sed -i 's/^affinity-ev02=.*/affinity-ev02=affinity_copy.txt/' $SD/TST-CAE001-AAA-QRST/TST-CAE001-AAA-QRST_spec.txt
printf 'DC0_H0\n' > $V/tt.txt; printf 'DC0_H0 TST-CAE001-AAA-QRST-cae-10-1-1-1 100\n' > $SD/vswitch_tt.txt
printf '%sy\n\n' "$INSTALL_ANS" | $V/vm_setup.sh -u tt -v 127.0.0.1:1 -s $SD -n > $T/v14.out 2>&1
grep -q '^     \[ev01~ev02\] affinity_ev01.txt, affinity_copy.txt$' $T/v14.out && ok "이름이 달라도 내용이 같으면 한 항목으로" || { ng "내용 기준 묶기"; grep -n -A4 'affinity 설정값' $T/v14.out; }
grep -q '=== 스펙 체크 — vm-param-check' $T/v1.out && grep -Eq '\[(일치|차이)\] 스펙 1 TST-CAE001-SAMP48c-QRST — VM 4대' $T/v1.out && ok "생성 뒤 만든 VM 4대를 실행한 스펙으로 체크" || { ng "스펙 체크"; sed -n '/스펙 체크/,$p' $T/v1.out | head; }

echo "== V15 기존 vm-param-check 폴더에 SPEC_DIR 을 복원해 두면 그것을 쓴다 (-n)"
CS=$V/../vm-param-check-usability-improvement/vm-param-check/SPEC_DIR
if [ ! -e $CS ]; then
  mkspec $CS TST-CAE001-AAA-QRST; printf 'DC0_H0 TST-CAE001-AAA-QRST-cae-10-1-1-1 100\n' > $CS/vswitch_tt.txt
  printf '%sy\n\n' "$INSTALL_ANS" | $V/vm_setup.sh -u tt -v 127.0.0.1:1 -n > $T/v15.out 2>&1
  grep -q 'SPEC_DIR=.*vm-param-check/SPEC_DIR' $T/v15.out && grep -q '스펙 1 *: TST-CAE001-AAA-QRST' $T/v15.out && ok "-s 없으면 vm-param-check/SPEC_DIR 을 먼저 사용" || { ng "SPEC_DIR 연동"; head -5 $T/v15.out; }
  rm -rf $CS
fi

echo "== V16 affinity 없는 스펙에 지금 추가 (y → ev01 vim, ev02 같은 파일) (-n)"
SD=$T/SPEC16; mkspec $SD TST-CAE001-AAA-QRST noaff
printf 'DC0_H0\n' > $V/tt.txt; printf 'DC0_H0 TST-CAE001-AAA-QRST-cae-10-1-1-1 100\n' > $SD/vswitch_tt.txt
: > $T/fake_count; VM_SETUP_EDITOR=$T/fake_editor.sh INSTALL_ANS="$INSTALL_ANS" bash -c "printf '%sy\n3\n\ny\n\n' \"\$INSTALL_ANS\" | $V/vm_setup.sh -u tt -v 127.0.0.1:1 -s $SD -n" > $T/v16.out 2>&1
F16=$SD/TST-CAE001-AAA-QRST/TST-CAE001-AAA-QRST_spec.txt
grep -q '^affinity-ev01=affinity_ev01.txt$' $F16 && grep -q '^affinity-ev02=affinity_ev01.txt$' $F16 && grep -q 'affinity 를 추가했습니다' $T/v16.out && grep -q '\-n 지정' $T/v16.out && ok "스펙 파일에 affinity-ev01/02 추가 후 그대로 진행" || { ng "affinity 추가"; cat $F16; tail -10 $T/v16.out; }

echo "== V17 계정 -id + 암호 파일(환경변수 없이), 틀린 비밀번호는 로그인 실패로 중단"
A=$($H/sim.sh v17 -dc 1 -cluster 0 -host 1 -username tester@vsphere.local -password 'S3cret!')
SD=$T/SPEC17; mkspec $SD TST-CAE001-AAA-QRST
printf 'DC0_H0\n' > $V/tt.txt; printf 'DC0_H0 TST-CAE001-AAA-QRST-cae-10-1-1-1 100\n' > $SD/vswitch_tt.txt
export VMSETUP_SECRET_DIR=$T/secret
bash -c ". $V/../secret_lib.sh && secret_put vcenter tester@vsphere.local 'S3cret!'"
( unset VC_PASSWORD; printf '%sy\n\ny\n' "$INSTALL_ANS" | $V/vm_setup.sh -u tt -v $A -s $SD -id tester@vsphere.local ) > $T/v17.out 2>&1; rc=$?
[ $rc -eq 0 ] && grep -q 'tester@vsphere.local 비밀번호: 암호 파일에서 읽었습니다' $T/v17.out && [ "$(VC_PASSWORD='S3cret!' $H/bin/vmdump -vc $A -id tester@vsphere.local -match ev0 | wc -l)" = 2 ] && ok "-id 계정 + 암호 파일로 로그인해 VM 2대 생성" || { ng "암호 파일 로그인 rc=$rc"; tail -15 $T/v17.out; }
bash -c ". $V/../secret_lib.sh && secret_put vcenter tester@vsphere.local 'wrong'"
( unset VC_PASSWORD; printf '%sy\n\ny\n' "$INSTALL_ANS" | $V/vm_setup.sh -u tt -v $A -s $SD -id tester@vsphere.local ) > $T/v17b.out 2>&1; rc=$?
[ $rc -ne 0 ] && grep -q 'Login failure' $T/v17b.out && grep -q '실패: vswitch_setting (종료코드 1)' $T/v17b.out && ok "틀린 비밀번호 → 로그인 실패, 종료코드 1 로 중단" || { ng "틀린 비밀번호 rc=$rc"; tail -8 $T/v17b.out; }
unset VMSETUP_SECRET_DIR
$H/stop.sh

echo; echo "결과: PASS=$pass FAIL=$fail"
