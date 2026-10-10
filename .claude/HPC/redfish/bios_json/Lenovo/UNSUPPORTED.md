# Lenovo - 지원 안 하는 버전·모델 (BIOS Redfish 관점)

조사일 2026-10-10.

## 사용자 보유 모델
**Redfish(JSON) BIOS 를 지원하지 않는 사용자 모델은 없다.** SR630, SR650, SD530, SR630 V2 (XCC) / SR645 V3, SR675 V3, SD650 V3 (XCC2) 모두 `/redfish/v1/Systems/1/Bios` + `/Bios/Pending`. 근거: Lenovo 공식 python-redfish-lenovo `products_supported.txt` 의 BIOS settings 스크립트(get_all_bios_attributes.py, get_bios_attribute.py, set_bios_attribute.py, get_bios_attribute_metadata.py)가 Purley / V2(Whitley) / V3(EGS) / V3(Genoa) / V4 열 모두 Yes. 단 Purley 에서 Redfish BIOS 가 지원되는 **최소 XCC 펌웨어 버전은 확인 못 함** (unverified; 구형 XCC 펌웨어에서는 `GET /redfish/v1/Systems/1/Bios` 가 404 일 수 있으므로 장비에서 확인).

## 해당 관리망에서 지원하지 않는 기능/URI (모든 Lenovo 버전)
| 기능 | 상태 | 근거 |
|---|---|---|
| `/redfish/v1/Systems/1/Bios/Settings` | **없음** (Lenovo 는 `Bios/Pending`) | REST 가이드 3종 |
| XML API / SNMP 기반 BIOS 설정 | 해당 없음 (Lenovo XCC 는 Redfish) | - |
| `Systems/Self` 경로 | 사용자 모델 해당 없음 (SR635/SR655 AMD 1세대 계열) | python-redfish-lenovo lenovo_set_bios_boot_order.py |
| AMT 시험 옵션 PATCH | XCC/XCC2 문서에만 존재, XCC3 문서 없음 | REST 가이드 diff |

## 사용자 모델이 아닌, 참고로 확인한 비-XCC 계열 (BIOS 관련 차이)
- ThinkSystem SR635/SR655 (AMD 1세대): `Systems/Self`, `Q00999_Boot_Option_Priorities` 형 속성명 사용(공식 스크립트 분기). 사용자 모델 아님.
- System x(M5 이하, IMM2): Redfish BIOS 해당 없음 (조사 범위 밖, 사용자 모델 아님).

## 버전 낮음으로 인한 제한
- XCC (SR630/SR650/SD530, Skylake 구성): 값 표기가 `Enable`/`Disable` (Cascade Lake 이후 `Enabled`/`Disabled`). 스크립트는 두 표기를 모두 처리해야 함 (`XCC/old_fw_diff.md`).
