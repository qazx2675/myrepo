# Supermicro X10 - 새 계정 id/pw 등록

해당 사용자 모델 없음 (참고용). 플레이스홀더만 사용.

## Redfish (BMC FW 3.xx + OOB 라이선스에서만)
- `POST https://<BMC_IP>/redfish/v1/AccountService/Accounts`
```json
{ "UserName": "<NEW_USER>", "Password": "<NEW_PASSWORD>", "RoleId": "Admin", "Enabled": true }
```
```
curl -sk -u <ADMIN_USER>:<ADMIN_PASSWORD> -H "Content-Type: application/json" \
  -X POST https://<BMC_IP>/redfish/v1/AccountService/Accounts \
  -d '{"UserName":"<NEW_USER>","Password":"<NEW_PASSWORD>","RoleId":"Admin","Enabled":true}'
```
- RoleId: 구 펌웨어 `Admin` / `Operator` / `ReadOnlyUser`, 개명 후 `Administrator` / `Operator` / `ReadOnly`. `GET /redfish/v1/AccountService/Roles` 로 확인 후 사용.
- 최대 10 프로파일. 응답 코드 문서 없음.
- Redfish 가 없는 펌웨어(2.xx 이하) 이거나 라이선스가 없으면 404/401/403 등 -> 아래 대체 사용.

## 대체
- **웹 UI:** Configuration > Users > `Add User` -> 빈 슬롯 선택, 이름/비밀번호/Network Privilege(Administrator/Operator/User).
- **ipmitool:**
  ```
  ipmitool -I lanplus -H <BMC_IP> -U <ADMIN_USER> -P <ADMIN_PASSWORD> user list 1
  ipmitool -I lanplus -H <BMC_IP> -U <ADMIN_USER> -P <ADMIN_PASSWORD> user set name <ID> <NEW_USER>
  ipmitool -I lanplus -H <BMC_IP> -U <ADMIN_USER> -P <ADMIN_PASSWORD> user set password <ID> <NEW_PASSWORD> 20
  ipmitool -I lanplus -H <BMC_IP> -U <ADMIN_USER> -P <ADMIN_PASSWORD> channel setaccess 1 <ID> callin=on ipmi=on link=on privilege=4
  ipmitool -I lanplus -H <BMC_IP> -U <ADMIN_USER> -P <ADMIN_PASSWORD> user enable <ID>
  ```
  (`user set password` 만 Supermicro FAQ 41692 로 확인. 구 X10 에서 lanplus 가 막혀 있으면 호스트 내부 `-I open`.)
- **IPMICFG (대역내):** `./IPMICFG-Linux.x86_64 -user add <ID> <NEW_USER> <NEW_PASSWORD> <PRIV>` (권한 코드는 `-user help`).
- **SMCIPMITool:** `SMCIPMITool <BMC_IP> <ADMIN_USER> <ADMIN_PASSWORD> user add <ID> <NEW_USER> <NEW_PASSWORD> <PRIV>` (4=Administrator, 3=Operator, 2=User, 1=Callback). X10 전용 서브커맨드군(`ipmi oem x10cfg ...`) 은 있으나 사용자 관리는 `user` 군.
- **SUM `ChangeBmcCfg`:** X10 지원 SUM 에서 `GetBmcCfg`/`ChangeBmcCfg` 로 설정 XML 편집. 사용자 테이블 요소명은 공개 문서에 없음 (unavailable). 노드 제품키 필요.
