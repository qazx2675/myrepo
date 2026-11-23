#!/bin/bash
# setup_mgmt.sh - 관리 서버(=NFS 서버) 테스트 환경 구성 (gpu_install 테스트 전용, root 로 실행)
# 사용법: bash test/setup_mgmt.sh [허용 네트워크(기본 192.168.0.0/24)] [테스트계정(기본 gpuadm)]
#  - /export/gpu            가짜 NVIDIA-Linux-x86_64-<ver>.run (550=CUDA12.4, 580=CUDA13.0, 470=CUDA11.4)
#  - /export/user/<계정>/gpu     위 .run 의 하드링크 (AUTOFS_PATH=/user/<계정>/gpu)
#  - /export/user/<계정>/gpu_alt 다른 경로 시험용 575.57.08(CUDA12.9)
#  - /export/repo           가짜 DCGM rpm 6종 + repodata (대상 rhel.repo baseurl=file:///export/repo)
#  - 관리 서버에서도 같은 경로로 보이게 /user/<계정> → /export/user/<계정> 심볼릭 링크
set -e
NET="${1:-192.168.0.0/24}"
TUSER="${2:-gpuadm}"
T="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
GPU=/export/gpu
REPO=/export/repo
UDIR=/export/user/$TUSER

mkrun() { # mkrun <디렉토리> <버전> <CUDA>
    sed -e "s/@VER@/$2/" -e "s/@CUDA@/$3/" "$T/fake/NVIDIA-Linux-x86_64.run.in" > "$1/NVIDIA-Linux-x86_64-$2.run"
    chmod 755 "$1/NVIDIA-Linux-x86_64-$2.run"
}

echo "== 가짜 .run"
mkdir -p $GPU $UDIR/gpu $UDIR/gpu_alt $REPO/dcgm
for vc in 550.163.01:12.4 580.95.05:13.0 470.256.02:11.4; do
    v=${vc%%:*}
    mkrun $GPU "$v" "${vc##*:}"
    ln -f "$GPU/NVIDIA-Linux-x86_64-$v.run" "$UDIR/gpu/"
done
mkrun $UDIR/gpu_alt 575.57.08 12.9
ls -l $GPU $UDIR/gpu $UDIR/gpu_alt

echo "== 가짜 DCGM rpm"
W=$(mktemp -d)
rpmbuild -bb --quiet --define "_topdir $W" "$T/rpm/fake-dcgm.spec"
rm -f $REPO/dcgm/*.rpm
cp $W/RPMS/noarch/*.rpm $REPO/dcgm/
rm -rf "$W"
if command -v createrepo_c >/dev/null 2>&1; then
    createrepo_c -q $REPO
else
    # 관리 서버에 createrepo_c 가 없고 로컬 미디어도 없어서(폐쇄망) 최소 생성기를 쓴다
    python3 "$T/mkrepo.py" $REPO
fi
ls -l $REPO/dcgm $REPO/repodata

echo "== NFS export"
cat > /etc/exports.d/gpu_install_test.exports <<EOF
$GPU $NET(ro,no_root_squash)
$REPO $NET(ro,no_root_squash)
$UDIR $NET(rw,no_root_squash)
EOF
systemctl enable --now nfs-server >/dev/null
exportfs -ra
exportfs -v | grep -E "^/export" || true
if systemctl is-active -q firewalld; then
    firewall-cmd -q --add-service=nfs
    firewall-cmd -q --permanent --add-service=nfs
fi

echo "== 관리 서버에서도 같은 경로: /user/$TUSER"
mkdir -p /user
if [ ! -e /user/$TUSER ]; then ln -s $UDIR /user/$TUSER; fi
ls -l /user/$TUSER/gpu/
echo "완료"
