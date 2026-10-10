# HPE iLO 6 — BIOS (Redfish)

조사일 2026-10-10. Jev 판정 생략(API 키 없음) — 문서 2중 출처로 verified 판정.

## 1. 프로토콜: **json** (Redfish)
- 근거: iLO6 Redfish 서비스 문서(servermanagementportal iLO6 v1.79 Bios 정의 / adaptation / changelog) + biosdoc("Gen10 Plus and Gen11") + HPE ilo-redfish-emulator 의 iLO 6 v1.66 실캡처. verified.
- 최신 펌웨어: 포털 기준 **iLO 6 v1.79** (changelog 최상단; Wikipedia 표는 1.78 — 포털이 더 새로움). 관측 샘플은 iLO 6 1.66.

## 2. 해당 사용자 모델
| 모델 | ROM 패밀리 | 비고 |
|---|---|---|
| DL360 Gen11, DL380 Gen11 | **U54** (Support Center 에서 ML350/DL360/DL380 Gen11 단일 레지스트리 패키지) | 이전 조사 verified. 최신 레지스트리 패키지 3.00_08-20-2026 |
| DL560 Gen11 | **U59** (별도 패키지) | 이전 조사 |
- 공개 mock 은 **DL380a Gen11 (U58)** 뿐이라 사용자 모델(U54/U59)과 ROM 이 다름. 매핑 정정 없음(모두 iLO6 맞음).

## 3. 이전 실패(로그인 뒤) 재시도 결과
이전 back/HPE: iLO6 속성 파일 미생성(Support Center zip 이 EULA/로그인 뒤). 이번 공개 경로 재시도:
1. 시도 1 — 포털 iLO6 v1.79 Bios 정의(.md raw): 540개 정의를 받음. **그러나 iLO5 v3.19 문서와 이름·type·allowed_values·read_only 100% 동일(AMD 샘플 복사본)** -> iLO6 전용/Intel 전용이 아님. (v1.11 문서는 URL 404, 목록에 없음.)
2. 시도 2 — HPE 공개 GitHub `HewlettPackard/ilo-redfish-emulator` mockups/DL380a(iLO 6 v1.66, `BiosAttributeRegistryU58.v1_2_44`): **Gen11 Intel 실제 노출 속성 300개 이름+현재값** 확보 (type/allowed 없음). 사용자 U54/U59 가 아닌 U58 이라는 한계.
3. 시도 3 — U54/U59/ProcHyperthreading 키워드로 공개 GitHub 코드 검색 -> 이 repo(myrepo) 자체의 자료와 합성 mock 뿐, 레지스트리 원본 없음. 레지스트리 zip 은 EULA 라 **받지 않고 링크만**:
   - U54: https://support.hpe.com/connect/s/softwaredetails?collectionId=MTX-e269fb8869a04cb2 (= https://support.hpe.com/km/software/MTX-e269fb8869a04cb2)
   - U59 (DL560 Gen11): Support Center 에서 `RESTful API BIOS Schemas/Registries dl560` 검색
   - 받은 뒤 `RegistryStore/en/BiosAttributeRegistryU54.v1_*_en.json` 의 `RegistryEntries.Attributes[]` 에서 추출 (biosdoc 의 jq 예시)
- 결론: **U54/U59 전체 속성(타입/허용값/기본값)은 unavailable**. 이름 수준으로 U58 관측 + 문서 정의를 합쳐 `bios_attributes.json` 작성.
- 참고: 저장소 `testdata/hpe-dl360gen11/…/biosattributeregistryu54.v1_4_0.json` 은 도구 테스트용 **합성(mock) 파일**("BIOS Attribute Registry (mock)")이며 실제 U54 가 아님 — 사용하지 않음.

## 4. URI (iLO 6)
| URI | 상태 | 근거 |
|---|---|---|
| `/redfish/v1/Systems/1/Bios` (GET) | verified | 포털 Bios 정의 인스턴스 + biosdoc 예시(Gen11) + U58 실캡처 |
| `/redfish/v1/Systems/1/Bios/Settings` (GET/POST/PATCH) | verified | 동일 |
| `/redfish/v1/Systems/1/Bios/Actions/Bios.ResetBios` (POST `{}`) | verified(실캡처 target) | iLO6 1.66 캡처: `/redfish/v1/systems/1/bios/Actions/Bios.ResetBios/` (iLO5 의 `Bios/Settings/Actions/…` 에서 이동) |
| `/redfish/v1/Systems/1/Bios/Actions/Bios.ChangePassword` | verified(문서 액션명) / 캡처 target 은 `Bios.ChangePasswords` 표기 | 장비 Actions 값을 따를 것 |
| `/redfish/v1/Systems/1/Bios/Oem/Hpe/BaseConfigs` | verified | biosdoc("Gen10 Plus and Gen11") + 캡처 |
| `…/Bios/Oem/Hpe/Boot`, `/Mappings`, `/TlsConfig`, `/iScsi`, `/ServerConfigLock`, `/KmsConfig`, `/NvmeOf`(1.66 캡처) | verified | 캡처 `Oem.Hpe.Links` (`HpeBiosExt.v2_0_1`) |
| `…/Bios/Oem/Hpe/ScalablePmem` | 삭제 — iLO6 v1.05 에서 deprecated | ilo6_adaptation |
| `/redfish/v1/Registries` -> `BiosAttributeRegistryU58.v1_2_44` -> `/redfish/v1/registrystore/registries/en/biosattributeregistryu58.v1_2_44` (en/ja/zh) | verified(경로 패턴), 내용 unavailable | 캡처 + biosdoc(A55 예시) |
| `…/Bios/Oem/Hpe/Service` | HPE 서비스 전용 — 사용 금지 | biosdoc |
- 구 URI(`/bios/baseconfigs/`, `/bios/boot/` …)는 iLO6 에서 `…/bios/oem/hpe/…` 로 이동 (ilo6_adaptation "Bios Renames and Removals", 이유 "Redfish compliance").
- 기본값: 레지스트리 `DefaultValue` (biosdoc: Gen10 Plus/Gen11 에서 유효) 또는 `Bios/Oem/Hpe/BaseConfigs` 의 `BaseConfigs[].default`.
- 설정 변경: `PATCH /redfish/v1/Systems/1/Bios/Settings` body `{"Attributes":{…}}` -> 재부팅 후 적용.
- `@odata.type`: 문서 `Bios.v1_0_5`, 관측 `Bios.v1_0_4`.

## 5. 속성 파일 `bios_attributes.json` (647개)
- 출처: 문서 v1.79(540, 정의 있음) + U58 관측(300, 값만). 정의 있는 540개 + 관측 전용 107개(type=null). default=null.
- 문서 540개는 AMD 샘플이라 Intel Gen11 모델(DL360/380/560 Gen11)에 없는 속성(AMD 전용)이 섞여 있음. U58 관측과 문서 교집합은 193개, 관측 전용 107개(Intel Gen11 신규 속성 후보).
- 속성 이름은 iLO5 와 대부분 동일 계열이나 ROM 별로 다름(diff_vs_iLO5.md).

## 6. 검증표
| 항목 | 상태 |
|---|---|
| Redfish 지원, Bios/Settings URI | verified |
| Oem/Hpe 하위 이동 | verified |
| 최신 FW 1.79 | verified(포털) / Wikipedia 1.78 과 불일치 |
| 속성 이름(U58 Intel Gen11) | verified(실캡처 이름 수준) |
| 속성 type/허용값/기본값 (U54/U59) | **unavailable** (Support Center EULA) |
| 문서 540 = iLO5 3.19 복사 | verified(스크립트 비교) |

## 7. 시도 횟수
3회(위 3절). 공개 경로로 U54/U59 원본은 못 구함 -> 직접 조사(원문 JSON 직접 읽기: 포털 raw .md, GitHub raw mock)로 결론.
