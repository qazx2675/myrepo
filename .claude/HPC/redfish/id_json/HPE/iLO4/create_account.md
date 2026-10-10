# HPE iLO 4 - 새 계정 id/pw 등록

적용 모델: DL360 G9, XL170r G9, XL250 G9, XL270d G9 (Redfish/REST), DL560 G8 (RIBCL/CLI 권장, Redfish 는 unverified)
플레이스홀더만 사용. `-k` 는 자체서명 인증서용. 호출 계정은 `UserConfigPriv`(Administer User Accounts) 필요.

## Redfish (iLO 4 2.30+) - 권장
- `POST https://<BMC_IP>/redfish/v1/AccountService/Accounts/`
- 구 REST(2.00~2.2x): `POST https://<BMC_IP>/rest/v1/AccountService/Accounts` (본문 동일, 컬렉션 URI 는 `/rest/v1/AccountService` 의 `links.Accounts.href`)

| 속성 | 타입 | 설명 |
|---|---|---|
| UserName | String | 로그인 ID (최대 39자, 인쇄 가능 문자). 웹 UI 의 "Login Name" |
| Password | String | 암호 (최대 39자, 최소는 AccountService `MinPasswordLength`, 기본 8) |
| Oem.Hp.LoginName | String | 설명용 이름(웹 UI "User Name"). 생략 시 UserName 으로 채워짐 |
| Oem.Hp.Privileges | Object | LoginPriv, RemoteConsolePriv, VirtualMediaPriv, VirtualPowerAndResetPriv, iLOConfigPriv, UserConfigPriv (불리언). LoginPriv 는 자동 부여 |

**iLO 4 는 `RoleId` 로 생성하는 방식이 문서상 iLO 5 부터이므로 privileges 로 생성.** `Oem.Hpe` 가 아니라 `Oem.Hp`.

```json
{
  "UserName": "<NEW_USER>",
  "Password": "<NEW_PASSWORD>",
  "Oem": {
    "Hp": {
      "LoginName": "<NEW_USER>",
      "Privileges": {
        "LoginPriv": true,
        "RemoteConsolePriv": true,
        "VirtualMediaPriv": true,
        "VirtualPowerAndResetPriv": true,
        "iLOConfigPriv": true,
        "UserConfigPriv": false
      }
    }
  }
}
```
```
curl -sk -u <ADMIN_USER>:<ADMIN_PASSWORD> -H "Content-Type: application/json" \
  -X POST https://<BMC_IP>/redfish/v1/AccountService/Accounts/ \
  -d '{"UserName":"<NEW_USER>","Password":"<NEW_PASSWORD>","Oem":{"Hp":{"LoginName":"<NEW_USER>","Privileges":{"LoginPriv":true,"RemoteConsolePriv":true,"VirtualMediaPriv":true,"VirtualPowerAndResetPriv":true,"iLOConfigPriv":true}}}}'
```
- 관리자급: `iLOConfigPriv` + `UserConfigPriv` 포함 모든 권한 true.
- 응답: 새 계정 리소스(`Password:null`) 또는 ExtendedInfo. 성공 코드: python-ilorest-library 샘플은 200/201 을 성공, 400 이면 `error.@Message.ExtendedInfo` 확인.
- 확인: `GET /redfish/v1/AccountService/Accounts/` 후 각 멤버의 `UserName` 확인.
- 검증: verified (iLO 4 REST 레퍼런스 ManagerAccount 예시 + python-ilorest-library `add_ilo_user_account_gen9`). 정확한 성공 코드는 실장비 미확인.

## ilorest (iLO 4 지원)
```
ilorest login <BMC_IP> -u <ADMIN_USER> -p <ADMIN_PASSWORD>
ilorest iloaccounts add <NEW_USER> "<LOGIN_NAME>" <NEW_PASSWORD> --addprivs 1,2,3,4,5,6
ilorest logout
```
- 인자 순서: `add USERNAME LOGINNAME PASSWORD` (HPE 문서: USERNAME=로그인용, LOGINNAME=설명). 위치로 해석되므로 순서 주의. Linux 에서 암호의 `$` 등 특수문자는 `\` 이스케이프.
- 권한 번호: 1 Login, 2 Remote Console, 3 User Config, 4 iLO Config, 5 Virtual Media, 6 Virtual Power and Reset. (7~10 은 iLO 5 전용). 기본값: iLO 4 는 **권한 없음**.
- `--role` 은 iLO 5 이상(문서: "ReadOnly 기본 role in iLO 5 and no privileges in iLO 4").
- 검증: verified (HPE ilorest 사용자 가이드 iloaccounts 절).

## RIBCL (XML) - DL560 G8 포함 전 모델
`add_user.xml`
```xml
<RIBCL VERSION="2.0">
  <LOGIN USER_LOGIN="<ADMIN_USER>" PASSWORD="<ADMIN_PASSWORD>">
    <USER_INFO MODE="write">
      <ADD_USER USER_NAME="<NEW_USER>" USER_LOGIN="<NEW_USER>" PASSWORD="<NEW_PASSWORD>">
        <ADMIN_PRIV value="Y"/>
        <REMOTE_CONS_PRIV value="Y"/>
        <RESET_SERVER_PRIV value="Y"/>
        <VIRTUAL_MEDIA_PRIV value="Y"/>
        <CONFIG_ILO_PRIV value="Y"/>
      </ADD_USER>
    </USER_INFO>
  </LOGIN>
</RIBCL>
```
- 호스트 OS 에서(인증 없이 로컬 채널): `hponcfg -f add_user.xml` (LOGIN 값은 더미 가능). 원격: `hpqlocfg.exe -s <BMC_IP> -f add_user.xml -u <ADMIN_USER> -p <ADMIN_PASSWORD>` 또는 `locfg.pl -s <BMC_IP> -f add_user.xml -u <ADMIN_USER> -p <ADMIN_PASSWORD>`.
- 속성/태그 확인: SaltStack `ilo.create_user`(ADMIN_PRIV=계정관리, REMOTE_CONS_PRIV, RESET_SERVER_PRIV, VIRTUAL_MEDIA_PRIV, CONFIG_ILO_PRIV, 권한 미지정 시 읽기 전용) + HPE 커뮤니티 예시. 권한값 표기 `Y`/`Yes` 둘 다 예시에 있음(공식 가이드 직접 확인 못함 - unverified).
- 검증: verified(구조) / unverified(공식 가이드 원문 미확인).

## SSH CLI (SMASH CLP)
```
ssh <ADMIN_USER>@<BMC_IP>
</>hpiLO-> create /map1/accounts1 username=<NEW_USER> password=<NEW_PASSWORD> name=<NEW_USER> group=0
</>hpiLO-> show /map1/accounts1
```
- `create /map1/accounts1 username=... password=...` 은 iLO 4 2.55 실사례(GoLinuxHub)에서 `User added successfully.`; `name=`, `group=0`(읽기전용) 은 Qualys 문서 실사례. 권한 그룹 문법(`group=admin,config,oemhp_rc,oemhp_power,oemhp_vm`)은 공식 가이드 미확인 -> unverified. 계정 속성 verbs: `cd version exit show create set oemhp_loadSSHKey oemhp_deleteSSHKey`.
- 검증: verified(기본 create) / unverified(권한 그룹).

## 웹 UI / 로컬
- 웹 UI: Administration > User Administration > New (Login Name, User Name, Password, 권한 체크).
- 서버 부팅 중 F9 System Utilities(iLO 4 Configuration Utility / RBSU) 의 User Management 에서도 추가 가능 (UG).
- IPMI: iLO IPMI 사용자는 로컬 계정과 통합(UG: IPMI 사용자는 로그인명 16자, 암호 20자 이하, 권한은 iLO 권한에서 User/Operator/Administrator 로 매핑). `ipmitool user set ...` 로 iLO 계정을 새로 만드는 방식은 HPE 문서 근거가 없어 **unavailable**. IPMI over LAN 은 기본 Disabled(2.60+).

## 제약
- 로컬 계정 최대 12개. 같은 UserName 중복 불가. 사용자명/LoginName 39자, 암호 39자.
- 필요 권한: UserConfigPriv. iLOConfigPriv 는 로컬 계정 관리를 포함하지 않음.
