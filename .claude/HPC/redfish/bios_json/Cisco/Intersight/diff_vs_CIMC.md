# Intersight vs CIMC: BIOS 토큰 비교 (스크립트 생성)

- 비교 기준: 토큰명을 소문자·영숫자만 남겨 정규화(UCSM/CIMC 는 `vp` 접두 제거) 후 일치 여부. 허용값도 소문자·영숫자만 남겨 비교(예: `Force L0s` = `forcel0s`). UCSM 의 `platform-recommended` 값은 UCSM 전용이므로 비교에서 제외했다. 약어·재명명된 토큰은 "에만 있는 토큰"에 섞여 같은 기능의 개명이 불일치로 잡힐 수 있다.
- CIMC 토큰 495개(정규화명 기준), Intersight 토큰 473개, 공통 464개, CIMC 에만 31개, Intersight 에만 9개. 허용값 차이가 있는 공통 토큰 23개.
- 근거: 각 폴더 bios_attributes.json (SDK 메타데이터). 상태: unverified(단일 SDK 출처 기반 이름 비교).

## 공통 토큰 중 허용값(enum) 차이 (23개)

| CIMC 이름 | Intersight 이름 | CIMC 에만 있는 값 | Intersight 에만 있는 값 |
|---|---|---|---|
| vpBaudRate | BaudRate | 1152k, 192k, 384k, 576k, 96k | - |
| vpCbsCmnApbdisDfPstateRs | CbsCmnApbdisDfPstateRs | platformdefault | - |
| vpCbsCmnCpuSevAsidSpaceLimit | CbsCmnCpuSevAsidSpaceLimit | platformdefault | - |
| vpCbsDbgCpuSnpMemSizeCover | CbsDbgCpuSnpMemSizeCover | platformdefault | - |
| vpCoreMultiProcessing | CoreMultiProcessing | - | 65, 66, 67, 68, 69, 70, 71, 72, 73, 74, 75, 76, 77, 78, 79, 80, 81, 82, 83, 84, 85, 86 |
| vpMemorySizeLimit | MemorySizeLimit | platformdefault | - |
| vpMmiohBase | MmiohBase | - | 30t, 60t, auto |
| vpMmiohSize | MmiohSize | - | 32g, auto |
| vpPackageCStateLimit | PackageCstateLimit | c0state, c1state, c3state, c6state | - |
| vpPartialMirrorPercent | PartialMirrorPercent | platformdefault | - |
| vpPartialMirrorValue1 | PartialMirrorValue1 | platformdefault | - |
| vpPartialMirrorValue2 | PartialMirrorValue2 | platformdefault | - |
| vpPartialMirrorValue3 | PartialMirrorValue3 | platformdefault | - |
| vpPartialMirrorValue4 | PartialMirrorValue4 | platformdefault | - |
| vpPatrolScrubDuration | PatrolScrubDuration | platformdefault | - |
| vpPchPciePllSsc | PchPciePllSsc | platformdefault | - |
| vpSgxEpoch0 | SgxEpoch0 | platformdefault | - |
| vpSgxEpoch1 | SgxEpoch1 | platformdefault | - |
| vpSgxLePubKeyHash0 | SgxLePubKeyHash0 | platformdefault | - |
| vpSgxLePubKeyHash1 | SgxLePubKeyHash1 | platformdefault | - |
| vpSgxLePubKeyHash2 | SgxLePubKeyHash2 | platformdefault | - |
| vpSgxLePubKeyHash3 | SgxLePubKeyHash3 | platformdefault | - |
| vpStreamerPrefetch | StreamerPrefetch | - | auto |

## CIMC 에만 있는 토큰 (31)

`delay`, `delayType`, `vpIOESlot1State`, `vpIOESlot2State`, `vpResumeOnACPowerLoss`, `vpSlotIOEMezz1LinkSpeed`, `vpSlotIOEMezz1State`, `vpSlotIOENVMe1LinkSpeed`, `vpSlotIOENVMe1State`, `vpSlotIOENVMe2LinkSpeed`, `vpSlotIOENVMe2State`, `vpSlotIOESlot1LinkSpeed`, `vpSlotIOESlot2LinkSpeed`, `vpSlotMLinkSpeed`, `vpSlotSBLom1State`, `vpSlotSBMezz1LinkSpeed`, `vpSlotSBMezz1State`, `vpSlotSBMezz2LinkSpeed`, `vpSlotSBMezz2State`, `vpSlotSBNVMe1LinkSpeed`, `vpSlotSBNVMe1State`, `vpSlotSBNVMe2LinkSpeed`, `vpSlotSBNVMe2State`, `vpSlotSIOC1LinkSpeed`, `vpSlotSIOC1State`, `vpSlotSIOC2LinkSpeed`, `vpSlotSIOC2State`, `vpSlotSIOCNVMe1LinkSpeed`, `vpSlotSIOCNvme1State`, `vpSlotSIOCNVMe2LinkSpeed`, `vpSlotSIOCNvme2State`

## Intersight 에만 있는 토큰 (9)

`CbsCmnCpuFrequencyControl`, `GpuDirectCpu1`, `GpuDirectCpu2`, `GpuDirectCpu3`, `GpuDirectCpu4`, `Model`, `PolicyType`, `SpeculativeLockEnable`, `UfsDisableIo`
