#!/usr/bin/env bash
# timing.sh — vm_create 조회 경로(생성 대상 없음) 수정 전/후 10회 평균, 호스트 20대
set -uo pipefail
H=/root/v2work/harness; OLD=/root/v2work/baseline_bin/vm_create; NEW=/root/v2work/V2/VMsetup/vm_create-source/vm_create
export VC_PASSWORD=x; W=/tmp/timing; rm -rf $W; mkdir -p $W; cd $W; trap "$H/stop.sh" EXIT
ARGS="-id=user -vmCount=2 -ev01Cpu=1 -ev01Mem=1 -ev01Disk=1 -ev02Cpu=1 -ev02Mem=1 -ev02Disk=1"
avg() { local t0 t1; t0=$(date +%s.%N); for i in $(seq 10); do "$@" >/dev/null 2>&1; done; t1=$(date +%s.%N); echo "scale=4; ($t1-$t0)/10" | bc; }
A=$($H/sim.sh t1 -dc 1 -cluster 2 -clusterHost 5 -host 10)
$H/bin/vmdump -vc $A -match . >/dev/null
awk 'BEGIN{for(i=0;i<10;i++)print "DC0_H"i; for(c=0;c<2;c++)for(h=0;h<5;h++)print "DC0_C"c"_H"h}' > worklist.txt; : > hostgroup.txt
$NEW -vcTargetIP=$A $ARGS >/dev/null 2>&1   # 40대 생성(이후 실행은 조회만)
echo "DC 1개, 호스트 20대, 조회만 10회 평균: 수정 전 $(avg $OLD -vcTargetIP=$A $ARGS)s / 수정 후 $(avg $NEW -vcTargetIP=$A $ARGS)s"
$H/stop.sh
B=$($H/sim.sh t3 -dc 3 -cluster 2 -clusterHost 5 -host 10 -nest 3)
awk 'BEGIN{for(d=0;d<3;d++){for(i=0;i<10;i++)print "DC"d"_H"i; for(c=0;c<2;c++)for(h=0;h<5;h++)print "DC"d"_C"c"_H"h}}' > worklist.txt
$NEW -vcTargetIP=$B $ARGS > create3.txt 2>&1; echo "DC 3개(중첩 폴더 3단계), 호스트 60대 최초 생성: $(grep -c . create3.txt)줄 로그, 결과: $(tail -1 create3.txt)"
echo "DC 3개(중첩 폴더 3단계), 호스트 60대, 조회만 10회 평균: 수정 후 $(avg $NEW -vcTargetIP=$B $ARGS)s (수정 전은 다중 DC에서 실행 불가)"
