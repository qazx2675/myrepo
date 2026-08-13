# RHEL/Rocky - NVIDIA 드라이버 설치 (dnf, EPEL/DKMS 기반)
sudo dnf install nvidia-driver nvidia-driver-cuda kmod-nvidia-open-dkms dkms kernel-devel kernel-headers gcc make
# https://dl.fedoraproject.org/pub/epel/9/Everything/x86_64/Packages/d (epel-release 참고)
sudo dnf install nvidia-driver nvidia-driver-cuda kmod-nvidia-open-dkms nvidia-kmod-common
