# Lenovo XCC3 - 새 계정 id/pw 등록

적용: ThinkSystem V4 (XCC3). 사용자 보유 모델 해당 없음(인접 최신 버전 조사).
플레이스홀더만 사용. `-k` 는 자체서명 인증서용.

## Redfish
- `POST https://<BMC_IP>/redfish/v1/AccountService/Accounts`
- 사전: 계정명/암호가 AccountService 규칙(길이·복잡도·변경주기)을 만족해야 함.
- 필요 권한: Administrator(OEM Supervisor) 또는 UserAccountManagement 포함 커스텀 롤 (XCC2 롤 문서 기준, XCC3 페이지에는 명시 없음).

| 속성 | 타입 | 설명 |
|---|---|---|
| UserName | String | 새 계정명 |
| Password | String | 새 암호 |
| RoleId | String | 새 계정 롤 ID |
| AccountTypes | Array | Redfish, SNMP, ManagerConsole, IPMI, WebUI |
| Enabled | Boolean | 활성 여부 |
| SNMP | Object | AccountTypes 에 SNMP 가 있을 때만 (AuthenticationProtocol, EncryptionKey, EncryptionProtocol) |

(XCC3 POST 페이지 표에는 `PasswordChangeRequired` 가 없다. XCC2 POST 에는 있음. XCC3 에서 생성 시 강제변경이 필요하면 전역 정책 "PasswordChangeOnFirstAccess" 사용 또는 생성 후 PATCH 로 PasswordChangeRequired 설정을 시도 - 후자는 XCC3 PATCH 표에도 없으므로 unverified.)

```json
{
  "UserName": "<NEW_USER>",
  "Password": "<NEW_PASSWORD>",
  "RoleId": "Operator",
  "AccountTypes": ["Redfish", "WebUI", "ManagerConsole"],
  "Enabled": true
}
```
```
curl -sk -u <ADMIN_USER>:<ADMIN_PASSWORD> -H "Content-Type: application/json" \
  -X POST https://<BMC_IP>/redfish/v1/AccountService/Accounts \
  -d '{"UserName":"<NEW_USER>","Password":"<NEW_PASSWORD>","RoleId":"Operator","AccountTypes":["Redfish","WebUI","ManagerConsole"],"Enabled":true}'
```
- 응답: 새 계정의 GET 내용 (Password null). 성공 코드 문서 미표기 (샘플 스크립트 200/201/204).
- 제약: IPMI 접근 계정 암호 <= 20자, SNMP EncryptionKey <= 32자, EncryptionProtocol != none 이면 AuthenticationProtocol != none.
- 슬롯: `Accounts/{1..14}` (GET 문서). HostBootStrap 은 별도 계정(호스트 인터페이스용).
- 정책: 복잡도 기본 ON, 길이 8~255 (XCC3 사용자 가이드). 허용문자 A-Z a-z 0-9 ~`!@#$%^&*()-+={}[]|:;"'<>,?/._ , 공백 불가. 영문+숫자 필수, 대/소/특수 2종 이상, 동일문자 3연속 금지, 사용자명/역순 금지. 복잡도 OFF 시 최소길이 0~255.
- 기본 계정 `USERID`/`PASSW0RD`(Supervisor), "Force to change password on first access" 기본 ON (XCC3 사용자 가이드).

## 흔한 오류
ResourceAlreadyExists(중복 UserName), CreateFailedMissingReqProperties/PropertyMissing(필수 속성 누락), PropertyValueFormatError/PropertyValueTypeError(형식·정책), ForbiddenOperation/NotRecommandedOperation, PasswordChangeRequired, 500 InternalError. (문서는 ID 별 상태코드 미매핑)

## OneCLI 설정명 (XCC3 는 레거시 IMM.* 대신 BMC.*)
| 의미 | XCC3 | 레거시 XCC/XCC2 |
|---|---|---|
| 로그인 ID (슬롯 1~12) | BMC.LoginID_1 ... BMC.LoginID_12 | IMM.LoginId.1 ... .12 |
| 암호 | BMC.Password_1 ... _12 | IMM.Password.1 ... .12 |
| 권한 레벨 | BMC.AuthorityLevel_1 ... _12 | IMM.LoginRole.1 ... .12 |
| 허용 인터페이스 | BMC.AccessibleInterfaces_1 ... _12 | IMM.Accessible_Interfaces.1 ... .12 |
| 복잡도 규칙 | BMC.ComplexPassword | IMM.ComplexPassword |
| 첫 접속 변경 | BMC.PasswordChangeOnFirstAccess | IMM.FirstAccessPwChange |

- OneCLI 5.x / UpdateXpress 5.x / BOMC 14.x 필요. `-c` 호환 모드는 V4 에서 일부만 번역(미번역은 error 104). 
- 예(조합 문법 unverified): `onecli config set BMC.LoginID_2 <NEW_USER> --bmc <USER>:<PASS>@<BMC_IP>` 등. 슬롯 수는 이 표에는 1~12 로 적혀 있어 Redfish(1~14)와 다름 - 장비에서 `onecli config show` 로 확인.

## 대체
- XCC CLI `users -<slot> -n <NEW_USER> -p <NEW_PASSWORD> -r Administrator` (XCC 계열 CLI; XCC3 페이지는 직접 확인 못함 - XCC2 문서 기준, unverified)
- ipmitool: 각 로그인 ID 는 IPMI User ID 에 대응 (change_password.md 표 참조, unverified)
- 웹 UI: BMC Configuration > User/LDAP > Create
