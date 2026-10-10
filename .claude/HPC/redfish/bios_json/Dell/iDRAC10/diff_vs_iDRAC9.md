# iDRAC10 (17G Intel, R770) vs iDRAC9 (16G Intel, R760xa) BIOS 속성 차이

- 이전(A): PowerEdge R760xa (iDRAC 7.10.50.00, BIOS 2.1.3) — 레지스트리 865개, /Bios 노출 538개
- 이후(B): PowerEdge R770 (iDRAC 1.10.17.00 (iDRAC10), BIOS 1.2.4) — 레지스트리 839개, /Bios 노출 499개
- 비교 방법: 두 캡처의 `GET /redfish/v1/Systems/System.Embedded.1/Bios/BiosRegistry` 원본 JSON 을 스크립트로 비교(요약 도구 미사용). 이름 기준; 이름 변경은 DisplayName 이 같고 이름이 다른 쌍으로 추정 표시.
- 주의: XE9780 실캡처 없음. 같은 Xeon 6 플랫폼의 R770(iDRAC10 1.10.17.00/BIOS 1.2.4)을 대리로 사용.

## 요약

| 항목 | 개수 |
|---|---|
| 추가 (B에만) | 27 |
| 삭제 (A에만) | 53 |
| 공통 | 812 |
| type 변경 | 4 |
| allowed_values 변경(Enumeration) | 68 |
| 정수 범위 변경 | 0 |
| DisplayName 변경 | 4 |
| default 변경 | 비교 불가 (Dell 레지스트리에 DefaultValue 없음) |

## 이름변경 후보 (DisplayName 동일, 이름 상이)

| A 이름 | B 이름 | DisplayName |
|---|---|---|
| `NvmeofHostNqn` | `NvmeofHostCustomNqn` | NVMe-oF Host NQN |
| `NvmeofHostNqn` | `NvmeofHostDellNqn` | NVMe-oF Host NQN |
| `NvmeofHostNqn` | `NvmeofHostUuidNqn` | NVMe-oF Host NQN |
| `Proc4Brand` | `Proc0Brand` | Brand |
| `Proc4Id` | `Proc0Id` | Family-Model-Stepping |
| `Proc4L2Cache` | `Proc0L2Cache` | Level 2 Cache |
| `Proc4L3Cache` | `Proc0L3Cache` | Level 3 Cache |
| `Proc4MaxMemoryCapacity` | `Proc0MaxMemoryCapacity` | Maximum Memory Capacity |
| `Proc4Microcode` | `Proc0Microcode` | Microcode |
| `Proc4NumCores` | `Proc0NumCores` | Number of Cores |
| `Proc2PPIN` | `Proc0PPIN` | Protected Processor Inventory Number |

## 추가/삭제 목록 (MenuPath 별, 전체)

### 추가 (B에만 존재) (27)

- `./IntegratedDevicesRef/SlotDisablementRef` (1): `Slot41`(hidden/미노출)
- `./MemSettingsRef` (2): `CxlSize`(hidden/미노출), `DdrSize`(hidden/미노출)
- `./MemSettingsRef/CxlMemoryRef` (3): `CXLMemoryAttribute`, `CXLMemoryMode`, `CurrentCXLMemoryInterleaveMode`
- `./MiscSettingsRef` (1): `AcpiFpdt`
- `./NetworkSettingsRef` (4): `HostNqnMode`, `NvmeofHostCustomNqn`(hidden/미노출), `NvmeofHostDellNqn`, `NvmeofHostUuidNqn`(hidden/미노출)
- `./ProcSettingsRef` (11): `PrebootDmaProtection`, `Proc0Brand`, `Proc0Id`, `Proc0L2Cache`, `Proc0L3Cache`, `Proc0MaxMemoryCapacity`(hidden/미노출), `Proc0Microcode`, `Proc0NumCores`, `Proc0PPIN`(hidden/미노출), `VirtualNuma`, `VirtualNumaNodes`(hidden/미노출)
- `./ProcSettingsRef/DellControlledTurboRef` (2): `Proc0ControlledTurbo`(hidden/미노출), `Proc0ControlledTurboMinusBin`(hidden/미노출)
- `./SysInformationRef` (2): `SystemFpga2Version`, `SystemFpgaVersion`
- `./SysProfileSettingsRef` (1): `LatencyOptimizedMode`

### 삭제 (A에만 존재) (53)

- `./BootSettingsRef` (17): `HddFailover`, `SetLegacyHddOrderFqdd1`, `SetLegacyHddOrderFqdd10`, `SetLegacyHddOrderFqdd11`, `SetLegacyHddOrderFqdd12`, `SetLegacyHddOrderFqdd13`, `SetLegacyHddOrderFqdd14`, `SetLegacyHddOrderFqdd15`, `SetLegacyHddOrderFqdd16`, `SetLegacyHddOrderFqdd2`, `SetLegacyHddOrderFqdd3`, `SetLegacyHddOrderFqdd4`, `SetLegacyHddOrderFqdd5`, `SetLegacyHddOrderFqdd6`, `SetLegacyHddOrderFqdd7`, `SetLegacyHddOrderFqdd8`, `SetLegacyHddOrderFqdd9`
- `./IntegratedDevicesRef` (2): `IoatEngine`, `MmioLimit`(hidden/미노출)
- `./MemSettingsRef` (4): `DarkMemoryAvailableMem`(hidden/미노출), `HbmMemSpeed`(hidden/미노출), `HbmMemType`(hidden/미노출), `HbmMode`(hidden/미노출)
- `./MiscSettingsRef` (1): `ForceInt10`
- `./NetworkSettingsRef` (1): `NvmeofHostNqn`
- `./ProcSettingsRef` (15): `CpuCrashLogControl`, `OpportunisticSnoopBroadcast`, `Proc1Cores`(hidden/미노출), `Proc2Cores`(hidden/미노출), `Proc2PPIN`(hidden/미노출), `Proc3Cores`(hidden/미노출), `Proc4Brand`(hidden/미노출), `Proc4Cores`(hidden/미노출), `Proc4Id`(hidden/미노출), `Proc4L2Cache`(hidden/미노출), `Proc4L3Cache`(hidden/미노출), `Proc4MaxMemoryCapacity`(hidden/미노출), `Proc4Microcode`(hidden/미노출), `Proc4NumCores`(hidden/미노출), `ProcUncoreFreqRapl`
- `./ProcSettingsRef/DellControlledTurboRef` (2): `Proc4ControlledTurbo`(hidden/미노출), `Proc4ControlledTurboMinusBin`(hidden/미노출)
- `./SerialCommSettingsRef` (1): `RedirAfterBoot`
- `./SysInformationRef` (3): `SystemCpld2Version`(hidden/미노출), `SystemCpldVersion`, `SystemMeVersion`
- `./SysProfileSettingsRef` (2): `PmCRQoS`(hidden/미노출), `PmNVMPerformanceSetting`(hidden/미노출)
- `./SysSecurityRef` (5): `AcPwrRcvry`, `AcPwrRcvryDelay`, `AcPwrRcvryUserDelay`, `EnableTmeBypass`, `TdxDisable1MbCmrExclude`

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
| `BootMode` | Bios | - | Uefi |
| `CpuInterconnectBusSpeed` | 14GTps, 12GTps | 20GTps | MaxDataRate, 20GTps, 16GTps |
| `CpuPaLimit` | Enabled | - | Disabled |
| `CustomUncoreFrequency` | - | 2.2GHz, 2.1GHz | 2.2GHz, 2.1GHz, 2.0GHz, 1.9GHz, 1.8GHz, 1.7GHz, 1.6GHz, 1.5GHz, 1.4GHz, 1.3GHz, 1.2GHz, 1.1GHz, 1.0GHz, 0.9GHz, 0.8GHz |
| `HttpDev1Interface` | NIC.Integrated.1-1-1, NIC.Integrated.1-2-1, NIC.Integrated.1-3-1, NIC.Integrated.1-4-1, NIC.Slot.2-1-1, NIC.Slot.2-2-1 | NIC.Slot.4-1-1, NIC.Slot.4-2-1, NIC.Slot.10-1-1, NIC.Slot.10-2-1, NIC.Slot.10-3-1, NIC.Slot.10-4-1 | NIC.Slot.4-1-1, NIC.Slot.4-2-1, NIC.Slot.10-1-1, NIC.Slot.10-2-1, NIC.Slot.10-3-1, NIC.Slot.10-4-1 |
| `HttpDev2Interface` | NIC.Integrated.1-1-1, NIC.Integrated.1-2-1, NIC.Integrated.1-3-1, NIC.Integrated.1-4-1, NIC.Slot.2-1-1, NIC.Slot.2-2-1 | NIC.Slot.4-1-1, NIC.Slot.4-2-1, NIC.Slot.10-1-1, NIC.Slot.10-2-1, NIC.Slot.10-3-1, NIC.Slot.10-4-1 | NIC.Slot.4-1-1, NIC.Slot.4-2-1, NIC.Slot.10-1-1, NIC.Slot.10-2-1, NIC.Slot.10-3-1, NIC.Slot.10-4-1 |
| `HttpDev3Interface` | NIC.Integrated.1-1-1, NIC.Integrated.1-2-1, NIC.Integrated.1-3-1, NIC.Integrated.1-4-1, NIC.Slot.2-1-1, NIC.Slot.2-2-1 | NIC.Slot.4-1-1, NIC.Slot.4-2-1, NIC.Slot.10-1-1, NIC.Slot.10-2-1, NIC.Slot.10-3-1, NIC.Slot.10-4-1 | NIC.Slot.4-1-1, NIC.Slot.4-2-1, NIC.Slot.10-1-1, NIC.Slot.10-2-1, NIC.Slot.10-3-1, NIC.Slot.10-4-1 |
| `HttpDev4Interface` | NIC.Integrated.1-1-1, NIC.Integrated.1-2-1, NIC.Integrated.1-3-1, NIC.Integrated.1-4-1, NIC.Slot.2-1-1, NIC.Slot.2-2-1 | NIC.Slot.4-1-1, NIC.Slot.4-2-1, NIC.Slot.10-1-1, NIC.Slot.10-2-1, NIC.Slot.10-3-1, NIC.Slot.10-4-1 | NIC.Slot.4-1-1, NIC.Slot.4-2-1, NIC.Slot.10-1-1, NIC.Slot.10-2-1, NIC.Slot.10-3-1, NIC.Slot.10-4-1 |
| `IscsiDev1Con1Interface` | NIC.Integrated.1-1-1, NIC.Integrated.1-2-1, NIC.Integrated.1-3-1, NIC.Integrated.1-4-1, NIC.Slot.2-1-1, NIC.Slot.2-2-1 | NIC.Slot.4-1-1, NIC.Slot.4-2-1, NIC.Slot.10-1-1, NIC.Slot.10-2-1, NIC.Slot.10-3-1, NIC.Slot.10-4-1 | NIC.Slot.4-1-1, NIC.Slot.4-2-1, NIC.Slot.10-1-1, NIC.Slot.10-2-1, NIC.Slot.10-3-1, NIC.Slot.10-4-1 |
| `IscsiDev1Con2Interface` | NIC.Integrated.1-1-1, NIC.Integrated.1-2-1, NIC.Integrated.1-3-1, NIC.Integrated.1-4-1, NIC.Slot.2-1-1, NIC.Slot.2-2-1 | NIC.Slot.4-1-1, NIC.Slot.4-2-1, NIC.Slot.10-1-1, NIC.Slot.10-2-1, NIC.Slot.10-3-1, NIC.Slot.10-4-1 | NIC.Slot.4-1-1, NIC.Slot.4-2-1, NIC.Slot.10-1-1, NIC.Slot.10-2-1, NIC.Slot.10-3-1, NIC.Slot.10-4-1 |
| `MemFrequency` | - | 6000MHz, 5600MHz, 5200MHz, MaxReliability | MaxPerf, 6000MHz, 5600MHz, 5200MHz, MaxReliability |
| `MemPatrolScrub` | Disabled | - | Extended, Standard |
| `NodeInterleave` | Enabled | - | Disabled |
| `NvmeofSubsys1ConInterface` | NIC.Integrated.1-1-1, NIC.Integrated.1-2-1, NIC.Integrated.1-3-1, NIC.Integrated.1-4-1, NIC.Slot.2-1-1, NIC.Slot.2-2-1 | NIC.Slot.4-1-1, NIC.Slot.4-2-1, NIC.Slot.10-1-1, NIC.Slot.10-2-1, NIC.Slot.10-3-1, NIC.Slot.10-4-1 | NIC.Slot.4-1-1, NIC.Slot.4-2-1, NIC.Slot.10-1-1, NIC.Slot.10-2-1, NIC.Slot.10-3-1, NIC.Slot.10-4-1 |
| `NvmeofSubsys2ConInterface` | NIC.Integrated.1-1-1, NIC.Integrated.1-2-1, NIC.Integrated.1-3-1, NIC.Integrated.1-4-1, NIC.Slot.2-1-1, NIC.Slot.2-2-1 | NIC.Slot.4-1-1, NIC.Slot.4-2-1, NIC.Slot.10-1-1, NIC.Slot.10-2-1, NIC.Slot.10-3-1, NIC.Slot.10-4-1 | NIC.Slot.4-1-1, NIC.Slot.4-2-1, NIC.Slot.10-1-1, NIC.Slot.10-2-1, NIC.Slot.10-3-1, NIC.Slot.10-4-1 |
| `NvmeofSubsys3ConInterface` | NIC.Integrated.1-1-1, NIC.Integrated.1-2-1, NIC.Integrated.1-3-1, NIC.Integrated.1-4-1, NIC.Slot.2-1-1, NIC.Slot.2-2-1 | NIC.Slot.4-1-1, NIC.Slot.4-2-1, NIC.Slot.10-1-1, NIC.Slot.10-2-1, NIC.Slot.10-3-1, NIC.Slot.10-4-1 | NIC.Slot.4-1-1, NIC.Slot.4-2-1, NIC.Slot.10-1-1, NIC.Slot.10-2-1, NIC.Slot.10-3-1, NIC.Slot.10-4-1 |
| `NvmeofSubsys4ConInterface` | NIC.Integrated.1-1-1, NIC.Integrated.1-2-1, NIC.Integrated.1-3-1, NIC.Integrated.1-4-1, NIC.Slot.2-1-1, NIC.Slot.2-2-1 | NIC.Slot.4-1-1, NIC.Slot.4-2-1, NIC.Slot.10-1-1, NIC.Slot.10-2-1, NIC.Slot.10-3-1, NIC.Slot.10-4-1 | NIC.Slot.4-1-1, NIC.Slot.4-2-1, NIC.Slot.10-1-1, NIC.Slot.10-2-1, NIC.Slot.10-3-1, NIC.Slot.10-4-1 |
| `OneTimeUefiBootSeqDev` | Disk.Bay.1:Enclosure.Internal.0-1, Optical.iDRACVirtual.1-1 | RAID.SL.3-2, NIC.PxeDevice.1-1 | RAID.SL.3-2, NIC.PxeDevice.1-1 |
| `ProcCores` | 1, 2, 4, 6, 8, 10 | 25, 50, 75 | All, 25, 50, 75 |
| `ProcIssSetting` | IssOp2, IssOp3 | - | IssOp1 |
| `ProcVirtualization` | Disabled | - | Enabled |
| `ProcX2Apic` | Disabled | - | Enabled |
| `ProcessorActivePbf` | - | Enabled | Enabled, Disabled |
| `ProcessorEist` | - | Disabled | Enabled, Disabled |
| `PxeDev10Interface` | NIC.Integrated.1-1-1, NIC.Integrated.1-2-1, NIC.Integrated.1-3-1, NIC.Integrated.1-4-1, NIC.Slot.2-1-1, NIC.Slot.2-2-1 | NIC.Slot.4-1-1, NIC.Slot.4-2-1, NIC.Slot.10-1-1, NIC.Slot.10-2-1, NIC.Slot.10-3-1, NIC.Slot.10-4-1 | NIC.Slot.4-1-1, NIC.Slot.4-2-1, NIC.Slot.10-1-1, NIC.Slot.10-2-1, NIC.Slot.10-3-1, NIC.Slot.10-4-1 |
| `PxeDev11Interface` | NIC.Integrated.1-1-1, NIC.Integrated.1-2-1, NIC.Integrated.1-3-1, NIC.Integrated.1-4-1, NIC.Slot.2-1-1, NIC.Slot.2-2-1 | NIC.Slot.4-1-1, NIC.Slot.4-2-1, NIC.Slot.10-1-1, NIC.Slot.10-2-1, NIC.Slot.10-3-1, NIC.Slot.10-4-1 | NIC.Slot.4-1-1, NIC.Slot.4-2-1, NIC.Slot.10-1-1, NIC.Slot.10-2-1, NIC.Slot.10-3-1, NIC.Slot.10-4-1 |
| `PxeDev12Interface` | NIC.Integrated.1-1-1, NIC.Integrated.1-2-1, NIC.Integrated.1-3-1, NIC.Integrated.1-4-1, NIC.Slot.2-1-1, NIC.Slot.2-2-1 | NIC.Slot.4-1-1, NIC.Slot.4-2-1, NIC.Slot.10-1-1, NIC.Slot.10-2-1, NIC.Slot.10-3-1, NIC.Slot.10-4-1 | NIC.Slot.4-1-1, NIC.Slot.4-2-1, NIC.Slot.10-1-1, NIC.Slot.10-2-1, NIC.Slot.10-3-1, NIC.Slot.10-4-1 |
| `PxeDev13Interface` | NIC.Integrated.1-1-1, NIC.Integrated.1-2-1, NIC.Integrated.1-3-1, NIC.Integrated.1-4-1, NIC.Slot.2-1-1, NIC.Slot.2-2-1 | NIC.Slot.4-1-1, NIC.Slot.4-2-1, NIC.Slot.10-1-1, NIC.Slot.10-2-1, NIC.Slot.10-3-1, NIC.Slot.10-4-1 | NIC.Slot.4-1-1, NIC.Slot.4-2-1, NIC.Slot.10-1-1, NIC.Slot.10-2-1, NIC.Slot.10-3-1, NIC.Slot.10-4-1 |
| `PxeDev14Interface` | NIC.Integrated.1-1-1, NIC.Integrated.1-2-1, NIC.Integrated.1-3-1, NIC.Integrated.1-4-1, NIC.Slot.2-1-1, NIC.Slot.2-2-1 | NIC.Slot.4-1-1, NIC.Slot.4-2-1, NIC.Slot.10-1-1, NIC.Slot.10-2-1, NIC.Slot.10-3-1, NIC.Slot.10-4-1 | NIC.Slot.4-1-1, NIC.Slot.4-2-1, NIC.Slot.10-1-1, NIC.Slot.10-2-1, NIC.Slot.10-3-1, NIC.Slot.10-4-1 |
| `PxeDev15Interface` | NIC.Integrated.1-1-1, NIC.Integrated.1-2-1, NIC.Integrated.1-3-1, NIC.Integrated.1-4-1, NIC.Slot.2-1-1, NIC.Slot.2-2-1 | NIC.Slot.4-1-1, NIC.Slot.4-2-1, NIC.Slot.10-1-1, NIC.Slot.10-2-1, NIC.Slot.10-3-1, NIC.Slot.10-4-1 | NIC.Slot.4-1-1, NIC.Slot.4-2-1, NIC.Slot.10-1-1, NIC.Slot.10-2-1, NIC.Slot.10-3-1, NIC.Slot.10-4-1 |
| `PxeDev16Interface` | NIC.Integrated.1-1-1, NIC.Integrated.1-2-1, NIC.Integrated.1-3-1, NIC.Integrated.1-4-1, NIC.Slot.2-1-1, NIC.Slot.2-2-1 | NIC.Slot.4-1-1, NIC.Slot.4-2-1, NIC.Slot.10-1-1, NIC.Slot.10-2-1, NIC.Slot.10-3-1, NIC.Slot.10-4-1 | NIC.Slot.4-1-1, NIC.Slot.4-2-1, NIC.Slot.10-1-1, NIC.Slot.10-2-1, NIC.Slot.10-3-1, NIC.Slot.10-4-1 |
| `PxeDev1Interface` | NIC.Integrated.1-1-1, NIC.Integrated.1-2-1, NIC.Integrated.1-3-1, NIC.Integrated.1-4-1, NIC.Slot.2-1-1, NIC.Slot.2-2-1 | NIC.Slot.4-1-1, NIC.Slot.4-2-1, NIC.Slot.10-1-1, NIC.Slot.10-2-1, NIC.Slot.10-3-1, NIC.Slot.10-4-1 | NIC.Slot.4-1-1, NIC.Slot.4-2-1, NIC.Slot.10-1-1, NIC.Slot.10-2-1, NIC.Slot.10-3-1, NIC.Slot.10-4-1 |
| `PxeDev2Interface` | NIC.Integrated.1-1-1, NIC.Integrated.1-2-1, NIC.Integrated.1-3-1, NIC.Integrated.1-4-1, NIC.Slot.2-1-1, NIC.Slot.2-2-1 | NIC.Slot.4-1-1, NIC.Slot.4-2-1, NIC.Slot.10-1-1, NIC.Slot.10-2-1, NIC.Slot.10-3-1, NIC.Slot.10-4-1 | NIC.Slot.4-1-1, NIC.Slot.4-2-1, NIC.Slot.10-1-1, NIC.Slot.10-2-1, NIC.Slot.10-3-1, NIC.Slot.10-4-1 |
| `PxeDev3Interface` | NIC.Integrated.1-1-1, NIC.Integrated.1-2-1, NIC.Integrated.1-3-1, NIC.Integrated.1-4-1, NIC.Slot.2-1-1, NIC.Slot.2-2-1 | NIC.Slot.4-1-1, NIC.Slot.4-2-1, NIC.Slot.10-1-1, NIC.Slot.10-2-1, NIC.Slot.10-3-1, NIC.Slot.10-4-1 | NIC.Slot.4-1-1, NIC.Slot.4-2-1, NIC.Slot.10-1-1, NIC.Slot.10-2-1, NIC.Slot.10-3-1, NIC.Slot.10-4-1 |
| `PxeDev4Interface` | NIC.Integrated.1-1-1, NIC.Integrated.1-2-1, NIC.Integrated.1-3-1, NIC.Integrated.1-4-1, NIC.Slot.2-1-1, NIC.Slot.2-2-1 | NIC.Slot.4-1-1, NIC.Slot.4-2-1, NIC.Slot.10-1-1, NIC.Slot.10-2-1, NIC.Slot.10-3-1, NIC.Slot.10-4-1 | NIC.Slot.4-1-1, NIC.Slot.4-2-1, NIC.Slot.10-1-1, NIC.Slot.10-2-1, NIC.Slot.10-3-1, NIC.Slot.10-4-1 |
| `PxeDev5Interface` | NIC.Integrated.1-1-1, NIC.Integrated.1-2-1, NIC.Integrated.1-3-1, NIC.Integrated.1-4-1, NIC.Slot.2-1-1, NIC.Slot.2-2-1 | NIC.Slot.4-1-1, NIC.Slot.4-2-1, NIC.Slot.10-1-1, NIC.Slot.10-2-1, NIC.Slot.10-3-1, NIC.Slot.10-4-1 | NIC.Slot.4-1-1, NIC.Slot.4-2-1, NIC.Slot.10-1-1, NIC.Slot.10-2-1, NIC.Slot.10-3-1, NIC.Slot.10-4-1 |
| `PxeDev6Interface` | NIC.Integrated.1-1-1, NIC.Integrated.1-2-1, NIC.Integrated.1-3-1, NIC.Integrated.1-4-1, NIC.Slot.2-1-1, NIC.Slot.2-2-1 | NIC.Slot.4-1-1, NIC.Slot.4-2-1, NIC.Slot.10-1-1, NIC.Slot.10-2-1, NIC.Slot.10-3-1, NIC.Slot.10-4-1 | NIC.Slot.4-1-1, NIC.Slot.4-2-1, NIC.Slot.10-1-1, NIC.Slot.10-2-1, NIC.Slot.10-3-1, NIC.Slot.10-4-1 |
| `PxeDev7Interface` | NIC.Integrated.1-1-1, NIC.Integrated.1-2-1, NIC.Integrated.1-3-1, NIC.Integrated.1-4-1, NIC.Slot.2-1-1, NIC.Slot.2-2-1 | NIC.Slot.4-1-1, NIC.Slot.4-2-1, NIC.Slot.10-1-1, NIC.Slot.10-2-1, NIC.Slot.10-3-1, NIC.Slot.10-4-1 | NIC.Slot.4-1-1, NIC.Slot.4-2-1, NIC.Slot.10-1-1, NIC.Slot.10-2-1, NIC.Slot.10-3-1, NIC.Slot.10-4-1 |
| `PxeDev8Interface` | NIC.Integrated.1-1-1, NIC.Integrated.1-2-1, NIC.Integrated.1-3-1, NIC.Integrated.1-4-1, NIC.Slot.2-1-1, NIC.Slot.2-2-1 | NIC.Slot.4-1-1, NIC.Slot.4-2-1, NIC.Slot.10-1-1, NIC.Slot.10-2-1, NIC.Slot.10-3-1, NIC.Slot.10-4-1 | NIC.Slot.4-1-1, NIC.Slot.4-2-1, NIC.Slot.10-1-1, NIC.Slot.10-2-1, NIC.Slot.10-3-1, NIC.Slot.10-4-1 |
| `PxeDev9Interface` | NIC.Integrated.1-1-1, NIC.Integrated.1-2-1, NIC.Integrated.1-3-1, NIC.Integrated.1-4-1, NIC.Slot.2-1-1, NIC.Slot.2-2-1 | NIC.Slot.4-1-1, NIC.Slot.4-2-1, NIC.Slot.10-1-1, NIC.Slot.10-2-1, NIC.Slot.10-3-1, NIC.Slot.10-4-1 | NIC.Slot.4-1-1, NIC.Slot.4-2-1, NIC.Slot.10-1-1, NIC.Slot.10-2-1, NIC.Slot.10-3-1, NIC.Slot.10-4-1 |
| `RedundantOsLocation` | IntegratedM2BOSS | - | None |
| `SerialComm` | OnConRedirAuto, OnConRedirCom1, OnConRedirCom2 | OnConRedir | OnNoConRedir, OnConRedir, Off |
| `SerialPortAddress` | Serial1Com1Serial2Com2, Serial1Com2Serial2Com1 | Com1, Com2 | Com1, Com2 |
| `Slot1` | Disabled | BootDriverDisabled | Enabled, BootDriverDisabled |
| `Slot10` | - | Disabled | Enabled, Disabled, BootDriverDisabled |
| `Slot10Bif` | - | x16, x4x4x8, x8x4x4 | x16, x4, x8, x4x4x8, x8x4x4 |
| `Slot1Bif` | x16, x4x4x8, x8x4x4 | - | x4, x8 |
| `Slot3` | - | Disabled | Enabled, Disabled, BootDriverDisabled |
| `Slot31` | Disabled | - | Enabled, BootDriverDisabled |
| `Slot31Bif` | x16, x4x4x8, x8x4x4 | - | x4, x8 |
| `Slot33` | Disabled | - | Enabled, BootDriverDisabled |
| `Slot33Bif` | x16, x4x4x8, x8x4x4 | - | x4, x8 |
| `Slot36` | Disabled | - | Enabled, BootDriverDisabled |
| `Slot36Bif` | x16, x4x4x8, x8x4x4 | - | x4, x8 |
| `Slot38` | Disabled | - | Enabled, BootDriverDisabled |
| `Slot38Bif` | x16, x4x4x8, x8x4x4 | - | x4, x8 |
| `Slot3Bif` | - | x16, x4x4x8, x8x4x4 | x16, x4, x8, x4x4x8, x8x4x4 |
| `Slot4` | - | Disabled | Enabled, Disabled, BootDriverDisabled |
| `Slot4Bif` | - | x16, x4x4x8, x8x4x4 | x16, x4, x8, x4x4x8, x8x4x4 |
| `Slot8` | Disabled | - | Enabled, BootDriverDisabled |
| `Slot8Bif` | x16, x4x4x8, x8x4x4 | - | x4, x8 |
| `Slot9` | - | Disabled | Enabled, Disabled, BootDriverDisabled |
| `Slot9Bif` | - | x16, x4x4x8, x8x4x4 | x16, x4, x8, x4x4x8, x8x4x4 |
| `SmmSecurityMitigation` | Disabled | - | Enabled |
| `SubNumaCluster` | 2-Way | Enabled | Enabled, Disabled |
| `Tpm2Algorithm` | - | SHA384 | SHA1, SHA256, SHA384 |
| `UefiVariableAccess` | - | Read-only | Standard, Controlled, Read-only |
| `WorkloadProfile` | HpcProfile, LowLatencyOptimizedProfile, VtOptimizedProfile, VtPerWattOptimizedProfile, DbOptimizedProfile, DbPerWattOptimizedProfile, SdsOptimizedProfile, SdsPerWattOptimizedProfile, TelcoOptimizedProfile, NfviFpOptimizedTurboProfile, NfviFpEngBalTurboProfile | NfviFpOptimizedTurboProfileSRF, NfviFpEngBalTurboProfileSRF | NotConfigured, NfviFpOptimizedTurboProfileSRF, NfviFpEngBalTurboProfileSRF |

## DisplayName 변경

| 속성 | A | B |
|---|---|---|
| `DimmSlot01` | DIMM Slot A2 | DIMM Slot A2 - Not Installed |
| `DimmSlot17` | DIMM Slot B2 | DIMM Slot B2 - Not Installed |
| `EnableTdx` | Intel Trust Domain Extension (TDX) | Intel Trust Domain Extension(TDX) to Enable |
| `KeySplit` | TME-MT/TDX Key Spilt to non-zero value | TME-MT/TDX Key Split to non-zero value |

