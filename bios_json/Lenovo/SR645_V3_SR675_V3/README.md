# Lenovo SR645 V3, SR675 V3

## 프로토콜 판정: json (Redfish)
- 근거: Lenovo XCC REST API 레퍼런스가 Redfish JSON 엔드포인트를 문서화 (https://pubs.lenovo.com/xcc2-restapi/resource_for_bios_get). XML API 아님.
- 관리 컨트롤러: XCC2, 플랫폼: AMD EPYC 4th Gen
- 이 그룹 모델: SR645 V3, SR675 V3

## URI 및 검증 상태
| 용도 | URI | 검증 | 시도 |
|---|---|---|---|
| BIOS 현재 속성 | /redfish/v1/Systems/1/Bios | verified (jev yes 1.00 + 독립 문서 xcc/xcc2/xcc3 REST API 3종에서 동일 확인) | 1차 |
| BIOS 설정(Pending) | /redfish/v1/Systems/1/Bios/Pending (Bios 의 @Redfish.Settings.SettingsObject, ApplyTime=OnReset) | verified (jev 1.00; `/Bios/Settings` 는 jev no 1.00 — Lenovo 는 Settings 가 아니라 Pending) | 1차 |
| 속성 레지스트리 | /redfish/v1/schemas/registries/BiosAttributeRegistry.1.0.0.json (Bios.AttributeRegistry = BiosAttributeRegistry.1.0.0) | verified (jev 0.99 + 위 3종 문서) | 1차 |
| 기타 | /redfish/v1/Systems/1/Bios/Actions/Bios.ResetBios, Bios.ChangePassword | 문서 확인(참고) | - |

주의: 위 URI 는 XCC 세대 문서(공통 구조)에서 확인한 것이며, SR645 V3, SR675 V3 개별 모델 전용 문서는 아니다. 레지스트리의 SupportedSystems 는 문서 예시(SR650 등)만 나온다.

## 최신 펌웨어
unknown — 모델별 최신 XCC/UEFI 버전을 문서에서 확정하지 못함(Lenovo 가 모델별 레지스트리를 공개 문서로 제공하지 않음).

## bios_attributes.json 미생성 사유
Lenovo 는 BIOS 속성 전체(Redfish AttributeName 포함)를 공개 문서로 게시하지 않는다. 레지스트리 JSON 은 BMC 자체(`GET /redfish/v1/schemas/registries/BiosAttributeRegistry.1.0.0.json`)에서만 얻을 수 있고, 문서의 예시는 일부 발췌뿐이다. UEFI 설정 문서(https://pubs.lenovo.com/uefi_amd_4th/operating_modes)는 메뉴 라벨/옵션/기본값만 있고 Redfish 속성명이 없어, 이름을 추측해 채우지 않았다. 확인된 속성명 예: SystemRecovery_POSTWatchdogTimer, SecureBootConfiguration_SecureBootMode, Memory_MemorySpeed (패턴 `<메뉴>_<설정>`).
→ 실제 장비/BMC 에서 레지스트리를 받아 채울 것 (문서 기반 조사의 한계).

## 그룹 묶음 근거
동일 XCC 세대·동일 플랫폼 계열(AMD EPYC 4th Gen) 이라 같은 UEFI 설정 문서 계열을 쓴다. 단 속성 이름/허용값 동일성은 레지스트리 미확보로 **미확인**(unverified) — 묶음은 플랫폼 계열 기준의 잠정 판단.

## 구버전 펌웨어 차이
확인불가 (비교할 속성 목록 없음).
