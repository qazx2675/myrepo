# iDRAC9 16G Intel vs 15G BIOS 속성 차이

- 이전(A): PowerEdge R750 (iDRAC 7.10.30.00, BIOS 1.13.2) — 레지스트리 687개, /Bios 노출 454개
- 이후(B): PowerEdge R760xa (iDRAC 7.10.50.00, BIOS 2.1.3) — 레지스트리 865개, /Bios 노출 538개
- 비교 방법: 두 캡처의 `GET /redfish/v1/Systems/System.Embedded.1/Bios/BiosRegistry` 원본 JSON 을 스크립트로 비교(요약 도구 미사용). 이름 기준; 이름 변경은 DisplayName 이 같고 이름이 다른 쌍으로 추정 표시.
- 주의: 15G=R750(7.10.30.00/BIOS 1.13.2), 16G=R760xa(7.10.50.00/BIOS 2.1.3) 실캡처. 둘 다 사용자 보유 모델.

## 요약

| 항목 | 개수 |
|---|---|
| 추가 (B에만) | 190 |
| 삭제 (A에만) | 12 |
| 공통 | 675 |
| type 변경 | 0 |
| allowed_values 변경(Enumeration) | 53 |
| 정수 범위 변경 | 0 |
| DisplayName 변경 | 43 |
| default 변경 | 비교 불가 (Dell 레지스트리에 DefaultValue 없음) |

## 이름변경 후보 (DisplayName 동일, 이름 상이)

| A 이름 | B 이름 | DisplayName |
|---|---|---|
| `ProcessorRaplPrioritization` | `ProcessorSstCpSetting` | Intel SST-CP |

## 추가/삭제 목록 (MenuPath 별, 전체)

### 추가 (B에만 존재) (190)

- `./BootSettingsRef` (2): `SetBootOrderDis2`, `SetBootOrderEn2`
- `./IntegratedDevicesRef` (5): `EmbNic5Nic6Nic7Nic8`(hidden/미노출), `EmbNic9Nic10Nic11Nic12`(hidden/미노출), `IioPcieDataLinkFeatureExchange`, `PcieBusAllocation`(hidden/미노출), `PcieResizBar`(hidden/미노출)
- `./IntegratedDevicesRef/SlotBifurcationRef` (14): `Slot21Bif`(hidden/미노출), `Slot22Bif`(hidden/미노출), `Slot23Bif`(hidden/미노출), `Slot24Bif`(hidden/미노출), `Slot25Bif`(hidden/미노출), `Slot26Bif`(hidden/미노출), `Slot27Bif`(hidden/미노출), `Slot28Bif`(hidden/미노출), `Slot35Bif`(hidden/미노출), `Slot36Bif`, `Slot37Bif`(hidden/미노출), `Slot38Bif`, `Slot39Bif`(hidden/미노출), `Slot40Bif`(hidden/미노출)
- `./IntegratedDevicesRef/SlotDisablementRef` (14): `Slot21`(hidden/미노출), `Slot22`(hidden/미노출), `Slot23`(hidden/미노출), `Slot24`(hidden/미노출), `Slot25`(hidden/미노출), `Slot26`(hidden/미노출), `Slot27`(hidden/미노출), `Slot28`(hidden/미노출), `Slot35`(hidden/미노출), `Slot36`, `Slot37`(hidden/미노출), `Slot38`, `Slot39`(hidden/미노출), `Slot40`(hidden/미노출)
- `./MemSettingsRef` (6): `CxlMemSize`(hidden/미노출), `DarkMemoryAvailableMem`(hidden/미노출), `HbmMemSpeed`(hidden/미노출), `HbmMemType`(hidden/미노출), `HbmMode`(hidden/미노출), `PagingPolicy`
- `./MemSettingsRef/MemoryMapOutRef` (32): `DimmSlot32`(hidden/미노출), `DimmSlot33`(hidden/미노출), `DimmSlot34`(hidden/미노출), `DimmSlot35`(hidden/미노출), `DimmSlot36`(hidden/미노출), `DimmSlot37`(hidden/미노출), `DimmSlot38`(hidden/미노출), `DimmSlot39`(hidden/미노출), `DimmSlot40`(hidden/미노출), `DimmSlot41`(hidden/미노출), `DimmSlot42`(hidden/미노출), `DimmSlot43`(hidden/미노출), `DimmSlot44`(hidden/미노출), `DimmSlot45`(hidden/미노출), `DimmSlot46`(hidden/미노출), `DimmSlot47`(hidden/미노출), `DimmSlot48`(hidden/미노출), `DimmSlot49`(hidden/미노출), `DimmSlot50`(hidden/미노출), `DimmSlot51`(hidden/미노출), `DimmSlot52`(hidden/미노출), `DimmSlot53`(hidden/미노출), `DimmSlot54`(hidden/미노출), `DimmSlot55`(hidden/미노출), `DimmSlot56`(hidden/미노출), `DimmSlot57`(hidden/미노출), `DimmSlot58`(hidden/미노출), `DimmSlot59`(hidden/미노출), `DimmSlot60`(hidden/미노출), `DimmSlot61`(hidden/미노출), `DimmSlot62`(hidden/미노출), `DimmSlot63`(hidden/미노출)
- `./MiscSettingsRef` (2): `Daylight`, `TimeZone`
- `./NetworkSettingsRef` (4): `NvmeofEnDis`, `NvmeofHostId`, `NvmeofHostNqn`, `NvmeofHostSecurityPath`
- `./NetworkSettingsRef/NvmeofSubSystemSettingsRef` (4): `NvmeofSubsys1EnDis`, `NvmeofSubsys2EnDis`, `NvmeofSubsys3EnDis`, `NvmeofSubsys4EnDis`
- `./NetworkSettingsRef/NvmeofSubSystemSettingsRef/NvmeofSubSystem1SettingsRef` (21): `NvmeofSubsys1Address`, `NvmeofSubsys1Auth`, `NvmeofSubsys1ConInterface`, `NvmeofSubsys1ConProtocol`, `NvmeofSubsys1ControllerId`, `NvmeofSubsys1HostDhcp`, `NvmeofSubsys1HostGateway`, `NvmeofSubsys1HostIP`, `NvmeofSubsys1HostMask`, `NvmeofSubsys1InfoDhcp`, `NvmeofSubsys1NameSpaceId`, `NvmeofSubsys1Nqn`, `NvmeofSubsys1Port`, `NvmeofSubsys1Retry`, `NvmeofSubsys1Security`, `NvmeofSubsys1SecurityKeyPath`, `NvmeofSubsys1Timeout`, `NvmeofSubsys1TransType`, `NvmeofSubsys1VlanEnDis`, `NvmeofSubsys1VlanId`, `NvmeofSubsys1VlanPriority`
- `./NetworkSettingsRef/NvmeofSubSystemSettingsRef/NvmeofSubSystem2SettingsRef` (21): `NvmeofSubsys2Address`, `NvmeofSubsys2Auth`, `NvmeofSubsys2ConInterface`, `NvmeofSubsys2ConProtocol`, `NvmeofSubsys2ControllerId`, `NvmeofSubsys2HostDhcp`, `NvmeofSubsys2HostGateway`, `NvmeofSubsys2HostIP`, `NvmeofSubsys2HostMask`, `NvmeofSubsys2InfoDhcp`, `NvmeofSubsys2NameSpaceId`, `NvmeofSubsys2Nqn`, `NvmeofSubsys2Port`, `NvmeofSubsys2Retry`, `NvmeofSubsys2Security`, `NvmeofSubsys2SecurityKeyPath`, `NvmeofSubsys2Timeout`, `NvmeofSubsys2TransType`, `NvmeofSubsys2VlanEnDis`, `NvmeofSubsys2VlanId`, `NvmeofSubsys2VlanPriority`
- `./NetworkSettingsRef/NvmeofSubSystemSettingsRef/NvmeofSubSystem3SettingsRef` (21): `NvmeofSubsys3Address`, `NvmeofSubsys3Auth`, `NvmeofSubsys3ConInterface`, `NvmeofSubsys3ConProtocol`, `NvmeofSubsys3ControllerId`, `NvmeofSubsys3HostDhcp`, `NvmeofSubsys3HostGateway`, `NvmeofSubsys3HostIP`, `NvmeofSubsys3HostMask`, `NvmeofSubsys3InfoDhcp`, `NvmeofSubsys3NameSpaceId`, `NvmeofSubsys3Nqn`, `NvmeofSubsys3Port`, `NvmeofSubsys3Retry`, `NvmeofSubsys3Security`, `NvmeofSubsys3SecurityKeyPath`, `NvmeofSubsys3Timeout`, `NvmeofSubsys3TransType`, `NvmeofSubsys3VlanEnDis`, `NvmeofSubsys3VlanId`, `NvmeofSubsys3VlanPriority`
- `./NetworkSettingsRef/NvmeofSubSystemSettingsRef/NvmeofSubSystem4SettingsRef` (21): `NvmeofSubsys4Address`, `NvmeofSubsys4Auth`, `NvmeofSubsys4ConInterface`, `NvmeofSubsys4ConProtocol`, `NvmeofSubsys4ControllerId`, `NvmeofSubsys4HostDhcp`, `NvmeofSubsys4HostGateway`, `NvmeofSubsys4HostIP`, `NvmeofSubsys4HostMask`, `NvmeofSubsys4InfoDhcp`, `NvmeofSubsys4NameSpaceId`, `NvmeofSubsys4Nqn`, `NvmeofSubsys4Port`, `NvmeofSubsys4Retry`, `NvmeofSubsys4Security`, `NvmeofSubsys4SecurityKeyPath`, `NvmeofSubsys4Timeout`, `NvmeofSubsys4TransType`, `NvmeofSubsys4VlanEnDis`, `NvmeofSubsys4VlanId`, `NvmeofSubsys4VlanPriority`
- `./ProcSettingsRef` (11): `CpuAcpiCstC2Latency`(hidden/미노출), `CpuCrashLogControl`, `FastGoConfig`(hidden/미노출), `L2RfoPrefetch`(hidden/미노출), `OpportunisticSnoopBroadcast`, `ProcAmpPrefetch`, `ProcHomelessPrefetch`, `ProcUncoreFreqRapl`, `ProcessorSstCpSetting`, `UmaBasedClusteringStatus`, `Upi3LinkCtrl`(hidden/미노출)
- `./SysProfileSettingsRef` (4): `CustomUncoreFrequency`(hidden/미노출), `OptimizedPowerMode`, `ProcessorApsRocketing`(hidden/미노출), `ProcessorScalability`(hidden/미노출)
- `./SysSecurityRef` (8): `EnableTdx`, `EnableTdxSeamldr`, `EnableTmeBypass`, `GlbMemIntegrity`, `InFieldScan`(hidden/미노출), `KeySplit`, `TdxDisable1MbCmrExclude`, `UefiCaCertScope`

### 삭제 (A에만 존재) (12)

- `./IntegratedDevicesRef` (1): `PcieBusCustomization`(hidden/미노출)
- `./MemSettingsRef` (2): `SnoopMode`(hidden/미노출), `SysMemVolt`
- `./ProcSettingsRef` (4): `ImcInterleave`(hidden/미노출), `ProcAts`(hidden/미노출), `ProcessorRaplPrioritization`, `RtidSetting`(hidden/미노출)
- `./SysProfileSettingsRef` (5): `CollaborativeCpuPerfCtrl`(hidden/미노출), `Cppc`(hidden/미노출), `EnablePkgcCriteria`(hidden/미노출), `PkgCLatNeg`(hidden/미노출), `WriteDataCrc`(hidden/미노출)

## allowed_values 변경 (Enumeration, 전체)

| 속성 | A 에만 있는 값 | B 에만 있는 값 | B 전체 값 |
|---|---|---|---|
| `CpuInterconnectBusSpeed` | 11GTps, 10GTps, 9GTps | 16GTps, 14GTps, 12GTps | MaxDataRate, 16GTps, 14GTps, 12GTps |
| `HttpDev1Interface` | NIC.Embedded.1-1-1, NIC.Embedded.2-1-1, NIC.Slot.2-1, NIC.Slot.5-1 | NIC.Integrated.1-1-1, NIC.Integrated.1-2-1, NIC.Integrated.1-3-1, NIC.Integrated.1-4-1, NIC.Slot.2-1-1, NIC.Slot.2-2-1 | NIC.Integrated.1-1-1, NIC.Integrated.1-2-1, NIC.Integrated.1-3-1, NIC.Integrated.1-4-1, NIC.Slot.2-1-1, NIC.Slot.2-2-1 |
| `HttpDev2Interface` | NIC.Embedded.1-1-1, NIC.Embedded.2-1-1, NIC.Slot.2-1, NIC.Slot.5-1 | NIC.Integrated.1-1-1, NIC.Integrated.1-2-1, NIC.Integrated.1-3-1, NIC.Integrated.1-4-1, NIC.Slot.2-1-1, NIC.Slot.2-2-1 | NIC.Integrated.1-1-1, NIC.Integrated.1-2-1, NIC.Integrated.1-3-1, NIC.Integrated.1-4-1, NIC.Slot.2-1-1, NIC.Slot.2-2-1 |
| `HttpDev3Interface` | NIC.Embedded.1-1-1, NIC.Embedded.2-1-1, NIC.Slot.2-1, NIC.Slot.5-1 | NIC.Integrated.1-1-1, NIC.Integrated.1-2-1, NIC.Integrated.1-3-1, NIC.Integrated.1-4-1, NIC.Slot.2-1-1, NIC.Slot.2-2-1 | NIC.Integrated.1-1-1, NIC.Integrated.1-2-1, NIC.Integrated.1-3-1, NIC.Integrated.1-4-1, NIC.Slot.2-1-1, NIC.Slot.2-2-1 |
| `HttpDev4Interface` | NIC.Embedded.1-1-1, NIC.Embedded.2-1-1, NIC.Slot.2-1, NIC.Slot.5-1 | NIC.Integrated.1-1-1, NIC.Integrated.1-2-1, NIC.Integrated.1-3-1, NIC.Integrated.1-4-1, NIC.Slot.2-1-1, NIC.Slot.2-2-1 | NIC.Integrated.1-1-1, NIC.Integrated.1-2-1, NIC.Integrated.1-3-1, NIC.Integrated.1-4-1, NIC.Slot.2-1-1, NIC.Slot.2-2-1 |
| `IscsiDev1Con1Interface` | NIC.Embedded.1-1-1, NIC.Embedded.2-1-1, NIC.Slot.2-1, NIC.Slot.5-1 | NIC.Integrated.1-1-1, NIC.Integrated.1-2-1, NIC.Integrated.1-3-1, NIC.Integrated.1-4-1, NIC.Slot.2-1-1, NIC.Slot.2-2-1 | NIC.Integrated.1-1-1, NIC.Integrated.1-2-1, NIC.Integrated.1-3-1, NIC.Integrated.1-4-1, NIC.Slot.2-1-1, NIC.Slot.2-2-1 |
| `IscsiDev1Con2Interface` | NIC.Embedded.1-1-1, NIC.Embedded.2-1-1, NIC.Slot.2-1, NIC.Slot.5-1 | NIC.Integrated.1-1-1, NIC.Integrated.1-2-1, NIC.Integrated.1-3-1, NIC.Integrated.1-4-1, NIC.Slot.2-1-1, NIC.Slot.2-2-1 | NIC.Integrated.1-1-1, NIC.Integrated.1-2-1, NIC.Integrated.1-3-1, NIC.Integrated.1-4-1, NIC.Slot.2-1-1, NIC.Slot.2-2-1 |
| `OneTimeUefiBootSeqDev` | NIC.HttpDevice.1-1, Unknown.Unknown.2-1 | Disk.Bay.1:Enclosure.Internal.0-1, Optical.iDRACVirtual.1-1 | Disk.Bay.1:Enclosure.Internal.0-1, Optical.iDRACVirtual.1-1 |
| `PmCRQoS` | NvmQosMode0, NvmQosMode1, NvmQosMode2 | NvmQosDisable, NvmQosProfile1 | NvmQosDisable, NvmQosProfile1 |
| `PmNVMPerformanceSetting` | PmLatencyOptimized | - | PmBWOptimized, PmBalancedProfile |
| `PrmrrSize` | 32G | 256M, 512M, 1G | 256M, 512M, 1G, 2G, 4G, 8G, 16G |
| `ProcCores` | 12, 14, 16 | - | All, 1, 2, 4, 6, 8, 10 |
| `ProcIssSetting` | - | IssOp2, IssOp3 | IssOp1, IssOp2, IssOp3 |
| `PxeDev10Interface` | NIC.Embedded.1-1-1, NIC.Embedded.2-1-1, NIC.Slot.2-1, NIC.Slot.5-1 | NIC.Integrated.1-1-1, NIC.Integrated.1-2-1, NIC.Integrated.1-3-1, NIC.Integrated.1-4-1, NIC.Slot.2-1-1, NIC.Slot.2-2-1 | NIC.Integrated.1-1-1, NIC.Integrated.1-2-1, NIC.Integrated.1-3-1, NIC.Integrated.1-4-1, NIC.Slot.2-1-1, NIC.Slot.2-2-1 |
| `PxeDev11Interface` | NIC.Embedded.1-1-1, NIC.Embedded.2-1-1, NIC.Slot.2-1, NIC.Slot.5-1 | NIC.Integrated.1-1-1, NIC.Integrated.1-2-1, NIC.Integrated.1-3-1, NIC.Integrated.1-4-1, NIC.Slot.2-1-1, NIC.Slot.2-2-1 | NIC.Integrated.1-1-1, NIC.Integrated.1-2-1, NIC.Integrated.1-3-1, NIC.Integrated.1-4-1, NIC.Slot.2-1-1, NIC.Slot.2-2-1 |
| `PxeDev12Interface` | NIC.Embedded.1-1-1, NIC.Embedded.2-1-1, NIC.Slot.2-1, NIC.Slot.5-1 | NIC.Integrated.1-1-1, NIC.Integrated.1-2-1, NIC.Integrated.1-3-1, NIC.Integrated.1-4-1, NIC.Slot.2-1-1, NIC.Slot.2-2-1 | NIC.Integrated.1-1-1, NIC.Integrated.1-2-1, NIC.Integrated.1-3-1, NIC.Integrated.1-4-1, NIC.Slot.2-1-1, NIC.Slot.2-2-1 |
| `PxeDev13Interface` | NIC.Embedded.1-1-1, NIC.Embedded.2-1-1, NIC.Slot.2-1, NIC.Slot.5-1 | NIC.Integrated.1-1-1, NIC.Integrated.1-2-1, NIC.Integrated.1-3-1, NIC.Integrated.1-4-1, NIC.Slot.2-1-1, NIC.Slot.2-2-1 | NIC.Integrated.1-1-1, NIC.Integrated.1-2-1, NIC.Integrated.1-3-1, NIC.Integrated.1-4-1, NIC.Slot.2-1-1, NIC.Slot.2-2-1 |
| `PxeDev14Interface` | NIC.Embedded.1-1-1, NIC.Embedded.2-1-1, NIC.Slot.2-1, NIC.Slot.5-1 | NIC.Integrated.1-1-1, NIC.Integrated.1-2-1, NIC.Integrated.1-3-1, NIC.Integrated.1-4-1, NIC.Slot.2-1-1, NIC.Slot.2-2-1 | NIC.Integrated.1-1-1, NIC.Integrated.1-2-1, NIC.Integrated.1-3-1, NIC.Integrated.1-4-1, NIC.Slot.2-1-1, NIC.Slot.2-2-1 |
| `PxeDev15Interface` | NIC.Embedded.1-1-1, NIC.Embedded.2-1-1, NIC.Slot.2-1, NIC.Slot.5-1 | NIC.Integrated.1-1-1, NIC.Integrated.1-2-1, NIC.Integrated.1-3-1, NIC.Integrated.1-4-1, NIC.Slot.2-1-1, NIC.Slot.2-2-1 | NIC.Integrated.1-1-1, NIC.Integrated.1-2-1, NIC.Integrated.1-3-1, NIC.Integrated.1-4-1, NIC.Slot.2-1-1, NIC.Slot.2-2-1 |
| `PxeDev16Interface` | NIC.Embedded.1-1-1, NIC.Embedded.2-1-1, NIC.Slot.2-1, NIC.Slot.5-1 | NIC.Integrated.1-1-1, NIC.Integrated.1-2-1, NIC.Integrated.1-3-1, NIC.Integrated.1-4-1, NIC.Slot.2-1-1, NIC.Slot.2-2-1 | NIC.Integrated.1-1-1, NIC.Integrated.1-2-1, NIC.Integrated.1-3-1, NIC.Integrated.1-4-1, NIC.Slot.2-1-1, NIC.Slot.2-2-1 |
| `PxeDev1Interface` | NIC.Embedded.1-1-1, NIC.Embedded.2-1-1, NIC.Slot.2-1, NIC.Slot.5-1 | NIC.Integrated.1-1-1, NIC.Integrated.1-2-1, NIC.Integrated.1-3-1, NIC.Integrated.1-4-1, NIC.Slot.2-1-1, NIC.Slot.2-2-1 | NIC.Integrated.1-1-1, NIC.Integrated.1-2-1, NIC.Integrated.1-3-1, NIC.Integrated.1-4-1, NIC.Slot.2-1-1, NIC.Slot.2-2-1 |
| `PxeDev2Interface` | NIC.Embedded.1-1-1, NIC.Embedded.2-1-1, NIC.Slot.2-1, NIC.Slot.5-1 | NIC.Integrated.1-1-1, NIC.Integrated.1-2-1, NIC.Integrated.1-3-1, NIC.Integrated.1-4-1, NIC.Slot.2-1-1, NIC.Slot.2-2-1 | NIC.Integrated.1-1-1, NIC.Integrated.1-2-1, NIC.Integrated.1-3-1, NIC.Integrated.1-4-1, NIC.Slot.2-1-1, NIC.Slot.2-2-1 |
| `PxeDev3Interface` | NIC.Embedded.1-1-1, NIC.Embedded.2-1-1, NIC.Slot.2-1, NIC.Slot.5-1 | NIC.Integrated.1-1-1, NIC.Integrated.1-2-1, NIC.Integrated.1-3-1, NIC.Integrated.1-4-1, NIC.Slot.2-1-1, NIC.Slot.2-2-1 | NIC.Integrated.1-1-1, NIC.Integrated.1-2-1, NIC.Integrated.1-3-1, NIC.Integrated.1-4-1, NIC.Slot.2-1-1, NIC.Slot.2-2-1 |
| `PxeDev4Interface` | NIC.Embedded.1-1-1, NIC.Embedded.2-1-1, NIC.Slot.2-1, NIC.Slot.5-1 | NIC.Integrated.1-1-1, NIC.Integrated.1-2-1, NIC.Integrated.1-3-1, NIC.Integrated.1-4-1, NIC.Slot.2-1-1, NIC.Slot.2-2-1 | NIC.Integrated.1-1-1, NIC.Integrated.1-2-1, NIC.Integrated.1-3-1, NIC.Integrated.1-4-1, NIC.Slot.2-1-1, NIC.Slot.2-2-1 |
| `PxeDev5Interface` | NIC.Embedded.1-1-1, NIC.Embedded.2-1-1, NIC.Slot.2-1, NIC.Slot.5-1 | NIC.Integrated.1-1-1, NIC.Integrated.1-2-1, NIC.Integrated.1-3-1, NIC.Integrated.1-4-1, NIC.Slot.2-1-1, NIC.Slot.2-2-1 | NIC.Integrated.1-1-1, NIC.Integrated.1-2-1, NIC.Integrated.1-3-1, NIC.Integrated.1-4-1, NIC.Slot.2-1-1, NIC.Slot.2-2-1 |
| `PxeDev6Interface` | NIC.Embedded.1-1-1, NIC.Embedded.2-1-1, NIC.Slot.2-1, NIC.Slot.5-1 | NIC.Integrated.1-1-1, NIC.Integrated.1-2-1, NIC.Integrated.1-3-1, NIC.Integrated.1-4-1, NIC.Slot.2-1-1, NIC.Slot.2-2-1 | NIC.Integrated.1-1-1, NIC.Integrated.1-2-1, NIC.Integrated.1-3-1, NIC.Integrated.1-4-1, NIC.Slot.2-1-1, NIC.Slot.2-2-1 |
| `PxeDev7Interface` | NIC.Embedded.1-1-1, NIC.Embedded.2-1-1, NIC.Slot.2-1, NIC.Slot.5-1 | NIC.Integrated.1-1-1, NIC.Integrated.1-2-1, NIC.Integrated.1-3-1, NIC.Integrated.1-4-1, NIC.Slot.2-1-1, NIC.Slot.2-2-1 | NIC.Integrated.1-1-1, NIC.Integrated.1-2-1, NIC.Integrated.1-3-1, NIC.Integrated.1-4-1, NIC.Slot.2-1-1, NIC.Slot.2-2-1 |
| `PxeDev8Interface` | NIC.Embedded.1-1-1, NIC.Embedded.2-1-1, NIC.Slot.2-1, NIC.Slot.5-1 | NIC.Integrated.1-1-1, NIC.Integrated.1-2-1, NIC.Integrated.1-3-1, NIC.Integrated.1-4-1, NIC.Slot.2-1-1, NIC.Slot.2-2-1 | NIC.Integrated.1-1-1, NIC.Integrated.1-2-1, NIC.Integrated.1-3-1, NIC.Integrated.1-4-1, NIC.Slot.2-1-1, NIC.Slot.2-2-1 |
| `PxeDev9Interface` | NIC.Embedded.1-1-1, NIC.Embedded.2-1-1, NIC.Slot.2-1, NIC.Slot.5-1 | NIC.Integrated.1-1-1, NIC.Integrated.1-2-1, NIC.Integrated.1-3-1, NIC.Integrated.1-4-1, NIC.Slot.2-1-1, NIC.Slot.2-2-1 | NIC.Integrated.1-1-1, NIC.Integrated.1-2-1, NIC.Integrated.1-3-1, NIC.Integrated.1-4-1, NIC.Slot.2-1-1, NIC.Slot.2-2-1 |
| `RedundantOsLocation` | - | IntegratedM2BOSS | None, IntegratedM2BOSS |
| `SecureBootPolicy` | - | LinuxBoot, VmwareBoot, MicrosoftBoot | Standard, Custom, LinuxBoot, VmwareBoot, MicrosoftBoot |
| `SerialComm` | OnConRedir | OnConRedirAuto, OnConRedirCom1, OnConRedirCom2 | OnNoConRedir, OnConRedirAuto, OnConRedirCom1, OnConRedirCom2, Off |
| `SerialPortAddress` | Com1, Com2 | Serial1Com1Serial2Com2, Serial1Com2Serial2Com1 | Serial1Com1Serial2Com2, Serial1Com2Serial2Com1 |
| `Slot1` | BootDriverDisabled | Disabled | Enabled, Disabled |
| `Slot1Bif` | - | x16, x4x4x8, x8x4x4 | x16, x4, x8, x4x4x8, x8x4x4 |
| `Slot3` | Disabled | - | Enabled, BootDriverDisabled |
| `Slot31` | - | Disabled | Enabled, Disabled, BootDriverDisabled |
| `Slot31Bif` | - | x16, x4x4x8, x8x4x4 | x16, x4, x8, x4x4x8, x8x4x4 |
| `Slot33` | - | Disabled | Enabled, Disabled, BootDriverDisabled |
| `Slot33Bif` | - | x16, x4x4x8, x8x4x4 | x16, x4, x8, x4x4x8, x8x4x4 |
| `Slot3Bif` | x16, x4x4x8, x8x4x4 | - | x4, x8 |
| `Slot4` | Disabled | - | Enabled, BootDriverDisabled |
| `Slot5` | Disabled | - | Enabled, BootDriverDisabled |
| `Slot6` | Disabled | - | Enabled, BootDriverDisabled |
| `Slot6Bif` | x16, x4x4x8, x8x4x4 | - | x4, x8 |
| `Slot8` | - | Disabled | Enabled, Disabled, BootDriverDisabled |
| `Slot8Bif` | - | x16, x4x4x8, x8x4x4 | x16, x4, x8, x4x4x8, x8x4x4 |
| `SysProfile` | - | PerfWorkStationOptimized | PerfPerWattOptimizedDapc, PerfPerWattOptimizedOs, PerfOptimized, PerfWorkStationOptimized, Custom |
| `Tpm2Algorithm` | SHA384 | - | SHA1, SHA256 |
| `UncoreFrequency` | OptimizedUFS | - | DynamicUFS, MaxUFS |
| `VmdMode` | Enabled | - | Disabled |
| `WorkloadProfile` | - | NfviFpOptimizedTurboProfile, NfviFpEngBalTurboProfile | NotConfigured, HpcProfile, LowLatencyOptimizedProfile, VtOptimizedProfile, VtPerWattOptimizedProfile, DbOptimizedProfile, DbPerWattOptimizedProfile, SdsOptimizedProfile, SdsPerWattOptimizedProfile, TelcoOptimizedProfile, NfviFpOptimizedTurboProfile, NfviFpEngBalTurboProfile |
| `WorkloadProfileHelper` | - | NfviFpOptimizedTurboProfile, NfviFpEngBalTurboProfile | NotConfigured, HpcProfile, LowLatencyOptimizedProfile, VtOptimizedProfile, VtPerWattOptimizedProfile, DbOptimizedProfile, DbPerWattOptimizedProfile, SdsOptimizedProfile, SdsPerWattOptimizedProfile, TelcoOptimizedProfile, NfviFpOptimizedTurboProfile, NfviFpEngBalTurboProfile |

## DisplayName 변경

| 속성 | A | B |
|---|---|---|
| `DimmSlot00` | Dimm Slot A1 | DIMM Slot A1 |
| `DimmSlot01` | Dimm Slot A2 | DIMM Slot A2 |
| `DimmSlot02` | Dimm Slot A3 | DIMM Slot A3 - Not Installed |
| `DimmSlot03` | Dimm Slot A4 | DIMM Slot A4 - Not Installed |
| `DimmSlot04` | Dimm Slot A5 - Not Installed | DIMM Slot A5 - Not Installed |
| `DimmSlot05` | Dimm Slot A6 - Not Installed | DIMM Slot A6 - Not Installed |
| `DimmSlot06` | Dimm Slot A7 - Not Installed | DIMM Slot A7 - Not Installed |
| `DimmSlot07` | Dimm Slot A8 - Not Installed | DIMM Slot A8 - Not Installed |
| `DimmSlot08` | Dimm Slot A9 - Not Installed | DIMM Slot A9 - Not Installed |
| `DimmSlot09` | Dimm Slot A10 - Not Installed | DIMM Slot A10 - Not Installed |
| `DimmSlot10` | Dimm Slot A11 - Not Installed | DIMM Slot A11 - Not Installed |
| `DimmSlot11` | Dimm Slot A12 - Not Installed | DIMM Slot A12 - Not Installed |
| `DimmSlot12` | Dimm Slot A13 - Not Installed | DIMM Slot A13 - Not Installed |
| `DimmSlot13` | Dimm Slot A14 - Not Installed | DIMM Slot A14 - Not Installed |
| `DimmSlot14` | Dimm Slot A15 - Not Installed | DIMM Slot A15 - Not Installed |
| `DimmSlot15` | Dimm Slot A16 - Not Installed | DIMM Slot A16 - Not Installed |
| `DimmSlot16` | Dimm Slot B1 | DIMM Slot B1 |
| `DimmSlot17` | Dimm Slot B2 | DIMM Slot B2 |
| `DimmSlot18` | Dimm Slot B3 | DIMM Slot B3 - Not Installed |
| `DimmSlot19` | Dimm Slot B4 | DIMM Slot B4 - Not Installed |
| `DimmSlot20` | Dimm Slot B5 - Not Installed | DIMM Slot B5 - Not Installed |
| `DimmSlot21` | Dimm Slot B6 - Not Installed | DIMM Slot B6 - Not Installed |
| `DimmSlot22` | Dimm Slot B7 - Not Installed | DIMM Slot B7 - Not Installed |
| `DimmSlot23` | Dimm Slot B8 - Not Installed | DIMM Slot B8 - Not Installed |
| `DimmSlot24` | Dimm Slot B9 - Not Installed | DIMM Slot B9 - Not Installed |
| `DimmSlot25` | Dimm Slot B10 - Not Installed | DIMM Slot B10 - Not Installed |
| `DimmSlot26` | Dimm Slot B11 - Not Installed | DIMM Slot B11 - Not Installed |
| `DimmSlot27` | Dimm Slot B12 - Not Installed | DIMM Slot B12 - Not Installed |
| `DimmSlot28` | Dimm Slot B13 - Not Installed | DIMM Slot B13 - Not Installed |
| `DimmSlot29` | Dimm Slot B14 - Not Installed | DIMM Slot B14 - Not Installed |
| `DimmSlot30` | Dimm Slot B15 - Not Installed | DIMM Slot B15 - Not Installed |
| `DimmSlot31` | Dimm Slot B16 - Not Installed | DIMM Slot B16 - Not Installed |
| `HttpDev1v6AutoConfig` | Autoconfiguration | Auto Configuration |
| `HttpDev1v6DnsDhcpEnDis` | DNS info via DHCP | DNS info via DHCPv6 |
| `HttpDev2v6AutoConfig` | Autoconfiguration | Auto Configuration |
| `HttpDev2v6DnsDhcpEnDis` | DNS info via DHCP | DNS info via DHCPv6 |
| `HttpDev3v6AutoConfig` | Autoconfiguration | Auto Configuration |
| `HttpDev3v6DnsDhcpEnDis` | DNS info via DHCP | DNS info via DHCPv6 |
| `HttpDev4v6AutoConfig` | Autoconfiguration | Auto Configuration |
| `HttpDev4v6DnsDhcpEnDis` | DNS info via DHCP | DNS info via DHCPv6 |
| `PackageCStates` | Package C States | Package C-States |
| `ProcCStates` | C States | C-States |
| `SysPrepClean` | Clean all Sysprep order and variables | Clean all SysPrep variables and order |

