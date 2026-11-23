# 가짜 DCGM 4 rpm 6종 (gpu_install 테스트 전용). setup_mgmt.sh 가 rpmbuild 로 만든다.
# core 에 가짜 nvidia-dcgm.service(sleep infinity)와 가짜 dcgmi('N GPUs found.')가 들어 있다.
# /var/lib/fakegpu/dcgm_slow 가 있으면 core 설치 중 30초 멈춘다(Ctrl-C 시험용).
Name:      datacenter-gpu-manager-4
Version:   4.4.2
Release:   1
Summary:   fake DCGM 4 for gpu_install test
License:   none
BuildArch: noarch

%description
fake DCGM 4 for gpu_install test (not a real NVIDIA package)

%package core
Summary: fake DCGM core (nvidia-dcgm.service, dcgmi)
%description core
fake DCGM core

%package proprietary
Summary: fake DCGM proprietary
Requires: datacenter-gpu-manager-4-core = %{version}-%{release}
%description proprietary
fake DCGM proprietary

%package cuda12
Summary: fake DCGM cuda12
Requires: datacenter-gpu-manager-4-core = %{version}-%{release}
%description cuda12
fake DCGM cuda12

%package cuda13
Summary: fake DCGM cuda13
Requires: datacenter-gpu-manager-4-core = %{version}-%{release}
%description cuda13
fake DCGM cuda13

%package proprietary-cuda12
Summary: fake DCGM proprietary cuda12
Requires: datacenter-gpu-manager-4-proprietary = %{version}-%{release}
Requires: datacenter-gpu-manager-4-cuda12 = %{version}-%{release}
%description proprietary-cuda12
fake DCGM proprietary cuda12

%package proprietary-cuda13
Summary: fake DCGM proprietary cuda13
Requires: datacenter-gpu-manager-4-proprietary = %{version}-%{release}
Requires: datacenter-gpu-manager-4-cuda13 = %{version}-%{release}
%description proprietary-cuda13
fake DCGM proprietary cuda13

%install
mkdir -p %{buildroot}/usr/bin %{buildroot}/usr/lib/systemd/system %{buildroot}/usr/share/fake-dcgm
cat > %{buildroot}/usr/bin/dcgmi <<'EOF'
#!/bin/sh
# 가짜 dcgmi: 'dcgmi discovery -l' 만 지원. nvidia-dcgm 이 active 이고 드라이버가 로드돼 있어야 GPU 를 센다.
S=/var/lib/fakegpu
if [ "$1 $2" != "discovery -l" ]; then echo "fake dcgmi: discovery -l only"; exit 1; fi
if ! systemctl is-active -q nvidia-dcgm; then
    echo "Error: unable to establish a connection to the specified host: localhost"
    exit 1
fi
n=0
if [ ! -e $S/nogpu ] && grep -qx nvidia $S/modules 2>/dev/null; then n=$(cat $S/gpus 2>/dev/null || echo 1); fi
echo "$n GPUs found."
EOF
chmod 755 %{buildroot}/usr/bin/dcgmi
cat > %{buildroot}/usr/lib/systemd/system/nvidia-dcgm.service <<'EOF'
[Unit]
Description=fake NVIDIA DCGM service (gpu_install test)

[Service]
ExecStart=/bin/sleep infinity

[Install]
WantedBy=multi-user.target
EOF
for p in core proprietary cuda12 cuda13 proprietary-cuda12 proprietary-cuda13; do
    echo "$p" > %{buildroot}/usr/share/fake-dcgm/$p
done

%pre core
if [ -f /var/lib/fakegpu/dcgm_slow ]; then sleep 30; fi
exit 0

%post core
systemctl daemon-reload >/dev/null 2>&1 || :

%preun core
if [ $1 -eq 0 ]; then systemctl disable --now nvidia-dcgm >/dev/null 2>&1 || :; fi

%postun core
systemctl daemon-reload >/dev/null 2>&1 || :

%files core
/usr/bin/dcgmi
/usr/lib/systemd/system/nvidia-dcgm.service
/usr/share/fake-dcgm/core

%files proprietary
/usr/share/fake-dcgm/proprietary

%files cuda12
/usr/share/fake-dcgm/cuda12

%files cuda13
/usr/share/fake-dcgm/cuda13

%files proprietary-cuda12
/usr/share/fake-dcgm/proprietary-cuda12

%files proprietary-cuda13
/usr/share/fake-dcgm/proprietary-cuda13
