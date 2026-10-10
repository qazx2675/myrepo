# XCC -> XCC2 차이

조사일 2026-10-10. 근거 등급: [문서]=Lenovo REST API 가이드 raw 텍스트 diff, [LP]=Lenovo Press 튜닝 가이드, [덤프]=공개 실장비 값 (모델이 다르므로 참고).

## 1. REST/URI 레벨 - 사실상 동일 [문서, verified]
- xcc-restapi 와 xcc2-restapi 의 `resource_for_bios_get`, `pending_bios_settings_get`, `update_pending_bios_settings_patch`, `reset_bios_operation_post`, `bios_attribute_registries_get` 본문은 **글자 단위로 동일** (`diff` 결과 차이 없음). 예시의 `SupportedSystems` 도 동일하게 "ThinkSystem SR650 / CDI340M" (XCC1 시절 예시를 복사 -> XCC2 에서 새로 만든 예시가 아님).
- 유일한 문서 차이: `change_bios_password_settings_post` 에 XCC2 만 "UEFI Admin 비밀번호 복구: OldPassword/NewPassword 둘 다 비우면 해제" 주석 추가, 값 마침표 표기 차이.
- URI 동일: `/redfish/v1/Systems/1/Bios`, `/Bios/Pending`, `/Bios/Actions/Bios.ResetBios`, `/Bios/Actions/Bios.ChangePassword`, `/redfish/v1/schemas/registries/BiosAttributeRegistry.1.0.0.json`, `Bios.AttributeRegistry = BiosAttributeRegistry.1.0.0`.
- 문서가 가리키는 `@odata.type`: Bios `v1_0_6`, Settings `v1_2_1` (둘 다 XCC 와 동일 예시).

## 2. 속성 명명 규칙 [LP + 덤프]
- `<Group>_<Setting>` 규칙 동일. XCC2(AMD)에서도 `OperatingModes_`, `Processors_`, `Memory_`, `Power_`, `DevicesandIOPorts_`, `BootModes_`, `SystemRecovery_`, `SecureBootConfiguration_`, `TrustedComputingGroup_`, `NetworkStackSettings_`, `LegacyBIOS_`, `UEFILanguage_`, `DiskGPTRecovery_`, `POSTAttempts_`, `AdvancedRAS_` 그룹이 XCC(Intel) 덤프와 공통.
- 값 표기: XCC1 Skylake 의 `Enable/Disable` 이 아니라 **`Enabled/Disabled`** (XCC Whitley 부터 동일). XCC2 증거: weka SR635 V3 덤프, sapcc SR650 V3 (`Processors_CStates: "Disabled"`).
- `Memory_MirrorMode` 류 `Group_Setting` 규칙은 변하지 않음. 변화는 (a) 플랫폼별 설정 추가/삭제 (b) UEFI 빌드별 철자(`Memory_MirrorBelow4GB` vs `Memory_Mirrorbelow4GB`, XCC old_fw_diff 참고) 이다. XCC2 에서 새 접두 규칙이 도입된 증거는 못 찾음.

## 3. Intel: XCC(Whitley) -> XCC2(Eagle Stream, SD650 V3)  [LP1477 vs LP1836 이름 비교 + SR670 V2 덤프]
| 구분 | 속성 |
|---|---|
| 신규 (LP1836 에만, SR670 V2 덤프에도 없음) | `Processors_RocketMode`, `Processors_P-stateHysteresis`, `Processors_UMA-BasedClustering`, `Processors_CoNapTime`, `Processors_C-StateInterruptResponseTime`, `Processors_PCHPCIeRelaxedOrdering`, `Processors_CPUPCIeRelaxedOrdering`, `Processors_IntelSpeedSelect` (문서 이름에 공백·하이픈이 섞여 있어 실제 철자는 장비 확인 필요) |
| LP1477 에만 | `Memory_MemoryPowerManagement` (LP1836 목록엔 없음; 단 Whitley 덤프에는 존재) |
| 값 변화 | `Processors_CStates`: Legacy/Autonomous -> Legacy/Disabled; `Processors_SNC`: Enabled/Disabled -> Disabled/SNC2/SNC4; `Processors_SnoopPreference` 표시명 `Home Snoop` -> `Home`; `OperatingModes_ChooseOperatingMode`: `Custom Mode` 선택지 명시; `Memory_PagePolicy` 기본 Adaptive -> Closed; `Power_PCIePowerBrake`: Reactive -> Reactive/Proactive(기본)/Disabled |
| OneCLI 전용(Redfish 이름 미기재) | `Processors.L2RFOPrefetcher`, `Processors.LLCPrefetch`, `Memory.CRQoSConfiguration`, `Processors.IrqThreshold`, `Processors.StaleAtoS`, `Processors.SnoopResponseHoldOff`, `Processors.LLCdeadlinealloc`, `Processors.UncoreFrequencyLimit` (XCC1 덤프에는 `Processors_L2RFOPrefetcher`, `Processors_LLCPrefetch` 가 실제 존재 -> 같은 이름 규칙으로 노출될 가능성) |
LP 의 추출은 스크립트 기반이며 문서 표기 오류(공백 포함 이름 등)를 그대로 가질 수 있다 -> unverified.

## 4. AMD: SR635 V3 덤프(XCC2) vs Intel Whitley 덤프(XCC)  - **교차 벤더라 세대 차이로 단정 금지**
- 공통 이름 76개 (일반 키 기준, AMD 154 / Intel 221).
- AMD 에만: `Secured_Core_*`(7개), `Processors_SMTMode`, `Processors_SVMMode`, `Processors_cTDP`, `Processors_DeterminismSlider`, `Processors_CorePerformanceBoost`, `Memory_NUMANodesperSocket`, `Memory_DRAMScrubTime`, `Memory_TSME`, `Memory_SEVControl`, `DevicesandIOPorts_IOMMU`, `Power_EfficiencyMode` 등 AMD 고유 설정.
- 이름 체계 차이: `EnableDisableAdapterOptionROMSupport_*` 가 Intel 덤프에서는 슬롯/NVMe 베이별 28개, AMD V3 덤프에서는 분류별 4개(`_Network`, `_Storage`, `_Video`, `_OtherPCIdevices`). 장치 전용 NIC 속성은 Intel 덤프 `BroadcomNetXtreme..._Slot<N>PhysicalPort<P>LogicalPort<L>_*`, AMD 덤프 `MellanoxNetworkAdapter_<ID>__Slot<N>_*`, `NvidiaNetworkAdapter_<ID>__Slot<N>_*` (펌웨어 버전·MAC 형태 값이 BIOS 속성으로 노출되는 점은 공통).
- 값 철자: AMD V3 에서 숫자 시작 선택지는 속성명 접두(`DRAMScrubTime_24Hours`, `DRAMRefreshRate_1x`, `Com1BaudRate_115200`).
- 문서-덤프 불일치: LP1977 `Processors_P_state` <-> 덤프 `Processors_P_state1`/`Processors_P_state2`; LP1977 의 2소켓 전용 이름은 1소켓 덤프에 없음.

## 5. 스키마/레지스트리 메타 [문서 + 덤프]
- `BiosAttributeRegistry.1.0.0` ID 동일. XCC2 문서 자체에는 XCC 와 다른 레지스트리 필드가 없음.
- Bios `@odata.type` 은 펌웨어 빌드에 따라 `v1_0_6`(문서)~`v1_2_0`(2024 Whitley 덤프). XCC2 실장비 값은 못 구함 -> unverified.
