# XD2000 BMC (XD220V) - 기존 id 패스워드 변경

상태: Redfish PATCH = verified (AMI 계열 일반 동작, XD220v 실기 응답 미확인), IPMI = verified (AMI/타 Cray 문서, XD220v 전용 아님), Web UI 메뉴 = unavailable. 값은 모두 플레이스홀더 (`<BMC_IP>`, `<ADMIN_USER>`, `<CURRENT_PASSWORD>`, `<NEW_PASSWORD>`, `<ACCOUNT_ID>`).
적용 모델: XD220V (XD225v, XD295v 는 같은 BMC 계열). 새 계정 생성은 `create_account.md`.

## 0. 사전 확인 (GET) - 슬롯 찾기
```bash
curl -k -u <ADMIN_USER>:<CURRENT_PASSWORD> https://<BMC_IP>/redfish/v1/AccountService/Accounts
# 각 멤버를 GET 해서 UserName 으로 <ACCOUNT_ID> 확인
curl -k -u <ADMIN_USER>:<CURRENT_PASSWORD> https://<BMC_IP>/redfish/v1/AccountService/Accounts/<ACCOUNT_ID>
curl -k -u <ADMIN_USER>:<CURRENT_PASSWORD> https://<BMC_IP>/redfish/v1/AccountService
```
- 슬롯 번호는 장비마다 다르다 (Cray CSM 예시 Accounts/1, 4 / DGX H100 은 2=admin / AMI 레퍼런스는 1=Administrator). **XD220v 값은 unverified. 가정하지 말고 GET 으로 확인.**
- `AccountService` 의 `MinPasswordLength`, `MaxPasswordLength`, `AccountLockoutThreshold` 로 정책 확인.
- 3번째 GET 의 응답 헤더 `ETag` 를 확보 (아래 조건부 헤더용): `curl -k -i ... | grep -i etag`

## 1. Redfish: PATCH
```
PATCH https://<BMC_IP>/redfish/v1/AccountService/Accounts/<ACCOUNT_ID>
Content-Type: application/json
If-Match: *            (또는 If-Match: <ETag 값>, 또는 If-None-Match: *)
```
```json
{ "Password": "<NEW_PASSWORD>" }
```
```bash
curl -k -u <ADMIN_USER>:<CURRENT_PASSWORD> -X PATCH \
  -H 'Content-Type: application/json' -H 'If-Match: *' \
  -d '{"Password":"<NEW_PASSWORD>"}' \
  https://<BMC_IP>/redfish/v1/AccountService/Accounts/<ACCOUNT_ID>
```

### 조건부 헤더 (If-Match / If-None-Match)
| 상황 | 동작 | 근거 |
|---|---|---|
| 헤더 없이 PATCH/PUT | AMI MegaRAC 은 HTTP 428 (Precondition Required) 로 거부할 수 있음 | AMI 계열 일반 동작. CONTROLLERS.md 에 기재. XD220v 실기 미확인 |
| `If-Match: *` | 어떤 ETag 든 통과 (NVIDIA DGX H100 문서가 사용) | verified (AMI 계열) |
| `If-Match: <ETag>` | 현재 ETag 와 일치할 때만 변경. 불일치 시 412 | DMTF DSP0266 일반 규칙 |
| `If-None-Match: *` | Cray CSM River BMC 자격 변경 절차가 사용했다고 기록되어 있으나, 이번 재조사에서 해당 문서 원문 재확인 실패 (아래 재시도 3 참조) | unverified (CSM) |
- 어느 헤더가 통하는지는 장비 응답으로 판단: 428 이면 헤더 추가, 412 이면 ETag 를 다시 GET 해서 재시도.
- POST(계정 생성)에는 조건부 헤더 불필요.

### 응답 / 오류
| 코드 | 의미 |
|---|---|
| 200 | 변경 성공 (AMI 는 갱신된 계정 JSON 반환, `Password` 는 null). 204 가능성은 미확인 |
| 400 | 비밀번호 정책 위반 (길이/복잡도), 속성 오류 |
| 401 | 인증 실패 |
| 403 | 권한 부족 (ConfigureUsers 없음) 또는 FW 업데이트 중 |
| 404 | `<ACCOUNT_ID>` 없음 |
| 412 / 428 | ETag 불일치 / 조건부 헤더 누락 |
| 415 | Content-Type 오류 |
- 권한: 타 계정은 Administrator (ConfigureUsers). 본인 암호는 ConfigureSelf 로 가능할 것으로 추정 (unverified).
- 변경 후 세션 무효화 여부: 문서 없음 (unverified). 새 암호로 재인증 권장:
```bash
curl -k -i -X POST -H 'Content-Type: application/json' \
  -d '{"UserName":"<ADMIN_USER>","Password":"<NEW_PASSWORD>"}' \
  https://<BMC_IP>/redfish/v1/SessionService/Sessions
```
- 확인: `GET .../Accounts/<ACCOUNT_ID>` 의 `PasswordChangeRequired`, `Locked` 값.
- 초기 계정이 `PasswordChangeRequired: true` 면(create_account.md 참고) 그 계정으로 위 PATCH 를 해서 해소.

## 2. IPMI (ipmitool)
```bash
ipmitool -I lanplus -H <BMC_IP> -U <ADMIN_USER> -P <CURRENT_PASSWORD> user list 1
ipmitool -I lanplus -H <BMC_IP> -U <ADMIN_USER> -P <CURRENT_PASSWORD> user set password <USER_ID> <NEW_PASSWORD>
# 호스트 OS 에서 (로컬 KCS)
ipmitool -I open user set password <USER_ID> <NEW_PASSWORD>
```
- 채널 1 이 아닐 수 있다: `user list <채널>` 로 확인 (XD220v 채널 번호 unverified).
- `<USER_ID>` 는 IPMI 번호이며 Redfish `<ACCOUNT_ID>` 와 같다고 가정하지 말 것 (AMI 는 동기화하지만 번호 일치는 미확인). Cray CSM Gigabyte(AMI) 예시는 IPMI ID 4 가 Redfish Accounts/4 로 보임.
- 비밀번호 20자 이하 (IPMI 20바이트, Cray CSM 경고: 20자 초과 시 ipmitool 거부, 관리 노드 전원 절차 실패).
- 선택: 20바이트 길이 인자 `user set password <ID> <PW> 20` 은 일반 ipmitool 문법 (XD220v 전용 아님).

## 3. Web UI
XD2000 BMC Web UI User Guide(`dp00002278en_us`, PDF 22MB)에 사용자 관리 화면이 있으나 읽지 못했다 -> 메뉴 경로 **unavailable**, 추측하지 않는다. 실장비에서는 로그인 후 사용자(User) 관리 메뉴에서 대상 사용자를 선택해 Password 를 수정한다 (문구는 가이드 확인 필요). XD220v 가이드의 알려진 메뉴는 "Maintenance > Firmware Update" 뿐이다.
- 다른 AMI 기반 BMC 예: 일부 AMI 펌웨어는 Configuration > Users > 사용자 선택 > Modify User > Change Password (타 벤더 문서). XD220v 와 동일하다는 근거 없음.

## 4. 정책 (XD220v 전용 값은 unverified)
| 항목 | 값 | 상태 |
|---|---|---|
| 비밀번호 길이 | 8~20자 (IPMI 동기화 규칙, AMI 레퍼런스). 실제는 AccountService Min/MaxPasswordLength 로 확인 | verified (AMI) / XD220v unverified |
| 사용자명 | 1~16자 영숫자, 첫 글자 영문, `-` `_` `@` | verified (AMI) |
| 잠금 임계값 | AMI 기본 5 | verified (AMI) / XD220v unverified |
| 계정 최대 | 14개 | verified (AMI) / XD220v unverified |
| 기본 계정명 | HPE 가이드는 서버 라벨의 사용자명/비밀번호라고만 기재 | unavailable |

## 출처
- HPE Cray XD220v Server User Guide `sd00002298en_us` (로그인은 서버 라벨 계정)
- AMI MegaRAC Redfish 레퍼런스(Lenovo 포장): https://pubs.lenovo.com/tsm/bmc_redfish_api_reference_sr635_sr655.pdf
- NVIDIA DGX H100 Redfish: https://docs.nvidia.com/dgx/dgxh100-user-guide/redfish-api-supp.html
- Cray CSM `Add_Root_Service_Account_for_Gigabyte_Controllers` (IPMI 명령, Accounts/4 예), `Change_Air-Cooled_Node_BMC_Credentials` (20자 경고): https://github.com/Cray-HPE/docs-csm
