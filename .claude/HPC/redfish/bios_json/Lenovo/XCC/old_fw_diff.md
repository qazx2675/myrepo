# XCC 폴더 내부 차이: Purley(SR630/SR650/SD530) vs Whitley(SR630 V2), 그리고 UEFI 펌웨어 간 차이

조사일 2026-10-10. 같은 XCC(XCC1) 관리망이지만 **Intel 플랫폼 세대/UEFI 빌드에 따라 속성·값이 달라진다.** 근거는 Lenovo Press LP1477(공식 튜닝 가이드, PDF) 본문과 공개 실덤프 2개이다.
(실덤프는 SR630 V2 본인 장비가 아님. 아래 "실덤프" 표기는 모두 프록시이며 unverified 로 취급.)

## 1. 값 표기: Enable/Disable -> Enabled/Disabled  (LP1477 원문, verified)
> "Enable in Skylake vs Enabled in Cascade Lake: In servers with 1st Gen Xeon Scalable (Skylake) processors, UEFI settings have values of "Enable" or "Disable", however in 2nd Gen ... (Cascade Lake) processors, these have been changed to "Enabled" and "Disabled"."

| 세대 | 사용자 모델 | 값 표기 | 근거 |
|---|---|---|---|
| 1st Gen Skylake | SR630/SR650/SD530 (Skylake 구성) | `Enable` / `Disable` | LP1477; XCC REST 가이드 예시(`Processors_CStates: "Disable"`, `DevicesandIOPorts_Device_Slot6: "Enable"`); 운영 설정 sapcc lenovo-sr650.yaml (`Processors_CStates: "Disable"`) |
| 2nd Gen Cascade Lake | SR630/SR650/SD530 (Cascade Lake 구성) | `Enabled` / `Disabled` | LP1477 (실덤프 없음) |
| 3rd Gen Ice Lake | SR630 V2 | `Enabled` / `Disabled` | LP1477 + SR670 V2 실덤프(예 `Processors_HyperThreading: "Enabled"`) |

즉 **SR630/SR650/SD530 는 CPU 세대(Skylake vs Cascade Lake)에 따라 같은 XCC 라도 값 문자열이 다르다.** 스크립트는 `Enable`/`Enabled` 를 모두 받도록 하거나 장비 레지스트리를 먼저 읽을 것. (주의: sapcc 의 SR850 P 설정도 `Disable` 표기였으나 해당 플랫폼 세대는 본 조사에서 확인하지 않음 -> 증거로 쓰지 않음.)

## 2. 3rd Gen(Ice Lake, SR630 V2) 에서 추가/삭제/변경 (LP1477 원문)
| 속성 | 변화 | 근거 |
|---|---|---|
| `Power_PCIePowerBrake` | 3rd Gen 부터 신설 (V1 제품에는 없음). 값 Reactive/Proactive(기본 Proactive)/Disabled | LP1477 "only available on servers with 3rd Gen"; 실덤프 값 `Proactive` |
| `Power_ASPM` | 3rd Gen(V2) 부터 신설, 기본 Disabled | LP1477 "available starting with ThinkSystem V2"; 실덤프 `Disabled` |
| `Processors_CStates` 의 `Autonomous` 선택지 | "3rd Gen 에서 제거" 라고 LP1477 이 기술 | **불일치:** 두 실덤프(2022-12, 2024-08) 모두 `Processors_CStates = "Autonomous"` 로 관측. 가이드 문구와 실덤프 중 어느 쪽이 맞는지 미확정 -> unverified |
| `Processors_CPUPstateControl` 의 `Cooperative with Legacy` | 3rd Gen 에서만 사용 가능. Purley 에서는 선택지 이름이 `Cooperative` (= "Cooperative without Legacy") | LP1477 본문 |
| `Memory_PagePolicy` 기본값 | LP1477: Adaptive, 이후 LP1836(4th/5th Gen): Closed | 두 LP 비교; 실덤프 Adaptive |
| SNC | 38코어 및 12코어 미만 3rd Gen CPU 는 SNC 미지원 | LP1477 |
| Uncore/L2 RFO 관련 `Auto` 의 의미 | 2nd Gen 부터 Auto = 2-hop 메모리 구성에서 Option 5 | LP1477 |

## 3. 두 실덤프 사이(Intel, XCC1 계열, 서로 다른 UEFI/모델) 이름·값 차이
- A = SR670 V2, UEFI U8E122J, `Bios.v1_2_0`, 2024-08-01 스냅샷 (일반 221 키)
- B = 모델 불명(Intel, NVIDIA/Mellanox NIC 장착), `Bios.v1_1_0`, 2022-12-31 스냅샷 (일반 209 키)

| 구분 | 내용 |
|---|---|
| **이름 철자 변경(대소문자)** | A `Memory_MirrorBelow4GB` / B `Memory_Mirrorbelow4GB` -> 같은 설정이 UEFI 빌드에 따라 철자가 다르다. 스크립트에서 이름 하드코딩 금지, 레지스트리 조회 필요 |
| A 에만 존재 | `BootModes_PreventOSChangesToBootOrder`, `DevicesandIOPorts_DMAControlOpt_InFlag`, `DevicesandIOPorts_SRIOV`, `Memory_2xRefreshRate`, `Memory_ADDDCSparing`, `Memory_AdvMemTestOptions`, `Memory_FullMirror`, `Memory_PartialMirror`, `Memory_PartialMirrorRatioInBasisPoints`, `Processors_L2RFOPrefetcher`, `Processors_LLCPrefetch`, `Processors_PRMRRSize` |
| 코어 수에 따라 달라지는 이름 | `Processors_Processors<X>to<Y>coresactive` 계열 (A: 1to16/17to20/21to22/.../33to32, B: 1to10/11to12/.../19to18, `...Q0F06` 접미사 포함). CPU SKU 별로 동적 -> **SR630 V2 의 실제 CPU 로 덤프해야 함** |
| 값 차이 예 | `LegacyBIOS_LegacyBIOS` (A Disabled / B Enabled), `AdvancedRAS_PCIErrorRecovery` (A Enabled / B Disabled), `Processors_IntelVirtualizationTechnology` (A Enabled / B Disabled) 등 -> 장비 설정 차이이지 펌웨어 차이로 단정 불가 |
| 장치 전용 속성 | A: Broadcom NetXtreme 73 + Mellanox 44 + M2NVMe2 RAID kit 10 / B: NVIDIA NIC 164. 이름에 슬롯·포트(·MAC 유래 ID)가 들어가 장비 구성 의존 |

## 4. Redfish 메타 차이 (펌웨어 버전별)
| 항목 | 값 | 근거 |
|---|---|---|
| `Bios.@odata.type` | 문서 `#Bios.v1_0_6.Bios` / 2022 덤프 `v1_1_0` / 2024 덤프 및 bmclib 픽스처 `v1_2_0` | 문서·덤프 |
| `@Redfish.Settings.@odata.type` | 문서 `Settings.v1_2_1` / 2022 `v1_3_0` / bmclib `v1_3_3` / 2024 `v1_3_4` | 문서·덤프 |
| `Bios.Oem.Lenovo` | 2024 덤프에서 `#LenovoBios.v1_0_0` (`IsUefiPowerOnPasswordSet`, `IsUefiAdminPasswordSet`) 확인; 2022 덤프에는 없음 | 덤프 |
| `Memory_MemorySpeed` 값 | 문서 예시 `MaxPerformance` vs 두 실덤프 `MaximumPerformance` (튜닝 가이드의 표시명은 Maximum Performance) -> 문서 예시는 오래된/오타 가능, 실덤프 값을 우선 | 문서·덤프 |
| `Memory_PagePolicy` | LP1477 `Closed, Adaptive` | LP1477 |

## 5. 이 장비들의 최신 펌웨어
확정 못 함(unknown). Lenovo 지원 페이지(로그인/EULA 뒤 패키지)는 받지 않았다. 확인 방법: `GET /redfish/v1/UpdateService/FirmwareInventory/UEFI` 와 `GET /redfish/v1/Managers/1` (`FirmwareVersion`).
