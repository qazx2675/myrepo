# Lenovo XCC (XCC1) - 기존 id 패스워드 변경

적용 모델: SR630 / SR650 / SD530 (Purley), SR630 V2 (Whitley). 변경 방식은 두 플랫폼 모두 동일한 PATCH.

## Redfish
- `PATCH https://<BMC_IP>/redfish/v1/AccountService/Accounts/{1..12}`
- 문서: "각 속성은 개별적으로 변경 가능" -> `Password` 만 단독 전송 가능. 응답의 `Password` 는 null.
```json
{ "Password": "<NEW_PASSWORD>" }
```
```
# 1) 대상 슬롯 찾기
curl -sk -u <ADMIN_USER>:<CURRENT_PASSWORD> https://<BMC_IP>/redfish/v1/AccountService/Accounts/1
# 2) 변경
curl -sk -u <ADMIN_USER>:<CURRENT_PASSWORD> -H "Content-Type: application/json" \
  -H "If-Match: <ETAG>" \
  -X PATCH https://<BMC_IP>/redfish/v1/AccountService/Accounts/<SLOT> \
  -d '{"Password":"<NEW_PASSWORD>"}'
```
- 같은 body 에 `UserName`, `RoleId`, `Enabled`, `PasswordChangeRequired`(Purley 제외) 포함 가능.
- 응답: 계정 GET 내용(변경된 속성 반영). 문서 예시 body `{"UserName":"USERID","RoleId":"Administrator","PasswordChangeRequired":false}` -> 응답 `Password:null`.
- If-Match: 문서에 필수 표기 없음. 샘플 스크립트가 PATCH 시 ETag 사용 -> 412/428 이 나오면 ETag 를 GET 후 재시도 (unverified).

## 권한
- 타 계정 변경: `Administrator`(Supervisor) 또는 `UserAccountManagement` 포함 커스텀 롤.
- 자기 계정: Operator/ReadOnly 롤의 AssignedPrivileges 에 `ConfigureSelf` 포함 (XCC2 Role 문서 표) -> 자기 암호 변경은 가능한 것으로 추정(Redfish 표준 의미), 장비 확인 필요.

## 강제 변경 상태 (기본 계정 / PasswordChangeRequired=true)
- XCC 설정 "Force to change password on first access"(기본 ON), "Force default account password must be changed on next login"(기본 ON).
- 변경 전까지 Redfish 요청은 `Base.1.12.PasswordChangeRequired` 메시지로 거부: "PATCH the Password property for this account located at the target URI '/redfish/v1/AccountService/Accounts/<N>'" (실제 XClarity 사례, Checkmk 포럼). HTTP 상태 코드는 근거 확보 못함.
- 해결: 해당 계정으로 인증하여 위 PATCH 로 `Password` 변경.
- 새 암호로 `PASSW0RD` 재설정 불가 (Lenovo 문서).

## 패스워드 정책
- 복잡도(기본 ON): 8~32자(XCC1 가이드), 영문+숫자, 대/소/특수 중 2종, 동일문자 3연속 금지, 사용자명 관련 금지, 공백 불가. 최소 재사용 주기(최대 10개 검사), 최소 변경 간격 0~240시간, 로그인 실패 0~10회 잠금.
- AccountService 값(`MinPasswordLength`, 잠금 등)은 `GET /redfish/v1/AccountService` 로 확인, `PATCH /redfish/v1/AccountService` 로 변경.
- IPMI 접근 계정은 20자 이하 권장.
- 최근 암호 재사용/변경 간격 위반 시 실패 가능 (Lenovo 가이드 정책 항목).

## 변경 후 세션 처리
- Lenovo 문서에 "암호 변경 시 기존 세션이 무효화되는지"는 명시 없음 -> unverified.
- 권장: 변경 후 새 암호로 다시 인증. 세션 토큰 방식은 `POST /redfish/v1/SessionService/Sessions` {"UserName","Password"} -> 201 + `X-Auth-Token` 헤더 (XCC 최대 16 세션). Basic 인증도 지원.
- 변경 직후 `GET /redfish/v1/AccountService/Accounts/<SLOT>` 로 `PasswordChangeRequired:false`, `Locked:false`, `PasswordExpiration` 확인.

## 대체 방법
| 도구 | 명령 (플레이스홀더) | 상태 |
|---|---|---|
| XCC SSH CLI | `users -<slot> -p <NEW_PASSWORD>` (`users -2 -n <NEW_USER> -p <NEW_PASSWORD> -r Administrator` 예시가 문서에 있음) | 문서 확인 |
| OneCLI | `onecli config set IMM.Password.<slot> <NEW_PASSWORD> --bmc <USER>:<PASS>@<BMC_IP>` | 설정명은 문서 확인, 조합 문법 unverified |
| ipmitool | `ipmitool -I lanplus -H <BMC_IP> -U <USER> -P <PASS> user list 1` -> ID 확인 후 `user set password <ID> <NEW_PASSWORD> 20` | unverified (일반 ipmitool 문법. Lenovo 는 "각 XCC 로그인 ID 는 IPMI User ID 와 대응"만 명시) |
| 웹 UI | BMC Configuration > User/LDAP > 계정 선택 > Edit | 문서 확인 |
