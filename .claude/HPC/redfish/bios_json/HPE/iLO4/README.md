# HPE iLO 4 — BIOS (Redfish)

조사일 2026-10-10. Jev 판정은 생략(API 키 없음) — "공식 문서 + 독립된 두 번째 출처" 로만 verified 판정.

## 1. 프로토콜 판정: **json** (Redfish) — 단 iLO 4 **2.30 이상**

| 펌웨어 | 프로토콜 | 근거 |
|---|---|---|
| iLO 4 2.00 ~ 2.29 | **json 이지만 Redfish 아님** — 레거시 HPE RESTful `/rest/v1/...` 만 (`/redfish/v1` 없음) | iLO4 레퍼런스 "The iLO RESTful API was first released with iLO 4 2.00 on HPE Gen9 servers", python-redfish-library 문서 "Legacy Rest API available starting in iLO 4 2.00" |
| iLO 4 2.30 이상 (최신 2.82) | **json (Redfish 1.0 적합)**, `/redfish/v1` 과 `/rest/v1` 미러. 요청에 `OData-Version: 4.0` 헤더를 주면 Redfish 전용 속성만 반환 | iLO4 레퍼런스 "iLO 4 2.30 is Redfish 1.0 conformant … Mirroring the resource model at both /redfish/v1/ and /rest/v1", ilorest 문서 "iLO 4 is Redfish conformant starting with HPE iLO 4 2.30" |
| XML | iLO4 에는 별도로 RIBCL(XML) 이 있으나 BIOS 속성은 Redfish/REST(UEFI BIOS, Gen9) 로 다룬다. 이 조사에서는 XML BIOS 경로는 다루지 않음 | - |
| none | **DL560 Gen8: 아래 3절 / UNSUPPORTED.md** | |

## 2. 해당 사용자 모델

| 모델 | iLO | 판정 | 비고 |
|---|---|---|---|
| DL360 Gen9 | iLO 4 | json (2.30+) | UEFI BIOS(Gen9). ROM 패밀리 P89 (fixture/이전 조사; 모델-ROM 대응은 unverified) |
| XL170r Gen9 | iLO 4 | json (2.30+) | ROM U14 (이전 조사 추정, unverified) |
| XL250 Gen9 | iLO 4 | json (2.30+) | ROM U13 계열 추정 (unverified) |
| XL270d Gen9 | iLO 4 | json (2.30+) | ROM U25 추정 (unverified) |
| **DL560 Gen8** | iLO 4 (Gen8) | **BIOS Redfish 미지원 (UNSUPPORTED)** | 3절 |

## 3. DL560 Gen8 — 판정 **UNSUPPORTED** (BIOS 속성 Redfish 불가)
- 공식 1: iLO4 RESTful API 레퍼런스(`hewlettpackard.github.io/ilo-rest-api-docs/ilo4/`) "The RESTful API for HPE iLO is available on **ProLiant Gen9 servers** running iLO 4 2.00 or later" 및 "first released with iLO 4 2.00 on HPE Gen9 servers". Gen8 은 문서 어디에도 대상으로 나오지 않는다.
- 공식 2(독립): iLOrest 설치 문서 `servermanagementportal.ext.hpe.com/docs/redfishclients/ilorest-userguide/installation` "HPE **Gen9 or greater** servers … can be managed by iLOrest" (iLOrest 가 iLO4 BIOS 를 다루는 도구).
- 보조: 레퍼런스의 `HpBios` 는 UEFI System Utilities(Gen9 UEFI BIOS) 설정이며 Gen8 은 RBSU(레거시 BIOS) 라서 대응 리소스가 없는 것으로 판단(배경 지식, 문서 직접 명시는 못 찾음). 3자 위키(shamimur/hp-proliant-sdk) 에 "UEFI BIOS configuration … not available on Gen8".
- 결론: **verified (문서 2건에서 Gen9 이상으로 한정)**. 단 "Gen8 에 `/redfish/v1/Systems/1/Bios` 가 404" 를 직접 관측한 것은 아님 -> 실장비 확인법: `curl -k -u <USER>:<PASS> https://<ILO>/redfish/v1/Systems/1/Bios` (404/미존재면 미지원). Gen8 BIOS 는 RBSU/`conrep`(OS) 또는 RIBCL XML 로 처리.
- 이전 back/HPE 조사의 "Gen8 json(Redfish 서비스만)" 은 "iLO4 2.30+ 면 `/redfish/v1` 이 뜬다" 는 추정이었고, 이번에 공식 문서 기준으로 정정: DL560 Gen8 은 BIOS 용도로는 **none** 으로 취급.

## 4. 최신 펌웨어 / ROM
- iLO 4: **2.82** (2023-03-02, 현재 EOL; Wikipedia HPE iLO 표 + HPE 커뮤니티 안내). 레퍼런스 문서 기준은 2.30 예제. unverified(Support Center 직접 확인 못함 — 로그인/EULA 필요).
- System ROM(Gen9): DL360 G9 P89 (최신 2.92, 2021-11 — 커뮤니티 근거), XL170r G9 U14, XL250 G9 U13 계열, XL270d G9 U25 — **모델-ROM 대응은 모두 unverified** (HPE Support Center 로그인 뒤).
- 레지스트리 파일명 예: `HpBiosAttributeRegistryP89.1.1.00` -> 펌웨어 갱신마다 번호 증가 (예 `P89.1.1.30` -> `1.1.60`).

## 5. URI (iLO 4)

| URI | 용도 | 상태 | 근거(2중) |
|---|---|---|---|
| `/redfish/v1/` | 서비스 루트 (2.30+) | verified | iLO4 레퍼런스 + ilorest 문서 |
| `/redfish/v1/Systems/1/Bios` (대소문자 무관, 문서 표기 `/redfish/v1/systems/{item}/bios`) | 현재 BIOS 속성 (읽기 전용). 속성은 **최상위**에 name:value | **verified** | ① iLO4 레퍼런스 `HpBios` "Resource Instances: https://{iLO}/redfish/v1/systems/{item}/bios (read only current settings)" ② HPE iLO5 adaptation 문서 iLO4 열(`HpBios.1.2.0`) + ilorest `--redfish` 플래그가 iLO4 에서 Redfish 모드 필요 |
| `/redfish/v1/Systems/1/Bios/Settings` | 대기(pending) 설정, GET/PATCH/PUT, 재부팅(POST) 후 적용 | **verified** | ① 동일 레퍼런스 "…/bios/settings (PATCHable Pending settings)" ② proliantutils 샘플(`GET_BIOS_PENDING_SETTINGS`, `/rest/v1/systems/1/bios/Settings`) |
| `/rest/v1/systems/1/bios`, `/rest/v1/systems/1/bios/Settings` | 레거시 REST (2.00~, 2.30+ 에서도 병행) | verified | 레퍼런스 예제 + proliantutils 샘플 |
| `/redfish/v1/Systems/1/Bios/BaseConfigs` | 기본값 (`BaseConfig:"default"` 를 Settings 에 PUT/PATCH 하면 기본값 복원) | verified (레퍼런스 `HpBaseConfigs` 인스턴스 목록 + proliantutils `GET_BASE_CONFIG`) | |
| `/redfish/v1/Systems/1/Bios/Boot`, `/Bios/Boot/Settings`, `/Bios/Boot/BaseConfigs` | UEFI 부트 순서 (`HpServerBootSettings`) | verified (레퍼런스) | |
| `/redfish/v1/Systems/1/Bios/Mappings` | PCI 매핑 (`HpBiosMapping`) | verified | |
| `/redfish/v1/Systems/1/Bios/iScsi`, `/iScsi/Settings`, `/iScsi/BaseConfigs` | UEFI iSCSI | verified | |
| `/redfish/v1/Registries` | 레지스트리 목록 (BIOS 속성 레지스트리 `HpBiosAttributeRegistry<ROM>.x.y.z`, gzip 응답 가능) | verified(경로) / 파일 내용 unavailable | 레퍼런스 `https://{iLO}/redfish/v1/Registries` + proliantutils(`AttributeRegistry: HpBiosAttributeRegistryP89.1.1.00`) |
| 액션 `ResetBios` / `ChangePassword` | **iLO4 에는 없음** (iLO5 에서 신규) | verified | iLO5 adaptation "New Actions on BIOS resources (NEW)" |

- 시스템 ID 는 하드코딩하지 말고 ComputerSystem 의 `Oem.Hp.links.BIOS` 링크를 따르라고 레퍼런스가 권고(실제 값은 `Systems/1`).
- BIOS 관리자 암호가 설정된 장비에 쓰기: `X-HPRESTFULAPI-AuthToken` 헤더 = BIOS 암호의 SHA-256 대문자 hex (ilorest `--biospassword` 가 iLO4 전용 플래그인 것과 일치).
- 쓰기 흐름: Settings 에 PATCH -> 재부팅 시 UEFI POST 에서 적용 -> 현재 리소스의 `SettingsResult` 로 결과 확인.

## 6. 속성 파일 `bios_attributes.json`
- **237 항목**: iLO4 레퍼런스 `HpBios Attributes` 234개(+ `SanitizeProc{1,2}Dimm{N}` 패턴 1건 포함) + proliantutils fixture 에만 있는 `SecureBoot`, `TcmOperation`, `TcmVisibility` 3개(type=null).
- 필드: type(Enumeration 205/String 22/Integer 3/Password 4), allowed_values, read_only(문서의 "read only"/"PATCHable" 구분: `SecureBootStatus`, `TpmState`, `TpmType` 만 read-only), default=null(문서에 없음), `sample_default`(fixture 153개 기본값 샘플, ROM 불명), `sample_value`(I36 ROM 현재값 샘플).
- 이전(back) 결과의 235와 다른 이유: 이번엔 HTML 을 직접 받아 스크립트 파싱 -> 문서 정의 234(패턴 포함) 확인, 요약 도구 전사 오류 배제.
- **한계(unverified)**: 모델/ROM 별 레지스트리(P89/U13/U14/U25)는 Support Center(로그인/EULA)라 받지 않음. 모델마다 실제 노출 속성은 다름(NVDIMM/GPU/PCIe 관련은 모델 의존). 링크만 남김: HPE Support Center "RESTful API BIOS Schemas/Registries" 검색(예: `RESTful API BIOS Schemas/Registries dl360`) — https://support.hpe.com 
- 실장비에서 얻는 법: `GET /redfish/v1/Systems/1/Bios` 의 `AttributeRegistry` 값 -> `GET /redfish/v1/Registries` -> 해당 레지스트리 파일(gzip JSON) 의 `RegistryEntries.Attributes[]`(AttributeName/Type/Value/DefaultValue/ReadOnly). 기본값: `GET /redfish/v1/Systems/1/Bios/BaseConfigs` 의 `BaseConfigs[].default`.

## 7. 검증표
| 항목 | 상태 |
|---|---|
| iLO4 Redfish = 2.30+ (2.00~2.29 는 /rest/v1 만) | verified |
| `/redfish/v1/Systems/1/Bios`, `/Bios/Settings` | **verified** (이전 unverified 에서 상향: 레퍼런스에 `/redfish/v1/systems/{item}/bios[/settings]` 명시 + 독립 출처) |
| Bios/BaseConfigs, Boot, Mappings, iScsi | verified (레퍼런스) |
| Registries 경로 | verified, 내용 unavailable |
| 속성 목록(모델별) | unverified (문서 합집합) |
| DL560 Gen8 Bios 미지원 | verified-by-docs (실장비 404 미관측) |
| 최신 펌웨어 2.82 / 모델-ROM 대응 | unverified |

## 8. 시도 횟수 / 내역
- 시도 1(공식 문서): iLO4 레퍼런스 HTML 직접 수신·파싱(HpBios, HpBios Attributes, HpBaseConfigs 절), ilo5_adaptation(iLO4 vs iLO5 표).
- 시도 2(다른 문서군/공개 코드): ilorest 사용자 가이드(bioscommands/globalcommands/installation), python-redfish-library 문서, stackhpc/proliantutils `ris_sample_outputs.py`(iLO4 Bios/BaseConfigs/Registry 이름 샘플), Wikipedia/HPE 커뮤니티(최신 버전).
- 시도 3(재검증): 레지스트리 파일 공개 미러 검색(GitHub 코드 검색 `HpBiosAttributeRegistryP89` — fixture 와 PowerShell SDK 뿐), HPE 공개 repo 트리(API 403이라 raw 만) -> 레지스트리 원본 없음 -> unavailable 로 결론.
- Jev 생략.
