# Lenovo XCC2 - 기존 id 패스워드 변경

적용 모델: SR645 V3, SR675 V3, SD650 V3

## Redfish
- `PATCH https://<BMC_IP>/redfish/v1/AccountService/Accounts/{1..12}`
- 문서: 각 속성은 개별적으로 변경 가능 -> `Password` 단독 전송 가능. 응답의 Password 는 null.
```json
{ "Password": "<NEW_PASSWORD>" }
```
```
curl -sk -u <ADMIN_USER>:<CURRENT_PASSWORD> -H "Content-Type: application/json" \
  -X PATCH https://<BMC_IPADDR>/redfish/v1/AccountService/Accounts/<SLOT> \
  -d '{"Password":"<NEW_PASSWORD>"}'
```
- 같은 요청에서 변경 가능한 속성: UserName, Password, RoleId, Enabled, PasswordChangeRequired, AccountTypes, SNMP{...}.
- 슬롯 찾기: `GET /redfish/v1/AccountService/Accounts` -> 각 `Accounts/N` 의 `UserName` 확인.
- 문서 예시 body `{"UserName":"USERID","RoleId":"Administrator","PasswordChangeRequired":false}`. 예시 응답 `@odata.type #ManagerAccount.v1_8_1.ManagerAccount`, `Password:null`, `PasswordExpiration:null`.
- 성공 코드: 문서 미표기 (샘플 스크립트는 PATCH 200/204 를 성공 처리).
- If-Match(ETag): 문서 필수 표기 없음. 샘플 스크립트는 PATCH 시 ETag 를 보냄 -> 412/428 나오면 GET 으로 ETag 확보 후 재시도 (unverified).

## 권한
- 타 계정: Administrator(Supervisor, ConfigureUsers) 또는 UserAccountManagement 포함 커스텀 롤.
- 자기 자신: Operator/ReadOnly 의 AssignedPrivileges 에 ConfigureSelf 포함 -> 자기 암호 변경 가능으로 추정(장비 확인 필요).

## 정책/제약
- IPMI AccountTypes 포함 계정은 암호 20자 이하. SNMP EncryptionKey 32자 이하.
- 복잡도 기본 ON: 8~32자(XCC2 사용자 가이드), 영문+숫자, 대/소/특수 2종, 동일문자 3연속 금지, 사용자명·역순 금지, 공백 불가. 재사용 주기 최대 10개 검사, 변경 간격 0~240시간.
- AccountService: `MinPasswordLength` 기본 10(문서), `MaxPasswordLength` 255 고정(문서), `AccountLockoutThreshold` 0~10, `AccountLockoutDuration` 0(null) 또는 60~172800초, `AccountLockoutCounterResetEnabled/After`. 변경은 `PATCH /redfish/v1/AccountService` ("Update global account lockout properties and LDAP properties" 페이지).
- `Locked:true` 계정은 잠금 시간 경과 후 해제(자동). 잠금 해제용 별도 문서 URI 확인 못함.

## 강제 변경 상태
- 기본 계정 `USERID`/`PASSW0RD`(Supervisor), "Force to change password on first access" 기본 ON.
- 변경 필요 상태에서는 `Base.1.12.PasswordChangeRequired` 메시지: "PATCH the Password property for this account located at the target URI '/redfish/v1/AccountService/Accounts/<N>' to complete this process." (XClarity 실사례, Checkmk 포럼). HTTP 상태 근거 없음.
- 해결: 그 계정으로 인증 후 위 PATCH 로 Password 변경. 기본 암호 PASSW0RD 로 되돌릴 수 없음.

## 변경 후 세션 처리
- 기존 세션 무효화 여부는 Lenovo 문서에 명시 없음 (unverified).
- 권장: 새 암호로 재인증. `POST /redfish/v1/SessionService/Sessions` {"UserName","Password"} -> 201 + `X-Auth-Token`/`Location`. Basic 인증도 가능. 변경 후 `GET Accounts/<SLOT>` 로 `PasswordChangeRequired:false`, `Locked:false`, `PasswordExpiration` 확인.

## 대체
| 도구 | 명령 | 상태 |
|---|---|---|
| XCC SSH CLI | `users -<slot> -p <NEW_PASSWORD>` | 문서 확인(users 명령 옵션 -p) |
| OneCLI | `onecli config set IMM.Password.<slot> <NEW_PASSWORD> --bmc <USER>:<PASS>@<BMC_IP>` | 설정명 확인, 조합 unverified |
| ipmitool | `ipmitool -I lanplus -H <BMC_IP> -U <USER> -P <PASS> user list 1` 후 `user set password <ID> <NEW_PASSWORD> 20` | unverified (일반 ipmitool 문법) |
| 웹 UI | BMC Configuration > User/LDAP > 사용자 선택 > Edit | 문서 확인 |
