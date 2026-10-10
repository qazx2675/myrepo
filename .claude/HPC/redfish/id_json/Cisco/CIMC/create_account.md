# Cisco CIMC (standalone C-Series) - 새 계정 등록

참고용(사용자 보유 모델 없음). 플레이스홀더만 사용. `-k` 는 자체서명 인증서용.

## Redfish (IMC 3.0+)
권한: RoleId `admin` (ConfigureUsers).
### 4.2 형식 (컬렉션 POST) - Cisco 4.2 가이드 "Setting up ID 11 Cisco IMC User ..."
```
curl -sk -u <ADMIN_USER>:<ADMIN_PASSWORD> -H "Content-Type: application/json" \
  -X POST https://<CIMC_IP>/redfish/v1/AccountService/Accounts \
  -d '{"Id":"11","UserName":"<NEW_USER>","Password":"<NEW_PASSWORD>","RoleId":"admin","Enabled":true}'
```
- 성공 시 본문 없음, 실패 시 오류 메시지. `RoleId`: admin / user / read-only.
### 3.0 형식 (슬롯 POST)
```
curl -sk -u <ADMIN_USER>:<ADMIN_PASSWORD> -X POST https://<CIMC_IP>/redfish/v1/AccountService/Accounts/5 \
  -d '{"UserName":"<NEW_USER>","Password":"<NEW_PASSWORD>","RoleId":"admin","Enabled":"true"}'
```
- 3.0 가이드는 `Enabled` 를 문자열로 보냄. 빈 슬롯 번호는 `GET .../Accounts` 후 비어 있는(UserName 빈) 슬롯을 사용.
- 정책: `PATCH /redfish/v1/AccountService` `{"Oem":{"Cisco":{"StrongPasswordPolicyEnabled":true,"PasswordHistory":5,"PasswordExpiry":{"Enabled":true,"ExpiryDuration":30,"NotificationPeriod":15,"GracePeriod":5}}}}` (4.2 가이드, C220/C240/C460 M4·S3X60 제외).

## XML API (`POST https://<CIMC_IP>/nuova`)
imcsdk `local_user_create`: 비어 있는(name 없고 inactive) `aaaUser` 슬롯 id 를 찾아 name/pwd/priv/accountStatus=active 를 설정.
```
curl -sk -H "Content-Type: text/xml" https://<CIMC_IP>/nuova -d '<aaaLogin inName="<ADMIN_USER>" inPassword="<ADMIN_PASSWORD>"/>'
curl -sk -H "Content-Type: text/xml" https://<CIMC_IP>/nuova -d '
<configConfMo cookie="<COOKIE>" dn="sys/user-ext/user-<ID>" inHierarchical="false">
  <inConfig><aaaUser id="<ID>" name="<NEW_USER>" pwd="<NEW_PASSWORD>" priv="read-only" accountStatus="active"/></inConfig>
</configConfMo>'
```
- priv: admin / read-only / user. 슬롯 `<ID>` 는 `configResolveClass classId="aaaUser"` 로 빈 슬롯 확인. (요청 XML 속성은 imcsdk 소스 기준, 실장비 미검증.)

## CLI
```
Server# scope user <N>
Server /user # set enabled yes
Server /user # set name <NEW_USER>
Server /user # set password        (2회 입력)
Server /user # set role admin      (readonly | user | admin)
Server /user # commit
```
(Cisco CIMC CLI 1.2(1) 문서, 상위 버전 동일 문법 unverified.)

## 제약
- 암호 8~20자(Redfish 응답 Min/Max), 강한 정책 ON 시 복잡도 적용. 이름 규칙은 README.
- 삭제: Redfish `DELETE .../Accounts/<id>` (3.0 가이드), XML 은 `accountStatus=inactive, priv=read-only, adminAction=clear` (imcsdk `local_user_delete`).
