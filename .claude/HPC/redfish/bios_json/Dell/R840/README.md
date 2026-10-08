# Dell R840 (14G, iDRAC9)

- 프로토콜: **json** (Redfish JSON, iDRAC9). XML 전용 아님.
- 모델: R840
- 그룹화 근거: Intel 4소켓. R640/R740 가이드와 문장 약 360개 상이(Processor 3/4, NVDIMM/Persistent Memory 옵션 등) -> 별도 그룹.
- 최신 펌웨어 기준: iDRAC9 4.40.00.00 (14G 최종 라인) Attribute Registry 문서

## BIOS 속성 파일
- `bios_attributes.json`: 965개 (레지스트리 합집합/상한선. 플랫폼 AMD 전용 속성 포함 가능, 모델별 존재 여부 미확인, type/default 는 문서에 없어 일부 unknown)
- default: 문서/레지스트리에 기본값 없음 -> null (추측 안 함).
- 각 모델 개별 레지스트리는 문서로 확보 불가. 정확한 모델별 목록은 실기 GET /redfish/v1/Systems/System.Embedded.1/Bios/BiosRegistry 로 확인 필요.

## URI 및 검증 (jev + 이중검증)
| URI | 용도 | 시도 | jev 확률 | 독립 2차 출처 | 판정 |
|---|---|---|---|---|---|
| /redfish/v1/Systems/System.Embedded.1/Bios | 현재 BIOS 속성(GET) | 1차 0.74 (템플릿 <ComputerSystem-Id> 만 있어 실패) -> 재조사(증거에 Ansible idrac_bios 문서 추가) 0.99 | 0.99 | Dell OpenManage Ansible idrac_bios 문서 + iDRAC9 7.10 목업(R650) | verified |
| /redfish/v1/Systems/System.Embedded.1/Bios/Settings | 대기(pending) 설정 PATCH | 1차 | 0.92 | iDRAC9 7.10 목업(Settings 링크/Attributes{} ) | verified |
| /redfish/v1/Systems/System.Embedded.1/Bios/BiosRegistry | 속성 레지스트리 | 1차 | 0.89 | iDRAC9 7.10 목업 /redfish/v1/Registries/BiosAttributeRegistry.v1_0_0 Location | verified |
| 프로토콜 json | Redfish JSON | 1차 | 1.00 | Ansible 문서 / 목업 | verified |

기본 시스템 ID: System.Embedded.1 (Dell 공통). Registry 파일 컬렉션: /redfish/v1/Registries (BiosAttributeRegistry).

## 출처
- Attribute Registry PDF (iDRAC9, 2020 Rev.A00, FW<=4.40.00.00): https://dl.dell.com/topicspdf/idrac9-lifecycle-controller-v4x-series_Reference-Guide_en-us.pdf
- iDRAC9 Redfish API Guide 4.20.20.20: https://dl.dell.com/topicspdf/idrac9-lifecycle-controller-v4x-series_api-guide_en-us.pdf
- Ansible idrac_bios: https://docs.ansible.com/ansible/6/collections/dellemc/openmanage/idrac_bios_module.html
- iDRAC9 7.10.30.00 목업(R650, 레지스트리 구조/URI 교차확인): https://github.com/dell/iDRAC-Redfish-Scripting (iDRAC Redfish Mockup Clients/iDRAC_mockup_client_basic_config.zip)
- 모델 BIOS and UEFI Reference Guide: https://dl.dell.com/topicspdf/poweredge-r840_reference-guide2_en-us.pdf

## 구버전 차이
old_fw_diff.md 참조 (BIOS 추가 이력 일부만 확인, 삭제/변경은 확인불가)
