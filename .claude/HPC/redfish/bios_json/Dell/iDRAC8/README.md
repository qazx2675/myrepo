# Dell iDRAC8 (13G) — BIOS 속성

- **사용자 보유 모델 없음.** 비교 기준과 지원 범위를 정리하려고 조사했다.
- 프로토콜: **json** (Redfish). 단 **BIOS 리소스는 iDRAC8 2.50.50.50 이상**에서만 있다. 그보다 낮으면 WS-MAN(**xml**)·racadm 로 다뤄야 하고, 2.40.40.40 이상이면 Redfish SCP 도 쓸 수 있다. 근거는 `old_fw_diff.md` 에 있다.
- Jev 판정: **생략** (`~/.jev-claude.env` 없음). 문서 2중 출처로만 판정했다.
- 조사일: 2026-10-10

## 파일

| 파일 | 내용 | 수 | 상태 |
|---|---|---|---|
| `bios_attributes.json` | PowerEdge **R630** 실장비의 `GET /redfish/v1/Systems/System.Embedded.1/Bios/BiosRegistry` 원본을 변환. iDRAC **2.81.81.81**, BIOS **2.13.0** | 249 (레지스트리 = /Bios 노출 249) | verified (원본 JSON). 13G 대표값 |
| `old_fw_diff.md` | Redfish BIOS 지원 시작 버전(2.30 → 2.40 → 2.50) + T630(2.50.50.50/BIOS 2.2.2) 와 R630(2.81) 노출 이름 비교 | — | 문서 verified / 캡처 비교 |

- json 필드 설명은 `../iDRAC9/README.md` 와 같다. 차이점은 다음과 같다.
  - 13G 레지스트리 HelpText 에는 "Default: X" 문구가 있는 항목이 있다. 그중 ValueDisplayName 과 정확히 일치하는 14개만 `default` 에 넣고 `default_source` 를 달았다. 나머지는 null 이다.
  - 13G 레지스트리에는 `ResetRequired` 필드가 없다 → null.
  - `in_T630_2.50_bios_resource`: 같은 이름이 T630(2.50.50.50) `GET /Bios` 에도 있는지 표시한다. `_meta.T630_extra_attributes_not_in_R630_registry` 에는 T630 에만 있는 62개 이름(디버그/SATA 포트/슬롯 4~8 등)을 넣었다.
- 13G 는 모델마다 BIOS 가 다르다(R630/R730/T630/FC630/C6320 …). 다른 13G 모델은 이 목록과 일부 다를 수 있다(unverified). 공개 원본 레지스트리는 R630 것만 찾았다.
- 이전 back/ 결과에는 iDRAC8 자료가 없었다(신규).

## 최신 펌웨어

- iDRAC8 마지막 펌웨어는 2.86.86.86 으로 알려져 있다(Dell SLN310710 를 인용한 웹검색 요약. 원문 403) → **unverified**. 캡처는 2.81.81.81 이다.
- BIOS 속성은 BIOS 버전이 결정하므로, 2.81 과 2.86 사이에서 속성이 바뀔 가능성은 낮다고 본다. 같은 BIOS 2.13.0 기준이면 같다(iDRAC9 R750 근거: `../iDRAC9/old_fw_diff.md`).

## BIOS 관련 URI (iDRAC8 ≥ 2.50.50.50)

| URI / 액션 | 용도 | 1차 출처 | 2차 출처 | 상태 |
|---|---|---|---|---|
| `/redfish/v1/Systems/System.Embedded.1/Bios` (GET) | 현재 속성 | iDRAC8/7 2.50.50.50·2.60.60.60 Redfish API Guide (`/redfish/v1/Systems/<ID>/Bios`) | R630 2.81·T630 2.50 실캡처 | verified |
| `/redfish/v1/Systems/System.Embedded.1/Bios/Settings` (GET/PATCH) | 대기값 | API Guide 2.60 (Settings resource, PATCH=SystemControl) | R630 실캡처 `@Redfish.Settings.SettingsObject` | verified |
| `/redfish/v1/Systems/System.Embedded.1/Bios/BiosRegistry` (GET) | 레지스트리 | API Guide 2.50/2.60 (AttributeRegistry) | R630·R720 실캡처, `/redfish/v1/Registries/BiosAttributeRegistry.v1_0_0` Location | verified |
| `/redfish/v1/Registries/BiosAttributeRegistry.v1_0_0` (GET) | 레지스트리 파일 위치 | 실캡처(T630/R630) | sailfish R720 | verified |
| `.../Bios/Actions/Bios.ResetBios` (POST) | 기본값 복원 | API Guide 2.60 (권한 SystemControl) | 실캡처 Actions | verified |
| `.../Bios/Actions/Bios.ChangePassword` (POST: PasswordName/OldPassword/NewPassword) | BIOS 암호 | API Guide 2.60 Table 13 | 실캡처 Actions | verified |
| `.../Bios/Settings/Actions/Oem/DellManager.ClearPending` (POST) | 대기값 삭제 | R630·R720 실캡처 | iDRAC9 API Guide 4.20, Ansible `CLEAR_PENDING_URI` | verified (iDRAC8 문서의 `/Bios/Actions/Oem/...` 표기는 오기로 판단 → old_fw_diff.md) |
| `/redfish/v1/Managers/iDRAC.Embedded.1/Jobs` (POST `{"TargetSettingsURI":"/redfish/v1/Systems/System.Embedded.1/Bios/Settings"}`) | 설정 적용 Job | API Guide 2.60 (TargetSettingsURI 예시) | — | unverified (iDRAC8 경로 단일 출처. iDRAC9 는 `/Oem/Dell/Jobs` 도 씀) |
| SCP `/redfish/v1/Managers/iDRAC.Embedded.1/Actions/Oem/EID_674_Manager.ExportSystemConfiguration` / `ImportSystemConfiguration` / `ImportSystemConfigurationPreview` | BIOS 포함 설정 일괄 내보내기·가져오기 (≥2.40.40.40) | 2.40.40.40 Release Notes "Server Configuration Profile via Redfish" | sailfish C6320(2.40)/FC630(2.41)/R630(2.81) 실캡처 Manager.Actions | verified |

## 시도 내역

1. 1차(문서): Dell iDRAC7/8 릴리스노트 2.30.119.30 / 2.40.40.40 / 2.50.50.50 / 2.81.81.81, Redfish API Guide 2.50.50.50 / 2.60.60.60 PDF 를 `dl.dell.com/topicspdf/...` 에서 받아(curl 기본 UA 는 Akamai 403 → 브라우저 UA 로 200) pdftotext 로 원문을 검색했다. 2.86.86.86 릴리스노트 URL 은 404.
2. 2차(공개 캡처): GitHub 코드검색 → `harvester/seeder` (pkg/events/testdata/mockup: R630 2.81.81.81 전체 BiosRegistry), `sailfishdell/sailfish` (T630 2.50.50.50 Bios, FC630 2.41.40.40, C6320 2.40.40.40, R720 iDRAC7 2.50.50.50).
3. 실패: R730/R830/C6320 의 BiosRegistry, iDRAC8 2.86 캡처.

## 실장비에서 확인하는 방법

`GET /redfish/v1/Managers/iDRAC.Embedded.1` 의 `FirmwareVersion` 이 2.50.50.50 미만이면 Redfish 로 BIOS 를 다룰 수 없다. 2.50.50.50 이상이면 `GET .../Bios/BiosRegistry` 로 해당 모델 목록을 받는다.

## 출처

- iDRAC7/8 2.40.40.40 Release Notes: https://dl.dell.com/topicspdf/idrac7-8-lifecycle-controller-v2404040_Release-Notes_en-us.pdf
- iDRAC7/8 2.50.50.50 Release Notes: https://dl.dell.com/topicspdf/idrac7-8-lifecycle-controller-v2505050_Release-Notes_en-us.pdf
- iDRAC8 2.30.119.30 Release Notes: https://dl.dell.com/manuals/all-products/esuprt_software/esuprt_remote_ent_sys_mgmt/esuprt_rmte_ent_sys_rmte_access_cntrllr/idrac8-lifecycle-controller-v2.30.119.30_Release%20Notes_en-us.pdf
- iDRAC8 2.81.81.81 Release Notes: https://dl.dell.com/topicspdf/idrac8-lifecycle-controller-v2818181_release-notes_en-us.pdf
- iDRAC7/8 2.50.50.50 Redfish API Guide: https://dl.dell.com/topicspdf/idrac7-8-lifecycle-controller-v2.50.50.50_API-Guide_en-us.pdf
- iDRAC7/8 2.60.60.60 Redfish API Guide: https://dl.dell.com/topicspdf/idrac7-8-lifecycle-controller-v2606060_API-Guide_en-us.pdf
- R630 캡처: https://github.com/harvester/seeder/tree/main/pkg/events/testdata/mockup
- T630/FC630/C6320/R720 캡처: https://github.com/sailfishdell/sailfish/tree/master/Mockup-Datasets
