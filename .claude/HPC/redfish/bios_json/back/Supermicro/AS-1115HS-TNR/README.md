# Supermicro AS-1115HS-TNR

- 모델: AS-1115HS-TNR (보드 H13SSH, AMD EPYC 9004 계열 1U)
- 프로토콜 판정: **json** (Redfish). 근거: Supermicro 는 X10/H11 이후 플랫폼에 Redfish 제공
  - https://www.supermicro.com/en/support/manuals/product/software/redfish-user-guide/Content/general-content/registries.htm
  - https://www.supermicro.com/manuals/other/redfish-user-guide-4-0/Content/general-content/bios-configuration.htm
- 최신 펌웨어 (Supermicro 다운로드 피드 기준): `H13SSH_4.0_AS01.13.11_SAA1.5.0-p9` (BIOS 4.0 / BMC 01.13.11). 날짜는 피드에 없음.
  - https://www.supermicro.com/en/support/resources/downloadcenter/firmware/AS-1115HS-TNR/BIOS/feed

## URI 와 검증상태
| URI | 용도 | jev | 독립 출처 | 상태 | 시도 |
|---|---|---|---|---|---|
| /redfish/v1/Systems/1/Bios | 현재 속성 GET, PATCH(Attributes) | 0.99 / 0.98 | 가이드 4.0 BIOS Configuration + 별도 검색결과(H12SSW 3.10.04 릴리스노트가 PATCH 언급) | verified | 1차 |
| /redfish/v1/Systems/1/Bios/SD | Pending Settings (Bios/Settings 아님) | 0.99 / 0.98 | 동일 | verified | 1차 |
| /redfish/v1/Systems/1/Bios/Actions/Bios.ResetBios, Bios.ChangePassword | 액션 | 미실행 | 가이드 단일 문서 | unverified (단일 출처) | 1차 |
| /redfish/v1/Registries/BiosAttributeRegistry | 레지스트리 파일(Registries 컬렉션 목록) | 미실행 | Registries 페이지 | 문서상 확인 | 1차 |
| /registries/BiosAttributeRegistry.1.0.0.json | 가이드의 레지스트리 JSON 예시 경로 | - | 가이드 일반 예시, 실제 장비와 다를 수 있음 | unverified | 1차 |

주의: 가이드는 레지스트리 이름을 `BiosAttributeRegistry.v1_0_0` 형태로도 언급한다. 실제 경로는 장비의 `/redfish/v1/Registries` 에서 확인해야 한다. 속성 이름에 `QuietBoot_0027` 처럼 숫자 접미사가 붙는 것이 Supermicro 레지스트리 특징(가이드 예시 기준).

## bios_attributes.json 미생성 사유
H13SSH 의 전체 BIOS Attribute Registry 는 공개 문서에 없고 BMC 의 레지스트리 JSON 에만 있다(가이드에는 QuietBoot_0027, OptionROMMessages_0028 두 예시만). 일부 발췌는 금지이므로 파일을 만들지 않았다. 실제 장비에서 `GET /redfish/v1/Registries/BiosAttributeRegistry` 로 확보 필요.

## 구버전 차이
확인불가 (펌웨어별 속성 변경 이력 공개 자료 없음).

## 그룹 근거
단일 모델.
