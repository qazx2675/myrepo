# HPE iLO 5 - 새 계정 id/pw 등록

적용 모델: DL360 G10, DL560 G10, DL580 G10, XL270d G10, DL360 G10 Plus
플레이스홀더만 사용. `-k` 는 자체서명 인증서용. 호출 계정은 `UserConfigPriv`(Administer User Accounts) 필요.

## Redfish - 권장
- `POST https://<BMC_IP>/redfish/v1/AccountService/Accounts/`

| 속성 | 타입 | 설명 |
|---|---|---|
| UserName | String | 로그인 ID (최대 39자). 계정별 고유 |
| Password | String | 암호 (최대 39자, 최소 MinPasswordLength) |
| RoleId | String | Administrator / Operator / ReadOnly (생략 시 ReadOnly) |
| Oem.Hpe.LoginName | String | 설명용 이름 (생략 시 UserName) |
| Oem.Hpe.Privileges | Object | LoginPriv, RemoteConsolePriv, VirtualMediaPriv, VirtualPowerAndResetPriv, HostBIOSConfigPriv, HostNICConfigPriv, HostStorageConfigPriv, iLOConfigPriv, UserConfigPriv, SystemRecoveryConfigPriv |
| Oem.Hpe.ServiceAccount | Boolean | 서비스 계정 지정(생성 시에만 가능) |

최소 예:
```json
{ "UserName": "<NEW_USER>", "Password": "<NEW_PASSWORD>" }
```
(ReadOnly, Login 권한만)

role 로 생성:
```json
{ "UserName": "<NEW_USER>", "Password": "<NEW_PASSWORD>", "RoleId": "Administrator" }
```
권한 직접 지정:
```json
{
  "UserName": "<NEW_USER>",
  "Password": "<NEW_PASSWORD>",
  "Oem": { "Hpe": { "LoginName": "<NEW_USER>",
    "Privileges": { "LoginPriv": true, "RemoteConsolePriv": true, "VirtualMediaPriv": true, "VirtualPowerAndResetPriv": true } } }
}
```
```
curl -sk -u <ADMIN_USER>:<ADMIN_PASSWORD> -H "Content-Type: application/json" \
  -X POST https://<BMC_IP>/redfish/v1/AccountService/Accounts/ \
  -d '{"UserName":"<NEW_USER>","Password":"<NEW_PASSWORD>","RoleId":"Administrator"}'
```
- 응답: 성공 시 200/201 (python-ilorest-library 샘플이 200/201 을 성공 처리), 400 은 `error.@Message.ExtendedInfo` 에 사유(중복 이름, 암호 정책 위반 등).
- 확인: `GET /redfish/v1/AccountService/Accounts/{id}/` -> `Links.Role`, `Oem.Hpe.Privileges`.
- 검증: verified (HPE managingusers 문서 + iLO 5 레퍼런스 + python-ilorest-library). 정확한 성공 코드는 실장비 미확인.

## ilorest
```
ilorest login <BMC_IP> -u <ADMIN_USER> -p <ADMIN_PASSWORD>
ilorest iloaccounts add <NEW_USER> "<LOGIN_NAME>" <NEW_PASSWORD> --role Administrator
ilorest iloaccounts add <NEW_USER2> "<LOGIN_NAME2>" <NEW_PASSWORD> --addprivs 1,2,5,6,8
ilorest logout
```
- 인자 순서: `USERNAME LOGINNAME PASSWORD` (위치 기반). 권한 번호: 1 Login, 2 Remote Console, 3 User Config, 4 iLO Config, 5 Virtual Media, 6 Virtual Power and Reset, 7 Host NIC, 8 Host BIOS, 9 Host Storage, 10 System Recovery. 기본은 ReadOnly(Login).
- Linux 쉘에서 암호 내 `$` 등 특수문자는 `\` 이스케이프.
- 검증: verified (HPE ilorest 가이드).

## RIBCL (XML, 레거시 - sustenance)
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
- 실행: 호스트 `hponcfg -f add_user.xml`(HPONCFG 5.2.0+ 와 iLO 5 1.20+ 에서는 권한 부족 시 오류 메시지), 원격 `hpqlocfg.exe -s <BMC_IP> -f add_user.xml -u <ADMIN_USER> -p <ADMIN_PASSWORD>` / `locfg.pl`.
- 위 5개 태그는 Salt `ilo.create_user` 소스로 확인. iLO 5 신규 권한(Host BIOS/NIC/Storage, Recovery) 태그명은 원문 가이드를 못 읽어 **unverified**.
- HPE: RIBCL 은 sustenance stage(중요 버그/보안 수정만). Redfish/ilorest 권장.

## SSH CLI (SMASH CLP)
- `ssh <ADMIN_USER>@<BMC_IP>` 후 `create /map1/accounts1 username=<NEW_USER> password=<NEW_PASSWORD> name=<NEW_USER> group=0` (iLO 4 2.55 실사례/Qualys 문서 기준). iLO 5 에서의 권한 그룹 구문과 동작은 HPE iLO 5 스크립팅 가이드 원문을 못 읽어 **unverified**.

## 웹 UI / 로컬
- 웹 UI: Administration > User Administration > New (Login Name, User Name, 암호, 권한 체크, 서비스 계정 체크박스).
- 서버 부팅 F9 > System Configuration > iLO 5 Configuration Utility > User Management > Add User (Recovery Set 권한은 이 경로로 부여 불가).
- IPMI: iLO 는 IPMI 사용자와 로컬 계정이 통합 관리(로그인 16자/암호 20자 이하). HPE 문서에 `ipmitool user set` 으로 iLO 계정을 만든다는 근거는 없어 **unavailable**. IPMI over LAN 은 기본 Disabled.

## 제약
- 로컬 계정 최대 12개. 같은 UserName 불가.
- 암호: 최대 39자, 최소 MinPasswordLength(기본 8), 복잡도 강제 시 대/소/숫자/기타 중 3종.
- 애플리케이션 토큰/Application account 세션은 `UserConfigPriv` 없음 - 계정 생성 불가(iLO 7 해당).
