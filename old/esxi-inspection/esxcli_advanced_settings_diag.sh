# ESXi 문제 서버 vs 정상 서버 esxcli advanced/kernel 설정 비교 진단

# 문제 서버에서
esxcli system settings advanced list > /tmp/adv_bad.txt
esxcli system settings kernel list > /tmp/kernel_bad.txt

# 정상 서버에서 동일하게
esxcli system settings advanced list > /tmp/adv_good.txt
esxcli system settings kernel list > /tmp/kernel_good.txt

# 개별 확인
esxcli system settings advanced list -o /UserVars/SuppressShellWarning
esxcli system settings advanced list -o /UserVars/DcuiTimeOut
esxcli system settings advanced list -o /Net/TcpipHeapSize

# 경로/값만 요약 추출 (awk)
esxcli system settings advanced list | awk '
/^   Path:/{path=$0} /Int Value:/{val=$0; print path; print val; print "---"}' > /tmp/adv_summary.txt
