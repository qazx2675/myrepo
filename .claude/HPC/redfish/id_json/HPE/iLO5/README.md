# HPE iLO 5 - 계정 id/pw 등록·패스워드 변경

## 프로토콜 판정: json (Redfish AccountService, `ManagerAccount.v1_3_0`) + xml(RIBCL, 레거시) + SSH CLI
- 관리망: HPE iLO 5 (ProLiant Gen10 / Gen10 Plus). Redfish 계정 API 전체 지원.
- 해당 사용자 보유 모델: **DL360 G10, DL560 G10, DL580 G10, XL270d G10, DL360 G10 Plus** (모두 iLO 5). 지원 불가 모델 없음.
- iLO 4 와 차이: OEM 네임스페이스 `Oem.Hpe`(iLO 4 는 `Oem.Hp`), `RoleId` 로 생성/변경 가능, 권한 4개 추가(HostBIOSConfigPriv, HostNICConfigPriv, HostStorageConfigPriv, SystemRecoveryConfigPriv), `ServiceAccount`, AccountService `Oem.Hpe.EnforcePasswordComplexity`(1.40+), `DefaultUserName/DefaultPassword`(1.17+), 표준 `MinPasswordLength/MaxPasswordLength`(읽기전용, 1.40+).
- 지원 안 함: 애플리케이션 계정(AppAccounts, iLO 7 전용).

## 방식 요약
| 동작 | 메서드 / URI | 비고 |
|---|---|---|
| 생성 | `POST /redfish/v1/AccountService/Accounts/` | `UserName`, `Password`, (`RoleId` 또는 `Oem.Hpe.Privileges`), `Oem.Hpe.LoginName` |
| 암호 변경 | `PATCH /redfish/v1/AccountService/Accounts/{id}/` `{"Password":"..."}` | 응답 Password=null |
| 삭제 | `DELETE /redfish/v1/AccountService/Accounts/{id}/` | `UserConfigPriv` 필요 |
| 정책 | `PATCH /redfish/v1/AccountService` `Oem.Hpe.MinPasswordLength`(0~39), `Oem.Hpe.EnforcePasswordComplexity`, 표준 `AccountLockout*` | |
| 대체 | ilorest `iloaccounts add/changepass/delete`, RIBCL, SSH CLI, 웹 UI, UEFI iLO 5 Configuration Utility | |

## 용어 혼동
Redfish `UserName` = 웹 UI "Login Name"(로그인 ID). `Oem/Hpe/LoginName` = 웹 UI "User Name"(설명). 로그인은 Redfish `UserName` 으로.

## RoleId 와 권한 (HPE managingusers 문서)
| RoleId | 부여 권한 |
|---|---|
| Administrator | HostBIOSConfig, HostNICConfig, HostStorageConfig, Login, RemoteConsole, UserConfig, VirtualMedia, VirtualPowerAndReset, iLOConfig (SystemRecoveryConfig 는 제외) |
| Operator | HostBIOSConfig, HostNICConfig, HostStorageConfig, Login, RemoteConsole, VirtualMedia, VirtualPowerAndReset |
| ReadOnly | Login |
- RoleId 는 저장되지 않고 권한에서 계산됨(Login 만=ReadOnly, iLOConfig/UserConfig/SystemRecoveryConfig 포함=Administrator, 그 외=Operator). RoleId 와 개별 권한을 함께 보내면 role 권한 먼저, 이후 명시 권한 적용. 기존 계정에 RoleId PATCH 하면 기존 권한이 role 권한으로 **초기화**.
- 권한 지정 없이 POST 하면 ReadOnly(Login 만).

## 기본 계정 / 정책 / 슬롯
| 항목 | 값 | 출처 | 검증 |
|---|---|---|---|
| 기본 계정 | `Administrator`. 암호는 serial label pull tab 의 랜덤 8자 **또는** 공통 기본 암호(주문 SKU 에 따라) | iLO 5 2.10 UG | verified |
| 공장 초기화 후 | pull tab 의 기본 계정. 사전 지정: `PATCH /redfish/v1/AccountService {"Oem":{"Hpe":{"DefaultUserName":"...","DefaultPassword":"..."}}}` (1.17+) | iLO 5 레퍼런스 + HPE 문서 | verified |
| 슬롯 | 로컬 **12**개 (초과는 디렉터리) | iLO 5 2.10 UG ("create up to 12 local user accounts") | verified |
| UserName / LoginName | 각 최대 39자, 인쇄 가능 문자 | UG + 레퍼런스 | verified |
| 암호 | 최대 39자, 최소 `MinPasswordLength` 0~39 (기본 8) | UG + 레퍼런스 | verified |
| 복잡도 | `EnforcePasswordComplexity` 기본 false(비활성). 활성 시 대/소/숫자/기타 중 3종 | UG + 레퍼런스(1.40) | verified |
| IPMI/DCMI 사용자 | 로그인 16자 / 암호 20자 이하. IPMI over LAN 기본 Disabled | UG | verified |
| Id 형식 | 문서 예시는 `Accounts/1/`, `Accounts/12/` 및 `65536`, `65544` 등 큰 수(펌웨어/HPE 문서 버전별). 반드시 GET 컬렉션으로 확인 | HPE 문서 예시 | verified |
| 서비스 계정 | `Oem.Hpe.ServiceAccount` 는 **생성 시에만** 지정, 이후 편집 불가 | UG | verified |

## URI 및 검증 상태
| 용도 | URI | 검증 | 근거 |
|---|---|---|---|
| 계정 컬렉션 | `/redfish/v1/AccountService/Accounts/` (GET, POST) | verified | iLO 5 레퍼런스 + HPE managingusers + python-ilorest-library `add_user_account.py` |
| 개별 | `/redfish/v1/AccountService/Accounts/{item}` (GET, PATCH, DELETE) | verified | iLO 5 레퍼런스(ManagerAccount.v1_3_0 Allow: GET PATCH DELETE) |
| AccountService | `/redfish/v1/AccountService` | verified | 레퍼런스 |
| 사용자 인증서 매핑 | `/redfish/v1/AccountService/UserCertificateMapping/` | verified(ilorest 문서) | 참고용 |
| 공장 초기화 | Manager Action `HpeiLO.ResetToFactoryDefaults` `{"ResetType":"Default"}` | verified | 레퍼런스 (모든 설정 삭제) |

## 시도 내역 (Jev 생략 - API 키 없음. "문서 2중 출처"로 판정)
- 1차(공식 문서군): HPE iLO 5 RESTful API 레퍼런스 raw 텍스트(ManagerAccount.v1_3_0 전 속성, AccountService 정책 속성, 컬렉션 Allow), HPE iLO 5 2.10 User Guide PDF(Hitachi 미러; 12 계정, 기본 계정, 암호 정책, IPMI), HPE managingusers 문서(role/권한 표, POST/PATCH/DELETE).
- 2차(공개 코드): python-ilorest-library `add_user_account.py`/`modify_user_account.py`(POST 컬렉션, `Oem.Hpe.Privileges`, 400 ExtendedInfo), ilorest `iloaccounts` 명령 문서(add/modify/changepass/delete), SaltStack ilo.py (RIBCL).
- 실패: HPE support.hpe.com iLO 5 Scripting and Command Line Guide(RIBCL ADD_USER 원문) 접근 차단(403) -> iLO 5 신규 권한 RIBCL 태그명 unverified. python-redfish-utility 소스 raw 경로 404.

## 한계
- 실장비 응답 코드 미확인. iLO 5 SSH CLI 계정 verb 의 최신 문법은 HPE 스크립팅 가이드 원문 미확인.

## 파일
- `create_account.md`, `change_password.md`
