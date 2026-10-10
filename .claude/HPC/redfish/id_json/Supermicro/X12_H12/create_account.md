# Supermicro X12/H12 - 새 계정 id/pw 등록

해당 사용자 모델 없음 (참고용). 플레이스홀더만 사용.

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
- RoleId 허용값: `Administrator`, `Operator`, `ReadOnly` (구 펌웨어는 `Admin`/`ReadOnlyUser` 였으나 Reference Guide 2.0b 에서 개명 - `GET /redfish/v1/AccountService/Roles` 로 확인).
- `AccountTypes` 는 Gen 13/14 부터 문서화. X12 에 보내면 거부될 수 있으므로 우선 생략 (unverified).
- 비밀번호: 8~19자 (웹 UI 기준), 3종 이상, 사용자명 역순 금지. 사용 가능 문자 제한은 `GET /redfish/v1/AccountService` 의 `PasswordGuidanceMessage` 가 있으면 참조(6.1 신규 속성, X12 펌웨어 보유 여부 unverified).
- 최대 16 프로파일. 응답 코드 문서 없음 (표준 201).
- 6.1 가이드의 Add Account URI 표기(`/redfish/v1/AccountService`) 는 오기로 보임.

## 대체
- **웹 UI:** Configuration > Account Services > Users > Add : username, password, network privilege (Administrator/Operator/User).
- **ipmitool:**
  ```
  ipmitool -I lanplus -H <BMC_IP> -U <ADMIN_USER> -P <ADMIN_PASSWORD> user list 1
  ipmitool -I lanplus -H <BMC_IP> -U <ADMIN_USER> -P <ADMIN_PASSWORD> user set name <ID> <NEW_USER>
  ipmitool -I lanplus -H <BMC_IP> -U <ADMIN_USER> -P <ADMIN_PASSWORD> user set password <ID> <NEW_PASSWORD> 20
  ipmitool -I lanplus -H <BMC_IP> -U <ADMIN_USER> -P <ADMIN_PASSWORD> channel setaccess 1 <ID> callin=on ipmi=on link=on privilege=4
  ipmitool -I lanplus -H <BMC_IP> -U <ADMIN_USER> -P <ADMIN_PASSWORD> user enable <ID>
  ```
  (`user set password` 만 Supermicro FAQ 41692 로 확인, 나머지는 일반 ipmitool 문법.)
- **SMCIPMITool:** `SMCIPMITool <BMC_IP> <ADMIN_USER> <ADMIN_PASSWORD> user add <ID> <NEW_USER> <NEW_PASSWORD> <PRIV 4|3|2|1>` (비밀번호 8~19자). `user passwd` 가 아니라 `user setpwd`.
- **IPMICFG (대역내):** `./IPMICFG-Linux.x86_64 -user add <ID> <NEW_USER> <NEW_PASSWORD> <PRIV>`
- **SUM `ChangeBmcCfg`:** `sum -i <BMC_IP> -u <ADMIN_USER> -p <ADMIN_PASSWORD> -c GetBmcCfg --file BMCCfg.xml --overwrite` 후 편집, `-c ChangeBmcCfg --file BMCCfg.xml`. 사용자 테이블 요소명은 공개 문서에 없음 (unavailable, 실장비 export 로 확인). 노드 제품키 필요. X12/H12 는 SUM(OOB/In-band) 및 SUM UEFI 지원 (H12 non-RoT 제외 문구는 SUM UEFI 에 한함).
