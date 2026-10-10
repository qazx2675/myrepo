# Supermicro X14/H14 - 기존 id 패스워드 변경

해당 사용자 모델 없음 (참고용).

## Redfish (unverified: Supermicro 문서에 Password PATCH 예시 없음, 표준 Redfish)
```
curl -sk -u <ADMIN_USER>:<CURRENT_PASSWORD> https://<BMC_IP>/redfish/v1/AccountService/Accounts/<SLOT>
curl -sk -u <ADMIN_USER>:<CURRENT_PASSWORD> -H "Content-Type: application/json" \
  -X PATCH https://<BMC_IP>/redfish/v1/AccountService/Accounts/<SLOT> \
  -d '{"Password":"<NEW_PASSWORD>"}'
```
- `PasswordGuidanceMessage` (`GET /redfish/v1/AccountService`) 로 현재 정책 확인. 길이 8~20자 (웹 UI), 3종 이상, 사용자명 역순 금지, 6.1 예시 기준 공백/탭/`"`/`'`/`:`/`,`/`#`/`-`/`;` 불가.
- 계정 형식이 `IPMI` 뿐이면 Redfish 대상이 아닐 수 있음 (BMC 01.02.xx.xx 이상).

## 대체
- **웹 UI:** Configuration > Account Services > Users > Modify(연필). 기본 ADMIN 은 목록에서 수정/삭제 불가로 표기 (X14 매뉴얼) -> ADMIN 암호는 Redfish PATCH 또는 IPMI 로. Operator/User 는 자기 암호만 변경 가능.
- **ipmitool (Supermicro FAQ 41692):**
  ```
  ipmitool -I lanplus -H <BMC_IP> -U <ADMIN_USER> -P <CURRENT_PASSWORD> user list 1
  ipmitool -I lanplus -H <BMC_IP> -U <ADMIN_USER> -P <CURRENT_PASSWORD> user set password <ID> <NEW_PASSWORD> 20
  ```
- **SMCIPMITool(`user setpwd`) / IPMICFG(`-user setpwd`) / SUM(`SetBmcPassword`):** X14/H14 이상 **미지원**. X13/H13 문서 참조.
- **SAA:** 후속 도구. 명령 존재 여부는 실장비 `saa -h` 로 확인 (unverified).
