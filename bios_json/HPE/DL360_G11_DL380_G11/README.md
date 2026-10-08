# HPE DL360 Gen11 / DL380 Gen11 (ML350 Gen11 포함 ROM 패밀리)

- 모델: DL360 Gen11, DL380 Gen11 (같은 레지스트리 패키지에 ML350 Gen11 도 포함)
- 프로토콜 판정: **json** (Redfish) - 근거: iLO 6 문서(Bios 리소스 Redfish)
- ROM 패밀리: **U54** (HPE Support Center 에서 "ML350/DL360/DL380 Gen11 (U54)" 단일 레지스트리 패키지로 확인)
- 최신 레지스트리 패키지: **3.00_08-20-2026** (2026-08-28 릴리스, 재조사 시점 최신) / 최신 iLO 6 문서 v1.79
- 재조사 결과: **bios_attributes.json 여전히 미생성(공개 URL 로 받을 수 있는 Gen11 Intel U54 레지스트리 원본 없음)**. 단, 원본의 정확한 위치(아래)는 특정함.

## URI 검증 (jev 이중검증)
| URI | 상태 | jev 확률 | 독립 출처 | 시도 |
|---|---|---|---|---|
| /redfish/v1/Systems/1/Bios | verified | 1차 yes 0.73 (미달), 재조사 yes 0.83, 2차 재조사 yes 0.94 | Managing HPE BIOS resources 문서(Gen11 예시) + Support Center U54 레지스트리 패키지 설명 | 재조사 x2 |
| /redfish/v1/Systems/1/Bios/Settings | verified | 재조사 yes 0.83 | 위와 동일 | 재조사 |
| /redfish/v1/Systems/1/Bios/Oem/Hpe/BaseConfigs | verified | 0.93(공통 문서) | biosdoc ("Gen10 Plus and Gen11" 절) | 1차 |
| /redfish/v1/Registries (+ /redfish/v1/registrystore/registries/en/biosattributeregistryU54.v... 패턴) | verified(경로 패턴) | 0.94 | biosdoc(A55 예시), Support Center | 재조사 |

## BIOS 속성 파일 (확보 불가 - 재조사 후에도 동일)
`bios_attributes.json` 을 만들지 않음. 추측으로 채우지 않음.

### 확보 가능한 유일한 정식 원본 (직접 받아야 함)
- HPE Support Center "RESTful API BIOS Schemas/Registries - HPE ML350/DL360/DL380 Gen11 (U54) Servers", 3.00_08-20-2026 (유틸리티-도구, OS Independent, zip)
  - https://support.hpe.com/connect/s/softwaredetails?collectionId=MTX-e269fb8869a04cb2 (= https://support.hpe.com/km/software/MTX-e269fb8869a04cb2)
  - 페이지는 로그인 없이 열리지만 zip 은 "Download Software" 버튼(JS) + EULA 동의가 필요하고, 파일 다운로드/약관 동의는 사용자 승인 사항이라 이번 조사에서 받지 않음.
  - 받은 뒤 `RegistryStore/en/BiosAttributeRegistryU54.v1_*_en.json` 의 `RegistryEntries.Attributes[]` 에서 AttributeName/Type/Value/DefaultValue/ReadOnly 추출(HPE biosdoc 의 jq 예시와 동일 방법).
- 또는 실제 장비: `GET /redfish/v1/Systems/1/Bios` 의 `AttributeRegistry` -> `/redfish/v1/Registries` 의 해당 파일(gzip JSON).

### 이번에 시도한 출처 (모두 U54/Intel Gen11 전체 속성 목록 불가)
1. servermanagementportal iLO 6 Bios 정의 v1.73~v1.79 (llms.txt 기준 공개본은 v1.79 뿐, 이전 버전 .md 는 404): `Bios.v1_0_5` AMD 샘플 540개(ProcHyperthreading 없음) -> Intel 부적합.
2. servermanagementportal iLO 6 v1.11 Bios 정의 (`ilo6_111`, 유일하게 Intel 샘플 포함): `Bios.v1_0_0` 속성 401개이나 **iLO 5 Intel 샘플(Gen10/Gen10 Plus 폴더의 401개)과 이름이 100% 동일** -> iLO 5 문서 복사본으로 판단, Gen11 U54 속성이라는 근거 없음. 사용하지 않음.
3. hewlettpackard.github.io/ilo-rest-api-docs/ilo6 (구 iLO 6 문서): Bios.v1_0_5 AMD 샘플(545 항목), ProcHyperthreading 없음.
4. GitHub HewlettPackard 조직(ilo-rest-api-docs, python-redfish-utility, python-ilorest-library 등) 트리 확인: 레지스트리/Bios 샘플 JSON 없음(문서 소스 .md 와 iLOrest 코드뿐). canopybmc/canopybmc #88 등은 U54 속성 데이터 없음.
5. 웹 검색: `BiosAttributeRegistryU54` / `U59` 문자열을 포함한 공개 문서·GitHub 파일 없음(검색 엔진 색인 없음).
6. HPE Support Center 레지스트리 패키지(위): 원본 확인 완료, 다운로드는 미수행.

## 묶은 근거
Support Center 가 ML350/DL360/DL380 Gen11 을 하나의 U54 레지스트리 패키지로 제공(jev: U54 공용 vs DL560(U59) 분리 yes 1.0). 따라서 그룹 유지. DL560 Gen11 은 U59 별도 패키지라 별도 폴더 유지.

## 참고 문서
- iLO 6 v1.79 Bios 정의: https://servermanagementportal.ext.hpe.com/docs/redfishservices/ilos/ilo6/ilo6_179/ilo6_bios_resourcedefns179
- iLO 6 v1.11 Bios 정의(iLO 5 샘플 복사본): https://servermanagementportal.ext.hpe.com/docs/redfishservices/ilos/ilo6/ilo6_111/ilo6_bios_resourcedefns111
- Managing HPE BIOS resources: https://servermanagementportal.ext.hpe.com/docs/redfishservices/ilos/supplementdocuments/biosdoc/
- U54 레지스트리 패키지(Support Center): https://support.hpe.com/km/software/MTX-e269fb8869a04cb2

## 구버전 차이
확인불가. Support Center 에 U54 레지스트리 패키지 31개 버전(1.22_01-18-2023 ~ 3.00_08-20-2026)이 있어 zip 을 받으면 버전 간 비교가 가능하나 이번에는 받지 못함.
