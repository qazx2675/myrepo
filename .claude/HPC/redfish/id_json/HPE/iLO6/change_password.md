# HPE iLO 6 - 기존 id 패스워드 변경

적용 모델: DL360 G11, DL380 G11, DL560 G11

## Redfish
- `PATCH https://<BMC_IP>/redfish/v1/AccountService/Accounts/{id}/`
- `Password` PATCH 가능(읽기 시 null). 최대 39자, 최소 AccountService `MinPasswordLength`(iLO 6 레퍼런스 ManagerAccount/Password).
```json
{ "Password": "<NEW_PASSWORD>" }
```
```
curl -sk -u <ADMIN_USER>:<CURRENT_PASSWORD> "https://<BMC_IP>/redfish/v1/AccountService/Accounts/?%24filter=UserName%20eq%20'<TARGET_USER>'"
curl -sk -u <ADMIN_USER>:<CURRENT_PASSWORD> -H "Content-Type: application/json" \
  -X PATCH https://<BMC_IP>/redfish/v1/AccountService/Accounts/<ID>/ \
  -d '{"Password":"<NEW_PASSWORD>"}'
```
- 슬롯 찾기: 컬렉션 GET 후 각 멤버 `UserName` 확인(또는 `$filter`, HPE managingusers 예시).
- 같이 변경 가능: `UserName`, `RoleId`(권한 초기화됨 주의), `Oem.Hpe.LoginName`, `Oem.Hpe.Privileges.*`, `Enabled`.
- If-Match: 문서 예시 미사용(unverified). 응답 상태 코드 문서 미표기(unverified).
- 검증: verified (iLO 6 레퍼런스 + HPE managingusers).

## 권한
- 타 계정: `UserConfigPriv`. 자기 자신 Redfish PATCH: unverified (iLO 4 UG 는 웹 UI 에서 권한 없어도 자기 암호 변경 가능이라 서술).

## 기본 Administrator 암호 변경 / 분실
- 기본 `Administrator`, pull tab 랜덤 8자 또는 공통 기본 암호(SKU P08040-B21). Id 는 `UserName == Administrator` 로 확인(보통 `1`).
- **암호 모를 때 in-band (Production 보안 상태)**: OS 에서 ilorest CHIF 로 자격증명 없이 `{"path":"/redfish/v1/AccountService/Accounts/1/","body":{"Password":"<NEW_PASSWORD>"}}` 전송(HPE 블로그, iLO 4/5/6 대상, 2025-11 갱신). 상위 보안 상태(High/FIPS/CNSA)는 자격증명 필요. 정확한 ilorest 서브명령은 그림이라 텍스트 확인 못함(unverified). `Require Host Authentication` 이 켜져 있으면 로컬에서도 인증 필요(UG).
- 공장 초기화는 모든 설정 삭제(`ilorest factorydefaults` 또는 Manager Action). 초기화 전에 `Oem.Hpe.DefaultUserName/DefaultPassword` 로 초기화 후 계정 지정 가능.

## 대체
| 도구 | 명령 | 상태 |
|---|---|---|
| ilorest | `ilorest iloaccounts changepass <ID 또는 USERNAME> <NEW_PASSWORD>` | verified |
| RIBCL | `<MOD_USER USER_LOGIN="<USER>"><PASSWORD value="<NEW_PASSWORD>"/></MOD_USER>` + `hponcfg`/`hpqlocfg` | verified(구조) / 원문 unverified; sustenance stage |
| SSH CLI | `set /map1/accounts1/<USER> password=<NEW_PASSWORD>` | iLO 6 unverified |
| 웹 UI | Administration > User Administration > Edit > Change password | verified (UG) |
| ipmitool | HPE 문서 근거 없음 | unavailable |

## 정책/제약
- `Oem.Hpe.MinPasswordLength` 0~39(기본 8), `Oem.Hpe.EnforcePasswordComplexity`(기본 false), 표준 `MinPasswordLength/MaxPasswordLength`(읽기전용), `AccountLockout*`.
- IPMI 계정은 암호 20자 이하.
- 변경 후 기존 세션 무효화: 문서 명시 없음(unverified). 새 암호로 재인증.

## 삭제
`DELETE /redfish/v1/AccountService/Accounts/{id}/`, `ilorest iloaccounts delete <ID|USERNAME>`, RIBCL `DELETE_USER`, 웹 UI Delete.
