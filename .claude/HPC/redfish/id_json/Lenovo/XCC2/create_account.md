# Lenovo XCC2 - 새 계정 id/pw 등록

적용 모델: SR645 V3, SR675 V3, SD650 V3
플레이스홀더만 사용. `-k` 는 자체서명 인증서용.

## Redfish
- `POST https://<BMC_IP>/redfish/v1/AccountService/Accounts`
- 권한: `Administrator`(OEM `Supervisor`, ConfigureUsers) 또는 `UserAccountManagement` 가 있는 커스텀 롤. (문서 페이지 자체에는 필요 권한 미기재 - 롤 문서 기준)
- 사전: 계정명/암호가 AccountService 규칙(길이·복잡도·변경주기)을 만족해야 함.

| 속성 | 타입 | 설명 |
|---|---|---|
| UserName | String | 새 계정명 (필수) |
| Password | String | 새 암호 (필수) |
| RoleId | String | Administrator / Operator / ReadOnly / CustomRoleN (필수) |
| Enabled | Boolean | 활성 여부 |
| PasswordChangeRequired | Boolean | 첫 로그인 시 암호 변경 요구 |
| AccountTypes | Array | Redfish, SNMP, ManagerConsole, IPMI, WebUI |
| SNMP | Object | AccountTypes 에 SNMP 가 있을 때만: AuthenticationProtocol, EncryptionKey(<=32자), EncryptionProtocol |

```json
{
  "UserName": "<NEW_USER>",
  "Password": "<NEW_PASSWORD>",
  "RoleId": "Operator",
  "Enabled": true,
  "PasswordChangeRequired": false,
  "AccountTypes": ["Redfish", "WebUI", "ManagerConsole"]
}
```
```
curl -sk -u <ADMIN_USER>:<ADMIN_PASSWORD> -H "Content-Type: application/json" \
  -X POST https://<BMC_IP>/redfish/v1/AccountService/Accounts \
  -d '{"UserName":"<NEW_USER>","Password":"<NEW_PASSWORD>","RoleId":"Operator","Enabled":true,"AccountTypes":["Redfish","WebUI","ManagerConsole"]}'
```
- IPMI 를 AccountTypes 에 넣는 계정은 **암호 20자 이하**. SNMP 를 넣으면 EncryptionKey 32자 이하, EncryptionProtocol 이 none 이 아니면 AuthenticationProtocol 도 none 이 아니어야 함.
- 응답: 새 계정의 GET 내용 (`Password:null`, `PasswordExpiration`, `@odata.id` 가 새 슬롯). 성공 코드 문서 미명시(Lenovo 샘플 스크립트는 200/201/204 성공 처리).
- 문서 예시: Administrator, PasswordChangeRequired true, AccountTypes 4종 + SNMPv3 HMAC_SHA96 / CFB128_AES128.

## 제약
- 슬롯 `Accounts/{1..12}` (GET/PATCH 문서). 컬렉션에 `HostBootStrap` 멤버가 있을 수 있으나 호스트 인터페이스 부트스트랩 전용(HostBootstrapAccount) 이므로 건드리지 말 것.
- 이미 같은 UserName 이 있으면 ResourceAlreadyExists, 필수 속성 누락 시 CreateFailedMissingReqProperties/PropertyMissing.
- 패스워드 정책: 복잡도 기본 ON, 8~32자(XCC2 사용자 가이드), 영문+숫자, 대/소/특수 중 2종, 동일문자 3연속 금지, 사용자명/역순 금지, 공백 불가. 복잡도 OFF 면 최소길이 0~32. 최소 변경 간격 0~240시간, 실패 잠금 0~10회, 잠금시간 60~172800초(Redfish).
- 사용자명 규칙(XCC2 CLI 문서): 영문/숫자/마침표/밑줄 4~16자.
- RoleId 커스텀: `POST /redfish/v1/AccountService/Roles` body `{"RoleId":"<ROLE>","OemPrivileges":["UserAccountManagement", ...]}` (RoleId 1~32자: A-Z a-z 0-9 - . _). 허용 OemPrivileges: UserAccountManagement, RemoteConsoleAccess, RemoteConsoleAndVirtualMediaAccess, RemoteServerPowerRestartAccess, AbilityClearEventLogs, Configuration_Basic, Configuration_NetworkingAndSecurity, Configuration_Advanced, Configuration_UEFISecurity.

## 흔한 오류
| 메시지 ID | 의미 |
|---|---|
| ResourceAlreadyExists | 동일 UserName 존재 |
| PropertyValueFormatError / PropertyValueTypeError | 암호·값 형식/정책 위반, 타입 오류 |
| CreateFailedMissingReqProperties / PropertyMissing | 필수 속성 누락 |
| ForbiddenOperation / NotRecommandedOperation | 금지 동작(예: 보호 대상 계정/롤) |
| PasswordChangeRequired | 해당 계정 암호 변경 필요 |
| InternalError (500) | 내부 오류 |

## 대체
- XCC SSH CLI: `users -<slot> -n <NEW_USER> -p <NEW_PASSWORD> -r Administrator` (XCC2 CLI 문서 예시. 생성 직후 첫 로그인 시 변경 요구. CLI 문서 표는 암호 6~20자/영문+비영문 포함이라 적고 문법란은 32자 - 문서 내부 불일치)
- OneCLI(XCC2, 레거시 설정명): `IMM.LoginId.<slot>`, `IMM.Password.<slot>`, `IMM.LoginRole.<slot>` (`onecli config set ...`) - 조합 문법 unverified
- IPMI: 각 XCC 로그인 ID 는 대응 IPMI User ID 가 있고 권한이 매핑됨(Supervisor->Administrator, Read Only->User 등, XCC 사용자 계정 문서). ipmitool 명령은 change_password.md 의 표 참조 (unverified)
- 웹 UI: BMC Configuration > User/LDAP > Create
