# ESXi 데이터스토어 전체에서 vmx 파일 찾기 및 내용 확인
find /vmfs/volumes -name "*.vmx"
cat /vmfs/volumes/xxxx/VMNAME/VMNAME.vmx
