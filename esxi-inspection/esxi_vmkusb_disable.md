# ESXi vmkusb(USB NIC) 비활성화 트러블슈팅

크레이 장비에 ESXi 설치 후 BIOS에서 USB를 Disabled 해놨는데도 vusb0가 계속 보이는 문제.
RHEL에서는 blacklist로 해결했으나 ESXi에서는 UI로 NIC 삭제가 불가능한 상태에서의 대응.

## 1. 관련 로그 확인
```
grep -i vmkusb /var/log/vmkernel.log | tail -30
grep -i vmkusb /var/log/hostd.log | tail -30
```

## 2. configstore 확인
```
/usr/lib/vmware/configstore/bin/cscli show -c esx -k /system/module/vmkusb 2>/dev/null
# 또는 전체 module 관련 설정 덤프
/usr/lib/vmware/configstore/bin/cscli show -c esx -g system 2>/dev/null | grep -A5 -i module
```

## 3. 모듈 비활성화
```
# 1. 현재 상태
esxcli system module list | grep vmkusb
# 2. 비활성화
esxcli system module set -m vmkusb -e false
# 3. 즉시 재확인 (재부팅 전)
esxcli system module list | grep vmkusb
# 4. 설정 강제 저장
/sbin/auto-backup.sh
```

## 4. 재부팅 전 configstore 반영 확인
```
/usr/lib/vmware/configstore/bin/cscli show -c esx -g system -k module 2>&1 | grep -A10 -i vmkusb
```

## 5. 모듈명 확인 팁
`esxcli system module list -m vmkusb` 명령이 안 먹힘 → `esxcli system module get -m vmkusb` 방식으로 확인 가능.
단, `esxcli system module get`에는 활성화 상태값(Enabled/Loaded)이 없고 모듈 파일 존재 여부만 확인됨.
