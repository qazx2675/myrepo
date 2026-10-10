# Lenovo XCC2 (XClarity Controller 2) - BIOS 속성

조사일 2026-10-10. 브랜치 bios-collect. **Jev 생략** (키 없음) - "공식 문서 + 독립 2차 출처"로만 verified 판정.

## 대상 모델 / 프로토콜
| 사용자 모델 | 플랫폼 | 관리망 | 프로토콜 | 근거 |
|---|---|---|---|---|
| SR645 V3 | AMD EPYC 9004 (Genoa), 2S | XCC2 | **json (Redfish)** | Lenovo Press LP1607: "XClarity Controller 2 (XCC2) ... AST2600" |
| SR675 V3 | AMD EPYC 9004, 2S + GPU | XCC2 | json | LP1611 |
| SD650 V3 | **Intel** Xeon 4th/5th Gen (Sapphire/Emerald Rapids, Eagle Stream) | XCC2 | json | LP1603: "SD650 V3 ... XClarity Controller 2 (XCC2) ... AST2600" |

**주의:** SD650 V3 는 AMD 가 아니라 Intel 이다. 같은 XCC2 라도 SR645/SR675 V3(AMD)와 SD650 V3(Intel)의 BIOS 속성 집합은 서로 다르다 (예: AMD `Processors_SMTMode`, `Memory_NUMANodesperSocket` / Intel `Processors_HyperThreading`, `Processors_SNC`). `bios_attributes.json` 의 `observed[].platform` / `tuning_guide[].platform` 이 `amd_genoa` / `intel_eagle_stream` 로 구분한다.
XML API 없음 -> 전부 json.

## URI (verified: XCC2 REST 가이드 + 실덤프(XCC 계열과 동일 구조) + Lenovo 공식 스크립트)
- `GET /redfish/v1/Systems/1/Bios` (Attributes, `@Redfish.Settings.SettingsObject` -> Pending)
- `GET/PATCH /redfish/v1/Systems/1/Bios/Pending` (Settings 아님, ApplyTime OnReset). PATCH 응답 200 `RebootRequired`; 403 InsufficientPrivilege, 500, 503.
- `POST /redfish/v1/Systems/1/Bios/Actions/Bios.ResetBios`, `POST .../Bios.ChangePassword` (XCC2 문서에만 "OldPassword/NewPassword 모두 비우면 UEFI Admin 비밀번호 초기화" 주석이 추가됨), `/redfish/v1/Systems/1/Bios/ChangePasswordActionInfo`
- `GET /redfish/v1/schemas/registries/BiosAttributeRegistry.1.0.0.json`
- 펌웨어 `/redfish/v1/UpdateService/FirmwareInventory/UEFI`
- 문서 불일치: LP1836/LP1977 은 `Systems/Self/Bios`, `Bios/SD` 를 적었으나 XCC2 REST 가이드와 불일치 -> `Systems/1` + `Pending` 사용 (실장비 확인 필요, unverified 불일치로 기록).

## 속성 목록 상태: **partial**
`bios_attributes.json` : 이름 합집합 199개(+ 장치 전용 NIC/RAID 패턴 133개), 그 중 verified 45.
| 플랫폼 | 해당 모델 | 확보한 것 | 한계 |
|---|---|---|---|
| amd_genoa | SR645 V3, SR675 V3 | **SR635 V3 의 실제 키/값 310개**(weka/tools defaults-db.yml, 일반 154 + 장치 전용 156) + LP1977 튜닝 설정 28개(Redfish 이름 기준) + SR655 V3 운영 설정 | SR635 V3 는 1소켓 형제 기종 = **프록시**. 2소켓/GPU 전용 속성(예 LP1977 의 `Processors_xGMIMaximumLinkWidth`, `Processors_4_LinkxGMIMaxSpeed`, `Processors_xGMIForceLinkWidth`, `Processors_PeriodicDirectoryRinsePDRTuning`)은 SR635 V3 덤프에 없음. 펌웨어 버전 미기재 |
| intel_eagle_stream | SD650 V3 | LP1836 튜닝 설정 42개(Redfish 이름) + OneCLI 전용 항목 8개(+AMD 가이드 1개) + SR650 V3 운영 설정 | **실덤프 없음.** 전체 목록 unavailable |
- type/allowed_values/default/read_only 는 공식 문서의 레지스트리 예시 2개(XCC 문서와 동일한 복사본)만 확정, 나머지 null. `default` 값은 weka 덤프의 값("defaults" 로 표시된 값)을 `observed` 로만 보존(레지스트리 default 가 아니므로 `default` 필드에는 넣지 않음).
- 값 철자 규칙(실덤프 교차 확인): 표시명의 공백 제거, `/`·`–`·`-` -> `_` (`Maximum Performance`->`MaximumPerformance`, `I/O sensitive`->`I_OSensitive`, `Efficiency – Favor Performance`->`Efficiency_FavorPerformance`), **숫자로 시작하는 선택지는 속성명 접두**(`24 hour` -> `DRAMScrubTime_24Hours`, `1x` -> `DRAMRefreshRate_1x`, `115200` -> `Com1BaudRate_115200`). 기계적 변환이 아니므로 반드시 장비 레지스트리로 확인.
- 문서에 적힌 이름이 실덤프와 다른 예: LP1977 `Processors_P_state` vs 덤프 `Processors_P_state1`/`Processors_P_state2`. LP 문서의 Redfish 이름도 unverified 취급.

## 실장비 덤프 방법
`bios_attributes.json` 의 `unavailable.how_to_dump` 참조 (Bios / Bios/Pending / BiosAttributeRegistry.1.0.0.json GET, OneCLI `config show all`).

## 시도 내역
1. 다른 문서군: XCC2 REST 가이드 전 페이지(Bios, Pending GET/PATCH, ResetBios, ChangePassword, AttributeRegistry, AMT) -> XCC 문서와 텍스트 diff (아래 diff_vs_XCC.md); LP1977(AMD 9004), LP1836(Intel 4th/5th), LP2210; pubs.lenovo.com uefi_amd_4th/uefi_xeon_4th(메뉴 라벨만, Redfish 이름 없음); 제품 가이드 LP1607/1611/1603 로 XCC2 판정.
2. 공개 코드: weka/tools defaults-db.yml(SR635 V3 실값), sapcc/helm-charts(SR650 V3, SR655 V3, SR850 V3 등 운영 설정), lenovo/python-redfish-lenovo(`products_supported.txt` 에서 V3 Intel(EGS)/V3 AMD(Genoa) 별 열 분리 확인). Intel V3 전체 덤프는 GitHub 코드 검색에서 못 찾음. DMTF 목업에 Lenovo 없음.
3. 결론: Intel V3(SD650 V3) 전체 및 AMD V3 의 type/allowed/default 는 **unavailable**.
