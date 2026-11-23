#!/bin/bash
# setup_target.sh - 설치 대상(GPU 서버 역할) 테스트 VM 구성 (gpu_install 테스트 전용, root 로 실행)
# 사용법: bash setup_target.sh <관리서버IP> [테스트계정(기본 gpuadm)] [nomount]
#         nomount: 비마운트(scp) 대상. NFS 자동 마운트를 구성하지 않는다(rhel.repo 와 가짜 도구만)
#         bash setup_target.sh remove        # 구성한 것 전부 되돌리기
#  - /opt/gpu_fake/bin 에 가짜 nvidia-smi/lspci/lsmod/rmmod/rpm(kernel-devel)/gcc 를 두고
#    root 의 비대화형 셸(gossh·ssh 명령)에서만 PATH 맨 앞에 오게 /root/.bashrc 에 한 블록을 넣는다.
#    진짜 /usr/sbin/rmmod 등은 건드리지 않는다.
#  - /user/<계정> 과 /export/repo 를 관리 서버 NFS 에서 자동 마운트한다.
#    폐쇄망이라 autofs·nfs-utils 패키지를 설치할 수 없어서 systemd automount(커널 autofs)로
#    nfs4 를 마운트 헬퍼 없이(addr= 옵션) 붙인다.
#  - /etc/yum.repos.d/rhel.repo (enabled=0, baseurl=file:///export/repo)
set -e
T="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
FAKE=/opt/gpu_fake/bin
STATE=/var/lib/fakegpu
REPO=/etc/yum.repos.d/rhel.repo
MARK_B="# >>> gpu_install test fake tools >>>"
MARK_E="# <<< gpu_install test fake tools <<<"

unit_name() { systemd-escape -p "$1"; }

if [ "$1" = remove ]; then
    for u in $(systemctl list-unit-files --no-legend 'user-*.automount' 'export-repo.automount' | awk '{print $1}'); do
        grep -q "gpu_install test" "/etc/systemd/system/$u" 2>/dev/null || continue
        systemctl disable --now "$u" "${u%.automount}.mount" 2>/dev/null || true
        rm -f "/etc/systemd/system/$u" "/etc/systemd/system/${u%.automount}.mount"
    done
    systemctl daemon-reload
    for f in /root/.bashrc /root/.cshrc /root/.tcshrc; do
        [ -f $f ] && sed -i "/^$MARK_B\$/,/^$MARK_E\$/d" $f
    done
    rpm -qa 'datacenter-gpu-manager-4-*' | xargs -r rpm -e
    grep -q "gpu_install test" $REPO 2>/dev/null && rm -f $REPO $REPO.gpu_install.orig
    rm -rf /opt/gpu_fake $STATE
    echo "되돌림 완료"
    exit 0
fi

MGMT="$1"
TUSER="${2:-gpuadm}"
NOMOUNT="$3"
if [ -z "$MGMT" ]; then echo "사용법: bash setup_target.sh <관리서버IP> [테스트계정]" >&2; exit 2; fi

echo "== 가짜 도구 $FAKE"
mkdir -p $FAKE $STATE
for f in nvidia-smi lspci lsmod rmmod rpm; do install -m 755 "$T/fake/$f" $FAKE/$f; done
if command -v gcc >/dev/null 2>&1 && [ "$(command -v gcc)" != $FAKE/gcc ]; then
    rm -f $FAKE/gcc
else
    install -m 755 "$T/fake/gcc" $FAKE/gcc
fi
[ -f $STATE/gpus ] || echo 1 > $STATE/gpus
if ! grep -qF "$MARK_B" /root/.bashrc; then
    cat >> /root/.bashrc <<EOF
$MARK_B
case \$- in *i*) ;; *) PATH=$FAKE:\$PATH ;; esac
$MARK_E
EOF
fi
# root 셸이 csh/tcsh 인 서버: tcsh 는 ~/.tcshrc(없으면 ~/.cshrc)를 읽는다.
# .tcshrc 가 prompt 를 늘 설정하는 경우가 있어 'tcsh -c 명령' 일 때만 생기는 command 변수로 비대화형을 가린다.
CSHRC=/root/.cshrc
[ -f /root/.tcshrc ] && CSHRC=/root/.tcshrc
if ! grep -qF "$MARK_B" $CSHRC 2>/dev/null; then
    cat >> $CSHRC <<EOF
$MARK_B
if (\$?command) setenv PATH $FAKE:\$PATH
$MARK_E
EOF
fi
ls -l $FAKE

echo "== NFS 자동 마운트 (/user/$TUSER, /export/repo)"
automount_unit() { # automount_unit <원격경로> <마운트경로> <옵션>
    local n; n=$(unit_name "$2")
    mkdir -p "$2"
    cat > "/etc/systemd/system/$n.mount" <<EOF
# gpu_install test: autofs 대용 (systemd automount)
[Unit]
Description=gpu_install test NFS $2

[Mount]
What=$MGMT:$1
Where=$2
Type=nfs4
Options=vers=4.2,$3,addr=$MGMT
EOF
    cat > "/etc/systemd/system/$n.automount" <<EOF
# gpu_install test: autofs 대용 (systemd automount)
[Unit]
Description=gpu_install test automount $2

[Automount]
Where=$2
TimeoutIdleSec=600

[Install]
WantedBy=multi-user.target
EOF
    systemctl daemon-reload
    systemctl enable --now "$n.automount"
}
if [ "$NOMOUNT" = nomount ]; then
    echo "nomount: 건너뜀 (scp 대상)"
else
    automount_unit /export/user/$TUSER /user/$TUSER rw
    automount_unit /export/repo /export/repo ro
    ls /user/$TUSER/gpu /export/repo
fi

echo "== $REPO"
if [ ! -f $REPO ] || grep -q "gpu_install test" $REPO; then
    cat > $REPO <<EOF
# gpu_install test repo (가짜 DCGM). 관리 서버의 같은 경로에도 같은 rpm 이 있다.
[rhel-gpu-test]
name=gpu_install test repo
baseurl=file:///export/repo
enabled=0
gpgcheck=0
EOF
fi
cat $REPO
echo "완료"
