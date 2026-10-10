# iDRAC9 펌웨어 라인 간 BIOS 차이 (3.x / 4.x / 5.x / 6.x / 7.x)

## 결론

- **BIOS 속성의 추가·삭제·허용값 변경은 iDRAC 버전이 아니라 BIOS 버전을 따라간다.** 같은 모델 R750 에서 iDRAC 6.00.30.00(BIOS 1.8.2) → 7.10.30.00(BIOS 1.13.2) 로 바뀌어도 레지스트리는 **+5 / −0** 였다. 같은 모델 R650 의 5.10.00.00(BIOS 1.2.4) → 7.10.30.00(BIOS 1.12.1) 은 +149 / −1 이었다. 대부분 BIOS 1.2.4 → 1.12.1 에서 늘어난 항목이다(PXE 장치 5~16번, HTTP boot IPv6, DIMM 맵아웃 `DimmSlot00~31`, `PPROnUCE`, `EnergyEfficientTurbo`, `PwrPerfSwitch`, `KernelDmaProtection` 등).
- iDRAC 펌웨어가 바꾸는 것은 **전달 방식**이다(Redfish 스키마 버전, ApplyTime, OEM 액션, 레지스트리 필드). 아래 표 참고.
- 4.x 라인 캡처는 확보하지 못했다(unavailable). 3.x 는 14G 초기 BIOS(0.4.1)와 함께 쓴 캡처 1개뿐이라, 펌웨어 차이와 BIOS 차이를 분리할 수 없다.
- 판정 방법: 공개된 실장비 Redfish 원본(BiosRegistry JSON)을 스크립트로 비교했다(요약 도구 미사용). Jev 생략.

## Redfish 전달 방식 차이 (실캡처에서 확인)

| iDRAC9 | 캡처 | Bios @odata.type | Settings SupportedApplyTimes | Bios OEM 액션 | 레지스트리 ResetRequired 필드 | DellBIOSService |
|---|---|---|---|---|---|---|
| 3.15.15.15 | R740 (BIOS 0.4.1) | Bios.v1_0_0 | 없음 (Settings.v1_0_2) | 없음 | 없음 | 캡처에 없음 |
| 4.20.20.20 | (문서만: API Guide) | — | 문서에 `@Redfish.SettingsApplyTime` 기재 | `DellBios.RunBIOSLiveScanning` 기재 | — | `/redfish/v1/Dell/Systems/<Id>/DellBIOSService` |
| 5.10.00.00 | R650 (BIOS 1.2.4) | Bios.v1_1_1 | OnReset, AtMaintenanceWindowStart, InMaintenanceWindowOnReset | RunBIOSLiveScanning | 있음 | 캡처에 없음 |
| 6.00.30.00 | R750 (BIOS 1.8.2) | Bios.v1_2_0 | 동일 3종 | RunBIOSLiveScanning | 있음 | 캡처에 없음 |
| 7.00.00.182 | R740xd (BIOS 2.24.0) | Bios.v1_2_0 (Settings.v1_3_5) | 동일 3종 | RunBIOSLiveScanning | 있음 | 캡처에 없음 |
| 7.10.30.00 / 7.10.50.00 | R650/R750 · R760/R760xa | Bios.v1_2_1 | 동일 3종 | RunBIOSLiveScanning | 있음 | `/redfish/v1/Systems/System.Embedded.1/Oem/Dell/DellBIOSService` |
| 7.20.10.05 | R660xs (BIOS 2.4.4) | Bios.v1_2_3 (Settings.v1_4_0) | 동일 3종 | RunBIOSLiveScanning | 있음 | 위와 동일 |

공통(3.15 부터 7.20 까지 모두): `/Bios`, `/Bios/Settings`, `/Bios/BiosRegistry`, `Bios.ResetBios`, `Bios.ChangePassword`, `/Bios/Settings/Actions/Oem/DellManager.ClearPending`, SCP `EID_674_Manager.Export/Import/ImportSystemConfigurationPreview`.

## 문서 기반 추가 이력 (back/ 결과 재사용, 출처 재표기)

출처: iDRAC9 Attribute Registry (FW ≤ 4.40.00.00) "New features added" 장, https://dl.dell.com/topicspdf/idrac9-lifecycle-controller-v4x-series_Reference-Guide_en-us.pdf (이전 조사에서 확인. 이번에 원문을 다시 받지 못해 **unverified**)
- 4.40.00.00: `SysPrepClean` (BiosBootSettings)
- 4.30.30.30: `AgesaVersion` (AMD 플랫폼용)
- 이 문서는 iDRAC 버전별 "레지스트리 문서 반영 시점"을 기록한 것이고, 실제 존재 여부는 BIOS 버전이 결정한다. 실캡처 확인 결과 `SysPrepClean` 은 15G(R750/R650)·16G(R760xa 등) 레지스트리에는 있으나 **14G R740xd BIOS 2.24.0 과 R740 BIOS 0.4.1 레지스트리에는 없다**. iDRAC 4.40 문서에 실렸다고 14G BIOS 에 생기는 것은 아님을 보여 준다.

## 실캡처 비교 상세 (전체 목록)

## 14G: iDRAC 3.15.15.15 (BIOS 0.4.1, 출시 전 BIOS) -> iDRAC 7.00.00.182 (BIOS 2.24.0)

- 이전(A): PowerEdge R740 (iDRAC 3.15.15.15, BIOS 0.4.1) — 레지스트리 358개, /Bios 노출 292개
- 이후(B): PowerEdge R740xd (iDRAC 7.00.00.182, BIOS 2.24.0) — 레지스트리 641개, /Bios 노출 338개
- 비교 방법: 두 캡처의 `GET /redfish/v1/Systems/System.Embedded.1/Bios/BiosRegistry` 원본 JSON 을 스크립트로 비교(요약 도구 미사용). 이름 기준; 이름 변경은 DisplayName 이 같고 이름이 다른 쌍으로 추정 표시.
- 주의: R740 vs R740xd 로 모델도 다름(같은 14G Intel BIOS 계열). 0.4.1 은 초기(프리릴리스급) BIOS.

### 요약

| 항목 | 개수 |
|---|---|
| 추가 (B에만) | 330 |
| 삭제 (A에만) | 47 |
| 공통 | 311 |
| type 변경 | 4 |
| allowed_values 변경(Enumeration) | 16 |
| 정수 범위 변경 | 1 |
| DisplayName 변경 | 5 |
| default 변경 | 비교 불가 (Dell 레지스트리에 DefaultValue 없음) |

### 이름변경 후보 (DisplayName 동일, 이름 상이)

없음

### 추가/삭제 목록 (MenuPath 별, 전체)

#### 추가 (B에만 존재) (330)

- `./BootSettingsRef` (5): `GenericUsbBoot`, `HddPlaceholder`, `OneTimeUefiBootPath`(hidden/미노출), `SetBootOrderDis`, `SetBootOrderEn`
- `./IntegratedDevicesRef` (32): `EmbNic1`(hidden/미노출), `EmbNic1Nic2`(hidden/미노출), `EmbNic3Nic4`(hidden/미노출), `EmbNicPort1BootProto`(hidden/미노출), `EmbNicPort2BootProto`(hidden/미노출), `EmbNicPort3BootProto`(hidden/미노출), `EmbNicPort4BootProto`(hidden/미노출), `IntNic1Port1BootProto`(hidden/미노출), `IntNic1Port2BootProto`(hidden/미노출), `IntNic1Port3BootProto`(hidden/미노출), `IntNic1Port4BootProto`(hidden/미노출), `IntNic2Port1BootProto`(hidden/미노출), `IntNic2Port2BootProto`(hidden/미노출), `IntNic2Port3BootProto`(hidden/미노출), `IntNic2Port4BootProto`(hidden/미노출), `IntegratedNetwork2`(hidden/미노출), `IntegratedRaid`(hidden/미노출), `InternalSdCard`(hidden/미노출), `InternalSdCardPresence`(hidden/미노출), `InternalSdCardPrimaryCard`(hidden/미노출), `InternalSdCardRedundancy`(hidden/미노출), `InternalUsb1`(hidden/미노출), `InternalUsb2`(hidden/미노출), `MmioLimit`(hidden/미노출), `Ndc1PcieLink1`(hidden/미노출), `Ndc1PcieLink2`(hidden/미노출), `Ndc1PcieLink3`(hidden/미노출), `PCIRootDeviceUnhide`, `PcieBusCustomization`(hidden/미노출), `RipsPresence`(hidden/미노출), `SnoopHldOff`, `UsbEnableFrontPortsOnly`(hidden/미노출)
- `./IntegratedDevicesRef/SlotBifurcationRef` (9): `Slot10Bif`(hidden/미노출), `Slot11Bif`(hidden/미노출), `Slot12Bif`(hidden/미노출), `Slot13Bif`(hidden/미노출), `Slot5Bif`, `Slot6Bif`, `Slot7Bif`, `Slot8Bif`, `Slot9Bif`(hidden/미노출)
- `./IntegratedDevicesRef/SlotDisablementRef` (9): `Slot10`(hidden/미노출), `Slot11`(hidden/미노출), `Slot12`(hidden/미노출), `Slot13`(hidden/미노출), `Slot5`, `Slot6`, `Slot7`, `Slot8`, `Slot9`(hidden/미노출)
- `./MemSettingsRef` (12): `AdddcSetting`, `CECriticalSEL`, `CkeProgramming`(hidden/미노출), `DdrtCke`(hidden/미노출), `DramRefreshDelay`, `FRMPercent`(hidden/미노출), `NativeTrfcTiming`, `OppSrefEn`, `PPROnUCE`, `RedundantMemCfgValid`(hidden/미노출), `RedundantMemInUse`(hidden/미노출), `SnoopMode`(hidden/미노출)
- `./MemSettingsRef/PersistentMemorySettingRef` (1): `PersistentMemoryScrubbing`(hidden/미노출)
- `./MemSettingsRef/PersistentMemorySettingRef/IntelPersistentMemorySettingsMainMenuRef` (5): `PersistentMemPassphrase`(hidden/미노출), `PmAppDirectCapacity`(hidden/미노출), `PmMemoryCapacity`(hidden/미노출), `PmRawCapacity`(hidden/미노출), `PmUnconfiguredCapacity`(hidden/미노출)
- `./MemSettingsRef/PersistentMemorySettingRef/IntelPersistentMemorySettingsMainMenuRef/PmIntelPersistentMemoryDIMMsRef` (2): `PmOverwriteDimmAll`(hidden/미노출), `PmSecureEraseAll`(hidden/미노출)
- `./MemSettingsRef/PersistentMemorySettingRef/IntelPersistentMemorySettingsMainMenuRef/PmIntelPersistentMemoryRegionsRef/PmCreateGoalConfigRef` (3): `PersistentMemoryType`(hidden/미노출), `PmMemoryMode`(hidden/미노출), `PmPersistentPercentage`(hidden/미노출)
- `./MemSettingsRef/PersistentMemorySettingRef/NVDIMMNMemorySettingsMainMenuRef` (60): `NvdimmFirmwareVer0`(hidden/미노출), `NvdimmFirmwareVer1`(hidden/미노출), `NvdimmFirmwareVer10`(hidden/미노출), `NvdimmFirmwareVer11`(hidden/미노출), `NvdimmFirmwareVer2`(hidden/미노출), `NvdimmFirmwareVer3`(hidden/미노출), `NvdimmFirmwareVer4`(hidden/미노출), `NvdimmFirmwareVer5`(hidden/미노출), `NvdimmFirmwareVer6`(hidden/미노출), `NvdimmFirmwareVer7`(hidden/미노출), `NvdimmFirmwareVer8`(hidden/미노출), `NvdimmFirmwareVer9`(hidden/미노출), `NvdimmFreq0`(hidden/미노출), `NvdimmFreq1`(hidden/미노출), `NvdimmFreq10`(hidden/미노출), `NvdimmFreq11`(hidden/미노출), `NvdimmFreq2`(hidden/미노출), `NvdimmFreq3`(hidden/미노출), `NvdimmFreq4`(hidden/미노출), `NvdimmFreq5`(hidden/미노출), `NvdimmFreq6`(hidden/미노출), `NvdimmFreq7`(hidden/미노출), `NvdimmFreq8`(hidden/미노출), `NvdimmFreq9`(hidden/미노출), `NvdimmLocation0`(hidden/미노출), `NvdimmLocation1`(hidden/미노출), `NvdimmLocation10`(hidden/미노출), `NvdimmLocation11`(hidden/미노출), `NvdimmLocation2`(hidden/미노출), `NvdimmLocation3`(hidden/미노출), `NvdimmLocation4`(hidden/미노출), `NvdimmLocation5`(hidden/미노출), `NvdimmLocation6`(hidden/미노출), `NvdimmLocation7`(hidden/미노출), `NvdimmLocation8`(hidden/미노출), `NvdimmLocation9`(hidden/미노출), `NvdimmSerialNum0`(hidden/미노출), `NvdimmSerialNum1`(hidden/미노출), `NvdimmSerialNum10`(hidden/미노출), `NvdimmSerialNum11`(hidden/미노출), `NvdimmSerialNum2`(hidden/미노출), `NvdimmSerialNum3`(hidden/미노출), `NvdimmSerialNum4`(hidden/미노출), `NvdimmSerialNum5`(hidden/미노출), `NvdimmSerialNum6`(hidden/미노출), `NvdimmSerialNum7`(hidden/미노출), `NvdimmSerialNum8`(hidden/미노출), `NvdimmSerialNum9`(hidden/미노출), `NvdimmSize0`(hidden/미노출), `NvdimmSize1`(hidden/미노출), `NvdimmSize10`(hidden/미노출), `NvdimmSize11`(hidden/미노출), `NvdimmSize2`(hidden/미노출), `NvdimmSize3`(hidden/미노출), `NvdimmSize4`(hidden/미노출), `NvdimmSize5`(hidden/미노출), `NvdimmSize6`(hidden/미노출), `NvdimmSize7`(hidden/미노출), `NvdimmSize8`(hidden/미노출), `NvdimmSize9`(hidden/미노출)
- `./MiscSettingsRef` (8): `CapsuleFirmwareUpdate`(hidden/미노출), `ExtendedPost`(hidden/미노출), `Frb2Timer`(hidden/미노출), `InSystemCharacterization`(hidden/미노출), `SysMgmtNVByte1`(hidden/미노출), `SysMgmtNVByte2`(hidden/미노출), `WheaEinj`(hidden/미노출), `WheaSupport`(hidden/미노출)
- `./NetworkSettingsRef` (12): `PxeDev10EnDis`(hidden/미노출), `PxeDev11EnDis`(hidden/미노출), `PxeDev12EnDis`(hidden/미노출), `PxeDev13EnDis`(hidden/미노출), `PxeDev14EnDis`(hidden/미노출), `PxeDev15EnDis`(hidden/미노출), `PxeDev16EnDis`(hidden/미노출), `PxeDev5EnDis`(hidden/미노출), `PxeDev6EnDis`(hidden/미노출), `PxeDev7EnDis`(hidden/미노출), `PxeDev8EnDis`(hidden/미노출), `PxeDev9EnDis`(hidden/미노출)
- `./NetworkSettingsRef/HttpDev1SettingsRef` (7): `HttpDev1DhcpEnDis`, `HttpDev1Dns1`, `HttpDev1Dns2`, `HttpDev1DnsDhcpEnDis`, `HttpDev1Gateway`, `HttpDev1Ip`, `HttpDev1Mask`
- `./NetworkSettingsRef/HttpDev1SettingsRef/HttpDev1TlsConfigRef` (1): `HttpDev1TlsMode`
- `./NetworkSettingsRef/HttpDev2SettingsRef` (7): `HttpDev2DhcpEnDis`, `HttpDev2Dns1`, `HttpDev2Dns2`, `HttpDev2DnsDhcpEnDis`, `HttpDev2Gateway`, `HttpDev2Ip`, `HttpDev2Mask`
- `./NetworkSettingsRef/HttpDev2SettingsRef/HttpDev2TlsConfigRef` (1): `HttpDev2TlsMode`
- `./NetworkSettingsRef/HttpDev3SettingsRef` (7): `HttpDev3DhcpEnDis`, `HttpDev3Dns1`, `HttpDev3Dns2`, `HttpDev3DnsDhcpEnDis`, `HttpDev3Gateway`, `HttpDev3Ip`, `HttpDev3Mask`
- `./NetworkSettingsRef/HttpDev3SettingsRef/HttpDev3TlsConfigRef` (1): `HttpDev3TlsMode`
- `./NetworkSettingsRef/HttpDev4SettingsRef` (7): `HttpDev4DhcpEnDis`, `HttpDev4Dns1`, `HttpDev4Dns2`, `HttpDev4DnsDhcpEnDis`, `HttpDev4Gateway`, `HttpDev4Ip`, `HttpDev4Mask`
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
- `./ProcSettingsRef` (62): `AvxIccpPregrant`, `ControlledTurboMinusBin`, `CpuInterconnectBusSpeed`, `CpuMinSevAsid`(hidden/미노출), `DeadLineLlcAlloc`, `DirectoryAtoS`, `DirectoryMode`(hidden/미노출), `FastGoConfig`(hidden/미노출), `ImcInterleave`(hidden/미노출), `IrqThrottle`(hidden/미노출), `L2RfoPrefetch`(hidden/미노출), `LlcPrefetch`, `NumaDistanceEnum`(hidden/미노출), `PPINControlOverride`(hidden/미노출), `PackageRAPLLimitCsr`(hidden/미노출), `Proc1ControlledTurbo`(hidden/미노출), `Proc1ControlledTurboMinusBin`(hidden/미노출), `Proc1Cores`(hidden/미노출), `Proc1MaxMemoryCapacity`, `Proc1Microcode`, `Proc2Brand`, `Proc2ControlledTurbo`(hidden/미노출), `Proc2ControlledTurboMinusBin`(hidden/미노출), `Proc2Cores`(hidden/미노출), `Proc2Id`, `Proc2L2Cache`, `Proc2L3Cache`, `Proc2MaxMemoryCapacity`, `Proc2Microcode`, `Proc2NumCores`, `Proc3Brand`(hidden/미노출), `Proc3ControlledTurbo`(hidden/미노출), `Proc3ControlledTurboMinusBin`(hidden/미노출), `Proc3Cores`(hidden/미노출), `Proc3Id`(hidden/미노출), `Proc3L2Cache`(hidden/미노출), `Proc3L3Cache`(hidden/미노출), `Proc3MaxMemoryCapacity`(hidden/미노출), `Proc3Microcode`(hidden/미노출), `Proc3NumCores`(hidden/미노출), `Proc4Brand`(hidden/미노출), `Proc4ControlledTurbo`(hidden/미노출), `Proc4ControlledTurboMinusBin`(hidden/미노출), `Proc4Cores`(hidden/미노출), `Proc4Id`(hidden/미노출), `Proc4L2Cache`(hidden/미노출), `Proc4L3Cache`(hidden/미노출), `Proc4MaxMemoryCapacity`(hidden/미노출), `Proc4Microcode`(hidden/미노출), `Proc4NumCores`(hidden/미노출), `ProcAts`(hidden/미노출), `ProcBusSpeed`, `ProcExecuteDisable`(hidden/미노출), `ProcFlexRatioOverride`(hidden/미노출), `ProcFlexRatioSetting`(hidden/미노출), `ProcIssSetting`(hidden/미노출), `ProcessorActivePbf`(hidden/미노출), `ProcessorRaplPrioritization`(hidden/미노출), `RtidSetting`(hidden/미노출), `TurboPowerLimitLock`(hidden/미노출), `UpiPrefetch`, `WFRWAEnableOverride`(hidden/미노출)
- `./RedundantOsControlRef` (2): `RedundantOsBoot`, `RedundantOsState`
- `./SysInformationRef` (1): `SystemCpld2Version`(hidden/미노출)
- `./SysProfileSettingsRef` (8): `OsAcpiCx`(hidden/미노출), `PchPcieAspm`(hidden/미노출), `PmCRQoS`(hidden/미노출), `PmNVMPerformanceSetting`(hidden/미노출), `Proc2TurboCoreNum`, `Proc3TurboCoreNum`(hidden/미노출), `Proc4TurboCoreNum`(hidden/미노출), `ProcessorEist`(hidden/미노출)
- `./SysSecurityRef` (17): `AuthorizeDeviceFirmware`, `BootmanagerPassword`(hidden/미노출), `InBandManageabilityInterface`, `IntelSgx`(hidden/미노출), `NmiButton`(hidden/미노출), `OldSetupPassword`(hidden/미노출), `OldSysPassword`(hidden/미노출), `SgxLEPubKeyHash0`(hidden/미노출), `SgxLEPubKeyHash1`(hidden/미노출), `SgxLEPubKeyHash2`(hidden/미노출), `SgxLEPubKeyHash3`(hidden/미노출), `SgxLcp`(hidden/미노출), `Tpm2Hierarchy`, `TpmCommand`(hidden/미노출), `TpmFirmware`, `TpmSecurity`, `TpmStatus`(hidden/미노출)
- `./SysSecurityRef/TpmAdvancedSettingsRef` (1): `Tpm2Algorithm`(hidden/미노출)

#### 삭제 (A에만 존재) (47)

- `./DebugMenuRef` (32): `AttemptFastBoot`, `AttemptFastBootCold`, `BugChecking`, `CurrentLimit`, `DciTransport`, `DebugErrorLevel`(hidden/미노출), `EmbSataRSTeDebug`, `EmbSataTestMode`, `IdracDebugMode`, `IgnoreIdracCrReq`, `JunoPmEnable`, `MRCSerialDbgOut`, `MeFailureRecoveryEnable`, `MeUmaEnable`, `MemHotThrottlingMode`, `MemTestOnFastBoot`, `MemoryRmt`, `MemoryThrottlingMode`, `MultiThreaded`, `NdcConfigurationSpeed`, `PCIeErrorInjection`, `PCIeLiveErrorRecovery`, `PPRErrInjectionTest`, `PostPackageRepair`, `ProcDpatProDebug`, `ProcMtrrPatDebug`, `RebootTestCount`, `RebootTestMode`, `RebootTestPoint`, `SccDebugEnabled`, `TpmBindingReset`, `TraceHubDebug`
- `./DebugMenuRef/BrowserOptionsRef` (3): `BrowserDebugMode`, `BrowserMode`, `InteractivePassword24A`
- `./DebugMenuRef/DebugMenuIioConfigurationSettingsRef` (9): `CTOMasking`, `DeviceUnhide`, `Dfx`, `DirectMediaInterfaceSpeed`, `IioPcieGlobalSpeed`, `IntelTestEventIio`, `LinkDowntrainReporting`, `TXEQWA`, `UnusedPcieClk`
- `./SysProfileSettingsRef` (2): `EnergyEfficientTurbo`, `PowerSaver`
- `./SysSecurityRef` (1): `SecureMePciCfgSpace`

### type 변경

| 속성 | A | B |
|---|---|---|
| `IscsiDev1Con1ChapSecret` | String | Password |
| `IscsiDev1Con1RevChapSecret` | String | Password |
| `IscsiDev1Con2ChapSecret` | String | Password |
| `IscsiDev1Con2RevChapSecret` | String | Password |

### allowed_values 변경 (Enumeration, 전체)

| 속성 | A 에만 있는 값 | B 에만 있는 값 | B 전체 값 |
|---|---|---|---|
| `BootSeqRetry` | - | Reset | Enabled, Disabled, Reset |
| `MemFrequency` | - | 2933MHz | MaxPerf, 2933MHz, 2666MHz, 2400MHz, 2133MHz, 1866MHz, MaxReliability |
| `MemOpMode` | - | SingleRankSpareMode, MultiRankSpareMode | OptimizerMode, SingleRankSpareMode, MultiRankSpareMode |
| `OneTimeUefiBootSeqDev` | Unknown.Unknown.1-1, NIC.PxeDevice.1-1, NIC.PxeDevice.2-1, Unknown.Unknown.4-1, Unknown.Unknown.5-1, Disk.SATAEmbedded.A-1, Disk.SATAEmbedded.A-1 | AHCI.Slot.1-2, GenericUSB.Placeholder.1-1, Disk.USBBack.2-1 | AHCI.Slot.1-2, GenericUSB.Placeholder.1-1, Disk.USBBack.2-1 |
| `ProcCStates` | - | Autonomous | Enabled, Disabled, Autonomous |
| `ProcCores` | - | 16, 18, 20, 22 | All, 1, 2, 4, 6, 8, 10, 12, 14, 16, 18, 20, 22 |
| `ProcPwrPerf` | HwpDbpm | - | SysDbpm, OsDbpm, MaxPerf |
| `RedundantOsLocation` | PortA | Slot1 | None, Slot1 |
| `SecureBootMode` | AuditMode | - | UserMode, DeployedMode |
| `Slot1Bif` | - | x16 | x16, x4, x8, x4x4x8, x8x4x4 |
| `Slot2` | Disabled | - | Enabled, BootDriverDisabled |
| `Slot3Bif` | - | x16, x4x4x8, x8x4x4 | x16, x4, x8, x4x4x8, x8x4x4 |
| `Slot4Bif` | - | x16, x4x4x8, x8x4x4 | x16, x4, x8, x4x4x8, x8x4x4 |
| `SubNumaCluster` | Enabled | - | Disabled |
| `SysProfile` | PerfOptimizedHwp | - | PerfPerWattOptimizedDapc, PerfPerWattOptimizedOs, PerfOptimized, PerfWorkStationOptimized, Custom |
| `WorkloadProfile` | SsdOptimizedProfile, SsdPerWattOptimizedProfile | SdsOptimizedProfile, SdsPerWattOptimizedProfile | NotAvailable, HpcProfile, LowLatencyOptimizedProfile, VtOptimizedProfile, VtPerWattOptimizedProfile, DbOptimizedProfile, DbPerWattOptimizedProfile, SdsOptimizedProfile, SdsPerWattOptimizedProfile |

### 정수 범위 변경

| 속성 | A (min/max/step) | B (min/max/step) |
|---|---|---|
| `AcPwrRcvryUserDelay` | 60/240/0 | 60/600/0 |

### DisplayName 변경

| 속성 | A | B |
|---|---|---|
| `AcPwrRcvryUserDelay` | User Defined Delay (60s to 240s) | User Defined Delay (60s to 600s) |
| `AesNi` | Intel(R) AES-NI | CPU AES-NI |
| `NvdimmFactoryDefault` | NVDIMM Factory Default All Dimms | Sanitize All NVDIMMs |
| `ProcCStates` | C States | C-States |
| `ProcX2Apic` | X2Apic Mode | x2APIC Mode |


## 15G: iDRAC 5.10.00.00 (BIOS 1.2.4) -> iDRAC 7.10.30.00 (BIOS 1.12.1), 동일 모델 R650

- 이전(A): PowerEdge R650 (iDRAC 5.10.00.00, BIOS 1.2.4) — 레지스트리 538개, /Bios 노출 344개
- 이후(B): PowerEdge R650 (iDRAC 7.10.30.00, BIOS 1.12.1) — 레지스트리 686개, /Bios 노출 440개
- 비교 방법: 두 캡처의 `GET /redfish/v1/Systems/System.Embedded.1/Bios/BiosRegistry` 원본 JSON 을 스크립트로 비교(요약 도구 미사용). 이름 기준; 이름 변경은 DisplayName 이 같고 이름이 다른 쌍으로 추정 표시.
- 주의: redfish_simgen 템플릿은 ServiceTag 가 {{SERVICE_TAG}} 로 치환된 실캡처 기반 템플릿.

### 요약

| 항목 | 개수 |
|---|---|
| 추가 (B에만) | 149 |
| 삭제 (A에만) | 1 |
| 공통 | 537 |
| type 변경 | 0 |
| allowed_values 변경(Enumeration) | 37 |
| 정수 범위 변경 | 1 |
| DisplayName 변경 | 3 |
| default 변경 | 비교 불가 (Dell 레지스트리에 DefaultValue 없음) |

### 이름변경 후보 (DisplayName 동일, 이름 상이)

없음

### 추가/삭제 목록 (MenuPath 별, 전체)

#### 추가 (B에만 존재) (149)

- `./MemSettingsRef` (1): `PPROnUCE`
- `./MemSettingsRef/MemoryMapOutRef` (32): `DimmSlot00`, `DimmSlot01`, `DimmSlot02`, `DimmSlot03`, `DimmSlot04`, `DimmSlot05`, `DimmSlot06`, `DimmSlot07`, `DimmSlot08`, `DimmSlot09`, `DimmSlot10`, `DimmSlot11`, `DimmSlot12`, `DimmSlot13`, `DimmSlot14`, `DimmSlot15`, `DimmSlot16`, `DimmSlot17`, `DimmSlot18`, `DimmSlot19`, `DimmSlot20`, `DimmSlot21`, `DimmSlot22`, `DimmSlot23`, `DimmSlot24`, `DimmSlot25`, `DimmSlot26`, `DimmSlot27`, `DimmSlot28`, `DimmSlot29`, `DimmSlot30`, `DimmSlot31`
- `./NetworkSettingsRef` (13): `NumberOfPxeDevices`(hidden/미노출), `PxeDev10EnDis`(hidden/미노출), `PxeDev11EnDis`(hidden/미노출), `PxeDev12EnDis`(hidden/미노출), `PxeDev13EnDis`(hidden/미노출), `PxeDev14EnDis`(hidden/미노출), `PxeDev15EnDis`(hidden/미노출), `PxeDev16EnDis`(hidden/미노출), `PxeDev5EnDis`(hidden/미노출), `PxeDev6EnDis`(hidden/미노출), `PxeDev7EnDis`(hidden/미노출), `PxeDev8EnDis`(hidden/미노출), `PxeDev9EnDis`(hidden/미노출)
- `./NetworkSettingsRef/HttpDev1SettingsRef` (8): `HttpDev1v6Address`(hidden/미노출), `HttpDev1v6AutoConfig`(hidden/미노출), `HttpDev1v6Dns1`(hidden/미노출), `HttpDev1v6Dns2`(hidden/미노출), `HttpDev1v6DnsDhcpEnDis`(hidden/미노출), `HttpDev1v6Gateway`(hidden/미노출), `HttpDev1v6PrefixLen`(hidden/미노출), `HttpDev1v6Uri`(hidden/미노출)
- `./NetworkSettingsRef/HttpDev2SettingsRef` (8): `HttpDev2v6Address`(hidden/미노출), `HttpDev2v6AutoConfig`(hidden/미노출), `HttpDev2v6Dns1`(hidden/미노출), `HttpDev2v6Dns2`(hidden/미노출), `HttpDev2v6DnsDhcpEnDis`(hidden/미노출), `HttpDev2v6Gateway`(hidden/미노출), `HttpDev2v6PrefixLen`(hidden/미노출), `HttpDev2v6Uri`(hidden/미노출)
- `./NetworkSettingsRef/HttpDev3SettingsRef` (8): `HttpDev3v6Address`(hidden/미노출), `HttpDev3v6AutoConfig`(hidden/미노출), `HttpDev3v6Dns1`(hidden/미노출), `HttpDev3v6Dns2`(hidden/미노출), `HttpDev3v6DnsDhcpEnDis`(hidden/미노출), `HttpDev3v6Gateway`(hidden/미노출), `HttpDev3v6PrefixLen`(hidden/미노출), `HttpDev3v6Uri`(hidden/미노출)
- `./NetworkSettingsRef/HttpDev4SettingsRef` (8): `HttpDev4v6Address`(hidden/미노출), `HttpDev4v6AutoConfig`(hidden/미노출), `HttpDev4v6Dns1`(hidden/미노출), `HttpDev4v6Dns2`(hidden/미노출), `HttpDev4v6DnsDhcpEnDis`(hidden/미노출), `HttpDev4v6Gateway`(hidden/미노출), `HttpDev4v6PrefixLen`(hidden/미노출), `HttpDev4v6Uri`(hidden/미노출)
- `./NetworkSettingsRef/PxeDev10SettingsRef` (5): `PxeDev10Interface`, `PxeDev10Protocol`, `PxeDev10VlanEnDis`, `PxeDev10VlanId`, `PxeDev10VlanPriority`
- `./NetworkSettingsRef/PxeDev11SettingsRef` (5): `PxeDev11Interface`, `PxeDev11Protocol`, `PxeDev11VlanEnDis`, `PxeDev11VlanId`, `PxeDev11VlanPriority`
- `./NetworkSettingsRef/PxeDev12SettingsRef` (5): `PxeDev12Interface`, `PxeDev12Protocol`, `PxeDev12VlanEnDis`, `PxeDev12VlanId`, `PxeDev12VlanPriority`
- `./NetworkSettingsRef/PxeDev13SettingsRef` (5): `PxeDev13Interface`, `PxeDev13Protocol`, `PxeDev13VlanEnDis`, `PxeDev13VlanId`, `PxeDev13VlanPriority`
- `./NetworkSettingsRef/PxeDev14SettingsRef` (5): `PxeDev14Interface`, `PxeDev14Protocol`, `PxeDev14VlanEnDis`, `PxeDev14VlanId`, `PxeDev14VlanPriority`
- `./NetworkSettingsRef/PxeDev15SettingsRef` (5): `PxeDev15Interface`, `PxeDev15Protocol`, `PxeDev15VlanEnDis`, `PxeDev15VlanId`, `PxeDev15VlanPriority`
- `./NetworkSettingsRef/PxeDev16SettingsRef` (5): `PxeDev16Interface`, `PxeDev16Protocol`, `PxeDev16VlanEnDis`, `PxeDev16VlanId`, `PxeDev16VlanPriority`
- `./NetworkSettingsRef/PxeDev5SettingsRef` (5): `PxeDev5Interface`, `PxeDev5Protocol`, `PxeDev5VlanEnDis`, `PxeDev5VlanId`, `PxeDev5VlanPriority`
- `./NetworkSettingsRef/PxeDev6SettingsRef` (5): `PxeDev6Interface`, `PxeDev6Protocol`, `PxeDev6VlanEnDis`, `PxeDev6VlanId`, `PxeDev6VlanPriority`
- `./NetworkSettingsRef/PxeDev7SettingsRef` (5): `PxeDev7Interface`, `PxeDev7Protocol`, `PxeDev7VlanEnDis`, `PxeDev7VlanId`, `PxeDev7VlanPriority`
- `./NetworkSettingsRef/PxeDev8SettingsRef` (5): `PxeDev8Interface`, `PxeDev8Protocol`, `PxeDev8VlanEnDis`, `PxeDev8VlanId`, `PxeDev8VlanPriority`
- `./NetworkSettingsRef/PxeDev9SettingsRef` (5): `PxeDev9Interface`, `PxeDev9Protocol`, `PxeDev9VlanEnDis`, `PxeDev9VlanId`, `PxeDev9VlanPriority`
- `./ProcSettingsRef` (6): `CpuFeatureErms`(hidden/미노출), `CpuFeatureFsrm`(hidden/미노출), `CpuFeatureRmss`(hidden/미노출), `CpuPaLimit`, `KernelDmaProtection`, `Rmp`(hidden/미노출)
- `./SysProfileSettingsRef` (2): `EnergyEfficientTurbo`, `PwrPerfSwitch`
- `./SysSecurityRef` (3): `DrtmSkinit`(hidden/미노출), `StrongPassword`(hidden/미노출), `StrongPasswordMinLength`(hidden/미노출)

#### 삭제 (A에만 존재) (1)

- `./MemSettingsRef` (1): `DarkMemoryAvailableMem`(hidden/미노출)

### allowed_values 변경 (Enumeration, 전체)

| 속성 | A 에만 있는 값 | B 에만 있는 값 | B 전체 값 |
|---|---|---|---|
| `AdddcSetting` | - | Enabled | Enabled, Disabled |
| `DynamicL1` | - | Enabled | Disabled, Enabled |
| `EnablePkgcCriteria` | - | Enabled | Disabled, Enabled |
| `HttpDev1Interface` | - | NIC.Slot.3-1-1, NIC.Slot.3-2-1, NIC.Slot.3-3-1, NIC.Slot.3-4-1 | NIC.Embedded.1-1-1, NIC.Embedded.2-1-1, NIC.Integrated.1-1-1, NIC.Integrated.1-2-1, NIC.Slot.3-1-1, NIC.Slot.3-2-1, NIC.Slot.3-3-1, NIC.Slot.3-4-1 |
| `HttpDev2Interface` | - | NIC.Slot.3-1-1, NIC.Slot.3-2-1, NIC.Slot.3-3-1, NIC.Slot.3-4-1 | NIC.Embedded.1-1-1, NIC.Embedded.2-1-1, NIC.Integrated.1-1-1, NIC.Integrated.1-2-1, NIC.Slot.3-1-1, NIC.Slot.3-2-1, NIC.Slot.3-3-1, NIC.Slot.3-4-1 |
| `HttpDev3Interface` | - | NIC.Slot.3-1-1, NIC.Slot.3-2-1, NIC.Slot.3-3-1, NIC.Slot.3-4-1 | NIC.Embedded.1-1-1, NIC.Embedded.2-1-1, NIC.Integrated.1-1-1, NIC.Integrated.1-2-1, NIC.Slot.3-1-1, NIC.Slot.3-2-1, NIC.Slot.3-3-1, NIC.Slot.3-4-1 |
| `HttpDev4Interface` | - | NIC.Slot.3-1-1, NIC.Slot.3-2-1, NIC.Slot.3-3-1, NIC.Slot.3-4-1 | NIC.Embedded.1-1-1, NIC.Embedded.2-1-1, NIC.Integrated.1-1-1, NIC.Integrated.1-2-1, NIC.Slot.3-1-1, NIC.Slot.3-2-1, NIC.Slot.3-3-1, NIC.Slot.3-4-1 |
| `IscsiDev1Con1Interface` | - | NIC.Slot.3-1-1, NIC.Slot.3-2-1, NIC.Slot.3-3-1, NIC.Slot.3-4-1 | NIC.Embedded.1-1-1, NIC.Embedded.2-1-1, NIC.Integrated.1-1-1, NIC.Integrated.1-2-1, NIC.Slot.3-1-1, NIC.Slot.3-2-1, NIC.Slot.3-3-1, NIC.Slot.3-4-1 |
| `IscsiDev1Con2Interface` | - | NIC.Slot.3-1-1, NIC.Slot.3-2-1, NIC.Slot.3-3-1, NIC.Slot.3-4-1 | NIC.Embedded.1-1-1, NIC.Embedded.2-1-1, NIC.Integrated.1-1-1, NIC.Integrated.1-2-1, NIC.Slot.3-1-1, NIC.Slot.3-2-1, NIC.Slot.3-3-1, NIC.Slot.3-4-1 |
| `MemFrequency` | - | MaxReliability | MaxPerf, MaxReliability |
| `MemOpMode` | FaultResilientMode, NUMAFaultResilientMode | - | OptimizerMode |
| `MemoryEncryption` | Enabled | MultipleKeys, SingleKey | MultipleKeys, SingleKey, Disabled |
| `NodeInterleave` | Enabled | - | Disabled |
| `OneTimeUefiBootSeqDev` | NIC.PxeDevice.1-1 | RAID.SL.3-2, Unknown.Unknown.3-1, Unknown.Unknown.4-1, Unknown.Unknown.5-1 | AHCI.SL.6-2, RAID.SL.3-2, Unknown.Unknown.3-1, Unknown.Unknown.4-1, Unknown.Unknown.5-1 |
| `OsAcpiCx` | - | OsCxC3 | OsCxC2, OsCxC3 |
| `PackageCStates` | - | Disabled | Disabled, Enabled |
| `PkgCLatNeg` | - | Enabled | Disabled, Enabled |
| `PmNVMPerformanceSetting` | - | PmLatencyOptimized | PmBWOptimized, PmBalancedProfile, PmLatencyOptimized |
| `ProcessorC1AutoDemotion` | - | Enabled | Disabled, Enabled |
| `ProcessorC1AutoUnDemotion` | - | Enabled | Disabled, Enabled |
| `ProcessorEist` | - | Disabled | Enabled, Disabled |
| `ProcessorGpssTimer` | - | 0us | 0us, 500us |
| `PxeDev1Interface` | - | NIC.Slot.3-1-1, NIC.Slot.3-2-1, NIC.Slot.3-3-1, NIC.Slot.3-4-1 | NIC.Embedded.1-1-1, NIC.Embedded.2-1-1, NIC.Integrated.1-1-1, NIC.Integrated.1-2-1, NIC.Slot.3-1-1, NIC.Slot.3-2-1, NIC.Slot.3-3-1, NIC.Slot.3-4-1 |
| `PxeDev2Interface` | - | NIC.Slot.3-1-1, NIC.Slot.3-2-1, NIC.Slot.3-3-1, NIC.Slot.3-4-1 | NIC.Embedded.1-1-1, NIC.Embedded.2-1-1, NIC.Integrated.1-1-1, NIC.Integrated.1-2-1, NIC.Slot.3-1-1, NIC.Slot.3-2-1, NIC.Slot.3-3-1, NIC.Slot.3-4-1 |
| `PxeDev3Interface` | - | NIC.Slot.3-1-1, NIC.Slot.3-2-1, NIC.Slot.3-3-1, NIC.Slot.3-4-1 | NIC.Embedded.1-1-1, NIC.Embedded.2-1-1, NIC.Integrated.1-1-1, NIC.Integrated.1-2-1, NIC.Slot.3-1-1, NIC.Slot.3-2-1, NIC.Slot.3-3-1, NIC.Slot.3-4-1 |
| `PxeDev4Interface` | - | NIC.Slot.3-1-1, NIC.Slot.3-2-1, NIC.Slot.3-3-1, NIC.Slot.3-4-1 | NIC.Embedded.1-1-1, NIC.Embedded.2-1-1, NIC.Integrated.1-1-1, NIC.Integrated.1-2-1, NIC.Slot.3-1-1, NIC.Slot.3-2-1, NIC.Slot.3-3-1, NIC.Slot.3-4-1 |
| `Slot1` | Disabled | - | Enabled, BootDriverDisabled |
| `Slot1Bif` | x16, x4x4x8, x8x4x4 | - | x4, x8 |
| `Slot2` | Disabled | - | Enabled, BootDriverDisabled |
| `Slot2Bif` | x16, x4x4x8, x8x4x4 | - | x4, x8 |
| `Slot3` | - | Disabled | Enabled, Disabled, BootDriverDisabled |
| `Slot3Bif` | - | x16, x4x4x8, x8x4x4 | x16, x4, x8, x4x4x8, x8x4x4 |
| `SysProfile` | PerfWorkStationOptimized | - | PerfPerWattOptimizedDapc, PerfPerWattOptimizedOs, PerfOptimized, Custom |
| `UncoreFrequency` | - | OptimizedUFS | DynamicUFS, MaxUFS, OptimizedUFS |
| `WorkloadConfiguration` | - | IoSensitive | Balance, IoSensitive |
| `WorkloadProfile` | WriteOnly, TelcoProfile | NotConfigured, TelcoOptimizedProfile | NotConfigured, HpcProfile, LowLatencyOptimizedProfile, VtOptimizedProfile, VtPerWattOptimizedProfile, DbOptimizedProfile, DbPerWattOptimizedProfile, SdsOptimizedProfile, SdsPerWattOptimizedProfile, TelcoOptimizedProfile |
| `WorkloadProfileHelper` | WriteOnly, TelcoProfile | NotConfigured, TelcoOptimizedProfile | NotConfigured, HpcProfile, LowLatencyOptimizedProfile, VtOptimizedProfile, VtPerWattOptimizedProfile, DbOptimizedProfile, DbPerWattOptimizedProfile, SdsOptimizedProfile, SdsPerWattOptimizedProfile, TelcoOptimizedProfile |

### 정수 범위 변경

| 속성 | A (min/max/step) | B (min/max/step) |
|---|---|---|
| `AcPwrRcvryUserDelay` | 60/600/0 | 120/600/0 |

### DisplayName 변경

| 속성 | A | B |
|---|---|---|
| `AcPwrRcvryUserDelay` | User Defined Delay (60s to 600s) | User Defined Delay (120s to 600s) |
| `EnablePkgcCriteria` | Power and system criteria for Package C state | Power and System Criteria for Package C State |
| `OsAcpiCx` | OS Acpi Cx | OS ACPI Cx |


## 15G: iDRAC 6.00.30.00 (BIOS 1.8.2) -> iDRAC 7.10.30.00 (BIOS 1.13.2), 동일 모델 R750 (사용자 보유)

- 이전(A): PowerEdge R750 (iDRAC 6.00.30.00, BIOS 1.8.2) — 레지스트리 682개, /Bios 노출 455개
- 이후(B): PowerEdge R750 (iDRAC 7.10.30.00, BIOS 1.13.2) — 레지스트리 687개, /Bios 노출 454개
- 비교 방법: 두 캡처의 `GET /redfish/v1/Systems/System.Embedded.1/Bios/BiosRegistry` 원본 JSON 을 스크립트로 비교(요약 도구 미사용). 이름 기준; 이름 변경은 DisplayName 이 같고 이름이 다른 쌍으로 추정 표시.
- 주의: 동일 모델 실캡처 2개.

### 요약

| 항목 | 개수 |
|---|---|
| 추가 (B에만) | 5 |
| 삭제 (A에만) | 0 |
| 공통 | 682 |
| type 변경 | 4 |
| allowed_values 변경(Enumeration) | 26 |
| 정수 범위 변경 | 0 |
| DisplayName 변경 | 8 |
| default 변경 | 비교 불가 (Dell 레지스트리에 DefaultValue 없음) |

### 이름변경 후보 (DisplayName 동일, 이름 상이)

없음

### 추가/삭제 목록 (MenuPath 별, 전체)

#### 추가 (B에만 존재) (5)

- `./SysProfileSettingsRef` (3): `Cppc`(hidden/미노출), `EnergyEfficientTurbo`(hidden/미노출), `PwrPerfSwitch`
- `./SysSecurityRef` (2): `StrongPassword`(hidden/미노출), `StrongPasswordMinLength`(hidden/미노출)

#### 삭제 (A에만 존재) (0)

없음

### type 변경

| 속성 | A | B |
|---|---|---|
| `HttpDev1v6PrefixLen` | String | Integer |
| `HttpDev2v6PrefixLen` | String | Integer |
| `HttpDev3v6PrefixLen` | String | Integer |
| `HttpDev4v6PrefixLen` | String | Integer |

### allowed_values 변경 (Enumeration, 전체)

| 속성 | A 에만 있는 값 | B 에만 있는 값 | B 전체 값 |
|---|---|---|---|
| `HttpDev1Interface` | InfiniBand.Slot.7-1, InfiniBand.Slot.7-2, NIC.Integrated.1-1-1, NIC.Integrated.1-2-1 | NIC.Slot.2-1 | NIC.Embedded.1-1-1, NIC.Embedded.2-1-1, NIC.Slot.2-1, NIC.Slot.5-1 |
| `HttpDev2Interface` | InfiniBand.Slot.7-1, InfiniBand.Slot.7-2, NIC.Integrated.1-1-1, NIC.Integrated.1-2-1 | NIC.Slot.2-1 | NIC.Embedded.1-1-1, NIC.Embedded.2-1-1, NIC.Slot.2-1, NIC.Slot.5-1 |
| `HttpDev3Interface` | InfiniBand.Slot.7-1, InfiniBand.Slot.7-2, NIC.Integrated.1-1-1, NIC.Integrated.1-2-1 | NIC.Slot.2-1 | NIC.Embedded.1-1-1, NIC.Embedded.2-1-1, NIC.Slot.2-1, NIC.Slot.5-1 |
| `HttpDev4Interface` | InfiniBand.Slot.7-1, InfiniBand.Slot.7-2, NIC.Integrated.1-1-1, NIC.Integrated.1-2-1 | NIC.Slot.2-1 | NIC.Embedded.1-1-1, NIC.Embedded.2-1-1, NIC.Slot.2-1, NIC.Slot.5-1 |
| `IscsiDev1Con1Interface` | InfiniBand.Slot.7-1, InfiniBand.Slot.7-2, NIC.Integrated.1-1-1, NIC.Integrated.1-2-1 | NIC.Slot.2-1 | NIC.Embedded.1-1-1, NIC.Embedded.2-1-1, NIC.Slot.2-1, NIC.Slot.5-1 |
| `IscsiDev1Con2Interface` | InfiniBand.Slot.7-1, InfiniBand.Slot.7-2, NIC.Integrated.1-1-1, NIC.Integrated.1-2-1 | NIC.Slot.2-1 | NIC.Embedded.1-1-1, NIC.Embedded.2-1-1, NIC.Slot.2-1, NIC.Slot.5-1 |
| `MemOpMode` | FaultResilientMode, NUMAFaultResilientMode | - | OptimizerMode |
| `OneTimeUefiBootSeqDev` | Unknown.Unknown.3-1, Unknown.Unknown.4-1, Disk.Bay.2:Enclosure.Internal.0-1 | - | NIC.HttpDevice.1-1, Unknown.Unknown.2-1 |
| `PrmrrSize` | 64G | - | 2G, 4G, 8G, 16G, 32G |
| `PxeDev10Interface` | InfiniBand.Slot.7-1, InfiniBand.Slot.7-2, NIC.Integrated.1-1-1, NIC.Integrated.1-2-1 | NIC.Slot.2-1 | NIC.Embedded.1-1-1, NIC.Embedded.2-1-1, NIC.Slot.2-1, NIC.Slot.5-1 |
| `PxeDev11Interface` | InfiniBand.Slot.7-1, InfiniBand.Slot.7-2, NIC.Integrated.1-1-1, NIC.Integrated.1-2-1 | NIC.Slot.2-1 | NIC.Embedded.1-1-1, NIC.Embedded.2-1-1, NIC.Slot.2-1, NIC.Slot.5-1 |
| `PxeDev12Interface` | InfiniBand.Slot.7-1, InfiniBand.Slot.7-2, NIC.Integrated.1-1-1, NIC.Integrated.1-2-1 | NIC.Slot.2-1 | NIC.Embedded.1-1-1, NIC.Embedded.2-1-1, NIC.Slot.2-1, NIC.Slot.5-1 |
| `PxeDev13Interface` | InfiniBand.Slot.7-1, InfiniBand.Slot.7-2, NIC.Integrated.1-1-1, NIC.Integrated.1-2-1 | NIC.Slot.2-1 | NIC.Embedded.1-1-1, NIC.Embedded.2-1-1, NIC.Slot.2-1, NIC.Slot.5-1 |
| `PxeDev14Interface` | InfiniBand.Slot.7-1, InfiniBand.Slot.7-2, NIC.Integrated.1-1-1, NIC.Integrated.1-2-1 | NIC.Slot.2-1 | NIC.Embedded.1-1-1, NIC.Embedded.2-1-1, NIC.Slot.2-1, NIC.Slot.5-1 |
| `PxeDev15Interface` | InfiniBand.Slot.7-1, InfiniBand.Slot.7-2, NIC.Integrated.1-1-1, NIC.Integrated.1-2-1 | NIC.Slot.2-1 | NIC.Embedded.1-1-1, NIC.Embedded.2-1-1, NIC.Slot.2-1, NIC.Slot.5-1 |
| `PxeDev16Interface` | InfiniBand.Slot.7-1, InfiniBand.Slot.7-2, NIC.Integrated.1-1-1, NIC.Integrated.1-2-1 | NIC.Slot.2-1 | NIC.Embedded.1-1-1, NIC.Embedded.2-1-1, NIC.Slot.2-1, NIC.Slot.5-1 |
| `PxeDev1Interface` | InfiniBand.Slot.7-1, InfiniBand.Slot.7-2, NIC.Integrated.1-1-1, NIC.Integrated.1-2-1 | NIC.Slot.2-1 | NIC.Embedded.1-1-1, NIC.Embedded.2-1-1, NIC.Slot.2-1, NIC.Slot.5-1 |
| `PxeDev2Interface` | InfiniBand.Slot.7-1, InfiniBand.Slot.7-2, NIC.Integrated.1-1-1, NIC.Integrated.1-2-1 | NIC.Slot.2-1 | NIC.Embedded.1-1-1, NIC.Embedded.2-1-1, NIC.Slot.2-1, NIC.Slot.5-1 |
| `PxeDev3Interface` | InfiniBand.Slot.7-1, InfiniBand.Slot.7-2, NIC.Integrated.1-1-1, NIC.Integrated.1-2-1 | NIC.Slot.2-1 | NIC.Embedded.1-1-1, NIC.Embedded.2-1-1, NIC.Slot.2-1, NIC.Slot.5-1 |
| `PxeDev4Interface` | InfiniBand.Slot.7-1, InfiniBand.Slot.7-2, NIC.Integrated.1-1-1, NIC.Integrated.1-2-1 | NIC.Slot.2-1 | NIC.Embedded.1-1-1, NIC.Embedded.2-1-1, NIC.Slot.2-1, NIC.Slot.5-1 |
| `PxeDev5Interface` | InfiniBand.Slot.7-1, InfiniBand.Slot.7-2, NIC.Integrated.1-1-1, NIC.Integrated.1-2-1 | NIC.Slot.2-1 | NIC.Embedded.1-1-1, NIC.Embedded.2-1-1, NIC.Slot.2-1, NIC.Slot.5-1 |
| `PxeDev6Interface` | InfiniBand.Slot.7-1, InfiniBand.Slot.7-2, NIC.Integrated.1-1-1, NIC.Integrated.1-2-1 | NIC.Slot.2-1 | NIC.Embedded.1-1-1, NIC.Embedded.2-1-1, NIC.Slot.2-1, NIC.Slot.5-1 |
| `PxeDev7Interface` | InfiniBand.Slot.7-1, InfiniBand.Slot.7-2, NIC.Integrated.1-1-1, NIC.Integrated.1-2-1 | NIC.Slot.2-1 | NIC.Embedded.1-1-1, NIC.Embedded.2-1-1, NIC.Slot.2-1, NIC.Slot.5-1 |
| `PxeDev8Interface` | InfiniBand.Slot.7-1, InfiniBand.Slot.7-2, NIC.Integrated.1-1-1, NIC.Integrated.1-2-1 | NIC.Slot.2-1 | NIC.Embedded.1-1-1, NIC.Embedded.2-1-1, NIC.Slot.2-1, NIC.Slot.5-1 |
| `PxeDev9Interface` | InfiniBand.Slot.7-1, InfiniBand.Slot.7-2, NIC.Integrated.1-1-1, NIC.Integrated.1-2-1 | NIC.Slot.2-1 | NIC.Embedded.1-1-1, NIC.Embedded.2-1-1, NIC.Slot.2-1, NIC.Slot.5-1 |
| `UncoreFrequency` | - | OptimizedUFS | DynamicUFS, MaxUFS, OptimizedUFS |

### DisplayName 변경

| 속성 | A | B |
|---|---|---|
| `DimmSlot04` | Dimm Slot A5 | Dimm Slot A5 - Not Installed |
| `DimmSlot05` | Dimm Slot A6 | Dimm Slot A6 - Not Installed |
| `DimmSlot06` | Dimm Slot A7 | Dimm Slot A7 - Not Installed |
| `DimmSlot07` | Dimm Slot A8 | Dimm Slot A8 - Not Installed |
| `DimmSlot20` | Dimm Slot B5 | Dimm Slot B5 - Not Installed |
| `DimmSlot21` | Dimm Slot B6 | Dimm Slot B6 - Not Installed |
| `DimmSlot22` | Dimm Slot B7 | Dimm Slot B7 - Not Installed |
| `DimmSlot23` | Dimm Slot B8 | Dimm Slot B8 - Not Installed |


## 16G: iDRAC 7.10.50.00 (R760xa BIOS 2.1.3) -> iDRAC 7.20.10.05 (R660xs BIOS 2.4.4)

- 이전(A): PowerEdge R760xa (iDRAC 7.10.50.00, BIOS 2.1.3) — 레지스트리 865개, /Bios 노출 538개
- 이후(B): PowerEdge R660xs (iDRAC 7.20.10.05, BIOS 2.4.4) — 레지스트리 870개, /Bios 노출 538개
- 비교 방법: 두 캡처의 `GET /redfish/v1/Systems/System.Embedded.1/Bios/BiosRegistry` 원본 JSON 을 스크립트로 비교(요약 도구 미사용). 이름 기준; 이름 변경은 DisplayName 이 같고 이름이 다른 쌍으로 추정 표시.
- 주의: 모델이 다름(R660xs 는 xs 계열 BIOS). 펌웨어 차이와 모델 차이가 섞여 있음 -> 참고용.

### 요약

| 항목 | 개수 |
|---|---|
| 추가 (B에만) | 12 |
| 삭제 (A에만) | 7 |
| 공통 | 858 |
| type 변경 | 0 |
| allowed_values 변경(Enumeration) | 51 |
| 정수 범위 변경 | 0 |
| DisplayName 변경 | 26 |
| default 변경 | 비교 불가 (Dell 레지스트리에 DefaultValue 없음) |

### 이름변경 후보 (DisplayName 동일, 이름 상이)

| A 이름 | B 이름 | DisplayName |
|---|---|---|
| `NvmeofHostNqn` | `NvmeofHostCustomNqn` | NVMe-oF Host NQN |
| `NvmeofHostNqn` | `NvmeofHostDellNqn` | NVMe-oF Host NQN |
| `NvmeofHostNqn` | `NvmeofHostUuidNqn` | NVMe-oF Host NQN |

### 추가/삭제 목록 (MenuPath 별, 전체)

#### 추가 (B에만 존재) (12)

- `./BootSettingsRef` (1): `InteractiveMode`
- `./IntegratedDevicesRef` (1): `NicAcpi`
- `./IntegratedDevicesRef/SlotBifurcationRef` (1): `Slot41Bif`(hidden/미노출)
- `./IntegratedDevicesRef/SlotDisablementRef` (2): `Slot41`(hidden/미노출), `Slot42`(hidden/미노출)
- `./MiscSettingsRef` (2): `AcpiFpdt`, `MiscAttributesDefaulted`(hidden/미노출)
- `./NetworkSettingsRef` (4): `HostNqnMode`, `NvmeofHostCustomNqn`(hidden/미노출), `NvmeofHostDellNqn`, `NvmeofHostUuidNqn`(hidden/미노출)
- `./NetworkSettingsRef/IscsiDev1SettingsRef` (1): `IscsiF1F2ErrorPrompt`

#### 삭제 (A에만 존재) (7)

- `./IntegratedDevicesRef` (1): `IoatEngine`
- `./NetworkSettingsRef` (1): `NvmeofHostNqn`
- `./NetworkSettingsRef/IscsiDev1SettingsRef/IscsiDev1Con1SettingsRef` (2): `IscsiDev1Con1ChapSecret`, `IscsiDev1Con1RevChapSecret`
- `./NetworkSettingsRef/IscsiDev1SettingsRef/IscsiDev1Con2SettingsRef` (2): `IscsiDev1Con2ChapSecret`, `IscsiDev1Con2RevChapSecret`
- `./SysSecurityRef` (1): `TdxDisable1MbCmrExclude`

### allowed_values 변경 (Enumeration, 전체)

| 속성 | A 에만 있는 값 | B 에만 있는 값 | B 전체 값 |
|---|---|---|---|
| `CustomUncoreFrequency` | - | 2.1GHz | 2.1GHz, 2.0GHz, 1.9GHz, 1.8GHz, 1.7GHz, 1.6GHz, 1.5GHz, 1.4GHz, 1.3GHz, 1.2GHz, 1.1GHz, 1.0GHz, 0.9GHz, 0.8GHz |
| `HttpDev1Interface` | NIC.Integrated.1-3-1, NIC.Integrated.1-4-1, NIC.Slot.2-1-1, NIC.Slot.2-2-1 | NIC.Embedded.1-1-1, NIC.Embedded.2-1-1 | NIC.Embedded.1-1-1, NIC.Embedded.2-1-1, NIC.Integrated.1-1-1, NIC.Integrated.1-2-1 |
| `HttpDev2Interface` | NIC.Integrated.1-3-1, NIC.Integrated.1-4-1, NIC.Slot.2-1-1, NIC.Slot.2-2-1 | NIC.Embedded.1-1-1, NIC.Embedded.2-1-1 | NIC.Embedded.1-1-1, NIC.Embedded.2-1-1, NIC.Integrated.1-1-1, NIC.Integrated.1-2-1 |
| `HttpDev3Interface` | NIC.Integrated.1-3-1, NIC.Integrated.1-4-1, NIC.Slot.2-1-1, NIC.Slot.2-2-1 | NIC.Embedded.1-1-1, NIC.Embedded.2-1-1 | NIC.Embedded.1-1-1, NIC.Embedded.2-1-1, NIC.Integrated.1-1-1, NIC.Integrated.1-2-1 |
| `HttpDev4Interface` | NIC.Integrated.1-3-1, NIC.Integrated.1-4-1, NIC.Slot.2-1-1, NIC.Slot.2-2-1 | NIC.Embedded.1-1-1, NIC.Embedded.2-1-1 | NIC.Embedded.1-1-1, NIC.Embedded.2-1-1, NIC.Integrated.1-1-1, NIC.Integrated.1-2-1 |
| `IscsiDev1Con1Interface` | NIC.Integrated.1-3-1, NIC.Integrated.1-4-1, NIC.Slot.2-1-1, NIC.Slot.2-2-1 | NIC.Embedded.1-1-1, NIC.Embedded.2-1-1 | NIC.Embedded.1-1-1, NIC.Embedded.2-1-1, NIC.Integrated.1-1-1, NIC.Integrated.1-2-1 |
| `IscsiDev1Con2Interface` | NIC.Integrated.1-3-1, NIC.Integrated.1-4-1, NIC.Slot.2-1-1, NIC.Slot.2-2-1 | NIC.Embedded.1-1-1, NIC.Embedded.2-1-1 | NIC.Embedded.1-1-1, NIC.Embedded.2-1-1, NIC.Integrated.1-1-1, NIC.Integrated.1-2-1 |
| `MemFrequency` | - | MaxReliability | MaxPerf, MaxReliability |
| `NvmeofSubsys1ConInterface` | NIC.Integrated.1-3-1, NIC.Integrated.1-4-1, NIC.Slot.2-1-1, NIC.Slot.2-2-1 | NIC.Embedded.1-1-1, NIC.Embedded.2-1-1 | NIC.Embedded.1-1-1, NIC.Embedded.2-1-1, NIC.Integrated.1-1-1, NIC.Integrated.1-2-1 |
| `NvmeofSubsys2ConInterface` | NIC.Integrated.1-3-1, NIC.Integrated.1-4-1, NIC.Slot.2-1-1, NIC.Slot.2-2-1 | NIC.Embedded.1-1-1, NIC.Embedded.2-1-1 | NIC.Embedded.1-1-1, NIC.Embedded.2-1-1, NIC.Integrated.1-1-1, NIC.Integrated.1-2-1 |
| `NvmeofSubsys3ConInterface` | NIC.Integrated.1-3-1, NIC.Integrated.1-4-1, NIC.Slot.2-1-1, NIC.Slot.2-2-1 | NIC.Embedded.1-1-1, NIC.Embedded.2-1-1 | NIC.Embedded.1-1-1, NIC.Embedded.2-1-1, NIC.Integrated.1-1-1, NIC.Integrated.1-2-1 |
| `NvmeofSubsys4ConInterface` | NIC.Integrated.1-3-1, NIC.Integrated.1-4-1, NIC.Slot.2-1-1, NIC.Slot.2-2-1 | NIC.Embedded.1-1-1, NIC.Embedded.2-1-1 | NIC.Embedded.1-1-1, NIC.Embedded.2-1-1, NIC.Integrated.1-1-1, NIC.Integrated.1-2-1 |
| `OneTimeUefiBootSeqDev` | Disk.Bay.1:Enclosure.Internal.0-1 | Unknown.Unknown.1-1, NIC.HttpDevice.1-1, NIC.PxeDevice.1-1, Floppy.iDRACVirtual.1-1, Unknown.Unknown.6-1 | Unknown.Unknown.1-1, NIC.HttpDevice.1-1, NIC.PxeDevice.1-1, Floppy.iDRACVirtual.1-1, Optical.iDRACVirtual.1-1, Unknown.Unknown.6-1 |
| `ProcCores` | 8, 10 | - | All, 1, 2, 4, 6 |
| `ProcIssSetting` | IssOp2, IssOp3 | - | IssOp1 |
| `PxeDev10Interface` | NIC.Integrated.1-3-1, NIC.Integrated.1-4-1, NIC.Slot.2-1-1, NIC.Slot.2-2-1 | NIC.Embedded.1-1-1, NIC.Embedded.2-1-1 | NIC.Embedded.1-1-1, NIC.Embedded.2-1-1, NIC.Integrated.1-1-1, NIC.Integrated.1-2-1 |
| `PxeDev11Interface` | NIC.Integrated.1-3-1, NIC.Integrated.1-4-1, NIC.Slot.2-1-1, NIC.Slot.2-2-1 | NIC.Embedded.1-1-1, NIC.Embedded.2-1-1 | NIC.Embedded.1-1-1, NIC.Embedded.2-1-1, NIC.Integrated.1-1-1, NIC.Integrated.1-2-1 |
| `PxeDev12Interface` | NIC.Integrated.1-3-1, NIC.Integrated.1-4-1, NIC.Slot.2-1-1, NIC.Slot.2-2-1 | NIC.Embedded.1-1-1, NIC.Embedded.2-1-1 | NIC.Embedded.1-1-1, NIC.Embedded.2-1-1, NIC.Integrated.1-1-1, NIC.Integrated.1-2-1 |
| `PxeDev13Interface` | NIC.Integrated.1-3-1, NIC.Integrated.1-4-1, NIC.Slot.2-1-1, NIC.Slot.2-2-1 | NIC.Embedded.1-1-1, NIC.Embedded.2-1-1 | NIC.Embedded.1-1-1, NIC.Embedded.2-1-1, NIC.Integrated.1-1-1, NIC.Integrated.1-2-1 |
| `PxeDev14Interface` | NIC.Integrated.1-3-1, NIC.Integrated.1-4-1, NIC.Slot.2-1-1, NIC.Slot.2-2-1 | NIC.Embedded.1-1-1, NIC.Embedded.2-1-1 | NIC.Embedded.1-1-1, NIC.Embedded.2-1-1, NIC.Integrated.1-1-1, NIC.Integrated.1-2-1 |
| `PxeDev15Interface` | NIC.Integrated.1-3-1, NIC.Integrated.1-4-1, NIC.Slot.2-1-1, NIC.Slot.2-2-1 | NIC.Embedded.1-1-1, NIC.Embedded.2-1-1 | NIC.Embedded.1-1-1, NIC.Embedded.2-1-1, NIC.Integrated.1-1-1, NIC.Integrated.1-2-1 |
| `PxeDev16Interface` | NIC.Integrated.1-3-1, NIC.Integrated.1-4-1, NIC.Slot.2-1-1, NIC.Slot.2-2-1 | NIC.Embedded.1-1-1, NIC.Embedded.2-1-1 | NIC.Embedded.1-1-1, NIC.Embedded.2-1-1, NIC.Integrated.1-1-1, NIC.Integrated.1-2-1 |
| `PxeDev1Interface` | NIC.Integrated.1-3-1, NIC.Integrated.1-4-1, NIC.Slot.2-1-1, NIC.Slot.2-2-1 | NIC.Embedded.1-1-1, NIC.Embedded.2-1-1 | NIC.Embedded.1-1-1, NIC.Embedded.2-1-1, NIC.Integrated.1-1-1, NIC.Integrated.1-2-1 |
| `PxeDev2Interface` | NIC.Integrated.1-3-1, NIC.Integrated.1-4-1, NIC.Slot.2-1-1, NIC.Slot.2-2-1 | NIC.Embedded.1-1-1, NIC.Embedded.2-1-1 | NIC.Embedded.1-1-1, NIC.Embedded.2-1-1, NIC.Integrated.1-1-1, NIC.Integrated.1-2-1 |
| `PxeDev3Interface` | NIC.Integrated.1-3-1, NIC.Integrated.1-4-1, NIC.Slot.2-1-1, NIC.Slot.2-2-1 | NIC.Embedded.1-1-1, NIC.Embedded.2-1-1 | NIC.Embedded.1-1-1, NIC.Embedded.2-1-1, NIC.Integrated.1-1-1, NIC.Integrated.1-2-1 |
| `PxeDev4Interface` | NIC.Integrated.1-3-1, NIC.Integrated.1-4-1, NIC.Slot.2-1-1, NIC.Slot.2-2-1 | NIC.Embedded.1-1-1, NIC.Embedded.2-1-1 | NIC.Embedded.1-1-1, NIC.Embedded.2-1-1, NIC.Integrated.1-1-1, NIC.Integrated.1-2-1 |
| `PxeDev5Interface` | NIC.Integrated.1-3-1, NIC.Integrated.1-4-1, NIC.Slot.2-1-1, NIC.Slot.2-2-1 | NIC.Embedded.1-1-1, NIC.Embedded.2-1-1 | NIC.Embedded.1-1-1, NIC.Embedded.2-1-1, NIC.Integrated.1-1-1, NIC.Integrated.1-2-1 |
| `PxeDev6Interface` | NIC.Integrated.1-3-1, NIC.Integrated.1-4-1, NIC.Slot.2-1-1, NIC.Slot.2-2-1 | NIC.Embedded.1-1-1, NIC.Embedded.2-1-1 | NIC.Embedded.1-1-1, NIC.Embedded.2-1-1, NIC.Integrated.1-1-1, NIC.Integrated.1-2-1 |
| `PxeDev7Interface` | NIC.Integrated.1-3-1, NIC.Integrated.1-4-1, NIC.Slot.2-1-1, NIC.Slot.2-2-1 | NIC.Embedded.1-1-1, NIC.Embedded.2-1-1 | NIC.Embedded.1-1-1, NIC.Embedded.2-1-1, NIC.Integrated.1-1-1, NIC.Integrated.1-2-1 |
| `PxeDev8Interface` | NIC.Integrated.1-3-1, NIC.Integrated.1-4-1, NIC.Slot.2-1-1, NIC.Slot.2-2-1 | NIC.Embedded.1-1-1, NIC.Embedded.2-1-1 | NIC.Embedded.1-1-1, NIC.Embedded.2-1-1, NIC.Integrated.1-1-1, NIC.Integrated.1-2-1 |
| `PxeDev9Interface` | NIC.Integrated.1-3-1, NIC.Integrated.1-4-1, NIC.Slot.2-1-1, NIC.Slot.2-2-1 | NIC.Embedded.1-1-1, NIC.Embedded.2-1-1 | NIC.Embedded.1-1-1, NIC.Embedded.2-1-1, NIC.Integrated.1-1-1, NIC.Integrated.1-2-1 |
| `SerialComm` | OnConRedirAuto, OnConRedirCom1, OnConRedirCom2 | OnConRedir | OnNoConRedir, OnConRedir, Off |
| `SerialPortAddress` | Serial1Com1Serial2Com2, Serial1Com2Serial2Com1 | Com1, Com2 | Com1, Com2 |
| `Slot1` | - | BootDriverDisabled | Enabled, Disabled, BootDriverDisabled |
| `Slot2` | Disabled | - | Enabled, BootDriverDisabled |
| `Slot2Bif` | x16, x4x4x8, x8x4x4 | - | x4, x8 |
| `Slot31` | Disabled | - | Enabled, BootDriverDisabled |
| `Slot31Bif` | x16, x4x4x8, x8x4x4 | - | x4, x8 |
| `Slot33` | Disabled | - | Enabled, BootDriverDisabled |
| `Slot33Bif` | x16, x4x4x8, x8x4x4 | - | x4, x8 |
| `Slot36` | Disabled | - | Enabled, BootDriverDisabled |
| `Slot36Bif` | x16, x4x4x8, x8x4x4 | - | x4, x8 |
| `Slot38` | Disabled | - | Enabled, BootDriverDisabled |
| `Slot38Bif` | x16, x4x4x8, x8x4x4 | - | x4, x8 |
| `Slot7` | Disabled | - | Enabled, BootDriverDisabled |
| `Slot7Bif` | x16, x4x4x8, x8x4x4 | - | x4, x8 |
| `Slot8` | Disabled | - | Enabled, BootDriverDisabled |
| `Slot8Bif` | x16, x4x4x8, x8x4x4 | - | x4, x8 |
| `SubNumaCluster` | 2-Way | - | Disabled |
| `UefiCaCertScope` | - | DeviceFirmware | DeviceFirmwareAndOs, DeviceFirmware |
| `UefiVariableAccess` | - | Read-only | Standard, Controlled, Read-only |

### DisplayName 변경

| 속성 | A | B |
|---|---|---|
| `DimmSlot01` | DIMM Slot A2 | DIMM Slot A2 - Not Installed |
| `DimmSlot08` | DIMM Slot A9 - Not Installed | DIMM Slot B1 |
| `DimmSlot09` | DIMM Slot A10 - Not Installed | DIMM Slot B2 - Not Installed |
| `DimmSlot10` | DIMM Slot A11 - Not Installed | DIMM Slot B3 - Not Installed |
| `DimmSlot11` | DIMM Slot A12 - Not Installed | DIMM Slot B4 - Not Installed |
| `DimmSlot12` | DIMM Slot A13 - Not Installed | DIMM Slot B5 - Not Installed |
| `DimmSlot13` | DIMM Slot A14 - Not Installed | DIMM Slot B6 - Not Installed |
| `DimmSlot14` | DIMM Slot A15 - Not Installed | DIMM Slot B7 - Not Installed |
| `DimmSlot15` | DIMM Slot A16 - Not Installed | DIMM Slot B8 - Not Installed |
| `DimmSlot16` | DIMM Slot B1 | DIMM Slot  |
| `DimmSlot17` | DIMM Slot B2 | DIMM Slot  |
| `DimmSlot18` | DIMM Slot B3 - Not Installed | DIMM Slot  |
| `DimmSlot19` | DIMM Slot B4 - Not Installed | DIMM Slot  |
| `DimmSlot20` | DIMM Slot B5 - Not Installed | DIMM Slot  |
| `DimmSlot21` | DIMM Slot B6 - Not Installed | DIMM Slot  |
| `DimmSlot22` | DIMM Slot B7 - Not Installed | DIMM Slot  |
| `DimmSlot23` | DIMM Slot B8 - Not Installed | DIMM Slot  |
| `DimmSlot24` | DIMM Slot B9 - Not Installed | DIMM Slot  |
| `DimmSlot25` | DIMM Slot B10 - Not Installed | DIMM Slot  |
| `DimmSlot26` | DIMM Slot B11 - Not Installed | DIMM Slot  |
| `DimmSlot27` | DIMM Slot B12 - Not Installed | DIMM Slot  |
| `DimmSlot28` | DIMM Slot B13 - Not Installed | DIMM Slot  |
| `DimmSlot29` | DIMM Slot B14 - Not Installed | DIMM Slot  |
| `DimmSlot30` | DIMM Slot B15 - Not Installed | DIMM Slot  |
| `DimmSlot31` | DIMM Slot B16 - Not Installed | DIMM Slot  |
| `IscsiDev1Con1Ip` | Initiator IP Address | Initiator IP Address   |

