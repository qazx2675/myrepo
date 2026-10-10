# iLO6 vs iLO5 BIOS 차이 (HPE)

기준: iLO5(Gen10/Gen10 Plus) -> 대상: iLO6(Gen11). 생성: 2026-10-10.

## A. 공식 문서끼리 (iLO5 v3.19 vs iLO6 v1.79 Bios resource definitions)
- 속성 이름·type·allowed_values·read_only 가 **완전히 동일: True** (540 vs 540, 추가 0/삭제 0/변경 0). 두 문서는 같은 AMD 플랫폼 샘플의 복사본이다. 따라서 문서만으로는 iLO5->iLO6 속성 차이를 알 수 없다 (문서 위치: iLO5 3.19 `…/ilo5_319/ilo5_bios_resourcedefns319.md`, iLO6 1.79 `…/ilo6_179/ilo6_bios_resourcedefns179.md`).
- 문서 차이는 스키마 버전뿐: iLO5 `Bios.v1_0_0` -> iLO6 `Bios.v1_0_5`(관측: DL380a Gen11 `Bios.v1_0_4`), `@Redfish.Settings` 절 추가, 링크 목록에서 `Oem/Hpe/Links/ScalablePmem` 삭제.

### B. 관측: iLO5 Gen10 Plus U46(iLO5 3.11) vs iLO6 DL380a Gen11 U58(iLO6 1.66)

- 기준 iLO5 U46: 276개 / 대상 iLO6 U58: 300개 / 공통 219 / 추가 81 / 삭제 57

**추가(iLO6 U58 에만)**

```
AvxIccpPreGrantLevel, AvxLicensePreGrantOverride, CpuCrashLogFeature, DfxTdxDisable1MbCmrExclude,
HardwarePmInterrupt, IntelPchVmdSupport, IntelPriorityBaseFreq, IntelPriorityCorePower,
IoatSnoopResponseHoldOff, KeySplit, MemoryPermanentFaultDetect, NvmeOfSoftwareInitiator, Ocp2AuxiliaryPower,
OptimizedPowerMode, OsbLocalRemoteRead, PchCrashLogFeature, PciSlot11Bifurcation, PciSlot14Aspm,
PciSlot14Bifurcation, PciSlot14Enable, PciSlot14LinkSpeed, PciSlot14OptionROM, PciSlot16Aspm,
PciSlot16Bifurcation, PciSlot16Enable, PciSlot16LinkSpeed, PciSlot16OptionROM, PciSlot17Aspm,
PciSlot17Bifurcation, PciSlot17Enable, PciSlot17LinkSpeed, PciSlot17OptionROM, PciSlot18Aspm,
PciSlot18Bifurcation, PciSlot18Enable, PciSlot18LinkSpeed, PciSlot18OptionROM, PciSlot2Aspm, PciSlot2Enable,
PciSlot2LinkSpeed, PciSlot2OptionROM, PciSlot3Bifurcation, PciSlot5Bifurcation, PciSlot6Bifurcation,
PciSlot9Bifurcation, PcieGlobalAspm, PersistentMemBackupPowerPolicy, RedundantPowerSupplyGpuDomain,
RedundantPowerSupplySystemDomain, SgxFactoryReset, SgxPrmrrSize, Slot11DataLinkFeatureExchange,
Slot11MctpBroadcastSupport, Slot14DataLinkFeatureExchange, Slot14EoiBroadcastSupport,
Slot14MctpBroadcastSupport, Slot16DataLinkFeatureExchange, Slot16MctpBroadcastSupport,
Slot17DataLinkFeatureExchange, Slot17MctpBroadcastSupport, Slot17StorageBoot, Slot18DataLinkFeatureExchange,
Slot18MctpBroadcastSupport, Slot18StorageBoot, Slot2NicBoot1, Slot2NicBoot2, Slot3DataLinkFeatureExchange,
Slot3EoiBroadcastSupport, Slot3MctpBroadcastSupport, Slot5DataLinkFeatureExchange, Slot5EoiBroadcastSupport,
Slot5MctpBroadcastSupport, Slot6DataLinkFeatureExchange, Slot6EoiBroadcastSupport, Slot6MctpBroadcastSupport,
Slot9DataLinkFeatureExchange, Slot9EoiBroadcastSupport, Slot9MctpBroadcastSupport, TdxEnable,
TdxSeamldrEnable, Upi3Link
```

**삭제(iLO5 U46 에만)**

```
BootMode, EnhancedProcPerf, IntelPerfMonitoring, LocalRemoteThreshold, NoExecutionProtection, NvmePort1,
NvmePort10, NvmePort2, NvmePort9, NvmeRaid, PciPeerToPeerSerialization, PciSlot10Aspm, PciSlot10Bifurcation,
PciSlot10Enable, PciSlot10LinkSpeed, PciSlot10OptionROM, PciSlot12Aspm, PciSlot12Enable, PciSlot12LinkSpeed,
PciSlot12OptionROM, PciSlot1Aspm, PciSlot1Bifurcation, PciSlot1Enable, PciSlot1LinkSpeed, PciSlot1OptionROM,
RedundantPowerSupply, RemoteXptPrefetcher, RemovableFlashBootSeq, Slot10DataLinkFeatureExchange,
Slot10EoiBroadcastSupport, Slot10MctpBroadcastSupport, Slot10NicBoot1, Slot10NicBoot2, Slot10NicBoot3,
Slot10NicBoot4, Slot12StorageBoot, Slot1DataLinkFeatureExchange, Slot1EoiBroadcastSupport,
Slot1MctpBroadcastSupport, Slot1NicBoot1, Slot1NicBoot2, Tpm20SoftwareInterfaceOperation, TpmFips,
TpmFipsModeSwitch, TpmModeSwitchOperation, TpmType, UefiOptimizedBoot, VmProprietaryPageRetireSupport,
VmdonCpu1Stack5Port1, VmdonCpu1Stack5Port2, VmdonCpu1Stack5Port3, VmdonCpu1Stack5Port4, VmdonCpu2Stack5Port1,
VmdonCpu2Stack5Port2, VmdonCpu2Stack5Port3, VmdonCpu2Stack5Port4, XptPrefetcher
```

이름 변경 후보(문자열 유사도 0.8 이상 자동 매칭 — 문서 근거 아님, 추측):

- PciSlot10Aspm -> PciSlot18Aspm
- PciSlot10Bifurcation -> PciSlot18Bifurcation
- PciSlot10Enable -> PciSlot18Enable
- PciSlot10LinkSpeed -> PciSlot18LinkSpeed
- PciSlot10OptionROM -> PciSlot18OptionROM
- PciSlot12Aspm -> PciSlot2Aspm
- PciSlot12Enable -> PciSlot2Enable
- PciSlot12LinkSpeed -> PciSlot2LinkSpeed
- PciSlot12OptionROM -> PciSlot2OptionROM
- PciSlot1Aspm -> PciSlot18Aspm
- PciSlot1Bifurcation -> PciSlot18Bifurcation
- PciSlot1Enable -> PciSlot18Enable
- PciSlot1LinkSpeed -> PciSlot18LinkSpeed
- PciSlot1OptionROM -> PciSlot18OptionROM
- RedundantPowerSupply -> RedundantPowerSupplyGpuDomain
- Slot10DataLinkFeatureExchange -> Slot18DataLinkFeatureExchange
- Slot10EoiBroadcastSupport -> Slot14EoiBroadcastSupport
- Slot10MctpBroadcastSupport -> Slot18MctpBroadcastSupport
- Slot10NicBoot1 -> Slot2NicBoot1
- Slot10NicBoot2 -> Slot2NicBoot2
- Slot10NicBoot3 -> Slot2NicBoot2
- Slot10NicBoot4 -> Slot2NicBoot2
- Slot12StorageBoot -> Slot18StorageBoot
- Slot1DataLinkFeatureExchange -> Slot18DataLinkFeatureExchange
- Slot1EoiBroadcastSupport -> Slot14EoiBroadcastSupport
- Slot1MctpBroadcastSupport -> Slot18MctpBroadcastSupport
- Slot1NicBoot1 -> Slot2NicBoot1
- Slot1NicBoot2 -> Slot2NicBoot2

주의: U46(Gen10 Plus)와 U58(DL380a Gen11)은 **플랫폼·ROM 이 달라** 순수한 iLO 버전 차이가 아니다. 사용자 DL360/DL380 Gen11(U54), DL560 Gen11(U59) 레지스트리는 확보 못함(unavailable).

## C. OEM URI 구조 차이 (verified: ilo6_adaptation 문서 + biosdoc + U46/U58 관측)
| iLO5 Gen10 (구) | iLO5 Gen10 Plus / iLO6 |
|---|---|
| `/redfish/v1/systems/{item}/bios/baseconfigs/` | `/redfish/v1/systems/{item}/bios/oem/hpe/baseconfigs/` |
| `…/bios/boot/` | `…/bios/oem/hpe/boot/` |
| `…/bios/kmsconfig/` | `…/bios/oem/hpe/kmsconfig/` |
| `…/bios/mappings/` | `…/bios/oem/hpe/mappings/` |
| `…/bios/serverconfiglock/` | `…/bios/oem/hpe/serverconfiglock/` |
| `…/bios/tlsconfig/` | `…/bios/oem/hpe/tlsconfig/` |
| `…/bios/iscsi/` | `…/bios/oem/hpe/iscsi/` |
| `…/bios/scalablepmem/` (U34 관측 `hpescalablepmem`) | iLO6 v1.05 에서 deprecated |
| (없음) | `…/bios/oem/hpe/nvmeof/` (DL380a Gen11 관측, iLO6 1.66) |

- 액션 target 위치: iLO5(관측 1.x/3.11) `/redfish/v1/systems/1/bios/settings/Actions/Bios.ResetBios/` -> iLO6(1.66 관측) `/redfish/v1/systems/1/bios/Actions/Bios.ResetBios/` (Bios 리소스 바로 아래). 관측 mock 은 ChangePassword 의 target 이 `Bios.ChangePasswords` (복수형)로 표기 — 문서는 `Bios.ChangePassword`. 장비의 `Actions` 에 적힌 target 을 그대로 따를 것.
- 기본값: 속성 레지스트리 파일의 `DefaultValue` 방식(biosdoc: Gen10 Plus/Gen11 만 유효) 또는 `Bios/Oem/Hpe/BaseConfigs`.
