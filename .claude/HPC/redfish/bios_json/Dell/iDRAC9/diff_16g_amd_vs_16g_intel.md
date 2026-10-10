# iDRAC9 16G AMD(R7615, R6615 대리) vs 16G Intel(R760xa) 노출 속성 차이

- A: PowerEdge R760xa (iDRAC 7.10.50.00, BIOS 2.1.3) — GET /Bios 노출 538개
- B: PowerEdge R7615 (iDRAC 7.00.60.00, BIOS 1.6.10) — GET /Bios 노출 487개
- 비교 대상: `GET .../Bios` 의 Attributes 이름(노출 속성). type/allowed_values 비교 불가(한쪽 레지스트리 없음).
- 주의: R6615 실캡처 없음. 동일 플랫폼(Genoa 1S) 형제 모델 R7615 의 GET /Bios 를 사용(unverified proxy).

| 항목 | 개수 |
|---|---|
| B에만 | 38 |
| A에만 | 89 |
| 공통 | 449 |

## B에만 존재 (38)

`AgesaVersion`, `ApbDis`, `BoostFMax`, `CcdCores`, `CcxAsNumaDomain`, `CpuAcpiCstC2Latency`, `CpuMinSevAsid`, `DeterminismSlider`, `DfCState`, `DfPstateFreqOptimizer`, `DfPstateLatencyOptimizer`, `DramRefreshDelay`, `DrtmSkinit`, `EqBypassToHighestRate`, `Hsmp`, `IntegratedRaid`, `IommuSupport`, `L1RegionPrefetcher`, `L1StreamHwPrefetcher`, `L1StridePrefetcher`, `L2StreamHwPrefetcher`, `L2UpDownPrefetcher`, `MmioLimit`, `MpioVersion`, `NumaNodesPerSocket`, `PcieSpeedPmmControl`, `PowerProfileSelect`, `ProcCcds`, `ProcConfigTdp`, `Rmp`, `Slot4`, `Slot4Bif`, `Slot5`, `Slot5Bif`, `Sme`, `SmuVersion`, `Snp`, `TransparentSme`

## A에만 존재 (89)

`AvxIccpPreGrantLicense`, `CpuCrashLogControl`, `CpuInterconnectBusLinkPower`, `CpuInterconnectBusSpeed`, `CpuPaLimit`, `DcuIpPrefetcher`, `DcuStreamerPrefetcher`, `DeadLineLlcAlloc`, `DimmSlot12`, `DimmSlot13`, `DimmSlot14`, `DimmSlot15`, `DimmSlot16`, `DimmSlot17`, `DimmSlot18`, `DimmSlot19`, `DimmSlot20`, `DimmSlot21`, `DimmSlot22`, `DimmSlot23`, `DimmSlot24`, `DimmSlot25`, `DimmSlot26`, `DimmSlot27`, `DimmSlot28`, `DimmSlot29`, `DimmSlot30`, `DimmSlot31`, `DirectoryAtoS`, `DirectoryMode`, `DynamicIss`, `EnableTdx`, `EnableTdxSeamldr`, `EnableTmeBypass`, `EnergyEfficientTurbo`, `EnergyPerformanceBias`, `GlbMemIntegrity`, `IioPcieDataLinkFeatureExchange`, `InBandManageabilityInterface`, `IntelSgx`, `IntelTxt`, `IoatEngine`, `KeySplit`, `LlcPrefetch`, `MemOpMode`, `MemoryEncryption`, `MemoryTraining`, `MonitorMwait`, `NodeInterleave`, `OpportunisticSnoopBroadcast`, `OptimizedPowerMode`, `OptimizerMode`, `PCIRootDeviceUnhide`, `PagingPolicy`, `Proc2Brand`, `Proc2Id`, `Proc2L2Cache`, `Proc2L3Cache`, `Proc2Microcode`, `Proc2NumCores`, `ProcAdjCacheLine`, `ProcAmpPrefetch`, `ProcAvxP1`, `ProcBusSpeed`, `ProcC1E`, `ProcCores`, `ProcHomelessPrefetch`, `ProcHwPrefetcher`, `ProcIssSetting`, `ProcUncoreFreqRapl`, `ProcessorSstCpSetting`, `PwrPerfSwitch`, `Slot31`, `Slot31Bif`, `Slot33`, `Slot33Bif`, `Slot36`, `Slot36Bif`, `Slot38`, `Slot38Bif`, `SnoopHldOff`, `SubNumaCluster`, `SystemMeVersion`, `TdxDisable1MbCmrExclude`, `UmaBasedClusteringStatus`, `UncoreFrequency`, `UpiPrefetch`, `WorkloadConfiguration`, `XptPrefetch`

