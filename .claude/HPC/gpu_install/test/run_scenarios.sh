#!/bin/bash
# run_scenarios.sh - 관리 서버에서 gpu_install 시나리오(PLAN.md 8장 T1~T10)를 실제로 돌리고 판정한다 (테스트 전용).
# 미리: 관리 서버에서 test/setup_mgmt.sh, 대상에서 test/setup_target.sh <관리서버IP> 를 한 번 실행.
# 사용법: bash test/run_scenarios.sh <대상IP> [시나리오 ...]   (예: T1 T2, 비우면 전부)
#         SCP_TGT=<비마운트 대상IP> bash test/run_scenarios.sh <마운트 대상IP> TM
#           TM: 두 대를 한 list 에 넣고 T1(버전 질문 550)·T2(580 재설치) → 두 서버 최종 상태 비교
# 결과: test/out/<날짜시각>/<시나리오>/ 에 화면 출력(out.txt)·list·fail_list·logs, 마지막에 통과/실패 표.
TGT="${1:?사용법: bash test/run_scenarios.sh <대상IP> [시나리오 ...]}"
shift
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
OUT="$ROOT/test/out/$(date +%Y%m%d_%H%M%S)"
AP=/user/gpuadm/gpu
AP_ALT=/user/gpuadm/gpu_alt
MU=gpuadm
SCEN=("$@")
[ ${#SCEN[@]} -eq 0 ] && SCEN=(T1 T2 T3 T4 T5 T6 T7a T7b T8a T8b T8c T9 T10)
SUMMARY=()

# 대상에서 bash 로 실행. root 셸이 csh 여도 되게 표준입력으로 넘긴다(비대화형이라 가짜 도구가 PATH 앞)
t() { printf '%s\n' "$*" | ssh -o BatchMode=yes "root@${TH:-$TGT}" bash 2>/dev/null; }

# 대상 초기화: DCGM 패키지·가짜 GPU 상태 지움, rhel.repo(enabled=0, 원래 baseurl), 자동 마운트 켬
reset_target() {
    t 'rpm -qa "datacenter-gpu-manager-4-*" fake-dcgm-dep | xargs -r rpm -e --nodeps >/dev/null 2>&1
       systemctl reset-failed nvidia-dcgm >/dev/null 2>&1
       rm -rf /var/lib/fakegpu/* /tmp/gpu_install /var/log/nvidia-installer.log
       echo 1 > /var/lib/fakegpu/gpus
       rm -f /etc/yum.repos.d/rhel.repo.gpu_install.orig
       sed -i "s|^baseurl=.*|baseurl=file:///export/repo|; s/^enabled=.*/enabled=0/" /etc/yum.repos.d/rhel.repo
       systemctl start user-gpuadm.automount export-repo.automount'
}
# 자동 마운트를 멈추고 /user/<계정> 을 없애 비마운트 서버(scp)처럼 만든다
automount_off() {
    t 'systemctl stop user-gpuadm.automount user-gpuadm.mount; rmdir /user/gpuadm 2>/dev/null; true'
}
# 드라이버가 이미 깔리고 모듈이 로드된 상태를 만든다: preinstall <버전> <CUDA>
preinstall() {
    t "echo $1 > /var/lib/fakegpu/version; echo $2 > /var/lib/fakegpu/cuda; printf '%s\n' nvidia nvidia_modeset nvidia_uvm nvidia_drm > /var/lib/fakegpu/modules"
}
# 대상 최종 상태 한 줄 (mount/scp 결과 비교용)
state() {
    t 'S=/var/lib/fakegpu
       echo "driver=$(nvidia-smi --query-gpu=driver_version --format=csv,noheader 2>/dev/null | head -n 1)" \
            "cuda=$(nvidia-smi 2>/dev/null | sed -n "s/.*CUDA Version: *\([0-9.]*\).*/\1/p")" \
            "pkgs=$(rpm -qa --qf "%{NAME}\n" "datacenter-gpu-manager-4-*" | sed s/datacenter-gpu-manager-4-// | sort | paste -sd, -)" \
            "dcgm=$(systemctl is-active nvidia-dcgm 2>/dev/null)/$(systemctl is-enabled nvidia-dcgm 2>/dev/null)/$(dcgmi discovery -l 2>/dev/null | sed -n "s/ GPUs\{0,1\} found.*//p")" \
            "repo=$(grep -E "^enabled" /etc/yum.repos.d/rhel.repo)" \
            "orig=$(ls /etc/yum.repos.d/rhel.repo.gpu_install.orig 2>/dev/null | wc -l)" \
            "tmp=$(ls /tmp/gpu_install 2>/dev/null | wc -l)"'
}
history_n() { t 'grep -c " run " /var/lib/fakegpu/history 2>/dev/null || echo 0'; }

# gpu_install.sh 를 맨 위 변수만 바꾼 사본으로 실행: run_gi <입력> <MOUNT_USER> [gpu_install 인자...]
# list 는 $W/list, 결과는 $W/out.txt, 종료코드는 RC
run_gi() {
    local input="$1" mu="$2"
    shift 2
    sed -e "s|^AUTOFS_PATH=\"\"|AUTOFS_PATH=\"$AP\"|" -e "s|^MOUNT_USER=\"\"|MOUNT_USER=\"$mu\"|" \
        "$ROOT/gpu_install.sh" > "$ROOT/.gi_test.sh"
    (cd "$W" && printf "$input" | bash "$ROOT/.gi_test.sh" "$@" > out.txt 2>&1)
    RC=$?
    rm -f "$ROOT/.gi_test.sh"
}

# 시나리오 시작: begin <이름> <list 내용>
begin() {
    NAME="$1"
    W="$OUT/$1"
    mkdir -p "$W"
    printf "$2" > "$W/list"
    BAD=()
    echo "=== $1"
}
# 판정 한 줄: chk <설명> <조건(bash)>
chk() {
    if eval "$2"; then echo "  [통과] $1"; else echo "  [실패] $1"; BAD+=("$1"); fi
}
end() {
    local s
    s=$(state)
    echo "$s" > "$W/state"
    echo "  상태: $s"
    if [ ${#BAD[@]} -eq 0 ]; then SUMMARY+=("$NAME|통과|$1"); else SUMMARY+=("$NAME|실패|$1 / 실패: ${BAD[*]}"); fi
}
has() { grep -qF -- "$1" "$W/${2:-out.txt}"; }
st_has() { state | grep -qF -- "$1"; }

OK550="driver=550.163.01 cuda=12.4 pkgs=core,cuda12,proprietary,proprietary-cuda12 dcgm=active/enabled/1 repo=enabled=0 orig=0 tmp=0"
OK580="driver=580.95.05 cuda=13.0 pkgs=core,cuda13,proprietary,proprietary-cuda13 dcgm=active/enabled/1 repo=enabled=0 orig=0 tmp=0"

T1() { # 버전 없는 줄 → 한 번 질문(550). mount 와 scp(자동 마운트 멈춤) 결과가 같아야 한다. 이어서 T2(580 재설치)
    local m s
    for mode in mount scp; do
        reset_target
        [ $mode = scp ] && automount_off
        begin "T1-$mode" "$TGT\n"
        run_gi "2\ny\n" "$MU"
        chk "종료코드 0" '[ $RC -eq 0 ]'
        chk "버전 질문 1회" '[ $(grep -c "번호 (1-3)" $W/out.txt) -eq 1 ]'
        chk "방식 $mode" 'grep -E "^$TGT +.* $mode( |$)" $W/out.txt | grep -q .'
        chk "550/CUDA12 DCGM 정상, repo=0" '[ "$(state)" = "$OK550" ]'
        end "$mode: $(state)"
        [ "$*" = noT2 ] && continue
        begin "T2-$mode" "$TGT 580.95.05\n"
        run_gi "y\na\n" "$MU"
        chk "종료코드 0" '[ $RC -eq 0 ]'
        chk "재설치 목록 550.163.01 → 580.95.05" 'has "$TGT 550.163.01 → 580.95.05"'
        chk "cuda12 패키지 제거 후 580/cuda13" '[ "$(state)" = "$OK580" ]'
        chk "반대 CUDA 패키지 제거 로그" 'grep -q "반대 CUDA 의 DCGM 패키지 제거" $W/logs/*/$TGT.log'
        end "$mode: $(state)"
    done
}
T2() { :; } # T1 안에서 함께 실행

T3() { # 호스트별 다른 버전: 대상은 470, 버전 없는 줄(접속 안 되는 가짜 호스트)은 질문한 550
    reset_target
    begin T3 "$TGT 470.256.02\ngi-nohost.invalid\n"
    run_gi "2\ny\n" "$MU"
    chk "대상은 요청한 470 설치" 'st_has "driver=470.256.02"'
    chk "버전 없는 줄은 550 으로 시도(fail_list)" 'grep -q "^gi-nohost.invalid 550.163.01 " $W/fail_list'
    chk "종료코드 1(접속 실패 호스트)" '[ $RC -eq 1 ]'
    end "60 대신 접속 불가 호스트로 확인(부분)"
}
T4() { # busy_once: 1차 실패 → 서비스 중지·rmmod → 재시도 성공
    reset_target
    preinstall 550.163.01 12.4
    t 'echo busy_once > /var/lib/fakegpu/busy'
    begin T4 "$TGT 580.95.05\n"
    run_gi "y\na\n" "$MU"
    chk "종료코드 0" '[ $RC -eq 0 ]'
    chk "재시도 메시지" 'has "1회 재시도"'
    chk "1차 실패 후 성공(history)" 't "cat /var/lib/fakegpu/history" | tr "\n" " " | grep -q "result=fail(busy_once).*result=ok 580.95.05"'
    chk "580/cuda13" '[ "$(state)" = "$OK580" ]'
    end "$(t 'grep result= /var/lib/fakegpu/history' | awk '{print $3}' | paste -sd' ' -)"
}
T5() { # busy_always: 재시도도 실패 → fail_list, fail_list.retry, 종료코드 1
    reset_target
    preinstall 550.163.01 12.4
    t 'echo busy_always > /var/lib/fakegpu/busy'
    begin T5 "$TGT 580.95.05\n"
    run_gi "y\na\n" "$MU"
    chk "종료코드 1" '[ $RC -eq 1 ]'
    chk "fail_list 형식" '[ "$(cat $W/fail_list)" = "$TGT 580.95.05 install failed(retry after unload)" ]'
    chk "fail_list.retry 형식" '[ "$(cat $W/fail_list.retry)" = "$TGT 580.95.05" ]'
    chk "lsof·설치로그 남김" 'grep -q "lsof /dev/nvidia" $W/logs/*/$TGT.log && grep -q "fake: busy_always" $W/logs/*/$TGT.log'
    end "$(cat $W/fail_list)"
}
T6() { # 470(CUDA11): 드라이버만, DCGM 건너뜀 경고
    reset_target
    begin T6 "$TGT 470.256.02\n"
    run_gi "y\n" "$MU"
    chk "종료코드 0" '[ $RC -eq 0 ]'
    chk "DCGM 건너뜀 경고" 'has "DCGM 을 건너뜁니다"'
    chk "결과표 skip(CUDA 11.4) ok" 'grep -E "^$TGT .*skip\(CUDA 11.4\).* ok$" $W/out.txt | grep -q .'
    chk "470 설치, DCGM 패키지 없음" 'st_has "driver=470.256.02 cuda=11.4 pkgs= "'
    end "$(grep -E "^$TGT " $W/out.txt | tail -n 1)"
}
T7a() { # dnf 실패(repo 경로 없음) → fail, rhel.repo enabled=0 복구
    reset_target
    t 'sed -i "s|^baseurl=.*|baseurl=file:///export/repo_missing|" /etc/yum.repos.d/rhel.repo'
    begin T7a "$TGT 550.163.01\n"
    run_gi "y\n" "$MU"
    chk "종료코드 1" '[ $RC -eq 1 ]'
    chk "fail 사유 dcgm install failed" 'grep -q "^$TGT 550.163.01 dcgm install failed" $W/fail_list'
    chk "repo enabled=0 복구, 원본 사본 없음" 'st_has "repo=enabled=0 orig=0"'
    chk "trap 복구 표식" 'grep -q "GI_REPO_RESTORED=1" $W/logs/*/$TGT.log'
    end "$(cat $W/fail_list)"
}
T7b() { # DCGM 설치(dnf) 중 Ctrl-C → 원격 trap 이 rhel.repo 를 0 으로 복구
    reset_target
    t 'touch /var/lib/fakegpu/dcgm_slow'
    begin T7b "$TGT 550.163.01\n"
    sed -e "s|^AUTOFS_PATH=\"\"|AUTOFS_PATH=\"$AP\"|" -e "s|^MOUNT_USER=\"\"|MOUNT_USER=\"$MU\"|" \
        "$ROOT/gpu_install.sh" > "$ROOT/.gi_test.sh"
    (cd "$W" && printf 'y\n' | setsid bash "$ROOT/.gi_test.sh" > out.txt 2>&1) &
    local seen=0 i pg
    for i in $(seq 120); do
        if t 'grep -q "^enabled=1" /etc/yum.repos.d/rhel.repo'; then seen=1; break; fi
        sleep 1
    done
    sleep 3
    pg=$(pgrep -f "gpu_install_os8 $AP" | head -n 1)
    [ -n "$pg" ] && kill -INT -- -"$(ps -o pgid= -p "$pg" | tr -d ' ')"
    wait
    rm -f "$ROOT/.gi_test.sh"
    echo "  (Ctrl-C 보냄: pid=$pg, repo enabled=1 관찰=$seen) 원격 정리 대기"
    for i in $(seq 90); do
        t 'pgrep -f "base64 -d" >/dev/null || pgrep -x dnf >/dev/null || pgrep -f dnf5 >/dev/null' || break
        sleep 1
    done
    chk "dnf 단계에서 enabled=1 관찰 후 중단" '[ $seen -eq 1 ] && [ -n "$pg" ]'
    chk "중단 뒤 repo enabled=0, 원본 사본 없음" 'st_has "repo=enabled=0 orig=0"'
    t 'rm -f /var/lib/fakegpu/dcgm_slow'
    end "중단 후 $(state | grep -o "repo=[^ ]* orig=[0-9]")"
}
T8a() { # kernel-devel 없음 → fail 사유
    reset_target
    t 'touch /var/lib/fakegpu/no_kdevel'
    begin T8a "$TGT 550.163.01\n"
    run_gi "y\n" "$MU"
    chk "종료코드 1" '[ $RC -eq 1 ]'
    chk "fail_list: no kernel-devel" '[ "$(cat $W/fail_list)" = "$TGT 550.163.01 no kernel-devel" ]'
    chk "설치 시도 안 함" '[ "$(history_n)" = 0 ]'
    end "$(cat $W/fail_list)"
}
T8b() { # nouveau 로드 → 언로드 후 진행
    reset_target
    t 'touch /var/lib/fakegpu/nouveau'
    begin T8b "$TGT 550.163.01\n"
    run_gi "y\n" "$MU"
    chk "종료코드 0" '[ $RC -eq 0 ]'
    chk "nouveau 언로드 메시지" 'has "nouveau 가 로드되어 있어 언로드합니다"'
    chk "nouveau 내려감" '! t "lsmod" | grep -q "^nouveau "'
    chk "550 설치" '[ "$(state)" = "$OK550" ]'
    end "언로드 후 설치"
}
T8c() { # GPU 없음 → 건너뜀
    reset_target
    t 'touch /var/lib/fakegpu/nogpu'
    begin T8c "$TGT 550.163.01\n"
    run_gi "y\n" "$MU"
    chk "종료코드 0" '[ $RC -eq 0 ]'
    chk "결과 skip: GPU 없음" 'grep -E "^$TGT .*skip: GPU 없음" $W/out.txt | grep -q .'
    chk "설치 시도 안 함" '[ "$(history_n)" = 0 ]'
    end "$(grep -E "^$TGT " $W/out.txt | tail -n 1)"
}
T9() { # 같은 버전 재요청 → 기본 건너뜀. DCGM 이 멈춰 있으면 DCGM 만 다시 맞춤
    reset_target
    begin T9-prep "$TGT 550.163.01\n"
    run_gi "y\n" "$MU"
    local n
    n=$(history_n)
    begin T9 "$TGT 550.163.01\n"
    run_gi "y\na\n" "$MU"
    chk "종료코드 0" '[ $RC -eq 0 ]'
    chk "같은 버전 표시" 'has "(같은 버전, 기본 건너뜀)"'
    chk "결과 skip: 같은 버전 설치됨" 'grep -E "^$TGT .*skip: 같은 버전 설치됨" $W/out.txt | grep -q .'
    chk ".run 다시 실행 안 함" '[ "$(history_n)" = "$n" ]'
    end "기본 건너뜀"
    t 'systemctl disable --now nvidia-dcgm >/dev/null 2>&1'
    begin T9-dcgm "$TGT 550.163.01\n"
    run_gi "y\na\n" "$MU"
    chk "종료코드 0" '[ $RC -eq 0 ]'
    chk "DCGM 만 다시 맞춤 메시지" 'has "DCGM 만 다시 맞춥니다"'
    chk ".run 다시 실행 안 함" '[ "$(history_n)" = "$n" ]'
    chk "DCGM 정상 복구" '[ "$(state)" = "$OK550" ]'
    end "DCGM 만 복구"
}
TM() { # 마운트 대상($TGT)과 비마운트 대상($SCP_TGT)을 한 list 로 T1·T2, 최종 상태가 같아야 한다
    local h
    [ -n "$SCP_TGT" ] || { echo "TM: SCP_TGT 가 필요합니다"; return; }
    for h in "$TGT" "$SCP_TGT"; do TH=$h reset_target; done
    for step in T1 T2; do
        if [ $step = T1 ]; then
            begin "TM-T1" "$TGT\n$SCP_TGT\n"
            run_gi "2\ny\n" "$MU"
            want="$OK550"
        else
            begin "TM-T2" "$TGT 580.95.05\n$SCP_TGT 580.95.05\n"
            run_gi "y\na\n" "$MU"
            want="$OK580"
            chk "재설치 목록 두 대" 'has "$TGT 550.163.01 → 580.95.05" && has "$SCP_TGT 550.163.01 → 580.95.05"'
        fi
        chk "종료코드 0" '[ $RC -eq 0 ]'
        chk "$TGT 방식 mount" 'grep -E "^$TGT +.* mount( |$)" $W/out.txt | grep -q .'
        chk "$SCP_TGT 방식 scp" 'grep -E "^$SCP_TGT +.* scp( |$)" $W/out.txt | grep -q .'
        TH=$TGT state > "$W/state.mount"
        TH=$SCP_TGT state > "$W/state.scp"
        chk "$TGT(mount) 최종 상태" '[ "$(cat $W/state.mount)" = "$want" ]'
        chk "$SCP_TGT(scp) 최종 상태" '[ "$(cat $W/state.scp)" = "$want" ]'
        chk "두 서버 상태 같음" 'cmp -s $W/state.mount $W/state.scp'
        echo "  mount: $(cat $W/state.mount)"
        echo "  scp  : $(cat $W/state.scp)"
        end "두 대 동시"
    done
}
T10() { # 인자로 다른 autofs 경로 → 그 경로의 .run (mount 와 scp 둘 다)
    for mode in mount scp; do
        reset_target
        [ $mode = scp ] && automount_off
        begin "T10-$mode" "$TGT\n"
        run_gi "1\ny\n" "$MU" "$AP_ALT"
        chk "종료코드 0" '[ $RC -eq 0 ]'
        chk "버전 목록이 다른 경로(575 하나)" 'has "1) 575.57.08" && ! has "550.163.01"'
        chk "방식 $mode" 'grep -E "^$TGT +.* $mode( |$)" $W/out.txt | grep -q .'
        if [ $mode = mount ]; then
            chk "다른 경로의 .run 실행" 't "cat /var/lib/fakegpu/history" | grep -q "run $AP_ALT/NVIDIA-Linux-x86_64-575.57.08.run -s"'
        else
            chk "다른 경로의 .run 을 scp 해 실행" 'grep -q "scp: $AP_ALT/NVIDIA-Linux-x86_64-575.57.08.run" $W/out.txt'
        fi
        chk "575/CUDA12.9 DCGM cuda12" 'st_has "driver=575.57.08 cuda=12.9 pkgs=core,cuda12,proprietary,proprietary-cuda12 dcgm=active/enabled/1"'
        end "$mode: $(state | cut -d" " -f1-3)"
    done
}

mkdir -p "$OUT"
for s in "${SCEN[@]}"; do "$s"; done
reset_target

echo
echo "===== 결과 ($OUT)"
printf '%s\n' "${SUMMARY[@]}" | column -t -s '|'
# T1/T2: mount 와 scp 최종 상태 비교
for x in T1 T2; do
    if [ -f "$OUT/$x-mount/state" ] && [ -f "$OUT/$x-scp/state" ]; then
        if cmp -s "$OUT/$x-mount/state" "$OUT/$x-scp/state"; then echo "$x mount/scp 최종 상태 같음"; else echo "$x mount/scp 최종 상태 다름"; fi
    fi
done
