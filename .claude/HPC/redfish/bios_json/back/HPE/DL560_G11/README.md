# HPE DL560 Gen11

- 모델: DL560 Gen11
- 프로토콜 판정: **json** (Redfish) - 근거: iLO 6 문서(Bios 리소스 Redfish)
- ROM 패밀리: **U59** (HPE Support Center 에 DL560 Gen11 (U59) 전용 레지스트리 패키지 존재 -> DL360/DL380 Gen11 (U54) 과 별도 그룹 유지)
- 최신 레지스트리 패키지: **3.00_08-20-2026** (재조사 시점 최신) / 최신 iLO 6 문서 v1.79
- 재조사 결과: **bios_attributes.json 여전히 미생성(공개 URL 로 받을 수 있는 U59 레지스트리 원본 없음)**. 원본의 정확한 위치는 특정함.

## URI 검증 (jev 이중검증)
| URI | 상태 | jev 확률 | 독립 출처 | 시도 |
|---|---|---|---|---|
| /redfish/v1/Systems/1/Bios | verified | 1차 yes 0.73 (미달), 재조사 yes 0.83, 2차 재조사 yes 0.94 | Managing HPE BIOS resources 문서(Gen11 예시) + Support Center 레지스트리 패키지 설명 | 재조사 x2 |
| /redfish/v1/Systems/1/Bios/Settings | verified | 재조사 yes 0.83 | 위와 동일 | 재조사 |
| /redfish/v1/Systems/1/Bios/Oem/Hpe/BaseConfigs | verified | 0.93(공통 문서) | biosdoc ("Gen10 Plus and Gen11" 절) | 1차 |
| /redfish/v1/Registries (+ /redfish/v1/registrystore/registries/en/biosattributeregistryU59.v... 패턴) | verified(경로 패턴) | 0.94 | biosdoc(A55 예시), Support Center | 재조사 |

## BIOS 속성 파일 (확보 불가 - 재조사 후에도 동일)
`bios_attributes.json` 을 만들지 않음. 추측으로 채우지 않음.

### 확보 가능한 유일한 정식 원본 (직접 받아야 함)
- HPE Support Center "RESTful API BIOS Schemas/Registries - HPE ProLiant DL560 Gen11 (U59) Servers", 3.00_08-20-2026 (유틸리티-도구, OS Independent, zip)
  - https://support.hpe.com/km/software/MTX-14a1b1423ce3475a
  - zip 은 "Download Software" 버튼(JS) + EULA 동의 필요, 파일 다운로드/약관 동의는 사용자 승인 사항이라 이번 조사에서 받지 않음.
  - 받은 뒤 `RegistryStore/en/BiosAttributeRegistryU59.v1_*_en.json` 의 `RegistryEntries.Attributes[]` 에서 AttributeName/Type/Value/DefaultValue/ReadOnly 추출.
- 또는 실제 장비: `GET /redfish/v1/Systems/1/Bios` 의 `AttributeRegistry` -> `/redfish/v1/Registries`.

### 이번에 시도한 출처 (모두 U59/Intel Gen11 전체 속성 목록 불가)
1. servermanagementportal iLO 6 Bios 정의 v1.73~v1.79: AMD 샘플 540개(ProcHyperthreading 없음) -> Intel 부적합. 이전 버전은 404 (공개본은 v1.79 뿐, llms.txt 확인).
2. iLO 6 v1.11 Bios 정의: 속성 401개이나 iLO 5 Intel 샘플과 이름 100% 동일(복사본) -> Gen11 근거 없음, 미사용.
3. hewlettpackard.github.io/ilo-rest-api-docs/ilo6 : AMD 샘플(545 항목).
4. GitHub HewlettPackard 조직 저장소 트리: 레지스트리/Bios 샘플 JSON 없음.
5. 웹 검색: `BiosAttributeRegistryU59` 포함 공개 문서·GitHub 파일 없음.
6. HPE Support Center U59 레지스트리 패키지(위): 원본 확인 완료, 다운로드는 미수행.

## 묶은 근거
단독 ROM 패밀리(U59). Support Center 에서 DL360/DL380/ML350(U54) 과 별도 패키지로 제공되므로 재분리하지 않고 현 구성 유지. (속성 일치 여부는 두 zip 을 받아 비교해야 확정)

## 참고 문서
- iLO 6 v1.79 Bios 정의: https://servermanagementportal.ext.hpe.com/docs/redfishservices/ilos/ilo6/ilo6_179/ilo6_bios_resourcedefns179
- Managing HPE BIOS resources: https://servermanagementportal.ext.hpe.com/docs/redfishservices/ilos/supplementdocuments/biosdoc/
- U59 레지스트리 패키지(Support Center): https://support.hpe.com/km/software/MTX-14a1b1423ce3475a

## 구버전 차이
확인불가(레지스트리 zip 미확보). Support Center 에 U59 패키지 30개 버전이 있어 받으면 비교 가능.
