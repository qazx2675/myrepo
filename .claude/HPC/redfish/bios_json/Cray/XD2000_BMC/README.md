# HPE Cray XD2000 BMC (XD220v / XD225v / XD295v): BIOS 조사

| 항목 | 내용 |
|---|---|
| 해당 사용자 모델 | **XD220V** (XD2000 섀시의 1U 2P Intel 노드. Sapphire Rapids 와 Emerald Rapids 구성이 있음) |
| 관리 컨트롤러 | HPE Cray XD BMC. iLO 가 아니고 AMI MegaRAC 계열이다. ODM 은 Inventec (BMC Web UI 가이드에 "Used with permission from Inventec Corporation") |
| 프로토콜 | **json** (Redfish). verified |
| BIOS | AMI Aptio UEFI. SMBIOS 문자열 `American Megatrends International, LLC. CU2K_5.xx_vX.XX` |
| 공개된 최신 BIOS | `CU2K_5.32_v3.80` (Ubuntu 인증 목록의 SPR·EMR 두 구성에서 봄. HPE 릴리스 노트는 찾지 못함). 이전 값: CU2K_5.32_v3.10, CU2K_5.29_v1.50, CU2K_5.27_v0.81(2022, PFUT 예시) |
| BMC 펌웨어 최신 | unknown (공개 릴리스 노트 없음) |
| BIOS 속성 | **unavailable**. 공개 Attribute Registry나 덤프가 없어서 0개다 (bios_attributes.json 의 `attributes: []`) |
| Jev | 생략 (이 환경에 키 없음). 판정 기준은 "문서 2중 출처" 하나뿐 |

## 1. BIOS 관련 URI

AMI MegaRAC 의 시스템 ID 는 `Self` 이다.

| URI | 메서드 | 상태 | 근거 |
|---|---|---|---|
| `/redfish/v1/Systems/Self` | GET (`BiosVersion`) | **verified** | ① HPE 공식 도구 CrayXD_PFUT `Power_Cycle.py` 가 XD220v/XD225v/XD295v 에 `Systems/Self/Actions/ComputerSystem.Reset` 사용 ② Cray CSM "Gigabyte(AMI) 노드는 /redfish/v1/Systems/Self", Lenovo TSM(AMI) 레퍼런스 |
| `/redfish/v1/Systems/Self/Bios` | GET | **verified** (XD220v 실장비 응답은 미확인) | ① HPE Server Management Portal "BIOS data model": "In an HPE CrayXD 255v it points toward /redfish/v1/Systems/Self/Bios/SD" (아래 주의 참조) ② AMI MegaRAC Redfish 레퍼런스(Lenovo TSM) `GET /redfish/v1/Systems/Self/Bios` ③ Cray-HPE/hms-scsd 의 Gigabyte(AMI) 구현 |
| `/redfish/v1/Systems/Self/Bios/SD` | GET / PATCH(PUT, POST) | **verified** | 위 ①②③과 같은 출처, 그리고 NVIDIA DGX H100(AMI) `PATCH .../Bios/SD` + `If-Match:*`. Bios 응답의 `@Redfish.Settings.SettingsObject` 가 이 경로를 가리킨다 |
| `/redfish/v1/Systems/Self/Bios/Actions/Bios.ResetBios` | POST `{"ResetType":"Reset"}` | unverified | AMI 레퍼런스(Lenovo TSM) 한 곳에서만 확인 |
| `/redfish/v1/Systems/Self/Bios/Actions/Bios.ChangePassword` | POST `{"PasswordName","OldPassword","NewPassword"}` | unverified | AMI 레퍼런스 한 곳에서만 확인. `PasswordName` 값은 플랫폼마다 다르다 (Lenovo 는 `SETUP001`) |
| `/redfish/v1/Systems/Self/Bios/ResetBiosActionInfo`, `.../ChangePasswordActionInfo` | GET | unverified | AMI 레퍼런스 응답 예시에만 나옴 |
| `/redfish/v1/Registries` | GET | verified | DGX H100(AMI), hms-scsd. 이 중 `BiosAttributeRegistry*` 항목을 찾는다 |
| `/redfish/v1/Registries/<AttributeRegistry>` → `Location[].Uri` | GET | unverified (파일명 unknown) | 파일명이 플랫폼마다 다르다. Gigabyte: `/redfish/v1/Registries/BiosAttributeRegistry.json`. DGX: `BiosAttributeRegistry106.en-US.1.0.6.json` |
| `/redfish/v1/UpdateService/FirmwareInventory/BIOS` | GET | verified | PFUT `XD295v_XD220v_XD225v/Bios_Update.py` |
| `/redfish/v1/UpdateService/upload` | POST multipart | verified | PFUT. BIOS 이미지(`.hpm`, `OemParameters {"ImageType":"HPM"}`)를 갱신할 때 쓰며 설정 변경용이 아니다. 진행률은 `UpdateService` 의 `Oem.AMIUpdateService.UpdateStatus/FlashPercentage` |

주의:
- HPE 포털 문구의 "CrayXD 255v" 라는 모델은 존재하지 않는다. 오타로 보이며 XD225v(같은 XD2000 BMC)일 가능성이 높지만 확정할 수 없다. 그래서 Bios 와 Bios/SD 의 verified 판정은 "HPE Cray XD 노드 + AMI 공통" 수준이다. XD220v 장비에서 GET 으로 최종 확인한다.
- AMI 레퍼런스의 전제는 "AMI BIOS REST/Redfish 모듈과 BMC Host Interface 가 있어야 동작"이다. XD220v BIOS 에 이 모듈이 들어 있는지 HPE 문서로는 확인하지 못했다. 들어 있지 않으면 `Bios` 의 `Attributes` 가 비어 있거나 리소스가 없을 수 있다.
- `Bios/SD` 에 대기 중인 변경이 없으면 GET 결과가 **404** 다 (AMI 레퍼런스). 정상 동작이다.
- 호스트가 부팅 중이면 BMC 가 인벤토리를 처리할 때까지 PATCH 와 Action 이 **503** 으로 거부된다 (AMI 레퍼런스).
- PATCH 할 때 `If-Match` 가 없으면 AMI 는 요청을 거부한다 (hms-scsd 주석 "gigabyte will reject any patch request that does not have a If-Match header"). SD 가 없으면 `If-Match: *` 를 쓴다.
- HPE 의 `ilorest`, `/redfish/v1/Systems/1/Bios/Settings`, `Oem/Hpe` 는 쓰지 않는다. PFUT 는 DMTF `redfish` 파이썬 라이브러리(PyPI redfish 3.1.6)를 쓴다. HPE python-redfish-utility(ilorest)와 python-ilorest-library 소스에는 Cray XD 나 `Systems/Self` 참조가 없다.

## 2. 사용 예 (AMI 공통 방식, 플레이스홀더)
```bash
# 현재 BIOS 값과 레지스트리 이름
curl -k -u <USER>:<PASSWORD> https://<BMC_IP>/redfish/v1/Systems/Self/Bios
# 대기 중인 변경 (없으면 404)
curl -k -u <USER>:<PASSWORD> https://<BMC_IP>/redfish/v1/Systems/Self/Bios/SD
# 변경 예약 (속성명은 반드시 레지스트리에서 확인. 아래 <ATTR> 는 자리표시)
curl -k -u <USER>:<PASSWORD> -X PATCH https://<BMC_IP>/redfish/v1/Systems/Self/Bios/SD \
  -H 'Content-Type: application/json' -H 'If-Match: *' \
  -d '{"Attributes":{"<ATTR>":"<VALUE>"}}'
# 적용하려면 재부팅
curl -k -u <USER>:<PASSWORD> -X POST https://<BMC_IP>/redfish/v1/Systems/Self/Actions/ComputerSystem.Reset \
  -H 'Content-Type: application/json' -d '{"ResetType":"ForceRestart"}'
```

## 3. 실장비에서 속성 전체 수집 (unavailable 항목을 채우는 방법)
1. `GET /redfish/v1/Systems/Self/Bios`: `AttributeRegistry` 값과 `Attributes` 전체(현재값)를 받는다.
2. `GET /redfish/v1/Registries`: 1에서 얻은 이름의 멤버를 고르고, 그 `Location[].Uri`(en)를 GET 한다. gzip 이면 `--compressed` 를 붙인다.
3. 레지스트리의 `RegistryEntries.Attributes[]` 에서 `AttributeName / Type / Value[].ValueName / DefaultValue / ReadOnly` 를 뽑아 이 폴더의 bios_attributes.json `attributes` 배열로 옮긴다. 항목 수가 수백 개이므로 스크립트로 변환한다.
4. BIOS 버전(`Systems/Self` 의 `BiosVersion`)을 meta.firmware_version 에 적고 status 를 `verified (실장비)` 로 바꾼다.

## 4. 시도 내역 (총 3회: 1차 + 범위 확대 재시도 2회)
| 회차 | 출처 | 결과 |
|---|---|---|
| 1차 (HPE 공식 문서) | XD220v Server User Guide `sd00002298en_us` (TOC API로 전체 목차 확인, "UEFI System Utilities", "Secure Boot", "Embedded UEFI Shell", "Redfish", "System Inventory" 본문), XD220v Maintenance & Service Guide `sd00002299en_us` ("Using UEFI System Utilities": F9 진입, F4 저장) | 메뉴명과 속성명 목록 없음. Redfish 는 일반 설명뿐 |
| 재시도 1 (다른 문서군) | XD2000 BMC Web UI User Guide `dp00002278en_us` (공개 PDF, 10-150118-001, 2026-08, 6,905줄 전체 텍스트 검색), "HPE Cray XD2000 BMC Redfish User Guide" 와 "XD2000 System UEFI User Guide" 검색, HPE Server Management Portal BIOS 데이터모델, Ubuntu 인증 목록 | Web UI 에는 BIOS 설정 화면이 없다 ("See BIOS UG" 라고만 참조). Redfish UG 와 UEFI UG 는 공개 위치를 찾지 못함. ODM 이 Inventec 인 것, BIOS 버전, Cray XD 의 `Systems/Self/Bios/SD` 를 확보 |
| 재시도 2 (공개 코드/샘플) | HewlettPackard/CrayXD_PFUT (전체 grep), Cray-HPE/docs-csm (`ncn_bios.md`, Gigabyte 문서), Cray-HPE/hms-scsd `cmd/scsd/bios.go`, HPE python-redfish-utility·python-ilorest-library, AMI MegaRAC Redfish 레퍼런스(Lenovo TSM: bios, get_bios_sd, post_bios_reset, post_change_bios_password, post_put_patch_change_bios_settings, redfish_settings), NVIDIA DGX H100 Redfish(AMI), 웹 검색("CU2K_5", "Systems/Self/Bios" + XD2000/Inventec) | AMI 공통 URI 와 동작은 확보. XD2000 전용 속성 덤프나 레지스트리는 공개된 것이 없다. GitHub 코드 검색 API 는 세션 정책상 차단되어 저장소별 clone 과 grep 으로 대신함 |
| 결론 | | URI 는 AMI 공통 + HPE Cray XD 언급으로 확정. 속성은 **unavailable**: 추측으로 만들지 않았고, 다른 AMI 플랫폼(Gigabyte, Lenovo, DGX)의 속성명은 체계가 서로 달라 옮기지 않았다 |

## 5. 이전 결과(back/HPE/XD220V) 정정
- 이전: "`/redfish/v1/Systems/<id>/Bios` unverified, id 미확인" → 정정: id 는 `Self` 이고 Bios 와 Bios/SD 는 verified (조건부).
- 이전: "iLO 5/6 여부 unknown" → 정정: iLO 가 아니고 AMI MegaRAC 계열, ODM Inventec (id_json/Cray/CONTROLLERS.md 와 일치).
- 이전: "최신 펌웨어 unknown" → BIOS 는 공개 목록 기준 CU2K_5.32_v3.80 까지 확인. BMC 는 여전히 unknown.

## 6. 구버전 차이
diff 파일 없음. 비교할 속성 데이터가 없어서 만들지 않았다. Redfish BIOS 를 지원하지 않는 펌웨어 범위는 ../UNSUPPORTED.md 참조.

## 출처
- HPE Cray XD220v Server User Guide: https://support.hpe.com/hpesc/public/docDisplay?docId=sd00002298en_us
- HPE Cray XD220v Maintenance and Service Guide: https://support.hpe.com/hpesc/public/docDisplay?docId=sd00002299en_us
- HPE Cray XD2000 BMC Web UI User Guide (PDF): https://support.hpe.com/hpesc/public/docDisplay?docId=dp00002278en_us
- HPE Server Management Portal, BIOS data model: https://servermanagementportal.ext.hpe.com/docs/concepts/biosdatamodel/
- HewlettPackard/CrayXD_PFUT: https://github.com/HewlettPackard/CrayXD_PFUT (`Power_Cycle.py`, `XD295v_XD220v_XD225v/Bios_Update.py`, `pre_requistes.txt`)
- Cray-HPE/hms-scsd: https://github.com/Cray-HPE/hms-scsd (`cmd/scsd/bios.go`)
- Cray-HPE/docs-csm: https://github.com/Cray-HPE/docs-csm (`operations/node_management/Find_Node_Type_and_Manufacturer.md`)
- AMI MegaRAC Redfish 레퍼런스 (Lenovo ThinkSystem System Manager): https://pubs.lenovo.com/tsm/get_bios_sd , /tsm/post_bios_reset , /tsm/post_change_bios_password , /tsm/post_put_patch_change_bios_settings
- NVIDIA DGX H100 Redfish: https://docs.nvidia.com/dgx/dgxh100-user-guide/redfish-api-supp.html
- Ubuntu 인증 (BIOS 버전): https://ubuntu.com/certified/202405-33985 , https://ubuntu.com/certified/202302-31299
