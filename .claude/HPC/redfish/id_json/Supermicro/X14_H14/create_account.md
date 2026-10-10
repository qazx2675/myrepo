# Supermicro X14/H14 - 새 계정 id/pw 등록

해당 사용자 모델 없음 (참고용). 플레이스홀더만 사용.

## Redfish
X13/H13 과 동일. 상세 속성표는 `../X13_H13/create_account.md` 참조.
```
curl -sk -u <ADMIN_USER>:<ADMIN_PASSWORD> -H "Content-Type: application/json" \
  -X POST https://<BMC_IP>/redfish/v1/AccountService/Accounts \
  -d '{"UserName":"<NEW_USER>","Password":"<NEW_PASSWORD>","RoleId":"Administrator","Enabled":true,"AccountTypes":["Redfish"]}'
```
- RoleId: `Administrator` / `Operator` / `ReadOnly`. AccountTypes: `Redfish`, `IPMI` (BMC 01.02.xx.xx 이상에서 분리, 미지정 시 `Redfish`).
- 6.1 가이드는 Add Account URI 를 `/redfish/v1/AccountService` 로 적었지만 오기로 보임. 405 면 `GET .../Accounts` 의 `Allow` 확인.
- 성공 코드 문서 없음 (표준 201).

## 대체
- **웹 UI:** Configuration > Account Services > Users > Add (User Name, Password, Network Privilege, Enable, Account Type). SNMP 형식은 Authentication Protocol(None/HMAC_MD5/HMAC_SHA96) 과 Encryption Protocol(None/CBC_DES/CFB128_AES128) 및 키 필요.
- **ipmitool (IPMI 형식 계정):**
  ```
  ipmitool -I lanplus -H <BMC_IP> -U <ADMIN_USER> -P <ADMIN_PASSWORD> user list 1
  ipmitool -I lanplus -H <BMC_IP> -U <ADMIN_USER> -P <ADMIN_PASSWORD> user set name <ID> <NEW_USER>
  ipmitool -I lanplus -H <BMC_IP> -U <ADMIN_USER> -P <ADMIN_PASSWORD> user set password <ID> <NEW_PASSWORD> 20
  ipmitool -I lanplus -H <BMC_IP> -U <ADMIN_USER> -P <ADMIN_PASSWORD> channel setaccess 1 <ID> callin=on ipmi=on link=on privilege=4
  ipmitool -I lanplus -H <BMC_IP> -U <ADMIN_USER> -P <ADMIN_PASSWORD> user enable <ID>
  ```
  (일반 ipmitool 문법, X14 실측 없음 - unverified.)
- **SMCIPMITool / IPMICFG:** X14/H14 이상 미지원 (Supermicro IPMI Utilities 페이지).
- **SUM:** X13/H13 까지만 지원 (X14 는 SAA 사용). SAA 에 `ChangeBmcCfg`/`SetBmcPassword` 가 있다는 정보는 서드파티 목록뿐 -> unverified.
- **BMC XML:** SUM/SAA `GetBmcCfg`/`ChangeBmcCfg` 의 BmcCfg XML 에서 사용자 테이블 요소명은 공개 문서에 없음 (unavailable). 실장비 export 로 확인.
