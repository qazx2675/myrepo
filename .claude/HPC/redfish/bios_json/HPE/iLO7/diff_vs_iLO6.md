# iLO7 vs iLO6 BIOS 차이 (HPE)

기준: iLO6(Gen11) -> 대상: iLO7(Gen12). **사용자 보유 모델 없음 — 공개 자료 범위 best-effort.** 생성: 2026-10-10.

## A. 공식 문서끼리 (iLO6 v1.79 vs iLO7 v1.25 Bios resource definitions)
- 이름·allowed_values 완전 동일: **True** (540 vs 540). iLO7 문서는 iLO6 문서의 복사본이며 문서 안에 `Added: iLO6 1.10` 문구까지 그대로 남아 있음(스크립트 확인). iLO8 v1.10 문서도 동일 복사본. -> 문서로는 iLO7 전용 속성 구분 불가 (unavailable).
- `@odata.type` 은 둘 다 `#Bios.v1_0_5.Bios` 로 문서 표기. 관측: iLO7 DL360 Gen12 는 `Bios.v1_2_3` (iLO 7 1.14).

### B. 관측: iLO6 DL380a Gen11 U58 vs iLO7 DL360 Gen12 U68

- 기준 iLO6 U58: 300개 / 대상 iLO7 U68: 256개 / 공통 227 / 추가 29 / 삭제 73

**추가(iLO7 U68 에만)**

```
DynamicIntelSpeedSelectMode, EfficiencyLatencyControl, EnabledModulesPerProc, EnhancedCStates,
IntelSpeedSelectConfigLevel, OcpAAspm, OcpAAuxiliaryPower, OcpABifurcation, OcpADataLinkFeatureExchange,
OcpAEnable, OcpAEoiBroadcastSupport, OcpALinkSpeed, OcpAMctpBroadcastSupport, OcpAOptionROM, OcpAStorageBoot,
PciSlot1Aspm, PciSlot1Bifurcation, PciSlot1Enable, PciSlot1LinkSpeed, PciSlot1OptionROM, PcieLinkRetraining,
RemoteXptPrefetcher, Slot1DataLinkFeatureExchange, Slot1EoiBroadcastSupport, Slot1MctpBroadcastSupport,
Slot1NicBoot1, Slot1NicBoot2, Slot2StorageBoot, XptPrefetcher
```

**삭제(iLO6 U58 에만)**

```
DfxTdxDisable1MbCmrExclude, EmbeddedSata, IntelPchVmdSupport, IntelPriorityBaseFreq, IntelPriorityCorePower,
Ocp1AuxiliaryPower, Ocp2AuxiliaryPower, OptimizedPowerMode, PchCrashLogFeature, PciSlot11Bifurcation,
PciSlot14Aspm, PciSlot14Bifurcation, PciSlot14Enable, PciSlot14LinkSpeed, PciSlot14OptionROM, PciSlot16Aspm,
PciSlot16Bifurcation, PciSlot16Enable, PciSlot16LinkSpeed, PciSlot16OptionROM, PciSlot17Aspm,
PciSlot17Bifurcation, PciSlot17Enable, PciSlot17LinkSpeed, PciSlot17OptionROM, PciSlot18Aspm,
PciSlot18Bifurcation, PciSlot18Enable, PciSlot18LinkSpeed, PciSlot18OptionROM, PciSlot3Bifurcation,
PciSlot5Bifurcation, PciSlot6Bifurcation, PciSlot9Bifurcation, PersistentMemBackupPowerPolicy,
PlatformCertificate, ProcX2Apic, RedundantPowerSupplyGpuDomain, RomSelection, SataSanitize, SataSecureErase,
SecStartBackupImage, Slot11DataLinkFeatureExchange, Slot11MctpBroadcastSupport, Slot14DataLinkFeatureExchange,
Slot14EoiBroadcastSupport, Slot14MctpBroadcastSupport, Slot16DataLinkFeatureExchange,
Slot16MctpBroadcastSupport, Slot17DataLinkFeatureExchange, Slot17MctpBroadcastSupport, Slot17StorageBoot,
Slot18DataLinkFeatureExchange, Slot18MctpBroadcastSupport, Slot18StorageBoot, Slot2NicBoot1, Slot2NicBoot2,
Slot3DataLinkFeatureExchange, Slot3EoiBroadcastSupport, Slot3MctpBroadcastSupport,
Slot5DataLinkFeatureExchange, Slot5EoiBroadcastSupport, Slot5MctpBroadcastSupport,
Slot6DataLinkFeatureExchange, Slot6EoiBroadcastSupport, Slot6MctpBroadcastSupport,
Slot9DataLinkFeatureExchange, Slot9EoiBroadcastSupport, Slot9MctpBroadcastSupport, UncoreFreqScaling,
UncoreFrequencyMAX, UncoreFrequencyMIN, Upi3Link
```

이름 변경 후보(문자열 유사도 0.8 이상 자동 매칭 — 문서 근거 아님, 추측):

- Ocp1AuxiliaryPower -> OcpAAuxiliaryPower
- Ocp2AuxiliaryPower -> OcpAAuxiliaryPower
- PciSlot11Bifurcation -> PciSlot1Bifurcation
- PciSlot14Aspm -> PciSlot1Aspm
- PciSlot14Bifurcation -> PciSlot1Bifurcation
- PciSlot14Enable -> PciSlot1Enable
- PciSlot14LinkSpeed -> PciSlot1LinkSpeed
- PciSlot14OptionROM -> PciSlot1OptionROM
- PciSlot16Aspm -> PciSlot1Aspm
- PciSlot16Bifurcation -> PciSlot1Bifurcation
- PciSlot16Enable -> PciSlot1Enable
- PciSlot16LinkSpeed -> PciSlot1LinkSpeed
- PciSlot16OptionROM -> PciSlot1OptionROM
- PciSlot17Aspm -> PciSlot1Aspm
- PciSlot17Bifurcation -> PciSlot1Bifurcation
- PciSlot17Enable -> PciSlot1Enable
- PciSlot17LinkSpeed -> PciSlot1LinkSpeed
- PciSlot17OptionROM -> PciSlot1OptionROM
- PciSlot18Aspm -> PciSlot1Aspm
- PciSlot18Bifurcation -> PciSlot1Bifurcation
- PciSlot18Enable -> PciSlot1Enable
- PciSlot18LinkSpeed -> PciSlot1LinkSpeed
- PciSlot18OptionROM -> PciSlot1OptionROM
- PciSlot3Bifurcation -> PciSlot1Bifurcation
- PciSlot5Bifurcation -> PciSlot1Bifurcation
- PciSlot6Bifurcation -> PciSlot1Bifurcation
- PciSlot9Bifurcation -> PciSlot1Bifurcation
- Slot11DataLinkFeatureExchange -> Slot1DataLinkFeatureExchange
- Slot11MctpBroadcastSupport -> Slot1MctpBroadcastSupport
- Slot14DataLinkFeatureExchange -> Slot1DataLinkFeatureExchange
- Slot14EoiBroadcastSupport -> Slot1EoiBroadcastSupport
- Slot14MctpBroadcastSupport -> Slot1MctpBroadcastSupport
- Slot16DataLinkFeatureExchange -> Slot1DataLinkFeatureExchange
- Slot16MctpBroadcastSupport -> Slot1MctpBroadcastSupport
- Slot17DataLinkFeatureExchange -> Slot1DataLinkFeatureExchange
- Slot17MctpBroadcastSupport -> Slot1MctpBroadcastSupport
- Slot17StorageBoot -> Slot2StorageBoot
- Slot18DataLinkFeatureExchange -> Slot1DataLinkFeatureExchange
- Slot18MctpBroadcastSupport -> Slot1MctpBroadcastSupport
- Slot18StorageBoot -> Slot2StorageBoot
- Slot2NicBoot1 -> Slot1NicBoot1
- Slot2NicBoot2 -> Slot1NicBoot2
- Slot3DataLinkFeatureExchange -> Slot1DataLinkFeatureExchange
- Slot3EoiBroadcastSupport -> Slot1EoiBroadcastSupport
- Slot3MctpBroadcastSupport -> Slot1MctpBroadcastSupport
- Slot5DataLinkFeatureExchange -> Slot1DataLinkFeatureExchange
- Slot5EoiBroadcastSupport -> Slot1EoiBroadcastSupport
- Slot5MctpBroadcastSupport -> Slot1MctpBroadcastSupport
- Slot6DataLinkFeatureExchange -> Slot1DataLinkFeatureExchange
- Slot6EoiBroadcastSupport -> Slot1EoiBroadcastSupport
- Slot6MctpBroadcastSupport -> Slot1MctpBroadcastSupport
- Slot9DataLinkFeatureExchange -> Slot1DataLinkFeatureExchange
- Slot9EoiBroadcastSupport -> Slot1EoiBroadcastSupport
- Slot9MctpBroadcastSupport -> Slot1MctpBroadcastSupport

### C. 관측: iLO6 DL380a Gen11 U58 vs iLO7 DL380a Gen12 U72 (같은 모델 라인, 세대만 다름)

- 기준 iLO6 U58: 300개 / 대상 iLO7 U72: 350개 / 공통 242 / 추가 108 / 삭제 58

**추가(iLO7 U72 에만)**

```
DynamicIntelSpeedSelectMode, EfficiencyLatencyControl, EnabledModulesPerProc, EnhancedCStates,
HpmRebindingRequest, IntelSpeedSelectConfigLevel, OcpAAuxiliaryPower, PciSlot13Aspm, PciSlot13Enable,
PciSlot13LinkSpeed, PciSlot13OptionROM, PciSlot15Aspm, PciSlot15Enable, PciSlot15LinkSpeed,
PciSlot15OptionROM, PciSlot19Aspm, PciSlot19Enable, PciSlot19LinkSpeed, PciSlot19OptionROM, PciSlot25Aspm,
PciSlot25Bifurcation, PciSlot25Enable, PciSlot25LinkSpeed, PciSlot25OptionROM, PciSlot26Aspm,
PciSlot26Bifurcation, PciSlot26Enable, PciSlot26LinkSpeed, PciSlot26OptionROM, PciSlot29Aspm, PciSlot29Enable,
PciSlot29LinkSpeed, PciSlot29OptionROM, PciSlot3Aspm, PciSlot3Enable, PciSlot3LinkSpeed, PciSlot3OptionROM,
PciSlot7Aspm, PciSlot7Bifurcation, PciSlot7Enable, PciSlot7LinkSpeed, PciSlot7OptionROM, PciSlot8Aspm,
PciSlot8Bifurcation, PciSlot8Enable, PciSlot8LinkSpeed, PciSlot8OptionROM, PcieLinkRetraining,
RedundantPowerSupplyGpuDomain1, RedundantPowerSupplyGpuDomain2, RemoteXptPrefetcher,
Slot10DataLinkFeatureExchange, Slot10EoiBroadcastSupport, Slot10MctpBroadcastSupport,
Slot11EoiBroadcastSupport, Slot12DataLinkFeatureExchange, Slot12EoiBroadcastSupport,
Slot12MctpBroadcastSupport, Slot13DataLinkFeatureExchange, Slot13EoiBroadcastSupport,
Slot13MctpBroadcastSupport, Slot15DataLinkFeatureExchange, Slot15EoiBroadcastSupport,
Slot15MctpBroadcastSupport, Slot16EoiBroadcastSupport, Slot17EoiBroadcastSupport, Slot18EoiBroadcastSupport,
Slot19DataLinkFeatureExchange, Slot19EoiBroadcastSupport, Slot19MctpBroadcastSupport,
Slot20DataLinkFeatureExchange, Slot20EoiBroadcastSupport, Slot20MctpBroadcastSupport,
Slot21DataLinkFeatureExchange, Slot21EoiBroadcastSupport, Slot21MctpBroadcastSupport,
Slot22DataLinkFeatureExchange, Slot22EoiBroadcastSupport, Slot22MctpBroadcastSupport,
Slot23DataLinkFeatureExchange, Slot23EoiBroadcastSupport, Slot23MctpBroadcastSupport,
Slot24DataLinkFeatureExchange, Slot24EoiBroadcastSupport, Slot24MctpBroadcastSupport,
Slot25DataLinkFeatureExchange, Slot25EoiBroadcastSupport, Slot25MctpBroadcastSupport, Slot25NicBoot1,
Slot26DataLinkFeatureExchange, Slot26EoiBroadcastSupport, Slot26MctpBroadcastSupport, Slot26NicBoot1,
Slot3NicBoot1, Slot3NicBoot2, Slot7DataLinkFeatureExchange, Slot7EoiBroadcastSupport,
Slot7MctpBroadcastSupport, Slot7NicBoot1, Slot8DataLinkFeatureExchange, Slot8EoiBroadcastSupport,
Slot8MctpBroadcastSupport, Slot8NicBoot1, VmdonCpu1Stack4Port1, VmdonCpu1Stack4Port3, VmdonCpu1Stack4Port5,
VmdonCpu1Stack4Port7, XptPrefetcher
```

**삭제(iLO6 U58 에만)**

```
DfxTdxDisable1MbCmrExclude, EmbeddedSata, IntelPchVmdSupport, IntelPriorityCorePower, Ocp1AuxiliaryPower,
Ocp2AuxiliaryPower, OptimizedPowerMode, PchCrashLogFeature, PciSlot11Bifurcation, PciSlot14Aspm,
PciSlot14Bifurcation, PciSlot14Enable, PciSlot14LinkSpeed, PciSlot14OptionROM, PciSlot16Aspm,
PciSlot16Bifurcation, PciSlot16Enable, PciSlot16LinkSpeed, PciSlot16OptionROM, PciSlot17Bifurcation,
PciSlot18Aspm, PciSlot18Bifurcation, PciSlot18Enable, PciSlot18LinkSpeed, PciSlot18OptionROM, PciSlot2Aspm,
PciSlot2Bifurcation, PciSlot2Enable, PciSlot2LinkSpeed, PciSlot2OptionROM, PciSlot5Bifurcation,
PciSlot6Bifurcation, PciSlot9Bifurcation, PersistentMemBackupPowerPolicy, PlatformCertificate, ProcX2Apic,
RedundantPowerSupplyGpuDomain, RomSelection, SataSanitize, SataSecureErase, SecStartBackupImage,
Slot17StorageBoot, Slot18StorageBoot, Slot2DataLinkFeatureExchange, Slot2EoiBroadcastSupport,
Slot2MctpBroadcastSupport, Slot2NicBoot1, Slot2NicBoot2, Slot5DataLinkFeatureExchange,
Slot5EoiBroadcastSupport, Slot5MctpBroadcastSupport, Slot6DataLinkFeatureExchange, Slot6EoiBroadcastSupport,
Slot6MctpBroadcastSupport, UncoreFreqScaling, UncoreFrequencyMAX, UncoreFrequencyMIN, Upi3Link
```

이름 변경 후보(문자열 유사도 0.8 이상 자동 매칭 — 문서 근거 아님, 추측):

- Ocp1AuxiliaryPower -> OcpAAuxiliaryPower
- Ocp2AuxiliaryPower -> OcpAAuxiliaryPower
- PciSlot11Bifurcation -> PciSlot8Bifurcation
- PciSlot14Aspm -> PciSlot19Aspm
- PciSlot14Bifurcation -> PciSlot8Bifurcation
- PciSlot14Enable -> PciSlot19Enable
- PciSlot14LinkSpeed -> PciSlot19LinkSpeed
- PciSlot14OptionROM -> PciSlot19OptionROM
- PciSlot16Aspm -> PciSlot26Aspm
- PciSlot16Bifurcation -> PciSlot26Bifurcation
- PciSlot16Enable -> PciSlot26Enable
- PciSlot16LinkSpeed -> PciSlot26LinkSpeed
- PciSlot16OptionROM -> PciSlot26OptionROM
- PciSlot17Bifurcation -> PciSlot7Bifurcation
- PciSlot18Aspm -> PciSlot8Aspm
- PciSlot18Bifurcation -> PciSlot8Bifurcation
- PciSlot18Enable -> PciSlot8Enable
- PciSlot18LinkSpeed -> PciSlot8LinkSpeed
- PciSlot18OptionROM -> PciSlot8OptionROM
- PciSlot2Aspm -> PciSlot29Aspm
- PciSlot2Bifurcation -> PciSlot26Bifurcation
- PciSlot2Enable -> PciSlot29Enable
- PciSlot2LinkSpeed -> PciSlot29LinkSpeed
- PciSlot2OptionROM -> PciSlot29OptionROM
- PciSlot5Bifurcation -> PciSlot25Bifurcation
- PciSlot6Bifurcation -> PciSlot26Bifurcation
- PciSlot9Bifurcation -> PciSlot8Bifurcation
- RedundantPowerSupplyGpuDomain -> RedundantPowerSupplyGpuDomain2
- Slot2DataLinkFeatureExchange -> Slot26DataLinkFeatureExchange
- Slot2EoiBroadcastSupport -> Slot26EoiBroadcastSupport
- Slot2MctpBroadcastSupport -> Slot26MctpBroadcastSupport
- Slot2NicBoot1 -> Slot26NicBoot1
- Slot2NicBoot2 -> Slot3NicBoot2
- Slot5DataLinkFeatureExchange -> Slot25DataLinkFeatureExchange
- Slot5EoiBroadcastSupport -> Slot25EoiBroadcastSupport
- Slot5MctpBroadcastSupport -> Slot25MctpBroadcastSupport
- Slot6DataLinkFeatureExchange -> Slot26DataLinkFeatureExchange
- Slot6EoiBroadcastSupport -> Slot26EoiBroadcastSupport
- Slot6MctpBroadcastSupport -> Slot26MctpBroadcastSupport

주의: 비교 대상이 서로 다른 플랫폼·ROM 이므로 모델/ROM 효과가 섞여 있음. 사용자 보유 iLO7 모델 없음.

## D. OEM URI/구조 차이
- iLO6 와 동일한 `Bios/Oem/Hpe/*` 구조 유지 (관측 U68/U72: `baseconfigs, boot, mappings, nvmeof, serverconfiglock, tlsconfig, iscsi`). 관측 차이: iLO6 1.66(U58) 에는 `kmsconfig` 링크가 있으나 iLO7 관측 3종(U68/U71/U72) 모두 `KmsConfig` 링크가 없음. 단 U68(DL360 Gen12) 은 `NvmeOf` 도 있음. 관측 mock 3종 모두 `Bios.v1_2_3`.
- iLO7 공식 adaptation 문서의 BIOS 관련 사항: `SerialInterface` 가 1.11.0 이후 읽기전용 -> BIOS 속성 `EmbeddedSerialPort`(Disabled/Com1Irq4 …) 를 `PATCH /redfish/v1/Systems/1/bios/settings/` 의 `Attributes` 로 설정. 레거시 pre-Redfish API 완전 제거. `HpeiLOEmbeddedMedia` 스키마 제거.
- 액션 target: iLO6/iLO7 모두 `/redfish/v1/systems/1/bios/Actions/Bios.ResetBios/` (관측).
