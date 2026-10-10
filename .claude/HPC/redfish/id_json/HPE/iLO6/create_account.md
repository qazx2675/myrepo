# HPE iLO 6 - 새 계정 id/pw 등록

적용 모델: DL360 G11, DL380 G11, DL560 G11
플레이스홀더만 사용. `-k` 는 자체서명 인증서용. 호출 계정은 `UserConfigPriv` 필요.

## Redfish - 권장
- `POST https://<BMC_IP>/redfish/v1/AccountService/Accounts/`
- 본문은 iLO 5 와 동일(속성 `ManagerAccount.v1_3_0`, `Oem.Hpe`).

| 속성 | 타입 | 설명 |
|---|---|---|
| UserName | String | 로그인 ID (최대 39자, 고유) |
| Password | String | 암호 (최대 39자, 최소 MinPasswordLength 기본 8) |
| RoleId | String | Administrator / Operator / ReadOnly (생략 시 ReadOnly) |
| Oem.Hpe.LoginName | String | 설명용 이름 (생략 시 UserName) |
| Oem.Hpe.Privileges | Object | LoginPriv, RemoteConsolePriv, VirtualMediaPriv, VirtualPowerAndResetPriv, HostBIOSConfigPriv, HostNICConfigPriv, HostStorageConfigPriv, iLOConfigPriv, UserConfigPriv, SystemRecoveryConfigPriv |
| Oem.Hpe.ServiceAccount | Boolean | 생성 시에만 지정 |
| Oem.Hpe.SkipEscCharsCheck | Boolean | 이스케이프 문자 검사 생략 여부(iLO 6 1.40+) |

```json
{
  "UserName": "<NEW_USER>",
  "Password": "<NEW_PASSWORD>",
  "RoleId": "Administrator",
  "Oem": { "Hpe": { "LoginName": "<NEW_USER>" } }
}
```
```
curl -sk -u <ADMIN_USER>:<ADMIN_PASSWORD> -H "Content-Type: application/json" \
  -X POST https://<BMC_IP>/redfish/v1/AccountService/Accounts/ \
  -d '{"UserName":"<NEW_USER>","Password":"<NEW_PASSWORD>","RoleId":"Administrator"}'
```
- 권한 직접 지정: `{"UserName":"...","Password":"...","Oem":{"Hpe":{"Privileges":{"LoginPriv":true,"RemoteConsolePriv":true,"VirtualPowerAndResetPriv":true}}}}`
- 성공: 200/201(샘플 코드 기준) / 400 은 `error.@Message.ExtendedInfo` 확인. 정확한 코드는 실장비 미확인.
- 검증: verified (iLO 6 레퍼런스 컬렉션 Allow POST + HPE managingusers + python-ilorest-library).

## ilorest
```
ilorest login <BMC_IP> -u <ADMIN_USER> -p <ADMIN_PASSWORD>
ilorest iloaccounts add <NEW_USER> "<LOGIN_NAME>" <NEW_PASSWORD> --role Administrator
ilorest logout
```
- 인자 순서 `USERNAME LOGINNAME PASSWORD` (위치 기반). `--addprivs` 번호 1~10 (iLO 5 와 동일; 7 Host NIC, 8 Host BIOS, 9 Host Storage, 10 System Recovery). `--role` Administrator/Operator/ReadOnly. Linux 에서 암호의 `$` 는 `\` 이스케이프.
- 검증: verified (HPE ilorest 가이드).

## RIBCL (XML, sustenance stage)
iLO 5 와 같은 구조(`ADD_USER` + `ADMIN_PRIV`/`REMOTE_CONS_PRIV`/`RESET_SERVER_PRIV`/`VIRTUAL_MEDIA_PRIV`/`CONFIG_ILO_PRIV`):
```xml
<RIBCL VERSION="2.0">
  <LOGIN USER_LOGIN="<ADMIN_USER>" PASSWORD="<ADMIN_PASSWORD>">
    <USER_INFO MODE="write">
      <ADD_USER USER_NAME="<NEW_USER>" USER_LOGIN="<NEW_USER>" PASSWORD="<NEW_PASSWORD>">
        <ADMIN_PRIV value="Y"/> <REMOTE_CONS_PRIV value="Y"/> <RESET_SERVER_PRIV value="Y"/>
        <VIRTUAL_MEDIA_PRIV value="Y"/> <CONFIG_ILO_PRIV value="Y"/>
      </ADD_USER>
    </USER_INFO>
  </LOGIN>
</RIBCL>
```
- `hponcfg -f add_user.xml`(HPONCFG 6.0.0+ + iLO 6 1.10+: 권한 부족 시 오류 메시지) 또는 `hpqlocfg`/`locfg.pl`.
- DL 시리즈는 지원(RL3xx 만 미지원). iLO 6 의 RIBCL 신규 권한 태그(Host BIOS 등)는 원문 못 읽어 unverified. 구조는 Salt 소스 기반 verified.
- HPE 는 RIBCL 사용을 권장하지 않음(ilorest 권장).

## SSH CLI / 웹 UI / 로컬
- SSH CLI: iLO 4 와 같은 `create /map1/accounts1 username=... password=...` 계열이 iLO 6 에도 있는지는 HPE iLO 6 스크립팅 가이드 원문을 못 읽어 **unverified**.
- 웹 UI: Administration > User Administration > New (Login Name, User Name, 암호, Role 또는 Custom 권한, Service Account).
- 부팅 중 UEFI System Utilities > iLO 6 Configuration Utility > User Management 에서 추가(UG "Adding user accounts (iLO 6 Configuration Utility)").
- IPMI: 로컬 계정과 통합 관리(16자/20자 제한). `ipmitool` 로 계정 생성한다는 HPE 문서 근거 없음 -> **unavailable**.

## 제약
- 로컬 계정 최대 12개, UserName/LoginName 각 39자, 암호 최대 39자.
- 복잡도 강제는 기본 off.
