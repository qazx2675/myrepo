# Dell iDRAC10 - 기존 id 패스워드 변경

적용 모델: XE9780

## Redfish (verified: Dell ChangeIdracUserPasswordREDFISH.py)
```
curl -sk -u <ADMIN_USER>:<CURRENT_PASSWORD> -H "Content-Type: application/json" \
  -X PATCH https://<BMC_IP>/redfish/v1/AccountService/Accounts/<ID> \
  -d '{"Password":"<NEW_PASSWORD>"}'
```
- 성공 HTTP 200. 슬롯 확인: `GET /redfish/v1/AccountService/Accounts?$expand=*($levels=1)`.
- 참고: 스크립트 도움말은 `--user-id` 가 "iDRAC9 이하만" 이라 쓰여 있으나 코드의 iDRAC10 분기는 위 URI 를 사용(도움말 불일치).
- X-Auth-Token 세션의 본인 암호를 바꾸면 그 토큰은 무효 -> 재로그인.

## 공장 기본 암호 변경 강제 시
```
curl -sk -u root:<DEFAULT_OR_LABEL_PASSWORD> -H "Content-Type: application/json" \
  -X PATCH https://<BMC_IP>/redfish/v1/Managers/iDRAC.Embedded.1/Oem/Dell/DellAttributes/iDRAC.Embedded.1 \
  -d '{"Attributes":{"Users.2.Password":"<NEW_PASSWORD>"}}'
```
(Dell 스크립트 `--force-change-enabled`, 공통 코드 경로. iDRAC10 에서의 동작은 코드상 동일하나 실장비 확인 못함.)

## 삭제 (참고, verified: Dell 스크립트)
`DELETE /redfish/v1/AccountService/Accounts/<ID>` -> 204.

## 정책
- 기본 계정 `root`, 암호는 라벨의 고유 암호 또는 `calvin` (Dell KB 000133536).
- 암호 길이 한도: unavailable (장비 `AccountService` 의 Min/MaxPasswordLength 확인).
- 7.x 이후 Basic 인증 Unadvertised (iDRAC10 1.30.10.50+).

## 대체
| 도구 | 명령 | 상태 |
|---|---|---|
| RACADM | `racadm set iDRAC.Users.<SLOT>.Password <NEW_PASSWORD>` | unverified (iDRAC9 KB 구문의 준용) |
| SCP | Export 후 `Users.<SLOT>#Password` 수정, `OemManager.ImportSystemConfiguration` | verified(스크립트 구조) |
| 웹 UI | iDRAC Settings > Users > Edit | unverified |
| ipmitool | `user set password <ID> <NEW_PASSWORD>` | unverified |
| Ansible | dellemc.openmanage.idrac_user (`user_password`) | verified (모듈 소스) |
