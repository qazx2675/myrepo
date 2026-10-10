# HPE iLO 5 — BIOS (Redfish)

조사일 2026-10-10. Jev 판정 생략(API 키 없음) — "공식 문서 + 독립된 두 번째 출처" 로 verified 판정.

## 1. 프로토콜: **json** (Redfish 전용, `/rest/v1` 레거시 제거)
- 근거: iLO5 adaptation 문서 "iLO5 supports Redfish standard BIOS Attributes and BIOS Attribute Registry resources that replace the HPE proprietary versions used in iLO4", 관측 mock(iLO 5 v3.11, 2018 캡처) 2종. verified.
- 최신 펌웨어: **iLO 5 3.21** (2026-07-31, Wikipedia; 포털 changelog 3.21 "No features are introduced or modified"). 포털의 Bios 정의 문서는 **3.19** 까지(3.20/3.21 문서 404). BIOS 정의에 3.19~3.21 변경 없음(changelog).

## 2. 해당 사용자 모델 (iLO5)
| 모델 | 세대 | ROM 패밀리 | 비고 |
|---|---|---|---|
| DL360 Gen10 | Gen10 | U32 | 이전 back 조사(SPP 목록 근거) — unverified |
| DL560 Gen10, DL580 Gen10 | Gen10 | U34 | 이전 조사 — unverified. U34 실캡처(레지스트리 `U34.v1_1_20`, 235속성)가 있으나 캡처 장비 모델은 미상 |
| XL270d Gen10 | Gen10 | U45 | 이전 조사 — unverified |
| DL360 Gen10 Plus | Gen10 Plus | **U46** | **verified** — HPE ilo-redfish-emulator mock 에서 "ProLiant DL360 Gen10 Plus, iLO 5 v3.11, U46 v2.34 (03/11/2025)" |
- 매핑 정정: 없음(모두 iLO5 가 맞음). Gen10 과 Gen10 Plus 는 **같은 iLO5 지만 Bios OEM URI 구조가 다름**(3절).

## 3. ROM 패밀리별 차이 (실측 근거)
iLO5 의 BIOS 속성은 **ROM 패밀리(Uxx) 별로 크게 다르다**. 공개된 실캡처 두 개만 비교해도:
| 항목 | U34 (Gen10, 2018, iLO5 1.x, `Bios.v1_0_0`) | U46 (Gen10 Plus, 2025, iLO5 3.11, `Bios.v1_0_4`) |
|---|---|---|
| 속성 수 | 235 | 276 |
| 공통 | 154 | 154 |
| 한쪽에만 | 81 | 122 |
- 따라서 `bios_attributes.json` 은 단일 ROM 의 목록이 아니라 **합집합**(763개)이며 각 속성의 `seen_in` 으로 출처를 구분한다. 문서 샘플(3.09 Intel 401 / 3.19 AMD 540)과 실캡처도 서로 다름(U46 의 122개가 U34 에 없음; U46 중 문서 3.09/3.19 에 없는 이름 다수: 예 `EppProfile`, `MkTme`, `SgxEnable`, `NvmeRaid` …).
- U30(DL380 Gen10 계열)/U32/U45 의 실캡처는 공개 자료에서 못 찾음(GitHub 코드 검색 `BiosAttributeRegistryU30/U32/U45` -> 없음 또는 문서뿐) -> **U32/U45 속성은 unavailable**. 레지스트리 링크만: HPE Support Center "RESTful API BIOS Schemas/Registries - HPE <모델> (U32|U34|U45|U46)" (로그인/EULA, 받지 않음) https://support.hpe.com

## 4. URI (iLO 5)
| URI | 용도 | 상태 | 근거(2중) |
|---|---|---|---|
| `/redfish/v1/Systems/1/Bios` | 현재 속성 (`Attributes` 오브젝트), GET | **verified** | ① iLO5 v3.09/3.19 Bios 정의 "Resource Instances /redfish/v1/systems/{item}/bios GET" ② biosdoc + U34/U46 실캡처 |
| `/redfish/v1/Systems/1/Bios/Settings` | 대기 설정 GET/PATCH(/POST) | **verified** | 동일 |
| `…/Bios/Settings/Actions/Bios.ResetBios` (POST `{}`) | 기본값으로 초기화 | verified (iLO5 adaptation + U34/U46 의 `Actions` target) | |
| `…/Bios/Settings/Actions/Bios.ChangePassword` (POST: PasswordName=Administrator\|User, OldPassword, NewPassword) | BIOS 암호 변경 | verified (adaptation; 실캡처 target 은 `Bios.ChangePasswords` 복수형 표기 — 장비의 Actions 값을 따를 것) | |
| `/redfish/v1/Registries` -> `BiosAttributeRegistryU46.v1_2_34` -> `/redfish/v1/registrystore/registries/en/biosattributeregistryu46.v1_2_34` (ja, zh 도 존재) | 속성 레지스트리 (`RegistryEntries.Attributes[]`) | verified(경로, U46 실캡처), 내용 unavailable | |
| **Gen10 (iLO5 2.33 이전 구조)** OEM: `/redfish/v1/systems/1/bios/baseconfigs/`, `/bios/boot/`, `/bios/mappings/`, `/bios/tlsconfig/`, `/bios/iscsi/`, `/bios/hpescalablepmem/`(관측) , `/bios/kmsconfig/`, `/bios/serverconfiglock/` | `Oem.Hpe.Links` | verified (biosdoc "Gen10 response body" + U34 관측) | |
| **Gen10 Plus (iLO5 2.33+)** OEM: `/redfish/v1/systems/1/bios/oem/hpe/{baseconfigs,boot,kmsconfig,mappings,serverconfiglock,tlsconfig,iscsi}/` (+ `boot/settings`, `serverconfiglock/settings`, `*/baseconfigs`) | `Oem.Hpe.Links` | verified (iLO5 changelog 2.33 "BIOS Redfish changes (GEN 10 to GEN 10 Plus)" + U46 관측) | |
| `…/Bios/Service` (Gen10) / `…/Bios/Oem/Hpe/Service` (Gen10 Plus) | HPE 현장 서비스 전용 — 클라이언트가 사용하지 말 것 | verified (biosdoc) | |
| 기본값 | Gen10: `GET …/Bios/BaseConfigs` -> `.BaseConfigs[].default`; Gen10 Plus: `GET …/Bios/Oem/Hpe/BaseConfigs`. 레지스트리 `DefaultValue` 방식은 Gen10 Plus/Gen11 만 유효 | verified (biosdoc) | |

- **iLO5 2.33 변경이 Gen10 에도 소급되는지는 문서가 "Gen10 / Gen10 Plus" 로 모델 세대로 구분**하고 있어 불확실 -> 하드코딩하지 말고 `GET /redfish/v1/systems/1/bios/?$select=Oem/Hpe/Links` 로 링크를 따라갈 것(biosdoc 권장).
- 쓰기: `PATCH /redfish/v1/Systems/1/Bios/Settings` body `{"Attributes":{"<이름>":"<값>"}}` -> 재부팅 후 적용. `ResetBios` 는 이전 2.10 부터 `ResetType` 불필요(빈 JSON `{}` 필요).
- 열거값은 숫자로 시작하지 않음(`Timeout10`, `Baud115200`).

## 5. 속성 파일 `bios_attributes.json` (763개)
- 출처 4종: 문서 3.19(AMD 540), 문서 3.09(Intel 401), 실캡처 U34(235, 값만), 실캡처 U46(276, 값만). 정의(type/allowed_values/read_only/description)는 문서에 있는 속성 699개만 있음(3.19 우선). 나머지 64개는 `type=null`(추측 금지).
- default=null (문서·캡처에 기본값 없음). 실캡처 현재값은 `sample_value` 에(기본값 아님).
- 이전(back) 결과는 3.09 Intel 401개만 수록 — 이번엔 3.19 AMD 항목과 실캡처 이름을 `seen_in` 으로 추가. 이전 README 의 "3.19 문서는 AMD 플랫폼이라 사용하지 않음" 은 맞았으나 합집합에 포함해 태그로 구분하는 쪽으로 변경. 사용자 모델은 모두 Intel 이므로 `seen_in` 에 `doc_ilo5_3.19_amd_sample` 만 있는 항목은 AMD 전용일 가능성이 높음.
- 모델별 정확 목록은 `GET /redfish/v1/Systems/1/Bios` -> `AttributeRegistry` -> `GET /redfish/v1/Registries/<이름>` 으로 장비에서.

## 6. 검증표
| 항목 | 상태 |
|---|---|
| Redfish 지원 / Bios, Bios/Settings URI | verified |
| Gen10 vs Gen10 Plus OEM URI 구조 차이 | verified |
| ResetBios / ChangePassword 액션 | verified |
| 최신 FW 3.21 | verified(Wikipedia + 포털 changelog) / 3.21 용 Bios 정의 문서 unavailable |
| 속성 목록(모델 전용) | unverified — U46(DL360 G10 Plus)만 이름 수준 verified |
| DL360 G10=U32, DL560/580 G10=U34, XL270d G10=U45 | unverified |
| U32/U45 레지스트리 내용 | unavailable |

## 7. 시도 횟수 / 내역
- 시도 1(공식 문서): servermanagementportal iLO5 v3.09/v3.19 Bios 정의(.md raw), biosdoc, ilo5_adaptation, ilo5_changelog.
- 시도 2(공개 코드·샘플): GitHub 코드 검색 -> `HewlettPackard/ilo-redfish-emulator` mockups (DL360 Gen10 Plus U46 등), `HewlettPackard/javascript-ilorest-library` test/mock_data (U34), stackhpc/proliantutils.
- 시도 3(재검증): U30/U32/U45/U54/U59 등 다른 ROM 코드 검색 -> 없음. Support Center 레지스트리는 EULA 라 링크만. 결론: U32/U45 unavailable.
- 이전 back/HPE 결과 재사용: DL360_G10 / DL560_G10_DL580_G10 / XL270d_G10 / DL360_G10_Plus 의 401개 Intel 샘플(출처 3.09 문서) — 동일 문서에서 재파싱해 일치 확인(401).
- Jev 생략.
