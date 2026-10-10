# Dell iDRAC9 - 새 계정 id/pw 등록

적용 모델: R640, C6420, R840, DSS8440, R740, R750, R750xa, R750xs, R760XA, XE9680, R6615, R660, R860, C6620
플레이스홀더만 사용. `-k` 는 자체서명 인증서용. 신규 계정은 슬롯 **3~16** 중 빈 슬롯(UserName 이 "") 을 골라 PATCH.

## 1) 빈 슬롯 찾기
```
curl -sk -u <ADMIN_USER>:<ADMIN_PASSWORD> \
  "https://<BMC_IP>/redfish/v1/Managers/iDRAC.Embedded.1/Accounts?\$expand=*(\$levels=1)"
```
각 멤버의 `Id`, `UserName`, `Enabled`, `RoleId` 확인. 같은 UserName 이 이미 있으면 생성 대신 change_password.md 사용. 슬롯 1(anonymous), 2(root)는 건드리지 않는다.

## 2) Redfish: 빈 슬롯 PATCH (verified: Dell 스크립트 + community.general)
```
curl -sk -u <ADMIN_USER>:<ADMIN_PASSWORD> -H "Content-Type: application/json" \
  -X PATCH https://<BMC_IP>/redfish/v1/Managers/iDRAC.Embedded.1/Accounts/<SLOT> \
  -d '{"UserName":"<NEW_USER>","Password":"<NEW_PASSWORD>","RoleId":"Administrator","Enabled":true}'
```
- RoleId: Administrator | Operator | ReadOnly | None. 성공 시 HTTP 200 (Dell 스크립트 기준).
- 권한: ConfigureUsers 가 있는 계정(Administrator).
- POST /Accounts 는 iDRAC9 용으로 확인되지 않음(unverified). 405 면 위 PATCH 사용.

## 3) Dell OEM 속성으로 IPMI/SOL 등 한 번에 설정 (verified: dellemc.openmanage idrac_user, Dell 스크립트의 DellAttributes URI)
```
curl -sk -u <ADMIN_USER>:<ADMIN_PASSWORD> -H "Content-Type: application/json" \
  -X PATCH https://<BMC_IP>/redfish/v1/Managers/iDRAC.Embedded.1/Oem/Dell/DellAttributes/iDRAC.Embedded.1 \
  -d '{"Attributes":{"Users.<SLOT>.UserName":"<NEW_USER>","Users.<SLOT>.Password":"<NEW_PASSWORD>","Users.<SLOT>.Privilege":511,"Users.<SLOT>.Enable":"Enabled","Users.<SLOT>.IpmiLanPrivilege":"Administrator","Users.<SLOT>.IpmiSerialPrivilege":"Administrator","Users.<SLOT>.SolEnable":"Enabled"}}'
```
- Privilege: Administrator 511, Operator 499, ReadOnly 1, None 0. Enable 값은 문자열 "Enabled"/"Disabled".
- 모듈은 `/redfish/v1/Managers/iDRAC.Embedded.1/Attributes/` 경로를 쓴다(구경로). 둘 다 코드에 존재하나 최신 FW 는 DellAttributes 쪽 권장(Dell 스크립트 사용).

## 제약
- 사용자명 1~16자, 암호 FW 3.xx 20자 / 4~6.xx 40자 / 7.xx 127자 (Dell KB 000177787). 대·소문자·숫자·특수문자 포함 권장.
- 빈 슬롯이 없으면 "Maximum number of users reached" -> 불필요한 계정 삭제 후 재시도.
- 7.30.10.50 이상(14G: 7.00.00.184 이상)은 Basic 인증 `Unadvertised` - 첫 요청부터 자격증명 전송.

## 대체
- RACADM (verified: Dell KB 로 Password 구문, PowerShell 모듈로 UserName/Privilege 구문):
  ```
  racadm set iDRAC.Users.<SLOT>.UserName <NEW_USER>
  racadm set iDRAC.Users.<SLOT>.Password <NEW_PASSWORD>
  racadm set iDRAC.Users.<SLOT>.Privilege 0x1ff
  racadm set iDRAC.Users.<SLOT>.Enable 1
  racadm set iDRAC.Users.<SLOT>.IpmiLanPrivilege 4
  ```
  Enable 값 `1`/`Enabled` 중 어느 쪽이 맞는지는 FW 별 확인 필요(unverified; iDRAC8 커뮤니티 글은 `enable 0` 이 성공). IpmiLanPrivilege 숫자 4=Administrator 는 Dell 커뮤니티 config 파일 예시(iDRAC8)에서 확인, 나머지 숫자(3 Operator, 2 User, 15 No Access)는 IPMI 표준값으로 unverified.
- SCP(Server Configuration Profile): Export `POST /redfish/v1/Managers/iDRAC.Embedded.1/Actions/Oem/EID_674_Manager.ExportSystemConfiguration` `{"ExportFormat":"JSON","ShareParameters":{"Target":["IDRAC"]}}` -> 속성 `Users.<SLOT>#UserName` 등을 수정 -> `.../EID_674_Manager.ImportSystemConfiguration` `{"ImportBuffer":"<SCP 내용>","ShareParameters":{"Target":["IDRAC"]}}` (Dell Export/Import 스크립트 기준, 비동기 Job). 사용자 암호 변경이 포함되면 이후 Job 조회에 새 암호 사용.
- IPMI: ipmitool `user set name <ID> <NEW_USER>` / `user set password <ID> <NEW_PASSWORD>` / `user enable <ID>` / `channel setaccess 1 <ID> ...`. iDRAC 의 IPMI User ID 는 Users.N 슬롯과 대응한다고 알려져 있으나 이번에 문서로 확인 못함 -> unverified. IPMI 암호는 20자 제한(표준) 가능 -> 장비 확인.
- 웹 UI: iDRAC Settings > User Authentication(Users) > 빈 사용자 ID 선택 > Configure User.
