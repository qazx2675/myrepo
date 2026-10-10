# Lenovo BIOS 요약 (관리망 버전별)

조사일 2026-10-10, 브랜치 bios-collect. 모든 Lenovo 서버는 **Redfish JSON** 으로 BIOS 를 다루며 XML API 는 없다. Jev 판정은 생략(키 없음) -> "공식 문서 + 독립 2차 출처"로 verified 판정.

| 관리망 | 프로토콜 | 사용자 모델 | 속성 수(이름 합집합 / 장치전용 패턴) | 상태 |
|---|---|---|---|---|
| **XCC** (XCC1) | json | SR630, SR650, SD530 (Purley) + **SR630 V2 (Whitley, XCC1 로 확정)** | 231 / 114 | **partial** - SR630 V2 는 같은 Whitley 의 SR670 V2 실덤프(프록시), Purley 는 튜닝 가이드 33개 + 문서 예시 |
| **XCC2** | json | SR645 V3, SR675 V3 (AMD Genoa) / SD650 V3 (**Intel** Eagle Stream) | 199 / 133 | **partial** - AMD 는 SR635 V3 실값 310키(프록시), Intel(SD650 V3)은 LP1836 튜닝 42개 + 운영 설정뿐 |
| **XCC3** | json | 없음 (비교용 V4) | 34 / 0 | **minimal** - 문서 레지스트리 예시 2 + 응답 예시 2 + LP2210 |

- 완전한 type/allowed_values/default/read_only 는 **문서에 실린 레지스트리 예시 항목(XCC·XCC2: 2개, XCC3: 2개)** 만 확정. 나머지는 null/unavailable. Lenovo 는 `BiosAttributeRegistry.1.0.0.json` 전체를 BMC 에서만 제공하며 공개 사본을 찾지 못함.
- 장비에서 전체를 얻는 방법: `GET /redfish/v1/Systems/1/Bios`, `.../Bios/Pending`, `GET /redfish/v1/schemas/registries/BiosAttributeRegistry.1.0.0.json` (각 버전 `bios_attributes.json` -> `unavailable.how_to_dump`).

## 공통 URI (XCC/XCC2/XCC3 동일, verified)
| 용도 | URI |
|---|---|
| BIOS | `/redfish/v1/Systems/1/Bios` |
| 설정(Settings 아님) | `/redfish/v1/Systems/1/Bios/Pending` (`Bios.@Redfish.Settings.SettingsObject`, ApplyTime OnReset). `PATCH {"Attributes":{...}}` -> 200 RebootRequired |
| 레지스트리 | `/redfish/v1/schemas/registries/BiosAttributeRegistry.1.0.0.json` (`Bios.AttributeRegistry`) |
| 액션 | `.../Bios/Actions/Bios.ResetBios`, `.../Bios/Actions/Bios.ChangePassword` (PasswordName: UefiAdminPassword / UefiPowerOnPassword), ActionInfo `.../Bios/ChangePasswordActionInfo` |
| UEFI 펌웨어 | `/redfish/v1/UpdateService/FirmwareInventory/UEFI` |
- Lenovo Press 튜닝 가이드가 적은 `Systems/Self/Bios`, `Bios/SD` 는 REST 가이드·실덤프와 충돌 -> 사용하지 않음 (`Systems/Self` 는 SR635/SR655 AMD 1세대 계열).

## SR630 V2 의 XCC 세대 판정 (요청 사항)
**XCC1 (XCC) 으로 확정, XCC2 아님 -> `XCC/` 폴더가 맞고 id_json/Lenovo/SUMMARY.md 의 분류와 일치. 정정 없음.**
근거 1: SR630 V2 제품 가이드 LP1391 - "XClarity Controller (XCC) ... Pilot4 XE401 BMC, dual-core ARM Cortex A9", 플랫폼 "Whitley". 근거 2: V3 제품 가이드(SR645 V3 LP1607, SR675 V3 LP1611, SD650 V3 LP1603)는 "XClarity Controller 2 (XCC2) ... AST2600", V4 는 XCC3 (LP1607). 근거 3: 공식 python-redfish-lenovo `products_supported.txt` 가 Purley / V2(Whitley) / V3(EGS, Genoa) / V4 를 별도 열로 구분. 근거 4: Whitley 실덤프(SR670 V2)의 Manager 가 "Lenovo XClarity Controller", FW `TGBT42S 2.83`.
다만 **같은 XCC 폴더 안에서 Purley(Skylake/Cascade Lake)와 Whitley 의 BIOS 값·속성이 다르다** (Skylake `Enable/Disable` -> Cascade Lake 이후 `Enabled/Disabled`, Whitley 에서 `Power_PCIePowerBrake`/`Power_ASPM` 신설 등: `XCC/old_fw_diff.md`).

## 버전 간 큰 차이
- XCC -> XCC2: REST 가이드의 Bios 관련 페이지 텍스트는 사실상 동일(문서 diff), URI 동일. 차이는 **플랫폼(Intel vs AMD EPYC 9004)과 UEFI 속성** 쪽 (`XCC2/diff_vs_XCC.md`). SD650 V3 는 Intel 이라 AMD 계열(SR645/SR675 V3)과 속성 집합이 다름.
- XCC2 -> XCC3: 문서 레벨 변화 - 레지스트리 `AttributeRegistry.v1_3_6`, `IsSystemUniqueProperty`, MenuPath 가 `SystemUEFI` 하위 구조, `CXLMemoryModule_*`/`DevicesandIOPorts_Bifurcation_Slot<N>` 신설, AMT 페이지 삭제, ResetBios target 문서 불일치 (`XCC3/diff_vs_XCC2.md`). `Group_Setting` 명명 규칙 자체의 변경 증거는 없음 (`Memory_MirrorMode` 류 이름 규칙 유지).

## 사용자 모델 매핑 정정
- 정정 없음: SR630/SR650/SD530 = XCC, SR630 V2 = XCC(XCC1), SR645 V3/SR675 V3/SD650 V3 = XCC2.
- 보완: **SD650 V3 는 Intel**(AMD 아님) -> XCC2 폴더 안에서 SR645/SR675 V3 와 속성 집합이 다름. 각 모델 그룹을 한 JSON 으로 합쳐도 `platform` 으로 구분.

## 실패/미확보 항목
1. 모든 버전의 `BiosAttributeRegistry.1.0.0.json` 전체 (공개 사본 없음) -> type/allowed_values/default/read_only 대부분 null.
2. SR630 V2/SR645 V3/SR675 V3 본인 장비 덤프 (프록시: SR670 V2, SR635 V3).
3. SD650 V3(Intel Eagle Stream) 및 XCC3(V4) 실덤프.
4. 각 장비 최신 XCC/UEFI 펌웨어 버전 (지원 사이트 다운로드는 받지 않음).
5. LP1477 PDF 파싱은 페이지 경계에서 값 누락 가능(예: `Power_PCIePowerBrake`, `Processors_SNC` 선택지) -> best-effort.
