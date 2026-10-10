# HPE iLO 4 - 기존 id 패스워드 변경

적용 모델: DL360 G9, XL170r G9, XL250 G9, XL270d G9 (Redfish/REST), DL560 G8 (RIBCL/CLI 권장)

## Redfish (2.30+) / 구 REST
- `PATCH https://<BMC_IP>/redfish/v1/AccountService/Accounts/{id}/` (구: `/rest/v1/AccountService/Accounts/{id}`)
- `Password` 는 PATCH 가능, GET 에서는 항상 null (iLO 4 REST 레퍼런스 ManagerAccount/Password: 최대 39자, 최소는 `MinPasswordLength`).
```json
{ "Password": "<NEW_PASSWORD>" }
```
```
# 1) 대상 슬롯 찾기
curl -sk -u <ADMIN_USER>:<CURRENT_PASSWORD> https://<BMC_IP>/redfish/v1/AccountService/Accounts/
curl -sk -u <ADMIN_USER>:<CURRENT_PASSWORD> https://<BMC_IP>/redfish/v1/AccountService/Accounts/<ID>/   # UserName 확인
# 2) 변경
curl -sk -u <ADMIN_USER>:<CURRENT_PASSWORD> -H "Content-Type: application/json" \
  -X PATCH https://<BMC_IP>/redfish/v1/AccountService/Accounts/<ID>/ \
  -d '{"Password":"<NEW_PASSWORD>"}'
```
- 같은 PATCH 로 변경 가능: `UserName`, `Oem.Hp.LoginName`, `Oem.Hp.Privileges.*`. (`Password` 만 보내는 것이 안전. Id 는 `UserName` 으로 찾는다 - python-ilorest-library `modify_user_account.py` 방식.)
- If-Match(ETag) 필요 여부: HPE 문서 예시에 사용 안 함 -> 불필요로 보이나 실장비 확인(unverified).
- 응답: ExtendedInfo(성공 시 `Base.x.AccountModified` 계열 / 오류 시 400). 정확한 상태 코드 unverified.
- 검증: verified (iLO 4 REST 레퍼런스 + HPE managingusers 문서 "Changing password").

## 권한
- 타 계정 암호: `UserConfigPriv`(Administer User Accounts).
- 자기 자신: UG "If you do not have this privilege, you can view your own settings and change your own password" (웹 UI 기준 verified). Redfish PATCH 로 자기 암호 변경 가능 여부는 unverified.

## 기본 Administrator 암호 변경
- 기본 계정 `Administrator`, 암호는 서버 앞면 pull tab 의 랜덤 8자 (UG). 보통 Id `1` 이나 **반드시 `UserName == Administrator` 로 슬롯 확인**(HPE 블로그 권고).
- **암호를 모를 때(in-band)**: 호스트 OS 에서 `hponcfg`(iLO 4 Channel Interface Driver 필요, 로컬은 iLO 로그인 불필요):
```xml
<RIBCL VERSION="2.0">
  <LOGIN USER_LOGIN="x" PASSWORD="x">
    <USER_INFO MODE="write">
      <MOD_USER USER_LOGIN="Administrator">
        <PASSWORD value="<NEW_PASSWORD>"/>
      </MOD_USER>
    </USER_INFO>
  </LOGIN>
</RIBCL>
```
`hponcfg -f change_pw.xml`. 단, iLO 설정 "Require Host Authentication"(iLO 5+ 에서 주로 사용)·서버 보안 설정이 켜져 있으면 로컬 인증이 필요할 수 있음. iLO 4 에서는 HPE 블로그(François Donzé)가 ilorest in-band(CHIF) 로 `{"path":"/redfish/v1/AccountService/Accounts/1/","body":{"Password":"..."}}` 를 담은 파일을 ilorest 로 in-band(CHIF) 전송하는 방법도 제시하나(실행 명령은 그림이라 텍스트 확인 못함; ilorest `rawpatch <파일>` 로 추정, unverified) 해당 문구의 "Production 보안 상태" 조건은 iLO 5/6 기준 설명 (iLO 4 에는 보안 상태 개념 없음) -> iLO 4 는 hponcfg 방식 권장.

## 대체
| 도구 | 명령 | 상태 |
|---|---|---|
| ilorest | `ilorest iloaccounts changepass <ID 또는 USERNAME> <NEW_PASSWORD>` (로그인 후) | verified (HPE ilorest 가이드: "changepass ... Id or Username and the new password") |
| RIBCL | 위 `MOD_USER` (원격은 `hpqlocfg`/`locfg.pl -s <BMC_IP> -f change_pw.xml -u <ADMIN_USER> -p <ADMIN_PASSWORD>`) | verified(구조: Salt `ilo.change_password`) / 원문 가이드 unverified |
| SSH CLI | `set /map1/accounts1/<USER> password=<NEW_PASSWORD>` | verified (GoLinuxHub iLO 4 2.55 실사례 `COMMAND COMPLETED`) |
| 웹 UI | Administration > User Administration > 사용자 선택 > Edit > Change password | verified (UG) |
| ipmitool | iLO 계정과 IPMI 사용자 통합이라 `ipmitool user set password` 가능성은 있으나 HPE 문서 근거 없음, IPMI over LAN 기본 Disabled | unavailable |

## 정책/제약
- 최소 길이 `Oem.Hp.MinPasswordLength` 0~39 (기본 8) - `PATCH /redfish/v1/AccountService {"Oem":{"Hp":{"MinPasswordLength":8}}}`. 최대 39자. 암호 변경 시 이 값 미만이면 거부.
- 복잡도 강제 속성 없음(권고만: 대/소/숫자/기타 중 3종).
- IPMI 사용 시 암호 20자 이하(UG).
- 로그인 실패 지연: `Oem.Hp.AuthFailureDelayTimeSeconds`(2/5/10/30), `AuthFailuresBeforeDelay`(0/1/3/5), `AuthFailureLoggingThreshold`.
- 변경 후 기존 세션 무효화 여부: HPE 문서 명시 없음(unverified). 새 암호로 재인증 권장.

## 삭제
- `DELETE /redfish/v1/AccountService/Accounts/{id}/` (REST 레퍼런스), `ilorest iloaccounts delete <ID|USERNAME>`, RIBCL `<DELETE_USER USER_LOGIN="<USER>"/>`, CLI `delete /map1/accounts1/<USER>` (verb 목록으로 확인, 예시 미확인), 웹 UI Delete.
