# Lenovo XCC3 - 기존 id 패스워드 변경

적용: ThinkSystem V4 (XCC3). 사용자 보유 모델 해당 없음.

## Redfish
- `PATCH https://<BMC_IP>/redfish/v1/AccountService/Accounts/{1..14}`
- 문서: 각 속성은 개별적으로 변경 가능, Password 는 PATCH 응답에서 null.
```json
{ "Password": "<NEW_PASSWORD>" }
```
```
curl -sk -u <ADMIN_USER>:<CURRENT_PASSWORD> -H "Content-Type: application/json" \
  -X PATCH https://<BMC_IP>/redfish/v1/AccountService/Accounts/<SLOT> \
  -d '{"Password":"<NEW_PASSWORD>"}'
```
- 변경 가능 속성: UserName, Password, RoleId, AccountTypes, Enabled, SNMP{AuthenticationProtocol, EncryptionProtocol, EncryptionKey}. SNMP 항목은 AccountTypes 에 SNMP 가 없으면 설정 불가.
- 제약: IPMI 접근 계정 암호 <= 20자, SNMP EncryptionKey <= 32자, EncryptionProtocol != none 이면 AuthenticationProtocol != none.
- 슬롯 찾기: `GET /redfish/v1/AccountService/Accounts` -> `Accounts/N` 의 UserName.
- 응답: 계정 GET 내용. 문서 오류 ID: 500 InternalError / 400 PropertyValueTypeError, PropertyValueFormatError, ResourceChangeRequried, NotRecommandedOperation, ForbiddenOperation, ResourceAlreadyExists, PropertyMissing, PasswordChangeRequired.
- XCC3 PATCH 문서에는 요청/응답 예시가 없음 -> XCC2 예시(`{"UserName":"USERID","RoleId":"Administrator","PasswordChangeRequired":false}`)를 준용(inferred). XCC3 PATCH 속성 표에는 `PasswordChangeRequired` 가 없음 - 계정 GET 에는 있음(읽기 전용일 가능성, unverified).

## 정책
- AccountService: `MinPasswordLength` 기본 10, `MaxPasswordLength` 255 고정, `AccountLockoutThreshold` 0~10, `AccountLockoutDuration` null(0) 또는 60~172800초.
- 사용자 가이드: 복잡도 ON 시 8~255자, 영문+숫자 필수 + 대/소/특수 2종, 동일문자 3연속 금지, 사용자명/역순 금지, 공백 불가. 복잡도 OFF 시 최소길이 0~255. 재사용 주기·변경 간격·만료/경고 기간·실패 잠금 설정 존재(기본값 문서 미기재).
- 기본 계정 USERID/PASSW0RD 는 첫 접속 시 변경 강제(기본 ON), PASSW0RD 로 되돌릴 수 없음.

## 강제 변경 / 세션
- 암호 변경 필요 상태에서는 `Base.1.12.PasswordChangeRequired` 메시지로 요청 거부 + 해당 계정 URI 에 Password PATCH 안내 (Lenovo XClarity 실사례, Checkmk 포럼; XCC 세대 구분은 포럼 글에 없음).
- 변경 후 기존 세션 무효화 여부는 문서 미기재 (unverified). 새 암호로 재인증 권장: `POST /redfish/v1/SessionService/Sessions` -> 201 + `X-Auth-Token`.
- 변경 후 `GET Accounts/<SLOT>` 의 `Locked`, `PasswordChangeRequired`, `PasswordExpiration` 확인.

## 대체 도구
| 도구 | 명령 | 상태 |
|---|---|---|
| OneCLI 5.x | `onecli config set BMC.Password_<slot> <NEW_PASSWORD> --bmc <USER>:<PASS>@<BMC_IP>` | 설정명 문서 확인, 조합 문법 unverified |
| XCC CLI (SSH) | `users -<slot> -p <NEW_PASSWORD>` | XCC2 문서 기준, unverified for XCC3 |
| ipmitool | `ipmitool -I lanplus -H <BMC_IP> -U <USER> -P <PASS> user list 1` 후 `user set password <ID> <NEW_PASSWORD> 20` | unverified (일반 ipmitool 문법; IPMI 암호 20자 한도는 Lenovo 문서) |
| 웹 UI | BMC Configuration > User/LDAP > 사용자 > Edit | 문서 확인 |
