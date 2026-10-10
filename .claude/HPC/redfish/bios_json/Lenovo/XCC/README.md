# Lenovo XCC (XClarity Controller, "XCC1") - BIOS 속성

조사일 2026-10-10. 브랜치 bios-collect. Jev API 키 없음 -> **Jev 판정 생략**, "공식 문서 + 독립 2차 출처"로만 verified 판정.

## 대상 모델 / 프로토콜
| 사용자 모델 | 플랫폼 | 관리망 | 프로토콜 | 근거 |
|---|---|---|---|---|
| SR630, SR650, SD530 | Intel Xeon SP 1st/2nd Gen (Purley) | XCC | **json (Redfish)** | SD530 제품 가이드 LP0635: "XClarity Controller (XCC) ... Pilot4 XE401"; SR630 LP0643 / SR650 LP0644 제품 가이드에 XClarity Controller 기재; Lenovo 공식 python-redfish-lenovo `products_supported.txt` 의 "ThinkSystem Intel (Purley)" 열에서 BIOS 스크립트(get_all_bios_attributes.py 등) 지원 Yes |
| SR630 V2 | Intel Xeon SP 3rd Gen (Whitley) | **XCC (XCC1, XCC2 아님)** | **json (Redfish)** | SR630 V2 제품 가이드 LP1391: "XClarity Controller (XCC) ... Pilot4 XE401 BMC (dual-core ARM Cortex A9)", 플랫폼 "Whitley". 반면 V3(SR645 V3 LP1607, SR675 V3 LP1611, SD650 V3 LP1603)은 "XClarity Controller 2 (XCC2) ... AST2600", V4 는 XCC3 (LP1607 의 "XCC3 Premier (V4 servers) or XCC2 Platinum (V3 servers)"). 기존 id_json/Lenovo/XCC 의 결론(LP0880 근거)과 일치 -> **폴더 정정 불필요** |

XML API 없음. Lenovo XCC 는 Redfish JSON 만 사용 (모든 Lenovo 버전 json).

## URI (Lenovo 공식 REST API 가이드 xcc-restapi + 2차: 실장비 덤프/공식 샘플 스크립트)
| 용도 | URI | 검증 |
|---|---|---|
| BIOS 현재 | `GET /redfish/v1/Systems/1/Bios` | verified (공식 문서 + SR670 V2 실덤프 + 공식 python-redfish-lenovo) |
| BIOS 대기(Pending) | `GET/PATCH /redfish/v1/Systems/1/Bios/Pending` - `Bios.@Redfish.Settings.SettingsObject`. **`/Bios/Settings` 는 없음.** ApplyTime = OnReset | verified |
| 속성 레지스트리 | `GET /redfish/v1/schemas/registries/BiosAttributeRegistry.1.0.0.json` (`Bios.AttributeRegistry = BiosAttributeRegistry.1.0.0`) | verified (문서; 장비에서만 내용 취득 가능) |
| ResetBios | `POST /redfish/v1/Systems/1/Bios/Actions/Bios.ResetBios` (본문 없음, 200 RebootRequired) | verified |
| ChangePassword | `POST .../Bios/Actions/Bios.ChangePassword` `{PasswordName: UefiAdminPassword|UefiPowerOnPassword, OldPassword, NewPassword}` (8~20자, 같은 문자 3연속 불가). ActionInfo: `/redfish/v1/Systems/1/Bios/ChangePasswordActionInfo` | verified (문서; 실덤프에서 Actions 확인) |
| 펌웨어 | `/redfish/v1/UpdateService/FirmwareInventory/UEFI` (Bios.Links.ActiveSoftwareImage) | verified (문서 + 실덤프) |
| AMT 시험 옵션 | `PATCH` (XCC/XCC2 문서에만 "PATCH - Configure AMT test options" 페이지, XCC3 문서에는 없음) | 문서만 |

주의: LP1836/1977/2210(Lenovo Press 튜닝 가이드)은 `https://<BMC>/redfish/v1/Systems/Self/Bios` 와 `.../Bios/SD` 를 적고 있으나 이는 REST API 가이드(Systems/1, Bios/Pending) 및 실덤프(Systems/1, Pending)와 충돌 -> 사용자 모델에는 **Systems/1 + Pending** 을 쓴다. (`Systems/Self` 는 Lenovo SR635/SR655 AMD 1세대용, python-redfish-lenovo `lenovo_set_bios_boot_order.py` 에서 확인.) unverified 상태의 문서 불일치로 기록.

## 속성 목록 상태: **partial**
`bios_attributes.json` 에 출처별로 구한 전부를 담았다 (이름 합집합 231개 + 장치(NIC/RAID) 전용 패턴 114개).
- **SR630 V2 (Whitley)**: 실장비 덤프 1 (ThinkSystem SR670 V2, 같은 Whitley/XCC 세대, XCC TGBT42S 2.83 / UEFI U8E122J, 348 키 = 일반 221 + 장치 전용 127). **SR630 V2 본인 덤프 아님 -> 프록시.** 슬롯/NVMe 베이 속성은 샤시마다 다름.
- **SR630/SR650/SD530 (Purley)**: 공개 전체 목록 없음. 확보한 것은 LP1477 튜닝 가이드(33개 설정: Redfish 이름 + 표시 선택지 + 기본값), 공식 문서의 레지스트리 예시 2개(타입/허용값/기본값/ReadOnly 전체 메타), 운영 설정(sapcc SR650) 값 표기.
- type/allowed_values/default/read_only 는 **공식 문서에 실린 레지스트리 항목 2개(SystemRecovery_POSTWatchdogTimer, SecureBootConfiguration_SecureBootMode)만** 확정, 나머지는 null. `type_hint` 는 JSON 값 타입에서 추정(string/integer). 튜닝 가이드가 준 선택지는 `allowed_values_display`(표시명)이며 Redfish ValueName 철자(공백/`-`/`/` -> `_`, 예 `I/O sensitive` -> `I_OSensitive`, `Efficiency – Favor Performance` -> `Efficiency_FavorPerformance`)는 실덤프 `observed[].value` 로 교차 확인.
- 검증 규칙: `verification: verified` = 속성 이름이 독립 출처 2곳 이상(또는 공식 레지스트리 예시)에서 확인. 값/허용값의 정확성까지 보증하는 것은 아님.
- 이 저장소의 `.claude/HPC/redfish/testdata/lenovo-sr650v3/` 는 **모의(MOCK) 데이터** 이므로 증거로 쓰지 않았다.

## 실장비에서 전체를 얻는 방법
```
curl -sk -u <USER>:<PASSWORD> https://<BMC>/redfish/v1/Systems/1/Bios -o bios.json
curl -sk -u <USER>:<PASSWORD> https://<BMC>/redfish/v1/Systems/1/Bios/Pending -o pending.json
curl -sk -u <USER>:<PASSWORD> https://<BMC>/redfish/v1/schemas/registries/BiosAttributeRegistry.1.0.0.json -o registry.json
onecli config show all --override --log 5 --imm <USER>:<PASSWORD>@<BMC_IP>
```
registry.json 의 `RegistryEntries.Attributes[]` 가 AttributeName/Type/Value[]/DefaultValue/ReadOnly 의 정답이다. (OneCLI 변수명은 `Group.Setting`, Redfish 는 `Group_Setting`)

## 시도 내역 (이번 2회 + 재조사)
1. **1차 (다른 문서군)**: Lenovo XCC/XCC2/XCC3 REST API 가이드의 Bios / Bios Pending GET·PATCH / ResetBios / ChangePassword / AttributeRegistry / AMT 페이지(전부 raw HTML 받아 텍스트로 비교); Lenovo Press LP1477(PDF, 공개)·LP1836·LP1977·LP2210·LP1267; pubs.lenovo.com uefi_xeon_4th / uefi_amd_4th / uefi_xeon_6th UEFI 메뉴 문서(메뉴 라벨만 있고 Redfish 이름 없음 -> 미사용); LXCA scripting 문서(ExtendedV4BIOS 예시 1개). 제품 가이드(LP1391/1392/0635/0643/0644/1603/1607/1611)로 XCC 세대 판정. -> 속성 이름은 튜닝 서브셋 + 예시만.
2. **2차 (공개 코드·샘플)**: GitHub 코드 검색(SystemRecovery_POSTWatchdogTimer, Processors_CStates 등) -> **cholcombe973/libredfish 의 Lenovo 실덤프 2개(SR670 V2 포함)**, weka/tools defaults-db.yml(SR635 V3, XCC2 쪽), sapcc/helm-charts 운영 설정, bmc-toolbox/bmclib 픽스처, lenovo/python-redfish-lenovo 예제(URI 흐름·Systems/Self 분기), spyroot/redfish_ctl 픽스처(문서 예시 복사본), vmware/vcf-readiness(AI 생성 도구, 문서 근거 없음 -> 제외). DMTF org 검색: Lenovo 목업 없음.
3. 재조사 한계: Lenovo 레지스트리 JSON 원문은 BMC 에서만 제공되어 공개 사본 없음 -> 전체 type/allowed/default 는 **unavailable**.

## 파일
- `bios_attributes.json` : 구조화 결과 (첫 키 `_meta`)
- `old_fw_diff.md` : Purley(구) vs Whitley(신) 및 UEFI 펌웨어 차이 증거
