# Dell iDRAC9 — BIOS 속성 (14G / 15G / 16G)

- 프로토콜: **json** (Redfish). iDRAC9 는 모든 펌웨어(3.00.00.00~7.xx)에서 Redfish BIOS 리소스를 제공한다 (verified: iDRAC9 4.20.20.20 Redfish API Guide + 3.15/5.10/6.00/7.00/7.10/7.20 실캡처).
- Jev 판정: **생략** (이 환경에 `~/.jev-claude.env` 없음). verified 는 "공식 문서/공식 원본 + 독립 2차 출처" 기준.
- 조사일: 2026-10-10. 이전 결과(`bios_json/back/Dell/*`)는 PDF 요약 fetch 기반(값 잘림, 14G 합집합 965개, 15G R650 대리, 16G 문서 상위집합 1431개)이었다. 이번에는 **실장비 Redfish 원본 JSON**(BiosRegistry + Bios)을 직접 받아 스크립트로 변환했다. 전사(요약) 단계가 없다.

## 핵심 사실 (먼저 읽을 것)

1. **BIOS 속성 목록은 iDRAC 펌웨어가 아니라 시스템 BIOS 버전·플랫폼이 결정한다.** iDRAC 는 BIOS 가 넘겨준 레지스트리를 `Bios/BiosRegistry` 로 노출할 뿐이다. 같은 R750 에서 iDRAC 6.00.30.00/BIOS 1.8.2 → 7.10.30.00/BIOS 1.13.2 로 올라갔을 때 레지스트리 차이는 추가 5개뿐이었고, 세대(14G→15G→16G) 간 차이는 수백 개였다. 그래서 파일을 iDRAC 버전이 아니라 **세대별**로 나눴다.
2. **레지스트리 ≠ 노출 속성.** 레지스트리(`Bios/BiosRegistry`)에는 Hidden 항목과 그 모델에 없는 슬롯/DIMM 항목까지 들어 있다. 실제 `GET /Bios` 에 나오는 속성은 그보다 훨씬 적다 (예: R760xa 865 → 538). json 의 `exposed_in_bios_resource` 로 구분했다.
3. **ReadOnly/Hidden/Immutable 은 상태값이다.** Dell 레지스트리의 이 플래그는 Dependencies 를 현재 값에 적용한 결과다. 예를 들어 `SysProfile` 이 Custom 이 아니면 성능 속성 다수가 ReadOnly=true 가 된다. 그래서 json 에서는 이 플래그들을 캡처별 맵(`{"R750@7.10.30": true, ...}`)으로 남겼다. R750@7.10.30 캡처는 ReadOnly 598/687 로 유난히 많다(System Lockdown 등 상태 영향으로 추정되며 원인은 미확인).
4. **Dell 레지스트리에는 DefaultValue 가 없다.** 모든 캡처에서 `DefaultValue` 키가 없었고 `CurrentValue` 는 null 이었다. 그래서 default 는 null 로 두었다. 14G 이후 HelpText 에도 "Default: X" 문구가 없다(13G 에는 일부 있음 → iDRAC8 json 에 반영). 공장 기본값을 알려면 실장비에서 `POST .../Bios/Actions/Bios.ResetBios` 후 재부팅한 다음 `GET /Bios` 를 받아야 한다.
5. Enumeration 중 장치 FQDD 를 값으로 갖는 속성(`HttpDev1Interface`, `PxeDev1Interface`, `IscsiDev1Con1Interface`, BootSeq 계열 등)의 allowed_values 는 **장착 NIC/디스크에 따라 달라진다**. 캡처 장비의 구성일 뿐이다.

## 파일

| 파일 | 세대 | 기준 캡처(원본) | 속성 수 | 상태 |
|---|---|---|---|---|
| `bios_attributes_14g.json` | 14G Intel (Skylake/Cascade Lake) | R740xd, iDRAC **7.00.00.182**, BIOS **2.24.0** | 레지스트리 641 / 노출 338 | verified (R740xd 실캡처). R640/R740 은 대리 |
| `bios_attributes_15g.json` | 15G Intel (Ice Lake) | **R750** iDRAC 7.10.30.00 / BIOS 1.13.2 (+R650 7.10.30.00/1.12.1, R750 6.00.30.00/1.8.2 합집합) | 687 (R750) / 노출 454 | verified (사용자 모델 R750 실캡처) |
| `bios_attributes_16g_intel.json` | 16G Intel (Sapphire/Emerald Rapids) | **R760xa** iDRAC 7.10.50.00 / BIOS 2.1.3 (+R760 동일 버전, R660xs 7.20.10.05/BIOS 2.4.4 합집합) | 합집합 877 (R760xa 865 / 노출 538) | verified (사용자 모델 R760xa 실캡처) |
| `bios_attributes_16g_amd.json` | 16G AMD (EPYC 9004 Genoa) | R7615 iDRAC 7.00.60.00 / BIOS 1.6.10 의 `GET /Bios` 만 | 노출 487 (이름+샘플값) | **partial**: 이름은 verified, type/allowed_values 는 unavailable(null) |
| `diff_vs_iDRAC8.md` | 14G vs 13G | R740xd 7.00 vs R630 2.81.81.81 | +400 / −8 | 원본 비교 |
| `diff_15g_vs_14g.md` | 15G vs 14G | R750 vs R740xd | +144 / −98 | 원본 비교 |
| `diff_16g_vs_15g.md` | 16G vs 15G | R760xa vs R750 | +190 / −12 | 원본 비교 |
| `diff_16g_amd_vs_16g_intel.md` | 16G AMD vs Intel | R7615 vs R760xa (노출 이름) | AMD에만 89 / Intel에만 38 | 이름만 비교 |
| `old_fw_diff.md` | iDRAC9 3.x/5.x/6.x/7.x | 동일 세대 다른 펌웨어 캡처 비교 | — | 원본 비교 (3.x→7.x 는 모델도 다름) |

json 공통 구조: `{"_meta": {...vendor, mgmt_version, generation, source, firmware_version, retrieved, status, captures[]...}, "attributes": [ {...}, ... ]}` (`_meta` 가 첫 줄).
속성 필드: `name, display_name, type(Enumeration/String/Integer/Password), allowed_values(ValueName), allowed_value_display, lower_bound/upper_bound/scalar_increment, min_length/max_length/value_expression, default(null), read_only/write_only/hidden/immutable/reset_required(캡처별 맵), menu_path, registry_in, exposed_in_bios_resource, sample_value(캡처 장비의 현재값 — 기본값 아님), variants(캡처 간 type/값 차이)`.

## 사용자 보유 모델 매핑

| 모델 | 세대/CPU | 사용할 파일 | 근거 | 상태 |
|---|---|---|---|---|
| R640 | 14G Intel | `bios_attributes_14g.json` | R740xd 와 같은 14G Intel 2S BIOS 계열(슬롯·드라이브 속성은 다를 수 있음) | unverified (대리) |
| R740 | 14G Intel | 〃 | R740/R740xd 는 같은 BIOS 계열. R740 실캡처는 iDRAC 3.15/BIOS 0.4.1 만 있음(old_fw_diff 참고) | unverified (대리, 근접) |
| R840 | 14G Intel 4S | 〃 | 4S 플랫폼이라 UPI/소켓 관련 속성이 다를 가능성 있음 | unverified |
| C6420 | 14G Intel 멀티노드 | 〃 | 별도 BIOS 라인 | unverified |
| DSS8440 | 14G Intel GPU | 〃 | 별도 BIOS 라인, 공개 캡처 없음 | unverified |
| **R750** | 15G Intel | `bios_attributes_15g.json` | **실캡처**(iDRAC 7.10.30.00, BIOS 1.13.2) | **verified** |
| R750xa | 15G Intel | 〃 | R750 계열 | unverified (대리) |
| R750xs | 15G Intel | 〃 | xs 계열은 별도 BIOS(R650xs 와 공유로 알려짐). 캡처 없음 | unverified |
| **R760XA** | 16G Intel | `bios_attributes_16g_intel.json` | **실캡처**(iDRAC 7.10.50.00, BIOS 2.1.3) | **verified** |
| XE9680 | 16G Intel GPU | 〃 | 캡처 없음 | unverified (대리) |
| R660 | 16G Intel | 〃 | R760 과 같은 BIOS 계열로 보임. R660xs 캡처(xs 계열)만 있음 | unverified (대리) |
| R860 | 16G Intel 4S | 〃 | 4S, 캡처 없음 | unverified |
| C6620 | 16G Intel 멀티노드 | 〃 | 캡처 없음 | unverified |
| R6615 | 16G **AMD** Genoa 1S | `bios_attributes_16g_amd.json` | 형제 모델 R7615(같은 Genoa 1S 플랫폼. Dell DSA 에서 R6615/R7615 가 같은 BIOS 수정 버전으로 함께 나옴)의 `GET /Bios` | unverified (대리), type 정보 없음 |

**R6615 (AMD) 의 실제 차이**: Intel 16G 에 없는 AMD 전용 속성이 노출된다. `NumaNodesPerSocket`, `CcxAsNumaDomain`, `ProcCcds`, `CcdCores`, `DeterminismSlider`, `PowerProfileSelect`, `BoostFMax`, `DfCState`, `DfPstateFreqOptimizer`, `DfPstateLatencyOptimizer`, `ApbDis`, `IommuSupport`, `L1/L2 Prefetcher` 계열 5종, `Hsmp`, `DramRefreshDelay`, `EqBypassToHighestRate`, `PcieSpeedPmmControl`, `ProcConfigTdp`, `AgesaVersion`, `SmuVersion`, `MpioVersion` 등이다. 반대로 Intel 의 `SubNumaCluster`, UPI 계열(`UpiPrefetch`, `CpuInterconnectBusSpeed/LinkPower`), `EnableTdx`, `IntelSgx`, `IntelTxt`, `DcuIpPrefetcher` 등은 없다. R7615/R6615 는 1소켓이라 `Proc2*`, `DimmSlot12` 이상도 없다(소켓 수 차이이며 AMD 여부와는 별개). 전체 목록은 `diff_16g_amd_vs_16g_intel.md` 에 있다.

## 최신 펌웨어

- 캡처 기준: 14G 7.00.00.182 / 15G 7.10.30.00 / 16G 7.10.50.00·7.20.10.05.
- 14G 는 iDRAC9 7.00.00.x 가 마지막 라인이다(14G 는 7.10 이상을 받지 않음). 근거는 R740xd 캡처가 7.00.00.182 인 점과 Dell Attribute Registry 가이드 체계(이전 조사)뿐이다 → unverified.
- 15G/16G 의 최신 iDRAC9 는 7.20/7.30 계열이다. 이전 조사에서 AR 가이드 최신 항목으로 7.30.30.54 를 확인했으나, 이번에 독립 출처는 확보하지 못했다 → unverified. 7.30 캡처는 공개된 것이 없다. 다만 위 1번 사실 때문에 같은 BIOS 버전이면 7.30 에서도 속성은 같다고 본다. BIOS 버전이 다르면 old_fw_diff 처럼 일부 추가·허용값 변경이 생긴다.

## BIOS 관련 URI (iDRAC9)

| URI / 액션 | 메서드 | 용도 | 1차 출처 | 2차 출처 | 상태 |
|---|---|---|---|---|---|
| `/redfish/v1/Systems/System.Embedded.1/Bios` | GET | 현재 BIOS 속성 | iDRAC9 Redfish API Guide 4.20.20.20 (`/Systems/<ComputerSystem-Id>/Bios`) | Dell 공식 목업(R650 7.10.30, R760xa/R760 7.10.50, R660xs 7.20.10.05) + dellemc.openmanage `idrac_bios.py` `BIOS_URI` + 제3자 실캡처(R750, R740xd) | verified |
| `/redfish/v1/Systems/System.Embedded.1/Bios/Settings` | GET, PATCH | 대기값 설정. PATCH body `{"Attributes":{...}, "@Redfish.SettingsApplyTime":{"ApplyTime":"OnReset"}}` | API Guide 4.20 + 목업 `@Redfish.Settings.SettingsObject` | `GetSetBiosAttributesREDFISH.py`, Ansible `BIOS_SETTINGS` | verified |
| `/redfish/v1/Systems/System.Embedded.1/Bios/BiosRegistry` | GET | 속성 레지스트리(Attributes/Menus/Dependencies) | API Guide 4.20 | 목업 6종 + Ansible `BIOS_REGISTRY` + `GetSetBiosAttributesREDFISH.py` | verified (이전 unverified 를 정정) |
| `/redfish/v1/Registries/BiosAttributeRegistry.v1_0_0` | GET | 레지스트리 파일 위치. `Location[].Uri` = 위 BiosRegistry | 목업 전체 | 제3자 캡처 전체 | verified |
| `/redfish/v1/Registries` | GET | 레지스트리 컬렉션 | 목업 | 제3자 캡처(GroundZero R740xd `redfish_v1_Registries.json`) | verified |
| `.../Bios/Actions/Bios.ResetBios` | POST `{}` | BIOS 기본값 복원(재부팅 후 적용) | API Guide 4.20 (`/Systems/<System-Id>/Bios/Actions/Bios.ResetBios`) | 목업 Actions + Ansible `RESET_BIOS_DEFAULT` + `BiosResetToDefaultsREDFISH.py` | verified |
| `.../Bios/Actions/Bios.ChangePassword` | POST `{"PasswordName":"SetupPassword"\|"SysPassword","OldPassword":"<OLD>","NewPassword":"<NEW_PASSWORD>"}` → 이후 `POST /Managers/iDRAC.Embedded.1/Oem/Dell/Jobs` `{"TargetSettingsURI":".../Bios/Settings"}` | BIOS 암호 변경 | API Guide 4.20 | 목업 + `BiosChangePasswordREDFISH.py` | verified |
| `.../Bios/Settings/Actions/Oem/DellManager.ClearPending` | POST `{}` | 대기값 삭제 | API Guide 4.20 (`/Bios/Settings/Actions/Oem/DellManager.ClearPending`) | 목업 Settings.Actions + Ansible `CLEAR_PENDING_URI` | verified |
| `.../Bios/Actions/Oem/DellBios.RunBIOSLiveScanning` | POST | BIOS 라이브 스캔(OEM) | API Guide 4.20 | 목업(5.10 이상) | verified (3.15 캡처에는 없음) |
| `/redfish/v1/Systems/System.Embedded.1/Oem/Dell/DellBIOSService/Actions/DellBIOSService.DeviceRecovery` | POST `{"Device":"BIOS"}` | BIOS 복구 | 목업 7.10/7.20 (4.20 가이드에는 `/redfish/v1/Dell/Systems/<Id>/DellBIOSService` 로 표기) | `BiosDeviceRecoveryREDFISH.py` | verified (경로는 펌웨어 따라 다름 → 실장비에서 링크로 확인) |
| `/redfish/v1/Managers/iDRAC.Embedded.1/Oem/Dell/Jobs` | POST/GET | BIOS 설정 Job 생성·조회 | Dell 스크립트 | Ansible `IDRAC_JOBS_URI` | verified |
| **SCP** `/redfish/v1/Managers/iDRAC.Embedded.1/Actions/Oem/EID_674_Manager.ExportSystemConfiguration` | POST | 서버 설정 프로파일 내보내기(BIOS 포함, `ShareParameters.Target:["BIOS"]`) | API Guide 4.20 | 모든 iDRAC9 목업 Manager.Actions + Ansible `EXPORT_URI` | verified |
| **SCP** `.../EID_674_Manager.ImportSystemConfiguration` / `...ImportSystemConfigurationPreview` | POST | SCP 가져오기(BIOS 일괄 변경, 재부팅 Job 자동 생성) / 사전검증 | API Guide 4.20 | 목업 + Ansible `IMPORT_URI`/`IMPORT_PREVIEW` | verified |

- 지원 ApplyTime (`Bios.@Redfish.Settings.SupportedApplyTimes`): 5.10 이상 `OnReset, AtMaintenanceWindowStart, InMaintenanceWindowOnReset`. 3.15.15.15 캡처에는 SupportedApplyTimes 가 없다(Settings.v1_0_2).
- 다중 System 주의: GPU/DPU 구성(R760xa 목업)은 `/redfish/v1/Systems` 에 `DPU.Embedded.1_NIC.Slot.2` 가 함께 있고, 그 System 에도 별도 `/Bios` 가 있다. 호스트 BIOS 는 반드시 `System.Embedded.1` 로 지정할 것.
- SCP 로 BIOS 변경: Import 시 iDRAC 가 BIOS 설정 Job 을 만들고 재부팅해 적용한다(`ShutdownType` Graceful/Forced/NoReboot). Redfish `Bios/Settings` PATCH 와 같은 BIOS 속성 이름(`BIOS.Setup.1-1` 컴포넌트의 Attribute Name)을 쓴다. 여러 장비에 같은 BIOS 프로파일을 적용할 때 쓰는 수단이다.

## 시도 내역 (총 3회 + 직접 조사)

1. 1차(문서): back/ 결과 재검토 → 값 잘림·합집합 문제 확인. Dell AR 가이드 HTML 은 요약 fetch 만 가능해서 폐기.
2. 2차(공개 코드·샘플, 범위 확대):
   - `github.com/dell/iDRAC-Redfish-Scripting` 클론. `iDRAC Redfish Mockup Clients/*.zip` 5종 + git 이력에서 삭제된 storage 목업(커밋 139fe67)을 압축 해제해 R650/R760/R760xa/R660xs 의 원본 레지스트리 확보.
   - GitHub 코드검색: `sailfishdell/sailfish`(R740 3.15, T630, R720), `cholcombe973/libredfish`(R750 6.00/7.10), `michzimm/redfish_simgen`(R650 5.10), `djsincla/GroundZero`(R740xd 7.00.00.182), `rackerlabs/understack`(R7615 `GET /Bios`), `harvester/seeder`(R630 iDRAC8) → 각 저장소에서 해당 json 만 꺼냄(blobless clone).
   - `dell/dellemc-openmanage-ansible-modules` `idrac_bios.py`, `module_utils/idrac_redfish.py` 로 URI 교차 검증.
   - DMTF 공개 목업(Redfish-Mockup-Server public-*)에는 Dell BIOS 레지스트리가 없어 사용하지 않음.
3. 3차(공식 문서 원문): `dl.dell.com/topicspdf/idrac9-lifecycle-controller-v4x-series_api-guide_en-us.pdf`(4.20.20.20) 를 받아 pdftotext 로 URI 를 추출. v5x/v6x/v7x 파일명은 404, developer.dell.com 은 SPA 라 본문을 못 받음.
4. 실패/미확보: 14G R640/R840/C6420/DSS8440, 15G R750xa/R750xs, 16G R660/R860/C6620/XE9680, R6615 의 개별 레지스트리. R7615 의 BiosRegistry(값 정의). 이것들은 공개 원본이 없다.

## 실장비에서 정확한 값을 얻는 방법

```
GET /redfish/v1/Systems/System.Embedded.1/Bios/BiosRegistry   # 정의(type, allowed values, Hidden/ReadOnly 상태)
GET /redfish/v1/Systems/System.Embedded.1/Bios                # 실제 노출 속성 + 현재값
GET /redfish/v1/Systems/System.Embedded.1/Bios/Settings       # 대기값
```
R6615/R840/DSS8440 등 대리 모델은 위 3개를 받아 이 디렉터리 json 과 비교하면 된다.

## 출처

- Dell iDRAC-Redfish-Scripting (목업·스크립트): https://github.com/dell/iDRAC-Redfish-Scripting
- iDRAC9 Redfish API Guide 4.20.20.20: https://dl.dell.com/topicspdf/idrac9-lifecycle-controller-v4x-series_api-guide_en-us.pdf
- dellemc.openmanage Ansible: https://github.com/dell/dellemc-openmanage-ansible-modules (plugins/modules/idrac_bios.py, plugins/module_utils/idrac_redfish.py)
- 제3자 실캡처: https://github.com/djsincla/GroundZero (tests/fixtures/dell-r740xd), https://github.com/cholcombe973/libredfish (tests/mockups/dell, dell_multi_dpu), https://github.com/michzimm/redfish_simgen (dell_server.template), https://github.com/sailfishdell/sailfish (Mockup-Datasets), https://github.com/rackerlabs/understack (tests/json_samples/bmc_chassis_info/R7615)
- Dell DSA-2026-075 (R6615/R7615 같은 BIOS 수정 버전): https://www.dell.com/support/kbdoc/000432584
