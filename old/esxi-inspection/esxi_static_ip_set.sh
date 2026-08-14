# ESXi vmk0 고정 IP 및 기본 게이트웨이 설정
esxcli network ip interface ipv4 set -i vmk0 -I [IP] -N [Mask] -t static && esxcli network routing ipv4 route add -n default -g [GW]
