# XCC2 -> XCC3 차이 (Lenovo REST API 가이드 raw 텍스트 diff + 레지스트리 예시)

조사일 2026-10-10. 모두 [문서] 근거. 실장비 확인 전이므로 변경점의 "실제 동작"은 unverified.

| 항목 | XCC2 | XCC3 |
|---|---|---|
| Bios URI | `/redfish/v1/Systems/1/Bios`, `/Bios/Pending` | 동일 (변경 없음). 단 ResetBios target 이 필드표에서 `/redfish/v1/Systems/system/Bios/Actions/Bios.ResetBios` (예시는 Systems/1) |
| Bios 리소스 타입 | `#Bios.v1_0_6.Bios` | `#Bios.v1_2_0.Bios`, `@Redfish.Settings` `Settings.v1_3_5`, ETag 짧은 해시(`"7277A030"`), `Actions.Oem: {}` 와 최상위 `Oem` 추가 |
| Pending 응답 예시 | `Id`, `Name` 포함, v1_0_6 | `@odata.type v1_2_0`, ETag `"E29D62F1"`, 예시 속성이 `AdvancedRAS_DIMMDisablePolicy` ... `iSCSI_TargetPort_8` (iSCSI 속성도 BIOS 속성으로 노출) |
| Pending PATCH 페이지 | 예시 본문 + 503 ServiceUnavailable 문서화 | 예시 본문 없음, 503 항목 없음 |
| ResetBios 페이지 | 응답 예시(ExtendedInfo RebootRequired) | 응답 예시 삭제 |
| ChangePassword 페이지 | 응답 예시, "UEFI Admin 비밀번호 복구" 주석 | 요청/응답 예시 삭제, 필드표에 `title`, `target` 행 추가 |
| AMT 시험 옵션 PATCH | 있음 ("PATCH - Configure AMT test options") | **페이지 없음** (404) |
| 레지스트리 `@odata.type` | `AttributeRegistry.v1_3_0`, 최상위 `RegistryVersion`/`SupportedSystems` 예시(SR650 CDI340M, XCC1 예시 복사) | `AttributeRegistry.v1_3_6`, SupportedSystems 예시 `ThinkSystem SR650 V4` / FW `1.20` / SystemId `7DGCCTO1WW` |
| 레지스트리 속성 필드 | AttributeName, CurrentValue, DefaultValue, DisplayName, GrayOut, HelpText, Hidden, MenuPath, ReadOnly, ResetRequired, Type, Value[], WriteOnly ... | 위 + 예시에 **`IsSystemUniqueProperty`** 추가 (필드 설명표에는 없음), Menus 에도 `Hidden` |
| MenuPath 구조 | `./SystemRecovery/SystemRecovery_SystemRecovery` (그룹/그룹_그룹) | `./SystemUEFI/DevicesandIOPorts_Bifurcation/DevicesandIOPorts_Bifurcation_OverrideSlotBifurcation` 처럼 **`SystemUEFI` 최상위 메뉴 하위 구조**, 메뉴 `BiosConfigurations` / `SystemUEFI` |
| 신규 속성군 | - | `CXLMemoryModule_*` (CXL 메모리 모듈; 값 `MemoryMode_1LM_Vol`, `HeterogeneousInterleave`, `FlatMemoryMode`), `DevicesandIOPorts_Bifurcation_Slot<N>` (값 `x16`,`x8x8`,`x8x4x4`,`x4x4x8`,`x4x4x4x4`) |
| 값 이름 규칙 | 숫자 시작 선택지 -> 속성명 접두 (`DRAMScrubTime_24Hours`) | 동일 규칙 (`MemoryMode_1LM_Vol`) |
| 속성 명명 `Group_Setting` | 유지 | 유지 (`AdvancedRAS_`, `DevicesandIOPorts_`, `iSCSI_` ...). `Memory_MirrorMode` 류 이름 규칙 변경 증거 없음 |
| 어댑터 종속 | `MellanoxNetworkAdapter_<ID>__Slot<N>_*` 등 | 문서 Dependencies 예시에 `BroadcomNetXtremeGigabitEthernetAdapter__Slot14PhysicalPort1LogicalPort1_LegacyVLANMode` -> `..._VLANID14094` GrayOut (긴 이름이 문서에서 줄바꿈으로 깨짐) |
| AMD 튜닝 (LP1977 -> LP2210) | EPYC 9004: `Memory_MemorySpeed` 4800/4400/4000 MHz | EPYC 9005: 6000/5600/5200 MHz; 신규 `Processors_GMIFolding`, `Processors_ProbeFilterOrganization`, `Processors_xGMIP-States`(하이픈은 문서 표기 의심), `PowerProfileSelection`(그룹 접두 없는 문서 표기), `Power_EfficiencyMode` 대신 PowerProfileSelection 안내; LP1977 의 `Processors_MONITORMWAIT` 는 LP2210 에서 제외. (LP2210 이 XCC3 전용인지는 unverified) |

## 확인 못 한 것
- XCC3 장비의 실제 `Systems/1` vs 다른 ID, `Bios` 속성 총 개수, HTTP 동작 차이 (장비 없음).
- Intel Xeon 6(V4) 속성 전체. 위 `unavailable` 참조.
