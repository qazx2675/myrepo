# iDRAC9 14G vs iDRAC8 13G BIOS 속성 차이

- 이전(A): PowerEdge R630 (iDRAC 2.81.81.81, BIOS 2.13.0) — 레지스트리 249개, /Bios 노출 249개
- 이후(B): PowerEdge R740xd (iDRAC 7.00.00.182, BIOS 2.24.0) — 레지스트리 641개, /Bios 노출 338개
- 비교 방법: 두 캡처의 `GET /redfish/v1/Systems/System.Embedded.1/Bios/BiosRegistry` 원본 JSON 을 스크립트로 비교(요약 도구 미사용). 이름 기준; 이름 변경은 DisplayName 이 같고 이름이 다른 쌍으로 추정 표시.
- 주의: 13G 는 R630 BIOS 2.13.0, 14G 는 R740xd BIOS 2.24.0 실캡처. 모델이 달라 슬롯/드라이브베이 계열 차이 포함.

## 요약

| 항목 | 개수 |
|---|---|
| 추가 (B에만) | 400 |
| 삭제 (A에만) | 8 |
| 공통 | 241 |
| type 변경 | 4 |
| allowed_values 변경(Enumeration) | 25 |
| 정수 범위 변경 | 1 |
| DisplayName 변경 | 5 |
| default 변경 | 비교 불가 (Dell 레지스트리에 DefaultValue 없음) |

## 이름변경 후보 (DisplayName 동일, 이름 상이)

없음

## 추가/삭제 목록 (MenuPath 별, 전체)

### 추가 (B에만 존재) (400)

- `./BootSettingsRef` (3): `GenericUsbBoot`, `HddPlaceholder`, `OneTimeUefiBootPath`(hidden/미노출)
- `./IntegratedDevicesRef` (32): `EmbNic1`(hidden/미노출), `EmbNic1Nic2`(hidden/미노출), `EmbNic3Nic4`(hidden/미노출), `EmbNicPort1BootProto`(hidden/미노출), `EmbNicPort2BootProto`(hidden/미노출), `EmbNicPort3BootProto`(hidden/미노출), `EmbNicPort4BootProto`(hidden/미노출), `IntNic1Port1BootProto`(hidden/미노출), `IntNic1Port2BootProto`(hidden/미노출), `IntNic1Port3BootProto`(hidden/미노출), `IntNic1Port4BootProto`(hidden/미노출), `IntNic2Port1BootProto`(hidden/미노출), `IntNic2Port2BootProto`(hidden/미노출), `IntNic2Port3BootProto`(hidden/미노출), `IntNic2Port4BootProto`(hidden/미노출), `IntegratedNetwork2`(hidden/미노출), `InternalSdCard`(hidden/미노출), `InternalSdCardPresence`(hidden/미노출), `InternalSdCardPrimaryCard`(hidden/미노출), `InternalSdCardRedundancy`(hidden/미노출), `InternalUsb1`(hidden/미노출), `InternalUsb2`(hidden/미노출), `MemoryMappedIOH`, `MmioLimit`(hidden/미노출), `Ndc1PcieLink1`(hidden/미노출), `Ndc1PcieLink2`(hidden/미노출), `Ndc1PcieLink3`(hidden/미노출), `PCIRootDeviceUnhide`, `PcieBusCustomization`(hidden/미노출), `RipsPresence`(hidden/미노출), `UsbEnableFrontPortsOnly`(hidden/미노출), `UsbManagedPort`
- `./IntegratedDevicesRef/SlotBifurcationRef` (11): `DellAutoDiscovery`, `Slot10Bif`(hidden/미노출), `Slot11Bif`(hidden/미노출), `Slot12Bif`(hidden/미노출), `Slot13Bif`(hidden/미노출), `Slot4Bif`, `Slot5Bif`, `Slot6Bif`, `Slot7Bif`, `Slot8Bif`, `Slot9Bif`(hidden/미노출)
- `./IntegratedDevicesRef/SlotDisablementRef` (10): `Slot10`(hidden/미노출), `Slot11`(hidden/미노출), `Slot12`(hidden/미노출), `Slot13`(hidden/미노출), `Slot4`, `Slot5`, `Slot6`, `Slot7`, `Slot8`, `Slot9`(hidden/미노출)
- `./MemSettingsRef` (12): `AdddcSetting`, `CECriticalSEL`, `CkeProgramming`(hidden/미노출), `CurrentMemOpModeState`(hidden/미노출), `DdrtCke`(hidden/미노출), `DramRefreshDelay`, `FRMPercent`(hidden/미노출), `NativeTrfcTiming`, `OppSrefEn`, `PPROnUCE`, `RedundantMemCfgValid`(hidden/미노출), `RedundantMemInUse`(hidden/미노출)
- `./MemSettingsRef/PersistentMemorySettingRef` (3): `NvdimmFactoryDefault`(hidden/미노출), `PersistentMemoryMode`(hidden/미노출), `PersistentMemoryScrubbing`(hidden/미노출)
- `./MemSettingsRef/PersistentMemorySettingRef/IntelPersistentMemorySettingsMainMenuRef` (5): `PersistentMemPassphrase`(hidden/미노출), `PmAppDirectCapacity`(hidden/미노출), `PmMemoryCapacity`(hidden/미노출), `PmRawCapacity`(hidden/미노출), `PmUnconfiguredCapacity`(hidden/미노출)
- `./MemSettingsRef/PersistentMemorySettingRef/IntelPersistentMemorySettingsMainMenuRef/PmIntelPersistentMemoryDIMMsRef` (2): `PmOverwriteDimmAll`(hidden/미노출), `PmSecureEraseAll`(hidden/미노출)
- `./MemSettingsRef/PersistentMemorySettingRef/IntelPersistentMemorySettingsMainMenuRef/PmIntelPersistentMemoryRegionsRef/PmCreateGoalConfigRef` (3): `PersistentMemoryType`(hidden/미노출), `PmMemoryMode`(hidden/미노출), `PmPersistentPercentage`(hidden/미노출)
- `./MemSettingsRef/PersistentMemorySettingRef/NVDIMMNMemorySettingsMainMenuRef` (63): `BatteryStatus`(hidden/미노출), `NvdimmFirmwareVer0`(hidden/미노출), `NvdimmFirmwareVer1`(hidden/미노출), `NvdimmFirmwareVer10`(hidden/미노출), `NvdimmFirmwareVer11`(hidden/미노출), `NvdimmFirmwareVer2`(hidden/미노출), `NvdimmFirmwareVer3`(hidden/미노출), `NvdimmFirmwareVer4`(hidden/미노출), `NvdimmFirmwareVer5`(hidden/미노출), `NvdimmFirmwareVer6`(hidden/미노출), `NvdimmFirmwareVer7`(hidden/미노출), `NvdimmFirmwareVer8`(hidden/미노출), `NvdimmFirmwareVer9`(hidden/미노출), `NvdimmFreq0`(hidden/미노출), `NvdimmFreq1`(hidden/미노출), `NvdimmFreq10`(hidden/미노출), `NvdimmFreq11`(hidden/미노출), `NvdimmFreq2`(hidden/미노출), `NvdimmFreq3`(hidden/미노출), `NvdimmFreq4`(hidden/미노출), `NvdimmFreq5`(hidden/미노출), `NvdimmFreq6`(hidden/미노출), `NvdimmFreq7`(hidden/미노출), `NvdimmFreq8`(hidden/미노출), `NvdimmFreq9`(hidden/미노출), `NvdimmInterleaveSupport`(hidden/미노출), `NvdimmLocation0`(hidden/미노출), `NvdimmLocation1`(hidden/미노출), `NvdimmLocation10`(hidden/미노출), `NvdimmLocation11`(hidden/미노출), `NvdimmLocation2`(hidden/미노출), `NvdimmLocation3`(hidden/미노출), `NvdimmLocation4`(hidden/미노출), `NvdimmLocation5`(hidden/미노출), `NvdimmLocation6`(hidden/미노출), `NvdimmLocation7`(hidden/미노출), `NvdimmLocation8`(hidden/미노출), `NvdimmLocation9`(hidden/미노출), `NvdimmReadOnly`(hidden/미노출), `NvdimmSerialNum0`(hidden/미노출), `NvdimmSerialNum1`(hidden/미노출), `NvdimmSerialNum10`(hidden/미노출), `NvdimmSerialNum11`(hidden/미노출), `NvdimmSerialNum2`(hidden/미노출), `NvdimmSerialNum3`(hidden/미노출), `NvdimmSerialNum4`(hidden/미노출), `NvdimmSerialNum5`(hidden/미노출), `NvdimmSerialNum6`(hidden/미노출), `NvdimmSerialNum7`(hidden/미노출), `NvdimmSerialNum8`(hidden/미노출), `NvdimmSerialNum9`(hidden/미노출), `NvdimmSize0`(hidden/미노출), `NvdimmSize1`(hidden/미노출), `NvdimmSize10`(hidden/미노출), `NvdimmSize11`(hidden/미노출), `NvdimmSize2`(hidden/미노출), `NvdimmSize3`(hidden/미노출), `NvdimmSize4`(hidden/미노출), `NvdimmSize5`(hidden/미노출), `NvdimmSize6`(hidden/미노출), `NvdimmSize7`(hidden/미노출), `NvdimmSize8`(hidden/미노출), `NvdimmSize9`(hidden/미노출)
- `./MiscSettingsRef` (9): `CapsuleFirmwareUpdate`(hidden/미노출), `DellWyseP25BIOSAccess`, `ExtendedPost`(hidden/미노출), `Frb2Timer`(hidden/미노출), `PowerCycleRequest`, `SysMgmtNVByte1`(hidden/미노출), `SysMgmtNVByte2`(hidden/미노출), `WheaEinj`(hidden/미노출), `WheaSupport`(hidden/미노출)
- `./NetworkSettingsRef` (16): `HttpDev1EnDis`, `HttpDev2EnDis`, `HttpDev3EnDis`, `HttpDev4EnDis`, `PxeDev10EnDis`(hidden/미노출), `PxeDev11EnDis`(hidden/미노출), `PxeDev12EnDis`(hidden/미노출), `PxeDev13EnDis`(hidden/미노출), `PxeDev14EnDis`(hidden/미노출), `PxeDev15EnDis`(hidden/미노출), `PxeDev16EnDis`(hidden/미노출), `PxeDev5EnDis`(hidden/미노출), `PxeDev6EnDis`(hidden/미노출), `PxeDev7EnDis`(hidden/미노출), `PxeDev8EnDis`(hidden/미노출), `PxeDev9EnDis`(hidden/미노출)
- `./NetworkSettingsRef/HttpDev1SettingsRef` (13): `HttpDev1DhcpEnDis`, `HttpDev1Dns1`, `HttpDev1Dns2`, `HttpDev1DnsDhcpEnDis`, `HttpDev1Gateway`, `HttpDev1Interface`, `HttpDev1Ip`, `HttpDev1Mask`, `HttpDev1Protocol`, `HttpDev1Uri`, `HttpDev1VlanEnDis`, `HttpDev1VlanId`, `HttpDev1VlanPriority`
- `./NetworkSettingsRef/HttpDev1SettingsRef/HttpDev1TlsConfigRef` (1): `HttpDev1TlsMode`
- `./NetworkSettingsRef/HttpDev2SettingsRef` (13): `HttpDev2DhcpEnDis`, `HttpDev2Dns1`, `HttpDev2Dns2`, `HttpDev2DnsDhcpEnDis`, `HttpDev2Gateway`, `HttpDev2Interface`, `HttpDev2Ip`, `HttpDev2Mask`, `HttpDev2Protocol`, `HttpDev2Uri`, `HttpDev2VlanEnDis`, `HttpDev2VlanId`, `HttpDev2VlanPriority`
- `./NetworkSettingsRef/HttpDev2SettingsRef/HttpDev2TlsConfigRef` (1): `HttpDev2TlsMode`
- `./NetworkSettingsRef/HttpDev3SettingsRef` (13): `HttpDev3DhcpEnDis`, `HttpDev3Dns1`, `HttpDev3Dns2`, `HttpDev3DnsDhcpEnDis`, `HttpDev3Gateway`, `HttpDev3Interface`, `HttpDev3Ip`, `HttpDev3Mask`, `HttpDev3Protocol`, `HttpDev3Uri`, `HttpDev3VlanEnDis`, `HttpDev3VlanId`, `HttpDev3VlanPriority`
- `./NetworkSettingsRef/HttpDev3SettingsRef/HttpDev3TlsConfigRef` (1): `HttpDev3TlsMode`
- `./NetworkSettingsRef/HttpDev4SettingsRef` (13): `HttpDev4DhcpEnDis`, `HttpDev4Dns1`, `HttpDev4Dns2`, `HttpDev4DnsDhcpEnDis`, `HttpDev4Gateway`, `HttpDev4Interface`, `HttpDev4Ip`, `HttpDev4Mask`, `HttpDev4Protocol`, `HttpDev4Uri`, `HttpDev4VlanEnDis`, `HttpDev4VlanId`, `HttpDev4VlanPriority`
- `./NetworkSettingsRef/HttpDev4SettingsRef/HttpDev4TlsConfigRef` (1): `HttpDev4TlsMode`
- `./NetworkSettingsRef/IscsiDev1SettingsRef` (1): `IscsiF1F2ErrorPrompt`
- `./NetworkSettingsRef/PxeDev10SettingsRef` (4): `PxeDev10Protocol`(hidden/미노출), `PxeDev10VlanEnDis`(hidden/미노출), `PxeDev10VlanId`(hidden/미노출), `PxeDev10VlanPriority`(hidden/미노출)
- `./NetworkSettingsRef/PxeDev11SettingsRef` (4): `PxeDev11Protocol`(hidden/미노출), `PxeDev11VlanEnDis`(hidden/미노출), `PxeDev11VlanId`(hidden/미노출), `PxeDev11VlanPriority`(hidden/미노출)
- `./NetworkSettingsRef/PxeDev12SettingsRef` (4): `PxeDev12Protocol`(hidden/미노출), `PxeDev12VlanEnDis`(hidden/미노출), `PxeDev12VlanId`(hidden/미노출), `PxeDev12VlanPriority`(hidden/미노출)
- `./NetworkSettingsRef/PxeDev13SettingsRef` (4): `PxeDev13Protocol`(hidden/미노출), `PxeDev13VlanEnDis`(hidden/미노출), `PxeDev13VlanId`(hidden/미노출), `PxeDev13VlanPriority`(hidden/미노출)
- `./NetworkSettingsRef/PxeDev14SettingsRef` (4): `PxeDev14Protocol`(hidden/미노출), `PxeDev14VlanEnDis`(hidden/미노출), `PxeDev14VlanId`(hidden/미노출), `PxeDev14VlanPriority`(hidden/미노출)
- `./NetworkSettingsRef/PxeDev15SettingsRef` (4): `PxeDev15Protocol`(hidden/미노출), `PxeDev15VlanEnDis`(hidden/미노출), `PxeDev15VlanId`(hidden/미노출), `PxeDev15VlanPriority`(hidden/미노출)
- `./NetworkSettingsRef/PxeDev16SettingsRef` (4): `PxeDev16Protocol`(hidden/미노출), `PxeDev16VlanEnDis`(hidden/미노출), `PxeDev16VlanId`(hidden/미노출), `PxeDev16VlanPriority`(hidden/미노출)
- `./NetworkSettingsRef/PxeDev5SettingsRef` (4): `PxeDev5Protocol`(hidden/미노출), `PxeDev5VlanEnDis`(hidden/미노출), `PxeDev5VlanId`(hidden/미노출), `PxeDev5VlanPriority`(hidden/미노출)
- `./NetworkSettingsRef/PxeDev6SettingsRef` (4): `PxeDev6Protocol`(hidden/미노출), `PxeDev6VlanEnDis`(hidden/미노출), `PxeDev6VlanId`(hidden/미노출), `PxeDev6VlanPriority`(hidden/미노출)
- `./NetworkSettingsRef/PxeDev7SettingsRef` (4): `PxeDev7Protocol`(hidden/미노출), `PxeDev7VlanEnDis`(hidden/미노출), `PxeDev7VlanId`(hidden/미노출), `PxeDev7VlanPriority`(hidden/미노출)
- `./NetworkSettingsRef/PxeDev8SettingsRef` (4): `PxeDev8Protocol`(hidden/미노출), `PxeDev8VlanEnDis`(hidden/미노출), `PxeDev8VlanId`(hidden/미노출), `PxeDev8VlanPriority`(hidden/미노출)
- `./NetworkSettingsRef/PxeDev9SettingsRef` (4): `PxeDev9Protocol`(hidden/미노출), `PxeDev9VlanEnDis`(hidden/미노출), `PxeDev9VlanId`(hidden/미노출), `PxeDev9VlanPriority`(hidden/미노출)
- `./NvmeSettingsRef` (1): `NvmeMode`
- `./ProcSettingsRef` (52): `AvxIccpPregrant`, `ControlledTurboMinusBin`, `CpuInterconnectBusSpeed`, `CpuMinSevAsid`(hidden/미노출), `DeadLineLlcAlloc`, `DirectoryAtoS`, `DirectoryMode`(hidden/미노출), `FastGoConfig`(hidden/미노출), `ImcInterleave`(hidden/미노출), `IrqThrottle`(hidden/미노출), `L2RfoPrefetch`(hidden/미노출), `LlcPrefetch`, `NumaDistanceEnum`(hidden/미노출), `PPINControlOverride`(hidden/미노출), `PackageRAPLLimitCsr`(hidden/미노출), `Proc1ControlledTurbo`(hidden/미노출), `Proc1ControlledTurboMinusBin`(hidden/미노출), `Proc1Cores`(hidden/미노출), `Proc1MaxMemoryCapacity`, `Proc2ControlledTurbo`(hidden/미노출), `Proc2ControlledTurboMinusBin`(hidden/미노출), `Proc2Cores`(hidden/미노출), `Proc2MaxMemoryCapacity`, `Proc3Brand`(hidden/미노출), `Proc3ControlledTurbo`(hidden/미노출), `Proc3ControlledTurboMinusBin`(hidden/미노출), `Proc3Cores`(hidden/미노출), `Proc3Id`(hidden/미노출), `Proc3L2Cache`(hidden/미노출), `Proc3L3Cache`(hidden/미노출), `Proc3MaxMemoryCapacity`(hidden/미노출), `Proc3Microcode`(hidden/미노출), `Proc3NumCores`(hidden/미노출), `Proc4Brand`(hidden/미노출), `Proc4ControlledTurbo`(hidden/미노출), `Proc4ControlledTurboMinusBin`(hidden/미노출), `Proc4Cores`(hidden/미노출), `Proc4Id`(hidden/미노출), `Proc4L2Cache`(hidden/미노출), `Proc4L3Cache`(hidden/미노출), `Proc4MaxMemoryCapacity`(hidden/미노출), `Proc4Microcode`(hidden/미노출), `Proc4NumCores`(hidden/미노출), `ProcFlexRatioOverride`(hidden/미노출), `ProcFlexRatioSetting`(hidden/미노출), `ProcIssSetting`(hidden/미노출), `ProcessorActivePbf`(hidden/미노출), `ProcessorRaplPrioritization`(hidden/미노출), `SubNumaCluster`, `TurboPowerLimitLock`(hidden/미노출), `UpiPrefetch`, `WFRWAEnableOverride`(hidden/미노출)
- `./RedundantOsControlRef` (3): `RedundantOsBoot`, `RedundantOsLocation`, `RedundantOsState`
- `./SataSettingsRef` (46): `SataPortACapacity`(hidden/미노출), `SataPortADriveType`(hidden/미노출), `SataPortAModel`(hidden/미노출), `SataPortBCapacity`(hidden/미노출), `SataPortBDriveType`(hidden/미노출), `SataPortBModel`(hidden/미노출), `SataPortCCapacity`(hidden/미노출), `SataPortCDriveType`(hidden/미노출), `SataPortCModel`(hidden/미노출), `SataPortDCapacity`(hidden/미노출), `SataPortDDriveType`(hidden/미노출), `SataPortDModel`(hidden/미노출), `SataPortECapacity`(hidden/미노출), `SataPortEDriveType`(hidden/미노출), `SataPortEModel`(hidden/미노출), `SataPortFCapacity`(hidden/미노출), `SataPortFDriveType`(hidden/미노출), `SataPortFModel`(hidden/미노출), `SataPortGCapacity`(hidden/미노출), `SataPortGDriveType`(hidden/미노출), `SataPortGModel`(hidden/미노출), `SataPortHCapacity`(hidden/미노출), `SataPortHDriveType`(hidden/미노출), `SataPortHModel`(hidden/미노출), `SataPortICapacity`(hidden/미노출), `SataPortIDriveType`(hidden/미노출), `SataPortIModel`(hidden/미노출), `SataPortJCapacity`(hidden/미노출), `SataPortJDriveType`(hidden/미노출), `SataPortJModel`(hidden/미노출), `SataPortK`, `SataPortKCapacity`(hidden/미노출), `SataPortKDriveType`(hidden/미노출), `SataPortKModel`(hidden/미노출), `SataPortL`, `SataPortLCapacity`(hidden/미노출), `SataPortLDriveType`(hidden/미노출), `SataPortLModel`(hidden/미노출), `SataPortM`, `SataPortMCapacity`(hidden/미노출), `SataPortMDriveType`(hidden/미노출), `SataPortMModel`(hidden/미노출), `SataPortN`, `SataPortNCapacity`(hidden/미노출), `SataPortNDriveType`(hidden/미노출), `SataPortNModel`(hidden/미노출)
- `./SysInformationRef` (1): `SystemCpld2Version`(hidden/미노출)
- `./SysProfileSettingsRef` (9): `OsAcpiCx`(hidden/미노출), `PchPcieAspm`(hidden/미노출), `PcieAspmL1`, `PmCRQoS`(hidden/미노출), `PmNVMPerformanceSetting`(hidden/미노출), `Proc3TurboCoreNum`(hidden/미노출), `Proc4TurboCoreNum`(hidden/미노출), `ProcessorEist`(hidden/미노출), `WriteDataCrc`
- `./SysSecurityRef` (13): `AuthorizeDeviceFirmware`, `BootmanagerPassword`(hidden/미노출), `InBandManageabilityInterface`, `IntelSgx`(hidden/미노출), `OldSetupPassword`(hidden/미노출), `OldSysPassword`(hidden/미노출), `SecureBootMode`, `SgxLEPubKeyHash0`(hidden/미노출), `SgxLEPubKeyHash1`(hidden/미노출), `SgxLEPubKeyHash2`(hidden/미노출), `SgxLEPubKeyHash3`(hidden/미노출), `SgxLcp`(hidden/미노출), `Tpm2Hierarchy`
- `./SysSecurityRef/TpmAdvancedSettingsRef` (1): `Tpm2Algorithm`(hidden/미노출)

### 삭제 (A에만 존재) (8)

- `./IntegratedDevicesRef` (3): `IoNonPostedPrefetch`, `LowerMmio`, `Usb3Setting`
- `./IntegratedDevicesRef/SlotDisablementRef` (1): `GlobalSlotDriverDisable`
- `./ProcSettingsRef` (2): `Proc64bit`, `QpiSpeed`
- `./SysProfileSettingsRef` (2): `EnergyEfficientTurbo`, `PowerSaver`

## type 변경

| 속성 | A | B |
|---|---|---|
| `IscsiDev1Con1ChapSecret` | String | Password |
| `IscsiDev1Con1RevChapSecret` | String | Password |
| `IscsiDev1Con2ChapSecret` | String | Password |
| `IscsiDev1Con2RevChapSecret` | String | Password |

## allowed_values 변경 (Enumeration, 전체)

| 속성 | A 에만 있는 값 | B 에만 있는 값 | B 전체 값 |
|---|---|---|---|
| `BootSeqRetry` | - | Reset | Enabled, Disabled, Reset |
| `EmbSata` | AtaMode | - | AhciMode, RaidMode, Off |
| `IscsiDev1Con1Interface` | NIC.Slot.3-1, NIC.Slot.3-2 | - | NIC.Integrated.1-1-1, NIC.Integrated.1-2-1, NIC.Integrated.1-3-1, NIC.Integrated.1-4-1 |
| `IscsiDev1Con2Interface` | NIC.Slot.3-1, NIC.Slot.3-2 | - | NIC.Integrated.1-1-1, NIC.Integrated.1-2-1, NIC.Integrated.1-3-1, NIC.Integrated.1-4-1 |
| `MemFrequency` | 2667MHz, 1600MHz, 1333MHz | 2933MHz, 2666MHz | MaxPerf, 2933MHz, 2666MHz, 2400MHz, 2133MHz, 1866MHz, MaxReliability |
| `MemOpMode` | SpareMode, MirrorMode, AdvEccMode, SpareWithAdvEccMode, FaultResilientMode, NUMAFaultResilientMode | SingleRankSpareMode, MultiRankSpareMode | OptimizerMode, SingleRankSpareMode, MultiRankSpareMode |
| `OneTimeUefiBootSeqDev` | Disk.Bay.9:Enclosure.Internal.0-1:PCIeExtender.Slot.1, NIC.PxeDevice.1-1 | AHCI.Slot.1-2, GenericUSB.Placeholder.1-1, Disk.USBBack.2-1 | AHCI.Slot.1-2, GenericUSB.Placeholder.1-1, Disk.USBBack.2-1 |
| `Proc1TurboCoreNum` | - | 12, 14, 16, 18, 20, 22, 24, 26, 28 | All, 1, 2, 4, 6, 8, 10, 12, 14, 16, 18, 20, 22, 24, 26, 28 |
| `Proc2TurboCoreNum` | - | 12, 14, 16, 18, 20, 22, 24, 26, 28 | All, 1, 2, 4, 6, 8, 10, 12, 14, 16, 18, 20, 22, 24, 26, 28 |
| `ProcConfigTdp` | - | Level2 | Nominal, Level1, Level2 |
| `ProcCores` | - | 12, 14, 16, 18, 20, 22 | All, 1, 2, 4, 6, 8, 10, 12, 14, 16, 18, 20, 22 |
| `ProcPwrPerf` | HwpDbpm | - | SysDbpm, OsDbpm, MaxPerf |
| `PxeDev1Interface` | NIC.Slot.3-1, NIC.Slot.3-2 | - | NIC.Integrated.1-1-1, NIC.Integrated.1-2-1, NIC.Integrated.1-3-1, NIC.Integrated.1-4-1 |
| `PxeDev2Interface` | NIC.Slot.3-1, NIC.Slot.3-2 | - | NIC.Integrated.1-1-1, NIC.Integrated.1-2-1, NIC.Integrated.1-3-1, NIC.Integrated.1-4-1 |
| `PxeDev3Interface` | NIC.Slot.3-1, NIC.Slot.3-2 | - | NIC.Integrated.1-1-1, NIC.Integrated.1-2-1, NIC.Integrated.1-3-1, NIC.Integrated.1-4-1 |
| `PxeDev4Interface` | NIC.Slot.3-1, NIC.Slot.3-2 | - | NIC.Integrated.1-1-1, NIC.Integrated.1-2-1, NIC.Integrated.1-3-1, NIC.Integrated.1-4-1 |
| `Slot1Bif` | DefaultBifurcation, x4x4x4x4, x8x8 | x16, x4, x8, x4x4x8, x8x4x4 | x16, x4, x8, x4x4x8, x8x4x4 |
| `Slot2` | Disabled | - | Enabled, BootDriverDisabled |
| `Slot2Bif` | DefaultBifurcation, x4x4 | x4, x8 | x4, x8 |
| `Slot3Bif` | DefaultBifurcation, x4x4x4x4, x8x8 | x16, x4, x8, x4x4x8, x8x4x4 | x16, x4, x8, x4x4x8, x8x4x4 |
| `SnoopMode` | ClusterOnDie | - | EarlySnoop, HomeSnoop |
| `SysProfile` | DenseCfgOptimized | PerfWorkStationOptimized | PerfPerWattOptimizedDapc, PerfPerWattOptimizedOs, PerfOptimized, PerfWorkStationOptimized, Custom |
| `TpmSecurity` | OnPbm, OnNoPbm | On | Off, On |
| `UsbPorts` | - | AllOffDynamic | AllOn, OnlyBackPortsOn, AllOff, AllOffDynamic |
| `WorkloadProfile` | - | VtOptimizedProfile, VtPerWattOptimizedProfile, DbOptimizedProfile, DbPerWattOptimizedProfile, SdsOptimizedProfile, SdsPerWattOptimizedProfile | NotAvailable, HpcProfile, LowLatencyOptimizedProfile, VtOptimizedProfile, VtPerWattOptimizedProfile, DbOptimizedProfile, DbPerWattOptimizedProfile, SdsOptimizedProfile, SdsPerWattOptimizedProfile |

## 정수 범위 변경

| 속성 | A (min/max/step) | B (min/max/step) |
|---|---|---|
| `AcPwrRcvryUserDelay` | 60/240/0 | 60/600/0 |

## DisplayName 변경

| 속성 | A | B |
|---|---|---|
| `AcPwrRcvryUserDelay` | User Defined Delay (60s to 240s) | User Defined Delay (60s to 600s) |
| `AesNi` | Intel(R) AES-NI | CPU AES-NI |
| `CpuInterconnectBusLinkPower` | QPI Link L1 Power Management | CPU Interconnect Bus Link Power Management |
| `ProcCStates` | C States | C-States |
| `ProcX2Apic` | X2Apic Mode | x2APIC Mode |

