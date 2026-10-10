# HPE iLO 6 - 계정 id/pw 등록·패스워드 변경

## 프로토콜 판정: json (Redfish AccountService, `ManagerAccount.v1_3_0`) + xml(RIBCL, sustenance) + SSH CLI
- 관리망: HPE iLO 6 (ProLiant Gen11). Redfish 계정 API 전체 지원.
- 해당 사용자 보유 모델: **DL360 G11, DL380 G11, DL560 G11** (모두 iLO 6). 지원 불가 모델 없음.
- 주의: **RL3xx Gen11 은 RIBCL 미지원**(iLO 6 UG) - 사용자 모델 아님. 사용자 모델(DL 시리즈)은 RIBCL 지원.
- iLO 5 와 동일 데이터 모델(`Oem.Hpe`, RoleId, 10개 권한). 속성 "Added: iLO6 1.05". 지원 안 함: 애플리케이션 계정(AppAccounts, iLO 7 전용).
- HPE 권고: RIBCL/HPONCFG/HPQLOCFG 는 sustenance stage(중요 버그·보안 수정만) -> ilorest/Redfish 사용.

## 방식 요약
| 동작 | 메서드 / URI | 비고 |
|---|---|---|
| 생성 | `POST /redfish/v1/AccountService/Accounts/` | 컬렉션 Allow: GET POST |
| 암호 변경 | `PATCH /redfish/v1/AccountService/Accounts/{id}/` `{"Password":"..."}` | 응답 Password=null |
| 삭제 | `DELETE /redfish/v1/AccountService/Accounts/{id}/` | 개별 Allow: GET PATCH DELETE |
| 정책 | `PATCH /redfish/v1/AccountService` `Oem.Hpe.MinPasswordLength`, `Oem.Hpe.EnforcePasswordComplexity`, 표준 `AccountLockout*` | |
| 대체 | ilorest `iloaccounts`, RIBCL, SSH CLI, 웹 UI(Role 선택 추가), UEFI iLO 6 Configuration Utility | |

## RoleId 와 권한 / 용어
iLO 5 와 동일(HPE managingusers 문서). 요약: Administrator = HostBIOS/HostNIC/HostStorage/Login/RemoteConsole/UserConfig/VirtualMedia/VirtualPowerAndReset/iLOConfig, Operator = Administrator 에서 UserConfig·iLOConfig 제외, ReadOnly = Login. SystemRecoveryConfigPriv 는 role 에 포함되지 않음(개별 지정). Redfish `UserName` = 웹 UI Login Name(로그인 ID), `Oem.Hpe.LoginName` = 설명용 User Name.

## 기본 계정 / 정책 / 슬롯
| 항목 | 값 | 출처 | 검증 |
|---|---|---|---|
| 기본 계정 | `Administrator`. 암호는 pull tab 랜덤 8자 또는 공통 기본 암호(SKU P08040-B21 주문 시) | iLO 6 1.53 UG | verified |
| 공장 초기화 후 | pull tab 기본 계정. 사전 지정 `Oem.Hpe.DefaultUserName/DefaultPassword` | iLO 6 레퍼런스 + HPE 문서 | verified |
| 슬롯 | 로컬 **12**개 | iLO 6 1.53 UG | verified |
| UserName / LoginName | 각 최대 39자 | UG + 레퍼런스 | verified |
| 암호 | 최대 39자, 최소 `MinPasswordLength` 0~39 (기본 8) | UG + 레퍼런스 | verified |
| 복잡도 | `EnforcePasswordComplexity` 기본 false(UG: "disabled (default)"). 활성 시 대/소/숫자/기타 중 3종 | UG + 레퍼런스 | verified |
| IPMI/DCMI 사용자 | 로그인 16자/암호 20자 이하. IPMI over LAN 기본 Disabled | UG | verified |
| 보안 상태 | 기본 `Production`. 높은 상태는 in-band 에도 자격증명 필요 | UG + HPE 문서 | verified |
| Id 형식 | 레퍼런스 예시 `Accounts/1/`; HPE 문서 예시에는 `65536`.. 대 존재. GET 으로 확인 | HPE 문서 | verified |

## URI 및 검증 상태
| 용도 | URI | 검증 | 근거 |
|---|---|---|---|
| 컬렉션 | `/redfish/v1/AccountService/Accounts/` (GET, POST) | verified | iLO 6 레퍼런스(ManagerAccountCollection Allow: GET POST) + HPE managingusers |
| 개별 | `/redfish/v1/AccountService/Accounts/{item}` (GET, PATCH, DELETE) | verified | iLO 6 레퍼런스 |
| AccountService | `/redfish/v1/AccountService` | verified | 레퍼런스 |
| 공장 초기화 | Manager Action `HpeiLO.ResetToFactoryDefaults` | verified(iLO 5 레퍼런스 확인, iLO 6 UG/문서에 factorydefaults 언급) | 모든 설정 삭제 |

## 시도 내역 (Jev 생략 - API 키 없음. "문서 2중 출처"로 판정)
- 1차(공식 문서군): HPE iLO 6 RESTful API 레퍼런스 raw 텍스트(ManagerAccount.v1_3_0, 컬렉션 Allow, AccountService 정책 속성), HPE iLO 6 1.53 User Guide PDF(Hitachi 미러; 12 계정, 기본 계정/SKU, 암호 정책, RIBCL sustenance/RL3xx 제한), HPE managingusers 문서.
- 2차(공개 코드): python-ilorest-library `add_user_account.py`/`modify_user_account.py`, ilorest `iloaccounts` 문서, SaltStack ilo.py(RIBCL).
- 실패: HPE iLO 6 Scripting and Command Line Guide 원문 접근 차단 -> RIBCL 신규 권한 태그·SSH CLI iLO 6 구문 unverified. python-redfish-utility 소스 raw 404.

## 파일
- `create_account.md`, `change_password.md`
