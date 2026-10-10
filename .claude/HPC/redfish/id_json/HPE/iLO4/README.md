# HPE iLO 4 - 계정 id/pw 등록·패스워드 변경

## 프로토콜 판정: json (Redfish 1.0 / 구 iLO RESTful API) + xml(RIBCL) + SSH CLI(SMASH CLP)
- 관리망: HPE iLO 4 (ProLiant Gen8 / Gen9). iLO RESTful API 는 iLO 4 2.00(Gen9)부터, **Redfish 1.0 적합은 iLO 4 2.30 이상**. 2.30 은 `/redfish/v1/` 와 구 `/rest/v1/` 를 동일 리소스 모델로 **둘 다 미러링**한다. (iLO 4 사용자 가이드 2.81 "iLO RESTful API", HPE iLO 4 RESTful API 레퍼런스 Introduction)
- 해당 사용자 보유 모델: **DL360 G9, XL170r G9, XL250 G9, XL270d G9** (Gen9, Redfish/REST 가능), **DL560 G8** (Gen8, iLO 4 - 아래 주의)
- **DL560 G8**: iLO 4 이므로 RIBCL(XML)·SSH CLI·웹 UI 로는 계정 관리 가능(문서 확인). Redfish/REST 계정 API 는 HPE iLO 4 REST 레퍼런스가 "Gen9, iLO 4 2.00+" 로 쓰여 있어 Gen8 에서의 동작은 **unverified** (이전 bios_json/back/HPE/DL560_G8 조사는 Gen8 도 iLO 4 2.00+ 에서 RESTful 제공이라 판단했으나 HPE 공식 2차 출처 없음). 실장비에서 `GET /redfish/v1/AccountService` 가 200 이면 Redfish, 아니면 RIBCL/CLI 사용.
- 지원 안 함/해당 없음: 애플리케이션 계정(iLO 7 전용), `RoleId` 로 생성(문서상 iLO 5 이상 - Add_User 샘플 "role ID by iLO 5"), Host BIOS/NIC/Storage/SystemRecovery 권한(iLO 5 신규). iLO 4 권한은 5개뿐.

## 방식 요약
| 동작 | 메서드 / URI | 비고 |
|---|---|---|
| 생성 | `POST /redfish/v1/AccountService/Accounts/` (구: `POST /rest/v1/AccountService/Accounts`) | `UserName`, `Password`, `Oem.Hp.LoginName`, `Oem.Hp.Privileges.*` |
| 암호 변경 | `PATCH /redfish/v1/AccountService/Accounts/{id}` `{"Password":"..."}` | 응답에서 Password 는 항상 null |
| 삭제 | `DELETE /redfish/v1/AccountService/Accounts/{id}` | `UserConfigPriv` 필요 |
| 정책 | `PATCH /redfish/v1/AccountService` `{"Oem":{"Hp":{"MinPasswordLength":N}}}` | 0~39 |
| 대체 | RIBCL `ADD_USER`/`MOD_USER`/`DELETE_USER`, SSH CLI `create /map1/accounts1`, ilorest `iloaccounts`, 웹 UI | create/change 문서 참조 |

**주의: OEM 네임스페이스가 `Oem.Hp` (iLO 5+ 의 `Oem.Hpe` 아님).** iLO 4 REST 레퍼런스는 모든 예가 `Oem.Hp`. python-ilorest-library 샘플도 gen9 용 `add_ilo_user_account_gen9` 는 `Oem.Hp`. iLO 4 최신 펌웨어가 `Hpe` 도 병행 노출하는지는 미확인이므로 먼저 기존 계정을 GET 해서 키 이름을 확인.

## 용어 혼동 (HPE 문서 명시)
Redfish `UserName` = 웹 UI "Login Name"(로그인 ID), Redfish `Oem/Hp(e)/LoginName` = 웹 UI "User Name"(설명용 이름). 로그인할 때 쓰는 것은 Redfish `UserName`.

## 기본 계정 / 정책 / 슬롯
| 항목 | 값 | 출처 | 검증 |
|---|---|---|---|
| 기본 계정 | `Administrator`, 암호는 서버 앞면 serial label pull tab 의 랜덤 8자 | iLO 4 2.81 UG "iLO default DNS name and user account" | verified |
| 공장 초기화 후 | pull tab 의 기본 계정으로 로그인. 초기화 후 기본 계정을 미리 지정하는 속성(`Oem.Hpe.DefaultUserName/DefaultPassword`)은 iLO 5 1.17 부터이며 iLO 4 레퍼런스에는 없음 | iLO 5 레퍼런스 | iLO 4 미지원 |
| 슬롯 | 로컬 계정 최대 **12**개 (초과는 디렉터리 서비스) | iLO 4 2.81 UG | verified |
| 사용자명(UserName)/LoginName 최대 | 39자, 인쇄 가능 문자 | UG + REST 레퍼런스 | verified |
| 암호 길이 | 최소 `MinPasswordLength`(0~39, 기본 8), 최대 39자 | UG + REST 레퍼런스(ManagerAccount/Password) | verified |
| 복잡도 | UG 권고: 대/소/숫자/기타 중 3종. iLO 4 에는 강제 토글(EnforcePasswordComplexity) 속성이 REST 레퍼런스에 없음(iLO 5 1.40 부터) | UG, 레퍼런스 | verified(없음 확인) |
| IPMI/DCMI 사용자 | 로그인명 16자, 암호 20자 이하. IPMI over LAN 기본 Disabled(2.60+) | UG | verified |
| 권한 | LoginPriv, RemoteConsolePriv, VirtualMediaPriv, VirtualPowerAndResetPriv, iLOConfigPriv, UserConfigPriv | REST 레퍼런스 | verified |
| Id 형식 | 보통 `1..` 숫자 (예: 레퍼런스/문서 `Accounts/12/`) | 문서 예시 | 실장비 GET 으로 확인 |

## URI 및 검증 상태
| 용도 | URI | 검증 | 근거 |
|---|---|---|---|
| 계정 컬렉션 | `/redfish/v1/AccountService/Accounts` (GET, POST) | verified | iLO 4 REST 레퍼런스 AccountService("POST ... accounts/", PATCH, DELETE 설명) + python-ilorest-library `add_user_account_gen9`(POST `/redfish/v1/AccountService/Accounts/`) |
| 계정 개별 | `/redfish/v1/AccountService/Accounts/{item}` (GET, PATCH, DELETE) | verified | 동일 + HPE managingusers 문서 |
| 구 REST | `/rest/v1/AccountService/Accounts` | verified(미러링 문구) | iLO 4 REST 레퍼런스 "Mirroring the resource model at both /redfish/v1/ and /rest/v1" + 서드파티 위키(`links.Accounts.href` 로 POST) |
| AccountService | `/redfish/v1/AccountService` (`Oem.Hp.MinPasswordLength` 등) | verified | iLO 4 REST 레퍼런스 |
| 공장 초기화 | `POST /redfish/v1/Managers/1/` Action `ResetToFactoryDefaults` `{"ResetType":"Default"}` | verified(레퍼런스 문구; 사용자 계정 삭제 + 기본값 복원) | 주의: 모든 설정 삭제 |

## 시도 내역 (Jev 생략 - API 키 없음. "문서 2중 출처"로 판정)
- 1차(공식 문서군): HPE iLO 4 RESTful API 레퍼런스(hewlettpackard.github.io/ilo-rest-api-docs/ilo4) 의 AccountService/ManagerAccount 절 raw 텍스트, HPE iLO 4 2.81 User Guide PDF(사용자 계정·IPMI·기본 계정), HPE Redfish 문서 포털 managingusers.md / ilorest iloaccounts 절.
- 2차(공개 코드): python-ilorest-library `examples/Redfish/add_user_account.py`(gen9 분기 `Oem.Hp`), `modify_user_account.py`, SaltStack `salt/modules/ilo.py`(RIBCL ADD_USER/MOD_USER/DELETE_USER), 서드파티 SSH CLI 가이드(GoLinuxHub iLO4 2.55) + Qualys 문서.
- 실패: HPE support.hpe.com 의 iLO 4 Scripting and Command Line Guide 직접 접근 불가(차단) -> RIBCL 속성은 Salt 소스와 커뮤니티 예시로 대체, SSH CLI 권한 그룹 문법(`group=admin,config,oemhp_rc,...`)은 unverified. python-redfish-utility(ilorest) 소스는 raw 경로 404 로 못 읽음 -> ilorest 공식 사용자 가이드 문서로 대체.

## 한계
- 실장비 응답 코드 미확인. iLO 4 구버전 펌웨어(2.30 미만)는 `/rest/v1` 전용.
- DL560 G8 Redfish 계정 API 지원은 unverified (위 설명).

## 파일
- `create_account.md`, `change_password.md`
