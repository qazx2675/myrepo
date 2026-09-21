#!/usr/bin/env bash
# scenarios.sh — V2 VMsetup 도구 vcsim 검증 시나리오 (수정 전 baseline_bin 과 수정 후 V2 바이너리 비교)
set -uo pipefail
H=/root/v2work/harness
OLD=/root/v2work/baseline_bin
NEW=/root/v2work/V2/VMsetup
export VC_PASSWORD=x
W=/tmp/scen; rm -rf $W; mkdir -p $W; cd $W
pass=0; fail=0
ok()  { echo "  [PASS] $*"; pass=$((pass+1)); }
ng()  { echo "  [FAIL] $*"; fail=$((fail+1)); }
trap "$H/stop.sh" EXIT

EV3="-ev01Cpu=4 -ev01Mem=2 -ev01Disk=2 -ev01Share=nomal -ev02Cpu=2 -ev02Mem=1 -ev02Disk=1 -ev02Share=4000 -ev03Cpu=1 -ev03Mem=1 -ev03Disk=1 -ev03Share=1000"
EV10="$EV3"; for n in 04 05 06 07 08 09 10; do EV10="$EV10 -ev${n}Cpu=1 -ev${n}Mem=1 -ev${n}Disk=1 -ev${n}Share=normal"; done

echo "== R1 회귀: affinity/lpage (단일 DC, ev01~ev03) 수정 전/후 결과 동일"
A=$($H/sim.sh r1a -dc 1 -cluster 1 -clusterHost 2 -host 2); B=$($H/sim.sh r1b -dc 1 -cluster 1 -clusterHost 2 -host 2)
printf "DC0_H0\nDC0_H1\nDC0_C0_H0\nDC0_C0_H1\n" > worklist.txt
: > hostgroup.txt
for s in "$OLD/vm_create -vcTargetIP=$A" "$NEW/vm_create-source/vm_create -vcTargetIP=$B"; do $s -id=user -vmCount=3 $EV3 >/dev/null 2>&1; done
$OLD/affinity_setting -vcTargetIP=$A -id=user -vm_cnt=3 > aff_old.txt 2>&1
$NEW/affinity_setting-source/affinity_setting -vcTargetIP=$B -id=user -vm_cnt=3 > aff_new.txt 2>&1
$OLD/lpage_setting -vcTargetIP=$A -id=user -ev01Cores=4 -ev01Sockets=1 -ev01Numa=1 -ev02Cores=2 -ev02Sockets=2 -ev03Cores=1 -ev03Sockets=1 > lp_old.txt 2>&1
$NEW/lpage_setting-source/lpage_setting -vcTargetIP=$B -id=user -ev01Cores=4 -ev01Sockets=1 -ev01Numa=1 -ev02Cores=2 -ev02Sockets=2 -ev03Cores=1 -ev03Sockets=1 > lp_new.txt 2>&1
$H/bin/vmdump -vc $A -match ev0 > r1_old.dump; $H/bin/vmdump -vc $B -match ev0 > r1_new.dump
[ -s r1_old.dump ] && diff -q r1_old.dump r1_new.dump >/dev/null && ok "VM 12대 affinity/lpage 적용 결과 동일 ($(wc -l < r1_new.dump)대)" || { ng "덤프 차이"; diff r1_old.dump r1_new.dump | head; }
diff <(grep -v "vCenter   :" aff_old.txt | sort) <(grep -v "vCenter   :" aff_new.txt | sort) >/dev/null && ok "affinity 출력 동일" || { ng "affinity 출력 차이"; diff <(sort aff_old.txt) <(sort aff_new.txt) | head; }
diff <(sort lp_old.txt) <(sort lp_new.txt) >/dev/null && ok "lpage 출력 동일" || { ng "lpage 출력 차이"; diff <(sort lp_old.txt) <(sort lp_new.txt) | head; }
$H/stop.sh

echo "== S1 호스트당 10대 (vm_create/affinity/lpage/tag ev01~ev10)"
A=$($H/sim.sh s1 -dc 1 -cluster 1 -clusterHost 2 -host 2)
$NEW/vm_create-source/vm_create -vcTargetIP=$A -id=user -vmCount=10 $EV10 > s1_create.txt 2>&1
n=$($H/bin/vmdump -vc $A -match 'ev(0[1-9]|10)$' | wc -l); [ "$n" = 40 ] && ok "4호스트 x 10대 = 40대 생성" || { ng "생성 대수 $n"; tail -5 s1_create.txt; }
$H/bin/vmdump -vc $A -match 'DC0_H0ev10$' | grep -q "cpuShares=normal/0" && ok "ev10 Shares=normal 적용" || ng "ev10 normal 미적용"
$NEW/affinity_setting-source/affinity_setting -vcTargetIP=$A -id=user -vm_cnt=10 -ht=OFF > s1_aff.txt 2>&1
$H/bin/vmdump -vc $A -match 'DC0_C0_H1ev10$' | grep -q "sched.vcpu0.affinity=0" && ok "affinity ev10 적용" || { ng "affinity ev10"; tail -5 s1_aff.txt; }
L=""; for n in 01 02 03 04 05 06 07 08 09 10; do L="$L -ev${n}Cores=1 -ev${n}Sockets=1"; done
$NEW/lpage_setting-source/lpage_setting -vcTargetIP=$A -id=user $L > s1_lp.txt 2>&1
grep -q "40" s1_lp.txt && ok "lpage ev01~ev10 40대 대상" || { ng "lpage"; tail -5 s1_lp.txt; }
$OLD/vm_create -vcTargetIP=$A -id=user -vmCount=10 $EV3 > s1_old.txt 2>&1; grep -q "1~3대" s1_old.txt && ok "(비교) 수정 전 vm_create는 -vmCount=10 거부" || ng "수정 전 동작 확인 실패"
$H/stop.sh

echo "== S2 규칙: ev 번호 누락 에러 / 값 없는 ev 제외"
A=$($H/sim.sh s2 -dc 1 -cluster 0 -host 1)
printf "DC0_H0\n" > worklist.txt
$NEW/vm_create-source/vm_create -vcTargetIP=$A -id=user -vmCount=3 -ev01Cpu=1 -ev01Mem=1 -ev01Disk=1 -ev03Cpu=1 -ev03Mem=1 -ev03Disk=1 > s2a.txt 2>&1
grep -q "연속" s2a.txt && ok "ev02 없이 ev03 → 에러로 중단" || { ng "연속 규칙"; cat s2a.txt; }
$NEW/vm_create-source/vm_create -vcTargetIP=$A -id=user -vmCount=3 -ev01Cpu=1 -ev01Mem=1 -ev01Disk=1 -ev02Cpu=1 -ev02Mem=1 -ev02Disk=1 > s2b.txt 2>&1
n=$($H/bin/vmdump -vc $A -match 'ev0' | wc -l); grep -q "만들지 않습니다" s2b.txt && [ "$n" = 2 ] && ok "ev03 값 없음 → ev01/ev02 2대만 생성 + 경고" || { ng "ev03 제외 ($n대)"; cat s2b.txt; }
$NEW/vm_create-source/vm_create -vcTargetIP=$A -id=user -vmCount=1 -ev01Cpu=1 -ev01Mem=1 -ev01Disk=1 > s2c.txt 2>&1
grep -q "완료\|이미 모두" s2c.txt && ok "vmCount=1 + ev02 값 없음 허용(예전엔 종료)" || { ng "vmCount=1"; cat s2c.txt; }
$H/stop.sh

echo "== S3 데이터센터 3개 + 호스트/VM 폴더 3단계"
A=$($H/sim.sh s3 -dc 3 -cluster 1 -clusterHost 1 -host 1 -machine 1 -nest 3)
printf "DC0_H0\nDC1_C0_H0\nDC2_H0\n" > worklist.txt
T0=$(date +%s.%N); $NEW/vm_create-source/vm_create -vcTargetIP=$A -id=user -vmCount=2 -ev01Cpu=1 -ev01Mem=1 -ev01Disk=1 -ev02Cpu=1 -ev02Mem=1 -ev02Disk=1 > s3_create.txt 2>&1; T1=$(date +%s.%N)
n=$($H/bin/vmdump -vc $A -match '^(DC0_H0|DC1_C0_H0|DC2_H0)ev0' | wc -l); [ "$n" = 6 ] && ok "DC 3개에 걸친 호스트 3대 x 2 = 6대 생성 ($(echo "$T1-$T0" | bc)s)" || { ng "다중 DC 생성 $n"; cat s3_create.txt; }
$H/bin/vmdump -vc $A -match '^DC2_H0ev01' | grep -q "host=DC2_H0 folder=vm" && ok "호스트가 속한 DC의 VM 폴더에 생성" || { ng "폴더"; $H/bin/vmdump -vc $A -match '^DC2_H0ev01'; }
$OLD/vm_create -vcTargetIP=$A -id=user -vmCount=1 -ev01Cpu=1 -ev01Mem=1 -ev01Disk=1 -ev02Cpu=1 > s3_old.txt 2>&1; grep -q "자동 선택이 불가" s3_old.txt && ok "(비교) 수정 전 vm_create는 다중 DC에서 종료" || ng "수정 전 비교"
$NEW/lpage_setting-source/lpage_setting -vcTargetIP=$A -id=user -ev01Cores=1 -ev01Sockets=1 > s3_lp.txt 2>&1; grep -q "총 3개" s3_lp.txt && ok "lpage: 다중 DC+중첩 폴더 VM 3대 처리" || { ng "lpage 다중 DC"; tail -3 s3_lp.txt; }
$OLD/lpage_setting -vcTargetIP=$A -id=user -ev01Cores=1 -ev01Sockets=1 > s3_lp_old.txt 2>&1; grep -q "총 3개" s3_lp_old.txt && ng "(비교) 수정 전 lpage도 성공함 — 문제 재현 안 됨" || ok "(비교) 수정 전 lpage는 다중 DC에서 실패: $(tail -1 s3_lp_old.txt | cut -c1-80)"
$NEW/affinity_setting-source/affinity_setting -vcTargetIP=$A -id=user -vm_cnt=2 > s3_aff.txt 2>&1; grep -q "성공\|완료" s3_aff.txt && ok "affinity: 다중 DC+중첩 폴더" || { ng "affinity 다중 DC"; tail -3 s3_aff.txt; }
printf "DC0_H0 PG-DC0 10\nDC2_H0 PG-DC2 20\n" > vsw.txt
$NEW/vswitch_setting-source/vswitch_setting -vcTargetIP=$A -id=user -worklistFile=vsw.txt > s3_vsw.txt 2>&1; [ "$(grep -c 성공 s3_vsw.txt)" = 2 ] && ok "vswitch: 다중 DC 호스트 포트그룹 생성" || { ng "vswitch 다중 DC"; cat s3_vsw.txt; }
$NEW/mac_info-source/mac_info -vcTargetIP=$A -id=user -arg1=a -argInt=1 -argStr=b > s3_mac.txt 2>&1; ls *.txt | grep -qi "prov\|list" ; tail -2 s3_mac.txt | sed 's/^/     mac_info: /'
T0=$(date +%s.%N); $NEW/vm_create-source/vm_create -vcTargetIP=$A -id=user -vmCount=2 -ev01Cpu=1 -ev01Mem=1 -ev01Disk=1 -ev02Cpu=1 -ev02Mem=1 -ev02Disk=1 >/dev/null 2>&1; T1=$(date +%s.%N); echo "     재실행(생성 대상 없음, 조회만) DC3개: $(echo "$T1-$T0" | bc)s"
$H/stop.sh
A=$($H/sim.sh s3b -dc 1 -cluster 1 -clusterHost 1 -host 1 -machine 1)
printf "DC0_H0\nDC0_C0_H0\n" > worklist.txt
$NEW/vm_create-source/vm_create -vcTargetIP=$A -id=user -vmCount=2 -ev01Cpu=1 -ev01Mem=1 -ev01Disk=1 -ev02Cpu=1 -ev02Mem=1 -ev02Disk=1 >/dev/null 2>&1
T0=$(date +%s.%N); $NEW/vm_create-source/vm_create -vcTargetIP=$A -id=user -vmCount=2 -ev01Cpu=1 -ev01Mem=1 -ev01Disk=1 -ev02Cpu=1 -ev02Mem=1 -ev02Disk=1 >/dev/null 2>&1; T1=$(date +%s.%N); echo "     재실행(생성 대상 없음, 조회만) DC1개(수정 후): $(echo "$T1-$T0" | bc)s"
T0=$(date +%s.%N); $OLD/vm_create -vcTargetIP=$A -id=user -vmCount=2 -ev01Cpu=1 -ev01Mem=1 -ev01Disk=1 -ev02Cpu=1 -ev02Mem=1 -ev02Disk=1 >/dev/null 2>&1; T1=$(date +%s.%N); echo "     재실행(생성 대상 없음, 조회만) DC1개(수정 전): $(echo "$T1-$T0" | bc)s"
$H/stop.sh

echo "== S4 한 BM에 포트그룹 2개 + VM별 어댑터 할당"
A=$($H/sim.sh s4 -dc 1 -cluster 0 -host 1)
printf "DC0_H0 PG-A 100\nDC0_H0 PG-B 200\n" > vsw.txt
$NEW/vswitch_setting-source/vswitch_setting -vcTargetIP=$A -id=user -worklistFile=vsw.txt > s4_vsw.txt 2>&1
[ "$(grep -c 성공 s4_vsw.txt)" = 2 ] && ok "같은 BM에 포트그룹 2개 생성" || { ng "포트그룹 2개"; cat s4_vsw.txt; }
B=$($H/sim.sh s4old -dc 1 -cluster 0 -host 1); $OLD/vswitch_setting -vcTargetIP=$B -id=user -worklistFile=vsw.txt > s4_vsw_old.txt 2>&1; [ "$(grep -c 성공 s4_vsw_old.txt)" = 2 ] && ok "(비교) 수정 전 vswitch도 같은 BM 포트그룹 2개 생성 — 다중 포트그룹은 원래 지원" || { ng "수정 전 vswitch 비교"; cat s4_vsw_old.txt; }
printf "DC0_H0\n" > worklist.txt
printf "DC0_H0 PG-A\nDC0_H0ev02 PG-B\n" > hostgroup.txt
$NEW/vm_create-source/vm_create -vcTargetIP=$A -id=user -vmCount=3 $EV3 > s4_create.txt 2>&1
$H/bin/vmdump -vc $A -match 'ev0' > s4.dump
grep -q "DC0_H0ev01 .*nic=.*:PG-A:" s4.dump && grep -q "DC0_H0ev02 .*nic=.*:PG-B:" s4.dump && grep -q "DC0_H0ev03 .*nic=.*:PG-A:" s4.dump && ok "mapFile: BM 키=PG-A, VM 키(ev02)=PG-B 로 생성" || { ng "VM 키 매핑"; cat s4.dump; cat s4_create.txt; }
printf "DC0_H0ev01 PG-B\nDC0_H0ev03 PG-B\n" > nic_map.txt
$NEW/nic_assign-source/nic_assign -vcTargetIP=$A -id=user -mapFile=nic_map.txt > s4_nic.txt 2>&1
$H/bin/vmdump -vc $A -match 'ev0' > s4b.dump
grep -q "DC0_H0ev01 .*:PG-B:conn=false/start=true" s4b.dump && grep -q "DC0_H0ev03 .*:PG-B:conn=false/start=true" s4b.dump && ok "nic_assign: 전원 꺼진 VM 어댑터 1 → PG-B, 전원 켤 때 연결 체크" || { ng "nic_assign off"; cat s4_nic.txt; cat s4b.dump; }
$NEW/nic_assign-source/nic_assign -vcTargetIP=$A -id=user -mapFile=nic_map.txt > s4_nic2.txt 2>&1; [ "$(grep -c '이미 적용됨' s4_nic2.txt)" = 2 ] && ok "nic_assign 재실행 멱등(이미 적용됨)" || { ng "멱등"; cat s4_nic2.txt; }
echo "     (전원 켜진 VM의 '연결됨' 체크는 vcsim이 런타임 연결을 흉내내지 않아 실환경(home-test)에서 확인)"
$H/stop.sh

echo; echo "결과: PASS=$pass FAIL=$fail"
