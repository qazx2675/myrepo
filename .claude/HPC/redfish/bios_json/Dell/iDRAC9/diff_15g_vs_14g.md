# iDRAC9 15G vs 14G BIOS 속성 차이

- 이전(A): PowerEdge R740xd (iDRAC 7.00.00.182, BIOS 2.24.0) — 레지스트리 641개, /Bios 노출 338개
- 이후(B): PowerEdge R750 (iDRAC 7.10.30.00, BIOS 1.13.2) — 레지스트리 687개, /Bios 노출 454개
- 비교 방법: 두 캡처의 `GET /redfish/v1/Systems/System.Embedded.1/Bios/BiosRegistry` 원본 JSON 을 스크립트로 비교(요약 도구 미사용). 이름 기준; 이름 변경은 DisplayName 이 같고 이름이 다른 쌍으로 추정 표시.
- 주의: 14G=R740xd(7.00.00.182/BIOS 2.24.0), 15G=R750(7.10.30.00/BIOS 1.13.2) 실캡처.

## 요약

| 항목 | 개수 |
|---|---|
| 추가 (B에만) | 144 |
| 삭제 (A에만) | 98 |
| 공통 | 543 |
| type 변경 | 5 |
| allowed_values 변경(Enumeration) | 44 |
| 정수 범위 변경 | 1 |
| DisplayName 변경 | 9 |
| default 변경 | 비교 불가 (Dell 레지스트리에 DefaultValue 없음) |

## 이름변경 후보 (DisplayName 동일, 이름 상이)

없음

## 추가/삭제 목록 (MenuPath 별, 전체)

### 추가 (B에만 존재) (144)

- `./BootSettingsRef` (1): `SysPrepClean`
- `./IntegratedDevicesRef` (1): `EmbNic1Nic2Nic3Nic4`(hidden/미노출)
- `./IntegratedDevicesRef/SlotBifurcationRef` (5): `Slot14Bif`(hidden/미노출), `Slot31Bif`(hidden/미노출), `Slot32Bif`(hidden/미노출), `Slot33Bif`(hidden/미노출), `Slot34Bif`(hidden/미노출)
- `./IntegratedDevicesRef/SlotDisablementRef` (5): `Slot14`(hidden/미노출), `Slot31`(hidden/미노출), `Slot32`(hidden/미노출), `Slot33`(hidden/미노출), `Slot34`(hidden/미노출)
- `./MemSettingsRef` (1): `MemoryTraining`
- `./MemSettingsRef/MemoryMapOutRef` (32): `DimmSlot00`, `DimmSlot01`, `DimmSlot02`, `DimmSlot03`, `DimmSlot04`, `DimmSlot05`, `DimmSlot06`, `DimmSlot07`, `DimmSlot08`, `DimmSlot09`, `DimmSlot10`, `DimmSlot11`, `DimmSlot12`, `DimmSlot13`, `DimmSlot14`, `DimmSlot15`, `DimmSlot16`, `DimmSlot17`, `DimmSlot18`, `DimmSlot19`, `DimmSlot20`, `DimmSlot21`, `DimmSlot22`, `DimmSlot23`, `DimmSlot24`, `DimmSlot25`, `DimmSlot26`, `DimmSlot27`, `DimmSlot28`, `DimmSlot29`, `DimmSlot30`, `DimmSlot31`
- `./NetworkSettingsRef` (1): `NumberOfPxeDevices`(hidden/미노출)
- `./NetworkSettingsRef/HttpDev1SettingsRef` (8): `HttpDev1v6Address`(hidden/미노출), `HttpDev1v6AutoConfig`(hidden/미노출), `HttpDev1v6Dns1`(hidden/미노출), `HttpDev1v6Dns2`(hidden/미노출), `HttpDev1v6DnsDhcpEnDis`(hidden/미노출), `HttpDev1v6Gateway`(hidden/미노출), `HttpDev1v6PrefixLen`(hidden/미노출), `HttpDev1v6Uri`(hidden/미노출)
- `./NetworkSettingsRef/HttpDev2SettingsRef` (8): `HttpDev2v6Address`(hidden/미노출), `HttpDev2v6AutoConfig`(hidden/미노출), `HttpDev2v6Dns1`(hidden/미노출), `HttpDev2v6Dns2`(hidden/미노출), `HttpDev2v6DnsDhcpEnDis`(hidden/미노출), `HttpDev2v6Gateway`(hidden/미노출), `HttpDev2v6PrefixLen`(hidden/미노출), `HttpDev2v6Uri`(hidden/미노출)
- `./NetworkSettingsRef/HttpDev3SettingsRef` (8): `HttpDev3v6Address`(hidden/미노출), `HttpDev3v6AutoConfig`(hidden/미노출), `HttpDev3v6Dns1`(hidden/미노출), `HttpDev3v6Dns2`(hidden/미노출), `HttpDev3v6DnsDhcpEnDis`(hidden/미노출), `HttpDev3v6Gateway`(hidden/미노출), `HttpDev3v6PrefixLen`(hidden/미노출), `HttpDev3v6Uri`(hidden/미노출)
- `./NetworkSettingsRef/HttpDev4SettingsRef` (8): `HttpDev4v6Address`(hidden/미노출), `HttpDev4v6AutoConfig`(hidden/미노출), `HttpDev4v6Dns1`(hidden/미노출), `HttpDev4v6Dns2`(hidden/미노출), `HttpDev4v6DnsDhcpEnDis`(hidden/미노출), `HttpDev4v6Gateway`(hidden/미노출), `HttpDev4v6PrefixLen`(hidden/미노출), `HttpDev4v6Uri`(hidden/미노출)
- `./NetworkSettingsRef/PxeDev10SettingsRef` (1): `PxeDev10Interface`
- `./NetworkSettingsRef/PxeDev11SettingsRef` (1): `PxeDev11Interface`
- `./NetworkSettingsRef/PxeDev12SettingsRef` (1): `PxeDev12Interface`
- `./NetworkSettingsRef/PxeDev13SettingsRef` (1): `PxeDev13Interface`
- `./NetworkSettingsRef/PxeDev14SettingsRef` (1): `PxeDev14Interface`
- `./NetworkSettingsRef/PxeDev15SettingsRef` (1): `PxeDev15Interface`
- `./NetworkSettingsRef/PxeDev16SettingsRef` (1): `PxeDev16Interface`
- `./NetworkSettingsRef/PxeDev5SettingsRef` (1): `PxeDev5Interface`
- `./NetworkSettingsRef/PxeDev6SettingsRef` (1): `PxeDev6Interface`
- `./NetworkSettingsRef/PxeDev7SettingsRef` (1): `PxeDev7Interface`
- `./NetworkSettingsRef/PxeDev8SettingsRef` (1): `PxeDev8Interface`
- `./NetworkSettingsRef/PxeDev9SettingsRef` (1): `PxeDev9Interface`
- `./NvmeSettingsRef` (2): `BiosNvmeDriver`, `VmdMode`(hidden/미노출)
- `./ProcSettingsRef` (19): `AvxIccpPreGrantLevel`(hidden/미노출), `AvxIccpPreGrantLicense`, `CpuFeatureErms`(hidden/미노출), `CpuFeatureFsrm`(hidden/미노출), `CpuFeatureRmss`(hidden/미노출), `CpuPaLimit`, `DynamicIss`(hidden/미노출), `KernelDmaProtection`, `LmceEn`, `MadtCoreEnumeration`, `Proc1PPIN`(hidden/미노출), `Proc2PPIN`(hidden/미노출), `ProcAvxP1`, `Rmp`(hidden/미노출), `Sme`(hidden/미노출), `Snp`(hidden/미노출), `SubNumaClusterChanged`(hidden/미노출), `TransparentSme`(hidden/미노출), `XptPrefetch`
- `./ProcSettingsRef/DellControlledTurboRef` (1): `OptimizerMode`
- `./SysProfileSettingsRef` (12): `Cppc`(hidden/미노출), `DynamicL1`(hidden/미노출), `EnablePkgcCriteria`(hidden/미노출), `EnergyEfficientTurbo`(hidden/미노출), `PackageCStates`(hidden/미노출), `PkgCLatNeg`(hidden/미노출), `ProcessorC1AutoDemotion`(hidden/미노출), `ProcessorC1AutoUnDemotion`(hidden/미노출), `ProcessorGpssTimer`(hidden/미노출), `PwrPerfSwitch`, `WorkloadConfiguration`(hidden/미노출), `WorkloadProfileHelper`(hidden/미노출)
- `./SysSecurityRef` (20): `DrtmSkinit`(hidden/미노출), `EnableMkTme`(hidden/미노출), `EnableTme`(hidden/미노출), `EpochUpdate`(hidden/미노출), `MemoryEncryption`, `PrmrrSize`(hidden/미노출), `SgxAutoRegistrationAgent`(hidden/미노출), `SgxEpoch0`(hidden/미노출), `SgxEpoch1`(hidden/미노출), `SgxFactoryReset`(hidden/미노출), `SgxLePubKeyHash0`(hidden/미노출), `SgxLePubKeyHash1`(hidden/미노출), `SgxLePubKeyHash2`(hidden/미노출), `SgxLePubKeyHash3`(hidden/미노출), `SgxLeWr`(hidden/미노출), `SgxPackageInfoInBandAccess`(hidden/미노출), `SgxQos`(hidden/미노출), `SmmSecurityMitigation`, `StrongPassword`(hidden/미노출), `StrongPasswordMinLength`(hidden/미노출)

### 삭제 (A에만 존재) (98)

- `./MemSettingsRef` (3): `DramRefreshDelay`, `NativeTrfcTiming`, `OppSrefEn`
- `./MemSettingsRef/PersistentMemorySettingRef` (3): `NvdimmFactoryDefault`(hidden/미노출), `PersistentMemoryMode`(hidden/미노출), `PersistentMemoryScrubbing`(hidden/미노출)
- `./MemSettingsRef/PersistentMemorySettingRef/IntelPersistentMemorySettingsMainMenuRef` (5): `PersistentMemPassphrase`(hidden/미노출), `PmAppDirectCapacity`(hidden/미노출), `PmMemoryCapacity`(hidden/미노출), `PmRawCapacity`(hidden/미노출), `PmUnconfiguredCapacity`(hidden/미노출)
- `./MemSettingsRef/PersistentMemorySettingRef/IntelPersistentMemorySettingsMainMenuRef/PmIntelPersistentMemoryDIMMsRef` (2): `PmOverwriteDimmAll`(hidden/미노출), `PmSecureEraseAll`(hidden/미노출)
- `./MemSettingsRef/PersistentMemorySettingRef/IntelPersistentMemorySettingsMainMenuRef/PmIntelPersistentMemoryRegionsRef/PmCreateGoalConfigRef` (3): `PersistentMemoryType`(hidden/미노출), `PmMemoryMode`(hidden/미노출), `PmPersistentPercentage`(hidden/미노출)
- `./MemSettingsRef/PersistentMemorySettingRef/NVDIMMNMemorySettingsMainMenuRef` (63): `BatteryStatus`(hidden/미노출), `NvdimmFirmwareVer0`(hidden/미노출), `NvdimmFirmwareVer1`(hidden/미노출), `NvdimmFirmwareVer10`(hidden/미노출), `NvdimmFirmwareVer11`(hidden/미노출), `NvdimmFirmwareVer2`(hidden/미노출), `NvdimmFirmwareVer3`(hidden/미노출), `NvdimmFirmwareVer4`(hidden/미노출), `NvdimmFirmwareVer5`(hidden/미노출), `NvdimmFirmwareVer6`(hidden/미노출), `NvdimmFirmwareVer7`(hidden/미노출), `NvdimmFirmwareVer8`(hidden/미노출), `NvdimmFirmwareVer9`(hidden/미노출), `NvdimmFreq0`(hidden/미노출), `NvdimmFreq1`(hidden/미노출), `NvdimmFreq10`(hidden/미노출), `NvdimmFreq11`(hidden/미노출), `NvdimmFreq2`(hidden/미노출), `NvdimmFreq3`(hidden/미노출), `NvdimmFreq4`(hidden/미노출), `NvdimmFreq5`(hidden/미노출), `NvdimmFreq6`(hidden/미노출), `NvdimmFreq7`(hidden/미노출), `NvdimmFreq8`(hidden/미노출), `NvdimmFreq9`(hidden/미노출), `NvdimmInterleaveSupport`(hidden/미노출), `NvdimmLocation0`(hidden/미노출), `NvdimmLocation1`(hidden/미노출), `NvdimmLocation10`(hidden/미노출), `NvdimmLocation11`(hidden/미노출), `NvdimmLocation2`(hidden/미노출), `NvdimmLocation3`(hidden/미노출), `NvdimmLocation4`(hidden/미노출), `NvdimmLocation5`(hidden/미노출), `NvdimmLocation6`(hidden/미노출), `NvdimmLocation7`(hidden/미노출), `NvdimmLocation8`(hidden/미노출), `NvdimmLocation9`(hidden/미노출), `NvdimmReadOnly`(hidden/미노출), `NvdimmSerialNum0`(hidden/미노출), `NvdimmSerialNum1`(hidden/미노출), `NvdimmSerialNum10`(hidden/미노출), `NvdimmSerialNum11`(hidden/미노출), `NvdimmSerialNum2`(hidden/미노출), `NvdimmSerialNum3`(hidden/미노출), `NvdimmSerialNum4`(hidden/미노출), `NvdimmSerialNum5`(hidden/미노출), `NvdimmSerialNum6`(hidden/미노출), `NvdimmSerialNum7`(hidden/미노출), `NvdimmSerialNum8`(hidden/미노출), `NvdimmSerialNum9`(hidden/미노출), `NvdimmSize0`(hidden/미노출), `NvdimmSize1`(hidden/미노출), `NvdimmSize10`(hidden/미노출), `NvdimmSize11`(hidden/미노출), `NvdimmSize2`(hidden/미노출), `NvdimmSize3`(hidden/미노출), `NvdimmSize4`(hidden/미노출), `NvdimmSize5`(hidden/미노출), `NvdimmSize6`(hidden/미노출), `NvdimmSize7`(hidden/미노출), `NvdimmSize8`(hidden/미노출), `NvdimmSize9`(hidden/미노출)
- `./NetworkSettingsRef/IscsiDev1SettingsRef` (1): `IscsiF1F2ErrorPrompt`
- `./ProcSettingsRef` (9): `AvxIccpPregrant`, `FastGoConfig`(hidden/미노출), `IrqThrottle`(hidden/미노출), `L2RfoPrefetch`(hidden/미노출), `PPINControlOverride`(hidden/미노출), `PackageRAPLLimitCsr`(hidden/미노출), `ProcConfigTdp`, `TurboPowerLimitLock`(hidden/미노출), `WFRWAEnableOverride`(hidden/미노출)
- `./SysProfileSettingsRef` (4): `Proc1TurboCoreNum`, `Proc2TurboCoreNum`, `Proc3TurboCoreNum`(hidden/미노출), `Proc4TurboCoreNum`(hidden/미노출)
- `./SysSecurityRef` (5): `SgxLEPubKeyHash0`(hidden/미노출), `SgxLEPubKeyHash1`(hidden/미노출), `SgxLEPubKeyHash2`(hidden/미노출), `SgxLEPubKeyHash3`(hidden/미노출), `SgxLcp`(hidden/미노출)

## type 변경

| 속성 | A | B |
|---|---|---|
| `CpuMinSevAsid` | Enumeration | Integer |
| `IscsiDev1Con1ChapSecret` | Password | String |
| `IscsiDev1Con1RevChapSecret` | Password | String |
| `IscsiDev1Con2ChapSecret` | Password | String |
| `IscsiDev1Con2RevChapSecret` | Password | String |

## allowed_values 변경 (Enumeration, 전체)

| 속성 | A 에만 있는 값 | B 에만 있는 값 | B 전체 값 |
|---|---|---|---|
| `ControlledTurbo` | ControlledTurboLimitMinus1, ControlledTurboLimitMinus2, ControlledTurboLimitMinus3 | - | Enabled, Disabled |
| `CpuInterconnectBusSpeed` | - | 11GTps | MaxDataRate, 11GTps, 10GTps, 9GTps |
| `HttpDev1Interface` | NIC.Integrated.1-1-1, NIC.Integrated.1-2-1, NIC.Integrated.1-3-1, NIC.Integrated.1-4-1 | NIC.Embedded.1-1-1, NIC.Embedded.2-1-1, NIC.Slot.2-1, NIC.Slot.5-1 | NIC.Embedded.1-1-1, NIC.Embedded.2-1-1, NIC.Slot.2-1, NIC.Slot.5-1 |
| `HttpDev2Interface` | NIC.Integrated.1-1-1, NIC.Integrated.1-2-1, NIC.Integrated.1-3-1, NIC.Integrated.1-4-1 | NIC.Embedded.1-1-1, NIC.Embedded.2-1-1, NIC.Slot.2-1, NIC.Slot.5-1 | NIC.Embedded.1-1-1, NIC.Embedded.2-1-1, NIC.Slot.2-1, NIC.Slot.5-1 |
| `HttpDev3Interface` | NIC.Integrated.1-1-1, NIC.Integrated.1-2-1, NIC.Integrated.1-3-1, NIC.Integrated.1-4-1 | NIC.Embedded.1-1-1, NIC.Embedded.2-1-1, NIC.Slot.2-1, NIC.Slot.5-1 | NIC.Embedded.1-1-1, NIC.Embedded.2-1-1, NIC.Slot.2-1, NIC.Slot.5-1 |
| `HttpDev4Interface` | NIC.Integrated.1-1-1, NIC.Integrated.1-2-1, NIC.Integrated.1-3-1, NIC.Integrated.1-4-1 | NIC.Embedded.1-1-1, NIC.Embedded.2-1-1, NIC.Slot.2-1, NIC.Slot.5-1 | NIC.Embedded.1-1-1, NIC.Embedded.2-1-1, NIC.Slot.2-1, NIC.Slot.5-1 |
| `IntelSgx` | On, Software | - | Off |
| `IscsiDev1Con1Interface` | NIC.Integrated.1-1-1, NIC.Integrated.1-2-1, NIC.Integrated.1-3-1, NIC.Integrated.1-4-1 | NIC.Embedded.1-1-1, NIC.Embedded.2-1-1, NIC.Slot.2-1, NIC.Slot.5-1 | NIC.Embedded.1-1-1, NIC.Embedded.2-1-1, NIC.Slot.2-1, NIC.Slot.5-1 |
| `IscsiDev1Con2Interface` | NIC.Integrated.1-1-1, NIC.Integrated.1-2-1, NIC.Integrated.1-3-1, NIC.Integrated.1-4-1 | NIC.Embedded.1-1-1, NIC.Embedded.2-1-1, NIC.Slot.2-1, NIC.Slot.5-1 | NIC.Embedded.1-1-1, NIC.Embedded.2-1-1, NIC.Slot.2-1, NIC.Slot.5-1 |
| `MemFrequency` | 2933MHz, 2666MHz, 2400MHz, 2133MHz, 1866MHz, MaxReliability | - | MaxPerf |
| `MemOpMode` | SingleRankSpareMode, MultiRankSpareMode | - | OptimizerMode |
| `OneTimeUefiBootSeqDev` | AHCI.Slot.1-2, GenericUSB.Placeholder.1-1, Disk.USBBack.2-1 | NIC.HttpDevice.1-1, Unknown.Unknown.2-1 | NIC.HttpDevice.1-1, Unknown.Unknown.2-1 |
| `PmCRQoS` | PmCRQoSRecipe1, PmCRQoSRecipe2, PmCRQoSRecipe3, Disabled | NvmQosMode0, NvmQosMode1, NvmQosMode2 | NvmQosMode0, NvmQosMode1, NvmQosMode2 |
| `PmNVMPerformanceSetting` | - | (순서만 변경) | PmBWOptimized, PmBalancedProfile, PmLatencyOptimized |
| `Proc1ControlledTurbo` | ControlledTurboLimit | Disabled | Disabled |
| `Proc2ControlledTurbo` | ControlledTurboLimit | Disabled | Disabled |
| `Proc3ControlledTurbo` | ControlledTurboLimit | Disabled | Disabled |
| `Proc4ControlledTurbo` | ControlledTurboLimit | Disabled | Disabled |
| `ProcCores` | 18, 20, 22 | - | All, 1, 2, 4, 6, 8, 10, 12, 14, 16 |
| `ProcIssSetting` | IssOp2, IssOp3 | - | IssOp1 |
| `ProcessorEist` | Disabled | - | Enabled |
| `PxeDev1Interface` | NIC.Integrated.1-1-1, NIC.Integrated.1-2-1, NIC.Integrated.1-3-1, NIC.Integrated.1-4-1 | NIC.Embedded.1-1-1, NIC.Embedded.2-1-1, NIC.Slot.2-1, NIC.Slot.5-1 | NIC.Embedded.1-1-1, NIC.Embedded.2-1-1, NIC.Slot.2-1, NIC.Slot.5-1 |
| `PxeDev2Interface` | NIC.Integrated.1-1-1, NIC.Integrated.1-2-1, NIC.Integrated.1-3-1, NIC.Integrated.1-4-1 | NIC.Embedded.1-1-1, NIC.Embedded.2-1-1, NIC.Slot.2-1, NIC.Slot.5-1 | NIC.Embedded.1-1-1, NIC.Embedded.2-1-1, NIC.Slot.2-1, NIC.Slot.5-1 |
| `PxeDev3Interface` | NIC.Integrated.1-1-1, NIC.Integrated.1-2-1, NIC.Integrated.1-3-1, NIC.Integrated.1-4-1 | NIC.Embedded.1-1-1, NIC.Embedded.2-1-1, NIC.Slot.2-1, NIC.Slot.5-1 | NIC.Embedded.1-1-1, NIC.Embedded.2-1-1, NIC.Slot.2-1, NIC.Slot.5-1 |
| `PxeDev4Interface` | NIC.Integrated.1-1-1, NIC.Integrated.1-2-1, NIC.Integrated.1-3-1, NIC.Integrated.1-4-1 | NIC.Embedded.1-1-1, NIC.Embedded.2-1-1, NIC.Slot.2-1, NIC.Slot.5-1 | NIC.Embedded.1-1-1, NIC.Embedded.2-1-1, NIC.Slot.2-1, NIC.Slot.5-1 |
| `RedundantOsLocation` | Slot1 | - | None |
| `SerialComm` | OnConRedirAuto, OnConRedirCom1, OnConRedirCom2 | OnConRedir | OnNoConRedir, OnConRedir, Off |
| `SerialPortAddress` | Serial1Com1Serial2Com2, Serial1Com2Serial2Com1 | Com1, Com2 | Com1, Com2 |
| `Slot1` | Disabled | - | Enabled, BootDriverDisabled |
| `Slot1Bif` | x16, x4x4x8, x8x4x4 | - | x4, x8 |
| `Slot2` | - | Disabled | Enabled, Disabled, BootDriverDisabled |
| `Slot2Bif` | - | x16, x4x4x8, x8x4x4 | x16, x4, x8, x4x4x8, x8x4x4 |
| `Slot4Bif` | x16, x4x4x8, x8x4x4 | - | x4, x8 |
| `Slot6` | - | BootDriverDisabled | Enabled, Disabled, BootDriverDisabled |
| `Slot6Bif` | - | x16, x4x4x8, x8x4x4 | x16, x4, x8, x4x4x8, x8x4x4 |
| `Slot7Bif` | - | x16, x4x4x8, x8x4x4 | x16, x4, x8, x4x4x8, x8x4x4 |
| `Slot8` | Disabled | - | Enabled, BootDriverDisabled |
| `Slot8Bif` | x16, x4x4x8, x8x4x4 | - | x4, x8 |
| `SnoopHldOff` | - | Roll4KCycles, Roll8KCycles, Roll16KCycles, Roll32KCycles, Roll64KCycles, Roll128KCycles | Roll256Cycles, Roll512Cycles, Roll1KCycles, Roll2KCycles, Roll4KCycles, Roll8KCycles, Roll16KCycles, Roll32KCycles, Roll64KCycles, Roll128KCycles |
| `SubNumaCluster` | - | 2-Way | 2-Way, Disabled |
| `SysProfile` | PerfWorkStationOptimized | - | PerfPerWattOptimizedDapc, PerfPerWattOptimizedOs, PerfOptimized, Custom |
| `Tpm2Algorithm` | - | SHA1, SHA256, SHA384 | SHA1, SHA256, SHA384 |
| `UncoreFrequency` | - | OptimizedUFS | DynamicUFS, MaxUFS, OptimizedUFS |
| `WorkloadProfile` | NotAvailable | NotConfigured, TelcoOptimizedProfile | NotConfigured, HpcProfile, LowLatencyOptimizedProfile, VtOptimizedProfile, VtPerWattOptimizedProfile, DbOptimizedProfile, DbPerWattOptimizedProfile, SdsOptimizedProfile, SdsPerWattOptimizedProfile, TelcoOptimizedProfile |

## 정수 범위 변경

| 속성 | A (min/max/step) | B (min/max/step) |
|---|---|---|
| `AcPwrRcvryUserDelay` | 60/600/0 | 120/600/0 |

## DisplayName 변경

| 속성 | A | B |
|---|---|---|
| `AcPwrRcvryUserDelay` | User Defined Delay (60s to 600s) | User Defined Delay (120s to 600s) |
| `ControlledTurbo` | Dell Controlled Turbo | Dell Controlled Turbo Setting |
| `CpuMinSevAsid` | Minimum SEV-ES ASID | Minimum SEV non-ES ASID |
| `OsAcpiCx` | OS Acpi Cx | OS ACPI Cx |
| `Proc2Cores` | Number of Cores per Processor 2 | Number of Cores for Processor 2 |
| `Proc3Cores` | Number of Cores per Processor 3 | Number of Cores for Processor 3 |
| `Proc4Cores` | Number of Cores per Processor 4 | Number of Cores for Processor 4 |
| `ProcCStates` | C-States | C States |
| `ProcessorEist` | Processor Eist | Processor EIST |

