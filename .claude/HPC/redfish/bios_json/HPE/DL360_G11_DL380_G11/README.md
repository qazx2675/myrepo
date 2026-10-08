# HPE DL360 Gen11 / DL380 Gen11

- 모델: DL360 Gen11, DL380 Gen11
- 프로토콜 판정: **json** (Redfish) - 근거: iLO 6 문서(Bios 리소스 Redfish)
- ROM 패밀리: ROM U54 (ML350/DL360/DL380 Gen11 공용, SPP)
- 최신 펌웨어: 확인된 U54 2.10_11-28-2023 (이후 미확인) / iLO 6 v1.79 문서

## URI 검증 (jev 이중검증)
| URI | 상태 | jev 확률 | 독립 출처 | 시도 |
|---|---|---|---|---|
| /redfish/v1/Systems/1/Bios | verified | 1차 yes 0.73 (미달), 재조사 yes 0.83 | Managing HPE BIOS resources 문서(Gen11 예시) | 재조사 |
| /redfish/v1/Systems/1/Bios/Settings | verified | 재조사 yes 0.83 | 위와 동일 | 재조사 |
| /redfish/v1/Systems/1/Bios/Oem/Hpe/BaseConfigs | verified | 0.93(공통 문서) | biosdoc | 1차 |
| /redfish/v1/Registries (+ /redfish/v1/registrystore/registries/en/biosattributeregistry<코드>.v...) | verified(경로 패턴, Gen11 A56 예시) | - | biosdoc | 1차 |

## BIOS 속성 파일
**bios_attributes.json 미생성(확보 불가)**: HPE 공개 문서의 iLO 6 Bios 정의(v1.73~1.79)는 AMD 플랫폼 레지스트리 샘플(540개, Intel 속성 ProcHyperthreading 등 없음)이라 Intel Gen11 에 부적합하고, 모델별 레지스트리 JSON(BiosAttributeRegistry<코드>)은 비공개/장비·Support Center 다운로드로만 제공됨. 실제 장비의 `/redfish/v1/Registries` 에서 확보해야 함. 추측으로 채우지 않음.

## 묶은 근거
U54 ROM 공용 -> 한 그룹.

## 참고 문서
- iLO 6 v1.79 Bios 정의: https://servermanagementportal.ext.hpe.com/docs/redfishservices/ilos/ilo6/ilo6_179/ilo6_bios_resourcedefns179
- Managing HPE BIOS resources: https://servermanagementportal.ext.hpe.com/docs/redfishservices/ilos/supplementdocuments/biosdoc/

## 구버전 차이
확인불가(구버전 iLO 6 문서·레지스트리 비공개).
