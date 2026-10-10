# Cisco CIMC (standalone C-Series) - 계정 id/pw 등록·패스워드 변경

## 프로토콜 판정: **json (Redfish AccountService, IMC 3.0+)** + xml (`aaaUser`) + CLI
- 관리망: Cisco Integrated Management Controller (rack/standalone C-Series). Redfish 는 IMC 3.0 REST API 가이드부터 문서화, 4.2/4.3 가이드에 계정 예시 있음.
- **사용자 보유 모델 중 이 모드로 동작하는 모델: 없음.** B200 M4/M5, B480 M5 는 블레이드 전용(UCSM 필수, standalone 불가). X210c M7 은 UCSM 또는 IMM(Intersight) 관리이며 standalone CIMC 모드 없음. 이 폴더는 참고용(보유 모델 없음).
- Jev 생략. 판정은 Cisco IMC REST API Programmer's Guide(3.0/4.2) + imcsdk 소스 2중.

## 방식 요약
| 동작 | 방법 | 검증 |
|---|---|---|
| 목록 | `GET /redfish/v1/AccountService/Accounts` (슬롯 1,2,3… 멤버) | verified (3.0, 4.2 가이드) |
| 생성 | 4.2: `POST /redfish/v1/AccountService/Accounts` body `{"Id":"11","UserName":..,"Password":..,"RoleId":"admin","Enabled":true}`; 3.0: `POST .../Accounts/5` body 에 `"Enabled":"true"`(문자열) | verified (두 버전 문서가 다름 - 버전별로 구분) |
| 암호 변경 | `PATCH /redfish/v1/AccountService/Accounts/<id>` `{"Password":"..."}` | verified (4.2 가이드 "Changing Password with PATCH ... After Restore Factory Default", 3.0 Modifying User) |
| 삭제 | `DELETE /redfish/v1/AccountService/Accounts/<id>` | verified (3.0 가이드 예시만; 4.2 목차에는 없음) |
| 정책 | `PATCH /redfish/v1/AccountService` `Oem.Cisco.{StrongPasswordPolicyEnabled,PasswordHistory,PasswordExpiry{...}}` | verified (4.2 가이드; C220 M4/C240 M4/C460 M4/S3X60 미지원) |
| XML | `configConfMo` dn=`sys/user-ext/user-<id>` `<aaaUser id name pwd priv accountStatus/>` | verified (imcsdk `AaaUser` 메타 + `apis/admin/user.py`) |
| CLI | `scope user <N>` / `set enabled yes` / `set name` / `set password` / `set role` / `commit` | verified (Cisco CIMC CLI Config Guide 1.2(1) - 구버전 문서, 신버전 동일 문법은 unverified) |

## 기본 계정 / 정책
- 기본 계정명 **admin** (슬롯 1, RoleId admin, Redfish 목록 예시). 공장 초기화 후 `PasswordChangeRequired=true` - 첫 PATCH `{"Password":..}` 로 해제 (4.2 가이드). 기본 암호는 문서에 명기돼 있지 않아 기재하지 않음 (FN64093: 일부 2015-11~2016-01 출하 장비는 비표준 기본 암호).
- 역할(Redfish `Roles`): admin(Login, ConfigureManager, ConfigureUsers, ConfigureSelf, ConfigureComponents + OemClearLog/OemPowerControl), user(Login, ConfigureSelf, ConfigureComponents), read-only(Login). XML `priv`: admin / read-only / user / snmponly(classic 메타).
- 계정 슬롯(imcsdk `AaaUser` 메타): classic(rack C-Series) id 1~32, name 0~32자 `[a-zA-Z0-9._+-]`, priv `""/admin/read-only/snmponly/user`; modular(M-series) id 1~15, name 0~16, pwd 0~20자. 실제 rack 슬롯 수는 모델/버전별로 다를 수 있어 실장비 `GET .../Accounts` 로 확인.
- 암호: Redfish `MinPasswordLength` 8 / `MaxPasswordLength` 20 (4.2 가이드 응답 예시). 강한 암호 정책 켜면 복잡도 적용(imcsdk `AaaUserPolicy.userPasswordPolicy` enabled/disabled).
- **이전 암호 재사용 금지**: `Oem.Cisco.PasswordHistory` (Redfish) / `AaaUserPasswordExpiration.passwordHistory` 0~5 (XML, IMC 3.0.1c+). 만료: `passwordExpiryDuration` 0~3650일(0=비활성), `passwordNotificationPeriod` 0~15, `passwordGracePeriod` 0~5.

## 한계
- 사용자 보유 모델이 아니므로 실장비 검증 불가. 문서/SDK 기반.
- 3.0 과 4.2 가이드의 POST 형식이 다름(슬롯 지정 vs 컬렉션). 장비 펌웨어에 맞게 `GET /redfish/v1/AccountService` 의 `Accounts` 컬렉션 `Allow` 헤더로 판별.

## 시도 내역
- 1차(Cisco 문서): C-Series REST API Programmer's Guide 3.0 / 4.2 (WebFetch 3구간 읽기) - 계정 조회/생성/수정/삭제/정책/암호 변경. 4.2 URL `www2-realm` 변형은 403, 정식 도메인으로 성공.
- 2차(공개 코드): imcsdk `AaaUser.py`, `AaaUserPolicy.py`, `AaaUserPasswordExpiration.py`, `apis/admin/user.py`, `imcsession.py`(`/nuova`).
- 미해결: 기본 암호 공식 표기, 4.3 가이드 계정 장, CIMC CLI 최신 버전 문법, 응답 오류 코드.

## 파일
- `create_account.md`, `change_password.md`
