# Dell R750xs (15G, iDRAC9)

- 프로토콜: **json** (Redfish JSON, iDRAC9). XML 전용 아님.
- 모델: R750xs
- 그룹화 근거: 가이드 R750xs(A03)는 R750 과 문장 약 175개 상이(Memory Map Out, Kernel DMA Protection, Dell Controlled Turbo 등 옵션 구성 차이), R650 과 더 가까움(약 96개) -> 별도 그룹.
- 최신 펌웨어 기준: iDRAC9 7.10.30.00 레지스트리(R650 BIOS 1.12.1 을 15G Intel 대리 샘플로 사용)

## BIOS 속성 파일
- `bios_attributes.json`: 레지스트리 686개 중 R650 Bios 리소스 노출 440개(in_bios_resource=true). hidden/read_only 플래그 포함. 모델별(R750 계열) 실측 아님 -> 프록시
- default: 문서/레지스트리에 기본값 없음 -> null (추측 안 함).
- R750/R750xa/R750xs 개별 레지스트리는 확보 불가. 슬롯(SlotDisablement/SlotBif)·GPU 관련 속성은 모델마다 다를 수 있음.

## URI 및 검증 (jev + 이중검증)
| URI | 용도 | 시도 | jev 확률 | 독립 2차 출처 | 판정 |
|---|---|---|---|---|---|
| /redfish/v1/Systems/System.Embedded.1/Bios | 현재 BIOS 속성(GET) | 1차 | 0.89 | Dell OpenManage Ansible idrac_bios 문서 | verified |
| /redfish/v1/Systems/System.Embedded.1/Bios/Settings | 대기(pending) 설정 PATCH | 1차 | 1.00 | iDRAC9 4.20.20.20 Redfish API Guide(TargetSettingsURI) | verified |
| /redfish/v1/Systems/System.Embedded.1/Bios/BiosRegistry | 속성 레지스트리 | 1차 | 0.92 | iDRAC9 4.20.20.20 Redfish API Guide(Attribute Registry URL) | verified |
| 프로토콜 json | Redfish JSON | 1차 | 1.00 | API Guide | verified |
(모델 직접 문서 대신 동일 iDRAC9 Redfish 구현의 R650 목업 + 가이드로 확인. R750 계열 실기 응답은 미확인이나 URI 구조는 iDRAC9 공통.)

기본 시스템 ID: System.Embedded.1 (Dell 공통). Registry 파일 컬렉션: /redfish/v1/Registries (BiosAttributeRegistry).

## 출처
- Attribute Registry PDF (iDRAC9, 2020 Rev.A00, FW<=4.40.00.00): https://dl.dell.com/topicspdf/idrac9-lifecycle-controller-v4x-series_Reference-Guide_en-us.pdf
- iDRAC9 Redfish API Guide 4.20.20.20: https://dl.dell.com/topicspdf/idrac9-lifecycle-controller-v4x-series_api-guide_en-us.pdf
- Ansible idrac_bios: https://docs.ansible.com/ansible/6/collections/dellemc/openmanage/idrac_bios_module.html
- iDRAC9 7.10.30.00 목업(R650, 레지스트리 구조/URI 교차확인): https://github.com/dell/iDRAC-Redfish-Scripting (iDRAC Redfish Mockup Clients/iDRAC_mockup_client_basic_config.zip)
- 모델 BIOS and UEFI Reference Guide: https://dl.dell.com/topicspdf/poweredge-r750xs_reference-guide2_en-us.pdf

## 구버전 차이
확인불가 (15G 구버전 레지스트리 확보 못함). 차이 없음으로 단정하지 않음.
