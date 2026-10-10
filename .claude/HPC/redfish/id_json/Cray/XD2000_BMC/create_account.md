# XD2000 BMC (XD220V) - 새 id/pw 등록

상태: Redfish POST = verified (AMI 계열 레퍼런스 + Cray CSM), XD220v 실기 응답 미확인. 값은 모두 플레이스홀더.

## 0. 사전 확인 (GET)
```bash
# 변수: <BMC_IP>, <ADMIN_USER>, <ADMIN_PASSWORD> (서버 라벨의 초기 계정 또는 기존 관리자)
curl -k -u <ADMIN_USER>:<ADMIN_PASSWORD> https://<BMC_IP>/redfish/v1/AccountService
curl -k -u <ADMIN_USER>:<ADMIN_PASSWORD> https://<BMC_IP>/redfish/v1/AccountService/Accounts
curl -k -u <ADMIN_USER>:<ADMIN_PASSWORD> https://<BMC_IP>/redfish/v1/AccountService/Roles
```
- `AccountService.ServiceEnabled` 가 `true` 여야 생성/변경/삭제가 된다. false 면 `PATCH /redfish/v1/AccountService {"ServiceEnabled": true}` (If-Match 헤더 필요, change_password.md 의 헤더 설명 참조).
- `MinPasswordLength`, `MaxPasswordLength`, `AccountLockoutThreshold` 를 읽어 비밀번호 정책을 확인한다 (AMI 기본 잠금 임계값 5).
- 기존 계정 수와 빈 ID 를 확인한다. AMI 레퍼런스 기준 계정 최대 14개.

## 1. Redfish: 계정 생성
```
POST https://<BMC_IP>/redfish/v1/AccountService/Accounts
Content-Type: application/json
```
```json
{
  "UserName": "<NEW_USER>",
  "Password": "<NEW_PASSWORD>",
  "RoleId": "Administrator",
  "Enabled": true,
  "Locked": false,
  "PasswordChangeRequired": false
}
```
```bash
curl -k -u <ADMIN_USER>:<ADMIN_PASSWORD> -X POST \
  -H 'Content-Type: application/json' \
  -d '{"UserName":"<NEW_USER>","Password":"<NEW_PASSWORD>","RoleId":"Administrator","Enabled":true,"PasswordChangeRequired":false}' \
  https://<BMC_IP>/redfish/v1/AccountService/Accounts
```
- 필수 프로퍼티: `UserName`, `Password`, `RoleId` (AMI 레퍼런스). `Name`, `Description`, `Enabled`, `Locked` 는 선택.
- `PasswordChangeRequired`: AMI 레퍼런스에서 POST 본문에 없으면 기본 `true` 로 처리되어 첫 사용 전에 비밀번호를 바꿔야 한다. 바로 쓰려면 `false` 를 명시한다. 이 속성은 PATCH 로 바꿀 수 없다.
- POST 에는 조건부 헤더가 필요 없다 (조건부 헤더 요구는 PUT/PATCH).
- 성공: HTTP 201 + 생성된 계정 JSON (`Id` 확인, 이후 URL 에 사용). 비밀번호는 `null` 로 표시된다.

### 롤(권한)
| RoleId | 용도 | 비고 |
|---|---|---|
| Administrator | 전체 (계정 생성/삭제 포함) | 계정을 만들려면 ConfigureUsers 권한 필요 |
| Operator | 전원/구성 요소 제어 | AMI 레퍼런스 POST 예시가 Operator |
| ReadOnly | 조회 | AMI 레퍼런스 PATCH 예시가 ReadOnly |
| 사용자 정의 롤 | `POST /redfish/v1/AccountService/Roles` | AssignedPrivileges: Login, ConfigureManager, ConfigureUsers, ConfigureSelf, ConfigureComponents |

사전 정의 롤의 정확한 권한 목록은 `GET /redfish/v1/AccountService/Roles/<RoleId>` 로 확인한다 (XD220v 문서 미확인).

### 제약
- 사용자명: 1~16자 영숫자, 첫 글자는 영문, 특수문자는 `-`, `_`, `@` 만 허용 (IPMI 규칙과 동기화, AMI 레퍼런스).
- 비밀번호: 8~20자 (IPMI 규칙과 동기화). 실제 한도는 AccountService 의 Min/MaxPasswordLength 로 확인.
- 계정 최대 14개. 사전 정의 계정(HostAutoFW, HostAutoOS 등)은 수정/삭제 불가 (Lenovo 포장본 기준, XD220v 에서 존재 여부 미확인).
- 계정 ID(슬롯)는 OEM 마다 다르다 (Cray CSM 예시 1~4, DGX 2=admin). 임의로 번호를 가정하지 말고 GET 으로 확인.
- Redfish 계정과 IPMI 계정은 동기화될 수 있다 (AMI "Redfish accounts and IPMI accounts synchronization").

### 응답 코드 / 흔한 오류
| 코드 | 의미 |
|---|---|
| 201 | 생성 성공 |
| 400 | 속성 오류 (사용자명/비밀번호 규칙 위반, 필수값 누락: Base.1.x PropertyMissing) |
| 401 | 인증 실패 (Security.1.0.AccessDenied) |
| 403 | 권한 부족 (InsufficientPrivilege) 또는 펌웨어 업데이트 중 (FWUpdateInProgress) |
| 404 | URI 없음 |
| 405 | 허용되지 않는 메서드 |
| 415 | Content-Type 오류 (application/json 필요) |
| 최대 개수 초과 | 계정 14개 초과 시 오류. 안 쓰는 계정을 DELETE 후 재시도 |

## 2. 세션 로그인으로 호출 (선택)
```bash
curl -k -i -X POST -H 'Content-Type: application/json' \
  -d '{"UserName":"<ADMIN_USER>","Password":"<ADMIN_PASSWORD>"}' \
  https://<BMC_IP>/redfish/v1/SessionService/Sessions
# 응답 헤더 X-Auth-Token 값을 이후 요청에 사용
curl -k -H "X-Auth-Token: <TOKEN>" -H 'Content-Type: application/json' -X POST \
  -d '{"UserName":"<NEW_USER>","Password":"<NEW_PASSWORD>","RoleId":"Operator","PasswordChangeRequired":false}' \
  https://<BMC_IP>/redfish/v1/AccountService/Accounts
```
AMI 레퍼런스: 동시 세션 한도가 있고(10~16), AccountService 가 비활성이면 새 세션을 만들 수 없다.

## 3. IPMI (ipmitool) 대체
Redfish 가 막혔거나 AMI 계열 IPMI 동기화를 쓸 때. 채널/ID 는 `user list` 로 먼저 확인한다 (XD675 가이드 예: 채널 1=eth0, 2=eth1. XD220v 는 미확인).
```bash
ipmitool -I lanplus -H <BMC_IP> -U <ADMIN_USER> -P <ADMIN_PASSWORD> user list 1
ipmitool -I lanplus -H <BMC_IP> -U <ADMIN_USER> -P <ADMIN_PASSWORD> user set name <USER_ID> <NEW_USER>
ipmitool -I lanplus -H <BMC_IP> -U <ADMIN_USER> -P <ADMIN_PASSWORD> user set password <USER_ID> <NEW_PASSWORD>
ipmitool -I lanplus -H <BMC_IP> -U <ADMIN_USER> -P <ADMIN_PASSWORD> user priv <USER_ID> 4 1
ipmitool -I lanplus -H <BMC_IP> -U <ADMIN_USER> -P <ADMIN_PASSWORD> user enable <USER_ID>
ipmitool -I lanplus -H <BMC_IP> -U <ADMIN_USER> -P <ADMIN_PASSWORD> channel setaccess 1 <USER_ID> callin=on ipmi=on link=on privilege=4
```
- 권한 레벨: 2 User, 3 Operator, 4 Administrator, 0xF No Access.
- 비밀번호 20자 이하 (IPMI 20바이트). 20자 초과 비밀번호는 ipmitool 이 거부한다 (Cray CSM 경고).
- 호스트 OS 에서는 `-I open` (로컬 KCS) 로 `-H/-U/-P` 없이 같은 명령을 쓸 수 있다.
- 근거: HPE XD675 BMC 가이드, Cray CSM `Add_Root_Service_Account_for_Gigabyte_Controllers`(AMI). XD220v 전용 문서는 아님.

## 4. Web UI
XD2000 BMC Web UI User Guide(dp00002278en_us)에 사용자 관리 화면이 있으나 PDF 22MB 라 이번에 읽지 못했다 (unavailable). 메뉴 경로를 추측해 적지 않는다. 가이드에서 "User Management" 항목을 확인한다.
