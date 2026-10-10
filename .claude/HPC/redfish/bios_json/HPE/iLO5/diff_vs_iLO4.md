# iLO5 vs iLO4 BIOS 차이 (HPE)

기준: iLO4(Gen9, `HpBios.1.2.0`) -> 대상: iLO5(Gen10/Gen10 Plus, `Bios.v1_0_0`). 생성: 2026-10-10.
**주의**: 속성 목록은 모델·ROM(P89/U32/U34/U45/U46 등)별로 다르다. 아래 "문서" 비교는 HPE 문서가 제공하는 플랫폼 샘플(iLO4 문서=합집합 234, iLO5 3.09=Intel 샘플 401, 3.19=AMD 샘플 540) 끼리의 비교라 **버전 차이와 플랫폼 차이가 섞여 있다**. "관측" 비교는 실제 장비 캡처 기반 이름 목록(U34/U46)이며 type/허용값은 없다. 이름 변경은 HPE 문서(ilo5_adaptation)에 명시된 것이 **없고**, 값 접두어 변경만 명시돼 있다(아래 1).

## 1. 구조·표현 차이 (HPE iLO5 adaptation 문서, verified: 공식 문서 + 관측 샘플 2종)
| 항목 | iLO4 | iLO5 |
|---|---|---|
| 리소스 타입 | `HpBios.1.2.0` (`@odata.type` 아님, Type 필드) | `#Bios.v1_0_0.Bios` (Gen10 Plus/iLO5 3.11 관측: `Bios.v1_0_4`) |
| 속성 위치 | 리소스 최상위에 name:value | `Attributes` 오브젝트 아래 |
| 레지스트리 | `HpBiosAttributeRegistryP89.1.1.00` 등 (HP OEM 타입 `HpBiosAttributeRegistrySchema.1.2.1`) | `BiosAttributeRegistryU32.v1_x_x` 등 (표준 `AttributeRegistry.v1_0_0`) |
| 열거값 | 숫자로 시작 가능 (`"AsrTimeoutMinutes":"10"`, `"SerialConsoleBaudRate":"115200"`) | 숫자 시작 금지(OData): `"Timeout10"`, `"Baud115200"` 등 접두어 |
| Settings 결과 | `SettingsResult` 오브젝트 | `@Redfish.Settings` |
| 액션 | 없음 (`BaseConfig:"default"` 를 PUT/PATCH 해 초기화) | `#Bios.ResetBios`, `#Bios.ChangePassword` (iLO5 문서: `…/Bios/Settings/Actions/Bios.ResetBios/`) |
| 기본값 | `/bios/baseconfigs` (HpBaseConfigs.0.10.0) | `…/Bios/BaseConfigs` (`HpeBaseConfigs.v2_0_0`) — Gen10 Plus 이후 `Bios/Oem/Hpe/BaseConfigs` |
| 관리 URI | `/rest/v1/systems/1/bios[/Settings]` (2.00+), `/redfish/v1/systems/1/bios[/settings]` (2.30+), 링크는 `links.*` | `/redfish/v1/Systems/1/Bios[/Settings]`, 링크는 `Oem.Hpe.Links.*` |
| 레거시 /rest/v1 | 있음 | 제거 |
| OEM 링크 (Gen10) | `links.BaseConfigs/Boot/Mappings/iScsi/Settings/self` | `Oem.Hpe.Links.BaseConfigs / Boot / Mappings / ScalablePmem(U34 관측) / TlsConfig / iScsi` → `/redfish/v1/systems/1/bios/baseconfigs/`, `/boot/`, `/mappings/`, `/hpescalablepmem/`, `/tlsconfig/`, `/iscsi/` |
| OEM 링크 (Gen10 Plus, iLO5 2.33+) | - | `/redfish/v1/systems/1/bios/oem/hpe/{baseconfigs,boot,kmsconfig,mappings,serverconfiglock,tlsconfig,iscsi}/` (U46 관측, ilo5_changelog "BIOS Redfish changes (GEN 10 to GEN 10 Plus)" 와 biosdoc) |
| ResetBios target 관측 | - | iLO5 1.x(U34) 및 3.11(U46): `/redfish/v1/systems/1/bios/settings/Actions/Bios.ResetBios/` |

## 2. 열거값(allowed_values) 변경 — iLO4 문서 vs iLO5 v3.09 문서 (공통 속성 201개 중 34개)
(플랫폼이 다른 샘플이라 일부는 단순히 지원 하드웨어가 다른 때문. 숫자->접두어 변경이 확실한 구조적 변경.)

| 속성 | 변경 |
|---|---|
| `AdvancedMemProtection` | allowed_values: 삭제 [] / 추가 ['FastFaultTolerantADDDC'] |
| `AsrTimeoutMinutes` | allowed_values: 삭제 ['10', '15', '20', '30', '5'] / 추가 ['Timeout10', 'Timeout15', 'Timeout20', 'Timeout30', 'Timeout5'] |
| `ConsistentDevNaming` | allowed_values: 삭제 [] / 추가 ['LomsAndSlots'] |
| `EmbNicEnable` | allowed_values: 삭제 ['Enabled'] / 추가 ['Auto'] |
| `EmbSas1Boot` | allowed_values: 삭제 ['ThreeTargets'] / 추가 ['TwentyFourTargets'] |
| `EmbSata1Enable` | allowed_values: 삭제 ['Enabled'] / 추가 ['Auto'] |
| `EmbSata2Enable` | allowed_values: 삭제 ['Enabled'] / 추가 ['Auto'] |
| `EmsConsole` | allowed_values: 삭제 ['Com1Irq4', 'Com2Irq3'] / 추가 ['Physical', 'Virtual'] |
| `FlexLom1Enable` | allowed_values: 삭제 ['Enabled'] / 추가 ['Auto'] |
| `IntelDmiLinkFreq` | allowed_values: 삭제 [] / 추가 ['DmiGen2'] |
| `MaxMemBusFreqMHz` | allowed_values: 삭제 ['1333', '1600', '1867', '2133'] / 추가 ['MaxMemBusFreq1867', 'MaxMemBusFreq2133', 'MaxMemBusFreq2400', 'MaxMemBusFreq2667', 'MaxMemBusFreq2933'] |
| `MaxPcieSpeed` | allowed_values: 삭제 ['MaxSupported'] / 추가 ['PerPortCtrl'] |
| `MinProcIdlePower` | allowed_values: 삭제 ['C3'] / 추가 [] |
| `NumaGroupSizeOpt` | allowed_values: 삭제 [] / 추가 [] |
| `NvDimmNSanitizePolicy` | allowed_values: 삭제 [] / 추가 ['SanitizeToFactoryDefaults'] |
| `PciSlot1Enable` | allowed_values: 삭제 ['Enabled'] / 추가 ['Auto'] |
| `PciSlot2Enable` | allowed_values: 삭제 ['Enabled'] / 추가 ['Auto'] |
| `PciSlot3Enable` | allowed_values: 삭제 ['Enabled'] / 추가 ['Auto'] |
| `PciSlot4Enable` | allowed_values: 삭제 ['Enabled'] / 추가 ['Auto'] |
| `PciSlot5Enable` | allowed_values: 삭제 ['Enabled'] / 추가 ['Auto'] |
| `PciSlot6Enable` | allowed_values: 삭제 ['Enabled'] / 추가 ['Auto'] |
| `PowerOnDelay` | allowed_values: 삭제 ['15Sec', '30Sec', '45Sec', '60Sec', 'None'] / 추가 ['Delay15Sec', 'Delay30Sec', 'Delay45Sec', 'Delay60Sec', 'NoDelay'] |
| `PreBootNetwork` | allowed_values: 삭제 ['EmbNic', 'FlexLom1', 'PciSlot1', 'PciSlot2', 'PciSlot3', 'PciSlot4', 'PciSlot5', 'PciSlot6'] / 추가 ['EmbNicPort1', 'EmbNicPort2', 'EmbNicPort3', 'EmbNicPort4', 'EmbNicPort5', 'EmbNicPort6', 'EmbNicPort7', 'EmbNicPort8', 'FlexLom1Port1', 'FlexLom1Port2', 'FlexLom1Port3', 'FlexLom1Port4', 'FlexLom1Port5', 'FlexLom1Port6', 'FlexLom1Port7', 'FlexLom1Port8', 'Slot1NicPort1', 'Slot1NicPort2', 'Slot1NicPort3', 'Slot1NicPort4', 'Slot1NicPort5', 'Slot1NicPort6', 'Slot1NicPort7', 'Slot1NicPort8', 'Slot2NicPort1', 'Slot2NicPort2', 'Slot2NicPort3', 'Slot2NicPort4', 'Slot2NicPort5', 'Slot2NicPort6', 'Slot2NicPort7', 'Slot2NicPort8', 'Slot3NicPort1', 'Slot3NicPort2', 'Slot3NicPort3', 'Slot3NicPort4', 'Slot3NicPort5', 'Slot3NicPort6', 'Slot3NicPort7', 'Slot3NicPort8', 'Slot4NicPort1', 'Slot4NicPort2', 'Slot4NicPort3', 'Slot4NicPort4', 'Slot4NicPort5', 'Slot4NicPort6', 'Slot4NicPort7', 'Slot4NicPort8', 'Slot5NicPort1', 'Slot5NicPort2', 'Slot5NicPort3', 'Slot5NicPort4', 'Slot5NicPort5', 'Slot5NicPort6', 'Slot5NicPort7', 'Slot5NicPort8', 'Slot6NicPort1', 'Slot6NicPort2', 'Slot6NicPort3', 'Slot6NicPort4', 'Slot6NicPort5', 'Slot6NicPort6', 'Slot6NicPort7', 'Slot6NicPort8', 'Slot7NicPort1', 'Slot7NicPort2', 'Slot7NicPort3', 'Slot7NicPort4', 'Slot7NicPort5', 'Slot7NicPort6', 'Slot7NicPort7', 'Slot7NicPort8', 'Slot8NicPort1', 'Slot8NicPort2', 'Slot8NicPort3', 'Slot8NicPort4', 'Slot8NicPort5', 'Slot8NicPort6', 'Slot8NicPort7', 'Slot8NicPort8'] |
| `SerialConsoleBaudRate` | allowed_values: 삭제 ['115200', '19200', '38400', '57600', '9600'] / 추가 ['BaudRate115200', 'BaudRate19200', 'BaudRate38400', 'BaudRate57600', 'BaudRate9600'] |
| `SerialConsoleEmulation` | allowed_values: 삭제 [] / 추가 ['VtUtf8'] |
| `Slot1StorageBoot` | allowed_values: 삭제 ['ThreeTargets'] / 추가 ['TwentyFourTargets'] |
| `Slot2StorageBoot` | allowed_values: 삭제 ['ThreeTargets'] / 추가 ['TwentyFourTargets'] |
| `Slot3StorageBoot` | allowed_values: 삭제 ['ThreeTargets'] / 추가 ['TwentyFourTargets'] |
| `Slot4StorageBoot` | allowed_values: 삭제 ['ThreeTargets'] / 추가 ['TwentyFourTargets'] |
| `Slot5StorageBoot` | allowed_values: 삭제 ['ThreeTargets'] / 추가 ['TwentyFourTargets'] |
| `Slot6StorageBoot` | allowed_values: 삭제 ['ThreeTargets'] / 추가 ['TwentyFourTargets'] |
| `ThermalConfig` | allowed_values: 삭제 [] / 추가 ['EnhancedCPUCooling'] |
| `TpmType` | allowed_values: 삭제 ['Tm10'] / 추가 [] |
| `UsbControl` | allowed_values: 삭제 [] / 추가 ['InternalUsbDisabled', 'UsbDisabled'] |

### A. 문서끼리: iLO4 HpBios Attributes(2.30) vs iLO5 v3.09 Intel 샘플

- 기준 iLO4 doc: 234개 / 대상 iLO5 3.09 doc: 401개 / 공통 201 / 추가 200 / 삭제 33

**추가(iLO5 3.09 doc 에만)**

```
AcpiHpet, AdvCrashDumpMode, CoreBoosting, DirectToUpi, EmbNicAspm, EmbNicLinkSpeed, EmbNicPCIeOptionROM,
EmbSas1Aspm, EmbSas1Enable, EmbSas1LinkSpeed, EmbSas1PcieOptionROM, EmbSata1Aspm, EmbSata1PCIeOptionROM,
EmbSata2Aspm, EmbSata2PCIeOptionROM, EnabledCoresPerProc, EnergyEfficientTurbo, EnhancedProcPerf,
FlexLom1Aspm, FlexLom1LinkSpeed, FlexLom1PCIeOptionROM, HttpSupport, IntelSpeedSelect, IntelUpiFreq,
IntelUpiLinkEn, IntelUpiPowerManagement, Ipv6Address, Ipv6ConfigPolicy, Ipv6Gateway, Ipv6PrimaryDNS,
Ipv6SecondaryDNS, LLCDeadLineAllocation, LlcPrefetch, LocalRemoteThreshold, MemClearWarmReset, MemMirrorMode,
MemPatrolScrubbing, MemRefreshRate, MemoryControllerInterleaving, MemoryRemap, NetworkBootRetryCount,
NvdimmLabelSupport, NvmeFormat1, NvmeFormat10, NvmeFormat11, NvmeFormat12, NvmeFormat13, NvmeFormat14,
NvmeFormat15, NvmeFormat16, NvmeFormat17, NvmeFormat18, NvmeFormat19, NvmeFormat2, NvmeFormat20, NvmeFormat21,
NvmeFormat22, NvmeFormat23, NvmeFormat24, NvmeFormat25, NvmeFormat26, NvmeFormat27, NvmeFormat28,
NvmeFormat29, NvmeFormat3, NvmeFormat30, NvmeFormat31, NvmeFormat32, NvmeFormat33, NvmeFormat34, NvmeFormat35,
NvmeFormat36, NvmeFormat37, NvmeFormat38, NvmeFormat39, NvmeFormat4, NvmeFormat40, NvmeFormat41, NvmeFormat42,
NvmeFormat43, NvmeFormat44, NvmeFormat45, NvmeFormat46, NvmeFormat47, NvmeFormat48, NvmeFormat49, NvmeFormat5,
NvmeFormat50, NvmeFormat6, NvmeFormat7, NvmeFormat8, NvmeFormat9, NvmeOptionRom, OpportunisticSelfRefresh,
PciPeerToPeerSerialization, PciResourcePadding, PciSlot1Aspm, PciSlot1Bifurcation, PciSlot1LinkSpeed,
PciSlot1OptionROM, PciSlot2Aspm, PciSlot2Bifurcation, PciSlot2LinkSpeed, PciSlot2OptionROM, PciSlot3Aspm,
PciSlot3Bifurcation, PciSlot3LinkSpeed, PciSlot3OptionROM, PciSlot4Aspm, PciSlot4Bifurcation,
PciSlot4LinkSpeed, PciSlot4OptionROM, PciSlot5Aspm, PciSlot5Bifurcation, PciSlot5LinkSpeed, PciSlot5OptionROM,
PciSlot6Aspm, PciSlot6Bifurcation, PciSlot6LinkSpeed, PciSlot6OptionROM, PciSlot7Aspm, PciSlot7Bifurcation,
PciSlot7Enable, PciSlot7LinkSpeed, PciSlot7OptionROM, PciSlot8Aspm, PciSlot8Bifurcation, PciSlot8Enable,
PciSlot8LinkSpeed, PciSlot8OptionROM, PersistentMemAddressRangeScrub, PersistentMemBackupPowerPolicy,
PersistentMemScanMem, PostBootProgress, PostDiscoveryMode, PostVideoSupport, PrebootNetworkEnvPolicy,
PrebootNetworkProxy, ProcessorConfigTDPLevel, ProcessorJitterControl, ProcessorJitterControlFrequency,
ProcessorJitterControlOptimization, SanitizeProc1Dimm1, SanitizeProc1Dimm10, SanitizeProc1Dimm11,
SanitizeProc1Dimm12, SanitizeProc1Dimm2, SanitizeProc1Dimm3, SanitizeProc1Dimm4, SanitizeProc1Dimm5,
SanitizeProc1Dimm6, SanitizeProc1Dimm7, SanitizeProc1Dimm8, SanitizeProc1Dimm9, SanitizeProc2Dimm1,
SanitizeProc2Dimm10, SanitizeProc2Dimm11, SanitizeProc2Dimm12, SanitizeProc2Dimm2, SanitizeProc2Dimm3,
SanitizeProc2Dimm4, SanitizeProc2Dimm5, SanitizeProc2Dimm6, SanitizeProc2Dimm7, SanitizeProc2Dimm8,
SanitizeProc2Dimm9, SecStartBackupImage, ServerConfigLockStatus, SetupBrowserSelection, Slot7NicBoot1,
Slot7NicBoot2, Slot7NicBoot3, Slot7NicBoot4, Slot7StorageBoot, Slot8NicBoot1, Slot8NicBoot2, Slot8NicBoot3,
Slot8NicBoot4, Slot8StorageBoot, StaleAtoS, SubNumaClustering, Tpm20SoftwareInterfaceOperation,
Tpm20SoftwareInterfaceStatus, TpmActivePcrs, TpmChipId, TpmFips, TpmFipsModeSwitch, TpmModeSwitchOperation,
UefiSerialDebugLevel, UefiShellScriptVerification, UefiShellStartupUrlFromDhcp, UncoreFreqScaling,
UpiPrefetcher, UrlBootFile2, UrlBootFile3, UrlBootFile4, UserDefaultsState, WorkloadProfile, XptPrefetcher,
iSCSIPolicy
```

**삭제(iLO4 doc 에만)**

```
AdminPassword, DynamicPowerResponse, EmbSasEnable, EmbeddedDiagsMode, EmbeddedUserPartition, IntelQpiFreq,
IntelQpiLinkEn, IntelQpiPowerManagement, IoNonPostedPrefetching, NmiDebugButton, NvDimmNBackupPowerPolicy,
NvDimmNForcedRecovery, NvDimmNIntegrityChecking, OldAdminPassword, OldPowerOnPassword, PciBusPadding,
PcieExpressEcrcSupport, PowerOnLogo, PowerOnPassword, PowerProfile, ProcCoreDisable, ProcNoExecute,
QpiBandwidthOpt, QpiHomeSnoopOpt, QpiSnoopConfig, SanitizeProc{1,2}Dimm{N}, TmOperation, TmVisibility,
Tpm2Ppi, Tpm2Visibility, TpmBinding, UefiPxeBoot, Usb3Mode
```

이름 변경 후보(문자열 유사도 0.8 이상 자동 매칭 — 문서 근거 아님, 추측):

- EmbSasEnable -> EmbSas1Enable
- IntelQpiFreq -> IntelUpiFreq
- IntelQpiLinkEn -> IntelUpiLinkEn
- IntelQpiPowerManagement -> IntelUpiPowerManagement
- SanitizeProc{1,2}Dimm{N} -> SanitizeProc2Dimm9

### B. 문서끼리: iLO4 vs iLO5 v3.19 (AMD 샘플)

- 기준 iLO4 doc: 234개 / 대상 iLO5 3.19 doc: 540개 / 공통 163 / 추가 377 / 삭제 71

**추가(iLO5 3.19 doc 에만)**

```
AMDPerformanceWorkloadProfile, AccessControlService, AcpiHpet, AdvCrashDumpMode, AllowLoginWithIlo,
Amd5LevelPage, AmdCdma, AmdCstC2Latency, AmdDmaRemapping, AmdL1Prefetcher, AmdL2Prefetcher, AmdMemPStates,
AmdMemoryBurstRefresh, AmdPeriodicDirectoryRinse, AmdSecureMemoryEncryption, AmdSecureNestedPaging,
AmdVirtualDrtmDevice, AmdXGMILinkSpeed, ApplicationPowerBoost, CustomPstate0, DataFabricCStateEnable,
DeterminismControl, DramControllerPowerDown, EmbSata1Aspm, EmbSata1PCIeOptionROM, EmbSata2Aspm,
EmbSata2PCIeOptionROM, EmbSata3Aspm, EmbSata3Enable, EmbSata3PCIeOptionROM, EmbSata4Aspm, EmbSata4Enable,
EmbSata4PCIeOptionROM, EmbeddedIpxe, EnabledCoresPerProc, HourFormat, HttpSupport, InfinityFabricPstate,
IpmiWatchdogTimerAction, IpmiWatchdogTimerStatus, IpmiWatchdogTimerTimeout, Ipv6Address, Ipv6ConfigPolicy,
Ipv6Gateway, Ipv6PrimaryDNS, IpxeAutoStartScriptLocation, IpxeBootOrder, IpxeScriptAutoStart,
IpxeScriptVerification, IpxeStartupUrl, LastLevelCacheAsNUMANode, MemPatrolScrubbing, MemRefreshRate,
MicrosoftSecuredCoreSupport, MinimumSevAsid, NetworkBootRetryCount, NumaMemoryDomainsPerSocket, NvmeOptionRom,
Ocp1AuxiliaryPower, Ocp2AuxiliaryPower, OmitBootDeviceEvent, PackagePowerLimitControlMode,
PackagePowerLimitValue, PatrolScrubDuration, PciSlot10Aspm, PciSlot10Bifurcation, PciSlot10Enable,
PciSlot10LinkSpeed, PciSlot10OptionROM, PciSlot11Aspm, PciSlot11Bifurcation, PciSlot11Enable,
PciSlot11LinkSpeed, PciSlot11OptionROM, PciSlot12Aspm, PciSlot12Bifurcation, PciSlot12Enable,
PciSlot12LinkSpeed, PciSlot12OptionROM, PciSlot13Aspm, PciSlot13Bifurcation, PciSlot13Enable,
PciSlot13LinkSpeed, PciSlot13OptionROM, PciSlot14Aspm, PciSlot14Bifurcation, PciSlot14Enable,
PciSlot14LinkSpeed, PciSlot14OptionROM, PciSlot15Aspm, PciSlot15Bifurcation, PciSlot15Enable,
PciSlot15LinkSpeed, PciSlot15OptionROM, PciSlot16Aspm, PciSlot16Bifurcation, PciSlot16Enable,
PciSlot16LinkSpeed, PciSlot16OptionROM, PciSlot17Aspm, PciSlot17Bifurcation, PciSlot17Enable,
PciSlot17LinkSpeed, PciSlot17OptionROM, PciSlot18Aspm, PciSlot18Bifurcation, PciSlot18Enable,
PciSlot18LinkSpeed, PciSlot18OptionROM, PciSlot19Aspm, PciSlot19Bifurcation, PciSlot19Enable,
PciSlot19LinkSpeed, PciSlot19OptionROM, PciSlot1Aspm, PciSlot1Bifurcation, PciSlot1LinkSpeed,
PciSlot1OptionROM, PciSlot20Aspm, PciSlot20Bifurcation, PciSlot20Enable, PciSlot20LinkSpeed,
PciSlot20OptionROM, PciSlot21Aspm, PciSlot21Bifurcation, PciSlot21Enable, PciSlot21LinkSpeed,
PciSlot21OptionROM, PciSlot22Aspm, PciSlot22Bifurcation, PciSlot22Enable, PciSlot22LinkSpeed,
PciSlot22OptionROM, PciSlot23Aspm, PciSlot23Bifurcation, PciSlot23Enable, PciSlot23LinkSpeed,
PciSlot23OptionROM, PciSlot2Aspm, PciSlot2Bifurcation, PciSlot2LinkSpeed, PciSlot2OptionROM, PciSlot3Aspm,
PciSlot3Bifurcation, PciSlot3LinkSpeed, PciSlot3OptionROM, PciSlot4Aspm, PciSlot4Bifurcation,
PciSlot4LinkSpeed, PciSlot4OptionROM, PciSlot5Aspm, PciSlot5Bifurcation, PciSlot5LinkSpeed, PciSlot5OptionROM,
PciSlot6Aspm, PciSlot6Bifurcation, PciSlot6LinkSpeed, PciSlot6OptionROM, PciSlot7Aspm, PciSlot7Bifurcation,
PciSlot7Enable, PciSlot7LinkSpeed, PciSlot7OptionROM, PciSlot8Aspm, PciSlot8Bifurcation, PciSlot8Enable,
PciSlot8LinkSpeed, PciSlot8OptionROM, PciSlot9Aspm, PciSlot9Bifurcation, PciSlot9Enable, PciSlot9LinkSpeed,
PciSlot9OptionROM, PerformanceDeterminism, PlatformCertificate, PlatformRASPolicy, PostAsr, PostAsrDelay,
PostBootProgress, PostDiscoveryMode, PostScreenMode, PostVideoSupport, PrebootNetworkEnvPolicy,
PrebootNetworkProxy, ProcAMDBoost, ProcAMDBoostControl, ProcAmdFmax, ProcAmdIoVt, ProcSMT, Pstate0Frequency,
RedundantPowerSupplyGpuDomain, RedundantPowerSupplySystemDomain, SataSanitize, SciRasSupport,
SecStartBackupImage, ServerConfigLockStatus, SetupBrowserSelection, Slot10MctpBroadcastSupport,
Slot10NicBoot1, Slot10NicBoot2, Slot10NicBoot3, Slot10NicBoot4, Slot10NicBoot5, Slot10NicBoot6,
Slot10NicBoot7, Slot10NicBoot8, Slot10StorageBoot, Slot11MctpBroadcastSupport, Slot11NicBoot1, Slot11NicBoot2,
Slot11NicBoot3, Slot11NicBoot4, Slot11NicBoot5, Slot11NicBoot6, Slot11NicBoot7, Slot11NicBoot8,
Slot11StorageBoot, Slot12MctpBroadcastSupport, Slot12NicBoot1, Slot12NicBoot2, Slot12NicBoot3, Slot12NicBoot4,
Slot12NicBoot5, Slot12NicBoot6, Slot12NicBoot7, Slot12NicBoot8, Slot12StorageBoot, Slot13MctpBroadcastSupport,
Slot13NicBoot1, Slot13NicBoot2, Slot13NicBoot3, Slot13NicBoot4, Slot13NicBoot5, Slot13NicBoot6,
Slot13NicBoot7, Slot13NicBoot8, Slot13StorageBoot, Slot14MctpBroadcastSupport, Slot14NicBoot1, Slot14NicBoot2,
Slot14NicBoot3, Slot14NicBoot4, Slot14NicBoot5, Slot14NicBoot6, Slot14NicBoot7, Slot14NicBoot8,
Slot14StorageBoot, Slot15MctpBroadcastSupport, Slot15NicBoot1, Slot15NicBoot2, Slot15NicBoot3, Slot15NicBoot4,
Slot15NicBoot5, Slot15NicBoot6, Slot15NicBoot7, Slot15NicBoot8, Slot15StorageBoot, Slot16MctpBroadcastSupport,
Slot16NicBoot1, Slot16NicBoot2, Slot16NicBoot3, Slot16NicBoot4, Slot16NicBoot5, Slot16NicBoot6,
Slot16NicBoot7, Slot16NicBoot8, Slot16StorageBoot, Slot17MctpBroadcastSupport, Slot17NicBoot1, Slot17NicBoot2,
Slot17NicBoot3, Slot17NicBoot4, Slot17NicBoot5, Slot17NicBoot6, Slot17NicBoot7, Slot17NicBoot8,
Slot17StorageBoot, Slot18MctpBroadcastSupport, Slot18NicBoot1, Slot18NicBoot2, Slot18NicBoot3, Slot18NicBoot4,
Slot18NicBoot5, Slot18NicBoot6, Slot18NicBoot7, Slot18NicBoot8, Slot18StorageBoot, Slot19MctpBroadcastSupport,
Slot19NicBoot1, Slot19NicBoot2, Slot19NicBoot3, Slot19NicBoot4, Slot19NicBoot5, Slot19NicBoot6,
Slot19NicBoot7, Slot19NicBoot8, Slot19StorageBoot, Slot1MctpBroadcastSupport, Slot20MctpBroadcastSupport,
Slot20NicBoot1, Slot20NicBoot2, Slot20NicBoot3, Slot20NicBoot4, Slot20NicBoot5, Slot20NicBoot6,
Slot20NicBoot7, Slot20NicBoot8, Slot20StorageBoot, Slot21MctpBroadcastSupport, Slot21NicBoot1, Slot21NicBoot2,
Slot21NicBoot3, Slot21NicBoot4, Slot21NicBoot5, Slot21NicBoot6, Slot21NicBoot7, Slot21NicBoot8,
Slot21StorageBoot, Slot22MctpBroadcastSupport, Slot22NicBoot1, Slot22NicBoot2, Slot22NicBoot3, Slot22NicBoot4,
Slot22NicBoot5, Slot22NicBoot6, Slot22NicBoot7, Slot22NicBoot8, Slot22StorageBoot, Slot23MctpBroadcastSupport,
Slot23StorageBoot, Slot2MctpBroadcastSupport, Slot3MctpBroadcastSupport, Slot4MctpBroadcastSupport,
Slot5MctpBroadcastSupport, Slot6MctpBroadcastSupport, Slot7MctpBroadcastSupport, Slot7NicBoot1, Slot7NicBoot2,
Slot7NicBoot3, Slot7NicBoot4, Slot7StorageBoot, Slot8MctpBroadcastSupport, Slot8NicBoot1, Slot8NicBoot2,
Slot8NicBoot3, Slot8NicBoot4, Slot8StorageBoot, Slot9MctpBroadcastSupport, Slot9NicBoot1, Slot9NicBoot2,
Slot9NicBoot3, Slot9NicBoot4, Slot9NicBoot5, Slot9NicBoot6, Slot9NicBoot7, Slot9NicBoot8, Slot9StorageBoot,
SpeculativeLockScheduling, TPM2EndorsementDisable, TPM2StorageDisable, Tpm20SoftwareInterfaceStatus,
TpmActivePcrs, TpmChipId, TransparentSecureMemoryEncryption, UefiSerialDebugLevel,
UefiShellPhysicalPresenceKeystroke, UefiShellScriptVerification, UefiShellStartupUrlFromDhcp,
UefiVariableAccessFwControl, UrlBootFile2, UrlBootFile3, UrlBootFile4, UserDefaultsState, WorkloadProfile,
XGMIForceLinkWidth, XGMIMaxLinkWidth, iSCSISoftwareInitiator
```

**삭제(iLO4 doc 에만)**

```
AdjSecPrefetch, AdminPassword, AdvancedMemProtection, AsrStatus, AsrTimeoutMinutes, BootMode,
ChannelInterleaving, DcuIpPrefetcher, DcuStreamPrefetcher, DynamicPowerResponse, EmbNicEnable, EmbSas1Boot,
EmbSasEnable, EmbeddedDiagsMode, EmbeddedSata, EmbeddedUserPartition, EnergyPerfBias, FlexLom1Enable,
HwPrefetcher, IntelDmiLinkFreq, IntelNicDmaChannels, IntelPerfMonitoring, IntelProcVtd, IntelQpiFreq,
IntelQpiLinkEn, IntelQpiPowerManagement, IntelTxt, InternalSDCardSlot, IoNonPostedPrefetching,
Ipv4SecondaryDNS, MemFastTraining, MinProcIdlePkgState, NmiDebugButton, NodeInterleaving,
NvDimmNBackupPowerPolicy, NvDimmNForcedRecovery, NvDimmNIntegrityChecking, NvDimmNMemFunctionality,
NvDimmNMemInterleaving, NvDimmNSanitizePolicy, OldAdminPassword, OldPowerOnPassword, PciBusPadding,
PcieExpressEcrcSupport, PowerOnLogo, PowerOnPassword, PowerProfile, ProcCoreDisable, ProcHyperthreading,
ProcNoExecute, ProcTurbo, ProcVirtualization, QpiBandwidthOpt, QpiHomeSnoopOpt, QpiSnoopConfig,
RedundantPowerSupply, RemovableFlashBootSeq, SanitizeAllNvDimmN, SanitizeProc1NvDimmN, SanitizeProc2NvDimmN,
SanitizeProc{1,2}Dimm{N}, TmOperation, TmVisibility, Tpm2Ppi, Tpm2Visibility, TpmBinding, TpmOperation,
TpmType, UefiOptimizedBoot, UefiPxeBoot, Usb3Mode
```

이름 변경 후보(문자열 유사도 0.8 이상 자동 매칭 — 문서 근거 아님, 추측):

- EmbSasEnable -> EmbSata4Enable
- RedundantPowerSupply -> RedundantPowerSupplyGpuDomain

### C. 관측: iLO4 문서 이름 vs iLO5 Gen10 Plus 실제 노출 속성(U46, iLO5 3.11)

- 기준 iLO4 doc: 234개 / 대상 U46 관측: 276개 / 공통 112 / 추가 164 / 삭제 122

**추가(U46 관측 에만)**

```
AccessControlService, AcpiHpet, AdvCrashDumpMode, AllowLoginWithIlo, DeadBlockPredictor, DirectToUpi,
DisableDynamicLoadlineSwitch, DramRapl, DramRaplLimit, DramRaplReport, EmbeddedIpxe, EnabledCoresPerProc,
EnergyEfficientTurbo, EnergyPerformancePreference, EnhancedProcPerf, EppProfile, FilterNonbootableDrive,
HourFormat, HttpSupport, IODCConfiguration, IntelUpiFreq, IntelUpiLinkEn, IntelUpiPowerManagement,
IntelVmdDirectAssign, IntelVmdSupport, IntelVrocSupport, IpmiWatchdogTimerAction, IpmiWatchdogTimerStatus,
IpmiWatchdogTimerTimeout, Ipv6Address, Ipv6ConfigPolicy, Ipv6Gateway, Ipv6PrimaryDNS,
IpxeAutoStartScriptLocation, IpxeBootOrder, IpxeScriptAutoStart, IpxeScriptVerification, IpxeStartupUrl,
LLCDeadLineAllocation, LlcPrefetch, LocalRemoteThreshold, MemClearWarmReset, MemMirrorMode,
MemPatrolScrubbing, MemRefreshRate, MemoryConfigurationViolationReporting, MemoryRemap,
MicrosoftSecuredCoreSupport, MkTme, NetworkBootRetryCount, NoExecutionProtection, Numa, NvmeOptionRom,
NvmePort1, NvmePort10, NvmePort2, NvmePort9, NvmeRaid, Ocp1AuxiliaryPower, OmitBootDeviceEvent,
PatrolScrubDuration, PciPeerToPeerSerialization, PciResourcePadding, PciSlot10Aspm, PciSlot10Bifurcation,
PciSlot10Enable, PciSlot10LinkSpeed, PciSlot10OptionROM, PciSlot12Aspm, PciSlot12Enable, PciSlot12LinkSpeed,
PciSlot12OptionROM, PciSlot1Aspm, PciSlot1Bifurcation, PciSlot1LinkSpeed, PciSlot1OptionROM,
PciSlot2Bifurcation, PcieHotPlugErrControl, PcuPMax, PlatformCertificate, PlatformRASPolicy, PostAsr,
PostAsrDelay, PostBootProgress, PostDiscoveryMode, PostScreenMode, PostVideoSupport, PrebootNetworkEnvPolicy,
PrebootNetworkProxy, ProcRapl, ProcessorConfigTDPLevel, ProcessorPhysicalAddress, ProcessorUuidControl,
RemoteXptPrefetcher, SataSanitize, SciRasSupport, SecStartBackupImage, SerialPortDtrSupport,
ServerConfigLockStatus, SetupBrowserSelection, SgxAutoMpRegistrationAgent, SgxEnable, SgxLaunchControlPolicy,
SgxLePublicKeyHash0, SgxLePublicKeyHash1, SgxLePublicKeyHash2, SgxLePublicKeyHash3, SgxLePublicKeyWriteEnable,
Slot10DataLinkFeatureExchange, Slot10EoiBroadcastSupport, Slot10MctpBroadcastSupport, Slot10NicBoot1,
Slot10NicBoot2, Slot10NicBoot3, Slot10NicBoot4, Slot12StorageBoot, Slot1DataLinkFeatureExchange,
Slot1EoiBroadcastSupport, Slot1MctpBroadcastSupport, Slot2DataLinkFeatureExchange, Slot2EoiBroadcastSupport,
Slot2MctpBroadcastSupport, SnoopResponseHoldOff, StaleAtoS, SubNumaClustering, TPM2EndorsementDisable,
TPM2StorageDisable, Tme, TmeExclusiveBase, TmeExclusiveLen, Tpm20SoftwareInterfaceOperation,
Tpm20SoftwareInterfaceStatus, TpmActivePcrs, TpmChipId, TpmFips, TpmFipsModeSwitch, TpmModeSwitchOperation,
Tsx, UefiSerialDebugLevel, UefiShellPhysicalPresenceKeystroke, UefiShellScriptVerification,
UefiShellStartupUrlFromDhcp, UefiVariableAccessFwControl, UncoreFreqScaling, UncoreFrequencyMAX,
UncoreFrequencyMIN, UpiPrefetcher, UrlBootFile2, UrlBootFile3, UrlBootFile4, UserDefaultsState, VirtualNuma,
VmProprietaryPageRetireSupport, VmdonCpu1Stack5Port1, VmdonCpu1Stack5Port2, VmdonCpu1Stack5Port3,
VmdonCpu1Stack5Port4, VmdonCpu2Stack5Port1, VmdonCpu2Stack5Port2, VmdonCpu2Stack5Port3, VmdonCpu2Stack5Port4,
WorkloadProfile, XptPrefetcher, iSCSISoftwareInitiator
```

**삭제(iLO4 doc 에만)**

```
AdminPassword, AsrStatus, AsrTimeoutMinutes, ChannelInterleaving, DynamicPowerResponse, EmbNicEnable,
EmbSas1Boot, EmbSasEnable, EmbSata1Enable, EmbSata2Enable, EmbeddedDiagsMode, EmbeddedUserPartition,
FlexLom1Enable, IntelQpiFreq, IntelQpiLinkEn, IntelQpiPowerManagement, InternalSDCardSlot,
IoNonPostedPrefetching, Ipv4SecondaryDNS, NicBoot1, NicBoot10, NicBoot11, NicBoot12, NicBoot2, NicBoot3,
NicBoot4, NicBoot5, NicBoot6, NicBoot7, NicBoot8, NicBoot9, NmiDebugButton, NodeInterleaving,
NvDimmNBackupPowerPolicy, NvDimmNForcedRecovery, NvDimmNIntegrityChecking, NvDimmNMemFunctionality,
NvDimmNMemInterleaving, NvDimmNSanitizePolicy, OldAdminPassword, OldPowerOnPassword, PciBusPadding,
PciSlot2Enable, PciSlot3Enable, PciSlot4Enable, PciSlot5Enable, PciSlot6Enable, PcieExpressEcrcSupport,
PowerOnLogo, PowerOnPassword, PowerProfile, ProcCoreDisable, ProcNoExecute, QpiBandwidthOpt, QpiHomeSnoopOpt,
QpiSnoopConfig, SanitizeAllNvDimmN, SanitizeProc1NvDimmN, SanitizeProc2NvDimmN, SanitizeProc{1,2}Dimm{N},
Slot1NicBoot3, Slot1NicBoot4, Slot1NicBoot5, Slot1NicBoot6, Slot1NicBoot7, Slot1NicBoot8, Slot1StorageBoot,
Slot2NicBoot1, Slot2NicBoot2, Slot2NicBoot3, Slot2NicBoot4, Slot2NicBoot5, Slot2NicBoot6, Slot2NicBoot7,
Slot2NicBoot8, Slot2StorageBoot, Slot3NicBoot1, Slot3NicBoot2, Slot3NicBoot3, Slot3NicBoot4, Slot3NicBoot5,
Slot3NicBoot6, Slot3NicBoot7, Slot3NicBoot8, Slot3StorageBoot, Slot4NicBoot1, Slot4NicBoot2, Slot4NicBoot3,
Slot4NicBoot4, Slot4NicBoot5, Slot4NicBoot6, Slot4NicBoot7, Slot4NicBoot8, Slot4StorageBoot, Slot5NicBoot1,
Slot5NicBoot2, Slot5NicBoot3, Slot5NicBoot4, Slot5NicBoot5, Slot5NicBoot6, Slot5NicBoot7, Slot5NicBoot8,
Slot5StorageBoot, Slot6NicBoot1, Slot6NicBoot2, Slot6NicBoot3, Slot6NicBoot4, Slot6NicBoot5, Slot6NicBoot6,
Slot6NicBoot7, Slot6NicBoot8, Slot6StorageBoot, TmOperation, TmVisibility, Tpm2Ppi, Tpm2Visibility,
TpmBinding, TpmOperation, UefiPxeBoot, Usb3Mode, VideoOptions, VirtualInstallDisk
```

이름 변경 후보(문자열 유사도 0.8 이상 자동 매칭 — 문서 근거 아님, 추측):

- IntelQpiFreq -> IntelUpiFreq
- IntelQpiLinkEn -> IntelUpiLinkEn
- IntelQpiPowerManagement -> IntelUpiPowerManagement
- PciSlot2Enable -> PciSlot12Enable
- PciSlot3Enable -> PciSlot12Enable
- PciSlot4Enable -> PciSlot12Enable
- PciSlot5Enable -> PciSlot12Enable
- PciSlot6Enable -> PciSlot12Enable
- Slot1NicBoot3 -> Slot10NicBoot3
- Slot1NicBoot4 -> Slot10NicBoot4
- Slot1NicBoot5 -> Slot10NicBoot4
- Slot1NicBoot6 -> Slot10NicBoot4
- Slot1NicBoot7 -> Slot10NicBoot4
- Slot1NicBoot8 -> Slot10NicBoot4
- Slot1StorageBoot -> Slot12StorageBoot
- Slot2NicBoot1 -> Slot10NicBoot1
- Slot2NicBoot2 -> Slot10NicBoot2
- Slot2NicBoot3 -> Slot10NicBoot3
- Slot2NicBoot4 -> Slot10NicBoot4
- Slot2NicBoot5 -> Slot10NicBoot4
- Slot2NicBoot6 -> Slot10NicBoot4
- Slot2NicBoot7 -> Slot10NicBoot4
- Slot2NicBoot8 -> Slot10NicBoot4
- Slot2StorageBoot -> Slot12StorageBoot
- Slot3NicBoot1 -> Slot10NicBoot1
- Slot3NicBoot2 -> Slot10NicBoot2
- Slot3NicBoot3 -> Slot10NicBoot3
- Slot3NicBoot4 -> Slot10NicBoot4
- Slot3NicBoot5 -> Slot10NicBoot4
- Slot3NicBoot6 -> Slot10NicBoot4
- Slot3NicBoot7 -> Slot10NicBoot4
- Slot3NicBoot8 -> Slot10NicBoot4
- Slot3StorageBoot -> Slot12StorageBoot
- Slot4NicBoot1 -> Slot10NicBoot1
- Slot4NicBoot2 -> Slot10NicBoot2
- Slot4NicBoot3 -> Slot10NicBoot3
- Slot4NicBoot4 -> Slot10NicBoot4
- Slot4NicBoot5 -> Slot10NicBoot4
- Slot4NicBoot6 -> Slot10NicBoot4
- Slot4NicBoot7 -> Slot10NicBoot4
- Slot4NicBoot8 -> Slot10NicBoot4
- Slot4StorageBoot -> Slot12StorageBoot
- Slot5NicBoot1 -> Slot10NicBoot1
- Slot5NicBoot2 -> Slot10NicBoot2
- Slot5NicBoot3 -> Slot10NicBoot3
- Slot5NicBoot4 -> Slot10NicBoot4
- Slot5NicBoot5 -> Slot10NicBoot4
- Slot5NicBoot6 -> Slot10NicBoot4
- Slot5NicBoot7 -> Slot10NicBoot4
- Slot5NicBoot8 -> Slot10NicBoot4
- Slot5StorageBoot -> Slot12StorageBoot
- Slot6NicBoot1 -> Slot10NicBoot1
- Slot6NicBoot2 -> Slot10NicBoot2
- Slot6NicBoot3 -> Slot10NicBoot3
- Slot6NicBoot4 -> Slot10NicBoot4
- Slot6NicBoot5 -> Slot10NicBoot4
- Slot6NicBoot6 -> Slot10NicBoot4
- Slot6NicBoot7 -> Slot10NicBoot4
- Slot6NicBoot8 -> Slot10NicBoot4
- Slot6StorageBoot -> Slot12StorageBoot

### D. 관측: iLO4 문서 이름 vs iLO5 Gen10 U34(2018, iLO5 1.x)

- 기준 iLO4 doc: 234개 / 대상 U34 관측: 235개 / 공통 124 / 추가 111 / 삭제 110

**추가(U34 관측 에만)**

```
AcpiHpet, Chipset_TpmFeatureEnableOrDisable, Chipset_TpmFeatureType, EmbNicAspm, EmbNicLinkSpeed,
EmbNicPCIeOptionROM, EmbSas1Aspm, EmbSas1LinkSpeed, EmbSas1PcieOptionROM, EmbSata1Aspm, EmbSata1PCIeOptionROM,
EmbSata2Aspm, EmbSata2PCIeOptionROM, EnabledCoresPerProc, EnergyEfficientTurbo, FlexLom1Aspm,
FlexLom1LinkSpeed, FlexLom1PCIeOptionROM, HttpSupport, IntelUpiFreq, IntelUpiPowerManagement, Ipv6Address,
Ipv6ConfigPolicy, Ipv6Gateway, Ipv6PrimaryDNS, Ipv6SecondaryDNS, LlcPrefetch, LocalRemoteThreshold,
MemClearWarmReset, MemMirrorMode, MemPatrolScrubbing, MemRefreshRate, MemoryRemap, NetworkBootRetryCount,
NvmeOptionRom, PciResourcePadding, PciSlot10Aspm, PciSlot10LinkSpeed, PciSlot10OptionROM, PciSlot11Aspm,
PciSlot11LinkSpeed, PciSlot11OptionROM, PciSlot12Aspm, PciSlot12LinkSpeed, PciSlot12OptionROM, PciSlot13Aspm,
PciSlot13LinkSpeed, PciSlot13OptionROM, PciSlot14Aspm, PciSlot14LinkSpeed, PciSlot14OptionROM, PciSlot15Aspm,
PciSlot15LinkSpeed, PciSlot15OptionROM, PciSlot16Aspm, PciSlot16LinkSpeed, PciSlot16OptionROM, PciSlot1Aspm,
PciSlot1LinkSpeed, PciSlot1OptionROM, PciSlot2Aspm, PciSlot2LinkSpeed, PciSlot2OptionROM, PciSlot3Aspm,
PciSlot3LinkSpeed, PciSlot3OptionROM, PciSlot4Aspm, PciSlot4LinkSpeed, PciSlot4OptionROM, PciSlot5Aspm,
PciSlot5LinkSpeed, PciSlot5OptionROM, PciSlot6Aspm, PciSlot6LinkSpeed, PciSlot6OptionROM, PciSlot7Aspm,
PciSlot7LinkSpeed, PciSlot7OptionROM, PciSlot8Aspm, PciSlot8LinkSpeed, PciSlot8OptionROM, PciSlot9Aspm,
PciSlot9LinkSpeed, PciSlot9OptionROM, PersistentMemAddressRangeScrub, PersistentMemBackupPowerPolicy,
PersistentMemScanMem, PostBootProgress, PostDiscoveryMode, PrebootNetworkEnvPolicy, PrebootNetworkProxy,
ProcessorJitterControl, ProcessorJitterControlFrequency, PwrSupplyReq, SecStartBackupImage,
SetupBrowserSelection, SubNumaClustering, TpmChipId, TpmFips, UefiSerialDebugLevel,
UefiShellScriptVerification, UefiShellStartupUrlFromDhcp, UncoreFreqScaling, UpiPrefetcher, UrlBootFile2,
UrlBootFile3, UrlBootFile4, UserDefaultsState, WorkloadProfile, XptPrefetcher, iSCSIPolicy
```

**삭제(iLO4 doc 에만)**

```
AdminPassword, DynamicPowerResponse, EmbSas1Boot, EmbSasEnable, EmbeddedUserPartition, IntelQpiFreq,
IntelQpiLinkEn, IntelQpiPowerManagement, IntelTxt, IoNonPostedPrefetching, NicBoot10, NicBoot11, NicBoot12,
NicBoot5, NicBoot6, NicBoot7, NicBoot8, NicBoot9, NmiDebugButton, NvDimmNBackupPowerPolicy,
NvDimmNForcedRecovery, NvDimmNIntegrityChecking, NvDimmNMemFunctionality, NvDimmNMemInterleaving,
NvDimmNSanitizePolicy, OldAdminPassword, OldPowerOnPassword, PciBusPadding, PciSlot1Enable, PciSlot2Enable,
PciSlot3Enable, PciSlot4Enable, PciSlot6Enable, PcieExpressEcrcSupport, PowerOnPassword, PowerProfile,
ProcCoreDisable, ProcNoExecute, QpiBandwidthOpt, QpiHomeSnoopOpt, QpiSnoopConfig, SanitizeAllNvDimmN,
SanitizeProc1NvDimmN, SanitizeProc2NvDimmN, SanitizeProc{1,2}Dimm{N}, Slot1NicBoot1, Slot1NicBoot2,
Slot1NicBoot3, Slot1NicBoot4, Slot1NicBoot5, Slot1NicBoot6, Slot1NicBoot7, Slot1NicBoot8, Slot1StorageBoot,
Slot2NicBoot1, Slot2NicBoot2, Slot2NicBoot3, Slot2NicBoot4, Slot2NicBoot5, Slot2NicBoot6, Slot2NicBoot7,
Slot2NicBoot8, Slot2StorageBoot, Slot3NicBoot1, Slot3NicBoot2, Slot3NicBoot3, Slot3NicBoot4, Slot3NicBoot5,
Slot3NicBoot6, Slot3NicBoot7, Slot3NicBoot8, Slot3StorageBoot, Slot4NicBoot1, Slot4NicBoot2, Slot4NicBoot3,
Slot4NicBoot4, Slot4NicBoot5, Slot4NicBoot6, Slot4NicBoot7, Slot4NicBoot8, Slot4StorageBoot, Slot5NicBoot1,
Slot5NicBoot2, Slot5NicBoot3, Slot5NicBoot4, Slot5NicBoot5, Slot5NicBoot6, Slot5NicBoot7, Slot5NicBoot8,
Slot6NicBoot1, Slot6NicBoot2, Slot6NicBoot3, Slot6NicBoot4, Slot6NicBoot5, Slot6NicBoot6, Slot6NicBoot7,
Slot6NicBoot8, Slot6StorageBoot, TmOperation, TmVisibility, Tpm2Operation, Tpm2Ppi, Tpm2Visibility,
TpmBinding, TpmOperation, TpmUefiOpromMeasuring, TpmVisibility, UefiPxeBoot, Usb3Mode, VideoOptions
```

이름 변경 후보(문자열 유사도 0.8 이상 자동 매칭 — 문서 근거 아님, 추측):

- IntelQpiFreq -> IntelUpiFreq
- IntelQpiPowerManagement -> IntelUpiPowerManagement

## 3. 모델 영향
- DL360 G9/XL170r G9/XL250 G9/XL270d G9 (iLO4) 와 Gen10 계열(iLO5)을 같은 스크립트로 비교하려면 (a) 속성 위치(`Attributes`), (b) 숫자 enum 접두어, (c) 이름 추가/삭제를 정규화해야 한다.
- 정확한 속성 단위 차이(모델/ROM 별)는 Support Center 의 ROM 별 레지스트리 zip(링크: 각 README)을 직접 받아야 확정 가능 — unavailable.
