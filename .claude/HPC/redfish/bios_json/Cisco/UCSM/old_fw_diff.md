# UCSM 릴리스 라인별 BIOS 토큰 차이 (old_fw_diff)

- 상태: **XML 클래스 도입 시점 = verified(ucsmsdk 메타 `introduced`)**, GUI 가이드 델타 = unverified(PDF 레이아웃 파싱).
- 기준: `bios_attributes.json` 의 `introduced_ucsm`(해당 `vp*` 속성이 UCSM 메타에 처음 등장한 릴리스). 이후 릴리스에서 기존 토큰의 enum/default 가 바뀐 이력은 ucsmsdk 에 없음(최초 도입만 기록).
- 라인 3.2/4.0/4.1/4.2/4.3/6.0 중 ucsmsdk 에 **신규 `vp*` 가 생긴 곳은 4.0(1a), 4.1(2c), 4.3(6a) 뿐**이며 3.2·4.2·6.0 에는 신규 XML 클래스가 없다. 반면 GUI 가이드는 그 라인들에서도 다수 토큰이 새로 생겼다고 서술(§2) → 이 차이분은 `biosTokenParam` 범용 경로이거나 SDK 미반영(README 한계 참고).

## 1. XML(vp*) 도입 릴리스별 (ucsmsdk 0.9.27, 전체)

| 최초 UCSM 릴리스 | 개수 | 속성 |
|---|---|---|
| 1.1(1j) | 37 | `vpACPI10Support`, `vpAssertNMIOnPERR`, `vpAssertNMIOnSERR`, `vpBootOptionRetry`, `vpBaudRate`, `vpConsoleRedirection`, `vpFlowControl`, `vpLegacyOSRedirection`, `vpTerminalType`, `vpCoreMultiProcessing`, `vpDirectCacheAccess`, `vpEnhancedIntelSpeedStepTech`, `vpExecuteDisableBit`, `vpFrontPanelLockout`, `vpIntelHyperThreadingTech`, `vpIntelTurboBoostTech`, `vpIntelVTDATSSupport`, `vpIntelVTDCoherencySupport`, `vpIntelVTDInterruptRemapping`, `vpIntelVTDPassThroughDMASupport`, `vpIntelVTForDirectedIO`, `vpIntelVirtualizationTechnology`, `vpMaximumMemoryBelow4GB`, `vpMemoryMappedIOAbove4GB`, `vpNUMAOptimized`, `vpOSBootWatchdogTimer`, `vpOSBootWatchdogTimerPolicy`, `vpOnboardSATAController`, `vpSATAMode`, `vpPOSTErrorPause`, `vpProcessorC3Report`, `vpProcessorC6Report`, `vpQuietBoot`, `vpResumeOnACPowerLoss`, `vpSelectMemoryRASConfiguration`, `vpSerialPortAEnable`, `vpMakeDeviceNonBootable` |
| 1.3(1c) | 2 | `vpLvDDRMode`, `vpMirroringMode` |
| 1.4(1i) | 6 | `vpCPUPerformance`, `vpSASRAID`, `vpSASRAIDModule`, `vpLoad`, `vpSparingMode`, `vpUEFIOSUseLegacyVideo` |
| 1.4(2b) | 4 | `vpProcessorMtrr`, `vpUCSMBootOrderRule`, `vpUSBFrontPanelLock`, `vpUSBIdlePowerOptimizing` |
| 2.0(1m) | 2 | `vpProcessorC1E`, `vpProcessorCState` |
| 2.0(2m) | 10 | `vpState`, `vpSlot1State`, `vpSlot2State`, `vpSlot3State`, `vpSlot4State`, `vpSlot5State`, `vpSlotMezzState`, `vpPackageCStateLimit`, `vpProcessorC7Report`, `vpLegacyUSBSupport` |
| 2.0(3a) | 2 | `vpOSBootWatchdogTimerTimeout`, `vpOnboardSCUStorageSupport` |
| 2.0(5a) | 1 | `vpDramRefreshRate` |
| 2.1(1a) | 1 | `vpSriov` |
| 2.1(2a) | 2 | `vpSlot6State`, `vpSlot7State` |
| 2.1(3a) | 2 | `vpLocalX2Apic`, `vpUEFIBootMode` |
| 2.2(2c) | 40 | `vpAllUSBDevices`, `vpPuttyKeyPad`, `vpDRAMClockThrottling`, `vpFRB2Timer`, `vpFrequencyFloorOverride`, `vpChannelInterleaving`, `vpMemoryInterleaving`, `vpRankInterleaving`, `vpPCIeSlot10LinkSpeed`, `vpPCIeSlot1LinkSpeed`, `vpPCIeSlot2LinkSpeed`, `vpPCIeSlot3LinkSpeed`, `vpPCIeSlot4LinkSpeed`, `vpPCIeSlot5LinkSpeed`, `vpPCIeSlot6LinkSpeed`, `vpPCIeSlot7LinkSpeed`, `vpPCIeSlot8LinkSpeed`, `vpPCIeSlot9LinkSpeed`, `vpPCIeSlotSASOptionROM`, `vpSlot10State`, `vpSlot8State`, `vpSlot9State`, `vpPSTATECoordination`, `vpEnergyPerformance`, `vpPowerTechnology`, `vpAdjacentCacheLinePrefetcher`, `vpDCUIPPrefetcher`, `vpDCUStreamerPrefetch`, `vpHardwarePrefetcher`, `vpQPILinkFrequencySelect`, `vpDemandScrub`, `vpPatrolScrub`, `vpPort6064Emulation`, `vpUSBPortFront`, `vpUSBPortInternal`, `vpUSBPortKVM`, `vpUSBPortRear`, `vpUSBPortSDCard`, `vpUSBPortVMedia`, `vpVGAPriority` |
| 2.2(3a) | 8 | `vpAltitude`, `vpPCIeSlotHBAOptionROM`, `vpPCIeSlotMLOMOptionROM`, `vpPCIeSlotN1OptionROM`, `vpPCIeSlotN2OptionROM`, `vpTPMSupport`, `vpLegacyUSBSupport`, `vpXHCIMode` |
| 2.2(4b) | 7 | `vpCDNControl`, `vpIntelTrustedExecutionTechnologySupport`, `vpSATAMode`, `vpPCIe10GLOM2Link`, `vpQPISnoopMode`, `vpTPMPendingOperation`, `vpTrustedPlatformModuleSupport` |
| 2.5(1a) | 2 | `vpASPMSupport`, `vpDDR3VoltageSelection` |
| 3.0(2c) | 1 | `vpEnhancedPowerCapping` |
| 3.1(1e) | 4 | `vpCPUHardwarePowerManagement`, `vpIntegratedGraphics`, `vpIntegratedGraphicsApertureSize`, `vpOnboardGraphics` |
| 3.1(2b) | 15 | `vpPwrPerfTuning`, `vpIOEMezz1OptionROM`, `vpIOENVMe1OptionROM`, `vpIOENVMe2OptionROM`, `vpIOESlot1OptionROM`, `vpIOESlot2OptionROM`, `vpComSpcrEnable`, `vpPCIROMCLP`, `vpProcessorCMCI`, `vpRedirectionAfterPOST`, `vpSBMezz1OptionROM`, `vpSBNVMe1OptionROM`, `vpSIOC1OptionROM`, `vpSIOC2OptionROM`, `vpWorkloadConfiguration` |
| 4.0(1a) | 1 | `vpBMEDMAMitigation` |
| 4.1(2c) | 1 | `vpPanicAndHighWatermark` |
| 4.3(6a) | 1 | `vpPreBootDMAProtection` |

## 2. 라인별 GUI 가이드 델타 (Cisco UCS Server BIOS Tokens 가이드, 사용자 모델 관련만)

### 3.2 (M5 최초)
- 가이드: "Cisco UCS M5 Server BIOS Tokens"(Server-BIOS-Tokens/3-2 HTML). 이번 조사에서 본문을 완전 취득하지 못함(검색 스니펫만) → **unavailable**. XML: `biosTokenFeatureGroup/Param/Settings` 도입(3.2(1d)).

### 4.0
- 별도 가이드 없음(4.1 가이드가 M5 표를 통합). XML: `vpBMEDMAMitigation` 도입(4.0(1a)).

### 4.1 (B200 M5/B480 M5 기준표 = 4.1(1a))
- 4.1(1a): M5 전체 표 (B200 M5 해당 74행 파싱, 14행 needs_review).
- 4.1(2): External SSC Enable, CR QoS(Recipe 1/2/3), NVM Performance Setting(BW/Latency Optimized/Balanced), CR FastGo Config(Default, Option 1~5, Auto), Snoopy mode for 2LM/AD.
- 4.1(3a): Memory Thermal Throttling Mode, Advanced Memory Test, Memory Refresh Rate(1x/2x), Panic and High Watermark(High/Low), PCIe PLL SSC, Configurable TDP Level, Uncore Frequency Scaling, UPI Link Frequency Select(Auto/9.6GT/s/10.4GT/s). (4.1(2) 표에 같은 항목이 중복 서술됨.) XML: `vpPanicAndHighWatermark` 4.1(2c).
- 4.1(3e): Burst and Postponed Refresh (Disabled; Enabled/Disabled). 4.1(3h): SHA-1 PCR Bank, SHA-256 PCR Bank (Enabled; Enabled/Disabled).

### 4.2
- 4.2(1d): M6(B200 M6 등) 도입. M5 변경분은 C220/C240/C480/C125 M5 전용(MRAID/RAID/NVMe Link Speed 값에 GEN4, C240 M5 의 Turbo/EIST/SpeedStep 등)으로 **B200 M5/B480 M5 에 대한 4.2 고유 변경 없음**(표 플랫폼 열 기준, unverified).
- **B200 M4 는 4.2(3s) 까지 UCSM 지원**하나 4.2 가이드는 M4 토큰을 다루지 않고 "refer ... Release 4.1" 로 안내하며 4.1 가이드에도 M4 표가 없다.

### 4.3 (X210c M7 은 4.3(2b) 부터)
- 4.3(2b): X210c M7 기본표 118행 (gui_tokens_by_model.json `x210c_m7.base_tokens`). 4.3(2c): X410c M7.
- 4.3(3a): TDX, TDX SEAM Loader, SHA384 PCR Bank, QpiLinkSpeed 신규; C1 Auto Demotion/UnDemotion 기본값 Auto(값에 Auto 추가).
- 4.3(3c): MMIO High Granularity Size, MMIO High Base, IOAT Configuration 신규.
- 4.3(4a): DFX OSB 신규, PRMRR Size 에 Auto 추가, DCPMM/CR QoS/NVM Performance/CR FastGo/Snoopy/eADR/Memory Bandwidth Boost 재게재. 4.3(4a) Deprecated 표 9행 존재(source_text 참조).
- 4.3(5a): Re-Size BAR Support 신규. 4.3(5c): UEFI Memory Map / ACPI SRAT Special Purpose Memory Flag 변경. XML: `vpPreBootDMAProtection` 4.3(6a).
- 4.3(6a)/(6c): M8 위주(IIO eDPC, M8 Sub NUMA Clustering 값 변경) — X210c M7/M5 영향 제한적.

### 6.0
- 6.0(1b)/(2b): GPU Direct CPU1/CPU2(X210c M7 포함) 신규, IIO eDPC Support. 6.0(2b) 가이드 원문: "The servers supported in this release 6.0(2b) continue utilizing the BIOS tokens from the previous release." B200 M5/B480 M5 는 6.0(2b) 지원 목록에 있으나 M5 토큰 변경 없음.

## 3. 사용자 영향 요약
- B200 M4: UCSM ≤ 4.2(3s). XML 토큰 후보는 introduced ≤ 3.1(2b) 클래스. 4.3 이상 지원 목록에서 빠짐(M4 제거 필요 서술은 포럼, unverified).
- B200 M5/B480 M5: 3.2~6.0 전 라인 지원; 토큰은 4.1 표 기준, 이후 라인에 M5 고유 변경 없음(가이드 근거).
- X210c M7: 4.3(2b) 이상만. 위 델타가 누적 적용된 값이 최신(6.0 포함).
