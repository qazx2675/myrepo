# HPE iLO 5 - 기존 id 패스워드 변경

적용 모델: DL360 G10, DL560 G10, DL580 G10, XL270d G10, DL360 G10 Plus

## Redfish
- `PATCH https://<BMC_IP>/redfish/v1/AccountService/Accounts/{id}/`
- `Password` 는 PATCH 가능, GET 에서는 항상 null (iLO 5 레퍼런스: 최대 39자, 최소는 AccountService `MinPasswordLength`).
```json
{ "Password": "<NEW_PASSWORD>" }
```
```
# 슬롯 찾기
curl -sk -u <ADMIN_USER>:<CURRENT_PASSWORD> "https://<BMC_IP>/redfish/v1/AccountService/Accounts/?%24filter=UserName%20eq%20'<TARGET_USER>'"
# 변경
curl -sk -u <ADMIN_USER>:<CURRENT_PASSWORD> -H "Content-Type: application/json" \
  -X PATCH https://<BMC_IP>/redfish/v1/AccountService/Accounts/<ID>/ \
  -d '{"Password":"<NEW_PASSWORD>"}'
```
- `$filter` 지원은 HPE managingusers 문서 예시(`?$filter=UserName eq 'jsmith'`). 미지원 펌웨어면 컬렉션의 각 멤버를 GET 해서 `UserName` 비교.
- 같은 PATCH 로 변경 가능: `UserName`, `Password`, `RoleId`(주의: 기존 권한을 role 권한으로 초기화), `Oem.Hpe.LoginName`, `Oem.Hpe.Privileges.*`, `Enabled`.
- 응답: ExtendedInfo(`Base.x.Success`/`AccountModified` 계열). 상태 코드는 문서 미표기(HPE 문서 예시는 error 구조 안에 Success 메시지를 담는 형식) -> 실장비 확인 unverified.
- If-Match(ETag): HPE 문서 예시에 사용하지 않음(불필요로 보이나 unverified).
- 검증: verified (iLO 5 레퍼런스 + HPE managingusers "Changing password").

## 권한
- 타 계정: `UserConfigPriv`. 자기 자신: 웹 UI 에서 권한 없어도 자기 암호는 변경 가능(iLO 4 UG 문구; iLO 5 UG 동일 취지는 직접 확인 못함) -> Redfish 자기 PATCH 는 unverified.
- 애플리케이션 계정 세션은 UserConfigPriv 가 없어 변경 불가(iLO 7).

## 기본 Administrator 암호 변경 / 분실
- 기본 Administrator 암호는 서버 pull tab 의 랜덤 8자 또는 공통 기본 암호(SKU 별). 보통 Id `1` 이나 `UserName == Administrator` 로 확인.
- **암호 모를 때 in-band (iLO 보안 상태 `Production` 한정)**: OS 에서 root/Administrator 로 `ilorest` CHIF 접속, 자격증명 없이:
  `{"path":"/redfish/v1/AccountService/Accounts/1/","body":{"Password":"<NEW_PASSWORD>"}}` 를 파일로 만들어 ilorest 로 전송(HPE 블로그 "How to change the factory generated iLO Administrator password", 2025-11 갱신). 보안 상태 확인: `ilorest get SecurityState --select HpeSecurityService. --url <BMC_IP> --user <ADMIN_USER> --password <PASSWORD> --logout`. `Production` 보다 높은 보안 상태(High/FIPS/CNSA)는 자격증명 필요. 정확한 ilorest 서브명령은 블로그 그림이라 텍스트 확인 못함(`rawpatch` 계열로 추정, unverified).
- 또는 `hponcfg` + `MOD_USER`(아래 RIBCL).
- 공장 초기화(모든 설정 삭제) 후에는 pull tab 기본 계정 사용. 초기화 전에 `DefaultUserName/DefaultPassword` 지정 가능.

## 대체
| 도구 | 명령 | 상태 |
|---|---|---|
| ilorest | `ilorest iloaccounts changepass <ID 또는 USERNAME> <NEW_PASSWORD>` (예: `changepass 3 newpassword` -> "The account was modified successfully.") | verified |
| RIBCL | `<MOD_USER USER_LOGIN="<USER>"><PASSWORD value="<NEW_PASSWORD>"/></MOD_USER>` (`USER_INFO MODE="write"` 안, `hponcfg -f`/`hpqlocfg`) | verified(구조: Salt `ilo.change_password`) / 원문 가이드 unverified |
| SSH CLI | `set /map1/accounts1/<USER> password=<NEW_PASSWORD>` | iLO 4 실사례만 verified, iLO 5 unverified |
| 웹 UI | Administration > User Administration > Edit > Change password | verified (UG) |
| ipmitool | HPE 문서 근거 없음 | unavailable |

## 정책/제약
- `Oem.Hpe.MinPasswordLength` 0~39 (기본 8, 1.10+), 표준 `MaxPasswordLength`(읽기전용), `Oem.Hpe.EnforcePasswordComplexity`(1.40+, 기본 false; true 면 대/소/숫자/기타 중 3종).
- 로그인 실패: `AccountLockoutThreshold/Duration/CounterResetAfter/CounterResetEnabled`, `Oem.Hpe.AuthFailureDelayTimeSeconds` 등.
- IPMI 계정은 암호 20자 이하.
- 변경 후 기존 세션 무효화: 문서 명시 없음(unverified). 새 암호로 재인증.

## 삭제
`DELETE /redfish/v1/AccountService/Accounts/{id}/` (UserConfigPriv), `ilorest iloaccounts delete <ID|USERNAME>`, RIBCL `DELETE_USER`, 웹 UI Delete.
