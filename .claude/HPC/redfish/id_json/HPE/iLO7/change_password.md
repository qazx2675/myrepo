# HPE iLO 7 - 기존 id 패스워드 변경 (best-effort, 사용자 모델 없음)

## Redfish
- `PATCH https://<BMC_IP>/redfish/v1/AccountService/Accounts/{id}/`
```json
{ "Password": "<NEW_PASSWORD>" }
```
```
curl -sk -u <ADMIN_USER>:<CURRENT_PASSWORD> -H "Content-Type: application/json" \
  -X PATCH https://<BMC_IP>/redfish/v1/AccountService/Accounts/<ID>/ \
  -d '{"Password":"<NEW_PASSWORD>"}'
```
- 슬롯 찾기: `GET .../Accounts/?$filter=UserName eq '<TARGET_USER>'` (HPE managingusers 예시, iLO 7 포함 서술) 또는 컬렉션 멤버 순회. Id 는 `65536`.. 대일 수 있음.
- 같이 변경 가능: UserName, RoleId(권한 초기화), Oem.Hpe.LoginName, Oem.Hpe.Privileges.*, Enabled.
- 검증: verified (문서) / 응답 코드·If-Match unverified.

## 권한
- `UserConfigPriv` 필요. **애플리케이션 계정 세션은 UserConfigPriv 없음 -> 불가.**

## 암호를 모를 때
- iLO 4/5/6 의 "자격증명 없는 in-band Administrator 암호 변경" 은 **iLO 7 에서 불가** (CHIF 제거, Production 보안 모드 없음; HPE 블로그 Note). 공장 라벨 암호 또는 공장 초기화(모든 설정 삭제) 후 기본 계정 사용. 초기화 전 `Oem.Hpe.DefaultUserName/DefaultPassword` 지정 속성이 iLO 7 에도 있는지는 unverified.

## 대체
- ilorest: `ilorest iloaccounts changepass <ID 또는 USERNAME> <NEW_PASSWORD>` (iLO 7 전용 예시 미확인, verified for iLO 4/5/6 문서).
- RIBCL/SSH CLI/IPMI: iLO 7 근거 없음 -> unverified / unavailable.

## 정책
- 표준 `MinPasswordLength/MaxPasswordLength`(읽기전용), `Oem.Hpe.MinPasswordLength`, `Oem.Hpe.EnforcePasswordComplexity`, `AccountLockout*`: iLO 7 에서의 값·존재는 **unverified** -> `GET /redfish/v1/AccountService` 로 확인.

## 삭제
`DELETE /redfish/v1/AccountService/Accounts/{id}/` (UserConfigPriv). 앱 계정은 `/redfish/v1/AccountService/Oem/Hpe/AppAccounts/{id}`(별도).
