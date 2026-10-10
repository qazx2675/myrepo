# Dell iDRAC10 - 새 계정 id/pw 등록

적용 모델: XE9780
플레이스홀더만 사용. `-k` 는 자체서명 인증서용.

## Redfish POST (verified: Dell CreateDeleteIdracUsersREDFISH.py)
```
curl -sk -u <ADMIN_USER>:<ADMIN_PASSWORD> -H "Content-Type: application/json" \
  -X POST https://<BMC_IP>/redfish/v1/AccountService/Accounts \
  -d '{"UserName":"<NEW_USER>","Password":"<NEW_PASSWORD>","RoleId":"Administrator","Enabled":true}'
```
- `Id` 를 넣으면 해당 번호(2~16)에 생성, 생략하면 첫 빈 번호에 자동 할당. 성공 HTTP **201** (스크립트는 201 만 성공 처리, 응답에 `error` 키가 있으면 실패).
- RoleId: Administrator | Operator | ReadOnly | 커스텀 롤 이름. **None 불가.**
- 필요 권한: ConfigureUsers.

## 커스텀 롤 (iDRAC10 이상, verified: Dell CustomIdracUserRoleREDFISH.py)
```
curl -sk -u <ADMIN_USER>:<ADMIN_PASSWORD> -H "Content-Type: application/json" \
  -X POST https://<BMC_IP>/redfish/v1/AccountService/Roles \
  -d '{"RoleId":"<ROLE_NAME>","AssignedPrivileges":["Login","ConfigureComponents"],"OemPrivileges":["AccessVirtualConsole"]}'
```
이후 계정 생성 시 `"RoleId":"<ROLE_NAME>"`. 롤 이름에는 공백 불가, `-`/`_` 만 허용.

## Dell OEM 속성 (verified: dellemc.openmanage idrac_user.py, generation>=17)
```
curl -sk -u <ADMIN_USER>:<ADMIN_PASSWORD> -H "Content-Type: application/json" \
  -X PATCH https://<BMC_IP>/redfish/v1/Managers/iDRAC.Embedded.1/Oem/Dell/DellAttributes/iDRAC.Embedded.1 \
  -d '{"Attributes":{"Users.<SLOT>.UserName":"<NEW_USER>","Users.<SLOT>.Password":"<NEW_PASSWORD>","Users.<SLOT>.Role":"Administrator","Users.<SLOT>.Enable":"Enabled","Users.<SLOT>.IpmiLanPrivilege":"Administrator"}}'
```
iDRAC10 은 `Users.N.Privilege` 대신 `Users.N.Role`. 슬롯 2~16 (2 = root).

## 제약
- 사용자명 16자 이하(모듈 검증). 암호 길이 한도는 iDRAC10 문서 확인 못함(unavailable) - `GET /redfish/v1/AccountService`.
- FW 1.30.10.50 이상은 Basic 인증이 Unadvertised -> 첫 요청부터 자격증명 전송.
- SNMPv3 는 SHA-384/SHA-512, AES-256 계열만.

## 대체
- RACADM: `racadm set iDRAC.Users.<SLOT>.UserName <NEW_USER>` / `.Password <NEW_PASSWORD>` / `.Role Administrator`(iDRAC10 은 Role, 추정) / `.Enable 1` - iDRAC10 racadm 구문은 직접 확인 못함(unverified).
- SCP: `POST /redfish/v1/Managers/iDRAC.Embedded.1/Actions/Oem/OemManager.ExportSystemConfiguration` `{"ExportFormat":"JSON","ShareParameters":{"Target":["IDRAC"]}}` 후 `OemManager.ImportSystemConfiguration` (Dell SCP 스크립트의 iDRAC10 분기, verified).
- IPMI: ipmitool `user set name/password` (unverified, 일반 문법).
- Ansible: dellemc.openmanage.idrac_user (`custom_role_name` 등).
- 웹 UI: iDRAC Settings > Users.
