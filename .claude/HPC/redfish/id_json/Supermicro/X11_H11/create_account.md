# Supermicro X11/H11 - 새 계정 id/pw 등록

해당 사용자 모델 없음 (참고용). 플레이스홀더만 사용. Redfish 는 BMC FW 1.xx + OOB 라이선스 필요.

## Redfish
- `POST https://<BMC_IP>/redfish/v1/AccountService/Accounts`
```json
{ "UserName": "<NEW_USER>", "Password": "<NEW_PASSWORD>", "RoleId": "Administrator", "Enabled": true }
```
```
curl -sk -u <ADMIN_USER>:<ADMIN_PASSWORD> -H "Content-Type: application/json" \
  -X POST https://<BMC_IP>/redfish/v1/AccountService/Accounts \
  -d '{"UserName":"<NEW_USER>","Password":"<NEW_PASSWORD>","RoleId":"Administrator","Enabled":true}'
```
- RoleId: 펌웨어에 따라 `Admin`/`Operator`/`ReadOnlyUser` (2.0, 2.0a 예시) 또는 `Administrator`/`Operator`/`ReadOnly` (2.0b 이후). 먼저 `GET /redfish/v1/AccountService/Roles` 로 확인.
- 최대 10 프로파일, 비밀번호 8~20자(3종 이상, 역순 금지).
- 응답 코드 문서 없음.
- 잠금 정책: `PATCH /redfish/v1/AccountService` `{"AccountLockoutThreshold":2,"AccountLockoutDuration":300,"AccountLockoutCounterResetAfter":300}`.

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
  (`user set password` 는 Supermicro FAQ 41692, 나머지 일반 문법.)
- **IPMICFG (X11 매뉴얼의 도움말 그대로):** `-user add <user id> <username> <password> <privilege>`, `-user del <id>`, `-user level <id> <privilege>`, `-user setpwd <id> <password>`, `-user list`.
- **SMCIPMITool:** `SMCIPMITool <BMC_IP> <ADMIN_USER> <ADMIN_PASSWORD> user add <ID> <NEW_USER> <NEW_PASSWORD> <PRIV 4|3|2|1>` (Java; 8~19자). 서브커맨드는 `user setpwd` (≠ `user passwd`).
- **SUM `ChangeBmcCfg`:** `-c GetBmcCfg --file BMCCfg.xml` 후 편집·`-c ChangeBmcCfg --file BMCCfg.xml`. SUM 2.x 릴리스 노트에 사용자 삭제/복잡도 검사 언급 (사용자 테이블 존재), 요소명은 공개 문서에 없음 (unavailable). 노드 제품키 필요.
