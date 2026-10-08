# HPE ProLiant Gen9 (iLO 4) - DL360 G9 / XL170r G9 / XL250 G9 / XL270d G9

## 프로토콜 판정: json
- iLO 4 펌웨어 2.30 이상은 Redfish 적합: "iLO 4 2.30 is Redfish 1.0 conformant" (https://hewlettpackard.github.io/ilo-rest-api-docs/ilo4/), "iLO 4 is Redfish conformant starting with HPE iLO 4 2.30" (https://servermanagementportal.ext.hpe.com/docs/redfishclients/python-redfish-library). 서비스 루트 `/redfish/v1/`.
- 2.00~2.29 는 `/rest/v1` 레거시 REST 만 (아래 old_fw_diff.md).
- jev 판정(1차): 1.00 / 독립 출처 2건 일치 -> **verified**.

## 최신 펌웨어
- iLO 4 최신: 2.82 (HPE 커뮤니티 안내 기준, 공식 릴리스노트 직접 확인은 못함 -> 확인불가 부분 있음).
- System ROM 패밀리(검색 결과 기준, 추정 포함): DL360 G9=P89(최신 2.92 2021-11-23), XL250a G9=U13 계열(2.92) 로 보이나 불확실, XL170r G9=U14(2.90), XL270d G9=U25(확인된 최신 2.74, 2019). 정확한 매핑은 HPE Support Center 확인 필요.

## URI (iLO 4)
| URI | 용도 | 상태 | 시도 |
|---|---|---|---|
| `/redfish/v1/` | 서비스 루트 | verified (jev 1.00 + 독립 출처 2) | 1차 |
| `/redfish/v1/Systems/1/Bios` | 현재 BIOS 속성(읽기 전용) | unverified (jev 0.69, 재조사 0.67; 문서는 `/rest/v1/Systems/{item}/Bios` 와 "/redfish/v1 에 미러링"을 명시, 모두 정합하다고 Claude 판단하나 iLO 4 문서에 /redfish/v1 BIOS 직접 예시 없음) | 1차+재조사+합동 |
| `/redfish/v1/Systems/1/Bios/Settings` | 대기(pending) 설정, PATCH/PUT, 재부팅 후 적용 | unverified (위와 동일) | 1차+재조사+합동 |
| `/redfish/v1/Systems/1/Bios/BaseConfigs` | 기본값 | unverified (iLO 4 문서 `bios/BaseConfigs/` 예시만) | 1차 |
| `/redfish/v1/Registries` (속성 레지스트리, gzip) | Menus/Dependencies/BaseConfigs | unverified | 1차 |
- 시스템 ID 는 문서상 `{item}` (실제 Systems/1). 대소문자 불일치 가능(iLO 는 소문자 `/redfish/v1/systems/1/bios/` 도 응답).
- 하드코딩 금지, ComputerSystem 의 `Oem/Hp/links/BIOS` 링크를 따라가라고 문서가 권고.
- 쓰기 시 BIOS Admin 패스워드가 있으면 `X-HPRESTFULAPI-AuthToken` (SHA-256 대문자 hex) 헤더 필요.

## bios_attributes.json
- 출처: iLO 4 데이터모델 레퍼런스의 `HpBios Attributes` 섹션 (iLO 4 전체 합집합, 235개 항목; `SanitizeProc{1,2}Dimm{n}`은 패턴 2건).
- **한계**: 모델/ROM 별 레지스트리(BiosAttributeRegistryP89 등)를 웹에서 확보하지 못했다. 모델마다 실제로 노출되는 속성은 다를 수 있다(예: NVDIMM, GPU/PCIe 관련은 모델 의존). 기본값은 문서에 없어 null. 문서 추출은 요약 도구를 거쳤으므로 누락 가능성이 있다(추정 아님, 단 완전성 보증 불가).
- 실장비에서 `GET /redfish/v1/Registries` -> BiosAttributeRegistry<ROM>.* 로 최종 확인 권장.

## 묶음 근거
- 4개 모델 모두 iLO 4 + UEFI BIOS(Gen9) 로 같은 HpBios 데이터모델을 공유. ROM 패밀리가 달라 실제 속성은 다를 수 있으나 모델별 레지스트리 미확보라 **unverified 그룹**으로 묶음.
