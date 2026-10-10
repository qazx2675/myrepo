# Lenovo XCC (XCC1) - 새 계정 id/pw 등록

적용 모델: SR630 / SR650 / SD530 (Purley, PATCH 방식), SR630 V2 (Whitley, POST 방식)
예시의 계정명/암호는 플레이스홀더입니다. `-k` 는 자체서명 인증서 때문이며 운영에서는 CA 검증 권장.

## 0. 사전 확인
1. 관리자 권한(Role `Administrator` = OEM 권한 `Supervisor`, 또는 `UserAccountManagement` 포함 커스텀 롤)으로 인증.
2. 새 계정명/암호가 AccountService 정책(길이·복잡도·변경주기)을 만족하는지 확인 (Lenovo 문서 공통 주의).
3. 방식 판별:
```
curl -sk -u <ADMIN_USER>:<ADMIN_PASSWORD> -D - -o /dev/null https://<BMC_IP>/redfish/v1/AccountService/Accounts
# 응답 헤더 Allow: ... POST ... 이면 방식 A, 없으면 방식 B
```

## A. Whitley (SR630 V2) - POST
- `POST https://<BMC_IP>/redfish/v1/AccountService/Accounts`
- Body 속성(문서): UserName, Password, RoleId (필수), Enabled, PasswordChangeRequired, SNMP{AuthenticationProtocol, EncryptionKey, EncryptionProtocol}
```json
{
  "UserName": "<NEW_USER>",
  "Password": "<NEW_PASSWORD>",
  "RoleId": "Operator",
  "Enabled": true,
  "PasswordChangeRequired": false
}
```
```
curl -sk -u <ADMIN_USER>:<ADMIN_PASSWORD> -H "Content-Type: application/json" \
  -X POST https://<BMC_IP>/redfish/v1/AccountService/Accounts \
  -d '{"UserName":"<NEW_USER>","Password":"<NEW_PASSWORD>","RoleId":"Operator","Enabled":true}'
```
- 응답: 새 계정의 GET 내용(예 `@odata.id: /redfish/v1/AccountService/Accounts/4`, `Password: null`, `Locked:false`). 성공 코드: 문서 미명시(공식 샘플 스크립트는 200/201/204 성공 처리).
- 문서 예시 `RoleId: Administrator`, `PasswordChangeRequired: true`. 예시 응답 `@odata.type #ManagerAccount.v1_6_0.ManagerAccount`.

## B. Purley (SR630 / SR650 / SD530) - PATCH 빈 슬롯
Purley 는 슬롯 12 개가 미리 존재(`Accounts/1`..`Accounts/12`). POST 로 새로 만들 수 없음.
1. `GET /redfish/v1/AccountService/Accounts` 로 멤버 확인
2. 각 `GET /redfish/v1/AccountService/Accounts/N` 에서 `"UserName": ""` 인 슬롯 선택 (없으면 가득 참)
3. 해당 슬롯 PATCH
```json
{
  "UserName": "<NEW_USER>",
  "Password": "<NEW_PASSWORD>",
  "RoleId": "Operator",
  "Enabled": true
}
```
```
curl -sk -u <ADMIN_USER>:<ADMIN_PASSWORD> -H "Content-Type: application/json" \
  -H "If-Match: <ETAG_OF_SLOT>" \
  -X PATCH https://<BMC_IP>/redfish/v1/AccountService/Accounts/<EMPTY_SLOT> \
  -d '{"UserName":"<NEW_USER>","Password":"<NEW_PASSWORD>","RoleId":"Operator","Enabled":true}'
```
- 문서 예시 응답은 `Enabled:false` 로 나오므로 `Enabled:true` 를 명시하는 것이 안전(공식 샘플 스크립트도 payload 에 Enabled 포함).
- `If-Match`(ETag): 공식 문서에는 필수 여부가 없으나 Lenovo 샘플 스크립트가 슬롯을 GET 해 ETag 를 If-Match 로 보냄 -> 보내는 것을 권장. 필요 여부는 장비에서 확인(unverified).
- `PasswordChangeRequired` 는 **Purley 에서 지원 안 됨** (XCC 문서: "Not available on Intel Purley-based systems").
- 커스텀 권한이 필요하면 `PATCH /redfish/v1/AccountService/Roles/CustomRole{N}` 로 OemPrivileges 설정 후 슬롯에 `Links.Role` 연결.

## 권한/롤
| RoleId | OEM 권한 | 비고 |
|---|---|---|
| Administrator | Supervisor | 계정 관리 가능 (ConfigureUsers) |
| Operator | RemoteServerPowerRestartAccess, AbilityClearEventLogs, Configuration_* | **UserAccountManagement 없음** |
| ReadOnly | ReadOnly | 로그인·자기 설정만 |
| CustomRole{N} | 지정 | `UserAccountManagement` 포함 시 계정 관리 가능 |

(권한표는 XCC2 Role 문서 기준. XCC1 도 같은 구조라고 문서가 명시하지는 않음 - 장비에서 `GET /redfish/v1/AccountService/Roles/<Id>` 확인)

## 제약 / 패스워드 정책
- 슬롯: 12 개 (`Accounts/{1..12}`), 이름 `User1`..`User12`.
- 암호 복잡도(기본 ON): 영문+숫자 필수, 대/소문자/특수문자 중 2종 이상, 동일문자 3연속 금지, 사용자명/역순/반복 금지, 공백 불가, 길이 8~32 (XCC1 사용자 가이드). 복잡도 OFF 시 최소길이 0~32.
- IPMI 접근을 줄 계정은 암호 20자 이하 (XCC2/XCC3 문서에 명시, XCC1 은 안전하게 20자 이하 권장).
- 사용자명: 영문/숫자/마침표/밑줄, 4~16자 (XCC `users` CLI 문서 기준).
- 기본 계정: `USERID` / `PASSW0RD`(Supervisor) - 초기에 변경 필수. 강제변경 옵션 기본 ON.

## 흔한 오류 (문서 + 공식 샘플)
| 상황 | 원인 |
|---|---|
| POST 거부(405 계열) | Purley 장비. 방식 B 사용 |
| `Accounts is full` (샘플 스크립트 메시지) | 빈 슬롯 없음 -> 불필요 계정 정리 |
| ResourceAlreadyExists | 같은 UserName 존재 (XCC2/3 문서 메시지 ID) |
| 암호 정책 위반 | PropertyValueFormatError 류 (XCC2/3 문서 400 계열 메시지 ID) |
| 500 InternalError | XCC1 문서가 유일하게 표기한 코드 |

## Redfish 를 쓸 수 없을 때 (대체)
- XCC SSH CLI: `users -<slot> -n <NEW_USER> -p <NEW_PASSWORD> -r Administrator` (문서 예시 `users -2 -n sptest -p ... -r Administrator`, 첫 로그인 시 변경 요구)
- OneCLI: `onecli config set IMM.LoginId.<slot> <NEW_USER>`, `IMM.Password.<slot>`, `IMM.LoginRole.<slot>` (`--bmc <USER>:<PASS>@<BMC_IP>`) - 설정 이름은 Lenovo XCC3 설정명 매핑 페이지(레거시 IMM.*)에서 확인, 정확한 조합 문법은 `onecli config set --help` 로 확인 (unverified)
- IPMI(`ipmitool user set name/password`): 각 XCC 로그인 ID 는 대응 IPMI User ID 를 가짐(Lenovo 문서). 일반 ipmitool 문법은 change_password.md 참조 (unverified)
- 웹 UI: BMC Configuration > User/LDAP > Create
