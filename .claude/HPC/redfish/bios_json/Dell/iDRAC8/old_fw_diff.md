# iDRAC8 펌웨어 라인 간 BIOS 관련 차이

## Redfish BIOS 지원 시작 시점 (공식 문서 원문 확인)

| iDRAC7/8 펌웨어 | 변화 | 1차 출처 | 2차 출처 | 상태 |
|---|---|---|---|---|
| 2.30.30.30 계열 (iDRAC8 2.30.119.30 릴리스노트) | **Redfish 1.0 최초 지원**. BIOS 리소스는 아직 없음 | iDRAC8 2.30.119.30 Release Notes "Added support for Redfish 1.0" | 2.40.40.40 Release Notes 의 이전 버전 목록(2.35/2.30/2.32) | verified |
| 2.40.40.40 (2016-08) | Redfish 1.0.2, **SCP(서버 설정 프로파일) Redfish 지원** (`EID_674_Manager.Export/ImportSystemConfiguration`). Release Notes 의 BIOS/UEFI 항목은 "N/A" | iDRAC7/8 2.40.40.40 Release Notes | sailfish FC630(2.41.40.40)·C6320(2.40.40.40) 캡처: Manager.Actions 에 SCP 3종이 있고 Systems 아래 Bios 리소스는 없음 | verified |
| **2.50.50.50** | **Redfish BIOS 설정 지원 시작** ("Support Redfish BIOS configuration by implementing Redfish 2016 Release 1 Attribute Registry and BIOS schemas"). `/Bios`, `/Bios/Settings`, `/Bios/BiosRegistry`, ResetBios, ChangePassword, ClearPending. SCP JSON 형식 지원 | iDRAC7/8 2.50.50.50 Release Notes + 2.50.50.50 Redfish API Guide ("Supports Redfish 2016.1 including BIOS and Secure Boot configuration") | sailfish T630·R720 2.50.50.50 실캡처(Bios.v1_0_0, ResetBios/ChangePassword) | verified |
| 2.60.60.60 | Redfish 1.0.2 범위 확대(스토리지/네트워크/메모리/UpdateService). BIOS URI 는 2.50 과 같음 | 2.60.60.60 Redfish API Guide | — | verified (문서) |
| 2.81.81.81 | Bios.v1_0_3, Settings 에 `SupportedApplyTimes`(OnReset, AtMaintenanceWindowStart, InMaintenanceWindowOnReset) | harvester/seeder R630 실캡처 | — | unverified (단일 출처) |
| 2.86.86.86 | iDRAC8 마지막 펌웨어로 알려짐 | 웹검색 요약(Dell SLN310710, 본문은 403으로 직접 확인 못함) | 없음 | unverified |

→ **2.50.50.50 미만의 iDRAC8(및 iDRAC7)은 Redfish 로 BIOS 를 읽거나 바꿀 수 없다.** 이 경우 BIOS 는 WS-MAN(**xml**, `DCIM_BIOSService`/`DCIM_BIOSEnumeration`)·racadm(`racadm get/set BIOS.*`) 또는 2.40.40.40 이상이면 Redfish SCP 로만 다룰 수 있다.

## 문서 불일치 메모

2.50/2.60 Redfish API Guide 는 ClearPending URL 을 `/redfish/v1/Systems/<ID>/Bios/Actions/Oem/DellManager.ClearPending` 로 적었다. 그러나 실캡처(R720·R630, iDRAC7/8 2.50~2.81)와 iDRAC9 API Guide 4.20, Ansible `idrac_bios` 는 모두 `/redfish/v1/Systems/System.Embedded.1/Bios/Settings/Actions/Oem/DellManager.ClearPending` 를 쓴다. 실장비에서는 `GET .../Bios/Settings` 의 `Actions.Oem` target 을 그대로 쓰는 것이 안전하다.

## 실캡처 비교: 2.50.50.50 (T630) vs 2.81.81.81 (R630)

## iDRAC8 2.50.50.50(T630, BIOS 2.2.2) vs 2.81.81.81(R630, BIOS 2.13.0) 노출 속성 이름 차이

- A: PowerEdge T630 (iDRAC 2.50.50.50, BIOS 2.2.2) — GET /Bios 노출 180개
- B: PowerEdge R630 (iDRAC 2.81.81.81, BIOS 2.13.0) — GET /Bios 노출 249개
- 비교 대상: `GET .../Bios` 의 Attributes 이름(노출 속성). type/allowed_values 비교 불가(한쪽 레지스트리 없음).
- 주의: 모델(T630 타워 vs R630 1U)과 BIOS 버전이 모두 다르므로 펌웨어 차이로만 해석하면 안 됨. T630 캡처에는 레지스트리가 없어 이름만 비교.

| 항목 | 개수 |
|---|---|
| B에만 | 131 |
| A에만 | 62 |
| 공통 | 118 |

### B에만 존재 (131)

`AssetTag`, `ControlledTurbo`, `CpuInterconnectBusLinkPower`, `IntegratedNetwork1`, `IntegratedRaid`, `IscsiDev1Con1Auth`, `IscsiDev1Con1ChapName`, `IscsiDev1Con1ChapSecret`, `IscsiDev1Con1ChapType`, `IscsiDev1Con1DhcpEnDis`, `IscsiDev1Con1EnDis`, `IscsiDev1Con1Gateway`, `IscsiDev1Con1Interface`, `IscsiDev1Con1Ip`, `IscsiDev1Con1IsId`, `IscsiDev1Con1Lun`, `IscsiDev1Con1Mask`, `IscsiDev1Con1Port`, `IscsiDev1Con1Protocol`, `IscsiDev1Con1Retry`, `IscsiDev1Con1RevChapName`, `IscsiDev1Con1RevChapSecret`, `IscsiDev1Con1TargetIp`, `IscsiDev1Con1TargetName`, `IscsiDev1Con1TgtDhcpEnDis`, `IscsiDev1Con1Timeout`, `IscsiDev1Con1VlanEnDis`, `IscsiDev1Con1VlanId`, `IscsiDev1Con1VlanPriority`, `IscsiDev1Con2Auth`, `IscsiDev1Con2ChapName`, `IscsiDev1Con2ChapSecret`, `IscsiDev1Con2ChapType`, `IscsiDev1Con2DhcpEnDis`, `IscsiDev1Con2EnDis`, `IscsiDev1Con2Gateway`, `IscsiDev1Con2Interface`, `IscsiDev1Con2Ip`, `IscsiDev1Con2IsId`, `IscsiDev1Con2Lun`, `IscsiDev1Con2Mask`, `IscsiDev1Con2Port`, `IscsiDev1Con2Protocol`, `IscsiDev1Con2Retry`, `IscsiDev1Con2RevChapName`, `IscsiDev1Con2RevChapSecret`, `IscsiDev1Con2TargetIp`, `IscsiDev1Con2TargetName`, `IscsiDev1Con2TgtDhcpEnDis`, `IscsiDev1Con2Timeout`, `IscsiDev1Con2VlanEnDis`, `IscsiDev1Con2VlanId`, `IscsiDev1Con2VlanPriority`, `IscsiDev1ConOrder`, `IscsiDev1EnDis`, `IscsiInitiatorName`, `LowerMmio`, `OneTimeUefiBootSeqDev`, `PxeDev1EnDis`, `PxeDev1Interface`, `PxeDev1Protocol`, `PxeDev1VlanEnDis`, `PxeDev1VlanId`, `PxeDev1VlanPriority`, `PxeDev2EnDis`, `PxeDev2Interface`, `PxeDev2Protocol`, `PxeDev2VlanEnDis`, `PxeDev2VlanId`, `PxeDev2VlanPriority`, `PxeDev3EnDis`, `PxeDev3Interface`, `PxeDev3Protocol`, `PxeDev3VlanEnDis`, `PxeDev3VlanId`, `PxeDev3VlanPriority`, `PxeDev4EnDis`, `PxeDev4Interface`, `PxeDev4Protocol`, `PxeDev4VlanEnDis`, `PxeDev4VlanId`, `PxeDev4VlanPriority`, `SHA256SetupPassword`, `SHA256SetupPasswordSalt`, `SHA256SystemPassword`, `SHA256SystemPasswordSalt`, `SetBootOrderDis`, `SetBootOrderEn`, `SetBootOrderFqdd1`, `SetBootOrderFqdd10`, `SetBootOrderFqdd11`, `SetBootOrderFqdd12`, `SetBootOrderFqdd13`, `SetBootOrderFqdd14`, `SetBootOrderFqdd15`, `SetBootOrderFqdd16`, `SetBootOrderFqdd2`, `SetBootOrderFqdd3`, `SetBootOrderFqdd4`, `SetBootOrderFqdd5`, `SetBootOrderFqdd6`, `SetBootOrderFqdd7`, `SetBootOrderFqdd8`, `SetBootOrderFqdd9`, `SetLegacyHddOrderFqdd1`, `SetLegacyHddOrderFqdd10`, `SetLegacyHddOrderFqdd11`, `SetLegacyHddOrderFqdd12`, `SetLegacyHddOrderFqdd13`, `SetLegacyHddOrderFqdd14`, `SetLegacyHddOrderFqdd15`, `SetLegacyHddOrderFqdd16`, `SetLegacyHddOrderFqdd2`, `SetLegacyHddOrderFqdd3`, `SetLegacyHddOrderFqdd4`, `SetLegacyHddOrderFqdd5`, `SetLegacyHddOrderFqdd6`, `SetLegacyHddOrderFqdd7`, `SetLegacyHddOrderFqdd8`, `SetLegacyHddOrderFqdd9`, `SetupPassword`, `Slot1Bif`, `Slot2Bif`, `Slot3Bif`, `SnoopHldOff`, `SysPassword`, `TpmCommand`, `TpmFirmware`, `TpmPpiBypassClear`, `TpmPpiBypassProvision`, `TpmSecurity`

### A에만 존재 (62)

`BugChecking`, `CTOMasking`, `CurrentLimit`, `DeviceUnhide`, `Dfx`, `EmbNic1Nic2`, `EmbSataRSTeDebug`, `EmbSataTestMode`, `IdracDebugMode`, `LinkDowntrainReporting`, `MRCSerialDbgOut`, `MemoryFastBootCold`, `MemoryMultiThread`, `MemoryPerBitMargin`, `MemoryRmt`, `OneTimeBootSeqDev`, `OneTimeHddSeqDev`, `PPRErrInjectionTest`, `PostPackageRepair`, `RebootTestCount`, `RebootTestMode`, `RebootTestPoint`, `SataPortACapacity`, `SataPortADriveType`, `SataPortAModel`, `SataPortBCapacity`, `SataPortBDriveType`, `SataPortBModel`, `SataPortCCapacity`, `SataPortCDriveType`, `SataPortCModel`, `SataPortDCapacity`, `SataPortDDriveType`, `SataPortDModel`, `SataPortECapacity`, `SataPortEDriveType`, `SataPortEModel`, `SataPortFCapacity`, `SataPortFDriveType`, `SataPortFModel`, `SataPortGCapacity`, `SataPortGDriveType`, `SataPortGModel`, `SataPortHCapacity`, `SataPortHDriveType`, `SataPortHModel`, `SataPortICapacity`, `SataPortIDriveType`, `SataPortIModel`, `SataPortJCapacity`, `SataPortJDriveType`, `SataPortJModel`, `SccDebugEnabled`, `Slot4`, `Slot5`, `Slot6`, `Slot7`, `Slot8`, `TXEQWA`, `TpmBindingReset`, `UnusedPcieClk`, `WriteDataCrc`

