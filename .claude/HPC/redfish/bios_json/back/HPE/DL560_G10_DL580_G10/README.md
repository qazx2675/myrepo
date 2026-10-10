# HPE DL560 Gen10 / DL580 Gen10

- 모델: DL560 Gen10, DL580 Gen10
- 프로토콜 판정: **json** (Redfish) - 근거: iLO 5 문서(Bios 리소스 Redfish)
- ROM 패밀리: ROM U34 (SPP: DL560 Gen10/DL580 Gen10 (U34) 공용 ROM)
- 최신 펌웨어: 최신 ROM 버전 미확인 / iLO 5 v3.19

## URI 검증 (jev 이중검증)
| URI | 상태 | jev 확률 | 독립 출처 | 시도 |
|---|---|---|---|---|
| /redfish/v1/Systems/1/Bios | verified | yes 0.89 | iLO5 v3.09 문서 + Managing HPE BIOS resources 문서 | 1차 |
| /redfish/v1/Systems/1/Bios/Settings (GET/PATCH) | verified | yes 0.89 | 위와 동일 | 1차 |
| /redfish/v1/Systems/1/Bios/BaseConfigs (default / default.user) | verified | yes 0.89 | Managing HPE BIOS resources 문서 | 1차 |
| /redfish/v1/Registries (BiosAttributeRegistry<ROM코드>.v...) | verified(경로만, 파일명은 장비 조회 필요) | - | biosdoc 문서 | 1차 |

## BIOS 속성 파일
`bios_attributes.json` (401개). 출처는 iLO 5 v3.09 Bios 정의 문서의 Intel 플랫폼 샘플이며 **이 모델 전용 레지스트리라는 보장은 없음**(범위 주의). 기본값은 문서에 없어 null. 모델별 정확한 목록은 장비의 `GET /redfish/v1/Systems/1/Bios` 의 `AttributeRegistry` 값 -> `/redfish/v1/Registries` 에서 확인해야 함.
- iLO 5 문서 273~309 사이 속성 이름 집합 동일(377 매칭 속성, 값 변경 없음). 3.19 문서는 AMD 플랫폼 샘플(540개, ProcHyperthreading 없음)로 바뀌어 Intel 모델에 부적합하여 사용하지 않음.

## 묶은 근거
같은 ROM U34 공유 -> 동일 레지스트리로 묶음.

## 참고 문서
- iLO 5 v3.19 Bios 정의: https://servermanagementportal.ext.hpe.com/docs/redfishservices/ilos/ilo5/ilo5_319/ilo5_bios_resourcedefns319
- iLO 5 v3.09 Bios 정의(독립 출처): https://servermanagementportal.ext.hpe.com/docs/redfishservices/ilos/ilo5/ilo5_309/ilo5_bios_resourcedefns309
- Managing HPE BIOS resources: https://servermanagementportal.ext.hpe.com/docs/redfishservices/ilos/supplementdocuments/biosdoc/

## 구버전 차이
문서 수준에서 iLO 5 v2.73~v3.09 Bios 정의의 속성 이름·허용값 동일(차이 없음). 실제 ROM 버전별 레지스트리 차이는 레지스트리 파일 비공개로 **확인불가**(HPE: Support Center 의 레지스트리 zip 을 직접 비교해야 함).
