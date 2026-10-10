# Dell iDRAC10 (17G) — BIOS 속성 / 사용자 모델 XE9780

- 프로토콜: **json** (Redfish) — **verified**. Dell 이 공개한 iDRAC10 실장비 목업 3종(R770 1.10.17.00, R7725xd 1.20.70.50, XE9712 1.30.07.10)에 모두 `/redfish/v1/Systems/System.Embedded.1/Bios` 가 있다. Dell 스크립트(`idrac_version >= 10` 분기)와 Ansible(`generation >= 17` 분기)도 같은 Bios URI 를 쓴다.
- **이전 결과의 "iDRAC10 Bios URI 미확인(0.99)"을 정정**: Bios / Bios/Settings / Bios/BiosRegistry 는 iDRAC9 와 경로가 같다. 바뀐 것은 SCP 액션 이름과 ApplyTime 이다(아래 표).
- Jev 판정: **생략** (`~/.jev-claude.env` 없음).
- 조사일: 2026-10-10

## 파일

| 파일 | 기준 | 수 | 상태 |
|---|---|---|---|
| `bios_attributes.json` | **R770** (Intel Xeon 6, 17G) iDRAC10 **1.10.17.00**, BIOS **1.2.4** 의 원본 BiosRegistry + /Bios | 레지스트리 839 / 노출 499 | XE9780 기준으로는 **unverified (대리)**. R770 자체로는 verified |
| `bios_attributes_ref_r7725xd_amd.json` | R7725xd (AMD EPYC 9005 Turin) iDRAC10 1.20.70.50, BIOS 1.4.1 | 794 / 노출 499 | 참고용(사용자 모델 아님). AMD 17G 비교 기준 |
| `diff_vs_iDRAC9.md` | R770(iDRAC10) vs R760xa(iDRAC9 7.10.50, 16G) 레지스트리 비교 | +27 / −53, 허용값 변경 68, type 변경 4 | 원본 비교 |

json 구조와 필드는 `../iDRAC9/README.md` 와 같다(`_meta` 첫 줄, default=null, 플래그는 캡처별 맵).

## XE9780 매핑과 한계

- XE9780 = 17G, Intel Xeon 6 (Granite Rapids, 6700P 계열) 2S + HGX B300/B200 8-GPU. XE9780 의 공개 Redfish 캡처는 찾지 못했다.
- 대리로 R770 을 쓴 이유: 같은 17G·같은 Xeon 6 플랫폼(Birch Stream)이고 같은 iDRAC10 이다. 다만 아래 차이가 예상된다(모두 unverified).
  - R770 캡처 CPU 는 **Xeon 6766E(E-core, Sierra Forest)** 라서 하이퍼스레딩이 없다. 그래서 `LogicalProc` 이 레지스트리에는 있지만 /Bios 에는 노출되지 않았다. XE9780(P-core)에서는 노출될 가능성이 높다.
  - GPU 플랫폼이라 슬롯(`SlotN`, `SlotNBif`)·PCIe·MMIO 관련 속성과 허용값이 다를 수 있다.
  - XE9712(GB200) 목업은 `/redfish/v1/Systems/HGX_Baseboard_0/Bios`(Attributes 비어 있음, ResetBios/ChangePassword 액션만 있음)라는 두 번째 System 을 노출한다. XE9780(HGX B300)에도 GPU 베이스보드 System 이 별도로 있을 수 있으니, 호스트 BIOS 는 반드시 `System.Embedded.1` 로 지정할 것.
- iDRAC10 최신 펌웨어: 공개 캡처 중 최신은 1.30.07.10(XE9712)이다. 이전 조사에서 AR 가이드 기준 1.30.60.50 을 적었으나 이번에는 원문을 다시 확인하지 못했다(unverified).

## iDRAC9 → iDRAC10 주요 차이 (원본 비교, 상세는 diff_vs_iDRAC9.md)

- 삭제: 레거시 부팅 관련(`SetLegacyHddOrderFqdd1..16`, `HddFailover`, `ForceInt10`, `RedirAfterBoot`), AC 전원 복구(`AcPwrRcvry`, `AcPwrRcvryDelay`, `AcPwrRcvryUserDelay` — BIOS 레지스트리에서 빠짐), `IoatEngine`, `SystemMeVersion`, `SystemCpldVersion`, `CpuCrashLogControl`, `OpportunisticSnoopBroadcast`, `EnableTmeBypass`, `TdxDisable1MbCmrExclude`, HBM 계열 등.
- 추가: CXL 메모리(`CXLMemoryMode`, `CXLMemoryAttribute`, `CurrentCXLMemoryInterleaveMode`), `VirtualNuma`, `LatencyOptimizedMode`, `PrebootDmaProtection`, `AcpiFpdt`, `HostNqnMode`/`NvmeofHostDellNqn`, `SystemFpgaVersion`/`SystemFpga2Version`.
- 이름 변경: 프로세서 정보가 0부터 번호를 매김(`Proc0Brand`, `Proc0Id` …). `NvmeofHostNqn` 은 `NvmeofHostDellNqn`/`NvmeofHostCustomNqn`/`NvmeofHostUuidNqn` 으로 나뉨.
- type 변경: iSCSI CHAP 비밀값(`IscsiDev1Con{1,2}ChapSecret`, `...RevChapSecret`)이 String → Password.

## BIOS 관련 URI (iDRAC10)

| URI / 액션 | 용도 | 1차 출처 (Dell 공식 원본) | 2차 출처 | 상태 |
|---|---|---|---|---|
| `/redfish/v1/Systems/System.Embedded.1/Bios` (GET) | 현재 속성 | Dell 목업 R770/R7725xd/XE9712 (Bios.v1_2_3) | Ansible `idrac_bios.py` `BIOS_URI`(세대 분기 없음), Dell `GetSetBiosAttributesREDFISH.py` | verified |
| `.../Bios/Settings` (GET/PATCH) | 대기값 | 목업 `@Redfish.Settings.SettingsObject` | Ansible `BIOS_SETTINGS` | verified |
| `.../Bios/BiosRegistry` (GET) | 레지스트리 | 목업 3종(BiosAttributeRegistry.v1_0_3) | Ansible `BIOS_REGISTRY` | verified |
| `/redfish/v1/Registries/BiosAttributeRegistry.v1_0_0` | 레지스트리 파일 위치 | 목업 | — | verified (Dell 원본 3개 장비) |
| `.../Bios/Actions/Bios.ResetBios` | 기본값 복원 | 목업 Actions | Ansible `RESET_BIOS_DEFAULT` | verified |
| `.../Bios/Actions/Bios.ChangePassword` | BIOS 암호 (`PasswordName`: `SetupPassword`/`SysPassword`) | 목업 Actions | `BiosChangePasswordREDFISH.py` | verified |
| `.../Bios/Settings/Actions/Oem/DellManager.ClearPending` | 대기값 삭제 | 목업 Settings.Actions | Ansible `CLEAR_PENDING_URI` | verified |
| `.../Bios/Actions/Oem/DellBios.RunBIOSLiveScanning` | OEM | 목업 | — | verified (Dell 원본 3개 장비) |
| `/redfish/v1/Systems/System.Embedded.1/Oem/Dell/DellBIOSService/Actions/DellBIOSService.DeviceRecovery` | BIOS 복구 | 목업 | `BiosDeviceRecoveryREDFISH.py` | verified |
| `/redfish/v1/Managers/iDRAC.Embedded.1/Oem/Dell/Jobs` | Job 조회·생성 | 목업(Settings.Oem.Dell.Jobs 링크) | Ansible `IDRAC_JOBS_URI` | verified |
| **SCP** `/redfish/v1/Managers/iDRAC.Embedded.1/Actions/Oem/OemManager.ExportSystemConfiguration` | BIOS 포함 설정 내보내기 | 목업 3종 Manager.Actions target | Ansible `EXPORT_URI_17`(`generation >= 17`), Dell `ExportSystemConfigurationLocalREDFISH.py`(`idrac_version >= 10`) | verified |
| **SCP** `.../OemManager.ImportSystemConfiguration`, `.../OemManager.ImportSystemConfigurationPreview` | BIOS 일괄 변경 / 사전검증 | 목업 | Ansible `IMPORT_URI_17`/`IMPORT_PREVIEW_17`, Dell `ImportSystemConfigurationLocalREDFISH.py` | verified |

- **iDRAC9 와 다른 점 ①**: SCP 액션 target 이 `EID_674_Manager.*` → `OemManager.*` 로 바뀌었다. Ansible 주석은 "newer SCP APIs, mandatory since 17G" 라고 적었다. iDRAC9 용 스크립트를 그대로 쓰면 SCP 가 실패한다.
- **다른 점 ②**: `Bios.@Redfish.Settings.SupportedApplyTimes` 가 iDRAC10 목업 3종 모두 `["OnReset"]` 뿐이다. iDRAC9 의 `AtMaintenanceWindowStart`/`InMaintenanceWindowOnReset` 이 광고되지 않는다. 유지보수 창 예약 PATCH 는 실장비에서 확인해야 한다(unverified).
- Manager `Model` 은 "17G Monolithic" 이다(세대 판별에 쓸 수 있음).
- developer.dell.com 의 iDRAC10 Redfish API 문서는 SPA 라 본문을 받지 못했다(WebFetch 시 목차만 나옴). Dell KB 000178045 는 iDRAC10 Redfish 문서 위치(developer.dell.com)만 안내한다. 그래서 1차 출처를 Dell 이 GitHub 에 공개한 실장비 목업 원본으로 삼았다.

## 시도 내역

1. 1차: back/ 결과 검토(Bios URI unverified, 속성은 AR 가이드 문서 상위집합 1431).
2. 2차: Dell KB 000178045(WebFetch) → iDRAC10 문서는 developer.dell.com 로 안내만 함. developer.dell.com(WebFetch) → 목차만. `dl.dell.com/topicspdf/idrac10-*_api-guide_en-us.pdf` 추정 URL 2개 → 404.
3. 3차(직접 조사): `dell/iDRAC-Redfish-Scripting` 의 iDRAC10 목업 zip 3개를 풀어 원본 JSON 확인 + Dell 스크립트의 iDRAC10 분기 코드 확인 + `dell/dellemc-openmanage-ansible-modules` 의 17G 분기 코드 확인 → 결론.
4. 실패: XE9780 자체 BiosRegistry, XE9780 HGX 베이스보드 System 존재 여부.

## 실장비 확인

```
GET /redfish/v1/Systems                                   # System.Embedded.1 외 GPU 베이스보드 System 이 있는지
GET /redfish/v1/Systems/System.Embedded.1/Bios/BiosRegistry
GET /redfish/v1/Systems/System.Embedded.1/Bios
GET /redfish/v1/Managers/iDRAC.Embedded.1                 # Actions.Oem 의 SCP target 이름 확인
```

## 출처

- Dell iDRAC10 목업: https://github.com/dell/iDRAC-Redfish-Scripting/tree/master/iDRAC%20Redfish%20Mockup%20Clients (R770_iDRAC10_1_10_17_00, R7725_iDRAC10_1_20_70_50, XE9712_iDRAC10_1_30_07_10)
- Dell 스크립트: https://github.com/dell/iDRAC-Redfish-Scripting/tree/master/Redfish%20Python (ExportSystemConfigurationLocalREDFISH.py, ImportSystemConfigurationLocalREDFISH.py, GetSetBiosAttributesREDFISH.py, BiosChangePasswordREDFISH.py)
- Ansible: https://github.com/dell/dellemc-openmanage-ansible-modules (plugins/module_utils/idrac_redfish.py `EXPORT_URI_17`, plugins/modules/idrac_bios.py)
- Dell KB 000178045: https://www.dell.com/support/kbdoc/en-us/000178045
