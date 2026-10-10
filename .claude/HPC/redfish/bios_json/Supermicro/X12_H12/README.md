# Supermicro X12 / H12 (BMC 펌웨어 1.xx, 예 01.07~01.08)

- 프로토콜 판정(BIOS): **json** (Redfish User Guide 의 적용 범위 "starting with Gen 12").
- 사용자 보유 모델: 없음.
- 속성: **unavailable** (공개 문서/덤프 없음). `bios_attributes.json` 메타만.
- 최신 펌웨어(피드 2026-07-19): X12DPi-NT6 BMC 1.08.12/BIOS 2.5 ; X12DPG-QT6 BMC 01.07.09/BIOS 2.4 ; H12SSL-i BMC 1.08.03/BIOS 3.7 ; H12DSi-NT6 BMC 01.06.32/BIOS 3.7. 보드마다 BIOS 속성이 다름(Intel X12 vs AMD H12).

## 근거
| 항목 | 내용 | 출처 | 상태 |
|---|---|---|---|
| 지원 범위 | Redfish User Guide 6.1: "applies to platforms starting with Gen 12" | https://www.supermicro.com/manuals/other/RedfishUserGuide.pdf | verified |
| Gen 12 이후 변경 | 레지스트리 GET 권한 NONE -> Login ("Since Gen 12") | Guide 6.1 Registries | verified |
| URI | `Bios`, `Bios/SD`, `Bios/Actions/Bios.ResetBios`, `Bios/Actions/Bios.ChangePassword`, `Bios/ChangePasswordActionInfo`, `Registries/BiosAttributeRegistry` (라이선스 SFT-DCMS-SINGLE) | Guide 6.1 Available APIs ; HTML 가이드 https://www.supermicro.com/manuals/other/redfish-user-guide-4-0/Content/general-content/available-apis.htm | verified |
| 최초 Redfish BMC 빌드 | HTML 가이드는 virtual media 만 "Redfish 2020.3 : X12/H12 (BMC FW 1.03.xx)" 라고 명시. BIOS 기능 시작 빌드는 없음 | 위 available-apis | unverified |
| 레지스트리 파일 | 일부 문서는 `/registries/BiosAttributeRegistry.1.0.0.json` 로 서술, 장비는 Location.Uri 를 줌 | Guide 6.1 / 4.0 | 경로 형식은 unverified -> 장비의 Registries 컬렉션을 따를 것 |

공통 BIOS 규칙(전 세대 공통)은 `../X13_H13/README.md` 의 "BIOS URI/규칙" 참조.

## 시도 내역
1차: Guide 6.1 PDF, 4.0 HTML, BMC User Guide X12/H12(BMC_Users_Guide_X12_H12.pdf, Redfish 언급은 계정/NIC 뿐, BIOS 속성 목록 없음(grep)), 피드. 2차: GitHub 코드 검색(이 세션은 지정 저장소만 허용), 웹 검색, libredfish 이슈 -- X12 속성 덤프 없음. Jev 생략.

## 실장비 덤프
`GET /redfish/v1/Registries` -> `BiosAttributeRegistry` -> `Location[0].Uri`.
